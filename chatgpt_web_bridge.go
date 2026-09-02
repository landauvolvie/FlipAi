package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type chatGPTWebConversationState struct {
	ID        string    `json:"id,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

func loadChatGPTWebConversation(path string) chatGPTWebConversationState {
	var s chatGPTWebConversationState
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveChatGPTWebConversation(path string, s chatGPTWebConversationState) error {
	s.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

func (b *Bridge) newChatGPTWebConversation() error {
	if b == nil {
		return nil
	}
	err := os.Remove(chatGPTWebConversationPath(filepath.Dir(b.statePath)))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (b *Bridge) runChatGPTWithAttachments(ctx context.Context, command, sender string, inbound []InboundAttachment) (string, error) {
	if len(inbound) > 0 {
		return "", errors.New("ChatGPT Chat in FlipAi does not support SMS attachments yet; send the text command without an attachment, or use Codex/Claude for this message")
	}
	if b == nil {
		return "", errors.New("ChatGPT bridge is not available")
	}
	dataDir := filepath.Dir(b.statePath)
	conversationPath := chatGPTWebConversationPath(dataDir)
	conv := loadChatGPTWebConversation(conversationPath)
	client := newChatGPTWebClient(dataDir, b.statePath)
	prompt := b.composePrompt("G", command)
	b.setProgress("waiting for ChatGPT")
	started := time.Now()
	result, err := client.Chat(ctx, prompt, conv.ID, conv.ID == "")
	if err != nil {
		return "", err
	}
	if id := strings.TrimSpace(result.ConversationID); id != "" {
		if err := saveChatGPTWebConversation(conversationPath, chatGPTWebConversationState{ID: id}); err != nil {
			b.event("warn", "chatgpt", "ChatGPT answered, but FlipAi could not remember the conversation id: "+truncate(err.Error(), 180), sender, "G", "")
		}
	}
	b.timedEvent("success", "chatgpt", "ChatGPT SMS turn completed via "+safeCaptureLabel(result.Capture), sender, "G", "", time.Since(started))
	return strings.TrimSpace(result.Reply), nil
}
