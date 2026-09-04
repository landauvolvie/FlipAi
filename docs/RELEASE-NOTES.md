# FlipAi v0.46.32

Image-sharing release for regular browser chat agents.

## Universal chat images

- Google Voice image messages can now be sent to ChatGPT Chat, Claude Chat, Gemini Chat, and Grok Chat, not only Codex/Claude Code.
- Attachment-only MMS stays with the phone number's selected/sticky chat agent instead of falling back to another agent.
- Images are attached through each signed-in provider's normal WebView file input; FlipAi does not encode the image into the model prompt or use a provider API.
- Multiple image attachments are supported within FlipAi's existing inbound attachment limits.

## Safety and isolation

- Browser-chat image paths are restricted to FlipAi-created temporary inbound folders and validated as regular image files before the WebView can select them.
- Temporary images are cleaned up after the turn, and each provider keeps its existing isolated WebView profile and authenticated loopback control channel.
- Existing text-only behavior is unchanged when no image is attached.

No Authenticode/code-signing certificate is included in this release.
