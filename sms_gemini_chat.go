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

// cleanGeminiChatReply removes Gemini's accessibility-only speaker label from
// the extracted DOM text. Gemini visually renders only the answer, but its
// response container can expose text such as "Gemini said Hello" to assistive
// technology. That label is page chrome, not part of the model's reply.
func cleanGeminiChatReply(reply string) string {
	s := strings.TrimSpace(reply)
	lower := strings.ToLower(s)
	const label = "gemini said"
	if !strings.HasPrefix(lower, label) {
		return s
	}
	if len(s) == len(label) {
		return s
	}
	rest := s[len(label):]
	if rest == "" {
		return s
	}
	first := rest[0]
	if first != ':' && first != '-' && first != '\n' && first != '\r' && first != '\t' && first != ' ' {
		return s
	}
	rest = strings.TrimSpace(strings.TrimLeft(rest, ":-"))
	if rest == "" {
		return s
	}
	return rest
}

func geminiChatBrowserSendWithProgress(ctx context.Context, dataDir, prompt string, onProgress func(string)) (string, error) {
	_ = onProgress // model thought/intermediate UI is intentionally never forwarded
	if !loadGeminiChatRuntime(dataDir).Connected {
		return "", errors.New("Gemini Chat is disconnected in FlipAi. Open FlipAi > Agents, press Connect for Gemini Chat, then try again")
	}
	// Browser models must prove a live signed-in session before FlipAi sends a
	// delayed "working on it" receipt. A dead/expired session therefore fails
	// clearly instead of producing endless progress texts.
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	s, err := ensureGeminiChatReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return "", fmt.Errorf("Gemini Chat is not connected and ready in FlipAi. Open FlipAi > Agents and reconnect Gemini Chat, then try again: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": false})
	turnCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	body, code, err := geminiChatControlRequest(turnCtx, s, http.MethodPost, "/chat", strings.NewReader(string(payload)))
	cancel()
	if err != nil {
		return "", err
	}
	var out geminiChatSMSReply
	_ = json.Unmarshal(body, &out)
	if code != http.StatusOK || !out.OK {
		if strings.TrimSpace(out.Detail) == "" {
			out.Detail = strings.TrimSpace(string(body))
		}
		if browserLongTurnTimeoutDetail(out.Detail) {
			reply, waitErr := waitForBrowserLongTurn(ctx, dataDir, "M", nil)
			cleaned := cleanGeminiChatReply(reply)
			if waitErr == nil && browserReplyEchoesPrompt(cleaned, prompt) {
				return "", errors.New("Gemini Chat did not produce a fresh assistant reply; FlipAi received the submitted prompt back instead. Reconnect Gemini Chat and try again")
			}
			return cleaned, waitErr
		}
		return "", errors.New(out.Detail)
	}
	cleaned := cleanGeminiChatReply(out.Reply)
	if cleaned == "" {
		return "", errors.New("Gemini Chat returned an empty reply")
	}
	if browserReplyEchoesPrompt(cleaned, prompt) {
		return "", errors.New("Gemini Chat did not produce a fresh assistant reply; FlipAi received the submitted prompt back instead. Reconnect Gemini Chat and try again")
	}
	return cleaned, nil
}

func geminiChatBrowserSend(ctx context.Context, dataDir, prompt string) (string, error) {
	return geminiChatBrowserSendWithProgress(ctx, dataDir, prompt, nil)
}

func geminiChatBrowserNewConversation(ctx context.Context, dataDir string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	s, err := ensureGeminiChatReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
	body, code, err := geminiChatControlRequest(reqCtx, s, http.MethodPost, "/new", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		var out geminiChatSMSReply
		_ = json.Unmarshal(body, &out)
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
	dataDir := filepath.Dir(b.statePath)
	reply, err := geminiChatBrowserSendWithProgress(ctx, dataDir, b.composeGeminiChatSMSPrompt(command), nil)
	return finishBrowserGeneratedImageTurn(ctx, command, reply, err)
}

func (b *Bridge) newGeminiChatConversation(ctx context.Context) error {
	return geminiChatBrowserNewConversation(ctx, filepath.Dir(b.statePath))
}
