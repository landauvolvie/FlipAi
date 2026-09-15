package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// A browser turn crosses three processes: the bridge, the provider's worker,
// and the page driver running inside the WebView. Until now the only thing that
// survived that trip was a single sentence written when the turn had already
// failed, so "it did not work" could mean the worker was never reachable, the
// prompt was never typed, the model never answered, or the answer was found and
// then discarded -- and telling those apart meant guessing.
//
// A step sink rides on the turn's context. Each stage reports what it did, and
// the bridge writes it to the activity log against the same sender and message
// as the turn itself.
//
// Steps are metadata, exactly like the rest of the activity log: stage names,
// timings, counts and lengths. The prompt and the reply are never recorded.
type browserTurnStepSink func(level, step string)

type browserTurnStepKey struct{}

func withBrowserTurnSteps(ctx context.Context, sink browserTurnStepSink) context.Context {
	if ctx == nil || sink == nil {
		return ctx
	}
	return context.WithValue(ctx, browserTurnStepKey{}, sink)
}

// browserTurnStep records one step of the turn. It is safe on a context that
// carries no sink, so the SMS layers can report unconditionally.
func browserTurnStep(ctx context.Context, level, step string) {
	if ctx == nil {
		return
	}
	sink, _ := ctx.Value(browserTurnStepKey{}).(browserTurnStepSink)
	if sink == nil {
		return
	}
	step = strings.TrimSpace(step)
	if step == "" {
		return
	}
	sink(level, step)
}

// browserPageTrace formats the page driver's own step trace for the log. The
// driver is the only place that can say whether the prompt was typed, whether
// the page accepted it, and whether anything that looked like an answer ever
// appeared, so its trace is the most useful line in the whole turn.
func browserPageTrace(provider, trace string) string {
	trace = strings.TrimSpace(trace)
	if trace == "" {
		return provider + " page driver reported no steps"
	}
	return provider + " page steps: " + truncate(trace, 400)
}

// The reporting helpers below are what each provider's SMS layer calls. They
// exist so all six report the same stages in the same words: which stage a turn
// died at is only useful if it means the same thing for every agent.

func browserTurnReady(ctx context.Context, provider string, err error) {
	if err != nil {
		browserTurnStep(ctx, "error", provider+" worker was not ready: "+truncate(err.Error(), 200))
		return
	}
	browserTurnStep(ctx, "info", provider+" worker is ready; sending the prompt to the page")
}

func browserTurnDisconnected(ctx context.Context, provider string) {
	browserTurnStep(ctx, "error", provider+" is disconnected in FlipAi, so the message was never sent to the page")
}

// browserTurnAnswered reports the worker's reply. The trace is the page
// driver's own account of the turn and is the line that says whether the
// message reached the model at all.
func browserTurnAnswered(ctx context.Context, provider string, code int, took time.Duration, ok bool, trace, detail, reply string) {
	browserTurnStep(ctx, "info", fmt.Sprintf("%s worker answered HTTP %d after %s", provider, code, took.Round(time.Millisecond)))
	browserTurnStep(ctx, "info", browserPageTrace(provider, trace))
	switch {
	case ok:
		browserTurnStep(ctx, "success", fmt.Sprintf("%s produced a reply of %d characters", provider, len(strings.TrimSpace(reply))))
	case strings.TrimSpace(detail) != "":
		browserTurnStep(ctx, "warn", provider+" did not finish in the page: "+truncate(detail, 200))
	}
}

func browserTurnRequestFailed(ctx context.Context, provider string, took time.Duration, err error) {
	browserTurnStep(ctx, "error", fmt.Sprintf("%s worker request failed after %s: %s", provider, took.Round(time.Millisecond), truncate(err.Error(), 200)))
}

func browserTurnWatching(ctx context.Context, provider string) {
	browserTurnStep(ctx, "info", provider+" reached its page checkpoint; FlipAi is watching the page for the finished answer")
}

func browserTurnWatchDone(ctx context.Context, provider string, reply string, err error) {
	if err != nil {
		browserTurnStep(ctx, "error", provider+" never finished in the page: "+truncate(err.Error(), 200))
		return
	}
	browserTurnStep(ctx, "success", fmt.Sprintf("%s finished in the page with a reply of %d characters", provider, len(strings.TrimSpace(reply))))
}
