package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Electron packages most of the desktop application's JavaScript in app.asar.
// Reading the archive index gives FlipAi file-level attribution instead of
// treating the whole archive as one opaque binary blob. This remains static
// package inspection only: no profile/user-data folder is opened.
type chatGPTASAREntry struct {
	Path     string
	Offset   int64
	Size     int64
	Unpacked bool
}

type chatGPTASARNode struct {
	Files    map[string]chatGPTASARNode `json:"files,omitempty"`
	Offset   string                     `json:"offset,omitempty"`
	Size     int64                      `json:"size,omitempty"`
	Unpacked bool                       `json:"unpacked,omitempty"`
}

type chatGPTASARHeader struct {
	Files map[string]chatGPTASARNode `json:"files"`
}

type chatGPTASARScan struct {
	Archives      []string
	CodeEntries   []string
	MarkerSources []string
	IPCCandidates []string
	Detail        string
}

var chatGPTIPCRegexes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:ipcRenderer|ipcMain)\s*\.\s*(?:invoke|send|sendSync|on|once|handle|handleOnce)\s*\(\s*["'\x60]([^"'\x60\r\n]{2,140})`),
	regexp.MustCompile(`(?i)contextBridge\s*\.\s*exposeInMainWorld\s*\(\s*["'\x60]([^"'\x60\r\n]{2,140})`),
	regexp.MustCompile(`(?i)(?:register|handle)\s*\(\s*["'\x60]((?:chatgpt|openai|codex)[^"'\x60\r\n]{0,120})`),
}

func readChatGPTASARIndex(path string) (int64, []chatGPTASAREntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()

	first := make([]byte, 8)
	if _, err := io.ReadFull(f, first); err != nil {
		return 0, nil, fmt.Errorf("read ASAR size pickle: %w", err)
	}
	// Chromium Pickle stores a 4-byte payload length before the uint32 value.
	// Electron reads exactly eight bytes for this first Pickle.
	if binary.LittleEndian.Uint32(first[:4]) < 4 {
		return 0, nil, fmt.Errorf("invalid ASAR size pickle")
	}
	headerPickleSize := binary.LittleEndian.Uint32(first[4:8])
	if headerPickleSize < 8 || headerPickleSize > 64<<20 {
		return 0, nil, fmt.Errorf("invalid ASAR header size %d", headerPickleSize)
	}

	second := make([]byte, int(headerPickleSize))
	if _, err := io.ReadFull(f, second); err != nil {
		return 0, nil, fmt.Errorf("read ASAR header pickle: %w", err)
	}
	if len(second) < 8 {
		return 0, nil, fmt.Errorf("short ASAR header pickle")
	}
	payloadSize := binary.LittleEndian.Uint32(second[:4])
	stringSize := binary.LittleEndian.Uint32(second[4:8])
	if payloadSize+4 > uint32(len(second)) || stringSize > uint32(len(second)-8) {
		return 0, nil, fmt.Errorf("invalid ASAR header pickle lengths")
	}

	var header chatGPTASARHeader
	if err := json.Unmarshal(second[8:8+stringSize], &header); err != nil {
		return 0, nil, fmt.Errorf("decode ASAR header JSON: %w", err)
	}
	if header.Files == nil {
		return 0, nil, fmt.Errorf("ASAR header has no files table")
	}

	dataBase := int64(8) + int64(headerPickleSize)
	entries := make([]chatGPTASAREntry, 0, 512)
	var walk func(prefix string, files map[string]chatGPTASARNode)
	walk = func(prefix string, files map[string]chatGPTASARNode) {
		for name, node := range files {
			p := name
			if prefix != "" {
				p = prefix + "/" + name
			}
			if node.Files != nil {
				walk(p, node.Files)
				continue
			}
			if node.Size < 0 {
				continue
			}
			off, err := strconv.ParseInt(strings.TrimSpace(node.Offset), 10, 64)
			if err != nil && !node.Unpacked {
				continue
			}
			entries = append(entries, chatGPTASAREntry{Path: filepath.ToSlash(p), Offset: dataBase + off, Size: node.Size, Unpacked: node.Unpacked})
		}
	}
	walk("", header.Files)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return dataBase, entries, nil
}

func chatGPTASARCodePath(path string) bool {
	low := strings.ToLower(filepath.ToSlash(path))
	if low == "" || strings.Contains(low, "/node_modules/") || strings.HasPrefix(low, "node_modules/") {
		return false
	}
	for _, noisy := range []string{"/cua_node/", "/pdfjs", "/playwright", "/locales/", "/assets/", "/resources/default_app/", "/plugins/codex-app-tools/", "/plugins/browser/", "/plugins/chrome/"} {
		if strings.Contains(low, noisy) {
			return false
		}
	}
	ext := strings.ToLower(filepath.Ext(low))
	switch ext {
	case ".js", ".mjs", ".cjs", ".json", ".html":
		return true
	default:
		return false
	}
}

func chatGPTASARStrongMarker(m string) bool {
	low := strings.ToLower(m)
	for _, s := range []string{"backend-api", "chatgpt.com", "openai.com", "conversation", "responses", "messages", "ipcrenderer", "ipcmain", "websocket", "chatgpt://", "openai://", "codex://"} {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

func extractChatGPTIPCCandidates(data []byte) []string {
	text := string(data)
	seen := map[string]bool{}
	out := []string{}
	for _, re := range chatGPTIPCRegexes {
		for _, match := range re.FindAllStringSubmatch(text, 80) {
			if len(match) < 2 {
				continue
			}
			v := strings.TrimSpace(match[1])
			low := strings.ToLower(v)
			if v == "" || len(v) > 140 || strings.Contains(low, "token") || strings.Contains(low, "cookie") || strings.Contains(low, "authorization") || strings.Contains(low, "secret") {
				continue
			}
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	sort.Strings(out)
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func scanOneChatGPTASAR(ctx context.Context, archivePath, display string) (chatGPTASARScan, error) {
	var result chatGPTASARScan
	_, entries, err := readChatGPTASARIndex(archivePath)
	if err != nil {
		return result, err
	}
	result.Archives = []string{display}

	f, err := os.Open(archivePath)
	if err != nil {
		return result, err
	}
	defer f.Close()

	var total int64
	const totalLimit = int64(320 << 20)
	const fileLimit = int64(32 << 20)
	codeCount := 0
	markerCount := 0
	ipcSeen := map[string]bool{}

	for _, entry := range entries {
		if ctx.Err() != nil || total >= totalLimit {
			break
		}
		if entry.Unpacked || entry.Size <= 0 || entry.Size > fileLimit || !chatGPTASARCodePath(entry.Path) {
			continue
		}
		codeCount++
		if len(result.CodeEntries) < 80 {
			result.CodeEntries = append(result.CodeEntries, entry.Path)
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

		markers := []string{}
		for _, m := range extractChatGPTProtocolMarkers(b) {
			if chatGPTASARStrongMarker(m) {
				markers = append(markers, m)
			}
			if len(markers) >= 10 {
				break
			}
		}
		if len(markers) > 0 && len(result.MarkerSources) < 50 {
			markerCount++
			line := entry.Path + " -> " + strings.Join(markers, ", ")
			if len(line) > 650 {
				line = line[:650]
			}
			result.MarkerSources = append(result.MarkerSources, line)
		}

		for _, ch := range extractChatGPTIPCCandidates(b) {
			if !ipcSeen[ch] && len(result.IPCCandidates) < 80 {
				ipcSeen[ch] = true
				result.IPCCandidates = append(result.IPCCandidates, entry.Path+" -> "+ch)
			}
		}
	}

	sort.Strings(result.CodeEntries)
	sort.Strings(result.MarkerSources)
	sort.Strings(result.IPCCandidates)
	result.Detail = fmt.Sprintf("ASAR index contained %d files; inspected %d app-code entries (%d bytes) and found %d files with strong Chat/backend markers", len(entries), codeCount, total, markerCount)
	return result, nil
}

func scanChatGPTASARArchives(ctx context.Context, roots []string) chatGPTASARScan {
	var out chatGPTASARScan
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
			display := filepath.ToSlash(path)
			if rel, e := filepath.Rel(root, path); e == nil && rel != "." {
				display = filepath.ToSlash(rel)
			}
			one, e := scanOneChatGPTASAR(ctx, path, display)
			if e != nil {
				if out.Detail == "" {
					out.Detail = "ASAR parse failed for " + display + ": " + e.Error()
				}
				return nil
			}
			out.Archives = append(out.Archives, one.Archives...)
			out.CodeEntries = append(out.CodeEntries, one.CodeEntries...)
			out.MarkerSources = append(out.MarkerSources, one.MarkerSources...)
			out.IPCCandidates = append(out.IPCCandidates, one.IPCCandidates...)
			if one.Detail != "" {
				if out.Detail != "" {
					out.Detail += " | "
				}
				out.Detail += one.Detail
			}
			return nil
		})
	}
	out.Archives = uniqueSortedStrings(out.Archives)
	out.CodeEntries = uniqueSortedStrings(out.CodeEntries)
	out.MarkerSources = uniqueSortedStrings(out.MarkerSources)
	out.IPCCandidates = uniqueSortedStrings(out.IPCCandidates)
	return out
}
