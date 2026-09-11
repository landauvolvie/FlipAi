from pathlib import Path


def replace_once(path: str, old: str, new: str, label: str) -> None:
    p = Path(path)
    s = p.read_text()
    if new in s:
        return
    if old not in s:
        raise SystemExit(f"missing expected {path} block: {label}")
    p.write_text(s.replace(old, new, 1))


replace_once(
    "chatgpt_webview_windows.go",
    '''\tmux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {\n\t\tif r.Method != http.MethodPost {\n\t\t\thttp.Error(rw, "POST required", http.StatusMethodNotAllowed)\n\t\t\treturn\n\t\t}\n\t\tturn(rw, r, "Reply with exactly: FLIPAI_OK", true, browserModeChat)\n\t})''',
    '''\tmux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {\n\t\tif r.Method != http.MethodPost {\n\t\t\thttp.Error(rw, "POST required", http.StatusMethodNotAllowed)\n\t\t\treturn\n\t\t}\n\t\tif !authorized(r) {\n\t\t\thttp.Error(rw, "FlipAi token required", http.StatusForbidden)\n\t\t\treturn\n\t\t}\n\t\tif !chatGPTPageIsSignedIn(dev) {\n\t\t\trw.WriteHeader(http.StatusUnauthorized)\n\t\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "ChatGPT is not signed in inside FlipAi."})\n\t\t\treturn\n\t\t}\n\t\tmutateChatGPTRuntime(dataDir, func(s *ChatGPTWebRuntime) {\n\t\t\ts.Connected, s.SignedIn, s.LastEvent, s.LastError = true, true, "health-check-ok", ""\n\t\t})\n\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "detail": "signed-in browser session ready"})\n\t})''',
    "ChatGPT read-only test endpoint",
)

replace_once(
    "grok_chat_webview_windows.go",
    '''\tmux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {\n\t\tif r.Method != http.MethodPost {\n\t\t\thttp.Error(rw, "POST required", http.StatusMethodNotAllowed)\n\t\t\treturn\n\t\t}\n\t\tturn(rw, r, "Reply with exactly: FLIPAI_OK", true)\n\t})''',
    '''\tmux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {\n\t\tif r.Method != http.MethodPost {\n\t\t\thttp.Error(rw, "POST required", http.StatusMethodNotAllowed)\n\t\t\treturn\n\t\t}\n\t\tif !authorized(r) {\n\t\t\thttp.Error(rw, "FlipAi token required", http.StatusForbidden)\n\t\t\treturn\n\t\t}\n\t\tif !grokChatPageIsSignedIn(dev) {\n\t\t\trw.WriteHeader(http.StatusUnauthorized)\n\t\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Grok Chat is not signed in inside FlipAi."})\n\t\t\treturn\n\t\t}\n\t\tmutateGrokChatRuntime(dataDir, func(s *GrokChatWebRuntime) {\n\t\t\ts.Connected, s.SignedIn, s.LastEvent, s.LastError = true, true, "health-check-ok", ""\n\t\t})\n\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "detail": "signed-in browser session ready"})\n\t})''',
    "Grok read-only test endpoint",
)

replace_once(
    "claude_chat_webview_windows.go",
    '''\tmux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {\n\t\tif r.Method != http.MethodPost { http.Error(rw, "POST required", http.StatusMethodNotAllowed); return }\n\t\tturn(rw, r, "Reply with exactly: FLIPAI_OK", true, browserModeChat)\n\t})''',
    '''\tmux.HandleFunc("/test", func(rw http.ResponseWriter, r *http.Request) {\n\t\tif r.Method != http.MethodPost { http.Error(rw, "POST required", http.StatusMethodNotAllowed); return }\n\t\tif !authorized(r) { http.Error(rw, "FlipAi token required", http.StatusForbidden); return }\n\t\tif !claudeChatPageIsSignedIn(dev) {\n\t\t\trw.WriteHeader(http.StatusUnauthorized)\n\t\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Claude Chat is not signed in inside FlipAi."})\n\t\t\treturn\n\t\t}\n\t\tmutateClaudeChatRuntime(dataDir, func(s *ClaudeChatWebRuntime) {\n\t\t\ts.Connected, s.SignedIn, s.LastEvent, s.LastError = true, true, "health-check-ok", ""\n\t\t})\n\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": true, "detail": "signed-in browser session ready"})\n\t})''',
    "Claude read-only test endpoint",
)

Path("browser_agent_readonly_test_regression_test.go").write_text(r'''package main

import (
    "os"
    "strings"
    "testing"
)

func TestBrowserConnectionTestsNeverSendSyntheticPrompts(t *testing.T) {
    files := []string{
        "chatgpt_webview_windows.go",
        "claude_chat_webview_windows.go",
        "gemini_chat_webview_windows.go",
        "grok_chat_webview_windows.go",
        "copilot_chat_webview_windows.go",
        "muse_chat_webview_windows.go",
    }
    forbidden := []string{
        `turn(rw, r, "Reply with exactly: FLIPAI_OK"`,
        `turn(rw,r,"Reply with exactly: FLIPAI_OK"`,
    }
    for _, path := range files {
        raw, err := os.ReadFile(path)
        if err != nil {
            t.Fatalf("read %s: %v", path, err)
        }
        s := string(raw)
        for _, marker := range forbidden {
            if strings.Contains(s, marker) {
                t.Fatalf("%s still sends a synthetic FLIPAI_OK prompt from its test endpoint", path)
            }
        }
        if !strings.Contains(s, `HandleFunc("/test"`) {
            t.Fatalf("%s lost its test endpoint", path)
        }
    }
}
''')
