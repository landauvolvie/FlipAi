package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// The hook helper is a fourth internal role of FlipAi.exe, alongside the host,
// watchdog, and tray. Claude Code runs it once per hook event in the live
// session it started for FlipAi.
//
// It exists rather than a shell one-liner for two reasons. A session's inbox
// address and token are exported into the hook process's own environment and
// nowhere else, so something FlipAi controls has to read them and hand them
// back. And using FlipAi.exe keeps live mode free of any dependency on curl,
// PowerShell quoting, or whatever else happens to be on the user's PATH.

// claudeHookHeader carries the per-session shared secret. The loopback endpoint
// accepts nothing without it, so another local process cannot post a fabricated
// reply and have FlipAi text it to the user's phone.
const claudeHookHeader = "X-FlipAi-Hook"

// Environment variables Claude Code exports to hook processes. They are the
// only documented way to learn a running session's inbox from outside it.
const (
	claudeMessagingSocketEnv = "CLAUDE_CODE_MESSAGING_SOCKET"
	claudeMessagingTokenEnv  = "CLAUDE_CODE_MESSAGING_TOKEN"
)

// claudeHookCommandLine builds the command string stored in the session's
// --settings hook block.
//
// Claude Code hands this to a shell, so every argument is quoted: an install
// path with a space in it is the normal case on Windows, not an edge case.
func claudeHookCommandLine(exe, url, token string) string {
	q := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }
	return q(exe) + " --claude-hook " + q(url) + " " + q(token)
}

// runClaudeHookCommand is the child role. It reads the hook event from stdin,
// adds the two messaging values from its own environment, and posts the result
// to FlipAi's loopback server.
//
// It always exits 0. A hook that exits non-zero can block the turn it is
// reporting on, and FlipAi failing to hear about a reply must never be allowed
// to stall the session the user is talking to.
func runClaudeHookCommand(args []string) int {
	if len(args) < 4 {
		return 0
	}
	url, token := args[2], args[3]

	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
	if err != nil || len(bytes.TrimSpace(raw)) == 0 {
		return 0
	}

	// Decode into a map rather than the typed struct so the payload forwarded to
	// FlipAi keeps every field Claude Code sent, including ones added by a
	// version newer than this build.
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return 0
	}
	if v := strings.TrimSpace(os.Getenv(claudeMessagingSocketEnv)); v != "" {
		payload["flipaiMessagingSocket"] = v
	}
	if v := strings.TrimSpace(os.Getenv(claudeMessagingTokenEnv)); v != "" {
		payload["flipaiMessagingToken"] = v
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(claudeHookHeader, token)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Nothing useful to do: the host may be restarting. Say so on stderr,
		// which Claude Code surfaces without treating the turn as failed.
		fmt.Fprintf(os.Stderr, "FlipAi hook could not reach the bridge: %v\n", err)
		return 0
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	return 0
}

// newHookToken mints the per-run secret the hook helper presents to the
// loopback endpoint. It lives only in memory: a fresh one each host start is
// strictly better here than a stored value, because the only thing that needs
// to know it is a child process FlipAi launches itself.
func newHookToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// A predictable token would let any local process post a reply, so fail
		// closed: an empty token makes the endpoint refuse everything and live
		// mode falls back to per-message.
		return ""
	}
	return hex.EncodeToString(b)
}
