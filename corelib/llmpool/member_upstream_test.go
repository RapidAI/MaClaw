package llmpool

import "testing"

func TestNormalizeMemberUpstreamOverridesCanonicalizes(t *testing.T) {
	extra, priority, drop, err := NormalizeMemberUpstreamOverrides(
		map[string]any{"reasoning_effort": "none", "nested": map[string]any{"model": "inner"}},
		"MEMBER",
		[]string{" thinking ", "thinking"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if extra["reasoning_effort"] != "none" || priority != "" {
		t.Fatalf("extra=%#v priority=%q", extra, priority)
	}
	nested, _ := extra["nested"].(map[string]any)
	if nested["model"] != "inner" {
		t.Fatalf("nested model = %#v", nested)
	}
	if len(drop) != 1 || drop[0] != "thinking" {
		t.Fatalf("drop = %#v", drop)
	}

	cleared, clearedPriority, clearedDrop, err := NormalizeMemberUpstreamOverrides(map[string]any{}, "downstream", []string{})
	if err != nil || cleared != nil || clearedPriority != MemberExtraBodyPriorityDownstream || clearedDrop != nil {
		t.Fatalf("clear extra=%#v priority=%q drop=%#v err=%v", cleared, clearedPriority, clearedDrop, err)
	}
}

func TestNormalizeMemberUpstreamOverridesRejectsProtectedAndInvalid(t *testing.T) {
	if _, _, _, err := NormalizeMemberUpstreamOverrides(map[string]any{"model": "x"}, "", nil); err == nil {
		t.Fatal("expected protected extra_body key to be rejected")
	}
	if _, _, _, err := NormalizeMemberUpstreamOverrides(nil, "high", nil); err == nil {
		t.Fatal("expected invalid priority to be rejected")
	}
	if _, _, _, err := NormalizeMemberUpstreamOverrides(nil, "", []string{"messages"}); err == nil {
		t.Fatal("expected protected drop field to be rejected")
	}
	if _, _, _, err := NormalizeMemberUpstreamOverrides(map[string]any{
		"reasoning_effort":  "high",
		" reasoning_effort": "none",
	}, "", nil); err == nil {
		t.Fatal("expected trimmed duplicate key to be rejected")
	}
}
