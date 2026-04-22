package main

// agent_adapter.go bridges TUI's Bubble Tea I/O model to the unified
// agent.Handler interface (corelib/agent). The actual Handler implementation
// is registered by gui/ via agent.RegisterHandlerFactory at init time.
//
// For standalone TUI binary (without gui/ linked), TUI falls back to
// its existing TUIAgentHandler. The adapter is only used when the
// unified handler is available.
//
// See docs/agent-unification-design.md Phase 1.

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// newUnifiedAgentConfig builds an agent.Config from TUI's initialized components.
func (a *TUIApp) newUnifiedAgentConfig() agent.Config {
	cfg := agent.Config{
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			llmCfg, err := commands.LoadLLMConfig()
			if err != nil {
				return corelib.MaclawLLMConfig{}
			}
			return llmCfg
		},
		ConversationStorePath: "", // in-memory for now
	}

	if a.workflowEngine != nil {
		cfg.WorkflowEngine = a.workflowEngine
	}
	if a.memoryStore != nil {
		cfg.MemoryStore = a.memoryStore
	}
	if a.router != nil {
		cfg.ToolRouter = a.router
	}
	if a.usageTracker != nil {
		cfg.UsageTracker = a.usageTracker
	}
	if a.steeringStore != nil {
		cfg.SteeringStore = a.steeringStore
	}

	return cfg
}

// initUnifiedHandler creates the unified agent handler if the factory
// is registered (i.e., gui/ is linked into this binary). Returns false
// if the factory is not available (standalone TUI binary).
func (a *TUIApp) initUnifiedHandler() bool {
	if !agent.HasHandlerFactory() {
		return false
	}
	cfg := a.newUnifiedAgentConfig()
	a.unifiedHandler = agent.NewHandler(cfg)
	return true
}
