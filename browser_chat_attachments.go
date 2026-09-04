package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type browserChatAttachment struct {
	Path      string `json:"path"`
	Filename  string `json:"filename,omitempty"`
	MediaType string `json:"mediaType,omitempty"`
}

type browserChatUploadRequest struct {
	Attachments []browserChatAttachment `json:"attachments"`
}

type browserChatUploadReply struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
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
	if len(attachments) == 0 {
		return errors.New("no image attachment was supplied")
	}
	paths := make([]string, 0, len(attachments))
	for _, a := range attachments {
		if err := validatePreparedBrowserChatImage(a); err != nil {
			return err
		}
		abs, _ := filepath.Abs(a.Path)
		paths = append(paths, abs)
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
	time.Sleep(1200 * time.Millisecond)
	return nil
}

func registerBrowserChatUploadEndpoint(mux *http.ServeMux, authorized func(*http.Request) bool, dev voiceDevTools) {
	mux.HandleFunc("/upload", func(rw http.ResponseWriter, r *http.Request) {
		if authorized == nil || !authorized(r) {
			http.Error(rw, "FlipAi token required", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(rw, "POST required", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(rw, r.Body, 32<<10)
		var body browserChatUploadRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(rw, "invalid upload request", http.StatusBadRequest)
			return
		}
		if len(body.Attachments) == 0 || len(body.Attachments) > maxInboundAttachmentCount {
			http.Error(rw, "invalid image attachment count", http.StatusBadRequest)
			return
		}
		if err := uploadBrowserChatImages(dev, body.Attachments); err != nil {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(rw).Encode(browserChatUploadReply{OK: false, Detail: err.Error()})
			return
		}
		rw.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(rw).Encode(browserChatUploadReply{OK: true})
	})
}

func browserChatUploadControl(ctx context.Context, dataDir, agent string, attachments []browserChatAttachment) error {
	payload, _ := json.Marshal(browserChatUploadRequest{Attachments: attachments})
	var body []byte
	var code int
	var err error
	switch strings.ToUpper(strings.TrimSpace(agent)) {
	case "G":
		var s ChatGPTWebRuntime
		s, err = ensureChatGPTReady(ctx, dataDir)
		if err == nil {
			body, code, err = chatGPTControlRequest(ctx, s, http.MethodPost, "/upload", strings.NewReader(string(payload)))
		}
	case "H":
		var s ClaudeChatWebRuntime
		s, err = ensureClaudeChatReady(ctx, dataDir)
		if err == nil {
			body, code, err = claudeChatControlRequest(ctx, s, http.MethodPost, "/upload", strings.NewReader(string(payload)))
		}
	case "M":
		var s GeminiChatWebRuntime
		s, err = ensureGeminiChatReady(ctx, dataDir)
		if err == nil {
			body, code, err = geminiChatControlRequest(ctx, s, http.MethodPost, "/upload", strings.NewReader(string(payload)))
		}
	case "X":
		var s GrokChatWebRuntime
		s, err = ensureGrokChatReady(ctx, dataDir)
		if err == nil {
			body, code, err = grokChatControlRequest(ctx, s, http.MethodPost, "/upload", strings.NewReader(string(payload)))
		}
	default:
		return errors.New("unknown browser chat agent")
	}
	if err != nil {
		return err
	}
	var reply browserChatUploadReply
	_ = json.Unmarshal(body, &reply)
	if code != http.StatusOK || !reply.OK {
		detail := strings.TrimSpace(reply.Detail)
		if detail == "" {
			detail = strings.TrimSpace(string(body))
		}
		if detail == "" {
			detail = fmt.Sprintf("image upload returned HTTP %d", code)
		}
		return errors.New(detail)
	}
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
	browserChatAttachmentTurnMu.Lock()
	defer browserChatAttachmentTurnMu.Unlock()

	dataDir := filepath.Dir(b.statePath)
	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = browserChatUploadControl(uploadCtx, dataDir, agent, images)
	cancel()
	if err != nil {
		return "", fmt.Errorf("attach image to %s: %w", agentDisplayName(agent), err)
	}

	command = strings.TrimSpace(command)
	if command == "" {
		command = browserChatImageOnlyPrompt(len(images))
	}
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
