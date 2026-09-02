//go:build !windows

package main

import "context"

func augmentChatGPTASARProbe(ctx context.Context, p *chatGPTDirectProbeResult) error {
	return nil
}
