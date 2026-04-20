package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadStateMissingDir(t *testing.T) {
	state, err := readState("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != nil {
		t.Errorf("expected nil state for missing dir, got %+v", state)
	}
}

func TestWriteReadStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	want := SkillState{
		Source:   "global",
		Hash:     "sha256:abc123",
		SyncedAt: "2026-03-23T10:00:00Z",
	}
	if err := writeState(skillDir, want); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	got, err := readState(skillDir)
	if err != nil {
		t.Fatalf("readState: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil state")
	}
	if got.Source != want.Source {
		t.Errorf("Source = %q, want %q", got.Source, want.Source)
	}
	if got.Hash != want.Hash {
		t.Errorf("Hash = %q, want %q", got.Hash, want.Hash)
	}
	if got.SyncedAt != want.SyncedAt {
		t.Errorf("SyncedAt = %q, want %q", got.SyncedAt, want.SyncedAt)
	}
}

func TestReadStateValidFile(t *testing.T) {
	dir := t.TempDir()
	stateContent := "source: global\nhash: sha256:def456\nsynced_at: \"2026-03-23T12:00:00Z\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".state"), []byte(stateContent), 0o644); err != nil {
		t.Fatalf("write .state: %v", err)
	}

	got, err := readState(dir)
	if err != nil {
		t.Fatalf("readState: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil state")
	}
	if got.Source != "global" {
		t.Errorf("Source = %q, want global", got.Source)
	}
	if got.Hash != "sha256:def456" {
		t.Errorf("Hash = %q, want sha256:def456", got.Hash)
	}
	if got.SyncedAt != "2026-03-23T12:00:00Z" {
		t.Errorf("SyncedAt = %q, want 2026-03-23T12:00:00Z", got.SyncedAt)
	}
}
