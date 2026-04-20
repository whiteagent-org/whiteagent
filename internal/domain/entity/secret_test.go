package entity

import (
	"testing"
	"time"
)

func TestSecretIDTypedID(t *testing.T) {
	id := SecretID("sec-123")
	if id.String() != "sec-123" {
		t.Errorf("String() = %q, want sec-123", id.String())
	}
	if id.IsEmpty() {
		t.Error("IsEmpty() should be false for non-empty ID")
	}

	empty := SecretID("")
	if !empty.IsEmpty() {
		t.Error("IsEmpty() should be true for empty ID")
	}
}

func TestSecretScopeConstants(t *testing.T) {
	if string(SecretScopeTenant) != "tenant" {
		t.Errorf("SecretScopeTenant = %q, want tenant", SecretScopeTenant)
	}
	if string(SecretScopeUser) != "user" {
		t.Errorf("SecretScopeUser = %q, want user", SecretScopeUser)
	}
}

func TestSecretStructFields(t *testing.T) {
	now := time.Now()
	s := Secret{
		ID:             SecretID("sec-1"),
		Key:            "API_KEY",
		EncryptedValue: []byte("encrypted"),
		Scope:          SecretScopeTenant,
		TenantID:       TenantID("t-1"),
		UserID:         UserID("u-1"),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if s.ID != "sec-1" {
		t.Errorf("ID = %q", s.ID)
	}
	if s.Key != "API_KEY" {
		t.Errorf("Key = %q", s.Key)
	}
	if string(s.EncryptedValue) != "encrypted" {
		t.Errorf("EncryptedValue unexpected")
	}
	if s.Scope != SecretScopeTenant {
		t.Errorf("Scope = %q", s.Scope)
	}
}

func TestSecretTokenStructFields(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	tok := SecretToken{
		TokenID:  "tok-1",
		Keys:     []string{"KEY1", "KEY2"},
		TenantID: TenantID("t-1"),
		UserID:   UserID("u-1"),
		ExpiresAt: exp,
		Used:     false,
	}
	if tok.TokenID != "tok-1" {
		t.Errorf("TokenID = %q", tok.TokenID)
	}
	if len(tok.Keys) != 2 {
		t.Errorf("Keys length = %d", len(tok.Keys))
	}
	if tok.Used {
		t.Error("Used should be false")
	}
}
