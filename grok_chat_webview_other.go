//go:build !windows

package main

import "errors"

var errGrokChatWebViewWindowsOnly = errors.New("the dedicated Grok Chat browser is available on Windows only")

func platformStartGrokChatLogin(string) error   { return errGrokChatWebViewWindowsOnly }
func platformEnsureGrokChatWorker(string) error { return errGrokChatWebViewWindowsOnly }
func platformStopGrokChatWorker(string) error   { return nil }
func grokChatWorkerMain(string, bool)           {}
