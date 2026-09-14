package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

// browserChatTurnRequestBudget is how long the FlipAi host waits for a browser
// worker to answer one /chat request.
//
// It has to outlast everything the worker itself may spend on that request,
// because the worker cannot answer sooner than its own work takes:
//
//	up to  25s  waiting for the page to report itself signed in
//	up to  95s  the awaited page turn (its own 90s checkpoint plus slack)
//	up to  20s  the returned-media scan that runs after the turn
//	-----------
//	      140s
//
// The budget was 100 seconds, so a turn that used its full page checkpoint was
// abandoned by the host while the worker was still finishing -- reported as
// `Post "http://127.0.0.1:PORT/chat": context deadline exceeded`, with the
// model's answer sitting completed in the browser.
const browserChatTurnRequestBudget = 180 * time.Second

// browserChatTurnRequestTimedOut reports whether a control-request error is the
// host giving up on its own deadline rather than the worker reporting a real
// failure. The model is usually still answering when this happens, so the
// caller should collect the answer through the long-turn state instead of
// failing the turn.
func browserChatTurnRequestTimedOut(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "client.timeout exceeded") ||
		strings.Contains(s, "timeout awaiting response headers")
}
