package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/invitation"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type configAgentPlanRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id,omitempty"`
}

type configAgentExecuteRequest struct {
	PlanID       string `json:"plan_id"`
	ConfirmToken string `json:"confirm_token"`
	RunOptional  bool   `json:"run_optional,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
}

// ConfigAgentPlanHandler builds a proposed plan from natural language.
func ConfigAgentPlanHandler(deps ConfigAgentDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, deps.System)
		var req configAgentPlanRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		msg := strings.TrimSpace(req.Message)
		if msg == "" {
			writeError(w, http.StatusBadRequest, "MESSAGE_REQUIRED", "message is required")
			return
		}

		serviceReg, err := llmservice.LoadRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		_ = llmservice.EnsureSystemFreeServiceGroup(serviceReg)
		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}

		tenantID := RequestTenantID(r)
		adminID := adminAuditUserID(r)
		sessionID := strings.TrimSpace(req.SessionID)

		var plan *configAgentPlan
		planner := "none"

		// Capture prior session turns before refine/replace.
		var priorTurns []string
		if sessionID != "" {
			if prev := globalConfigAgentSessions.get(sessionID); prev != nil && prev.AdminUserID == adminID {
				priorTurns = append([]string{}, prev.History...)
			}
		}

		// Multi-turn: refine incomplete plan from session when follow-up arrives.
		refinedFromSession := false
		if sessionID != "" {
			if sess := globalConfigAgentSessions.get(sessionID); sess != nil &&
				sess.AdminUserID == adminID &&
				sess.PendingPlan != nil &&
				len(sess.PendingPlan.MissingFields) > 0 {
				if refined := refinePlanWithFollowUp(sess.PendingPlan, msg, serviceReg, providerReg); refined != nil {
					plan = refined
					planner = firstNonEmptyStr(refined.Planner, "rule")
					refinedFromSession = true
					priorTurns = appendConfigAgentSessionTurn(priorTurns, msg)
				}
			}
		}

		if plan == nil {
			plan, planner = planConfigAgentMessage(r.Context(), msg, tenantID, serviceReg, providerReg)
		}
		if plan == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":      false,
				"code":    "PLAN_NOT_UNDERSTOOD",
				"message": "Could not understand the request. Try examples below, or ensure system-free is ready for LLM planning.",
				"examples": []string{
					"Add LLM provider DeepSeek url https://api.deepseek.com/v1 model deepseek-chat key sk-xxx",
					"Invite user alice@example.com as member",
					"Generate 5 invitation codes",
					"Bind user alice@example.com to system-free",
					"Block email spam@example.com reason abuse",
					"List pending enrollments",
					"Approve enrollment for alice@example.com",
					"Test system-free",
					"Show feishu config",
					"Show migration settings",
					"Set migration max to 200MB",
					"Show feishu auto enroll",
					"Enable feishu auto enroll",
					"Show card store config",
					"List invitation codes",
					"Show invitation code required status",
					"List service groups",
					"Diagnose LLM service for alice@example.com",
					"Export invitation codes",
					"Unbind user alice@example.com from system-free",
					"Bind user alice@example.com to system-free and coding-basic",
				},
				"session_id":     sessionID,
				"session_turns":  priorTurns,
				"planner_tried":  planner,
				"system_free":    llmservice.EvaluateSystemFreeStatus(serviceReg, configuredProviderIDSet(providerReg)),
			})
			return
		}

		planID, token, err := newConfigAgentIDs()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "PLAN_ID_FAILED", err.Error())
			return
		}
		plan.PlanID = planID
		plan.ConfirmToken = token
		plan.TenantID = tenantID
		plan.AdminUserID = adminID
		plan.CreatedAt = time.Now().UTC()
		plan.ExpiresAt = time.Now().UTC().Add(10 * time.Minute)
		if plan.SourceMessage == "" {
			plan.SourceMessage = msg
		}
		if plan.Planner == "" {
			plan.Planner = planner
		}
		if plan.RiskLevel == "" {
			plan.RiskLevel = "medium"
		}
		// Always recompute missing fields from step args (authoritative).
		plan.MissingFields = recomputeMissingFields(plan)
		// Enrich simulations (e.g. bind current vs target) for plan preview / history.
		enrichConfigAgentPlanSimulated(plan, serviceReg)
		globalConfigAgentStore.put(plan)

		// Persist session: keep turn timeline; only keep pending when still incomplete.
		if sessionID == "" {
			sid, _, _ := newConfigAgentIDs()
			sessionID = "sess_" + strings.TrimPrefix(sid, "pln_")
		}
		sessionTurns := priorTurns
		if !refinedFromSession {
			// New topic or first turn in this session.
			if len(sessionTurns) == 0 {
				sessionTurns = []string{msg}
			} else {
				sessionTurns = appendConfigAgentSessionTurn(sessionTurns, msg)
			}
		}
		var pending *configAgentPlan
		if len(plan.MissingFields) > 0 {
			pending = plan
		}
		sessionExpiresAt := time.Now().UTC().Add(15 * time.Minute)
		globalConfigAgentSessions.put(&configAgentSession{
			SessionID:   sessionID,
			AdminUserID: adminID,
			TenantID:    tenantID,
			PendingPlan: pending,
			History:     sessionTurns,
			ExpiresAt:   sessionExpiresAt,
		})

		resp := *plan
		resp.Steps = sanitizePlanSteps(plan.Steps)
		note := "Review the plan. Call POST /api/admin/config-agent/execute with plan_id and confirm_token to apply writes."
		if len(plan.MissingFields) > 0 {
			note = "Plan is incomplete. Reply with missing fields (same session_id) to refine, then confirm."
		}
		writeAdminAuditLog(r.Context(), deps.Audit, adminID, "config_agent.plan", map[string]any{
			"plan_id": plan.PlanID, "intent": plan.Intent, "planner": plan.Planner,
			"summary": plan.Summary, "risk_level": plan.RiskLevel,
			"missing_fields": plan.MissingFields, "step_count": len(plan.Steps),
			"source_message": plan.SourceMessage, "session_id": sessionID,
			"session_turns":  sessionTurns,
			"plan":           resp, // sanitized snapshot for history replay
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                 true,
			"plan":               resp,
			"planner":            plan.Planner,
			"session_id":         sessionID,
			"session_turns":      sessionTurns,
			"session_expires_at": sessionExpiresAt.Format(time.RFC3339),
			"needs_input":        len(plan.MissingFields) > 0,
			"system_free":        llmservice.EvaluateSystemFreeStatus(serviceReg, configuredProviderIDSet(providerReg)),
			"note":               note,
		})
	}
}

// appendConfigAgentSessionTurn appends a user turn, de-duping consecutive identical messages.
func appendConfigAgentSessionTurn(turns []string, msg string) []string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return turns
	}
	if n := len(turns); n > 0 && strings.TrimSpace(turns[n-1]) == msg {
		return turns
	}
	return append(turns, msg)
}

// ConfigAgentExecuteHandler runs a previously planned config change after confirm.
func ConfigAgentExecuteHandler(deps ConfigAgentDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, deps.System)
		audit := deps.Audit
		if audit == nil {
			audit = firstAdminAuditRepo()
		}
		var req configAgentExecuteRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		plan, err := globalConfigAgentStore.consume(strings.TrimSpace(req.PlanID), strings.TrimSpace(req.ConfirmToken), adminAuditUserID(r))
		if err != nil {
			writeError(w, http.StatusBadRequest, "PLAN_CONFIRM_FAILED", err.Error())
			return
		}
		plan.MissingFields = recomputeMissingFields(plan)
		if len(plan.MissingFields) > 0 {
			writeError(w, http.StatusBadRequest, "PLAN_INCOMPLETE", "plan has missing fields: "+strings.Join(plan.MissingFields, ", "))
			return
		}

		results := make([]map[string]any, 0, len(plan.Steps))
		for _, step := range plan.Steps {
			if step.Optional && !req.RunOptional {
				results = append(results, map[string]any{
					"step_id": step.StepID, "tool": step.Tool, "skipped": true, "reason": "optional",
				})
				continue
			}
			out, stepErr := executeConfigAgentStep(r, deps, system, step)
			entry := map[string]any{
				"step_id": step.StepID,
				"tool":    step.Tool,
				"ok":      stepErr == nil,
				"result":  out,
			}
			if stepErr != nil {
				entry["error"] = stepErr.Error()
				results = append(results, entry)
				writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "config_agent.execute", map[string]any{
					"plan_id":        plan.PlanID,
					"intent":         plan.Intent,
					"summary":        plan.Summary,
					"source_message": plan.SourceMessage,
					"session_id":     strings.TrimSpace(req.SessionID),
					"ok":             false,
					"results":        results,
					"error":          stepErr.Error(),
				})
				writeJSON(w, http.StatusOK, map[string]any{
					"ok": false, "plan_id": plan.PlanID, "intent": plan.Intent, "results": results,
					"error": "stopped on step " + step.StepID + ": " + stepErr.Error(),
				})
				return
			}
			results = append(results, entry)
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "config_agent.execute", map[string]any{
			"plan_id":        plan.PlanID,
			"intent":         plan.Intent,
			"summary":        plan.Summary,
			"source_message": plan.SourceMessage,
			"session_id":     strings.TrimSpace(req.SessionID),
			"ok":             true,
			"results":        results,
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "plan_id": plan.PlanID, "intent": plan.Intent, "results": results,
		})
	}
}

func executeConfigAgentStep(r *http.Request, deps ConfigAgentDeps, system store.SystemSettingsRepository, step configAgentStep) (any, error) {
	switch step.Tool {
	case "system_free.get":
		reg, err := loadSystemFreeRegistry(r, system)
		if err != nil {
			return nil, err
		}
		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			return nil, err
		}
		return llmservice.EvaluateSystemFreeStatus(reg, configuredProviderIDSet(providerReg)), nil

	case "system_free.test":
		return liveTestSystemFree(r, system)

	case "system_free.update":
		return execSystemFreeUpdate(r, system, step.Args)

	case "llm.providers.get":
		reg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(reg.Providers))
		for _, p := range reg.Providers {
			items = append(items, map[string]any{"id": p.ID, "name": p.Name, "model": p.Model, "api_url": p.APIURL})
		}
		return map[string]any{"count": len(items), "providers": items, "enabled": reg.Enabled}, nil

	case "llm.providers.upsert":
		return upsertProviderFromPlanArgs(r, system, step.Args)

	case "llm.providers.test":
		id := strings.TrimSpace(fmt.Sprint(step.Args["id"]))
		if id == "" {
			return map[string]any{"skipped": true, "reason": "no provider id"}, nil
		}
		return liveTestLLMProvider(r, system, id)

	case "users.invite.create":
		return execCreateInvite(r, deps, step.Args)

	case "users.invite.list":
		return execListInvites(r, deps)

	case "invitation_codes.generate":
		return execGenerateInviteCodes(r, deps, step.Args)
	case "invitation_codes.list":
		return execInvitationCodesList(r, deps, step.Args)
	case "invitation_codes.required.get":
		return execInvitationCodesRequiredGet(r, deps)
	case "invitation_codes.required.update":
		return execInvitationCodesRequiredUpdate(r, deps, step.Args)
	case "invitation_codes.export":
		return execInvitationCodesExport(r, deps, step.Args)

	case "llm.services.list":
		return execLLMServicesList(r, system)
	case "llm.services.diagnose":
		return execLLMServicesDiagnose(r, deps, system, step.Args)
	case "llm.services.user_bind":
		return execUserServiceBind(r, system, step.Args)
	case "llm.services.user_unbind":
		return execUserServiceUnbind(r, system, step.Args)

	case "feishu.config.get":
		return execFeishuConfigGet(r, system)

	case "feishu.config.update":
		return execFeishuConfigUpdate(r, deps, system, step.Args)

	case "users.blocklist.add":
		return execBlocklistAdd(r, deps, step.Args)

	case "users.blocklist.list":
		return execBlocklistList(r, deps)

	case "users.blocklist.remove":
		return execBlocklistRemove(r, deps, step.Args)

	case "enrollments.list_pending":
		return execEnrollmentsListPending(r, deps)

	case "enrollments.approve":
		return execEnrollmentApprove(r, deps, step.Args)

	case "enrollments.reject":
		return execEnrollmentReject(r, deps, step.Args)

	case "users.manual_bind":
		return execManualBind(r, deps, step.Args)

	case "wecom.config.get":
		return execWeComConfigGet(r, system)
	case "wecom.config.update":
		return execWeComConfigUpdate(r, deps, system, step.Args)
	case "dingtalk.config.get":
		return execDingTalkConfigGet(r, system)
	case "dingtalk.config.update":
		return execDingTalkConfigUpdate(r, deps, system, step.Args)
	case "openclaw_im.config.get":
		return execOpenclawIMConfigGet(r, system)
	case "openclaw_im.config.update":
		return execOpenclawIMConfigUpdate(r, system, step.Args)
	case "content_audit.config.get":
		return execContentAuditConfigGet(r, system)
	case "content_audit.config.update":
		return execContentAuditConfigUpdate(r, system, step.Args)

	case "qqbot.config.get":
		return execQQBotConfigGet(r, system)
	case "qqbot.config.update":
		return execQQBotConfigUpdate(r, deps, system, step.Args)
	case "bridge.channels.list":
		return execBridgeChannelsList(r, system, deps.BridgeDir)
	case "bridge.channels.save":
		return execBridgeChannelSaveWithDeps(r, deps, system, step.Args)

	case "mail.sender_name.get":
		return execMailSenderNameGet(r, system)
	case "mail.sender_name.update":
		return execMailSenderNameUpdate(r, system, step.Args)
	case "smart_route_all.get":
		return execSmartRouteAllGet(r, system)
	case "smart_route_all.update":
		return execSmartRouteAllUpdate(r, system, step.Args)
	case "registration_auth.get":
		return execRegistrationAuthGet(r, system)
	case "registration_auth.update":
		return execRegistrationAuthUpdate(r, system, step.Args)
	case "migration.settings.get":
		return execMigrationSettingsGet(r, system)
	case "migration.settings.update":
		return execMigrationSettingsUpdate(r, system, step.Args)
	case "feishu.auto_enroll.get":
		return execFeishuAutoEnrollGet(r, system)
	case "feishu.auto_enroll.update":
		return execFeishuAutoEnrollUpdate(r, deps, system, step.Args)
	case "card_store.config.get":
		return execCardStoreConfigGet(r, system)
	case "card_store.config.update":
		return execCardStoreConfigUpdate(r, system, step.Args)

	default:
		return nil, fmt.Errorf("unknown tool %q", step.Tool)
	}
}

func execSystemFreeUpdate(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	providerID := strings.TrimSpace(fmt.Sprint(args["provider_id"]))
	if providerID == "" {
		return nil, fmt.Errorf("provider_id required")
	}
	modelName := strings.TrimSpace(fmt.Sprint(args["model"]))
	if modelName == "" {
		modelName = "auto"
	}
	reg, err := loadSystemFreeRegistry(r, system)
	if err != nil {
		return nil, err
	}
	old := *reg
	group := reg.FindModelServiceGroup(llmservice.SystemFreeServiceGroupID)
	if group == nil {
		return nil, fmt.Errorf("system-free missing")
	}
	if !llmservice.IsBuiltinProvider(providerID) {
		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			return nil, err
		}
		if providerReg.FindProvider(providerID) == nil {
			return nil, fmt.Errorf("provider %q not found", providerID)
		}
	}
	group.Models = []llmservice.ModelServiceModel{{
		Name: modelName, ProviderIDs: []string{providerID},
	}}
	group.ID = llmservice.SystemFreeServiceGroupID
	group.AccessPolicy = llmservice.AccessPolicyFree
	for i := range reg.ModelServiceGroups {
		if llmservice.IsSystemFreeServiceGroup(reg.ModelServiceGroups[i].ID) {
			reg.ModelServiceGroups[i] = *group
			break
		}
	}
	llmservice.ProtectSystemFreeOnSave(reg, &old)
	reg.Normalize()
	if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
		return nil, err
	}
	invalidateLLMRuntimeCaches(system)
	providerReg, _ := im.LoadLLMProviderRegistry(r.Context(), system)
	return llmservice.EvaluateSystemFreeStatus(reg, configuredProviderIDSet(providerReg)), nil
}

func execCreateInvite(r *http.Request, deps ConfigAgentDeps, args map[string]any) (any, error) {
	if deps.Invites == nil {
		return nil, fmt.Errorf("invite repository unavailable")
	}
	email := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["email"])))
	role := strings.TrimSpace(fmt.Sprint(args["role"]))
	if email == "" {
		return nil, fmt.Errorf("email required")
	}
	switch role {
	case "viewer", "member", "admin":
	case "":
		role = "viewer"
	default:
		return nil, fmt.Errorf("role must be viewer, member, or admin")
	}
	now := time.Now()
	item := &store.EmailInvite{
		ID:        emailInviteID(),
		TenantID:  RequestTenantID(r),
		Email:     email,
		Role:      role,
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := deps.Invites.Create(r.Context(), item); err != nil {
		return nil, err
	}
	return toEmailInviteResponse(item), nil
}

func execListInvites(r *http.Request, deps ConfigAgentDeps) (any, error) {
	if deps.Invites == nil {
		return nil, fmt.Errorf("invite repository unavailable")
	}
	var (
		items []*store.EmailInvite
		err   error
	)
	if isTenantScopedAdminRequest(r) {
		items, err = deps.Invites.ListByTenant(r.Context(), RequestTenantID(r))
	} else {
		items, err = deps.Invites.List(r.Context())
	}
	if err != nil {
		return nil, err
	}
	out := make([]emailInviteResponse, 0, len(items))
	for _, it := range items {
		if it == nil {
			continue
		}
		out = append(out, toEmailInviteResponse(it))
	}
	return map[string]any{"count": len(out), "items": out}, nil
}

func execInvitationCodesExport(r *http.Request, deps ConfigAgentDeps, args map[string]any) (any, error) {
	if deps.Codes == nil {
		return nil, fmt.Errorf("invitation code service unavailable")
	}
	exportedFilter := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["exported"])))
	if exportedFilter == "" || exportedFilter == "<nil>" {
		exportedFilter = "unexported"
	}
	switch exportedFilter {
	case "unexported", "exported", "all":
	default:
		exportedFilter = "unexported"
	}
	vipOnly := false
	if b, ok := args["vip_only"].(bool); ok {
		vipOnly = b
	} else if v, ok := args["vip_only"]; ok && v != nil {
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		vipOnly = s == "true" || s == "1" || s == "yes" || s == "vip"
	}
	// Also accept vip: true as alias.
	if !vipOnly {
		if b, ok := args["vip"].(bool); ok {
			vipOnly = b
		}
	}
	codes, err := deps.Codes.ExportUnusedCodesForTenant(r.Context(), RequestTenantID(r), exportedFilter, vipOnly)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(codes))
	plain := make([]string, 0, len(codes))
	for _, c := range codes {
		if c == nil {
			continue
		}
		out = append(out, map[string]any{
			"id": c.ID, "code": c.Code, "status": c.Status, "vip": c.VIP, "exported": true,
		})
		if strings.TrimSpace(c.Code) != "" {
			plain = append(plain, c.Code)
		}
	}
	return map[string]any{
		"ok":       true,
		"count":    len(out),
		"exported": exportedFilter,
		"vip_only": vipOnly,
		"codes":    out,
		"text":     strings.Join(plain, "\n"),
		"note":     "unexported codes are marked exported after this call",
	}, nil
}

func execLLMServicesDiagnose(r *http.Request, deps ConfigAgentDeps, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	email := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["email"])))
	if email == "" || email == "<nil>" {
		return nil, fmt.Errorf("email required")
	}
	tenantID := RequestTenantID(r)
	system = scopedSystemSettingsForRequest(r, system)
	ctx := security.WithTenant(r.Context(), tenantID)
	diagnostic, err := llmservice.ExplainEntitlementDiagnostic(ctx, system, deps.Security, email, externalLLMBaseURL(r))
	if err != nil {
		return nil, err
	}
	return diagnostic, nil
}

func execInvitationCodesRequiredGet(r *http.Request, deps ConfigAgentDeps) (any, error) {
	if deps.Codes == nil {
		return nil, fmt.Errorf("invitation code service unavailable")
	}
	tenantID := RequestTenantID(r)
	required, err := deps.Codes.IsRequiredForTenant(r.Context(), tenantID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":                       true,
		"tenant_id":                tenantID,
		"invitation_code_required": required,
	}, nil
}

func execInvitationCodesRequiredUpdate(r *http.Request, deps ConfigAgentDeps, args map[string]any) (any, error) {
	if deps.Codes == nil {
		return nil, fmt.Errorf("invitation code service unavailable")
	}
	required := false
	if b, ok := args["required"].(bool); ok {
		required = b
	} else if v, ok := args["required"]; ok && v != nil {
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		required = s == "true" || s == "1" || s == "yes" || s == "on" || s == "require" || s == "required"
	}
	tenantID := RequestTenantID(r)
	if err := deps.Codes.SetRequiredForTenant(r.Context(), tenantID, required); err != nil {
		return nil, err
	}
	return map[string]any{
		"ok":                       true,
		"tenant_id":                tenantID,
		"invitation_code_required": required,
	}, nil
}

func execLLMServicesList(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	system = scopedSystemSettingsForRequest(r, system)
	reg, err := llmservice.LoadRegistry(r.Context(), system)
	if err != nil {
		return nil, err
	}
	if reg == nil {
		reg = &llmservice.Registry{}
	}
	groups := make([]map[string]any, 0, len(reg.ModelServiceGroups))
	for _, g := range reg.ModelServiceGroups {
		models := make([]string, 0, len(g.Models))
		for _, m := range g.Models {
			name := strings.TrimSpace(m.Name)
			if name != "" {
				models = append(models, name)
			}
		}
		groups = append(groups, map[string]any{
			"id":            g.ID,
			"name":          g.Name,
			"access_policy": g.AccessPolicy,
			"models":        models,
			"model_count":   len(g.Models),
			"system_free":   llmservice.IsSystemFreeServiceGroup(g.ID),
		})
	}
	return map[string]any{
		"count":                        len(groups),
		"model_service_groups":         groups,
		"system_default_service_group": reg.SystemDefaultServiceGroupID,
		"user_binding_count":           len(reg.UserBindings),
		"card_count":                   len(reg.Cards),
		"grant_count":                  len(reg.Grants),
	}, nil
}

func execGenerateInviteCodes(r *http.Request, deps ConfigAgentDeps, args map[string]any) (any, error) {
	if deps.Codes == nil {
		return nil, fmt.Errorf("invitation code service unavailable")
	}
	count := 1
	switch v := args["count"].(type) {
	case float64:
		count = int(v)
	case int:
		count = v
	case json.Number:
		n, _ := v.Int64()
		count = int(n)
	case string:
		fmt.Sscanf(v, "%d", &count)
	}
	if count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	validity := 0
	switch v := args["validity_days"].(type) {
	case float64:
		validity = int(v)
	case int:
		validity = v
	}
	vip := false
	if b, ok := args["vip"].(bool); ok {
		vip = b
	}
	codes, err := deps.Codes.GenerateCodesForTenantWithOptions(r.Context(), RequestTenantID(r), invitation.GenerateCodeOptions{
		Count:        count,
		ValidityDays: validity,
		VIP:          vip,
	})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(codes))
	for _, c := range codes {
		if c == nil {
			continue
		}
		out = append(out, map[string]any{
			"id": c.ID, "code": c.Code, "status": c.Status, "vip": c.VIP,
			"validity_days": c.ValidityDays,
		})
	}
	return map[string]any{"count": len(out), "codes": out}, nil
}

func enrichConfigAgentPlanSimulated(plan *configAgentPlan, serviceReg *llmservice.Registry) {
	if plan == nil {
		return
	}
	for _, step := range plan.Steps {
		switch step.Tool {
		case "llm.services.user_bind":
			email := strings.ToLower(strings.TrimSpace(fmt.Sprint(step.Args["email"])))
			groupIDs := normalizeServiceGroupIDsArg(step.Args["service_group_ids"])
			diff := simulateUserBindDiff(serviceReg, email, groupIDs)
			if known, missing := validateServiceGroupIDs(serviceReg, groupIDs); len(missing) > 0 {
				diff["unknown_service_group_ids"] = missing
				diff["known_service_group_ids"] = known
			}
			if plan.Simulated == nil {
				plan.Simulated = diff
			} else {
				for k, v := range diff {
					plan.Simulated[k] = v
				}
			}
		case "llm.services.user_unbind":
			email := strings.ToLower(strings.TrimSpace(fmt.Sprint(step.Args["email"])))
			groupIDs := normalizeServiceGroupIDsArg(step.Args["service_group_ids"])
			removeAll := false
			if b, ok := step.Args["remove_all"].(bool); ok {
				removeAll = b
			}
			diff := simulateUserUnbindDiff(serviceReg, email, groupIDs, removeAll)
			if plan.Simulated == nil {
				plan.Simulated = diff
			} else {
				for k, v := range diff {
					plan.Simulated[k] = v
				}
			}
		}
	}
}

func validateServiceGroupIDs(reg *llmservice.Registry, groupIDs []string) (known []string, missing []string) {
	for _, gid := range groupIDs {
		gid = strings.TrimSpace(gid)
		if gid == "" {
			continue
		}
		if reg != nil && reg.FindModelServiceGroup(gid) != nil {
			known = append(known, gid)
		} else {
			missing = append(missing, gid)
		}
	}
	return known, missing
}

func availableServiceGroupIDs(reg *llmservice.Registry) []string {
	if reg == nil {
		return nil
	}
	out := make([]string, 0, len(reg.ModelServiceGroups))
	for _, g := range reg.ModelServiceGroups {
		if id := strings.TrimSpace(g.ID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func execUserServiceBind(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	email := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["email"])))
	if email == "" {
		return nil, fmt.Errorf("email required")
	}
	groupIDs := normalizeServiceGroupIDsArg(args["service_group_ids"])
	if len(groupIDs) == 0 {
		return nil, fmt.Errorf("service_group_ids required")
	}
	reg, err := llmservice.LoadRegistry(r.Context(), system)
	if err != nil {
		return nil, err
	}
	_ = llmservice.EnsureSystemFreeServiceGroup(reg)
	if _, missing := validateServiceGroupIDs(reg, groupIDs); len(missing) > 0 {
		return nil, fmt.Errorf("service group(s) not found: %s (available: %s)",
			strings.Join(missing, ", "), strings.Join(availableServiceGroupIDs(reg), ", "))
	}
	before := simulateUserBindDiff(reg, email, groupIDs)
	found := false
	var afterIDs []string
	for i := range reg.UserBindings {
		if strings.EqualFold(strings.TrimSpace(reg.UserBindings[i].Email), email) {
			// merge unique
			set := map[string]struct{}{}
			for _, id := range reg.UserBindings[i].ServiceGroupIDs {
				set[strings.TrimSpace(id)] = struct{}{}
			}
			for _, id := range groupIDs {
				set[id] = struct{}{}
			}
			merged := make([]string, 0, len(set))
			for id := range set {
				if id != "" {
					merged = append(merged, id)
				}
			}
			reg.UserBindings[i].ServiceGroupIDs = merged
			afterIDs = merged
			found = true
			break
		}
	}
	if !found {
		reg.UserBindings = append(reg.UserBindings, llmservice.UserBinding{
			Email: email, ServiceGroupIDs: groupIDs,
		})
		afterIDs = append([]string{}, groupIDs...)
	}
	reg.Normalize()
	if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
		return nil, err
	}
	invalidateLLMRuntimeCaches(system)
	return map[string]any{
		"email":                     email,
		"service_group_ids":         groupIDs,
		"updated":                   found,
		"created":                   !found,
		"current_service_group_ids": before["current_service_group_ids"],
		"added_service_group_ids":   before["added_service_group_ids"],
		"target_service_group_ids":  afterIDs,
		"merge":                     true,
	}, nil
}

func execUserServiceUnbind(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	email := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["email"])))
	if email == "" {
		return nil, fmt.Errorf("email required")
	}
	removeAll := false
	if b, ok := args["remove_all"].(bool); ok {
		removeAll = b
	} else if v, ok := args["remove_all"]; ok && v != nil {
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		removeAll = s == "true" || s == "1" || s == "yes" || s == "all"
	}
	groupIDs := normalizeServiceGroupIDsArg(args["service_group_ids"])
	if !removeAll && len(groupIDs) == 0 {
		return nil, fmt.Errorf("service_group_ids required (or set remove_all=true)")
	}
	reg, err := llmservice.LoadRegistry(r.Context(), system)
	if err != nil {
		return nil, err
	}
	before := simulateUserUnbindDiff(reg, email, groupIDs, removeAll)
	idx := -1
	var current []string
	for i := range reg.UserBindings {
		if strings.EqualFold(strings.TrimSpace(reg.UserBindings[i].Email), email) {
			idx = i
			current = append([]string{}, reg.UserBindings[i].ServiceGroupIDs...)
			break
		}
	}
	if idx < 0 {
		return map[string]any{
			"email":                     email,
			"ok":                        true,
			"noop":                      true,
			"message":                   "user has no service group bindings",
			"current_service_group_ids": []string{},
			"removed_service_group_ids": []string{},
			"target_service_group_ids":  []string{},
		}, nil
	}
	var after []string
	var removed []string
	if removeAll {
		removed = append([]string{}, current...)
		// drop binding entry
		reg.UserBindings = append(reg.UserBindings[:idx], reg.UserBindings[idx+1:]...)
		after = []string{}
	} else {
		removeSet := map[string]struct{}{}
		for _, id := range groupIDs {
			removeSet[strings.TrimSpace(id)] = struct{}{}
		}
		for _, id := range current {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, drop := removeSet[id]; drop {
				removed = append(removed, id)
				continue
			}
			after = append(after, id)
		}
		if len(after) == 0 {
			reg.UserBindings = append(reg.UserBindings[:idx], reg.UserBindings[idx+1:]...)
		} else {
			reg.UserBindings[idx].ServiceGroupIDs = after
		}
	}
	reg.Normalize()
	if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
		return nil, err
	}
	invalidateLLMRuntimeCaches(system)
	_ = before
	return map[string]any{
		"email":                     email,
		"ok":                        true,
		"remove_all":                removeAll,
		"service_group_ids":         groupIDs,
		"current_service_group_ids": current,
		"removed_service_group_ids": removed,
		"target_service_group_ids":  after,
		"updated":                   len(removed) > 0,
	}, nil
}

func execFeishuConfigGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	raw, err := system.Get(r.Context(), feishuConfigKey)
	if err != nil || raw == "" {
		return FeishuConfigState{}, nil
	}
	var cfg FeishuConfigState
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return FeishuConfigState{}, nil
	}
	if cfg.AppSecret != "" {
		cfg.AppSecret = maskSecret(cfg.AppSecret)
	}
	return cfg, nil
}

func execFeishuConfigUpdate(r *http.Request, deps ConfigAgentDeps, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	cfg := FeishuConfigState{
		AppID:     strings.TrimSpace(fmt.Sprint(args["app_id"])),
		AppSecret: strings.TrimSpace(fmt.Sprint(args["app_secret"])),
	}
	if b, ok := args["enabled"].(bool); ok {
		cfg.Enabled = b
	} else {
		// default enable when credentials provided
		cfg.Enabled = cfg.AppID != "" && cfg.AppSecret != ""
	}
	if isMasked(cfg.AppSecret) {
		old := loadFeishuConfig(r, system)
		cfg.AppSecret = old.AppSecret
	}
	data, _ := json.Marshal(cfg)
	if err := system.Set(r.Context(), feishuConfigKey, string(data)); err != nil {
		return nil, err
	}
	if deps.Feishu != nil && shouldReloadSharedRuntimeForRequest(r) {
		if cfg.Enabled {
			deps.Feishu.Reconfigure(cfg.AppID, cfg.AppSecret)
		} else {
			deps.Feishu.Reconfigure("", "")
		}
	}
	resp := cfg
	if resp.AppSecret != "" {
		resp.AppSecret = maskSecret(resp.AppSecret)
	}
	return resp, nil
}

func execBlocklistAdd(r *http.Request, deps ConfigAgentDeps, args map[string]any) (any, error) {
	if deps.Identity == nil {
		return nil, fmt.Errorf("identity service unavailable")
	}
	email := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["email"])))
	reason := strings.TrimSpace(fmt.Sprint(args["reason"]))
	if email == "" {
		return nil, fmt.Errorf("email required")
	}
	ctx := auth.WithTenant(r.Context(), RequestTenantID(r))
	if err := deps.Identity.AddBlockedEmail(ctx, email, reason); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "email": email, "reason": reason}, nil
}

func execBlocklistList(r *http.Request, deps ConfigAgentDeps) (any, error) {
	if deps.Identity == nil {
		return nil, fmt.Errorf("identity service unavailable")
	}
	items, err := deps.Identity.ListBlockedEmails(auth.WithTenant(r.Context(), RequestTenantID(r)))
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(items), "blocked_emails": items}, nil
}

func execBlocklistRemove(r *http.Request, deps ConfigAgentDeps, args map[string]any) (any, error) {
	if deps.Identity == nil {
		return nil, fmt.Errorf("identity service unavailable")
	}
	email := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["email"])))
	if email == "" {
		return nil, fmt.Errorf("email required")
	}
	if err := deps.Identity.RemoveBlockedEmail(auth.WithTenant(r.Context(), RequestTenantID(r)), email); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "email": email}, nil
}

func execEnrollmentsListPending(r *http.Request, deps ConfigAgentDeps) (any, error) {
	if deps.Identity == nil {
		return nil, fmt.Errorf("identity service unavailable")
	}
	items, err := deps.Identity.ListPendingEnrollments(auth.WithTenant(r.Context(), RequestTenantID(r)))
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, map[string]any{
			"id": item.ID, "email": item.Email, "status": item.Status,
			"note": item.Note, "created_at": item.CreatedAt.Format(time.RFC3339),
		})
	}
	return map[string]any{"count": len(out), "enrollments": out}, nil
}

func resolveEnrollmentID(r *http.Request, deps ConfigAgentDeps, args map[string]any) (string, error) {
	id := strings.TrimSpace(fmt.Sprint(args["id"]))
	if id != "" {
		return id, nil
	}
	email := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["email"])))
	if email == "" {
		return "", fmt.Errorf("id or email required")
	}
	if deps.Identity == nil {
		return "", fmt.Errorf("identity service unavailable")
	}
	items, err := deps.Identity.ListPendingEnrollments(auth.WithTenant(r.Context(), RequestTenantID(r)))
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item != nil && strings.EqualFold(strings.TrimSpace(item.Email), email) {
			return item.ID, nil
		}
	}
	return "", fmt.Errorf("no pending enrollment for %s", email)
}

func execEnrollmentApprove(r *http.Request, deps ConfigAgentDeps, args map[string]any) (any, error) {
	if deps.Identity == nil {
		return nil, fmt.Errorf("identity service unavailable")
	}
	id, err := resolveEnrollmentID(r, deps, args)
	if err != nil {
		return nil, err
	}
	ctx := auth.WithTenant(r.Context(), RequestTenantID(r))
	user, _, err := deps.Identity.ApproveEnrollment(ctx, id)
	if err != nil {
		return nil, err
	}
	if deps.Security != nil && user != nil {
		if err := deps.Security.AssignNewUser(security.WithTenant(r.Context(), user.TenantID), user.Email, ""); err != nil {
			log.Printf("[config-agent] security group assignment failed for %s: %v", user.Email, err)
		}
	}
	out := map[string]any{"ok": true, "enrollment_id": id}
	if user != nil {
		out["user"] = map[string]any{"email": user.Email, "sn": user.SN, "id": user.ID}
	}
	return out, nil
}

func execEnrollmentReject(r *http.Request, deps ConfigAgentDeps, args map[string]any) (any, error) {
	if deps.Identity == nil {
		return nil, fmt.Errorf("identity service unavailable")
	}
	id, err := resolveEnrollmentID(r, deps, args)
	if err != nil {
		return nil, err
	}
	if err := deps.Identity.RejectEnrollment(r.Context(), id); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "enrollment_id": id}, nil
}

func execManualBind(r *http.Request, deps ConfigAgentDeps, args map[string]any) (any, error) {
	if deps.Identity == nil {
		return nil, fmt.Errorf("identity service unavailable")
	}
	email := strings.ToLower(strings.TrimSpace(fmt.Sprint(args["email"])))
	if email == "" {
		return nil, fmt.Errorf("email required")
	}
	user, err := deps.Identity.ManualBindForTenant(r.Context(), RequestTenantID(r), email)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"ok": true,
		"user": map[string]any{
			"id": user.ID, "email": user.Email, "sn": user.SN,
		},
	}, nil
}

func loadJSONConfig(r *http.Request, system store.SystemSettingsRepository, key string, dest any) error {
	raw, err := system.Get(r.Context(), key)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), dest)
}

func execWeComConfigGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	cfg := WeComConfigState{}
	_ = loadJSONConfig(r, system, wecomConfigKey, &cfg)
	if cfg.Secret != "" {
		cfg.Secret = maskSecret(cfg.Secret)
	}
	return cfg, nil
}

func execWeComConfigUpdate(r *http.Request, deps ConfigAgentDeps, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	cfg := WeComConfigState{
		BotID:  strings.TrimSpace(fmt.Sprint(args["bot_id"])),
		Secret: strings.TrimSpace(fmt.Sprint(args["secret"])),
		WSURL:  strings.TrimSpace(fmt.Sprint(args["ws_url"])),
	}
	if b, ok := args["enabled"].(bool); ok {
		cfg.Enabled = b
	} else {
		cfg.Enabled = cfg.BotID != "" && cfg.Secret != ""
	}
	if isMasked(cfg.Secret) {
		old := loadWeComConfig(r, system)
		cfg.Secret = old.Secret
	}
	data, _ := json.Marshal(cfg)
	if err := system.Set(r.Context(), wecomConfigKey, string(data)); err != nil {
		return nil, err
	}
	var reloadErr error
	if deps.WeCom != nil && shouldReloadSharedRuntimeForRequest(r) {
		_ = deps.WeCom.Stop(r.Context())
		if cfg.Enabled {
			_ = deps.WeCom.Start(r.Context())
		}
	} else if isTenantScopedAdminRequest(r) && deps.IMRuntime != nil {
		reloadErr = reloadTenantIMRuntime(r.Context(), deps.IMRuntime, RequestTenantID(r), "wecom")
	}
	resp := cfg
	if resp.Secret != "" {
		resp.Secret = maskSecret(resp.Secret)
	}
	if reloadErr != nil {
		return map[string]any{"config": resp, "reload_error": reloadErr.Error()}, nil
	}
	return resp, nil
}

func execDingTalkConfigGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	cfg := DingTalkConfigState{}
	_ = loadJSONConfig(r, system, dingtalkConfigKey, &cfg)
	if cfg.ClientSecret != "" {
		cfg.ClientSecret = maskSecret(cfg.ClientSecret)
	}
	return cfg, nil
}

func execDingTalkConfigUpdate(r *http.Request, deps ConfigAgentDeps, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	cfg := DingTalkConfigState{
		ClientID:     strings.TrimSpace(fmt.Sprint(args["client_id"])),
		ClientSecret: strings.TrimSpace(fmt.Sprint(args["client_secret"])),
	}
	if b, ok := args["enabled"].(bool); ok {
		cfg.Enabled = b
	} else {
		cfg.Enabled = cfg.ClientID != "" && cfg.ClientSecret != ""
	}
	if isMasked(cfg.ClientSecret) {
		old := loadDingTalkConfig(r, system)
		cfg.ClientSecret = old.ClientSecret
	}
	data, _ := json.Marshal(cfg)
	if err := system.Set(r.Context(), dingtalkConfigKey, string(data)); err != nil {
		return nil, err
	}
	var reloadErr error
	if deps.DingTalk != nil && shouldReloadSharedRuntimeForRequest(r) {
		_ = deps.DingTalk.Stop(r.Context())
		if cfg.Enabled {
			_ = deps.DingTalk.Start(r.Context())
		}
	} else if isTenantScopedAdminRequest(r) && deps.IMRuntime != nil {
		reloadErr = reloadTenantIMRuntime(r.Context(), deps.IMRuntime, RequestTenantID(r), "dingtalk")
	}
	resp := cfg
	if resp.ClientSecret != "" {
		resp.ClientSecret = maskSecret(resp.ClientSecret)
	}
	if reloadErr != nil {
		return map[string]any{"config": resp, "reload_error": reloadErr.Error()}, nil
	}
	return resp, nil
}

func execOpenclawIMConfigGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	cfg := OpenclawIMConfigState{
		WebhookURL: "http://127.0.0.1:3210/outbound",
		Secret:     maskSecret(DefaultOpenclawIMSecret),
	}
	raw, err := system.Get(r.Context(), openclawIMConfigKey)
	if err == nil && strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
		if cfg.Secret != "" {
			cfg.Secret = maskSecret(cfg.Secret)
		}
	}
	return cfg, nil
}

func execOpenclawIMConfigUpdate(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	cfg := OpenclawIMConfigState{
		WebhookURL: strings.TrimSpace(fmt.Sprint(args["webhook_url"])),
		Secret:     strings.TrimSpace(fmt.Sprint(args["secret"])),
	}
	if b, ok := args["enabled"].(bool); ok {
		cfg.Enabled = b
	} else {
		cfg.Enabled = true
	}
	if cfg.WebhookURL == "" {
		cfg.WebhookURL = "http://127.0.0.1:3210/outbound"
	}
	if isMasked(cfg.Secret) || cfg.Secret == "" {
		old := loadOpenclawIMConfig(r, system)
		if isMasked(cfg.Secret) || cfg.Secret == "" {
			if old.Secret != "" {
				cfg.Secret = old.Secret
			} else {
				cfg.Secret = DefaultOpenclawIMSecret
			}
		}
	}
	data, _ := json.Marshal(cfg)
	if err := system.Set(r.Context(), openclawIMConfigKey, string(data)); err != nil {
		return nil, err
	}
	resp := cfg
	if resp.Secret != "" {
		resp.Secret = maskSecret(resp.Secret)
	}
	return resp, nil
}

func execContentAuditConfigGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	cfg := im.ContentAuditDynamicConfig{}
	raw, err := system.Get(r.Context(), contentAuditConfigKey)
	if err == nil && strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg, nil
}

// ConfigAgentHistoryHandler lists recent config-agent plan/execute audit entries.
// Query: limit, id (detail), action=plan|execute|all, intent, q, export=1 (download JSON).
func ConfigAgentHistoryHandler(audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if audit == nil {
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{}, "count": 0})
			return
		}
		// Detail by audit id
		if detailID := strings.TrimSpace(r.URL.Query().Get("id")); detailID != "" {
			writeConfigAgentHistoryDetail(w, r, audit, detailID)
			return
		}
		export := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("export")), "1") ||
			strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("export")), "true") ||
			strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "download")
		limit := 30
		if export {
			limit = 100
		}
		if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
			maxLimit := 100
			if export {
				maxLimit = 500
			}
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxLimit {
				limit = n
			}
		}
		// action filter: plan | execute | all (default)
		wantPlan, wantExec := true, true
		switch normalizeConfigAgentHistoryAction(r.URL.Query().Get("action")) {
		case "plan", "config_agent.plan":
			wantPlan, wantExec = true, false
		case "execute", "config_agent.execute":
			wantPlan, wantExec = false, true
		}
		tenantScoped := false
		tenantID := ""
		if admin := AdminFromContext(r.Context()); admin != nil && strings.TrimSpace(admin.Scope) == "tenant" {
			tenantScoped = true
			tenantID = AdminTenantID(r.Context())
		}
		items := make([]map[string]any, 0, limit)
		appendAudit := func(rows []*store.AdminAuditLog) {
			for _, item := range rows {
				if item == nil {
					continue
				}
				payload := map[string]any{}
				if raw := strings.TrimSpace(item.PayloadJSON); raw != "" {
					_ = json.Unmarshal([]byte(raw), &payload)
				}
				items = append(items, map[string]any{
					"id": item.ID, "action": item.Action, "payload": payload,
					"admin_user_id": item.AdminUserID, "tenant_id": item.TenantID,
					"created_at": item.CreatedAt.Format(time.RFC3339),
				})
			}
		}
		if wantPlan {
			filterPlan := store.AdminAuditLogFilter{Limit: limit, Action: "config_agent.plan"}
			if tenantScoped {
				filterPlan.TenantID = tenantID
				filterPlan.TenantScoped = true
			}
			plans, err := audit.List(r.Context(), filterPlan)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "HISTORY_LIST_FAILED", err.Error())
				return
			}
			appendAudit(plans)
		}
		if wantExec {
			filterExec := store.AdminAuditLogFilter{Limit: limit, Action: "config_agent.execute"}
			if tenantScoped {
				filterExec.TenantID = tenantID
				filterExec.TenantScoped = true
			}
			execs, err := audit.List(r.Context(), filterExec)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "HISTORY_LIST_FAILED", err.Error())
				return
			}
			appendAudit(execs)
		}
		// sort by created_at desc
		sort.Slice(items, func(i, j int) bool {
			return fmt.Sprint(items[i]["created_at"]) > fmt.Sprint(items[j]["created_at"])
		})
		if len(items) > limit {
			items = items[:limit]
		}
		intentQ := strings.TrimSpace(r.URL.Query().Get("intent"))
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if intentQ != "" || q != "" {
			items = filterConfigAgentHistoryItems(items, intentQ, q)
		}
		actionFilter := normalizeConfigAgentHistoryAction(r.URL.Query().Get("action"))
		if actionFilter == "" {
			actionFilter = "all"
		}
		resp := map[string]any{"items": items, "count": len(items), "action": actionFilter, "limit": limit}
		if intentQ != "" {
			resp["intent"] = intentQ
		}
		if q != "" {
			resp["q"] = q
		}
		if export {
			w.Header().Set("Content-Disposition", `attachment; filename="config-agent-history.json"`)
			w.Header().Set("X-Content-Type-Options", "nosniff")
		}
		writeJSON(w, http.StatusOK, resp)
	}
}


func filterConfigAgentHistoryItems(items []map[string]any, intentQ, q string) []map[string]any {
	intentQ = strings.ToLower(strings.TrimSpace(intentQ))
	q = strings.ToLower(strings.TrimSpace(q))
	if intentQ == "" && q == "" {
		return items
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		payload, _ := item["payload"].(map[string]any)
		intent := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["intent"])))
		summary := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["summary"])))
		src := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["source_message"])))
		action := strings.ToLower(strings.TrimSpace(fmt.Sprint(item["action"])))
		if intentQ != "" && !strings.Contains(intent, intentQ) && !strings.Contains(action, intentQ) {
			continue
		}
		if q != "" {
			// Deep-ish search: headers + emails in results/plan args (diagnose, bind, invite).
			blob := intent + " " + summary + " " + src + " " + action + " " + strings.ToLower(fmt.Sprint(item["id"]))
			blob += " " + strings.ToLower(fmt.Sprint(payload["session_id"]))
			blob += " " + configAgentHistorySearchBlob(payload)
			if !strings.Contains(blob, q) {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

// configAgentHistorySearchBlob flattens common nested fields for free-text history search.
func configAgentHistorySearchBlob(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	parts := make([]string, 0, 16)
	appendStr := func(v any) {
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		if s == "" || s == "<nil>" {
			return
		}
		parts = append(parts, s)
	}
	// plan snapshot (from plan audit)
	if plan, ok := payload["plan"].(map[string]any); ok && plan != nil {
		appendStr(plan["summary"])
		appendStr(plan["source_message"])
		if sim, ok := plan["simulated"].(map[string]any); ok {
			appendStr(sim["email"])
		}
		if steps, ok := plan["steps"].([]any); ok {
			for _, raw := range steps {
				step, _ := raw.(map[string]any)
				if step == nil {
					continue
				}
				if args, ok := step["args"].(map[string]any); ok {
					appendStr(args["email"])
					appendStr(args["service_group_id"])
					if ids, ok := args["service_group_ids"].([]any); ok {
						for _, id := range ids {
							appendStr(id)
						}
					}
				}
			}
		}
	}
	// multi-turn session turns (plan audit)
	switch turns := payload["session_turns"].(type) {
	case []any:
		for _, t := range turns {
			appendStr(t)
		}
	case []string:
		for _, t := range turns {
			appendStr(t)
		}
	}
	// execute results (diagnose / bind / etc.)
	if results, ok := payload["results"].([]any); ok {
		for _, raw := range results {
			entry, _ := raw.(map[string]any)
			if entry == nil {
				continue
			}
			appendStr(entry["tool"])
			appendStr(entry["error"])
			if res, ok := entry["result"].(map[string]any); ok && res != nil {
				appendStr(res["email"])
				appendStr(res["user_id"])
				if ids, ok := res["service_group_ids"].([]any); ok {
					for _, id := range ids {
						appendStr(id)
					}
				}
				if added, ok := res["added_service_group_ids"].([]any); ok {
					for _, id := range added {
						appendStr(id)
					}
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

func normalizeConfigAgentHistoryAction(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "plan", "config_agent.plan", "plans":
		return "plan"
	case "execute", "config_agent.execute", "exec", "executed":
		return "execute"
	case "all", "":
		return ""
	default:
		return v
	}
}

func writeConfigAgentHistoryDetail(w http.ResponseWriter, r *http.Request, audit store.AdminAuditRepository, id string) {
	// Scan recent plan+execute rows for matching audit id.
	filters := []store.AdminAuditLogFilter{
		{Limit: 100, Action: "config_agent.plan"},
		{Limit: 100, Action: "config_agent.execute"},
	}
	if admin := AdminFromContext(r.Context()); admin != nil && strings.TrimSpace(admin.Scope) == "tenant" {
		tid := AdminTenantID(r.Context())
		for i := range filters {
			filters[i].TenantID = tid
			filters[i].TenantScoped = true
		}
	}
	for _, filter := range filters {
		rows, err := audit.List(r.Context(), filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "HISTORY_DETAIL_FAILED", err.Error())
			return
		}
		for _, item := range rows {
			if item == nil || item.ID != id {
				continue
			}
			payload := map[string]any{}
			if raw := strings.TrimSpace(item.PayloadJSON); raw != "" {
				_ = json.Unmarshal([]byte(raw), &payload)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": true,
				"item": map[string]any{
					"id": item.ID, "action": item.Action, "payload": payload,
					"admin_user_id": item.AdminUserID, "tenant_id": item.TenantID,
					"created_at": item.CreatedAt.Format(time.RFC3339),
				},
			})
			return
		}
	}
	writeError(w, http.StatusNotFound, "HISTORY_NOT_FOUND", "audit entry not found")
}

func configAgentToolDomain(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.Index(name, "."); i > 0 {
		return name[:i]
	}
	if name == "" {
		return "other"
	}
	return name
}

func configAgentToolMode(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasSuffix(name, ".get") || strings.HasSuffix(name, ".list") ||
		strings.HasSuffix(name, ".diagnose") || strings.HasSuffix(name, ".status") ||
		strings.Contains(name, "list_") {
		return "read"
	}
	if strings.HasSuffix(name, ".test") {
		return "probe"
	}
	return "write"
}

// configAgentToolExample returns a natural-language sample for the tool catalog UI.
func configAgentToolExample(name string) string {
	switch strings.TrimSpace(name) {
	case "llm.services.diagnose":
		return "diagnose LLM service for demo@example.com"
	case "llm.services.list":
		return "list service groups"
	case "llm.services.user_bind":
		return "bind user demo@example.com to system-free"
	case "llm.services.user_unbind":
		return "unbind user demo@example.com from system-free"
	case "invitation_codes.export":
		return "export invitation codes"
	case "invitation_codes.list":
		return "list invitation codes"
	case "invitation_codes.generate":
		return "generate 3 invitation codes"
	case "invitation_codes.required.get":
		return "show invitation code required status"
	case "users.invite.create":
		return "invite user demo@example.com as member"
	case "system_free.get":
		return "show system-free status"
	case "system_free.test":
		return "test system-free"
	case "migration.settings.get":
		return "show migration settings"
	case "migration.settings.update":
		return "set migration max to 200MB"
	case "feishu.auto_enroll.get":
		return "show feishu auto enroll"
	case "card_store.config.get":
		return "show card store config"
	case "mail.sender_name.get":
		return "show mail sender name"
	case "llm.providers.get":
		return "list LLM providers"
	case "enrollments.list_pending":
		return "list pending enrollments"
	case "users.blocklist.add":
		return "block email spam@example.com reason abuse"
	case "wecom.config.get":
		return "show wecom config"
	case "dingtalk.config.get":
		return "show dingtalk config"
	case "qqbot.config.get":
		return "show qqbot config"
	case "bridge.channels.list":
		return "list bridge channels"
	case "content_audit.config.get":
		return "show content audit config"
	case "smart_route_all.get":
		return "show smart route"
	case "feishu.config.get":
		return "show feishu config"
	default:
		// Generic fallback for *.get / *.list / *.test
		if strings.HasSuffix(name, ".get") || strings.HasSuffix(name, ".list") || strings.HasSuffix(name, ".test") {
			s := strings.ReplaceAll(name, ".", " ")
			s = strings.TrimSuffix(s, " get")
			s = strings.TrimSuffix(s, " list")
			s = strings.TrimSuffix(s, " test")
			return "show " + strings.TrimSpace(s)
		}
		return strings.ReplaceAll(name, ".", " ")
	}
}

// ConfigAgentCatalogHandler returns the tool catalog for UI help.
func ConfigAgentCatalogHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tools := make([]map[string]any, 0, len(configAgentAllowedTools))
		for name := range configAgentAllowedTools {
			preview := defaultAPIPreviewForTool(name)
			tools = append(tools, map[string]any{
				"name":        name,
				"mode":        configAgentToolMode(name),
				"domain":      configAgentToolDomain(name),
				"example":     configAgentToolExample(name),
				"api_preview": preview,
			})
		}
		sort.Slice(tools, func(i, j int) bool {
			return fmt.Sprint(tools[i]["name"]) < fmt.Sprint(tools[j]["name"])
		})
		// Prefer curated examples; also surface a few tool-derived samples.
		examples := []string{
			"Add LLM provider ...",
			"Test system-free",
			"Invite user a@b.com as member",
			"List pending enrollments",
			"Enable telegram channel botToken xxx",
			"Show wecom config",
			"Update content audit keywords: spam, ads",
			"Show migration settings",
			"Set migration max to 200MB",
			"Show feishu auto enroll",
			"Enable card store",
			"List invitation codes",
			"Show invitation code required status",
			"List service groups",
			"Diagnose LLM service for a@b.com",
			"Export invitation codes",
			"Unbind user a@b.com from system-free",
			"Bind user a@b.com to system-free",
		}
		// Domains list for UI filters
		domainSet := map[string]struct{}{}
		for _, t := range tools {
			domainSet[fmt.Sprint(t["domain"])] = struct{}{}
		}
		domains := make([]string, 0, len(domainSet))
		for d := range domainSet {
			if d != "" && d != "<nil>" {
				domains = append(domains, d)
			}
		}
		sort.Strings(domains)
		writeJSON(w, http.StatusOK, map[string]any{
			"tools":    tools,
			"examples": examples,
			"domains":  domains,
			"count":    len(tools),
		})
	}
}

func execQQBotConfigGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	cfg := QQBotConfigState{}
	_ = loadJSONConfig(r, system, qqbotConfigKey, &cfg)
	if cfg.AppSecret != "" {
		cfg.AppSecret = maskSecret(cfg.AppSecret)
	}
	return cfg, nil
}

func execQQBotConfigUpdate(r *http.Request, deps ConfigAgentDeps, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	cfg := QQBotConfigState{
		AppID:     strings.TrimSpace(fmt.Sprint(args["app_id"])),
		AppSecret: strings.TrimSpace(fmt.Sprint(args["app_secret"])),
	}
	if b, ok := args["enabled"].(bool); ok {
		cfg.Enabled = b
	} else {
		cfg.Enabled = cfg.AppID != "" && cfg.AppSecret != ""
	}
	if isMasked(cfg.AppSecret) {
		old := loadQQBotConfig(r, system)
		cfg.AppSecret = old.AppSecret
	}
	data, _ := json.Marshal(cfg)
	if err := system.Set(r.Context(), qqbotConfigKey, string(data)); err != nil {
		return nil, err
	}
	var reloadErr error
	if deps.QQBot != nil && shouldReloadSharedRuntimeForRequest(r) {
		_ = deps.QQBot.Stop(r.Context())
		if cfg.Enabled {
			_ = deps.QQBot.Start(r.Context())
		}
	} else if isTenantScopedAdminRequest(r) && deps.IMRuntime != nil {
		reloadErr = reloadTenantIMRuntime(r.Context(), deps.IMRuntime, RequestTenantID(r), "qqbot")
	}
	resp := cfg
	if resp.AppSecret != "" {
		resp.AppSecret = maskSecret(resp.AppSecret)
	}
	if reloadErr != nil {
		return map[string]any{"config": resp, "reload_error": reloadErr.Error()}, nil
	}
	return resp, nil
}

func execBridgeChannelsList(r *http.Request, system store.SystemSettingsRepository, bridgeDir string) (any, error) {
	saved := loadChannelStates(r, system)
	type channelResp struct {
		ID        string            `json:"id"`
		Name      string            `json:"name"`
		Enabled   bool              `json:"enabled"`
		Config    map[string]string `json:"config"`
		Installed bool              `json:"installed"`
	}
	result := make([]channelResp, 0, len(knownChannels))
	for _, ch := range knownChannels {
		cr := channelResp{ID: ch.ID, Name: ch.Name, Config: map[string]string{}}
		if st, ok := saved[ch.ID]; ok {
			cr.Enabled = st.Enabled
			if st.Fields != nil {
				// mask password-like fields
				cr.Config = map[string]string{}
				for k, v := range st.Fields {
					lk := strings.ToLower(k)
					if strings.Contains(lk, "token") || strings.Contains(lk, "secret") || strings.Contains(lk, "password") {
						cr.Config[k] = maskSecret(v)
					} else {
						cr.Config[k] = v
					}
				}
			}
		}
		cr.Installed = isNpmPackageInstalled(bridgeDir, ch.Package) || (ch.AltPackage != "" && isNpmPackageInstalled(bridgeDir, ch.AltPackage))
		result = append(result, cr)
	}
	return map[string]any{"channels": result, "count": len(result)}, nil
}

func execBridgeChannelSave(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	return execBridgeChannelSaveWithDeps(r, ConfigAgentDeps{System: system}, system, args)
}

func execBridgeChannelSaveWithDeps(r *http.Request, deps ConfigAgentDeps, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	id := strings.TrimSpace(fmt.Sprint(args["id"]))
	if id == "" {
		return nil, fmt.Errorf("channel id required")
	}
	var known *KnownChannel
	for i := range knownChannels {
		if knownChannels[i].ID == id {
			known = &knownChannels[i]
			break
		}
	}
	if known == nil {
		return nil, fmt.Errorf("unknown channel %q", id)
	}
	enabled := false
	if b, ok := args["enabled"].(bool); ok {
		enabled = b
	} else {
		enabled = true
	}
	installNPM := false
	if b, ok := args["install_npm"].(bool); ok {
		installNPM = b
	}
	validKeys := map[string]bool{}
	for _, f := range known.Fields {
		validKeys[f.Key] = true
	}
	cleanFields := map[string]string{}
	// fields may be map[string]any or map[string]string
	switch fv := args["fields"].(type) {
	case map[string]string:
		for k, v := range fv {
			if validKeys[k] {
				cleanFields[k] = v
			}
		}
	case map[string]any:
		for k, v := range fv {
			if validKeys[k] {
				cleanFields[k] = strings.TrimSpace(fmt.Sprint(v))
			}
		}
	}
	// also accept top-level common keys
	if token := strings.TrimSpace(fmt.Sprint(args["botToken"])); token != "" && validKeys["botToken"] {
		cleanFields["botToken"] = token
	}
	saved := loadChannelStates(r, system)
	// preserve existing secrets if masked/empty
	if prev, ok := saved[id]; ok && prev.Fields != nil {
		for k, oldVal := range prev.Fields {
			newVal := cleanFields[k]
			if newVal == "" || isMasked(newVal) {
				cleanFields[k] = oldVal
			}
		}
	}
	saved[id] = ChannelState{Enabled: enabled, Fields: cleanFields}
	data, _ := json.Marshal(saved)
	if err := system.Set(r.Context(), bridgeChannelsConfigKey, string(data)); err != nil {
		return nil, err
	}

	bridgeDir := deps.BridgeDir
	installMsg := ""
	if installNPM && enabled && bridgeDir != "" {
		if !isNpmPackageInstalled(bridgeDir, known.Package) {
			if err := npmInstallPackage(bridgeDir, known.Package); err != nil {
				if known.AltPackage != "" {
					if err2 := npmInstallPackage(bridgeDir, known.AltPackage); err2 != nil {
						installMsg = fmt.Sprintf("npm install failed for both %s and %s", known.Package, known.AltPackage)
					} else {
						installMsg = fmt.Sprintf("npm install %s succeeded (fallback)", known.AltPackage)
					}
				} else {
					installMsg = fmt.Sprintf("npm install %s failed: %v", known.Package, err)
				}
			} else {
				installMsg = fmt.Sprintf("npm install %s succeeded", known.Package)
			}
		} else {
			installMsg = "package already installed"
		}
	} else if enabled && !installNPM {
		installMsg = "npm install skipped (set install_npm=true to install)"
	}

	configErr := ""
	if shouldWriteSharedBridgeConfig(r, bridgeDir) {
		if err := writeBridgeConfig(r, system, bridgeDir, saved); err != nil {
			configErr = err.Error()
		}
	}

	// Mask secrets in response
	masked := map[string]string{}
	for k, v := range cleanFields {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "token") || strings.Contains(lk, "secret") || strings.Contains(lk, "password") {
			masked[k] = maskSecret(v)
		} else {
			masked[k] = v
		}
	}
	return map[string]any{
		"id": id, "enabled": enabled, "fields": masked,
		"install_msg": installMsg, "config_err": configErr,
		"install_npm": installNPM,
	}, nil
}

func execMailSenderNameGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	scoped := scopedSystemSettingsForRequest(r, system)
	raw, err := scoped.Get(r.Context(), "mail_sender_name")
	if err != nil {
		return nil, err
	}
	state := TenantMailSenderNameState{}
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &state)
	}
	return state, nil
}

func execMailSenderNameUpdate(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	name := strings.TrimSpace(fmt.Sprint(args["from_name"]))
	if name == "" {
		return nil, fmt.Errorf("from_name required")
	}
	if len([]rune(name)) > 80 {
		return nil, fmt.Errorf("from_name must be 80 characters or fewer")
	}
	state := TenantMailSenderNameState{FromName: name}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	scoped := scopedSystemSettingsForRequest(r, system)
	if err := scoped.Set(r.Context(), "mail_sender_name", string(data)); err != nil {
		return nil, err
	}
	return state, nil
}

func execSmartRouteAllGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	system = scopedSystemSettingsForRequest(r, system)
	raw, _ := system.Get(r.Context(), smartRouteAllKey)
	return map[string]any{"enabled": raw == "true"}, nil
}

func execSmartRouteAllUpdate(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	system = scopedSystemSettingsForRequest(r, system)
	enabled := false
	if b, ok := args["enabled"].(bool); ok {
		enabled = b
	}
	val := "false"
	if enabled {
		val = "true"
	}
	if err := system.Set(r.Context(), smartRouteAllKey, val); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "enabled": enabled}, nil
}

func execRegistrationAuthGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	cfg, err := loadRegistrationAuthConfig(r, scopedSystemSettingsForRequest(r, system))
	if err != nil {
		return nil, err
	}
	// mask secret
	if cfg.AliyunAccessKeySecret != "" {
		cfg.AliyunAccessKeySecret = maskSecret(cfg.AliyunAccessKeySecret)
	}
	return cfg, nil
}

func execRegistrationAuthUpdate(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	scoped := scopedSystemSettingsForRequest(r, system)
	cfg, err := loadRegistrationAuthConfig(r, scoped)
	if err != nil {
		return nil, err
	}
	if v := strings.TrimSpace(fmt.Sprint(args["method"])); v != "" {
		cfg.Method = v
	}
	if v := strings.TrimSpace(fmt.Sprint(args["aliyun_access_key_id"])); v != "" {
		cfg.AliyunAccessKeyID = v
	}
	if v := strings.TrimSpace(fmt.Sprint(args["aliyun_access_key_secret"])); v != "" && !isMasked(v) {
		cfg.AliyunAccessKeySecret = v
	}
	if v := strings.TrimSpace(fmt.Sprint(args["aliyun_sign_name"])); v != "" {
		cfg.AliyunSignName = v
	}
	if v := strings.TrimSpace(fmt.Sprint(args["aliyun_template_code"])); v != "" {
		cfg.AliyunTemplateCode = v
	}
	cfg = normalizeRegistrationAuthConfig(cfg)
	if err := validateRegistrationAuthConfig(cfg); err != nil {
		return nil, err
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := scoped.Set(r.Context(), registrationAuthConfigKey, string(data)); err != nil {
		return nil, err
	}
	if cfg.AliyunAccessKeySecret != "" {
		cfg.AliyunAccessKeySecret = maskSecret(cfg.AliyunAccessKeySecret)
	}
	return cfg, nil
}


func execFeishuAutoEnrollGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	scoped := scopedSystemSettingsForRequest(r, system)
	return feishu.LoadAutoEnrollSetting(r.Context(), scoped), nil
}

func execFeishuAutoEnrollUpdate(r *http.Request, deps ConfigAgentDeps, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	scoped := scopedSystemSettingsForRequest(r, system)
	cfg := feishu.LoadAutoEnrollSetting(r.Context(), scoped)
	if b, ok := args["enabled"].(bool); ok {
		cfg.Enabled = b
	} else if v, ok := args["enabled"]; ok && v != nil {
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		cfg.Enabled = s == "true" || s == "1" || s == "yes" || s == "on"
	}
	if v := strings.TrimSpace(fmt.Sprint(args["department_id"])); v != "" && v != "<nil>" {
		cfg.DepartmentID = v
	}
	if b, ok := args["use_lark"].(bool); ok {
		cfg.UseLark = b
	} else if v, ok := args["use_lark"]; ok && v != nil {
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		if s != "" && s != "<nil>" {
			cfg.UseLark = s == "true" || s == "1" || s == "yes" || s == "lark"
		}
	}
	if n, ok := toInt64(args["employee_type"]); ok && n > 0 {
		cfg.EmployeeType = int(n)
	}
	if err := feishu.SaveAutoEnrollSetting(r.Context(), scoped, cfg); err != nil {
		return nil, err
	}
	if deps.Feishu != nil && shouldReloadSharedRuntimeForRequest(r) {
		if ae := deps.Feishu.AutoEnroller(); ae != nil {
			ae.SetConfig(cfg)
		}
	}
	return cfg, nil
}

func execCardStoreConfigGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	cfg := loadCardStoreConfig(r.Context(), scopedSystemSettingsForRequest(r, system))
	if strings.TrimSpace(cfg.NotifyURL) == "" {
		cfg.NotifyURL = defaultCardStoreNotifyURL(r)
	}
	cfg.AccessKey = ""
	cfg.AlipayDirect.PrivateKey = ""
	return cfg, nil
}

func execCardStoreConfigUpdate(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	scoped := scopedSystemSettingsForRequest(r, system)
	cfg := loadCardStoreConfig(r.Context(), scoped)
	touched := false
	if b, ok := args["enabled"].(bool); ok {
		cfg.Enabled = b
		touched = true
	} else if v, ok := args["enabled"]; ok && v != nil {
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		if s != "" && s != "<nil>" {
			cfg.Enabled = s == "true" || s == "1" || s == "yes" || s == "on"
			touched = true
		}
	}
	if v := strings.TrimSpace(fmt.Sprint(args["payment_mode"])); v != "" && v != "<nil>" {
		if mode, ok := parseCardStorePaymentMode(v); ok {
			cfg.PaymentMode = mode
			touched = true
		} else {
			return nil, fmt.Errorf("invalid payment_mode %q (use personal_semimanual|alipay_direct)", v)
		}
	}
	if !touched {
		return nil, fmt.Errorf("enabled or payment_mode required")
	}
	cfg = normalizeCardStoreConfig(cfg)
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := scoped.Set(r.Context(), cardStoreConfigKey, string(data)); err != nil {
		return nil, err
	}
	cfg.AccessKey = ""
	cfg.AlipayDirect.PrivateKey = ""
	return cfg, nil
}

func execInvitationCodesList(r *http.Request, deps ConfigAgentDeps, args map[string]any) (any, error) {
	if deps.Codes == nil {
		return nil, fmt.Errorf("invitation code service unavailable")
	}
	status := strings.TrimSpace(fmt.Sprint(args["status"]))
	if status == "<nil>" {
		status = ""
	}
	search := strings.TrimSpace(fmt.Sprint(args["search"]))
	if search == "<nil>" {
		search = ""
	}
	page := 1
	if n, ok := toInt64(args["page"]); ok && n > 0 {
		page = int(n)
	}
	pageSize := 20
	if n, ok := toInt64(args["page_size"]); ok && n > 0 {
		pageSize = int(n)
	}
	if pageSize > 200 {
		pageSize = 200
	}
	codes, total, err := deps.Codes.ListCodesPagedForTenant(r.Context(), RequestTenantID(r), status, search, page, pageSize)
	if err != nil {
		return nil, err
	}
	resp := make([]invitationCodeResponse, len(codes))
	for i, c := range codes {
		resp[i] = toInvitationCodeResponse(c)
	}
	return map[string]any{"codes": resp, "total": total, "page": page, "page_size": pageSize}, nil
}

func execMigrationSettingsGet(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	scoped := scopedSystemSettingsForRequest(r, system)
	raw, err := scoped.Get(r.Context(), migrationSettingMaxCompressedBytes)
	value := migrationDefaultMaxCompressedBytes
	if err == nil && strings.TrimSpace(raw) != "" {
		var payload struct {
			Value int64 `json:"value"`
		}
		if json.Unmarshal([]byte(raw), &payload) == nil && payload.Value > 0 {
			value = payload.Value
		}
	}
	value = clampMigrationLimit(value)
	return map[string]any{
		"max_compressed_bytes": value,
		"max_mb":               value / (1024 * 1024),
		"min_bytes":            migrationMinCompressedBytes,
		"max_bytes":            migrationMaxCompressedBytes,
		"min_mb":               migrationMinCompressedBytes / (1024 * 1024),
		"max_mb_limit":         migrationMaxCompressedBytes / (1024 * 1024),
	}, nil
}

func execMigrationSettingsUpdate(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	bytesVal, ok := parseMigrationMaxBytesArgs(args)
	if !ok {
		return nil, fmt.Errorf("max_compressed_bytes or max_mb required")
	}
	value := clampMigrationLimit(bytesVal)
	data, err := json.Marshal(map[string]int64{"value": value})
	if err != nil {
		return nil, err
	}
	scoped := scopedSystemSettingsForRequest(r, system)
	if err := scoped.Set(r.Context(), migrationSettingMaxCompressedBytes, string(data)); err != nil {
		return nil, err
	}
	return map[string]any{
		"max_compressed_bytes": value,
		"max_mb":               value / (1024 * 1024),
		"min_bytes":            migrationMinCompressedBytes,
		"max_bytes":            migrationMaxCompressedBytes,
	}, nil
}

func parseMigrationMaxBytesArgs(args map[string]any) (int64, bool) {
	if args == nil {
		return 0, false
	}
	if v, ok := args["max_compressed_bytes"]; ok && v != nil {
		if n, ok2 := toInt64(v); ok2 && n > 0 {
			return n, true
		}
	}
	if v, ok := args["max_mb"]; ok && v != nil {
		if n, ok2 := toInt64(v); ok2 && n > 0 {
			return n * 1024 * 1024, true
		}
	}
	return 0, false
}

func toInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int:
		return int64(t), true
	case int32:
		return int64(t), true
	case int64:
		return t, true
	case float64:
		if t > 0 {
			return int64(t), true
		}
	case float32:
		if t > 0 {
			return int64(t), true
		}
	case json.Number:
		n, err := t.Int64()
		if err == nil {
			return n, true
		}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, false
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err == nil {
			return n, true
		}
		f, err := strconv.ParseFloat(s, 64)
		if err == nil && f > 0 {
			return int64(f), true
		}
	}
	return 0, false
}


func execContentAuditConfigUpdate(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	cfg := im.ContentAuditDynamicConfig{}
	raw, _ := system.Get(r.Context(), contentAuditConfigKey)
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	if v, ok := args["program_path"]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
		cfg.ProgramPath = strings.TrimSpace(fmt.Sprint(v))
	}
	if v, ok := args["timeout_policy"]; ok && strings.TrimSpace(fmt.Sprint(v)) != "" {
		cfg.TimeoutPolicy = strings.TrimSpace(fmt.Sprint(v))
	}
	switch v := args["timeout_seconds"].(type) {
	case float64:
		cfg.TimeoutSeconds = int(v)
	case int:
		cfg.TimeoutSeconds = v
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 5
	}
	if cfg.TimeoutPolicy == "" {
		cfg.TimeoutPolicy = "block"
	}
	if kws := normalizeServiceGroupIDsArg(args["keywords"]); len(kws) > 0 {
		// reuse string slice normalizer name is misleading but works for string lists
		cfg.Keywords = kws
	} else if rawKW, ok := args["keywords"].([]any); ok {
		out := make([]string, 0, len(rawKW))
		for _, item := range rawKW {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			cfg.Keywords = out
		}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := system.Set(r.Context(), contentAuditConfigKey, string(data)); err != nil {
		return nil, err
	}
	return cfg, nil
}

func normalizeServiceGroupIDsArg(v any) []string {
	out := []string{}
	switch t := v.(type) {
	case []string:
		out = append(out, t...)
	case []any:
		for _, item := range t {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
	case string:
		for _, p := range strings.FieldsFunc(t, func(r rune) bool {
			return r == ',' || r == ';' || r == ' '
		}) {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
	}
	// unique
	seen := map[string]struct{}{}
	uniq := make([]string, 0, len(out))
	for _, id := range out {
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		uniq = append(uniq, id)
	}
	return uniq
}

func upsertProviderFromPlanArgs(r *http.Request, system store.SystemSettingsRepository, args map[string]any) (any, error) {
	id := strings.TrimSpace(fmt.Sprint(args["id"]))
	name := strings.TrimSpace(fmt.Sprint(args["name"]))
	apiURL := strings.TrimSpace(fmt.Sprint(args["api_url"]))
	apiKey := strings.TrimSpace(fmt.Sprint(args["api_key"]))
	model := strings.TrimSpace(fmt.Sprint(args["model"]))
	if id == "" || apiURL == "" || apiKey == "" || model == "" {
		return nil, fmt.Errorf("id, api_url, api_key, model are required")
	}
	if name == "" {
		name = id
	}
	reg, err := im.LoadLLMProviderRegistry(r.Context(), system)
	if err != nil {
		return nil, err
	}
	protocol := strings.TrimSpace(fmt.Sprint(args["protocol"]))
	if protocol == "" {
		protocol = "openai"
	}
	wireAPI := strings.TrimSpace(fmt.Sprint(args["wire_api"]))
	if wireAPI == "" {
		wireAPI = "chat"
	}
	next := im.LLMProvider{
		ID: id, Name: name, APIURL: apiURL, APIKey: apiKey, Model: model,
		Protocol: protocol, WireAPI: wireAPI,
	}
	found := false
	for i := range reg.Providers {
		if strings.EqualFold(strings.TrimSpace(reg.Providers[i].ID), id) {
			if next.APIKey == "" {
				next.APIKey = reg.Providers[i].APIKey
			}
			reg.Providers[i] = next
			found = true
			break
		}
	}
	if !found {
		reg.Providers = append(reg.Providers, next)
	}
	if !reg.Enabled {
		reg.Enabled = true
	}
	if err := im.SaveLLMProviderRegistry(r.Context(), system, reg); err != nil {
		return nil, err
	}
	invalidateLLMRuntimeCaches(system)
	return map[string]any{
		"provider_id": id, "name": name, "api_url": apiURL, "model": model,
		"updated": found, "created": !found,
	}, nil
}

func liveTestSystemFree(r *http.Request, system store.SystemSettingsRepository) (any, error) {
	reg, err := loadSystemFreeRegistry(r, system)
	if err != nil {
		return nil, err
	}
	providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
	if err != nil {
		return nil, err
	}
	status := llmservice.EvaluateSystemFreeStatus(reg, configuredProviderIDSet(providerReg))
	if !status.Present {
		return map[string]any{"ok": false, "status": status}, fmt.Errorf("system-free missing")
	}
	models, _ := llmservice.BuildAuthorizedModelsForServiceGroups(reg, []string{llmservice.SystemFreeServiceGroupID})
	models = filterAuthorizedModelsForConfiguredProviders(models, providerReg)
	if len(models) == 0 {
		return map[string]any{"ok": false, "status": status}, fmt.Errorf("system-free has no routable models")
	}
	body := map[string]any{
		"model": "auto",
		"messages": []map[string]string{
			{"role": "user", "content": "Reply with exactly: pong"},
		},
	}
	model, externalModel, err := resolveAuthorizedModel(body, models)
	if err != nil {
		return map[string]any{"ok": false, "status": status}, err
	}
	start := time.Now()
	providerID := ""
	if len(model.ProviderIDs) > 0 {
		providerID = strings.TrimSpace(model.ProviderIDs[0])
	}
	if llmservice.IsBuiltinProvider(providerID) {
		respBody, statusCode, usedProviderID, serviceGroupIDs, fwdErr := forwardAuthorizedModelRequest(r, providerReg, model, body, externalModel)
		elapsed := time.Since(start)
		if fwdErr != nil {
			return map[string]any{
				"ok": false, "error": fwdErr.Error(), "provider_id": usedProviderID,
				"service_groups": serviceGroupIDs, "status_code": statusCode,
				"latency_ms": elapsed.Milliseconds(), "status": status,
			}, fwdErr
		}
		if statusCode < 200 || statusCode >= 300 {
			err := fmt.Errorf("upstream HTTP %d: %s", statusCode, workflowDraftProviderResponseSnippet(respBody))
			return map[string]any{
				"ok": false, "error": err.Error(), "provider_id": usedProviderID,
				"status_code": statusCode, "latency_ms": elapsed.Milliseconds(), "status": status,
			}, err
		}
		return map[string]any{
			"ok": true, "provider_id": usedProviderID, "model": model.Name,
			"latency_ms": elapsed.Milliseconds(), "status": status,
		}, nil
	}
	local := providerReg.FindProvider(providerID)
	if local == nil || strings.TrimSpace(local.APIURL) == "" || strings.TrimSpace(local.APIKey) == "" {
		return map[string]any{"ok": false, "status": status}, fmt.Errorf("provider %q not fully configured", providerID)
	}
	cfg := corelib.MaclawLLMConfig{
		URL: local.APIURL, Key: local.APIKey,
		Model:     systemFreeFirstNonEmpty(local.Model, model.Name, "auto"),
		Protocol:  normalizeProviderProtocol(local.Protocol),
		WireAPI:   normalizeProviderWireAPI(local.WireAPI),
		AgentType: strings.TrimSpace(local.AgentType),
	}
	resp, err := agent.DoSimpleLLMRequest(cfg, []interface{}{
		map[string]string{"role": "user", "content": "Reply with exactly: pong"},
	}, http.DefaultClient, 15*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		return map[string]any{
			"ok": false, "error": err.Error(), "provider_id": providerID,
			"latency_ms": elapsed.Milliseconds(), "status": status,
		}, err
	}
	return map[string]any{
		"ok": true, "provider_id": providerID, "model": cfg.Model,
		"reply": resp.Content, "latency_ms": elapsed.Milliseconds(), "status": status,
	}, nil
}

func liveTestLLMProvider(r *http.Request, system store.SystemSettingsRepository, providerID string) (any, error) {
	reg, err := im.LoadLLMProviderRegistry(r.Context(), system)
	if err != nil {
		return nil, err
	}
	p := reg.FindProvider(providerID)
	if p == nil {
		return nil, fmt.Errorf("provider %q not found", providerID)
	}
	if strings.TrimSpace(p.APIURL) == "" || strings.TrimSpace(p.APIKey) == "" || strings.TrimSpace(p.Model) == "" {
		return nil, fmt.Errorf("provider %q missing api_url/api_key/model", providerID)
	}
	cfg := corelib.MaclawLLMConfig{
		URL: p.APIURL, Key: p.APIKey, Model: p.Model,
		Protocol:  normalizeProviderProtocol(p.Protocol),
		WireAPI:   normalizeProviderWireAPI(p.WireAPI),
		AgentType: strings.TrimSpace(p.AgentType),
	}
	start := time.Now()
	resp, err := agent.DoSimpleLLMRequest(cfg, []interface{}{
		map[string]string{"role": "user", "content": "Reply with exactly: pong"},
	}, http.DefaultClient, 12*time.Second)
	elapsed := time.Since(start)
	if err != nil {
		return map[string]any{
			"ok": false, "provider_id": providerID, "error": err.Error(),
			"latency_ms": elapsed.Milliseconds(),
		}, err
	}
	return map[string]any{
		"ok": true, "provider_id": providerID, "model": p.Model,
		"reply": resp.Content, "latency_ms": elapsed.Milliseconds(),
	}, nil
}

func sanitizePlanSteps(steps []configAgentStep) []configAgentStep {
	out := make([]configAgentStep, len(steps))
	for i, s := range steps {
		out[i] = s
		if s.Args == nil {
			continue
		}
		args := map[string]any{}
		for k, v := range s.Args {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "key") || strings.Contains(lk, "secret") || strings.Contains(lk, "token") {
				args[k] = maskConfigAgentSecret(fmt.Sprint(v))
			} else {
				args[k] = v
			}
		}
		out[i].Args = args
	}
	return out
}

