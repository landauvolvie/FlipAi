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
	if !s.RequireCode {
		return strings.TrimSpace(raw), nil
	}
	f := strings.Fields(raw)
	if len(f) < 2 {
		return "", errors.New("missing the Muse security code or the command")
	}
	if !verifyAgentCode(s, f[0]) {
		return "", errors.New("invalid SMS security code for Muse")
	}
	return strings.TrimSpace(strings.TrimPrefix(raw, f[0])), nil
}

func parseMuseChatSMSCommand(raw string, cfg Config) (remoteCommand, error) {
	rest, err := authorizeMuseChatRaw(raw, cfg)
	if err != nil {
		return remoteCommand{}, err
	}
	if strings.EqualFold(rest, "STATUS") {
		return remoteCommand{Status: true}, nil
	}
	newWord := configuredNewSessionCommand(cfg)
	if strings.EqualFold(rest, newWord) || isAgentNewSession(rest, configuredMuseChatPrefix(cfg), newWord) {
		return remoteCommand{Agent: "U", New: true}, nil
	}
	text := rest
	if tail, ok := stripAgentCommandPrefix(rest, configuredMuseChatPrefix(cfg)); ok {
		text = tail
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return remoteCommand{}, errors.New("empty Muse command")
	}
	return remoteCommand{Agent: "U", Text: text}, nil
}

type museChatSMSReply struct {
	OK             bool   `json:"ok"`
	Reply          string `json:"reply"`
	Detail         string `json:"detail"`
	ConversationID string `json:"conversationId"`
}

func museChatBrowserSendWithProgress(ctx context.Context, dataDir, prompt string, onProgress func(string)) (string, error) {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s, err := ensureMuseChatReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": false})
	turnCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	b, code, err := museChatControlRequest(turnCtx, s, http.MethodPost, "/chat", strings.NewReader(string(payload)))
	cancel()
	if err != nil {
		return "", err
	}
	var out museChatSMSReply
	_ = json.Unmarshal(b, &out)
	if code != http.StatusOK || !out.OK {
		if strings.TrimSpace(out.Detail) == "" {
			out.Detail = strings.TrimSpace(string(b))
		}
		if browserLongTurnTimeoutDetail(out.Detail) {
			return waitForBrowserLongTurn(ctx, dataDir, "U", onProgress)
		}
		return "", errors.New(out.Detail)
	}
	if strings.TrimSpace(out.Reply) == "" {
		return "", errors.New("Muse returned an empty reply")
	}
	return strings.TrimSpace(out.Reply), nil
}

func museChatBrowserSend(ctx context.Context, dataDir, prompt string) (string, error) {
	return museChatBrowserSendWithProgress(ctx, dataDir, prompt, nil)
}

func museChatBrowserNewConversation(ctx context.Context, dataDir string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s, err := ensureMuseChatReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
	b, code, err := museChatControlRequest(reqCtx, s, http.MethodPost, "/new", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		var out museChatSMSReply
		_ = json.Unmarshal(b, &out)
		if out.Detail != "" {
			return errors.New(out.Detail)
		}
		return fmt.Errorf("Muse new-chat request returned HTTP %d", code)
	}
	return nil
}

func (b *Bridge) composeMuseChatSMSPrompt(command string) string {
	command = strings.TrimSpace(command)
	hint := strings.TrimSpace(b.cfg.replyStyleHintFor("U"))
	if hint == "" {
		hint = defaultReplyStyleHint
	}
	if hint == "" {
		return command
	}
	return command + "\n\n" + hint
}

func (b *Bridge) runMuseChatSMS(ctx context.Context, command string) (string, error) {
	dataDir := filepath.Dir(b.statePath)
	reply, err := museChatBrowserSendWithProgress(ctx, dataDir, b.composeMuseChatSMSPrompt(command), b.setProgress)
	return finishBrowserGeneratedImageTurn(ctx, command, reply, err)
}

func (b *Bridge) newMuseChatConversation(ctx context.Context) error {
	return museChatBrowserNewConversation(ctx, filepath.Dir(b.statePath))
}
