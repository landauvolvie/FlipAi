package main

import (
	"testing"
	"time"
)

// "Answered -- starting voice" with nothing beside it is a dead end: it does
// not say whether FlipAi could read the desktop app, what the app offered, or
// whether a voice control was found and pressed. That observation has to reach
// the desktop UI, which reads the runtime snapshot.
func TestTheDesktopVoiceObservationReachesTheSnapshot(t *testing.T) {
	dir := t.TempDir()
	mutateVoiceRuntime(dir, func(s *VoiceRuntimeState) {
		s.AgentVoiceReadable = true
		s.AgentVoiceApp = "ChatGPT"
		s.AgentVoiceControls = []string{"New chat", "Send", "Settings"}
		s.AgentVoiceStart = ""
		s.AgentVoiceResult = "not-found"
		s.AgentVoiceAt = time.Now()
		// The token that drives the signed-in session must never leak to a page.
		s.ControlToken = "secret"
	})

	snap := voiceSnapshot(dir, nil)
	rt := snap.Runtime
	if !rt.AgentVoiceReadable || rt.AgentVoiceApp != "ChatGPT" {
		t.Fatalf("the accessibility observation did not survive to the snapshot: %+v", rt)
	}
	if len(rt.AgentVoiceControls) != 3 || rt.AgentVoiceResult != "not-found" {
		t.Errorf("the controls the app offered were not carried through: %+v", rt.AgentVoiceControls)
	}
	if rt.AgentVoiceStart != "" {
		t.Errorf("a voice control was reported when none was matched: %q", rt.AgentVoiceStart)
	}
	if rt.ControlToken != "" {
		t.Error("the control token leaked into the page snapshot")
	}
}
