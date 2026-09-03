from pathlib import Path

p = Path('tools/integrate_grok_chat.py')
s = p.read_text(encoding='utf-8')

# The current config.go uses compact one-line wrapper structs and aligned fields.
# Rewrite only the generator's expected/replacement literals; doing this without
# per-step assertions keeps overlapping replacements idempotent.
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
    'type GeminiChatConfig struct{ AgentSettings }',
    'type GrokChatConfig struct{ AgentSettings }',
    'GrokChatConfig',
]
for token in required:
    if token not in s:
        raise SystemExit(f'adapted generator is still missing {token!r}')

p.write_text(s, encoding='utf-8')
print('Grok generator adapted to current compact Config layout')
