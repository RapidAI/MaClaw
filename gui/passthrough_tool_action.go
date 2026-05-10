package main

import "strings"

type passthroughToolAction string

const (
	passthroughToolActionUnknown    passthroughToolAction = ""
	passthroughToolActionList       passthroughToolAction = "list"
	passthroughToolActionStatus     passthroughToolAction = "status"
	passthroughToolActionShow       passthroughToolAction = "show"
	passthroughToolActionExport     passthroughToolAction = "export"
	passthroughToolActionPreview    passthroughToolAction = "preview"
	passthroughToolActionSave       passthroughToolAction = "save"
	passthroughToolActionDelete     passthroughToolAction = "delete"
	passthroughToolActionSetEnabled passthroughToolAction = "set_enabled"
	passthroughToolActionAudit      passthroughToolAction = "audit"
)

func normalizePassthroughToolAction(action string) passthroughToolAction {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", string(passthroughToolActionList):
		return passthroughToolActionList
	case string(passthroughToolActionStatus), "settings":
		return passthroughToolActionStatus
	case string(passthroughToolActionShow):
		return passthroughToolActionShow
	case string(passthroughToolActionExport):
		return passthroughToolActionExport
	case string(passthroughToolActionPreview):
		return passthroughToolActionPreview
	case string(passthroughToolActionSave), "upsert":
		return passthroughToolActionSave
	case string(passthroughToolActionDelete):
		return passthroughToolActionDelete
	case string(passthroughToolActionSetEnabled), string(passthroughControlActionEnable), string(passthroughControlActionDisable):
		return passthroughToolActionSetEnabled
	case string(passthroughToolActionAudit):
		return passthroughToolActionAudit
	default:
		return passthroughToolActionUnknown
	}
}
