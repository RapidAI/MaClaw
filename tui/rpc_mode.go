package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agent/sshtool"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/goal"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// ─────────────────────────────────────────────────────────────────────────────
// RPC Mode — stdin/stdout JSONL protocol for process integration.

// parseRPCModeFlag checks if --mode rpc was passed on the command line.
func parseRPCModeFlag() bool {
	for i, arg := range os.Args[1:] {
		if arg == "--mode" && i+1 < len(os.Args)-1 && os.Args[i+2] == "rpc" {
			return true
		}
		if arg == "--mode=rpc" {
			return true
		}
	}
	return false
}

// RPCRequest is a client → server message.
type RPCRequest struct {
	Type string `json:"type"`           // "prompt" | "abort" | "shutdown"
	ID   string `json:"id,omitempty"`   // client-assigned request ID
	Text string `json:"text,omitempty"` // prompt text (for "prompt" type)
}

// RPCEvent is a server → client message.
type RPCEvent struct {
	Type      string      `json:"type"`                 // "ready" | "text_delta" | "tool_call" | "tool_result" | "done" | "error" | "shutdown_ack"
	RequestID string      `json:"request_id,omitempty"` // echoes the request ID
	Delta     string      `json:"delta,omitempty"`      // for text_delta
	Name      string      `json:"name,omitempty"`       // tool name
	Args      interface{} `json:"args,omitempty"`       // tool call arguments
	Result    string      `json:"result,omitempty"`     // tool result
	Text      string      `json:"text,omitempty"`       // full response text (for done)
	Message   string      `json:"message,omitempty"`    // error message
	Usage     *RPCUsage   `json:"usage,omitempty"`      // token usage (for done)
}

// RPCUsage contains token usage information for a completed request.
type RPCUsage struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

// runRPCMode starts the RPC server loop.
func runRPCMode() {
	// Redirect all log output to stderr (stdout is protocol-only).
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	log.Printf("[rpc] starting %s RPC mode", brand.Current().DisplayName)

	// --- Initialize infrastructure (same as pipe mode) ---
	dataDir := commands.ResolveDataDir()
	os.MkdirAll(dataDir, 0755)

	configStore := commands.NewFileConfigStore(dataDir)
	appCfg, err := configStore.LoadConfig()
	if err != nil {
		log.Printf("[rpc] config load warning: %v", err)
	}

	llmCfg := buildLLMConfigFromAppConfig(appCfg)
	if !tuiConfigLLMReady(appCfg) {
		emitRPCEvent(RPCEvent{Type: "error", Message: "LLM not configured. Run setup or configure via GUI first."})
		os.Exit(1)
	}

	// Initialize stores.
	memStore, _ := memory.OpenDataDirStore(dataDir, memory.StoreModeAuto)
	sshMgr := remote.NewSSHSessionManager(nil)
	steeringDir := filepath.Join(corelib.MaclawBaseDir(), "steering")
	steeringStore := steering.NewStore(steeringDir, "")
	steeringStore.Load()

	dataSubDir := filepath.Join(dataDir, "data")
	os.MkdirAll(dataSubDir, 0755)

	// Initialize credential store for OAuth token refresh.
	credStore := oauth.NewFileCredentialStore(oauth.DefaultCredentialStorePath())

	app := &TUIApp{
		logger:        NewTUILogger(),
		llmConfig:     llmCfg,
		memoryStore:   memStore,
		sshMgr:        sshMgr,
		steeringStore: steeringStore,
		appConfig:     appCfg,
		history:       agent.NewPersistentConversationMemory(filepath.Join(dataSubDir, "rpc_conversation.json")),
		taskStore:     task.NewStore(),
		toolRegistry:  agent.NewCoreToolRegistry(),
	}
	_ = credStore // available for future OAuth token refresh in RPC mode

	// Register tools.
	rpcBGTaskMgr := remote.NewSSHBackgroundTaskManager(sshMgr)
	rpcBGTaskMgr.SetPersistDir(dataSubDir)
	sshHandler := func(args map[string]interface{}) string {
		deps := sshtool.SSHToolDeps{
			Manager:       sshMgr,
			BGTaskMgr:     rpcBGTaskMgr,
			PolicyOwnerID: "tui:rpc",
			HostLoader: func() []corelib.SSHHostEntry {
				return app.appConfig.SSHHosts
			},
			OnConnected: func(session *remote.SSHManagedSession, cfg remote.SSHHostConfig) {
				rpcBGTaskMgr.RediscoverOrphanTasksForOwner(session.ID, "tui:rpc")
			},
		}
		return sshtool.ToolSSH(deps, args)
	}
	agent.RegisterCoreTools(app.toolRegistry, agent.CoreToolDeps{
		MemoryStore: memStore,
		TaskStore:   app.taskStore,
		GoalStore:   goal.NewStore(filepath.Join(dataSubDir, "goals")),
		SecurityGuard: tuiSecurityGuard(func() corelib.AppConfig {
			return app.appConfig
		}),
		SSHHandler: sshHandler,
		WebSearchHandlerCtx: func(ctx context.Context, args map[string]interface{}) string {
			var provider corelib.WebSearchProvider
			if len(app.appConfig.WebSearchProviders) > 0 {
				for _, p := range app.appConfig.WebSearchProviders {
					if p.Name == app.appConfig.WebSearchCurrentProvider {
						provider = p
						break
					}
				}
				if provider.Name == "" {
					provider = app.appConfig.WebSearchProviders[0]
				}
			}
			if provider.Type == "" {
				provider.Type = "duckduckgo"
			}
			return agent.ToolWebSearchCtx(ctx, provider, args)
		},
		WebFetchHandlerCtx: func(ctx context.Context, args map[string]interface{}) string {
			var provider corelib.WebSearchProvider
			if len(app.appConfig.WebSearchProviders) > 0 {
				for _, p := range app.appConfig.WebSearchProviders {
					if p.Name == app.appConfig.WebSearchCurrentProvider {
						provider = p
						break
					}
				}
				if provider.Name == "" {
					provider = app.appConfig.WebSearchProviders[0]
				}
			}
			return agent.ToolWebFetchWithProviderCtx(ctx, args, provider)
		},
	})

	// --- Signal handling ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		log.Printf("[rpc] received interrupt, shutting down")
		emitRPCEvent(RPCEvent{Type: "shutdown_ack"})
		os.Exit(0)
	}()

	// --- Emit ready event ---
	emitRPCEvent(RPCEvent{Type: "ready"})
	log.Printf("[rpc] ready, awaiting requests on stdin")

	// --- Main request loop ---
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line buffer

	var activeCancel context.CancelFunc
	var activeMu sync.Mutex
	var activeGeneration uint64

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var req RPCRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			emitRPCEvent(RPCEvent{Type: "error", Message: fmt.Sprintf("invalid JSON: %v", err)})
			continue
		}

		switch req.Type {
		case "prompt":
			if strings.TrimSpace(req.Text) == "" {
				emitRPCEvent(RPCEvent{Type: "error", RequestID: req.ID, Message: "empty prompt"})
				continue
			}

			// Cancel any previous request.
			activeMu.Lock()
			if activeCancel != nil {
				activeCancel()
			}
			ctx, cancel := context.WithCancel(context.Background())
			activeCancel = cancel
			activeGeneration++
			gen := activeGeneration
			activeMu.Unlock()

			// Execute in goroutine to allow abort.
			go func(reqID, text string, ctx context.Context, myGen uint64) {
				executeRPCPrompt(app, reqID, text, ctx)
				// Only clear activeCancel if we are still the active generation.
				// Prevents a stale goroutine from nil-ing a newer request's cancel.
				activeMu.Lock()
				if activeGeneration == myGen {
					activeCancel = nil
				}
				activeMu.Unlock()
			}(req.ID, req.Text, ctx, gen)

		case "abort":
			activeMu.Lock()
			if activeCancel != nil {
				activeCancel()
				activeCancel = nil
			}
			activeMu.Unlock()
			emitRPCEvent(RPCEvent{Type: "done", RequestID: req.ID, Message: "aborted"})

		case "shutdown":
			emitRPCEvent(RPCEvent{Type: "shutdown_ack"})
			log.Printf("[rpc] shutdown requested")
			os.Exit(0)

		default:
			emitRPCEvent(RPCEvent{Type: "error", RequestID: req.ID, Message: fmt.Sprintf("unknown request type: %s", req.Type)})
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		log.Printf("[rpc] stdin read error: %v", err)
	}
	log.Printf("[rpc] stdin closed, exiting")
}

// executeRPCPrompt runs a single agent loop and emits events.
func executeRPCPrompt(app *TUIApp, requestID, text string, ctx context.Context) {
	startedAt := time.Now()
	log.Printf("[rpc] prompt request_id=%s text_len=%d", requestID, len([]rune(text)))

	cb := &rpcCallbacks{
		app:       app,
		requestID: requestID,
		ctx:       ctx,
	}

	result := agent.RunLoop(cb, text, nil, nil)

	elapsed := time.Since(startedAt)
	if result.Error != "" {
		emitRPCEvent(RPCEvent{Type: "error", RequestID: requestID, Message: result.Error})
		log.Printf("[rpc] prompt failed request_id=%s elapsed=%s error=%s", requestID, elapsed, result.Error)
		return
	}

	usage := &RPCUsage{
		InputTokens:  0, // LoopResult doesn't expose per-request token counts
		OutputTokens: 0, // Cost tracking is done at the CostTracker level
	}

	emitRPCEvent(RPCEvent{
		Type:      "done",
		RequestID: requestID,
		Text:      strings.TrimSpace(result.Text),
		Usage:     usage,
	})
	log.Printf("[rpc] prompt done request_id=%s elapsed=%s iterations=%d tool_calls=%d",
		requestID, elapsed, result.Iterations, result.ToolCalls)
}

// emitRPCEvent writes a single JSON event to stdout (LF-terminated).
// Thread-safe via stdout mutex.
var stdoutMu sync.Mutex

func emitRPCEvent(event RPCEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[rpc] marshal event error: %v", err)
		return
	}
	stdoutMu.Lock()
	os.Stdout.Write(data)
	os.Stdout.Write([]byte{'\n'})
	stdoutMu.Unlock()
}

// ─────────────────────────────────────────────────────────────────────────────
// rpcCallbacks implements agent.LoopCallbacks for RPC mode.
// Emits JSONL events to stdout for each agent loop lifecycle event.
// ─────────────────────────────────────────────────────────────────────────────

type rpcCallbacks struct {
	app       *TUIApp
	requestID string
	ctx       context.Context
}

func (c *rpcCallbacks) Cancel() {
	// No-op: cancellation is done via ctx.
}

func (c *rpcCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.app.llmConfig
}

func (c *rpcCallbacks) GetMaxIterations() int {
	return 80
}

func (c *rpcCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	var sb strings.Builder
	sb.WriteString("You are a helpful coding assistant running in RPC mode. ")
	sb.WriteString("You have access to tools for reading/writing files, running commands, and searching. ")
	sb.WriteString("Be concise and action-oriented.")

	// Append steering rules if available.
	if c.app.steeringStore != nil {
		ctx := steering.ResolveContext{
			UserMessage:  userText,
			ContextFiles: nil,
		}
		if resolved := c.app.steeringStore.Resolve(ctx); len(resolved) > 0 {
			sb.WriteString("\n\n")
			for _, r := range resolved {
				sb.WriteString(r.Content)
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

func (c *rpcCallbacks) BuildTools(userText string) []map[string]interface{} {
	if c.app.toolRegistry == nil {
		return nil
	}
	return c.app.toolRegistry.BuildDefinitions()
}

func (c *rpcCallbacks) ExecuteTool(name, argsJSON string) string {
	if c.app.toolRegistry == nil {
		return `{"error":"no tool registry"}`
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		args = map[string]interface{}{"_raw": argsJSON}
	}
	return c.app.toolRegistry.Execute(name, args)
}

func (c *rpcCallbacks) IsToolAllowed(name string) bool {
	return true // RPC mode trusts the caller.
}

func (c *rpcCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	return true, ""
}

func (c *rpcCallbacks) OnToken(delta string) {
	if delta != "" {
		emitRPCEvent(RPCEvent{Type: "text_delta", RequestID: c.requestID, Delta: delta})
	}
}

func (c *rpcCallbacks) OnProgress(text string) {
	// Progress messages go to stderr only (not part of protocol).
	log.Printf("[rpc] progress request_id=%s: %s", c.requestID, text)
}

func (c *rpcCallbacks) OnToolCall(name string) {
	emitRPCEvent(RPCEvent{Type: "tool_call", RequestID: c.requestID, Name: name})
}

func (c *rpcCallbacks) OnToolResult(name string) {
	emitRPCEvent(RPCEvent{Type: "tool_result", RequestID: c.requestID, Name: name})
}

func (c *rpcCallbacks) ShouldStop() bool {
	select {
	case <-c.ctx.Done():
		return true
	default:
		return false
	}
}
