package teams

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Bot Framework authentication constants.
const (
	openIDMetadataURL  = "https://login.botframework.com/v1/.well-known/openidconfiguration"
	botFrameworkScope  = "https://api.botframework.com/.default"
	botFrameworkIssuer = "https://api.botframework.com"
	jwksCacheDuration  = 6 * time.Hour
	tokenExpirySkew    = 30 * time.Second
	clockSkew          = 5 * time.Minute
)

// jwksCache caches JWKS keys fetched from the Bot Framework OpenID metadata.
type jwksCache struct {
	mu        sync.Mutex
	keys      map[string][]string // kid -> x5c certs
	fetchedAt time.Time
	client    *http.Client
}

// getKey returns x5c certs for the given kid. Refreshes cache if expired or kid not found.
func (c *jwksCache) getKey(kid string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if certs, ok := c.keys[kid]; ok && time.Since(c.fetchedAt) < jwksCacheDuration {
		return certs, nil
	}

	if err := c.refresh(); err != nil {
		return nil, err
	}

	certs, ok := c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("teams: kid %q not found in JWKS", kid)
	}
	return certs, nil
}

// refresh fetches OpenID metadata and JWKS, populating the key cache.
func (c *jwksCache) refresh() error {
	// Fetch OpenID metadata.
	resp, err := c.client.Get(openIDMetadataURL)
	if err != nil {
		return fmt.Errorf("teams: fetch OpenID metadata: %w", err)
	}
	defer resp.Body.Close()

	var meta openIDConfig
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return fmt.Errorf("teams: decode OpenID metadata: %w", err)
	}
	if meta.JwksURI == "" {
		return fmt.Errorf("teams: empty jwks_uri in OpenID metadata")
	}

	// Fetch JWKS.
	jwksResp, err := c.client.Get(meta.JwksURI)
	if err != nil {
		return fmt.Errorf("teams: fetch JWKS: %w", err)
	}
	defer jwksResp.Body.Close()

	var jwks jwksResponse
	if err := json.NewDecoder(jwksResp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("teams: decode JWKS: %w", err)
	}

	keys := make(map[string][]string, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kid != "" && len(k.X5c) > 0 {
			keys[k.Kid] = k.X5c
		}
	}

	c.keys = keys
	c.fetchedAt = time.Now()
	return nil
}

// ---------------------------------------------------------------------------
// JWT validation
// ---------------------------------------------------------------------------

// jwtHeader is the decoded JWT header.
type jwtHeader struct {
	Kid string `json:"kid"`
	Alg string `json:"alg"`
}

// jwtClaims is the decoded JWT payload for Bot Framework tokens.
type jwtClaims struct {
	Aud string      `json:"aud"`
	Iss string      `json:"iss"`
	Exp json.Number `json:"exp"`
	Nbf json.Number `json:"nbf"`
	Tid string      `json:"tid"`
}

// validateJWT validates the Authorization header JWT against JWKS keys.
func (p *Plugin) validateJWT(authHeader string) error {
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == authHeader {
		return fmt.Errorf("teams: missing Bearer prefix")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("teams: invalid JWT structure")
	}

	// Decode header.
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("teams: decode JWT header: %w", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return fmt.Errorf("teams: parse JWT header: %w", err)
	}
	if header.Alg != "RS256" {
		return fmt.Errorf("teams: unsupported JWT algorithm %q", header.Alg)
	}

	// Look up signing key.
	certs, err := p.jwks.getKey(header.Kid)
	if err != nil {
		return err
	}

	// Verify signature with any matching cert.
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("teams: decode JWT signature: %w", err)
	}

	signed := []byte(parts[0] + "." + parts[1])
	hash := sha256.Sum256(signed)

	var lastErr error
	for _, certB64 := range certs {
		certDER, err := base64.StdEncoding.DecodeString(certB64)
		if err != nil {
			lastErr = err
			continue
		}
		cert, err := x509.ParseCertificate(certDER)
		if err != nil {
			lastErr = err
			continue
		}
		pub, ok := cert.PublicKey.(*rsa.PublicKey)
		if !ok {
			lastErr = fmt.Errorf("teams: certificate key is not RSA")
			continue
		}
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, hash[:], sigBytes); err == nil {
			// Signature valid -- proceed to claims validation.
			return p.validateClaims(parts[1])
		}
		lastErr = err
	}

	if lastErr != nil {
		return fmt.Errorf("teams: JWT signature verification failed: %w", lastErr)
	}
	return fmt.Errorf("teams: no valid certificate found for kid %q", header.Kid)
}

// validateClaims decodes and validates the JWT payload claims.
func (p *Plugin) validateClaims(payloadB64 string) error {
	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return fmt.Errorf("teams: decode JWT payload: %w", err)
	}

	var claims jwtClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return fmt.Errorf("teams: parse JWT claims: %w", err)
	}

	if claims.Aud != p.config.AppID {
		return fmt.Errorf("teams: invalid audience %q", claims.Aud)
	}
	if claims.Iss != botFrameworkIssuer {
		return fmt.Errorf("teams: invalid issuer %q", claims.Iss)
	}

	now := time.Now()

	exp, err := claims.Exp.Int64()
	if err != nil {
		return fmt.Errorf("teams: invalid exp claim: %w", err)
	}
	if time.Unix(exp, 0).Before(now.Add(-clockSkew)) {
		return fmt.Errorf("teams: token expired")
	}

	nbf, err := claims.Nbf.Int64()
	if err != nil {
		return fmt.Errorf("teams: invalid nbf claim: %w", err)
	}
	if time.Unix(nbf, 0).After(now.Add(clockSkew)) {
		return fmt.Errorf("teams: token not yet valid")
	}

	if p.config.TenantID != "" && claims.Tid != "" && claims.Tid != p.config.TenantID {
		return fmt.Errorf("teams: invalid tenant %q", claims.Tid)
	}

	return nil
}

// ---------------------------------------------------------------------------
// OAuth2 token acquisition
// ---------------------------------------------------------------------------

// getAccessToken returns a cached or freshly acquired OAuth2 access token.
func (p *Plugin) getAccessToken(ctx context.Context) (string, error) {
	p.tokenCache.mu.Lock()
	defer p.tokenCache.mu.Unlock()

	if p.tokenCache.token != "" && time.Now().Add(tokenExpirySkew).Before(p.tokenCache.expiresAt) {
		return p.tokenCache.token, nil
	}

	// Try botframework.com tenant first.
	token, expiresIn, err := p.requestToken(ctx, "botframework.com")
	if err != nil {
		// Fallback to configured tenant.
		if p.config.TenantID != "" {
			token, expiresIn, err = p.requestToken(ctx, p.config.TenantID)
		}
		if err != nil {
			return "", fmt.Errorf("teams: acquire access token: %w", err)
		}
	}

	p.tokenCache.token = token
	p.tokenCache.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	return token, nil
}

// requestToken performs an OAuth2 client_credentials grant for the given tenant.
func (p *Plugin) requestToken(ctx context.Context, tenant string) (string, int, error) {
	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", tenant)

	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {p.config.AppID},
		"client_secret": {p.config.AppPassword},
		"scope":         {botFrameworkScope},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", 0, fmt.Errorf("decode token response: %w", err)
	}

	return tok.AccessToken, tok.ExpiresIn, nil
}
