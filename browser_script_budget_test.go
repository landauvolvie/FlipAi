package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The generic page-probe deadline. A script that can legitimately run longer
// than this must be marked so the DevTools layer gives it more.
const genericPageProbeBudgetSeconds = 8

var (
	jsConstRE    = regexp.MustCompile("(?s)const (\\w*JS) = (.*?)\n(?:const |func |type |var |// )")
	jsDeadlineRE = regexp.MustCompile(`Date\.now\(\)\s*\+\s*(\d+)`)
	jsRetryRE    = regexp.MustCompile(`for\(let \w+=0;\w+<(\d+)[^)]*\)\{[^}]*sleep\((\d+)\)`)
)

// scriptBudgetMillis estimates how long a page script may keep running before
// it gives up on its own: its longest explicit deadline, or its longest
// retry loop.
func scriptBudgetMillis(js string) int {
	worst := 0
	for _, m := range jsDeadlineRE.FindAllStringSubmatch(js, -1) {
		if n, err := strconv.Atoi(m[1]); err == nil && n > worst {
			worst = n
		}
	}
	for _, m := range jsRetryRE.FindAllStringSubmatch(js, -1) {
		n, err1 := strconv.Atoi(m[1])
		ms, err2 := strconv.Atoi(m[2])
		if err1 == nil && err2 == nil && n*ms > worst {
			worst = n * ms
		}
	}
	return worst
}

// A page script that can run longer than the generic probe deadline gets cut
// off mid-work and reported as an unresponsive page. ChatGPT's experience
// picker retries for over twelve seconds and was given eight, so a slow picker
// failed every message with "the ChatGPT WebView did not answer
// Runtime.evaluate" -- and ChatGPT never saw the message at all.
//
// Any such script must carry a marker the DevTools layer recognizes.
func TestLongPageScriptsAreGivenADeadlineThatOutlastsThem(t *testing.T) {
	// The markers the DevTools layer recognizes, as literals: the constants
	// themselves are Windows-only, and this guard has to run everywhere the
	// drivers are edited. TestRecognizedLongCallMarkersMatchTheirConstants
	// keeps the two in step.
	markers := []string{
		"const deadline=Date.now()+90000;", // a browser turn
		"__FLIPAI_LONG_PAGE_CALL__", "browserLongPageCallMarker",
		"__FLIPAI_BROWSER_RETURN_MEDIA__", "browserChatReturnedMediaMarker",
		"__FLIPAI_GV_UI_SEND__",
		"__FLIPAI_GV_MEDIA_CAPTURE__",
		"__flipAiGVRequest",
		"__FLIPAI_BROWSER_LONG_TURN_SNAPSHOT__", "browserLongTurnSnapshotMarker",
	}
	files := []string{
		"chatgpt_webview_windows.go", "claude_chat_webview_windows.go",
		"copilot_chat_webview_windows.go", "gemini_chat_webview_windows.go",
		"grok_chat_webview_windows.go", "muse_chat_webview_windows.go",
	}
	for _, file := range files {
		src := readGoSource(t, file)
		for _, m := range jsConstRE.FindAllStringSubmatch(src, -1) {
			name, js := m[1], m[2]
			budget := scriptBudgetMillis(js)
			if budget <= genericPageProbeBudgetSeconds*1000 {
				continue
			}
			marked := false
			for _, marker := range markers {
				if strings.Contains(js, marker) {
					marked = true
					break
				}
			}
			if !marked {
				t.Errorf("%s: %s keeps working for up to %.1fs but carries no marker, so the DevTools layer allows it only %ds and reports the page as unresponsive",
					file, name, float64(budget)/1000, genericPageProbeBudgetSeconds)
			}
		}
	}
}

// The marker literals above are the ones the DevTools layer actually keys on.
func TestRecognizedLongCallMarkersMatchTheirConstants(t *testing.T) {
	want := map[string]string{
		"browserLongPageCallMarker":       "__FLIPAI_LONG_PAGE_CALL__",
		"browserLongTurnSnapshotMarker":   "__FLIPAI_BROWSER_LONG_TURN_SNAPSHOT__",
		"browserChatReturnedMediaMarker":  "__FLIPAI_BROWSER_RETURN_MEDIA__",
		"googleVoiceSMSUITurnMarker":      "__FLIPAI_GV_UI_SEND__",
		"googleVoiceMediaCaptureMarker":   "__FLIPAI_GV_MEDIA_CAPTURE__",
		"googleVoiceSMSPageRequestMarker": "__flipAiGVRequest",
	}
	var src strings.Builder
	for _, file := range []string{
		"browser_chat_return_media_windows.go", "browser_long_turn_windows.go",
		"google_voice_sms_windows.go", "google_voice_sms_api_windows.go",
		"google_voice_media_windows.go", "voice_cdp_windows.go",
		"google_voice_sms_webview_windows.go", "google_voice_media_script.go",
		"voice_page_script.go", "google_voice_sms_netcapture.go",
	} {
		if s, err := os.ReadFile(file); err == nil {
			src.Write(s)
		}
	}
	all := strings.ReplaceAll(src.String(), "\r\n", "\n")
	for name, literal := range want {
		if !strings.Contains(all, name+" = \""+literal+"\"") {
			t.Errorf("%s is no longer defined as %q; the long-call guard keys on that literal", name, literal)
		}
	}
}
