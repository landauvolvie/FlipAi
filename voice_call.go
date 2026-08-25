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

type VoiceAgentCallConfig struct {
	Enabled        bool   `json:"enabled"`
	AllowedCallers string `json:"allowedCallers"`
	// AllowedLabels is the opt-in escape hatch for the common case where the
	// caller is in the user's Google Contacts: Google Voice then shows a name
	// where FlipAi expects digits, and a number-only allowlist can never match.
	// Entries are exact names the user copies from what Google Voice displayed.
	AllowedLabels string `json:"allowedLabels,omitempty"`
	AppTitle      string `json:"appTitle"`
	AppCommand    string `json:"appCommand,omitempty"`
	VoiceShortcut string `json:"voiceShortcut,omitempty"`
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
	DeviceLabelsHidden bool      `json:"deviceLabelsHidden,omitempty"`
	LastError          string    `json:"lastError,omitempty"`
	LastEvent          string    `json:"lastEvent,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt,omitempty"`
}

type voiceControlSnapshot struct {
	Config  VoiceCallConfig   `json:"config"`
	Runtime VoiceRuntimeState `json:"runtime"`
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
		agent.AllowedCallers = strings.TrimSpace(agent.AllowedCallers)
		if agent.AllowedCallers != "" {
			numbers, err := normalizeAllowedPhoneList(agent.AllowedCallers)
			if err != nil {
				return err
			}
			agent.AllowedCallers = strings.Join(numbers, "\n")
		}
		labels, err := normalizeAllowedCallerLabels(agent.AllowedLabels)
		if err != nil {
			return err
		}
		agent.AllowedLabels = strings.Join(labels, "\n")
		if strict && agent.Enabled && agent.AllowedCallers == "" && agent.AllowedLabels == "" {
			return errors.New("each voice-enabled agent needs at least one allowed caller")
		}
		return nil
	}
	if err := normalizeAgent(&cfg.Codex, "ChatGPT"); err != nil {
		return cfg, fmt.Errorf("ChatGPT/Codex callers: %w", err)
	}
	if err := normalizeAgent(&cfg.Claude, "Claude"); err != nil {
		return cfg, fmt.Errorf("Claude callers: %w", err)
	}
	if strict && cfg.Enabled {
		if !cfg.Codex.Enabled && !cfg.Claude.Enabled {
			return cfg, errors.New("enable phone voice for at least one agent")
		}
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
func validateVoiceAudioBridge(cfg VoiceCallConfig) error {
	if cfg.GoogleVoiceOutput == "" {
		return errors.New("choose the Google Voice speaker: the virtual cable that carries the caller toward the AI app")
	}
	if cfg.GoogleVoiceInput == "" {
		return errors.New("choose the Google Voice microphone: the virtual cable that carries the AI app's voice back to the caller")
	}
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

func normalizeAllowedCallerLabels(raw string) ([]string, error) {
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
			return nil, fmt.Errorf("%q is what Google Voice shows when there is no caller ID, so it cannot be an allowed caller name", name)
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

func voiceAgentAccepts(agent VoiceAgentCallConfig, number, label string) bool {
	if number != "" && allowedPhone(agent.AllowedCallers, number) {
		return true
	}
	return allowedCallerLabel(agent.AllowedLabels, label)
}

// voiceCallDecision is the whole authorization answer for one ring, including
// why a call was refused. The reason is shown in the desktop UI: a call that
// silently does nothing is the single most confusing failure this feature has.
type voiceCallDecision struct {
	Agent   string
	Allowed bool
	Reason  string
}

func decideVoiceCall(cfg VoiceCallConfig, caller, label string) voiceCallDecision {
	if !cfg.Enabled {
		return voiceCallDecision{Reason: "Phone voice is turned off in FlipAi settings."}
	}
	if !cfg.Codex.Enabled && !cfg.Claude.Enabled {
		return voiceCallDecision{Reason: "No agent is allowed on phone calls yet."}
	}
	number := normalizeUSPhone(caller)
	label = normalizeCallerLabel(label)
	codexOK := cfg.Codex.Enabled && voiceAgentAccepts(cfg.Codex, number, label)
	claudeOK := cfg.Claude.Enabled && voiceAgentAccepts(cfg.Claude, number, label)
	switch {
	case codexOK && claudeOK:
		if cfg.DefaultAgent == "A" {
			return voiceCallDecision{Agent: "A", Allowed: true}
		}
		return voiceCallDecision{Agent: "C", Allowed: true}
	case codexOK:
		return voiceCallDecision{Agent: "C", Allowed: true}
	case claudeOK:
		return voiceCallDecision{Agent: "A", Allowed: true}
	}
	switch {
	case number == "" && label == "":
		return voiceCallDecision{Reason: "Google Voice showed no caller ID for this call, so FlipAi could not match it to an agent."}
	case number == "":
		return voiceCallDecision{Reason: fmt.Sprintf("Google Voice showed %q instead of a phone number, which usually means the caller is in your Google Contacts. Add that exact name under Allowed caller names to let it through.", label)}
	default:
		return voiceCallDecision{Reason: fmt.Sprintf("%s is not on any agent's allowed-caller list.", formatUSPhone(number))}
	}
}

func voiceAgentForCaller(cfg VoiceCallConfig, caller string) (string, bool) {
	d := decideVoiceCall(cfg, caller, "")
	return d.Agent, d.Allowed
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
func startVoiceControlServer(dataDir, mainConfigPath string) {
	mainCfg, err := loadConfig(mainConfigPath, dataDir)
	if err != nil {
		mainCfg = defaultConfig(dataDir)
	}
	mux := http.NewServeMux()
	withCORS := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if !voiceOriginAllowed(origin, mainCfg.Listen) {
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
		writeJSON(w, voiceControlSnapshot{Config: loadVoiceCallConfig(dataDir), Runtime: loadVoiceRuntime(dataDir)})
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
		cfg = loadVoiceCallConfig(dataDir)
		platformVoiceConfigChanged(dataDir, cfg)
		writeJSON(w, voiceControlSnapshot{Config: cfg, Runtime: loadVoiceRuntime(dataDir)})
	}))
	mux.HandleFunc("/open", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		if err := platformOpenGoogleVoice(dataDir, true); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
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

	server := &http.Server{Addr: voiceControlListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
				s.LastError = "Voice settings service: " + err.Error()
				s.LastEvent = "control-error"
			})
		}
	}()
}

// voiceBridge holds every decision the injected Google Voice page asks FlipAi
// to make during a call. It deliberately contains no Windows types: the same
// code that runs behind the WebView2 bindings is what the tests and the browser
// harness exercise, so the ring/answer/bridge/hang-up path is verifiable
// without a phone line.
type voiceBridge struct {
	dataDir    string
	activate   func(cfg VoiceCallConfig, agent string) error
	deactivate func(cfg VoiceCallConfig, agent string) error

	mu    sync.Mutex
	agent string
}

func newVoiceBridge(dataDir string, activate, deactivate func(VoiceCallConfig, string) error) *voiceBridge {
	return &voiceBridge{dataDir: dataDir, activate: activate, deactivate: deactivate}
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
	d := decideVoiceCall(cfg, caller, label)
	mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
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
	d := decideVoiceCall(cfg, caller, label)
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
	mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
		s.InCall = true
		s.Caller = number
		s.CallerLabel = name
		s.Agent = d.Agent
		s.Blocked = ""
		s.LastError = ""
		s.LastEvent = "call-bridged"
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

func (b *voiceBridge) Page(href string, signedIn bool) {
	mutateVoiceRuntime(b.dataDir, func(s *VoiceRuntimeState) {
		s.BrowserRunning = true
		s.Page = href
		s.SignedIn = signedIn
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
