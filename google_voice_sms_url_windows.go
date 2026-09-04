//go:build windows

package main

import (
	"net/url"
	"strconv"
	"strings"
)

// Preserve the selected Google account index when converting another Google
// Voice surface to Messages. The independent SMS profile normally starts at
// u/0, but this helper remains useful for tests and multi-account sessions.
func googleVoiceMessagesURLFromPage(page string) string {
	const fallback = "https://voice.google.com/u/0/messages"
	u, err := url.Parse(strings.TrimSpace(page))
	if err != nil || !strings.EqualFold(u.Hostname(), "voice.google.com") {
		return fallback
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "u" {
		if _, err := strconv.Atoi(parts[1]); err == nil {
			return "https://voice.google.com/u/" + parts[1] + "/messages"
		}
	}
	return fallback
}
