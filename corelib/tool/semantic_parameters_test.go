package tool

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalRegisteredToolInvocationSchemaNormalizesLegacyPropertyMaps(t *testing.T) {
	schema, err := CanonicalRegisteredToolInvocationSchema(map[string]interface{}{
		"query":       map[string]string{"type": "string", "description": "search text"},
		"max_results": map[string]string{"type": "integer", "description": "limit"},
	}, []string{"query"})
	if err != nil {
		t.Fatal(err)
	}
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties=%#v", schema["properties"])
	}
	if _, ok := properties["query"].(map[string]interface{}); !ok {
		t.Fatalf("query property was not JSON-normalized: %#v", properties["query"])
	}
	if _, err := NewParameterAuthorization(schema); err != nil {
		t.Fatalf("canonical schema cannot be authorized: %v", err)
	}
	request, err := CanonicalizeInvocationArguments(`{"query":"Beijing weather","max_results":5}`, schema)
	if err != nil || !strings.Contains(string(request.CanonicalJSON), `"query":"Beijing weather"`) {
		t.Fatalf("request=%+v err=%v", request, err)
	}
}

func TestCanonicalizeInvocationArgumentsRejectsDuplicateUnknownAndReservedFields(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"display": map[string]interface{}{"type": "integer"},
		},
		"additionalProperties": false,
	}
	for _, test := range []struct {
		name string
		json string
		code string
	}{
		{"duplicate", `{"display":1,"display":2}`, "parameter_duplicate_key"},
		{"unknown", `{"other":1}`, "parameter_unknown_field"},
		{"reserved", `{"provider":"other"}`, "parameter_reserved_field"},
		{"wrong type", `{"display":"primary"}`, "parameter_type_mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := CanonicalizeInvocationArguments(test.json, schema)
			if err == nil || !strings.Contains(err.Error(), test.code) {
				t.Fatalf("CanonicalizeInvocationArguments(%s) error=%v, want %s", test.json, err, test.code)
			}
		})
	}
}

func TestCanonicalizeInvocationArgumentsProducesStableClosedJSON(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"label": map[string]interface{}{"type": "string"},
			"count": map[string]interface{}{"type": "integer"},
		},
		"additionalProperties": false,
	}
	first, err := CanonicalizeInvocationArguments(`{"count":2,"label":"screen"}`, schema)
	if err != nil {
		t.Fatalf("first canonicalization: %v", err)
	}
	second, err := CanonicalizeInvocationArguments(` { "label" : "screen", "count" : 2 } `, schema)
	if err != nil {
		t.Fatalf("second canonicalization: %v", err)
	}
	if got, want := string(first.CanonicalJSON), `{"count":2,"label":"screen"}`; got != want {
		t.Fatalf("canonical json=%s, want %s", got, want)
	}
	if first.Digest != second.Digest || string(first.CanonicalJSON) != string(second.CanonicalJSON) {
		t.Fatalf("equivalent input produced unstable result: %#v vs %#v", first, second)
	}
}

func TestCanonicalizeInvocationArgumentsNormalizesUnicodeNFC(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"label": map[string]interface{}{"type": "string"},
		},
	}
	composed, err := CanonicalizeInvocationArguments(`{"label":"é"}`, schema)
	if err != nil {
		t.Fatalf("composed canonicalization: %v", err)
	}
	decomposed, err := CanonicalizeInvocationArguments(`{"label":"e\u0301"}`, schema)
	if err != nil {
		t.Fatalf("decomposed canonicalization: %v", err)
	}
	if composed.Digest != decomposed.Digest || string(composed.CanonicalJSON) != string(decomposed.CanonicalJSON) {
		t.Fatalf("NFC-equivalent values diverged: %s vs %s", composed.CanonicalJSON, decomposed.CanonicalJSON)
	}
}

func TestCanonicalizeInvocationArgumentsEnforcesNestedClosedObjects(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"filter": map[string]interface{}{
				"type":                 "object",
				"properties":           map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
				"additionalProperties": false,
			},
		},
	}
	_, err := CanonicalizeInvocationArguments(`{"filter":{"name":"ok","server":"forged"}}`, schema)
	if err == nil || !strings.Contains(err.Error(), "parameter_reserved_field") {
		t.Fatalf("nested reserved field err=%v", err)
	}
}

func TestCanonicalizeAuthorizedInvocationArgumentsRejectsSchemaDrift(t *testing.T) {
	published := map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		}, "required": []string{"query"}, "additionalProperties": false,
	}
	authorization, err := NewParameterAuthorization(published)
	if err != nil {
		t.Fatal(err)
	}
	request, err := CanonicalizeAuthorizedInvocationArguments(`{"query":"Beijing weather"}`, published, authorization)
	if err != nil || string(request.CanonicalJSON) != `{"query":"Beijing weather"}` {
		t.Fatalf("authorized request=%+v err=%v", request, err)
	}
	drifted := map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
			"limit": map[string]interface{}{"type": "integer"},
		}, "required": []string{"query"}, "additionalProperties": false,
	}
	if _, err := CanonicalizeAuthorizedInvocationArguments(`{"query":"Beijing weather"}`, drifted, authorization); err == nil || err.Error() != "parameter_authorization_stale" {
		t.Fatalf("schema drift error=%v", err)
	}
}

// Shape failures against the rendered schema must localize the offending
// field: the path is what lets the model correct the call instead of guessing
// (2026-08-27 birthday-deck turn: three blind parameter_schema_invalid
// retries on a slide-level subtitle, then the no-progress breaker killed the
// turn). Error() still returns the bare code for stable diagnostics.
func TestCanonicalizeInvocationArgumentsLocalizesShapeErrors(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string"},
			"slides": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"title":   map[string]interface{}{"type": "string"},
						"bullets": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					},
					"required":             []string{"title"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
	for _, test := range []struct {
		name string
		json string
		code string
		path string
	}{
		{"nested unknown field", `{"path":"a.pptx","slides":[{"title":"t","subtitle":"x"}]}`, "parameter_unknown_field", "slides[0].subtitle"},
		{"nested type mismatch", `{"path":"a.pptx","slides":[{"title":"t","bullets":"x"}]}`, "parameter_type_mismatch", "slides[0].bullets"},
		{"nested required missing", `{"path":"a.pptx","slides":[{"bullets":["x"]}]}`, "parameter_required_field_missing", "slides[0].title"},
		{"root required missing", `{"slides":[]}`, "parameter_required_field_missing", "path"},
		{"reserved field", `{"path":"a.pptx","slides":[],"tool_name":"x"}`, "parameter_reserved_field", "tool_name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := CanonicalizeInvocationArguments(test.json, schema)
			if err == nil {
				t.Fatal("expected a rejection")
			}
			if err.Error() != test.code {
				t.Fatalf("Error()=%q, want bare code %q", err.Error(), test.code)
			}
			var paramErr *ParameterError
			if !errors.As(err, &paramErr) {
				t.Fatalf("error type %T, want *ParameterError", err)
			}
			if paramErr.Path != test.path {
				t.Fatalf("Path=%q, want %q", paramErr.Path, test.path)
			}
			if strings.TrimSpace(paramErr.Hint) == "" {
				t.Fatal("shape errors must carry a model-facing hint")
			}
		})
	}
}

// Authorization-closure failures stay detail-free: the allowlists are
// server-side and must not leak into model-facing refusals.
func TestCanonicalizeAuthorizedInvocationArgumentsKeepsClosureErrorsGeneric(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
		},
	}
	authorization, err := NewParameterAuthorization(schema)
	if err != nil {
		t.Fatal(err)
	}
	_, err = CanonicalizeAuthorizedInvocationArguments(`{"query":"file:///etc/passwd"}`, schema, authorization)
	var paramErr *ParameterError
	if !errors.As(err, &paramErr) || paramErr.Code != "parameter_target_not_authorized" {
		t.Fatalf("err=%v", err)
	}
	if paramErr.Path != "" || paramErr.Hint != "" {
		t.Fatalf("closure errors must stay narrowed: %+v", paramErr)
	}
}
