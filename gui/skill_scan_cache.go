package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/fileutil"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

const (
	skillScanCacheFileName       = ".maclaw_scan_status.json"
	skillScanCacheScannerVersion = "2026-05-12.3"
	skillScanCacheSigningKeyName = "skill_scan_cache_hmac.key"
)

type skillScanCacheRecord struct {
	SkillName      string               `json:"skill_name"`
	Hash           string               `json:"hash"`
	ScannerVersion string               `json:"scanner_version,omitempty"`
	Status         skillScanCacheStatus `json:"status"`
	Level          string               `json:"level,omitempty"`
	Summary        string               `json:"summary,omitempty"`
	ScannedBy      string               `json:"scanned_by,omitempty"`
	ScannedAt      string               `json:"scanned_at"`
	Signature      string               `json:"signature,omitempty"`
}

func (r *SkillRunner) ensureSkillSecurityScanned(skill *corelib.NLSkillEntry) error {
	if r == nil || r.executor == nil || r.executor.app == nil || skill == nil {
		return nil
	}
	app := r.executor.app
	if app.policyEngine != nil && app.policyEngine.IsDeveloperMode() {
		return nil
	}
	if app.securityPolicyMode() == "none" || app.securityPolicyMode() == "relaxed" {
		return nil
	}
	if strings.TrimSpace(skill.SkillDir) == "" {
		return nil
	}
	hash, err := skillContentHash(skill)
	if err != nil {
		return fmt.Errorf("skill security scan hash failed for %q: %w", skill.Name, err)
	}
	if rec, err := readSkillScanCache(skill.SkillDir, skill.Name); err == nil && rec.Hash == hash && skillScanCacheRecordVersionMatches(rec) && skillScanCacheRecordSignatureValid(rec) {
		switch status := normalizeSkillScanCacheStatus(rec.Status); {
		case skillScanCacheRecordIsCritical(rec) && app.securityPolicyMode() == "strict":
			return fmt.Errorf("skill %q was previously blocked by security scan (level=%s): %s", skill.Name, rec.Level, rec.Summary)
		case status.IsAllowed():
			return nil
		case status.IsBlocked() && app.securityPolicyMode() == "strict":
			return fmt.Errorf("skill %q was previously blocked by security scan (level=%s): %s", skill.Name, rec.Level, rec.Summary)
		}
	}

	// Runtime is only a safety net for legacy or locally modified skills.
	// Pre-install scanning writes .maclaw_scan_status.json, so normal execution
	// avoids LLM-backed scans and only pays the hash/cache check cost.
	// Uses ScanInstallStaged (community trust) for conservative assessment:
	// if content changed since install, we don't trust the skill's claimed
	// trust level. The safe-tool category check in AssessSkill (which runs
	// AFTER trust escalation) will cap safe skills (weather, pdf, etc.) at
	// medium regardless of community escalation.
	scanner := cskill.NewSecurityScanner(nil)
	report := scanner.ScanInstallStaged(context.Background(), skill, skill.SkillDir, func(status string) {
		app.log(fmt.Sprintf("[skill-runner] security scan %s: %s", skill.Name, status))
	})
	if report == nil {
		if !app.skillInstallMissingScanShouldBlock() {
			app.log(fmt.Sprintf("[skill-runner] security scan %s produced no report; current policy allows execution", skill.Name))
			return nil
		}
		return fmt.Errorf("skill %q security scan produced no report", skill.Name)
	}
	if err := writeSkillScanCacheForReport(skill, skill.SkillDir, hash, report); err != nil {
		app.log(fmt.Sprintf("[skill-runner] failed to write scan cache for %s: %v", skill.Name, err))
	}
	if app.skillInstallScanShouldBlock(report) {
		return fmt.Errorf("skill %q blocked by security scan (level=%s): %s", skill.Name, report.FinalLevel, report.Summary)
	}
	return nil
}

func skillScanCacheRecordIsCritical(rec skillScanCacheRecord) bool {
	return strings.EqualFold(strings.TrimSpace(rec.Level), string(security.RiskCritical))
}

func skillScanCacheRecordVersionMatches(rec skillScanCacheRecord) bool {
	return strings.TrimSpace(rec.ScannerVersion) == skillScanCacheScannerVersion
}

func skillScanCacheRecordSignatureValid(rec skillScanCacheRecord) bool {
	expected, err := signSkillScanCacheRecord(rec)
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(strings.TrimSpace(rec.Signature)), []byte(expected))
}

func writeSkillScanCacheForReport(skill *corelib.NLSkillEntry, skillDir, hash string, report *cskill.ScanReport) error {
	status := skillScanCacheStatusAllowed
	if report != nil && (report.IsDangerous() || report.NeedsUserReview()) {
		status = skillScanCacheStatusBlocked
	}
	return writeSkillScanCacheForReportStatus(skill, skillDir, hash, report, status)
}

func writeSkillScanCacheForReportStatus(skill *corelib.NLSkillEntry, skillDir, hash string, report *cskill.ScanReport, status skillScanCacheStatus) error {
	if skill == nil || strings.TrimSpace(skillDir) == "" || report == nil {
		return nil
	}
	status = normalizeSkillScanCacheStatus(status)
	if status == skillScanCacheStatusUnknown {
		status = skillScanCacheStatusBlocked
	}
	if report.IsDangerous() && status == skillScanCacheStatusUnknown {
		status = skillScanCacheStatusBlocked
	}
	if hash == "" {
		var err error
		cp := *skill
		cp.SkillDir = skillDir
		hash, err = skillContentHash(&cp)
		if err != nil {
			return err
		}
	}
	rec := skillScanCacheRecord{
		SkillName:      skill.Name,
		Hash:           hash,
		ScannerVersion: skillScanCacheScannerVersion,
		Status:         status,
		Level:          fmt.Sprint(report.FinalLevel),
		Summary:        report.Summary,
		ScannedBy:      report.ScannedBy,
		ScannedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	return writeSkillScanCache(skillDir, skill.Name, rec)
}

func skillContentHash(skill *corelib.NLSkillEntry) (string, error) {
	if skill == nil {
		return "", fmt.Errorf("nil skill")
	}
	h := sha256.New()
	meta := struct {
		Name                    string                     `json:"name"`
		DirName                 string                     `json:"dir_name,omitempty"`
		Description             string                     `json:"description"`
		Triggers                []string                   `json:"triggers,omitempty"`
		Type                    string                     `json:"type,omitempty"`
		Content                 string                     `json:"content,omitempty"`
		Platforms               []string                   `json:"platforms,omitempty"`
		RequiresGUI             bool                       `json:"requires_gui,omitempty"`
		RequiresTools           []string                   `json:"requires_tools,omitempty"`
		FallbackForTools        []string                   `json:"fallback_for_tools,omitempty"`
		RequiresToolsets        []string                   `json:"requires_toolsets,omitempty"`
		FallbackForToolsets     []string                   `json:"fallback_for_toolsets,omitempty"`
		RequiredCredentialFiles []string                   `json:"required_credential_files,omitempty"`
		RequiresPython          []string                   `json:"requires_python,omitempty"`
		RequiresNode            []string                   `json:"requires_node,omitempty"`
		RequiresBins            []string                   `json:"requires_bins,omitempty"`
		RequiredEnv             []string                   `json:"required_env,omitempty"`
		PreferredShell          string                     `json:"preferred_shell,omitempty"`
		Mode                    string                     `json:"mode,omitempty"`
		ExecMode                string                     `json:"exec_mode,omitempty"`
		GlobalTimeout           int                        `json:"global_timeout,omitempty"`
		ProducesArtifact        bool                       `json:"produces_artifact"`
		Operations              []corelib.NLSkillOperation `json:"operations,omitempty"`
		Params                  []corelib.NLSkillParam     `json:"params,omitempty"`
		RequiredArgs            []string                   `json:"required_args,omitempty"`
		Steps                   []corelib.NLSkillStep      `json:"steps,omitempty"`
	}{
		Name:                    skill.Name,
		DirName:                 skill.DirName,
		Description:             skill.Description,
		Triggers:                skill.Triggers,
		Type:                    skill.Type,
		Content:                 skill.Content,
		Platforms:               skill.Platforms,
		RequiresGUI:             skill.RequiresGUI,
		RequiresTools:           skill.RequiresTools,
		FallbackForTools:        skill.FallbackForTools,
		RequiresToolsets:        skill.RequiresToolsets,
		FallbackForToolsets:     skill.FallbackForToolsets,
		RequiredCredentialFiles: skill.RequiredCredentialFiles,
		RequiresPython:          skill.RequiresPython,
		RequiresNode:            skill.RequiresNode,
		RequiresBins:            skill.RequiresBins,
		RequiredEnv:             skill.RequiredEnv,
		PreferredShell:          skill.PreferredShell,
		Mode:                    skill.Mode,
		ExecMode:                skill.ExecMode,
		GlobalTimeout:           skill.GlobalTimeout,
		ProducesArtifact:        skill.ProducesArtifact,
		Operations:              skill.Operations,
		Params:                  skill.Params,
		RequiredArgs:            skill.RequiredArgs,
		Steps:                   skill.Steps,
	}
	metaData, _ := json.Marshal(meta)
	h.Write(metaData)
	if strings.TrimSpace(skill.SkillDir) == "" {
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	root, err := filepath.Abs(skill.SkillDir)
	if err != nil {
		return "", err
	}
	includeRuntimeArtifacts := skillReferencesRuntimeArtifacts(skill)
	var files []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		base := filepath.Base(path)
		if info.IsDir() {
			if path != root && !includeRuntimeArtifacts && isSkillRuntimePackageDir(base) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if base == skillScanCacheFileName || (!includeRuntimeArtifacts && isSkillRuntimePackageFile(base)) {
			return nil
		}
		files = append(files, rel)
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(files)
	for _, rel := range files {
		full := filepath.Join(root, rel)
		info, err := os.Lstat(full)
		if err != nil {
			return "", err
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(full)
			h.Write([]byte("symlink:" + target))
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return "", err
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func skillReferencesRuntimeArtifacts(skill *corelib.NLSkillEntry) bool {
	if skill == nil {
		return false
	}
	for _, step := range skill.Steps {
		if textReferencesRuntimeArtifacts(step.Action) {
			return true
		}
		for _, value := range step.Params {
			if valueReferencesRuntimeArtifacts(value) {
				return true
			}
		}
	}
	return false
}

func valueReferencesRuntimeArtifacts(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return textReferencesRuntimeArtifacts(v)
	case []string:
		for _, item := range v {
			if textReferencesRuntimeArtifacts(item) {
				return true
			}
		}
	case []interface{}:
		for _, item := range v {
			if valueReferencesRuntimeArtifacts(item) {
				return true
			}
		}
	case map[string]string:
		for key, item := range v {
			if textReferencesRuntimeArtifacts(key) || textReferencesRuntimeArtifacts(item) {
				return true
			}
		}
	case map[string]interface{}:
		for key, item := range v {
			if textReferencesRuntimeArtifacts(key) || valueReferencesRuntimeArtifacts(item) {
				return true
			}
		}
	}
	return false
}

func textReferencesRuntimeArtifacts(text string) bool {
	lower := strings.ToLower(text)
	for _, token := range []string{"node_modules", "__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".cache"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	for _, token := range []string{"upload_status.json", "quality_status.json", "skill_package_manifest.json", ".patches.json"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func readSkillScanCache(skillDir, skillName string) (skillScanCacheRecord, error) {
	cachePath := skillScanCachePath(skillDir, skillName)
	if err := validateSkillScanCachePathForRead(cachePath); err != nil {
		return skillScanCacheRecord{}, err
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return skillScanCacheRecord{}, err
	}
	var rec skillScanCacheRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return skillScanCacheRecord{}, err
	}
	return rec, nil
}

func writeSkillScanCache(skillDir, skillName string, rec skillScanCacheRecord) error {
	cachePath := skillScanCachePath(skillDir, skillName)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	if err := validateSkillScanCachePathForWrite(cachePath); err != nil {
		return err
	}
	signature, err := signSkillScanCacheRecord(rec)
	if err != nil {
		return err
	}
	rec.Signature = signature
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(cachePath, data, 0o600)
}

func validateSkillScanCachePathForRead(cachePath string) error {
	info, err := os.Lstat(cachePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("skill scan cache is a symlink: %s", cachePath)
	}
	if info.IsDir() {
		return fmt.Errorf("skill scan cache is a directory: %s", cachePath)
	}
	return nil
}

func validateSkillScanCachePathForWrite(cachePath string) error {
	info, err := os.Lstat(cachePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write skill scan cache through symlink: %s", cachePath)
	}
	if info.IsDir() {
		return fmt.Errorf("refusing to write skill scan cache over directory: %s", cachePath)
	}
	return nil
}

func skillScanCachePath(skillDir, skillName string) string {
	return filepath.Join(skillDir, skillScanCacheFileName)
}

func signSkillScanCacheRecord(rec skillScanCacheRecord) (string, error) {
	key, err := skillScanCacheSigningKey()
	if err != nil {
		return "", err
	}
	rec.Signature = ""
	data, err := json.Marshal(rec)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func skillScanCacheSigningKey() ([]byte, error) {
	path := skillScanCacheSigningKeyPath()
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("skill scan cache signing key is a symlink: %s", path)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("skill scan cache signing key is a directory: %s", path)
		}
		key, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(key) < 32 {
			return nil, fmt.Errorf("skill scan cache signing key is too short")
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := fileutil.AtomicWriteFile(path, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return nil, err
	}
	return []byte(hex.EncodeToString(key)), nil
}

func skillScanCacheSigningKeyPath() string {
	return filepath.Join(corelib.MaclawBaseDir(), "data", skillScanCacheSigningKeyName)
}
