package main

import (
	"strings"
)

// The elevated audio-driver installer runs under Start-Transcript, and a
// transcript begins with a dozen lines of header: the time, the machine name,
// the account, the host application, the PowerShell build numbers. When the
// install failed, the message the user was shown was that header, truncated
// before it ever reached the sentence that said what went wrong.
//
// summarizeInstallLog answers the only question the message has to answer:
// what stopped it. It prefers the error the script raised, falls back to the
// last thing that happened, and never returns transcript boilerplate.

// transcriptNoise is the header and footer Start-Transcript writes around the
// output that matters.
var transcriptNoise = []string{
	"windows powershell transcript",
	"start time:",
	"end time:",
	"username:",
	"runas user:",
	"configuration name:",
	"machine:",
	"host application:",
	"process id:",
	"psversion:",
	"psedition:",
	"pscompatibleversions:",
	"buildversion:",
	"clrversion:",
	"wsmanstackversion:",
	"psremotingprotocolversion:",
	"serializationversion:",
	"transcript started",
	"transcript stopped",
}

func transcriptBoilerplate(line string) bool {
	l := strings.ToLower(strings.TrimSpace(line))
	if l == "" {
		return true
	}
	// The banner rows of asterisks, and the separators around them.
	if strings.Trim(l, "*") == "" {
		return true
	}
	for _, noise := range transcriptNoise {
		if strings.HasPrefix(l, noise) {
			return true
		}
	}
	return false
}

// summarizeInstallLog turns a PowerShell transcript into the one or two lines
// worth showing. limit bounds the result; 0 means a sensible default.
func summarizeInstallLog(log string, limit int) string {
	if limit <= 0 {
		limit = 600
	}
	var kept []string
	for _, raw := range strings.Split(strings.ReplaceAll(log, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if transcriptBoilerplate(line) {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return ""
	}

	// The script says what went wrong in as many words. That is the answer.
	var errors []string
	for _, line := range kept {
		if strings.HasPrefix(line, "ERROR:") {
			errors = append(errors, strings.TrimSpace(strings.TrimPrefix(line, "ERROR:")))
		}
	}
	if len(errors) > 0 {
		return truncate(strings.Join(errors, " "), limit)
	}

	// Nothing raised, so show what it was doing when it stopped rather than
	// what it was doing when it started.
	tail := kept
	if len(tail) > 8 {
		tail = tail[len(tail)-8:]
	}
	return truncate(strings.Join(tail, " | "), limit)
}
