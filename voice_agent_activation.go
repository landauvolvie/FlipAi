package main

import "time"

// agentVoiceStartActions are deliberately separate attempts. Windows accepting
// one UI Automation or input call does not prove Chromium acted on it, so the
// Windows caller verifies that live Voice appeared after every method before it
// tries the next one.
var agentVoiceStartActions = []string{
	"start-invoke",
	"start-keyboard",
	"start-legacy",
	"start-pointer",
}

const agentVoiceAttemptConfirm = 5 * time.Second

func agentVoiceAttemptDeadline(overall time.Time) time.Time {
	attempt := time.Now().Add(agentVoiceAttemptConfirm)
	if overall.Before(attempt) {
		return overall
	}
	return attempt
}
