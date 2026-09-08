from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"expected patch anchor not found in {path}: {old[:100]!r}")
    p.write_text(text.replace(old, new, 1), encoding="utf-8")


# Central SMS execution: without these cases U would fall through to Codex.
replace_once(
    "bridge.go",
    '\t\tcase "P":\n\t\t\terr = b.newCopilotChatConversation(ctx)\n\t\t\tfinal = "New Microsoft Copilot Chat conversation started."\n\t\tdefault:',
    '\t\tcase "P":\n\t\t\terr = b.newCopilotChatConversation(ctx)\n\t\t\tfinal = "New Microsoft Copilot Chat conversation started."\n\t\tcase "U":\n\t\t\terr = b.newMuseChatConversation(ctx)\n\t\t\tfinal = "New Muse conversation started."\n\t\tdefault:',
)
replace_once(
    "bridge.go",
    '\t} else if rc.Agent == "P" {\n\t\tb.event("info", "agent", "Microsoft Copilot Chat command started", rc.Sender, "P", m.ID)\n\t\tfinal, err = b.runCopilotChatSMS(ctx, rc.Text)\n\t} else {\n\t\tb.event("info", "agent", "Codex command started", rc.Sender, "C", m.ID)',
    '\t} else if rc.Agent == "P" {\n\t\tb.event("info", "agent", "Microsoft Copilot Chat command started", rc.Sender, "P", m.ID)\n\t\tfinal, err = b.runCopilotChatSMS(ctx, rc.Text)\n\t} else if rc.Agent == "U" {\n\t\tb.event("info", "agent", "Muse command started", rc.Sender, "U", m.ID)\n\t\tfinal, err = b.runMuseChatSMS(ctx, rc.Text)\n\t} else {\n\t\tb.event("info", "agent", "Codex command started", rc.Sender, "C", m.ID)',
)

# Register Muse's authenticated local control routes.
replace_once(
    "webui.go",
    '\tm.HandleFunc("/copilot-chat/status.json", a.requireAuth(a.copilotChatStatusJSON))\n',
    '\tm.HandleFunc("/copilot-chat/status.json", a.requireAuth(a.copilotChatStatusJSON))\n\tm.HandleFunc("/muse-chat/status.json", a.requireAuth(a.museChatStatusJSON))\n',
)
replace_once(
    "webui.go",
    '\t\t"/copilot-chat/disconnect": a.copilotChatDisconnect,\n\t\t"/agents/numbers/add":',
    '\t\t"/copilot-chat/disconnect": a.copilotChatDisconnect,\n\t\t"/muse-chat/connect":       a.museChatConnect,\n\t\t"/muse-chat/test":          a.museChatTest,\n\t\t"/muse-chat/disconnect":    a.museChatDisconnect,\n\t\t"/agents/numbers/add":',
)

# New installs and clean shutdown know about the Muse browser profile.
replace_once(
    "main.go",
    '\t\tcfg.Security.GrokChatAgentMigrated = true\n\t\tif err := saveConfig',
    '\t\tcfg.Security.GrokChatAgentMigrated = true\n\t\tcfg.Security.CopilotChatAgentMigrated = true\n\t\tcfg.Security.MuseChatAgentMigrated = true\n\t\tif err := saveConfig',
)
replace_once(
    "main.go",
    '!claudeChatBrowserStillOpen(dataDir) && !geminiChatBrowserStillOpen(dataDir) && !grokChatBrowserStillOpen(dataDir) {',
    '!claudeChatBrowserStillOpen(dataDir) && !geminiChatBrowserStillOpen(dataDir) && !grokChatBrowserStillOpen(dataDir) && !copilotChatBrowserStillOpen(dataDir) && !museChatBrowserStillOpen(dataDir) {',
)
replace_once(
    "main.go",
    '\tgo app.watchForUpdates(ctx)\n\tgo func() {',
    '\tgo app.watchForUpdates(ctx)\n\t// Muse restores a saved connected session after a FlipAi restart instead of\n\t// waiting for the next SMS to discover that its worker is gone.\n\tgo runMuseChatBackgroundSupervisor(ctx, dataDir)\n\tgo func() {',
)

# Agents page data model and field names.
replace_once(
    "ui_page_agents.go",
    '\tCopilotChatAccess agentAccessView\n}',
    '\tCopilotChatAccess agentAccessView\n\tMuseChatAccess    agentAccessView\n}',
)
replace_once(
    "ui_page_agents.go",
    '\tcase "P":\n\t\tprefix = "copilotChat"\n\t}\n',
    '\tcase "P":\n\t\tprefix = "copilotChat"\n\tcase "U":\n\t\tprefix = "museChat"\n\t}\n',
)
replace_once(
    "ui_page_agents.go",
    "Grok Chat, and Microsoft Copilot Chat all receive this same line.",
    "Grok Chat, Microsoft Copilot Chat, and Muse all receive this same line.",
)
replace_once(
    "ui_page_agents.go",
    '\tview.CopilotChatAccess = newAgentAccessView(cfg, "P", configuredCopilotChatPrefix(cfg))\n\ta.render',
    '\tview.CopilotChatAccess = newAgentAccessView(cfg, "P", configuredCopilotChatPrefix(cfg))\n\tview.MuseChatAccess = newAgentAccessView(cfg, "U", configuredMuseChatPrefix(cfg))\n\ta.render',
)
replace_once(
    "ui_page_agents.go",
    'SMSOnly: agent == "G" || agent == "H" || agent == "M" || agent == "X" || agent == "P",',
    'SMSOnly: agent == "G" || agent == "H" || agent == "M" || agent == "X" || agent == "P" || agent == "U",',
)

# Saving the Muse pane must persist its own access/PIN settings and prefix.
replace_once(
    "ui_actions.go",
    '|| r.Form.Has("copilotChatPrefix") || r.Form.Has("newSessionCommand") {',
    '|| r.Form.Has("copilotChatPrefix") || r.Form.Has("museChatPrefix") || r.Form.Has("newSessionCommand") {',
)
replace_once(
    "ui_actions.go",
    'codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, copilotChatPrefix, newSession := configuredCodexPrefix(*cfg), configuredClaudePrefix(*cfg), configuredChatGPTPrefix(*cfg), configuredClaudeChatPrefix(*cfg), configuredGeminiChatPrefix(*cfg), configuredGrokChatPrefix(*cfg), configuredCopilotChatPrefix(*cfg), configuredNewSessionCommand(*cfg)',
    'codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, copilotChatPrefix, museChatPrefix, newSession := configuredCodexPrefix(*cfg), configuredClaudePrefix(*cfg), configuredChatGPTPrefix(*cfg), configuredClaudeChatPrefix(*cfg), configuredGeminiChatPrefix(*cfg), configuredGrokChatPrefix(*cfg), configuredCopilotChatPrefix(*cfg), configuredMuseChatPrefix(*cfg), configuredNewSessionCommand(*cfg)',
)
replace_once(
    "ui_actions.go",
    '\t\t\tif r.Form.Has("copilotChatPrefix") {\n\t\t\t\tcopilotChatPrefix, err = validateCommandToken(r.FormValue("copilotChatPrefix"), "Microsoft Copilot Chat shortcut")\n\t\t\t\tif err != nil {\n\t\t\t\t\treturn err\n\t\t\t\t}\n\t\t\t}\n\t\t\tprefixes := []string{codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, copilotChatPrefix}',
    '\t\t\tif r.Form.Has("copilotChatPrefix") {\n\t\t\t\tcopilotChatPrefix, err = validateCommandToken(r.FormValue("copilotChatPrefix"), "Microsoft Copilot Chat shortcut")\n\t\t\t\tif err != nil {\n\t\t\t\t\treturn err\n\t\t\t\t}\n\t\t\t}\n\t\t\tif r.Form.Has("museChatPrefix") {\n\t\t\t\tmuseChatPrefix, err = validateCommandToken(r.FormValue("museChatPrefix"), "Muse shortcut")\n\t\t\t\tif err != nil {\n\t\t\t\t\treturn err\n\t\t\t\t}\n\t\t\t}\n\t\t\tprefixes := []string{codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, copilotChatPrefix, museChatPrefix}',
)
replace_once(
    "ui_actions.go",
    'return fmt.Errorf("Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, Grok Chat, and Microsoft Copilot Chat shortcuts must all be different")',
    'return fmt.Errorf("Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, Grok Chat, Microsoft Copilot Chat, and Muse shortcuts must all be different")',
)
replace_once(
    "ui_actions.go",
    'cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.GrokChatPrefix, cfg.CopilotChatPrefix, cfg.NewSessionCommand = codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, copilotChatPrefix, newSession',
    'cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.GrokChatPrefix, cfg.CopilotChatPrefix, cfg.MuseChatPrefix, cfg.NewSessionCommand = codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, copilotChatPrefix, museChatPrefix, newSession',
)
replace_once(
    "ui_actions.go",
    'for _, agent := range []string{"C", "A", "G", "H", "M", "X", "P"} {',
    'for _, agent := range []string{"C", "A", "G", "H", "M", "X", "P", "U"} {',
)
replace_once(
    "ui_actions.go",
    '\t\tcfg.CopilotChat.Instruction = ""\n',
    '\t\tcfg.CopilotChat.Instruction = ""\n\t\tcfg.MuseChat.Instruction = ""\n',
)

# Make Muse part of the final Agents-page presentation pass.
replace_once(
    "zzzzzzzzzzzzzz_sms_route_ui.go",
    'registerPage("agents", smsRouteAgentsUI(copilotChatDirectUI(exactWebAgentsHTML())))',
    'registerPage("agents", smsRouteAgentsUI(museChatDirectUI(copilotChatDirectUI(exactWebAgentsHTML()))))',
)
replace_once(
    "zzzzzzzzzzzzzz_sms_route_ui.go",
    "M = Microsoft Copilot · X = Grok.",
    "M = Microsoft Copilot · MU = Muse · X = Grok.",
)
replace_once(
    "zzzzzzzzzzzzzz_sms_route_ui.go",
    '{`Answers {{.CopilotChatAccess.Prefix}}: messages`, `Answers M: messages`},',
    '{`Answers {{.CopilotChatAccess.Prefix}}: messages`, `Answers M: messages`},\n\t\t{`Answers {{.MuseChatAccess.Prefix}}: messages`, `Answers MU: messages`},',
)
replace_once(
    "zzzzzzzzzzzzzz_sms_route_ui.go",
    "rail('agent-copilot-chat','Answers M: messages');",
    "rail('agent-copilot-chat','Answers M: messages');\n    rail('agent-muse-chat','Answers MU: messages');",
)
replace_once(
    "zzzzzzzzzzzzzz_sms_route_ui.go",
    "copilotChatPrefix:{value:'M',hint:'Text M: to use Microsoft Copilot. Start fresh with M '+newWord+': your task.'}",
    "copilotChatPrefix:{value:'M',hint:'Text M: to use Microsoft Copilot. Start fresh with M '+newWord+': your task.'},\n      museChatPrefix:{value:'MU',hint:'Text MU: to use Muse. Start fresh with MU '+newWord+': your task.'}",
)

# Temporary automation files should never ship in the release.
Path(".github/workflows/muse-finalize.yml").unlink(missing_ok=True)
Path("scripts/finalize_muse.py").unlink(missing_ok=True)
