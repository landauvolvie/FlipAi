//go:build !windows

package main

import (
	"context"
	"errors"
)

func requestGoogleVoiceText(context.Context, string, string, string) error {
	return errors.New("direct Google Voice SMS is available on Windows only")
}
