package agentservice

import (
	"context"
	"testing"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

func reviewedDynamicCapabilityRegistry(t *testing.T) *coretool.CapabilityRegistry {
	t.Helper()
	registry := coretool.NewCapabilityRegistry("dynamic-contracts-v1")
	if err := registry.Register(coretool.CapabilityDescriptor{
		ID:      "document.lookup",
		Version: "v1",
		Qualifiers: map[string]coretool.QualifierConstraint{
			"scope": {Values: []string{"current"}, Required: true},
		},
		Effects: []coretool.EffectClass{coretool.EffectReadOnly},
		Owner:   "routing-review",
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Seal(); err != nil {
		t.Fatal(err)
	}
	return registry
}

func reviewedDynamicContract() DynamicCapabilityContract {
	return DynamicCapabilityContract{
		Provisions: []coretool.CapabilityProvision{{Capability: "document.lookup", Qualifiers: map[string]string{"scope": "current"}, Quality: 1}},
		Effects:    []coretool.EffectClass{coretool.EffectReadOnly},
	}
}

func newDynamicCapabilityPublisherForTest(t *testing.T) (*Service, *DynamicCapabilityContractPublisher) {
	t.Helper()
	svc, err := NewService(Config{DataRoot: t.TempDir()}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := NewDynamicCapabilityContractPublisher(svc, reviewedDynamicCapabilityRegistry(t))
	if err != nil {
		_ = svc.Close()
		t.Fatal(err)
	}
	return svc, publisher
}

func TestDynamicCapabilityContractPublisherRejectsUnreviewedRegistry(t *testing.T) {
	svc, err := NewService(Config{DataRoot: t.TempDir()}, nil, EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	registry := coretool.NewCapabilityRegistry("unsealed-v1")
	if _, err := NewDynamicCapabilityContractPublisher(svc, registry); err == nil {
		t.Fatal("publisher accepted an unsealed capability registry")
	}
}

func TestDynamicCapabilityContractPublisherRejectsUnknownCapability(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	contract := reviewedDynamicContract()
	contract.Provisions[0].Capability = "unreviewed.capability"
	if err := publisher.publishObservedMCP(p, "server", "lookup", contract); err == nil {
		t.Fatal("publisher accepted an unknown capability")
	}
}

func TestDynamicCapabilityContractPublisherRejectsInvalidQualifier(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	contract := reviewedDynamicContract()
	contract.Provisions[0].Qualifiers = map[string]string{"scope": "other"}
	if err := publisher.publishObservedMCP(p, "server", "lookup", contract); err == nil {
		t.Fatal("publisher accepted an invalid qualifier value")
	}
}

func TestDynamicCapabilityContractPublisherRejectsEffectEscalation(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	contract := reviewedDynamicContract()
	contract.Effects = []coretool.EffectClass{coretool.EffectExternalEffect}
	if err := publisher.publishObservedSkill(p, "vendor.lookup", contract); err == nil {
		t.Fatal("publisher accepted an effect escalation")
	}
}

func TestDynamicCapabilityContractPublisherRejectsUnreviewedArtifactContract(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	contract := reviewedDynamicContract()
	contract.Produces = []coretool.ArtifactContract{{Kind: "unreviewed", MIMEType: "application/octet-stream"}}
	contract.ObservedBindingDigest = "observed"
	if err := publisher.publishObservedSkill(p, "vendor.lookup", contract); err == nil {
		t.Fatal("publisher accepted an artifact contract absent from the capability descriptor")
	}
}

func TestDynamicCapabilityContractPublisherRejectsUnobservedBinding(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	if err := publisher.publishObservedMCP(p, "server", "lookup", reviewedDynamicContract()); err == nil {
		t.Fatal("publisher accepted a contract without a Service-observed binding digest")
	}
}

func TestDynamicCapabilityContractPublisherRequiresObservedMCPBinding(t *testing.T) {
	svc, publisher := newDynamicCapabilityPublisherForTest(t)
	defer svc.Close()
	p := Principal{TenantID: "tenant", UserID: "user"}
	if err := svc.EnsurePrincipal(context.Background(), p, "user@example.test", "User"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateMCPServer(context.Background(), p, MCPServerCreateInput{Kind: "remote", Name: "remote", EndpointURL: "https://mcp.example"}); err != nil {
		t.Fatal(err)
	}
	if err := publisher.PublishObservedMCP(context.Background(), p, "missing", "lookup", reviewedDynamicContract()); err == nil {
		t.Fatal("publisher accepted an MCP binding absent from the ready inventory")
	}
}

func TestObservedBindingDigestsQuarantineSchemaAndContentDrift(t *testing.T) {
	if dynamicMCPContractMatchesEntry(reviewedDynamicContract(), MCPToolEntry{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object"}}) {
		t.Fatal("MCP contract without an observed binding digest was routable")
	}
	mcpContract := reviewedDynamicContract()
	mcpContract.ObservedBindingDigest = dynamicMCPObservedBindingDigest("server", "lookup", map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}})
	matchingMCP := MCPToolEntry{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}}, Contract: mcpContract}
	if !dynamicMCPContractMatchesEntry(mcpContract, matchingMCP) {
		t.Fatal("matching MCP binding was rejected")
	}
	matchingMCP.InputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{"page": map[string]interface{}{"type": "integer"}}}
	if dynamicMCPContractMatchesEntry(mcpContract, matchingMCP) {
		t.Fatal("MCP schema drift retained a contract")
	}

	if dynamicSkillContractMatchesEntry(reviewedDynamicContract(), SkillToolEntry{StableID: "vendor.lookup", Name: "lookup", Version: "v1", ContentDigest: "content-v1"}) {
		t.Fatal("Skill contract without an observed binding digest was routable")
	}
	skillContract := reviewedDynamicContract()
	skillContract.ObservedBindingDigest = dynamicSkillObservedBindingDigest("vendor.lookup", "v1", "content-v1")
	skillEntry := SkillToolEntry{StableID: "vendor.lookup", Name: "lookup", Version: "v1", ContentDigest: "content-v1", Contract: skillContract}
	if !dynamicSkillContractMatchesEntry(skillContract, skillEntry) {
		t.Fatal("matching Skill binding was rejected")
	}
	skillEntry.ContentDigest = "content-v2"
	if dynamicSkillContractMatchesEntry(skillContract, skillEntry) {
		t.Fatal("Skill content drift retained a contract")
	}
}
