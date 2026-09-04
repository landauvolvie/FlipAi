//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows/registry"
)

var (
	platformShell32       = syscall.NewLazyDLL("shell32.dll")
	procPlatformShellOpen = platformShell32.NewProc("ShellExecuteW")

	// The window icon is applied at runtime rather than through
	// WindowOptions.IconId, which reads an icon compiled into the executable's
	// resources. FlipAi is built with plain `go build` and embeds none, which is
	// why the app window and its taskbar button showed the generic Windows
	// placeholder. This reuses the same FlipAi.ico the installer places beside
	// the executable and the tray already loads.
	procPlatformSendMessage = trayUser32.NewProc("SendMessageW")

	procPlatformFindWindow    = trayUser32.NewProc("FindWindowW")
	procPlatformShowWindow    = trayUser32.NewProc("ShowWindow")
	procPlatformSetForeground = trayUser32.NewProc("SetForegroundWindow")
)

const flipAiWindowTitle = "FlipAi"

// flipAiWindowClass is the window class the WebView2 binding registers. The
// search has to be class-scoped: the tray process owns a hidden helper window
// with the very same title, and matching that one meant the desktop window was
// never created at all -- while restoring it put an empty frame on screen.
const flipAiWindowClass = "webview"

// flipAiWindowHWND finds an already-open FlipAi window, whichever process owns
// it. Opening from the tray should raise the window the user already has rather
// than stack another copy on top of it.
//
// It answers 0 for anything it is not certain about. Opening a second window is
// a small annoyance; failing to open one at all leaves the user with no way
// into the app.
func flipAiWindowHWND() uintptr {
	class, err := syscall.UTF16PtrFromString(flipAiWindowClass)
	if err != nil {
		return 0
	}
	title, err := syscall.UTF16PtrFromString(flipAiWindowTitle)
	if err != nil {
		return 0
	}
	h, _, _ := procPlatformFindWindow.Call(uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)))
	if h == 0 {
		return 0
	}
	// A window nobody can see is not the one the user is asking for.
	if visible, _, _ := procVoiceIsWindowVisible.Call(h); visible == 0 {
		return 0
	}
	return h
}

const (
	wmSetIcon      = 0x0080
	iconSmallParam = 0
	iconBigParam   = 1
)

// applyFlipAiWindowIcon gives a window FlipAi's own icon. A failure is not worth
// failing the window over: the app still opens, it just keeps the placeholder.
func applyFlipAiWindowIcon(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	icon, _ := loadFlipAiTrayIcon(0)
	if icon == 0 {
		return
	}
	// Small drives the title bar, big drives the taskbar button and Alt-Tab.
	procPlatformSendMessage.Call(hwnd, wmSetIcon, iconSmallParam, icon)
	procPlatformSendMessage.Call(hwnd, wmSetIcon, iconBigParam, icon)
}

// openBrowser keeps external links (for example Google OAuth) in the user's
// normal browser, but renders FlipAi's own loopback control UI inside a real
// desktop window. The local HTTP server remains an internal implementation
// detail; the user sees an ordinary FlipAi app window with no address bar.
func openBrowser(target string) error {
	if capture := os.Getenv("FLIPAI_BROWSER_TEST_CAPTURE"); capture != "" {
		return os.WriteFile(capture, []byte(target), 0600)
	}
	if isFlipAiLocalTarget(target) {
		return openFlipAiWindow(target)
	}
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r, _, callErr := procPlatformShellOpen.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0,
		0,
		1, // SW_SHOWNORMAL
	)
	if r <= 32 {
		return fmt.Errorf("open %q with Windows shell failed (ShellExecuteW=%d): %v", target, r, callErr)
	}
	return nil
}

func isFlipAiLocalTarget(target string) bool {
	lower := strings.ToLower(strings.TrimSpace(target))
	return strings.HasPrefix(lower, "http://127.0.0.1:") || strings.HasPrefix(lower, "http://localhost:")
}

// platformFlipAiWindowOpen reports whether the FlipAi window is on screen, so an
// automatic update can restore what the user had rather than always coming back
// as a background process.
func platformFlipAiWindowOpen() bool { return flipAiWindowHWND() != 0 }

func openFlipAiWindow(target string) error {
	// Raise the window that already exists instead of opening a second one.
	if h := flipAiWindowHWND(); h != 0 {
		procPlatformShowWindow.Call(h, 9) // SW_RESTORE
		procPlatformSetForeground.Call(h)
		return nil
	}
	// The Win32 message loop below only receives this window's messages on the
	// thread that created it. Without pinning the goroutine, Go is free to move
	// it to another thread partway through, and the window then never responds
	// or never appears -- which is why opening FlipAi from the tray sometimes
	// did nothing at all.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "FlipAi",
			Width:  1320,
			Height: 860,
			Center: true,
		},
	})
	if w == nil {
		return fmt.Errorf("could not create the FlipAi desktop window; Microsoft Edge WebView2 Runtime may be unavailable")
	}
	defer w.Destroy()
	applyFlipAiWindowIcon(uintptr(w.Window()))
	w.SetSize(1040, 680, webview2.HintMin)
	w.Init(desktopInitScript)

	// This window lives in its own process and its message loop blocks below,
	// so Quit from the tray never reached it: the bridge stopped but the FlipAi
	// window stayed on screen, still holding the data folder open.
	stop := watchQuitAndClose(uintptr(w.Window()))
	defer close(stop)

	w.Navigate(target)
	w.Run()
	handleWindowClosed()
	return nil
}

// watchQuitAndClose closes a window as soon as a quit is requested. The returned
// channel stops the watcher.
func watchQuitAndClose(hwnd uintptr) chan struct{} {
	stop := make(chan struct{})
	dataDir, _, _, _, err := appPaths()
	if err != nil {
		return stop
	}
	go func() {
		t := time.NewTicker(400 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if quitRequested(dataDir) {
					procVoicePostMessage.Call(hwnd, voiceWMClose, 0, 0)
					return
				}
			}
		}
	}()
	return stop
}

// handleWindowClosed honours the Settings "Close to tray" choice. With it on
// (the default) closing the window leaves the bridge running in the tray; with
// it off, closing the window stops FlipAi completely.
func handleWindowClosed() {
	dataDir, cfgPath, _, _, err := appPaths()
	if err != nil {
		return
	}
	cfg, err := loadConfig(cfgPath, dataDir)
	if err != nil {
		return
	}
	if !cfg.UI.CloseToTray {
		requestQuit(dataDir, "desktop window closed with close-to-tray off")
	}
}

// runUpdateInstaller launches the downloaded Setup EXE in silent mode. The
// installer stops the running bridge, replaces the files in place, and starts
// FlipAi again; no setup questions are asked on an update.
//
// reopenWindow says how FlipAi should come back. An update the user pressed
// Install for should reopen the window they were just looking at; an automatic
// background update should restore the bridge without stealing focus.
func runUpdateInstaller(path string, reopenWindow bool) error {
	// /restartapp is FlipAi's own flag; the installer maps 1 to "launcher, with
	// the window" and 2 to "background bridge only". Without it a silent run
	// leaves nothing running at all, which is correct for scripted deployment
	// and wrong for an in-app update.
	mode := "/restartapp=2"
	if reopenWindow {
		mode = "/restartapp=1"
	}
	cmd := exec.Command(path, "/VERYSILENT", "/NORESTART", "/SUPPRESSMSGBOXES", mode)
	hideWindow(cmd)
	return cmd.Start()
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func spawnDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	hideWindow(cmd)
	return cmd.Start()
}

const flipAiRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func installAutostart(exe string) error {
	// Remove the pre-v0.6.1 value so upgrades cannot start two copies.
	_ = uninstallAutostartNamed("AISMSBridge")
	defer autostartProbe.invalidate()
	return installAutostartNamed("FlipAi", exe)
}

// installAutostartNamed writes the per-user Run value through the supported
// Windows registry API. Previous builds spawned a hidden reg.exe child for the
// same operation. The user-facing behavior is identical, but the native API
// avoids a command-line persistence pattern that endpoint protection rightly
// treats with extra suspicion.
func installAutostartNamed(name, exe string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, flipAiRunKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("enable startup: open Run key: %w", err)
	}
	defer key.Close()
	value := fmt.Sprintf("\"%s\" --watchdog", exe)
	if err := key.SetStringValue(name, value); err != nil {
		return fmt.Errorf("enable startup: write Run value: %w", err)
	}
	return nil
}

func uninstallAutostart() error {
	_ = uninstallAutostartNamed("AISMSBridge")
	defer autostartProbe.invalidate()
	return uninstallAutostartNamed("FlipAi")
}

func uninstallAutostartNamed(name string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, flipAiRunKey, registry.SET_VALUE)
	if err != nil {
		// A missing Run key/value already represents the requested state.
		return nil
	}
	defer key.Close()
	_ = key.DeleteValue(name)
	return nil
}

// autostartEnabled reports whether this Windows user's Run key still holds the
// FlipAi entry, so Settings shows the real state instead of a remembered one.
// The registry read is cached: the status snapshot behind it is rebuilt on
// every page render and every status poll.
var autostartProbe = newCachedBool(20*time.Second, func() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, flipAiRunKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, _, err := key.GetStringValue("FlipAi")
	if err != nil {
		return false
	}
	lower := strings.ToLower(value)
	return strings.Contains(lower, "flipai.exe") && strings.Contains(lower, "--watchdog")
})

func autostartEnabled() bool { return autostartProbe.get() }

// startClaudeSignIn opens a console window running the Claude Code sign-in on
// the user's desktop.
//
// ShellExecuteW rather than a plain exec: FlipAi is a GUI process with no
// console of its own, so a child started normally would have nowhere to draw
// the interactive flow. The shell gives it a real window the user can see and
// type into, which is the whole point — the credential Claude Code writes at the
// end is the one the Chrome extension authenticates against.
func startClaudeSignIn(exe, dir string) error {
	if capture := os.Getenv("FLIPAI_BROWSER_TEST_CAPTURE"); capture != "" {
		return os.WriteFile(capture, []byte(claudeSignInArgs(exe)), 0600)
	}
	shell := strings.TrimSpace(os.Getenv("COMSPEC"))
	if shell == "" {
		shell = "cmd.exe"
	}
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(shell)
	if err != nil {
		return err
	}
	params, err := syscall.UTF16PtrFromString(claudeSignInArgs(exe))
	if err != nil {
		return err
	}
	var workdir *uint16
	if strings.TrimSpace(dir) != "" && existingDir(dir) {
		if workdir, err = syscall.UTF16PtrFromString(dir); err != nil {
			return err
		}
	}
	r, _, callErr := procPlatformShellOpen.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(workdir)),
		1, // SW_SHOWNORMAL
	)
	if r <= 32 {
		return fmt.Errorf("open a sign-in console with %s failed (ShellExecuteW=%d): %v", shell, r, callErr)
	}
	return nil
}

// openFolder shows a local folder in File Explorer. It is used only for the
// FlipAi data and log folders the user already owns.
func openFolder(path string) error {
	if capture := os.Getenv("FLIPAI_BROWSER_TEST_CAPTURE"); capture != "" {
		return os.WriteFile(capture, []byte(path), 0600)
	}
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	r, _, callErr := procPlatformShellOpen.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	if r <= 32 {
		return fmt.Errorf("open folder %q failed (ShellExecuteW=%d): %v", path, r, callErr)
	}
	return nil
}

func installedExePath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return "", fmt.Errorf("LOCALAPPDATA not set")
	}
	dir := filepath.Join(base, "Programs", "FlipAi")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "FlipAi.exe"), nil
}
func copySelfInstall() (string, error) {
	src, err := os.Executable()
	if err != nil {
		return "", err
	}
	dst, err := installedExePath()
	if err != nil {
		return "", err
	}
	if strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
		return dst, nil
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, b, 0755); err != nil {
		return "", err
	}
	return dst, nil
}

func regularExecutable(path string) bool { st, err := os.Stat(path); return err == nil && !st.IsDir() }
func existingDir(path string) bool       { st, err := os.Stat(path); return err == nil && st.IsDir() }

func resolveCodexExecutable(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" && !strings.EqualFold(configured, "codex") && !strings.EqualFold(configured, "codex.exe") {
		return configured
	}
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		root := filepath.Join(base, "OpenAI", "Codex", "bin")
		direct := filepath.Join(root, "codex.exe")
		if regularExecutable(direct) {
			return direct
		}
		matches, _ := filepath.Glob(filepath.Join(root, "*", "codex.exe"))
		sort.Slice(matches, func(i, j int) bool {
			si, ei := os.Stat(matches[i])
			sj, ej := os.Stat(matches[j])
			if ei == nil && ej == nil && !si.ModTime().Equal(sj.ModTime()) {
				return si.ModTime().After(sj.ModTime())
			}
			return strings.ToLower(matches[i]) > strings.ToLower(matches[j])
		})
		for _, p := range matches {
			if regularExecutable(p) {
				return p
			}
		}
	}
	if p, err := exec.LookPath("codex"); err == nil {
		return p
	}
	if configured != "" {
		return configured
	}
	return "codex"
}

// augmentCodexEnv makes the helper executables that belong to the selected
// Codex runtime discoverable. This is important on Windows where codex.exe can
// be staged in a per-user cache while sandbox/command-runner helpers live in a
// sibling runtime directory.
func augmentCodexEnv(exe string, env []string) []string {
	var dirs []string
	add := func(p string) {
		if p == "" || !existingDir(p) {
			return
		}
		for _, d := range dirs {
			if strings.EqualFold(d, p) {
				return
			}
		}
		dirs = append(dirs, p)
	}
	exeDir := filepath.Dir(exe)
	add(exeDir)
	// Standalone layout: <release>\bin\codex.exe + <release>\codex-resources\...
	add(filepath.Join(filepath.Dir(exeDir), "codex-resources"))
	// Desktop layout: %LOCALAPPDATA%\OpenAI\Codex\bin may contain helpers.
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		add(filepath.Join(base, "OpenAI", "Codex", "bin"))
	}
	if len(dirs) == 0 {
		return env
	}
	prefix := strings.Join(dirs, string(os.PathListSeparator))
	out := append([]string(nil), env...)
	for i, e := range out {
		if eq := strings.IndexByte(e, '='); eq > 0 && strings.EqualFold(e[:eq], "PATH") {
			out[i] = e[:eq+1] + prefix + string(os.PathListSeparator) + e[eq+1:]
			return out
		}
	}
	return append(out, "PATH="+prefix)
}

func resolveClaudeExecutable(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" && !strings.EqualFold(configured, "claude") && !strings.EqualFold(configured, "claude.exe") {
		return configured
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"claude.exe", "claude.cmd"} {
			p := filepath.Join(home, ".local", "bin", name)
			if regularExecutable(p) {
				return p
			}
		}
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	if configured != "" {
		return configured
	}
	return "claude"
}
