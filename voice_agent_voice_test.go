package main

import (
	"strings"
	"testing"
)

// The desktop app is driven through one accessibility script whose output is a
// small report. Everything FlipAi decides about voice mode is decided from that
// report, so the report is what these tests are about: it is the only part of
// driving a Windows desktop app that can be checked without one.

func TestVoiceReportSaysWhetherVoiceModeIsRunning(t *testing.T) {
	s := parseAgentVoiceReport("found=1\ncontrol=New chat\ncontrol=End voice mode\nactive=1\nstart=\nend=End voice mode\nresult=already-active\n")
	if !s.Found {
		t.Fatal("a readable window was reported as unreadable")
	}
	if !s.Active {
		t.Fatal("a window offering End voice mode was not reported as being in voice mode")
	}
	if s.EndControl != "End voice mode" {
		t.Errorf("end control = %q", s.EndControl)
	}
	if s.Result != "already-active" {
		t.Errorf("result = %q", s.Result)
	}

	idle := parseAgentVoiceReport("found=1\ncontrol=Start voice mode\nactive=0\nstart=Start voice mode\nend=\nresult=read\n")
	if idle.Active {
		t.Fatal("a window with no way to end voice was reported as being in voice mode")
	}
	if idle.StartControl != "Start voice mode" {
		t.Errorf("start control = %q", idle.StartControl)
	}
}

// A window that could not be read at all is not the same as a window with no
// Voice control, and the difference decides whether FlipAi retries or explains.
func TestVoiceReportDistinguishesUnreadableFromEmpty(t *testing.T) {
	missing := parseAgentVoiceReport("found=0\nresult=no-window\n")
	if missing.Found {
		t.Fatal("a window that could not be opened was reported as read")
	}
	empty := parseAgentVoiceReport("found=1\nactive=0\nstart=\nend=\nresult=not-found\n")
	if !empty.Found || empty.StartControl != "" {
		t.Fatalf("an empty window was misread: %+v", empty)
	}
}

// Rubbish on the pipe -- a PowerShell warning, a progress bar, a stack trace --
// must not be mistaken for a report.
func TestVoiceReportIgnoresNoise(t *testing.T) {
	s := parseAgentVoiceReport("WARNING: something\r\nfound=1\r\nrandom line without an equals\r\nactive=1\r\n")
	if !s.Found || !s.Active {
		t.Fatalf("noise around a real report changed it: %+v", s)
	}
	if len(s.Controls) != 0 {
		t.Errorf("noise was recorded as controls: %v", s.Controls)
	}
}

// The list of controls is bounded: it goes into a status message a person
// reads, not into a log.
func TestVoiceReportBoundsTheControlList(t *testing.T) {
	var b strings.Builder
	b.WriteString("found=1\n")
	for i := 0; i < 200; i++ {
		b.WriteString("control=Button\n")
	}
	if got := len(parseAgentVoiceReport(b.String()).Controls); got > 24 {
		t.Fatalf("%d controls were kept for a status message", got)
	}
}

// A voice session that did not start has to say which of the several possible
// reasons it was, because they need different things from the user.
func TestVoiceStartFailuresAreSpecific(t *testing.T) {
	cases := []struct {
		name  string
		state agentVoiceState
		want  string
	}{
		{"unreadable", agentVoiceState{}, "Windows accessibility"},
		{"still starting", agentVoiceState{Found: true}, "may still be starting up"},
		{"no voice control", agentVoiceState{Found: true, Controls: []string{"New chat", "Settings"}}, "could not find the voice control"},
		{"activation refused", agentVoiceState{Found: true, StartControl: "Voice", Result: "invoke-failed"}, "verified activation methods"},
		{"activated but nothing happened", agentVoiceState{Found: true, StartControl: "Voice", Result: "pointer-sent"}, "did not enter voice mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := agentVoiceStartFailure("Codex", tc.state)
			if err == nil {
				t.Fatal("a failed voice session reported success")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%q does not explain %q", err.Error(), tc.want)
			}
			if !strings.Contains(err.Error(), "Codex") {
				t.Errorf("the message does not name the app: %q", err.Error())
			}
		})
	}
	if err := agentVoiceStartFailure("Codex", agentVoiceState{Found: true, Active: true}); err != nil {
		t.Fatalf("a running voice session was reported as a failure: %v", err)
	}
}

// The script is built by substitution, and the only thing substituted into it
// besides a window handle is one of the fixed action names below.
func TestVoiceScriptOnlyAcceptsKnownActions(t *testing.T) {
	for _, action := range []string{"state", "start", "start-invoke", "start-keyboard", "start-legacy", "start-pointer", "stop"} {
		script, err := voiceAgentUIAScript(0x1234, action)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if !strings.Contains(script, "'"+action+"'") {
			t.Errorf("%s was not substituted into the script", action)
		}
		if strings.Contains(script, "__ACTION__") || strings.Contains(script, "__HWND__") {
			t.Errorf("%s left a placeholder in the script", action)
		}
		if !strings.Contains(script, "4660") { // 0x1234
			t.Errorf("%s did not carry the window handle", action)
		}
	}
	for _, bad := range []string{"", "Start", "state; Remove-Item C:\\", "invoke", "start-magic"} {
		if _, err := voiceAgentUIAScript(0x1234, bad); err == nil {
			t.Errorf("action %q was accepted", bad)
		}
	}
	if _, err := voiceAgentUIAScript(0, "state"); err == nil {
		t.Error("a script was built for no window at all")
	}
}

// Pressing something that ends voice mode in order to start it would hang up on
// the caller, so the script must decide "this ends voice" before it decides
// "this starts voice".
func TestVoiceScriptChecksForAnEndControlFirst(t *testing.T) {
	script, err := voiceAgentUIAScript(1, "start")
	if err != nil {
		t.Fatal(err)
	}
	endAt := strings.Index(script, "$active = $true")
	startAt := strings.Index(script, "$startEl = $e")
	if endAt < 0 || startAt < 0 {
		t.Fatal("the script no longer distinguishes starting from ending voice mode")
	}
	if endAt > startAt {
		t.Error("the script looks for a way to start voice before it checks whether voice is already running")
	}
	// Nothing that would change the app's audio devices is ever pressed.
	if !strings.Contains(script, "setting|settings|input|output|device|volume") {
		t.Error("the script no longer refuses to press the app's audio settings")
	}
}

// The Answer control has Decline, Ignore and Send to voicemail sitting right
// beside it. Pressing one of those is worse than not answering at all.
func TestAnswerScriptNeverPressesDecline(t *testing.T) {
	script, err := googleVoiceAnswerUIAScript(99)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "decline|reject|ignore|dismiss|voicemail|block|spam") {
		t.Fatal("the accessibility answer path no longer refuses Decline and Send to voicemail")
	}
	declineAt := strings.Index(script, "decline|reject")
	answerAt := strings.Index(script, "answer|accept|pick")
	if declineAt > answerAt {
		t.Error("Decline is only ruled out after a control has already matched Answer")
	}
	if strings.Contains(script, "__HWND__") {
		t.Error("the answer script left its placeholder in")
	}
	if _, err := googleVoiceAnswerUIAScript(0); err == nil {
		t.Error("an answer script was built for no window")
	}
}

// FlipAi has to find the desktop app on a machine it has never seen. The
// executable list covers where these apps install themselves; the Start Menu
// shortcut covers a Store-packaged app, whose executable cannot be launched by
// path at all.
func TestDesktopAppIsLookedForWhereItInstallsItself(t *testing.T) {
	got := agentAppExecutables("C", `C:\Users\me\AppData\Local`, `C:\Program Files`, `C:\Program Files (x86)`)
	if len(got) == 0 {
		t.Fatal("no way to find the Codex desktop app")
	}
	joined := strings.ToLower(strings.Join(got, "\n"))
	for _, want := range []string{`appdata\local\programs\codex\codex.exe`, `chatgpt.exe`} {
		if !strings.Contains(joined, want) {
			t.Errorf("the candidate list does not include %s:\n%s", want, strings.Join(got, "\n"))
		}
	}
	seen := map[string]bool{}
	for _, p := range got {
		if seen[strings.ToLower(p)] {
			t.Errorf("%s is tried twice", p)
		}
		seen[strings.ToLower(p)] = true
	}
	// ChatGPT is the voice front-end (see agentAppTitles), so its per-user
	// install is what a spoken call launches first; a stale copy elsewhere, or
	// the standalone Codex app whose voice is less reliable, must not win.
	if !strings.HasPrefix(strings.ToLower(got[0]), `c:\users\me\appdata\local\programs\chatgpt`) {
		t.Errorf("the ChatGPT per-user install is not tried first: %q", got[0])
	}

	// An empty root contributes nothing rather than a path rooted at nowhere.
	for _, p := range agentAppExecutables("C", `C:\x`, "", "") {
		if strings.HasPrefix(p, `\`) {
			t.Errorf("an unset environment folder produced the path %q", p)
		}
	}

	if names := agentAppShortcutNames("C"); len(names) == 0 || names[0] != "ChatGPT" {
		t.Errorf("the ChatGPT Start Menu shortcut is not looked for first: %v", names)
	}
	if names := agentAppShortcutNames("A"); len(names) != 1 || names[0] != "Claude" {
		t.Errorf("the Claude agent looks for the wrong shortcut: %v", names)
	}
}

// The window FlipAi drives is the ChatGPT desktop app first -- it carries the
// voice a caller talks to, and drives Codex from there -- and the standalone
// Codex app second, and a title the user configured beats both.
func TestConfiguredWindowTitleWinsOverTheBuiltInList(t *testing.T) {
	titles := agentAppTitles("C")
	if len(titles) < 2 || titles[0] != "ChatGPT" {
		t.Fatalf("ChatGPT is not the first desktop app looked for: %v", titles)
	}
	if titles[1] != "Codex" {
		t.Errorf("the standalone Codex app is not the fallback: %v", titles)
	}
	if got := agentAppTitles("A"); len(got) != 1 || got[0] != "Claude" {
		t.Errorf("the Claude agent looks for %v", got)
	}
}
