package util

import (
	"testing"
	"time"
)

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1 KB"},
		{1536, "1 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		got := FormatSize(tt.bytes)
		if got != tt.want {
			t.Errorf("FormatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestFormatTimestampUTC(t *testing.T) {
	ts := time.Date(2026, 3, 14, 10, 30, 0, 0, time.UTC)
	got := FormatTimestampUTC(ts)
	want := "2026-03-14T10:30:00Z"
	if got != want {
		t.Errorf("FormatTimestampUTC = %q, want %q", got, want)
	}

	if got := FormatTimestampUTC(time.Time{}); got != "" {
		t.Errorf("FormatTimestampUTC(zero) = %q, want empty", got)
	}
}
