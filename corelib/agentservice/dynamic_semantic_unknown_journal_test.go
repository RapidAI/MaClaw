package agentservice

import (
	"context"
	"errors"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// unknownJournalSurface builds an uncoordinated semantic surface whose only
// adapter fails its bound transport, which is the ordinary way a provider
// produces an outcome that is neither success nor definite failure.
func unknownJournalSurface(t *testing.T) (*coreDynamicSemanticSurface, DynamicSemanticRouting, *boundMCPProviderStub) {
	t.Helper()
	registry := dynamicSemanticRegistry(t)
	issuer, err := coretool.NewInvocationIssuer(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	entry := MCPToolEntry{ServerID: "server", ToolName: "lookup", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "additionalProperties": false}, Contract: testDynamicCapabilityContract()}
	provider := &boundMCPProviderStub{entries: []MCPToolEntry{entry}, err: errors.New("disconnected")}
	catalog, err := BuildDynamicSemanticCatalog(provider.entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).Publish(catalog.Providers, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: snapshot, Needs: []coretool.CapabilityNeed{{ID: "need", Capability: "test.dynamic.execute", Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	routing := DynamicSemanticRouting{
		Registry: registry, Issuer: issuer, ExecutionStore: coretool.NewMemoryPlanExecutionStore(),
		RouteState: coretool.NewMemoryRouteStateStore(), HostCalls: coretool.NewMemoryHostCallJournal(), GrantTTL: time.Minute,
	}
	scope := coretool.InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "tenant:user"}
	surface, err := newCoreDynamicSemanticSurface(routing, catalog, plan, scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := surface.Definitions(); err != nil {
		t.Fatal(err)
	}
	return surface, routing, provider
}

// A provider that may have applied an effect before its outcome was lost must
// not be journalled as completed. Completing it lets a later acquire replay a
// definite verdict derived from stored text, which is exactly what invites a
// second attempt at an operation that might already hold.
func TestUncoordinatedUnknownOutcomeIsJournalledAsUnknownNotCompleted(t *testing.T) {
	surface, routing, provider := unknownJournalSurface(t)
	name := ""
	for functionName := range surface.grants {
		name = functionName
	}
	if name == "" {
		t.Fatal("no grant was materialized")
	}
	grant := surface.grants[name]
	fingerprint := coretool.InvocationGrantFingerprint(grant)
	selection, ok := dynamicSemanticSelectionByID(surface.plan, grant.SelectionID)
	if !ok {
		t.Fatal("planned selection is missing")
	}
	canonical, err := coretool.CanonicalizeAuthorizedInvocationArguments("{}", surface.catalog.schemas[selection.AdapterName], selection.ParameterAuthorization)
	if err != nil {
		t.Fatal(err)
	}
	result, handled := surface.Execute(context.Background(), Principal{TenantID: "tenant", UserID: "user"}, provider, nil, name, "{}", "call-1")
	if !handled || !result.Unknown || result.Succeeded {
		t.Fatalf("transport failure was not surfaced as unknown: %#v", result)
	}
	identity := coretool.HostCallIdentity{Protocol: "core-agent-loop/v1", ConnectionID: "session\x00turn", CallID: "call-1"}
	record, action, err := routing.HostCalls.Acquire(identity, fingerprint, canonical.Digest, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if record.State != coretool.HostCallUnknown {
		t.Fatalf("unknown outcome was journalled as %q", record.State)
	}
	if action != coretool.HostCallAcquireUnknown {
		t.Fatalf("a later acquire reported %q instead of unknown", action)
	}
}

// The journal saying "this may have reached its provider" is the strongest
// evidence the host has. Surfacing it as a definite failure is the mirror of
// surfacing it as success: both erase the uncertainty the record exists to keep.
func TestAcquiredUnknownHostCallIsSurfacedAsUnknownNotFailure(t *testing.T) {
	surface, routing, provider := unknownJournalSurface(t)
	name := ""
	for functionName := range surface.grants {
		name = functionName
	}
	grant := surface.grants[name]
	fingerprint := coretool.InvocationGrantFingerprint(grant)
	selection, ok := dynamicSemanticSelectionByID(surface.plan, grant.SelectionID)
	if !ok {
		t.Fatal("planned selection is missing")
	}
	canonical, err := coretool.CanonicalizeAuthorizedInvocationArguments("{}", surface.catalog.schemas[selection.AdapterName], selection.ParameterAuthorization)
	if err != nil {
		t.Fatal(err)
	}
	identity := coretool.HostCallIdentity{Protocol: "core-agent-loop/v1", ConnectionID: "session\x00turn", CallID: "call-1"}
	if _, _, err := routing.HostCalls.Acquire(identity, fingerprint, canonical.Digest, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := routing.HostCalls.MarkUnknown(identity, fingerprint, canonical.Digest, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	result, handled := surface.Execute(context.Background(), Principal{TenantID: "tenant", UserID: "user"}, provider, nil, name, "{}", "call-1")
	if !handled || result.ReasonCode != "host_call_unknown" {
		t.Fatalf("result=%#v", result)
	}
	if !result.Unknown || result.Succeeded {
		t.Fatalf("a recorded unknown host call was reported as a definite outcome: %#v", result)
	}
	if provider.boundCalls != 0 {
		t.Fatalf("an unknown record must never reach the provider again, got %d calls", provider.boundCalls)
	}
}
