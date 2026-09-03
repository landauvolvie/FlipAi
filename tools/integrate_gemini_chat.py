from pathlib import Path
import re
import shutil

ROOT = Path(__file__).resolve().parents[1]


def read(path):
    return (ROOT / path).read_text(encoding="utf-8")


def write(path, text):
    (ROOT / path).write_text(text, encoding="utf-8")


def replace_once(text, old, new, label):
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"{label}: expected exactly one match, found {count}")
    return text.replace(old, new, 1)


def replace_all_required(text, old, new, label):
    if old not in text:
        raise RuntimeError(f"{label}: required text not found")
    return text.replace(old, new)


def patch(path, fn):
    text = read(path)
    out = fn(text)
    if out == text:
        raise RuntimeError(f"{path}: patch produced no change")
    write(path, out)


def provider_clone(src, dst, agent_marker=False):
    text = read(src)
    for old, new in [
        ("ClaudeChat", "GeminiChat"),
        ("claudeChat", "geminiChat"),
        ("Claude Chat", "Gemini Chat"),
        ("claude-chat", "gemini-chat"),
        ("Claude", "Gemini"),
        ("claude", "gemini"),
    ]:
        text = text.replace(old, new)
    text = text.replace("https://gemini.ai/new", "https://gemini.google.com")
    text = text.replace("https://gemini.ai", "https://gemini.google.com")
    if agent_marker:
        text = text.replace('"H"', '"M"')
    write(dst, text)


# ---------------------------------------------------------------------------
# Clone the mature Claude Chat browser architecture, then specialize only the
# provider-facing page driver. Runtime ownership, private profiles, loopback
# control, liveness, and restart behavior remain identical by construction.
# ---------------------------------------------------------------------------
for src, dst, marker in [
    ("claude_chat_webview.go", "gemini_chat_webview.go", False),
    ("claude_chat_webview_lifecycle.go", "gemini_chat_webview_lifecycle.go", False),
    ("claude_chat_webview_mode_windows.go", "gemini_chat_webview_mode_windows.go", False),
    ("claude_chat_webview_other.go", "gemini_chat_webview_other.go", False),
    ("claude_chat_webview_windows.go", "gemini_chat_webview_windows.go", False),
    ("sms_claude_chat.go", "sms_gemini_chat.go", True),
]:
    provider_clone(src, dst, marker)

# Gemini now enters at the stable product root. Google may add/remove path
# segments after sign-in, so conversation id extraction accepts both the old
# /app/<id> family and a future /chat/<id> family without requiring either.
p = read("gemini_chat_webview.go")
start = p.index("func geminiChatConversationID")
end = p.index("\n}\n\nfunc geminiChatControlRequest", start) + 2
conv = '''func geminiChatConversationID(href string) string {
    for _, marker := range []string{"/app/", "/chat/"} {
        i := strings.Index(href, marker)
        if i < 0 {
            continue
        }
        v := href[i+len(marker):]
        if j := strings.IndexAny(v, "?#/"); j >= 0 {
            v = v[:j]
        }
        if v = strings.TrimSpace(v); v != "" {
            return v
        }
    }
    return ""
}'''
p = p[:start] + conv + p[end:]
write("gemini_chat_webview.go", p)

# Replace Claude-specific DOM selectors with a resilient Gemini page driver.
# Prefer semantic/ARIA/custom-element selectors; class-name fallbacks are last.
p = read("gemini_chat_webview_windows.go")
block_start = p.index("const geminiChatPageMonitorJS")
block_end = p.index("type geminiChatTurnResult struct", block_start)
js_block = r'''const geminiChatPageMonitorJS = `(function(){
  if(window.__flipAiGeminiChatMonitor)return;
  window.__flipAiGeminiChatMonitor=true;
  const composer=()=>document.querySelector('rich-textarea .ql-editor[contenteditable="true"],rich-textarea [contenteditable="true"],div.ql-editor[contenteditable="true"],[contenteditable="true"][role="textbox"],[contenteditable="true"][aria-label*="prompt" i],textarea[aria-label*="prompt" i],textarea');
  const signIn=()=>Array.from(document.querySelectorAll('a,button')).find(n=>/sign in/i.test(((n.getAttribute('aria-label')||'')+' '+(n.innerText||'')).trim()));
  const signed=()=>location.hostname==='gemini.google.com' && !!composer() && !signIn();
  async function tick(){
    try{ if(window.flipGeminiChatStatus) await window.flipGeminiChatStatus(signed(), location.href); }catch(e){}
  }
  setInterval(tick,1000); addEventListener('load',tick); setTimeout(tick,350);
})();`

const geminiChatSignedInJS = `(()=>{const c=document.querySelector('rich-textarea .ql-editor[contenteditable="true"],rich-textarea [contenteditable="true"],div.ql-editor[contenteditable="true"],[contenteditable="true"][role="textbox"],[contenteditable="true"][aria-label*="prompt" i],textarea[aria-label*="prompt" i],textarea');const signIn=Array.from(document.querySelectorAll('a,button')).find(n=>/sign in/i.test(((n.getAttribute('aria-label')||'')+' '+(n.innerText||'')).trim()));return location.hostname==='gemini.google.com'&&!!c&&!signIn})()`

const geminiChatTurnJS = `(async(input)=>{
  const sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const text=n=>(n&&n.innerText||n&&n.textContent||'').trim();
  const all=q=>Array.from(document.querySelectorAll(q));
  const unique=xs=>Array.from(new Set(xs));
  const assistants=()=>{
    const primary=unique([...all('model-response'),...all('[data-test-id="model-response"]'),...all('[data-testid="model-response"]')]).filter(n=>text(n));
    if(primary.length)return primary;
    return unique([...all('.model-response-text'),...all('.markdown-main-panel'),...all('[class*="model-response"]')]).filter(n=>text(n));
  };
  const composer=()=>document.querySelector('rich-textarea .ql-editor[contenteditable="true"],rich-textarea [contenteditable="true"],div.ql-editor[contenteditable="true"],[contenteditable="true"][role="textbox"],[contenteditable="true"][aria-label*="prompt" i],textarea[aria-label*="prompt" i],textarea');
  const send=()=>{
    const candidates=unique([...all('button[aria-label*="Send" i]'),...all('button[mattooltip*="Send" i]'),...all('button[data-test-id*="send" i]'),...all('button[data-testid*="send" i]'),...all('button.send-button')]);
    return candidates.find(b=>!b.disabled && b.offsetParent!==null)||candidates.find(b=>!b.disabled)||null;
  };
  const stop=()=>document.querySelector('button[aria-label*="Stop" i],button[mattooltip*="Stop" i],button[data-test-id*="stop" i],button[data-testid*="stop" i]');
  let c=null;
  for(let i=0;i<120&&!c;i++){c=composer();if(!c)await sleep(200);}
  if(!c)return {ok:false,detail:'Gemini is loaded but FlipAi could not find the prompt box. The Gemini site layout may have changed.',href:location.href};
  const before=assistants();
  const beforeSet=new Set(before);
  const responseForTurn=()=>{const current=assistants();for(let i=current.length-1;i>=0;i--){if(!beforeSet.has(current[i]))return current[i]}return null};
  c.focus();
  try{
    if(c instanceof HTMLTextAreaElement || c instanceof HTMLInputElement){
      const proto=c instanceof HTMLTextAreaElement?HTMLTextAreaElement.prototype:HTMLInputElement.prototype;
      const setter=Object.getOwnPropertyDescriptor(proto,'value').set;setter.call(c,input);
      c.dispatchEvent(new Event('input',{bubbles:true}));c.dispatchEvent(new Event('change',{bubbles:true}));
    }else{
      const sel=getSelection(),range=document.createRange();range.selectNodeContents(c);sel.removeAllRanges();sel.addRange(range);
      document.execCommand('delete',false,null);document.execCommand('insertText',false,input);
      c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));c.dispatchEvent(new Event('change',{bubbles:true}));
    }
  }catch(e){c.textContent=input;c.dispatchEvent(new Event('input',{bubbles:true}))}
  await sleep(250);
  let b=null;
  for(let i=0;i<60&&!b;i++){b=send();if(!b)await sleep(100);}
  if(!b)return {ok:false,detail:'FlipAi filled the Gemini prompt box but the Send button never became ready.',href:location.href};
  b.click();
  let last='',stable=0,started=false;
  const deadline=Date.now()+90000;
  while(Date.now()<deadline){
    await sleep(250);
    const node=responseForTurn();
    if(node){started=true;const now=text(node);if(now===last)stable++;else{last=now;stable=0}if(!stop()&&stable>=5)return {ok:true,reply:now||'Gemini completed the turn.',href:location.href}}
  }
  return {ok:false,detail:started?'Gemini started answering but did not finish within 90 seconds.':'Gemini did not produce a new response within 90 seconds.',href:location.href};
})(%s)`

'''
p = p[:block_start] + js_block + p[block_end:]
p = p.replace("https://gemini.google.com/new", "https://gemini.google.com")
write("gemini_chat_webview_windows.go", p)

# Worker singleton and command names were cloned, but the provider name
# replacement can leave the historical URL in comments only; enforce none.
for f in ["gemini_chat_webview.go", "gemini_chat_webview_windows.go"]:
    t = read(f)
    if "gemini.ai" in t:
        raise RuntimeError(f"{f}: stale gemini.ai URL remains")

# ---------------------------------------------------------------------------
# Core config / agent model
# ---------------------------------------------------------------------------
def config_patch(t):
    t = replace_once(t, 'const version = "0.46.20"', 'const version = "0.46.21"', "version")
    t = replace_once(t, '\tClaudeChatPrefix   string `json:"claudeChatPrefix,omitempty"`\n', '\tClaudeChatPrefix   string `json:"claudeChatPrefix,omitempty"`\n\tGeminiChatPrefix   string `json:"geminiChatPrefix,omitempty"`\n', "config prefix field")
    t = replace_once(t, '\tClaudeChat  ClaudeChatConfig  `json:"claudeChat"`\n', '\tClaudeChat  ClaudeChatConfig  `json:"claudeChat"`\n\tGeminiChat  GeminiChatConfig  `json:"geminiChat"`\n', "config Gemini field")
    t = replace_once(t, 'type ClaudeChatConfig struct{ AgentSettings }\n', 'type ClaudeChatConfig struct{ AgentSettings }\n\n// GeminiChatConfig is the user\'s regular gemini.google.com account in its own\n// dedicated WebView2 profile. It is intentionally independent from every CLI/API.\ntype GeminiChatConfig struct{ AgentSettings }\n', "Gemini config type")
    t = replace_once(t, '\tClaudeChatAgentMigrated bool `json:"claudeChatAgentMigrated,omitempty"`\n', '\tClaudeChatAgentMigrated bool `json:"claudeChatAgentMigrated,omitempty"`\n\tGeminiChatAgentMigrated bool `json:"geminiChatAgentMigrated,omitempty"`\n', "Gemini migration flag")
    t = replace_once(t, 'ClaudeChatPrefix: defaultClaudeChatPrefix, NewSessionCommand:', 'ClaudeChatPrefix: defaultClaudeChatPrefix, GeminiChatPrefix: defaultGeminiChatPrefix, NewSessionCommand:', "default prefix")
    t = replace_once(t, '\t\tClaudeChat:  ClaudeChatConfig{AgentSettings: browserDefaults},\n', '\t\tClaudeChat:  ClaudeChatConfig{AgentSettings: browserDefaults},\n\t\tGeminiChat:  GeminiChatConfig{AgentSettings: browserDefaults},\n', "default Gemini config")
    t = replace_once(t, '\tcfg.ClaudeChat.Instruction = normalizeReplyStyleHint(cfg.ClaudeChat.Instruction)\n', '\tcfg.ClaudeChat.Instruction = normalizeReplyStyleHint(cfg.ClaudeChat.Instruction)\n\tcfg.GeminiChat.Instruction = normalizeReplyStyleHint(cfg.GeminiChat.Instruction)\n', "Gemini instruction normalize")
    t = replace_once(t, '\tcfg.ClaudeChatPrefix = normalizeCommandToken(cfg.ClaudeChatPrefix, defaultClaudeChatPrefix)\n', '\tcfg.ClaudeChatPrefix = normalizeCommandToken(cfg.ClaudeChatPrefix, defaultClaudeChatPrefix)\n\tcfg.GeminiChatPrefix = normalizeCommandToken(cfg.GeminiChatPrefix, defaultGeminiChatPrefix)\n', "Gemini prefix normalize")
    t = replace_once(t, 'prefixes := []string{cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix}', 'prefixes := []string{cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix}', "prefix duplicate list")
    t = replace_once(t, 'cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix = defaultCodexPrefix, defaultClaudePrefix, defaultChatGPTPrefix, defaultClaudeChatPrefix', 'cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix = defaultCodexPrefix, defaultClaudePrefix, defaultChatGPTPrefix, defaultClaudeChatPrefix, defaultGeminiChatPrefix', "prefix duplicate reset")
    return t
patch("config.go", config_patch)


def commands_patch(t):
    t = replace_once(t, '\tdefaultClaudeChatPrefix  = "H"\n', '\tdefaultClaudeChatPrefix  = "H"\n\tdefaultGeminiChatPrefix  = "M"\n', "Gemini default prefix")
    anchor = '''func configuredClaudeChatPrefix(cfg Config) string {
\treturn normalizeCommandToken(cfg.ClaudeChatPrefix, defaultClaudeChatPrefix)
}
'''
    addition = anchor + '''
func configuredGeminiChatPrefix(cfg Config) string {
\treturn normalizeCommandToken(cfg.GeminiChatPrefix, defaultGeminiChatPrefix)
}
'''
    t = replace_once(t, anchor, addition, "Gemini prefix helper")
    return t
patch("commands.go", commands_patch)


def agents_patch(t):
    t = replace_once(t, '\tcase "H":\n\t\treturn cfg.ClaudeChat.AgentSettings\n', '\tcase "H":\n\t\treturn cfg.ClaudeChat.AgentSettings\n\tcase "M":\n\t\treturn cfg.GeminiChat.AgentSettings\n', "agentSettings M")
    t = replace_once(t, '\tcase "H":\n\t\tcfg.ClaudeChat.AgentSettings = s\n', '\tcase "H":\n\t\tcfg.ClaudeChat.AgentSettings = s\n\tcase "M":\n\t\tcfg.GeminiChat.AgentSettings = s\n', "putAgentSettings M")
    t = replace_once(t, '{"C", "Codex"}, {"A", "Claude"}, {"G", "ChatGPT Chat"}, {"H", "Claude Chat"},', '{"C", "Codex"}, {"A", "Claude"}, {"G", "ChatGPT Chat"}, {"H", "Claude Chat"}, {"M", "Gemini Chat"},', "display name M")
    t = t.replace('[]string{"C", "A", "G", "H"}', '[]string{"C", "A", "G", "H", "M"}')
    t = t.replace('agent == "G" || agent == "H"', 'agent == "G" || agent == "H" || agent == "M"')
    t = replace_once(t, '\t\tmigrateClaudeChatAgent(cfg)\n\t\treturn\n', '\t\tmigrateClaudeChatAgent(cfg)\n\t\tmigrateGeminiChatAgent(cfg)\n\t\treturn\n', "migrate existing branch")
    t = replace_once(t, '\tmigrateClaudeChatAgent(cfg)\n}\n\nfunc migrateChatGPTAgent', '\tmigrateClaudeChatAgent(cfg)\n\tmigrateGeminiChatAgent(cfg)\n}\n\nfunc migrateChatGPTAgent', "migrate normal branch")
    anchor = '''func migrateClaudeChatAgent(cfg *Config) {
\tif cfg.Security.ClaudeChatAgentMigrated {
\t\treturn
\t}
\t// Claude Chat is a new security boundary. Mark the migration complete but
\t// start with no phone numbers or PIN copied from any existing agent.
\tcfg.Security.ClaudeChatAgentMigrated = true
}
'''
    addition = anchor + '''
func migrateGeminiChatAgent(cfg *Config) {
\tif cfg.Security.GeminiChatAgentMigrated {
\t\treturn
\t}
\t// Gemini Chat is also a new security boundary: never inherit a phone or PIN.
\tcfg.Security.GeminiChatAgentMigrated = true
}
'''
    t = replace_once(t, anchor, addition, "Gemini migration function")
    return t
patch("agents.go", agents_patch)

# ---------------------------------------------------------------------------
# Sticky SMS routing and execution
# ---------------------------------------------------------------------------
def sticky_patch(t):
    t = replace_once(t, '\t\t\t{"H", configuredClaudeChatPrefix(cfg)},\n', '\t\t\t{"H", configuredClaudeChatPrefix(cfg)},\n\t\t\t{"M", configuredGeminiChatPrefix(cfg)},\n', "explicit M")
    t = replace_all_required(t, 'target == "C" || target == "A" || target == "G" || target == "H"', 'target == "C" || target == "A" || target == "G" || target == "H" || target == "M"', "B allows M")
    t = replace_once(t, 'sourceAgent == "C" || sourceAgent == "A" || sourceAgent == "G" || sourceAgent == "H"', 'sourceAgent == "C" || sourceAgent == "A" || sourceAgent == "G" || sourceAgent == "H" || sourceAgent == "M"', "single source M")
    t = replace_once(t, 'C: for Codex, A: for Claude, G: for ChatGPT Chat, or H: for Claude Chat', 'C: for Codex, A: for Claude, G: for ChatGPT Chat, H: for Claude Chat, or M: for Gemini Chat', "routing error M")
    t = replace_once(t, '\t\tcase "H":\n\t\t\treturn parseClaudeChatSMSCommand(raw, cfg)\n', '\t\tcase "H":\n\t\t\treturn parseClaudeChatSMSCommand(raw, cfg)\n\t\tcase "M":\n\t\t\treturn parseGeminiChatSMSCommand(raw, cfg)\n', "parser M")
    t = replace_once(t, 'if target == "G" || target == "H" {', 'if target == "G" || target == "H" || target == "M" {', "attachment M")
    t = replace_once(t, 'agent != "C" && agent != "A" && agent != "G" && agent != "H"', 'agent != "C" && agent != "A" && agent != "G" && agent != "H" && agent != "M"', "remember M")
    return t
patch("sms_sticky_chatgpt.go", sticky_patch)


def bridge_patch(t):
    t = replace_once(t, 'if rc.Agent == "G" || rc.Agent == "H" {', 'if rc.Agent == "G" || rc.Agent == "H" || rc.Agent == "M" {', "attachment execution M")
    t = replace_once(t, '\t\tcase "H":\n\t\t\terr = b.newClaudeChatConversation(ctx)\n\t\t\tfinal = "New Claude Chat conversation started."\n', '\t\tcase "H":\n\t\t\terr = b.newClaudeChatConversation(ctx)\n\t\t\tfinal = "New Claude Chat conversation started."\n\t\tcase "M":\n\t\t\terr = b.newGeminiChatConversation(ctx)\n\t\t\tfinal = "New Gemini Chat conversation started."\n', "new Gemini conversation")
    t = replace_once(t, '\t} else if rc.Agent == "H" {\n\t\tb.event("info", "agent", "Claude Chat command started", rc.Sender, "H", m.ID)\n\t\tfinal, err = b.runClaudeChatSMS(ctx, rc.Text)\n', '\t} else if rc.Agent == "H" {\n\t\tb.event("info", "agent", "Claude Chat command started", rc.Sender, "H", m.ID)\n\t\tfinal, err = b.runClaudeChatSMS(ctx, rc.Text)\n\t} else if rc.Agent == "M" {\n\t\tb.event("info", "agent", "Gemini Chat command started", rc.Sender, "M", m.ID)\n\t\tfinal, err = b.runGeminiChatSMS(ctx, rc.Text)\n', "run Gemini turn")
    return t
patch("bridge.go", bridge_patch)

# ---------------------------------------------------------------------------
# UI data + save paths
# ---------------------------------------------------------------------------
def page_agents_patch(t):
    t = replace_once(t, '\tClaudeChatAccess agentAccessView\n', '\tClaudeChatAccess agentAccessView\n\tGeminiChatAccess agentAccessView\n', "view Gemini access")
    t = replace_once(t, '\tcase "H":\n\t\tprefix = "claudeChat"\n', '\tcase "H":\n\t\tprefix = "claudeChat"\n\tcase "M":\n\t\tprefix = "geminiChat"\n', "field prefix M")
    t = replace_once(t, 'Codex, Claude, ChatGPT Chat, and Claude Chat all receive this same line.', 'Codex, Claude, ChatGPT Chat, Claude Chat, and Gemini Chat all receive this same line.', "shared prompt Gemini")
    t = replace_once(t, '\tview.ClaudeChatAccess = newAgentAccessView(cfg, "H", configuredClaudeChatPrefix(cfg))\n', '\tview.ClaudeChatAccess = newAgentAccessView(cfg, "H", configuredClaudeChatPrefix(cfg))\n\tview.GeminiChatAccess = newAgentAccessView(cfg, "M", configuredGeminiChatPrefix(cfg))\n', "view populate M")
    t = replace_once(t, 'SMSOnly: agent == "G" || agent == "H",', 'SMSOnly: agent == "G" || agent == "H" || agent == "M",', "SMS-only M")
    return t
patch("ui_page_agents.go", page_agents_patch)


def action_agents_patch(t):
    t = replace_once(t, '\tcase "H":\n\t\treturn "H"\n', '\tcase "H":\n\t\treturn "H"\n\tcase "M":\n\t\treturn "M"\n', "agent form M")
    t = t.replace('agent == "G" || agent == "H"', 'agent == "G" || agent == "H" || agent == "M"')
    t = replace_once(t, 'agent != "A" && agent != "G" && agent != "H"', 'agent != "A" && agent != "G" && agent != "H" && agent != "M"', "remove M")
    return t
patch("ui_actions_agents.go", action_agents_patch)


def actions_patch(t):
    t = replace_once(t, 'r.Form.Has("chatgptPrefix") || r.Form.Has("claudeChatPrefix") || r.Form.Has("newSessionCommand")', 'r.Form.Has("chatgptPrefix") || r.Form.Has("claudeChatPrefix") || r.Form.Has("geminiChatPrefix") || r.Form.Has("newSessionCommand")', "save prefix trigger")
    t = replace_once(t, 'codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, newSession := configuredCodexPrefix(*cfg), configuredClaudePrefix(*cfg), configuredChatGPTPrefix(*cfg), configuredClaudeChatPrefix(*cfg), configuredNewSessionCommand(*cfg)', 'codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, newSession := configuredCodexPrefix(*cfg), configuredClaudePrefix(*cfg), configuredChatGPTPrefix(*cfg), configuredClaudeChatPrefix(*cfg), configuredGeminiChatPrefix(*cfg), configuredNewSessionCommand(*cfg)', "save prefix vars")
    anchor = '''\t\t\tif r.Form.Has("claudeChatPrefix") {
\t\t\t\tclaudeChatPrefix, err = validateCommandToken(r.FormValue("claudeChatPrefix"), "Claude Chat shortcut")
\t\t\t\tif err != nil {
\t\t\t\t\treturn err
\t\t\t\t}
\t\t\t}
'''
    addition = anchor + '''\t\t\tif r.Form.Has("geminiChatPrefix") {
\t\t\t\tgeminiChatPrefix, err = validateCommandToken(r.FormValue("geminiChatPrefix"), "Gemini Chat shortcut")
\t\t\t\tif err != nil {
\t\t\t\t\treturn err
\t\t\t\t}
\t\t\t}
'''
    t = replace_once(t, anchor, addition, "Gemini prefix validation")
    t = replace_once(t, 'prefixes := []string{codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix}', 'prefixes := []string{codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix}', "save duplicate list")
    t = replace_once(t, 'Codex, Claude, ChatGPT Chat, and Claude Chat shortcuts must all be different', 'Codex, Claude, ChatGPT Chat, Claude Chat, and Gemini Chat shortcuts must all be different', "duplicate error")
    t = replace_once(t, 'cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.NewSessionCommand = codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, newSession', 'cfg.CodexPrefix, cfg.ClaudePrefix, cfg.ChatGPTPrefix, cfg.ClaudeChatPrefix, cfg.GeminiChatPrefix, cfg.NewSessionCommand = codexPrefix, claudePrefix, chatGPTPrefix, claudeChatPrefix, geminiChatPrefix, newSession', "save prefixes")
    t = replace_once(t, 'for _, agent := range []string{"C", "A", "G", "H"} {', 'for _, agent := range []string{"C", "A", "G", "H", "M"} {', "apply access M")
    t = replace_once(t, '\t\tcfg.ClaudeChat.Instruction = ""\n', '\t\tcfg.ClaudeChat.Instruction = ""\n\t\tcfg.GeminiChat.Instruction = ""\n', "clear Gemini instruction")
    return t
patch("ui_actions.go", actions_patch)

# ---------------------------------------------------------------------------
# Local HTTP router and app lifecycle
# ---------------------------------------------------------------------------
def webui_patch(t):
    t = replace_once(t, '\tm.HandleFunc("/claude-chat/status.json", a.requireAuth(a.claudeChatStatusJSON))\n', '\tm.HandleFunc("/claude-chat/status.json", a.requireAuth(a.claudeChatStatusJSON))\n\tm.HandleFunc("/gemini-chat/status.json", a.requireAuth(a.geminiChatStatusJSON))\n', "Gemini status route")
    t = replace_once(t, '\t\t"/claude-chat/disconnect": a.claudeChatDisconnect,\n', '\t\t"/claude-chat/disconnect": a.claudeChatDisconnect,\n\t\t"/gemini-chat/connect":    a.geminiChatConnect,\n\t\t"/gemini-chat/test":       a.geminiChatTest,\n\t\t"/gemini-chat/disconnect": a.geminiChatDisconnect,\n', "Gemini action routes")
    return t
patch("webui.go", webui_patch)


def main_patch(t):
    t = replace_once(t, '\t\tcfg.Security.ClaudeChatAgentMigrated = true\n', '\t\tcfg.Security.ClaudeChatAgentMigrated = true\n\t\tcfg.Security.GeminiChatAgentMigrated = true\n', "new config migration M")
    t = replace_once(t, '!chatGPTBrowserStillOpen(dataDir) && !claudeChatBrowserStillOpen(dataDir)', '!chatGPTBrowserStillOpen(dataDir) && !claudeChatBrowserStillOpen(dataDir) && !geminiChatBrowserStillOpen(dataDir)', "shutdown Gemini worker")
    return t
patch("main.go", main_patch)

# ---------------------------------------------------------------------------
# Final Agents-pane augmentation. It deliberately composes all earlier page
# augmentations instead of replacing them, so Codex/Claude/ChatGPT/Claude Chat
# remain untouched.
# ---------------------------------------------------------------------------
ui = read("zzzzz_claude_chat_ui.go")
for old, new in [
    ("ClaudeChat", "GeminiChat"), ("claudeChat", "geminiChat"),
    ("Claude Chat", "Gemini Chat"), ("claude-chat", "gemini-chat"),
    ("Claude", "Gemini"), ("claude", "gemini"),
]:
    ui = ui.replace(old, new)
ui = ui.replace('registerPage("agents", geminiChatDirectUI(chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML))))', 'registerPage("agents", geminiChatDirectUI(claudeChatDirectUI(chatGPTDirectUI(agentConnectFirstRunHTML(agentsPageHTML)))))')
ui = ui.replace('{{brand "gemini"}}', '{{brand "google"}}').replace('bmark gemini', 'bmark google').replace('bmark lg gemini', 'bmark lg google')
ui = ui.replace("Regular Gemini at gemini.ai in FlipAi's private persistent browser session. No Gemini Desktop app or Gemini Code CLI is required.", "Regular Gemini at gemini.google.com in FlipAi's private persistent browser session. No Gemini API or Gemini CLI is required.")
ui = ui.replace('C:, A:, G:, or {{.GeminiChatAccess.Prefix}}:', 'C:, A:, G:, H:, or {{.GeminiChatAccess.Prefix}}:')
ui = ui.replace('Codex, Gemini, ChatGPT Chat, and Gemini Chat.', 'Codex, Claude, ChatGPT Chat, Claude Chat, and Gemini Chat.')
ui = ui.replace('Claude site', 'Gemini site')
# The cloned rail insertion originally looks for ChatGPT. Insert Gemini after
# Claude Chat instead so the visible order is Codex, Claude, ChatGPT, Claude Chat, Gemini.
old_rail_anchor = '''      <label class="agent-item" for="agent-chatgpt">
        <span class="bmark codex">{{brand "codex"}}</span>
        <span class="agent-item-copy">
          <b>ChatGPT Chat <span class="agent-chip warn" id="chatgpt-rail-status">Checking</span></b>
          <span>Answers {{.ChatGPTAccess.Prefix}}: messages</span>
        </span>
      </label>'''
# After provider replacement, the generated function still contains this anchor.
# Replace its insertion logic with an anchor matching the already-augmented Claude Chat item.
old_logic_start = ui.index('\tconst chatRail = `')
old_logic_end = ui.index('\tbody = replaceAgentUIOnce(body, chatRail, chatRail+geminiChatRail, "Gemini Chat rail item")') + len('\tbody = replaceAgentUIOnce(body, chatRail, chatRail+geminiChatRail, "Gemini Chat rail item")')
new_logic = r'''\tconst geminiRailAnchor = `      <label class="agent-item" for="agent-claude-chat">
        <span class="bmark claude">{{brand "claude"}}</span>
        <span class="agent-item-copy">
          <b>Claude Chat <span class="agent-chip warn" id="claude-chat-rail-status">Checking</span></b>
          <span>Answers {{.ClaudeChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
\tconst geminiChatRail = `
      <label class="agent-item" for="agent-gemini-chat">
        <span class="bmark google">{{brand "google"}}</span>
        <span class="agent-item-copy">
          <b>Gemini Chat <span class="agent-chip warn" id="gemini-chat-rail-status">Checking</span></b>
          <span>Answers {{.GeminiChatAccess.Prefix}}: messages</span>
        </span>
      </label>`
\tbody = replaceAgentUIOnce(body, geminiRailAnchor, geminiRailAnchor+geminiChatRail, "Gemini Chat rail item")'''
ui = ui[:old_logic_start] + new_logic + ui[old_logic_end:]
# Update the top routing explanation in the fully composed page.
needle = '<p>C: selects Codex, A: selects Claude, and G: selects ChatGPT Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>'
replacement = '<p>C: selects Codex, A: selects Claude, G: selects ChatGPT Chat, H: selects Claude Chat, and M: selects Gemini Chat. After a selection, unprefixed follow-up texts stay with that agent until you switch again.</p>'
insert_at = ui.index('func geminiChatDirectUI(body string) string {') + len('func geminiChatDirectUI(body string) string {')
ui = ui[:insert_at] + f'\n\tbody = strings.Replace(body, `{needle}`, `{replacement}`, 1)' + ui[insert_at:]
write("zzzzzz_gemini_chat_ui.go", ui)

# ---------------------------------------------------------------------------
# Tests: prevent the exact route omission that broke Claude Chat, and verify
# Gemini's routing prefix is independent.
# ---------------------------------------------------------------------------
write("gemini_chat_routes_test.go", r'''package main

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestGeminiChatActionRoutesAreRegistered(t *testing.T) {
    const token = "gemini-chat-route-test-token"
    app := &App{dataDir: t.TempDir(), cfg: Config{LocalToken: token}}
    handler := app.handler()
    for _, path := range []string{"/gemini-chat/connect", "/gemini-chat/test", "/gemini-chat/disconnect"} {
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

func TestGeminiChatSMSPrefix(t *testing.T) {
    cfg := defaultConfig(t.TempDir())
    got, err := parseGeminiChatSMSCommand("M: hello Gemini", cfg)
    if err != nil { t.Fatal(err) }
    if got.Agent != "M" || got.Text != "hello Gemini" {
        t.Fatalf("got agent=%q text=%q", got.Agent, got.Text)
    }
    if len(cfg.GeminiChat.Phones) != 0 || cfg.GeminiChat.RequireCode {
        t.Fatal("new Gemini Chat security boundary must not inherit phones or a required PIN")
    }
}
''')

write("VERSION", "0.46.21\n")

# Sanity checks before gofmt/go test.
required = {
    "config.go": ["GeminiChatPrefix", "GeminiChatConfig", 'version = "0.46.21"'],
    "agents.go": ['{"M", "Gemini Chat"}', "migrateGeminiChatAgent"],
    "webui.go": ["/gemini-chat/connect", "/gemini-chat/status.json"],
    "bridge.go": ["runGeminiChatSMS", "newGeminiChatConversation"],
    "zzzzzz_gemini_chat_ui.go": ["Gemini Chat", "/gemini-chat/connect", ".GeminiChatAccess"],
    "gemini_chat_webview_windows.go": ["https://gemini.google.com", "rich-textarea", "flipGeminiChatStatus"],
}
for f, needles in required.items():
    text = read(f)
    for needle in needles:
        if needle not in text:
            raise RuntimeError(f"{f}: missing {needle!r}")

print("Gemini Chat integration applied")
