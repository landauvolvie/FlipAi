package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func listenFromServerURL(raw string) string {
	return strings.TrimPrefix(raw, "http://")
}

func TestHealthOKRequiresFlipAiIdentityAndVersion(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"ok":true,"version":%q}`, version)
	}))
	defer good.Close()
	if !healthOK(listenFromServerURL(good.URL)) {
		t.Fatal("valid FlipAi health endpoint was rejected")
	}

	wrongVersion := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":true,"version":"some-other-program"}`)
	}))
	defer wrongVersion.Close()
	if healthOK(listenFromServerURL(wrongVersion.URL)) {
		t.Fatal("unrelated/wrong-version local HTTP service was accepted as FlipAi")
	}

	plain200 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "OK")
	}))
	defer plain200.Close()
	if healthOK(listenFromServerURL(plain200.URL)) {
		t.Fatal("generic HTTP 200 service was accepted as FlipAi")
	}
}
