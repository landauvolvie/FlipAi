package main

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

// voiceAgentLink is the rendezvous between the two pages FlipAi hosts during a
// phone call: the Google Voice page and the Codex voice page. The two pages
// carry the sound between themselves over a WebRTC connection that never
// leaves this machine; what passes through here is only the handshake that
// sets that connection up, plus FlipAi's own start/stop instructions.
//
// It is deliberately a dumb pair of queues. The pages own the protocol; Go
// only ferries opaque JSON between two WebViews that have no other way to
// reach each other. Everything is bounded so a stuck page cannot grow the
// queues without limit.
type voiceAgentLink struct {
	mu      sync.Mutex
	toAgent []string
	toCall  []string
}

const (
	// voiceLinkMaxMessages bounds each direction. ICE negotiation is a few
	// dozen messages; anything past this is a page in a loop.
	voiceLinkMaxMessages = 256
	// voiceLinkMaxMessageSize bounds one message. An SDP offer is a few KB.
	voiceLinkMaxMessageSize = 64 << 10
)

func newVoiceAgentLink() *voiceAgentLink { return &voiceAgentLink{} }

func (l *voiceAgentLink) push(q *[]string, msg string) {
	if msg == "" || len(msg) > voiceLinkMaxMessageSize || !json.Valid([]byte(msg)) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(*q) >= voiceLinkMaxMessages {
		*q = (*q)[1:]
	}
	*q = append(*q, msg)
}

func (l *voiceAgentLink) pop(q *[]string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(*q) == 0 {
		return ""
	}
	msg := (*q)[0]
	*q = (*q)[1:]
	return msg
}

// CallSend is the Google Voice page handing a message to the Codex page.
func (l *voiceAgentLink) CallSend(msg string) { l.push(&l.toAgent, msg) }

// CallRecv is the Google Voice page collecting one waiting message, or "".
func (l *voiceAgentLink) CallRecv() string { return l.pop(&l.toCall) }

// AgentSend is the Codex page handing a message to the Google Voice page.
func (l *voiceAgentLink) AgentSend(msg string) { l.push(&l.toCall, msg) }

// AgentRecv is the Codex page collecting one waiting message, or "".
func (l *voiceAgentLink) AgentRecv() string { return l.pop(&l.toAgent) }

// StartVoice tells the Codex page to enter voice mode for a call that has just
// been answered. The command is queued even when the page looks unready --
// it may be mid-reload -- but the error still says what will keep the caller
// waiting in silence, so the call state can show it.
func (l *voiceAgentLink) StartVoice(agent VoiceAgentRuntime) error {
	l.push(&l.toAgent, `{"type":"voice-start"}`)
	if !agent.Running {
		return errors.New("the built-in Codex voice window is not running yet; it starts with the Google Voice window and can be reopened from Settings")
	}
	if !agent.SignedIn {
		return errors.New("the built-in Codex voice window is not signed in to ChatGPT; open Settings → Google Voice calling → Sign in to ChatGPT")
	}
	return nil
}

// StopVoice tells the Codex page the call is over.
func (l *voiceAgentLink) StopVoice() { l.push(&l.toAgent, `{"type":"voice-stop"}`) }

// VoiceAgentRuntime is what the Codex voice page last reported about itself.
// It lives inside VoiceRuntimeState so the settings page can say whether a
// call would actually reach a listening agent before one is missed.
type VoiceAgentRuntime struct {
	Running     bool      `json:"running"`
	SignedIn    bool      `json:"signedIn"`
	VoiceActive bool      `json:"voiceActive"`
	Page        string    `json:"page,omitempty"`
	Controls    string    `json:"controls,omitempty"`
	LastError   string    `json:"lastError,omitempty"`
	ReportedAt  time.Time `json:"reportedAt,omitempty"`
}

// recordCodexVoiceStatus is the Codex voice page's once-a-second report about
// itself, written where the settings page and the call bridge can see it.
func recordCodexVoiceStatus(dataDir, href string, signedIn, voiceActive bool, controls, lastError string) {
	trim := func(v string, max int) string {
		if len(v) > max {
			return v[:max]
		}
		return v
	}
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.CodexVoice = VoiceAgentRuntime{
			Running:     true,
			SignedIn:    signedIn,
			VoiceActive: voiceActive,
			Page:        trim(href, 300),
			Controls:    trim(controls, 2000),
			LastError:   trim(lastError, 500),
			ReportedAt:  time.Now(),
		}
	})
}

// recordCodexVoiceGone marks the Codex voice window as no longer running, so
// the settings page does not wait out the staleness window to say so.
func recordCodexVoiceGone(dataDir, why string) {
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.CodexVoice.Running = false
		s.CodexVoice.VoiceActive = false
		if why != "" {
			s.CodexVoice.LastError = why
		}
		s.CodexVoice.ReportedAt = time.Now()
	})
}

// voiceAgentStale is how long a status report stays believable. The page
// reports about once a second; a report much older than that means the window
// is gone, whatever the last report said.
const voiceAgentStale = 20 * time.Second

// Current returns the report with staleness applied: a page that has stopped
// reporting is not running, whatever it last said.
func (a VoiceAgentRuntime) Current(now time.Time) VoiceAgentRuntime {
	if a.ReportedAt.IsZero() || now.Sub(a.ReportedAt) > voiceAgentStale {
		a.Running = false
		a.VoiceActive = false
	}
	return a
}

// voiceBridgeWarning says what is wrong with the sound path, in the words the
// Settings page shows. Empty means a call answered right now would carry audio
// both ways. It replaces the old virtual-cable warning: there are no cables
// and no device pickers any more, the whole path lives inside FlipAi's two
// browser windows, so what can be wrong is the Codex window, its sign-in, or
// the connection between the two pages.
func voiceBridgeWarning(rt VoiceRuntimeState, now time.Time) string {
	agent := rt.CodexVoice.Current(now)
	switch {
	case !rt.BrowserRunning:
		return ""
	case !agent.Running:
		return "The built-in Codex voice window is not running, so an answered call would have nobody on the line. It starts with the Google Voice window; if it stays away, restart the Google Voice window from Connections."
	case !agent.SignedIn:
		return "The built-in Codex voice window is not signed in to ChatGPT yet. Use \"Sign in to ChatGPT\" below; the caller talks to the ChatGPT voice mode of that account."
	case !strings.EqualFold(rt.BridgeState, "connected"):
		return "The audio link between Google Voice and Codex voice is not connected yet. It connects by itself within a few seconds of both windows loading; if it stays like this, restart the Google Voice window from Connections."
	}
	return ""
}
