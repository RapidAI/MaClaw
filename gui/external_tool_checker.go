package main

// external_tool_checker.go bridges SessionPrecheck to the
// ExternalToolChecker interface used by TaskExecutionOrchestrator.

// sessionPrecheckAdapter implements ExternalToolChecker using SessionPrecheck.
type sessionPrecheckAdapter struct {
	precheck *SessionPrecheck
}

func (a *sessionPrecheckAdapter) IsExternalToolAvailable(toolName, projectPath string) bool {
	if a.precheck == nil {
		return false
	}
	result := a.precheck.Check(toolName, projectPath)
	return result.ToolReady && result.ModelReady
}
