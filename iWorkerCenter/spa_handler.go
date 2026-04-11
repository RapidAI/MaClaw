package main

import (
	"net/http"
	"strings"
)

// serveSPA returns an http.HandlerFunc that serves a single-page application.
// Static assets (js, css, images) are served directly from the embedded FS.
// All other paths fall back to index.html for client-side routing.
func serveSPA(fsys http.FileSystem) http.HandlerFunc {
	fileServer := http.StripPrefix("/admin/", http.FileServer(fsys))

	return func(w http.ResponseWriter, r *http.Request) {
		// Strip the /admin/ prefix to get the relative path
		relPath := strings.TrimPrefix(r.URL.Path, "/admin/")
		if relPath == "" {
			relPath = "index.html"
		}

		// Try to open the file — if it exists, serve it directly
		if f, err := fsys.Open(relPath); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// File doesn't exist — serve index.html for SPA routing
		r.URL.Path = "/admin/index.html"
		fileServer.ServeHTTP(w, r)
	}
}
