package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

func TestPluginManifest(t *testing.T) {
	m := Manifest()
	if m.Kind != entity.PluginKindSandbox {
		t.Fatalf("Kind = %q, want sandbox", m.Kind)
	}
}

func TestPluginNewPlugin(t *testing.T) {
	p := NewPlugin()
	if p == nil {
		t.Fatal("NewPlugin returned nil")
	}
	if _, ok := p.(port.SandboxPlugin); !ok {
		t.Fatal("NewPlugin does not implement SandboxPlugin")
	}
}

func TestPluginInitValid(t *testing.T) {
	p := NewPlugin()
	cfg := map[string]any{
		"socket_path":   "/var/run/docker.sock",
		"image":         "alpine:latest",
		"idle_timeout":  "10m",
		"exec_timeout":  "3m",
		"stop_timeout":  "5s",
		"max_output_mb": 2,
		"resources": map[string]any{
			"cpu_cores":  0.5,
			"memory_mb":  128,
			"pids_limit": 50,
			"tmpfs_mb":   32,
		},
	}
	raw, _ := json.Marshal(cfg)

	err := p.Init(context.Background(), "test-docker", raw)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if p.ID() != "test-docker" {
		t.Errorf("ID = %q", p.ID())
	}
	if p.Kind() != entity.PluginKindSandbox {
		t.Errorf("Kind = %q", p.Kind())
	}
	if p.Status() != entity.PluginStateUnhealthy {
		t.Errorf("Status = %q, want unhealthy (not yet started)", p.Status())
	}
}

func TestPluginInitDefaults(t *testing.T) {
	p := NewPlugin()
	err := p.Init(context.Background(), "test-default", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Init with defaults: %v", err)
	}
}

func TestPluginInitInvalid(t *testing.T) {
	p := NewPlugin()
	err := p.Init(context.Background(), "bad", json.RawMessage(`{"idle_timeout":"nope"}`))
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestPluginStatusDegradedRecovery(t *testing.T) {
	ds := &dockerSandbox{}
	ds.state.Store(entity.PluginStateHealthy)

	// Simulate Docker error causing degradation.
	m := &mockClient{}
	m.containerInspectFn = func(ctx context.Context, id string) (containerInspectResponse, error) {
		return containerInspectResponse{}, errors.New("docker: daemon unavailable: connection refused")
	}

	ds.cfg = *testConfig()
	ds.client = m
	ds.pool = newPool(m, &ds.cfg, "")

	// Ensure with a user that doesn't have a container yet -- should succeed (create new).
	// But let's test degradation: add a broken entry to trigger inspect failure.
	info := &containerInfo{containerID: "broken-ctr"}
	ds.pool.containers.Store(entity.UserID("user-degrade"), info)

	// containerCreate should work but inspect fails on existing -> removes old -> creates new.
	_, err := ds.Ensure(context.Background(), entity.UserID("user-degrade"))
	if err != nil {
		t.Logf("Ensure error (expected path): %v", err)
	}

	// Now test a clean ensure (new user) that succeeds.
	ds.state.Store(entity.PluginStateDegraded) // manually set degraded
	m.containerInspectFn = nil                 // restore healthy mock

	_, err = ds.Ensure(context.Background(), entity.UserID("user-recov1"))
	if err != nil {
		t.Fatalf("Ensure recovery: %v", err)
	}
	if ds.Status() != entity.PluginStateHealthy {
		t.Errorf("Status = %q after recovery, want healthy", ds.Status())
	}
}

func TestDockerSandboxUserHomePath(t *testing.T) {
	ds := &dockerSandbox{}
	ds.cfg.applyDefaults()

	got := ds.UserHomePath("u1")
	if got != "/home/whiteagent" {
		t.Errorf("UserHomePath = %q, want /home/whiteagent", got)
	}
}

func TestDockerSandboxTenantHomePath(t *testing.T) {
	ds := &dockerSandbox{}
	ds.cfg.applyDefaults()

	got := ds.TenantHomePath("t1")
	if got != "/tenant" {
		t.Errorf("TenantHomePath = %q, want /tenant", got)
	}
}

func TestDockerSandboxMessagesPath(t *testing.T) {
	ds := &dockerSandbox{}
	ds.cfg.applyDefaults()

	got := ds.MessagesPath()
	if got != "/messages" {
		t.Errorf("MessagesPath = %q, want /messages", got)
	}
}

func TestDockerSandboxCustomMountPaths(t *testing.T) {
	ds := &dockerSandbox{}
	ds.cfg.UserHomeMount = "/custom/home"
	ds.cfg.TenantHomeMount = "/custom/tenant"
	ds.cfg.MessagesMount = "/custom/messages"

	if got := ds.UserHomePath("u1"); got != "/custom/home" {
		t.Errorf("UserHomePath = %q, want /custom/home", got)
	}
	if got := ds.TenantHomePath("t1"); got != "/custom/tenant" {
		t.Errorf("TenantHomePath = %q, want /custom/tenant", got)
	}
	if got := ds.MessagesPath(); got != "/custom/messages" {
		t.Errorf("MessagesPath = %q, want /custom/messages", got)
	}
}

func TestStartRetriesPingUntilDaemonReady(t *testing.T) {
	var attempts atomic.Int32
	const failCount = 3

	m := &mockClient{}
	m.pingFn = func(ctx context.Context) error {
		n := attempts.Add(1)
		if int(n) <= failCount {
			return fmt.Errorf("%w: connection refused", ErrDockerUnavailable)
		}
		return nil
	}

	ds := &dockerSandbox{}
	ds.cfg = *testConfig()
	ds.client = m
	ds.state.Store(entity.PluginStateUnhealthy)

	if err := ds.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := int(attempts.Load()); got != failCount+1 {
		t.Errorf("ping attempts = %d, want %d", got, failCount+1)
	}
	if ds.Status() != entity.PluginStateHealthy {
		t.Errorf("Status = %q, want healthy", ds.Status())
	}
}

func TestStartPingNonDockerErrorNoRetry(t *testing.T) {
	var attempts atomic.Int32
	m := &mockClient{}
	m.pingFn = func(ctx context.Context) error {
		attempts.Add(1)
		return errors.New("unexpected error")
	}

	ds := &dockerSandbox{}
	ds.cfg = *testConfig()
	ds.client = m
	ds.state.Store(entity.PluginStateUnhealthy)

	err := ds.Start(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if got := int(attempts.Load()); got != 1 {
		t.Errorf("ping attempts = %d, want 1 (no retry for non-docker errors)", got)
	}
}

func TestStartPingRespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	m := &mockClient{}
	m.pingFn = func(_ context.Context) error {
		cancel() // cancel after first failed ping
		return fmt.Errorf("%w: connection refused", ErrDockerUnavailable)
	}

	ds := &dockerSandbox{}
	ds.cfg = *testConfig()
	ds.client = m
	ds.state.Store(entity.PluginStateUnhealthy)

	err := ds.Start(ctx)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestPluginStopWithoutStart(t *testing.T) {
	ds := &dockerSandbox{}
	ds.state.Store(entity.PluginStateUnhealthy)

	// Stop before Start should not panic.
	if err := ds.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
