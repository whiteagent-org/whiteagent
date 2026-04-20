package docker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type resourceConfig struct {
	CPUCores  float64 `json:"cpu_cores"`
	MemoryMB  int     `json:"memory_mb"`
	PidsLimit int     `json:"pids_limit"`
	TmpfsMB   int     `json:"tmpfs_mb"`
}

type dockerConfig struct {
	SocketPath      string            `json:"socket_path"`
	Image           string            `json:"image"`
	IdleTimeout     string            `json:"idle_timeout"`
	ExecTimeout     string            `json:"exec_timeout"`
	AllowNetwork    *bool             `json:"allow_network"`
	NetworkMode     string            `json:"network_mode"`
	MaxOutputMB     int               `json:"max_output_mb"`
	Resources       resourceConfig    `json:"resources"`
	StopTimeout     string            `json:"stop_timeout"`
	UserHomeMount   string            `json:"user_home_mount"`
	TenantHomeMount string            `json:"tenant_home_mount"`
	MessagesMount   string            `json:"messages_mount"`
	HostDataDir     string            `json:"host_data_dir"`
	ContainerUID    int               `json:"container_uid"`
	ContainerGID    int               `json:"container_gid"`
	Env             map[string]string `json:"env"`
	Network         string            `json:"network"`
	Services        string            `json:"services"`
}

func (c *dockerConfig) applyDefaults() {
	if c.SocketPath == "" {
		if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
			c.SocketPath = strings.TrimPrefix(dockerHost, "unix://")
		} else {
			c.SocketPath = "/var/run/docker.sock"
		}
	}
	if c.Image == "" {
		c.Image = "ghcr.io/whiteagent-org/sandbox:latest"
	}
	if c.IdleTimeout == "" {
		c.IdleTimeout = "15m"
	}
	if c.ExecTimeout == "" {
		c.ExecTimeout = "5m"
	}
	if c.StopTimeout == "" {
		c.StopTimeout = "10s"
	}
	if c.MaxOutputMB == 0 {
		c.MaxOutputMB = 1
	}
	if c.AllowNetwork == nil {
		t := true
		c.AllowNetwork = &t
	}
	if c.Resources.CPUCores == 0 {
		c.Resources.CPUCores = 0.5
	}
	if c.Resources.MemoryMB == 0 {
		c.Resources.MemoryMB = 256
	}
	if c.Resources.PidsLimit == 0 {
		c.Resources.PidsLimit = 100
	}
	if c.Resources.TmpfsMB == 0 {
		c.Resources.TmpfsMB = 64
	}
	if c.UserHomeMount == "" {
		c.UserHomeMount = "/home/whiteagent"
	}
	if c.TenantHomeMount == "" {
		c.TenantHomeMount = "/tenant"
	}
	if c.MessagesMount == "" {
		c.MessagesMount = "/messages"
	}
	if c.ContainerUID == 0 {
		c.ContainerUID = 1000
	}
	if c.ContainerGID == 0 {
		c.ContainerGID = 1000
	}
}

func (c *dockerConfig) validate() error {
	var errs []error

	if c.SocketPath == "" {
		errs = append(errs, fmt.Errorf("socket_path: must not be empty"))
	}
	if c.Image == "" {
		errs = append(errs, fmt.Errorf("image: must not be empty"))
	}
	if _, err := time.ParseDuration(c.IdleTimeout); err != nil {
		errs = append(errs, fmt.Errorf("idle_timeout: %w", err))
	}
	if _, err := time.ParseDuration(c.ExecTimeout); err != nil {
		errs = append(errs, fmt.Errorf("exec_timeout: %w", err))
	}
	if _, err := time.ParseDuration(c.StopTimeout); err != nil {
		errs = append(errs, fmt.Errorf("stop_timeout: %w", err))
	}
	if c.MaxOutputMB <= 0 {
		errs = append(errs, fmt.Errorf("max_output_mb: must be positive"))
	}
	if c.Resources.CPUCores <= 0 {
		errs = append(errs, fmt.Errorf("cpu_cores: must be positive"))
	}
	if c.Resources.MemoryMB <= 0 {
		errs = append(errs, fmt.Errorf("memory_mb: must be positive"))
	}
	if c.Resources.PidsLimit <= 0 {
		errs = append(errs, fmt.Errorf("pids_limit: must be positive"))
	}
	if c.Resources.TmpfsMB <= 0 {
		errs = append(errs, fmt.Errorf("tmpfs_mb: must be positive"))
	}
	if c.HostDataDir != "" && !filepath.IsAbs(c.HostDataDir) {
		errs = append(errs, fmt.Errorf("host_data_dir: must be an absolute path"))
	}
	if c.ContainerUID < 0 || c.ContainerUID > 65534 {
		errs = append(errs, fmt.Errorf("container_uid: must be 0-65534"))
	}
	if c.ContainerGID < 0 || c.ContainerGID > 65534 {
		errs = append(errs, fmt.Errorf("container_gid: must be 0-65534"))
	}
	if c.NetworkMode != "" {
		validModes := map[string]bool{"bridge": true, "host": true, "none": true}
		if !validModes[c.NetworkMode] && !strings.HasPrefix(c.NetworkMode, "container:") {
			errs = append(errs, fmt.Errorf("network_mode: must be bridge, host, none, or container:<id>"))
		}
	}
	if c.NetworkMode == "host" && c.HostDataDir != "" {
		errs = append(errs, fmt.Errorf("network_mode \"host\" is not allowed with host_data_dir (DooD mode): it would expose the host network to sandbox containers"))
	}
	if c.Network != "" && c.NetworkMode != "" {
		errs = append(errs, fmt.Errorf("network and network_mode are mutually exclusive"))
	}
	if c.Network != "" && c.AllowNetwork != nil && !*c.AllowNetwork {
		errs = append(errs, fmt.Errorf("network: cannot set network when allow_network is false"))
	}
	if c.Services != "" && c.Network == "" {
		errs = append(errs, fmt.Errorf("services: requires network to be set"))
	}
	if c.Services != "" {
		if _, err := os.Stat(c.Services); err != nil {
			errs = append(errs, fmt.Errorf("services: %w", err))
		}
	}

	return errors.Join(errs...)
}
