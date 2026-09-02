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

// ChatGPTWebRuntime is metadata only. The browser profile itself stays in its
// dedicated WebView2 data directory and is never copied into FlipAi state.
type ChatGPTWebRuntime struct {
	Running        bool      `json:"running"`
	Visible        bool      `json:"visible"`
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

func loadChatGPTRuntime(dataDir string) ChatGPTWebRuntime {
	chatGPTRuntimeMu.Lock()
	defer chatGPTRuntimeMu.Unlock()
	var s ChatGPTWebRuntime
	if b, err := os.ReadFile(chatGPTRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func mutateChatGPTRuntime(dataDir string, fn func(*ChatGPTWebRuntime)) {
	chatGPTRuntimeMu.Lock()
	defer chatGPTRuntimeMu.Unlock()
	var s ChatGPTWebRuntime
	if b, err := os.ReadFile(chatGPTRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
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

func waitForChatGPTStopped(dataDir string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !loadChatGPTRuntime(dataDir).Running {
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

func (a *App) chatGPTConnect(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if err := platformStartChatGPTLogin(a.dataDir); err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-connect", "Could not open the dedicated ChatGPT sign-in window: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "Could not open ChatGPT sign-in", err.Error())
		return
	}
	chatGPTActivity(a.dataDir, "info", "chatgpt-connect", "Opened the dedicated ChatGPT sign-in window. FlipAi is waiting for a signed-in ChatGPT page.", time.Since(started))
	renderResult(w, r, 200, true, "ChatGPT sign-in opened", "Sign in to ChatGPT in the window FlipAi opened. This uses FlipAi's own persistent browser profile, separate from your normal ChatGPT desktop app. You can leave that window open while testing, or close it after sign-in; FlipAi will reuse the same private profile in the background.")
}

func (a *App) chatGPTTest(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	chatGPTActivity(a.dataDir, "info", "chatgpt-test", "Starting an end-to-end ChatGPT test turn in the dedicated browser session.", 0)
	if err := platformEnsureChatGPTWorker(a.dataDir); err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-test", "Could not start the ChatGPT background browser: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "ChatGPT background session could not start", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	s, err := waitForChatGPTControl(ctx, a.dataDir)
	cancel()
	if err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-test", "ChatGPT background browser did not become ready: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "ChatGPT browser did not become ready", err.Error())
		return
	}
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
	if err := platformEnsureChatGPTWorker(a.dataDir); err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-turn", "Could not start the ChatGPT background browser: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "ChatGPT background session could not start", err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	s, err := waitForChatGPTControl(ctx, a.dataDir)
	cancel()
	if err != nil {
		chatGPTActivity(a.dataDir, "error", "chatgpt-turn", "ChatGPT background browser did not become ready: "+err.Error(), time.Since(started))
		renderResult(w, r, 500, false, "ChatGPT browser did not become ready", err.Error())
		return
	}
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
