// Command maclaw-desktop-mcp serves one MaClaw cloud desktop over MCP stdio,
// for operators and local testing.
//
// Hub itself serves MCP at https://<hub-domain>/mcp (Streamable HTTP, Hub
// user tokens); that is the endpoint for Claude Desktop, Cursor and other
// remote clients. See docs/desktop-mcp.md. This binary is the optional local
// variant: it holds the service's desktop API token, so run it only where
// that token may live.
//
//	MACLAW_MCP_BACKEND=hub MACLAW_MCP_BACKEND_URL=https://hub.example \
//	MACLAW_MCP_BACKEND_TOKEN=<desktop API token> \
//	MACLAW_MCP_TENANT=t MACLAW_MCP_USER=u MACLAW_MCP_SCOPE=control \
//	maclaw-desktop-mcp
//
// MACLAW_MCP_BACKEND=desktopd talks to one desktopd (/v1/desktops/*) instead.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/RapidAI/CodeClaw/corelib/desktop/desktopmcp"
)

func main() {
	backend, err := newHTTPBackend(envOr("MACLAW_MCP_BACKEND", "hub"), os.Getenv("MACLAW_MCP_BACKEND_URL"), os.Getenv("MACLAW_MCP_BACKEND_TOKEN"), os.Getenv("MACLAW_MCP_IMAGE"))
	if err != nil {
		log.Fatalf("maclaw-desktop-mcp: %v", err)
	}
	server := desktopmcp.New(backend, desktopmcp.Options{Takeover: envOr("MACLAW_MCP_TAKEOVER", "viewer")})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	id := Identity{Name: "stdio", TenantID: envOr("MACLAW_MCP_TENANT", "tenant_default"), UserID: os.Getenv("MACLAW_MCP_USER"),
		Scope: envOr("MACLAW_MCP_SCOPE", "read"), AllowBash: os.Getenv("MACLAW_MCP_ALLOW_BASH") == "1", AllowInstall: os.Getenv("MACLAW_MCP_ALLOW_INSTALL") == "1"}
	if id.UserID == "" {
		log.Fatal("maclaw-desktop-mcp: MACLAW_MCP_USER is required")
	}
	if err := server.ServeStdio(ctx, id, os.Stdin, os.Stdout); err != nil {
		log.Fatalf("maclaw-desktop-mcp: %v", err)
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
