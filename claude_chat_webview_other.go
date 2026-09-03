//go:build !windows

package main

import "errors"

var errClaudeChatWebViewWindowsOnly = errors.New("the dedicated Claude Chat browser is available on Windows only")

func platformStartClaudeChatLogin(string) error  { return errClaudeChatWebViewWindowsOnly }
func platformEnsureClaudeChatWorker(string) error { return errClaudeChatWebViewWindowsOnly }
func platformStopClaudeChatWorker(string) error    { return nil }
func claudeChatWorkerMain(string, bool)             {}
