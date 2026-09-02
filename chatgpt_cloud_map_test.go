package main

import (
	"strings"
	"testing"
)

func TestChatGPTCloudMapperFindsIndependentOAuthAndConversationShape(t *testing.T) {
	data := []byte(`
const authorize = "https://auth.openai.com/oauth/authorize?state=TRANSIENT";
const token = "https://auth.openai.com/oauth/token";
const oauth = {
  client_id: "public.desktop-client",
  redirect_uri: "codex://oauth/callback?state=TEMP_STATE#fragment",
  scope: "openid profile email offline_access",
  response_type: "code",
  grant_type: "authorization_code",
  code_challenge: "DO_NOT_RETURN_CHALLENGE",
  code_verifier: "DO_NOT_RETURN_VERIFIER"
};
fetch("https://chatgpt.com/backend-api/conversation?access_token=DO_NOT_RETURN", {
  method: "POST",
  headers: {
    "Authorization": "Bearer NEVER_RETURN_THIS",
    "Content-Type": "application/json",
    "OAI-Device-Id": "NEVER_RETURN_DEVICE_VALUE"
  },
  body: JSON.stringify({
    action: "next",
    messages: [],
    parent_message_id: "PRIVATE_PARENT_VALUE",
    model: "auto",
    conversation_id: null
  })
});
const stream = "text/event-stream";
`)
	got := extractChatGPTCloudMap(".vite/build/main.js", data)
	all := strings.Join(append(append(append(append(append(append(append(append(append(append(append([]string{}, got.OAuthEndpoints...), got.PublicClientIDs...), got.RedirectURIs...), got.OAuthScopes...), got.OAuthMechanics...), got.ConversationEndpoints...), got.HeaderNames...), got.RequestFields...), got.ConversationState...), got.StreamFormats...), got.SessionDependencies...), "\n")

	for _, want := range []string{
		"https://auth.openai.com/oauth/authorize",
		"https://auth.openai.com/oauth/token",
		"public.desktop-client",
		"codex://oauth/callback",
		"openid profile email offline_access",
		"code_challenge",
		"code_verifier",
		"/backend-api/conversation",
		"Authorization",
		"Content-Type",
		"OAI-Device-Id",
		"messages",
		"parent_message_id",
		"conversation_id",
		"action=next",
		"new-chat signal",
		"SSE content type",
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("cloud protocol map missing %q:\n%s", want, all)
		}
	}

	for _, forbidden := range []string{
		"TRANSIENT", "TEMP_STATE", "DO_NOT_RETURN", "NEVER_RETURN_THIS",
		"NEVER_RETURN_DEVICE_VALUE", "PRIVATE_PARENT_VALUE", "Bearer ", "access_token=",
	} {
		if strings.Contains(strings.ToLower(all), strings.ToLower(forbidden)) {
			t.Fatalf("cloud protocol map leaked value %q:\n%s", forbidden, all)
		}
	}
}

func TestChatGPTCloudMapperReportsHeaderNamesNotCredentialValues(t *testing.T) {
	data := []byte(`
const endpoint = "/backend-api/conversation";
const headers = {
  "Authorization": "Bearer SECRET_BEARER_VALUE",
  "Cookie": "session=SECRET_COOKIE_VALUE",
  "OpenAI-Sentinel-Chat-Requirements-Token": "SECRET_REQUIREMENTS_VALUE",
  "X-Test-Header": "SECRET_X_VALUE"
};
`)
	got := extractChatGPTCloudMap(".vite/build/worker.js", data)
	all := strings.Join(append(append(got.HeaderNames, got.SessionDependencies...), got.ConversationEndpoints...), "\n")
	for _, want := range []string{"Authorization", "Cookie", "OpenAI-Sentinel-Chat-Requirements-Token", "X-Test-Header"} {
		if !strings.Contains(all, want) {
			t.Fatalf("header-name map missing %q: %s", want, all)
		}
	}
	for _, forbidden := range []string{"SECRET_BEARER_VALUE", "SECRET_COOKIE_VALUE", "SECRET_REQUIREMENTS_VALUE", "SECRET_X_VALUE", "session="} {
		if strings.Contains(all, forbidden) {
			t.Fatalf("header-name map leaked %q: %s", forbidden, all)
		}
	}
}

func TestChatGPTCloudAssessmentAllowsExplicitOAuthProofOnlyWhenStaticPiecesExist(t *testing.T) {
	s := chatGPTCloudMapScan{
		OAuthEndpoints:        []string{"main.js -> https://auth.openai.com/oauth/authorize", "main.js -> https://auth.openai.com/oauth/token"},
		PublicClientIDs:       []string{"main.js -> public.desktop-client"},
		RedirectURIs:          []string{"main.js -> codex://oauth/callback"},
		OAuthMechanics:        []string{"main.js -> PKCE code_challenge", "main.js -> PKCE code_verifier"},
		ConversationEndpoints: []string{"main.js -> /backend-api/conversation"},
		RequestFields:         []string{"main.js -> messages", "main.js -> parent_message_id"},
		StreamFormats:         []string{"main.js -> SSE content type text/event-stream"},
	}
	got := assessChatGPTCloudMap(s)
	for _, want := range []string{"explicit user-authorized independent OAuth proof", "does not yet prove", "streaming mechanism"} {
		if !strings.Contains(got, want) {
			t.Fatalf("independent OAuth assessment missing %q: %s", want, got)
		}
	}
}

func TestChatGPTCloudAssessmentFallsBackWhenBrowserSessionContextIsRequired(t *testing.T) {
	s := chatGPTCloudMapScan{
		ConversationEndpoints: []string{"main.js -> /backend-api/conversation"},
		RequestFields:         []string{"main.js -> messages"},
		SessionDependencies:   []string{"main.js -> fetch credentials=include", "main.js -> cookie/session marker near Chat/auth code"},
	}
	got := assessChatGPTCloudMap(s)
	for _, want := range []string{"browser/session credential context", "dedicated signed-in WebView", "rather than copying the desktop profile"} {
		if !strings.Contains(got, want) {
			t.Fatalf("browser-session assessment missing %q: %s", want, got)
		}
	}
}

func TestChatGPTPublicRedirectSanitizerDropsTransientState(t *testing.T) {
	got, ok := chatGPTSafeRedirectURI("codex://oauth/callback?state=SECRET_STATE#frag")
	if !ok || got != "codex://oauth/callback" {
		t.Fatalf("unexpected redirect sanitizer result ok=%v value=%q", ok, got)
	}
}
