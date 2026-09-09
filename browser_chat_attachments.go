package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	browserChatAttachmentMarkerStart = "[[FLIPAI_IMAGE_ATTACHMENTS:"
	browserChatAttachmentMarkerEnd   = "]]"
)

type browserChatAttachment struct {
	Path      string `json:"path"`
	Filename  string `json:"filename,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
}

var browserChatAttachmentTurnMu sync.Mutex

func isBrowserChatAgent(agent string) bool {
	switch strings.ToUpper(strings.TrimSpace(agent)) {
	case "G", "H", "M", "X", "P", "U":
		return true
	default:
		return false
	}
}

func parseBrowserChatAttachmentOnlyCommand(cfg Config, agent string, m GmailMessage) (remoteCommand, error) {
	agent = strings.ToUpper(strings.TrimSpace(agent))
	if !isBrowserChatAgent(agent) {
		return remoteCommand{}, errors.New("not a browser chat agent")
	}
	found := false
	for _, a := range m.Attachments {
		if len(a.Data) > 0 && supportedInboundMediaType(inboundMediaType(a.MediaType, a.Filename)) {
			found = true
			break
		}
	}
	if !found {
		return remoteCommand{}, fmt.Errorf("%s received no usable Google Voice media attachment", agentDisplayName(agent))
	}
	if agentSettings(cfg, agent).RequireCode {
		return remoteCommand{}, fmt.Errorf("attachment-only commands cannot supply the %s text security code; include a caption beginning with the code", agentDisplayName(agent))
	}
	return remoteCommand{Agent: agent}, nil
}

func preparedBrowserChatImages(in []InboundAttachment) ([]browserChatAttachment, error) {
	out := make([]browserChatAttachment, 0, len(in))
	for _, a := range in {
		mediaType := inboundMediaType(a.MediaType, a.Filename)
		if !supportedInboundMediaType(mediaType) {
			return nil, fmt.Errorf("browser chat does not relay attachment type %s (%s)", mediaType, a.Filename)
		}
		item := browserChatAttachment{Path: a.Path, Filename: a.Filename, MediaType: mediaType}
		if err := validatePreparedBrowserChatImage(item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil, errors.New("no usable media attachment was found")
	}
	if len(out) > maxInboundAttachmentCount {
		return nil, fmt.Errorf("too many media attachments: %d", len(out))
	}
	return out, nil
}

func validatePreparedBrowserChatImage(a browserChatAttachment) error {
	if !supportedInboundMediaType(a.MediaType) {
		return fmt.Errorf("attachment %q has unsupported media type %q", a.Filename, a.MediaType)
	}
	abs, err := filepath.Abs(strings.TrimSpace(a.Path))
	if err != nil || abs == "" {
		return errors.New("invalid prepared attachment path")
	}
	temp, err := filepath.Abs(os.TempDir())
	if err != nil {
		return errors.New("could not resolve the temporary attachment folder")
	}
	rel, err := filepath.Rel(temp, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("browser chat refused a file outside FlipAi's temporary attachment folder")
	}
	first := rel
	if i := strings.IndexRune(first, os.PathSeparator); i >= 0 {
		first = first[:i]
	}
	if !strings.HasPrefix(strings.ToLower(first), "flipai-inbound-") {
		return errors.New("browser chat refused a file that was not prepared by FlipAi")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("read prepared attachment: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("prepared attachment is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > maxInboundAttachmentBytes {
		return fmt.Errorf("attachment must be between 1 byte and %d MB", maxInboundAttachmentBytes>>20)
	}
	return nil
}

func browserChatAttachmentMarker(in []browserChatAttachment) (string, error) {
	if len(in) == 0 {
		return "", errors.New("no media attachment was supplied")
	}
	for _, a := range in {
		if err := validatePreparedBrowserChatImage(a); err != nil {
			return "", err
		}
	}
	b, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	return browserChatAttachmentMarkerStart + base64.RawURLEncoding.EncodeToString(b) + browserChatAttachmentMarkerEnd, nil
}

func extractBrowserChatAttachmentMarker(expression string) (string, []browserChatAttachment, bool, error) {
	start := strings.Index(expression, browserChatAttachmentMarkerStart)
	if start < 0 {
		return expression, nil, false, nil
	}
	payloadStart := start + len(browserChatAttachmentMarkerStart)
	relEnd := strings.Index(expression[payloadStart:], browserChatAttachmentMarkerEnd)
	if relEnd < 0 {
		return expression, nil, true, errors.New("invalid FlipAi attachment marker")
	}
	end := payloadStart + relEnd
	raw, err := base64.RawURLEncoding.DecodeString(expression[payloadStart:end])
	if err != nil {
		return expression, nil, true, errors.New("invalid FlipAi attachment metadata")
	}
	var attachments []browserChatAttachment
	if err := json.Unmarshal(raw, &attachments); err != nil || len(attachments) == 0 || len(attachments) > maxInboundAttachmentCount {
		return expression, nil, true, errors.New("invalid FlipAi attachment list")
	}
	for _, a := range attachments {
		if err := validatePreparedBrowserChatImage(a); err != nil {
			return expression, nil, true, err
		}
	}
	clean := expression[:start] + expression[end+len(browserChatAttachmentMarkerEnd):]
	return clean, attachments, true, nil
}

func browserChatFindFileInputJS(attachments []browserChatAttachment) string {
	specs := make([]map[string]string, 0, len(attachments))
	for _, a := range attachments {
		specs = append(specs, map[string]string{"type": normalizeInboundMediaType(a.MediaType), "ext": strings.ToLower(filepath.Ext(a.Path))})
	}
	raw, _ := json.Marshal(specs)
	return `(()=>{
  const files=` + string(raw) + `;
  const accepts=(input)=>{
    if(input.disabled || (files.length>1&&!input.multiple))return false;
    const tokens=String(input.accept||'').toLowerCase().split(',').map(v=>v.trim()).filter(Boolean);
    return files.every(f=>!tokens.length||tokens.some(t=>t==='*/*'||t==='*'||t===f.type||t===f.ext||(t.endsWith('/*')&&f.type.startsWith(t.slice(0,-1)))));
  };
  const pick=()=>Array.from(document.querySelectorAll('input[type="file"]')).find(accepts)||null;
  const input=pick();if(input)return input;
  // Open each visible attachment/menu control once. Never fall back to an
  // image-only picker for audio, or repeatedly toggle the same menu closed.
  const clicked=globalThis.__flipaiAttachmentMenus||(globalThis.__flipaiAttachmentMenus=new WeakSet());
  const visible=n=>{const r=n.getBoundingClientRect();return r.width>0&&r.height>0&&getComputedStyle(n).visibility!=='hidden'};
  const controls=Array.from(document.querySelectorAll('button,[role="button"],[role="menuitem"],label'));
  const button=controls.find(n=>{
    if(!visible(n)||n.disabled||clicked.has(n)||n.matches('label[for]')&&document.getElementById(n.htmlFor)?.matches('input[type="file"]'))return false;
    const s=((n.getAttribute('aria-label')||'')+' '+(n.getAttribute('title')||'')+' '+(n.innerText||n.textContent||'')).toLowerCase();
    return /attach|upload|add files?|add photos?|add images?|from (computer|device)/.test(s);
  });
  if(button){clicked.add(button);button.click();}
  return pick();
})()`
}

// Observe the composer's attachment receipts, not just the local file input.
// A populated FileList does not mean a provider accepted or uploaded the file.
func browserChatAudioUploadJS(attachments []browserChatAttachment, begin bool) string {
	names := []string{}
	for _, a := range attachments {
		if strings.HasPrefix(normalizeInboundMediaType(a.MediaType), "audio/") {
			names = append(names, filepath.Base(a.Path))
		}
	}
	raw, _ := json.Marshal(names)
	return `(()=>{
 const names=` + string(raw) + `;
 const visible=n=>{const r=n.getBoundingClientRect();return r.width>0&&r.height>0&&getComputedStyle(n).visibility!=='hidden'};
 const label=n=>String((n.getAttribute('aria-label')||'')+' '+(n.getAttribute('title')||'')+' '+(n.innerText||n.textContent||'')).trim();
 const leaves=()=>Array.from(document.querySelectorAll('span,div,p,button,a,[role="alert"],[role="status"]')).filter(n=>visible(n)&&label(n).length<600);
 const counts=()=>names.map(name=>leaves().filter(n=>label(n).includes(name)).length);
 if (` + fmt.Sprintf("%t", begin) + `) {
  globalThis.__flipaiAudioUpload={before:counts(),alerts:new Set(leaves().map(label)),since:Date.now(),stable:0};
  return {ready:false};
 }
 const state=globalThis.__flipaiAudioUpload;
 if(!state)return {ready:false,error:'Upload tracking was lost; the voice note was not sent.'};
 const rejection=leaves().map(label).find(t=>!state.alerts.has(t)&&/unsupported (file|format|type)|file (type|format) (is )?not supported|cannot upload|can.t upload|upload failed|failed to upload|file too large|file is too large|exceeds.*limit/i.test(t));
 if(rejection)return {ready:false,error:rejection.slice(0,250)};
 const busy=Array.from(document.querySelectorAll('[role="progressbar"],[aria-busy="true"],[data-state="uploading"]')).some(visible)||leaves().some(n=>/^(uploading|processing)(\b|\.)/i.test(label(n)));
 const now=counts();
 const receipt=names.every((_,i)=>now[i]>state.before[i]);
 state.stable=receipt&&!busy?state.stable+1:0;
 return {ready:state.stable>=3&&Date.now()-state.since>=1000};
})()`
}

func uploadBrowserChatImages(d voiceDevTools, attachments []browserChatAttachment) error {
	if d == nil {
		return errors.New("browser chat has no in-process control channel")
	}
	paths := make([]string, 0, len(attachments))
	for _, a := range attachments {
		if err := validatePreparedBrowserChatImage(a); err != nil {
			return err
		}
		abs, _ := filepath.Abs(a.Path)
		paths = append(paths, abs)
	}
	if len(paths) == 0 {
		return errors.New("no media attachment was supplied")
	}
	var objectID string
	var lastErr error
	_ = voiceEval(d, `(()=>{globalThis.__flipaiAttachmentMenus=new WeakSet();return true})()`, false, nil)
	findJS := browserChatFindFileInputJS(attachments)
	for i := 0; i < 24; i++ {
		objectID, lastErr = voiceEvalObject(d, findJS)
		if lastErr == nil && objectID != "" {
			break
		}
		time.Sleep(125 * time.Millisecond)
	}
	if objectID == "" {
		return errors.New("This chat has no file picker that accepts these attachments. Voice notes require audio-file upload support; the message was not sent.")
	}
	hasAudio := false
	for _, a := range attachments {
		hasAudio = hasAudio || strings.HasPrefix(normalizeInboundMediaType(a.MediaType), "audio/")
	}
	if hasAudio {
		if err := voiceEval(d, browserChatAudioUploadJS(attachments, true), false, nil); err != nil {
			return err
		}
	}
	if err := d.Call("DOM.setFileInputFiles", map[string]any{"files": paths, "objectId": objectID}, nil); err != nil {
		return fmt.Errorf("could not attach the file to the chat: %w", err)
	}
	if hasAudio {
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			var status struct {
				Ready bool   `json:"ready"`
				Error string `json:"error"`
			}
			if err := voiceEval(d, browserChatAudioUploadJS(attachments, false), false, &status); err != nil {
				return err
			}
			if status.Error != "" {
				return fmt.Errorf("The chat rejected the voice note: %s. The message was not sent", status.Error)
			}
			if status.Ready {
				return nil
			}
			time.Sleep(250 * time.Millisecond)
		}
		return errors.New("The chat did not confirm the voice-note upload within 60 seconds. The message was not sent; check the chat's audio-file support and upload limits")
	}
	time.Sleep(450 * time.Millisecond)
	return nil
}

func browserChatImageOnlyPrompt(count int) string {
	if count == 1 {
		return "Please respond to the attached file."
	}
	return "Please respond to the attached files."
}

func browserChatAttachmentOnlyPrompt(attachments []browserChatAttachment) string {
	for _, a := range attachments {
		if strings.HasPrefix(normalizeInboundMediaType(a.MediaType), "audio/") {
			return "The attached voice note is my message. Listen to it, follow my spoken request, and answer it. If you cannot access or understand the audio, say so; do not guess its contents."
		}
	}
	return browserChatImageOnlyPrompt(len(attachments))
}

func (b *Bridge) runBrowserChatSMSWithAttachments(ctx context.Context, agent, command string, in []InboundAttachment) (string, error) {
	attachments, err := preparedBrowserChatImages(in)
	if err != nil {
		return "", err
	}
	marker, err := browserChatAttachmentMarker(attachments)
	if err != nil {
		return "", err
	}
	browserChatAttachmentTurnMu.Lock()
	defer browserChatAttachmentTurnMu.Unlock()
	command = strings.TrimSpace(command)
	if command == "" {
		command = browserChatAttachmentOnlyPrompt(attachments)
	}
	command = marker + "\n" + command
	switch strings.ToUpper(strings.TrimSpace(agent)) {
	case "G":
		return b.runChatGPTSMS(ctx, command)
	case "H":
		return b.runClaudeChatSMS(ctx, command)
	case "M":
		return b.runGeminiChatSMS(ctx, command)
	case "X":
		return b.runGrokChatSMS(ctx, command)
	case "P":
		return b.runCopilotChatSMS(ctx, command)
	case "U":
		return b.runMuseChatSMS(ctx, command)
	default:
		return "", errors.New("unknown browser chat agent")
	}
}
