package moa

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestShouldActivateAuto(t *testing.T) {
	cases := []struct {
		name string
		auto bool
		task llm.TaskType
		tier string
		want bool
	}{
		{"off", false, llm.TaskReasoning, "c3", false},
		{"fast blocked", true, llm.TaskFast, "c3", false},
		{"intent blocked", true, llm.TaskIntent, "", false},
		{"summary blocked", true, llm.TaskSummary, "c3", false},
		{"reasoning ok", true, llm.TaskReasoning, "c2", true},
		{"c3 default ok", true, llm.TaskDefault, "c3", true},
		{"default no tier", true, llm.TaskDefault, "c2", false},
		{"vision c3", true, llm.TaskVision, "c3", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldActivateAuto(tc.auto, tc.task, tc.tier); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestEnvEffectiveEnabled(t *testing.T) {
	t.Setenv("MACLAW_MOA", "")
	if !EnvAllows() {
		t.Fatal("unset should allow (config-driven)")
	}
	if !EffectiveEnabled(true) {
		t.Fatal("config on + unset env")
	}
	if EffectiveEnabled(false) {
		t.Fatal("config off")
	}
	t.Setenv("MACLAW_MOA", "off")
	if EnvAllows() || EffectiveEnabled(true) {
		t.Fatal("forced off")
	}
	t.Setenv("MACLAW_MOA", "on")
	if !EffectiveEnabled(true) {
		t.Fatal("forced on + config")
	}
}
