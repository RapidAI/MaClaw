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

// MigrateSkillsDir is a no-op kept for backward compatibility.
// The old ~/.maclaw/skills path is no longer supported.
// Skills live exclusively in ~/.maclaw/data/skills.
func MigrateSkillsDir() {}

// SkillScanRoots returns all directories that should be scanned for
// file-based skills, in priority order (first wins on name conflict):
//  1. ~/.maclaw/data/skills  (canonical location)
//  2. ~/.agents/skills
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
// directory (contains at least one subdirectory with skill.yaml, skill.md, or SKILL.md).
// Returns the count of valid skill subdirectories and an error if the
// directory is not usable.
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
		for _, name := range []string{"skill.yaml", "skill.md", "SKILL.md"} {
			if _, err := os.Stat(filepath.Join(subDir, name)); err == nil {
				count++
				break
			}
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("no valid skill subdirectories found (need skill.yaml, skill.md, or SKILL.md)")
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
	Extra       map[string]any  `yaml:"-"`
}

// SkillYAMLStep is a single step in a YAML skill definition.
type SkillYAMLStep struct {
	Action  string                 `yaml:"action"`
	Params  map[string]interface{} `yaml:"params"`
	OnError string                 `yaml:"on_error"`
}

func ParseSkillYAMLFile(data []byte) (*SkillYAMLFile, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}
	var sf SkillYAMLFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("YAML parse error: %w", err)
	}
	knownKeys := map[string]bool{
		"name": true, "description": true, "triggers": true, "steps": true,
		"status": true, "platforms": true, "requires_gui": true,
	}
	extra := make(map[string]any)
	for k, v := range raw {
		if !knownKeys[k] {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		sf.Extra = extra
	}
	return &sf, nil
}

func FormatSkillYAMLFile(sf *SkillYAMLFile) ([]byte, error) {
	data, err := yaml.Marshal(sf)
	if err != nil {
		return nil, fmt.Errorf("YAML format error: %w", err)
	}
	if len(sf.Extra) == 0 {
		return data, nil
	}
	var known map[string]any
	if err := yaml.Unmarshal(data, &known); err != nil {
		return nil, fmt.Errorf("YAML format error: %w", err)
	}
	if known == nil {
		known = make(map[string]any)
	}
	for k, v := range sf.Extra {
		known[k] = v
	}
	merged, err := yaml.Marshal(known)
	if err != nil {
		return nil, fmt.Errorf("YAML format error: %w", err)
	}
	return merged, nil
}

func skillYAMLPath(skillDir string) (string, error) {
	path := filepath.Join(skillDir, "skill.yaml")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	return "", os.ErrNotExist
}

func loadSkillFromDir(skillDir, fallbackName string) (*corelib.NLSkillEntry, string, error) {
	yamlPath, yamlErr := skillYAMLPath(skillDir)
	if yamlErr == nil {
		data, err := os.ReadFile(yamlPath)
		if err != nil {
			return nil, "", err
		}
		parsedYAML, err := ParseSkillYAMLFile(data)
		if err != nil {
			return nil, "", err
		}
		sf := *parsedYAML
		name := strings.TrimSpace(sf.Name)
		if name == "" {
			name = fallbackName
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
		if len(steps) == 0 {
			parsed, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{
				NameFallback:        name,
				DescriptionFallback: sf.Description,
				Triggers:            sf.Triggers,
				Source:              "file",
				SkillDir:            skillDir,
				Platforms:           sf.Platforms,
				RequiresGUI:         &sf.RequiresGUI,
			})
			if err == nil {
				if mdPath, mdErr := skillMarkdownPath(skillDir); mdErr == nil {
					return parsed, mdPath, nil
				}
				return parsed, yamlPath, nil
			}
		}
		return &corelib.NLSkillEntry{
			Name:        name,
			Description: sf.Description,
			Triggers:    sf.Triggers,
			Steps:       steps,
			Status:      status,
			Source:      "file",
			Platforms:   sf.Platforms,
			RequiresGUI: sf.RequiresGUI,
			SkillDir:    skillDir,
			CreatedAt:   fileModTime(yamlPath),
		}, yamlPath, nil
	}

	parsed, err := ImportMarkdownSkillDir(skillDir, MarkdownSkillOptions{
		NameFallback: fallbackName,
		Source:       "file",
		SkillDir:     skillDir,
	})
	if err != nil {
		return nil, "", err
	}
	if mdPath, mdErr := skillMarkdownPath(skillDir); mdErr == nil {
		return parsed, mdPath, nil
	}
	return parsed, "", nil
}

// uploadStatusFile mirrors the GUI-side upload_status.json format.
type uploadStatusFile struct {
	SubmissionID string `json:"submission_id"`
}

// ScanSkillDir scans a single directory for skill.yaml / skill.md files
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
		info, err := os.Stat(filepath.Join(root, entry.Name()))
		if err != nil {
			log.Printf("[skill-scanner] skip %s/%s: %v", root, entry.Name(), err)
			continue
		}
		if !info.IsDir() {
			continue
		}
		skillDir := filepath.Join(root, entry.Name())
		parsed, defPath, err := loadSkillFromDir(skillDir, entry.Name())
		if err != nil {
			continue
		}

		var hubSkillID string
		if statusData, err := os.ReadFile(filepath.Join(skillDir, "upload_status.json")); err == nil {
			var us uploadStatusFile
			if json.Unmarshal(statusData, &us) == nil && us.SubmissionID != "" {
				hubSkillID = us.SubmissionID
			}
		}
		parsed.HubSkillID = hubSkillID
		if parsed.SkillDir == "" {
			parsed.SkillDir = skillDir
		}
		if parsed.Source == "" {
			parsed.Source = "file"
		}
		if defPath != "" {
			parsed.CreatedAt = fileModTime(defPath)
		}
		result = append(result, *parsed)
	}
	return result
}

func fileModTime(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Now().Format(time.RFC3339)
	}
	return fi.ModTime().Format(time.RFC3339)
}
