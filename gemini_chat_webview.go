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
	geminiChatWebURL         = "https://gemini.google.com"
	geminiChatRuntimeFile    = "gemini-chat-webview-runtime.json"
	geminiChatProfileDirName = "gemini-chat-webview-profile"
	geminiChatAgentName      = "Gemini Chat"
)

type GeminiChatWebRuntime struct {
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

var geminiChatRuntimeMu sync.Mutex

func geminiChatRuntimePath(dataDir string) string {
	return filepath.Join(dataDir, geminiChatRuntimeFile)
}
func geminiChatProfilePath(dataDir string) string {
	return filepath.Join(dataDir, geminiChatProfileDirName)
}

func migrateGeminiChatRuntime(s *GeminiChatWebRuntime) {
	if s.SignedIn && !s.Connected {
		s.Connected = true
	}
}

func loadGeminiChatRuntime(dataDir string) GeminiChatWebRuntime {
	geminiChatRuntimeMu.Lock()
	defer geminiChatRuntimeMu.Unlock()
	var s GeminiChatWebRuntime
	if b, err := os.ReadFile(geminiChatRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateGeminiChatRuntime(&s)
	return s
}

func mutateGeminiChatRuntime(dataDir string, fn func(*GeminiChatWebRuntime)) {
	geminiChatRuntimeMu.Lock()
	defer geminiChatRuntimeMu.Unlock()
	var s GeminiChatWebRuntime
	if b, err := os.ReadFile(geminiChatRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateGeminiChatRuntime(&s)
	fn(&s)
	s.UpdatedAt = time.Now()
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(geminiChatRuntimePath(dataDir), b, 0600)
	}
}

func geminiChatActivity(dataDir, level, stage, message string, took time.Duration) {
	log := activityLogForStatePath(filepath.Join(dataDir, "state.json"))
	log.AddTimed(level, stage, message, "", geminiChatAgentName, "", took)
}

func geminiChatJSString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func geminiChatConversationID(href string) string {
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

func geminiChatControlRequest(ctx context.Context, s GeminiChatWebRuntime, method, path string, body io.Reader) ([]byte, int, error) {
	if s.ControlPort < 1 || s.ControlToken == "" {
		return nil, 0, errors.New("Gemini Chat background session is not running")
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

func waitForGeminiChatReady(ctx context.Context, dataDir string) (GeminiChatWebRuntime, error) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		s := loadGeminiChatRuntime(dataDir)
		if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			b, code, err := geminiChatControlRequest(probeCtx, s, http.MethodGet, "/health", nil)
			cancel()
			if err == nil && code == http.StatusOK {
				var health struct {
					SignedIn bool `json:"signedIn"`
				}
				if json.Unmarshal(b, &health) == nil && health.SignedIn {
					mutateGeminiChatRuntime(dataDir, func(v *GeminiChatWebRuntime) {
						v.Connected, v.SignedIn, v.Starting = true, true, false
						v.LastError = ""
					})
					return loadGeminiChatRuntime(dataDir), nil
				}
			}
		}
		select {
		case <-ctx.Done():
			s = loadGeminiChatRuntime(dataDir)
			if s.LastError != "" {
				return s, errors.New(s.LastError)
			}
			if s.Connected {
				return s, errors.New("the saved Gemini Chat session did not become signed in in time; retry once, and reconnect only if Gemini has expired the account session")
			}
			return s, errors.New("Gemini Chat is not signed in inside FlipAi; press Connect and complete sign-in")
		case <-t.C:
		}
	}
}

func ensureGeminiChatReady(ctx context.Context, dataDir string) (GeminiChatWebRuntime, error) {
	if err := platformEnsureGeminiChatWorker(dataDir); err != nil {
		return GeminiChatWebRuntime{}, err
	}
	return waitForGeminiChatReady(ctx, dataDir)
}

func waitForGeminiChatStopped(dataDir string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s := loadGeminiChatRuntime(dataDir)
		if !s.Running && !s.Starting {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The supervisor deliberately uses a cheap process-liveness probe, just like
// ChatGPT Chat. A slow renderer must never be mistaken for a dead browser and
// cause another WebView2 process tree to be spawned on top of it.
func runGeminiChatBackgroundSupervisor(ctx context.Context, dataDir string) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	var lastAttempt, notReadySince time.Time
	var announced bool
	for {
		s := loadGeminiChatRuntime(dataDir)
		want := s.Connected && !s.LoginActive
		if !want {
			announced = false
			notReadySince = time.Time{}
		} else {
			if s.Running && !geminiChatBrowserStillOpen(dataDir) {
				mutateGeminiChatRuntime(dataDir, func(v *GeminiChatWebRuntime) {
					v.Running, v.Starting, v.SignedIn = false, false, false
					v.ControlPort, v.ControlToken = 0, ""
					v.LastEvent = "background-restart-pending"
				})
				s = loadGeminiChatRuntime(dataDir)
			}
			if s.Starting && time.Since(s.UpdatedAt) > 20*time.Second {
				mutateGeminiChatRuntime(dataDir, func(v *GeminiChatWebRuntime) { v.Starting = false; v.LastEvent = "background-restart-pending" })
				s = loadGeminiChatRuntime(dataDir)
			}
			if !s.Running && !s.Starting && time.Since(lastAttempt) >= 5*time.Second {
				lastAttempt = time.Now()
				if !announced {
					geminiChatActivity(dataDir, "info", "gemini-chat-session", "Restoring the saved Gemini Chat session invisibly in the background.", 0)
					announced = true
				}
				if err := platformEnsureGeminiChatWorker(dataDir); err != nil {
					geminiChatActivity(dataDir, "error", "gemini-chat-session", "Could not restore Gemini Chat: "+err.Error(), 0)
				}
			} else if s.Running && s.SignedIn {
				announced = false
				notReadySince = time.Time{}
			} else if s.Running {
				if notReadySince.IsZero() {
					notReadySince = time.Now()
				} else if time.Since(notReadySince) > 75*time.Second && time.Since(lastAttempt) > 30*time.Second {
					lastAttempt = time.Now()
					_ = platformStopGeminiChatWorker(dataDir)
					waitForGeminiChatStopped(dataDir, 4*time.Second)
					_ = platformEnsureGeminiChatWorker(dataDir)
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

func (a *App) geminiChatConnect(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if err := platformStartGeminiChatLogin(a.dataDir); err != nil {
		geminiChatActivity(a.dataDir, "error", "gemini-chat-connect", "Could not open Gemini Chat sign-in: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "Could not open Gemini Chat sign-in", err.Error())
		return
	}
	geminiChatActivity(a.dataDir, "info", "gemini-chat-connect", "Opened the one-time Gemini Chat sign-in window.", time.Since(started))
	renderResult(w, r, 200, true, "Gemini Chat sign-in opened", "Sign in to Gemini in the window FlipAi opened. Once verified, close that window; FlipAi keeps the dedicated session running invisibly.")
}

func (a *App) geminiChatTest(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	s, err := ensureGeminiChatReady(ctx, a.dataDir)
	cancel()
	if err != nil {
		renderResult(w, r, 500, false, "Gemini Chat is not ready", err.Error())
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 100*time.Second)
	b, code, err := geminiChatControlRequest(ctx, s, http.MethodPost, "/test", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		renderResult(w, r, 500, false, "Gemini Chat test failed", err.Error())
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
		renderResult(w, r, 500, false, "Gemini Chat test failed", out.Detail)
		return
	}
	geminiChatActivity(a.dataDir, "info", "gemini-chat-test", "Gemini Chat completed a real browser turn successfully.", time.Since(started))
	message := "Gemini returned a real response through FlipAi's dedicated browser session."
	if out.ConversationID != "" {
		message += "\nConversation: " + out.ConversationID
	}
	if strings.TrimSpace(out.Reply) != "" {
		message += "\nReply: " + strings.TrimSpace(out.Reply)
	}
	renderResult(w, r, 200, true, "Gemini Chat is working", message)
}

func (a *App) geminiChatDisconnect(w http.ResponseWriter, r *http.Request) {
	mutateGeminiChatRuntime(a.dataDir, func(s *GeminiChatWebRuntime) { s.Connected, s.LoginActive = false, false })
	_ = platformStopGeminiChatWorker(a.dataDir)
	waitForGeminiChatStopped(a.dataDir, 5*time.Second)
	if err := os.RemoveAll(geminiChatProfilePath(a.dataDir)); err != nil {
		renderResult(w, r, 500, false, "Could not disconnect Gemini Chat", err.Error())
		return
	}
	_ = os.Remove(geminiChatRuntimePath(a.dataDir))
	geminiChatActivity(a.dataDir, "info", "gemini-chat-disconnect", "Disconnected Gemini Chat and removed its dedicated browser profile.", 0)
	renderResult(w, r, 200, true, "Gemini Chat disconnected", "FlipAi's private Gemini Chat profile was removed. Gemini Desktop and your normal browser profiles were not touched.")
}

func (a *App) geminiChatStatusJSON(w http.ResponseWriter, r *http.Request) {
	s := loadGeminiChatRuntime(a.dataDir)
	s.ControlToken = ""
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s)
}
