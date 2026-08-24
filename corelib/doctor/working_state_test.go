package doctor

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestWorkingStateCheck_On(t *testing.T) {
	t.Setenv(agent.WorkingStateEnvKey, "")
	c := WorkingStateCheck()
	if c.ID != "agent.working_state" || c.Status != StatusOK {
		t.Fatalf("%+v", c)
	}
	if c.Detail["enabled"] != true {
		t.Fatalf("detail=%#v", c.Detail)
	}
}

func TestWorkingStateCheck_Off(t *testing.T) {
	t.Setenv(agent.WorkingStateEnvKey, "off")
	c := WorkingStateCheck()
	if c.ID != "agent.working_state" || c.Status != StatusInfo {
		t.Fatalf("%+v", c)
	}
	if c.Detail["enabled"] != false {
		t.Fatalf("detail=%#v", c.Detail)
	}
}

func TestRunIncludesWorkingStateCheck(t *testing.T) {
	dir := t.TempDir()
	report := Run(Input{
		Config: corelib.AppConfig{
			MaclawLLMUrl:   "http://x",
			MaclawLLMModel: "m",
			MaclawLLMKey:   "k",
			OnboardingDone: true,
		},
		BaseDir: dir,
	})
	if !hasCheck(report, "agent.working_state", StatusOK) && !hasCheck(report, "agent.working_state", StatusInfo) {
		t.Fatalf("missing agent.working_state: %+v", report.Checks)
	}
}
