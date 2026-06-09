package main

import (
	"fmt"
	"os"
	"strings"
)

// normalizeWorkflowProjectPath validates workflow project context without
// mutating it. Missing directories are allowed: coding implementation must
// create new project roots through CodingSubAgent, not workflow control code.
func normalizeWorkflowProjectPath(projectPath string) (path string, existsAsDir bool, err error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "", false, nil
	}
	info, statErr := os.Stat(projectPath)
	if statErr == nil {
		if !info.IsDir() {
			return "", false, fmt.Errorf("workflow project path %q is not a directory", projectPath)
		}
		return projectPath, true, nil
	}
	if os.IsNotExist(statErr) {
		return projectPath, false, nil
	}
	return "", false, fmt.Errorf("inspect workflow project path %q: %w", projectPath, statErr)
}
