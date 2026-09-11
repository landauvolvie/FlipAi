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

	// Browser page drivers already wait 90 seconds before handing a turn to the
	// long-turn tracker. Keep genuinely long work alive, but never let a dead
	// browser turn pin SMS delivery forever. This is intentionally browser-only;
	// CLI agents keep their own lifecycle semantics.
	browserLongTurnMaxWait = 5 * time.Minute
)

type browserLongTurnState struct {
	Provider string    `json:"provider"`
	Status   string    `json:"status"`
	Reply    string    `json:"reply,omitempty"`
	Detail   string    `json:"detail,omitempty"`
	// Progress is retained only so older state files still decode. FlipAi no
	// longer scrapes or forwards model thinking/intermediate UI text.
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
	case "U", "MUSE", "MUSE CHAT":
		return "muse"
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
	// Never persist model thought/progress text. Long turns are tracked only by
	// pending/done/failed state and their final reply/error.
	state.Progress = ""
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
	// Ignore progress left by an older FlipAi build.
	state.Progress = ""
	return state, nil
}

func browserLongTurnTimeoutDetail(detail string) bool {
	s := strings.ToLower(strings.TrimSpace(detail))
	return strings.Contains(s, "within 90 seconds") ||
		strings.Contains(s, "did not finish within 90 seconds") ||
		strings.Contains(s, "did not produce") && strings.Contains(s, "90 seconds")
}

// sanitizeBrowserProgress is intentionally retired. It remains as a compatibility
// helper for older callers/tests, but FlipAi must never scrape or forward a
// model's thought process or transient status text.
func sanitizeBrowserProgress(string) string { return "" }

func waitForBrowserLongTurn(ctx context.Context, dataDir, provider string, onProgress func(string)) (string, error) {
	_ = onProgress // deliberately unused: only final output is deliverable
	provider = browserLongTurnProvider(provider)
	if provider == "" {
		return "", errors.New("unknown browser long-turn provider")
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(browserLongTurnMaxWait)
	defer deadline.Stop()
	for {
		if state, err := loadBrowserLongTurnState(dataDir, provider); err == nil {
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
		case <-deadline.C:
			detail := "the browser model stopped making a verifiable response and did not finish after the extended wait; reconnect this model in FlipAi and try again"
			_ = saveBrowserLongTurnState(dataDir, browserLongTurnState{Provider: provider, Status: browserLongTurnFailed, Detail: detail})
			return "", errors.New(detail)
		case <-ticker.C:
		}
	}
}
