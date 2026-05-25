package plugin

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

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// LocalMCPPluginAdapter wraps a local stdio MCP Server as a Plugin implementation.
// It starts the configured command as a child process, communicates via JSON-RPC
// over stdin/stdout, and exposes discovered tools as ToolDefinitions.
type LocalMCPPluginAdapter struct {
	manifest PluginManifest
	config   PluginConfig
	tools    []ToolDefinition
	healthy  atomic.Bool

	// Parsed from RawTypeConfig["local_mcp"]
	command string
	args    []string
	env     map[string]string

	// Process management
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	nextID int64
}

// NewLocalMCPPluginAdapter creates a new LocalMCPPluginAdapter for the given manifest.
func NewLocalMCPPluginAdapter(manifest PluginManifest) *LocalMCPPluginAdapter {
	return &LocalMCPPluginAdapter{
		manifest: manifest,
	}
}

func (a *LocalMCPPluginAdapter) Manifest() PluginManifest { return a.manifest }

func (a *LocalMCPPluginAdapter) Init(cfg PluginConfig) error {
	a.config = cfg

	raw, ok := a.manifest.RawTypeConfig["local_mcp"]
	if !ok {
		return fmt.Errorf("local_mcp plugin %q: missing 'local_mcp' config section", a.manifest.Name)
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("local_mcp plugin %q: 'local_mcp' config must be a map", a.manifest.Name)
	}

	if cmd, ok := m["command"].(string); ok && cmd != "" {
		a.command = cmd
	} else {
		return fmt.Errorf("local_mcp plugin %q: 'local_mcp.command' is required", a.manifest.Name)
	}

	if args, ok := m["args"]; ok {
		if argList, ok := args.([]interface{}); ok {
			for _, v := range argList {
				if s, ok := v.(string); ok {
					a.args = append(a.args, s)
				}
			}
		}
	}

	if envMap, ok := m["env"].(map[string]interface{}); ok {
		a.env = make(map[string]string, len(envMap))
		for k, v := range envMap {
			a.env[k] = fmt.Sprintf("%v", v)
		}
	}

	return nil
}

func (a *LocalMCPPluginAdapter) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, a.command, a.args...)
	coretool.PrepareCommandForTreeKill(cmd)
	cmd.Cancel = func() error {
		coretool.TerminateCommandTree(cmd)
		return nil
	}

	// Inherit current env + overlay plugin env.
	cmd.Env = os.Environ()
	for k, v := range a.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if a.manifest.Dir != "" {
		cmd.Dir = a.manifest.Dir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr // let MCP server errors show in logs

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}
	a.mu.Lock()
	a.cmd = cmd
	a.stdin = stdin
	a.reader = bufio.NewReader(stdout)
	a.mu.Unlock()

	// MCP initialize handshake.
	initResp, err := a.rpcCall("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "maclaw",
			"version": "1.0.0",
		},
	})
	if err != nil {
		a.killProcess()
		return fmt.Errorf("MCP initialize: %w", err)
	}

	// Send initialized notification.
	_ = a.rpcNotify("notifications/initialized", nil)

	log.Printf("INFO: local_mcp plugin %q initialized: %v", a.manifest.Name, initResp)

	// List tools.
	toolsResp, err := a.rpcCall("tools/list", map[string]interface{}{})
	if err != nil {
		log.Printf("WARN: local_mcp plugin %q: tools/list failed: %v", a.manifest.Name, err)
		a.healthy.Store(true) // process is running, just no tools
		return nil
	}

	a.tools = a.parseToolsResponse(toolsResp)
	a.healthy.Store(true)
	return nil
}

func (a *LocalMCPPluginAdapter) Stop(_ context.Context) error {
	a.healthy.Store(false)
	a.killProcess()
	return nil
}

func (a *LocalMCPPluginAdapter) Tools() []ToolDefinition {
	if !a.healthy.Load() {
		return nil
	}
	return a.tools
}

func (a *LocalMCPPluginAdapter) Health() HealthStatus {
	if a.healthy.Load() {
		return HealthStatus{Status: PluginHealthHealthy}
	}
	return HealthStatus{Status: PluginHealthUnhealthy}
}

// ---------- JSON-RPC helpers ----------

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      *int64      `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *int64           `json:"id,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *json.RawMessage `json:"error,omitempty"`
}

func (a *LocalMCPPluginAdapter) rpcCall(method string, params interface{}) (json.RawMessage, error) {
	id := atomic.AddInt64(&a.nextID, 1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	// Write under lock (fast, non-blocking).
	a.mu.Lock()
	if a.stdin == nil || a.reader == nil {
		a.mu.Unlock()
		return nil, fmt.Errorf("process not running")
	}
	if _, err := a.stdin.Write(data); err != nil {
		a.mu.Unlock()
		return nil, fmt.Errorf("write: %w", err)
	}
	reader := a.reader
	a.mu.Unlock()

	// Read response outside the lock. Use a channel + goroutine to enforce timeout
	// since bufio.Reader.ReadBytes is blocking and has no deadline support.
	type readResult struct {
		line []byte
		err  error
	}
	lineCh := make(chan readResult, 1)

	deadline := time.After(30 * time.Second)
	for {
		// Launch a goroutine for each read attempt.
		go func() {
			line, err := reader.ReadBytes('\n')
			lineCh <- readResult{line, err}
		}()

		select {
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for response to %s", method)
		case r := <-lineCh:
			if r.err != nil {
				return nil, fmt.Errorf("read: %w", r.err)
			}
			line := []byte(strings.TrimSpace(string(r.line)))
			if len(line) == 0 {
				continue
			}

			var resp jsonRPCResponse
			if err := json.Unmarshal(line, &resp); err != nil {
				continue // skip malformed lines
			}

			// Skip notifications (no ID).
			if resp.ID == nil {
				continue
			}
			if *resp.ID != id {
				continue
			}

			if resp.Error != nil {
				return nil, fmt.Errorf("RPC error: %s", string(*resp.Error))
			}
			return resp.Result, nil
		}
	}
}

func (a *LocalMCPPluginAdapter) rpcNotify(method string, params interface{}) error {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.stdin == nil {
		return fmt.Errorf("process not running")
	}

	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = a.stdin.Write(data)
	return err
}

func (a *LocalMCPPluginAdapter) killProcess() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.stdin != nil {
		a.stdin.Close()
		a.stdin = nil
	}
	if a.cmd != nil && a.cmd.Process != nil {
		coretool.TerminateCommandTree(a.cmd)
		a.cmd.Wait()
		a.cmd = nil
	}
}

// parseToolsResponse extracts ToolDefinitions from the MCP tools/list result.
func (a *LocalMCPPluginAdapter) parseToolsResponse(result json.RawMessage) []ToolDefinition {
	var resp struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		log.Printf("WARN: local_mcp plugin %q: parse tools response: %v", a.manifest.Name, err)
		return nil
	}

	defs := make([]ToolDefinition, 0, len(resp.Tools))
	for _, t := range resp.Tools {
		toolName := t.Name
		defs = append(defs, ToolDefinition{
			Name:        toolName,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Handler:     a.makeToolHandler(toolName),
		})
	}
	return defs
}

// makeToolHandler returns a handler that calls the MCP tool via JSON-RPC.
func (a *LocalMCPPluginAdapter) makeToolHandler(toolName string) func(args map[string]interface{}) (string, error) {
	return func(args map[string]interface{}) (string, error) {
		result, err := a.rpcCall("tools/call", map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		})
		if err != nil {
			return "", err
		}

		// MCP tools/call returns { content: [{type, text}] }
		var callResp struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		}
		if err := json.Unmarshal(result, &callResp); err != nil {
			return string(result), nil
		}

		var texts []string
		for _, c := range callResp.Content {
			if c.Text != "" {
				texts = append(texts, c.Text)
			}
		}
		output := strings.Join(texts, "\n")
		if callResp.IsError {
			return "", fmt.Errorf("tool error: %s", output)
		}
		return output, nil
	}
}
