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
