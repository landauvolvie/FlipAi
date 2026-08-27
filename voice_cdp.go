package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// The Google Voice window is FlipAi's own WebView2, not a browser FlipAi drives
// from outside. This file is the second, narrow way into that window: a
// loopback-only DevTools channel the WebView2 runtime opens for FlipAi and
// nobody else.
//
// It exists for two things the page's own script cannot do:
//
//   - press Answer with a real mouse event. A ringing card is answered by a
//     script click first, because that is instant; when Google Voice ignores
//     one -- and it does, on a card it is still animating in -- a genuine
//     pointer press delivered through the browser's input pipeline is the next
//     rung of the ladder, and it carries the user activation that a media call
//     sometimes insists on.
//   - watch the call from outside the page. A page script that has wedged,
//     been replaced by a navigation, or lost its bindings can no longer say
//     that a call ended, and a call that ends unnoticed leaves the desktop app
//     talking to itself. The observation this channel produces is the
//     independent check on that.
//
// The port is chosen at random, bound to 127.0.0.1, and never leaves this
// machine. It is written to the runtime state file so the FlipAi host can use
// the same channel to send a Google Voice MMS.

const voiceCDPDialTimeout = 4 * time.Second

// voiceCDPArguments are the WebView2 browser arguments that open the channel.
// --remote-allow-origins is required by current Chromium builds for a
// WebSocket attach; the dialer sends no Origin header, but browsers have
// tightened this twice and a refused attach is a silent loss of the answer
// ladder.
func voiceCDPArguments(port int) string {
	if port <= 0 {
		return ""
	}
	return " --remote-debugging-address=127.0.0.1 --remote-debugging-port=" + strconv.Itoa(port) +
		" --remote-allow-origins=*"
}

func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

type voiceCDPTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func voiceCDPHTTPJSON(port int, path string, out any) error {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the Google Voice control channel returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// voiceCDPPageTarget finds the Google Voice page. A sign-in page counts: it is
// the same window, and reading it is how FlipAi knows it is not signed in.
func voiceCDPPageTarget(port int) (voiceCDPTarget, error) {
	var targets []voiceCDPTarget
	if err := voiceCDPHTTPJSON(port, "/json/list", &targets); err != nil {
		return voiceCDPTarget{}, err
	}
	var fallback voiceCDPTarget
	for _, t := range targets {
		if t.Type != "page" || t.WebSocketDebuggerURL == "" {
			continue
		}
		u := strings.ToLower(t.URL)
		if strings.Contains(u, "voice.google.com") {
			return t, nil
		}
		if strings.Contains(u, "accounts.google.com") || strings.Contains(u, "google.com") {
			fallback = t
		}
	}
	if fallback.WebSocketDebuggerURL != "" {
		return fallback, nil
	}
	return voiceCDPTarget{}, errors.New("the Google Voice page is not ready yet")
}

type voiceCDPClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
	next int64
}

type voiceCDPEnvelope struct {
	ID     int64           `json:"id,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func dialVoiceCDP(raw string) (*voiceCDPClient, error) {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = voiceCDPDialTimeout
	conn, _, err := dialer.Dial(raw, nil)
	if err != nil {
		return nil, err
	}
	return &voiceCDPClient{conn: conn}, nil
}

// connectVoiceCDP opens the channel to the Google Voice page on this port.
func connectVoiceCDP(port int) (*voiceCDPClient, error) {
	if port <= 0 {
		return nil, errors.New("the Google Voice control channel is not open")
	}
	target, err := voiceCDPPageTarget(port)
	if err != nil {
		return nil, err
	}
	return dialVoiceCDP(target.WebSocketDebuggerURL)
}

func (c *voiceCDPClient) close() {
	if c != nil && c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *voiceCDPClient) call(method string, params any, out any) error {
	if c == nil || c.conn == nil {
		return errors.New("the Google Voice control channel is closed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	req := map[string]any{"id": id, "method": method, "params": params}
	_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	if err := c.conn.WriteJSON(req); err != nil {
		return err
	}
	for {
		_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return err
		}
		var msg voiceCDPEnvelope
		if json.Unmarshal(raw, &msg) != nil || msg.ID != id {
			continue
		}
		if msg.Error != nil {
			return fmt.Errorf("%s (%d)", msg.Error.Message, msg.Error.Code)
		}
		if out != nil && len(msg.Result) > 0 {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	}
}

type voiceCDPEval struct {
	Result struct {
		Value json.RawMessage `json:"value"`
	} `json:"result"`
	ExceptionDetails json.RawMessage `json:"exceptionDetails,omitempty"`
}

func (c *voiceCDPClient) eval(expression string, awaitPromise bool, out any) error {
	var got voiceCDPEval
	params := map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  awaitPromise,
	}
	if err := c.call("Runtime.evaluate", params, &got); err != nil {
		return err
	}
	if len(got.ExceptionDetails) > 0 && string(got.ExceptionDetails) != "null" {
		return errors.New("the Google Voice page script failed")
	}
	if out == nil {
		return nil
	}
	if len(got.Result.Value) == 0 {
		return errors.New("the Google Voice page returned no value")
	}
	return json.Unmarshal(got.Result.Value, out)
}

// voicePageSnapshot is what the control channel can see of the call, read
// without the page script's cooperation.
type voicePageSnapshot struct {
	Href     string             `json:"href"`
	SignedIn bool               `json:"signedIn"`
	Answer   bool               `json:"answer"`
	Hangup   bool               `json:"hangup"`
	Caller   string             `json:"caller"`
	Label    string             `json:"label"`
	Controls []string           `json:"controls"`
	Devices  []VoiceAudioDevice `json:"devices"`
}

func (c *voiceCDPClient) voiceSnapshot() (voicePageSnapshot, error) {
	var out voicePageSnapshot
	err := c.eval(voicePageSnapshotJS, true, &out)
	return out, err
}

// observation turns one snapshot into what the call machine understands.
func (s voicePageSnapshot) observation() voiceObservation {
	return voiceObservation{
		Answer: s.Answer,
		InCall: s.Hangup,
		Caller: s.Caller,
		Label:  s.Label,
	}
}

// answerPoint is where on screen the Answer control is, in the page's own
// coordinates. It is read separately from the snapshot because it is only
// needed on the rung of the ladder that uses it.
type answerPoint struct {
	Found bool    `json:"found"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

// clickAnswerScripted is the cheap rung: the page's own click handler.
func (c *voiceCDPClient) clickAnswerScripted() (bool, error) {
	var clicked bool
	err := c.eval(voiceClickAnswerJS, false, &clicked)
	return clicked, err
}

// clickAnswerTrusted is the forceful rung: a real pointer press delivered
// through the browser's own input pipeline, at the middle of the Answer
// control. Unlike a scripted click it carries user activation, which is what a
// page is entitled to demand before it opens a microphone and starts playing a
// call.
func (c *voiceCDPClient) clickAnswerTrusted() (bool, error) {
	var at answerPoint
	if err := c.eval(voiceAnswerPointJS, false, &at); err != nil {
		return false, err
	}
	if !at.Found {
		return false, nil
	}
	press := func(kind string, buttons int) error {
		return c.call("Input.dispatchMouseEvent", map[string]any{
			"type":       kind,
			"x":          at.X,
			"y":          at.Y,
			"button":     "left",
			"buttons":    buttons,
			"clickCount": 1,
		}, nil)
	}
	if err := c.call("Input.dispatchMouseEvent", map[string]any{
		"type": "mouseMoved", "x": at.X, "y": at.Y, "button": "none", "buttons": 0,
	}, nil); err != nil {
		return false, err
	}
	if err := press("mousePressed", 1); err != nil {
		return false, err
	}
	if err := press("mouseReleased", 0); err != nil {
		return false, err
	}
	return true, nil
}

// voiceCDPPermissions grants, at the browser level, exactly the capabilities a
// phone call needs. The WebView2 permission callback below covers the prompt a
// page raises; this covers the capabilities a page checks before it ever
// prompts, which is what Google Voice does when it decides whether this browser
// can take calls at all.
func (c *voiceCDPClient) grantVoicePermissions() error {
	permissions := []map[string]any{
		{"name": "notifications"},
		{"name": "microphone"},
		{"name": "speaker-selection"},
	}
	var failed error
	for _, permission := range permissions {
		params := map[string]any{
			"permission": permission,
			"setting":    "granted",
			"origin":     googleVoiceWebURL,
		}
		if err := c.call("Browser.setPermission", params, nil); err != nil {
			// speaker-selection is only present on newer builds, and the
			// microphone grant already reveals output endpoints without it.
			if permission["name"] == "speaker-selection" {
				continue
			}
			failed = err
		}
	}
	return failed
}
