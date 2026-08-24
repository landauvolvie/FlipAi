//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
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
)

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

func openFlipAiWindow(target string) error {
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
	w.Navigate(target)
	w.Run()
	handleWindowClosed()
	return nil
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
	cmd := exec.Command(path, "/SILENT", "/NORESTART", "/SUPPRESSMSGBOXES", mode)
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

func installAutostart(exe string) error {
	// Remove the pre-v0.6.1 value so upgrades cannot start two copies.
	_ = uninstallAutostartNamed("AISMSBridge")
	defer autostartProbe.invalidate()
	return installAutostartNamed("FlipAi", exe)
}
func installAutostartNamed(name, exe string) error {
	value := fmt.Sprintf("\"%s\" --watchdog", exe)
	cmd := exec.Command("reg.exe", "ADD", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", name, "/t", "REG_SZ", "/d", value, "/f")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("enable startup: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
func uninstallAutostart() error {
	_ = uninstallAutostartNamed("AISMSBridge")
	defer autostartProbe.invalidate()
	return uninstallAutostartNamed("FlipAi")
}
func uninstallAutostartNamed(name string) error {
	cmd := exec.Command("reg.exe", "DELETE", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", name, "/f")
	hideWindow(cmd)
	_ = cmd.Run()
	return nil
}

// autostartEnabled reports whether this Windows user's Run key still holds the
// FlipAi entry, so Settings shows the real state instead of a remembered one.
// The registry read is cached: the status snapshot behind it is rebuilt on
// every page render and every status poll.
var autostartProbe = newCachedBool(20*time.Second, func() bool {
	cmd := exec.Command("reg.exe", "QUERY", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "FlipAi")
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(string(out), "FlipAi")
})

func autostartEnabled() bool { return autostartProbe.get() }

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
