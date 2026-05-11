package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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
	Name             string                 `json:"name"`
	DirName          string                 `json:"dir_name,omitempty"`
	Description      string                 `json:"description"`
	Triggers         []string               `json:"triggers"`
	Steps            []corelib.NLSkillStep  `json:"steps"`
	Status           string                 `json:"status"`
	CreatedAt        time.Time              `json:"created_at"`
	Source           string                 `json:"source"`
	SourceProject    string                 `json:"source_project"`
	ExecutionClass   string                 `json:"execution_class,omitempty"`
	HubSkillID       string                 `json:"hub_skill_id,omitempty"`
	HubVersion       string                 `json:"hub_version,omitempty"`
	TrustLevel       string                 `json:"trust_level,omitempty"`
	Type             string                 `json:"type,omitempty"`          // "executable" (default) | "knowledge"
	Content          string                 `json:"content,omitempty"`       // Markdown content for knowledge-type skills
	Publisher        string                 `json:"publisher,omitempty"`     // Plugin namespace publisher
	Mode             string                 `json:"mode,omitempty"`          // "sequential" (default) | "interactive" | "api_workflow"
	HasDocumentation bool                   `json:"has_documentation"`       // true if SKILL.md exists in skill directory
	SkillDir         string                 `json:"-"`                       // skill directory path (internal use, not serialized to frontend)
	Params           []corelib.NLSkillParam `json:"params,omitempty"`        // parameter schema (explicit or synthesized)
	RequiredArgs     []string               `json:"required_args,omitempty"` // required template variables
	UsageCount       int                    `json:"usage_count"`
	SuccessCount     int                    `json:"success_count"`
	FailureCount     int                    `json:"failure_count"`
	SuccessRate      float64                `json:"success_rate"` // computed: SuccessCount / UsageCount
	LastUsedAt       *time.Time             `json:"last_used_at,omitempty"`
	LastError        string                 `json:"last_error,omitempty"`
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

const skillLoadCacheTTL = 30 * time.Second

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
	cfg, err := e.app.LoadConfig()
	if err != nil {
		return nil
	}
	cacheKey := skillLoadCacheKey(cfg.NLSkills, cfg.ExternalSkillDirs)
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
			knownByDir[filepath.Clean(s.SkillDir)] = i
		}
		knownByName[s.Name] = i
	}

	fileSkills := skill.ScanAllSkillDirsWithExternal(cfg.ExternalSkillDirs)
	for _, fs := range fileSkills {
		// Primary: match by directory path (stable identity).
		// Fallback: match by Name (backward compat for config entries without SkillDir).
		idx := -1
		if fs.SkillDir != "" {
			if i, ok := knownByDir[filepath.Clean(fs.SkillDir)]; ok {
				idx = i
			}
		}
		if idx < 0 {
			if i, ok := knownByName[fs.Name]; ok {
				idx = i
			}
		}

		if idx >= 0 {
			configSkill := &skills[idx]
			if len(fs.Steps) > 0 {
				// On-disk skill.yaml is the source of truth for steps.
				configSkill.Steps = fs.Steps
				configSkill.SkillDir = fs.SkillDir
				configSkill.Params = fs.Params
				if len(configSkill.Triggers) == 0 {
					configSkill.Triggers = fs.Triggers
				}
				if strings.TrimSpace(configSkill.Description) == "" {
					configSkill.Description = fs.Description
				}
				// Stats-only stubs (source == "file") have empty Status;
				// inherit from the on-disk YAML so the skill is usable.
				if configSkill.Status == "" && fs.Status != "" {
					configSkill.Status = fs.Status
				}
			}
			continue
		}
		skills = append(skills, fs)
		// Update both indexes for dedup of subsequent file skills.
		newIdx := len(skills) - 1
		if fs.SkillDir != "" {
			knownByDir[filepath.Clean(fs.SkillDir)] = newIdx
		}
		knownByName[fs.Name] = newIdx
	}

	e.skillCacheMu.Lock()
	e.skillCache = cloneSkillEntries(skills)
	e.skillCacheAt = time.Now()
	e.skillCacheKey = cacheKey
	e.skillCacheMu.Unlock()

	return cloneSkillEntries(skills)
}

func (e *SkillExecutor) invalidateSkillCache() {
	e.skillCacheMu.Lock()
	e.skillCache = nil
	e.skillCacheAt = time.Time{}
	e.skillCacheKey = ""
	e.skillCacheMu.Unlock()
}

func skillLoadCacheKey(skills []corelib.NLSkillEntry, externalDirs []string) string {
	payload := struct {
		Skills       []corelib.NLSkillEntry `json:"skills"`
		ExternalDirs []string               `json:"external_dirs"`
	}{
		Skills:       skills,
		ExternalDirs: externalDirs,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("skills=%d external=%s", len(skills), strings.Join(externalDirs, string(os.PathListSeparator)))
	}
	return string(data)
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
	cfg, err := e.app.LoadConfig()
	if err != nil {
		return skill.ScanAllSkillDirs()
	}
	return skill.ScanAllSkillDirsWithExternal(cfg.ExternalSkillDirs)
}

// saveSkills persists skill entries to config.
// File-based skills (source == "file") are saved as stats-only stubs so that
// usage statistics survive across restarts. The full definition (steps,
// triggers, description, etc.) is always loaded from the YAML file at runtime
// via loadSkills -> scanSkillYAMLFiles (directory-path identity matching).
func (e *SkillExecutor) saveSkills(skills []corelib.NLSkillEntry) error {
	cfg, err := e.app.LoadConfig()
	if err != nil {
		return err
	}
	filtered := make([]corelib.NLSkillEntry, 0, len(skills))
	for _, s := range skills {
		if normalizeSkillEntrySource(s.Source) == skillEntrySourceFile {
			// Only persist the stats overlay; strip definition data so
			// config.json is not polluted with YAML-managed content.
			if s.UsageCount > 0 || s.SuccessCount > 0 || s.FailureCount > 0 || s.WorkaroundCount > 0 {
				filtered = append(filtered, corelib.NLSkillEntry{
					Name:            s.Name,
					Source:          string(skillEntrySourceFile),
					UsageCount:      s.UsageCount,
					SuccessCount:    s.SuccessCount,
					FailureCount:    s.FailureCount,
					WorkaroundCount: s.WorkaroundCount,
					LastUsedAt:      s.LastUsedAt,
					LastError:       s.LastError,
				})
			}
			continue
		}
		filtered = append(filtered, s)
	}
	cfg.NLSkills = filtered
	if err := e.app.SaveConfig(cfg); err != nil {
		return err
	}
	e.invalidateSkillCache()
	return nil
}

// Register adds a new Skill definition.
func (e *SkillExecutor) Register(entry corelib.NLSkillEntry) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	name := strings.TrimSpace(entry.Name)
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	skills := e.loadSkills()
	primaryDir, primaryErr := skill.PrimarySkillsDir()
	for _, s := range skills {
		if s.Name != name {
			continue
		}
		if normalizeSkillEntrySource(entry.Source) == skillEntrySourceHub && normalizeSkillEntrySource(s.Source) == skillEntrySourceFile && primaryErr == nil {
			extractedDir := filepath.Join(primaryDir, name)
			if filepath.Clean(s.SkillDir) == filepath.Clean(extractedDir) {
				continue
			}
		}
		return fmt.Errorf("skill %q already exists", name)
	}
	entry.Name = name
	if entry.Status == "" {
		entry.Status = string(skillEntryStatusActive)
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
	if idx == -1 {
		return fmt.Errorf("skill %q was removed during update", name)
	}

	// Replace mutable fields, preserve identity fields.
	skills[idx].Description = updated.Description
	skills[idx].Triggers = updated.Triggers
	skills[idx].Steps = updated.Steps
	skills[idx].HubVersion = updated.HubVersion
	skills[idx].TrustLevel = updated.TrustLevel

	return e.saveSkills(skills)
}

// Delete removes a Skill by name.
func (e *SkillExecutor) Delete(name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills := e.loadSkills()
	found := false
	for i, s := range skills {
		if s.Name == name {
			found = true
			// Always remove from config regardless of source.
			// Previously file-based skills were skipped here, leaving
			// orphaned stats-only stubs in config.json when the on-disk
			// directory was already deleted; the "ghost skill" bug.
			skills = append(skills[:i], skills[i+1:]...)
			if err := e.saveSkills(skills); err != nil {
				return err
			}
			break
		}
	}
	if !found {
		return fmt.Errorf("skill %q not found", name)
	}
	// Always clean up on-disk skill directories so that loadSkills
	// (which scans disk via scanSkillYAMLFiles) won't rediscover it.
	e.removeSkillDirs(name)
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
			if s.Name == name || s.DirName == name {
				if s.SkillDir != "" {
					_ = os.RemoveAll(s.SkillDir)
				}
			}
		}
	}
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
			Name:             s.Name,
			DirName:          s.DirName,
			Description:      s.Description,
			Triggers:         triggers,
			Steps:            steps,
			Status:           s.Status,
			Source:           s.Source,
			SourceProject:    s.SourceProject,
			ExecutionClass:   classifySkillExecutionClass(s),
			HubSkillID:       s.HubSkillID,
			HubVersion:       s.HubVersion,
			TrustLevel:       s.TrustLevel,
			Type:             s.Type,
			Content:          s.Content,
			Publisher:        s.Publisher,
			Mode:             s.Mode,
			HasDocumentation: (normalizeSkillTypeKind(s.Type).IsKnowledge() && s.Content != "") || (s.SkillDir != "" && hasSkillDocFile(s.SkillDir)),
			SkillDir:         s.SkillDir,
			Params:           s.Params,
			RequiredArgs:     s.RequiredArgs,
			UsageCount:       s.UsageCount,
			SuccessCount:     s.SuccessCount,
			FailureCount:     s.FailureCount,
			LastError:        s.LastError,
		}
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
		primaryDir, err := skill.PrimarySkillsDir()
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
	if skill.IsPipelineSkill(&preparedEntry) {
		return e.executePipelineSkillDetailed(&preparedEntry, vars, runArgs)
	}
	prep, err := skill.PrepareRunnerExecution(&preparedEntry, vars, runArgs, extraEnv, skill.RunnerBackendGUI)
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
	// This synchronous path (used by capability_gap_detector and auto-install)
	// also needs proxy support, mirroring the async SkillRunner.executeAsync path.
	// Note: uses os.Setenv because executeBashStep inherits process env.
	// Save/restore previous values to avoid clobbering user-set env vars.
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
			proxyCfg = corelib.OpenAIProxyConfig{
				URL:      llmCfg.URL,
				Key:      llmCfg.Key,
				Model:    llmCfg.Model,
				Protocol: llmCfg.Protocol,
				WireAPI:  llmCfg.WireAPI,
			}
		}
		if strings.TrimSpace(proxyCfg.URL) == "" || strings.TrimSpace(proxyCfg.Model) == "" {
			return skillExecutionResult{Captured: cloneStringMapGUI(vars), Err: fmt.Errorf("skill requires OpenAI-compatible environment variables, but the GUI local proxy cannot start because no LLM provider URL/model is configured [action: configure_llm]")}
		}
		proxy := corelib.NewOpenAIProxy(proxyCfg)
		port, proxyErr := proxy.Start()
		if proxyErr != nil {
			return skillExecutionResult{Captured: cloneStringMapGUI(vars), Err: fmt.Errorf("skill requires OpenAI-compatible environment variables, but the GUI local proxy failed to start: %v [action: retry]", proxyErr)}
		}
		defer proxy.Stop()
		// Save previous values for restore
		prevKey, hadKey := os.LookupEnv("OPENAI_API_KEY")
		prevURL, hadURL := os.LookupEnv("OPENAI_BASE_URL")
		prevModel, hadModel := os.LookupEnv("OPENAI_MODEL")
		os.Setenv("OPENAI_API_KEY", "sk-maclaw-local-proxy")
		os.Setenv("OPENAI_BASE_URL", fmt.Sprintf("http://127.0.0.1:%d/v1", port))
		os.Setenv("OPENAI_MODEL", proxyCfg.Model)
		defer func() {
			if hadKey {
				os.Setenv("OPENAI_API_KEY", prevKey)
			} else {
				os.Unsetenv("OPENAI_API_KEY")
			}
			if hadURL {
				os.Setenv("OPENAI_BASE_URL", prevURL)
			} else {
				os.Unsetenv("OPENAI_BASE_URL")
			}
			if hadModel {
				os.Setenv("OPENAI_MODEL", prevModel)
			} else {
				os.Unsetenv("OPENAI_MODEL")
			}
		}()
		log.Printf("[skill-executor] openai proxy started on port %d for skill %q", port, preparedEntry.Name)
	}

	hasFailure := false
	for _, warning := range prep.Warnings {
		results = append(results, "[Warning] "+warning)
	}
	for i, step := range executionSteps {
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
		if classifySkillStepAction(stepCopy.Action).IsCraftTool() && len(extraEnv) > 0 {
			if stepCopy.Params == nil {
				stepCopy.Params = map[string]interface{}{}
			}
			skill.MergeExtraEnvParam(stepCopy.Params, extraEnv)
		}
		stepCopy = skill.PrepareResolvedStepEnv(stepCopy, preparedEntry.RequiredEnv, extraEnv)
		restoreEnv := installSkillStepProcessEnv(stepCopy.Action, extraEnv)
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
		"args":        userPrompt,
		"input":       userPrompt,
		"user_prompt": userPrompt,
	}
}

func skillExecutionExtraEnv(runArgs map[string]interface{}) map[string]string {
	return skill.ExtractRunExtraEnvFromArgs(runArgs)
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
		e.app.emitEvent("skill:usage_updated")
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
	if len(entry.Pipeline) == 0 {
		return skillExecutionResult{Err: fmt.Errorf("%s", skill.FormatNoExecutableStepsMessage(entry.Name, entry, skill.RunnerBackendGUI))}
	}
	if vars == nil {
		vars = map[string]string{}
	}
	extraEnv := skill.ExtractRunExtraEnvFromArgs(runArgs)
	prep, err := skill.PreparePipelineRunnerExecution(entry, vars, runArgs, extraEnv, skill.RunnerBackendGUI)
	if err != nil {
		return skillExecutionResult{Captured: cloneStringMapGUI(vars), Err: err}
	}
	for _, warning := range prep.RequirementWarnings {
		log.Printf("[skill-executor] pipeline requirement warning for %q: %s", entry.Name, warning.Message)
	}
	pipelineRunArgs := skill.WithPipelineRunStack(runArgs, entry.Name)
	runCtx := context.Background()
	cancel := func() {}
	if entry.GlobalTimeout > 0 {
		runCtx, cancel = context.WithTimeout(runCtx, time.Duration(entry.GlobalTimeout)*time.Second)
	}
	defer cancel()
	runner := &skill.PipelineRunner{Executor: skillExecutorPipelineExecutor{exec: e, baseRunArgs: pipelineRunArgs}}
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
}

func (e skillExecutorPipelineExecutor) RunSubSkill(ctx context.Context, skillName string, params map[string]string) (map[string]string, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	runArgs := skill.BuildPipelineSubSkillRunArgs(e.baseRunArgs, params)
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
	switch classifySkillStepAction(step.Action) {
	case skillStepActionCreateSession:
		tool, _ := step.Params["tool"].(string)
		projectPath, _ := step.Params["project_path"].(string)
		projectID, _ := step.Params["project_id"].(string)
		provider, _ := step.Params["provider"].(string)
		resumeSessionID, _ := step.Params["resume_session_id"].(string)
		if tool == "" {
			return "", fmt.Errorf("missing tool parameter")
		}
		if hint := skillCreateSessionGuard(skillDescription, step); hint != "" {
			return "", fmt.Errorf("%s", hint)
		}
		starter := e.app.sessionStarter
		if starter == nil {
			e.app.ensureInteractionInfra()
			starter = e.app.sessionStarter
		}
		if starter == nil {
			return "", fmt.Errorf("session starter not initialized")
		}
		startResult, err := starter.Start(CodingSessionStartRequest{
			Tool:               tool,
			ProjectID:          projectID,
			ProjectPath:        projectPath,
			Provider:           provider,
			ResumeSessionID:    resumeSessionID,
			InjectResumePrompt: false,
			LaunchSource:       RemoteLaunchSourceAI,
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("会话已创建: ID=%s", startResult.View.ID), nil

	case skillStepActionSendInput:
		sessionID, _ := step.Params["session_id"].(string)
		text, _ := step.Params["text"].(string)
		if sessionID == "" || text == "" {
			return "", fmt.Errorf("missing session_id or text parameter")
		}
		if e.manager == nil {
			return "", fmt.Errorf("session manager not initialized")
		}
		if err := e.manager.WriteInput(sessionID, text); err != nil {
			return "", err
		}
		return fmt.Sprintf("sent input to session %s", sessionID), nil

	case skillStepActionSendAndObserve:
		sessionID, _ := step.Params["session_id"].(string)
		text, _ := step.Params["text"].(string)
		timeoutSeconds, _ := step.Params["timeout_seconds"].(float64)
		if sessionID == "" || text == "" {
			return "", fmt.Errorf("missing session_id or text parameter")
		}
		if e.manager == nil {
			return "", fmt.Errorf("session manager not initialized")
		}
		return SendAndObserveSession(e.manager, sessionID, text, SessionObserveOptions{
			TimeoutSeconds: timeoutSeconds,
			Lines:          40,
		}, func(renderArgs map[string]interface{}) string {
			h := &IMMessageHandler{app: e.app, manager: e.manager}
			return h.toolGetSessionOutput(renderArgs)
		}), nil

	case skillStepActionCallMCPTool:
		serverRef, _ := step.Params["server_id"].(string)
		toolName, _ := step.Params["tool_name"].(string)
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
			return e.app.localMCPManager.CallTool(resolvedID, toolName, args)
		}
		if e.mcpRegistry == nil {
			return "", fmt.Errorf("MCP registry not initialized")
		}
		return e.mcpRegistry.CallTool(resolvedID, toolName, args)

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
	case sshToolActionListTasks:
		return e.sshListTasks(), nil
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
		return "", fmt.Errorf("未知 SSH 操作 %s; supported: connect/exec/exec_background/check_task/list_tasks/kill_task/upload/download/list/close", action)
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
	sessionID, _ := args["session_id"].(string)
	command, _ := args["command"].(string)
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
	task, err := e.bgTaskMgr.Submit(sessionID, command)
	if err != nil {
		return fmt.Sprintf("background task submit failed: %v", err)
	}
	return fmt.Sprintf("background task started\ntask_id: %s\ncommand: %s\nlog_file: %s\npid: %s\nstatus: running", task.TaskID, task.Command, task.LogFile, task.PID)
}

func (e *SkillExecutor) sshCheckTask(args map[string]interface{}) string {
	if e.bgTaskMgr == nil {
		return "background task manager is not initialized"
	}
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return "ssh check_task requires task_id"
	}
	tailLines := 50
	if t, ok := args["tail_lines"].(float64); ok && t > 0 {
		tailLines = int(t)
	}
	result, err := e.bgTaskMgr.CheckTask(taskID, tailLines)
	if err != nil {
		return fmt.Sprintf("check task failed: %v", err)
	}
	logTail := result.LogTail
	if logTail == "" {
		logTail = "(no log output)"
	}
	if len(logTail) > 6000 {
		logTail = logTail[:3000] + "\n... (truncated) ...\n" + logTail[len(logTail)-3000:]
	}
	return fmt.Sprintf("task_id: %s\ncommand: %s\nstatus: %s\nalive: %v\nelapsed: %s\n\n--- log tail ---\n%s", result.TaskID, result.Command, result.Status, result.IsAlive, result.Elapsed, logTail)
}

func (e *SkillExecutor) sshListTasks() string {
	if e.bgTaskMgr == nil {
		return "当前无后台任务"
	}
	tasks := e.bgTaskMgr.ListTasks()
	if len(tasks) == 0 {
		return "当前无后台任务"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("background tasks: %d\n", len(tasks)))
	for _, t := range tasks {
		elapsed := time.Since(t.StartedAt).Round(time.Second)
		sb.WriteString(fmt.Sprintf("  - %s | PID: %s | status: %s | elapsed: %s\n    command: %s\n", t.TaskID, t.PID, t.Status, elapsed, t.Command))
	}
	return sb.String()
}

func (e *SkillExecutor) sshKillTask(args map[string]interface{}) string {
	if e.bgTaskMgr == nil {
		return "background task manager is not initialized"
	}
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return "ssh kill_task requires task_id"
	}
	if err := e.bgTaskMgr.KillTask(taskID); err != nil {
		return fmt.Sprintf("kill task failed: %v", err)
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
		return ""
	case intentSSH:
		return fmt.Sprintf("SSH/server operation task detected (%s). Use ssh(action=\"connect\", ...), ssh(action=\"exec\", ...), ssh(action=\"exec_background\", ...), and upload/download/check_task before create_session.", formatIntentEvidence(result))
	case intentNonCoding:
		return fmt.Sprintf("Not a coding task (%s). Prefer direct file/tool execution before opening a coding session.", formatIntentEvidence(result))
	case intentUnknown, intentAmbiguous:
		return fmt.Sprintf("Cannot determine whether this needs a coding session (%s). Clarify before creating a coding session.", formatIntentEvidence(result))
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

// DiagnoseSkillFiles scans ~/.maclaw/data/skills/ and reports load status for each
// subdirectory, including the reason if a skill failed to load (Wails binding).
func (a *App) DiagnoseSkillFiles() []SkillDiagEntry {
	skillsRoot, err := skill.PrimarySkillsDir()
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
	cfg.ExternalSkillDirs = append(cfg.ExternalSkillDirs, dir)
	return count, a.SaveConfig(cfg)
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
	cfg.ExternalSkillDirs = filtered
	return a.SaveConfig(cfg)
}

// CreateNLSkill registers a new NL Skill definition (Wails binding).
func (a *App) CreateNLSkill(def corelib.NLSkillEntry) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.Register(def)
}

// UpdateNLSkill updates an existing NL Skill definition (Wails binding).
func (a *App) UpdateNLSkill(def corelib.NLSkillEntry) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.Update(def)
}

// DeleteNLSkill removes an NL Skill by name (Wails binding).
func (a *App) DeleteNLSkill(name string) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	err := a.skillExecutor.Delete(name)
	if err == nil && a.hubUpdCache != nil {
		a.hubUpdCache.invalidate()
	}
	return err
}

// ImportNLSkillZip opens a file dialog to select a zip file, validates it as a
// file-backed skill package using skill.yaml, skill.yml, or skill.md, and imports it.
// Returns the imported skill name on success.
func (a *App) ImportNLSkillZip() (string, error) {
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
	name, importErr := a.importFileBackedSkillZipPath(selection)
	if importErr == nil {
		return name, nil
	}
	if validationErr := a.validateSkillZip(selection); validationErr != nil && errors.Is(importErr, errNoRecognizableSkillDefinition) {
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
	for _, packageRoot := range packageRoots {
		entry, err := loadImportedSkillEntry(packageRoot)
		if err != nil {
			return "", err
		}
		if existingNames[entry.Name] {
			return "", fmt.Errorf("skill %q already exists", entry.Name)
		}
		existingNames[entry.Name] = true
		entries = append(entries, entry)
	}

	primaryDir, err := skill.PrimarySkillsDir()
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
			continue
		}
		installedDirs = append(installedDirs, destDir)
	}

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
		params = append(params, corelib.NLSkillParam{Name: p.Name, Description: p.Description, Aliases: p.Aliases, CLIFlag: p.CLIFlag, Default: p.Default, Required: p.Required})
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
	a.ensureSkillRunner()
	if a.skillRunner == nil {
		return "", fmt.Errorf("skill runner not initialized")
	}
	return a.skillRunner.StartRun(skillName, runArgs)
}

// GetNLSkillRunStatus returns the status of an async skill run for Wails.
func (a *App) GetNLSkillRunStatus(runID string) (*SkillRunStatus, error) {
	a.ensureSkillRunner()
	if a.skillRunner == nil {
		return nil, fmt.Errorf("skill runner not initialized")
	}
	return a.skillRunner.GetRunStatus(runID)
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

	_, report, err := prepareSkillDirForMarket(tmpDir, true)
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

	zipPath := filepath.Join(a.GetTempDir(), fmt.Sprintf("skill-%s-%d.zip", toKebabCase(skillName), time.Now().UnixMilli()))
	if err := zipDirectory(tmpDir, zipPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", err
	}
	return zipPath, tmpDir, nil
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
	dst.UsageCount = src.UsageCount
	dst.SuccessCount = src.SuccessCount
	dst.FailureCount = src.FailureCount
	dst.WorkaroundCount = src.WorkaroundCount
	dst.LastUsedAt = src.LastUsedAt
	dst.RepairAttemptCount = src.RepairAttemptCount
	dst.LastRepairAt = src.LastRepairAt
	dst.RepairHistory = append([]corelib.SkillRepairRecord(nil), src.RepairHistory...)
}

// executeBashStep runs a shell command as a skill step.
// Keep this synchronous path on the same shell/runtime mechanics as SkillRunner.
func executeBashStep(command string, params map[string]interface{}, app *App) (string, error) {
	return runBashStepWithContextFull(context.Background(), command, params, "", app)
}

// File system helpers.

// copyDirContents copies src contents into dst without following links.
func copyDirContents(src, dst string) error {
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
	return filepath.WalkDir(srcAbs, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to package link: %s", path)
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
		if !entry.IsDir() && isSkillRuntimePackageFile(base) {
			return nil
		}
		target := filepath.Join(dstAbs, rel)
		if !pathWithinDir(dstAbs, target) {
			return fmt.Errorf("illegal copy target: %s", target)
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
		if !entry.IsDir() && isSkillRuntimePackageFile(base) {
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
