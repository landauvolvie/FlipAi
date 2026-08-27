package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// allowCaller is the authorization answer the machine is given in these tests.
// Everything about who may call is decided in decideVoiceCall and tested
// separately; here it is a stub so the lifecycle can be tested on its own.
func allowCaller(agent string, allowed ...string) func(string, string) voiceCallDecision {
	set := map[string]bool{}
	for _, a := range allowed {
		set[a] = true
	}
	return func(caller, label string) voiceCallDecision {
		if set[caller] || set[label] {
			return voiceCallDecision{Agent: agent, Allowed: true}
		}
		return voiceCallDecision{Reason: "not allowed"}
	}
}

func kinds(effects []voiceCallEffect) []voiceEffectKind {
	out := make([]voiceEffectKind, 0, len(effects))
	for _, e := range effects {
		out = append(out, e.Kind)
	}
	return out
}

func hasKind(effects []voiceCallEffect, kind voiceEffectKind) bool {
	for _, e := range effects {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

func ring(caller string) voiceObservation {
	return voiceObservation{Answer: true, Caller: caller}
}

func connected(caller string) voiceObservation {
	return voiceObservation{InCall: true, Caller: caller}
}

// An allowed caller must be answered, and answered immediately: every ring
// spent deciding is a ring closer to voicemail.
func TestAllowedCallerIsAnsweredAtOnce(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()

	effects := m.Observe(ring("+1 555 123 4567"), now)
	if !hasKind(effects, voiceEffectAnswer) {
		t.Fatalf("an allowed caller was not answered: %v", kinds(effects))
	}
	if !hasKind(effects, voiceEffectRouteAudio) {
		t.Fatalf("the desktop app was not routed to the cables while the phone rang: %v", kinds(effects))
	}
	st := m.Status()
	if st.Phase != voicePhaseRinging || st.Agent != "C" {
		t.Fatalf("unexpected state after an authorized ring: %+v", st)
	}
	if st.InCall() {
		t.Fatal("a ringing call must not be reported as a call in progress")
	}
}

// The single most important negative case: a caller nobody allowed must not be
// answered, ever, by any rung of the ladder.
func TestUnauthorizedCallerIsNeverAnswered(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()
	for i := 0; i < 60; i++ {
		if effects := m.Observe(ring("5559998888"), now); len(effects) != 0 {
			t.Fatalf("tick %d acted on an unauthorized ring: %v", i, kinds(effects))
		}
		now = now.Add(voiceAnswerRetry)
	}
	if st := m.Status(); st.Phase != voicePhaseRefused || st.Refused == "" {
		t.Fatalf("an unauthorized ring left no explanation: %+v", st)
	}
}

// Google Voice paints the ringing card before it fills in who is calling. A
// decision made on the blank card must be revisited, or an allowed caller is
// refused for the whole ring.
func TestCallerNamedAfterTheCardAppearsIsStillAnswered(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()

	if effects := m.Observe(voiceObservation{Answer: true}, now); hasKind(effects, voiceEffectAnswer) {
		t.Fatal("an unidentified ring was answered before anyone was identified")
	}
	now = now.Add(300 * time.Millisecond)
	effects := m.Observe(ring("5551234567"), now)
	if !hasKind(effects, voiceEffectAnswer) {
		t.Fatalf("the caller was identified a moment later and still not answered: %v", kinds(effects))
	}
	if st := m.Status(); st.Session != 1 {
		t.Fatalf("naming the caller mid-ring started a second session: %+v", st)
	}
}

// One press is not an answer. A ring that keeps ringing has to keep being
// pressed, walking a ladder of increasingly forceful ways to press it.
func TestAnswerIsRetriedWithAnEscalatingLadder(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	start := time.Now()

	var attempts []int
	for now := start; now.Sub(start) < 5*time.Second; now = now.Add(voiceAnswerRetry) {
		for _, e := range m.Observe(ring("5551234567"), now) {
			if e.Kind == voiceEffectAnswer {
				attempts = append(attempts, e.Attempt)
			}
		}
	}
	if len(attempts) < 4 {
		t.Fatalf("a ring that never connected was pressed only %d times", len(attempts))
	}
	if attempts[0] != 1 || attempts[1] != 2 || attempts[2] != 3 {
		t.Fatalf("the answer ladder did not escalate: %v", attempts)
	}
	if attempts[3] != 1 {
		t.Fatalf("the ladder did not restart after its last rung: %v", attempts)
	}
	for _, a := range attempts {
		if a < 1 || a > voiceAnswerLadder {
			t.Fatalf("attempt %d is not a rung of the ladder", a)
		}
	}
}

// Pressing Answer must not be spread out so far that the caller reaches
// voicemail, nor go on forever once the call is gone.
func TestAnswerStopsAtTheRingDeadline(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	start := time.Now()
	pressed := 0
	within := 0
	for now := start; now.Sub(start) < 90*time.Second; now = now.Add(voiceAnswerRetry) {
		for _, e := range m.Observe(ring("5551234567"), now) {
			if e.Kind != voiceEffectAnswer {
				continue
			}
			pressed++
			if now.Sub(start) < voiceAnswerDeadline {
				within++
			}
		}
	}
	if pressed != within {
		t.Fatalf("Answer was still being pressed past the ring deadline: %d presses, %d within the deadline", pressed, within)
	}
	// Google Voice rings for roughly 25 seconds. Anything much less than one
	// press a second across that window is a caller who can reach voicemail.
	if within < 20 {
		t.Fatalf("an allowed caller was only offered %d answer attempts before voicemail", within)
	}
}

// The whole feature in one test: ring, answer, bridge, hang up, and the next
// call gets its own session.
func TestFullCallCycleAndFreshSessionOnTheNextCall(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()

	m.Observe(ring("5551234567"), now)
	now = now.Add(time.Second)

	effects := m.Observe(connected("5551234567"), now)
	if !hasKind(effects, voiceEffectStartAgentVoice) {
		t.Fatalf("the answered call did not start a desktop voice session: %v", kinds(effects))
	}
	var started voiceCallEffect
	for _, e := range effects {
		if e.Kind == voiceEffectStartAgentVoice {
			started = e
		}
	}
	if started.Agent != "C" || started.Session != 1 {
		t.Fatalf("the voice session was started for the wrong call: %+v", started)
	}
	if st := m.Status(); st.Phase != voicePhaseConnecting {
		t.Fatalf("an answered call should be connecting until voice mode reports back: %+v", st)
	}

	m.AgentVoiceResult(started.Session, nil)
	if st := m.Status(); st.Phase != voicePhaseLive || !st.InCall() {
		t.Fatalf("a started voice session did not make the call live: %+v", st)
	}

	// The caller talks for a while and hangs up.
	now = now.Add(voiceMinCallDuration + time.Second)
	var stop []voiceCallEffect
	for i := 0; i < voiceEndDebounce; i++ {
		stop = m.Observe(voiceObservation{}, now)
		now = now.Add(700 * time.Millisecond)
	}
	if !hasKind(stop, voiceEffectStopAgentVoice) {
		t.Fatalf("hanging up did not end the desktop voice session: %v", kinds(stop))
	}
	if st := m.Status(); st.Phase != voicePhaseIdle || st.Agent != "" || st.Caller != "" || st.InCall() {
		t.Fatalf("the call did not clean up after hang-up: %+v", st)
	}

	// The same person calls back.
	now = now.Add(time.Minute)
	second := m.Observe(ring("5551234567"), now)
	if !hasKind(second, voiceEffectAnswer) {
		t.Fatalf("the second call from the same caller was not answered: %v", kinds(second))
	}
	now = now.Add(time.Second)
	second = m.Observe(connected("5551234567"), now)
	var restarted voiceCallEffect
	for _, e := range second {
		if e.Kind == voiceEffectStartAgentVoice {
			restarted = e
		}
	}
	if restarted.Kind != voiceEffectStartAgentVoice {
		t.Fatalf("the second call did not start a voice session: %v", kinds(second))
	}
	if restarted.Session == started.Session {
		t.Fatalf("the second call reused session %d instead of starting a fresh one", restarted.Session)
	}
}

// A single frame with no controls is Google Voice swapping one card for
// another, not a call that ended.
func TestOneEmptyFrameDoesNotEndACall(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()
	m.Observe(ring("5551234567"), now)
	effects := m.Observe(connected("5551234567"), now.Add(time.Second))
	for _, e := range effects {
		if e.Kind == voiceEffectStartAgentVoice {
			m.AgentVoiceResult(e.Session, nil)
		}
	}

	if got := m.Observe(voiceObservation{}, now.Add(2*time.Second)); len(got) != 0 {
		t.Fatalf("one empty frame ended a live call: %v", kinds(got))
	}
	if st := m.Status(); st.Phase != voicePhaseLive {
		t.Fatalf("one empty frame moved a live call out of live: %+v", st)
	}
	// The call comes back, and the debounce resets with it.
	m.Observe(connected("5551234567"), now.Add(3*time.Second))
	if got := m.Observe(voiceObservation{}, now.Add(4*time.Second)); len(got) != 0 {
		t.Fatalf("the empty-frame count did not reset when the call reappeared: %v", kinds(got))
	}
}

// A source that cannot read the page says nothing at all. Treating "I cannot
// see" as "the call is gone" tore down live calls whenever the control channel
// blinked.
func TestUnreadableObservationsNeverEndACall(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()
	m.Observe(ring("5551234567"), now)
	for _, e := range m.Observe(connected("5551234567"), now.Add(time.Second)) {
		if e.Kind == voiceEffectStartAgentVoice {
			m.AgentVoiceResult(e.Session, nil)
		}
	}
	for i := 0; i < 20; i++ {
		if got := m.Observe(voiceObservation{Unreadable: true}, now.Add(time.Duration(i)*time.Second)); len(got) != 0 {
			t.Fatalf("an unreadable observation acted on a live call: %v", kinds(got))
		}
	}
	if st := m.Status(); st.Phase != voicePhaseLive {
		t.Fatalf("unreadable observations ended a live call: %+v", st)
	}
}

// A call answered by hand still has to be authorized and still has to be
// bridged; and one the user answers for somebody unauthorized must not start
// an agent.
func TestManuallyAnsweredCallsAreAuthorizedToo(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()
	effects := m.Observe(connected("5551234567"), now)
	if !hasKind(effects, voiceEffectStartAgentVoice) {
		t.Fatalf("a hand-answered authorized call was not bridged: %v", kinds(effects))
	}

	other := newVoiceCallMachine(allowCaller("C", "5551234567"))
	effects = other.Observe(connected("5559998888"), now)
	if hasKind(effects, voiceEffectStartAgentVoice) {
		t.Fatalf("a hand-answered call from an unauthorized caller started an agent: %v", kinds(effects))
	}
	st := other.Status()
	if st.Phase != voicePhaseUnbridged || st.Agent != "" {
		t.Fatalf("an unauthorized hand-answered call was not left unbridged: %+v", st)
	}
}

// A voice session that failed to start still has to be torn down, and the call
// must never be described as a working conversation.
func TestFailedVoiceStartIsNotLiveButIsStillTornDown(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()
	m.Observe(ring("5551234567"), now)
	var session int
	for _, e := range m.Observe(connected("5551234567"), now.Add(time.Second)) {
		if e.Kind == voiceEffectStartAgentVoice {
			session = e.Session
		}
	}
	m.AgentVoiceResult(session, errors.New("no Voice control"))
	st := m.Status()
	if st.Phase == voicePhaseLive {
		t.Fatal("a voice session that never started was reported as a live conversation")
	}
	if st.Event != "agent-voice-error" {
		t.Fatalf("a failed voice start left event %q", st.Event)
	}

	var stop []voiceCallEffect
	at := now.Add(voiceMinCallDuration + 2*time.Second)
	for i := 0; i < voiceEndDebounce; i++ {
		stop = m.Observe(voiceObservation{}, at)
		at = at.Add(700 * time.Millisecond)
	}
	if !hasKind(stop, voiceEffectStopAgentVoice) {
		t.Fatalf("a half-started voice session was left running after hang-up: %v", kinds(stop))
	}
}

// Exactly one voice session per call, and exactly one teardown for it.
func TestVoiceSessionIsStartedOnceAndStoppedOnce(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()
	m.Observe(ring("5551234567"), now)

	starts, stops := 0, 0
	count := func(effects []voiceCallEffect) {
		for _, e := range effects {
			switch e.Kind {
			case voiceEffectStartAgentVoice:
				starts++
				m.AgentVoiceResult(e.Session, nil)
			case voiceEffectStopAgentVoice:
				stops++
				m.AgentVoiceStopped(e.Session)
			}
		}
	}
	for i := 0; i < 20; i++ {
		count(m.Observe(connected("5551234567"), now.Add(time.Duration(i)*time.Second)))
	}
	// Past the window a fresh call is protected in, and then long enough for
	// the machine to be sure the call is gone.
	for i := 0; i < 20; i++ {
		count(m.Observe(voiceObservation{}, now.Add(voiceMinCallDuration+time.Duration(20+i)*time.Second)))
	}
	if starts != 1 {
		t.Fatalf("one call started %d voice sessions", starts)
	}
	if stops != 1 {
		t.Fatalf("one call ended with %d teardowns", stops)
	}
}

// A forced end -- quit, the window closing, calling switched off -- must leave
// nothing listening in the desktop app.
func TestForcedEndTearsDownTheAgent(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()
	m.Observe(ring("5551234567"), now)
	for _, e := range m.Observe(connected("5551234567"), now.Add(time.Second)) {
		if e.Kind == voiceEffectStartAgentVoice {
			m.AgentVoiceResult(e.Session, nil)
		}
	}
	if !hasKind(m.End(now.Add(2*time.Second)), voiceEffectStopAgentVoice) {
		t.Fatal("a forced end left the desktop app in voice mode")
	}
	if got := m.End(now.Add(3 * time.Second)); len(got) != 0 {
		t.Fatalf("a second forced end emitted more teardowns: %v", kinds(got))
	}
	if st := m.Status(); st.Phase != voicePhaseIdle {
		t.Fatalf("a forced end did not return to idle: %+v", st)
	}
}

// Call waiting: a second ring while a call is up must not steal the agent or
// answer over the conversation in progress.
func TestSecondRingDuringALiveCallIsIgnored(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567", "5557654321"))
	now := time.Now()
	m.Observe(ring("5551234567"), now)
	for _, e := range m.Observe(connected("5551234567"), now.Add(time.Second)) {
		if e.Kind == voiceEffectStartAgentVoice {
			m.AgentVoiceResult(e.Session, nil)
		}
	}
	got := m.Observe(voiceObservation{Answer: true, InCall: true, Caller: "5557654321"}, now.Add(2*time.Second))
	if len(got) != 0 {
		t.Fatalf("a waiting call acted on a live conversation: %v", kinds(got))
	}
	if st := m.Status(); st.Caller != "5551234567" {
		t.Fatalf("a waiting call replaced the live caller: %+v", st)
	}
}

// The status note is what the user reads. It must never say a conversation is
// working when no voice session is behind it.
func TestStatusNoteNeverClaimsAWorkingCallWithoutAVoiceSession(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()
	m.Observe(ring("5551234567"), now)
	var session int
	for _, e := range m.Observe(connected("5551234567"), now.Add(time.Second)) {
		if e.Kind == voiceEffectStartAgentVoice {
			session = e.Session
		}
	}
	m.AgentVoiceResult(session, errors.New("Voice control not found"))
	note := voiceCallStatusNote(m.Status())
	if note == "" {
		t.Fatal("a call whose voice session failed said nothing at all")
	}
	if !strings.Contains(note, "not entered voice mode") {
		t.Fatalf("a failed voice session was described as %q", note)
	}
}

// The worst failure this feature can have is tearing the agent's voice session
// down in the middle of a real conversation, because the caller is then talking
// to nothing. A call is recognized by the controls Google Voice draws, and
// Google can rename them; the first few seconds of a connected call are
// therefore not allowed to end it.
func TestAFreshCallCannotBeEndedByBlindness(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()
	m.Observe(ring("5551234567"), now)
	for _, e := range m.Observe(connected("5551234567"), now.Add(time.Second)) {
		if e.Kind == voiceEffectStartAgentVoice {
			m.AgentVoiceResult(e.Session, nil)
		}
	}
	connectedAt := now.Add(time.Second)

	// Nothing at all is seen for the whole grace period.
	at := connectedAt
	for at.Sub(connectedAt) < voiceMinCallDuration {
		if got := m.Observe(voiceObservation{}, at); len(got) != 0 {
			t.Fatalf("a conversation %v old was torn down: %v", at.Sub(connectedAt), kinds(got))
		}
		at = at.Add(600 * time.Millisecond)
	}
	if st := m.Status(); !st.InCall() {
		t.Fatalf("the call was dropped inside the protected window: %+v", st)
	}

	// Past it, a call that really is gone still ends.
	var stop []voiceCallEffect
	for i := 0; i < voiceEndDebounce+1; i++ {
		stop = append(stop, m.Observe(voiceObservation{}, at)...)
		at = at.Add(600 * time.Millisecond)
	}
	if !hasKind(stop, voiceEffectStopAgentVoice) {
		t.Fatalf("a call that really ended was never torn down: %v", kinds(stop))
	}
}

// The protection is for a connected call only. A ring nobody answered must not
// leave the machine stuck thinking a call is in progress.
func TestAnUnansweredRingStillClearsPromptly(t *testing.T) {
	m := newVoiceCallMachine(allowCaller("C", "5551234567"))
	now := time.Now()
	m.Observe(ring("5551234567"), now)
	for i := 0; i < voiceEndDebounce; i++ {
		m.Observe(voiceObservation{}, now.Add(time.Duration(i)*600*time.Millisecond))
	}
	if st := m.Status(); st.Phase != voicePhaseIdle {
		t.Fatalf("a ring that stopped left the machine at %+v", st)
	}
}
