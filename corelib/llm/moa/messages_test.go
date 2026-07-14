package moa

import (
	"strings"
	"testing"
)

func TestBuildReferenceMessagesStripsTools(t *testing.T) {
	conv := []interface{}{
		map[string]string{"role": "system", "content": "sys"},
		map[string]string{"role": "user", "content": "hello"},
		map[string]interface{}{
			"role": "assistant",
			"tool_calls": []interface{}{
				map[string]interface{}{"id": "1"},
			},
		},
		map[string]interface{}{"role": "tool", "content": "result"},
		map[string]string{"role": "assistant", "content": "done"},
	}
	out := BuildReferenceMessages(conv)
	if len(out) < 2 {
		t.Fatalf("len=%d %#v", len(out), out)
	}
	// system + user hello + assistant done
	roles := make([]string, 0, len(out))
	for _, m := range out {
		mm, ok := m.(map[string]string)
		if !ok {
			t.Fatalf("expected map[string]string, got %T", m)
		}
		roles = append(roles, mm["role"])
	}
	for _, r := range roles {
		if r == "tool" {
			t.Fatal("tool role leaked")
		}
	}
	if roles[0] != "system" || roles[1] != "user" {
		t.Fatalf("roles=%v", roles)
	}
}

func TestInjectAdviceDeepCopyDoesNotMutateOriginal(t *testing.T) {
	origUser := map[string]interface{}{"role": "user", "content": "q"}
	conv := []interface{}{
		map[string]string{"role": "system", "content": "s"},
		origUser,
	}
	advice := FormatAdviceBlock([]RefAdvice{{Label: "a", Content: "think"}})
	out := InjectAdviceDeepCopy(conv, advice)
	if origUser["content"] != "q" {
		t.Fatalf("original mutated: %v", origUser["content"])
	}
	last := out[len(out)-1].(map[string]interface{})
	if last["content"] == "q" {
		t.Fatal("expected advice appended")
	}
}

func TestSanitizeErrorRedactsSecrets(t *testing.T) {
	if got := sanitizeError("Bearer sk-abc123 failed"); got != "auth_or_request_failed" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeError("connection refused"); !strings.Contains(got, "connection") {
		t.Fatalf("got %q", got)
	}
	if got := FormatAdviceBlock(nil); got != "" {
		t.Fatalf("empty items: %q", got)
	}
}

