# FlipAi v0.46.8

This release turns the regular ChatGPT direct-backend experiment into a full Windows architecture survey. v0.46.7 found useful-looking strings, but the real-PC result showed that many came from bundled Playwright/browser/Codex tooling rather than proven regular ChatGPT Chat code. v0.46.8 is designed to finish that uncertainty in one diagnostic instead of adding another narrow probe.

## Full ChatGPT architecture diagnostic

- Renames the action to **Run full architecture diagnostic**.
- Keeps checking for a ChatGPT-owned localhost listener, Chromium DevTools endpoint, remote-debugging pipe, and ChatGPT/OpenAI-named pipes while continuing to exclude neighboring `codex-*` pipes from connection decisions.
- Inventories the ChatGPT process tree without reading full command lines, including child/runtime process names and parent relationships.
- Checks loaded module names for WebView2, Edge, Electron/Chromium, WinUI and related runtime signals.
- Reads the AppX package identity and manifest to report application entry points, Windows AppServices, other extension categories, and registered activation protocols without invoking them.
- Reports top-level installed package entries and the main window class so FlipAi can distinguish a native shell, WebView2 host and Electron/Chromium-style client.
- Records only safe active-network metadata (remote address/port) plus OpenAI/ChatGPT-related DNS names already cached by Windows. It does not capture packets, HTTP headers, request bodies or TLS/session secrets.
- Re-runs package protocol discovery with **source attribution**: every strong Chat/backend/conversation/IPC marker is tied to the exact installed file that contained it.
- Separates app-specific marker sources from generic/runtime sources such as `node_modules`, `cua_node`, Playwright, PDF.js, browser/chrome plugins, Codex app tools and Electron's default app.
- Produces a final **Direct-backend assessment** explaining which path is actually worth implementing next: owned local transport, Windows AppService, activation-only protocol, app-specific cloud backend machinery, or no safe direct interface.

## Privacy and interference rules

The diagnostic does not use Windows accessibility, move the mouse, type, focus ChatGPT, launch a hidden ChatGPT browser, invoke activation protocols, read cookies/tokens, read Local Storage/IndexedDB/session storage, inspect process memory, capture network payloads, or copy the desktop app's authentication material.

## Goal

After this diagnostic runs on the real PC, the result should be specific enough to stop guessing. If ChatGPT exposes a real local/AppService/IPC path, the next build can protocol-test it. If the evidence shows only a cloud-backed private session path, FlipAi can stop pursuing an unsafe credential-copying design and choose a supported alternative instead.

This release remains diagnostic-only and does not enable ChatGPT SMS routing until an actual usable request path is proven. Keep ChatGPT desktop open and signed in, run the diagnostic once, and copy the full result including the final **Direct-backend assessment**. The result is intentionally verbose so one real-PC run can distinguish all of the supported-looking local paths at once.

## Verified

The normal FlipAi CI covers Linux and Windows tests, `go vet`, race tests, Windows x64 build, desktop/background lifecycle checks, Google Voice receiver validation, installer build, install/uninstall smoke test, Microsoft Defender scan when available, and SHA-256 generation.
