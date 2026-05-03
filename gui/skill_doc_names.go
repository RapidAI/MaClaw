package main

import (
	"os"
	"path/filepath"
	"strings"
)

func isSkillMarkdownDocFileName(name string) bool {
	return strings.EqualFold(name, "skill.md") || strings.EqualFold(name, "README.md")
}

func findSkillMarkdownDocPath(skillDir string) string {
	if strings.TrimSpace(skillDir) == "" {
		return ""
	}
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return ""
	}
	for _, name := range []string{"skill.md", "SKILL.md", "README.md", "readme.md"} {
		for _, entry := range entries {
			if !entry.IsDir() && entry.Name() == name {
				return filepath.Join(skillDir, entry.Name())
			}
		}
	}
	for _, entry := range entries {
		if !entry.IsDir() && isSkillMarkdownDocFileName(entry.Name()) {
			return filepath.Join(skillDir, entry.Name())
		}
	}
	return ""
}
