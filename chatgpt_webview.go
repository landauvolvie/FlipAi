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
	chatGPTWebURL         = "https://chatgpt.com/"
	chatGPTRuntimeFile    = "chatgpt-webview-runtime.json"
	chatGPTProfileDirName = "chatgpt-webview-profile"
	chatGPTAgentName      = "ChatGPT Chat"
)

// ChatGPTWebRuntime contains metadata only. Connected means the user completed
// sign-in at least once and FlipAi should keep restoring that dedicated profile
// invisibly. SignedIn means the currently running WebView has actually verified
// the session on its current page. Keeping those two states separate is what
// lets a saved session survive the short signed-out/loading phase after a
// browser or PC restart.
type ChatGPTWebRuntime struct {
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

var chatGPTRuntimeMu sync.Mutex

func chatGPTRuntimePath(dataDir string) string { return filepath.Join(dataDir, chatGPTRuntimeFile) }
func chatGPTProfilePath(dataDir string) string { return filepath.Join(dataDir, chatGPTProfileDirName) }

// migrateChatGPTRuntime preserves v0.46.12 connections on upgrade. That build
// only had SignedIn; if it was true, the dedicated profile was already proven
// and must become a durable Connected state before a new worker starts loading.
func migrateChatGPTRuntime(s *ChatGPTWebRuntime) {
	if s.SignedIn && !s.Connected {
		s.Connected = true
	}
}

func loadChatGPTRuntime(dataDir string) ChatGPTWebRuntime {
	chatGPTRuntimeMu.Lock()
	defer chatGPTRuntimeMu.Unlock()
	var s ChatGPTWebRuntime
	if b, err := os.ReadFile(chatGPTRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateChatGPTRuntime(&s)
	return s
}

func mutateChatGPTRuntime(dataDir string, fn func(*ChatGPTWebRuntime)) {
	chatGPTRuntimeMu.Lock()
	defer chatGPTRuntimeMu.Unlock()
	var s ChatGPTWebRuntime
	if b, err := os.ReadFile(chatGPTRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	migrateChatGPTRuntime(&s)
	fn(&s)
	s.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(chatGPTRuntimePath(dataDir), b, 0600)
}

func chatGPTActivity(dataDir, level, stage, message string, took time.Duration) {
	log := activityLogForStatePath(filepath.Join(dataDir, "state.json"))
	log.AddTimed(level, stage, message, "", chatGPTAgentName, "", took)
}

func chatGPTJSString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func chatGPTConversationID(href string) string {
	i := strings.Index(href, "/c/")
	if i < 0 {
		return ""
	}
	v := href[i+3:]
	if j := strings.IndexAny(v, "?#/"); j >= 0 {
		v = v[:j]
	}
	return strings.TrimSpace(v)
}

func waitForChatGPTControl(ctx context.Context, dataDir string) (ChatGPTWebRuntime, error) {
	t := time.NewTicker(150 * time.Millisecond)
	defer t.Stop()
	for {
		s := loadChatGPTRuntime(dataDir)
		if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
			return s, nil
		}
		select {
		case <-ctx.Done():
			if s.LastError != "" {
				return s, errors.New(s.LastError)
			}
			return s, ctx.Err()
		case <-t.C:
		}
	}
}

// waitForChatGPTReady fixes the important difference between "the WebView
// process exists" and "the saved ChatGPT login has finished restoring". A new
// WebView often exposes its private control endpoint hundreds of milliseconds
// before chatgpt.com has loaded its persisted auth session. Sending during that
// gap produced the v0.46.12 false "not signed in" error.
func waitForChatGPTReady(ctx context.Context, dataDir string) (ChatGPTWebRuntime, error) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		s := loadChatGPTRuntime(dataDir)
		if s.Running && s.ControlPort > 0 && s.ControlToken != "" {
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			b, code, err := chatGPTControlRequest(probeCtx, s, http.MethodGet, "/health", nil)
			cancel()
			if err == nil && code == http.StatusOK {
				var health struct {
					SignedIn bool `json:"signedIn"`
				}
				if json.Unmarshal(b, &health) == nil && health.SignedIn {
					mutateChatGPTRuntime(dataDir, func(v *ChatGPTWebRuntime) {
						v.Connected = true
						v.SignedIn = true
						v.Starting = false
						v.LastError = ""
					})
					return loadChatGPTRuntime(dataDir), nil
				}
			}
		}

		select {
		case <-ctx.Done():
			s = loadChatGPTRuntime(dataDir)
			if s.LastError != "" {
				return s, errors.New(s.LastError)
			}
			if s.Connected {
				return s, errors.New("the saved ChatGPT session did not become signed in in time; FlipAi kept the saved profile, so retry once, and use Connect ChatGPT only if ChatGPT has actually expired the account session")
			}
			return s, errors.New("ChatGPT is not signed in inside FlipAi; press Connect ChatGPT once and complete sign-in")
		case <-t.C:
		}
	}
}

func ensureChatGPTReady(ctx context.Context, dataDir string) (ChatGPTWebRuntime, error) {
	if err := platformEnsureChatGPTWorker(dataDir); err != nil {
		return ChatGPTWebRuntime{}, err
	}
	return waitForChatGPTReady(ctx, dataDir)
}

func waitForChatGPTStopped(dataDir string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s := loadChatGPTRuntime(dataDir)
		if !s.Running && !s.Starting {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func chatGPTControlRequest(ctx context.Context, s ChatGPTWebRuntime, method, path string, body io.Reader) ([]byte, int, error) {
	if s.ControlPort < 1 || s.ControlToken == "" {
		return nil, 0, errors.New("ChatGPT background session is not running")
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

// runChatGPTBackgroundSupervisor is owned by the tray process because that is
// the FlipAi process guaranteed to live in the signed-in Windows desktop
// session. Once Connected is true, it silently restores the WebView whenever it
// is missing: after the one-time sign-in window closes, after FlipAi restarts,
// and after Windows restarts. It never opens a login window on its own.
func runChatGPTBackgroundSupervisor(ctx context.Context, dataDir string) {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	var lastAttempt time.Time
	var notReadySince time.Time
	var announced bool
	for {
		s := loadChatGPTRuntime(dataDir)
		want := s.Connected && !s.LoginActive
		if !want {
			announced = false
			notReadySince = time.Time{}
		} else {
			// Running/Starting are process-local facts. After a hard reboot their
			// persisted values can be stale, so verify the private loopback worker.
			if s.Running && !chatGPTBrowserStillOpen(dataDir) {
				mutateChatGPTRuntime(dataDir, func(v *ChatGPTWebRuntime) {
					v.Running = false
					v.Starting = false
					v.SignedIn = false
					v.ControlPort = 0
					v.ControlToken = ""
					v.LastEvent = "background-restart-pending"
				})
				chatGPTActivity(dataDir, "warn", "chatgpt-session", "Saved ChatGPT browser state had no live worker; restarting it invisibly.", 0)
				s = loadChatGPTRuntime(dataDir)
			}
			if s.Starting && time.Since(s.UpdatedAt) > 20*time.Second {
				mutateChatGPTRuntime(dataDir, func(v *ChatGPTWebRuntime) { v.Starting = false; v.LastEvent = "background-restart-pending" })
				s = loadChatGPTRuntime(dataDir)
			}
			if !s.Running && !s.Starting && time.Since(lastAttempt) >= 5*time.Second {
				lastAttempt = time.Now()
				if !announced {
					chatGPTActivity(dataDir, "info", "chatgpt-session", "Restoring the saved ChatGPT session invisibly in the background.", 0)
					announced = true
				}
				if err := platformEnsureChatGPTWorker(dataDir); err != nil {
					chatGPTActivity(dataDir, "error", "chatgpt-session", "Could not restore the saved ChatGPT background session: "+err.Error(), 0)
				}
			} else if s.Running && s.SignedIn {
				announced = false
				notReadySince = time.Time{}
			} else if s.Running {
				if notReadySince.IsZero() {
					notReadySince = time.Now()
				} else if time.Since(notReadySince) > 75*time.Second && time.Since(lastAttempt) > 30*time.Second {
					lastAttempt = time.Now()
					chatGPTActivity(dataDir, "warn", "chatgpt-session", "ChatGPT browser stayed loaded without restoring the saved sign-in; recycling the hidden worker once and retrying.", 0)
					_ = platformStopChatGPTWorker(dataDir)
					waitForChatGPTStopped(dataDir, 4*time.Second)
					_ = platformEnsureChatGPTWorker(dataDir)
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

func (a *App) chatGPTConnect(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if err := platformStartChatGPTLogin(a.dataDir); err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-connect", "Could not open the dedicated ChatGPT sign-in window: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "Could not open ChatGPT sign-in", err.Error())
		return
	}
	chatGPTActivity(a.dataDir, "info", "chatgpt-connect", "Opened the one-time ChatGPT sign-in window. FlipAi is waiting for a signed-in ChatGPT page.", time.Since(started))
	renderResult(w, r, 200, true, "ChatGPT sign-in opened", "Sign in to ChatGPT in the window FlipAi opened. Once sign-in is verified, you can close that window. FlipAi saves this dedicated browser profile and automatically runs it invisibly after that, including after FlipAi or Windows restarts.")
}

func (a *App) chatGPTTest(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	chatGPTActivity(a.dataDir, "info", "chatgpt-test", "Starting an end-to-end ChatGPT test turn in the dedicated browser session.", 0)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	s, err := ensureChatGPTReady(ctx, a.dataDir)
	cancel()
	if err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-test", "ChatGPT background session did not become signed in and ready: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "ChatGPT background session is not ready", err.Error())
		return
	}
	chatGPTActivity(a.dataDir, "info", "chatgpt-session", "ChatGPT background session is signed in and ready for the test turn.", time.Since(started))
	ctx, cancel = context.WithTimeout(r.Context(), 100*time.Second)
	b, code, err := chatGPTControlRequest(ctx, s, http.MethodPost, "/test", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-test", "ChatGPT test transport failed: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "ChatGPT test failed", err.Error())
		return
	}
	var out struct {
		OK             bool   `json:"ok"`
		Reply          string `json:"reply"`
		ConversationID string `json:"conversationId"`
		Detail         string `json:"detail"`
	}
	_ = json.Unmarshal(b, &out)
	if code != http.StatusOK || !out.OK {
		if out.Detail == "" {
			out.Detail = strings.TrimSpace(string(b))
		}
		chatGPTActivity(a.dataDir, "error", "chatgpt-test", "ChatGPT rejected or could not complete the test turn: "+truncate(out.Detail, 300), time.Since(started))
		renderResult(w, r, 500, false, "ChatGPT test failed", out.Detail)
		return
	}
	chatGPTActivity(a.dataDir, "info", "chatgpt-test", "ChatGPT completed a real test turn successfully; conversation id was captured.", time.Since(started))
	message := "ChatGPT returned a real response through FlipAi's dedicated browser session."
	if out.ConversationID != "" {
		message += "\nConversation: " + out.ConversationID
	}
	if strings.TrimSpace(out.Reply) != "" {
		message += "\nReply: " + strings.TrimSpace(out.Reply)
	}
	renderResult(w, r, 200, true, "ChatGPT is working", message)
}

func (a *App) chatGPTChat(w http.ResponseWriter, r *http.Request) {
	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if prompt == "" {
		renderResult(w, r, 400, false, "Message is empty", "Type a message for ChatGPT first.")
		return
	}
	if len(prompt) > 12000 {
		prompt = prompt[:12000]
	}
	started := time.Now()
	chatGPTActivity(a.dataDir, "info", "chatgpt-turn", "Starting a ChatGPT browser-session turn.", 0)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	s, err := ensureChatGPTReady(ctx, a.dataDir)
	cancel()
	if err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-turn", "ChatGPT background session did not become signed in and ready: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "ChatGPT background session is not ready", err.Error())
		return
	}
	chatGPTActivity(a.dataDir, "info", "chatgpt-session", "ChatGPT background session is signed in and ready for a turn.", time.Since(started))
	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": r.FormValue("new") == "1"})
	ctx, cancel = context.WithTimeout(r.Context(), 100*time.Second)
	b, code, err := chatGPTControlRequest(ctx, s, http.MethodPost, "/chat", strings.NewReader(string(payload)))
	cancel()
	if err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-turn", "ChatGPT turn transport failed: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "ChatGPT message failed", err.Error())
		return
	}
	var out struct {
		OK             bool   `json:"ok"`
		Reply          string `json:"reply"`
		ConversationID string `json:"conversationId"`
		Detail         string `json:"detail"`
	}
	_ = json.Unmarshal(b, &out)
	if code != http.StatusOK || !out.OK {
		chatGPTActivity(a.dataDir, "error", "chatgpt-turn", "ChatGPT could not complete the turn: "+truncate(out.Detail, 300), time.Since(started))
		renderResult(w, r, 500, false, "ChatGPT message failed", out.Detail)
		return
	}
	chatGPTActivity(a.dataDir, "info", "chatgpt-turn", "ChatGPT completed a browser-session turn successfully.", time.Since(started))
	renderResult(w, r, 200, true, "ChatGPT replied", strings.TrimSpace(out.Reply))
}

func (a *App) chatGPTDisconnect(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	// Clear the durable desire first so the tray supervisor cannot restart the
	// worker while Disconnect is removing the dedicated profile.
	mutateChatGPTRuntime(a.dataDir, func(s *ChatGPTWebRuntime) {
		s.Connected = false
		s.LoginActive = false
	})
	_ = platformStopChatGPTWorker(a.dataDir)
	waitForChatGPTStopped(a.dataDir, 5*time.Second)
	if err := os.RemoveAll(chatGPTProfilePath(a.dataDir)); err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-disconnect", "Could not remove the dedicated ChatGPT browser profile: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "Could not disconnect ChatGPT", err.Error())
		return
	}
	_ = os.Remove(chatGPTRuntimePath(a.dataDir))
	chatGPTActivity(a.dataDir, "info", "chatgpt-disconnect", "Disconnected ChatGPT and removed FlipAi's dedicated ChatGPT browser profile.", time.Since(started))
	renderResult(w, r, 200, true, "ChatGPT disconnected", "FlipAi's dedicated ChatGPT sign-in profile was removed. Your normal ChatGPT desktop app and browser profiles were not touched.")
}

func (a *App) chatGPTStatusJSON(w http.ResponseWriter, r *http.Request) {
	s := loadChatGPTRuntime(a.dataDir)
	s.ControlToken = ""
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s)
}
