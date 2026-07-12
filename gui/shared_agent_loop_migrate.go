package main

import (
	"log"

	"github.com/RapidAI/CodeClaw/corelib"
)

// migrateSharedAgentLoopOnce enables shared_agent_loop_enabled for existing
// installs that have not yet been migrated. Safe to call multiple times.
//
// Skips when MACLAW_SHARED_AGENT_LOOP is explicitly set (operator owns the
// mode via env). Still marks migration complete only when config is written.
func (a *App) migrateSharedAgentLoopOnce() {
	if a == nil {
		return
	}
	// Operator env override: do not rewrite config under their feet.
	if stringsTrimEnv("MACLAW_SHARED_AGENT_LOOP") != "" {
		return
	}
	changed, err := a.PatchConfigIfChanged(func(cfg *corelib.AppConfig) bool {
		return corelib.ApplySharedAgentLoopMigration(cfg)
	})
	if err != nil {
		log.Printf("[migrate] shared agent loop: %v", err)
		return
	}
	if changed {
		log.Printf("[migrate] shared agent loop enabled for existing install (one-time)")
	}
}
