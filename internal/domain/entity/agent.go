package entity

import (
	_ "embed"
	"time"
)

// Agent is an AI agent configuration belonging to a tenant.
type Agent struct {
	ID           AgentID
	TenantID     TenantID
	Name         string
	Instructions string
	CreatedAt    time.Time
}

//go:embed agent_instructions.tmpl
var defaultAgentInstructions string

// DefaultAgentInstructions returns the built-in baseline persona text
// used when creating a new agent without custom instructions.
func DefaultAgentInstructions() string {
	return defaultAgentInstructions
}
