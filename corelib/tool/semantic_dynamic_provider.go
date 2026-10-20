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
	// TrustTier is projected from the resolved control-plane contract family
	// (reviewed -> approved, open lookup -> unreviewed). The host adapter sets
	// it from entry.Contract.TrustTier; discovery metadata cannot influence it.
	TrustTier        ProviderTrustTier
	InvocationSchema map[string]interface{}
	// ModelFunction is set only by a code-reviewed binding whose capability
	// already has a stable prompt spelling. Empty keeps the opaque grant token.
	ModelFunction string
}

// Project converts a verified runtime binding into the common catalog model
// and its trusted renderer source. The returned AdapterName is an internal
// resolver identity, never a vendor tool name. CatalogRenderer shows the
// stable prompt spelling when ModelFunction names one, and otherwise replaces
// the adapter with a signed opaque InvocationGrant token.
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
	// A reviewed binding that already has a sealed prompt spelling keeps that
	// spelling. Every other installed MCP tool gets a stable server__tool name
	// instead of a rotating grant token.
	if d.Kind == "mcp" && strings.TrimSpace(d.ModelFunction) == "" {
		if stable := InstalledMCPModelFunctionName(d.ProviderID, d.ImplementationID); stable != "" {
			adapterName = installedMCPAdapterPrefix + stable + ":" + adapterName
		}
	}
	if function := strings.TrimSpace(d.ModelFunction); function != "" {
		if !AllowReviewedDynamicPrompt(function) {
			return ProviderSpec{}, nil, fmt.Errorf("dynamic provider prompt %q is not a reviewed model function", function)
		}
		adapterName = reviewedDynamicPromptPrefix + function + ":" + adapterName
	}
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
		TrustTier:              d.TrustTier,
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
	// Refs are resolved against the original document. A $ref target does not
	// carry the root $defs map, and rewriting the root in place would let one
	// tool's projection mutate the inventory schema.
	stack := map[string]bool{}
	resolved, err := resolveDynamicSchemaNode(schema, schema, stack)
	if err != nil {
		return nil, err
	}
	kind, _ := resolved["type"].(string)
	if strings.TrimSpace(kind) != "object" {
		return nil, fmt.Errorf("dynamic invocation schema must be an object")
	}
	properties, err := dynamicObjectProperties(resolved)
	if err != nil {
		return nil, err
	}
	if err := dynamicObjectClosed(resolved); err != nil {
		return nil, fmt.Errorf("dynamic invocation schema must close additional properties")
	}
	required, err := dynamicRequiredFields(resolved["required"])
	if err != nil {
		return nil, err
	}
	projectedProperties := make(map[string]interface{}, len(properties))
	for name, raw := range properties {
		if skip, err := admitDynamicInvocationProperty(name); err != nil {
			return nil, err
		} else if skip || raw == nil || dynamicUnconstrainedProperty(raw) {
			continue
		}
		property, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("dynamic invocation property %q is invalid", name)
		}
		projected, err := projectDynamicSchemaNode(property, schema, stack)
		if err != nil {
			return nil, fmt.Errorf("dynamic invocation property %q: %w", name, err)
		}
		projectedProperties[name] = projected
	}
	required = dynamicAdmittedRequiredFields(required, projectedProperties)
	return map[string]interface{}{"type": "object", "properties": projectedProperties, "required": required, "additionalProperties": false}, nil
}

func projectDynamicSchemaNode(schema, root map[string]interface{}, stack map[string]bool) (map[string]interface{}, error) {
	resolved, err := resolveDynamicSchemaNode(schema, root, stack)
	if err != nil {
		return nil, err
	}
	kind, _ := resolved["type"].(string)
	switch strings.TrimSpace(kind) {
	case "string", "number", "integer", "boolean":
		// Descriptions, titles, defaults, examples and enums are vendor text.
		// The model sees the type only.
		return map[string]interface{}{"type": kind}, nil
	case "array":
		rawItems, ok := resolved["items"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("array items are required")
		}
		items, err := projectDynamicSchemaNode(rawItems, root, stack)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"type": "array", "items": items}, nil
	case "object":
		if err := dynamicObjectClosed(resolved); err != nil {
			return nil, err
		}
		rawProperties, err := dynamicObjectProperties(resolved)
		if err != nil {
			return nil, err
		}
		required, err := dynamicRequiredFields(resolved["required"])
		if err != nil {
			return nil, err
		}
		properties := make(map[string]interface{}, len(rawProperties))
		for name, raw := range rawProperties {
			if skip, err := admitDynamicInvocationProperty(name); err != nil {
				return nil, err
			} else if skip || raw == nil || dynamicUnconstrainedProperty(raw) {
				continue
			}
			child, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("object property %q is invalid", name)
			}
			projected, err := projectDynamicSchemaNode(child, root, stack)
			if err != nil {
				return nil, err
			}
			properties[name] = projected
		}
		required = dynamicAdmittedRequiredFields(required, properties)
		return map[string]interface{}{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, nil
	default:
		return nil, fmt.Errorf("unsupported schema type %q", kind)
	}
}

// resolveDynamicSchemaNode reduces the vendor shapes that still name one
// structural type. A same-document $ref is followed back into root; remote
// refs and cycles are not. Nullable unions (type arrays and anyOf/oneOf with
// null) become that type. An allOf is the intersection: one structural branch
// is that branch, and compatible objects or arrays merge. An object that only
// declares properties is an object. A union of two real types, or an allOf
// whose branches disagree, stays unrepresentable: projecting one of them
// would authorize arguments the tool did not declare.
func resolveDynamicSchemaNode(schema, root map[string]interface{}, stack map[string]bool) (map[string]interface{}, error) {
	if schema == nil {
		return nil, fmt.Errorf("unsupported schema type %q", "")
	}
	if ref, ok := schema["$ref"].(string); ok && strings.TrimSpace(ref) != "" {
		ref = strings.TrimSpace(ref)
		if stack[ref] {
			return nil, fmt.Errorf("unsupported schema type %q", "$ref")
		}
		target, err := lookupDynamicSchemaRef(root, ref)
		if err != nil {
			return nil, err
		}
		if stack == nil {
			stack = map[string]bool{}
		}
		stack[ref] = true
		resolved, err := resolveDynamicSchemaNode(target, root, stack)
		delete(stack, ref)
		return resolved, err
	}
	if alts, ok := schema["allOf"].([]interface{}); ok && len(alts) > 0 {
		return resolveDynamicAllOf(schema, alts, root, stack)
	}
	// anyOf/oneOf beside a shape this node already names is a constraint the
	// server enforces. Treating it as the type source quarantines a tool whose
	// arguments are already declared. A single structural branch is still that
	// branch: a nullable union must not drop the fields written only there.
	redundant, err := dynamicUnionRedundant(schema, root, stack)
	if err != nil {
		return nil, err
	}
	if !redundant {
		alternatives, exclusive, err := dynamicSchemaAlternatives(schema)
		if err != nil {
			return nil, err
		}
		if exclusive {
			branch, err := oneDynamicSchemaAlternative(alternatives)
			if err != nil {
				return nil, err
			}
			return resolveDynamicSchemaNode(branch, root, stack)
		}
	}
	if types, ok := dynamicSchemaTypeList(schema["type"]); ok {
		kind, err := oneDynamicSchemaType(types)
		if err != nil {
			return nil, err
		}
		copied := make(map[string]interface{}, len(schema)+1)
		for key, value := range schema {
			copied[key] = value
		}
		copied["type"] = kind
		return copied, nil
	}
	if _, ok := schema["type"].(string); ok {
		return schema, nil
	}
	if _, ok := schema["properties"].(map[string]interface{}); ok {
		return copyDynamicSchemaWithType(schema, "object"), nil
	}
	// required applies to objects. A wrapper that only adds required next
	// to allOf is that object, not an unrepresentable schema.
	if _, exists := schema["required"]; exists {
		return copyDynamicSchemaWithType(schema, "object"), nil
	}
	if _, ok := schema["items"].(map[string]interface{}); ok {
		return copyDynamicSchemaWithType(schema, "array"), nil
	}
	return schema, nil
}

// lookupDynamicSchemaRef resolves one local JSON pointer. Only "#/..." refs
// into this document are structural; a URL or a missing target is not a type
// we can project without fetching or inventing a schema.
func lookupDynamicSchemaRef(root map[string]interface{}, ref string) (map[string]interface{}, error) {
	if root == nil || !strings.HasPrefix(ref, "#") {
		return nil, fmt.Errorf("unsupported schema type %q", "$ref")
	}
	pointer := strings.TrimPrefix(ref, "#")
	if pointer == "" {
		return root, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("unsupported schema type %q", "$ref")
	}
	current := interface{}(root)
	for _, raw := range strings.Split(pointer[1:], "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		if token == "" {
			return nil, fmt.Errorf("unsupported schema type %q", "$ref")
		}
		object, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unsupported schema type %q", "$ref")
		}
		next, ok := object[token]
		if !ok {
			return nil, fmt.Errorf("unsupported schema type %q", "$ref")
		}
		current = next
	}
	object, ok := current.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unsupported schema type %q", "$ref")
	}
	return object, nil
}

func copyDynamicSchemaWithType(schema map[string]interface{}, kind string) map[string]interface{} {
	copied := make(map[string]interface{}, len(schema)+1)
	for key, value := range schema {
		copied[key] = value
	}
	copied["type"] = kind
	return copied
}

// resolveDynamicAllOf projects an intersection onto one structural schema.
// Description text, a boolean close flag, and validation keywords this host
// does not enforce (length, pattern, enum, const, numeric bounds) are not
// separate shapes. A single remaining branch is that branch. Objects and
// arrays combine only when every repeated field projects to the same shape.
func resolveDynamicAllOf(schema map[string]interface{}, alts []interface{}, root map[string]interface{}, stack map[string]bool) (map[string]interface{}, error) {
	branches := make([]map[string]interface{}, 0, len(alts)+1)
	for _, raw := range alts {
		candidate, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unsupported schema type %q", "allOf")
		}
		if dynamicSchemaNonStructural(candidate) {
			continue
		}
		resolved, err := resolveDynamicSchemaNode(candidate, root, stack)
		if err != nil {
			return nil, err
		}
		if dynamicSchemaNonStructural(resolved) {
			continue
		}
		branches = append(branches, resolved)
	}
	// A root anyOf/oneOf next to a real allOf branch is a constraint, not a
	// second shape. A union that is the only branch is still resolved below,
	// so a nullable schema written beside an empty allOf keeps its fields.
	if sibling := dynamicAllOfSibling(schema); sibling != nil && (len(branches) == 0 || !dynamicSchemaIsUnionConstraint(sibling)) {
		branches = append(branches, sibling)
	}
	if len(branches) == 0 {
		return nil, fmt.Errorf("unsupported schema type %q", "allOf")
	}
	if len(branches) == 1 {
		// Resolve again so a required-only sibling gains an object type.
		// The allOf key is already removed, so this does not re-enter here.
		return resolveDynamicSchemaNode(branches[0], root, stack)
	}
	kind := ""
	normalized := make([]map[string]interface{}, 0, len(branches))
	for _, branch := range branches {
		branchKind, typed := dynamicResolvedSchemaKind(branch)
		if branchKind == "" {
			return nil, fmt.Errorf("unsupported schema type %q", "allOf")
		}
		if kind == "" {
			kind = branchKind
		} else if kind != branchKind {
			return nil, fmt.Errorf("unsupported schema type %q", "allOf")
		}
		if !typed {
			branch = copyDynamicSchemaWithType(branch, branchKind)
		}
		normalized = append(normalized, branch)
	}
	switch kind {
	case "string", "number", "integer", "boolean":
		return map[string]interface{}{"type": kind}, nil
	case "object":
		return mergeDynamicAllOfObjects(normalized, root, stack)
	case "array":
		return mergeDynamicAllOfArrays(normalized, root, stack)
	default:
		return nil, fmt.Errorf("unsupported schema type %q", "allOf")
	}
}

func dynamicAllOfSibling(schema map[string]interface{}) map[string]interface{} {
	sibling := make(map[string]interface{}, len(schema))
	for key, value := range schema {
		if key == "allOf" {
			continue
		}
		sibling[key] = value
	}
	if dynamicSchemaNonStructural(sibling) {
		return nil
	}
	return sibling
}

func dynamicSchemaNonStructural(schema map[string]interface{}) bool {
	if len(schema) == 0 {
		return true
	}
	for key, value := range schema {
		if !dynamicSchemaKeywordIgnored(key, value) {
			return false
		}
	}
	return true
}

// dynamicSchemaKeywordIgnored reports a keyword that does not name a value
// shape. Applicators such as if/then and patternProperties stay on the server.
// Keeping them as a structural allOf branch quarantines the tool, because that
// branch has no type of its own. Schema-valued additionalProperties is not
// ignored: an open map is still unrepresentable.
func dynamicSchemaKeywordIgnored(key string, value interface{}) bool {
	switch key {
	case "description", "title", "default", "examples", "example", "$comment", "markdownDescription", "deprecated", "readOnly", "writeOnly", "$defs", "definitions", "$schema", "$id", "id",
		"if", "then", "else", "not", "dependentSchemas", "dependentRequired", "patternProperties", "propertyNames", "contains", "prefixItems":
		return true
	case "additionalProperties", "unevaluatedProperties", "unevaluatedItems", "nullable":
		_, ok := value.(bool)
		return ok
	default:
		return dynamicSchemaValidationKeyword(key)
	}
}

// dynamicSchemaIsUnionConstraint reports a sibling that is only anyOf or oneOf
// plus ignored keywords. It is not an object or array shape.
func dynamicSchemaIsUnionConstraint(schema map[string]interface{}) bool {
	if len(schema) == 0 {
		return false
	}
	sawUnion := false
	for key, value := range schema {
		switch key {
		case "anyOf", "oneOf":
			sawUnion = true
		default:
			if !dynamicSchemaKeywordIgnored(key, value) {
				return false
			}
		}
	}
	return sawUnion
}

// dynamicUnionRedundant reports whether anyOf or oneOf adds no shape beyond
// the type this node already declares. Two agreeing branches, or only
// validation keywords, are the server's constraint. One structural branch
// stays in force so a nullable union is that value. A union of two different
// types is not redundant, and a reserved field inside a branch still rejects
// the tool.
func dynamicUnionRedundant(schema, root map[string]interface{}, stack map[string]bool) (bool, error) {
	if !dynamicDeclaredShapeCoversUnion(schema) {
		return false, nil
	}
	declared, ok := dynamicDeclaredKind(schema)
	if !ok {
		return false, nil
	}
	alternatives, exclusive, err := dynamicSchemaAlternatives(schema)
	if err != nil || !exclusive {
		return err == nil, err
	}
	structural := 0
	for _, raw := range alternatives {
		branch, ok := raw.(map[string]interface{})
		if !ok {
			return false, nil
		}
		if dynamicSchemaIsNull(branch) || dynamicSchemaNonStructural(branch) {
			continue
		}
		resolved, err := resolveDynamicSchemaNode(branch, root, stack)
		if err != nil {
			return false, err
		}
		if dynamicSchemaNonStructural(resolved) {
			continue
		}
		kind, _ := dynamicResolvedSchemaKind(resolved)
		if kind == "" || kind != declared {
			return false, nil
		}
		// Project the branch so a reserved field such as token still rejects the
		// tool when the union itself is only a constraint on the declared shape.
		if _, err := projectDynamicSchemaNode(resolved, root, stack); err != nil {
			return false, err
		}
		structural++
	}
	return structural != 1, nil
}

func dynamicDeclaredKind(schema map[string]interface{}) (string, bool) {
	if types, ok := dynamicSchemaTypeList(schema["type"]); ok {
		kind, err := oneDynamicSchemaType(types)
		if err != nil {
			return "", false
		}
		return kind, true
	}
	if kind, ok := schema["type"].(string); ok {
		switch strings.TrimSpace(kind) {
		case "string", "number", "integer", "boolean", "array", "object":
			return strings.TrimSpace(kind), true
		}
	}
	if properties, ok := schema["properties"].(map[string]interface{}); ok && len(properties) > 0 {
		return "object", true
	}
	if _, ok := schema["items"].(map[string]interface{}); ok {
		return "array", true
	}
	return "", false
}

// dynamicDeclaredShapeCoversUnion reports that the node already lists the
// value the model can send. An object type with no properties does not cover
// a union, because the fields may exist only on a nullable branch.
func dynamicDeclaredShapeCoversUnion(schema map[string]interface{}) bool {
	kind, ok := dynamicDeclaredKind(schema)
	if !ok {
		return false
	}
	switch kind {
	case "object":
		properties, isMap := schema["properties"].(map[string]interface{})
		return isMap && len(properties) > 0
	case "array":
		_, isMap := schema["items"].(map[string]interface{})
		return isMap
	case "string", "number", "integer", "boolean":
		return true
	default:
		return false
	}
}

// dynamicUnconstrainedProperty reports an argument that names no value the
// model can send: a boolean schema, an empty schema, or annotations only.
// Omitting that field keeps the rest of the tool callable. enum and const
// without a type are not unconstrained; they still reject the tool.
func dynamicUnconstrainedProperty(raw interface{}) bool {
	typed, ok := raw.(map[string]interface{})
	if !ok {
		_, isBool := raw.(bool)
		return isBool
	}
	for key, value := range typed {
		switch key {
		case "description", "title", "default", "examples", "example", "$comment", "markdownDescription", "deprecated", "readOnly", "writeOnly", "$defs", "definitions", "$schema", "$id", "id":
		case "additionalProperties", "unevaluatedProperties", "unevaluatedItems", "nullable":
			if _, isBool := value.(bool); !isBool {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// dynamicSchemaValidationKeyword reports a JSON Schema keyword that constrains
// a value the host already projects as a closed type. The server still
// validates it. Keeping the keyword as a structural branch quarantines the
// tool, because the branch has no type of its own.
func dynamicSchemaValidationKeyword(key string) bool {
	switch key {
	case "minLength", "maxLength", "pattern", "format", "contentMediaType", "contentEncoding",
		"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf",
		"minItems", "maxItems", "uniqueItems", "minContains", "maxContains",
		"minProperties", "maxProperties",
		"const", "enum", "discriminator", "xml", "externalDocs":
		return true
	default:
		return false
	}
}

func dynamicResolvedSchemaKind(schema map[string]interface{}) (string, bool) {
	if kind, ok := schema["type"].(string); ok {
		kind = strings.TrimSpace(kind)
		if kind != "" {
			return kind, true
		}
	}
	if types, ok := dynamicSchemaTypeList(schema["type"]); ok {
		kind, err := oneDynamicSchemaType(types)
		if err != nil {
			return "", false
		}
		return kind, false
	}
	if _, ok := schema["properties"].(map[string]interface{}); ok {
		return "object", false
	}
	if _, exists := schema["required"]; exists {
		return "object", false
	}
	if _, ok := schema["items"].(map[string]interface{}); ok {
		return "array", false
	}
	return "", false
}

func mergeDynamicAllOfObjects(branches []map[string]interface{}, root map[string]interface{}, stack map[string]bool) (map[string]interface{}, error) {
	properties := map[string]interface{}{}
	requiredSeen := map[string]bool{}
	var required []string
	for _, branch := range branches {
		if err := dynamicObjectClosed(branch); err != nil {
			return nil, err
		}
		rawProperties, err := dynamicObjectProperties(branch)
		if err != nil {
			return nil, err
		}
		for name, raw := range rawProperties {
			if skip, err := admitDynamicInvocationProperty(name); err != nil {
				return nil, err
			} else if skip || raw == nil || dynamicUnconstrainedProperty(raw) {
				continue
			}
			child, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("unsupported schema type %q", "allOf")
			}
			projected, err := projectDynamicSchemaNode(child, root, stack)
			if err != nil {
				return nil, err
			}
			if existing, ok := properties[name]; ok {
				equal, err := dynamicProjectedJSONEqual(existing, projected)
				if err != nil || !equal {
					return nil, fmt.Errorf("unsupported schema type %q", "allOf")
				}
				continue
			}
			properties[name] = projected
		}
		fields, err := dynamicRequiredFields(branch["required"])
		if err != nil {
			return nil, err
		}
		for _, field := range fields {
			if requiredSeen[field] {
				continue
			}
			requiredSeen[field] = true
			required = append(required, field)
		}
	}
	sort.Strings(required)
	merged := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		merged["required"] = required
	}
	return merged, nil
}

func mergeDynamicAllOfArrays(branches []map[string]interface{}, root map[string]interface{}, stack map[string]bool) (map[string]interface{}, error) {
	var items map[string]interface{}
	for _, branch := range branches {
		raw, ok := branch["items"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unsupported schema type %q", "allOf")
		}
		projected, err := projectDynamicSchemaNode(raw, root, stack)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = projected
			continue
		}
		equal, err := dynamicProjectedJSONEqual(items, projected)
		if err != nil || !equal {
			return nil, fmt.Errorf("unsupported schema type %q", "allOf")
		}
	}
	if items == nil {
		return nil, fmt.Errorf("unsupported schema type %q", "allOf")
	}
	return map[string]interface{}{"type": "array", "items": items}, nil
}

func dynamicProjectedJSONEqual(left, right interface{}) (bool, error) {
	encodedLeft, err := json.Marshal(left)
	if err != nil {
		return false, err
	}
	encodedRight, err := json.Marshal(right)
	if err != nil {
		return false, err
	}
	return string(encodedLeft) == string(encodedRight), nil
}

// dynamicSchemaAlternatives reports anyOf or oneOf. Both at once is not a
// single type, so it is an error rather than a silent pick.
func dynamicSchemaAlternatives(schema map[string]interface{}) ([]interface{}, bool, error) {
	anyOf, anyOK := schema["anyOf"].([]interface{})
	oneOf, oneOK := schema["oneOf"].([]interface{})
	if anyOK && oneOK && len(anyOf) > 0 && len(oneOf) > 0 {
		return nil, false, fmt.Errorf("unsupported schema type %q", "anyOf+oneOf")
	}
	if anyOK && len(anyOf) > 0 {
		return anyOf, true, nil
	}
	if oneOK && len(oneOf) > 0 {
		return oneOf, true, nil
	}
	return nil, false, nil
}

func oneDynamicSchemaAlternative(alternatives []interface{}) (map[string]interface{}, error) {
	var branch map[string]interface{}
	for _, raw := range alternatives {
		candidate, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unsupported schema type %q", "anyOf")
		}
		if dynamicSchemaIsNull(candidate) {
			continue
		}
		if branch != nil {
			return nil, fmt.Errorf("unsupported schema type %q", "anyOf")
		}
		branch = candidate
	}
	if branch == nil {
		return nil, fmt.Errorf("unsupported schema type %q", "null")
	}
	return branch, nil
}

func dynamicSchemaIsNull(schema map[string]interface{}) bool {
	types, ok := dynamicSchemaTypeList(schema["type"])
	if !ok {
		kind, _ := schema["type"].(string)
		return kind == "null"
	}
	if len(types) == 0 {
		return false
	}
	for _, kind := range types {
		if kind != "null" {
			return false
		}
	}
	return true
}

func dynamicSchemaTypeList(value interface{}) ([]string, bool) {
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), true
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			kind, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, kind)
		}
		return out, true
	default:
		return nil, false
	}
}

func oneDynamicSchemaType(types []string) (string, error) {
	var kind string
	for _, candidate := range types {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == "null" {
			continue
		}
		switch candidate {
		case "string", "number", "integer", "boolean", "array", "object":
		default:
			return "", fmt.Errorf("unsupported schema type %q", candidate)
		}
		if kind != "" && kind != candidate {
			return "", fmt.Errorf("unsupported schema type %q", candidate)
		}
		kind = candidate
	}
	if kind == "" {
		return "", fmt.Errorf("unsupported schema type %q", "null")
	}
	return kind, nil
}

// dynamicObjectClosed rejects a map-valued additionalProperties. A missing
// flag, or an explicit true, is closed by the host: the root schema already
// forces that, and a nested object that merely omitted the flag was dropping
// the whole tool before planning.
func dynamicObjectClosed(schema map[string]interface{}) error {
	raw, exists := schema["additionalProperties"]
	if !exists || raw == nil {
		return nil
	}
	if _, ok := raw.(bool); ok {
		return nil
	}
	return fmt.Errorf("object must close additional properties")
}

func dynamicObjectProperties(schema map[string]interface{}) (map[string]interface{}, error) {
	raw, exists := schema["properties"]
	if !exists || raw == nil {
		return map[string]interface{}{}, nil
	}
	properties, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("object properties are required")
	}
	return properties, nil
}

func reservedDynamicInvocationField(field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "provider", "provider_id", "server", "server_id", "tool", "tool_name", "selection", "selection_id", "credential", "credentials", "token", "authorization", "artifact", "artifact_ref", "artifactref", "location", "location_ref", "receipt", "confirmation", "plan_id", "root_task_id":
		return true
	default:
		return false
	}
}

// HostBoundDynamicField reports an invocation name the host already binds:
// server, tool, provider, selection, credential, or artifact identity. A
// composed allOf branch can reintroduce one after the root map dropped it.
// Omitting the field keeps the tool callable. Other reserved names, such as
// token and location, still reject the tool.
func HostBoundDynamicField(field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "server_id", "server", "tool_name", "tool", "provider", "provider_id", "selection_id", "artifact_id", "artifact_ref", "credential", "credentials":
		return true
	default:
		return false
	}
}

// admitDynamicInvocationProperty reports whether a property is absent from the
// model schema. Host-bound identity fields are skipped. Any other reserved
// name rejects the whole tool.
func admitDynamicInvocationProperty(name string) (bool, error) {
	if strings.TrimSpace(name) == "" {
		return false, fmt.Errorf("dynamic invocation property %q is reserved or invalid", name)
	}
	if HostBoundDynamicField(name) {
		return true, nil
	}
	if reservedDynamicInvocationField(name) {
		return false, fmt.Errorf("dynamic invocation property %q is reserved or invalid", name)
	}
	return false, nil
}

// dynamicAdmittedRequiredFields keeps required names the projected object
// actually exposes. A name with no property cannot be sent, and failing the
// whole tool for it drops every other argument as well.
func dynamicAdmittedRequiredFields(required []string, properties map[string]interface{}) []string {
	if len(required) == 0 {
		return required
	}
	admitted := make([]string, 0, len(required))
	for _, field := range required {
		if _, ok := properties[field]; ok {
			admitted = append(admitted, field)
		}
	}
	return admitted
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
	deduped := make([]string, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field) == "" {
			return nil, fmt.Errorf("dynamic invocation required fields are invalid")
		}
		// A repeated name is the same argument. Rejecting it quarantines a tool
		// whose schema is otherwise callable.
		if seen[field] {
			continue
		}
		seen[field] = true
		deduped = append(deduped, field)
	}
	sort.Strings(deduped)
	return deduped, nil
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
