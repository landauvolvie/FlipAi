package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

func main() {
	dataDir, cfgPath, statePath, tokenPath, err := appPaths()
	if err != nil {
		panic(err)
	}
	if err := ensureDataDir(dataDir); err != nil {
		panic(err)
	}

	mode := "ui"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	switch mode {
	case "--host":
		runHost(dataDir, cfgPath, statePath, tokenPath)
	case "--watchdog":
		runWatchdog(dataDir, cfgPath)
	case "--tray":
		runTrayProcess(dataDir, cfgPath)
	case "--quit":
		requestQuit(dataDir, "command-line quit")
	case "--uninstall", "uninstall":
		_ = uninstallAutostart()
		_ = os.WriteFile(filepath.Join(dataDir, "quit.flag"), []byte("uninstall"), 0600)
	default:
		runLauncher(dataDir, cfgPath)
	}
}

func loadOrCreateConfig(cfgPath, dataDir string) Config {
	cfg, err := loadConfig(cfgPath, dataDir)
	if errors.Is(err, os.ErrNotExist) {
		cfg = defaultConfig(dataDir)
		if err := saveConfig(cfgPath, cfg); err != nil {
			panic(err)
		}
	} else if err != nil {
		panic(err)
	}
	return cfg
}

// healthOK verifies that the process on the configured loopback port is
// actually this FlipAi version, not merely an unrelated local HTTP service.
func healthOK(listen string) bool {
	c := &http.Client{Timeout: 700 * time.Millisecond}
	r, err := c.Get("http://" + listen + "/health")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return false
	}
	var v struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&v) != nil {
		return false
	}
	return v.OK && v.Version == version
}

func requestQuit(dataDir, reason string) {
	if reason == "" {
		reason = "quit"
	}
	_ = os.WriteFile(filepath.Join(dataDir, "quit.flag"), []byte(reason+"\n"+time.Now().Format(time.RFC3339)), 0600)
}

func quitRequested(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, "quit.flag"))
	return err == nil
}

func showLauncherError(dataDir string, cfg Config, detail string) {
	if detail == "" {
		detail = "The FlipAi background host did not become ready."
	}
	path := filepath.Join(dataDir, "launch-error.html")
	body := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>FlipAi could not start</title><style>body{margin:0;background:#f5f6fa;color:#17151f;font:15px/1.55 system-ui,-apple-system,"Segoe UI",sans-serif}.box{max-width:680px;margin:10vh auto;padding:24px}.card{background:#fff;border:1px solid #e7e4ee;border-radius:22px;padding:30px;box-shadow:0 20px 60px rgba(45,34,88,.09)}.mark{width:44px;height:44px;border-radius:14px;background:#fff0ee;color:#b42318;display:grid;place-items:center;font-weight:900;font-size:24px}h1{font-size:26px;margin:18px 0 8px}p{color:#625d6b}.code{background:#18151f;color:#f2edff;padding:13px;border-radius:12px;font:13px ui-monospace,Consolas,monospace}.note{background:#fff6df;color:#765000;padding:13px;border-radius:12px;margin-top:14px}</style></head><body><div class="box"><div class="card"><div class="mark">!</div><h1>FlipAi could not open its local control page</h1><p>%s</p><div class="code">Expected local address: http://%s</div><div class="note">Common causes: another program is using this local port, endpoint security blocked the EXE, or the background process could not start. FlipAi does not request administrator access or attempt to bypass security policy.</div><p>Open Task Manager to confirm AISMSBridge.exe is allowed to run, then launch FlipAi again. If this is a managed PC, your administrator may need to allow the application.</p></div></div></body></html>`, html.EscapeString(detail), html.EscapeString(cfg.Listen))
	if err := os.WriteFile(path, []byte(body), 0600); err == nil && os.Getenv("AISMSBRIDGE_NO_BROWSER") != "1" {
		_ = openBrowser(path)
	}
}

func runLauncher(dataDir, cfgPath string) {
	cfg := loadOrCreateConfig(cfgPath, dataDir)
	_ = os.Remove(filepath.Join(dataDir, "quit.flag"))
	exe, err := os.Executable()
	if err != nil {
		showLauncherError(dataDir, cfg, err.Error())
		return
	}
	// Always try to start the watchdog. A per-session Windows mutex makes a
	// duplicate watchdog exit immediately. This also repairs a missing tray if
	// the host survived but the watchdog was previously terminated.
	if err := spawnDetached(exe, "--watchdog"); err != nil {
		showLauncherError(dataDir, cfg, "The FlipAi watchdog could not start: "+err.Error())
		return
	}
	ready := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if healthOK(cfg.Listen) {
			ready = true
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !ready {
		showLauncherError(dataDir, cfg, "The background host did not identify itself on the configured loopback port within 10 seconds.")
		return
	}
	if os.Getenv("AISMSBRIDGE_NO_BROWSER") != "1" {
		_ = openBrowser("http://" + cfg.Listen + "/?token=" + cfg.LocalToken)
	}
}

func runWatchdog(dataDir, cfgPath string) {
	release, owner, err := acquireWatchdogInstance()
	if err != nil || !owner {
		return
	}
	defer release()

	cfg := loadOrCreateConfig(cfgPath, dataDir)
	_ = os.Remove(filepath.Join(dataDir, "quit.flag"))
	exe, err := os.Executable()
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchQuitFlag(ctx, dataDir, cancel)

	// The watchdog owns the lifetime of both children. Do not return until both
	// have actually stopped; otherwise Windows can be left with an orphan tray
	// process after an explicit Quit.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		superviseHost(ctx, exe, cfg.Listen)
	}()
	go func() {
		defer wg.Done()
		superviseChild(ctx, exe, "--tray")
	}()
	<-ctx.Done()
	wg.Wait()
}

func watchQuitFlag(ctx context.Context, dataDir string, cancel context.CancelFunc) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if quitRequested(dataDir) {
				cancel()
				return
			}
		}
	}
}

func superviseHost(ctx context.Context, exe, listen string) {
	var child *exec.Cmd
	var done chan error
	failures := 0
	nextStart := time.Time{}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if child != nil {
			select {
			case <-done:
				child = nil
				failures++
				delay := time.Second * time.Duration(1<<minInt(failures, 5))
				if delay > 30*time.Second {
					delay = 30 * time.Second
				}
				nextStart = time.Now().Add(delay)
			default:
			}
		}
		if healthOK(listen) {
			failures = 0
			nextStart = time.Time{}
		} else if child == nil && (nextStart.IsZero() || !time.Now().Before(nextStart)) {
			cmd := exec.CommandContext(ctx, exe, "--host")
			hideWindow(cmd)
			if err := cmd.Start(); err != nil {
				failures++
				delay := time.Second * time.Duration(1<<minInt(failures, 5))
				if delay > 30*time.Second {
					delay = 30 * time.Second
				}
				nextStart = time.Now().Add(delay)
			} else {
				child = cmd
				done = make(chan error, 1)
				go func(c *exec.Cmd, ch chan<- error) { ch <- c.Wait() }(cmd, done)
			}
		}

		select {
		case <-ctx.Done():
			if child != nil && child.Process != nil {
				_ = child.Process.Kill()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
				}
			}
			return
		case <-ticker.C:
		}
	}
}

func superviseChild(ctx context.Context, exe string, args ...string) {
	failures := 0
	for {
		if ctx.Err() != nil {
			return
		}
		cmd := exec.CommandContext(ctx, exe, args...)
		hideWindow(cmd)
		started := time.Now()
		if err := cmd.Start(); err == nil {
			_ = cmd.Wait()
		}
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > 30*time.Second {
			failures = 0
		} else {
			failures++
		}
		delay := time.Second * time.Duration(1<<minInt(failures, 5))
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func runTrayProcess(dataDir, cfgPath string) {
	cfg := loadOrCreateConfig(cfgPath, dataDir)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	openSettings := func() {
		_ = openBrowser("http://" + cfg.Listen + "/?token=" + cfg.LocalToken)
	}
	quit := func() {
		requestQuit(dataDir, "tray quit")
		cancel()
	}
	if err := runSystemTray(ctx, "FlipAi — running", openSettings, quit); err != nil {
		// The watchdog will retry the tray process. This commonly happens only
		// when Explorer has not finished starting yet.
		return
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func runHost(dataDir, cfgPath, statePath, tokenPath string) {
	lf, _ := os.OpenFile(filepath.Join(dataDir, "bridge.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if lf != nil {
		log.SetOutput(io.MultiWriter(lf))
		defer lf.Close()
	}
	cfg := loadOrCreateConfig(cfgPath, dataDir)
	mailClient, oauthClient, ge := buildConfiguredMailClient(cfg.Gmail, dataDir, tokenPath)
	if ge != nil {
		log.Printf("Gmail not ready: %v", ge)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	app := &App{dataDir: dataDir, configPath: cfgPath, statePath: statePath, tokenPath: tokenPath, cfg: cfg, mail: mailClient, gmail: oauthClient, stop: cancel}
	quitFlag := filepath.Join(dataDir, "quit.flag")
	go func() {
		t := time.NewTicker(400 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := os.Stat(quitFlag); err == nil {
					cancel()
					return
				}
			}
		}
	}()
	srv := &http.Server{Addr: cfg.Listen, Handler: app.handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		log.Printf("FlipAi v%s host starting on http://%s", version, cfg.Listen)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP: %v", err)
			cancel()
		}
	}()
	go func() { time.Sleep(800 * time.Millisecond); app.startBridge(ctx) }()
	<-ctx.Done()
	sd, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	_ = srv.Shutdown(sd)
	fmt.Print("")
}
