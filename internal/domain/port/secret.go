package port

import "github.com/whiteagent-org/whiteagent/internal/domain/service/secret"

// SecretAware is an optional interface for plugins that need a SecretService reference.
// The runtime type-asserts and injects after Init, before Start.
type SecretAware interface {
	SetSecretService(svc secret.SecretService)
}
