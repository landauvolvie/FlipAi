package main

import (
	"net/url"
	"strings"
	"sync"
)

// browserChatReturnedMedia is the media that belongs to the browser-chat turn
// that just finished. Browser turns are serialized by Bridge, so one guarded
// slot is enough and prevents an asset from leaking into the next SMS.
type browserChatReturnedMedia struct {
	Kind            string
	Filename        string
	MediaType       string
	Data            []byte
	ConversationURL string
}

var (
	capturedBrowserChatMediaMu sync.Mutex
	capturedBrowserChatMedia   *browserChatReturnedMedia
)

func clearCapturedBrowserChatReturnedMedia() {
	capturedBrowserChatMediaMu.Lock()
	capturedBrowserChatMedia = nil
	capturedBrowserChatMediaMu.Unlock()
}

func storeCapturedBrowserChatReturnedMedia(m *browserChatReturnedMedia) {
	if m == nil {
		return
	}
	copyMedia := &browserChatReturnedMedia{
		Kind:            strings.ToLower(strings.TrimSpace(m.Kind)),
		Filename:        strings.TrimSpace(m.Filename),
		MediaType:       strings.ToLower(strings.TrimSpace(m.MediaType)),
		ConversationURL: safeBrowserConversationURL(m.ConversationURL),
		Data:            append([]byte(nil), m.Data...),
	}
	if copyMedia.Kind == "" {
		return
	}
	capturedBrowserChatMediaMu.Lock()
	capturedBrowserChatMedia = copyMedia
	capturedBrowserChatMediaMu.Unlock()
}

func takeCapturedBrowserChatReturnedMedia() *browserChatReturnedMedia {
	capturedBrowserChatMediaMu.Lock()
	defer capturedBrowserChatMediaMu.Unlock()
	m := capturedBrowserChatMedia
	capturedBrowserChatMedia = nil
	if m == nil {
		return nil
	}
	return &browserChatReturnedMedia{
		Kind:            m.Kind,
		Filename:        m.Filename,
		MediaType:       m.MediaType,
		ConversationURL: m.ConversationURL,
		Data:            append([]byte(nil), m.Data...),
	}
}

// Only the five browser providers FlipAi itself controls are valid fallback
// destinations. This keeps a page element from turning into an arbitrary URL in
// an outgoing SMS.
func safeBrowserConversationURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	switch host {
	case "chatgpt.com", "claude.ai", "gemini.google.com", "grok.com", "copilot.microsoft.com":
		return u.String()
	default:
		return ""
	}
}

func appendBrowserMediaConversationLink(body, conversationURL string) string {
	body = strings.TrimSpace(body)
	conversationURL = safeBrowserConversationURL(conversationURL)
	if conversationURL == "" || strings.Contains(body, conversationURL) {
		return body
	}
	if body == "" {
		return "Open the media in the chat: " + conversationURL
	}
	return body + "\n\nOpen the media in the chat: " + conversationURL
}

// prepareBrowserChatReturnedMediaReply prefers a real Google Voice image MMS.
// Google Voice's web UI accepts images but not arbitrary model files, so video,
// audio, files, or an image that cannot be normalized safely fall back to the
// provider conversation URL as requested by the user.
func prepareBrowserChatReturnedMediaReply(body string, media *browserChatReturnedMedia) (string, *outboundVoiceImage) {
	if media == nil {
		return body, nil
	}
	if strings.EqualFold(media.Kind, "image") && len(media.Data) > 0 {
		name := strings.TrimSpace(media.Filename)
		if name == "" {
			name = "flipai-generated.png"
		}
		if image, err := normalizeVoiceImage(name, media.Data); err == nil {
			return body, image
		}
	}
	return appendBrowserMediaConversationLink(body, media.ConversationURL), nil
}
