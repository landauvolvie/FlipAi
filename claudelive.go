package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Session modes. "print" is the original one-subprocess-per-turn behaviour and
// the default; "live" keeps a single Claude Code session running so the SMS
// conversation can also be opened through Remote Control.
const (
	claudeSessionModePrint = "print"
	claudeSessionModeLive  = "live"
)

// claudeLiveMinVersion is the first Claude Code that binds a cross-session
// inbox on native Windows. Below it there is no named pipe to deliver an SMS
// into, so live mode has no way to reach a running session at all and FlipAi
// must stay in print mode.
//
// The pipe is what carries the message; Remote Control only makes the same
// session visible at claude.ai/code. The two are separate requirements and are
// reported separately, because a machine can satisfy one and not the other.
const claudeLiveMinVersion = "2.1.234"

// normalizeClaudeSessionMode maps a configured value onto a mode FlipAi
// implements. Anything unknown, empty, or retired means print, so a
// hand-edited config or a downgrade can never leave the bridge in a mode it
// does not understand.
func normalizeClaudeSessionMode(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), claudeSessionModeLive) {
		return claudeSessionModeLive
	}
	return claudeSessionModePrint
}

// claudeSessionModeLabel describes a mode in the same plain words the rest of
// the Agents page uses, so the choice can be understood without knowing what a
// Claude Code session is.
func claudeSessionModeLabel(mode string) string {
	if normalizeClaudeSessionMode(mode) == claudeSessionModeLive {
		return "Live session — viewable in Remote Control"
	}
	return "Per-message — one request per text"
}

// claudeLiveSupport is the answer to "can this machine run live mode right
// now", and why not when it cannot.
//
// Reason is written for the person reading the Agents page, not for a log: it
// names the thing to change. OK is false whenever anything at all is missing,
// so a caller never has to interpret the individual fields.
type claudeLiveSupport struct {
	OK      bool
	Reason  string
	Version string

	// RemoteControl reports whether the session will actually appear at
	// claude.ai/code. Live mode still runs without it — the SMS conversation
	// stays in one session either way — but the headline reason for choosing
	// live mode is gone, so the UI says so rather than implying a browser view
	// that will never appear.
	RemoteControl bool
}

// parseClaudeVersion pulls the semantic version out of `claude --version`,
// which prints a line like "2.1.241 (Claude Code)".
func parseClaudeVersion(out string) ([3]int, bool) {
	var v [3]int
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		return v, false
	}
	parts := strings.Split(strings.TrimSpace(fields[0]), ".")
	if len(parts) < 3 {
		return v, false
	}
	for i := 0; i < 3; i++ {
		// Trim any pre-release suffix such as "234-beta" so a tagged build is
		// compared on its numeric version rather than rejected outright.
		p := parts[i]
		if cut := strings.IndexFunc(p, func(r rune) bool { return r < '0' || r > '9' }); cut >= 0 {
			p = p[:cut]
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// claudeVersionAtLeast compares two parsed versions. An unparseable installed
// version counts as too old: live mode depends on behaviour that only exists
// above a known version, so guessing in its favour would fail later and less
// clearly.
func claudeVersionAtLeast(have, min string) bool {
	h, ok := parseClaudeVersion(have)
	if !ok {
		return false
	}
	m, ok := parseClaudeVersion(min)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if h[i] != m[i] {
			return h[i] > m[i]
		}
	}
	return true
}

// evaluateClaudeLiveSupport decides whether live mode can run, from facts the
// caller has already gathered. It is deliberately pure so every branch can be
// asserted in a test rather than only on a machine that happens to be in that
// state.
//
// The checks run worst-first: a missing pipe means live mode cannot deliver an
// SMS at all, while a token only costs the browser view, so the message the
// user sees names the blocking problem rather than the first one found.
//
// loginExists is the token-free probe: when this account has a real
// `claude /login` session, FlipAi withholds the stored token from the live
// session exactly as it does for a Chrome turn, so Remote Control works and the
// token stays available as the fallback. A stored token only costs the browser
// view when it is the *only* credential on the machine.
func evaluateClaudeLiveSupport(version string, hasToken, loginExists bool, st ClaudeAuthStatus) claudeLiveSupport {
	s := claudeLiveSupport{Version: strings.TrimSpace(version)}

	if !claudeVersionAtLeast(version, claudeLiveMinVersion) {
		have := s.Version
		if have == "" {
			have = "an unknown version"
		}
		s.Reason = "Live session mode needs Claude Code " + claudeLiveMinVersion +
			" or newer on Windows, and this machine has " + have +
			". Update Claude Code, or leave FlipAi in per-message mode."
		return s
	}

	// FlipAi refuses API/Console billing everywhere else; live mode is no
	// different, and Remote Control would refuse it too.
	if err := validateClaudeSubscriptionPath(st); err != nil {
		s.Reason = err.Error()
		return s
	}

	// The session can run and take SMS, so live mode itself is available. What
	// is left decides whether Remote Control comes with it.
	s.OK = true

	// A real sign-in wins over the token, so the token no longer costs the
	// browser view on a machine that has both.
	if loginExists {
		s.RemoteControl = true
		return s
	}
	if hasToken {
		// This is the one that catches most FlipAi installs, because the stored
		// token is exactly what makes an unattended bridge survive. Say plainly
		// that the trade is real rather than implying the token is a mistake.
		s.Reason = "Remote Control cannot use the stored Claude token: a `claude setup-token` value can only make model requests, " +
			"so it cannot open a session at claude.ai/code. SMS still stays in one live session. " +
			"Press Connect Claude under Authentication & session to add a `claude /login` sign-in — FlipAi will use it for " +
			"the live session and keep the token as the fallback."
		return s
	}
	if !st.LoggedIn {
		s.Reason = "Remote Control needs a completed Claude Code sign-in on this Windows account. " +
			"Press Connect Claude under Authentication & session. SMS still stays in one live session until then, " +
			"but it will not appear at claude.ai/code."
		return s
	}

	s.RemoteControl = true
	return s
}

// claudeLiveSettings builds the --settings payload for the live session.
//
// Everything live mode needs from Claude Code is configured here rather than
// through flags, for two reasons: `claude remote-control` documents a small
// flag set that does not include a permission mode, and a --settings value
// applies only to the session FlipAi starts, so nothing here changes how the
// user's own Claude Code sessions behave.
func claudeLiveSettings(cfg ClaudeConfig, hookCommand string) (string, error) {
	if strings.TrimSpace(hookCommand) == "" {
		return "", errors.New("live mode needs a hook command to receive replies")
	}
	hook := func() any {
		return []any{map[string]any{
			"hooks": []any{map[string]any{"type": "command", "command": hookCommand}},
		}}
	}
	s := map[string]any{
		// Without this the session holds every SMS for an approval nobody is
		// there to give. A session running with bypassed permissions holds
		// inbound peer messages by default, and FlipAi's own process is the
		// session's parent rather than its child, so it never qualifies for the
		// own-child exemption. Held messages then expire silently.
		"crossSessionInbound": "accept",
		"permissions": map[string]any{
			"defaultMode": normalizeClaudePermissionMode(cfg.PermissionMode),
		},
		"hooks": map[string]any{
			// SessionStart is how FlipAi learns the session's inbox address and
			// token: Claude Code exports both to hook processes, and there is no
			// other documented way to read them from outside the session.
			"SessionStart":     hook(),
			"UserPromptSubmit": hook(),
			"Stop":             hook(),
		},
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// claudeLiveArgs builds the command line for the supervised session.
//
// --spawn session is what makes this one conversation rather than a pool:
// the server serves exactly one session and rejects further connections, which
// matches FlipAi's "one SMS conversation, one Claude session" model and stops a
// stray browser connection from opening a second session against the same
// working folder.
func claudeLiveArgs(cfg ClaudeConfig, sessionName, settings string) []string {
	args := []string{"remote-control", "--spawn", "session", "--settings", settings}
	if name := strings.TrimSpace(sessionName); name != "" {
		args = append(args, "--name", name)
	}
	if cfg.UseChrome {
		args = append(args, "--chrome")
	}
	return args
}

// claudeLiveMarker is the correlation tag FlipAi puts in front of a prompt it
// injects. It exists because a live session has two writers: FlipAi and
// whoever is typing at claude.ai/code. Without it, FlipAi would text the user
// the answer to a question they asked in the browser.
//
// The tag sits outside <sms_command> so the fenced-command guarantee is
// untouched, and its id is FlipAi's own value rather than anything derived
// from the text message.
func claudeLiveMarker(id string) string {
	return "<flipai_turn id=\"" + id + "\"/>"
}

// claudeLiveMarkerID recovers the id from a prompt Claude Code reports back
// through UserPromptSubmit, and reports false for a prompt that carries none —
// which is how a browser-typed turn is recognised and left alone.
func claudeLiveMarkerID(prompt string) (string, bool) {
	const open = "<flipai_turn id=\""
	i := strings.Index(prompt, open)
	if i < 0 {
		return "", false
	}
	rest := prompt[i+len(open):]
	j := strings.Index(rest, "\"")
	if j <= 0 {
		return "", false
	}
	return rest[:j], true
}

// claudeHookPayload is the subset of a Claude Code hook event FlipAi reads,
// plus the two messaging values the hook helper adds from its own environment.
//
// Only the fields FlipAi acts on are declared. Claude Code sends more, and a
// future version will send more still; an unknown field is ignored rather than
// failing the decode, so a Claude Code upgrade does not break SMS.
type claudeHookPayload struct {
	Event      string `json:"hook_event_name"`
	SessionID  string `json:"session_id"`
	PromptID   string `json:"prompt_id"`
	UserPrompt string `json:"user_prompt"`

	// LastAssistantMessage is the reply FlipAi texts back. Claude Code
	// recommends this field over reading the transcript, which can lag behind
	// the end of the turn.
	LastAssistantMessage string `json:"last_assistant_message"`

	// Socket and Token are not part of the hook event. The hook helper reads
	// them from the environment Claude Code exported to it and adds them here,
	// because that environment is the only documented way to learn a running
	// session's inbox address from outside the session.
	Socket string `json:"flipaiMessagingSocket,omitempty"`
	Token  string `json:"flipaiMessagingToken,omitempty"`
}

// Hook event names FlipAi subscribes to.
const (
	claudeHookSessionStart = "SessionStart"
	claudeHookUserPrompt   = "UserPromptSubmit"
	claudeHookStop         = "Stop"
)

// claudeInboxFrame builds the bytes written to a session's inbox.
//
// The first line authenticates the connection. Claude Code documents this line
// exactly, and on native Windows — the only platform FlipAi ships — it is
// required rather than optional: a connection whose first line is not a valid
// auth line is closed and delivers nothing.
//
// The message line that follows is the part Claude Code does not publish a
// schema for. FlipAi therefore treats a live turn as best-effort and falls back
// to print mode when a turn does not come back, so an unrecognised frame costs
// one slower reply rather than a lost text. See claudeLiveClient.Run.
func claudeInboxFrame(token, sender, text string) ([]byte, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("the live session did not report an inbox token")
	}
	auth, err := json.Marshal(map[string]any{"type": "auth", "token": token})
	if err != nil {
		return nil, err
	}
	msg, err := json.Marshal(map[string]any{
		"type": "message",
		"from": sender,
		"text": text,
	})
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.Write(auth)
	b.WriteByte('\n')
	b.Write(msg)
	b.WriteByte('\n')
	return []byte(b.String()), nil
}

// claudeLiveUnavailable is the error a caller turns into a print-mode fallback.
// It is a distinct type so a genuine Claude failure is never mistaken for
// "live mode could not run", which would silently downgrade every turn.
type claudeLiveUnavailable struct{ reason string }

func (e claudeLiveUnavailable) Error() string { return e.reason }

func liveUnavailable(format string, a ...any) error {
	return claudeLiveUnavailable{reason: fmt.Sprintf(format, a...)}
}

// isClaudeLiveUnavailable reports whether a turn failed because live mode could
// not run, rather than because Claude answered with an error.
func isClaudeLiveUnavailable(err error) bool {
	var u claudeLiveUnavailable
	return errors.As(err, &u)
}

// normalizeClaudeInboxAddr strips the scheme prefix Claude Code shows in
// `/status`, where the same address is printed as "uds:<path>". The exported
// environment variable is expected to be the bare path, so this is defensive:
// a prefixed value is a working address written a different way, not an error.
func normalizeClaudeInboxAddr(addr string) string {
	a := strings.TrimSpace(addr)
	for _, prefix := range []string{"uds:", "unix:", "npipe:", "pipe:"} {
		if strings.HasPrefix(strings.ToLower(a), prefix) {
			a = a[len(prefix):]
			break
		}
	}
	return strings.TrimSpace(a)
}
