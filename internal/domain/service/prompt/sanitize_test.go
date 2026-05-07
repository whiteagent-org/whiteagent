package prompt

import "testing"

func TestStripContextBlocksSelfClosing(t *testing.T) {
	input := `<wa_msg_context msg_id="msg-1" ts="2026-03-14T10:00:00Z"/> Hello`
	got := StripContextBlocks(input)
	want := "Hello"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripContextBlocksWithBody(t *testing.T) {
	input := `<wa_msg_context msg_id="msg-1" ts="2026-03-14T10:00:00Z">
<attachment idx="0"><kind>photo</kind></attachment>
</wa_msg_context>
Hello`
	got := StripContextBlocks(input)
	want := "Hello"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripContextBlocksNoTag(t *testing.T) {
	input := "Just a normal message"
	got := StripContextBlocks(input)
	if got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

func TestStripContextBlocksMultiple(t *testing.T) {
	input := `<wa_msg_context msg_id="a"/> First part <wa_msg_context msg_id="b"/> Second part`
	got := StripContextBlocks(input)
	want := "First part Second part"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripContextBlocksInMiddle(t *testing.T) {
	input := `Before <wa_msg_context msg_id="x"/> After`
	got := StripContextBlocks(input)
	want := "Before After"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripContextBlocksIgnoresGenericContext(t *testing.T) {
	input := `<context msg_id="msg-1"/> Hello`
	got := StripContextBlocks(input)
	if got != input {
		t.Errorf("generic <context> should not be stripped, got %q", got)
	}
}

func TestStripContextBlocksUnclosedOpenerAtStart(t *testing.T) {
	input := `<wa_msg_context msg_id="019e0017-63c2-7522-8f52-8448b9476b04" ts="2026-05-07T01:39:53Z" reply_to="019e0015-5d3d-7506-97ab-407acccbf11f">

Done! I've marked 4 of the 2022 tasks as completed:`
	got := StripContextBlocks(input)
	want := "Done! I've marked 4 of the 2022 tasks as completed:"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripContextBlocksUnclosedOpenerInMiddle(t *testing.T) {
	input := `Before <wa_msg_context msg_id="x" ts="2026-05-07T01:39:53Z"> After`
	got := StripContextBlocks(input)
	want := "Before After"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripContextBlocksMixedSelfCloseAndUnclosedOpener(t *testing.T) {
	input := `<wa_msg_context msg_id="a"/> hello <wa_msg_context msg_id="b"> world`
	got := StripContextBlocks(input)
	want := "hello world"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripContextBlocksPairedSurvivesSecondPass(t *testing.T) {
	// Ensures the bare-opener pass doesn't eat the open half of a valid pair —
	// the paired-block pass must consume it first.
	input := `<wa_msg_context msg_id="a"><attachment idx="0"/></wa_msg_context>Hello`
	got := StripContextBlocks(input)
	want := "Hello"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripThinkingBlocks(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "think block",
			input: "<think>internal reasoning here</think>Hello user!",
			want:  "Hello user!",
		},
		{
			name:  "thinking block",
			input: "<thinking>I need to figure this out</thinking>The answer is 42.",
			want:  "The answer is 42.",
		},
		{
			name:  "reasoning block",
			input: "<reasoning>step 1, step 2</reasoning>Here is my response.",
			want:  "Here is my response.",
		},
		{
			name:  "multiline content",
			input: "<think>\nLine 1\nLine 2\nLine 3\n</think>Result",
			want:  "Result",
		},
		{
			name:  "mixed content and thinking",
			input: "Before <think>internal thought</think> After",
			want:  "Before After",
		},
		{
			name:  "no tags passthrough",
			input: "Just a normal response with no tags.",
			want:  "Just a normal response with no tags.",
		},
		{
			name:  "self-closing tag",
			input: "<think/>Hello",
			want:  "Hello",
		},
		{
			name:  "tag with attributes",
			input: `<thinking type="internal">hidden</thinking>Visible`,
			want:  "Visible",
		},
		{
			name:  "self-closing with attributes",
			input: `<reasoning type="cot" />Hello`,
			want:  "Hello",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripThinkingBlocks(tt.input)
			if got != tt.want {
				t.Errorf("StripThinkingBlocks() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strips both context and thinking blocks",
			input: `<wa_msg_context msg_id="1">data</wa_msg_context><think>reasoning</think>Clean response`,
			want:  "Clean response",
		},
		{
			name:  "trims whitespace",
			input: "  \n Hello \n  ",
			want:  "Hello",
		},
		{
			name:  "composes all sanitizers",
			input: "<think>thought</think>  <wa_msg_context id=\"x\"/>  Answer  ",
			want:  "Answer",
		},
		{
			name:  "passthrough unchanged",
			input: "Normal response",
			want:  "Normal response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeResponse(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}
