package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func listenFromServerURL(raw string) string {
	return strings.TrimPrefix(raw, "http://")
}

// The release workflow tags the release and names the installer from the
// VERSION file, while the app reports the config.go constant on /health, the
// Settings page, and the Activity page. Nothing else catches a drift between
// them, so a release could otherwise ship FlipAi-Setup-v0.7.0.exe containing a
// binary that calls itself 0.6.3. This test runs inside the release workflow's
// own `go test ./...` step, ahead of any publishing.
func TestVersionFileMatchesCompiledVersion(t *testing.T) {
	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatalf("cannot read the VERSION file that drives the release: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != version {
		t.Fatalf("VERSION file says %q but the compiled version constant is %q; bump both (config.go and VERSION, plus installer/FlipAi.iss)", got, version)
	}
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
