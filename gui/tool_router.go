package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// ToolRouter selects the most relevant tools for a given user message.
// This is a thin adapter around corelib/tool.Router, bridging gui-local
// types (ToolRegistry, SkillHubClient) to corelib interfaces.
type ToolRouter struct {
	inner     *tool.Router
	generator *ToolDefinitionGenerator
	hubClient *SkillHubClient
	registry  *ToolRegistry
	// refreshSkillIndexOverride is test-only injection for transactional
	// callers; production uses the checked core Router boundary.
	refreshSkillIndexOverride func() error
}

// NewToolRouter creates a new ToolRouter.
func NewToolRouter(generator *ToolDefinitionGenerator) *ToolRouter {
	return &ToolRouter{
		inner:     tool.NewRouter(nil),
		generator: generator,
	}
}

// SetRegistry sets the ToolRegistry used for dynamic builtin detection and
// tag-based scoring. Internally converts to a corelib Registry adapter.
func (r *ToolRouter) SetRegistry(reg *ToolRegistry) {
	r.registry = reg
	if reg != nil {
		r.inner.SetRegistry(guiRegistryAdapter(reg))
	}
}

// SetHubClient sets the SkillHubClient used for recommendation matching.
func (r *ToolRouter) SetHubClient(client *SkillHubClient) {
	r.hubClient = client
	if client != nil {
		r.inner.SetRecommender(&hubRecommenderAdapter{client: client})
	}
}

// Route delegates to corelib/tool.Router.Route.
func (r *ToolRouter) Route(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	if r == nil || r.inner == nil {
		return nil
	}
	return r.inner.Route(userMessage, allTools)
}

// RouteWithOptions delegates to corelib/tool.Router.RouteWithOptions.
func (r *ToolRouter) RouteWithOptions(userMessage string, allTools []map[string]interface{}, opts tool.RouteOptions) []map[string]interface{} {
	if r == nil || r.inner == nil {
		return nil
	}
	return r.inner.RouteWithOptions(userMessage, allTools, opts)
}

// RecommendWithOptions returns one atomic legacy compatibility selection and
// its reviewed capability evidence. New GUI callers must create and render a
// LegacyAdapterPlan from this pair; the returned definitions are not an
// execution grant and must not be appended to a prior surface.
func (r *ToolRouter) RecommendWithOptions(userMessage string, allTools []map[string]interface{}, opts tool.RouteOptions) ([]map[string]interface{}, tool.RoutingRecommendation) {
	if r == nil || r.inner == nil {
		return nil, tool.RoutingRecommendation{}
	}
	return r.inner.RecommendWithOptions(userMessage, allTools, opts)
}

// RouteForSession keeps the owner argument for compatibility, but deliberately
// does not restore session pins. Continuity is derived from the current task
// plan/facts, never from a previous model-tool name.
func (r *ToolRouter) RouteForSession(sessionID, userMessage string, allTools []map[string]interface{}, opts tool.RouteOptions) []map[string]interface{} {
	if r == nil || r.inner == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return r.RouteWithOptions(userMessage, allTools, opts)
	}
	return r.inner.RouteWithOptions(userMessage, allTools, opts)
}

// SetEmbedder delegates to corelib/tool.Router.SetEmbedder.
func (r *ToolRouter) SetEmbedder(emb embedding.Embedder) {
	r.inner.SetEmbedder(emb)
}

// HybridActive returns true if hybrid retrieval is currently enabled.
func (r *ToolRouter) HybridActive() bool {
	return r.inner.HybridActive()
}

// SetEnrichmentStore delegates to corelib/tool.Router.SetEnrichmentStore.
func (r *ToolRouter) SetEnrichmentStore(store *tool.EnrichmentStore) {
	r.inner.SetEnrichmentStore(store)
}

// SetUsageTracker delegates to corelib/tool.Router.SetUsageTracker.
func (r *ToolRouter) SetUsageTracker(tracker *tool.UsageTracker) {
	r.inner.SetUsageTracker(tracker)
}

// SetReranker delegates to corelib/tool.Router.SetReranker.
func (r *ToolRouter) SetReranker(rr tool.Reranker) {
	r.inner.SetReranker(rr)
}

// SetIntentClassifier delegates to corelib/tool.Router.SetIntentClassifier.
func (r *ToolRouter) SetIntentClassifier(ic *tool.IntentClassifier) {
	r.inner.SetIntentClassifier(ic)
}

// SetUnifiedClassifier delegates to corelib/tool.Router.SetUnifiedClassifier.
func (r *ToolRouter) SetUnifiedClassifier(uic *intent.UnifiedIntentClassifier) {
	r.inner.SetUnifiedClassifier(uic)
}

// IntentClassifier returns the configured IntentClassifier from the inner router.
func (r *ToolRouter) IntentClassifier() *tool.IntentClassifier {
	return r.inner.IntentClassifier()
}

// SetSkillProvider delegates to corelib/tool.Router.SetSkillProvider.
func (r *ToolRouter) SetSkillProvider(provider tool.SkillProvider) {
	r.inner.SetSkillProvider(provider)
}

// SetSkillIndexProvider exposes the checked core index boundary to GUI tests
// and alternate providers. Production callers normally retain the default
// in-memory BM25 provider; injected providers may return rebuild errors that
// must cause the surrounding Skill transaction to compensate.
func (r *ToolRouter) SetSkillIndexProvider(provider tool.SkillIndexProvider) {
	if r == nil || r.inner == nil {
		return
	}
	r.inner.SetSkillIndexProvider(provider)
}

// RefreshSkillIndex delegates to corelib/tool.Router.RefreshSkillIndex.
// Call after installing or removing skills to update the BM25 index.
func (r *ToolRouter) RefreshSkillIndex() {
	_ = r.RefreshSkillIndexChecked()
}

// RefreshSkillIndexChecked makes index rebuild failures observable to commit
// transactions. Callers must treat a non-nil error as a rollback condition.
func (r *ToolRouter) RefreshSkillIndexChecked() error {
	if r == nil || r.inner == nil {
		return nil
	}
	if r.refreshSkillIndexOverride != nil {
		return r.refreshSkillIndexOverride()
	}
	return r.inner.RefreshSkillIndexChecked()
}

// ActivateSessionTool is a compatibility no-op. Historical tool names do not
// authorize a future model surface; task continuity requires a verified route.
func (r *ToolRouter) ActivateSessionTool(name string) {
	_ = r
	_ = name
}

// ActivateSessionToolForSession is a compatibility no-op. The owner key was
// not a task/revision/fencing proof and must not survive as hidden authority.
func (r *ToolRouter) ActivateSessionToolForSession(sessionID, name string) {
	_ = r
	_ = sessionID
	_ = name
}

// IsSessionPinned reports migration diagnostics only; callers must not use it
// to retain or add a model-visible tool.
func (r *ToolRouter) IsSessionPinned(name string) bool {
	if r == nil || r.inner == nil {
		return false
	}
	_ = name
	return false
}

func (r *ToolRouter) IsSessionPinnedForSession(sessionID, name string) bool {
	_ = r
	_ = sessionID
	_ = name
	return false
}

// SessionPinnedToolsMissing is retained for observability compatibility. It
// must not be used to mutate a model-visible surface.
func (r *ToolRouter) SessionPinnedToolsMissing(currentNames map[string]bool) []string {
	_ = r
	_ = currentNames
	return nil
}

// SessionPinnedToolsMissingForSession is a compatibility no-op. A missing tool
// name cannot be upgraded into a planner need or a visible function.
func (r *ToolRouter) SessionPinnedToolsMissingForSession(sessionID string, currentNames map[string]bool) []string {
	_ = r
	_ = sessionID
	_ = currentNames
	return nil
}

// ResetSession is retained for source compatibility; no task state is stored
// in the name router.
func (r *ToolRouter) ResetSession() {
	_ = r
}

func (r *ToolRouter) ResetSessionForSession(sessionID string) {
	_ = r
	_ = sessionID
}

// WarmupDeferredEmbeddings delegates to corelib/tool.Router.WarmupDeferredEmbeddings.
func (r *ToolRouter) WarmupDeferredEmbeddings(toolDefs []map[string]interface{}) {
	r.inner.WarmupDeferredEmbeddings(toolDefs)
}

// matchRecommendations is exposed for tests that call it directly.
// corelib's matchRecommendations is unexported, so we keep a thin local copy.
func (r *ToolRouter) matchRecommendations(msgTokens []string) map[string]interface{} {
	if r.hubClient == nil || len(msgTokens) == 0 {
		return nil
	}
	return matchRecommendationsLocal(r.hubClient, msgTokens)
}

// ---------------------------------------------------------------------------
// Adapters: bridge gui types → corelib interfaces
// ---------------------------------------------------------------------------

// hubRecommenderAdapter adapts SkillHubClient to tool.SkillRecommender.
type hubRecommenderAdapter struct {
	client *SkillHubClient
}

func (a *hubRecommenderAdapter) GetRecommendations() []tool.SkillRecommendation {
	recs := a.client.GetRecommendations()
	out := make([]tool.SkillRecommendation, len(recs))
	for i, r := range recs {
		out[i] = tool.SkillRecommendation{Name: r.Name, Description: r.Description}
	}
	return out
}

// guiRegistryAdapter converts a gui ToolRegistry snapshot into a corelib
// tool.Registry. Delegates to guiRegistryToCorelib (defined in tool_builder.go).
func guiRegistryAdapter(guiReg *ToolRegistry) *tool.Registry {
	return guiRegistryToCorelib(guiReg)
}

// ---------------------------------------------------------------------------
// Constants and maps — thin aliases to corelib/tool equivalents.
// Kept for test compatibility and local references.
// ---------------------------------------------------------------------------

const (
	maxToolBudget    = tool.MaxToolBudget
	maxDynamicRouted = tool.MaxDynamicRouted
)

// coreToolNames mirrors corelib/tool.CoreToolNames.
var coreToolNames = tool.CoreToolNames

// builtinToolNames mirrors corelib/tool.BuiltinToolNames.
var builtinToolNames = tool.BuiltinToolNames

// isBuiltinToolName delegates to corelib/tool.IsBuiltinToolName.
func isBuiltinToolName(name string) bool {
	return tool.IsBuiltinToolName(name)
}

// extractToolDescription delegates to corelib/tool.ExtractToolDescription.
func extractToolDescription(def map[string]interface{}) string {
	return tool.ExtractToolDescription(def)
}

// searchAndInstallSkillHint delegates to corelib/tool.SearchAndInstallSkillHint.
func searchAndInstallSkillHint() map[string]interface{} {
	return tool.SearchAndInstallSkillHint()
}

// ---------------------------------------------------------------------------
// matchRecommendationsLocal — local implementation for test compatibility.
// corelib's matchRecommendations is unexported, so we keep a thin copy.
// ---------------------------------------------------------------------------

func matchRecommendationsLocal(hubClient *SkillHubClient, msgTokens []string) map[string]interface{} {
	recommendations := hubClient.GetRecommendations()
	if len(recommendations) == 0 {
		return nil
	}

	msgSet := make(map[string]struct{}, len(msgTokens))
	for _, t := range msgTokens {
		msgSet[t] = struct{}{}
	}

	for _, rec := range recommendations {
		recTokens := bm25.Tokenize(rec.Name + " " + rec.Description)
		matchCount := 0
		for _, rt := range recTokens {
			if _, ok := msgSet[rt]; ok {
				matchCount++
				if len([]rune(rt)) > 1 {
					return searchAndInstallSkillHint()
				}
			}
		}
		if matchCount >= 2 {
			return searchAndInstallSkillHint()
		}
	}
	return nil
}

// codingSessionToolNames mirrors corelib/tool.CodingSessionToolNames.
var codingSessionToolNames = tool.CodingSessionToolNames

// filterCodingTools removes coding session tools from the tool list.
// Used in lite/simple mode where coding LLM providers are not configured.
func filterCodingTools(tools []map[string]interface{}) []map[string]interface{} {
	return tool.FilterCodingTools(tools)
}
