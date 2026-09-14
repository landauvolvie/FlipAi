package main

import (
	"regexp"
	"strings"
)

// Some provider pages put a speaker label in the same block as the answer, so
// the text FlipAi reads begins "Copilot said ...". The label belongs to the
// page, not to the message, and a text message has only one speaker: the
// agent the sender addressed. Strip it.
//
// Only a label at the very start is removed, and only when recognizable text
// follows it, so an answer that genuinely begins by quoting someone survives.
var browserChatReplyLabelRE = regexp.MustCompile(
	`^(?i)(?:microsoft\s+)?(copilot|chatgpt|gpt|claude|gemini|grok|muse|assistant|bot|ai)\s*` +
		`(?:said|says|replied|responded)?\s*[:\-–—]?\s*`)

func stripBrowserChatReplyLabel(reply string) string {
	trimmed := strings.TrimSpace(reply)
	if trimmed == "" {
		return trimmed
	}
	loc := browserChatReplyLabelRE.FindStringIndex(trimmed)
	if loc == nil {
		return trimmed
	}
	rest := strings.TrimSpace(trimmed[loc[1]:])
	// A label with nothing after it was never a label.
	if rest == "" {
		return trimmed
	}
	// "Claude Code is a CLI" starts with a name but is not a speaker label:
	// require that the match actually consumed a said/says/colon/dash form, or
	// that the name stood alone on its own before the answer.
	matched := trimmed[loc[0]:loc[1]]
	if !regexp.MustCompile(`(?i)(said|says|replied|responded|[:\-–—])`).MatchString(matched) {
		return trimmed
	}
	return rest
}
