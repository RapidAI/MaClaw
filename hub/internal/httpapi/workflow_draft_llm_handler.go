package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/condeval"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

type workflowDraftGenerateRequest struct {
	Description string `json:"description"`
	Language    string `json:"language"`
}

const workflowDraftDescriptionMaxBytes = 4000

const (
	workflowDraftFallbackReasonSettings = "llm_settings"
	workflowDraftFallbackReasonRoute    = "llm_route"
	workflowDraftFallbackReasonProvider = "llm_provider"
	workflowDraftFallbackReasonResponse = "llm_response"
)

const workflowDraftDefaultApproverRoleID = "role:dynamic:applicant_department:direct_manager"

var workflowDraftDebugSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*bearer\s+)[^\s,"'}]+`),
	regexp.MustCompile(`(?i)((?:"|')?[\w-]*(?:api[_-]?key|access[_-]?token|accessToken|refresh[_-]?token|refreshToken|secret|password)[\w-]*(?:"|')?\s*[:=]\s*(?:"|')?)[^"',\s}]+`),
}

type workflowDraftLLMDiagnosticError struct {
	Err             error
	ServiceGroupID  string
	Model           string
	ProviderID      string
	ServiceGroupIDs []string
	StatusCode      int
	ResponseSnippet string
}

func (e *workflowDraftLLMDiagnosticError) Error() string {
	if e == nil || e.Err == nil {
		return "LLM draft generation failed"
	}
	return e.Err.Error()
}

func (e *workflowDraftLLMDiagnosticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func WorkflowDraftLLMHandler(identity veMachineAuthenticator, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	_ = securitySvc
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, workflowDraftDescriptionMaxBytes*2)
		var req workflowDraftGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "workflow draft request is too large")
				return
			}
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON")
			return
		}
		description, ok := normalizeWorkflowDraftDescription(req.Description)
		if !ok {
			writeError(w, http.StatusBadRequest, "DESCRIPTION_REQUIRED", "description is required")
			return
		}
		r = requestWithWorkflowDraftTenant(r, principal.TenantID)
		tenantSystem := veSystemSettingsForMachine(system, principal)
		if draft, err := generateWorkflowDraftWithLLM(r, tenantSystem, description, req.Language); err == nil {
			draft["generated_by"] = "llm"
			writeJSON(w, http.StatusOK, draft)
			return
		} else {
			draft := buildFallbackWorkflowDraft(description, req.Language)
			draft["generated_by"] = "fallback"
			draft["fallback_reason"] = workflowDraftFallbackReason(err)
			if debug := workflowDraftFallbackDebug(err); len(debug) > 0 {
				draft["debug"] = debug
			}
			draft["notes"] = []string{"LLM draft generation was unavailable, so a basic fallback draft was generated."}
			writeJSON(w, http.StatusOK, draft)
			return
		}
	}
}

func workflowDraftFallbackDebug(err error) map[string]any {
	if err == nil {
		return nil
	}
	debug := map[string]any{
		"message": workflowDraftDebugMessage(err),
	}
	var diag *workflowDraftLLMDiagnosticError
	if errors.As(err, &diag) && diag != nil {
		if diag.ServiceGroupID != "" {
			debug["service_group_id"] = diag.ServiceGroupID
		}
		if diag.Model != "" {
			debug["model"] = diag.Model
		}
		if diag.ProviderID != "" {
			debug["provider_id"] = diag.ProviderID
		}
		if len(diag.ServiceGroupIDs) > 0 {
			debug["provider_service_group_ids"] = append([]string(nil), diag.ServiceGroupIDs...)
		}
		if diag.StatusCode > 0 {
			debug["status_code"] = diag.StatusCode
		}
		if diag.ResponseSnippet != "" {
			debug["response"] = diag.ResponseSnippet
		}
	}
	return debug
}

func workflowDraftDebugMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "LLM draft generation failed"
	}
	return workflowDraftSanitizeDebugText(msg, 600)
}

func workflowDraftFallbackReason(err error) string {
	if err == nil {
		return workflowDraftFallbackReasonProvider
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "settings are not configured"),
		strings.Contains(msg, "system default llm service group"):
		return workflowDraftFallbackReasonSettings
	case strings.Contains(msg, "no active model service entitlement"),
		strings.Contains(msg, "not authorized for this account"):
		return workflowDraftFallbackReasonRoute
	case strings.Contains(msg, "empty llm response"),
		strings.Contains(msg, "llm response"),
		strings.Contains(msg, "invalid workflow graph"),
		strings.Contains(msg, "invalid json"):
		return workflowDraftFallbackReasonResponse
	default:
		return workflowDraftFallbackReasonProvider
	}
}

func requestWithWorkflowDraftTenant(r *http.Request, tenantID string) *http.Request {
	if r == nil {
		return r
	}
	ctx := store.WithTenant(r.Context(), tenantID)
	ctx = security.WithTenant(ctx, tenantID)
	ctx = WithRequestTenant(ctx, tenantID)
	return r.WithContext(ctx)
}

func normalizeWorkflowDraftDescription(value string) (string, bool) {
	description := strings.TrimSpace(value)
	if description == "" {
		return "", false
	}
	if len(description) > workflowDraftDescriptionMaxBytes {
		var clipped strings.Builder
		clipped.Grow(workflowDraftDescriptionMaxBytes)
		for _, r := range description {
			runeLen := utf8.RuneLen(r)
			if runeLen < 0 {
				runeLen = len("\ufffd")
			}
			if clipped.Len()+runeLen > workflowDraftDescriptionMaxBytes {
				break
			}
			clipped.WriteRune(r)
		}
		description = clipped.String()
	}
	return description, true
}

func generateWorkflowDraftWithLLM(r *http.Request, system store.SystemSettingsRepository, description, language string) (map[string]any, error) {
	if r == nil || system == nil {
		return nil, fmt.Errorf("LLM settings are not configured")
	}
	providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
	if err != nil {
		return nil, err
	}
	serviceReg, err := llmservice.LoadRegistry(r.Context(), system)
	if err != nil {
		return nil, err
	}
	// Server-side LLM always uses the reserved free system group.
	if llmservice.EnsureSystemFreeServiceGroup(serviceReg) {
		if saveErr := llmservice.SaveRegistry(r.Context(), system, serviceReg); saveErr != nil {
			log.Printf("[workflow-draft] ensure system-free save failed: %v", saveErr)
		}
	}
	serviceGroupID := serviceReg.SystemDefaultServiceGroupIDOrFree()
	if serviceReg.FindModelServiceGroup(serviceGroupID) == nil {
		return nil, fmt.Errorf("system free LLM service group %q was not found", serviceGroupID)
	}
	models, _ := llmservice.BuildAuthorizedModelsForServiceGroups(serviceReg, []string{serviceGroupID})
	models = filterAuthorizedModelsForConfiguredProviders(models, providerReg)
	body := workflowDraftLLMRequestBody(description, language)
	model, externalModel, err := resolveAuthorizedModel(body, models)
	if err != nil {
		return nil, err
	}
	respBody, statusCode, providerID, providerServiceGroupIDs, err := forwardAuthorizedModelRequest(r, providerReg, model, body, externalModel)
	if err != nil {
		diag := workflowDraftLLMDiagnosticError{
			Err:             err,
			ServiceGroupID:  serviceGroupID,
			Model:           model.Name,
			ProviderID:      providerID,
			ServiceGroupIDs: providerServiceGroupIDs,
			StatusCode:      statusCode,
			ResponseSnippet: workflowDraftProviderResponseSnippet(respBody),
		}
		log.Printf("[workflow-draft] LLM provider request failed service_group=%q model=%q provider=%q status=%d err=%v response=%q", diag.ServiceGroupID, diag.Model, diag.ProviderID, diag.StatusCode, err, diag.ResponseSnippet)
		return nil, &diag
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		diag := workflowDraftLLMDiagnosticError{
			Err:             fmt.Errorf("LLM provider returned HTTP %d", statusCode),
			ServiceGroupID:  serviceGroupID,
			Model:           model.Name,
			ProviderID:      providerID,
			ServiceGroupIDs: providerServiceGroupIDs,
			StatusCode:      statusCode,
			ResponseSnippet: workflowDraftProviderResponseSnippet(respBody),
		}
		log.Printf("[workflow-draft] LLM provider returned non-success service_group=%q model=%q provider=%q status=%d response=%q", diag.ServiceGroupID, diag.Model, diag.ProviderID, diag.StatusCode, diag.ResponseSnippet)
		return nil, &diag
	}
	content, err := workflowDraftLLMResponseText(respBody)
	if err != nil {
		return nil, err
	}
	draft, err := parseWorkflowDraftLLMResponse(content)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(fmt.Sprint(draft["description"])) == "" {
		draft["description"] = description
	}
	return draft, nil
}

func workflowDraftProviderResponseSnippet(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		if msg := workflowDraftProviderErrorMessage(payload); msg != "" {
			return workflowDraftSanitizeDebugText(msg, 800)
		}
	}
	return workflowDraftSanitizeDebugText(string(body), 800)
}

func workflowDraftProviderErrorMessage(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if errValue, ok := payload["error"]; ok {
		switch errPayload := errValue.(type) {
		case string:
			return errPayload
		case map[string]any:
			for _, key := range []string{"message", "error", "code", "type"} {
				if text := workflowDraftString(errPayload[key]); text != "" {
					return text
				}
			}
		}
	}
	for _, key := range []string{"message", "detail", "code"} {
		if text := workflowDraftString(payload[key]); text != "" {
			return text
		}
	}
	return ""
}

func workflowDraftSanitizeDebugText(text string, maxLen int) string {
	text = strings.ToValidUTF8(text, "\ufffd")
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	text = workflowDraftRedactDebugSecrets(text)
	if maxLen <= 0 || utf8.RuneCountInString(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return string([]rune(text)[:maxLen])
	}
	return string([]rune(text)[:maxLen-3]) + "..."
}

func workflowDraftRedactDebugSecrets(text string) string {
	for _, pattern := range workflowDraftDebugSecretPatterns {
		text = pattern.ReplaceAllString(text, "${1}[redacted]")
	}
	return text
}

func workflowDraftLLMRequestBody(description, language string) map[string]any {
	return map[string]any{
		"model":       "auto",
		"temperature": 0.2,
		"stream":      false,
		"messages": []map[string]string{
			{"role": "system", "content": workflowDraftLLMSystemPrompt(language)},
			{"role": "user", "content": description},
		},
	}
}

func workflowDraftLLMSystemPrompt(language string) string {
	langHint := "Use the same language as the user's description for node labels."
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "zh") {
		langHint = "\u8282\u70b9 label \u548c\u8bf4\u660e\u4f18\u5148\u4f7f\u7528\u4e2d\u6587\u3002"
	}
	return strings.Join([]string{
		"You generate approval workflow drafts for a visual workflow editor.",
		"Return JSON only. Do not wrap it in Markdown.",
		langHint,
		"The JSON shape must be: {\"name\":\"...\",\"description\":\"...\",\"graph\":{\"nodes\":[...],\"edges\":[...]},\"notes\":[\"...\"]}.",
		"Allowed node types: trigger, form, approval, condition_branch, action, notification, sub_process, terminal.",
		"Each node must include id, type, label, position {x,y}, and config.",
		"Each edge must include id, source_id, target_id.",
		"Return exactly one trigger node. The trigger node must be the entry point and must not have incoming edges.",
		"Generate nodes dynamically from the user's language. Do not use a fixed template.",
		"If the user describes conditions such as if/when/otherwise/\u8d85\u8fc7/\u5927\u4e8e/\u5c0f\u4e8e/\u5426\u5219, create a condition_branch node with branches and default_branch, then connect separate branch target nodes.",
		"Each condition branch must include target_node_id, priority, and expression {field, operator, value}. You may also include a short label for the editor.",
		"Each approval node must include config.approver_ids. Use " + workflowDraftDefaultApproverRoleID + " when the user does not name a specific approver.",
		"Do not collapse conditional approvers into a single approval node. For example, 'if leave is longer than 3 days, HR approves' requires a condition_branch and a separate HR approval node.",
		"Prefer a readable left-to-right layout: x increases by about 220 for each step; branch nodes may use different y values.",
	}, "\n")
}

func workflowDraftLLMResponseText(body []byte) (string, error) {
	if resp, err := llm.ParseNonStreamOpenAIResponseBody(body); err == nil && resp != nil && len(resp.Choices) > 0 {
		text := firstNonEmptyString(resp.Choices[0].Message.Content, resp.Choices[0].Message.ReasoningContent)
		if strings.TrimSpace(text) != "" {
			return text, nil
		}
	}
	if resp, err := llm.ParseNonStreamResponsesAPIBody(body); err == nil && resp != nil && len(resp.Choices) > 0 {
		text := firstNonEmptyString(resp.Choices[0].Message.Content, resp.Choices[0].Message.ReasoningContent)
		if strings.TrimSpace(text) != "" {
			return text, nil
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if text := workflowDraftTextContent(payload); text != "" {
		return text, nil
	}
	return "", fmt.Errorf("LLM response did not contain text content")
}

func workflowDraftTextContent(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := workflowDraftTextContent(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case map[string]any:
		for _, key := range []string{"text", "output_text", "content", "message", "output", "choices"} {
			if text := workflowDraftTextContent(v[key]); text != "" {
				return text
			}
		}
		return ""
	default:
		return ""
	}
}

func parseWorkflowDraftLLMResponse(content string) (map[string]any, error) {
	payload, err := workflowDraftJSONPayload(content)
	if err != nil {
		return nil, err
	}
	var draft map[string]any
	if err := json.Unmarshal([]byte(payload), &draft); err != nil {
		return nil, err
	}
	graph, ok := draft["graph"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("LLM response missing graph")
	}
	normalizedGraph, err := normalizeWorkflowDraftGraph(graph)
	if err != nil {
		return nil, err
	}
	if err := validateNormalizedWorkflowDraftGraph(normalizedGraph); err != nil {
		return nil, err
	}
	if workflowDraftString(draft["name"]) == "" {
		draft["name"] = "Approval workflow draft"
	}
	if _, ok := draft["notes"].([]any); !ok {
		draft["notes"] = []any{}
	}
	draft["graph"] = normalizedGraph
	return draft, nil
}

func workflowDraftJSONPayload(content string) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("empty LLM response")
	}
	if strings.HasPrefix(content, "```") {
		if end := strings.LastIndex(content, "```"); end > 0 {
			fenced := strings.TrimSpace(content[3:end])
			if newline := strings.IndexAny(fenced, "\r\n"); newline >= 0 {
				lang := strings.TrimSpace(fenced[:newline])
				body := strings.TrimSpace(fenced[newline:])
				if lang == "" || strings.EqualFold(lang, "json") || strings.EqualFold(lang, "javascript") {
					content = body
				}
			} else {
				content = fenced
			}
		}
	}
	if json.Valid([]byte(content)) {
		return content, nil
	}
	start := strings.Index(content, "{")
	if start < 0 {
		return "", fmt.Errorf("LLM response did not contain a JSON object")
	}
	end := matchingWorkflowDraftJSONObjectEnd(content, start)
	if end < 0 {
		return "", fmt.Errorf("LLM response JSON object was incomplete")
	}
	payload := strings.TrimSpace(content[start : end+1])
	if !json.Valid([]byte(payload)) {
		return "", fmt.Errorf("LLM response JSON object was invalid")
	}
	return payload, nil
}

func matchingWorkflowDraftJSONObjectEnd(content string, start int) int {
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(content); i++ {
		ch := content[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func validateNormalizedWorkflowDraftGraph(graph map[string]any) error {
	body, err := json.Marshal(graph)
	if err != nil {
		return err
	}
	var wfGraph workflow.WorkflowGraph
	if err := json.Unmarshal(body, &wfGraph); err != nil {
		return err
	}
	if err := workflow.ValidateGraphStructure(wfGraph); err != nil {
		return err
	}
	nodeTypes := make(map[string]workflow.NodeType, len(wfGraph.Nodes))
	for _, node := range wfGraph.Nodes {
		nodeTypes[node.ID] = node.Type
	}
	for _, edge := range wfGraph.Edges {
		if nodeTypes[edge.SourceID] == workflow.NodeTypeTerminal {
			return fmt.Errorf("terminal node %s must not have outgoing edges", edge.SourceID)
		}
	}
	for _, node := range wfGraph.Nodes {
		if node.Type != workflow.NodeConditionBranch {
			continue
		}
		var config workflow.ConditionBranchConfig
		if err := json.Unmarshal(node.Config, &config); err != nil {
			return fmt.Errorf("parse condition branch config for %s: %w", node.ID, err)
		}
		hasRoute := false
		for _, branch := range config.Branches {
			if branch.TargetNodeID == "" {
				continue
			}
			if _, ok := nodeTypes[branch.TargetNodeID]; !ok {
				return fmt.Errorf("condition branch %s targets missing node %s", node.ID, branch.TargetNodeID)
			}
			if !isWorkflowDraftConditionOperator(branch.Expression.Operator) || strings.TrimSpace(branch.Expression.Field) == "" {
				return fmt.Errorf("condition branch %s has invalid expression", node.ID)
			}
			hasRoute = true
		}
		if config.DefaultBranch != "" {
			if _, ok := nodeTypes[config.DefaultBranch]; !ok {
				return fmt.Errorf("condition branch %s default targets missing node %s", node.ID, config.DefaultBranch)
			}
			hasRoute = true
		}
		if !hasRoute {
			return fmt.Errorf("condition branch %s has no route targets", node.ID)
		}
	}
	return nil
}

func normalizeWorkflowDraftGraph(graph map[string]any) (map[string]any, error) {
	rawNodes, ok := graph["nodes"].([]any)
	if !ok || len(rawNodes) == 0 {
		return nil, fmt.Errorf("LLM response missing graph nodes")
	}
	rawEdges, ok := graph["edges"].([]any)
	if !ok {
		return nil, fmt.Errorf("LLM response missing graph edges")
	}
	nodes := make([]map[string]any, 0, len(rawNodes))
	idMap := map[string]string{}
	usedNodeIDs := map[string]struct{}{}
	hasTrigger := false
	for _, raw := range rawNodes {
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		nodeType := workflowDraftString(node["type"])
		if !isAllowedWorkflowDraftNodeType(nodeType) {
			continue
		}
		if nodeType == "trigger" {
			if hasTrigger {
				nodeType = "action"
			} else {
				hasTrigger = true
			}
		}
		oldID := workflowDraftString(node["id"])
		nodeID := uniqueWorkflowDraftID(firstNonEmptyString(oldID, fmt.Sprintf("node_%d", len(nodes)+1)), usedNodeIDs)
		usedNodeIDs[nodeID] = struct{}{}
		if oldID != "" {
			if _, exists := idMap[oldID]; exists {
				oldID = ""
			}
		}
		if oldID != "" {
			idMap[oldID] = nodeID
		}
		nodes = append(nodes, map[string]any{
			"id":       nodeID,
			"type":     nodeType,
			"label":    firstNonEmptyString(workflowDraftString(node["label"]), workflowDraftDefaultNodeLabel(nodeType)),
			"position": normalizeWorkflowDraftPosition(node["position"], len(nodes)),
			"config":   normalizeWorkflowDraftNodeConfig(nodeType, node["config"]),
		})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("LLM response has no usable graph nodes")
	}
	if !hasTrigger {
		nodes[0]["type"] = "trigger"
		nodes[0]["label"] = firstNonEmptyString(workflowDraftString(nodes[0]["label"]), workflowDraftDefaultNodeLabel("trigger"))
		nodes[0]["config"] = normalizeWorkflowDraftNodeConfig("trigger", nodes[0]["config"])
	}
	nodeIDs := map[string]struct{}{}
	nodeTypes := map[string]string{}
	triggerID := ""
	for _, node := range nodes {
		nodeID := workflowDraftString(node["id"])
		nodeIDs[nodeID] = struct{}{}
		nodeTypes[nodeID] = workflowDraftString(node["type"])
		if node["type"] == "trigger" && triggerID == "" {
			triggerID = nodeID
		}
	}
	for _, node := range nodes {
		if node["type"] == "condition_branch" {
			node["config"] = normalizeWorkflowDraftConditionTargets(node["config"], idMap, nodeIDs)
		}
	}
	edges := normalizeWorkflowDraftEdges(rawEdges, idMap, nodeIDs, nodeTypes, triggerID)
	if len(edges) == 0 && len(nodes) > 1 {
		for i := 0; i < len(nodes)-1; i++ {
			if nodes[i+1]["id"] == triggerID {
				continue
			}
			if workflowDraftString(nodes[i]["type"]) == "terminal" {
				continue
			}
			edges = append(edges, map[string]any{
				"id":        fmt.Sprintf("edge_%d", i+1),
				"source_id": nodes[i]["id"],
				"target_id": nodes[i+1]["id"],
				"label":     "",
				"priority":  0,
			})
		}
	}
	edges = ensureWorkflowDraftReachability(nodes, edges, triggerID)
	normalizeWorkflowDraftConditionRoutes(nodes, edges, nodeIDs)
	return map[string]any{"nodes": nodes, "edges": edges}, nil
}

func normalizeWorkflowDraftEdges(rawEdges []any, idMap map[string]string, nodeIDs map[string]struct{}, nodeTypes map[string]string, triggerID string) []map[string]any {
	edges := make([]map[string]any, 0, len(rawEdges))
	usedEdgeIDs := map[string]struct{}{}
	usedEdgePairs := map[string]struct{}{}
	for _, raw := range rawEdges {
		edge, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		sourceID := remapWorkflowDraftNodeID(workflowDraftString(edge["source_id"]), idMap)
		targetID := remapWorkflowDraftNodeID(workflowDraftString(edge["target_id"]), idMap)
		if sourceID == "" || targetID == "" || sourceID == targetID {
			continue
		}
		if nodeTypes[sourceID] == "terminal" {
			continue
		}
		if targetID == triggerID {
			continue
		}
		if _, ok := nodeIDs[sourceID]; !ok {
			continue
		}
		if _, ok := nodeIDs[targetID]; !ok {
			continue
		}
		pair := sourceID + "->" + targetID
		if _, ok := usedEdgePairs[pair]; ok {
			continue
		}
		usedEdgePairs[pair] = struct{}{}
		edgeID := uniqueWorkflowDraftID(firstNonEmptyString(workflowDraftString(edge["id"]), fmt.Sprintf("edge_%d", len(edges)+1)), usedEdgeIDs)
		usedEdgeIDs[edgeID] = struct{}{}
		edges = append(edges, map[string]any{
			"id":        edgeID,
			"source_id": sourceID,
			"target_id": targetID,
			"label":     workflowDraftString(edge["label"]),
			"priority":  workflowDraftNumberOrDefault(edge["priority"], 0),
		})
	}
	return edges
}

func ensureWorkflowDraftReachability(nodes []map[string]any, edges []map[string]any, triggerID string) []map[string]any {
	if triggerID == "" || len(nodes) <= 1 {
		return edges
	}
	reachable := workflowDraftReachableIDs(triggerID, edges)
	usedEdgeIDs := map[string]struct{}{}
	usedEdgePairs := map[string]struct{}{}
	for _, edge := range edges {
		usedEdgeIDs[workflowDraftString(edge["id"])] = struct{}{}
		usedEdgePairs[workflowDraftString(edge["source_id"])+"->"+workflowDraftString(edge["target_id"])] = struct{}{}
	}
	lastRoutable := triggerID
	for _, node := range nodes {
		nodeID := workflowDraftString(node["id"])
		if nodeID == "" || nodeID == triggerID {
			continue
		}
		if reachable[nodeID] {
			if workflowDraftString(node["type"]) != "terminal" {
				lastRoutable = nodeID
			}
			continue
		}
		pair := lastRoutable + "->" + nodeID
		if _, exists := usedEdgePairs[pair]; !exists {
			edgeID := uniqueWorkflowDraftID(fmt.Sprintf("edge_%d", len(edges)+1), usedEdgeIDs)
			usedEdgeIDs[edgeID] = struct{}{}
			usedEdgePairs[pair] = struct{}{}
			edges = append(edges, map[string]any{
				"id":        edgeID,
				"source_id": lastRoutable,
				"target_id": nodeID,
				"label":     "",
				"priority":  0,
			})
		}
		reachable = workflowDraftReachableIDs(triggerID, edges)
		if workflowDraftString(node["type"]) != "terminal" {
			lastRoutable = nodeID
		}
	}
	return edges
}

func workflowDraftReachableIDs(triggerID string, edges []map[string]any) map[string]bool {
	reachable := map[string]bool{triggerID: true}
	queue := []string{triggerID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range edges {
			if workflowDraftString(edge["source_id"]) != current {
				continue
			}
			targetID := workflowDraftString(edge["target_id"])
			if targetID == "" || reachable[targetID] {
				continue
			}
			reachable[targetID] = true
			queue = append(queue, targetID)
		}
	}
	return reachable
}

func normalizeWorkflowDraftConditionRoutes(nodes []map[string]any, edges []map[string]any, nodeIDs map[string]struct{}) {
	outgoing := map[string][]string{}
	for _, edge := range edges {
		sourceID := workflowDraftString(edge["source_id"])
		targetID := workflowDraftString(edge["target_id"])
		if sourceID != "" && targetID != "" {
			outgoing[sourceID] = append(outgoing[sourceID], targetID)
		}
	}
	for _, node := range nodes {
		if workflowDraftString(node["type"]) != "condition_branch" {
			continue
		}
		nodeID := workflowDraftString(node["id"])
		config, _ := node["config"].(map[string]any)
		if config == nil {
			config = workflowDraftDefaultConfig("condition_branch")
		}
		targets := outgoing[nodeID]
		rawBranches, _ := config["branches"].([]any)
		branches := make([]any, 0, len(rawBranches)+1)
		usedTargets := map[string]struct{}{}
		for _, raw := range rawBranches {
			branch, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			next := map[string]any{}
			for key, val := range branch {
				next[key] = val
			}
			targetID := workflowDraftString(next["target_node_id"])
			if _, ok := nodeIDs[targetID]; !ok || targetID == "" {
				targetID = firstUnusedWorkflowDraftTarget(targets, usedTargets)
			}
			next["target_node_id"] = targetID
			if targetID != "" {
				usedTargets[targetID] = struct{}{}
			}
			if _, ok := next["priority"]; !ok {
				next["priority"] = len(branches)
			}
			next["expression"] = normalizeWorkflowDraftConditionExpression(next["expression"], next["condition"], next["label"])
			branches = append(branches, next)
		}
		if len(branches) == 0 && len(targets) > 0 {
			targetID := targets[0]
			usedTargets[targetID] = struct{}{}
			branches = append(branches, map[string]any{
				"label":          "Condition is met",
				"target_node_id": targetID,
				"priority":       0,
				"expression": map[string]any{
					"field":    "value",
					"operator": condeval.OpEquals,
					"value":    true,
				},
			})
		}
		defaultBranch := workflowDraftString(config["default_branch"])
		if _, ok := nodeIDs[defaultBranch]; !ok || defaultBranch == "" {
			defaultBranch = firstUnusedWorkflowDraftTarget(targets, usedTargets)
			if defaultBranch == "" && len(targets) > 0 {
				defaultBranch = targets[len(targets)-1]
			}
		}
		config["branches"] = branches
		config["default_branch"] = defaultBranch
		node["config"] = config
	}
}

func firstUnusedWorkflowDraftTarget(targets []string, used map[string]struct{}) string {
	for _, targetID := range targets {
		if targetID == "" {
			continue
		}
		if _, ok := used[targetID]; ok {
			continue
		}
		return targetID
	}
	return ""
}

func isAllowedWorkflowDraftNodeType(nodeType string) bool {
	switch nodeType {
	case "trigger", "form", "approval", "condition_branch", "action", "notification", "sub_process", "terminal":
		return true
	default:
		return false
	}
}

func workflowDraftString(value any) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func uniqueWorkflowDraftID(candidate string, used map[string]struct{}) string {
	candidate = sanitizeWorkflowDraftID(candidate)
	if candidate == "" {
		candidate = "id"
	}
	if _, ok := used[candidate]; !ok {
		return candidate
	}
	for i := 2; ; i++ {
		next := fmt.Sprintf("%s_%d", candidate, i)
		if _, ok := used[next]; !ok {
			return next
		}
	}
}

func sanitizeWorkflowDraftID(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range candidate {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if allowed {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func remapWorkflowDraftNodeID(id string, idMap map[string]string) string {
	if next := idMap[id]; next != "" {
		return next
	}
	return id
}

func normalizeWorkflowDraftPosition(value any, index int) map[string]any {
	position, _ := value.(map[string]any)
	x := workflowDraftNumberOrDefault(position["x"], 80+index*220)
	y := workflowDraftNumberOrDefault(position["y"], 80)
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return map[string]any{"x": x, "y": y}
}

func workflowDraftNumberOrDefault(value any, fallback int) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case float32:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	case string:
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}

func workflowDraftDefaultNodeLabel(nodeType string) string {
	switch nodeType {
	case "trigger":
		return "Start"
	case "form":
		return "Submit request"
	case "approval":
		return "Approval"
	case "condition_branch":
		return "Condition"
	case "action":
		return "Action"
	case "notification":
		return "Notification"
	case "sub_process":
		return "Sub-process"
	case "terminal":
		return "Complete"
	default:
		return "Node"
	}
}

func normalizeWorkflowDraftNodeConfig(nodeType string, value any) map[string]any {
	config, _ := value.(map[string]any)
	out := workflowDraftDefaultConfig(nodeType)
	for key, val := range config {
		out[key] = val
	}
	if nodeType == "approval" {
		out = normalizeWorkflowDraftApprovalConfig(out)
	}
	return out
}

func normalizeWorkflowDraftApprovalConfig(config map[string]any) map[string]any {
	if config == nil {
		config = workflowDraftDefaultConfig("approval")
	}
	if !workflowDraftHasNonEmptyString(config["approver_ids"]) {
		config["approver_ids"] = []any{workflowDraftDefaultApproverRoleID}
	}
	if strings.EqualFold(workflowDraftString(config["mode"]), "sequential") && !workflowDraftHasNonEmptyString(config["approver_order"]) {
		config["approver_order"] = []any{workflowDraftDefaultApproverRoleID}
	}
	return config
}

func workflowDraftHasNonEmptyString(value any) bool {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if workflowDraftString(item) != "" {
				return true
			}
		}
	case []string:
		for _, item := range v {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
	case string:
		return strings.TrimSpace(v) != ""
	}
	return false
}

func workflowDraftDefaultConfig(nodeType string) map[string]any {
	switch nodeType {
	case "trigger":
		return map[string]any{"trigger_type": "manual", "description": ""}
	case "form":
		return map[string]any{"fields": []any{}, "description": ""}
	case "approval":
		return map[string]any{"approver_ids": []any{}, "mode": "single", "min_approvals": 1, "approver_order": []any{}, "timeout_hours": 24, "fallback_approver": ""}
	case "condition_branch":
		return map[string]any{"branches": []any{}, "default_branch": ""}
	case "action":
		return map[string]any{"action_type": "", "parameters": map[string]any{}}
	case "notification":
		return map[string]any{"recipients": []any{}, "message_template": ""}
	case "sub_process":
		return map[string]any{"workflow_id": "", "input_mapping": map[string]any{}}
	case "terminal":
		return map[string]any{"result_executors": []any{}, "notifiers": []any{}}
	default:
		return map[string]any{}
	}
}

func normalizeWorkflowDraftConditionTargets(value any, idMap map[string]string, nodeIDs map[string]struct{}) map[string]any {
	config, _ := value.(map[string]any)
	if config == nil {
		config = workflowDraftDefaultConfig("condition_branch")
	}
	defaultBranch := remapWorkflowDraftNodeID(workflowDraftString(config["default_branch"]), idMap)
	if _, ok := nodeIDs[defaultBranch]; !ok {
		defaultBranch = ""
	}
	config["default_branch"] = defaultBranch
	rawBranches, _ := config["branches"].([]any)
	branches := make([]any, 0, len(rawBranches))
	for _, raw := range rawBranches {
		branch, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		targetID := remapWorkflowDraftNodeID(workflowDraftString(branch["target_node_id"]), idMap)
		if _, ok := nodeIDs[targetID]; !ok {
			targetID = ""
		}
		next := map[string]any{}
		for key, val := range branch {
			next[key] = val
		}
		next["target_node_id"] = targetID
		if _, ok := next["priority"]; !ok {
			next["priority"] = len(branches)
		}
		next["expression"] = normalizeWorkflowDraftConditionExpression(next["expression"], next["condition"], next["label"])
		branches = append(branches, next)
	}
	config["branches"] = branches
	return config
}

func normalizeWorkflowDraftConditionExpression(expression, condition, label any) map[string]any {
	if expr, ok := expression.(map[string]any); ok {
		return map[string]any{
			"field":    firstNonEmptyString(workflowDraftString(expr["field"]), workflowDraftConditionFieldHint(condition, label)),
			"operator": normalizeWorkflowDraftConditionOperator(firstNonEmptyString(workflowDraftString(expr["operator"]), workflowDraftConditionOperatorHint(condition, label))),
			"value":    workflowDraftConditionValueOrDefault(expr["value"], condition, label),
		}
	}
	return map[string]any{
		"field":    workflowDraftConditionFieldHint(condition, label),
		"operator": workflowDraftConditionOperatorHint(condition, label),
		"value":    workflowDraftConditionValueOrDefault(nil, condition, label),
	}
}

func workflowDraftConditionFieldHint(values ...any) string {
	text := strings.ToLower(workflowDraftJoinedText(values...))
	switch {
	case strings.Contains(text, "leave") || strings.Contains(text, "\u8bf7\u5047") || strings.Contains(text, "\u5929"):
		return "days"
	case strings.Contains(text, "amount") || strings.Contains(text, "\u91d1\u989d") || strings.Contains(text, "\u8d39\u7528"):
		return "amount"
	default:
		return "value"
	}
}

func workflowDraftConditionOperatorHint(values ...any) string {
	text := strings.ToLower(workflowDraftJoinedText(values...))
	switch {
	case strings.Contains(text, ">=") || strings.Contains(text, "at least") || strings.Contains(text, "\u5927\u4e8e\u7b49\u4e8e"):
		return condeval.OpGreaterThan
	case strings.Contains(text, "<=") || strings.Contains(text, "at most") || strings.Contains(text, "\u5c0f\u4e8e\u7b49\u4e8e"):
		return condeval.OpLessThan
	case strings.Contains(text, ">") || strings.Contains(text, "above") || strings.Contains(text, "greater") || strings.Contains(text, "longer") || strings.Contains(text, "\u8d85\u8fc7") || strings.Contains(text, "\u5927\u4e8e") || strings.Contains(text, "\u9ad8\u4e8e"):
		return condeval.OpGreaterThan
	case strings.Contains(text, "<") || strings.Contains(text, "below") || strings.Contains(text, "less") || strings.Contains(text, "\u5c0f\u4e8e") || strings.Contains(text, "\u4f4e\u4e8e"):
		return condeval.OpLessThan
	default:
		return condeval.OpEquals
	}
}

func normalizeWorkflowDraftConditionOperator(operator string) string {
	switch strings.ToLower(strings.TrimSpace(operator)) {
	case "=", "==", "eq", "equal", "equals", "\u7b49\u4e8e":
		return condeval.OpEquals
	case "!=", "<>", "ne", "not_equal", "not_equals", "\u4e0d\u7b49\u4e8e":
		return condeval.OpNotEquals
	case ">", ">=", "gt", "gte", "greater", "greater_than", "above", "longer", "\u8d85\u8fc7", "\u5927\u4e8e", "\u9ad8\u4e8e":
		return condeval.OpGreaterThan
	case "<", "<=", "lt", "lte", "less", "less_than", "below", "\u5c0f\u4e8e", "\u4f4e\u4e8e":
		return condeval.OpLessThan
	case "contains", "\u5305\u542b":
		return condeval.OpContains
	case "in", "in_list":
		return condeval.OpInList
	case "not_in", "not_in_list":
		return condeval.OpNotInList
	case "is_empty", "empty":
		return condeval.OpIsEmpty
	case "is_not_empty", "not_empty":
		return condeval.OpIsNotEmpty
	default:
		return condeval.OpEquals
	}
}

func isWorkflowDraftConditionOperator(operator string) bool {
	switch strings.TrimSpace(operator) {
	case condeval.OpEquals, condeval.OpNotEquals, condeval.OpGreaterThan, condeval.OpLessThan, condeval.OpContains, condeval.OpInList, condeval.OpNotInList, condeval.OpIsEmpty, condeval.OpIsNotEmpty:
		return true
	default:
		return false
	}
}

func workflowDraftConditionValueOrDefault(value any, hints ...any) any {
	if value != nil {
		return value
	}
	if n, ok := firstWorkflowDraftInteger(workflowDraftJoinedText(hints...)); ok {
		return n
	}
	return true
}

func workflowDraftJoinedText(values ...any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if text := workflowDraftString(value); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func buildFallbackWorkflowDraft(description, language string) map[string]any {
	zh := strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "zh")
	name := "Approval workflow draft"
	if zh {
		name = "\u5ba1\u6279\u6d41\u7a0b\u8349\u7a3f"
	}
	graph := buildLinearFallbackWorkflowGraph(description, zh)
	if workflowDraftDescriptionHasCondition(description) {
		graph = buildConditionalFallbackWorkflowGraph(description, zh)
	}
	return map[string]any{
		"name":        name,
		"description": description,
		"graph":       graph,
	}
}

func fallbackWorkflowLabel(zh bool, en, zhHans string) string {
	if zh {
		return zhHans
	}
	return en
}

func buildLinearFallbackWorkflowGraph(description string, zh bool) map[string]any {
	return map[string]any{
		"nodes": []map[string]any{
			fallbackTriggerNode(description, zh),
			fallbackFormNode(description, zh),
			fallbackApprovalNode("node3", fallbackWorkflowLabel(zh, "Approval", "\u5ba1\u6279"), 520, 80),
			fallbackTerminalNode("node4", 740, 80, zh),
		},
		"edges": []map[string]any{
			{"id": "edge1", "source_id": "node1", "target_id": "node2"},
			{"id": "edge2", "source_id": "node2", "target_id": "node3"},
			{"id": "edge3", "source_id": "node3", "target_id": "node4"},
		},
	}
}

func buildConditionalFallbackWorkflowGraph(description string, zh bool) map[string]any {
	conditionalRole := fallbackConditionalApprovalLabel(description, zh)
	return map[string]any{
		"nodes": []map[string]any{
			fallbackTriggerNode(description, zh),
			fallbackFormNode(description, zh),
			fallbackApprovalNode("node3", fallbackWorkflowLabel(zh, "Manager approval", "\u4e3b\u7ba1\u5ba1\u6279"), 520, 80),
			{
				"id":       "node4",
				"type":     "condition_branch",
				"label":    fallbackWorkflowLabel(zh, "Check condition", "\u6761\u4ef6\u5224\u65ad"),
				"position": map[string]any{"x": 740, "y": 80},
				"config": map[string]any{
					"branches": []map[string]any{
						{
							"label":          fallbackConditionBranchLabel(description, zh),
							"condition":      fallbackConditionBranchLabel(description, zh),
							"target_node_id": "node5",
							"priority":       0,
							"expression": map[string]any{
								"field":    fallbackConditionField(description),
								"operator": condeval.OpGreaterThan,
								"value":    fallbackConditionThreshold(description),
							},
						},
					},
					"default_branch": "node6",
				},
			},
			fallbackApprovalNode("node5", conditionalRole, 960, 20),
			fallbackTerminalNode("node6", 1180, 80, zh),
		},
		"edges": []map[string]any{
			{"id": "edge1", "source_id": "node1", "target_id": "node2"},
			{"id": "edge2", "source_id": "node2", "target_id": "node3"},
			{"id": "edge3", "source_id": "node3", "target_id": "node4"},
			{"id": "edge4", "source_id": "node4", "target_id": "node5"},
			{"id": "edge5", "source_id": "node4", "target_id": "node6"},
			{"id": "edge6", "source_id": "node5", "target_id": "node6"},
		},
	}
}

func fallbackConditionField(description string) string {
	lower := strings.ToLower(description)
	switch {
	case strings.Contains(lower, "leave") || strings.Contains(description, "\u8bf7\u5047"):
		return "days"
	case strings.Contains(lower, "amount") || strings.Contains(description, "\u91d1\u989d"):
		return "amount"
	default:
		return "value"
	}
}

func fallbackConditionThreshold(description string) any {
	if n, ok := firstWorkflowDraftInteger(description); ok {
		return n
	}
	return true
}

func firstWorkflowDraftInteger(text string) (int, bool) {
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			continue
		}
		n := 0
		for ; i < len(text) && text[i] >= '0' && text[i] <= '9'; i++ {
			n = n*10 + int(text[i]-'0')
		}
		return n, true
	}
	return 0, false
}

func fallbackTriggerNode(description string, zh bool) map[string]any {
	return map[string]any{
		"id":       "node1",
		"type":     "trigger",
		"label":    fallbackWorkflowLabel(zh, "Start", "\u5f00\u59cb"),
		"position": map[string]any{"x": 80, "y": 80},
		"config":   map[string]any{"trigger_type": "manual", "description": description},
	}
}

func fallbackFormNode(description string, zh bool) map[string]any {
	return map[string]any{
		"id":       "node2",
		"type":     "form",
		"label":    fallbackWorkflowLabel(zh, "Submit request", "\u63d0\u4ea4\u7533\u8bf7"),
		"position": map[string]any{"x": 300, "y": 80},
		"config": map[string]any{
			"fields":      []any{},
			"description": description,
		},
	}
}

func fallbackApprovalNode(id, label string, x, y int) map[string]any {
	return map[string]any{
		"id":       id,
		"type":     "approval",
		"label":    label,
		"position": map[string]any{"x": x, "y": y},
		"config": map[string]any{
			"approver_ids":      []any{workflowDraftDefaultApproverRoleID},
			"mode":              "single",
			"min_approvals":     1,
			"approver_order":    []any{},
			"timeout_hours":     24,
			"fallback_approver": "",
		},
	}
}

func fallbackTerminalNode(id string, x, y int, zh bool) map[string]any {
	return map[string]any{
		"id":       id,
		"type":     "terminal",
		"label":    fallbackWorkflowLabel(zh, "Complete", "\u5b8c\u6210"),
		"position": map[string]any{"x": x, "y": y},
		"config": map[string]any{
			"result_executors": []any{},
			"notifiers":        []any{},
		},
	}
}

func workflowDraftDescriptionHasCondition(description string) bool {
	lower := strings.ToLower(description)
	conditionHints := []string{
		"if ", "when ", "otherwise", "else", "above", "below", "greater than", "less than", ">",
		"\u5982\u679c", "\u5f53", "\u5426\u5219", "\u8d85\u8fc7", "\u5927\u4e8e", "\u5c0f\u4e8e", "\u9ad8\u4e8e", "\u4f4e\u4e8e",
	}
	for _, hint := range conditionHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

func fallbackConditionalApprovalLabel(description string, zh bool) string {
	lower := strings.ToLower(description)
	switch {
	case strings.Contains(lower, "hr") || strings.Contains(description, "\u4eba\u529b"):
		return fallbackWorkflowLabel(zh, "HR approval", "HR \u5ba1\u6279")
	case strings.Contains(lower, "finance") || strings.Contains(description, "\u8d22\u52a1"):
		return fallbackWorkflowLabel(zh, "Finance approval", "\u8d22\u52a1\u5ba1\u6279")
	case strings.Contains(lower, "legal") || strings.Contains(description, "\u6cd5\u52a1"):
		return fallbackWorkflowLabel(zh, "Legal approval", "\u6cd5\u52a1\u5ba1\u6279")
	default:
		return fallbackWorkflowLabel(zh, "Conditional approval", "\u6761\u4ef6\u5ba1\u6279")
	}
}

func fallbackConditionBranchLabel(description string, zh bool) string {
	lower := strings.ToLower(description)
	switch {
	case strings.Contains(lower, "leave") || strings.Contains(description, "\u8bf7\u5047"):
		return fallbackWorkflowLabel(zh, "Leave is longer than the threshold", "\u8bf7\u5047\u5929\u6570\u8d85\u8fc7\u9608\u503c")
	case strings.Contains(lower, "amount") || strings.Contains(description, "\u91d1\u989d"):
		return fallbackWorkflowLabel(zh, "Amount is above the threshold", "\u91d1\u989d\u8d85\u8fc7\u9608\u503c")
	default:
		return fallbackWorkflowLabel(zh, "Condition is met", "\u6761\u4ef6\u6210\u7acb")
	}
}
