package main

import (
	"errors"
	"net/mail"
	"regexp"
	"sort"
	"strings"
)

var subjectPhoneRE = regexp.MustCompile(`(?i)(?:\+?1[\s.\-]?)?(?:\([0-9]{3}\)|[0-9]{3})[\s.\-]?[0-9]{3}[\s.\-]?[0-9]{4}`)

func normalizeUSPhone(v string) string {
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) == 11 && s[0] == '1' {
		s = s[1:]
	}
	if len(s) != 10 {
		return ""
	}
	return s
}

func normalizeAllowedPhoneList(raw string) ([]string, error) {
	parts := regexp.MustCompile(`[\r\n,;]+`).Split(raw, -1)
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n := normalizeUSPhone(p)
		if n == "" {
			return nil, errors.New("each allowed SMS sender must be a 10-digit US/Canada phone number (a leading +1 is okay)")
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("add at least one allowed SMS phone number")
	}
	sort.Strings(out)
	return out, nil
}

func allowedPhone(raw, sender string) bool {
	sender = normalizeUSPhone(sender)
	if sender == "" {
		return false
	}
	list, err := normalizeAllowedPhoneList(raw)
	if err != nil {
		return false
	}
	for _, n := range list {
		if n == sender {
			return true
		}
	}
	return false
}

// googleVoiceSender extracts the SMS sender from Google Voice's structured
// @txt.voice.google.com envelope. A typical address looks like:
// 1<google-voice-number>.<sms-sender>.<opaque-token>@txt.voice.google.com
// This is safer than searching the message body for digits because the SMS
// content itself is untrusted and may contain arbitrary phone numbers.
func googleVoiceSender(m GmailMessage, requiredPhrase string) (string, bool) {
	if requiredPhrase != "" && !strings.Contains(strings.ToLower(m.Subject), strings.ToLower(requiredPhrase)) {
		return "", false
	}
	if a, err := mail.ParseAddress(m.From); err == nil {
		addr := strings.ToLower(strings.TrimSpace(a.Address))
		at := strings.LastIndex(addr, "@")
		if at > 0 && addr[at+1:] == "txt.voice.google.com" {
			local := addr[:at]
			fields := strings.Split(local, ".")
			if len(fields) >= 3 {
				if n := normalizeUSPhone(fields[1]); n != "" {
					return n, true
				}
			}
		}
	}

	// Some Google Voice notification variants use voice-noreply@google.com.
	// Only accept that fallback when the sender's full phone number appears in
	// the trusted Subject header after the expected Google Voice phrase.
	if strings.Contains(strings.ToLower(m.From), "voice-noreply@google.com") {
		phrasePos := strings.Index(strings.ToLower(m.Subject), strings.ToLower(requiredPhrase))
		tail := m.Subject
		if phrasePos >= 0 {
			tail = m.Subject[phrasePos+len(requiredPhrase):]
		}
		for _, match := range subjectPhoneRE.FindAllString(tail, -1) {
			if n := normalizeUSPhone(match); n != "" {
				return n, true
			}
		}
	}
	return "", false
}
