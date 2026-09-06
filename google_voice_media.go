package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// googleVoiceMediaMailClient keeps the existing direct Google Voice SMS client
// as the source of truth for sender/thread identity, but upgrades an MMS marker
// into the actual bytes from the signed-in Google Voice page before Bridge sees
// the message. Nothing passed to an agent is a download link.
type googleVoiceMediaMailClient struct {
	*GoogleVoiceSMSClient
	dataDir string
}

func newGoogleVoiceMediaMailClient(dataDir string) MailClient {
	return &googleVoiceMediaMailClient{
		GoogleVoiceSMSClient: NewGoogleVoiceSMSClient(dataDir),
		dataDir:              dataDir,
	}
}

func googleVoiceBodySignalsMedia(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return false
	}
	for _, phrase := range []string{
		"mms received",
		"media received",
		"attachment received",
		"photo received",
		"image received",
		"video received",
		"audio received",
		"voice message",
		"voice note",
	} {
		if strings.Contains(v, phrase) {
			return true
		}
	}
	return false
}

func (g *googleVoiceMediaMailClient) Get(ctx context.Context, id string) (GmailMessage, error) {
	m, err := g.GoogleVoiceSMSClient.Get(ctx, id)
	if err != nil {
		return GmailMessage{}, err
	}
	if !googleVoiceBodySignalsMedia(m.Snippet) && !googleVoiceBodySignalsMedia(m.Body) {
		return m, nil
	}
	phone, thread, err := g.directReplyIdentity(m)
	if err != nil {
		return GmailMessage{}, err
	}
	attachments, err := requestGoogleVoiceInboundMedia(ctx, g.dataDir, m.ID, phone, thread, m.Snippet)
	if err != nil {
		return GmailMessage{}, fmt.Errorf("read Google Voice MMS attachment: %w", err)
	}
	if len(attachments) == 0 {
		return GmailMessage{}, errors.New("Google Voice reported an MMS, but FlipAi could not find the attached file in the conversation")
	}
	m.Attachments = attachments
	return m, nil
}
