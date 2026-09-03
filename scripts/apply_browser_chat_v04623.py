from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]


def read(path):
    return (ROOT / path).read_text(encoding="utf-8")


def write(path, text):
    (ROOT / path).write_text(text, encoding="utf-8")


def replace_once(path, old, new):
    text = read(path)
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected one match, found {count}: {old[:100]!r}")
    write(path, text.replace(old, new, 1))


# Version + release notes.
write("VERSION", "0.46.23\n")
replace_once("config.go", 'const version = "0.46.22"', 'const version = "0.46.23"')
write("docs/RELEASE-NOTES.md", """# FlipAi v0.46.23

Gemini Chat and Grok Chat now use the same long-running WebView turn allowance as ChatGPT Chat, so a model reply that is already visible in the browser is not falsely reported as a Runtime.evaluate timeout.

## Gemini Chat

Gemini's WebView turn is no longer limited by the 8-second Google Voice DevTools deadline. The existing 90-second page driver now receives the same 95-second DevTools allowance as ChatGPT Chat.

Multiline SMS prompts are inserted into Gemini's Quill/contenteditable composer line by line. This preserves the blank line and shared SMS reply instruction instead of dropping everything after the user's first line.

## Grok Chat

Grok sign-in detection now uses Grok's actual ProseMirror/TipTap composer and explicit login-page detection instead of a copied Gemini selector plus a broad search for any Sign in/Log in button.

Grok response tracking now accepts either a newly-created assistant response or a changed last response container. It also ignores hidden Stop controls. A fast visible Grok answer therefore completes the FlipAi turn promptly, cancelling delayed working/progress texts instead of continuing to report that Grok is still working.

Regression coverage verifies the long DevTools timeout for ChatGPT, Gemini and Grok and locks in the Gemini multiline and Grok sign-in/response fixes.
""")

# Give every browser-chat 90s page driver the same 95s DevTools allowance as ChatGPT.
replace_once(
    "voice_cdp_windows.go",
    '''\tif await &&\n\t\tstrings.Contains(expression, "const deadline=Date.now()+90000;") &&\n\t\tstrings.Contains(expression, `data-message-author-role=\\"assistant\\"`) {\n\t\treturn chatGPTTurnDevToolsTimeout\n\t}\n''',
    '''\tif await &&\n\t\tstrings.Contains(expression, "const deadline=Date.now()+90000;") &&\n\t\t(strings.Contains(expression, `data-message-author-role=\\"assistant\\"`) ||\n\t\t\tstrings.Contains(expression, "model-response") ||\n\t\t\tstrings.Contains(expression, "grokResponse")) {\n\t\treturn chatGPTTurnDevToolsTimeout\n\t}\n''',
)

# Gemini: preserve multiline SMS prompt/instruction in Quill/contenteditable.
replace_once(
    "gemini_chat_webview_windows.go",
    '''      const sel=getSelection(),range=document.createRange();range.selectNodeContents(c);sel.removeAllRanges();sel.addRange(range);\n      document.execCommand('delete',false,null);document.execCommand('insertText',false,input);\n      c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));c.dispatchEvent(new Event('change',{bubbles:true}));\n''',
    '''      const sel=getSelection(),range=document.createRange();range.selectNodeContents(c);sel.removeAllRanges();sel.addRange(range);\n      document.execCommand('delete',false,null);\n      const lines=String(input).replace(/\\r\\n?/g,'\\n').split('\\n');\n      for(let i=0;i<lines.length;i++){\n        if(i>0)document.execCommand('insertLineBreak',false,null);\n        if(lines[i])document.execCommand('insertText',false,lines[i]);\n      }\n      c.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:input}));c.dispatchEvent(new Event('change',{bubbles:true}));\n''',
)
replace_once(
    "gemini_chat_webview_windows.go",
    '''  const stop=()=>document.querySelector('button[aria-label*="Stop" i],button[mattooltip*="Stop" i],button[data-test-id*="stop" i],button[data-testid*="stop" i]');\n''',
    '''  const stop=()=>{const xs=unique([...all('button[aria-label*="Stop" i]'),...all('button[mattooltip*="Stop" i]'),...all('button[data-test-id*="stop" i]'),...all('button[data-testid*="stop" i]')]);return xs.find(b=>!b.disabled&&b.offsetParent!==null)||null};\n''',
)

# Grok: use its real composer + explicit login page state. Do not fail because an unrelated Sign in link exists.
grok = read("grok_chat_webview_windows.go")
new_monitor = r'''const grokChatPageMonitorJS = `(function(){
  if(window.__flipAiGrokChatMonitor)return;
  window.__flipAiGrokChatMonitor=true;
  const composer=()=>document.querySelector('div.ProseMirror[contenteditable="true"][role="textbox"],div.tiptap.ProseMirror[contenteditable="true"],[data-testid="grokInput"][contenteditable="true"],[data-testid="grokInput"],[contenteditable="true"][role="textbox"],textarea[placeholder],textarea');
  const loginPage=()=>/\/(?:login|sign-?in)(?:\/|$)/i.test(location.pathname)||!!document.querySelector('form input[type="email"],form input[name="username"],form input[autocomplete="username"]');
  const signed=()=>/(^|\.)grok\.com$/i.test(location.hostname) && !!composer() && !loginPage();
  async function tick(){
    try{ if(window.flipGrokChatStatus) await window.flipGrokChatStatus(signed(), location.href); }catch(e){}
  }
  setInterval(tick,1000); addEventListener('load',tick); setTimeout(tick,350);
})();`

const grokChatSignedInJS = `(()=>{const c=document.querySelector('div.ProseMirror[contenteditable="true"][role="textbox"],div.tiptap.ProseMirror[contenteditable="true"],[data-testid="grokInput"][contenteditable="true"],[data-testid="grokInput"],[contenteditable="true"][role="textbox"],textarea[placeholder],textarea');const loginPage=/\/(?:login|sign-?in)(?:\/|$)/i.test(location.pathname)||!!document.querySelector('form input[type="email"],form input[name="username"],form input[autocomplete="username"]');return /(^|\.)grok\.com$/i.test(location.hostname)&&!!c&&!loginPage})()`'''
pattern = r"const grokChatPageMonitorJS = `.*?`\n\nconst grokChatSignedInJS = `.*?`"
grok, n = re.subn(pattern, new_monitor, grok, count=1, flags=re.S)
if n != 1:
    raise SystemExit(f"grok monitor: expected one replacement, got {n}")
write("grok_chat_webview_windows.go", grok)

replace_once(
    "grok_chat_webview_windows.go",
    '''  const stop=()=>document.querySelector('button[data-testid*="stop" i],button[aria-label*="stop" i]');\n''',
    '''  const stop=()=>{const xs=unique([...all('button[data-testid*="stop" i]'),...all('button[aria-label*="stop" i]')]);return xs.find(b=>!b.disabled&&b.offsetParent!==null)||null};\n''',
)
replace_once(
    "grok_chat_webview_windows.go",
    '''  const before=assistants();\n  const beforeSet=new Set(before);\n  const responseForTurn=()=>{const current=assistants();for(let i=current.length-1;i>=0;i--){if(!beforeSet.has(current[i]))return current[i]}return null};\n''',
    '''  const before=assistants();\n  const beforeCount=before.length;\n  const beforeLast=beforeCount?text(before[beforeCount-1]):'';\n  const responseForTurn=()=>{const current=assistants();if(!current.length)return null;const last=current[current.length-1];if(current.length>beforeCount)return last;return text(last)&&text(last)!==beforeLast?last:null};\n''',
)

# Expand the existing timeout regression to all browser chat turn drivers.
write("voice_cdp_chatgpt_timeout_windows_test.go", r'''//go:build windows

package main

import (
    "fmt"
    "testing"
)

func TestWebViewDevToolsCallTimeoutKeepsVoiceShortAndBrowserChatsLong(t *testing.T) {
    ordinary := map[string]any{
        "expression":    "(()=>true)()",
        "returnByValue": true,
        "awaitPromise":  true,
    }
    if got := webViewDevToolsCallTimeout("Runtime.evaluate", ordinary); got != voiceDevToolsTimeout {
        t.Fatalf("ordinary WebView eval timeout = %v, want %v", got, voiceDevToolsTimeout)
    }

    turns := map[string]string{
        "ChatGPT": fmt.Sprintf(chatGPTTurnJS, chatGPTJSString("hello")),
        "Gemini":  fmt.Sprintf(geminiChatTurnJS, geminiChatJSString("hello")),
        "Grok":    fmt.Sprintf(grokChatTurnJS, grokChatJSString("hello")),
    }
    for name, expression := range turns {
        t.Run(name, func(t *testing.T) {
            turn := map[string]any{
                "expression":    expression,
                "returnByValue": true,
                "awaitPromise":  true,
            }
            if got := webViewDevToolsCallTimeout("Runtime.evaluate", turn); got != chatGPTTurnDevToolsTimeout {
                t.Fatalf("%s turn timeout = %v, want %v", name, got, chatGPTTurnDevToolsTimeout)
            }
        })
    }
}
''')

# Source-level regression for the provider-specific DOM bugs. This runs on Linux too,
# where the Windows WebView source is not compiled.
write("browser_chat_provider_regression_test.go", r'''package main

import (
    "os"
    "strings"
    "testing"
)

func TestGeminiWebViewPreservesMultilineSMSPrompt(t *testing.T) {
    b, err := os.ReadFile("gemini_chat_webview_windows.go")
    if err != nil {
        t.Fatal(err)
    }
    s := string(b)
    for _, want := range []string{"split('\\n')", "insertLineBreak", "data:input"} {
        if !strings.Contains(s, want) {
            t.Fatalf("Gemini WebView is missing multiline prompt safeguard %q", want)
        }
    }
}

func TestGrokWebViewUsesGrokSignInAndMutableResponseDetection(t *testing.T) {
    b, err := os.ReadFile("grok_chat_webview_windows.go")
    if err != nil {
        t.Fatal(err)
    }
    s := string(b)
    for _, want := range []string{"div.ProseMirror", "loginPage", "beforeCount", "beforeLast", "offsetParent!==null"} {
        if !strings.Contains(s, want) {
            t.Fatalf("Grok WebView is missing regression safeguard %q", want)
        }
    }
    if strings.Contains(s, "const grokChatSignedInJS = `(()=>{const c=document.querySelector('rich-textarea") {
        t.Fatal("Grok signed-in probe regressed to Gemini's rich-textarea selector")
    }
}
''')

print("Applied FlipAi v0.46.23 browser chat fixes")
