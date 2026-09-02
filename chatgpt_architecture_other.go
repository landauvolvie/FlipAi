//go:build !windows

package main

import "context"

func augmentChatGPTDirectProbe(ctx context.Context, p *chatGPTDirectProbeResult) error {
	_ = ctx
	_ = p
	return nil
}

func assessChatGPTDirectPath(p chatGPTDirectProbeResult) string {
	_ = p
	return "Direct ChatGPT architecture assessment is only available on Windows."
}
