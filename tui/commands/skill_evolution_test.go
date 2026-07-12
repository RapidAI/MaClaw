package commands

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

type memConfigStore struct {
	cfg corelib.AppConfig
}

func (m *memConfigStore) LoadConfig() (corelib.AppConfig, error) { return m.cfg, nil }
func (m *memConfigStore) SaveConfig(cfg corelib.AppConfig) error {
	m.cfg = cfg
	return nil
}

func TestSkillEvolutionDisabled(t *testing.T) {
	// Reset session flag so tests don't leak.
	SetSkillEvolutionSessionDisabled(false)
	t.Cleanup(func() { SetSkillEvolutionSessionDisabled(false) })

	t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "")
	if SkillEvolutionDisabled() {
		t.Fatal("empty env should not disable")
	}
	t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "1")
	if !SkillEvolutionDisabled() {
		t.Fatal("1 should disable")
	}
	t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "true")
	if !SkillEvolutionDisabled() {
		t.Fatal("true should disable")
	}
	t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "no")
	if SkillEvolutionDisabled() {
		t.Fatal("no should not disable")
	}

	// Session flag alone disables.
	t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "")
	SetSkillEvolutionSessionDisabled(true)
	if !SkillEvolutionDisabled() || !SkillEvolutionSessionDisabled() {
		t.Fatal("session disable should disable evolution")
	}
	SetSkillEvolutionSessionDisabled(false)
	if SkillEvolutionDisabled() {
		t.Fatal("session enable should clear disable")
	}
}

func TestSharedEvolutionStatusShape(t *testing.T) {
	SetSkillEvolutionSessionDisabled(false)
	t.Cleanup(func() { SetSkillEvolutionSessionDisabled(false) })
	t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "")
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)

	st := SharedEvolutionStatus()
	if st["disabled"] != false {
		t.Fatalf("disabled = %v, want false", st["disabled"])
	}
	if st["config_enabled"] != true {
		t.Fatalf("config_enabled = %v, want true (default)", st["config_enabled"])
	}
	if st["config_disabled"] != false {
		t.Fatalf("config_disabled = %v, want false", st["config_disabled"])
	}
	if _, ok := st["pipeline_started"]; !ok {
		t.Fatal("missing pipeline_started")
	}
}

func TestNLSkillEvolutionPersistEnableDisable(t *testing.T) {
	SetSkillEvolutionSessionDisabled(false)
	t.Cleanup(func() { SetSkillEvolutionSessionDisabled(false) })
	t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "")
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)

	if err := nlskillEvolution([]string{"disable", "--persist"}); err != nil {
		t.Fatalf("disable --persist: %v", err)
	}
	if !SkillEvolutionSessionDisabled() {
		t.Fatal("session should be disabled")
	}
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.IsSkillEvolutionEnabled() {
		t.Fatal("config should have skill_evolution_enabled=false")
	}
	st := SharedEvolutionStatus()
	if st["disabled"] != true || st["config_disabled"] != true {
		t.Fatalf("status after disable: %#v", st)
	}

	if err := nlskillEvolution([]string{"enable", "--persist"}); err != nil {
		t.Fatalf("enable --persist: %v", err)
	}
	if SkillEvolutionSessionDisabled() {
		t.Fatal("session should be enabled")
	}
	cfg, err = store.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig after enable: %v", err)
	}
	if !cfg.IsSkillEvolutionEnabled() {
		t.Fatal("config should have skill_evolution_enabled=true")
	}
	st = SharedEvolutionStatus()
	if st["disabled"] != false || st["config_enabled"] != true {
		t.Fatalf("status after enable: %#v", st)
	}
}

func TestNLSkillEvolutionSessionOnlyDisable(t *testing.T) {
	SetSkillEvolutionSessionDisabled(false)
	t.Cleanup(func() { SetSkillEvolutionSessionDisabled(false) })
	t.Setenv("MACLAW_DISABLE_SKILL_EVOLUTION", "")
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)

	// Ensure config starts enabled (default).
	_ = PersistSkillEvolutionEnabled(true)

	if err := nlskillEvolution([]string{"disable"}); err != nil {
		t.Fatalf("session disable: %v", err)
	}
	if !SkillEvolutionSessionDisabled() {
		t.Fatal("session should be disabled")
	}
	cfg, err := NewFileConfigStore(ResolveDataDir()).LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.IsSkillEvolutionEnabled() {
		t.Fatal("session-only disable must not write config")
	}
}

func TestNotifySharedSkillEvolution_DoesNotPanic(t *testing.T) {
	// Reset once for isolation — only safe in this single test process.
	// We just verify Notify is non-blocking and queues work.
	store := &memConfigStore{cfg: corelib.AppConfig{
		NLSkills: []corelib.NLSkillEntry{{
			Name:        "cli-skill",
			Source:      "hub",
			Status:      "active",
			UsageCount:  1,
			LastError:   "[class: command_not_found] foo",
			Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "foo"}}},
		}},
		SkillEvolutionRepairCooldownHours: 1,
	}}
	entry := store.cfg.NLSkills[0]
	// First notify starts the pipeline; no LLM configured so repair hook may no-op.
	NotifySharedSkillEvolution(store.cfg, store, &entry, false, map[string]string{"input": "x"})
	// Give the worker a moment to wake without asserting repair outcome.
	time.Sleep(50 * time.Millisecond)
	if sharedEvolution == nil {
		// once.Do may still be racing if Notify returned early — wait a bit more
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && sharedEvolution == nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if sharedEvolution == nil {
		t.Fatal("shared evolution pipeline was not started")
	}
	// Cooldown helper is independent.
	if skill.RepairCooldownFromHours(store.cfg.SkillEvolutionRepairCooldownHours) != time.Hour {
		t.Fatal("unexpected cooldown")
	}
}
