package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

// codingDynamicSurface is the request-local invocation binding for the
// CodingSubAgent's selected Skills and MCP tools. Its aliases are the only
// dynamic definitions sent to a model; provider/resource identities never
// appear as model-controlled arguments.
type codingDynamicSurface struct {
	mu     sync.RWMutex
	active string
	byName map[string]codingDynamicInvocation
}

// trustedCodingInvocationIdentity is the explicit handoff required before a
// Coding dynamic surface can enter the durable semantic lifecycle. It is kept
// separate from LoopContext because LoopContext.ID, Runtime.RequestID,
// codeSessionID, UserID and project paths are diagnostic/runtime values, not
// a verified semantic task anchor. This package currently has no verified
// Coding ingress that supplies all fields, so model-visible dynamic aliases
// remain fail-closed until the ToolPlan/coordinator integration lands.
type trustedCodingInvocationIdentity struct {
	TenantID    string
	PrincipalID string
	SessionID   string
	RootTaskID  string
	TurnID      string
}

// trustedCodingIdentityFromAnchor is the only conversion boundary between the
// durable runtime-to-semantic anchor and the Coding agent. It intentionally
// accepts no LoopContext, user, path, SSH or model values: those values are
// useful diagnostics but are not a trusted semantic lineage.
func trustedCodingIdentityFromAnchor(anchor *codingruntime.SemanticTaskAnchor) (*trustedCodingInvocationIdentity, bool) {
	if anchor == nil {
		return nil, false
	}
	identity := &trustedCodingInvocationIdentity{
		TenantID: anchor.TenantID, PrincipalID: anchor.PrincipalID, SessionID: anchor.SessionID,
		RootTaskID: anchor.RootTaskID, TurnID: anchor.TurnID,
	}
	if !identity.complete() {
		return nil, false
	}
	return identity, true
}

func resolveTrustedCodingInvocationIdentity(store codingruntime.Store, request codingruntime.ExecutionRequest) (*trustedCodingInvocationIdentity, bool) {
	anchors, ok := store.(codingruntime.SemanticTaskAnchorStore)
	if !ok || anchors == nil {
		return nil, false
	}
	anchor, err := anchors.ResolveSemanticTaskAnchor(request.Task.TaskID, request.Attempt.AttemptID)
	if err != nil {
		return nil, false
	}
	return trustedCodingIdentityFromAnchor(anchor)
}

// registerTrustedCodingInvocationIdentity is the sole Coding-host ingress for
// a previously verified identity. The caller must have obtained identity from
// authentication/task-relation infrastructure, not from a LoopContext or
// model input. Registering it against the just-started runtime Attempt makes
// the later resolver (and restart recovery) prove the linkage independently.
//
// Passing nil or an incomplete identity is intentionally a no-op. The caller
// must subsequently resolve the anchor and remain fail-closed on every error.
func registerTrustedCodingInvocationIdentity(store codingruntime.Store, request codingruntime.ExecutionRequest, identity *trustedCodingInvocationIdentity) bool {
	if identity == nil || !identity.complete() {
		return false
	}
	anchors, ok := store.(codingruntime.SemanticTaskAnchorStore)
	if !ok || anchors == nil {
		return false
	}
	_, err := anchors.RegisterSemanticTaskAnchor(codingruntime.SemanticTaskAnchor{
		RuntimeTaskID: request.Task.TaskID, RuntimeAttemptID: request.Attempt.AttemptID,
		TenantID: identity.TenantID, PrincipalID: identity.PrincipalID, SessionID: identity.SessionID,
		RootTaskID: identity.RootTaskID, TurnID: identity.TurnID,
	})
	return err == nil
}

// bindVerifiedCodingTaskHandle is the R1b runtime-start boundary. It accepts
// the opaque host-issued task handle, not a semantic identity assembled by the
// agent. The relation service revalidates subject scope and lifecycle before
// it writes the attempt fence and durable SemanticTaskAnchor.
func bindVerifiedCodingTaskHandle(service *codingTaskRelationService, subject verifiedCodingSubject, handle *verifiedCodingTaskHandle, store codingruntime.Store, request codingruntime.ExecutionRequest) (*trustedCodingInvocationIdentity, bool) {
	if service == nil || handle == nil {
		return nil, false
	}
	identity, err := service.BindCodingAttempt(subject, *handle, store, request, time.Now().UTC())
	if err != nil {
		return nil, false
	}
	return identity, identity != nil && identity.complete()
}

func (i trustedCodingInvocationIdentity) complete() bool {
	return strings.TrimSpace(i.TenantID) != "" && strings.TrimSpace(i.PrincipalID) != "" &&
		strings.TrimSpace(i.SessionID) != "" && strings.TrimSpace(i.RootTaskID) != "" &&
		strings.TrimSpace(i.TurnID) != ""
}

// codingDynamicAliasesMayMaterialize is deliberately stricter than merely
// having matches. The current request-local map is a compatibility renderer,
// not a durable grant/journal authority. Do not loosen this predicate until
// this callback is backed by ToolPlan + PublishSurface + ModelRequestSurface
// + coordinator admission; otherwise an identity field would only disguise
// the original match-is-authorization flaw.
func (c *codingSubAgentCallbacks) codingDynamicAliasesMayMaterialize() bool {
	if c == nil || c.subagent == nil || c.subagent.dynamicInvocationIdentity == nil || !c.subagent.dynamicInvocationIdentity.complete() {
		return false
	}
	qualification := codingDynamicProductionAdapterForConfig(c.subagent.cfg)
	if !qualification.eligible() {
		return false
	}
	// An eligible transport descriptor is necessary but not sufficient. The
	// local callback still needs to own a published durable request surface and
	// bind the response before it can render an alias.
	return false
}

func (c *remoteCodingCallbacks) codingDynamicAliasesMayMaterialize() bool {
	if c == nil || c.agent == nil || c.agent.dynamicInvocationIdentity == nil || !c.agent.dynamicInvocationIdentity.complete() {
		return false
	}
	qualification := codingDynamicProductionAdapterForConfig(c.agent.cfg)
	if !qualification.eligible() {
		return false
	}
	// See the local callback: R3/R4 production wiring remains a separate hard
	// gate from this adapter capability check.
	return false
}

type codingDynamicInvocationKind string

const (
	codingDynamicInvocationSkill codingDynamicInvocationKind = "skill"
	codingDynamicInvocationMCP   codingDynamicInvocationKind = "mcp"
)

type codingDynamicInvocation struct {
	kind  codingDynamicInvocationKind
	skill codingSubAgentSkillMatch
	mcp   codingSubAgentMCPToolMatch
}

func (s *codingDynamicSurface) render(skills []codingSubAgentSkillMatch, mcpTools []codingSubAgentMCPToolMatch) []map[string]interface{} {
	if s == nil {
		return nil
	}
	next := "coding-dynamic:" + codingDynamicSurfaceNonce()
	bindings := make(map[string]codingDynamicInvocation, len(skills)+len(mcpTools))
	definitions := make([]map[string]interface{}, 0, len(skills)+len(mcpTools))
	for i, skill := range skills {
		if !codingDynamicSkillBindingComplete(skill) {
			continue
		}
		alias := fmt.Sprintf("skill_%02d_%s", i+1, codingDynamicSurfaceNonce()[:12])
		bindings[alias] = codingDynamicInvocation{kind: codingDynamicInvocationSkill, skill: skill}
		definitions = append(definitions, buildCodingSkillInvocationDefinition(alias, skill))
	}
	for i, mcpTool := range mcpTools {
		if !codingDynamicMCPBindingComplete(mcpTool) {
			continue
		}
		alias := fmt.Sprintf("mcp_%02d_%s", i+1, codingDynamicSurfaceNonce()[:12])
		bindings[alias] = codingDynamicInvocation{kind: codingDynamicInvocationMCP, mcp: mcpTool}
		definitions = append(definitions, buildCodingMCPInvocationDefinition(alias, mcpTool))
	}
	s.mu.Lock()
	s.active = next
	s.byName = bindings
	s.mu.Unlock()
	return definitions
}

func codingDynamicSkillBindingComplete(binding codingSubAgentSkillMatch) bool {
	return strings.TrimSpace(binding.StableID) != "" && strings.TrimSpace(binding.ContentDigest) != "" && strings.TrimSpace(binding.ContractDigest) != ""
}

func codingDynamicMCPBindingComplete(binding codingSubAgentMCPToolMatch) bool {
	return strings.TrimSpace(binding.ServerID) != "" && strings.TrimSpace(binding.ToolName) != "" &&
		strings.TrimSpace(binding.SchemaDigest) != "" && strings.TrimSpace(binding.ContractDigest) != ""
}

func isCodingDynamicInvocationAlias(name string) bool {
	name = strings.TrimSpace(name)
	return strings.HasPrefix(name, "skill_") || strings.HasPrefix(name, "mcp_")
}

// isLegacyCodingDynamicGateway identifies the pre-semantic Coding gateways.
// They remain available only to explicit host-maintenance callers through the
// non-model callback APIs while the durable dynamic-catalog migration is in
// progress. A model response must never get from either name to the matched
// set: that would make candidate recall an authorization source again.
func isLegacyCodingDynamicGateway(name string) bool {
	switch strings.TrimSpace(name) {
	case "manage_skill", "call_mcp_tool":
		return true
	default:
		return false
	}
}

func rejectedCodingDynamicModelGateway() agent.ToolExecutionResult {
	return agent.ToolExecutionResult{Result: "[system rejected] catalog_incomplete", Outcome: agent.ToolExecutionOutcomeError}
}

// codingDynamicCapabilityUnavailableNotice is intentionally a prompt-only
// status note. Candidate matching may still help the model explain which
// capability is unavailable, but it must never imply an executable alias.
func codingDynamicCapabilityUnavailableNotice() string {
	return "\n\n> 动态 Skill/MCP 当前未通过本任务的受管授权检查，不能调用。请继续使用已提供的静态工具；若该能力不可替代，请说明需要重新规划或配置能力目录。\n"
}

func (s *codingDynamicSurface) beginEpoch() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active
}

func (s *codingDynamicSurface) epochIsCurrent(epoch string) bool {
	if s == nil || strings.TrimSpace(epoch) == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return epoch == s.active
}

func (s *codingDynamicSurface) binding(alias string) (codingDynamicInvocation, bool) {
	if s == nil {
		return codingDynamicInvocation{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	binding, ok := s.byName[strings.TrimSpace(alias)]
	return binding, ok
}

func (s *codingDynamicSurface) hasAlias(alias string) bool {
	_, ok := s.binding(alias)
	return ok
}

func codingDynamicSurfaceNonce() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	// A crypto-random failure is exceptionally rare. Returning a clearly
	// unusable alias preserves fail-closed behavior rather than falling back to
	// a predictable name or the generic gateway.
	return "random_unavailable"
}

func (c *codingSubAgentCallbacks) BuildToolsForModelRequest(userText string, iteration int) []map[string]interface{} {
	_ = userText
	_ = iteration
	if c.staticCompatibilitySurfaceQuarantined() {
		return nil
	}
	base := c.BuildTools("")
	// Until an eligible S1-C adapter supplies real request/response/tool-call
	// correlation, do not expose effectful workspace/provider families through
	// this name-dispatch compatibility belt. The remaining control-plane names
	// are governed independently by the S3 callback-local state machine.
	base = filterUncorrelatedCodingStaticCompatibilityEffects(codingStaticCompatibilityHostLocal, base)
	revision := c.setStaticCompatibilitySurface(base)
	base = annotateCodingTodoDefinitionForControlPlane(base, revision, c.todos.controlPlaneSnapshot().Version)
	c.recordStaticCompatibilitySurface(base, revision)
	// Dynamic Skill/MCP aliases are deliberately unavailable until the durable
	// plan, request/response binding, grant, and journal path exists. Do not
	// create an empty compatibility epoch here: static Coding tools use the
	// normal callback execution path and must not accidentally acquire a
	// request-epoch requirement just because dynamic capability work is pending.
	return base
}

// ContainToolSurfaceAmbiguousDelivery opts the legacy Coding static belt into
// the S0.5 fail-closed rule. It does not make the belt response-correlated.
func (c *codingSubAgentCallbacks) ContainToolSurfaceAmbiguousDelivery() bool {
	return true
}

// OnToolSurfaceAttemptStarted intentionally records no local identity. The
// current HTTP/SSE transport cannot issue a trustworthy connection/response
// identifier, so this callback only receives a lifecycle notification.
func (c *codingSubAgentCallbacks) OnToolSurfaceAttemptStarted(agent.ToolCallExecutionContext) {}

// OnToolSurfaceAttemptFinished quarantines a model-visible legacy static
// surface when a request may have left the host without a safely consumed
// response. It must not be used to infer provider correlation or replay.
func (c *codingSubAgentCallbacks) OnToolSurfaceAttemptFinished(_ agent.ToolCallExecutionContext, delivery agent.ToolSurfaceDeliveryState) {
	if delivery == agent.ToolSurfaceAmbiguousDelivery {
		c.quarantineStaticCompatibilitySurface()
	}
}

func rejectedCodingStaticCompatibilityTool(name string) agent.ToolExecutionResult {
	return agent.ToolExecutionResult{
		Result:  "[system rejected] static_surface_unavailable: " + strings.TrimSpace(name),
		Outcome: agent.ToolExecutionOutcomeError,
	}
}

func (c *codingSubAgentCallbacks) codingDynamicCapabilityPromptAvailable() bool {
	return c != nil && c.codingDynamicAliasesMayMaterialize()
}

func (c *remoteCodingCallbacks) codingDynamicCapabilityPromptAvailable() bool {
	return c != nil && c.codingDynamicAliasesMayMaterialize()
}

func (c *codingSubAgentCallbacks) BeginToolSurfaceEpoch(iteration int) string {
	if c.dynamicLifecycleRelaySnapshot() != nil {
		// A qualified dynamic composition owns no static compatibility surface.
		// This opaque nonce is a request fence only; it is not an identity,
		// provider response ID, or recovery key.
		return "coding-dynamic:" + codingDynamicSurfaceNonce()
	}
	_ = iteration
	return c.beginStaticCompatibilitySurfaceEpoch()
}

func (c *codingSubAgentCallbacks) ExecuteToolCallWithContext(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) agent.ToolExecutionResult {
	if relay := c.dynamicLifecycleRelaySnapshot(); relay != nil {
		// Once the dynamic composition is active, there is deliberately no
		// fallback to the name-based S0.5 dispatcher. A missing/terminal holder
		// is a stale surface, not permission to reinterpret the alias.
		return relay.ExecuteToolCallWithContext(name, argsJSON, callID, execution)
	}
	// Do this before the generic structured-tool fallback. The legacy gateway
	// names carry a model-controlled provider selector and therefore cannot be
	// made safe by merely checking the current request epoch.
	if isLegacyCodingDynamicGateway(name) {
		return rejectedCodingDynamicModelGateway()
	}
	if isCodingDynamicInvocationAlias(name) {
		return rejectedCodingDynamicModelGateway()
	}
	// A model-visible function name is an exact transport key. Do not apply
	// legacy convenience aliases before this fence: `search_files` must not
	// become `Glob` merely because Glob was rendered in this request.
	if !c.staticCompatibilityCanonicalToolAllowed(name) {
		return rejectedCodingStaticCompatibilityTool(name)
	}
	if !c.staticCompatibilityExecutionEpochAllowed(execution.SurfaceEpoch) {
		return rejectedCodingStaticCompatibilityTool(name)
	}
	_ = callID
	// Keep model dispatch on the context-aware branch through to the canonical
	// executor. Do not route back through ExecuteToolStructured: that public
	// compatibility entry intentionally has no request epoch for host-owned
	// maintenance and tests.
	return c.executeToolStructuredCanonical(name, argsJSON)
}

func codingDynamicAgentOutcome(outcome codingToolOutcome) agent.ToolExecutionOutcome {
	if outcome == codingToolOutcomeSuccess {
		return agent.ToolExecutionOutcomeOK
	}
	if outcome == codingToolOutcomeTimeout {
		return agent.ToolExecutionOutcomeTimeout
	}
	return agent.ToolExecutionOutcomeError
}
