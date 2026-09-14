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

// browserChatReplyChrome is the action row a provider renders under a reply.
// It is page furniture -- controls for the person looking at the browser -- and
// it was arriving on the end of text messages. Only a trailing run of these is
// removed, so an answer that happens to end with one of these words survives.
var browserChatReplyChrome = []string{
	"Edit in a page", "Edit in page", "Edit in canvas",
	"Good response", "Bad response", "Copy link", "Copy",
	"Read aloud", "Regenerate", "Share", "Export", "Save",
	"Like", "Dislike", "Retry", "Try again", "More actions",
}

func stripBrowserChatReplyChrome(reply string) string {
	out := strings.TrimSpace(reply)
	for changed := true; changed; {
		changed = false
		for _, chrome := range browserChatReplyChrome {
			if len(out) <= len(chrome) {
				continue
			}
			tail := out[len(out)-len(chrome):]
			if !strings.EqualFold(tail, chrome) {
				continue
			}
			head := out[:len(out)-len(chrome)]
			// A one-word control is also an ordinary English word, so it only
			// counts as furniture when the page set it apart -- its own line, or
			// a separator. Without that rule "you can copy" lost its last word.
			if !strings.Contains(chrome, " ") && !endsWithChromeSeparator(head) {
				continue
			}
			trimmed := strings.TrimRight(strings.TrimSpace(head), " \t\r\n·|•")
			if strings.TrimSpace(trimmed) == "" {
				continue
			}
			out = strings.TrimSpace(trimmed)
			changed = true
		}
	}
	return out
}

// endsWithChromeSeparator reports whether the page visibly separated what
// follows from the message: a line break, or a bullet/pipe separator.
func endsWithChromeSeparator(head string) bool {
	trimmed := strings.TrimRight(head, " \t")
	if trimmed == "" {
		return false
	}
	switch trimmed[len(trimmed)-1] {
	case '\n', '\r', '|':
		return true
	}
	return strings.HasSuffix(trimmed, "·") || strings.HasSuffix(trimmed, "•")
}

// cleanBrowserChatReply removes what the page added around the message: the
// speaker label in front and the action row behind.
func cleanBrowserChatReply(reply string) string {
	return stripBrowserChatReplyChrome(stripBrowserChatReplyLabel(reply))
}
