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
// list, recognizes supported cable families, and wires both directions itself.
// FlipAi's preferred free path is two instances of the MIT-licensed Virtual
// Audio Driver by MTT, installed by the one-click audio-bridge installer. Existing
// VB-CABLE/VoiceMeeter installs remain supported too.

type VoiceAudioDevice struct {
	Kind     string `json:"kind"` // audioinput or audiooutput
	DeviceID string `json:"deviceId,omitempty"`
	Label    string `json:"label"`
}

type voiceCablePlan struct {
	GoogleVoiceOutput string `json:"googleVoiceOutput,omitempty"`
	AgentInput        string `json:"agentInput,omitempty"`
	AgentOutput       string `json:"agentOutput,omitempty"`
	GoogleVoiceInput  string `json:"googleVoiceInput,omitempty"`
	Cables            []string `json:"cables,omitempty"`
	Warning           string   `json:"warning,omitempty"`
}

func (p voiceCablePlan) complete() bool {
	return p.GoogleVoiceOutput != "" && p.AgentInput != "" && p.AgentOutput != "" && p.GoogleVoiceInput != ""
}

func numericDevicePrefix(l string) string {
	l = strings.TrimSpace(l)
	dash := strings.Index(l, "-")
	if dash <= 0 || dash > 3 {
		return "1"
	}
	prefix := strings.TrimSpace(l[:dash])
	if prefix == "" {
		return "1"
	}
	for _, r := range prefix {
		if r < '0' || r > '9' {
			return "1"
		}
	}
	return prefix
}

func cableFamily(label string) string {
	l := strings.ToLower(strings.TrimSpace(label))
	switch {
	case strings.Contains(l, "virtual audio driver by mtt") || strings.Contains(l, "virtual mic driver by mtt"):
		// Multiple root instances have the same base endpoint names. Windows
		// disambiguates the later ones with prefixes such as "2- ". Pair the
		// matching speaker/microphone by that prefix so instance 1 and instance 2
		// become the two independent FlipAi directions.
		return "flipai-mtt-" + numericDevicePrefix(l)
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
		f := l
		for _, drop := range []string{"input", "output", "(", ")", " "} {
			f = strings.ReplaceAll(f, drop, "")
		}
		return "vac:" + f
	}
	return ""
}

func cableFamilyRank(family string) int {
	// Prefer FlipAi's managed/free driver whenever it is present, then preserve
	// the existing deterministic order for third-party cable packages.
	if strings.HasPrefix(family, "flipai-mtt-") {
		return -10
	}
	order := []string{"cable", "cable-a", "cable-b", "cable-c", "cable-d", "vb-point", "voicemeeter", "voicemeeter-aux", "voicemeeter-vaio3"}
	for i, f := range order {
		if f == family {
			return i
		}
	}
	return len(order)
}

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
			plan.Warning = "The audio devices on this PC are not known yet; they are read by the Google Voice window once it is running."
			return plan
		}
		plan.Warning = "No two-way virtual audio bridge is installed yet. Use the free built-in audio-bridge installer below; FlipAi installs two signed virtual speaker/microphone pairs and wires them automatically."
	case 1:
		plan.GoogleVoiceOutput = usable[0].render
		plan.AgentInput = usable[0].capture
		plan.Cables = []string{usable[0].family}
		plan.Warning = "Only one virtual audio pair was found. FlipAi needs two independent pairs for caller-to-agent and agent-to-caller audio. Use the built-in audio-bridge installer to create the missing pair."
	default:
		plan.GoogleVoiceOutput = usable[0].render
		plan.AgentInput = usable[0].capture
		plan.AgentOutput = usable[1].render
		plan.GoogleVoiceInput = usable[1].capture
		plan.Cables = []string{usable[0].family, usable[1].family}
	}
	return plan
}

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
			plan.Warning = "Google Voice and the desktop app are wired to the same render endpoint, so the agent would hear itself instead of the caller. Each direction needs its own audio pair; clear the overrides in voice-call.json to let FlipAi choose."
		} else if strings.EqualFold(plan.AgentInput, plan.GoogleVoiceInput) {
			plan.Warning = "Google Voice and the desktop app are wired to the same capture endpoint, so the caller would hear themselves instead of the agent. Each direction needs its own audio pair; clear the overrides in voice-call.json to let FlipAi choose."
		} else {
			plan.Warning = ""
		}
	}
	return plan
}

func currentVoiceCablePlan(dataDir string) voiceCablePlan {
	cfg := loadVoiceCallConfig(dataDir)
	rt := loadVoiceRuntime(dataDir)
	return applyCableOverrides(planVoiceCables(rt.Devices), cfg, rt.Devices)
}
