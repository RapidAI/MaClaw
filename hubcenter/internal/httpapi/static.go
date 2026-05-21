package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func registerStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	staticDir = resolveStaticDir(staticDir)
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

func resolveStaticDir(staticDir string) string {
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return ""
	}
	if filepath.IsAbs(staticDir) {
		return filepath.Clean(staticDir)
	}

	baseDirs := []string{"."}
	if exe, err := os.Executable(); err == nil && exe != "" {
		exeDir := filepath.Dir(exe)
		baseDirs = append(baseDirs, exeDir, filepath.Join(exeDir, ".."), filepath.Join(exeDir, "..", ".."))
	}

	return resolveStaticDirFromBases(staticDir, baseDirs)
}

func resolveStaticDirFromBases(staticDir string, baseDirs []string) string {
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return ""
	}
	if filepath.IsAbs(staticDir) {
		return filepath.Clean(staticDir)
	}

	seen := map[string]struct{}{}
	for _, baseDir := range baseDirs {
		baseDir = strings.TrimSpace(baseDir)
		if baseDir == "" {
			continue
		}
		candidate := filepath.Clean(filepath.Join(baseDir, staticDir))
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	return filepath.Clean(staticDir)
}

// registerAdminStaticRoutes is an alias for registerStaticRoutes with /admin default.
func registerAdminStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	if routePrefix == "" {
		routePrefix = "/admin"
	}
	registerStaticRoutes(mux, staticDir, routePrefix)
}

func registerSharedStaticAssets(mux *http.ServeMux, staticDir string) {
	staticDir = resolveStaticDir(staticDir)
	if strings.TrimSpace(staticDir) == "" {
		return
	}
	cssPath := filepath.Join(staticDir, "pro-ui.css")
	mux.HandleFunc("GET /pro-ui.css", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, cssPath)
	})
}

func serveStaticIndexFallback(w http.ResponseWriter, r *http.Request, staticDir string, indexPath string, routePrefix string) {
	relPath := strings.TrimPrefix(r.URL.Path, routePrefix)
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "" {
		relPath = "index.html"
	}

	candidate := filepath.Join(staticDir, filepath.FromSlash(relPath))

	// Prevent path traversal: ensure candidate stays within staticDir.
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
		// Directory: try its index.html
		dirIndex := filepath.Join(candidate, "index.html")
		if _, err := os.Stat(dirIndex); err == nil {
			http.ServeFile(w, r, dirIndex)
			return
		}
	}

	// Fallback to root index.html (SPA-style)
	if _, err := os.Stat(indexPath); err == nil {
		http.ServeFile(w, r, indexPath)
		return
	}

	http.NotFound(w, r)
}
