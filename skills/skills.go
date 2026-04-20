// Package skills embeds global skill assets and extracts them at startup.
package skills

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:*
var FS embed.FS

// Extract removes targetDir and recreates it with the embedded skill assets.
// Go source files (.go) are skipped so that skills.go itself is not extracted.
// Returns the first error encountered; the caller is expected to treat errors as fatal.
func Extract(targetDir string) error {
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("skills: remove %s: %w", targetDir, err)
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("skills: create %s: %w", targetDir, err)
	}

	return fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip Go source files (e.g. skills.go).
		if !d.IsDir() && filepath.Ext(path) == ".go" {
			return nil
		}

		target := filepath.Join(targetDir, path)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := fs.ReadFile(FS, path)
		if err != nil {
			return fmt.Errorf("skills: read embedded %s: %w", path, err)
		}

		return os.WriteFile(target, data, 0o644)
	})
}
