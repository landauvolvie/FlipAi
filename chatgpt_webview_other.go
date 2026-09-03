//go:build !windows

package main

import "errors"

var errChatGPTWebViewWindowsOnly = errors.New("the dedicated ChatGPT browser is available on Windows only")

func platformStartChatGPTLogin(string) error   { return errChatGPTWebViewWindowsOnly }
func platformEnsureChatGPTWorker(string) error { return errChatGPTWebViewWindowsOnly }
func platformStopChatGPTWorker(string) error   { return nil }
func chatGPTWorkerMain(string, bool)           {}
