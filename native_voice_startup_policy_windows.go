//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Windows policy for the current FlipAi product:
//   * direct Google Voice is the live SMS transport;
//   * the background host always comes back at the next interactive sign-in;
//   * pre-sign-in/S4U startup is retired because persistent WebView2 sessions
//     must restore inside the user's normal Windows session.
//
// Gmail source is deliberately not deleted. The v0.46.50 Gmail-capable state
// is also preserved on the archive/gmail-voice-bridge-v0.46.50 branch.
func init() {
	mode := ""
	if len(os.Args) > 1 {
		mode = strings.ToLower(strings.TrimSpace(os.Args[1]))
	}

	// A quit.flag means "stop this running process tree", not "stay dead after
	// the next reboot/sign-in forever". Every newly launched watchdog begins a
	// new Windows startup cycle, so clear a stale flag before main reaches the
	// watchdog's first quitRequested check. This fixes the state where the tray
	// could reappear but the host never resumed polling until the UI was opened.
	if mode == "--watchdog" {
		if dataDir, _, _, _, err := appPaths(); err == nil {
			_ = os.Remove(filepath.Join(dataDir, "quit.flag"))
		}
	}

	if mode != "--host" {
		return
	}
	dataDir, configPath, _, _, err := appPaths()
	if err != nil {
		return
	}

	// Signed-in startup is intentionally always present and is now the only
	// supported Windows startup path. It keeps the existing tray/desktop broker
	// architecture and all persistent WebView2 profiles in the interactive user
	// session that created their credentials.
	if exe, err := os.Executable(); err == nil {
		_ = installAutostart(exe)
	}

	// Migrate an existing Gmail-selected install to the native Google Voice
	// transport before runHost loads bridge.json. Use a generic JSON edit so old
	// Gmail credentials remain untouched on disk and can be restored from the
	// archive branch later; the app simply stops selecting them.
	raw, err := os.ReadFile(configPath)
	if err != nil || len(raw) == 0 {
		return
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return
	}
	gmail, _ := doc["gmail"].(map[string]any)
	if gmail == nil {
		gmail = map[string]any{}
		doc["gmail"] = gmail
	}
	if current, _ := gmail["method"].(string); current == GmailMethodGoogleVoice {
		return
	}
	gmail["method"] = GmailMethodGoogleVoice
	updated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return
	}
	updated = append(updated, '\n')
	tmp := configPath + ".native-voice.tmp"
	if os.WriteFile(tmp, updated, 0600) == nil {
		if os.Rename(tmp, configPath) != nil {
			_ = os.Remove(tmp)
		}
	}
	_ = dataDir // kept explicit: policy is scoped to this FlipAi data directory.
}
