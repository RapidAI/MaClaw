package main

import "strings"

type misDataToolAction string

const (
	misDataToolActionUnknown               misDataToolAction = ""
	misDataToolActionStatus                misDataToolAction = "status"
	misDataToolActionGetCapabilities       misDataToolAction = "get_capabilities"
	misDataToolActionListDomains           misDataToolAction = "list_domains"
	misDataToolActionGetDomain             misDataToolAction = "get_domain"
	misDataToolActionListBusinessObjects   misDataToolAction = "list_business_objects"
	misDataToolActionResolveObjectRole     misDataToolAction = "resolve_object_role"
	misDataToolActionListRelationships     misDataToolAction = "list_relationships"
	misDataToolActionResolveIntent         misDataToolAction = "resolve_intent"
	misDataToolActionGetInbox              misDataToolAction = "get_inbox"
	misDataToolActionGetInboxSummary       misDataToolAction = "get_inbox_summary"
	misDataToolActionGetStats              misDataToolAction = "get_stats"
	misDataToolActionRunMaintenance        misDataToolAction = "run_maintenance"
	misDataToolActionListBusinessActions   misDataToolAction = "list_business_actions"
	misDataToolActionGetBusinessAction     misDataToolAction = "get_business_action"
	misDataToolActionExecuteBusinessAction misDataToolAction = "execute_business_action"
	misDataToolActionListBusinessViews     misDataToolAction = "list_business_views"
	misDataToolActionGetBusinessView       misDataToolAction = "get_business_view"
	misDataToolActionQueryBusinessView     misDataToolAction = "query_business_view"
	misDataToolActionListDashboards        misDataToolAction = "list_dashboards"
	misDataToolActionGetDashboard          misDataToolAction = "get_dashboard"
	misDataToolActionRunDashboard          misDataToolAction = "run_dashboard"
	misDataToolActionListReports           misDataToolAction = "list_reports"
	misDataToolActionGetReport             misDataToolAction = "get_report"
	misDataToolActionRunReport             misDataToolAction = "run_report"
	misDataToolActionListQualityChecks     misDataToolAction = "list_quality_checks"
	misDataToolActionRunQualityCheck       misDataToolAction = "run_quality_check"
	misDataToolActionListDatasets          misDataToolAction = "list_datasets"
	misDataToolActionGetDataset            misDataToolAction = "get_dataset"
	misDataToolActionCreateDataset         misDataToolAction = "create_dataset"
	misDataToolActionDeleteDataset         misDataToolAction = "delete_dataset"
	misDataToolActionListFields            misDataToolAction = "list_fields"
	misDataToolActionUpsertFields          misDataToolAction = "upsert_fields"
	misDataToolActionUpsertRecord          misDataToolAction = "upsert_record"
	misDataToolActionGetRecord             misDataToolAction = "get_record"
	misDataToolActionDeleteRecord          misDataToolAction = "delete_record"
	misDataToolActionQueryRecords          misDataToolAction = "query_records"
	misDataToolActionIngestEvent           misDataToolAction = "ingest_event"
	misDataToolActionListAuditLogs         misDataToolAction = "list_audit_logs"
	misDataToolActionExportAuditLogsCSV    misDataToolAction = "export_audit_logs_csv"
	misDataToolActionListAgentTransactions misDataToolAction = "list_agent_transactions"
	misDataToolActionCreateBackup          misDataToolAction = "create_backup"
	misDataToolActionListBackups           misDataToolAction = "list_backups"
	misDataToolActionGetBackup             misDataToolAction = "get_backup"
	misDataToolActionRestoreBackup         misDataToolAction = "restore_backup"
)

func normalizeMISDataToolAction(action string) misDataToolAction {
	return misDataToolAction(strings.ToLower(strings.TrimSpace(action)))
}

func (action misDataToolAction) String() string {
	return string(action)
}
