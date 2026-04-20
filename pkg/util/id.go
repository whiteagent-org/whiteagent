// Package util provides shared utility functions for ID generation and invite codes.
package util

import (
	"crypto/rand"
	"fmt"

	"github.com/google/uuid"
)

// NewID generates a time-ordered UUIDv7 string suitable for primary keys.
func NewID() string {
	return uuid.Must(uuid.NewV7()).String()
}

// NewRandomID generates a random UUIDv4 string.
func NewRandomID() string {
	return uuid.Must(uuid.NewRandom()).String()
}

// inviteCharset is the set of characters used for invite code generation.
const inviteCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// NewInviteCode generates an invite code in XXXX-XXXX format
// (two groups of 4 uppercase alphanumeric characters separated by a dash).
// Uses crypto/rand for secure randomness.
func NewInviteCode() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("NewInviteCode: %w", err)
	}
	code := make([]byte, 9) // 4 + 1 dash + 4
	for i := 0; i < 4; i++ {
		code[i] = inviteCharset[int(buf[i])%len(inviteCharset)]
	}
	code[4] = '-'
	for i := 0; i < 4; i++ {
		code[5+i] = inviteCharset[int(buf[4+i])%len(inviteCharset)]
	}
	return string(code), nil
}
