package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// SmokeTest runs one tiny ephemeral Codex turn. It verifies the exact app-server
// request/notification path used by SMS commands without creating a durable
// conversation or running tools. modelProvider is pinned to openai so a custom
// provider in the user's Codex config cannot silently turn this verification
// into external/API-provider billing.
func (c *CodexClient) SmokeTest(ctx context.Context) error {
	if c == nil || !c.Alive() {
		return errors.New("Codex app-server is not running")
	}
	raw, err := c.Request(ctx, "thread/start", map[string]any{"ephemeral": true, "modelProvider": "openai"})
	if err != nil {
		return fmt.Errorf("start ephemeral Codex test thread: %w", err)
	}
	var started struct {
		Thread struct {
			ID            string `json:"id"`
			ModelProvider string `json:"modelProvider"`
		} `json:"thread"`
		ModelProvider string `json:"modelProvider"`
	}
	if json.Unmarshal(raw, &started) != nil || started.Thread.ID == "" {
		return errors.New("Codex test thread returned no thread id")
	}
	provider := started.Thread.ModelProvider
	if provider == "" {
		provider = started.ModelProvider
	}
	if provider != "" && !strings.EqualFold(provider, "openai") {
		return fmt.Errorf("Codex test resolved to model provider %q instead of openai; FlipAi refuses external provider billing", provider)
	}
	raw, err = c.Request(ctx, "turn/start", map[string]any{
		"threadId":       started.Thread.ID,
		"approvalPolicy": "never",
		"input":          []map[string]any{{"type": "text", "text": "Reply with exactly FLIPAI_CODEX_OK and nothing else. Do not use tools."}},
	})
	if err != nil {
		return fmt.Errorf("start Codex test turn: %w", err)
	}
	var turnStart struct {
		Turn struct{ ID string `json:"id"` } `json:"turn"`
	}
	if json.Unmarshal(raw, &turnStart) != nil || turnStart.Turn.ID == "" {
		return errors.New("Codex test turn returned no turn id")
	}
	final := ""
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("Codex test timed out: %w", ctx.Err())
		case <-c.done:
			return errors.New("Codex app-server stopped during the test")
		case n := <-c.notifications:
			switch n.Method {
			case "item/completed":
				var p struct {
					TurnID string `json:"turnId"`
					Item   struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"item"`
				}
				if json.Unmarshal(n.Params, &p) == nil && (p.TurnID == "" || p.TurnID == turnStart.Turn.ID) && p.Item.Type == "agentMessage" {
					final = p.Item.Text
				}
			case "turn/completed":
				var p struct {
					Turn struct {
						ID     string `json:"id"`
						Status string `json:"status"`
						Error  *struct{ Message string `json:"message"` } `json:"error"`
					} `json:"turn"`
				}
				if json.Unmarshal(n.Params, &p) == nil && p.Turn.ID == turnStart.Turn.ID {
					if p.Turn.Error != nil && p.Turn.Error.Message != "" {
						return fmt.Errorf("Codex test failed: %s", p.Turn.Error.Message)
					}
					if !strings.EqualFold(p.Turn.Status, "completed") {
						return fmt.Errorf("Codex test ended with status %q", p.Turn.Status)
					}
					if !strings.Contains(strings.ToUpper(final), "FLIPAI_CODEX_OK") {
						return fmt.Errorf("Codex test did not return the expected verification response: %s", truncate(final, 300))
					}
					return nil
				}
			}
		}
	}
}
