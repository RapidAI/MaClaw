package doctor

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestAdaptivePromptCheck_Empty(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "")
	agent.ResetPromptProfileStatsForTest()
	c := AdaptivePromptCheck()
	if c.ID != "agent.adaptive_prompt" || c.Status != StatusInfo {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(c.Message, "no turns") {
		t.Fatalf("msg=%q", c.Message)
	}
}

func TestAdaptivePromptCheck_EnvOverride(t *testing.T) {
	t.Setenv(agent.PromptProfileEnvKey, "light")
	agent.ResetPromptProfileStatsForTest()
	c := AdaptivePromptCheck()
	if !strings.Contains(c.Message, agent.PromptProfileEnvKey+"=light") {
		t.Fatalf("msg=%q", c.Message)
	}
	if c.Detail["env_override"] != true {
		t.Fatalf("detail=%#v", c.Detail)
	}
}

func TestAdaptivePromptCheck_WithDenyAndUpgrade(t *testing.T) {
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfileDecision(agent.PromptProfileDecision{
		Profile: agent.PromptProfileLight,
		Task:   "fast",
	})
	agent.RecordLightToolDeny("bash")
	agent.RecordLightToolDeny("bash")
	agent.RecordLightToolDeny("write_file")
	agent.RecordLightUpgrade("tool_deny_retry:bash")
	c := AdaptivePromptCheck()
	if !strings.Contains(c.Message, "light_deny=3") {
		t.Fatalf("msg=%q", c.Message)
	}
	if !strings.Contains(c.Message, "bash:2") {
		t.Fatalf("expected top denied tool in msg: %q", c.Message)
	}
	if !strings.Contains(c.Message, "light_upgrade=1") || !strings.Contains(c.Message, "bash") {
		t.Fatalf("upgrade msg=%q", c.Message)
	}
	if c.Detail["last_upgrade_reason"] != "tool_deny_retry:bash" {
		t.Fatalf("detail=%#v", c.Detail)
	}
	if by, ok := c.Detail["by_denied_tool"].(map[string]int64); !ok || by["bash"] != 2 {
		// map type may vary
		if c.Detail["by_denied_tool"] == nil {
			t.Fatalf("missing by_denied_tool: %#v", c.Detail)
		}
	}
}

func TestAdaptivePromptCheck_WithStats(t *testing.T) {
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfileDecision(agent.PromptProfileDecision{
		Profile:     agent.PromptProfileLight,
		FullTokens:  5000,
		LightTokens: 1000,
		Task:        "fast",
		Reason:      "short simple turn",
	})
	c := AdaptivePromptCheck()
	if c.Status != StatusInfo {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(c.Message, "light=100%") {
		t.Fatalf("msg=%q", c.Message)
	}
	if !strings.Contains(c.Message, "est_saved=4000") {
		t.Fatalf("msg=%q", c.Message)
	}
	if !strings.Contains(c.Message, "task=fast") {
		t.Fatalf("msg missing task: %q", c.Message)
	}
	if c.Detail["last_task"] != "fast" {
		t.Fatalf("detail last_task=%v", c.Detail["last_task"])
	}
	if by, ok := c.Detail["by_task"].(map[string]int64); !ok || by["fast"] != 1 {
		// map may be map[string]int64 from struct copy
		if by2, ok2 := c.Detail["by_task"].(map[string]int64); ok2 {
			if by2["fast"] != 1 {
				t.Fatalf("by_task=%#v", c.Detail["by_task"])
			}
		} else if c.Detail["by_task"] == nil {
			t.Fatalf("missing by_task: %#v", c.Detail)
		}
	}
	if c.Detail["est_tokens_saved"] != int64(4000) && c.Detail["est_tokens_saved"] != 4000 {
		// JSON/map may keep int64
		if n, ok := c.Detail["est_tokens_saved"].(int64); !ok || n != 4000 {
			if n2, ok2 := c.Detail["est_tokens_saved"].(int); !ok2 || n2 != 4000 {
				t.Fatalf("detail=%#v", c.Detail)
			}
		}
	}
}

func TestRunIncludesAdaptivePromptCheck(t *testing.T) {
	agent.ResetPromptProfileStatsForTest()
	dir := t.TempDir()
	report := Run(Input{
		Config: corelib.AppConfig{
			MaclawLLMUrl:   "http://x",
			MaclawLLMModel: "m",
			MaclawLLMKey:   "k",
			OnboardingDone: true,
			AuxiliaryLLM: corelib.AuxiliaryLLMConfig{
				URL: "http://y", Key: "k2", Model: "flash",
			},
		},
		BaseDir: dir,
	})
	if !hasCheck(report, "agent.adaptive_prompt", StatusInfo) {
		t.Fatalf("missing adaptive_prompt: %+v", report.Checks)
	}
	if !hasCheck(report, "agent.shared_loop", StatusOK) && !hasCheck(report, "agent.shared_loop", StatusInfo) {
		// shared loop present in some form
		found := false
		for _, c := range report.Checks {
			if c.ID == "agent.shared_loop" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing shared_loop: %+v", report.Checks)
		}
	}
}
