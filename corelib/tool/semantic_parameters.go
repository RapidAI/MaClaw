package tool

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// CanonicalRequest is the closed, deterministic parameter representation that
// crosses the semantic execution boundary. The caller must use CanonicalJSON,
// rather than the model's original JSON, when invoking a legacy adapter.
type CanonicalRequest struct {
	ProfileVersion string
	CanonicalJSON  []byte
	Digest         string
	Values         map[string]interface{}
}

// ParameterAuthorization is the server-side authorization projection for a
// materialized selection. The schema digest and closed field set bind the
// parameter shape; AllowedTargets/AllowedArtifactIDs close the remaining
// execution-side gap: a model may never introduce a target or artifact
// reference (in any string value, at any depth) that the trusted
// authorization did not declare. Empty constraint lists mean no references
// are authorized, which is fail closed for legacy authorizations.
type ParameterAuthorization struct {
	Digest           string
	CanonicalizerVer string
	AllowedFields    []string
	// AllowedTargets lists the exact target/location references (file://,
	// location:, target: forms) a canonical request may contain.
	AllowedTargets []string
	// AllowedArtifactIDs lists the exact artifact references (artifact:...
	// forms) a canonical request may contain, typically the immutable
	// ArtifactDependency IDs of the published plan.
	AllowedArtifactIDs []string
}

const semanticCanonicalizerVersion = "semantic-parameters-v1"

// ParameterError carries a stable, model-safe rejection code. Its detail is
// intentionally not needed by callers outside trusted diagnostics.
type ParameterError struct {
	Code string
}

func (e *ParameterError) Error() string { return e.Code }

func parameterError(code string) error { return &ParameterError{Code: code} }

// CanonicalizeInvocationArguments parses one model-controlled argument object
// with duplicate-key detection, applies a closed subset of JSON Schema, and
// emits stable JSON. Provider, selection, credential and artifact transport
// fields are always reserved and may never be supplied by the model.
func CanonicalizeInvocationArguments(argsJSON string, schema map[string]interface{}) (CanonicalRequest, error) {
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" {
		argsJSON = "{}"
	}
	value, err := decodeCanonicalJSON([]byte(argsJSON))
	if err != nil {
		return CanonicalRequest{}, err
	}
	values, ok := value.(map[string]interface{})
	if !ok {
		return CanonicalRequest{}, parameterError("parameter_root_not_object")
	}
	if schema == nil {
		return CanonicalRequest{}, parameterError("parameter_schema_missing")
	}
	if err := validateCanonicalObject(values, schema); err != nil {
		return CanonicalRequest{}, err
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return CanonicalRequest{}, parameterError("parameter_canonicalization_failed")
	}
	return CanonicalRequest{
		ProfileVersion: semanticCanonicalizerVersion,
		CanonicalJSON:  encoded,
		Digest:         SchemaDigest(encoded),
		Values:         values,
	}, nil
}

// NewParameterAuthorization derives a stable closed-field authorization from
// the canonical invocation schema. It is deliberately server generated: no
// model argument can add an allowed field.
func NewParameterAuthorization(schema map[string]interface{}) (ParameterAuthorization, error) {
	if schema == nil {
		return ParameterAuthorization{}, parameterError("parameter_schema_missing")
	}
	properties, err := schemaProperties(schema)
	if err != nil {
		return ParameterAuthorization{}, err
	}
	fields := make([]string, 0, len(properties))
	for field := range properties {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	encoded, err := json.Marshal(schema)
	if err != nil {
		return ParameterAuthorization{}, parameterError("parameter_schema_invalid")
	}
	return ParameterAuthorization{Digest: SchemaDigest(encoded), CanonicalizerVer: semanticCanonicalizerVersion, AllowedFields: fields}, nil
}

// NewParameterAuthorizationWithConstraints derives the schema-bound
// authorization and additionally binds the exact target/artifact references
// the invocation may use. Constraints come from trusted plan materialization
// (never from model arguments) and become part of the signed grant payload.
func NewParameterAuthorizationWithConstraints(schema map[string]interface{}, allowedTargets, allowedArtifactIDs []string) (ParameterAuthorization, error) {
	authorization, err := NewParameterAuthorization(schema)
	if err != nil {
		return ParameterAuthorization{}, err
	}
	authorization.AllowedTargets = normalizeAuthorizedReferences(allowedTargets)
	authorization.AllowedArtifactIDs = normalizeAuthorizedReferences(allowedArtifactIDs)
	return authorization, nil
}

func normalizeAuthorizedReferences(refs []string) []string {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(refs))
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	sort.Strings(out)
	return out
}

// CanonicalizeAuthorizedInvocationArguments is the execution boundary for a
// materialized selection. The authorization was derived at catalog publication
// and is bound into the immutable plan/grant; it is never recreated as an
// execution-time policy decision from mutable adapter metadata.
func CanonicalizeAuthorizedInvocationArguments(argsJSON string, schema map[string]interface{}, authorization ParameterAuthorization) (CanonicalRequest, error) {
	expected, err := NewParameterAuthorization(schema)
	if err != nil {
		return CanonicalRequest{}, err
	}
	if !sameParameterAuthorization(authorization, expected) {
		return CanonicalRequest{}, parameterError("parameter_authorization_stale")
	}
	request, err := CanonicalizeInvocationArguments(argsJSON, schema)
	if err != nil {
		return CanonicalRequest{}, err
	}
	if request.ProfileVersion != authorization.CanonicalizerVer {
		return CanonicalRequest{}, parameterError("parameter_authorization_stale")
	}
	if err := ValidateAuthorizedInvocationTargets(request, authorization); err != nil {
		return CanonicalRequest{}, err
	}
	return request, nil
}

// ValidateAuthorizedInvocationTargets enforces the target/artifact half of
// the authorization closure at execution time. Reserved field names are
// already rejected by the canonicalizer; this closes the remaining gap where
// a model smuggles an unauthorized artifact ID or target/location through an
// ordinary string value. Only explicit reference syntax (artifact:,
// file://, location:, target:) is classified, so free-text parameters such as
// search queries are never mistaken for references.
func ValidateAuthorizedInvocationTargets(request CanonicalRequest, authorization ParameterAuthorization) error {
	return validateAuthorizedReferencesInValue(request.Values, authorization)
}

func validateAuthorizedReferencesInValue(value interface{}, authorization ParameterAuthorization) error {
	switch typed := value.(type) {
	case string:
		kind, ref := classifyParameterReference(typed)
		switch kind {
		case "artifact":
			if !authorizedReferenceContains(authorization.AllowedArtifactIDs, ref) {
				return parameterError("parameter_artifact_not_authorized")
			}
		case "target":
			if !authorizedReferenceContains(authorization.AllowedTargets, ref) {
				return parameterError("parameter_target_not_authorized")
			}
		}
	case map[string]interface{}:
		for _, item := range typed {
			if err := validateAuthorizedReferencesInValue(item, authorization); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, item := range typed {
			if err := validateAuthorizedReferencesInValue(item, authorization); err != nil {
				return err
			}
		}
	}
	return nil
}

// classifyParameterReference detects reference syntax in a model-supplied
// string. Detection is case-insensitive against smuggled case variants; the
// allowlist match itself is exact because trusted references are canonical.
func classifyParameterReference(value string) (kind, ref string) {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "artifact:"):
		return "artifact", trimmed
	case strings.HasPrefix(lower, "file://"), strings.HasPrefix(lower, "location:"), strings.HasPrefix(lower, "target:"):
		return "target", trimmed
	default:
		return "", ""
	}
}

func authorizedReferenceContains(allowed []string, ref string) bool {
	for _, candidate := range allowed {
		if candidate == ref {
			return true
		}
	}
	return false
}

// ValidateParameterAuthorization is for catalog/render publication paths. It
// detects schema drift before an invocation grant is issued.
func ValidateParameterAuthorization(schema map[string]interface{}, authorization ParameterAuthorization) error {
	expected, err := NewParameterAuthorization(schema)
	if err != nil {
		return err
	}
	if !sameParameterAuthorization(authorization, expected) {
		return parameterError("parameter_authorization_stale")
	}
	return nil
}

func sameParameterAuthorization(left, right ParameterAuthorization) bool {
	if strings.TrimSpace(left.Digest) == "" || left.Digest != right.Digest || left.CanonicalizerVer != right.CanonicalizerVer || len(left.AllowedFields) != len(right.AllowedFields) {
		return false
	}
	for index := range left.AllowedFields {
		if left.AllowedFields[index] != right.AllowedFields[index] {
			return false
		}
	}
	return true
}

// parameterAuthorizationsEqual compares two immutable plan/grant projections.
// Empty projections exist only for pre-migration in-memory fixtures and cannot
// pass CanonicalizeAuthorizedInvocationArguments; treating two such legacy
// values as equal keeps plan/grant identity checks backward-compatible without
// granting an executable argument surface.
func parameterAuthorizationsEqual(left, right ParameterAuthorization) bool {
	if left.Digest == "" && right.Digest == "" && left.CanonicalizerVer == "" && right.CanonicalizerVer == "" && len(left.AllowedFields) == 0 && len(right.AllowedFields) == 0 && len(left.AllowedTargets) == 0 && len(right.AllowedTargets) == 0 && len(left.AllowedArtifactIDs) == 0 && len(right.AllowedArtifactIDs) == 0 {
		return true
	}
	return sameParameterAuthorization(left, right) && authorizedReferencesEqual(left.AllowedTargets, right.AllowedTargets) && authorizedReferencesEqual(left.AllowedArtifactIDs, right.AllowedArtifactIDs)
}

func authorizedReferencesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func decodeCanonicalJSON(data []byte) (interface{}, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeCanonicalValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, parameterError("parameter_json_invalid")
	}
	return value, nil
}

func decodeCanonicalValue(decoder *json.Decoder) (interface{}, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, parameterError("parameter_json_invalid")
	}
	switch token := token.(type) {
	case json.Delim:
		switch token {
		case '{':
			out := make(map[string]interface{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, parameterError("parameter_json_invalid")
				}
				key, ok := keyToken.(string)
				if !ok || !utf8.ValidString(key) {
					return nil, parameterError("parameter_json_invalid")
				}
				key = norm.NFC.String(key)
				if strings.TrimSpace(key) == "" {
					return nil, parameterError("parameter_json_invalid")
				}
				if _, exists := out[key]; exists {
					return nil, parameterError("parameter_duplicate_key")
				}
				value, err := decodeCanonicalValue(decoder)
				if err != nil {
					return nil, err
				}
				out[key] = value
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
				return nil, parameterError("parameter_json_invalid")
			}
			return out, nil
		case '[':
			out := make([]interface{}, 0)
			for decoder.More() {
				value, err := decodeCanonicalValue(decoder)
				if err != nil {
					return nil, err
				}
				out = append(out, value)
			}
			if end, err := decoder.Token(); err != nil || end != json.Delim(']') {
				return nil, parameterError("parameter_json_invalid")
			}
			return out, nil
		default:
			return nil, parameterError("parameter_json_invalid")
		}
	case string:
		if !utf8.ValidString(token) {
			return nil, parameterError("parameter_json_invalid")
		}
		return norm.NFC.String(token), nil
	case json.Number:
		if _, ok := new(big.Rat).SetString(token.String()); !ok {
			return nil, parameterError("parameter_number_invalid")
		}
		return token, nil
	case bool, nil:
		return token, nil
	default:
		return nil, parameterError("parameter_json_invalid")
	}
}

func validateCanonicalObject(values map[string]interface{}, schema map[string]interface{}) error {
	properties, err := schemaProperties(schema)
	if err != nil {
		return err
	}
	for field, value := range values {
		if reservedInvocationField(field) {
			return parameterError("parameter_reserved_field")
		}
		propertySchema, ok := properties[field]
		if !ok {
			return parameterError("parameter_unknown_field")
		}
		if err := validateCanonicalValue(value, propertySchema); err != nil {
			return err
		}
	}
	for _, required := range schemaStringList(schema["required"]) {
		if _, ok := values[required]; !ok {
			return parameterError("parameter_required_field_missing")
		}
	}
	return nil
}

func schemaProperties(schema map[string]interface{}) (map[string]map[string]interface{}, error) {
	if kind, _ := schema["type"].(string); kind != "" && kind != "object" {
		return nil, parameterError("parameter_schema_invalid")
	}
	raw, ok := schema["properties"]
	if !ok || raw == nil {
		// RegisteredTool historically represents a root object schema as a
		// direct field -> property map. Accept that trusted legacy projection
		// here, but keep the resulting object closed at execution time.
		properties := make(map[string]map[string]interface{}, len(schema))
		for field, rawProperty := range schema {
			if field == "type" || field == "required" || field == "additionalProperties" {
				continue
			}
			property, ok := rawProperty.(map[string]interface{})
			if !ok || strings.TrimSpace(field) == "" || reservedInvocationField(field) {
				return nil, parameterError("parameter_schema_invalid")
			}
			properties[field] = property
		}
		return properties, nil
	}
	propertiesRaw, ok := raw.(map[string]interface{})
	if !ok {
		return nil, parameterError("parameter_schema_invalid")
	}
	properties := make(map[string]map[string]interface{}, len(propertiesRaw))
	for field, rawProperty := range propertiesRaw {
		property, ok := rawProperty.(map[string]interface{})
		if !ok || strings.TrimSpace(field) == "" || reservedInvocationField(field) {
			return nil, parameterError("parameter_schema_invalid")
		}
		properties[field] = property
	}
	return properties, nil
}

func validateCanonicalValue(value interface{}, schema map[string]interface{}) error {
	kind, _ := schema["type"].(string)
	switch kind {
	case "", "any":
		return parameterError("parameter_schema_invalid")
	case "string":
		if _, ok := value.(string); !ok {
			return parameterError("parameter_type_mismatch")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return parameterError("parameter_type_mismatch")
		}
	case "number", "integer":
		number, ok := value.(json.Number)
		if !ok {
			return parameterError("parameter_type_mismatch")
		}
		if kind == "integer" && strings.ContainsAny(number.String(), ".eE") {
			return parameterError("parameter_type_mismatch")
		}
	case "object":
		object, ok := value.(map[string]interface{})
		if !ok {
			return parameterError("parameter_type_mismatch")
		}
		if err := validateCanonicalObject(object, schema); err != nil {
			return err
		}
	case "array":
		items, ok := value.([]interface{})
		if !ok {
			return parameterError("parameter_type_mismatch")
		}
		if itemSchema, ok := schema["items"].(map[string]interface{}); ok {
			for _, item := range items {
				if err := validateCanonicalValue(item, itemSchema); err != nil {
					return err
				}
			}
		} else {
			return parameterError("parameter_schema_invalid")
		}
	default:
		return parameterError("parameter_schema_invalid")
	}
	return nil
}

func schemaStringList(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		if stringsList, ok := value.([]string); ok {
			return append([]string(nil), stringsList...)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if field, ok := item.(string); ok && strings.TrimSpace(field) != "" {
			out = append(out, field)
		}
	}
	return out
}

func reservedInvocationField(field string) bool {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "provider", "provider_id", "server", "server_id", "tool", "tool_name", "selection", "selection_id", "credential", "credentials", "token", "authorization", "artifact", "artifact_ref", "artifactref", "location", "location_ref", "receipt", "confirmation", "plan_id", "root_task_id":
		return true
	default:
		return false
	}
}
