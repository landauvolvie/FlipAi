package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDownloadWithProgressResumesPartialFile(t *testing.T) {
	payload := []byte(strings.Repeat("FlipAi-update-payload-", 4096))
	const already = 8192
	var gotRange string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		start := 0
		if strings.HasPrefix(gotRange, "bytes=") {
			raw := strings.TrimSuffix(strings.TrimPrefix(gotRange, "bytes="), "-")
			if n, err := strconv.Atoi(raw); err == nil {
				start = n
			}
		}
		if start > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)-start))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		}
		_, _ = w.Write(payload[start:])
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "FlipAi-Setup-v99.0.0.exe")
	if err := os.WriteFile(dest+".part", payload[:already], 0600); err != nil {
		t.Fatal(err)
	}
	var finalDone, finalTotal int64
	if _, err := downloadWithProgress(context.Background(), srv.URL, dest, func(done, total int64) {
		finalDone, finalTotal = done, total
	}); err != nil {
		t.Fatal(err)
	}
	if gotRange != fmt.Sprintf("bytes=%d-", already) {
		t.Fatalf("Range = %q, want bytes=%d-", gotRange, already)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatal("resumed installer bytes do not match the source payload")
	}
	if finalDone != int64(len(payload)) || finalTotal != int64(len(payload)) {
		t.Fatalf("final progress = %d/%d, want %d/%d", finalDone, finalTotal, len(payload), len(payload))
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatalf("completed .part file should have been renamed, stat err=%v", err)
	}
}
