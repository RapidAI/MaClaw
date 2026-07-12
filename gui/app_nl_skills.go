package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"gopkg.in/yaml.v3"
)

// SkillDiagEntry reports the scan result for a single skill directory.
type SkillDiagEntry struct {
	Dir    string `json:"dir"`
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// NLSkillDefinition is the Wails-facing view of a Skill.
type NLSkillDefinition struct {
	Name                string                      `json:"name"`
	DirName             string                      `json:"dir_name,omitempty"`
	SkillID             string                      `json:"skill_id,omitempty"` // publisher.skill-name stable identifier
	Description         string                      `json:"description"`
	Triggers            []string                    `json:"triggers"`
	Steps               []corelib.NLSkillStep       `json:"steps"`
	Status              string                      `json:"status"`
	CreatedAt           time.Time                   `json:"created_at"`
	Source              string                      `json:"source"`
	SourceProject       string                      `json:"source_project"`
	ExecutionClass      string                      `json:"execution_class,omitempty"`
	HubSkillID          string                      `json:"hub_skill_id,omitempty"`
	HubVersion          string                      `json:"hub_version,omitempty"`
	Capability          *corelib.SkillCapabilityRef `json:"capability,omitempty"`
	TrustLevel          string                      `json:"trust_level,omitempty"`
	Type                string                      `json:"type,omitempty"`          // "executable" (default) | "knowledge"
	Content             string                      `json:"content,omitempty"`       // Markdown content for knowledge-type skills
	Publisher           string                      `json:"publisher,omitempty"`     // Plugin namespace publisher
	Mode                string                      `json:"mode,omitempty"`          // "sequential" (default) | "interactive" | "api_workflow"
	HasDocumentation    bool                        `json:"has_documentation"`       // true if SKILL.md exists in skill directory
	SkillDir            string                      `json:"-"`                       // skill directory path (internal use, not serialized to frontend)
	Params              []corelib.NLSkillParam      `json:"params,omitempty"`        // parameter schema (explicit or synthesized)
	RequiredArgs        []string                    `json:"required_args,omitempty"` // required template variables
	RequiresGUI         bool                        `json:"requires_gui,omitempty"`
	Capabilities        []string                    `json:"capabilities,omitempty"`
	RequiresTools       []string                    `json:"requires_tools,omitempty"`
	FallbackForTools    []string                    `json:"fallback_for_tools,omitempty"`
	RequiresToolsets    []string                    `json:"requires_toolsets,omitempty"`
	FallbackForToolsets []string                    `json:"fallback_for_toolsets,omitempty"`
	UsageCount          int                         `json:"usage_count"`
	SuccessCount        int                         `json:"success_count"`
	FailureCount        int                         `json:"failure_count"`
	SuccessRate         float64                     `json:"success_rate"` // computed: SuccessCount / UsageCount
	LastUsedAt          *time.Time                  `json:"last_used_at,omitempty"`
	LastError           string                      `json:"last_error,omitempty"`
	ReviewReason        string                      `json:"review_reason,omitempty"`
	// Self-repair audit (from NLSkillEntry runtime state).
	RepairAttemptCount  int                          `json:"repair_attempt_count,omitempty"`
	LastRepairAt        string                       `json:"last_repair_at,omitempty"`
	RepairHistory       []corelib.SkillRepairRecord  `json:"repair_history,omitempty"`
	OptimizationCount   int                          `json:"optimization_count,omitempty"`
	LastOptimizedAt     string                       `json:"last_optimized_at,omitempty"`
	IsMaclawApp         bool                        `json:"is_maclaw_app,omitempty"`
	MaclawAppCount      int                         `json:"maclaw_app_count,omitempty"`
	MaclawAppEntry      string                      `json:"maclaw_app_entry,omitempty"`
}

// QualifiedID returns the canonical unique identifier for this skill definition.
// Format: "publisher:name" when Publisher is set, otherwise bare "name".
func (d NLSkillDefinition) QualifiedID() string {
	name := strings.TrimSpace(d.Name)
	if name == "" {
		return ""
	}
	publisher := strings.TrimSpace(d.Publisher)
	if publisher != "" {
		return publisher + ":" + name
	}
	return name
}

// asNLSkillEntry projects definition fields used for identity / runtime ref.
func (d NLSkillDefinition) asNLSkillEntry() corelib.NLSkillEntry {
	return corelib.NLSkillEntry{
		Name:       d.Name,
		DirName:    d.DirName,
		SkillID:    d.SkillID,
		HubSkillID: d.HubSkillID,
		Publisher:  d.Publisher,
		SkillDir:   d.SkillDir,
	}
}

// SkillIdentityKeys returns lookup keys shared with install planning and run resolution.
func (d NLSkillDefinition) SkillIdentityKeys() []string {
	return d.asNLSkillEntry().SkillIdentityKeys()
}

// PreferredRuntimeSkillRef is the preferred RunNLSkillAsync argument for this skill.
func (d NLSkillDefinition) PreferredRuntimeSkillRef() string {
	return d.asNLSkillEntry().PreferredRuntimeSkillRef()
}

// SkillAppManifestEntry is the Wails-facing app extension declared by maclaw.apps.json.
type SkillAppManifestEntry struct {
	ID                string                  `json:"id"`
	SkillID           string                  `json:"skill_id"`
	Name              string                  `json:"name"`
	Description       string                  `json:"description,omitempty"`
	Category          string                  `json:"category,omitempty"`
	Kind              string                  `json:"kind,omitempty"`
	Icon              string                  `json:"icon,omitempty"`
	CustomIconDataURL string                  `json:"custom_icon_data_url,omitempty"`
	InputMode         string                  `json:"input_mode,omitempty"`
	MultipleFiles     bool                    `json:"multiple_files,omitempty"`
	OutputModes       []string                `json:"output_modes,omitempty"`
	Fields            []SkillAppManifestField `json:"fields,omitempty"`
	AppDefinitionFile string                  `json:"app_definition_file,omitempty"`
	AppDefinition     map[string]any          `json:"app_definition,omitempty"`
	Governance        map[string]any          `json:"governance,omitempty"`
}

func (entry *SkillAppManifestEntry) UnmarshalJSON(data []byte) error {
	type skillAppManifestEntryAlias SkillAppManifestEntry
	var raw struct {
		*skillAppManifestEntryAlias
		CustomIconDataURLCamel string `json:"customIconDataUrl,omitempty"`
	}
	raw.skillAppManifestEntryAlias = (*skillAppManifestEntryAlias)(entry)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if strings.TrimSpace(entry.CustomIconDataURL) == "" {
		entry.CustomIconDataURL = raw.CustomIconDataURLCamel
	}
	return nil
}

// SkillAppManifestField describes one fixed form control exposed by a tool app.
type SkillAppManifestField struct {
	Name     string        `json:"name"`
	Label    string        `json:"label,omitempty"`
	Type     string        `json:"type,omitempty"`
	Required bool          `json:"required,omitempty"`
	Default  interface{}   `json:"default,omitempty"`
	Options  []interface{} `json:"options,omitempty"`
}

// SkillAppInputFileRef is a staged file reference passed from the app panel to a skill run.
type SkillAppInputFileRef struct {
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	Type         string `json:"type,omitempty"`
	LastModified int64  `json:"last_modified,omitempty"`
	StagedPath   string `json:"staged_path"`
	Transfer     string `json:"transfer"`
}

type skillAppManifestFile struct {
	PrivateMarker string                  `json:"x_maclaw_apps"`
	Apps          []SkillAppManifestEntry `json:"apps"`
}

// SkillExecutor manages and executes locally-defined NL Skills.
type SkillExecutor struct {
	app         *App
	mcpRegistry *MCPRegistry
	manager     *RemoteSessionManager
	sshMgr      *remote.SSHSessionManager
	bgTaskMgr   *remote.SSHBackgroundTaskManager
	mu          sync.RWMutex

	skillCacheMu  sync.Mutex
	skillCache    []corelib.NLSkillEntry
	skillCacheAt  time.Time
	skillCacheKey string
}

const skillLoadCacheTTL = 10 * time.Minute

// NewSkillExecutor creates a new client-side Skill executor.
func NewSkillExecutor(app *App, mcpRegistry *MCPRegistry, manager *RemoteSessionManager) *SkillExecutor {
	return &SkillExecutor{
		app:         app,
		mcpRegistry: mcpRegistry,
		manager:     manager,
	}
}

// loadSkills reads skill entries from config and merges skills discovered
// from on-disk YAML files under ~/.maclaw/data/skills/ and ~/.agents/skills/.
// Config-based skills usually take precedence over file-based ones with the
// same name, except that stale config entries without executable steps are
// hydrated from an executable file-backed definition when available.
//
// Identity key: skill directory path (stable, not affected by Name changes,
// Hub publisher prefixes, or SKILL.md frontmatter overrides). Falls back to
// Name matching for config entries without SkillDir (e.g., learned/crafted
// skills that don't have a directory).
func (e *SkillExecutor) loadSkills() []corelib.NLSkillEntry {
	if e == nil {
		return nil
	}
	if e.app == nil {
		return skill.ScanAllSkillDirs()
	}
	cfg, err := e.app.LoadConfig()
	if err != nil {
		return nil
	}
	cacheKey := skillLoadCacheKey(cfg.NLSkills, cfg.ExternalSkillDirs, e.cachedSkillScannerVersion())
	e.skillCacheMu.Lock()
	if e.skillCache != nil && e.skillCacheKey == cacheKey && time.Since(e.skillCacheAt) < skillLoadCacheTTL {
		cached := cloneSkillEntries(e.skillCache)
		e.skillCacheMu.Unlock()
		return cached
	}
	e.skillCacheMu.Unlock()

	skills := append([]corelib.NLSkillEntry(nil), cfg.NLSkills...)

	// Build two indexes: primary by directory path (stable), fallback by Name.
	knownByDir := make(map[string]int, len(skills))
	knownByName := make(map[string]int, len(skills))
	for i, s := range skills {
		if s.SkillDir != "" {
			knownByDir[skillDirIdentityKey(s.SkillDir)] = i
		}
		if nameKey := skillNameIdentityKey(s.Name); nameKey != "" {
			knownByName[nameKey] = i
		}
	}

	fileSkills, fileSkillsReady := e.scanFileSkills(cfg.ExternalSkillDirs)
	for _, fs := range fileSkills {
		// Primary: match by directory path (stable identity).
		// Fallback: match by Name (backward compat for config entries without SkillDir).
		idx := -1
		if fs.SkillDir != "" {
			if i, ok := knownByDir[skillDirIdentityKey(fs.SkillDir)]; ok {
				idx = i
			}
		}
		if idx < 0 {
			if i, ok := knownByName[skillNameIdentityKey(fs.Name)]; ok {
				idx = i
			}
		}

		if idx >= 0 {
			configSkill := &skills[idx]
			if len(fs.Steps) > 0 {
				// Detect skill version change before merging fields.
				// When the on-disk version differs from the config version,
				// emit an InvalidationEvent scoped to that skill name so that
				// stale outcome records for manage_skill are decayed.
				if oldVersion := configSkill.HubVersion; oldVersion != "" && fs.HubVersion != "" && oldVersion != fs.HubVersion {
					if tracker := e.app.usageTracker; tracker != nil {
						event := tool.InvalidationEvent{
							ToolName:    "manage_skill",
							Timestamp:   time.Now(),
							Reason:      fmt.Sprintf("skill_version_changed: %s (%s -> %s)", fs.Name, oldVersion, fs.HubVersion),
							ScopeTokens: []string{fs.Name},
						}
						tracker.ApplyInvalidation(event)
					}
				}

				// On-disk skill.yaml is the source of truth for steps.
				configSkill.Steps = fs.Steps
				configSkill.SkillDir = fs.SkillDir
				configSkill.Params = fs.Params
				configSkill.RequiredArgs = fs.RequiredArgs
				if len(configSkill.Triggers) == 0 {
					configSkill.Triggers = fs.Triggers
				}
				if strings.TrimSpace(configSkill.Description) == "" {
					configSkill.Description = fs.Description
				}
				// The file remains the source of truth for normal status.
				// Only a non-active config overlay (for example a security block)
				// is allowed to override the on-disk value.
				if fs.Status != "" && !fileSkillStatusIsOverlay(configSkill.Status) {
					configSkill.Status = fs.Status
				}

				// Propagate HubVersion from file to config so subsequent
				// loads can detect future version changes.
				if fs.HubVersion != "" {
					configSkill.HubVersion = fs.HubVersion
				}
			}
			continue
		}
		skills = append(skills, fs)
		// Update both indexes for dedup of subsequent file skills.
		newIdx := len(skills) - 1
		if fs.SkillDir != "" {
			knownByDir[skillDirIdentityKey(fs.SkillDir)] = newIdx
		}
		if nameKey := skillNameIdentityKey(fs.Name); nameKey != "" {
			knownByName[nameKey] = newIdx
		}
	}

	if fileSkillsReady {
		e.skillCacheMu.Lock()
		e.skillCache = cloneSkillEntries(skills)
		e.skillCacheAt = time.Now()
		e.skillCacheKey = cacheKey
		e.skillCacheMu.Unlock()
	}

	return cloneSkillEntries(skills)
}

func skillDirIdentityKey(dir string) string {
	key := filepath.Clean(strings.TrimSpace(dir))
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

func skillNameIdentityKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (e *SkillExecutor) invalidateSkillCache() {
	e.skillCacheMu.Lock()
	e.skillCache = nil
	e.skillCacheAt = time.Time{}
	e.skillCacheKey = ""
	e.skillCacheMu.Unlock()

	// Also invalidate the CachedSkillScanner so the next Route() call
	// picks up the updated skill list from disk.
	if e.app != nil && e.app.cachedSkillScanner != nil {
		e.app.cachedSkillScanner.Invalidate()
	}
}

// scanFileSkills returns file-based skill entries using the CachedSkillScanner
// when available. If the app-level scanner is still warming up, it returns no
// file skills and ready=false instead of starting a duplicate foreground scan.
func (e *SkillExecutor) scanFileSkills(externalDirs []string) ([]corelib.NLSkillEntry, bool) {
	if e.app != nil && e.app.cachedSkillScanner != nil {
		cached := e.app.cachedSkillScanner.Get()
		if cached != nil {
			// cached is non-nil: scan completed (may be empty slice if no skills found).
			return cached, true
		}
		// nil: scan not yet complete. Do not block the active agent loop with a
		// full markdown scan; the background scanner will publish the cache soon.
		log.Printf("[skill-cache] scan_not_ready using_config_skills_only external_dirs=%d", len(externalDirs))
		return nil, false
	}
	// Fallback for tests and minimal App wiring without CachedSkillScanner.
	if e != nil && e.app != nil {
		return scanSkillRoots(e.app.skillScanRootsWithExternal(externalDirs)), true
	}
	return skill.ScanAllSkillDirsWithExternal(externalDirs), true
}

func (e *SkillExecutor) cachedSkillScannerVersion() uint64 {
	if e == nil || e.app == nil || e.app.cachedSkillScanner == nil {
		return 0
	}
	return e.app.cachedSkillScanner.Version()
}

func skillLoadCacheKey(skills []corelib.NLSkillEntry, externalDirs []string, scannerVersion uint64) string {
	payload := struct {
		Skills         []corelib.NLSkillEntry `json:"skills"`
		ExternalDirs   []string               `json:"external_dirs"`
		ScannerVersion uint64                 `json:"scanner_version,omitempty"`
	}{
		Skills:         skills,
		ExternalDirs:   skillDirIdentityKeys(externalDirs),
		ScannerVersion: scannerVersion,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("skills=%d external=%s", len(skills), strings.Join(externalDirs, string(os.PathListSeparator)))
	}
	return string(data)
}

func skillDirIdentityKeys(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		out = append(out, skillDirIdentityKey(dir))
	}
	return out
}

func cloneSkillEntries(in []corelib.NLSkillEntry) []corelib.NLSkillEntry {
	out := append([]corelib.NLSkillEntry(nil), in...)
	for i := range out {
		out[i].Triggers = cloneStringSlice(out[i].Triggers)
		out[i].Steps = cloneSkillSteps(out[i].Steps)
		out[i].Operations = cloneSkillOperations(out[i].Operations)
		out[i].Params = cloneSkillParams(out[i].Params)
		out[i].RequiredArgs = cloneStringSlice(out[i].RequiredArgs)
		out[i].Platforms = cloneStringSlice(out[i].Platforms)
		out[i].Capabilities = cloneStringSlice(out[i].Capabilities)
		out[i].RequiresTools = cloneStringSlice(out[i].RequiresTools)
		out[i].FallbackForTools = cloneStringSlice(out[i].FallbackForTools)
		out[i].RequiresToolsets = cloneStringSlice(out[i].RequiresToolsets)
		out[i].FallbackForToolsets = cloneStringSlice(out[i].FallbackForToolsets)
		out[i].RequiredCredentialFiles = cloneStringSlice(out[i].RequiredCredentialFiles)
		out[i].RequiresPython = cloneStringSlice(out[i].RequiresPython)
		out[i].RequiresNode = cloneStringSlice(out[i].RequiresNode)
		out[i].RequiresBins = cloneStringSlice(out[i].RequiresBins)
		out[i].RequiredEnv = cloneStringSlice(out[i].RequiredEnv)
		out[i].RepairHistory = append([]corelib.SkillRepairRecord(nil), out[i].RepairHistory...)
		out[i].SolidificationCandidates = cloneSolidificationCandidates(out[i].SolidificationCandidates)
		out[i].References = append([]corelib.SkillReference(nil), out[i].References...)
		out[i].Pipeline = cloneSkillPipelineSteps(out[i].Pipeline)
	}
	return out
}

func cloneSkillSteps(in []corelib.NLSkillStep) []corelib.NLSkillStep {
	out := append([]corelib.NLSkillStep(nil), in...)
	for i := range out {
		out[i].Params = cloneInterfaceMap(out[i].Params)
		out[i].Capture = cloneStringMap(out[i].Capture)
		if out[i].Poll != nil {
			poll := *out[i].Poll
			out[i].Poll = &poll
		}
		if out[i].Loop != nil {
			loop := *out[i].Loop
			out[i].Loop = &loop
		}
		if out[i].FallbackStep != nil {
			fallback := cloneSkillSteps([]corelib.NLSkillStep{*out[i].FallbackStep})[0]
			out[i].FallbackStep = &fallback
		}
	}
	return out
}

func cloneSkillOperations(in []corelib.NLSkillOperation) []corelib.NLSkillOperation {
	out := append([]corelib.NLSkillOperation(nil), in...)
	for i := range out {
		out[i].Params = cloneStringSlice(out[i].Params)
		out[i].Labels = cloneStringSlice(out[i].Labels)
	}
	return out
}

func cloneSkillParams(in []corelib.NLSkillParam) []corelib.NLSkillParam {
	out := append([]corelib.NLSkillParam(nil), in...)
	for i := range out {
		out[i].Aliases = cloneStringSlice(out[i].Aliases)
	}
	return out
}

func cloneSolidificationCandidates(in []corelib.SolidificationCandidate) []corelib.SolidificationCandidate {
	out := append([]corelib.SolidificationCandidate(nil), in...)
	for i := range out {
		out[i].ParamSlots = cloneStringSlice(out[i].ParamSlots)
	}
	return out
}

func cloneSkillPipelineSteps(in []corelib.SkillPipelineStep) []corelib.SkillPipelineStep {
	out := append([]corelib.SkillPipelineStep(nil), in...)
	for i := range out {
		out[i].Params = cloneStringMap(out[i].Params)
	}
	return out
}

func cloneInterfaceMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = cloneInterfaceValue(v)
	}
	return out
}

func cloneInterfaceValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		return cloneInterfaceMap(typed)
	case map[interface{}]interface{}:
		out := make(map[interface{}]interface{}, len(typed))
		for k, v := range typed {
			out[k] = cloneInterfaceValue(v)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, v := range typed {
			out[i] = cloneInterfaceValue(v)
		}
		return out
	case []string:
		return cloneStringSlice(typed)
	case map[string]string:
		return cloneStringMap(typed)
	default:
		return v
	}
}

func cloneStringSlice(in []string) []string {
	return append([]string(nil), in...)
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// shouldHydrateSkillFromFile is a predicate that decides whether a config-based
// skill's steps should be replaced with the on-disk file version.
// The on-disk skill.yaml is the source of truth for steps whenever the file
// has valid steps and names match.
//
// Note: loadSkills now uses directory-path-based identity matching instead of
// calling this function directly. This function is retained for property-based
// test validation of the hydration predicate.
func shouldHydrateSkillFromFile(configSkill, fileSkill corelib.NLSkillEntry, _ string) bool {
	if fileSkill.Name == "" || configSkill.Name != fileSkill.Name || len(fileSkill.Steps) == 0 {
		return false
	}
	return true
}

// scanSkillYAMLFiles discovers skill definitions from all known skill
// directories (e.g. ~/.maclaw/data/skills, ~/.agents/skills) plus
// user-configured external directories via corelib.
func (e *SkillExecutor) scanSkillYAMLFiles() []corelib.NLSkillEntry {
	if e == nil || e.app == nil {
		return skill.ScanAllSkillDirs()
	}
	cfg, err := e.app.LoadConfig()
	if err != nil {
		return scanSkillRoots(e.app.skillScanRootsWithExternal(nil))
	}
	return scanSkillRoots(e.app.skillScanRootsWithExternal(cfg.ExternalSkillDirs))
}

// saveSkills persists skill entries to config.
// File-based skills (source == "file") are saved as stats-only stubs so that
// usage statistics survive across restarts. The full definition (steps,
// triggers, description, etc.) is always loaded from the YAML file at runtime
// via loadSkills -> scanSkillYAMLFiles (directory-path identity matching).
func (e *SkillExecutor) saveSkills(skills []corelib.NLSkillEntry) error {
	if e == nil || e.app == nil {
		return fmt.Errorf("skill executor app not initialized")
	}
	_, err := e.app.LoadConfig()
	if err != nil {
		return err
	}
	filtered := make([]corelib.NLSkillEntry, 0, len(skills))
	for _, s := range skills {
		if normalizeSkillEntrySource(s.Source) == skillEntrySourceFile {
			// Only persist the runtime overlay; strip definition data so
			// config.json is not polluted with YAML-managed content.
			// Repair metadata is runtime/audit state, not definition state.
			if fileSkillHasRuntimeOverlay(s) {
				filtered = append(filtered, corelib.NLSkillEntry{
					Name:               s.Name,
					Source:             string(skillEntrySourceFile),
					SkillDir:           s.SkillDir,
					Status:             fileSkillOverlayStatus(s.Status),
					UsageCount:         s.UsageCount,
					SuccessCount:       s.SuccessCount,
					FailureCount:       s.FailureCount,
					WorkaroundCount:    s.WorkaroundCount,
					LastUsedAt:         s.LastUsedAt,
					LastError:          s.LastError,
					RepairAttemptCount: s.RepairAttemptCount,
					LastRepairAt:       s.LastRepairAt,
					RepairHistory:      append([]corelib.SkillRepairRecord(nil), s.RepairHistory...),
				})
			}
			continue
		}
		filtered = append(filtered, s)
	}
	if err := e.app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.NLSkills = filtered
	}); err != nil {
		return err
	}
	e.invalidateSkillCache()
	return nil
}

func fileSkillStatusIsOverlay(status string) bool {
	status = strings.TrimSpace(status)
	return status != "" && normalizeSkillEntryStatus(status) != skillEntryStatusActive
}

func fileSkillOverlayStatus(status string) string {
	if fileSkillStatusIsOverlay(status) {
		return strings.TrimSpace(status)
	}
	return ""
}

func fileSkillHasRuntimeOverlay(s corelib.NLSkillEntry) bool {
	return s.UsageCount > 0 ||
		s.SuccessCount > 0 ||
		s.FailureCount > 0 ||
		s.WorkaroundCount > 0 ||
		strings.TrimSpace(s.LastUsedAt) != "" ||
		strings.TrimSpace(s.LastError) != "" ||
		fileSkillStatusIsOverlay(s.Status) ||
		s.RepairAttemptCount > 0 ||
		strings.TrimSpace(s.LastRepairAt) != "" ||
		len(s.RepairHistory) > 0
}

// Register adds a new Skill definition.
func (e *SkillExecutor) Register(entry corelib.NLSkillEntry) error {
	if e == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	name := strings.TrimSpace(entry.Name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	skills := e.loadSkills()
	var app *App
	if e != nil {
		app = e.app
	}
	primaryDir, primaryErr := app.primarySkillsDir()
	for _, s := range skills {
		if s.Name != name {
			continue
		}
		if normalizeSkillEntrySource(entry.Source) == skillEntrySourceHub && normalizeSkillEntrySource(s.Source) == skillEntrySourceFile && primaryErr == nil {
			extractedDir := filepath.Join(primaryDir, name)
			if skillDirIdentityKey(s.SkillDir) == skillDirIdentityKey(extractedDir) {
				continue
			}
		}
		return fmt.Errorf("skill %q already exists", name)
	}
	entry.Name = name
	if entry.Status == "" {
		entry.Status = string(skillEntryStatusActive)
	}
	if isShellBrowserAutomationSkillEntry(entry) {
		return browserAutomationSkillRejectedError(name)
	}
	if entry.CreatedAt == "" {
		entry.CreatedAt = time.Now().Format(time.RFC3339)
	}
	if entry.Source == "" {
		entry.Source = string(skillEntrySourceManual)
	}
	if entry.Triggers == nil {
		entry.Triggers = []string{}
	}
	if entry.Steps == nil {
		entry.Steps = []corelib.NLSkillStep{}
	}
	skills = append(skills, entry)
	return e.saveSkills(skills)
}

func (a *App) primarySkillsDir() (string, error) {
	if a == nil {
		return skill.PrimarySkillsDir()
	}
	return filepath.Join(a.GetDataDir(), "skills"), nil
}

func (a *App) skillStagingDir() (string, error) {
	if a == nil {
		return skill.StagingDir()
	}
	return filepath.Join(a.GetDataDir(), "skills_staging"), nil
}

func (a *App) skillScanRootsWithExternal(externalDirs []string) []string {
	var roots []string
	if primary, err := a.primarySkillsDir(); err == nil && strings.TrimSpace(primary) != "" {
		roots = append(roots, primary)
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".agents", "skills"))
	}
	seen := make(map[string]bool, len(roots))
	filtered := roots[:0]
	for _, root := range roots {
		cleaned := filepath.Clean(strings.TrimSpace(root))
		if cleaned == "." || seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		filtered = append(filtered, cleaned)
	}
	for _, dir := range externalDirs {
		cleaned := filepath.Clean(strings.TrimSpace(dir))
		if cleaned == "." || seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		filtered = append(filtered, cleaned)
	}
	return filtered
}

func scanSkillRoots(roots []string) []corelib.NLSkillEntry {
	seen := make(map[string]bool)
	var result []corelib.NLSkillEntry
	for _, root := range roots {
		for _, s := range skill.ScanSkillDir(root) {
			if !seen[s.Name] {
				result = append(result, s)
				seen[s.Name] = true
			}
		}
	}
	return result
}

// Update modifies an existing Skill definition.
// Usage tracking fields (UsageCount, SuccessCount, LastUsedAt, LastError)
// are preserved from the caller if non-zero, allowing the experience
// extractor to carry forward stats when replacing a pattern.
func (e *SkillExecutor) Update(entry corelib.NLSkillEntry) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	for i, s := range skills {
		if s.Name == entry.Name {
			if entry.Status == "" {
				entry.Status = s.Status
			}
			if isShellBrowserAutomationSkillEntry(entry) {
				return browserAutomationSkillRejectedError(entry.Name)
			}
			skills[i].Description = entry.Description
			skills[i].Triggers = entry.Triggers
			skills[i].Steps = entry.Steps
			skills[i].Status = entry.Status
			// Preserve usage stats from caller if provided (experience extractor
			// carries forward existing stats); otherwise keep what's on disk.
			if entry.UsageCount > 0 {
				skills[i].UsageCount = entry.UsageCount
				skills[i].SuccessCount = entry.SuccessCount
				skills[i].LastUsedAt = entry.LastUsedAt
				skills[i].LastError = entry.LastError
			}
			return e.saveSkills(skills)
		}
	}
	return fmt.Errorf("skill %q not found", entry.Name)
}

// UpdateStatus changes only the status field of an existing skill.
// Used by focused lifecycle actions such as setup gating or local review approval.
func (e *SkillExecutor) UpdateStatus(name, status string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	idx := -1
	for i, s := range skills {
		if s.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i, s := range skills {
			if s.MatchesName(name) {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		return fmt.Errorf("skill %q not found", name)
	}
	skills[idx].Status = status
	// When re-enabling, clear maintenance-only markers so the skill leaves the
	// retired/archived evolution queue. Real runtime errors are preserved.
	if strings.EqualFold(strings.TrimSpace(status), "active") {
		le := strings.ToLower(skills[idx].LastError)
		if strings.Contains(le, "retired_by_maintenance") || strings.Contains(le, "archived_by_maintenance") {
			skills[idx].LastError = ""
		}
	}
	return e.saveSkills(skills)
}

// UpdateFromHub checks for a newer version of a Hub Skill and updates it locally.
// It preserves Name, Source, HubSkillID, SourceProject, Status, and CreatedAt.
// Network calls are made outside the mutex to avoid blocking other skill operations.
func (e *SkillExecutor) UpdateFromHub(name string) error {
	// Phase 1: Read skill info under read lock.
	e.mu.RLock()
	skills := e.loadSkills()
	var skill corelib.NLSkillEntry
	found := false
	for _, s := range skills {
		if s.Name == name {
			skill = s
			found = true
			break
		}
	}
	if !found {
		for _, s := range skills {
			if s.MatchesName(name) {
				skill = s
				found = true
				break
			}
		}
	}
	e.mu.RUnlock()

	if !found {
		return fmt.Errorf("skill %q not found", name)
	}
	if normalizeSkillEntrySource(skill.Source) != skillEntrySourceHub || skill.HubSkillID == "" {
		return fmt.Errorf("skill %q is not a hub skill", name)
	}
	if e.app.skillHubClient == nil {
		return fmt.Errorf("skill hub client not initialized")
	}

	// Phase 2: Network calls without holding the lock.
	ctx := context.Background()

	meta, err := e.app.skillHubClient.CheckUpdate(ctx, skill.HubSkillID, skill.HubVersion)
	if err != nil {
		return fmt.Errorf("failed to check update for skill %q: %w", name, err)
	}
	if meta == nil {
		return nil // already up to date
	}

	updated, err := e.app.skillHubClient.Install(ctx, skill.HubSkillID, meta.HubURL)
	if err != nil {
		return fmt.Errorf("failed to download update for skill %q: %w", name, err)
	}

	// Phase 3: Apply update under write lock.
	e.mu.Lock()
	defer e.mu.Unlock()

	// Re-read skills in case they changed while we were doing network I/O.
	skills = e.loadSkills()
	idx := -1
	for i, s := range skills {
		if s.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i, s := range skills {
			if s.MatchesName(name) {
				idx = i
				break
			}
		}
	}
	if idx == -1 {
		return fmt.Errorf("skill %q was removed during update", name)
	}

	// Replace mutable fields, preserve identity fields.
	skills[idx].Description = updated.Description
	skills[idx].Triggers = updated.Triggers
	skills[idx].Steps = updated.Steps
	skills[idx].HubVersion = updated.HubVersion
	skills[idx].TrustLevel = updated.TrustLevel
	if isShellBrowserAutomationSkillEntry(skills[idx]) {
		return browserAutomationSkillRejectedError(skills[idx].Name)
	}

	// Invalidate the security scan cache: the content hash changed (new steps),
	// so the next StartRunForOwner → ensureSkillSecurityScanned will trigger a
	// fresh scan before execution. Without this, a stale "allowed" cache from
	// the previous version could let new (potentially malicious) steps execute
	// without re-scanning.
	if skillDir := strings.TrimSpace(skills[idx].SkillDir); skillDir != "" {
		_ = removeSkillScanCacheFile(skillDir, skills[idx].Name)
	}

	return e.saveSkills(skills)
}

// Delete removes a Skill by name.
func (e *SkillExecutor) Delete(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	found := false
	var targetSkillDir string

	// Two-pass lookup: first try exact Name match (what the frontend sends),
	// then fallback to MatchesName for case-insensitive / DirName / SkillDir
	// basename matching. This prevents a false-positive deletion when another
	// skill's DirName happens to match the target's Name.
	idx := -1
	for i, s := range skills {
		if s.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i, s := range skills {
			if s.MatchesName(name) {
				idx = i
				break
			}
		}
	}
	if idx >= 0 {
		found = true
		targetSkillDir = skills[idx].SkillDir
		skills = append(skills[:idx], skills[idx+1:]...)
		if err := e.saveSkills(skills); err != nil {
			return err
		}
	}

	if !found {
		return fmt.Errorf("skill %q not found", name)
	}

	// Prefer deleting the precise SkillDir recorded in the entry to avoid
	// accidentally removing a same-named skill from a different root directory.
	if targetSkillDir != "" {
		rmErr := os.RemoveAll(targetSkillDir)
		if rmErr == nil || !dirExists(targetSkillDir) {
			// Directory gone — remove from scanner cache for instant UI feedback.
			if e.app != nil && e.app.cachedSkillScanner != nil {
				e.app.cachedSkillScanner.RemoveByDir(targetSkillDir)
			}
		} else {
			// Directory still present (file locked by antivirus, etc.).
			// Log for diagnostics. The scanner will re-discover it on next scan,
			// which is correct — the user will see it reappear and can retry.
			log.Printf("[skill-delete] os.RemoveAll(%s) failed: %v", targetSkillDir, rmErr)
		}
	} else {
		// Fallback: scan all roots by name (backward compatibility for
		// old entries that have no SkillDir recorded).
		e.removeSkillDirs(name)
	}
	return nil
}

// removeSkillDirs removes on-disk skill directories whose resolved name
// matches the given name. It delegates discovery to skill.ScanSkillDirAll,
// the unfiltered variant of the same scanner that loadSkills uses, so
// any format the scanner can parse is automatically covered, and platform-
// incompatible skills can still be deleted.
// Errors are silently ignored so that config deletion is never blocked
// by a disk cleanup failure.
func (e *SkillExecutor) removeSkillDirs(name string) {
	cfg, _ := e.app.LoadConfig()
	for _, root := range skill.SkillScanRootsWithExternal(cfg.ExternalSkillDirs) {
		for _, s := range skill.ScanSkillDirAll(root) {
			if s.MatchesName(name) {
				if s.SkillDir != "" {
					rmErr := os.RemoveAll(s.SkillDir)
					if (rmErr == nil || !dirExists(s.SkillDir)) && e.app.cachedSkillScanner != nil {
						e.app.cachedSkillScanner.RemoveByDir(s.SkillDir)
					}
				}
			}
		}
	}
}

// dirExists returns true if path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Rename changes the name of a skill. It updates the in-memory list,
// the on-disk skill.yaml/skill.yml name field, renames the directory,
// and persists the changes to config.json.
func (e *SkillExecutor) Rename(oldName, newName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Sanitize newName for filesystem safety (strip path separators, illegal chars).
	newName = skill.SanitizeSkillName(newName)
	if newName == "" {
		return fmt.Errorf("new name is invalid after sanitization")
	}
	if newName == oldName {
		return nil
	}

	skills := e.loadSkills()

	// Check target name doesn't conflict with an existing skill.
	for _, s := range skills {
		if strings.EqualFold(s.Name, newName) {
			return fmt.Errorf("skill %q already exists", s.Name)
		}
	}

	// Find the skill to rename.
	idx := -1
	for i, s := range skills {
		if s.Name == oldName {
			idx = i
			break
		}
	}
	if idx < 0 {
		for i, s := range skills {
			if s.MatchesName(oldName) {
				idx = i
				break
			}
		}
	}
	if idx == -1 {
		return fmt.Errorf("skill %q not found", oldName)
	}

	target := &skills[idx]

	// Update name field in the on-disk definition file (skill.yaml/skill.yml).
	if target.SkillDir != "" {
		defPath, defFormat := findSkillDefinitionFile(target.SkillDir)
		if defPath != "" && defFormat == "yaml" {
			if data, err := os.ReadFile(defPath); err == nil {
				if updated := replaceYAMLNameField(string(data), oldName, newName); updated != "" {
					_ = os.WriteFile(defPath, []byte(updated), 0644)
				}
			}
		}

		// Rename the directory to match the new name.
		parentDir := filepath.Dir(target.SkillDir)
		newDir := filepath.Join(parentDir, newName)
		if _, err := os.Stat(newDir); os.IsNotExist(err) {
			if err := os.Rename(target.SkillDir, newDir); err == nil {
				target.SkillDir = newDir
			}
			// If rename fails (permission, cross-device, etc.), keep the old
			// directory path — loadSkills will still find the skill by name
			// field in skill.yaml which has already been updated.
		}
	}

	// Update the in-memory entry.
	target.Name = newName
	// Preserve old name as DirName alias so MatchesName can still find this
	// skill by the old name (important if directory rename failed).
	if target.DirName == "" || target.DirName == oldName {
		target.DirName = oldName
	}

	return e.saveSkills(skills)
}

// replaceYAMLNameField replaces only the top-level "name:" field value in a
// YAML skill definition. It matches "name:" at the start of a line (no
// leading whitespace) to avoid replacing nested occurrences in descriptions
// or step parameters.
func replaceYAMLNameField(content, oldName, newName string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimRight(line, "\r")
		// Match only top-level name field (no leading whitespace).
		if !strings.HasPrefix(trimmed, "name:") {
			continue
		}
		valuePart := strings.TrimSpace(trimmed[len("name:"):])
		// Strip quotes from the value for comparison.
		unquoted := strings.Trim(valuePart, "\"'")
		if unquoted == oldName {
			// Preserve original line ending style.
			hasCR := strings.HasSuffix(line, "\r")
			newLine := "name: " + newName
			if hasCR {
				newLine += "\r"
			}
			lines[i] = newLine
			return strings.Join(lines, "\n")
		}
	}
	return "" // no match found, don't modify
}

// uploadStatusFile is a small JSON file stored alongside file-based skills
// to persist upload metadata that can't be saved in config.json.
type uploadStatusFile struct {
	SubmissionID string `json:"submission_id"`
	UploadedAt   string `json:"uploaded_at"`
}

// MarkUploaded records that a skill has been uploaded to SkillMarket.
// For config-based skills, it writes hub_skill_id into config.
// For file-based skills, it writes an upload_status.json next to skill.yaml.
func (e *SkillExecutor) MarkUploaded(name, submissionID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	for i, s := range skills {
		if s.Name != name {
			continue
		}
		if normalizeSkillEntrySource(s.Source) == skillEntrySourceFile && s.SkillDir != "" {
			// File-based skill: write upload_status.json to skill directory.
			status := uploadStatusFile{
				SubmissionID: submissionID,
				UploadedAt:   time.Now().Format(time.RFC3339),
			}
			data, err := json.MarshalIndent(status, "", "  ")
			if err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(s.SkillDir, "upload_status.json"), data, 0644)
		}
		// Config-based skill: persist in config.json.
		skills[i].HubSkillID = submissionID
		return e.saveSkills(skills)
	}
	return fmt.Errorf("skill %q not found", name)
}

func classifySkillExecutionClass(entry corelib.NLSkillEntry) string {
	if len(entry.Steps) == 1 && classifySkillStepAction(entry.Steps[0].Action).IsCraftTool() {
		if normalizeSkillEntrySource(entry.Source).IsAgentMarkdownSkillSource() {
			return "agent_markdown_skill"
		}
	}
	return "native_skill"
}

// List returns all skill definitions.
func (e *SkillExecutor) List() []NLSkillDefinition {
	e.mu.RLock()
	defer e.mu.RUnlock()

	skills := e.loadSkills()
	defs := make([]NLSkillDefinition, 0, len(skills))
	for _, s := range skills {
		triggers := s.Triggers
		if triggers == nil {
			triggers = []string{}
		}
		steps := s.Steps
		if steps == nil {
			steps = []corelib.NLSkillStep{}
		}
		d := NLSkillDefinition{
			Name:                s.Name,
			DirName:             s.DirName,
			SkillID:             s.SkillID,
			Description:         s.Description,
			Triggers:            triggers,
			Steps:               steps,
			Status:              s.Status,
			Source:              s.Source,
			SourceProject:       s.SourceProject,
			ExecutionClass:      classifySkillExecutionClass(s),
			HubSkillID:          s.HubSkillID,
			HubVersion:          s.HubVersion,
			TrustLevel:          s.TrustLevel,
			Type:                s.Type,
			Content:             s.Content,
			Publisher:           s.Publisher,
			Mode:                s.Mode,
			HasDocumentation:    (normalizeSkillTypeKind(s.Type).IsKnowledge() && s.Content != "") || (s.SkillDir != "" && hasSkillDocFile(s.SkillDir)),
			SkillDir:            s.SkillDir,
			Params:              s.Params,
			RequiredArgs:        s.RequiredArgs,
			RequiresGUI:         s.RequiresGUI,
			Capabilities:        cloneStringSlice(s.Capabilities),
			RequiresTools:       cloneStringSlice(s.RequiresTools),
			FallbackForTools:    cloneStringSlice(s.FallbackForTools),
			RequiresToolsets:    cloneStringSlice(s.RequiresToolsets),
			FallbackForToolsets: cloneStringSlice(s.FallbackForToolsets),
			UsageCount:         s.UsageCount,
			SuccessCount:       s.SuccessCount,
			FailureCount:       s.FailureCount,
			LastError:          s.LastError,
			ReviewReason:       skillReviewReason(s),
			RepairAttemptCount: s.RepairAttemptCount,
			LastRepairAt:       s.LastRepairAt,
			RepairHistory:      append([]corelib.SkillRepairRecord(nil), s.RepairHistory...),
			OptimizationCount:  s.OptimizationCount,
			LastOptimizedAt:    s.LastOptimizedAt,
		}
		d.IsMaclawApp, d.MaclawAppCount, d.MaclawAppEntry = inspectMaclawAppSkillMetadata(s.SkillDir)
		if s.UsageCount > 0 {
			d.SuccessRate = float64(s.SuccessCount) / float64(s.UsageCount)
		}
		if t, err := time.Parse(time.RFC3339, s.CreatedAt); err == nil {
			d.CreatedAt = t
		}
		if s.LastUsedAt != "" {
			if t, err := time.Parse(time.RFC3339, s.LastUsedAt); err == nil {
				d.LastUsedAt = &t
			}
		}
		defs = append(defs, d)
	}
	return defs
}

func skillReviewReason(s corelib.NLSkillEntry) string {
	status := normalizeSkillEntryStatus(s.Status)
	lastError := strings.TrimSpace(s.LastError)
	if status == skillEntryStatusNeedsReview {
		if lastError != "" {
			return lastError
		}
		if s.RepairAttemptCount > 0 {
			return fmt.Sprintf("self-repair attempted %d time(s); manual review is required before re-enabling", s.RepairAttemptCount)
		}
		return "skill was marked needs_review by local governance or safety checks"
	}
	if status == skillEntryStatusNeedsSetup {
		if lastError != "" {
			return lastError
		}
		return "skill needs configuration before use"
	}
	return ""
}

func inspectMaclawAppSkillMetadata(skillDir string) (bool, int, string) {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return false, 0, ""
	}
	if count := countMaclawAppsManifestEntries(filepath.Join(skillDir, "maclaw.apps.json")); count > 0 {
		return true, count, "maclaw.apps.json"
	}
	if hasValidMaclawAppFile(filepath.Join(skillDir, "maclaw.app.json")) {
		return true, 1, "maclaw.app.json"
	}
	if entry := readMaclawAppEntryFromSkillYAML(skillDir); entry != "" {
		count := 0
		if hasValidMaclawAppFile(filepath.Join(skillDir, entry)) {
			count = 1
		}
		return true, count, entry
	}
	return false, 0, ""
}

func countMaclawAppsManifestEntries(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var manifest skillAppManifestFile
	if err := json.Unmarshal(data, &manifest); err != nil || strings.TrimSpace(manifest.PrivateMarker) != "v1" {
		return 0
	}
	count := 0
	for _, app := range manifest.Apps {
		if strings.TrimSpace(app.ID) != "" && strings.TrimSpace(app.Name) != "" {
			count++
		}
	}
	return count
}

func hasValidMaclawAppFile(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false
	}
	if stringMapValue(doc, "schema") != "maclaw.app.v1" || stringMapValue(doc, "privateMarker") != "x_maclaw_apps" {
		return false
	}
	app, ok := doc["app"].(map[string]interface{})
	if !ok {
		return false
	}
	return strings.TrimSpace(stringMapValue(app, "id")) != "" && strings.TrimSpace(stringMapValue(app, "name")) != ""
}

func readMaclawAppEntryFromSkillYAML(skillDir string) string {
	return readMaclawAppYAMLMetadata(skillDir).Entry
}

// AsRegisteredTools converts all active NL Skills to corelib tool.RegisteredTool
// entries with Body populated from skill.md content. This is the bridge between
// the NL Skill system and the body-aware tool routing pipeline.
func (e *SkillExecutor) AsRegisteredTools() []tool.RegisteredTool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	skills := e.loadSkills()
	var result []tool.RegisteredTool
	for _, s := range skills {
		if normalizeSkillEntryStatus(s.Status) != skillEntryStatusActive {
			continue
		}
		if isShellBrowserAutomationSkillEntry(s) {
			continue
		}
		body := e.readSkillBody(s)
		var bodySummary string
		if body != "" {
			bodySummary = tool.TruncateBody(body, tool.DefaultBodyMaxChars)
		}
		rt := tool.RegisteredTool{
			Name:        s.Name,
			Description: s.Description,
			Category:    tool.CategorySkill,
			Tags:        s.Triggers,
			Status:      tool.StatusAvailable,
			Body:        body,
			BodySummary: bodySummary,
		}
		result = append(result, rt)
	}
	return result
}

// readSkillBody reads the skill.md/SKILL.md content for a skill entry.
// For file-based skills with a SkillDir, it reads Markdown docs from that directory.
// For hub/other skills without SkillDir, it checks the primary skills directory.
// Errors are logged as warnings and do not prevent skill registration.
func (e *SkillExecutor) readSkillBody(entry corelib.NLSkillEntry) string {
	// Try SkillDir first (file-based skills).
	if entry.SkillDir != "" {
		return readSkillMarkdownBody(entry.SkillDir, entry.Name)
	}

	// For hub-installed skills, check the primary skills directory where
	// extractFiles writes skill.md/SKILL.md during installation.
	switch normalizeSkillEntrySource(entry.Source) {
	case skillEntrySourceHub, skillEntrySourceAgent:
		var app *App
		if e != nil {
			app = e.app
		}
		primaryDir, err := app.primarySkillsDir()
		if err != nil {
			return ""
		}
		return readSkillMarkdownBody(filepath.Join(primaryDir, entry.Name), entry.Name)
	}

	return ""
}

func readSkillMarkdownBody(skillDir, skillName string) string {
	mdPath := findSkillMarkdownDocPath(skillDir)
	if mdPath == "" {
		return ""
	}
	data, err := os.ReadFile(mdPath)
	if err != nil {
		log.Printf("[SkillRegister] WARN: cannot read %s for %s: %v", filepath.Base(mdPath), skillName, err)
		return ""
	}
	return string(data)
}

func (e *SkillExecutor) executeSkillSteps(entry *corelib.NLSkillEntry) (string, error) {
	result := e.executeSkillStepsDetailed(entry, nil)
	return result.Output, result.Err
}

func (e *SkillExecutor) executeSkillStepsWithArgs(entry *corelib.NLSkillEntry, runArgs map[string]interface{}) (string, error) {
	result := e.executeSkillStepsDetailed(entry, runArgs)
	return result.Output, result.Err
}

type skillExecutionResult struct {
	Output   string
	Captured map[string]string
	Err      error
}

func (e *SkillExecutor) executeSkillStepsDetailed(entry *corelib.NLSkillEntry, runArgs map[string]interface{}) skillExecutionResult {
	if entry == nil {
		return skillExecutionResult{Err: fmt.Errorf("skill entry is nil")}
	}
	ownerID := skillRunOwnerIDFromArgs(runArgs)
	var results []string
	var execErr error
	lastSessionID := ""
	// Maintain a vars map for output capture, mirroring the SkillRunner's
	// templateVars. This allows bash steps that output sessionId (e.g. via
	// Python scripts) to propagate state to subsequent steps.
	vars := skill.NormalizeRunVars(runArgs)
	if vars == nil {
		vars = make(map[string]string)
	}
	extraEnv := skillExecutionExtraEnv(runArgs)

	preparedEntry := *entry
	preparedEntry.Steps = append([]corelib.NLSkillStep(nil), entry.Steps...)
	preparedEntry.Params = append([]corelib.NLSkillParam(nil), entry.Params...)
	skill.NormalizeSkillForRunner(&preparedEntry)
	if isShellBrowserAutomationSkillEntry(preparedEntry) {
		return skillExecutionResult{Captured: cloneStringMapGUI(vars), Err: browserAutomationSkillRejectedError(preparedEntry.Name)}
	}
	if skill.IsContractlessSkill(&preparedEntry) {
		skill.FoldUnconsumedArgsToInput(vars, preparedEntry.Params)
	}
	sourceSkillDir := preparedEntry.SkillDir
	if workspace, cleanup, err := prepareSkillRunWorkspace("sync", preparedEntry.Name, preparedEntry.SkillDir); err != nil {
		log.Printf("[skill-executor] owner=%q skill=%q workspace isolation unavailable dir=%q err=%v; using installed dir", ownerID, preparedEntry.Name, preparedEntry.SkillDir, err)
	} else if workspace != "" {
		log.Printf("[skill-executor] owner=%q skill=%q workspace=%s source_dir=%s", ownerID, preparedEntry.Name, workspace, preparedEntry.SkillDir)
		preparedEntry.SkillDir = workspace
		defer cleanup()
	}
	if skill.IsPipelineSkill(&preparedEntry) {
		return e.executePipelineSkillDetailed(&preparedEntry, vars, runArgs)
	}
	dataDir := ""
	if e != nil && e.app != nil {
		dataDir = e.app.GetDataDir()
	}
	prep, err := skill.PrepareRunnerExecutionWithProgressWithDataDir(dataDir, &preparedEntry, vars, runArgs, extraEnv, skill.RunnerBackendGUI, nil)
	if err != nil {
		return skillExecutionResult{Captured: cloneStringMapGUI(vars), Err: err}
	}
	for _, warning := range prep.RequirementWarnings {
		log.Printf("[skill-executor] requirement warning for %q: %s", preparedEntry.Name, warning.Message)
	}
	for _, warning := range prep.FileWarnings {
		log.Printf("[skill-executor] file warning for %q: %s", preparedEntry.Name, warning)
	}
	executionSteps := prep.ExecutionSteps
	if len(executionSteps) == 0 {
		return skillExecutionResult{Captured: cloneStringMapGUI(vars), Err: fmt.Errorf("%s", skill.FormatNoExecutableStepsMessage(preparedEntry.Name, &preparedEntry, skill.RunnerBackendGUI))}
	}

	// OpenAI proxy for skills requiring OPENAI_API_KEY.
	// Keep proxy env in the per-run extraEnv map so concurrent skill executions
	// never mutate process-wide environment variables.
	proxyProbeSteps := skill.PrecheckExecutableSteps(executionSteps, vars)
	proxyRequiredEnv := preparedEntry.RequiredEnv
	if len(proxyProbeSteps) == 0 && len(executionSteps) > 0 {
		proxyRequiredEnv = nil
	}
	needsProxy := corelib.NeedsOpenAIProxyAuto(proxyRequiredEnv, extraEnv, proxyProbeSteps, preparedEntry.SkillDir)
	log.Printf("[skill-executor] openai proxy check: needsProxy=%v required_env=%v processEnv_OPENAI_API_KEY=%q",
		needsProxy, preparedEntry.RequiredEnv, truncateEnvForLog(os.Getenv("OPENAI_API_KEY")))
	if needsProxy {
		var proxyCfg corelib.OpenAIProxyConfig
		if e.app != nil {
			llmCfg := e.app.GetMaclawLLMConfig()
			providerName := maclawLLMUsageProviderName(e.app, llmCfg)
			proxyCfg = corelib.OpenAIProxyConfig{
				URL:      llmCfg.URL,
				Key:      llmCfg.Key,
				Model:    llmCfg.Model,
				Protocol: llmCfg.Protocol,
				WireAPI:  llmCfg.WireAPI,
				AuthType: llmCfg.AuthType,
				UsageCallback: func(usage corelib.OpenAIProxyUsage) {
					if providerName == "" {
						return
					}
					e.app.AccumulateLLMTokenUsageWithCache(providerName, usage.InputTokens, usage.OutputTokens, usage.CachedInputTokens, usage.CacheWriteTokens)
				},
			}
		}
		if err := corelib.ValidateOpenAIProxyUpstreamConfig(proxyCfg); err != nil {
			return skillExecutionResult{Captured: cloneStringMapGUI(vars), Err: fmt.Errorf("skill requires OpenAI-compatible environment variables, but the GUI local proxy cannot start because %s [action: configure_llm]", err)}
		}
		proxy := corelib.NewOpenAIProxy(proxyCfg)
		port, proxyErr := proxy.Start()
		if proxyErr != nil {
			return skillExecutionResult{Captured: cloneStringMapGUI(vars), Err: fmt.Errorf("skill requires OpenAI-compatible environment variables, but the GUI local proxy failed to start: %v [action: retry]", proxyErr)}
		}
		defer proxy.Stop()
		if extraEnv == nil {
			extraEnv = map[string]string{}
		}
		extraEnv["OPENAI_API_KEY"] = "sk-maclaw-local-proxy"
		extraEnv["OPENAI_BASE_URL"] = fmt.Sprintf("http://127.0.0.1:%d/v1", port)
		extraEnv["OPENAI_MODEL"] = proxyCfg.Model
		log.Printf("[skill-executor] openai proxy started on port %d for skill %q", port, preparedEntry.Name)
	}

	hasFailure := false
	for _, warning := range prep.Warnings {
		results = append(results, "[Warning] "+warning)
	}
	// Compute the shared Python runtime extra env once outside the step loop.
	runtimeExtraEnv := skill.SharedPythonRuntimeExtraEnvWithDataDir(dataDir, &preparedEntry, os.Environ())
	for i, step := range executionSteps {
		stepExtraEnv := mergeSkillRuntimeExtraEnv(extraEnv, runtimeExtraEnv)
		condition := normalizeSkillStepConditionKind(step.Condition)
		onError := normalizeSkillStepOnErrorKind(step.OnError)
		if condition == skillStepConditionOnFailure && !hasFailure {
			results = append(results, fmt.Sprintf("step %d (%s) skipped: waiting for prior failure", i+1, step.Action))
			continue
		}
		if condition == skillStepConditionOnSuccess && hasFailure {
			results = append(results, fmt.Sprintf("step %d (%s) skipped: prior failure", i+1, step.Action))
			continue
		}
		if step.When != "" && !skill.EvaluateStepWhen(step.When, vars) {
			results = append(results, fmt.Sprintf("step %d (%s) skipped: when=%q", i+1, step.Action, step.When))
			continue
		}
		stepCopy := step
		stepCopyAction := classifySkillStepAction(stepCopy.Action)
		if stepCopyAction == skillStepActionSendInput || stepCopyAction == skillStepActionSendAndObserve {
			resolvedSessionID := resolveSkillStepSessionID(stepCopy, lastSessionID, e.manager)
			if resolvedSessionID != "" {
				if stepCopy.Params == nil {
					stepCopy.Params = map[string]interface{}{}
				}
				stepCopy.Params["session_id"] = resolvedSessionID
			}
		}
		stepCopy = withSkillPreferredShell(stepCopy, preparedEntry.PreferredShell)
		resolvedStep, resolveErr := resolveSkillStep(stepCopy, vars, preparedEntry.SkillDir, prep.Params)
		if resolveErr != nil {
			hasFailure = true
			errMsg := fmt.Sprintf("step %d (%s) parameter binding failed: %s", i+1, step.Action, resolveErr.Error())
			if onError.ShouldContinue() {
				results = append(results, errMsg)
				continue
			}
			results = append(results, errMsg)
			execErr = fmt.Errorf("skill execution stopped at step %d: %w", i+1, resolveErr)
			break
		}
		stepCopy = resolvedStep
		if classifySkillStepAction(stepCopy.Action).IsCraftTool() && len(stepExtraEnv) > 0 {
			if stepCopy.Params == nil {
				stepCopy.Params = map[string]interface{}{}
			}
			skill.MergeExtraEnvParam(stepCopy.Params, stepExtraEnv)
		}
		if classifySkillStepAction(stepCopy.Action).IsCraftTool() {
			if stepCopy.Params == nil {
				stepCopy.Params = map[string]interface{}{}
			}
			if _, exists := stepCopy.Params["user_prompt"]; !exists {
				if prompt := firstNonEmptySkillRunVar(vars, "user_prompt", "input", "query"); prompt != "" {
					stepCopy.Params["user_prompt"] = prompt
				}
			}
		}
		stepCopy = skill.PrepareResolvedStepEnv(stepCopy, preparedEntry.RequiredEnv, stepExtraEnv)
		stepCopy = remapSkillRunStepToWorkspace(stepCopy, sourceSkillDir, preparedEntry.SkillDir)
		if stepCopy.Params == nil {
			stepCopy.Params = map[string]interface{}{}
		}
		stepCopy.Params["_skill_run_id"] = "sync"
		stepCopy.Params["_skill_owner_id"] = ownerID
		restoreEnv := installSkillStepProcessEnv(stepCopy.Action, stepExtraEnv)
		result, err := func() (string, error) {
			defer restoreEnv()
			return e.executeStep(stepCopy, preparedEntry.Description)
		}()
		if classifySkillStepAction(stepCopy.Action) == skillStepActionCreateSession {
			if sessionID := parseCreatedSessionID(result); sessionID != "" {
				lastSessionID = sessionID
			}
		}
		// Capture before error handling so on_error/continue_on_fail paths can
		// still pass structured diagnostics to later steps.
		if len(step.Capture) > 0 && result != "" {
			for varName, value := range skill.CaptureOutputVariables(result, step.Capture) {
				vars[varName] = value
			}
		}
		if err != nil {
			hasFailure = true
			errMsg := fmt.Sprintf("step %d (%s) failed: %s", i+1, step.Action, err.Error())
			if onError == skillStepOnErrorContinue {
				results = append(results, errMsg)
				continue
			}
			results = append(results, errMsg)
			execErr = fmt.Errorf("skill execution stopped at step %d: %w", i+1, err)
			break
		}
		results = append(results, result)
	}
	output := strings.Join(results, "\n")
	if execErr != nil {
		return skillExecutionResult{Output: output, Captured: cloneStringMapGUI(vars), Err: execErr}
	}
	return skillExecutionResult{Output: output, Captured: cloneStringMapGUI(vars)}
}

func cloneStringMapGUI(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstNonEmptySkillRunVar(vars map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(vars[key]); value != "" {
			return value
		}
	}
	return ""
}

func parseCreatedSessionID(result string) string {
	trimmed := strings.TrimSpace(result)
	for _, prefix := range []string{"会话已创建: ID=", "session created: ID="} {
		if strings.HasPrefix(trimmed, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		}
	}
	return ""
}

func skillExecutionRunArgs(userPrompt string) map[string]interface{} {
	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" {
		return nil
	}
	return map[string]interface{}{
		"args":                        userPrompt,
		"input":                       userPrompt,
		"user_prompt":                 userPrompt,
		"_skill_infer_natural_prompt": true,
	}
}

func skillExecutionExtraEnv(runArgs map[string]interface{}) map[string]string {
	return skill.ExtractRunExtraEnvFromArgs(runArgs)
}

func skillRunOwnerIDFromArgs(runArgs map[string]interface{}) string {
	if len(runArgs) == 0 {
		return ""
	}
	for _, key := range []string{"_skill_owner_id", registeredToolPolicyOwnerIDField} {
		if value, ok := runArgs[key].(string); ok {
			if ownerID := strings.TrimSpace(value); ownerID != "" {
				return ownerID
			}
		}
	}
	return ""
}

func skillSSHRuntimeOwner(args map[string]interface{}) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if value, ok := args[registeredToolPolicyOwnerIDField]; ok {
		ownerID, _ := value.(string)
		if strings.TrimSpace(ownerID) == "" {
			return "", fmt.Errorf("runtime owner is missing; isolated runtime will not fall back to desktop loop")
		}
	}
	return skillRunOwnerIDFromArgs(args), nil
}

// Execute runs a Skill by name. Steps are executed sequentially; if a step
// fails and OnError is "stop" (default), execution halts.
// Usage statistics (count, success rate, last error) are updated after execution.
func (e *SkillExecutor) Execute(name string) (string, error) {
	return e.ExecuteWithArgs(name, nil)
}

// ExecuteWithArgs runs a Skill by name with caller-provided run arguments.
// It is used by automatic skill installation paths so the original user
// request can satisfy required_args through the shared runner inference layer.
func (e *SkillExecutor) ExecuteWithArgs(name string, runArgs map[string]interface{}) (string, error) {
	internalPipelineCall := skill.IsInternalPipelineRunArgs(runArgs)
	namedResult := e.executeSkillByNameDetailed(name, runArgs)
	if namedResult.Entry == nil {
		return namedResult.Output, namedResult.Err
	}
	target := namedResult.Entry
	output, execErr := namedResult.Output, namedResult.Err

	if internalPipelineCall {
		if execErr != nil {
			return output, execErr
		}
		return output, nil
	}

	// Update usage statistics under write lock.
	e.mu.Lock()
	skills := e.loadSkills()
	shouldEmitUsageEvent := false
	for i, s := range skills {
		if s.Name == target.Name || s.MatchesName(name) {
			skills[i].UsageCount++
			skills[i].LastUsedAt = time.Now().Format(time.RFC3339)
			if execErr == nil {
				skills[i].SuccessCount++
				skills[i].LastError = ""
			} else {
				skills[i].FailureCount++
				skills[i].LastError = formatExecErrorForStorage(execErr)
			}
			_ = e.saveSkills(skills)
			shouldEmitUsageEvent = true

			// Auto-rate hub skills after execution.
			if normalizeSkillEntrySource(s.Source) == skillEntrySourceHub && s.HubSkillID != "" && e.app.capabilityGapDetector != nil {
				go e.app.capabilityGapDetector.autoRate(
					context.Background(), s.HubSkillID, output, execErr,
				)
			}
			break
		}
	}
	e.mu.Unlock()

	// Notify frontend to refresh skill list with updated stats (outside lock).
	if shouldEmitUsageEvent && e.app != nil {
		e.app.emitEvent(EventSkillUsageUpdated)
	}

	if execErr != nil {
		return output, execErr
	}
	return output, nil
}

type namedSkillExecutionResult struct {
	skillExecutionResult
	Entry *corelib.NLSkillEntry
}

func (e *SkillExecutor) executeSkillByNameDetailed(name string, runArgs map[string]interface{}) namedSkillExecutionResult {
	e.mu.RLock()
	var target *corelib.NLSkillEntry
	for _, s := range e.loadSkills() {
		if s.MatchesName(name) && normalizeSkillEntryStatus(s.Status) == skillEntryStatusActive {
			cp := s
			target = &cp
			break
		}
	}
	e.mu.RUnlock()

	if target == nil {
		return namedSkillExecutionResult{skillExecutionResult: skillExecutionResult{Err: fmt.Errorf("skill %q not found or disabled", name)}}
	}
	if isShellBrowserAutomationSkillEntry(*target) {
		return namedSkillExecutionResult{skillExecutionResult: skillExecutionResult{Err: browserAutomationSkillRejectedError(name)}, Entry: target}
	}

	execResult := e.executeSkillStepsDetailed(target, runArgs)
	return namedSkillExecutionResult{skillExecutionResult: execResult, Entry: target}
}

func (e *SkillExecutor) executePipelineSkillWithArgs(entry *corelib.NLSkillEntry, vars map[string]string, runArgs map[string]interface{}) (string, error) {
	result := e.executePipelineSkillDetailed(entry, vars, runArgs)
	return result.Output, result.Err
}

func (e *SkillExecutor) executePipelineSkillDetailed(entry *corelib.NLSkillEntry, vars map[string]string, runArgs map[string]interface{}) skillExecutionResult {
	if entry == nil {
		return skillExecutionResult{Err: fmt.Errorf("skill entry is nil")}
	}
	if isShellBrowserAutomationSkillEntry(*entry) {
		return skillExecutionResult{Captured: cloneStringMapGUI(vars), Err: browserAutomationSkillRejectedError(entry.Name)}
	}
	if len(entry.Pipeline) == 0 {
		return skillExecutionResult{Err: fmt.Errorf("%s", skill.FormatNoExecutableStepsMessage(entry.Name, entry, skill.RunnerBackendGUI))}
	}
	if vars == nil {
		vars = map[string]string{}
	}
	extraEnv := skill.ExtractRunExtraEnvFromArgs(runArgs)
	dataDir := ""
	if e != nil && e.app != nil {
		dataDir = e.app.GetDataDir()
	}
	prep, err := skill.PreparePipelineRunnerExecutionWithDataDir(dataDir, entry, vars, runArgs, extraEnv, skill.RunnerBackendGUI)
	if err != nil {
		return skillExecutionResult{Captured: cloneStringMapGUI(vars), Err: err}
	}
	for _, warning := range prep.RequirementWarnings {
		log.Printf("[skill-executor] pipeline requirement warning for %q: %s", entry.Name, warning.Message)
	}
	pipelineRunArgs := skill.WithPipelineRunStack(runArgs, entry.Name)
	ownerID := skillRunOwnerIDFromArgs(runArgs)
	if ownerID != "" {
		pipelineRunArgs["_skill_owner_id"] = ownerID
	}
	runCtx := context.Background()
	cancel := func() {}
	if entry.GlobalTimeout > 0 {
		runCtx, cancel = context.WithTimeout(runCtx, time.Duration(entry.GlobalTimeout)*time.Second)
	}
	defer cancel()
	runner := &skill.PipelineRunner{Executor: skillExecutorPipelineExecutor{exec: e, baseRunArgs: pipelineRunArgs, ownerID: ownerID}}
	result, err := runner.Run(runCtx, entry.Pipeline, vars)
	output := skill.FormatPipelineResult(result)
	output = skill.PrefixOutputWithWarnings(output, prep.Warnings)
	captured := cloneStringMapGUI(vars)
	if result != nil && len(result.Vars) > 0 {
		captured = cloneStringMapGUI(result.Vars)
	}
	if err != nil {
		return skillExecutionResult{Output: output, Captured: captured, Err: err}
	}
	if result == nil {
		return skillExecutionResult{Output: output, Captured: captured, Err: fmt.Errorf("pipeline returned no result")}
	}
	if normalizeSkillPipelineStatusFromCore(result.Status) != skillPipelineStatusCompleted {
		if result.Error != "" {
			return skillExecutionResult{Output: output, Captured: captured, Err: fmt.Errorf("%s", result.Error)}
		}
		return skillExecutionResult{Output: output, Captured: captured, Err: fmt.Errorf("pipeline status: %s", result.Status)}
	}
	return skillExecutionResult{Output: output, Captured: captured}
}

type skillExecutorPipelineExecutor struct {
	exec        *SkillExecutor
	baseRunArgs map[string]interface{}
	ownerID     string
}

func (e skillExecutorPipelineExecutor) RunSubSkill(ctx context.Context, skillName string, params map[string]string) (map[string]string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	runArgs := skill.BuildPipelineSubSkillRunArgs(e.baseRunArgs, params)
	if ownerID := strings.TrimSpace(e.ownerID); ownerID != "" {
		runArgs["_skill_owner_id"] = ownerID
	}
	result := e.exec.executeSkillByNameDetailed(skillName, runArgs)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, result.Output, ctxErr
	}
	if result.Err != nil {
		return result.Captured, result.Output, result.Err
	}
	captured := cloneStringMapGUI(result.Captured)
	if captured == nil {
		captured = map[string]string{}
	}
	if strings.TrimSpace(result.Output) != "" {
		captured["output"] = result.Output
	}
	return captured, result.Output, nil
}

func (e *SkillExecutor) RunSubSkill(ctx context.Context, skillName string, params map[string]string) (map[string]string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	runArgs := make(map[string]interface{}, len(params))
	for key, value := range params {
		runArgs[key] = value
	}
	result := e.executeSkillByNameDetailed(skillName, runArgs)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, result.Output, ctxErr
	}
	if result.Err != nil {
		return result.Captured, result.Output, result.Err
	}
	captured := cloneStringMapGUI(result.Captured)
	if captured == nil {
		captured = map[string]string{}
	}
	if strings.TrimSpace(result.Output) != "" {
		captured["output"] = result.Output
	}
	return captured, result.Output, nil
}

// executeStep runs a single skill step.
func (e *SkillExecutor) executeStep(step corelib.NLSkillStep, skillDescription string) (string, error) {
	kind := classifySkillStepAction(step.Action)
	switch kind {
	case skillStepActionCreateSession:
		return "", fmt.Errorf("external coding-session steps are disabled; coding tasks must run through CodingSubAgent")

	case skillStepActionSendInput:
		return "", fmt.Errorf("external coding-session input steps are disabled; coding tasks must run through CodingSubAgent")

	case skillStepActionSendAndObserve:
		return "", fmt.Errorf("external coding-session observe steps are disabled; coding tasks must run through CodingSubAgent")

	case skillStepActionControlSession:
		return "", fmt.Errorf("external coding-session control steps are disabled; coding tasks must run through CodingSubAgent")

	case skillStepActionCallMCPTool:
		serverRef, _ := step.Params["server_id"].(string)
		toolName, _ := step.Params["tool_name"].(string)
		if isDisabledExternalCodingSessionTool(toolName) {
			return "", fmt.Errorf("external coding-session MCP target %q is disabled", toolName)
		}
		var args map[string]interface{}
		switch v := step.Params["arguments"].(type) {
		case map[string]interface{}:
			args = v
		case string:
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				_ = json.Unmarshal([]byte(trimmed), &args)
			}
		}
		if args == nil {
			args = map[string]interface{}{}
		}
		if serverRef == "" || toolName == "" {
			return "", fmt.Errorf("missing server_id or tool_name parameter")
		}
		resolvedID, isLocal, err := e.app.resolveMCPServerRef(serverRef)
		if err != nil {
			return "", err
		}
		if isLocal {
			if e.app.localMCPManager == nil {
				return "", fmt.Errorf("local MCP manager not initialized")
			}
			return e.app.localMCPManager.CallToolForOwner(strings.TrimSpace(nonEmptyStringFromAny(step.Params["_skill_owner_id"])), resolvedID, toolName, args)
		}
		if e.mcpRegistry == nil {
			return "", fmt.Errorf("MCP registry not initialized")
		}
		return e.mcpRegistry.CallToolForOwner(strings.TrimSpace(nonEmptyStringFromAny(step.Params["_skill_owner_id"])), resolvedID, toolName, args)

	case skillStepActionSSH:
		return e.executeSSHStep(step.Params)

	case skillStepActionBash:
		command, _ := step.Params["command"].(string)
		if command == "" {
			return "", fmt.Errorf("missing command parameter")
		}
		return executeBashStep(command, step.Params, e.app)

	case skillStepActionCraftTool:
		if e.app == nil {
			return "", fmt.Errorf("app not initialized")
		}
		return executeCraftToolCore(e.app, nil, step.Params, nil)

	default:
		return "", fmt.Errorf("unknown action: %s", step.Action)
	}
}

func (e *SkillExecutor) ensureSSHManager() *remote.SSHSessionManager {
	if e.sshMgr == nil {
		e.sshMgr = remote.NewSSHSessionManager(nil)
	}
	if e.bgTaskMgr == nil {
		e.bgTaskMgr = remote.NewSSHBackgroundTaskManager(e.sshMgr)
	}
	return e.sshMgr
}

func (e *SkillExecutor) executeSSHStep(args map[string]interface{}) (string, error) {
	actionText, _ := args["action"].(string)
	action := classifySSHToolAction(actionText)
	switch action {
	case sshToolActionConnect:
		return e.sshConnect(args), nil
	case sshToolActionExec:
		return e.sshExec(args), nil
	case sshToolActionExecBackground:
		return e.sshExecBackground(args), nil
	case sshToolActionCheckTask:
		return e.sshCheckTask(args), nil
	case sshToolActionWaitTask:
		return e.sshWaitTask(args), nil
	case sshToolActionListTasks:
		return e.sshListTasks(args), nil
	case sshToolActionKillTask:
		return e.sshKillTask(args), nil
	case sshToolActionUpload:
		return e.sshUpload(args), nil
	case sshToolActionDownload:
		return e.sshDownload(args), nil
	case sshToolActionList:
		return e.sshList(), nil
	case sshToolActionClose:
		return e.sshClose(args), nil
	default:
		return "", fmt.Errorf("未知 SSH 操作 %s; supported: connect/exec/exec_background/check_task/wait_task/list_tasks/kill_task/upload/download/list/close", action)
	}
}

func (e *SkillExecutor) sshConnect(args map[string]interface{}) string {
	mgr := e.ensureSSHManager()
	host, _ := args["host"].(string)
	user, _ := args["user"].(string)
	label, _ := args["label"].(string)
	if host == "" || user == "" {
		return "ssh connect requires host and user"
	}
	port := 22
	if p, ok := args["port"].(float64); ok && p > 0 {
		port = int(p)
	}
	password, _ := args["password"].(string)
	keyPath, _ := args["key_path"].(string)
	if keyPath == "" {
		keyPath, _ = args["private_key_path"].(string)
	}
	cfg := remote.SSHHostConfig{Host: host, Port: port, User: user, Password: password, KeyPath: keyPath, Label: label}
	spec := remote.SSHSessionSpec{HostConfig: cfg, InitialCommand: sshSkillStrArg(args, "initial_command"), Cols: 120, Rows: 40}
	session, err := mgr.Create(spec)
	if err != nil {
		return fmt.Sprintf("ssh connect failed: %v", err)
	}
	return fmt.Sprintf("ssh connected: session_id=%s host=%s", session.ID, session.GetSummary().HostLabel)
}

func (e *SkillExecutor) sshExec(args map[string]interface{}) string {
	mgr := e.ensureSSHManager()
	sessionID, _ := args["session_id"].(string)
	command, _ := args["command"].(string)
	if sessionID == "" || command == "" {
		return "ssh exec requires session_id and command"
	}
	waitSec := 5
	if w, ok := args["wait_seconds"].(float64); ok && w > 0 {
		waitSec = int(w)
	}
	if remote.IsLongRunningCommand(command) && waitSec <= 30 {
		return e.sshExecBackground(args)
	}
	session, ok := mgr.Get(sessionID)
	if !ok {
		return fmt.Sprintf("ssh session %s not found", sessionID)
	}
	reconnectNote := ""
	status, _ := mgr.GetSessionStatus(sessionID)
	sessionDead := status == remote.SessionExited || status == remote.SessionError
	if sessionDead {
		if err := mgr.ReconnectByID(sessionID); err != nil {
			return fmt.Sprintf("ssh reconnect failed: %v", err)
		}
		reconnectNote = "reconnected; "
		time.Sleep(2 * time.Second)
	}
	linesBefore := session.LineCount()
	if sessionDead {
		if err := mgr.WriteInput(sessionID, command); err != nil {
			return fmt.Sprintf("%sssh write failed: %v", reconnectNote, err)
		}
	} else {
		reconnected, err := mgr.WriteInputChecked(sessionID, command)
		if err != nil {
			return fmt.Sprintf("ssh write failed: %v", err)
		}
		if reconnected {
			reconnectNote = "reconnected; "
			time.Sleep(2 * time.Second)
			linesBefore = session.LineCount()
		}
	}
	if waitSec > 600 {
		waitSec = 600
	}
	newLines, status := mgr.WaitForOutput(sessionID, linesBefore, time.Duration(waitSec)*time.Second)
	output := strings.Join(newLines, "\n")
	if output == "" {
		output = "(no output)"
	}
	if len(output) > 8000 {
		output = output[:4000] + "\n... (truncated) ...\n" + output[len(output)-4000:]
	}
	return fmt.Sprintf("%s[%s] status: %s\n$ %s\n%s", reconnectNote, sessionID, string(status), command, output)
}

func (e *SkillExecutor) sshExecBackground(args map[string]interface{}) string {
	mgr := e.ensureSSHManager()
	ownerID, err := skillSSHRuntimeOwner(args)
	if err != nil {
		return fmt.Sprintf("ssh exec_background failed: %v", err)
	}
	sessionID, _ := args["session_id"].(string)
	command, _ := args["command"].(string)
	taskRole, _ := args["task_role"].(string)
	if sessionID == "" || command == "" {
		return "ssh exec_background requires session_id and command"
	}
	status, _ := mgr.GetSessionStatus(sessionID)
	if status == remote.SessionExited || status == remote.SessionError {
		if err := mgr.ReconnectByID(sessionID); err != nil {
			return fmt.Sprintf("ssh reconnect failed: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
	task, err := e.bgTaskMgr.SubmitWithOwner(sessionID, command, taskRole, ownerID)
	if err != nil {
		return fmt.Sprintf("background task submit failed: %v", err)
	}
	if e.app != nil {
		e.app.emitEvent("background-loops-changed")
	}
	return fmt.Sprintf("background task started\ntask_id: %s\nrole: %s\ncommand: %s\nlog_file: %s\npid: %s\nstatus: running", task.TaskID, task.TaskRole, task.Command, task.LogFile, task.PID)
}

func (e *SkillExecutor) sshCheckTask(args map[string]interface{}) string {
	if e.bgTaskMgr == nil {
		return "background task manager is not initialized"
	}
	ownerID, ownerErr := skillSSHRuntimeOwner(args)
	if ownerErr != nil {
		return fmt.Sprintf("check task failed: %v", ownerErr)
	}
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return "ssh check_task requires task_id"
	}
	if err := authorizeSSHBackgroundTaskOwner(e.bgTaskMgr, taskID, ownerID); err != nil {
		return fmt.Sprintf("check task failed: %v", err)
	}
	tailLines := 50
	if t, ok := args["tail_lines"].(float64); ok && t > 0 {
		tailLines = int(t)
	}
	result, err := e.bgTaskMgr.CheckTaskForOwner(taskID, tailLines, ownerID)
	if err != nil {
		return fmt.Sprintf("check task failed: %v", err)
	}
	if e.app != nil {
		e.app.emitEvent("background-loops-changed")
	}
	return formatSSHBackgroundTaskStatus(result)
}

func (e *SkillExecutor) sshWaitTask(args map[string]interface{}) string {
	ownerID, err := skillSSHRuntimeOwner(args)
	if err != nil {
		return fmt.Sprintf("wait_task failed: %v", err)
	}
	return waitSSHBackgroundTaskForOwner(e.bgTaskMgr, args, ownerID, "background task manager is not initialized", "ssh wait_task requires task_id", func() {
		if e.app != nil {
			e.app.emitEvent("background-loops-changed")
		}
	})
}

func (e *SkillExecutor) sshListTasks(args map[string]interface{}) string {
	if e.bgTaskMgr == nil {
		return "当前无后台任务"
	}
	ownerID, err := skillSSHRuntimeOwner(args)
	if err != nil {
		return fmt.Sprintf("list tasks failed: %v", err)
	}
	tasks := e.bgTaskMgr.ListTasksForOwner(ownerID)
	if len(tasks) == 0 {
		return "当前无后台任务"
	}
	rows := make([]string, 0, len(tasks))
	for _, t := range tasks {
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		role := strings.TrimSpace(t.TaskRole)
		if role == "" {
			role = "command"
		}
		rows = append(rows, fmt.Sprintf("  - %s | PID: %s | role: %s | status: %s | elapsed: %s\n    command: %s\n", t.TaskID, t.PID, role, t.Status, elapsed, t.Command))
	}
	if len(rows) == 0 {
		return "当前无后台任务"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "background tasks: %d\n", len(rows))
	for _, row := range rows {
		sb.WriteString(row)
	}
	return sb.String()
}

func (e *SkillExecutor) sshKillTask(args map[string]interface{}) string {
	if e.bgTaskMgr == nil {
		return "background task manager is not initialized"
	}
	ownerID, ownerErr := skillSSHRuntimeOwner(args)
	if ownerErr != nil {
		return fmt.Sprintf("kill task failed: %v", ownerErr)
	}
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return "ssh kill_task requires task_id"
	}
	if err := authorizeSSHBackgroundTaskOwner(e.bgTaskMgr, taskID, ownerID); err != nil {
		return fmt.Sprintf("kill task failed: %v", err)
	}
	if err := e.bgTaskMgr.KillTaskForOwner(taskID, ownerID); err != nil {
		return fmt.Sprintf("kill task failed: %v", err)
	}
	if e.app != nil {
		e.app.emitEvent("background-loops-changed")
	}
	return fmt.Sprintf("background task %s killed", taskID)
}

func (e *SkillExecutor) sshUpload(args map[string]interface{}) string {
	mgr := e.ensureSSHManager()
	sessionID, _ := args["session_id"].(string)
	localPath, _ := args["local_path"].(string)
	remotePath, _ := args["remote_path"].(string)
	if sessionID == "" || localPath == "" || remotePath == "" {
		return "ssh upload requires session_id, local_path, and remote_path"
	}
	result, err := mgr.SFTPTransfer(sessionID, "upload", localPath, remotePath)
	if err != nil {
		return fmt.Sprintf("upload failed: %v", err)
	}
	return fmt.Sprintf("uploaded %s -> %s\n%s", localPath, remotePath, result)
}

func (e *SkillExecutor) sshDownload(args map[string]interface{}) string {
	mgr := e.ensureSSHManager()
	sessionID, _ := args["session_id"].(string)
	localPath, _ := args["local_path"].(string)
	remotePath, _ := args["remote_path"].(string)
	if sessionID == "" || localPath == "" || remotePath == "" {
		return "ssh download requires session_id, local_path, and remote_path"
	}
	result, err := mgr.SFTPTransfer(sessionID, "download", localPath, remotePath)
	if err != nil {
		return fmt.Sprintf("download failed: %v", err)
	}
	return fmt.Sprintf("downloaded %s -> %s\n%s", remotePath, localPath, result)
}

func (e *SkillExecutor) sshList() string {
	if e.sshMgr == nil {
		return "当前无活跃 SSH 会话"
	}
	sessions := e.sshMgr.List()
	if len(sessions) == 0 {
		return "当前无活跃 SSH 会话"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ssh sessions: %d\n", len(sessions)))
	for _, s := range sessions {
		summary := s.GetSummary()
		sb.WriteString(fmt.Sprintf("  - %s | %s | status: %s\n", s.ID, summary.HostLabel, summary.Status))
	}
	poolStats := e.sshMgr.Pool().Stats()
	if len(poolStats) > 0 {
		sb.WriteString("connection pool:\n")
		for hostID, ref := range poolStats {
			sb.WriteString(fmt.Sprintf("  - %s (refs: %d)\n", hostID, ref))
		}
	}
	return sb.String()
}

func (e *SkillExecutor) sshClose(args map[string]interface{}) string {
	if e.sshMgr == nil {
		return "ssh manager is not initialized"
	}
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "ssh close requires session_id"
	}
	if err := e.sshMgr.Kill(sessionID); err != nil {
		return fmt.Sprintf("close ssh session failed: %v", err)
	}
	return fmt.Sprintf("ssh session %s closed", sessionID)
}
func sshSkillStrArg(args map[string]interface{}, key string) string {
	s, _ := args[key].(string)
	return s
}

func skillCreateSessionGuard(skillDescription string, step corelib.NLSkillStep) string {
	taskText := resolveSkillTaskText(skillDescription, step)
	result := classifyTaskIntent(taskText)
	switch result.Intent {
	case intentCoding:
		return "External coding-session steps are disabled. Coding tasks should be routed through CodingSubAgent."
	case intentSSH:
		return fmt.Sprintf("SSH/server operation task detected (%s). Use ssh(action=\"connect\", ...), ssh(action=\"exec\", ...), ssh(action=\"exec_background\", ...), and upload/download/check_task instead of external coding sessions.", formatIntentEvidence(result))
	case intentNonCoding:
		return fmt.Sprintf("Not a coding task (%s). Prefer direct file/tool execution instead of external coding sessions.", formatIntentEvidence(result))
	case intentUnknown, intentAmbiguous:
		return fmt.Sprintf("Cannot determine whether this needs a coding session (%s). External coding sessions are disabled; clarify before routing to CodingSubAgent.", formatIntentEvidence(result))
	default:
		return ""
	}
}

func resolveSkillTaskText(skillDescription string, step corelib.NLSkillStep) string {
	candidates := []string{
		stringParam(step.Params, "task"),
		stringParam(step.Params, "task_description"),
		stringParam(step.Params, "description"),
		stringParam(step.Params, "prompt"),
		skillDescription,
	}
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			return trimmed
		}
	}
	return stringParam(step.Params, "project_path")
}

func stringParam(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	value, _ := params[key].(string)
	return value
}

// --- Wails binding functions ---

// ListNLSkills returns all registered NL Skill definitions (Wails binding).
func (a *App) ListNLSkills() []NLSkillDefinition {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil
	}
	return a.skillExecutor.List()
}

const maxSkillAppInputFileBytes = 25 * 1024 * 1024
const staleSkillAppInputFileAge = 24 * time.Hour

// StageSkillAppInputFile stores an uploaded app input file in MaClaw temp storage.
func (a *App) StageSkillAppInputFile(fileName, mimeType string, lastModified int64, contentBase64 string) (*SkillAppInputFileRef, error) {
	name := sanitizeSkillAppInputFileName(fileName)
	if name == "" {
		return nil, fmt.Errorf("file name is required")
	}
	if strings.TrimSpace(contentBase64) == "" {
		return nil, fmt.Errorf("file content is required")
	}
	data, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid file content encoding: %w", err)
	}
	if len(data) > maxSkillAppInputFileBytes {
		return nil, fmt.Errorf("file is too large for app staging: %d bytes > %d bytes", len(data), maxSkillAppInputFileBytes)
	}
	root := filepath.Join(a.GetTempDir(), "app-inputs")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create app input staging root: %w", err)
	}
	if _, err := a.cleanupStaleSkillAppInputDirs(staleSkillAppInputFileAge); err != nil {
		log.Printf("[skill-app] cleanup stale staged input dirs failed: %v", err)
	}
	dir, err := os.MkdirTemp(root, "input-*")
	if err != nil {
		return nil, fmt.Errorf("create app input staging dir: %w", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("write app input file: %w", err)
	}
	return &SkillAppInputFileRef{
		Name:         name,
		Size:         int64(len(data)),
		Type:         strings.TrimSpace(mimeType),
		LastModified: lastModified,
		StagedPath:   path,
		Transfer:     "staged_file",
	}, nil
}

func (a *App) cleanupStaleSkillAppInputDirs(maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		return 0, nil
	}
	root := filepath.Join(a.GetTempDir(), "app-inputs")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	var failures []string
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "input-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			failures = append(failures, fmt.Sprintf("stat staged input dir %s: %v", entry.Name(), err))
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			failures = append(failures, fmt.Sprintf("remove stale staged input dir %s: %v", path, err))
			continue
		}
		removed++
	}
	if len(failures) > 0 {
		return removed, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return removed, nil
}

func (a *App) cleanupStagedSkillAppInputFilesFromRunArgs(runArgs map[string]interface{}) error {
	paths := stagedAppInputPathsFromRunArgs(runArgs)
	if len(paths) == 0 {
		return nil
	}
	root := filepath.Clean(filepath.Join(a.GetTempDir(), "app-inputs"))
	var failures []string
	for _, path := range paths {
		cleanPath := filepath.Clean(strings.TrimSpace(path))
		if cleanPath == "" || cleanPath == "." {
			continue
		}
		rel, err := filepath.Rel(root, cleanPath)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || (filepath.IsAbs(rel) && strings.HasPrefix(rel, "..")) {
			failures = append(failures, fmt.Sprintf("refused to cleanup staged app input outside temp dir: %s", cleanPath))
			continue
		}
		if err := os.Remove(cleanPath); err != nil && !os.IsNotExist(err) {
			failures = append(failures, fmt.Sprintf("cleanup staged app input %s: %v", cleanPath, err))
			continue
		}
		parent := filepath.Dir(cleanPath)
		if parent != root {
			_ = os.Remove(parent)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func stagedAppInputPathsFromRunArgs(runArgs map[string]interface{}) []string {
	if len(runArgs) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(value interface{}) {
		path := ""
		if m, ok := value.(map[string]interface{}); ok {
			path, _ = m["staged_path"].(string)
		}
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, exists := seen[path]; exists {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	addPath := func(value interface{}) {
		path := ""
		switch v := value.(type) {
		case string:
			path = strings.TrimSpace(v)
		}
		if path == "" {
			return
		}
		if _, exists := seen[path]; exists {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	add(runArgs["file"])
	if files, ok := runArgs["files"].([]interface{}); ok {
		for _, item := range files {
			add(item)
		}
	}
	if files, ok := runArgs["files"].([]map[string]interface{}); ok {
		for _, item := range files {
			add(item)
		}
	}
	// paramMapping mode: staged paths passed as _staged_cleanup_paths control key
	if paths, ok := runArgs["_staged_cleanup_paths"].([]interface{}); ok {
		for _, item := range paths {
			addPath(item)
		}
	}
	if paths, ok := runArgs["_staged_cleanup_paths"].([]string); ok {
		for _, item := range paths {
			addPath(item)
		}
	}
	return out
}

func withSkillAppInputFileAliases(runArgs map[string]interface{}) map[string]interface{} {
	if len(runArgs) == 0 {
		return runArgs
	}
	file, ok := runArgs["file"].(map[string]interface{})
	if !ok {
		return runArgs
	}
	stagedPath, _ := file["staged_path"].(string)
	stagedPath = strings.TrimSpace(stagedPath)
	if stagedPath == "" {
		return runArgs
	}
	out := make(map[string]interface{}, len(runArgs)+5)
	for key, value := range runArgs {
		out[key] = value
	}
	addStringAlias := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, exists := out[key]; !exists {
			out[key] = value
		}
	}
	addStringAlias("file_path", stagedPath)
	addStringAlias("input_file_path", stagedPath)
	addStringAlias("local_file_path", stagedPath)
	addStringAlias("uploaded_file_path", stagedPath)
	// Skill templates use {{input}} for the input file path. Set "input" alias
	// so NormalizeRunVars can resolve it directly without relying on inference.
	addStringAlias("input", stagedPath)
	if _, exists := out["file_paths"]; !exists {
		if paths := stagedAppInputPathsFromRunArgs(out); len(paths) > 0 {
			out["file_paths"] = paths
		}
	}
	if name, _ := file["name"].(string); strings.TrimSpace(name) != "" {
		addStringAlias("file_name", name)
	}

	// Synthesize "output" path for file-conversion skills when the caller
	// provided output_mode (format name like "docx") but not an explicit
	// output file path. Derive: same directory as input, same base name,
	// extension replaced by output_mode.
	if _, hasOutput := out["output"]; !hasOutput {
		outputMode := skillAppOutputModeFromRunArgs(out)
		if outputMode != "" {
			outputPath := synthesizeSkillAppOutputPath(stagedPath, outputMode)
			if outputPath != "" {
				if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
					log.Printf("[skill-app] create synthesized output dir failed path=%q err=%v", filepath.Dir(outputPath), err)
				}
				out["output"] = outputPath
				log.Printf("[skill-app] synthesized output path: %s (input=%s, mode=%s)", outputPath, filepath.Base(stagedPath), outputMode)
			}
		}
	}
	return out
}

// skillAppOutputModeFromRunArgs extracts the output format identifier from
// app-panel run args. The frontend passes it as "output_mode" in legacy mode.
// We intentionally do NOT check "format" here — it is too generic and may
// mean data serialization format (json/xml) rather than output file format.
func skillAppOutputModeFromRunArgs(runArgs map[string]interface{}) string {
	for _, key := range []string{"output_mode", "outputMode", "output_format"} {
		if raw, ok := runArgs[key]; ok {
			if s, ok := raw.(string); ok {
				s = strings.TrimSpace(strings.ToLower(s))
				if s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// synthesizeSkillAppOutputPath derives an output file path from the input path
// and the desired output format extension. Returns empty string if synthesis
// is not possible (e.g. missing input path or format).
//
// Strategy: place ordinary outputs next to the input file with the same base
// name and the target format as extension. App-staged input files use a
// persistent output directory so staged-input cleanup cannot delete artifacts.
func synthesizeSkillAppOutputPath(inputPath, outputFormat string) string {
	inputPath = strings.TrimSpace(inputPath)
	outputFormat = strings.TrimSpace(strings.ToLower(outputFormat))
	if inputPath == "" || outputFormat == "" {
		return ""
	}
	// Normalize format to a file extension (no leading dot)
	outputFormat = strings.TrimPrefix(outputFormat, ".")

	dir := filepath.Dir(inputPath)
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if base == "" {
		base = "output"
	}
	if outputDir := persistentSkillAppOutputDir(inputPath); outputDir != "" {
		dir = outputDir
	}

	outputPath := filepath.Join(dir, base+"."+outputFormat)
	// Avoid overwriting input when formats match (e.g. pdf→pdf)
	if strings.EqualFold(filepath.Clean(outputPath), filepath.Clean(inputPath)) {
		outputPath = filepath.Join(dir, base+"_output."+outputFormat)
	}
	return outputPath
}

func persistentSkillAppOutputDir(inputPath string) string {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return ""
	}
	inputDir := filepath.Dir(filepath.Clean(inputPath))
	stagingRoot := filepath.Clean(filepath.Join(corelib.MaclawBaseDir(), "temp", "app-inputs"))
	rel, err := filepath.Rel(stagingRoot, inputDir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) == 0 || !strings.HasPrefix(parts[0], "input-") {
		return ""
	}
	return filepath.Join(corelib.MaclawAppOutputsDir(), parts[0])
}

func sanitizeSkillAppInputFileName(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', 0:
			return '-'
		default:
			return r
		}
	}, name)
	return strings.TrimSpace(name)
}

// ListSkillAppManifests returns tool-app declarations from installed skills.
func (a *App) ListSkillAppManifests() []SkillAppManifestEntry {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil
	}
	out := []SkillAppManifestEntry{}
	seen := map[string]struct{}{}
	addApp := func(skillName string, app SkillAppManifestEntry, definitionFile string) {
		app.ID = strings.TrimSpace(app.ID)
		app.Name = strings.TrimSpace(app.Name)
		if app.ID == "" || app.Name == "" {
			return
		}
		if app.SkillID == "" {
			app.SkillID = skillName
		}
		app.SkillID = strings.TrimSpace(app.SkillID)
		if app.Category == "" {
			app.Category = "Skill"
		}
		app.Icon = normalizeSkillAppIconName(app.Icon)
		app.CustomIconDataURL = normalizeMaclawAppCustomIconDataURL(app.CustomIconDataURL)
		app.InputMode = normalizeSkillAppInputMode(app.InputMode)
		app.OutputModes = normalizeSkillAppOutputModes(app.OutputModes)
		app.Fields = normalizeSkillAppFields(app.Fields)
		app.AppDefinitionFile = strings.TrimSpace(definitionFile)
		key := app.SkillID + ":" + app.ID
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, app)
	}
	for _, item := range a.skillExecutor.loadSkills() {
		skillDir := strings.TrimSpace(item.SkillDir)
		if skillDir == "" {
			continue
		}
		if metadata := readMaclawAppYAMLMetadata(skillDir); metadata.AddToAppPanel != nil && !*metadata.AddToAppPanel {
			continue
		}
		manifestPath := filepath.Join(skillDir, "maclaw.apps.json")
		data, err := os.ReadFile(manifestPath)
		if err == nil {
			var manifest skillAppManifestFile
			if err := json.Unmarshal(data, &manifest); err == nil && strings.TrimSpace(manifest.PrivateMarker) == "v1" {
				for _, app := range manifest.Apps {
					addApp(item.Name, app, "maclaw.apps.json")
				}
			}
		}
		for _, entry := range candidateMaclawAppDefinitionFiles(skillDir) {
			app, ok := readMaclawAppDefinitionAsSkillApp(filepath.Join(skillDir, entry), item.Name)
			if !ok {
				continue
			}
			addApp(item.Name, app, entry)
		}
	}
	return out
}

// SaveMaclawAppDefinitionForSkill writes a MaClaw App definition into an existing
// skill directory as maclaw.app.json or updates the matching maclaw.apps.json entry.
func (a *App) SaveMaclawAppDefinitionForSkill(skillName string, appJSON string) (map[string]any, error) {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not initialized")
	}
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	var target *corelib.NLSkillEntry
	for _, item := range a.skillExecutor.loadSkills() {
		if item.Name == skillName {
			clone := item
			target = &clone
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("skill %q not found", skillName)
	}
	skillDir := strings.TrimSpace(target.SkillDir)
	if skillDir == "" {
		return nil, fmt.Errorf("skill %q has no writable skill directory", skillName)
	}
	cleanDir, err := filepath.Abs(skillDir)
	if err != nil {
		return nil, fmt.Errorf("resolve skill directory: %w", err)
	}
	info, err := os.Stat(cleanDir)
	if err != nil {
		return nil, fmt.Errorf("stat skill directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skill directory is not a directory")
	}
	doc, appID, appName, err := normalizeMaclawAppDefinitionForSkill(appJSON, skillName)
	if err != nil {
		return nil, err
	}
	appDefinitionFile := maclawAppDefinitionFileFromDoc(doc)
	if appDefinitionFile == "maclaw.apps.json" {
		data, err := writeMaclawAppsManifestEntry(cleanDir, doc, skillName)
		if err != nil {
			return nil, err
		}
		skillYAMLUpdated, err := ensureMaclawAppEntryInSkillYAML(cleanDir, "maclaw.apps.json")
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"skill_name":            skillName,
			"app_id":                appID,
			"app_name":              appName,
			"app_definition_file":   "maclaw.apps.json",
			"app_definition_sha256": sha256Hex(data),
			"skill_yaml_updated":    skillYAMLUpdated,
		}, nil
	}
	outPath := filepath.Join(cleanDir, "maclaw.app.json")
	resolvedOut, err := filepath.Abs(outPath)
	if err != nil {
		return nil, fmt.Errorf("resolve app definition path: %w", err)
	}
	if filepath.Dir(resolvedOut) != cleanDir {
		return nil, fmt.Errorf("app definition path escapes skill directory")
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode maclaw app definition: %w", err)
	}
	if err := configfile.AtomicWrite(resolvedOut, append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write maclaw.app.json: %w", err)
	}
	skillYAMLUpdated, err := ensureMaclawAppEntryInSkillYAML(cleanDir, "maclaw.app.json")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"skill_name":            skillName,
		"app_id":                appID,
		"app_name":              appName,
		"app_definition_file":   "maclaw.app.json",
		"app_definition_sha256": sha256Hex(data),
		"skill_yaml_updated":    skillYAMLUpdated,
	}, nil
}

func (a *App) RecordMaclawAppRunEvidenceForSkill(skillName string, appID string, definitionHash string, runID string, artifactPath string, verifiedAt string) (map[string]any, error) {
	a.ensureRemoteInfra()
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(verifiedAt) == "" {
		verifiedAt = time.Now().Format(time.RFC3339)
	}
	skillDir, err := a.skillDirByName(skillName)
	if err != nil {
		return nil, err
	}
	artifactName := ""
	if trimmed := strings.TrimSpace(artifactPath); trimmed != "" {
		artifactName = filepath.Base(trimmed)
	}
	evidence := map[string]any{
		"runId":           strings.TrimSpace(runID),
		"verifiedAt":      strings.TrimSpace(verifiedAt),
		"definitionHash":  strings.TrimSpace(definitionHash),
		"artifactPresent": artifactName != "",
		"artifactName":    artifactName,
	}
	preferredEntry := readMaclawAppEntryFromSkillYAML(skillDir)
	if preferredEntry == "maclaw.apps.json" {
		return recordMaclawAppsRunEvidence(skillName, skillDir, appID, evidence, artifactName)
	}
	if preferredEntry == "maclaw.app.json" {
		return recordMaclawSingleAppRunEvidence(skillName, skillDir, appID, evidence, artifactName)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "maclaw.app.json")); err == nil {
		return recordMaclawSingleAppRunEvidence(skillName, skillDir, appID, evidence, artifactName)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat maclaw app definition: %w", err)
	}
	return recordMaclawAppsRunEvidence(skillName, skillDir, appID, evidence, artifactName)
}

func recordMaclawSingleAppRunEvidence(skillName string, skillDir string, appID string, evidence map[string]any, artifactName string) (map[string]any, error) {
	path := filepath.Join(skillDir, "maclaw.app.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read maclaw app definition: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("decode maclaw app definition: %w", err)
	}
	app, _ := doc["app"].(map[string]any)
	if app == nil {
		return nil, fmt.Errorf("maclaw app definition app must be an object")
	}
	if id := strings.TrimSpace(stringMapValue(app, "id")); id != "" && strings.TrimSpace(appID) != "" && !maclawAppDefinitionIDMatches(id, appID, skillName) {
		return nil, fmt.Errorf("maclaw app id %q does not match evidence app %q", id, strings.TrimSpace(appID))
	}
	governance, _ := app["governance"].(map[string]any)
	if governance == nil {
		governance = map[string]any{}
		app["governance"] = governance
	}
	governance["testEvidence"] = mergeMaclawAppRunEvidence(governance["testEvidence"], evidence)
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode maclaw app definition: %w", err)
	}
	if err := configfile.AtomicWrite(path, append(out, '\n')); err != nil {
		return nil, fmt.Errorf("write maclaw app definition: %w", err)
	}
	return maclawAppRunEvidenceResult(skillName, "maclaw.app.json", out, evidence, artifactName), nil
}

func recordMaclawAppsRunEvidence(skillName string, skillDir string, appID string, evidence map[string]any, artifactName string) (map[string]any, error) {
	path := filepath.Join(skillDir, "maclaw.apps.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read maclaw.apps.json: %w", err)
	}
	var manifest skillAppManifestFile
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode maclaw.apps.json: %w", err)
	}
	if strings.TrimSpace(manifest.PrivateMarker) != "v1" {
		return nil, fmt.Errorf("maclaw.apps.json x_maclaw_apps must be v1")
	}
	evidenceAppID := strings.TrimSpace(appID)
	targetIndex := -1
	fallbackIndex := -1
	for index := range manifest.Apps {
		id := strings.TrimSpace(manifest.Apps[index].ID)
		if id == "" {
			continue
		}
		if evidenceAppID != "" && id == evidenceAppID {
			targetIndex = index
			break
		}
		if fallbackIndex == -1 && maclawAppDefinitionIDMatches(id, evidenceAppID, skillName) {
			fallbackIndex = index
		}
	}
	if targetIndex == -1 {
		targetIndex = fallbackIndex
	}
	if targetIndex == -1 && evidenceAppID == "" && len(manifest.Apps) == 1 {
		targetIndex = 0
	}
	if targetIndex == -1 {
		return nil, fmt.Errorf("maclaw app %q not found in maclaw.apps.json", evidenceAppID)
	}
	if manifest.Apps[targetIndex].Governance == nil {
		manifest.Apps[targetIndex].Governance = map[string]any{}
	}
	manifest.Apps[targetIndex].Governance["testEvidence"] = mergeMaclawAppRunEvidence(manifest.Apps[targetIndex].Governance["testEvidence"], evidence)
	out, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode maclaw.apps.json: %w", err)
	}
	if err := configfile.AtomicWrite(path, append(out, '\n')); err != nil {
		return nil, fmt.Errorf("write maclaw.apps.json: %w", err)
	}
	return maclawAppRunEvidenceResult(skillName, "maclaw.apps.json", out, evidence, artifactName), nil
}

func mergeMaclawAppRunEvidence(existing any, incoming map[string]any) map[string]any {
	merged := map[string]any{}
	if current, ok := existing.(map[string]any); ok {
		for key, value := range current {
			merged[key] = value
		}
	}
	for key, value := range incoming {
		merged[key] = mergeMaclawAppRunEvidenceValue(merged[key], value)
	}
	return merged
}

func mergeMaclawAppRunEvidenceValue(existing any, incoming any) any {
	if incoming == nil {
		return existing
	}
	switch typed := incoming.(type) {
	case map[string]any:
		if len(typed) == 0 {
			return existing
		}
		if current, ok := existing.(map[string]any); ok {
			return mergeMaclawAppRunEvidence(current, typed)
		}
		return typed
	case []any:
		if len(typed) == 0 {
			return existing
		}
		return typed
	default:
		return incoming
	}
}

func maclawAppDefinitionIDMatches(definitionID string, appID string, skillName string) bool {
	definitionID = strings.TrimSpace(definitionID)
	appID = strings.TrimSpace(appID)
	skillName = strings.TrimSpace(skillName)
	if definitionID == "" || appID == "" {
		return false
	}
	if definitionID == appID {
		return true
	}
	if skillName != "" && appID == fmt.Sprintf("skill-app-%s-%s", skillName, definitionID) {
		return true
	}
	return strings.HasSuffix(appID, "-"+definitionID)
}

func maclawAppRunEvidenceResult(skillName string, appDefinitionFile string, out []byte, evidence map[string]any, artifactName string) map[string]any {
	return map[string]any{
		"skill_name":            skillName,
		"app_definition_file":   appDefinitionFile,
		"app_definition_sha256": sha256Hex(out),
		"test_run_id":           strings.TrimSpace(fmt.Sprint(evidence["runId"])),
		"test_verified_at":      strings.TrimSpace(fmt.Sprint(evidence["verifiedAt"])),
		"test_definition_hash":  strings.TrimSpace(fmt.Sprint(evidence["definitionHash"])),
		"test_artifact_present": artifactName != "",
		"test_artifact_name":    artifactName,
	}
}

func (a *App) skillDirByName(skillName string) (string, error) {
	if a.skillExecutor == nil {
		return "", fmt.Errorf("skill executor not initialized")
	}
	for _, item := range a.skillExecutor.loadSkills() {
		if item.Name != skillName {
			continue
		}
		skillDir := strings.TrimSpace(item.SkillDir)
		if skillDir == "" {
			return "", fmt.Errorf("skill %q has no writable skill directory", skillName)
		}
		cleanDir, err := filepath.Abs(skillDir)
		if err != nil {
			return "", fmt.Errorf("resolve skill directory: %w", err)
		}
		info, err := os.Stat(cleanDir)
		if err != nil {
			return "", fmt.Errorf("stat skill directory: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("skill directory is not a directory")
		}
		return cleanDir, nil
	}
	return "", fmt.Errorf("skill %q not found", skillName)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func ensureMaclawAppEntryInSkillYAML(skillDir string, entry string) (bool, error) {
	defPath, _ := findSkillDefinitionFile(skillDir)
	if defPath == "" {
		return false, nil
	}
	data, err := os.ReadFile(defPath)
	if err != nil {
		return false, fmt.Errorf("read skill definition: %w", err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, fmt.Errorf("parse skill definition: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	block, _ := doc["maclaw_app"].(map[string]any)
	if block == nil {
		block = map[string]any{}
		doc["maclaw_app"] = block
	}
	changed := false
	if strings.TrimSpace(stringMapValue(block, "entry")) != entry {
		block["entry"] = entry
		changed = true
	}
	if strings.TrimSpace(stringMapValue(block, "status")) == "" {
		block["status"] = "draft"
		changed = true
	}
	if _, ok := block["add_to_app_panel"]; !ok {
		block["add_to_app_panel"] = true
		changed = true
	}
	if !changed {
		return false, nil
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return false, fmt.Errorf("encode skill definition: %w", err)
	}
	if err := configfile.AtomicWrite(defPath, out); err != nil {
		return false, fmt.Errorf("write skill definition: %w", err)
	}
	return true, nil
}

func maclawAppDefinitionFileFromDoc(doc map[string]any) string {
	app, _ := doc["app"].(map[string]any)
	binding, _ := app["binding"].(map[string]any)
	skillBinding, _ := binding["skill"].(map[string]any)
	appSkillBinding, _ := binding["appSkill"].(map[string]any)
	value := firstNonEmptySkillAppString(
		stringMapValue(skillBinding, "appDefinitionFile"),
		stringMapValue(skillBinding, "app_definition_file"),
		stringMapValue(appSkillBinding, "appDefinitionFile"),
		stringMapValue(appSkillBinding, "app_definition_file"),
	)
	if value == "maclaw.apps.json" {
		return "maclaw.apps.json"
	}
	return "maclaw.app.json"
}

func writeMaclawAppsManifestEntry(skillDir string, doc map[string]any, skillName string) ([]byte, error) {
	entry, err := skillAppManifestEntryFromDefinitionDoc(doc, skillName)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(skillDir, "maclaw.apps.json")
	resolvedPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve maclaw.apps.json path: %w", err)
	}
	if filepath.Dir(resolvedPath) != skillDir {
		return nil, fmt.Errorf("maclaw.apps.json path escapes skill directory")
	}
	manifest := skillAppManifestFile{PrivateMarker: "v1"}
	if data, err := os.ReadFile(resolvedPath); err == nil && strings.TrimSpace(string(data)) != "" {
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("decode maclaw.apps.json: %w", err)
		}
		if strings.TrimSpace(manifest.PrivateMarker) != "" && strings.TrimSpace(manifest.PrivateMarker) != "v1" {
			return nil, fmt.Errorf("maclaw.apps.json x_maclaw_apps must be v1")
		}
		manifest.PrivateMarker = "v1"
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read maclaw.apps.json: %w", err)
	}
	replaced := false
	for index := range manifest.Apps {
		if strings.TrimSpace(manifest.Apps[index].ID) == entry.ID {
			entry.Governance = mergeMaclawAppGovernance(manifest.Apps[index].Governance, entry.Governance)
			manifest.Apps[index] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		manifest.Apps = append(manifest.Apps, entry)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode maclaw.apps.json: %w", err)
	}
	if err := configfile.AtomicWrite(resolvedPath, append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write maclaw.apps.json: %w", err)
	}
	return data, nil
}

func skillAppManifestEntryFromDefinitionDoc(doc map[string]any, skillName string) (SkillAppManifestEntry, error) {
	app, ok := doc["app"].(map[string]any)
	if !ok {
		return SkillAppManifestEntry{}, fmt.Errorf("maclaw app definition app must be an object")
	}
	binding, _ := app["binding"].(map[string]any)
	skillBinding, _ := binding["skill"].(map[string]any)
	entry := SkillAppManifestEntry{
		ID:                strings.TrimSpace(stringMapValue(app, "id")),
		SkillID:           skillName,
		Name:              strings.TrimSpace(stringMapValue(app, "name")),
		Description:       stringMapValue(app, "description"),
		Category:          firstNonEmptySkillAppString(stringMapValue(app, "category"), "Skill"),
		Kind:              firstNonEmptySkillAppString(stringMapValue(app, "kind"), "tool_app"),
		Icon:              normalizeSkillAppIconName(stringMapValue(app, "icon")),
		CustomIconDataURL: normalizeMaclawAppCustomIconDataURL(firstNonEmptySkillAppString(stringMapValue(app, "customIconDataUrl"), stringMapValue(app, "custom_icon_data_url"))),
		InputMode:         normalizeSkillAppInputMode(firstNonEmptySkillAppString(stringMapValue(skillBinding, "inputMode"), stringMapValue(skillBinding, "input_mode"))),
		MultipleFiles:     boolMapValue(skillBinding, "multipleFiles") || boolMapValue(skillBinding, "multiple_files"),
		OutputModes:       normalizeSkillAppOutputModes(firstNonEmptyStringSlice(stringSliceMapValue(skillBinding, "outputModes"), stringSliceMapValue(skillBinding, "output_modes"))),
		Fields:            normalizeSkillAppFields(skillAppFieldsFromAny(skillBinding["fields"])),
	}
	if governance, ok := app["governance"].(map[string]any); ok && len(governance) > 0 {
		entry.Governance = governance
	}
	if panel, ok := app["panel"].(map[string]any); ok && entry.CustomIconDataURL == "" {
		entry.CustomIconDataURL = normalizeMaclawAppCustomIconDataURL(firstNonEmptySkillAppString(stringMapValue(panel, "customIconDataUrl"), stringMapValue(panel, "custom_icon_data_url")))
	}
	if entry.ID == "" || entry.Name == "" {
		return SkillAppManifestEntry{}, fmt.Errorf("maclaw app definition app.id and app.name are required")
	}
	return entry, nil
}

func mergeMaclawAppGovernance(existing map[string]any, incoming map[string]any) map[string]any {
	if len(incoming) == 0 {
		return existing
	}
	if len(existing) == 0 {
		return incoming
	}
	merged := make(map[string]any, len(incoming)+1)
	for key, value := range incoming {
		merged[key] = value
	}
	if _, ok := merged["testEvidence"]; !ok {
		if evidence, ok := existing["testEvidence"]; ok {
			merged["testEvidence"] = evidence
		}
	}
	return merged
}

func firstNonEmptyStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func normalizeMaclawAppDefinitionForSkill(appJSON string, skillName string) (map[string]any, string, string, error) {
	if strings.TrimSpace(appJSON) == "" {
		return nil, "", "", fmt.Errorf("maclaw app definition is empty")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(appJSON), &doc); err != nil {
		return nil, "", "", fmt.Errorf("decode maclaw app definition: %w", err)
	}
	if stringMapValue(doc, "schema") != "maclaw.app.v1" {
		return nil, "", "", fmt.Errorf("maclaw app definition schema must be maclaw.app.v1")
	}
	if stringMapValue(doc, "privateMarker") != "x_maclaw_apps" {
		return nil, "", "", fmt.Errorf("maclaw app definition privateMarker must be x_maclaw_apps")
	}
	app, ok := doc["app"].(map[string]any)
	if !ok {
		return nil, "", "", fmt.Errorf("maclaw app definition app must be an object")
	}
	appID := strings.TrimSpace(stringMapValue(app, "id"))
	appName := strings.TrimSpace(stringMapValue(app, "name"))
	if appID == "" {
		return nil, "", "", fmt.Errorf("maclaw app definition app.id is required")
	}
	if appName == "" {
		return nil, "", "", fmt.Errorf("maclaw app definition app.name is required")
	}
	kind := normalizeMaclawAppKind(stringMapValue(app, "kind"))
	if kind == "" {
		kind = "tool_app"
	}
	if kind != "tool_app" && kind != "enterprise_approval_app" && kind != "enterprise_normal_app" {
		return nil, "", "", fmt.Errorf("maclaw app kind %q cannot be saved into a skill package", kind)
	}
	app["kind"] = kind
	binding, _ := app["binding"].(map[string]any)
	if binding == nil {
		binding = map[string]any{}
		app["binding"] = binding
	}
	skillBindingKey := "skill"
	if kind != "tool_app" {
		skillBindingKey = "appSkill"
	}
	skillBinding, _ := binding[skillBindingKey].(map[string]any)
	if skillBinding == nil {
		skillBinding = map[string]any{}
		binding[skillBindingKey] = skillBinding
	}
	boundSkill := strings.TrimSpace(stringMapValue(skillBinding, "id"))
	if boundSkill != "" && boundSkill != skillName {
		return nil, "", "", fmt.Errorf("maclaw app binding %s id %q does not match target skill %q", skillBindingKey, boundSkill, skillName)
	}
	skillBinding["id"] = skillName
	legacySkillBinding, _ := binding["skill"].(map[string]any)
	appDefinitionFile := firstNonEmptySkillAppString(
		stringMapValue(skillBinding, "appDefinitionFile"),
		stringMapValue(skillBinding, "app_definition_file"),
		stringMapValue(legacySkillBinding, "appDefinitionFile"),
		stringMapValue(legacySkillBinding, "app_definition_file"),
	)
	if appDefinitionFile != "" && appDefinitionFile != "maclaw.app.json" && appDefinitionFile != "maclaw.apps.json" {
		return nil, "", "", fmt.Errorf("maclaw app binding %s appDefinitionFile must be maclaw.app.json or maclaw.apps.json", skillBindingKey)
	}
	if kind != "tool_app" && appDefinitionFile == "maclaw.apps.json" {
		return nil, "", "", fmt.Errorf("enterprise MaClaw App definitions must be saved as maclaw.app.json")
	}
	if stringMapValue(app, "launchMode") == "" {
		if kind == "tool_app" {
			app["launchMode"] = "fixed_skill_ui"
		} else {
			app["launchMode"] = "agent_dynamic_ui"
		}
	}
	return doc, appID, appName, nil
}

func candidateMaclawAppDefinitionFiles(skillDir string) []string {
	seen := map[string]struct{}{}
	add := func(value string, out *[]string) {
		value = strings.TrimSpace(filepath.Clean(value))
		if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "..") || value == "maclaw.apps.json" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		*out = append(*out, value)
	}
	out := []string{}
	add("maclaw.app.json", &out)
	add(readMaclawAppEntryFromSkillYAML(skillDir), &out)
	return out
}

type maclawAppYAMLMetadata struct {
	Entry         string
	AddToAppPanel *bool
}

func readMaclawAppYAMLMetadata(skillDir string) maclawAppYAMLMetadata {
	for _, name := range []string{"skill.yaml", "skill.yml"} {
		data, err := os.ReadFile(filepath.Join(skillDir, name))
		if err != nil {
			continue
		}
		var doc map[string]interface{}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			continue
		}
		block, ok := doc["maclaw_app"].(map[string]interface{})
		if !ok {
			continue
		}
		entry := strings.TrimSpace(stringMapValue(block, "entry"))
		if entry == "" {
			entry = "maclaw.app.json"
		}
		entry = filepath.Clean(entry)
		if filepath.IsAbs(entry) || strings.HasPrefix(entry, "..") {
			entry = ""
		}
		return maclawAppYAMLMetadata{
			Entry:         entry,
			AddToAppPanel: optionalBoolMapValue(block, "add_to_app_panel"),
		}
	}
	return maclawAppYAMLMetadata{}
}

func readMaclawAppDefinitionAsSkillApp(path string, fallbackSkillID string) (SkillAppManifestEntry, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillAppManifestEntry{}, false
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return SkillAppManifestEntry{}, false
	}
	if stringMapValue(doc, "schema") != "maclaw.app.v1" || stringMapValue(doc, "privateMarker") != "x_maclaw_apps" {
		return SkillAppManifestEntry{}, false
	}
	app, ok := doc["app"].(map[string]interface{})
	if !ok {
		return SkillAppManifestEntry{}, false
	}
	kind := normalizeMaclawAppKind(stringMapValue(app, "kind"))
	if kind != "" && kind != "tool_app" && kind != "enterprise_approval_app" && kind != "enterprise_normal_app" {
		return SkillAppManifestEntry{}, false
	}
	if kind == "" {
		kind = "tool_app"
	}
	id := strings.TrimSpace(stringMapValue(app, "id"))
	name := strings.TrimSpace(stringMapValue(app, "name"))
	if id == "" || name == "" {
		return SkillAppManifestEntry{}, false
	}
	entry := SkillAppManifestEntry{
		ID:                id,
		Name:              name,
		Description:       stringMapValue(app, "description"),
		Category:          stringMapValue(app, "category"),
		Kind:              kind,
		Icon:              stringMapValue(app, "icon"),
		CustomIconDataURL: normalizeMaclawAppCustomIconDataURL(firstNonEmptySkillAppString(stringMapValue(app, "customIconDataUrl"), stringMapValue(app, "custom_icon_data_url"))),
		SkillID:           fallbackSkillID,
		AppDefinition:     doc,
	}
	if panel, ok := app["panel"].(map[string]interface{}); ok && entry.CustomIconDataURL == "" {
		entry.CustomIconDataURL = normalizeMaclawAppCustomIconDataURL(firstNonEmptySkillAppString(stringMapValue(panel, "customIconDataUrl"), stringMapValue(panel, "custom_icon_data_url")))
	}
	if binding, ok := app["binding"].(map[string]interface{}); ok {
		skillBinding, _ := binding["skill"].(map[string]interface{})
		if kind != "tool_app" {
			skillBinding, _ = binding["appSkill"].(map[string]interface{})
		}
		if skillBinding != nil {
			entry.InputMode = firstNonEmptySkillAppString(stringMapValue(skillBinding, "inputMode"), stringMapValue(skillBinding, "input_mode"))
			entry.MultipleFiles = boolMapValue(skillBinding, "multipleFiles") || boolMapValue(skillBinding, "multiple_files")
			entry.OutputModes = stringSliceMapValue(skillBinding, "outputModes")
			if len(entry.OutputModes) == 0 {
				entry.OutputModes = stringSliceMapValue(skillBinding, "output_modes")
			}
			entry.Fields = skillAppFieldsFromAny(skillBinding["fields"])
		}
	}
	return entry, true
}

func firstNonEmptySkillAppString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeMaclawAppCustomIconDataURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) >= 260000 {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:image/png;base64,") ||
		strings.HasPrefix(lower, "data:image/jpeg;base64,") ||
		strings.HasPrefix(lower, "data:image/webp;base64,") {
		return value
	}
	return ""
}

func boolMapValue(m map[string]interface{}, key string) bool {
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func optionalBoolMapValue(m map[string]interface{}, key string) *bool {
	value, ok := m[key]
	if !ok {
		return nil
	}
	switch v := value.(type) {
	case bool:
		return &v
	case string:
		parsed := strings.EqualFold(strings.TrimSpace(v), "true") || strings.EqualFold(strings.TrimSpace(v), "yes") || strings.TrimSpace(v) == "1"
		if strings.EqualFold(strings.TrimSpace(v), "false") || strings.EqualFold(strings.TrimSpace(v), "no") || strings.TrimSpace(v) == "0" {
			parsed = false
		}
		return &parsed
	default:
		return nil
	}
}

func stringSliceMapValue(m map[string]interface{}, key string) []string {
	items, ok := m[key].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func skillAppFieldsFromAny(value interface{}) []SkillAppManifestField {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	out := make([]SkillAppManifestField, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		field := SkillAppManifestField{
			Name:     stringMapValue(raw, "name"),
			Label:    stringMapValue(raw, "label"),
			Type:     stringMapValue(raw, "type"),
			Required: boolMapValue(raw, "required"),
			Default:  raw["default"],
		}
		if options, ok := raw["options"].([]interface{}); ok {
			field.Options = options
		}
		out = append(out, field)
	}
	return out
}

func normalizeSkillAppIconName(icon string) string {
	switch strings.TrimSpace(strings.ToLower(icon)) {
	case "receipt", "warehouse", "inventory", "customer", "contract", "pdf", "shield", "sheet", "web", "sync":
		return strings.TrimSpace(strings.ToLower(icon))
	default:
		return "contract"
	}
}

func normalizeSkillAppInputMode(inputMode string) string {
	switch strings.TrimSpace(strings.ToLower(inputMode)) {
	case "form", "mixed":
		return strings.TrimSpace(strings.ToLower(inputMode))
	default:
		return "file"
	}
}

func normalizeSkillAppOutputModes(outputModes []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(outputModes))
	for _, mode := range outputModes {
		mode = strings.TrimSpace(strings.ToLower(mode))
		switch mode {
		case "docx", "xlsx", "pdf", "json", "txt":
			if _, ok := seen[mode]; ok {
				continue
			}
			seen[mode] = struct{}{}
			out = append(out, mode)
		}
	}
	return out
}

func normalizeSkillAppFields(fields []SkillAppManifestField) []SkillAppManifestField {
	out := make([]SkillAppManifestField, 0, len(fields))
	for _, field := range fields {
		field.Name = strings.TrimSpace(field.Name)
		if field.Name == "" {
			continue
		}
		field.Label = strings.TrimSpace(field.Label)
		if field.Label == "" {
			field.Label = field.Name
		}
		switch strings.TrimSpace(strings.ToLower(field.Type)) {
		case "select":
			field.Type = "select"
		case "boolean":
			field.Type = "boolean"
		default:
			field.Type = "text"
		}
		if field.Type == "boolean" {
			field.Default = normalizeSkillAppBoolDefault(field.Default)
			field.Options = nil
		} else {
			field.Default = strings.TrimSpace(fmt.Sprint(field.Default))
			if field.Default == "<nil>" {
				field.Default = ""
			}
			field.Options = normalizeSkillAppFieldOptions(field.Options, field.Type, fmt.Sprint(field.Default))
		}
		out = append(out, field)
	}
	return out
}

func normalizeSkillAppBoolDefault(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func normalizeSkillAppFieldOptions(options []interface{}, fieldType string, defaultValue string) []interface{} {
	if fieldType != "select" {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]interface{}, 0, len(options)+1)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	add(defaultValue)
	for _, option := range options {
		add(fmt.Sprint(option))
	}
	return out
}

// DiagnoseSkillFiles scans the app data skills directory and reports load status for each
// subdirectory, including the reason if a skill failed to load (Wails binding).
func (a *App) DiagnoseSkillFiles() []SkillDiagEntry {
	skillsRoot, err := a.primarySkillsDir()
	if err != nil {
		return []SkillDiagEntry{{Dir: "~", Reason: "cannot resolve user skills directory: " + err.Error()}}
	}

	// Check if directory exists at all.
	if info, err := os.Stat(skillsRoot); err != nil {
		if os.IsNotExist(err) {
			return []SkillDiagEntry{{Dir: skillsRoot, Reason: "skills directory does not exist: " + skillsRoot}}
		}
		return []SkillDiagEntry{{Dir: skillsRoot, Reason: "cannot access skills directory: " + err.Error()}}
	} else if !info.IsDir() {
		return []SkillDiagEntry{{Dir: skillsRoot, Reason: skillsRoot + " is not a directory"}}
	}

	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return []SkillDiagEntry{{Dir: skillsRoot, Reason: "cannot read skills directory: " + err.Error()}}
	}
	if len(entries) == 0 {
		return []SkillDiagEntry{{Dir: skillsRoot, Reason: "skills directory is empty"}}
	}

	var result []SkillDiagEntry
	for _, entry := range entries {
		dirName := entry.Name()
		dirPath := filepath.Join(skillsRoot, dirName)
		if !entry.IsDir() {
			result = append(result, SkillDiagEntry{Dir: dirName, Reason: "not a directory, skipped"})
			continue
		}
		yamlPath := filepath.Join(dirPath, "skill.yaml")
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			// No skill.yaml; try SKILL.md / skill.md fallback (mirrors loadSkillFromDir logic).
			name, diagReason := diagTryMarkdownFallback(dirPath, dirName)
			if diagReason != "" {
				result = append(result, SkillDiagEntry{Dir: dirName, Reason: diagReason})
				continue
			}
			result = append(result, SkillDiagEntry{Dir: dirName, Name: name, OK: true})
			continue
		}
		var sf skill.SkillYAMLFile
		if err := yaml.Unmarshal(data, &sf); err != nil {
			result = append(result, SkillDiagEntry{Dir: dirName, Reason: "YAML parse failed: " + err.Error()})
			continue
		}
		name := strings.TrimSpace(sf.Name)
		if name == "" {
			name = dirName
		}
		result = append(result, SkillDiagEntry{Dir: dirName, Name: name, OK: true})
	}
	return result
}

// diagTryMarkdownFallback attempts to load a skill from SKILL.md / skill.md
// when skill.yaml is absent. Returns (name, "") on success or ("", reason) on
// failure. This mirrors the fallback chain in corelib/skill/scanner.go's
// loadSkillFromDir so that the diagnostic result matches actual load behavior.
func diagTryMarkdownFallback(dirPath, dirName string) (string, string) {
	// Try standard SKILL.md / skill.md import.
	parsed, err := skill.ImportMarkdownSkillDir(dirPath, skill.MarkdownSkillOptions{
		NameFallback: dirName,
		Source:       "file",
		SkillDir:     dirPath,
	})
	if err == nil && parsed != nil {
		name := strings.TrimSpace(parsed.Name)
		if name == "" {
			name = dirName
		}
		return name, ""
	}

	// Try Claude SKILL.md format (YAML frontmatter with allowed-tools).
	for _, candidate := range []string{"skill.md", "SKILL.md"} {
		mdPath := filepath.Join(dirPath, candidate)
		data, readErr := os.ReadFile(mdPath)
		if readErr != nil {
			continue
		}
		if skill.IsClaudeSKILLMD(data) {
			claudeEntry, claudeErr := skill.ParseClaudeSKILLMD(dirPath, data)
			if claudeErr == nil && claudeEntry != nil {
				name := strings.TrimSpace(claudeEntry.Name)
				if name == "" {
					name = dirName
				}
				return name, ""
			}
		}
		// Has a markdown file but couldn't parse it; still a valid skill.
		// candidate (craft_tool LLM fallback would handle it at runtime).
		return dirName, ""
	}

	// Check for KNOWLEDGE.md
	for _, candidate := range []string{"KNOWLEDGE.md", "knowledge.md"} {
		knPath := filepath.Join(dirPath, candidate)
		if _, statErr := os.Stat(knPath); statErr == nil {
			return dirName, ""
		}
	}

	return "", "missing skill.yaml or SKILL.md"
}

// ---------------------------------------------------------------------------
// External Skill Directories - Wails bindings
// ---------------------------------------------------------------------------

// ListExternalSkillDirs returns the user-configured external skill directories (Wails binding).
func (a *App) ListExternalSkillDirs() []string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.ExternalSkillDirs
}

// ExternalSkillDirInfo is the Wails-facing view of an external skill directory.
type ExternalSkillDirInfo struct {
	Path       string `json:"path"`
	SkillCount int    `json:"skill_count"`
	Error      string `json:"error,omitempty"`
}

// ListExternalSkillDirsDetailed returns external dirs with skill counts (Wails binding).
func (a *App) ListExternalSkillDirsDetailed() []ExternalSkillDirInfo {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil
	}
	var result []ExternalSkillDirInfo
	for _, dir := range cfg.ExternalSkillDirs {
		count, verr := skill.ValidateExternalSkillDir(dir)
		info := ExternalSkillDirInfo{Path: dir, SkillCount: count}
		if verr != nil {
			info.Error = verr.Error()
		}
		result = append(result, info)
	}
	return result
}

// AddExternalSkillDir validates and adds an external skill directory (Wails binding).
func (a *App) AddExternalSkillDir(dir string) (int, error) {
	dir = filepath.Clean(dir)
	// Reject built-in skill directories.
	for _, builtin := range skill.SkillScanRoots() {
		if filepath.Clean(builtin) == dir {
			return 0, fmt.Errorf("this is a built-in skill directory, no need to add")
		}
	}
	count, err := skill.ValidateExternalSkillDir(dir)
	if err != nil {
		return 0, err
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return 0, err
	}
	for _, d := range cfg.ExternalSkillDirs {
		if filepath.Clean(d) == dir {
			return 0, fmt.Errorf("directory already added")
		}
	}
	nextDirs := append(append([]string(nil), cfg.ExternalSkillDirs...), dir)
	return count, a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.ExternalSkillDirs = nextDirs
	})
}

// RemoveExternalSkillDir removes an external skill directory from config (Wails binding).
func (a *App) RemoveExternalSkillDir(dir string) error {
	dir = filepath.Clean(dir)
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	var filtered []string
	found := false
	for _, d := range cfg.ExternalSkillDirs {
		if filepath.Clean(d) == dir {
			found = true
			continue
		}
		filtered = append(filtered, d)
	}
	if !found {
		return fmt.Errorf("directory not found in config")
	}
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.ExternalSkillDirs = filtered
	})
}

// CreateNLSkill registers a new NL Skill definition (Wails binding).
func (a *App) CreateNLSkill(def corelib.NLSkillEntry) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "create", "name": def.Name}); err != nil {
		return err
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	report, err := a.scanAndAdmitSkillBeforeRegister(context.Background(), &def, "manual skill create")
	if err != nil {
		return err
	}
	if err := writeSkillScanCacheForInstalledEntry(&def, report); err != nil {
		return fmt.Errorf("write skill scan cache: %w", err)
	}
	if err := a.skillExecutor.Register(def); err != nil {
		return err
	}
	return nil
}

// UpdateNLSkill updates an existing NL Skill definition (Wails binding).
func (a *App) UpdateNLSkill(def corelib.NLSkillEntry) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "update", "name": def.Name}); err != nil {
		return err
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	report, err := a.scanAndAdmitSkillBeforeRegister(context.Background(), &def, "manual skill update")
	if err != nil {
		return err
	}
	if err := writeSkillScanCacheForInstalledEntry(&def, report); err != nil {
		return fmt.Errorf("write skill scan cache: %w", err)
	}
	if err := a.skillExecutor.Update(def); err != nil {
		return err
	}
	return nil
}

// SetNLSkillStatus changes only the status field of an existing NL Skill.
// It is used for explicit local review decisions such as re-enabling a Skill
// that was marked needs_review by self-repair or governance maintenance.
func (a *App) SetNLSkillStatus(name, status string) error {
	name = strings.TrimSpace(name)
	status = strings.TrimSpace(status)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	switch normalizeSkillEntryStatus(status) {
	case skillEntryStatusActive, skillEntryStatusDisabled, skillEntryStatusNeedsSetup, skillEntryStatusNeedsReview:
	default:
		return fmt.Errorf("unsupported skill status %q", status)
	}
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "status", "name": name, "status": status}); err != nil {
		return err
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	normalized := string(normalizeSkillEntryStatus(status))
	if err := a.skillExecutor.UpdateStatus(name, normalized); err != nil {
		return err
	}
	// Durable audit when operators park a skill for review (e.g. from repair draft UI).
	if normalizeSkillEntryStatus(status) == skillEntryStatusNeedsReview {
		skill.RecordEvolutionEvent("skill:mark_needs_review", map[string]string{
			"skill":       name,
			"explanation": "status set to needs_review by operator",
		}, "desktop")
	}
	return nil
}

// BatchSetNLSkillStatus applies the same status to multiple skills.
// Returns updated names and per-skill errors. Partial success is allowed.
func (a *App) BatchSetNLSkillStatus(names []string, status string) map[string]interface{} {
	out := map[string]interface{}{
		"ok":      false,
		"status":  strings.TrimSpace(status),
		"updated": []string{},
		"errors":  []string{},
		"count":   0,
	}
	status = strings.TrimSpace(status)
	if status == "" {
		out["error"] = "status is required"
		return out
	}
	seen := map[string]bool{}
	updated := make([]string, 0, len(names))
	errs := make([]string, 0)
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		if err := a.SetNLSkillStatus(name, status); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		updated = append(updated, name)
	}
	out["updated"] = updated
	out["errors"] = errs
	out["count"] = len(updated)
	out["ok"] = len(errs) == 0 && len(updated) > 0
	if len(updated) == 0 && len(errs) == 0 {
		out["error"] = "no skill names provided"
	}
	return out
}

// DeleteNLSkill removes an NL Skill by name (Wails binding).
func (a *App) DeleteNLSkill(name string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "delete", "name": name}); err != nil {
		return err
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	err := a.skillExecutor.Delete(name)
	if err == nil {
		if a.hubUpdCache != nil {
			a.hubUpdCache.invalidate()
		}
		// Refresh the tool router's skill index so that subsequent LLM
		// Route() calls no longer include the deleted skill in matches.
		if a.toolRouter != nil {
			a.toolRouter.RefreshSkillIndex()
		}
	}
	return err
}

// RenameNLSkill renames a learned/crafted skill to a user-friendly name (Wails binding).
// It updates the name field in skill.yaml, renames the on-disk directory,
// and updates the config.json entry.
func (a *App) RenameNLSkill(oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return fmt.Errorf("new name cannot be empty")
	}
	if newName == oldName {
		return nil // no-op
	}
	// Reject names containing path separators at the API boundary (user input).
	if strings.ContainsAny(newName, "/\\") {
		return fmt.Errorf("skill name cannot contain path separators")
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.Rename(oldName, newName)
}

// ImportNLSkillZip opens a file dialog to select a zip file, validates it as a
// file-backed skill package using skill.yaml, skill.yml, or skill.md, and imports it.
// Returns the imported skill name on success.
func (a *App) ImportNLSkillZip() (string, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "import", "source": "zip"}); err != nil {
		return "", err
	}
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return "", fmt.Errorf("skill executor not initialized")
	}

	selection := a.SelectSkillFile()
	if selection == "" {
		return "", nil // user cancelled
	}
	return a.importNLSkillZipPath(selection)
}

func (a *App) importNLSkillZipPath(selection string) (string, error) {
	snapshot, cleanup, err := snapshotSkillZipForInstall(selection)
	if err != nil {
		return "", err
	}
	defer cleanup()

	name, importErr := a.importFileBackedSkillZipPath(snapshot)
	if importErr == nil {
		return name, nil
	}
	if validationErr := a.validateSkillZip(snapshot); validationErr != nil && errors.Is(importErr, errNoRecognizableSkillDefinition) {
		return "", validationErr
	}
	return "", importErr
}

var errNoRecognizableSkillDefinition = errors.New("no recognizable skill definition")

func (a *App) importFileBackedSkillZipPath(selection string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "skill-import-*")
	if err != nil {
		return "", fmt.Errorf("create temporary skill import directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	if err := a.unzip(selection, tmpDir); err != nil {
		return "", fmt.Errorf("unzip skill package: %w", err)
	}

	packageRoots, err := resolveImportedSkillPackageRoots(tmpDir)
	if err != nil {
		return "", err
	}

	existingNames := make(map[string]bool)
	for _, existing := range a.skillExecutor.loadSkills() {
		existingNames[existing.Name] = true
	}

	entries := make([]*corelib.NLSkillEntry, 0, len(packageRoots))
	scanReports := make([]*skill.ScanReport, 0, len(packageRoots))
	for _, packageRoot := range packageRoots {
		entry, err := loadImportedSkillEntry(packageRoot)
		if err != nil {
			return "", err
		}
		if existingNames[entry.Name] {
			return "", fmt.Errorf("skill %q already exists", entry.Name)
		}
		existingNames[entry.Name] = true
		report, err := a.scanAndAdmitSkillBeforeRegister(context.Background(), entry, "manual skill pack")
		if err != nil {
			return "", err
		}
		entries = append(entries, entry)
		scanReports = append(scanReports, report)
	}

	primaryDir, err := a.primarySkillsDir()
	if err != nil {
		return "", fmt.Errorf("resolve primary skills directory: %w", err)
	}
	if err := os.MkdirAll(primaryDir, 0o755); err != nil {
		return "", fmt.Errorf("create primary skills directory: %w", err)
	}

	var installedDirs []string
	for i, packageRoot := range packageRoots {
		entry := entries[i]
		destDir := filepath.Join(primaryDir, entry.Name)
		if _, err := os.Stat(destDir); err == nil {
			cleanupImportedSkillDirs(installedDirs)
			return "", fmt.Errorf("skill %q already exists", entry.Name)
		} else if !os.IsNotExist(err) {
			cleanupImportedSkillDirs(installedDirs)
			return "", fmt.Errorf("check destination skill directory: %w", err)
		}
		if err := os.Rename(packageRoot, destDir); err != nil {
			if err := copySkillPackageRootAtomically(packageRoot, destDir, primaryDir); err != nil {
				cleanupImportedSkillDirs(installedDirs)
				return "", err
			}
			installedDirs = append(installedDirs, destDir)
		} else {
			installedDirs = append(installedDirs, destDir)
		}
		installedEntry := *entry
		installedEntry.SkillDir = destDir
		if err := writeSkillScanCacheForInstalledEntry(&installedEntry, scanReports[i]); err != nil {
			cleanupImportedSkillDirs(installedDirs)
			return "", fmt.Errorf("write skill scan cache: %w", err)
		}
	}

	a.emitSkillInstallProgress(entries[0].Name, "done", "Skill imported successfully.", scanReports[0])
	return entries[0].Name, nil
}

func cleanupImportedSkillDirs(paths []string) {
	for i := len(paths) - 1; i >= 0; i-- {
		if strings.TrimSpace(paths[i]) == "" {
			continue
		}
		_ = os.RemoveAll(paths[i])
	}
}

func copySkillPackageRootAtomically(packageRoot, destDir, primaryDir string) error {
	tmpDest, err := os.MkdirTemp(primaryDir, ".skill-import-*")
	if err != nil {
		return fmt.Errorf("create temporary skill import directory: %w", err)
	}
	cleanupTmpDest := true
	defer func() {
		if cleanupTmpDest {
			_ = os.RemoveAll(tmpDest)
		}
	}()
	if err := copyDirContents(packageRoot, tmpDest); err != nil {
		return fmt.Errorf("copy skill package into temporary import directory: %w", err)
	}
	if err := os.Rename(tmpDest, destDir); err != nil {
		return fmt.Errorf("install copied skill package: %w", err)
	}
	cleanupTmpDest = false
	return nil
}

func resolveImportedSkillPackageRoots(sandboxDir string) ([]string, error) {
	if importedSkillDefinitionExists(sandboxDir) {
		return []string{sandboxDir}, nil
	}
	entries, err := os.ReadDir(sandboxDir)
	if err != nil {
		return nil, fmt.Errorf("read extracted skill package directory: %w", err)
	}
	var roots []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == "__MACOSX" || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		candidate := filepath.Join(sandboxDir, e.Name())
		if importedSkillDefinitionExists(candidate) {
			roots = append(roots, candidate)
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("%w: zip package contains no recognizable Skill definition file (need skill.yaml/skill.yml or markdown docs such as skill.md/SKILL.md/README.md)", errNoRecognizableSkillDefinition)
	}
	return roots, nil
}

func importedSkillDefinitionExists(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	hasLegacySkillMD := false
	hasLegacyMeta := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		switch entry.Name() {
		case "skill.yaml", "skill.yml":
			return true
		case "SKILL.md":
			hasLegacySkillMD = true
		case "_meta.json":
			hasLegacyMeta = true
		default:
			if isSkillMarkdownDocFileName(entry.Name()) {
				return true
			}
		}
	}
	return hasLegacySkillMD && !hasLegacyMeta
}

func loadImportedSkillEntry(skillDir string) (*corelib.NLSkillEntry, error) {
	if defPath, defFormat := importedStructuredSkillDefinitionPath(skillDir); defPath != "" {
		data, err := os.ReadFile(defPath)
		if err != nil {
			return nil, fmt.Errorf("read skill definition file: %w", err)
		}
		sf, err := skill.ParseSkillDefinitionFile(data, defFormat)
		if err != nil {
			return nil, fmt.Errorf("invalid %s format: %w", filepath.Base(defPath), err)
		}
		name := strings.TrimSpace(sf.Name)
		if name == "" {
			name = filepath.Base(skillDir)
		}
		status := sf.Status
		if status == "" {
			status = "active"
		}
		steps := make([]corelib.NLSkillStep, 0, len(sf.Steps))
		for _, s := range sf.Steps {
			params := s.Params
			if params == nil {
				params = map[string]interface{}{}
			}
			if s.TimeoutSeconds > 0 {
				params["timeout"] = float64(s.TimeoutSeconds)
			}
			onError := s.OnError
			if onError == "" {
				if s.ContinueOnErr {
					onError = "continue"
				} else {
					onError = "stop"
				}
			}
			steps = append(steps, corelib.NLSkillStep{Action: s.Action, Params: params, OnError: onError, Name: s.Name, Condition: s.Condition, When: s.When, Label: s.Label, Capture: s.Capture})
		}
		if len(steps) == 0 {
			parsed, err := skill.ImportMarkdownSkillDir(skillDir, skill.MarkdownSkillOptions{
				NameFallback:        name,
				DescriptionFallback: sf.Description,
				Triggers:            sf.Triggers,
				Source:              "file",
				SkillDir:            skillDir,
			})
			if err == nil {
				applyImportedSkillDefinitionFields(parsed, sf, defPath)
				skill.NormalizeSkillForRunner(parsed)
				return parsed, nil
			}
		}
		entry := importedSkillEntryFromDefinition(sf, name, status, skillDir, steps, defPath)
		skill.NormalizeSkillForRunner(entry)
		return entry, nil
	}
	parsed, err := skill.ImportMarkdownSkillDir(skillDir, skill.MarkdownSkillOptions{
		NameFallback: filepath.Base(skillDir),
		Source:       "file",
		SkillDir:     skillDir,
	})
	if err != nil {
		return nil, fmt.Errorf("skill package has no importable markdown docs and no compatible skill.yaml/skill.yml: %v", err)
	}
	return parsed, nil
}

func loadMarketPackageSkillEntry(skillDir string, runtimeStats *corelib.NLSkillEntry) (*corelib.NLSkillEntry, error) {
	if defPath, defFormat := importedStructuredSkillDefinitionPath(skillDir); defPath != "" {
		data, err := os.ReadFile(defPath)
		if err != nil {
			return nil, fmt.Errorf("read skill definition file: %w", err)
		}
		sf, err := skill.ParseSkillDefinitionFile(data, defFormat)
		if err != nil {
			return nil, fmt.Errorf("invalid %s format: %w", filepath.Base(defPath), err)
		}
		name := strings.TrimSpace(sf.Name)
		if name == "" {
			name = filepath.Base(skillDir)
		}
		status := strings.TrimSpace(sf.Status)
		if status == "" {
			status = "active"
		}
		steps := make([]corelib.NLSkillStep, 0, len(sf.Steps))
		for _, s := range sf.Steps {
			steps = append(steps, skillYAMLStepToEntryStep(s))
		}
		entry := importedSkillEntryFromDefinition(sf, name, status, skillDir, steps, defPath)
		rewriteRoot := skillDir
		if runtimeStats != nil && strings.TrimSpace(runtimeStats.SkillDir) != "" {
			rewriteRoot = runtimeStats.SkillDir
		}
		entry = skill.PackageViewFromRuntimeEntry(entry, rewriteRoot)
		entry.SkillDir = skillDir
		mergeSkillPackagingRuntimeFields(entry, runtimeStats)
		return entry, nil
	}
	entry, err := loadImportedSkillEntry(skillDir)
	if err != nil {
		return nil, err
	}
	entry = skill.PackageViewFromRuntimeEntry(entry, skillDir)
	mergeSkillPackagingRuntimeFields(entry, runtimeStats)
	return entry, nil
}

func skillYAMLStepToEntryStep(s skill.SkillYAMLStep) corelib.NLSkillStep {
	params := cloneInterfaceMap(s.Params)
	if params == nil {
		params = map[string]interface{}{}
	}
	if s.TimeoutSeconds > 0 {
		params["timeout"] = float64(s.TimeoutSeconds)
	}
	onError := s.OnError
	if onError == "" {
		if s.ContinueOnErr {
			onError = "continue"
		} else {
			onError = "stop"
		}
	}
	step := corelib.NLSkillStep{
		Action:    s.Action,
		Params:    params,
		OnError:   onError,
		Name:      s.Name,
		Condition: s.Condition,
		When:      s.When,
		Label:     s.Label,
		Capture:   cloneStringMap(s.Capture),
	}
	if s.Poll != nil {
		step.Poll = &corelib.StepPollConfig{
			Interval:    s.Poll.Interval,
			MaxAttempts: s.Poll.MaxAttempts,
			UntilMatch:  s.Poll.UntilMatch,
			UntilStatus: s.Poll.UntilStatus,
		}
	}
	if s.Loop != nil {
		step.Loop = &corelib.StepLoopConfig{
			MaxIterations: s.Loop.MaxIterations,
			UntilStep:     s.Loop.UntilStep,
			UntilMatch:    s.Loop.UntilMatch,
			OnFailStep:    s.Loop.OnFailStep,
		}
	}
	return step
}

func importedSkillEntryFromDefinition(sf *skill.SkillYAMLFile, name, status, skillDir string, steps []corelib.NLSkillStep, defPath string) *corelib.NLSkillEntry {
	entry := &corelib.NLSkillEntry{
		Name:             name,
		Description:      sf.Description,
		Triggers:         sf.Triggers,
		Steps:            steps,
		Status:           status,
		Source:           "file",
		SkillDir:         skillDir,
		ProducesArtifact: true,
	}
	applyImportedSkillDefinitionFields(entry, sf, defPath)
	return entry
}

func applyImportedSkillDefinitionFields(entry *corelib.NLSkillEntry, sf *skill.SkillYAMLFile, defPath string) {
	if entry == nil || sf == nil {
		return
	}
	if sf.Description != "" {
		entry.Description = sf.Description
	}
	if len(sf.Triggers) > 0 {
		entry.Triggers = sf.Triggers
	}
	if len(sf.Platforms) > 0 {
		entry.Platforms = sf.Platforms
	}
	if sf.RequiresGUI {
		entry.RequiresGUI = true
	}
	entry.CreatedAt = importedSkillFileModTime(defPath)
	if sf.Type != "" {
		entry.Type = sf.Type
	}
	if sf.Content != "" {
		entry.Content = sf.Content
	}
	if sf.Mode != "" {
		entry.Mode = sf.Mode
	}
	if sf.ExecMode != "" {
		entry.ExecMode = sf.ExecMode
	}
	if sf.GlobalTimeout > 0 {
		entry.GlobalTimeout = sf.GlobalTimeout
	}
	if sf.ProducesArtifact != nil {
		entry.ProducesArtifact = *sf.ProducesArtifact
	}
	if len(sf.Operations) > 0 {
		entry.Operations = importedSkillOperations(sf.Operations)
	}
	if len(sf.Params) > 0 {
		entry.Params = importedSkillParams(sf.Params)
	}
	if len(sf.RequiredArgs) > 0 {
		entry.RequiredArgs = sf.RequiredArgs
	}
	if len(sf.RequiredEnv) > 0 {
		entry.RequiredEnv = sf.RequiredEnv
	}
	if sf.PreferredShell != "" {
		entry.PreferredShell = sf.PreferredShell
	}
	if len(sf.Capabilities) > 0 {
		entry.Capabilities = sf.Capabilities
	}
	if len(sf.RequiresTools) > 0 {
		entry.RequiresTools = sf.RequiresTools
	}
	if len(sf.FallbackForTools) > 0 {
		entry.FallbackForTools = sf.FallbackForTools
	}
	if len(sf.RequiresToolsets) > 0 {
		entry.RequiresToolsets = sf.RequiresToolsets
	}
	if len(sf.FallbackForToolsets) > 0 {
		entry.FallbackForToolsets = sf.FallbackForToolsets
	}
	if len(sf.RequiredCredentialFiles) > 0 {
		entry.RequiredCredentialFiles = sf.RequiredCredentialFiles
	}
	if reqs := importedRequiresPython(sf.Requires); len(reqs) > 0 {
		entry.RequiresPython = reqs
	}
	if reqs := importedRequiresNode(sf.Requires); len(reqs) > 0 {
		entry.RequiresNode = reqs
	}
	if reqs := importedRequiresBins(sf.Requires); len(reqs) > 0 {
		entry.RequiresBins = reqs
	}
	if sf.Stateful {
		entry.Stateful = true
	}
	if len(sf.Pipeline) > 0 {
		entry.Pipeline = importedSkillPipeline(sf.Pipeline)
	}
}

func importedSkillFileModTime(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return time.Now().Format(time.RFC3339)
	}
	return info.ModTime().Format(time.RFC3339)
}

func importedSkillOperations(yamlOps []skill.SkillYAMLOperation) []corelib.NLSkillOperation {
	if len(yamlOps) == 0 {
		return nil
	}
	operations := make([]corelib.NLSkillOperation, 0, len(yamlOps))
	for _, op := range yamlOps {
		operations = append(operations, corelib.NLSkillOperation{Name: op.Name, Description: op.Description, Params: op.Params, Labels: op.Labels})
	}
	return operations
}

func importedSkillParams(yamlParams []skill.SkillYAMLParam) []corelib.NLSkillParam {
	if len(yamlParams) == 0 {
		return nil
	}
	params := make([]corelib.NLSkillParam, 0, len(yamlParams))
	for _, p := range yamlParams {
		params = append(params, corelib.NLSkillParam{Name: p.Name, Description: p.Description, Type: p.Type, Aliases: p.Aliases, CLIFlag: p.CLIFlag, Default: p.Default, Required: p.Required})
	}
	return params
}

func importedRequiresPython(req *skill.SkillYAMLRequires) []string {
	if req == nil {
		return nil
	}
	return req.Python
}

func importedRequiresNode(req *skill.SkillYAMLRequires) []string {
	if req == nil {
		return nil
	}
	return req.Node
}

func importedRequiresBins(req *skill.SkillYAMLRequires) []string {
	if req == nil {
		return nil
	}
	return req.Bins
}

func importedSkillPipeline(yamlSteps []skill.SkillYAMLPipelineStep) []corelib.SkillPipelineStep {
	if len(yamlSteps) == 0 {
		return nil
	}
	steps := make([]corelib.SkillPipelineStep, 0, len(yamlSteps))
	for _, s := range yamlSteps {
		steps = append(steps, corelib.SkillPipelineStep{Skill: s.Skill, Params: s.Params, Checkpoint: s.Checkpoint, CheckpointMessage: s.CheckpointMessage, ContinueOnFail: s.ContinueOnFail, TimeImpactOnReject: s.TimeImpactOnReject})
	}
	return steps
}

func importedStructuredSkillDefinitionPath(skillDir string) (string, string) {
	for _, candidate := range []struct {
		name   string
		format string
	}{
		{name: "skill.yaml", format: "yaml"},
		{name: "skill.yml", format: "yaml"},
	} {
		defPath := filepath.Join(skillDir, candidate.name)
		if _, err := os.Stat(defPath); err == nil {
			return defPath, candidate.format
		}
	}
	return "", ""
}

// CleanupStaleSkills disables learned/crafted Skills that have been unused
// for over 30 days and have a success rate below 50% (or were never used).
// Returns the names of disabled Skills.
func (e *SkillExecutor) CleanupStaleSkills() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	var disabled []string

	for i, s := range skills {
		if normalizeSkillEntryStatus(s.Status) != skillEntryStatusActive {
			continue
		}
		// Only auto-cleanup learned/crafted skills; manual, hub, and auto-installed skills are user-managed.
		if !corelib.IsLearnedSource(s.Source) {
			continue
		}
		// Never used and older than 30 days.
		if s.UsageCount == 0 {
			created, err := time.Parse(time.RFC3339, s.CreatedAt)
			if err == nil && created.Before(cutoff) {
				skills[i].Status = "disabled"
				disabled = append(disabled, s.Name)
			}
			continue
		}
		// Used but low success rate and not recently used.
		successRate := float64(s.SuccessCount) / float64(s.UsageCount)
		if successRate < 0.5 {
			lastUsed, err := time.Parse(time.RFC3339, s.LastUsedAt)
			if err == nil && lastUsed.Before(cutoff) {
				skills[i].Status = "disabled"
				disabled = append(disabled, s.Name)
			}
		}
	}

	if len(disabled) > 0 {
		_ = e.saveSkills(skills)
	}
	return disabled
}

// CleanupStaleNLSkills disables stale learned/crafted Skills (Wails binding).
func (a *App) CleanupStaleNLSkills() []string {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil
	}
	return a.skillExecutor.CleanupStaleSkills()
}

// Skill Runner Wails bindings.

// RunNLSkillAsync starts a skill run asynchronously for Wails.
func (a *App) RunNLSkillAsync(skillName string, runArgs map[string]interface{}) (string, error) {
	// Log incoming args for debugging App → Skill parameter passing
	argKeys := make([]string, 0, len(runArgs))
	for k := range runArgs {
		argKeys = append(argKeys, k)
	}
	log.Printf("[skill-app] RunNLSkillAsync skill=%q arg_keys=%v arg_count=%d", skillName, argKeys, len(runArgs))

	runArgs = markMaclawAppSkillRunArgs(withSkillAppInputFileAliases(runArgs))
	a.ensureSkillRunner()
	if a.skillRunner == nil {
		if err := a.cleanupStagedSkillAppInputFilesFromRunArgs(runArgs); err != nil {
			log.Printf("[skill-app] cleanup staged input after runner init failure: %v", err)
		}
		return "", fmt.Errorf("skill runner not initialized")
	}
	runID, err := a.skillRunner.StartRunForOwner(a.skillRunPolicyOwnerID(runArgs), skillName, runArgs)
	if err != nil && strings.Contains(err.Error(), "not found") {
		// Auto-install fallback: when an app-initiated skill run fails because
		// the dependency skill was never installed, attempt to install it from
		// the hub/skillmarket before giving up.
		if installed := a.tryAutoInstallAppDependencySkill(skillName); installed {
			log.Printf("[skill-app] RunNLSkillAsync auto-installed dependency %q, retrying", skillName)
			runID, err = a.skillRunner.StartRunForOwner(a.skillRunPolicyOwnerID(runArgs), skillName, runArgs)
		} else {
			// Replace the cryptic "not found" error with an actionable message.
			err = fmt.Errorf("应用依赖的 Skill「%s」未安装且自动安装失败。请在应用详情页点击「安装依赖」或检查网络连接后重试", skillName)
		}
	}
	if err != nil {
		log.Printf("[skill-app] RunNLSkillAsync FAILED skill=%q err=%v", skillName, err)
		// Detailed diagnostic for AI tools: log the full runArgs values (not just keys)
		// so the exact parameter mismatch can be identified from the log file.
		log.Printf("[skill-app-diag] === RUN FAILURE DIAGNOSTIC ===")
		log.Printf("[skill-app-diag] skill=%q", skillName)
		for k, v := range runArgs {
			switch val := v.(type) {
			case string:
				if len(val) > 200 {
					log.Printf("[skill-app-diag] arg %s = %q (len=%d, truncated)", k, val[:200], len(val))
				} else {
					log.Printf("[skill-app-diag] arg %s = %q", k, val)
				}
			case nil:
				log.Printf("[skill-app-diag] arg %s = nil", k)
			case bool:
				log.Printf("[skill-app-diag] arg %s = %v", k, val)
			default:
				s := fmt.Sprintf("%v", v)
				if len(s) > 300 {
					log.Printf("[skill-app-diag] arg %s = (%T) %s...(len=%d)", k, v, s[:300], len(s))
				} else {
					log.Printf("[skill-app-diag] arg %s = (%T) %s", k, v, s)
				}
			}
		}
		log.Printf("[skill-app-diag] === END DIAGNOSTIC ===")
		if cleanupErr := a.cleanupStagedSkillAppInputFilesFromRunArgs(runArgs); cleanupErr != nil {
			log.Printf("[skill-app] cleanup staged input after start failure: %v", cleanupErr)
		}
		return "", err
	}
	log.Printf("[skill-app] RunNLSkillAsync OK skill=%q run_id=%s", skillName, runID)
	return runID, nil
}

// tryAutoInstallAppDependencySkill attempts to install a missing skill dependency
// that an app requires. It uses a three-layer fallback strategy:
//
//  1. Bundled recovery: restore from bundled_dependencies stored in the app's
//     install record. This works offline, for private skills, and when remote
//     sources no longer host the skill. This is the PRIMARY recovery mechanism.
//  2. Remote install: download from Enterprise Hub → SkillMarket → SkillHub.
//  3. If all layers fail, returns false.
//
// The bundled-first approach ensures that an installed MaClaw App can always
// recover its runtime dependencies regardless of network availability or remote
// source state — the app package is self-contained.
func (a *App) tryAutoInstallAppDependencySkill(skillName string) bool {
	if a == nil {
		return false
	}

	// Layer 1: Try to restore from bundled_dependencies in install records.
	// This is the most reliable path — the skill files were captured at publish
	// time and stored in the local install registry. No network needed.
	if a.tryRestoreSkillFromInstallRecordBundle(skillName) {
		log.Printf("[skill-app] auto-install: restored %q from bundled dependencies in install record", skillName)
		if a.skillExecutor != nil {
			a.skillExecutor.invalidateSkillCache()
		}
		return true
	}

	// Layer 2: Try remote sources.
	// Build a synthetic dependency entry to leverage the existing alias resolution.
	// Use empty source to match the broadest set of alias registry entries.
	dep := maclawAppInstallPlanDependency{
		ID:   skillName,
		Kind: "runtime_skill",
	}
	resolved, ok := maclawAppImplicitHubSkillResolution(dep)
	if !ok {
		log.Printf("[skill-app] auto-install: no alias resolution for %q and no bundled fallback available", skillName)
		return false
	}
	target := resolved.Target
	if target == "" {
		target = skillName
	}
	log.Printf("[skill-app] auto-install: attempting remote install %q (resolved target=%q)", skillName, target)

	// Try sources in priority order: enterprise hub → skillmarket → hub.
	// Enterprise hub is tried first because enterprise-only policies reject
	// other sources. Skip sources that are clearly unavailable.
	type installAttempt struct {
		source string
		label  string
	}
	attempts := []installAttempt{
		{"skillmarket", "SkillMarket"},
		{"hub", "SkillHub"},
	}
	// Prepend enterprise hub if configured AND connected.
	// Skip if hub is not connected to avoid 45s timeout on unreachable enterprise hub.
	if cfg, err := a.LoadConfig(); err == nil && strings.TrimSpace(cfg.RemoteHubURL) != "" {
		if hc := a.hubClient(); hc != nil && hc.IsConnected() {
			attempts = append([]installAttempt{{"enterprise_hub", "Enterprise Hub"}}, attempts...)
		}
	}

	for _, attempt := range attempts {
		if err := a.InstallMixedSkill(attempt.source, target, target); err != nil {
			log.Printf("[skill-app] auto-install: %s install failed for %q: %v", attempt.label, target, err)
			continue
		}
		log.Printf("[skill-app] auto-install: successfully installed %q from %s", target, attempt.label)
		// Invalidate skill cache so the next loadSkills() picks up the new skill.
		if a.skillExecutor != nil {
			a.skillExecutor.invalidateSkillCache()
		}
		return true
	}
	return false
}

// tryRestoreSkillFromInstallRecordBundle searches all install records for a
// bundled_dependencies entry matching the given skill name, and if found,
// extracts and installs the bundled skill files locally.
//
// This is the root-cause fix for the scenario where a MaClaw App is installed
// (with bundled dependencies) but the dependency skill is later deleted or
// corrupted. Without this, the app would fail at runtime with "Skill not found"
// even though the skill files are available in the install record.
func (a *App) tryRestoreSkillFromInstallRecordBundle(skillName string) bool {
	if a == nil {
		return false
	}
	// Build a synthetic dependency to leverage alias resolution — the skill
	// name used at runtime (e.g., "paper_pdf_translator") may differ from
	// the canonical name stored in the bundle (e.g., a hub_skill_id).
	syntheticDep := maclawAppInstallPlanDependency{
		ID:   skillName,
		Kind: "runtime_skill",
	}
	candidate := a.findBundledSkillInInstallRecords(syntheticDep)
	if candidate == nil {
		return false
	}
	log.Printf("[skill-app] auto-install: found bundled skill %q in install records", skillName)
	if err := a.installBundledMaclawAppSkill(*candidate); err != nil {
		log.Printf("[skill-app] auto-install: bundled restore failed for %q: %v", skillName, err)
		return false
	}
	return true
}

func markMaclawAppSkillRunArgs(runArgs map[string]interface{}) map[string]interface{} {
	if runArgs == nil {
		runArgs = map[string]interface{}{}
	}
	runArgs["_maclaw_app"] = true
	return runArgs
}

// GetNLSkillRunStatus returns the status of an async skill run for Wails.
func (a *App) GetNLSkillRunStatus(runID string) (*SkillRunStatus, error) {
	a.ensureSkillRunner()
	if a.skillRunner == nil {
		return nil, fmt.Errorf("skill runner not initialized")
	}
	status, err := a.skillRunner.GetRunStatus(runID)
	if err == nil {
		a.registerSkillRunArtifacts(status)
	}
	return status, err
}

// CancelNLSkillRun cancels an async skill run for Wails.
func (a *App) CancelNLSkillRun(runID string) error {
	a.ensureSkillRunner()
	if a.skillRunner == nil {
		return fmt.Errorf("skill runner not initialized")
	}
	return a.skillRunner.CancelRun(runID)
}

// UploadNLSkillToMarket manually packages and uploads a skill to SkillMarket.
func (a *App) UploadNLSkillToMarket(skillName string) (string, error) {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "upload", "name": skillName, "source": "skillmarket"}); err != nil {
		return "", err
	}
	a.ensureSkillLifecycleManager()
	if a.skillLifecycle == nil {
		return "", fmt.Errorf("skill lifecycle manager not initialized")
	}
	return a.skillLifecycle.UploadNow(context.Background(), skillName, "manual_upload", true)

}

// AuditInstalledSkillQuality normalizes and scores installed skills, writing quality_status.json for file-backed skills.
func (a *App) AuditInstalledSkillQuality(requireRuntimeProof bool) ([]SkillQualityStatus, error) {
	a.ensureSkillLifecycleManager()
	if a.skillLifecycle == nil {
		return nil, fmt.Errorf("skill lifecycle manager not initialized")
	}
	return a.skillLifecycle.EvaluateInstalledSkills(requireRuntimeProof)
}

// ListSkillUploadQueue returns the persisted SkillMarket upload queue.
func (a *App) ListSkillUploadQueue() ([]SkillUploadQueueItem, error) {
	a.ensureSkillLifecycleManager()
	if a.skillLifecycle == nil {
		return nil, fmt.Errorf("skill lifecycle manager not initialized")
	}
	return a.skillLifecycle.ListUploadQueue()
}

// RetryBlockedSkillUpload moves blocked upload items back to pending after a skill is repaired or verified.
func (a *App) RetryBlockedSkillUpload(skillName string) error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "upload", "name": skillName, "retry": true, "source": "skillmarket"}); err != nil {
		return err
	}
	a.ensureSkillLifecycleManager()
	if a.skillLifecycle == nil {
		return fmt.Errorf("skill lifecycle manager not initialized")
	}
	if err := a.skillLifecycle.RetryBlocked(skillName); err != nil {
		return err
	}
	return a.skillLifecycle.ProcessPendingUploads(context.Background(), 0)
}

// RetrySkillUploadQueue asks the lifecycle manager to process pending uploads now.
func (a *App) RetrySkillUploadQueue() error {
	if err := a.ensureWorkflowAllowsRemoteToolCall("manage_skill", map[string]interface{}{"action": "upload", "retry_queue": true, "source": "skillmarket"}); err != nil {
		return err
	}
	a.ensureSkillLifecycleManager()
	if a.skillLifecycle == nil {
		return fmt.Errorf("skill lifecycle manager not initialized")
	}
	return a.skillLifecycle.ProcessPendingUploads(context.Background(), 0)
}

// packageSkillForMarket packages a skill as a SkillMarket-compatible zip file.
func (a *App) packageSkillForMarket(skillName string) (string, error) {
	zipPath, tmpDir, err := a.packageSkillForMarketWithDir(skillName)
	if err != nil {
		return "", err
	}
	os.RemoveAll(tmpDir)
	return zipPath, nil
}

// packageSkillForMarketWithDir packages a skill into a zip file and also returns
// the temporary directory containing the packaged copy. The caller is responsible
// for cleaning up both the zip file and the tmpDir.
func (a *App) packageSkillForMarketWithDir(skillName string) (string, string, error) {
	return a.packageSkillForMarketWithDirOptions(skillName, false)
}

func (a *App) packageSkillForMarketWithDirForOutbound(skillName string) (string, string, error) {
	return a.packageSkillForMarketWithDirOptions(skillName, true)
}

func (a *App) packageSkillForMarketWithDirOptions(skillName string, strictOutbound bool) (string, string, error) {
	a.skillExecutor.mu.RLock()
	var target *corelib.NLSkillEntry
	for _, s := range a.skillExecutor.loadSkills() {
		if s.Name == skillName {
			cp := s
			target = &cp
			break
		}
	}
	a.skillExecutor.mu.RUnlock()

	if target == nil {
		return "", "", fmt.Errorf("skill %q not found", skillName)
	}

	if len(target.Platforms) == 0 {
		target.Platforms = []string{"universal"}
	}

	tmpDir, err := os.MkdirTemp("", "skill-package-*")
	if err != nil {
		return "", "", err
	}

	// Copy the source skill directory into a temporary package workspace.
	if target.SkillDir != "" {
		if err := copyDirContents(target.SkillDir, tmpDir); err != nil {
			os.RemoveAll(tmpDir)
			return "", "", fmt.Errorf("copy skill directory: %w", err)
		}
	}

	// Reload the copied package and convert runtime-local paths to package refs.
	if target.SkillDir != "" {
		packagedEntry, loadErr := loadMarketPackageSkillEntry(tmpDir, target)
		if loadErr != nil {
			os.RemoveAll(tmpDir)
			return "", "", fmt.Errorf("load packaged skill definition: %w", loadErr)
		}
		target = packagedEntry
	}

	target.SkillDir = ""
	target.LastError = ""

	if err := writePackageViewSkillYAML(tmpDir, target); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", err
	}
	if appEntry := firstExistingMaclawAppDefinitionFile(tmpDir); appEntry != "" {
		if _, err := ensureMaclawAppEntryInSkillYAML(tmpDir, appEntry); err != nil {
			os.RemoveAll(tmpDir)
			return "", "", fmt.Errorf("write maclaw app metadata: %w", err)
		}
	}

	_, report, err := prepareSkillDirForMarket(tmpDir, true, a)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("prepare skill package for market: %w", err)
	}
	quality := evaluateSkillQualityForDir(target, report, false, tmpDir)
	writeSkillQualityStatus(tmpDir, target, quality, "package", false)
	if err := writeSkillPackageManifest(tmpDir, target, quality, "package", false); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("write skill package manifest: %w", err)
	}
	if !quality.MarketReady {
		os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("skill quality gate blocked upload: score=%d reasons=%s", quality.Score, strings.Join(quality.Reasons, "; "))
	}
	if strictOutbound {
		if err := scanSkillDirForOutboundPackage(tmpDir, a); err != nil {
			os.RemoveAll(tmpDir)
			return "", "", err
		}
	}

	zipPath := filepath.Join(a.GetTempDir(), fmt.Sprintf("skill-%s-%d.zip", toKebabCase(skillName), time.Now().UnixMilli()))
	if err := zipDirectory(tmpDir, zipPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", err
	}
	return zipPath, tmpDir, nil
}

func firstExistingMaclawAppDefinitionFile(skillDir string) string {
	for _, entry := range candidateMaclawAppDefinitionFiles(skillDir) {
		if hasValidMaclawAppFile(filepath.Join(skillDir, entry)) {
			return entry
		}
	}
	return ""
}

func writePackageViewSkillYAML(dir string, entry *corelib.NLSkillEntry) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("skill package directory is empty")
	}
	skillYAML := buildSkillYAMLFileFromPackageEntry(entry)
	yamlData, err := skill.FormatSkillYAMLFile(skillYAML)
	if err != nil {
		return fmt.Errorf("generate skill.yaml failed: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "skill.yaml"), yamlData, 0o644)
}

func buildSkillYAMLFileFromPackageEntry(entry *corelib.NLSkillEntry) *skill.SkillYAMLFile {
	if entry == nil {
		return &skill.SkillYAMLFile{Status: "active"}
	}
	producesArtifact := entry.ProducesArtifact
	sf := &skill.SkillYAMLFile{
		Name:                    entry.Name,
		Description:             entry.Description,
		Triggers:                append([]string(nil), entry.Triggers...),
		Status:                  entry.Status,
		Platforms:               append([]string(nil), entry.Platforms...),
		RequiresGUI:             entry.RequiresGUI,
		ProducesArtifact:        &producesArtifact,
		Mode:                    entry.Mode,
		ExecMode:                entry.ExecMode,
		GlobalTimeout:           entry.GlobalTimeout,
		RequiredArgs:            append([]string(nil), entry.RequiredArgs...),
		RequiredEnv:             append([]string(nil), entry.RequiredEnv...),
		PreferredShell:          entry.PreferredShell,
		Type:                    entry.Type,
		Content:                 entry.Content,
		Capabilities:            append([]string(nil), entry.Capabilities...),
		RequiresTools:           append([]string(nil), entry.RequiresTools...),
		FallbackForTools:        append([]string(nil), entry.FallbackForTools...),
		RequiresToolsets:        append([]string(nil), entry.RequiresToolsets...),
		FallbackForToolsets:     append([]string(nil), entry.FallbackForToolsets...),
		RequiredCredentialFiles: append([]string(nil), entry.RequiredCredentialFiles...),
		Stateful:                entry.Stateful,
	}
	if sf.Status == "" {
		sf.Status = "active"
	}
	if len(sf.Platforms) == 0 {
		sf.Platforms = []string{"universal"}
	}
	if len(entry.RequiresPython) > 0 || len(entry.RequiresNode) > 0 || len(entry.RequiresBins) > 0 {
		sf.Requires = &skill.SkillYAMLRequires{
			Python: append([]string(nil), entry.RequiresPython...),
			Node:   append([]string(nil), entry.RequiresNode...),
			Bins:   append([]string(nil), entry.RequiresBins...),
		}
	}
	sf.Operations = skillYAMLOperationsFromEntry(entry.Operations)
	sf.Params = skillYAMLParamsFromEntry(entry.Params)
	sf.Steps = skillYAMLStepsFromEntry(entry.Steps)
	sf.Pipeline = skillYAMLPipelineFromEntry(entry.Pipeline)
	return sf
}

func skillYAMLOperationsFromEntry(ops []corelib.NLSkillOperation) []skill.SkillYAMLOperation {
	if len(ops) == 0 {
		return nil
	}
	out := make([]skill.SkillYAMLOperation, 0, len(ops))
	for _, op := range ops {
		out = append(out, skill.SkillYAMLOperation{
			Name:        op.Name,
			Description: op.Description,
			Params:      append([]string(nil), op.Params...),
			Labels:      append([]string(nil), op.Labels...),
		})
	}
	return out
}

func skillYAMLParamsFromEntry(params []corelib.NLSkillParam) []skill.SkillYAMLParam {
	if len(params) == 0 {
		return nil
	}
	out := make([]skill.SkillYAMLParam, 0, len(params))
	for _, param := range params {
		out = append(out, skill.SkillYAMLParam{
			Name:        param.Name,
			Description: param.Description,
			Type:        param.Type,
			Aliases:     append([]string(nil), param.Aliases...),
			CLIFlag:     param.CLIFlag,
			Default:     param.Default,
			Required:    param.Required,
		})
	}
	return out
}

func skillYAMLStepsFromEntry(steps []corelib.NLSkillStep) []skill.SkillYAMLStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]skill.SkillYAMLStep, 0, len(steps))
	for _, step := range steps {
		yamlStep := skill.SkillYAMLStep{
			Action:    step.Action,
			Params:    cloneInterfaceMap(step.Params),
			OnError:   step.OnError,
			Name:      step.Name,
			Condition: step.Condition,
			When:      step.When,
			Label:     step.Label,
			Capture:   cloneStringMap(step.Capture),
		}
		if step.Poll != nil {
			yamlStep.Poll = &skill.SkillYAMLStepPoll{
				Interval:    step.Poll.Interval,
				MaxAttempts: step.Poll.MaxAttempts,
				UntilMatch:  step.Poll.UntilMatch,
				UntilStatus: step.Poll.UntilStatus,
			}
		}
		if step.Loop != nil {
			yamlStep.Loop = &skill.SkillYAMLStepLoop{
				MaxIterations: step.Loop.MaxIterations,
				UntilStep:     step.Loop.UntilStep,
				UntilMatch:    step.Loop.UntilMatch,
				OnFailStep:    step.Loop.OnFailStep,
			}
		}
		out = append(out, yamlStep)
	}
	return out
}

func skillYAMLPipelineFromEntry(steps []corelib.SkillPipelineStep) []skill.SkillYAMLPipelineStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]skill.SkillYAMLPipelineStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, skill.SkillYAMLPipelineStep{
			Skill:              step.Skill,
			Params:             cloneStringMap(step.Params),
			Checkpoint:         step.Checkpoint,
			CheckpointMessage:  step.CheckpointMessage,
			ContinueOnFail:     step.ContinueOnFail,
			TimeImpactOnReject: step.TimeImpactOnReject,
		})
	}
	return out
}

func mergeSkillPackagingRuntimeFields(dst, src *corelib.NLSkillEntry) {
	if dst == nil || src == nil {
		return
	}
	declSrc := src
	if strings.TrimSpace(src.SkillDir) != "" {
		declSrc = skill.PackageViewFromRuntimeEntry(src, src.SkillDir)
	}
	dst.UsageCount = src.UsageCount
	dst.SuccessCount = src.SuccessCount
	dst.FailureCount = src.FailureCount
	dst.WorkaroundCount = src.WorkaroundCount
	dst.LastUsedAt = src.LastUsedAt
	dst.RepairAttemptCount = src.RepairAttemptCount
	dst.LastRepairAt = src.LastRepairAt
	dst.RepairHistory = append([]corelib.SkillRepairRecord(nil), src.RepairHistory...)
	if declSrc.RequiresGUI {
		dst.RequiresGUI = true
	}
	if len(dst.RequiredEnv) == 0 {
		dst.RequiredEnv = cloneStringSlice(declSrc.RequiredEnv)
	}
	if len(dst.RequiredCredentialFiles) == 0 {
		dst.RequiredCredentialFiles = cloneStringSlice(declSrc.RequiredCredentialFiles)
	}
	if len(dst.RequiresTools) == 0 {
		dst.RequiresTools = cloneStringSlice(declSrc.RequiresTools)
	}
	if len(dst.RequiresToolsets) == 0 {
		dst.RequiresToolsets = cloneStringSlice(declSrc.RequiresToolsets)
	}
	if len(dst.Capabilities) == 0 {
		dst.Capabilities = cloneStringSlice(declSrc.Capabilities)
	}
}

// executeBashStep runs a shell command as a skill step.
// Keep this synchronous path on the same shell/runtime mechanics as SkillRunner.
func executeBashStep(command string, params map[string]interface{}, app *App) (string, error) {
	return runBashStepWithContextFull(context.Background(), command, params, "", app, 0)
}

// File system helpers.

// copyDirContents copies src contents into dst without following links.
func copyDirContents(src, dst string) error {
	return copyDirContentsWithOptions(src, dst, false)
}

func copyDirContentsForSkillRun(src, dst string) error {
	return copyDirContentsWithOptions(src, dst, true)
}

func copyDirContentsWithOptions(src, dst string, includeRuntimeArtifacts bool) error {
	return copyDirContentsRecursive(src, dst, includeRuntimeArtifacts, nil)
}

// copyDirContentsRecursive is the internal recursive implementation.
// visitedDirs tracks real paths of directories we've already copied to
// detect circular symlinks (e.g., pnpm's node_modules structure).
func copyDirContentsRecursive(src, dst string, includeRuntimeArtifacts bool, visitedDirs map[string]bool) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	srcAbs = filepath.Clean(srcAbs)
	dstAbs = filepath.Clean(dstAbs)

	// Cycle detection: prevent infinite recursion from circular symlinks.
	if visitedDirs == nil {
		visitedDirs = make(map[string]bool)
	}
	realSrc := srcAbs
	if resolved, err := filepath.EvalSymlinks(srcAbs); err == nil {
		realSrc = resolved
	}
	if visitedDirs[realSrc] {
		// Already copied this directory — circular symlink detected, skip.
		return nil
	}
	visitedDirs[realSrc] = true
	return filepath.WalkDir(srcAbs, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		base := filepath.Base(path)
		if !includeRuntimeArtifacts && entry.IsDir() && isSkillRuntimePackageDir(base) {
			return filepath.SkipDir
		}
		if !includeRuntimeArtifacts && !entry.IsDir() && isSkillRuntimePackageFile(base) {
			return nil
		}
		target := filepath.Join(dstAbs, rel)
		if !pathWithinDir(dstAbs, target) {
			return fmt.Errorf("illegal copy target: %s", target)
		}
		// Handle symlinks: dereference and copy the target content instead of
		// rejecting. npm's node_modules/.bin/ uses symlinks extensively; refusing
		// them would make workspace isolation fail for all npm-based skills.
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, resolveErr := filepath.EvalSymlinks(path)
			if resolveErr != nil {
				// Broken symlink — skip silently (non-critical).
				return nil
			}
			// Security: resolved target must be within the source tree.
			if !pathWithinDir(srcAbs, resolved) {
				// Symlink points outside the skill dir — skip for safety.
				return nil
			}
			resolvedInfo, statErr := os.Stat(resolved)
			if statErr != nil {
				return nil
			}
			if resolvedInfo.IsDir() {
				// Symlink to directory: create the directory in target and
				// let WalkDir traverse it naturally (it won't — WalkDir
				// doesn't follow dir symlinks). Copy recursively.
				return copyDirContentsRecursive(resolved, target, includeRuntimeArtifacts, visitedDirs)
			}
			// Symlink to file: copy the resolved file content.
			data, readErr := os.ReadFile(resolved)
			if readErr != nil {
				return readErr
			}
			return os.WriteFile(target, data, resolvedInfo.Mode().Perm())
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if !pathWithinDir(srcAbs, resolved) {
			return fmt.Errorf("refusing to package file outside skill dir: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func pathWithinDir(root, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(targetAbs))
	if err != nil {
		return false
	}
	return rel == "." || (rel != "" && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

// zipDirectory packages srcDir into a zip file without following links.
func zipDirectory(srcDir, zipPath string) (err error) {
	srcAbs, err := filepath.Abs(srcDir)
	if err != nil {
		return err
	}
	srcAbs = filepath.Clean(srcAbs)
	outFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(zipPath)
		}
	}()
	outClosed := false
	defer func() {
		if outClosed {
			return
		}
		if closeErr := outFile.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	zw := zip.NewWriter(outFile)
	walkErr := filepath.WalkDir(srcAbs, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to zip link: %s", path)
		}
		rel, err := filepath.Rel(srcAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		base := filepath.Base(path)
		if entry.IsDir() && isSkillRuntimePackageDir(base) {
			return filepath.SkipDir
		}
		if !entry.IsDir() && isSkillRuntimePackageFile(base) && base != "skill_package_manifest.json" {
			return nil
		}
		zipName := filepath.ToSlash(rel)
		if entry.IsDir() {
			zipName += "/"
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = zipName
		if !entry.IsDir() {
			header.Method = zip.Deflate
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if walkErr != nil {
		_ = zw.Close()
		return walkErr
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := outFile.Close(); err != nil {
		return err
	}
	outClosed = true
	return nil
}
