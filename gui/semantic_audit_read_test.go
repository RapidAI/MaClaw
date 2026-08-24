package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/security"
	"github.com/RapidAI/CodeClaw/corelib/session"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func auditReadClassification() *intent.ClassificationResult {
	return &intent.ClassificationResult{
		Primary:    intent.LabelAuditRead,
		Confidence: .98,
		ToolNames:  []string{"session_search", "check_health", "query_audit_log"},
	}
}

func TestIMSemanticAuditReadUsesClosedHostAdapter(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelAuditRead)}
	h.semanticTrustedAuditRead = func(userID, query string) (string, error) {
		t.Fatalf("planning must not execute the reader user=%q query=%q", userID, query)
		return "", nil
	}
	registerBuiltinTools(h.registry, h)
	registerNonCodeTools(h.registry, &App{testHomeDir: t.TempDir()})
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "审计日志", "lansenger", "root-audit", "turn-audit", auditReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v surface=%#v err=%v", defs, handled, surface, err)
	}
	selection := surface.plan.Selections[0]
	if selection.AdapterName != semanticTrustedAuditReadAdapter || selection.FitProof.MatchedCapability != tool.CapabilitySecurityAuditRead {
		t.Fatalf("selection=%+v", selection)
	}
	if semanticSelectionRequiresReceipt(selection) {
		t.Fatalf("read-only audit must not require a receipt: %+v", selection.Effects)
	}
	definition := defs[0]["function"].(map[string]interface{})
	name := extractToolName(defs[0])
	assertManagedModelName(t, name, definition, selection, "session_search", "check_health", "query_audit_log")
	properties := definition["parameters"].(map[string]interface{})["properties"].(map[string]interface{})
	if _, ok := properties["query"]; !ok || len(properties) != 1 {
		t.Fatalf("audit schema=%#v", properties)
	}
	for _, forbidden := range []string{
		"channel", "destination", "group_name", "path", "tool_name", "tenant_id", "user_id",
		"project_path", "action", "since", "until", "risk_level", "max_results", "health",
	} {
		if _, exists := properties[forbidden]; exists {
			t.Fatalf("model-facing audit schema exposed %q: %#v", forbidden, properties)
		}
	}
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(semanticTrustedAuditReadAdapter, `{}`); !strings.Contains(got, "selection_not_authorized") {
		t.Fatalf("direct adapter call=%q", got)
	}
	if got := cb.ExecuteTool(name, `{"tool_name":"bash","project_path":"C:/src"}`); !strings.Contains(got, "parameter_reserved_field") && !strings.Contains(got, "parameter_unknown_field") {
		t.Fatalf("forged audit fields=%q", got)
	}
}

func TestIMSemanticAuditReadExecutesBothSectionsWithoutKeywordBranch(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelAuditRead)}
	seen := make([]string, 0, 2)
	h.semanticTrustedAuditRead = func(userID, query string) (string, error) {
		if userID != "user-1" {
			t.Fatalf("principal=%q", userID)
		}
		seen = append(seen, query)
		return "audit events (1):\n- 2026-08-17T00:00:00Z tool=bash\n\nconversations (1):\n- 2026-08-17T00:00:00Z session=s1 hello", nil
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "历史对话", "lansenger", "root-audit-exec", "turn-audit-exec", auditReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	got := cb.ExecuteTool(name, `{"query":"health"}`)
	if !strings.Contains(got, "audit events (1):") || !strings.Contains(got, "conversations (1):") {
		t.Fatalf("bound audit=%q", got)
	}
	if strings.Contains(got, "check_health") || strings.Contains(got, "编译") {
		t.Fatalf("health query must not become project health: %q", got)
	}
	if len(seen) != 1 || seen[0] != "health" {
		t.Fatalf("reader calls=%q", seen)
	}
	if replay := cb.ExecuteTool(name, `{"query":"health"}`); !strings.Contains(replay, "invocation_grant_replayed") {
		t.Fatalf("replay=%q", replay)
	}
}

func TestIMSemanticAuditReadScopesStoresAndListsRecentWhenQueryEmpty(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := &IMMessageHandler{app: app}
	audit := h.getAuditLog()
	if audit == nil {
		t.Fatal("audit log")
	}
	now := time.Now().UTC()
	if err := audit.Log(security.AuditEntry{
		Timestamp: now.Add(-time.Minute), UserID: "user-1", ToolName: "bash", Result: "ok",
		RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow,
	}); err != nil {
		t.Fatal(err)
	}
	if err := audit.Log(security.AuditEntry{
		Timestamp: now, UserID: "user-2", ToolName: "secret_tool", Result: "denied",
		RiskLevel: security.RiskHigh, PolicyAction: security.PolicyDeny,
	}); err != nil {
		t.Fatal(err)
	}
	store := h.getSessionStore()
	if store == nil {
		t.Fatal("session store")
	}
	t.Cleanup(func() {
		_ = audit.Close()
		_ = store.Close()
	})
	if err := audit.Log(security.AuditEntry{
		Timestamp: now.Add(-30 * time.Second), SessionID: "user-1", ToolName: "local_tool", Result: "ok",
		RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow,
	}); err != nil {
		t.Fatal(err)
	}
	if err := audit.Log(security.AuditEntry{
		Timestamp: now.Add(-20 * time.Second), ToolName: "ghost_tool", Result: "process-local",
		RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow,
	}); err != nil {
		t.Fatal(err)
	}
	if err := audit.Log(security.AuditEntry{
		Timestamp: now.Add(-10 * time.Second), SessionID: "local", ToolName: "firewall_local", Result: "allow",
		RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Persist(session.SessionDocument{
		SessionID: "user-1_1001", Timestamp: now, Platform: "gui", Topic: "weather",
		FullText: "user talked about the morning weather",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Persist(session.SessionDocument{
		SessionID: "user-2_2002", Timestamp: now.Add(time.Second), Platform: "gui", Topic: "secrets",
		FullText: "other user talked about secret foreign chat",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Persist(session.SessionDocument{
		SessionID: "user_extra_3003", Timestamp: now.Add(2 * time.Second), Platform: "gui", Topic: "collision",
		FullText: "prefix collision must not leak to shorter principal",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := h.readTrustedAudit("user-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "audit events (2):") || !strings.Contains(got, "tool=bash") || !strings.Contains(got, "tool=local_tool") {
		t.Fatalf("empty query audit=%q", got)
	}
	if strings.Contains(got, "secret_tool") || strings.Contains(got, "ghost_tool") || strings.Contains(got, "firewall_local") {
		t.Fatalf("other principal or host-local process entry leaked: %q", got)
	}
	desktop, err := h.readTrustedAudit("desktop-user", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(desktop, "tool=ghost_tool") || !strings.Contains(desktop, "tool=firewall_local") {
		t.Fatalf("desktop must see host-local firewall events: %q", desktop)
	}
	if strings.Contains(desktop, "secret_tool") || strings.Contains(desktop, "tool=bash") {
		t.Fatalf("desktop must not see another principal's attributed events: %q", desktop)
	}
	if !strings.Contains(got, "conversations (1):") || !strings.Contains(got, "user-1_1001") || !strings.Contains(got, "morning weather") {
		t.Fatalf("empty query must list recent conversations: %q", got)
	}
	if strings.Contains(got, "user-2_2002") || strings.Contains(got, "secret foreign") || strings.Contains(got, "user_extra_3003") {
		t.Fatalf("other conversation leaked: %q", got)
	}
	if strings.Contains(got, "check_health") || strings.Contains(got, "project_path") || strings.Contains(got, "编译") {
		t.Fatalf("store reader leaked health surface: %q", got)
	}

	filtered, err := h.readTrustedAudit("user-1", "weather")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filtered, "audit events (0):") || !strings.Contains(filtered, "conversations (1):") || !strings.Contains(filtered, "user-1_1001") {
		t.Fatalf("query filter=%q", filtered)
	}
	if strings.Contains(filtered, "secret foreign") {
		t.Fatalf("search leaked other principal: %q", filtered)
	}
}

func TestIMSemanticAuditReadFindsOwnedConversationBehindNewerForeignSessions(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := &IMMessageHandler{app: app}
	store := h.getSessionStore()
	if store == nil {
		t.Fatal("session store")
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	if err := store.Persist(session.SessionDocument{
		SessionID: "user-1_1001", Timestamp: now, Platform: "gui", Topic: "weather",
		FullText: "user talked about the morning weather",
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err := store.Persist(session.SessionDocument{
			SessionID: "user-2_" + strconv.Itoa(2000+i),
			Timestamp: now.Add(time.Duration(i+1) * time.Second),
			Platform:  "gui",
			Topic:     "secrets",
			FullText:  "other user talked about secret foreign chat",
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := h.readTrustedAudit("user-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "user-1_1001") || !strings.Contains(got, "morning weather") {
		t.Fatalf("owned conversation hidden behind foreign recency: %q", got)
	}
	if strings.Contains(got, "secret foreign") {
		t.Fatalf("foreign conversation leaked: %q", got)
	}
}

func TestIMSemanticAuditReadKeepsOtherSectionWhenOneStoreFails(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := &IMMessageHandler{app: app}
	audit := h.getAuditLog()
	store := h.getSessionStore()
	if audit == nil || store == nil {
		t.Fatal("stores")
	}
	t.Cleanup(func() {
		_ = audit.Close()
		_ = store.Close()
	})
	now := time.Now().UTC()
	if err := audit.Log(security.AuditEntry{
		Timestamp: now, UserID: "user-1", ToolName: "bash", Result: "ok",
		RiskLevel: security.RiskLow, PolicyAction: security.PolicyAllow,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Persist(session.SessionDocument{
		SessionID: "user-1_1001", Timestamp: now, Platform: "gui", Topic: "weather",
		FullText: "user talked about the morning weather",
	}); err != nil {
		t.Fatal(err)
	}

	originalDir := audit.dir
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	audit.dir = blocked
	conversationsOnly, err := h.readTrustedAudit("user-1", "")
	if err != nil {
		t.Fatalf("event store failure must not drop conversations: %v", err)
	}
	if !strings.Contains(conversationsOnly, "audit events (unavailable):") || !strings.Contains(conversationsOnly, "conversations (1):") || !strings.Contains(conversationsOnly, "user-1_1001") {
		t.Fatalf("conversations hidden behind event store failure: %q", conversationsOnly)
	}
	if strings.Contains(conversationsOnly, "audit events (0):") {
		t.Fatalf("failed event store must not look empty: %q", conversationsOnly)
	}

	audit.dir = originalDir
	_ = store.Close()
	eventsOnly, err := h.readTrustedAudit("user-1", "")
	if err != nil {
		t.Fatalf("conversation store failure must not drop events: %v", err)
	}
	if !strings.Contains(eventsOnly, "audit events (1):") || !strings.Contains(eventsOnly, "tool=bash") || !strings.Contains(eventsOnly, "conversations (unavailable):") {
		t.Fatalf("events hidden behind conversation store failure: %q", eventsOnly)
	}
	if strings.Contains(eventsOnly, "conversations (0):") {
		t.Fatalf("failed conversation store must not look empty: %q", eventsOnly)
	}
}

func TestTrustedAuditEntryVisibleToPrincipalHostLocalIsDesktopOnly(t *testing.T) {
	owned := security.AuditEntry{UserID: "user-1", ToolName: "bash"}
	sessionOwned := security.AuditEntry{SessionID: "user-1_1001", ToolName: "local_tool"}
	hostLocal := security.AuditEntry{SessionID: "local", ToolName: "firewall_local"}
	empty := security.AuditEntry{ToolName: "ghost_tool"}
	foreign := security.AuditEntry{UserID: "user-2", ToolName: "secret_tool"}

	if !trustedAuditEntryVisibleToPrincipal(owned, "user-1") || trustedAuditEntryVisibleToPrincipal(owned, "desktop-user") {
		t.Fatal("attributed events are visible only to that principal")
	}
	if !trustedAuditEntryVisibleToPrincipal(sessionOwned, "user-1") || !trustedAuditEntryVisibleToPrincipal(sessionOwned, "desktop-user") {
		t.Fatal("empty UserID with persist-owned SessionID is visible to owner; desktop also sees unmatched host-local leftovers")
	}
	if trustedAuditEntryVisibleToPrincipal(hostLocal, "user-1") || !trustedAuditEntryVisibleToPrincipal(hostLocal, "desktop-user") || !trustedAuditEntryVisibleToPrincipal(hostLocal, "desktop-user:D:/proj") {
		t.Fatal("unstamped firewall SessionID=local is desktop host-local only")
	}
	if trustedAuditEntryVisibleToPrincipal(empty, "user-1") || !trustedAuditEntryVisibleToPrincipal(empty, "desktop-user") {
		t.Fatal("empty UserID and SessionID is desktop host-local only")
	}
	if trustedAuditEntryVisibleToPrincipal(foreign, "user-1") || trustedAuditEntryVisibleToPrincipal(foreign, "desktop-user") {
		t.Fatal("foreign UserID must stay hidden")
	}
	if trustedAuditEntryVisibleToPrincipal(hostLocal, "") || trustedAuditEntryVisibleToPrincipal(owned, "") {
		t.Fatal("missing principal must fail closed")
	}
}

func TestIMSemanticAuditReadSeesFirewallEventsForStampedPrincipal(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := &IMMessageHandler{app: app}
	audit := h.getAuditLog()
	if audit == nil {
		t.Fatal("audit log")
	}
	store := h.getSessionStore()
	t.Cleanup(func() {
		_ = audit.Close()
		if store != nil {
			_ = store.Close()
		}
	})
	h.firewall = NewSecurityFirewall(NewSecurityRiskAnalyzer(), NewPolicyEngineWithMode("developer"), audit)

	allowed, reason := h.firewall.Check("bash", map[string]interface{}{"command": "echo hi"}, &SecurityCallContext{
		SessionID: "local",
		UserID:    "user-1",
	})
	if !allowed {
		t.Fatalf("firewall check rejected: %s", reason)
	}
	allowed, reason = h.firewall.Check("write_file", map[string]interface{}{"path": "out.txt"}, &SecurityCallContext{
		SessionID: "local",
	})
	if !allowed {
		t.Fatalf("unstamped firewall check rejected: %s", reason)
	}

	owned, err := h.readTrustedAudit("user-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(owned, "tool=bash") {
		t.Fatalf("stamped principal must see own firewall event: %q", owned)
	}
	if strings.Contains(owned, "tool=write_file") {
		t.Fatalf("IM principal must not see unstamped host-local firewall event: %q", owned)
	}

	other, err := h.readTrustedAudit("user-2", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(other, "tool=bash") || strings.Contains(other, "tool=write_file") {
		t.Fatalf("other IM principal leaked firewall events: %q", other)
	}

	desktop, err := h.readTrustedAudit("desktop-user", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(desktop, "tool=write_file") {
		t.Fatalf("desktop must see unstamped host-local firewall event: %q", desktop)
	}
	if strings.Contains(desktop, "tool=bash") {
		t.Fatalf("desktop must not see another principal's stamped firewall event: %q", desktop)
	}
}

func TestTrustedAuditPrincipalFromContextPrefersCatalogPrincipal(t *testing.T) {
	if got := trustedAuditPrincipalFromContext(nil, "remote:mobile"); got != "remote:mobile" {
		t.Fatalf("fallback=%q", got)
	}
	ctx := withTrustedAuditPrincipal(context.Background(), "weixin-user")
	if got := trustedAuditPrincipalFromContext(ctx, "remote:mobile"); got != "weixin-user" {
		t.Fatalf("catalog principal=%q", got)
	}
	if got := trustedAuditPrincipalFromContext(withTrustedAuditPrincipal(context.Background(), "  "), "remote:mobile"); got != "remote:mobile" {
		t.Fatalf("blank principal must fall back: %q", got)
	}
	if got := trustedAuditPrincipalFromSecurityContext(&SecurityCallContext{UserID: "weixin-user"}, "remote:mobile"); got != "weixin-user" {
		t.Fatalf("security context principal=%q", got)
	}
	if got := trustedAuditPrincipalFromSecurityContext(&SecurityCallContext{}, "remote:mobile"); got != "remote:mobile" {
		t.Fatalf("empty security context must fall back: %q", got)
	}
}

func TestRegisteredToolApprovalAuditUsesCatalogPrincipal(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	h := &IMMessageHandler{app: app}
	audit := h.getAuditLog()
	if audit == nil {
		t.Fatal("audit log")
	}
	store := h.getSessionStore()
	t.Cleanup(func() {
		_ = audit.Close()
		if store != nil {
			_ = store.Close()
		}
	})
	h.firewall = NewSecurityFirewall(NewSecurityRiskAnalyzer(), NewPolicyEngineWithMode("developer"), audit)
	approval := storeRegisteredToolPendingApprovalForPrincipal(
		"bash",
		map[string]interface{}{"command": "echo hi"},
		"local",
		"remote:mobile",
		"weixin-user",
		security.RiskAssessment{Level: security.RiskHigh},
	)
	if approval.trustedAuditPrincipal() != "weixin-user" || approval.PolicyOwnerID != "remote:mobile" {
		t.Fatalf("approval principal=%q owner=%q", approval.trustedAuditPrincipal(), approval.PolicyOwnerID)
	}

	resp := h.handleRegisteredToolApprovalAgentViewSubmit(map[string]interface{}{
		"approved": false,
		"parameters": map[string]interface{}{
			registeredToolApprovalIDField: approval.ID,
		},
	})
	if resp == nil || resp.Error != "" {
		t.Fatalf("reject resp=%#v", resp)
	}

	owned, err := h.readTrustedAudit("weixin-user", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(owned, "tool=bash") || !strings.Contains(owned, "agent_view_approval_rejected") {
		t.Fatalf("catalog principal must see approval rejection: %q", owned)
	}
	policyOwner, err := h.readTrustedAudit("remote:mobile", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(policyOwner, "tool=bash") {
		t.Fatalf("workflow policy owner must not be the audit principal: %q", policyOwner)
	}
}

func TestIMSemanticAuditReadUsesCatalogPrincipalNotPolicyOwner(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	registry := NewToolRegistry()
	if err := registry.Register(RegisteredTool{
		Name:    "audit_probe_tool",
		Handler: func(map[string]interface{}) string { return "ok" },
	}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app, registry: registry}
	audit := h.getAuditLog()
	if audit == nil {
		t.Fatal("audit log")
	}
	store := h.getSessionStore()
	t.Cleanup(func() {
		_ = audit.Close()
		if store != nil {
			_ = store.Close()
		}
	})
	h.firewall = NewSecurityFirewall(NewSecurityRiskAnalyzer(), NewPolicyEngineWithMode("developer"), audit)

	result := h.executeToolDetailedWithRuntimeContext(
		withTrustedAuditPrincipal(context.Background(), "weixin-user"),
		"remote:mobile",
		true,
		"weixin",
		"audit_probe_tool",
		`{}`,
		"",
		nil,
	)
	if result.Outcome != toolOutcomeSucceeded {
		t.Fatalf("execute=%+v", result)
	}

	owned, err := h.readTrustedAudit("weixin-user", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(owned, "tool=audit_probe_tool") {
		t.Fatalf("catalog principal must see own firewall event: %q", owned)
	}

	policyOwner, err := h.readTrustedAudit("remote:mobile", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(policyOwner, "tool=audit_probe_tool") {
		t.Fatalf("workflow policy owner must not be the audit principal: %q", policyOwner)
	}
}

func TestTrustedAuditSessionOwnedByPrincipalRejectsPrefixCollision(t *testing.T) {
	if !trustedAuditSessionOwnedByPrincipal("user-1_1001", "user-1") || !trustedAuditSessionOwnedByPrincipal("user-1", "user-1") {
		t.Fatal("owned session must match exact principal or digit suffix")
	}
	if trustedAuditSessionOwnedByPrincipal("user_extra_3003", "user") || trustedAuditSessionOwnedByPrincipal("user-1_1001", "user") {
		t.Fatal("shorter principal must not own a longer userID session")
	}
	if trustedAuditSessionOwnedByPrincipal("", "user-1") || trustedAuditSessionOwnedByPrincipal("user-1_1001", "") {
		t.Fatal("empty ids must fail closed")
	}
}

func TestIMSemanticAuditReadRejectsDeliveryTokensAndMissingPrincipal(t *testing.T) {
	h := &IMMessageHandler{registry: NewToolRegistry(), unifiedClassifier: semanticClassifierForLabel(t, intent.LabelAuditRead)}
	h.semanticTrustedAuditRead = func(string, string) (string, error) {
		return "audit events (1):\n- [file_base64|application/pdf]AAAA\n\nconversations (0):\n", nil
	}
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "审计日志", "lansenger", "root-audit-token", "turn-audit-token", auditReadClassification(),
	)
	if err != nil || !handled || surface == nil || len(defs) < 1 {
		t.Fatalf("defs=%#v handled=%v err=%v", defs, handled, err)
	}
	name := extractToolName(defs[0])
	cb := &sharedAgentLoopCallbacks{handler: h, semanticSurface: surface}
	if got := cb.ExecuteTool(name, `{}`); !strings.Contains(got, "trusted_audit_delivery_token") {
		t.Fatalf("delivery token result=%q", got)
	}

	if _, err := h.readTrustedAudit("", "x"); err == nil || !strings.Contains(err.Error(), "trusted_audit_principal_required") {
		t.Fatalf("missing principal err=%v", err)
	}
}
