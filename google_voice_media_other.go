//go:build !windows

package main

import (
	"context"
	"errors"
)

func requestGoogleVoiceInboundMedia(context.Context, string, string, string, string, string) ([]MailAttachment, error) {
	return nil, errors.New("native Google Voice MMS capture is available on Windows only")
}
