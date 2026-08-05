package main

import (
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/clientsecurity"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// ---------------------------------------------------------------------------
// Unified config transaction protocol
//
// Problem this solves:
//   Callers previously interleaved configMu, disk I/O, and heavy side effects
//   (data-dir teardown, pet migration persist) in ad-hoc ways. That caused
//   exclusive-lock storms (skills/settings UI stuck on "加载中") and easy-to-
//   miss unlock paths that forgot to drain deferred work.
//
// Protocol (the ONLY safe way to touch config):
//
//   READ (lock-free hot path):
//     1. publishedConfig() → copy *configSnap   // no configMu
//     2. runConfigPostUnlock(snapshot)          // always
//     3. on miss: Lock → loadConfigLocked → Unlock → post-unlock
//
//   WRITE:
//     1. Lock (configMu)
//     2. getConfigForMutationLocked / load
//     3. mutate in-memory copy
//     4. publishConfigLocked(cfg)  // immutable snap + data-dir pointer
//     5. Unlock
//     6. runConfigPostUnlock(cfg)
//     7. persistConfig(path, cfg)  // configDiskMu + AtomicWrite
//
// Rules:
//   - Never hold configMu across AtomicWrite.
//   - Never mutate a configSnap-pointed value after Store (always publish a new copy).
//   - Never run resetPathBoundStateForDataDirChange under configMu.
//   - Never invent a new unlock path that skips runConfigPostUnlock.
// ---------------------------------------------------------------------------

// publishedConfig returns a full config including NLSkills (reattached from
// nlSkillsSnap) without taking configMu. ok=false means cold/invalid.
// Prefer PeekConfig for single-field reads (skills omitted from snap).
func (a *App) publishedConfig() (corelib.AppConfig, bool) {
	if p := a.PeekConfig(); p != nil {
		return a.attachPublishedSkills(*p), true
	}
	return corelib.AppConfig{}, false
}

// PeekConfig returns a pointer to the immutable published config snap, or nil.
// The snap does NOT include NLSkills (see PeekNLSkills). MUST NOT mutate.
func (a *App) PeekConfig() *corelib.AppConfig {
	if a == nil {
		return nil
	}
	if p := a.configSnap.Load(); p != nil {
		return p
	}
	// Legacy / unit-test seed: promote configCache once.
	if a.configCacheValid {
		cp := a.configCache
		// Promote skills if present on legacy cache.
		if len(cp.NLSkills) > 0 && a.nlSkillsSnap.Load() == nil {
			a.publishNLSkillsLocked(cp.NLSkills)
		}
		slim := cp
		slim.NLSkills = nil
		snap := new(corelib.AppConfig)
		*snap = slim
		if a.configSnap.CompareAndSwap(nil, snap) {
			return snap
		}
		if p := a.configSnap.Load(); p != nil {
			return p
		}
	}
	return nil
}

// publishConfigLocked installs cfg as the authoritative in-memory snapshot.
// Caller MUST hold configMu. Skills are stored on nlSkillsSnap; configSnap is
// published without NLSkills so hot-path readers stay small.
func (a *App) publishConfigLocked(cfg corelib.AppConfig) {
	// Full mirror under write lock (includes skills) for mutation helpers.
	a.configCache = cfg
	a.configCacheValid = true
	a.publishNLSkillsLocked(cfg.NLSkills)

	slim := cfg
	slim.NLSkills = nil
	snap := new(corelib.AppConfig)
	*snap = slim
	a.configSnap.Store(snap)
	_ = a.applyDataDirFromConfigLocked(cfg)
}

// runConfigPostUnlock executes all deferred work that is unsafe under configMu.
// Safe and idempotent. Pass the best-known config snapshot (usually the one
// just published or loaded) so deferred disk persist has content to write.
// Hot path: no-op when no deferred work is pending (avoids work on every LoadConfig).
func (a *App) runConfigPostUnlock(snapshot corelib.AppConfig) {
	if a == nil {
		return
	}
	if !a.pendingDataDirReset.Load() && !a.pendingConfigDiskWrite.Load() {
		return
	}
	a.drainPendingDataDirReset()
	a.drainPendingConfigDiskWrite(snapshot)
}

// persistConfig writes the latest cache (or fallback) under configDiskMu.
// Must NOT be called while holding configMu for writing.
func (a *App) persistConfig(path string, fallback corelib.AppConfig) error {
	return a.commitConfigToDisk(path, fallback)
}

// unlockConfigAndFinish is the standard WRITE epilogue:
// publish (already done or do it), unlock, post-unlock side effects, optional persist.
// Caller must hold configMu for writing when calling this.
func (a *App) unlockConfigAndFinish(cfg corelib.AppConfig, path string, persist bool) error {
	a.publishConfigLocked(cfg)
	a.configMu.Unlock()
	a.runConfigPostUnlock(cfg)
	if !persist {
		return nil
	}
	if path == "" {
		var err error
		path, err = a.getConfigPath()
		if err != nil {
			return err
		}
	}
	return a.persistConfig(path, cfg)
}

// unlockConfigAbort releases configMu after a failed/aborted mutation and still
// drains any side effects that loadConfigLocked may have staged (data-dir, pet).
// Caller must hold configMu for writing.
func (a *App) unlockConfigAbort(snapshot corelib.AppConfig) {
	a.configMu.Unlock()
	a.runConfigPostUnlock(snapshot)
}

// configMutateOpts controls shared write-path sanitization.
type configMutateOpts struct {
	allowHubManagedSecurity bool
	caller                  string
}

// mutateConfig is the unified write API for small field updates (PatchConfig).
// It owns the full lock/publish/unlock/persist lifecycle so call sites cannot
// forget post-unlock drains.
func (a *App) mutateConfig(patchFn func(cfg *corelib.AppConfig), opts configMutateOpts) error {
	_, err := a.mutateConfigMaybe(func(cfg *corelib.AppConfig) bool {
		patchFn(cfg)
		return true
	}, opts)
	return err
}

// mutateConfigMaybe is PatchConfigIfChanged via the unified protocol.
func (a *App) mutateConfigMaybe(patchFn func(cfg *corelib.AppConfig) bool, opts configMutateOpts) (bool, error) {
	caller := opts.caller
	if caller == "" {
		caller = configPatchCaller(2)
	}

	a.configMu.Lock()
	cfg, err := a.getConfigForMutationLocked()
	if err != nil {
		a.unlockConfigAbort(corelib.AppConfig{})
		return false, err
	}
	current := cfg
	changed := patchFn(&cfg)
	if !changed {
		// Re-publish current so snap/legacy mirrors stay consistent after a cold load.
		a.publishConfigLocked(current)
		a.unlockConfigAbort(current)
		if configPatchShouldLog(caller) {
			log.Printf("[config] PatchConfig:skip_no_change caller=%q", caller)
		}
		return false, nil
	}
	if !opts.allowHubManagedSecurity && a.shouldPreserveHubManagedSecurity(current) {
		clientsecurity.PreserveHubManagedSecurityConfig(current, &cfg)
	} else if !opts.allowHubManagedSecurity && a.hubSecurityExplicitlyCentralizedFalse() {
		cfg.HubSecurityCentralized = false
	}
	if err := validateHardwareGatewayInvariant(cfg); err != nil {
		a.unlockConfigAbort(current)
		return false, err
	}
	sanitizeCodingToolSelection(&cfg)
	normalizeConfigTimeouts(&cfg)
	path, err := a.getConfigPath()
	if err != nil {
		a.unlockConfigAbort(current)
		return false, err
	}
	if err := a.unlockConfigAndFinish(cfg, path, true); err != nil {
		a.configMu.Lock()
		a.invalidateConfigCacheLocked()
		a.configMu.Unlock()
		return false, err
	}
	if configPatchShouldLog(caller) {
		log.Printf("[config] PatchConfig:done caller=%q", caller)
	}
	return true, nil
}

// loadConfigSnapshot is the unified read API used by LoadConfig.
// Hot path is lock-free (atomic snap). Always runs post-unlock drains so
// deferred writer work is never stranded.
//
// NOTE: the hot path returns the slim snap without NLSkills, while the cold
// paths below reattach them via publishedConfig/attachPublishedSkills. This
// inconsistency is the root cause of a family of bugs (any caller doing
// LoadConfig -> mutate -> SaveConfig drops the skill table; see the failing
// TestNLSkillsSplitFromConfigSnap expectation). Attaching skills here fixes
// those, but flips provenance/upgrade decisions in the maclaw app install
// flow (TestInstallMaclawAppDependencies{UpgradesKnownLegacyLocalDependencyForMarketApp,
// DerivesSkillMarketProvenanceFromInstalledWrapper,DerivesProvenanceFromLegacyStableWrapperPanelID,
// DoesNotAcceptResolvedMetadataForInstalledWrapper,DoesNotAcceptBundleForInstalledWrapper}
// and TestRecordExperienceDraftReviewReturnsSkillDraftExecutionPreview),
// whose logic was tuned to the slim hot path. Resolving the semantic conflict
// (attach-everywhere vs slim-by-design) needs the install-flow owner's call.
func (a *App) loadConfigSnapshot() (corelib.AppConfig, error) {
	if p := a.PeekConfig(); p != nil {
		// Apply log gates from the snap without copying the whole AppConfig first.
		corelib.SetLogDetailEnabled(p.LogDetailEnabled)
		memory.SetMemoryRecallLogEnabled(p.MemoryRecallLogEnabled)
		a.runConfigPostUnlock(*p)
		return *p, nil
	}

	// Cold path: exclusive load from disk.
	lockStart := time.Now()
	a.configMu.Lock()
	lockWait := time.Since(lockStart)
	if lockWait > 50*time.Millisecond {
		log.Printf("[config] LoadConfig:lock_wait=%s", lockWait)
	}
	// Another writer may have published while we waited.
	if cfg, ok := a.publishedConfig(); ok {
		a.configMu.Unlock()
		corelib.SetLogDetailEnabled(cfg.LogDetailEnabled)
		memory.SetMemoryRecallLogEnabled(cfg.MemoryRecallLogEnabled)
		a.runConfigPostUnlock(cfg)
		return cfg, nil
	}
	config, err := a.loadConfigLocked()
	if err != nil {
		a.configMu.Unlock()
		a.runConfigPostUnlock(corelib.AppConfig{})
		return corelib.AppConfig{}, err
	}
	// loadConfigLocked already calls publishConfigLocked.
	a.workflowDisabled.Store(!config.IsWorkflowEnabled())
	a.configMu.Unlock()
	a.runConfigPostUnlock(config)
	return config, nil
}

// configPatchShouldLog suppresses high-frequency PatchConfig success logs
// (token usage flush) that otherwise drown out real config events.
func configPatchShouldLog(caller string) bool {
	if strings.Contains(caller, "flushPendingTokenUsage") ||
		strings.Contains(caller, "AccumulateLLMTokenUsage") {
		return false
	}
	return true
}
