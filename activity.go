package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ActivityEvent is intentionally metadata-only. It never stores SMS bodies,
// agent prompts/results, security codes, Gmail passwords, OAuth tokens, or API
// credentials. The log exists to make the bridge pipeline observable without
// turning diagnostics into a second copy of private message content.
type ActivityEvent struct {
	Time      time.Time `json:"time"`
	Level     string    `json:"level"`
	Stage     string    `json:"stage"`
	Message   string    `json:"message"`
	Sender    string    `json:"sender,omitempty"`
	Agent     string    `json:"agent,omitempty"`
	MessageID string    `json:"messageId,omitempty"`
}

type ActivityLog struct {
	path string
	mu   sync.Mutex
}

func activityLogForStatePath(statePath string) *ActivityLog {
	return &ActivityLog{path: filepath.Join(filepath.Dir(statePath), "activity.jsonl")}
}

func (l *ActivityLog) Add(level, stage, message, sender, agent, messageID string) {
	if l == nil || strings.TrimSpace(l.path) == "" {
		return
	}
	e := ActivityEvent{
		Time:      time.Now(),
		Level:     strings.TrimSpace(level),
		Stage:     strings.TrimSpace(stage),
		Message:   strings.TrimSpace(message),
		Sender:    normalizeUSPhone(sender),
		Agent:     strings.TrimSpace(agent),
		MessageID: strings.TrimSpace(messageID),
	}
	if e.Level == "" {
		e.Level = "info"
	}
	if e.Stage == "" {
		e.Stage = "bridge"
	}
	if e.Message == "" {
		return
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(l.path), 0700)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
	_ = f.Close()
	l.compactLocked()
}

func (l *ActivityLog) compactLocked() {
	st, err := os.Stat(l.path)
	if err != nil || st.Size() < 2<<20 {
		return
	}
	events := l.recentLocked(500)
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	enc := json.NewEncoder(f)
	for i := len(events) - 1; i >= 0; i-- {
		_ = enc.Encode(events[i])
	}
	_ = f.Close()
}

func (l *ActivityLog) Recent(limit int) []ActivityEvent {
	if l == nil {
		return nil
	}
	if limit < 1 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.recentLocked(limit)
}

func (l *ActivityLog) recentLocked(limit int) []ActivityEvent {
	f, err := os.Open(l.path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []ActivityEvent
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 32*1024), 512*1024)
	for s.Scan() {
		var e ActivityEvent
		if json.Unmarshal(s.Bytes(), &e) == nil {
			all = append(all, e)
		}
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	out := make([]ActivityEvent, len(all))
	for i := range all {
		out[i] = all[len(all)-1-i]
	}
	return out
}

func (l *ActivityLog) Clear() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
