from pathlib import Path


def replace(path, old, new, label):
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"{label}: expected source text not found in {path}")
    if text.count(old) != 1:
        raise SystemExit(f"{label}: expected exactly one match in {path}, got {text.count(old)}")
    p.write_text(text.replace(old, new, 1), encoding="utf-8")

# Central SMS execution: P behaves exactly like the existing browser-chat agents.
replace("bridge.go",
'''\t\tcase "X":
\t\t\terr = b.newGrokChatConversation(ctx)
\t\t\tfinal = "New Grok Chat conversation started."
\t\tdefault:''',
'''\t\tcase "X":
\t\t\terr = b.newGrokChatConversation(ctx)
\t\t\tfinal = "New Grok Chat conversation started."
\t\tcase "P":
\t\t\terr = b.newCopilotChatConversation(ctx)
\t\t\tfinal = "New Microsoft Copilot Chat conversation started."
\t\tdefault:''',
"bridge new Copilot conversation")

replace("bridge.go",
'''\t} else if rc.Agent == "X" {
\t\tb.event("info", "agent", "Grok Chat command started", rc.Sender, "X", m.ID)
\t\tfinal, err = b.runGrokChatSMS(ctx, rc.Text)
\t} else {''',
'''\t} else if rc.Agent == "X" {
\t\tb.event("info", "agent", "Grok Chat command started", rc.Sender, "X", m.ID)
\t\tfinal, err = b.runGrokChatSMS(ctx, rc.Text)
\t} else if rc.Agent == "P" {
\t\tb.event("info", "agent", "Microsoft Copilot Chat command started", rc.Sender, "P", m.ID)
\t\tfinal, err = b.runCopilotChatSMS(ctx, rc.Text)
\t} else {''',
"bridge Copilot turn")

# Local authenticated UI routes.
replace("webui.go",
'''\tm.HandleFunc("/grok-chat/status.json", a.requireAuth(a.grokChatStatusJSON))''',
'''\tm.HandleFunc("/grok-chat/status.json", a.requireAuth(a.grokChatStatusJSON))
\tm.HandleFunc("/copilot-chat/status.json", a.requireAuth(a.copilotChatStatusJSON))''',
"Copilot status route")

replace("webui.go",
'''\t\t"/grok-chat/disconnect":   a.grokChatDisconnect,
\t\t"/agents/numbers/add":''',
'''\t\t"/grok-chat/disconnect":   a.grokChatDisconnect,
\t\t"/copilot-chat/connect":    a.copilotChatConnect,
\t\t"/copilot-chat/test":       a.copilotChatTest,
\t\t"/copilot-chat/disconnect": a.copilotChatDisconnect,
\t\t"/agents/numbers/add":''',
"Copilot action routes")

# Agents settings parity: shortcut, access, PIN, receipts/progress, shared prompt.
replace("ui_actions.go",
'''r.Form.Has("geminiChatPrefix") || r.Form.Has("grokChatPrefix") || r.Form.Has("newSessionCommand")''',
'''r.Form.Has("geminiChatPrefix") || r.Form.Has("grokChatPrefix") || r.Form.Has("copilotChatPrefix") || r.Form.Has("newSessionCommand")''',
"Copilot prefix form trigger")

replace("ui_actions.go",
'''codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, newSession := configuredCodexPrefix(*cfg), configuredClaudePrefix(*cfg), configuredChatGPTPrefix(*cfg), configuredClaudeChatPrefix(*cfg), configuredGeminiChatPrefix(*cfg), configuredGrokChatPrefix(*cfg), configuredNewSessionCommand(*cfg)''',
'''codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, copilotChatPrefix, newSession := configuredCodexPrefix(*cfg), configuredClaudePrefix(*cfg), configuredChatGPTPrefix(*cfg), configuredClaudeChatPrefix(*cfg), configuredGeminiChatPrefix(*cfg), configuredGrokChatPrefix(*cfg), configuredCopilotChatPrefix(*cfg), configuredNewSessionCommand(*cfg)''',
"Copilot prefix local")

replace("ui_actions.go",
'''\t\t\tif r.Form.Has("grokChatPrefix") {
\t\t\t\tgrokChatPrefix, err = validateCommandToken(r.FormValue("grokChatPrefix"), "Grok Chat shortcut")
\t\t\t\tif err != nil {
\t\t\t\t\treturn err
\t\t\t\t}
\t\t\t}
\t\t\tprefixes := []string{codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix}''',
'''\t\t\tif r.Form.Has("grokChatPrefix") {
\t\t\t\tgrokChatPrefix, err = validateCommandToken(r.FormValue("grokChatPrefix"), "Grok Chat shortcut")
\t\t\t\tif err != nil {
\t\t\t\t\treturn err
\t\t\t\t}
\t\t\t}
\t\t\tif r.Form.Has("copilotChatPrefix") {
\t\t\t\tcopilotChatPrefix, err = validateCommandToken(r.FormValue("copilotChatPrefix"), "Microsoft Copilot Chat shortcut")
\t\t\t\tif err != nil {
\t\t\t\t\treturn err
\t\t\t\t}
\t\t\t}
\t\t\tprefixes := []string{codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, copilotChatPrefix}''',
"Copilot prefix validation")

replace("ui_actions.go",
'''return fmt.Errorf("Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat shortcuts must all be different")''',
'''return fmt.Errorf("Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, Grok Chat, and Microsoft Copilot Chat shortcuts must all be different")''',
"Copilot duplicate prefix error")

replace("ui_actions.go",
'''cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.GrokChatPrefix, cfg.NewSessionCommand = codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, newSession''',
'''cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.GrokChatPrefix, cfg.CopilotChatPrefix, cfg.NewSessionCommand = codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, copilotChatPrefix, newSession''',
"Copilot prefix assignment")

replace("ui_actions.go",
'''for _, agent := range []string{"C", "A", "G", "H", "M", "X"} {''',
'''for _, agent := range []string{"C", "A", "G", "H", "M", "X", "P"} {''',
"Copilot access save")

replace("ui_actions.go",
'''\t\tcfg.GrokChat.Instruction = ""
\t\tif r.Form.Has("claudeSessionMode") {''',
'''\t\tcfg.GrokChat.Instruction = ""
\t\tcfg.CopilotChat.Instruction = ""
\t\tif r.Form.Has("claudeSessionMode") {''',
"Copilot shared instruction")

# View data needed by the new pane. The mature HTML remains untouched; the
# late augmentation inserts only the Copilot radio/rail/pane.
replace("ui_page_agents.go",
'''\tGeminiChatAccess agentAccessView
\tGrokChatAccess   agentAccessView''',
'''\tGeminiChatAccess  agentAccessView
\tGrokChatAccess    agentAccessView
\tCopilotChatAccess agentAccessView''',
"Copilot agents view")

replace("ui_page_agents.go",
'''\tcase "X":
\t\tprefix = "grokChat"
\t}''',
'''\tcase "X":
\t\tprefix = "grokChat"
\tcase "P":
\t\tprefix = "copilotChat"
\t}''',
"Copilot form field prefix")

replace("ui_page_agents.go",
'''Hint: "Edit once. Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat all receive this same line."''',
'''Hint: "Edit once. Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, Grok Chat, and Microsoft Copilot Chat all receive this same line."''',
"Copilot prompt hint")

replace("ui_page_agents.go",
'''\tview.GrokChatAccess = newAgentAccessView(cfg, "X", configuredGrokChatPrefix(cfg))
\ta.render(w, "agents", view)''',
'''\tview.GrokChatAccess = newAgentAccessView(cfg, "X", configuredGrokChatPrefix(cfg))
\tview.CopilotChatAccess = newAgentAccessView(cfg, "P", configuredCopilotChatPrefix(cfg))
\ta.render(w, "agents", view)''',
"Copilot view data")

replace("ui_page_agents.go",
'''SMSOnly: agent == "G" || agent == "H" || agent == "M" || agent == "X",''',
'''SMSOnly: agent == "G" || agent == "H" || agent == "M" || agent == "X" || agent == "P",''',
"Copilot SMS-only access")

# Browser-image parity: remove FlipAi's private marker before Copilot sees the
# prompt and attach the prepared files through the page's real file input.
replace("copilot_chat_webview_windows.go",
'''\t\tif !waitForCopilotChatPageSignedIn(dev, 25*time.Second) {
\t\t\trw.WriteHeader(http.StatusUnauthorized)
\t\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Microsoft Copilot Chat is not ready inside FlipAi. Press Connect and complete sign-in first."})
\t\t\treturn
\t\t}
\t\texpr := fmt.Sprintf(copilotChatTurnJS, copilotChatJSString(prompt))''',
'''\t\tif !waitForCopilotChatPageSignedIn(dev, 25*time.Second) {
\t\t\trw.WriteHeader(http.StatusUnauthorized)
\t\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": "Microsoft Copilot Chat is not ready inside FlipAi. Press Connect and complete sign-in first."})
\t\t\treturn
\t\t}
\t\tcleanPrompt, attachments, marked, markerErr := extractBrowserChatAttachmentMarker(prompt)
\t\tif marked {
\t\t\tif markerErr != nil {
\t\t\t\trw.WriteHeader(http.StatusBadRequest)
\t\t\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": markerErr.Error()})
\t\t\t\treturn
\t\t\t}
\t\t\tif err := uploadBrowserChatImages(dev, attachments); err != nil {
\t\t\t\trw.WriteHeader(http.StatusBadGateway)
\t\t\t\t_ = json.NewEncoder(rw).Encode(map[string]any{"ok": false, "detail": err.Error()})
\t\t\t\treturn
\t\t\t}
\t\t\tprompt = strings.TrimSpace(cleanPrompt)
\t\t\tif prompt == "" {
\t\t\t\tprompt = browserChatImageOnlyPrompt(len(attachments))
\t\t\t}
\t\t}
\t\texpr := fmt.Sprintf(copilotChatTurnJS, copilotChatJSString(prompt))''',
"Copilot browser image upload")

print("Copilot central integration patches applied")
