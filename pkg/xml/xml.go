// Package xml provides a builder for constructing XML elements programmatically.
package xml

import (
	"bytes"
	"fmt"
	"html"
)

// Attr is a key-value pair for XML element attributes.
type Attr struct {
	Key   string
	Value string
}

// Builder constructs XML elements programmatically.
type Builder struct {
	buf bytes.Buffer
}

// NewBuilder creates a new Builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// OpenTag writes an opening tag with optional attributes and a trailing newline.
func (b *Builder) OpenTag(name string, attrs ...Attr) *Builder {
	b.buf.WriteByte('<')
	b.buf.WriteString(name)
	for _, a := range attrs {
		fmt.Fprintf(&b.buf, ` %s="%s"`, a.Key, html.EscapeString(a.Value))
	}
	b.buf.WriteString(">\n")
	return b
}

// SelfCloseTag writes a self-closing tag with optional attributes (no newline).
func (b *Builder) SelfCloseTag(name string, attrs ...Attr) *Builder {
	b.buf.WriteByte('<')
	b.buf.WriteString(name)
	for _, a := range attrs {
		fmt.Fprintf(&b.buf, ` %s="%s"`, a.Key, html.EscapeString(a.Value))
	}
	b.buf.WriteString("/>")
	return b
}

// CloseTag writes a closing tag with a trailing newline.
func (b *Builder) CloseTag(name string) *Builder {
	fmt.Fprintf(&b.buf, "</%s>\n", name)
	return b
}

// Child writes an indented child element with escaped text. Skips if text is empty.
func (b *Builder) Child(name, text string) *Builder {
	if text == "" {
		return b
	}
	fmt.Fprintf(&b.buf, "  <%s>%s</%s>\n", name, html.EscapeString(text), name)
	return b
}

// ChildRaw writes an indented child element without escaping text. Skips if text is empty.
func (b *Builder) ChildRaw(name, text string) *Builder {
	if text == "" {
		return b
	}
	fmt.Fprintf(&b.buf, "  <%s>%s</%s>\n", name, text, name)
	return b
}

// ChildAlways writes an indented child element even if text is empty.
func (b *Builder) ChildAlways(name, text string) *Builder {
	fmt.Fprintf(&b.buf, "  <%s>%s</%s>\n", name, html.EscapeString(text), name)
	return b
}

// String returns the built XML string.
func (b *Builder) String() string {
	return b.buf.String()
}
