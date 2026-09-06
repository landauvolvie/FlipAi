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
	copilotChatWebURL         = "https://copilot.microsoft.com/"
	copilotChatRuntimeFile    = "copilot-chat-webview-runtime.json"
	copilotChatProfileDirName = "copilot-chat-webview-profile"
	copilotChatAgentName      = "Microsoft Copilot Chat"
)

type CopilotChatWebRuntime struct {
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

var copilotChatRuntimeMu sync.Mutex

func copilotChatRuntimePath(dataDir string) string {
	return filepath.Join(dataDir, copilotChatRuntimeFile)
}
func copilotChatProfilePath(dataDir string) string {
	return filepath.Join(dataDir, copilotChatProfileDirName)
}

func migrateCopilotChatRuntime(s *CopilotChatWebRuntime) {
	if s.SignedIn && !s.Connected {
		s.Connected = true
	}
}

func loadCopilotChatRuntime(dataDir string) CopilotChatWebRuntime {
	copilotChatRuntimeMu.Lock()
	defer copilotChatRuntimeMu.Unlock()
	var s CopilotChatWebRuntime
	if b, err := os.ReadFile(copilotChatRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateCopilotChatRuntime(&s)
	return s
}

func mutateCopilotChatRuntime(dataDir string, fn func(*CopilotChatWebRuntime)) {
	copilotChatRuntimeMu.Lock()
	defer copilotChatRuntimeMu.Unlock()
	var s CopilotChatWebRuntime
	if b, err := os.ReadFile(copilotChatRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateCopilotChatRuntime(&s)
	fn(&s)
	s.UpdatedAt = time.Now()
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(copilotChatRuntimePath(dataDir), b, 0600)
	}
}

func copilotChatActivity(dataDir, level, stage, message string, took time.Duration) {
	log := activityLogForStatePath(filepath.Join(dataDir, "state.json"))
	log.AddTimed(level, stage, message, "", copilotChatAgentName, "", took)
}

func copilotChatJSString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func copilotChatConversationID(href string) string {
	for _, marker := range []string{"/chats/", "/chat/", "conversationId="} {
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

func copilotChatControlRequest(ctx context.Context, s CopilotChatWebRuntime, method, path string, body io.Reader) ([]byte, int, error) {
	if s.ControlPort < 1 || s.ControlToken == "" {
		return nil, 0, errors.New("Microsoft Copilot Chat background session is not running")
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

func waitForCopilotChatReady(ctx context.Context, dataDir string) (CopilotChatWebRuntime, error) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		s := loadCopilotChatRuntime(dataDir)
		if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			b, code, err := copilotChatControlRequest(probeCtx, s, http.MethodGet, "/health", nil)
			cancel()
			if err == nil && code == http.StatusOK {
				var health struct {
					SignedIn bool `json:"signedIn"`
				}
				if json.Unmarshal(b, &health) == nil && health.SignedIn {
					mutateCopilotChatRuntime(dataDir, func(v *CopilotChatWebRuntime) {
						v.Connected, v.SignedIn, v.Starting = true, true, false
						v.LastError = ""
					})
					return loadCopilotChatRuntime(dataDir), nil
				}
			}
		}
		select {
		case <-ctx.Done():
			s = loadCopilotChatRuntime(dataDir)
			if s.LastError != "" {
				return s, errors.New(s.LastError)
			}
			if s.Connected {
				return s, errors.New("the saved Microsoft Copilot Chat session did not become ready in time; reconnect only if Microsoft has expired the account session")
			}
			return s, errors.New("Microsoft Copilot Chat is not connected inside FlipAi; press Connect and complete sign-in")
		case <-t.C:
		}
	}
}

func ensureCopilotChatReady(ctx context.Context, dataDir string) (CopilotChatWebRuntime, error) {
	if err := platformEnsureCopilotChatWorker(dataDir); err != nil {
		return CopilotChatWebRuntime{}, err
	}
	return waitForCopilotChatReady(ctx, dataDir)
}

func waitForCopilotChatStopped(dataDir string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s := loadCopilotChatRuntime(dataDir)
		if !s.Running && !s.Starting {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Once a user connects Copilot in the one visible setup window, the saved
// WebView2 profile is restored by an off-screen worker. Renderer slowness is
// never treated as process death, preventing duplicate hidden browser trees.
func runCopilotChatBackgroundSupervisor(ctx context.Context, dataDir string) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	var lastAttempt, notReadySince time.Time
	var announced bool
	for {
		s := loadCopilotChatRuntime(dataDir)
		want := s.Connected && !s.LoginActive
		if !want {
			announced = false
			notReadySince = time.Time{}
		} else {
			if s.Running && !copilotChatBrowserStillOpen(dataDir) {
				mutateCopilotChatRuntime(dataDir, func(v *CopilotChatWebRuntime) {
					v.Running, v.Starting, v.SignedIn = false, false, false
					v.ControlPort, v.ControlToken = 0, ""
					v.LastEvent = "background-restart-pending"
				})
				s = loadCopilotChatRuntime(dataDir)
			}
			if s.Starting && time.Since(s.UpdatedAt) > 20*time.Second {
				mutateCopilotChatRuntime(dataDir, func(v *CopilotChatWebRuntime) { v.Starting = false; v.LastEvent = "background-restart-pending" })
				s = loadCopilotChatRuntime(dataDir)
			}
			if !s.Running && !s.Starting && time.Since(lastAttempt) >= 5*time.Second {
				lastAttempt = time.Now()
				if !announced {
					copilotChatActivity(dataDir, "info", "copilot-chat-session", "Restoring the saved Microsoft Copilot Chat session in the background.", 0)
					announced = true
				}
				if err := platformEnsureCopilotChatWorker(dataDir); err != nil {
					copilotChatActivity(dataDir, "error", "copilot-chat-session", "Could not restore Microsoft Copilot Chat: "+err.Error(), 0)
				}
			} else if s.Running && s.SignedIn {
				announced = false
				notReadySince = time.Time{}
			} else if s.Running {
				if notReadySince.IsZero() {
					notReadySince = time.Now()
				} else if time.Since(notReadySince) > 75*time.Second && time.Since(lastAttempt) > 30*time.Second {
					lastAttempt = time.Now()
					_ = platformStopCopilotChatWorker(dataDir)
					waitForCopilotChatStopped(dataDir, 4*time.Second)
					_ = platformEnsureCopilotChatWorker(dataDir)
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

func (a *App) copilotChatConnect(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if err := platformStartCopilotChatLogin(a.dataDir); err != nil {
		copilotChatActivity(a.dataDir, "error", "copilot-chat-connect", "Could not open Microsoft Copilot Chat sign-in: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "Could not open Microsoft Copilot Chat sign-in", err.Error())
		return
	}
	copilotChatActivity(a.dataDir, "info", "copilot-chat-connect", "Opened the one-time Microsoft Copilot Chat sign-in window.", time.Since(started))
	renderResult(w, r, 200, true, "Microsoft Copilot Chat sign-in opened", "Sign in to Microsoft Copilot in the window FlipAi opened. Once the chat is ready, close that window; FlipAi keeps the dedicated session running in the background.")
}

func (a *App) copilotChatTest(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	s, err := ensureCopilotChatReady(ctx, a.dataDir)
	cancel()
	if err != nil {
		renderResult(w, r, 500, false, "Microsoft Copilot Chat is not ready", err.Error())
		return
	}
	ctx, cancel = context.WithTimeout(r.Context(), 100*time.Second)
	b, code, err := copilotChatControlRequest(ctx, s, http.MethodPost, "/test", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		renderResult(w, r, 500, false, "Microsoft Copilot Chat test failed", err.Error())
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
		renderResult(w, r, 500, false, "Microsoft Copilot Chat test failed", out.Detail)
		return
	}
	copilotChatActivity(a.dataDir, "info", "copilot-chat-test", "Microsoft Copilot Chat completed a real browser turn successfully.", time.Since(started))
	message := "Copilot returned a real response through FlipAi's dedicated browser session."
	if out.ConversationID != "" {
		message += "\nConversation: " + out.ConversationID
	}
	if strings.TrimSpace(out.Reply) != "" {
		message += "\nReply: " + strings.TrimSpace(out.Reply)
	}
	renderResult(w, r, 200, true, "Microsoft Copilot Chat is working", message)
}

func (a *App) copilotChatDisconnect(w http.ResponseWriter, r *http.Request) {
	mutateCopilotChatRuntime(a.dataDir, func(s *CopilotChatWebRuntime) { s.Connected, s.LoginActive = false, false })
	_ = platformStopCopilotChatWorker(a.dataDir)
	waitForCopilotChatStopped(a.dataDir, 5*time.Second)
	if err := os.RemoveAll(copilotChatProfilePath(a.dataDir)); err != nil {
		renderResult(w, r, 500, false, "Could not disconnect Microsoft Copilot Chat", err.Error())
		return
	}
	_ = os.Remove(copilotChatRuntimePath(a.dataDir))
	copilotChatActivity(a.dataDir, "info", "copilot-chat-disconnect", "Disconnected Microsoft Copilot Chat and removed its dedicated browser profile.", 0)
	renderResult(w, r, 200, true, "Microsoft Copilot Chat disconnected", "FlipAi's private Copilot profile was removed. Your normal Edge and browser profiles were not touched.")
}

func (a *App) copilotChatStatusJSON(w http.ResponseWriter, r *http.Request) {
	s := loadCopilotChatRuntime(a.dataDir)
	s.ControlToken = ""
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s)
}
