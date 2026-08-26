//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// voiceSignOutFlagPath marks a pending Google sign-out. The browser profile
// belongs to the --google-voice process, so the deletion has to happen there,
// between one window and the next; the flag is how the request crosses
// processes.
func voiceSignOutFlagPath(dataDir string) string {
	return filepath.Join(dataDir, "google-voice-signout")
}

// platformSignOutGoogleVoice forgets the Google account the Google Voice
// window is signed in to by deleting its browser profile. The window is closed
// first so the profile is not held open, and the window process recreates a
// fresh, signed-out window afterwards when calling is on.
func platformSignOutGoogleVoice(dataDir string) error {
	flag := voiceSignOutFlagPath(dataDir)
	if err := os.WriteFile(flag, []byte(time.Now().Format(time.RFC3339)), 0600); err != nil {
		return err
	}
	if !googleVoiceProcessAlive() {
		// Nothing holds the profile; it can be removed right here.
		_ = os.Remove(flag)
		if err := removeVoiceProfile(dataDir); err != nil {
			return err
		}
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.SignedIn = false
			s.LastEvent = "signed-out"
		})
		return nil
	}
	if h := googleVoiceHWND(); h != 0 {
		procVoicePostMessage.Call(h, voiceWMClose, 0, 0)
	}
	// The window process consumes the flag after its window closes: it deletes
	// the profile and starts a fresh window. Waiting for the flag to disappear
	// is waiting for the sign-out to actually have happened.
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(flag); os.IsNotExist(err) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return errors.New("the Google Voice window did not complete the sign-out; quit FlipAi from the tray, start it again, and retry")
}

// consumePendingVoiceSignOut runs in the window process between windows: if a
// sign-out was requested, the profile is deleted now, while no browser holds
// it.
func consumePendingVoiceSignOut(dataDir string) {
	flag := voiceSignOutFlagPath(dataDir)
	if _, err := os.Stat(flag); err != nil {
		return
	}
	if err := removeVoiceProfile(dataDir); err != nil {
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.LastError = "Sign-out could not remove the saved Google session: " + err.Error()
		})
	} else {
		mutateVoiceRuntime(dataDir, func(s *VoiceRuntimeState) {
			s.SignedIn = false
			s.LastError = ""
			s.LastEvent = "signed-out"
		})
	}
	_ = os.Remove(flag)
}

// removeVoiceProfile deletes the Google Voice browser profile, retrying while
// the browser processes that held it finish exiting.
func removeVoiceProfile(dataDir string) error {
	var err error
	deadline := time.Now().Add(15 * time.Second)
	for {
		err = os.RemoveAll(voiceProfilePath(dataDir))
		if err == nil {
			return nil
		}
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
