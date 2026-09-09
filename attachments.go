package main

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxInboundAttachmentBytes = 8 << 20
	maxInboundAttachmentCount = 6
	maxInboundTextPartBytes   = 2 << 20
)

type MailAttachment struct {
	Filename  string
	MediaType string
	Data      []byte
}

type InboundAttachment struct {
	Filename  string
	MediaType string
	Path      string
}

func normalizeInboundMediaType(v string) string {
	med, _, err := mime.ParseMediaType(strings.TrimSpace(v))
	if err == nil && med != "" {
		med = strings.ToLower(med)
		switch med {
		case "audio/x-m4a", "audio/m4a":
			return "audio/mp4"
		case "audio/x-wav", "audio/wave":
			return "audio/wav"
		case "audio/mp3", "audio/x-mp3":
			return "audio/mpeg"
		case "application/ogg":
			return "audio/ogg"
		}
		return med
	}
	return strings.ToLower(strings.TrimSpace(strings.SplitN(v, ";", 2)[0]))
}

func supportedInboundMediaType(v string) bool {
	v = normalizeInboundMediaType(v)
	return strings.HasPrefix(v, "image/") || strings.HasPrefix(v, "audio/") || v == "video/mp4"
}

// Use a deterministic map for phone recordings: Windows MIME registrations
// vary by installed apps, and carriers often label voice notes as binary files.
var inboundMediaExtensions = map[string]string{
	".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".wav": "audio/wav",
	".ogg": "audio/ogg", ".oga": "audio/ogg", ".opus": "audio/ogg",
	".aac": "audio/aac", ".amr": "audio/amr", ".flac": "audio/flac",
	".3gp": "audio/3gpp", ".3gpp": "audio/3gpp", ".3g2": "audio/3gpp2",
	".mp4": "video/mp4", ".webm": "audio/webm",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp",
}

func inboundMediaType(declared, filename string) string {
	med := normalizeInboundMediaType(declared)
	if supportedInboundMediaType(med) {
		return med
	}
	// Do not reinterpret HTML error pages or other explicitly typed documents.
	switch med {
	case "", "application/octet-stream", "binary/octet-stream", "application/download", "application/x-download":
		ext := strings.ToLower(filepath.Ext(filename))
		if known := inboundMediaExtensions[ext]; known != "" {
			return known
		}
		if guessed := normalizeInboundMediaType(mime.TypeByExtension(ext)); supportedInboundMediaType(guessed) {
			return guessed
		}
		return med
	}
	return med
}

func hasSupportedMailAttachments(in []MailAttachment) bool {
	for _, a := range in {
		if supportedInboundMediaType(inboundMediaType(a.MediaType, a.Filename)) && len(a.Data) > 0 {
			return true
		}
	}
	return false
}

func mediaTypeForPart(p *multipart.Part) string {
	return inboundMediaType(p.Header.Get("Content-Type"), p.FileName())
}

func readBounded(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("attachment exceeds FlipAi's %d MB inbound limit", maxInboundAttachmentBytes>>20)
	}
	return b, nil
}

// extractMailContent walks nested MIME parts once and returns both the message
// text and the image/audio media that FlipAi intentionally relays.
func extractMailContent(h mail.Header, r io.Reader) (string, []MailAttachment, error) {
	med, params, _ := mime.ParseMediaType(h.Get("Content-Type"))
	med = strings.ToLower(med)
	if strings.HasPrefix(med, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", nil, errors.New("multipart email has no boundary")
		}
		mr := multipart.NewReader(r, boundary)
		var plain, html []string
		var attachments []MailAttachment
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", attachments, err
			}
			ph := mail.Header(textproto.MIMEHeader(p.Header))
			pmed, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
			pmed = strings.ToLower(pmed)

			if strings.HasPrefix(pmed, "multipart/") {
				childBody, childAttachments, err := extractMailContent(ph, p)
				if err != nil {
					return "", attachments, err
				}
				if strings.TrimSpace(childBody) != "" {
					plain = append(plain, childBody)
				}
				attachments = append(attachments, childAttachments...)
				if len(attachments) > maxInboundAttachmentCount {
					return "", attachments, fmt.Errorf("Google Voice message has more than %d supported attachments", maxInboundAttachmentCount)
				}
				continue
			}

			if strings.HasPrefix(pmed, "text/plain") || strings.HasPrefix(pmed, "text/html") {
				b, err := readBounded(decodeTransfer(ph, p), maxInboundTextPartBytes)
				if err != nil {
					return "", attachments, err
				}
				s := strings.TrimSpace(string(b))
				if strings.HasPrefix(pmed, "text/html") {
					html = append(html, stripHTML(s))
				} else {
					plain = append(plain, s)
				}
				continue
			}

			actualType := mediaTypeForPart(p)
			if !supportedInboundMediaType(actualType) {
				continue
			}
			b, err := readBounded(decodeTransfer(ph, p), maxInboundAttachmentBytes)
			if err != nil {
				return "", attachments, err
			}
			if len(b) == 0 {
				continue
			}
			attachments = append(attachments, MailAttachment{Filename: p.FileName(), MediaType: actualType, Data: b})
			if len(attachments) > maxInboundAttachmentCount {
				return "", attachments, fmt.Errorf("Google Voice message has more than %d supported attachments", maxInboundAttachmentCount)
			}
		}
		if len(plain) > 0 {
			return strings.TrimSpace(strings.Join(plain, "\n")), attachments, nil
		}
		return strings.TrimSpace(strings.Join(html, "\n")), attachments, nil
	}

	if strings.HasPrefix(med, "text/") {
		b, err := readBounded(decodeTransfer(h, r), maxInboundTextPartBytes)
		if err != nil {
			return "", nil, err
		}
		s := strings.TrimSpace(string(b))
		if strings.HasPrefix(med, "text/html") {
			s = stripHTML(s)
		}
		return s, nil, nil
	}
	return "", nil, nil
}

func attachmentExtension(mediaType string) string {
	switch normalizeInboundMediaType(mediaType) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/mp4":
		return ".m4a"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/aac":
		return ".aac"
	case "audio/amr":
		return ".amr"
	case "audio/flac":
		return ".flac"
	case "audio/3gpp":
		return ".3gp"
	case "audio/3gpp2":
		return ".3g2"
	case "audio/webm":
		return ".webm"
	case "video/mp4":
		return ".mp4"
	}
	if exts, _ := mime.ExtensionsByType(mediaType); len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

func safeAttachmentFilename(name string, index int, mediaType string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." {
		name = fmt.Sprintf("attachment-%d%s", index+1, attachmentExtension(mediaType))
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._-", r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	clean := strings.Trim(b.String(), ". ")
	if clean == "" {
		clean = fmt.Sprintf("attachment-%d%s", index+1, attachmentExtension(mediaType))
	}
	if ext := strings.ToLower(filepath.Ext(clean)); ext == "" || ext == ".bin" {
		if mediaExt := attachmentExtension(mediaType); mediaExt != ".bin" {
			clean = strings.TrimSuffix(clean, filepath.Ext(clean)) + mediaExt
		}
	}
	return clean
}

func prepareInboundAttachments(in []MailAttachment) ([]InboundAttachment, func(), error) {
	var selected []MailAttachment
	for _, a := range in {
		if supportedInboundMediaType(inboundMediaType(a.MediaType, a.Filename)) && len(a.Data) > 0 {
			a.MediaType = inboundMediaType(a.MediaType, a.Filename)
			selected = append(selected, a)
		}
	}
	if len(selected) == 0 {
		return nil, nil, nil
	}
	if len(selected) > maxInboundAttachmentCount {
		return nil, nil, fmt.Errorf("too many inbound attachments: %d", len(selected))
	}
	dir, err := os.MkdirTemp("", "FlipAi-inbound-")
	if err != nil {
		return nil, nil, fmt.Errorf("create inbound attachment folder: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	out := make([]InboundAttachment, 0, len(selected))
	for i, a := range selected {
		if len(a.Data) > maxInboundAttachmentBytes {
			cleanup()
			return nil, nil, fmt.Errorf("attachment exceeds FlipAi's %d MB inbound limit", maxInboundAttachmentBytes>>20)
		}
		name := safeAttachmentFilename(a.Filename, i, a.MediaType)
		path := filepath.Join(dir, fmt.Sprintf("%02d-%s", i+1, name))
		if err := os.WriteFile(path, a.Data, 0600); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("save inbound attachment: %w", err)
		}
		out = append(out, InboundAttachment{Filename: name, MediaType: normalizeInboundMediaType(a.MediaType), Path: path})
	}
	return out, cleanup, nil
}

func agentForSharedSMSCommand(raw string, cfg Config) string {
	pick := func(text string) string {
		text = strings.TrimSpace(text)
		newSession := configuredNewSessionCommand(cfg)
		if _, ok := stripAgentCommandPrefix(text, configuredCodexPrefix(cfg)); ok ||
			isAgentNewSession(text, configuredCodexPrefix(cfg), newSession) {
			return "C"
		}
		if _, ok := stripAgentCommandPrefix(text, configuredClaudePrefix(cfg)); ok ||
			isAgentNewSession(text, configuredClaudePrefix(cfg), newSession) {
			return "A"
		}
		return ""
	}
	if agent := pick(raw); agent != "" {
		return agent
	}
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) > 1 {
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), fields[0]))
		if agent := pick(rest); agent != "" {
			return agent
		}
	}
	if cfg.DefaultAgent == "A" {
		return "A"
	}
	return "C"
}

func parseRemoteCommandForMessage(raw string, cfg Config, agent string, m GmailMessage) (remoteCommand, error) {
	if agent == "B" {
		agent = agentForSharedSMSCommand(raw, cfg)
	}
	if strings.TrimSpace(raw) != "" {
		return parseRemoteCommand(raw, cfg, agent)
	}
	if !hasSupportedMailAttachments(m.Attachments) {
		return remoteCommand{}, errors.New("empty command")
	}
	if agent != "A" && agent != "C" {
		agent = "C"
	}
	if agentSettings(cfg, agent).RequireCode {
		return remoteCommand{}, fmt.Errorf("attachment-only commands cannot supply the %s text security code; include text with the attachment or disable Require code for this agent", agentDisplayName(agent))
	}
	return remoteCommand{Agent: agent}, nil
}

func promptForInboundAttachments(base string, in []InboundAttachment) string {
	if len(in) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(base))
	b.WriteString("\n\n<flipai_attachments>\n")
	b.WriteString("These are the user's actual Google Voice attachments. Use them as input to the command above.\n")
	hasAudio := false
	for _, a := range in {
		fmt.Fprintf(&b, "- %s (%s)\n", a.Path, a.MediaType)
		if strings.HasPrefix(a.MediaType, "audio/") {
			hasAudio = true
		}
	}
	b.WriteString("</flipai_attachments>")
	if hasAudio {
		b.WriteString("\n\n<flipai_voice_command>\n")
		b.WriteString("The audio attachment is the user's command. Listen to and understand its spoken content using your available capabilities, treat that spoken content as the user's command, carry it out, and respond with the result. Do not merely return a transcription unless the user asks for one.\n")
		b.WriteString("</flipai_voice_command>")
	}
	return b.String()
}

func codexInputForInbound(prompt string, in []InboundAttachment) []map[string]any {
	out := []map[string]any{{"type": "text", "text": prompt}}
	for _, a := range in {
		switch {
		case strings.HasPrefix(a.MediaType, "image/"):
			out = append(out, map[string]any{"type": "localImage", "path": a.Path})
		case strings.HasPrefix(a.MediaType, "audio/"):
			out = append(out, map[string]any{"type": "localAudio", "path": a.Path})
		}
	}
	return out
}
