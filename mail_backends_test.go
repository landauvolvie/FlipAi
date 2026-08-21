package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func voiceFixture(body string) string {
	return "From: Google Voice <voice-noreply@google.com>\r\n" +
		"To: user@example.com\r\n" +
		"Reply-To: 18455551234.1234567890abcdef@txt.voice.google.com\r\n" +
		"Subject: New text message from (845) 555-1234\r\n" +
		"Date: Fri, 21 Aug 2026 18:10:00 -0400\r\n" +
		"Authentication-Results: mx.google.com; dkim=pass header.d=google.com\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n"
}

func fakeIMAPDial(raw string) func(context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			r := bufio.NewReader(server)
			_, _ = fmt.Fprint(server, "* OK fake Gmail IMAP ready\r\n")
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				line = strings.TrimSpace(line)
				f := strings.Fields(line)
				if len(f) < 2 {
					return
				}
				tag := f[0]
				upper := strings.ToUpper(line)
				switch {
				case strings.Contains(upper, " LOGIN "):
					_, _ = fmt.Fprintf(server, "%s OK LOGIN completed\r\n", tag)
				case strings.Contains(upper, " EXAMINE INBOX"):
					_, _ = fmt.Fprintf(server, "* 2 EXISTS\r\n%s OK EXAMINE completed\r\n", tag)
				case strings.Contains(upper, " UID SEARCH "):
					_, _ = fmt.Fprintf(server, "* SEARCH 41 42\r\n%s OK SEARCH completed\r\n", tag)
				case strings.Contains(upper, " UID FETCH 42 "):
					_, _ = fmt.Fprintf(server, "* 2 FETCH (UID 42 INTERNALDATE \"21-Aug-2026 18:10:00 -0400\" BODY[] {%d}\r\n", len(raw))
					_, _ = fmt.Fprint(server, raw)
					_, _ = fmt.Fprintf(server, ")\r\n%s OK FETCH completed\r\n", tag)
				case strings.Contains(upper, " UID FETCH 41 "):
					_, _ = fmt.Fprintf(server, "%s NO no such message\r\n", tag)
				case strings.Contains(upper, " LOGOUT"):
					_, _ = fmt.Fprintf(server, "* BYE bye\r\n%s OK LOGOUT completed\r\n", tag)
					return
				default:
					_, _ = fmt.Fprintf(server, "%s BAD unexpected command\r\n", tag)
				}
			}
		}()
		return client, nil
	}
}

func fakeSMTPDial(captured *string, mu *sync.Mutex) func(context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			r := bufio.NewReader(server)
			_, _ = fmt.Fprint(server, "220 localhost ESMTP fake\r\n")
			inData := false
			var data strings.Builder
			for {
				line, err := r.ReadString('\n')
				if err != nil {
					return
				}
				trimmed := strings.TrimRight(line, "\r\n")
				if inData {
					if trimmed == "." {
						mu.Lock()
						*captured = data.String()
						mu.Unlock()
						inData = false
						_, _ = fmt.Fprint(server, "250 queued\r\n")
						continue
					}
					data.WriteString(line)
					continue
				}
				upper := strings.ToUpper(trimmed)
				switch {
				case strings.HasPrefix(upper, "EHLO"):
					_, _ = fmt.Fprint(server, "250-localhost\r\n250-AUTH PLAIN\r\n250 OK\r\n")
				case strings.HasPrefix(upper, "AUTH PLAIN"):
					_, _ = fmt.Fprint(server, "235 2.7.0 Authentication successful\r\n")
				case strings.HasPrefix(upper, "MAIL FROM:"):
					_, _ = fmt.Fprint(server, "250 OK\r\n")
				case strings.HasPrefix(upper, "RCPT TO:"):
					_, _ = fmt.Fprint(server, "250 OK\r\n")
				case upper == "DATA":
					inData = true
					data.Reset()
					_, _ = fmt.Fprint(server, "354 End data with <CR><LF>.<CR><LF>\r\n")
				case upper == "QUIT":
					_, _ = fmt.Fprint(server, "221 Bye\r\n")
					return
				default:
					_, _ = fmt.Fprint(server, "250 OK\r\n")
				}
			}
		}()
		return client, nil
	}
}

func TestAppPasswordBackendIMAPAndSMTP(t *testing.T) {
	tmp := t.TempDir()
	secret := filepath.Join(tmp, "app.dat")
	if err := saveAppPasswordSecret(secret, "user@example.com", "abcd efgh ijkl mnop"); err != nil {
		t.Fatal(err)
	}
	c, err := NewIMAPMailClient(GmailConfig{SubjectPhrase: "new text message from"}, secret)
	if err != nil {
		t.Fatal(err)
	}
	raw := voiceFixture("482913 C: check GitHub")
	c.dialIMAP = fakeIMAPDial(raw)
	c.smtpServerName = "localhost"
	var sent string
	var mu sync.Mutex
	c.dialSMTP = fakeSMTPDial(&sent, &mu)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ids, err := c.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "42" || ids[1] != "41" {
		t.Fatalf("unexpected IMAP ids: %#v", ids)
	}
	m, err := c.Get(ctx, "42")
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "New text message from (845) 555-1234" || !strings.Contains(m.Body, "482913 C:") {
		t.Fatalf("unexpected parsed message: %#v", m)
	}
	cmd, ok := parseGoogleVoiceBody(m, "8455551234", "new text message from")
	if !ok || cmd != "482913 C: check GitHub" {
		t.Fatalf("voice parsing failed: %q %v", cmd, ok)
	}
	if err := c.SendText(ctx, m.ReplyTo, "Done from Codex"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotSent := sent
	mu.Unlock()
	if !strings.Contains(gotSent, "Done from Codex") || !strings.Contains(gotSent, "txt.voice.google.com") {
		t.Fatalf("SMTP reply missing content: %q", gotSent)
	}
}

func TestOAuthBackendAgainstFakeGmailAPI(t *testing.T) {
	tmp := t.TempDir()
	creds := filepath.Join(tmp, "creds.json")
	if err := os.WriteFile(creds, []byte(`{"installed":{"client_id":"id","client_secret":"secret","auth_uri":"http://example/auth","token_uri":"http://example/token"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(tmp, "token.dat")
	tb, _ := json.Marshal(oauthToken{AccessToken: "access", Expiry: time.Now().Add(time.Hour)})
	enc, err := protect(tb)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenFile, enc, 0600); err != nil {
		t.Fatal(err)
	}
	raw := voiceFixture("482913 A: check Gmail")
	var sendBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access" {
			http.Error(w, "missing bearer", 401)
			return
		}
		switch {
		case r.URL.Path == "/users/me/profile":
			_, _ = w.Write([]byte(`{"emailAddress":"user@example.com"}`))
		case r.URL.Path == "/users/me/messages" && r.Method == "GET":
			_, _ = w.Write([]byte(`{"messages":[{"id":"m42"}]}`))
		case r.URL.Path == "/users/me/messages/m42":
			out := gmailRaw{ID: "m42", InternalDate: fmt.Sprint(time.Now().UnixMilli()), Raw: base64.RawURLEncoding.EncodeToString([]byte(raw))}
			_ = json.NewEncoder(w).Encode(out)
		case r.URL.Path == "/users/me/messages/send" && r.Method == "POST":
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			b, _ := base64.RawURLEncoding.DecodeString(payload["raw"])
			sendBody = string(b)
			_, _ = w.Write([]byte(`{"id":"sent"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c, err := NewGmailClient(GmailConfig{CredentialsFile: creds, SearchQuery: `subject:"new text message from"`}, tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	c.apiBase = srv.URL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Test(ctx); err != nil {
		t.Fatal(err)
	}
	ids, err := c.List(ctx)
	if err != nil || len(ids) != 1 || ids[0] != "m42" {
		t.Fatalf("OAuth list failed: %v %#v", err, ids)
	}
	m, err := c.Get(ctx, "m42")
	if err != nil {
		t.Fatal(err)
	}
	cmd, ok := parseGoogleVoiceBody(m, "8455551234", "new text message from")
	if !ok || cmd != "482913 A: check Gmail" {
		t.Fatalf("OAuth message parsing failed: %q %v", cmd, ok)
	}
	if err := c.SendText(ctx, m.ReplyTo, "Done from Claude"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sendBody, "Done from Claude") || !strings.Contains(sendBody, "txt.voice.google.com") {
		t.Fatalf("OAuth send failed: %q", sendBody)
	}
}

func TestNoDefaultGmailMethod(t *testing.T) {
	cfg := defaultConfig(t.TempDir())
	if cfg.Gmail.Method != "" {
		t.Fatalf("new installs must have no Gmail default, got %q", cfg.Gmail.Method)
	}
}

func TestOAuthDesktopAuthorizationExchange(t *testing.T) {
	tmp := t.TempDir()
	var sawVerifier bool
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "test-code" || r.Form.Get("code_verifier") == "" {
			http.Error(w, "bad exchange", 400)
			return
		}
		sawVerifier = true
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer tokenSrv.Close()

	creds := filepath.Join(tmp, "creds.json")
	doc := map[string]any{"installed": map[string]any{
		"client_id": "desktop-id", "client_secret": "desktop-secret",
		"auth_uri": tokenSrv.URL + "/auth", "token_uri": tokenSrv.URL,
	}}
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(creds, b, 0600); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(tmp, "token.dat")
	c, err := NewGmailClient(GmailConfig{CredentialsFile: creds}, tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	authURL, attempt, err := c.AuthURL("http://127.0.0.1:8765/oauth/google/callback")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(authURL, "code_challenge=") || !strings.Contains(authURL, "gmail.readonly") {
		t.Fatalf("OAuth URL missing PKCE/scopes: %s", authURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.ExchangeCode(ctx, "test-code", attempt); err != nil {
		t.Fatal(err)
	}
	if !sawVerifier || !c.Authorized() {
		t.Fatal("OAuth exchange did not persist an authorized token")
	}
	enc, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := unprotect(enc)
	if err != nil || !strings.Contains(string(plain), "new-refresh") {
		t.Fatalf("protected OAuth token not persisted correctly: %v", err)
	}
}
