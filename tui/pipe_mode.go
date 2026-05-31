package main

// pipe_mode.go implements the -p / --prompt non-interactive mode.
//
// Usage:
//   maclaw-tui -p "分析这个项目结构，返回纯JSON"
//   maclaw-tui -p "返回{name:'maclaw',version:'1.0'}纯JSON" | jq .version
//   echo "some data" | maclaw-tui -p "summarize this"
//
// This mode initializes the full agent infrastructure (LLM, tools, memory,
// steering) without any Bubble Tea UI, runs a single agent loop, prints the
// result to stdout, and exits. Diagnostic messages go to stderr so stdout
// remains clean for piping.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agent/sshtool"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/task"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// parsePipePromptFlag checks if the FIRST argument is -p or --prompt and
// returns the prompt text. Returns "" if the flag is not present.
//
// The flag is only recognized at position 1 to avoid conflicts with
// subcommands that might have their own -p flags.
//
// Supported forms:
//
//	-p "prompt text"
//	--prompt "prompt text"
//	-p="prompt text"
//	--prompt="prompt text"
func parsePipePromptFlag() string {
	if len(os.Args) < 2 {
		return ""
	}
	arg := os.Args[1]

	switch arg {
	case "-p", "--prompt":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "error: -p/--prompt requires a prompt argument")
			os.Exit(commands.ExitUsage)
		}
		return os.Args[2]
	}

	if strings.HasPrefix(arg, "-p=") {
		return arg[3:]
	}
	if strings.HasPrefix(arg, "--prompt=") {
		return arg[9:]
	}
	return ""
}

// runPrompt executes a single prompt non-interactively and prints the result
// to stdout. All diagnostic/progress messages go to stderr.
func runPrompt(promptText string) {
	if strings.TrimSpace(promptText) == "" {
		fmt.Fprintln(os.Stderr, "error: prompt cannot be empty")
		os.Exit(commands.ExitUsage)
	}

	// --- Initialize infrastructure (same as runTUIWithOptions but no UI) ---
	dataDir := commands.ResolveDataDir()
	os.MkdirAll(dataDir, 0755)

	// Load config.
	configStore := commands.NewFileConfigStore(dataDir)
	appCfg, err := configStore.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: config load failed, using defaults: %v\n", err)
	}

	// Check LLM is configured.
	llmCfg := buildLLMConfigFromAppConfig(appCfg)
	if !tuiConfigLLMReady(appCfg) {
		cliName := strings.ToLower(brand.Current().DisplayName) + "-tui"
		fmt.Fprintf(os.Stderr, "error: LLM not configured. Run '%s setup' or configure via GUI first.\n", cliName)
		os.Exit(1)
	}

	// Initialize memory store through the shared corelib data-dir convention.
	memStore, _ := memory.OpenDataDirStore(dataDir, memory.StoreModeAuto)

	// Initialize SSH manager.
	sshMgr := remote.NewSSHSessionManager(nil)

	// Initialize steering store.
	home, _ := os.UserHomeDir()
	steeringDir := filepath.Join(home, ".maclaw", "steering")
	steeringStore := steering.NewStore(steeringDir, "")
	steeringStore.Load()

	// Ensure data subdirectory exists for conversation file.
	dataSubDir := filepath.Join(dataDir, "data")
	os.MkdirAll(dataSubDir, 0755)

	// Build the app (no UI components).
	app := &TUIApp{
		logger:        NewTUILogger(),
		llmConfig:     llmCfg,
		memoryStore:   memStore,
		sshMgr:        sshMgr,
		steeringStore: steeringStore,
		appConfig:     appCfg,
		// Pipe mode is stateless — no history carried between runs.
		history:      agent.NewPersistentConversationMemory(filepath.Join(dataSubDir, "pipe_conversation.json")),
		taskStore:    task.NewStore(),
		toolRegistry: agent.NewCoreToolRegistry(),
	}

	// Register tools.
	sshHandler := func(args map[string]interface{}) string {
		deps := sshtool.SSHToolDeps{
			Manager: sshMgr,
			HostLoader: func() []corelib.SSHHostEntry {
				return app.appConfig.SSHHosts
			},
		}
		return sshtool.ToolSSH(deps, args)
	}
	agent.RegisterCoreTools(app.toolRegistry, agent.CoreToolDeps{
		MemoryStore: memStore,
		TaskStore:   app.taskStore,
		SecurityGuard: tuiSecurityGuard(func() corelib.AppConfig {
			return app.appConfig
		}),
		SSHHandler: sshHandler,
		WebSearchHandler: func(args map[string]interface{}) string {
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
			return agent.ToolWebSearch(provider, args)
		},
		WebFetchHandler: func(args map[string]interface{}) string {
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
			return agent.ToolWebFetchWithProvider(args, provider)
		},
	})

	// --- Read stdin if available (piped input) ---
	finalPrompt := promptText
	if stdinHasData() {
		stdinData, err := io.ReadAll(os.Stdin)
		if err == nil && len(stdinData) > 0 {
			finalPrompt = fmt.Sprintf("Input data:\n```\n%s\n```\n\nTask: %s",
				strings.TrimSpace(string(stdinData)), promptText)
		}
	}

	// --- Run agent loop ---
	cb := &pipeCallbacks{
		app:      app,
		cancelCh: make(chan struct{}),
	}

	// Handle Ctrl+C gracefully.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		cb.Cancel()
	}()

	result := agent.RunLoop(cb, finalPrompt, nil, nil)

	// Stop signal handler to avoid goroutine leak.
	signal.Stop(sigCh)

	// --- Output result ---
	if result.Error != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", result.Error)
		os.Exit(1)
	}

	// Cancelled by user (Ctrl+C) — exit non-zero.
	if cb.stopped {
		fmt.Fprintln(os.Stderr, "interrupted")
		os.Exit(130) // Standard SIGINT exit code.
	}

	text := strings.TrimSpace(result.Text)
	if text != "" {
		fmt.Println(text)
	}
}

// stdinHasData checks if stdin has piped data (not a terminal).
func stdinHasData() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// ---------------------------------------------------------------------------
// pipeCallbacks implements agent.LoopCallbacks for non-interactive pipe mode.
// Final result goes to stdout via RunLoop return value.
// Progress/diagnostics go to stderr.
// ---------------------------------------------------------------------------

type pipeCallbacks struct {
	app      *TUIApp
	cancelCh chan struct{}
	stopped  bool
	quiet    bool
}

func silencePipeModeDiagnostics() func() {
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	originalStderr := os.Stderr
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		log.SetOutput(io.Discard)
		return func() {
			log.SetOutput(originalWriter)
			log.SetFlags(originalFlags)
			os.Stderr = originalStderr
		}
	}
	log.SetOutput(devNull)
	log.SetFlags(0)
	os.Stderr = devNull
	return func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
		os.Stderr = originalStderr
		_ = devNull.Close()
	}
}

func (c *pipeCallbacks) Cancel() {
	select {
	case <-c.cancelCh:
	default:
		close(c.cancelCh)
		c.stopped = true
	}
}

func (c *pipeCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.app.llmConfig
}

func (c *pipeCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(c.app.appConfig.MaclawAgentMaxIterations)
}

func (c *pipeCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	deps := c.app.buildSystemPromptDeps()
	return agent.BuildSystemPrompt(deps, userText, isFirstTurn)
}

func (c *pipeCallbacks) BuildTools(userText string) []map[string]interface{} {
	return c.app.toolRegistry.BuildDefinitions()
}

func (c *pipeCallbacks) ExecuteTool(name, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("tool arg parse failed: %v", err)
	}
	if !c.quiet {
		fmt.Fprintf(os.Stderr, "⚙ %s\n", name)
	}
	return c.app.toolRegistry.Execute(name, args)
}

func (c *pipeCallbacks) IsToolAllowed(name string) bool {
	if c == nil || c.app == nil {
		return true
	}
	return c.app.isWorkflowToolAllowedTUI(name)
}

func (c *pipeCallbacks) IsToolCallAllowed(name, argsJSON string) (bool, string) {
	if c == nil || c.app == nil {
		return true, ""
	}
	return c.app.isWorkflowToolCallAllowedTUI(name, argsJSON)
}

func (c *pipeCallbacks) OnToken(delta string) {
	// In pipe mode, tokens are NOT streamed to stdout — that would corrupt
	// the final output for piping. The complete result is printed at the end.
}

func (c *pipeCallbacks) OnProgress(text string) {
	if c.quiet {
		return
	}
	fmt.Fprintf(os.Stderr, "%s\n", text)
}

func (c *pipeCallbacks) OnToolCall(name string) {
	// Already handled in ExecuteTool.
}

func (c *pipeCallbacks) OnToolResult(name string) {
	// No-op.
}

func (c *pipeCallbacks) ShouldStop() bool {
	select {
	case <-c.cancelCh:
		return true
	default:
		return c.stopped
	}
}

// Compile-time interface check.
var _ agent.LoopCallbacks = (*pipeCallbacks)(nil)
