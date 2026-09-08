//go:build !windows

package main

import "errors"

var errMuseChatWebViewWindowsOnly = errors.New("the dedicated Muse browser is available on Windows only")

func platformStartMuseChatLogin(string) error   { return errMuseChatWebViewWindowsOnly }
func platformEnsureMuseChatWorker(string) error { return errMuseChatWebViewWindowsOnly }
func platformStopMuseChatWorker(string) error   { return nil }
func museChatWorkerMain(string, bool)           {}
