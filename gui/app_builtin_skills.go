package main

import (
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
// already exist. This ensures tool_app skills (pdf-word, doc-redact, etc.)
// are available out-of-the-box without manual installation.
//
// Rules:
//   - Only deploys if the skill directory does NOT already exist (never overwrites user modifications)
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

		// Skip if already deployed (never overwrite user modifications)
		if _, statErr := os.Stat(destDir); statErr == nil {
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
