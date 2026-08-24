package main

import (
	"path/filepath"
	"testing"
)

// The Agents page used to show the result of the last Test button press and
// nothing else, so a bridge that was answering texts perfectly well still read
// as "Needs attention" until somebody pressed Test again. A finished turn is
// the most authoritative health signal available, and it now records one.
func TestFinishedTurnsRecordAgentHealth(t *testing.T) {
	dir := t.TempDir()
	b := NewBridge(defaultConfig(dir), filepath.Join(dir, "state.json"), State{}, nil, nil, nil)

	type record struct {
		agent  string
		ok     bool
		detail string
	}
	var got []record
	b.SetAgentResultSink(func(agent string, ok bool, detail string) {
		got = append(got, record{agent, ok, detail})
	})

	b.recordAgentResult("A", true, "answered an SMS turn")
	b.recordAgentResult("C", false, "codex fell over")
	// An unknown agent code must not invent a check for something that has none.
	b.recordAgentResult("", true, "nothing")

	if len(got) != 2 {
		t.Fatalf("want two records, got %d: %+v", len(got), got)
	}
	if got[0].agent != "claude" || !got[0].ok {
		t.Errorf("A must map to a healthy claude check, got %+v", got[0])
	}
	if got[1].agent != "codex" || got[1].ok {
		t.Errorf("C must map to a failed codex check, got %+v", got[1])
	}
}

// With no sink installed the bridge must simply not report, rather than panic.
// Every existing test constructs a Bridge without one.
func TestAgentResultWithoutASinkIsSafe(t *testing.T) {
	dir := t.TempDir()
	b := NewBridge(defaultConfig(dir), filepath.Join(dir, "state.json"), State{}, nil, nil, nil)
	b.recordAgentResult("A", true, "no sink installed")
}
