package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

func authorizeGrokChatRaw(raw string, cfg Config) (string, error) {
	s := agentSettings(cfg, "X")
	if !s.RequireCode { return strings.TrimSpace(raw), nil }
	f := strings.Fields(raw)
	if len(f) < 2 { return "", errors.New("missing the Grok Chat security code or the command") }
	if !verifyAgentCode(s, f[0]) { return "", errors.New("invalid SMS security code for Grok Chat") }
	return strings.TrimSpace(strings.TrimPrefix(raw, f[0])), nil
}

func parseGrokChatSMSCommand(raw string, cfg Config) (remoteCommand, error) {
	rest, err := authorizeGrokChatRaw(raw, cfg); if err != nil { return remoteCommand{}, err }
	if strings.EqualFold(rest, "STATUS") { return remoteCommand{Status: true}, nil }
	newWord := configuredNewSessionCommand(cfg)
	if strings.EqualFold(rest, newWord) || isAgentNewSession(rest, configuredGrokChatPrefix(cfg), newWord) { return remoteCommand{Agent: "X", New: true}, nil }
	text := rest; if tail, ok := stripAgentCommandPrefix(rest, configuredGrokChatPrefix(cfg)); ok { text = tail }
	text = strings.TrimSpace(text); if text == "" { return remoteCommand{}, errors.New("empty Grok Chat command") }
	return remoteCommand{Agent: "X", Text: text}, nil
}

type grokChatSMSReply struct { OK bool `json:"ok"`; Reply string `json:"reply"`; Detail string `json:"detail"`; ConversationID string `json:"conversationId"` }

// browserReplyEchoesPrompt is a final transport-level safety check. Even if a
// provider changes its DOM in a way the page driver does not recognize, FlipAi
// must never send the user's own prompt back as though it were the assistant's
// answer.
func browserReplyEchoesPrompt(reply, prompt string) bool {
	canon := func(s string) string { return strings.Join(strings.Fields(strings.TrimSpace(s)), " ") }
	r, p := canon(reply), canon(prompt)
	return r != "" && p != "" && r == p
}

func grokChatBrowserSendWithProgress(ctx context.Context, dataDir, prompt string, onProgress func(string)) (string, error) {
	_ = onProgress
	if !loadGrokChatRuntime(dataDir).Connected { return "", errors.New("Grok Chat is disconnected in FlipAi. Open FlipAi > Agents, press Connect for Grok Chat, then try again") }
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second); s, err := ensureGrokChatReady(readyCtx, dataDir); cancel()
	if err != nil { return "", fmt.Errorf("Grok Chat is not connected and ready in FlipAi. Open FlipAi > Agents and reconnect Grok Chat, then try again: %w", err) }

	// Bind this turn to the exact WebView worker that accepted it. Previously a
	// disconnect/reconnect could destroy that worker while the old HTTP request
	// remained blocked forever. execute() therefore never returned and its SMS
	// heartbeat kept texting "Grok Chat still working…" indefinitely. A changed
	// control port/token or Connected=false now cancels the stale turn at once.
	turnCtx, turnCancel := context.WithCancel(ctx)
	defer turnCancel()
	sessionChanged := make(chan struct{})
	go func(port int, token string) {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-turnCtx.Done():
				return
			case <-t.C:
				now := loadGrokChatRuntime(dataDir)
				if !now.Connected || now.ControlPort != port || now.ControlToken != token {
					close(sessionChanged)
					turnCancel()
					return
				}
			}
		}
	}(s.ControlPort, s.ControlToken)
	changedError := func(err error) error {
		select {
		case <-sessionChanged:
			return errors.New("Grok Chat was disconnected or restarted while this message was running. Retry the message; the old turn has been stopped")
		default:
			return err
		}
	}

	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": false})
	requestCtx, requestCancel := context.WithTimeout(turnCtx, 100*time.Second)
	body, code, err := grokChatControlRequest(requestCtx, s, http.MethodPost, "/chat", strings.NewReader(string(payload)))
	requestCancel()
	if err != nil { return "", changedError(err) }
	var out grokChatSMSReply; _ = json.Unmarshal(body, &out)
	if code != http.StatusOK || !out.OK {
		if strings.TrimSpace(out.Detail) == "" { out.Detail = strings.TrimSpace(string(body)) }
		if browserLongTurnTimeoutDetail(out.Detail) {
			reply, waitErr := waitForBrowserLongTurn(turnCtx, dataDir, "X", nil)
			if waitErr != nil { return reply, changedError(waitErr) }
			return reply, nil
		}
		return "", errors.New(out.Detail)
	}
	if strings.TrimSpace(out.Reply) == "" { return "", errors.New("Grok Chat returned an empty reply") }
	if browserReplyEchoesPrompt(out.Reply, prompt) { return "", errors.New("Grok Chat did not produce a fresh assistant reply; FlipAi received the submitted prompt back instead. Reconnect Grok Chat and try again") }
	return strings.TrimSpace(out.Reply), nil
}

func grokChatBrowserSend(ctx context.Context, dataDir, prompt string) (string, error) { return grokChatBrowserSendWithProgress(ctx, dataDir, prompt, nil) }

func grokChatBrowserNewConversation(ctx context.Context, dataDir string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second); s, err := ensureGrokChatReady(readyCtx, dataDir); cancel(); if err != nil { return err }
	reqCtx, cancel := context.WithTimeout(ctx, 55*time.Second); body, code, err := grokChatControlRequest(reqCtx, s, http.MethodPost, "/new", strings.NewReader(`{}`)); cancel(); if err != nil { return err }
	if code != http.StatusOK { var out grokChatSMSReply; _ = json.Unmarshal(body, &out); if out.Detail != "" { return errors.New(out.Detail) }; return fmt.Errorf("Grok Chat new-chat request returned HTTP %d", code) }
	return nil
}

func (b *Bridge) composeGrokChatSMSPrompt(command string) string { command = strings.TrimSpace(command); hint := strings.TrimSpace(b.cfg.replyStyleHintFor("X")); if hint == "" { return command }; return command + "\n\n" + hint }
func (b *Bridge) runGrokChatSMS(ctx context.Context, command string) (string, error) { dataDir := filepath.Dir(b.statePath); reply, err := grokChatBrowserSendWithProgress(ctx, dataDir, b.composeGrokChatSMSPrompt(command), nil); return finishBrowserGeneratedImageTurn(ctx, command, reply, err) }
func (b *Bridge) newGrokChatConversation(ctx context.Context) error { return grokChatBrowserNewConversation(ctx, filepath.Dir(b.statePath)) }
