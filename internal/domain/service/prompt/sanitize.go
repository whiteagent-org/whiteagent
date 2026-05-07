package prompt

import (
	"regexp"
	"strings"
)

var reContextBlock = regexp.MustCompile(
	`<wa_msg_context\s[^>]*/>\s*` +
		`|<wa_msg_context\s[^>]*>[\s\S]*?</wa_msg_context>\s*`,
)

// reContextOpenTag matches a leftover bare opening <wa_msg_context …> tag
// after reContextBlock has already consumed valid self-closing and paired
// forms — anything still matching here must be an unclosed echo from the
// model, since the runtime never emits unclosed openers.
var reContextOpenTag = regexp.MustCompile(`<wa_msg_context\s[^>]*>\s*`)

var reThinkingBlock = regexp.MustCompile(
	`<(?:think|thinking|reasoning)(?:\s[^>]*)?>[\s\S]*?</(?:think|thinking|reasoning)>\s*` +
		`|<(?:think|thinking|reasoning)(?:\s[^>]*)?/>\s*`,
)

// StripContextBlocks removes runtime-injected <wa_msg_context> tags
// that the LLM may echo back in its responses.
func StripContextBlocks(content string) string {
	content = reContextBlock.ReplaceAllString(content, "")
	content = reContextOpenTag.ReplaceAllString(content, "")
	return content
}

// StripThinkingBlocks removes structured reasoning tags (<think>, <thinking>,
// <reasoning>) that some models emit as part of chain-of-thought.
func StripThinkingBlocks(content string) string {
	return reThinkingBlock.ReplaceAllString(content, "")
}

// SanitizeResponse applies all response sanitization: strips context blocks,
// strips thinking blocks, and trims surrounding whitespace.
func SanitizeResponse(content string) string {
	content = StripContextBlocks(content)
	content = StripThinkingBlocks(content)
	return strings.TrimSpace(content)
}
