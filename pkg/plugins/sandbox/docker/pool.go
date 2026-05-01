package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Client interface (for testing)
// ---------------------------------------------------------------------------

// poolClient is a narrow interface over the Docker client methods used by the
// container pool. The concrete *dockerClient satisfies it; tests use a mock.
type poolClient interface {
	ping(ctx context.Context) error
	containerCreate(ctx context.Context, name string, body createContainerRequest) (string, error)
	containerStart(ctx context.Context, id string) error
	containerStop(ctx context.Context, id string, timeoutSec int) error
	containerRemove(ctx context.Context, id string, force bool) error
	containerList(ctx context.Context, labelFilter string) ([]containerListEntry, error)
	containerInspect(ctx context.Context, id string) (containerInspectResponse, error)
	execCreate(ctx context.Context, containerID string, body createExecRequest) (string, error)
	execStart(ctx context.Context, execID string, maxOutputBytes int) (string, string, error)
	execInspect(ctx context.Context, execID string) (inspectExecResponse, error)
	putArchive(ctx context.Context, containerID, destPath string, tarData io.Reader) error
	getArchive(ctx context.Context, containerID, srcPath string) (io.ReadCloser, error)
	imagePull(ctx context.Context, image string) error
}

// ---------------------------------------------------------------------------
// Container info
// ---------------------------------------------------------------------------

type containerInfo struct {
	containerID string
	lastUsed    atomic.Int64
	activeExecs atomic.Int32
}

// ---------------------------------------------------------------------------
// Container pool
// ---------------------------------------------------------------------------

type containerPool struct {
	client     poolClient
	cfg        *dockerConfig
	containers sync.Map // entity.UserID -> *containerInfo

	// Parsed durations (computed once at construction).
	idleTimeout time.Duration
	execTimeout time.Duration
	stopTimeout time.Duration

	// Bare-metal path translation: replace containerDataDir prefix with hostDataDir
	// in bind mount source paths. Empty hostDataDir means no translation (DinD mode).
	containerDataDir string
	hostDataDir      string

	sweepDone chan struct{}
}

// newPool creates a container pool. The config must already be validated.
// containerDataDir is the container-side absolute data directory (from ScopedFS).
func newPool(client poolClient, cfg *dockerConfig, containerDataDir string) *containerPool {
	idle, _ := time.ParseDuration(cfg.IdleTimeout)
	exec, _ := time.ParseDuration(cfg.ExecTimeout)
	stop, _ := time.ParseDuration(cfg.StopTimeout)

	return &containerPool{
		client:           client,
		cfg:              cfg,
		containerDataDir: containerDataDir,
		hostDataDir:      cfg.HostDataDir,
		idleTimeout:      idle,
		execTimeout:      exec,
		stopTimeout:      stop,
		sweepDone:        make(chan struct{}),
	}
}

// ---------------------------------------------------------------------------
// Ensure
// ---------------------------------------------------------------------------

const labelManaged = "org.whiteagent.managed"
const labelUserID = "org.whiteagent.user-id"
const labelTenantID = "org.whiteagent.tenant-id"

// mountsToBinds converts port.Mount slice to Docker Binds format strings.
func mountsToBinds(mounts []port.Mount) []string {
	binds := make([]string, 0, len(mounts))
	for _, m := range mounts {
		b := m.Source + ":" + m.Target
		if m.ReadOnly {
			b += ":ro"
		}
		binds = append(binds, b)
	}
	return binds
}

// bindsMatch returns true if the actual binds match the expected binds
// (order-independent comparison).
func bindsMatch(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	a := make([]string, len(actual))
	copy(a, actual)
	sort.Strings(a)
	e := make([]string, len(expected))
	copy(e, expected)
	sort.Strings(e)
	return strings.Join(a, "\n") == strings.Join(e, "\n")
}

// translateMountPaths rewrites mount source paths when whiteagent shares the
// host Docker daemon. When hostDataDir is set, container-side data paths are
// replaced with host-side paths so the host Docker daemon can resolve them.
func (p *containerPool) translateMountPaths(mounts []port.Mount) []port.Mount {
	if p.hostDataDir == "" || p.containerDataDir == "" {
		return mounts
	}
	out := make([]port.Mount, len(mounts))
	for i, m := range mounts {
		out[i] = m
		if strings.HasPrefix(m.Source, p.containerDataDir) {
			out[i].Source = p.hostDataDir + m.Source[len(p.containerDataDir):]
		}
	}
	return out
}

// validateMounts checks that all mount source and target paths are absolute.
func validateMounts(mounts []port.Mount) error {
	for _, m := range mounts {
		if !filepath.IsAbs(m.Source) {
			return fmt.Errorf("mount source %q: must be absolute path", m.Source)
		}
		if !filepath.IsAbs(m.Target) {
			return fmt.Errorf("mount target %q: must be absolute path", m.Target)
		}
	}
	return nil
}

func (p *containerPool) ensure(ctx context.Context, userID entity.UserID, tenantID entity.TenantID, mounts []port.Mount) (string, error) {
	if err := validateMounts(mounts); err != nil {
		return "", fmt.Errorf("pool: %w", err)
	}
	wantBinds := mountsToBinds(p.translateMountPaths(mounts))

	// Check existing.
	if val, ok := p.containers.Load(userID); ok {
		ci := val.(*containerInfo)
		resp, err := p.client.containerInspect(ctx, ci.containerID)
		if err == nil && resp.State.Status == "running" {
			// Verify mounts match.
			if bindsMatch(resp.HostConfig.Binds, wantBinds) {
				ci.lastUsed.Store(time.Now().Unix())
				return "/workspace", nil
			}
			// Mounts differ -- release and recreate.
			slog.Info("pool: mounts changed, recreating container", "user", userID)
		}
		// Stopped, error, or mount mismatch -- remove old container.
		_ = p.client.containerStop(ctx, ci.containerID, int(p.stopTimeout.Seconds()))
		_ = p.client.containerRemove(ctx, ci.containerID, true)
		p.containers.Delete(userID)
	}

	// Ensure writable mount sources are owned by the container user so the
	// sandbox process can write to them. Best-effort: harmless when not root.
	for _, m := range mounts {
		if !m.ReadOnly {
			if err := os.Chown(m.Source, p.cfg.ContainerUID, p.cfg.ContainerGID); err != nil {
				slog.Debug("pool: chown mount source", "path", m.Source, "err", err)
			}
		}
	}

	// Build hardened container request.
	uid := string(userID)
	nameID := uid
	if len(nameID) > 8 {
		nameID = nameID[:8]
	}
	name := "whiteagent-" + nameID

	pidsLimit := int64(p.cfg.Resources.PidsLimit)
	tmpfsOpt := fmt.Sprintf("size=%dm", p.cfg.Resources.TmpfsMB)
	msgTmpfsOpt := fmt.Sprintf("size=%dm,uid=%d,mode=0700", p.cfg.Resources.TmpfsMB, p.cfg.ContainerUID)

	var networkMode string
	var netConfig *networkingConfig
	if p.cfg.Network != "" {
		networkMode = p.cfg.Network
		netConfig = &networkingConfig{
			EndpointsConfig: map[string]endpointSettings{
				p.cfg.Network: {},
			},
		}
	} else if p.cfg.NetworkMode != "" {
		networkMode = p.cfg.NetworkMode
	} else if !*p.cfg.AllowNetwork {
		networkMode = "none"
	} else {
		networkMode = "bridge"
	}

	containerUser := fmt.Sprintf("%d:%d", p.cfg.ContainerUID, p.cfg.ContainerGID)

	body := createContainerRequest{
		Image: p.cfg.Image,
		Cmd:   []string{"sleep", "infinity"},
		User:  containerUser,
		Labels: map[string]string{
			labelManaged:  "true",
			labelUserID:   uid,
			labelTenantID: string(tenantID),
		},
		HostConfig: hostConfig{
			CapDrop:        []string{"ALL"},
			SecurityOpt:    []string{"no-new-privileges"},
			ReadonlyRootfs: true,
			NetworkMode:    networkMode,
			NanoCpus:       int64(p.cfg.Resources.CPUCores * 1e9),
			Memory:         int64(p.cfg.Resources.MemoryMB) * 1024 * 1024,
			PidsLimit:      &pidsLimit,
			Ulimits: []ulimit{
				{Name: "nofile", Soft: 1024, Hard: 2048},
			},
			Tmpfs: map[string]string{
				"/tmp":     tmpfsOpt,
				"/var/tmp": tmpfsOpt,
				"/message": msgTmpfsOpt,
			},
			Binds: wantBinds,
		},
		NetworkingConfig: netConfig,
	}

	if len(p.cfg.Env) > 0 {
		env := make([]string, 0, len(p.cfg.Env))
		for k, v := range p.cfg.Env {
			env = append(env, k+"="+v)
		}
		body.Env = env
	}

	containerID, err := p.client.containerCreate(ctx, name, body)
	if err != nil {
		return "", fmt.Errorf("pool: create container: %w", err)
	}

	if err := p.client.containerStart(ctx, containerID); err != nil {
		// Best-effort cleanup on start failure.
		_ = p.client.containerRemove(ctx, containerID, true)
		return "", fmt.Errorf("pool: start container: %w", err)
	}

	ci := &containerInfo{containerID: containerID}
	ci.lastUsed.Store(time.Now().Unix())
	p.containers.Store(userID, ci)

	return "/workspace", nil
}

// ---------------------------------------------------------------------------
// Exec
// ---------------------------------------------------------------------------

func (p *containerPool) exec(ctx context.Context, userID entity.UserID, req port.ExecRequest) (port.ExecResult, error) {
	val, ok := p.containers.Load(userID)
	if !ok {
		return port.ExecResult{}, fmt.Errorf("pool: no container for user %s", userID)
	}
	ci := val.(*containerInfo)

	ci.activeExecs.Add(1)
	defer func() {
		ci.activeExecs.Add(-1)
		ci.lastUsed.Store(time.Now().Unix())
	}()

	// Apply exec timeout.
	execCtx, cancel := context.WithTimeout(ctx, p.execTimeout)
	defer cancel()

	workDir := req.WorkDir
	if workDir == "" {
		workDir = "/home/whiteagent"
	}

	cmd := append([]string{req.Command}, req.Args...)
	var env []string
	for k, v := range req.Env {
		env = append(env, k+"="+v)
	}

	execID, err := p.client.execCreate(execCtx, ci.containerID, createExecRequest{
		Cmd:          cmd,
		WorkingDir:   workDir,
		Env:          env,
		User:         fmt.Sprintf("%d:%d", p.cfg.ContainerUID, p.cfg.ContainerGID),
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
	})
	if err != nil {
		return port.ExecResult{}, fmt.Errorf("pool: exec create: %w", err)
	}

	maxOutput := p.cfg.MaxOutputMB * 1024 * 1024
	stdout, stderr, err := p.client.execStart(execCtx, execID, maxOutput)
	if err != nil {
		if execCtx.Err() != nil {
			return port.ExecResult{}, fmt.Errorf("%w: %v", ErrExecTimeout, err)
		}
		return port.ExecResult{}, fmt.Errorf("pool: exec start: %w", err)
	}

	info, err := p.client.execInspect(execCtx, execID)
	if err != nil {
		return port.ExecResult{}, fmt.Errorf("pool: exec inspect: %w", err)
	}

	return port.ExecResult{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: info.ExitCode,
	}, nil
}

// ---------------------------------------------------------------------------
// Release
// ---------------------------------------------------------------------------

func (p *containerPool) release(ctx context.Context, userID entity.UserID) error {
	val, ok := p.containers.LoadAndDelete(userID)
	if !ok {
		return nil
	}
	ci := val.(*containerInfo)

	stopSec := int(p.stopTimeout.Seconds())
	if err := p.client.containerStop(ctx, ci.containerID, stopSec); err != nil {
		slog.Warn("pool: stop container", "container", ci.containerID, "err", err)
	}
	if err := p.client.containerRemove(ctx, ci.containerID, true); err != nil {
		slog.Warn("pool: remove container", "container", ci.containerID, "err", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Sweep
// ---------------------------------------------------------------------------

func (p *containerPool) startSweep(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.sweepOnce()
			case <-p.sweepDone:
				return
			}
		}
	}()
}

func (p *containerPool) sweepOnce() {
	now := time.Now().Unix()
	idleSec := int64(p.idleTimeout.Seconds())

	p.containers.Range(func(key, value any) bool {
		uid := key.(entity.UserID)
		ci := value.(*containerInfo)

		if ci.activeExecs.Load() > 0 {
			return true // skip active
		}
		if now-ci.lastUsed.Load() > idleSec {
			_ = p.release(context.Background(), uid)
		}
		return true
	})
}

func (p *containerPool) stopSweep() {
	select {
	case <-p.sweepDone:
		// already closed
	default:
		close(p.sweepDone)
	}
}

// ---------------------------------------------------------------------------
// Orphan cleanup & rebuild
// ---------------------------------------------------------------------------

func (p *containerPool) cleanOrphans(ctx context.Context) {
	entries, err := p.client.containerList(ctx, labelManaged+"=true")
	if err != nil {
		slog.Warn("pool: list orphans", "err", err)
		return
	}
	for _, e := range entries {
		// Skip service containers (e.g. browserless) — they are managed
		// by the service lifecycle, not the container pool.
		if e.Labels[labelService] != "" {
			continue
		}
		stopSec := int(p.stopTimeout.Seconds())
		if err := p.client.containerStop(ctx, e.ID, stopSec); err != nil {
			slog.Warn("pool: stop orphan", "container", e.ID, "err", err)
		}
		if err := p.client.containerRemove(ctx, e.ID, true); err != nil {
			slog.Warn("pool: remove orphan", "container", e.ID, "err", err)
		}
	}
}

func (p *containerPool) rebuildFromLabels(ctx context.Context) {
	entries, err := p.client.containerList(ctx, labelManaged+"=true")
	if err != nil {
		slog.Warn("pool: list for rebuild", "err", err)
		return
	}
	for _, e := range entries {
		if e.State != "running" {
			continue
		}
		uid := entity.UserID(e.Labels[labelUserID])
		if uid == "" {
			continue
		}
		ci := &containerInfo{containerID: e.ID}
		ci.lastUsed.Store(time.Now().Unix())
		p.containers.Store(uid, ci)
	}
}

// ---------------------------------------------------------------------------
// Stop all
// ---------------------------------------------------------------------------

func (p *containerPool) stopAll(ctx context.Context) {
	p.containers.Range(func(key, value any) bool {
		uid := key.(entity.UserID)
		_ = p.release(ctx, uid)
		return true
	})
}
