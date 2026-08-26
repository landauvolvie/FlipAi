package main

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseRawGmailMessageExtractsBlankFilenameAudioMP4(t *testing.T) {
	audio := []byte{0, 1, 2, 3, 4, 5, 6, 7}
	raw := strings.Join([]string{
		"From: Me (SMS) <18453842803.18453241813.test@txt.voice.google.com>",
		"To: test@example.com",
		"Subject: New text message from Me (845) 324-1813",
		"Authentication-Results: mx.google.com; dkim=pass header.d=google.com",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=flipai-test",
		"",
		"--flipai-test",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"Google Voice\r\nhttps://voice.google.com\r\nYOUR ACCOUNT\r\nHELP CENTER",
		"--flipai-test",
		"Content-Type: audio/mp4",
		"Content-Transfer-Encoding: base64",
		"",
		base64.StdEncoding.EncodeToString(audio),
		"--flipai-test--",
		"",
	}, "\r\n")

	m, err := parseRawGmailMessage("test-id", []byte(raw), "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Attachments) != 1 {
		t.Fatalf("attachments=%d, want 1", len(m.Attachments))
	}
	a := m.Attachments[0]
	if a.Filename != "" {
		t.Fatalf("filename=%q, want blank", a.Filename)
	}
	if a.MediaType != "audio/mp4" {
		t.Fatalf("media type=%q, want audio/mp4", a.MediaType)
	}
	if string(a.Data) != string(audio) {
		t.Fatalf("audio bytes changed: %v", a.Data)
	}
}

func TestAttachmentOnlyCommandRoutesWithoutLocalTranscription(t *testing.T) {
	m := GmailMessage{Attachments: []MailAttachment{{MediaType: "audio/mp4", Data: []byte{1, 2, 3}}}}
	rc, err := parseRemoteCommandForMessage("", Config{}, "C", m)
	if err != nil {
		t.Fatal(err)
	}
	if rc.Agent != "C" || rc.Text != "" {
		t.Fatalf("unexpected command: %+v", rc)
	}
}

func TestPrepareInboundBlankFilenameAndCodexNativeInputs(t *testing.T) {
	in := []MailAttachment{
		{MediaType: "image/png", Data: []byte{137, 80, 78, 71}},
		{MediaType: "audio/mp4", Data: []byte{0, 0, 0, 24}},
	}
	files, cleanup, err := prepareInboundAttachments(in)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if len(files) != 2 {
		t.Fatalf("files=%d, want 2", len(files))
	}
	for _, f := range files {
		if _, err := os.Stat(f.Path); err != nil {
			t.Fatalf("materialized attachment missing: %v", err)
		}
	}
	prompt := promptForInboundAttachments("do the command", files)
	if !strings.Contains(prompt, "audio attachment is the user's command") {
		t.Fatalf("voice-command instruction missing: %s", prompt)
	}
	items := codexInputForInbound(prompt, files)
	if len(items) != 3 {
		t.Fatalf("Codex inputs=%d, want text + image + audio", len(items))
	}
	if items[1]["type"] != "localImage" || items[2]["type"] != "localAudio" {
		t.Fatalf("unexpected Codex media inputs: %#v", items)
	}
}
