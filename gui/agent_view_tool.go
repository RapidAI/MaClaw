package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

const registeredToolAgentViewArgsField = "_tool_args"
const registeredToolApprovalIDField = "_tool_approval_id"
const registeredToolPolicyOwnerIDField = "_runtime_policy_owner_id"
const registeredToolRuntimePlatformField = "_runtime_platform"

// registeredToolContextTokensField is injected only by the local execution
// dispatcher after model-supplied arguments have been parsed and sanitized.
// It is never part of a tool schema and must not be accepted from the model.
const registeredToolContextTokensField = "_runtime_context_tokens"

// archiveExternalApprovalTokenField is populated only in the in-memory copy
// of a pending approval. It is deliberately not part of the archive schema.
const archiveExternalApprovalTokenField = "_archive_external_approval_token"

type registeredToolPendingApproval struct {
	ID               string
	ToolName         string
	Args             map[string]interface{}
	SessionID        string
	PolicyOwnerID    string
	AuditPrincipalID string
	Risk             security.RiskAssessment
	CreatedAt        time.Time
}

func (a registeredToolPendingApproval) trustedAuditPrincipal() string {
	if id := strings.TrimSpace(a.AuditPrincipalID); id != "" {
		return id
	}
	return strings.TrimSpace(a.PolicyOwnerID)
}

var registeredToolApprovalStore = struct {
	sync.Mutex
	next                  int64
	items                 map[string]registeredToolPendingApproval
	archiveExternalTokens map[string]bool
}{items: map[string]registeredToolPendingApproval{}, archiveExternalTokens: map[string]bool{}}

type registeredToolValidationIssue struct {
	Path    string
	Message string
}

type registeredToolSchemaVariant struct {
	ID          string
	Label       string
	Description string
	Schema      map[string]interface{}
}

func (h *IMMessageHandler) emitRegisteredToolApprovalAgentViewIfNeeded(name string, args map[string]interface{}, ctx *SecurityCallContext, policyOwnerID string) bool {
	if h == nil || h.app == nil || h.firewall == nil || h.firewall.analyzer == nil {
		return false
	}
	if h.firewall.policy != nil && h.firewall.policy.IsDeveloperMode() {
		return false
	}
	sessionID := ""
	if ctx != nil {
		sessionID = strings.TrimSpace(ctx.SessionID)
	}
	if sessionID != "" && h.firewall.isSessionApproved(sessionID, name) {
		return false
	}
	risk := h.firewall.analyzer.Assess(name, args, ctx)
	action := security.PolicyAllow
	if h.firewall.policy != nil {
		action = h.firewall.policy.Evaluate(name, args, risk.Level)
	}
	if action != security.PolicyAsk {
		return false
	}
	principal := trustedAuditPrincipalFromSecurityContext(ctx, policyOwnerID)
	approval := storeRegisteredToolPendingApprovalForPrincipal(name, args, sessionID, policyOwnerID, principal, risk)
	h.firewall.recordAudit(name, args, risk, security.PolicyAsk, "agent_view_approval_pending", sessionID, principal)
	return h.app.emitAgentView(buildRegisteredToolApprovalAgentView(approval))
}

// emitArchiveExternalApprovalIfNeeded is an action-scoped approval boundary.
// The ordinary tool firewall can allow the "archive" tool for safe embedded
// work, but that must never imply approval to launch an external executable.
func (h *IMMessageHandler) emitArchiveExternalApprovalIfNeeded(args map[string]interface{}, ctx *SecurityCallContext, policyOwnerID string) bool {
	if !isArchiveExternalExtraction(args) || hasArchiveExternalApproval(args) || h == nil || h.app == nil {
		return false
	}
	token, err := newArchiveExternalApprovalToken()
	if err != nil {
		return false
	}
	sessionID := ""
	if ctx != nil {
		sessionID = strings.TrimSpace(ctx.SessionID)
	}
	risk := security.RiskAssessment{Level: security.RiskHigh, Reason: "启动已安装的外部解压程序", Factors: []string{"external_archive_program", "requires_explicit_user_approval"}}
	principal := trustedAuditPrincipalFromSecurityContext(ctx, policyOwnerID)
	approval := storeRegisteredToolPendingApprovalForPrincipal("archive", args, sessionID, policyOwnerID, principal, risk)
	registeredToolApprovalStore.Lock()
	item := registeredToolApprovalStore.items[approval.ID]
	item.Args[archiveExternalApprovalTokenField] = token
	registeredToolApprovalStore.items[approval.ID] = item
	registeredToolApprovalStore.archiveExternalTokens[token] = true
	registeredToolApprovalStore.Unlock()
	if h.firewall != nil {
		h.firewall.recordAudit("archive", args, risk, security.PolicyAsk, "external_archive_approval_pending", sessionID, principal)
	}
	pending, ok := getRegisteredToolPendingApproval(approval.ID)
	if !ok {
		return false
	}
	return h.app.emitAgentView(buildRegisteredToolApprovalAgentView(pending))
}

func isArchiveExternalExtraction(args map[string]interface{}) bool {
	action, _ := args["action"].(string)
	return strings.EqualFold(strings.TrimSpace(action), "extract_external")
}

func newArchiveExternalApprovalToken() (string, error) {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func hasArchiveExternalApproval(args map[string]interface{}) bool {
	token, _ := args[archiveExternalApprovalTokenField].(string)
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	registeredToolApprovalStore.Lock()
	defer registeredToolApprovalStore.Unlock()
	return registeredToolApprovalStore.archiveExternalTokens[token]
}

// consumeArchiveExternalApproval makes an external-program approval one-time.
func consumeArchiveExternalApproval(args map[string]interface{}) bool {
	token, _ := args[archiveExternalApprovalTokenField].(string)
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	registeredToolApprovalStore.Lock()
	defer registeredToolApprovalStore.Unlock()
	if !registeredToolApprovalStore.archiveExternalTokens[token] {
		return false
	}
	delete(registeredToolApprovalStore.archiveExternalTokens, token)
	return true
}

func revokeArchiveExternalApproval(args map[string]interface{}) {
	token, _ := args[archiveExternalApprovalTokenField].(string)
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	registeredToolApprovalStore.Lock()
	defer registeredToolApprovalStore.Unlock()
	delete(registeredToolApprovalStore.archiveExternalTokens, token)
}

func (h *IMMessageHandler) handleRegisteredToolAgentViewSubmit(toolName string, data map[string]interface{}) *IMAgentResponse {
	toolName = strings.TrimSpace(toolName)
	if isDisabledExternalCodingSessionTool(toolName) {
		return &IMAgentResponse{Text: disabledExternalCodingSessionToolText(toolName), Error: "external coding-session tool disabled: " + toolName, ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if h == nil || h.registry == nil || toolName == "" {
		return &IMAgentResponse{Text: "Tool task panel submission is missing tool context.", Error: "missing tool context", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	tool, ok := h.registry.Get(toolName)
	if !ok || tool == nil {
		return &IMAgentResponse{Text: "Tool is no longer available.", Error: "tool not found: " + toolName, ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}

	baseArgs, _ := data[registeredToolAgentViewArgsField].(map[string]interface{})
	args := cloneMISInterfaceMap(baseArgs)
	if args == nil {
		args = make(map[string]interface{})
	}
	for key, value := range data {
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(key, "_") {
			continue
		}
		args[key] = coerceRegisteredToolValue(tool, key, value, data)
	}

	if missing := registeredToolMissingRequired(tool, args); len(missing) > 0 {
		if h.app != nil {
			if view := buildRegisteredToolAgentView(*tool, args, missing); view != nil {
				h.app.emitAgentView(view)
			}
		}
		return &IMAgentResponse{Text: "Tool parameters are still incomplete. Fill the required fields in the task panel.", Error: "missing required parameters: " + strings.Join(missing, ", "), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if validationIssues := registeredToolValidateArgIssues(*tool, args); len(validationIssues) > 0 {
		validationErrors := registeredToolValidationMessages(validationIssues)
		if h.app != nil {
			if view := buildRegisteredToolAgentView(*tool, args, nil); view != nil {
				applyRegisteredToolFieldIssues(view, validationIssues)
				h.app.emitAgentView(view)
			}
		}
		return &IMAgentResponse{Text: "Tool parameters need correction. Review the task panel.", Error: strings.Join(validationErrors, "; "), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	policyOwnerID, explicitRuntimeOwner := consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(args)
	if explicitRuntimeOwner && policyOwnerID == "" && h.registeredToolAcceptsRuntimePolicyOwnerArg(toolName) {
		return &IMAgentResponse{Text: "Tool execution failed: runtime owner is missing; isolated runtime will not fall back to desktop loop.", Error: "runtime owner is missing", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	// A task-panel submit can outlive the model turn that opened it.  Keep the
	// current loop's local-file Computer Use fence on this direct-handler path
	// too, otherwise a stale computer_* panel can bypass the normal execution
	// gateway and drive the desktop after an attachment has been staged.
	if localFileWorkBlocksComputerUseExecution(h.runtimeLoopContextForOwner(policyOwnerID), "", toolName) {
		text := "[system rejected] Computer Use is unavailable while handling the current local attachment. Use the local file/document tools instead."
		return &IMAgentResponse{Text: text, Error: text, ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if rejection := h.registeredToolWorkflowPolicyRejectionForOwner(policyOwnerID, toolName, args); rejection != nil {
		return rejection
	}
	if policyOwnerID != "" && h.registeredToolAcceptsRuntimePolicyOwnerArg(toolName) {
		args[registeredToolPolicyOwnerIDField] = policyOwnerID
	}
	if h.emitArchiveExternalApprovalIfNeeded(args, &SecurityCallContext{SessionID: localSessionIDFromToolArgs(args)}, policyOwnerID) {
		return &IMAgentResponse{Text: "External archive extraction needs approval. An approval panel has been opened on the right.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}

	var result string
	if tool.HandlerProg != nil {
		result = tool.HandlerProg(args, nil)
	} else if tool.Handler != nil {
		result = tool.Handler(args)
	} else {
		return &IMAgentResponse{Text: "Tool has no runnable handler.", Error: "missing handler: " + toolName, ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if h.app != nil {
		h.app.emitAgentView(buildRegisteredToolResultAgentView(*tool, result))
	}
	return &IMAgentResponse{Text: fmt.Sprintf("Tool %s completed from task panel.", toolName), ResponseSource: imResponseSourceAgentViewSubmit.String()}
}

func (h *IMMessageHandler) handleRegisteredToolApprovalAgentViewSubmit(data map[string]interface{}) *IMAgentResponse {
	parameters, _ := data["parameters"].(map[string]interface{})
	approvalID := nonEmptyStringFromAny(data[registeredToolApprovalIDField])
	if approvalID == "" {
		approvalID = nonEmptyStringFromAny(parameters[registeredToolApprovalIDField])
	}
	if approvalID == "" {
		return &IMAgentResponse{Text: "Tool approval is missing context.", Error: "missing approval id", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	approval, ok := getRegisteredToolPendingApproval(approvalID)
	if !ok {
		return &IMAgentResponse{Text: "Tool approval has expired or was already handled.", Error: "approval not found", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if !boolFromAny(data["approved"]) {
		deleteRegisteredToolPendingApproval(approvalID)
		if h != nil && h.firewall != nil {
			h.firewall.recordAudit(approval.ToolName, approval.Args, approval.Risk, security.PolicyAsk, "agent_view_approval_rejected", approval.SessionID, approval.trustedAuditPrincipal())
		}
		return &IMAgentResponse{Text: "Tool execution was rejected.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if rejection := h.registeredToolWorkflowPolicyRejectionForOwner(approval.PolicyOwnerID, approval.ToolName, approval.Args); rejection != nil {
		deleteRegisteredToolPendingApproval(approvalID)
		return rejection
	}
	if h != nil && h.firewall != nil && approval.SessionID != "" {
		h.firewall.ApproveForSession(approval.SessionID, approval.ToolName)
		h.firewall.recordAudit(approval.ToolName, approval.Args, approval.Risk, security.PolicyUserOverride, "agent_view_approval_approved", approval.SessionID, approval.trustedAuditPrincipal())
	}
	// Keep the action-scoped archive token alive just long enough for the
	// immediately following execution gateway to consume it.  Rejected and
	// expired approvals still revoke it through deleteRegisteredToolPendingApproval.
	removeRegisteredToolPendingApprovalPreservingExecutionToken(approvalID)
	dataBytes, _ := json.Marshal(approval.Args)
	defer revokeArchiveExternalApproval(approval.Args)
	result := h.executeToolDetailedWithRuntimeContext(
		withTrustedAuditPrincipal(context.Background(), approval.trustedAuditPrincipal()),
		approval.PolicyOwnerID,
		strings.TrimSpace(approval.PolicyOwnerID) != "",
		"",
		approval.ToolName,
		string(dataBytes),
		"",
		nil,
	).Text
	if h != nil && h.app != nil && h.registry != nil {
		if tool, ok := h.registry.Get(approval.ToolName); ok && tool != nil {
			h.app.emitAgentView(buildRegisteredToolResultAgentView(*tool, result))
		}
	}
	return &IMAgentResponse{Text: "Approved tool execution completed.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
}

func (h *IMMessageHandler) registeredToolWorkflowPolicyRejectionForOwner(policyOwnerID, toolName string, args map[string]interface{}) *IMAgentResponse {
	if h == nil {
		return nil
	}
	policyOwnerID = strings.TrimSpace(policyOwnerID)
	if policyOwnerID == "" {
		return nil
	}
	if !h.isWorkflowToolAllowedForOwner(policyOwnerID, toolName) {
		text := workflowPolicyToolRejectedText(toolName)
		return &IMAgentResponse{Text: text, Error: text, ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	data, err := json.Marshal(args)
	if err != nil {
		return &IMAgentResponse{Text: "Tool parameters could not be checked against workflow policy.", Error: err.Error(), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if allowed, reason := h.isWorkflowToolCallAllowedForOwner(policyOwnerID, toolName, string(data)); !allowed {
		text := "[system rejected] " + reason
		return &IMAgentResponse{Text: text, Error: text, ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	return nil
}

func (h *IMMessageHandler) attachRegisteredToolPolicyOwner(args map[string]interface{}) map[string]interface{} {
	return h.attachRegisteredToolPolicyOwnerForOwner(args, h.currentRuntimePolicyOwnerID())
}

func (h *IMMessageHandler) attachRegisteredToolPolicyOwnerForOwner(args map[string]interface{}, policyOwnerID string) map[string]interface{} {
	out := cloneMISInterfaceMap(args)
	if out == nil {
		out = map[string]interface{}{}
	}
	if ownerID := strings.TrimSpace(policyOwnerID); ownerID != "" {
		out[registeredToolPolicyOwnerIDField] = ownerID
	}
	return out
}

func (h *IMMessageHandler) currentRuntimePolicyOwnerID() string {
	ownerID, _ := h.currentRuntimePolicyOwnerState()
	return ownerID
}

func (h *IMMessageHandler) currentRuntimeOrLegacyPolicyOwnerID() string {
	if h == nil {
		return ""
	}
	if ownerID, explicit := h.currentRuntimePolicyOwnerState(); explicit {
		return ownerID
	}
	return ""
}

func (h *IMMessageHandler) legacyLastUserID() string {
	if h == nil {
		return ""
	}
	h.globalLoopMu.RLock()
	defer h.globalLoopMu.RUnlock()
	return strings.TrimSpace(h.lastUserID)
}

func (h *IMMessageHandler) currentRuntimePolicyOwnerState() (string, bool) {
	if h == nil {
		return "", false
	}
	h.globalLoopMu.RLock()
	ctx := h.currentLoopCtx
	h.globalLoopMu.RUnlock()
	if ctx == nil {
		return "", false
	}
	if strings.TrimSpace(ctx.Runtime.RequestID) != "" {
		return strings.TrimSpace(ctx.Runtime.PolicyOwnerID), true
	}
	if ownerID := strings.TrimSpace(ctx.Runtime.PolicyOwnerID); ownerID != "" {
		return ownerID, true
	}
	return "", false
}

func registeredToolValidateArgs(tool RegisteredTool, args map[string]interface{}) []string {
	return registeredToolValidationMessages(registeredToolValidateArgIssues(tool, args))
}

func registeredToolValidateArgIssues(tool RegisteredTool, args map[string]interface{}) []registeredToolValidationIssue {
	props := registeredToolSchemaProperties(tool.InputSchema)
	variants := registeredToolSchemaVariants(tool.InputSchema)
	if len(props) == 0 && len(variants) == 0 {
		return nil
	}
	issues := []registeredToolValidationIssue{}
	var selectedVariant *registeredToolSchemaVariant
	if len(variants) > 0 {
		selectedVariant = registeredToolSelectedVariant(args, variants)
	}
	issues = append(issues, registeredToolValidateAdditionalProperties("", "Parameters", args, registeredToolEffectiveSchema(tool.InputSchema, selectedVariant))...)
	issues = append(issues, registeredToolValidateDependentRequired("", "Parameters", args, tool.InputSchema)...)
	if len(variants) > 0 {
		issues = append(issues, registeredToolValidateSelectedVariant(args, variants)...)
		if selectedVariant != nil {
			issues = append(issues, registeredToolValidateDependentRequired("", "Parameters", args, selectedVariant.Schema)...)
			issues = append(issues, registeredToolValidateSchemaProperties("", "Parameters", args, selectedVariant.Schema)...)
		}
	}
	issues = append(issues, registeredToolValidateSchemaProperties("", "Parameters", args, tool.InputSchema)...)
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].Path == issues[j].Path {
			return issues[i].Message < issues[j].Message
		}
		return issues[i].Path < issues[j].Path
	})
	return issues
}

func registeredToolValidateSchemaProperties(path, label string, args map[string]interface{}, schema map[string]interface{}) []registeredToolValidationIssue {
	props := registeredToolSchemaProperties(schema)
	required := stringSet(registeredToolRequiredFromSchema(schema))
	issues := []registeredToolValidationIssue{}
	for name, prop := range props {
		value, ok := args[name]
		if !ok || registeredToolValueMissing(value) {
			if required[name] {
				fieldPath := name
				if strings.TrimSpace(path) != "" {
					fieldPath = path + "." + name
				}
				fieldLabel := registeredToolFieldLabel(name, prop)
				if strings.TrimSpace(label) != "" && label != "Parameters" {
					fieldLabel = label + "." + fieldLabel
				}
				issues = append(issues, registeredToolValidationIssue{Path: fieldPath, Message: fieldLabel})
			}
			continue
		}
		fieldPath := name
		if strings.TrimSpace(path) != "" {
			fieldPath = path + "." + name
		}
		fieldLabel := registeredToolFieldLabel(name, prop)
		if strings.TrimSpace(label) != "" && label != "Parameters" {
			fieldLabel = label + "." + fieldLabel
		}
		issues = append(issues, registeredToolValidateValue(fieldPath, fieldLabel, value, prop)...)
	}
	return issues
}

func registeredToolValidateValue(path, label string, value interface{}, prop map[string]interface{}) []registeredToolValidationIssue {
	issues := []registeredToolValidationIssue{}
	if constIssue := registeredToolValidateConst(path, label, value, prop); constIssue != nil {
		issues = append(issues, *constIssue)
	}
	if enumIssues := registeredToolValidateEnum(path, label, value, prop); len(enumIssues) > 0 {
		issues = append(issues, enumIssues...)
	}
	typ := normalizeRegisteredToolJSONSchemaType(fmt.Sprint(prop["type"]))
	if typ == registeredToolJSONSchemaUnknown {
		typ = normalizeRegisteredToolJSONSchemaType(registeredToolInferJSONType(value))
	}
	switch typ {
	case registeredToolJSONSchemaNumber, registeredToolJSONSchemaInteger:
		number, ok := numberFromAny(value)
		if !ok {
			return append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be a valid number", label)})
		}
		if min, ok := numberFromAny(prop["minimum"]); ok && number < min {
			issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be at least %s", label, formatConstraintNumber(min))})
		}
		if max, ok := numberFromAny(prop["maximum"]); ok && number > max {
			issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be at most %s", label, formatConstraintNumber(max))})
		}
		if exclusiveMin, ok := registeredToolExclusiveBoundary(prop["exclusiveMinimum"], prop["minimum"]); ok && number <= exclusiveMin {
			issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be greater than %s", label, formatConstraintNumber(exclusiveMin))})
		}
		if exclusiveMax, ok := registeredToolExclusiveBoundary(prop["exclusiveMaximum"], prop["maximum"]); ok && number >= exclusiveMax {
			issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be less than %s", label, formatConstraintNumber(exclusiveMax))})
		}
		if multipleOf, ok := numberFromAny(prop["multipleOf"]); ok && !registeredToolMultipleOf(number, multipleOf) {
			issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be a multiple of %s", label, formatConstraintNumber(multipleOf))})
		}
	case registeredToolJSONSchemaString:
		text := fmt.Sprint(value)
		if minLength, ok := numberFromAny(prop["minLength"]); ok && float64(len([]rune(text))) < minLength {
			issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be at least %s characters", label, formatConstraintNumber(minLength))})
		}
		if maxLength, ok := numberFromAny(prop["maxLength"]); ok && float64(len([]rune(text))) > maxLength {
			issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be at most %s characters", label, formatConstraintNumber(maxLength))})
		}
		if pattern := nonEmptyStringFromAny(prop["pattern"]); pattern != "" {
			if compiled, err := regexp.Compile(pattern); err == nil && !compiled.MatchString(text) {
				issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s has an invalid format", label)})
			}
		}
		if format := nonEmptyStringFromAny(prop["format"]); format != "" {
			if errText := registeredToolFormatValidationError(label, text, format); errText != "" {
				issues = append(issues, registeredToolValidationIssue{Path: path, Message: errText})
			}
		}
	case registeredToolJSONSchemaArray:
		length, ok := registeredToolCollectionLen(value)
		if !ok {
			return append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be an array", label)})
		}
		if minItems, ok := numberFromAny(prop["minItems"]); ok && float64(length) < minItems {
			issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s needs at least %s item(s)", label, formatConstraintNumber(minItems))})
		}
		if maxItems, ok := numberFromAny(prop["maxItems"]); ok && float64(length) > maxItems {
			issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s allows at most %s item(s)", label, formatConstraintNumber(maxItems))})
		}
		if boolFromAny(prop["uniqueItems"]) && !registeredToolArrayItemsUnique(value) {
			issues = append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must not contain duplicate items", label)})
		}
		items := registeredToolAsMap(prop["items"])
		if itemEnumIssues := registeredToolValidateArrayItemEnum(path, label, value, items); len(itemEnumIssues) > 0 {
			issues = append(issues, itemEnumIssues...)
		}
		itemProps := registeredToolSchemaProperties(items)
		required := stringSet(registeredToolRequiredFromSchema(items))
		if len(itemProps) > 0 {
			for rowIndex, row := range registeredToolSliceItems(value) {
				rowMap := registeredToolAsMap(row)
				rowLabel := fmt.Sprintf("%s[%d]", label, rowIndex+1)
				issues = append(issues, registeredToolValidateAdditionalProperties(path, rowLabel, rowMap, items)...)
				issues = append(issues, registeredToolValidateDependentRequired(path, rowLabel, rowMap, items)...)
				for key, itemProp := range itemProps {
					nestedLabel := fmt.Sprintf("%s[%d].%s", label, rowIndex+1, registeredToolFieldLabel(key, itemProp))
					if registeredToolValueMissing(rowMap[key]) {
						if required[key] {
							issues = append(issues, registeredToolValidationIssue{Path: path, Message: nestedLabel})
						}
						continue
					}
					issues = append(issues, registeredToolValidateValue(path+"."+key, nestedLabel, rowMap[key], itemProp)...)
				}
			}
		}
	case registeredToolJSONSchemaObject:
		objectValue := registeredToolAsMap(value)
		if objectValue == nil {
			return append(issues, registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be an object", label)})
		}
		nestedProps := registeredToolSchemaProperties(prop)
		required := stringSet(registeredToolRequiredFromSchema(prop))
		issues = append(issues, registeredToolValidateAdditionalProperties(path, label, objectValue, prop)...)
		issues = append(issues, registeredToolValidateDependentRequired(path, label, objectValue, prop)...)
		for key, itemProp := range nestedProps {
			nestedLabel := label + "." + registeredToolFieldLabel(key, itemProp)
			if registeredToolValueMissing(objectValue[key]) {
				if required[key] {
					issues = append(issues, registeredToolValidationIssue{Path: path + "." + key, Message: nestedLabel})
				}
				continue
			}
			issues = append(issues, registeredToolValidateValue(path+"."+key, nestedLabel, objectValue[key], itemProp)...)
		}
	}
	return issues
}

func registeredToolValidateAdditionalProperties(path, label string, value map[string]interface{}, schema map[string]interface{}) []registeredToolValidationIssue {
	if !registeredToolAdditionalPropertiesDisabled(schema) || len(value) == 0 {
		return nil
	}
	props := registeredToolSchemaProperties(schema)
	issues := []registeredToolValidationIssue{}
	for key := range value {
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(key, "_") {
			continue
		}
		if _, ok := props[key]; ok {
			continue
		}
		issuePath := key
		if strings.TrimSpace(path) != "" {
			issuePath = path + "." + key
		}
		issues = append(issues, registeredToolValidationIssue{Path: issuePath, Message: fmt.Sprintf("%s.%s is not allowed", label, key)})
	}
	return issues
}

func registeredToolAdditionalPropertiesDisabled(schema map[string]interface{}) bool {
	value, ok := schema["additionalProperties"]
	if !ok {
		return false
	}
	if disabled, ok := value.(bool); ok {
		return !disabled
	}
	return false
}

func registeredToolValidateDependentRequired(path, label string, value map[string]interface{}, schema map[string]interface{}) []registeredToolValidationIssue {
	if len(value) == 0 || len(schema) == 0 {
		return nil
	}
	deps := registeredToolDependentRequired(schema)
	if len(deps) == 0 {
		return nil
	}
	issues := []registeredToolValidationIssue{}
	for trigger, required := range deps {
		if registeredToolValueMissing(value[trigger]) {
			continue
		}
		for _, dep := range required {
			if !registeredToolValueMissing(value[dep]) {
				continue
			}
			depPath := dep
			if strings.TrimSpace(path) != "" {
				depPath = path + "." + dep
			}
			issues = append(issues, registeredToolValidationIssue{
				Path:    depPath,
				Message: fmt.Sprintf("%s.%s is required when %s is provided", label, dep, trigger),
			})
		}
	}
	return issues
}

func registeredToolDependentRequired(schema map[string]interface{}) map[string][]string {
	out := map[string][]string{}
	for trigger, raw := range registeredToolAsMap(schema["dependentRequired"]) {
		if required := stringSliceFromAny(raw); len(required) > 0 {
			out[trigger] = required
		}
	}
	for trigger, raw := range registeredToolAsMap(schema["dependencies"]) {
		if required := stringSliceFromAny(raw); len(required) > 0 {
			if _, exists := out[trigger]; !exists {
				out[trigger] = required
			}
		}
	}
	return out
}

func registeredToolSchemaVariants(schema map[string]interface{}) []registeredToolSchemaVariant {
	rawVariants, keyword := schemaVariantValues(schema)
	if len(rawVariants) == 0 {
		return nil
	}
	variants := make([]registeredToolSchemaVariant, 0, len(rawVariants))
	for index, raw := range rawVariants {
		variantSchema := registeredToolAsMap(raw)
		if len(variantSchema) == 0 {
			continue
		}
		id := nonEmptyStringFromAny(variantSchema["$id"])
		if id == "" {
			id = nonEmptyStringFromAny(variantSchema["id"])
		}
		if id == "" {
			id = fmt.Sprintf("%s_%d", keyword, index+1)
		}
		label := nonEmptyStringFromAny(variantSchema["title"])
		if label == "" {
			label = nonEmptyStringFromAny(variantSchema["label"])
		}
		if label == "" {
			label = fmt.Sprintf("Option %d", index+1)
		}
		variants = append(variants, registeredToolSchemaVariant{
			ID:          id,
			Label:       label,
			Description: registeredToolFieldDescription(variantSchema),
			Schema:      variantSchema,
		})
	}
	return variants
}

func schemaVariantValues(schema map[string]interface{}) ([]interface{}, string) {
	for _, keyword := range []string{"oneOf", "anyOf"} {
		switch raw := schema[keyword].(type) {
		case []interface{}:
			return raw, keyword
		default:
			if raw == nil {
				continue
			}
			rv := reflect.ValueOf(raw)
			if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
				continue
			}
			values := make([]interface{}, 0, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				values = append(values, rv.Index(i).Interface())
			}
			return values, keyword
		}
	}
	return nil, ""
}

func registeredToolValidateSelectedVariant(args map[string]interface{}, variants []registeredToolSchemaVariant) []registeredToolValidationIssue {
	if len(variants) == 0 {
		return nil
	}
	selected := strings.TrimSpace(fmt.Sprint(args["_agent_view_variant"]))
	if selected == "" || selected == "<nil>" {
		return []registeredToolValidationIssue{{Path: "_agent_view_variant", Message: "Mode is required"}}
	}
	for _, variant := range variants {
		if variant.ID == selected {
			return nil
		}
	}
	return []registeredToolValidationIssue{{Path: "_agent_view_variant", Message: "Mode is not valid"}}
}

func registeredToolSelectedVariant(args map[string]interface{}, variants []registeredToolSchemaVariant) *registeredToolSchemaVariant {
	selected := strings.TrimSpace(fmt.Sprint(args["_agent_view_variant"]))
	for i := range variants {
		if variants[i].ID == selected {
			return &variants[i]
		}
	}
	if len(variants) > 0 {
		return &variants[0]
	}
	return nil
}

func registeredToolEffectiveSchema(base map[string]interface{}, selected *registeredToolSchemaVariant) map[string]interface{} {
	if selected == nil || len(selected.Schema) == 0 {
		return base
	}
	effective := cloneMISInterfaceMap(base)
	props := map[string]interface{}{}
	for name, prop := range registeredToolSchemaProperties(base) {
		props[name] = prop
	}
	for name, prop := range registeredToolSchemaProperties(selected.Schema) {
		props[name] = prop
	}
	effective["properties"] = props
	return effective
}

func registeredToolValidateConst(path, label string, value interface{}, prop map[string]interface{}) *registeredToolValidationIssue {
	constValue, ok := prop["const"]
	if !ok {
		return nil
	}
	if registeredToolValuesEqual(value, constValue) {
		return nil
	}
	return &registeredToolValidationIssue{Path: path, Message: fmt.Sprintf("%s must be %s", label, formatSchemaValue(constValue))}
}

func registeredToolValidateEnum(path, label string, value interface{}, prop map[string]interface{}) []registeredToolValidationIssue {
	allowed := registeredToolEnumValues(prop)
	if len(allowed) == 0 {
		return nil
	}
	if !registeredToolEnumContains(allowed, value) {
		return []registeredToolValidationIssue{{Path: path, Message: fmt.Sprintf("%s must be one of: %s", label, strings.Join(allowed, ", "))}}
	}
	return nil
}

func registeredToolValidateArrayItemEnum(path, label string, value interface{}, items map[string]interface{}) []registeredToolValidationIssue {
	allowed := registeredToolEnumValues(items)
	if len(allowed) == 0 {
		return nil
	}
	issues := []registeredToolValidationIssue{}
	for index, item := range registeredToolSliceItems(value) {
		if !registeredToolEnumContains(allowed, item) {
			issues = append(issues, registeredToolValidationIssue{
				Path:    path,
				Message: fmt.Sprintf("%s[%d] must be one of: %s", label, index+1, strings.Join(allowed, ", ")),
			})
		}
	}
	return issues
}

func registeredToolEnumValues(prop map[string]interface{}) []string {
	values := stringSliceFromAny(prop["enum"])
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func registeredToolEnumContains(allowed []string, value interface{}) bool {
	text := strings.TrimSpace(fmt.Sprint(value))
	for _, item := range allowed {
		if text == item {
			return true
		}
	}
	return false
}

func registeredToolValuesEqual(left, right interface{}) bool {
	if reflect.DeepEqual(left, right) {
		return true
	}
	if leftNumber, leftOK := numberFromAny(left); leftOK {
		if rightNumber, rightOK := numberFromAny(right); rightOK {
			return math.Abs(leftNumber-rightNumber) < 1e-9
		}
	}
	return strings.TrimSpace(fmt.Sprint(left)) == strings.TrimSpace(fmt.Sprint(right))
}

func formatSchemaValue(value interface{}) string {
	if value == nil {
		return "null"
	}
	switch v := value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		if number, ok := numberFromAny(value); ok {
			return formatConstraintNumber(number)
		}
		data, err := json.Marshal(value)
		if err == nil {
			return string(data)
		}
		return fmt.Sprint(value)
	}
}

func registeredToolExclusiveBoundary(exclusiveValue interface{}, inclusiveValue interface{}) (float64, bool) {
	if boundary, ok := numberFromAny(exclusiveValue); ok {
		return boundary, true
	}
	if exclusive, ok := exclusiveValue.(bool); ok && exclusive {
		return numberFromAny(inclusiveValue)
	}
	return 0, false
}

func registeredToolMultipleOf(value, step float64) bool {
	if step <= 0 {
		return true
	}
	quotient := value / step
	return math.Abs(quotient-math.Round(quotient)) < 1e-9
}

func registeredToolValidationMessages(issues []registeredToolValidationIssue) []string {
	if len(issues) == 0 {
		return nil
	}
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		message := strings.TrimSpace(issue.Message)
		if message != "" {
			messages = append(messages, message)
		}
	}
	return messages
}

func applyRegisteredToolFieldIssues(view map[string]interface{}, validationIssues []registeredToolValidationIssue) {
	if len(validationIssues) == 0 {
		return
	}
	view["formErrors"] = registeredToolValidationMessages(validationIssues)
	fields, _ := view["fields"].([]map[string]interface{})
	for _, field := range fields {
		name := strings.TrimSpace(fmt.Sprint(field["name"]))
		label := strings.TrimSpace(fmt.Sprint(field["label"]))
		for _, issue := range validationIssues {
			errText := strings.TrimSpace(issue.Message)
			topPath := registeredToolTopLevelPath(issue.Path)
			if topPath != "" && topPath == name || name != "" && strings.Contains(errText, name) || label != "" && strings.Contains(errText, label) {
				field["error"] = errText
				break
			}
		}
	}
}

func registeredToolTopLevelPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if dot := strings.Index(path, "."); dot >= 0 {
		path = path[:dot]
	}
	if bracket := strings.Index(path, "["); bracket >= 0 {
		path = path[:bracket]
	}
	return strings.TrimSpace(path)
}

func registeredToolMissingRequired(tool *RegisteredTool, args map[string]interface{}) []string {
	if tool == nil {
		return nil
	}
	required := registeredToolRequiredNames(*tool)
	if len(required) == 0 {
		return nil
	}
	missing := make([]string, 0, len(required))
	for _, key := range required {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if registeredToolValueMissing(args[key]) {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func buildRegisteredToolAgentView(tool RegisteredTool, args map[string]interface{}, missing []string) map[string]interface{} {
	if view := registeredToolSpecializedAgentView(tool, args, registeredToolSpecializedAgentViewOptions{
		ViewID:      "tool:run:" + tool.Name,
		TitlePrefix: avTr("Run ", "运行 "),
		Description: strings.TrimSpace(tool.Description),
		SubmitLabel: avTr("Run tool", "运行工具"),
		HiddenData: map[string]interface{}{
			registeredToolAgentViewArgsField: cloneMISInterfaceMap(args),
		},
		Meta: map[string]interface{}{
			"source": "tool.adapter",
			"tool":   tool.Name,
		},
	}); view != nil {
		return attachAgentViewSchemaVersion(view, "tool.adapter", tool.Name, tool.InputSchema)
	}
	fields := registeredToolAgentViewFields(tool, args, missing)
	if len(fields) == 0 {
		return nil
	}
	title := strings.TrimSpace(tool.Name)
	if title == "" {
		title = avTr("Tool", "工具")
	}
	description := strings.TrimSpace(tool.Description)
	if description == "" {
		description = avTr("Fill required parameters before running this tool.", "请填写必要参数后运行此工具。")
	}
	view := map[string]interface{}{
		"type":        "form",
		"id":          "tool:run:" + tool.Name,
		"title":       avTr("Run ", "运行 ") + title,
		"description": description,
		"fields":      fields,
		"formErrors":  []string{avTr("Required tool parameters are missing. Fill them here to continue.", "缺少必要的工具参数，请在此填写后继续。")},
		"submitLabel": avTr("Run tool", "运行工具"),
		"meta": map[string]interface{}{
			"source": "tool.adapter",
			"tool":   tool.Name,
		},
	}
	if deps := registeredToolDependentRequired(tool.InputSchema); len(deps) > 0 {
		view["dependentRequired"] = deps
	}
	if variants := registeredToolAgentViewVariants(tool.InputSchema, args); len(variants) > 0 {
		view["variants"] = variants
	}
	return attachAgentViewSchemaVersion(view, "tool.adapter", tool.Name, tool.InputSchema)
}

type registeredToolSpecializedAgentViewOptions struct {
	ViewID      string
	TitlePrefix string
	Description string
	SubmitLabel string
	HiddenData  map[string]interface{}
	Meta        map[string]interface{}
}

func registeredToolSpecializedAgentView(tool RegisteredTool, args map[string]interface{}, opts registeredToolSpecializedAgentViewOptions) map[string]interface{} {
	props := registeredToolSchemaProperties(tool.InputSchema)
	if len(props) == 0 {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		prop := props[name]
		switch registeredToolAgentViewKind(prop) {
		case registeredToolAgentViewResourcePicker:
			if view := registeredToolResourcePickerView(tool, name, prop, args, opts); view != nil {
				return view
			}
		case registeredToolAgentViewFieldMapper:
			if view := registeredToolFieldMapperView(tool, name, prop, args, opts); view != nil {
				return view
			}
		}
	}
	return nil
}

func registeredToolAgentViewKind(prop map[string]interface{}) registeredToolAgentViewWidgetKind {
	for _, key := range []string{"x-agent-view", "x_agent_view", "ui:widget", "ui_widget"} {
		if kind := normalizeRegisteredToolAgentViewKind(fmt.Sprint(prop[key])); kind != registeredToolAgentViewUnknown {
			return kind
		}
	}
	return registeredToolAgentViewUnknown
}

func registeredToolResourcePickerView(tool RegisteredTool, name string, prop map[string]interface{}, args map[string]interface{}, opts registeredToolSpecializedAgentViewOptions) map[string]interface{} {
	options := registeredToolResourceOptions(prop)
	if len(options) == 0 {
		return nil
	}
	title := registeredToolSpecializedTitle(tool, opts)
	description := firstNonEmptyMISAgentView(registeredToolFieldDescription(prop), opts.Description)
	view := map[string]interface{}{
		"type":         "resource_picker",
		"id":           opts.ViewID,
		"title":        title,
		"description":  description,
		"resourceType": firstNonEmptyMISAgentView(nonEmptyStringFromAny(prop["x-resource-type"]), nonEmptyStringFromAny(prop["resourceType"]), nonEmptyStringFromAny(prop["resource_type"]), name),
		"options":      options,
		"multiple":     boolFromAny(prop["x-multiple"]) || strings.EqualFold(strings.TrimSpace(fmt.Sprint(prop["type"])), "array"),
		"dataKey":      name,
		"hiddenData":   cloneMISInterfaceMap(opts.HiddenData),
		"submitLabel":  firstNonEmptyMISAgentView(opts.SubmitLabel, avTr("Submit", "提交")),
	}
	if value, ok := args[name]; ok && !registeredToolValueMissing(value) {
		view["value"] = value
	}
	if len(opts.Meta) > 0 {
		view["meta"] = cloneMISInterfaceMap(opts.Meta)
	}
	return view
}

func registeredToolFieldMapperView(tool RegisteredTool, name string, prop map[string]interface{}, args map[string]interface{}, opts registeredToolSpecializedAgentViewOptions) map[string]interface{} {
	sourceFields := registeredToolSchemaStringList(prop, "x-source-fields", "x_source_fields", "sourceFields", "source_fields")
	targetFields := registeredToolMapperTargetFields(prop)
	if len(sourceFields) == 0 || len(targetFields) == 0 {
		return nil
	}
	title := registeredToolSpecializedTitle(tool, opts)
	description := firstNonEmptyMISAgentView(registeredToolFieldDescription(prop), opts.Description)
	view := map[string]interface{}{
		"type":         "field_mapper",
		"id":           opts.ViewID,
		"title":        title,
		"description":  description,
		"sourceFields": sourceFields,
		"targetFields": targetFields,
		"dataKey":      name,
		"hiddenData":   cloneMISInterfaceMap(opts.HiddenData),
		"submitLabel":  firstNonEmptyMISAgentView(opts.SubmitLabel, avTr("Apply mapping", "应用映射")),
	}
	if value, ok := args[name]; ok && !registeredToolValueMissing(value) {
		view["value"] = value
	}
	if len(opts.Meta) > 0 {
		view["meta"] = cloneMISInterfaceMap(opts.Meta)
	}
	return view
}

func registeredToolSpecializedTitle(tool RegisteredTool, opts registeredToolSpecializedAgentViewOptions) string {
	title := strings.TrimSpace(tool.Name)
	if title == "" {
		title = "Tool"
	}
	return opts.TitlePrefix + title
}

func registeredToolAgentViewFields(tool RegisteredTool, args map[string]interface{}, missing []string) []map[string]interface{} {
	return registeredToolAgentViewFieldsForSchema(tool.InputSchema, registeredToolRequiredNames(tool), args, missing, true)
}

func registeredToolAgentViewFieldsForSchema(schema map[string]interface{}, requiredNames []string, args map[string]interface{}, missing []string, includeHiddenArgs bool) []map[string]interface{} {
	props := registeredToolSchemaProperties(schema)
	required := map[string]bool{}
	for _, key := range requiredNames {
		required[key] = true
	}
	missingSet := map[string]bool{}
	for _, key := range missing {
		missingSet[key] = true
	}

	names := make([]string, 0, len(props))
	for key := range props {
		if strings.TrimSpace(key) != "" {
			names = append(names, key)
		}
	}
	if len(names) == 0 {
		for _, key := range requiredNames {
			if strings.TrimSpace(key) != "" {
				names = append(names, key)
			}
		}
	}
	sort.Strings(names)

	fields := make([]map[string]interface{}, 0, len(names)+1)
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		prop := props[name]
		field := map[string]interface{}{
			"name":        name,
			"label":       registeredToolFieldLabel(name, prop),
			"type":        registeredToolAgentViewFieldTypeForName(name, prop),
			"required":    required[name],
			"description": registeredToolFieldDescription(prop),
		}
		if value, ok := args[name]; ok && !registeredToolValueMissing(value) {
			field["value"] = value
		} else if defaultValue, ok := prop["default"]; ok {
			field["defaultValue"] = defaultValue
		}
		if options := registeredToolFieldOptions(prop); len(options) > 0 {
			if normalizeRegisteredToolJSONSchemaType(fmt.Sprint(prop["type"])) == registeredToolJSONSchemaArray {
				field["type"] = agentViewFieldTypeMultiSelect.String()
			} else {
				field["type"] = agentViewFieldTypeSelect.String()
			}
			field["options"] = options
		}
		registeredToolApplySchemaConstraints(field, prop)
		if normalizeAgentViewFieldType(fmt.Sprint(field["type"])) == agentViewFieldTypeArrayTable {
			if columns := registeredToolArrayTableColumns(prop); len(columns) > 0 {
				field["columns"] = columns
			}
			if deps := registeredToolDependentRequired(registeredToolAsMap(prop["items"])); len(deps) > 0 {
				field["dependentRequired"] = deps
			}
		}
		if normalizeAgentViewFieldType(fmt.Sprint(field["type"])) == agentViewFieldTypeObjectForm {
			if columns := registeredToolObjectColumns(prop); len(columns) > 0 {
				field["columns"] = columns
			}
			if deps := registeredToolDependentRequired(prop); len(deps) > 0 {
				field["dependentRequired"] = deps
			}
		}
		if missingSet[name] {
			field["error"] = "Required before running this tool."
		}
		fields = append(fields, field)
	}
	if includeHiddenArgs {
		fields = append(fields, map[string]interface{}{
			"name":  registeredToolAgentViewArgsField,
			"label": registeredToolAgentViewArgsField,
			"type":  "hidden",
			"value": cloneMISInterfaceMap(args),
		})
	}
	return fields
}

func registeredToolAgentViewVariants(schema map[string]interface{}, args map[string]interface{}) []map[string]interface{} {
	variants := registeredToolSchemaVariants(schema)
	if len(variants) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(variants))
	for _, variant := range variants {
		fields := registeredToolAgentViewFieldsFromSchema(variant.Schema, args, nil)
		if len(fields) == 0 {
			continue
		}
		item := map[string]interface{}{
			"id":     variant.ID,
			"label":  variant.Label,
			"fields": fields,
		}
		if variant.Description != "" {
			item["description"] = variant.Description
		}
		if deps := registeredToolDependentRequired(variant.Schema); len(deps) > 0 {
			item["dependentRequired"] = deps
		}
		out = append(out, item)
	}
	return out
}

func registeredToolAgentViewFieldsFromSchema(schema map[string]interface{}, args map[string]interface{}, missing []string) []map[string]interface{} {
	return registeredToolAgentViewFieldsForSchema(schema, registeredToolRequiredFromSchema(schema), args, missing, false)
}

func buildRegisteredToolResultAgentView(tool RegisteredTool, result string) map[string]interface{} {
	title := strings.TrimSpace(tool.Name)
	if title == "" {
		title = avTr("Tool", "工具")
	}
	return map[string]interface{}{
		"type":        "result_browser",
		"id":          "tool:result:" + title,
		"title":       title + avTr(" result", " 结果"),
		"description": avTr("Tool execution completed.", "工具执行完成。"),
		"results": []map[string]interface{}{{
			"id":     "result",
			"title":  avTr("Output", "输出"),
			"status": avTr("done", "完成"),
			"data": map[string]interface{}{
				"output": result,
			},
		}},
	}
}

func storeRegisteredToolPendingApproval(toolName string, args map[string]interface{}, sessionID, policyOwnerID string, risk security.RiskAssessment) registeredToolPendingApproval {
	return storeRegisteredToolPendingApprovalForPrincipal(toolName, args, sessionID, policyOwnerID, "", risk)
}

func storeRegisteredToolPendingApprovalForPrincipal(toolName string, args map[string]interface{}, sessionID, policyOwnerID, auditPrincipalID string, risk security.RiskAssessment) registeredToolPendingApproval {
	registeredToolApprovalStore.Lock()
	defer registeredToolApprovalStore.Unlock()
	pruneRegisteredToolPendingApprovals(30 * time.Minute)
	registeredToolApprovalStore.next++
	id := fmt.Sprintf("tool-approval-%d", registeredToolApprovalStore.next)
	policyOwnerID = strings.TrimSpace(policyOwnerID)
	auditPrincipalID = strings.TrimSpace(auditPrincipalID)
	if auditPrincipalID == "" {
		auditPrincipalID = policyOwnerID
	}
	item := registeredToolPendingApproval{
		ID:               id,
		ToolName:         strings.TrimSpace(toolName),
		Args:             cloneMISInterfaceMap(args),
		SessionID:        strings.TrimSpace(sessionID),
		PolicyOwnerID:    policyOwnerID,
		AuditPrincipalID: auditPrincipalID,
		Risk:             risk,
		CreatedAt:        time.Now(),
	}
	registeredToolApprovalStore.items[id] = item
	return item
}

func getRegisteredToolPendingApproval(id string) (registeredToolPendingApproval, bool) {
	registeredToolApprovalStore.Lock()
	defer registeredToolApprovalStore.Unlock()
	item, ok := registeredToolApprovalStore.items[strings.TrimSpace(id)]
	return item, ok
}

func deleteRegisteredToolPendingApproval(id string) {
	registeredToolApprovalStore.Lock()
	defer registeredToolApprovalStore.Unlock()
	if item, ok := registeredToolApprovalStore.items[strings.TrimSpace(id)]; ok {
		if token, _ := item.Args[archiveExternalApprovalTokenField].(string); token != "" {
			delete(registeredToolApprovalStore.archiveExternalTokens, token)
		}
	}
	delete(registeredToolApprovalStore.items, strings.TrimSpace(id))
}

func removeRegisteredToolPendingApprovalPreservingExecutionToken(id string) {
	registeredToolApprovalStore.Lock()
	defer registeredToolApprovalStore.Unlock()
	delete(registeredToolApprovalStore.items, strings.TrimSpace(id))
}

// pruneRegisteredToolPendingApprovals removes stale approval entries older than maxAge.
// Must be called with registeredToolApprovalStore.Lock held.
func pruneRegisteredToolPendingApprovals(maxAge time.Duration) {
	now := time.Now()
	for id, item := range registeredToolApprovalStore.items {
		if now.Sub(item.CreatedAt) > maxAge {
			if token, _ := item.Args[archiveExternalApprovalTokenField].(string); token != "" {
				delete(registeredToolApprovalStore.archiveExternalTokens, token)
			}
			delete(registeredToolApprovalStore.items, id)
		}
	}
}

func buildRegisteredToolApprovalAgentView(approval registeredToolPendingApproval) map[string]interface{} {
	reviewData := map[string]interface{}{
		"tool":       approval.ToolName,
		"risk_level": string(approval.Risk.Level),
	}
	if approval.Risk.Reason != "" {
		reviewData["reason"] = approval.Risk.Reason
	}
	if len(approval.Risk.Factors) > 0 {
		reviewData["factors"] = append([]string(nil), approval.Risk.Factors...)
	}
	if approval.SessionID != "" {
		reviewData["session_id"] = approval.SessionID
	}
	return map[string]interface{}{
		"type":         "approval",
		"id":           "tool:approval",
		"title":        avTr("Approve tool execution", "审批工具执行"),
		"description":  avTr("Review this operation before it runs.", "请在执行前审查此操作。"),
		"approveLabel": avTr("Approve and run", "批准并执行"),
		"rejectLabel":  avTr("Reject", "拒绝"),
		"action": map[string]interface{}{
			"summary":    avTr("Run ", "运行 ") + approval.ToolName,
			"risk":       registeredToolApprovalRiskLabel(approval.Risk.Level),
			"effects":    registeredToolApprovalEffects(approval),
			"reviewData": reviewData,
			"parameters": map[string]interface{}{
				registeredToolApprovalIDField: approval.ID,
			},
		},
	}
}

func registeredToolApprovalRiskLabel(level security.RiskLevel) string {
	switch level {
	case security.RiskCritical, security.RiskHigh:
		return "high"
	case security.RiskMedium:
		return "medium"
	default:
		return "low"
	}
}

func registeredToolApprovalEffects(approval registeredToolPendingApproval) []string {
	effects := []string{"The saved tool call will execute with the parameters shown below."}
	if approval.Risk.Reason != "" {
		effects = append(effects, approval.Risk.Reason)
	}
	if approval.SessionID != "" {
		effects = append(effects, "Approval applies to this session and tool.")
	}
	return effects
}

func registeredToolRequiredNames(tool RegisteredTool) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, key := range tool.Required {
		key = strings.TrimSpace(key)
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	for _, key := range stringSliceFromAny(tool.InputSchema["required"]) {
		key = strings.TrimSpace(key)
		if key != "" && !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func registeredToolSchemaProperties(schema map[string]interface{}) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	rawProps, ok := schema["properties"]
	if !ok {
		rawProps = schema
	}
	props, ok := rawProps.(map[string]interface{})
	if !ok {
		return out
	}
	for name, raw := range props {
		if isRegisteredToolSchemaContainerProperty(name) {
			continue
		}
		if prop := registeredToolAsMap(raw); len(prop) > 0 {
			out[name] = prop
		} else {
			out[name] = map[string]interface{}{}
		}
	}
	return out
}

func registeredToolAsMap(value interface{}) map[string]interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return v
	case map[string]string:
		out := make(map[string]interface{}, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var out map[string]interface{}
		if err := json.Unmarshal(data, &out); err != nil {
			return nil
		}
		return out
	}
}

func registeredToolValueMissing(value interface{}) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	case []interface{}:
		return len(v) == 0
	case map[string]interface{}:
		return len(v) == 0
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array, reflect.Map:
			return rv.Len() == 0
		}
		return false
	}
}

func registeredToolAgentViewFieldType(prop map[string]interface{}) string {
	return registeredToolAgentViewFieldTypeForName("", prop)
}

func registeredToolAgentViewFieldTypeForName(name string, prop map[string]interface{}) string {
	switch normalizeRegisteredToolJSONSchemaType(fmt.Sprint(prop["type"])) {
	case registeredToolJSONSchemaNumber, registeredToolJSONSchemaInteger:
		return agentViewFieldTypeNumber.String()
	case registeredToolJSONSchemaBoolean:
		return agentViewFieldTypeBoolean.String()
	case registeredToolJSONSchemaArray:
		if len(registeredToolArrayTableColumns(prop)) > 0 {
			return agentViewFieldTypeArrayTable.String()
		}
		if len(registeredToolFieldOptions(prop)) > 0 {
			return agentViewFieldTypeMultiSelect.String()
		}
		return agentViewFieldTypeTextarea.String()
	case registeredToolJSONSchemaObject:
		if len(registeredToolObjectColumns(prop)) > 0 {
			return agentViewFieldTypeObjectForm.String()
		}
		return agentViewFieldTypeTextarea.String()
	default:
		if registeredToolStringFieldIsDirectory(name, prop) {
			return agentViewFieldTypeDirectory.String()
		}
		if registeredToolStringFieldIsPathLike(name, prop) {
			return agentViewFieldTypeFile.String()
		}
		return agentViewFieldTypeText.String()
	}
}

func registeredToolStringFieldIsDirectory(name string, prop map[string]interface{}) bool {
	for _, key := range []string{"x-agent-view", "x_agent_view", "ui:widget", "ui_widget", "format"} {
		value := strings.ToLower(strings.TrimSpace(fmt.Sprint(prop[key])))
		if value == "directory" || value == "folder" || value == "dir" {
			return true
		}
	}
	text := strings.ToLower(strings.Join([]string{name, registeredToolFieldLabel(name, prop), registeredToolFieldDescription(prop)}, " "))
	return strings.Contains(text, "working directory") || strings.Contains(text, "working dir") || strings.Contains(text, "workdir") || agentViewTextHasWord(text, "directory") || agentViewTextHasWord(text, "folder") || agentViewTextHasWord(text, "cwd") || agentViewTextHasWord(text, "dir")
}

func registeredToolStringFieldIsPathLike(name string, prop map[string]interface{}) bool {
	text := strings.ToLower(strings.Join([]string{name, registeredToolFieldLabel(name, prop), registeredToolFieldDescription(prop), nonEmptyStringFromAny(prop["format"])}, " "))
	return strings.Contains(text, "filepath") || strings.Contains(text, "file path") || agentViewTextHasWord(text, "file") || agentViewTextHasWord(text, "path")
}

func registeredToolFieldLabel(name string, prop map[string]interface{}) string {
	for _, key := range []string{"title", "label"} {
		value := strings.TrimSpace(fmt.Sprint(prop[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return name
}

func registeredToolFieldDescription(prop map[string]interface{}) string {
	value := strings.TrimSpace(fmt.Sprint(prop["description"]))
	if value == "<nil>" {
		return ""
	}
	return value
}

func registeredToolFieldOptions(prop map[string]interface{}) []map[string]interface{} {
	values := stringSliceFromAny(prop["enum"])
	if len(values) == 0 && normalizeRegisteredToolJSONSchemaType(fmt.Sprint(prop["type"])) == registeredToolJSONSchemaArray {
		if itemValues := stringSliceFromAny(registeredToolAsMap(prop["items"])["enum"]); len(itemValues) > 0 {
			values = itemValues
		}
	}
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, map[string]interface{}{"label": value, "value": value})
	}
	return out
}

func registeredToolResourceOptions(prop map[string]interface{}) []map[string]interface{} {
	for _, key := range []string{"x-options", "x_options", "options", "resources", "candidates"} {
		if options := registeredToolResourceOptionsFromAny(prop[key]); len(options) > 0 {
			return options
		}
	}
	values := stringSliceFromAny(prop["enum"])
	if len(values) == 0 && normalizeRegisteredToolJSONSchemaType(fmt.Sprint(prop["type"])) == registeredToolJSONSchemaArray {
		values = stringSliceFromAny(registeredToolAsMap(prop["items"])["enum"])
	}
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, map[string]interface{}{"label": value, "value": value})
		}
	}
	return out
}

func registeredToolResourceOptionsFromAny(raw interface{}) []map[string]interface{} {
	items := registeredToolSliceItems(raw)
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		m := registeredToolAsMap(item)
		if len(m) == 0 {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				out = append(out, map[string]interface{}{"label": text, "value": text})
			}
			continue
		}
		value := firstNonEmptyMISAgentView(nonEmptyStringFromAny(m["value"]), nonEmptyStringFromAny(m["id"]), nonEmptyStringFromAny(m["key"]), nonEmptyStringFromAny(m["name"]))
		if value == "" {
			continue
		}
		option := map[string]interface{}{
			"value": value,
			"label": firstNonEmptyMISAgentView(nonEmptyStringFromAny(m["label"]), nonEmptyStringFromAny(m["title"]), nonEmptyStringFromAny(m["name"]), value),
		}
		if description := nonEmptyStringFromAny(m["description"]); description != "" {
			option["description"] = description
		}
		if status := nonEmptyStringFromAny(m["status"]); status != "" {
			option["status"] = status
		}
		if data := registeredToolAsMap(m["data"]); len(data) > 0 {
			option["data"] = data
		}
		out = append(out, option)
	}
	return out
}

func registeredToolSchemaStringList(schema map[string]interface{}, keys ...string) []string {
	for _, key := range keys {
		if values := stringSliceFromAny(schema[key]); len(values) > 0 {
			return values
		}
	}
	return nil
}

func registeredToolMapperTargetFields(prop map[string]interface{}) []map[string]interface{} {
	for _, key := range []string{"x-target-fields", "x_target_fields", "targetFields", "target_fields"} {
		if fields := registeredToolMapperTargetFieldsFromAny(prop[key]); len(fields) > 0 {
			return fields
		}
	}
	props := registeredToolSchemaProperties(prop)
	required := stringSet(registeredToolRequiredFromSchema(prop))
	if len(props) == 0 {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	fields := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		fieldProp := props[name]
		field := map[string]interface{}{
			"name":     name,
			"label":    registeredToolFieldLabel(name, fieldProp),
			"type":     registeredToolAgentViewFieldTypeForName(name, fieldProp),
			"required": required[name],
		}
		if description := registeredToolFieldDescription(fieldProp); description != "" {
			field["description"] = description
		}
		fields = append(fields, field)
	}
	return fields
}

func registeredToolMapperTargetFieldsFromAny(raw interface{}) []map[string]interface{} {
	items := registeredToolSliceItems(raw)
	if len(items) == 0 {
		return nil
	}
	fields := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		m := registeredToolAsMap(item)
		if len(m) == 0 {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" && text != "<nil>" {
				fields = append(fields, map[string]interface{}{"name": text, "label": text})
			}
			continue
		}
		name := firstNonEmptyMISAgentView(nonEmptyStringFromAny(m["name"]), nonEmptyStringFromAny(m["id"]), nonEmptyStringFromAny(m["key"]))
		if name == "" {
			continue
		}
		field := map[string]interface{}{
			"name":     name,
			"label":    firstNonEmptyMISAgentView(nonEmptyStringFromAny(m["label"]), nonEmptyStringFromAny(m["title"]), name),
			"type":     firstNonEmptyMISAgentView(nonEmptyStringFromAny(m["type"]), "text"),
			"required": boolFromAny(m["required"]),
		}
		if description := nonEmptyStringFromAny(m["description"]); description != "" {
			field["description"] = description
		}
		fields = append(fields, field)
	}
	return fields
}

func registeredToolArrayTableColumns(prop map[string]interface{}) []map[string]interface{} {
	items := registeredToolAsMap(prop["items"])
	return registeredToolColumnsFromProperties(registeredToolSchemaProperties(items), stringSet(registeredToolRequiredFromSchema(items)))
}

func registeredToolObjectColumns(prop map[string]interface{}) []map[string]interface{} {
	return registeredToolColumnsFromProperties(registeredToolSchemaProperties(prop), stringSet(registeredToolRequiredFromSchema(prop)))
}

func registeredToolColumnsFromProperties(props map[string]map[string]interface{}, required map[string]bool) []map[string]interface{} {
	if len(props) == 0 {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	columns := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		itemProp := props[name]
		column := map[string]interface{}{
			"name":     name,
			"label":    registeredToolFieldLabel(name, itemProp),
			"type":     registeredToolTableColumnTypeForName(name, itemProp),
			"required": required[name],
		}
		if options := registeredToolFieldOptions(itemProp); len(options) > 0 {
			column["type"] = "select"
			column["options"] = options
		}
		registeredToolApplySchemaConstraints(column, itemProp)
		columns = append(columns, column)
	}
	return columns
}

func registeredToolApplySchemaConstraints(target map[string]interface{}, prop map[string]interface{}) {
	if len(target) == 0 || len(prop) == 0 {
		return
	}
	if min, ok := numberFromAny(prop["minimum"]); ok {
		target["min"] = min
	}
	if max, ok := numberFromAny(prop["maximum"]); ok {
		target["max"] = max
	}
	if constValue, ok := prop["const"]; ok {
		target["constValue"] = constValue
	}
	if exclusiveMin, ok := registeredToolExclusiveBoundary(prop["exclusiveMinimum"], prop["minimum"]); ok {
		target["exclusiveMin"] = exclusiveMin
	}
	if exclusiveMax, ok := registeredToolExclusiveBoundary(prop["exclusiveMaximum"], prop["maximum"]); ok {
		target["exclusiveMax"] = exclusiveMax
	}
	if multipleOf, ok := numberFromAny(prop["multipleOf"]); ok {
		target["step"] = multipleOf
	}
	if minItems, ok := numberFromAny(prop["minItems"]); ok {
		target["minItems"] = minItems
	}
	if maxItems, ok := numberFromAny(prop["maxItems"]); ok {
		target["maxItems"] = maxItems
	}
	if boolFromAny(prop["uniqueItems"]) {
		target["uniqueItems"] = true
	}
	if boolFromAny(prop["readOnly"]) {
		target["readOnly"] = true
	}
	if boolFromAny(prop["writeOnly"]) || isSensitiveSchemaFormat(prop) {
		target["sensitive"] = true
	}
	if minLength, ok := numberFromAny(prop["minLength"]); ok {
		target["minLength"] = minLength
	}
	if maxLength, ok := numberFromAny(prop["maxLength"]); ok {
		target["maxLength"] = maxLength
	}
	if pattern := nonEmptyStringFromAny(prop["pattern"]); pattern != "" {
		target["pattern"] = pattern
	}
	if format := nonEmptyStringFromAny(prop["format"]); format != "" {
		target["format"] = format
	}
}

func isSensitiveSchemaFormat(prop map[string]interface{}) bool {
	return normalizeAgentViewSchemaFormatKind(nonEmptyStringFromAny(prop["format"])).IsSensitive()
}

func registeredToolRequiredFromSchema(schema map[string]interface{}) []string {
	return stringSliceFromAny(schema["required"])
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func numberFromAny(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return 0, false
		}
		var n json.Number = json.Number(text)
		value, err := n.Float64()
		return value, err == nil
	default:
		return 0, false
	}
}

func boolFromAny(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, _ := coerceAgentViewBoolToken(v)
		return parsed
	default:
		return false
	}
}

func nonEmptyStringFromAny(value interface{}) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return ""
	}
	return text
}

func registeredToolInferJSONType(value interface{}) string {
	switch value.(type) {
	case string:
		return "string"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return "number"
	case bool:
		return "boolean"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			return "array"
		case reflect.Map, reflect.Struct:
			return "object"
		default:
			return ""
		}
	}
}

func registeredToolCollectionLen(value interface{}) (int, bool) {
	if value == nil {
		return 0, false
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len(), true
	default:
		return 0, false
	}
}

func registeredToolSliceItems(value interface{}) []interface{} {
	if value == nil {
		return nil
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}
	out := make([]interface{}, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out = append(out, rv.Index(i).Interface())
	}
	return out
}

func registeredToolArrayItemsUnique(value interface{}) bool {
	seen := map[string]bool{}
	for _, item := range registeredToolSliceItems(value) {
		key := stableSchemaValueKey(item)
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func stableSchemaValueKey(value interface{}) string {
	data, err := json.Marshal(value)
	if err == nil {
		return string(data)
	}
	return fmt.Sprint(value)
}

func formatConstraintNumber(value float64) string {
	text := fmt.Sprintf("%.6f", value)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" || text == "-" {
		return "0"
	}
	return text
}

func registeredToolFormatValidationError(label, text, format string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	switch formatKind := normalizeAgentViewSchemaFormatKind(format); {
	case formatKind == agentViewSchemaFormatEmail:
		if regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`).MatchString(text) {
			return ""
		}
		return fmt.Sprintf("%s must be a valid email", label)
	case formatKind.IsURLLike():
		parsed, err := url.ParseRequestURI(text)
		if err == nil && parsed.Scheme != "" {
			return ""
		}
		return fmt.Sprintf("%s must be a valid URL", label)
	case formatKind == agentViewSchemaFormatUUID:
		if regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(text) {
			return ""
		}
		return fmt.Sprintf("%s must be a valid UUID", label)
	case formatKind == agentViewSchemaFormatDate:
		if _, err := time.Parse("2006-01-02", text); err == nil {
			return ""
		}
		return fmt.Sprintf("%s must be a valid date", label)
	case formatKind.IsDateTime():
		if _, err := time.Parse(time.RFC3339, text); err == nil {
			return ""
		}
		if _, err := time.Parse("2006-01-02T15:04", text); err == nil {
			return ""
		}
		return fmt.Sprintf("%s must be a valid date and time", label)
	default:
		return ""
	}
}

func registeredToolTableColumnType(prop map[string]interface{}) string {
	return registeredToolTableColumnTypeForName("", prop)
}

func registeredToolTableColumnTypeForName(name string, prop map[string]interface{}) string {
	switch normalizeRegisteredToolJSONSchemaType(fmt.Sprint(prop["type"])) {
	case registeredToolJSONSchemaNumber, registeredToolJSONSchemaInteger:
		return agentViewFieldTypeNumber.String()
	case registeredToolJSONSchemaBoolean:
		return agentViewFieldTypeBoolean.String()
	case registeredToolJSONSchemaDate:
		return agentViewFieldTypeDate.String()
	default:
		if registeredToolStringFieldIsDirectory(name, prop) {
			return agentViewFieldTypeDirectory.String()
		}
		return agentViewFieldTypeText.String()
	}
}

func coerceRegisteredToolValue(tool *RegisteredTool, key string, value interface{}, allArgs map[string]interface{}) interface{} {
	if tool == nil {
		return value
	}
	prop := registeredToolSchemaProperties(tool.InputSchema)[key]
	if len(prop) == 0 {
		if selected := registeredToolSelectedVariant(allArgs, registeredToolSchemaVariants(tool.InputSchema)); selected != nil {
			prop = registeredToolSchemaProperties(selected.Schema)[key]
		}
	}
	typ := normalizeRegisteredToolJSONSchemaType(fmt.Sprint(prop["type"]))
	if typ.IsObjectLike() && value != nil {
		text, ok := value.(string)
		if !ok {
			return value
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return value
		}
		var parsed interface{}
		if err := json.Unmarshal([]byte(text), &parsed); err == nil {
			return parsed
		}
	}
	return value
}
