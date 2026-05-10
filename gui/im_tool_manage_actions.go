package main

import "strings"

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

type manageScheduleAction string

const (
	manageScheduleActionUnknown manageScheduleAction = ""
	manageScheduleActionCreate  manageScheduleAction = "create"
	manageScheduleActionList    manageScheduleAction = "list"
	manageScheduleActionDelete  manageScheduleAction = "delete"
	manageScheduleActionUpdate  manageScheduleAction = "update"
)

func normalizeManageScheduleAction(action string) manageScheduleAction {
	switch manageScheduleAction(strings.ToLower(strings.TrimSpace(action))) {
	case manageScheduleActionCreate:
		return manageScheduleActionCreate
	case manageScheduleActionList:
		return manageScheduleActionList
	case manageScheduleActionDelete:
		return manageScheduleActionDelete
	case manageScheduleActionUpdate:
		return manageScheduleActionUpdate
	default:
		return manageScheduleActionUnknown
	}
}
