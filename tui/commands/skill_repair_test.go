package commands

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

type skillRepairMemoryStore struct {
	cfg corelib.AppConfig
}

func (s *skillRepairMemoryStore) LoadConfig() (corelib.AppConfig, error) {
	return s.cfg, nil
}

func (s *skillRepairMemoryStore) SaveConfig(cfg corelib.AppConfig) error {
	s.cfg = cfg
	return nil
}

func TestMaybeRepairSkillTUISkipsFileBackedSkill(t *testing.T) {
	formatted := skill.FormatErrorForLLM(skill.ClassifiedError{Class: skill.ErrCommandNotFound, UserMessage: "missing cmd", Repairable: true})
	entry := &corelib.NLSkillEntry{
		Name:      "file-repairable",
		Source:    "file",
		SkillDir:  t.TempDir(),
		LastError: formatted,
	}
	started := MaybeRepairSkillTUI(entry, corelib.AppConfig{MaclawLLMProviders: []corelib.MaclawLLMProvider{{URL: "http://127.0.0.1", Key: "key", Model: "model"}}}, &skillRepairMemoryStore{})

	if started {
		t.Fatal("file-backed repair should require reviewed patch flow, not background repair")
	}
}

func TestMaybeRepairSkillTUIReportsNotStartedWhenLLMUnavailable(t *testing.T) {
	formatted := skill.FormatErrorForLLM(skill.ClassifiedError{Class: skill.ErrCommandNotFound, UserMessage: "missing cmd", Repairable: true})
	entry := &corelib.NLSkillEntry{Name: "repairable", Source: "manual", LastError: formatted}
	started := MaybeRepairSkillTUI(entry, corelib.AppConfig{}, &skillRepairMemoryStore{})

	if started {
		t.Fatal("repair should not report started without LLM configuration")
	}
}
