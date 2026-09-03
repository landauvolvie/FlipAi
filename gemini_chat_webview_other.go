//go:build !windows

package main

import "errors"

var errGeminiChatWebViewWindowsOnly = errors.New("the dedicated Gemini Chat browser is available on Windows only")

func platformStartGeminiChatLogin(string) error   { return errGeminiChatWebViewWindowsOnly }
func platformEnsureGeminiChatWorker(string) error { return errGeminiChatWebViewWindowsOnly }
func platformStopGeminiChatWorker(string) error   { return nil }
func geminiChatWorkerMain(string, bool)           {}
