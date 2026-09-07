package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
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

const googleVoiceMMSRecoveryWindow = 3 * time.Minute

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

func googleVoiceMMSMediaMissing(err error) bool {
	if err == nil {
		return false
	}
	v := strings.ToLower(err.Error())
	return strings.Contains(v, "no mms media element was found next to the received message") ||
		strings.Contains(v, "could not find the attached file in the conversation")
}

// staleGoogleVoiceMMSNoop turns an old MMS marker whose media is no longer
// exposed by Google Voice into a deliberately empty candidate. Bridge will
// checkpoint it through its normal security/parser path, but it cannot replay
// yesterday's "MMS Received" marker as a new command after a restart. Fresh
// MMS stays retryable for a short window so the real media still has time to
// appear in the conversation DOM.
func staleGoogleVoiceMMSNoop(m GmailMessage, mediaErr error, now time.Time) (GmailMessage, bool) {
	if !googleVoiceMMSMediaMissing(mediaErr) || m.InternalDate.IsZero() {
		return GmailMessage{}, false
	}
	age := now.Sub(m.InternalDate)
	if age < googleVoiceMMSRecoveryWindow {
		return GmailMessage{}, false
	}
	m.Body = "Google Voice\n"
	m.Snippet = ""
	m.Attachments = nil
	return m, true
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
		if noop, ok := staleGoogleVoiceMMSNoop(m, err, time.Now()); ok {
			return noop, nil
		}
		return GmailMessage{}, fmt.Errorf("read Google Voice MMS attachment: %w", err)
	}
	if len(attachments) == 0 {
		err := errors.New("Google Voice reported an MMS, but FlipAi could not find the attached file in the conversation")
		if noop, ok := staleGoogleVoiceMMSNoop(m, err, time.Now()); ok {
			return noop, nil
		}
		return GmailMessage{}, err
	}
	m.Attachments = attachments
	return m, nil
}
