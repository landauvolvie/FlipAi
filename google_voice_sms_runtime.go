package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type GoogleVoiceSMSRuntimeState struct {
	Running         bool      `json:"running"`
	Starting        bool      `json:"starting,omitempty"`
	Visible         bool      `json:"visible"`
	LoginActive     bool      `json:"loginActive,omitempty"`
	Connected       bool      `json:"connected,omitempty"`
	SignedIn        bool      `json:"signedIn"`
	ListenerRunning bool      `json:"listenerRunning"`
	Ready           bool      `json:"ready"`
	Page            string    `json:"page,omitempty"`
	LastEvent       string    `json:"lastEvent,omitempty"`
	LastError       string    `json:"lastError,omitempty"`
	LastProbeAt     time.Time `json:"lastProbeAt,omitempty"`
	LastInboundAt   time.Time `json:"lastInboundAt,omitempty"`
	LastOutboundAt  time.Time `json:"lastOutboundAt,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt,omitempty"`
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
	// Older v0.46.34 state described the retired shared-profile listener. Never
	// turn that into a connected state for the new independent SMS profile.
	if !s.Running {
		s.ListenerRunning = false
		s.Ready = false
		s.SignedIn = false
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
