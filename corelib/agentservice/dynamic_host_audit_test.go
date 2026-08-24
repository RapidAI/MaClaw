package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostAuditReader struct {
	query     string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostAuditReader) ReadReviewedHostAudit(_ context.Context, principal Principal, query string) (string, error) {
	f.principal = principal
	f.query = query
	return f.result, f.err
}

func TestReviewedHostAuditExecutesQueryAndRejectsLookupMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	reader := &fakeHostAuditReader{result: "audit events (1):\n- 2026-08-16T13:00:00Z action=message.posted"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Audit: reader})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "audit", Capability: CapabilityAuditRead, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("audit plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityAuditRead {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	result := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"query":"message.posted"}`)
	if !result.Succeeded || result.Result != reader.result {
		t.Fatalf("audit result=%#v", result)
	}
	if reader.query != "message.posted" || reader.principal.TenantID != principal.TenantID || reader.principal.UserID != principal.UserID {
		t.Fatalf("reader=%#v", reader)
	}
	empty := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !empty.Succeeded {
		t.Fatalf("empty query must be allowed, result=%#v", empty)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"query":"x","channel":"lansenger"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("channel args must fail closed, result=%#v", rejected)
	}

	lookupPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-lookup", TurnID: "turn-lookup", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{
			ID: "lookup", Capability: CapabilityInformationLookup, Required: true,
			Qualifiers: map[string]string{QualifierInformationScope: InformationScopeReference},
		}},
	})
	if err != nil || len(lookupPlan.Selections) != 0 {
		t.Fatalf("lookup must not be satisfied by host audit, plan=%#v err=%v", lookupPlan, err)
	}
}

func TestReviewedHostAuditIsAbsentWithoutReader(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "audit", Capability: CapabilityAuditRead, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("audit without reader must stay unmet, plan=%#v err=%v", plan, err)
	}
	clockPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-clock", TurnID: "turn-clock", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "clock", Capability: CapabilityCurrentTime, Required: true}},
	})
	if err != nil || len(clockPlan.Selections) != 1 {
		t.Fatalf("clock must still plan without an audit reader, plan=%#v err=%v", clockPlan, err)
	}
}

func TestProjectReviewedHostAuditRejectsChannelFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostAuditProvider(&fakeHostAuditReader{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityAuditRead {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["query"]; !ok || len(props) != 1 {
		t.Fatalf("audit schema=%#v", props)
	}
	for _, key := range []string{"channel", "destination", "group_name", "path", "tool_name", "tenant_id", "user_id"} {
		if _, ok := props[key]; ok {
			t.Fatalf("audit schema leaked %s", key)
		}
	}
}

func TestServiceReviewedHostAuditReaderScopesToPrincipal(t *testing.T) {
	executor := &CoreAgentExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, NewMemoryStore(), executor)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	if err := svc.RecordAuditEvent(context.Background(), AuditEvent{
		TenantID: "t1", UserID: "u1", ActorType: "user", Action: "message.posted", ResourceType: "message", ResourceID: "m1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordAuditEvent(context.Background(), AuditEvent{
		TenantID: "t1", UserID: "u2", ActorType: "user", Action: "secret.audit", ResourceType: "secret", ResourceID: "s1",
	}); err != nil {
		t.Fatal(err)
	}
	reader := serviceReviewedHostAuditReader{svc: svc}
	out, err := reader.ReadReviewedHostAudit(context.Background(), Principal{TenantID: "t1", UserID: "u1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "message.posted") || strings.Contains(out, "secret.audit") {
		t.Fatalf("principal scope leaked: %s", out)
	}
	filtered, err := reader.ReadReviewedHostAudit(context.Background(), Principal{TenantID: "t1", UserID: "u1"}, "message.posted")
	if err != nil || !strings.Contains(filtered, "message.posted") {
		t.Fatalf("query filter=%q err=%v", filtered, err)
	}
	if _, err := reader.ReadReviewedHostAudit(context.Background(), Principal{}, ""); err == nil {
		t.Fatal("empty principal must fail closed")
	}
}

func TestServiceReviewedHostAuditReaderIncludesPrincipalConversations(t *testing.T) {
	store := NewMemoryStore()
	executor := &CoreAgentExecutor{}
	svc, err := NewService(Config{DataRoot: t.TempDir(), TokenSecret: "test"}, store, executor)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	now := time.Now().UTC()
	if err := store.SaveInstance(Instance{ID: "i1", TenantID: "t1", UserID: "u1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(Session{ID: "s1", TenantID: "t1", UserID: "u1", InstanceID: "i1"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(Message{
		ID: "m1", SessionID: "s1", TenantID: "t1", UserID: "u1",
		Role: MessageRoleUser, Content: "our previous routing discussion", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInstance(Instance{ID: "i2", TenantID: "t1", UserID: "u2"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(Session{ID: "s2", TenantID: "t1", UserID: "u2", InstanceID: "i2"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(Message{
		ID: "m2", SessionID: "s2", TenantID: "t1", UserID: "u2",
		Role: MessageRoleUser, Content: "secret conversation about billing", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordAuditEvent(context.Background(), AuditEvent{
		TenantID: "t1", UserID: "u1", ActorType: "user", Action: "message.posted", ResourceType: "message", ResourceID: "m1",
	}); err != nil {
		t.Fatal(err)
	}
	reader := serviceReviewedHostAuditReader{svc: svc}
	out, err := reader.ReadReviewedHostAudit(context.Background(), Principal{TenantID: "t1", UserID: "u1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "message.posted") || !strings.Contains(out, "our previous routing discussion") {
		t.Fatalf("empty query must include events and conversations: %s", out)
	}
	if strings.Contains(out, "secret conversation") || strings.Contains(out, "billing") {
		t.Fatalf("conversation scope leaked: %s", out)
	}
	filtered, err := reader.ReadReviewedHostAudit(context.Background(), Principal{TenantID: "t1", UserID: "u1"}, "routing")
	if err != nil || !strings.Contains(filtered, "our previous routing discussion") {
		t.Fatalf("query filter=%q err=%v", filtered, err)
	}
	if strings.Contains(filtered, "secret conversation") {
		t.Fatalf("query filter leaked other principal: %s", filtered)
	}
}
