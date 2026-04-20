package docker

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildFrame creates a Docker multiplexed stream frame.
// streamType: 1=stdout, 2=stderr.
func buildFrame(streamType byte, data []byte) []byte {
	header := make([]byte, 8)
	header[0] = streamType
	binary.BigEndian.PutUint32(header[4:8], uint32(len(data)))
	return append(header, data...)
}

func TestDemuxStream_StdoutOnly(t *testing.T) {
	frame := buildFrame(1, []byte("hello stdout"))
	stdout, stderr, err := demuxStream(bytes.NewReader(frame), 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "hello stdout" {
		t.Fatalf("stdout = %q, want %q", stdout, "hello stdout")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestDemuxStream_StderrOnly(t *testing.T) {
	frame := buildFrame(2, []byte("hello stderr"))
	stdout, stderr, err := demuxStream(bytes.NewReader(frame), 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "hello stderr" {
		t.Fatalf("stderr = %q, want %q", stderr, "hello stderr")
	}
}

func TestDemuxStream_Interleaved(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(buildFrame(1, []byte("out1")))
	buf.Write(buildFrame(2, []byte("err1")))
	buf.Write(buildFrame(1, []byte("out2")))

	stdout, stderr, err := demuxStream(&buf, 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "out1out2" {
		t.Fatalf("stdout = %q, want %q", stdout, "out1out2")
	}
	if stderr != "err1" {
		t.Fatalf("stderr = %q, want %q", stderr, "err1")
	}
}

func TestDemuxStream_Truncation(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(buildFrame(1, []byte("12345"))) // 5 bytes
	buf.Write(buildFrame(2, []byte("67890"))) // 5 bytes — only 3 fit (maxBytes=8)
	buf.Write(buildFrame(1, []byte("extra"))) // should not be read

	stdout, stderr, err := demuxStream(&buf, 8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "12345" {
		t.Fatalf("stdout = %q, want %q", stdout, "12345")
	}
	if stderr != "678" {
		t.Fatalf("stderr = %q, want %q", stderr, "678")
	}
}

func TestDemuxStream_Empty(t *testing.T) {
	stdout, stderr, err := demuxStream(bytes.NewReader(nil), 1024)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("expected empty, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestDemuxStream_MalformedHeader(t *testing.T) {
	// Only 4 bytes — short header.
	_, _, err := demuxStream(bytes.NewReader([]byte{1, 0, 0, 0}), 1024)
	if err == nil {
		t.Fatal("expected error for short header")
	}
}

func TestConfigDefaults(t *testing.T) {
	var c dockerConfig
	c.applyDefaults()

	if c.SocketPath != "/var/run/docker.sock" {
		t.Fatalf("SocketPath = %q, want /var/run/docker.sock", c.SocketPath)
	}
	if c.IdleTimeout != "15m" {
		t.Fatalf("IdleTimeout = %q, want 15m", c.IdleTimeout)
	}
	if c.ExecTimeout != "5m" {
		t.Fatalf("ExecTimeout = %q, want 5m", c.ExecTimeout)
	}
	if c.Resources.CPUCores != 0.5 {
		t.Fatalf("CPUCores = %v, want 0.5", c.Resources.CPUCores)
	}
	if c.Resources.MemoryMB != 256 {
		t.Fatalf("MemoryMB = %d, want 256", c.Resources.MemoryMB)
	}
	if c.Resources.PidsLimit != 100 {
		t.Fatalf("PidsLimit = %d, want 100", c.Resources.PidsLimit)
	}
	if c.Resources.TmpfsMB != 64 {
		t.Fatalf("TmpfsMB = %d, want 64", c.Resources.TmpfsMB)
	}
	if c.AllowNetwork == nil || *c.AllowNetwork != true {
		t.Fatal("AllowNetwork should default to true")
	}
	if c.MaxOutputMB != 1 {
		t.Fatalf("MaxOutputMB = %d, want 1", c.MaxOutputMB)
	}
	if c.StopTimeout != "10s" {
		t.Fatalf("StopTimeout = %q, want 10s", c.StopTimeout)
	}
}

func TestConfigDefaultsDockerHostUnixPrefix(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")

	var c dockerConfig
	c.applyDefaults()

	if c.SocketPath != "/var/run/docker.sock" {
		t.Fatalf("SocketPath = %q, want /var/run/docker.sock (unix:// prefix should be stripped)", c.SocketPath)
	}
}

func TestConfigValidate_Valid(t *testing.T) {
	c := dockerConfig{
		SocketPath:  "/var/run/docker.sock",
		Image:       "alpine:latest",
		IdleTimeout: "15m",
		ExecTimeout: "5m",
		StopTimeout: "10s",
		MaxOutputMB: 1,
		Resources: resourceConfig{
			CPUCores:  0.5,
			MemoryMB:  256,
			PidsLimit: 100,
			TmpfsMB:   64,
		},
	}
	if err := c.validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestConfigValidate_Errors(t *testing.T) {
	c := dockerConfig{
		SocketPath:  "",
		Image:       "",
		IdleTimeout: "not-a-duration",
		ExecTimeout: "also-bad",
		StopTimeout: "nope",
		Resources: resourceConfig{
			CPUCores:  -1,
			MemoryMB:  0,
			PidsLimit: -5,
			TmpfsMB:   0,
		},
	}
	err := c.validate()
	if err == nil {
		t.Fatal("expected validation errors")
	}
	// Should contain multiple errors joined.
	s := err.Error()
	for _, want := range []string{"socket_path", "image", "idle_timeout", "exec_timeout", "stop_timeout", "cpu_cores", "memory_mb", "pids_limit", "tmpfs_mb"} {
		if !contains(s, want) {
			t.Errorf("expected error to mention %q, got: %s", want, s)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
