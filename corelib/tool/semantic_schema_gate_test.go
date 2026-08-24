package tool

import "testing"

func gateCodes(findings []SchemaGateFinding) map[string]string {
	out := make(map[string]string, len(findings))
	for _, finding := range findings {
		out[finding.Pointer] = finding.Code
	}
	return out
}

func TestManagedSchemaGateAcceptsAClosedCapabilityScopedSchema(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command":         map[string]interface{}{"type": "string"},
			"timeout_seconds": map[string]interface{}{"type": "integer"},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
	if findings := InspectManagedInvocationSchema(schema); len(findings) != 0 {
		t.Fatalf("closed capability schema reported %v", findings)
	}
}

func TestManagedSchemaGateRejectsServerBoundFields(t *testing.T) {
	for _, field := range []string{"provider", "server", "tool", "selection", "credential", "token", "receipt", "policy", "confirmation", "artifact_ref"} {
		schema := map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{field: map[string]interface{}{"type": "string"}},
			"additionalProperties": false,
		}
		findings := InspectManagedInvocationSchema(schema)
		if code := gateCodes(findings)[field]; code != SchemaGateReservedField {
			t.Fatalf("field %q classified as %q, want reserved: %v", field, code, findings)
		}
	}
}

func TestManagedSchemaGateRejectsModelSuppliedIdentifiers(t *testing.T) {
	for _, field := range []string{"id", "artifact_id", "selection_id", "chat_id", "chatid", "grant_id"} {
		schema := map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{field: map[string]interface{}{"type": "string"}},
			"additionalProperties": false,
		}
		if code := gateCodes(InspectManagedInvocationSchema(schema))[field]; code != SchemaGateIdentifierField {
			t.Fatalf("identifier %q classified as %q", field, code)
		}
	}
	// A word that merely ends in the same two letters is not an identifier: the
	// gate must not force adapters to rename ordinary parameters.
	for _, field := range []string{"valid", "grid", "rapid"} {
		schema := map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{field: map[string]interface{}{"type": "boolean"}},
			"additionalProperties": false,
		}
		if findings := InspectManagedInvocationSchema(schema); len(findings) != 0 {
			t.Fatalf("ordinary field %q was rejected: %v", field, findings)
		}
	}
}

// Where a provider writes is a location too. The legacy downloader accepted
// save_path, output, dest, path and filename for one destination, so the rule
// has to cover the spellings rather than a single canonical name.
func TestManagedSchemaGateFlagsWriteDestinationsAsLocations(t *testing.T) {
	for _, field := range []string{
		"save_path", "save_to", "save_dir", "output_dir", "output_file",
		"download_path", "local_path", "abs_path", "base_dir", "write_to",
		"base_url", "webhook_url", "callback_url", "links", "ip_address",
	} {
		schema := map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{field: map[string]interface{}{"type": "string"}},
			"additionalProperties": false,
		}
		if code := gateCodes(InspectManagedInvocationSchema(schema))[field]; code != SchemaGateLocationField {
			t.Fatalf("write destination %q classified as %q", field, code)
		}
	}
	// Names that merely contain a location word are not locations themselves,
	// so the rule must stay an exact-name match.
	for _, field := range []string{"format", "output_format", "save_mode", "link_text", "host_language"} {
		schema := map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{field: map[string]interface{}{"type": "string"}},
			"additionalProperties": false,
		}
		if findings := InspectManagedInvocationSchema(schema); len(findings) != 0 {
			t.Fatalf("ordinary field %q was rejected: %v", field, findings)
		}
	}
}

func TestManagedSchemaGateFlagsLocationFieldsSeparately(t *testing.T) {
	for _, field := range []string{"path", "file_path", "directory", "url", "endpoint"} {
		schema := map[string]interface{}{
			"type":                 "object",
			"properties":           map[string]interface{}{field: map[string]interface{}{"type": "string"}},
			"additionalProperties": false,
		}
		if code := gateCodes(InspectManagedInvocationSchema(schema))[field]; code != SchemaGateLocationField {
			t.Fatalf("location field %q classified as %q", field, code)
		}
	}
}

func TestManagedSchemaGateRequiresClosedObjectsAtEveryDepth(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"sheets": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
				},
			},
		},
		"additionalProperties": false,
	}
	codes := gateCodes(InspectManagedInvocationSchema(schema))
	if codes["sheets[]"] != SchemaGateOpenObject {
		t.Fatalf("nested open object was not reported: %v", codes)
	}
}

func TestManagedSchemaGateReachesNestedReservedFields(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"rows": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type":                 "object",
					"properties":           map[string]interface{}{"secret": map[string]interface{}{"type": "string"}},
					"additionalProperties": false,
				},
			},
		},
		"additionalProperties": false,
	}
	if code := gateCodes(InspectManagedInvocationSchema(schema))["rows[].secret"]; code != SchemaGateReservedField {
		t.Fatalf("nested reserved field was not reported: %v", InspectManagedInvocationSchema(schema))
	}
}

func TestManagedSchemaGateTreatsAMissingSchemaAsAFinding(t *testing.T) {
	findings := InspectManagedInvocationSchema(nil)
	if len(findings) != 1 || findings[0].Code != SchemaGateMissingSchema {
		t.Fatalf("missing schema findings=%v", findings)
	}
}

// A parameterless adapter is the safest possible call surface, so the gate must
// not push its author towards inventing fields.
func TestManagedSchemaGateAcceptsAClosedParameterlessSchema(t *testing.T) {
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{},
		"additionalProperties": false,
	}
	if findings := InspectManagedInvocationSchema(schema); len(findings) != 0 {
		t.Fatalf("parameterless schema reported %v", findings)
	}
}
