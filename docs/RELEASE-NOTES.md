# FlipAi v0.46.24

Gemini Chat completion detection now finishes the SMS turn when Gemini has visibly completed its response, even if Gemini leaves a stale Stop-like control in the page.

## Gemini Chat

The browser driver now recognizes Gemini's finished-response action controls as a strong completion signal. It still keeps the existing no-Stop fast path, and adds a stable-text fallback so an obsolete or unrelated Stop element can never hold an already-finished reply for the full 90-second timeout.

This fixes the case where Gemini visibly answered immediately but FlipAi later texted `Gemini started answering but did not finish within 90 seconds.`
