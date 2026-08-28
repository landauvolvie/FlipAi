package main

import (
	"strings"
	"testing"
)

// The installer runs under Start-Transcript, whose header is longer than the
// message the user gets. When the install failed, what they were shown was that
// header -- the machine name, the account, the PowerShell build numbers --
// truncated before it reached the sentence saying what went wrong.
func TestInstallFailuresReportTheErrorNotTheTranscriptHeader(t *testing.T) {
	log := `**********************
Windows PowerShell transcript start
Start time: 20260827200616
Username: LAND-D-UPSTATE\Volvie
RunAs User: LAND-D-UPSTATE\Volvie
Configuration Name:
Machine: LAND-D-UPSTATE (Microsoft Windows NT 10.0.26200.0)
Host Application: powershell.exe -File install-audio-bridge.ps1
Process ID: 26896
PSVersion: 5.1.26100.9168
PSEdition: Desktop
BuildVersion: 10.0.26100.9168
CLRVersion: 4.0.30319.42000
**********************
Validated driver signer: CN=SignPath Foundation, O=SignPath Foundation
INFO pnputil exit 0
INFO create-device-node exit 1
ERROR: creating a virtual audio device failed (nefcon exit 1)
**********************
Windows PowerShell transcript end
End time: 20260827200620
**********************`

	got := summarizeInstallLog(log, 700)
	if !strings.Contains(got, "creating a virtual audio device failed") {
		t.Fatalf("the reason the install failed was not reported: %q", got)
	}
	for _, noise := range []string{"transcript", "PSVersion", "Machine:", "Username:", "Process ID"} {
		if strings.Contains(got, noise) {
			t.Errorf("transcript boilerplate reached the user: %q contains %q", got, noise)
		}
	}
}

// When nothing raised, the last thing that happened is far more useful than the
// first -- which is all a head-truncated transcript ever shows.
func TestAnInstallLogWithNoErrorReportsTheEndNotTheStart(t *testing.T) {
	log := `**********************
Windows PowerShell transcript start
Machine: PC
**********************
Validated driver signer: CN=SignPath Foundation
INFO pnputil exit 0
INFO existing virtual audio devices: 0
INFO create-device-node exit 0
INFO virtual audio devices now: 1
INFO virtual audio devices now: 1`
	got := summarizeInstallLog(log, 400)
	if !strings.Contains(got, "virtual audio devices now: 1") {
		t.Fatalf("the end of the log was not reported: %q", got)
	}
	if strings.Contains(got, "Machine: PC") {
		t.Errorf("boilerplate reached the user: %q", got)
	}
}

func TestAnEmptyInstallLogSaysNothingRatherThanNoise(t *testing.T) {
	if got := summarizeInstallLog("", 200); got != "" {
		t.Errorf("an empty log produced %q", got)
	}
	onlyHeader := "**********************\nWindows PowerShell transcript start\nStart time: 1\n**********************"
	if got := summarizeInstallLog(onlyHeader, 200); got != "" {
		t.Errorf("a header-only log produced %q", got)
	}
}
