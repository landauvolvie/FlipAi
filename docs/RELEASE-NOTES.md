# FlipAi v0.46.9

This release follows the v0.46.8 real-PC result: ChatGPT is an Electron/Chromium desktop client, but it exposes no ChatGPT-owned localhost API, DevTools endpoint, named pipe, Windows AppService, or message-send activation protocol. The only registered protocol is `codex://`, and the earlier backend-looking strings were not attributable to regular ChatGPT app code.

## Electron app.asar deep scan

- Parses Electron `app.asar` directly instead of treating it as an opaque binary blob.
- Reads the ASAR file index and attributes findings to exact application-bundle paths.
- Scans regular app JavaScript/JSON/HTML while excluding `node_modules`, CUA, Playwright, PDF.js, Codex app tools, browser/chrome plugins, locales and asset noise.
- Reports app-code entries, strong Chat/backend/conversation markers and Electron IPC/bridge channel candidates such as `ipcMain.handle`, `ipcRenderer.invoke/send`, and `contextBridge.exposeInMainWorld` names.
- Recalculates the Direct-backend assessment after the ASAR scan so app-bundle evidence outranks generic runtime strings.
- Keeps the diagnostic read-only: no accessibility, mouse/keyboard automation, hidden ChatGPT browser, DevTools enabling, protocol invocation, cookies/tokens, profile storage, process memory or network-payload capture.

## Why this is the next step

The real v0.46.8 result proved there is no externally advertised local ChatGPT transport to call. The next remaining non-invasive source of truth is the Electron application bundle itself. If regular ChatGPT has an internal main/preload/renderer bridge, v0.46.9 should expose the exact channel and file names. If the ASAR contains only direct cloud-request code and no callable bridge, we will know that a clean local background integration is not exposed by the desktop app.

Keep ChatGPT desktop open and signed in, install v0.46.9, go to Agents -> ChatGPT Chat, run **Run app bundle deep scan**, and copy the entire result, especially the ASAR sections and Direct-backend assessment.
