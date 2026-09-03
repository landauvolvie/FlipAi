from pathlib import Path
import re

p = Path(__file__).with_name("apply_browser_chat_v04623.py")
s = p.read_text(encoding="utf-8")
replacement = r'''# Give every browser-chat 90s page driver the same 95s DevTools allowance as ChatGPT.
voice = read("voice_cdp_windows.go")
pattern = r''' + "'''" + r'''\tif await &&\n\t\tstrings\.Contains\(expression, "const deadline=Date\.now\(\)\+90000;"\) &&\n\t\tstrings\.Contains\(expression, `data-message-author-role=\\"assistant\\"`\) \{\n\t\treturn chatGPTTurnDevToolsTimeout\n\t\}''' + "'''" + r'''
new_timeout = ''' + "'''" + r'''\tif await &&
\t\tstrings.Contains(expression, "const deadline=Date.now()+90000;") &&
\t\t(strings.Contains(expression, `data-message-author-role=\"assistant\"`) ||
\t\t\tstrings.Contains(expression, "model-response") ||
\t\t\tstrings.Contains(expression, "grokResponse")) {
\t\treturn chatGPTTurnDevToolsTimeout
\t}''' + "'''" + r'''
voice, n = re.subn(pattern, new_timeout, voice, count=1)
if n != 1:
    raise SystemExit(f"voice timeout: expected one replacement, got {n}")
write("voice_cdp_windows.go", voice)

'''
s, n = re.subn(r'# Give every browser-chat 90s page driver.*?# Gemini: preserve multiline', replacement + '# Gemini: preserve multiline', s, count=1, flags=re.S)
if n != 1:
    raise SystemExit(f"patcher timeout section: expected one replacement, got {n}")
p.write_text(s, encoding="utf-8")
print("Repaired v0.46.23 patcher")
