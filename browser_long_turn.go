package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	browserLongTurnPending = "pending"
	browserLongTurnDone    = "done"
	browserLongTurnFailed  = "failed"
)

type browserLongTurnState struct {
	Provider string    `json:"provider"`
	Status   string    `json:"status"`
	Reply    string    `json:"reply,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	Progress string    `json:"progress,omitempty"`
	Updated  time.Time `json:"updated"`
}

func browserLongTurnProvider(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "G", "CHATGPT", "CHATGPT CHAT":
		return "chatgpt"
	case "H", "CLAUDE", "CLAUDE CHAT":
		return "claude-chat"
	case "M", "GEMINI", "GEMINI CHAT":
		return "gemini"
	case "X", "GROK", "GROK CHAT":
		return "grok"
	case "P", "COPILOT", "MICROSOFT COPILOT", "MICROSOFT COPILOT CHAT":
		return "copilot"
	default:
		return ""
	}
}

func browserLongTurnPath(dataDir, provider string) string {
	provider = browserLongTurnProvider(provider)
	if provider == "" || strings.TrimSpace(dataDir) == "" {
		return ""
	}
	return filepath.Join(dataDir, "browser-long-turn-"+provider+".json")
}

func clearBrowserLongTurnState(dataDir, provider string) {
	if path := browserLongTurnPath(dataDir, provider); path != "" {
		_ = os.Remove(path)
	}
}

func saveBrowserLongTurnState(dataDir string, state browserLongTurnState) error {
	provider := browserLongTurnProvider(state.Provider)
	path := browserLongTurnPath(dataDir, provider)
	if path == "" {
		return errors.New("unknown browser long-turn provider")
	}
	state.Provider = provider
	state.Status = strings.ToLower(strings.TrimSpace(state.Status))
	state.Reply = strings.TrimSpace(state.Reply)
	state.Detail = strings.TrimSpace(state.Detail)
	state.Progress = sanitizeBrowserProgress(state.Progress)
	state.Updated = time.Now()
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadBrowserLongTurnState(dataDir, provider string) (browserLongTurnState, error) {
	path := browserLongTurnPath(dataDir, provider)
	if path == "" {
		return browserLongTurnState{}, errors.New("unknown browser long-turn provider")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return browserLongTurnState{}, err
	}
	var state browserLongTurnState
	if err := json.Unmarshal(b, &state); err != nil {
		return browserLongTurnState{}, err
	}
	return state, nil
}

func browserLongTurnTimeoutDetail(detail string) bool {
	s := strings.ToLower(strings.TrimSpace(detail))
	return strings.Contains(s, "within 90 seconds") ||
		strings.Contains(s, "did not finish within 90 seconds") ||
		strings.Contains(s, "did not produce") && strings.Contains(s, "90 seconds")
}

// sanitizeBrowserProgress only forwards concise, visible status-like text. It
// deliberately refuses long prose so a provider's detailed reasoning is never
// relayed as a progress SMS. When no safe visible status is available, the
// normal generic "still working" heartbeat remains the fallback.
func sanitizeBrowserProgress(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	candidates := strings.Split(raw, "\n")
	if len(candidates) == 1 {
		candidates = []string{raw}
	}
	keywords := []string{
		"thinking", "working", "searching", "researching", "browsing", "reading",
		"analyzing", "analysing", "checking", "generating", "creating", "rendering",
		"preparing", "writing", "coding", "running", "uploading", "downloading",
		"finishing", "finalizing", "finalising", "almost done", "processing",
	}
	for i := len(candidates) - 1; i >= 0; i-- {
		line := strings.Join(strings.Fields(candidates[i]), " ")
		if line == "" || len([]rune(line)) > 160 {
			continue
		}
		lower := strings.ToLower(line)
		for _, keyword := range keywords {
			if strings.Contains(lower, keyword) {
				return line
			}
		}
	}
	return ""
}

// waitForBrowserLongTurn has intentionally no elapsed-time deadline. The
// caller's context is cancelled only by a real app/session shutdown. A model
// that works for twenty minutes therefore stays alive; only an explicit failed
// state ends the wait as a failure.
func waitForBrowserLongTurn(ctx context.Context, dataDir, provider string, onProgress func(string)) (string, error) {
	provider = browserLongTurnProvider(provider)
	if provider == "" {
		return "", errors.New("unknown browser long-turn provider")
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	lastProgress := ""
	for {
		if state, err := loadBrowserLongTurnState(dataDir, provider); err == nil {
			if progress := sanitizeBrowserProgress(state.Progress); progress != "" && progress != lastProgress {
				lastProgress = progress
				if onProgress != nil {
					onProgress(progress)
				}
			}
			switch strings.ToLower(strings.TrimSpace(state.Status)) {
			case browserLongTurnDone:
				return strings.TrimSpace(state.Reply), nil
			case browserLongTurnFailed:
				detail := strings.TrimSpace(state.Detail)
				if detail == "" {
					detail = "the browser model reported that the task failed"
				}
				return strings.TrimSpace(state.Reply), errors.New(detail)
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}
