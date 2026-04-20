// Package docker implements a Docker-based sandbox plugin that manages
// per-user containers via the Docker Engine HTTP API over Unix socket.
package docker

import "errors"

var (
	// ErrDockerUnavailable indicates the Docker daemon is not reachable.
	ErrDockerUnavailable = errors.New("docker: daemon unavailable")

	// ErrContainerNotFound indicates the requested container does not exist.
	ErrContainerNotFound = errors.New("docker: container not found")

	// ErrContainerNotRunning indicates the container exists but is not running.
	ErrContainerNotRunning = errors.New("docker: container not running")

	// ErrExecFailed indicates exec creation or start failed.
	ErrExecFailed = errors.New("docker: exec failed")

	// ErrExecTimeout indicates the exec exceeded its timeout.
	ErrExecTimeout = errors.New("docker: exec timeout")

	// ErrOutputTruncated indicates exec output exceeded the max buffer size.
	// Informational — the truncated output is still returned.
	ErrOutputTruncated = errors.New("docker: output truncated")
)
