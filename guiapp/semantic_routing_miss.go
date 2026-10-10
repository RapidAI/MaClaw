package guiapp

import (
	"errors"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	routingMissFallbackReason    = "routing miss fallback"
	routingMissHostAdapterReason = "host adapter leftover"
)

// routingMissPrivilegeTools expand power when a precise surface missed.
// Parent invariant 11: a failed plan must not dump downloaders, line-range
// editors, provider gateways, or governed publishers. bash, read_file,
// write_file, edit_file, craft_tool, and manage_skill are the execution
// baseline: a degraded planner must still be able to run a command, change
// a file, run a one-off script, or invoke an installed skill. Lexical pins
// (screenshot, office, IM) are not in this set; they stay if the leftover
// router already selected them.
var routingMissPrivilegeTools = map[string]bool{
	"edit_lines":               true,
	"download_file":            true,
	"call_mcp_tool":            true,
	"search_and_install_skill": true,
	"task":                     true,
	"goal":                     true,
	// A document renderer performs a local mutation and publishes an artifact.
	// It therefore cannot survive a semantic-plan miss merely because the legacy
	// ranker happened to retrieve its definition.  Its capability must be
	// selected by the governed document.generate.file plan.
	"generate_pdf": true,
}

var (
	errSemanticAwaitingConfirmation     = errors.New("semantic route awaiting confirmation")
	errSemanticGenerateDeliveryConflict = errors.New("semantic route has conflicting attachment_delivery and document_generate")
	// errSemanticSessionCeilingSpent means this session already used every
	// planned invocation. The turn stays closed: the model answers from the
	// conversation, and the legacy tool catalog is not reopened.
	errSemanticSessionCeilingSpent = errors.New("semantic session ceiling spent")
	routingMissHostAdapterTool     = "generate_pdf"
)

func stampRoutingMissReason(result *intent.ClassificationResult, hostAdapter bool) {
	if result == nil {
		return
	}
	reason := strings.TrimSpace(result.Reason)
	if !strings.Contains(reason, routingMissFallbackReason) {
		reason = strings.TrimSpace(reason + "; " + routingMissFallbackReason)
	}
	if hostAdapter {
		if !strings.Contains(reason, routingMissHostAdapterReason) {
			reason = strings.TrimSpace(reason + "; " + routingMissHostAdapterReason)
		}
	} else {
		reason = stripReasonToken(reason, routingMissHostAdapterReason)
	}
	result.Reason = reason
}

func stripReasonToken(reason, token string) string {
	if token == "" || !strings.Contains(reason, token) {
		return strings.TrimSpace(reason)
	}
	parts := strings.Split(reason, ";")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == token {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "; ")
}

func semanticIntentHasLeftoverReason(result *intent.ClassificationResult) bool {
	if result == nil {
		return false
	}
	reason := result.Reason
	return strings.Contains(reason, routingMissFallbackReason) ||
		strings.Contains(reason, routingMissHostAdapterReason)
}

func sanitizeSemanticLeftoverReason(result *intent.ClassificationResult) {
	if result == nil {
		return
	}
	result.Reason = stripReasonToken(stripReasonToken(result.Reason, routingMissHostAdapterReason), routingMissFallbackReason)
}

func loopContextHasRoutingMissFallback(ctx *LoopContext) bool {
	return ctx != nil && ctx.Runtime.RoutingMissFallback
}

func loopContextHasHostAdapterLeftover(ctx *LoopContext) bool {
	return loopContextHasRoutingMissFallback(ctx) && ctx.Runtime.HostAdapterLeftover
}

func resetLoopSemanticLeftoverState(ctx *LoopContext) {
	if ctx == nil {
		return
	}
	dropIntent := ctx.Runtime.RoutingMissFallback || ctx.Runtime.HostAdapterLeftover ||
		semanticIntentHasLeftoverReason(ctx.Runtime.SemanticIntent)
	ctx.Runtime.RoutingMissFallback = false
	ctx.Runtime.HostAdapterLeftover = false
	ctx.Runtime.ClassifierTimeoutLookup = false
	if dropIntent {
		ctx.Runtime.SemanticIntent = nil
		return
	}
	sanitizeSemanticLeftoverReason(ctx.Runtime.SemanticIntent)
}

func bindLoopSemanticIntent(ctx *LoopContext, result *intent.ClassificationResult) {
	if ctx == nil {
		return
	}
	sanitizeSemanticLeftoverReason(result)
	ctx.Runtime.SemanticIntent = result
	ctx.Runtime.RoutingMissFallback = false
	ctx.Runtime.HostAdapterLeftover = false
	ctx.Runtime.ClassifierTimeoutLookup = false
}

func leftoverToolCatalog(h *IMMessageHandler, ctx *LoopContext, known []map[string]interface{}) []map[string]interface{} {
	if !loopContextHasHostAdapterLeftover(ctx) {
		return nil
	}
	if len(known) > 0 {
		return known
	}
	if h == nil {
		return nil
	}
	return h.unmanagedLegacyHostCatalog()
}

func routingMissWantsHostAdapter(result intent.ClassificationResult) bool {
	// A compatibility surface has no capability-plan evidence.  In particular,
	// a label is not a grant for a legacy adapter: doing that made a failed or
	// degraded semantic route expose generate_pdf without its required lookup
	// predecessor.  Workflow document staging must publish its own reviewed
	// plan rather than revive this fallback.
	_ = result
	return false
}

// routingMissReadOnlyFloor lists read-only lookup tools guaranteed to survive
// a routing-miss leftover surface regardless of how the legacy name router
// ranked them. A degraded turn (tree timeout, planner miss) is exactly when
// the model most needs to consult existing knowledge — "查询驱网登录信息"
// collapsed to [web_fetch, web_search] in production 2026-09-18 because
// knowledge_search/memory_recall ranked below the cut, and the model had no
// way to look up what the user had already saved. Read-only, zero-risk:
// they answer from local stores and grant no capability.
var routingMissReadOnlyFloor = map[string]bool{
	"knowledge_search": true,
	"memory_recall":    true,
}

// routingMissFloorDefinitions resolves the read-only floor directly from the
// builtin registry, independent of catalog composition (the unmanaged legacy
// catalog may exclude capability-catalogued tools like knowledge_search).
func (h *IMMessageHandler) routingMissFloorDefinitions() []map[string]interface{} {
	if h == nil || h.registry == nil {
		return nil
	}
	var out []map[string]interface{}
	for name := range routingMissReadOnlyFloor {
		registered, ok := h.registry.Get(name)
		if !ok || registered == nil || registered.Status != RegToolAvailable {
			continue
		}
		out = append(out, registeredToolToDef(*registered))
	}
	return out
}

func applyRoutingMissLeftoverTools(tools, allTools, floorCatalog []map[string]interface{}, ctx *LoopContext) []map[string]interface{} {
	if !loopContextHasRoutingMissFallback(ctx) {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	seen := make(map[string]bool, len(tools))
	for _, def := range tools {
		name := extractToolName(def)
		if routingMissPrivilegeTools[name] {
			continue
		}
		filtered = append(filtered, def)
		if name != "" {
			seen[name] = true
		}
	}
	// Guarantee the read-only lookup floor: pull any floored tool the ranker
	// dropped from the full catalog. (read-only tools are never in the
	// privilege-strip set, so this is purely additive — EXCEPT the group
	// boundary, which ran before this filter and must not be re-expanded:
	// a group that denies knowledge or uses the narrowed memory view would
	// otherwise get the full tools back through the floor.)
	for name := range routingMissReadOnlyFloor {
		if seen[name] {
			continue
		}
		if ctx != nil && ctx.LansengerGroupPermissions != nil && !lansengerGroupFloorAllowed(name, *ctx.LansengerGroupPermissions) {
			continue
		}
		for _, def := range floorCatalog {
			if extractToolName(def) != name {
				continue
			}
			filtered = append(filtered, def)
			seen[name] = true
			break
		}
	}
	if !loopContextHasHostAdapterLeftover(ctx) || ctx == nil || !semanticFileDeliveryPublished(ctx.Platform) {
		return filtered
	}
	if seen[routingMissHostAdapterTool] {
		return filtered
	}
	for _, def := range allTools {
		if extractToolName(def) != routingMissHostAdapterTool {
			continue
		}
		return append(filtered, def)
	}
	return filtered
}

// lansengerGroupFloorAllowed gates the read-only floor against the group
// permission boundary, which runs BEFORE applyRoutingMissLeftoverTools and
// must not be silently re-expanded. Groups that allow knowledge already get
// knowledge_search re-added by ensureLansengerGroupKnowledgeSearchTool (the
// seen-check skips the floor); groups that deny it must not receive it here.
// memory_recall is never floored into a group: group chats carry the
// narrowed "memory" recall view instead (fail-closed default in allowsTool).
func lansengerGroupFloorAllowed(name string, policy lansengerGroupPermissionPolicy) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "knowledge_search":
		return policy.allowsKnowledge()
	case "memory_recall":
		return false
	default:
		return true
	}
}

type semanticUnmetNeedsError struct {
	Unmet []tool.UnmetNeed
}

func (e semanticUnmetNeedsError) Error() string {
	return "semantic route has unmet needs: " + formatSemanticUnmetNeeds(e.Unmet)
}

func formatSemanticUnmetNeeds(unmet []tool.UnmetNeed) string {
	if len(unmet) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(unmet))
	for _, item := range unmet {
		parts = append(parts, item.NeedID+"="+item.ReasonCode)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func semanticUnmetOnlyBudget(unmet []tool.UnmetNeed) bool {
	if len(unmet) == 0 {
		return false
	}
	for _, item := range unmet {
		if item.ReasonCode != "budget_exceeded" && item.ReasonCode != "planning_budget_exceeded" {
			return false
		}
	}
	return true
}

func semanticUnmetOnlyReasons(unmet []tool.UnmetNeed, reasons ...string) bool {
	if len(unmet) == 0 {
		return false
	}
	allowed := make(map[string]bool, len(reasons))
	for _, reason := range reasons {
		allowed[reason] = true
	}
	for _, item := range unmet {
		if !allowed[item.ReasonCode] {
			return false
		}
	}
	return true
}

func semanticUnmetHasReason(err error, reason string) bool {
	var unmet semanticUnmetNeedsError
	if !errors.As(err, &unmet) {
		return false
	}
	for _, item := range unmet.Unmet {
		if item.ReasonCode == reason {
			return true
		}
	}
	return false
}
