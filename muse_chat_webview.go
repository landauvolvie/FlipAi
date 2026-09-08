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
	museChatWebURL         = "https://muse.ai/"
	museChatRuntimeFile    = "muse-chat-webview-runtime.json"
	museChatProfileDirName = "muse-chat-webview-profile"
	museChatAgentName      = "Muse"
)

type MuseChatWebRuntime struct {
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

var museChatRuntimeMu sync.Mutex

func museChatRuntimePath(dataDir string) string { return filepath.Join(dataDir, museChatRuntimeFile) }
func museChatProfilePath(dataDir string) string { return filepath.Join(dataDir, museChatProfileDirName) }

func migrateMuseChatRuntime(s *MuseChatWebRuntime) {
	if s.SignedIn && !s.Connected {
		s.Connected = true
	}
}

func loadMuseChatRuntime(dataDir string) MuseChatWebRuntime {
	museChatRuntimeMu.Lock()
	defer museChatRuntimeMu.Unlock()
	var s MuseChatWebRuntime
	if b, err := os.ReadFile(museChatRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateMuseChatRuntime(&s)
	return s
}

func mutateMuseChatRuntime(dataDir string, fn func(*MuseChatWebRuntime)) {
	museChatRuntimeMu.Lock()
	defer museChatRuntimeMu.Unlock()
	var s MuseChatWebRuntime
	if b, err := os.ReadFile(museChatRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateMuseChatRuntime(&s)
	fn(&s)
	s.UpdatedAt = time.Now()
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(museChatRuntimePath(dataDir), b, 0600)
	}
}

func museChatActivity(dataDir, level, stage, message string, took time.Duration) {
	log := activityLogForStatePath(filepath.Join(dataDir, "state.json"))
	log.AddTimed(level, stage, message, "", museChatAgentName, "", took)
}

func museChatJSString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func museChatConversationID(href string) string {
	for _, marker := range []string{"/chats/", "/chat/", "/conversation/", "conversationId="} {
		i := strings.Index(href, marker)
		if i < 0 {
			continue
		}
		v := href[i+len(marker):]
		if j := strings.IndexAny(v, "&#?/"); j >= 0 {
			v = v[:j]
		}
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func museChatControlRequest(ctx context.Context, s MuseChatWebRuntime, method, path string, body io.Reader) ([]byte, int, error) {
	if s.ControlPort < 1 || s.ControlToken == "" {
		return nil, 0, errors.New("Muse background session is not running")
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

func waitForMuseChatReady(ctx context.Context, dataDir string) (MuseChatWebRuntime, error) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		s := loadMuseChatRuntime(dataDir)
		if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			b, code, err := museChatControlRequest(probeCtx, s, http.MethodGet, "/health", nil)
			cancel()
			if err == nil && code == http.StatusOK {
				var health struct{ SignedIn bool `json:"signedIn"` }
				if json.Unmarshal(b, &health) == nil && health.SignedIn {
					mutateMuseChatRuntime(dataDir, func(v *MuseChatWebRuntime) {
						v.Connected, v.SignedIn, v.Starting = true, true, false
						v.LastError = ""
					})
					return loadMuseChatRuntime(dataDir), nil
				}
			}
		}
		select {
		case <-ctx.Done():
			s = loadMuseChatRuntime(dataDir)
			if s.LastError != "" {
				return s, errors.New(s.LastError)
			}
			if s.Connected {
				return s, errors.New("the saved Muse session did not become ready in time; reconnect only if Muse has expired the account session")
			}
			return s, errors.New("Muse is not connected inside FlipAi; press Connect and complete sign-in")
		case <-t.C:
		}
	}
}

func ensureMuseChatReady(ctx context.Context, dataDir string) (MuseChatWebRuntime, error) {
	if err := platformEnsureMuseChatWorker(dataDir); err != nil {
		return MuseChatWebRuntime{}, err
	}
	return waitForMuseChatReady(ctx, dataDir)
}

func waitForMuseChatStopped(dataDir string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s := loadMuseChatRuntime(dataDir)
		if !s.Running && !s.Starting {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func runMuseChatBackgroundSupervisor(ctx context.Context, dataDir string) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	var lastAttempt, notReadySince time.Time
	var announced bool
	for {
		s := loadMuseChatRuntime(dataDir)
		want := s.Connected && !s.LoginActive
		if !want {
			announced = false
			notReadySince = time.Time{}
		} else {
			if s.Running && !museChatBrowserStillOpen(dataDir) {
				mutateMuseChatRuntime(dataDir, func(v *MuseChatWebRuntime) {
					v.Running, v.Starting, v.SignedIn = false, false, false
					v.ControlPort, v.ControlToken = 0, ""
					v.LastEvent = "background-restart-pending"
				})
				s = loadMuseChatRuntime(dataDir)
			}
			if s.Starting && time.Since(s.UpdatedAt) > 20*time.Second {
				mutateMuseChatRuntime(dataDir, func(v *MuseChatWebRuntime) { v.Starting = false; v.LastEvent = "background-restart-pending" })
				s = loadMuseChatRuntime(dataDir)
			}
			if !s.Running && !s.Starting && time.Since(lastAttempt) >= 5*time.Second {
				lastAttempt = time.Now()
				if !announced {
					museChatActivity(dataDir, "info", "muse-chat-session", "Restoring the saved Muse session in the background.", 0)
					announced = true
				}
				if err := platformEnsureMuseChatWorker(dataDir); err != nil {
					museChatActivity(dataDir, "error", "muse-chat-session", "Could not restore Muse: "+err.Error(), 0)
				}
			} else if s.Running && s.SignedIn {
				announced = false
				notReadySince = time.Time{}
			} else if s.Running {
				if notReadySince.IsZero() {
					notReadySince = time.Now()
				} else if time.Since(notReadySince) > 75*time.Second && time.Since(lastAttempt) > 30*time.Second {
					lastAttempt = time.Now()
					_ = platformStopMuseChatWorker(dataDir)
					waitForMuseChatStopped(dataDir, 4*time.Second)
					_ = platformEnsureMuseChatWorker(dataDir)
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

func (a *App) museChatConnect(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if err := platformStartMuseChatLogin(a.dataDir); err != nil {
		museChatActivity(a.dataDir, "error", "muse-chat-connect", "Could not open Muse sign-in: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "Could not open Muse sign-in", err.Error())
		return
	}
	museChatActivity(a.dataDir, "info", "muse-chat-connect", "Opened the one-time Muse sign-in window.", time.Since(started))
	renderResult(w, r, 200, true, "Muse sign-in opened", "Sign in to Muse in the window FlipAi opened. Once the chat is ready, close that window; FlipAi keeps the dedicated session running in the background.")
}

func (a *App) museChatTest(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	s, err := ensureMuseChatReady(ctx, a.dataDir)
	cancel()
	if err != nil {
		renderResult(w, r, 500, false, "Muse is not ready", err.Error())
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 100*time.Second)
	b, code, err := museChatControlRequest(ctx, s, http.MethodPost, "/test", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		renderResult(w, r, 500, false, "Muse test failed", err.Error())
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
		renderResult(w, r, 500, false, "Muse test failed", out.Detail)
		return
	}
	museChatActivity(a.dataDir, "info", "muse-chat-test", "Muse completed a real browser turn successfully.", time.Since(started))
	message := "Muse returned a real response through FlipAi's dedicated browser session."
	if out.ConversationID != "" {
		message += "\nConversation: " + out.ConversationID
	}
	if strings.TrimSpace(out.Reply) != "" {
		message += "\nReply: " + strings.TrimSpace(out.Reply)
	}
	renderResult(w, r, 200, true, "Muse is working", message)
}

func (a *App) museChatDisconnect(w http.ResponseWriter, r *http.Request) {
	mutateMuseChatRuntime(a.dataDir, func(s *MuseChatWebRuntime) { s.Connected, s.LoginActive = false, false })
	_ = platformStopMuseChatWorker(a.dataDir)
	waitForMuseChatStopped(a.dataDir, 5*time.Second)
	if err := os.RemoveAll(museChatProfilePath(a.dataDir)); err != nil {
		renderResult(w, r, 500, false, "Could not disconnect Muse", err.Error())
		return
	}
	_ = os.Remove(museChatRuntimePath(a.dataDir))
	museChatActivity(a.dataDir, "info", "muse-chat-disconnect", "Disconnected Muse and removed its dedicated browser profile.", 0)
	renderResult(w, r, 200, true, "Muse disconnected", "FlipAi's private Muse profile was removed. Your normal Edge and browser profiles were not touched.")
}

func (a *App) museChatStatusJSON(w http.ResponseWriter, r *http.Request) {
	s := loadMuseChatRuntime(a.dataDir)
	s.ControlToken = ""
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s)
}
