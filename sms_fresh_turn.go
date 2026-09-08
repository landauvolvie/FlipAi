package main

import (
	"context"
	"path/filepath"
	"strings"
)

func remoteCommandHasTurn(rc remoteCommand) bool {
	_, text := extractBrowserModeCommand(rc.Text)
	return strings.TrimSpace(text) != ""
}

func remoteCommandHasBrowserMode(rc remoteCommand) bool {
	mode, _ := extractBrowserModeCommand(rc.Text)
	return mode != ""
}

func remoteCommandDisplayName(rc remoteCommand) string {
	mode, _ := extractBrowserModeCommand(rc.Text)
	switch rc.Agent {
	case "G":
		if mode == browserModeWork {
			return "ChatGPT Work"
		}
		return "ChatGPT Chat"
	case "H":
		switch mode {
		case browserModeCowork:
			return "Claude Cowork"
		case browserModeCode:
			return "Claude Code Web"
		default:
			return "Claude Chat"
		}
	default:
		return agentDisplayName(rc.Agent)
	}
}

func chatGPTFreshResetMode(requested string) string {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" || requested == browserModeWork {
		return browserModeChat
	}
	return requested
}

func (b *Bridge) resetAgentConversation(ctx context.Context, rc remoteCommand) (string, error) {
	mode, _ := extractBrowserModeCommand(rc.Text)
	switch rc.Agent {
	case "C":
		return "New Codex conversation started.", b.newCodexThread(ctx)
	case "A":
		b.startNewClaudeSession()
		return "New Claude Code Local conversation started.", nil
	case "G":
		if mode == "" {
			mode = browserModeChat
		}
		label := "ChatGPT Chat"
		if mode == browserModeWork {
			label = "ChatGPT Work"
		}
		return "New " + label + " session started.", b.newChatGPTConversationMode(ctx, chatGPTFreshResetMode(mode))
	case "H":
		if mode == "" {
			mode = browserModeChat
		}
		label := "Claude Chat"
		switch mode {
		case browserModeCowork:
			label = "Claude Cowork"
		case browserModeCode:
			label = "Claude Code Web"
		}
		return "New " + label + " session started.", b.newClaudeChatConversationMode(ctx, mode)
	case "M":
		return "New Gemini Chat conversation started.", b.newGeminiChatConversation(ctx)
	case "X":
		return "New Grok Chat conversation started.", b.newGrokChatConversation(ctx)
	case "P":
		return "New Microsoft Copilot Chat conversation started.", b.newCopilotChatConversation(ctx)
	case "U":
		return "New Muse conversation started.", b.newMuseChatConversation(ctx)
	default:
		b.startNewClaudeSession()
		return "New Claude conversation started.", nil
	}
}

func (b *Bridge) runFreshAgentTurn(ctx context.Context, rc remoteCommand, inbound []InboundAttachment) (string, error) {
	if _, err := b.resetAgentConversation(ctx, rc); err != nil {
		return "", err
	}
	if rc.Agent == "A" {
		return b.runClaudeWithAttachments(ctx, rc.Text, rc.Sender, inbound)
	}
	if isBrowserChatAgent(rc.Agent) && len(inbound) > 0 {
		return b.runBrowserChatSMSWithAttachments(ctx, rc.Agent, rc.Text, inbound)
	}
	switch rc.Agent {
	case "G":
		return b.runChatGPTSMS(ctx, rc.Text)
	case "H":
		return b.runClaudeChatSMS(ctx, rc.Text)
	case "M":
		return b.runGeminiChatSMS(ctx, rc.Text)
	case "X":
		return b.runGrokChatSMS(ctx, rc.Text)
	case "P":
		return b.runCopilotChatSMS(ctx, rc.Text)
	case "U":
		return b.runMuseChatSMS(ctx, rc.Text)
	default:
		return b.runCodexWithAttachments(ctx, rc.Text, rc.Sender, inbound)
	}
}

func freshBrowserDataDir(b *Bridge) string {
	if b == nil {
		return ""
	}
	return filepath.Dir(b.statePath)
}
