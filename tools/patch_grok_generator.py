from pathlib import Path

p = Path('tools/integrate_grok_chat.py')
s = p.read_text(encoding='utf-8')

patches = [
    (
        'GeminiChatPrefix string `json:"geminiChatPrefix,omitempty"`\\n\\tNewSessionCommand',
        'GeminiChatPrefix   string `json:"geminiChatPrefix,omitempty"`\\n\\tNewSessionCommand',
    ),
    (
        'GeminiChatPrefix string `json:"geminiChatPrefix,omitempty"`\\n\\tGrokChatPrefix   string `json:"grokChatPrefix,omitempty"`\\n\\tNewSessionCommand',
        'GeminiChatPrefix   string `json:"geminiChatPrefix,omitempty"`\\n\\tGrokChatPrefix     string `json:"grokChatPrefix,omitempty"`\\n\\tNewSessionCommand',
    ),
    (
        '\\tGeminiChat GeminiChatConfig `json:"geminiChat,omitempty"`\\n\\tGmail',
        '\\tGeminiChat  GeminiChatConfig  `json:"geminiChat,omitempty"`\\n\\tSecurity',
    ),
    (
        '\\tGeminiChat GeminiChatConfig `json:"geminiChat,omitempty"`\\n\\tGrokChat   GrokChatConfig   `json:"grokChat,omitempty"`\\n\\tGmail',
        '\\tGeminiChat  GeminiChatConfig  `json:"geminiChat,omitempty"`\\n\\tGrokChat    GrokChatConfig    `json:"grokChat,omitempty"`\\n\\tSecurity',
    ),
    (
        'type GeminiChatConfig struct {\\n\\tAgentSettings\\n}\\n',
        'type GeminiChatConfig struct{ AgentSettings }\\n',
    ),
    (
        'type GeminiChatConfig struct {\\n\\tAgentSettings\\n}\\n\\ntype GrokChatConfig struct {\\n\\tAgentSettings\\n}\\n',
        'type GeminiChatConfig struct{ AgentSettings }\\n\\ntype GrokChatConfig struct{ AgentSettings }\\n',
    ),
    (
        '// values are "C", "A", "G", "H", or "M".',
        '// for each allowed phone. Explicit C:, A:, G:, or H: changes it.',
    ),
    (
        '// values are "C", "A", "G", "H", "M", or "X".',
        '// for each allowed phone. Explicit C:, A:, G:, H:, M:, or X: changes it.',
    ),
    (
        "'GrokChat GrokChatConfig'",
        "'GrokChatConfig'",
    ),
]

for old, new in patches:
    if old not in s:
        raise SystemExit(f'generator patch target missing: {old!r}')
    s = s.replace(old, new)

p.write_text(s, encoding='utf-8')
print('Grok generator adapted to current compact Config layout')
