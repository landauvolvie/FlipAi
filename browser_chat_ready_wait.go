package main

import "time"

// browserChatTurnReadyWait is how long an SMS turn waits for a connected
// browser agent's background browser to be signed in and answering.
//
// It used to be 15 seconds, which is shorter than the restore itself. Every
// browser agent reloads its saved session when FlipAi starts, all of them at
// once, and that regularly takes over a minute on a cold machine. A queued text
// dispatched in the first seconds after a restart therefore met a browser that
// was still coming up and burned the turn -- and with the per-agent queue, one
// burned turn delays every text behind it.
//
// This only ever applies to an agent the user has connected: a disconnected one
// is refused before this wait, from its saved runtime state, and still answers
// immediately.
const browserChatTurnReadyWait = 90 * time.Second
