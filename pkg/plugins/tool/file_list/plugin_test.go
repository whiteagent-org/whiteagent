package file_list

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whiteagent-org/whiteagent/internal/domain/port"
)

func stubTC() port.ToolContext { return port.ToolContext{} }

func TestExecute_ListDirectory(t *testing.T) {
	dir := t.TempDir()
	// Create a file and a subdirectory.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}

	p := &Plugin{}
	if err := p.Init(context.Background(), "tool.file_list", nil); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]string{"path": dir})
	result, err := p.Execute(context.Background(), stubTC(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "readme.txt (file)") {
		t.Fatalf("expected file entry, got: %s", result)
	}
	if !strings.Contains(result, "subdir/ (directory)") {
		t.Fatalf("expected directory entry, got: %s", result)
	}
}

func TestExecute_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	p := &Plugin{}
	if err := p.Init(context.Background(), "tool.file_list", nil); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]string{"path": dir})
	result, err := p.Execute(context.Background(), stubTC(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "empty") {
		t.Fatalf("expected empty note, got: %s", result)
	}
}

func TestExecute_ErrorOnFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(fp, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	p := &Plugin{}
	if err := p.Init(context.Background(), "tool.file_list", nil); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]string{"path": fp})
	_, err := p.Execute(context.Background(), stubTC(), args)
	if err == nil {
		t.Fatal("expected error for file path")
	}
	if !strings.Contains(err.Error(), "file_read") {
		t.Fatalf("error should suggest file_read, got: %v", err)
	}
}

func TestExecute_ErrorOnNonexistent(t *testing.T) {
	p := &Plugin{}
	if err := p.Init(context.Background(), "tool.file_list", nil); err != nil {
		t.Fatal(err)
	}

	args, _ := json.Marshal(map[string]string{"path": "/nonexistent/dir"})
	_, err := p.Execute(context.Background(), stubTC(), args)
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}
