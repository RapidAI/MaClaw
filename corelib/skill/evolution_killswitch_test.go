package skill

import "testing"

func TestEvolutionEnvDisabled(t *testing.T) {
	t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "")
	if EvolutionEnvDisabled() {
		t.Fatal("empty env should not disable")
	}
	for _, v := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", v)
		if !EvolutionEnvDisabled() {
			t.Fatalf("%q should disable evolution", v)
		}
	}
	for _, v := range []string{"0", "false", "no", "off"} {
		t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", v)
		if EvolutionEnvDisabled() {
			t.Fatalf("%q should not disable evolution", v)
		}
	}
}
