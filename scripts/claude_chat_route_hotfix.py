from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, got {count}: {old[:120]!r}")
    p.write_text(text.replace(old, new, 1))


# v0.46.19 rendered forms for these three Claude Chat actions but forgot to
# register the action handlers in App.handler(), so every button hit ServeMux's
# 404 instead of the already-implemented claudeChat* handler.
replace_once(
    "webui.go",
    '''\t\t"/chatgpt/connect":       a.chatGPTConnect,\n\t\t"/chatgpt/test":          a.chatGPTTest,\n\t\t"/chatgpt/chat":          a.chatGPTChat,\n\t\t"/chatgpt/disconnect":    a.chatGPTDisconnect,\n''',
    '''\t\t"/chatgpt/connect":       a.chatGPTConnect,\n\t\t"/chatgpt/test":          a.chatGPTTest,\n\t\t"/chatgpt/chat":          a.chatGPTChat,\n\t\t"/chatgpt/disconnect":    a.chatGPTDisconnect,\n\t\t"/claude-chat/connect":   a.claudeChatConnect,\n\t\t"/claude-chat/test":      a.claudeChatTest,\n\t\t"/claude-chat/disconnect": a.claudeChatDisconnect,\n''',
)

# Route-level regression: GET must reach requirePost and return 405. Before the
# fix the exact same requests fell through to the local 404 handler. This test
# proves all three UI action URLs are actually wired without launching a browser.
Path("claude_chat_routes_test.go").write_text(r'''package main

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestClaudeChatActionRoutesAreRegistered(t *testing.T) {
    const token = "claude-chat-route-test-token"
    app := &App{
        dataDir: t.TempDir(),
        cfg:     Config{LocalToken: token},
    }
    handler := app.handler()

    for _, path := range []string{
        "/claude-chat/connect",
        "/claude-chat/test",
        "/claude-chat/disconnect",
    } {
        t.Run(path, func(t *testing.T) {
            req := httptest.NewRequest(http.MethodGet, path, nil)
            req.AddCookie(&http.Cookie{Name: "aisms_session", Value: token})
            rr := httptest.NewRecorder()

            handler.ServeHTTP(rr, req)

            if rr.Code != http.StatusMethodNotAllowed {
                t.Fatalf("%s status = %d; want %d so the registered action reaches requirePost instead of 404", path, rr.Code, http.StatusMethodNotAllowed)
            }
        })
    }
}
''')

# Hotfix release metadata.
replace_once("config.go", 'const version = "0.46.19"', 'const version = "0.46.20"')
Path("VERSION").write_text("0.46.20\n")
replace_once("installer/FlipAi.iss", '#define MyVersion "0.46.19"', '#define MyVersion "0.46.20"')
Path("docs/RELEASE-NOTES.md").write_text('''# FlipAi v0.46.20\n\nThis hotfix fixes the Claude Chat **Connect** screen showing `404 page not found`.\n\n## Claude Chat Connect works\n\nThe v0.46.19 Agents page correctly posted Connect, Test, and Disconnect to Claude Chat-specific URLs, and the Claude Chat handlers already existed, but those three URLs were accidentally omitted from FlipAi's local HTTP router. The embedded FlipAi window therefore received a local 404 before Claude sign-in could open.\n\nv0.46.20 registers all three Claude Chat action routes. Pressing **Connect** now reaches the Claude Chat sign-in handler and opens the dedicated persistent Claude browser session as intended. Test and Disconnect are wired through the same authenticated POST-only route table.\n\n## Regression protection\n\nA route-level test now requests all three Claude Chat action URLs and verifies that they reach FlipAi's POST-only action guard instead of falling through to 404. The normal Linux, Windows, race, browser, lifecycle, installer, and release gates still run before publishing.\n''')
