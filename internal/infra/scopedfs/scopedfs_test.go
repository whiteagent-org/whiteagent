package scopedfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

func TestEnsureDir_AllScopes(t *testing.T) {
	base := t.TempDir()
	fs := New(base)

	tests := []struct {
		scope  port.Scope
		id     string
		subdir string
	}{
		{port.ScopeUser, "user-001", "users"},
		{port.ScopeTenant, "tenant-002", "tenants"},
		{port.ScopeMessage, "msg-003", "messages"},
		{port.ScopeCron, "cron-004", "cron"},
	}

	for _, tt := range tests {
		t.Run(tt.scope.String(), func(t *testing.T) {
			dir, err := fs.EnsureDir(tt.scope, tt.id)
			if err != nil {
				t.Fatalf("EnsureDir(%s, %s): %v", tt.scope, tt.id, err)
			}

			want := filepath.Join(base, tt.subdir, tt.id)
			if dir != want {
				t.Errorf("got %q, want %q", dir, want)
			}

			info, err := os.Stat(dir)
			if err != nil {
				t.Fatalf("directory not created: %v", err)
			}
			if !info.IsDir() {
				t.Fatalf("not a directory: %s", dir)
			}

			// Idempotent: second call returns same path.
			dir2, err := fs.EnsureDir(tt.scope, tt.id)
			if err != nil {
				t.Fatalf("EnsureDir second call: %v", err)
			}
			if dir2 != dir {
				t.Errorf("second call got %q, want %q", dir2, dir)
			}
		})
	}
}

func TestGetDir_AllScopes(t *testing.T) {
	base := t.TempDir()
	fs := New(base)

	tests := []struct {
		scope  port.Scope
		id     string
		subdir string
	}{
		{port.ScopeUser, "user-100", "users"},
		{port.ScopeTenant, "tenant-200", "tenants"},
		{port.ScopeMessage, "msg-300", "messages"},
		{port.ScopeCron, "cron-400", "cron"},
	}

	for _, tt := range tests {
		t.Run(tt.scope.String(), func(t *testing.T) {
			dir, err := fs.GetDir(tt.scope, tt.id)
			if err != nil {
				t.Fatalf("GetDir(%s, %s): %v", tt.scope, tt.id, err)
			}

			want := filepath.Join(base, tt.subdir, tt.id)
			if dir != want {
				t.Errorf("got %q, want %q", dir, want)
			}

			// GetDir does NOT create the directory.
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("expected directory to not exist, got err=%v", err)
			}
		})
	}
}

func TestCleanup(t *testing.T) {
	base := t.TempDir()
	fs := New(base)

	scope := port.ScopeMessage
	id := "msg-cleanup-001"

	if _, err := fs.EnsureDir(scope, id); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	if err := fs.Cleanup(scope, id); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	dir := filepath.Join(base, "messages", id)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("directory should be removed, got err=%v", err)
	}

	// Cleanup on non-existent ID is not an error.
	if err := fs.Cleanup(scope, "nonexistent"); err != nil {
		t.Errorf("cleanup nonexistent: %v", err)
	}
}

func TestNew_RelativePathBecomesAbsolute(t *testing.T) {
	fs := New("./data")

	dir, err := fs.GetDir(port.ScopeUser, "uid-001")
	if err != nil {
		t.Fatalf("GetDir: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("expected absolute path, got %q", dir)
	}
}

func TestEnsureMessageDir(t *testing.T) {
	base := t.TempDir()
	fs := New(base)

	dir, err := fs.EnsureMessageDir("t1", "conv1", "msg1")
	if err != nil {
		t.Fatalf("EnsureMessageDir: %v", err)
	}

	want := filepath.Join(base, "messages", "t1", "conv1", "msg1")
	if dir != want {
		t.Errorf("got %q, want %q", dir, want)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("not a directory: %s", dir)
	}

	// Idempotent: second call returns same path.
	dir2, err := fs.EnsureMessageDir("t1", "conv1", "msg1")
	if err != nil {
		t.Fatalf("EnsureMessageDir second call: %v", err)
	}
	if dir2 != dir {
		t.Errorf("second call got %q, want %q", dir2, dir)
	}
}

func TestGetMessageDir(t *testing.T) {
	base := t.TempDir()
	fs := New(base)

	dir, err := fs.GetMessageDir("t1", "conv1", "msg1")
	if err != nil {
		t.Fatalf("GetMessageDir: %v", err)
	}

	want := filepath.Join(base, "messages", "t1", "conv1", "msg1")
	if dir != want {
		t.Errorf("got %q, want %q", dir, want)
	}

	// GetMessageDir does NOT create the directory.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected directory to not exist, got err=%v", err)
	}
}

func TestGetConversationMessagesDir(t *testing.T) {
	base := t.TempDir()
	fs := New(base)

	dir, err := fs.GetConversationMessagesDir("t1", "conv1")
	if err != nil {
		t.Fatalf("GetConversationMessagesDir: %v", err)
	}

	want := filepath.Join(base, "messages", "t1", "conv1")
	if dir != want {
		t.Errorf("got %q, want %q", dir, want)
	}

	// GetConversationMessagesDir does NOT create the directory.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected directory to not exist, got err=%v", err)
	}
}

func TestInvalidScope(t *testing.T) {
	base := t.TempDir()
	fs := New(base)

	invalid := port.Scope(99)

	_, err := fs.EnsureDir(invalid, "id")
	if err == nil {
		t.Fatal("expected error for invalid scope in EnsureDir")
	}

	_, err = fs.GetDir(invalid, "id")
	if err == nil {
		t.Fatal("expected error for invalid scope in GetDir")
	}

	err = fs.Cleanup(invalid, "id")
	if err == nil {
		t.Fatal("expected error for invalid scope in Cleanup")
	}
}
