package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestV046VoiceStartUsesSeparateVerifiedActivationMethods(t *testing.T) {
	want := []string{"start-invoke", "start-keyboard", "start-legacy", "start-pointer"}
	if !reflect.DeepEqual(agentVoiceStartActions, want) {
		t.Fatalf("voice activation order = %v, want %v", agentVoiceStartActions, want)
	}
	for _, action := range want {
		script, err := voiceAgentUIAScript(0x1234, action)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		if strings.Contains(script, "__ACTION__") {
			t.Fatalf("%s left the action placeholder in the UIA script", action)
		}
		if !strings.Contains(script, "$action = '"+action+"'") {
			t.Fatalf("%s was not embedded as a fixed action", action)
		}
	}

	// The pointer call is no longer allowed to mean "Voice started" merely
	// because mouse_event returned. Each method reports only that its operation
	// was sent; the Windows caller must perform a separate active-state read.
	script, _ := voiceAgentUIAScript(0x1234, "start-pointer")
	if !strings.Contains(script, "pointer-sent") || strings.Contains(script, "PointerClickElement $target\nif (-not $done)") {
		t.Fatal("pointer activation can again swallow the verified fallbacks")
	}
}

func TestV046RealCallsUseVerifiedStartAndRerouteAfterVoice(t *testing.T) {
	receiver, err := os.ReadFile("voice_receiver_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(receiver), "startAgentVoiceSessionVerified(dataDir, cfg, agent)") {
		t.Fatal("the real Google Voice bridge is not using the verified v0.46 Voice start path")
	}

	start, err := os.ReadFile("voice_agent_start_verified_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(start)
	if !strings.Contains(body, "waitForAgentVoice(hwnd, true, agentVoiceAttemptDeadline(deadline))") {
		t.Fatal("an activation method is not verified before the next method can run")
	}
	if !strings.Contains(body, "completeVerifiedAgentVoiceStart") || !strings.Contains(body, "routeAgentAppAudio(dataDir, cfg, agent)") {
		t.Fatal("audio is not re-routed after live Voice creates its Electron audio process")
	}
}

func TestV046AudioRouterTargetsElectronProcessTree(t *testing.T) {
	for _, want := range []string{
		"CreateToolhelp32Snapshot",
		"th32ParentProcessID",
		"CandidateProcessIds",
		"PersistProcessTree",
		"foreach (uint candidatePid in CandidateProcessIds(rootProcessId))",
	} {
		if !strings.Contains(routeAppAudioPS, want) {
			t.Errorf("process-tree audio routing is missing %q", want)
		}
	}
	// Keep the two parts already proven correct in v0.45: EarTrumpet's current
	// AudioPolicyConfig IID/method ordering and native HSTRING device-id ABI.
	for _, want := range []string{
		`Guid("ab3d4648-e242-459f-b02f-541c70306324")`,
		"IntPtr deviceId",
		"WindowsCreateString",
		`#{e6327cad-dcec-4949-ae8a-991e976a79d2}`,
	} {
		if !strings.Contains(routeAppAudioPS, want) {
			t.Errorf("working AudioPolicyConfig behavior lost %q", want)
		}
	}
	if strings.Contains(routeAppAudioPS, "read-back HRESULT") {
		t.Error("the old diagnostic still mislabels E_INVALIDARG as a vtable-slot failure")
	}
}
