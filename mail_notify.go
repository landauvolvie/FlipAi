package main

import "context"

// MailChangeWaiter is optional. Backends that support a server-side mailbox
// notification channel (Gmail IMAP IDLE) implement it so Bridge can react to
// new mail immediately instead of polling.
type MailChangeWaiter interface {
	WaitForChange(context.Context) error
}
