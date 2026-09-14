package main

import (
	"context"
	"strings"
	"time"
)

// browserChatGeneratedImageMaxWait bounds the wait for media a provider may
// still be rendering. It outlasts any realistic image generation while still
// guaranteeing the turn ends.
const browserChatGeneratedImageMaxWait = 10 * time.Minute

// finishBrowserGeneratedImageTurn turns the browser providers' intermediate
// text state into a real completed media turn. The provider page keeps doing
// the image work; this function only decides whether FlipAi should wait for the
// media collector instead of forwarding a premature text placeholder/failure.
func finishBrowserGeneratedImageTurn(ctx context.Context, command, reply string, turnErr error) (string, error) {
	requestedImage := browserChatPromptRequestsGeneratedImage(command)
	// A turn that produced no text at all is only an image turn if the prompt
	// asked for one. Otherwise it is an ordinary text turn that failed, and it
	// must surface that failure now rather than wait for media.
	pendingImage := browserChatReplySuggestsPendingImage(reply) ||
		(requestedImage && strings.TrimSpace(reply) == "")
	if !requestedImage && !pendingImage {
		return reply, turnErr
	}

	// Most images are already present by the time the text turn returns. Give
	// that normal case a short grace period. A provider can also reveal image
	// intent only through its temporary reply (for example a contextual "make it
	// better" follow-up), so pending-image text is enough to enter this path even
	// when the current prompt is not an explicit "generate an image" sentence.
	if waitForCapturedBrowserChatReturnedMedia(ctx, browserChatInitialMediaWait) {
		return completedBrowserGeneratedImageReply(reply), nil
	}

	shouldKeepWaiting := pendingImage
	if turnErr != nil && browserChatImageTurnCanStillBeRendering(turnErr) {
		shouldKeepWaiting = true
	}
	if !shouldKeepWaiting {
		return reply, turnErr
	}

	// Image tools can stay active for many minutes, so this wait is generous --
	// but it is not unbounded. It is entered on a turn that has already failed
	// with a timeout, and an image that is never coming would otherwise hold the
	// agent's whole SMS queue for the life of the process while the sender was
	// texted "still working" every couple of minutes.
	if waitForCapturedBrowserChatReturnedMedia(ctx, browserChatGeneratedImageMaxWait) {
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
