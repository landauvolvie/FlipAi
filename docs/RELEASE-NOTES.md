# FlipAi v0.46.16

This hotfix fixes incomplete ChatGPT/Codex/Claude text replies arriving through Google Voice.

## Full answer arrives as one FlipAi reply

FlipAi previously split a longer agent answer into numbered parts such as `1/2` and `2/2` before replying through Google Voice's email gateway. The gateway only forwarded the first logical line of each email body, so a complete multi-line answer visible in the web chat could reach the phone as only two tiny fragments while the middle of the answer disappeared.

v0.46.16 now reassembles FlipAi's numbered chunks before delivery, removes the part prefixes, and collapses transport newlines so the gateway receives the complete answer as one logical outbound message. Bullets and punctuation are preserved in the text.

The earlier v0.46.15 ChatGPT `Runtime.evaluate` timeout fix remains unchanged.

## Verification

Regression coverage verifies multiline answers keep all content, split parts are reassembled in order, and ordinary text such as `1/2 cup` is not mistaken for a generated split reply. The full Windows build and real-browser test suite run before publishing.
