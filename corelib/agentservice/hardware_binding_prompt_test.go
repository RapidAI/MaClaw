package agentservice

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHardwareBindingPromptInjectsExpertAndInitialPrompt(t *testing.T) {
	cb := &coreAgentCallbacks{instance: Instance{Metadata: map[string]string{
		"hardware_assistant_mode":       "expert",
		"hardware_expert_name":          "Support",
		"hardware_expert_system_prompt": "Only answer supported product questions.",
		"hardware_initial_prompt":       "Escalate uncertain answers to a human.",
		"hardware_expert_tools_json":    `["read_file"]`,
	}}}
	prompt := cb.hardwareBindingPrompt()
	for _, want := range []string{"Hardware-bound assistant policy", "Support", "Only answer supported product questions.", "Escalate uncertain answers to a human."} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("hardware prompt missing %q: %s", want, prompt)
		}
	}
	allowed := cb.hardwareExpertToolAllowSet()
	if !allowed["read_file"] || len(allowed) != 1 {
		t.Fatalf("allowed tool set = %#v", allowed)
	}
	if _, err := json.Marshal(allowed); err != nil {
		t.Fatalf("tool set should be serializable: %v", err)
	}
}

func TestHardwareExpertToolRestrictionAppliesToRuntimeAuthorization(t *testing.T) {
	cb := &coreAgentCallbacks{instance: Instance{Metadata: map[string]string{
		"hardware_assistant_mode":    "expert",
		"hardware_expert_tools_json": `["read_file"]`,
	}}}
	if cb.IsToolAllowed("write_file") {
		t.Fatal("a tool omitted by the hardware expert must not be authorized")
	}
	if ok, reason := cb.IsToolCallAllowed("write_file", `{}`); ok || !strings.Contains(reason, "selected hardware expert") {
		t.Fatalf("tool-call restriction = (%v, %q)", ok, reason)
	}
}

func TestHardwareExpertSkillRestrictionAppliesToSkillRuns(t *testing.T) {
	cb := &coreAgentCallbacks{instance: Instance{Metadata: map[string]string{
		"hardware_assistant_mode":     "expert",
		"hardware_expert_tools_json":  `["manage_skill"]`,
		"hardware_expert_skills_json": `["weather"]`,
	}}}
	if ok, reason := cb.hardwareExpertToolCallAllowed("manage_skill", map[string]interface{}{"action": "run", "name": "shell-admin"}); ok || !strings.Contains(reason, "not allowed") {
		t.Fatalf("skill restriction = (%v, %q)", ok, reason)
	}
	if ok, reason := cb.hardwareExpertToolCallAllowed("manage_skill", map[string]interface{}{"action": "run", "name": "weather"}); !ok || reason != "" {
		t.Fatalf("allowed skill = (%v, %q)", ok, reason)
	}
}
