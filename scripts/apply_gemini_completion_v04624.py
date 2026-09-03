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
        raise SystemExit(f"{path}: expected one match, found {count}: {old[:120]!r}")
    write(path, text.replace(old, new, 1))


write("VERSION", "0.46.24\n")
replace_once("config.go", 'const version = "0.46.23"', 'const version = "0.46.24"')
write("docs/RELEASE-NOTES.md", """# FlipAi v0.46.24

Gemini Chat completion detection now finishes the SMS turn when Gemini has visibly completed its response, even if Gemini leaves a stale Stop-like control in the page.

## Gemini Chat

The browser driver now recognizes Gemini's finished-response action controls as a strong completion signal. It still keeps the existing no-Stop fast path, and adds a stable-text fallback so an obsolete or unrelated Stop element can never hold an already-finished reply for the full 90-second timeout.

This fixes the case where Gemini visibly answered immediately but FlipAi later texted `Gemini started answering but did not finish within 90 seconds.`
""")

replace_once(
    "gemini_chat_webview_windows.go",
    '''  const stop=()=>{const xs=unique([...all('button[aria-label*="Stop" i]'),...all('button[mattooltip*="Stop" i]'),...all('button[data-test-id*="stop" i]'),...all('button[data-testid*="stop" i]')]);return xs.find(b=>!b.disabled&&b.offsetParent!==null)||null};\n''',
    '''  const stop=()=>{const xs=unique([...all('button[aria-label*="Stop" i]'),...all('button[mattooltip*="Stop" i]'),...all('button[data-test-id*="stop" i]'),...all('button[data-testid*="stop" i]')]);return xs.find(b=>!b.disabled&&b.offsetParent!==null)||null};\n  const responseFinishedChrome=node=>{\n    if(!node)return false;\n    const root=node.closest('model-response,[data-test-id="model-response"],[data-testid="model-response"]')||node;\n    const scope=root.parentElement||root;\n    const buttons=unique([...Array.from(root.querySelectorAll('button')),...Array.from(scope.querySelectorAll('button'))]);\n    return buttons.some(b=>/good response|bad response|regenerate|copy response|more options|share/i.test(((b.getAttribute('aria-label')||'')+' '+(b.getAttribute('mattooltip')||'')+' '+(b.innerText||'')).trim()));\n  };\n''',
)

replace_once(
    "gemini_chat_webview_windows.go",
    '''    if(node){started=true;const now=text(node);if(now===last)stable++;else{last=now;stable=0}if(!stop()&&stable>=5)return {ok:true,reply:now||'Gemini completed the turn.',href:location.href}}\n''',
    '''    if(node){\n      started=true;\n      const now=text(node);\n      if(now===last)stable++;else{last=now;stable=0}\n      if(now&&responseFinishedChrome(node)&&stable>=2)return {ok:true,reply:now,href:location.href};\n      if(now&&!stop()&&stable>=5)return {ok:true,reply:now,href:location.href};\n      if(now&&stable>=16)return {ok:true,reply:now,href:location.href};\n    }\n''',
)

write("gemini_completion_regression_test.go", r'''package main

import (
    "os"
    "strings"
    "testing"
)

func TestGeminiFinishedResponseCannotBeHeldByStaleStopControl(t *testing.T) {
    b, err := os.ReadFile("gemini_chat_webview_windows.go")
    if err != nil {
        t.Fatal(err)
    }
    s := string(b)
    for _, want := range []string{
        "responseFinishedChrome",
        "good response|bad response|regenerate|copy response|more options|share",
        "responseFinishedChrome(node)&&stable>=2",
        "!stop()&&stable>=5",
        "stable>=16",
    } {
        if !strings.Contains(s, want) {
            t.Fatalf("Gemini completion driver is missing safeguard %q", want)
        }
    }
}
''')

print("Applied FlipAi v0.46.24 Gemini completion fix")
