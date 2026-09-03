package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	grokChatWebURL         = "https://grok.com"
	grokChatRuntimeFile    = "grok-chat-webview-runtime.json"
	grokChatProfileDirName = "grok-chat-webview-profile"
	grokChatAgentName      = "Grok Chat"
)

type GrokChatWebRuntime struct {
	Running        bool      `json:"running"`
	Starting       bool      `json:"starting,omitempty"`
	Visible        bool      `json:"visible"`
	LoginActive    bool      `json:"loginActive,omitempty"`
	Connected      bool      `json:"connected,omitempty"`
	SignedIn       bool      `json:"signedIn"`
	ControlPort    int       `json:"controlPort,omitempty"`
	ControlToken   string    `json:"controlToken,omitempty"`
	LastURL        string    `json:"lastUrl,omitempty"`
	ConversationID string    `json:"conversationId,omitempty"`
	LastEvent      string    `json:"lastEvent,omitempty"`
	LastError      string    `json:"lastError,omitempty"`
	UpdatedAt      time.Time `json:"updatedAt,omitempty"`
}

var grokChatRuntimeMu sync.Mutex

func grokChatRuntimePath(dataDir string) string {
	return filepath.Join(dataDir, grokChatRuntimeFile)
}
func grokChatProfilePath(dataDir string) string {
	return filepath.Join(dataDir, grokChatProfileDirName)
}

func migrateGrokChatRuntime(s *GrokChatWebRuntime) {
	if s.SignedIn && !s.Connected {
		s.Connected = true
	}
}

func loadGrokChatRuntime(dataDir string) GrokChatWebRuntime {
	grokChatRuntimeMu.Lock()
	defer grokChatRuntimeMu.Unlock()
	var s GrokChatWebRuntime
	if b, err := os.ReadFile(grokChatRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateGrokChatRuntime(&s)
	return s
}

func mutateGrokChatRuntime(dataDir string, fn func(*GrokChatWebRuntime)) {
	grokChatRuntimeMu.Lock()
	defer grokChatRuntimeMu.Unlock()
	var s GrokChatWebRuntime
	if b, err := os.ReadFile(grokChatRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateGrokChatRuntime(&s)
	fn(&s)
	s.UpdatedAt = time.Now()
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(grokChatRuntimePath(dataDir), b, 0600)
	}
}

func grokChatActivity(dataDir, level, stage, message string, took time.Duration) {
	log := activityLogForStatePath(filepath.Join(dataDir, "state.json"))
	log.AddTimed(level, stage, message, "", grokChatAgentName, "", took)
}

func grokChatJSString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func grokChatConversationID(href string) string {
	for _, marker := range []string{"/app/", "/chat/"} {
		i := strings.Index(href, marker)
		if i < 0 {
			continue
		}
		v := href[i+len(marker):]
		if j := strings.IndexAny(v, "?#/"); j >= 0 {
			v = v[:j]
		}
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func grokChatControlRequest(ctx context.Context, s GrokChatWebRuntime, method, path string, body io.Reader) ([]byte, int, error) {
	if s.ControlPort < 1 || s.ControlToken == "" {
		return nil, 0, errors.New("Grok Chat background session is not running")
	}
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", s.ControlPort, path), body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-FlipAi-Token", s.ControlToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 100 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return b, resp.StatusCode, err
}

func waitForGrokChatReady(ctx context.Context, dataDir string) (GrokChatWebRuntime, error) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		s := loadGrokChatRuntime(dataDir)
		if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			b, code, err := grokChatControlRequest(probeCtx, s, http.MethodGet, "/health", nil)
			cancel()
			if err == nil && code == http.StatusOK {
				var health struct {
					SignedIn bool `json:"signedIn"`
				}
				if json.Unmarshal(b, &health) == nil && health.SignedIn {
					mutateGrokChatRuntime(dataDir, func(v *GrokChatWebRuntime) {
						v.Connected, v.SignedIn, v.Starting = true, true, false
						v.LastError = ""
					})
					return loadGrokChatRuntime(dataDir), nil
				}
			}
		}
		select {
		case <-ctx.Done():
			s = loadGrokChatRuntime(dataDir)
			if s.LastError != "" {
				return s, errors.New(s.LastError)
			}
			if s.Connected {
				return s, errors.New("the saved Grok Chat session did not become signed in in time; retry once, and reconnect only if Grok has expired the account session")
			}
			return s, errors.New("Grok Chat is not signed in inside FlipAi; press Connect and complete sign-in")
		case <-t.C:
		}
	}
}

func ensureGrokChatReady(ctx context.Context, dataDir string) (GrokChatWebRuntime, error) {
	if err := platformEnsureGrokChatWorker(dataDir); err != nil {
		return GrokChatWebRuntime{}, err
	}
	return waitForGrokChatReady(ctx, dataDir)
}

func waitForGrokChatStopped(dataDir string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s := loadGrokChatRuntime(dataDir)
		if !s.Running && !s.Starting {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The supervisor deliberately uses a cheap process-liveness probe, just like
// ChatGPT Chat. A slow renderer must never be mistaken for a dead browser and
// cause another WebView2 process tree to be spawned on top of it.
func runGrokChatBackgroundSupervisor(ctx context.Context, dataDir string) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	var lastAttempt, notReadySince time.Time
	var announced bool
	for {
		s := loadGrokChatRuntime(dataDir)
		want := s.Connected && !s.LoginActive
		if !want {
			announced = false
			notReadySince = time.Time{}
		} else {
			if s.Running && !grokChatBrowserStillOpen(dataDir) {
				mutateGrokChatRuntime(dataDir, func(v *GrokChatWebRuntime) {
					v.Running, v.Starting, v.SignedIn = false, false, false
					v.ControlPort, v.ControlToken = 0, ""
					v.LastEvent = "background-restart-pending"
				})
				s = loadGrokChatRuntime(dataDir)
			}
			if s.Starting && time.Since(s.UpdatedAt) > 20*time.Second {
				mutateGrokChatRuntime(dataDir, func(v *GrokChatWebRuntime) { v.Starting = false; v.LastEvent = "background-restart-pending" })
				s = loadGrokChatRuntime(dataDir)
			}
			if !s.Running && !s.Starting && time.Since(lastAttempt) >= 5*time.Second {
				lastAttempt = time.Now()
				if !announced {
					grokChatActivity(dataDir, "info", "grok-chat-session", "Restoring the saved Grok Chat session invisibly in the background.", 0)
					announced = true
				}
				if err := platformEnsureGrokChatWorker(dataDir); err != nil {
					grokChatActivity(dataDir, "error", "grok-chat-session", "Could not restore Grok Chat: "+err.Error(), 0)
				}
			} else if s.Running && s.SignedIn {
				announced = false
				notReadySince = time.Time{}
			} else if s.Running {
				if notReadySince.IsZero() {
					notReadySince = time.Now()
				} else if time.Since(notReadySince) > 75*time.Second && time.Since(lastAttempt) > 30*time.Second {
					lastAttempt = time.Now()
					_ = platformStopGrokChatWorker(dataDir)
					waitForGrokChatStopped(dataDir, 4*time.Second)
					_ = platformEnsureGrokChatWorker(dataDir)
					notReadySince = time.Now()
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) grokChatConnect(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if err := platformStartGrokChatLogin(a.dataDir); err != nil {
		grokChatActivity(a.dataDir, "error", "grok-chat-connect", "Could not open Grok Chat sign-in: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "Could not open Grok Chat sign-in", err.Error())
		return
	}
	grokChatActivity(a.dataDir, "info", "grok-chat-connect", "Opened the one-time Grok Chat sign-in window.", time.Since(started))
	renderResult(w, r, 200, true, "Grok Chat sign-in opened", "Sign in to Grok in the window FlipAi opened. Once verified, close that window; FlipAi keeps the dedicated session running invisibly.")
}

func (a *App) grokChatTest(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	s, err := ensureGrokChatReady(ctx, a.dataDir)
	cancel()
	if err != nil {
		renderResult(w, r, 500, false, "Grok Chat is not ready", err.Error())
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 100*time.Second)
	b, code, err := grokChatControlRequest(ctx, s, http.MethodPost, "/test", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		renderResult(w, r, 500, false, "Grok Chat test failed", err.Error())
		return
	}
	var out struct {
		OK                            bool `json:"ok"`
		Reply, Detail, ConversationID string
	}
	_ = json.Unmarshal(b, &out)
	if code != http.StatusOK || !out.OK {
		if out.Detail == "" {
			out.Detail = strings.TrimSpace(string(b))
		}
		renderResult(w, r, 500, false, "Grok Chat test failed", out.Detail)
		return
	}
	grokChatActivity(a.dataDir, "info", "grok-chat-test", "Grok Chat completed a real browser turn successfully.", time.Since(started))
	message := "Grok returned a real response through FlipAi's dedicated browser session."
	if out.ConversationID != "" {
		message += "\nConversation: " + out.ConversationID
	}
	if strings.TrimSpace(out.Reply) != "" {
		message += "\nReply: " + strings.TrimSpace(out.Reply)
	}
	renderResult(w, r, 200, true, "Grok Chat is working", message)
}

func (a *App) grokChatDisconnect(w http.ResponseWriter, r *http.Request) {
	mutateGrokChatRuntime(a.dataDir, func(s *GrokChatWebRuntime) { s.Connected, s.LoginActive = false, false })
	_ = platformStopGrokChatWorker(a.dataDir)
	waitForGrokChatStopped(a.dataDir, 5*time.Second)
	if err := os.RemoveAll(grokChatProfilePath(a.dataDir)); err != nil {
		renderResult(w, r, 500, false, "Could not disconnect Grok Chat", err.Error())
		return
	}
	_ = os.Remove(grokChatRuntimePath(a.dataDir))
	grokChatActivity(a.dataDir, "info", "grok-chat-disconnect", "Disconnected Grok Chat and removed its dedicated browser profile.", 0)
	renderResult(w, r, 200, true, "Grok Chat disconnected", "FlipAi's private Grok Chat profile was removed. Grok Desktop and your normal browser profiles were not touched.")
}

func (a *App) grokChatStatusJSON(w http.ResponseWriter, r *http.Request) {
	s := loadGrokChatRuntime(a.dataDir)
	s.ControlToken = ""
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s)
}
