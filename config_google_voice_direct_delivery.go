package main

import "encoding/json"

// The direct Google Voice transport has its own exact 1,500-character splitter.
// The older bridge-level ReplyMaxChars/MaxReplyParts splitter predates that
// transport and is lossy: it numbers parts and drops everything beyond the part
// cap before GoogleVoiceSMSClient ever sees the answer. Keep those legacy knobs
// for Gmail/IMAP compatibility, but disable them whenever the configured inbox
// is FlipAi's direct Google Voice connection so the complete logical answer
// reaches the one canonical 1,500-character splitter.
const directGoogleVoiceLogicalReplyMaxRunes = 1 << 20

func (c *Config) UnmarshalJSON(data []byte) error {
	type plainConfig Config
	v := plainConfig(*c) // preserve defaults for fields omitted by older files
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*c = Config(v)
	if c.Gmail.Method == GmailMethodGoogleVoice {
		c.GoogleVoice.ReplyMaxChars = directGoogleVoiceLogicalReplyMaxRunes
		c.GoogleVoice.MaxReplyParts = 1
	}
	return nil
}
