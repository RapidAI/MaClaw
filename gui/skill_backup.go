package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/security"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

// SkillManifest is the metadata stored in manifest.json inside a skill backup zip.
type SkillManifest struct {
	BackupTime    string `json:"backup_time"`
	SkillCount    int    `json:"skill_count"`
	MaclawVersion string `json:"maclaw_version"`
}

// RestoreReport summarises the outcome of a RestoreSkills operation.
type RestoreReport struct {
	Restored int      `json:"restored"`
	Skipped  int      `json:"skipped"`
	Failed   int      `json:"failed"`
	Details  []string `json:"details"`
}

// BackupSkills serialises every registered NL Skill to JSON and writes them
// into a zip archive at outputPath.  The archive contains a manifest.json
// plus one <kebab-name>.json file per skill.
func (e *SkillExecutor) BackupSkills(outputPath string) error {
	e.mu.RLock()
	skills := e.loadSkills()
	e.mu.RUnlock()
	for _, skill := range skills {
		if err := e.scanSkillBeforeArchiveExport(skill); err != nil {
			return err
		}
	}

	return writeSkillsZipAtomic(outputPath, skills, true)

}

// ExportLearnedSkillsZip exports the specified learned/crafted skills (by name)
// to a zip archive at outputPath. Only skills where IsLearnedSource returns true
// are eligible; names that don't match are silently skipped.
func (e *SkillExecutor) ExportLearnedSkillsZip(names []string, outputPath string) error {
	if len(names) == 0 {
		return fmt.Errorf("no skill names specified")
	}

	e.mu.RLock()
	allSkills := e.loadSkills()
	e.mu.RUnlock()

	// Build set of requested names.
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}

	// Filter to learned/crafted skills that match the requested names.
	var selected []corelib.NLSkillEntry
	for _, s := range allSkills {
		if corelib.IsLearnedSource(s.Source) && wanted[s.Name] {
			selected = append(selected, s)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("no matching learned/crafted skills found")
	}
	for _, skill := range selected {
		if err := e.scanSkillBeforeArchiveExport(skill); err != nil {
			return err
		}
	}

	return writeSkillsZipAtomic(outputPath, selected, true)

}

func writeSkillsZipAtomic(outputPath string, skills []corelib.NLSkillEntry, dedupeNames bool) error {
	outFile, err := createAtomicSkillZipTemp(outputPath)
	if err != nil {
		return err
	}
	tmpPath := outFile.Name()
	committed := false
	defer func() {
		if !committed {
			_ = outFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	zw := zip.NewWriter(outFile)
	if err := writeSkillsZipContents(zw, skills, dedupeNames); err != nil {
		_ = zw.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("failed to finalize zip: %w", err)
	}
	if err := outFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync zip file: %w", err)
	}
	if err := outFile.Close(); err != nil {
		return fmt.Errorf("failed to close zip file: %w", err)
	}
	if err := replaceFileWithTemp(tmpPath, outputPath); err != nil {
		return err
	}
	committed = true
	return nil
}

func replaceFileWithTemp(tmpPath, outputPath string) error {
	if err := os.Rename(tmpPath, outputPath); err == nil {
		return nil
	} else if _, statErr := os.Stat(outputPath); os.IsNotExist(statErr) {
		return fmt.Errorf("failed to move zip into place: %w", err)
	}

	backup, err := os.CreateTemp(filepath.Dir(outputPath), "."+filepath.Base(outputPath)+"-replace-*.bak")
	if err != nil {
		return fmt.Errorf("failed to create replacement backup marker: %w", err)
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		_ = os.Remove(backupPath)
		return fmt.Errorf("failed to close replacement backup marker: %w", err)
	}
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("failed to prepare replacement backup path: %w", err)
	}

	if err := os.Rename(outputPath, backupPath); err != nil {
		return fmt.Errorf("failed to move existing zip aside: %w", err)
	}
	restored := false
	defer func() {
		if !restored {
			_ = os.Remove(backupPath)
		}
	}()
	if err := os.Rename(tmpPath, outputPath); err != nil {
		if restoreErr := os.Rename(backupPath, outputPath); restoreErr != nil {
			return fmt.Errorf("failed to move zip into place: %w; additionally failed to restore previous zip: %v", err, restoreErr)
		}
		restored = true
		return fmt.Errorf("failed to move zip into place: %w", err)
	}
	return nil
}

func createAtomicSkillZipTemp(outputPath string) (*os.File, error) {
	if strings.TrimSpace(outputPath) == "" {
		return nil, fmt.Errorf("output path is required")
	}
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}
	base := filepath.Base(outputPath)
	return os.CreateTemp(dir, "."+base+"-*.tmp")
}

func writeSkillsZipContents(zw *zip.Writer, skills []corelib.NLSkillEntry, dedupeNames bool) error {
	manifest := SkillManifest{
		BackupTime:    time.Now().Format(time.RFC3339),
		SkillCount:    len(skills),
		MaclawVersion: remoteAppVersion(),
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	mw, err := zw.Create("manifest.json")
	if err != nil {
		return fmt.Errorf("failed to create manifest entry in zip: %w", err)
	}
	if _, err := mw.Write(manifestData); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	usedNames := make(map[string]bool, len(skills))
	for _, skill := range skills {
		data, err := json.MarshalIndent(skill, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal skill %q: %w", skill.Name, err)
		}
		fileName := toKebabCase(skill.Name) + ".json"
		if dedupeNames {
			for usedNames[fileName] {
				fileName = toKebabCase(skill.Name) + "-" + fmt.Sprintf("%d", len(usedNames)) + ".json"
			}
		}
		usedNames[fileName] = true
		sw, err := zw.Create(fileName)
		if err != nil {
			return fmt.Errorf("failed to create zip entry for skill %q: %w", skill.Name, err)
		}
		if _, err := sw.Write(data); err != nil {
			return fmt.Errorf("failed to write skill %q: %w", skill.Name, err)
		}
	}
	return nil
}

func (e *SkillExecutor) scanSkillBeforeArchiveExport(entry corelib.NLSkillEntry) error {
	if e != nil && e.app != nil && e.app.isRiskGuardrailOffMode() {
		return nil
	}
	report := cskill.NewSecurityScanner(nil).ScanInstallStaged(context.Background(), &entry, entry.SkillDir, nil)
	if report == nil {
		if e != nil && e.app != nil && !e.app.skillInstallMissingScanShouldBlock() {
			return nil
		}
		return fmt.Errorf("skill export security scan produced no report")
	}
	if e != nil && e.app != nil {
		if e.app.skillInstallScanShouldBlock(report) {
			return fmt.Errorf("skill export blocked by security scan: %s level=%s summary=%s", entry.Name, report.FinalLevel, report.Summary)
		}
		return nil
	}
	return nil
}

// RestoreSkills reads a skill backup zip from zipPath and restores the
// contained skills.  Skills whose name already exists locally are skipped
// and marked as "skipped (duplicate)" in the report.
func (e *SkillExecutor) RestoreSkills(zipPath string) (*RestoreReport, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("invalid zip file: %w", err)
	}
	defer zr.Close()

	// Locate manifest.json
	var manifestFile *zip.File
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			manifestFile = f
			break
		}
	}
	if manifestFile == nil {
		return nil, fmt.Errorf("invalid backup: manifest.json not found in zip")
	}

	// Parse manifest (validate it is well-formed JSON)
	mrc, err := manifestFile.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest.json: %w", err)
	}
	var manifest SkillManifest
	if err := json.NewDecoder(mrc).Decode(&manifest); err != nil {
		mrc.Close()
		return nil, fmt.Errorf("failed to parse manifest.json: %w", err)
	}
	mrc.Close()

	// Build a set of existing skill names for duplicate detection
	e.mu.Lock()
	defer e.mu.Unlock()

	existingSkills := e.loadSkills()
	existingNames := make(map[string]bool, len(existingSkills))
	for _, s := range existingSkills {
		existingNames[s.Name] = true
	}

	report := &RestoreReport{}

	// Process each skill file (skip manifest.json)
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			continue
		}
		if !strings.HasSuffix(f.Name, ".json") {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			report.Failed++
			report.Details = append(report.Details, fmt.Sprintf("%s: failed to open - %v", f.Name, err))
			continue
		}

		data, err := io.ReadAll(io.LimitReader(rc, 10*1024*1024)) // 10MB per skill max
		rc.Close()
		if err != nil {
			report.Failed++
			report.Details = append(report.Details, fmt.Sprintf("%s: failed to read - %v", f.Name, err))
			continue
		}

		var skill corelib.NLSkillEntry
		if err := json.Unmarshal(data, &skill); err != nil {
			report.Failed++
			report.Details = append(report.Details, fmt.Sprintf("%s: invalid JSON - %v", f.Name, err))
			continue
		}

		if strings.TrimSpace(skill.Name) == "" {
			report.Failed++
			report.Details = append(report.Details, fmt.Sprintf("%s: missing skill name", f.Name))
			continue
		}

		if isShellBrowserAutomationSkillEntry(skill) {
			report.Failed++
			report.Details = append(report.Details, fmt.Sprintf("%s: %s", skill.Name, browserAutomationSkillRejectedError(skill.Name)))
			continue
		}

		if existingNames[skill.Name] {
			report.Skipped++
			report.Details = append(report.Details, fmt.Sprintf("%s: skipped (duplicate)", skill.Name))
			continue
		}

		skippedRiskScan := e.app != nil && e.app.isRiskGuardrailOffMode()
		var scanReport *cskill.ScanReport
		if !skippedRiskScan {
			scanReport = cskill.NewSecurityScanner(nil).ScanInstallStaged(context.Background(), &skill, skill.SkillDir, nil)
		}
		missingScanBlocked := scanReport == nil && (e.app == nil || e.app.skillInstallMissingScanShouldBlock())
		riskyScanBlocked := scanReport != nil && e.app != nil && e.app.skillInstallScanShouldBlock(scanReport)
		legacyRiskyScanBlocked := false
		if missingScanBlocked || riskyScanBlocked || legacyRiskyScanBlocked {
			report.Failed++
			level := security.RiskCritical
			summary := "security scan unavailable"
			if scanReport != nil {
				level = scanReport.FinalLevel
				summary = scanReport.Summary
			}
			report.Details = append(report.Details, fmt.Sprintf("%s: blocked by security scan (level=%s): %s", skill.Name, level, summary))
			continue
		}
		if scanReport == nil && !skippedRiskScan {
			report.Details = append(report.Details, fmt.Sprintf("%s: security scan unavailable; restored by current policy", skill.Name))
		} else if scanReport != nil && scanReport.NeedsUserReview() {
			report.Details = append(report.Details, fmt.Sprintf("%s: security scan recorded risk (level=%s); restored by current policy", skill.Name, scanReport.FinalLevel))
		}

		existingSkills = append(existingSkills, skill)
		existingNames[skill.Name] = true
		report.Restored++
		report.Details = append(report.Details, fmt.Sprintf("%s: restored", skill.Name))
	}

	if report.Restored > 0 {
		if err := e.saveSkills(existingSkills); err != nil {
			return nil, fmt.Errorf("failed to persist restored skills: %w", err)
		}
	}

	return report, nil
}

// SerializeSkill serialises an NLSkillEntry to JSON bytes.
// It returns an error if the required fields (name or steps) are missing.
func SerializeSkill(skill corelib.NLSkillEntry) ([]byte, error) {
	if strings.TrimSpace(skill.Name) == "" {
		return nil, fmt.Errorf("serialize skill: name is required")
	}
	if len(skill.Steps) == 0 {
		return nil, fmt.Errorf("serialize skill: steps are required")
	}
	data, err := json.Marshal(skill)
	if err != nil {
		return nil, fmt.Errorf("serialize skill: %w", err)
	}
	return data, nil
}

// DeserializeSkill parses JSON bytes into an NLSkillEntry.
// It returns an error if the JSON is invalid or required fields (name or steps) are missing.
func DeserializeSkill(data []byte) (corelib.NLSkillEntry, error) {
	var skill corelib.NLSkillEntry
	if err := json.Unmarshal(data, &skill); err != nil {
		return corelib.NLSkillEntry{}, fmt.Errorf("deserialize skill: invalid JSON - %w", err)
	}
	if strings.TrimSpace(skill.Name) == "" {
		return corelib.NLSkillEntry{}, fmt.Errorf("deserialize skill: name is required")
	}
	if len(skill.Steps) == 0 {
		return corelib.NLSkillEntry{}, fmt.Errorf("deserialize skill: steps are required")
	}
	return skill, nil
}

// toKebabCase converts a string to kebab-case for use as a filename.
// It lowercases the input, replaces spaces and underscores with hyphens,
// strips non-alphanumeric/hyphen characters, and collapses multiple hyphens.
// kebabNonAlnum and kebabMultiDash are compiled once to avoid repeated
// regexp compilation on every call to toKebabCase.
var (
	kebabNonAlnum  = regexp.MustCompile(`[^a-z0-9\-]`)
	kebabMultiDash = regexp.MustCompile(`-{2,}`)
)

func toKebabCase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = kebabNonAlnum.ReplaceAllString(s, "")
	s = kebabMultiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "skill"
	}
	return s
}
