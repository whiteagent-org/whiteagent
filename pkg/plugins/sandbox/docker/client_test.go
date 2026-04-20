package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
)

// mockDockerServer starts an HTTP server on a temporary Unix socket.
// Uses /tmp to keep socket path short (macOS 104-byte limit).
func mockDockerServer(t *testing.T, handler http.HandlerFunc) (string, func()) {
	t.Helper()

	f, err := os.CreateTemp("/tmp", "wa-docker-*.sock")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	sock := f.Name()
	f.Close()
	os.Remove(sock)

	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}

	srv := &http.Server{Handler: handler}
	go srv.Serve(l) //nolint:errcheck

	return sock, func() {
		srv.Close()
		os.Remove(sock)
	}
}

func TestClientPing_OK(t *testing.T) {
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "OK")
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	if err := c.ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestClientPing_Unavailable(t *testing.T) {
	// Connect to a non-existent socket.
	c := newDockerClient("/tmp/nonexistent-docker-test.sock", 0)
	err := c.ping(context.Background())
	if err == nil {
		t.Fatal("expected error for unavailable docker")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected ErrDockerUnavailable, got: %v", err)
	}
}

func TestClientContainerCreate(t *testing.T) {
	var gotPath, gotQuery string
	var gotBody createContainerRequest

	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(createContainerResponse{ID: "abc123"}) //nolint:errcheck
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	id, err := c.containerCreate(context.Background(), "test-name", createContainerRequest{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "infinity"},
	})
	if err != nil {
		t.Fatalf("containerCreate: %v", err)
	}
	if id != "abc123" {
		t.Fatalf("id = %q, want abc123", id)
	}
	if gotPath != "/v1.47/containers/create" {
		t.Fatalf("path = %q, want /v1.47/containers/create", gotPath)
	}
	if !strings.Contains(gotQuery, "name=test-name") {
		t.Fatalf("query = %q, missing name param", gotQuery)
	}
	if gotBody.Image != "alpine:latest" {
		t.Fatalf("body.Image = %q, want alpine:latest", gotBody.Image)
	}
}

func TestClientContainerStart(t *testing.T) {
	var gotPath, gotMethod string
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	if err := c.containerStart(context.Background(), "cid123"); err != nil {
		t.Fatalf("containerStart: %v", err)
	}
	if gotPath != "/v1.47/containers/cid123/start" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q", gotMethod)
	}
}

func TestClientContainerStop(t *testing.T) {
	var gotPath, gotQuery string
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	if err := c.containerStop(context.Background(), "cid123", 10); err != nil {
		t.Fatalf("containerStop: %v", err)
	}
	if gotPath != "/v1.47/containers/cid123/stop" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(gotQuery, "t=10") {
		t.Fatalf("query = %q, missing t param", gotQuery)
	}
}

func TestClientContainerRemove(t *testing.T) {
	var gotPath, gotMethod, gotQuery string
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusNoContent)
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	if err := c.containerRemove(context.Background(), "cid123", true); err != nil {
		t.Fatalf("containerRemove: %v", err)
	}
	if gotPath != "/v1.47/containers/cid123" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %q", gotMethod)
	}
	if !strings.Contains(gotQuery, "force=true") {
		t.Fatalf("query = %q, missing force param", gotQuery)
	}
}

func TestClientContainerList(t *testing.T) {
	var gotPath string
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]containerListEntry{
			{ID: "c1", State: "running", Labels: map[string]string{"org.whiteagent.managed": "true"}},
		}) //nolint:errcheck
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	entries, err := c.containerList(context.Background(), "org.whiteagent.managed=true")
	if err != nil {
		t.Fatalf("containerList: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].ID != "c1" {
		t.Fatalf("entry.ID = %q", entries[0].ID)
	}
	if gotPath != "/v1.47/containers/json" {
		t.Fatalf("path = %q", gotPath)
	}
}

func TestClientContainerInspect(t *testing.T) {
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(containerInspectResponse{
			State: containerState{Status: "running"},
		}) //nolint:errcheck
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	resp, err := c.containerInspect(context.Background(), "cid123")
	if err != nil {
		t.Fatalf("containerInspect: %v", err)
	}
	if resp.State.Status != "running" {
		t.Fatalf("state = %q", resp.State.Status)
	}
}

func TestClientExecCreate(t *testing.T) {
	var gotBody createExecRequest
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(createExecResponse{ID: "exec123"}) //nolint:errcheck
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	id, err := c.execCreate(context.Background(), "cid123", createExecRequest{
		Cmd:          []string{"echo", "hello"},
		AttachStdout: true,
		AttachStderr: true,
		WorkingDir:   "/workspace",
	})
	if err != nil {
		t.Fatalf("execCreate: %v", err)
	}
	if id != "exec123" {
		t.Fatalf("id = %q", id)
	}
	if gotBody.WorkingDir != "/workspace" {
		t.Fatalf("workingDir = %q", gotBody.WorkingDir)
	}
}

func TestClientExecStart(t *testing.T) {
	// Build a multiplexed stream with stdout frame.
	frame := buildFrame(1, []byte("hello world"))

	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
		w.WriteHeader(http.StatusOK)
		w.Write(frame) //nolint:errcheck
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	stdout, stderr, err := c.execStart(context.Background(), "exec123", 1024)
	if err != nil {
		t.Fatalf("execStart: %v", err)
	}
	if stdout != "hello world" {
		t.Fatalf("stdout = %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestClientExecInspect(t *testing.T) {
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inspectExecResponse{ExitCode: 42, Running: false}) //nolint:errcheck
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	resp, err := c.execInspect(context.Background(), "exec123")
	if err != nil {
		t.Fatalf("execInspect: %v", err)
	}
	if resp.ExitCode != 42 {
		t.Fatalf("exitCode = %d", resp.ExitCode)
	}
	if resp.Running {
		t.Fatal("expected not running")
	}
}

func TestClientImagePull_OK(t *testing.T) {
	var gotPath, gotQuery string
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		// Simulate streaming progress JSON.
		fmt.Fprint(w, `{"status":"Pulling from library/alpine"}`)
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	if err := c.imagePull(context.Background(), "alpine:latest"); err != nil {
		t.Fatalf("imagePull: %v", err)
	}
	if gotPath != "/v1.47/images/create" {
		t.Fatalf("path = %q, want /v1.47/images/create", gotPath)
	}
	if !strings.Contains(gotQuery, "fromImage=alpine") {
		t.Fatalf("query = %q, missing fromImage", gotQuery)
	}
	if !strings.Contains(gotQuery, "tag=latest") {
		t.Fatalf("query = %q, missing tag", gotQuery)
	}
}

func TestClientImagePull_NotFound(t *testing.T) {
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"repository does not exist"}`)
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	err := c.imagePull(context.Background(), "nonexistent/image:v1")
	if err == nil {
		t.Fatal("expected error for missing image")
	}
	if !strings.Contains(err.Error(), "image pull failed") {
		t.Fatalf("expected pull failed error, got: %v", err)
	}
}

func TestClientImagePull_SplitsTag(t *testing.T) {
	var gotQuery string
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	if err := c.imagePull(context.Background(), "myregistry/myimage:v2"); err != nil {
		t.Fatalf("imagePull: %v", err)
	}
	if !strings.Contains(gotQuery, "fromImage=myregistry%2Fmyimage") {
		t.Fatalf("query = %q, wrong fromImage", gotQuery)
	}
	if !strings.Contains(gotQuery, "tag=v2") {
		t.Fatalf("query = %q, wrong tag", gotQuery)
	}
}

func TestClientImagePull_DefaultTag(t *testing.T) {
	var gotQuery string
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	if err := c.imagePull(context.Background(), "alpine"); err != nil {
		t.Fatalf("imagePull: %v", err)
	}
	if !strings.Contains(gotQuery, "tag=latest") {
		t.Fatalf("query = %q, expected default tag=latest", gotQuery)
	}
}

func TestClient404_ContainerNotFound(t *testing.T) {
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"No such container"}`)
	})
	defer cleanup()

	c := newDockerClient(sock, 0)

	// containerStart on 404 should return ErrContainerNotFound.
	err := c.containerStart(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected ErrContainerNotFound, got: %v", err)
	}
}

func TestClientExecStart_ReadsBody(t *testing.T) {
	// Verify exec start sends the correct JSON body.
	var gotBody []byte
	sock, cleanup := mockDockerServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	defer cleanup()

	c := newDockerClient(sock, 0)
	c.execStart(context.Background(), "exec123", 1024) //nolint:errcheck

	var req startExecRequest
	if err := json.Unmarshal(gotBody, &req); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if req.Detach {
		t.Fatal("Detach should be false")
	}
	if req.Tty {
		t.Fatal("Tty should be false")
	}
}
