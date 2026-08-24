package tool

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// DynamicProviderDescriptor is the provider-neutral projection boundary for a
// verified Skill or MCP binding. Its fields are populated by a trusted host
// adapter after it has resolved a control-plane contract and a concrete runtime
// binding. Neither discovery descriptions nor provider names become part of an
// LLM-visible definition.
//
// ObservedSchemaDigest identifies the source runtime schema. ContractDigest
// identifies the control-plane declaration. Both, together with the closed
// InvocationSchema, are folded into ProviderBinding.SchemaDigest so contract or
// schema drift invalidates plans and grants rather than silently inheriting an
// old selection.
type DynamicProviderDescriptor struct {
	Kind                 string
	ProviderID           string
	ImplementationID     string
	ObservedSchemaDigest string
	ContractDigest       string
	Provides             []CapabilityProvision
	Consumes             []ArtifactContract
	Produces             []ArtifactContract
	Effects              []EffectClass
	Ready                bool
	ReadyUntil           time.Time
	ChannelScopes        []string
	InvocationSchema     map[string]interface{}
}

// Project converts a verified runtime binding into the common catalog model
// and its trusted renderer source. The returned AdapterName is an internal
// resolver identity, never a model-facing function name; CatalogRenderer will
// replace it with a signed opaque InvocationGrant token.
func (d DynamicProviderDescriptor) Project() (ProviderSpec, map[string]interface{}, error) {
	d.Kind = strings.ToLower(strings.TrimSpace(d.Kind))
	d.ProviderID = strings.TrimSpace(d.ProviderID)
	d.ImplementationID = strings.TrimSpace(d.ImplementationID)
	d.ObservedSchemaDigest = strings.TrimSpace(d.ObservedSchemaDigest)
	d.ContractDigest = strings.TrimSpace(d.ContractDigest)
	if d.Kind != "mcp" && d.Kind != "skill" {
		return ProviderSpec{}, nil, fmt.Errorf("dynamic provider kind %q is unsupported", d.Kind)
	}
	if d.ProviderID == "" || d.ImplementationID == "" || d.ObservedSchemaDigest == "" || d.ContractDigest == "" {
		return ProviderSpec{}, nil, fmt.Errorf("dynamic provider binding and contract digests are required")
	}
	parameters, err := projectedDynamicInvocationSchema(d.InvocationSchema)
	if err != nil {
		return ProviderSpec{}, nil, err
	}
	invocationDigest, err := dynamicInvocationSchemaDigest(parameters)
	if err != nil {
		return ProviderSpec{}, nil, err
	}
	bindingSchemaDigest := SchemaDigest([]byte(strings.Join([]string{d.ObservedSchemaDigest, d.ContractDigest, invocationDigest}, "\x00")))
	authorization, err := NewParameterAuthorization(parameters)
	if err != nil {
		return ProviderSpec{}, nil, fmt.Errorf("authorize dynamic invocation schema: %w", err)
	}
	adapterName := "dynamic_" + d.Kind + "_" + SchemaDigest([]byte(strings.Join([]string{d.Kind, d.ProviderID, d.ImplementationID, bindingSchemaDigest}, "\x00")))[:24]
	provider := ProviderSpec{
		AdapterName: adapterName,
		Binding: ProviderBinding{
			Kind:             d.Kind,
			ProviderID:       d.ProviderID,
			ImplementationID: d.ImplementationID,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides:               cloneCapabilityProvisions(d.Provides),
		Consumes:               append([]ArtifactContract(nil), d.Consumes...),
		Produces:               append([]ArtifactContract(nil), d.Produces...),
		Effects:                append([]EffectClass(nil), d.Effects...),
		Ready:                  d.Ready,
		ReadyUntil:             d.ReadyUntil,
		ChannelScopes:          append([]string(nil), d.ChannelScopes...),
	}
	return provider, map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			// The renderer discards this name and description. Keeping a fixed
			// value here makes accidental direct rendering apparent in tests.
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}, nil
}

// projectedDynamicInvocationSchema is intentionally narrower than JSON Schema.
// A dynamic provider owns neither the model prompt nor parameter authorisation:
// its discovery schema may describe data shape, but descriptions, examples,
// defaults, titles and extension fields are untrusted text and must not cross
// the renderer boundary. The canonical parameter validator supports exactly
// this closed structural subset as well.
func projectedDynamicInvocationSchema(schema map[string]interface{}) (map[string]interface{}, error) {
	if schema == nil {
		return nil, fmt.Errorf("dynamic invocation schema is required")
	}
	if strings.TrimSpace(fmt.Sprint(schema["type"])) != "object" {
		return nil, fmt.Errorf("dynamic invocation schema must be an object")
	}
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("dynamic invocation schema properties are required")
	}
	if additional, ok := schema["additionalProperties"].(bool); !ok || additional {
		return nil, fmt.Errorf("dynamic invocation schema must close additional properties")
	}
	required, err := dynamicRequiredFields(schema["required"])
	if err != nil {
		return nil, err
	}
	projectedProperties := make(map[string]interface{}, len(properties))
	for name, raw := range properties {
		if strings.TrimSpace(name) == "" || reservedDynamicInvocationField(name) {
			return nil, fmt.Errorf("dynamic invocation property %q is reserved or invalid", name)
		}
		property, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("dynamic invocation property %q is invalid", name)
		}
		projected, err := projectDynamicSchemaNode(property)
		if err != nil {
			return nil, fmt.Errorf("dynamic invocation property %q: %w", name, err)
		}
		projectedProperties[name] = projected
	}
	for _, field := range required {
		if _, exists := projectedProperties[field]; !exists {
			return nil, fmt.Errorf("dynamic invocation required field %q is not a property", field)
		}
	}
	return map[string]interface{}{"type": "object", "properties": projectedProperties, "required": required, "additionalProperties": false}, nil
}

func projectDynamicSchemaNode(schema map[string]interface{}) (map[string]interface{}, error) {
	kind, _ := schema["type"].(string)
	switch strings.TrimSpace(kind) {
	case "string", "number", "integer", "boolean":
		return map[string]interface{}{"type": kind}, nil
	case "array":
		rawItems, ok := schema["items"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("array items are required")
		}
		items, err := projectDynamicSchemaNode(rawItems)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"type": "array", "items": items}, nil
	case "object":
		rawProperties, ok := schema["properties"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("object properties are required")
		}
		if additional, ok := schema["additionalProperties"].(bool); !ok || additional {
			return nil, fmt.Errorf("object must close additional properties")
		}
		required, err := dynamicRequiredFields(schema["required"])
		if err != nil {
			return nil, err
		}
		properties := make(map[string]interface{}, len(rawProperties))
		for name, raw := range rawProperties {
			if strings.TrimSpace(name) == "" || reservedDynamicInvocationField(name) {
				return nil, fmt.Errorf("object property %q is reserved or invalid", name)
			}
			child, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("object property %q is invalid", name)
			}
			projected, err := projectDynamicSchemaNode(child)
			if err != nil {
				return nil, err
			}
			properties[name] = projected
		}
		for _, field := range required {
			if _, exists := properties[field]; !exists {
				return nil, fmt.Errorf("required field %q is not a property", field)
			}
		}
		return map[string]interface{}{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, nil
	default:
		return nil, fmt.Errorf("unsupported schema type %q", kind)
	}
}

func reservedDynamicInvocationField(field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "provider", "provider_id", "server", "server_id", "tool", "tool_name", "selection", "selection_id", "credential", "credentials", "token", "authorization", "artifact", "artifact_ref", "artifactref", "location", "location_ref", "receipt", "confirmation", "plan_id", "root_task_id":
		return true
	default:
		return false
	}
}

func dynamicRequiredFields(value interface{}) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	fields := make([]string, 0)
	switch typed := value.(type) {
	case []string:
		fields = append(fields, typed...)
	case []interface{}:
		for _, raw := range typed {
			field, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("dynamic invocation required field is not a string")
			}
			fields = append(fields, field)
		}
	default:
		return nil, fmt.Errorf("dynamic invocation required fields are invalid")
	}
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field) == "" || seen[field] {
			return nil, fmt.Errorf("dynamic invocation required fields are invalid")
		}
		seen[field] = true
	}
	sort.Strings(fields)
	return fields, nil
}

func dynamicInvocationSchemaDigest(schema map[string]interface{}) (string, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("encode dynamic invocation schema: %w", err)
	}
	return SchemaDigest(data), nil
}

func cloneCapabilityProvisions(in []CapabilityProvision) []CapabilityProvision {
	out := make([]CapabilityProvision, len(in))
	for i, provision := range in {
		out[i] = provision
		out[i].Qualifiers = cloneStringMap(provision.Qualifiers)
	}
	return out
}
