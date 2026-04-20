package port

import "fmt"

// Scope identifies the directory tree within ScopedFS.
type Scope int

const (
	ScopeUser    Scope = iota // Per-user workspace directories
	ScopeTenant               // Per-tenant shared directories
	ScopeMessage              // Ephemeral message attachment directories
	ScopeCron                 // Cron job attachment directories
)

// String returns the human-readable scope name.
func (s Scope) String() string {
	switch s {
	case ScopeUser:
		return "user"
	case ScopeTenant:
		return "tenant"
	case ScopeMessage:
		return "message"
	case ScopeCron:
		return "cron"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ScopedFS manages scope-aware directories keyed by globally unique IDs.
// Each scope maps to a separate subdirectory tree under the base data directory.
type ScopedFS interface {
	BaseDir() string
	EnsureDir(scope Scope, id string) (dir string, err error)
	GetDir(scope Scope, id string) (dir string, err error)
	Cleanup(scope Scope, id string) error
}

// ScopedFSAware is an optional interface for plugins that need a ScopedFS reference.
// The runtime type-asserts and injects after Init, before Start.
type ScopedFSAware interface {
	SetScopedFS(ScopedFS)
}
