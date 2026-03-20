package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
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

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}

	zw := zip.NewWriter(outFile)

	// Write manifest.json
	manifest := SkillManifest{
		BackupTime:    time.Now().Format(time.RFC3339),
		SkillCount:    len(skills),
		MaclawVersion: remoteAppVersion(),
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		zw.Close()
		outFile.Close()
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	mw, err := zw.Create("manifest.json")
	if err != nil {
		zw.Close()
		outFile.Close()
		return fmt.Errorf("failed to create manifest entry in zip: %w", err)
	}
	if _, err := mw.Write(manifestData); err != nil {
		zw.Close()
		outFile.Close()
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	// Write each skill as an individual JSON file
	for _, skill := range skills {
		data, err := json.MarshalIndent(skill, "", "  ")
		if err != nil {
			zw.Close()
			outFile.Close()
			return fmt.Errorf("failed to marshal skill %q: %w", skill.Name, err)
		}
		fileName := toKebabCase(skill.Name) + ".json"
		sw, err := zw.Create(fileName)
		if err != nil {
			zw.Close()
			outFile.Close()
			return fmt.Errorf("failed to create zip entry for skill %q: %w", skill.Name, err)
		}
		if _, err := sw.Write(data); err != nil {
			zw.Close()
			outFile.Close()
			return fmt.Errorf("failed to write skill %q: %w", skill.Name, err)
		}
	}

	// Close the zip writer first (writes central directory), then the file.
	if err := zw.Close(); err != nil {
		outFile.Close()
		return fmt.Errorf("failed to finalize zip: %w", err)
	}
	return outFile.Close()
}

// ExportLearnedSkillsZip exports the specified learned/crafted skills (by name)
// to a zip archive at outputPath. Only skills with source "learned" or "crafted"
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
	var selected []NLSkillEntry
	for _, s := range allSkills {
		if (s.Source == "learned" || s.Source == "crafted") && wanted[s.Name] {
			selected = append(selected, s)
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("no matching learned/crafted skills found")
	}

	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create export file: %w", err)
	}

	zw := zip.NewWriter(outFile)

	manifest := SkillManifest{
		BackupTime:    time.Now().Format(time.RFC3339),
		SkillCount:    len(selected),
		MaclawVersion: remoteAppVersion(),
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		zw.Close()
		outFile.Close()
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}
	mw, err := zw.Create("manifest.json")
	if err != nil {
		zw.Close()
		outFile.Close()
		return fmt.Errorf("failed to create manifest entry: %w", err)
	}
	if _, err := mw.Write(manifestData); err != nil {
		zw.Close()
		outFile.Close()
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	usedNames := make(map[string]bool, len(selected))
	for _, skill := range selected {
		data, err := json.MarshalIndent(skill, "", "  ")
		if err != nil {
			zw.Close()
			outFile.Close()
			return fmt.Errorf("failed to marshal skill %q: %w", skill.Name, err)
		}
		fileName := toKebabCase(skill.Name) + ".json"
		// Deduplicate file names within the zip (different skill names may
		// produce the same kebab-case, e.g. "my skill" vs "my-skill").
		if usedNames[fileName] {
			fileName = toKebabCase(skill.Name) + "-" + fmt.Sprintf("%d", len(usedNames)) + ".json"
		}
		usedNames[fileName] = true
		sw, err := zw.Create(fileName)
		if err != nil {
			zw.Close()
			outFile.Close()
			return fmt.Errorf("failed to create zip entry for %q: %w", skill.Name, err)
		}
		if _, err := sw.Write(data); err != nil {
			zw.Close()
			outFile.Close()
			return fmt.Errorf("failed to write skill %q: %w", skill.Name, err)
		}
	}

	if err := zw.Close(); err != nil {
		outFile.Close()
		return fmt.Errorf("failed to finalize zip: %w", err)
	}
	return outFile.Close()
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
			report.Details = append(report.Details, fmt.Sprintf("%s: failed to open — %v", f.Name, err))
			continue
		}

		data, err := io.ReadAll(io.LimitReader(rc, 10*1024*1024)) // 10MB per skill max
		rc.Close()
		if err != nil {
			report.Failed++
			report.Details = append(report.Details, fmt.Sprintf("%s: failed to read — %v", f.Name, err))
			continue
		}

		var skill NLSkillEntry
		if err := json.Unmarshal(data, &skill); err != nil {
			report.Failed++
			report.Details = append(report.Details, fmt.Sprintf("%s: invalid JSON — %v", f.Name, err))
			continue
		}

		if strings.TrimSpace(skill.Name) == "" {
			report.Failed++
			report.Details = append(report.Details, fmt.Sprintf("%s: missing skill name", f.Name))
			continue
		}

		if existingNames[skill.Name] {
			report.Skipped++
			report.Details = append(report.Details, fmt.Sprintf("%s: skipped (duplicate)", skill.Name))
			continue
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
func SerializeSkill(skill NLSkillEntry) ([]byte, error) {
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
func DeserializeSkill(data []byte) (NLSkillEntry, error) {
	var skill NLSkillEntry
	if err := json.Unmarshal(data, &skill); err != nil {
		return NLSkillEntry{}, fmt.Errorf("deserialize skill: invalid JSON — %w", err)
	}
	if strings.TrimSpace(skill.Name) == "" {
		return NLSkillEntry{}, fmt.Errorf("deserialize skill: name is required")
	}
	if len(skill.Steps) == 0 {
		return NLSkillEntry{}, fmt.Errorf("deserialize skill: steps are required")
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
