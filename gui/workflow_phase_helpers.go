package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	workflowPhaseRequirements = "requirements"
	workflowPhaseDesign       = "design"
	workflowPhaseTasks        = "tasks"
)

type workflowPhaseKind string

const workflowPhaseUnknown workflowPhaseKind = ""

func normalizeWorkflowPhaseKind(value string) workflowPhaseKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "requirements", "requirement", "req":
		return workflowPhaseKind(workflowPhaseRequirements)
	case "design", "tech_design", "technical_design":
		return workflowPhaseKind(workflowPhaseDesign)
	case "tasks", "task", "task_plan", "task_breakdown":
		return workflowPhaseKind(workflowPhaseTasks)
	default:
		return workflowPhaseUnknown
	}
}

func (k workflowPhaseKind) String() string {
	return string(k)
}

func normalizeWorkflowPhaseID(value string) string {
	return normalizeWorkflowPhaseKind(value).String()
}

func workflowPhaseString(phase string) string {
	return normalizeWorkflowPhaseID(phase)
}

func workflowPhaseFromMetadata(values ...string) string {
	for _, value := range values {
		if phaseID := normalizeWorkflowPhaseID(value); phaseID != "" {
			return phaseID
		}
	}
	return ""
}

func inferFileDeliveryMessage(fileName string) string {
	base := strings.TrimSpace(filepath.Base(fileName))
	if base == "." || base == string(filepath.Separator) {
		base = "the generated file"
	}
	return fmt.Sprintf("Please send %s to the user.", base)
}

type searchAndInstallSkillResult struct {
	Text    string
	Success bool
}

func (h *IMMessageHandler) toolSearchAndInstallSkillResult(args map[string]interface{}, onProgress tool.ProgressCallback) searchAndInstallSkillResult {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return searchAndInstallSkillResult{Text: "Missing query parameter.", Success: false}
	}
	sendStatus := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}
	ctx := context.Background()
	searcher := NewSkillSearcher(NewSkillMarketClient(h.app))
	best, err := searcher.SearchAndInstall(ctx, query)
	if err != nil {
		return searchAndInstallSkillResult{Text: fmt.Sprintf("Skill search failed: %v", err), Success: false}
	}
	if best == nil {
		return searchAndInstallSkillResult{Text: fmt.Sprintf("No matching skill found for %q.", query), Success: false}
	}
	platform := ""
	if h.currentLoopCtx != nil {
		platform = h.currentLoopCtx.Platform
	}
	installResult := h.installAndExecuteSkill(ctx, best, query, platform, h.lastUserID, sendStatus)
	return searchAndInstallSkillResult{Text: installResult.Text, Success: installResult.Success}
}
