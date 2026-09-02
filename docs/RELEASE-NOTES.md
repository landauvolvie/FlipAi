# FlipAi v0.46.10

This release follows the v0.46.9 ASAR result, which found the real ChatGPT Electron bundle, `electronBridge`, regular ChatGPT backend routes, conversation/WebSocket markers, and internal Electron IPC names while still proving that no ChatGPT-owned local listener or DevTools transport is exposed.

## Full Chat request protocol map

- Maps `contextBridge.exposeInMainWorld` exposure names, including `electronBridge`.
- Maps `ipcRenderer` calls and `ipcMain` handlers by direction and channel name.
- Maps bridge method/property names near the exposed Electron bridge without returning their values.
- Extracts regular ChatGPT `/backend-api` and `/conversation` route paths with query strings removed.
- Extracts conversation request-shape **key names only** around those routes, such as message/conversation/model/parent identifiers when present.
- Detects OAuth/PKCE flow markers (`auth.openai.com`, authorize, client-id field, redirect field, code challenge/verifier) without reading or returning credentials.
- Identifies streaming/runtime mechanisms such as WebSocket, SSE, fetch, Electron net, MessagePort/MessageChannel, utility processes and webContents messaging.
- Looks for external-callability primitives such as Node servers, WebSocketServer, Windows pipe paths, stdin/stdout and Electron protocol handlers, attributed to exact app-bundle files.
- Updates the Direct-backend assessment so internal Electron IPC is never mistaken for an API FlipAi can call from another process.

## Privacy boundary

The mapper remains static package inspection only. It does not read ChatGPT cookies, tokens, Local Storage, IndexedDB, session storage, request bodies, network payloads, Windows credential values, process memory, or full process command lines. It does not enable DevTools, use accessibility, move the mouse, type into ChatGPT, or launch a hidden ChatGPT browser.

## What to do

Keep ChatGPT desktop open and signed in. In FlipAi go to **Agents -> ChatGPT Chat -> Map Chat request protocol** and copy the complete result. The decisive lines are **BRIDGE EXPOSURE**, **BRIDGE METHOD**, **IPC BINDING**, **BACKEND ROUTE**, **REQUEST KEY**, **AUTH FLOW**, **EXTERNAL TRANSPORT SIGNAL**, the ASAR scan note, and **Direct-backend assessment**.

ChatGPT Chat intentionally remains **Not connected** in this release; the mapper must first prove a safe externally callable path or an independent authentication flow before SMS routing can be enabled.
