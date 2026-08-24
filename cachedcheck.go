package main

import (
	"sync"
	"time"
)

// Some state the UI shows comes from Windows itself — the Run registry value and
// the boot scheduled task — and reading it means launching reg.exe or
// schtasks.exe. The status snapshot is rebuilt on every page render and on
// every few-second poll, so those reads are cached briefly and invalidated the
// moment FlipAi changes the thing being read.
type cachedBool struct {
	mu   sync.Mutex
	at   time.Time
	val  bool
	ttl  time.Duration
	read func() bool
}

func newCachedBool(ttl time.Duration, read func() bool) *cachedBool {
	return &cachedBool{ttl: ttl, read: read}
}

func (c *cachedBool) get() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.IsZero() && time.Since(c.at) < c.ttl {
		return c.val
	}
	c.val = c.read()
	c.at = time.Now()
	return c.val
}

func (c *cachedBool) invalidate() {
	c.mu.Lock()
	c.at = time.Time{}
	c.mu.Unlock()
}
