package main

import (
	"errors"
	"time"
)

const (
	googleVoiceOutboundTextChunkRunes = 1500

	// Google Voice's page can optimistically clear/re-render its composer before
	// the previous send has settled at the service. Back-to-back clicks a few
	// hundred milliseconds apart made later chunks disappear even though the UI
	// driver reported success. Leave a small gap between confirmed chunks so a
	// long answer is delivered in order instead of only its first 1,500 chars.
	googleVoiceOutboundTextChunkGap = time.Second
)

// splitGoogleVoiceOutboundText divides one logical FlipAi reply into Google
// Voice sends of at most 1,500 Unicode characters. It deliberately does not add
// part numbers or otherwise rewrite the model's answer: 2,000 characters become
// a 1,500-character message followed by a 500-character message.
func splitGoogleVoiceOutboundText(body string) []string {
	runes := []rune(body)
	if len(runes) == 0 {
		return nil
	}
	chunks := make([]string, 0, (len(runes)+googleVoiceOutboundTextChunkRunes-1)/googleVoiceOutboundTextChunkRunes)
	for len(runes) > 0 {
		n := googleVoiceOutboundTextChunkRunes
		if len(runes) < n {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}

// sendGoogleVoiceTextChunks sends chunks sequentially so their order on the
// phone matches the model's answer. If one send fails, later chunks are not
// attempted; continuing would create a gap in the answer and obscure the real
// delivery error. Confirmed chunks are deliberately paced because Google Voice
// can drop rapid consecutive page sends while still clearing the composer.
func sendGoogleVoiceTextChunks(body string, send func(string) error) error {
	chunks := splitGoogleVoiceOutboundText(body)
	if len(chunks) == 0 {
		return errors.New("Google Voice SMS needs text")
	}
	for i, chunk := range chunks {
		if i > 0 {
			time.Sleep(googleVoiceOutboundTextChunkGap)
		}
		if err := send(chunk); err != nil {
			return err
		}
	}
	return nil
}
