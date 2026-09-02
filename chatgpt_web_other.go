//go:build !windows

package main

import "fmt"

func requestChatGPTWebDesktopAction(_ string, _ string) error {
	return fmt.Errorf("ChatGPT WebView integration is currently available on Windows only")
}
