package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func isSkillMarkdownDocName(name string) bool {
	return strings.EqualFold(name, "skill.md") || strings.EqualFold(name, "README.md")
}

func findSkillMarkdownDocPath(skillDir string) (string, error) {
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return "", err
	}
	for _, name := range []string{"skill.md", "SKILL.md", "README.md", "readme.md"} {
		for _, entry := range entries {
			if !entry.IsDir() && entry.Name() == name {
				return filepath.Join(skillDir, entry.Name()), nil
			}
		}
	}
	for _, entry := range entries {
		if !entry.IsDir() && isSkillMarkdownDocName(entry.Name()) {
			return filepath.Join(skillDir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("skill markdown documentation not found")
}
