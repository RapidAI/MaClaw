package main

import "strings"

type knowledgeMaintenanceActionKind string

const (
	knowledgeMaintenanceActionUnknown                       knowledgeMaintenanceActionKind = ""
	knowledgeMaintenanceActionDisableSensitiveSources       knowledgeMaintenanceActionKind = "disable_sensitive_sources"
	knowledgeMaintenanceActionRebuildDerivedGaps            knowledgeMaintenanceActionKind = "rebuild_derived_gaps"
	knowledgeMaintenanceActionBackfillLabels                knowledgeMaintenanceActionKind = "backfill_labels"
	knowledgeMaintenanceActionRefreshTopicLinks             knowledgeMaintenanceActionKind = "refresh_topic_links"
	knowledgeMaintenanceActionSuppressDuplicateGroups       knowledgeMaintenanceActionKind = "suppress_duplicate_groups"
	knowledgeMaintenanceActionRefreshOrReimportMissingNodes knowledgeMaintenanceActionKind = "refresh_or_reimport_missing_nodes"
)

func normalizeKnowledgeMaintenanceActionKind(kind string) knowledgeMaintenanceActionKind {
	switch knowledgeMaintenanceActionKind(strings.TrimSpace(kind)) {
	case knowledgeMaintenanceActionDisableSensitiveSources:
		return knowledgeMaintenanceActionDisableSensitiveSources
	case knowledgeMaintenanceActionRebuildDerivedGaps:
		return knowledgeMaintenanceActionRebuildDerivedGaps
	case knowledgeMaintenanceActionBackfillLabels:
		return knowledgeMaintenanceActionBackfillLabels
	case knowledgeMaintenanceActionRefreshTopicLinks:
		return knowledgeMaintenanceActionRefreshTopicLinks
	case knowledgeMaintenanceActionSuppressDuplicateGroups:
		return knowledgeMaintenanceActionSuppressDuplicateGroups
	case knowledgeMaintenanceActionRefreshOrReimportMissingNodes:
		return knowledgeMaintenanceActionRefreshOrReimportMissingNodes
	default:
		return knowledgeMaintenanceActionUnknown
	}
}

func (kind knowledgeMaintenanceActionKind) IsExecutable() bool {
	switch kind {
	case knowledgeMaintenanceActionDisableSensitiveSources,
		knowledgeMaintenanceActionRebuildDerivedGaps,
		knowledgeMaintenanceActionBackfillLabels,
		knowledgeMaintenanceActionRefreshTopicLinks,
		knowledgeMaintenanceActionSuppressDuplicateGroups,
		knowledgeMaintenanceActionRefreshOrReimportMissingNodes:
		return true
	default:
		return false
	}
}

func (kind knowledgeMaintenanceActionKind) ManualReason() string {
	switch kind {
	case knowledgeMaintenanceActionRefreshOrReimportMissingNodes:
		return "requires_refresh_or_reimport_entrypoint"
	case knowledgeMaintenanceActionUnknown:
		return "missing_action_kind"
	default:
		return "unsupported_quality_maintenance_action"
	}
}
