// Package yaml provides YAML frontmatter parsing utilities.
package yaml

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// frontmatter holds the recognized fields from YAML frontmatter.
// Unknown fields are silently ignored via KnownFields(false) default.
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ParseFrontmatter extracts name and description from YAML frontmatter
// delimited by --- markers. If no frontmatter is present (no opening ---
// or unclosed ---), empty strings are returned without error.
// Invalid YAML within the delimiters returns an error.
func ParseFrontmatter(content []byte) (name, description string, err error) {
	// Frontmatter must start with "---\n".
	if !bytes.HasPrefix(content, []byte("---\n")) {
		return "", "", nil
	}

	// Find closing "---" delimiter.
	rest := content[4:] // skip opening "---\n"
	idx := bytes.Index(rest, []byte("\n---"))
	if idx < 0 {
		// Unclosed frontmatter — treat as no frontmatter.
		return "", "", nil
	}

	yamlBlock := rest[:idx]
	var fm frontmatter
	if err := yaml.Unmarshal(yamlBlock, &fm); err != nil {
		return "", "", fmt.Errorf("parse frontmatter: %w", err)
	}
	return fm.Name, fm.Description, nil
}
