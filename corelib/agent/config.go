package agent

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/steering"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// Config holds the components needed to construct a Handler.
// All fields are optional — nil fields disable the corresponding
// functionality gracefully.
type Config struct {
	// WorkflowEngine is the corelib workflow engine (19 templates, phase management).
	WorkflowEngine *v2.WorkflowEngine

	// SteeringStore provides declarative rule injection from ~/.maclaw/steering/.
	SteeringStore *steering.Store

	// MemoryStore is the long-term memory store (BM25 + vector recall).
	MemoryStore *memory.Store

	// ToolRouter handles dynamic tool selection via BM25/embedding scoring.
	ToolRouter *tool.Router

	// UsageTracker records tool usage for outcome learning.
	UsageTracker *tool.UsageTracker

	// SSHManager manages SSH sessions. Typed as interface{} to avoid
	// import cycle (corelib/remote imports corelib/agent). The gui/
	// factory asserts the concrete *remote.SSHSessionManager type.
	SSHManager interface{}

	// LLMConfigFunc returns the current LLM configuration.
	// Required — without this the agent cannot make LLM calls.
	LLMConfigFunc func() corelib.MaclawLLMConfig

	// MaxIterationsFunc returns the max agent loop iterations.
	// Defaults to 30 if nil.
	MaxIterationsFunc func() int

	// IsProMode controls whether coding session tools are available.
	// Defaults to true if nil.
	IsProMode *bool

	// ConversationStorePath is the file path for persisting conversation history.
	// Empty string uses in-memory only.
	ConversationStorePath string

	// ConfirmationStorePath is the file path for persisting confirmation state.
	// Empty string uses in-memory only.
	ConfirmationStorePath string
}
