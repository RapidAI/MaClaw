package security

import (
	"strings"
	"testing"
)

// RedactMetadataMap backs the agentservice Service.recordAudit path, which
// previously persisted metadata verbatim into agentservice_state.json
// (P0-3 of the 2026-09-08 review).
func TestRedactMetadataMap(t *testing.T) {
	in := map[string]string{
		"password":      "hunter2",
		"api_key":       "sk-live-abcdef",
		"authorization": "Bearer tok",
		"token":         "t0ken",
		"secret":        "s3cret",
		"scope":         "tenant",
		"message":       "user set api_key=leaked-value",
	}

	out := RedactMetadataMap(in)

	for _, key := range []string{"password", "api_key", "authorization", "token", "secret"} {
		if got := out[key]; got != auditRedactedValue {
			t.Errorf("metadata[%q] = %q, want %q", key, got, auditRedactedValue)
		}
	}
	// Non-sensitive keys have their *values* scanned for embedded secrets.
	// This is safe now that redactAuditString is complete and idempotent —
	// before that fix, scanning here corrupted the text and made the
	// export-time pass leak more than doing nothing at all.
	if out["scope"] != "tenant" {
		t.Errorf("metadata[\"scope\"] = %q, want %q", out["scope"], "tenant")
	}
	if got := out["message"]; strings.Contains(got, "leaked-value") {
		t.Errorf("metadata[\"message\"] leaked an embedded secret: %q", got)
	}

	// The input map must not be mutated — callers reuse it after auditing.
	if in["password"] != "hunter2" {
		t.Errorf("RedactMetadataMap mutated its input: %q", in["password"])
	}
}

func TestRedactMetadataMapNilAndEmpty(t *testing.T) {
	if got := RedactMetadataMap(nil); got != nil {
		t.Errorf("RedactMetadataMap(nil) = %#v, want nil", got)
	}
	if got := RedactMetadataMap(map[string]string{}); got != nil {
		t.Errorf("RedactMetadataMap(empty) = %#v, want nil", got)
	}
}

// Boolean values cannot carry secret material. Redacting them under
// secret-named keys destroys the diagnostic flag (include_secrets,
// token_set, admin_secret_set) an admin needs to read back.
func TestRedactMetadataMapKeepsBooleanDiagnosticValues(t *testing.T) {
	out := RedactMetadataMap(map[string]string{
		"include_secrets":  "true",
		"include_messages": "false",
		"token_set":        "true",
		"admin_secret_set": "false",
		"password":         "hunter2",
		"api_key":          "true",  // a bare boolean token is not secret material, whatever the key
		"secret":           "TRUE",  // only the exact JSON spelling passes
		"token":            " true", // no whitespace tolerance
		"authorization":    "true ", // ditto
	})
	if out["include_secrets"] != "true" || out["include_messages"] != "false" {
		t.Errorf("boolean diagnostic values were redacted: include_secrets=%q include_messages=%q", out["include_secrets"], out["include_messages"])
	}
	if out["token_set"] != "true" || out["admin_secret_set"] != "false" || out["api_key"] != "true" {
		t.Errorf("boolean token values must pass through: token_set=%q admin_secret_set=%q api_key=%q", out["token_set"], out["admin_secret_set"], out["api_key"])
	}
	if out["password"] != auditRedactedValue || out["secret"] != auditRedactedValue || out["token"] != auditRedactedValue || out["authorization"] != auditRedactedValue {
		t.Errorf("non-boolean secret values must stay redacted: %#v", out)
	}

	// Same rule for the interface{} sanitizers and category collection.
	if got := SanitizeSensitiveValue("include_secrets", "true"); got != "true" {
		t.Errorf("SanitizeSensitiveValue boolean = %v, want true", got)
	}
	if got := SanitizeSensitiveValue("include_secrets", true); got != true {
		t.Errorf("SanitizeSensitiveValue typed bool = %v, want true", got)
	}
	if got := SanitizeSensitiveValue("include_secrets", "yes"); got != auditRedactedValue {
		t.Errorf("SanitizeSensitiveValue non-boolean = %v, want %q", got, auditRedactedValue)
	}
	categories := RedactedAuditCategories(AuditEntry{Arguments: map[string]interface{}{"include_secrets": "true", "password": "hunter2"}})
	if stringSliceForTest(categories, "include_secrets") {
		t.Errorf("boolean diagnostic key should not be a redaction category: %#v", categories)
	}
	if !stringSliceForTest(categories, "password") {
		t.Errorf("sensitive key should still be a redaction category: %#v", categories)
	}
}

func stringSliceForTest(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
