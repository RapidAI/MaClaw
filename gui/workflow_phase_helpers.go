package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

const (
	workflowPhaseRequirements = "requirements"
	workflowPhaseDesign       = "design"
	workflowPhaseTasks        = "tasks"
)

type workflowPhaseKind string

const workflowPhaseUnknown workflowPhaseKind = ""

// normalizeWorkflowPhaseKind classifies a raw phase ID into one of the three
// canonical coding-workflow kinds, or workflowPhaseUnknown for anything else.
//
// It delegates to workflow.CanonicalPhaseID so the phase-ID alias table lives in
// exactly one place (corelib/workflow): adding or changing an alias there flows
// here automatically and the two can never drift. A canonical ID that is not one
// of the three known coding kinds (i.e. CanonicalPhaseID passed it through
// unchanged) maps to workflowPhaseUnknown.
func normalizeWorkflowPhaseKind(value string) workflowPhaseKind {
	switch canonical := workflow.CanonicalPhaseID(value); canonical {
	case workflowPhaseRequirements, workflowPhaseDesign, workflowPhaseTasks:
		return workflowPhaseKind(canonical)
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

func workflowPhaseKindFromMetadata(values ...string) workflowPhaseKind {
	for _, value := range values {
		if phase := normalizeWorkflowPhaseKind(value); phase != workflowPhaseUnknown {
			return phase
		}
	}
	return workflowPhaseUnknown
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

func (h *IMMessageHandler) executeSkillSearchInstall(args map[string]interface{}, onProgress tool.ProgressCallback) searchAndInstallSkillResult {
	if h != nil && h.skillSearchInstallHandler != nil {
		return h.skillSearchInstallHandler(args, onProgress)
	}
	return h.toolSearchAndInstallSkillResult(args, onProgress)
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
	policyOwnerID, explicitRuntime := h.consumeRuntimePolicyOwnerIDFromToolArgsOrCurrentState(args)
	if policyOwnerID == "" && explicitRuntime {
		return searchAndInstallSkillResult{Text: "Skill search failed: runtime owner is missing; isolated runtime will not fall back to desktop owner", Success: false}
	}
	platform := consumeRuntimePlatformFromToolArgs(args)
	if platform == "" {
		platform = h.runtimePlatformForOwnerOrCurrent(policyOwnerID, explicitRuntime)
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
	installResult := h.installAndExecuteSkill(ctx, best, query, platform, policyOwnerID, policyOwnerID, sendStatus)
	return searchAndInstallSkillResult{Text: installResult.Text, Success: installResult.Success}
}
