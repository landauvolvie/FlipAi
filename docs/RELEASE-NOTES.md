# FlipAi v0.46.25

Gemini Chat SMS replies no longer include Gemini's hidden accessibility speaker label.

## Gemini Chat

Gemini's page can expose response text to automation as `Gemini said ...` even though the visible answer does not show that label. FlipAi now strips that provider-only accessibility prefix before sending the reply through Google Voice.

The SMS therefore contains only the model's actual answer, matching the behavior of ChatGPT Chat.
