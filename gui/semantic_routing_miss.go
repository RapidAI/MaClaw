package main

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
// Parent invariant 11 (revised): a failed plan must not dump editors,
// gateways, downloaders or governed publishers — but bash and write_file are
// the guaranteed basic capability floor. They stay available even on a
// routing-miss leftover turn, so a degraded or offline planner never leaves
// the desktop agent unable to run a command or write a file.
// Lexical pins (screenshot, office, IM) are not in this set; they stay if the
// leftover router already selected them.
var routingMissPrivilegeTools = map[string]bool{
	"edit_file":                true,
	"edit_lines":               true,
	"download_file":            true,
	"call_mcp_tool":            true,
	"manage_skill":             true,
	"search_and_install_skill": true,
	"craft_tool":               true,
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
	routingMissHostAdapterTool          = "generate_pdf"
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
	return h.getTools()
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

func applyRoutingMissLeftoverTools(tools, allTools []map[string]interface{}, ctx *LoopContext) []map[string]interface{} {
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
