package main

import (
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
	return b.inner.BuildAll()
}

// Build returns tool definitions, applying context-aware filtering when
// the number of available tools exceeds the threshold.
func (b *DynamicToolBuilder) Build(userMessage string) []map[string]interface{} {
	b.syncRegistry()
	return b.inner.Build(userMessage)
}

// SetEmbedder delegates to corelib/tool.DynamicToolBuilder.SetEmbedder.
func (b *DynamicToolBuilder) SetEmbedder(emb embedding.Embedder) {
	b.inner.SetEmbedder(emb)
}

// syncRegistry refreshes the inner corelib registry from the gui registry.
// The corelib builder's BM25 index is preserved; only the registry is swapped.
func (b *DynamicToolBuilder) syncRegistry() {
	coreReg := guiRegistryToCorelib(b.registry)
	b.inner.SetRegistry(coreReg)
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
		})
	}
	return reg
}

// registeredToolToDef converts a gui RegisteredTool to an OpenAI function
// calling definition. Delegates to corelib/tool.RegisteredToolToDef.
func registeredToolToDef(t RegisteredTool) map[string]interface{} {
	return tool.RegisteredToolToDef(tool.RegisteredTool{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: t.InputSchema,
		Required:    t.Required,
	})
}

// groupKeywords and detectGroupTags are now delegated to corelib.
var groupKeywords = tool.GroupKeywords

func detectGroupTags(userMessage string) map[string]bool {
	return tool.DetectGroupTags(userMessage)
}
