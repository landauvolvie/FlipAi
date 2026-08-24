//go:build windows

package main

import (
	"errors"
	"os"
	"time"
)

// writeClaudeInbox delivers one frame to a running session's inbox.
//
// On native Windows that inbox is a named pipe rather than a Unix socket, and
// Claude Code requires the connection's first line to be a valid auth line —
// see claudeInboxFrame, which builds both lines together so a caller cannot
// send the message without it.
//
// A named pipe with a single listener answers a second connection attempt with
// "all pipe instances are busy" while it services the first, which is ordinary
// contention rather than a failure, so a short retry runs before giving up.
func writeClaudeInbox(addr string, frame []byte) error {
	path := normalizeClaudeInboxAddr(addr)
	if path == "" {
		return errors.New("the live session reported no inbox address")
	}
	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		f, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			lastErr = err
			time.Sleep(100 * time.Millisecond)
			continue
		}
		_, werr := f.Write(frame)
		cerr := f.Close()
		if werr != nil {
			return werr
		}
		return cerr
	}
	return lastErr
}
