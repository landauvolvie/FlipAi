package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// WaitForChange uses RFC 2177 IMAP IDLE. Gmail sends an untagged EXISTS/RECENT
// response when new mail reaches the selected Inbox, waking the bridge without
// waiting for the fallback poll interval.
func (c *IMAPMailClient) WaitForChange(ctx context.Context) error {
	s, err := c.openIMAP(ctx)
	if err != nil {
		return err
	}
	defer s.close()

	_ = s.conn.SetDeadline(deadlineFor(ctx, 25*time.Minute))
	tag := s.nextTag()
	if _, err := fmt.Fprintf(s.conn, "%s IDLE\r\n", tag); err != nil {
		return err
	}
	cont, err := s.r.ReadString('\n')
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	if !strings.HasPrefix(strings.TrimSpace(cont), "+") {
		return fmt.Errorf("Gmail IMAP IDLE rejected: %s", strings.TrimSpace(cont))
	}

	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.conn.SetDeadline(time.Now())
		case <-stop:
		}
	}()
	defer close(stop)

	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		upper := strings.ToUpper(strings.TrimSpace(line))
		if strings.HasPrefix(upper, "* ") && (strings.Contains(upper, " EXISTS") || strings.Contains(upper, " RECENT")) {
			if _, err := fmt.Fprint(s.conn, "DONE\r\n"); err != nil {
				return err
			}
			for {
				line, err := s.r.ReadString('\n')
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					return err
				}
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, tag+" ") {
					parts := strings.Fields(trimmed)
					if len(parts) < 2 || !strings.EqualFold(parts[1], "OK") {
						return errors.New("Gmail IMAP IDLE did not terminate cleanly")
					}
					return nil
				}
			}
		}
	}
}
