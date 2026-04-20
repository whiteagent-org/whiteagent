package secret

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// testKey is a fixed 32-byte AES key for testing (64 hex chars).
const testKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func testKey(t *testing.T) []byte {
	t.Helper()
	k, err := hex.DecodeString(testKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	svc := newTestService(t)
	plaintext := []byte("hello world")
	encrypted, err := svc.encrypt(plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	decrypted, err := svc.decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Errorf("roundtrip: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptProducesUniqueNonces(t *testing.T) {
	svc := newTestService(t)
	c1, _ := svc.encrypt([]byte("same"))
	c2, _ := svc.encrypt([]byte("same"))
	if string(c1) == string(c2) {
		t.Error("two encryptions of same plaintext produced identical ciphertext")
	}
}

func TestKeyValidation(t *testing.T) {
	// Valid 32-byte key
	validKey, _ := hex.DecodeString(testKeyHex)
	_, err := NewService(&mockStore{}, validKey, "http://localhost", true)
	if err != nil {
		t.Errorf("valid key rejected: %v", err)
	}

	// Wrong length
	_, err = NewService(&mockStore{}, []byte("short"), "http://localhost", true)
	if err == nil {
		t.Error("expected error for short key")
	}

	// Nil key
	_, err = NewService(&mockStore{}, nil, "http://localhost", true)
	if err == nil {
		t.Error("expected error for nil key")
	}
}

func TestSetAndGet(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	if err := svc.Set(ctx, tid, uid, "API_KEY", []byte("my-secret"), entity.SecretModeValue); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := svc.Get(ctx, tid, uid, "API_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != "my-secret" {
		t.Errorf("Get = %q, want %q", got, "my-secret")
	}
}

func TestExists(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	exists, err := svc.Exists(ctx, tid, uid, "NOPE")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("expected false for non-existent key")
	}

	svc.Set(ctx, tid, uid, "NOPE", []byte("val"), entity.SecretModeValue)
	exists, err = svc.Exists(ctx, tid, uid, "NOPE")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("expected true after Set")
	}
}

func TestListMergesScopes(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	// Tenant-scoped
	svc.Set(ctx, tid, entity.UserID(""), "SHARED", []byte("tenant-val"), entity.SecretModeValue)
	// User-scoped with same key (should shadow)
	svc.Set(ctx, tid, uid, "SHARED", []byte("user-val"), entity.SecretModeValue)
	// User-scoped unique
	svc.Set(ctx, tid, uid, "PERSONAL", []byte("personal"), entity.SecretModeValue)

	entries, err := svc.List(ctx, tid, uid)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// Should have 2 entries (SHARED user-scoped, PERSONAL user-scoped)
	if len(entries) != 2 {
		t.Fatalf("List len = %d, want 2", len(entries))
	}

	found := map[string]entity.SecretScope{}
	for _, e := range entries {
		found[e.Key] = e.Scope
	}
	if found["SHARED"] != entity.SecretScopeUser {
		t.Errorf("SHARED scope = %q, want user (shadowed)", found["SHARED"])
	}
	if found["PERSONAL"] != entity.SecretScopeUser {
		t.Errorf("PERSONAL scope = %q, want user", found["PERSONAL"])
	}
}

func TestDeleteIdempotent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	svc.Set(ctx, tid, uid, "K", []byte("v"), entity.SecretModeValue)
	if err := svc.Delete(ctx, tid, uid, "K"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Second delete should not error
	if err := svc.Delete(ctx, tid, uid, "K"); err != nil {
		t.Fatalf("Delete idempotent: %v", err)
	}
}

func TestRedactReplacesValues(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	svc.Set(ctx, tid, uid, "TOKEN", []byte("super-secret-token"), entity.SecretModeValue)

	text := "Using token: super-secret-token in request"
	result := svc.Redact(ctx, text, tid, uid)
	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in result, got: %s", result)
	}
	if strings.Contains(result, "super-secret-token") {
		t.Error("secret value still present after redaction")
	}
}

func TestRedactSkipsShortValues(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	svc.Set(ctx, tid, uid, "SHORT", []byte("abc"), entity.SecretModeValue) // 3 chars, skip

	text := "value is abc here"
	result := svc.Redact(ctx, text, tid, uid)
	if strings.Contains(result, "[REDACTED]") {
		t.Error("should not redact values < 4 chars")
	}
	if !strings.Contains(result, "abc") {
		t.Error("short value should remain")
	}
}

func TestRedactSkipsEmptyValues(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	svc.Set(ctx, tid, uid, "EMPTY", []byte(""), entity.SecretModeValue)

	text := "some text"
	result := svc.Redact(ctx, text, tid, uid)
	if result != text {
		t.Errorf("expected unchanged text, got: %s", result)
	}
}

func TestRedactNoSecrets(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	text := "no secrets here"
	result := svc.Redact(ctx, text, entity.TenantID("t1"), entity.UserID("u1"))
	if result != text {
		t.Errorf("expected unchanged text, got: %s", result)
	}
}

func TestRequestEntryAndValidate(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	url, err := svc.RequestEntry(ctx, []string{"API_KEY"}, nil, tid, uid, entity.ConversationID("c1"), entity.ChatID("chat-1"))
	if err != nil {
		t.Fatalf("RequestEntry: %v", err)
	}
	if !strings.HasPrefix(url, "http://localhost/secrets/") {
		t.Errorf("URL = %q, want prefix http://localhost/secrets/", url)
	}

	tokenID := strings.TrimPrefix(url, "http://localhost/secrets/")
	token, err := svc.ValidateToken(ctx, tokenID)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if token == nil {
		t.Fatal("ValidateToken returned nil")
	}
	if token.Used {
		t.Error("token should not be used yet")
	}
}

func TestValidateTokenExpired(t *testing.T) {
	ms := &mockStore{}
	svc := newTestServiceWithStore(t, ms)
	ctx := context.Background()

	// Manually save an expired token
	ms.SaveSecretToken(ctx, entity.SecretToken{
		TokenID:   "expired-tok",
		Keys:      []string{"K"},
		TenantID:  entity.TenantID("t1"),
		UserID:    entity.UserID("u1"),
		ExpiresAt: time.Now().Add(-time.Hour),
		Used:      false,
	})

	_, err := svc.ValidateToken(ctx, "expired-tok")
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestValidateTokenAlreadyUsed(t *testing.T) {
	ms := &mockStore{}
	svc := newTestServiceWithStore(t, ms)
	ctx := context.Background()

	ms.SaveSecretToken(ctx, entity.SecretToken{
		TokenID:   "used-tok",
		Keys:      []string{"K"},
		TenantID:  entity.TenantID("t1"),
		UserID:    entity.UserID("u1"),
		ExpiresAt: time.Now().Add(time.Hour),
		Used:      true,
	})

	_, err := svc.ValidateToken(ctx, "used-tok")
	if err == nil {
		t.Error("expected error for used token")
	}
}

func TestConsumeTokenStoresValues(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	url, _ := svc.RequestEntry(ctx, []string{"API_KEY", "SECRET"}, nil, tid, uid, entity.ConversationID("c1"), entity.ChatID("chat-1"))
	tokenID := strings.TrimPrefix(url, "http://localhost/secrets/")

	err := svc.ConsumeToken(ctx, tokenID, map[string]SecretSubmission{
		"API_KEY": {Value: []byte("key-value"), Mode: entity.SecretModeValue},
		"SECRET":  {Value: []byte("secret-value"), Mode: entity.SecretModeValue},
	})
	if err != nil {
		t.Fatalf("ConsumeToken: %v", err)
	}

	// Verify values were encrypted and stored
	got, err := svc.Get(ctx, tid, uid, "API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "key-value" {
		t.Errorf("API_KEY = %q, want %q", got, "key-value")
	}

	got, err = svc.Get(ctx, tid, uid, "SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "secret-value" {
		t.Errorf("SECRET = %q, want %q", got, "secret-value")
	}

	// Token should be consumed
	_, err = svc.ValidateToken(ctx, tokenID)
	if err == nil {
		t.Error("expected error for consumed token")
	}
}

func TestEnvVars(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	svc.Set(ctx, tid, uid, "API_KEY", []byte("val1"), entity.SecretModeValue)
	svc.Set(ctx, tid, entity.UserID(""), "SHARED", []byte("val2"), entity.SecretModeValue)

	entries, err := svc.EnvVars(ctx, tid, uid)
	if err != nil {
		t.Fatalf("EnvVars: %v", err)
	}
	envMap := make(map[string]SecretEnvEntry, len(entries))
	for _, e := range entries {
		envMap[e.Key] = e
	}
	if envMap["API_KEY"].Value != "val1" {
		t.Errorf("API_KEY = %q, want val1", envMap["API_KEY"].Value)
	}
	if envMap["SHARED"].Value != "val2" {
		t.Errorf("SHARED = %q, want val2", envMap["SHARED"].Value)
	}
}

// ---------------------------------------------------------------------------
// File mode tests
// ---------------------------------------------------------------------------

func TestSetWithFileMode(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	if err := svc.Set(ctx, tid, uid, "CERT", []byte("-----BEGIN CERT-----\nfoo\n-----END CERT-----"), entity.SecretModeFile); err != nil {
		t.Fatalf("Set file mode: %v", err)
	}

	got, err := svc.Get(ctx, tid, uid, "CERT")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.Contains(string(got), "BEGIN CERT") {
		t.Errorf("expected cert content, got %q", got)
	}
}

func TestEnvVarsReturnsMode(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	svc.Set(ctx, tid, uid, "API_KEY", []byte("val1"), entity.SecretModeValue)
	svc.Set(ctx, tid, uid, "CERT", []byte("cert-content"), entity.SecretModeFile)

	entries, err := svc.EnvVars(ctx, tid, uid)
	if err != nil {
		t.Fatalf("EnvVars: %v", err)
	}

	envMap := make(map[string]SecretEnvEntry, len(entries))
	for _, e := range entries {
		envMap[e.Key] = e
	}

	if envMap["API_KEY"].Mode != entity.SecretModeValue {
		t.Errorf("API_KEY mode = %q, want value", envMap["API_KEY"].Mode)
	}
	if envMap["CERT"].Mode != entity.SecretModeFile {
		t.Errorf("CERT mode = %q, want file", envMap["CERT"].Mode)
	}
	if envMap["CERT"].Value != "cert-content" {
		t.Errorf("CERT value = %q, want cert-content", envMap["CERT"].Value)
	}
}

func TestConsumeTokenWithFileMode(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	url, _ := svc.RequestEntry(ctx, []string{"CERT"}, map[string]entity.SecretMode{"CERT": entity.SecretModeFile}, tid, uid, entity.ConversationID("c1"), entity.ChatID("chat-1"))
	tokenID := strings.TrimPrefix(url, "http://localhost/secrets/")

	err := svc.ConsumeToken(ctx, tokenID, map[string]SecretSubmission{
		"CERT": {Value: []byte("cert-data"), Mode: entity.SecretModeFile},
	})
	if err != nil {
		t.Fatalf("ConsumeToken: %v", err)
	}

	// Verify the secret was stored with file mode.
	entries, _ := svc.EnvVars(ctx, tid, uid)
	for _, e := range entries {
		if e.Key == "CERT" {
			if e.Mode != entity.SecretModeFile {
				t.Errorf("CERT mode = %q, want file", e.Mode)
			}
			if e.Value != "cert-data" {
				t.Errorf("CERT value = %q, want cert-data", e.Value)
			}
			return
		}
	}
	t.Error("CERT not found in EnvVars")
}

func TestListIncludesMode(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	svc.Set(ctx, tid, uid, "TOKEN", []byte("tok"), entity.SecretModeValue)
	svc.Set(ctx, tid, uid, "KEY_FILE", []byte("pem"), entity.SecretModeFile)

	entries, err := svc.List(ctx, tid, uid)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := make(map[string]entity.SecretMode)
	for _, e := range entries {
		found[e.Key] = e.Mode
	}
	if found["TOKEN"] != entity.SecretModeValue {
		t.Errorf("TOKEN mode = %q, want value", found["TOKEN"])
	}
	if found["KEY_FILE"] != entity.SecretModeFile {
		t.Errorf("KEY_FILE mode = %q, want file", found["KEY_FILE"])
	}
}

func TestSetDefaultsToValueMode(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	// Empty mode should default to value.
	svc.Set(ctx, tid, uid, "K", []byte("v"), "")

	entries, _ := svc.EnvVars(ctx, tid, uid)
	for _, e := range entries {
		if e.Key == "K" {
			if e.Mode != entity.SecretModeValue {
				t.Errorf("mode = %q, want value", e.Mode)
			}
			return
		}
	}
	t.Error("K not found")
}

func TestRequestEntryWithModeHints(t *testing.T) {
	ms := &mockStore{}
	svc := newTestServiceWithStore(t, ms)
	ctx := context.Background()
	tid := entity.TenantID("t1")
	uid := entity.UserID("u1")

	modes := map[string]entity.SecretMode{"CERT": entity.SecretModeFile}
	_, err := svc.RequestEntry(ctx, []string{"API_KEY", "CERT"}, modes, tid, uid, entity.ConversationID("c1"), entity.ChatID("chat-1"))
	if err != nil {
		t.Fatalf("RequestEntry: %v", err)
	}

	// Verify modes are stored on the token.
	if len(ms.tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(ms.tokens))
	}
	token := ms.tokens[0]
	if token.Modes["CERT"] != entity.SecretModeFile {
		t.Errorf("token mode for CERT = %q, want file", token.Modes["CERT"])
	}
}

// ---------------------------------------------------------------------------
// Test helpers: mock store
// ---------------------------------------------------------------------------

// mockStore is a minimal in-memory implementation of the secret store methods.
type mockStore struct {
	secrets []entity.Secret
	tokens  []entity.SecretToken
}

func (m *mockStore) SaveSecret(_ context.Context, _ entity.TenantID, s entity.Secret) error {
	for i, existing := range m.secrets {
		if existing.TenantID == s.TenantID && existing.UserID == s.UserID && existing.Key == s.Key {
			m.secrets[i] = s
			return nil
		}
	}
	m.secrets = append(m.secrets, s)
	return nil
}

func (m *mockStore) GetSecret(_ context.Context, tenantID entity.TenantID, userID entity.UserID, key string) (*entity.Secret, error) {
	for _, s := range m.secrets {
		if s.TenantID == tenantID && s.UserID == userID && s.Key == key {
			return &s, nil
		}
	}
	return nil, nil
}

func (m *mockStore) ListSecrets(_ context.Context, tenantID entity.TenantID, userID entity.UserID) ([]entity.Secret, error) {
	var result []entity.Secret
	for _, s := range m.secrets {
		if s.TenantID == tenantID && (s.UserID == "" || s.UserID == userID) {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockStore) DeleteSecret(_ context.Context, tenantID entity.TenantID, userID entity.UserID, key string) error {
	for i, s := range m.secrets {
		if s.TenantID == tenantID && s.UserID == userID && s.Key == key {
			m.secrets = append(m.secrets[:i], m.secrets[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *mockStore) SaveSecretToken(_ context.Context, token entity.SecretToken) error {
	m.tokens = append(m.tokens, token)
	return nil
}

func (m *mockStore) GetSecretToken(_ context.Context, tokenID string) (*entity.SecretToken, error) {
	for _, t := range m.tokens {
		if t.TokenID == tokenID {
			return &t, nil
		}
	}
	return nil, nil
}

func (m *mockStore) ConsumeSecretToken(_ context.Context, tokenID string) error {
	for i, t := range m.tokens {
		if t.TokenID == tokenID && !t.Used {
			m.tokens[i].Used = true
			return nil
		}
	}
	return fmt.Errorf("token %q not found or already consumed", tokenID)
}

// ---------------------------------------------------------------------------
// Test service constructors
// ---------------------------------------------------------------------------

func newTestService(t *testing.T) *serviceImpl {
	t.Helper()
	svc, err := NewService(&mockStore{}, testKey(t), "http://localhost", true)
	if err != nil {
		t.Fatal(err)
	}
	return svc.(*serviceImpl)
}

func newTestServiceWithStore(t *testing.T, store secretStore) *serviceImpl {
	t.Helper()
	svc, err := NewService(store, testKey(t), "http://localhost", true)
	if err != nil {
		t.Fatal(err)
	}
	return svc.(*serviceImpl)
}
