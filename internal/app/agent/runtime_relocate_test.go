// Package agent (internal test) verifies the relocateAttachments function
// defined in runtime.go. The function is unexported so this file must be in
// the same package.
package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/entity"
	"github.com/whiteagent-org/whiteagent/internal/infra/scopedfs"
)

// TestRelocateAttachments covers the four key behaviors of relocateAttachments:
//   a. Files are moved from temp paths to {baseDir}/messages/{tenantID}/{convID}/{msgID}/{filename}
//   b. After relocation, attachments[i].Path is set to RELATIVE path: {tenantID}/{convID}/{msgID}/{filename}
//   c. Empty Path attachments are skipped without error
//   d. Target directory is created by EnsureMessageDir (3-level path)
func TestRelocateAttachments(t *testing.T) {
	t.Run("files_moved_to_canonical_3_level_path", func(t *testing.T) {
		base := t.TempDir()
		sfs := scopedfs.New(base)

		// Arrange: create a temp file simulating a downloaded attachment.
		tempDir := t.TempDir()
		srcFile := filepath.Join(tempDir, "photo.jpg")
		if err := os.WriteFile(srcFile, []byte("imgdata"), 0o644); err != nil {
			t.Fatalf("setup: write src file: %v", err)
		}

		tenantID := entity.TenantID("tenant-1")
		convID := entity.ConversationID("conv-1")
		msgID := entity.MessageID("msg-1")

		attachments := []entity.Attachment{
			{ID: "att-1", Kind: "photo", Filename: "photo.jpg", Path: srcFile},
		}

		// Act.
		relocateAttachments(sfs, tenantID, convID, msgID, attachments)

		// Assert: file is at canonical 3-level path.
		wantDest := filepath.Join(base, "messages", "tenant-1", "conv-1", "msg-1", "photo.jpg")
		if _, err := os.Stat(wantDest); err != nil {
			t.Errorf("file not at canonical path %q: %v", wantDest, err)
		}

		// Assert: source file is gone (renamed, not copied).
		if _, err := os.Stat(srcFile); !os.IsNotExist(err) {
			t.Errorf("source file still exists at %q", srcFile)
		}
	})

	t.Run("attachment_path_set_to_relative_path", func(t *testing.T) {
		base := t.TempDir()
		sfs := scopedfs.New(base)

		tempDir := t.TempDir()
		srcFile := filepath.Join(tempDir, "document.pdf")
		if err := os.WriteFile(srcFile, []byte("pdfdata"), 0o644); err != nil {
			t.Fatalf("setup: write src file: %v", err)
		}

		tenantID := entity.TenantID("tenant-2")
		convID := entity.ConversationID("conv-2")
		msgID := entity.MessageID("msg-2")

		attachments := []entity.Attachment{
			{ID: "att-2", Kind: "document", Filename: "document.pdf", Path: srcFile},
		}

		relocateAttachments(sfs, tenantID, convID, msgID, attachments)

		// Assert: path in entity is RELATIVE (not absolute).
		wantRelPath := filepath.Join("tenant-2", "conv-2", "msg-2", "document.pdf")
		if attachments[0].Path != wantRelPath {
			t.Errorf("attachment.Path = %q, want relative %q", attachments[0].Path, wantRelPath)
		}

		// Confirm it is not absolute.
		if filepath.IsAbs(attachments[0].Path) {
			t.Errorf("attachment.Path must be relative, got absolute: %q", attachments[0].Path)
		}
	})

	t.Run("empty_path_attachments_are_skipped", func(t *testing.T) {
		base := t.TempDir()
		sfs := scopedfs.New(base)

		tenantID := entity.TenantID("tenant-3")
		convID := entity.ConversationID("conv-3")
		msgID := entity.MessageID("msg-3")

		attachments := []entity.Attachment{
			{ID: "att-3", Kind: "photo", Filename: "nopath.jpg", Path: ""},
		}

		// Must not panic or error; empty path attachment is left unchanged.
		relocateAttachments(sfs, tenantID, convID, msgID, attachments)

		if attachments[0].Path != "" {
			t.Errorf("empty-path attachment should remain empty, got %q", attachments[0].Path)
		}
	})

	t.Run("target_directory_created_by_ensure_message_dir", func(t *testing.T) {
		base := t.TempDir()
		sfs := scopedfs.New(base)

		tempDir := t.TempDir()
		srcFile := filepath.Join(tempDir, "voice.ogg")
		if err := os.WriteFile(srcFile, []byte("audiodata"), 0o644); err != nil {
			t.Fatalf("setup: write src file: %v", err)
		}

		tenantID := entity.TenantID("tenant-4")
		convID := entity.ConversationID("conv-4")
		msgID := entity.MessageID("msg-4")

		// Verify target dir does NOT exist before the call.
		targetDir := filepath.Join(base, "messages", "tenant-4", "conv-4", "msg-4")
		if _, err := os.Stat(targetDir); !os.IsNotExist(err) {
			t.Fatalf("target dir should not exist before relocation")
		}

		attachments := []entity.Attachment{
			{ID: "att-4", Kind: "voice", Filename: "voice.ogg", Path: srcFile},
		}

		relocateAttachments(sfs, tenantID, convID, msgID, attachments)

		// Assert: target directory was created.
		info, err := os.Stat(targetDir)
		if err != nil {
			t.Fatalf("target directory not created: %v", err)
		}
		if !info.IsDir() {
			t.Fatalf("target path is not a directory: %s", targetDir)
		}
	})

	t.Run("multiple_attachments_all_relocated", func(t *testing.T) {
		base := t.TempDir()
		sfs := scopedfs.New(base)

		tempDir := t.TempDir()

		files := []string{"a.jpg", "b.jpg", "c.jpg"}
		attachments := make([]entity.Attachment, len(files))
		for i, name := range files {
			src := filepath.Join(tempDir, name)
			if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
				t.Fatalf("setup: %v", err)
			}
			attachments[i] = entity.Attachment{ID: name, Kind: "photo", Filename: name, Path: src}
		}

		tenantID := entity.TenantID("tenant-5")
		convID := entity.ConversationID("conv-5")
		msgID := entity.MessageID("msg-5")

		relocateAttachments(sfs, tenantID, convID, msgID, attachments)

		for i, name := range files {
			wantRel := filepath.Join("tenant-5", "conv-5", "msg-5", name)
			if attachments[i].Path != wantRel {
				t.Errorf("attachments[%d].Path = %q, want %q", i, attachments[i].Path, wantRel)
			}
			wantAbs := filepath.Join(base, "messages", "tenant-5", "conv-5", "msg-5", name)
			if _, err := os.Stat(wantAbs); err != nil {
				t.Errorf("file %q not found at canonical path: %v", name, err)
			}
		}
	})
}
