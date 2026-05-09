package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/RapidAI/CodeClaw/datasrv/structureddata"
)

var serviceVersion = "dev"

func main() {
	if len(os.Args) > 1 && isHelpArg(os.Args[1]) {
		printUsage(os.Stdout)
		os.Exit(0)
	}
	addr := getenv("MACLAW_DATA_HTTP_ADDR", "127.0.0.1:18180")
	token := strings.TrimSpace(os.Getenv("MACLAW_DATA_TOKEN"))
	if err := validateServiceToken(token); err != nil {
		log.Fatal(err)
	}
	if err := validateListenAddr(addr); err != nil {
		log.Fatal(err)
	}
	store, err := structureddata.NewSQLiteStore(defaultDBPath())
	if err != nil {
		log.Fatalf("create sqlite store: %v", err)
	}
	defer store.Close()
	svc := structureddata.NewService(store, "sqlite")
	apiKeys, err := structureddata.ParseAPIKeyPolicies(os.Getenv("MACLAW_DATA_API_KEYS"))
	if err != nil {
		log.Fatalf("parse MACLAW_DATA_API_KEYS: %v", err)
	}
	server := structureddata.NewHTTPServerWithAPIKeys(svc, token, serviceVersion, apiKeys)
	httpServer := &http.Server{Addr: addr, Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 120 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		log.Printf("MaClawDataSrv listening on %s", addr)
		errCh <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("shutdown server: %v", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}

func defaultDBPath() string {
	if path := strings.TrimSpace(os.Getenv("MACLAW_DATA_SQLITE_PATH")); path != "" {
		return path
	}
	root := strings.TrimSpace(os.Getenv("MACLAW_DATA_ROOT"))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			root = ".maclaw_data"
		} else {
			root = filepath.Join(home, ".maclaw_data")
		}
	}
	return filepath.Join(root, "data.db")
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func validateListenAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("MaClawDataSrv defaults to loopback-only; refusing plain HTTP addr %q", addr)
	}
	return nil
}

func isHelpArg(arg string) bool {
	switch strings.TrimSpace(arg) {
	case "-h", "--help", "help":
		return true
	default:
		return false
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "MaClawDataSrv compatibility service entry point")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  maclaw-data-srv")
	fmt.Fprintln(w, "      Start the HTTP service using environment variables.")
	fmt.Fprintln(w, "  maclaw-data-srv --help")
	fmt.Fprintln(w, "      Show this help without starting the service.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Service environment:")
	fmt.Fprintln(w, "  MACLAW_DATA_TOKEN        Optional service bearer token. When set, it must be at least 24 characters.")
	fmt.Fprintln(w, "  MACLAW_DATA_HTTP_ADDR    Optional listen address. Default: 127.0.0.1:18180. Plain HTTP is loopback-only.")
	fmt.Fprintln(w, "  MACLAW_DATA_SQLITE_PATH  Optional explicit SQLite database path.")
	fmt.Fprintln(w, "  MACLAW_DATA_ROOT         Optional data root. Default: $HOME/.maclaw_data; database file is data.db.")
	fmt.Fprintln(w, "  MACLAW_DATA_API_KEYS     Optional JSON array of static API key policies.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "First-time administrator setup:")
	fmt.Fprintln(w, "  GET  /api/v1/setup/status")
	fmt.Fprintln(w, "  POST /api/v1/setup/admin")
	fmt.Fprintln(w, "  POST /api/v1/login")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Offline administrator recovery commands are available from the independent datasrv module:")
	fmt.Fprintln(w, "  cd datasrv")
	fmt.Fprintln(w, "  go run ./cmd/maclaw-data-srv admin --help")
}

func validateServiceToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if len(token) < 24 {
		return fmt.Errorf("MACLAW_DATA_TOKEN must be at least 24 characters when set")
	}
	return nil
}
