package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// registerGroupDiscussionTools exposes current-Hub-only MaClaw group discussion
// to the agent loop. Starting a discussion is intentionally split from suggesting
// one: start_authorized must only be called after explicit human authorization.
func registerGroupDiscussionTools(registry *ToolRegistry, app *App, handler *IMMessageHandler) {
	if registry == nil || app == nil {
		return
	}
	registry.Register(RegisteredTool{
		Name:        "group_discussion",
		Description: "Use current-Hub-only MaClaw group discussion for difficult tasks. Actions: status, list_experts, list_mine, get_discussion, get_detail, readiness, summarize_result, cleanup_stale, process_invites, suggest, start_authorized, send_message, submit_result, set_state. Must ask the human before start_authorized unless local policy explicitly permits same-security-group free discussion.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"group", "discussion", "consultation", "expert", "maclaw", "hub", "collaboration"},
		Priority:    8,
		Status:      RegToolAvailable,
		Required:    []string{"action"},
		InputSchema: map[string]interface{}{
			"action":          map[string]interface{}{"type": "string", "description": "status | list_experts | list_mine | get_discussion | get_detail | readiness | summarize_result | cleanup_stale | process_invites | suggest | start_authorized | send_message | submit_result | set_state"},
			"topic":           map[string]interface{}{"type": "string", "description": "Short discussion topic"},
			"question":        map[string]interface{}{"type": "string", "description": "The concrete problem for other MaClaw experts"},
			"context_summary": map[string]interface{}{"type": "string", "description": "Minimal context summary; do not include sensitive/raw context unless policy allows"},
			"skills_wanted":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Desired expert skills"},
			"risk_level":      map[string]interface{}{"type": "string", "description": "low | medium | high"},
			"invitee_ids":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional explicit expert agent IDs. Empty means auto-select up to three eligible experts."},
			"role":            map[string]interface{}{"type": "string", "description": "observe | speak | review"},
			"trusted":         map[string]interface{}{"type": "boolean", "description": "Whether this invite is trusted by local policy"},
			"consultation_id": map[string]interface{}{"type": "string", "description": "Discussion/consultation ID"},
			"content":         map[string]interface{}{"type": "string", "description": "Discussion message content"},
			"summary":         map[string]interface{}{"type": "string", "description": "Final result summary"},
			"rationale":       map[string]interface{}{"type": "string", "description": "Reasoning/rationale summary"},
			"risks":           map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Known risks or caveats"},
			"state_action":    map[string]interface{}{"type": "string", "description": "pause | resume | cancel"},
			"submit":          map[string]interface{}{"type": "boolean", "description": "For summarize_result, submit the synthesized result to the Hub discussion"},
			"inject":          map[string]interface{}{"type": "boolean", "description": "For summarize_result, inject the synthesized result into the active AI assistant loop"},
			"force":           map[string]interface{}{"type": "boolean", "description": "For summarize_result, allow summarizing before the readiness gate is satisfied"},
			"dry_run":         map[string]interface{}{"type": "boolean", "description": "For cleanup_stale, list stale discussions without cancelling them"},
			"role_filter":     map[string]interface{}{"type": "string", "description": "Optional list_mine role filter"},
		},
		Source: "builtin:group_discussion",
		Handler: func(args map[string]interface{}) string {
			return dispatchGroupDiscussionTool(app, handler, args)
		},
	})
}

func dispatchGroupDiscussionTool(app *App, handler *IMMessageHandler, args map[string]interface{}) string {
	switch strings.TrimSpace(strings.ToLower(stringVal(args, "action"))) {
	case "status":
		return groupDiscussionJSON(app.GroupDiscussionStatus())
	case "list_experts":
		experts, err := app.GroupDiscussionListExperts()
		return groupDiscussionResult(map[string]interface{}{"experts": experts}, err)
	case "list_mine":
		discussions, err := app.GroupDiscussionListMine(stringVal(args, "role_filter"))
		return groupDiscussionResult(map[string]interface{}{"discussions": discussions}, err)
	case "get_discussion":
		discussion, err := app.GroupDiscussionGetConsultation(stringVal(args, "consultation_id"))
		return groupDiscussionResult(map[string]interface{}{"discussion": discussion}, err)
	case "get_detail":
		detail, err := app.GroupDiscussionGetConsultationDetail(stringVal(args, "consultation_id"))
		return groupDiscussionResult(map[string]interface{}{"discussion_detail": detail}, err)
	case "readiness":
		readiness, err := app.GroupDiscussionGetReadiness(stringVal(args, "consultation_id"))
		return groupDiscussionResult(map[string]interface{}{"readiness": readiness}, err)
	case "summarize_result":
		result, err := app.GroupDiscussionSummarizeResult(GroupDiscussionSummarizeRequest{ConsultationID: stringVal(args, "consultation_id"), Submit: groupDiscussionBool(args["submit"]), Inject: groupDiscussionBool(args["inject"]), Force: groupDiscussionBool(args["force"])})
		return groupDiscussionResult(map[string]interface{}{"summary": result}, err)
	case "cleanup_stale":
		result, err := app.GroupDiscussionCleanupStale(GroupDiscussionStaleCleanupRequest{DryRun: groupDiscussionBool(args["dry_run"])})
		return groupDiscussionResult(map[string]interface{}{"cleanup": result}, err)
	case "process_invites":
		invites, err := app.GroupDiscussionProcessPendingInvites()
		return groupDiscussionResult(map[string]interface{}{"pending_invites": invites}, err)
	case "suggest":
		return groupDiscussionSuggest(app, args)
	case "start_authorized":
		if err := groupDiscussionAuthorizeStartGate(app, handler, args); err != nil {
			return groupDiscussionResult(nil, err)
		}
		result, err := app.GroupDiscussionStartAuthorizedConsultation(GroupDiscussionAuthorizedStartRequest{
			Request:    groupDiscussionRequestFromArgs(app, args),
			InviteeIDs: groupDiscussionStringSlice(args["invitee_ids"]),
			Role:       a2a.GroupRole(strings.TrimSpace(stringVal(args, "role"))),
			Trusted:    groupDiscussionBool(args["trusted"]),
		})
		return groupDiscussionResult(result, err)
	case "send_message":
		consultationID := strings.TrimSpace(stringVal(args, "consultation_id"))
		msg := a2a.GroupDiscussionMessage{Kind: a2a.MessageStatement, Content: strings.TrimSpace(stringVal(args, "content")), CreatedAt: time.Now()}
		return groupDiscussionResult(map[string]interface{}{"sent": true, "consultation_id": consultationID}, app.GroupDiscussionSendMessage(consultationID, msg))
	case "submit_result":
		consultationID := strings.TrimSpace(stringVal(args, "consultation_id"))
		result := a2a.GroupDiscussionResult{Summary: strings.TrimSpace(stringVal(args, "summary")), Rationale: strings.TrimSpace(stringVal(args, "rationale")), Risks: groupDiscussionStringSlice(args["risks"]), CreatedAt: time.Now()}
		return groupDiscussionResult(map[string]interface{}{"submitted": true, "consultation_id": consultationID}, app.GroupDiscussionSubmitResult(consultationID, result))
	case "set_state":
		consultationID := strings.TrimSpace(stringVal(args, "consultation_id"))
		action := strings.TrimSpace(stringVal(args, "state_action"))
		return groupDiscussionResult(map[string]interface{}{"updated": true, "consultation_id": consultationID, "state_action": action}, app.GroupDiscussionSetState(consultationID, action))
	default:
		return "unsupported group_discussion action; use status, list_experts, list_mine, get_discussion, get_detail, readiness, summarize_result, cleanup_stale, process_invites, suggest, start_authorized, send_message, submit_result, or set_state"
	}
}

func groupDiscussionAuthorizeStartGate(app *App, handler *IMMessageHandler, args map[string]interface{}) error {
	cfg, err := app.LoadConfig()
	if err != nil {
		return err
	}
	if !cfg.GroupDiscussion.ConfirmBeforeStart {
		return nil
	}
	risk := strings.ToLower(strings.TrimSpace(stringVal(args, "risk_level")))
	if risk == "" {
		risk = "medium"
	}
	maxRisk := strings.ToLower(strings.TrimSpace(cfg.GroupDiscussion.MaxRiskLevel))
	if maxRisk == "" {
		maxRisk = "medium"
	}
	if groupDiscussionRiskRank(risk) > groupDiscussionRiskRank(maxRisk) {
		return fmt.Errorf("group discussion risk %q exceeds local max risk %q", risk, maxRisk)
	}
	if cfg.GroupDiscussion.AllowSecurityGroupFreeDiscussion && strings.TrimSpace(cfg.GroupDiscussion.SecurityGroupID) != "" && groupDiscussionRiskRank(risk) <= groupDiscussionRiskRank("medium") {
		return nil
	}
	decision, err := groupDiscussionClassifyHumanAuthorization(app, handler, args)
	if err != nil {
		return fmt.Errorf("group discussion authorization could not be verified: %w", err)
	}
	if decision.Decision == "approve" && decision.Confidence >= 0.7 {
		return nil
	}
	if decision.Reason != "" {
		return fmt.Errorf("group discussion start is not authorized: %s", decision.Reason)
	}
	return fmt.Errorf("group discussion start requires explicit human approval")
}

func groupDiscussionRiskRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low":
		return 1
	case "medium", "":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	default:
		return 2
	}
}

type groupDiscussionAuthorizationDecision struct {
	Decision   string  `json:"decision"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

func groupDiscussionClassifyHumanAuthorization(app *App, handler *IMMessageHandler, args map[string]interface{}) (groupDiscussionAuthorizationDecision, error) {
	if app == nil || handler == nil {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("assistant context is unavailable")
	}
	userText := strings.TrimSpace(handler.lastUserText)
	if userText == "" {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("latest user message is empty")
	}
	cfg := app.GetMaclawLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("MaClaw LLM is not configured")
	}
	payload := map[string]interface{}{
		"latest_user_message": userText,
		"discussion_request": map[string]interface{}{
			"topic":           strings.TrimSpace(stringVal(args, "topic")),
			"question":        strings.TrimSpace(stringVal(args, "question")),
			"context_summary": strings.TrimSpace(stringVal(args, "context_summary")),
			"risk_level":      strings.TrimSpace(stringVal(args, "risk_level")),
		},
	}
	payloadJSON, _ := json.Marshal(payload)
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": groupDiscussionAuthorizationPrompt},
		map[string]interface{}{"role": "user", "content": string(payloadJSON)},
	}
	client := handler.client
	if client == nil {
		client = &http.Client{Timeout: 12 * time.Second}
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Protocol), "anthropic") {
		resp, err := handler.doAnthropicLLMRequest(cfg, messages, nil, client)
		if err != nil {
			return groupDiscussionAuthorizationDecision{}, err
		}
		return decodeGroupDiscussionAuthorizationDecision(firstLLMResponseText(resp))
	}
	return requestGroupDiscussionAuthorizationOpenAI(handler, cfg, messages, client)
}

func requestGroupDiscussionAuthorizationOpenAI(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, messages []interface{}, client *http.Client) (groupDiscussionAuthorizationDecision, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	responseFormat := map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "group_discussion_authorization",
			"schema": groupDiscussionAuthorizationJSONSchema,
		},
	}
	req, body, endpoint, err := llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{
		Stream:         false,
		ResponseFormat: responseFormat,
	})
	if err != nil {
		return groupDiscussionAuthorizationDecision{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return groupDiscussionAuthorizationDecision{}, dumpLLMContext(resp.StatusCode, "group discussion authorization request failed", body, handler.getTempDir())
	}
	parsedResp, err := llm.ParseNonStreamOpenAIResponse(resp)
	if err != nil {
		return groupDiscussionAuthorizationDecision{}, err
	}
	return decodeGroupDiscussionAuthorizationDecision(firstLLMResponseText(parsedResp))
}

func decodeGroupDiscussionAuthorizationDecision(content string) (groupDiscussionAuthorizationDecision, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	if content == "" {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("empty authorization response")
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}
	var parsed groupDiscussionAuthorizationDecision
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return groupDiscussionAuthorizationDecision{}, err
	}
	parsed.Decision = strings.ToLower(strings.TrimSpace(parsed.Decision))
	if parsed.Decision != "approve" && parsed.Decision != "reject" && parsed.Decision != "unclear" {
		return groupDiscussionAuthorizationDecision{}, fmt.Errorf("unknown authorization decision %q", parsed.Decision)
	}
	if parsed.Confidence < 0 {
		parsed.Confidence = 0
	}
	if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}
	parsed.Reason = strings.TrimSpace(parsed.Reason)
	return parsed, nil
}

var groupDiscussionAuthorizationJSONSchema = map[string]interface{}{
	"type":                 "object",
	"additionalProperties": false,
	"properties": map[string]interface{}{
		"decision": map[string]interface{}{
			"type": "string",
			"enum": []string{"approve", "reject", "unclear"},
		},
		"confidence": map[string]interface{}{
			"type":    "number",
			"minimum": 0,
			"maximum": 1,
		},
		"reason": map[string]interface{}{"type": "string"},
	},
	"required": []string{"decision", "confidence", "reason"},
}

const groupDiscussionAuthorizationPrompt = `You are a strict authorization classifier for MaClaw current-Hub group discussion.
Return only JSON matching the schema.
Task: decide whether the latest human message explicitly authorizes starting the proposed MaClaw group discussion now.
Labels:
- approve: the user clearly grants permission to start/invite/allow this group discussion now.
- reject: the user clearly refuses, cancels, forbids, or asks not to start it.
- unclear: anything else, including continuing development, discussing design, ambiguous agreement, or talking about inviting other MaClaws without clearly authorizing this start.
Rules:
- Do not rely on keywords alone; judge intent in context.
- The user may use Chinese, English, or mixed language.
- If there is doubt, choose unclear.
- Same-Hub/security policy is handled outside this classifier; classify only human authorization intent.`

func groupDiscussionRequestFromArgs(app *App, args map[string]interface{}) a2a.GroupConsultationRequest {
	cfg, _ := app.LoadConfig()
	return a2a.GroupConsultationRequest{
		FromID:         cfg.RemoteMachineID,
		Topic:          strings.TrimSpace(stringVal(args, "topic")),
		Question:       strings.TrimSpace(stringVal(args, "question")),
		ContextSummary: strings.TrimSpace(stringVal(args, "context_summary")),
		SkillsWanted:   groupDiscussionStringSlice(args["skills_wanted"]),
		RiskLevel:      strings.TrimSpace(stringVal(args, "risk_level")),
		MaxRounds:      groupDiscussionInt(args["max_rounds"]),
		TimeoutSeconds: groupDiscussionInt(args["timeout_seconds"]),
		CreatedAt:      time.Now(),
	}
}

func groupDiscussionSuggest(app *App, args map[string]interface{}) string {
	status := app.GroupDiscussionStatus()
	payload := map[string]interface{}{
		"enabled":          status.Enabled,
		"discoverable":     status.Discoverable,
		"current_hub_only": true,
		"topic":            strings.TrimSpace(stringVal(args, "topic")),
		"question":         strings.TrimSpace(stringVal(args, "question")),
		"context_summary":  strings.TrimSpace(stringVal(args, "context_summary")),
		"skills_wanted":    groupDiscussionStringSlice(args["skills_wanted"]),
		"instruction":      "Ask the human for explicit permission before calling group_discussion(action=start_authorized). If permission is granted, use only the provided summary/minimal context and keep the discussion on the current Hub.",
	}
	if status.Error != "" {
		payload["error"] = status.Error
	}
	return groupDiscussionJSON(payload)
}

func groupDiscussionResult(value interface{}, err error) string {
	if err != nil {
		return groupDiscussionJSON(map[string]interface{}{"ok": false, "error": err.Error()})
	}
	return groupDiscussionJSON(map[string]interface{}{"ok": true, "result": value})
}

func groupDiscussionJSON(value interface{}) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("group discussion result encode failed: %v", err)
	}
	return string(data)
}

func groupDiscussionStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
		return out
	default:
		return nil
	}
}

func groupDiscussionBool(value interface{}) bool {
	b, _ := value.(bool)
	return b
}

func groupDiscussionInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}
