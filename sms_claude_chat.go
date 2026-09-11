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

func authorizeClaudeChatRaw(raw string, cfg Config) (string, error) {
	s := agentSettings(cfg, "H")
	if !s.RequireCode { return strings.TrimSpace(raw), nil }
	f := strings.Fields(raw)
	if len(f) < 2 { return "", errors.New("missing the Claude security code or the command") }
	if !verifyAgentCode(s, f[0]) { return "", errors.New("invalid SMS security code for Claude") }
	return strings.TrimSpace(strings.TrimPrefix(raw, f[0])), nil
}

func parseClaudeChatSMSCommand(raw string, cfg Config) (remoteCommand, error) {
	rest, err := authorizeClaudeChatRaw(raw, cfg); if err != nil { return remoteCommand{}, err }
	if strings.EqualFold(rest, "STATUS") { return remoteCommand{Status: true}, nil }
	newWord := configuredNewSessionCommand(cfg)
	if strings.EqualFold(rest, newWord) || isAgentNewSession(rest, configuredClaudeChatPrefix(cfg), newWord) { return remoteCommand{Agent: "H", New: true}, nil }
	text := rest; if tail, ok := stripAgentCommandPrefix(rest, configuredClaudeChatPrefix(cfg)); ok { text = tail }
	text = strings.TrimSpace(text); if text == "" { return remoteCommand{}, errors.New("empty Claude command") }
	return remoteCommand{Agent: "H", Text: text}, nil
}

type claudeChatSMSReply struct { OK bool `json:"ok"`; Reply string `json:"reply"`; Detail string `json:"detail"`; ConversationID string `json:"conversationId"` }

func claudeChatBrowserSendModeWithProgress(ctx context.Context, dataDir, prompt, mode string, onProgress func(string)) (string, error) {
	_ = onProgress
	if !loadClaudeChatRuntime(dataDir).Connected { return "", errors.New("Claude Chat is disconnected in FlipAi. Open FlipAi > Agents, press Connect for Claude Chat, then try again") }
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second); s, err := ensureClaudeChatReady(readyCtx, dataDir); cancel()
	if err != nil { return "", fmt.Errorf("Claude Chat is not connected and ready in FlipAi. Open FlipAi > Agents and reconnect Claude Chat, then try again: %w", err) }
	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": false, "mode": mode})
	turnCtx, cancel := context.WithTimeout(ctx, 100*time.Second); body, code, err := claudeChatControlRequest(turnCtx, s, http.MethodPost, "/chat", strings.NewReader(string(payload))); cancel()
	if err != nil { return "", err }
	var out claudeChatSMSReply; _ = json.Unmarshal(body, &out)
	if code != http.StatusOK || !out.OK {
		if strings.TrimSpace(out.Detail) == "" { out.Detail = strings.TrimSpace(string(body)) }
		if browserLongTurnTimeoutDetail(out.Detail) { return waitForBrowserLongTurn(ctx, dataDir, "H", nil) }
		return "", errors.New(out.Detail)
	}
	if strings.TrimSpace(out.Reply) == "" { return "", errors.New("Claude returned an empty reply") }
	return strings.TrimSpace(out.Reply), nil
}

func claudeChatBrowserSendMode(ctx context.Context, dataDir, prompt, mode string) (string, error) { return claudeChatBrowserSendModeWithProgress(ctx, dataDir, prompt, mode, nil) }
func claudeChatBrowserSend(ctx context.Context, dataDir, prompt string) (string, error) { return claudeChatBrowserSendMode(ctx, dataDir, prompt, browserModeChat) }
func claudeChatBrowserNewConversation(ctx context.Context, dataDir string) error { return claudeChatBrowserNewConversationMode(ctx, dataDir, browserModeChat) }

func claudeChatBrowserNewConversationMode(ctx context.Context, dataDir, mode string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second); s, err := ensureClaudeChatReady(readyCtx, dataDir); cancel(); if err != nil { return err }
	if mode == "" { mode = browserModeChat }
	payload, _ := json.Marshal(map[string]any{"mode": mode})
	reqCtx, cancel := context.WithTimeout(ctx, 55*time.Second); body, code, err := claudeChatControlRequest(reqCtx, s, http.MethodPost, "/new", strings.NewReader(string(payload))); cancel(); if err != nil { return err }
	if code != http.StatusOK { var out claudeChatSMSReply; _ = json.Unmarshal(body, &out); if out.Detail != "" { return errors.New(out.Detail) }; return fmt.Errorf("Claude new-chat request returned HTTP %d", code) }
	return nil
}

func (b *Bridge) composeClaudeChatSMSPrompt(command string) string {
	command = strings.TrimSpace(command); hint := strings.TrimSpace(b.cfg.replyStyleHintFor("H")); if hint == "" { return command }; return command + "\n\n" + hint
}

func (b *Bridge) runClaudeChatSMS(ctx context.Context, command string) (string, error) {
	mode, command := extractBrowserModeCommand(command); if mode == "" { mode = browserModeChat }
	dataDir := filepath.Dir(b.statePath); reply, err := claudeChatBrowserSendModeWithProgress(ctx, dataDir, b.composeClaudeChatSMSPrompt(command), mode, nil)
	return finishBrowserGeneratedImageTurn(ctx, command, reply, err)
}
func (b *Bridge) newClaudeChatConversation(ctx context.Context) error { return claudeChatBrowserNewConversation(ctx, filepath.Dir(b.statePath)) }
func (b *Bridge) newClaudeChatConversationMode(ctx context.Context, mode string) error { return claudeChatBrowserNewConversationMode(ctx, filepath.Dir(b.statePath), mode) }
