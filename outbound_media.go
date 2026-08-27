package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Google Voice can send JPG/PNG/GIF images. Its own clients shrink still images
// above 2 MB, but keeping the attachment under that limit before it reaches the
// Google Voice web UI is substantially more reliable.
const voiceImageMaxBytes = 2 * 1024 * 1024
const codexRolloutTailBytes int64 = 64 * 1024 * 1024

type outboundVoiceImage struct {
	Filename  string
	MediaType string
	Data      []byte
}

var deliveredVoiceImages sync.Map // original Gmail message id -> true

var (
	capturedCodexImageMu sync.Mutex
	capturedCodexImage   *outboundVoiceImage
)

func isTransientVoiceReply(body string) bool {
	s := strings.ToLower(strings.TrimSpace(body))
	if s == "" {
		return true
	}
	return strings.HasPrefix(s, "✓") ||
		strings.Contains(s, " still working") ||
		strings.HasPrefix(s, "queued for ")
}

// clearCapturedCodexImage starts each Codex turn with an empty media slot. The
// bridge runs one agent turn at a time, so the next imageGeneration item belongs
// to the final reply for that same SMS and cannot leak into a later request.
func clearCapturedCodexImage() {
	capturedCodexImageMu.Lock()
	capturedCodexImage = nil
	capturedCodexImageMu.Unlock()
}

// captureCodexImageFromItemCompleted reads the image directly from Codex App
// Server's official item/completed notification. Current v2 ThreadItem payloads
// use type=imageGeneration and include the generated image as base64 in result,
// plus an optional savedPath. Reading the event is preferable to searching the
// filesystem after the fact: it is the exact image for the active turn and does
// not require another prompt, tool call, save instruction, or model token.
//
// Do not require status==completed here. Codex has had versions where a valid
// non-empty result arrived while the status string still said generating. The
// item/completed lifecycle plus valid image bytes is sufficient.
func captureCodexImageFromItemCompleted(params json.RawMessage) bool {
	var p struct {
		TurnID string `json:"turnId"`
		Item   struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Status    string `json:"status"`
			Result    string `json:"result"`
			SavedPath string `json:"savedPath"`
		} `json:"item"`
	}
	if json.Unmarshal(params, &p) != nil || p.Item.Type != "imageGeneration" {
		return false
	}

	var img *outboundVoiceImage
	if encoded := strings.TrimSpace(p.Item.Result); encoded != "" {
		if data, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(data) > 0 {
			name := "flipai-generated.png"
			if p.Item.SavedPath != "" {
				name = filepath.Base(p.Item.SavedPath)
			}
			if normalized, err := normalizeVoiceImage(name, data); err == nil {
				img = normalized
			}
		}
	}
	if img == nil && strings.TrimSpace(p.Item.SavedPath) != "" {
		if data, err := os.ReadFile(p.Item.SavedPath); err == nil && len(data) > 0 {
			if normalized, err := normalizeVoiceImage(filepath.Base(p.Item.SavedPath), data); err == nil {
				img = normalized
			}
		}
	}
	if img == nil {
		return false
	}

	copyImage := &outboundVoiceImage{
		Filename:  img.Filename,
		MediaType: img.MediaType,
		Data:      append([]byte(nil), img.Data...),
	}
	capturedCodexImageMu.Lock()
	capturedCodexImage = copyImage
	capturedCodexImageMu.Unlock()
	return true
}

func takeCapturedCodexImage() *outboundVoiceImage {
	capturedCodexImageMu.Lock()
	defer capturedCodexImageMu.Unlock()
	img := capturedCodexImage
	capturedCodexImage = nil
	if img == nil {
		return nil
	}
	return &outboundVoiceImage{
		Filename:  img.Filename,
		MediaType: img.MediaType,
		Data:      append([]byte(nil), img.Data...),
	}
}

// generatedImageForVoiceReply is intentionally outside the model path. The
// primary path consumes the exact image bytes Codex already emitted in its
// item/completed event. The filesystem scan remains only as compatibility
// fallback for older Codex builds that did not expose the imageGeneration item.
func generatedImageForVoiceReply(original GmailMessage, body string) (*outboundVoiceImage, string) {
	if isTransientVoiceReply(body) || strings.TrimSpace(original.ID) == "" {
		return nil, ""
	}
	key := original.ID
	if _, sent := deliveredVoiceImages.Load(key); sent {
		return nil, ""
	}
	if img := takeCapturedCodexImage(); img != nil {
		return img, key
	}

	since := original.InternalDate
	if since.IsZero() {
		since = time.Now().Add(-5 * time.Minute)
	}
	// Mail/provider timestamps can differ from the PC clock by a few seconds.
	since = since.Add(-15 * time.Second)

	img, err := newestCodexGeneratedImageSince(since)
	if err != nil || img == nil {
		return nil, ""
	}
	return img, key
}

func markGeneratedImageDelivered(key string) {
	if strings.TrimSpace(key) != "" {
		deliveredVoiceImages.Store(key, true)
	}
}

func codexHome() string {
	if v := strings.TrimSpace(os.Getenv("CODEX_HOME")); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".codex")
	}
	return ""
}

type recentFile struct {
	path string
	mod  time.Time
}

func recentFiles(root string, since time.Time, extensions map[string]bool) []recentFile {
	if root == "" {
		return nil
	}
	var files []recentFile
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if len(extensions) > 0 && !extensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(since) {
			return nil
		}
		files = append(files, recentFile{path: path, mod: info.ModTime()})
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	return files
}

func newestCodexGeneratedImageSince(since time.Time) (*outboundVoiceImage, error) {
	home := codexHome()
	if home == "" {
		return nil, errors.New("Codex home is unavailable")
	}

	// Compatibility path: many Codex builds persist the completed image here.
	imageExt := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}
	for _, f := range recentFiles(filepath.Join(home, "generated_images"), since, imageExt) {
		data, err := os.ReadFile(f.path)
		if err != nil || len(data) == 0 {
			continue
		}
		if img, err := normalizeVoiceImage(filepath.Base(f.path), data); err == nil {
			return img, nil
		}
	}

	// Last-resort compatibility path. Older versions may have no generated_images
	// file even though the image-generation result lives in the rollout as base64.
	jsonExt := map[string]bool{".jsonl": true}
	for _, f := range recentFiles(filepath.Join(home, "sessions"), since, jsonExt) {
		data, err := latestImageResultFromRollout(f.path)
		if err != nil || len(data) == 0 {
			continue
		}
		if img, err := normalizeVoiceImage("flipai-generated.png", data); err == nil {
			return img, nil
		}
	}
	return nil, errors.New("no recent Codex generated image found")
}

func latestImageResultFromRollout(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > codexRolloutTailBytes {
		if _, err := f.Seek(st.Size()-codexRolloutTailBytes, io.SeekStart); err == nil {
			// The seek probably starts inside a JSONL record. Discard that fragment.
			r := bufio.NewReaderSize(f, 256*1024)
			_, _ = r.ReadBytes('\n')
			return latestImageResultFromReader(r)
		}
	}
	return latestImageResultFromReader(bufio.NewReaderSize(f, 256*1024))
}

func latestImageResultFromReader(r *bufio.Reader) ([]byte, error) {
	var latest []byte
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 && looksLikeImageGenerationRecord(line) {
			var v any
			if json.Unmarshal(bytes.TrimSpace(line), &v) == nil {
				if encoded := findImageGenerationResult(v); encoded != "" {
					if decoded, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded)); decErr == nil && len(decoded) > 0 {
						latest = decoded
					}
				}
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return latest, err
		}
	}
	if len(latest) == 0 {
		return nil, errors.New("rollout contains no image generation result")
	}
	return latest, nil
}

func looksLikeImageGenerationRecord(line []byte) bool {
	l := bytes.ToLower(line)
	return bytes.Contains(l, []byte("image_generation")) || bytes.Contains(l, []byte("imagegeneration"))
}

func findImageGenerationResult(v any) string {
	switch x := v.(type) {
	case map[string]any:
		typeName, _ := x["type"].(string)
		typeName = strings.ToLower(strings.ReplaceAll(typeName, "_", ""))
		if strings.Contains(typeName, "imagegeneration") {
			if result, ok := x["result"].(string); ok && strings.TrimSpace(result) != "" {
				return result
			}
		}
		for _, child := range x {
			if result := findImageGenerationResult(child); result != "" {
				return result
			}
		}
	case []any:
		for _, child := range x {
			if result := findImageGenerationResult(child); result != "" {
				return result
			}
		}
	}
	return ""
}

func normalizeVoiceImage(name string, data []byte) (*outboundVoiceImage, error) {
	if len(data) == 0 {
		return nil, errors.New("empty generated image")
	}
	mediaType := strings.ToLower(strings.TrimSpace(http.DetectContentType(data)))
	allowed := mediaType == "image/png" || mediaType == "image/jpeg" || mediaType == "image/gif"
	if !allowed {
		return nil, fmt.Errorf("unsupported Google Voice image type %s", mediaType)
	}
	if len(data) <= voiceImageMaxBytes {
		ext := ".png"
		switch mediaType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/gif":
			ext = ".gif"
		}
		base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
		if base == "" || base == "." {
			base = "flipai-generated"
		}
		return &outboundVoiceImage{Filename: base + ext, MediaType: mediaType, Data: data}, nil
	}

	// Google Voice documents a 2 MB image boundary. Re-encode oversized still
	// images to JPEG in memory; no extra model step and no permanent local copy.
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("compress generated image: %w", err)
	}
	bounds := decoded.Bounds()
	flat := image.NewRGBA(bounds)
	draw.Draw(flat, bounds, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(flat, bounds, decoded, bounds.Min, draw.Over)
	for _, quality := range []int{82, 70, 58, 46} {
		var out bytes.Buffer
		if err := jpeg.Encode(&out, flat, &jpeg.Options{Quality: quality}); err != nil {
			continue
		}
		if out.Len() <= voiceImageMaxBytes {
			return &outboundVoiceImage{Filename: "flipai-generated.jpg", MediaType: "image/jpeg", Data: out.Bytes()}, nil
		}
	}
	return nil, errors.New("generated image is too large for Google Voice MMS")
}

func buildThreadedReplyMessageWithImage(from, to string, meta replyThreadMeta, body string, img *outboundVoiceImage) (string, error) {
	if img == nil {
		return buildThreadedReplyMessage(from, to, meta, body)
	}
	addr, err := safeGoogleVoiceReplyAddress(to)
	if err != nil {
		return "", err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		body = "Image generated by FlipAi."
	}
	messageID := cleanReplyHeader(meta.MessageID)
	if messageID == "" {
		return "", errors.New("original Google Voice email has no Message-ID; refusing standalone reply")
	}
	subject := cleanReplyHeader(meta.Subject)
	if subject == "" {
		return "", errors.New("original Google Voice email has no subject; refusing standalone reply")
	}
	references := appendReplyReference(meta.References, messageID)

	var b bytes.Buffer
	if from = cleanReplyHeader(from); from != "" {
		fmt.Fprintf(&b, "From: %s\r\n", from)
	}
	fmt.Fprintf(&b, "To: %s\r\n", addr)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "In-Reply-To: %s\r\n", messageID)
	fmt.Fprintf(&b, "References: %s\r\n", references)
	b.WriteString("MIME-Version: 1.0\r\n")

	mw := multipart.NewWriter(&b)
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mw.Boundary())

	textHeader := make(textproto.MIMEHeader)
	textHeader.Set("Content-Type", "text/plain; charset=UTF-8")
	textHeader.Set("Content-Transfer-Encoding", "8bit")
	textPart, err := mw.CreatePart(textHeader)
	if err != nil {
		return "", err
	}
	_, _ = io.WriteString(textPart, body+"\r\n")

	imageHeader := make(textproto.MIMEHeader)
	imageHeader.Set("Content-Type", img.MediaType)
	imageHeader.Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", cleanReplyHeader(img.Filename)))
	imageHeader.Set("Content-ID", "<flipai-generated-image>")
	imageHeader.Set("Content-Transfer-Encoding", "base64")
	imagePart, err := mw.CreatePart(imageHeader)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(img.Data)
	for len(encoded) > 76 {
		_, _ = io.WriteString(imagePart, encoded[:76]+"\r\n")
		encoded = encoded[76:]
	}
	if encoded != "" {
		_, _ = io.WriteString(imagePart, encoded+"\r\n")
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	return b.String(), nil
}
