package main

import (
	"fmt"
	"sync"
	"time"
)

// A phone call has exactly one life story, and this is where it is told.
//
// Before this existed the story was spread across the page script, an Edge
// control loop, a PowerShell answer guard and a second PowerShell activation
// guard, each with its own timers and its own idea of what was happening. They
// answered the same ring twice, started the desktop app's voice mode twice, and
// -- worse -- each could declare a call "connected" on its own, which is how a
// status could read connected while nobody could hear anything.
//
// The machine below is the only thing allowed to decide what happens to a call.
// Everything that can see the Google Voice page reports what it sees; the
// machine turns a stream of those observations into a small list of effects to
// carry out. It holds no Windows types and starts no processes, so the whole
// ring -> answer -> bridge -> hang up -> next call cycle is exercised in tests
// without a phone line, a browser, or Windows.

type voiceCallPhase string

const (
	// Nothing is happening.
	voicePhaseIdle voiceCallPhase = "idle"
	// An authorized caller is ringing and FlipAi is pressing Answer.
	voicePhaseRinging voiceCallPhase = "ringing"
	// Somebody is ringing who is not allowed to use the agent. FlipAi does not
	// touch the call: Google Voice takes it to voicemail as it normally would.
	voicePhaseRefused voiceCallPhase = "refused"
	// The call is up and the desktop app's voice mode is being started.
	voicePhaseConnecting voiceCallPhase = "connecting"
	// The call is up and the agent is in voice mode: a real conversation.
	voicePhaseLive voiceCallPhase = "live"
	// The call is up but the caller is not authorized, so it was never bridged.
	// This only happens when a person answers by hand.
	voicePhaseUnbridged voiceCallPhase = "unbridged"
)

const (
	// voiceAnswerRetry is the gap between attempts to press Answer. Google
	// Voice re-renders the ringing card as it animates in, so the first press
	// can land on an element that is replaced a moment later.
	voiceAnswerRetry = 700 * time.Millisecond

	// voiceAnswerDeadline is how long an authorized ring keeps being answered.
	// Google Voice gives up and takes a call to voicemail after roughly 25
	// seconds, so FlipAi keeps trying for slightly longer than the whole ring
	// rather than pressing once and hoping. An allowed caller reaching
	// voicemail is the failure this exists to prevent.
	voiceAnswerDeadline = 30 * time.Second

	// voiceAnswerLadder is how many distinct ways of pressing Answer there are.
	// Attempt 1 is the page's own click, 2 is a real mouse event delivered to
	// the page, 3 is the Windows accessibility Invoke. Beyond that the ladder
	// repeats from the top until the deadline.
	voiceAnswerLadder = 3

	// voiceEndDebounce is how many consecutive observations with no call
	// controls at all end a call. Google Voice briefly renders neither an
	// Answer nor a hang-up control while it swaps one card for another, and
	// tearing the agent down on that single frame ended calls that were still
	// connected.
	voiceEndDebounce = 3

	// voiceMinCallDuration is how long a connected call is protected from the
	// polled observations ending it.
	//
	// A call is recognized by the controls Google Voice draws, and Google is
	// free to change them. If the hang-up control stops being recognized, every
	// observation reads as "no call" and the desktop voice session is torn down
	// seconds into a real conversation -- the worst failure this feature has,
	// because the caller is left talking to nothing. No real call is over this
	// fast, so the first few seconds are simply not allowed to end one. A
	// caller who really did hang up immediately is torn down at the end of it
	// instead, which nobody notices.
	voiceMinCallDuration = 6 * time.Second
)

// voiceObservation is one look at the Google Voice page.
type voiceObservation struct {
	// Answer is true while a control that answers the call is on screen.
	Answer bool
	// InCall is true while a control that ends the call is on screen, which is
	// the only trustworthy sign that a call is actually up. It is the only
	// thing that may start one.
	InCall bool
	// Sustain is a weaker sign that a call is still up -- the controls a call
	// in progress offers, whatever the one that ends it is called. Google
	// Voice's ordinary page offers those too, so this may only keep a call
	// FlipAi already knows about from being declared over. Letting it start a
	// call made FlipAi believe it was permanently in one, and every real ring
	// after that was ignored as call waiting.
	Sustain bool
	// Caller and Label are who Google Voice says is calling. Either may be
	// empty, and both may arrive a moment after the ring itself.
	Caller string
	Label  string
	// Unreadable marks an observation from a source that could not read the
	// page at all -- a closed DevTools socket, a page mid-navigation. It can
	// never end a call, because "I cannot see" is not "nothing is there".
	Unreadable bool
}

type voiceEffectKind int

const (
	// voiceEffectAnswer presses Answer. Attempt says which rung of the ladder.
	voiceEffectAnswer voiceEffectKind = iota + 1
	// voiceEffectRouteAudio points the desktop app at the virtual cables. It is
	// emitted before the voice session starts so the microphone stream that
	// voice mode opens is already the cable.
	voiceEffectRouteAudio
	// voiceEffectStartAgentVoice starts a brand new voice session in the
	// desktop app.
	voiceEffectStartAgentVoice
	// voiceEffectStopAgentVoice ends the voice session started for this call.
	voiceEffectStopAgentVoice
)

// voiceAnswerRungName names one rung of the answer ladder, for the record of
// what FlipAi tried on a call that was not answered.
func voiceAnswerRungName(attempt int) string {
	switch attempt {
	case 1:
		return "the page's own click"
	case 2:
		return "a real pointer press"
	case 3:
		return "Windows accessibility"
	}
	return "unknown"
}

func (k voiceEffectKind) String() string {
	switch k {
	case voiceEffectAnswer:
		return "answer"
	case voiceEffectRouteAudio:
		return "route-audio"
	case voiceEffectStartAgentVoice:
		return "start-agent-voice"
	case voiceEffectStopAgentVoice:
		return "stop-agent-voice"
	}
	return "none"
}

// voiceCallEffect is one thing to do. It carries the session it belongs to so a
// late effect from a call that has already ended can be dropped rather than
// starting a voice session for nobody.
type voiceCallEffect struct {
	Kind    voiceEffectKind
	Session int
	Agent   string
	Caller  string
	Label   string
	Attempt int
}

// voiceCallStatus is the machine's whole state, for the status file and the
// desktop UI. Nothing else may claim a call is connected.
type voiceCallStatus struct {
	Phase   voiceCallPhase
	Session int
	Agent   string
	Caller  string
	Label   string
	// Refused is why an unauthorized caller was left to ring.
	Refused string
	// Event is a short machine-readable name for what last happened, kept for
	// the activity log and the status page.
	Event string
}

// InCall reports whether a real, bridged conversation is up. A call being
// connected is exactly "the agent's voice session is running for it", which is
// why nothing outside this type is allowed to answer the question.
func (s voiceCallStatus) InCall() bool {
	return s.Phase == voicePhaseConnecting || s.Phase == voicePhaseLive || s.Phase == voicePhaseUnbridged
}

// voiceCallMachine turns observations into effects.
type voiceCallMachine struct {
	mu sync.Mutex

	// decide answers "may this caller use an agent, and which one". It is the
	// authorization boundary and is consulted on every fresh identification,
	// never cached across calls.
	decide func(caller, label string) voiceCallDecision

	phase   voiceCallPhase
	session int
	agent   string
	caller  string
	label   string
	refused string
	event   string

	ringStart    time.Time
	connectedAt  time.Time
	lastAnswerAt time.Time
	attempts     int
	quiet        int

	// agentRunning records that a voice session was actually started, so
	// exactly one stop is emitted for it and never a stop for a session that
	// was never started.
	agentRunning bool
	// startIssued keeps a second start from being emitted while the first is
	// still being carried out.
	startIssued bool
}

func newVoiceCallMachine(decide func(caller, label string) voiceCallDecision) *voiceCallMachine {
	if decide == nil {
		decide = func(string, string) voiceCallDecision { return voiceCallDecision{} }
	}
	return &voiceCallMachine{decide: decide, phase: voicePhaseIdle}
}

// Status is the current state of the call.
func (m *voiceCallMachine) Status() voiceCallStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status()
}

func (m *voiceCallMachine) status() voiceCallStatus {
	return voiceCallStatus{
		Phase:   m.phase,
		Session: m.session,
		Agent:   m.agent,
		Caller:  m.caller,
		Label:   m.label,
		Refused: m.refused,
		Event:   m.event,
	}
}

// identified reports whether an observation says anything about who is calling.
func (o voiceObservation) identified() bool { return o.Caller != "" || o.Label != "" }

// Observe feeds one look at the page in and returns what to do about it.
func (m *voiceCallMachine) Observe(obs voiceObservation, now time.Time) []voiceCallEffect {
	m.mu.Lock()
	defer m.mu.Unlock()

	if obs.Unreadable {
		// A source that cannot read the page contributes nothing. In
		// particular it must not count towards ending a live call.
		return nil
	}

	obs.Caller = normalizeUSPhone(obs.Caller)
	obs.Label = normalizeCallerLabel(obs.Label)

	if obs.InCall {
		return m.onCallUp(obs, now)
	}
	// No control that ends a call is on screen. Whatever else is true, that has
	// to be accounted for before a ring is considered -- otherwise a call FlipAi
	// wrongly believes is up can never be cleared, because a ringing page never
	// reaches the quiet path at all. That is exactly how one bad reading stopped
	// every later call from being answered.
	effects := m.onQuiet(obs, now)
	if obs.Answer {
		effects = append(effects, m.onRinging(obs, now)...)
	}
	return effects
}

// callUp reports whether a phase means a call is connected.
func callUp(p voiceCallPhase) bool {
	return p == voicePhaseConnecting || p == voicePhaseLive || p == voicePhaseUnbridged
}

// onRinging handles a ringing call: a call that can still be answered.
func (m *voiceCallMachine) onRinging(obs voiceObservation, now time.Time) []voiceCallEffect {
	m.quiet = 0

	// A ring that arrives while a call is genuinely up is Google Voice's call
	// waiting, and it reaches onCallUp rather than here. Anything left is a
	// ring on a machine that believes a call is up and has just been corrected
	// by onQuiet; if the correction has not happened yet, wait for it.
	if callUp(m.phase) {
		return nil
	}

	// A new ring, or the same ring now naming somebody it could not name
	// before. Google Voice paints the card first and fills the caller in a
	// frame or two later, so a decision made on an empty card is always
	// revisited once there is a name or a number to decide about.
	fresh := m.phase == voicePhaseIdle
	renamed := !fresh && obs.identified() && (obs.Caller != m.caller || obs.Label != m.label)
	if fresh || renamed {
		if fresh {
			m.session++
			m.ringStart = now
			m.attempts = 0
			m.lastAnswerAt = time.Time{}
		}
		m.caller, m.label = obs.Caller, obs.Label
		d := m.decide(obs.Caller, obs.Label)
		if !d.Allowed {
			m.phase = voicePhaseRefused
			m.agent = ""
			m.refused = d.Reason
			m.event = "blocked-call-ringing"
			return nil
		}
		m.phase = voicePhaseRinging
		m.agent = d.Agent
		m.refused = ""
		m.event = "authorized-call-ringing"
		// The desktop app is pointed at the cables while the phone is still
		// ringing, so nothing has to happen between answering and talking.
		effects := []voiceCallEffect{m.effect(voiceEffectRouteAudio)}
		return append(effects, m.answerEffect(now))
	}

	if m.phase != voicePhaseRinging {
		// A refused caller keeps ringing. FlipAi does nothing at all, which is
		// what sends them to voicemail exactly as if FlipAi were not installed.
		return nil
	}

	// Keep pressing. An allowed caller reaching voicemail because one click was
	// swallowed is the failure this loop exists to prevent.
	if now.Sub(m.ringStart) >= voiceAnswerDeadline {
		return nil
	}
	if now.Sub(m.lastAnswerAt) < voiceAnswerRetry {
		return nil
	}
	return []voiceCallEffect{m.answerEffect(now)}
}

// onCallUp handles a call that is connected, however it came to be answered.
func (m *voiceCallMachine) onCallUp(obs voiceObservation, now time.Time) []voiceCallEffect {
	m.quiet = 0

	// Somebody answered by hand while the page had not yet named the caller.
	// Take whatever identity the live call reports.
	if obs.identified() && (m.caller == "" && m.label == "") {
		m.caller, m.label = obs.Caller, obs.Label
	}

	if callUp(m.phase) {
		return nil
	}

	// A call answered from the phone itself, or by the user in the panel,
	// never rang through this machine. Decide about it now: who may talk to
	// the agent is the same answer either way.
	if m.phase == voicePhaseIdle {
		m.session++
		m.ringStart = now
		m.caller, m.label = obs.Caller, obs.Label
	}
	d := m.decide(m.caller, m.label)
	if !d.Allowed {
		m.phase = voicePhaseUnbridged
		m.connectedAt = now
		m.agent = ""
		m.refused = d.Reason
		m.event = "unbridged-call"
		return nil
	}
	m.phase = voicePhaseConnecting
	m.connectedAt = now
	m.agent = d.Agent
	m.refused = ""
	m.event = "call-answered"
	m.startIssued = true
	return []voiceCallEffect{
		m.effect(voiceEffectRouteAudio),
		m.effect(voiceEffectStartAgentVoice),
	}
}

// onQuiet handles an observation with no control that ends a call on it.
func (m *voiceCallMachine) onQuiet(obs voiceObservation, now time.Time) []voiceCallEffect {
	if m.phase == voicePhaseIdle {
		m.quiet = 0
		return nil
	}

	// A call that can still be answered is a call that is not up. If FlipAi
	// thinks one is, it is wrong, and it has to stop thinking so now rather
	// than in several seconds' time: Google Voice is offering to answer, and
	// every second spent clearing a stale belief is a second closer to
	// voicemail. Call waiting does not come through here -- a second call
	// ringing during a live one still shows the control that ends the live
	// one, so that observation goes to onCallUp instead.
	if obs.Answer && callUp(m.phase) {
		return m.end(now)
	}

	// A ring that is still ringing has not ended.
	if obs.Answer {
		m.quiet = 0
		return nil
	}

	// A connected call still showing the controls a call offers has not ended
	// either, whatever Google has renamed the hang-up control to.
	if obs.Sustain && callUp(m.phase) {
		m.quiet = 0
		return nil
	}

	m.quiet++
	if m.quiet < voiceEndDebounce {
		return nil
	}
	if callUp(m.phase) && (m.connectedAt.IsZero() || now.Sub(m.connectedAt) < voiceMinCallDuration) {
		return nil
	}
	return m.end(now)
}

// End forces the call down: the window is closing, calling was switched off, or
// FlipAi is quitting. Whatever the page says, nothing may be left listening.
func (m *voiceCallMachine) End(now time.Time) []voiceCallEffect {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.phase == voicePhaseIdle && !m.agentRunning {
		return nil
	}
	return m.end(now)
}

func (m *voiceCallMachine) end(now time.Time) []voiceCallEffect {
	var effects []voiceCallEffect
	if m.agentRunning || m.startIssued {
		// The stop is emitted for the session that started, and carries its
		// agent, so a teardown can never be sent to the wrong desktop app.
		effects = append(effects, voiceCallEffect{
			Kind:    voiceEffectStopAgentVoice,
			Session: m.session,
			Agent:   m.agent,
			Caller:  m.caller,
			Label:   m.label,
		})
	}
	m.phase = voicePhaseIdle
	m.agent = ""
	m.caller = ""
	m.label = ""
	m.refused = ""
	m.event = "call-ended"
	m.attempts = 0
	m.quiet = 0
	m.agentRunning = false
	m.startIssued = false
	m.ringStart = time.Time{}
	m.connectedAt = time.Time{}
	m.lastAnswerAt = time.Time{}
	return effects
}

// AgentVoiceResult reports what happened when a start effect was carried out.
// A failure keeps the call in Connecting rather than promoting it to Live, so a
// call whose agent never entered voice mode is never described as a working
// conversation.
func (m *voiceCallMachine) AgentVoiceResult(session int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session != m.session || m.phase != voicePhaseConnecting {
		return
	}
	m.startIssued = false
	if err != nil {
		m.agentRunning = true // half-started: it still has to be torn down
		m.event = "agent-voice-error"
		return
	}
	m.agentRunning = true
	m.phase = voicePhaseLive
	m.event = "call-bridged"
}

// AgentVoiceStopped records that a teardown finished, so a retry is not queued
// for a session that is already gone.
func (m *voiceCallMachine) AgentVoiceStopped(session int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if session == m.session {
		m.agentRunning = false
	}
}

func (m *voiceCallMachine) answerEffect(now time.Time) voiceCallEffect {
	m.lastAnswerAt = now
	m.attempts++
	e := m.effect(voiceEffectAnswer)
	// The ladder repeats rather than stopping, because a call that is still
	// ringing has not been answered by any of them yet.
	e.Attempt = ((m.attempts - 1) % voiceAnswerLadder) + 1
	return e
}

func (m *voiceCallMachine) effect(kind voiceEffectKind) voiceCallEffect {
	return voiceCallEffect{
		Kind:    kind,
		Session: m.session,
		Agent:   m.agent,
		Caller:  m.caller,
		Label:   m.label,
	}
}

// voiceCallStatusNote is the one-line description of a call state that the
// desktop UI shows. It exists so "connected" is never printed for a call that
// has no working agent voice session behind it.
func voiceCallStatusNote(s voiceCallStatus) string {
	switch s.Phase {
	case voicePhaseRinging:
		who := callerDescription(s.Caller, s.Label)
		return fmt.Sprintf("Answering a call from %s for %s.", who, agentDisplayName(s.Agent))
	case voicePhaseRefused:
		return "A call is ringing that is not allowed to use an agent, so FlipAi is leaving it alone. " + s.Refused
	case voicePhaseConnecting:
		if s.Event == "agent-voice-error" {
			return fmt.Sprintf("The call is connected, but %s has not entered voice mode yet.", agentDisplayName(s.Agent))
		}
		return fmt.Sprintf("Call connected; starting a voice session in %s.", agentDisplayName(s.Agent))
	case voicePhaseLive:
		return fmt.Sprintf("%s is on the call with %s.", agentDisplayName(s.Agent), callerDescription(s.Caller, s.Label))
	case voicePhaseUnbridged:
		return "The call was answered by hand, but the caller is not allowed to use an agent. " + s.Refused
	}
	return ""
}

func callerDescription(caller, label string) string {
	switch {
	case caller != "" && label != "":
		return label + " (" + formatUSPhone(caller) + ")"
	case caller != "":
		return formatUSPhone(caller)
	case label != "":
		return label
	}
	return "an unidentified caller"
}
