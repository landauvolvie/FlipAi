# ChatGPT direct v0.46.9 implementation guardrails

The v0.46.8 real-machine evidence established that regular ChatGPT has no advertised localhost listener, DevTools port/pipe, ChatGPT/OpenAI named pipe, Windows AppService, or message-send activation protocol. `codex://` is navigation/activation evidence only and Codex pipes are not regular ChatGPT Chat connectivity.

v0.46.9 therefore treats the installed Electron `app.asar` archive as the next read-only source of truth. A future direct-connection implementation must require one of these stronger forms of evidence before enabling SMS routing:

1. A regular-ChatGPT IPC/preload bridge channel whose handler and caller can both be attributed to app-bundle files.
2. A regular-ChatGPT app-bundle request call site whose session mechanism can be used through a supported OS/app interface without copying credentials.
3. A newly exposed ChatGPT-owned local/AppService interface that can be ownership-verified.

Static strings, generic bundled tooling, activation-only protocols, Codex pipes, or unowned named-pipe names never count as a connected ChatGPT agent.
