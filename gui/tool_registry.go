package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type ToolCategory string

const (
	ToolCategoryBuiltin ToolCategory = "builtin"
	ToolCategoryMCP     ToolCategory = "mcp"
	ToolCategorySkill   ToolCategory = "skill"
	ToolCategoryNonCode ToolCategory = "non_code"
)

type RegToolStatus string

const (
	RegToolAvailable   RegToolStatus = "available"
	RegToolDegraded    RegToolStatus = "degraded"
	RegToolUnavailable RegToolStatus = "unavailable"
)

// SemanticCatalogState makes catalog coverage explicit. A tool may either
// declare a governed capability provision, be an approved control-plane
// operation, or be quarantined from semantic materialization until its
// capability/trust contract is reviewed.
type SemanticCatalogState string

const (
	SemanticCatalogUnclassified      SemanticCatalogState = ""
	SemanticCatalogCapability        SemanticCatalogState = "capability"
	SemanticCatalogFixedControlPlane SemanticCatalogState = "fixed_control_plane"
	SemanticCatalogQuarantined       SemanticCatalogState = "quarantined"
)

type ToolHandler func(args map[string]interface{}) string
type ToolHandlerWithProgress func(args map[string]interface{}, onProgress tool.ProgressCallback) string
type ToolHandlerWithContext func(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string

type RegisteredTool struct {
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	Category          ToolCategory           `json:"category"`
	Tags              []string               `json:"tags"`
	Priority          int                    `json:"priority"`
	Status            RegToolStatus          `json:"status"`
	InputSchema       map[string]interface{} `json:"input_schema"`
	Required          []string               `json:"required"`
	Source            string                 `json:"source"`
	ExecutionContract map[string]interface{} `json:"x_execution_contract,omitempty"`
	// CapabilityProvisions and SemanticEffects are catalog declarations for
	// capability-first routing. They are registration metadata, not a tool-name
	// decision channel: planners consume only the declared capability contract.
	CapabilityProvisions []tool.CapabilityProvision `json:"-"`
	SemanticEffects      []tool.EffectClass         `json:"-"`
	// SemanticConsumes / SemanticProduces define the only typed values that a
	// governed selection may receive from, or publish to, another selection.
	// They are deliberately separate from the JSON function schema: a path or
	// base64 string supplied by a model is not an ArtifactRef.
	SemanticConsumes      []tool.ArtifactContract `json:"-"`
	SemanticProduces      []tool.ArtifactContract `json:"-"`
	SemanticCatalogState  SemanticCatalogState    `json:"-"`
	RuntimePolicyOwnerArg bool                    `json:"-"`
	RuntimePlatformArg    bool                    `json:"-"`
	Body                  string                  `json:"body,omitempty"`
	BodySummary           string                  `json:"body_summary,omitempty"`
	Handler               ToolHandler             `json:"-"`
	HandlerProg           ToolHandlerWithProgress `json:"-"`
	HandlerCtx            ToolHandlerWithContext  `json:"-"`
}

type ToolRegistry struct {
	mu       sync.RWMutex
	tools    map[string]*RegisteredTool
	onChange []func()
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]*RegisteredTool)}
}

func (r *ToolRegistry) Register(registered RegisteredTool) error {
	if registered.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if isDisabledExternalCodingSessionTool(registered.Name) {
		return nil
	}
	if registered.Status == "" {
		registered.Status = RegToolAvailable
	}
	if toolAcceptsRuntimePolicyOwnerArg(registered.Name) {
		registered.RuntimePolicyOwnerArg = true
	}
	if toolAcceptsRuntimePlatformArg(registered.Name) {
		registered.RuntimePlatformArg = true
	}
	registered.CapabilityProvisions = cloneCapabilityProvisions(registered.CapabilityProvisions)
	registered.SemanticEffects = append([]tool.EffectClass(nil), registered.SemanticEffects...)
	registered.SemanticConsumes = cloneSemanticArtifactContracts(registered.SemanticConsumes)
	registered.SemanticProduces = cloneSemanticArtifactContracts(registered.SemanticProduces)
	registered.Tags = append([]string(nil), registered.Tags...)
	registered.InputSchema = agent.CloneToolDefinitionMap(registered.InputSchema)
	registered.Required = append([]string(nil), registered.Required...)
	registered.ExecutionContract = agent.CloneToolDefinitionMap(registered.ExecutionContract)
	// Every registered implementation must have an explicit semantic-catalog
	// state. Existing tool surfaces may remain on a legacy compatibility path
	// during migration, but an unclassified entry must never be accidentally
	// treated as a governed capability provider.
	if registered.SemanticCatalogState == SemanticCatalogUnclassified {
		registered.SemanticCatalogState = SemanticCatalogQuarantined
	}
	if len(registered.CapabilityProvisions) > 0 || len(registered.SemanticEffects) > 0 {
		if len(registered.CapabilityProvisions) == 0 || len(registered.SemanticEffects) == 0 {
			return fmt.Errorf("semantic capability registration requires provisions and effects together")
		}
		if registered.SemanticCatalogState == SemanticCatalogQuarantined {
			registered.SemanticCatalogState = SemanticCatalogCapability
		}
		if registered.SemanticCatalogState != SemanticCatalogCapability {
			return fmt.Errorf("semantic capability registration cannot use catalog state %q", registered.SemanticCatalogState)
		}
	}
	r.mu.Lock()
	cp := registered
	r.tools[registered.Name] = &cp
	cbs := append([]func(){}, r.onChange...)
	r.mu.Unlock()
	for _, fn := range cbs {
		fn()
	}
	return nil
}

func (r *ToolRegistry) Unregister(name string) {
	r.mu.Lock()
	_, existed := r.tools[name]
	delete(r.tools, name)
	cbs := append([]func(){}, r.onChange...)
	r.mu.Unlock()
	if existed {
		for _, fn := range cbs {
			fn()
		}
	}
}

func (r *ToolRegistry) Get(name string) (*RegisteredTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, false
	}
	cp := *t
	cp.Tags = append([]string(nil), t.Tags...)
	cp.InputSchema = agent.CloneToolDefinitionMap(t.InputSchema)
	cp.Required = append([]string(nil), t.Required...)
	cp.ExecutionContract = agent.CloneToolDefinitionMap(t.ExecutionContract)
	cp.CapabilityProvisions = cloneCapabilityProvisions(t.CapabilityProvisions)
	cp.SemanticEffects = append([]tool.EffectClass(nil), t.SemanticEffects...)
	cp.SemanticConsumes = cloneSemanticArtifactContracts(t.SemanticConsumes)
	cp.SemanticProduces = cloneSemanticArtifactContracts(t.SemanticProduces)
	cp.SemanticCatalogState = t.SemanticCatalogState
	return &cp, true
}

func (r *ToolRegistry) List() []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RegisteredTool, 0, len(r.tools))
	for _, t := range r.tools {
		cp := *t
		cp.Tags = append([]string(nil), t.Tags...)
		cp.InputSchema = agent.CloneToolDefinitionMap(t.InputSchema)
		cp.Required = append([]string(nil), t.Required...)
		cp.ExecutionContract = agent.CloneToolDefinitionMap(t.ExecutionContract)
		cp.CapabilityProvisions = cloneCapabilityProvisions(t.CapabilityProvisions)
		cp.SemanticEffects = append([]tool.EffectClass(nil), t.SemanticEffects...)
		cp.SemanticConsumes = cloneSemanticArtifactContracts(t.SemanticConsumes)
		cp.SemanticProduces = cloneSemanticArtifactContracts(t.SemanticProduces)
		cp.SemanticCatalogState = t.SemanticCatalogState
		out = append(out, cp)
	}
	return out
}

func cloneCapabilityProvisions(in []tool.CapabilityProvision) []tool.CapabilityProvision {
	if len(in) == 0 {
		return nil
	}
	out := make([]tool.CapabilityProvision, len(in))
	for i, provision := range in {
		out[i] = provision
		if len(provision.Qualifiers) > 0 {
			out[i].Qualifiers = make(map[string]string, len(provision.Qualifiers))
			for key, value := range provision.Qualifiers {
				out[i].Qualifiers[key] = value
			}
		}
	}
	return out
}

func cloneSemanticArtifactContracts(in []tool.ArtifactContract) []tool.ArtifactContract {
	return append([]tool.ArtifactContract(nil), in...)
}

func (r *ToolRegistry) ListAvailable() []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []RegisteredTool
	for _, t := range r.tools {
		if t.Status == RegToolAvailable {
			cp := *t
			cp.Tags = append([]string(nil), t.Tags...)
			cp.InputSchema = agent.CloneToolDefinitionMap(t.InputSchema)
			cp.Required = append([]string(nil), t.Required...)
			cp.ExecutionContract = agent.CloneToolDefinitionMap(t.ExecutionContract)
			cp.CapabilityProvisions = cloneCapabilityProvisions(t.CapabilityProvisions)
			cp.SemanticEffects = append([]tool.EffectClass(nil), t.SemanticEffects...)
			cp.SemanticConsumes = cloneSemanticArtifactContracts(t.SemanticConsumes)
			cp.SemanticProduces = cloneSemanticArtifactContracts(t.SemanticProduces)
			cp.SemanticCatalogState = t.SemanticCatalogState
			out = append(out, cp)
		}
	}
	// Stable alphabetical order keeps LLM tool-list prefix cache hits stable.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *ToolRegistry) ListByCategory(cat ToolCategory) []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []RegisteredTool
	for _, t := range r.tools {
		if t.Category == cat {
			out = append(out, cloneRegisteredTool(*t))
		}
	}
	return out
}

func (r *ToolRegistry) ListByTags(tags []string) []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = true
	}
	var out []RegisteredTool
	for _, t := range r.tools {
		for _, tt := range t.Tags {
			if tagSet[strings.ToLower(tt)] {
				out = append(out, cloneRegisteredTool(*t))
				break
			}
		}
	}
	return out
}

func cloneRegisteredTool(in RegisteredTool) RegisteredTool {
	out := in
	out.Tags = append([]string(nil), in.Tags...)
	out.InputSchema = agent.CloneToolDefinitionMap(in.InputSchema)
	out.Required = append([]string(nil), in.Required...)
	out.ExecutionContract = agent.CloneToolDefinitionMap(in.ExecutionContract)
	out.CapabilityProvisions = cloneCapabilityProvisions(in.CapabilityProvisions)
	out.SemanticEffects = append([]tool.EffectClass(nil), in.SemanticEffects...)
	out.SemanticConsumes = cloneSemanticArtifactContracts(in.SemanticConsumes)
	out.SemanticProduces = cloneSemanticArtifactContracts(in.SemanticProduces)
	out.SemanticCatalogState = in.SemanticCatalogState
	return out
}

func (r *ToolRegistry) UpdateStatus(name string, status RegToolStatus) {
	r.mu.Lock()
	if t, ok := r.tools[name]; ok {
		t.Status = status
	}
	r.mu.Unlock()
}

func (r *ToolRegistry) OnChange(fn func()) {
	r.mu.Lock()
	r.onChange = append(r.onChange, fn)
	r.mu.Unlock()
}
