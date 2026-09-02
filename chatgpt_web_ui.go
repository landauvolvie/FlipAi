package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type chatGPTWebUIResult struct {
	OK             bool             `json:"ok"`
	Message        string           `json:"message,omitempty"`
	Status         chatGPTWebStatus `json:"status"`
	Capture        string           `json:"capture,omitempty"`
	DurationMS     int64            `json:"durationMs,omitempty"`
	ConversationID string           `json:"conversationId,omitempty"`
}

func (a *App) chatGPTWebHandler(w http.ResponseWriter, r *http.Request) {
	action := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("action")))
	client := newChatGPTWebClient(a.dataDir, a.statePath)
	if action == "" || action == "status" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			writeJSON(w, map[string]any{"error": "GET required for ChatGPT status"})
			return
		}
		writeJSON(w, chatGPTWebUIResult{OK: true, Status: client.Status(r.Context())})
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]any{"error": "POST required for this ChatGPT action"})
		return
	}
	ctx := r.Context()
	switch action {
	case "connect":
		connectCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		s, err := client.Connect(connectCtx)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{"error": err.Error(), "status": client.Status(ctx)})
			return
		}
		msg := "ChatGPT sign-in window opened. Finish sign-in there; FlipAi will park it automatically when the composer is ready."
		if s.SignedIn {
			msg = "ChatGPT is already signed in and ready."
		}
		writeJSON(w, chatGPTWebUIResult{OK: true, Message: msg, Status: s})
	case "show":
		connectCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		s, err := client.Connect(connectCtx)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, chatGPTWebUIResult{OK: true, Message: "ChatGPT session window shown.", Status: s})
	case "test":
		testCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
		defer cancel()
		result, err := client.Test(testCtx)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			writeJSON(w, map[string]any{"error": err.Error(), "status": client.Status(ctx)})
			return
		}
		writeJSON(w, chatGPTWebUIResult{OK: true, Message: "A real ChatGPT prompt was sent and an assistant reply came back to FlipAi.", Status: client.Status(ctx), Capture: result.Capture, DurationMS: result.DurationMS, ConversationID: result.ConversationID})
	case "new":
		path := chatGPTWebConversationPath(a.dataDir)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{"error": err.Error()})
			return
		}
		activityLogForStatePath(a.statePath).Add("info", "chatgpt", "ChatGPT SMS conversation reset; the next G: text starts a new saved ChatGPT chat", "", "G", "")
		writeJSON(w, chatGPTWebUIResult{OK: true, Message: "The next G: text will start a new ChatGPT conversation.", Status: client.Status(ctx)})
	case "disconnect":
		disconnectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := client.Disconnect(disconnectCtx); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]any{"error": err.Error(), "status": client.Status(ctx)})
			return
		}
		writeJSON(w, chatGPTWebUIResult{OK: true, Message: "FlipAi's private ChatGPT browser profile was removed.", Status: client.Status(ctx)})
	default:
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]any{"error": "unknown ChatGPT action"})
	}
}

func chatGPTWebProfileExists(dataDir string) bool {
	st, err := os.Stat(filepath.Join(dataDir, chatGPTWebProfileDir))
	return err == nil && st.IsDir()
}
