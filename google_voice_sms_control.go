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
	selected := func() bool {
		cfg, err := loadConfig(cfgPath, dataDir)
		return err == nil && cfg.Gmail.Method == GmailMethodGoogleVoice
	}
	writeStatus := func(w http.ResponseWriter, isSelected bool) {
		sms := loadGoogleVoiceSMSRuntime(dataDir)
		fresh := !sms.LastProbeAt.IsZero() && time.Since(sms.LastProbeAt) < 8*time.Second
		connected := isSelected && sms.Running && sms.Connected && sms.SignedIn && sms.ListenerRunning && sms.Ready && fresh
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"selected":        isSelected,
			"connected":       connected,
			"browserRunning":  sms.Running,
			"starting":        sms.Starting,
			"visible":         sms.Visible,
			"loginActive":     sms.LoginActive,
			"signedIn":        sms.SignedIn,
			"page":            sms.Page,
			"listenerRunning": sms.ListenerRunning,
			"listenerReady":   connected,
			"listenerPage":    sms.Page,
			"listenerError":   sms.LastError,
			"lastEvent":       sms.LastEvent,
			"lastProbeAt":     sms.LastProbeAt,
			"lastInboundAt":   sms.LastInboundAt,
			"lastOutboundAt":  sms.LastOutboundAt,
		})
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
		if err := googleVoiceSMSLoginForUI(dataDir); err != nil {
			mutateGoogleVoiceSMSRuntime(dataDir, func(s *GoogleVoiceSMSRuntimeState) {
				s.LastError = "Could not open the Google Voice SMS sign-in window: " + err.Error()
				s.LastEvent = "sign-in-window-error"
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "message": "Could not open the Google Voice SMS sign-in window: " + err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"message": "Google Voice SMS sign-in opened. Sign in in the separate window FlipAi opened; Connected will appear only after the Messages page is verified.",
		})
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
		}
		resetGoogleVoiceSMSCheckpoint(statePath)
		if err := platformDisconnectGoogleVoiceSMS(dataDir); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "message": "Google Voice SMS was deselected, but FlipAi could not remove its private SMS browser profile: " + err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "message": "Google Voice SMS disconnected. Its private SMS browser profile was removed; Google Voice calling was not touched."})
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
