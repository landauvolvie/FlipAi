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

// stripBrowserChatConversation drops everything up to and including the last
// place the prompt is quoted back, when something follows it.
//
// A provider page does not always expose one message as one element. When the
// only thing FlipAi can match is the conversation itself, the text it reads is
// the whole thread: weeks of older messages, then this turn's prompt, then the
// answer. The page driver trims that in the page; this is the same rule on the
// way out, for a reply that reached here through any other path -- a long-turn
// continuation, say, which reads a state file and not the DOM.
//
// The *last* boundary that still leaves text after it is the one to cut at. The
// last occurrence outright is not: an answer commonly repeats the question, and
// cutting there would leave nothing at all.
func stripBrowserChatConversation(reply, prompt string) string {
	out := strings.TrimSpace(reply)
	needle := strings.TrimSpace(prompt)
	// Too short a prompt matches inside ordinary prose; "ok" is not a boundary.
	if out == "" || len(needle) < 12 {
		return out
	}
	cut := -1
	for at := 0; ; {
		i := strings.Index(out[at:], needle)
		if i < 0 {
			break
		}
		end := at + i + len(needle)
		if strings.TrimSpace(out[end:]) != "" {
			cut = end
		}
		at = at + i + 1
		if at >= len(out) {
			break
		}
	}
	if cut < 0 {
		return out
	}
	tail := strings.TrimSpace(out[cut:])
	// Leading punctuation is what separated the quoted prompt from the answer.
	tail = strings.TrimSpace(strings.TrimLeft(tail, ":-,;\u2013\u2014>|\u00b7\u2022 \t\r\n"))
	if tail == "" {
		return out
	}
	return tail
}

// cleanBrowserChatReply removes what the page added around the message: the
// speaker label in front and the action row behind.
func cleanBrowserChatReply(reply string) string {
	return stripBrowserChatReplyChrome(stripBrowserChatReplyLabel(reply))
}

// cleanBrowserChatReplyForPrompt also drops the conversation in front of the
// answer, which needs the prompt to recognize.
func cleanBrowserChatReplyForPrompt(reply, prompt string) string {
	return cleanBrowserChatReply(stripBrowserChatConversation(reply, prompt))
}
