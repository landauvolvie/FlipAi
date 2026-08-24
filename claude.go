package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type ClaudeAuthStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	AuthMethod       string `json:"authMethod"`
	ApiProvider      string `json:"apiProvider"`
	SubscriptionType string `json:"subscriptionType"`
}

// claudeAuthTTL is how long a validated auth status is reused. The check exists
// to refuse API/Console billing, which does not change mid-session; running it
// as a separate subprocess before every single SMS added seconds to each turn
// for no benefit. A failed run clears the cache, so a genuine sign-out is
// caught on the next attempt.
const claudeAuthTTL = 10 * time.Minute

// claudeFullAccess is the Claude Code permission mode that gives an SMS turn
// the same reach FlipAi already gives Codex.
//
// Codex SMS turns run with approvalPolicy "never" and sandbox
// "danger-full-access" (see applyCodexRequestDefaults). "acceptEdits" is not
// the Claude equivalent of that: it auto-approves file edits and nothing else,
// so Bash and every MCP tool — including the Claude in Chrome tools, which are
// MCP tools — still go through the normal permission prompt. An unattended SMS
// turn has nobody to answer that prompt, so the tool call is refused and Claude
// truthfully reports that it was not allowed to drive Chrome, even though the
// very same account drives Chrome fine in an interactive session. Matching the
// access FlipAi already grants Codex is what makes the two agents behave alike.
const claudeFullAccess = "bypassPermissions"

// claudeSessionPrefix labels the SMS conversation so it is recognisable, and
// resumable by name, in Claude Code.
//
// Claude Code deliberately leaves `claude -p` sessions out of the interactive
// /resume picker, so a name does not put the conversation in that list. What it
// does give is a working resume handle: `claude --resume "<name>"` continues
// the session. That only holds while the name is unique — Claude Code answers
// an ambiguous name with "matches N sessions" and exits — and FlipAi starts a
// fresh session on every new-session command, so the name carries a timestamp
// rather than being the same string every time.
const claudeSessionPrefix = "FlipAi SMS"

// newClaudeSessionName mints the name for one SMS conversation. It is stored
// with the session id and reused on every resume, so the label stays put for
// the life of the conversation instead of being rewritten each turn.
//
// The timestamp is for the user's benefit; the random suffix is what actually
// guarantees uniqueness, because two new-session commands in the same second
// would otherwise collide and make --resume by name ambiguous.
func newClaudeSessionName(now time.Time) string {
	return claudeSessionPrefix + " " + now.Format("2006-01-02 15:04") + " " + randomNameSuffix()
}

// randomNameSuffix returns four lowercase base32 characters. Randomness failing
// is not worth failing a turn over, so the clock's nanoseconds stand in.
func randomNameSuffix() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz234567"
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		n := uint32(time.Now().UnixNano())
		for i := range b {
			b[i] = byte(n >> (8 * i))
		}
	}
	out := make([]byte, len(b))
	for i, v := range b {
		out[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(out)
}

// claudeSessionIsGone reports whether a turn failed because the stored session
// no longer exists, rather than for a transient reason.
//
// Claude Code answers every variant of this — transcript deleted, emptied,
// corrupted beyond parsing, or aged out by cleanupPeriodDays (30 days by
// default) — with exit 1 and the same single line, so one match covers them
// all. Without this the stored id stays poisoned and every later Claude text
// fails identically, forever. Codex has had the equivalent recovery since
// codexThreadIsGone; this is the missing Claude half.
func claudeSessionIsGone(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no conversation found with session id") ||
		strings.Contains(s, "no conversation found")
}

type ClaudeClient struct {
	path string
	cwd  string
	cfg  ClaudeConfig
	// token is the optional long-lived `claude setup-token` value. Empty means
	// fall back to the CLI's own browser login, exactly as before.
	token string
	mu    sync.Mutex

	authCached ClaudeAuthStatus
	authAt     time.Time
	authValid  bool

	// loginChecked/loginExists cache whether this account has a real
	// `claude /login` session, probed with the stored token removed, and
	// loginAt is when that answer was taken.
	loginChecked bool
	loginExists  bool
	loginAt      time.Time
}

type claudeResult struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`

	// PermissionDenials lists tool calls Claude attempted and was refused. A
	// refusal is reported inside a successful run, so without reading this a
	// blocked turn looks like a normal answer and the user is left with Claude
	// saying it lacks permission and no way to see why.
	PermissionDenials []claudeDenial `json:"permission_denials"`
}

type claudeDenial struct {
	ToolName string `json:"tool_name"`
}

// deniedToolNames returns the distinct tools a turn was refused, in the order
// they were first refused.
func (r claudeResult) deniedToolNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range r.PermissionDenials {
		name := strings.TrimSpace(d.ToolName)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// normalizeClaudePermissionMode maps a configured value onto a mode the Claude
// Code CLI accepts. An empty, unknown, or retired value means full access, so a
// Claude SMS turn is never quietly narrower than the Codex turn beside it.
func normalizeClaudePermissionMode(v string) string {
	switch strings.TrimSpace(v) {
	case "plan":
		return "plan"
	case "acceptEdits":
		return "acceptEdits"
	case "dontAsk":
		return "dontAsk"
	case "default", "manual":
		return "default"
	default:
		return claudeFullAccess
	}
}

// claudePermissionModeLabel describes a mode in the same plain words the Codex
// card uses for its own access level, so the two agents can be compared without
// knowing what a Claude Code permission mode is.
func claudePermissionModeLabel(mode string) string {
	switch normalizeClaudePermissionMode(mode) {
	case "acceptEdits":
		return "File edits only — Chrome blocked"
	case "plan":
		return "Plan only — no tools"
	case "dontAsk":
		return "No prompts — allowlist only"
	case "default":
		return "Ask — unattended turns stall"
	default:
		return "Full user access"
	}
}

// claudeTurnArgs builds the command line for one SMS turn. It is separate from
// Run so the flags an SMS turn actually receives can be asserted in a test.
func claudeTurnArgs(cfg ClaudeConfig, sessionID, sessionName, prompt string) []string {
	args := []string{"-p", prompt, "--output-format", "json"}
	if sessionID != "" {
		args = append(args, "--resume", sessionID)
	}
	args = append(args, "--permission-mode", normalizeClaudePermissionMode(cfg.PermissionMode))
	// Name every turn, not just the first. --name is accepted alongside
	// --resume, so a conversation started by an older build picks the label up
	// on its next turn. The name is minted once per conversation and stored, so
	// resuming keeps the same one rather than renaming the session each turn.
	if name := strings.TrimSpace(sessionName); name != "" {
		args = append(args, "--name", name)
	}
	// Claude keeps whatever capabilities it has at the desktop, browser
	// included, so "check my sales in the browser" works by text too. FlipAi
	// no longer probes `claude --help` for this: the flag is configuration.
	if cfg.UseChrome {
		args = append(args, "--chrome")
	}
	return args
}

func NewClaudeClient(path, cwd string, cfg ClaudeConfig) *ClaudeClient {
	return &ClaudeClient{path: resolveClaudeExecutable(path), cwd: cwd, cfg: cfg}
}

// NewClaudeClientWithToken builds a client that authenticates with a stored
// long-lived token instead of relying on the CLI's expiring browser session.
func NewClaudeClientWithToken(path, cwd string, cfg ClaudeConfig, token string) *ClaudeClient {
	c := NewClaudeClient(path, cwd, cfg)
	c.token = strings.TrimSpace(token)
	return c
}

// scrubAnthropicEnv prevents FlipAi from silently using API-key, custom
// endpoint, Bedrock, Vertex, or Foundry billing inherited from the Windows
// environment. CLAUDE_CODE_OAUTH_TOKEN is intentionally not removed because it
// is Claude Code's subscription/OAuth token mechanism.
func scrubAnthropicEnv(env []string) []string {
	deny := map[string]bool{
		"ANTHROPIC_API_KEY":             true,
		"ANTHROPIC_AUTH_TOKEN":          true,
		"ANTHROPIC_BASE_URL":            true,
		"CLAUDE_CODE_USE_BEDROCK":       true,
		"CLAUDE_CODE_USE_VERTEX":        true,
		"CLAUDE_CODE_USE_FOUNDRY":       true,
		"ANTHROPIC_VERTEX_PROJECT_ID":   true,
		"ANTHROPIC_VERTEX_REGION":       true,
		"ANTHROPIC_FOUNDRY_RESOURCE":    true,
		"ANTHROPIC_FOUNDRY_API_VERSION": true,
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		k := e
		if i := strings.IndexByte(e, '='); i >= 0 {
			k = e[:i]
		}
		if !deny[strings.ToUpper(k)] {
			out = append(out, e)
		}
	}
	return out
}

// cachedAuthStatus returns a recent validated status, or runs the real check.
func (c *ClaudeClient) cachedAuthStatus(ctx context.Context) (ClaudeAuthStatus, error) {
	c.mu.Lock()
	if c.authValid && time.Since(c.authAt) < claudeAuthTTL {
		st := c.authCached
		c.mu.Unlock()
		return st, nil
	}
	c.mu.Unlock()

	st, err := c.authStatus(ctx)
	if err != nil {
		return st, err
	}
	c.mu.Lock()
	c.authCached, c.authAt, c.authValid = st, time.Now(), true
	c.mu.Unlock()
	return st, nil
}

func (c *ClaudeClient) invalidateAuthCache() {
	c.mu.Lock()
	c.authValid = false
	c.mu.Unlock()
}

// authStatus deliberately parses Claude's JSON even when the command exits 1.
// Current Claude Code versions can return a perfectly useful JSON status such
// as {loggedIn:false, authMethod:"none", apiProvider:"firstParty"} with exit 1.
// Treating the exit code as the status was the reason FlipAi stopped before it
// could try the actual background request.
func (c *ClaudeClient) authStatus(ctx context.Context) (ClaudeAuthStatus, error) {
	return c.authStatusWithEnv(ctx, c.childEnv())
}

// authStatusWithEnv is authStatus against a chosen environment, so the login
// probe can ask about the machine's own session rather than the token's.
func (c *ClaudeClient) authStatusWithEnv(ctx context.Context, env []string) (ClaudeAuthStatus, error) {
	var st ClaudeAuthStatus
	if strings.TrimSpace(c.path) == "" {
		return st, errors.New("Claude Code path is empty")
	}
	cmd := exec.CommandContext(ctx, c.path, "auth", "status")
	cmd.Env = env
	hideWindow(cmd)
	out, runErr := cmd.CombinedOutput()
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) > 0 && json.Unmarshal(trimmed, &st) == nil {
		return st, nil
	}
	if runErr != nil {
		return st, fmt.Errorf("Claude Code auth status could not run using %q: %v: %s", c.path, runErr, truncate(c.redact(string(out)), 800))
	}
	return st, fmt.Errorf("Claude auth status did not return JSON; update Claude Code: %s", truncate(c.redact(string(out)), 500))
}

func validateClaudeSubscriptionPath(st ClaudeAuthStatus) error {
	method := strings.ToLower(strings.TrimSpace(st.AuthMethod))
	provider := strings.ToLower(strings.TrimSpace(st.ApiProvider))
	if provider != "" && provider != "firstparty" {
		return fmt.Errorf("Claude is configured for provider %q, not the first-party Claude subscription service; FlipAi refuses external/provider billing", st.ApiProvider)
	}
	if strings.Contains(method, "api") || strings.Contains(method, "console") {
		return fmt.Errorf("Claude is authenticated for API/Console billing (%s); FlipAi refuses that billing path", st.AuthMethod)
	}
	return nil
}

// baseEnv is the scrubbed parent environment with any Claude token removed,
// inherited or stored. It is what a subprocess gets when it must authenticate
// as the machine's own `claude /login` session rather than as a token.
func (c *ClaudeClient) baseEnv() []string {
	env := scrubAnthropicEnv(os.Environ())
	out := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(strings.ToUpper(e), "CLAUDE_CODE_OAUTH_TOKEN=") {
			out = append(out, e)
		}
	}
	return out
}

// envWithToken is baseEnv plus the stored long-lived token.
// scrubAnthropicEnv deliberately preserves CLAUDE_CODE_OAUTH_TOKEN, so setting
// it here is the supported way to keep an unattended bridge signed in.
func (c *ClaudeClient) envWithToken() []string {
	env := c.baseEnv()
	if tok := strings.TrimSpace(c.token); tok != "" {
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+tok)
	}
	return env
}

// claudeLoginTTL is how long the interactive-login probe is reused.
//
// It used to be cached for the life of the client, which meant a user who
// completed `claude /login` to get Chrome working had to restart FlipAi before
// anything noticed — the one step nothing on screen asked for. A short TTL
// costs at most one extra `claude auth status` subprocess every couple of
// minutes, and only on machines that have a token stored, because that is the
// only case where the answer changes which credential a turn uses.
const claudeLoginTTL = 2 * time.Minute

// interactiveLoginExists reports whether this Windows account has a real
// `claude /login` session, checked with the stored token deliberately removed
// so the answer is about the machine rather than about the token.
func (c *ClaudeClient) interactiveLoginExists(ctx context.Context) bool {
	c.mu.Lock()
	if c.loginChecked && time.Since(c.loginAt) < claudeLoginTTL {
		exists := c.loginExists
		c.mu.Unlock()
		return exists
	}
	c.mu.Unlock()

	st, err := c.authStatusWithEnv(ctx, c.baseEnv())
	exists := err == nil && st.LoggedIn && validateClaudeSubscriptionPath(st) == nil

	c.mu.Lock()
	c.loginChecked, c.loginExists, c.loginAt = true, exists, time.Now()
	c.mu.Unlock()
	return exists
}

// invalidateLoginCache forces the next probe to ask the machine again. Connect
// and Disconnect call it so a sign-in the user has just completed is picked up
// on the spot rather than whenever the TTL happens to expire.
func (c *ClaudeClient) invalidateLoginCache() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.loginChecked, c.loginExists, c.loginAt = false, false, time.Time{}
	c.authValid = false
	c.mu.Unlock()
}

// RefreshLogin re-probes the machine and reports the fresh answer. It is what
// the Connect flow calls, so the answer the user is shown is the one the next
// SMS turn will act on.
func (c *ClaudeClient) RefreshLogin(ctx context.Context) bool {
	if c == nil {
		return false
	}
	c.invalidateLoginCache()
	return c.interactiveLoginExists(ctx)
}

// CachedLogin reports the last probe result without starting a subprocess, for
// page renders. checked is false when nothing has probed yet.
func (c *ClaudeClient) CachedLogin() (checked, exists bool) {
	if c == nil {
		return false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginChecked, c.loginExists
}

// childEnv builds the environment for a Claude Code subprocess.
//
// The stored token is withheld when Chrome is enabled and the machine has a
// real login to fall back on. Claude Code keeps Chrome integration off for a
// `claude setup-token` session even when --chrome is passed, because the
// browser extension cannot authenticate with that credential. Injecting the
// token therefore silently disabled the browser for every SMS turn, while the
// same account driving Chrome fine when the user ran `claude` themselves — the
// exact difference FlipAi exists to erase.
//
// The token is still used whenever it is the only thing that works: with Chrome
// off it costs nothing, and with no interactive login it is the difference
// between a bridge that answers and one that is stranded. In that case Chrome
// genuinely cannot work, and the Agents page says so rather than letting Claude
// report a permission problem the user cannot find.
func (c *ClaudeClient) childEnv() []string {
	if c.tokenBlocksChrome() {
		return c.baseEnv()
	}
	return c.envWithToken()
}

// tokenBlocksChrome reports whether the stored token is being withheld because
// it would turn Chrome off. It reads the cached probe only, so it never starts a
// subprocess from inside another subprocess's setup.
func (c *ClaudeClient) tokenBlocksChrome() bool {
	if strings.TrimSpace(c.token) == "" || !c.cfg.UseChrome {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginChecked && c.loginExists
}

// resolveTokenPolicy runs the interactive-login probe once, when its answer can
// change which credential a turn uses. With Chrome off, or with no token
// stored, the token is used unconditionally and there is nothing to probe.
func (c *ClaudeClient) resolveTokenPolicy(ctx context.Context) {
	if strings.TrimSpace(c.token) == "" || !c.cfg.UseChrome {
		return
	}
	c.interactiveLoginExists(ctx)
}

// CachedChromeTokenConflict is the render-safe form of ChromeTokenConflict. A
// page render must never start a subprocess, so this reports the conflict only
// once a turn or a test has already run the probe.
func (c *ClaudeClient) CachedChromeTokenConflict() string {
	if c == nil || strings.TrimSpace(c.token) == "" || !c.cfg.UseChrome {
		return ""
	}
	c.mu.Lock()
	checked, exists := c.loginChecked, c.loginExists
	c.mu.Unlock()
	if !checked || exists {
		return ""
	}
	return chromeTokenConflictText
}

// chromeTokenConflictText is shared by both forms so the page and any log say
// the same thing.
const chromeTokenConflictText = "Claude cannot control Chrome while the stored token is the only sign-in on this account. " +
	"Claude Code turns Chrome off for a `claude setup-token` session even with the Chrome switch on, " +
	"because the browser extension cannot authenticate with that credential. " +
	"Press Connect Claude under Authentication & session to complete `claude /login` on this Windows account; " +
	"FlipAi then uses that sign-in for Chrome turns and keeps the token only as the fallback."

// ChromeTokenConflict describes, for the Agents page, a machine where Chrome is
// switched on but the only credential available cannot drive it. Empty means
// there is nothing to warn about.
func (c *ClaudeClient) ChromeTokenConflict(ctx context.Context) string {
	if strings.TrimSpace(c.token) == "" || !c.cfg.UseChrome {
		return ""
	}
	if c.interactiveLoginExists(ctx) {
		return ""
	}
	return chromeTokenConflictText
}

// Version reports the installed Claude Code version, empty when it cannot be
// determined. Live mode needs it to decide whether the running session will
// even have an inbox to deliver an SMS into, so a failure here is answered with
// an empty string and treated as "too old" rather than as a hard error.
func (c *ClaudeClient) Version(ctx context.Context) string {
	if strings.TrimSpace(c.path) == "" {
		return ""
	}
	cmd := exec.CommandContext(ctx, c.path, "--version")
	cmd.Env = c.childEnv()
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c *ClaudeClient) runPrint(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.path, args...)
	if c.cwd != "" {
		cmd.Dir = c.cwd
	}
	cmd.Env = c.childEnv()
	hideWindow(cmd)
	return cmd.CombinedOutput()
}

// Test performs the real non-interactive Claude Code request used by SMS.
// `claude auth status` is used only to reject explicit API/provider billing;
// it is not trusted as the final readiness signal because current Claude Code
// builds can report loggedIn:false while another first-party OAuth path still
// lets `claude -p` work. The real request is the authoritative test.
func (c *ClaudeClient) Test(ctx context.Context) error {
	c.resolveTokenPolicy(ctx)
	st, err := c.authStatus(ctx)
	if err != nil {
		return err
	}
	if err := validateClaudeSubscriptionPath(st); err != nil {
		return err
	}
	args := []string{"-p", "Reply with exactly FLIPAI_CLAUDE_OK and nothing else.", "--output-format", "json", "--max-turns", "1", "--permission-mode", "plan"}
	out, runErr := c.runPrint(ctx, args)
	if runErr != nil {
		if !st.LoggedIn {
			return fmt.Errorf("Claude Code CLI could not run a first-party background request: %v: %s\n\nClaude Desktop and Claude Code CLI have separate sign-in state on Windows. Being signed into the Claude desktop app does not prove the CLI at %q is authenticated. Open Claude Code in PowerShell and complete /login (or run `claude auth login`), then test again", runErr, truncate(c.redact(string(out)), 800), c.path)
		}
		return fmt.Errorf("Claude subscription login exists, but the real background request failed: %v: %s", runErr, truncate(c.redact(string(out)), 800))
	}
	var r claudeResult
	if err := json.Unmarshal(out, &r); err != nil {
		return fmt.Errorf("Claude background test returned unexpected output; update Claude Code: %s", truncate(c.redact(string(out)), 500))
	}
	if r.IsError {
		return fmt.Errorf("Claude background test reported an error: %s", truncate(r.Result, 500))
	}
	if !strings.Contains(strings.ToUpper(r.Result), "FLIPAI_CLAUDE_OK") {
		return fmt.Errorf("Claude background test did not return the expected verification response: %s", truncate(r.Result, 300))
	}
	return nil
}

func (c *ClaudeClient) Run(ctx context.Context, sessionID, sessionName, prompt string) (result, newSession string, err error) {
	// Resolve the Chrome/token question before anything builds an environment.
	// childEnv only reads the cached answer, so without this the first turn
	// after a restart would still inject the token and lose Chrome.
	c.resolveTokenPolicy(ctx)

	st, err := c.cachedAuthStatus(ctx)
	if err != nil {
		return "", sessionID, err
	}
	if err := validateClaudeSubscriptionPath(st); err != nil {
		return "", sessionID, err
	}
	args := claudeTurnArgs(c.cfg, sessionID, sessionName, prompt)
	out, runErr := c.runPrint(ctx, args)
	if runErr != nil {
		// A failed run may mean the CLI was signed out since the last check.
		c.invalidateAuthCache()
		if elevErr := claudeElevationRefusal(string(out)); elevErr != nil {
			return "", sessionID, elevErr
		}
		// Report a vanished conversation verbatim so the bridge can recognise
		// it and start a fresh session instead of failing forever.
		if sessionID != "" && claudeSessionIsGone(errors.New(string(out))) {
			return "", sessionID, fmt.Errorf("no conversation found with session id %s: %s", sessionID, truncate(c.redact(string(out)), 300))
		}
		if !st.LoggedIn {
			return "", sessionID, fmt.Errorf("Claude Code CLI is not usable from FlipAi yet: %v: %s. Claude Desktop sign-in is separate from Claude Code CLI sign-in; complete Claude Code /login once on this Windows account", runErr, truncate(c.redact(string(out)), 800))
		}
		return "", sessionID, fmt.Errorf("Claude Code failed: %v: %s", runErr, truncate(c.redact(string(out)), 800))
	}
	var r claudeResult
	if json.Unmarshal(out, &r) != nil {
		return strings.TrimSpace(c.redact(string(out))), sessionID, nil
	}
	if r.IsError {
		return "", r.SessionID, fmt.Errorf("Claude reported an error: %s", r.Result)
	}
	if r.SessionID == "" {
		r.SessionID = sessionID
	}
	// A refused tool is reported inside a successful run. Say so in the reply:
	// otherwise the only clue is Claude explaining that it lacks a permission
	// the same account plainly has when the user runs Claude themselves.
	answer := strings.TrimSpace(r.Result)
	if note := claudeDenialNote(c.cfg.PermissionMode, r.deniedToolNames()); note != "" {
		answer = strings.TrimSpace(answer + "\n\n" + note)
	}
	return answer, r.SessionID, nil
}

// claudeDenialNote is the one short line added to a reply whose tools were
// refused. It stays terse because it shares a length-capped SMS with the actual
// answer, and it only recommends widening the mode when widening is the fix —
// under full access a denial comes from the user's own Claude settings instead.
func claudeDenialNote(configured string, denied []string) string {
	if len(denied) == 0 {
		return ""
	}
	const maxNamed = 3
	shown := denied
	if len(shown) > maxNamed {
		shown = shown[:maxNamed]
	}
	list := strings.Join(shown, ", ")
	if len(denied) > maxNamed {
		list += fmt.Sprintf(" +%d more", len(denied)-maxNamed)
	}
	mode := normalizeClaudePermissionMode(configured)
	if mode == claudeFullAccess {
		// Nothing FlipAi sets caused this, so point at what did.
		return "[FlipAi: Claude blocked " + list + " — check your own Claude permission rules.]"
	}
	return "[FlipAi: mode " + mode + " blocked " + list + " — set Claude to full user access.]"
}

// claudeElevationRefusal recognises Claude Code refusing full-access mode
// because the process is elevated. FlipAi never requests elevation, so this
// only happens when the whole bridge was started as an administrator, and the
// raw CLI text does not say which FlipAi setting caused it.
func claudeElevationRefusal(out string) error {
	low := strings.ToLower(out)
	if !strings.Contains(low, "root/sudo") && !strings.Contains(low, "administrator privileges") {
		return nil
	}
	// Require the permission flag by name too. "administrator" alone also
	// appears in ordinary Windows access-denied text, which this must not
	// misreport as a FlipAi setting problem.
	if !strings.Contains(low, "skip-permissions") && !strings.Contains(low, "bypasspermissions") {
		return nil
	}
	return errors.New("Claude refuses full-access mode when it is started with administrator privileges. Run FlipAi as your normal Windows user (FlipAi never needs elevation), or set Claude permission mode to Accept edits in Settings -> Agents")
}
