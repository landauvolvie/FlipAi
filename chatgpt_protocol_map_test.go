package main

import (
	"strings"
	"testing"
)

func TestChatGPTProtocolMapperFindsBridgeAndRequestShape(t *testing.T) {
	data := []byte(`
const { contextBridge, ipcRenderer, ipcMain } = require("electron");
contextBridge.exposeInMainWorld("electronBridge", {
  getAccount: () => ipcRenderer.invoke("chatgpt:get-account"),
  sendConversation: (body) => ipcRenderer.invoke("chatgpt:send-conversation", body),
});
ipcMain.handle("chatgpt:send-conversation", async (_event, body) => {
  return fetch("https://chatgpt.com/backend-api/conversation", {
    method: "POST",
    body: JSON.stringify({ action: "next", messages: [], parent_message_id: "x", model: "auto" })
  });
});
const auth = "https://auth.openai.com/oauth/authorize";
const oauth = { client_id: "public-client", redirect_uri: "codex://auth", code_challenge: "pkce" };
const socket = "wss://chatgpt.com/conversation";
`)
	got := extractChatGPTProtocolMap(".vite/build/preload.js", data)
	joined := strings.Join(append(append(append(append(append(got.BridgeExposures, got.BridgeMethods...), got.IPCBindings...), got.BackendRoutes...), got.RequestShapeKeys...), got.AuthFlowMarkers...), "\n")
	for _, want := range []string{
		"contextBridge exposes electronBridge",
		"chatgpt:send-conversation",
		"/backend-api/conversation",
		"action",
		"messages",
		"parent_message_id",
		"model",
		"OAuth authorize endpoint",
		"PKCE code_challenge",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("protocol map missing %q:\n%s", want, joined)
		}
	}
}

func TestChatGPTProtocolMapperNeverReturnsCredentialValues(t *testing.T) {
	data := []byte(`
const endpoint = "https://chatgpt.com/backend-api/conversation?access_token=SUPER_SECRET";
const headers = { Authorization: "Bearer PRIVATE_VALUE", cookie: "session=NOPE" };
const oauth = { client_id: "public-client", code_verifier: "PRIVATE_PKCE_VALUE" };
contextBridge.exposeInMainWorld("electronBridge", { getToken: () => "NOPE" });
`)
	got := extractChatGPTProtocolMap(".vite/build/preload.js", data)
	all := strings.Join(append(append(append(append(append(append(append(got.BridgeExposures, got.BridgeMethods...), got.IPCBindings...), got.BackendRoutes...), got.RequestShapeKeys...), got.AuthFlowMarkers...), got.TransportSignals...), got.ExternalSignals...), "\n")
	low := strings.ToLower(all)
	for _, forbidden := range []string{"super_secret", "private_value", "private_pkce_value", "bearer ", "session=nope", "access_token="} {
		if strings.Contains(low, strings.ToLower(forbidden)) {
			t.Fatalf("protocol map leaked forbidden value %q:\n%s", forbidden, all)
		}
	}
}

func TestSanitizeChatGPTRouteDropsQueryAndFragment(t *testing.T) {
	got := sanitizeChatGPTRoute("https://chatgpt.com/backend-api/conversation?token=x#frag")
	if got != "https://chatgpt.com/backend-api/conversation" {
		t.Fatalf("unexpected sanitized route: %q", got)
	}
}
