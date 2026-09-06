//go:build !windows

package main

import "errors"

var errCopilotChatWebViewWindowsOnly = errors.New("the dedicated Microsoft Copilot Chat browser is available on Windows only")

func platformStartCopilotChatLogin(string) error   { return errCopilotChatWebViewWindowsOnly }
func platformEnsureCopilotChatWorker(string) error { return errCopilotChatWebViewWindowsOnly }
func platformStopCopilotChatWorker(string) error   { return nil }
func copilotChatWorkerMain(string, bool)           {}
