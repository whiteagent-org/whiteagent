package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestInit(t *testing.T) {
	tests := []struct {
		name    string
		raw     json.RawMessage
		wantErr string // substring expected in error; empty means no error
	}{
		{
			name:    "NilConfig",
			raw:     nil,
			wantErr: "config is required",
		},
		{
			name:    "EmptyConfig",
			raw:     json.RawMessage("{}"),
			wantErr: "config.path must not be empty",
		},
		{
			name:    "MissingPath",
			raw:     json.RawMessage(`{"path":""}`),
			wantErr: "config.path must not be empty",
		},
		{
			name:    "ValidConfig",
			raw:     json.RawMessage(`{"path":":memory:"}`),
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPlugin().(*Plugin)
			err := p.Init(context.Background(), "store.sqlite", tt.raw)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				// Clean up: close the DB opened by Init.
				if stopErr := p.Stop(context.Background()); stopErr != nil {
					t.Fatalf("Stop failed: %v", stopErr)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}
