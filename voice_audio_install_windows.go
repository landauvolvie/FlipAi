//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// This is the Windows half of the audio-bridge setup. It is deliberately small:
// FlipAi opens the vendor's download page for the pair the PC still needs, and
// that is all. See voice_audio_bridge.go for why FlipAi no longer installs a
// driver itself -- the short version is that Windows will not load a
// kernel-mode audio driver unless Microsoft signed it, and the free driver
// FlipAi used to download is not signed that way, so the install ended in
// problem code 52 no matter what the installer did.
//
// Wiring the endpoints, once they exist, stays entirely automatic.

const voiceAudioInstallListen = "127.0.0.1:8772"

var voiceAudioInstallMu sync.Mutex

func init() {
	if len(os.Args) < 2 || os.Args[1] != "--host" {
		return
	}
	dataDir, cfgPath, _, _, err := appPaths()
	if err != nil {
		return
	}
	cfg, err := loadConfig(cfgPath, dataDir)
	if err != nil {
		cfg = defaultConfig(dataDir)
	}
	go startVoiceAudioInstallServer(dataDir, cfg.Listen)
}

type voiceAudioInstallResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	// URL is the page FlipAi tried to open, so the UI can offer it as plain
	// text when no browser could be launched.
	URL string `json:"url,omitempty"`
}

func startVoiceAudioInstallServer(dataDir, mainListen string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/install", func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !voiceOriginAllowed(origin, mainListen) {
			http.Error(w, "FlipAi audio setup is local-only", http.StatusForbidden)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		voiceAudioInstallMu.Lock()
		defer voiceAudioInstallMu.Unlock()
		result := openVoiceAudioBridgeSource(dataDir)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if !result.OK {
			w.WriteHeader(http.StatusInternalServerError)
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	server := &http.Server{Addr: voiceAudioInstallListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	_ = server.ListenAndServe()
}

// openVoiceAudioBridgeSource sends the user to the vendor page for whichever
// free, Microsoft-signed pair the PC is still missing.
func openVoiceAudioBridgeSource(dataDir string) voiceAudioInstallResult {
	setup := planVoiceAudioBridge(currentVoiceCablePlan(dataDir))
	if setup.Done {
		return voiceAudioInstallResult{OK: true, Message: setup.Headline}
	}
	// openBrowser is the product's one way of opening a link: ShellExecuteW,
	// the same call the rest of FlipAi uses, and no DLL-launch helper.
	if err := openBrowser(setup.Next.URL); err != nil {
		return voiceAudioInstallResult{
			Message: "FlipAi could not open your browser. Go to " + setup.Next.URL + " and download " + setup.Next.Name + ", which is free.",
			URL:     setup.Next.URL,
		}
	}
	mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
		s.RoutingNote = "Waiting for " + setup.Next.Name + ". FlipAi wires the endpoints as soon as Windows publishes them."
		s.LastEvent = "audio-bridge-guided"
	})
	return voiceAudioInstallResult{
		OK:      true,
		Message: setup.Next.Name + " opened in your browser. " + strings.Join(setup.Steps[1:], " "),
		URL:     setup.Next.URL,
	}
}
