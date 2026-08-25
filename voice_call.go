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
	AutoAnswer   bool   `json:"autoAnswer"`
	DefaultAgent string `json:"defaultAgent"` // C = ChatGPT/Codex, A = Claude Desktop

	GoogleVoiceURL    string `json:"googleVoiceUrl"`
	GoogleVoiceInput  string `json:"googleVoiceInput"`  // virtual capture endpoint Google Voice uses as its mic
	GoogleVoiceOutput string `json:"googleVoiceOutput"` // virtual render endpoint Google Voice uses as its speaker
	AgentInput        string `json:"agentInput"`        // paired virtual capture endpoint selected in the AI app
	AgentOutput       string `json:"agentOutput"`       // paired virtual render endpoint selected in the AI app
	RingOutput        string `json:"ringOutput,omitempty"`

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

type VoiceAudioDevice struct {
	Kind     string `json:"kind"` // audioinput or audiooutput
	DeviceID string `json:"deviceId,omitempty"`
	Label    string `json:"label"`
}

type VoiceRuntimeState struct {
	BrowserRunning bool   `json:"browserRunning"`
	SignedIn       bool   `json:"signedIn"`
	Page           string `json:"page,omitempty"`
	InCall         bool   `json:"inCall"`
	Caller         string `json:"caller,omitempty"`
	// CallerLabel is the raw caller ID text Google Voice displayed. It is kept
	// so a blocked call can say what FlipAi actually saw instead of leaving the
	// user guessing why nothing happened.
	CallerLabel string             `json:"callerLabel,omitempty"`
	Agent       string             `json:"agent,omitempty"`
	Blocked     string             `json:"blocked,omitempty"`
	Devices     []VoiceAudioDevice `json:"devices,omitempty"`
	// DeviceLabelsHidden records that the browser returned endpoints with no
	// names, which happens until the microphone permission is actually granted.
	// Without it the settings page looks merely empty rather than blocked.
	DeviceLabelsHidden bool   `json:"deviceLabelsHidden,omitempty"`
	LastError          string `json:"lastError,omitempty"`
	LastEvent          string `json:"lastEvent,omitempty"`
	// LastOpen is the outcome of the most recent attempt to put the Google
	// Voice window on screen. Opening it spans two processes, so without this a
	// click that produced nothing leaves nothing behind to explain itself.
	// Controls is what the Google Voice page currently offers, and LastRingAt is
	// when an answer control was last seen. Together they answer "is a call even
	// reaching this window", which is otherwise invisible.
	Controls   string    `json:"controls,omitempty"`
	LastRingAt time.Time `json:"lastRingAt,omitempty"`

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
	// CallAgents names the agents a call can currently be handed to. It is
	// derived rather than stored: an agent is on calls because somebody gave it
	// a number that may call. Showing it is how the desktop UI can say "nothing
	// would be answered yet" before a call is missed rather than after.
	CallAgents []string `json:"callAgents,omitempty"`
}

func voiceSnapshot(dataDir string, mainConfig func() Config) voiceControlSnapshot {
	vc := loadVoiceCallConfig(dataDir)
	snap := voiceControlSnapshot{
		Config:   vc,
		Runtime:  loadVoiceRuntime(dataDir),
		WebView2: platformWebView2Runtime(),
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
		AutoAnswer:     true,
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

// audioBridgeReady reports whether a bridged call would actually carry sound.
func audioBridgeReady(cfg VoiceCallConfig) bool {
	return cfg.GoogleVoiceInput != "" && cfg.GoogleVoiceOutput != ""
}

func voiceConfigPath(dataDir string) string  { return filepath.Join(dataDir, "voice-call.json") }
func voiceRuntimePath(dataDir string) string { return filepath.Join(dataDir, "voice-call-state.json") }
func voiceProfilePath(dataDir string) string { return filepath.Join(dataDir, "google-voice-webview") }

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
	cfg.RingOutput = clean(cfg.RingOutput, 300)

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
	if strict && cfg.Enabled {
		if err := validateVoiceAudioBridge(cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// validateVoiceAudioBridge rejects the wiring mistakes that leave a call
// connected but silent. The conversation needs two separate virtual cables:
// one carrying the caller to the AI app, one carrying the AI app back to the
// caller. Pointing both ends of a direction at the same endpoint is the
// classic setup error, and it produces a call in which nobody hears anything.
// validateVoiceAudioBridge rejects wiring that is wrong, not wiring that is
// merely absent.
//
// It used to refuse to save unless both virtual endpoints were chosen, which
// made the whole feature unreachable on a PC with no cable driver installed:
// the switch that keeps Google Voice running -- and the switch every call is
// checked against -- could not be turned on at all. Missing endpoints are now
// reported on the call itself, which is answered either way; only a
// contradiction is refused.
func validateVoiceAudioBridge(cfg VoiceCallConfig) error {
	if cfg.AgentOutput != "" && strings.EqualFold(cfg.AgentOutput, cfg.GoogleVoiceOutput) {
		return errors.New("Google Voice and the AI app cannot share one speaker endpoint; each direction of the conversation needs its own virtual cable")
	}
	if cfg.AgentInput != "" && strings.EqualFold(cfg.AgentInput, cfg.GoogleVoiceInput) {
		return errors.New("Google Voice and the AI app cannot share one microphone endpoint; each direction of the conversation needs its own virtual cable")
	}
	return nil
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

// recordVoiceOpen leaves a trail for one step of opening the window. The window
// lives in its own process, so the process handling the click cannot see why the
// other one failed unless that one writes it down.
func recordVoiceOpen(dataDir, outcome string, err error) {
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.LastOpen = outcome
		s.LastOpenAt = time.Now()
		s.LastOpenError = ""
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
		var cfg VoiceCallConfig
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&cfg); err != nil {
			http.Error(w, "Could not read voice settings: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := saveVoiceCallConfig(dataDir, cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		platformVoiceConfigChanged(dataDir, loadVoiceCallConfig(dataDir))
		writeJSON(w, voiceSnapshot(dataDir, mainConfig))
	}))
	mux.HandleFunc("/open", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
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

// voiceBridge holds every decision the injected Google Voice page asks FlipAi
// to make during a call. It deliberately contains no Windows types: the same
// code that runs behind the WebView2 bindings is what the tests and the browser
// harness exercise, so the ring/answer/bridge/hang-up path is verifiable
// without a phone line.
type voiceBridge struct {
	dataDir string
	// mainConfig supplies the agents, because who may call an agent is the same
	// list as who may text it.
	mainConfig func() Config
	activate   func(cfg VoiceCallConfig, agent string) error
	deactivate func(cfg VoiceCallConfig, agent string) error

	mu    sync.Mutex
	agent string
}

func newVoiceBridge(dataDir string, mainConfig func() Config, activate, deactivate func(VoiceCallConfig, string) error) *voiceBridge {
	if mainConfig == nil {
		mainConfig = func() Config { return Config{} }
	}
	return &voiceBridge{dataDir: dataDir, mainConfig: mainConfig, activate: activate, deactivate: deactivate}
}

// AudioSettings tells the page which Windows endpoints Google Voice must use.
// The page caches this; it is not a per-element lookup.
func (b *voiceBridge) AudioSettings() map[string]string {
	cfg := loadVoiceCallConfig(b.dataDir)
	return map[string]string{"input": cfg.GoogleVoiceInput, "output": cfg.GoogleVoiceOutput, "ring": cfg.RingOutput}
}

// Incoming answers one question: should the page click Answer? It records what
// Google Voice displayed either way so a refused call is explainable.
func (b *voiceBridge) Incoming(caller, label string) bool {
	cfg := loadVoiceCallConfig(b.dataDir)
	d := decideVoiceCall(cfg, b.mainConfig(), caller, label)
	mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
		s.LastRingAt = time.Now()
		s.Caller = normalizeUSPhone(caller)
		s.CallerLabel = normalizeCallerLabel(label)
		s.Agent = d.Agent
		s.Blocked = d.Reason
		if d.Allowed {
			s.LastEvent = "authorized-call-ringing"
		} else {
			s.LastEvent = "blocked-call-ringing"
		}
	})
	return d.Allowed && cfg.AutoAnswer
}

// Answered runs when a call is actually up, however it was answered. A call the
// user picked up by hand still gets bridged if the caller is authorized.
func (b *voiceBridge) Answered(caller, label string) bool {
	cfg := loadVoiceCallConfig(b.dataDir)
	d := decideVoiceCall(cfg, b.mainConfig(), caller, label)
	number := normalizeUSPhone(caller)
	name := normalizeCallerLabel(label)
	if !d.Allowed {
		b.setAgent("")
		mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
			s.InCall = true
			s.Caller = number
			s.CallerLabel = name
			s.Agent = ""
			s.Blocked = d.Reason
			s.LastEvent = "unbridged-call"
		})
		return false
	}
	if err := b.activate(cfg, d.Agent); err != nil {
		// The agent is still recorded so hang-up can try to undo a half-started
		// voice session rather than leaving the desktop app listening.
		b.setAgent(d.Agent)
		mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
			s.InCall = true
			s.Caller = number
			s.CallerLabel = name
			s.Agent = d.Agent
			s.Blocked = ""
			s.LastError = err.Error()
			s.LastEvent = "agent-voice-error"
		})
		return false
	}
	b.setAgent(d.Agent)
	silent := !audioBridgeReady(cfg)
	mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
		s.InCall = true
		s.Caller = number
		s.CallerLabel = name
		s.Agent = d.Agent
		s.Blocked = ""
		s.LastEvent = "call-bridged"
		if silent {
			// The call is up and the agent is listening, but nothing carries the
			// sound between them yet.
			s.LastError = "The call was answered but no audio path is set up: choose the Google Voice microphone and speaker under Connections, and install a virtual audio cable if you have not."
		} else {
			s.LastError = ""
		}
	})
	return true
}

func (b *voiceBridge) Ended() {
	agent := b.takeAgent()
	if agent == "A" || agent == "C" {
		_ = b.deactivate(loadVoiceCallConfig(b.dataDir), agent)
	}
	mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
		s.InCall = false
		s.Caller = ""
		s.CallerLabel = ""
		s.Agent = ""
		s.LastEvent = "call-ended"
	})
}

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
		// put an unselectable placeholder in the settings dropdowns and make the
		// real problem invisible, so unnamed endpoints are counted, not renamed.
		if strings.TrimSpace(d.Label) == "" {
			hidden = true
			continue
		}
		named = append(named, d)
	}
	mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
		s.Devices = named
		s.DeviceLabelsHidden = hidden && len(named) == 0
		s.LastEvent = "audio-devices"
	})
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

func (b *voiceBridge) setAgent(agent string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.agent = agent
}

func (b *voiceBridge) takeAgent() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	agent := b.agent
	b.agent = ""
	if agent == "" {
		// A call that FlipAi bridged in an earlier run of this process still has
		// its agent on disk; the desktop app should not be left in voice mode.
		agent = loadVoiceRuntime(b.dataDir).Agent
	}
	return agent
}
