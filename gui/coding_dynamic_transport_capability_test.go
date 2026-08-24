package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestCodingDynamicProviderCorrelationMatrixFailsClosedForCurrentAdapters(t *testing.T) {
	tests := []struct {
		name       string
		cfg        corelib.MaclawLLMConfig
		adapterKey string
		protocol   string
		reason     string
	}{
		{
			name:       "openai chat HTTP SSE",
			cfg:        corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "chat"},
			adapterKey: "openai-chat-http-sse",
			protocol:   "openai-chat-completions",
			reason:     "provider_correlation_not_guaranteed",
		},
		{
			name:       "anthropic HTTP SSE",
			cfg:        corelib.MaclawLLMConfig{Protocol: "anthropic"},
			adapterKey: "anthropic-http-sse",
			protocol:   "anthropic-messages",
			reason:     "provider_correlation_not_guaranteed",
		},
		{
			name:       "responses HTTP SSE",
			cfg:        corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses"},
			adapterKey: "responses-http-sse",
			protocol:   "openai-responses",
			reason:     "provider_correlation_not_guaranteed",
		},
		{
			name:       "responses websocket channel is not yet a Coding adapter proof",
			cfg:        corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"},
			adapterKey: "responses-ws-channel-available-not-wired",
			protocol:   "openai-responses-ws",
			reason:     "responses_ws_channel_not_wired_to_coding_surface_lifecycle",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := codingDynamicProviderCorrelationForConfig(tc.cfg)
			if got.AdapterKey != tc.adapterKey || got.Protocol != tc.protocol || got.UnavailableReason != tc.reason {
				t.Fatalf("capability=%#v", got)
			}
			if got.HasTransportConnectionID || got.eligible() {
				t.Fatalf("current adapter must not materialize dynamic aliases: %#v", got)
			}
			if got.HasProviderResponseID || got.HasProviderToolCallID || got.HasCancellationFence || got.HasReplayIdentitySemantics {
				t.Fatalf("parser support is not a provider-adapter guarantee: %#v", got)
			}
		})
	}
}

func TestCodingDynamicProviderCorrelationMatrixIgnoresUserDescriptiveFields(t *testing.T) {
	base := codingDynamicProviderCorrelationForConfig(corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "chat"})
	spoofed := codingDynamicProviderCorrelationForConfig(corelib.MaclawLLMConfig{
		Protocol: "openai", WireAPI: "chat", URL: "wss://looks-like-a-connection.example/v1",
		Model: "connection-123", ProviderID: "request-456", ProviderName: "WebSocket proven",
	})
	if base != spoofed || spoofed.eligible() {
		t.Fatalf("descriptive configuration must not create correlation authority: base=%#v spoofed=%#v", base, spoofed)
	}
}

func TestCodingCallbacksKeepDynamicAliasesClosedForEveryCurrentTransport(t *testing.T) {
	identity := &trustedCodingInvocationIdentity{
		TenantID: "tenant", PrincipalID: "principal", SessionID: "session", RootTaskID: "root", TurnID: "turn",
	}
	configs := []corelib.MaclawLLMConfig{
		{Protocol: "openai", WireAPI: "chat"},
		{Protocol: "anthropic"},
		{Protocol: "openai", WireAPI: "responses"},
		{Protocol: "openai", WireAPI: "responses-ws"},
	}
	for _, cfg := range configs {
		local := &codingSubAgentCallbacks{subagent: &CodingSubAgent{cfg: cfg, dynamicInvocationIdentity: identity}}
		if local.codingDynamicAliasesMayMaterialize() {
			t.Fatalf("local callback opened aliases for unqualified adapter %#v", cfg)
		}
		remote := &remoteCodingCallbacks{agent: &RemoteCodingSubAgent{cfg: cfg, dynamicInvocationIdentity: identity}}
		if remote.codingDynamicAliasesMayMaterialize() {
			t.Fatalf("remote callback opened aliases for unqualified adapter %#v", cfg)
		}
	}
}

func TestCodingDynamicProductionAdapterQualificationStaysDisabledDespiteWSPrimitive(t *testing.T) {
	qualification := codingDynamicProductionAdapterForConfig(corelib.MaclawLLMConfig{Protocol: "openai", WireAPI: "responses-ws"})
	if qualification.Wired || qualification.Enabled || qualification.Reason != "coding_dynamic_production_wiring_disabled" {
		t.Fatalf("unreviewed production adapter qualification=%#v", qualification)
	}
	if qualification.Capability.AdapterKey != "responses-ws-channel-available-not-wired" || qualification.Capability.eligible() {
		t.Fatalf("WS primitive was promoted to a callback adapter: %#v", qualification)
	}
	if qualification.eligible() || qualification.AdapterVersion != "" || qualification.VerifiedIngress != "" || qualification.LifecycleDispositionVersion != "" || qualification.CatalogReceiptPolicyCovered || qualification.ReceiptDispatchVersion != "" || qualification.ReplacementSemanticsVersion != "" || qualification.FixedCohort != "" || qualification.KillSwitchInstalled {
		t.Fatalf("disabled qualification claimed production gate evidence: %#v", qualification)
	}
}

func TestCodingDynamicProductionQualificationRequiresEveryHostOwnedReleaseGate(t *testing.T) {
	completeCapability := codingDynamicProviderCorrelationCapability{
		AdapterKey: "reviewed-test-adapter", Protocol: "reviewed-test-protocol",
		HasTransportConnectionID: true, HasProviderResponseID: true, HasProviderToolCallID: true,
		HasCancellationFence: true, HasReplayIdentitySemantics: true,
	}
	base := codingDynamicProductionAdapterQualification{
		Capability: completeCapability, AdapterVersion: "s1c-v1", VerifiedIngress: "desktop-verified-coding",
		LifecycleDispositionVersion: "surface-disposition-v1", CatalogReceiptPolicyCovered: true,
		ReceiptDispatchVersion: "surface-dispatch-v1", ReplacementSemanticsVersion: "reviewed-test-replace-v1",
		ReplacementSemantics: &codingDynamicReplacementSemanticsCertificate{
			Version: "reviewed-test-replace-v1", Protocol: completeCapability.Protocol, Envelope: agent.ToolSurfaceEnvelopeResponses,
			ExplicitEmptySurfaceVerified: true, RejectsToolBearingRedirects: true, PolicyProjectionVersion: "responses-policy-v1",
			AppendContractTested: true, RetainContractTested: true,
		},
		FixedCohort: "opaque-reviewed-cohort", KillSwitchInstalled: true, Wired: true, Enabled: true,
	}
	if !base.eligible() {
		t.Fatalf("complete qualification unexpectedly rejected: %#v", base)
	}
	for _, mutate := range []func(*codingDynamicProductionAdapterQualification){
		func(q *codingDynamicProductionAdapterQualification) { q.AdapterVersion = "" },
		func(q *codingDynamicProductionAdapterQualification) { q.VerifiedIngress = "" },
		func(q *codingDynamicProductionAdapterQualification) { q.LifecycleDispositionVersion = "" },
		func(q *codingDynamicProductionAdapterQualification) { q.CatalogReceiptPolicyCovered = false },
		func(q *codingDynamicProductionAdapterQualification) { q.ReceiptDispatchVersion = "" },
		func(q *codingDynamicProductionAdapterQualification) { q.ReplacementSemanticsVersion = "" },
		func(q *codingDynamicProductionAdapterQualification) { q.ReplacementSemantics = nil },
		func(q *codingDynamicProductionAdapterQualification) {
			q.ReplacementSemantics.Protocol = "other-protocol"
		},
		func(q *codingDynamicProductionAdapterQualification) {
			q.ReplacementSemantics.ExplicitEmptySurfaceVerified = false
		},
		func(q *codingDynamicProductionAdapterQualification) {
			q.ReplacementSemantics.RejectsToolBearingRedirects = false
		},
		func(q *codingDynamicProductionAdapterQualification) {
			q.ReplacementSemantics.PolicyProjectionVersion = ""
		},
		func(q *codingDynamicProductionAdapterQualification) {
			q.ReplacementSemantics.AppendContractTested = false
		},
		func(q *codingDynamicProductionAdapterQualification) {
			q.ReplacementSemantics.RetainContractTested = false
		},
		func(q *codingDynamicProductionAdapterQualification) { q.FixedCohort = "" },
		func(q *codingDynamicProductionAdapterQualification) { q.KillSwitchInstalled = false },
		func(q *codingDynamicProductionAdapterQualification) { q.Wired = false },
		func(q *codingDynamicProductionAdapterQualification) { q.Enabled = false },
	} {
		candidate := base
		if base.ReplacementSemantics != nil {
			copy := *base.ReplacementSemantics
			candidate.ReplacementSemantics = &copy
		}
		mutate(&candidate)
		if candidate.eligible() {
			t.Fatalf("missing release gate became eligible: %#v", candidate)
		}
	}
}

func TestCodingDynamicProductionQualificationRejectsCertificateVersionSpoofing(t *testing.T) {
	capability := codingDynamicProviderCorrelationCapability{
		AdapterKey: "reviewed-test-adapter", Protocol: "reviewed-test-protocol",
		HasTransportConnectionID: true, HasProviderResponseID: true, HasProviderToolCallID: true,
		HasCancellationFence: true, HasReplayIdentitySemantics: true,
	}
	qualification := codingDynamicProductionAdapterQualification{
		Capability: capability, AdapterVersion: "s1c-v1", VerifiedIngress: "verified", LifecycleDispositionVersion: "v1",
		CatalogReceiptPolicyCovered: true, ReceiptDispatchVersion: "v1", ReplacementSemanticsVersion: "string-only-proof",
		ReplacementSemantics: &codingDynamicReplacementSemanticsCertificate{
			Version: "different-certificate-version", Protocol: capability.Protocol, Envelope: agent.ToolSurfaceEnvelopeResponses,
			ExplicitEmptySurfaceVerified: true, RejectsToolBearingRedirects: true, PolicyProjectionVersion: "v1", AppendContractTested: true, RetainContractTested: true,
		},
		FixedCohort: "cohort", KillSwitchInstalled: true, Wired: true, Enabled: true,
	}
	if qualification.eligible() {
		t.Fatalf("version string bypassed replacement certificate: %#v", qualification)
	}
}
