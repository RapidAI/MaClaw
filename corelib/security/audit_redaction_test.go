package security

import (
	"strings"
	"testing"
)

func TestSanitizeAuditEntryRedactsSecretsAcrossShapes(t *testing.T) {
	entry := AuditEntry{
		Arguments: map[string]interface{}{
			"password": "pw-value",
			"command":  "docker run -e JWT_SECRET='jwt-value' --token token-value --api-key=flag-api-value image && curl -H \"Authorization: Bearer curl-token\" https://user:url-pass@example.test --data '{\"api_key_secret\":\"json-api-value\"}'",
			"headers":  []string{"Authorization: Bearer bearer-value", "Cookie: session=cookie-value"},
			"nested": map[string]interface{}{
				"storage_encryption_key": "encryption-value",
			},
		},
		Result:        "used API_KEY_SECRET=api-value with Authorization: Bearer result-token",
		OutputSnippet: "Cookie: output-cookie",
	}

	sanitized := SanitizeAuditEntry(entry)
	serialized := sanitized.Result + "\n" + sanitized.OutputSnippet + "\n" + stringifyAuditValueForTest(sanitized.Arguments)
	for _, secret := range []string{"pw-value", "jwt-value", "token-value", "flag-api-value", "json-api-value", "url-pass", "curl-token", "bearer-value", "cookie-value", "encryption-value", "api-value", "result-token", "output-cookie"} {
		if containsForTest(serialized, secret) {
			t.Fatalf("sanitized audit entry leaked %q: %s", secret, serialized)
		}
	}
	if !strings.Contains(serialized, "https://user:[REDACTED]@example.test") {
		t.Fatalf("sanitized URL credential lost diagnostic shape: %s", serialized)
	}
	if !strings.Contains(serialized, `"Authorization: [REDACTED]"`) {
		t.Fatalf("sanitized quoted header lost diagnostic shape: %s", serialized)
	}

	categories := RedactedAuditCategories(entry)
	for _, want := range []string{"command", "headers", "output_snippet", "password", "result", "storage_encryption_key"} {
		if !stringSliceContainsForTest(categories, want) {
			t.Fatalf("redaction categories missing %q from %#v", want, categories)
		}
	}
}

func stringifyAuditValueForTest(value interface{}) string {
	switch v := value.(type) {
	case map[string]interface{}:
		out := ""
		for key, item := range v {
			out += key + "=" + stringifyAuditValueForTest(item) + "\n"
		}
		return out
	case []string:
		out := ""
		for _, item := range v {
			out += item + "\n"
		}
		return out
	case string:
		return v
	default:
		return ""
	}
}

func containsForTest(s, substr string) bool {
	return strings.Contains(s, substr)
}

func stringSliceContainsForTest(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
