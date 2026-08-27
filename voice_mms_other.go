//go:build !windows

package main

import (
	"context"
	"errors"
)

func sendGoogleVoiceImageMMS(context.Context, GmailMessage, string, *outboundVoiceImage) error {
	return errors.New("Google Voice image MMS delivery through the desktop UI is only available on Windows")
}
