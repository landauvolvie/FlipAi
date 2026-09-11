package main

import (
	"os"
	"time"
)

// removeBrowserProfileWithRetry waits for WebView2 helper processes to release
// profile lock files after the owning window has been terminated. WebView2 can
// keep EBWebView/lockfile open briefly after the control endpoint has returned,
// so a single immediate os.RemoveAll produces a false disconnect failure.
func removeBrowserProfileWithRetry(path string, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var last error
	for {
		last = os.RemoveAll(path)
		if last == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(250 * time.Millisecond)
	}
}
