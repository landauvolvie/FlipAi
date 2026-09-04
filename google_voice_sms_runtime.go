package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type GoogleVoiceSMSRuntimeState struct {
	ListenerRunning bool      `json:"listenerRunning"`
	Ready           bool      `json:"ready"`
	Page            string    `json:"page,omitempty"`
	LastError       string    `json:"lastError,omitempty"`
	LastProbeAt     time.Time `json:"lastProbeAt,omitempty"`
	LastInboundAt   time.Time `json:"lastInboundAt,omitempty"`
	LastOutboundAt  time.Time `json:"lastOutboundAt,omitempty"`
}

var googleVoiceSMSRuntimeMu sync.Mutex

func googleVoiceSMSRuntimePath(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-sms-runtime.json")
}

func loadGoogleVoiceSMSRuntime(dataDir string) GoogleVoiceSMSRuntimeState {
	googleVoiceSMSRuntimeMu.Lock()
	defer googleVoiceSMSRuntimeMu.Unlock()
	var s GoogleVoiceSMSRuntimeState
	if b, err := os.ReadFile(googleVoiceSMSRuntimePath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &s)
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
	_ = os.MkdirAll(dataDir, 0700)
	if b, err := json.MarshalIndent(s, "", "  "); err == nil {
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0600) == nil {
			_ = os.Rename(tmp, path)
		}
	}
}
