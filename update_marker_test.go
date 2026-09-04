package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestUpdateMarkerKeepsReleaseAPIQuietWhenVersionIsCurrent(t *testing.T) {
	oldMarker, oldAPI := updateVersionFeedURL, updateAPIURL
	defer func() { updateVersionFeedURL, updateAPIURL = oldMarker, oldAPI }()

	var apiCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/VERSION":
			fmt.Fprint(w, version)
		case "/release":
			apiCalls.Add(1)
			http.Error(w, "release API should not be called", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	updateVersionFeedURL = server.URL + "/VERSION"
	updateAPIURL = server.URL + "/release"

	a := newTestApp(t)
	info := a.checkForUpdate(context.Background(), false)
	if got := apiCalls.Load(); got != 0 {
		t.Fatalf("release API calls = %d, want 0 when VERSION is current", got)
	}
	if info.CheckedAt.IsZero() {
		t.Fatal("30-second marker check did not record CheckedAt")
	}
}

func TestUpdateMarkerEscalatesToReleaseAPIWhenVersionAdvances(t *testing.T) {
	oldMarker, oldAPI := updateVersionFeedURL, updateAPIURL
	defer func() { updateVersionFeedURL, updateAPIURL = oldMarker, oldAPI }()

	var apiCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/VERSION":
			fmt.Fprint(w, "99.0.0")
		case "/release":
			apiCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name":   "v99.0.0",
				"name":       "FlipAi 99.0.0",
				"html_url":   "https://example.invalid/release",
				"draft":      false,
				"prerelease": false,
				"assets": []map[string]string{{
					"name":                 "FlipAi-Setup-v99.0.0.exe",
					"browser_download_url": "https://example.invalid/FlipAi-Setup-v99.0.0.exe",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	updateVersionFeedURL = server.URL + "/VERSION"
	updateAPIURL = server.URL + "/release"

	a := newTestApp(t)
	info := a.checkForUpdate(context.Background(), false)
	if got := apiCalls.Load(); got != 1 {
		t.Fatalf("release API calls = %d, want 1 after newer VERSION marker", got)
	}
	if !info.Newer() || info.Version != "99.0.0" {
		t.Fatalf("resolved update = %+v, want newer 99.0.0 release", info)
	}
}
