// Package codingagent contains the host-neutral coding-agent contract shared
// by GUI, TUI and MaClawSrv. It deliberately does not know about Wails, SSH,
// filesystems or a particular LLM provider: each host supplies those through
// agent.LoopCallbacks.
package codingagent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

// Role describes the authority level of a coding agent. Worker is reserved
// for a future isolated-write runtime; Explorer and Reviewer are read-only.
type Role string

const (
	RoleWorker   Role = "worker"
	RoleExplorer Role = "explorer"
	RoleReviewer Role = "reviewer"
)

// ParseRole normalizes public role aliases used by host tool schemas.
func ParseRole(raw string) (Role, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "worker", "implement", "coder", "default":
		return RoleWorker, nil
	case "explorer", "explore", "research", "search":
		return RoleExplorer, nil
	case "reviewer", "review", "verify", "qa":
		return RoleReviewer, nil
	default:
		return "", fmt.Errorf("unknown coding agent role %q (supported: explorer, worker, reviewer)", raw)
	}
}

// ReadOnly reports whether a role is safe to admit as a read-only child.
func (r Role) ReadOnly() bool { return r == RoleExplorer || r == RoleReviewer }

// ToolPolicy binds a host-provided allow-list to a role. Tool definitions are
// advisory, so hosts must enforce this same policy at execution time too.
type ToolPolicy struct {
	Role      Role
	Allowed   map[string]bool
	Normalize func(string) string
}

// IsToolAllowed lets ToolPolicy serve directly as agent.ToolAuthorizer. This
// keeps the model-facing definition filter and the execution-time boundary on
// the same policy object.
func (p ToolPolicy) IsToolAllowed(name string) bool { return p.Allows(name) }

// Allows fails closed for inspection roles with no allow-list. A worker with
// no list retains the host-defined full tool surface.
func (p ToolPolicy) Allows(name string) bool {
	role := p.Role
	if role == "" {
		role = RoleWorker
	}
	if p.Normalize != nil {
		name = p.Normalize(name)
	} else {
		name = strings.TrimSpace(name)
	}
	if p.Allowed == nil {
		return role == RoleWorker
	}
	return p.Allowed[name]
}

// FilterToolDefinitions removes unavailable function definitions before they
// reach the model.
func (p ToolPolicy) FilterToolDefinitions(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		fn, _ := tool["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if p.Allows(name) {
			out = append(out, tool)
		}
	}
	return out
}

// IsToolCallAllowed applies the role allow-list at the concrete invocation
// boundary. Tool definitions are only advisory: in particular web_fetch can
// become a filesystem write when a host accepts a destination argument. The
// common aliases are denied here so GUI, TUI and service adapters cannot
// accidentally make an inspection child writable as their web tool evolves.
func (p ToolPolicy) IsToolCallAllowed(name string, args map[string]interface{}) (bool, string) {
	if !p.Allows(name) {
		return false, strings.TrimSpace(name) + " is not allowed for this coding-agent role"
	}
	canonical := strings.TrimSpace(name)
	if p.Normalize != nil {
		canonical = p.Normalize(canonical)
	}
	if p.Role.ReadOnly() && strings.EqualFold(canonical, "web_fetch") {
		for _, key := range []string{"save_path", "output", "dest", "path", "filename"} {
			if value, ok := args[key]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
				return false, "web_fetch " + key + " is not allowed for a read-only coding child"
			}
		}
	}
	return true, ""
}

// Run executes a coding-agent turn using the shared core agent loop. It
// centralizes text/multimodal dispatch while hosts own prompts, tools,
// authorization, transport and presentation callbacks.
func Run(callbacks agent.LoopCallbacks, userText string, userContent interface{}, history []agent.ConversationEntry, client *http.Client, hooks agent.LoopHooks) agent.LoopResult {
	if userContent != nil && userContent != userText {
		return agent.RunLoopWithUserContent(callbacks, userText, userContent, history, client, hooks)
	}
	return agent.RunLoop(callbacks, userText, history, client, hooks)
}

// LoopRunner is the host-owned bridge from a durable runtime attempt to one
// coding-agent loop. It receives a fresh execution request and must not use a
// prior attempt's tool arguments or command output as a replay source.
//
// GUI, TUI, and MaClawSrv retain ownership of LoopCallbacks, tools, identity,
// cancellation wiring, and presentation. The core adapter only normalizes the
// bounded terminal fact for codingruntime.
type LoopRunner func(context.Context, codingruntime.ExecutionRequest) agent.LoopResult

// LoopExecutor adapts a host-owned RunLoop invocation to codingruntime.Executor
// without importing GUI, SSH, TUI, or service packages. It intentionally does
// not retain raw model/tool output: the ledger gets only a status, a generic
// error summary, and hashed aggregate usage facts.
type LoopExecutor struct {
	Run LoopRunner
}

// Execute implements codingruntime.Executor. A host that needs stronger
// completion evidence (for example an isolated writer's diff/merge gate) must
// provide its own adapter, because that evidence belongs to the host's file or
// transport layer rather than the generic model loop.
func (e LoopExecutor) Execute(ctx context.Context, request codingruntime.ExecutionRequest) codingruntime.ExecutionResult {
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ExecutionResult{
			Status:          codingruntime.TaskInterrupted,
			SideEffectState: codingruntime.SideEffectUncertain,
			ErrorCode:       "cancelled_before_agent_loop",
			ErrorSummary:    "coding-agent loop was cancelled before it started",
		}
	}
	if e.Run == nil {
		return codingruntime.ExecutionResult{
			Status:       codingruntime.TaskFailed,
			ErrorCode:    "nil_agent_loop_runner",
			ErrorSummary: "coding-agent host runner is unavailable",
		}
	}

	result := e.Run(ctx, request)
	if ctx != nil && ctx.Err() != nil {
		return codingruntime.ExecutionResult{
			Status:          codingruntime.TaskInterrupted,
			SideEffectState: codingruntime.SideEffectUncertain,
			ErrorCode:       "cancelled_during_agent_loop",
			ErrorSummary:    "coding-agent loop was cancelled; side effects require a read-only recovery probe",
		}
	}

	evidence := []codingruntime.Evidence{{Type: "agent_loop_usage", Digest: loopResultDigest(result)}}
	sideEffects := codingruntime.SideEffectNone
	if result.ToolCalls > 0 {
		// A generic RunLoop cannot establish whether a given tool mutates a
		// workspace. Observed is deliberately weaker than confirmed; concrete
		// adapters can report stronger evidence after host-side verification.
		sideEffects = codingruntime.SideEffectObserved
		evidence = append(evidence, codingruntime.Evidence{Type: "agent_loop_tool_activity", Digest: loopActivityDigest(result.ToolCalls)})
	}
	if result.HardExit {
		if result.ToolCalls > 0 {
			sideEffects = codingruntime.SideEffectUncertain
		}
		return codingruntime.ExecutionResult{
			Status:          codingruntime.TaskFailed,
			SideEffectState: sideEffects,
			ErrorCode:       "agent_loop_hard_exit",
			ErrorSummary:    "coding-agent loop exited abnormally; inspect host-local diagnostics",
			Evidence:        evidence,
		}
	}
	if result.AskUser != nil || result.RecordAudio != nil {
		return codingruntime.ExecutionResult{
			Status:          codingruntime.TaskBlocked,
			SideEffectState: sideEffects,
			ErrorCode:       "agent_loop_waiting_for_user",
			ErrorSummary:    "coding-agent loop requires explicit user input before a new attempt",
			Evidence:        evidence,
		}
	}
	if strings.TrimSpace(result.Error) != "" {
		if isLoopCancellationError(result.Error) {
			return codingruntime.ExecutionResult{
				Status:          codingruntime.TaskInterrupted,
				SideEffectState: codingruntime.SideEffectUncertain,
				ErrorCode:       "agent_loop_cancelled",
				ErrorSummary:    "coding-agent loop was cancelled; side effects require a read-only recovery probe",
				Evidence:        evidence,
			}
		}
		if result.ToolCalls > 0 {
			sideEffects = codingruntime.SideEffectUncertain
		}
		return codingruntime.ExecutionResult{
			Status:          codingruntime.TaskFailed,
			SideEffectState: sideEffects,
			ErrorCode:       "agent_loop_failed",
			// Provider and tool errors can contain paths, request payloads, or
			// host details. Keep those in host diagnostics, not in Ledger.
			ErrorSummary: "coding-agent loop failed; inspect host-local diagnostics",
			Evidence:     evidence,
		}
	}
	return codingruntime.ExecutionResult{
		Status:          codingruntime.TaskCompleted,
		SideEffectState: sideEffects,
		Evidence:        evidence,
	}
}

func isLoopCancellationError(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "cancelled" || strings.HasPrefix(value, "cancelled ") || strings.HasPrefix(value, "canceled ") || value == "canceled"
}

func loopResultDigest(result agent.LoopResult) string {
	return loopDigest(fmt.Sprintf("iterations=%d|tools=%d|hard_exit=%t|usage=%d:%d", result.Iterations, result.ToolCalls, result.HardExit, result.Usage.InputTokens, result.Usage.OutputTokens))
}

func loopActivityDigest(toolCalls int) string {
	return loopDigest(fmt.Sprintf("tool_calls=%d", toolCalls))
}

func loopDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum[:])
}
