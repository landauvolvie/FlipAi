# FlipAi v0.46.74

Voice-note attachment forwarding for browser chats.

- Recognizes common phone recordings (M4A, MP3, WAV, AMR, 3GP, AAC, Ogg/Opus, and FLAC), including generic binary MIME attachments.
- Keeps recording bytes intact and supplies usable audio filenames and extensions.
- Captures Google Voice audio download links and hidden audio players with visible controls.
- Selects a compatible file input instead of falling back to an image-only picker. Shared by ChatGPT Chat, Claude Chat, Grok, Gemini, Microsoft Copilot Chat, and Muse.
- Waits for an audio upload receipt and for upload progress to finish before submitting the prompt. Reports rejected or unconfirmed uploads instead of silently sending without the voice note.
- Voice-note-only messages ask the selected model to respond to the spoken request.

The destination website must support the recording's format and audio-file uploads. This change does not add audio understanding to providers that do not offer it, transcribe recordings through another service, or disguise audio as another file type.

Validation: MIME and routing regression tests, browser tests for compatible file pickers, delayed uploads, rejections, Google Voice download links, and hidden audio players, plus the normal Linux/Windows release checks.

The quiet update icon and install-on-restart behavior from v0.46.73 are retained.
