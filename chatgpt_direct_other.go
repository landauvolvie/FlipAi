//go:build !windows

package main

import "context"

func platformProbeChatGPTDirect(context.Context) (chatGPTDirectProbeResult, error) {
	return chatGPTDirectProbeResult{
		Supported: false,
		Detail:    "Direct ChatGPT desktop discovery is currently implemented for Windows, which is the supported FlipAi desktop platform.",
	}, nil
}
