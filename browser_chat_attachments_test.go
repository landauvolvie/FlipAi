package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPhoneVoiceNoteTypesAndFilenames(t *testing.T) {
	for _, tc := range []struct{ declared, name, want string }{
		{"application/octet-stream", "recording.M4A", "audio/mp4"},
		{"", "voice.amr", "audio/amr"}, {"application/x-download", "voice.3gp", "audio/3gpp"},
		{"audio/x-m4a", "voice.bin", "audio/mp4"}, {"audio/x-wav", "voice", "audio/wav"},
		{"application/ogg", "voice.opus", "audio/ogg"}, {"", "voice.mp3", "audio/mpeg"},
		{"", "voice.aac", "audio/aac"}, {"", "voice.flac", "audio/flac"},
		{"text/html", "error.mp3", "text/html"},
	} {
		if got := inboundMediaType(tc.declared, tc.name); got != tc.want {
			t.Errorf("%s/%s = %s, want %s", tc.declared, tc.name, got, tc.want)
		}
	}
	for _, name := range []string{"recording", "voice.bin", ""} {
		if got := safeAttachmentFilename(name, 0, "audio/mp4"); !strings.HasSuffix(got, ".m4a") {
			t.Errorf("voice note has no usable extension: %s", got)
		}
	}
}

func TestBrowserAgentsRetainVoiceNoteBytesAndRouting(t *testing.T) {
	input := []MailAttachment{{Filename: "note.M4A", MediaType: "application/octet-stream", Data: []byte("voice-note-test-bytes")}}
	if !hasSupportedMailAttachments(input) {
		t.Fatal("binary voice note was dropped")
	}
	files, cleanup, err := prepareInboundAttachments(input)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	prepared, err := preparedBrowserChatImages(files)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := browserChatAttachmentMarker(prepared)
	if err != nil {
		t.Fatal(err)
	}
	clean, got, found, err := extractBrowserChatAttachmentMarker(marker + "\nlisten")
	if err != nil || !found || strings.TrimSpace(clean) != "listen" || len(got) != 1 {
		t.Fatalf("marker roundtrip: %s %+v %v", clean, got, err)
	}
	raw, err := os.ReadFile(got[0].Path)
	if err != nil || string(raw) != string(input[0].Data) || got[0].MediaType != "audio/mp4" {
		t.Fatal("recording bytes/type were lost")
	}
	cfg := defaultConfig(t.TempDir())
	for _, agent := range []string{"G", "H", "M", "X", "P", "U"} {
		cmd, err := parseBrowserChatAttachmentOnlyCommand(cfg, agent, GmailMessage{Attachments: input})
		if err != nil || cmd.Agent != agent {
			t.Fatalf("%s rejected voice note: %+v %v", agent, cmd, err)
		}
	}
	if !strings.Contains(browserChatAttachmentOnlyPrompt(prepared), "Listen to it") {
		t.Fatal("voice-only message has no spoken-command instruction")
	}
}

func TestGenericMIMEVoiceNoteIsExtracted(t *testing.T) {
	raw := "From: test@example.invalid\r\nSubject: Voice note\r\nContent-Type: multipart/mixed; boundary=voice\r\n\r\n--voice\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=recording.amr\r\n\r\n#!AMR\nbytes\r\n--voice--\r\n"
	m, err := parseRawGmailMessage("voice", []byte(raw), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].MediaType != "audio/amr" {
		t.Fatalf("MIME recording lost: %+v", m.Attachments)
	}
}

func TestVoiceNoteUploadInRealBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser harness")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	pw := playwrightModule(t)
	files, cleanup, err := prepareInboundAttachments([]MailAttachment{{Filename: "voice-note.m4a", MediaType: "audio/mp4", Data: []byte("test recording bytes")}})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	prepared, err := preparedBrowserChatImages(files)
	if err != nil {
		t.Fatal(err)
	}
	scripts := map[string]any{"picker": browserChatFindFileInputJS(prepared), "begin": browserChatAudioUploadJS(prepared, true), "status": browserChatAudioUploadJS(prepared, false), "capture": googleVoiceMediaCaptureExpression("MMS Received"), "path": files[0].Path, "name": filepath.Base(files[0].Path)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(scripts)
	}))
	defer srv.Close()
	cmd := exec.Command("node", filepath.Join("testdata", "voice-note-attachments.mjs"))
	cmd.Env = append(os.Environ(), "FLIPAI_VOICE_NOTE_TEST_URL="+srv.URL, "FLIPAI_PLAYWRIGHT_MODULE="+pw)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("voice-note browser: %v\n%s", err, out)
	}
}
