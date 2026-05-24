package commands

import (
	"strings"
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
	entry := &corelib.NLSkillEntry{Name: "repairable", Source: "manual", UsageCount: 3, FailureCount: 3, LastError: formatted}
	started := MaybeRepairSkillTUI(entry, corelib.AppConfig{}, &skillRepairMemoryStore{})

	if started {
		t.Fatal("repair should not report started without LLM configuration")
	}
}

func TestRepairLLMConfigFromAppConfigUsesCurrentProvider(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "configured",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "empty", URL: "", Key: "", Model: ""},
			{Name: "configured", URL: "http://127.0.0.1:11434/v1", Key: "sk-test", Model: "qwen", Protocol: "openai", WireAPI: "chat"},
		},
	}

	got := repairLLMConfigFromAppConfig(cfg)
	if got.ProviderName != "configured" || got.URL != "http://127.0.0.1:11434/v1" || got.Key != "sk-test" || got.Model != "qwen" || got.WireAPI != "chat" {
		t.Fatalf("repair config = %#v, want current configured provider", got)
	}
}

func TestRepairLLMConfigFromAppConfigFallsBackWhenCurrentMissing(t *testing.T) {
	cfg := corelib.AppConfig{
		MaclawLLMCurrentProvider: "missing",
		MaclawLLMProviders:       []corelib.MaclawLLMProvider{{Name: "fallback", URL: "http://127.0.0.1:11434/v1", Model: "qwen"}},
	}

	got := repairLLMConfigFromAppConfig(cfg)
	if got.ProviderName != "fallback" || got.URL == "" || got.Model != "qwen" {
		t.Fatalf("repair config = %#v, want fallback provider", got)
	}
}

func TestTUISkillRepairerAllowsLocalModelWithoutKey(t *testing.T) {
	repairer := &tuiSkillRepairer{cfg: corelib.MaclawLLMConfig{URL: "http://127.0.0.1:11434/v1", Model: "qwen"}}
	if !repairer.IsConfigured() {
		t.Fatal("local model with URL and model should be considered configured")
	}
}

func TestPersistTUISkillRepairResultKeepsRepairMetadata(t *testing.T) {
	store := &skillRepairMemoryStore{cfg: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{Name: "repairable", Status: "active"}}}}
	entry := &corelib.NLSkillEntry{
		Name:               "repairable",
		Status:             "active",
		LastError:          "auto-repaired: fixed command",
		RepairAttemptCount: 2,
		LastRepairAt:       "2026-05-23T10:00:00Z",
		Steps:              []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "echo fixed"}}},
		RepairHistory:      []corelib.SkillRepairRecord{{Timestamp: "2026-05-23T10:00:00Z", Explanation: "fixed command", Success: false}},
	}

	if err := persistTUISkillRepairResult(store, entry); err != nil {
		t.Fatalf("persistTUISkillRepairResult() error = %v", err)
	}
	got := store.cfg.NLSkills[0]
	if got.RepairAttemptCount != 2 || got.LastRepairAt == "" || len(got.RepairHistory) != 1 || len(got.Steps) != 1 {
		t.Fatalf("persisted repair metadata = %#v, want steps/count/history", got)
	}
}

func TestPersistTUISkillRepairResultKeepsDisableResult(t *testing.T) {
	store := &skillRepairMemoryStore{cfg: corelib.AppConfig{NLSkills: []corelib.NLSkillEntry{{Name: "bad-skill", Status: "active"}}}}
	entry := &corelib.NLSkillEntry{
		Name:               "bad-skill",
		Status:             "needs_review",
		LastError:          "auto-disabled: impossible",
		RepairAttemptCount: 1,
		LastRepairAt:       "2026-05-23T10:00:00Z",
		RepairHistory:      []corelib.SkillRepairRecord{{Timestamp: "2026-05-23T10:00:00Z", Explanation: "impossible", Success: false}},
	}

	if err := persistTUISkillRepairResult(store, entry); err != nil {
		t.Fatalf("persistTUISkillRepairResult() error = %v", err)
	}
	got := store.cfg.NLSkills[0]
	if got.Status != "needs_review" || !strings.Contains(got.LastError, "auto-disabled") || got.RepairAttemptCount != 1 || len(got.RepairHistory) != 1 {
		t.Fatalf("persisted disable result = %#v", got)
	}
}
