// maclaw-acp-bridge is a thin ACP stdio adapter for VS Code (and other clients).
//
// Product rule: the ONLY agent brain is MaClaw GUI (Mode B). This process must
// not reimplement tools, LLM routing, or a second RunLoop (no TUI-style fork).
//
// Prefer Mode B: attach to the running MaClaw GUI ACP host
// (session cwd → GUI project_path / tool working dir).
// Fall back: third-party IM Gateway HTTP when Mode B is unavailable.
//
// Stdout is reserved for ACP NDJSON. All logs go to stderr.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/acpagent"
)

var version = "0.2.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("maclaw-acp-bridge", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	showVersion := fs.Bool("version", false, "print version")
	doctor := fs.Bool("doctor", false, "check GUI gateway connectivity and exit")
	configPath := fs.String("config", "", "MaClaw GUI config.json path (default: <MaclawBaseDir>/config.json)")
	baseURL := fs.String("base", "", "gateway base URL override (or MACLAW_GATEWAY_URL)")
	token := fs.String("token", "", "gateway bearer token (or MACLAW_GATEWAY_TOKEN)")
	clientID := fs.String("client", acpagent.DefaultClientID, "gateway client id")
	clientName := fs.String("client-name", "MaClaw ACP Bridge", "gateway client display name")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintf(os.Stderr, "unexpected args: %v\n", fs.Args())
		return 2
	}

	if *showVersion {
		fmt.Fprintf(os.Stdout, "maclaw-acp-bridge %s\n", version)
		return 0
	}

	ep := acpagent.DiscoverGateway(*configPath)
	if u := strings.TrimSpace(*baseURL); u != "" {
		ep.BaseURL = strings.TrimRight(u, "/")
	}
	if t := strings.TrimSpace(*token); t != "" {
		ep.Token = t
		ep.OK = true
	}

	if *doctor {
		modeBOK := false
		if modeB, ok := acpagent.DiscoverModeB(); ok {
			fmt.Fprintf(os.Stdout, "modeB: discovered host=%s port=%d agent=%s\n", modeB.Host, modeB.Port, modeB.Agent)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			cli, err := acpagent.DialModeB(ctx, modeB)
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stdout, "modeB.dial: FAIL (%v)\n", err)
			} else {
				_ = cli.Close()
				fmt.Fprintln(os.Stdout, "modeB.dial: ok (GUI AI assistant programming agent)")
				modeBOK = true
			}
		} else {
			fmt.Fprintln(os.Stdout, "modeB: not running (start MaClaw GUI)")
		}
		report := acpagent.Doctor(ep)
		fmt.Fprint(os.Stdout, report)
		if modeBOK || strings.Contains(report, "health: ok") {
			return 0
		}
		return 1
	}

	logger := log.New(os.Stderr, "[acp-bridge] ", log.LstdFlags)

	// Prefer Mode B: GUI-hosted ACP → desktop AI assistant (programming agent).
	if modeB, ok := acpagent.DiscoverModeB(); ok {
		logger.Printf("Mode B detected (%s:%d agent=%s) — proxying to GUI AI assistant", modeB.Host, modeB.Port, modeB.Agent)
		if err := acpagent.ServeStdioModeBProxy(os.Stdin, os.Stdout, logger); err != nil {
			logger.Printf("Mode B proxy exit: %v", err)
			return 1
		}
		return 0
	}
	logger.Printf("Mode B not available; falling back to IM Gateway attach")

	if strings.TrimSpace(ep.Token) == "" {
		logger.Printf("error: neither Mode B nor gateway token available — start MaClaw GUI (AI assistant) or enable Third-party Gateway")
		return 1
	}

	gw := acpagent.NewGatewayClient(ep)
	gw.ClientID = strings.TrimSpace(*clientID)
	gw.ClientName = strings.TrimSpace(*clientName)

	bridge, err := acpagent.NewBridge(acpagent.BridgeOptions{
		AgentInfo: acpagent.ImplementationInfo{
			Name:    "maclaw-gui-bridge",
			Title:   "MaClaw GUI Bridge",
			Version: version,
		},
		Gateway: gw,
		Logger:  logger,
	})
	if err != nil {
		logger.Printf("error: %v", err)
		return 1
	}

	logger.Printf("serving ACP on stdio → gateway %s", ep.BaseURL)
	if err := bridge.ServeStdio(os.Stdin, os.Stdout); err != nil {
		logger.Printf("exit: %v", err)
		return 1
	}
	return 0
}
