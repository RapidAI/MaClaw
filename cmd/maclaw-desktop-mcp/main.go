// Command maclaw-desktop-mcp exposes a MaClaw cloud desktop to external
// agents (Claude Desktop/Code, Cursor, any MCP client) over MCP.
//
// Streamable HTTP (multi-user):
//
//	MACLAW_MCP_BACKEND=hub MACLAW_MCP_BACKEND_URL=https://hub.example \
//	MACLAW_MCP_BACKEND_TOKEN=<desktop API token> \
//	MACLAW_MCP_TOKENS_FILE=/etc/maclaw/mcp-tokens.json \
//	maclaw-desktop-mcp -http 127.0.0.1:8790
//
// The tokens file lists client tokens by SHA-256 only:
//
//	[{"name":"alice-cursor","token_sha256":"<hex>","tenant_id":"t","user_id":"u","scope":"control","allow_bash":false,"allow_install":false}]
//
// stdio (one user, for a local client):
//
//	MACLAW_MCP_TENANT=t MACLAW_MCP_USER=u MACLAW_MCP_SCOPE=control maclaw-desktop-mcp -stdio
//
// "maclaw-desktop-mcp -hash-token" reads a token on stdin and prints its hash.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	httpAddr := flag.String("http", "", "listen address for Streamable HTTP (POST /mcp), e.g. 127.0.0.1:8790")
	stdio := flag.Bool("stdio", false, "serve one identity over stdin/stdout")
	hashToken := flag.Bool("hash-token", false, "read a client token on stdin and print its sha256 for the tokens file")
	flag.Parse()
	if *hashToken {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		fmt.Println(HashToken(strings.TrimSpace(line)))
		return
	}
	backend, err := newHTTPBackend(envOr("MACLAW_MCP_BACKEND", "hub"), os.Getenv("MACLAW_MCP_BACKEND_URL"), os.Getenv("MACLAW_MCP_BACKEND_TOKEN"), os.Getenv("MACLAW_MCP_IMAGE"))
	if err != nil {
		log.Fatalf("maclaw-desktop-mcp: %v", err)
	}
	server := &Server{backend: backend, takeover: envOr("MACLAW_MCP_TAKEOVER", "viewer"), settle: 500 * time.Millisecond}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch {
	case *stdio:
		id := Identity{Name: "stdio", TenantID: envOr("MACLAW_MCP_TENANT", "tenant_default"), UserID: os.Getenv("MACLAW_MCP_USER"),
			Scope: envOr("MACLAW_MCP_SCOPE", "read"), AllowBash: os.Getenv("MACLAW_MCP_ALLOW_BASH") == "1", AllowInstall: os.Getenv("MACLAW_MCP_ALLOW_INSTALL") == "1"}
		if id.UserID == "" {
			log.Fatal("maclaw-desktop-mcp: MACLAW_MCP_USER is required for -stdio")
		}
		if err := server.ServeStdio(ctx, id, os.Stdin, os.Stdout); err != nil {
			log.Fatalf("maclaw-desktop-mcp: %v", err)
		}
	case *httpAddr != "":
		tokens, err := loadTokens(os.Getenv("MACLAW_MCP_TOKENS_FILE"))
		if err != nil {
			log.Fatalf("maclaw-desktop-mcp: %v", err)
		}
		server.tokens = tokens
		srv := &http.Server{Addr: *httpAddr, Handler: server, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutdown)
		}()
		log.Printf("maclaw-desktop-mcp: %d client token(s), listening on %s/mcp", len(tokens), *httpAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("maclaw-desktop-mcp: %v", err)
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// loadTokens reads the client tokens file: identities keyed by token hash.
func loadTokens(path string) (map[string]Identity, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("MACLAW_MCP_TOKENS_FILE is required for -http")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []struct {
		Identity
		TokenSHA256 string `json:"token_sha256"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("tokens file is not a JSON array: %v", err)
	}
	out := map[string]Identity{}
	for i, entry := range entries {
		hash := strings.ToLower(strings.TrimSpace(entry.TokenSHA256))
		if len(hash) != 64 || strings.TrimSpace(entry.UserID) == "" {
			return nil, fmt.Errorf("tokens file entry %d needs token_sha256 (64 hex) and user_id", i+1)
		}
		if entry.TenantID == "" {
			entry.TenantID = "tenant_default"
		}
		if entry.Scope != "control" {
			entry.Scope = "read"
		}
		out[hash] = entry.Identity
	}
	return out, nil
}
