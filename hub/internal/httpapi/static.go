package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/brand"
)

func registerPWAStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	registerStaticRoutes(mux, staticDir, routePrefix)
}

func registerAdminStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	staticDir = resolveStaticDir(staticDir)
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return
	}
	if routePrefix == "" {
		routePrefix = "/admin"
	}
	if !strings.HasPrefix(routePrefix, "/") {
		routePrefix = "/" + routePrefix
	}
	routePrefix = strings.TrimRight(routePrefix, "/")

	brandName := brand.Current().DisplayName

	serve := func(w http.ResponseWriter, r *http.Request) {
		// The admin console's role-based navigation is client-side. Never allow a
		// shared intermediary cache to serve an older JavaScript bundle after an
		// authorization UI change: doing so can temporarily expose tenant-only
		// navigation to a global administrator. The existing no-store policy for
		// JS/CSS must also apply to HEAD responses, which net/http dispatches to
		// the matching GET route.
		if r.Method == http.MethodHead {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		}
		relPath := strings.TrimPrefix(r.URL.Path, routePrefix)
		relPath = strings.TrimPrefix(relPath, "/")

		if relPath != "" {
			candidate := filepath.Join(staticDir, filepath.FromSlash(relPath))
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				ext := strings.ToLower(filepath.Ext(relPath))
				if ext == ".js" || ext == ".css" {
					w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
				}
				if ext == ".js" && brandName != "" && brandName != "MaClaw" {
					data, err := os.ReadFile(candidate)
					if err != nil {
						http.NotFound(w, r)
						return
					}
					replaced := strings.ReplaceAll(string(data), "MaClaw", brandName)
					w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
					_, _ = w.Write([]byte(replaced))
					return
				}
				http.ServeFile(w, r, candidate)
				return
			}
			ext := strings.ToLower(filepath.Ext(relPath))
			if staticAssetExtensions[ext] {
				http.NotFound(w, r)
				return
			}
		}

		indexPath := filepath.Join(staticDir, "index.html")
		htmlData, err := os.ReadFile(indexPath)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		html := string(htmlData)

		// Inject brand name as a runtime JS variable instead of string-replacing
		// inside admin.js. This avoids the fragile escapeInlineScript path that
		// causes "Unexpected end of input" when the 107KB admin.js is inlined.
		if brandName != "" && brandName != "MaClaw" {
			html = strings.ReplaceAll(html, "MaClaw", brandName)
			brandInjection := `<script>window.__MACLAW_BRAND__=` + strconv.Quote(brandName) + `;</script>`
			if idx := strings.Index(html, "<script"); idx >= 0 {
				html = html[:idx] + brandInjection + "\n" + html[idx:]
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		_, _ = w.Write([]byte(html))
	}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		mux.HandleFunc(method+" "+routePrefix, serve)
		mux.HandleFunc(method+" "+routePrefix+"/{rest...}", serve)
	}
}

func escapeInlineScript(js string) string {
	var out strings.Builder
	searchStart := 0
	for {
		idx := strings.Index(strings.ToLower(js[searchStart:]), "</script")
		if idx < 0 {
			out.WriteString(js[searchStart:])
			return out.String()
		}
		idx += searchStart
		out.WriteString(js[searchStart:idx])
		out.WriteString("<\\/script")
		searchStart = idx + len("</script")
	}
}

func registerBindStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	staticDir = resolveStaticDir(staticDir)
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return
	}
	if routePrefix == "" {
		routePrefix = "/bind"
	}
	if !strings.HasPrefix(routePrefix, "/") {
		routePrefix = "/" + routePrefix
	}
	routePrefix = strings.TrimRight(routePrefix, "/")
	indexPath := filepath.Join(staticDir, "index.html")

	allowIframe := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Frame-Options", "ALLOWALL")
			w.Header().Set("Content-Security-Policy", "frame-ancestors *")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			next(w, r)
		}
	}

	mux.HandleFunc("GET "+routePrefix, allowIframe(func(w http.ResponseWriter, r *http.Request) {
		serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix)
	}))
	mux.HandleFunc("GET "+routePrefix+"/{rest...}", allowIframe(func(w http.ResponseWriter, r *http.Request) {
		serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix)
	}))
}

func registerGetCreditsStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	staticDir = resolveStaticDir(staticDir)
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return
	}
	if routePrefix == "" {
		routePrefix = "/get-credits"
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

func registerCardStoreStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	staticDir = resolveStaticDir(staticDir)
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return
	}
	if routePrefix == "" {
		routePrefix = "/card_store"
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

func registerStaticRoutes(mux *http.ServeMux, staticDir string, routePrefix string) {
	staticDir = resolveStaticDir(staticDir)
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return
	}

	if routePrefix == "" {
		routePrefix = "/app"
	}
	if !strings.HasPrefix(routePrefix, "/") {
		routePrefix = "/" + routePrefix
	}
	routePrefix = strings.TrimRight(routePrefix, "/")
	indexPath := filepath.Join(staticDir, "index.html")
	if routePrefix == "/app" {
		registerRootFaviconRoute(mux, staticDir)
	}

	serve := func(w http.ResponseWriter, r *http.Request) {
		if routePrefix == "/connector" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; object-src 'none'; script-src 'self'; style-src 'self'")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
		}
		serveStaticIndexFallback(w, r, staticDir, indexPath, routePrefix)
	}
	mux.HandleFunc("GET "+routePrefix, serve)
	mux.HandleFunc("GET "+routePrefix+"/{rest...}", serve)
}

func resolveStaticDir(staticDir string) string {
	staticDir = strings.TrimSpace(staticDir)
	if staticDir == "" {
		return ""
	}
	if filepath.IsAbs(staticDir) {
		if dirExists(staticDir) {
			return filepath.Clean(staticDir)
		}
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
		if dirExists(candidate) {
			return candidate
		}
	}
	return filepath.Clean(staticDir)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

var staticAssetExtensions = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".map": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".ico": true, ".webp": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true, ".otf": true,
	".json": true, ".xml": true, ".txt": true, ".webmanifest": true,
	".wasm": true, ".mp4": true, ".webm": true, ".mp3": true, ".ogg": true, ".pdf": true,
}

func registerRootFaviconRoute(mux *http.ServeMux, staticDir string) {
	faviconPath := filepath.Join(staticDir, "icons", "favicon-32x32.png")
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		if info, err := os.Stat(faviconPath); err == nil && !info.IsDir() {
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Header().Set("Content-Type", "image/png")
			http.ServeFile(w, r, faviconPath)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("HEAD /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		if info, err := os.Stat(faviconPath); err == nil && !info.IsDir() {
			w.Header().Set("Cache-Control", "public, max-age=3600")
			w.Header().Set("Content-Type", "image/png")
			return
		}
		http.NotFound(w, r)
	})
}

func serveStaticIndexFallback(w http.ResponseWriter, r *http.Request, staticDir string, indexPath string, routePrefix string) {
	relPath := strings.TrimPrefix(r.URL.Path, routePrefix)
	relPath = strings.TrimPrefix(relPath, "/")
	if relPath == "" {
		relPath = "index.html"
	}

	candidate := filepath.Join(staticDir, filepath.FromSlash(relPath))
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		if strings.ToLower(filepath.Ext(relPath)) == ".html" || relPath == "index.html" {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		}
		http.ServeFile(w, r, candidate)
		return
	}

	ext := strings.ToLower(filepath.Ext(relPath))
	if staticAssetExtensions[ext] {
		http.NotFound(w, r)
		return
	}

	if _, err := os.Stat(indexPath); err == nil {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, indexPath)
		return
	}

	http.NotFound(w, r)
}
