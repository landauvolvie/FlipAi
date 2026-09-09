package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestQuietUpdateIconInRealBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("browser harness")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	pw := playwrightModule(t)
	a := newTestApp(t)
	// Render every real page; status/install requests are controlled by the
	// browser harness so no actual installer can be started by this UI test.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rr := a.do(t, http.MethodGet, r.URL.RequestURI(), nil)
		for key, values := range rr.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(rr.Code)
		_, _ = w.Write(rr.Body.Bytes())
	}))
	defer srv.Close()
	cmd := exec.Command("node", filepath.Join("testdata", "quiet-updater.mjs"))
	cmd.Env = append(os.Environ(), "FLIPAI_UPDATE_TEST_URL="+srv.URL, "FLIPAI_PLAYWRIGHT_MODULE="+pw)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("updater browser: %v\n%s", err, out)
	}
}
