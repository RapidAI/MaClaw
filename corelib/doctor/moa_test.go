package doctor

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestMoACheckDisabled(t *testing.T) {
	t.Setenv("MACLAW_MOA", "")
	c := MoACheck(corelib.AppConfig{})
	if c.ID != "llm.moa" || c.Status != StatusInfo {
		t.Fatalf("%+v", c)
	}
}

func TestMoACheckReady(t *testing.T) {
	t.Setenv("MACLAW_MOA", "")
	c := MoACheck(corelib.AppConfig{
		MoA: corelib.MoAConfig{
			Enabled:       true,
			DefaultPreset: "review",
			Presets: map[string]corelib.MoAPresetConfig{
				"review": {
					Enabled:         true,
					Aggregator:      corelib.MoAModelRef{UsePrimary: true},
					ReferenceModels: []corelib.MoAModelRef{{Provider: "OpenAI"}},
				},
			},
		},
	})
	if c.Status != StatusOK {
		t.Fatalf("%+v", c)
	}
}

func TestMoACheckEnvOff(t *testing.T) {
	t.Setenv("MACLAW_MOA", "off")
	c := MoACheck(corelib.AppConfig{MoA: corelib.MoAConfig{Enabled: true}})
	if c.Status != StatusInfo {
		t.Fatalf("%+v", c)
	}
}
