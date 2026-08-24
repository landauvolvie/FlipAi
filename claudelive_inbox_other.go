//go:build !windows

package main

import (
	"errors"
	"net"
	"time"
)

// writeClaudeInbox is the non-Windows half of the inbox writer.
//
// FlipAi ships on Windows, where a session's inbox is a named pipe. Claude Code
// uses a Unix domain socket everywhere else, and implementing that path here
// keeps the whole delivery sequence — connect, auth line, message line —
// exercisable by a test on the machine the tests actually run on, instead of
// leaving the only implementation behind a build tag nothing can execute.
func writeClaudeInbox(addr string, frame []byte) error {
	path := normalizeClaudeInboxAddr(addr)
	if path == "" {
		return errors.New("the live session reported no inbox address")
	}
	conn, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	_, err = conn.Write(frame)
	return err
}
