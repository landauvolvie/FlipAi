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

	GoogleVoiceURL     string `json:"googleVoiceUrl"`
	GoogleVoiceInput   string `json:"googleVoiceInput"`   // virtual capture endpoint Google Voice uses as its mic
	GoogleVoiceOutput  string `json:"googleVoiceOutput"`  // virtual render endpoint Google Voice uses as its speaker
	AgentInput         string `json:"agentInput"`         // paired virtual capture endpoint selected in the AI app
	AgentOutput        string `json:"agentOutput"`        // paired virtual render endpoint selected in the AI app
	RingOutput         string `json:"ringOutput,omitempty"`

	Codex  VoiceAgentCallConfig `json:"codex"`
	Claude VoiceAgentCallConfig `json:"claude"`
}

type VoiceAgentCallConfig struct {
	Enabled        bool   `json:"enabled"`
	AllowedCallers string `json:"allowedCallers"`
	AppTitle       string `json:"appTitle"`
	AppCommand     string `json:"appCommand,omitempty"`
	VoiceShortcut  string `json:"voiceShortcut,omitempty"`
}

type VoiceAudioDevice struct {
	Kind     string `json:"kind"` // audioinput or audiooutput
	DeviceID string `json:"deviceId,omitempty"`
	Label    string `json:"label"`
}

type VoiceRuntimeState struct {
	BrowserRunning bool               `json:"browserRunning"`
	SignedIn       bool               `json:"signedIn"`
	Page           string             `json:"page,omitempty"`
	InCall         bool               `json:"inCall"`
	Caller         string             `json:"caller,omitempty"`
	Agent          string             `json:"agent,omitempty"`
	Devices        []VoiceAudioDevice `json:"devices,omitempty"`
	LastError      string             `json:"lastError,omitempty"`
	LastEvent      string             `json:"lastEvent,omitempty"`
	UpdatedAt      time.Time          `json:"updatedAt,omitempty"`
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
		} else if strict && agent.Enabled {
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
	if strict && cfg.Enabled && !cfg.Codex.Enabled && !cfg.Claude.Enabled {
		return cfg, errors.New("enable phone voice for at least one agent")
	}
	return cfg, nil
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

func voiceAgentForCaller(cfg VoiceCallConfig, caller string) (string, bool) {
	caller = normalizeUSPhone(caller)
	if caller == "" || !cfg.Enabled {
		return "", false
	}
	codexOK := cfg.Codex.Enabled && allowedPhone(cfg.Codex.AllowedCallers, caller)
	claudeOK := cfg.Claude.Enabled && allowedPhone(cfg.Claude.AllowedCallers, caller)
	if codexOK && claudeOK {
		if cfg.DefaultAgent == "A" {
			return "A", true
		}
		return "C", true
	}
	if codexOK {
		return "C", true
	}
	if claudeOK {
		return "A", true
	}
	return "", false
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
