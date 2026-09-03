package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// chatGPTCloudMapScan is the final static-protocol pass before FlipAi attempts
// any independently authenticated ChatGPT cloud connection. It reads only the
// installed app package. Public protocol constants may be reported; session,
// token, cookie and credential values are never read or returned.
type chatGPTCloudMapScan struct {
	OAuthEndpoints        []string
	PublicClientIDs       []string
	RedirectURIs          []string
	OAuthScopes           []string
	OAuthMechanics        []string
	ConversationEndpoints []string
	HeaderNames           []string
	RequestFields         []string
	ConversationState     []string
	StreamFormats         []string
	SessionDependencies   []string
	Detail                string
	Assessment            string
}

var (
	chatGPTCloudAuthURLRegex      = regexp.MustCompile(`(?i)https://(?:auth|api)\.openai\.com/[A-Za-z0-9._~!$&'()*+,;=:@%/?#-]{0,300}`)
	chatGPTCloudConversationRegex = regexp.MustCompile(`(?i)(?:https://chatgpt\.com)?/(?:backend-api/)?conversation(?:/[A-Za-z0-9._~!$&'()*+,;=:@%/?#-]{0,260})?`)
	chatGPTCloudClientRegexes     = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:["'\x60]?(?:client_id|clientId)["'\x60]?)\s*[:=]\s*["'\x60]([^"'\x60\r\n]{3,220})`),
		regexp.MustCompile(`(?i)(?:set|append)\s*\(\s*["'\x60](?:client_id|clientId)["'\x60]\s*,\s*["'\x60]([^"'\x60\r\n]{3,220})`),
	}
	chatGPTCloudRedirectRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:["'\x60]?(?:redirect_uri|redirectUri)["'\x60]?)\s*[:=]\s*["'\x60]([^"'\x60\r\n]{3,320})`),
		regexp.MustCompile(`(?i)(?:set|append)\s*\(\s*["'\x60](?:redirect_uri|redirectUri)["'\x60]\s*,\s*["'\x60]([^"'\x60\r\n]{3,320})`),
	}
	chatGPTCloudScopeRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:["'\x60]?(?:scope|scopes)["'\x60]?)\s*[:=]\s*["'\x60]([^"'\x60\r\n]{2,360})`),
		regexp.MustCompile(`(?i)(?:set|append)\s*\(\s*["'\x60](?:scope|scopes)["'\x60]\s*,\s*["'\x60]([^"'\x60\r\n]{2,360})`),
	}
	chatGPTCloudHeaderRegex           = regexp.MustCompile(`(?i)["'\x60]((?:authorization|content-type|accept|cookie|origin|referer|user-agent|oai-[a-z0-9-]+|openai-[a-z0-9-]+|x-[a-z0-9-]+))["'\x60]\s*(?::|,)`)
	chatGPTCloudActionRegex           = regexp.MustCompile(`(?i)(?:["'\x60]?action["'\x60]?)\s*:\s*["'\x60]([A-Za-z0-9_-]{1,50})["'\x60]`)
	chatGPTCloudNullConversationRegex = regexp.MustCompile(`(?i)(?:conversation_id|conversationId)\s*:\s*(?:null|undefined)`)
)

var chatGPTCloudRequestKeys = map[string]bool{
	"action": true, "messages": true, "message": true, "author": true,
	"role": true, "content": true, "content_type": true, "parts": true,
	"model": true, "conversation_id": true, "conversationId": true,
	"parent_message_id": true, "parentMessageId": true, "message_id": true,
	"messageId": true, "timezone_offset_min": true,
	"history_and_training_disabled": true, "websocket_request_id": true,
	"websocketRequestId": true, "conversation_mode": true,
	"conversationMode": true, "force_paragen": true, "force_rate_limit": true,
	"suggestions": true, "supported_encodings": true, "system_hints": true,
	"metadata": true, "recipient": true, "status": true, "end_turn": true,
	"parent_id": true, "parentId": true,
}

func chatGPTCloudStripURL(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, "?#"); i >= 0 {
		v = v[:i]
	}
	if len(v) > 320 {
		v = v[:320]
	}
	return strings.TrimSpace(v)
}

func chatGPTSafePublicClientID(v string) bool {
	v = strings.TrimSpace(v)
	if len(v) < 6 || len(v) > 220 {
		return false
	}
	low := strings.ToLower(v)
	for _, bad := range []string{"secret", "token", "bearer", "cookie", "password", "session"} {
		if strings.Contains(low, bad) {
			return false
		}
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._~-", r)) {
			return false
		}
	}
	return true
}

func chatGPTSafeRedirectURI(v string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil || u.Scheme == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" && !strings.Contains(scheme, "openai") && !strings.Contains(scheme, "chatgpt") && !strings.Contains(scheme, "codex") {
		return "", false
	}
	u.RawQuery = ""
	u.Fragment = ""
	out := u.String()
	return out, out != "" && len(out) <= 320
}

func chatGPTSafeOAuthScope(v string) (string, bool) {
	parts := strings.Fields(strings.TrimSpace(v))
	if len(parts) == 0 || len(parts) > 24 {
		return "", false
	}
	for _, p := range parts {
		if len(p) > 80 {
			return "", false
		}
		for _, r := range p {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:/-", r)) {
				return "", false
			}
		}
	}
	return strings.Join(parts, " "), true
}

// chatGPTCloudRelevantText limits generic marker searches to code close to a
// Chat/auth anchor. This keeps a generic cookie helper elsewhere in a large
// bundle from falsely proving that the conversation request needs cookies.
func chatGPTCloudRelevantText(text string) string {
	low := strings.ToLower(text)
	anchors := []string{"auth.openai.com", "client_id", "code_challenge", "code_verifier", "redirect_uri", "backend-api", "/conversation", "conversationid", "conversation_id"}
	pieces := []string{}
	seen := map[int]bool{}
	for _, anchor := range anchors {
		from := 0
		for len(pieces) < 48 && from < len(low) {
			i := strings.Index(low[from:], anchor)
			if i < 0 {
				break
			}
			i += from
			bucket := i / 4000
			if !seen[bucket] {
				seen[bucket] = true
				start, end := i-7000, i+7000
				if start < 0 {
					start = 0
				}
				if end > len(text) {
					end = len(text)
				}
				pieces = append(pieces, text[start:end])
			}
			from = i + len(anchor)
		}
	}
	return strings.Join(pieces, "\n")
}

func extractChatGPTCloudMap(path string, data []byte) chatGPTCloudMapScan {
	var out chatGPTCloudMapScan
	text := string(data)
	relevant := chatGPTCloudRelevantText(text)
	if relevant == "" {
		return out
	}

	for _, raw := range chatGPTCloudAuthURLRegex.FindAllString(text, 80) {
		v := chatGPTCloudStripURL(raw)
		low := strings.ToLower(v)
		if strings.Contains(low, "authorize") || strings.Contains(low, "oauth") || strings.Contains(low, "/token") || strings.Contains(low, "/auth") {
			out.OAuthEndpoints = append(out.OAuthEndpoints, path+" -> "+v)
		}
	}
	for _, raw := range chatGPTCloudConversationRegex.FindAllString(text, 100) {
		if v := chatGPTCloudStripURL(raw); v != "" {
			out.ConversationEndpoints = append(out.ConversationEndpoints, path+" -> "+v)
		}
	}
	for _, re := range chatGPTCloudClientRegexes {
		for _, m := range re.FindAllStringSubmatch(relevant, 60) {
			if len(m) >= 2 && chatGPTSafePublicClientID(m[1]) {
				out.PublicClientIDs = append(out.PublicClientIDs, path+" -> "+strings.TrimSpace(m[1]))
			}
		}
	}
	for _, re := range chatGPTCloudRedirectRegexes {
		for _, m := range re.FindAllStringSubmatch(relevant, 60) {
			if len(m) >= 2 {
				if v, ok := chatGPTSafeRedirectURI(m[1]); ok {
					out.RedirectURIs = append(out.RedirectURIs, path+" -> "+v)
				}
			}
		}
	}
	for _, re := range chatGPTCloudScopeRegexes {
		for _, m := range re.FindAllStringSubmatch(relevant, 60) {
			if len(m) >= 2 {
				if v, ok := chatGPTSafeOAuthScope(m[1]); ok {
					out.OAuthScopes = append(out.OAuthScopes, path+" -> "+v)
				}
			}
		}
	}

	low := strings.ToLower(relevant)
	for needle, label := range map[string]string{
		"code_challenge_method": "PKCE code_challenge_method",
		"code_challenge":        "PKCE code_challenge",
		"code_verifier":         "PKCE code_verifier",
		"response_type":         "OAuth response_type",
		"grant_type":            "OAuth grant_type",
		"offline_access":        "OAuth offline_access scope marker",
	} {
		if strings.Contains(low, needle) {
			out.OAuthMechanics = append(out.OAuthMechanics, path+" -> "+label)
		}
	}
	for _, m := range chatGPTCloudHeaderRegex.FindAllStringSubmatch(relevant, 160) {
		if len(m) >= 2 {
			out.HeaderNames = append(out.HeaderNames, path+" -> "+strings.TrimSpace(m[1]))
		}
	}
	for _, m := range chatGPTStringKeyRegex.FindAllStringSubmatch(relevant, 900) {
		if len(m) >= 2 && chatGPTCloudRequestKeys[m[1]] {
			out.RequestFields = append(out.RequestFields, path+" -> "+m[1])
		}
	}
	for _, m := range chatGPTObjectKeyRegex.FindAllStringSubmatch(relevant, 900) {
		if len(m) >= 2 && chatGPTCloudRequestKeys[m[1]] {
			out.RequestFields = append(out.RequestFields, path+" -> "+m[1])
		}
	}
	for _, m := range chatGPTCloudActionRegex.FindAllStringSubmatch(relevant, 60) {
		if len(m) >= 2 {
			out.ConversationState = append(out.ConversationState, path+" -> action="+m[1])
		}
	}
	if chatGPTCloudNullConversationRegex.MatchString(relevant) {
		out.ConversationState = append(out.ConversationState, path+" -> new-chat signal: conversation_id null/undefined")
	}
	for _, key := range []string{"conversation_id", "conversationId", "parent_message_id", "parentMessageId", "message_id", "messageId", "websocket_request_id", "websocketRequestId"} {
		if strings.Contains(relevant, key) {
			out.ConversationState = append(out.ConversationState, path+" -> state key "+key)
		}
	}

	for needle, label := range map[string]string{
		"text/event-stream":    "SSE content type text/event-stream",
		"eventsource":          "EventSource/SSE",
		"websocket":            "WebSocket",
		"wss://chatgpt.com":    "ChatGPT WSS endpoint",
		"wss://ws.chatgpt.com": "ChatGPT ws.chatgpt.com endpoint",
		"[done]":               "SSE [DONE] terminator marker",
		"messageevent":         "MessageEvent",
		"event.data":           "event.data parser",
	} {
		if strings.Contains(low, needle) {
			out.StreamFormats = append(out.StreamFormats, path+" -> "+label)
		}
	}
	for needle, label := range map[string]string{
		"credentials:\"include\"":                 "fetch credentials=include",
		"credentials:'include'":                   "fetch credentials=include",
		"withcredentials":                         "withCredentials/browser session context",
		"oai-device-id":                           "OAI device-id header/state",
		"openai-sentinel-chat-requirements-token": "Sentinel chat-requirements token header",
		"chat-requirements":                       "chat requirements challenge",
		"requirements_token":                      "requirements token field",
		"arkose":                                  "Arkose challenge marker",
		"turnstile":                               "Turnstile challenge marker",
		"cf_clearance":                            "Cloudflare clearance cookie marker",
		"csrf":                                    "CSRF marker",
		"proof_token":                             "proof token field",
		"device_id":                               "device_id field",
		"\"cookie\"":                              "cookie/session marker near Chat/auth code",
		"'cookie'":                                "cookie/session marker near Chat/auth code",
	} {
		if strings.Contains(low, needle) {
			out.SessionDependencies = append(out.SessionDependencies, path+" -> "+label)
		}
	}

	out.OAuthEndpoints = uniqueSortedStrings(out.OAuthEndpoints)
	out.PublicClientIDs = uniqueSortedStrings(out.PublicClientIDs)
	out.RedirectURIs = uniqueSortedStrings(out.RedirectURIs)
	out.OAuthScopes = uniqueSortedStrings(out.OAuthScopes)
	out.OAuthMechanics = uniqueSortedStrings(out.OAuthMechanics)
	out.ConversationEndpoints = uniqueSortedStrings(out.ConversationEndpoints)
	out.HeaderNames = uniqueSortedStrings(out.HeaderNames)
	out.RequestFields = uniqueSortedStrings(out.RequestFields)
	out.ConversationState = uniqueSortedStrings(out.ConversationState)
	out.StreamFormats = uniqueSortedStrings(out.StreamFormats)
	out.SessionDependencies = uniqueSortedStrings(out.SessionDependencies)
	return out
}

func mergeChatGPTCloudMap(dst *chatGPTCloudMapScan, src chatGPTCloudMapScan) {
	dst.OAuthEndpoints = append(dst.OAuthEndpoints, src.OAuthEndpoints...)
	dst.PublicClientIDs = append(dst.PublicClientIDs, src.PublicClientIDs...)
	dst.RedirectURIs = append(dst.RedirectURIs, src.RedirectURIs...)
	dst.OAuthScopes = append(dst.OAuthScopes, src.OAuthScopes...)
	dst.OAuthMechanics = append(dst.OAuthMechanics, src.OAuthMechanics...)
	dst.ConversationEndpoints = append(dst.ConversationEndpoints, src.ConversationEndpoints...)
	dst.HeaderNames = append(dst.HeaderNames, src.HeaderNames...)
	dst.RequestFields = append(dst.RequestFields, src.RequestFields...)
	dst.ConversationState = append(dst.ConversationState, src.ConversationState...)
	dst.StreamFormats = append(dst.StreamFormats, src.StreamFormats...)
	dst.SessionDependencies = append(dst.SessionDependencies, src.SessionDependencies...)
}

func assessChatGPTCloudMap(s chatGPTCloudMapScan) string {
	endpoints := strings.ToLower(strings.Join(s.OAuthEndpoints, " "))
	mechanics := strings.ToLower(strings.Join(s.OAuthMechanics, " "))
	deps := strings.ToLower(strings.Join(s.SessionDependencies, " "))
	hasAuthorize := strings.Contains(endpoints, "authorize") || strings.Contains(endpoints, "oauth")
	hasTokenEndpoint := strings.Contains(endpoints, "/token")
	hasPKCE := strings.Contains(mechanics, "code_challenge") && strings.Contains(mechanics, "code_verifier")
	hasPublicClient := len(s.PublicClientIDs) > 0
	hasRedirect := len(s.RedirectURIs) > 0
	hasConversation := len(s.ConversationEndpoints) > 0
	hasRequest := len(s.RequestFields) > 0
	hasStreaming := len(s.StreamFormats) > 0
	hasBrowserState := strings.Contains(deps, "credentials=include") || strings.Contains(deps, "withcredentials") || strings.Contains(deps, "clearance cookie") || strings.Contains(deps, "cookie/session")
	hasChallenges := strings.Contains(deps, "arkose") || strings.Contains(deps, "turnstile") || strings.Contains(deps, "sentinel") || strings.Contains(deps, "chat requirements")

	switch {
	case hasPublicClient && hasRedirect && hasAuthorize && hasPKCE && hasConversation && hasRequest && !hasBrowserState:
		extra := ""
		if !hasTokenEndpoint {
			extra = " The token-exchange endpoint was not recovered as a literal, so it still has to be resolved from the same OAuth flow before any live login test."
		}
		if hasChallenges {
			extra += " The app also references anti-abuse/device challenge machinery, which must be satisfied legitimately rather than copied from the desktop session."
		}
		if hasStreaming {
			extra += " The response streaming mechanism is also statically identified."
		}
		return "Static app code contains a public OAuth client id, redirect URI, authorize/PKCE mechanics, and the regular Chat conversation request shape. That is enough evidence to build an explicit user-authorized independent OAuth proof without harvesting the desktop app's private session credentials; it does not yet prove ChatGPT will accept that client outside its own app." + extra
	case hasConversation && hasBrowserState:
		return "The regular Chat conversation protocol is mapped, but the same code also requests browser/session credential context. A clean independent OAuth path is not proven yet; if those cookies/session requirements are mandatory, a dedicated signed-in WebView will be the practical fallback rather than copying the desktop profile."
	case hasAuthorize && hasPKCE && !hasPublicClient:
		return "OAuth/PKCE is clearly present, but the public client id was not recovered as a safe literal. The next step is still static resolution of that public configuration before any live authentication attempt."
	case hasPublicClient && hasRedirect && hasAuthorize && !hasConversation:
		return "The independent OAuth configuration is statically mapped, but the regular Chat conversation call shape is still incomplete. Do not attempt authentication or message sending until both halves are attributable to the same Chat client path."
	case hasConversation:
		return "The cloud Chat conversation path is mapped, but a complete independent OAuth/PKCE configuration was not proven from static app code. Do not reuse desktop cookies/tokens to fill the gap."
	default:
		return "The package scan did not recover enough independently usable OAuth and regular-Chat request metadata to justify a live cloud authentication test."
	}
}

func scanOneChatGPTCloudMap(ctx context.Context, archivePath string) (chatGPTCloudMapScan, error) {
	var out chatGPTCloudMapScan
	_, entries, err := readChatGPTASARIndex(archivePath)
	if err != nil {
		return out, err
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return out, err
	}
	defer f.Close()

	var total int64
	inspected, matched := 0, 0
	const totalLimit = int64(256 << 20)
	const fileLimit = int64(32 << 20)
	for _, entry := range entries {
		if ctx.Err() != nil || total >= totalLimit {
			break
		}
		if entry.Unpacked || entry.Size <= 0 || entry.Size > fileLimit || !chatGPTASARCodePath(entry.Path) {
			continue
		}
		lowPath := strings.ToLower(entry.Path)
		if !(strings.Contains(lowPath, ".vite/build/") || strings.Contains(lowPath, "preload") || strings.Contains(lowPath, "main") || strings.Contains(lowPath, "worker") || strings.Contains(lowPath, "webview/") || strings.Contains(lowPath, "renderer")) {
			continue
		}
		limit := entry.Size
		if remain := totalLimit - total; remain < limit {
			limit = remain
		}
		b := make([]byte, int(limit))
		n, readErr := f.ReadAt(b, entry.Offset)
		if readErr != nil && readErr != io.EOF {
			continue
		}
		b = b[:n]
		total += int64(n)
		inspected++
		one := extractChatGPTCloudMap(entry.Path, b)
		if len(one.OAuthEndpoints)+len(one.PublicClientIDs)+len(one.ConversationEndpoints)+len(one.RequestFields) > 0 {
			matched++
		}
		mergeChatGPTCloudMap(&out, one)
	}

	out.OAuthEndpoints = uniqueSortedStrings(out.OAuthEndpoints)
	out.PublicClientIDs = uniqueSortedStrings(out.PublicClientIDs)
	out.RedirectURIs = uniqueSortedStrings(out.RedirectURIs)
	out.OAuthScopes = uniqueSortedStrings(out.OAuthScopes)
	out.OAuthMechanics = uniqueSortedStrings(out.OAuthMechanics)
	out.ConversationEndpoints = uniqueSortedStrings(out.ConversationEndpoints)
	out.HeaderNames = uniqueSortedStrings(out.HeaderNames)
	out.RequestFields = uniqueSortedStrings(out.RequestFields)
	out.ConversationState = uniqueSortedStrings(out.ConversationState)
	out.StreamFormats = uniqueSortedStrings(out.StreamFormats)
	out.SessionDependencies = uniqueSortedStrings(out.SessionDependencies)
	out.Assessment = assessChatGPTCloudMap(out)
	out.Detail = fmt.Sprintf("cloud-auth mapper inspected %d focused Electron app-code files (%d bytes); matched files=%d oauth endpoints=%d public client ids=%d redirects=%d scopes=%d conversation endpoints=%d header names=%d request fields=%d stream signals=%d session/challenge signals=%d", inspected, total, matched, len(out.OAuthEndpoints), len(out.PublicClientIDs), len(out.RedirectURIs), len(out.OAuthScopes), len(out.ConversationEndpoints), len(out.HeaderNames), len(out.RequestFields), len(out.StreamFormats), len(out.SessionDependencies))
	return out, nil
}

func scanChatGPTCloudMaps(ctx context.Context, roots []string) chatGPTCloudMapScan {
	var out chatGPTCloudMapScan
	seen := map[string]bool{}
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || root == "." || ctx.Err() != nil {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return nil
			}
			low := strings.ToLower(filepath.Clean(path))
			for _, privatePart := range []string{"\\user data\\", "\\local storage\\", "\\indexeddb\\", "\\session storage\\", "\\network\\", "\\cache\\", "\\gpucache\\"} {
				if strings.Contains(low, privatePart) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if d.IsDir() || !strings.EqualFold(d.Name(), "app.asar") {
				return nil
			}
			key := strings.ToLower(filepath.Clean(path))
			if seen[key] {
				return nil
			}
			seen[key] = true
			one, e := scanOneChatGPTCloudMap(ctx, path)
			if e != nil {
				if out.Detail == "" {
					out.Detail = "cloud-auth ASAR parse failed: " + e.Error()
				}
				return nil
			}
			mergeChatGPTCloudMap(&out, one)
			if one.Detail != "" {
				if out.Detail != "" {
					out.Detail += " | "
				}
				out.Detail += one.Detail
			}
			return nil
		})
	}
	out.OAuthEndpoints = uniqueSortedStrings(out.OAuthEndpoints)
	out.PublicClientIDs = uniqueSortedStrings(out.PublicClientIDs)
	out.RedirectURIs = uniqueSortedStrings(out.RedirectURIs)
	out.OAuthScopes = uniqueSortedStrings(out.OAuthScopes)
	out.OAuthMechanics = uniqueSortedStrings(out.OAuthMechanics)
	out.ConversationEndpoints = uniqueSortedStrings(out.ConversationEndpoints)
	out.HeaderNames = uniqueSortedStrings(out.HeaderNames)
	out.RequestFields = uniqueSortedStrings(out.RequestFields)
	out.ConversationState = uniqueSortedStrings(out.ConversationState)
	out.StreamFormats = uniqueSortedStrings(out.StreamFormats)
	out.SessionDependencies = uniqueSortedStrings(out.SessionDependencies)
	out.Assessment = assessChatGPTCloudMap(out)
	return out
}
