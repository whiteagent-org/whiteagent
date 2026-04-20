package secret

import (
	"context"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
)

// TestSecretServiceInterfaceCompiles is a compile-time check that the
// SecretService interface has the expected method signatures.
func TestSecretServiceInterfaceCompiles(t *testing.T) {
	var _ func(SecretService) = func(svc SecretService) {
		ctx := context.Background()
		tid := entity.TenantID("t")
		uid := entity.UserID("u")

		_ = svc.Set(ctx, tid, uid, "key", []byte("plaintext"), entity.SecretModeValue)
		_, _ = svc.Get(ctx, tid, uid, "key")
		_, _ = svc.Exists(ctx, tid, uid, "key")
		_, _ = svc.List(ctx, tid, uid)
		_ = svc.Delete(ctx, tid, uid, "key")
		_ = svc.Redact(ctx, "text", tid, uid)
		_, _ = svc.EnvVars(ctx, tid, uid)
		_, _ = svc.RequestEntry(ctx, []string{"k"}, nil, tid, uid, entity.ConversationID("c"), entity.ChatID("chat-1"))
		_, _ = svc.ValidateToken(ctx, "tok-1")
		_ = svc.ConsumeToken(ctx, "tok-1", map[string]SecretSubmission{"k": {Value: []byte("v"), Mode: entity.SecretModeValue}})
	}
}
