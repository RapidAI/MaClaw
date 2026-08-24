package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// codingResponsesWSRequestChannel is the reviewed adapter bridge between the
// shared loop's single-request channel seam and one live Responses WebSocket.
// It owns no retries, redirects or reconnections. A new RunLoop request must
// reserve a distinct socket and receive a distinct connection ID.
//
// This type intentionally lives outside CodingSubAgent callbacks: it is a
// transport primitive. Current callbacks do not opt into it yet, so no dynamic
// alias is materialized merely because a Responses WebSocket can be opened.
type codingResponsesWSRequestChannel struct {
	handler   *IMMessageHandler
	cfg       corelib.MaclawLLMConfig
	channel   *responsesWSRequestChannel
	mu        sync.Mutex
	audit     agent.ToolSurfacePlanEvidence
	auditSet  bool
	policy    agent.ToolSurfaceInvocationPolicy
	policySet bool
	// dispatchAttempted makes a pre-handoff integrity failure terminal for this
	// one-shot reservation too. Otherwise a caller could first invoke DoVerified
	// without audit evidence, then attach evidence and reuse the same socket as
	// an unrecorded logical successor.
	dispatchAttempted bool
}

func (c *codingResponsesWSRequestChannel) ExecutionContext() agent.ToolCallExecutionContext {
	if c == nil || c.channel == nil {
		return agent.ToolCallExecutionContext{}
	}
	return agent.ToolCallExecutionContext{
		Protocol:     "openai-responses-ws",
		ConnectionID: c.channel.connectionID,
	}
}

func (c *codingResponsesWSRequestChannel) Do(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (*llm.Response, error) {
	dispatch, err := c.DoVerified(ctx, conversation, tools, onToken, stream)
	return dispatch.Response, err
}

func (c *codingResponsesWSRequestChannel) DoVerified(ctx context.Context, conversation []interface{}, tools []map[string]interface{}, onToken llm.TokenCallback, stream bool) (agent.VerifiedToolSurfaceDispatch, error) {
	if c == nil || c.handler == nil || c.channel == nil {
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("coding responses websocket request channel is unavailable")
	}
	c.mu.Lock()
	if c.dispatchAttempted {
		c.mu.Unlock()
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("coding responses websocket request channel already used")
	}
	c.dispatchAttempted = true
	evidence, evidenceSet := c.audit, c.auditSet
	policy, policySet := c.policy, c.policySet
	c.mu.Unlock()
	// RunLoop currently requests streaming presentation. Rejecting a different
	// mode is safer than silently using an unrelated HTTP non-stream fallback,
	// which would break channel-to-surface ownership. This is deliberately after
	// dispatchAttempted: any invocation of a one-shot reservation consumes it,
	// even when it fails before the socket write.
	if !stream {
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("coding responses websocket request channel requires stream mode")
	}
	if !evidenceSet {
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("surface_integrity_failure: coding responses websocket audit evidence was not set")
	}
	if !policySet {
		return agent.VerifiedToolSurfaceDispatch{}, fmt.Errorf("surface_integrity_failure: coding responses websocket invocation policy was not set")
	}
	return c.handler.streamResponsesWSRequestChannelVerified(ctx, c.cfg, conversation, tools, onToken, nil, c.channel, evidence, policy)
}

// SetToolSurfaceDispatchPreparation atomically freezes both pieces of
// manifest-owned setup. RunLoop uses this instead of two setters so no future
// caller can observe a reservation with only audit evidence or only policy.
func (c *codingResponsesWSRequestChannel) SetToolSurfaceDispatchPreparation(preparation agent.ToolSurfaceDispatchPreparation) error {
	if c == nil || c.channel == nil {
		return fmt.Errorf("coding responses websocket request channel is unavailable")
	}
	evidence, err := agent.NormalizeToolSurfacePlanEvidence(preparation.AuditEvidence)
	if err != nil {
		return fmt.Errorf("surface_integrity_failure: invalid coding responses websocket audit evidence: %w", err)
	}
	policy, err := agent.NormalizeToolSurfaceInvocationPolicy(preparation.InvocationPolicy)
	if err != nil {
		return fmt.Errorf("surface_integrity_failure: invalid coding responses websocket invocation policy: %w", err)
	}
	if policy.Envelope != agent.ToolSurfaceEnvelopeResponses {
		return fmt.Errorf("surface_integrity_failure: coding responses websocket invocation policy envelope %q is not responses", policy.Envelope)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dispatchAttempted {
		return fmt.Errorf("surface_integrity_failure: coding responses websocket dispatch preparation set after dispatch attempt")
	}
	if (c.auditSet && !sameToolSurfacePlanEvidence(c.audit, evidence)) || (c.policySet && c.policy != policy) {
		return fmt.Errorf("surface_integrity_failure: coding responses websocket dispatch preparation changed")
	}
	c.channel.useMu.Lock()
	used := c.channel.used
	c.channel.useMu.Unlock()
	if used {
		return fmt.Errorf("surface_integrity_failure: coding responses websocket dispatch preparation set after channel use")
	}
	c.audit, c.auditSet = evidence, true
	c.policy, c.policySet = policy, true
	return nil
}

func (c *codingResponsesWSRequestChannel) Close(error) {
	if c != nil && c.channel != nil {
		c.channel.Close()
	}
}

// reserveCodingResponsesWSRequestChannel opens an actual socket for one model
// request. It is intentionally not a callback method and is not selected from
// user configuration alone; a future S1-C callback must pair this with verified
// ingress, durable surface publication and the fixed bridge before it may use
// the returned channel for model-visible semantic aliases.
func reserveCodingResponsesWSRequestChannel(ctx context.Context, handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client) (agent.ToolSurfaceRequestChannel, error) {
	if handler == nil || !cfg.IsResponsesWebSocket() {
		return nil, nil
	}
	channel, err := openResponsesWSRequestChannel(ctx, cfg, httpClient, nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(channel.connectionID) == "" {
		channel.Close()
		return nil, fmt.Errorf("coding responses websocket connection identity unavailable")
	}
	return &codingResponsesWSRequestChannel{handler: handler, cfg: cfg, channel: channel}, nil
}
