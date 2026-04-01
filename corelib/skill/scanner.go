package skill

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"gopkg.in/yaml.v3"
)

// MigrateSkillsDir moves ~/.maclaw/skills to ~/.maclaw/data/skills if the old
// directory exists and the new one does not. This is called once at startup to
// consolidate data under ~/.maclaw/data/ for easier backup.
func MigrateSkillsDir() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	oldDir := filepath.Join(home, ".maclaw", "skills")
	newDir := filepath.Join(home, ".maclaw", "data", "skills")

	// Only migrate when old exists and new does not.
	if _, err := os.Stat(oldDir); err != nil {
		return // old dir doesn't exist, nothing to migrate
	}
	if _, err := os.Stat(newDir); err == nil {
		// Both exist — log a hint so the user knows to clean up.
		log.Printf("[skill-scanner] migrate: both %s and %s exist; using new path, consider removing old one", oldDir, newDir)
		return
	}

	// Ensure parent exists.
	if err := os.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
		log.Printf("[skill-scanner] migrate: cannot create %s: %v", filepath.Dir(newDir), err)
		return
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		log.Printf("[skill-scanner] migrate: rename %s → %s failed: %v", oldDir, newDir, err)
	} else {
		log.Printf("[skill-scanner] migrated skills: %s → %s", oldDir, newDir)
	}
}

// SkillScanRoots returns all directories that should be scanned for
// file-based skills, in priority order (first wins on name conflict):
//   1. ~/.maclaw/data/skills  (canonical location)
//   2. ~/.agents/skills
func SkillScanRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".maclaw", "data", "skills"),
		filepath.Join(home, ".agents", "skills"),
	}
}

// PrimarySkillsDir returns the canonical skills directory (~/.maclaw/data/skills).
// Callers that need to write new skills should use this path.
func PrimarySkillsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".maclaw", "data", "skills"), nil
}

// SkillScanRootsWithExternal returns SkillScanRoots() plus the given
// external directories appended at the end (lower priority).
// Duplicates of built-in roots are silently skipped.
func SkillScanRootsWithExternal(externalDirs []string) []string {
	roots := SkillScanRoots()
	seen := make(map[string]bool, len(roots))
	for _, r := range roots {
		seen[filepath.Clean(r)] = true
	}
	for _, d := range externalDirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		cleaned := filepath.Clean(d)
		if !seen[cleaned] {
			roots = append(roots, cleaned)
			seen[cleaned] = true
		}
	}
	return roots
}

// ScanAllSkillDirsWithExternal scans built-in roots plus external directories.
func ScanAllSkillDirsWithExternal(externalDirs []string) []corelib.NLSkillEntry {
	roots := SkillScanRootsWithExternal(externalDirs)
	seen := make(map[string]bool)
	var result []corelib.NLSkillEntry
	for _, root := range roots {
		skills := ScanSkillDir(root)
		for _, s := range skills {
			if !seen[s.Name] {
				result = append(result, s)
				seen[s.Name] = true
			}
		}
	}
	return result
}

// ValidateExternalSkillDir checks whether a directory is a valid skill
// directory (contains at least one subdirectory with skill.md, skill.yaml,
// or skill.yml). Returns the count of valid skill subdirectories and an error
// if the directory is not usable.
func ValidateExternalSkillDir(dir string) (int, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return 0, fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("path is not a directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("cannot read directory: %w", err)
	}
	count := 0
	for _, entry := range entries {
		subInfo, err := os.Stat(filepath.Join(dir, entry.Name()))
		if err != nil || !subInfo.IsDir() {
			continue
		}
		subDir := filepath.Join(dir, entry.Name())
		// Check for skill.yaml/yml (parseable by ScanSkillDir) first,
		// then fall back to skill.md as a valid marker.
		for _, name := range []string{"skill.yaml", "skill.yml", "skill.md"} {
			if _, err := os.Stat(filepath.Join(subDir, name)); err == nil {
				count++
				break
			}
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("no valid skill subdirectories found (need skill.md or skill.yaml)")
	}
	return count, nil
}

// ScanAllSkillDirs scans all known skill directories and returns
// deduplicated NLSkillEntry list. Earlier roots have higher priority.
func ScanAllSkillDirs() []corelib.NLSkillEntry {
	roots := SkillScanRoots()
	seen := make(map[string]bool)
	var result []corelib.NLSkillEntry
	for _, root := range roots {
		skills := ScanSkillDir(root)
		for _, s := range skills {
			if !seen[s.Name] {
				result = append(result, s)
				seen[s.Name] = true
			}
		}
	}
	return result
}

// SkillYAMLFile is the on-disk YAML format for a skill definition.
type SkillYAMLFile struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Triggers    []string        `yaml:"triggers"`
	Steps       []SkillYAMLStep `yaml:"steps"`
	Status      string          `yaml:"status"`
	Platforms   []string        `yaml:"platforms"`
	RequiresGUI bool            `yaml:"requires_gui"`
}

// SkillYAMLStep is a single step in a YAML skill definition.
type SkillYAMLStep struct {
	Action  string                 `yaml:"action"`
	Params  map[string]interface{} `yaml:"params"`
	OnError string                 `yaml:"on_error"`
}

// uploadStatusFile mirrors the GUI-side upload_status.json format.
type uploadStatusFile struct {
	SubmissionID string `json:"submission_id"`
}

// ScanSkillDir scans a single directory for skill.yaml / skill.yml files
// in immediate subdirectories and returns parsed NLSkillEntry list.
// Permission errors and symlink issues are logged and skipped gracefully.
func ScanSkillDir(root string) []corelib.NLSkillEntry {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[skill-scanner] cannot read %s: %v", root, err)
		}
		return nil
	}

	var result []corelib.NLSkillEntry
	for _, entry := range entries {
		// Resolve symlinks: DirEntry.IsDir() returns false for symlinks to dirs.
		info, err := os.Stat(filepath.Join(root, entry.Name()))
		if err != nil {
			log.Printf("[skill-scanner] skip %s/%s: %v", root, entry.Name(), err)
			continue
		}
		if !info.IsDir() {
			continue
		}
		yamlPath := filepath.Join(root, entry.Name(), "skill.yaml")
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			yamlPath = filepath.Join(root, entry.Name(), "skill.yml")
			data, err = os.ReadFile(yamlPath)
			if err != nil {
				continue
			}
		}

		var sf SkillYAMLFile
		if err := yaml.Unmarshal(data, &sf); err != nil {
			log.Printf("[skill-scanner] skip %s/%s: YAML parse error: %v", root, entry.Name(), err)
			continue
		}

		name := strings.TrimSpace(sf.Name)
		if name == "" {
			name = entry.Name()
		}
		status := sf.Status
		if status == "" {
			status = "active"
		}

		steps := make([]corelib.NLSkillStep, 0, len(sf.Steps))
		for _, s := range sf.Steps {
			steps = append(steps, corelib.NLSkillStep{
				Action:  s.Action,
				Params:  s.Params,
				OnError: s.OnError,
			})
		}

		skillDir := filepath.Join(root, entry.Name())

		var hubSkillID string
		if statusData, err := os.ReadFile(filepath.Join(skillDir, "upload_status.json")); err == nil {
			var us uploadStatusFile
			if json.Unmarshal(statusData, &us) == nil && us.SubmissionID != "" {
				hubSkillID = us.SubmissionID
			}
		}

		result = append(result, corelib.NLSkillEntry{
			Name:        name,
			Description: sf.Description,
			Triggers:    sf.Triggers,
			Steps:       steps,
			Status:      status,
			Source:      "file",
			Platforms:   sf.Platforms,
			RequiresGUI: sf.RequiresGUI,
			SkillDir:    skillDir,
			HubSkillID:  hubSkillID,
			CreatedAt:   fileModTime(yamlPath),
		})
	}
	return result
}

func fileModTime(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return time.Now().Format(time.RFC3339)
	}
	return info.ModTime().Format(time.RFC3339)
}
