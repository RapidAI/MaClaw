package main

// agent_handler_bridge.go registers gui.IMMessageHandler as the implementation
// of corelib/agent.Handler. This allows TUI (and any other package) to create
// a Handler via agent.NewHandler(cfg) without importing gui/ directly.
//
// See docs/agent-unification-design.md Phase 1, Section 4.1.2.

import (
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func init() {
	agent.RegisterHandlerFactory(func(cfg agent.Config) agent.Handler {
		// Convert agent.Config → gui.StandaloneConfig
		sc := StandaloneConfig{
			WorkflowEngine:        cfg.WorkflowEngine,
			SteeringStore:         cfg.SteeringStore,
			MemoryStore:           cfg.MemoryStore,
			UsageTracker:          cfg.UsageTracker,
			LLMConfigFunc:         cfg.LLMConfigFunc,
			MaxIterationsFunc:     cfg.MaxIterationsFunc,
			IsProMode:             cfg.IsProMode,
			ConversationStorePath: cfg.ConversationStorePath,
			ConfirmationStorePath: cfg.ConfirmationStorePath,
		}
		if cfg.ToolRouter != nil {
			sc.ToolRouter = cfg.ToolRouter
		}
		// Type-assert SSHManager from interface{} to concrete type.
		if cfg.SSHManager != nil {
			if mgr, ok := cfg.SSHManager.(*remote.SSHSessionManager); ok {
				sc.SSHManager = mgr
			}
		}

		h := NewIMMessageHandlerStandalone(sc)
		return &handlerAdapter{h: h}
	})
}

// handlerAdapter wraps *IMMessageHandler to implement agent.Handler.
type handlerAdapter struct {
	h *IMMessageHandler
}

func (a *handlerAdapter) HandleMessage(msg agent.UserMessage) *agent.Response {
	resp := a.h.HandleIMMessage(toIMUserMessage(msg))
	return toAgentResponse(resp)
}

func (a *handlerAdapter) HandleMessageWithProgress(msg agent.UserMessage, onProgress agent.ProgressCallback) *agent.Response {
	var guiProgress tool.ProgressCallback
	if onProgress != nil {
		guiProgress = func(text string) { onProgress(text) }
	}
	resp := a.h.HandleIMMessageWithProgress(toIMUserMessage(msg), guiProgress)
	return toAgentResponse(resp)
}

func (a *handlerAdapter) HandleMessageWithStream(
	msg agent.UserMessage,
	onProgress agent.ProgressCallback,
	onToken agent.TokenCallback,
	onNewRound agent.NewRoundCallback,
	onStreamDone agent.StreamDoneCallback,
) *agent.Response {
	var guiProgress tool.ProgressCallback
	var guiToken llm.TokenCallback
	var guiNewRound NewRoundCallback
	var guiStreamDone StreamDoneCallback
	if onProgress != nil {
		guiProgress = func(text string) { onProgress(text) }
	}
	if onToken != nil {
		guiToken = func(delta string) { onToken(delta) }
	}
	if onNewRound != nil {
		guiNewRound = func() { onNewRound() }
	}
	if onStreamDone != nil {
		guiStreamDone = func() { onStreamDone() }
	}
	resp := a.h.HandleIMMessageWithProgressAndStream(
		toIMUserMessage(msg), guiProgress, guiToken, guiNewRound, guiStreamDone,
	)
	return toAgentResponse(resp)
}

func (a *handlerAdapter) Stop() {
	if a.h.memory != nil {
		a.h.memory.Stop()
	}
}

// --- Type conversion helpers ---

func toIMUserMessage(msg agent.UserMessage) IMUserMessage {
	// IMUserMessage is now an alias for agent.UserMessage — no conversion needed.
	return msg
}

func toAgentResponse(resp *IMAgentResponse) *agent.Response {
	if resp == nil {
		return &agent.Response{}
	}
	return &agent.Response{
		Text:     resp.Text,
		Error:    resp.Error,
		ImageKey: resp.ImageKey,
		HardExit: resp.HardExit,
	}
}
