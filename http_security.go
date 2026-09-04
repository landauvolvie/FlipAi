package main

import "net/http"

// secureLocalHandler adds browser-facing hardening headers to FlipAi's local
// control UI. The server is already loopback-only and authenticated; these
// headers make the intended trust boundary explicit to the embedded WebView and
// prevent local pages from being framed or MIME-sniffed by another origin.
func secureLocalHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}
