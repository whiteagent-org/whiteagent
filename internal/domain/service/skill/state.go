package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SkillState tracks sync metadata for a skill directory.
type SkillState struct {
	Source   string `yaml:"source"`
	Hash     string `yaml:"hash"`
	SyncedAt string `yaml:"synced_at"`
}

// readState reads the .state file from dir. Returns (nil, nil) if the file
// does not exist (missing directory or missing file).
func readState(dir string) (*SkillState, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".state"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read .state: %w", err)
	}
	var s SkillState
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse .state: %w", err)
	}
	return &s, nil
}

// writeState writes a .state YAML file to dir.
func writeState(dir string, state SkillState) error {
	data, err := yaml.Marshal(&state)
	if err != nil {
		return fmt.Errorf("marshal .state: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, ".state"), data, 0o644)
}
