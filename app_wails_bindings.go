package main

import (
	"fmt"
	"os/exec"
)

// BackupSkills exports all NL Skills to a zip file (Wails binding).
func (a *App) BackupSkills(outputPath string) error {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.BackupSkills(outputPath)
}

// RestoreSkills imports NL Skills from a zip file (Wails binding).
func (a *App) RestoreSkills(zipPath string) (*RestoreReport, error) {
	a.ensureRemoteInfra()
	if a.skillExecutor == nil {
		return nil, fmt.Errorf("skill executor not initialized")
	}
	return a.skillExecutor.RestoreSkills(zipPath)
}

// QueryAuditLog queries the audit log with the given filter (Wails binding).
func (a *App) QueryAuditLog(filter AuditFilter) ([]AuditEntry, error) {
	a.ensureRemoteInfra()
	if a.auditLog == nil {
		return nil, fmt.Errorf("audit log not initialized")
	}
	return a.auditLog.Query(filter)
}

// RecommendTool suggests the best programming tool for a task (Wails binding).
func (a *App) RecommendTool(taskDescription string) (string, string) {
	a.ensureRemoteInfra()
	if a.toolSelector == nil {
		return "", "tool selector not initialized"
	}
	// Get installed tools by checking which known tools have their binary available.
	var installed []string
	for _, tool := range []string{"claude", "codex", "gemini", "cursor", "opencode", "iflow", "kilo"} {
		meta, ok := remoteToolCatalog[tool]
		if !ok {
			continue
		}
		if _, err := exec.LookPath(meta.BinaryName); err == nil {
			installed = append(installed, tool)
		}
	}
	return a.toolSelector.Recommend(taskDescription, installed)
}
