package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

// Config agent is a human-in-the-loop admin assistant that proposes
// configuration changes (plan → simulate → confirm → execute).

type configAgentPlan struct {
	PlanID         string              `json:"plan_id"`
	TenantID       string              `json:"tenant_id,omitempty"`
	Intent         string              `json:"intent"`
	Summary        string              `json:"summary"`
	RiskLevel      string              `json:"risk_level"`
	Assumptions    []string            `json:"assumptions,omitempty"`
	MissingFields  []string            `json:"missing_fields,omitempty"`
	Steps          []configAgentStep   `json:"steps"`
	Simulated      map[string]any      `json:"simulated,omitempty"`
	ConfirmToken   string              `json:"confirm_token,omitempty"`
	ExpiresAt      time.Time           `json:"expires_at"`
	AdminUserID    string              `json:"-"`
	CreatedAt      time.Time           `json:"created_at"`
	SourceMessage  string              `json:"source_message,omitempty"`
	Planner        string              `json:"planner,omitempty"` // rule | llm
}

type configAgentStep struct {
	StepID      string         `json:"step_id"`
	Tool        string         `json:"tool"`
	Mode        string         `json:"mode"` // read | write | probe
	Purpose     string         `json:"purpose,omitempty"`
	Args        map[string]any `json:"args,omitempty"`
	DependsOn   []string       `json:"depends_on,omitempty"`
	Optional    bool           `json:"optional,omitempty"`
	APIPreview  map[string]any `json:"api_preview,omitempty"`
}

type configAgentStore struct {
	mu    sync.Mutex
	plans map[string]*configAgentPlan
}

var globalConfigAgentStore = &configAgentStore{plans: map[string]*configAgentPlan{}}

func (s *configAgentStore) put(p *configAgentPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Drop expired entries opportunistically.
	now := time.Now()
	for id, plan := range s.plans {
		if now.After(plan.ExpiresAt) {
			delete(s.plans, id)
		}
	}
	s.plans[p.PlanID] = p
}

func (s *configAgentStore) get(id string) *configAgentPlan {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.plans[id]
	if p == nil {
		return nil
	}
	if time.Now().After(p.ExpiresAt) {
		delete(s.plans, id)
		return nil
	}
	return p
}

func (s *configAgentStore) consume(id, token, adminID string) (*configAgentPlan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.plans[id]
	if p == nil {
		return nil, fmt.Errorf("plan not found or expired")
	}
	if time.Now().After(p.ExpiresAt) {
		delete(s.plans, id)
		return nil, fmt.Errorf("plan expired")
	}
	if strings.TrimSpace(token) == "" || token != p.ConfirmToken {
		return nil, fmt.Errorf("invalid confirm token")
	}
	if p.AdminUserID != "" && adminID != "" && p.AdminUserID != adminID {
		return nil, fmt.Errorf("plan was created by a different admin")
	}
	delete(s.plans, id) // one-shot
	return p, nil
}

func newConfigAgentIDs() (planID, confirmToken string, err error) {
	var b [24]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	planID = "pln_" + hex.EncodeToString(b[:12])
	confirmToken = "ct_" + hex.EncodeToString(b[12:])
	return planID, confirmToken, nil
}

func planHash(p *configAgentPlan) string {
	raw, _ := json.Marshal(struct {
		Intent string            `json:"intent"`
		Steps  []configAgentStep `json:"steps"`
	}{Intent: p.Intent, Steps: p.Steps})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// rulePlanFromMessage builds a deterministic plan for common admin phrases.
// Returns nil when the message is not recognized (caller may try LLM).
func rulePlanFromMessage(message, tenantID string, serviceReg *llmservice.Registry, providerReg *im.LLMProviderRegistry) *configAgentPlan {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return nil
	}
	lower := strings.ToLower(msg)

	// Intent: list providers
	if (strings.Contains(lower, "list") && strings.Contains(lower, "provider")) ||
		(strings.Contains(msg, "列出") && (strings.Contains(msg, "服务商") || strings.Contains(lower, "provider"))) ||
		(strings.Contains(msg, "有哪些") && (strings.Contains(msg, "服务商") || strings.Contains(lower, "llm"))) {
		return &configAgentPlan{
			Intent:    "llm.providers.list",
			Summary:   "List configured LLM providers on this tenant",
			RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "llm.providers.get", Mode: "read",
				Purpose:    "Load provider registry",
				APIPreview: map[string]any{"method": "GET", "path": "/api/admin/llm/providers"},
			}},
			Planner: "rule",
		}
	}

	// Intent: test system-free
	if strings.Contains(lower, "test system-free") ||
		strings.Contains(msg, "测试 system-free") ||
		strings.Contains(msg, "测试system-free") ||
		(strings.Contains(lower, "system-free") && (strings.Contains(lower, "test") || strings.Contains(msg, "测"))) {
		return &configAgentPlan{
			Intent:    "system_free.test",
			Summary:   "Test the reserved system-free LLM service group connectivity",
			RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID:  "s1",
				Tool:    "system_free.test",
				Mode:    "probe",
				Purpose: "Probe system-free availability",
				APIPreview: map[string]any{
					"method": "POST",
					"path":   "/api/admin/llm/system-free/test",
				},
			}},
			Simulated: map[string]any{
				"action": "probe",
				"note":   "No configuration changes. Runs a minimal chat completion via system-free.",
			},
			Planner: "rule",
		}
	}

	// Intent: show system-free status
	if strings.Contains(lower, "system-free") &&
		(strings.Contains(lower, "status") || strings.Contains(msg, "状态") || strings.Contains(msg, "就绪")) {
		return &configAgentPlan{
			Intent:    "system_free.get",
			Summary:   "Show system-free readiness status",
			RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID:  "s1",
				Tool:    "system_free.get",
				Mode:    "read",
				Purpose: "Load system-free status",
				APIPreview: map[string]any{
					"method": "GET",
					"path":   "/api/admin/llm/system-free",
				},
			}},
			Planner: "rule",
		}
	}

	// Intent: add LLM provider — very light NL extraction
	if looksLikeAddProvider(msg, lower) {
		args := extractProviderArgs(msg)
		missing := []string{}
		if strings.TrimSpace(fmt.Sprint(args["name"])) == "" && strings.TrimSpace(fmt.Sprint(args["id"])) == "" {
			missing = append(missing, "name/id")
		}
		if strings.TrimSpace(fmt.Sprint(args["api_url"])) == "" {
			missing = append(missing, "api_url")
		}
		if strings.TrimSpace(fmt.Sprint(args["api_key"])) == "" {
			missing = append(missing, "api_key")
		}
		if strings.TrimSpace(fmt.Sprint(args["model"])) == "" {
			missing = append(missing, "model")
		}
		id := strings.TrimSpace(fmt.Sprint(args["id"]))
		if id == "" {
			id = slugID(fmt.Sprint(args["name"]))
			args["id"] = id
		}
		existingCount := 0
		if providerReg != nil {
			existingCount = len(providerReg.Providers)
		}
		plan := &configAgentPlan{
			Intent:    "llm.provider.upsert",
			Summary:   fmt.Sprintf("Add or update LLM provider %q on this tenant", id),
			RiskLevel: "medium",
			Assumptions: []string{
				"Existing providers are preserved (GET → merge → PUT)",
				"Protocol defaults to openai-compatible when omitted",
			},
			MissingFields: missing,
			Steps: []configAgentStep{
				{
					StepID:  "s1",
					Tool:    "llm.providers.get",
					Mode:    "read",
					Purpose: "Load current provider registry to avoid overwrite",
					APIPreview: map[string]any{"method": "GET", "path": "/api/admin/llm/providers"},
				},
				{
					StepID:    "s2",
					Tool:      "llm.providers.upsert",
					Mode:      "write",
					Purpose:   "Merge provider into registry and save",
					Args:      args,
					DependsOn: []string{"s1"},
					APIPreview: map[string]any{
						"method": "PUT",
						"path":   "/api/admin/llm/providers",
						"note":   "Full registry PUT after merge",
					},
				},
				{
					StepID:    "s3",
					Tool:      "llm.providers.test",
					Mode:      "probe",
					Purpose:   "Optional connectivity test for the provider",
					Args:      map[string]any{"id": id},
					DependsOn: []string{"s2"},
					Optional:  true,
					APIPreview: map[string]any{
						"method": "POST",
						"path":   "/api/admin/llm/providers/test",
					},
				},
			},
			Simulated: map[string]any{
				"before": map[string]any{"providers_count": existingCount},
				"after":  map[string]any{"providers_count": existingCount + 1, "upsert_id": id},
				"diff": []map[string]any{{
					"op":   "add_or_update",
					"path": "providers[" + id + "]",
					"value": map[string]any{
						"id":      id,
						"name":    args["name"],
						"api_url": args["api_url"],
						"model":   args["model"],
						"api_key": maskConfigAgentSecret(fmt.Sprint(args["api_key"])),
					},
				}},
			},
			Planner: "rule",
		}
		if len(missing) > 0 {
			plan.Summary += " (missing: " + strings.Join(missing, ", ") + ")"
		}
		return plan
	}

	// Intent: set system-free to use a provider
	if (strings.Contains(lower, "system-free") || strings.Contains(msg, "系统免费")) &&
		(strings.Contains(msg, "改") || strings.Contains(lower, "set") || strings.Contains(msg, "绑定") || strings.Contains(msg, "换成") || strings.Contains(msg, "使用")) {
		providerID := extractMentionedProviderID(msg, providerReg)
		args := map[string]any{}
		missing := []string{}
		if providerID == "" {
			missing = append(missing, "provider_id")
		} else {
			args["provider_id"] = providerID
			args["model"] = "auto"
		}
		return &configAgentPlan{
			Intent:        "system_free.set_provider",
			Summary:       "Update system-free model route providers (free policy kept)",
			RiskLevel:     "medium",
			MissingFields: missing,
			Steps: []configAgentStep{
				{
					StepID: "s1", Tool: "system_free.get", Mode: "read",
					Purpose: "Load current system-free config",
					APIPreview: map[string]any{"method": "GET", "path": "/api/admin/llm/system-free"},
				},
				{
					StepID: "s2", Tool: "system_free.update", Mode: "write",
					Purpose: "Set system-free auto route providers",
					Args: args, DependsOn: []string{"s1"},
					APIPreview: map[string]any{"method": "PUT", "path": "/api/admin/llm/system-free"},
				},
				{
					StepID: "s3", Tool: "system_free.test", Mode: "probe",
					Purpose: "Verify system-free after change",
					DependsOn: []string{"s2"}, Optional: true,
					APIPreview: map[string]any{"method": "POST", "path": "/api/admin/llm/system-free/test"},
				},
			},
			Simulated: map[string]any{
				"service_group_id": llmservice.SystemFreeServiceGroupID,
				"provider_id":      providerID,
				"access_policy":    "free",
			},
			Planner: "rule",
		}
	}

	// Intent: invite user by email
	if looksLikeInviteUser(msg, lower) {
		email := extractEmail(msg)
		role := extractInviteRole(msg)
		missing := []string{}
		if email == "" {
			missing = append(missing, "email")
		}
		if role == "" {
			role = "viewer"
		}
		return &configAgentPlan{
			Intent:        "users.invite.create",
			Summary:       fmt.Sprintf("Invite user %s with role %s", firstNonEmptyStr(email, "(email pending)"), role),
			RiskLevel:     "medium",
			MissingFields: missing,
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "users.invite.create", Mode: "write",
				Purpose: "Create pending email invite",
				Args:    map[string]any{"email": email, "role": role},
				APIPreview: map[string]any{"method": "POST", "path": "/api/admin/invites"},
			}},
			Simulated: map[string]any{"email": email, "role": role, "status": "pending"},
			Planner:   "rule",
		}
	}

	// Intent: list invites
	if (strings.Contains(lower, "list") && strings.Contains(lower, "invite")) ||
		(strings.Contains(msg, "邀请") && (strings.Contains(msg, "列表") || strings.Contains(msg, "查看"))) {
		return &configAgentPlan{
			Intent: "users.invite.list", Summary: "List email invites", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "users.invite.list", Mode: "read",
				Purpose: "List invites for current tenant",
				APIPreview: map[string]any{"method": "GET", "path": "/api/admin/invites"},
			}},
			Planner: "rule",
		}
	}

	// Intent: invitation code required for registration (toggle policy)
	if (strings.Contains(lower, "invitation code") || strings.Contains(msg, "邀请码") || strings.Contains(lower, "invite code")) &&
		(strings.Contains(lower, "required") || strings.Contains(lower, "require") || strings.Contains(lower, "requirement") ||
			strings.Contains(msg, "必填") || strings.Contains(msg, "需要") || strings.Contains(msg, "强制") ||
			strings.Contains(lower, "registration") || strings.Contains(msg, "注册")) &&
		!looksLikeGenerateInviteCodes(msg, lower) {
		// "show/status/is required" are read queries; avoid matching "required" as a write verb.
		isShow := strings.Contains(lower, "show") || strings.Contains(lower, "status") || strings.Contains(lower, "check") ||
			strings.Contains(msg, "查看") || strings.Contains(msg, "状态") || strings.Contains(msg, "是否") ||
			strings.HasPrefix(lower, "is ") || strings.Contains(lower, " is ")
		isUpdate := !isShow && (strings.Contains(lower, "enable") || strings.Contains(lower, "disable") ||
			strings.Contains(lower, "require ") || strings.Contains(lower, "set ") || strings.Contains(lower, "make ") ||
			strings.Contains(msg, "开启") || strings.Contains(msg, "关闭") || strings.Contains(msg, "启用") ||
			strings.Contains(msg, "禁用") || strings.Contains(msg, "设置") || strings.Contains(msg, "取消") ||
			strings.Contains(msg, "必须") || strings.Contains(msg, "不必") || strings.Contains(msg, "不需要") ||
			strings.Contains(lower, "not required") || strings.Contains(lower, "optional"))
		if isUpdate {
			required := true
			if strings.Contains(lower, "disable") || strings.Contains(lower, "not required") ||
				strings.Contains(lower, "optional") || strings.Contains(msg, "关闭") || strings.Contains(msg, "禁用") ||
				strings.Contains(msg, "取消") || strings.Contains(msg, "不必") || strings.Contains(msg, "不需要") ||
				strings.Contains(msg, "可选") {
				required = false
			}
			return &configAgentPlan{
				Intent: "invitation_codes.required.update",
				Summary: fmt.Sprintf("Set invitation_code_required=%v", required),
				RiskLevel: "medium",
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "invitation_codes.required.update", Mode: "write",
					Args: map[string]any{"required": required},
					APIPreview: defaultAPIPreviewForTool("invitation_codes.required.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "invitation_codes.required.get", Summary: "Show whether invitation codes are required", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "invitation_codes.required.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("invitation_codes.required.get"),
			}},
			Planner: "rule",
		}
	}

	// Intent: generate invitation codes
	if looksLikeGenerateInviteCodes(msg, lower) {
		count := extractPositiveInt(msg, 5)
		if count > 50 {
			count = 50
		}
		return &configAgentPlan{
			Intent:    "invitation_codes.generate",
			Summary:   fmt.Sprintf("Generate %d invitation code(s)", count),
			RiskLevel: "medium",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "invitation_codes.generate", Mode: "write",
				Purpose: "Generate invitation codes",
				Args:    map[string]any{"count": count, "validity_days": 0, "vip": false},
				APIPreview: map[string]any{"method": "POST", "path": "/api/admin/invitation-codes/generate"},
			}},
			Simulated: map[string]any{"count": count},
			Planner:   "rule",
		}
	}

	// Intent: export invitation codes (before list; export is a side-effect write)
	if (strings.Contains(lower, "invitation code") || strings.Contains(msg, "邀请码") || strings.Contains(lower, "invite code")) &&
		(strings.Contains(lower, "export") || strings.Contains(msg, "导出") || strings.Contains(msg, "下载")) &&
		!looksLikeGenerateInviteCodes(msg, lower) {
		exportedFilter := "unexported"
		if strings.Contains(lower, "all") || strings.Contains(msg, "全部") {
			exportedFilter = "all"
		} else if strings.Contains(lower, "already exported") || strings.Contains(lower, "exported only") {
			exportedFilter = "exported"
		}
		vipOnly := strings.Contains(lower, "vip")
		return &configAgentPlan{
			Intent: "invitation_codes.export", Summary: "Export unused invitation codes",
			RiskLevel: "medium",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "invitation_codes.export", Mode: "write",
				Args: map[string]any{"exported": exportedFilter, "vip_only": vipOnly},
				APIPreview: defaultAPIPreviewForTool("invitation_codes.export"),
			}},
			Simulated: map[string]any{"exported": exportedFilter, "vip_only": vipOnly, "note": "marks unexported as exported"},
			Planner:   "rule",
		}
	}

	// Intent: list invitation codes
	if (strings.Contains(lower, "invitation code") || strings.Contains(msg, "邀请码") || strings.Contains(lower, "invite code")) &&
		(strings.Contains(lower, "list") || strings.Contains(msg, "列表") || strings.Contains(msg, "查看") || strings.Contains(lower, "show")) &&
		!looksLikeGenerateInviteCodes(msg, lower) {
		status := firstMatchGroup(msg, `(?i)status\s*[:：=]?\s*(active|used|expired|disabled|all)`)
		search := firstMatchGroup(msg, `(?i)(?:search|查询|搜索)\s*[:：=]?\s*([A-Za-z0-9_\-]+)`)
		args := map[string]any{"page": 1, "page_size": 20}
		if status != "" && status != "all" {
			args["status"] = status
		}
		if search != "" {
			args["search"] = search
		}
		return &configAgentPlan{
			Intent: "invitation_codes.list", Summary: "List invitation codes", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "invitation_codes.list", Mode: "read",
				Args: args, APIPreview: defaultAPIPreviewForTool("invitation_codes.list"),
			}},
			Planner: "rule",
		}
	}

	// Intent: diagnose LLM service entitlement for a user
	if (strings.Contains(lower, "diagnose") || strings.Contains(msg, "诊断") || strings.Contains(msg, "排查") ||
		(strings.Contains(lower, "entitlement") && (strings.Contains(lower, "check") || strings.Contains(lower, "show")))) &&
		(strings.Contains(lower, "llm") || strings.Contains(lower, "service") || strings.Contains(msg, "服务") ||
			strings.Contains(msg, "额度") || strings.Contains(msg, "权限") || strings.Contains(msg, "@") ||
			strings.Contains(lower, "user")) {
		email := extractEmail(msg)
		missing := []string{}
		if email == "" {
			missing = append(missing, "email")
		}
		return &configAgentPlan{
			Intent: "llm.services.diagnose", Summary: "Diagnose LLM service entitlement for " + firstNonEmptyStr(email, "user"),
			RiskLevel: "low", MissingFields: missing,
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "llm.services.diagnose", Mode: "read",
				Args: map[string]any{"email": email},
				APIPreview: defaultAPIPreviewForTool("llm.services.diagnose"),
			}},
			Planner: "rule",
		}
	}

	// Intent: list LLM service groups (before bind)
	if !looksLikeBindServiceGroup(msg, lower) &&
		((strings.Contains(lower, "service group") || strings.Contains(lower, "model service") ||
			strings.Contains(lower, "llm service") || strings.Contains(msg, "服务组") || strings.Contains(msg, "模型服务")) &&
			(strings.Contains(lower, "list") || strings.Contains(lower, "show") || strings.Contains(msg, "列表") ||
				strings.Contains(msg, "查看") || strings.Contains(msg, "有哪些"))) {
		return &configAgentPlan{
			Intent: "llm.services.list", Summary: "List LLM model service groups", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "llm.services.list", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("llm.services.list"),
			}},
			Planner: "rule",
		}
	}

	// Intent: unbind user from service group(s) (before bind; "unbind" contains "bind")
	if looksLikeUnbindServiceGroup(msg, lower) {
		email := extractEmail(msg)
		groupIDs := extractServiceGroupIDs(msg, serviceReg)
		removeAll := strings.Contains(lower, "all") || strings.Contains(msg, "全部") ||
			strings.Contains(msg, "所有") || strings.Contains(lower, "everything") ||
			(strings.Contains(lower, "clear") && strings.Contains(lower, "bind"))
		missing := []string{}
		if email == "" {
			missing = append(missing, "email")
		}
		if !removeAll && len(groupIDs) == 0 {
			missing = append(missing, "service_group_id")
		}
		summaryGroups := strings.Join(groupIDs, ", ")
		if removeAll {
			summaryGroups = "ALL"
		} else if summaryGroups == "" {
			summaryGroups = "?"
		}
		return &configAgentPlan{
			Intent:  "llm.services.user_unbind",
			Summary: fmt.Sprintf("Unbind user %s from service group(s) %s", firstNonEmptyStr(email, "?"), summaryGroups),
			RiskLevel: "medium", MissingFields: missing,
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "llm.services.user_unbind", Mode: "write",
				Purpose: "Remove service groups from user binding",
				Args: map[string]any{
					"email": email, "service_group_ids": groupIDs, "remove_all": removeAll,
				},
				APIPreview: defaultAPIPreviewForTool("llm.services.user_unbind"),
			}},
			Simulated: simulateUserUnbindDiff(serviceReg, email, groupIDs, removeAll),
			Planner:   "rule",
		}
	}

	// Intent: bind user to service group(s)
	if looksLikeBindServiceGroup(msg, lower) {
		email := extractEmail(msg)
		groupIDs := extractServiceGroupIDs(msg, serviceReg)
		missing := []string{}
		if email == "" {
			missing = append(missing, "email")
		}
		if len(groupIDs) == 0 {
			missing = append(missing, "service_group_id")
		}
		summaryGroups := strings.Join(groupIDs, ", ")
		if summaryGroups == "" {
			summaryGroups = "?"
		}
		sim := simulateUserBindDiff(serviceReg, email, groupIDs)
		if _, unknown := validateServiceGroupIDs(serviceReg, groupIDs); len(unknown) > 0 {
			sim["unknown_service_group_ids"] = unknown
			sim["known_service_group_ids"] = availableServiceGroupIDs(serviceReg)
			// Keep plan but surface unknowns in assumptions so admin can fix before confirm.
		}
		return &configAgentPlan{
			Intent:        "llm.services.user_bind",
			Summary:       fmt.Sprintf("Bind user %s to service group(s) %s", firstNonEmptyStr(email, "?"), summaryGroups),
			RiskLevel:     "medium",
			MissingFields: missing,
			Assumptions:   bindUnknownAssumptions(serviceReg, groupIDs),
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "llm.services.user_bind", Mode: "write",
				Purpose: "Upsert user binding service_group_ids (merge)",
				Args:    map[string]any{"email": email, "service_group_ids": groupIDs},
				APIPreview: map[string]any{"method": "PUT", "path": "/api/admin/llm/services", "note": "merge user_bindings"},
			}},
			Simulated: sim,
			Planner:   "rule",
		}
	}

	// Intent: feishu config status / update
	if strings.Contains(msg, "飞书") || strings.Contains(lower, "feishu") || strings.Contains(lower, "lark") {
		if strings.Contains(lower, "auto") && (strings.Contains(lower, "enroll") || strings.Contains(msg, "入职") || strings.Contains(msg, "自动")) ||
			strings.Contains(msg, "自动入职") || strings.Contains(lower, "auto-enroll") || strings.Contains(lower, "auto_enroll") {
			isUpdate := strings.Contains(lower, "enable") || strings.Contains(lower, "disable") ||
				strings.Contains(msg, "开启") || strings.Contains(msg, "关闭") || strings.Contains(msg, "启用") ||
				strings.Contains(msg, "禁用") || strings.Contains(msg, "设置") || strings.Contains(lower, "set") ||
				strings.Contains(lower, "update") || strings.Contains(msg, "改")
			if isUpdate {
				enabled := parseEnableFlag(msg, lower, true)
				dept := firstMatchGroup(msg, `(?i)(?:department[_\s-]?id|部门)\s*[:：=]?\s*([A-Za-z0-9_\-]+)`)
				useLark := strings.Contains(lower, "lark") && !strings.Contains(msg, "飞书")
				args := map[string]any{"enabled": enabled, "use_lark": useLark}
				if dept != "" {
					args["department_id"] = dept
				}
				return &configAgentPlan{
					Intent: "feishu.auto_enroll.update", Summary: fmt.Sprintf("Set Feishu auto-enroll enabled=%v", enabled),
					RiskLevel: "medium",
					Steps: []configAgentStep{{
						StepID: "s1", Tool: "feishu.auto_enroll.update", Mode: "write",
						Args: args, APIPreview: defaultAPIPreviewForTool("feishu.auto_enroll.update"),
					}},
					Planner: "rule",
				}
			}
			return &configAgentPlan{
				Intent: "feishu.auto_enroll.get", Summary: "Show Feishu auto-enroll setting", RiskLevel: "low",
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "feishu.auto_enroll.get", Mode: "read",
					APIPreview: defaultAPIPreviewForTool("feishu.auto_enroll.get"),
				}},
				Planner: "rule",
			}
		}
		if looksLikeFeishuUpdate(msg, lower) {
			appID := firstMatchGroup(msg, `(?i)(?:app[_\s-]?id|AppID)\s*[:：=]?\s*([A-Za-z0-9_\-]+)`)
			secret := firstMatchGroup(msg, `(?i)(?:app[_\s-]?secret|secret|密钥)\s*[:：=]?\s*([A-Za-z0-9_\-\.]+)`)
			enabled := strings.Contains(lower, "enable") || strings.Contains(msg, "启用") || strings.Contains(msg, "开启")
			if strings.Contains(lower, "disable") || strings.Contains(msg, "禁用") || strings.Contains(msg, "关闭") {
				enabled = false
			}
			missing := []string{}
			if enabled && appID == "" {
				missing = append(missing, "app_id")
			}
			if enabled && secret == "" {
				missing = append(missing, "app_secret")
			}
			return &configAgentPlan{
				Intent: "feishu.config.update", Summary: "Update Feishu/Lark integration config", RiskLevel: "high",
				MissingFields: missing,
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "feishu.config.update", Mode: "write",
					Purpose: "Save Feishu app credentials",
					Args: map[string]any{
						"enabled": enabled, "app_id": appID, "app_secret": secret,
					},
					APIPreview: map[string]any{"method": "POST", "path": "/api/admin/feishu/config"},
				}},
				Planner: "rule",
			}
		}
		if strings.Contains(msg, "配置") || strings.Contains(lower, "config") || strings.Contains(msg, "状态") || strings.Contains(lower, "status") || strings.Contains(msg, "查看") {
			return &configAgentPlan{
				Intent: "feishu.config.get", Summary: "Show Feishu/Lark integration config (secret masked)", RiskLevel: "low",
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "feishu.config.get", Mode: "read",
					Purpose: "Load Feishu config",
					APIPreview: map[string]any{"method": "GET", "path": "/api/admin/feishu/config"},
				}},
				Planner: "rule",
			}
		}
	}

	// Intent: blocklist
	if looksLikeBlockUser(msg, lower) {
		email := extractEmail(msg)
		reason := firstMatchGroup(msg, `(?i)(?:reason|原因|备注)\s*[:：]?\s*(.+)`)
		missing := []string{}
		if email == "" {
			missing = append(missing, "email")
		}
		return &configAgentPlan{
			Intent: "users.blocklist.add", Summary: "Block email " + firstNonEmptyStr(email, "(pending)"),
			RiskLevel: "high", MissingFields: missing,
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "users.blocklist.add", Mode: "write",
				Purpose: "Add email to blocklist",
				Args:    map[string]any{"email": email, "reason": strings.TrimSpace(reason)},
				APIPreview: map[string]any{"method": "POST", "path": "/api/admin/blocklist"},
			}},
			Planner: "rule",
		}
	}
	if looksLikeUnblockUser(msg, lower) {
		email := extractEmail(msg)
		missing := []string{}
		if email == "" {
			missing = append(missing, "email")
		}
		return &configAgentPlan{
			Intent: "users.blocklist.remove", Summary: "Unblock email " + firstNonEmptyStr(email, "(pending)"),
			RiskLevel: "medium", MissingFields: missing,
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "users.blocklist.remove", Mode: "write",
				Args: map[string]any{"email": email},
				APIPreview: map[string]any{"method": "DELETE", "path": "/api/admin/blocklist/{email}"},
			}},
			Planner: "rule",
		}
	}
	if (strings.Contains(lower, "blocklist") || strings.Contains(msg, "黑名单")) &&
		(strings.Contains(lower, "list") || strings.Contains(msg, "列表") || strings.Contains(msg, "查看")) {
		return &configAgentPlan{
			Intent: "users.blocklist.list", Summary: "List blocked emails", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "users.blocklist.list", Mode: "read",
				APIPreview: map[string]any{"method": "GET", "path": "/api/admin/blocklist"},
			}},
			Planner: "rule",
		}
	}

	// Intent: enrollments
	if looksLikeListEnrollments(msg, lower) {
		return &configAgentPlan{
			Intent: "enrollments.list_pending", Summary: "List pending enrollments", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "enrollments.list_pending", Mode: "read",
				APIPreview: map[string]any{"method": "GET", "path": "/api/admin/enrollments/pending"},
			}},
			Planner: "rule",
		}
	}
	if looksLikeApproveEnrollment(msg, lower) {
		email := extractEmail(msg)
		id := firstMatchGroup(msg, `(?i)(?:enrollment[_\s-]?id|id)\s*[:：]?\s*([A-Za-z0-9_\-]+)`)
		missing := []string{}
		if email == "" && id == "" {
			missing = append(missing, "id_or_email")
		}
		return &configAgentPlan{
			Intent: "enrollments.approve", Summary: "Approve pending enrollment",
			RiskLevel: "high", MissingFields: missing,
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "enrollments.approve", Mode: "write",
				Args: map[string]any{"id": id, "email": email},
				APIPreview: map[string]any{"method": "POST", "path": "/api/admin/enrollments/approve"},
			}},
			Planner: "rule",
		}
	}
	if looksLikeRejectEnrollment(msg, lower) {
		email := extractEmail(msg)
		id := firstMatchGroup(msg, `(?i)(?:enrollment[_\s-]?id|id)\s*[:：]?\s*([A-Za-z0-9_\-]+)`)
		missing := []string{}
		if email == "" && id == "" {
			missing = append(missing, "id_or_email")
		}
		return &configAgentPlan{
			Intent: "enrollments.reject", Summary: "Reject pending enrollment",
			RiskLevel: "high", MissingFields: missing,
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "enrollments.reject", Mode: "write",
				Args: map[string]any{"id": id, "email": email},
				APIPreview: map[string]any{"method": "POST", "path": "/api/admin/enrollments/reject"},
			}},
			Planner: "rule",
		}
	}

	// Intent: manual bind
	if looksLikeManualBind(msg, lower) {
		email := extractEmail(msg)
		missing := []string{}
		if email == "" {
			missing = append(missing, "email")
		}
		return &configAgentPlan{
			Intent: "users.manual_bind", Summary: "Manual bind user " + firstNonEmptyStr(email, "(pending)"),
			RiskLevel: "medium", MissingFields: missing,
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "users.manual_bind", Mode: "write",
				Args: map[string]any{"email": email},
				APIPreview: map[string]any{"method": "POST", "path": "/api/admin/users/manual-bind"},
			}},
			Planner: "rule",
		}
	}

	// Intent: WeCom
	if strings.Contains(msg, "企微") || strings.Contains(msg, "企业微信") || strings.Contains(lower, "wecom") || strings.Contains(lower, "wechat work") {
		if looksLikeIMUpdate(msg, lower) {
			botID := firstMatchGroup(msg, `(?i)(?:bot[_\s-]?id|BotID)\s*[:：=]?\s*([A-Za-z0-9_\-]+)`)
			secret := firstMatchGroup(msg, `(?i)(?:secret|密钥|Secret)\s*[:：=]?\s*([A-Za-z0-9_\-\.]+)`)
			enabled := parseEnableFlag(msg, lower, true)
			missing := []string{}
			if enabled && botID == "" {
				missing = append(missing, "bot_id")
			}
			if enabled && secret == "" {
				missing = append(missing, "secret")
			}
			return &configAgentPlan{
				Intent: "wecom.config.update", Summary: "Update WeCom bot config", RiskLevel: "high",
				MissingFields: missing,
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "wecom.config.update", Mode: "write",
					Args: map[string]any{"enabled": enabled, "bot_id": botID, "secret": secret},
					APIPreview: defaultAPIPreviewForTool("wecom.config.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "wecom.config.get", Summary: "Show WeCom config (secret masked)", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "wecom.config.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("wecom.config.get"),
			}},
			Planner: "rule",
		}
	}

	// Intent: DingTalk
	if strings.Contains(msg, "钉钉") || strings.Contains(lower, "dingtalk") || strings.Contains(lower, "ding talk") {
		if looksLikeIMUpdate(msg, lower) {
			clientID := firstMatchGroup(msg, `(?i)(?:client[_\s-]?id|AppKey|app[_\s-]?key)\s*[:：=]?\s*([A-Za-z0-9_\-]+)`)
			secret := firstMatchGroup(msg, `(?i)(?:client[_\s-]?secret|app[_\s-]?secret|secret|密钥)\s*[:：=]?\s*([A-Za-z0-9_\-\.]+)`)
			enabled := parseEnableFlag(msg, lower, true)
			missing := []string{}
			if enabled && clientID == "" {
				missing = append(missing, "client_id")
			}
			if enabled && secret == "" {
				missing = append(missing, "client_secret")
			}
			return &configAgentPlan{
				Intent: "dingtalk.config.update", Summary: "Update DingTalk config", RiskLevel: "high",
				MissingFields: missing,
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "dingtalk.config.update", Mode: "write",
					Args: map[string]any{"enabled": enabled, "client_id": clientID, "client_secret": secret},
					APIPreview: defaultAPIPreviewForTool("dingtalk.config.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "dingtalk.config.get", Summary: "Show DingTalk config (secret masked)", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "dingtalk.config.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("dingtalk.config.get"),
			}},
			Planner: "rule",
		}
	}

	// Intent: OpenClaw IM
	if strings.Contains(lower, "openclaw") || strings.Contains(msg, "OpenClaw") {
		if looksLikeIMUpdate(msg, lower) {
			url := firstMatchGroup(msg, `(?i)(https?://[^\s,，;；]+)`)
			secret := firstMatchGroup(msg, `(?i)(?:secret|密钥)\s*[:：=]?\s*([A-Za-z0-9_\-\.]+)`)
			enabled := parseEnableFlag(msg, lower, true)
			return &configAgentPlan{
				Intent: "openclaw_im.config.update", Summary: "Update OpenClaw IM bridge config", RiskLevel: "medium",
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "openclaw_im.config.update", Mode: "write",
					Args: map[string]any{"enabled": enabled, "webhook_url": url, "secret": secret},
					APIPreview: defaultAPIPreviewForTool("openclaw_im.config.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "openclaw_im.config.get", Summary: "Show OpenClaw IM bridge config", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "openclaw_im.config.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("openclaw_im.config.get"),
			}},
			Planner: "rule",
		}
	}

	// Intent: QQ Bot
	if strings.Contains(lower, "qqbot") || strings.Contains(lower, "qq bot") || strings.Contains(msg, "QQ机器人") || strings.Contains(msg, "QQ 机器人") || (strings.Contains(msg, "QQ") && (strings.Contains(msg, "机器人") || strings.Contains(lower, "bot"))) {
		if looksLikeIMUpdate(msg, lower) {
			appID := firstMatchGroup(msg, `(?i)(?:app[_\s-]?id|AppID)\s*[:：=]?\s*([A-Za-z0-9_\-]+)`)
			secret := firstMatchGroup(msg, `(?i)(?:app[_\s-]?secret|secret|密钥)\s*[:：=]?\s*([A-Za-z0-9_\-\.]+)`)
			enabled := parseEnableFlag(msg, lower, true)
			missing := []string{}
			if enabled && appID == "" {
				missing = append(missing, "app_id")
			}
			if enabled && secret == "" {
				missing = append(missing, "app_secret")
			}
			return &configAgentPlan{
				Intent: "qqbot.config.update", Summary: "Update QQ Bot config", RiskLevel: "high",
				MissingFields: missing,
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "qqbot.config.update", Mode: "write",
					Args: map[string]any{"enabled": enabled, "app_id": appID, "app_secret": secret},
					APIPreview: defaultAPIPreviewForTool("qqbot.config.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "qqbot.config.get", Summary: "Show QQ Bot config (secret masked)", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "qqbot.config.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("qqbot.config.get"),
			}},
			Planner: "rule",
		}
	}

	// Intent: bridge channels
	if strings.Contains(lower, "bridge") || strings.Contains(msg, "桥接") ||
		((strings.Contains(lower, "telegram") || strings.Contains(lower, "discord") || strings.Contains(lower, "slack")) &&
			(strings.Contains(lower, "channel") || strings.Contains(msg, "频道") || strings.Contains(msg, "通道"))) {
		// save/enable specific channel
		channelID := extractBridgeChannelID(msg, lower)
		if channelID != "" && looksLikeIMUpdate(msg, lower) {
			token := firstMatchGroup(msg, `(?i)(?:bot[_\s-]?token|token)\s*[:：=]?\s*([A-Za-z0-9:_\-\.]+)`)
			fields := map[string]string{}
			if token != "" {
				fields["botToken"] = token
			}
			// discord application id
			if appID := firstMatchGroup(msg, `(?i)(?:application[_\s-]?id)\s*[:：=]?\s*([A-Za-z0-9_\-]+)`); appID != "" {
				fields["applicationId"] = appID
			}
			if appToken := firstMatchGroup(msg, `(?i)(?:app[_\s-]?token)\s*[:：=]?\s*([A-Za-z0-9_\-\.]+)`); appToken != "" {
				fields["appToken"] = appToken
			}
			enabled := parseEnableFlag(msg, lower, true)
			installNPM := strings.Contains(lower, "install") || strings.Contains(msg, "安装")
			return &configAgentPlan{
				Intent: "bridge.channels.save", Summary: "Save bridge channel " + channelID, RiskLevel: "medium",
				Assumptions: []string{
					"Channel config is saved to Hub settings",
					"npm install runs only when install_npm=true (user said install/安装)",
					"bridge config.json is regenerated when shared bridge dir is available",
				},
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "bridge.channels.save", Mode: "write",
					Args: map[string]any{
						"id": channelID, "enabled": enabled, "fields": fields,
						"install_npm": installNPM,
					},
					APIPreview: defaultAPIPreviewForTool("bridge.channels.save"),
				}},
				Planner: "rule",
			}
		}
		if strings.Contains(lower, "list") || strings.Contains(msg, "列表") || strings.Contains(msg, "查看") || strings.Contains(lower, "show") || channelID == "" {
			return &configAgentPlan{
				Intent: "bridge.channels.list", Summary: "List OpenClaw bridge channels", RiskLevel: "low",
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "bridge.channels.list", Mode: "read",
					APIPreview: defaultAPIPreviewForTool("bridge.channels.list"),
				}},
				Planner: "rule",
			}
		}
	}

	// Intent: mail sender name
	if strings.Contains(msg, "发件人") || strings.Contains(msg, "发件名") || strings.Contains(lower, "sender name") ||
		(strings.Contains(lower, "mail") && strings.Contains(lower, "sender")) ||
		(strings.Contains(msg, "邮件") && (strings.Contains(msg, "名称") || strings.Contains(msg, "显示名"))) {
		if looksLikeIMUpdate(msg, lower) || strings.Contains(msg, "改成") || strings.Contains(msg, "设为") || strings.Contains(lower, "set") {
			name := firstMatchGroup(msg, `(?i)(?:from_name|sender(?:\s+name)?|发件人|发件名|显示名)\s*[:：=]?\s*[\"']?([^\"'\n。]{1,80})`)
			if name == "" {
				// try: set sender name to XXX
				name = firstMatchGroup(msg, `(?i)(?:to|as|为|成)\s*[\"']?([^\"'\n。]{1,80})`)
			}
			missing := []string{}
			if strings.TrimSpace(name) == "" {
				missing = append(missing, "from_name")
			}
			return &configAgentPlan{
				Intent: "mail.sender_name.update", Summary: "Update tenant mail sender display name",
				RiskLevel: "low", MissingFields: missing,
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "mail.sender_name.update", Mode: "write",
					Args: map[string]any{"from_name": strings.TrimSpace(name)},
					APIPreview: defaultAPIPreviewForTool("mail.sender_name.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "mail.sender_name.get", Summary: "Show tenant mail sender display name", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "mail.sender_name.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("mail.sender_name.get"),
			}},
			Planner: "rule",
		}
	}

	// Intent: smart route all
	if strings.Contains(lower, "smart_route") || strings.Contains(lower, "smart route") || strings.Contains(msg, "智能路由") {
		if strings.Contains(lower, "enable") || strings.Contains(msg, "开启") || strings.Contains(msg, "启用") ||
			strings.Contains(lower, "disable") || strings.Contains(msg, "关闭") || strings.Contains(msg, "禁用") ||
			strings.Contains(lower, "set") || strings.Contains(msg, "设置") {
			enabled := parseEnableFlag(msg, lower, true)
			return &configAgentPlan{
				Intent: "smart_route_all.update", Summary: fmt.Sprintf("Set smart_route_all=%v", enabled), RiskLevel: "medium",
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "smart_route_all.update", Mode: "write",
					Args: map[string]any{"enabled": enabled},
					APIPreview: defaultAPIPreviewForTool("smart_route_all.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "smart_route_all.get", Summary: "Show smart_route_all toggle", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "smart_route_all.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("smart_route_all.get"),
			}},
			Planner: "rule",
		}
	}

	// Intent: registration auth
	if strings.Contains(lower, "registration auth") || strings.Contains(msg, "注册认证") ||
		(strings.Contains(msg, "注册") && (strings.Contains(msg, "方式") || strings.Contains(msg, "邮箱") || strings.Contains(msg, "手机") || strings.Contains(lower, "phone") || strings.Contains(lower, "email"))) {
		if strings.Contains(lower, "phone") || strings.Contains(msg, "手机") || strings.Contains(lower, "email") || strings.Contains(msg, "邮箱") ||
			strings.Contains(lower, "set") || strings.Contains(msg, "设置") || strings.Contains(msg, "改为") {
			method := "email"
			if strings.Contains(lower, "phone") || strings.Contains(msg, "手机") {
				method = "phone"
			}
			args := map[string]any{"method": method}
			missing := []string{}
			if method == "phone" {
				ak := firstMatchGroup(msg, `(?i)(?:access[_\s-]?key[_\s-]?id|ak)\s*[:：=]?\s*([A-Za-z0-9]+)`)
				sk := firstMatchGroup(msg, `(?i)(?:access[_\s-]?key[_\s-]?secret|sk|secret)\s*[:：=]?\s*([A-Za-z0-9]+)`)
				if ak != "" {
					args["aliyun_access_key_id"] = ak
				} else {
					missing = append(missing, "aliyun_access_key_id")
				}
				if sk != "" {
					args["aliyun_access_key_secret"] = sk
				} else {
					missing = append(missing, "aliyun_access_key_secret")
				}
			}
			return &configAgentPlan{
				Intent: "registration_auth.update", Summary: "Set registration auth method to " + method,
				RiskLevel: "high", MissingFields: missing,
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "registration_auth.update", Mode: "write",
					Args: args, APIPreview: defaultAPIPreviewForTool("registration_auth.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "registration_auth.get", Summary: "Show registration auth config", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "registration_auth.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("registration_auth.get"),
			}},
			Planner: "rule",
		}
	}

	// Intent: tenant migration package size settings
	if strings.Contains(lower, "migration") || strings.Contains(msg, "数据迁移") || strings.Contains(msg, "迁移包") ||
		(strings.Contains(msg, "迁移") && (strings.Contains(msg, "上限") || strings.Contains(msg, "大小") || strings.Contains(lower, "max"))) {
		// Avoid matching "settings" as "set"; require explicit write cues or a size value.
		_, _, hasSize := extractMigrationMaxSize(msg)
		isUpdate := hasSize ||
			strings.Contains(msg, "改成") || strings.Contains(msg, "设为") || strings.Contains(msg, "调整") ||
			strings.Contains(lower, "set to") || strings.Contains(lower, "set migration") ||
			strings.Contains(lower, "update migration") || strings.Contains(lower, "change migration") ||
			(strings.Contains(msg, "设置") && (strings.Contains(msg, "上限") || strings.Contains(msg, "大小") || hasSize))
		if isUpdate {
			maxBytes, maxMB, has := extractMigrationMaxSize(msg)
			args := map[string]any{}
			missing := []string{}
			if has {
				args["max_compressed_bytes"] = maxBytes
				args["max_mb"] = maxMB
			} else {
				missing = append(missing, "max_mb")
			}
			return &configAgentPlan{
				Intent: "migration.settings.update", Summary: "Update tenant migration max package size",
				RiskLevel: "medium", MissingFields: missing,
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "migration.settings.update", Mode: "write",
					Args: args, APIPreview: defaultAPIPreviewForTool("migration.settings.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "migration.settings.get", Summary: "Show tenant migration settings", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "migration.settings.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("migration.settings.get"),
			}},
			Planner: "rule",
		}
	}

	// Intent: card store config
	if strings.Contains(lower, "card store") || strings.Contains(lower, "card_store") || strings.Contains(msg, "卡密商店") ||
		strings.Contains(msg, "服务卡商店") || (strings.Contains(msg, "卡店") && strings.Contains(msg, "配置")) {
		isUpdate := strings.Contains(lower, "enable") || strings.Contains(lower, "disable") ||
			strings.Contains(msg, "开启") || strings.Contains(msg, "关闭") || strings.Contains(msg, "启用") ||
			strings.Contains(msg, "禁用") || strings.Contains(lower, "payment") || strings.Contains(msg, "支付") ||
			strings.Contains(lower, "set") || strings.Contains(msg, "设置")
		if isUpdate {
			args := map[string]any{}
			// only set enabled when explicit enable/disable cues present
			if strings.Contains(lower, "enable") || strings.Contains(msg, "开启") || strings.Contains(msg, "启用") ||
				strings.Contains(lower, "disable") || strings.Contains(msg, "关闭") || strings.Contains(msg, "禁用") {
				args["enabled"] = parseEnableFlag(msg, lower, true)
			}
			mode := ""
			if strings.Contains(lower, "alipay") || strings.Contains(msg, "支付宝") {
				mode = "alipay_direct"
			} else if strings.Contains(lower, "manual") || strings.Contains(lower, "personal") || strings.Contains(msg, "半自动") || strings.Contains(msg, "人工") {
				mode = "personal_semimanual"
			}
			if mode != "" {
				args["payment_mode"] = mode
			}
			missing := []string{}
			if len(args) == 0 {
				missing = append(missing, "enabled")
			}
			return &configAgentPlan{
				Intent: "card_store.config.update", Summary: "Update card store enabled/payment mode",
				RiskLevel: "medium", MissingFields: missing,
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "card_store.config.update", Mode: "write",
					Args: args, APIPreview: defaultAPIPreviewForTool("card_store.config.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "card_store.config.get", Summary: "Show card store config", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "card_store.config.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("card_store.config.get"),
			}},
			Planner: "rule",
		}
	}

	// Intent: content audit
	if strings.Contains(msg, "内容审核") || strings.Contains(lower, "content audit") || strings.Contains(lower, "content_audit") {
		if looksLikeIMUpdate(msg, lower) || strings.Contains(msg, "关键词") || strings.Contains(lower, "keyword") {
			// extract quoted keywords or comma list after 关键词
			keywordsRaw := firstMatchGroup(msg, `(?i)(?:keywords?|关键词)\s*[:：]?\s*(.+)`)
			keywords := splitKeywords(keywordsRaw)
			policy := "block"
			if strings.Contains(lower, "pass") || strings.Contains(msg, "放行") {
				policy = "pass"
			}
			timeout := extractPositiveInt(msg, 5)
			return &configAgentPlan{
				Intent: "content_audit.config.update", Summary: "Update content audit keywords/policy", RiskLevel: "medium",
				Steps: []configAgentStep{{
					StepID: "s1", Tool: "content_audit.config.update", Mode: "write",
					Args: map[string]any{
						"keywords": keywords, "timeout_seconds": timeout, "timeout_policy": policy,
					},
					APIPreview: defaultAPIPreviewForTool("content_audit.config.update"),
				}},
				Planner: "rule",
			}
		}
		return &configAgentPlan{
			Intent: "content_audit.config.get", Summary: "Show content audit config", RiskLevel: "low",
			Steps: []configAgentStep{{
				StepID: "s1", Tool: "content_audit.config.get", Mode: "read",
				APIPreview: defaultAPIPreviewForTool("content_audit.config.get"),
			}},
			Planner: "rule",
		}
	}

	_ = tenantID
	return nil
}

func looksLikeIMUpdate(msg, lower string) bool {
	// Read-only cues should not trigger write plans.
	if strings.Contains(lower, "show") || strings.Contains(lower, "list") || strings.Contains(lower, "status") ||
		strings.Contains(msg, "查看") || strings.Contains(msg, "状态") || strings.Contains(msg, "列表") {
		// still allow explicit update keywords to win
		if !(strings.Contains(lower, "update") || strings.Contains(lower, "set") || strings.Contains(msg, "更新") ||
			strings.Contains(msg, "设置") || strings.Contains(msg, "启用") || strings.Contains(lower, "enable") ||
			strings.Contains(msg, "关闭") || strings.Contains(lower, "disable") || strings.Contains(lower, "secret") ||
			strings.Contains(msg, "密钥") || strings.Contains(lower, "app_id") || strings.Contains(lower, "bot_id") ||
			strings.Contains(lower, "client_id")) {
			return false
		}
	}
	return strings.Contains(msg, "配置") || strings.Contains(msg, "设置") || strings.Contains(msg, "更新") ||
		strings.Contains(lower, "set") || strings.Contains(lower, "update") || strings.Contains(lower, "enable") ||
		strings.Contains(msg, "启用") || strings.Contains(msg, "关闭") || strings.Contains(lower, "disable") ||
		strings.Contains(lower, "secret") || strings.Contains(msg, "密钥") ||
		strings.Contains(lower, "bot_id") || strings.Contains(lower, "client_id") || strings.Contains(lower, "app_id")
}

func parseEnableFlag(msg, lower string, defaultEnabled bool) bool {
	if strings.Contains(lower, "disable") || strings.Contains(msg, "禁用") || strings.Contains(msg, "关闭") {
		return false
	}
	if strings.Contains(lower, "enable") || strings.Contains(msg, "启用") || strings.Contains(msg, "开启") {
		return true
	}
	return defaultEnabled
}

func extractBridgeChannelID(msg, lower string) string {
	known := []string{"telegram", "discord", "slack", "wechatwork", "dingtalk"}
	for _, id := range known {
		if strings.Contains(lower, id) {
			return id
		}
	}
	if strings.Contains(msg, "企微") || strings.Contains(msg, "企业微信") {
		return "wechatwork"
	}
	if strings.Contains(msg, "钉钉") {
		return "dingtalk"
	}
	return firstMatchGroup(msg, `(?i)(?:channel|频道|通道)\s*[:：]?\s*([A-Za-z0-9_\-]+)`)
}

func splitKeywords(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// strip trailing sentence noise
	raw = strings.Split(raw, "。")[0]
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；' || r == ' ' || r == '、'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(p, "\"'` ")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func looksLikeFeishuUpdate(msg, lower string) bool {
	if !(strings.Contains(msg, "飞书") || strings.Contains(lower, "feishu") || strings.Contains(lower, "lark")) {
		return false
	}
	return strings.Contains(msg, "配置") && (strings.Contains(msg, "设置") || strings.Contains(msg, "更新") || strings.Contains(lower, "set") || strings.Contains(lower, "update") || strings.Contains(lower, "enable") || strings.Contains(msg, "启用") || strings.Contains(msg, "app_id") || strings.Contains(lower, "app id") || strings.Contains(lower, "app_id"))
}

func looksLikeBlockUser(msg, lower string) bool {
	if strings.Contains(msg, "拉黑") || strings.Contains(msg, "加入黑名单") {
		return true
	}
	if strings.Contains(lower, "block") && (strings.Contains(lower, "email") || strings.Contains(msg, "@") || strings.Contains(lower, "user")) {
		return !strings.Contains(lower, "unblock") && !strings.Contains(msg, "解除")
	}
	return false
}

func looksLikeUnblockUser(msg, lower string) bool {
	return strings.Contains(lower, "unblock") || strings.Contains(msg, "解除黑名单") || strings.Contains(msg, "取消拉黑") || (strings.Contains(msg, "移出") && strings.Contains(msg, "黑名单"))
}

func looksLikeListEnrollments(msg, lower string) bool {
	if strings.Contains(msg, "注册申请") || strings.Contains(msg, "待审批") || strings.Contains(msg, "入驻申请") {
		return true
	}
	return (strings.Contains(lower, "enrollment") || strings.Contains(lower, "enroll")) &&
		(strings.Contains(lower, "list") || strings.Contains(lower, "pending") || strings.Contains(msg, "列表") || strings.Contains(msg, "查看"))
}

func looksLikeApproveEnrollment(msg, lower string) bool {
	if strings.Contains(msg, "通过") && (strings.Contains(msg, "注册") || strings.Contains(msg, "入驻") || strings.Contains(msg, "申请") || strings.Contains(lower, "enroll")) {
		return true
	}
	return strings.Contains(lower, "approve") && (strings.Contains(lower, "enroll") || strings.Contains(msg, "@") || strings.Contains(lower, "enrollment"))
}

func looksLikeRejectEnrollment(msg, lower string) bool {
	if (strings.Contains(msg, "拒绝") || strings.Contains(msg, "驳回")) && (strings.Contains(msg, "注册") || strings.Contains(msg, "入驻") || strings.Contains(msg, "申请") || strings.Contains(lower, "enroll")) {
		return true
	}
	return strings.Contains(lower, "reject") && (strings.Contains(lower, "enroll") || strings.Contains(msg, "@") || strings.Contains(lower, "enrollment"))
}

func looksLikeManualBind(msg, lower string) bool {
	if strings.Contains(msg, "手动绑定") || strings.Contains(msg, "手工绑定") {
		return true
	}
	return strings.Contains(lower, "manual bind") || (strings.Contains(lower, "bind") && strings.Contains(lower, "manual"))
}

func looksLikeInviteUser(msg, lower string) bool {
	if strings.Contains(msg, "邀请") && (strings.Contains(msg, "用户") || strings.Contains(msg, "邮箱") || strings.Contains(msg, "@")) {
		return true
	}
	if strings.Contains(lower, "invite") && (strings.Contains(lower, "user") || strings.Contains(msg, "@") || strings.Contains(lower, "email")) {
		return true
	}
	return false
}

func looksLikeGenerateInviteCodes(msg, lower string) bool {
	// Policy toggles like "make invitation codes not required" must not look like generation.
	if strings.Contains(lower, "required") || strings.Contains(lower, "requirement") ||
		strings.Contains(msg, "必填") || strings.Contains(msg, "不需要") || strings.Contains(msg, "可选") {
		return false
	}
	if strings.Contains(msg, "邀请码") && (strings.Contains(msg, "生成") || strings.Contains(msg, "创建") || strings.Contains(msg, "发")) {
		return true
	}
	if strings.Contains(lower, "invitation code") || strings.Contains(lower, "invite code") {
		return strings.Contains(lower, "generate") || strings.Contains(lower, "create") || strings.Contains(lower, "make")
	}
	return false
}

func looksLikeUnbindServiceGroup(msg, lower string) bool {
	if strings.Contains(lower, "unbind") || strings.Contains(msg, "解绑") {
		return strings.Contains(msg, "服务组") || strings.Contains(lower, "service group") ||
			strings.Contains(lower, "system-free") || strings.Contains(msg, "系统免费") ||
			strings.Contains(msg, "@") || strings.Contains(lower, "user")
	}
	if (strings.Contains(lower, "remove") || strings.Contains(msg, "移除") || strings.Contains(msg, "去掉")) &&
		(strings.Contains(msg, "服务组") || strings.Contains(lower, "service group") || strings.Contains(lower, "system-free")) {
		return true
	}
	return false
}

func looksLikeBindServiceGroup(msg, lower string) bool {
	// "unbind" contains "bind" — exclude unbind first.
	if looksLikeUnbindServiceGroup(msg, lower) {
		return false
	}
	// "manual bind" is Hub user enrollment, not LLM service binding.
	if looksLikeManualBind(msg, lower) {
		return false
	}
	if strings.Contains(msg, "绑定") && (strings.Contains(msg, "服务组") || strings.Contains(lower, "service group") || strings.Contains(lower, "system-free") ||
		(strings.Contains(msg, "@") && (strings.Contains(msg, "到") || strings.Contains(msg, "至")))) {
		return true
	}
	if strings.Contains(lower, "bind") && (strings.Contains(lower, "service group") || strings.Contains(lower, "system-free") ||
		(strings.Contains(lower, "to ") && strings.Contains(msg, "@"))) {
		return true
	}
	return false
}

func bindUnknownAssumptions(serviceReg *llmservice.Registry, groupIDs []string) []string {
	_, missing := validateServiceGroupIDs(serviceReg, groupIDs)
	if len(missing) == 0 {
		return nil
	}
	avail := availableServiceGroupIDs(serviceReg)
	return []string{
		fmt.Sprintf("unknown service group(s): %s; available: %s", strings.Join(missing, ", "), strings.Join(avail, ", ")),
	}
}

// simulateUserUnbindDiff builds current vs target preview when removing groups.
func simulateUserUnbindDiff(serviceReg *llmservice.Registry, email string, removeGroupIDs []string, removeAll bool) map[string]any {
	email = strings.ToLower(strings.TrimSpace(email))
	current := []string{}
	if serviceReg != nil && email != "" {
		for _, b := range serviceReg.UserBindings {
			if strings.EqualFold(strings.TrimSpace(b.Email), email) {
				for _, id := range b.ServiceGroupIDs {
					id = strings.TrimSpace(id)
					if id != "" {
						current = append(current, id)
					}
				}
				break
			}
		}
	}
	removed := []string{}
	target := []string{}
	if removeAll {
		removed = append(removed, current...)
	} else {
		rm := map[string]struct{}{}
		for _, id := range removeGroupIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				rm[id] = struct{}{}
			}
		}
		for _, id := range current {
			if _, drop := rm[id]; drop {
				removed = append(removed, id)
				continue
			}
			target = append(target, id)
		}
	}
	return map[string]any{
		"email":                      email,
		"current_service_group_ids":  current,
		"requested_service_group_ids": removeGroupIDs,
		"removed_service_group_ids":  removed,
		"target_service_group_ids":   target,
		"remove_all":                 removeAll,
		"unchanged":                  len(removed) == 0,
		"merge":                      false,
	}
}

// simulateUserBindDiff builds a current vs target service-group binding preview.
// Binding is merge-semantics: target = current ∪ requested.
func simulateUserBindDiff(serviceReg *llmservice.Registry, email string, addGroupIDs []string) map[string]any {
	email = strings.ToLower(strings.TrimSpace(email))
	current := []string{}
	if serviceReg != nil && email != "" {
		for _, b := range serviceReg.UserBindings {
			if strings.EqualFold(strings.TrimSpace(b.Email), email) {
				for _, id := range b.ServiceGroupIDs {
					id = strings.TrimSpace(id)
					if id != "" {
						current = append(current, id)
					}
				}
				break
			}
		}
	}
	seen := map[string]struct{}{}
	for _, id := range current {
		seen[id] = struct{}{}
	}
	added := []string{}
	for _, id := range addGroupIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		added = append(added, id)
	}
	target := make([]string, 0, len(seen))
	for id := range seen {
		target = append(target, id)
	}
	// stable-ish order: current first, then added
	ordered := append([]string{}, current...)
	ordered = append(ordered, added...)
	return map[string]any{
		"email":                     email,
		"current_service_group_ids": current,
		"requested_service_group_ids": addGroupIDs,
		"added_service_group_ids":   added,
		"target_service_group_ids":  ordered,
		"unchanged":                 len(added) == 0 && email != "" && len(addGroupIDs) > 0,
		"merge":                     true,
	}
}

func extractEmail(msg string) string {
	return firstMatchGroup(msg, `(?i)\b([A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,})\b`)
}

func extractInviteRole(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "admin") || strings.Contains(msg, "管理员"):
		return "admin"
	case strings.Contains(lower, "member") || strings.Contains(msg, "成员"):
		return "member"
	case strings.Contains(lower, "viewer") || strings.Contains(msg, "访客") || strings.Contains(msg, "只读"):
		return "viewer"
	default:
		return ""
	}
}


func extractMigrationMaxSize(msg string) (maxBytes int64, maxMB int64, ok bool) {
	// Prefer explicit MB: 200MB / 200 MB / 200兆
	if g := firstMatchGroup(msg, `(?i)(\d+(?:\.\d+)?)\s*(?:mb|mib|兆)`); g != "" {
		if n, err := strconv.ParseFloat(g, 64); err == nil && n > 0 {
			mb := int64(n + 0.5)
			if mb < 1 {
				mb = 1
			}
			return mb * 1024 * 1024, mb, true
		}
	}
	// GB: 1GB
	if g := firstMatchGroup(msg, `(?i)(\d+(?:\.\d+)?)\s*(?:gb|gib)`); g != "" {
		if n, err := strconv.ParseFloat(g, 64); err == nil && n > 0 {
			mb := int64(n*1024 + 0.5)
			if mb < 1 {
				mb = 1
			}
			return mb * 1024 * 1024, mb, true
		}
	}
	// max_compressed_bytes=...
	if g := firstMatchGroup(msg, `(?i)max[_ ]?compressed[_ ]?bytes\s*[=:：]?\s*(\d+)`); g != "" {
		if n, err := strconv.ParseInt(g, 10, 64); err == nil && n > 0 {
			return n, n / (1024 * 1024), true
		}
	}
	// bare number after 上限/max
	if g := firstMatchGroup(msg, `(?i)(?:上限|max(?:imum)?(?:\s+size)?|大小)\s*[=:：]?\s*(\d+)`); g != "" {
		if n, err := strconv.ParseInt(g, 10, 64); err == nil && n > 0 {
			// treat bare numbers under 2048 as MB, larger as bytes
			if n <= 2048 {
				return n * 1024 * 1024, n, true
			}
			return n, n / (1024 * 1024), true
		}
	}
	return 0, 0, false
}

func extractPositiveInt(msg string, def int) int {
	re := regexp.MustCompile(`(?i)(\d+)\s*(?:个|份|codes?|个码)?`)
	m := re.FindStringSubmatch(msg)
	if len(m) < 2 {
		return def
	}
	n := 0
	for _, ch := range m[1] {
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	if n <= 0 {
		return def
	}
	return n
}

func extractServiceGroupID(msg string, serviceReg *llmservice.Registry) string {
	ids := extractServiceGroupIDs(msg, serviceReg)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// extractServiceGroupIDs collects all mentioned service group ids (multi-bind).
func extractServiceGroupIDs(msg string, serviceReg *llmservice.Registry) []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "system-free") || strings.Contains(msg, "系统免费") {
		add(llmservice.SystemFreeServiceGroupID)
	}
	if serviceReg != nil {
		for _, g := range serviceReg.ModelServiceGroups {
			id := strings.TrimSpace(g.ID)
			name := strings.TrimSpace(g.Name)
			if id != "" && strings.Contains(lower, strings.ToLower(id)) {
				add(id)
				continue
			}
			// Avoid matching very short names as false positives.
			if name != "" && len([]rune(name)) >= 2 && strings.Contains(lower, strings.ToLower(name)) {
				add(id)
			}
		}
	}
	// Explicit list after "service group(s): a, b" / "服务组 a、b"
	if raw := firstMatchGroup(msg, `(?i)(?:服务组|service[_\s-]?groups?)\s*[:：]?\s*([A-Za-z0-9._\-,，、\s]+)`); raw != "" {
		for _, p := range splitKeywords(raw) {
			add(p)
		}
	}
	// "to system-free, coding-basic" / "到 system-free 和 coding-basic"
	if raw := firstMatchGroup(msg, `(?i)(?:to|到|至)\s+(.+)$`); raw != "" {
		// Drop email if present in the tail.
		raw = regexp.MustCompile(`(?i)[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}`).ReplaceAllString(raw, " ")
		raw = strings.ReplaceAll(raw, " and ", ",")
		raw = strings.ReplaceAll(raw, " 和 ", ",")
		raw = strings.ReplaceAll(raw, "与", ",")
		for _, p := range splitKeywords(raw) {
			// Keep slug-like tokens only.
			if re := regexp.MustCompile(`^[A-Za-z0-9._\-]+$`); re.MatchString(p) {
				add(p)
			}
		}
	}
	return out
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// refinePlanWithFollowUp fills missing fields on a previous incomplete plan
// using a follow-up user message (multi-turn completion).
func refinePlanWithFollowUp(prev *configAgentPlan, followUp string, serviceReg *llmservice.Registry, providerReg *im.LLMProviderRegistry) *configAgentPlan {
	if prev == nil || strings.TrimSpace(followUp) == "" {
		return nil
	}
	// Prefer re-planning with combined text for richer extraction.
	combined := strings.TrimSpace(prev.SourceMessage + "\n" + followUp)
	if plan := rulePlanFromMessage(combined, prev.TenantID, serviceReg, providerReg); plan != nil {
		plan.SourceMessage = combined
		return plan
	}
	// Fallback: patch known tools from follow-up only.
	next := *prev
	next.Steps = append([]configAgentStep(nil), prev.Steps...)
	next.MissingFields = nil
	email := extractEmail(followUp)
	role := extractInviteRole(followUp)
	providerArgs := extractProviderArgs(followUp)
	groupID := extractServiceGroupID(followUp, serviceReg)

	for i := range next.Steps {
		args := next.Steps[i].Args
		if args == nil {
			args = map[string]any{}
		}
		switch next.Steps[i].Tool {
		case "users.invite.create":
			if email != "" {
				args["email"] = email
			}
			if role != "" {
				args["role"] = role
			}
		case "llm.providers.upsert":
			for k, v := range providerArgs {
				if strings.TrimSpace(fmt.Sprint(v)) != "" {
					args[k] = v
				}
			}
		case "llm.services.user_bind", "llm.services.user_unbind":
			if email != "" {
				args["email"] = email
			}
			if ids := extractServiceGroupIDs(followUp, serviceReg); len(ids) > 0 {
				args["service_group_ids"] = ids
			} else if groupID != "" {
				args["service_group_ids"] = []string{groupID}
			}
			if next.Steps[i].Tool == "llm.services.user_unbind" {
				fl := strings.ToLower(followUp)
				if strings.Contains(fl, "all") || strings.Contains(followUp, "全部") || strings.Contains(followUp, "所有") {
					args["remove_all"] = true
				}
			}
		case "system_free.update":
			if pid := extractMentionedProviderID(followUp, providerReg); pid != "" {
				args["provider_id"] = pid
			}
		case "users.blocklist.add", "users.blocklist.remove", "users.manual_bind":
			if email != "" {
				args["email"] = email
			}
		case "enrollments.approve", "enrollments.reject":
			if email != "" {
				args["email"] = email
			}
			if id := firstMatchGroup(followUp, `(?i)(?:enrollment[_\s-]?id|id)\s*[:：]?\s*([A-Za-z0-9_\-]+)`); id != "" {
				args["id"] = id
			}
		case "feishu.config.update":
			if appID := firstMatchGroup(followUp, `(?i)(?:app[_\s-]?id|AppID)\s*[:：=]?\s*([A-Za-z0-9_\-]+)`); appID != "" {
				args["app_id"] = appID
			}
			if secret := firstMatchGroup(followUp, `(?i)(?:app[_\s-]?secret|secret|密钥)\s*[:：=]?\s*([A-Za-z0-9_\-\.]+)`); secret != "" {
				args["app_secret"] = secret
			}
		case "feishu.auto_enroll.update":
			if strings.Contains(strings.ToLower(followUp), "enable") || strings.Contains(followUp, "开启") || strings.Contains(followUp, "启用") {
				args["enabled"] = true
			}
			if strings.Contains(strings.ToLower(followUp), "disable") || strings.Contains(followUp, "关闭") || strings.Contains(followUp, "禁用") {
				args["enabled"] = false
			}
			if dept := firstMatchGroup(followUp, `(?i)(?:department[_\s-]?id|部门)\s*[:：=]?\s*([A-Za-z0-9_\-]+)`); dept != "" {
				args["department_id"] = dept
			}
		case "card_store.config.update":
			if strings.Contains(strings.ToLower(followUp), "enable") || strings.Contains(followUp, "开启") || strings.Contains(followUp, "启用") {
				args["enabled"] = true
			}
			if strings.Contains(strings.ToLower(followUp), "disable") || strings.Contains(followUp, "关闭") || strings.Contains(followUp, "禁用") {
				args["enabled"] = false
			}
			if strings.Contains(strings.ToLower(followUp), "alipay") || strings.Contains(followUp, "支付宝") {
				args["payment_mode"] = "alipay_direct"
			}
			if strings.Contains(strings.ToLower(followUp), "manual") || strings.Contains(followUp, "半自动") {
				args["payment_mode"] = "personal_semimanual"
			}
		case "migration.settings.update":
			if maxBytes, maxMB, ok := extractMigrationMaxSize(followUp); ok {
				args["max_compressed_bytes"] = maxBytes
				args["max_mb"] = maxMB
			}
		case "mail.sender_name.update":
			if name := firstMatchGroup(followUp, `(?i)(?:from_name|sender(?:\s+name)?|发件人|发件名|显示名)\s*[:：=]?\s*[\"']?([^\"'\n。]{1,80})`); name != "" {
				args["from_name"] = strings.TrimSpace(name)
			}
		case "invitation_codes.required.update":
			fl := strings.ToLower(followUp)
			if strings.Contains(fl, "require") || strings.Contains(fl, "enable") || strings.Contains(followUp, "必须") ||
				strings.Contains(followUp, "开启") || strings.Contains(followUp, "启用") || strings.Contains(followUp, "需要") {
				args["required"] = true
			}
			if strings.Contains(fl, "not required") || strings.Contains(fl, "optional") || strings.Contains(fl, "disable") ||
				strings.Contains(followUp, "不必") || strings.Contains(followUp, "不需要") || strings.Contains(followUp, "可选") ||
				strings.Contains(followUp, "关闭") || strings.Contains(followUp, "取消") {
				args["required"] = false
			}
		case "llm.services.diagnose":
			if email != "" {
				args["email"] = email
			}
		case "invitation_codes.export":
			fl := strings.ToLower(followUp)
			if strings.Contains(fl, "all") || strings.Contains(followUp, "全部") {
				args["exported"] = "all"
			}
			if strings.Contains(fl, "vip") {
				args["vip_only"] = true
			}
		}
		next.Steps[i].Args = args
	}
	next.MissingFields = recomputeMissingFields(&next)
	next.SourceMessage = combined
	next.Planner = firstNonEmptyStr(prev.Planner, "rule")
	return &next
}

func recomputeMissingFields(plan *configAgentPlan) []string {
	if plan == nil {
		return nil
	}
	missing := []string{}
	for _, step := range plan.Steps {
		args := step.Args
		if args == nil {
			args = map[string]any{}
		}
		switch step.Tool {
		case "users.invite.create":
			if isBlankConfigArg(args["email"]) {
				missing = append(missing, "email")
			}
		case "llm.providers.upsert":
			for _, k := range []string{"api_url", "api_key", "model"} {
				if isBlankConfigArg(args[k]) {
					missing = append(missing, k)
				}
			}
			if isBlankConfigArg(args["id"]) && isBlankConfigArg(args["name"]) {
				missing = append(missing, "name/id")
			}
		case "llm.services.diagnose":
			if isBlankConfigArg(args["email"]) {
				missing = append(missing, "email")
			}
		case "llm.services.user_unbind":
			if isBlankConfigArg(args["email"]) {
				missing = append(missing, "email")
			}
			removeAll := false
			if b, ok := args["remove_all"].(bool); ok {
				removeAll = b
			}
			if !removeAll {
				if len(normalizeServiceGroupIDsArg(args["service_group_ids"])) == 0 {
					missing = append(missing, "service_group_id")
				}
			}
		case "llm.services.user_bind":
			if isBlankConfigArg(args["email"]) {
				missing = append(missing, "email")
			}
			// Accept []string, []any, or comma-separated string from planners.
			if len(normalizeServiceGroupIDsArg(args["service_group_ids"])) == 0 {
				missing = append(missing, "service_group_id")
			}
		case "system_free.update":
			if isBlankConfigArg(args["provider_id"]) {
				missing = append(missing, "provider_id")
			}
		case "users.blocklist.add", "users.blocklist.remove", "users.manual_bind":
			if isBlankConfigArg(args["email"]) {
				missing = append(missing, "email")
			}
		case "enrollments.approve", "enrollments.reject":
			if isBlankConfigArg(args["id"]) && isBlankConfigArg(args["email"]) {
				missing = append(missing, "id_or_email")
			}
		case "feishu.config.update":
			enabled := false
			if b, ok := args["enabled"].(bool); ok {
				enabled = b
			}
			if enabled {
				if isBlankConfigArg(args["app_id"]) {
					missing = append(missing, "app_id")
				}
				if isBlankConfigArg(args["app_secret"]) {
					missing = append(missing, "app_secret")
				}
			}
		case "wecom.config.update":
			enabled := false
			if b, ok := args["enabled"].(bool); ok {
				enabled = b
			}
			if enabled {
				if isBlankConfigArg(args["bot_id"]) {
					missing = append(missing, "bot_id")
				}
				if isBlankConfigArg(args["secret"]) {
					missing = append(missing, "secret")
				}
			}
		case "dingtalk.config.update":
			enabled := false
			if b, ok := args["enabled"].(bool); ok {
				enabled = b
			}
			if enabled {
				if isBlankConfigArg(args["client_id"]) {
					missing = append(missing, "client_id")
				}
				if isBlankConfigArg(args["client_secret"]) {
					missing = append(missing, "client_secret")
				}
			}
		case "qqbot.config.update":
			enabled := false
			if b, ok := args["enabled"].(bool); ok {
				enabled = b
			}
			if enabled {
				if isBlankConfigArg(args["app_id"]) {
					missing = append(missing, "app_id")
				}
				if isBlankConfigArg(args["app_secret"]) {
					missing = append(missing, "app_secret")
				}
			}
		case "bridge.channels.save":
			if isBlankConfigArg(args["id"]) {
				missing = append(missing, "channel_id")
			}
		case "mail.sender_name.update":
			if isBlankConfigArg(args["from_name"]) {
				missing = append(missing, "from_name")
			}
		case "registration_auth.update":
			method := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["method"])))
			if method == "phone" {
				if isBlankConfigArg(args["aliyun_access_key_id"]) {
					missing = append(missing, "aliyun_access_key_id")
				}
				if isBlankConfigArg(args["aliyun_access_key_secret"]) {
					missing = append(missing, "aliyun_access_key_secret")
				}
			}
		case "migration.settings.update":
			if _, ok := parseMigrationMaxBytesArgs(args); !ok {
				missing = append(missing, "max_mb")
			}
		case "card_store.config.update":
			_, hasEnabled := args["enabled"]
			if !hasEnabled && isBlankConfigArg(args["payment_mode"]) {
				missing = append(missing, "enabled")
			}
		}
	}
	// de-dup
	seen := map[string]struct{}{}
	out := make([]string, 0, len(missing))
	for _, m := range missing {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

// isBlankConfigArg treats nil / "" / "<nil>" as missing plan args.
func isBlankConfigArg(v any) bool {
	if v == nil {
		return true
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	return s == "" || s == "<nil>"
}

func looksLikeAddProvider(msg, lower string) bool {
	if strings.Contains(msg, "添加") && (strings.Contains(msg, "服务商") || strings.Contains(lower, "llm") || strings.Contains(msg, "提供商")) {
		return true
	}
	if strings.Contains(lower, "add") && (strings.Contains(lower, "provider") || strings.Contains(lower, "llm")) {
		return true
	}
	if strings.Contains(msg, "新增") && (strings.Contains(msg, "服务商") || strings.Contains(lower, "provider")) {
		return true
	}
	return false
}

func extractProviderArgs(msg string) map[string]any {
	args := map[string]any{
		"protocol": "openai",
		"wire_api": "chat",
	}
	// crude key=value / labeled field extraction
	// name: after 服务商 / provider
	if name := firstMatchGroup(msg, `(?i)(?:服务商|提供商|provider)\s*[:：]?\s*([A-Za-z0-9._\-\u4e00-\u9fff]+)`); name != "" {
		args["name"] = name
		args["id"] = slugID(name)
	}
	if url := firstMatchGroup(msg, `(?i)(https?://[^\s,，;；]+)`); url != "" {
		args["api_url"] = strings.TrimRight(url, "。.,")
	}
	if key := firstMatchGroup(msg, `(?i)(?:key|api[_-]?key|密钥)\s*[:：]?\s*([A-Za-z0-9_\-\.]+)`); key != "" {
		args["api_key"] = key
	} else if key := firstMatchGroup(msg, `(?i)\b(sk-[A-Za-z0-9]+)\b`); key != "" {
		args["api_key"] = key
	}
	if model := firstMatchGroup(msg, `(?i)(?:模型|model)\s*[:：]?\s*([A-Za-z0-9._\-:]+)`); model != "" {
		args["model"] = model
	}
	return args
}

func extractMentionedProviderID(msg string, providerReg *im.LLMProviderRegistry) string {
	if strings.Contains(strings.ToLower(msg), "maclaw") || strings.Contains(msg, "官方") {
		return llmservice.MaClawOfficialProviderID
	}
	if providerReg == nil {
		return ""
	}
	lower := strings.ToLower(msg)
	for _, p := range providerReg.Providers {
		id := strings.TrimSpace(p.ID)
		name := strings.TrimSpace(p.Name)
		if id != "" && strings.Contains(lower, strings.ToLower(id)) {
			return id
		}
		if name != "" && strings.Contains(lower, strings.ToLower(name)) {
			return id
		}
	}
	return ""
}

func firstMatchGroup(s, pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func slugID(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "provider"
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '.' {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "provider"
	}
	return out
}

func maskConfigAgentSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "****"
	}
	return s[:2] + "****" + s[len(s)-4:]
}
