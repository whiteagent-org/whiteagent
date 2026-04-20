package docker

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const apiVersion = "v1.47"

// ---------------------------------------------------------------------------
// Request/response structs (unexported)
// ---------------------------------------------------------------------------

type createContainerRequest struct {
	Image            string                `json:"Image"`
	Cmd              []string              `json:"Cmd"`
	User             string                `json:"User,omitempty"`
	Env              []string              `json:"Env,omitempty"`
	Labels           map[string]string     `json:"Labels,omitempty"`
	HostConfig       hostConfig            `json:"HostConfig"`
	NetworkingConfig *networkingConfig     `json:"NetworkingConfig,omitempty"`
	Healthcheck      *containerHealthcheck `json:"Healthcheck,omitempty"`
}

type createContainerResponse struct {
	ID string `json:"Id"`
}

type ulimit struct {
	Name string `json:"Name"`
	Soft int64  `json:"Soft"`
	Hard int64  `json:"Hard"`
}

type hostConfig struct {
	CapDrop           []string          `json:"CapDrop,omitempty"`
	SecurityOpt       []string          `json:"SecurityOpt,omitempty"`
	ReadonlyRootfs    bool              `json:"ReadonlyRootfs,omitempty"`
	NetworkMode       string            `json:"NetworkMode,omitempty"`
	NanoCpus          int64             `json:"NanoCpus,omitempty"`
	Memory            int64             `json:"Memory,omitempty"`
	MemoryReservation int64             `json:"MemoryReservation,omitempty"`
	PidsLimit         *int64            `json:"PidsLimit,omitempty"`
	Ulimits           []ulimit          `json:"Ulimits,omitempty"`
	Tmpfs             map[string]string `json:"Tmpfs,omitempty"`
	Binds             []string          `json:"Binds,omitempty"`
	ShmSize           int64             `json:"ShmSize,omitempty"`
	Init              *bool             `json:"Init,omitempty"`
	RestartPolicy     *restartPolicy    `json:"RestartPolicy,omitempty"`
}

type createExecRequest struct {
	Cmd          []string `json:"Cmd"`
	AttachStdout bool     `json:"AttachStdout"`
	AttachStderr bool     `json:"AttachStderr"`
	Tty          bool     `json:"Tty"`
	WorkingDir   string   `json:"WorkingDir,omitempty"`
	Env          []string `json:"Env,omitempty"`
	User         string   `json:"User,omitempty"`
}

type createExecResponse struct {
	ID string `json:"Id"`
}

type startExecRequest struct {
	Detach bool `json:"Detach"`
	Tty    bool `json:"Tty"`
}

type inspectExecResponse struct {
	ExitCode int  `json:"ExitCode"`
	Running  bool `json:"Running"`
}

type endpointSettings struct {
	Aliases []string `json:"Aliases,omitempty"`
}

type networkingConfig struct {
	EndpointsConfig map[string]endpointSettings `json:"EndpointsConfig,omitempty"`
}

type containerHealthcheck struct {
	Test     []string `json:"Test"`
	Interval int64    `json:"Interval,omitempty"` // nanoseconds
	Timeout  int64    `json:"Timeout,omitempty"`  // nanoseconds
	Retries  int      `json:"Retries,omitempty"`
}

type restartPolicy struct {
	Name string `json:"Name"`
}

type createNetworkRequest struct {
	Name   string `json:"Name"`
	Driver string `json:"Driver"`
}

type createNetworkResponse struct {
	ID string `json:"Id"`
}

type containerListEntry struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

type healthState struct {
	Status string `json:"Status"` // starting, healthy, unhealthy
}

type containerState struct {
	Status string       `json:"Status"`
	Health *healthState `json:"Health,omitempty"`
}

type inspectHostConfig struct {
	Binds []string `json:"Binds"`
}

type containerInspectResponse struct {
	State      containerState    `json:"State"`
	HostConfig inspectHostConfig `json:"HostConfig"`
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

type dockerClient struct {
	http    *http.Client
	baseURL string
	rootURL string
}

// newDockerClient creates a Docker Engine HTTP client. It supports both Unix
// sockets (default) and TCP connections (for Docker-in-Docker setups).
// TCP addresses must use the "tcp://" scheme. When DOCKER_TLS_VERIFY is set,
// TLS certificates are loaded from DOCKER_CERT_PATH.
func newDockerClient(socketPath string, timeout time.Duration) *dockerClient {
	var transport http.RoundTripper
	var baseHost string

	if strings.HasPrefix(socketPath, "tcp://") || strings.HasPrefix(socketPath, "https://") {
		host := strings.TrimPrefix(strings.TrimPrefix(socketPath, "tcp://"), "https://")
		tlsCfg, err := loadDockerTLS()
		if err == nil && tlsCfg != nil {
			transport = &http.Transport{TLSClientConfig: tlsCfg}
			baseHost = "https://" + host
		} else {
			transport = &http.Transport{}
			baseHost = "http://" + host
		}
	} else {
		transport = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		}
		baseHost = "http://localhost"
	}

	c := &http.Client{Transport: transport}
	if timeout > 0 {
		c.Timeout = timeout
	}

	return &dockerClient{
		http:    c,
		baseURL: baseHost + "/" + apiVersion,
		rootURL: baseHost,
	}
}

// loadDockerTLS loads TLS certificates from the standard DOCKER_CERT_PATH
// directory. Returns nil config if DOCKER_TLS_VERIFY is not set.
func loadDockerTLS() (*tls.Config, error) {
	if os.Getenv("DOCKER_TLS_VERIFY") == "" {
		return nil, nil
	}

	certPath := os.Getenv("DOCKER_CERT_PATH")
	if certPath == "" {
		return nil, fmt.Errorf("DOCKER_TLS_VERIFY set but DOCKER_CERT_PATH is empty")
	}

	caCert, err := os.ReadFile(filepath.Join(certPath, "ca.pem"))
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caCert)

	cert, err := tls.LoadX509KeyPair(
		filepath.Join(certPath, "cert.pem"),
		filepath.Join(certPath, "key.pem"),
	)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	return &tls.Config{
		RootCAs:      pool,
		Certificates: []tls.Certificate{cert},
	}, nil
}

// ---------------------------------------------------------------------------
// Operations
// ---------------------------------------------------------------------------

func (c *dockerClient) ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.rootURL+"/_ping", nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: ping returned %d", ErrDockerUnavailable, resp.StatusCode)
	}
	return nil
}

func (c *dockerClient) containerCreate(ctx context.Context, name string, body createContainerRequest) (string, error) {
	u := c.baseURL + "/containers/create?name=" + url.QueryEscape(name)

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return "", fmt.Errorf("docker: encode create request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusCreated); err != nil {
		return "", err
	}

	var result createContainerResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("docker: decode create response: %w", err)
	}
	return result.ID, nil
}

func (c *dockerClient) containerStart(ctx context.Context, id string) error {
	u := c.baseURL + "/containers/" + url.PathEscape(id) + "/start"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	// 204 = started, 304 = already started (both OK).
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	return checkStatus(resp, http.StatusNoContent)
}

func (c *dockerClient) containerStop(ctx context.Context, id string, timeoutSec int) error {
	u := fmt.Sprintf("%s/containers/%s/stop?t=%d", c.baseURL, url.PathEscape(id), timeoutSec)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	// 204 = stopped, 304 = already stopped (both OK).
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		return nil
	}
	return checkStatus(resp, http.StatusNoContent)
}

func (c *dockerClient) containerRemove(ctx context.Context, id string, force bool) error {
	u := fmt.Sprintf("%s/containers/%s?force=%t", c.baseURL, url.PathEscape(id), force)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}
	return checkStatus(resp, http.StatusNoContent)
}

func (c *dockerClient) containerList(ctx context.Context, labelFilter string) ([]containerListEntry, error) {
	filters := fmt.Sprintf(`{"label":[%q]}`, labelFilter)
	u := c.baseURL + "/containers/json?all=true&filters=" + url.QueryEscape(filters)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusOK); err != nil {
		return nil, err
	}

	var entries []containerListEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("docker: decode container list: %w", err)
	}
	return entries, nil
}

func (c *dockerClient) containerInspect(ctx context.Context, id string) (containerInspectResponse, error) {
	u := c.baseURL + "/containers/" + url.PathEscape(id) + "/json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return containerInspectResponse{}, fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return containerInspectResponse{}, fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusOK); err != nil {
		return containerInspectResponse{}, err
	}

	var result containerInspectResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return containerInspectResponse{}, fmt.Errorf("docker: decode inspect: %w", err)
	}
	return result, nil
}

func (c *dockerClient) execCreate(ctx context.Context, containerID string, body createExecRequest) (string, error) {
	u := c.baseURL + "/containers/" + url.PathEscape(containerID) + "/exec"

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return "", fmt.Errorf("docker: encode exec request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusCreated); err != nil {
		return "", fmt.Errorf("%w: %v", ErrExecFailed, err)
	}

	var result createExecResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("docker: decode exec create response: %w", err)
	}
	return result.ID, nil
}

func (c *dockerClient) execStart(ctx context.Context, execID string, maxOutputBytes int) (string, string, error) {
	u := c.baseURL + "/exec/" + url.PathEscape(execID) + "/start"

	body := startExecRequest{Detach: false, Tty: false}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return "", "", fmt.Errorf("docker: encode exec start: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		return "", "", fmt.Errorf("%w: exec start returned %d", ErrExecFailed, resp.StatusCode)
	}

	stdout, stderr, err := demuxStream(resp.Body, maxOutputBytes)
	if err != nil {
		return "", "", fmt.Errorf("%w: read stream: %v", ErrExecFailed, err)
	}
	return stdout, stderr, nil
}

func (c *dockerClient) execInspect(ctx context.Context, execID string) (inspectExecResponse, error) {
	u := c.baseURL + "/exec/" + url.PathEscape(execID) + "/json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return inspectExecResponse{}, fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return inspectExecResponse{}, fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp, http.StatusOK); err != nil {
		return inspectExecResponse{}, err
	}

	var result inspectExecResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return inspectExecResponse{}, fmt.Errorf("docker: decode exec inspect: %w", err)
	}
	return result, nil
}

func (c *dockerClient) putArchive(ctx context.Context, containerID, destPath string, tarData io.Reader) error {
	u := fmt.Sprintf("%s/containers/%s/archive?path=%s",
		c.baseURL, url.PathEscape(containerID), url.QueryEscape(destPath))

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, tarData)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/x-tar")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return checkStatus(resp, http.StatusOK)
}

func (c *dockerClient) getArchive(ctx context.Context, containerID, srcPath string) (io.ReadCloser, error) {
	u := fmt.Sprintf("%s/containers/%s/archive?path=%s",
		c.baseURL, url.PathEscape(containerID), url.QueryEscape(srcPath))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, checkStatus(resp, http.StatusOK)
	}

	return resp.Body, nil
}

func (c *dockerClient) imagePull(ctx context.Context, image string) error {
	ref, tag := image, "latest"
	if i := strings.LastIndex(ref, ":"); i > 0 {
		ref, tag = image[:i], image[i+1:]
	}
	u := fmt.Sprintf("%s/images/create?fromImage=%s&tag=%s",
		c.baseURL, url.QueryEscape(ref), url.QueryEscape(tag))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker: image pull failed (%d): %s", resp.StatusCode, body)
	}

	// Drain the streaming JSON progress response.
	io.Copy(io.Discard, resp.Body) //nolint:errcheck
	return nil
}

// ---------------------------------------------------------------------------
// Networks
// ---------------------------------------------------------------------------

func (c *dockerClient) networkCreate(ctx context.Context, name, driver string) error {
	u := c.baseURL + "/networks/create"

	body := createNetworkRequest{Name: name, Driver: driver}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return fmt.Errorf("docker: encode network create: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	// 201 = created, 409 = already exists (both OK).
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		return nil
	}
	return fmt.Errorf("docker: network create returned %d", resp.StatusCode)
}

func (c *dockerClient) networkRemove(ctx context.Context, name string) error {
	u := c.baseURL + "/networks/" + url.PathEscape(name)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("docker: network remove returned %d", resp.StatusCode)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// checkStatus maps non-success HTTP status codes to typed errors.
func checkStatus(resp *http.Response, expected int) error {
	if resp.StatusCode == expected {
		return nil
	}

	// Drain body for error context.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))

	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrContainerNotFound, body)
	case http.StatusConflict:
		// 409 = container already started/stopped — usually benign.
		return nil
	default:
		return fmt.Errorf("docker: unexpected status %d: %s", resp.StatusCode, body)
	}
}
