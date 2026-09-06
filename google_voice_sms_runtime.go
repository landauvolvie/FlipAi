package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type GoogleVoiceSMSRuntimeState struct {
	Running            bool      `json:"running"`
	Starting           bool      `json:"starting,omitempty"`
	Visible            bool      `json:"visible"`
	LoginActive        bool      `json:"loginActive,omitempty"`
	Connected          bool      `json:"connected,omitempty"`
	SignedIn           bool      `json:"signedIn"`
	ListenerRunning    bool      `json:"listenerRunning"`
	Ready              bool      `json:"ready"`
	Page               string    `json:"page,omitempty"`
	LastEvent          string    `json:"lastEvent,omitempty"`
	LastError          string    `json:"lastError,omitempty"`
	LastNote           string    `json:"lastNote,omitempty"`
	LastProbeAt        time.Time `json:"lastProbeAt,omitempty"`
	LastObserverAt     time.Time `json:"lastObserverAt,omitempty"`
	ObservedRows       int       `json:"observedRows,omitempty"`
	ObserverCandidates int       `json:"observerCandidates,omitempty"`
	LastInboundAt      time.Time `json:"lastInboundAt,omitempty"`
	LastOutboundAt     time.Time `json:"lastOutboundAt,omitempty"`
	DesktopRequest     string    `json:"desktopRequest,omitempty"`
	DesktopRequestAt   time.Time `json:"desktopRequestAt,omitempty"`
	UpdatedAt          time.Time `json:"updatedAt,omitempty"`
}

// googleVoiceSMSFreshWindow is how long one recorded listener probe keeps
// proving that the background connection is alive. Every readiness decision --
// the Connections card, Test, and the outbound send gate -- uses this single
// window so they can never disagree about what "connected" means.
const googleVoiceSMSFreshWindow = 10 * time.Second

// googleVoiceSMSProbeFresh is the one definition of a live listener: the
// background browser recorded a successful Google Voice poll moments ago.
func googleVoiceSMSProbeFresh(s GoogleVoiceSMSRuntimeState) bool {
	return !s.LastProbeAt.IsZero() && time.Since(s.LastProbeAt) < googleVoiceSMSFreshWindow
}

// googleVoiceSMSConnected is the shared answer to "can FlipAi text right now".
func googleVoiceSMSConnected(s GoogleVoiceSMSRuntimeState) bool {
	return s.Running && s.Connected && s.SignedIn && s.ListenerRunning && s.Ready && googleVoiceSMSProbeFresh(s)
}

var googleVoiceSMSRuntimeMu sync.Mutex

const googleVoiceSMSProfileDirName = "google-voice-sms-webview-profile"

func googleVoiceSMSRuntimePath(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-sms-runtime.json")
}

func googleVoiceSMSProfilePath(dataDir string) string {
	return filepath.Join(dataDir, googleVoiceSMSProfileDirName)
}

func loadGoogleVoiceSMSRuntime(dataDir string) GoogleVoiceSMSRuntimeState {
	googleVoiceSMSRuntimeMu.Lock()
	defer googleVoiceSMSRuntimeMu.Unlock()
	var s GoogleVoiceSMSRuntimeState
	if b, err := os.ReadFile(googleVoiceSMSRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	// A state file can describe a renderer that was alive without proving that
	// the Google Voice listener is still executing, so readiness is only ever
	// believed while a recent successful poll backs it up. The freshness source
	// is LastProbeAt, which the background inbox loop stamps on every poll.
	//
	// This used to key off LastObserverAt, which belonged to the retired DOM
	// observer and is no longer written by anything: readiness could therefore
	// never become true, so the Connections card stayed "Not connected", Test
	// always timed out, and every reply failed with "background connection is
	// not ready" even though the listener was working.
	if !s.Running || !googleVoiceSMSProbeFresh(s) {
		s.ListenerRunning = false
		s.Ready = false
		if !s.Running {
			s.SignedIn = false
		}
	}
	return s
}

func mutateGoogleVoiceSMSRuntime(dataDir string, fn func(*GoogleVoiceSMSRuntimeState)) {
	googleVoiceSMSRuntimeMu.Lock()
	defer googleVoiceSMSRuntimeMu.Unlock()
	var s GoogleVoiceSMSRuntimeState
	path := googleVoiceSMSRuntimePath(dataDir)
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	fn(&s)
	s.UpdatedAt = time.Now()
	_ = os.MkdirAll(dataDir, 0700)
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0600) == nil {
			_ = os.Rename(tmp, path)
		}
	}
}
