package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

func TestSecretSaveAndGet(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")

	s := entity.Secret{
		ID:             entity.SecretID("s1"),
		Key:            "API_KEY",
		EncryptedValue: []byte("encrypted-data"),
		Scope:          entity.SecretScopeUser,
		TenantID:       tid,
		UserID:         entity.UserID("u1"),
	}
	if err := p.SaveSecret(ctx, tid, s); err != nil {
		t.Fatalf("SaveSecret: %v", err)
	}

	got, err := p.GetSecret(ctx, tid, entity.UserID("u1"), "API_KEY")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got == nil {
		t.Fatal("GetSecret returned nil")
	}
	if string(got.EncryptedValue) != "encrypted-data" {
		t.Errorf("EncryptedValue = %q, want %q", got.EncryptedValue, "encrypted-data")
	}
	if got.Scope != entity.SecretScopeUser {
		t.Errorf("Scope = %q, want %q", got.Scope, entity.SecretScopeUser)
	}
}

func TestSecretUpsert(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")

	s := entity.Secret{
		ID:             entity.SecretID("s1"),
		Key:            "API_KEY",
		EncryptedValue: []byte("v1"),
		Scope:          entity.SecretScopeUser,
		TenantID:       tid,
		UserID:         entity.UserID("u1"),
	}
	if err := p.SaveSecret(ctx, tid, s); err != nil {
		t.Fatalf("SaveSecret: %v", err)
	}

	// Upsert with new value
	s.ID = entity.SecretID("s2")
	s.EncryptedValue = []byte("v2")
	if err := p.SaveSecret(ctx, tid, s); err != nil {
		t.Fatalf("SaveSecret upsert: %v", err)
	}

	got, err := p.GetSecret(ctx, tid, entity.UserID("u1"), "API_KEY")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if string(got.EncryptedValue) != "v2" {
		t.Errorf("EncryptedValue after upsert = %q, want %q", got.EncryptedValue, "v2")
	}
}

func TestSecretListBothScopes(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")

	// Tenant-scoped secret (user_id = "")
	if err := p.SaveSecret(ctx, tid, entity.Secret{
		ID: entity.SecretID("s1"), Key: "SHARED", EncryptedValue: []byte("shared"),
		Scope: entity.SecretScopeTenant, TenantID: tid, UserID: entity.UserID(""),
	}); err != nil {
		t.Fatal(err)
	}

	// User-scoped secret
	if err := p.SaveSecret(ctx, tid, entity.Secret{
		ID: entity.SecretID("s2"), Key: "PERSONAL", EncryptedValue: []byte("personal"),
		Scope: entity.SecretScopeUser, TenantID: tid, UserID: entity.UserID("u1"),
	}); err != nil {
		t.Fatal(err)
	}

	// List for user u1 should return both
	secrets, err := p.ListSecrets(ctx, tid, entity.UserID("u1"))
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(secrets) != 2 {
		t.Fatalf("ListSecrets len = %d, want 2", len(secrets))
	}

	keys := map[string]bool{}
	for _, s := range secrets {
		keys[s.Key] = true
	}
	if !keys["SHARED"] || !keys["PERSONAL"] {
		t.Errorf("expected SHARED and PERSONAL keys, got %v", keys)
	}
}

func TestSecretDeleteIdempotent(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()
	tid := entity.TenantID("t1")

	if err := p.SaveSecret(ctx, tid, entity.Secret{
		ID: entity.SecretID("s1"), Key: "KEY", EncryptedValue: []byte("data"),
		Scope: entity.SecretScopeUser, TenantID: tid, UserID: entity.UserID("u1"),
	}); err != nil {
		t.Fatal(err)
	}

	if err := p.DeleteSecret(ctx, tid, entity.UserID("u1"), "KEY"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}

	got, err := p.GetSecret(ctx, tid, entity.UserID("u1"), "KEY")
	if err != nil {
		t.Fatalf("GetSecret after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}

	// Second delete should not error (idempotent)
	if err := p.DeleteSecret(ctx, tid, entity.UserID("u1"), "KEY"); err != nil {
		t.Fatalf("DeleteSecret idempotent: %v", err)
	}
}

func TestSecretTokenSaveGetRoundtrip(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()

	token := entity.SecretToken{
		TokenID:        "tok-123",
		Keys:           []string{"API_KEY", "SECRET"},
		TenantID:       entity.TenantID("t1"),
		UserID:         entity.UserID("u1"),
		ConversationID: entity.ConversationID("conv-1"),
		ChatID:         entity.ChatID("chat-123"),
		ExpiresAt:      time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}
	if err := p.SaveSecretToken(ctx, token); err != nil {
		t.Fatalf("SaveSecretToken: %v", err)
	}

	got, err := p.GetSecretToken(ctx, "tok-123")
	if err != nil {
		t.Fatalf("GetSecretToken: %v", err)
	}
	if got == nil {
		t.Fatal("GetSecretToken returned nil")
	}
	if len(got.Keys) != 2 || got.Keys[0] != "API_KEY" || got.Keys[1] != "SECRET" {
		t.Errorf("Keys = %v, want [API_KEY SECRET]", got.Keys)
	}
	if got.ChatID != "chat-123" {
		t.Errorf("ChatID = %q, want chat-123", got.ChatID)
	}
	if got.ConversationID != "conv-1" {
		t.Errorf("ConversationID = %q, want conv-1", got.ConversationID)
	}
	if got.Used {
		t.Error("expected Used=false")
	}
}

func TestSecretTokenConsumeAtomic(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()

	token := entity.SecretToken{
		TokenID:   "tok-456",
		Keys:      []string{"K"},
		TenantID:  entity.TenantID("t1"),
		UserID:    entity.UserID("u1"),
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}
	if err := p.SaveSecretToken(ctx, token); err != nil {
		t.Fatal(err)
	}

	// First consume succeeds
	if err := p.ConsumeSecretToken(ctx, "tok-456"); err != nil {
		t.Fatalf("ConsumeSecretToken: %v", err)
	}

	// Verify used flag
	got, err := p.GetSecretToken(ctx, "tok-456")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Used {
		t.Error("expected Used=true after consume")
	}

	// Second consume fails
	if err := p.ConsumeSecretToken(ctx, "tok-456"); err == nil {
		t.Error("expected error on second consume")
	}
}

func TestSecretTokenGetNonExistent(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()

	got, err := p.GetSecretToken(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for non-existent token")
	}
}
