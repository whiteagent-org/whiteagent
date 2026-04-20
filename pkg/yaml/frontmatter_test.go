package yaml

import "testing"

func TestParseFrontmatterValid(t *testing.T) {
	content := []byte("---\nname: Foo\ndescription: Bar\n---\n# content")
	name, desc, err := ParseFrontmatter(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "Foo" {
		t.Errorf("name = %q, want Foo", name)
	}
	if desc != "Bar" {
		t.Errorf("description = %q, want Bar", desc)
	}
}

func TestParseFrontmatterNoFrontmatter(t *testing.T) {
	content := []byte("# no frontmatter")
	name, desc, err := ParseFrontmatter(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
	if desc != "" {
		t.Errorf("description = %q, want empty", desc)
	}
}

func TestParseFrontmatterPartial(t *testing.T) {
	content := []byte("---\nname: Foo\n---\n# partial")
	name, desc, err := ParseFrontmatter(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "Foo" {
		t.Errorf("name = %q, want Foo", name)
	}
	if desc != "" {
		t.Errorf("description = %q, want empty", desc)
	}
}

func TestParseFrontmatterInvalidYAML(t *testing.T) {
	content := []byte("---\ninvalid: yaml: {{{\n---")
	_, _, err := ParseFrontmatter(content)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestParseFrontmatterUnclosed(t *testing.T) {
	content := []byte("---\nname: Foo\n# no closing delimiter")
	name, desc, err := ParseFrontmatter(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
	if desc != "" {
		t.Errorf("description = %q, want empty", desc)
	}
}

func TestParseFrontmatterUnknownFields(t *testing.T) {
	content := []byte("---\nname: Test\nunknown_field: whatever\ntags: [a, b]\n---\n# body")
	name, desc, err := ParseFrontmatter(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "Test" {
		t.Errorf("name = %q, want Test", name)
	}
	if desc != "" {
		t.Errorf("description = %q, want empty", desc)
	}
}
