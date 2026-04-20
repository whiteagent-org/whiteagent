package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mock client
// ---------------------------------------------------------------------------

type mockClient struct {
	mu sync.Mutex

	pingFn             func(ctx context.Context) error
	containerCreateFn  func(ctx context.Context, name string, body createContainerRequest) (string, error)
	containerStartFn   func(ctx context.Context, id string) error
	containerStopFn    func(ctx context.Context, id string, timeoutSec int) error
	containerRemoveFn  func(ctx context.Context, id string, force bool) error
	containerListFn    func(ctx context.Context, labelFilter string) ([]containerListEntry, error)
	containerInspectFn func(ctx context.Context, id string) (containerInspectResponse, error)
	execCreateFn       func(ctx context.Context, containerID string, body createExecRequest) (string, error)
	execStartFn        func(ctx context.Context, execID string, maxOutputBytes int) (string, string, error)
	execInspectFn      func(ctx context.Context, execID string) (inspectExecResponse, error)
	putArchiveFn       func(ctx context.Context, containerID, destPath string, tarData io.Reader) error
	getArchiveFn       func(ctx context.Context, containerID, srcPath string) (io.ReadCloser, error)

	// tracking
	createdRequests []createContainerRequest
	createdNames    []string
	startedIDs      []string
	stoppedIDs      []string
	removedIDs      []string
}

func (m *mockClient) ping(ctx context.Context) error {
	if m.pingFn != nil {
		return m.pingFn(ctx)
	}
	return nil
}

func (m *mockClient) containerCreate(ctx context.Context, name string, body createContainerRequest) (string, error) {
	m.mu.Lock()
	m.createdRequests = append(m.createdRequests, body)
	m.createdNames = append(m.createdNames, name)
	m.mu.Unlock()
	if m.containerCreateFn != nil {
		return m.containerCreateFn(ctx, name, body)
	}
	return "ctr-" + name, nil
}

func (m *mockClient) containerStart(ctx context.Context, id string) error {
	m.mu.Lock()
	m.startedIDs = append(m.startedIDs, id)
	m.mu.Unlock()
	if m.containerStartFn != nil {
		return m.containerStartFn(ctx, id)
	}
	return nil
}

func (m *mockClient) containerStop(ctx context.Context, id string, timeoutSec int) error {
	m.mu.Lock()
	m.stoppedIDs = append(m.stoppedIDs, id)
	m.mu.Unlock()
	if m.containerStopFn != nil {
		return m.containerStopFn(ctx, id, timeoutSec)
	}
	return nil
}

func (m *mockClient) containerRemove(ctx context.Context, id string, force bool) error {
	m.mu.Lock()
	m.removedIDs = append(m.removedIDs, id)
	m.mu.Unlock()
	if m.containerRemoveFn != nil {
		return m.containerRemoveFn(ctx, id, force)
	}
	return nil
}

func (m *mockClient) containerList(ctx context.Context, labelFilter string) ([]containerListEntry, error) {
	if m.containerListFn != nil {
		return m.containerListFn(ctx, labelFilter)
	}
	return nil, nil
}

func (m *mockClient) containerInspect(ctx context.Context, id string) (containerInspectResponse, error) {
	if m.containerInspectFn != nil {
		return m.containerInspectFn(ctx, id)
	}
	return containerInspectResponse{State: containerState{Status: "running"}}, nil
}

func (m *mockClient) execCreate(ctx context.Context, containerID string, body createExecRequest) (string, error) {
	if m.execCreateFn != nil {
		return m.execCreateFn(ctx, containerID, body)
	}
	return "exec-1", nil
}

func (m *mockClient) execStart(ctx context.Context, execID string, maxOutputBytes int) (string, string, error) {
	if m.execStartFn != nil {
		return m.execStartFn(ctx, execID, maxOutputBytes)
	}
	return "hello\n", "", nil
}

func (m *mockClient) execInspect(ctx context.Context, execID string) (inspectExecResponse, error) {
	if m.execInspectFn != nil {
		return m.execInspectFn(ctx, execID)
	}
	return inspectExecResponse{ExitCode: 0, Running: false}, nil
}

func (m *mockClient) putArchive(ctx context.Context, containerID, destPath string, tarData io.Reader) error {
	if m.putArchiveFn != nil {
		return m.putArchiveFn(ctx, containerID, destPath, tarData)
	}
	return nil
}

func (m *mockClient) getArchive(ctx context.Context, containerID, srcPath string) (io.ReadCloser, error) {
	if m.getArchiveFn != nil {
		return m.getArchiveFn(ctx, containerID, srcPath)
	}
	return io.NopCloser(io.LimitReader(nil, 0)), nil
}

func (m *mockClient) imagePull(ctx context.Context, image string) error {
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func boolPtr(b bool) *bool { return &b }

func testConfig() *dockerConfig {
	return &dockerConfig{
		SocketPath:   "/var/run/docker.sock",
		Image:        "alpine:latest",
		IdleTimeout:  "15m",
		ExecTimeout:  "5m",
		StopTimeout:  "10s",
		MaxOutputMB:  1,
		AllowNetwork: boolPtr(true),
		ContainerUID: 1000,
		ContainerGID: 1000,
		Resources: resourceConfig{
			CPUCores:  0.5,
			MemoryMB:  256,
			PidsLimit: 100,
			TmpfsMB:   64,
		},
	}
}

func testPool(m *mockClient) *containerPool {
	return newPool(m, testConfig(), "")
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPoolEnsureCreatesContainer(t *testing.T) {
	m := &mockClient{}
	p := testPool(m)
	ctx := context.Background()
	uid := entity.UserID("user-12345678-abcd")
	tid := entity.TenantID("tenant-1")

	workDir, err := p.ensure(ctx, uid, tid, nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if workDir != "/workspace" {
		t.Fatalf("workDir = %q, want /workspace", workDir)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.createdRequests) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(m.createdRequests))
	}
	if len(m.startedIDs) != 1 {
		t.Fatalf("expected 1 start call, got %d", len(m.startedIDs))
	}

	// Verify name uses first 8 chars of user ID.
	if m.createdNames[0] != "whiteagent-user-123" {
		t.Fatalf("name = %q, want whiteagent-user-123", m.createdNames[0])
	}
}

func TestPoolEnsureHardenedDefaults(t *testing.T) {
	m := &mockClient{}
	p := testPool(m)
	ctx := context.Background()

	_, err := p.ensure(ctx, "user-abcdefgh", "tenant-1", nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	req := m.createdRequests[0]

	if req.User != "1000:1000" {
		t.Errorf("User = %q, want 1000:1000", req.User)
	}
	if len(req.HostConfig.CapDrop) != 1 || req.HostConfig.CapDrop[0] != "ALL" {
		t.Errorf("CapDrop = %v, want [ALL]", req.HostConfig.CapDrop)
	}
	if len(req.HostConfig.SecurityOpt) != 1 || req.HostConfig.SecurityOpt[0] != "no-new-privileges" {
		t.Errorf("SecurityOpt = %v, want [no-new-privileges]", req.HostConfig.SecurityOpt)
	}
	if !req.HostConfig.ReadonlyRootfs {
		t.Error("ReadonlyRootfs = false, want true")
	}
	if req.HostConfig.Tmpfs["/tmp"] == "" || req.HostConfig.Tmpfs["/var/tmp"] == "" || req.HostConfig.Tmpfs["/message"] == "" {
		t.Errorf("Tmpfs = %v, want /tmp, /var/tmp, and /message entries", req.HostConfig.Tmpfs)
	}
	if msgOpt := req.HostConfig.Tmpfs["/message"]; !strings.Contains(msgOpt, "uid=1000") || !strings.Contains(msgOpt, "mode=0700") {
		t.Errorf("Tmpfs[/message] = %q, want uid=1000,mode=0700", msgOpt)
	}
	if len(req.HostConfig.Ulimits) != 2 {
		t.Errorf("Ulimits count = %d, want 2", len(req.HostConfig.Ulimits))
	} else {
		nofile := req.HostConfig.Ulimits[0]
		if nofile.Name != "nofile" || nofile.Soft != 1024 || nofile.Hard != 2048 {
			t.Errorf("Ulimits[nofile] = %+v, want {nofile 1024 2048}", nofile)
		}
		nproc := req.HostConfig.Ulimits[1]
		if nproc.Name != "nproc" || nproc.Soft != 100 || nproc.Hard != 100 {
			t.Errorf("Ulimits[nproc] = %+v, want {nproc 100 100}", nproc)
		}
	}
	if req.HostConfig.NanoCpus != 500000000 {
		t.Errorf("NanoCpus = %d, want 500000000", req.HostConfig.NanoCpus)
	}
	if req.HostConfig.Memory != 256*1024*1024 {
		t.Errorf("Memory = %d, want %d", req.HostConfig.Memory, 256*1024*1024)
	}
	if req.HostConfig.PidsLimit == nil || *req.HostConfig.PidsLimit != 100 {
		t.Errorf("PidsLimit = %v, want 100", req.HostConfig.PidsLimit)
	}
	if req.Cmd[0] != "sleep" || req.Cmd[1] != "infinity" {
		t.Errorf("Cmd = %v, want [sleep infinity]", req.Cmd)
	}
	if req.Labels["org.whiteagent.managed"] != "true" {
		t.Errorf("missing managed label")
	}
	if req.Labels["org.whiteagent.user-id"] != "user-abcdefgh" {
		t.Errorf("user-id label = %q", req.Labels["org.whiteagent.user-id"])
	}
}

func TestPoolEnsureExistingRunning(t *testing.T) {
	m := &mockClient{}
	p := testPool(m)
	ctx := context.Background()
	uid := entity.UserID("user-existing1")
	tid := entity.TenantID("t1")

	// First call creates.
	if _, err := p.ensure(ctx, uid, tid, nil); err != nil {
		t.Fatalf("ensure 1: %v", err)
	}

	// Second call should not create a new container.
	if _, err := p.ensure(ctx, uid, tid, nil); err != nil {
		t.Fatalf("ensure 2: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.createdRequests) != 1 {
		t.Fatalf("expected 1 create call for reuse, got %d", len(m.createdRequests))
	}
}

func TestPoolEnsureStoppedContainerRecreates(t *testing.T) {
	m := &mockClient{}
	m.containerInspectFn = func(ctx context.Context, id string) (containerInspectResponse, error) {
		return containerInspectResponse{State: containerState{Status: "exited"}}, nil
	}

	p := testPool(m)
	ctx := context.Background()
	uid := entity.UserID("user-stopped1")
	tid := entity.TenantID("t1")

	// Pre-populate the pool to simulate existing stopped container.
	info := &containerInfo{containerID: "old-ctr"}
	p.containers.Store(uid, info)

	workDir, err := p.ensure(ctx, uid, tid, nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if workDir != "/workspace" {
		t.Fatalf("workDir = %q", workDir)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Old container should have been force-removed.
	if len(m.removedIDs) < 1 {
		t.Fatal("expected old container to be removed")
	}
	// New container should have been created.
	if len(m.createdRequests) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(m.createdRequests))
	}
}

func TestPoolExec(t *testing.T) {
	m := &mockClient{}
	p := testPool(m)
	ctx := context.Background()
	uid := entity.UserID("user-exec0001")
	tid := entity.TenantID("t1")

	if _, err := p.ensure(ctx, uid, tid, nil); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	result, err := p.exec(ctx, uid, port.ExecRequest{
		Command: "echo",
		Args:    []string{"hello"},
		WorkDir: "/workspace",
	})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("stdout = %q, want hello\\n", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
}

func TestPoolExecTimeout(t *testing.T) {
	m := &mockClient{}
	m.execStartFn = func(ctx context.Context, execID string, maxOutputBytes int) (string, string, error) {
		<-ctx.Done()
		return "", "", ctx.Err()
	}

	cfg := testConfig()
	cfg.ExecTimeout = "50ms"
	p := newPool(m, cfg, "")
	ctx := context.Background()
	uid := entity.UserID("user-timeout1")
	tid := entity.TenantID("t1")

	if _, err := p.ensure(ctx, uid, tid, nil); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	_, err := p.exec(ctx, uid, port.ExecRequest{Command: "sleep", Args: []string{"10"}})
	if !errors.Is(err, ErrExecTimeout) {
		t.Fatalf("expected ErrExecTimeout, got %v", err)
	}
}

func TestPoolExecNotFound(t *testing.T) {
	m := &mockClient{}
	p := testPool(m)
	ctx := context.Background()

	_, err := p.exec(ctx, "no-such-user", port.ExecRequest{Command: "echo"})
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
}

func TestPoolRelease(t *testing.T) {
	m := &mockClient{}
	p := testPool(m)
	ctx := context.Background()
	uid := entity.UserID("user-release1")
	tid := entity.TenantID("t1")

	if _, err := p.ensure(ctx, uid, tid, nil); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	if err := p.release(ctx, uid); err != nil {
		t.Fatalf("release: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.stoppedIDs) != 1 {
		t.Fatalf("expected 1 stop call, got %d", len(m.stoppedIDs))
	}
	if len(m.removedIDs) != 1 {
		t.Fatalf("expected 1 remove call, got %d", len(m.removedIDs))
	}

	// Second release should be idempotent.
	if err := p.release(ctx, uid); err != nil {
		t.Fatalf("release idempotent: %v", err)
	}
}

func TestPoolReleaseUnknownUser(t *testing.T) {
	m := &mockClient{}
	p := testPool(m)

	if err := p.release(context.Background(), "unknown"); err != nil {
		t.Fatalf("release unknown: %v", err)
	}
}

func TestPoolSweepSkipsActive(t *testing.T) {
	m := &mockClient{}
	p := testPool(m)

	info := &containerInfo{containerID: "ctr-active"}
	info.activeExecs.Store(1)
	info.lastUsed.Store(0) // very old
	p.containers.Store(entity.UserID("user-active"), info)

	p.sweepOnce()

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.stoppedIDs) != 0 {
		t.Fatal("sweep should not stop container with active execs")
	}
}

func TestPoolSweepRemovesIdle(t *testing.T) {
	m := &mockClient{}
	cfg := testConfig()
	cfg.IdleTimeout = "1ms"
	p := newPool(m, cfg, "")

	info := &containerInfo{containerID: "ctr-idle"}
	info.lastUsed.Store(time.Now().Add(-time.Hour).Unix())
	p.containers.Store(entity.UserID("user-idle"), info)

	p.sweepOnce()

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.stoppedIDs) != 1 {
		t.Fatalf("sweep should stop idle container, stopped=%d", len(m.stoppedIDs))
	}
}

func TestPoolCleanOrphans(t *testing.T) {
	m := &mockClient{}
	m.containerListFn = func(ctx context.Context, labelFilter string) ([]containerListEntry, error) {
		return []containerListEntry{
			{ID: "orphan-1", Labels: map[string]string{"org.whiteagent.managed": "true"}},
			{ID: "orphan-2", Labels: map[string]string{"org.whiteagent.managed": "true"}},
		}, nil
	}
	p := testPool(m)

	p.cleanOrphans(context.Background())

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.stoppedIDs) != 2 {
		t.Fatalf("expected 2 stopped orphans, got %d", len(m.stoppedIDs))
	}
	if len(m.removedIDs) != 2 {
		t.Fatalf("expected 2 removed orphans, got %d", len(m.removedIDs))
	}
}

func TestPoolRebuildFromLabels(t *testing.T) {
	m := &mockClient{}
	m.containerListFn = func(ctx context.Context, labelFilter string) ([]containerListEntry, error) {
		return []containerListEntry{
			{
				ID:     "ctr-rebuild",
				State:  "running",
				Labels: map[string]string{"org.whiteagent.user-id": "user-abc"},
			},
		}, nil
	}
	p := testPool(m)

	p.rebuildFromLabels(context.Background())

	val, ok := p.containers.Load(entity.UserID("user-abc"))
	if !ok {
		t.Fatal("expected user-abc in pool after rebuild")
	}
	ci := val.(*containerInfo)
	if ci.containerID != "ctr-rebuild" {
		t.Fatalf("containerID = %q, want ctr-rebuild", ci.containerID)
	}
}

func TestPoolStopAll(t *testing.T) {
	m := &mockClient{}
	p := testPool(m)
	ctx := context.Background()

	// Add two containers.
	info1 := &containerInfo{containerID: "ctr-1"}
	info2 := &containerInfo{containerID: "ctr-2"}
	p.containers.Store(entity.UserID("u1"), info1)
	p.containers.Store(entity.UserID("u2"), info2)

	p.stopAll(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.stoppedIDs) != 2 {
		t.Fatalf("expected 2 stopped, got %d", len(m.stoppedIDs))
	}
	if len(m.removedIDs) != 2 {
		t.Fatalf("expected 2 removed, got %d", len(m.removedIDs))
	}
}

func TestPoolEnsureWithBindMounts(t *testing.T) {
	m := &mockClient{}
	p := testPool(m)
	ctx := context.Background()
	uid := entity.UserID("user-mounts01")
	tid := entity.TenantID("t1")

	mounts := []port.Mount{
		{Source: "/host/home", Target: "/home/whiteagent", ReadOnly: false},
		{Source: "/host/tenant", Target: "/tenant", ReadOnly: true},
	}

	_, err := p.ensure(ctx, uid, tid, mounts)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.createdRequests) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(m.createdRequests))
	}
	binds := m.createdRequests[0].HostConfig.Binds
	if len(binds) != 2 {
		t.Fatalf("expected 2 Binds, got %d: %v", len(binds), binds)
	}
	if binds[0] != "/host/home:/home/whiteagent" {
		t.Errorf("Binds[0] = %q, want /host/home:/home/whiteagent", binds[0])
	}
	if binds[1] != "/host/tenant:/tenant:ro" {
		t.Errorf("Binds[1] = %q, want /host/tenant:/tenant:ro", binds[1])
	}
}

func TestPoolEnsureMountMismatchRecreates(t *testing.T) {
	inspectCount := 0
	m := &mockClient{}
	m.containerInspectFn = func(ctx context.Context, id string) (containerInspectResponse, error) {
		inspectCount++
		return containerInspectResponse{
			State:      containerState{Status: "running"},
			HostConfig: inspectHostConfig{Binds: []string{"/old/path:/home/whiteagent"}},
		}, nil
	}

	p := testPool(m)
	ctx := context.Background()
	uid := entity.UserID("user-remount1")
	tid := entity.TenantID("t1")

	// Pre-populate the pool to simulate existing container with old mounts.
	info := &containerInfo{containerID: "old-ctr"}
	p.containers.Store(uid, info)

	newMounts := []port.Mount{
		{Source: "/new/home", Target: "/home/whiteagent", ReadOnly: false},
	}

	_, err := p.ensure(ctx, uid, tid, newMounts)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Old container should have been removed.
	if len(m.removedIDs) < 1 {
		t.Fatal("expected old container to be removed")
	}
	// New container should have been created with new mounts.
	if len(m.createdRequests) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(m.createdRequests))
	}
	binds := m.createdRequests[0].HostConfig.Binds
	if len(binds) != 1 || binds[0] != "/new/home:/home/whiteagent" {
		t.Errorf("Binds = %v, want [/new/home:/home/whiteagent]", binds)
	}
}

func TestPoolEnsureNetworkModeHost(t *testing.T) {
	m := &mockClient{}
	cfg := testConfig()
	cfg.NetworkMode = "host"
	p := newPool(m, cfg, "")

	_, err := p.ensure(context.Background(), "user-hostnet1", "t1", nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createdRequests[0].HostConfig.NetworkMode != "host" {
		t.Errorf("NetworkMode = %q, want host", m.createdRequests[0].HostConfig.NetworkMode)
	}
}

func TestPoolEnsureNetworkModeOverridesAllowNetwork(t *testing.T) {
	m := &mockClient{}
	cfg := testConfig()
	cfg.NetworkMode = "host"
	cfg.AllowNetwork = boolPtr(false)
	p := newPool(m, cfg, "")

	_, err := p.ensure(context.Background(), "user-override1", "t1", nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createdRequests[0].HostConfig.NetworkMode != "host" {
		t.Errorf("NetworkMode = %q, want host (should override allow_network=false)", m.createdRequests[0].HostConfig.NetworkMode)
	}
}

func TestPoolEnsureNetworkNone(t *testing.T) {
	m := &mockClient{}
	cfg := testConfig()
	cfg.AllowNetwork = boolPtr(false)
	p := newPool(m, cfg, "")

	_, err := p.ensure(context.Background(), "user-nonet01", "t1", nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createdRequests[0].HostConfig.NetworkMode != "none" {
		t.Errorf("NetworkMode = %q, want none", m.createdRequests[0].HostConfig.NetworkMode)
	}
}

func TestConfigAllowNetworkFalsePreserved(t *testing.T) {
	raw := `{"allow_network": false}`
	var cfg dockerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	cfg.applyDefaults()
	if cfg.AllowNetwork == nil || *cfg.AllowNetwork != false {
		t.Errorf("AllowNetwork = %v, want false (preserved after applyDefaults)", cfg.AllowNetwork)
	}
}

func TestConfigAllowNetworkDefaultTrue(t *testing.T) {
	var cfg dockerConfig
	cfg.applyDefaults()
	if cfg.AllowNetwork == nil || *cfg.AllowNetwork != true {
		t.Errorf("AllowNetwork = %v, want true (default)", cfg.AllowNetwork)
	}
}

func TestPoolEnsureCustomUID(t *testing.T) {
	m := &mockClient{}
	cfg := testConfig()
	cfg.ContainerUID = 2000
	cfg.ContainerGID = 2000
	p := newPool(m, cfg, "")

	_, err := p.ensure(context.Background(), "user-customuid", "t1", nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	req := m.createdRequests[0]
	if req.User != "2000:2000" {
		t.Errorf("User = %q, want 2000:2000", req.User)
	}
	if msgOpt := req.HostConfig.Tmpfs["/message"]; !strings.Contains(msgOpt, "uid=2000") {
		t.Errorf("Tmpfs[/message] = %q, want uid=2000", msgOpt)
	}
}

func TestValidateMounts(t *testing.T) {
	tests := []struct {
		name    string
		mounts  []port.Mount
		wantErr bool
	}{
		{
			name: "valid absolute paths",
			mounts: []port.Mount{
				{Source: "/host/data", Target: "/container/data"},
			},
		},
		{
			name: "relative source",
			mounts: []port.Mount{
				{Source: "relative/path", Target: "/container/data"},
			},
			wantErr: true,
		},
		{
			name: "relative target",
			mounts: []port.Mount{
				{Source: "/host/data", Target: "relative/path"},
			},
			wantErr: true,
		},
		{
			name:   "empty mounts",
			mounts: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMounts(tt.mounts)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateMounts() err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPoolEnsureWithNetwork(t *testing.T) {
	m := &mockClient{}
	cfg := testConfig()
	cfg.Network = "sandbox"
	cfg.NetworkMode = "" // ensure network takes precedence
	p := newPool(m, cfg, "")

	_, err := p.ensure(context.Background(), "user-net001", "t1", nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	req := m.createdRequests[0]

	if req.HostConfig.NetworkMode != "sandbox" {
		t.Errorf("NetworkMode = %q, want sandbox", req.HostConfig.NetworkMode)
	}
	if req.NetworkingConfig == nil {
		t.Fatal("NetworkingConfig should not be nil")
	}
	if _, ok := req.NetworkingConfig.EndpointsConfig["sandbox"]; !ok {
		t.Error("expected endpoint config for 'sandbox' network")
	}
}

func TestPoolEnsureWithoutNetwork(t *testing.T) {
	m := &mockClient{}
	cfg := testConfig()
	p := newPool(m, cfg, "")

	_, err := p.ensure(context.Background(), "user-nonet02", "t1", nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	req := m.createdRequests[0]

	if req.HostConfig.NetworkMode != "bridge" {
		t.Errorf("NetworkMode = %q, want bridge", req.HostConfig.NetworkMode)
	}
	if req.NetworkingConfig != nil {
		t.Error("NetworkingConfig should be nil when network is not set")
	}
}
