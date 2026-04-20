package token

import "testing"

func TestCount(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"single char", "a", 1},
		{"four chars", "abcd", 1},
		{"five chars", "abcde", 2},
		{"100 chars", string(make([]byte, 100)), 25},
		{"hello world", "hello world", 3},
		{"unicode", "\u4f60\u597d\u4e16\u754c", 3}, // 12 bytes UTF-8
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Count(tt.input)
			if got != tt.want {
				t.Errorf("Count(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
