package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var version = "dev"

// HubCLIClient handles WebSocket connection to the Hub for CLI operations.
type HubCLIClient struct {
	hubURL string
	token  string
	conn   *websocket.Conn
}

// CLIEnvelope is the message envelope for Hub WebSocket communication.
type CLIEnvelope struct {
	Type      string      `json:"type"`
	RequestID string      `json:"request_id,omitempty"`
	Payload   interface{} `json:"payload,omitempty"`
}

// CLISessionView is a minimal session representation for CLI display.
type CLISessionView struct {
	ID          string `json:"id"`
	Tool        string `json:"tool"`
	Title       string `json:"title"`
	ProjectPath string `json:"project_path"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

// Connect dials the Hub WebSocket endpoint and sends an auth message.
func (c *HubCLIClient) Connect() error {
	// Build WebSocket URL: ws://{hub_url}/ws/cli?token={token}
	wsURL := strings.Replace(c.hubURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = strings.TrimRight(wsURL, "/")

	u, err := url.Parse(wsURL + "/ws/cli")
	if err != nil {
		return fmt.Errorf("invalid hub URL: %w", err)
	}
	q := u.Query()
	q.Set("token", c.token)
	u.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect to Hub: %w", err)
	}
	c.conn = conn

	// Send auth message
	authMsg := CLIEnvelope{
		Type: "auth.cli",
		Payload: map[string]string{
			"token": c.token,
		},
	}
	if err := c.SendJSON(authMsg); err != nil {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("failed to send auth: %w", err)
	}

	// Read auth response
	var resp CLIEnvelope
	if err := c.ReadJSON(&resp); err != nil {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("failed to read auth response: %w", err)
	}
	if resp.Type == "error" {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("auth failed: %v", resp.Payload)
	}

	return nil
}

// Close closes the WebSocket connection.
func (c *HubCLIClient) Close() {
	if c.conn != nil {
		c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		c.conn.Close()
		c.conn = nil
	}
}

// SendJSON sends a JSON-encoded message over the WebSocket.
func (c *HubCLIClient) SendJSON(v interface{}) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return c.conn.WriteJSON(v)
}

// ReadJSON reads a JSON-encoded message from the WebSocket.
func (c *HubCLIClient) ReadJSON(v interface{}) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return c.conn.ReadJSON(v)
}

// sessionList sends a list_sessions request to the Hub and prints results.
func sessionList(client *HubCLIClient) error {
	req := CLIEnvelope{
		Type:      "cli.list_sessions",
		RequestID: fmt.Sprintf("cli-%d", time.Now().UnixNano()),
	}
	if err := client.SendJSON(req); err != nil {
		return fmt.Errorf("send list request: %w", err)
	}

	var raw json.RawMessage
	if err := client.conn.ReadJSON(&raw); err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Parse envelope to check type
	var envelope struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if envelope.Type == "error" {
		return fmt.Errorf("hub error: %s", string(envelope.Payload))
	}

	var sessions []CLISessionView
	if err := json.Unmarshal(envelope.Payload, &sessions); err != nil {
		return fmt.Errorf("parse sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No active sessions.")
		return nil
	}

	fmt.Printf("%-20s %-12s %-10s %-30s %s\n", "ID", "TOOL", "STATUS", "PROJECT", "TITLE")
	fmt.Println(strings.Repeat("-", 90))
	for _, s := range sessions {
		title := s.Title
		if len(title) > 30 {
			title = title[:27] + "..."
		}
		project := s.ProjectPath
		if len(project) > 30 {
			project = "..." + project[len(project)-27:]
		}
		fmt.Printf("%-20s %-12s %-10s %-30s %s\n", s.ID, s.Tool, s.Status, project, title)
	}
	return nil
}

// sessionStart creates a new session via the Hub API.
func sessionStart(client *HubCLIClient, tool, project, template string) error {
	payload := map[string]string{}
	if tool != "" {
		payload["tool"] = tool
	}
	if project != "" {
		payload["project_path"] = project
	}
	if template != "" {
		payload["template_name"] = template
	}

	req := CLIEnvelope{
		Type:      "cli.start_session",
		RequestID: fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Payload:   payload,
	}
	if err := client.SendJSON(req); err != nil {
		return fmt.Errorf("send start request: %w", err)
	}

	var raw json.RawMessage
	if err := client.conn.ReadJSON(&raw); err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var envelope struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if envelope.Type == "error" {
		var errMsg struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(envelope.Payload, &errMsg); err == nil && errMsg.Message != "" {
			return fmt.Errorf("hub error: %s", errMsg.Message)
		}
		return fmt.Errorf("hub error: %s", string(envelope.Payload))
	}

	var result struct {
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(envelope.Payload, &result); err != nil {
		return fmt.Errorf("parse session result: %w", err)
	}

	fmt.Printf("Session created: %s (status: %s)\n", result.SessionID, result.Status)
	return nil
}

// sessionAttach attaches to a running session, streaming output and accepting input.
// Ctrl+C gracefully disconnects without killing the session.
func sessionAttach(client *HubCLIClient, sessionID string) error {
	// Send attach request
	req := CLIEnvelope{
		Type:      "cli.attach_session",
		RequestID: fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Payload:   map[string]string{"session_id": sessionID},
	}
	if err := client.SendJSON(req); err != nil {
		return fmt.Errorf("send attach request: %w", err)
	}

	// Read attach response
	var raw json.RawMessage
	if err := client.conn.ReadJSON(&raw); err != nil {
		return fmt.Errorf("read attach response: %w", err)
	}
	var envelope struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("parse attach response: %w", err)
	}
	if envelope.Type == "error" {
		return fmt.Errorf("attach failed: %s", string(envelope.Payload))
	}

	fmt.Printf("Attached to session %s. Press Ctrl+C to detach.\n", sessionID)

	// Catch SIGINT to gracefully detach
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)

	doneCh := make(chan struct{})

	// Read loop: read messages from WebSocket and print session output
	go func() {
		defer close(doneCh)
		for {
			var msg CLIEnvelope
			if err := client.ReadJSON(&msg); err != nil {
				// Connection closed or error — stop reading
				return
			}
			if msg.Payload != nil {
				if data, err := json.Marshal(msg.Payload); err == nil {
					fmt.Println(string(data))
				}
			}
		}
	}()

	// Input loop: read lines from stdin and send as session input
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Text()
			inputMsg := CLIEnvelope{
				Type:      "cli.session_input",
				RequestID: fmt.Sprintf("cli-%d", time.Now().UnixNano()),
				Payload:   map[string]string{"session_id": sessionID, "input": line},
			}
			if err := client.SendJSON(inputMsg); err != nil {
				return
			}
		}
	}()

	// Wait for Ctrl+C or read loop to finish
	select {
	case <-sigCh:
		fmt.Println("\nDetaching from session (session continues running)...")
	case <-doneCh:
		fmt.Println("Session connection closed.")
	}

	return nil
}

// sessionKill terminates a session via the Hub API.
func sessionKill(client *HubCLIClient, sessionID string) error {
	req := CLIEnvelope{
		Type:      "cli.kill_session",
		RequestID: fmt.Sprintf("cli-%d", time.Now().UnixNano()),
		Payload:   map[string]string{"session_id": sessionID},
	}
	if err := client.SendJSON(req); err != nil {
		return fmt.Errorf("send kill request: %w", err)
	}

	var raw json.RawMessage
	if err := client.conn.ReadJSON(&raw); err != nil {
		return fmt.Errorf("read kill response: %w", err)
	}
	var envelope struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("parse kill response: %w", err)
	}
	if envelope.Type == "error" {
		return fmt.Errorf("kill failed: %s", string(envelope.Payload))
	}

	fmt.Printf("Session %s terminated.\n", sessionID)
	return nil
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: maclaw-tool [flags] <command> <action> [args]

Commands:
  session list                List all active sessions
  session start               Start a new session
  session attach <session-id> Attach to a running session
  session kill <session-id>   Kill a session

  security-check [--mode standard|strict|relaxed] [--project /path]
                              Run security check (stdin JSON, local only)
  audit-record [--audit-dir dir]
                              Record audit entry (stdin JSON, local only)

Flags:
  --version                   Show version
`)
	flag.PrintDefaults()
}

func main() {
	hubURL := flag.String("hub-url", "http://localhost:9099", "Hub server URL")
	token := flag.String("token", "", "Authentication token (required)")
	showVersion := flag.Bool("version", false, "Show version")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Printf("maclaw-tool %s\n", version)
		return
	}

	// Check for local-only subcommands BEFORE --token validation.
	// security-check and audit-record run purely locally without Hub connection.
	args := flag.Args()
	if len(args) > 0 {
		switch args[0] {
		case "security-check":
			scFlags := flag.NewFlagSet("security-check", flag.ExitOnError)
			mode := scFlags.String("mode", "standard", "Security mode: standard, strict, relaxed")
			project := scFlags.String("project", "", "Project path for project-level policy")
			scFlags.Parse(args[1:])
			os.Exit(runSecurityCheck(*mode, *project))
		case "audit-record":
			arFlags := flag.NewFlagSet("audit-record", flag.ExitOnError)
			auditDir := arFlags.String("audit-dir", "", "Audit log directory (default: ~/.maclaw/audit/)")
			arFlags.Parse(args[1:])
			os.Exit(runAuditRecord(*auditDir))
		}
	}

	if *token == "" {
		fmt.Fprintln(os.Stderr, "Error: --token is required")
		usage()
		os.Exit(1)
	}

	// args already declared above; reuse for session commands
	if len(args) < 2 {
		usage()
		os.Exit(1)
	}

	command := args[0]
	action := args[1]

	if command != "session" {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		usage()
		os.Exit(1)
	}

	client := &HubCLIClient{
		hubURL: *hubURL,
		token:  *token,
	}

	if err := client.Connect(); err != nil {
		log.Fatalf("Connection failed: %v\nCheck Hub URL (%s) and token.", err, *hubURL)
	}
	defer client.Close()

	var err error
	switch action {
	case "list":
		err = sessionList(client)
	case "start":
		// Parse start-specific flags from remaining args
		startFlags := flag.NewFlagSet("start", flag.ExitOnError)
		tool := startFlags.String("tool", "", "Tool name (e.g., claude, codex)")
		project := startFlags.String("project", "", "Project path")
		tmpl := startFlags.String("template", "", "Template name")
		startFlags.Parse(args[2:])
		err = sessionStart(client, *tool, *project, *tmpl)
	case "attach":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "Error: session attach requires a session ID")
			os.Exit(1)
		}
		err = sessionAttach(client, args[2])
	case "kill":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "Error: session kill requires a session ID")
			os.Exit(1)
		}
		err = sessionKill(client, args[2])
	default:
		fmt.Fprintf(os.Stderr, "Unknown action: %s\n", action)
		usage()
		os.Exit(1)
	}

	if err != nil {
		log.Fatalf("Error: %v", err)
	}
}
