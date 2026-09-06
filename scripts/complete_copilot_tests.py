from pathlib import Path

p = Path("ui_page_agents_test.go")
text = p.read_text(encoding="utf-8")
repls = [
    ('`name="geminiChatPrefix"`, `name="grokChatPrefix"`,', '`name="geminiChatPrefix"`, `name="grokChatPrefix"`, `name="copilotChatPrefix"`,'),
    ('`name="geminiChatRequireCode"`, `name="grokChatRequireCode"`,', '`name="geminiChatRequireCode"`, `name="grokChatRequireCode"`, `name="copilotChatRequireCode"`,'),
    ('`name="geminiChatAckDelay"`, `name="grokChatAckDelay"`,', '`name="geminiChatAckDelay"`, `name="grokChatAckDelay"`, `name="copilotChatAckDelay"`,'),
    ('strings.Count(body, `name="sharedReplyStyle"`) != 6', 'strings.Count(body, `name="sharedReplyStyle"`) != 7'),
    ('shared SMS instruction should be available from all six panes', 'shared SMS instruction should be available from all seven panes'),
    ('`name="geminiChatPrefix"`, `name="grokChatPrefix"`, `name="sharedReplyStyle"`', '`name="geminiChatPrefix"`, `name="grokChatPrefix"`, `name="copilotChatPrefix"`, `name="sharedReplyStyle"`'),
    ('// FlipAi has one SMS instruction for all six agents.', '// FlipAi has one SMS instruction for all seven agents.'),
    ('cfg.GeminiChat.Instruction != "" || cfg.GrokChat.Instruction != ""', 'cfg.GeminiChat.Instruction != "" || cfg.GrokChat.Instruction != "" || cfg.CopilotChat.Instruction != ""'),
    ('[]string{"C", "A", "G", "H", "M", "X"}', '[]string{"C", "A", "G", "H", "M", "X", "P"}'),
]
for old, new in repls:
    if old not in text:
        raise SystemExit(f"expected test source not found: {old}")
    text = text.replace(old, new, 1)
p.write_text(text, encoding="utf-8")
print("Copilot agent tests updated")
