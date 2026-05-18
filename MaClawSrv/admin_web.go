package main

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed admin_web/*
var adminWebFS embed.FS

func setAdminSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

func (s *HTTPServer) withAdminSecurityHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setAdminSecurityHeaders(w)
		next(w, r)
	}
}

func (s *HTTPServer) handleAdminWeb(w http.ResponseWriter, r *http.Request) {
	setAdminSecurityHeaders(w)
	if r.URL.Path == "/admin" {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
		return
	}
	assets, err := fs.Sub(adminWebFS, "admin_web")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	http.StripPrefix("/admin/", http.FileServer(http.FS(assets))).ServeHTTP(w, r)
}
