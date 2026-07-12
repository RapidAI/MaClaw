package commands

import (
	"log"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// sharedEvolution is a process-wide EvolutionPipeline for CLI/headless skill
// runs that do not have a TUIApp instance (e.g. `maclaw-tui skill run`).
var (
	sharedEvolutionMu   sync.Mutex
	sharedEvolution     *skill.EvolutionPipeline
	sharedEvolutionOnce sync.Once

	// sessionEvolutionDisabled is an in-process opt-out (nlskill evolution disable).
	// Cleared only when the process exits or evolution enable is called.
	sessionEvolutionDisabled atomic.Bool
)

// SkillEvolutionDisabled reports whether skill evolution is opted out via:
//   - env MACLAW_DISABLE_SKILL_EVOLUTION=1|true|yes|on
//   - session flag (nlskill evolution disable)
// Used by CLI and TUI agent paths as a global kill switch.
func SkillEvolutionDisabled() bool {
	if sessionEvolutionDisabled.Load() {
		return true
	}
	return skill.EvolutionEnvDisabled()
}

// SetSkillEvolutionSessionDisabled toggles in-process evolution for this session.
func SetSkillEvolutionSessionDisabled(disabled bool) {
	sessionEvolutionDisabled.Store(disabled)
}

// SkillEvolutionSessionDisabled reports the in-process session flag only.
func SkillEvolutionSessionDisabled() bool {
	return sessionEvolutionDisabled.Load()
}

// SkillEvolutionEnvDisabled reports the environment kill switch only.
func SkillEvolutionEnvDisabled() bool {
	return skill.EvolutionEnvDisabled()
}

// SharedEvolutionStatus returns a diagnostic map for the shared CLI pipeline.
// Pipeline may be nil if never started. Includes env/session/config kill layers.
func SharedEvolutionStatus() map[string]interface{} {
	configEnabled := true
	cooldownHours := 1
	if store := NewFileConfigStore(ResolveDataDir()); store != nil {
		if cfg, err := store.LoadConfig(); err == nil {
			configEnabled = cfg.IsSkillEvolutionEnabled()
			if cfg.SkillEvolutionRepairCooldownHours > 0 {
				cooldownHours = cfg.SkillEvolutionRepairCooldownHours
			}
		}
	}
	disabled := SkillEvolutionDisabled() || !configEnabled
	out := map[string]interface{}{
		"session_disabled": SkillEvolutionSessionDisabled(),
		"env_disabled":     SkillEvolutionEnvDisabled(),
		"config_enabled":   configEnabled,
		"config_disabled":  !configEnabled,
		"disabled":         disabled,
		"pipeline_started": false,
		"repair_cooldown_hours": cooldownHours,
	}
	sharedEvolutionMu.Lock()
	p := sharedEvolution
	sharedEvolutionMu.Unlock()
	if p == nil {
		return out
	}
	st := p.Status()
	out["pipeline_started"] = true
	out["pending_skills"] = st.PendingSkills
	out["coalesced_notifications"] = st.CoalescedNotifications
	out["dropped_notifications"] = st.DroppedNotifications
	out["processed_requests"] = st.ProcessedRequests
	// Effective flags: pipeline wiring AND not fully disabled.
	out["enable_repair"] = st.EnableRepair && !disabled
	out["enable_optimizer"] = st.EnableOptimizer && !disabled
	out["enable_promoter"] = st.EnablePromoter && !disabled
	out["repair_cooldown"] = st.RepairCooldown.String()
	out["has_repair_hook"] = st.HasRepairHook
	out["has_optimizer"] = st.HasOptimizer
	out["has_promoter"] = st.HasPromoter
	return out
}

// PersistSkillEvolutionEnabled writes skill_evolution_enabled to the user config.
func PersistSkillEvolutionEnabled(enabled bool) error {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return err
	}
	cfg.SetSkillEvolutionEnabled(enabled)
	return store.SaveConfig(cfg)
}

// NotifySharedSkillEvolution feeds a completed skill run into the shared
// EvolutionPipeline (self-repair + optional optimize). Safe from any goroutine.
// No-ops when SkillEvolutionDisabled() is true.
func NotifySharedSkillEvolution(cfg corelib.AppConfig, store ConfigStore, entry *corelib.NLSkillEntry, success bool, runArgs map[string]string) {
	if entry == nil || store == nil {
		return
	}
	if SkillEvolutionDisabled() {
		return
	}
	// Refresh cooldown / enable flag from latest config if present on store reload.
	if fresh, err := store.LoadConfig(); err == nil {
		cfg = fresh
	}
	if !cfg.IsSkillEvolutionEnabled() {
		return
	}
	p := ensureSharedEvolutionPipeline(cfg, store)
	if p == nil {
		return
	}
	p.RepairCooldown = skill.RepairCooldownFromHours(cfg.SkillEvolutionRepairCooldownHours)
	p.NotifySkillExecution(entry.Name, entry, &skill.SkillExecutionResultCompat{
		Success:       success,
		OutputQuality: "basic",
	}, runArgs)
}

func ensureSharedEvolutionPipeline(cfg corelib.AppConfig, store ConfigStore) *skill.EvolutionPipeline {
	sharedEvolutionOnce.Do(func() {
		pipeline := skill.NewEvolutionPipeline()
		pipeline.EnableRepair = true
		pipeline.EnablePromoter = false // no nudge UX in CLI
		pipeline.EnableOptimizer = true
		pipeline.RepairCooldown = skill.RepairCooldownFromHours(cfg.SkillEvolutionRepairCooldownHours)
		pipeline.Versioner = &skill.Versioner{}
		pipeline.Gate = skill.NewRepairGate(skill.RepairGateConfig{}, skill.NewDefaultSandboxExecutor())

		if trackerPath := coretool.DefaultUsageTrackerPath(); trackerPath != "" {
			if tracker, err := coretool.NewUsageTracker(trackerPath); err == nil {
				pipeline.UsageTracker = tracker
			} else {
				log.Printf("[cli-evolution] usage tracker unavailable: %v", err)
			}
		}

		pipeline.SkillLoader = func() []corelib.NLSkillEntry {
			fresh, err := store.LoadConfig()
			if err != nil {
				return nil
			}
			return append([]corelib.NLSkillEntry(nil), fresh.NLSkills...)
		}
		pipeline.SkillSaver = func(skills []corelib.NLSkillEntry) error {
			fresh, err := store.LoadConfig()
			if err != nil {
				return err
			}
			fresh.NLSkills = skills
			return store.SaveConfig(fresh)
		}
		llmAdapter := &sharedEvolutionLLM{store: store, fallback: cfg}
		pipeline.LLM = llmAdapter
		pipeline.Optimizer = skill.NewSkillOptimizer(pipeline.LLM, pipeline.Gate, pipeline.Versioner)
		pipeline.EventEmitter = func(event string, data map[string]string) {
			skill.RecordEvolutionEvent(event, data, "cli")
			log.Printf("[cli-evolution] event=%s data=%v", event, data)
		}
		pipeline.RepairHook = func(entry *corelib.NLSkillEntry, runArgs map[string]string) {
			if entry == nil {
				return
			}
			PerformTUISkillRepair(entry, llmAdapter.loadCfg(), store)
		}

		pipeline.Start()
		sharedEvolutionMu.Lock()
		sharedEvolution = pipeline
		sharedEvolutionMu.Unlock()
		log.Printf("[cli-evolution] shared pipeline started cooldown=%s optimizer=%v", pipeline.RepairCooldown, pipeline.EnableOptimizer)
	})

	sharedEvolutionMu.Lock()
	defer sharedEvolutionMu.Unlock()
	return sharedEvolution
}

type sharedEvolutionLLM struct {
	store    ConfigStore
	fallback corelib.AppConfig
}

func (a *sharedEvolutionLLM) loadCfg() corelib.AppConfig {
	if a != nil && a.store != nil {
		if fresh, err := a.store.LoadConfig(); err == nil {
			return fresh
		}
	}
	if a != nil {
		return a.fallback
	}
	return corelib.AppConfig{}
}

func (a *sharedEvolutionLLM) ChatCall(messages []map[string]string) (string, error) {
	return NewTUISkillRepairerFromAppConfig(a.loadCfg()).ChatCall(messages)
}

func (a *sharedEvolutionLLM) IsConfigured() bool {
	return NewTUISkillRepairerFromAppConfig(a.loadCfg()).IsConfigured()
}
