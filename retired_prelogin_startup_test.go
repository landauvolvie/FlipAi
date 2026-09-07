package main

import (
	"os"
	"strings"
	"testing"
)

func TestRetiredPreLoginStartupGuardStaysInWindowsBuild(t *testing.T) {
	b, err := os.ReadFile("retired_prelogin_startup_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		`mode == "--boot-task"`,
		`mode != "--watchdog"`,
		"ProcessIdToSessionId",
		"WTSGetActiveConsoleSessionId",
		"bestEffortDeleteRetiredBootTask",
		`"/Delete", "/TN", bootTaskName, "/F"`,
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("retired boot guard is missing %q", want)
		}
	}
}
