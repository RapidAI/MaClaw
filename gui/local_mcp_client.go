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

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// LocalMCPClient manages a single local (stdio) MCP server process.
// It launches the command, communicates via JSON-RPC 2.0 over stdin/stdout,
// and provides tool discovery and invocation.
type LocalMCPClient struct {
	entry     corelib.LocalMCPServerEntry
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	mu        sync.Mutex   // serializes writes to stdin; responses are routed by id
	stateMu   sync.RWMutex // guards running, tools
	pendingMu sync.Mutex
	pending   map[int64]chan localMCPPendingResponse
	nextID    atomic.Int64
	tools     []MCPToolView
	running   bool
	cancel    context.CancelFunc
}

type localMCPPendingResponse struct {
	result json.RawMessage
	err    error
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
func NewLocalMCPClient(entry corelib.LocalMCPServerEntry) *LocalMCPClient {
	return &LocalMCPClient{entry: entry}
}

// startMaxRetries is the number of attempts to launch and initialize an MCP
// server before giving up. Some servers need a moment to become ready, so
// retrying a few times avoids the "works manually but not on auto-start" issue.
const startMaxRetries = 3

// startRetryBaseDelay is the initial backoff between retry attempts.
const startRetryBaseDelay = 2 * time.Second

// Start launches the child process and performs the MCP initialize handshake.
// It retries up to startMaxRetries times with exponential backoff when the
// process starts but the initialize handshake fails (e.g. server not ready).
func (c *LocalMCPClient) Start(ctx context.Context) error {
	var lastErr error
	for attempt := 1; attempt <= startMaxRetries; attempt++ {
		lastErr = c.tryStart(ctx)
		if lastErr == nil {
			if attempt > 1 {
				log.Printf("[LocalMCP] %s started successfully on attempt %d", c.entry.Name, attempt)
			}
			return nil
		}

		log.Printf("[LocalMCP] %s start attempt %d/%d failed: %v", c.entry.Name, attempt, startMaxRetries, lastErr)

		if attempt < startMaxRetries {
			delay := startRetryBaseDelay * time.Duration(attempt)
			select {
			case <-ctx.Done():
				return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
			case <-time.After(delay):
			}
		}
	}
	return fmt.Errorf("failed after %d attempts: %w", startMaxRetries, lastErr)
}

// tryStart performs a single attempt to launch the process and complete the
// MCP initialize handshake. On failure it cleans up so the next attempt
// starts fresh.
func (c *LocalMCPClient) tryStart(ctx context.Context) error {
	c.stateMu.Lock()
	if c.running {
		c.stateMu.Unlock()
		return nil
	}

	childCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(childCtx, c.entry.Command, c.entry.Args...)
	coretool.PrepareCommandForTreeKill(cmd)
	cmd.Cancel = func() error {
		coretool.TerminateCommandTree(cmd)
		return nil
	}

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
	c.pending = make(map[int64]chan localMCPPendingResponse)
	c.running = true
	c.stateMu.Unlock()

	// Monitor process exit in background to update running state.
	go c.watchProcess()
	go c.readLoop()

	// Perform MCP initialize handshake.
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
	c.failPending(fmt.Errorf("process exited: %v", err))
}

func (c *LocalMCPClient) readLoop() {
	for {
		c.stateMu.RLock()
		alive := c.running
		reader := c.stdout
		c.stateMu.RUnlock()
		if !alive || reader == nil {
			return
		}

		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			c.stateMu.Lock()
			if c.running {
				c.running = false
			}
			c.stateMu.Unlock()
			c.failPending(fmt.Errorf("read response: %w", readErr))
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.ID == 0 {
			continue
		}

		c.pendingMu.Lock()
		ch := c.pending[resp.ID]
		delete(c.pending, resp.ID)
		c.pendingMu.Unlock()
		if ch == nil {
			continue
		}
		if resp.Error != nil {
			ch <- localMCPPendingResponse{err: fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)}
		} else {
			ch <- localMCPPendingResponse{result: resp.Result}
		}
	}
}

func (c *LocalMCPClient) failPending(err error) {
	if err == nil {
		err = fmt.Errorf("local MCP client stopped")
	}
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[int64]chan localMCPPendingResponse)
	c.pendingMu.Unlock()
	for _, ch := range pending {
		ch <- localMCPPendingResponse{err: err}
	}
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

// sendRequest sends a JSON-RPC request and waits for its matching response.
// Writes are serialized so JSON lines do not interleave. Responses are routed
// by request id in readLoop, allowing concurrent calls to the same MCP server.
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

	responseC := make(chan localMCPPendingResponse, 1)
	c.pendingMu.Lock()
	if c.pending == nil {
		c.pending = make(map[int64]chan localMCPPendingResponse)
	}
	c.pending[id] = responseC
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()

	c.mu.Lock()
	_, err = c.stdin.Write(data)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	select {
	case resp := <-responseC:
		if resp.err != nil {
			return nil, resp.err
		}
		return resp.result, nil
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("timeout waiting for response to %s", method)
	}
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
	if args == nil {
		args = map[string]interface{}{}
	}
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
	c.failPending(fmt.Errorf("local MCP client stopped"))

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
		coretool.TerminateCommandTree(c.cmd)
	}
}
