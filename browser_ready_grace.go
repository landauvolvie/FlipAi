package main

import (
	"context"
	"time"
)

// browserReadyGrace is how long a provider gets to prove it is signed in before
// FlipAi stops taking its sign-in probe's word for it.
//
// A sign-in probe is a guess about someone else's page, written against a
// layout that changes without notice. When that guess goes stale the worker is
// running, the account is signed in, and every turn is still refused for the
// whole readiness window with "reconnect Muse, then try again" -- advice about a
// connection that was never broken. Muse spent ninety seconds doing exactly
// that while the user had Muse open and working in front of them.
//
// After this grace, a worker that is alive and answering its own health check
// gets to run the turn. The page driver then reports what it actually found,
// which is the truth, instead of a guess standing in for it.
const browserReadyGrace = 20 * time.Second

// acceptBrowserWorker decides whether a turn may proceed.
func acceptBrowserWorker(signedIn, alive, healthOK bool, waited time.Duration) bool {
	if signedIn {
		return true
	}
	return alive && healthOK && waited >= browserReadyGrace
}

// noteBrowserWorkerAccepted records that the turn is going ahead on a worker
// whose sign-in probe never agreed, so the log says why rather than looking
// like the probe passed.
func noteBrowserWorkerAccepted(ctx context.Context, provider string) {
	browserTurnStep(ctx, "warn", provider+" is running and answering, but its sign-in probe never said so; FlipAi is running the turn anyway rather than refusing it")
}

// noteBrowserWorkerWaiting records what is actually missing while a provider is
// not yet ready, so a readiness failure names the stage it stopped at.
func noteBrowserWorkerWaiting(ctx context.Context, provider string, alive, healthOK bool) {
	switch {
	case !alive:
		browserTurnStep(ctx, "warn", provider+" worker is not running yet; waiting for it to start")
	case !healthOK:
		browserTurnStep(ctx, "warn", provider+" worker is running but its control endpoint is not answering yet")
	default:
		browserTurnStep(ctx, "warn", provider+" worker is answering but reports its page as signed out")
	}
}
