package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// chatGPTProtocolMapScan is static application-package evidence only. It maps
// Electron bridge exposure, IPC directions, cloud Chat routes and request-shape
// key names from app.asar. It never reads a ChatGPT profile, cookies, tokens,
// browser storage, request bodies, process memory, or credential-manager data.
type chatGPTProtocolMapScan struct {
	BridgeExposures  []string
	BridgeMethods    []string
	IPCBindings      []string
	BackendRoutes    []string
	RequestShapeKeys []string
	AuthFlowMarkers  []string
	TransportSignals []string
	ExternalSignals  []string
	Detail           string
}

var (
	chatGPTExposeRegex             = regexp.MustCompile(`(?i)contextBridge\s*\.\s*exposeInMainWorld\s*\(\s*["'\x60]([^"'\x60\r\n]{1,100})`)
	chatGPTIPCRendererRegex        = regexp.MustCompile(`(?i)ipcRenderer\s*\.\s*(invoke|send|sendSync|on|once)\s*\(\s*["'\x60]([^"'\x60\r\n]{1,160})`)
	chatGPTIPCMainRegex            = regexp.MustCompile(`(?i)ipcMain\s*\.\s*(handle|handleOnce|on|once)\s*\(\s*["'\x60]([^"'\x60\r\n]{1,160})`)
	chatGPTBridgeMethodInvokeRegex = regexp.MustCompile(`(?i)([A-Za-z_$][A-Za-z0-9_$]{1,80})\s*:\s*(?:async\s*)?(?:\([^)]{0,300}\)|[A-Za-z_$][A-Za-z0-9_$]*)\s*=>\s*[A-Za-z_$][A-Za-z0-9_$]*\s*\.\s*(?:invoke|send)\s*\(\s*["'\x60]([^"'\x60\r\n]{1,160})`)
	chatGPTRouteRegex              = regexp.MustCompile(`(?i)(?:https://chatgpt\.com|https://chat\.openai\.com|wss://(?:ws\.)?chatgpt\.com)?/(?:backend-api|conversation)(?:/[A-Za-z0-9._~!$&'()*+,;=:@%/-]{0,220})?`)
	chatGPTStringKeyRegex          = regexp.MustCompile(`["'\x60]([A-Za-z_][A-Za-z0-9_]{2,80})["'\x60]\s*:`)
	chatGPTObjectKeyRegex          = regexp.MustCompile(`(?:^|[,;{])\s*([A-Za-z_$][A-Za-z0-9_$]{1,80})\s*:`)
)

var chatGPTInterestingRequestKeys = map[string]bool{
	"action": true, "messages": true, "message": true, "author": true, "role": true, "content": true,
	"content_type": true, "parts": true, "model": true, "conversation_id": true, "conversationId": true,
	"parent_message_id": true, "parentMessageId": true, "message_id": true, "messageId": true,
	"timezone_offset_min": true, "history_and_training_disabled": true, "websocket_request_id": true,
	"websocketRequestId": true, "conversation_mode": true, "conversationMode": true, "arkose_token": true,
	"force_paragen": true, "force_rate_limit": true, "suggestions": true, "supported_encodings": true,
	"system_hints": true, "metadata": true, "recipient": true, "status": true, "end_turn": true,
}

func sanitizeChatGPTRoute(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, "?#"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimSpace(v)
	if len(v) > 260 {
		v = v[:260]
	}
	return v
}

func safeChatGPTStaticName(v string) bool {
	low := strings.ToLower(strings.TrimSpace(v))
	if low == "" || len(low) > 180 {
		return false
	}
	for _, forbidden := range []string{"token", "cookie", "authorization", "secret", "password", "sessionid", "access_key", "api_key"} {
		if strings.Contains(low, forbidden) {
			return false
		}
	}
	return true
}

func chatGPTCodeWindow(text string, center, radius int) string {
	if center < 0 {
		center = 0
	}
	start := center - radius
	if start < 0 {
		start = 0
	}
	end := center + radius
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

func extractChatGPTProtocolMap(path string, data []byte) chatGPTProtocolMapScan {
	var out chatGPTProtocolMapScan
	text := string(data)

	for _, m := range chatGPTExposeRegex.FindAllStringSubmatch(text, 30) {
		if len(m) >= 2 && safeChatGPTStaticName(m[1]) {
			out.BridgeExposures = append(out.BridgeExposures, path+" -> contextBridge exposes "+m[1])
		}
	}
	for _, m := range chatGPTIPCRendererRegex.FindAllStringSubmatch(text, 120) {
		if len(m) >= 3 && safeChatGPTStaticName(m[2]) {
			out.IPCBindings = append(out.IPCBindings, fmt.Sprintf("%s -> renderer.%s -> %s", path, strings.ToLower(m[1]), m[2]))
		}
	}
	for _, m := range chatGPTIPCMainRegex.FindAllStringSubmatch(text, 120) {
		if len(m) >= 3 && safeChatGPTStaticName(m[2]) {
			out.IPCBindings = append(out.IPCBindings, fmt.Sprintf("%s -> main.%s -> %s", path, strings.ToLower(m[1]), m[2]))
		}
	}
	for _, m := range chatGPTBridgeMethodInvokeRegex.FindAllStringSubmatch(text, 120) {
		if len(m) >= 3 && safeChatGPTStaticName(m[1]) && safeChatGPTStaticName(m[2]) {
			out.BridgeMethods = append(out.BridgeMethods, fmt.Sprintf("%s -> method %s -> IPC %s", path, m[1], m[2]))
		}
	}

	// Minified preload bundles do not always keep "ipcRenderer" next to the
	// exposed object. Around electronBridge we therefore collect property names
	// only; no values or source snippets are returned.
	for from := 0; ; {
		i := strings.Index(text[from:], "electronBridge")
		if i < 0 {
			break
		}
		i += from
		window := chatGPTCodeWindow(text, i, 9000)
		for _, m := range chatGPTObjectKeyRegex.FindAllStringSubmatch(window, 240) {
			if len(m) < 2 || !safeChatGPTStaticName(m[1]) {
				continue
			}
			low := strings.ToLower(m[1])
			if low == "electronbridge" || low == "default" || low == "exports" || low == "prototype" || low == "constructor" {
				continue
			}
			out.BridgeMethods = append(out.BridgeMethods, path+" -> electronBridge property "+m[1])
		}
		from = i + len("electronBridge")
		if from >= len(text) {
			break
		}
	}

	for _, m := range chatGPTRouteRegex.FindAllString(text, 120) {
		if route := sanitizeChatGPTRoute(m); route != "" {
			out.BackendRoutes = append(out.BackendRoutes, path+" -> "+route)
		}
	}

	// Collect schema key names only from code surrounding Chat backend routes.
	// This is enough to reconstruct request shape without exposing request data.
	for _, loc := range chatGPTRouteRegex.FindAllStringIndex(text, 80) {
		window := chatGPTCodeWindow(text, loc[0], 12000)
		for _, m := range chatGPTStringKeyRegex.FindAllStringSubmatch(window, 500) {
			if len(m) >= 2 && chatGPTInterestingRequestKeys[m[1]] {
				out.RequestShapeKeys = append(out.RequestShapeKeys, path+" -> "+m[1])
			}
		}
	}

	low := strings.ToLower(text)
	authMarkers := []struct{ needle, label string }{
		{"oauth/authorize", "OAuth authorize endpoint"},
		{"auth.openai.com", "auth.openai.com"},
		{"code_challenge", "PKCE code_challenge"},
		{"code_verifier", "PKCE code_verifier"},
		{"redirect_uri", "OAuth redirect_uri"},
		{"client_id", "OAuth client_id field"},
		{"device_authorization", "OAuth device authorization"},
		{"grant_type", "OAuth grant_type field"},
	}
	for _, a := range authMarkers {
		if strings.Contains(low, a.needle) {
			out.AuthFlowMarkers = append(out.AuthFlowMarkers, path+" -> "+a.label)
		}
	}

	transportMarkers := []struct{ needle, label string }{
		{"websocket", "WebSocket"}, {"eventsource", "EventSource/SSE"}, {"text/event-stream", "SSE content type"},
		{"fetch(", "fetch"}, {"net.fetch", "electron.net.fetch"}, {"webcontents.send", "webContents.send"},
		{"executejavascript", "webContents.executeJavaScript"}, {"messagechannelmain", "MessageChannelMain"},
		{"messageportmain", "MessagePortMain"}, {"utilityprocess", "utilityProcess"}, {"broadcastchannel", "BroadcastChannel"},
	}
	for _, t := range transportMarkers {
		if strings.Contains(low, t.needle) {
			out.TransportSignals = append(out.TransportSignals, path+" -> "+t.label)
		}
	}

	externalMarkers := []struct{ needle, label string }{
		{"net.createserver", "Node net.createServer"}, {"createserver(", "server creation call"},
		{"websocketserver", "WebSocketServer"}, {"namedpipe", "named-pipe code"}, {"\\\\.\\pipe\\", "Windows pipe path"},
		{"process.stdin", "process.stdin"}, {"process.stdout", "process.stdout"}, {"child_process", "child_process"},
		{"protocol.handle", "Electron protocol.handle"}, {"protocol.register", "Electron protocol.register"},
	}
	for _, e := range externalMarkers {
		if strings.Contains(low, e.needle) {
			out.ExternalSignals = append(out.ExternalSignals, path+" -> "+e.label)
		}
	}
	return out
}

func mergeChatGPTProtocolMap(dst *chatGPTProtocolMapScan, src chatGPTProtocolMapScan) {
	dst.BridgeExposures = append(dst.BridgeExposures, src.BridgeExposures...)
	dst.BridgeMethods = append(dst.BridgeMethods, src.BridgeMethods...)
	dst.IPCBindings = append(dst.IPCBindings, src.IPCBindings...)
	dst.BackendRoutes = append(dst.BackendRoutes, src.BackendRoutes...)
	dst.RequestShapeKeys = append(dst.RequestShapeKeys, src.RequestShapeKeys...)
	dst.AuthFlowMarkers = append(dst.AuthFlowMarkers, src.AuthFlowMarkers...)
	dst.TransportSignals = append(dst.TransportSignals, src.TransportSignals...)
	dst.ExternalSignals = append(dst.ExternalSignals, src.ExternalSignals...)
}

func scanOneChatGPTProtocolMap(ctx context.Context, archivePath string) (chatGPTProtocolMapScan, error) {
	var out chatGPTProtocolMapScan
	_, entries, err := readChatGPTASARIndex(archivePath)
	if err != nil {
		return out, err
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return out, err
	}
	defer f.Close()

	var total int64
	inspected := 0
	const totalLimit = int64(256 << 20)
	const fileLimit = int64(32 << 20)
	for _, entry := range entries {
		if ctx.Err() != nil || total >= totalLimit {
			break
		}
		if entry.Unpacked || entry.Size <= 0 || entry.Size > fileLimit || !chatGPTASARCodePath(entry.Path) {
			continue
		}
		lowPath := strings.ToLower(entry.Path)
		// Prioritize actual preload/main/renderer/worker/webview code. Locale JSON
		// and unrelated application metadata add noise to bridge mapping.
		if !(strings.Contains(lowPath, ".vite/build/") || strings.Contains(lowPath, "preload") || strings.Contains(lowPath, "main") || strings.Contains(lowPath, "worker") || strings.Contains(lowPath, "webview/") || strings.Contains(lowPath, "renderer")) {
			continue
		}
		limit := entry.Size
		if remain := totalLimit - total; remain < limit {
			limit = remain
		}
		b := make([]byte, int(limit))
		n, readErr := f.ReadAt(b, entry.Offset)
		if readErr != nil && readErr != io.EOF {
			continue
		}
		b = b[:n]
		total += int64(n)
		inspected++
		mergeChatGPTProtocolMap(&out, extractChatGPTProtocolMap(entry.Path, b))
	}
	out.BridgeExposures = uniqueSortedStrings(out.BridgeExposures)
	out.BridgeMethods = uniqueSortedStrings(out.BridgeMethods)
	out.IPCBindings = uniqueSortedStrings(out.IPCBindings)
	out.BackendRoutes = uniqueSortedStrings(out.BackendRoutes)
	out.RequestShapeKeys = uniqueSortedStrings(out.RequestShapeKeys)
	out.AuthFlowMarkers = uniqueSortedStrings(out.AuthFlowMarkers)
	out.TransportSignals = uniqueSortedStrings(out.TransportSignals)
	out.ExternalSignals = uniqueSortedStrings(out.ExternalSignals)
	out.Detail = fmt.Sprintf("protocol mapper inspected %d focused Electron app-code files (%d bytes); exposures=%d bridge methods/properties=%d IPC bindings=%d backend routes=%d request keys=%d auth markers=%d external-transport signals=%d", inspected, total, len(out.BridgeExposures), len(out.BridgeMethods), len(out.IPCBindings), len(out.BackendRoutes), len(out.RequestShapeKeys), len(out.AuthFlowMarkers), len(out.ExternalSignals))
	return out, nil
}

func scanChatGPTProtocolMaps(ctx context.Context, roots []string) chatGPTProtocolMapScan {
	var out chatGPTProtocolMapScan
	seen := map[string]bool{}
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "" || root == "." || ctx.Err() != nil {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return nil
			}
			low := strings.ToLower(filepath.Clean(path))
			for _, privatePart := range []string{"\\user data\\", "\\local storage\\", "\\indexeddb\\", "\\session storage\\", "\\network\\", "\\cache\\", "\\gpucache\\"} {
				if strings.Contains(low, privatePart) {
					if d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if d.IsDir() || !strings.EqualFold(d.Name(), "app.asar") {
				return nil
			}
			key := strings.ToLower(filepath.Clean(path))
			if seen[key] {
				return nil
			}
			seen[key] = true
			one, e := scanOneChatGPTProtocolMap(ctx, path)
			if e == nil {
				mergeChatGPTProtocolMap(&out, one)
				if one.Detail != "" {
					if out.Detail != "" {
						out.Detail += " | "
					}
					out.Detail += one.Detail
				}
			}
			return nil
		})
	}
	out.BridgeExposures = uniqueSortedStrings(out.BridgeExposures)
	out.BridgeMethods = uniqueSortedStrings(out.BridgeMethods)
	out.IPCBindings = uniqueSortedStrings(out.IPCBindings)
	out.BackendRoutes = uniqueSortedStrings(out.BackendRoutes)
	out.RequestShapeKeys = uniqueSortedStrings(out.RequestShapeKeys)
	out.AuthFlowMarkers = uniqueSortedStrings(out.AuthFlowMarkers)
	out.TransportSignals = uniqueSortedStrings(out.TransportSignals)
	out.ExternalSignals = uniqueSortedStrings(out.ExternalSignals)
	sort.Strings(out.BridgeMethods)
	return out
}
