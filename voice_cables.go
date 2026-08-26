package main

import (
	"sort"
	"strings"
)

// A phone conversation between Google Voice and the desktop AI app needs two
// one-way paths, and on Windows each path is one virtual audio cable: sound
// played into the cable's render endpoint comes back out of its capture
// endpoint, silently, with no speaker or microphone anywhere near it.
//
// Nobody should have to pick these by hand. FlipAi reads the machine's device
// list (reported by the Google Voice page, which is the one place real
// endpoint names are visible), recognizes the cable families people actually
// install -- VB-CABLE, VB-CABLE A/B, VoiceMeeter -- and wires both directions
// itself: Google Voice's side is pinned inside the page, and the desktop app's
// side is set per-app through the Windows audio policy store. The user selects
// nothing, in FlipAi or in the AI app.

type VoiceAudioDevice struct {
	Kind     string `json:"kind"` // audioinput or audiooutput
	DeviceID string `json:"deviceId,omitempty"`
	Label    string `json:"label"`
}

// voiceCablePlan is the whole automatic wiring decision for one machine.
type voiceCablePlan struct {
	// Cable 1 carries the caller to the agent.
	GoogleVoiceOutput string `json:"googleVoiceOutput,omitempty"` // render: Google Voice's "speaker"
	AgentInput        string `json:"agentInput,omitempty"`        // capture: the desktop app's "microphone"
	// Cable 2 carries the agent back to the caller.
	AgentOutput      string `json:"agentOutput,omitempty"`      // render: the desktop app's "speaker"
	GoogleVoiceInput string `json:"googleVoiceInput,omitempty"` // capture: Google Voice's "microphone"

	// Cables names the families the wiring uses, for the status row.
	Cables []string `json:"cables,omitempty"`
	// Warning says, in the words the Settings page shows, why a call would not
	// carry sound both ways. Empty means the path is complete.
	Warning string `json:"warning,omitempty"`
}

func (p voiceCablePlan) complete() bool {
	return p.GoogleVoiceOutput != "" && p.AgentInput != "" && p.AgentOutput != "" && p.GoogleVoiceInput != ""
}

// cableFamily classifies one endpoint into the virtual cable it belongs to,
// or "" for an ordinary device. Order matters: the more specific names carry
// their own tokens ("CABLE-A") that the generic pattern would also match.
func cableFamily(label string) string {
	l := strings.ToLower(label)
	switch {
	case strings.Contains(l, "cable-a"):
		return "cable-a"
	case strings.Contains(l, "cable-b"):
		return "cable-b"
	case strings.Contains(l, "cable-c"):
		return "cable-c"
	case strings.Contains(l, "cable-d"):
		return "cable-d"
	case strings.Contains(l, "vb-audio point"):
		return "vb-point"
	case strings.Contains(l, "cable input") || strings.Contains(l, "cable output"):
		return "cable"
	case strings.Contains(l, "voicemeeter vaio3") || strings.Contains(l, "vaio3"):
		return "voicemeeter-vaio3"
	case strings.Contains(l, "voicemeeter aux"):
		return "voicemeeter-aux"
	case strings.Contains(l, "voicemeeter"):
		return "voicemeeter"
	case strings.Contains(l, "virtual audio cable") || (strings.Contains(l, "virtual") && strings.Contains(l, "cable")):
		// Generic third-party cables: pair by the name with its line direction
		// words removed, so "Line 1 (Virtual Audio Cable)" render and capture
		// meet in one family while Line 2 stays its own.
		f := l
		for _, drop := range []string{"input", "output", "(", ")", " "} {
			f = strings.ReplaceAll(f, drop, "")
		}
		return "vac:" + f
	}
	return ""
}

// cableFamilyRank orders usable cables so the same machine always wires the
// same way: dedicated cables first, in their lettered order, then VoiceMeeter
// strips, then anything else alphabetically.
func cableFamilyRank(family string) int {
	order := []string{"cable", "cable-a", "cable-b", "cable-c", "cable-d", "vb-point", "voicemeeter", "voicemeeter-aux", "voicemeeter-vaio3"}
	for i, f := range order {
		if f == family {
			return i
		}
	}
	return len(order)
}

// planVoiceCables works out the whole audio wiring from the machine's device
// list. It needs two distinct cables; with one it wires the caller-to-agent
// half and says exactly what is missing, and with none it says what to
// install.
func planVoiceCables(devices []VoiceAudioDevice) voiceCablePlan {
	type pair struct {
		family  string
		render  string
		capture string
	}
	found := map[string]*pair{}
	for _, d := range devices {
		family := cableFamily(d.Label)
		if family == "" || strings.TrimSpace(d.Label) == "" {
			continue
		}
		p := found[family]
		if p == nil {
			p = &pair{family: family}
			found[family] = p
		}
		switch d.Kind {
		case "audiooutput":
			if p.render == "" {
				p.render = d.Label
			}
		case "audioinput":
			if p.capture == "" {
				p.capture = d.Label
			}
		}
	}
	usable := []*pair{}
	for _, p := range found {
		if p.render != "" && p.capture != "" {
			usable = append(usable, p)
		}
	}
	sort.Slice(usable, func(i, j int) bool {
		ri, rj := cableFamilyRank(usable[i].family), cableFamilyRank(usable[j].family)
		if ri != rj {
			return ri < rj
		}
		return usable[i].family < usable[j].family
	})

	var plan voiceCablePlan
	switch len(usable) {
	case 0:
		if len(devices) == 0 {
			// Endpoint names only become visible once the Google Voice window
			// is running and holds its microphone grant; until then nothing is
			// known about this machine's audio and "install a cable" would be
			// a guess.
			plan.Warning = "The audio devices on this PC are not known yet; they are read by the Google Voice window once it is running."
			return plan
		}
		plan.Warning = "No virtual audio cable is installed, so there is no silent path between Google Voice and the desktop app. Install two virtual cables once -- VB-CABLE A+B, or VoiceMeeter (which includes two) -- and FlipAi wires everything itself."
	case 1:
		plan.GoogleVoiceOutput = usable[0].render
		plan.AgentInput = usable[0].capture
		plan.Cables = []string{usable[0].family}
		plan.Warning = "Only one virtual audio cable was found, which carries the caller to the desktop app but nothing back. Install a second cable -- VB-CABLE A or B, or VoiceMeeter's Aux pair -- and FlipAi wires the return path itself."
	default:
		plan.GoogleVoiceOutput = usable[0].render
		plan.AgentInput = usable[0].capture
		plan.AgentOutput = usable[1].render
		plan.GoogleVoiceInput = usable[1].capture
		plan.Cables = []string{usable[0].family, usable[1].family}
	}
	return plan
}

// applyCableOverrides lets a hand-edited voice-call.json pin an endpoint the
// automatic plan would not have chosen. An override only applies when a device
// with that exact or containing label is really present in the right
// direction: a stale name from an unplugged or renamed device must not
// silently replace a working automatic choice.
func applyCableOverrides(plan voiceCablePlan, cfg VoiceCallConfig, devices []VoiceAudioDevice) voiceCablePlan {
	present := func(kind, wanted string) bool {
		if wanted == "" {
			return false
		}
		w := strings.ToLower(wanted)
		for _, d := range devices {
			if d.Kind == kind && d.Label != "" && strings.Contains(strings.ToLower(d.Label), w) {
				return true
			}
		}
		return false
	}
	if present("audiooutput", cfg.GoogleVoiceOutput) {
		plan.GoogleVoiceOutput = cfg.GoogleVoiceOutput
	}
	if present("audioinput", cfg.GoogleVoiceInput) {
		plan.GoogleVoiceInput = cfg.GoogleVoiceInput
	}
	if present("audioinput", cfg.AgentInput) {
		plan.AgentInput = cfg.AgentInput
	}
	if present("audiooutput", cfg.AgentOutput) {
		plan.AgentOutput = cfg.AgentOutput
	}
	if plan.complete() {
		if strings.EqualFold(plan.AgentOutput, plan.GoogleVoiceOutput) {
			plan.Warning = "Google Voice and the desktop app are wired to the same render endpoint, so the agent would hear itself instead of the caller. Each direction needs its own cable; clear the overrides in voice-call.json to let FlipAi choose."
		} else if strings.EqualFold(plan.AgentInput, plan.GoogleVoiceInput) {
			plan.Warning = "Google Voice and the desktop app are wired to the same capture endpoint, so the caller would hear themselves instead of the agent. Each direction needs its own cable; clear the overrides in voice-call.json to let FlipAi choose."
		} else {
			plan.Warning = ""
		}
	}
	return plan
}

// currentVoiceCablePlan is the wiring for the machine as last reported by the
// Google Voice window, with any hand-edited overrides applied.
func currentVoiceCablePlan(dataDir string) voiceCablePlan {
	cfg := loadVoiceCallConfig(dataDir)
	rt := loadVoiceRuntime(dataDir)
	return applyCableOverrides(planVoiceCables(rt.Devices), cfg, rt.Devices)
}
