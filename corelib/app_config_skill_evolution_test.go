package corelib

import "testing"

func TestIsSkillEvolutionEnabled_DefaultDisabled(t *testing.T) {
	var cfg AppConfig
	if cfg.IsSkillEvolutionEnabled() {
		t.Fatal("nil SkillEvolutionEnabled should default to false")
	}
	cfg.SetSkillEvolutionEnabled(true)
	if !cfg.IsSkillEvolutionEnabled() {
		t.Fatal("expected true after SetSkillEvolutionEnabled(true)")
	}
	cfg.SetSkillEvolutionEnabled(false)
	if cfg.IsSkillEvolutionEnabled() {
		t.Fatal("expected false after SetSkillEvolutionEnabled(false)")
	}
}
