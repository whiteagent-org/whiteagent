// Package scopedfs implements the port.ScopedFS interface.
// It manages scope-aware directories under a base data directory.
// Each scope (user, tenant, message, cron) has its own subdirectory tree.
package scopedfs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

// Compile-time interface check.
var _ port.ScopedFS = (*FS)(nil)

// scopeDirs maps each scope to its subdirectory name under baseDir.
var scopeDirs = map[port.Scope]string{
	port.ScopeUser:    "users",
	port.ScopeTenant:  "tenants",
	port.ScopeMessage: "messages",
	port.ScopeCron:    "cron",
}

// FS implements port.ScopedFS with scope-aware directory management.
type FS struct {
	baseDir string
}

// New creates a ScopedFS rooted at baseDir.
// Relative paths are converted to absolute so that downstream consumers
// (e.g. Docker bind mounts) receive valid absolute paths.
func New(baseDir string) *FS {
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		abs = baseDir
	}
	return &FS{baseDir: abs}
}

// BaseDir returns the absolute base directory path.
func (fs *FS) BaseDir() string { return fs.baseDir }

// EnsureDir creates and returns the directory for the given scope and ID.
func (fs *FS) EnsureDir(scope port.Scope, id string) (dir string, err error) {
	dir, err = fs.resolve(scope, id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("scopedfs: create dir: %w", err)
	}
	return dir, nil
}

// GetDir returns the directory path for the given scope and ID without creating it.
func (fs *FS) GetDir(scope port.Scope, id string) (dir string, err error) {
	return fs.resolve(scope, id)
}

// Cleanup removes the directory for a specific scope and ID.
// Ignores "not exists" errors.
func (fs *FS) Cleanup(scope port.Scope, id string) error {
	dir, err := fs.resolve(scope, id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("scopedfs: cleanup %s/%s: %w", scope, id, err)
	}
	return nil
}

// EnsureMessageDir creates and returns the 3-level message directory:
// {baseDir}/messages/{tenantID}/{convID}/{msgID}/
func (fs *FS) EnsureMessageDir(tenantID, convID, msgID string) (string, error) {
	dir := filepath.Join(fs.baseDir, "messages", tenantID, convID, msgID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("scopedfs: create message dir: %w", err)
	}
	return dir, nil
}

// GetMessageDir returns the 3-level message directory path without creating it.
func (fs *FS) GetMessageDir(tenantID, convID, msgID string) (string, error) {
	return filepath.Join(fs.baseDir, "messages", tenantID, convID, msgID), nil
}

// GetConversationMessagesDir returns the conversation-level messages directory path
// without creating it: {baseDir}/messages/{tenantID}/{convID}/
func (fs *FS) GetConversationMessagesDir(tenantID, convID string) (string, error) {
	return filepath.Join(fs.baseDir, "messages", tenantID, convID), nil
}

// resolve builds the full path for a scope+id pair.
func (fs *FS) resolve(scope port.Scope, id string) (string, error) {
	subdir, ok := scopeDirs[scope]
	if !ok {
		return "", fmt.Errorf("scopedfs: unknown scope %s", scope)
	}
	return filepath.Join(fs.baseDir, subdir, id), nil
}
