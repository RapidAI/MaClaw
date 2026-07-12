package main

import (
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// ensureEvolutionPipeline lazily starts a shared EvolutionPipeline for TUI
// skill self-repair (and future optimize/promote). Safe for concurrent callers.
func (app *TUIApp) ensureEvolutionPipeline() {
	if app == nil {
		return
	}
	app.evolutionOnce.Do(func() {
		pipeline := skill.NewEvolutionPipeline()
		pipeline.EnableOptimizer = true
		pipeline.EnablePromoter = true
		pipeline.EnableRepair = true
		pipeline.RepairCooldown = skill.RepairCooldownFromHours(app.appConfig.SkillEvolutionRepairCooldownHours)
		pipeline.Versioner = &skill.Versioner{}

		if trackerPath := coretool.DefaultUsageTrackerPath(); trackerPath != "" {
			if tracker, err := coretool.NewUsageTracker(trackerPath); err == nil {
				pipeline.UsageTracker = tracker
			} else {
				log.Printf("[tui-evolution] usage tracker unavailable: %v", err)
			}
		}

		store := commands.NewFileConfigStore(commands.ResolveDataDir())
		pipeline.SkillLoader = func() []corelib.NLSkillEntry {
			if fresh, err := store.LoadConfig(); err == nil {
				app.appConfig = fresh
				return append([]corelib.NLSkillEntry(nil), fresh.NLSkills...)
			}
			return append([]corelib.NLSkillEntry(nil), app.appConfig.NLSkills...)
		}
		pipeline.SkillSaver = func(skills []corelib.NLSkillEntry) error {
			fresh, err := store.LoadConfig()
			if err != nil {
				fresh = app.appConfig
			}
			fresh.NLSkills = skills
			app.appConfig = fresh
			return store.SaveConfig(fresh)
		}
		llmAdapter := &tuiEvolutionLLMAdapter{loadCfg: func() corelib.AppConfig {
			if fresh, err := store.LoadConfig(); err == nil {
				return fresh
			}
			return app.appConfig
		}}
		pipeline.LLM = llmAdapter
		pipeline.Gate = skill.NewRepairGate(skill.RepairGateConfig{}, skill.NewDefaultSandboxExecutor())
		pipeline.Optimizer = skill.NewSkillOptimizer(pipeline.LLM, pipeline.Gate, pipeline.Versioner)
		if skillsDir, err := skill.PrimarySkillsDir(); err == nil {
			pipeline.Promoter = skill.NewNudgePromoter(
				pipeline.LLM,
				skill.NewAutoPromotionStagingValidator(),
				&tuiSkillRegistrar{app: app, store: store},
				skillsDir,
			)
		}
		pipeline.EventEmitter = func(event string, data map[string]string) {
			skill.RecordEvolutionEvent(event, data, "tui")
			log.Printf("[tui-evolution] event=%s data=%v", event, data)
		}

		pipeline.RepairHook = func(entry *corelib.NLSkillEntry, runArgs map[string]string) {
			if entry == nil {
				return
			}
			cfg := app.appConfig
			if fresh, err := store.LoadConfig(); err == nil {
				cfg = fresh
			}
			commands.PerformTUISkillRepair(entry, cfg, store)
			if refreshed, err := store.LoadConfig(); err == nil {
				app.appConfig = refreshed
			}
		}

		pipeline.Start()
		app.evolutionPipeline = pipeline
		log.Printf("[tui-evolution] pipeline started repair_cooldown=%s optimizer=%v promoter=%v",
			pipeline.RepairCooldown, pipeline.EnableOptimizer, pipeline.EnablePromoter && pipeline.Promoter != nil)
	})
}

// notifySkillEvolution feeds a completed TUI skill run into the evolution pipeline.
func (app *TUIApp) notifySkillEvolution(entry *corelib.NLSkillEntry, success bool, runArgs map[string]string) {
	if app == nil || entry == nil {
		return
	}
	if commands.SkillEvolutionDisabled() {
		return
	}
	if !app.appConfig.IsSkillEvolutionEnabled() {
		return
	}
	app.ensureEvolutionPipeline()
	if app.evolutionPipeline == nil {
		return
	}
	app.evolutionPipeline.NotifySkillExecution(entry.Name, entry, &skill.SkillExecutionResultCompat{
		Success:       success,
		OutputQuality: "basic",
	}, runArgs)
}

// tuiEvolutionLLMAdapter implements skill.LLMRepairer for the evolution pipeline.
type tuiEvolutionLLMAdapter struct {
	loadCfg func() corelib.AppConfig
}

func (a *tuiEvolutionLLMAdapter) ChatCall(messages []map[string]string) (string, error) {
	cfg := corelib.AppConfig{}
	if a != nil && a.loadCfg != nil {
		cfg = a.loadCfg()
	}
	return commands.NewTUISkillRepairerFromAppConfig(cfg).ChatCall(messages)
}

func (a *tuiEvolutionLLMAdapter) IsConfigured() bool {
	cfg := corelib.AppConfig{}
	if a != nil && a.loadCfg != nil {
		cfg = a.loadCfg()
	}
	return commands.NewTUISkillRepairerFromAppConfig(cfg).IsConfigured()
}

// tuiSkillRegistrar registers auto-promoted skills into the TUI NLSkills config.
type tuiSkillRegistrar struct {
	app   *TUIApp
	store commands.ConfigStore
}

func (r *tuiSkillRegistrar) RegisterSkill(entry *corelib.NLSkillEntry) error {
	if r == nil || entry == nil || r.store == nil {
		return fmt.Errorf("skill registrar not available")
	}
	cfg, err := r.store.LoadConfig()
	if err != nil {
		return err
	}
	for _, s := range cfg.NLSkills {
		if s.MatchesName(entry.Name) {
			return fmt.Errorf("skill %q already registered", entry.Name)
		}
	}
	cfg.NLSkills = append(cfg.NLSkills, *entry)
	if err := r.store.SaveConfig(cfg); err != nil {
		return err
	}
	if r.app != nil {
		r.app.appConfig = cfg
	}
	return nil
}
