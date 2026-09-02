# FlipAi v0.46.18

This hotfix fixes two regular ChatGPT SMS regressions visible in the ChatGPT and Google Voice screenshots.

## Only the new ChatGPT turn is returned

FlipAi previously identified a new ChatGPT answer by checking whether the text of the last assistant element had changed. ChatGPT can update an older assistant element while it renders controls or an image, so a later SMS could accidentally reuse the previous turn's answer. That is why an image request could return the earlier Gmail summary followed by FlipAi's image-delivery notice.

v0.46.18 records the user and assistant message boundaries before sending the SMS and accepts only an assistant node created for the new turn. Mutations to older assistant messages are ignored. Image-only turns are also recognized as the new turn even when their assistant node has no text.

## Clean SMS prompts in ChatGPT

Regular ChatGPT no longer sees the internal `<sms_command>` wrapper. The web chat now shows only the user's message, followed by the shared short SMS instruction, for example:

`Generate me an image of a nice waterfall`

`Reply for SMS. Keep it brief and plain text.`

The same shared instruction remains editable in FlipAi settings.

## Verification

The real Chromium browser test now deliberately mutates an old assistant response before creating the new response. The test fails with the old extraction rule and passes only when FlipAi returns the new turn. A Go regression test also verifies that ChatGPT SMS prompts contain no internal wrapper.
