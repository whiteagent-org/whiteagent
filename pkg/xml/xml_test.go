package xml

import (
	"strings"
	"testing"
)

func TestBuilderSelfClose(t *testing.T) {
	xb := NewBuilder()
	xb.SelfCloseTag("tag", Attr{"key", "val"})
	got := xb.String()
	want := `<tag key="val"/>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuilderOpenClose(t *testing.T) {
	xb := NewBuilder()
	xb.OpenTag("parent").Child("child", "text").CloseTag("parent")
	got := xb.String()
	if !strings.Contains(got, "<parent>") {
		t.Error("missing opening tag")
	}
	if !strings.Contains(got, "  <child>text</child>") {
		t.Error("missing child element")
	}
	if !strings.Contains(got, "</parent>") {
		t.Error("missing closing tag")
	}
}

func TestBuilderChildSkipsEmpty(t *testing.T) {
	xb := NewBuilder()
	xb.Child("empty", "")
	if xb.String() != "" {
		t.Errorf("expected empty, got %q", xb.String())
	}
}

func TestBuilderChildAlways(t *testing.T) {
	xb := NewBuilder()
	xb.ChildAlways("present", "")
	got := xb.String()
	if !strings.Contains(got, "<present></present>") {
		t.Errorf("expected element, got %q", got)
	}
}

func TestBuilderChildRaw(t *testing.T) {
	xb := NewBuilder()
	xb.ChildRaw("raw", `{"key":"value"}`)
	got := xb.String()
	if !strings.Contains(got, `<raw>{"key":"value"}</raw>`) {
		t.Errorf("expected raw content, got %q", got)
	}
}

func TestBuilderChildRawSkipsEmpty(t *testing.T) {
	xb := NewBuilder()
	xb.ChildRaw("empty", "")
	if xb.String() != "" {
		t.Errorf("expected empty, got %q", xb.String())
	}
}

func TestBuilderEscaping(t *testing.T) {
	xb := NewBuilder()
	xb.OpenTag("tag", Attr{"attr", `<>&"`})
	xb.Child("text", `<>&"`)
	xb.CloseTag("tag")
	got := xb.String()
	if !strings.Contains(got, `attr="&lt;&gt;&amp;&#34;"`) {
		t.Errorf("attribute not escaped properly: %s", got)
	}
	if !strings.Contains(got, "<text>&lt;&gt;&amp;&#34;</text>") {
		t.Errorf("text not escaped properly: %s", got)
	}
}
