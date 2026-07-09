package structureddata

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed webui_assets/*
var webuiAssetsFS embed.FS

// handleWebConsoleV2 serves the new modular admin console from embedded files.
func (s *HTTPServer) handleWebConsoleV2(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(webuiAssetsFS, "webui_assets")
	if err != nil {
		http.Error(w, "internal error", 500)
		return
	}

	// Determine which file to serve
	reqPath := r.URL.Path

	// For root routes (/, /ui, /console, /console/), serve index.html
	if reqPath == "/" || reqPath == "/ui" || reqPath == "/console" || reqPath == "/console/" {
		serveFile(w, sub, "index.html")
		return
	}

	// For /console/xxx paths, strip the prefix
	filePath := strings.TrimPrefix(reqPath, "/console/")
	if filePath == reqPath {
		// Path didn't have /console/ prefix — not a static asset request
		serveFile(w, sub, "index.html")
		return
	}

	// Try to serve the requested file
	f, err := sub.Open(filePath)
	if err != nil {
		// File not found — serve index.html (SPA fallback)
		serveFile(w, sub, "index.html")
		return
	}
	f.Close()

	serveFile(w, sub, filePath)
}

func serveFile(w http.ResponseWriter, fsys fs.FS, path string) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	// Set content type based on extension
	ext := filepath.Ext(path)
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	w.Write(data)
}
