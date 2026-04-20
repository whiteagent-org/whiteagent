package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Compile-time interface assertions.
var (
	_ port.SandboxPlugin    = (*dockerSandbox)(nil)
	_ port.FileTransferable = (*dockerSandbox)(nil)
	_ port.ScopedFSAware    = (*dockerSandbox)(nil)
)

// Manifest returns plugin metadata for pre-instantiation kind validation.
func Manifest() port.PluginManifest {
	return port.PluginManifest{Kind: entity.PluginKindSandbox}
}

// NewPlugin returns a new Docker sandbox plugin instance.
func NewPlugin() port.Plugin {
	return &dockerSandbox{}
}

// ---------------------------------------------------------------------------
// Plugin
// ---------------------------------------------------------------------------

type dockerSandbox struct {
	id          string
	cfg         dockerConfig
	client      poolClient
	pool        *containerPool
	state       atomic.Value // entity.PluginState
	tenantID    entity.TenantID
	dataBaseDir string // container-side data dir from ScopedFS (for DooD path translation)
	services    []serviceInstance
}

func (s *dockerSandbox) ID() string              { return s.id }
func (s *dockerSandbox) Kind() entity.PluginKind { return entity.PluginKindSandbox }

func (s *dockerSandbox) Status() entity.PluginState {
	if v := s.state.Load(); v != nil {
		return v.(entity.PluginState)
	}
	return entity.PluginStateUnhealthy
}

func (s *dockerSandbox) Init(_ context.Context, id string, cfg json.RawMessage) error {
	s.id = id

	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &s.cfg); err != nil {
			return fmt.Errorf("docker-sandbox: parse config: %w", err)
		}
	}
	s.cfg.applyDefaults()
	if err := s.cfg.validate(); err != nil {
		return fmt.Errorf("docker-sandbox: invalid config: %w", err)
	}

	// No HTTP-level timeout — exec streaming is bounded by the context-based
	// exec_timeout applied in pool.exec. A fixed http.Client.Timeout would race
	// against that context and kill long-running commands prematurely.
	s.client = newDockerClient(s.cfg.SocketPath, 0)
	s.state.Store(entity.PluginStateUnhealthy)
	return nil
}

func (s *dockerSandbox) Start(ctx context.Context) error {
	// Retry ping to tolerate Docker daemon startup delay (e.g. dind sidecar).
	const (
		maxRetries    = 30
		retryInterval = time.Second
	)
	var pingErr error
	for i := range maxRetries {
		if pingErr = s.client.ping(ctx); pingErr == nil {
			break
		}
		if !errors.Is(pingErr, ErrDockerUnavailable) {
			return fmt.Errorf("docker-sandbox: start: %w", pingErr)
		}
		if i < maxRetries-1 {
			slog.Info("docker sandbox waiting for daemon", "attempt", i+1, "err", pingErr)
			select {
			case <-ctx.Done():
				return fmt.Errorf("docker-sandbox: start: %w", ctx.Err())
			case <-time.After(retryInterval):
			}
		}
	}
	if pingErr != nil {
		return fmt.Errorf("docker-sandbox: start: %w", pingErr)
	}

	// Create isolated network for sandbox containers and services.
	if s.cfg.Network != "" {
		dc := s.client.(*dockerClient)
		slog.Info("docker sandbox creating network", "network", s.cfg.Network)
		if err := dc.networkCreate(ctx, s.cfg.Network, "bridge"); err != nil {
			return fmt.Errorf("docker-sandbox: create network: %w", err)
		}
	}

	// Start sidecar services (e.g. browserless) on the sandbox network.
	if s.cfg.Services != "" {
		dc := s.client.(*dockerClient)
		sf, err := parseServices(s.cfg.Services)
		if err != nil {
			return fmt.Errorf("docker-sandbox: %w", err)
		}
		instances, err := startServices(ctx, dc, s.cfg.Network, sf)
		if err != nil {
			return fmt.Errorf("docker-sandbox: start services: %w", err)
		}
		s.services = instances
	}

	slog.Info("docker sandbox pulling image", "image", s.cfg.Image)
	if err := s.client.imagePull(ctx, s.cfg.Image); err != nil {
		return fmt.Errorf("docker-sandbox: pull image: %w", err)
	}

	s.pool = newPool(s.client, &s.cfg, s.dataBaseDir)

	s.pool.cleanOrphans(ctx)
	s.pool.rebuildFromLabels(ctx)
	s.pool.startSweep(60 * time.Second)

	s.state.Store(entity.PluginStateHealthy)
	slog.Info("docker sandbox started",
		"socket", s.cfg.SocketPath,
		"image", s.cfg.Image,
	)
	return nil
}

func (s *dockerSandbox) Stop(ctx context.Context) error {
	if s.pool != nil {
		s.pool.stopSweep()
		s.pool.stopAll(ctx)
	}

	// Stop sidecar services and remove network.
	if dc, ok := s.client.(*dockerClient); ok {
		if len(s.services) > 0 {
			stopServiceInstances(ctx, dc, s.services)
			s.services = nil
		}
		if s.cfg.Network != "" {
			_ = dc.networkRemove(ctx, s.cfg.Network)
		}
	}

	s.state.Store(entity.PluginStateUnhealthy)
	slog.Info("docker sandbox stopped")
	return nil
}

// ---------------------------------------------------------------------------
// ScopedFSAware
// ---------------------------------------------------------------------------

func (s *dockerSandbox) SetScopedFS(sfs port.ScopedFS) {
	s.dataBaseDir = sfs.BaseDir()
}

// ---------------------------------------------------------------------------
// SandboxPlugin
// ---------------------------------------------------------------------------

func (s *dockerSandbox) Ensure(ctx context.Context, userID entity.UserID) (string, error) {
	return s.EnsureWithMounts(ctx, userID, nil)
}

// EnsureWithMounts creates or reuses a sandbox with specified bind mounts.
func (s *dockerSandbox) EnsureWithMounts(ctx context.Context, userID entity.UserID, mounts []port.Mount) (string, error) {
	if s.pool == nil {
		return "", fmt.Errorf("docker-sandbox: not ready (still starting)")
	}
	workDir, err := s.pool.ensure(ctx, userID, s.tenantID, mounts)
	if err != nil {
		if isDockerError(err) {
			s.state.Store(entity.PluginStateDegraded)
		}
		return "", err
	}
	if s.Status() == entity.PluginStateDegraded {
		s.state.Store(entity.PluginStateHealthy)
	}
	return workDir, nil
}

func (s *dockerSandbox) Exec(ctx context.Context, userID entity.UserID, req port.ExecRequest) (port.ExecResult, error) {
	if s.pool == nil {
		return port.ExecResult{}, fmt.Errorf("docker-sandbox: not ready (still starting)")
	}
	result, err := s.pool.exec(ctx, userID, req)
	if err != nil {
		if isDockerError(err) {
			s.state.Store(entity.PluginStateDegraded)
		}
		return port.ExecResult{}, err
	}
	if s.Status() == entity.PluginStateDegraded {
		s.state.Store(entity.PluginStateHealthy)
	}
	return result, nil
}

func (s *dockerSandbox) Release(ctx context.Context, userID entity.UserID) error {
	return s.pool.release(ctx, userID)
}

func (s *dockerSandbox) UserHomePath(_ entity.UserID) string     { return s.cfg.UserHomeMount }
func (s *dockerSandbox) TenantHomePath(_ entity.TenantID) string { return s.cfg.TenantHomeMount }
func (s *dockerSandbox) MessagesPath() string                    { return s.cfg.MessagesMount }

// ---------------------------------------------------------------------------
// FileTransferable
// ---------------------------------------------------------------------------

// CopyTo copies a host directory into the container at containerPath using
// the Docker archive API (PUT /containers/{id}/archive). The host directory
// contents are tar-archived and streamed to the container.
func (s *dockerSandbox) CopyTo(ctx context.Context, userID entity.UserID, hostPath, containerPath string) error {
	val, ok := s.pool.containers.Load(userID)
	if !ok {
		return fmt.Errorf("docker-sandbox: no container for user %s", userID)
	}
	ci := val.(*containerInfo)

	// Ensure destination directory exists inside the container.
	// Docker's putArchive requires the target path to exist.
	if _, err := s.pool.exec(ctx, userID, port.ExecRequest{
		Command: "mkdir",
		Args:    []string{"-p", containerPath},
	}); err != nil {
		return fmt.Errorf("docker-sandbox: create dest dir: %w", err)
	}

	archive, err := tarDir(hostPath)
	if err != nil {
		return fmt.Errorf("docker-sandbox: create tar: %w", err)
	}

	if err := s.pool.client.putArchive(ctx, ci.containerID, containerPath, archive); err != nil {
		return fmt.Errorf("docker-sandbox: copy to container: %w", err)
	}
	return nil
}

// CopyFrom copies a container directory to the host at hostPath using the
// Docker archive API (GET /containers/{id}/archive). The tar stream is
// extracted to the host directory.
func (s *dockerSandbox) CopyFrom(ctx context.Context, userID entity.UserID, containerPath, hostPath string) error {
	val, ok := s.pool.containers.Load(userID)
	if !ok {
		return fmt.Errorf("docker-sandbox: no container for user %s", userID)
	}
	ci := val.(*containerInfo)

	rc, err := s.pool.client.getArchive(ctx, ci.containerID, containerPath)
	if err != nil {
		if errors.Is(err, ErrContainerNotFound) {
			// Container exists in pool but Docker returned 404 —
			// the path doesn't exist inside the container. Not an error.
			return nil
		}
		return fmt.Errorf("docker-sandbox: copy from container: %w", err)
	}
	defer rc.Close()

	if err := untarDir(rc, hostPath); err != nil {
		return fmt.Errorf("docker-sandbox: extract tar: %w", err)
	}
	return nil
}

// tarDir creates a tar archive from the contents of dir.
func tarDir(dir string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}

// untarDir extracts a tar archive into destDir. Docker's archive API wraps
// directory contents with the source directory name as a prefix. This function
// detects and strips that leading directory so files land directly in destDir.
func untarDir(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	var stripPrefix string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		// Sanitize path to prevent directory traversal.
		name := filepath.Clean(header.Name)
		if strings.HasPrefix(name, "..") {
			continue
		}

		// Strip the leading directory that Docker's archive API adds.
		if stripPrefix == "" && header.Typeflag == tar.TypeDir {
			stripPrefix = name + "/"
			continue
		}
		if stripPrefix != "" {
			name = strings.TrimPrefix(name, stripPrefix)
			if name == "" {
				continue
			}
		}

		target := filepath.Join(destDir, name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
}

// isDockerError returns true if the error chain contains a Docker-specific sentinel.
func isDockerError(err error) bool {
	return errors.Is(err, ErrDockerUnavailable) ||
		errors.Is(err, ErrContainerNotFound) ||
		errors.Is(err, ErrExecFailed)
}
