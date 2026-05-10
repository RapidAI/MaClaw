package main

import (
	"strings"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type manageSkillAction string

const (
	manageSkillActionUnknown  manageSkillAction = ""
	manageSkillActionList     manageSkillAction = "list"
	manageSkillActionSearch   manageSkillAction = "search"
	manageSkillActionInstall  manageSkillAction = "install"
	manageSkillActionRun      manageSkillAction = "run"
	manageSkillActionStatus   manageSkillAction = "status"
	manageSkillActionUpload   manageSkillAction = "upload"
	manageSkillActionValidate manageSkillAction = "validate"
	manageSkillActionPatch    manageSkillAction = "patch"
	manageSkillActionHistory  manageSkillAction = "history"
)

func classifyManageSkillAction(action string) manageSkillAction {
	switch manageSkillAction(strings.ToLower(strings.TrimSpace(action))) {
	case manageSkillActionList:
		return manageSkillActionList
	case manageSkillActionSearch:
		return manageSkillActionSearch
	case manageSkillActionInstall:
		return manageSkillActionInstall
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
	default:
		return manageSkillAction(strings.TrimSpace(action))
	}
}

// toolManageSkill dispatches the merged manage_skill tool to individual handlers.
func (h *IMMessageHandler) toolManageSkill(args map[string]interface{}, onProgress tool.ProgressCallback) string {
	action := stringVal(args, "action")
	switch classifyManageSkillAction(action) {
	case manageSkillActionList:
		return h.toolListSkills()
	case manageSkillActionSearch:
		return h.toolSearchSkillHub(args)
	case manageSkillActionInstall:
		return h.toolInstallSkillHub(args)
	case manageSkillActionRun:
		return h.toolRunSkill(args, onProgress)
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
	default:
		return cskill.ManageSkillUnknownActionError(action)
	}
}
