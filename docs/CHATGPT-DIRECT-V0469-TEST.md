# v0.46.9 real-PC ChatGPT direct test

1. Install FlipAi v0.46.9 after it is published.
2. Keep the regular ChatGPT desktop app open and signed in.
3. Open **Agents -> ChatGPT Chat**.
4. Click **Run app bundle deep scan** once.
5. Copy the entire result.

The highest-value sections are:

- **Electron ASAR archives opened**
- **ASAR app-code entries indexed**
- **ASAR app-code protocol marker sources**
- **ASAR IPC/bridge candidates**
- **ASAR scan note**
- **Direct-backend assessment**

Interpretation:

- If **ASAR IPC/bridge candidates** contains regular ChatGPT-owned channel names, the next build should trace those exact channels between main/preload/renderer code and attempt only a harmless non-message invocation first.
- If app-specific ASAR files contain `/backend-api`, conversation, messages, responses, websocket, or ChatGPT/OpenAI endpoints but there is no IPC bridge, the next build should trace those exact call sites to determine whether the renderer itself owns the cloud request path.
- If the real app bundle has neither usable IPC nor app-specific backend call sites, stop treating the desktop client as if it exposes a clean local message API.

This diagnostic never reads ChatGPT cookies, tokens, Local Storage, IndexedDB, session storage, process memory, request bodies, or full process command lines, and it never uses accessibility, mouse/keyboard automation, hidden browsers, or DevTools enabling.
