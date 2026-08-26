//go:build windows

package main

import (
	"regexp"
	"strings"
	"testing"
)

func TestVoiceUIBootstrapsBeforeDocumentElementExists(t *testing.T) {
	if !strings.Contains(baseDesktopInitScript, "globalThis.__flipaiDesktop = true") {
		t.Fatal("desktop startup must mark the FlipAi WebView without requiring documentElement")
	}
	if !strings.Contains(baseDesktopInitScript, "if (document.documentElement)") {
		t.Fatal("documentElement access must be guarded at document-created time")
	}
}

func TestVoiceUIShowsServiceFailureInsteadOfDisappearing(t *testing.T) {
	if !strings.Contains(voiceDesktopInitScript, "serviceErrorCard") {
		t.Fatal("voice UI must render a visible local-service failure state")
	}
	if !strings.Contains(voiceDesktopInitScript, "catch(e){serviceErrorCard(e);return}") {
		t.Fatal("voice status failure must not silently hide the calling feature")
	}
	if !strings.Contains(voiceVisibilityFallbackScript, "Google Voice calling is installed") {
		t.Fatal("desktop UI needs a fallback visibility warning")
	}
}

// The desktop shell warns, after four seconds, that the voice service is not
// responding when no card has appeared. It looks for the card by id, so the id
// it looks for has to be the id the card is given -- when the card was renamed
// and this was not, every working Connections page announced a broken voice
// service to a user whose voice service was fine.
func TestTheNoServiceWarningLooksForTheCardThatIsActuallyBuilt(t *testing.T) {
	ids := regexp.MustCompile(`#voice-(?:call|preview)-[a-z-]+`).FindAllString(voiceVisibilityFallbackScript, -1)
	if len(ids) < 2 {
		t.Fatalf("the fallback warning must look for both the Settings and Connections cards, found %v", ids)
	}
	for _, id := range ids {
		// The banner's own id is the one thing it may name without building it.
		if id == "#voice-call-unavailable" {
			continue
		}
		if !strings.Contains(voiceDesktopInitScript, "card.id='"+strings.TrimPrefix(id, "#")+"'") {
			t.Errorf("the fallback waits for %s, which the voice card script never creates", id)
		}
	}
}
