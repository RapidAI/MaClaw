// iWorkerCenter service entry point.
// Runs as a headless HTTP service (no Wails GUI).
// Serves the admin web UI at /admin/ and all API endpoints.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/app"
)

//go:embed web
var webAssets embed.FS

func main() {
	addr := flag.String("addr", ":9377", "listen address")
	flag.Parse()

	log.Printf("[iWorkerCenter] starting service on %s", *addr)

	// Bootstrap the modular backend (DB, migrations, all module routes)
	center, bootstrapErr := app.Bootstrap()
	if bootstrapErr != nil {
		log.Printf("[iWorkerCenter] WARNING: bootstrap failed: %v", bootstrapErr)
		log.Printf("[iWorkerCenter] admin API routes will return 503 until the issue is resolved")
	} else {
		defer center.Close()
	}

	mux := http.NewServeMux()

	// /health — always available
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if center != nil && center.Mux != nil {
			center.Mux.ServeHTTP(w, r)
			return
		}
		writeBootstrapError(w, bootstrapErr)
	})

	// Forward known non-admin API prefixes to center.Mux
	forwardOrUnavailable := func(w http.ResponseWriter, r *http.Request) {
		if center != nil && center.Mux != nil {
			center.Mux.ServeHTTP(w, r)
			return
		}
		writeBootstrapError(w, bootstrapErr)
	}
	mux.HandleFunc("/client/", forwardOrUnavailable)
	mux.HandleFunc("/runtime/", forwardOrUnavailable)
	mux.HandleFunc("/auth/", forwardOrUnavailable)
	mux.HandleFunc("/api/", forwardOrUnavailable)
	mux.HandleFunc("/v1/", forwardOrUnavailable)
	mux.HandleFunc("/diworker-auth/", forwardOrUnavailable)

	// Prepare SPA file server for the admin frontend.
	// Embed layout: web/admin/index.html → after fs.Sub("web/admin") → index.html
	adminFS, err := fs.Sub(webAssets, "web/admin")
	if err != nil {
		log.Fatalf("embed web assets: %v", err)
	}
	spaFS := http.FS(adminFS)
	spaFileServer := http.StripPrefix("/admin/", http.FileServer(spaFS))

	// /admin — redirect to /admin/
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})

	// /admin/ — API routes + SPA fallback
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		// If the path matches a known admin API prefix, forward to center.Mux
		if isAdminAPIPath(r.URL.Path) {
			if center != nil && center.Mux != nil {
				center.Mux.ServeHTTP(w, r)
			} else {
				writeBootstrapError(w, bootstrapErr)
			}
			return
		}
		// Otherwise serve static files or fall back to index.html for SPA routing
		relPath := strings.TrimPrefix(r.URL.Path, "/admin/")
		if relPath == "" {
			relPath = "index.html"
		}
		// Try to open the file; if it exists, serve it directly
		if f, err := spaFS.Open(relPath); err == nil {
			_ = f.Close()
			spaFileServer.ServeHTTP(w, r)
			return
		}
		// File doesn't exist — serve index.html for SPA client-side routing
		r.URL.Path = "/admin/index.html"
		spaFileServer.ServeHTTP(w, r)
	})

	// Redirect root to admin
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/admin/", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	server := &http.Server{Addr: *addr, Handler: mux}

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[iWorkerCenter] shutting down...")
		cancel()
		server.Close()
	}()

	fmt.Printf("iWorkerCenter listening on %s\n", *addr)
	fmt.Printf("Admin UI: http://localhost%s/admin/\n", *addr)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}

	_ = ctx
}

// isAdminAPIPath returns true if the path looks like a backend API route
// rather than a SPA navigation route.
func isAdminAPIPath(path string) bool {
	apiPrefixes := []string{
		"/admin/roles",
		"/admin/colleagues",
		"/admin/memories",
		"/admin/capabilities",
		"/admin/capabilities-import",
		"/admin/collaborations",
		"/admin/workflows",
		"/admin/workflow-instances",
		"/admin/workflow-design",
		"/admin/audit",
		"/admin/config-bundles",
		"/admin/security",
		"/admin/model-endpoints",
		"/admin/model-routing-policies",
		"/admin/im-config",
		"/admin/diworker-auth",
		"/admin/compute",
		"/admin/recommend",
		"/admin/profile",
		"/admin/password",
	}
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// writeBootstrapError writes a JSON 503 response indicating bootstrap failure.
func writeBootstrapError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	msg := "bootstrap failed"
	if err != nil {
		msg = err.Error()
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"code":    "BOOTSTRAP_FAILED",
		"message": msg,
	})
}
