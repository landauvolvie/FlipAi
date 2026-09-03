from pathlib import Path

p = Path('tools/integrate_grok_chat.py')
s = p.read_text(encoding='utf-8')

# Adapt the temporary generator to the exact compact/aligned source layout on
# v0.46.21. These substitutions alter generator literals only; the generator's
# source replacements remain fail-fast.
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
    ('type GeminiChatConfig struct {\\n\\tAgentSettings\\n}\\n', 'type GeminiChatConfig struct{ AgentSettings }\\n'),
    ('type GrokChatConfig struct {\\n\\tAgentSettings\\n}\\n', 'type GrokChatConfig struct{ AgentSettings }\\n'),
    ('// values are "C", "A", "G", "H", or "M".', '// for each allowed phone. Explicit C:, A:, G:, or H: changes it.'),
    ('// values are "C", "A", "G", "H", "M", or "X".', '// for each allowed phone. Explicit C:, A:, G:, H:, M:, or X: changes it.'),
    ("'GrokChat GrokChatConfig'", "'GrokChatConfig'"),
]
for old, new in changes:
    s = s.replace(old, new)

# commands.go aligns constant names before '='.
old_stmt = '''replace('commands.go', '\\tdefaultGeminiChatPrefix = "M"\\n', '\\tdefaultGeminiChatPrefix = "M"\\n\\tdefaultGrokChatPrefix   = "X"\\n', 1)'''
new_stmt = '''replace('commands.go', '\\tdefaultGeminiChatPrefix  = "M"\\n', '\\tdefaultGeminiChatPrefix  = "M"\\n\\tdefaultGrokChatPrefix    = "X"\\n', 1)'''
s = s.replace(old_stmt, new_stmt)
s = s.replace(
    '''replace('commands.go', '\\tdefaultGeminiChatPrefix  = "M"\\n', '\\tdefaultGeminiChatPrefix = "M"\\n\\tdefaultGrokChatPrefix   = "X"\\n', 1)''',
    new_stmt,
)

# bridge.go now keeps a final response string and activity events around every
# browser-agent turn. Replace the older generator block with the current shape.
old_bridge = '''s = s.replace('\\t\\tcase "M":\\n\\t\\t\\tif err := b.newGeminiChatConversation(ctx); err != nil {', '\\t\\tcase "M":\\n\\t\\t\\tif err := b.newGeminiChatConversation(ctx); err != nil {', 1)
needle = ''' + "'''" + '''\t\tcase "M":
\t\t\tif err := b.newGeminiChatConversation(ctx); err != nil {
\t\t\t\treturn err
\t\t\t}
''' + "'''" + '''
if needle not in s: raise SystemExit('bridge.go: Gemini NEW case missing')
s = s.replace(needle, needle + ''' + "'''" + '''\t\tcase "X":
\t\t\tif err := b.newGrokChatConversation(ctx); err != nil {
\t\t\t\treturn err
\t\t\t}
''' + "'''" + ''', 1)
needle = ''' + "'''" + '''\t} else if rc.Agent == "M" {
\t\tanswer, err = b.runGeminiChatSMS(ctx, rc.Text)
''' + "'''" + '''
if needle not in s: raise SystemExit('bridge.go: Gemini turn case missing')
s = s.replace(needle, needle + ''' + "'''" + '''\t} else if rc.Agent == "X" {
\t\tanswer, err = b.runGrokChatSMS(ctx, rc.Text)
''' + "'''" + ''', 1)'''
new_bridge = '''needle = ''' + "'''" + '''\t\tcase "M":
\t\t\terr = b.newGeminiChatConversation(ctx)
\t\t\tfinal = "New Gemini Chat conversation started."
''' + "'''" + '''
if needle not in s: raise SystemExit('bridge.go: current Gemini NEW case missing')
s = s.replace(needle, needle + ''' + "'''" + '''\t\tcase "X":
\t\t\terr = b.newGrokChatConversation(ctx)
\t\t\tfinal = "New Grok Chat conversation started."
''' + "'''" + ''', 1)
needle = ''' + "'''" + '''\t} else if rc.Agent == "M" {
\t\tb.event("info", "agent", "Gemini Chat command started", rc.Sender, "M", m.ID)
\t\tfinal, err = b.runGeminiChatSMS(ctx, rc.Text)
''' + "'''" + '''
if needle not in s: raise SystemExit('bridge.go: current Gemini turn case missing')
s = s.replace(needle, needle + ''' + "'''" + '''\t} else if rc.Agent == "X" {
\t\tb.event("info", "agent", "Grok Chat command started", rc.Sender, "X", m.ID)
\t\tfinal, err = b.runGrokChatSMS(ctx, rc.Text)
''' + "'''" + ''', 1)'''
if old_bridge not in s:
    raise SystemExit('temporary generator bridge block changed unexpectedly')
s = s.replace(old_bridge, new_bridge, 1)

required = [
    'GeminiChatPrefix   string `json:"geminiChatPrefix,omitempty"`',
    'GrokChatPrefix     string `json:"grokChatPrefix,omitempty"`',
    '\\tGeminiChat  GeminiChatConfig  `json:"geminiChat"`\\n\\tGrokChat    GrokChatConfig    `json:"grokChat"`\\n\\tSecurity',
    'type GeminiChatConfig struct{ AgentSettings }',
    'type GrokChatConfig struct{ AgentSettings }',
    'current Gemini NEW case missing',
    'Grok Chat command started',
]
for token in required:
    if token not in s:
        raise SystemExit(f'adapted generator is still missing {token!r}')

p.write_text(s, encoding='utf-8')
print('Grok generator adapted to current source layout')
