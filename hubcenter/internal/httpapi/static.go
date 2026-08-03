package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	staticAssetCacheControl = "public, max-age=300"
	staticHTMLCacheControl  = "no-cache"
)

func registerStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	registerStaticRoutesWithAssetCache(mux, staticDir, routePrefix, staticAssetCacheControl)
}

func registerStaticRoutesWithAssetCache(mux *http.ServeMux, staticDir string, routePrefix string, assetCacheControl string) {
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
		serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix, assetCacheControl)
	})
	mux.HandleFunc("GET "+routePrefix+"/{rest...}", func(w http.ResponseWriter, r *http.Request) {
		serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix, assetCacheControl)
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

	// The packaged server keeps web/ next to the executable. In local development
	// and package tests, the process may instead start below hubcenter/. Search
	// the current directory and its parents before falling back to executable
	// locations so public static routes remain available in both layouts.
	baseDirs := staticDirWorkingBases()
	if exe, err := os.Executable(); err == nil && exe != "" {
		exeDir := filepath.Dir(exe)
		baseDirs = append(baseDirs, exeDir, filepath.Join(exeDir, ".."), filepath.Join(exeDir, "..", ".."))
	}

	return resolveStaticDirFromBases(staticDir, baseDirs)
}

func staticDirWorkingBases() []string {
	bases := []string{"."}
	workingDir, err := os.Getwd()
	if err != nil || workingDir == "" {
		return bases
	}
	for dir := filepath.Clean(workingDir); ; {
		bases = append(bases, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return bases
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

// registerAdminStaticRoutes serves the admin app with revalidation. The HTML
// contains cache-busting versions for each asset so a fresh page cannot pair
// with an outdated script after deployment.
func registerAdminStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	if routePrefix == "" {
		routePrefix = "/admin"
	}
	registerStaticRoutesWithAssetCache(mux, staticDir, routePrefix, staticHTMLCacheControl)
}

func registerSharedStaticAssets(mux *http.ServeMux, staticDir string) {
	staticDir = resolveStaticDir(staticDir)
	if strings.TrimSpace(staticDir) == "" {
		return
	}
	cssPath := filepath.Join(staticDir, "pro-ui.css")
	mux.HandleFunc("GET /pro-ui.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", staticAssetCacheControl)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeFile(w, r, cssPath)
	})
}

func shouldCacheStaticAsset(relPath string) bool {
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".css", ".js", ".mjs", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".otf":
		return true
	default:
		return false
	}
}

func shouldRevalidateStaticHTML(relPath string) bool {
	ext := strings.ToLower(filepath.Ext(relPath))
	return relPath == "index.html" || ext == ".html" || ext == ".htm"
}

func serveStaticIndexFallback(w http.ResponseWriter, r *http.Request, staticDir string, indexPath string, routePrefix string, assetCacheControl string) {
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
			if shouldCacheStaticAsset(relPath) {
				w.Header().Set("Cache-Control", assetCacheControl)
				w.Header().Set("X-Content-Type-Options", "nosniff")
			} else if shouldRevalidateStaticHTML(relPath) {
				w.Header().Set("Cache-Control", staticHTMLCacheControl)
			}
			http.ServeFile(w, r, candidate)
			return
		}
		// Directory: try its index.html
		dirIndex := filepath.Join(candidate, "index.html")
		if _, err := os.Stat(dirIndex); err == nil {
			w.Header().Set("Cache-Control", staticHTMLCacheControl)
			http.ServeFile(w, r, dirIndex)
			return
		}
	}
	if shouldCacheStaticAsset(relPath) || strings.HasPrefix(filepath.ToSlash(relPath), "assets/") {
		http.NotFound(w, r)
		return
	}

	// Fallback to root index.html (SPA-style)
	if _, err := os.Stat(indexPath); err == nil {
		w.Header().Set("Cache-Control", staticHTMLCacheControl)
		http.ServeFile(w, r, indexPath)
		return
	}

	http.NotFound(w, r)
}
