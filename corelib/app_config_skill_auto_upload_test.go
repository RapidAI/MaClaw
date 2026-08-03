package corelib

import "testing"

func TestIsSkillAutoUploadEnabled_DefaultTrue(t *testing.T) {
	var cfg AppConfig
	if !cfg.IsSkillAutoUploadEnabled() {
		t.Fatal("nil SkillAutoUploadEnabled should default to true")
	}
	cfg.SetSkillAutoUploadEnabled(false)
	if cfg.IsSkillAutoUploadEnabled() {
		t.Fatal("expected false after SetSkillAutoUploadEnabled(false)")
	}
	cfg.SetSkillAutoUploadEnabled(true)
	if !cfg.IsSkillAutoUploadEnabled() {
		t.Fatal("expected true after SetSkillAutoUploadEnabled(true)")
	}
}

func TestEffectiveSkillAutoUploadMinSuccesses(t *testing.T) {
	var cfg AppConfig
	if got := cfg.EffectiveSkillAutoUploadMinSuccesses(); got != DefaultSkillAutoUploadMinSuccesses {
		t.Fatalf("zero value should default to %d, got %d", DefaultSkillAutoUploadMinSuccesses, got)
	}
	cfg.SkillAutoUploadMinSuccesses = -1
	if got := cfg.EffectiveSkillAutoUploadMinSuccesses(); got != DefaultSkillAutoUploadMinSuccesses {
		t.Fatalf("negative value should default to %d, got %d", DefaultSkillAutoUploadMinSuccesses, got)
	}
	cfg.SkillAutoUploadMinSuccesses = 5
	if got := cfg.EffectiveSkillAutoUploadMinSuccesses(); got != 5 {
		t.Fatalf("configured value should win, got %d", got)
	}
}
