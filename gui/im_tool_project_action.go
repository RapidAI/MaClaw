package main

import "strings"

type projectToolAction string
type projectToolResultStatus string

const (
	projectToolActionUnknown projectToolAction = ""
	projectToolActionCreate  projectToolAction = "create"
	projectToolActionList    projectToolAction = "list"
	projectToolActionDelete  projectToolAction = "delete"
	projectToolActionSwitch  projectToolAction = "switch"

	projectToolStatusCreated  projectToolResultStatus = "created"
	projectToolStatusDeleted  projectToolResultStatus = "deleted"
	projectToolStatusSwitched projectToolResultStatus = "switched"
)

func normalizeProjectToolAction(action string) projectToolAction {
	switch projectToolAction(strings.ToLower(strings.TrimSpace(action))) {
	case projectToolActionCreate:
		return projectToolActionCreate
	case projectToolActionList:
		return projectToolActionList
	case projectToolActionDelete:
		return projectToolActionDelete
	case projectToolActionSwitch:
		return projectToolActionSwitch
	default:
		return projectToolAction(strings.TrimSpace(action))
	}
}

func (status projectToolResultStatus) String() string {
	return string(status)
}
