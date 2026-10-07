package guiapp

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
	manageSkillActionUploadSuite            manageSkillAction = "upload_suite"
	manageSkillActionValidate               manageSkillAction = "validate"
	manageSkillActionPatch                  manageSkillAction = "patch"
	manageSkillActionHistory                manageSkillAction = "history"
	manageSkillActionMaintenancePlan        manageSkillAction = "maintenance_plan"
	manageSkillActionMaintenanceDrafts      manageSkillAction = "maintenance_drafts"
	manageSkillActionExecuteMaintenancePlan manageSkillAction = "execute_maintenance_plan"
	manageSkillActionEvolutionStatus        manageSkillAction = "evolution_status"
	manageSkillActionEvolutionCompensations manageSkillAction = "evolution_compensations"
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
	case manageSkillActionUploadSuite:
		return manageSkillActionUploadSuite
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

// legacyModelManageSkillActionAllowed identifies the compatibility subset of
// the merged manage_skill gateway. Listing does not take a skill identity.
// run is allowed only when skillInstalled reports that the named skill is
// already in the local registry: the desktop runner binds that name, and the
// model is not selecting a Hub package or a remote provider. Install, search,
// upload, mutation, and status stay closed. status is an arbitrary run_id,
// which is an unbound cross-run reference and can also cause a long poll.
func legacyModelManageSkillActionAllowed(argumentsJSON string, skillInstalled func(string) bool) bool {
	args := map[string]interface{}{}
	if err := json.Unmarshal([]byte(normalizeAgentLoopToolArgumentsJSON(argumentsJSON)), &args); err != nil {
		return false
	}
	action, ok := args["action"].(string)
	if !ok {
		return false
	}
	switch classifyManageSkillAction(action) {
	case manageSkillActionList:
		return true
	case manageSkillActionInfo, manageSkillActionRun:
		name, _ := args["name"].(string)
		name = strings.TrimSpace(name)
		return name != "" && skillInstalled != nil && skillInstalled(name)
	default:
		return false
	}
}

func (h *IMMessageHandler) legacyManageSkillInstalled(name string) bool {
	if h == nil || h.app == nil {
		return false
	}
	return h.app.findSkillForAgentView(name) != nil
}

func legacyModelManageSkillCallDenied(h *IMMessageHandler, name, argumentsJSON string) bool {
	if strings.TrimSpace(name) != "manage_skill" {
		return false
	}
	return !legacyModelManageSkillActionAllowed(argumentsJSON, h.legacyManageSkillInstalled)
}

func legacyModelManageSkillGatewayDeniedText() string {
	return "[system rejected] dynamic_skill_requires_managed_surface: legacy model calls may list Skill inventory, or inspect and run a Skill that is already installed locally. Installing, searching, mutating, uploading, or querying a Skill run requires a managed semantic binding. Request a managed semantic replan."
}

// legacyManageSkillSurfaceDescription is the only contract a full unmanaged
// surface advertises. The registry entry still lists install, search, and
// governance actions for the host UI; publishing those names makes the model
// call them and spend the turn on a rejection.
const legacyManageSkillSurfaceDescription = "运行本机已经安装的 Skill。action 只能是 list、info、run。list 列出已安装 Skill，不需要 name。info 查看某个已安装 Skill 的参数，name 必填。run 执行它，name 必填，参数放在 args。name 必须来自 list 的结果。"

func narrowLegacyManageSkillSurface(tools []map[string]interface{}) []map[string]interface{} {
	for i, def := range tools {
		if extractToolName(def) != "manage_skill" {
			continue
		}
		tools[i] = toolDef("manage_skill", legacyManageSkillSurfaceDescription, map[string]interface{}{
			"action":       map[string]interface{}{"type": "string", "description": "list、info 或 run"},
			"name":         map[string]interface{}{"type": "string", "description": "已安装 Skill 的名称。info 和 run 必填，必须来自 list 的结果。"},
			"args":         map[string]interface{}{"type": "object", "description": "run 的参数。"},
			"operation":    map[string]interface{}{"type": "string", "description": "run 时可选的 operation。"},
			"input":        map[string]interface{}{"type": "string", "description": "run 时可选的输入。"},
			"output":       map[string]interface{}{"type": "string", "description": "run 时可选的输出路径。"},
			"user_prompt":  map[string]interface{}{"type": "string", "description": "run 时可选的用户原话。"},
			"env":          map[string]interface{}{"type": "object", "description": "run 时注入子进程的环境变量。"},
			"wait_seconds": map[string]interface{}{"type": "number", "description": "run 后等待状态快照的秒数。"},
		}, []string{"action"})
	}
	return tools
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
	case manageSkillActionUploadSuite:
		return h.toolUploadSkillSuite(args)
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
	case manageSkillActionEvolutionCompensations:
		return h.toolSkillEvolutionCompensations(args)
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
