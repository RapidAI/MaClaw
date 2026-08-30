package main

import "strings"

type agentStatusCategory string

const (
	agentStatusCategoryAll         agentStatusCategory = "all"
	agentStatusCategoryMainAgent   agentStatusCategory = "main_agent"
	agentStatusCategoryLocalTasks  agentStatusCategory = "local_tasks"
	agentStatusCategorySSHTasks    agentStatusCategory = "ssh_tasks"
	agentStatusCategorySessions    agentStatusCategory = "sessions"
	agentStatusCategorySSHSessions agentStatusCategory = "ssh_sessions"
)

func normalizeAgentStatusCategory(category string) agentStatusCategory {
	switch agentStatusCategory(strings.ToLower(strings.TrimSpace(category))) {
	case "":
		return agentStatusCategoryAll
	case agentStatusCategoryAll:
		return agentStatusCategoryAll
	case agentStatusCategoryMainAgent:
		return agentStatusCategoryMainAgent
	case agentStatusCategoryLocalTasks:
		return agentStatusCategoryLocalTasks
	case agentStatusCategorySSHTasks:
		return agentStatusCategorySSHTasks
	case agentStatusCategorySessions:
		return agentStatusCategorySessions
	case agentStatusCategorySSHSessions:
		return agentStatusCategorySSHSessions
	default:
		return agentStatusCategory(strings.ToLower(strings.TrimSpace(category)))
	}
}

func (category agentStatusCategory) String() string {
	return string(category)
}

func (category agentStatusCategory) IncludesMainAgent() bool {
	return category == agentStatusCategoryAll || category == agentStatusCategoryMainAgent
}

func (category agentStatusCategory) IncludesLocalTasks() bool {
	return category == agentStatusCategoryAll || category == agentStatusCategoryLocalTasks
}

func (category agentStatusCategory) IncludesSSHTasks() bool {
	return category == agentStatusCategoryAll || category == agentStatusCategorySSHTasks
}

func (category agentStatusCategory) IncludesCodingSessions() bool {
	return category == agentStatusCategoryAll || category == agentStatusCategorySessions
}

func (category agentStatusCategory) IncludesSSHSessions() bool {
	return category == agentStatusCategoryAll || category == agentStatusCategorySSHSessions
}
