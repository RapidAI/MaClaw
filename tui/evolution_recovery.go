package main

// Startup recovery for TUI-owned Skill transactions.  The compensation queue
// is process-global because GUI, TUI and headless services share the data
// directory, so recovery must be narrowed by both action prefix and scope.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// recoverTUIEvolutionCompensations replays only records created by TUI
// install/uninstall, auto-fix validation and skillhub CLI config transactions
// for this data root.
// It deliberately does not claim legacy unscoped records: without a durable
// owner or an in-root path, attributing those rows to this process would risk
// restoring another service's Skill registry.
func recoverTUIEvolutionCompensations(dataDir string) (recovered, pending int, err error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return 0, 0, fmt.Errorf("TUI data directory is empty")
	}
	scope, err := filepath.Abs(dataDir)
	if err != nil {
		return 0, 0, fmt.Errorf("resolve TUI recovery scope: %w", err)
	}
	store := commands.NewFileConfigStore(scope)
	load := func() []corelib.NLSkillEntry {
		cfg, loadErr := store.LoadConfig()
		if loadErr != nil {
			return nil
		}
		return cfg.NLSkills
	}
	save := func(entries []corelib.NLSkillEntry) error {
		cfg, loadErr := store.LoadConfig()
		if loadErr != nil {
			return loadErr
		}
		cfg.NLSkills = entries
		return store.SaveConfig(cfg)
	}

	// Coordinate with in-process TUI mutations.  Cross-process safety is
	// provided by the durable queue and scope/path validation in corelib.
	tuiSkillMutationMu.Lock()
	defer tuiSkillMutationMu.Unlock()
	pipeline := &skill.EvolutionPipeline{SkillLoader: load, SkillSaver: save}
	for _, prefix := range []string{"tui_install", "tui_uninstall", "tui_validate", "tui_upload", "tui_cli_"} {
		r, p, recoverErr := pipeline.RecoverPendingCompensationsForActionPrefixAndScopeWithExternalRecovery(prefix, scope, nil)
		recovered += r
		pending += p
		if recoverErr != nil {
			return recovered, pending, fmt.Errorf("recover %s compensations: %w", prefix, recoverErr)
		}
	}
	return recovered, pending, nil
}

// runTUIStartupRecovery is intentionally best-effort at process startup:
// unreadable or still-pending rows are reported, while mutation paths remain
// fail-closed through their own queue admission checks.
func runTUIStartupRecovery(dataDir string) {
	recovered, pending, err := recoverTUIEvolutionCompensations(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: TUI Skill recovery unavailable (mutations remain blocked): %v\n", err)
		return
	}
	if recovered > 0 || pending > 0 {
		fmt.Fprintf(os.Stderr, "TUI Skill recovery: recovered=%d pending=%d\n", recovered, pending)
	}
}
