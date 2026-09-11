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

func authorizeCopilotChatRaw(raw string, cfg Config) (string, error) {
	s := agentSettings(cfg, "P")
	if !s.RequireCode { return strings.TrimSpace(raw), nil }
	f := strings.Fields(raw)
	if len(f) < 2 { return "", errors.New("missing the Microsoft Copilot Chat security code or the command") }
	if !verifyAgentCode(s, f[0]) { return "", errors.New("invalid SMS security code for Microsoft Copilot Chat") }
	return strings.TrimSpace(strings.TrimPrefix(raw, f[0])), nil
}

func parseCopilotChatSMSCommand(raw string, cfg Config) (remoteCommand, error) {
	rest, err := authorizeCopilotChatRaw(raw, cfg); if err != nil { return remoteCommand{}, err }
	if strings.EqualFold(rest, "STATUS") { return remoteCommand{Status: true}, nil }
	newWord := configuredNewSessionCommand(cfg)
	if strings.EqualFold(rest, newWord) || isAgentNewSession(rest, configuredCopilotChatPrefix(cfg), newWord) { return remoteCommand{Agent: "P", New: true}, nil }
	text := rest; if tail, ok := stripAgentCommandPrefix(rest, configuredCopilotChatPrefix(cfg)); ok { text = tail }
	text = strings.TrimSpace(text); if text == "" { return remoteCommand{}, errors.New("empty Microsoft Copilot Chat command") }
	return remoteCommand{Agent: "P", Text: text}, nil
}

type copilotChatSMSReply struct { OK bool `json:"ok"`; Reply string `json:"reply"`; Detail string `json:"detail"`; ConversationID string `json:"conversationId"` }

func copilotChatBrowserSendWithProgress(ctx context.Context, dataDir, prompt string, onProgress func(string)) (string, error) {
	_ = onProgress
	if !loadCopilotChatRuntime(dataDir).Connected { return "", errors.New("Microsoft Copilot Chat is disconnected in FlipAi. Open FlipAi > Agents, press Connect for Microsoft Copilot Chat, then try again") }
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second); s, err := ensureCopilotChatReady(readyCtx, dataDir); cancel()
	if err != nil { return "", fmt.Errorf("Microsoft Copilot Chat is not connected and ready in FlipAi. Open FlipAi > Agents and reconnect Microsoft Copilot Chat, then try again: %w", err) }
	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": false})
	turnCtx, cancel := context.WithTimeout(ctx, 100*time.Second); body, code, err := copilotChatControlRequest(turnCtx, s, http.MethodPost, "/chat", strings.NewReader(string(payload))); cancel(); if err != nil { return "", err }
	var out copilotChatSMSReply; _ = json.Unmarshal(body, &out)
	if code != http.StatusOK || !out.OK {
		if strings.TrimSpace(out.Detail) == "" { out.Detail = strings.TrimSpace(string(body)) }
		if browserLongTurnTimeoutDetail(out.Detail) { return waitForBrowserLongTurn(ctx, dataDir, "P", nil) }
		return "", errors.New(out.Detail)
	}
	if strings.TrimSpace(out.Reply) == "" { return "", errors.New("Microsoft Copilot Chat returned an empty reply") }
	return strings.TrimSpace(out.Reply), nil
}

func copilotChatBrowserSend(ctx context.Context, dataDir, prompt string) (string, error) { return copilotChatBrowserSendWithProgress(ctx, dataDir, prompt, nil) }
func copilotChatBrowserNewConversation(ctx context.Context, dataDir string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second); s, err := ensureCopilotChatReady(readyCtx, dataDir); cancel(); if err != nil { return err }
	reqCtx, cancel := context.WithTimeout(ctx, 55*time.Second); body, code, err := copilotChatControlRequest(reqCtx, s, http.MethodPost, "/new", strings.NewReader(`{}`)); cancel(); if err != nil { return err }
	if code != http.StatusOK { var out copilotChatSMSReply; _ = json.Unmarshal(body, &out); if out.Detail != "" { return errors.New(out.Detail) }; return fmt.Errorf("Microsoft Copilot Chat new-chat request returned HTTP %d", code) }
	return nil
}

func (b *Bridge) composeCopilotChatSMSPrompt(command string) string { command = strings.TrimSpace(command); hint := strings.TrimSpace(b.cfg.replyStyleHintFor("P")); if hint == "" { return command }; return command + "\n\n" + hint }
func (b *Bridge) runCopilotChatSMS(ctx context.Context, command string) (string, error) { dataDir := filepath.Dir(b.statePath); reply, err := copilotChatBrowserSendWithProgress(ctx, dataDir, b.composeCopilotChatSMSPrompt(command), nil); return finishBrowserGeneratedImageTurn(ctx, command, reply, err) }
func (b *Bridge) newCopilotChatConversation(ctx context.Context) error { return copilotChatBrowserNewConversation(ctx, filepath.Dir(b.statePath)) }
