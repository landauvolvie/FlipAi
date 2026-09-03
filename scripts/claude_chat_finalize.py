from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    s = p.read_text()
    count = s.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, got {count}: {old[:100]!r}")
    p.write_text(s.replace(old, new, 1))


# Claude Chat is a new independent agent. Existing phone/PIN permissions must
# not be copied into it automatically; users explicitly opt a number into H:.
replace_once('agents.go', '''func migrateClaudeChatAgent(cfg *Config) {
\tif cfg.Security.ClaudeChatAgentMigrated {
\t\treturn
\t}
\tcfg.Security.ClaudeChatAgentMigrated = true
\t// Existing installs already had C/A/G phone permissions. Giving the new
\t// browser agent the same SMS-capable numbers preserves the user's current
\t// remote access while still keeping Claude Chat independently editable.
\tmigrateBrowserChatAgent(cfg, "H", []string{"C", "A", "G"})
}''', '''func migrateClaudeChatAgent(cfg *Config) {
\tif cfg.Security.ClaudeChatAgentMigrated {
\t\treturn
\t}
\t// Claude Chat is a new security boundary. Mark the migration complete but
\t// start with no phone numbers or PIN copied from any existing agent.
\tcfg.Security.ClaudeChatAgentMigrated = true
}''')

# Update the existing three-agent UI regressions for the fourth independent pane.
replace_once('ui_page_agents_test.go', '`name="codexPrefix"`, `name="claudePrefix"`, `name="chatgptPrefix"`,', '`name="codexPrefix"`, `name="claudePrefix"`, `name="chatgptPrefix"`, `name="claudeChatPrefix"`,')
replace_once('ui_page_agents_test.go', '`name="codexRequireCode"`, `name="claudeRequireCode"`, `name="chatgptRequireCode"`,', '`name="codexRequireCode"`, `name="claudeRequireCode"`, `name="chatgptRequireCode"`, `name="claudeChatRequireCode"`,')
replace_once('ui_page_agents_test.go', '`name="codexAckDelay"`, `name="claudeAckDelay"`, `name="chatgptAckDelay"`,', '`name="codexAckDelay"`, `name="claudeAckDelay"`, `name="chatgptAckDelay"`, `name="claudeChatAckDelay"`,')
replace_once('ui_page_agents_test.go', 'if strings.Count(body, `name="sharedReplyStyle"`) != 3 {\n\t\tt.Fatalf("shared SMS instruction should be available from all three panes")\n\t}', 'if strings.Count(body, `name="sharedReplyStyle"`) != 4 {\n\t\tt.Fatalf("shared SMS instruction should be available from all four panes")\n\t}')
replace_once('ui_page_agents_test.go', '`name="codexPath"`, `name="claudePath"`, `name="permissionMode"`, `name="codexPrefix"`, `name="claudePrefix"`, `name="chatgptPrefix"`, `name="sharedReplyStyle"`', '`name="codexPath"`, `name="claudePath"`, `name="permissionMode"`, `name="codexPrefix"`, `name="claudePrefix"`, `name="chatgptPrefix"`, `name="claudeChatPrefix"`, `name="sharedReplyStyle"`')
replace_once('ui_page_agents_test.go', 'if cfg.Codex.Instruction != "" || cfg.Claude.Instruction != "" || cfg.ChatGPT.Instruction != "" {', 'if cfg.Codex.Instruction != "" || cfg.Claude.Instruction != "" || cfg.ChatGPT.Instruction != "" || cfg.ClaudeChat.Instruction != "" {')
replace_once('ui_page_agents_test.go', 'for _, agent := range []string{"C", "A", "G"} {', 'for _, agent := range []string{"C", "A", "G", "H"} {')
replace_once('ui_page_agents_test.go', '// FlipAi has one SMS instruction for all three agents.', '// FlipAi has one SMS instruction for all four agents.')

# Release metadata must stay in lockstep with the compiled version constant.
Path('VERSION').write_text('0.46.19\n')
replace_once('installer/FlipAi.iss', '#define MyVersion "0.46.18"', '#define MyVersion "0.46.19"')

Path('docs/RELEASE-NOTES.md').write_text('''# FlipAi v0.46.19\n\nThis release adds regular Claude Chat as a separate browser-backed FlipAi agent, alongside Claude Code and regular ChatGPT Chat.\n\n## Claude Chat agent\n\nClaude Chat signs into claude.ai through its own persistent FlipAi WebView2 profile. It has Connect, Test, and Disconnect controls, an independent H: SMS shortcut, sticky follow-up routing, NEW conversation support, its own allowed-phone list and security code, and the same receipt/progress controls as the other agents. Claude Code remains a separate agent and is not replaced.\n\nExisting phone-number permissions are not silently copied into Claude Chat. Add a number under Claude Chat before H: can receive texts.\n\n## RAM and lifecycle protection\n\nClaude Chat uses one process-level WebView2 owner protected by a Windows named mutex, cheap worker liveness checks, a persistent browser profile, controlled restart behavior, and explicit shutdown/update cleanup. A slow Claude renderer therefore cannot cause FlipAi to spawn repeated hidden browser trees and consume unbounded RAM.\n\n## Verification\n\nThe release is gated by the normal Linux and Windows Go tests, vet/race checks, Windows x64 build, Google Voice embedded-browser checks, installer build/smoke test, Defender scan when available, and the release workflow before the installer is published.\n''')
