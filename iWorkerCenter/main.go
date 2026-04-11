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
	"strconv"
	"syscall"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/config"
)

//go:embed web/admin/dist
var adminAssets embed.FS

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	configPath := flag.String("config", "", "Path to iWorkerCenter config file")
	if err := flag.CommandLine.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Build the unified HTTP mux
	mux, cleanup, err := buildMux(cfg)
	if err != nil {
		return fmt.Errorf("build mux: %w", err)
	}
	defer cleanup()

	// Serve embedded admin frontend at /admin/ with SPA fallback.
	// API routes under /admin/ are handled by center.Mux (registered in buildMux).
	// Non-API requests serve static files or fall back to index.html for SPA routing.
	adminFS, err := fs.Sub(adminAssets, "web/admin/dist")
	if err != nil {
		return fmt.Errorf("embed admin assets: %w", err)
	}
	// Set the SPA fallback handler so buildMux's /admin/ handler can use it
	spaFallback = serveSPA(http.FS(adminFS))

	addr := cfg.Server.Host + ":" + strconv.Itoa(cfg.Server.Port)
	server := &http.Server{Addr: addr, Handler: mux}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[iWorkerCenter] shutting down...")
		cancel()
		_ = server.Close()
	}()

	log.Printf("[iWorkerCenter] listening on %s", addr)
	log.Printf("[iWorkerCenter] Admin UI: http://localhost:%d/admin/", cfg.Server.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen: %w", err)
	}
	_ = ctx
	return nil
}
