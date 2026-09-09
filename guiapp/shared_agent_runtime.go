package guiapp

import (
	"context"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// guiRuntimeContext is the minimal non-serializable host state retained by
// the compatibility adapter. Conversational content and callbacks travel via
// agentruntime.TurnInput; this payload no longer carries a second private turn
// DTO or duplicated prompt/history/attachment fields.
type guiRuntimeContext struct {
	ctx        *LoopContext
	userID     string
	events     agentruntime.EventSink
	onProgress tool.ProgressCallback
}

func runtimeScopeForLoop(ctx *LoopContext, userID, runID string) agentruntime.Scope {
	scope := agentruntime.Scope{UserID: strings.TrimSpace(userID), RunID: strings.TrimSpace(runID)}
	if ctx != nil {
		scope.SessionID = strings.TrimSpace(ctx.SessionID)
	}
	return scope
}

type guiSharedAgentRuntime struct {
	handler *IMMessageHandler
	// modules is optional during migration. When supplied by the composition
	// root, capability discovery uses the same registry as headless hosts.
	modules *agentruntime.ModuleRegistry
}

func (r *guiSharedAgentRuntime) Execute(ctx context.Context, request agentruntime.TurnRequest) (agentruntime.TurnResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	input, ok := agentruntime.DecodeTurnInput(request.Input)
	host, payloadOK := input.HostPayload.(guiRuntimeContext)
	if !ok || !payloadOK || r == nil || r.handler == nil {
		return agentruntime.TurnResult{}, fmt.Errorf("invalid GUI runtime turn input")
	}
	// Scope is authoritative at the Runtime boundary. During migration the
	// compatibility payload still carries the GUI loop context, but it must not
	// be able to silently execute for a different principal supplied by a
	// transport caller.
	if scopedUser := strings.TrimSpace(request.Scope.UserID); scopedUser != "" && scopedUser != strings.TrimSpace(host.userID) {
		return agentruntime.TurnResult{}, fmt.Errorf("GUI runtime scope user does not match host payload")
	}
	if request.Host != nil && host.ctx != nil {
		host.ctx.Host = request.Host
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.TurnResult{}, err
	}
	history, historyOK := decodeGUIHistory(input.History)
	attachments, attachmentsOK := decodeGUIAttachments(input.Attachments)
	if !historyOK || !attachmentsOK {
		return agentruntime.TurnResult{}, fmt.Errorf("invalid GUI runtime turn history or attachments")
	}
	var onStreamDone StreamDoneCallback
	if input.Callbacks != nil {
		onStreamDone = input.Callbacks.OnStreamDone
	}
	events := request.Events
	if events == nil {
		events = host.events
	}
	response := r.handler.executeSharedTurn(host.ctx, host.userID, input.SystemPrompt, history, input.UserText, attachments, events, host.onProgress, onStreamDone, input.MinIterations, input.Platform)
	if response == nil {
		return agentruntime.TurnResult{}, fmt.Errorf("shared runtime returned no response")
	}
	return agentruntime.TurnResult{Output: response}, nil
}

func decodeGUIHistory(value any) ([]agent.ConversationEntry, bool) {
	if value == nil {
		return nil, true
	}
	history, ok := value.([]agent.ConversationEntry)
	return history, ok
}

func decodeGUIAttachments(value any) ([]MessageAttachment, bool) {
	if value == nil {
		return nil, true
	}
	attachments, ok := value.([]MessageAttachment)
	return attachments, ok
}

func (r *guiSharedAgentRuntime) DescribeCapabilities(ctx context.Context, request agentruntime.CapabilityRequest) (agentruntime.CapabilitySnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.CapabilitySnapshot{}, err
	}
	prompt := ""
	if input, ok := agentruntime.DecodeTurnInput(request.Input); ok {
		prompt = input.SystemPrompt
	}
	host := request.Host
	if host == nil {
		if r != nil && r.handler != nil {
			host = guiHostCapabilities{}
		} else {
			host = agentruntime.HeadlessHostCapabilities{}
		}
	}
	snapshot := agentruntime.CapabilitySnapshot{
		ContractVersion: agentruntime.ContractVersion,
		Profile:         host.Profile(),
		PromptDigest:    agentruntime.DigestPrompt(prompt),
	}
	if r != nil && r.handler != nil {
		if strings.TrimSpace(prompt) == "" {
			prompt = r.handler.buildSystemPrompt()
			snapshot.PromptDigest = agentruntime.DigestPrompt(prompt)
		}
		snapshot.Tools = agentruntime.CapabilityToolsFromOpenAI(r.handler.buildToolDefinitions())
	}
	if r != nil && r.modules != nil {
		if strings.TrimSpace(prompt) == "" {
			contributed, err := r.modules.ContributePrompt(ctx, agentruntime.TurnRequest{Scope: request.Scope, Input: request.Input, Host: host})
			if err != nil {
				return agentruntime.CapabilitySnapshot{}, err
			}
			prompt = contributed
			snapshot.PromptDigest = agentruntime.DigestPrompt(prompt)
		}
		definitions, executable, err := r.modules.ToolsWithExecutability(ctx, agentruntime.TurnRequest{Scope: request.Scope, Input: request.Input, Host: host})
		if err != nil {
			return agentruntime.CapabilitySnapshot{}, err
		}
		snapshot.Modules = r.modules.SnapshotForHost(host)
		snapshot.Tools = agentruntime.MergeCapabilityTools(snapshot.Tools, agentruntime.CapabilityToolsFromDefinitions(definitions, executable))
	}
	snapshot.SurfaceDigest = agentruntime.DigestCapabilitySurface(snapshot)
	return snapshot, nil
}

func (r *guiSharedAgentRuntime) Close() error { return nil }

func (h *IMMessageHandler) sharedAgentRuntime() agentruntime.Runtime {
	h.sharedRuntimeMu.Lock()
	defer h.sharedRuntimeMu.Unlock()
	if h.sharedRuntime == nil {
		adapter := &guiSharedAgentRuntime{handler: h}
		if registry, err := agentruntime.RegisterBuiltinModules(); err == nil {
			adapter.modules = registry
		}
		h.sharedRuntime = adapter
	}
	return h.sharedRuntime
}

// SetSharedAgentRuntime installs the Runtime supplied by the GUI composition
// root. Passing nil restores the migration adapter on the next call. The
// setter is intentionally narrow so transports can inject a core Runtime
// without gaining access to IMMessageHandler internals.
func (h *IMMessageHandler) SetSharedAgentRuntime(runtime agentruntime.Runtime) {
	if h == nil {
		return
	}
	h.sharedRuntimeMu.Lock()
	h.sharedRuntime = runtime
	h.sharedRuntimeMu.Unlock()
}
