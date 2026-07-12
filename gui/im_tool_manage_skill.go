package main

import (
	"context"
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
	default:
		return manageSkillAction(strings.TrimSpace(action))
	}
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
	default:
		return cskill.ManageSkillUnknownActionError(action)
	}
}
