package main

import (
	"github.com/RapidAI/CodeClaw/corelib/bm25"
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
	return r.inner.Route(userMessage, allTools)
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
	maxToolBudget  = tool.MaxToolBudget
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
