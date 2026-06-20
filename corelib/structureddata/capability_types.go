package structureddata

type DataCapabilities struct {
	Service          string                   `json:"service"`
	Engine           string                   `json:"engine"`
	TenantID         string                   `json:"tenant_id"`
	UserID           string                   `json:"user_id,omitempty"`
	Role             string                   `json:"role,omitempty"`
	APIKeyID         string                   `json:"api_key_id,omitempty"`
	Policy           map[string]any           `json:"policy"`
	Access           AccessCapabilitySummary  `json:"access"`
	Domains          []string                 `json:"domains"`
	AgentPlaybooks   []AgentBusinessPlaybook  `json:"agent_playbooks,omitempty"`
	Relationships    []DatasetRelationship    `json:"relationships,omitempty"`
	BusinessObjects  []BusinessObjectCatalog  `json:"business_objects,omitempty"`
	AppInstallations []AppInstallation        `json:"app_installations,omitempty"`
	Datasets         []DatasetCapability      `json:"datasets"`
	Templates        []DatasetTemplate        `json:"templates"`
	BusinessActions  []BusinessAction         `json:"business_actions"`
	EventContracts   []EventContract          `json:"event_contracts,omitempty"`
	Connectors       []ExternalConnector      `json:"connectors,omitempty"`
	BusinessViews    []BusinessViewDefinition `json:"business_views"`
	Dashboards       []DashboardDefinition    `json:"dashboards"`
	Reports          []ReportDefinition       `json:"reports"`
	QualityChecks    []QualityCheckDefinition `json:"quality_checks"`
	BusinessRules    []BusinessRuleDefinition `json:"business_rules,omitempty"`
	ToolActions      []ToolActionCapability   `json:"tool_actions"`
}

type DatasetCapability struct {
	Dataset Dataset           `json:"dataset"`
	Fields  []FieldDefinition `json:"fields,omitempty"`
}

type ToolActionCapability struct {
	Action      string   `json:"action"`
	Purpose     string   `json:"purpose"`
	Preferred   bool     `json:"preferred,omitempty"`
	Requires    []string `json:"requires,omitempty"`
	AdminOnly   bool     `json:"admin_only,omitempty"`
	Description string   `json:"description,omitempty"`
}

type AccessCapabilitySummary struct {
	AuthenticatedBy        string         `json:"authenticated_by"`
	ScopeMode              string         `json:"scope_mode"`
	BusinessOperationFirst bool           `json:"business_operation_first"`
	RawDatasetAllowed      bool           `json:"raw_dataset_allowed"`
	SensitiveAllowed       bool           `json:"sensitive_allowed"`
	AdminAllowed           bool           `json:"admin_allowed"`
	AllowedDomains         []string       `json:"allowed_domains,omitempty"`
	AllowedDatasets        []string       `json:"allowed_datasets,omitempty"`
	AllowedActions         []string       `json:"allowed_actions,omitempty"`
	AllowedViews           []string       `json:"allowed_views,omitempty"`
	AllowedReports         []string       `json:"allowed_reports,omitempty"`
	AllowedDashboards      []string       `json:"allowed_dashboards,omitempty"`
	VisibleCounts          map[string]int `json:"visible_counts"`
	Guardrails             []string       `json:"guardrails"`
	RecommendedNextActions []string       `json:"recommended_next_actions"`
}

type BusinessRuleDefinition struct {
	ID                string                  `json:"id"`
	Domain            string                  `json:"domain,omitempty"`
	DatasetID         string                  `json:"dataset_id,omitempty"`
	BusinessAction    string                  `json:"business_action_id,omitempty"`
	Title             string                  `json:"title"`
	Description       string                  `json:"description,omitempty"`
	Severity          string                  `json:"severity,omitempty"`
	RequiresDryRun    bool                    `json:"requires_dry_run,omitempty"`
	RequiresApproval  bool                    `json:"requires_approval,omitempty"`
	ApprovalKind      string                  `json:"approval_kind,omitempty"`
	RequiresBackup    bool                    `json:"requires_backup,omitempty"`
	RequiresQuality   bool                    `json:"requires_quality,omitempty"`
	RequiresAdmin     bool                    `json:"requires_admin,omitempty"`
	ConditionsMode    string                  `json:"conditions_mode,omitempty"`
	Conditions        []BusinessRuleCondition `json:"conditions,omitempty"`
	DefaultApprover   string                  `json:"default_approver,omitempty"`
	RecommendedChecks []string                `json:"recommended_checks,omitempty"`
	ToolCallTemplates []map[string]any        `json:"tool_call_templates,omitempty"`
}

type BusinessRuleCondition struct {
	Field       string `json:"field,omitempty"`
	Op          string `json:"op"`
	Value       any    `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
}

type QueryBusinessRulesInput struct {
	Domain         string `json:"domain,omitempty"`
	DatasetID      string `json:"dataset_id,omitempty"`
	BusinessAction string `json:"business_action_id,omitempty"`
	Severity       string `json:"severity,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	BeforeID       string `json:"before_id,omitempty"`
}

type EvaluateBusinessRulesInput struct {
	Domain         string         `json:"domain,omitempty"`
	DatasetID      string         `json:"dataset_id,omitempty"`
	BusinessAction string         `json:"business_action_id,omitempty"`
	RecordID       string         `json:"record_id,omitempty"`
	DryRun         bool           `json:"dry_run,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}

type BusinessRuleEvaluation struct {
	BusinessAction    string                   `json:"business_action_id,omitempty"`
	DatasetID         string                   `json:"dataset_id,omitempty"`
	Domain            string                   `json:"domain,omitempty"`
	DryRun            bool                     `json:"dry_run,omitempty"`
	GovernanceStatus  string                   `json:"governance_status"`
	StatusReasons     []string                 `json:"status_reasons,omitempty"`
	CanExecuteNow     bool                     `json:"can_execute_now"`
	RecommendedAction string                   `json:"recommended_action,omitempty"`
	ApprovalKind      string                   `json:"approval_kind,omitempty"`
	ApprovalID        string                   `json:"approval_id,omitempty"`
	ApprovalStatus    string                   `json:"approval_status,omitempty"`
	GateStatuses      []BusinessRuleGateStatus `json:"gate_statuses,omitempty"`
	RuleEvaluations   []BusinessRuleMatch      `json:"rule_evaluations,omitempty"`
	MatchedRules      []BusinessRuleDefinition `json:"matched_rules"`
	RequiresDryRun    bool                     `json:"requires_dry_run,omitempty"`
	RequiresApproval  bool                     `json:"requires_approval,omitempty"`
	RequiresBackup    bool                     `json:"requires_backup,omitempty"`
	RequiresQuality   bool                     `json:"requires_quality,omitempty"`
	RequiresAdmin     bool                     `json:"requires_admin,omitempty"`
	RecommendedChecks []string                 `json:"recommended_checks,omitempty"`
	Summary           string                   `json:"summary,omitempty"`
	NextSteps         []BusinessIntentNextStep `json:"next_steps,omitempty"`
}

type BusinessRuleGateStatus struct {
	Gate        string `json:"gate"`
	Status      string `json:"status"`
	Action      string `json:"action,omitempty"`
	Description string `json:"description,omitempty"`
}

type BusinessRuleMatch struct {
	RuleID           string                            `json:"rule_id"`
	Applies          bool                              `json:"applies"`
	ConditionsMode   string                            `json:"conditions_mode,omitempty"`
	ConditionResults []BusinessRuleConditionEvaluation `json:"condition_results,omitempty"`
}

type BusinessRuleConditionEvaluation struct {
	Condition BusinessRuleCondition `json:"condition"`
	Matched   bool                  `json:"matched"`
	Actual    any                   `json:"actual,omitempty"`
	Reason    string                `json:"reason,omitempty"`
}

type AgentBusinessPlaybook struct {
	ID          string                   `json:"id"`
	Domain      string                   `json:"domain"`
	Title       string                   `json:"title"`
	Description string                   `json:"description,omitempty"`
	IntentHints []string                 `json:"intent_hints,omitempty"`
	Policy      string                   `json:"policy"`
	UseCase     BusinessDomainUseCase    `json:"use_case"`
	Steps       []BusinessIntentNextStep `json:"steps"`
}
