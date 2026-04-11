package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func registerStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return
	}
	if !strings.HasPrefix(routePrefix, "/") {
		routePrefix = "/" + routePrefix
	}
	routePrefix = strings.TrimRight(routePrefix, "/")
	indexPath := filepath.Join(staticDir, "index.html")

	mux.HandleFunc("GET "+routePrefix, func(w http.ResponseWriter, r *http.Request) {
		serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix)
	})
	mux.HandleFunc("GET "+routePrefix+"/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix)
	})
}

func serveStaticIndexFallback(w http.ResponseWriter, r *http.Request, staticDir string, indexPath string, routePrefix string) {
	relPath := strings.TrimPrefix(r.URL.Path, routePrefix)
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "" {
		relPath = "index.html"
	}

	candidate := filepath.Join(staticDir, filepath.FromSlash(relPath))

	absStatic, err := filepath.Abs(staticDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil || !strings.HasPrefix(absCandidate, absStatic+string(filepath.Separator)) && absCandidate != absStatic {
		http.NotFound(w, r)
		return
	}

	if info, err := os.Stat(candidate); err == nil {
		if !info.IsDir() {
			http.ServeFile(w, r, candidate)
			return
		}
		dirIndex := filepath.Join(candidate, "index.html")
		if _, err := os.Stat(dirIndex); err == nil {
			http.ServeFile(w, r, dirIndex)
			return
		}
	}

	if _, err := os.Stat(indexPath); err == nil {
		http.ServeFile(w, r, indexPath)
		return
	}

	http.NotFound(w, r)
}
