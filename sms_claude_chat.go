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
	if !s.RequireCode {
		return strings.TrimSpace(raw), nil
	}
	f := strings.Fields(raw)
	if len(f) < 2 {
		return "", errors.New("missing the Claude Chat security code or the command")
	}
	if !verifyAgentCode(s, f[0]) {
		return "", errors.New("invalid SMS security code for Claude Chat")
	}
	return strings.TrimSpace(strings.TrimPrefix(raw, f[0])), nil
}

func parseClaudeChatSMSCommand(raw string, cfg Config) (remoteCommand, error) {
	rest, err := authorizeClaudeChatRaw(raw, cfg)
	if err != nil {
		return remoteCommand{}, err
	}
	if strings.EqualFold(rest, "STATUS") {
		return remoteCommand{Status: true}, nil
	}
	newWord := configuredNewSessionCommand(cfg)
	if strings.EqualFold(rest, newWord) || isAgentNewSession(rest, configuredClaudeChatPrefix(cfg), newWord) {
		return remoteCommand{Agent: "H", New: true}, nil
	}
	text := rest
	if tail, ok := stripAgentCommandPrefix(rest, configuredClaudeChatPrefix(cfg)); ok {
		text = tail
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return remoteCommand{}, errors.New("empty Claude Chat command")
	}
	return remoteCommand{Agent: "H", Text: text}, nil
}

type claudeChatSMSReply struct {
	OK             bool   `json:"ok"`
	Reply          string `json:"reply"`
	Detail         string `json:"detail"`
	ConversationID string `json:"conversationId"`
}

func claudeChatBrowserSend(ctx context.Context, dataDir, prompt string) (string, error) {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s, err := ensureClaudeChatReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": false})
	turnCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	b, code, err := claudeChatControlRequest(turnCtx, s, http.MethodPost, "/chat", strings.NewReader(string(payload)))
	cancel()
	if err != nil {
		return "", err
	}
	var out claudeChatSMSReply
	_ = json.Unmarshal(b, &out)
	if code != http.StatusOK || !out.OK {
		if strings.TrimSpace(out.Detail) == "" {
			out.Detail = strings.TrimSpace(string(b))
		}
		return "", errors.New(out.Detail)
	}
	if strings.TrimSpace(out.Reply) == "" {
		return "", errors.New("Claude Chat returned an empty reply")
	}
	return strings.TrimSpace(out.Reply), nil
}

func claudeChatBrowserNewConversation(ctx context.Context, dataDir string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s, err := ensureClaudeChatReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
	b, code, err := claudeChatControlRequest(reqCtx, s, http.MethodPost, "/new", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		var out claudeChatSMSReply
		_ = json.Unmarshal(b, &out)
		if out.Detail != "" {
			return errors.New(out.Detail)
		}
		return fmt.Errorf("Claude Chat new-chat request returned HTTP %d", code)
	}
	return nil
}

func (b *Bridge) composeClaudeChatSMSPrompt(command string) string {
	command = strings.TrimSpace(command)
	hint := strings.TrimSpace(b.cfg.replyStyleHintFor("H"))
	if hint == "" {
		hint = defaultReplyStyleHint
	}
	if hint == "" {
		return command
	}
	return command + "\n\n" + hint
}

func (b *Bridge) runClaudeChatSMS(ctx context.Context, command string) (string, error) {
	return claudeChatBrowserSend(ctx, filepath.Dir(b.statePath), b.composeClaudeChatSMSPrompt(command))
}

func (b *Bridge) newClaudeChatConversation(ctx context.Context) error {
	return claudeChatBrowserNewConversation(ctx, filepath.Dir(b.statePath))
}
