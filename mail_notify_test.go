package main

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type wakeMail struct {
	mu    sync.Mutex
	lists int
	wake  chan struct{}
}

func (m *wakeMail) Authorized() bool           { return true }
func (m *wakeMail) Test(context.Context) error { return nil }
func (m *wakeMail) List(context.Context) ([]string, error) {
	m.mu.Lock()
	m.lists++
	m.mu.Unlock()
	return nil, nil
}
func (m *wakeMail) Get(context.Context, string) (GmailMessage, error) { return GmailMessage{}, nil }
func (m *wakeMail) SendText(context.Context, string, string) error    { return nil }
func (m *wakeMail) WaitForChange(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.wake:
		return nil
	}
}
func (m *wakeMail) count() int { m.mu.Lock(); defer m.mu.Unlock(); return m.lists }

func TestBridgeWakesImmediatelyOnMailNotification(t *testing.T) {
	m := &wakeMail{wake: make(chan struct{}, 1)}
	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.PollSeconds = 999
	b := NewBridge(cfg, t.TempDir()+"/state.json", State{GmailBaselineUnix: time.Now().Unix()}, m, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)
	deadline := time.Now().Add(time.Second)
	for m.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.count() < 1 {
		t.Fatal("initial mailbox check did not run")
	}
	start := time.Now()
	m.wake <- struct{}{}
	deadline = time.Now().Add(700 * time.Millisecond)
	for m.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.count() < 2 {
		t.Fatal("mail notification did not wake bridge")
	}
	if time.Since(start) > 700*time.Millisecond {
		t.Fatal("mail wake was too slow")
	}
}

func TestIMAPIdleReturnsOnExists(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	c := &IMAPMailClient{email: "user@gmail.com", password: "abcdefghijklmnop"}
	c.dialIMAP = func(context.Context) (net.Conn, error) { return clientConn, nil }

	serverDone := make(chan error, 1)
	go func() {
		defer serverConn.Close()
		r := bufio.NewReader(serverConn)
		if _, err := serverConn.Write([]byte("* OK Gmail ready\r\n")); err != nil {
			serverDone <- err
			return
		}
		line, err := r.ReadString('\n')
		if err != nil { serverDone <- err; return }
		tag := strings.Fields(line)[0]
		serverConn.Write([]byte(tag + " OK LOGIN completed\r\n"))
		line, err = r.ReadString('\n')
		if err != nil { serverDone <- err; return }
		tag = strings.Fields(line)[0]
		serverConn.Write([]byte("* 1 EXISTS\r\n" + tag + " OK EXAMINE completed\r\n"))
		line, err = r.ReadString('\n')
		if err != nil { serverDone <- err; return }
		tag = strings.Fields(line)[0]
		if !strings.Contains(strings.ToUpper(line), " IDLE") { serverDone <- context.Canceled; return }
		serverConn.Write([]byte("+ idling\r\n"))
		time.Sleep(40 * time.Millisecond)
		serverConn.Write([]byte("* 2 EXISTS\r\n"))
		line, err = r.ReadString('\n')
		if err != nil { serverDone <- err; return }
		if strings.TrimSpace(line) != "DONE" { serverDone <- context.Canceled; return }
		serverConn.Write([]byte(tag + " OK IDLE terminated\r\n"))
		line, err = r.ReadString('\n')
		if err == nil {
			tag = strings.Fields(line)[0]
			serverConn.Write([]byte("* BYE\r\n" + tag + " OK LOGOUT completed\r\n"))
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := c.WaitForChange(ctx); err != nil { t.Fatal(err) }
	if time.Since(start) > time.Second { t.Fatal("IMAP IDLE wake was unexpectedly slow") }
	if err := <-serverDone; err != nil { t.Fatal(err) }
}

type pollMail struct {
	mu    sync.Mutex
	lists int
}
func (m *pollMail) Authorized() bool           { return true }
func (m *pollMail) Test(context.Context) error { return nil }
func (m *pollMail) List(context.Context) ([]string, error) { m.mu.Lock(); m.lists++; m.mu.Unlock(); return nil, nil }
func (m *pollMail) Get(context.Context, string) (GmailMessage, error) { return GmailMessage{}, nil }
func (m *pollMail) SendText(context.Context, string, string) error { return nil }
func (m *pollMail) count() int { m.mu.Lock(); defer m.mu.Unlock(); return m.lists }

func TestOAuthStylePollingChecksAboutOncePerSecond(t *testing.T) {
	m := &pollMail{}
	cfg := defaultConfig(t.TempDir())
	cfg.Gmail.PollSeconds = 1
	b := NewBridge(cfg, t.TempDir()+"/state.json", State{GmailBaselineUnix: time.Now().Unix()}, m, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.Run(ctx)
	deadline := time.Now().Add(1600 * time.Millisecond)
	for m.count() < 2 && time.Now().Before(deadline) { time.Sleep(20 * time.Millisecond) }
	if m.count() < 2 { t.Fatalf("expected initial + one-second mailbox checks, got %d", m.count()) }
}
