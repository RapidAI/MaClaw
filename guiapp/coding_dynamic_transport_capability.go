package guiapp

import (
	"strings"
	"sync/atomic"

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
// considered for a production cohort. Only the repository-owned Responses-WS
// adapter row can provide it; unqualified transports remain closed. Tests may
// construct one to prove that every clause is required; that does not make it
// a provider claim.
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

// codingResponsesWSReplacementSemanticsVersion is the compile-time version of
// the replacement-semantics certificate for the qualified
// responses-ws-single-use-channel row. The loopback contract suite
// (coding_responses_ws_replacement_contract_test.go) pins every certificate
// field and the equality of this constant with its own suite version.
const codingResponsesWSReplacementSemanticsVersion = "responses-ws-replacement-semantics-v1"

// codingDynamicVerifiedIngressScope identifies the §9.10 desktop verified
// Coding ingress: durable identity plus host workspace binding from the same
// verified source. It is a host-owned constant, never derived from
// configuration, task text, or runtime IDs.
const codingDynamicVerifiedIngressScope = "desktop-verified-coding-ingress-v1"

// codingDynamicLifecycleDispositionVersion is the compile-time version of the
// D2 lifecycle disposition matrix. The hermetic production-composition suite
// (coding_e3_hermetic_matrix_test.go) pins every disposition × request phase ×
// local/remote cell and the equality of this constant with its suite version.
const codingDynamicLifecycleDispositionVersion = "coding-dynamic-lifecycle-disposition-v1"

// codingDynamicFixedCohortV1 is the E4 rollout cohort for the qualified
// responses-ws-single-use-channel row. The value was generated once with a
// cryptographic random source (crypto/rand, 128 bits) and is committed
// verbatim. It is deliberately opaque: it is not derivable from any user,
// task, configuration, model, URL, or path value, and it is not rotated —
// changing the cohort means a new constant with its own drill evidence, never
// an edit in place.
const codingDynamicFixedCohortV1 = "coding-dynamic-fixed-cohort-v1:648fb8c10b87c158f62b3e4f67d4d90b"

// codingDynamicCatalogReceiptPolicyCoveredV1 is the E3 catalog/receipt
// coverage certificate. It is a build-time claim proven by the hermetic
// catalog guard, never inferred from endpoint configuration.
const codingDynamicCatalogReceiptPolicyCoveredV1 = true

// codingDynamicKillSwitchControlVersion identifies the kill-switch control
// contract proven by the E4 drill suite (coding_e4_cohort_killswitch_test.go).
const codingDynamicKillSwitchControlVersion = "coding-dynamic-kill-switch-v1"

// codingDynamicKillSwitchState is the host-owned process-internal runtime
// control plane for qualified dynamic assembly. No config field, model output,
// task text, runtime ID, or RPC payload can reach it. The default follows the
// evidence; engaging closes new assembly immediately while in-flight
// reservations keep their existing lifecycle contract.
var codingDynamicKillSwitchState atomic.Int32

const (
	codingDynamicKillSwitchFollowEvidence = 0
	codingDynamicKillSwitchEngaged        = 1
	// codingDynamicKillSwitchInvalidated latches evidence invalidation after a
	// disengage: eligibility is restored only by a control-plane rearm that
	// re-validates the complete evidence, never by flipping the switch alone.
	codingDynamicKillSwitchInvalidated = 2
)

func codingDynamicKillSwitchClosed() bool {
	return codingDynamicKillSwitchState.Load() != codingDynamicKillSwitchFollowEvidence
}

// engageCodingDynamicKillSwitch closes new dynamic assembly immediately.
func engageCodingDynamicKillSwitch() {
	codingDynamicKillSwitchState.Store(codingDynamicKillSwitchEngaged)
}

// disengageCodingDynamicKillSwitch lifts the runtime kill but leaves the
// evidence invalidation latch engaged.
func disengageCodingDynamicKillSwitch() {
	codingDynamicKillSwitchState.CompareAndSwap(codingDynamicKillSwitchEngaged, codingDynamicKillSwitchInvalidated)
}

// rearmCodingDynamicKillSwitch is the host control-plane evidence
// re-validation: it restores evidence-following only when the switch is
// disengaged and the current assembly qualification is complete. It reports
// whether evidence-following was (or remained) restored.
func rearmCodingDynamicKillSwitch(cfg corelib.MaclawLLMConfig) bool {
	if codingDynamicKillSwitchState.Load() == codingDynamicKillSwitchEngaged {
		return false
	}
	if !codingDynamicEvidenceQualification(cfg).eligible() {
		return false
	}
	codingDynamicKillSwitchState.Store(codingDynamicKillSwitchFollowEvidence)
	return true
}

// codingDynamicProductionAdapterQualificationOverride is the E3 hermetic
// substitution point: a package-internal slot that focused tests use to
// install a fully populated qualification literal while rehearsing the real
// assembly against a loopback provider. It is nil in production, is read only
// by the two assembly entry points below, and is never consulted by
// codingDynamicAliasesMayMaterialize, which keeps reading the disabled default.
var codingDynamicProductionAdapterQualificationOverride *codingDynamicProductionAdapterQualification

// codingDynamicEvidenceQualification reads the current evidence without the
// runtime kill mask: the test override slot when installed, else the
// production registry. The kill switch layers above this read; the
// control-plane rearm must evaluate the evidence itself, not the masked
// assembly answer.
func codingDynamicEvidenceQualification(cfg corelib.MaclawLLMConfig) codingDynamicProductionAdapterQualification {
	if override := codingDynamicProductionAdapterQualificationOverride; override != nil {
		return *override
	}
	return codingDynamicProductionAdapterForConfig(cfg)
}

// codingDynamicProductionAdapterQualificationForAssembly is the single
// qualification read for relay construction and the future factory. The
// default path reads the host-owned production registry; the override exists
// so the hermetic suite can drive the same assembly code without changing it.
// An engaged kill switch makes every read ineligible regardless of evidence.
func codingDynamicProductionAdapterQualificationForAssembly(cfg corelib.MaclawLLMConfig) codingDynamicProductionAdapterQualification {
	if codingDynamicKillSwitchClosed() {
		return codingDynamicProductionAdapterQualification{Capability: codingDynamicProviderCorrelationForConfig(cfg), Reason: "coding_dynamic_kill_switch_engaged"}
	}
	return codingDynamicEvidenceQualification(cfg)
}

// codingDynamicResponsesWSReplacementSemanticsCertificate is the compile-time
// certificate for the qualified responses-ws-single-use-channel row. Every
// field is a declaration about this repository's adapter implementation that
// the loopback replacement-semantics contract suite proves with positive and
// negative cases; it is not a claim about any user-configured endpoint.
func codingDynamicResponsesWSReplacementSemanticsCertificate() *codingDynamicReplacementSemanticsCertificate {
	return &codingDynamicReplacementSemanticsCertificate{
		Version:                      codingResponsesWSReplacementSemanticsVersion,
		Protocol:                     "openai-responses-ws",
		Envelope:                     agent.ToolSurfaceEnvelopeResponses,
		ExplicitEmptySurfaceVerified: true,
		RejectsToolBearingRedirects:  true,
		PolicyProjectionVersion:      codingResponsesWSPolicyProjectionVersion,
		AppendContractTested:         true,
		RetainContractTested:         true,
	}
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

// codingDynamicProductionCompositionWired is deliberately derived from the
// concrete callback method sets and the first-family cutover marker.  Wired
// must never be a release toggle that can drift independently from the
// composition assembled by the host.
func codingDynamicProductionCompositionWired() bool {
	// The compile-time assertions in coding_dynamic_callback_composition.go
	// guarantee the method set.  Keep a runtime predicate as the single source
	// used by qualification so adding/removing one of the five relay hooks
	// cannot be papered over by setting a bool in a registry literal.
	return codingDynamicFirstFamilyLegacyRemoved()
}

// codingDynamicProductionEvidenceEnabled derives Enabled from the complete
// machine evidence.  It intentionally excludes Wired: a complete certificate
// without a wired composition is not executable.
func (q codingDynamicProductionAdapterQualification) evidenceEnabled() bool {
	return q.Capability.eligible() &&
		strings.TrimSpace(q.AdapterVersion) != "" &&
		strings.TrimSpace(q.VerifiedIngress) != "" &&
		strings.TrimSpace(q.LifecycleDispositionVersion) != "" &&
		q.CatalogReceiptPolicyCovered &&
		strings.TrimSpace(q.ReceiptDispatchVersion) != "" &&
		q.hasValidReplacementSemanticsCertificate() &&
		strings.TrimSpace(q.FixedCohort) != "" && q.KillSwitchInstalled
}

func (q codingDynamicProductionAdapterQualification) eligible() bool {
	return q.Wired && q.Enabled && q.evidenceEnabled()
}

func (q codingDynamicProductionAdapterQualification) hasValidReplacementSemanticsCertificate() bool {
	certificate := q.ReplacementSemantics
	return certificate != nil && certificate.Version == strings.TrimSpace(q.ReplacementSemanticsVersion) && certificate.validFor(q.Capability)
}

func codingDynamicProductionAdapterForConfig(cfg corelib.MaclawLLMConfig) codingDynamicProductionAdapterQualification {
	capability := codingDynamicProviderCorrelationForConfig(cfg)
	qualification := codingDynamicProductionAdapterQualification{
		Capability: capability,
		Reason:     "coding_dynamic_production_wiring_disabled",
	}
	if capability.eligible() {
		// E1 machine evidence: the adapter implementation's compile-time
		// version, pinned equal to the loopback conformance suite version by
		// TestCodingResponsesWSAdapterVersionMatchesConformanceSuite. The
		// capability row being qualified does not wire the callback composition.
		qualification.AdapterVersion = codingResponsesWSAdapterVersion
		qualification.Reason = "coding_dynamic_capability_qualified_wiring_disabled"
		// E2 machine evidence: the replacement-semantics certificate and the
		// one-shot dispatch receipt contract, both proven by the loopback
		// contract suite in the same commit.
		qualification.ReplacementSemanticsVersion = codingResponsesWSReplacementSemanticsVersion
		qualification.ReplacementSemantics = codingDynamicResponsesWSReplacementSemanticsCertificate()
		qualification.ReceiptDispatchVersion = codingResponsesWSAdapterVersion
		// E3 machine evidence: the desktop verified ingress scope and the
		// hermetic production-composition disposition matrix.
		qualification.VerifiedIngress = codingDynamicVerifiedIngressScope
		qualification.LifecycleDispositionVersion = codingDynamicLifecycleDispositionVersion
		// E4 machine evidence: the committed opaque fixed cohort and the
		// drill-proven runtime kill switch.
		qualification.FixedCohort = codingDynamicFixedCohortV1
		qualification.KillSwitchInstalled = true
		// E5: derive both gates from the assembled composition and all evidence;
		// neither value is hand-authored in the registry.
		qualification.Wired = codingDynamicProductionCompositionWired()
		qualification.CatalogReceiptPolicyCovered = codingDynamicCatalogReceiptPolicyCoveredV1
		qualification.Enabled = qualification.evidenceEnabled()
		if qualification.eligible() {
			qualification.Reason = "coding_dynamic_production_enabled"
		}
	}
	return qualification
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
// The repository's own Responses WebSocket single-use channel adapter is the
// one qualified row: its five correlation claims are compile-time declarations
// about this repository's adapter implementation, each pinned by the loopback
// WS conformance suite (coding_responses_ws_conformance_test.go, versioned).
// That row declares what the adapter does — it is not trust in a user endpoint.
// An endpoint that violates the contract (missing response.id, missing or
// conflicting tool-call IDs) still fails closed at the bind/dispatch boundary.
// Current Coding callbacks do not reserve the channel, publish a durable
// surface against it, or route calls through the fixed bridge: qualification
// stays disabled independently of this capability row.
//
// When another real adapter is introduced it must add a new reviewed row here
// *and* pass its actual Protocol/ConnectionID through ToolSurfaceExecutionContext
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
		capability.AdapterKey = "responses-ws-single-use-channel"
		capability.Protocol = "openai-responses-ws"
		capability.HasTransportConnectionID = true
		capability.HasProviderResponseID = true
		capability.HasProviderToolCallID = true
		capability.HasCancellationFence = true
		capability.HasReplayIdentitySemantics = true
		capability.UnavailableReason = ""
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
	// Only the conformance-proven Responses WebSocket adapter row sets the
	// correlation claims. Every other row keeps them explicit and false so
	// adding a future eligible adapter is an intentional, testable change.
	return capability
}
