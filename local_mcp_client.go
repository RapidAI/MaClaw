package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LocalMCPClient manages a single local (stdio) MCP server process.
// It launches the command, communicates via JSON-RPC 2.0 over stdin/stdout,
// and provides tool discovery and invocation.
type LocalMCPClient struct {
	entry   LocalMCPServerEntry
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	mu      sync.Mutex // guards sendRequest (serializes JSON-RPC I/O)
	stateMu sync.RWMutex // guards running, tools
	nextID  atomic.Int64
	tools   []MCPToolView
	running bool
	cancel  context.CancelFunc
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewLocalMCPClient creates a client for the given local MCP server entry.
func NewLocalMCPClient(entry LocalMCPServerEntry) *LocalMCPClient {
	return &LocalMCPClient{entry: entry}
}

// Start launches the child process and performs the MCP initialize handshake.
func (c *LocalMCPClient) Start(ctx context.Context) error {
	c.stateMu.Lock()
	if c.running {
		c.stateMu.Unlock()
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(childCtx, c.entry.Command, c.entry.Args...)

	// Inherit current environment, then overlay custom env vars.
	cmd.Env = os.Environ()
	for k, v := range c.entry.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		c.stateMu.Unlock()
		cancel()
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		c.stateMu.Unlock()
		cancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	hideCommandWindow(cmd)

	if err := cmd.Start(); err != nil {
		c.stateMu.Unlock()
		cancel()
		return fmt.Errorf("start command %q: %w", c.entry.Command, err)
	}

	c.cmd = cmd
	c.stdin = stdinPipe
	c.stdout = bufio.NewReaderSize(stdoutPipe, 256*1024)
	c.cancel = cancel
	c.running = true
	c.stateMu.Unlock()

	// Monitor process exit in background to update running state.
	go c.watchProcess()

	// Perform MCP initialize handshake (holds mu for serialized I/O).
	if err := c.initialize(); err != nil {
		c.Stop()
		return fmt.Errorf("MCP initialize: %w", err)
	}

	return nil
}

// watchProcess waits for the child process to exit and marks the client
// as not running. This prevents stale "running" state when a process crashes.
func (c *LocalMCPClient) watchProcess() {
	if c.cmd == nil {
		return
	}
	err := c.cmd.Wait()
	if err != nil {
		log.Printf("[LocalMCP] process %s exited: %v", c.entry.Name, err)
	}
	c.stateMu.Lock()
	c.running = false
	c.stateMu.Unlock()
}

// initialize sends the MCP initialize request and initialized notification.
func (c *LocalMCPClient) initialize() error {
	initParams := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "maclaw",
			"version": "1.0.0",
		},
	}

	_, err := c.sendRequest("initialize", initParams)
	if err != nil {
		return err
	}

	// Send initialized notification (no id, no response expected).
	notification := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	data, _ := json.Marshal(notification)
	data = append(data, '\n')

	c.mu.Lock()
	_, writeErr := c.stdin.Write(data)
	c.mu.Unlock()
	if writeErr != nil {
		return fmt.Errorf("send initialized notification: %w", writeErr)
	}

	return nil
}

// sendRequest sends a JSON-RPC request and reads the response.
// It serializes all I/O through c.mu to prevent interleaved reads/writes.
func (c *LocalMCPClient) sendRequest(method string, params interface{}) (json.RawMessage, error) {
	c.stateMu.RLock()
	if !c.running {
		c.stateMu.RUnlock()
		return nil, fmt.Errorf("client not running")
	}
	c.stateMu.RUnlock()

	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read lines until we get a response with matching id.
	// Skip notifications and non-JSON lines (e.g. stderr leaking to stdout).
	// Use a hard deadline to avoid blocking forever if the process dies.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		// Check if process is still alive before blocking on read.
		c.stateMu.RLock()
		alive := c.running
		c.stateMu.RUnlock()
		if !alive {
			return nil, fmt.Errorf("process exited while waiting for response to %s", method)
		}

		line, readErr := c.stdout.ReadString('\n')
		if readErr != nil {
			return nil, fmt.Errorf("read response: %w", readErr)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			// Not valid JSON-RPC — skip (could be log output from the server).
			continue
		}

		if resp.ID != id {
			// Different request ID or notification — skip.
			continue
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		return resp.Result, nil
	}

	return nil, fmt.Errorf("timeout waiting for response to %s", method)
}

// DiscoverTools calls tools/list and caches the result.
func (c *LocalMCPClient) DiscoverTools() ([]MCPToolView, error) {
	result, err := c.sendRequest("tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var listResult struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &listResult); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}

	tools := make([]MCPToolView, len(listResult.Tools))
	for i, t := range listResult.Tools {
		tools[i] = MCPToolView{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}

	c.stateMu.Lock()
	c.tools = tools
	c.stateMu.Unlock()

	return tools, nil
}

// CallTool invokes a tool on the local MCP server.
func (c *LocalMCPClient) CallTool(toolName string, args map[string]interface{}) (string, error) {
	params := map[string]interface{}{
		"name":      toolName,
		"arguments": args,
	}

	result, err := c.sendRequest("tools/call", params)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

// GetTools returns the cached tool list.
func (c *LocalMCPClient) GetTools() []MCPToolView {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.tools
}

// IsRunning returns whether the process is alive.
func (c *LocalMCPClient) IsRunning() bool {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.running
}

// Stop terminates the child process.
func (c *LocalMCPClient) Stop() {
	c.stateMu.Lock()
	wasRunning := c.running
	c.running = false
	c.stateMu.Unlock()

	if !wasRunning {
		return
	}

	if c.cancel != nil {
		c.cancel()
	}
	if c.stdin != nil {
		c.stdin.Close()
	}
	// Kill the process; don't call Wait() here because watchProcess
	// already does that. Killing is idempotent.
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
}
