package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/textproto"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

type googleCredentials struct {
	Installed struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		AuthURI      string `json:"auth_uri"`
		TokenURI     string `json:"token_uri"`
	} `json:"installed"`
	Web struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		AuthURI      string `json:"auth_uri"`
		TokenURI     string `json:"token_uri"`
	} `json:"web"`
}
type oauthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	Expiry       time.Time `json:"expiry"`
}
type gmailList struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}
type gmailRaw struct {
	ID           string `json:"id"`
	ThreadID     string `json:"threadId"`
	InternalDate string `json:"internalDate"`
	Snippet      string `json:"snippet"`
	Raw          string `json:"raw"`
}

type GmailMessage struct {
	ID, Subject, From, ReplyTo, AuthenticationResults, Body, Snippet string
	InternalDate                                                     time.Time
	Attachments                                                      []MailAttachment
}
type GmailClient struct {
	cfg       GmailConfig
	tokenFile string
	creds     googleCredentials
	http      *http.Client
	apiBase   string
	mu        sync.Mutex
	token     oauthToken
}

func NewGmailClient(cfg GmailConfig, tokenFile string) (*GmailClient, error) {
	b, err := os.ReadFile(cfg.CredentialsFile)
	if err != nil {
		return nil, fmt.Errorf("read Google credentials: %w", err)
	}
	var cr googleCredentials
	if err := json.Unmarshal(b, &cr); err != nil {
		return nil, err
	}
	if cr.Installed.ClientID == "" {
		return nil, errors.New("Google OAuth credentials must be a Desktop app (installed) client")
	}
	g := &GmailClient{cfg: cfg, tokenFile: tokenFile, creds: cr, http: &http.Client{Timeout: 25 * time.Second}, apiBase: "https://gmail.googleapis.com/gmail/v1"}
	if enc, err := os.ReadFile(tokenFile); err == nil {
		if plain, e := unprotect(enc); e == nil {
			_ = json.Unmarshal(plain, &g.token)
		}
	}
	return g, nil
}
func (g *GmailClient) cred() (id, secret, authURI, tokenURI string) {
	c := g.creds.Installed
	if authURI = c.AuthURI; authURI == "" {
		authURI = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if tokenURI = c.TokenURI; tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}
	return c.ClientID, c.ClientSecret, authURI, tokenURI
}
func (g *GmailClient) Authorized() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.token.RefreshToken != "" || g.token.AccessToken != ""
}
func (g *GmailClient) Test(ctx context.Context) error {
	var profile map[string]any
	return g.apiGET(ctx, g.apiBase+"/users/me/profile", &profile)
}
func (g *GmailClient) saveToken(t oauthToken) error {
	b, _ := json.Marshal(t)
	enc, err := protect(b)
	if err != nil {
		return err
	}
	if err := os.WriteFile(g.tokenFile, enc, 0600); err != nil {
		return err
	}
	g.mu.Lock()
	g.token = t
	g.mu.Unlock()
	return nil
}

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type GoogleOAuthAttempt struct{ State, Verifier, RedirectURI string }

func (g *GmailClient) AuthURL(redirectURI string) (string, GoogleOAuthAttempt, error) {
	id, _, auth, _ := g.cred()
	if id == "" {
		return "", GoogleOAuthAttempt{}, errors.New("missing client id")
	}
	state, err := randomURLSafe(24)
	if err != nil {
		return "", GoogleOAuthAttempt{}, err
	}
	verifier, err := randomURLSafe(48)
	if err != nil {
		return "", GoogleOAuthAttempt{}, err
	}
	a := GoogleOAuthAttempt{State: state, Verifier: verifier, RedirectURI: redirectURI}
	sum := sha256.Sum256([]byte(a.Verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	q := url.Values{"client_id": {id}, "redirect_uri": {redirectURI}, "response_type": {"code"}, "scope": {"https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/gmail.send"}, "access_type": {"offline"}, "prompt": {"consent"}, "state": {a.State}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	return auth + "?" + q.Encode(), a, nil
}
func (g *GmailClient) ExchangeCode(ctx context.Context, code string, a GoogleOAuthAttempt) error {
	id, secret, _, tokenURI := g.cred()
	form := url.Values{"code": {code}, "client_id": {id}, "client_secret": {secret}, "redirect_uri": {a.RedirectURI}, "grant_type": {"authorization_code"}, "code_verifier": {a.Verifier}}
	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURI, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("Google token exchange HTTP %d: %s", resp.StatusCode, string(body))
	}
	var t oauthToken
	if err := json.Unmarshal(body, &t); err != nil {
		return err
	}
	if t.RefreshToken == "" {
		g.mu.Lock()
		t.RefreshToken = g.token.RefreshToken
		g.mu.Unlock()
	}
	t.Expiry = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	return g.saveToken(t)
}
func (g *GmailClient) accessToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	t := g.token
	g.mu.Unlock()
	if t.AccessToken != "" && time.Until(t.Expiry) > 90*time.Second {
		return t.AccessToken, nil
	}
	if t.RefreshToken == "" {
		return "", errors.New("Gmail is not authorized")
	}
	id, secret, _, tokenURI := g.cred()
	form := url.Values{"client_id": {id}, "client_secret": {secret}, "refresh_token": {t.RefreshToken}, "grant_type": {"refresh_token"}}
	req, _ := http.NewRequestWithContext(ctx, "POST", tokenURI, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("Google refresh HTTP %d: %s", resp.StatusCode, string(body))
	}
	var n oauthToken
	if err := json.Unmarshal(body, &n); err != nil {
		return "", err
	}
	n.RefreshToken = t.RefreshToken
	n.Expiry = time.Now().Add(time.Duration(n.ExpiresIn) * time.Second)
	if err := g.saveToken(n); err != nil {
		return "", err
	}
	return n.AccessToken, nil
}
func (g *GmailClient) apiGET(ctx context.Context, u string, out any) error {
	tok, err := g.accessToken(ctx)
	if err != nil {
		return err
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("Gmail HTTP %d: %s", resp.StatusCode, string(b))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
}
func (g *GmailClient) List(ctx context.Context) ([]string, error) {
	type page struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		NextPageToken string `json:"nextPageToken"`
	}
	ids := make([]string, 0, 256)
	pageToken := ""
	for len(ids) < 2000 {
		u := g.apiBase + "/users/me/messages?maxResults=500&q=" + url.QueryEscape(g.cfg.SearchQuery)
		if pageToken != "" {
			u += "&pageToken=" + url.QueryEscape(pageToken)
		}
		var out page
		if err := g.apiGET(ctx, u, &out); err != nil {
			return nil, err
		}
		for _, m := range out.Messages {
			ids = append(ids, m.ID)
			if len(ids) >= 2000 {
				break
			}
		}
		if out.NextPageToken == "" || len(out.Messages) == 0 {
			break
		}
		pageToken = out.NextPageToken
	}
	return ids, nil
}
func (g *GmailClient) Get(ctx context.Context, id string) (GmailMessage, error) {
	var raw gmailRaw
	u := g.apiBase + "/users/me/messages/" + url.PathEscape(id) + "?format=raw"
	if err := g.apiGET(ctx, u, &raw); err != nil {
		return GmailMessage{}, err
	}
	data, err := base64.RawURLEncoding.DecodeString(raw.Raw)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(raw.Raw)
	}
	if err != nil {
		return GmailMessage{}, err
	}
	var dt time.Time
	if n, err := parseInt64(raw.InternalDate); err == nil {
		dt = time.UnixMilli(n)
	}
	return parseRawGmailMessage(id, data, raw.Snippet, dt)
}
func parseInt64(s string) (int64, error) { var n int64; _, e := fmt.Sscan(s, &n); return n, e }
func decodeTransfer(h mail.Header, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(h.Get("Content-Transfer-Encoding"))) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, r)
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	default:
		return r
	}
}

func extractMailBody(h mail.Header, r io.Reader) (string, error) {
	ct := h.Get("Content-Type")
	med, params, _ := mime.ParseMediaType(ct)
	if strings.HasPrefix(med, "multipart/") {
		mr := multipart.NewReader(r, params["boundary"])
		var plain, html []string
		for {
			p, e := mr.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				return "", e
			}
			b, _ := io.ReadAll(io.LimitReader(p, 2<<20))
			ph := mail.Header(textproto.MIMEHeader(p.Header))
			x, _ := extractMailBody(ph, bytes.NewReader(b))
			pct, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
			if strings.HasPrefix(pct, "text/plain") {
				plain = append(plain, x)
			} else if strings.HasPrefix(pct, "text/html") {
				html = append(html, x)
			}
		}
		if len(plain) > 0 {
			return strings.Join(plain, "\n"), nil
		}
		return stripHTML(strings.Join(html, "\n")), nil
	}
	r = decodeTransfer(h, r)
	b, _ := io.ReadAll(io.LimitReader(r, 2<<20))
	s := string(b)
	if strings.HasPrefix(med, "text/html") {
		s = stripHTML(s)
	}
	return strings.TrimSpace(s), nil
}

var reTags = regexp.MustCompile(`(?s)<[^>]*>`)
var reSpaces = regexp.MustCompile(`[ \t]+`)

func stripHTML(s string) string {
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = reTags.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = reSpaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func safeGoogleVoiceReplyAddress(v string) (string, error) {
	v = strings.TrimSpace(v)
	if strings.ContainsAny(v, "\r\n") {
		return "", errors.New("invalid reply address")
	}
	a, err := mail.ParseAddress(v)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.ToLower(a.Address), "@")
	if len(parts) != 2 || parts[1] != "txt.voice.google.com" {
		return "", errors.New("reply address is not txt.voice.google.com")
	}
	return a.Address, nil
}

func (g *GmailClient) SendText(ctx context.Context, to, body string) error {
	addr, err := safeGoogleVoiceReplyAddress(to)
	if err != nil {
		return err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("empty reply")
	}
	tok, err := g.accessToken(ctx)
	if err != nil {
		return err
	}
	rawMsg := "To: " + addr + "\r\nContent-Type: text/plain; charset=UTF-8\r\nMIME-Version: 1.0\r\n\r\n" + body
	payload, _ := json.Marshal(map[string]string{"raw": base64.RawURLEncoding.EncodeToString([]byte(rawMsg))})
	req, _ := http.NewRequestWithContext(ctx, "POST", g.apiBase+"/users/me/messages/send", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("Gmail send HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
