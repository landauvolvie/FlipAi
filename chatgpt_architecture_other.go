//go:build !windows

package main

import "context"

func augmentChatGPTDirectProbe(ctx context.Context, p *chatGPTDirectProbeResult) error {
	_ = ctx
	_ = p
	return nil
}
