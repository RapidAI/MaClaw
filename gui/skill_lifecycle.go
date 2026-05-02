package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

const skillMarketReadyMinScore = 70

type skillQualityReport struct {
	Score       int
	MarketReady bool
	Reasons     []string
	Portability *skill.PortabilityReport
	Package     skillPackageQualitySummary
}

type skillPackageQualitySummary struct {
	Files              int      `json:"files"`
	HasSkillYAML       bool     `json:"has_skill_yaml"`
	HasSkillDefinition bool     `json:"has_skill_definition"`
	HasSkillMD         bool     `json:"has_skill_md"`
	ReferencedMissing  []string `json:"referenced_missing,omitempty"`
}

// SkillQualityStatus is the exported view returned by lifecycle quality audits.
type SkillQualityStatus = persistedSkillQualityStatus

type persistedSkillQualityStatus struct {
	SkillName           string                     `json:"skill_name"`
	Stage               string                     `json:"stage"`
	Score               int                        `json:"score"`
	MarketReady         bool                       `json:"market_ready"`
	MinMarketScore      int                        `json:"min_market_score"`
	Reasons             []string                   `json:"reasons,omitempty"`
	PortabilitySummary  skill.IssueSummary         `json:"portability_summary"`
	PackageSummary      skillPackageQualitySummary `json:"package_summary"`
	RequireRuntimeProof bool                       `json:"require_runtime_proof"`
	UsageCount          int                        `json:"usage_count"`
	SuccessCount        int                        `json:"success_count"`
	FailureCount        int                        `json:"failure_count"`
	VerificationStatus  string                     `json:"verification_status"`
	VerificationSummary string                     `json:"verification_summary,omitempty"`
	LocalHash           string                     `json:"local_hash,omitempty"`
	UpdatedAt           string                     `json:"updated_at"`
}

type skillPackageManifest struct {
	SkillName   string                      `json:"skill_name"`
	PackageKind string                      `json:"package_kind"`
	GeneratedAt string                      `json:"generated_at"`
	Quality     persistedSkillQualityStatus `json:"quality"`
	Files       []skillPackageManifestFile  `json:"files"`
}

type skillPackageManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func skillVerificationStatus(entry *corelib.NLSkillEntry, requireRuntimeProof bool) string {
	if entry == nil {
		return "missing_entry"
	}
	if entry.SuccessCount > 0 {
		return "verified_success"
	}
	if entry.FailureCount > 0 {
		return "failed"
	}
	if requireRuntimeProof {
		return "needs_runtime_proof"
	}
	return "static_checked"
}

func skillVerificationSummary(entry *corelib.NLSkillEntry, requireRuntimeProof bool) string {
	if entry == nil {
		return "skill entry is missing"
	}
	if entry.SuccessCount > 0 {
		return fmt.Sprintf("%d successful run(s), %d failed run(s)", entry.SuccessCount, entry.FailureCount)
	}
	if entry.FailureCount > 0 {
		return fmt.Sprintf("no successful run; %d failed run(s)", entry.FailureCount)
	}
	if requireRuntimeProof {
		return "upload requires at least one successful verification run"
	}
	return "static portability and package checks completed"
}

func buildSkillQualityStatus(skillDir string, entry *corelib.NLSkillEntry, quality skillQualityReport, stage string, requireRuntimeProof bool) SkillQualityStatus {
	status := persistedSkillQualityStatus{
		Stage:               stage,
		Score:               quality.Score,
		MarketReady:         quality.MarketReady,
		MinMarketScore:      skillMarketReadyMinScore,
		Reasons:             append([]string(nil), quality.Reasons...),
		RequireRuntimeProof: requireRuntimeProof,
		VerificationStatus:  skillVerificationStatus(entry, requireRuntimeProof),
		VerificationSummary: skillVerificationSummary(entry, requireRuntimeProof),
		UpdatedAt:           time.Now().Format(time.RFC3339),
	}
	if entry != nil {
		status.SkillName = entry.Name
		status.UsageCount = entry.UsageCount
		status.SuccessCount = entry.SuccessCount
		status.FailureCount = entry.FailureCount
	}
	if strings.TrimSpace(skillDir) != "" {
		status.LocalHash = skillDirHash(skillDir)
	}
	if quality.Portability != nil {
		status.PortabilitySummary = quality.Portability.Summary
	}
	status.PackageSummary = quality.Package
	return status
}

func writeSkillPackageManifest(skillDir string, entry *corelib.NLSkillEntry, quality skillQualityReport, stage string, requireRuntimeProof bool) error {
	if strings.TrimSpace(skillDir) == "" || entry == nil {
		return nil
	}
	status := buildSkillQualityStatus(skillDir, entry, quality, stage, requireRuntimeProof)
	manifest := skillPackageManifest{
		SkillName:   entry.Name,
		PackageKind: "maclaw-skill-market",
		GeneratedAt: time.Now().Format(time.RFC3339),
		Quality:     status,
	}
	if err := filepath.WalkDir(skillDir, func(path string, dirEntry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if dirEntry.IsDir() {
			return nil
		}
		base := dirEntry.Name()
		if strings.HasSuffix(base, ".bak") || base == "upload_status.json" || base == "quality_status.json" || base == "skill_package_manifest.json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := dirEntry.Info()
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		rel, err := filepath.Rel(skillDir, path)
		if err != nil {
			return err
		}
		manifest.Files = append(manifest.Files, skillPackageManifestFile{
			Path:   filepath.ToSlash(rel),
			Size:   info.Size(),
			SHA256: hex.EncodeToString(sum[:]),
		})
		return nil
	}); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(skillDir, "skill_package_manifest.json"), data, 0o644)
}

func skillYAMLFromEntry(entry *corelib.NLSkillEntry) *skill.SkillYAMLFile {
	if entry == nil {
		return nil
	}
	status := strings.TrimSpace(entry.Status)
	if status == "" {
		status = "active"
	}
	platforms := append([]string(nil), entry.Platforms...)
	if len(platforms) == 0 {
		platforms = []string{"universal"}
	}
	sf := &skill.SkillYAMLFile{
		Name:                    entry.Name,
		Description:             entry.Description,
		Triggers:                append([]string(nil), entry.Triggers...),
		Status:                  status,
		Platforms:               platforms,
		RequiresGUI:             entry.RequiresGUI,
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
	if entry.ProducesArtifact {
		produces := true
		sf.ProducesArtifact = &produces
	}
	if len(entry.RequiresPython) > 0 || len(entry.RequiresNode) > 0 {
		sf.Requires = &skill.SkillYAMLRequires{
			Python: append([]string(nil), entry.RequiresPython...),
			Node:   append([]string(nil), entry.RequiresNode...),
		}
	}
	for _, step := range entry.Steps {
		sf.Steps = append(sf.Steps, skill.SkillYAMLStep{
			Action:    step.Action,
			Params:    step.Params,
			OnError:   step.OnError,
			Name:      step.Name,
			Condition: step.Condition,
			When:      step.When,
			Label:     step.Label,
			Capture:   step.Capture,
		})
	}
	for _, op := range entry.Operations {
		sf.Operations = append(sf.Operations, skill.SkillYAMLOperation{
			Name:        op.Name,
			Description: op.Description,
			Params:      append([]string(nil), op.Params...),
			Labels:      append([]string(nil), op.Labels...),
		})
	}
	for _, step := range entry.Pipeline {
		sf.Pipeline = append(sf.Pipeline, skill.SkillYAMLPipelineStep{
			Skill:              step.Skill,
			Params:             step.Params,
			Checkpoint:         step.Checkpoint,
			CheckpointMessage:  step.CheckpointMessage,
			ContinueOnFail:     step.ContinueOnFail,
			TimeImpactOnReject: step.TimeImpactOnReject,
		})
	}
	for _, p := range entry.Params {
		sf.Params = append(sf.Params, skill.SkillYAMLParam{
			Name:        p.Name,
			Description: p.Description,
			Aliases:     append([]string(nil), p.Aliases...),
			CLIFlag:     p.CLIFlag,
			Default:     p.Default,
			Required:    p.Required,
		})
	}
	return sf
}

func writeSkillYAMLForEntry(skillDir string, entry *corelib.NLSkillEntry) error {
	if strings.TrimSpace(skillDir) == "" || entry == nil {
		return nil
	}
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return err
	}
	yamlPath := filepath.Join(skillDir, "skill.yaml")
	if data, err := os.ReadFile(yamlPath); err == nil {
		_ = os.WriteFile(yamlPath+".bak", data, 0o644)
	}
	data, err := skill.FormatSkillYAMLFile(skillYAMLFromEntry(entry))
	if err != nil {
		return err
	}
	return os.WriteFile(yamlPath, data, 0o644)
}

func prepareSkillDirForMarket(skillDir string, autoFix bool) ([]skill.PortabilityChange, *skill.PortabilityReport, error) {
	if strings.TrimSpace(skillDir) == "" {
		return nil, nil, fmt.Errorf("skill directory is empty")
	}
	var changes []skill.PortabilityChange
	if autoFix {
		fixes, err := skill.AutoFixPortability(skillDir)
		if err != nil {
			return nil, nil, err
		}
		changes = append(changes, fixes...)
	}
	report, err := skill.ValidateSkillPortability(skillDir)
	if err != nil {
		return changes, nil, err
	}
	return changes, report, nil
}

func removeSkillPackagingBackups(skillDir string) error {
	return filepath.WalkDir(skillDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".bak") || name == "upload_status.json" {
			return os.Remove(path)
		}
		return nil
	})
}

func evaluateSkillQuality(entry *corelib.NLSkillEntry, report *skill.PortabilityReport, requireRuntimeSuccess bool) skillQualityReport {
	skillDir := ""
	if entry != nil {
		skillDir = entry.SkillDir
	}
	return evaluateSkillQualityForDir(entry, report, requireRuntimeSuccess, skillDir)
}

func evaluateSkillQualityForDir(entry *corelib.NLSkillEntry, report *skill.PortabilityReport, requireRuntimeSuccess bool, skillDir string) skillQualityReport {
	q := skillQualityReport{Score: 100, MarketReady: true, Portability: report}
	if entry == nil {
		q.Score = 0
		q.MarketReady = false
		q.Reasons = append(q.Reasons, "missing skill entry")
		return q
	}
	if report == nil {
		q.Score -= 30
		q.Reasons = append(q.Reasons, "missing portability report")
	} else {
		q.Score -= report.Summary.Errors * 35
		q.Score -= report.Summary.Warnings * 8
		if report.Summary.Errors > 0 {
			q.Reasons = append(q.Reasons, fmt.Sprintf("%d portability error(s)", report.Summary.Errors))
		}
	}
	if strings.TrimSpace(entry.Description) == "" || len([]rune(entry.Description)) < 10 {
		q.Score -= 15
		q.Reasons = append(q.Reasons, "description is missing or too short")
	}
	if len(entry.Triggers) == 0 {
		q.Score -= 10
		q.Reasons = append(q.Reasons, "no trigger keywords")
	}
	if entry.Type != "knowledge" && len(entry.Steps) == 0 && len(entry.Pipeline) == 0 {
		q.Score -= 35
		q.Reasons = append(q.Reasons, "no executable steps or pipeline")
	}
	if strings.TrimSpace(entry.LastError) != "" {
		q.Score -= 20
		q.Reasons = append(q.Reasons, "latest run has an error")
	}
	if requireRuntimeSuccess && entry.SuccessCount == 0 {
		q.Score -= 35
		q.Reasons = append(q.Reasons, "no successful verification run yet")
	}
	if entry.FailureCount > entry.SuccessCount && entry.FailureCount > 0 {
		q.Score -= 20
		q.Reasons = append(q.Reasons, "failure count exceeds success count")
	}
	if q.Score < 0 {
		q.Score = 0
	}
	packageSummary, packagePenalty, packageFatal, packageReasons := evaluateSkillPackageCompleteness(skillDir, entry)
	q.Package = packageSummary
	if packagePenalty > 0 {
		q.Score -= packagePenalty
		q.Reasons = append(q.Reasons, packageReasons...)
	}
	if q.Score < 0 {
		q.Score = 0
	}
	q.MarketReady = q.Score >= skillMarketReadyMinScore && !packageFatal && (report == nil || report.Summary.Errors == 0)
	return q
}

func evaluateSkillPackageCompleteness(skillDir string, entry *corelib.NLSkillEntry) (skillPackageQualitySummary, int, bool, []string) {
	var summary skillPackageQualitySummary
	if strings.TrimSpace(skillDir) == "" {
		return summary, 0, false, nil
	}
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return summary, 35, true, []string{"skill directory cannot be read"}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if isSkillRuntimePackageFile(name) {
			continue
		}
		summary.Files++
		switch strings.ToLower(name) {
		case "skill.yaml", "skill.yml":
			summary.HasSkillYAML = true
			summary.HasSkillDefinition = true
		case "skill.md", "readme.md":
			summary.HasSkillMD = true
		}
	}

	penalty := 0
	fatal := false
	var reasons []string
	if !summary.HasSkillDefinition && !summary.HasSkillMD {
		penalty += 45
		fatal = true
		reasons = append(reasons, "package lacks skill definition or skill documentation")
	}
	if !summary.HasSkillMD {
		penalty += 8
		reasons = append(reasons, "missing skill documentation")
	}
	if entry != nil {
		summary.ReferencedMissing = missingReferencedSkillFiles(skillDir, entry)
		if len(summary.ReferencedMissing) > 0 {
			penalty += 35
			fatal = true
			reasons = append(reasons, "package is missing referenced local file(s): "+strings.Join(summary.ReferencedMissing, ", "))
		}
	}
	return summary, penalty, fatal, reasons
}

func isSkillRuntimePackageFile(name string) bool {
	return strings.HasSuffix(name, ".bak") || name == "upload_status.json" || name == "quality_status.json" || name == "skill_package_manifest.json"
}

func missingReferencedSkillFiles(skillDir string, entry *corelib.NLSkillEntry) []string {
	seen := map[string]struct{}{}
	var missing []string
	for _, step := range entry.Steps {
		command, _ := step.Params["command"].(string)
		for _, ref := range localFileRefsFromCommand(command) {
			cleanRef := strings.TrimPrefix(filepath.ToSlash(ref), "./")
			if cleanRef == "" || strings.HasPrefix(cleanRef, "../") {
				continue
			}
			if _, ok := seen[cleanRef]; ok {
				continue
			}
			seen[cleanRef] = struct{}{}
			if _, err := os.Stat(filepath.Join(skillDir, filepath.FromSlash(cleanRef))); err != nil {
				missing = append(missing, cleanRef)
			}
		}
	}
	return missing
}

func localFileRefsFromCommand(command string) []string {
	var refs []string
	for _, raw := range strings.Fields(command) {
		token := normalizeLocalFileRefToken(raw)
		if token == "" || filepath.IsAbs(token) {
			continue
		}
		if strings.HasPrefix(token, "./") || strings.HasPrefix(filepath.ToSlash(token), "scripts/") || looksLikeScriptFile(token) {
			refs = append(refs, token)
		}
	}
	return refs
}

func normalizeLocalFileRefToken(token string) string {
	token = strings.Trim(token, "\\\"'\\;,()[]{}")
	if idx := strings.Index(token, "="); idx >= 0 && idx+1 < len(token) {
		left := strings.TrimSpace(token[:idx])
		if strings.HasPrefix(left, "-") {
			token = token[idx+1:]
		}
	}
	token = strings.Trim(token, "\\\"'\\;,()[]{}")
	token = strings.TrimPrefix(token, "{baseDir}/")
	token = strings.TrimPrefix(token, "{baseDir}\\")
	token = strings.TrimPrefix(token, "$BASE_DIR/")
	token = strings.TrimPrefix(token, "$BASE_DIR\\")
	token = strings.TrimPrefix(token, "${BASE_DIR}/")
	token = strings.TrimPrefix(token, "${BASE_DIR}\\")
	return token
}
func looksLikeScriptFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py", ".js", ".mjs", ".cjs", ".sh", ".ps1", ".bat", ".cmd":
		return true
	default:
		return false
	}
}

func writeSkillQualityStatus(skillDir string, entry *corelib.NLSkillEntry, quality skillQualityReport, stage string, requireRuntimeProof bool) {
	if strings.TrimSpace(skillDir) == "" || entry == nil {
		return
	}
	status := buildSkillQualityStatus(skillDir, entry, quality, stage, requireRuntimeProof)
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		log.Printf("[skill-lifecycle] marshal quality status for %s failed: %v", entry.Name, err)
		return
	}
	if err := os.WriteFile(filepath.Join(skillDir, "quality_status.json"), data, 0o644); err != nil {
		log.Printf("[skill-lifecycle] write quality status for %s failed: %v", entry.Name, err)
	}
}

func normalizeInstalledSkillEntry(entry *corelib.NLSkillEntry) *corelib.NLSkillEntry {
	if entry == nil || strings.TrimSpace(entry.SkillDir) == "" {
		return entry
	}
	changes, report, err := prepareSkillDirForMarket(entry.SkillDir, true)
	if err != nil {
		log.Printf("[skill-lifecycle] normalize %s failed: %v", entry.Name, err)
		return entry
	}
	if len(changes) > 0 {
		log.Printf("[skill-lifecycle] normalized %s with %d portability fix(es)", entry.Name, len(changes))
	}
	qualityEntry := entry
	if reloaded, err := loadImportedSkillEntry(entry.SkillDir); err == nil {
		qualityEntry = reloaded
	}
	quality := evaluateSkillQuality(qualityEntry, report, false)
	writeSkillQualityStatus(entry.SkillDir, qualityEntry, quality, "normalize", false)
	if report != nil && report.Summary.Errors > 0 {
		log.Printf("[skill-lifecycle] %s still has %d portability error(s) after normalization", entry.Name, report.Summary.Errors)
	}
	if reloaded, err := loadImportedSkillEntry(entry.SkillDir); err == nil {
		reloaded.Source = entry.Source
		reloaded.SourceProject = entry.SourceProject
		reloaded.HubSkillID = entry.HubSkillID
		reloaded.HubVersion = entry.HubVersion
		reloaded.TrustLevel = entry.TrustLevel
		return reloaded
	}
	return entry
}

func (a *App) normalizeInstalledSkill(entry *corelib.NLSkillEntry) *corelib.NLSkillEntry {
	if a != nil {
		a.ensureSkillLifecycleManager()
		if a.skillLifecycle != nil {
			return a.skillLifecycle.NormalizeInstalled(entry)
		}
	}
	return normalizeInstalledSkillEntry(entry)
}
