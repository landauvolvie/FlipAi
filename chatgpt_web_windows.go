//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

const (
	chatGPTWSExToolWindow = 0x00000080
	chatGPTSWRestore      = 9
	chatGPTSWShow         = 5
	chatGPTSWPNoActivate  = 0x0010
	chatGPTSWPShowWindow  = 0x0040
	chatGPTWMClose        = 0x0010
)

var (
	chatGPTUser32            = syscall.NewLazyDLL("user32.dll")
	chatGPTSetWindowPos      = chatGPTUser32.NewProc("SetWindowPos")
	chatGPTShowWindow        = chatGPTUser32.NewProc("ShowWindow")
	chatGPTSetForeground     = chatGPTUser32.NewProc("SetForegroundWindow")
	chatGPTGetSystemMetrics  = chatGPTUser32.NewProc("GetSystemMetrics")
	chatGPTPostMessage       = chatGPTUser32.NewProc("PostMessageW")
)

func init() {
	// The tray is guaranteed to live in the signed-in interactive desktop.
	// Start the request worker there, rather than in the background host, so
	// Connect remains visible even when FlipAi itself was started in Session 0.
	if len(os.Args) > 1 && os.Args[1] == "--tray" {
		go func() {
			time.Sleep(350 * time.Millisecond)
			dataDir, _, _, _, err := appPaths()
			if err == nil {
				runChatGPTWebDesktopWorker(dataDir)
			}
		}()
	}
}

func requestChatGPTWebDesktopAction(dataDir, action string) error {
	return writeChatGPTDesktopRequest(dataDir, action)
}

type chatGPTPageState struct {
	Running        bool   `json:"running"`
	SignedIn       bool   `json:"signedIn"`
	ComposerReady  bool   `json:"composerReady"`
	Href           string `json:"href"`
	ConversationID string `json:"conversationId"`
}

type chatGPTPageReply struct {
	TurnID         string `json:"turnId"`
	Text           string `json:"text"`
	Capture        string `json:"capture"`
	Href           string `json:"href"`
	ConversationID string `json:"conversationId"`
}

type chatGPTPageSubmitted struct {
	TurnID         string `json:"turnId"`
	Href           string `json:"href"`
	ConversationID string `json:"conversationId"`
}

type chatGPTPageError struct {
	TurnID string `json:"turnId"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type chatGPTPendingTurn struct {
	Started time.Time
	Reply   chan chatGPTPageReply
	Err     chan error
}

func runChatGPTWebDesktopWorker(dataDir string) {
	var launchMu sync.Mutex
	for {
		if quitRequested(dataDir) {
			return
		}
		action := takeChatGPTDesktopRequest(dataDir)
		if action == "" {
			time.Sleep(250 * time.Millisecond)
			continue
		}
		client := newChatGPTWebClient(dataDir, filepath.Join(dataDir, "state.json"))
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		s := client.Status(ctx)
		cancel()
		switch action {
		case "shutdown":
			if s.Running {
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				_ = client.runtimeRequest(ctx, http.MethodPost, "/shutdown", map[string]any{}, nil)
				cancel()
			}
		case "show":
			if s.Running {
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				_ = client.runtimeRequest(ctx, http.MethodPost, "/show", map[string]any{}, nil)
				cancel()
				continue
			}
			launchMu.Lock()
			go func() {
				defer launchMu.Unlock()
				_ = runChatGPTWebWindow(dataDir, true)
			}()
		case "background":
			if s.Running {
				continue
			}
			launchMu.Lock()
			go func() {
				defer launchMu.Unlock()
				_ = runChatGPTWebWindow(dataDir, false)
			}()
		}
	}
}

func showChatGPTWindow(hwnd uintptr) {
	width, height := uintptr(920), uintptr(760)
	sw, _, _ := chatGPTGetSystemMetrics.Call(0)
	sh, _, _ := chatGPTGetSystemMetrics.Call(1)
	x, y := uintptr(80), uintptr(80)
	if sw > width {
		x = (sw - width) / 2
	}
	if sh > height {
		y = (sh - height) / 2
	}
	chatGPTSetWindowPos.Call(hwnd, 0, x, y, width, height, chatGPTSWPShowWindow)
	chatGPTShowWindow.Call(hwnd, chatGPTSWRestore)
	chatGPTShowWindow.Call(hwnd, chatGPTSWShow)
	chatGPTSetForeground.Call(hwnd)
}

func parkChatGPTWindow(hwnd uintptr) {
	chatGPTSetWindowPos.Call(hwnd, 0, uintptr(^uint32(31999)), uintptr(^uint32(31999)), 720, 720, chatGPTSWPNoActivate|chatGPTSWPShowWindow)
}

func runChatGPTWebWindow(dataDir string, show bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	profile := chatGPTWebProfilePath(dataDir)
	if err := os.MkdirAll(profile, 0700); err != nil {
		return err
	}
	activity := activityLogForStatePath(filepath.Join(dataDir, "state.json"))
	options := webview2.WebViewOptions{
		Debug:      false,
		DataPath:   profile,
		AutoFocus:  show,
		WindowOptions: webview2.WindowOptions{
			Title:      "FlipAi — ChatGPT sign in",
			Width:      920,
			Height:     760,
			Center:     show,
			ExStyle:    chatGPTWSExToolWindow,
			NoActivate: !show,
		},
	}
	if !show {
		options.WindowOptions.Position = true
		options.WindowOptions.X = -32000
		options.WindowOptions.Y = -32000
	}
	w := webview2.NewWithOptions(options)
	if w == nil {
		err := errors.New("Microsoft Edge WebView2 Runtime could not create the ChatGPT view")
		activity.Add("error", "chatgpt", err.Error(), "", "G", "")
		return err
	}
	defer w.Destroy()
	hwnd := uintptr(w.Window())
	applyFlipAiWindowIcon(hwnd)
	w.SetSize(720, 600, webview2.HintMin)

	token, err := secureRandomToken(24)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	var stateMu sync.Mutex
	state := chatGPTWebRuntime{Running: true, Port: port, ControlToken: token, LastEvent: "webview-starting"}
	persist := func() {
		stateMu.Lock()
		copy := state
		stateMu.Unlock()
		_ = saveChatGPTWebRuntime(dataDir, copy)
	}
	update := func(fn func(*chatGPTWebRuntime)) {
		stateMu.Lock()
		fn(&state)
		copy := state
		stateMu.Unlock()
		_ = saveChatGPTWebRuntime(dataDir, copy)
	}
	persist()

	var pendingMu sync.Mutex
	pending := map[string]*chatGPTPendingTurn{}
	var signedInOnce bool

	_ = w.Bind("flipChatGPTState", func(s chatGPTPageState) {
		becameSignedIn := false
		update(func(rt *chatGPTWebRuntime) {
			rt.Running = true
			rt.SignedIn = s.SignedIn
			rt.ComposerReady = s.ComposerReady
			rt.CurrentURL = s.Href
			if s.ConversationID != "" {
				rt.ConversationID = s.ConversationID
			}
			if s.SignedIn {
				rt.LastError = ""
				rt.LastEvent = "signed-in"
			}
		})
		if s.SignedIn && !signedInOnce {
			signedInOnce = true
			becameSignedIn = true
		}
		if becameSignedIn {
			activity.Add("success", "chatgpt", "ChatGPT signed in successfully; the private WebView session is ready", "", "G", "")
			go func() {
				time.Sleep(900 * time.Millisecond)
				w.Dispatch(func() { parkChatGPTWindow(hwnd) })
			}()
		}
	})
	_ = w.Bind("flipChatGPTSubmitted", func(s chatGPTPageSubmitted) {
		pendingMu.Lock()
		p := pending[s.TurnID]
		pendingMu.Unlock()
		if p != nil {
			activity.AddTimed("info", "chatgpt", "ChatGPT prompt submitted inside the background WebView", "", "G", "", time.Since(p.Started))
		}
		update(func(rt *chatGPTWebRuntime) {
			rt.LastEvent = "turn-submitted"
			if s.ConversationID != "" {
				rt.ConversationID = s.ConversationID
			}
		})
	})
	_ = w.Bind("flipChatGPTReply", func(r chatGPTPageReply) {
		pendingMu.Lock()
		p := pending[r.TurnID]
		if p != nil {
			delete(pending, r.TurnID)
		}
		pendingMu.Unlock()
		if p == nil || strings.TrimSpace(r.Text) == "" {
			return
		}
		if r.ConversationID == "" {
			r.ConversationID = chatGPTConversationIDFromURL(r.Href)
		}
		select {
		case p.Reply <- r:
		default:
		}
	})
	_ = w.Bind("flipChatGPTError", func(e chatGPTPageError) {
		msg := strings.TrimSpace(e.Detail)
		if msg == "" {
			msg = strings.TrimSpace(e.Code)
		}
		if msg == "" {
			msg = "ChatGPT page automation failed"
		}
		pendingMu.Lock()
		p := pending[e.TurnID]
		if p != nil {
			delete(pending, e.TurnID)
		}
		pendingMu.Unlock()
		if p != nil {
			select {
			case p.Err <- errors.New(msg):
			default:
			}
		}
		update(func(rt *chatGPTWebRuntime) { rt.LastError = truncate(msg, 220); rt.LastEvent = "turn-error" })
		activity.Add("error", "chatgpt", "ChatGPT page error: "+truncate(msg, 220), "", "G", "")
	})
	w.Init(chatGPTWebInitScript)

	auth := func(r *http.Request) bool {
		return subtleConstantStringEqual(r.Header.Get("X-FlipAi-Token"), token)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(rw http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		stateMu.Lock()
		s := state
		stateMu.Unlock()
		writeJSON(rw, chatGPTWebStatus{Running: true, SignedIn: s.SignedIn, ComposerReady: s.ComposerReady, CurrentURL: s.CurrentURL, ConversationID: s.ConversationID, LastCapture: s.LastCapture, LastDurationMS: s.LastDurationMS, LastError: s.LastError, LastEvent: s.LastEvent})
	})
	mux.HandleFunc("/show", func(rw http.ResponseWriter, r *http.Request) {
		if !auth(r) || r.Method != http.MethodPost {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		w.Dispatch(func() { showChatGPTWindow(hwnd) })
		activity.Add("info", "chatgpt", "ChatGPT sign-in/session window shown by explicit user request", "", "G", "")
		writeJSON(rw, map[string]any{"ok": true})
	})
	mux.HandleFunc("/chat", func(rw http.ResponseWriter, r *http.Request) {
		if !auth(r) || r.Method != http.MethodPost {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		var req chatGPTWebTurnRequest
		if err := json.NewDecoder(http.MaxBytesReader(rw, r.Body, 256<<10)).Decode(&req); err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			writeJSON(rw, map[string]any{"error": "invalid ChatGPT turn request"})
			return
		}
		if strings.TrimSpace(req.Text) == "" {
			rw.WriteHeader(http.StatusBadRequest)
			writeJSON(rw, map[string]any{"error": "empty ChatGPT message"})
			return
		}
		stateMu.Lock()
		signedIn := state.SignedIn && state.ComposerReady
		stateMu.Unlock()
		if !signedIn {
			rw.WriteHeader(http.StatusConflict)
			writeJSON(rw, map[string]any{"error": "ChatGPT is not signed in in FlipAi yet"})
			return
		}
		turnID, _ := secureRandomToken(12)
		p := &chatGPTPendingTurn{Started: time.Now(), Reply: make(chan chatGPTPageReply, 1), Err: make(chan error, 1)}
		pendingMu.Lock()
		pending[turnID] = p
		pendingMu.Unlock()
		defer func() {
			pendingMu.Lock()
			delete(pending, turnID)
			pendingMu.Unlock()
		}()

		target := chatGPTWebURL
		if !req.New && strings.TrimSpace(req.ConversationID) != "" {
			target = chatGPTWebURL + "c/" + urlPathEscape(req.ConversationID)
		}
		textJSON, _ := json.Marshal(req.Text)
		turnJSON, _ := json.Marshal(turnID)
		w.Dispatch(func() {
			stateMu.Lock()
			current := state.CurrentURL
			stateMu.Unlock()
			if req.New || (req.ConversationID != "" && chatGPTConversationIDFromURL(current) != req.ConversationID) {
				w.Navigate(target)
			}
			w.Eval("setTimeout(function(){ if(window.__flipAiChatGPTSubmit){ window.__flipAiChatGPTSubmit(" + string(turnJSON) + "," + string(textJSON) + "); } }, 350)")
		})

		select {
		case reply := <-p.Reply:
			cid := reply.ConversationID
			if cid == "" {
				deadline := time.Now().Add(2200 * time.Millisecond)
				for time.Now().Before(deadline) {
					stateMu.Lock()
					cid = state.ConversationID
					stateMu.Unlock()
					if cid != "" {
						break
					}
					time.Sleep(100 * time.Millisecond)
				}
			}
			d := time.Since(p.Started)
			update(func(rt *chatGPTWebRuntime) {
				rt.LastCapture = reply.Capture
				rt.LastDurationMS = d.Milliseconds()
				rt.LastError = ""
				rt.LastEvent = "reply-captured"
				if cid != "" {
					rt.ConversationID = cid
				}
			})
			activity.AddTimed("success", "chatgpt", "ChatGPT assistant reply captured via "+safeCaptureLabel(reply.Capture), "", "G", "", d)
			writeJSON(rw, chatGPTWebTurnResult{Reply: reply.Text, ConversationID: cid, Capture: safeCaptureLabel(reply.Capture), DurationMS: d.Milliseconds()})
		case err := <-p.Err:
			rw.WriteHeader(http.StatusBadGateway)
			writeJSON(rw, map[string]any{"error": truncate(err.Error(), 240)})
		case <-r.Context().Done():
			rw.WriteHeader(http.StatusGatewayTimeout)
			writeJSON(rw, map[string]any{"error": "ChatGPT turn timed out while waiting for the assistant reply"})
		}
	})
	mux.HandleFunc("/shutdown", func(rw http.ResponseWriter, r *http.Request) {
		if !auth(r) || r.Method != http.MethodPost {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		writeJSON(rw, map[string]any{"ok": true})
		go func() {
			time.Sleep(80 * time.Millisecond)
			chatGPTPostMessage.Call(hwnd, chatGPTWMClose, 0, 0)
		}()
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 15 * time.Minute, IdleTimeout: 90 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	activity.Add("info", "chatgpt", "Private ChatGPT WebView started with its own persistent browser profile", "", "G", "")
	if show {
		activity.Add("info", "chatgpt", "ChatGPT sign-in window opened", "", "G", "")
	} else {
		parkChatGPTWindow(hwnd)
	}

	stopWatch := make(chan struct{})
	go func() {
		t := time.NewTicker(400 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case <-t.C:
				if quitRequested(dataDir) {
					chatGPTPostMessage.Call(hwnd, chatGPTWMClose, 0, 0)
					return
				}
			}
		}
	}()

	w.Navigate(chatGPTWebURL)
	w.Run()
	close(stopWatch)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = srv.Shutdown(ctx)
	cancel()
	update(func(rt *chatGPTWebRuntime) {
		rt.Running = false
		rt.Port = 0
		rt.ControlToken = ""
		rt.ComposerReady = false
		rt.SignedIn = false
		rt.LastEvent = "webview-closed"
	})
	activity.Add("info", "chatgpt", "ChatGPT WebView closed; its private signed-in profile remains available for the next start", "", "G", "")
	return nil
}

func subtleConstantStringEqual(a, b string) bool {
	if len(a) != len(b) || a == "" {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func urlPathEscape(v string) string {
	// Conversation IDs are opaque but normally URL-safe UUID-ish strings. Keep
	// only the unreserved set so no caller can turn the local endpoint into a
	// navigation to another origin.
	var b strings.Builder
	for _, r := range strings.TrimSpace(v) {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == '~' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func safeCaptureLabel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "network":
		return "network"
	case "network-xhr":
		return "network-xhr"
	case "dom":
		return "dom"
	default:
		return "unknown"
	}
}

// Keep unsafe referenced in the Windows build: the WebView handle is an
// unsafe.Pointer by contract and the explicit conversion documents that this
// file is the only place we turn it into a Win32 HWND.
var _ unsafe.Pointer
var _ = fmt.Sprintf
