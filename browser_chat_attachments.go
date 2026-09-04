package main

import (
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
	case "G", "H", "M", "X":
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
	foundImage := false
	for _, a := range m.Attachments {
		if len(a.Data) > 0 && strings.HasPrefix(normalizeInboundMediaType(a.MediaType), "image/") {
			foundImage = true
			break
		}
	}
	if !foundImage {
		return remoteCommand{}, fmt.Errorf("%s supports image attachments from Google Voice; no usable image was found", agentDisplayName(agent))
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
		if !strings.HasPrefix(mediaType, "image/") {
			return nil, fmt.Errorf("browser chat currently accepts image attachments from Google Voice; %s is %s", a.Filename, mediaType)
		}
		item := browserChatAttachment{Path: a.Path, Filename: a.Filename, MediaType: mediaType}
		if err := validatePreparedBrowserChatImage(item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil, errors.New("no usable image attachment was found")
	}
	if len(out) > maxInboundAttachmentCount {
		return nil, fmt.Errorf("too many image attachments: %d", len(out))
	}
	return out, nil
}

func validatePreparedBrowserChatImage(a browserChatAttachment) error {
	if !strings.HasPrefix(normalizeInboundMediaType(a.MediaType), "image/") {
		return fmt.Errorf("attachment %q is not an image", a.Filename)
	}
	abs, err := filepath.Abs(strings.TrimSpace(a.Path))
	if err != nil || abs == "" {
		return errors.New("invalid prepared image path")
	}
	temp, err := filepath.Abs(os.TempDir())
	if err != nil {
		return errors.New("could not resolve the temporary attachment folder")
	}
	rel, err := filepath.Rel(temp, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("browser chat refused an image outside FlipAi's temporary attachment folder")
	}
	first := rel
	if i := strings.IndexRune(first, os.PathSeparator); i >= 0 {
		first = first[:i]
	}
	if !strings.HasPrefix(strings.ToLower(first), "flipai-inbound-") {
		return errors.New("browser chat refused an image that was not prepared by FlipAi")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("read prepared image: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("prepared image is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > maxInboundAttachmentBytes {
		return fmt.Errorf("image must be between 1 byte and %d MB", maxInboundAttachmentBytes>>20)
	}
	return nil
}

func browserChatAttachmentMarker(in []browserChatAttachment) (string, error) {
	if len(in) == 0 {
		return "", errors.New("no image attachment was supplied")
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

// extractBrowserChatAttachmentMarker runs inside the provider WebView worker.
// It removes FlipAi's private attachment metadata before the page sees the
// prompt and returns the validated local image files for the shared CDP upload.
func extractBrowserChatAttachmentMarker(expression string) (string, []browserChatAttachment, bool, error) {
	start := strings.Index(expression, browserChatAttachmentMarkerStart)
	if start < 0 {
		return expression, nil, false, nil
	}
	payloadStart := start + len(browserChatAttachmentMarkerStart)
	relEnd := strings.Index(expression[payloadStart:], browserChatAttachmentMarkerEnd)
	if relEnd < 0 {
		return expression, nil, true, errors.New("invalid FlipAi image attachment marker")
	}
	end := payloadStart + relEnd
	raw, err := base64.RawURLEncoding.DecodeString(expression[payloadStart:end])
	if err != nil {
		return expression, nil, true, errors.New("invalid FlipAi image attachment metadata")
	}
	var attachments []browserChatAttachment
	if err := json.Unmarshal(raw, &attachments); err != nil || len(attachments) == 0 || len(attachments) > maxInboundAttachmentCount {
		return expression, nil, true, errors.New("invalid FlipAi image attachment list")
	}
	for _, a := range attachments {
		if err := validatePreparedBrowserChatImage(a); err != nil {
			return expression, nil, true, err
		}
	}
	clean := expression[:start] + expression[end+len(browserChatAttachmentMarkerEnd):]
	return clean, attachments, true, nil
}

const browserChatFindFileInputJS = `(()=>{
  const pick=()=>{
    const inputs=Array.from(document.querySelectorAll('input[type="file"]')).filter(n=>!n.disabled);
    return inputs.find(n=>String(n.accept||'').toLowerCase().includes('image'))||inputs[0]||null;
  };
  let input=pick();
  if(input)return input;
  const words=['attach','attachment','upload','add file','add files','add photo','add image','photo','image'];
  const controls=Array.from(document.querySelectorAll('button,[role="button"],label'));
  const button=controls.find(n=>{
    const s=((n.getAttribute('aria-label')||'')+' '+(n.getAttribute('title')||'')+' '+(n.innerText||n.textContent||'')).toLowerCase();
    return words.some(w=>s.includes(w));
  });
  if(button)button.click();
  return pick();
})()`

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
		return errors.New("no image attachment was supplied")
	}

	var objectID string
	var lastErr error
	for i := 0; i < 24; i++ {
		objectID, lastErr = voiceEvalObject(d, browserChatFindFileInputJS)
		if lastErr == nil && objectID != "" {
			break
		}
		time.Sleep(125 * time.Millisecond)
	}
	if objectID == "" {
		if lastErr != nil {
			return fmt.Errorf("could not open the chat image picker: %w", lastErr)
		}
		return errors.New("could not find the chat image picker")
	}
	if err := d.Call("DOM.setFileInputFiles", map[string]any{"files": paths, "objectId": objectID}, nil); err != nil {
		return fmt.Errorf("could not attach the image to the chat: %w", err)
	}
	// The provider's existing turn driver waits for its Send button to become
	// ready, so only a short handoff delay is needed here.
	time.Sleep(350 * time.Millisecond)
	return nil
}

func browserChatImageOnlyPrompt(count int) string {
	if count == 1 {
		return "Please respond to the attached image."
	}
	return "Please respond to the attached images."
}

func (b *Bridge) runBrowserChatSMSWithAttachments(ctx context.Context, agent, command string, in []InboundAttachment) (string, error) {
	images, err := preparedBrowserChatImages(in)
	if err != nil {
		return "", err
	}
	marker, err := browserChatAttachmentMarker(images)
	if err != nil {
		return "", err
	}
	browserChatAttachmentTurnMu.Lock()
	defer browserChatAttachmentTurnMu.Unlock()

	command = strings.TrimSpace(command)
	if command == "" {
		command = browserChatImageOnlyPrompt(len(images))
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
	default:
		return "", errors.New("unknown browser chat agent")
	}
}
