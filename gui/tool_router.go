package main

import (
	"strings"
	"sync"

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

	// sessionPinned keeps conditional-tool affinity per assistant owner. The
	// underlying core router has a process-wide pin set, so it cannot be used
	// directly when project, expert, and local conversations run concurrently.
	pinsMu        sync.RWMutex
	sessionPinned map[string]map[string]bool
}

// NewToolRouter creates a new ToolRouter.
func NewToolRouter(generator *ToolDefinitionGenerator) *ToolRouter {
	return &ToolRouter{
		inner:         tool.NewRouter(nil),
		generator:     generator,
		sessionPinned: make(map[string]map[string]bool),
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
	// Legacy ownerless callers use the fallback pin bucket. Serialize them with
	// owner-scoped routes so they cannot observe the core router's temporary
	// per-owner pin state.
	r.pinsMu.Lock()
	defer r.pinsMu.Unlock()
	return r.inner.Route(userMessage, allTools)
}

// RouteWithOptions delegates to corelib/tool.Router.RouteWithOptions.
func (r *ToolRouter) RouteWithOptions(userMessage string, allTools []map[string]interface{}, opts tool.RouteOptions) []map[string]interface{} {
	if r == nil || r.inner == nil {
		return nil
	}
	r.pinsMu.Lock()
	defer r.pinsMu.Unlock()
	return r.inner.RouteWithOptions(userMessage, allTools, opts)
}

// RouteForSession applies only this conversation's conditional tool pins for
// the duration of routing. The core router is retained for scoring, while the
// lock prevents a concurrent conversation from observing another owner's pins.
func (r *ToolRouter) RouteForSession(sessionID, userMessage string, allTools []map[string]interface{}, opts tool.RouteOptions) []map[string]interface{} {
	if r == nil || r.inner == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return r.RouteWithOptions(userMessage, allTools, opts)
	}
	r.pinsMu.Lock()
	defer r.pinsMu.Unlock()
	r.inner.ResetSession()
	// Do not leave a project's pins in the shared core-router instance after
	// routing. All owner pins live in sessionPinned; the core map is a guarded
	// transient compatibility bridge only.
	defer r.inner.ResetSession()
	for name := range r.sessionPinned[sessionID] {
		r.inner.ActivateSessionTool(name)
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

// RefreshSkillIndex delegates to corelib/tool.Router.RefreshSkillIndex.
// Call after installing or removing skills to update the BM25 index.
func (r *ToolRouter) RefreshSkillIndex() {
	r.inner.RefreshSkillIndex()
}

// ActivateSessionTool delegates to corelib/tool.Router.ActivateSessionTool.
func (r *ToolRouter) ActivateSessionTool(name string) {
	if r == nil || r.inner == nil {
		return
	}
	r.pinsMu.Lock()
	defer r.pinsMu.Unlock()
	r.inner.ActivateSessionTool(name)
}

func (r *ToolRouter) ActivateSessionToolForSession(sessionID, name string) {
	if r == nil || !tool.ShouldPinConditionalTool(name) {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		r.ActivateSessionTool(name)
		return
	}
	r.pinsMu.Lock()
	defer r.pinsMu.Unlock()
	if r.sessionPinned == nil {
		r.sessionPinned = make(map[string]map[string]bool)
	}
	if r.sessionPinned[sessionID] == nil {
		r.sessionPinned[sessionID] = make(map[string]bool)
	}
	r.sessionPinned[sessionID][name] = true
}

// IsSessionPinned returns true if the tool was session-pinned (via
// ActivateSessionTool). Used by routeTools to avoid removing ssh from
// the tool list when it was previously used in this session.
func (r *ToolRouter) IsSessionPinned(name string) bool {
	if r == nil || r.inner == nil {
		return false
	}
	r.pinsMu.RLock()
	defer r.pinsMu.RUnlock()
	return r.inner.IsSessionPinned(name)
}

func (r *ToolRouter) IsSessionPinnedForSession(sessionID, name string) bool {
	if r == nil {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return r.IsSessionPinned(name)
	}
	r.pinsMu.RLock()
	defer r.pinsMu.RUnlock()
	return r.sessionPinned[sessionID][name]
}

// SessionPinnedToolsMissing returns session-pinned tool names that are NOT
// in the provided currentNames set. It is the legacy fallback bucket only;
// owner-scoped agent loops must use SessionPinnedToolsMissingForSession.
func (r *ToolRouter) SessionPinnedToolsMissing(currentNames map[string]bool) []string {
	if r == nil || r.inner == nil {
		return nil
	}
	r.pinsMu.RLock()
	defer r.pinsMu.RUnlock()
	return r.inner.SessionPinnedToolsMissing(currentNames)
}

// SessionPinnedToolsMissingForSession returns just one assistant owner's pins
// that are absent from the current tool list. Keeping this lookup in the
// adapter avoids exposing the core router's temporary routing state to a
// different concurrent session.
func (r *ToolRouter) SessionPinnedToolsMissingForSession(sessionID string, currentNames map[string]bool) []string {
	if r == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return r.SessionPinnedToolsMissing(currentNames)
	}
	r.pinsMu.RLock()
	defer r.pinsMu.RUnlock()
	missing := make([]string, 0, len(r.sessionPinned[sessionID]))
	for name := range r.sessionPinned[sessionID] {
		if !currentNames[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// ResetSession delegates to corelib/tool.Router.ResetSession.
func (r *ToolRouter) ResetSession() {
	if r == nil || r.inner == nil {
		return
	}
	r.pinsMu.Lock()
	defer r.pinsMu.Unlock()
	r.inner.ResetSession()
}

func (r *ToolRouter) ResetSessionForSession(sessionID string) {
	if r == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		r.ResetSession()
		return
	}
	r.pinsMu.Lock()
	delete(r.sessionPinned, sessionID)
	r.pinsMu.Unlock()
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
