//go:build !windows

package main

import (
	"context"
	"errors"
)

func runSystemTray(ctx context.Context, tooltip string, onOpen, onQuit func()) error {
	return errors.New("system tray is only available on Windows")
}

func acquireWatchdogInstance() (func(), bool, error) { return func() {}, true, nil }
func acquireHostInstance() (func(), bool, error)     { return func() {}, true, nil }
