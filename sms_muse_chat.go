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

func authorizeMuseChatRaw(raw string, cfg Config) (string, error) {
	s := agentSettings(cfg, "U")
	if !s.RequireCode { return strings.TrimSpace(raw), nil }
	f := strings.Fields(raw)
	if len(f) < 2 { return "", errors.New("missing the Muse security code or the command") }
	if !verifyAgentCode(s, f[0]) { return "", errors.New("invalid SMS security code for Muse") }
	return strings.TrimSpace(strings.TrimPrefix(raw, f[0])), nil
}

func parseMuseChatSMSCommand(raw string, cfg Config) (remoteCommand, error) {
	rest, err := authorizeMuseChatRaw(raw, cfg); if err != nil { return remoteCommand{}, err }
	if strings.EqualFold(rest, "STATUS") { return remoteCommand{Status: true}, nil }
	newWord := configuredNewSessionCommand(cfg)
	if strings.EqualFold(rest, newWord) || isAgentNewSession(rest, configuredMuseChatPrefix(cfg), newWord) { return remoteCommand{Agent: "U", New: true}, nil }
	text := rest; if tail, ok := stripAgentCommandPrefix(rest, configuredMuseChatPrefix(cfg)); ok { text = tail }
	text = strings.TrimSpace(text); if text == "" { return remoteCommand{}, errors.New("empty Muse command") }
	return remoteCommand{Agent: "U", Text: text}, nil
}

type museChatSMSReply struct { OK bool `json:"ok"`; Reply string `json:"reply"`; Detail string `json:"detail"`; ConversationID string `json:"conversationId"`; Trace string `json:"trace"` }

func museChatBrowserSendWithProgress(ctx context.Context, dataDir, prompt string, onProgress func(string)) (string, error) {
	_ = onProgress
	if !loadMuseChatRuntime(dataDir).Connected { return "", errors.New("Muse is disconnected in FlipAi. Open FlipAi > Agents, press Connect for Muse, then try again") }
	readyCtx, cancel := context.WithTimeout(ctx, browserChatTurnReadyWait); s, err := ensureMuseChatReady(readyCtx, dataDir); cancel(); browserTurnReady(ctx, "Muse", err)
	if err != nil { return "", fmt.Errorf("Muse is not connected and ready in FlipAi. Open FlipAi > Agents and reconnect Muse, then try again: %w", err) }
	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": false})
	turnCtx, cancel := context.WithTimeout(ctx, browserChatTurnRequestBudget); sentAt := time.Now(); body, code, err := museChatControlRequest(turnCtx, s, http.MethodPost, "/chat", strings.NewReader(string(payload))); cancel()
	if err != nil { browserTurnRequestFailed(ctx, "Muse", time.Since(sentAt), err); if browserChatTurnRequestTimedOut(err) { browserTurnWatching(ctx, "Muse"); reply, waitErr := waitForBrowserLongTurn(ctx, dataDir, "U", nil); browserTurnWatchDone(ctx, "Muse", reply, waitErr); return cleanBrowserChatReplyForPrompt(reply, prompt), waitErr }; return "", err }
	var out museChatSMSReply; _ = json.Unmarshal(body, &out)
	browserTurnAnswered(ctx, "Muse", code, time.Since(sentAt), code == http.StatusOK && out.OK, out.Trace, out.Detail, out.Reply)
	if code != http.StatusOK || !out.OK {
		if strings.TrimSpace(out.Detail) == "" { out.Detail = strings.TrimSpace(string(body)) }
		if browserLongTurnTimeoutDetail(out.Detail) { browserTurnWatching(ctx, "Muse"); reply, waitErr := waitForBrowserLongTurn(ctx, dataDir, "U", nil); browserTurnWatchDone(ctx, "Muse", reply, waitErr); return cleanBrowserChatReplyForPrompt(reply, prompt), waitErr }
		return "", errors.New(out.Detail)
	}
	cleaned := cleanBrowserChatReplyForPrompt(out.Reply, prompt)
	if cleaned == "" { return "", errors.New("Muse returned an empty reply") }
	return cleaned, nil
}

func museChatBrowserSend(ctx context.Context, dataDir, prompt string) (string, error) { return museChatBrowserSendWithProgress(ctx, dataDir, prompt, nil) }
func museChatBrowserNewConversation(ctx context.Context, dataDir string) error {
	readyCtx, cancel := context.WithTimeout(ctx, browserChatTurnReadyWait); s, err := ensureMuseChatReady(readyCtx, dataDir); cancel(); if err != nil { return err }
	reqCtx, cancel := context.WithTimeout(ctx, 55*time.Second); body, code, err := museChatControlRequest(reqCtx, s, http.MethodPost, "/new", strings.NewReader(`{}`)); cancel(); if err != nil { return err }
	if code != http.StatusOK { var out museChatSMSReply; _ = json.Unmarshal(body, &out)
	if out.Detail != "" { return errors.New(out.Detail) }; return fmt.Errorf("Muse new-chat request returned HTTP %d", code) }
	return nil
}

func (b *Bridge) composeMuseChatSMSPrompt(command string) string { command = strings.TrimSpace(command); hint := strings.TrimSpace(b.cfg.replyStyleHintFor("U")); if hint == "" { return command }; return command + "\n\n" + hint }
func (b *Bridge) runMuseChatSMS(ctx context.Context, command string) (string, error) { dataDir := filepath.Dir(b.statePath); reply, err := museChatBrowserSendWithProgress(ctx, dataDir, b.composeMuseChatSMSPrompt(command), nil); return finishBrowserGeneratedImageTurn(ctx, command, reply, err) }
func (b *Bridge) newMuseChatConversation(ctx context.Context) error { return museChatBrowserNewConversation(ctx, filepath.Dir(b.statePath)) }
