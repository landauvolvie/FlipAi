package main

import (
	"context"
	"path/filepath"
	"strings"
)

// remoteCommandHasTurn distinguishes "OW NEW:" (reset only) from
// "OW NEW: research this" (reset, then run the task). Browser-mode markers are
// transport metadata and do not count as task text.
func remoteCommandHasTurn(rc remoteCommand) bool {
	_, text := extractBrowserModeCommand(rc.Text)
	return strings.TrimSpace(text) != ""
}

func remoteCommandHasBrowserMode(rc remoteCommand) bool {
	mode, _ := extractBrowserModeCommand(rc.Text)
	return mode != ""
}

// remoteCommandDisplayName keeps status/receipt text aligned with the public
// route the sender actually selected. The execution engine alone is not enough
// to distinguish ChatGPT Chat from ChatGPT Work (or Claude Chat from its web
// Code/Cowork experiences), because those routes intentionally share an engine.
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

// resetAgentConversation resets exactly the destination selected by the public
// SMS route. For browser agents this preserves Chat vs Work/Cowork/Code rather
// than silently falling back to the provider's regular chat mode.
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
		return "New " + label + " session started.", b.newChatGPTConversationMode(ctx, mode)
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
	default:
		b.startNewClaudeSession()
		return "New Claude conversation started.", nil
	}
}

// runFreshAgentTurn performs the reset and the user's task as one queued bridge
// job. That guarantees no later SMS can slip between the reset and the first
// turn of the new conversation.
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
	default:
		return b.runCodexWithAttachments(ctx, rc.Text, rc.Sender, inbound)
	}
}

// Kept here as a tiny assertion helper used by tests and future browser routes.
func freshBrowserDataDir(b *Bridge) string {
	if b == nil {
		return ""
	}
	return filepath.Dir(b.statePath)
}
