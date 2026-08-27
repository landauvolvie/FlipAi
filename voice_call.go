package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Voice calling is intentionally separate from the SMS transport. It uses its
// own config file and local control endpoint so enabling the experimental call
// bridge cannot change Gmail, message routing, or the SMS phone allowlist.
const (
	voiceControlListen = "127.0.0.1:8771"
	googleVoiceWebURL  = "https://voice.google.com/"
)

type VoiceCallConfig struct {
	Enabled      bool   `json:"enabled"`
	DefaultAgent string `json:"defaultAgent"` // C = ChatGPT/Codex, A = Claude Desktop

	GoogleVoiceURL string `json:"googleVoiceUrl"`

	// Deprecated: answering is not an option any more. With calling enabled,
	// an authorized caller is always answered and an unauthorized one never
	// is; the field is kept only so an existing voice-call.json parses.
	AutoAnswer bool `json:"autoAnswer,omitempty"`

	// The audio wiring is chosen automatically from the machine's virtual
	// cable endpoints (see voice_cables.go); there are no pickers anywhere.
	// These four fields exist only as hand-edited overrides for the rare
	// machine whose cables FlipAi does not recognize, and an override only
	// applies while a matching device is really present.
	GoogleVoiceInput  string `json:"googleVoiceInput,omitempty"`
	GoogleVoiceOutput string `json:"googleVoiceOutput,omitempty"`
	AgentInput        string `json:"agentInput,omitempty"`
	AgentOutput       string `json:"agentOutput,omitempty"`

	Codex  VoiceAgentCallConfig `json:"codex"`
	Claude VoiceAgentCallConfig `json:"claude"`
}

// VoiceAgentCallConfig is only about how to drive one desktop app during a
// call. Who may call it lives with the agent, on the Agents page, alongside who
// may text it -- there is one list of allowed numbers now, not two.
type VoiceAgentCallConfig struct {
	Enabled       bool   `json:"enabled"`
	AppTitle      string `json:"appTitle"`
	AppCommand    string `json:"appCommand,omitempty"`
	VoiceShortcut string `json:"voiceShortcut,omitempty"`

	// Deprecated: retained so an existing voice-call.json keeps parsing. The
	// allowlist moved to the agent; nothing reads these.
	AllowedCallers string `json:"allowedCallers,omitempty"`
	AllowedLabels  string `json:"allowedLabels,omitempty"`
}

type VoiceRuntimeState struct {
	BrowserRunning bool   `json:"browserRunning"`
	SignedIn       bool   `json:"signedIn"`
	Page           string `json:"page,omitempty"`
	InCall         bool   `json:"inCall"`
	// CallPhase is where in a call's life the machine in voice_session.go
	// currently is, and CallNote is the same thing in a sentence. They exist so
	// the desktop UI can tell "ringing", "answered but the agent has not
	// started talking yet" and "a real conversation is up" apart -- which all
	// used to read as the same "connected".
	CallPhase string `json:"callPhase,omitempty"`
	CallNote  string `json:"callNote,omitempty"`
	Caller    string `json:"caller,omitempty"`
	// CallerLabel is the raw caller ID text Google Voice displayed. It is kept
	// so a blocked call can say what FlipAi actually saw instead of leaving the
	// user guessing why nothing happened.
	CallerLabel string `json:"callerLabel,omitempty"`
	Agent       string `json:"agent,omitempty"`
	Blocked     string `json:"blocked,omitempty"`
	// Devices is the machine's audio endpoint list as the Google Voice window
	// last saw it. It is what the automatic cable detection reads.
	Devices []VoiceAudioDevice `json:"devices,omitempty"`
	// DeviceLabelsHidden records that the browser returned endpoints with no
	// names, which happens until the microphone permission is actually
	// granted. Without it the status page looks merely empty rather than
	// blocked.
	DeviceLabelsHidden bool `json:"deviceLabelsHidden,omitempty"`
	// RoutingNote is the outcome of the last attempt to point the desktop
	// app's own microphone and speaker at the cables, per-app, through the
	// Windows audio policy store.
	RoutingNote string `json:"routingNote,omitempty"`
	// RenderMode is how Google Voice is currently being drawn, and
	// RenderAttempt is where in the list of ways to draw it FlipAi has got to.
	// A window that comes up black is a graphics problem rather than a startup
	// one, so Retry moves along the list instead of repeating what did not work.
	RenderMode    string `json:"renderMode,omitempty"`
	RenderAttempt int    `json:"renderAttempt,omitempty"`
	// DockBlocked says why the window is not standing in the FlipAi panel, when
	// it is running but not placed. Without it "could not put it in this panel"
	// is a statement with no cause attached.
	DockBlocked string `json:"dockBlocked,omitempty"`
	// Docked is set while the Google Voice window is standing inside the FlipAi
	// window. The page uses it to know whether the panel it reserved is really
	// showing a browser or whether it should explain why it is empty.
	Docked    bool   `json:"docked,omitempty"`
	LastError string `json:"lastError,omitempty"`
	LastEvent string `json:"lastEvent,omitempty"`
	// LastOpen is the outcome of the most recent attempt to put the Google
	// Voice window on screen. Opening it spans two processes, so without this a
	// click that produced nothing leaves nothing behind to explain itself.
	// Controls is what the Google Voice page currently offers, and LastRingAt is
	// when an answer control was last seen. Together they answer "is a call even
	// reaching this window", which is otherwise invisible.
	Controls   string    `json:"controls,omitempty"`
	LastRingAt time.Time `json:"lastRingAt,omitempty"`

	// ControlPort is the loopback DevTools port the Google Voice window opened
	// for FlipAi. It is written down rather than discovered so the host process
	// can reach the same window to send an MMS without guessing at listeners.
	ControlPort int `json:"controlPort,omitempty"`

	LastOpen string `json:"lastOpen,omitempty"`
	// LastOpenError is only ever set by a step that failed, so a progress note
	// can never be mistaken for the reason a window did not appear.
	LastOpenError string    `json:"lastOpenError,omitempty"`
	LastOpenAt    time.Time `json:"lastOpenAt,omitempty"`
	UpdatedAt     time.Time `json:"updatedAt,omitempty"`
}

type voiceControlSnapshot struct {
	Config  VoiceCallConfig   `json:"config"`
	Runtime VoiceRuntimeState `json:"runtime"`
	// WebView2 is the installed Microsoft Edge WebView2 Runtime version, or
	// empty when it is not installed. Its absence is the most common reason the
	// Google Voice window cannot be created, and it is worth saying so before
	// the user clicks Open rather than after.
	WebView2 string `json:"webView2,omitempty"`
	// AudioWarning is what is wrong with the sound path, in the words the
	// Settings page shows. Empty means a call would carry audio both ways.
	AudioWarning string `json:"audioWarning,omitempty"`
	// Audio is the automatic wiring currently in effect: which cable endpoint
	// stands in for each microphone and speaker, and which cable families are
	// in use. The Settings page shows it so "automatic" never means "opaque".
	Audio voiceCablePlan `json:"audio"`
	// CallAgents names the agents a call can currently be handed to. It is
	// derived rather than stored: an agent is on calls because somebody gave it
	// a number that may call. Showing it is how the desktop UI can say "nothing
	// would be answered yet" before a call is missed rather than after.
	CallAgents []string `json:"callAgents,omitempty"`
}

func voiceSnapshot(dataDir string, mainConfig func() Config) voiceControlSnapshot {
	vc := loadVoiceCallConfig(dataDir)
	rt := loadVoiceRuntime(dataDir)
	plan := applyCableOverrides(planVoiceCables(rt.Devices), vc, rt.Devices)
	snap := voiceControlSnapshot{
		Config:       vc,
		Runtime:      rt,
		WebView2:     platformWebView2Runtime(),
		Audio:        plan,
		AudioWarning: plan.Warning,
	}
	if mainConfig != nil {
		cfg := mainConfig()
		for _, agent := range []string{"C", "A"} {
			if voiceAgentOnCalls(vc, cfg, agent) {
				snap.CallAgents = append(snap.CallAgents, agentDisplayName(agent))
			}
		}
	}
	return snap
}

func defaultVoiceCallConfig() VoiceCallConfig {
	return VoiceCallConfig{
		DefaultAgent:   "C",
		GoogleVoiceURL: googleVoiceWebURL,
		Codex: VoiceAgentCallConfig{
			AppTitle: "ChatGPT",
		},
		Claude: VoiceAgentCallConfig{
			AppTitle: "Claude",
		},
	}
}

func voiceConfigPath(dataDir string) string  { return filepath.Join(dataDir, "voice-call.json") }
func voiceRuntimePath(dataDir string) string { return filepath.Join(dataDir, "voice-call-state.json") }
func voiceProfilePath(dataDir string) string { return filepath.Join(dataDir, "google-voice-webview") }
func voiceDockPath(dataDir string) string    { return filepath.Join(dataDir, "voice-dock.json") }

// VoiceDockRequest is where, on screen, the FlipAi window wants the Google
// Voice window to sit.
//
// Google Voice must keep running with FlipAi closed, so it cannot be a child of
// the FlipAi window; it lives in its own process with its own lifetime. But a
// second window popping up in front of the app is exactly what nobody wants to
// see. So the Connections page measures the empty panel it has reserved,
// reports that rectangle here several times a second, and the Google Voice
// window is moved -- borderless -- onto it. The result reads as one app with
// Google Voice embedded beside it, while remaining a window FlipAi can keep
// alive on its own.
//
// The rectangle is an offset inside the FlipAi window's client area, in
// physical pixels: the page reports where the panel sits in its own viewport,
// converted from CSS pixels with its device pixel ratio, and the window process
// turns that into a screen position from the FlipAi window itself. Going
// through the client area rather than the page's own idea of its screen
// position is what keeps the panel aligned to the pixel: a window frame, a
// title bar, or a display scale the page reports differently would otherwise
// shift it.
type VoiceDockRequest struct {
	// Visible is the page saying it currently wants the panel on screen.
	Visible bool `json:"visible"`
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Width   int  `json:"width"`
	Height  int  `json:"height"`
	// At is when the page last said so. A page that has navigated away, been
	// hidden, or closed stops saying it, and the dock expires by itself rather
	// than leaving a stranded window behind.
	At time.Time `json:"at"`
}

// voiceDockMinSize is the smallest panel worth docking. Anything smaller is a
// page mid-layout, not a place to put a browser.
const voiceDockMinSize = 120

// voiceDockTTL is how long one report keeps the panel docked. The page repeats
// itself about four times a second, so this is several missed reports -- long
// enough to survive a slow frame, short enough that closing the FlipAi window
// puts Google Voice away almost immediately.
const voiceDockTTL = 2500 * time.Millisecond

// Active reports whether this request should currently place a window.
func (d VoiceDockRequest) Active(now time.Time) bool {
	if !d.Visible || d.Width < voiceDockMinSize || d.Height < voiceDockMinSize {
		return false
	}
	if d.At.IsZero() {
		return false
	}
	return now.Sub(d.At) >= 0 && now.Sub(d.At) < voiceDockTTL
}

func normalizeVoiceDock(d VoiceDockRequest, now time.Time) VoiceDockRequest {
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	// A rectangle is only ever a position inside a window. Absurd values are
	// clamped rather than refused so a stray layout frame cannot break docking.
	d.X = clamp(d.X, 0, 32000)
	d.Y = clamp(d.Y, 0, 32000)
	d.Width = clamp(d.Width, 0, 32000)
	d.Height = clamp(d.Height, 0, 32000)
	d.At = now
	return d
}

// dockWriter keeps the panel position from becoming a stream of identical
// writes. The page repeats its rectangle four times a second so the dock never
// expires under it; a rectangle that has not moved only has to be written often
// enough to stay current.
type dockWriter struct {
	mu   sync.Mutex
	last VoiceDockRequest
}

func (d *dockWriter) shouldWrite(next VoiceDockRequest) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	same := d.last.Visible == next.Visible && d.last.X == next.X && d.last.Y == next.Y &&
		d.last.Width == next.Width && d.last.Height == next.Height
	if same && next.At.Sub(d.last.At) < voiceDockTTL/3 {
		return false
	}
	d.last = next
	return true
}

func saveVoiceDock(dataDir string, d VoiceDockRequest) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	tmp := voiceDockPath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, voiceDockPath(dataDir))
}

func loadVoiceDock(dataDir string) VoiceDockRequest {
	var d VoiceDockRequest
	if b, err := os.ReadFile(voiceDockPath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &d)
	}
	return d
}

func loadVoiceCallConfig(dataDir string) VoiceCallConfig {
	cfg := defaultVoiceCallConfig()
	b, err := os.ReadFile(voiceConfigPath(dataDir))
	if err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	cfg, _ = normalizeVoiceCallConfig(cfg, false)
	return cfg
}

func normalizeVoiceCallConfig(cfg VoiceCallConfig, strict bool) (VoiceCallConfig, error) {
	// This window is deliberately not a general-purpose browser. The stored URL
	// is always rewritten to Google Voice even if somebody hand-edits the JSON.
	cfg.GoogleVoiceURL = googleVoiceWebURL
	if cfg.DefaultAgent != "A" && cfg.DefaultAgent != "C" {
		cfg.DefaultAgent = "C"
	}
	clean := func(v string, max int) string {
		v = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(v, "\r", " "), "\n", " "))
		if len(v) > max {
			v = v[:max]
		}
		return strings.TrimSpace(v)
	}
	cfg.GoogleVoiceInput = clean(cfg.GoogleVoiceInput, 300)
	cfg.GoogleVoiceOutput = clean(cfg.GoogleVoiceOutput, 300)
	cfg.AgentInput = clean(cfg.AgentInput, 300)
	cfg.AgentOutput = clean(cfg.AgentOutput, 300)

	normalizeAgent := func(agent *VoiceAgentCallConfig, fallback string) error {
		agent.AppTitle = clean(agent.AppTitle, 120)
		if agent.AppTitle == "" {
			agent.AppTitle = fallback
		}
		agent.AppCommand = clean(agent.AppCommand, 500)
		agent.VoiceShortcut = clean(agent.VoiceShortcut, 80)
		return nil
	}
	if err := normalizeAgent(&cfg.Codex, "ChatGPT"); err != nil {
		return cfg, fmt.Errorf("ChatGPT/Codex callers: %w", err)
	}
	if err := normalizeAgent(&cfg.Claude, "Claude"); err != nil {
		return cfg, fmt.Errorf("Claude callers: %w", err)
	}
	// Whether any agent may take a call is deliberately not checked here. That
	// answer lives with the agents, changes without this file being written, and
	// refusing the save over it would keep the window from starting itself --
	// which is the one thing this switch exists to do. A call that reaches no
	// agent says so on the call, and the desktop UI says so before that.
	_ = strict
	return cfg, nil
}

// voiceConfigMu serializes read-modify-write of the voice configuration. Two
// endpoints change it -- the switch and the rest of the card -- and both rewrite
// the whole file, so without this the later read of one could overwrite the
// write of the other.
var voiceConfigMu sync.Mutex

// updateVoiceCallConfig applies one change to the stored configuration and
// returns what was saved.
func updateVoiceCallConfig(dataDir string, fn func(*VoiceCallConfig)) (VoiceCallConfig, error) {
	voiceConfigMu.Lock()
	defer voiceConfigMu.Unlock()
	cfg := loadVoiceCallConfig(dataDir)
	fn(&cfg)
	if err := saveVoiceCallConfig(dataDir, cfg); err != nil {
		return cfg, err
	}
	return loadVoiceCallConfig(dataDir), nil
}

func saveVoiceCallConfig(dataDir string, cfg VoiceCallConfig) error {
	var err error
	cfg, err = normalizeVoiceCallConfig(cfg, true)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := voiceConfigPath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, voiceConfigPath(dataDir))
}

// genericCallerLabels are the placeholders a network supplies when there is no
// caller ID at all. Allowing one would turn the name allowlist into "answer
// anything", so they are refused at save time rather than silently matching.
var genericCallerLabels = map[string]bool{
	"unknown": true, "unknown caller": true, "unknown number": true,
	"private": true, "private caller": true, "private number": true,
	"anonymous": true, "no caller id": true, "restricted": true,
	"blocked": true, "unavailable": true, "spam": true, "maybe: spam": true,
}

// normalizeCallerLabel collapses the caller ID text Google Voice renders into a
// single stable line so that display whitespace does not decide whether a call
// is authorized.
func normalizeCallerLabel(v string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(v, "\u00a0", " ")), " ")
}

// normalizeAllowedCallerLabels rejects placeholder names when saving, but only
// drops them when loading: a hand-edited file must never stop the rest of the
// configuration from being read.
func normalizeAllowedCallerLabels(raw string, strict bool) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n") {
		name := normalizeCallerLabel(line)
		if name == "" {
			continue
		}
		if len(name) > 120 {
			name = strings.TrimSpace(name[:120])
		}
		key := strings.ToLower(name)
		if genericCallerLabels[key] {
			if strict {
				return nil, fmt.Errorf("%q is what Google Voice shows when there is no caller ID, so it cannot be an allowed caller name", name)
			}
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out, nil
}

func allowedCallerLabel(raw, label string) bool {
	label = strings.ToLower(normalizeCallerLabel(label))
	if label == "" || genericCallerLabels[label] {
		return false
	}
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n") {
		if name := strings.ToLower(normalizeCallerLabel(line)); name != "" && name == label {
			return true
		}
	}
	return false
}

// voiceAgentOnCalls reports whether an agent may take phone calls at all.
//
// The per-agent switch on the voice card is not the only way to say yes: giving
// an agent a number marked "Texts and calls" or "Calls only" is the same
// statement, made on the page where numbers actually live. Requiring both was a
// hidden second gate, so a number the user had explicitly allowed to call still
// produced "No agent is allowed on phone calls yet". Either one is enough now.
func voiceAgentOnCalls(vc VoiceCallConfig, cfg Config, agent string) bool {
	own := vc.Codex
	if agent == "A" {
		own = vc.Claude
	}
	if own.Enabled {
		return true
	}
	return agentTakesVoice(agentSettings(cfg, agent))
}

func agentTakesVoice(s AgentSettings) bool {
	for _, p := range s.Phones {
		if p.AllowsVoice() {
			return true
		}
	}
	names, _ := normalizeAllowedCallerLabels(s.CallerNames, false)
	return len(names) > 0
}

func voiceAgentAccepts(cfg Config, agent, number, label string) bool {
	settings := agentSettings(cfg, agent)
	if number != "" {
		for _, p := range settings.Phones {
			if p.Number == number {
				return p.AllowsVoice()
			}
		}
	}
	return allowedCallerLabel(settings.CallerNames, label)
}

// voiceCallDecision is the whole authorization answer for one ring, including
// why a call was refused. The reason is shown in the desktop UI: a call that
// silently does nothing is the single most confusing failure this feature has.
type voiceCallDecision struct {
	Agent   string
	Allowed bool
	Reason  string
}

// decideVoiceCall answers one ring. Who may call is the same list that decides
// who may text, held on the agent, so a number is allowed in one place and
// carries what it is allowed to do.
func decideVoiceCall(vc VoiceCallConfig, cfg Config, caller, label string) voiceCallDecision {
	if !vc.Enabled {
		return voiceCallDecision{Reason: "Phone voice is turned off in FlipAi settings."}
	}
	codexOn := voiceAgentOnCalls(vc, cfg, "C")
	claudeOn := voiceAgentOnCalls(vc, cfg, "A")
	if !codexOn && !claudeOn {
		return voiceCallDecision{Reason: "No agent can take phone calls yet. On the Agents page, give an agent a phone number set to \"Texts and calls\" or \"Calls only\"."}
	}
	number := normalizeUSPhone(caller)
	label = normalizeCallerLabel(label)
	codexOK := codexOn && voiceAgentAccepts(cfg, "C", number, label)
	claudeOK := claudeOn && voiceAgentAccepts(cfg, "A", number, label)
	switch {
	case codexOK && claudeOK:
		// A number belongs to one agent, so this only happens through a caller
		// name listed on both. The default agent settles it.
		if cfg.DefaultAgent == "A" {
			return voiceCallDecision{Agent: "A", Allowed: true}
		}
		return voiceCallDecision{Agent: "C", Allowed: true}
	case codexOK:
		return voiceCallDecision{Agent: "C", Allowed: true}
	case claudeOK:
		return voiceCallDecision{Agent: "A", Allowed: true}
	}
	if number != "" {
		if agent, phone, known := agentForSender(cfg, number); known && !phone.AllowsVoice() {
			return voiceCallDecision{Reason: fmt.Sprintf("%s is allowed to text %s but not to call it. Change that number to \"Texts and calls\" or \"Calls only\" under Agents.", formatUSPhone(number), agentDisplayName(agent))}
		} else if known {
			return voiceCallDecision{Reason: fmt.Sprintf("%s reaches %s, but phone calls are not switched on for that agent.", formatUSPhone(number), agentDisplayName(agent))}
		}
	}
	switch {
	case number == "" && label == "":
		return voiceCallDecision{Reason: "Google Voice showed no caller ID for this call, so FlipAi could not match it to an agent."}
	case number == "":
		return voiceCallDecision{Reason: fmt.Sprintf("Google Voice showed %q instead of a phone number, which usually means the caller is in your Google Contacts. Add that exact name under the agent's Allowed caller names to let it through.", label)}
	default:
		return voiceCallDecision{Reason: fmt.Sprintf("%s is not allowed on any agent.", formatUSPhone(number))}
	}
}

// voiceWindowStartup is how long the window is given to appear. WebView2's
// first run in a fresh profile unpacks and initializes before anything is drawn,
// which on a cold machine is far slower than the steady-state case.
const voiceWindowStartup = 40 * time.Second

// superviseVoiceShouldStart decides, on one tick, whether the supervisor should
// start the Google Voice window.
//
// It is the whole decision, and it deliberately has no matching "should close".
// The supervisor used to close the window on every tick where calling was
// switched off -- which meant a window opened deliberately, to sign in to
// Google before switching calling on, was shut four seconds later by a
// background loop. From the user's side a window opened and closed by itself,
// with nothing to explain it. Turning calling off already closes the window at
// the moment it is turned off, which is the only moment that means anything;
// the standing rule only ever destroyed windows somebody had asked for.
func superviseVoiceShouldStart(enabled, hasWindow bool, sinceLastAttempt time.Duration) bool {
	return enabled && !hasWindow && sinceLastAttempt > voiceWindowStartup
}

var voiceRuntimeMu sync.Mutex

func loadVoiceRuntime(dataDir string) VoiceRuntimeState {
	voiceRuntimeMu.Lock()
	defer voiceRuntimeMu.Unlock()
	return loadVoiceRuntimeUnlocked(dataDir)
}

func loadVoiceRuntimeUnlocked(dataDir string) VoiceRuntimeState {
	var s VoiceRuntimeState
	if b, err := os.ReadFile(voiceRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func mutateVoiceRuntime(dataDir string, fn func(*VoiceRuntimeState)) {
	voiceRuntimeMu.Lock()
	defer voiceRuntimeMu.Unlock()
	s := loadVoiceRuntimeUnlocked(dataDir)
	fn(&s)
	s.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	tmp := voiceRuntimePath(dataDir) + ".tmp"
	if os.WriteFile(tmp, b, 0600) == nil {
		_ = os.Rename(tmp, voiceRuntimePath(dataDir))
	}
}

// openGoogleVoiceWindow is indirected through a variable so the contract that
// matters -- a failure reaching the button that started it -- can be tested
// without launching a real window on the machine running the tests.
var openGoogleVoiceWindow = platformOpenGoogleVoice
var signOutGoogleVoice = platformSignOutGoogleVoice

// beginVoiceOpen starts one attempt at putting the window on screen, and is the
// only thing that clears the reason the last attempt failed.
//
// recordVoiceOpen used to clear it on every note it wrote, failure or not. Two
// processes write these notes -- the one handling the click and the one that
// owns the window -- so a progress note from one could erase the reason the
// other had just recorded. That is how a window process that died on startup
// could leave behind "window opened" and nothing else: the click reported
// success, the window was gone, and the explanation had been overwritten.
func beginVoiceOpen(dataDir, outcome string) {
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.LastOpen = outcome
		s.LastOpenAt = time.Now()
		s.LastOpenError = ""
	})
}

// recordVoiceOpen leaves a trail for one step of opening the window. The window
// lives in its own process, so the process handling the click cannot see why the
// other one failed unless that one writes it down.
func recordVoiceOpen(dataDir, outcome string, err error) {
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.LastOpen = outcome
		s.LastOpenAt = time.Now()
		if err != nil {
			s.LastOpen = outcome + ": " + err.Error()
			s.LastOpenError = s.LastOpen
			s.LastError = err.Error()
			s.LastEvent = "open-failed"
		}
	})
}

// lastVoiceOpenFailure returns why the window process gave up, when that was
// recorded during the attempt currently being waited on. Anything older belongs
// to a previous attempt and would only mislead.
func lastVoiceOpenFailure(dataDir string, since time.Time) string {
	s := loadVoiceRuntime(dataDir)
	if s.LastOpenError != "" && !s.LastOpenAt.Before(since) {
		return s.LastOpenError
	}
	return ""
}

func voiceOriginAllowed(origin, mainListen string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host, port, err := net.SplitHostPort(mainListen)
	if err != nil {
		return false
	}
	originHost := u.Hostname()
	originPort := u.Port()
	if originPort != port {
		return false
	}
	if host != "127.0.0.1" && !strings.EqualFold(host, "localhost") {
		return false
	}
	return originHost == "127.0.0.1" || strings.EqualFold(originHost, "localhost")
}

// startVoiceControlServer exposes the voice-only settings to FlipAi's own
// desktop WebView. It binds to loopback and additionally requires the browser
// Origin to be the authenticated local FlipAi UI. Google Voice itself therefore
// cannot call these endpoints even though it is hosted by another WebView in
// the same executable.
func startVoiceControlServer(dataDir, mainConfigPath, statePath string) {
	mainCfg, err := loadConfig(mainConfigPath, dataDir)
	if err != nil {
		mainCfg = defaultConfig(dataDir)
	}
	handler := voiceControlHandler(dataDir, mainCfg.Listen, func() Config {
		// Reloaded per request: numbers are added on the Agents page while this
		// server is running, and a stale copy would report the old answer.
		cfg, err := loadConfig(mainConfigPath, dataDir)
		if err != nil {
			return defaultConfig(dataDir)
		}
		return cfg
	}, activityLogForStatePath(statePath))
	server := &http.Server{Addr: voiceControlListen, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
				s.LastError = "Voice settings service: " + err.Error()
				s.LastEvent = "control-error"
			})
		}
	}()
}

// voiceControlHandler is split out so the endpoints the desktop UI calls can be
// exercised directly. The Open path in particular has to carry a failure back to
// the button that started it, and that is only worth anything if it is tested.
func voiceControlHandler(dataDir, mainListen string, mainConfig func() Config, activity *ActivityLog) http.Handler {
	mux := http.NewServeMux()
	dockWrites := &dockWriter{}
	withCORS := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if !voiceOriginAllowed(origin, mainListen) {
				http.Error(w, "FlipAi voice control is local-only", http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next(w, r)
		}
	}
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/status", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, voiceSnapshot(dataDir, mainConfig))
	}))
	mux.HandleFunc("/config", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var sent VoiceCallConfig
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&sent); err != nil {
			http.Error(w, "Could not read voice settings: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Whether FlipAi answers the phone belongs to /enable and to nothing
		// else. The page sends the whole card, and a card assembled a moment
		// before the switch was touched still carries the old value; letting
		// that through would switch calling back off behind the user -- the
		// very symptom this card exists to end.
		saved, err := updateVoiceCallConfig(dataDir, func(cfg *VoiceCallConfig) {
			enabled := cfg.Enabled
			*cfg = sent
			cfg.Enabled = enabled
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		platformVoiceConfigChanged(dataDir, saved)
		writeJSON(w, voiceSnapshot(dataDir, mainConfig))
	}))
	// /enable exists so the one switch that decides whether FlipAi answers the
	// phone can never be held up by anything else on the page. It writes that
	// field and nothing else, so a half-filled audio section, a device that has
	// gone away, or a typo in a window title cannot stop calling being turned
	// on -- which is precisely what used to happen when the switch travelled
	// inside the whole-card save.
	mux.HandleFunc("/enable", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
			http.Error(w, "Could not read the setting: "+err.Error(), http.StatusBadRequest)
			return
		}
		saved, err := updateVoiceCallConfig(dataDir, func(cfg *VoiceCallConfig) {
			cfg.Enabled = body.Enabled
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if saved.Enabled {
			activity.Add("info", "voice", "Google Voice calling turned on; the window will be kept running.", "", "", "")
		} else {
			activity.Add("info", "voice", "Google Voice calling turned off.", "", "", "")
		}
		platformVoiceConfigChanged(dataDir, saved)
		writeJSON(w, voiceSnapshot(dataDir, mainConfig))
	}))
	// /dock is the FlipAi window saying where on screen it has reserved room
	// for Google Voice. See VoiceDockRequest: this is what puts the browser
	// inside the app instead of in a window of its own.
	mux.HandleFunc("/dock", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		var d VoiceDockRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&d); err != nil {
			http.Error(w, "Could not read the panel position: "+err.Error(), http.StatusBadRequest)
			return
		}
		d = normalizeVoiceDock(d, time.Now())
		if dockWrites.shouldWrite(d) {
			if err := saveVoiceDock(dataDir, d); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		// A page asking for the panel is a page that wants the window running.
		// Starting it here is what makes the embedded view appear on its own,
		// with no Open button to find first.
		if d.Active(time.Now()) && loadVoiceCallConfig(dataDir).Enabled {
			platformEnsureGoogleVoice(dataDir)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/open", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		// Google Voice has no window of its own to open any more: it lives in
		// the FlipAi panel or nowhere. This makes sure it is running and
		// standing where the page asked for it.
		if err := openGoogleVoiceWindow(dataDir, true); err != nil {
			activity.Add("error", "voice", "Open Google Voice failed: "+truncate(err.Error(), 300), "", "", "")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		activity.Add("info", "voice", "Google Voice window is open.", "", "", "")
		// The note carries how it opened. Windows can refuse to bring a window
		// forward for a background process, and a window that opened behind the
		// one being looked at is indistinguishable from nothing happening.
		writeJSON(w, map[string]any{"ok": true, "note": loadVoiceRuntime(dataDir).LastOpen})
	}))
	// /restart is the Retry on the empty panel. A window that failed to load
	// leaves a process holding the single-instance mutex, so "try again" has to
	// mean "take that one down first" rather than "ask the one that is stuck".
	mux.HandleFunc("/restart", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		beginVoiceOpen(dataDir, "restarting the Google Voice window")
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.BrowserRunning = false
			s.Docked = false
			s.LastError = ""
			// Retry is the user saying "that did not work", so the next window
			// is drawn a different way rather than the same way again.
			s.RenderAttempt++
		})
		activity.Add("info", "voice", "Restarting the Google Voice window.", "", "", "")
		platformRestartGoogleVoice(dataDir)
		writeJSON(w, voiceSnapshot(dataDir, mainConfig))
	}))
	// /signout forgets the Google account the Google Voice window is signed in
	// to, by closing the window and deleting its browser profile. The window
	// comes back signed out (and is kept running if calling is on), so signing
	// in as somebody else is just the normal sign-in again.
	mux.HandleFunc("/signout", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if err := signOutGoogleVoice(dataDir); err != nil {
			activity.Add("error", "voice", "Google Voice sign-out failed: "+truncate(err.Error(), 300), "", "", "")
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		activity.Add("info", "voice", "Signed out of Google Voice; the saved browser session was removed.", "", "", "")
		writeJSON(w, voiceSnapshot(dataDir, mainConfig))
	}))
	mux.HandleFunc("/test-agent", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		agent := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("agent")))
		if agent != "A" && agent != "C" {
			http.Error(w, "choose agent A or C", http.StatusBadRequest)
			return
		}
		if err := platformTestAgentVoice(loadVoiceCallConfig(dataDir), agent); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}))

	return mux
}

// voiceBridge is the one thing that acts on a call.
//
// It owns the call state machine, turns the effects the machine emits into
// real work, and is the only writer of the call fields in the runtime state
// file. Everything that can see the Google Voice page -- the script injected
// into it, and FlipAi's own control channel watching from outside -- reports
// what it sees here, and neither of them decides anything.
//
// It deliberately contains no Windows types: the same code that runs behind the
// WebView2 bindings is what the tests and the headless browser harness
// exercise, so ring/answer/bridge/hang-up is verifiable without a phone line.
type voiceBridge struct {
	dataDir string
	// mainConfig supplies the agents, because who may call an agent is the same
	// list as who may text it.
	mainConfig func() Config
	activate   func(cfg VoiceCallConfig, agent string) error
	deactivate func(cfg VoiceCallConfig, agent string) error

	// press is how Answer is pressed, one rung of the ladder per attempt. It is
	// nil where there is no browser to press anything in, which is every test
	// that is not the browser harness; the page's own click still happens.
	press func(effect voiceCallEffect) error
	// route points the desktop app's audio at the cables. Nil off Windows.
	route func(cfg VoiceCallConfig, agent string)

	machine *voiceCallMachine

	mu           sync.Mutex
	agentWork    chan func()
	answerWork   chan func()
	pendingPress *voiceCallEffect
}

func newVoiceBridge(dataDir string, mainConfig func() Config, activate, deactivate func(VoiceCallConfig, string) error) *voiceBridge {
	if mainConfig == nil {
		mainConfig = func() Config { return Config{} }
	}
	b := &voiceBridge{dataDir: dataDir, mainConfig: mainConfig, activate: activate, deactivate: deactivate}
	// Authorization is answered fresh for every ring, from the configuration on
	// disk, so a number added on the Agents page a moment ago is already in
	// force and a number removed is already gone.
	b.machine = newVoiceCallMachine(func(caller, label string) voiceCallDecision {
		return decideVoiceCall(loadVoiceCallConfig(b.dataDir), b.mainConfig(), caller, label)
	})
	return b
}

// RunEffectsInBackground moves the slow effects off the caller's thread.
//
// The receiver calls this because its bindings run on the window's own message
// thread: starting a desktop voice session takes seconds, and doing that inline
// would freeze the Google Voice window for the whole of it -- during a call,
// with the caller listening. Answering has its own queue so it can never end up
// waiting behind a desktop app that is slow to come up.
func (b *voiceBridge) RunEffectsInBackground() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.agentWork != nil {
		return
	}
	b.agentWork = make(chan func(), 32)
	b.answerWork = make(chan func(), 8)
	for _, queue := range []chan func(){b.agentWork, b.answerWork} {
		go func(q chan func()) {
			for fn := range q {
				fn()
			}
		}(queue)
	}
}

// Drain waits for the queued effects to finish.
//
// It exists for one moment: the Google Voice view closing, after which this
// process exits. A teardown that was only queued at that point would never run,
// and the desktop app would be left in voice mode with nobody on the line.
func (b *voiceBridge) Drain(timeout time.Duration) {
	b.mu.Lock()
	queue := b.agentWork
	b.mu.Unlock()
	if queue == nil {
		return
	}
	done := make(chan struct{})
	select {
	case queue <- func() { close(done) }:
	case <-time.After(timeout):
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// pressLatest keeps at most one queued answer attempt, replacing whatever was
// waiting.
func (b *voiceBridge) pressLatest(effect voiceCallEffect) {
	b.mu.Lock()
	b.pendingPress = &effect
	queue := b.answerWork
	b.mu.Unlock()

	take := func() {
		b.mu.Lock()
		next := b.pendingPress
		b.pendingPress = nil
		b.mu.Unlock()
		if next == nil {
			return
		}
		if err := b.press(*next); err != nil {
			mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
				s.LastError = "FlipAi is trying to answer an allowed caller and has not managed it yet: " + truncate(err.Error(), 300)
			})
		}
	}
	if queue == nil {
		take()
		return
	}
	select {
	case queue <- take:
	default:
		// A press is already running and another is already queued behind it.
		// The one just recorded will be picked up by whichever of them runs
		// next, so there is nothing to add.
	}
}

func (b *voiceBridge) schedule(queue chan func(), fn func()) {
	if queue == nil {
		fn()
		return
	}
	select {
	case queue <- fn:
	default:
		// The queue only backs up behind a desktop app that has stopped
		// responding. Losing a teardown would leave it listening to the cable
		// for the next caller, so the work is still done, just not in order.
		go fn()
	}
}

// AudioSettings tells the page which endpoints Google Voice must use. They are
// the automatically chosen cable ends -- nobody picks them -- and the page
// caches this; it is not a per-element lookup.
func (b *voiceBridge) AudioSettings() map[string]string {
	plan := currentVoiceCablePlan(b.dataDir)
	return map[string]string{"input": plan.GoogleVoiceInput, "output": plan.GoogleVoiceOutput, "ring": ""}
}

// Devices is the page reporting the machine's audio endpoints. It is what the
// cable detection reads, and a change to it is the moment to re-point the
// desktop app's per-app audio at the cables.
func (b *voiceBridge) Devices(raw string) {
	var devices []VoiceAudioDevice
	if json.Unmarshal([]byte(raw), &devices) != nil {
		return
	}
	if len(devices) > 80 {
		devices = devices[:80]
	}
	named := devices[:0]
	hidden := false
	for _, d := range devices {
		if d.Kind != "audioinput" && d.Kind != "audiooutput" {
			continue
		}
		// An endpoint with no name is one the browser is hiding until the
		// microphone permission is granted. Inventing "Microphone 2" here would
		// feed placeholders into the cable detection and make the real problem
		// invisible, so unnamed endpoints are counted, not renamed.
		if strings.TrimSpace(d.Label) == "" {
			hidden = true
			continue
		}
		named = append(named, d)
	}
	mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
		s.Devices = named
		s.DeviceLabelsHidden = hidden && len(named) == 0
	})
	platformVoiceDevicesChanged(b.dataDir)
}

// Observe is the single entrance for everything that can see the call.
func (b *voiceBridge) Observe(obs voiceObservation) voiceCallStatus {
	effects := b.machine.Observe(obs, time.Now())
	b.run(effects)
	status := b.machine.Status()
	b.writeCallState(status)
	return status
}

// Incoming answers one question for the page: should it click Answer? The
// answer is the authorization decision and nothing else -- an authorized caller
// is always answered, an unauthorized one never is; there is no separate
// auto-answer mode to find.
//
// FlipAi presses Answer through its own control channel as well. Pressing a
// ringing call's Answer control twice does nothing the second time, and having
// two independent ways to press it is what stops one wedged page from sending
// an allowed caller to voicemail.
func (b *voiceBridge) Incoming(caller, label string) bool {
	status := b.Observe(voiceObservation{Answer: true, Caller: caller, Label: label})
	return status.Phase == voicePhaseRinging
}

// Answered runs when a call is actually up, however it was answered. A call the
// user picked up by hand still gets bridged if the caller is authorized.
func (b *voiceBridge) Answered(caller, label string) bool {
	status := b.Observe(voiceObservation{InCall: true, Caller: caller, Label: label})
	return status.InCall() && status.Agent != ""
}

// Ended is the page saying the call is gone. It is a definite statement -- the
// page watched the hang-up control disappear -- so it ends the call outright
// rather than waiting out the debounce the polled observations use.
func (b *voiceBridge) Ended() {
	b.run(b.machine.End(time.Now()))
	b.writeCallState(b.machine.Status())
}

func (b *voiceBridge) Page(href string, signedIn bool, controls string) {
	if len(controls) > 2000 {
		controls = controls[:2000]
	}
	mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
		s.BrowserRunning = true
		s.Page = href
		s.SignedIn = signedIn
		s.Controls = controls
		if s.LastEvent == "" || s.LastEvent == "browser-starting" {
			s.LastEvent = "browser-ready"
		}
	})
}

// Status is the call as the machine sees it.
func (b *voiceBridge) Status() voiceCallStatus { return b.machine.Status() }

// run carries out the effects one observation produced.
func (b *voiceBridge) run(effects []voiceCallEffect) {
	cfg := loadVoiceCallConfig(b.dataDir)
	for _, effect := range effects {
		effect := effect
		switch effect.Kind {
		case voiceEffectAnswer:
			if b.press == nil {
				continue
			}
			// At most one press is ever waiting. The forceful rungs of the
			// ladder take a second or two, and the machine keeps asking while
			// the phone rings; a queue of them would still be draining after
			// the caller had given up. The newest attempt replaces the one
			// waiting because it is the one that reflects the card on screen
			// now.
			b.pressLatest(effect)
		case voiceEffectRouteAudio:
			if b.route == nil {
				continue
			}
			agent := effect.Agent
			b.schedule(b.agentWork, func() { b.route(cfg, agent) })
		case voiceEffectStartAgentVoice:
			b.schedule(b.agentWork, func() { b.startAgentVoice(cfg, effect) })
		case voiceEffectStopAgentVoice:
			b.schedule(b.agentWork, func() { b.stopAgentVoice(cfg, effect) })
		}
	}
}

func (b *voiceBridge) startAgentVoice(cfg VoiceCallConfig, effect voiceCallEffect) {
	var err error
	if b.activate != nil {
		err = b.activate(cfg, effect.Agent)
	}
	b.machine.AgentVoiceResult(effect.Session, err)
	status := b.machine.Status()
	if err != nil {
		mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
			s.LastError = "The call is connected but the desktop voice session did not start: " + truncate(err.Error(), 400)
		})
	}
	b.writeCallState(status)
}

func (b *voiceBridge) stopAgentVoice(cfg VoiceCallConfig, effect voiceCallEffect) {
	var err error
	if b.deactivate != nil && (effect.Agent == "A" || effect.Agent == "C") {
		err = b.deactivate(cfg, effect.Agent)
	}
	b.machine.AgentVoiceStopped(effect.Session)
	if err != nil {
		mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
			s.LastError = "The call ended but the desktop voice session may still be running: " + truncate(err.Error(), 300)
		})
	}
	b.writeCallState(b.machine.Status())
}

// writeCallState is the only place the call fields in the runtime file are
// written, so nothing can leave a stale "in a call" behind.
func (b *voiceBridge) writeCallState(status voiceCallStatus) {
	audioProblem := ""
	if status.InCall() || status.Phase == voicePhaseRinging {
		audioProblem = currentVoiceCablePlan(b.dataDir).Warning
	}
	now := time.Now()
	mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
		s.InCall = status.InCall()
		s.Caller = status.Caller
		s.CallerLabel = status.Label
		s.Agent = status.Agent
		s.Blocked = status.Refused
		s.CallPhase = string(status.Phase)
		s.CallNote = voiceCallStatusNote(status)
		if status.Event != "" {
			s.LastEvent = status.Event
		}
		if status.Phase == voicePhaseRinging || status.Phase == voicePhaseRefused {
			s.LastRingAt = now
		}
		switch {
		case audioProblem != "":
			// The call is up and the agent is listening, but nothing carries
			// the sound between them yet.
			if status.InCall() {
				s.LastError = "The call was answered but the audio path is not usable. " + audioProblem
			}
		case status.Phase == voicePhaseIdle, status.Phase == voicePhaseLive:
			// A finished call and a working call both have nothing wrong with
			// them; anything left over belongs to something that is over.
			s.LastError = ""
		}
	})
}
