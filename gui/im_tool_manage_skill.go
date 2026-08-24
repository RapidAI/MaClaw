package main

import (
	"context"
	"encoding/json"
	"strings"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type manageSkillAction string

const (
	manageSkillActionUnknown                manageSkillAction = ""
	manageSkillActionList                   manageSkillAction = "list"
	manageSkillActionInfo                   manageSkillAction = "info"
	manageSkillActionSearch                 manageSkillAction = "search"
	manageSkillActionInstall                manageSkillAction = "install"
	manageSkillActionUninstall              manageSkillAction = "uninstall"
	manageSkillActionRun                    manageSkillAction = "run"
	manageSkillActionStatus                 manageSkillAction = "status"
	manageSkillActionUpload                 manageSkillAction = "upload"
	manageSkillActionValidate               manageSkillAction = "validate"
	manageSkillActionPatch                  manageSkillAction = "patch"
	manageSkillActionHistory                manageSkillAction = "history"
	manageSkillActionMaintenancePlan        manageSkillAction = "maintenance_plan"
	manageSkillActionMaintenanceDrafts      manageSkillAction = "maintenance_drafts"
	manageSkillActionExecuteMaintenancePlan manageSkillAction = "execute_maintenance_plan"
	manageSkillActionEvolutionStatus        manageSkillAction = "evolution_status"
	manageSkillActionEvolutionAudit         manageSkillAction = "evolution_audit"
	manageSkillActionSetEvolutionEnabled    manageSkillAction = "set_evolution_enabled"
	manageSkillActionTriggerRepair          manageSkillAction = "trigger_repair"
	manageSkillActionTriggerOptimize        manageSkillAction = "trigger_optimize"
	manageSkillActionListRepairDrafts       manageSkillAction = "list_repair_drafts"
	manageSkillActionApplyRepairDraft       manageSkillAction = "apply_repair_draft"
	manageSkillActionRejectRepairDraft      manageSkillAction = "reject_repair_draft"
)

func classifyManageSkillAction(action string) manageSkillAction {
	switch manageSkillAction(cskill.NormalizeManageSkillAction(action)) {
	case manageSkillActionList:
		return manageSkillActionList
	case manageSkillActionInfo:
		return manageSkillActionInfo
	case manageSkillActionSearch:
		return manageSkillActionSearch
	case manageSkillActionInstall:
		return manageSkillActionInstall
	case manageSkillActionUninstall:
		return manageSkillActionUninstall
	case manageSkillActionRun:
		return manageSkillActionRun
	case manageSkillActionStatus:
		return manageSkillActionStatus
	case manageSkillActionUpload:
		return manageSkillActionUpload
	case manageSkillActionValidate:
		return manageSkillActionValidate
	case manageSkillActionPatch:
		return manageSkillActionPatch
	case manageSkillActionHistory:
		return manageSkillActionHistory
	case manageSkillActionMaintenancePlan:
		return manageSkillActionMaintenancePlan
	case manageSkillActionMaintenanceDrafts:
		return manageSkillActionMaintenanceDrafts
	case manageSkillActionExecuteMaintenancePlan:
		return manageSkillActionExecuteMaintenancePlan
	case manageSkillActionEvolutionStatus:
		return manageSkillActionEvolutionStatus
	case manageSkillActionEvolutionAudit:
		return manageSkillActionEvolutionAudit
	case manageSkillActionSetEvolutionEnabled:
		return manageSkillActionSetEvolutionEnabled
	case manageSkillActionTriggerRepair:
		return manageSkillActionTriggerRepair
	case manageSkillActionTriggerOptimize:
		return manageSkillActionTriggerOptimize
	case manageSkillActionListRepairDrafts:
		return manageSkillActionListRepairDrafts
	case manageSkillActionApplyRepairDraft:
		return manageSkillActionApplyRepairDraft
	case manageSkillActionRejectRepairDraft:
		return manageSkillActionRejectRepairDraft
	default:
		return manageSkillAction(strings.TrimSpace(action))
	}
}

// legacyModelManageSkillActionAllowed identifies the deliberately tiny
// compatibility subset of the merged manage_skill gateway. The merged schema
// is not a static capability: most actions let model arguments select an
// installed Skill, a Hub package, or a mutable Skill directory. Those choices
// need the dynamic semantic catalog's reviewed binding, not a legacy function
// name plus a top-level argument allow-list.
//
// Listing is the sole read-only inventory operation retained for legacy turns.
// It does not accept a provider, package, Skill, or run identity from the
// model. status is intentionally excluded: an arbitrary run_id is an
// unbound, cross-run resource reference and can also cause a long poll.
func legacyModelManageSkillActionAllowed(argumentsJSON string) bool {
	args := map[string]interface{}{}
	if err := json.Unmarshal([]byte(normalizeAgentLoopToolArgumentsJSON(argumentsJSON)), &args); err != nil {
		return false
	}
	action, ok := args["action"].(string)
	return ok && classifyManageSkillAction(action) == manageSkillActionList
}

func isLegacyModelManageSkillGateway(name, argumentsJSON string) bool {
	return strings.TrimSpace(name) == "manage_skill" && !legacyModelManageSkillActionAllowed(argumentsJSON)
}

func legacyModelManageSkillGatewayDeniedText() string {
	return "[system rejected] dynamic_skill_requires_managed_surface: legacy model calls may only list Skill inventory. Running, inspecting, installing, searching, mutating, uploading, or querying a Skill run requires a managed semantic binding. Request a managed semantic replan."
}

// toolManageSkill dispatches the merged manage_skill tool to individual handlers.
func (h *IMMessageHandler) toolManageSkill(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string {
	ownerID, explicitRuntimeOwner := runtimePolicyOwnerIDFromToolArgsWithPresence(args)
	if explicitRuntimeOwner && ownerID == "" {
		return "manage_skill failed: runtime owner is missing; isolated runtime will not fall back to desktop owner"
	}
	action := stringVal(args, "action")
	switch classifyManageSkillAction(action) {
	case manageSkillActionList:
		return h.toolListSkills()
	case manageSkillActionInfo:
		return h.toolSkillInfo(args)
	case manageSkillActionSearch:
		return h.toolSearchSkillHub(args)
	case manageSkillActionInstall:
		return h.toolInstallSkillHub(args)
	case manageSkillActionUninstall:
		return h.toolUninstallSkill(args)
	case manageSkillActionRun:
		return h.toolRunSkill(ctx, args, onProgress)
	case manageSkillActionStatus:
		return h.toolGetSkillRun(args)
	case manageSkillActionUpload:
		return h.toolUploadSkill(args)
	case manageSkillActionValidate:
		return h.toolValidateSkill(args)
	case manageSkillActionPatch:
		return h.toolPatchSkill(args)
	case manageSkillActionHistory:
		return h.toolSkillPatchHistory(args)
	case manageSkillActionMaintenancePlan:
		return h.toolSkillMaintenancePlan(args)
	case manageSkillActionMaintenanceDrafts:
		return h.toolSkillMaintenanceDrafts(args)
	case manageSkillActionExecuteMaintenancePlan:
		return h.toolExecuteSkillMaintenancePlan(args)
	case manageSkillActionEvolutionStatus:
		return h.toolSkillEvolutionStatus(args)
	case manageSkillActionEvolutionAudit:
		return h.toolSkillEvolutionAudit(args)
	case manageSkillActionSetEvolutionEnabled:
		return h.toolSetSkillEvolutionEnabled(args)
	case manageSkillActionTriggerRepair:
		return h.toolTriggerSkillRepair(args)
	case manageSkillActionTriggerOptimize:
		return h.toolTriggerSkillOptimize(args)
	case manageSkillActionListRepairDrafts:
		return h.toolListSkillRepairDrafts(args)
	case manageSkillActionApplyRepairDraft:
		return h.toolApplySkillRepairDraft(args)
	case manageSkillActionRejectRepairDraft:
		return h.toolRejectSkillRepairDraft(args)
	default:
		return cskill.ManageSkillUnknownActionError(action)
	}
}
