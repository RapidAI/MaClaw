package database

import (
	"encoding/json"
	"strings"
	"testing"
)

// mustToolResult decodes a successful JSON tool result or fails the test with
// the raw handler output (error paths return plain text instead of JSON).
// Shared by the SQL integration tests (dbintegration tag) and the Windows
// Access fixture test, so it intentionally carries no build tag itself.
func mustToolResult[T any](t *testing.T, raw string) T {
	t.Helper()
	var out T
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		t.Fatalf("expected JSON result, got: %s", raw)
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode tool result: %v (raw: %s)", err, raw)
	}
	return out
}
