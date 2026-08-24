package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// codingDynamicProviderCorrelationCapability is a host-reviewed statement of
// what the *implemented* request adapter can prove.  It is deliberately not a
// provider configuration field: a URL, model name, WireAPI value, or display
// name must not turn an uncorrelated transport into an eligible dynamic
// invocation channel.
//
// Dynamic Coding aliases require every value below.  The first three identify
// a concrete provider response and tool call; the latter two describe whether
// the adapter has a defined lifecycle for late responses and retransmission.
// Until an adapter supplies all of them from transport/provider events, the
// dynamic surface remains fail-closed while ordinary static tools continue to
// use the existing compatibility loop.
type codingDynamicProviderCorrelationCapability struct {
	AdapterKey                 string
	Protocol                   string
	HasTransportConnectionID   bool
	HasProviderResponseID      bool
	HasProviderToolCallID      bool
	HasCancellationFence       bool
	HasReplayIdentitySemantics bool
	UnavailableReason          string
}

// codingDynamicReplacementSemanticsCertificate is a versioned, host-reviewed
// claim about one concrete provider protocol. It is deliberately not derived
// from a URL, model, WireAPI setting, request payload, or a successful local
// hash comparison: those facts cannot prove that a stateful provider replaces
// its retained tool surface.
//
// A certificate records the contract checks required before an adapter may be
// considered for a production cohort. The current product installs no such
// certificate, so dynamic aliases remain closed. Tests may construct one to
// prove that every clause is required; that does not make it a provider claim.
type codingDynamicReplacementSemanticsCertificate struct {
	Version string
	// Protocol must exactly match the transport protocol exported by the live
	// request channel; it is never inferred from the configuration that chose
	// the channel.
	Protocol string
	Envelope agent.ToolSurfaceEnvelope
	// ExplicitEmptySurfaceVerified proves tools:[] clears the provider-visible
	// callable surface rather than omitting the field or retaining prior tools.
	ExplicitEmptySurfaceVerified bool
	// RejectsToolBearingRedirects proves redirects are terminal for this
	// request, not an implicit second send that could inherit a predecessor
	// receipt/surface.
	RejectsToolBearingRedirects bool
	// PolicyProjectionVersion identifies the reviewed mapping for tool_choice
	// and parallel_tool_calls in this envelope.
	PolicyProjectionVersion string
	// These are independent provider-contract regressions: append and retain
	// semantics must each be observed to fail, not merely assumed equivalent.
	AppendContractTested bool
	RetainContractTested bool
}

func (c codingDynamicReplacementSemanticsCertificate) validFor(capability codingDynamicProviderCorrelationCapability) bool {
	return strings.TrimSpace(c.Version) != "" &&
		strings.TrimSpace(c.Protocol) != "" && c.Protocol == capability.Protocol &&
		c.Envelope != agent.ToolSurfaceEnvelopeUnspecified &&
		c.ExplicitEmptySurfaceVerified && c.RejectsToolBearingRedirects &&
		strings.TrimSpace(c.PolicyProjectionVersion) != "" &&
		c.AppendContractTested && c.RetainContractTested
}

// codingDynamicProductionAdapterQualification is intentionally separate from
// the provider capability matrix. The matrix describes transport facts; this
// result records whether the complete Coding callback lifecycle has been
// reviewed and enabled. A WebSocket channel existing in the process never
// upgrades this result by itself.
type codingDynamicProductionAdapterQualification struct {
	Capability codingDynamicProviderCorrelationCapability
	// AdapterVersion identifies the reviewed callback composition, not a
	// transport/provider configuration. A channel primitive cannot populate it.
	AdapterVersion string
	// VerifiedIngress identifies the authenticated Coding ingress scope for the
	// adapter. It must be supplied by host policy, never by runtime/request IDs.
	VerifiedIngress string
	// LifecycleDispositionVersion proves the adapter implements the shared-loop
	// exactly-once disposition contract for every reservation terminal path.
	LifecycleDispositionVersion string
	// CatalogReceiptPolicyCovered proves the reviewed catalog/effect policy is
	// complete for the selections that this adapter may materialize.
	CatalogReceiptPolicyCovered bool
	// ReceiptDispatchVersion proves the channel returns the receipt from the
	// same one-shot dispatch that produced the response, and RunLoop verifies it
	// before binding. A transport-local log is not sufficient.
	ReceiptDispatchVersion string
	// ReplacementSemanticsVersion is a compatibility/audit mirror of
	// ReplacementSemantics.Version. It cannot independently satisfy the gate.
	// The actual certificate covers per-request replacement, explicit empty
	// surfaces, redirect behavior, policy projection, and append/retain tests.
	ReplacementSemanticsVersion string
	ReplacementSemantics        *codingDynamicReplacementSemanticsCertificate
	// FixedCohort identifies the host-owned rollout cohort. It is intentionally
	// opaque and cannot be derived from user/task/config/model values.
	FixedCohort string
	// KillSwitchInstalled proves the cohort can be returned to the fail-closed
	// surface without falling back to a name dispatcher.
	KillSwitchInstalled bool
	Wired               bool
	Enabled             bool
	Reason              string
}

func (q codingDynamicProductionAdapterQualification) eligible() bool {
	return q.Capability.eligible() && q.Wired && q.Enabled &&
		strings.TrimSpace(q.AdapterVersion) != "" &&
		strings.TrimSpace(q.VerifiedIngress) != "" &&
		strings.TrimSpace(q.LifecycleDispositionVersion) != "" &&
		q.CatalogReceiptPolicyCovered && strings.TrimSpace(q.ReceiptDispatchVersion) != "" &&
		q.hasValidReplacementSemanticsCertificate() && strings.TrimSpace(q.FixedCohort) != "" && q.KillSwitchInstalled
}

func (q codingDynamicProductionAdapterQualification) hasValidReplacementSemanticsCertificate() bool {
	certificate := q.ReplacementSemantics
	return certificate != nil && certificate.Version == strings.TrimSpace(q.ReplacementSemanticsVersion) && certificate.validFor(q.Capability)
}

func codingDynamicProductionAdapterForConfig(cfg corelib.MaclawLLMConfig) codingDynamicProductionAdapterQualification {
	capability := codingDynamicProviderCorrelationForConfig(cfg)
	return codingDynamicProductionAdapterQualification{
		Capability: capability,
		// The holder is exercised only by focused tests. Neither local nor remote
		// callbacks have been approved to reserve it, publish a live surface, or
		// delegate model dispatch to its fixed bridge yet.
		Wired:   false,
		Enabled: false,
		Reason:  "coding_dynamic_production_wiring_disabled",
	}
}

func (c codingDynamicProviderCorrelationCapability) eligible() bool {
	return strings.TrimSpace(c.AdapterKey) != "" && strings.TrimSpace(c.Protocol) != "" &&
		c.HasTransportConnectionID && c.HasProviderResponseID && c.HasProviderToolCallID &&
		c.HasCancellationFence && c.HasReplayIdentitySemantics
}

// codingDynamicProviderCorrelationForConfig maps a configured LLM onto the
// host's adapter matrix.  This is intentionally conservative: the current
// core loop parses response and tool-call IDs, but its HTTP/SSE paths do not
// expose a stable, transport-owned connection identity to the Coding callback.
// A reviewed Responses WebSocket one-request channel now exists as a transport
// primitive, but current Coding callbacks do not reserve it, publish a durable
// surface against it, or route calls through the fixed bridge. Its configuration
// label still is not evidence that a WebSocket session identity was established
// for the request a Coding callback actually sent.
//
// When a real adapter is introduced it must add a new reviewed row here *and*
// pass its actual Protocol/ConnectionID through ToolSurfaceExecutionContext
// and response binder.  Do not "fix" this function by deriving ConnectionID
// from a loop ID, request ID, model value, URL, or tool-call name.
func codingDynamicProviderCorrelationForConfig(cfg corelib.MaclawLLMConfig) codingDynamicProviderCorrelationCapability {
	capability := codingDynamicProviderCorrelationCapability{
		// The common parsers can carry IDs when a wire response includes them,
		// but a user-configured compatible endpoint is not a reviewed adapter
		// promise that every response will contain them. In particular, legacy
		// function/content fallbacks may have no provider-issued call ID at all.
		// Keep all three correlation claims false until a concrete adapter
		// validates and exports those values from the transport boundary.
		UnavailableReason: "provider_correlation_not_guaranteed",
	}
	switch {
	case cfg.IsResponsesWebSocket():
		capability.AdapterKey = "responses-ws-channel-available-not-wired"
		capability.Protocol = "openai-responses-ws"
		capability.UnavailableReason = "responses_ws_channel_not_wired_to_coding_surface_lifecycle"
	case cfg.IsResponsesAPI():
		capability.AdapterKey = "responses-http-sse"
		capability.Protocol = "openai-responses"
	case strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic"):
		capability.AdapterKey = "anthropic-http-sse"
		capability.Protocol = "anthropic-messages"
	default:
		capability.AdapterKey = "openai-chat-http-sse"
		capability.Protocol = "openai-chat-completions"
	}
	// No current row sets HasTransportConnectionID. The field remains explicit
	// so adding a future eligible adapter is an intentional, testable change.
	return capability
}
