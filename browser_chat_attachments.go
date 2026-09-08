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
		if len(a.Data) > 0 && supportedInboundMediaType(a.MediaType) {
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
		mediaType := normalizeInboundMediaType(a.MediaType)
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
	kind := "image"
	for _, a := range attachments {
		media := normalizeInboundMediaType(a.MediaType)
		if strings.HasPrefix(media, "audio/") {
			kind = "audio"
			break
		}
		if strings.HasPrefix(media, "video/") {
			kind = "video"
			break
		}
	}
	kindJSON, _ := json.Marshal(kind)
	return `(()=>{
  const desired=` + string(kindJSON) + `;
  const pick=()=>{
    const inputs=Array.from(document.querySelectorAll('input[type="file"]')).filter(n=>!n.disabled);
    const accepts=(n)=>String(n.accept||'').toLowerCase();
    return inputs.find(n=>!accepts(n)||accepts(n).includes(desired)||accepts(n).includes('*/*'))||inputs[0]||null;
  };
  let input=pick();
  if(input)return input;
  const words=['attach','attachment','upload','add file','add files','add photo','add image','photo','image','audio','video'];
  const controls=Array.from(document.querySelectorAll('button,[role="button"],label'));
  const button=controls.find(n=>{
    const s=((n.getAttribute('aria-label')||'')+' '+(n.getAttribute('title')||'')+' '+(n.innerText||n.textContent||'')).toLowerCase();
    return words.some(w=>s.includes(w));
  });
  if(button)button.click();
  return pick();
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
	findJS := browserChatFindFileInputJS(attachments)
	for i := 0; i < 24; i++ {
		objectID, lastErr = voiceEvalObject(d, findJS)
		if lastErr == nil && objectID != "" {
			break
		}
		time.Sleep(125 * time.Millisecond)
	}
	if objectID == "" {
		if lastErr != nil {
			return fmt.Errorf("could not open the chat file picker: %w", lastErr)
		}
		return errors.New("could not find the chat file picker")
	}
	if err := d.Call("DOM.setFileInputFiles", map[string]any{"files": paths, "objectId": objectID}, nil); err != nil {
		return fmt.Errorf("could not attach the file to the chat: %w", err)
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
		command = browserChatImageOnlyPrompt(len(attachments))
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
