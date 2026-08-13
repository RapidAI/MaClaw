package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

// Allowed config-agent tools for LLM / rule planners.
var configAgentAllowedTools = map[string]struct{}{
	"system_free.get":                  {},
	"system_free.test":                 {},
	"system_free.update":               {},
	"llm.providers.get":                {},
	"llm.providers.upsert":             {},
	"llm.providers.test":               {},
	"users.invite.create":              {},
	"users.invite.list":                {},
	"invitation_codes.generate":        {},
	"invitation_codes.list":            {},
	"invitation_codes.required.get":    {},
	"invitation_codes.required.update": {},
	"llm.services.list":                {},
	"llm.services.diagnose":            {},
	"llm.services.user_bind":           {},
	"llm.services.user_unbind":         {},
	"invitation_codes.export":          {},
	"feishu.config.get":                {},
	"feishu.config.update":             {},
	"users.blocklist.add":              {},
	"users.blocklist.list":             {},
	"users.blocklist.remove":           {},
	"enrollments.list_pending":         {},
	"enrollments.approve":              {},
	"enrollments.reject":               {},
	"users.manual_bind":                {},
	"wecom.config.get":                 {},
	"wecom.config.update":              {},
	"dingtalk.config.get":              {},
	"dingtalk.config.update":           {},
	"openclaw_im.config.get":           {},
	"openclaw_im.config.update":        {},
	"content_audit.config.get":         {},
	"content_audit.config.update":      {},
	"qqbot.config.get":                 {},
	"qqbot.config.update":              {},
	"bridge.channels.list":             {},
	"bridge.channels.save":             {},
	"mail.sender_name.get":             {},
	"mail.sender_name.update":          {},
	"smart_route_all.get":              {},
	"smart_route_all.update":           {},
	"registration_auth.get":            {},
	"registration_auth.update":         {},
	"migration.settings.get":           {},
	"migration.settings.update":        {},
	"feishu.auto_enroll.get":           {},
	"feishu.auto_enroll.update":        {},
	"card_store.config.get":            {},
	"card_store.config.update":         {},
	"digital_assets.settings.get":      {},
	"digital_assets.settings.update":   {},
	"ve.config.get":                    {},
	"ve.config.update":                 {},
	"security.settings.get":            {},
	"security.settings.update":         {},
	"security.default_group.get":       {},
	"security.default_group.update":    {},
	"security.approval_roles.get":      {},
	"security.approval_roles.update":   {},
	"referrals.config.get":             {},
	"referrals.config.update":          {},
	"capability_market.policy.get":     {},
	"capability_market.policy.update":  {},
}

type llmPlanDraft struct {
	Intent        string            `json:"intent"`
	Summary       string            `json:"summary"`
	RiskLevel     string            `json:"risk_level"`
	Assumptions   []string          `json:"assumptions"`
	MissingFields []string          `json:"missing_fields"`
	Steps         []configAgentStep `json:"steps"`
	Simulated     map[string]any    `json:"simulated"`
}

func configAgentToolCatalogJSON(providerReg *im.LLMProviderRegistry) string {
	providers := []string{llmservice.MaClawOfficialProviderID}
	if providerReg != nil {
		for _, p := range providerReg.Providers {
			id := strings.TrimSpace(p.ID)
			if id != "" {
				providers = append(providers, id)
			}
		}
	}
	catalog := map[string]any{
		"tools": []map[string]any{
			{"name": "system_free.get", "mode": "read", "desc": "Get system-free status"},
			{"name": "system_free.test", "mode": "probe", "desc": "Live probe system-free LLM"},
			{"name": "system_free.update", "mode": "write", "desc": "Update system-free models/providers; access_policy always free", "args": []string{"provider_id", "model"}},
			{"name": "llm.providers.get", "mode": "read", "desc": "List providers"},
			{"name": "llm.providers.upsert", "mode": "write", "desc": "Add/update provider; full registry is merged server-side", "args": []string{"id", "name", "api_url", "api_key", "model", "protocol", "wire_api"}},
			{"name": "llm.providers.test", "mode": "probe", "desc": "Live test one provider by id", "args": []string{"id"}},
			{"name": "users.invite.create", "mode": "write", "desc": "Create email invite", "args": []string{"email", "role"}},
			{"name": "users.invite.list", "mode": "read", "desc": "List email invites"},
			{"name": "invitation_codes.generate", "mode": "write", "desc": "Generate invitation codes", "args": []string{"count", "validity_days", "vip"}},
			{"name": "invitation_codes.list", "mode": "read", "desc": "List invitation codes", "args": []string{"status", "search", "page", "page_size"}},
			{"name": "invitation_codes.required.get", "mode": "read", "desc": "Get whether invitation code is required for registration"},
			{"name": "invitation_codes.required.update", "mode": "write", "desc": "Require or not require invitation code at registration", "args": []string{"required"}},
			{"name": "llm.services.list", "mode": "read", "desc": "List LLM model service groups (summary)"},
			{"name": "llm.services.diagnose", "mode": "read", "desc": "Diagnose LLM service entitlement for a user email", "args": []string{"email"}},
			{"name": "llm.services.user_bind", "mode": "write", "desc": "Bind user email to service groups (merge)", "args": []string{"email", "service_group_ids"}},
			{"name": "llm.services.user_unbind", "mode": "write", "desc": "Remove user from service groups; remove_all clears all bindings", "args": []string{"email", "service_group_ids", "remove_all"}},
			{"name": "invitation_codes.export", "mode": "write", "desc": "Export unused invitation codes (marks as exported)", "args": []string{"exported", "vip_only"}},
			{"name": "feishu.config.get", "mode": "read", "desc": "Get Feishu config (secret masked)"},
			{"name": "feishu.config.update", "mode": "write", "desc": "Update Feishu config", "args": []string{"enabled", "app_id", "app_secret"}},
			{"name": "users.blocklist.add", "mode": "write", "desc": "Block an email", "args": []string{"email", "reason"}},
			{"name": "users.blocklist.list", "mode": "read", "desc": "List blocked emails"},
			{"name": "users.blocklist.remove", "mode": "write", "desc": "Unblock an email", "args": []string{"email"}},
			{"name": "enrollments.list_pending", "mode": "read", "desc": "List pending enrollments"},
			{"name": "enrollments.approve", "mode": "write", "desc": "Approve enrollment by id or email", "args": []string{"id", "email"}},
			{"name": "enrollments.reject", "mode": "write", "desc": "Reject enrollment by id or email", "args": []string{"id", "email"}},
			{"name": "users.manual_bind", "mode": "write", "desc": "Manual bind user by email", "args": []string{"email"}},
			{"name": "wecom.config.get", "mode": "read", "desc": "Get WeCom config (secret masked)"},
			{"name": "wecom.config.update", "mode": "write", "desc": "Update WeCom config", "args": []string{"enabled", "bot_id", "secret", "ws_url"}},
			{"name": "dingtalk.config.get", "mode": "read", "desc": "Get DingTalk config (secret masked)"},
			{"name": "dingtalk.config.update", "mode": "write", "desc": "Update DingTalk config", "args": []string{"enabled", "client_id", "client_secret"}},
			{"name": "openclaw_im.config.get", "mode": "read", "desc": "Get OpenClaw IM bridge config"},
			{"name": "openclaw_im.config.update", "mode": "write", "desc": "Update OpenClaw IM bridge config", "args": []string{"enabled", "webhook_url", "secret"}},
			{"name": "content_audit.config.get", "mode": "read", "desc": "Get content audit config"},
			{"name": "content_audit.config.update", "mode": "write", "desc": "Update content audit keywords/timeout", "args": []string{"enabled_via_keywords", "keywords", "timeout_seconds", "timeout_policy", "program_path"}},
			{"name": "qqbot.config.get", "mode": "read", "desc": "Get QQ Bot config (secret masked)"},
			{"name": "qqbot.config.update", "mode": "write", "desc": "Update QQ Bot config", "args": []string{"enabled", "app_id", "app_secret"}},
			{"name": "bridge.channels.list", "mode": "read", "desc": "List OpenClaw bridge channels"},
			{"name": "bridge.channels.save", "mode": "write", "desc": "Save one bridge channel config", "args": []string{"id", "enabled", "fields", "install_npm"}},
			{"name": "mail.sender_name.get", "mode": "read", "desc": "Get tenant mail sender display name"},
			{"name": "mail.sender_name.update", "mode": "write", "desc": "Update tenant mail sender display name", "args": []string{"from_name"}},
			{"name": "smart_route_all.get", "mode": "read", "desc": "Get smart_route_all toggle"},
			{"name": "smart_route_all.update", "mode": "write", "desc": "Enable/disable smart_route_all", "args": []string{"enabled"}},
			{"name": "registration_auth.get", "mode": "read", "desc": "Get registration auth method config"},
			{"name": "registration_auth.update", "mode": "write", "desc": "Update registration auth method (email|phone|mixed)", "args": []string{"method", "aliyun_access_key_id", "aliyun_access_key_secret", "aliyun_sign_name", "aliyun_template_code"}},
			{"name": "migration.settings.get", "mode": "read", "desc": "Get tenant migration max package size settings"},
			{"name": "migration.settings.update", "mode": "write", "desc": "Update tenant migration encrypted package size limit", "args": []string{"max_package_bytes", "max_mb"}},
			{"name": "feishu.auto_enroll.get", "mode": "read", "desc": "Get Feishu auto-enroll setting"},
			{"name": "feishu.auto_enroll.update", "mode": "write", "desc": "Update Feishu auto-enroll", "args": []string{"enabled", "department_id", "use_lark", "employee_type"}},
			{"name": "card_store.config.get", "mode": "read", "desc": "Get card store config (secrets masked)"},
			{"name": "card_store.config.update", "mode": "write", "desc": "Toggle card store enabled / payment_mode", "args": []string{"enabled", "payment_mode"}},
			{"name": "digital_assets.settings.get", "mode": "read", "desc": "Get this tenant's digital-assets feature settings"},
			{"name": "digital_assets.settings.update", "mode": "write", "desc": "Enable/disable digital assets or sync for this tenant", "args": []string{"enabled", "sync_enabled"}},
			{"name": "ve.config.get", "mode": "read", "desc": "Get this tenant's virtual-employee group settings"},
			{"name": "ve.config.update", "mode": "write", "desc": "Set virtual-employee auto approval or group size", "args": []string{"auto_approve", "max_group_participants"}},
			{"name": "security.settings.get", "mode": "read", "desc": "Get tenant security and organization settings"},
			{"name": "security.settings.update", "mode": "write", "desc": "Update tenant security and organization settings", "args": []string{"centralized_security_enabled", "org_structure_enabled"}},
			{"name": "security.default_group.get", "mode": "read", "desc": "Get the default group for new tenant users"},
			{"name": "security.default_group.update", "mode": "write", "desc": "Set the default group for new tenant users", "args": []string{"group_id"}},
			{"name": "security.approval_roles.get", "mode": "read", "desc": "Get tenant approval role assignments"},
			{"name": "security.approval_roles.update", "mode": "write", "desc": "Replace tenant approval role assignments", "args": []string{"roles", "function_scopes"}},
			{"name": "referrals.config.get", "mode": "read", "desc": "Get tenant user-referral policy"},
			{"name": "referrals.config.update", "mode": "write", "desc": "Update tenant user-referral policy", "args": []string{"enabled", "inviter_credits", "invitee_credits", "duration_days", "daily_reward_cap", "daily_network_client_review_cap", "service_group_id"}},
			{"name": "capability_market.policy.get", "mode": "read", "desc": "Get tenant capability-market policy"},
			{"name": "capability_market.policy.update", "mode": "write", "desc": "Replace tenant capability-market policy", "args": []string{"policy"}},
		},
		"known_provider_ids": providers,
		"rules": []string{
			"Only use tools from the catalog",
			"Never delete system-free",
			"system-free access_policy is always free",
			"Writes require user confirm (server enforces)",
			"Prefer GET then merge for provider registry",
			"Invite role must be viewer|member|admin",
			"Return JSON only, no markdown",
		},
	}
	b, _ := json.Marshal(catalog)
	return string(b)
}

func llmPlanFromMessage(ctx context.Context, message, tenantID string, serviceReg *llmservice.Registry, providerReg *im.LLMProviderRegistry) (*configAgentPlan, error) {
	resolver := im.DefaultSystemLLMResolver()
	if resolver == nil || !resolver.HasSystemFreeRoute(ctx) {
		return nil, fmt.Errorf("system-free LLM not available for planning")
	}

	sfStatus := llmservice.EvaluateSystemFreeStatus(serviceReg, configuredProviderIDSet(providerReg))
	providerIDs := []string{}
	if providerReg != nil {
		for _, p := range providerReg.Providers {
			if id := strings.TrimSpace(p.ID); id != "" {
				providerIDs = append(providerIDs, id)
			}
		}
	}

	systemPrompt := `You are the MaClaw Hub tenant config planner.
Convert the admin natural-language request into a JSON plan for tools that configure THIS tenant only.
Output ONLY one JSON object with keys:
intent, summary, risk_level (low|medium|high), assumptions (string[]), missing_fields (string[]), steps (array), simulated (object).
Each step: step_id, tool, mode (read|write|probe), purpose, args (object), depends_on (string[]), optional (bool).
If required args are missing, list them in missing_fields and still produce best-effort steps.
Do not invent tools outside the catalog.`

	userPrompt := fmt.Sprintf(
		"Tenant: %s\nSystem-free ready: %v providers=%v reasons=%v\nConfigured local providers: %s\nTool catalog: %s\n\nAdmin request:\n%s",
		tenantID,
		sfStatus.Ready,
		sfStatus.ProviderIDs,
		sfStatus.Reasons,
		strings.Join(providerIDs, ", "),
		configAgentToolCatalogJSON(providerReg),
		message,
	)

	messages := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
		map[string]string{"role": "user", "content": userPrompt},
	}
	resp, err := resolver.Call(ctx, messages, 25*time.Second)
	if err != nil {
		return nil, err
	}
	content := ""
	if resp != nil {
		content = strings.TrimSpace(resp.Content)
	}
	draft, err := parseLLMPlanDraft(content)
	if err != nil {
		return nil, err
	}
	plan, err := validateAndNormalizeLLMPlan(draft)
	if err != nil {
		return nil, err
	}
	plan.Planner = "llm"
	plan.SourceMessage = message
	return plan, nil
}

func parseLLMPlanDraft(content string) (*llmPlanDraft, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty LLM plan")
	}
	// Strip fenced code blocks if present.
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSpace(content)
		if strings.HasPrefix(strings.ToLower(content), "json") {
			content = strings.TrimSpace(content[4:])
		}
		if i := strings.LastIndex(content, "```"); i >= 0 {
			content = strings.TrimSpace(content[:i])
		}
	}
	// Extract first JSON object.
	if !strings.HasPrefix(content, "{") {
		re := regexp.MustCompile(`(?s)\{.*\}`)
		m := re.FindString(content)
		if m == "" {
			return nil, fmt.Errorf("no JSON object in LLM response")
		}
		content = m
	}
	var draft llmPlanDraft
	if err := json.Unmarshal([]byte(content), &draft); err != nil {
		return nil, fmt.Errorf("parse LLM plan JSON: %w", err)
	}
	return &draft, nil
}

func validateAndNormalizeLLMPlan(draft *llmPlanDraft) (*configAgentPlan, error) {
	if draft == nil {
		return nil, fmt.Errorf("nil draft")
	}
	intent := strings.TrimSpace(draft.Intent)
	if intent == "" {
		intent = "config.unknown"
	}
	summary := strings.TrimSpace(draft.Summary)
	if summary == "" {
		summary = intent
	}
	risk := strings.ToLower(strings.TrimSpace(draft.RiskLevel))
	switch risk {
	case "low", "medium", "high":
	default:
		risk = "medium"
	}
	steps := make([]configAgentStep, 0, len(draft.Steps))
	stepIDs := make(map[string]struct{}, len(draft.Steps))
	for i, s := range draft.Steps {
		tool := strings.TrimSpace(s.Tool)
		if tool == "" {
			continue
		}
		if _, ok := configAgentAllowedTools[tool]; !ok {
			return nil, fmt.Errorf("disallowed tool %q", tool)
		}
		mode := configAgentToolMode(tool)
		stepID := strings.TrimSpace(s.StepID)
		if stepID == "" {
			stepID = fmt.Sprintf("s%d", i+1)
		}
		if _, exists := stepIDs[stepID]; exists {
			return nil, fmt.Errorf("duplicate step_id %q", stepID)
		}
		stepIDs[stepID] = struct{}{}
		args := s.Args
		if args == nil {
			args = map[string]any{}
		}
		// Force free policy never set via LLM args for system-free.
		if tool == "system_free.update" {
			delete(args, "access_policy")
			delete(args, "id")
		}
		steps = append(steps, configAgentStep{
			StepID:     stepID,
			Tool:       tool,
			Mode:       mode,
			Purpose:    strings.TrimSpace(s.Purpose),
			Args:       args,
			DependsOn:  s.DependsOn,
			Optional:   s.Optional,
			APIPreview: defaultAPIPreviewForTool(tool),
		})
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("LLM plan has no valid steps")
	}
	if err := validateConfigAgentStepDependencies(steps); err != nil {
		return nil, err
	}
	if configAgentPlanHasWrite(steps) && risk != "high" {
		risk = "high"
	}
	return &configAgentPlan{
		Intent:        intent,
		Summary:       summary,
		RiskLevel:     risk,
		Assumptions:   draft.Assumptions,
		MissingFields: draft.MissingFields,
		Steps:         steps,
		Simulated:     draft.Simulated,
		Planner:       "llm",
	}, nil
}

func configAgentPlanHasWrite(steps []configAgentStep) bool {
	for _, step := range steps {
		if step.Mode == "write" {
			return true
		}
	}
	return false
}

func validateConfigAgentStepDependencies(steps []configAgentStep) error {
	known := make(map[string]struct{}, len(steps))
	positions := make(map[string]int, len(steps))
	for index, step := range steps {
		known[step.StepID] = struct{}{}
		positions[step.StepID] = index
	}
	for index, step := range steps {
		for _, dependency := range step.DependsOn {
			dependency = strings.TrimSpace(dependency)
			if dependency == "" {
				continue
			}
			if dependency == step.StepID {
				return fmt.Errorf("step %q cannot depend on itself", step.StepID)
			}
			if _, ok := known[dependency]; !ok {
				return fmt.Errorf("step %q depends on unknown step %q", step.StepID, dependency)
			}
			if positions[dependency] >= index {
				return fmt.Errorf("step %q depends on %q, which must appear earlier in the plan", step.StepID, dependency)
			}
		}
	}
	return nil
}

func defaultAPIPreviewForTool(tool string) map[string]any {
	switch tool {
	case "system_free.get":
		return map[string]any{"method": "GET", "path": "/api/admin/llm/system-free"}
	case "system_free.test":
		return map[string]any{"method": "POST", "path": "/api/admin/llm/system-free/test"}
	case "system_free.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/llm/system-free"}
	case "llm.providers.get":
		return map[string]any{"method": "GET", "path": "/api/admin/llm/providers"}
	case "llm.providers.upsert":
		return map[string]any{"method": "PUT", "path": "/api/admin/llm/providers", "note": "GET merge then PUT"}
	case "llm.providers.test":
		return map[string]any{"method": "POST", "path": "/api/admin/llm/providers/test"}
	case "users.invite.create":
		return map[string]any{"method": "POST", "path": "/api/admin/invites"}
	case "users.invite.list":
		return map[string]any{"method": "GET", "path": "/api/admin/invites"}
	case "invitation_codes.generate":
		return map[string]any{"method": "POST", "path": "/api/admin/invitation-codes/generate"}
	case "invitation_codes.list":
		return map[string]any{"method": "GET", "path": "/api/admin/invitation-codes"}
	case "invitation_codes.required.get":
		return map[string]any{"method": "GET", "path": "/api/admin/invitation-codes/status"}
	case "invitation_codes.required.update":
		return map[string]any{"method": "POST", "path": "/api/admin/invitation-codes/toggle"}
	case "llm.services.list":
		return map[string]any{"method": "GET", "path": "/api/admin/llm/services"}
	case "llm.services.diagnose":
		return map[string]any{"method": "GET", "path": "/api/admin/llm/services/diagnose", "note": "query email=..."}
	case "llm.services.user_bind":
		return map[string]any{"method": "PUT", "path": "/api/admin/llm/services", "note": "merge user_bindings"}
	case "llm.services.user_unbind":
		return map[string]any{"method": "PUT", "path": "/api/admin/llm/services", "note": "remove from user_bindings"}
	case "invitation_codes.export":
		return map[string]any{"method": "GET", "path": "/api/admin/invitation-codes/export", "note": "marks unexported codes as exported"}
	case "feishu.config.get":
		return map[string]any{"method": "GET", "path": "/api/admin/feishu/config"}
	case "feishu.config.update":
		return map[string]any{"method": "POST", "path": "/api/admin/feishu/config"}
	case "users.blocklist.add":
		return map[string]any{"method": "POST", "path": "/api/admin/blocklist"}
	case "users.blocklist.list":
		return map[string]any{"method": "GET", "path": "/api/admin/blocklist"}
	case "users.blocklist.remove":
		return map[string]any{"method": "DELETE", "path": "/api/admin/blocklist/{email}"}
	case "enrollments.list_pending":
		return map[string]any{"method": "GET", "path": "/api/admin/enrollments/pending"}
	case "enrollments.approve":
		return map[string]any{"method": "POST", "path": "/api/admin/enrollments/approve"}
	case "enrollments.reject":
		return map[string]any{"method": "POST", "path": "/api/admin/enrollments/reject"}
	case "users.manual_bind":
		return map[string]any{"method": "POST", "path": "/api/admin/users/manual-bind"}
	case "wecom.config.get":
		return map[string]any{"method": "GET", "path": "/api/admin/settings/wecom"}
	case "wecom.config.update":
		return map[string]any{"method": "POST", "path": "/api/admin/settings/wecom"}
	case "dingtalk.config.get":
		return map[string]any{"method": "GET", "path": "/api/admin/settings/dingtalk"}
	case "dingtalk.config.update":
		return map[string]any{"method": "POST", "path": "/api/admin/settings/dingtalk"}
	case "openclaw_im.config.get":
		return map[string]any{"method": "GET", "path": "/api/admin/settings/openclaw_im"}
	case "openclaw_im.config.update":
		return map[string]any{"method": "POST", "path": "/api/admin/settings/openclaw_im"}
	case "content_audit.config.get":
		return map[string]any{"method": "GET", "path": "/api/admin/content_audit/config"}
	case "content_audit.config.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/content_audit/config"}
	case "qqbot.config.get":
		return map[string]any{"method": "GET", "path": "/api/admin/settings/qqbot"}
	case "qqbot.config.update":
		return map[string]any{"method": "POST", "path": "/api/admin/settings/qqbot"}
	case "bridge.channels.list":
		return map[string]any{"method": "GET", "path": "/api/admin/bridge/channels"}
	case "bridge.channels.save":
		return map[string]any{"method": "POST", "path": "/api/admin/bridge/channels", "note": "saves channel config; npm install when install_npm=true"}
	case "mail.sender_name.get":
		return map[string]any{"method": "GET", "path": "/api/admin/mail/sender-name"}
	case "mail.sender_name.update":
		return map[string]any{"method": "POST", "path": "/api/admin/mail/sender-name"}
	case "smart_route_all.get":
		return map[string]any{"method": "GET", "path": "/api/admin/smart_route_all"}
	case "smart_route_all.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/smart_route_all"}
	case "registration_auth.get":
		return map[string]any{"method": "GET", "path": "/api/admin/settings/registration-auth"}
	case "registration_auth.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/settings/registration-auth"}
	case "migration.settings.get":
		return map[string]any{"method": "GET", "path": "/api/admin/migration/settings"}
	case "migration.settings.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/migration/settings"}
	case "feishu.auto_enroll.get":
		return map[string]any{"method": "GET", "path": "/api/admin/feishu/auto-enroll"}
	case "feishu.auto_enroll.update":
		return map[string]any{"method": "POST", "path": "/api/admin/feishu/auto-enroll"}
	case "card_store.config.get":
		return map[string]any{"method": "GET", "path": "/api/admin/card-store/config"}
	case "card_store.config.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/card-store/config", "note": "partial: enabled/payment_mode"}
	case "digital_assets.settings.get":
		return map[string]any{"method": "GET", "path": "/api/admin/digital-assets/settings"}
	case "digital_assets.settings.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/digital-assets/settings"}
	case "ve.config.get":
		return map[string]any{"method": "GET", "path": "/api/ve/config"}
	case "ve.config.update":
		return map[string]any{"method": "PUT", "path": "/api/ve/config"}
	case "security.settings.get":
		return map[string]any{"method": "GET", "path": "/api/admin/security/settings"}
	case "security.settings.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/security/settings"}
	case "security.default_group.get":
		return map[string]any{"method": "GET", "path": "/api/admin/security/settings"}
	case "security.default_group.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/security/settings/default-group"}
	case "security.approval_roles.get":
		return map[string]any{"method": "GET", "path": "/api/admin/security/approval-roles"}
	case "security.approval_roles.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/security/approval-roles"}
	case "referrals.config.get":
		return map[string]any{"method": "GET", "path": "/api/admin/user-referrals/config"}
	case "referrals.config.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/user-referrals/config"}
	case "capability_market.policy.get":
		return map[string]any{"method": "GET", "path": "/api/admin/capability-market/policy"}
	case "capability_market.policy.update":
		return map[string]any{"method": "PUT", "path": "/api/admin/capability-market/policy"}
	default:
		return nil
	}
}

// planConfigAgentMessage tries rule planner first, then system-free LLM planner.
func planConfigAgentMessage(ctx context.Context, message, tenantID string, serviceReg *llmservice.Registry, providerReg *im.LLMProviderRegistry) (*configAgentPlan, string) {
	if plan := rulePlanFromMessage(message, tenantID, serviceReg, providerReg); plan != nil {
		return plan, "rule"
	}
	// Tenant context for system-free resolver.
	ctx = im.WithTenant(ctx, tenantID)
	plan, err := llmPlanFromMessage(ctx, message, tenantID, serviceReg, providerReg)
	if err != nil {
		log.Printf("[config-agent] LLM planner failed: %v", err)
		return nil, "none"
	}
	return plan, "llm"
}
