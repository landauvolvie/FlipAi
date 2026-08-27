package main

import (
	"encoding/json"
	"errors"
	"fmt"
)

// The Google Voice page is FlipAi's own WebView2, and this is the second, narrow
// way into it: the DevTools protocol, reached in-process through WebView2's own
// CallDevToolsProtocolMethod.
//
// It exists for three things a page script cannot do for itself:
//
//   - press Answer with a real mouse event. A ringing card is answered by a
//     script click first, because that is instant; when Google Voice ignores
//     one -- and it does, on a card it is still animating in -- a genuine
//     pointer press delivered through the browser's own input pipeline is the
//     next rung of the ladder, and it carries the user activation a page is
//     entitled to demand before it opens a microphone and plays a call.
//   - watch the call from outside the page's own script. A script that has
//     wedged can no longer say that a call ended, and a call that ends
//     unnoticed leaves the desktop app talking to itself.
//   - attach an image to an outgoing message. A file input cannot be filled in
//     by a page.
//
// No port is opened and nothing listens: this is the same process talking to
// its own browser view. An earlier version used a loopback DevTools port
// instead, which the WebView2 runtime turns out to ignore -- so the channel
// silently did not exist, and with it the second way of pressing Answer and
// the ability to send an image.

// voiceDevTools is one DevTools conversation with the Google Voice page.
type voiceDevTools interface {
	// Call runs one DevTools method and decodes its reply into out.
	Call(method string, params any, out any) error
}

type voiceDevToolsEval struct {
	Result struct {
		Value       json.RawMessage `json:"value"`
		Type        string          `json:"type"`
		Subtype     string          `json:"subtype,omitempty"`
		ObjectID    string          `json:"objectId,omitempty"`
		Description string          `json:"description,omitempty"`
	} `json:"result"`
	ExceptionDetails json.RawMessage `json:"exceptionDetails,omitempty"`
}

// voiceEval runs an expression in the page and decodes its value.
func voiceEval(d voiceDevTools, expression string, awaitPromise bool, out any) error {
	if d == nil {
		return errNoVoiceControlChannel
	}
	var got voiceDevToolsEval
	params := map[string]any{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  awaitPromise,
	}
	if err := d.Call("Runtime.evaluate", params, &got); err != nil {
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

// voiceEvalObject runs an expression and returns a handle to the object it
// produced, for the DevTools methods that take one.
func voiceEvalObject(d voiceDevTools, expression string) (string, error) {
	if d == nil {
		return "", errNoVoiceControlChannel
	}
	var got voiceDevToolsEval
	params := map[string]any{
		"expression":    expression,
		"returnByValue": false,
		"awaitPromise":  false,
	}
	if err := d.Call("Runtime.evaluate", params, &got); err != nil {
		return "", err
	}
	if len(got.ExceptionDetails) > 0 && string(got.ExceptionDetails) != "null" {
		return "", errors.New("the Google Voice page script failed")
	}
	if got.Result.ObjectID == "" {
		return "", fmt.Errorf("the Google Voice page returned %s rather than an element", got.Result.Type)
	}
	return got.Result.ObjectID, nil
}

// voicePageSnapshot is what the control channel can see of the call, read
// without the page script's cooperation.
type voicePageSnapshot struct {
	Href     string `json:"href"`
	SignedIn bool   `json:"signedIn"`
	Answer   bool   `json:"answer"`
	Hangup   bool   `json:"hangup"`
	// CallControls is the weaker second opinion: a call in progress offers mute
	// and a keypad. Google Voice's ordinary page offers both too, so this may
	// only keep a known call alive, never start one. See voice_page_probe.go.
	CallControls bool               `json:"callControls"`
	Caller       string             `json:"caller"`
	Label        string             `json:"label"`
	Controls     []string           `json:"controls"`
	Devices      []VoiceAudioDevice `json:"devices"`
}

func voiceReadSnapshot(d voiceDevTools) (voicePageSnapshot, error) {
	var out voicePageSnapshot
	err := voiceEval(d, voicePageSnapshotJS, true, &out)
	return out, err
}

// observation turns one snapshot into what the call machine understands.
func (s voicePageSnapshot) observation() voiceObservation {
	return voiceObservation{
		Answer:  s.Answer,
		InCall:  s.Hangup,
		Sustain: s.CallControls,
		Caller:  s.Caller,
		Label:   s.Label,
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

// voiceClickAnswerScripted is the cheap rung: the page's own click handler.
func voiceClickAnswerScripted(d voiceDevTools) (bool, error) {
	var clicked bool
	err := voiceEval(d, voiceClickAnswerJS, false, &clicked)
	return clicked, err
}

// voiceClickAnswerTrusted is the forceful rung: a real pointer press delivered
// through the browser's own input pipeline, at the middle of the Answer
// control. Unlike a scripted click it carries user activation.
func voiceClickAnswerTrusted(d voiceDevTools) (bool, error) {
	if d == nil {
		return false, errNoVoiceControlChannel
	}
	var at answerPoint
	if err := voiceEval(d, voiceAnswerPointJS, false, &at); err != nil {
		return false, err
	}
	if !at.Found {
		return false, nil
	}
	if err := d.Call("Input.dispatchMouseEvent", map[string]any{
		"type": "mouseMoved", "x": at.X, "y": at.Y, "button": "none", "buttons": 0,
	}, nil); err != nil {
		return false, err
	}
	press := func(kind string, buttons int) error {
		return d.Call("Input.dispatchMouseEvent", map[string]any{
			"type":       kind,
			"x":          at.X,
			"y":          at.Y,
			"button":     "left",
			"buttons":    buttons,
			"clickCount": 1,
		}, nil)
	}
	if err := press("mousePressed", 1); err != nil {
		return false, err
	}
	if err := press("mouseReleased", 0); err != nil {
		return false, err
	}
	return true, nil
}

var errNoVoiceControlChannel = errors.New("the Google Voice window has no control channel open")
