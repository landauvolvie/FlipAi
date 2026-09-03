from pathlib import Path
import re

ROOT = Path('.')


def read(path):
    return (ROOT / path).read_text(encoding='utf-8')


def write(path, text):
    (ROOT / path).write_text(text, encoding='utf-8')


def replace(path, old, new, count=None):
    text = read(path)
    hits = text.count(old)
    if hits == 0:
        raise SystemExit(f'{path}: missing expected text: {old[:100]!r}')
    if count is not None and hits != count:
        raise SystemExit(f'{path}: expected {count} hits, found {hits}: {old[:100]!r}')
    write(path, text.replace(old, new))


def clone_gemini(src, dst):
    text = read(src)
    for old, new in [
        ('GeminiChat', 'GrokChat'),
        ('geminiChat', 'grokChat'),
        ('gemini-chat', 'grok-chat'),
        ('Gemini Chat', 'Grok Chat'),
        ('Gemini', 'Grok'),
    ]:
        text = text.replace(old, new)
    text = text.replace('https://gemini.google.com', 'https://grok.com')
    text = text.replace('gemini.google.com', 'grok.com')
    write(dst, text)


# ---------------------------------------------------------------------------
# Clone the proven Gemini browser-agent worker. Provider-specific DOM logic is
# adjusted below; lifecycle, singleton, loopback control, and recovery remain
# identical so Grok inherits the RAM protections already proven in production.
# ---------------------------------------------------------------------------
for src, dst in [
    ('gemini_chat_webview.go', 'grok_chat_webview.go'),
    ('gemini_chat_webview_lifecycle.go', 'grok_chat_webview_lifecycle.go'),
    ('gemini_chat_webview_mode_windows.go', 'grok_chat_webview_mode_windows.go'),
    ('gemini_chat_webview_other.go', 'grok_chat_webview_other.go'),
    ('gemini_chat_webview_windows.go', 'grok_chat_webview_windows.go'),
    ('sms_gemini_chat.go', 'sms_grok_chat.go'),
    ('gemini_chat_routes_test.go', 'grok_chat_routes_test.go'),
    ('zzzzzz_gemini_chat_ui.go', 'zzzzzzz_grok_chat_ui.go'),
]:
    clone_gemini(src, dst)

# Grok is the sixth SMS agent and uses X: by default.
for path in ['sms_grok_chat.go', 'grok_chat_routes_test.go']:
    text = read(path).replace('"M"', '"X"').replace('M: hello Grok', 'X: hello Grok')
    write(path, text)

# Grok web currently uses a TipTap / ProseMirror contenteditable composer.
p = Path('grok_chat_webview_windows.go')
s = p.read_text(encoding='utf-8')
old_composer = "const composer=()=>document.querySelector('rich-textarea .ql-editor[contenteditable=\"true\"],rich-textarea [contenteditable=\"true\"],div.ql-editor[contenteditable=\"true\"],[contenteditable=\"true\"][role=\"textbox\"],[contenteditable=\"true\"][aria-label*=\"prompt\" i],textarea[aria-label*=\"prompt\" i],textarea');"
new_composer = "const composer=()=>document.querySelector('div.ProseMirror[contenteditable=\"true\"][role=\"textbox\"],div.tiptap.ProseMirror[contenteditable=\"true\"],[data-testid=\"grokInput\"][contenteditable=\"true\"],[data-testid=\"grokInput\"],[contenteditable=\"true\"][role=\"textbox\"],textarea[placeholder],textarea');"
if s.count(old_composer) != 2:
    raise SystemExit(f'grok windows: expected 2 composer selectors, got {s.count(old_composer)}')
s = s.replace(old_composer, new_composer)
s = s.replace("/sign in/i", "/sign in|log in/i")
old_assist = "const primary=unique([...all('model-response'),...all('[data-test-id=\"model-response\"]'),...all('[data-testid=\"model-response\"]')]).filter(n=>text(n));\n    if(primary.length)return primary;\n    return unique([...all('.model-response-text'),...all('.markdown-main-panel'),...all('[class*=\"model-response\"]')]).filter(n=>text(n));"
new_assist = "const primary=unique([...all('[data-testid=\"grokResponse\"]'),...all('[data-testid*=\"response\" i]'),...all('[data-message-author-role=\"assistant\"]')]).filter(n=>text(n));\n    if(primary.length)return primary;\n    return unique([...all('.markdown'),...all('[class*=\"markdown\"]'),...all('.prose')]).filter(n=>text(n) && !n.closest('form'));"
if old_assist not in s:
    raise SystemExit('grok windows: assistant selector block not found')
s = s.replace(old_assist, new_assist, 1)
old_send = "const candidates=unique([...all('button[aria-label*=\"Send\" i]'),...all('button[mattooltip*=\"Send\" i]'),...all('button[data-test-id*=\"send\" i]'),...all('button[data-testid*=\"send\" i]'),...all('button.send-button')]);"
new_send = "const candidates=unique([...all('button[data-testid=\"chat-submit\"]'),...all('button[data-testid=\"grokSend\"]'),...all('button[aria-label*=\"send\" i]'),...all('button[type=\"submit\"]')]);"
if old_send not in s:
    raise SystemExit('grok windows: send selector block not found')
s = s.replace(old_send, new_send, 1)
old_stop = "const stop=()=>document.querySelector('button[aria-label*=\"Stop\" i],button[mattooltip*=\"Stop\" i],button[data-test-id*=\"stop\" i],button[data-testid*=\"stop\" i]');"
new_stop = "const stop=()=>document.querySelector('button[data-testid*=\"stop\" i],button[aria-label*=\"stop\" i]');"
if old_stop not in s:
    raise SystemExit('grok windows: stop selector block not found')
s = s.replace(old_stop, new_stop, 1)
old_insert = "    }else{\n      const sel=getSelection(),range=document.createRange();range.selectNodeContents(c);sel.removeAllRanges();sel.addRange(range);\n      document.execCommand('delete',false,null);document.execCommand('insertText',false,input);\n      c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));c.dispatchEvent(new Event('change',{bubbles:true}));\n    }"
new_insert = "    }else if(c.editor&&c.editor.commands){\n      if(c.editor.commands.clearContent)c.editor.commands.clearContent();\n      c.editor.commands.insertContent(input);\n      c.dispatchEvent(new Event('input',{bubbles:true}));\n    }else{\n      const sel=getSelection(),range=document.createRange();range.selectNodeContents(c);sel.removeAllRanges();sel.addRange(range);\n      document.execCommand('delete',false,null);document.execCommand('insertText',false,input);\n      c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));c.dispatchEvent(new Event('change',{bubbles:true}));\n    }"
if old_insert not in s:
    raise SystemExit('grok windows: editor insertion block not found')
s = s.replace(old_insert, new_insert, 1)
p.write_text(s, encoding='utf-8')

# Grok UI is applied after the Gemini augmentation so all six panes coexist.
p = Path('zzzzzzz_grok_chat_ui.go')
s = p.read_text(encoding='utf-8')
s = s.replace(
    'registerPage("agents", grokChatDirectUI(claudeChatDirectUI(chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML)))))',
    'registerPage("agents", grokChatDirectUI(geminiChatDirectUI(claudeChatDirectUI(chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML))))))',
)
s = s.replace(
    '<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, and X: selects Grok Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>',
    '<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, M: selects Gemini Chat, and X: selects Grok Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>',
)
# The copied Gemini augmentation still looks for the original three-agent header.
s = s.replace(
    'body = strings.Replace(body, `<p>C: selects Codex, A: selects Claude, and G: selects ChatGPT Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`, `<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, and X: selects Grok Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`, 1)',
    'body = strings.Replace(body, `<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, and M: selects Gemini Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`, `<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, M: selects Gemini Chat, and X: selects Grok Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>`, 1)',
)
s = s.replace('<span class="bmark google">{{brand "google"}}</span>', '<span class="bmark grok">𝕏</span>')
s = s.replace('<span class="bmark lg google">{{brand "google"}}</span>', '<span class="bmark lg grok">𝕏</span>')
s = s.replace('Regular Grok at grok.com', 'Regular Grok at grok.com')
s = s.replace('Codex, Claude, ChatGPT Chat, Claude Chat, and Grok Chat.', 'Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat.')
s = s.replace('C:, A:, G:, H:, or {{.GrokChatAccess.Prefix}}:', 'C:, A:, G:, H:, M:, or {{.GrokChatAccess.Prefix}}:')
# Add a minimal brand treatment without introducing an external asset dependency.
s = s.replace('.grok-chat-actions{display:flex;', '.bmark.grok{font-family:Segoe UI Symbol,Segoe UI,sans-serif;font-weight:800}\n    .grok-chat-actions{display:flex;')
p.write_text(s, encoding='utf-8')

# ---------------------------------------------------------------------------
# Shared sixth-agent plumbing.
# ---------------------------------------------------------------------------
replace('VERSION', '0.46.21\n', '0.46.22\n', 1)

# config.go
replace('config.go', 'const version = "0.46.21"', 'const version = "0.46.22"', 1)
replace('config.go', 'GeminiChatPrefix string `json:"geminiChatPrefix,omitempty"`\n\tNewSessionCommand', 'GeminiChatPrefix string `json:"geminiChatPrefix,omitempty"`\n\tGrokChatPrefix   string `json:"grokChatPrefix,omitempty"`\n\tNewSessionCommand', 1)
replace('config.go', '\tGeminiChat GeminiChatConfig `json:"geminiChat,omitempty"`\n\tGmail', '\tGeminiChat GeminiChatConfig `json:"geminiChat,omitempty"`\n\tGrokChat   GrokChatConfig   `json:"grokChat,omitempty"`\n\tGmail', 1)
replace('config.go', 'type GeminiChatConfig struct {\n\tAgentSettings\n}\n', 'type GeminiChatConfig struct {\n\tAgentSettings\n}\n\ntype GrokChatConfig struct {\n\tAgentSettings\n}\n', 1)
replace('config.go', 'GeminiChatAgentMigrated bool `json:"geminiChatAgentMigrated,omitempty"`', 'GeminiChatAgentMigrated bool `json:"geminiChatAgentMigrated,omitempty"`\n\tGrokChatAgentMigrated   bool `json:"grokChatAgentMigrated,omitempty"`', 1)
replace('config.go', '// values are "C", "A", "G", "H", or "M".', '// values are "C", "A", "G", "H", "M", or "X".', 1)
replace('config.go', 'GeminiChatPrefix: defaultGeminiChatPrefix, NewSessionCommand:', 'GeminiChatPrefix: defaultGeminiChatPrefix, GrokChatPrefix: defaultGrokChatPrefix, NewSessionCommand:', 1)
replace('config.go', '\t\tGeminiChat:  GeminiChatConfig{AgentSettings: browserDefaults},\n\t\tSecurity:', '\t\tGeminiChat:  GeminiChatConfig{AgentSettings: browserDefaults},\n\t\tGrokChat:    GrokChatConfig{AgentSettings: browserDefaults},\n\t\tSecurity:', 1)
replace('config.go', '\tcfg.GeminiChat.Instruction = normalizeReplyStyleHint(cfg.GeminiChat.Instruction)\n', '\tcfg.GeminiChat.Instruction = normalizeReplyStyleHint(cfg.GeminiChat.Instruction)\n\tcfg.GrokChat.Instruction = normalizeReplyStyleHint(cfg.GrokChat.Instruction)\n', 1)
replace('config.go', '\tcfg.GeminiChatPrefix = normalizeCommandToken(cfg.GeminiChatPrefix, defaultGeminiChatPrefix)\n', '\tcfg.GeminiChatPrefix = normalizeCommandToken(cfg.GeminiChatPrefix, defaultGeminiChatPrefix)\n\tcfg.GrokChatPrefix = normalizeCommandToken(cfg.GrokChatPrefix, defaultGrokChatPrefix)\n', 1)
replace('config.go', 'prefixes := []string{cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix}', 'prefixes := []string{cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.GrokChatPrefix}', 1)
replace('config.go', 'cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix = defaultCodexPrefix, defaultClaudePrefix, defaultChatGPTPrefix, defaultClaudeChatPrefix, defaultGeminiChatPrefix', 'cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.GrokChatPrefix = defaultCodexPrefix, defaultClaudePrefix, defaultChatGPTPrefix, defaultClaudeChatPrefix, defaultGeminiChatPrefix, defaultGrokChatPrefix', 1)

# commands.go
replace('commands.go', '\tdefaultGeminiChatPrefix = "M"\n', '\tdefaultGeminiChatPrefix = "M"\n\tdefaultGrokChatPrefix   = "X"\n', 1)
replace('commands.go', 'func configuredGeminiChatPrefix(cfg Config) string {\n\treturn normalizeCommandToken(cfg.GeminiChatPrefix, defaultGeminiChatPrefix)\n}\n', 'func configuredGeminiChatPrefix(cfg Config) string {\n\treturn normalizeCommandToken(cfg.GeminiChatPrefix, defaultGeminiChatPrefix)\n}\n\nfunc configuredGrokChatPrefix(cfg Config) string {\n\treturn normalizeCommandToken(cfg.GrokChatPrefix, defaultGrokChatPrefix)\n}\n', 1)

# agents.go
p = Path('agents.go'); s = p.read_text(encoding='utf-8')
s = s.replace('\tcase "M":\n\t\treturn cfg.GeminiChat.AgentSettings\n', '\tcase "M":\n\t\treturn cfg.GeminiChat.AgentSettings\n\tcase "X":\n\t\treturn cfg.GrokChat.AgentSettings\n', 1)
s = s.replace('\tcase "M":\n\t\tcfg.GeminiChat.AgentSettings = s\n', '\tcase "M":\n\t\tcfg.GeminiChat.AgentSettings = s\n\tcase "X":\n\t\tcfg.GrokChat.AgentSettings = s\n', 1)
s = s.replace('{"C", "Codex"}, {"A", "Claude"}, {"G", "ChatGPT Chat"}, {"H", "Claude Chat"}, {"M", "Gemini Chat"},', '{"C", "Codex"}, {"A", "Claude"}, {"G", "ChatGPT Chat"}, {"H", "Claude Chat"}, {"M", "Gemini Chat"}, {"X", "Grok Chat"},', 1)
s = s.replace('[]string{"C", "A", "G", "H", "M"}', '[]string{"C", "A", "G", "H", "M", "X"}')
s = s.replace('agent == "G" || agent == "H" || agent == "M"', 'agent == "G" || agent == "H" || agent == "M" || agent == "X"')
s = s.replace('\t\tmigrateGeminiChatAgent(cfg)\n\t\treturn', '\t\tmigrateGeminiChatAgent(cfg)\n\t\tmigrateGrokChatAgent(cfg)\n\t\treturn', 1)
s = s.replace('\tmigrateGeminiChatAgent(cfg)\n}\n\nfunc migrateChatGPTAgent', '\tmigrateGeminiChatAgent(cfg)\n\tmigrateGrokChatAgent(cfg)\n}\n\nfunc migrateChatGPTAgent', 1)
anchor = '''func migrateGeminiChatAgent(cfg *Config) {
	if cfg.Security.GeminiChatAgentMigrated {
		return
	}
	// Gemini Chat is also a new security boundary: never inherit a phone or PIN.
	cfg.Security.GeminiChatAgentMigrated = true
}
'''
if anchor not in s: raise SystemExit('agents.go: Gemini migration anchor missing')
s = s.replace(anchor, anchor + '''
func migrateGrokChatAgent(cfg *Config) {
	if cfg.Security.GrokChatAgentMigrated {
		return
	}
	// Grok Chat is a separate account/security boundary: never inherit a phone or PIN.
	cfg.Security.GrokChatAgentMigrated = true
}
''', 1)
p.write_text(s, encoding='utf-8')

# bridge.go
p = Path('bridge.go'); s = p.read_text(encoding='utf-8')
s = s.replace('rc.Agent == "G" || rc.Agent == "H" || rc.Agent == "M"', 'rc.Agent == "G" || rc.Agent == "H" || rc.Agent == "M" || rc.Agent == "X"')
s = s.replace('\t\tcase "M":\n\t\t\tif err := b.newGeminiChatConversation(ctx); err != nil {', '\t\tcase "M":\n\t\t\tif err := b.newGeminiChatConversation(ctx); err != nil {', 1)
needle = '''		case "M":
			if err := b.newGeminiChatConversation(ctx); err != nil {
				return err
			}
'''
if needle not in s: raise SystemExit('bridge.go: Gemini NEW case missing')
s = s.replace(needle, needle + '''		case "X":
			if err := b.newGrokChatConversation(ctx); err != nil {
				return err
			}
''', 1)
needle = '''	} else if rc.Agent == "M" {
		answer, err = b.runGeminiChatSMS(ctx, rc.Text)
'''
if needle not in s: raise SystemExit('bridge.go: Gemini turn case missing')
s = s.replace(needle, needle + '''	} else if rc.Agent == "X" {
		answer, err = b.runGrokChatSMS(ctx, rc.Text)
''', 1)
p.write_text(s, encoding='utf-8')

# sticky SMS routing
p = Path('sms_sticky_chatgpt.go'); s = p.read_text(encoding='utf-8')
s = s.replace('[]string{"C", "A", "G", "H", "M"}', '[]string{"C", "A", "G", "H", "M", "X"}')
s = s.replace('target != "C" && target != "A" && target != "G" && target != "H" && target != "M"', 'target != "C" && target != "A" && target != "G" && target != "H" && target != "M" && target != "X"')
s = s.replace('source != "C" && source != "A" && source != "G" && source != "H" && source != "M"', 'source != "C" && source != "A" && source != "G" && source != "H" && source != "M" && source != "X"')
s = s.replace('Start with C:, A:, G:, H:, or M:', 'Start with C:, A:, G:, H:, M:, or X:')
needle = '''	case "M":
		rc, err = parseGeminiChatSMSCommand(raw, b.cfg)
'''
if needle not in s: raise SystemExit('sticky routing: Gemini parse case missing')
s = s.replace(needle, needle + '''	case "X":
		rc, err = parseGrokChatSMSCommand(raw, b.cfg)
''', 1)
s = s.replace('selected == "G" || selected == "H" || selected == "M"', 'selected == "G" || selected == "H" || selected == "M" || selected == "X"')
s = s.replace('agent != "C" && agent != "A" && agent != "G" && agent != "H" && agent != "M"', 'agent != "C" && agent != "A" && agent != "G" && agent != "H" && agent != "M" && agent != "X"')
p.write_text(s, encoding='utf-8')

# web routes
replace('webui.go', '\tm.HandleFunc("/gemini-chat/status.json", a.requireAuth(a.geminiChatStatusJSON))\n', '\tm.HandleFunc("/gemini-chat/status.json", a.requireAuth(a.geminiChatStatusJSON))\n\tm.HandleFunc("/grok-chat/status.json", a.requireAuth(a.grokChatStatusJSON))\n', 1)
replace('webui.go', '\t\t"/gemini-chat/disconnect": a.geminiChatDisconnect,\n', '\t\t"/gemini-chat/disconnect": a.geminiChatDisconnect,\n\t\t"/grok-chat/connect":      a.grokChatConnect,\n\t\t"/grok-chat/test":         a.grokChatTest,\n\t\t"/grok-chat/disconnect":   a.grokChatDisconnect,\n', 1)

# main lifecycle and fresh-config security boundary
replace('main.go', '\t\tcfg.Security.GeminiChatAgentMigrated = true\n', '\t\tcfg.Security.GeminiChatAgentMigrated = true\n\t\tcfg.Security.GrokChatAgentMigrated = true\n', 1)
replace('main.go', '!geminiChatBrowserStillOpen(dataDir) {', '!geminiChatBrowserStillOpen(dataDir) && !grokChatBrowserStillOpen(dataDir) {', 1)

# UI settings plumbing
p = Path('ui_actions_agents.go'); s = p.read_text(encoding='utf-8')
s = s.replace('\tcase "M":\n\t\treturn "M"\n', '\tcase "M":\n\t\treturn "M"\n\tcase "X":\n\t\treturn "X"\n', 1)
s = s.replace('agent == "G" || agent == "H" || agent == "M"', 'agent == "G" || agent == "H" || agent == "M" || agent == "X"')
s = s.replace('agent != "A" && agent != "G" && agent != "H" && agent != "M"', 'agent != "A" && agent != "G" && agent != "H" && agent != "M" && agent != "X"')
p.write_text(s, encoding='utf-8')

p = Path('ui_actions.go'); s = p.read_text(encoding='utf-8')
s = s.replace('r.Form.Has("geminiChatPrefix") || r.Form.Has("newSessionCommand")', 'r.Form.Has("geminiChatPrefix") || r.Form.Has("grokChatPrefix") || r.Form.Has("newSessionCommand")', 1)
s = s.replace('codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, newSession := configuredCodexPrefix(*cfg), configuredClaudePrefix(*cfg), configuredChatGPTPrefix(*cfg), configuredClaudeChatPrefix(*cfg), configuredGeminiChatPrefix(*cfg), configuredNewSessionCommand(*cfg)', 'codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, newSession := configuredCodexPrefix(*cfg), configuredClaudePrefix(*cfg), configuredChatGPTPrefix(*cfg), configuredClaudeChatPrefix(*cfg), configuredGeminiChatPrefix(*cfg), configuredGrokChatPrefix(*cfg), configuredNewSessionCommand(*cfg)', 1)
needle = '''			if r.Form.Has("geminiChatPrefix") {
				geminiChatPrefix, err = validateCommandToken(r.FormValue("geminiChatPrefix"), "Gemini Chat shortcut")
				if err != nil {
					return err
				}
			}
'''
if needle not in s: raise SystemExit('ui_actions.go: Gemini prefix validation missing')
s = s.replace(needle, needle + '''			if r.Form.Has("grokChatPrefix") {
				grokChatPrefix, err = validateCommandToken(r.FormValue("grokChatPrefix"), "Grok Chat shortcut")
				if err != nil {
					return err
				}
			}
''', 1)
s = s.replace('prefixes := []string{codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix}', 'prefixes := []string{codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix}', 1)
s = s.replace('Codex, Claude, ChatGPT Chat, Claude Chat, and Gemini Chat shortcuts must all be different', 'Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat shortcuts must all be different', 1)
s = s.replace('cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.NewSessionCommand = codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, newSession', 'cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.GrokChatPrefix, cfg.NewSessionCommand = codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, grokChatPrefix, newSession', 1)
s = s.replace('[]string{"C", "A", "G", "H", "M"}', '[]string{"C", "A", "G", "H", "M", "X"}', 1)
s = s.replace('\t\tcfg.GeminiChat.Instruction = ""\n', '\t\tcfg.GeminiChat.Instruction = ""\n\t\tcfg.GrokChat.Instruction = ""\n', 1)
p.write_text(s, encoding='utf-8')

p = Path('ui_page_agents.go'); s = p.read_text(encoding='utf-8')
s = s.replace('\tGeminiChatAccess agentAccessView\n', '\tGeminiChatAccess agentAccessView\n\tGrokChatAccess   agentAccessView\n', 1)
s = s.replace('\tcase "M":\n\t\tprefix = "geminiChat"\n', '\tcase "M":\n\t\tprefix = "geminiChat"\n\tcase "X":\n\t\tprefix = "grokChat"\n', 1)
s = s.replace('Codex, Claude, ChatGPT Chat, Claude Chat, and Gemini Chat all receive this same line.', 'Codex, Claude, ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat all receive this same line.', 1)
s = s.replace('\tview.GeminiChatAccess = newAgentAccessView(cfg, "M", configuredGeminiChatPrefix(cfg))\n', '\tview.GeminiChatAccess = newAgentAccessView(cfg, "M", configuredGeminiChatPrefix(cfg))\n\tview.GrokChatAccess = newAgentAccessView(cfg, "X", configuredGrokChatPrefix(cfg))\n', 1)
s = s.replace('agent == "G" || agent == "H" || agent == "M"', 'agent == "G" || agent == "H" || agent == "M" || agent == "X"', 1)
p.write_text(s, encoding='utf-8')

# The current UI regression test deliberately counts one shared instruction per pane.
p = Path('ui_page_agents_test.go'); s = p.read_text(encoding='utf-8')
s = s.replace('`name="geminiChatPrefix"`,', '`name="geminiChatPrefix"`, `name="grokChatPrefix"`,')
s = s.replace('`name="geminiChatRequireCode"`,', '`name="geminiChatRequireCode"`, `name="grokChatRequireCode"`,')
s = s.replace('`name="geminiChatAckDelay"`,', '`name="geminiChatAckDelay"`, `name="grokChatAckDelay"`,')
s = s.replace('strings.Count(body, `name="sharedReplyStyle"`) != 5', 'strings.Count(body, `name="sharedReplyStyle"`) != 6')
s = s.replace('all five panes', 'all six panes')
s = s.replace('all five agents', 'all six agents')
s = s.replace('cfg.GeminiChat.Instruction != "" {', 'cfg.GeminiChat.Instruction != "" || cfg.GrokChat.Instruction != "" {')
s = s.replace('[]string{"C", "A", "G", "H", "M"}', '[]string{"C", "A", "G", "H", "M", "X"}')
p.write_text(s, encoding='utf-8')

# Release notes are part of the release gate and must describe the exact version.
write('docs/RELEASE-NOTES.md', '''# FlipAi v0.46.22

Grok Chat is now a sixth independent FlipAi agent, using the user's regular xAI Grok account in a dedicated persistent browser session at grok.com. It does not use the xAI API.

## Grok Chat

The Agents page now includes Grok Chat with Connect, Test, Disconnect, routing, access, security, and the same shared SMS instruction used by the other agents. The default SMS shortcut is `X:`. Once selected for a phone, unprefixed follow-up texts stay with Grok Chat until another agent is selected.

Grok Chat has its own WebView2 profile and its own phone/PIN security boundary. A saved sign-in is restored invisibly after FlipAi or Windows restarts. The browser worker uses the same singleton owner, cheap liveness probe, and restart throttling as ChatGPT Chat, Claude Chat, and Gemini Chat so a slow Grok renderer cannot spawn duplicate hidden WebView2 trees and fill RAM.

## grok.com browser control

FlipAi targets Grok's current TipTap / ProseMirror contenteditable prompt, with multiple fallback selectors for the prompt, send/stop controls, and response containers. Connect opens grok.com in FlipAi's private profile; Test performs a real browser turn through that saved session.

Connect, Test, and Disconnect are all registered in FlipAi's authenticated POST router, with route-level regression coverage so they cannot fall through to a local 404.

The normal Linux/browser tests plus Windows unit, vet, race, build, lifecycle, Google Voice, Defender, installer, and real install/uninstall smoke gates still run before publishing.
''')

# Sanity checks that the integration actually produced a sixth isolated browser agent.
required = {
    'config.go': ['GrokChatPrefix', 'GrokChat GrokChatConfig', 'GrokChatAgentMigrated'],
    'agents.go': ['{"X", "Grok Chat"}', 'migrateGrokChatAgent'],
    'webui.go': ['/grok-chat/connect', '/grok-chat/status.json'],
    'sms_sticky_chatgpt.go': ['parseGrokChatSMSCommand'],
    'zzzzzzz_grok_chat_ui.go': ['Grok Chat', 'GrokChatAccess', '/grok-chat/connect'],
}
for path, tokens in required.items():
    text = read(path)
    for token in tokens:
        if token not in text:
            raise SystemExit(f'{path}: Grok integration missing {token!r}')

print('Grok Chat v0.46.22 integration applied')
