package main

import (
	"context"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type executionLayer string

const (
	executionLayerDirect executionLayer = "direct"
	executionLayerFull   executionLayer = "full"
	executionLayerLight  executionLayer = "light"
)

type ExecutionProfile struct {
	Layer                string
	TaskType             string
	PromptProfile        string
	Confidence           float64
	Reason               string
	RequiredCapabilities []string
	DirectToolName       string
	ToolBudget           int
	// SchemaTokenBudget is the trusted host cap on CatalogRenderer schema
	// tokens for a managed plan. Zero means unlimited. It is independent of
	// ToolBudget and must never be inferred from the selection count.
	SchemaTokenBudget int
	IterationBudget   int
}

func (p ExecutionProfile) IsLight() bool {
	return strings.EqualFold(strings.TrimSpace(p.Layer), string(executionLayerLight))
}

// PromptIsLight is the loop-authorizer view of this profile. An empty prompt
// profile follows the execution layer so a light lookup cannot skip the
// light-safe grant filter.
func (p ExecutionProfile) PromptIsLight() bool {
	pp := strings.TrimSpace(p.PromptProfile)
	if pp == "" {
		return p.IsLight()
	}
	return agent.NormalizePromptProfile(pp).IsLight()
}

func (p ExecutionProfile) IsDirect() bool {
	return strings.EqualFold(strings.TrimSpace(p.Layer), string(executionLayerDirect))
}

func fullExecutionProfile(reason string) ExecutionProfile {
	return ExecutionProfile{
		Layer:           string(executionLayerFull),
		TaskType:        "general",
		PromptProfile:   "full",
		Confidence:      1,
		Reason:          reason,
		ToolBudget:      0,
		IterationBudget: 0,
	}
}

func classifyIMExecutionProfile(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool) ExecutionProfile {
	return classifyIMExecutionProfileWithSemantic(msg, workflowAgentLoop, isAskUserResponse, nil)
}

func (h *IMMessageHandler) classifyIMExecutionProfile(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool) ExecutionProfile {
	profile, _ := h.classifyIMExecutionProfileAndSemantic(msg, workflowAgentLoop, isAskUserResponse)
	return profile
}

func (h *IMMessageHandler) classifyIMExecutionProfileAndSemantic(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool) (ExecutionProfile, *intent.ClassificationResult) {
	return h.classifyIMExecutionProfileAndSemanticContext(context.Background(), msg, workflowAgentLoop, isAskUserResponse, nil)
}

// classifyIMExecutionProfileAndSemanticContext preserves the authoritative
// semantic verdict used by capability materialization, while making its L3
// work part of the enclosing turn's cancellation tree.
func (h *IMMessageHandler) classifyIMExecutionProfileAndSemanticContext(ctx context.Context, msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool, recentHistory []string) (ExecutionProfile, *intent.ClassificationResult) {
	structuralProfile, structurallyForced := hardStructuralFullExecutionProfile(msg, workflowAgentLoop, isAskUserResponse)
	// Keep the pre-semantic deterministic clock shortcut only for deployments
	// without UIC. Once UIC exists, LabelCurrentTime is capability-managed and
	// must go through the same catalog/grant path as the other managed families.
	if h.getUnifiedClassifier() == nil {
		if profile, ok := localCurrentTimeExecutionProfile(msg.Text, h.executionContractForRegisteredToolName); ok {
			return profile, nil
		}
		if structurallyForced {
			return structuralProfile, nil
		}
	}
	// Classification must still happen for a structurally-full turn.  The
	// execution profile controls budgets, while the semantic result controls
	// capability selection; returning early for attachments/background/workflow
	// shapes used to leave a governed request without SemanticIntent and let it
	// re-enter the legacy router.
	var semantic *intent.ClassificationResult
	if uic := h.getUnifiedClassifier(); uic != nil {
		result := uic.ClassifyContext(ctx, intent.MessageContext{
			Text:          semanticUserIntentText(msg.Text),
			UserID:        msg.UserID,
			RecentHistory: recentHistory,
		})
		semantic = &result
	}
	normalizeSemanticClassificationForTurn(semantic)
	if structurallyForced {
		return structuralProfile, semantic
	}
	if semantic != nil && imSemanticIntentIsManaged(*semantic) {
		return classifyIMExecutionProfileWithSemanticAndContracts(msg, workflowAgentLoop, isAskUserResponse, semantic, h.executionContractForRegisteredToolName), semantic
	}
	if profile, forced := lengthFullExecutionProfile(msg); forced {
		return profile, semantic
	}
	// ACP Mode B short turns: force light without embedding/UIC. Avoids
	// multi-second tool-routing fusion and oversized full prompts for chatty
	// editor messages while real coding asks (paths/URLs/fences/long text)
	// still hit full via structural/length gates above.
	if acpPreferLightProfile(msg) {
		// Keep the light profile, not the UIC result: a short ACP turn must
		// not become a closed managed grant. Loop start leftover then treats
		// the unset SemanticIntent as a chat leftover, not CoreToolNames+UIC.
		return ExecutionProfile{
			Layer:                string(executionLayerLight),
			TaskType:             "general",
			PromptProfile:        "light",
			Confidence:           1,
			Reason:               "acp-mode-b short programming turn",
			RequiredCapabilities: []string{"current_data", "time", "web", "fetch", "status", "files"},
			ToolBudget:           8,
			IterationBudget:      3,
		}, nil
	}
	profile := classifyIMExecutionProfileWithSemanticAndContracts(msg, workflowAgentLoop, isAskUserResponse, semantic, h.executionContractForRegisteredToolName)
	return profile, semantic
}

func classifyIMExecutionProfileWithSemantic(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool, semantic *intent.ClassificationResult) ExecutionProfile {
	return classifyIMExecutionProfileWithSemanticAndContracts(msg, workflowAgentLoop, isAskUserResponse, semantic, nil)
}

func classifyIMExecutionProfileWithSemanticAndContracts(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool, semantic *intent.ClassificationResult, contractForTool func(string) ToolExecutionContract) ExecutionProfile {
	if profile, forced := hardStructuralFullExecutionProfile(msg, workflowAgentLoop, isAskUserResponse); forced {
		return profile
	}
	normalizeSemanticClassificationForTurn(semantic)
	// A supplied semantic result is authoritative for this decision. Do not
	// reintroduce a wording-based direct route before checking whether that
	// label belongs to a capability-managed family.
	if semantic != nil && imSemanticIntentIsManaged(*semantic) {
		return executionProfileFromSemanticIntent(semantic, contractForTool)
	}
	if profile, ok := localCurrentTimeExecutionProfile(msg.Text, contractForTool); ok {
		return profile
	}
	if profile, forced := lengthFullExecutionProfile(msg); forced {
		return profile
	}
	return executionProfileFromSemanticIntent(semantic, contractForTool)
}

func structuralFullExecutionProfile(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool) (ExecutionProfile, bool) {
	if profile, forced := hardStructuralFullExecutionProfile(msg, workflowAgentLoop, isAskUserResponse); forced {
		return profile, true
	}
	return lengthFullExecutionProfile(msg)
}

func hardStructuralFullExecutionProfile(msg IMUserMessage, workflowAgentLoop, isAskUserResponse bool) (ExecutionProfile, bool) {
	text := strings.TrimSpace(msg.Text)
	switch {
	case expertDefForUserID(msg.UserID) != nil:
		// Expert sessions always run the full profile: the light prompt carries
		// a hard "do not inspect files / do not manage tasks" fence and a
		// generic persona that would contradict the expert's system prompt.
		return fullExecutionProfile("expert session"), true
	case text == "":
		return fullExecutionProfile("empty message"), true
	case workflowAgentLoop:
		return fullExecutionProfile("workflow agent loop"), true
	case isAskUserResponse:
		return fullExecutionProfile("ask_user continuation"), true
	case msg.IsBackground:
		return fullExecutionProfile("background task"), true
	case len(msg.Attachments) > 0:
		return fullExecutionProfile("attachments present"), true
	case hasStructuralFullExecutionSignal(text):
		return fullExecutionProfile("structural execution signal"), true
	default:
		return ExecutionProfile{}, false
	}
}

func lengthFullExecutionProfile(msg IMUserMessage) (ExecutionProfile, bool) {
	if utf8.RuneCountInString(strings.TrimSpace(msg.Text)) > 40 {
		return fullExecutionProfile("message too long for light profile"), true
	}
	return ExecutionProfile{}, false
}

func executionProfileFromSemanticIntent(result *intent.ClassificationResult, contractForTool func(string) ToolExecutionContract) ExecutionProfile {
	if result == nil {
		return fullExecutionProfile("semantic classifier unavailable")
	}
	// Sub-floor search/live_data is a chat turn (gate 7). Do not promote it to
	// a full degraded profile or a managed lookup budget; that is how a typo
	// like 「北京天所」 paid for web tools after UIC kept the hint.
	if semanticNeedsChatProjection(*result) {
		reason := "semantic lookup hint below floor"
		if semanticReadOnlyUnderstandFamily(*result) {
			reason = "semantic understand hint below floor"
		}
		return ExecutionProfile{
			Layer:           string(executionLayerLight),
			TaskType:        "general",
			PromptProfile:   "light",
			Confidence:      result.Confidence,
			Reason:          reason,
			ToolBudget:      8,
			IterationBudget: 3,
		}
	}
	readOnlyHint := semanticReadOnlyGovernedHint(*result)
	if result.Degraded && !readOnlyHint {
		return fullExecutionProfile("semantic classifier degraded")
	}
	if result.WorkflowType != "" {
		return fullExecutionProfile("semantic workflow intent")
	}
	if !semanticClassificationMeetsResolverFloor(*result) && !semanticClassificationPlansBelowResolverFloor(*result) {
		return fullExecutionProfile("semantic confidence below light threshold")
	}
	// Capability-managed families are materialized only through the semantic
	// catalog/planner. In particular, an old UIC ToolNames projection must not
	// turn a governed outcome into a direct name-based execution path.
	if managed, unmapped := imSemanticIntentCoverage(*result); managed {
		if unmapped != "" {
			// The semantic router will fail closed with an explicit coverage
			// error. Keep a full profile here so an incomplete migration never
			// narrows a multi-capability request to the light lookup loop.
			return fullExecutionProfile("semantic capability migration coverage incomplete")
		}
		// The capability surface remains grant-bound even on a light turn. The
		// profile only budgets the agent loop; semanticPlanForTurn replaces its
		// tool list before the model sees it, so this does not reopen the legacy
		// name-router or direct execution path.
		if semanticIntentRequiresFullProfile(*result) {
			return fullExecutionProfile("semantic capability-managed mutating intent")
		}
		if result.Primary == intent.LabelSearch || result.Primary == intent.LabelLiveData {
			return ExecutionProfile{
				Layer:                string(executionLayerLight),
				TaskType:             string(result.Primary),
				PromptProfile:        "light",
				Confidence:           result.Confidence,
				Reason:               "semantic capability-managed lookup",
				RequiredCapabilities: []string{"information.search.web"},
				ToolBudget:           1,
				IterationBudget:      3,
			}
		}
		return fullExecutionProfile("semantic capability-managed intent")
	}
	if toolName, contract := directToolFromSemanticResult(*result, contractForTool); toolName != "" {
		return ExecutionProfile{
			Layer:                string(executionLayerDirect),
			TaskType:             "direct_tool",
			PromptProfile:        "none",
			Confidence:           result.Confidence,
			Reason:               "semantic direct tool intent",
			RequiredCapabilities: contract.Capabilities,
			DirectToolName:       toolName,
			ToolBudget:           1,
			IterationBudget:      0,
		}
	}
	switch result.Primary {
	case intent.LabelLiveData:
		return ExecutionProfile{
			Layer:                string(executionLayerLight),
			TaskType:             string(result.Primary),
			PromptProfile:        "light",
			Confidence:           result.Confidence,
			Reason:               "semantic low-complexity intent",
			RequiredCapabilities: []string{"current_data", "time", "web", "fetch"},
			ToolBudget:           8,
			IterationBudget:      3,
		}
	default:
		return fullExecutionProfile("semantic intent requires full agent")
	}
}

func semanticIntentRequiresFullProfile(result intent.ClassificationResult) bool {
	for _, label := range result.Labels() {
		for _, tmpl := range imSemanticIntentRuleSet[label] {
			if semanticCapabilityRequiresFullProfile(string(tmpl.Capability)) {
				return true
			}
		}
	}
	return false
}

func semanticCapabilityRequiresFullProfile(capability string) bool {
	switch strings.TrimSpace(capability) {
	case "information.search.web", "information.current_time", "document.read.local", "visual.render.live_data",
		"visual.capture.desktop",
		string(tool.CapabilityFSReadLocal), string(tool.CapabilityRepoInspectVCS),
		string(tool.CapabilityInformationFetchWeb), string(tool.CapabilityAudioTranscribeSpeech),
		string(tool.CapabilitySecurityAuditRead), string(tool.CapabilityKnowledgeReadLocal):
		return false
	default:
		return capability != ""
	}
}

var executionProfileLocalPathPattern = regexp.MustCompile(`(?i)([a-z]:[\\/]|\.{1,2}[\\/]|/[\w.-])`)

func hasStructuralFullExecutionSignal(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return executionProfileLocalPathPattern.MatchString(lower) ||
		strings.Contains(lower, "```") ||
		strings.Contains(lower, "http://") ||
		strings.Contains(lower, "https://")
}

func directToolFromSemanticResult(result intent.ClassificationResult, contractForTool func(string) ToolExecutionContract) (string, ToolExecutionContract) {
	if result.Confidence < 0.95 || len(result.ToolNames) != 1 {
		return "", ToolExecutionContract{}
	}
	toolName := strings.TrimSpace(result.ToolNames[0])
	contract := executionContractForToolName(toolName, contractForTool)
	if contract.Explicit && contract.SupportsDirect && contract.Deterministic {
		return toolName, contract
	}
	return "", ToolExecutionContract{}
}

func localCurrentTimeExecutionProfile(text string, contractForTool func(string) ToolExecutionContract) (ExecutionProfile, bool) {
	if !isLocalCurrentTimeQuery(text) {
		return ExecutionProfile{}, false
	}
	toolName := "current_datetime"
	contract := executionContractForToolName(toolName, contractForTool)
	if !contract.Explicit || !contract.SupportsDirect || !contract.Deterministic {
		return ExecutionProfile{}, false
	}
	return ExecutionProfile{
		Layer:                string(executionLayerDirect),
		TaskType:             "direct_tool",
		PromptProfile:        "none",
		Confidence:           1,
		Reason:               "local deterministic current time intent",
		RequiredCapabilities: contract.Capabilities,
		DirectToolName:       toolName,
		ToolBudget:           1,
		IterationBudget:      0,
	}, true
}

func isLocalCurrentTimeQuery(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	compact := strings.NewReplacer(" ", "", "?", "", "？", "", "。", "", "！", "", "!", "").Replace(lower)
	if strings.Contains(compact, "时间复杂度") || strings.Contains(lower, "time complexity") {
		return false
	}
	if strings.Contains(compact, "几点") && (strings.Contains(compact, "会议") || strings.Contains(compact, "开始")) {
		return false
	}
	cnSignals := []string{
		"现在几点", "现在时间", "当前时间", "当前日期", "今天几号", "今天周几", "今天星期几",
		"几点了", "几点啦", "现在几点钟", "啥时候了",
	}
	for _, signal := range cnSignals {
		if strings.Contains(compact, signal) {
			return true
		}
	}
	enSignals := []string{
		"what time is it", "current time", "local time", "date today", "today's date",
		"what day is it", "current date", "date and time",
	}
	for _, signal := range enSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

type ToolExecutionContract struct {
	Name                  string
	Capabilities          []string
	Deterministic         bool
	SupportsDirect        bool
	RequiresAgentPlanning bool
	AvgLatencyMS          int
	Explicit              bool
}

func filterToolsForExecutionProfile(tools []map[string]interface{}, profile ExecutionProfile) []map[string]interface{} {
	// Light execution layer OR adaptive light prompt profile both need a
	// reduced tool surface so tools match the light system prompt.
	if (!profile.IsLight() && !isLightPromptProfile(profile.PromptProfile)) || len(tools) == 0 {
		return tools
	}
	filterProfile := profile
	if !filterProfile.IsLight() && isLightPromptProfile(filterProfile.PromptProfile) {
		// Soft light: full layer but light prompt — still restrict tools.
		filterProfile = softLightExecutionProfile(filterProfile)
	}
	budget := filterProfile.ToolBudget
	if budget <= 0 {
		budget = 8
	}
	filtered := make([]map[string]interface{}, 0, budget)
	seen := make(map[string]bool, budget)
	for _, def := range tools {
		contract := executionContractForTool(def)
		if !contract.Explicit {
			continue
		}
		if !contractAllowedForLight(contract) || !contractMatchesExecutionProfile(contract, filterProfile) || seen[contract.Name] {
			continue
		}
		filtered = append(filtered, def)
		seen[contract.Name] = true
		if len(filtered) >= budget {
			break
		}
	}
	if len(filtered) == 0 {
		return tools
	}
	return filtered
}

func isLightPromptProfile(s string) bool {
	return agent.NormalizePromptProfile(s).IsLight()
}

// softLightExecutionProfile applies default light tool budgets/capabilities when
// only PromptProfile=light is set (adaptive prompt on a full execution layer).
func softLightExecutionProfile(p ExecutionProfile) ExecutionProfile {
	out := p
	out.Layer = string(executionLayerLight)
	out.PromptProfile = "light"
	if out.ToolBudget <= 0 {
		out.ToolBudget = 8
	}
	if len(out.RequiredCapabilities) == 0 {
		out.RequiredCapabilities = []string{"current_data", "time", "web", "fetch", "status"}
	}
	if strings.TrimSpace(out.Reason) == "" {
		out.Reason = "adaptive light prompt profile"
	}
	return out
}

func executionContractForTool(def map[string]interface{}) ToolExecutionContract {
	name := extractToolName(def)
	if raw, ok := def["x_execution_contract"].(map[string]interface{}); ok {
		return executionContractFromMetadata(name, raw)
	}
	return inferredExecutionContract(name)
}

func executionContractForToolName(name string, contractForTool func(string) ToolExecutionContract) ToolExecutionContract {
	name = strings.TrimSpace(name)
	if contractForTool != nil {
		contract := contractForTool(name)
		if strings.TrimSpace(contract.Name) == "" {
			contract.Name = name
		}
		return contract
	}
	return inferredExecutionContract(name)
}

func (h *IMMessageHandler) executionContractForRegisteredToolName(name string) ToolExecutionContract {
	name = strings.TrimSpace(name)
	if h == nil || h.registry == nil || name == "" {
		return inferredExecutionContract(name)
	}
	if tool, ok := h.registry.Get(name); ok && tool != nil && len(tool.ExecutionContract) > 0 {
		return executionContractFromMetadata(name, tool.ExecutionContract)
	}
	return inferredExecutionContract(name)
}

func executionContractFromMetadata(name string, raw map[string]interface{}) ToolExecutionContract {
	contract := inferredExecutionContract(name)
	contract.Explicit = true
	if caps, ok := raw["capabilities"].([]string); ok {
		contract.Capabilities = normalizedExecutionCapabilities(caps)
	} else if caps, ok := raw["capabilities"].([]interface{}); ok {
		values := make([]string, 0, len(caps))
		for _, item := range caps {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				values = append(values, s)
			}
		}
		contract.Capabilities = normalizedExecutionCapabilities(values)
	}
	if v, ok := raw["deterministic"].(bool); ok {
		contract.Deterministic = v
	}
	if v, ok := raw["supports_direct"].(bool); ok {
		contract.SupportsDirect = v
	}
	if v, ok := raw["requires_agent_planning"].(bool); ok {
		contract.RequiresAgentPlanning = v
	}
	switch v := raw["avg_latency_ms"].(type) {
	case int:
		contract.AvgLatencyMS = v
	case float64:
		contract.AvgLatencyMS = int(v)
	}
	return contract
}

func inferredExecutionContract(name string) ToolExecutionContract {
	contract := ToolExecutionContract{Name: strings.TrimSpace(name), RequiresAgentPlanning: true}
	switch contract.Name {
	case "manage_skill":
		contract.Capabilities = []string{"skill"}
		contract.RequiresAgentPlanning = false
	case "web_search":
		contract.Capabilities = []string{"web", "current_data"}
		contract.SupportsDirect = true
		contract.RequiresAgentPlanning = false
	case "web_fetch", "download_file":
		contract.Capabilities = []string{"web", "fetch", "download"}
		contract.SupportsDirect = true
		contract.RequiresAgentPlanning = false
	case "call_mcp_tool":
		contract.Capabilities = []string{"mcp", "external_tool"}
		contract.RequiresAgentPlanning = false
	case "async_wait":
		contract.Capabilities = []string{"async_status"}
		contract.Deterministic = true
		contract.SupportsDirect = true
		contract.RequiresAgentPlanning = false
	case "current_datetime":
		contract.Capabilities = []string{"time"}
		contract.Deterministic = true
		contract.SupportsDirect = true
		contract.RequiresAgentPlanning = false
		contract.AvgLatencyMS = 5
	default:
		contract.Capabilities = []string{"general"}
	}
	return contract
}

func defaultExplicitExecutionContractMetadata(name string) map[string]interface{} {
	contract := inferredExecutionContract(name)
	if strings.TrimSpace(contract.Name) == "" || len(contract.Capabilities) == 0 || contract.RequiresAgentPlanning {
		return nil
	}
	return map[string]interface{}{
		"capabilities":            append([]string(nil), contract.Capabilities...),
		"deterministic":           contract.Deterministic,
		"supports_direct":         contract.SupportsDirect,
		"requires_agent_planning": contract.RequiresAgentPlanning,
		"avg_latency_ms":          contract.AvgLatencyMS,
	}
}

func contractAllowedForLight(contract ToolExecutionContract) bool {
	if contract.Name == "" || contract.RequiresAgentPlanning {
		return false
	}
	for _, cap := range contract.Capabilities {
		switch normalizeExecutionCapability(cap) {
		case "skill", "web", "current_data", "fetch", "mcp", "external_tool", "async_status", "time", "status":
			return true
		}
	}
	return false
}

func contractMatchesExecutionProfile(contract ToolExecutionContract, profile ExecutionProfile) bool {
	if len(profile.RequiredCapabilities) == 0 {
		return true
	}
	required := make(map[string]bool, len(profile.RequiredCapabilities))
	for _, cap := range profile.RequiredCapabilities {
		cap = normalizeExecutionCapability(cap)
		if cap != "" {
			required[cap] = true
		}
	}
	if len(required) == 0 {
		return true
	}
	for _, cap := range contract.Capabilities {
		if required[normalizeExecutionCapability(cap)] {
			return true
		}
	}
	return false
}

func normalizedExecutionCapabilities(caps []string) []string {
	if len(caps) == 0 {
		return nil
	}
	out := make([]string, 0, len(caps))
	seen := make(map[string]bool, len(caps))
	for _, cap := range caps {
		cap = normalizeExecutionCapability(cap)
		if cap == "" || seen[cap] {
			continue
		}
		seen[cap] = true
		out = append(out, cap)
	}
	return out
}

func normalizeExecutionCapability(cap string) string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(cap)), func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || unicode.IsSpace(r)
	})
	return strings.Join(parts, "_")
}

func executionProfileToolNames(tools []map[string]interface{}) string {
	if len(tools) == 0 {
		return ""
	}
	names := make([]string, 0, len(tools))
	for _, def := range tools {
		if name := extractToolName(def); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ",")
}

func stripExecutionContractMetadataForLLM(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	stripped := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if def == nil {
			stripped = append(stripped, def)
			continue
		}
		if _, ok := def["x_execution_contract"]; !ok {
			stripped = append(stripped, def)
			continue
		}
		cp := make(map[string]interface{}, len(def)-1)
		for k, v := range def {
			if k == "x_execution_contract" {
				continue
			}
			cp[k] = v
		}
		stripped = append(stripped, cp)
	}
	return stripped
}
