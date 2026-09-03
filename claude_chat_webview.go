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
	claudeChatWebURL         = "https://claude.ai/new"
	claudeChatRuntimeFile    = "claude-chat-webview-runtime.json"
	claudeChatProfileDirName = "claude-chat-webview-profile"
	claudeChatAgentName      = "Claude Chat"
)

type ClaudeChatWebRuntime struct {
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

var claudeChatRuntimeMu sync.Mutex

func claudeChatRuntimePath(dataDir string) string {
	return filepath.Join(dataDir, claudeChatRuntimeFile)
}
func claudeChatProfilePath(dataDir string) string {
	return filepath.Join(dataDir, claudeChatProfileDirName)
}

func migrateClaudeChatRuntime(s *ClaudeChatWebRuntime) {
	if s.SignedIn && !s.Connected {
		s.Connected = true
	}
}

func loadClaudeChatRuntime(dataDir string) ClaudeChatWebRuntime {
	claudeChatRuntimeMu.Lock()
	defer claudeChatRuntimeMu.Unlock()
	var s ClaudeChatWebRuntime
	if b, err := os.ReadFile(claudeChatRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateClaudeChatRuntime(&s)
	return s
}

func mutateClaudeChatRuntime(dataDir string, fn func(*ClaudeChatWebRuntime)) {
	claudeChatRuntimeMu.Lock()
	defer claudeChatRuntimeMu.Unlock()
	var s ClaudeChatWebRuntime
	if b, err := os.ReadFile(claudeChatRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateClaudeChatRuntime(&s)
	fn(&s)
	s.UpdatedAt = time.Now()
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(claudeChatRuntimePath(dataDir), b, 0600)
	}
}

func claudeChatActivity(dataDir, level, stage, message string, took time.Duration) {
	log := activityLogForStatePath(filepath.Join(dataDir, "state.json"))
	log.AddTimed(level, stage, message, "", claudeChatAgentName, "", took)
}

func claudeChatJSString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func claudeChatConversationID(href string) string {
	i := strings.Index(href, "/chat/")
	if i < 0 {
		return ""
	}
	v := href[i+6:]
	if j := strings.IndexAny(v, "?#/"); j >= 0 {
		v = v[:j]
	}
	return strings.TrimSpace(v)
}

func claudeChatControlRequest(ctx context.Context, s ClaudeChatWebRuntime, method, path string, body io.Reader) ([]byte, int, error) {
	if s.ControlPort < 1 || s.ControlToken == "" {
		return nil, 0, errors.New("Claude Chat background session is not running")
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

func waitForClaudeChatReady(ctx context.Context, dataDir string) (ClaudeChatWebRuntime, error) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		s := loadClaudeChatRuntime(dataDir)
		if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			b, code, err := claudeChatControlRequest(probeCtx, s, http.MethodGet, "/health", nil)
			cancel()
			if err == nil && code == http.StatusOK {
				var health struct {
					SignedIn bool `json:"signedIn"`
				}
				if json.Unmarshal(b, &health) == nil && health.SignedIn {
					mutateClaudeChatRuntime(dataDir, func(v *ClaudeChatWebRuntime) {
						v.Connected, v.SignedIn, v.Starting = true, true, false
						v.LastError = ""
					})
					return loadClaudeChatRuntime(dataDir), nil
				}
			}
		}
		select {
		case <-ctx.Done():
			s = loadClaudeChatRuntime(dataDir)
			if s.LastError != "" {
				return s, errors.New(s.LastError)
			}
			if s.Connected {
				return s, errors.New("the saved Claude Chat session did not become signed in in time; retry once, and reconnect only if Claude has expired the account session")
			}
			return s, errors.New("Claude Chat is not signed in inside FlipAi; press Connect and complete sign-in")
		case <-t.C:
		}
	}
}

func ensureClaudeChatReady(ctx context.Context, dataDir string) (ClaudeChatWebRuntime, error) {
	if err := platformEnsureClaudeChatWorker(dataDir); err != nil {
		return ClaudeChatWebRuntime{}, err
	}
	return waitForClaudeChatReady(ctx, dataDir)
}

func waitForClaudeChatStopped(dataDir string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s := loadClaudeChatRuntime(dataDir)
		if !s.Running && !s.Starting {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The supervisor deliberately uses a cheap process-liveness probe, just like
// ChatGPT Chat. A slow renderer must never be mistaken for a dead browser and
// cause another WebView2 process tree to be spawned on top of it.
func runClaudeChatBackgroundSupervisor(ctx context.Context, dataDir string) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	var lastAttempt, notReadySince time.Time
	var announced bool
	for {
		s := loadClaudeChatRuntime(dataDir)
		want := s.Connected && !s.LoginActive
		if !want {
			announced = false
			notReadySince = time.Time{}
		} else {
			if s.Running && !claudeChatBrowserStillOpen(dataDir) {
				mutateClaudeChatRuntime(dataDir, func(v *ClaudeChatWebRuntime) {
					v.Running, v.Starting, v.SignedIn = false, false, false
					v.ControlPort, v.ControlToken = 0, ""
					v.LastEvent = "background-restart-pending"
				})
				s = loadClaudeChatRuntime(dataDir)
			}
			if s.Starting && time.Since(s.UpdatedAt) > 20*time.Second {
				mutateClaudeChatRuntime(dataDir, func(v *ClaudeChatWebRuntime) { v.Starting = false; v.LastEvent = "background-restart-pending" })
				s = loadClaudeChatRuntime(dataDir)
			}
			if !s.Running && !s.Starting && time.Since(lastAttempt) >= 5*time.Second {
				lastAttempt = time.Now()
				if !announced {
					claudeChatActivity(dataDir, "info", "claude-chat-session", "Restoring the saved Claude Chat session invisibly in the background.", 0)
					announced = true
				}
				if err := platformEnsureClaudeChatWorker(dataDir); err != nil {
					claudeChatActivity(dataDir, "error", "claude-chat-session", "Could not restore Claude Chat: "+err.Error(), 0)
				}
			} else if s.Running && s.SignedIn {
				announced = false
				notReadySince = time.Time{}
			} else if s.Running {
				if notReadySince.IsZero() {
					notReadySince = time.Now()
				} else if time.Since(notReadySince) > 75*time.Second && time.Since(lastAttempt) > 30*time.Second {
					lastAttempt = time.Now()
					_ = platformStopClaudeChatWorker(dataDir)
					waitForClaudeChatStopped(dataDir, 4*time.Second)
					_ = platformEnsureClaudeChatWorker(dataDir)
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

func (a *App) claudeChatConnect(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if err := platformStartClaudeChatLogin(a.dataDir); err != nil {
		claudeChatActivity(a.dataDir, "error", "claude-chat-connect", "Could not open Claude Chat sign-in: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "Could not open Claude Chat sign-in", err.Error())
		return
	}
	claudeChatActivity(a.dataDir, "info", "claude-chat-connect", "Opened the one-time Claude Chat sign-in window.", time.Since(started))
	renderResult(w, r, 200, true, "Claude Chat sign-in opened", "Sign in to Claude in the window FlipAi opened. Once verified, close that window; FlipAi keeps the dedicated session running invisibly.")
}

func (a *App) claudeChatTest(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	s, err := ensureClaudeChatReady(ctx, a.dataDir)
	cancel()
	if err != nil {
		renderResult(w, r, 500, false, "Claude Chat is not ready", err.Error())
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 100*time.Second)
	b, code, err := claudeChatControlRequest(ctx, s, http.MethodPost, "/test", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		renderResult(w, r, 500, false, "Claude Chat test failed", err.Error())
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
		renderResult(w, r, 500, false, "Claude Chat test failed", out.Detail)
		return
	}
	claudeChatActivity(a.dataDir, "info", "claude-chat-test", "Claude Chat completed a real browser turn successfully.", time.Since(started))
	message := "Claude returned a real response through FlipAi's dedicated browser session."
	if out.ConversationID != "" {
		message += "\nConversation: " + out.ConversationID
	}
	if strings.TrimSpace(out.Reply) != "" {
		message += "\nReply: " + strings.TrimSpace(out.Reply)
	}
	renderResult(w, r, 200, true, "Claude Chat is working", message)
}

func (a *App) claudeChatDisconnect(w http.ResponseWriter, r *http.Request) {
	mutateClaudeChatRuntime(a.dataDir, func(s *ClaudeChatWebRuntime) { s.Connected, s.LoginActive = false, false })
	_ = platformStopClaudeChatWorker(a.dataDir)
	waitForClaudeChatStopped(a.dataDir, 5*time.Second)
	if err := os.RemoveAll(claudeChatProfilePath(a.dataDir)); err != nil {
		renderResult(w, r, 500, false, "Could not disconnect Claude Chat", err.Error())
		return
	}
	_ = os.Remove(claudeChatRuntimePath(a.dataDir))
	claudeChatActivity(a.dataDir, "info", "claude-chat-disconnect", "Disconnected Claude Chat and removed its dedicated browser profile.", 0)
	renderResult(w, r, 200, true, "Claude Chat disconnected", "FlipAi's private Claude Chat profile was removed. Claude Desktop and your normal browser profiles were not touched.")
}

func (a *App) claudeChatStatusJSON(w http.ResponseWriter, r *http.Request) {
	s := loadClaudeChatRuntime(a.dataDir)
	s.ControlToken = ""
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s)
}
