package main

import (
	"bytes"
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/skill"
)

//go:embed all:builtin_skills
var builtinSkillsFS embed.FS

// deployBuiltinSkills copies bundled skills from the embedded filesystem to
// the user's primary skills directory (~/.maclaw/data/skills/) if they do not
// already exist, and upgrades script files for already-deployed skills.
// This ensures tool_app skills (pdf-word, doc-redact, etc.) are available
// out-of-the-box and receive bug fixes automatically.
//
// Rules:
//   - Deploys if the skill directory does NOT already exist (first install)
//   - For existing skills: only scripts/ subdirectory is refreshed (bug fixes propagate)
//   - skill.yaml and other top-level files are never overwritten (user/hub customization preserved)
//   - Silently skips on any error (non-critical — app works without builtin skills)
func deployBuiltinSkills() {
	primaryDir, err := skill.PrimarySkillsDir()
	if err != nil {
		return
	}
	if err := os.MkdirAll(primaryDir, 0o755); err != nil {
		return
	}

	entries, err := fs.ReadDir(builtinSkillsFS, "builtin_skills")
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillName := entry.Name()
		destDir := filepath.Join(primaryDir, skillName)

		if _, statErr := os.Stat(destDir); statErr == nil {
			// Already deployed — check if the bundled scripts need upgrading.
			// We unconditionally overwrite the scripts/ subdirectory because
			// script bug fixes (like XML character sanitization) must propagate
			// to already-installed skills without requiring user action.
			upgradeBuiltinSkillScripts(builtinSkillsFS, "builtin_skills/"+skillName, destDir)
			continue
		}

		// Deploy the entire skill directory
		if deployErr := copyEmbeddedDir(builtinSkillsFS, "builtin_skills/"+skillName, destDir); deployErr != nil {
			log.Printf("[builtin-skills] failed to deploy %s: %v", skillName, deployErr)
			// Clean up partial deployment
			os.RemoveAll(destDir)
		} else {
			log.Printf("[builtin-skills] deployed %s → %s", skillName, destDir)
		}
	}
}

// upgradeBuiltinSkillScripts overwrites the scripts/ subdirectory of an
// already-deployed skill with the latest bundled version. This ensures
// script-level bug fixes propagate automatically without requiring the user
// to reinstall the skill.
//
// Only the scripts/ directory is overwritten — skill.yaml and other top-level
// files are preserved to maintain any user customization (triggers, params, etc.).
//
// Safety: only upgrades if the installed skill.yaml's name matches the builtin
// name. This prevents overwriting a same-directory-name but different-author
// skill installed from GitHub or other sources.
func upgradeBuiltinSkillScripts(fsys embed.FS, srcDir, destDir string) {
	scriptsPrefix := srcDir + "/scripts/"
	// Check if bundled skill has a scripts/ directory
	if _, err := fs.Stat(fsys, srcDir+"/scripts"); err != nil {
		return
	}

	// Safety check: verify the installed skill name matches the builtin name.
	// This prevents overwriting a third-party skill that happens to share the
	// same directory name but has completely different scripts.
	builtinYAML, err := fs.ReadFile(fsys, srcDir+"/skill.yaml")
	if err != nil {
		return // no builtin skill.yaml → cannot verify, skip
	}
	installedYAML, err := os.ReadFile(filepath.Join(destDir, "skill.yaml"))
	if err != nil {
		return // no installed skill.yaml → cannot verify, skip
	}
	if !skillYAMLNameMatches(builtinYAML, installedYAML) {
		return // different skill — do not overwrite
	}

	destScripts := filepath.Join(destDir, "scripts")
	if err := os.MkdirAll(destScripts, 0o755); err != nil {
		return
	}

	fs.WalkDir(fsys, srcDir+"/scripts", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(path, scriptsPrefix)
		if rel == "" {
			return nil
		}
		destPath := filepath.Join(destScripts, filepath.FromSlash(rel))
		if dir := filepath.Dir(destPath); dir != destScripts {
			os.MkdirAll(dir, 0o755)
		}
		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return nil // skip unreadable files
		}
		// Only write if content differs (avoid unnecessary disk I/O and mtime changes)
		if existing, err := os.ReadFile(destPath); err == nil && bytes.Equal(existing, data) {
			return nil
		}
		if writeErr := os.WriteFile(destPath, data, 0o644); writeErr != nil {
			log.Printf("[builtin-skills] upgrade scripts %s: %v", rel, writeErr)
		}
		return nil
	})
}

// skillYAMLNameMatches does a lightweight check that both YAML files declare
// the same skill name (first "name:" line). This avoids full YAML parsing
// overhead on every app startup for each builtin skill.
func skillYAMLNameMatches(a, b []byte) bool {
	nameA := extractYAMLNameField(a)
	nameB := extractYAMLNameField(b)
	return nameA != "" && nameA == nameB
}

// extractYAMLNameField extracts the value of the first "name:" field from raw
// YAML bytes without full parsing. Returns "" if not found.
func extractYAMLNameField(data []byte) string {
	for _, line := range strings.SplitN(string(data), "\n", 20) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			val = strings.Trim(val, "\"'")
			return val
		}
	}
	return ""
}

// copyEmbeddedDir recursively copies an embedded directory to the filesystem.
// Note: embed.FS uses forward slashes; we use strings.TrimPrefix instead of
// filepath.Rel to correctly compute the relative path on all platforms.
func copyEmbeddedDir(fsys embed.FS, srcDir string, destDir string) error {
	// Normalize: ensure srcDir has trailing slash for prefix stripping
	prefix := srcDir + "/"
	return fs.WalkDir(fsys, srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == srcDir {
			return os.MkdirAll(destDir, 0o755)
		}

		// embed.FS paths use forward slashes — strip the prefix to get relative path
		rel := strings.TrimPrefix(path, prefix)
		// Convert forward-slash relative path to OS path
		destPath := filepath.Join(destDir, filepath.FromSlash(rel))

		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		// Read file from embedded FS
		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return readErr
		}

		// Write to destination
		return os.WriteFile(destPath, data, 0o644)
	})
}
