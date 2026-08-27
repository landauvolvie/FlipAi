package main

import (
	"strings"
	"testing"
)

func dev(kind, label string) VoiceAudioDevice {
	return VoiceAudioDevice{Kind: kind, Label: label, DeviceID: label}
}

func TestCablePlanWiresBothDirectionsFromInstalledCables(t *testing.T) {
	plan := planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "Speakers (Realtek High Definition Audio)"),
		dev("audioinput", "Microphone Array (Intel Smart Sound)"),
		dev("audiooutput", "CABLE-A Input (VB-Audio Cable A)"),
		dev("audioinput", "CABLE-A Output (VB-Audio Cable A)"),
		dev("audiooutput", "CABLE-B Input (VB-Audio Cable B)"),
		dev("audioinput", "CABLE-B Output (VB-Audio Cable B)"),
	})
	if plan.Warning != "" {
		t.Fatalf("two installed cables still warned: %s", plan.Warning)
	}
	if plan.GoogleVoiceOutput != "CABLE-A Input (VB-Audio Cable A)" || plan.AgentInput != "CABLE-A Output (VB-Audio Cable A)" {
		t.Errorf("caller-to-agent direction wired wrongly: %+v", plan)
	}
	if plan.AgentOutput != "CABLE-B Input (VB-Audio Cable B)" || plan.GoogleVoiceInput != "CABLE-B Output (VB-Audio Cable B)" {
		t.Errorf("agent-to-caller direction wired wrongly: %+v", plan)
	}
	for _, chosen := range []string{plan.GoogleVoiceInput, plan.GoogleVoiceOutput, plan.AgentInput, plan.AgentOutput} {
		if strings.Contains(chosen, "Realtek") || strings.Contains(chosen, "Intel") {
			t.Errorf("a real device was wired into the call: %q", chosen)
		}
	}
}

func TestCablePlanRecognizesFlipAiManagedMTTInstances(t *testing.T) {
	plan := planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "Virtual Audio Driver by MTT"),
		dev("audioinput", "Virtual Mic Driver by MTT"),
		dev("audiooutput", "2- Virtual Audio Driver by MTT"),
		dev("audioinput", "2- Virtual Mic Driver by MTT"),
		dev("audiooutput", "Speakers (Realtek)"),
		dev("audioinput", "Microphone (Realtek)"),
	})
	if !plan.complete() || plan.Warning != "" {
		t.Fatalf("two managed MTT instances were not wired: %+v", plan)
	}
	if plan.GoogleVoiceOutput != "Virtual Audio Driver by MTT" || plan.AgentInput != "Virtual Mic Driver by MTT" {
		t.Fatalf("first managed pair is wrong: %+v", plan)
	}
	if plan.AgentOutput != "2- Virtual Audio Driver by MTT" || plan.GoogleVoiceInput != "2- Virtual Mic Driver by MTT" {
		t.Fatalf("second managed pair is wrong: %+v", plan)
	}
}

func TestCablePlanRecognizesVoiceMeeter(t *testing.T) {
	plan := planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "VoiceMeeter Input (VB-Audio VoiceMeeter VAIO)"),
		dev("audioinput", "VoiceMeeter Output (VB-Audio VoiceMeeter VAIO)"),
		dev("audiooutput", "VoiceMeeter Aux Input (VB-Audio VoiceMeeter AUX VAIO)"),
		dev("audioinput", "VoiceMeeter Aux Output (VB-Audio VoiceMeeter AUX VAIO)"),
		dev("audiooutput", "Speakers (USB Audio)"),
	})
	if !plan.complete() || plan.Warning != "" {
		t.Fatalf("VoiceMeeter's two strips were not wired: %+v", plan)
	}
}

func TestCablePlanPairsThePlainVBCableWithASecondFamily(t *testing.T) {
	plan := planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "CABLE Input (VB-Audio Virtual Cable)"),
		dev("audioinput", "CABLE Output (VB-Audio Virtual Cable)"),
		dev("audiooutput", "VoiceMeeter Input (VB-Audio VoiceMeeter VAIO)"),
		dev("audioinput", "VoiceMeeter Output (VB-Audio VoiceMeeter VAIO)"),
	})
	if !plan.complete() || plan.Warning != "" {
		t.Fatalf("a cable plus VoiceMeeter was not enough: %+v", plan)
	}
	if plan.GoogleVoiceOutput != "CABLE Input (VB-Audio Virtual Cable)" {
		t.Errorf("the dedicated cable should carry the caller first: %+v", plan)
	}
}

func TestCablePlanExplainsWhatIsMissing(t *testing.T) {
	none := planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "Speakers (Realtek High Definition Audio)"),
		dev("audioinput", "Microphone (Realtek High Definition Audio)"),
	})
	if none.complete() {
		t.Fatal("a machine with no cables reported a complete path")
	}
	if !strings.Contains(none.Warning, "built-in audio-bridge installer") {
		t.Errorf("a cableless machine must point at the built-in free installer, got %q", none.Warning)
	}

	one := planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "CABLE Input (VB-Audio Virtual Cable)"),
		dev("audioinput", "CABLE Output (VB-Audio Virtual Cable)"),
	})
	if one.GoogleVoiceOutput == "" || one.AgentInput == "" {
		t.Error("the one installed cable should still carry the caller to the agent")
	}
	if !strings.Contains(one.Warning, "two independent pairs") {
		t.Errorf("one cable must ask for the missing pair, got %q", one.Warning)
	}

	empty := planVoiceCables(nil)
	if !strings.Contains(empty.Warning, "not known yet") {
		t.Errorf("an unreported machine must not be told to install anything, got %q", empty.Warning)
	}
}

func TestCablePlanIgnoresHalfCables(t *testing.T) {
	plan := planVoiceCables([]VoiceAudioDevice{
		dev("audiooutput", "CABLE Input (VB-Audio Virtual Cable)"),
		dev("audiooutput", "Speakers (Realtek)"),
	})
	if plan.GoogleVoiceOutput != "" {
		t.Errorf("a cable with no capture side was wired: %+v", plan)
	}
}

func TestCableOverridesApplyOnlyWhenPresent(t *testing.T) {
	devices := []VoiceAudioDevice{
		dev("audiooutput", "CABLE-A Input (VB-Audio Cable A)"),
		dev("audioinput", "CABLE-A Output (VB-Audio Cable A)"),
		dev("audiooutput", "CABLE-B Input (VB-Audio Cable B)"),
		dev("audioinput", "CABLE-B Output (VB-Audio Cable B)"),
		dev("audioinput", "Line 3 (Odd Virtual Device)"),
	}
	base := planVoiceCables(devices)

	cfg := defaultVoiceCallConfig()
	cfg.GoogleVoiceInput = "Line 3 (Odd Virtual Device)"
	got := applyCableOverrides(base, cfg, devices)
	if got.GoogleVoiceInput != "Line 3 (Odd Virtual Device)" {
		t.Errorf("a present override was not applied: %+v", got)
	}
	if got.Warning != "" {
		t.Errorf("a consistent override warned: %q", got.Warning)
	}

	stale := defaultVoiceCallConfig()
	stale.GoogleVoiceInput = "Unplugged Cable (gone)"
	got = applyCableOverrides(base, stale, devices)
	if got.GoogleVoiceInput != base.GoogleVoiceInput {
		t.Errorf("a stale override replaced the automatic choice: %+v", got)
	}

	clash := defaultVoiceCallConfig()
	clash.AgentOutput = base.GoogleVoiceOutput
	got = applyCableOverrides(base, clash, devices)
	if !strings.Contains(got.Warning, "same render endpoint") {
		t.Errorf("wiring both sides to one render endpoint must warn, got %q", got.Warning)
	}
}
