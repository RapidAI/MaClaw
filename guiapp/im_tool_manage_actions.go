package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

type manageConfigAction string

const (
	manageConfigActionUnknown manageConfigAction = ""
	manageConfigActionGet     manageConfigAction = "get"
	manageConfigActionSet     manageConfigAction = "set"
	manageConfigActionBatch   manageConfigAction = "batch"
	manageConfigActionSchema  manageConfigAction = "schema"
	manageConfigActionExport  manageConfigAction = "export"
	manageConfigActionImport  manageConfigAction = "import"
)

func normalizeManageConfigAction(action string) manageConfigAction {
	switch manageConfigAction(strings.ToLower(strings.TrimSpace(action))) {
	case manageConfigActionGet:
		return manageConfigActionGet
	case manageConfigActionSet:
		return manageConfigActionSet
	case manageConfigActionBatch:
		return manageConfigActionBatch
	case manageConfigActionSchema:
		return manageConfigActionSchema
	case manageConfigActionExport:
		return manageConfigActionExport
	case manageConfigActionImport:
		return manageConfigActionImport
	default:
		return manageConfigActionUnknown
	}
}

type manageTemplateAction string

const (
	manageTemplateActionUnknown manageTemplateAction = ""
	manageTemplateActionCreate  manageTemplateAction = "create"
	manageTemplateActionList    manageTemplateAction = "list"
	manageTemplateActionLaunch  manageTemplateAction = "launch"
)

func normalizeManageTemplateAction(action string) manageTemplateAction {
	switch manageTemplateAction(strings.ToLower(strings.TrimSpace(action))) {
	case manageTemplateActionCreate:
		return manageTemplateActionCreate
	case manageTemplateActionList:
		return manageTemplateActionList
	case manageTemplateActionLaunch:
		return manageTemplateActionLaunch
	default:
		return manageTemplateActionUnknown
	}
}

// manageScheduleAction and its constants alias the shared agentruntime
// vocabulary so admission gates and dispatch use one implementation.
type manageScheduleAction = agentruntime.ManageScheduleAction

const (
	manageScheduleActionUnknown     = agentruntime.ManageScheduleActionUnknown
	manageScheduleActionCreate      = agentruntime.ManageScheduleActionCreate
	manageScheduleActionList        = agentruntime.ManageScheduleActionList
	manageScheduleActionRun         = agentruntime.ManageScheduleActionRun
	manageScheduleActionPause       = agentruntime.ManageScheduleActionPause
	manageScheduleActionResume      = agentruntime.ManageScheduleActionResume
	manageScheduleActionDelete      = agentruntime.ManageScheduleActionDelete
	manageScheduleActionUpdate      = agentruntime.ManageScheduleActionUpdate
	manageScheduleActionListTargets = agentruntime.ManageScheduleActionListTargets
)

// normalizeManageScheduleAction delegates to the shared agentruntime
// canonicalizer.
func normalizeManageScheduleAction(action string) manageScheduleAction {
	return agentruntime.NormalizeManageScheduleAction(action)
}
