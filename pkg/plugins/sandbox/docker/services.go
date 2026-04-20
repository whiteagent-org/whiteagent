package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Service spec (compose-like YAML subset)
// ---------------------------------------------------------------------------

type serviceHealthcheck struct {
	Test     []string      `yaml:"test"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
	Retries  int           `yaml:"retries"`
}

type serviceSpec struct {
	Image          string              `yaml:"image"`
	User           string              `yaml:"user"`
	Environment    map[string]string   `yaml:"environment"`
	ShmSize        string              `yaml:"shm_size"`
	MemLimit       string              `yaml:"mem_limit"`
	MemReservation string              `yaml:"mem_reservation"`
	CPUs           float64             `yaml:"cpus"`
	Init           bool                `yaml:"init"`
	Healthcheck    *serviceHealthcheck `yaml:"healthcheck"`
	SecurityOpt    []string            `yaml:"security_opt"`
	Tmpfs          []string            `yaml:"tmpfs"`
	Restart        string              `yaml:"restart"`
}

type servicesFile struct {
	Services map[string]serviceSpec `yaml:"services"`
}

// Labels applied to managed service containers.
const (
	labelService = "whiteagent.service"
)

// envVarPattern matches ${VAR} or $VAR references.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

func parseServices(path string) (*servicesFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read services file: %w", err)
	}

	// Expand environment variables before YAML parsing.
	expanded := envVarPattern.ReplaceAllStringFunc(string(data), func(match string) string {
		var name string
		if strings.HasPrefix(match, "${") {
			name = match[2 : len(match)-1]
		} else {
			name = match[1:]
		}
		return os.Getenv(name)
	})

	var sf servicesFile
	if err := yaml.Unmarshal([]byte(expanded), &sf); err != nil {
		return nil, fmt.Errorf("parse services YAML: %w", err)
	}

	for name, spec := range sf.Services {
		if spec.Image == "" {
			return nil, fmt.Errorf("service %q: image is required", name)
		}
	}

	return &sf, nil
}

// parseSize converts human-readable sizes like "1gb", "512mb", "2g" to bytes.
func parseSize(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	s = strings.TrimSpace(strings.ToLower(s))

	// Ordered longest-suffix-first to avoid "b" matching before "gb"/"kb"/"mb".
	suffixes := []struct {
		suffix string
		mult   int64
	}{
		{"gb", 1024 * 1024 * 1024},
		{"mb", 1024 * 1024},
		{"kb", 1024},
		{"g", 1024 * 1024 * 1024},
		{"m", 1024 * 1024},
		{"k", 1024},
		{"b", 1},
	}

	for _, e := range suffixes {
		if strings.HasSuffix(s, e.suffix) {
			num := strings.TrimSuffix(s, e.suffix)
			v, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid size %q: %w", s, err)
			}
			return int64(v * float64(e.mult)), nil
		}
	}

	// Try plain number (bytes).
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return v, nil
}

// parseTmpfs converts a slice like ["/tmp:size=512m,exec"] to a Docker Tmpfs map.
func parseTmpfs(entries []string) map[string]string {
	if len(entries) == 0 {
		return nil
	}
	m := make(map[string]string, len(entries))
	for _, e := range entries {
		path, opts, _ := strings.Cut(e, ":")
		m[path] = opts
	}
	return m
}

// ---------------------------------------------------------------------------
// Service lifecycle
// ---------------------------------------------------------------------------

type serviceInstance struct {
	name        string
	containerID string
}

func startServices(ctx context.Context, client *dockerClient, network string, sf *servicesFile) ([]serviceInstance, error) {
	var instances []serviceInstance

	for name, spec := range sf.Services {
		slog.Info("sandbox: pulling service image", "service", name, "image", spec.Image)
		if err := client.imagePull(ctx, spec.Image); err != nil {
			stopServiceInstances(ctx, client, instances)
			return nil, fmt.Errorf("service %q: pull image: %w", name, err)
		}

		body, err := buildServiceRequest(name, network, spec)
		if err != nil {
			stopServiceInstances(ctx, client, instances)
			return nil, fmt.Errorf("service %q: %w", name, err)
		}

		containerName := "whiteagent-svc-" + name

		// Remove stale container with same name (best-effort).
		if old, _ := client.containerList(ctx, labelService+"="+name); len(old) > 0 {
			for _, c := range old {
				_ = client.containerStop(ctx, c.ID, 10)
				_ = client.containerRemove(ctx, c.ID, true)
			}
		}

		id, err := client.containerCreate(ctx, containerName, body)
		if err != nil {
			stopServiceInstances(ctx, client, instances)
			return nil, fmt.Errorf("service %q: create: %w", name, err)
		}

		if err := client.containerStart(ctx, id); err != nil {
			_ = client.containerRemove(ctx, id, true)
			stopServiceInstances(ctx, client, instances)
			return nil, fmt.Errorf("service %q: start: %w", name, err)
		}

		instances = append(instances, serviceInstance{name: name, containerID: id})
		slog.Info("sandbox: service started", "service", name, "container", id[:12])

		if spec.Healthcheck != nil {
			slog.Info("sandbox: waiting for service healthcheck", "service", name, "interval", spec.Healthcheck.Interval, "retries", spec.Healthcheck.Retries)
			if err := waitHealthy(ctx, client, id, spec.Healthcheck); err != nil {
				stopServiceInstances(ctx, client, instances)
				return nil, fmt.Errorf("service %q: healthcheck: %w", name, err)
			}
		}
	}

	return instances, nil
}

func buildServiceRequest(name, network string, spec serviceSpec) (createContainerRequest, error) {
	var env []string
	for k, v := range spec.Environment {
		env = append(env, k+"="+v)
	}

	hc := hostConfig{
		SecurityOpt: spec.SecurityOpt,
		Tmpfs:       parseTmpfs(spec.Tmpfs),
	}

	if spec.ShmSize != "" {
		size, err := parseSize(spec.ShmSize)
		if err != nil {
			return createContainerRequest{}, fmt.Errorf("shm_size: %w", err)
		}
		hc.ShmSize = size
	}
	if spec.MemLimit != "" {
		size, err := parseSize(spec.MemLimit)
		if err != nil {
			return createContainerRequest{}, fmt.Errorf("mem_limit: %w", err)
		}
		hc.Memory = size
	}
	if spec.MemReservation != "" {
		size, err := parseSize(spec.MemReservation)
		if err != nil {
			return createContainerRequest{}, fmt.Errorf("mem_reservation: %w", err)
		}
		hc.MemoryReservation = size
	}
	if spec.CPUs > 0 {
		hc.NanoCpus = int64(spec.CPUs * 1e9)
	}
	if spec.Init {
		t := true
		hc.Init = &t
	}
	if spec.Restart != "" {
		hc.RestartPolicy = &restartPolicy{Name: mapRestartPolicy(spec.Restart)}
	}

	body := createContainerRequest{
		Image: spec.Image,
		User:  spec.User,
		Env:   env,
		Labels: map[string]string{
			labelManaged: "true",
			labelService: name,
		},
		HostConfig: hc,
		NetworkingConfig: &networkingConfig{
			EndpointsConfig: map[string]endpointSettings{
				network: {Aliases: []string{name}},
			},
		},
	}

	if spec.Healthcheck != nil {
		body.Healthcheck = &containerHealthcheck{
			Test:     spec.Healthcheck.Test,
			Interval: spec.Healthcheck.Interval.Nanoseconds(),
			Timeout:  spec.Healthcheck.Timeout.Nanoseconds(),
			Retries:  spec.Healthcheck.Retries,
		}
	}

	return body, nil
}

func mapRestartPolicy(s string) string {
	switch s {
	case "always":
		return "always"
	case "unless-stopped":
		return "unless-stopped"
	case "on-failure":
		return "on-failure"
	default:
		return "no"
	}
}

func waitHealthy(ctx context.Context, client *dockerClient, containerID string, hc *serviceHealthcheck) error {
	interval := hc.Interval
	if interval == 0 {
		interval = 10 * time.Second
	}
	timeout := hc.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	retries := hc.Retries
	if retries == 0 {
		retries = 3
	}

	// Total wait = retries * (interval + timeout) with some margin.
	maxWait := time.Duration(retries+2) * (interval + timeout)
	deadline, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	for attempt := 0; ; attempt++ {
		select {
		case <-deadline.Done():
			return fmt.Errorf("timed out waiting for healthy status after %d attempts", attempt)
		case <-time.After(interval):
		}

		resp, err := client.containerInspect(deadline, containerID)
		if err != nil {
			continue
		}
		if resp.State.Status == "running" && resp.State.Health != nil && resp.State.Health.Status == "healthy" {
			slog.Info("sandbox: service healthy", "container", containerID[:12])
			return nil
		}
		if resp.State.Status != "running" {
			return fmt.Errorf("container stopped unexpectedly (status: %s)", resp.State.Status)
		}
	}
}

func stopServiceInstances(ctx context.Context, client *dockerClient, instances []serviceInstance) {
	for i := len(instances) - 1; i >= 0; i-- {
		inst := instances[i]
		slog.Info("sandbox: stopping service", "service", inst.name)
		_ = client.containerStop(ctx, inst.containerID, 10)
		_ = client.containerRemove(ctx, inst.containerID, true)
	}
}
