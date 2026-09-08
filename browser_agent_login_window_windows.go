//go:build windows

package main

import (
	"os"
	"syscall"
	"time"
	"unsafe"
)

var procBrowserAgentBringWindowToTop = trayUser32.NewProc("BringWindowToTop")

type browserAgentLoginWindowWatch struct {
	title  string
	active func() bool
}

func findBrowserAgentLoginWindow(title string) uintptr {
	class, err := syscall.UTF16PtrFromString(flipAiWindowClass)
	if err != nil {
		return 0
	}
	caption, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	h, _, _ := procPlatformFindWindow.Call(uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(caption)))
	return h
}

func raiseBrowserAgentLoginWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	// A child WebView process can be created while the main FlipAi window still
	// owns the foreground. SetFocus only affects the child's thread and was not
	// enough to make the sign-in window appear to the user. Restore the native
	// window and explicitly raise/foreground it once when it appears.
	procPlatformShowWindow.Call(hwnd, 9) // SW_RESTORE
	procBrowserAgentBringWindowToTop.Call(hwnd)
	procPlatformSetForeground.Call(hwnd)
}

func browserAgentLoginWatches(dataDir string) []browserAgentLoginWindowWatch {
	return []browserAgentLoginWindowWatch{
		{title: "Connect ChatGPT to FlipAi", active: func() bool { return loadChatGPTRuntime(dataDir).LoginActive }},
		{title: "Connect Claude to FlipAi", active: func() bool { return loadClaudeChatRuntime(dataDir).LoginActive }},
		{title: "Connect Gemini Chat to FlipAi", active: func() bool { return loadGeminiChatRuntime(dataDir).LoginActive }},
		{title: "Connect Grok Chat to FlipAi", active: func() bool { return loadGrokChatRuntime(dataDir).LoginActive }},
		{title: "Connect Microsoft Copilot Chat to FlipAi", active: func() bool { return loadCopilotChatRuntime(dataDir).LoginActive }},
	}
}

func runBrowserAgentLoginWindowPromoter(dataDir string) {
	watches := browserAgentLoginWatches(dataDir)
	seen := make(map[string]uintptr, len(watches))
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		for _, watch := range watches {
			if !watch.active() {
				delete(seen, watch.title)
				continue
			}
			hwnd := findBrowserAgentLoginWindow(watch.title)
			if hwnd == 0 || seen[watch.title] == hwnd {
				continue
			}
			raiseBrowserAgentLoginWindow(hwnd)
			seen[watch.title] = hwnd
		}
	}
}

func init() {
	if len(os.Args) < 2 || os.Args[1] != "--tray" {
		return
	}
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return
	}
	// The tray process owns this goroutine. It exits with that process, so no
	// separate shutdown signal is needed.
	go runBrowserAgentLoginWindowPromoter(dataDir)
}
