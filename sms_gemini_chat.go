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

func authorizeGeminiChatRaw(raw string, cfg Config) (string, error) {
	s := agentSettings(cfg, "M")
	if !s.RequireCode {
		return strings.TrimSpace(raw), nil
	}
	f := strings.Fields(raw)
	if len(f) < 2 {
		return "", errors.New("missing the Gemini Chat security code or the command")
	}
	if !verifyAgentCode(s, f[0]) {
		return "", errors.New("invalid SMS security code for Gemini Chat")
	}
	return strings.TrimSpace(strings.TrimPrefix(raw, f[0])), nil
}

func parseGeminiChatSMSCommand(raw string, cfg Config) (remoteCommand, error) {
	rest, err := authorizeGeminiChatRaw(raw, cfg)
	if err != nil {
		return remoteCommand{}, err
	}
	if strings.EqualFold(rest, "STATUS") {
		return remoteCommand{Status: true}, nil
	}
	newWord := configuredNewSessionCommand(cfg)
	if strings.EqualFold(rest, newWord) || isAgentNewSession(rest, configuredGeminiChatPrefix(cfg), newWord) {
		return remoteCommand{Agent: "M", New: true}, nil
	}
	text := rest
	if tail, ok := stripAgentCommandPrefix(rest, configuredGeminiChatPrefix(cfg)); ok {
		text = tail
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return remoteCommand{}, errors.New("empty Gemini Chat command")
	}
	return remoteCommand{Agent: "M", Text: text}, nil
}

type geminiChatSMSReply struct {
	OK             bool   `json:"ok"`
	Reply          string `json:"reply"`
	Detail         string `json:"detail"`
	ConversationID string `json:"conversationId"`
}

func geminiChatBrowserSend(ctx context.Context, dataDir, prompt string) (string, error) {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s, err := ensureGeminiChatReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": false})
	turnCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	b, code, err := geminiChatControlRequest(turnCtx, s, http.MethodPost, "/chat", strings.NewReader(string(payload)))
	cancel()
	if err != nil {
		return "", err
	}
	var out geminiChatSMSReply
	_ = json.Unmarshal(b, &out)
	if code != http.StatusOK || !out.OK {
		if strings.TrimSpace(out.Detail) == "" {
			out.Detail = strings.TrimSpace(string(b))
		}
		return "", errors.New(out.Detail)
	}
	if strings.TrimSpace(out.Reply) == "" {
		return "", errors.New("Gemini Chat returned an empty reply")
	}
	return strings.TrimSpace(out.Reply), nil
}

func geminiChatBrowserNewConversation(ctx context.Context, dataDir string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s, err := ensureGeminiChatReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
	b, code, err := geminiChatControlRequest(reqCtx, s, http.MethodPost, "/new", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		var out geminiChatSMSReply
		_ = json.Unmarshal(b, &out)
		if out.Detail != "" {
			return errors.New(out.Detail)
		}
		return fmt.Errorf("Gemini Chat new-chat request returned HTTP %d", code)
	}
	return nil
}

func (b *Bridge) composeGeminiChatSMSPrompt(command string) string {
	command = strings.TrimSpace(command)
	hint := strings.TrimSpace(b.cfg.replyStyleHintFor("M"))
	if hint == "" {
		hint = defaultReplyStyleHint
	}
	if hint == "" {
		return command
	}
	return command + "\n\n" + hint
}

func (b *Bridge) runGeminiChatSMS(ctx context.Context, command string) (string, error) {
	return geminiChatBrowserSend(ctx, filepath.Dir(b.statePath), b.composeGeminiChatSMSPrompt(command))
}

func (b *Bridge) newGeminiChatConversation(ctx context.Context) error {
	return geminiChatBrowserNewConversation(ctx, filepath.Dir(b.statePath))
}
