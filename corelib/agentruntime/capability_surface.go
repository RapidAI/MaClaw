package agentruntime

import (
	"sort"
	"strings"
)

// CapabilityToolInput is the host-neutral projection accepted from legacy
// tool catalogs. Hosts may keep their native schema type, but the resulting
// capability ordering and enabled/disabled semantics are centralized here.
type CapabilityToolInput struct {
	Name           string
	Description    string
	Parameters     map[string]any
	Enabled        bool
	DisabledReason string
}

// CapabilityToolsFromSpecs projects a native catalog into the shared surface.
// Empty names are ignored so malformed optional entries cannot create an
// addressable capability; output is deterministic by name.
func CapabilityToolsFromSpecs(specs []CapabilityToolInput) []CapabilityTool {
	tools := make([]CapabilityTool, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		tools = append(tools, CapabilityTool{
			Name: name, Description: spec.Description, Parameters: cloneCapabilityParameters(spec.Parameters),
			Enabled: spec.Enabled, DisabledReason: spec.DisabledReason,
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

// CapabilityToolsFromDefinitions projects the Runtime-native tool registry
// into the public capability envelope. Keeping this beside the legacy
// projections lets GUI and headless adapters publish the same schema without
// reimplementing sorting or parameter cloning.
func CapabilityToolsFromDefinitions(definitions []ToolDefinition, executable map[string]bool) []CapabilityTool {
	tools := make([]CapabilityTool, 0, len(definitions))
	for _, definition := range definitions {
		name := strings.TrimSpace(definition.Name)
		if name == "" {
			continue
		}
		enabled := true
		if executable != nil {
			enabled = executable[name]
		}
		reason := ""
		if !enabled {
			reason = "not_executable"
		}
		tools = append(tools, CapabilityTool{
			Name: name, Description: definition.Description,
			Parameters: cloneCapabilityParameters(definition.Parameters),
			Enabled:    enabled, DisabledReason: reason,
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

// MergeCapabilityTools overlays Runtime module tools onto a host catalog.
// Module names win so a shared builtin cannot be shadowed by a stale host copy.
func MergeCapabilityTools(base, overlay []CapabilityTool) []CapabilityTool {
	merged := make(map[string]CapabilityTool, len(base)+len(overlay))
	for _, tool := range base {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		tool.Name = name
		merged[name] = tool
	}
	for _, tool := range overlay {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		tool.Name = name
		merged[name] = tool
	}
	out := make([]CapabilityTool, 0, len(merged))
	for _, tool := range merged {
		out = append(out, tool)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cloneCapabilityParameters(parameters map[string]any) map[string]any {
	if parameters == nil {
		return nil
	}
	clone := make(map[string]any, len(parameters))
	for key, value := range parameters {
		clone[key] = value
	}
	return clone
}

// CapabilityToolsFromOpenAI projects OpenAI-compatible function definitions
// into the transport-neutral capability contract. Keeping this projection in
// agentruntime prevents GUI and headless hosts from maintaining subtly
// different name/description/parameter extraction code.
func CapabilityToolsFromOpenAI(definitions []map[string]interface{}) []CapabilityTool {
	tools := make([]CapabilityTool, 0, len(definitions))
	for _, raw := range definitions {
		fn, _ := raw["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		description, _ := fn["description"].(string)
		parameters, _ := fn["parameters"].(map[string]interface{})
		normalized := cloneCapabilityParameters(parameters)
		tools = append(tools, CapabilityTool{Name: name, Description: description, Parameters: normalized, Enabled: true})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}
