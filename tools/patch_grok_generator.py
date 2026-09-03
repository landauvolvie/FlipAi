from pathlib import Path

p = Path('tools/integrate_grok_chat.py')
s = p.read_text(encoding='utf-8')

# Adapt the temporary generator to the exact compact/aligned Config layout on
# v0.46.21. Top-level agent config wrappers intentionally do not use omitempty.
changes = [
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
        '\\tGeminiChat  GeminiChatConfig  `json:"geminiChat"`\\n\\tSecurity',
    ),
    (
        '\\tGeminiChat GeminiChatConfig `json:"geminiChat,omitempty"`\\n\\tGrokChat   GrokChatConfig   `json:"grokChat,omitempty"`\\n\\tGmail',
        '\\tGeminiChat  GeminiChatConfig  `json:"geminiChat"`\\n\\tGrokChat    GrokChatConfig    `json:"grokChat"`\\n\\tSecurity',
    ),
    (
        'type GeminiChatConfig struct {\\n\\tAgentSettings\\n}\\n',
        'type GeminiChatConfig struct{ AgentSettings }\\n',
    ),
    (
        'type GrokChatConfig struct {\\n\\tAgentSettings\\n}\\n',
        'type GrokChatConfig struct{ AgentSettings }\\n',
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
for old, new in changes:
    s = s.replace(old, new)

required = [
    'GeminiChatPrefix   string `json:"geminiChatPrefix,omitempty"`',
    'GrokChatPrefix     string `json:"grokChatPrefix,omitempty"`',
    '\\tGeminiChat  GeminiChatConfig  `json:"geminiChat"`\\n\\tGrokChat    GrokChatConfig    `json:"grokChat"`\\n\\tSecurity',
    'type GeminiChatConfig struct{ AgentSettings }',
    'type GrokChatConfig struct{ AgentSettings }',
]
for token in required:
    if token not in s:
        raise SystemExit(f'adapted generator is still missing {token!r}')

p.write_text(s, encoding='utf-8')
print('Grok generator adapted to current compact Config layout')
