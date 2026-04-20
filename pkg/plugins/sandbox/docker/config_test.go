package docker

import (
	"encoding/json"
	"testing"
)

func TestDockerConfig_EnvField(t *testing.T) {
	raw := `{
		"socket_path": "/var/run/docker.sock",
		"image": "sandbox:latest",
		"idle_timeout": "15m",
		"exec_timeout": "5m",
		"stop_timeout": "10s",
		"max_output_mb": 1,
		"container_uid": 1000,
		"container_gid": 1000,
		"env": {
			"BROWSER_WS": "ws://browserless:3000",
			"FOO": "bar"
		}
	}`

	var cfg dockerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(cfg.Env) != 2 {
		t.Fatalf("expected 2 env entries, got %d", len(cfg.Env))
	}
	if cfg.Env["BROWSER_WS"] != "ws://browserless:3000" {
		t.Errorf("BROWSER_WS = %q, want %q", cfg.Env["BROWSER_WS"], "ws://browserless:3000")
	}
	if cfg.Env["FOO"] != "bar" {
		t.Errorf("FOO = %q, want %q", cfg.Env["FOO"], "bar")
	}
}

func TestDockerConfig_NetworkModeField(t *testing.T) {
	raw := `{
		"socket_path": "/var/run/docker.sock",
		"image": "sandbox:latest",
		"idle_timeout": "15m",
		"exec_timeout": "5m",
		"stop_timeout": "10s",
		"max_output_mb": 1,
		"container_uid": 1000,
		"container_gid": 1000,
		"network_mode": "host"
	}`

	var cfg dockerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.NetworkMode != "host" {
		t.Errorf("NetworkMode = %q, want %q", cfg.NetworkMode, "host")
	}
}

func TestDockerConfig_NetworkModeValidation(t *testing.T) {
	base := dockerConfig{
		SocketPath:   "/var/run/docker.sock",
		Image:        "sandbox:latest",
		IdleTimeout:  "15m",
		ExecTimeout:  "5m",
		StopTimeout:  "10s",
		MaxOutputMB:  1,
		AllowNetwork: func() *bool { b := true; return &b }(),
		ContainerUID: 1000,
		ContainerGID: 1000,
		Resources:    resourceConfig{CPUCores: 0.5, MemoryMB: 256, PidsLimit: 100, TmpfsMB: 64},
	}

	t.Run("valid modes", func(t *testing.T) {
		for _, mode := range []string{"bridge", "host", "none", "container:abc123"} {
			cfg := base
			cfg.NetworkMode = mode
			if err := cfg.validate(); err != nil {
				t.Errorf("network_mode %q: unexpected error: %v", mode, err)
			}
		}
	})

	t.Run("invalid mode", func(t *testing.T) {
		cfg := base
		cfg.NetworkMode = "foobar"
		err := cfg.validate()
		if err == nil {
			t.Fatal("expected validation error for invalid network_mode")
		}
	})

	t.Run("host with host_data_dir rejected", func(t *testing.T) {
		cfg := base
		cfg.NetworkMode = "host"
		cfg.HostDataDir = "/host/data"
		err := cfg.validate()
		if err == nil {
			t.Fatal("expected validation error for host mode with host_data_dir")
		}
	})
}

func TestDockerConfig_NetworkAndNetworkModeMutualExclusion(t *testing.T) {
	base := dockerConfig{
		SocketPath:   "/var/run/docker.sock",
		Image:        "sandbox:latest",
		IdleTimeout:  "15m",
		ExecTimeout:  "5m",
		StopTimeout:  "10s",
		MaxOutputMB:  1,
		AllowNetwork: func() *bool { b := true; return &b }(),
		ContainerUID: 1000,
		ContainerGID: 1000,
		Resources:    resourceConfig{CPUCores: 0.5, MemoryMB: 256, PidsLimit: 100, TmpfsMB: 64},
	}

	t.Run("network alone valid", func(t *testing.T) {
		cfg := base
		cfg.Network = "sandbox"
		if err := cfg.validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("network and network_mode rejected", func(t *testing.T) {
		cfg := base
		cfg.Network = "sandbox"
		cfg.NetworkMode = "host"
		if err := cfg.validate(); err == nil {
			t.Fatal("expected error for network + network_mode")
		}
	})

	t.Run("network with allow_network false rejected", func(t *testing.T) {
		cfg := base
		cfg.Network = "sandbox"
		cfg.AllowNetwork = func() *bool { b := false; return &b }()
		if err := cfg.validate(); err == nil {
			t.Fatal("expected error for network with allow_network=false")
		}
	})

	t.Run("services without network rejected", func(t *testing.T) {
		cfg := base
		cfg.Services = "/some/path.yaml"
		if err := cfg.validate(); err == nil {
			t.Fatal("expected error for services without network")
		}
	})
}

func TestDockerConfig_EnvFieldEmpty(t *testing.T) {
	raw := `{
		"socket_path": "/var/run/docker.sock",
		"image": "sandbox:latest",
		"idle_timeout": "15m",
		"exec_timeout": "5m",
		"stop_timeout": "10s",
		"max_output_mb": 1
	}`

	var cfg dockerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Env != nil {
		t.Errorf("expected nil env map, got %v", cfg.Env)
	}
}
