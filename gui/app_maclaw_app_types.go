package main

type maclawAppSubmissionRecord struct {
	SubmissionID    string                           `json:"submission_id"`
	HubCapabilityID string                           `json:"hub_capability_id,omitempty"`
	SubmittedAt     string                           `json:"submitted_at"`
	Status          string                           `json:"status"`
	Channel         string                           `json:"channel"`
	AppIDs          []string                         `json:"app_ids"`
	AppNames        []string                         `json:"app_names,omitempty"`
	PackageSHA      string                           `json:"package_sha256,omitempty"`
	PackageSize     int                              `json:"package_bytes,omitempty"`
	ReviewedAt      string                           `json:"reviewed_at,omitempty"`
	PublishedAt     string                           `json:"published_at,omitempty"`
	Reviewer        string                           `json:"reviewer,omitempty"`
	RiskLevel       string                           `json:"risk_level,omitempty"`
	ApprovedScopes  []string                         `json:"approved_scopes,omitempty"`
	ReviewIssues    []maclawAppReviewIssue           `json:"review_issues,omitempty"`
	ReviewEvidence  map[string]any                   `json:"review_evidence,omitempty"`
	Dependencies    []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
	Events          []maclawAppSubmissionEvent       `json:"events,omitempty"`
	Package         map[string]any                   `json:"package"`
	Message         string                           `json:"message"`
}

type maclawAppSubmissionQueue struct {
	Schema      string                      `json:"schema"`
	UpdatedAt   string                      `json:"updated_at"`
	Submissions []maclawAppSubmissionRecord `json:"submissions"`
}

type maclawAppSubmissionSummary struct {
	SubmissionID    string                           `json:"submission_id"`
	HubCapabilityID string                           `json:"hub_capability_id,omitempty"`
	SubmittedAt     string                           `json:"submitted_at"`
	Status          string                           `json:"status"`
	Channel         string                           `json:"channel"`
	AppIDs          []string                         `json:"app_ids"`
	AppNames        []string                         `json:"app_names,omitempty"`
	PackageSHA      string                           `json:"package_sha,omitempty"`
	PackageSHA256   string                           `json:"package_sha256,omitempty"`
	PackageSize     int                              `json:"package_bytes,omitempty"`
	ReviewedAt      string                           `json:"reviewed_at,omitempty"`
	PublishedAt     string                           `json:"published_at,omitempty"`
	Reviewer        string                           `json:"reviewer,omitempty"`
	RiskLevel       string                           `json:"risk_level,omitempty"`
	ApprovedScopes  []string                         `json:"approved_scopes,omitempty"`
	ReviewIssues    []maclawAppReviewIssue           `json:"review_issues,omitempty"`
	Dependencies    []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
	Evidence        map[string]any                   `json:"submission_evidence,omitempty"`
	ReviewEvidence  map[string]any                   `json:"review_evidence,omitempty"`
	EventCount      int                              `json:"event_count,omitempty"`
	LastEventAt     string                           `json:"last_event_at,omitempty"`
	Message         string                           `json:"message"`
}

type maclawAppReviewIssue struct {
	Path       string         `json:"path,omitempty"`
	Severity   string         `json:"severity,omitempty"`
	Message    string         `json:"message"`
	Suggestion string         `json:"suggestion,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type maclawAppSubmissionEvent struct {
	At           string `json:"at"`
	Status       string `json:"status"`
	Channel      string `json:"channel"`
	SubmissionID string `json:"submission_id"`
	Message      string `json:"message,omitempty"`
	Reviewer     string `json:"reviewer,omitempty"`
}

type maclawAppSubmissionStatusUpdate struct {
	Status          string                 `json:"status"`
	HubCapabilityID string                 `json:"hub_capability_id"`
	Channel         string                 `json:"channel"`
	Message         string                 `json:"message"`
	SubmissionID    string                 `json:"submission_id"`
	ReviewedAt      string                 `json:"reviewed_at"`
	PublishedAt     string                 `json:"published_at"`
	Reviewer        string                 `json:"reviewer"`
	RiskLevel       string                 `json:"risk_level"`
	ApprovedScopes  []string               `json:"approved_scopes"`
	ReviewIssues    []maclawAppReviewIssue `json:"review_issues"`
	ReviewEvidence  map[string]any         `json:"review_evidence"`
}

type maclawAppHubSubmissionResponse struct {
	Schema        string `json:"schema"`
	Status        string `json:"status"`
	PackageSHA256 string `json:"package_sha256"`
	AppCount      int    `json:"app_count"`
	// SubmissionID / CapabilityID are package-level primary identifiers (Hub protocol v1+).
	// Prefer these over package_sha256, which must never be used as a submission identity.
	SubmissionID string                         `json:"submission_id,omitempty"`
	CapabilityID string                         `json:"capability_id,omitempty"`
	Submissions  []maclawAppHubSubmissionResult `json:"submissions"`
}

type maclawAppHubSubmissionResult struct {
	SubmissionID string `json:"submission_id"`
	CapabilityID string `json:"capability_id"`
	AppID        string `json:"app_id"`
	AppName      string `json:"app_name"`
	Status       string `json:"status"`
	VersionKey   string `json:"version_key"`
}

type maclawAppHubCapabilityDetail struct {
	ID                string `json:"id"`
	CapabilityID      string `json:"capability_id"`
	Status            string `json:"status"`
	CurrentVersionKey string `json:"current_version_key"`
	MetadataJSON      string `json:"metadata_json"`
}

type maclawAppInstallPlan struct {
	Schema                   string                           `json:"schema"`
	Apps                     []maclawAppInstallPlanApp        `json:"apps"`
	Dependencies             []maclawAppInstallPlanDependency `json:"dependencies"`
	WorkflowContractIssues   []maclawAppReviewIssue           `json:"workflow_contract_issues,omitempty"`
	GovernanceReviewIssues   []maclawAppReviewIssue           `json:"governance_review_issues,omitempty"`
	HasMissingRequired       bool                             `json:"has_missing_required"`
	HasBlockingDependency    bool                             `json:"has_blocking_dependency,omitempty"`
	HasWorkflowContractIssue bool                             `json:"has_workflow_contract_issue,omitempty"`
	HasGovernanceReviewIssue bool                             `json:"has_governance_review_issue,omitempty"`
}

type maclawAppInstallPlanApp struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Schema string `json:"schema,omitempty"`
}

type maclawAppInstallPlanDependency struct {
	ID                  string   `json:"id"`
	SkillID             string   `json:"skill_id,omitempty"` // publisher.skill-name stable identifier
	Version             string   `json:"version,omitempty"`
	Kind                string   `json:"kind,omitempty"`
	Required            bool     `json:"required"`
	Source              string   `json:"source,omitempty"`
	InstallRef          string   `json:"install_ref,omitempty"`
	CanonicalID         string   `json:"canonical_id,omitempty"`
	Aliases             []string `json:"aliases,omitempty"`
	InstallRefKind      string   `json:"install_ref_kind,omitempty"`
	InstallRefTarget    string   `json:"install_ref_target,omitempty"`
	InstallRefVersion   string   `json:"install_ref_version,omitempty"`
	InstallRefStatus    string   `json:"install_ref_status,omitempty"`
	InstallRefMessage   string   `json:"install_ref_message,omitempty"`
	InstallErrorCode    string   `json:"install_error_code,omitempty"`
	InstallErrorStage   string   `json:"install_error_stage,omitempty"`
	InstallErrorDetail  string   `json:"install_error_detail,omitempty"`
	PreflightStatus     string   `json:"preflight_status,omitempty"`
	PreflightCode       string   `json:"preflight_code,omitempty"`
	PreflightStage      string   `json:"preflight_stage,omitempty"`
	PreflightMessage    string   `json:"preflight_message,omitempty"`
	PackageSHA256       string   `json:"package_sha256,omitempty"`
	PackageChecksum     string   `json:"package_checksum,omitempty"`
	PackageSignature    string   `json:"package_signature,omitempty"`
	PackageDownloadURL  string   `json:"package_download_url,omitempty"`
	DownloadNode        string   `json:"download_node,omitempty"`         // HubCenter base that served the package (after failover)
	ResolvedDownloadURL string   `json:"resolved_download_url,omitempty"` // full URL actually used
	IntegrityStatus     string   `json:"integrity_status,omitempty"`
	IntegrityCode       string   `json:"integrity_code,omitempty"`
	IntegrityStage      string   `json:"integrity_stage,omitempty"`
	IntegrityMessage    string   `json:"integrity_message,omitempty"`
	AppIDs              []string `json:"app_ids,omitempty"`
	Installed           bool     `json:"installed"`
	InstalledName       string   `json:"installed_name,omitempty"`
	InstalledVersion    string   `json:"installed_version,omitempty"`
	RequiredVersion     string   `json:"required_version,omitempty"`
	VersionStatus       string   `json:"version_status,omitempty"`
	// RuntimeSkillRef is the preferred RunNLSkillAsync / run_skill argument after
	// local resolution (usually HubSkillID, else SkillID, else Name). Authoring,
	// install planning, and app runtime must share this coordinate.
	RuntimeSkillRef string `json:"runtime_skill_ref,omitempty"`
	InstalledDir    string `json:"installed_dir,omitempty"`
	InstalledStatus string `json:"installed_status,omitempty"`
	Health          string `json:"health,omitempty"`
	Action          string `json:"action"`
	Message         string `json:"message,omitempty"`
}

type maclawAppBundledDependencies struct {
	Schema string                       `json:"schema"`
	Skills []maclawAppBundledSkillEntry `json:"skills,omitempty"`
}

type maclawAppBundledSkillEntry struct {
	StableID    string            `json:"stable_id"`
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name"`
	Version     string            `json:"version,omitempty"`
	Source      string            `json:"source,omitempty"`
	HubSkillID  string            `json:"hub_skill_id,omitempty"`
	HubVersion  string            `json:"hub_version,omitempty"`
	CanonicalID string            `json:"canonical_id,omitempty"`
	SHA256      string            `json:"sha256"`
	Files       map[string]string `json:"files"`
	AppIDs      []string          `json:"app_ids,omitempty"`
}

type maclawAppInstallSkillVersionSnapshot struct {
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Source  string `json:"source,omitempty"`
}

type maclawAppInstallApprovalBindingSnapshot struct {
	Event           string `json:"event,omitempty"`
	DatasetID       string `json:"dataset_id,omitempty"`
	BlueprintID     string `json:"blueprint_id,omitempty"`
	ObjectRole      string `json:"object_role,omitempty"`
	WorkflowSkillID string `json:"workflow_skill_id,omitempty"`
	WorkflowVersion string `json:"workflow_version,omitempty"`
}

type maclawAppInstallVersionSnapshot struct {
	AppEntryVersion  string                                    `json:"app_entry_version,omitempty"`
	AppSkill         *maclawAppInstallSkillVersionSnapshot     `json:"app_skill,omitempty"`
	WorkflowSkills   []maclawAppInstallSkillVersionSnapshot    `json:"workflow_skills,omitempty"`
	ApprovalBindings []maclawAppInstallApprovalBindingSnapshot `json:"approval_bindings,omitempty"`
}

type parsedMaclawAppEntry struct {
	Schema string
	Entry  map[string]any
	App    map[string]any
	ID     string
	Name   string
	Kind   string
}

type maclawAppDataSrvInstallationPayload struct {
	AppID            string
	RoleBindingCount int
	Body             map[string]interface{}
}

type MaclawAppBusinessOperationInput struct {
	AppID              string         `json:"app_id"`
	AppName            string         `json:"app_name,omitempty"`
	DatasetID          string         `json:"dataset_id,omitempty"`
	ObjectRole         string         `json:"object_role,omitempty"`
	BlueprintID        string         `json:"blueprint_id,omitempty"`
	BusinessEntity     string         `json:"business_entity,omitempty"`
	BusinessAction     string         `json:"business_action,omitempty"`
	BusinessNote       string         `json:"business_note,omitempty"`
	PreferredAction    string         `json:"preferred_action,omitempty"`
	PreferredView      string         `json:"preferred_view,omitempty"`
	PreferredReport    string         `json:"preferred_report,omitempty"`
	PreferredDashboard string         `json:"preferred_dashboard,omitempty"`
	Data               map[string]any `json:"data,omitempty"`
	Filter             map[string]any `json:"filter,omitempty"`
	Limit              int            `json:"limit,omitempty"`
	DryRun             bool           `json:"dry_run,omitempty"`
}

type maclawAppBusinessOperationInput = MaclawAppBusinessOperationInput

type maclawAppInstallRecord struct {
	AppID                  string                           `json:"app_id"`
	AppName                string                           `json:"app_name,omitempty"`
	Kind                   string                           `json:"kind,omitempty"`
	Source                 string                           `json:"source,omitempty"`
	InstalledAt            string                           `json:"installed_at"`
	PackageSHA             string                           `json:"package_sha256,omitempty"`
	PackageSize            int                              `json:"package_bytes,omitempty"`
	Package                map[string]any                   `json:"package,omitempty"`
	Dependencies           []maclawAppInstallPlanDependency `json:"dependencies,omitempty"`
	VersionSnapshot        maclawAppInstallVersionSnapshot  `json:"version_snapshot,omitempty"`
	WorkflowContract       map[string]any                   `json:"workflow_contract,omitempty"`
	WorkspaceLayout        map[string]any                   `json:"workspace_layout,omitempty"`
	ResultContract         map[string]any                   `json:"result_contract,omitempty"`
	ReviewEvidence         map[string]any                   `json:"review_evidence,omitempty"`
	Submission             map[string]any                   `json:"submission,omitempty"`
	TestEvidence           map[string]any                   `json:"test_evidence,omitempty"`
	DependencyVerification map[string]any                   `json:"dependency_verification,omitempty"`
	DataSrvRegistration    map[string]any                   `json:"datasrv_registration,omitempty"`
	HasMissingRequired     bool                             `json:"has_missing_required"`
	HasBlockingDependency  bool                             `json:"has_blocking_dependency,omitempty"`
	Message                string                           `json:"message,omitempty"`
}

type maclawAppInstallRegistry struct {
	Schema    string                   `json:"schema"`
	UpdatedAt string                   `json:"updated_at"`
	Installs  []maclawAppInstallRecord `json:"installs"`
}

type maclawAppApprovalInstance struct {
	AppID               string                      `json:"app_id"`
	AppName             string                      `json:"app_name,omitempty"`
	BlueprintID         string                      `json:"blueprint_id,omitempty"`
	DatasetID           string                      `json:"dataset_id,omitempty"`
	ObjectRole          string                      `json:"object_role,omitempty"`
	ApprovalObjectRole  string                      `json:"approval_object_role,omitempty"`
	ApprovalEvent       string                      `json:"approval_event,omitempty"`
	ApprovalWorkflowID  string                      `json:"approval_workflow_id,omitempty"`
	// ApprovalEngine is the authority for node advancement: "hub" or "local".
	// hub = Hub WorkflowExecutor is source of truth; local = desktop-only projection.
	ApprovalEngine string `json:"approval_engine,omitempty"`
	// HubWorkflowID / HubInstanceID / HubNodeID bind this App projection to a Hub runtime instance.
	HubWorkflowID string `json:"hub_workflow_id,omitempty"`
	HubInstanceID string `json:"hub_instance_id,omitempty"`
	HubNodeID     string `json:"hub_node_id,omitempty"`
	// HubSyncError records the last Hub trigger/decision failure without inventing a final business status.
	HubSyncError        string                      `json:"hub_sync_error,omitempty"`
	InstanceID          string                      `json:"instance_id"`
	Title               string                      `json:"title"`
	Lane                string                      `json:"lane"`
	Status              string                      `json:"status"`
	CurrentNode         string                      `json:"current_node"`
	CurrentNodeStatus   string                      `json:"current_node_status,omitempty"`
	CurrentNodeIDs      []string                    `json:"current_node_ids,omitempty"`
	WorkflowNodeIDs     []string                    `json:"workflow_node_ids,omitempty"`
	NodeTasks           []map[string]any            `json:"node_tasks,omitempty"`
	Owner               string                      `json:"owner"`
	Applicant           string                      `json:"applicant,omitempty"`
	Approver            string                      `json:"approver"`
	CurrentAssignee     string                      `json:"current_assignee,omitempty"`
	CurrentAssigneeType string                      `json:"current_assignee_type,omitempty"`
	CreatedAt           string                      `json:"created_at,omitempty"`
	UpdatedAt           string                      `json:"updated_at"`
	Result              string                      `json:"result"`
	WorkflowSkillID     string                      `json:"workflow_skill_id,omitempty"`
	WorkflowVersion     string                      `json:"workflow_version,omitempty"`
	BusinessStatus      string                      `json:"business_status,omitempty"`
	ResultStatus        string                      `json:"result_status,omitempty"`
	FromStatus          string                      `json:"from_status,omitempty"`
	ToStatus            string                      `json:"to_status,omitempty"`
	WorkflowDecisionID  string                      `json:"workflow_decision_id,omitempty"`
	RecordID            string                      `json:"record_id,omitempty"`
	ApprovalID          string                      `json:"approval_id,omitempty"`
	RecordApprovalID    string                      `json:"record_approval_id,omitempty"`
	DetailURL           string                      `json:"detail_url,omitempty"`
	BusinessEntity      string                      `json:"business_entity,omitempty"`
	BusinessAction      string                      `json:"business_action,omitempty"`
	BusinessNote        string                      `json:"business_note,omitempty"`
	ResultPayload       map[string]any              `json:"result_payload,omitempty"`
	Outputs             []maclawAppApprovalOutput   `json:"outputs,omitempty"`
	Artifacts           []maclawAppApprovalArtifact `json:"artifacts,omitempty"`
	Events              []maclawAppApprovalEvent    `json:"events,omitempty"`
}

type maclawAppApprovalEvent struct {
	At       string `json:"at"`
	Node     string `json:"node,omitempty"`
	Actor    string `json:"actor,omitempty"`
	Decision string `json:"decision,omitempty"`
	Message  string `json:"message,omitempty"`
}

type maclawAppApprovalArtifact struct {
	ID            string `json:"id,omitempty"`
	URI           string `json:"uri,omitempty"`
	Name          string `json:"name,omitempty"`
	Path          string `json:"path,omitempty"`
	MimeType      string `json:"mime_type,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	RemoteURL     string `json:"remote_url,omitempty"`
	Checksum      string `json:"checksum,omitempty"`
	DownloadState string `json:"download_state,omitempty"`
	Status        string `json:"status,omitempty"`
	Presentation  string `json:"presentation,omitempty"`
}

type maclawAppApprovalOutput struct {
	Type       string                     `json:"type,omitempty"`
	Kind       string                     `json:"kind,omitempty"`
	Title      string                     `json:"title,omitempty"`
	Text       string                     `json:"text,omitempty"`
	Status     string                     `json:"status,omitempty"`
	ArtifactID string                     `json:"artifact_id,omitempty"`
	Artifact   *maclawAppApprovalArtifact `json:"artifact,omitempty"`
	Data       map[string]any             `json:"data,omitempty"`
}

type maclawAppApprovalRegistry struct {
	Schema    string                      `json:"schema"`
	UpdatedAt string                      `json:"updated_at"`
	Instances []maclawAppApprovalInstance `json:"instances"`
}

type maclawAppApprovalDataSrvSyncInput struct {
	DatasetID   string                    `json:"dataset_id"`
	ObjectRole  string                    `json:"object_role,omitempty"`
	AppID       string                    `json:"app_id,omitempty"`
	BlueprintID string                    `json:"blueprint_id,omitempty"`
	RecordID    string                    `json:"record_id"`
	ApprovalID  string                    `json:"approval_id,omitempty"`
	Instance    maclawAppApprovalInstance `json:"instance"`
}

type MaclawAppApprovalWorkflowStartInput struct {
	AppID               string         `json:"app_id"`
	AppName             string         `json:"app_name,omitempty"`
	DatasetID           string         `json:"dataset_id,omitempty"`
	ObjectRole          string         `json:"object_role,omitempty"`
	BlueprintID         string         `json:"blueprint_id,omitempty"`
	RecordID            string         `json:"record_id"`
	ApprovalID          string         `json:"approval_id,omitempty"`
	InstanceID          string         `json:"instance_id,omitempty"`
	ContinueFromID      string         `json:"continue_from_instance_id,omitempty"`
	Title               string         `json:"title,omitempty"`
	Applicant           string         `json:"applicant,omitempty"`
	Owner               string         `json:"owner,omitempty"`
	Approver            string         `json:"approver,omitempty"`
	CurrentAssignee     string         `json:"current_assignee,omitempty"`
	CurrentAssigneeType string         `json:"current_assignee_type,omitempty"`
	ApprovalEvent       string         `json:"approval_event,omitempty"`
	WorkflowSkillID     string         `json:"workflow_skill_id,omitempty"`
	WorkflowVersion     string         `json:"workflow_version,omitempty"`
	// HubWorkflowID is the published Hub workflow graph id (source of truth for engine=hub).
	HubWorkflowID string `json:"hub_workflow_id,omitempty"`
	// HubInstanceID / HubNodeID bind to an existing Hub instance (skip re-trigger when set).
	HubInstanceID string `json:"hub_instance_id,omitempty"`
	HubNodeID     string `json:"hub_node_id,omitempty"`
	// TriggerHubWorkflow controls Hub StartInstance. nil = auto (trigger when hub workflow id + hub creds exist).
	TriggerHubWorkflow *bool          `json:"trigger_hub_workflow,omitempty"`
	CurrentNode        string         `json:"current_node,omitempty"`
	CurrentNodeIDs     []string       `json:"current_node_ids,omitempty"`
	WorkflowNodeIDs    []string       `json:"workflow_node_ids,omitempty"`
	BusinessStatus     string         `json:"business_status,omitempty"`
	ResultStatus       string         `json:"result_status,omitempty"`
	FromStatus         string         `json:"from_status,omitempty"`
	ToStatus           string         `json:"to_status,omitempty"`
	BusinessEntity     string         `json:"business_entity,omitempty"`
	BusinessAction     string         `json:"business_action,omitempty"`
	BusinessNote       string         `json:"business_note,omitempty"`
	FormData           map[string]any `json:"form_data,omitempty"`
	BusinessPayload    map[string]any `json:"business_payload,omitempty"`
	ResultPayload      map[string]any `json:"result_payload,omitempty"`
	RunWorkflowSkill   bool           `json:"run_workflow_skill,omitempty"`
	WorkflowRunArgs    map[string]any `json:"workflow_run_args,omitempty"`
}

type maclawAppApprovalWorkflowStartInput = MaclawAppApprovalWorkflowStartInput

type maclawAppWorkspaceUILayoutSource struct {
	path   string
	layout map[string]any
}

type maclawAppDependencyIntegrityMetadata struct {
	PackageSHA256      string
	PackageChecksum    string
	PackageSignature   string
	PackageDownloadURL string
}

type maclawAppDependencyImplicitResolution struct {
	Target     string
	Aliases    []string
	LocalNames []string
	Message    string
}

type maclawAppDependencyAliasRegistryEntry struct {
	Target     string
	Aliases    []string
	LocalNames []string
	Sources    []string
	Kinds      []string
}

type maclawAppResolvedDependencyEntry struct {
	ID                 string
	InstallRef         string
	Source             string
	Version            string
	CanonicalID        string
	Aliases            []string
	AppIDs             []string
	InstallRefKind     string
	InstallRefTarget   string
	InstallRefVersion  string
	PackageSHA256      string
	PackageChecksum    string
	PackageSignature   string
	PackageDownloadURL string
}
