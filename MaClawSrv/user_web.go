package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed user_web/*
var userWebFS embed.FS

func setUserSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

func (s *HTTPServer) handleUserWeb(w http.ResponseWriter, r *http.Request) {
	setUserSecurityHeaders(w)
	if r.URL.Path == "/app" {
		target := "/app/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}
	assets, err := fs.Sub(userWebFS, "user_web")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	http.StripPrefix("/app/", http.FileServer(http.FS(assets))).ServeHTTP(w, r)
}
