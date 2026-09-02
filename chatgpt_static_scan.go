package main

import (
	"regexp"
	"sort"
	"strings"
)

var chatGPTProtocolRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)https?://[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]{6,220}`),
	regexp.MustCompile(`(?i)wss?://[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]{6,220}`),
	regexp.MustCompile(`(?i)(?:chatgpt|openai)://[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]{3,220}`),
	regexp.MustCompile(`(?i)/(?:backend-api|ces|conversation|conversations|messages|chat|responses|api)(?:/[A-Za-z0-9._~!$&'()*+,;=:@%-]{1,80}){0,8}`),
}

var chatGPTProtocolLiterals = []string{
	"backend-api",
	"conversation_id",
	"conversationId",
	"ipcRenderer",
	"ipcMain",
	"websocket",
	"WebSocket",
	"postMessage",
}

// extractChatGPTProtocolMarkers looks only at application package bytes. It is
// deliberately a static-code scanner: it never opens the user's ChatGPT data
// directory, cookie store, Local Storage, credential vault, or process memory.
// Returned strings are also scrubbed so a URL query or fragment can never
// accidentally expose a build-time secret.
func extractChatGPTProtocolMarkers(data []byte) []string {
	text := string(data)
	seen := map[string]bool{}
	out := make([]string, 0, 24)
	add := func(v string) {
		v = sanitizeChatGPTProtocolMarker(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	for _, re := range chatGPTProtocolRegexes {
		for _, m := range re.FindAllString(text, 80) {
			low := strings.ToLower(m)
			if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "ws://") || strings.HasPrefix(low, "wss://") {
				if !strings.Contains(low, "openai") && !strings.Contains(low, "chatgpt") {
					continue
				}
			}
			add(m)
			if len(out) >= 40 {
				break
			}
		}
		if len(out) >= 40 {
			break
		}
	}
	if len(out) < 40 {
		for _, literal := range chatGPTProtocolLiterals {
			if strings.Contains(text, literal) {
				add("marker: " + literal)
			}
		}
	}
	sort.Strings(out)
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

func sanitizeChatGPTProtocolMarker(v string) string {
	v = strings.TrimSpace(strings.ReplaceAll(v, "\x00", ""))
	if v == "" {
		return ""
	}
	low := strings.ToLower(v)
	for _, forbidden := range []string{"authorization:", "bearer ", "access_token", "refresh_token", "cookie:", "set-cookie:"} {
		if strings.Contains(low, forbidden) {
			return ""
		}
	}
	if i := strings.IndexAny(v, "?#"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimRight(v, ".,;:)]}\"'")
	if len(v) > 200 {
		v = v[:200]
	}
	return strings.TrimSpace(v)
}
