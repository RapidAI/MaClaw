package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestRulePlanFromMessageAddProvider(t *testing.T) {
	msg := "添加一个 LLM 服务商 DeepSeek，地址 https://api.deepseek.com/v1，模型 deepseek-chat，key sk-abc123456789"
	plan := rulePlanFromMessage(msg, "tenant_default", &llmservice.Registry{}, &im.LLMProviderRegistry{})
	if plan == nil {
		t.Fatal("expected plan")
	}
	if plan.Intent != "llm.provider.upsert" {
		t.Fatalf("intent = %q", plan.Intent)
	}
	if len(plan.Steps) < 2 {
		t.Fatalf("steps = %d", len(plan.Steps))
	}
	var upsert *configAgentStep
	for i := range plan.Steps {
		if plan.Steps[i].Tool == "llm.providers.upsert" {
			upsert = &plan.Steps[i]
			break
		}
	}
	if upsert == nil {
		t.Fatal("missing upsert step")
	}
	if upsert.Args["api_url"] != "https://api.deepseek.com/v1" {
		t.Fatalf("api_url = %#v", upsert.Args["api_url"])
	}
	if upsert.Args["model"] != "deepseek-chat" {
		t.Fatalf("model = %#v", upsert.Args["model"])
	}
	key := strings.TrimSpace(toString(upsert.Args["api_key"]))
	if !strings.HasPrefix(key, "sk-") {
		t.Fatalf("api_key = %#v", upsert.Args["api_key"])
	}
}

func TestRulePlanFromMessageSystemFreeTest(t *testing.T) {
	plan := rulePlanFromMessage("测试 system-free", "t1", nil, nil)
	if plan == nil || plan.Intent != "system_free.test" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestRulePlanFromMessageSystemFreeSetProvider(t *testing.T) {
	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "deepseek", Name: "DeepSeek"}}}
	plan := rulePlanFromMessage("把 system-free 换成 deepseek", "t1", nil, reg)
	if plan == nil || plan.Intent != "system_free.set_provider" {
		t.Fatalf("plan = %#v", plan)
	}
	var update *configAgentStep
	for i := range plan.Steps {
		if plan.Steps[i].Tool == "system_free.update" {
			update = &plan.Steps[i]
		}
	}
	if update == nil || update.Args["provider_id"] != "deepseek" {
		t.Fatalf("update = %#v", update)
	}
}

func TestRulePlanFromMessageUnknown(t *testing.T) {
	if plan := rulePlanFromMessage("今天天气怎么样", "t1", nil, nil); plan != nil {
		t.Fatalf("expected nil, got %#v", plan)
	}
}

func TestRulePlanInviteAndBind(t *testing.T) {
	plan := rulePlanFromMessage("Invite user alice@example.com as member", "t1", nil, nil)
	if plan == nil || plan.Intent != "users.invite.create" {
		t.Fatalf("invite plan = %#v", plan)
	}
	if plan.Steps[0].Args["email"] != "alice@example.com" || plan.Steps[0].Args["role"] != "member" {
		t.Fatalf("args = %#v", plan.Steps[0].Args)
	}

	svc := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{
		{ID: "system-free", Name: "SF"},
	}}
	bind := rulePlanFromMessage("Bind user bob@example.com to system-free", "t1", svc, nil)
	if bind == nil || bind.Intent != "llm.services.user_bind" {
		t.Fatalf("bind plan = %#v", bind)
	}
	if len(bind.MissingFields) != 0 {
		t.Fatalf("missing = %#v", bind.MissingFields)
	}
}

func TestRulePlanMailAndSmartRoute(t *testing.T) {
	mailGet := rulePlanFromMessage("show mail sender name", "t1", nil, nil)
	if mailGet == nil || mailGet.Intent != "mail.sender_name.get" {
		t.Fatalf("mail get = %#v", mailGet)
	}
	mailSet := rulePlanFromMessage("set mail sender name to Acme Hub", "t1", nil, nil)
	if mailSet == nil || mailSet.Intent != "mail.sender_name.update" {
		t.Fatalf("mail set = %#v", mailSet)
	}
	sr := rulePlanFromMessage("enable smart route", "t1", nil, nil)
	if sr == nil || sr.Intent != "smart_route_all.update" {
		t.Fatalf("smart route = %#v", sr)
	}
	if sr.Steps[0].Args["enabled"] != true {
		t.Fatalf("enabled = %#v", sr.Steps[0].Args)
	}
	reg := rulePlanFromMessage("show registration auth", "t1", nil, nil)
	if reg == nil || reg.Intent != "registration_auth.get" {
		t.Fatalf("registration auth = %#v", reg)
	}
}

func TestRulePlanTenantConfigurationOverview(t *testing.T) {
	plan := rulePlanFromMessage("show all tenant configurations", "t1", nil, nil)
	if plan == nil || plan.Intent != "tenant.configuration.overview" {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.Steps) < 20 {
		t.Fatalf("expected broad tenant settings coverage, steps = %d", len(plan.Steps))
	}
	want := map[string]bool{
		"digital_assets.settings.get":  false,
		"ve.config.get":                false,
		"security.settings.get":        false,
		"security.approval_roles.get":  false,
		"referrals.config.get":         false,
		"capability_market.policy.get": false,
	}
	for _, step := range plan.Steps {
		if _, ok := want[step.Tool]; ok {
			want[step.Tool] = true
		}
		if step.Mode != "read" {
			t.Fatalf("overview step %s mode = %q, want read", step.Tool, step.Mode)
		}
	}
	for tool, seen := range want {
		if !seen {
			t.Errorf("overview missing %s", tool)
		}
	}
}

func TestConfigAgentExtendedTenantToolCatalog(t *testing.T) {
	for _, tool := range []string{
		"digital_assets.settings.get", "digital_assets.settings.update",
		"ve.config.get", "ve.config.update",
		"security.settings.get", "security.settings.update",
		"security.default_group.get", "security.default_group.update",
		"security.approval_roles.get", "security.approval_roles.update",
		"referrals.config.get", "referrals.config.update",
		"capability_market.policy.get", "capability_market.policy.update",
	} {
		if _, ok := configAgentAllowedTools[tool]; !ok {
			t.Errorf("missing allowed tool %q", tool)
		}
		if preview := defaultAPIPreviewForTool(tool); preview == nil {
			t.Errorf("missing API preview for %q", tool)
		}
	}
}

func TestValidateAndNormalizeLLMPlanDerivesToolModesAndRisk(t *testing.T) {
	plan, err := validateAndNormalizeLLMPlan(&llmPlanDraft{
		RiskLevel: "low",
		Steps: []configAgentStep{
			{StepID: "s1", Tool: "security.settings.get", Mode: "write"},
			{StepID: "s2", Tool: "security.settings.update", Mode: "read", Args: map[string]any{"org_structure_enabled": true}, DependsOn: []string{"s1"}},
		},
	})
	if err != nil {
		t.Fatalf("normalize LLM plan: %v", err)
	}
	if plan.Steps[0].Mode != "read" || plan.Steps[1].Mode != "write" {
		t.Fatalf("modes = %#v", plan.Steps)
	}
	if plan.RiskLevel != "high" {
		t.Fatalf("risk = %q, want high for a write plan", plan.RiskLevel)
	}
}

func TestValidateAndNormalizeLLMPlanRejectsInvalidDependencies(t *testing.T) {
	_, err := validateAndNormalizeLLMPlan(&llmPlanDraft{Steps: []configAgentStep{{
		StepID: "s1", Tool: "security.settings.get", DependsOn: []string{"missing"},
	}}})
	if err == nil || !strings.Contains(err.Error(), "unknown step") {
		t.Fatalf("err = %v, want unknown dependency error", err)
	}
}

func TestValidateAndNormalizeLLMPlanRejectsNonSequentialDependencies(t *testing.T) {
	_, err := validateAndNormalizeLLMPlan(&llmPlanDraft{Steps: []configAgentStep{
		{StepID: "s1", Tool: "security.settings.get", DependsOn: []string{"s2"}},
		{StepID: "s2", Tool: "security.settings.get", DependsOn: []string{"s1"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "must appear earlier") {
		t.Fatalf("err = %v, want non-sequential dependency error", err)
	}
}

func TestConfigAgentUnmetDependencies(t *testing.T) {
	step := configAgentStep{DependsOn: []string{"s1", "s2", "s1", "  "}}
	if got := configAgentUnmetDependencies(step, map[string]bool{"s1": true}); len(got) != 1 || got[0] != "s2" {
		t.Fatalf("unmet dependencies = %#v", got)
	}
	if got := configAgentUnmetDependencies(step, map[string]bool{"s1": true, "s2": true}); len(got) != 0 {
		t.Fatalf("unmet dependencies = %#v, want none", got)
	}
}

func TestExecReferralConfigUpdateRotatesSessionEpochWhenAvailabilityChanges(t *testing.T) {
	system := &testSystemSettingsRepo{}
	tenantID := "tenant-referrals"
	initial := defaultUserReferralConfig()
	initial.SessionEpoch = "epoch-before"
	initialData, err := json.Marshal(initial)
	if err != nil {
		t.Fatalf("marshal initial referral config: %v", err)
	}
	if err := ScopedSystemSettingsForTenant(tenantID, system).Set(t.Context(), userReferralSettingsKey, string(initialData)); err != nil {
		t.Fatalf("seed referral config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/config-agent/execute", nil)
	req = req.WithContext(WithRequestTenant(req.Context(), tenantID))
	// The execute handler now passes its unscoped base repository to tools;
	// verify the tool still persists under exactly one tenant prefix.
	result, err := execReferralConfigUpdate(req, system, map[string]any{"enabled": true})
	if err != nil {
		t.Fatalf("update referral config: %v", err)
	}
	updated, ok := result.(UserReferralConfig)
	if !ok {
		t.Fatalf("result type = %T, want UserReferralConfig", result)
	}
	if !updated.Enabled || updated.SessionEpoch == "" || updated.SessionEpoch == initial.SessionEpoch {
		t.Fatalf("updated config = %#v; expected enabled config with a new session epoch", updated)
	}

	persisted, _, err := loadUserReferralConfigWithVersion(t.Context(), system, tenantID)
	if err != nil {
		t.Fatalf("load persisted referral config: %v", err)
	}
	if persisted.SessionEpoch != updated.SessionEpoch {
		t.Fatalf("persisted epoch = %q, want %q", persisted.SessionEpoch, updated.SessionEpoch)
	}
	if duplicated, err := system.Get(t.Context(), "tenant:"+tenantID+":tenant:"+tenantID+":"+userReferralSettingsKey); err != nil || duplicated != "" {
		t.Fatalf("unexpected doubly scoped referral setting = %q, err=%v", duplicated, err)
	}
}

func TestRulePlanQQBotAndBridge(t *testing.T) {
	qq := rulePlanFromMessage("show qqbot config", "t1", nil, nil)
	if qq == nil || qq.Intent != "qqbot.config.get" {
		t.Fatalf("qqbot = %#v", qq)
	}
	bridge := rulePlanFromMessage("list bridge channels", "t1", nil, nil)
	if bridge == nil || bridge.Intent != "bridge.channels.list" {
		t.Fatalf("bridge list = %#v", bridge)
	}
	tg := rulePlanFromMessage("enable telegram channel botToken 123:ABC", "t1", nil, nil)
	if tg == nil || tg.Intent != "bridge.channels.save" {
		t.Fatalf("telegram save = %#v", tg)
	}
	if tg.Steps[0].Args["id"] != "telegram" {
		t.Fatalf("channel id = %#v", tg.Steps[0].Args)
	}
	if tg.Steps[0].Args["install_npm"] == true {
		t.Fatal("install_npm should be false without install keyword")
	}
	tgInstall := rulePlanFromMessage("enable telegram channel and install botToken 123:ABC", "t1", nil, nil)
	if tgInstall == nil || tgInstall.Steps[0].Args["install_npm"] != true {
		t.Fatalf("install plan = %#v", tgInstall)
	}
}

func TestRulePlanIMAndContentAudit(t *testing.T) {
	wecom := rulePlanFromMessage("show wecom config", "t1", nil, nil)
	if wecom == nil || wecom.Intent != "wecom.config.get" {
		t.Fatalf("wecom = %#v", wecom)
	}
	ding := rulePlanFromMessage("show dingtalk config", "t1", nil, nil)
	if ding == nil || ding.Intent != "dingtalk.config.get" {
		t.Fatalf("dingtalk = %#v", ding)
	}
	audit := rulePlanFromMessage("show content audit config", "t1", nil, nil)
	if audit == nil || audit.Intent != "content_audit.config.get" {
		t.Fatalf("audit = %#v", audit)
	}
	update := rulePlanFromMessage("update content audit keywords: spam, phishing timeout_policy pass", "t1", nil, nil)
	if update == nil || update.Intent != "content_audit.config.update" {
		t.Fatalf("audit update = %#v", update)
	}
	openclaw := rulePlanFromMessage("show openclaw config", "t1", nil, nil)
	if openclaw == nil || openclaw.Intent != "openclaw_im.config.get" {
		t.Fatalf("openclaw = %#v", openclaw)
	}
}

func TestRulePlanBlockAndEnrollment(t *testing.T) {
	block := rulePlanFromMessage("Block email spam@example.com reason abuse", "t1", nil, nil)
	if block == nil || block.Intent != "users.blocklist.add" {
		t.Fatalf("block plan = %#v", block)
	}
	if block.Steps[0].Args["email"] != "spam@example.com" {
		t.Fatalf("email = %#v", block.Steps[0].Args)
	}
	list := rulePlanFromMessage("list pending enrollments", "t1", nil, nil)
	if list == nil || list.Intent != "enrollments.list_pending" {
		t.Fatalf("list plan = %#v", list)
	}
	approve := rulePlanFromMessage("Approve enrollment for alice@example.com", "t1", nil, nil)
	if approve == nil || approve.Intent != "enrollments.approve" {
		t.Fatalf("approve plan = %#v", approve)
	}
	bind := rulePlanFromMessage("manual bind user bob@example.com", "t1", nil, nil)
	if bind == nil || bind.Intent != "users.manual_bind" {
		t.Fatalf("manual bind = %#v", bind)
	}
}

func TestRefinePlanWithFollowUpFillsEmail(t *testing.T) {
	prev := rulePlanFromMessage("Invite user as admin", "t1", nil, nil)
	if prev == nil {
		// without email, still may create invite intent via 邀请
		prev = &configAgentPlan{
			Intent: "users.invite.create", TenantID: "t1", SourceMessage: "invite user as admin",
			MissingFields: []string{"email"},
			Steps: []configAgentStep{{
				Tool: "users.invite.create", Mode: "write",
				Args: map[string]any{"role": "admin"},
			}},
		}
	}
	next := refinePlanWithFollowUp(prev, "email is carol@example.com", nil, nil)
	if next == nil {
		t.Fatal("expected refined plan")
	}
	if recomputeMissingFields(next) != nil && len(recomputeMissingFields(next)) > 0 {
		// email should be filled
		email := ""
		for _, s := range next.Steps {
			if s.Tool == "users.invite.create" {
				email = toString(s.Args["email"])
			}
		}
		if email != "carol@example.com" {
			t.Fatalf("email not filled, plan=%#v missing=%v", next, recomputeMissingFields(next))
		}
	}
}

func TestConfigAgentStoreConsumeOnce(t *testing.T) {
	store := &configAgentStore{plans: map[string]*configAgentPlan{}}
	p := &configAgentPlan{
		PlanID:       "pln_x",
		ConfirmToken: "ct_y",
		AdminUserID:  "admin1",
		TenantID:     "tenant_a",
		ExpiresAt:    time.Now().Add(5 * time.Minute),
	}
	store.put(p)
	got, err := store.consume("pln_x", "ct_y", "admin1", "tenant_a")
	if err != nil || got == nil {
		t.Fatalf("consume: %v %#v", err, got)
	}
	if _, err := store.consume("pln_x", "ct_y", "admin1", "tenant_a"); err != nil {
		t.Fatalf("plan should remain available until execution begins: %v", err)
	}
	store.discard("pln_x")
	if _, err := store.consume("pln_x", "ct_y", "admin1", "tenant_a"); err == nil {
		t.Fatal("expected discarded plan to fail")
	}
}

func TestConfigAgentStoreRejectsCrossTenantConsume(t *testing.T) {
	store := &configAgentStore{plans: map[string]*configAgentPlan{}}
	store.put(&configAgentPlan{
		PlanID:       "pln_cross_tenant",
		ConfirmToken: "ct_cross_tenant",
		AdminUserID:  "admin1",
		TenantID:     "tenant_a",
		ExpiresAt:    time.Now().Add(5 * time.Minute),
	})
	if _, err := store.consume("pln_cross_tenant", "ct_cross_tenant", "admin1", "tenant_b"); err == nil {
		t.Fatal("expected cross-tenant consume to fail")
	}
	if _, err := store.consume("pln_cross_tenant", "ct_cross_tenant", "admin1", "tenant_a"); err != nil {
		t.Fatalf("matching tenant should consume plan: %v", err)
	}
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func TestRulePlanMigrationSettings(t *testing.T) {
	get := rulePlanFromMessage("show migration settings", "t1", nil, nil)
	if get == nil || get.Intent != "migration.settings.get" {
		t.Fatalf("migration get = %#v", get)
	}
	set := rulePlanFromMessage("set migration max to 200MB", "t1", nil, nil)
	if set == nil || set.Intent != "migration.settings.update" {
		t.Fatalf("migration set = %#v", set)
	}
	if set.Steps[0].Args["max_mb"] != int64(200) {
		t.Fatalf("max_mb = %#v", set.Steps[0].Args)
	}
	if set.Steps[0].Args["max_package_bytes"] != int64(200*1024*1024) {
		t.Fatalf("bytes = %#v", set.Steps[0].Args)
	}
	zh := rulePlanFromMessage("把迁移包上限设为 512MB", "t1", nil, nil)
	if zh == nil || zh.Intent != "migration.settings.update" {
		t.Fatalf("zh migration = %#v", zh)
	}
}

func TestIsBlankConfigArg(t *testing.T) {
	if !isBlankConfigArg(nil) || !isBlankConfigArg("") || !isBlankConfigArg("  ") || !isBlankConfigArg("<nil>") {
		t.Fatal("expected blanks")
	}
	if isBlankConfigArg("x") || isBlankConfigArg(0) || isBlankConfigArg(false) {
		// 0 and false are intentional values for some args; not blank.
		t.Fatal("expected non-blank for concrete values")
	}
}

func TestRecomputeMissingUserBindGroupIDs(t *testing.T) {
	// []any from JSON-like planners must count as present.
	plan := &configAgentPlan{
		Steps: []configAgentStep{{
			Tool: "llm.services.user_bind",
			Mode: "write",
			Args: map[string]any{
				"email":             "a@b.com",
				"service_group_ids": []any{"system-free", "coding"},
			},
		}},
	}
	if got := recomputeMissingFields(plan); len(got) != 0 {
		t.Fatalf("expected no missing, got %#v", got)
	}
	plan2 := &configAgentPlan{
		Steps: []configAgentStep{{
			Tool: "llm.services.user_bind",
			Mode: "write",
			Args: map[string]any{
				"email":             "a@b.com",
				"service_group_ids": []string{},
			},
		}},
	}
	if got := recomputeMissingFields(plan2); len(got) != 1 || got[0] != "service_group_id" {
		t.Fatalf("expected service_group_id missing, got %#v", got)
	}
	plan3 := &configAgentPlan{
		Steps: []configAgentStep{{
			Tool: "llm.services.user_bind",
			Mode: "write",
			Args: map[string]any{
				"email": nil,
			},
		}},
	}
	got3 := recomputeMissingFields(plan3)
	if len(got3) < 2 {
		t.Fatalf("expected email+group missing, got %#v", got3)
	}
}

func TestConfigAgentToolMeta(t *testing.T) {
	if got := configAgentToolDomain("llm.services.diagnose"); got != "llm" {
		t.Fatalf("domain = %q", got)
	}
	if got := configAgentToolDomain("users.invite.create"); got != "users" {
		t.Fatalf("domain users = %q", got)
	}
	if got := configAgentToolMode("llm.services.diagnose"); got != "read" {
		t.Fatalf("mode diagnose = %q", got)
	}
	if got := configAgentToolMode("system_free.test"); got != "probe" {
		t.Fatalf("mode test = %q", got)
	}
	if got := configAgentToolMode("llm.services.user_bind"); got != "write" {
		t.Fatalf("mode bind = %q", got)
	}
	ex := configAgentToolExample("llm.services.diagnose")
	if !strings.Contains(ex, "diagnose") || !strings.Contains(ex, "@") {
		t.Fatalf("example diagnose = %q", ex)
	}
	ex2 := configAgentToolExample("mail.sender_name.get")
	if !strings.HasPrefix(ex2, "show ") {
		t.Fatalf("example fallback = %q", ex2)
	}
}

func TestConfigAgentCatalogHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/config-agent/catalog", nil)
	w := httptest.NewRecorder()
	ConfigAgentCatalogHandler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Tools    []map[string]any `json:"tools"`
		Examples []string         `json:"examples"`
		Domains  []string         `json:"domains"`
		Count    int              `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp.Count <= 0 || len(resp.Tools) == 0 {
		t.Fatalf("empty catalog %#v", resp)
	}
	if resp.Count != len(resp.Tools) {
		t.Fatalf("count=%d tools=%d", resp.Count, len(resp.Tools))
	}
	if len(resp.Domains) == 0 {
		t.Fatal("expected domains")
	}
	if len(resp.Examples) == 0 {
		t.Fatal("expected examples")
	}
	var sawDiagnose bool
	for _, tool := range resp.Tools {
		name := fmt.Sprint(tool["name"])
		if name == "" || name == "<nil>" {
			t.Fatalf("tool missing name: %#v", tool)
		}
		if fmt.Sprint(tool["domain"]) == "" || fmt.Sprint(tool["domain"]) == "<nil>" {
			t.Fatalf("tool missing domain: %#v", tool)
		}
		mode := fmt.Sprint(tool["mode"])
		if mode != "read" && mode != "write" && mode != "probe" {
			t.Fatalf("bad mode %q for %s", mode, name)
		}
		if name == "llm.services.diagnose" {
			sawDiagnose = true
			if !strings.Contains(fmt.Sprint(tool["example"]), "diagnose") {
				t.Fatalf("diagnose example = %#v", tool["example"])
			}
			if fmt.Sprint(tool["domain"]) != "llm" {
				t.Fatalf("diagnose domain = %#v", tool["domain"])
			}
		}
	}
	if !sawDiagnose {
		t.Fatal("expected llm.services.diagnose in catalog")
	}
}

func TestConfigAgentSessionStoreExpiry(t *testing.T) {
	store := &configAgentSessionStore{sessions: map[string]*configAgentSession{}}
	store.put(&configAgentSession{
		SessionID:   "sess_live",
		AdminUserID: "a1",
		History:     []string{"hello"},
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	})
	store.put(&configAgentSession{
		SessionID:   "sess_dead",
		AdminUserID: "a1",
		History:     []string{"old"},
		ExpiresAt:   time.Now().Add(-time.Minute),
	})
	if got := store.get("sess_live"); got == nil || len(got.History) != 1 {
		t.Fatalf("live = %#v", got)
	}
	if got := store.get("sess_dead"); got != nil {
		t.Fatalf("expired should be nil, got %#v", got)
	}
	// put should purge expired
	store.put(&configAgentSession{
		SessionID:   "sess_new",
		AdminUserID: "a1",
		History:     []string{"n"},
		ExpiresAt:   time.Now().Add(time.Minute),
	})
	store.mu.Lock()
	_, deadStill := store.sessions["sess_dead"]
	store.mu.Unlock()
	if deadStill {
		t.Fatal("put should purge expired sess_dead")
	}
	store.clear("sess_live")
	if got := store.get("sess_live"); got != nil {
		t.Fatalf("cleared = %#v", got)
	}
}

// TestConfigAgentMultiTurnSessionTurns simulates incomplete plan → follow-up fill
// and verifies session History preserves each user turn (no wipe).
func TestConfigAgentMultiTurnSessionTurns(t *testing.T) {
	store := &configAgentSessionStore{sessions: map[string]*configAgentSession{}}
	// First turn: invite without email → pending plan with missing fields.
	first := rulePlanFromMessage("invite user as member", "t1", nil, nil)
	if first == nil {
		t.Fatal("expected first plan")
	}
	// Ensure missing email path (some locales may still parse).
	if len(recomputeMissingFields(first)) == 0 {
		// Force missing for the test when rule already extracted nothing useful.
		first.MissingFields = []string{"email"}
		if len(first.Steps) == 0 {
			first.Steps = []configAgentStep{{
				Tool: "users.invite.create", Mode: "write",
				Args: map[string]any{"role": "member"},
			}}
		}
	}
	sid := "sess_multi_1"
	turns := []string{"invite user as member"}
	store.put(&configAgentSession{
		SessionID:   sid,
		AdminUserID: "admin1",
		TenantID:    "t1",
		PendingPlan: first,
		History:     append([]string{}, turns...),
		ExpiresAt:   time.Now().Add(15 * time.Minute),
	})

	// Second turn: refine with email.
	follow := "email is multi@example.com"
	sess := store.get(sid)
	if sess == nil || sess.PendingPlan == nil {
		t.Fatal("expected pending session")
	}
	refined := refinePlanWithFollowUp(sess.PendingPlan, follow, nil, nil)
	if refined == nil {
		t.Fatal("expected refined plan")
	}
	turns = appendConfigAgentSessionTurn(turns, follow)
	// Only keep pending when still incomplete.
	var pending *configAgentPlan
	if filled := recomputeMissingFields(refined); len(filled) > 0 {
		refined.MissingFields = filled
		pending = refined
	}
	store.put(&configAgentSession{
		SessionID:   sid,
		AdminUserID: "admin1",
		TenantID:    "t1",
		PendingPlan: pending,
		History:     turns,
		ExpiresAt:   time.Now().Add(15 * time.Minute),
	})

	got := store.get(sid)
	if got == nil {
		t.Fatal("session gone")
	}
	if len(got.History) != 2 {
		t.Fatalf("turns = %#v", got.History)
	}
	if got.History[0] != "invite user as member" || got.History[1] != follow {
		t.Fatalf("history order = %#v", got.History)
	}
	// Email should be filled on invite step after refine.
	email := ""
	for _, s := range refined.Steps {
		if s.Tool == "users.invite.create" {
			email = toString(s.Args["email"])
		}
	}
	if email != "multi@example.com" {
		t.Fatalf("email = %q refined=%#v", email, refined)
	}
}

func TestConfigAgentHistorySearchBlob(t *testing.T) {
	blob := configAgentHistorySearchBlob(map[string]any{
		"session_turns": []any{"invite user", "email bob@corp.test"},
		"results": []any{
			map[string]any{
				"tool": "llm.services.diagnose",
				"result": map[string]any{
					"email": "alice@example.com",
				},
			},
		},
	})
	blob = strings.ToLower(blob)
	if !strings.Contains(blob, "bob@corp.test") {
		t.Fatalf("missing turn email in blob: %q", blob)
	}
	if !strings.Contains(blob, "alice@example.com") {
		t.Fatalf("missing result email in blob: %q", blob)
	}
	if !strings.Contains(blob, "llm.services.diagnose") {
		t.Fatalf("missing tool in blob: %q", blob)
	}
}

func TestAppendConfigAgentSessionTurn(t *testing.T) {
	got := appendConfigAgentSessionTurn(nil, "hello")
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("first = %#v", got)
	}
	got = appendConfigAgentSessionTurn(got, "hello")
	if len(got) != 1 {
		t.Fatalf("dedupe consecutive = %#v", got)
	}
	got = appendConfigAgentSessionTurn(got, "email a@b.com")
	if len(got) != 2 || got[1] != "email a@b.com" {
		t.Fatalf("append = %#v", got)
	}
	got = appendConfigAgentSessionTurn(got, "  ")
	if len(got) != 2 {
		t.Fatalf("empty ignored = %#v", got)
	}
}

func TestNormalizeConfigAgentHistoryAction(t *testing.T) {
	if got := normalizeConfigAgentHistoryAction("plan"); got != "plan" {
		t.Fatalf("plan = %q", got)
	}
	if got := normalizeConfigAgentHistoryAction("config_agent.execute"); got != "execute" {
		t.Fatalf("execute = %q", got)
	}
	if got := normalizeConfigAgentHistoryAction(""); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := normalizeConfigAgentHistoryAction("all"); got != "" {
		t.Fatalf("all = %q", got)
	}
}

func TestRulePlanFeishuAutoEnrollAndCardStore(t *testing.T) {
	get := rulePlanFromMessage("show feishu auto enroll", "t1", nil, nil)
	if get == nil || get.Intent != "feishu.auto_enroll.get" {
		t.Fatalf("auto enroll get = %#v", get)
	}
	set := rulePlanFromMessage("enable feishu auto enroll", "t1", nil, nil)
	if set == nil || set.Intent != "feishu.auto_enroll.update" {
		t.Fatalf("auto enroll set = %#v", set)
	}
	if set.Steps[0].Args["enabled"] != true {
		t.Fatalf("enabled = %#v", set.Steps[0].Args)
	}
	cs := rulePlanFromMessage("show card store config", "t1", nil, nil)
	if cs == nil || cs.Intent != "card_store.config.get" {
		t.Fatalf("card store get = %#v", cs)
	}
	csu := rulePlanFromMessage("enable card store", "t1", nil, nil)
	if csu == nil || csu.Intent != "card_store.config.update" {
		t.Fatalf("card store update = %#v", csu)
	}
	list := rulePlanFromMessage("list invitation codes", "t1", nil, nil)
	if list == nil || list.Intent != "invitation_codes.list" {
		t.Fatalf("invite list = %#v", list)
	}
}

func TestRulePlanInviteRequiredAndServiceList(t *testing.T) {
	st := rulePlanFromMessage("show invitation code required status", "t1", nil, nil)
	if st == nil || st.Intent != "invitation_codes.required.get" {
		t.Fatalf("required get = %#v", st)
	}
	req := rulePlanFromMessage("require invitation codes for registration", "t1", nil, nil)
	if req == nil || req.Intent != "invitation_codes.required.update" {
		t.Fatalf("required set = %#v", req)
	}
	if req.Steps[0].Args["required"] != true {
		t.Fatalf("required=true args = %#v", req.Steps[0].Args)
	}
	opt := rulePlanFromMessage("make invitation codes not required", "t1", nil, nil)
	if opt == nil || opt.Intent != "invitation_codes.required.update" {
		t.Fatalf("required off = %#v", opt)
	}
	if opt.Steps[0].Args["required"] != false {
		t.Fatalf("required=false args = %#v", opt.Steps[0].Args)
	}
	svc := rulePlanFromMessage("list service groups", "t1", nil, nil)
	if svc == nil || svc.Intent != "llm.services.list" {
		t.Fatalf("services list = %#v", svc)
	}
}

func TestSimulateUserBindDiff(t *testing.T) {
	reg := &llmservice.Registry{
		UserBindings: []llmservice.UserBinding{
			{Email: "alice@example.com", ServiceGroupIDs: []string{"coding-basic"}},
		},
	}
	diff := simulateUserBindDiff(reg, "alice@example.com", []string{"system-free"})
	cur, _ := diff["current_service_group_ids"].([]string)
	if len(cur) != 1 || cur[0] != "coding-basic" {
		t.Fatalf("current = %#v", diff["current_service_group_ids"])
	}
	added, _ := diff["added_service_group_ids"].([]string)
	if len(added) != 1 || added[0] != "system-free" {
		t.Fatalf("added = %#v", diff["added_service_group_ids"])
	}
	tgt, _ := diff["target_service_group_ids"].([]string)
	if len(tgt) != 2 {
		t.Fatalf("target = %#v", diff["target_service_group_ids"])
	}
	same := simulateUserBindDiff(reg, "alice@example.com", []string{"coding-basic"})
	if same["unchanged"] != true {
		t.Fatalf("expected unchanged, got %#v", same)
	}
}

func TestRulePlanBindIncludesDiff(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "system-free", Name: "SF"}},
		UserBindings:       []llmservice.UserBinding{{Email: "bob@example.com", ServiceGroupIDs: []string{"paid"}}},
	}
	plan := rulePlanFromMessage("Bind user bob@example.com to system-free", "t1", reg, nil)
	if plan == nil || plan.Intent != "llm.services.user_bind" {
		t.Fatalf("plan = %#v", plan)
	}
	if plan.Simulated == nil {
		t.Fatal("expected simulated bind diff")
	}
	added, _ := plan.Simulated["added_service_group_ids"].([]string)
	if len(added) != 1 || added[0] != "system-free" {
		t.Fatalf("simulated added = %#v", plan.Simulated)
	}
}

func TestRulePlanUnbindAndBindExclusion(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "system-free", Name: "SF"}},
		UserBindings:       []llmservice.UserBinding{{Email: "c@example.com", ServiceGroupIDs: []string{"system-free", "paid"}}},
	}
	u := rulePlanFromMessage("unbind user c@example.com from system-free", "t1", reg, nil)
	if u == nil || u.Intent != "llm.services.user_unbind" {
		t.Fatalf("unbind plan = %#v", u)
	}
	ids, _ := u.Steps[0].Args["service_group_ids"].([]string)
	if len(ids) != 1 || ids[0] != "system-free" {
		t.Fatalf("unbind args = %#v", u.Steps[0].Args)
	}
	rem, _ := u.Simulated["removed_service_group_ids"].([]string)
	if len(rem) != 1 || rem[0] != "system-free" {
		t.Fatalf("removed = %#v", u.Simulated)
	}
	all := rulePlanFromMessage("unbind user c@example.com from all service groups", "t1", reg, nil)
	if all == nil || all.Intent != "llm.services.user_unbind" {
		t.Fatalf("unbind all = %#v", all)
	}
	if all.Steps[0].Args["remove_all"] != true {
		t.Fatalf("remove_all = %#v", all.Steps[0].Args)
	}
}

func TestBindUnknownAssumptions(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "system-free", Name: "SF"}},
	}
	plan := rulePlanFromMessage("Bind user x@y.com to no-such-group", "t1", reg, nil)
	if plan == nil || plan.Intent != "llm.services.user_bind" {
		t.Fatalf("plan = %#v", plan)
	}
	if len(plan.Assumptions) == 0 {
		t.Fatalf("expected unknown group assumptions, plan=%#v", plan)
	}
}

func TestExtractServiceGroupIDsMulti(t *testing.T) {
	reg := &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{
			{ID: "system-free", Name: "System Free"},
			{ID: "coding-basic", Name: "Coding Basic"},
			{ID: "vip", Name: "VIP"},
		},
	}
	ids := extractServiceGroupIDs("bind user a@b.com to system-free and coding-basic", reg)
	if len(ids) < 2 {
		t.Fatalf("ids = %#v, want at least system-free + coding-basic", ids)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen["system-free"] || !seen["coding-basic"] {
		t.Fatalf("ids = %#v", ids)
	}
	list := extractServiceGroupIDs("bind user a@b.com to service groups: vip, coding-basic", reg)
	seen2 := map[string]bool{}
	for _, id := range list {
		seen2[id] = true
	}
	if !seen2["vip"] || !seen2["coding-basic"] {
		t.Fatalf("list ids = %#v", list)
	}
}

func TestRulePlanDiagnoseAndExport(t *testing.T) {
	d := rulePlanFromMessage("diagnose LLM service for alice@example.com", "t1", nil, nil)
	if d == nil || d.Intent != "llm.services.diagnose" {
		t.Fatalf("diagnose = %#v", d)
	}
	if d.Steps[0].Args["email"] != "alice@example.com" {
		t.Fatalf("email = %#v", d.Steps[0].Args)
	}
	missing := rulePlanFromMessage("diagnose LLM entitlement", "t1", nil, nil)
	if missing == nil || missing.Intent != "llm.services.diagnose" {
		t.Fatalf("diagnose missing = %#v", missing)
	}
	if len(missing.MissingFields) == 0 {
		t.Fatalf("expected missing email, got %#v", missing)
	}
	exp := rulePlanFromMessage("export invitation codes", "t1", nil, nil)
	if exp == nil || exp.Intent != "invitation_codes.export" {
		t.Fatalf("export = %#v", exp)
	}
	vip := rulePlanFromMessage("export vip invitation codes", "t1", nil, nil)
	if vip == nil || vip.Intent != "invitation_codes.export" {
		t.Fatalf("export vip = %#v", vip)
	}
	if vip.Steps[0].Args["vip_only"] != true {
		t.Fatalf("vip_only = %#v", vip.Steps[0].Args)
	}
}

func TestFilterConfigAgentHistoryItems(t *testing.T) {
	items := []map[string]any{
		{"action": "config_agent.plan", "payload": map[string]any{"intent": "migration.settings.get", "summary": "Show migration", "source_message": "show migration settings"}},
		{"action": "config_agent.execute", "payload": map[string]any{"intent": "mail.sender_name.update", "summary": "Update mail", "source_message": "set sender"}},
		{"action": "config_agent.execute", "payload": map[string]any{
			"intent": "llm.services.diagnose", "summary": "Diagnose",
			"results": []any{
				map[string]any{
					"tool": "llm.services.diagnose",
					"ok":   true,
					"result": map[string]any{
						"email": "alice@example.com",
					},
				},
			},
		}},
	}
	got := filterConfigAgentHistoryItems(items, "migration", "")
	if len(got) != 1 {
		t.Fatalf("intent filter len=%d %#v", len(got), got)
	}
	got = filterConfigAgentHistoryItems(items, "", "mail")
	if len(got) != 1 {
		t.Fatalf("q filter len=%d %#v", len(got), got)
	}
	got = filterConfigAgentHistoryItems(items, "", "alice@example.com")
	if len(got) != 1 {
		t.Fatalf("email q filter len=%d %#v", len(got), got)
	}
	got = filterConfigAgentHistoryItems(items, "llm.services.diagnose", "alice")
	if len(got) != 1 {
		t.Fatalf("intent+email filter len=%d %#v", len(got), got)
	}
	items = append(items, map[string]any{
		"action": "config_agent.plan",
		"payload": map[string]any{
			"intent":        "users.invite.create",
			"summary":       "Invite",
			"session_id":    "sess_xyz",
			"session_turns": []any{"invite user", "email bob@corp.test as member"},
		},
	})
	got = filterConfigAgentHistoryItems(items, "", "bob@corp.test")
	if len(got) != 1 {
		t.Fatalf("session_turns q filter len=%d %#v", len(got), got)
	}
	got = filterConfigAgentHistoryItems(items, "", "sess_xyz")
	if len(got) != 1 {
		t.Fatalf("session_id q filter len=%d %#v", len(got), got)
	}
}
