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
			if isSkillRuntimePackageDir(dirEntry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		base := dirEntry.Name()
		if isSkillRuntimePackageFile(base) {
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
	return buildSkillYAMLFileFromPackageEntry(entry)
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
		if report.Summary.Warnings > 0 {
			q.Reasons = append(q.Reasons, fmt.Sprintf("%d portability warning(s)", report.Summary.Warnings))
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
	if !normalizeSkillTypeKind(entry.Type).IsKnowledge() && len(entry.Steps) == 0 && len(entry.Pipeline) == 0 {
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
	base := strings.ToLower(filepath.Base(name))
	return strings.HasSuffix(base, ".bak") || base == "upload_status.json" || base == "quality_status.json" || base == "skill_package_manifest.json" || base == ".patches.json"
}

func isSkillRuntimePackageDir(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	switch base {
	case ".git", ".hg", ".svn", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".cache", "node_modules":
		return true
	default:
		return false
	}
}

func missingReferencedSkillFiles(skillDir string, entry *corelib.NLSkillEntry) []string {
	seen := map[string]struct{}{}
	var missing []string
	addMissing := func(ref string) {
		if ref == "" {
			return
		}
		if _, ok := seen[ref]; ok {
			return
		}
		seen[ref] = struct{}{}
		missing = append(missing, ref)
	}
	collectMissingReferencedParamDefaults(skillDir, entry.Params, addMissing)
	collectInvalidRequiredCredentialFileRefs(entry.RequiredCredentialFiles, addMissing)
	for _, step := range entry.Steps {
		collectMissingReferencedStepFiles(skillDir, step, addMissing)
	}
	for _, step := range entry.Pipeline {
		for _, ref := range packageLocalPathRefsFromStringMap(step.Params) {
			if missingRef, missing := missingPackageLocalRef(skillDir, ref, ""); missing {
				addMissing(missingRef)
			}
		}
	}
	return missing
}

func collectInvalidRequiredCredentialFileRefs(refs []string, addMissing func(string)) {
	for _, ref := range refs {
		if cleanRef, invalid := invalidPackageLocalRef(ref); invalid {
			addMissing(cleanRef)
		}
	}
}

func invalidPackageLocalRef(ref string) (string, bool) {
	cleanRef := normalizePackageRelativePath(ref)
	if cleanRef == "" {
		return "", false
	}
	if packagePathIsAbs(ref) || packagePathIsAbs(cleanRef) || strings.HasPrefix(cleanRef, "../") {
		return cleanRef, true
	}
	return "", false
}

func collectMissingReferencedParamDefaults(skillDir string, params []corelib.NLSkillParam, addMissing func(string)) {
	for _, param := range params {
		defaultValue := strings.TrimSpace(param.Default)
		if defaultValue == "" {
			continue
		}
		var refs []string
		if packageQualityShouldCheckPathKey(param.Name) {
			refs = append(refs, packageLocalPathRefsFromValue(defaultValue)...)
		} else {
			refs = append(refs, packageLocalPathRefFromString(defaultValue)...)
		}
		for _, ref := range refs {
			if missingRef, missing := missingPackageLocalRef(skillDir, ref, ""); missing {
				addMissing(missingRef)
			}
		}
	}
}

func collectMissingReferencedStepFiles(skillDir string, step corelib.NLSkillStep, addMissing func(string)) {
	collectMissingReferencedStepFilesSeen(skillDir, step, addMissing, map[*corelib.NLSkillStep]struct{}{})
}

func collectMissingReferencedStepFilesSeen(skillDir string, step corelib.NLSkillStep, addMissing func(string), seenFallbacks map[*corelib.NLSkillStep]struct{}) {
	collectMissingReferencedStepParams(skillDir, step, addMissing)
	if step.FallbackStep != nil {
		if _, seen := seenFallbacks[step.FallbackStep]; seen {
			return
		}
		seenFallbacks[step.FallbackStep] = struct{}{}
		collectMissingReferencedStepFilesSeen(skillDir, *step.FallbackStep, addMissing, seenFallbacks)
	}
}

func collectMissingReferencedStepParams(skillDir string, step corelib.NLSkillStep, addMissing func(string)) {
	workingDir, invalidWorkingDir := packageStepWorkingDirForQuality(step)
	if invalidWorkingDir != "" {
		addMissing(invalidWorkingDir)
	}
	if workingDir != "" {
		if _, err := os.Stat(filepath.Join(skillDir, filepath.FromSlash(workingDir))); err != nil {
			addMissing(workingDir)
		}
	}
	for _, command := range commandStringsForPackageRefs(step) {
		for _, ref := range localFileRefsFromCommand(command) {
			if missingRef, missing := missingPackageLocalRef(skillDir, ref, workingDir); missing {
				addMissing(missingRef)
			}
		}
	}
	for _, ref := range packageLocalPathRefsFromParams(step.Params) {
		if missingRef, missing := missingPackageLocalRef(skillDir, ref, workingDir); missing {
			addMissing(missingRef)
		}
	}
}

func missingPackageLocalRef(skillDir, ref, workingDir string) (string, bool) {
	cleanRef := normalizePackageRelativePath(ref)
	if cleanRef == "" {
		return "", false
	}
	if missingRef, invalid := invalidPackageLocalRef(ref); invalid {
		return missingRef, true
	}
	candidates := packageLocalRefCandidates(cleanRef, workingDir)
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(skillDir, filepath.FromSlash(candidate))); err == nil {
			return "", false
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	return candidates[len(candidates)-1], true
}

func packageStepWorkingDir(step corelib.NLSkillStep) string {
	wd, _ := packageStepWorkingDirForQuality(step)
	return wd
}

func packageStepWorkingDirForQuality(step corelib.NLSkillStep) (string, string) {
	if len(step.Params) == 0 {
		return "", ""
	}
	wd := normalizePackageRelativePath(firstPackageString(step.Params, "working_dir", "cwd", "workdir", "dir"))
	if wd == "" {
		return "", ""
	}
	if strings.HasPrefix(wd, "../") || packagePathIsAbs(wd) {
		return "", wd
	}
	return wd, ""
}

func packageLocalRefCandidates(ref, workingDir string) []string {
	ref = normalizePackageRelativePath(ref)
	if ref == "" || packagePathIsAbs(ref) || strings.HasPrefix(ref, "../") {
		return nil
	}
	candidates := []string{ref}
	if workingDir != "" && ref != workingDir && !strings.HasPrefix(ref, workingDir+"/") {
		candidates = append(candidates, filepath.ToSlash(filepath.Join(filepath.FromSlash(workingDir), filepath.FromSlash(ref))))
	}
	return candidates
}

func normalizePackageRelativePath(value string) string {
	value = strings.TrimSpace(value)
	value = trimPackageRefDecorations(value)
	value = strings.TrimPrefix(value, "./")
	if stripped, ok := stripPackageBaseDirRef(value); ok {
		value = stripped
	}
	if value == "" || value == "." {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(value))
}

func stripPackageBaseDirRef(value string) (string, bool) {
	for _, marker := range []string{"{baseDir}", "$BASE_DIR", "${BASE_DIR}"} {
		if value == marker {
			return "", true
		}
		if strings.HasPrefix(value, marker+"/") {
			return strings.TrimPrefix(value, marker+"/"), true
		}
		if strings.HasPrefix(value, marker+"\\") {
			return strings.TrimPrefix(value, marker+"\\"), true
		}
	}
	return value, false
}

func packagePathIsAbs(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if filepath.IsAbs(value) {
		return true
	}
	slash := filepath.ToSlash(value)
	if strings.HasPrefix(slash, "/") {
		return true
	}
	if len(slash) >= 2 && slash[1] == ':' {
		drive := slash[0]
		return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
	}
	return false
}

func commandStringsForPackageRefs(step corelib.NLSkillStep) []string {
	if len(step.Params) == 0 {
		return nil
	}
	keys := []string{"command", "cmd", "run", "script", "shell_command"}
	var commands []string
	for _, key := range keys {
		raw, ok := step.Params[key]
		if !ok || raw == nil {
			continue
		}
		commands = append(commands, commandStringsFromPackageValue(raw)...)
	}
	return commands
}

func packageLocalPathRefsFromParams(params map[string]interface{}) []string {
	if len(params) == 0 {
		return nil
	}
	var refs []string
	for key, raw := range params {
		if !packageQualityShouldCheckPathKey(key) {
			continue
		}
		refs = append(refs, packageLocalPathRefsFromValue(raw)...)
	}
	return refs
}

func packageLocalPathRefsFromStringMap(params map[string]string) []string {
	if len(params) == 0 {
		return nil
	}
	var refs []string
	for key, value := range params {
		if !packageQualityShouldCheckPathKey(key) {
			continue
		}
		refs = append(refs, packageLocalPathRefsFromValue(value)...)
	}
	return refs
}

func packageQualityShouldCheckPathKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "file", "path", "filename", "filepath":
		return true
	default:
		return strings.HasSuffix(key, "_path") ||
			strings.HasSuffix(key, "_file") ||
			strings.HasSuffix(key, "_files") ||
			strings.HasSuffix(key, "_script")
	}
}

func packageLocalPathRefsFromValue(raw interface{}) []string {
	switch v := raw.(type) {
	case string:
		return packageLocalPathRefFromString(v)
	case []string:
		var refs []string
		for _, item := range v {
			refs = append(refs, packageLocalPathRefFromString(item)...)
		}
		return refs
	case []interface{}:
		var refs []string
		for _, item := range v {
			refs = append(refs, packageLocalPathRefsFromValue(item)...)
		}
		return refs
	case map[string]interface{}:
		return packageLocalPathRefsFromParams(v)
	case map[interface{}]interface{}:
		return packageLocalPathRefsFromParams(packageInterfaceKeyMapToStringMap(v))
	case map[string]string:
		return packageLocalPathRefsFromStringMap(v)
	default:
		return nil
	}
}

func packageLocalPathRefFromString(value string) []string {
	ref := normalizePackageRelativePath(value)
	if ref == "" {
		return nil
	}
	if packagePathIsAbs(ref) || strings.HasPrefix(ref, "../") || strings.HasPrefix(ref, "./") || strings.Contains(ref, "/") || looksLikeScriptFile(ref) {
		return []string{ref}
	}
	return nil
}

func commandStringsFromPackageValue(raw interface{}) []string {
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	case []string:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := packageCommandTokenString(item); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return []string{strings.Join(parts, " ")}
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := packageCommandTokenString(item); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return []string{strings.Join(parts, " ")}
	case map[string]interface{}:
		return commandStringsFromPackageMap(v)
	case map[interface{}]interface{}:
		return commandStringsFromPackageMap(packageInterfaceKeyMapToStringMap(v))
	case map[string]string:
		converted := make(map[string]interface{}, len(v))
		for key, value := range v {
			converted[key] = value
		}
		return commandStringsFromPackageMap(converted)
	default:
		return nil
	}
}

func commandStringsFromPackageMap(m map[string]interface{}) []string {
	program := firstPackageString(m, "program", "cmd", "command", "executable", "binary")
	if program == "" {
		return nil
	}
	parts := []string{program}
	for _, key := range []string{"args", "argv", "arguments"} {
		if raw, ok := m[key]; ok && raw != nil {
			if values := commandStringsFromPackageValue(raw); len(values) > 0 {
				parts = append(parts, values...)
			}
			break
		}
	}
	return []string{strings.Join(parts, " ")}
}

func packageInterfaceKeyMapToStringMap(m map[interface{}]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for key, value := range m {
		name := strings.TrimSpace(fmt.Sprintf("%v", key))
		if name == "" || name == "<nil>" {
			continue
		}
		out[name] = value
	}
	return out
}

func firstPackageString(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if s := packageStringParam(value); s != "" {
				return s
			}
		}
	}
	return ""
}

func packageStringParam(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]interface{}:
		if len(v) == 1 {
			if raw, ok := v["baseDir"]; ok && raw == nil {
				return "{baseDir}"
			}
		}
	case map[interface{}]interface{}:
		if len(v) == 1 {
			for key, raw := range v {
				if fmt.Sprintf("%v", key) == "baseDir" && raw == nil {
					return "{baseDir}"
				}
			}
		}
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", value))
	if s == "" || s == "<nil>" {
		return ""
	}
	return s
}

func packageCommandTokenString(value interface{}) string {
	s := strings.TrimSpace(fmt.Sprintf("%v", value))
	if s == "" || s == "<nil>" {
		return ""
	}
	if strings.ContainsAny(s, " \t\r\n") && !isPackageQuotedToken(s) {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func isPackageQuotedToken(s string) bool {
	if len(s) < 2 {
		return false
	}
	return (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')
}

func localFileRefsFromCommand(command string) []string {
	var refs []string
	for _, raw := range splitPackageCommandTokens(command) {
		rawToken := strings.TrimSpace(raw)
		hasBaseDirRef := strings.Contains(rawToken, "{baseDir}") ||
			strings.Contains(rawToken, "$BASE_DIR") ||
			strings.Contains(rawToken, "${BASE_DIR}")
		token := normalizeLocalFileRefToken(raw)
		if token == "" || strings.HasPrefix(token, "$") || strings.HasPrefix(token, "%") {
			continue
		}
		if packagePathIsAbs(token) {
			refs = append(refs, token)
			continue
		}
		slashToken := filepath.ToSlash(token)
		if strings.HasPrefix(slashToken, "../") ||
			strings.HasPrefix(token, "./") ||
			strings.HasPrefix(slashToken, "scripts/") ||
			hasBaseDirRef ||
			looksLikeScriptFile(token) {
			refs = append(refs, token)
		}
	}
	return refs
}

func splitPackageCommandTokens(command string) []string {
	var tokens []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	flush := func() {
		if b.Len() == 0 {
			return
		}
		tokens = append(tokens, b.String())
		b.Reset()
	}
	for _, r := range command {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
				continue
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
				continue
			}
		}
		if !inSingle && !inDouble && (r == ' ' || r == '\t' || r == '\r' || r == '\n') {
			flush()
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return tokens
}

func normalizeLocalFileRefToken(token string) string {
	token = trimPackageRefDecorations(token)
	if idx := strings.Index(token, "="); idx >= 0 && idx+1 < len(token) {
		left := strings.TrimSpace(token[:idx])
		if strings.HasPrefix(left, "-") {
			token = token[idx+1:]
		}
	}
	token = trimPackageRefDecorations(token)
	if stripped, ok := stripPackageBaseDirRef(token); ok {
		token = stripped
	}
	return token
}

func trimPackageRefDecorations(value string) string {
	return strings.Trim(value, "\"'`;,()[]")
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
	if reloaded, err := loadMarketPackageSkillEntry(entry.SkillDir, entry); err == nil {
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
