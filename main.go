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
	case "--quit":
		_ = os.WriteFile(filepath.Join(dataDir, "quit.flag"), []byte(time.Now().Format(time.RFC3339)), 0600)
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

func runLauncher(dataDir, cfgPath string) {
	cfg := loadOrCreateConfig(cfgPath, dataDir)
	_ = os.Remove(filepath.Join(dataDir, "quit.flag"))
	if !healthOK(cfg.Listen) {
		exe, err := os.Executable()
		if err != nil {
			panic(err)
		}
		if err := spawnDetached(exe, "--watchdog"); err != nil {
			panic(err)
		}
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if healthOK(cfg.Listen) {
				break
			}
			time.Sleep(150 * time.Millisecond)
		}
	}
	_ = openBrowser("http://" + cfg.Listen + "/?token=" + cfg.LocalToken)
}

func runWatchdog(dataDir, cfgPath string) {
	cfg := loadOrCreateConfig(cfgPath, dataDir)
	if healthOK(cfg.Listen) {
		return
	}
	quit := filepath.Join(dataDir, "quit.flag")
	_ = os.Remove(quit)
	exe, err := os.Executable()
	if err != nil {
		return
	}
	failures := 0
	for {
		if _, err := os.Stat(quit); err == nil {
			return
		}
		cmd := exec.Command(exe, "--host")
		hideWindow(cmd)
		if err := cmd.Start(); err != nil {
			failures++
		} else {
			started := time.Now()
			_ = cmd.Wait()
			if time.Since(started) > 30*time.Second {
				failures = 0
			} else {
				failures++
			}
		}
		if _, err := os.Stat(quit); err == nil {
			return
		}
		delay := time.Second * time.Duration(1<<minInt(failures, 5))
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		time.Sleep(delay)
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
