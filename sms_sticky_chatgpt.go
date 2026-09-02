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

const (
	chatGPTSMSAgent  = "G"
	chatGPTSMSPrefix = "G"
)

func explicitSMSAgent(raw string, cfg Config) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	candidates := []string{raw}
	if f := strings.Fields(raw); len(f) > 1 {
		candidates = append(candidates, strings.TrimSpace(strings.TrimPrefix(raw, f[0])))
	}
	newWord := configuredNewSessionCommand(cfg)
	for _, v := range candidates {
		for _, x := range []struct{ agent, prefix string }{
			{"C", configuredCodexPrefix(cfg)},
			{"A", configuredClaudePrefix(cfg)},
			{"G", configuredChatGPTPrefix(cfg)},
		} {
			if _, ok := stripAgentCommandPrefix(v, x.prefix); ok || isAgentNewSession(v, x.prefix, newWord) {
				return x.agent
			}
		}
	}
	return ""
}

func smsTargetAllowed(sourceAgent, target string) bool {
	sourceAgent = strings.ToUpper(strings.TrimSpace(sourceAgent))
	target = strings.ToUpper(strings.TrimSpace(target))
	if sourceAgent == "B" { // old test/config compatibility
		return target == "C" || target == "A" || target == "G"
	}
	return target != "" && strings.Contains(sourceAgent, target)
}

func selectStickySMSAgent(raw string, cfg Config, sourceAgent, sticky string) (string, error) {
	if explicit := explicitSMSAgent(raw, cfg); explicit != "" {
		if !smsTargetAllowed(sourceAgent, explicit) {
			return "", wrongAgentForNumber(sourceAgent, explicit)
		}
		return explicit, nil
	}
	sticky = strings.ToUpper(strings.TrimSpace(sticky))
	if smsTargetAllowed(sourceAgent, sticky) {
		return sticky, nil
	}
	// A phone that can reach only one CLI agent has no ambiguity. Shared phones
	// have no default: the first conversation must name C:, A:, or G: once.
	if sourceAgent == "C" || sourceAgent == "A" || sourceAgent == "G" {
		return sourceAgent, nil
	}
	return "", errors.New("no SMS agent is selected for this phone yet; start the message with C: for Codex, A: for Claude, or G: for ChatGPT Chat")
}

// authorizeChatGPTRaw uses ChatGPT Chat's own PIN policy. G no longer
// borrows whichever Codex/Claude permission happened to admit the phone.
func authorizeChatGPTRaw(raw string, cfg Config, _ string) (string, error) {
	s := agentSettings(cfg, "G")
	if !s.RequireCode {
		return strings.TrimSpace(raw), nil
	}
	f := strings.Fields(raw)
	if len(f) < 2 {
		return "", errors.New("missing the ChatGPT Chat security code or the command")
	}
	if !verifyAgentCode(s, f[0]) {
		return "", errors.New("invalid SMS security code for ChatGPT Chat")
	}
	return strings.TrimSpace(strings.TrimPrefix(raw, f[0])), nil
}

func parseChatGPTSMSCommand(raw string, cfg Config, sourceAgent string) (remoteCommand, error) {
	rest, err := authorizeChatGPTRaw(raw, cfg, sourceAgent)
	if err != nil {
		return remoteCommand{}, err
	}
	if strings.EqualFold(rest, "STATUS") {
		return remoteCommand{Status: true}, nil
	}
	newWord := configuredNewSessionCommand(cfg)
	if strings.EqualFold(rest, newWord) || isAgentNewSession(rest, configuredChatGPTPrefix(cfg), newWord) {
		return remoteCommand{Agent: "G", New: true}, nil
	}
	text := rest
	if tail, ok := stripAgentCommandPrefix(rest, configuredChatGPTPrefix(cfg)); ok {
		text = tail
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return remoteCommand{}, errors.New("empty ChatGPT command")
	}
	return remoteCommand{Agent: "G", Text: text}, nil
}

func parseRemoteCommandForMessageSticky(raw string, cfg Config, sourceAgent, sticky string, m GmailMessage) (remoteCommand, error) {
	target, err := selectStickySMSAgent(raw, cfg, sourceAgent, sticky)
	if err != nil {
		return remoteCommand{}, err
	}
	if strings.TrimSpace(raw) != "" {
		if target == "G" {
			return parseChatGPTSMSCommand(raw, cfg, sourceAgent)
		}
		return parseRemoteCommand(raw, cfg, target)
	}
	if target == "G" {
		return remoteCommand{}, errors.New("ChatGPT Chat over Google Voice supports text messages in this release; switch to C: or A: for an attachment")
	}
	return parseRemoteCommandForMessage(raw, cfg, target, m)
}

func stickySMSKey(sender string) string {
	if n := normalizeUSPhone(sender); n != "" {
		return n
	}
	return strings.TrimSpace(sender)
}

func (b *Bridge) stickySMSAgent(sender string) string {
	key := stickySMSKey(sender)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.LastAgentBySender == nil {
		return ""
	}
	return b.state.LastAgentBySender[key]
}

func (b *Bridge) rememberStickySMSAgent(sender, agent string) error {
	agent = strings.ToUpper(strings.TrimSpace(agent))
	if agent != "C" && agent != "A" && agent != "G" {
		return fmt.Errorf("unknown sticky SMS agent %q", agent)
	}
	key := stickySMSKey(sender)
	if key == "" {
		return errors.New("SMS sender is empty")
	}
	b.mu.Lock()
	if b.state.LastAgentBySender == nil {
		b.state.LastAgentBySender = map[string]string{}
	}
	b.state.LastAgentBySender[key] = agent
	s := b.state
	b.mu.Unlock()
	return saveState(b.statePath, s)
}

type chatGPTSMSReply struct {
	OK             bool   `json:"ok"`
	Reply          string `json:"reply"`
	Detail         string `json:"detail"`
	ConversationID string `json:"conversationId"`
}

func chatGPTBrowserSend(ctx context.Context, dataDir, prompt string) (string, error) {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s, err := ensureChatGPTReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": false})
	turnCtx, cancel := context.WithTimeout(ctx, 100*time.Second)
	b, code, err := chatGPTControlRequest(turnCtx, s, http.MethodPost, "/chat", strings.NewReader(string(payload)))
	cancel()
	if err != nil {
		return "", err
	}
	var out chatGPTSMSReply
	_ = json.Unmarshal(b, &out)
	if code != http.StatusOK || !out.OK {
		if strings.TrimSpace(out.Detail) == "" {
			out.Detail = strings.TrimSpace(string(b))
		}
		return "", errors.New(out.Detail)
	}
	if strings.TrimSpace(out.Reply) == "" {
		return "", errors.New("ChatGPT returned an empty reply")
	}
	return strings.TrimSpace(out.Reply), nil
}

func chatGPTBrowserNewConversation(ctx context.Context, dataDir string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s, err := ensureChatGPTReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
	b, code, err := chatGPTControlRequest(reqCtx, s, http.MethodPost, "/new", strings.NewReader(`{}`))
	cancel()
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		var out chatGPTSMSReply
		_ = json.Unmarshal(b, &out)
		if out.Detail != "" {
			return errors.New(out.Detail)
		}
		return fmt.Errorf("ChatGPT new-chat request returned HTTP %d", code)
	}
	return nil
}

func (b *Bridge) runChatGPTSMS(ctx context.Context, command string) (string, error) {
	dataDir := filepath.Dir(b.statePath)
	return chatGPTBrowserSend(ctx, dataDir, b.composePrompt("G", command))
}

func (b *Bridge) newChatGPTConversation(ctx context.Context) error {
	return chatGPTBrowserNewConversation(ctx, filepath.Dir(b.statePath))
}
