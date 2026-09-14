package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// browserPageScriptError turns a DevTools exceptionDetails payload into an
// error that names what actually went wrong in the page.
//
// Every provider used to report a page-script failure as the bare sentence
// "the <provider> page script failed", throwing the exception away. A syntax
// error in a driver script -- a missing separator between two statements, say
// -- then looked exactly like a site redesign, a signed-out session or a
// changed selector, and the one fact that identified it was the one fact
// discarded. The detail goes to the Activity log, never to the sender.
func browserPageScriptError(provider string, details json.RawMessage) error {
	reason := browserPageScriptReason(details)
	if reason == "" {
		return fmt.Errorf("the %s page script failed", provider)
	}
	return fmt.Errorf("the %s page script failed: %s", provider, reason)
}

func browserPageScriptReason(details json.RawMessage) string {
	if len(details) == 0 || string(details) == "null" {
		return ""
	}
	var d struct {
		Text       string `json:"text"`
		LineNumber int    `json:"lineNumber"`
		Exception  struct {
			Description string `json:"description"`
			ClassName   string `json:"className"`
			Value       any    `json:"value"`
		} `json:"exception"`
	}
	if json.Unmarshal(details, &d) != nil {
		return ""
	}
	// description carries the message and, for a thrown Error, its stack. The
	// first line is the part that identifies the fault.
	reason := strings.TrimSpace(d.Exception.Description)
	if reason == "" {
		reason = strings.TrimSpace(d.Exception.ClassName)
	}
	if reason == "" {
		if s, ok := d.Exception.Value.(string); ok {
			reason = strings.TrimSpace(s)
		}
	}
	if reason == "" {
		reason = strings.TrimSpace(d.Text)
	}
	if reason == "" {
		return ""
	}
	if i := strings.IndexAny(reason, "\r\n"); i >= 0 {
		reason = strings.TrimSpace(reason[:i])
	}
	// A syntax error is reported against the evaluated expression, so its line
	// number is the one thing that locates it in the driver script.
	if d.LineNumber > 0 && strings.Contains(strings.ToLower(reason), "syntaxerror") {
		reason = fmt.Sprintf("%s (line %d of the page driver)", reason, d.LineNumber+1)
	}
	return truncate(reason, 220)
}
