package main

import (
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// DynamicToolBuilder builds LLM tool definitions dynamically from the ToolRegistry.
// This is a thin adapter around corelib/tool.DynamicToolBuilder, bridging
// gui-local ToolRegistry to corelib's Registry.
type DynamicToolBuilder struct {
	inner    *tool.DynamicToolBuilder
	registry *ToolRegistry
}

// NewDynamicToolBuilder creates a builder backed by the given gui registry.
func NewDynamicToolBuilder(registry *ToolRegistry) *DynamicToolBuilder {
	coreReg := guiRegistryToCorelib(registry)
	return &DynamicToolBuilder{
		inner:    tool.NewDynamicToolBuilder(coreReg),
		registry: registry,
	}
}

// BuildAll returns tool definitions for every available tool (no filtering).
func (b *DynamicToolBuilder) BuildAll() []map[string]interface{} {
	b.syncRegistry()
	return b.attachExecutionContracts(filterLegacyModelDynamicGatewayDefinitions(b.inner.BuildAll()))
}

// Build returns tool definitions, applying context-aware filtering when
// the number of available tools exceeds the threshold.
func (b *DynamicToolBuilder) Build(userMessage string) []map[string]interface{} {
	b.syncRegistry()
	return b.attachExecutionContracts(filterLegacyModelDynamicGatewayDefinitions(b.inner.Build(userMessage)))
}

// filterLegacyModelDynamicGatewayDefinitions is deliberately applied at the
// GUI wrapper too. The core builder enforces the same invariant, but this
// protects a partially upgraded or alternate builder from turning a registry
// entry into legacy model authority. Dynamic Skill/MCP calls are emitted only
// by their managed, identity-bound surfaces.
func filterLegacyModelDynamicGatewayDefinitions(defs []map[string]interface{}) []map[string]interface{} {
	if len(defs) == 0 {
		return defs
	}
	filtered := make([]map[string]interface{}, 0, len(defs))
	for _, def := range defs {
		if tool.IsLegacyModelDynamicGateway(extractToolName(def)) {
			continue
		}
		filtered = append(filtered, def)
	}
	return filtered
}

// SetEmbedder delegates to corelib/tool.DynamicToolBuilder.SetEmbedder.
func (b *DynamicToolBuilder) SetEmbedder(emb embedding.Embedder) {
	b.inner.SetEmbedder(emb)
}

// SetEnrichmentStore delegates to corelib/tool.DynamicToolBuilder.SetEnrichmentStore.
func (b *DynamicToolBuilder) SetEnrichmentStore(store *tool.EnrichmentStore) {
	b.inner.SetEnrichmentStore(store)
}

// SetUsageTracker delegates to corelib/tool.DynamicToolBuilder.SetUsageTracker.
func (b *DynamicToolBuilder) SetUsageTracker(tracker *tool.UsageTracker) {
	b.inner.SetUsageTracker(tracker)
}

// SetReranker delegates to corelib/tool.DynamicToolBuilder.SetReranker.
func (b *DynamicToolBuilder) SetReranker(rr tool.Reranker) {
	b.inner.SetReranker(rr)
}

// SetSkillProvider delegates to corelib/tool.DynamicToolBuilder.SetSkillProvider.
func (b *DynamicToolBuilder) SetSkillProvider(provider tool.SkillProvider) {
	b.inner.SetSkillProvider(provider)
}

// RefreshSkillIndex forces a rebuild of the inner skill routing index.
func (b *DynamicToolBuilder) RefreshSkillIndex() {
	b.inner.RefreshSkillIndex()
}

// syncRegistry refreshes the inner corelib registry from the gui registry.
// The corelib builder's BM25 index is preserved; only the registry is swapped.
func (b *DynamicToolBuilder) syncRegistry() {
	coreReg := guiRegistryToCorelib(b.registry)
	b.inner.SetRegistry(coreReg)
}

func (b *DynamicToolBuilder) attachExecutionContracts(defs []map[string]interface{}) []map[string]interface{} {
	if b == nil || b.registry == nil || len(defs) == 0 {
		return defs
	}
	contracts := make(map[string]map[string]interface{})
	for _, gt := range b.registry.List() {
		if len(gt.ExecutionContract) > 0 {
			contracts[gt.Name] = gt.ExecutionContract
		}
	}
	if len(contracts) == 0 {
		return defs
	}
	for _, def := range defs {
		if _, ok := def["x_execution_contract"]; ok {
			continue
		}
		name := extractToolName(def)
		if contract := contracts[name]; len(contract) > 0 {
			def["x_execution_contract"] = contract
		}
	}
	return defs
}

// guiRegistryToCorelib converts a gui ToolRegistry into a corelib tool.Registry.
func guiRegistryToCorelib(guiReg *ToolRegistry) *tool.Registry {
	reg := tool.NewRegistry()
	if guiReg == nil {
		return reg
	}
	for _, gt := range guiReg.List() {
		reg.Register(tool.RegisteredTool{
			Name:        gt.Name,
			Description: gt.Description,
			Category:    tool.Category(gt.Category),
			Tags:        gt.Tags,
			Priority:    gt.Priority,
			Status:      tool.Status(gt.Status),
			InputSchema: gt.InputSchema,
			Required:    gt.Required,
			Body:        gt.Body,
			BodySummary: gt.BodySummary,
		})
	}
	return reg
}

// registeredToolToDef converts a gui RegisteredTool to an OpenAI function
// calling definition. Delegates to corelib/tool.RegisteredToolToDef.
func registeredToolToDef(t RegisteredTool) map[string]interface{} {
	def := tool.RegisteredToolToDef(tool.RegisteredTool{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: t.InputSchema,
		Required:    t.Required,
	})
	if len(t.ExecutionContract) > 0 {
		def["x_execution_contract"] = agent.CloneToolDefinitionMap(t.ExecutionContract)
	}
	return def
}

// groupKeywords and detectGroupTags are now delegated to corelib.
var groupKeywords = tool.GroupKeywords

func detectGroupTags(userMessage string) map[string]bool {
	return tool.DetectGroupTags(userMessage)
}
