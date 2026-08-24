package main

// Keep production Google Voice delivery on the threaded-reply path even if
// the MailClient interface evolves later. Test doubles may still use SendText.
var _ threadedReplySender = (*GmailClient)(nil)
var _ threadedReplySender = (*IMAPMailClient)(nil)
