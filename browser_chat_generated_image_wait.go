package main

import (
	"context"
	"strings"
)

// finishBrowserGeneratedImageTurn turns the browser providers' intermediate
// text state into a real completed media turn. The provider page keeps doing
// the image work; this function only decides whether FlipAi should wait for the
// media collector instead of forwarding a premature text placeholder/failure.
func finishBrowserGeneratedImageTurn(ctx context.Context, command, reply string, turnErr error) (string, error) {
	if !browserChatPromptRequestsGeneratedImage(command) {
		return reply, turnErr
	}

	// Most images are already present by the time the text turn returns. Give
	// that normal case a short grace period before deciding whether a longer
	// generation wait is needed.
	if waitForCapturedBrowserChatReturnedMedia(ctx, browserChatInitialMediaWait) {
		return completedBrowserGeneratedImageReply(reply), nil
	}

	shouldKeepWaiting := browserChatReplySuggestsPendingImage(reply)
	if turnErr != nil {
		shouldKeepWaiting = browserChatImageTurnCanStillBeRendering(turnErr)
	}
	if !shouldKeepWaiting {
		return reply, turnErr
	}

	if waitForCapturedBrowserChatReturnedMedia(ctx, browserChatGeneratedImageWait-browserChatInitialMediaWait) {
		return completedBrowserGeneratedImageReply(reply), nil
	}
	return reply, turnErr
}

func browserChatImageTurnCanStillBeRendering(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(s, "90 seconds") ||
		strings.Contains(s, "did not produce an assistant response") ||
		strings.Contains(s, "did not answer runtime.evaluate")
}

func completedBrowserGeneratedImageReply(reply string) string {
	if browserChatReplySuggestsPendingImage(reply) {
		return "Image created."
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return "Image created."
	}
	return reply
}
