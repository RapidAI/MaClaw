package main

import "strings"

type experienceDraftKind string

const experienceDraftKindUnknown experienceDraftKind = ""

func normalizeExperienceDraftKind(value string) experienceDraftKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "memory", "maintenance", "memory_maintenance", "memory_maintenance_draft":
		return experienceDraftKind(experienceDraftKindMaintenance)
	case "routing", "router", "routing_adjustment", "routing_adjustment_draft":
		return experienceDraftKind(experienceDraftKindRouting)
	case "skill", "skill_draft", "skill_nudge", "skill_nudge_draft":
		return experienceDraftKind(experienceDraftKindSkill)
	case "rollback", "rollback_workflow", "rollback_draft", "rollback_workflow_draft":
		return experienceDraftKind(experienceDraftKindRollback)
	case "escalation", "escalation_brief", "escalation_handoff", "escalation_handoff_brief":
		return experienceDraftKind(experienceDraftKindEscalation)
	case "conflict", "conflict_reconciliation", "conflict_draft", "conflict_reconciliation_draft":
		return experienceDraftKind(experienceDraftKindConflict)
	default:
		return experienceDraftKindUnknown
	}
}

func (k experienceDraftKind) String() string {
	return string(k)
}

func (k experienceDraftKind) IsKnown() bool {
	return k != experienceDraftKindUnknown
}

func (k experienceDraftKind) Title() string {
	switch k {
	case experienceDraftKind(experienceDraftKindMaintenance):
		return "memory maintenance draft"
	case experienceDraftKind(experienceDraftKindRouting):
		return "routing adjustment draft"
	case experienceDraftKind(experienceDraftKindSkill):
		return "skill draft"
	case experienceDraftKind(experienceDraftKindRollback):
		return "rollback workflow draft"
	case experienceDraftKind(experienceDraftKindEscalation):
		return "escalation brief"
	case experienceDraftKind(experienceDraftKindConflict):
		return "conflict reconciliation draft"
	default:
		return strings.TrimSpace(k.String())
	}
}
