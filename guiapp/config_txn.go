package guiapp

import (
	"log"
	"os"
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
//     7. persistConfig(path, cfg)  // configDiskMu + storage transaction
//
// Rules:
//   - Never hold configMu across storage writes.
//   - Never mutate a configSnap-pointed value after Store (always publish a new copy).
//   - Never run resetPathBoundStateForDataDirChange under configMu.
//   - Never invent a new unlock path that skips runConfigPostUnlock.
// ---------------------------------------------------------------------------

type configFileFingerprint struct {
	path         string
	size         int64
	modTimeNanos int64
	valid        bool
}

func newConfigFileFingerprint(path string, info os.FileInfo) *configFileFingerprint {
	fingerprint := &configFileFingerprint{path: path}
	if info != nil {
		fingerprint.size = info.Size()
		fingerprint.modTimeNanos = info.ModTime().UnixNano()
		fingerprint.valid = true
	}
	return fingerprint
}

func (f *configFileFingerprint) matches(path string, info os.FileInfo) bool {
	return f != nil && f.valid && info != nil && f.path == path &&
		f.size == info.Size() && f.modTimeNanos == info.ModTime().UnixNano()
}

// configCacheMatchesDisk validates disk-backed snapshots without taking
// configMu. Nil metadata belongs to an in-memory bootstrap or a legacy seed.
// During our own transaction, the published snapshot remains authoritative.
func (a *App) configCacheMatchesDisk() bool {
	if a.configWritesInFlight.Load() > 0 {
		return true
	}
	fingerprint := a.configFileState.Load()
	if fingerprint == nil {
		return true
	}
	jsonPath, err := a.getConfigPath()
	if err != nil {
		return false
	}
	path := a.configStoreForPath(jsonPath).StoragePath()
	if path != fingerprint.path {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && fingerprint.matches(path, info)
}

func (a *App) recordConfigFileStateAfterRead(path string, before os.FileInfo) {
	after, err := os.Stat(path)
	beforeFingerprint := newConfigFileFingerprint(path, before)
	if err != nil || !beforeFingerprint.matches(path, after) {
		a.configFileState.Store(&configFileFingerprint{path: path})
		return
	}
	a.configFileState.Store(newConfigFileFingerprint(path, after))
}

func (a *App) recordConfigFileStateAfterWrite(path string) {
	info, err := os.Stat(path)
	if err != nil {
		a.configFileState.Store(&configFileFingerprint{path: path})
		return
	}
	a.configFileState.Store(newConfigFileFingerprint(path, info))
}

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

	// Legacy / unit-test seed: configCache is a write-side mirror protected by
	// configMu. A cold peek must take that lock before reading or promoting it;
	// otherwise it races with publishConfigLocked. The usual hot path remains
	// lock-free because it returns above once configSnap has been published.
	a.configMu.Lock()
	defer a.configMu.Unlock()
	return a.promoteLegacyConfigCacheLocked()
}

// promoteLegacyConfigCacheLocked promotes a pre-snapshot configCache seed.
// Caller MUST hold configMu. Keeping it separate lets the cold LoadConfig path
// inspect configSnap directly while it already owns configMu, avoiding a
// non-reentrant lock through PeekConfig.
func (a *App) promoteLegacyConfigCacheLocked() *corelib.AppConfig {
	if p := a.configSnap.Load(); p != nil {
		return p
	}
	if !a.configCacheValid {
		return nil
	}
	cp := a.configCache
	// Promote skills if present on legacy cache.
	if len(cp.NLSkills) > 0 && a.nlSkillsSnap.Load() == nil {
		a.publishNLSkillsLocked(cp.NLSkills)
	}
	slim := cp
	slim.NLSkills = nil
	snap := new(corelib.AppConfig)
	*snap = slim
	a.configSnap.Store(snap)
	return snap
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
	if persist {
		// Mark the write window before releasing configMu so a concurrent
		// cold load that lost its fingerprint cannot publish stale disk
		// contents over this freshly published snapshot while the persist is
		// still queued (see loadConfigLocked's in-flight guard).
		a.configWritesInFlight.Add(1)
	}
	a.configMu.Unlock()
	a.runConfigPostUnlock(cfg)
	if !persist {
		return nil
	}
	defer a.configWritesInFlight.Add(-1)
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
	corelib.ApplyZhipuCodingConfigMigration(&cfg)
	remapLegacyZhipuCodingProfileModels(cfg.MaclawLLMProfiles, cfg.MaclawLLMProviders)
	if !opts.allowHubManagedSecurity && a.shouldPreserveHubManagedSecurity(current) {
		clientsecurity.PreserveHubManagedSecurityConfig(current, &cfg)
	} else if !opts.allowHubManagedSecurity && a.hubSecurityExplicitlyCentralizedFalse() {
		cfg.HubSecurityCentralized = false
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
// Hot path is lock-free (atomic snap plus one mtime/size stat). Always runs
// post-unlock drains so deferred writer work is never stranded.
//
// configSnap intentionally omits NLSkills so single-field readers stay cheap.
// LoadConfig is the full-config API, however, and must return the same shape
// on both hot and cold paths. Reattach the separately published skill table
// before returning so a read-modify-save caller cannot silently discard it.
func (a *App) loadConfigSnapshot() (corelib.AppConfig, error) {
	if p := a.PeekConfig(); p != nil && a.configCacheMatchesDisk() {
		// Apply log gates from the snap without copying the whole AppConfig first.
		corelib.SetLogDetailEnabled(p.LogDetailEnabled)
		memory.SetMemoryRecallLogEnabled(p.MemoryRecallLogEnabled)
		cfg := a.attachPublishedSkills(*p)
		a.runConfigPostUnlock(cfg)
		return cfg, nil
	}

	// Cold or externally changed path: exclusive load from disk.
	lockStart := time.Now()
	a.configMu.Lock()
	lockWait := time.Since(lockStart)
	if lockWait > 50*time.Millisecond {
		log.Printf("[config] LoadConfig:lock_wait=%s", lockWait)
	}
	// Another writer may have published while we waited. Revalidate the file
	// before accepting that snapshot; an external edit may have raced the wait.
	if p := a.configSnap.Load(); p != nil && a.configCacheMatchesDisk() {
		cfg := a.attachPublishedSkills(*p)
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
