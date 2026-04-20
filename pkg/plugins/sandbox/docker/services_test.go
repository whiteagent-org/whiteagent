package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseServices(t *testing.T) {
	yaml := `
services:
  browserless:
    image: ghcr.io/browserless/chromium:latest
    environment:
      HOST: "0.0.0.0"
      PORT: "3000"
      TOKEN: secret123
    shm_size: 1gb
    mem_limit: 4g
    mem_reservation: 2g
    cpus: 2.0
    init: true
    security_opt:
      - "no-new-privileges:true"
    tmpfs:
      - "/tmp:size=512m,exec"
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://127.0.0.1:3000/config"]
      interval: 60s
      timeout: 5s
      retries: 3
`
	path := writeTempFile(t, yaml)

	sf, err := parseServices(path)
	if err != nil {
		t.Fatalf("parseServices: %v", err)
	}

	if len(sf.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(sf.Services))
	}

	svc, ok := sf.Services["browserless"]
	if !ok {
		t.Fatal("expected 'browserless' service")
	}

	if svc.Image != "ghcr.io/browserless/chromium:latest" {
		t.Errorf("Image = %q", svc.Image)
	}
	if svc.Environment["PORT"] != "3000" {
		t.Errorf("PORT = %q", svc.Environment["PORT"])
	}
	if svc.ShmSize != "1gb" {
		t.Errorf("ShmSize = %q", svc.ShmSize)
	}
	if svc.MemLimit != "4g" {
		t.Errorf("MemLimit = %q", svc.MemLimit)
	}
	if svc.MemReservation != "2g" {
		t.Errorf("MemReservation = %q", svc.MemReservation)
	}
	if svc.CPUs != 2.0 {
		t.Errorf("CPUs = %f", svc.CPUs)
	}
	if !svc.Init {
		t.Error("Init should be true")
	}
	if svc.Restart != "unless-stopped" {
		t.Errorf("Restart = %q", svc.Restart)
	}
	if len(svc.SecurityOpt) != 1 || svc.SecurityOpt[0] != "no-new-privileges:true" {
		t.Errorf("SecurityOpt = %v", svc.SecurityOpt)
	}
	if len(svc.Tmpfs) != 1 || svc.Tmpfs[0] != "/tmp:size=512m,exec" {
		t.Errorf("Tmpfs = %v", svc.Tmpfs)
	}
	if svc.Healthcheck == nil {
		t.Fatal("Healthcheck should not be nil")
	}
	if svc.Healthcheck.Retries != 3 {
		t.Errorf("Healthcheck.Retries = %d", svc.Healthcheck.Retries)
	}
}

func TestParseServicesEnvExpansion(t *testing.T) {
	t.Setenv("TEST_TOKEN", "expanded_value")

	yaml := `
services:
  svc:
    image: test:latest
    environment:
      TOKEN: ${TEST_TOKEN}
      OTHER: $TEST_TOKEN
`
	path := writeTempFile(t, yaml)

	sf, err := parseServices(path)
	if err != nil {
		t.Fatalf("parseServices: %v", err)
	}

	svc := sf.Services["svc"]
	if svc.Environment["TOKEN"] != "expanded_value" {
		t.Errorf("TOKEN = %q, want expanded_value", svc.Environment["TOKEN"])
	}
	if svc.Environment["OTHER"] != "expanded_value" {
		t.Errorf("OTHER = %q, want expanded_value", svc.Environment["OTHER"])
	}
}

func TestParseServicesMissingImage(t *testing.T) {
	yaml := `
services:
  bad:
    environment:
      FOO: bar
`
	path := writeTempFile(t, yaml)

	_, err := parseServices(path)
	if err == nil {
		t.Fatal("expected error for missing image")
	}
}

func TestParseServicesEmptyFile(t *testing.T) {
	path := writeTempFile(t, "services: {}")

	sf, err := parseServices(path)
	if err != nil {
		t.Fatalf("parseServices: %v", err)
	}
	if len(sf.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(sf.Services))
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"1gb", 1024 * 1024 * 1024},
		{"1g", 1024 * 1024 * 1024},
		{"512mb", 512 * 1024 * 1024},
		{"512m", 512 * 1024 * 1024},
		{"1024kb", 1024 * 1024},
		{"1024k", 1024 * 1024},
		{"100b", 100},
		{"4096", 4096},
		{"2gb", 2 * 1024 * 1024 * 1024},
		{"", 0},
	}

	for _, tt := range tests {
		got, err := parseSize(tt.input)
		if err != nil {
			t.Errorf("parseSize(%q): %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseSize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseSizeInvalid(t *testing.T) {
	for _, input := range []string{"abc", "1xyz", "gb"} {
		_, err := parseSize(input)
		if err == nil {
			t.Errorf("parseSize(%q): expected error", input)
		}
	}
}

func TestParseTmpfs(t *testing.T) {
	entries := []string{"/tmp:size=512m,exec", "/var/tmp"}
	m := parseTmpfs(entries)
	if m["/tmp"] != "size=512m,exec" {
		t.Errorf("/tmp opts = %q", m["/tmp"])
	}
	if m["/var/tmp"] != "" {
		t.Errorf("/var/tmp opts = %q, want empty", m["/var/tmp"])
	}
}

func TestBuildServiceRequest(t *testing.T) {
	spec := serviceSpec{
		Image: "test:latest",
		Environment: map[string]string{
			"FOO": "bar",
		},
		ShmSize:        "1gb",
		MemLimit:       "4g",
		MemReservation: "2g",
		CPUs:           2.0,
		Init:           true,
		SecurityOpt:    []string{"no-new-privileges:true"},
		Tmpfs:          []string{"/tmp:size=512m"},
		Restart:        "unless-stopped",
		Healthcheck: &serviceHealthcheck{
			Test:    []string{"CMD", "curl", "-f", "http://localhost:3000"},
			Retries: 3,
		},
	}

	body, err := buildServiceRequest("browserless", "sandbox", spec)
	if err != nil {
		t.Fatalf("buildServiceRequest: %v", err)
	}

	if body.Image != "test:latest" {
		t.Errorf("Image = %q", body.Image)
	}
	if body.Labels[labelService] != "browserless" {
		t.Errorf("service label = %q", body.Labels[labelService])
	}

	// Network config.
	if body.NetworkingConfig == nil {
		t.Fatal("NetworkingConfig should not be nil")
	}
	ep, ok := body.NetworkingConfig.EndpointsConfig["sandbox"]
	if !ok {
		t.Fatal("expected endpoint for 'sandbox' network")
	}
	if len(ep.Aliases) != 1 || ep.Aliases[0] != "browserless" {
		t.Errorf("Aliases = %v, want [browserless]", ep.Aliases)
	}

	// Host config.
	if body.HostConfig.ShmSize != 1024*1024*1024 {
		t.Errorf("ShmSize = %d", body.HostConfig.ShmSize)
	}
	if body.HostConfig.Memory != 4*1024*1024*1024 {
		t.Errorf("Memory = %d", body.HostConfig.Memory)
	}
	if body.HostConfig.MemoryReservation != 2*1024*1024*1024 {
		t.Errorf("MemoryReservation = %d", body.HostConfig.MemoryReservation)
	}
	if body.HostConfig.NanoCpus != 2e9 {
		t.Errorf("NanoCpus = %d", body.HostConfig.NanoCpus)
	}
	if body.HostConfig.Init == nil || !*body.HostConfig.Init {
		t.Error("Init should be true")
	}
	if body.HostConfig.RestartPolicy == nil || body.HostConfig.RestartPolicy.Name != "unless-stopped" {
		t.Error("RestartPolicy should be unless-stopped")
	}

	// Healthcheck.
	if body.Healthcheck == nil {
		t.Fatal("Healthcheck should not be nil")
	}
	if body.Healthcheck.Retries != 3 {
		t.Errorf("Healthcheck.Retries = %d", body.Healthcheck.Retries)
	}
}

func TestMapRestartPolicy(t *testing.T) {
	tests := map[string]string{
		"always":         "always",
		"unless-stopped": "unless-stopped",
		"on-failure":     "on-failure",
		"no":             "no",
		"":               "no",
		"unknown":        "no",
	}
	for input, want := range tests {
		got := mapRestartPolicy(input)
		if got != want {
			t.Errorf("mapRestartPolicy(%q) = %q, want %q", input, got, want)
		}
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "services.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
