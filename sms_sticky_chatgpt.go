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
	chatGPTSMSPrefix = "O"
)

// explicitSMSAgent is retained for callers/tests that care about the internal
// execution engine. Public SMS shortcuts are resolved through explicitSMSRoute.
func explicitSMSAgent(raw string, cfg Config) string {
	id := explicitSMSRoute(raw, cfg)
	if route, ok := smsRouteByID(cfg, id); ok {
		return route.Agent
	}
	return ""
}

func smsTargetAllowed(sourceAgent, target string) bool {
	sourceAgent = strings.ToUpper(strings.TrimSpace(sourceAgent))
	target = strings.ToUpper(strings.TrimSpace(target))
	if sourceAgent == "B" {
		return target == "C" || target == "A" || target == "G" || target == "H" || target == "M" || target == "X" || target == "P"
	}
	return target != "" && strings.Contains(sourceAgent, target)
}

// selectStickySMSAgent remains as an internal-engine compatibility wrapper.
func selectStickySMSAgent(raw string, cfg Config, sourceAgent, sticky string) (string, error) {
	route, err := selectStickySMSRoute(raw, cfg, sourceAgent, sticky)
	if err != nil {
		return "", err
	}
	return route.Agent, nil
}

func authorizeChatGPTRaw(raw string, cfg Config, _ string) (string, error) {
	s := agentSettings(cfg, "G")
	if !s.RequireCode {
		return strings.TrimSpace(raw), nil
	}
	f := strings.Fields(raw)
	if len(f) < 2 {
		return "", errors.New("missing the ChatGPT security code or the command")
	}
	if !verifyAgentCode(s, f[0]) {
		return "", errors.New("invalid SMS security code for ChatGPT")
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
	route, err := selectStickySMSRoute(raw, cfg, sourceAgent, sticky)
	if err != nil {
		return remoteCommand{}, err
	}
	rewritten := rewriteSMSRoutePrefix(raw, route.Prefix, underlyingPrefixForRoute(cfg, route))
	var rc remoteCommand
	if strings.TrimSpace(raw) != "" {
		switch route.Agent {
		case "G":
			rc, err = parseChatGPTSMSCommand(rewritten, cfg, sourceAgent)
		case "H":
			rc, err = parseClaudeChatSMSCommand(rewritten, cfg)
		case "M":
			rc, err = parseGeminiChatSMSCommand(rewritten, cfg)
		case "X":
			rc, err = parseGrokChatSMSCommand(rewritten, cfg)
		case "P":
			rc, err = parseCopilotChatSMSCommand(rewritten, cfg)
		default:
			rc, err = parseRemoteCommand(rewritten, cfg, route.Agent)
		}
	} else if isBrowserChatAgent(route.Agent) {
		rc, err = parseBrowserChatAttachmentOnlyCommand(cfg, route.Agent, m)
	} else {
		rc, err = parseRemoteCommandForMessage(rewritten, cfg, route.Agent, m)
	}
	if err != nil {
		return remoteCommand{}, err
	}
	if !rc.Status {
		rc.Route = route.ID
		rc.Mode = route.Mode
		if rc.Agent == "" {
			rc.Agent = route.Agent
		}
	}
	return rc, nil
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

func (b *Bridge) rememberStickySMSAgent(sender, routeID string) error {
	routeID = strings.ToUpper(strings.TrimSpace(routeID))
	if _, ok := smsRouteByID(b.cfg, routeID); !ok {
		return fmt.Errorf("unknown sticky SMS route %q", routeID)
	}
	key := stickySMSKey(sender)
	if key == "" {
		return errors.New("SMS sender is empty")
	}
	b.mu.Lock()
	if b.state.LastAgentBySender == nil {
		b.state.LastAgentBySender = map[string]string{}
	}
	b.state.LastAgentBySender[key] = "route:" + routeID
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

func chatGPTBrowserSendMode(ctx context.Context, dataDir, prompt, mode string) (string, error) {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s, err := ensureChatGPTReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"prompt": prompt, "new": false, "mode": mode})
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

func chatGPTBrowserSend(ctx context.Context, dataDir, prompt string) (string, error) {
	return chatGPTBrowserSendMode(ctx, dataDir, prompt, browserModeChat)
}

func chatGPTBrowserNewConversationMode(ctx context.Context, dataDir, mode string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	s, err := ensureChatGPTReady(readyCtx, dataDir)
	cancel()
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"mode": mode})
	reqCtx, cancel := context.WithTimeout(ctx, 55*time.Second)
	b, code, err := chatGPTControlRequest(reqCtx, s, http.MethodPost, "/new", strings.NewReader(string(payload)))
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

func chatGPTBrowserNewConversation(ctx context.Context, dataDir string) error {
	return chatGPTBrowserNewConversationMode(ctx, dataDir, browserModeChat)
}

func (b *Bridge) composeChatGPTSMSPrompt(command string) string {
	command = strings.TrimSpace(command)
	hint := strings.TrimSpace(b.cfg.replyStyleHintFor("G"))
	if hint == "" {
		hint = defaultReplyStyleHint
	}
	if hint == "" {
		return command
	}
	return command + "\n\n" + hint
}

func (b *Bridge) runChatGPTSMSMode(ctx context.Context, command, mode string) (string, error) {
	dataDir := filepath.Dir(b.statePath)
	if mode == "" {
		mode = browserModeChat
	}
	return chatGPTBrowserSendMode(ctx, dataDir, b.composeChatGPTSMSPrompt(command), mode)
}

func (b *Bridge) runChatGPTSMS(ctx context.Context, command string) (string, error) {
	return b.runChatGPTSMSMode(ctx, command, browserModeChat)
}

func (b *Bridge) newChatGPTConversationMode(ctx context.Context, mode string) error {
	if mode == "" {
		mode = browserModeChat
	}
	return chatGPTBrowserNewConversationMode(ctx, filepath.Dir(b.statePath), mode)
}

func (b *Bridge) newChatGPTConversation(ctx context.Context) error {
	return b.newChatGPTConversationMode(ctx, browserModeChat)
}
