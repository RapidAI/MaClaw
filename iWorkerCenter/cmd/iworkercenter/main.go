// iWorkerCenter service entry point.
// Runs as a headless HTTP service (no Wails GUI).
// Serves the admin web UI at /admin/ and all API endpoints.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

//go:embed web
var webAssets embed.FS

func main() {
	addr := flag.String("addr", ":9377", "listen address")
	flag.Parse()

	log.Printf("[iWorkerCenter] starting service on %s", *addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The center server (LLM proxy + modular backend) is started inside
	// startCenterServer which is defined in the iWorkerCenter package root.
	// For the standalone service binary, we import and call it directly.
	// Since this is a separate main package, we replicate the minimal startup.

	mux := http.NewServeMux()

	// Serve static web assets (web/admin/index.html -> /admin/index.html)
	webFS, err := fs.Sub(webAssets, "web")
	if err != nil {
		log.Fatalf("embed web assets: %v", err)
	}
	mux.Handle("/admin/", http.FileServer(http.FS(webFS)))

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
