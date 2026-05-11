package main

import (
	"context"
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
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

const skillScanCacheFileName = ".maclaw_scan_status.json"

type skillScanCacheRecord struct {
	SkillName string               `json:"skill_name"`
	Hash      string               `json:"hash"`
	Status    skillScanCacheStatus `json:"status"`
	Level     string               `json:"level,omitempty"`
	Summary   string               `json:"summary,omitempty"`
	ScannedBy string               `json:"scanned_by,omitempty"`
	ScannedAt string               `json:"scanned_at"`
}

func (r *SkillRunner) ensureSkillSecurityScanned(skill *corelib.NLSkillEntry) error {
	if r == nil || r.executor == nil || r.executor.app == nil || skill == nil {
		return nil
	}
	app := r.executor.app
	if app.policyEngine != nil && app.policyEngine.IsDeveloperMode() {
		return nil
	}
	if strings.TrimSpace(skill.SkillDir) == "" {
		return nil
	}
	hash, err := skillContentHash(skill)
	if err != nil {
		return fmt.Errorf("skill security scan hash failed for %q: %w", skill.Name, err)
	}
	if rec, err := readSkillScanCache(skill.SkillDir, skill.Name); err == nil && rec.Hash == hash {
		switch status := normalizeSkillScanCacheStatus(rec.Status); {
		case status.IsAllowed():
			return nil
		case status.IsBlocked():
			return fmt.Errorf("skill %q was previously blocked by security scan (level=%s): %s", skill.Name, rec.Level, rec.Summary)
		}
	}

	scanner := NewSkillSecurityScanner(app, nil)
	report := scanner.ScanStaged(context.Background(), skill, skill.SkillDir, func(status string) {
		app.log(fmt.Sprintf("[skill-runner] security scan %s: %s", skill.Name, status))
	})
	if err := writeSkillScanCacheForReport(skill, skill.SkillDir, hash, report); err != nil {
		app.log(fmt.Sprintf("[skill-runner] failed to write scan cache for %s: %v", skill.Name, err))
	}
	if report.IsDangerous() || report.NeedsUserReview() {
		return fmt.Errorf("skill %q blocked by security scan (level=%s): %s", skill.Name, report.FinalLevel, report.Summary)
	}
	return nil
}

func writeSkillScanCacheForReport(skill *corelib.NLSkillEntry, skillDir, hash string, report *cskill.ScanReport) error {
	if skill == nil || strings.TrimSpace(skillDir) == "" || report == nil {
		return nil
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
	status := skillScanCacheStatusAllowed
	if report.IsDangerous() || report.NeedsUserReview() {
		status = skillScanCacheStatusBlocked
	}
	rec := skillScanCacheRecord{
		SkillName: skill.Name,
		Hash:      hash,
		Status:    status,
		Level:     fmt.Sprint(report.FinalLevel),
		Summary:   report.Summary,
		ScannedBy: report.ScannedBy,
		ScannedAt: time.Now().UTC().Format(time.RFC3339),
	}
	return writeSkillScanCache(skillDir, skill.Name, rec)
}

func skillContentHash(skill *corelib.NLSkillEntry) (string, error) {
	if skill == nil {
		return "", fmt.Errorf("nil skill")
	}
	h := sha256.New()
	meta := struct {
		Name        string                `json:"name"`
		Description string                `json:"description"`
		Triggers    []string              `json:"triggers,omitempty"`
		Platforms   []string              `json:"platforms,omitempty"`
		Steps       []corelib.NLSkillStep `json:"steps,omitempty"`
	}{Name: skill.Name, Description: skill.Description, Triggers: skill.Triggers, Platforms: skill.Platforms, Steps: skill.Steps}
	metaData, _ := json.Marshal(meta)
	h.Write(metaData)
	if strings.TrimSpace(skill.SkillDir) == "" {
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	root, err := filepath.Abs(skill.SkillDir)
	if err != nil {
		return "", err
	}
	var files []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if filepath.Base(rel) == skillScanCacheFileName {
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

func readSkillScanCache(skillDir, skillName string) (skillScanCacheRecord, error) {
	data, err := os.ReadFile(skillScanCachePath(skillDir, skillName))
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
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cachePath, data, 0o600)
}

func skillScanCachePath(skillDir, skillName string) string {
	return filepath.Join(skillDir, skillScanCacheFileName)
}
