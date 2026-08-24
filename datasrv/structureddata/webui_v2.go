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

	reqPath := r.URL.Path
	if reqPath == "/" || reqPath == "/ui" || reqPath == "/console" || reqPath == "/console/" {
		serveFile(w, sub, "index.html")
		return
	}

	// <base href="/console/"> so JS/CSS resolve under this prefix.
	filePath := strings.TrimPrefix(reqPath, "/console/")
	if filePath == reqPath {
		serveFile(w, sub, "index.html")
		return
	}

	f, err := sub.Open(filePath)
	if err != nil {
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
