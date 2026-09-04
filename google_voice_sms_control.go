package main

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const googleVoiceSMSControlListen = "127.0.0.1:8772"

func init() {
	if len(os.Args) < 2 || os.Args[1] != "--host" {
		return
	}
	dataDir, cfgPath, statePath, _, err := appPaths()
	if err != nil {
		return
	}
	go startGoogleVoiceSMSControlServer(dataDir, cfgPath, statePath)
}

func googleVoiceSMSLocalOrigin(r *http.Request) bool {
	origin := strings.ToLower(strings.TrimSpace(r.Header.Get("Origin")))
	return strings.HasPrefix(origin, "http://127.0.0.1:") || strings.HasPrefix(origin, "http://localhost:")
}

func startGoogleVoiceSMSControlServer(dataDir, cfgPath, statePath string) {
	listener, err := net.Listen("tcp", googleVoiceSMSControlListen)
	if err != nil {
		log.Printf("Google Voice SMS control service: %v", err)
		return
	}
	mux := http.NewServeMux()
	withLocalUI := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && googleVoiceSMSLocalOrigin(r) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			}
			if r.Method == http.MethodOptions {
				if !googleVoiceSMSLocalOrigin(r) {
					http.Error(w, "local FlipAi UI required", http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if origin != "" && !googleVoiceSMSLocalOrigin(r) {
				http.Error(w, "local FlipAi UI required", http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
	writeStatus := func(w http.ResponseWriter, selected bool) {
		rt := loadVoiceRuntime(dataDir)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"selected":       selected,
			"browserRunning": rt.BrowserRunning,
			"signedIn":       rt.SignedIn,
			"page":           rt.Page,
			"lastError":      rt.LastError,
		})
	}
	selected := func() bool {
		cfg, err := loadConfig(cfgPath, dataDir)
		return err == nil && cfg.Gmail.Method == GmailMethodGoogleVoice
	}
	mux.HandleFunc("/status", withLocalUI(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeStatus(w, selected())
	}))
	mux.HandleFunc("/connect", withLocalUI(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		rt := loadVoiceRuntime(dataDir)
		if !rt.BrowserRunning {
			_ = platformOpenGoogleVoice(dataDir, false)
			deadline := time.Now().Add(8 * time.Second)
			for time.Now().Before(deadline) {
				rt = loadVoiceRuntime(dataDir)
				if rt.BrowserRunning {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
		}
		if !rt.SignedIn {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "message": "Sign in to Google Voice in the panel on this page, then press Connect again."})
			return
		}
		cfg, err := loadConfig(cfgPath, dataDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cfg.Gmail.Method = GmailMethodGoogleVoice
		cfg.Gmail.Email = ""
		if err := saveConfig(cfgPath, cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resetGoogleVoiceSMSCheckpoint(statePath)
		// The direct listener is its own Messages WebView. Recreate the Google
		// Voice process after selecting this transport so that listener exists
		// immediately, including when the user switched from Gmail without
		// restarting Windows or signing in again.
		platformRestartGoogleVoice(dataDir)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "Google Voice SMS connected. FlipAi is starting the dedicated message listener."})
	}))
	mux.HandleFunc("/disconnect", withLocalUI(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		cfg, err := loadConfig(cfgPath, dataDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if cfg.Gmail.Method == GmailMethodGoogleVoice {
			cfg.Gmail.Method = ""
			if err := saveConfig(cfgPath, cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			resetGoogleVoiceSMSCheckpoint(statePath)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("Google Voice SMS control service stopped: %v", err)
	}
}

func resetGoogleVoiceSMSCheckpoint(statePath string) {
	st := loadState(statePath)
	st.GmailBaselineUnix = 0
	st.ProcessedMessageIDs = nil
	st.LastMessageID = ""
	_ = saveState(statePath, st)
}
