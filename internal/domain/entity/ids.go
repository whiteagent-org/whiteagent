package entity

// ---------------------------------------------------------------------------
// Typed ID types
// ---------------------------------------------------------------------------

// TenantID is a distinct string type for tenant identifiers.
// Using a defined type (not alias) so the compiler catches mismatched ID usage.
type TenantID string

func (id TenantID) String() string { return string(id) }
func (id TenantID) IsEmpty() bool  { return id == "" }

// AgentID is a distinct string type for agent identifiers.
type AgentID string

func (id AgentID) String() string { return string(id) }
func (id AgentID) IsEmpty() bool  { return id == "" }

// UserID is a distinct string type for user identifiers.
type UserID string

func (id UserID) String() string { return string(id) }
func (id UserID) IsEmpty() bool  { return id == "" }

// ConversationID is a distinct string type for conversation identifiers.
type ConversationID string

func (id ConversationID) String() string { return string(id) }
func (id ConversationID) IsEmpty() bool  { return id == "" }

// MessageID is a distinct string type for message identifiers.
type MessageID string

func (id MessageID) String() string { return string(id) }
func (id MessageID) IsEmpty() bool  { return id == "" }

// CronEntryID is a distinct string type for cron entry identifiers.
type CronEntryID string

func (id CronEntryID) String() string { return string(id) }
func (id CronEntryID) IsEmpty() bool  { return id == "" }

// CronRunID is a distinct string type for cron run identifiers.
type CronRunID string

func (id CronRunID) String() string { return string(id) }
func (id CronRunID) IsEmpty() bool  { return id == "" }

// ErrorLogEntryID is a distinct string type for error log entry identifiers.
type ErrorLogEntryID string

func (id ErrorLogEntryID) String() string { return string(id) }
func (id ErrorLogEntryID) IsEmpty() bool  { return id == "" }

// ChatID is a distinct string type for chat identifiers (DM and group chats).
type ChatID string

func (id ChatID) String() string { return string(id) }
func (id ChatID) IsEmpty() bool  { return id == "" }

// SecretID is a distinct string type for secret identifiers.
type SecretID string

func (id SecretID) String() string { return string(id) }
func (id SecretID) IsEmpty() bool  { return id == "" }
