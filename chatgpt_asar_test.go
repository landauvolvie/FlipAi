package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type asarFixtureNode struct {
	Files  map[string]asarFixtureNode `json:"files,omitempty"`
	Offset string                     `json:"offset,omitempty"`
	Size   int                        `json:"size,omitempty"`
}

func buildASARFixture(t *testing.T, files map[string]string) []byte {
	t.Helper()
	root := map[string]asarFixtureNode{}
	body := []byte{}
	offset := 0
	for path, content := range files {
		parts := strings.Split(filepath.ToSlash(path), "/")
		var add func(map[string]asarFixtureNode, []string)
		add = func(m map[string]asarFixtureNode, rest []string) {
			name := rest[0]
			if len(rest) == 1 {
				m[name] = asarFixtureNode{Offset: stringInt(offset), Size: len(content)}
				return
			}
			n := m[name]
			if n.Files == nil {
				n.Files = map[string]asarFixtureNode{}
			}
			add(n.Files, rest[1:])
			m[name] = n
		}
		add(root, parts)
		body = append(body, []byte(content)...)
		offset += len(content)
	}

	headerJSON, err := json.Marshal(map[string]any{"files": root})
	if err != nil {
		t.Fatal(err)
	}
	padded := (len(headerJSON) + 3) &^ 3
	payloadSize := 4 + padded
	second := make([]byte, 4+payloadSize)
	binary.LittleEndian.PutUint32(second[0:4], uint32(payloadSize))
	binary.LittleEndian.PutUint32(second[4:8], uint32(len(headerJSON)))
	copy(second[8:], headerJSON)
	first := make([]byte, 8)
	binary.LittleEndian.PutUint32(first[0:4], 4)
	binary.LittleEndian.PutUint32(first[4:8], uint32(len(second)))
	return append(append(first, second...), body...)
}

func stringInt(v int) string {
	if v == 0 {
		return "0"
	}
	buf := [24]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func TestReadChatGPTASARIndex(t *testing.T) {
	fixture := buildASARFixture(t, map[string]string{
		"dist/main.js":    `ipcMain.handle("chat-send", async () => fetch("https://chatgpt.com/backend-api/conversation"))`,
		"dist/preload.js": `contextBridge.exposeInMainWorld("chatgptBridge", {})`,
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "app.asar")
	if err := os.WriteFile(path, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	_, entries, err := readChatGPTASARIndex(path)
	if err != nil {
		t.Fatalf("readChatGPTASARIndex: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%d want 2", len(entries))
	}
}

func TestScanOneChatGPTASARFindsAppMarkersAndIPC(t *testing.T) {
	fixture := buildASARFixture(t, map[string]string{
		"dist/main.js": `const {ipcMain}=require("electron"); ipcMain.handle("chat-send", async () => fetch("https://chatgpt.com/backend-api/conversation"));`,
		"dist/preload.js": `contextBridge.exposeInMainWorld("chatgptBridge", {});`,
		"node_modules/noise/index.js": `https://chatgpt.com/backend-api should be ignored`,
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "app.asar")
	if err := os.WriteFile(path, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	scan, err := scanOneChatGPTASAR(context.Background(), path, "app.asar")
	if err != nil {
		t.Fatalf("scanOneChatGPTASAR: %v", err)
	}
	joined := strings.Join(scan.MarkerSources, "\n")
	if !strings.Contains(joined, "dist/main.js") || !strings.Contains(joined, "backend-api") {
		t.Fatalf("expected app marker attribution, got %q", joined)
	}
	if strings.Contains(joined, "node_modules") {
		t.Fatalf("node_modules noise should be excluded: %q", joined)
	}
	ipc := strings.Join(scan.IPCCandidates, "\n")
	if !strings.Contains(ipc, "chat-send") || !strings.Contains(ipc, "chatgptBridge") {
		t.Fatalf("expected IPC candidates, got %q", ipc)
	}
}
