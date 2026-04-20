package port

import (
	"fmt"
	"time"
)

// ProviderError is returned by LLM plugins on non-200 HTTP responses.
// It carries the HTTP status code, an optional Retry-After duration (extracted
// from 429 responses), and the response body snippet as Message.
type ProviderError struct {
	StatusCode int
	RetryAfter time.Duration
	Message    string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider error %d: %s", e.StatusCode, e.Message)
}
