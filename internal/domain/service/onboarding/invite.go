package onboarding

// Invite code CRUD operations (CreateInviteCode, ListInviteCodes, RevokeInviteCode)
// are performed directly via the StorePlugin by CLI commands (internal/app/cli/invite.go).
// No service-level wrapper is needed since the CLI operates as an administrative tool
// that interacts with the store layer without additional business logic.
