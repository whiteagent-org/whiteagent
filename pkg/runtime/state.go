// Package runtime provides the atomic state machine for the whiteagent process lifecycle.
// Importable from pkg/ so both cmd/ and internal/ can reference it without circular deps.
package runtime

import "sync/atomic"

// State represents the lifecycle state of the whiteagent runtime.
type State int32

const (
	// StateStarting is the zero-value initial state. The runtime is booting and
	// not yet ready to accept traffic.
	StateStarting State = iota

	// StateStopped indicates the runtime has completed shutdown.
	StateStopped

	// StateReady indicates the runtime is fully initialized and serving traffic.
	StateReady

	// StateDraining indicates the runtime is rejecting new work and finishing
	// in-flight requests before stopping.
	StateDraining
)

// String returns a human-readable label for the state.
func (s State) String() string {
	switch s {
	case StateStarting:
		return "starting"
	case StateStopped:
		return "stopped"
	case StateReady:
		return "ready"
	case StateDraining:
		return "draining"
	default:
		return "unknown"
	}
}

// RuntimeState holds the current lifecycle state as an atomic int32.
// Safe for concurrent reads and writes from multiple goroutines.
type RuntimeState struct {
	state atomic.Int32
}

// NewRuntimeState creates a RuntimeState initialized to StateStarting.
func NewRuntimeState() *RuntimeState {
	return &RuntimeState{} // zero value of atomic.Int32 is 0, which is StateStarting
}

// Get returns the current state.
func (rs *RuntimeState) Get() State {
	return State(rs.state.Load())
}

// Set atomically stores the new state.
func (rs *RuntimeState) Set(s State) {
	rs.state.Store(int32(s))
}

// IsReady returns true when the runtime is in the Ready state.
func (rs *RuntimeState) IsReady() bool {
	return rs.Get() == StateReady
}

// IsDraining returns true when the runtime is in the Draining state.
func (rs *RuntimeState) IsDraining() bool {
	return rs.Get() == StateDraining
}
