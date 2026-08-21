package main

import (
	"context"
	"errors"
	"fmt"
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

func healthOK(listen string) bool {
	c := &http.Client{Timeout: 700 * time.Millisecond}
	r, err := c.Get("http://" + listen + "/health")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	return r.StatusCode == http.StatusOK
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

func runLauncher(dataDir, cfgPath string) {
	cfg := loadOrCreateConfig(cfgPath, dataDir)
	_ = os.Remove(filepath.Join(dataDir, "quit.flag"))
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	// Always try to start the watchdog. A per-session Windows mutex makes a
	// duplicate watchdog exit immediately. This also repairs a missing tray if
	// the host survived but the watchdog was previously terminated.
	if err := spawnDetached(exe, "--watchdog"); err != nil {
		panic(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if healthOK(cfg.Listen) {
			break
		}
		time.Sleep(150 * time.Millisecond)
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
	if err := runSystemTray(ctx, "AI SMS Bridge — running", openSettings, quit); err != nil {
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
		log.Printf("AI SMS Bridge v%s host starting on http://%s", version, cfg.Listen)
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
