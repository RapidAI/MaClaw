import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { CSSProperties, KeyboardEvent } from 'react';
import { CancelNLSkillRun, DownloadSkillRunArtifact, ExecuteMaclawAppBusinessOperation, OpenMaclawAppBusinessWorkspace, OpenMaclawAppApprovalWorkspace, OpenMaclawAppWorkspaceFromInstall, GetMISDataConfig, GetNLSkillRunStatus, GetSkillRunArtifact, ListMaclawAppApprovalInstances, ListMaclawAppApprovalInstancesAll, ListMaclawAppInstalls, ListNLSkills, ListSkillAppManifests, LoadConfig, OpenFileOrShowInFolder, InstallMaclawAppDependencies, InstallMaclawAppPackageFromHub, InstallSelectedMaclawAppPackageFromHub, PlanMaclawAppInstall, RecordMaclawAppApprovalInstance, RecordMaclawAppInstall, StartMaclawAppApprovalWorkflow, SyncMaclawAppApprovalInstanceToDataSrv, OpenSkillRunArtifact, RecordMaclawAppRunEvidenceForSkill, RevealSkillRunArtifact, RunNLSkillAsync, SaveMaclawAppDefinitionForSkill, SearchMixedSkills, ShowItemInFolder, StageSkillAppInputFile, UploadNLSkillToMarket } from '../../../wailsjs/go/main/App';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import {
    clearWorkspaceLaunchIssue,
    formatWorkspaceLaunchError,
    formatWorkspaceLaunchIssue,
    isEnterpriseAppKindForWorkspace,
    setWorkspaceLaunchIssue,
} from './appsWorkspaceLaunch';
import './AppsPage.css';

type AppKind = 'enterprise_approval_app' | 'enterprise_normal_app' | 'tool_app' | 'automation_app';
type StudioTab = 'create' | 'manage' | 'publish';
type AppMoveTarget = -1 | 1 | 'top' | 'bottom';
type AppOperation = 'approval_status' | 'run_history' | 'market';
type ApprovalLaneFilter = 'my_requests' | 'pending_my_approval' | 'handled' | 'attention' | 'all';

type AppAboutInfo = {
    author?: string;
    copyright?: string;
    website?: string;
    email?: string;
};

function normalizeAppAboutInfo(info?: AppAboutInfo): AppAboutInfo | undefined {
    const author = String(info?.author || '').trim();
    const copyright = String(info?.copyright || '').trim();
    const website = String(info?.website || '').trim();
    const email = String(info?.email || '').trim();
    return author || copyright || website || email ? {
        author: author || undefined,
        copyright: copyright || undefined,
        website: website || undefined,
        email: email || undefined,
    } : undefined;
}

type AppEntry = {
    id: string;
    name: string;
    description: string;
    category: string;
    kind: AppKind;
    icon: AppIconName;
    customIconDataUrl?: string;
    accent: string;
    pinned?: boolean;
    recentUsedAt?: string;
    version?: number;
    source: 'builtin' | 'skill' | 'datasrv' | 'market' | 'local';
    manifest?: AppManifestBinding;
    importedRunEvidence?: AppRunHistoryEntry;
    versionSnapshot?: BackendAppInstallVersionSnapshot;
    installEvidence?: BackendAppInstallRecord;
    workflowContract?: AppWorkflowContract;
    marketCapabilityID?: string;
    marketInstallSource?: 'enterprise_hub';
    marketSourceLabel?: string;
    marketReviewEvidence?: Record<string, unknown>;
    studioOrigin?: 'app_studio';
    disabled?: boolean;
    disabledReason?: string;
    disabledSource?: 'local' | 'hub_governance';
    aboutInfo?: AppAboutInfo;
};

type AppIconName = 'receipt' | 'wallet' | 'invoice' | 'warehouse' | 'inventory' | 'customer' | 'users' | 'contract' | 'pdf' | 'shield' | 'sheet' | 'chart' | 'dashboard' | 'database' | 'eraser' | 'truck' | 'calendar' | 'web' | 'sync' | 'bot' | 'shop';

type AppsPageProps = {
    lang?: string;
    onOpenMISDataSettings?: () => void;
    onOpenManual?: () => void;
};
type AppSkillDependency = {
    id: string;
    version?: string;
    kind?: 'app_skill' | 'runtime_skill' | 'workflow_skill' | 'ui_component_skill' | 'connector_skill' | 'policy_skill';
    required?: boolean;
    source?: 'local' | 'hub' | 'market' | 'skillmarket' | 'enterprise_hub' | 'github' | 'builtin';
    install_ref?: string;
    installRef?: string;
    canonical_id?: string;
    canonicalID?: string;
    aliases?: string[];
    capabilities?: string[];
};
type StudioLayoutTemplate = 'classic_split' | 'left_nav' | 'document_workspace' | 'dashboard';
type StudioLayoutDensity = 'comfortable' | 'compact' | 'spacious';
type StudioPrimaryRegion = 'left' | 'center' | 'right';
type StudioOutputRegion = 'right' | 'bottom' | 'modal';
type RuntimeWorkspaceRegion = {
    id: string;
    role: string;
    placement: StudioPrimaryRegion | StudioOutputRegion | 'bottom';
    visible?: boolean;
    order?: number;
};
type RuntimeWorkspaceLayout = {
    template: StudioLayoutTemplate;
    density: StudioLayoutDensity;
    primaryRegion: StudioPrimaryRegion;
    outputRegion: StudioOutputRegion;
    regions: RuntimeWorkspaceRegion[];
};

type BackendAppInstallDependency = {
    id: string;
    version?: string;
    kind?: string;
    required?: boolean;
    source?: string;
    install_ref?: string;
    installRef?: string;
    canonical_id?: string;
    canonicalID?: string;
    aliases?: string[];
    install_ref_kind?: string;
    install_ref_target?: string;
    install_ref_version?: string;
    install_ref_status?: string;
    install_ref_message?: string;
    app_ids?: string[];
    installed?: boolean;
    installed_name?: string;
    installed_version?: string;
    required_version?: string;
    version_status?: 'matched' | 'mismatch' | 'unknown' | string;
    /** Preferred RunNLSkillAsync argument after plan resolution (usually hub_skill_id). */
    runtime_skill_ref?: string;
    runtimeSkillRef?: string;
    RuntimeSkillRef?: string;
    installed_dir?: string;
    installed_status?: string;
    health?: 'ready' | 'missing' | 'disabled' | 'needs_setup' | 'unknown' | string;
    action?: 'skip' | 'install' | 'installed' | 'blocked' | 'failed' | 'optional_missing' | 'optional_unhealthy' | string;
    message?: string;
    install_error_code?: string;
    install_error_stage?: string;
    install_error_detail?: string;
    preflight_status?: string;
    preflight_code?: string;
    preflight_stage?: string;
    preflight_message?: string;
    package_sha256?: string;
    package_checksum?: string;
    package_signature?: string;
    package_download_url?: string;
    /** HubCenter base that actually served the package (after cluster failover). */
    download_node?: string;
    /** Full URL used for the successful (or last) download attempt. */
    resolved_download_url?: string;
    integrity_status?: string;
    integrity_code?: string;
    integrity_stage?: string;
    integrity_message?: string;};

type BackendAppInstallPlan = {
    schema?: string;
    apps?: Array<{ id: string; name?: string; kind?: string; schema?: string }>;
    dependencies?: BackendAppInstallDependency[];
    workflow_contract_issues?: AppReviewIssue[];
    has_workflow_contract_issue?: boolean;
    governance_review_issues?: AppReviewIssue[];
    has_governance_review_issue?: boolean;
    has_missing_required?: boolean;
    has_blocking_dependency?: boolean;
    install_trace?: Record<string, unknown>;
    installTrace?: Record<string, unknown>;
};

type BackendAppInstallSkillVersionSnapshot = {
    id?: string;
    version?: string;
    kind?: string;
    source?: string;
};

type BackendAppInstallApprovalBindingSnapshot = {
    event?: string;
    dataset_id?: string;
    blueprint_id?: string;
    object_role?: string;
    workflow_skill_id?: string;
    workflow_version?: string;
};

type BackendAppInstallVersionSnapshot = {
    app_entry_version?: string;
    app_skill?: BackendAppInstallSkillVersionSnapshot | null;
    workflow_skills?: BackendAppInstallSkillVersionSnapshot[];
    approval_bindings?: BackendAppInstallApprovalBindingSnapshot[];
};

type BackendAppDataSrvRegistrationItem = {
    app_id?: string;
    synced?: boolean;
    reason?: string;
    role_binding_count?: number;
    status?: string;
};

type BackendAppDataSrvRegistration = {
    synced?: boolean;
    eligible_count?: number;
    synced_count?: number;
    failed_count?: number;
    reason?: string;
    items?: BackendAppDataSrvRegistrationItem[];
};

type BackendAppInstallRecord = {
    schema?: string;
    package_sha?: string;
    package_sha256?: string;
    hub_package_signature?: Record<string, unknown>;
    hub_package_signature_algorithm?: string;
    hub_package_signature_fingerprint?: string;
    hub_package_signature_signed_at?: string;
    hub_package_signature_signed_by?: string;
    source?: string;
    installed_at?: string;
    app_count?: number;
    apps?: Array<{ id?: string; name?: string; kind?: string; schema?: string }>;
    dependencies?: BackendAppInstallDependency[];
    has_missing_required?: boolean;
    has_blocking_dependency?: boolean;
    datasrv_registration?: BackendAppDataSrvRegistration;
    version_snapshot?: BackendAppInstallVersionSnapshot;
    app_versions?: Record<string, BackendAppInstallVersionSnapshot>;
    workspace_layout?: Record<string, unknown>;
		result_contract?: Record<string, unknown>;
		workflow_mapping?: Record<string, unknown>;
		workflow_contract?: Record<string, unknown>;
    review_evidence?: Record<string, unknown>;
    submission?: Record<string, unknown>;
		test_evidence?: Record<string, unknown>;
		dependency_verification?: Record<string, unknown>;
		install_evidence?: Record<string, unknown>;
	package?: Record<string, unknown>;
};

type AppWorkspaceLayout = {
    schema: 'maclaw.app.ui.v1';
    generated?: boolean;
    entry?: string;
    layouts?: Record<string, any>;
};

type AppResultContract = {
    schema: 'maclaw.app.result.v1';
    primary: string;
    types: string[];
    outputModes?: string[];
    approvalDecisions?: string[];
    delivery: {
        inlineContent: boolean;
        artifacts: boolean;
        businessRecord: boolean;
        notifications: boolean;
    };
};

type AppTestProtocol = {
    schema: 'maclaw.app.test_protocol.v1';
    fingerprint?: string;
    sampleInput: Record<string, unknown>;
    expectedToolCalls?: Array<Record<string, unknown>>;
    expectedOutput: Record<string, unknown>;
    workflowRequiredInputs?: string[];
    workflowDecisionOutputs?: string[];
    workflowRequiredOutputs?: string[];
    requiredRoles: string[];
    requiredScopes: string[];
    riskLevel: 'low' | 'medium' | 'high' | 'critical' | string;
};

type AppApprovalBinding = {
    event: string;
    workflowSkillId: string;
    workflowVersion?: string;
    objectRole?: string;
};

type AppWorkflowMapping = {
    schema: 'maclaw.app.workflow.v1';
    submitNode: string;
    approvalNode: string;
    resultNode: string;
    attentionNode?: string;
    statusMapping: {
        pending: string;
        approved: string;
        rejected: string;
        attention: string;
        requiresInput?: string;
    };
};

type AppWorkflowContract = {
    schema: 'maclaw.app.workflow_contract.v1';
    workflowSkillId: string;
    workflowVersion?: string;
    objectRole: string;
    requiredInputs: string[];
    decisionOutputs: string[];
    requiredOutputs: string[];
    statusMapping: {
        pending: string;
        approved: string;
        rejected: string;
        attention: string;
        requiresInput?: string;
    };
};

type AppManifestBinding = {
    schema: 'maclaw.app.v1';
    installUnit: 'enterprise_app_pack' | 'skill' | 'mcp' | 'builtin';
    privateMarker: 'x_maclaw_apps';
    entryKind: AppKind;
    launchMode: 'agent_dynamic_ui' | 'fixed_skill_ui' | 'automation_console';
    appSkill?: {
        id: string;
        version?: string;
        source?: AppSkillDependency['source'];
    };
    dependencies?: {
        skills?: AppSkillDependency[];
    };
    ui?: AppWorkspaceLayout;
    resultContract?: AppResultContract;
    testProtocol?: AppTestProtocol;
    workflow?: AppWorkflowMapping;
    mis?: {
        approvalBindings?: AppApprovalBinding[];
    };
    datasrv?: {
        appID?: string;
        domain: string;
        datasetID?: string;
        objectRole?: string;
        blueprintID?: string;
        templateID?: string;
        preferredAction?: string;
        preferredView?: string;
        preferredReport?: string;
        preferredDashboard?: string;
    };
    skill?: {
        id: string;
        appDefinitionFile: string;
        inputMode: 'file' | 'form' | 'mixed';
        multipleFiles?: boolean;
        outputModes?: string[];
        fields?: SkillAppField[];
        paramMapping?: Record<string, SkillParamMappingEntry>;
    };
};

type SkillAppField = {
    name: string;
    label?: string;
    type?: 'text' | 'select' | 'boolean';
    required?: boolean;
    default?: string | boolean;
    options?: string[];
};

/**
 * SkillParamMappingEntry describes how a single skill parameter is sourced from the App UI.
 * - source: "file_path" - use the staged file path
 * - source: "file_paths" - use all staged file paths (joined by comma)
 * - source: "field:<name>" - use a form field value
 * - source: "params" - use the free-text params input
 * - source: "literal" - use a fixed value (from `value` field)
 * - source: "composite" - build from template string with {file_path}, {params}, {field:<name>} placeholders
 */
type SkillParamMappingEntry = {
    source: 'file_path' | 'file_paths' | 'params' | 'literal' | 'composite' | string;
    value?: string;
    description?: string;
};

type SkillRunStatusView = {
    run_id?: string;
    expected_artifact?: boolean;
    total_steps?: number;
    failed_steps?: number;
    skipped_steps?: number;
    steps?: SkillRunStepView[];
    status?: string;
    error?: string;
    summary?: {
        current_step?: string;
        current_step_status?: string;
        last_completed_step?: string;
        last_output_snippet?: string;
        last_error_snippet?: string;
        artifact_path?: string;
        artifact_status?: string;
        artifacts?: SkillRunArtifactView[];
        output_blocks?: SkillRunOutputBlockView[];
        needs_artifact_verification?: boolean;
    };
    artifacts?: SkillRunArtifactView[];
    outputs?: SkillRunOutputBlockView[];
    session_progress?: {
        progress_summary?: string;
        current_task?: string;
        last_result?: string;
        waiting_for_user?: boolean;
    };
};

type SkillRunArtifactView = {
    id?: string;
    artifact_id?: string;
    uri?: string;
    name?: string;
    path?: string;
    mime_type?: string;
    size_bytes?: number;
    remote_url?: string;
    checksum?: string;
    download_state?: string;
    status?: string;
    presentation?: string;
    available?: boolean;
};

type SkillRunOutputBlockView = {
    id?: string;
    type?: string;
    kind?: string;
    title?: string;
    text?: string;
    status?: string;
    artifact_id?: string;
    artifact?: SkillRunArtifactView;
    data?: unknown;
};

type SkillRunStepView = {
	index?: number;
	name?: string;
	action?: string;
	status?: string;
    output?: string;
    error?: string;
	duration_ms?: number;
};

type StructuredBusinessErrorActionView = {
	label?: string;
	action: string;
	args?: Record<string, unknown>;
};

type StructuredBusinessErrorView = {
	code: string;
	message: string;
	actor?: string;
	target?: string;
	required?: string;
	actual?: string;
	nextActions: StructuredBusinessErrorActionView[];
	metadata?: Record<string, unknown>;
};

type SkillAppStagedFileRef = {
	name: string;
	size: number;
	type?: string;
    last_modified?: number;
    staged_path: string;
    transfer: 'staged_file';
};

type AppRunHistoryEntry = {
    runID: string;
    appID: string;
    status: 'done' | 'error' | 'cancelled';
    definitionHash?: string;
    testProtocolFingerprint?: string;
    workspaceLayoutFingerprint?: string;
    outputMode: string;
    inputSummary: string;
    message: string;
    artifactID?: string;
    artifactURI?: string;
    artifactName?: string;
    artifactPath?: string;
    artifactDownloadState?: string;
    artifacts?: SkillRunArtifactView[];
    resultPayload?: Record<string, unknown>;
    outputs?: ApprovalInstanceOutputView[];
    resultCoverage?: Record<string, unknown>;
    dependencyVerification?: AppRunEvidenceDependencyVerification;
    approvalInstance?: AppRunApprovalInstanceEvidence;
    at: string;
};

type AppActiveRunKind = 'tool_skill' | 'business_skill' | 'approval_workflow';

type AppActiveRunSession = {
    appID: string;
    runID: string;
    runKind: AppActiveRunKind;
    inputSummary: string;
    outputMode: string;
    startedAt: string;
    definitionHash?: string;
    approvalInstance?: BackendApprovalInstance;
};

type AppRunApprovalInstanceEvidence = {
    instanceId: string;
    approvalInstanceId?: string;
    approvalID?: string;
    workflowInstanceId?: string;
    status: string;
    lane?: string;
    currentNode?: string;
    currentNodeIDs?: string[];
    datasetID?: string;
    blueprintID?: string;
    objectRole?: string;
    approvalObjectRole?: string;
    approvalEvent?: string;
    approvalWorkflowID?: string;
    workflowSkillId?: string;
    workflowVersion?: string;
    businessStatus?: string;
    resultStatus?: string;
    result?: string;
    recordID?: string;
    detailURL?: string;
    resultPayload?: Record<string, unknown>;
    outputs?: ApprovalInstanceOutputView[];
    artifacts?: ApprovalInstanceArtifactView[];
    viewVerified?: boolean;
    approvalInstanceViewVerified?: boolean;
    approvalViews?: Record<string, unknown>;
    progressInstances?: AppRunApprovalInstanceEvidence[];
    verifiedAt?: string;
};

type AppRunEvidenceDependencyVerification = {
    schema: 'maclaw.app.install_plan.v1';
    verifiedAt: string;
    appCount: number;
    dependencyCount: number;
    hasMissingRequired: boolean;
    hasBlockingDependency: boolean;
    hasWorkflowContractIssue: boolean;
    workflowContractIssueCount: number;
    hasGovernanceReviewIssue: boolean;
    governanceReviewIssueCount: number;
    workflowContractIssues?: AppReviewIssue[];
    governanceReviewIssues?: AppReviewIssue[];
    installTrace?: Record<string, unknown>;
    install_trace?: Record<string, unknown>;
    dependencies: BackendAppInstallDependency[];
};

type BusinessOperationResultKind = 'business_record' | 'business_status' | 'table' | 'dashboard' | 'text' | 'external_receipt' | 'error';

type BusinessOperationResultView = {
    mode: string;
    target: string;
    status: string;
    kind: BusinessOperationResultKind;
    message: string;
    recordCount: number;
    columns: string[];
    rows: Record<string, unknown>[];
    response?: Record<string, unknown>;
    primaryResult?: string;
    resultPayload?: Record<string, unknown>;
    outputs?: ApprovalInstanceOutputView[];
    artifacts?: SkillRunArtifactView[];
};

type ApprovalInstanceEventView = {
	at: string;
	node?: string;
	actor?: string;
	decision?: string;
	action?: string;
	message?: string;
	metadata?: Record<string, unknown>;
};

type ApprovalInstanceArtifactView = SkillRunArtifactView;

type ApprovalInstanceOutputView = {
    type?: string;
    kind?: string;
    title?: string;
    text?: string;
    status?: string;
    artifact_id?: string;
    artifact?: ApprovalInstanceArtifactView;
    data?: Record<string, unknown>;
};

type ApprovalInstanceView = {
    id: string;
    appID?: string;
    appName?: string;
    blueprintID?: string;
    datasetID?: string;
    objectRole?: string;
    approvalEvent?: string;
    approvalWorkflowID?: string;
    approvalID?: string;
    title: string;
    lane: 'my_requests' | 'pending_my_approval' | 'handled' | 'attention';
    status: 'draft' | 'pending' | 'approved' | 'rejected' | 'attention' | 'requires_input' | 'failed' | 'cancelled' | 'timeout' | 'running' | 'submitted' | 'in_progress';
    currentNode: string;
    currentNodeStatus?: string;
    currentNodeIDs?: string[];
    nodeTasks?: Record<string, unknown>[];
    owner: string;
    applicant?: string;
    approver: string;
    currentAssignee?: string;
    currentAssigneeType?: string;
    updatedAt: string;
    result: string;
    workflowSkillID?: string;
    workflowVersion?: string;
    workflowDecisionID?: string;
    businessStatus?: string;
    resultStatus?: string;
    fromStatus?: string;
    toStatus?: string;
    recordID?: string;
    detailURL?: string;
    resultPayload?: Record<string, unknown>;
    outputs?: ApprovalInstanceOutputView[];
    artifacts?: ApprovalInstanceArtifactView[];
    events?: ApprovalInstanceEventView[];
    amount?: string;
};

type BackendApprovalInstance = {
    app_id?: string;
    appID?: string;
    app_name?: string;
    appName?: string;
    blueprint_id?: string;
    blueprintID?: string;
    dataset_id?: string;
    datasetID?: string;
    object_role?: string;
    objectRole?: string;
    instance_id?: string;
    instanceID?: string;
    instanceId?: string;
    title?: string;
    lane?: string;
    status?: string;
    current_node?: string;
    currentNode?: string;
    current_node_status?: string;
    currentNodeStatus?: string;
    node_status?: string;
    nodeStatus?: string;
    current_node_ids?: string[];
    currentNodeIDs?: string[];
    workflow_node_ids?: string[];
    workflowNodeIDs?: string[];
    node_tasks?: Record<string, unknown>[];
    nodeTasks?: Record<string, unknown>[];
    approval_tasks?: Record<string, unknown>[];
    approvalTasks?: Record<string, unknown>[];
    owner?: string;
    applicant?: string;
    submitted_by?: string;
    submittedBy?: string;
    approver?: string;
    current_assignee?: string;
    currentAssignee?: string;
    current_assignee_type?: string;
    currentAssigneeType?: string;
    updated_at?: string;
    updatedAt?: string;
    result?: string;
    workflow_skill_id?: string;
    workflowSkillID?: string;
    workflowSkillId?: string;
    workflow_version?: string;
    workflowVersion?: string;
    workflow_decision_id?: string;
    workflowDecisionID?: string;
    workflowDecisionId?: string;
    approval_event?: string;
    approvalEvent?: string;
    approval_workflow_id?: string;
    approvalWorkflowID?: string;
    approvalWorkflowId?: string;
    approval_object_role?: string;
    approvalObjectRole?: string;
    business_status?: string;
    businessStatus?: string;
    result_status?: string;
    resultStatus?: string;
    from_status?: string;
    fromStatus?: string;
    to_status?: string;
    toStatus?: string;
    business_entity?: string;
    businessEntity?: string;
    business_action?: string;
    businessAction?: string;
    business_note?: string;
    businessNote?: string;
    created_at?: string;
    createdAt?: string;
	events?: Array<{ at?: string; node?: string; actor?: string; decision?: string; message?: string; action?: string; note?: string; metadata?: Record<string, unknown> }>;
    record_id?: string;
    recordID?: string;
    recordId?: string;
    approval_id?: string;
    approvalID?: string;
    approvalId?: string;
    record_approval_id?: string;
    recordApprovalID?: string;
    recordApprovalId?: string;
    detail_url?: string;
    detailURL?: string;
    detailUrl?: string;
    result_payload?: Record<string, unknown>;
    resultPayload?: Record<string, unknown>;
    outputs?: ApprovalInstanceOutputView[];
    artifacts?: ApprovalInstanceArtifactView[];
};

type ApprovalRunContext = {
    instance: BackendApprovalInstance;
    lastProgressKey?: string;
};

type ApprovalWorkflowCompletion = {
    status: 'approved' | 'rejected' | 'attention' | 'cancelled' | 'timeout';
    lane: ApprovalInstanceView['lane'];
    currentNode: string;
    resultText: string;
    businessStatus: string;
    resultStatus: string;
    detailURL?: string;
    recordID?: string;
    resultPayload?: Record<string, unknown>;
    outputs?: ApprovalInstanceOutputView[];
    artifacts?: ApprovalInstanceArtifactView[];
    currentNodeIDs?: string[];
    eventAction: string;
};

type AppLayoutState = {
    orderedIds?: string[];
    pinnedIds?: string[];
    hiddenIds?: string[];
    editedApps?: AppEntry[];
    customApps?: AppEntry[];
    recentUsedAtById?: Record<string, string>;
    disabledIds?: string[];
    disabledReasonsById?: Record<string, string>;
    disabledSourcesById?: Record<string, 'local' | 'hub_governance'>;
};

type AppPublishStatus = 'submitted' | 'pending_review' | 'review_failed' | 'approved' | 'published' | 'deprecated' | 'revoked';

type AppReviewIssue = {
    path?: string;
    severity?: string;
    message: string;
    suggestion?: string;
    metadata?: Record<string, unknown>;
};

type AppInstallResultItem = {
    key: string;
    appID?: string;
    dataSrvCandidate?: boolean;
    name: string;
    icon: AppIconName;
    customIconDataUrl?: string;
    accent: string;
	action: 'installed' | 'upgraded' | 'skipped';
	detail: string;
	versionSnapshot?: BackendAppInstallVersionSnapshot;
	installEvidence?: BackendAppInstallRecord;
};

type ApprovedHubAppInstallResult = {
    appIDs: string[];
    plan?: BackendAppInstallPlan | null;
    installRecord?: BackendAppInstallRecord | null;
    versionSnapshot?: BackendAppInstallVersionSnapshot;
    installEvidence?: BackendAppInstallRecord;
};

type AppGovernanceOverrides = {
    dependencyVerification?: BackendAppInstallPlan;
};

type AppPublishSubmission = {
    id: string;
    appID: string;
    submittedAt: string;
    status: AppPublishStatus;
    channel?: 'local' | 'hub';
    reviewedAt?: string;
    publishedAt?: string;
    reviewer?: string;
    riskLevel?: string;
    approvedScopes?: string[];
    reviewIssues?: AppReviewIssue[];
    modifiedAt?: string;
    version?: number;
    message?: string;
};

type AppPackageSubmissionSummary = {
    submissionID: string;
    hubCapabilityID?: string;
    submittedAt: string;
    status: AppPublishStatus | string;
    channel: 'local' | 'hub' | string;
    appIDs: string[];
    appNames: string[];
    packageSHA: string;
    packageBytes: number;
    reviewedAt: string;
    publishedAt: string;
    reviewer: string;
    riskLevel: string;
    approvedScopes: string[];
    reviewIssues: AppReviewIssue[];
    dependencies: BackendAppInstallDependency[];
    submissionEvidence?: BackendAppInstallRecord;
    reviewEvidence?: Record<string, unknown>;
    eventCount: number;
    lastEventAt: string;
    message: string;
};

type MISDataConfig = {
    enabled?: boolean;
    endpoint?: string;
    token?: string;
    user_id?: string;
};

type DataSrvDiscovery = {
    status: 'idle' | 'loading' | 'disabled' | 'ready' | 'error';
    endpoint?: string;
    service?: string;
    candidates: AppEntry[];
    domains: number;
    actions: number;
    views: number;
    reports: number;
    dashboards: number;
    error?: string;
};

type SkillAppDiscovery = {
    status: 'idle' | 'loading' | 'ready' | 'error';
    candidates: AppEntry[];
    error?: string;
};

type SkillAppManifestEntry = {
    id?: string;
    skill_id?: string;
    name?: string;
    description?: string;
    category?: string;
    kind?: AppKind | string;
    icon?: string;
    custom_icon_data_url?: string;
    customIconDataUrl?: string;
    input_mode?: 'file' | 'form' | 'mixed';
    multiple_files?: boolean;
    output_modes?: string[];
    fields?: SkillAppField[];
    app_definition_file?: string;
    app_definition?: Record<string, any>;
};

type SkillSummary = {
    name?: string;
    description?: string;
    source?: string;
    product_kind?: string;
    productKind?: string;
    is_maclaw_app?: boolean;
    isMaclawApp?: boolean;
    capabilities?: string[] | string;
};

type StudioSkillChoice = {
    id: string;
    name: string;
    description?: string;
    source: AppSkillDependency['source'] | 'installed';
    sourceLabel: string;
    installed: boolean;
    productKind?: string;
    isMaclawApp?: boolean;
    capabilities?: string[] | string;
};

const storageKey = 'maclaw:apps-panel:v1';
const runHistoryStorageKey = 'maclaw:apps-run-history:v1';
const activeRunStorageKey = 'maclaw:apps-active-runs:v1';
const publishSubmissionStorageKey = 'maclaw:apps-publish-submissions:v1';
const dismissedSkillAppsStorageKey = 'maclaw:apps-dismissed-skill:v1';
const maxPinnedApps = 8;
const maxSkillAppStagingBytes = 25 * 1024 * 1024;
const maxAppIconUploadBytes = 5 * 1024 * 1024;
const allowedOutputModes = ['docx', 'xlsx', 'pdf', 'json', 'txt'];
const allowedSkillFieldTypes = ['text', 'select', 'boolean'];
let recentUseSequence = 0;
let localAppIdSequence = 0;

const labels = {
    zh: {
        search: '\u641c\u7d22\u5e94\u7528',
        clearSearch: '\u6e05\u7a7a\u641c\u7d22',
        resetFilter: '\u91cd\u7f6e\u7b5b\u9009',
        create: '\u521b\u5efa',
        category: '\u5206\u7c7b',
        operations: '\u64cd\u4f5c',
        approvalStatus: '\u5ba1\u6279\u72b6\u6001',
        approvalStatusHint: '\u6253\u5f00\u5ba1\u6279\u5b9e\u4f8b\u7ba1\u7406',
        runHistoryOps: '\u8fd0\u884c\u8bb0\u5f55',
        runHistoryOpsHint: '\u67e5\u770b\u5e94\u7528\u6267\u884c\u5386\u53f2',
        marketOps: '\u5e94\u7528\u5e02\u573a',
        marketOpsHint: '\u641c\u7d22\u4e0b\u8f7d\u5b89\u88c5\u5e94\u7528',
        all: '\u5168\u90e8\u5e94\u7528',
        otherApps: '\u5176\u4ed6\u5e94\u7528',
        pinned: '\u5e38\u7528\u5e94\u7528',
        recent: '\u6700\u8fd1\u4f7f\u7528',
        searchResults: '\u641c\u7d22\u7ed3\u679c',
        recentUsed: '\u6700\u8fd1\u4f7f\u7528',
        neverUsed: '\u5c1a\u672a\u4f7f\u7528',
        appStatus: '\u72b6\u6001',
        appAvailable: '\u53ef\u7528',
        appOpened: '\u5df2\u6253\u5f00',
        appRunning: '\u8fd0\u884c\u4e2d',
        appDisabled: '\u5df2\u505c\u7528',
        appDisabledReason: '\u4f01\u4e1a\u7ba1\u7406\u5458\u5df2\u505c\u7528\u6b64\u5e94\u7528\uff0c\u5165\u53e3\u548c\u5386\u53f2\u5df2\u4fdd\u7559',
        appHubDeprecatedReason: '\u4f01\u4e1a\u80fd\u529b\u5e02\u573a\u5df2\u505c\u6b62\u6b64\u5e94\u7528\u7684\u65b0\u88c5\u6216\u8fd0\u884c\uff0c\u5165\u53e3\u548c\u5386\u53f2\u5df2\u4fdd\u7559',
        appHubRevokedReason: '\u4f01\u4e1a\u80fd\u529b\u5e02\u573a\u5df2\u64a4\u56de\u6b64\u5e94\u7528\uff0c\u5165\u53e3\u548c\u5386\u53f2\u5df2\u4fdd\u7559',
        apps: '\u5e94\u7528',
        appStudio: 'MaClaw \u5e94\u7528\u5de5\u4f5c\u5ba4',
        studioSubtitle: '\u521b\u5efa\u3001\u7ba1\u7406\u3001\u4ece\u4f01\u4e1a\u80fd\u529b\u5e02\u573a\u6dfb\u52a0\u5e94\u7528\u3002',
        manual: '\u4f7f\u7528\u8bf4\u660e',
        manualAria: '\u6253\u5f00 MaClaw \u5e94\u7528\u5de5\u4f5c\u5ba4\u4f7f\u7528\u8bf4\u660e',
        createTab: '\u521b\u5efa\u5e94\u7528',
        promptDraft: '\u7528\u5bf9\u8bdd\u751f\u6210\u8349\u7a3f',
        generateDraft: '\u751f\u6210\u8349\u7a3f',
        draftPromptPlaceholder: '\u4f8b\uff1a\u505a\u4e00\u4e2a\u5408\u540c\u5f52\u6863\u5e94\u7528\uff0c\u4e0a\u4f20 Word/PDF\uff0c\u8f93\u51fa\u5f52\u6863\u7f16\u53f7\u548c\u5ba1\u6838\u7ed3\u679c',
        manageTab: '\u5e94\u7528\u7ba1\u7406',
        publishTab: '\u5ba1\u6838/\u53d1\u5e03',
        publishSubtitle: '\u68c0\u67e5\u672c\u5730\u5e94\u7528\u662f\u5426\u53ef\u4e0a\u4f20\u5230\u4f01\u4e1a\u80fd\u529b\u5e02\u573a\u3002',
        publishChecklist: '\u53d1\u5e03\u68c0\u67e5',
        readyToSubmit: '\u53ef\u63d0\u4ea4',
        needsWork: '\u9700\u8865\u9f50',
        submitPackage: '\u63d0\u4ea4\u5305\u9884\u89c8',
        copySubmitPackage: '\u590d\u5236\u63d0\u4ea4\u5305',
        noPublishApps: '\u6682\u65e0\u672c\u5730\u5e94\u7528\u53ef\u53d1\u5e03',
        submitReview: '\u63d0\u4ea4\u5ba1\u6838',
        submittedReview: '\u5df2\u63d0\u4ea4',
        pendingReview: '\u7b49\u5f85\u4f01\u4e1a\u5e02\u573a\u5ba1\u6838',
        localReviewPending: '\u672c\u5730\u5f85\u540c\u6b65',
        localModifiedReview: '\u672c\u5730\u5df2\u4fee\u6539\uff0c\u9700\u91cd\u65b0\u63d0\u4ea4',
        localModifiedAt: '\u672c\u5730\u4fee\u6539',
        submitReviewBusy: '\u63d0\u4ea4\u4e2d',
        submitReviewLocalFallback: '\u4f01\u4e1a\u5e02\u573a\u6682\u672a\u8fde\u63a5\uff0c\u5df2\u4fdd\u5b58\u4e3a\u672c\u5730\u5f85\u540c\u6b65\u63d0\u4ea4\u3002',
        localSubmissionQueue: '\u672c\u673a\u63d0\u4ea4\u961f\u5217',
        noLocalSubmissionQueue: '\u6682\u65e0\u672c\u673a\u5f85\u540c\u6b65\u63d0\u4ea4',
        localSubmissionQueueError: '\u63d0\u4ea4\u961f\u5217\u8bfb\u53d6\u5931\u8d25',
        localSubmissionQueueLoading: '\u63d0\u4ea4\u961f\u5217\u8bfb\u53d6\u4e2d',
        reviewEvidence: '\u5ba1\u6838\u8bc1\u636e',
        installGovernance: '\u5b89\u88c5\u6cbb\u7406',
        runtimeEvidence: '\u8fd0\u884c\u8bc1\u636e',
        packageSignature: 'Hub \u5305\u7b7e\u540d',
        approval: '\u5ba1\u6279',
        progress: '\u8fdb\u5ea6',
        workflow: '\u5de5\u4f5c\u6d41',
        refreshQueue: '\u5237\u65b0',
        refreshingQueue: '\u5237\u65b0\u4e2d',
        syncQueueToHub: '\u540c\u6b65\u5230 Hub',
        syncingQueueToHub: '\u540c\u6b65\u4e2d',
        refreshQueueFromHub: '\u5237\u65b0 Hub \u72b6\u6001',
        refreshingQueueFromHub: '\u5237\u65b0\u4e2d',
        queueHubSyncFailed: 'Hub \u540c\u6b65\u5931\u8d25',
        installApprovedHubApp: '\u5b89\u88c5\u5df2\u5ba1\u6838\u5e94\u7528',
        installingApprovedHubApp: '\u5b89\u88c5\u4e2d',
        approvedHubAppInstalled: '\u5df2\u5b89\u88c5',
        approvedHubAppInstallFailed: '\u5b89\u88c5\u5931\u8d25',
        queueRefreshedAt: '\u6700\u540e\u5237\u65b0',
        copyQueuePackage: '\u590d\u5236\u961f\u5217\u5305',
        copyQueueAudit: '\u590d\u5236\u5ba1\u8ba1',
        viewQueueDetail: '\u67e5\u770b\u8be6\u60c5',
        hideQueueDetail: '\u6536\u8d77\u8be6\u60c5',
        queueDetailLoading: '\u8be6\u60c5\u8bfb\u53d6\u4e2d',
        queueDetailTitle: '\u63d0\u4ea4\u8be6\u60c5',
        queueDetailPackageApps: '\u5305\u542b\u5e94\u7528',
        queueDetailEvents: '\u5ba1\u8ba1\u4e8b\u4ef6',
        copyingQueuePackage: '\u590d\u5236\u4e2d',
        queuePackageCopied: '\u5df2\u590d\u5236',
        queueAuditCopied: '\u5ba1\u8ba1\u5df2\u590d\u5236',
        queuePackageUnavailable: '\u65e0\u6cd5\u8bfb\u53d6\u5b8c\u6574\u5305',
        reviewIssues: '\u5ba1\u6838\u95ee\u9898',
        reviewIssuesMore: '\u53e6',
        reviewIssuesMoreUnit: '\u9879',
        fixReviewIssue: '\u53bb\u4fee\u590d',
        resolveReviewDependencies: '\u5904\u7406\u4f9d\u8d56',
        appVersion: '\u7248\u672c',
        reviewer: '\u5ba1\u6838\u4eba',
        riskLevel: '\u98ce\u9669',
        approvedScopes: '\u6279\u51c6\u6743\u9650',
        eventHistory: '\u4e8b\u4ef6',
        reviewFailed: '\u5ba1\u6838\u9700\u4fee\u6539',
        reviewApproved: '\u5ba1\u6838\u901a\u8fc7',
        reviewPublished: '\u5df2\u53d1\u5e03',
        reviewDeprecated: '\u5df2\u505c\u6b62\u65b0\u88c5',
        reviewRevoked: '\u5df2\u64a4\u56de',
        submissionId: '\u63d0\u4ea4\u7f16\u53f7',
        withdrawSubmission: '\u64a4\u56de\u63d0\u4ea4',
        run: '\u6267\u884c',
        reset: '\u91cd\u7f6e',
        output: '\u8f93\u51fa\u7ed3\u679c',
        upload: '\u62d6\u5165\u6216\u9009\u62e9\u6587\u4ef6',
        chooseFile: '\u9009\u62e9\u6587\u4ef6',
        selectedFile: '\u5df2\u9009\u62e9',
        noFile: '\u672a\u9009\u62e9\u6587\u4ef6',
        readyOutput: '\u7b49\u5f85\u6267\u884c',
        generatedOutput: '\u5df2\u751f\u6210\u8f93\u51fa',
        skillRunStarted: 'Skill \u5df2\u542f\u52a8',
        skillRunRunning: 'Skill \u6267\u884c\u4e2d',
        skillRunCompleted: 'Skill \u5df2\u5b8c\u6210',
        skillRunFailed: 'Skill \u6267\u884c\u5931\u8d25',
        skillRunCancelled: 'Skill \u5df2\u53d6\u6d88',
        activeRunLost: '\u8fd0\u884c\u4f1a\u8bdd\u5df2\u5931\u6548\uff0c\u53ef\u80fd\u662f\u5e94\u7528\u5df2\u91cd\u542f\u6216\u540e\u53f0\u4efb\u52a1\u5df2\u6e05\u7406\uff0c\u8bf7\u67e5\u770b\u5386\u53f2\u6216\u91cd\u65b0\u6267\u884c\u3002',
        runSteps: '\u6267\u884c\u6b65\u9aa4',
        runArtifacts: '\u8f93\u51fa\u4ea7\u7269',
        runtimeInput: '\u8f93\u5165',
		runtimeStatus: '\u6267\u884c\u72b6\u6001',
		showExecDetail: '\u8be6\u60c5',
		hideExecDetail: '\u6536\u8d77',
		runtimeOutput: '\u8f93\u51fa',
		outputResult: '\u7ed3\u679c',
		outputText: '\u6587\u672c\u7ed3\u679c',
		outputContent: '\u8f93\u51fa\u5185\u5bb9',
		businessErrorCode: '\u9519\u8bef\u4ee3\u7801',
		businessErrorTarget: '\u5bf9\u8c61',
		businessErrorRequired: '\u8981\u6c42',
		businessErrorActual: '\u5f53\u524d',
		businessErrorNextActions: '\u5efa\u8bae\u52a8\u4f5c',
		businessResultTarget: '\u6267\u884c\u5bf9\u8c61',
		businessResultType: '\u7ed3\u679c\u7c7b\u578b',
		businessResultRecords: '\u8bb0\u5f55\u6570',
        noOutputYet: '\u6267\u884c\u5b8c\u6210\u540e\uff0c\u8fd9\u91cc\u663e\u793a\u8f93\u51fa\u6587\u4ef6\u6216\u6587\u672c\u7ed3\u679c\u3002',
        runCompleted: '\u6267\u884c\u5b8c\u6210',
        setAsPinned: '\u8bbe\u4e3a\u5e38\u7528',
        removeFromPinned: '\u79fb\u51fa\u5e38\u7528',
        artifactPending: '\u7b49\u5f85\u4ea7\u7269',
        artifactReady: '\u4ea7\u7269\u5df2\u751f\u6210',
        openArtifact: '\u6253\u5f00',
        downloadArtifact: '\u4e0b\u8f7d\u5e76\u6253\u5f00',
        revealArtifact: '\u5b9a\u4f4d',
        noRunEvidence: '\u6267\u884c\u540e\u663e\u793a\u6b65\u9aa4\u548c\u4ea7\u7269\u72b6\u6001',
        fileTooLarge: '\u6587\u4ef6\u8d85\u8fc7 25MB\uff0c\u6682\u4e0d\u652f\u6301\u6b64\u65b9\u5f0f\u4e0a\u4f20',
        cancelRun: '\u53d6\u6d88\u6267\u884c',
        runHistory: '\u8fd0\u884c\u5386\u53f2',
        runDetails: '\u8fd0\u884c\u8be6\u60c5',
        runHistoryManager: '\u8fd0\u884c\u8bb0\u5f55',
        runHistoryManagerHint: '\u805a\u5408\u5c55\u793a\u672c\u673a\u4fdd\u5b58\u7684\u5e94\u7528\u6267\u884c\u5386\u53f2\u3002',
        runHistoryAllApps: '\u5168\u90e8\u5e94\u7528',
        noGlobalRunHistory: '\u6682\u65e0\u5e94\u7528\u8fd0\u884c\u8bb0\u5f55',
        approvalWorkspace: '\u5ba1\u6279\u5b9e\u4f8b',
        approvalManagerTitle: '\u5ba1\u6279\u5b9e\u4f8b\u7ba1\u7406',
        approvalManagerHint: '\u6240\u6709\u5ba1\u6279\u578b\u5e94\u7528\u7684\u7533\u8bf7\u3001\u5f85\u529e\u548c\u7ed3\u679c\u5728\u8fd9\u91cc\u7edf\u4e00\u7ba1\u7406\u3002',
        approvalManagerLocalSection: '\u672c\u5730\u5ba1\u6279\u5b9e\u4f8b',
        approvalManagerLocalHint: '\u5de6\u4fa7\u9009\u62e9\u4e00\u6761\u5b9e\u4f8b\uff0c\u53f3\u4fa7\u67e5\u770b\u8be6\u60c5\u548c\u53ef\u7528\u5904\u7406\u52a8\u4f5c\u3002',
        approvalDetailSection: '\u5ba1\u6279\u5b9e\u4f8b\u8be6\u60c5',
        approvalDetailHint: '\u9009\u4e2d\u5de6\u4fa7\u5b9e\u4f8b\u540e\uff0c\u8fd9\u91cc\u663e\u793a\u8f68\u8ff9\u3001\u7ed3\u679c\u548c\u5ba1\u6279\u52a8\u4f5c\u3002',
        approvalSearch: '\u641c\u7d22\u5ba1\u6279\u5b9e\u4f8b',
        approvalAppFilter: '\u5e94\u7528',
        approvalStatusFilter: '\u72b6\u6001',
        approvalAllApps: '\u5168\u90e8\u5e94\u7528',
        approvalAllStatuses: '\u5168\u90e8\u72b6\u6001',
        approvalRefresh: '\u5237\u65b0',
        approvalLoading: '\u6b63\u5728\u8bfb\u53d6\u5ba1\u6279\u5b9e\u4f8b',
        approvalLoadError: '\u5ba1\u6279\u5b9e\u4f8b\u8bfb\u53d6\u5931\u8d25',
        approvalRecentCount: '\u6700\u8fd1\u5b9e\u4f8b',
        approvalPendingCount: '\u5f85\u5904\u7406',
        viewAllApprovals: '\u67e5\u770b\u5168\u90e8\u5ba1\u6279\u72b6\u6001',
        viewAppApprovals: '\u67e5\u770b\u672c\u5e94\u7528\u5ba1\u6279',
        myRequests: '\u6211\u7684\u7533\u8bf7',
        pendingMyApproval: '\u5f85\u6211\u5ba1\u6279',
        handledApprovals: '\u5df2\u5904\u7406',
        approvedApprovals: '\u5df2\u901a\u8fc7',
        rejectedApprovals: '\u5df2\u9a73\u56de',
        attentionApprovals: '\u9700\u5173\u6ce8',
        allApprovalInstances: '\u5168\u90e8',
        documentOutputs: '\u6587\u6863\u8f93\u51fa',
        inlineContentOutputs: '\u6587\u672c\u8f93\u51fa',
        datasrvApprovalSummary: 'DataSrv \u5ba1\u6279\u6982\u89c8',
        datasrvApprovalSummaryHint: '\u6309 DataSrv app-installations \u67e5\u8be2\u805a\u5408\u6211\u7684\u7533\u8bf7\u3001\u5f85\u5ba1\u3001\u5ba1\u6279\u7ed3\u679c\u548c\u8f93\u51fa\u7c7b\u578b\u3002',
        datasrvApprovalSummaryDisabled: 'DataSrv \u672a\u542f\u7528\uff0c\u53ea\u663e\u793a\u672c\u5730\u5ba1\u6279\u5b9e\u4f8b\u3002',
        datasrvApprovalSummaryLoading: '\u6b63\u5728\u805a\u5408 DataSrv \u5ba1\u6279\u7ed3\u679c',
        datasrvApprovalSummaryError: 'DataSrv \u5ba1\u6279\u6982\u89c8\u8bfb\u53d6\u5931\u8d25',
        datasrvApprovalSummaryEmpty: '\u6682\u65e0\u5339\u914d\u7684\u5ba1\u6279\u578b\u5e94\u7528',
        datasrvApprovalDetails: 'DataSrv \u5ba1\u6279\u660e\u7ec6',
        openDataSrvApproval: '\u6253\u5f00\u5ba1\u6279',
        openDataSrvRecord: '\u6253\u5f00\u8bb0\u5f55',
        approvalInstanceData: '\u5b9e\u4f8b\u6570\u636e',
        currentApprovalNode: '\u5f53\u524d\u8282\u70b9',
        currentNodeStatus: '\u8282\u70b9\u72b6\u6001',
        approvalNodeTasks: '\u8282\u70b9\u4efb\u52a1',
        approvalApplicantLabel: '\u7533\u8bf7\u4eba',
        approvalApproverLabel: '\u5ba1\u6279\u4eba',
        currentAssigneeLabel: '\u5f53\u524d\u5904\u7406\u4eba',
        assigneeTypeLabel: '\u5904\u7406\u4eba\u7c7b\u578b',
        statusTransitionLabel: '\u72b6\u6001\u6d41\u8f6c',
        approvalTimeline: '\u5ba1\u6279\u8f68\u8ff9',
        approvalDetailEmpty: '\u53f3\u4fa7\u662f\u5b9e\u4f8b\u8be6\u60c5\u548c\u5904\u7406\u52a8\u4f5c\u533a\uff0c\u8bf7\u5148\u5728\u5de6\u4fa7\u9009\u62e9\u4e00\u6761\u5ba1\u6279\u5b9e\u4f8b\u3002',
        approvalActions: '\u5ba1\u6279\u64cd\u4f5c',
        approve: '\u901a\u8fc7',
        reject: '\u9a73\u56de',
        markAttention: '\u6807\u8bb0\u5173\u6ce8',
        supplementContinue: '\u8865\u5145\u5e76\u7ee7\u7eed',
        supplementNoteLabel: '\u8865\u5145\u8bf4\u660e',
        supplementReferenceLabel: '\u6750\u6599\u6216\u9644\u4ef6\u5f15\u7528',
        supplementFieldsLabel: '\u7f3a\u5931\u9879',
        supplementRunning: '\u6b63\u5728\u7ee7\u7eed\u6d41\u8f6c',
	        approvalResult: '\u5ba1\u6279\u7ed3\u679c',
	        approvalResultFeedback: '\u7ed3\u679c\u53cd\u9988',
	        approvalResultPackage: '\u7ed3\u679c\u5305',
	        approvalOutputData: '\u7ed3\u6784\u5316\u6570\u636e',
	        approvalPrimaryArtifact: '\u4e3b\u4ea7\u7269',
	        approvalOutputCount: '\u8f93\u51fa\u6570',
	        approvalArtifactCount: '\u4ea7\u7269\u6570',
	        approvalInstanceId: '\u5b9e\u4f8b\u7f16\u53f7',
        workflowSkill: '\u81ea\u52a8\u5ba1\u6279\u6d41\u7a0b',
        dataSrvRecord: '\u4e1a\u52a1\u8bb0\u5f55',
        approvalObjectRoleLabel: '\u4e1a\u52a1\u5bf9\u8c61',
        remoteApprovalLabel: '\u8fdc\u7aef\u5ba1\u6279',
        businessStatusLabel: '\u4e1a\u52a1\u72b6\u6001',
        resultStatusLabel: '\u7ed3\u679c\u72b6\u6001',
        viewFullWorkflow: '\u67e5\u770b\u5b8c\u6574\u6d41\u7a0b',
        noRunHistory: '\u6682\u65e0\u8fd0\u884c\u8bb0\u5f55',
        noApprovalInstances: '\u5f53\u524d\u5206\u7c7b\u6682\u65e0\u5ba1\u6279\u5b9e\u4f8b',
        clearHistory: '\u6e05\u7a7a\u5386\u53f2',
        submitted: '\u5df2\u63d0\u4ea4',
        noOpenAppTitle: '\u9009\u62e9\u5e94\u7528',
        noOpenAppHint: '\u70b9\u51fb\u5de6\u4fa7\u5e94\u7528\u56fe\u6807\uff0c\u4ee5\u6253\u5f00\u5e94\u7528\u3002',
        noApps: '\u6ca1\u6709\u5339\u914d\u7684\u5e94\u7528',
        noMoreApps: '\u6ca1\u6709\u66f4\u591a\u5e94\u7528',
        pin: '\u7f6e\u9876',
        unpin: '\u53d6\u6d88',
        pinLimitReached: '\u5e38\u7528\u5e94\u7528\u5df2\u6ee1 8 \u4e2a\uff0c\u8bf7\u5148\u53d6\u6d88\u4e00\u4e2a',
        edit: '\u7f16\u8f91',
        duplicate: '\u590d\u5236\u5e94\u7528',
        duplicateSuffix: '\u526f\u672c',
        save: '\u4fdd\u5b58',
        cancel: '\u53d6\u6d88',
        moveUp: '\u4e0a\u79fb',
        moveDown: '\u4e0b\u79fb',
        moveTop: '\u79fb\u5230\u9876\u90e8',
        moveBottom: '\u79fb\u5230\u5e95\u90e8',
        moveUpShort: '\u4e0a\u79fb',
        moveDownShort: '\u4e0b\u79fb',
        moveTopShort: '\u9876\u90e8',
        moveBottomShort: '\u5e95\u90e8',
        clearFilterToSort: '\u6e05\u7a7a\u7b5b\u9009\u540e\u8c03\u6574\u987a\u5e8f',
        hidden: '\u9690\u85cf',
        remove: '\u79fb\u9664',
        disable: '\u505c\u7528',
        enable: '\u542f\u7528',
        restore: '\u6062\u590d',
        hiddenApps: '\u5df2\u9690\u85cf\u5e94\u7528',
        datasrvDiscovery: 'DataSrv \u80fd\u529b\u53d1\u73b0',
        datasrvReady: '\u5df2\u8fde\u63a5',
        datasrvLoading: '\u8bfb\u53d6\u4e2d',
        datasrvDisabled: '\u672a\u542f\u7528',
        datasrvError: '\u4e0d\u53ef\u7528',
        addToPanel: '\u52a0\u5230\u9762\u677f',
        added: '\u5df2\u6dfb\u52a0',
        addingToPanel: '\u52a0\u5165\u4e2d',
        inPanel: '\u5df2\u52a0\u5165\u9762\u677f',
        discoveredApps: '\u53ef\u751f\u6210\u5e94\u7528',
        skillApps: '\u53d1\u73b0\u7684\u5e94\u7528',
        skillAppsMeta: '\u4ece\u5df2\u5b89\u88c5\u80fd\u529b\u4e2d\u627e\u5230\uff0c\u5df2\u81ea\u52a8\u540c\u6b65\u5230\u5de6\u4fa7\u5e94\u7528\u9762\u677f',
        skillAppsErrorMeta: '\u68c0\u67e5\u5df2\u5b89\u88c5\u80fd\u529b\u65f6\u9047\u5230\u95ee\u9898',
        manifestPreview: '\u5f53\u524d\u8349\u7a3f manifest',
        manifestPreviewHint: '\u7528\u4e8e\u5ba1\u6838\u548c\u590d\u5236\u7684\u5b9e\u65f6 JSON\uff0c\u5df2\u6839\u636e\u4e0a\u65b9\u914d\u7f6e\u540c\u6b65\u66f4\u65b0',
        manifest: '\u6e05\u5355',
        exportPack: '\u590d\u5236\u5e94\u7528\u5305',
        copy: '\u590d\u5236',
        copied: '\u5df2\u590d\u5236',
        fields: '\u8868\u5355\u5b57\u6bb5',
        addField: '\u6dfb\u52a0\u5b57\u6bb5',
        deleteField: '\u5220\u9664\u5b57\u6bb5',
        fieldName: '\u5b57\u6bb5\u540d',
        fieldLabel: '\u663e\u793a\u540d',
        fieldType: '\u7c7b\u578b',
        fieldRequired: '\u5fc5\u586b',
        defaultValue: '\u9ed8\u8ba4\u503c',
        options: '\u9009\u9879',
        appColor: '\u56fe\u6807\u989c\u8272',
        validationMissing: '\u8bf7\u8865\u5145\u5fc5\u586b\u8f93\u5165',
        installManifest: '\u5b89\u88c5\u5e94\u7528\u5305',
        marketApps: '\u5e02\u573a\u5e94\u7528\u5217\u8868',
        marketAdvancedImport: '\u5bfc\u5165\u5e94\u7528\u5305',
        marketAdvancedImportHint: '\u7c98\u8d34\u5df2\u5ba1\u6838\u7684\u5e94\u7528\u5305 JSON\uff0c\u9002\u5408\u7ba1\u7406\u5458\u6216\u79c1\u6709\u5e94\u7528\u3002',
        marketSource: '\u6765\u6e90',
        marketAddableCount: '\u53ef\u6dfb\u52a0',
        marketUpgradeableCount: '\u53ef\u5347\u7ea7',
        marketHubSearchPlaceholder: '\u641c\u7d22\u4f01\u4e1a Hub \u5e94\u7528',
        marketHubSearch: '\u641c\u7d22 Hub',
        marketHubSearching: '\u641c\u7d22\u4e2d',
        marketHubEmpty: '\u672a\u627e\u5230\u5e94\u7528',
        marketHubError: '\u641c\u7d22\u5931\u8d25',
        marketAdd: '\u6dfb\u52a0',
        pasteManifest: '\u7c98\u8d34\u5e94\u7528\u5305 JSON\uff08maclaw.app.v1 / maclaw.app.pack.v1 / maclaw.apps.json\uff09',
        install: '\u5b89\u88c5',
        installing: '\u5b89\u88c5\u4e2d',
        confirmHighRiskInstall: '\u786e\u8ba4\u5b89\u88c5',
        highRiskInstallWarning: '\u9009\u4e2d\u7684\u5347\u7ea7\u5305\u5305\u542b\u9ad8\u98ce\u9669\u65b0\u6743\u9650\uff0c\u9700\u518d\u6b21\u786e\u8ba4\u3002',
        dependencyPlanLoading: '\u6b63\u5728\u68c0\u67e5 Skill \u4f9d\u8d56',
        dependencyReady: '\u4f9d\u8d56 Skill \u5df2\u5c31\u7eea',
        installDependenciesAndRun: '\u5b89\u88c5\u4f9d\u8d56\u5e76\u6267\u884c',
        installingDependencies: '\u6b63\u5728\u5b89\u88c5\u4f9d\u8d56',
        missingRequiredDependency: '\u5fc5\u9700 Skill \u4f9d\u8d56\u7f3a\u5931\u6216\u4e0d\u53ef\u7528\uff0c\u8bf7\u5148\u5b89\u88c5\u6216\u542f\u7528\u4f9d\u8d56',
        dependencyPlanError: '\u4f9d\u8d56\u68c0\u67e5\u5931\u8d25',
        dependencyVerification: '\u4f9d\u8d56\u9a8c\u8bc1',
        dependencyDiagnostics: '\u4f9d\u8d56\u8bca\u65ad',
        dependencyInstallTrace: '\u5b89\u88c5\u8fc7\u7a0b',
        dependencyPreflightStage: '\u9884\u68c0',
        dependencyResolutionStage: '\u89e3\u6790',
        dependencyDownloadStage: '\u4e0b\u8f7d',
        dependencyIntegrityStage: '\u5b8c\u6574\u6027',
        dependencyInstallStage: '\u5b89\u88c5\u6267\u884c',
        dependencyErrorStage: '\u9519\u8bef',
        dependencyVerificationReady: '\u4f9d\u8d56\u9a8c\u8bc1\u5df2\u5b8c\u6210',
        dependencyVerificationBlocked: '\u4f9d\u8d56\u9a8c\u8bc1\u53d1\u73b0\u963b\u65ad\u9879',
        versionSnapshot: '\u7248\u672c\u5feb\u7167',
        workspaceLayout: '\u754c\u9762\u5e03\u5c40',
        resultContract: '\u7ed3\u679c\u5951\u7ea6',
        resultCoverage: '\u7ed3\u679c\u8986\u76d6',
        resultCovered: '\u5df2\u8986\u76d6',
        resultMissing: '\u7f3a\u53e3',
        testEvidence: '\u6d4b\u8bd5\u8bc1\u636e',
        appSkill: '\u5e94\u7528 Skill',
        workflowContract: '\u8fd0\u884c\u5951\u7ea6',
        workflowContractReady: '\u8fd0\u884c\u5951\u7ea6\u5df2\u5bf9\u9f50',
        workflowContractBlocked: '\u8fd0\u884c\u5951\u7ea6\u9700\u5904\u7406',
        workflowContractInputs: '\u8f93\u5165',
        workflowContractOutputs: '\u8f93\u51fa',
        workflowContractMissing: '\u672a\u58f0\u660e\u8fd0\u884c\u5951\u7ea6',
        approvalBinding: '\u5ba1\u6279\u7ed1\u5b9a',
        skillDependencies: '\u4f9d\u8d56 Skill',
        installedDependency: '\u5df2\u5b89\u88c5',
        missingDependency: '\u7f3a\u5931',
        unavailableDependency: '\u4e0d\u53ef\u7528',
        installPreview: '\u5b89\u88c5\u9884\u89c8',
        willInstall: '\u5c06\u5b89\u88c5',
        willUpgrade: '\u5c06\u5347\u7ea7',
        permissionChanges: '\u6743\u9650\u53d8\u5316',
        highRiskPermission: '\u9ad8\u98ce\u9669',
        willSkip: '\u5c06\u8df3\u8fc7',
        alreadyInstalled: '\u5df2\u5b89\u88c5',
        duplicateApp: '\u91cd\u590d\u5e94\u7528',
        notSelected: '\u672a\u9009\u62e9',
        selectAll: '\u5168\u9009',
        clearSelection: '\u5168\u4e0d\u9009',
        installableCount: '\u53ef\u5b89\u88c5',
        upgradeableCount: '\u53ef\u5347\u7ea7',
        installedCount: '\u5df2\u5b89\u88c5',
        upgradedCount: '\u5df2\u5347\u7ea7',
        upgradedItem: '\u5df2\u5347\u7ea7',
        skippedItem: '\u5df2\u8df3\u8fc7',
        installDetails: '\u5b89\u88c5\u660e\u7ec6',
        hubInstallSummary: '\u6e90\u5305 {source} \u4e2a \u00b7 \u5df2\u5b89\u88c5 {installed} \u4e2a',
        hubInstallDependencySummary: '\u4f9d\u8d56 {count} \u4e2a',
        installRecords: '\u6700\u8fd1\u5b89\u88c5',
        installRecordsHint: '\u672c\u673a\u5e94\u7528\u5305\u5b89\u88c5\u548c Skill \u4f9d\u8d56\u5ba1\u8ba1',
        datasrvRegistrationReady: '\u0044\u0061\u0074\u0061\u0053\u0072\u0076 \u7ed1\u5b9a\u5df2\u6ce8\u518c',
        datasrvRegistrationPartial: '\u0044\u0061\u0074\u0061\u0053\u0072\u0076 \u7ed1\u5b9a\u90e8\u5206\u6ce8\u518c',
        datasrvRegistrationSkipped: '\u0044\u0061\u0074\u0061\u0053\u0072\u0076 \u7ed1\u5b9a\u672a\u6ce8\u518c',
        datasrvRegistrationFailed: '\u0044\u0061\u0074\u0061\u0053\u0072\u0076 \u7ed1\u5b9a\u6ce8\u518c\u5931\u8d25',
        auditDataSrvRegistration: '\u5ba1\u8ba1 DataSrv',
        auditingDataSrvRegistration: '\u5ba1\u8ba1\u4e2d',
        dataSrvRegistrationAuditFound: 'DataSrv \u5b89\u88c5\u8bb0\u5f55',
        dataSrvRegistrationAuditMissing: 'DataSrv \u5b89\u88c5\u8bb0\u5f55\u672a\u627e\u5230',
        installRecordsLoading: '\u6b63\u5728\u8bfb\u53d6\u5b89\u88c5\u8bb0\u5f55',
        installRecordsError: '\u5b89\u88c5\u8bb0\u5f55\u8bfb\u53d6\u5931\u8d25',
        noInstallRecords: '\u6682\u65e0\u5e94\u7528\u5b89\u88c5\u8bb0\u5f55',
        refreshInstallRecords: '\u5237\u65b0\u8bb0\u5f55',
        recheckInstallDependencies: '\u68c0\u67e5\u4f9d\u8d56',
        checkingInstallDependencies: '\u68c0\u67e5\u4e2d',
        repairInstallDependencies: '\u4fee\u590d\u4f9d\u8d56',
        repairingInstallDependencies: '\u4fee\u590d\u4e2d',
        installRecordPackageMissing: '\u5b89\u88c5\u8bb0\u5f55\u7f3a\u5c11\u5e94\u7528\u5305\u5feb\u7167',
        packageSha: '\u5305\u6307\u7eb9',
        installedAt: '\u5b89\u88c5\u65f6\u95f4',
        missingDependencyCount: '\u963b\u65ad\u4f9d\u8d56',
        skippedCount: '\u5df2\u8df3\u8fc7',
        installAuditRequired: '\u4f01\u4e1a\u5e94\u7528\u5b89\u88c5\u8bc1\u636e\u672a\u4fdd\u5b58\uff0c\u8bf7\u91cd\u8bd5\u6216\u68c0\u67e5 DataSrv \u6ce8\u518c',
        installError: '\u5e94\u7528\u5305\u65e0\u6548',
        installErrorShowDetail: '\u67e5\u770b\u8be6\u60c5',
        installErrorHideDetail: '\u6536\u8d77\u8be6\u60c5',
        installErrorFriendlyGovernance: '\u5b89\u88c5\u5931\u8d25\uff1a\u5e94\u7528\u672a\u901a\u8fc7\u5b89\u88c5\u524d\u68c0\uff0c\u53ef\u80fd\u7f3a\u5c11\u5fc5\u8981\u7684\u8fd0\u884c\u8bc1\u636e\u6216\u4f9d\u8d56\u3002',
        installErrorFriendlyDependency: '\u5b89\u88c5\u5931\u8d25\uff1a\u6240\u9700\u7684 Skill \u4f9d\u8d56\u4e0d\u53ef\u7528\u6216\u4e0b\u8f7d\u5931\u8d25\uff0c\u8bf7\u68c0\u67e5\u7f51\u7edc\u6216\u8054\u7cfb\u7ba1\u7406\u5458\u3002',
        installErrorFriendlyGeneric: '\u5b89\u88c5\u5931\u8d25\uff1a\u5e94\u7528\u65e0\u6cd5\u6b63\u5e38\u5b89\u88c5\uff0c\u8bf7\u91cd\u8bd5\u6216\u8054\u7cfb\u7ba1\u7406\u5458\u3002',
        installErrorFriendlyWorkflow: '\u5b89\u88c5\u5931\u8d25\uff1a\u5e94\u7528\u8fd0\u884c\u5951\u7ea6\u672a\u5bf9\u9f50\uff0c\u8bf7\u8054\u7cfb\u5e94\u7528\u53d1\u5e03\u8005\u4fee\u590d\u3002',
        parseError: 'JSON \u89e3\u6790\u5931\u8d25',
        schemaError: '\u672a\u8bc6\u522b\u5e94\u7528\u5305\u683c\u5f0f',
        close: '\u5173\u95ed',
        aboutInfoSection: '\u5173\u4e8e\u4fe1\u606f',
        aboutInfoSectionDesc: '\u53d1\u5e03\u540e\u5728\u5e94\u7528\u8fd0\u884c\u65f6\u663e\u793a\u7ed9\u7528\u6237',
        aboutAuthor: '\u4f5c\u8005',
        aboutAuthorPlaceholder: '\u4f8b\u5982\uff1a\u5f20\u4e09',
        aboutCopyright: '\u7248\u6743\u58f0\u660e',
        aboutCopyrightPlaceholder: '\u4f8b\u5982\uff1aCopyright \u00a9 2026 MyCompany',
        aboutWebsite: '\u5b98\u7f51',
        aboutWebsitePlaceholder: '\u4f8b\u5982\uff1ahttps://example.com',
        aboutEmail: '\u8054\u7cfb\u90ae\u7bb1',
        aboutEmailPlaceholder: '\u4f8b\u5982\uff1asupport@example.com',
        aboutDialogTitle: '\u5173\u4e8e',
        aboutVersion: '\u7248\u672c',
        aboutNoInfo: '\u5f00\u53d1\u8005\u672a\u63d0\u4f9b\u5173\u4e8e\u4fe1\u606f',
    },
    en: {
        search: 'Search apps',
        clearSearch: 'Clear search',
        resetFilter: 'Reset filters',
        create: 'Create',
        category: 'Category',
        operations: 'Actions',
        approvalStatus: 'Approval status',
        approvalStatusHint: 'Open approval instance management',
        runHistoryOps: 'Run history',
        runHistoryOpsHint: 'Review app execution history',
        marketOps: 'App Market',
        marketOpsHint: 'Search and install apps',
        all: 'All apps',
        otherApps: 'Other apps',
        pinned: 'Pinned apps',
        recent: 'Recent',
        searchResults: 'Search results',
        recentUsed: 'Recent',
        neverUsed: 'Never used',
        appStatus: 'Status',
        appAvailable: 'Available',
        appOpened: 'Opened',
        appRunning: 'Running',
        appDisabled: 'Disabled',
        appDisabledReason: 'Disabled by enterprise policy. The entry and history are retained.',
        appHubDeprecatedReason: 'Deprecated by the enterprise capability market. The entry and history are retained.',
        appHubRevokedReason: 'Revoked by the enterprise capability market. The entry and history are retained.',
        apps: 'Apps',
        appStudio: 'MaClaw App Studio',
        studioSubtitle: 'Create, manage, and add apps from the capability market.',
        manual: 'App Studio manual',
        manualAria: 'Open the MaClaw App Studio manual',
        createTab: 'Create app',
        promptDraft: 'Generate draft from chat',
        generateDraft: 'Generate draft',
        draftPromptPlaceholder: 'Example: build a contract filing app, upload Word/PDF, output archive number and review result',
        manageTab: 'Manage apps',
        publishTab: 'Review / publish',
        publishSubtitle: 'Check whether local apps are ready for upload to the enterprise capability market.',
        publishChecklist: 'Publish checklist',
        readyToSubmit: 'Ready to submit',
        needsWork: 'Needs work',
        submitPackage: 'Submission package preview',
        copySubmitPackage: 'Copy submission package',
        noPublishApps: 'No local apps ready for publishing yet',
        submitReview: 'Submit for review',
        submittedReview: 'Submitted',
        pendingReview: 'Waiting for enterprise market review',
        localReviewPending: 'Local sync pending',
        localModifiedReview: 'Modified locally, resubmit required',
        localModifiedAt: 'Local change',
        submitReviewBusy: 'Submitting',
        submitReviewLocalFallback: 'Enterprise market is not connected yet; saved as a local pending submission.',
        localSubmissionQueue: 'Local submission queue',
        noLocalSubmissionQueue: 'No local pending submissions',
        localSubmissionQueueError: 'Failed to read submission queue',
        localSubmissionQueueLoading: 'Reading submission queue',
        reviewEvidence: 'Review evidence',
        installGovernance: 'Install governance',
        runtimeEvidence: 'Runtime evidence',
        packageSignature: 'Package signature',
        approval: 'Approval',
        progress: 'Progress',
        workflow: 'Workflow',
        refreshQueue: 'Refresh',
        refreshingQueue: 'Refreshing',
        syncQueueToHub: 'Sync to Hub',
        syncingQueueToHub: 'Syncing',
        refreshQueueFromHub: 'Refresh Hub Status',
        refreshingQueueFromHub: 'Refreshing',
        queueHubSyncFailed: 'Hub sync failed',
        installApprovedHubApp: 'Install approved app',
        installingApprovedHubApp: 'Installing',
        approvedHubAppInstalled: 'Installed',
        approvedHubAppInstallFailed: 'Install failed',
        queueRefreshedAt: 'Last refreshed',
        copyQueuePackage: 'Copy queued package',
        copyQueueAudit: 'Copy audit',
        viewQueueDetail: 'View details',
        hideQueueDetail: 'Hide details',
        queueDetailLoading: 'Reading details',
        queueDetailTitle: 'Submission details',
        queueDetailPackageApps: 'Package apps',
        queueDetailEvents: 'Audit events',
        copyingQueuePackage: 'Copying',
        queuePackageCopied: 'Copied',
        queueAuditCopied: 'Audit copied',
        queuePackageUnavailable: 'Full package unavailable',
        reviewIssues: 'Review issues',
        reviewIssuesMore: 'plus',
        reviewIssuesMoreUnit: 'more',
        fixReviewIssue: 'Fix',
        resolveReviewDependencies: 'Resolve dependencies',
        appVersion: 'Version',
        reviewer: 'Reviewer',
        riskLevel: 'Risk',
        approvedScopes: 'Approved scopes',
        eventHistory: 'Events',
        reviewFailed: 'Changes requested',
        reviewApproved: 'Approved',
        reviewPublished: 'Published',
        reviewDeprecated: 'Deprecated',
        reviewRevoked: 'Revoked',
        submissionId: 'Submission ID',
        withdrawSubmission: 'Withdraw submission',
        run: 'Run',
        reset: 'Reset',
        output: 'Output',
        upload: 'Drop or choose files',
        chooseFile: 'Choose file',
        selectedFile: 'Selected',
        noFile: 'No file selected',
        readyOutput: 'Waiting to run',
        generatedOutput: 'Output generated',
        skillRunStarted: 'Skill run started',
        skillRunRunning: 'Skill running',
        skillRunCompleted: 'Skill completed',
        skillRunFailed: 'Skill failed',
        skillRunCancelled: 'Skill cancelled',
        activeRunLost: 'The running session is no longer available. The app may have restarted or the background task was cleaned up. Review history or run it again.',
        runSteps: 'Run steps',
        runArtifacts: 'Output artifacts',
        runtimeInput: 'Input',
		runtimeStatus: 'Run status',
		showExecDetail: 'Details',
		hideExecDetail: 'Hide',
		runtimeOutput: 'Output',
		outputResult: 'Result',
		outputText: 'Text result',
		outputContent: 'Output content',
		businessErrorCode: 'Error code',
		businessErrorTarget: 'Target',
		businessErrorRequired: 'Required',
		businessErrorActual: 'Actual',
		businessErrorNextActions: 'Next actions',
		businessResultTarget: 'Target',
		businessResultType: 'Result type',
		businessResultRecords: 'Records',
        noOutputYet: 'Output files or text results appear here after execution.',
        runCompleted: 'Run completed',
        setAsPinned: 'Pin to favorites',
        removeFromPinned: 'Remove from favorites',
        artifactPending: 'Waiting for artifact',
        artifactReady: 'Artifact generated',
        openArtifact: 'Open',
        downloadArtifact: 'Download and open',
        revealArtifact: 'Reveal',
        noRunEvidence: 'Steps and artifact status appear after execution',
        fileTooLarge: 'File is larger than 25MB and cannot be uploaded this way yet',
        cancelRun: 'Cancel run',
        runHistory: 'Run history',
        runDetails: 'Run details',
        runHistoryManager: 'Run history',
        runHistoryManagerHint: 'Aggregated execution history saved on this device.',
        runHistoryAllApps: 'All apps',
        noGlobalRunHistory: 'No app run history yet',
        approvalWorkspace: 'Approval instances',
        approvalManagerTitle: 'Approval instance management',
        approvalManagerHint: 'Requests, pending work, and outcomes from every approval app are managed here.',
        approvalManagerLocalSection: 'Local approval instances',
        approvalManagerLocalHint: 'Select an instance on the left to inspect details and available actions on the right.',
        approvalDetailSection: 'Approval instance details',
        approvalDetailHint: 'After selecting an instance on the left, this area shows timeline, results, and approval actions.',
        approvalSearch: 'Search approvals',
        approvalAppFilter: 'App',
        approvalStatusFilter: 'Status',
        approvalAllApps: 'All apps',
        approvalAllStatuses: 'All statuses',
        approvalRefresh: 'Refresh',
        approvalLoading: 'Loading approval instances',
        approvalLoadError: 'Failed to load approval instances',
        approvalRecentCount: 'Recent instances',
        approvalPendingCount: 'Pending',
        viewAllApprovals: 'View all approval status',
        viewAppApprovals: 'View this app approvals',
        myRequests: 'My requests',
        pendingMyApproval: 'Pending my approval',
        handledApprovals: 'Handled',
        approvedApprovals: 'Approved',
        rejectedApprovals: 'Rejected',
        attentionApprovals: 'Needs attention',
        allApprovalInstances: 'All',
        documentOutputs: 'Document outputs',
        inlineContentOutputs: 'Text outputs',
        datasrvApprovalSummary: 'DataSrv approval overview',
        datasrvApprovalSummaryHint: 'Aggregates my requests, approvals, decisions, and output types from DataSrv app-installations.',
        datasrvApprovalSummaryDisabled: 'DataSrv is disabled; showing local approval instances only.',
        datasrvApprovalSummaryLoading: 'Aggregating DataSrv approval results',
        datasrvApprovalSummaryError: 'Failed to load the DataSrv approval overview',
        datasrvApprovalSummaryEmpty: 'No matching approval apps yet',
        datasrvApprovalDetails: 'DataSrv approval details',
        openDataSrvApproval: 'Open approval',
        openDataSrvRecord: 'Open record',
        approvalInstanceData: 'Instance data',
        currentApprovalNode: 'Current node',
        currentNodeStatus: 'Node status',
        approvalNodeTasks: 'Node tasks',
        approvalApplicantLabel: 'Applicant',
        approvalApproverLabel: 'Approver',
        currentAssigneeLabel: 'Current assignee',
        assigneeTypeLabel: 'Assignee type',
        statusTransitionLabel: 'Status transition',
        approvalTimeline: 'Approval timeline',
        approvalDetailEmpty: 'This is the detail and action area. Select an approval instance on the left first.',
        approvalActions: 'Approval actions',
        approve: 'Approve',
        reject: 'Reject',
        markAttention: 'Mark attention',
        supplementContinue: 'Continue with supplement',
        supplementNoteLabel: 'Supplement note',
        supplementReferenceLabel: 'Material or attachment reference',
        supplementFieldsLabel: 'Missing items',
        supplementRunning: 'Continuing workflow',
	        approvalResult: 'Approval result',
	        approvalResultFeedback: 'Result feedback',
	        approvalResultPackage: 'Result package',
	        approvalOutputData: 'Structured data',
	        approvalPrimaryArtifact: 'Primary artifact',
	        approvalOutputCount: 'Outputs',
	        approvalArtifactCount: 'Artifacts',
	        approvalInstanceId: 'Instance ID',
        workflowSkill: 'Approval workflow',
        dataSrvRecord: 'Business record',
        approvalObjectRoleLabel: 'Business object',
        remoteApprovalLabel: 'Remote approval',
        businessStatusLabel: 'Business status',
        resultStatusLabel: 'Result status',
        viewFullWorkflow: 'View full workflow',
        noRunHistory: 'No runs yet',
        noApprovalInstances: 'No approval instances in this lane',
        clearHistory: 'Clear history',
        submitted: 'Submitted',
        noOpenAppTitle: 'Choose an app',
        noOpenAppHint: 'Click an app icon on the left to open the app.',
        noApps: 'No matching apps',
        noMoreApps: 'No more apps',
        pin: 'Pin',
        unpin: 'Unpin',
        pinLimitReached: 'Pinned apps are full at 8. Unpin one first.',
        edit: 'Edit',
        duplicate: 'Duplicate app',
        duplicateSuffix: 'Copy',
        save: 'Save',
        cancel: 'Cancel',
        moveUp: 'Move up',
        moveDown: 'Move down',
        moveTop: 'Move to top',
        moveBottom: 'Move to bottom',
        moveUpShort: 'Up',
        moveDownShort: 'Down',
        moveTopShort: 'Top',
        moveBottomShort: 'Bottom',
        clearFilterToSort: 'Clear filters before sorting',
        hidden: 'Hide',
        remove: 'Remove',
        disable: 'Disable',
        enable: 'Enable',
        restore: 'Restore',
        hiddenApps: 'Hidden apps',
        datasrvDiscovery: 'DataSrv discovery',
        datasrvReady: 'Connected',
        datasrvLoading: 'Loading',
        datasrvDisabled: 'Disabled',
        datasrvError: 'Unavailable',
        addToPanel: 'Add to panel',
        added: 'Added',
        addingToPanel: 'Adding',
        inPanel: 'In panel',
        discoveredApps: 'Discovered apps',
        skillApps: 'Found apps',
        skillAppsMeta: 'Found from installed capabilities and synced to the app panel',
        skillAppsErrorMeta: 'Could not check installed capabilities',
        manifestPreview: 'Current draft manifest',
        manifestPreviewHint: 'Live JSON for review and copy, synchronized from the configuration above',
        manifest: 'Manifest',
        exportPack: 'Copy app pack',
        copy: 'Copy',
        copied: 'Copied',
        fields: 'Fields',
        addField: 'Add field',
        deleteField: 'Delete field',
        fieldName: 'Field name',
        fieldLabel: 'Label',
        fieldType: 'Type',
        fieldRequired: 'Required',
        defaultValue: 'Default',
        options: 'Options',
        appColor: 'Icon color',
        validationMissing: 'Complete required input',
        installManifest: 'Install app package',
        marketApps: 'App catalog',
        marketAdvancedImport: 'Import app package',
        marketAdvancedImportHint: 'Paste reviewed app package JSON for admin installs or private apps.',
        marketSource: 'Source',
        marketAddableCount: 'Addable',
        marketUpgradeableCount: 'Upgradable',
        marketHubSearchPlaceholder: 'Search enterprise Hub apps',
        marketHubSearch: 'Search Hub',
        marketHubSearching: 'Searching',
        marketHubEmpty: 'No apps found',
        marketHubError: 'Search failed',
        marketAdd: 'Add',
        pasteManifest: 'Paste app package JSON (maclaw.app.v1 / maclaw.app.pack.v1 / maclaw.apps.json)',
        install: 'Install',
        installing: 'Installing',
        confirmHighRiskInstall: 'Confirm install',
        highRiskInstallWarning: 'Selected upgrades include high-risk new permissions; confirm once more.',
        dependencyPlanLoading: 'Checking Skill dependencies',
        dependencyReady: 'Skill dependencies ready',
        installDependenciesAndRun: 'Install dependencies and run',
        installingDependencies: 'Installing dependencies',
        missingRequiredDependency: 'Required Skill dependencies are missing or unavailable. Install or enable them first.',
        dependencyPlanError: 'Dependency check failed',
        dependencyVerification: 'Dependency verification',
        dependencyDiagnostics: 'Dependency diagnostics',
        dependencyInstallTrace: 'Install trace',
        dependencyPreflightStage: 'Preflight',
        dependencyResolutionStage: 'Resolve',
        dependencyDownloadStage: 'Download',
        dependencyIntegrityStage: 'Integrity',
        dependencyInstallStage: 'Install step',
        dependencyErrorStage: 'Error',
        dependencyVerificationReady: 'Dependency verification complete',
        dependencyVerificationBlocked: 'Dependency verification found blocking items',
        versionSnapshot: 'Version snapshot',
        workspaceLayout: 'Workspace layout',
        resultContract: 'Result contract',
        resultCoverage: 'Result coverage',
        resultCovered: 'Covered',
        resultMissing: 'Missing',
        testEvidence: 'Test evidence',
        appSkill: 'App Skill',
        workflowContract: 'Runtime contract',
        workflowContractReady: 'Runtime contract aligned',
        workflowContractBlocked: 'Runtime contract needs attention',
        workflowContractInputs: 'Inputs',
        workflowContractOutputs: 'Outputs',
        workflowContractMissing: 'Runtime contract not declared',
        approvalBinding: 'Approval binding',
        skillDependencies: 'Skill dependencies',
        installedDependency: 'Installed',
        missingDependency: 'Missing',
        unavailableDependency: 'Unavailable',
        installPreview: 'Install preview',
        willInstall: 'Will install',
        willUpgrade: 'Will upgrade',
        permissionChanges: 'Permission changes',
        highRiskPermission: 'High risk',
        willSkip: 'Will skip',
        alreadyInstalled: 'Already installed',
        duplicateApp: 'Duplicate app',
        notSelected: 'Not selected',
        selectAll: 'Select all',
        clearSelection: 'Select none',
        installableCount: 'Installable',
        upgradeableCount: 'Upgradeable',
        installedCount: 'Installed',
        upgradedCount: 'Upgraded',
        upgradedItem: 'Upgraded',
        skippedItem: 'Skipped',
        installDetails: 'Install details',
        hubInstallSummary: 'Source package {source} · installed {installed}',
        hubInstallDependencySummary: '{count} dependencies',
        installRecords: 'Recent installs',
        installRecordsHint: 'Local app package installs and Skill dependency audit',
        datasrvRegistrationReady: 'DataSrv bindings registered',
        datasrvRegistrationPartial: 'DataSrv bindings partially registered',
        datasrvRegistrationSkipped: 'DataSrv bindings not registered',
        datasrvRegistrationFailed: 'DataSrv binding registration failed',
        auditDataSrvRegistration: 'Audit DataSrv',
        auditingDataSrvRegistration: 'Auditing',
        dataSrvRegistrationAuditFound: 'DataSrv app installations',
        dataSrvRegistrationAuditMissing: 'DataSrv app installation not found',
        installRecordsLoading: 'Reading install records',
        installRecordsError: 'Failed to read install records',
        noInstallRecords: 'No app install records yet',
        refreshInstallRecords: 'Refresh records',
        recheckInstallDependencies: 'Check dependencies',
        checkingInstallDependencies: 'Checking',
        repairInstallDependencies: 'Repair dependencies',
        repairingInstallDependencies: 'Repairing',
        installRecordPackageMissing: 'Install record has no app package snapshot',
        packageSha: 'Package SHA',
        installedAt: 'Installed at',
        missingDependencyCount: 'Blocking deps',
        skippedCount: 'Skipped',
        installAuditRequired: 'Enterprise app install evidence was not saved. Retry or check DataSrv registration.',
        installError: 'Invalid app package',
        installErrorShowDetail: 'Show details',
        installErrorHideDetail: 'Hide details',
        installErrorFriendlyGovernance: 'Install failed: app did not pass pre-install checks. Run evidence or dependencies may be missing.',
        installErrorFriendlyDependency: 'Install failed: required Skill dependencies are unavailable or download failed. Check network or contact admin.',
        installErrorFriendlyGeneric: 'Install failed: unable to install the app. Please retry or contact admin.',
        installErrorFriendlyWorkflow: 'Install failed: app workflow contract is not aligned. Contact the app publisher to fix.',
        parseError: 'JSON parse failed',
        schemaError: 'Unrecognized app package format',
        close: 'Close',
        aboutInfoSection: 'About Info',
        aboutInfoSectionDesc: 'Displayed to users at runtime after publishing',
        aboutAuthor: 'Author',
        aboutAuthorPlaceholder: 'e.g. John Smith',
        aboutCopyright: 'Copyright',
        aboutCopyrightPlaceholder: 'e.g. Copyright \u00a9 2026 MyCompany',
        aboutWebsite: 'Website',
        aboutWebsitePlaceholder: 'e.g. https://example.com',
        aboutEmail: 'Contact Email',
        aboutEmailPlaceholder: 'e.g. support@example.com',
        aboutDialogTitle: 'About',
        aboutVersion: 'Version',
        aboutNoInfo: 'No about information provided by the developer',
    },
};

const appKinds: Record<AppKind, { zh: string; en: string }> = {
    enterprise_approval_app: { zh: '\u4f01\u4e1a\u5ba1\u6279\u578b', en: 'Approval app' },
    enterprise_normal_app: { zh: '\u4f01\u4e1a\u666e\u901a\u5e94\u7528', en: 'Business app' },
    tool_app: { zh: '\u5de5\u5177\u5e94\u7528', en: 'Tool app' },
    automation_app: { zh: '\u81ea\u52a8\u5316', en: 'Automation' },
};

const sourceLabels: Record<AppEntry['source'], { zh: string; en: string }> = {
    builtin: { zh: '\u5185\u7f6e', en: 'Built-in' },
    skill: { zh: 'Skill', en: 'Skill' },
    datasrv: { zh: 'DataSrv', en: 'DataSrv' },
    market: { zh: '\u5e02\u573a', en: 'Market' },
    local: { zh: '\u672c\u5730', en: 'Local' },
};

// initialApps is intentionally empty · apps are discovered at runtime from
// installed Skills (via ListSkillAppManifests), DataSrv capabilities, or
// user-created entries persisted in localStorage. Placeholder entries that
// lacked real backend implementations have been removed to avoid misleading
// users with non-functional apps in the management panel.
const initialApps: AppEntry[] = [];

const marketCatalogApps: AppEntry[] = [
    { id: 'market-contract-archive', name: '\u5408\u540c\u5f52\u6863', description: '\u4e0a\u4f20\u5408\u540c\u548c\u9644\u4ef6\uff0c\u751f\u6210\u5f52\u6863\u7f16\u53f7\u3001\u5ba1\u6838\u72b6\u6001\u548c\u8bc1\u636e\u6458\u8981\u3002', category: '\u6cd5\u52a1', kind: 'tool_app', icon: 'contract', accent: '#7c3f58', source: 'market', manifest: makeSkillManifest('contract-archive', 'mixed', ['docx', 'pdf', 'json']) },
    { id: 'market-invoice-check', name: '\u53d1\u7968\u6838\u9a8c', description: '\u6838\u5bf9\u53d1\u7968\u3001\u91d1\u989d\u3001\u62ac\u5934\u548c\u8d39\u7528\u79d1\u76ee\uff0c\u8f93\u51fa\u5f02\u5e38\u6e05\u5355\u3002', category: '\u8d22\u52a1', kind: 'tool_app', icon: 'invoice', accent: '#2f5f98', source: 'market', manifest: makeSkillManifest('invoice-check', 'file', ['xlsx', 'pdf']) },
    { id: 'market-meeting-minutes', name: '\u4f1a\u8bae\u7eaa\u8981', description: '\u6574\u7406\u4f1a\u8bae\u8bb0\u5f55\u6216\u5f55\u97f3\u8f6c\u5199\uff0c\u751f\u6210\u51b3\u8bae\u3001\u5f85\u529e\u548c\u8d23\u4efb\u4eba\u3002', category: 'OA', kind: 'tool_app', icon: 'calendar', accent: '#4b6572', source: 'market', manifest: makeSkillManifest('meeting-minutes', 'mixed', ['docx', 'txt']) },
    { id: 'market-sales-followup', name: '\u5ba2\u6237\u8ddf\u8fdb', description: '\u6839\u636e\u5ba2\u6237\u6c9f\u901a\u8bb0\u5f55\u751f\u6210\u8ddf\u8fdb\u8ba1\u5212\uff0c\u540c\u6b65\u5ba2\u6237\u72b6\u6001\u548c\u4e0b\u4e00\u6b65\u52a8\u4f5c\u3002', category: 'CRM', kind: 'enterprise_normal_app', icon: 'customer', accent: '#8a5a44', source: 'market', manifest: makeDataSrvManifest('sales', 'sales.followup_upsert', 'sales.customer_directory', 'sales.followup_by_status', 'sales.overview') },
];

const builtInAppIds = new Set(initialApps.map((app) => app.id));

const appIconMeta: Record<AppIconName, { zh: string; en: string }> = {
    receipt: { zh: '\u62a5\u9500/\u8d39\u7528', en: 'Expense' },
    wallet: { zh: '\u4ed8\u6b3e/\u8d22\u52a1', en: 'Payment' },
    invoice: { zh: '\u53d1\u7968/\u7968\u636e', en: 'Invoice' },
    warehouse: { zh: '\u4ed3\u50a8/\u5165\u5e93', en: 'Warehouse' },
    inventory: { zh: '\u5e93\u5b58/\u76d8\u70b9', en: 'Inventory' },
    customer: { zh: '\u5ba2\u6237/\u5efa\u6863', en: 'Customer' },
    users: { zh: '\u4eba\u5458/\u7ec4\u7ec7', en: 'People' },
    contract: { zh: '\u5408\u540c/\u6cd5\u52a1', en: 'Contract' },
    pdf: { zh: 'PDF/\u8f6c\u6362', en: 'PDF' },
    shield: { zh: '\u5ba1\u67e5/\u8131\u654f', en: 'Review' },
    sheet: { zh: '\u8868\u683c/\u6570\u636e', en: 'Sheet' },
    chart: { zh: '\u62a5\u8868/\u5206\u6790', en: 'Report' },
    dashboard: { zh: '\u770b\u677f/\u6307\u6807', en: 'Dashboard' },
    database: { zh: '\u6570\u636e\u5e93/\u5904\u7406', en: 'Database' },
    eraser: { zh: '\u6e05\u6d17/\u8131\u654f', en: 'Clean' },
    truck: { zh: '\u7269\u6d41/\u914d\u9001', en: 'Logistics' },
    calendar: { zh: '\u65e5\u7a0b/\u8003\u52e4', en: 'Calendar' },
    web: { zh: '\u7f51\u9875/\u91c7\u96c6', en: 'Web' },
    sync: { zh: '\u540c\u6b65/\u81ea\u52a8\u5316', en: 'Sync' },
    bot: { zh: 'Agent/\u81ea\u52a8\u5316', en: 'Agent' },
    shop: { zh: '\u95e8\u5e97/\u96f6\u552e', en: 'Store' },
};

const appIconNames = Object.keys(appIconMeta) as AppIconName[];

const appIconLabel = (icon: AppIconName, lang?: string) => {
    const meta = appIconMeta[icon];
    return `${meta[isZh(lang) ? 'zh' : 'en']} (${icon})`;
};

const appAccentSwatches = [
    { value: '#2f5f98', zh: '\u84dd\u8272', en: 'Blue' },
    { value: '#657a42', zh: '\u7eff\u8272', en: 'Green' },
    { value: '#7c3f58', zh: '\u7d2b\u7ea2', en: 'Plum' },
    { value: '#b45309', zh: '\u7425\u73c0', en: 'Amber' },
    { value: '#28705f', zh: '\u9752\u7eff', en: 'Teal' },
    { value: '#4b6572', zh: '\u7070\u84dd', en: 'Slate' },
    { value: '#8a5a44', zh: '\u68d5\u8272', en: 'Brown' },
    { value: '#5b5ea6', zh: '\u975b\u84dd', en: 'Indigo' },
    { value: '#6b7280', zh: '\u4e2d\u6027\u7070', en: 'Gray' },
];

const defaultAccentForKind = (kind: AppKind) => kind === 'enterprise_approval_app' ? '#2f5f98' : kind === 'enterprise_normal_app' ? '#4b6572' : kind === 'automation_app' ? '#4b6572' : '#28705f';

function normalizeAppKind(raw: unknown): AppKind {
    if (raw === 'enterprise_approval_app' || raw === 'approval_app') return 'enterprise_approval_app';
    if (raw === 'enterprise_normal_app' || raw === 'enterprise_app' || raw === 'business_app') return 'enterprise_normal_app';
    if (raw === 'tool_app') return 'tool_app';
    if (raw === 'automation_app') return 'automation_app';
    return 'tool_app';
}

const isEnterpriseAppKind = (kind: AppKind) => kind === 'enterprise_approval_app' || kind === 'enterprise_normal_app';
const isEnterpriseApprovalAppKind = (kind: AppKind) => kind === 'enterprise_approval_app';

const appAccentLabel = (swatch: { value: string; zh: string; en: string }, lang?: string) => `${swatch[isZh(lang) ? 'zh' : 'en']} ${swatch.value}`;

function makeEnterpriseManifest(kind: 'enterprise_approval_app' | 'enterprise_normal_app', domain: string, preferredAction: string, preferredView: string, preferredReport: string, preferredDashboard: string, appSkillID = '', dependencies: AppSkillDependency[] = [], approvalBinding?: Pick<AppApprovalBinding, 'event' | 'objectRole'>, appSkillSource: AppSkillDependency['source'] = 'local'): AppManifestBinding {
    const approvalWorkflow = kind === 'enterprise_approval_app' ? dependencies.find((dep) => dep.kind === 'workflow_skill' && dep.id) : undefined;
    const approvalEvent = approvalBinding?.event?.trim() || (domain || 'record') + '.submitted';
    const approvalObjectRole = approvalBinding?.objectRole?.trim() || domain || undefined;
    const approvalBindings = approvalWorkflow ? [{ event: approvalEvent, workflowSkillId: approvalWorkflow.id, workflowVersion: approvalWorkflow.version, objectRole: approvalObjectRole }] : undefined;
    return {
        schema: 'maclaw.app.v1',
        installUnit: 'enterprise_app_pack',
        privateMarker: 'x_maclaw_apps',
        entryKind: kind,
        launchMode: 'agent_dynamic_ui',
        datasrv: { domain, preferredAction, preferredView, preferredReport, preferredDashboard },
        mis: approvalBindings ? { approvalBindings } : undefined,
        appSkill: appSkillID ? { id: appSkillID, version: '1.0.0', source: appSkillSource } : undefined,
        dependencies: dependencies.length ? { skills: dependencies } : undefined,
        workflow: defaultAppWorkflowMapping(kind, domain, approvalObjectRole),
        ui: defaultWorkspaceLayoutForKind(kind),
    };
}

function makeDataSrvManifest(domain: string, preferredAction: string, preferredView: string, preferredReport: string, preferredDashboard: string): AppManifestBinding {
    return makeEnterpriseManifest('enterprise_normal_app', domain, preferredAction, preferredView, preferredReport, preferredDashboard);
}

function makeSkillManifest(id: string, inputMode: 'file' | 'form' | 'mixed', outputModes: string[] = ['docx', 'pdf'], fields: SkillAppField[] = [], multipleFiles = false, appDefinitionFile = 'maclaw.apps.json'): AppManifestBinding {
    return {
        schema: 'maclaw.app.v1',
        installUnit: 'skill',
        privateMarker: 'x_maclaw_apps',
        entryKind: 'tool_app',
        launchMode: 'fixed_skill_ui',
        appSkill: { id, version: '1.0.0' },
        ui: defaultWorkspaceLayoutForKind('tool_app'),
        skill: { id, appDefinitionFile, inputMode, multipleFiles, outputModes: normalizeOutputModes(outputModes), fields: normalizeSkillAppFields(fields) },
    };
}


function defaultWorkspaceLayoutForKind(kind: AppKind): AppWorkspaceLayout {
    const base = { schema: 'maclaw.app.ui.v1' as const, generated: true };
    if (kind === 'enterprise_approval_app') {
        return {
            ...base,
            entry: 'approval_workspace',
            layouts: {
                approval_workspace: {
                    type: 'split_view',
                    toolbar: ['create_request', 'refresh', 'export', 'filter'],
                    navigation: ['my_requests', 'pending_my_approval', 'handled', 'attention', 'all'],
                    list: { columns: ['title', 'applicant', 'current_node', 'status', 'updated_at'] },
                    detail: { sections: ['summary', 'form_data', 'attachments', 'timeline', 'approval_actions', 'result'] },
                },
            },
        };
    }
    if (kind === 'enterprise_normal_app') {
        return {
            ...base,
            entry: 'business_workspace',
            layouts: {
                business_workspace: {
                    type: 'split_view',
                    toolbar: ['new_record', 'query', 'refresh', 'export'],
                    navigation: ['records', 'recent', 'needs_attention'],
                    list: { columns: ['title', 'status', 'owner', 'updated_at'] },
                    detail: { sections: ['form_panel', 'business_record', 'operation_history', 'output_panel'] },
                },
            },
        };
    }
    return {
        ...base,
        entry: 'tool_workspace',
        layouts: {
            tool_workspace: {
                type: 'tool_workspace',
                toolbar: ['add_file', 'run', 'cancel', 'open_output'],
                regions: ['file_queue', 'preview_panel', 'settings_panel', 'output_panel'],
            },
        },
    };
}

function workspaceEntryForKind(kind: AppKind): string {
    if (kind === 'enterprise_approval_app') return 'approval_workspace';
    if (kind === 'enterprise_normal_app') return 'business_workspace';
    return 'tool_workspace';
}

function studioRegionsForLayout(kind: AppKind, template: StudioLayoutTemplate, primaryRegion: StudioPrimaryRegion, outputRegion: StudioOutputRegion): RuntimeWorkspaceRegion[] {
    if (kind === 'tool_app') {
        return [
            { id: 'file_queue', role: 'input', placement: primaryRegion },
            { id: 'settings_panel', role: 'parameters', placement: primaryRegion === 'left' ? 'right' : 'left' },
            { id: 'preview_panel', role: 'preview', placement: 'center' },
            { id: 'output_panel', role: 'output', placement: outputRegion },
        ];
    }
    if (kind === 'enterprise_approval_app') {
        return [
            { id: 'request_form', role: 'input', placement: primaryRegion },
            { id: 'approval_inbox', role: 'instance_list', placement: template === 'left_nav' ? 'left' : 'center' },
            { id: 'approval_detail', role: 'detail', placement: 'center' },
            { id: 'result_panel', role: 'output', placement: outputRegion },
        ];
    }
    return [
        { id: 'operation_form', role: 'input', placement: primaryRegion },
        { id: 'record_list', role: 'record_list', placement: template === 'left_nav' ? 'left' : 'center' },
        { id: 'record_detail', role: 'detail', placement: 'center' },
        { id: 'output_panel', role: 'output', placement: outputRegion },
    ];
}

function applyStudioWorkspaceLayout(manifest: AppManifestBinding, kind: AppKind, options: RuntimeWorkspaceLayout): AppManifestBinding {
    const baseUI = manifest.ui || defaultWorkspaceLayoutForKind(kind);
    const entry = baseUI.entry || workspaceEntryForKind(kind);
    const currentLayouts = baseUI.layouts || {};
    const currentLayout = currentLayouts[entry] || {};
    const regions = canonicalRuntimeWorkspaceRegions(normalizeRuntimeWorkspaceRegions(options.regions, kind, options.template, options.primaryRegion, options.outputRegion));
    const fingerprint = workspaceLayoutFingerprint(entry, { ...options, regions });
    return {
        ...manifest,
        ui: {
            ...baseUI,
            generated: true,
            entry,
            layouts: {
                ...currentLayouts,
                [entry]: {
                    ...currentLayout,
                    template: options.template,
                    density: options.density,
                    primaryRegion: options.primaryRegion,
                    outputRegion: options.outputRegion,
                    regions,
                    fingerprint,
                    studio: {
                        editable: true,
                        savedInManifest: true,
                        updatedBy: 'app_studio',
                    },
                },
            },
        },
    };
}

function defaultEnterpriseNavigation(kind: AppKind) {
    return kind === 'enterprise_approval_app' ? ['my_requests', 'pending_my_approval', 'handled', 'attention', 'all'] : ['records', 'recent', 'needs_attention'];
}

function defaultEnterpriseColumns(kind: AppKind) {
    return kind === 'enterprise_approval_app' ? ['title', 'applicant', 'current_node', 'status', 'updated_at'] : ['title', 'status', 'owner', 'updated_at'];
}

function normalizeUIStringList(value: unknown, fallback: string[]) {
    const raw = Array.isArray(value) ? value : [];
    const out = raw.map((item) => String(item || '').trim()).filter(Boolean);
    return out.length ? Array.from(new Set(out)) : fallback;
}

function appWorkspaceLayoutConfig(app: AppEntry) {
    const entry = app.manifest?.ui?.entry || workspaceEntryForKind(app.kind);
    return app.manifest?.ui?.layouts?.[entry] || {};
}

function workspaceLayoutFingerprint(entry: string, runtime: RuntimeWorkspaceLayout) {
    return textHash(stableStringify({
        entry,
        template: runtime.template,
        density: runtime.density,
        primaryRegion: runtime.primaryRegion,
        outputRegion: runtime.outputRegion,
        regions: runtime.regions.map((region, index) => ({
            id: region.id,
            role: region.role,
            placement: region.placement,
            visible: region.visible !== false,
            order: region.order || index + 1,
        })),
    }));
}

function canonicalRuntimeWorkspaceRegions(regions: RuntimeWorkspaceRegion[]) {
    return [...regions]
        .sort((a, b) => (a.order || regions.indexOf(a) + 1) - (b.order || regions.indexOf(b) + 1))
        .map((region, index) => ({
            ...region,
            order: region.order || index + 1,
        }));
}

function appEnterpriseNavigation(app: AppEntry) {
    return normalizeUIStringList(appWorkspaceLayoutConfig(app).navigation, defaultEnterpriseNavigation(app.kind));
}

function appEnterpriseColumns(app: AppEntry) {
    return normalizeUIStringList(appWorkspaceLayoutConfig(app).list?.columns, defaultEnterpriseColumns(app.kind));
}

function applyEnterpriseUIConfig(manifest: AppManifestBinding, kind: AppKind, navigation: string[], columns: string[]): AppManifestBinding {
    if (!isEnterpriseAppKind(kind)) return manifest;
    const baseUI = manifest.ui || defaultWorkspaceLayoutForKind(kind);
    const entry = baseUI.entry || workspaceEntryForKind(kind);
    const currentLayouts = baseUI.layouts || {};
    const currentLayout = currentLayouts[entry] || {};
    return {
        ...manifest,
        ui: {
            ...baseUI,
            entry,
            layouts: {
                ...currentLayouts,
                [entry]: {
                    ...currentLayout,
                    navigation: normalizeUIStringList(navigation, defaultEnterpriseNavigation(kind)),
                    list: {
                        ...(currentLayout.list || {}),
                        columns: normalizeUIStringList(columns, defaultEnterpriseColumns(kind)),
                    },
                },
            },
        },
    };
}

function defaultRuntimeWorkspaceLayout(kind: AppKind): RuntimeWorkspaceLayout {
    const template = kind === 'tool_app' ? 'document_workspace' : 'classic_split';
    const primaryRegion: StudioPrimaryRegion = 'left';
    const outputRegion: StudioOutputRegion = kind === 'tool_app' ? 'right' : 'bottom';
    return {
        template,
        density: 'comfortable',
        primaryRegion,
        outputRegion,
        regions: studioRegionsForLayout(kind, template, primaryRegion, outputRegion),
    };
}

function normalizeRuntimeWorkspaceRegions(value: unknown, kind: AppKind, template: StudioLayoutTemplate, primaryRegion: StudioPrimaryRegion, outputRegion: StudioOutputRegion): RuntimeWorkspaceRegion[] {
    const allowedPlacements = new Set(['left', 'center', 'right', 'bottom', 'modal']);
    const normalized = (Array.isArray(value) ? value : []).reduce<RuntimeWorkspaceRegion[]>((regions, item) => {
        const raw = item && typeof item === 'object' ? item as Record<string, unknown> : {};
        const id = String(raw.id || '').trim();
        const role = String(raw.role || '').trim();
        const placement = String(raw.placement || '').trim();
        if (!id || !role || !allowedPlacements.has(placement)) return regions;
        const region: RuntimeWorkspaceRegion = { id, role, placement: placement as RuntimeWorkspaceRegion['placement'] };
        if (raw.visible === false) region.visible = false;
        const order = Number(raw.order);
        if (Number.isFinite(order) && order > 0) region.order = Math.floor(order);
        regions.push(region);
        return regions;
    }, []);
    return normalized.length ? normalized : studioRegionsForLayout(kind, template, primaryRegion, outputRegion);
}

function normalizeRuntimeWorkspaceLayout(raw: any, kind: AppKind): RuntimeWorkspaceLayout {
    const fallback = defaultRuntimeWorkspaceLayout(kind);
    const template = ['classic_split', 'left_nav', 'document_workspace', 'dashboard'].includes(String(raw?.template)) ? raw.template as StudioLayoutTemplate : fallback.template;
    const density = ['compact', 'comfortable', 'spacious'].includes(String(raw?.density)) ? raw.density as StudioLayoutDensity : fallback.density;
    const primaryRegion = ['left', 'center', 'right'].includes(String(raw?.primaryRegion)) ? raw.primaryRegion as StudioPrimaryRegion : fallback.primaryRegion;
    const outputRegion = ['right', 'bottom', 'modal'].includes(String(raw?.outputRegion)) ? raw.outputRegion as StudioOutputRegion : fallback.outputRegion;
    return {
        template,
        density,
        primaryRegion,
        outputRegion,
        regions: normalizeRuntimeWorkspaceRegions(raw?.regions, kind, template, primaryRegion, outputRegion),
    };
}

function installEvidenceWorkspaceLayoutCandidate(app: AppEntry, entry: string): unknown {
    const raw = app.installEvidence?.workspace_layout;
    if (!raw || typeof raw !== 'object') return undefined;
    const layout = raw as Record<string, any>;
    const layouts = layout.layouts && typeof layout.layouts === 'object' ? layout.layouts as Record<string, unknown> : undefined;
    if (layouts) {
        const layoutEntry = String(layout.entry || entry || '').trim();
        return layouts[entry] || (layoutEntry ? layouts[layoutEntry] : undefined);
    }
    return layout;
}

function runtimeWorkspaceLayoutForApp(app: AppEntry): RuntimeWorkspaceLayout {
    const entry = app.manifest?.ui?.entry || workspaceEntryForKind(app.kind);
    const layout = app.manifest?.ui?.layouts?.[entry] || installEvidenceWorkspaceLayoutCandidate(app, entry) || {};
    return normalizeRuntimeWorkspaceLayout(layout, app.kind);
}

function runtimeWorkspaceOrder(layout: RuntimeWorkspaceLayout) {
    const outputEarly = layout.outputRegion === 'right' || layout.outputRegion === 'modal';
    const hasCustomOrder = layout.regions.some((region) => Number.isFinite(region.order));
    if (hasCustomOrder) {
        const regionOrder = (roles: string[], fallback: number) => {
            const matches = layout.regions
                .filter((region) => region.visible !== false && roles.includes(region.role) && Number.isFinite(region.order))
                .map((region) => Number(region.order));
            return matches.length ? Math.min(...matches) * 10 : fallback;
        };
        const input = regionOrder(['input'], 10);
        const approval = regionOrder(['instance_list', 'record_list', 'preview'], 20);
        const status = regionOrder(['detail', 'parameters'], outputEarly ? 40 : 30);
        const output = regionOrder(['output'], outputEarly ? 30 : 50);
        return {
            input,
            approval,
            status,
            actions: input + 1,
            output,
            history: output + 1,
        };
    }    return {
        input: 10,
        approval: 20,
        status: outputEarly ? 40 : 30,
        actions: outputEarly ? 50 : 40,
        output: outputEarly ? 30 : 50,
        history: 60,
    };
}
const studioLayoutTemplateOptions: Array<{ value: StudioLayoutTemplate; zh: string; en: string }> = [
    { value: 'document_workspace', zh: '\u6587\u6863\u5de5\u4f5c\u53f0', en: 'Document workspace' },
    { value: 'classic_split', zh: '\u7ecf\u5178\u5206\u680f', en: 'Classic split' },
    { value: 'left_nav', zh: '\u5de6\u4fa7\u5bfc\u822a', en: 'Left navigation' },
    { value: 'dashboard', zh: '\u770b\u677f\u5de5\u4f5c\u53f0', en: 'Dashboard' },
];
const studioLayoutDensityOptions: Array<{ value: StudioLayoutDensity; zh: string; en: string }> = [
    { value: 'comfortable', zh: '\u6807\u51c6', en: 'Comfortable' },
    { value: 'compact', zh: '\u7d27\u51d1', en: 'Compact' },
    { value: 'spacious', zh: '\u5bbd\u677e', en: 'Spacious' },
];
const studioPrimaryRegionOptions: Array<{ value: StudioPrimaryRegion; zh: string; en: string }> = [
    { value: 'left', zh: '\u5de6\u4fa7', en: 'Left' },
    { value: 'center', zh: '\u4e2d\u95f4', en: 'Center' },
    { value: 'right', zh: '\u53f3\u4fa7', en: 'Right' },
];
const studioOutputRegionOptions: Array<{ value: StudioOutputRegion; zh: string; en: string }> = [
    { value: 'right', zh: '\u53f3\u4fa7', en: 'Right' },
    { value: 'bottom', zh: '\u5e95\u90e8', en: 'Bottom' },
    { value: 'modal', zh: '\u5f39\u7a97', en: 'Modal' },
];
const studioLayoutSlotIds = ['left', 'center', 'right'] as const;

type StudioLayoutDesignerProps = {
    kind: AppKind;
    value: RuntimeWorkspaceLayout;
    onChange: (layout: RuntimeWorkspaceLayout) => void;
    lang?: string;
    testIdPrefix?: string;
};

function studioLayoutOptionLabel<T extends string>(options: Array<{ value: T; zh: string; en: string }>, value: T, lang?: string) {
    const option = options.find((item) => item.value === value) || options[0];
    return option[isZh(lang) ? 'zh' : 'en'];
}

function studioLayoutRegionLabel(kind: AppKind, id: string, lang?: string) {
    const zh = isZh(lang);
    const labelsById: Record<string, { zh: string; en: string }> = {
        request_form: { zh: '\u53d1\u8d77\u8868\u5355', en: 'Request form' },
        approval_inbox: { zh: '\u5ba1\u6279\u5b9e\u4f8b', en: 'Approval instances' },
        approval_detail: { zh: '\u8282\u70b9\u72b6\u6001', en: 'Node status' },
        result_panel: { zh: '\u7ed3\u679c\u53cd\u9988', en: 'Result feedback' },
        operation_form: { zh: '\u64cd\u4f5c\u8868\u5355', en: 'Operation form' },
        record_list: { zh: '\u6570\u636e\u5217\u8868', en: 'Record list' },
        record_detail: { zh: '\u6570\u636e\u660e\u7ec6', en: 'Record detail' },
        output_panel: { zh: '\u8f93\u51fa\u9762\u677f', en: 'Output panel' },
        file_queue: { zh: '\u6587\u4ef6\u961f\u5217', en: 'File queue' },
        settings_panel: { zh: '\u53c2\u6570\u533a', en: 'Parameters' },
        preview_panel: { zh: '\u9884\u89c8\u533a', en: 'Preview' },
    };
    return labelsById[id]?.[zh ? 'zh' : 'en'] || (kind === 'tool_app' ? (zh ? '\u5de5\u5177\u9762\u677f' : 'Tool panel') : id);
}

function studioLayoutRoleLabel(role: string, lang?: string) {
    const zh = isZh(lang);
    const labelsByRole: Record<string, { zh: string; en: string }> = {
        input: { zh: '\u8f93\u5165', en: 'Input' },
        parameters: { zh: '\u53c2\u6570', en: 'Parameters' },
        preview: { zh: '\u9884\u89c8', en: 'Preview' },
        output: { zh: '\u8f93\u51fa', en: 'Output' },
        instance_list: { zh: '\u5b9e\u4f8b', en: 'Instances' },
        detail: { zh: '\u660e\u7ec6', en: 'Detail' },
        record_list: { zh: '\u5217\u8868', en: 'List' },
    };
    return labelsByRole[role]?.[zh ? 'zh' : 'en'] || role;
}

const StudioLayoutDesigner = ({ kind, value, onChange, lang, testIdPrefix = 'studio' }: StudioLayoutDesignerProps) => {
    const zh = isZh(lang);
    const regions = normalizeRuntimeWorkspaceRegions(value.regions, kind, value.template, value.primaryRegion, value.outputRegion);
    const updateLayout = (patch: Partial<RuntimeWorkspaceLayout>) => {
        const next = { ...value, ...patch };
        const resetRegions = patch.template !== undefined || patch.primaryRegion !== undefined || patch.outputRegion !== undefined;
        onChange({ ...next, regions: resetRegions ? studioRegionsForLayout(kind, next.template, next.primaryRegion, next.outputRegion) : regions });
    };
    const placementOptions: Array<{ value: RuntimeWorkspaceRegion['placement']; zh: string; en: string }> = [
        { value: 'left', zh: '\u5de6\u4fa7', en: 'Left' },
        { value: 'center', zh: '\u4e2d\u95f4', en: 'Center' },
        { value: 'right', zh: '\u53f3\u4fa7', en: 'Right' },
        { value: 'bottom', zh: '\u5e95\u90e8', en: 'Bottom' },
        { value: 'modal', zh: '\u5f39\u7a97', en: 'Modal' },
    ];
    const updateRegionPlacement = (regionID: string, placement: RuntimeWorkspaceRegion['placement']) => {
        const nextRegions = regions.map((region) => region.id === regionID ? { ...region, placement } : region);
        const changedRegion = nextRegions.find((region) => region.id === regionID);
        const summaryPatch: Partial<RuntimeWorkspaceLayout> = {};
        if (changedRegion?.role === 'input' && (placement === 'left' || placement === 'center' || placement === 'right')) summaryPatch.primaryRegion = placement;
        if (changedRegion?.role === 'output' && (placement === 'right' || placement === 'bottom' || placement === 'modal')) summaryPatch.outputRegion = placement;
        onChange({ ...value, ...summaryPatch, regions: nextRegions });
    };
    const updateRegionVisibility = (regionID: string, visible: boolean) => {
        onChange({ ...value, regions: regions.map((region) => region.id === regionID ? { ...region, visible: visible ? undefined : false } : region) });
    };
    const reorderRegion = (regionID: string, direction: -1 | 1) => {
        const index = regions.findIndex((region) => region.id === regionID);
        const targetIndex = index + direction;
        if (index < 0 || targetIndex < 0 || targetIndex >= regions.length) return;
        const nextRegions = [...regions];
        const [moved] = nextRegions.splice(index, 1);
        nextRegions.splice(targetIndex, 0, moved);
        onChange({ ...value, regions: nextRegions.map((region, orderIndex) => ({ ...region, order: orderIndex + 1 })) });
    };
    const orderedRegions = [...regions].sort((a, b) => (a.order || regions.indexOf(a) + 1) - (b.order || regions.indexOf(b) + 1));
    const visibleRegionCount = orderedRegions.filter((region) => region.visible !== false).length;
    const layoutFingerprint = workspaceLayoutFingerprint(workspaceEntryForKind(kind), { ...value, regions: orderedRegions });
    const regionsForPlacement = (placement: StudioPrimaryRegion | 'bottom') => orderedRegions.filter((region) => region.visible !== false && (region.placement === placement || (placement === 'right' && region.placement === 'modal')));
    const handleSlotKeyDown = (event: KeyboardEvent, action: () => void) => {
        if (event.key !== 'Enter' && event.key !== ' ') return;
        event.preventDefault();
        action();
    };
    const renderRegionPill = (region: { id: string; role: string; placement: string }) => (
        <span className="apps-layout-designer__region" data-role={region.role} key={region.id}>
            <strong>{studioLayoutRegionLabel(kind, region.id, lang)}</strong>
            <small>{studioLayoutRoleLabel(region.role, lang)}</small>
            <span className="apps-layout-designer__region-actions" aria-label={zh ? '\u79fb\u52a8\u533a\u57df' : 'Move region'}>
                {placementOptions.map((option) => (
                    <button
                        key={option.value}
                        type="button"
                        data-active={region.placement === option.value ? 'true' : undefined}
                        data-testid={`${testIdPrefix}-layout-region-${region.id}-move-${option.value}`}
                        aria-label={`${zh ? '\u79fb\u52a8' : 'Move'} ${studioLayoutRegionLabel(kind, region.id, lang)} ${zh ? '\u5230' : 'to'} ${option[zh ? 'zh' : 'en']}`}
                        title={option[zh ? 'zh' : 'en']}
                        onClick={(event) => {
                            event.stopPropagation();
                            updateRegionPlacement(region.id, option.value);
                        }}
                    >
                        {option[zh ? 'zh' : 'en'].slice(0, 1)}
                    </button>
                ))}
            </span>
        </span>
    );
    const renderSlot = (slot: typeof studioLayoutSlotIds[number]) => {
        const slotRegions = regionsForPlacement(slot);
        const isPrimary = value.primaryRegion === slot;
        const hasOutput = value.outputRegion === slot || (slot === 'right' && value.outputRegion === 'modal');
        return (
            <div
                key={slot}
                className="apps-layout-designer__slot"
                data-slot={slot}
                data-primary={isPrimary ? 'true' : undefined}
                data-output={hasOutput ? value.outputRegion : undefined}
                data-testid={`${testIdPrefix}-layout-slot-${slot}`}
                role="button"
                tabIndex={0}
                aria-pressed={isPrimary}
                onClick={() => updateLayout({ primaryRegion: slot })}
                onKeyDown={(event) => handleSlotKeyDown(event, () => updateLayout({ primaryRegion: slot }))}
            >
                <span className="apps-layout-designer__slot-title">
                    {studioLayoutOptionLabel(studioPrimaryRegionOptions, slot, lang)}
                    {isPrimary && <em>{zh ? '\u4e3b\u64cd\u4f5c' : 'Primary'}</em>}
                </span>
                <span className="apps-layout-designer__slot-body">
                    {slotRegions.length ? slotRegions.map(renderRegionPill) : <span className="apps-layout-designer__empty-slot">{zh ? '\u53ef\u653e\u7f6e\u533a\u57df' : 'Available region'}</span>}
                </span>
            </div>
        );
    };
    return (
        <section className="apps-layout-designer" aria-label={zh ? '\u754c\u9762\u5e03\u5c40' : 'UI layout'}>
            <div className="apps-preview-title-row">
                <div>
                    <div className="apps-definition__title">{zh ? '\u754c\u9762\u5e03\u5c40' : 'UI layout'}</div>
                    <small className="apps-layout-designer__hint">{zh ? '\u70b9\u51fb\u9884\u89c8\u533a\u57df\u8c03\u6574\u4e3b\u64cd\u4f5c\u4f4d\u7f6e\uff0c\u8f93\u51fa\u4f4d\u7f6e\u4f1a\u5199\u5165 manifest\u3002' : 'Click the preview regions to place the primary work area; output placement is saved to the manifest.'}</small>
                </div>
                <span className="apps-count">{zh ? '\u4fdd\u5b58\u5230 manifest' : 'Saved in manifest'}</span>
            </div>
            <div className="apps-layout-designer__body">
                <div className="apps-layout-designer__preview" data-template={value.template} data-density={value.density}>
                    <div className="apps-layout-designer__template-row" role="group" aria-label={zh ? '\u5e03\u5c40\u6a21\u677f' : 'Layout template'}>
                        {studioLayoutTemplateOptions.map((option) => (
                            <button
                                key={option.value}
                                className="apps-layout-designer__template"
                                data-active={value.template === option.value ? 'true' : undefined}
                                data-testid={`${testIdPrefix}-layout-template-${option.value}`}
                                type="button"
                                aria-pressed={value.template === option.value}
                                onClick={() => updateLayout({ template: option.value })}
                            >
                                {option[zh ? 'zh' : 'en']}
                            </button>
                        ))}
                    </div>
                    <div className="apps-layout-designer__evidence" data-testid={`${testIdPrefix}-layout-evidence`}>
                        <span>
                            <strong>{zh ? '\u5e03\u5c40' : 'Layout'}</strong>
                            <small>{value.template} · {value.density}</small>
                        </span>
                        <span>
                            <strong>{zh ? '\u4e3b\u533a' : 'Primary'}</strong>
                            <small>{value.primaryRegion}</small>
                        </span>
                        <span>
                            <strong>{zh ? '\u8f93\u51fa' : 'Output'}</strong>
                            <small>{value.outputRegion}</small>
                        </span>
                        <span>
                            <strong>{zh ? '\u533a\u57df' : 'Regions'}</strong>
                            <small>{visibleRegionCount}/{orderedRegions.length}</small>
                        </span>
                        <span>
                            <strong>{zh ? '\u6307\u7eb9' : 'Fingerprint'}</strong>
                            <code>{layoutFingerprint}</code>
                        </span>
                    </div>
                    <div className="apps-layout-designer__canvas" data-testid={`${testIdPrefix}-layout-canvas`} data-template={value.template} data-density={value.density}>
                        {studioLayoutSlotIds.map(renderSlot)}
                        <div
                            className="apps-layout-designer__slot apps-layout-designer__slot--bottom"
                            data-slot="bottom"
                            data-output={value.outputRegion === 'bottom' ? 'bottom' : undefined}
                            data-testid={`${testIdPrefix}-layout-slot-bottom`}
                            role="button"
                            tabIndex={0}
                            aria-pressed={value.outputRegion === 'bottom'}
                            onClick={() => updateLayout({ outputRegion: 'bottom' })}
                            onKeyDown={(event) => handleSlotKeyDown(event, () => updateLayout({ outputRegion: 'bottom' }))}
                        >
                            <span className="apps-layout-designer__slot-title">
                                {zh ? '\u5e95\u90e8' : 'Bottom'}
                                {value.outputRegion === 'bottom' && <em>{zh ? '\u8f93\u51fa' : 'Output'}</em>}
                            </span>
                            <span className="apps-layout-designer__slot-body">
                                {regionsForPlacement('bottom').length ? regionsForPlacement('bottom').map(renderRegionPill) : <span className="apps-layout-designer__empty-slot">{zh ? '\u7ed3\u679c\u533a\u6216\u65e5\u5fd7\u533a' : 'Results or log lane'}</span>}
                            </span>
                        </div>
                    </div>
                </div>
                <div className="apps-layout-designer__controls">
                    <div className="apps-layout-designer__grid">
                        <div className="apps-form-row">
                            <label>{zh ? '\u5e03\u5c40\u6a21\u677f' : 'Layout template'}</label>
                            <select data-testid={`${testIdPrefix}-layout-template`} value={value.template} onChange={(event) => updateLayout({ template: event.target.value as StudioLayoutTemplate })}>
                                {studioLayoutTemplateOptions.map((option) => <option key={option.value} value={option.value}>{option[zh ? 'zh' : 'en']}</option>)}
                            </select>
                        </div>
                        <div className="apps-form-row">
                            <label>{zh ? '\u5bc6\u5ea6' : 'Density'}</label>
                            <select data-testid={`${testIdPrefix}-layout-density`} value={value.density} onChange={(event) => updateLayout({ density: event.target.value as StudioLayoutDensity })}>
                                {studioLayoutDensityOptions.map((option) => <option key={option.value} value={option.value}>{option[zh ? 'zh' : 'en']}</option>)}
                            </select>
                        </div>
                        <div className="apps-form-row">
                            <label>{zh ? '\u4e3b\u64cd\u4f5c\u533a' : 'Primary region'}</label>
                            <select data-testid={`${testIdPrefix}-primary-region`} value={value.primaryRegion} onChange={(event) => updateLayout({ primaryRegion: event.target.value as StudioPrimaryRegion })}>
                                {studioPrimaryRegionOptions.map((option) => <option key={option.value} value={option.value}>{option[zh ? 'zh' : 'en']}</option>)}
                            </select>
                        </div>
                        <div className="apps-form-row">
                            <label>{zh ? '\u8f93\u51fa\u533a' : 'Output region'}</label>
                            <select data-testid={`${testIdPrefix}-output-region`} value={value.outputRegion} onChange={(event) => updateLayout({ outputRegion: event.target.value as StudioOutputRegion })}>
                                {studioOutputRegionOptions.map((option) => <option key={option.value} value={option.value}>{option[zh ? 'zh' : 'en']}</option>)}
                            </select>
                        </div>
                    </div>
                    <div className="apps-form-row apps-layout-designer__output-control-row">
                        <span className="apps-layout-designer__control-spacer" aria-hidden="true" />
                        <div className="apps-layout-designer__output-row" role="group" aria-label={zh ? '\u8f93\u51fa\u4f4d\u7f6e' : 'Output placement'}>
                            {studioOutputRegionOptions.map((option) => (
                                <button
                                    key={option.value}
                                    className="apps-layout-designer__output"
                                    data-active={value.outputRegion === option.value ? 'true' : undefined}
                                    data-testid={`${testIdPrefix}-layout-output-${option.value}`}
                                    type="button"
                                    aria-pressed={value.outputRegion === option.value}
                                    onClick={() => updateLayout({ outputRegion: option.value })}
                                >
                                    {option[zh ? 'zh' : 'en']}
                                </button>
                            ))}
                        </div>
                    </div>
                    <div className="apps-layout-designer__region-controls" aria-label={zh ? '\u533a\u57df\u4f4d\u7f6e' : 'Region placement'}>
                        <div className="apps-layout-designer__region-controls-head">
                            <strong>{zh ? '\u533a\u57df\u4f4d\u7f6e' : 'Region placement'}</strong>
                            <span>{zh ? '\u5199\u5165 manifest.regions' : 'Saved to manifest.regions'}</span>
                        </div>
                        {orderedRegions.map((region, index) => (
                            <div className="apps-layout-designer__region-control" data-visible={region.visible === false ? 'false' : 'true'} key={region.id}>
                                <label className="apps-layout-designer__region-visible">
                                    <input
                                        data-testid={`${testIdPrefix}-layout-region-visible-${region.id}`}
                                        type="checkbox"
                                        checked={region.visible !== false}
                                        onChange={(event) => updateRegionVisibility(region.id, event.target.checked)}
                                    />
                                    <span>
                                        <strong>{studioLayoutRegionLabel(kind, region.id, lang)}</strong>
                                        <small>{studioLayoutRoleLabel(region.role, lang)}</small>
                                    </span>
                                </label>
                                <select
                                    data-testid={`${testIdPrefix}-layout-region-${region.id}`}
                                    disabled={region.visible === false}
                                    value={region.placement}
                                    onChange={(event) => updateRegionPlacement(region.id, event.target.value as RuntimeWorkspaceRegion['placement'])}
                                >
                                    {placementOptions.map((option) => <option key={option.value} value={option.value}>{option[zh ? 'zh' : 'en']}</option>)}
                                </select>
                                <div className="apps-layout-designer__order" role="group" aria-label={zh ? '\u533a\u57df\u987a\u5e8f' : 'Region order'}>
                                    <button type="button" data-testid={`${testIdPrefix}-layout-region-${region.id}-order-up`} disabled={index === 0} onClick={() => reorderRegion(region.id, -1)}>{zh ? '\u4e0a\u79fb' : 'Up'}</button>
                                    <button type="button" data-testid={`${testIdPrefix}-layout-region-${region.id}-order-down`} disabled={index === orderedRegions.length - 1} onClick={() => reorderRegion(region.id, 1)}>{zh ? '\u4e0b\u79fb' : 'Down'}</button>
                                </div>
                            </div>
                        ))}
                    </div>
                </div>
            </div>
        </section>
    );
};
function normalizeAppDependencies(raw: unknown): AppManifestBinding['dependencies'] {
    const value = raw && typeof raw === 'object' ? raw as { skills?: unknown } : {};
    if (!Array.isArray(value.skills)) return undefined;
    const skills: AppSkillDependency[] = [];
    value.skills.forEach((item) => {
        const dep = item && typeof item === 'object' ? item as AppSkillDependency : undefined;
        const id = String(dep?.id || '').trim();
        if (!id) return;
        skills.push({
            id,
            version: dep?.version ? String(dep.version) : undefined,
            kind: dep?.kind,
            required: dep?.required !== false,
            source: dep?.source || 'hub',
            install_ref: appSkillDependencyInstallRef(dep) || undefined,
            canonical_id: String(dep?.canonical_id || dep?.canonicalID || '').trim() || undefined,
            aliases: Array.isArray(dep?.aliases) ? dep.aliases.map((alias) => String(alias || '').trim()).filter(Boolean) : undefined,
            capabilities: Array.isArray(dep?.capabilities) ? dep.capabilities.map((capability) => String(capability || '').trim()).filter(Boolean) : undefined,
        });
    });
    return skills.length ? { skills } : undefined;
}

function normalizeAppMIS(raw: unknown): AppManifestBinding['mis'] {
    const value = raw && typeof raw === 'object' ? raw as { approvalBindings?: unknown; approval_bindings?: unknown } : {};
    const rawBindings = Array.isArray(value.approvalBindings) ? value.approvalBindings : Array.isArray(value.approval_bindings) ? value.approval_bindings : [];
    const approvalBindings = rawBindings.reduce<AppApprovalBinding[]>((items, item) => {
        const binding = item && typeof item === 'object' ? item as Record<string, unknown> : {};
        const event = String(binding.event || binding.businessEvent || binding.business_event || '').trim();
        const workflowSkillId = String(binding.workflowSkillId || binding.workflow_skill_id || binding.workflowId || binding.workflow_id || '').trim();
        if (!event || !workflowSkillId) return items;
        items.push({
            event,
            workflowSkillId,
            workflowVersion: String(binding.workflowVersion || binding.workflow_version || '').trim() || undefined,
            objectRole: String(binding.objectRole || binding.object_role || '').trim() || undefined,
        });
        return items;
    }, []);
    return approvalBindings.length ? { approvalBindings } : undefined;
}

function normalizeAppDataSrv(raw: unknown): AppManifestBinding['datasrv'] {
    const value = raw && typeof raw === 'object' ? raw as Record<string, unknown> : {};
    const datasetID = String(value.datasetID || value.dataset_id || value.dataset || '').trim();
    const domain = String(value.domain || '').trim() || datasetID.split('.')[0] || '';
    if (!domain && !datasetID) return undefined;
    const datasrv: NonNullable<AppManifestBinding['datasrv']> = { domain };
    if (datasetID) datasrv.datasetID = datasetID;
    const objectRole = String(value.objectRole || value.object_role || value.businessObjectRole || value.business_object_role || '').trim();
    if (objectRole) datasrv.objectRole = objectRole;
    const blueprintID = String(value.blueprintID || value.blueprint_id || '').trim();
    if (blueprintID) datasrv.blueprintID = blueprintID;
    const templateID = String(value.templateID || value.template_id || '').trim();
    if (templateID) datasrv.templateID = templateID;
    const preferredAction = String(value.preferredAction || value.preferred_action || '').trim();
    if (preferredAction) datasrv.preferredAction = preferredAction;
    const preferredView = String(value.preferredView || value.preferred_view || '').trim();
    if (preferredView) datasrv.preferredView = preferredView;
    const preferredReport = String(value.preferredReport || value.preferred_report || '').trim();
    if (preferredReport) datasrv.preferredReport = preferredReport;
    const preferredDashboard = String(value.preferredDashboard || value.preferred_dashboard || '').trim();
    if (preferredDashboard) datasrv.preferredDashboard = preferredDashboard;
    return datasrv;
}
function normalizeAppWorkspaceLayout(raw: unknown, kind: AppKind): AppWorkspaceLayout {
    const value = raw && typeof raw === 'object' ? raw as Partial<AppWorkspaceLayout> : undefined;
    return value?.schema === 'maclaw.app.ui.v1' ? { ...value, schema: 'maclaw.app.ui.v1' } : defaultWorkspaceLayoutForKind(kind);
}

function appSkillDependencies(app: AppEntry): AppSkillDependency[] {
    const deps = app.manifest?.dependencies?.skills || [];
    const appSkill = app.manifest?.appSkill?.id ? [{ id: app.manifest.appSkill.id, version: app.manifest.appSkill.version, kind: 'app_skill' as const, required: true, source: (app.manifest.appSkill.source || 'local') as AppSkillDependency['source'] }] : [];
    const boundSkill = app.manifest?.skill?.id ? [{ id: app.manifest.skill.id, kind: 'runtime_skill' as const, required: true, source: 'hub' as const }] : [];
    const approvalWorkflowSkills = (app.manifest?.mis?.approvalBindings || []).map((binding) => ({
        id: binding.workflowSkillId,
        version: binding.workflowVersion,
        kind: 'workflow_skill' as const,
        required: true,
        source: 'hub' as const,
        capabilities: ['approval.workflow'],
    })).filter((dep) => dep.id);
    const merged = new Map<string, AppSkillDependency>();
    [...appSkill, ...boundSkill, ...deps, ...approvalWorkflowSkills].forEach((dep) => {
        const id = String(dep.id || '').trim();
        if (!id) return;
        const existing = merged.get(id);
        const depVersion = 'version' in dep ? dep.version : undefined;
        const depCapabilities = 'capabilities' in dep && Array.isArray(dep.capabilities) ? dep.capabilities : [];
        if (!existing) {
            merged.set(id, { ...dep, id });
            return;
        }
        merged.set(id, {
            ...existing,
            version: existing.version || depVersion,
            kind: existing.kind || dep.kind,
            required: existing.required !== false || dep.required !== false,
            source: existing.source || dep.source,
            install_ref: appSkillDependencyInstallRef(existing) || appSkillDependencyInstallRef(dep as AppSkillDependency) || undefined,
            capabilities: Array.from(new Set([...(existing.capabilities || []), ...depCapabilities])),
        });
    });
    return Array.from(merged.values());
}

/**
 * Extract stable package id from enterprise hub version keys / dual-upload
 * submission ids (mirrors corelib.ExtractSkillPackageIDFromHubRef).
 */
function extractSkillPackageIDFromHubRef(ref: string): string {
    const raw = String(ref || '').trim();
    if (!raw) return '';
    const lower = raw.toLowerCase();
    const marker = ':skill:';
    const idx = lower.lastIndexOf(marker);
    if (idx < 0) return '';
    let rest = raw.slice(idx + marker.length);
    const at = rest.indexOf('@');
    if (at >= 0) rest = rest.slice(0, at);
    const semi = rest.indexOf(';');
    if (semi >= 0) rest = rest.slice(0, semi);
    rest = rest.trim();
    // Reject empty / submission-shaped segments; keep ordinary "sub-*" package names.
    if (!rest || isUploadSubmissionSkillRef(rest)) return '';
    return rest;
}

/** Mirrors corelib.IsUploadSubmissionSkillRef — do not treat ordinary "sub-*" package names as submissions. */
function isUploadSubmissionSkillRef(ref: string): boolean {
    const lower = String(ref || '').trim().toLowerCase();
    if (!lower) return false;
    if (lower.includes(';enterprise_hub=') || lower.startsWith('enterprise_hub=')) return true;
    // Lifecycle ids: sub-<digits>… (not sub-process-monitor style package names).
    if (lower.startsWith('sub-') && lower.length > 4) {
        const c = lower.charCodeAt(4);
        if (c >= 48 && c <= 57) return true; // '0'..'9'
    }
    return false;
}

/** Prefer package id over upload submission ids (mirrors corelib.StableSkillIdentityFromRef). */
function preferRunnableSkillRef(...candidates: string[]): string {
    for (const raw of candidates) {
        const trimmed = String(raw || '').trim();
        if (!trimmed) continue;
        const pkg = extractSkillPackageIDFromHubRef(trimmed);
        if (pkg) return pkg;
        if (!isUploadSubmissionSkillRef(trimmed)) return trimmed;
    }
    return '';
}

/** Comparison keys for a skill identity (strip @version + optional package extract). */
function skillIdentityMatchKeys(raw: string): string[] {
    const stripAt = (value: string) => {
        const trimmed = String(value || '').trim();
        const at = trimmed.indexOf('@');
        return (at > 0 ? trimmed.slice(0, at) : trimmed).trim().toLowerCase();
    };
    const identityKey = (value: string) => stripAt(value).replace(/[\s\-_]+/g, '_').replace(/^_+|_+$/g, '');
    const keys = new Set<string>();
    const add = (v: string) => {
        const s = stripAt(v);
        if (!s) return;
        keys.add(s);
        keys.add(identityKey(s));
    };
    add(raw);
    const pkg = extractSkillPackageIDFromHubRef(raw);
    if (pkg) add(pkg);
    return Array.from(keys);
}

function skillIdentityKeysOverlap(a: string, b: string): boolean {
    const ak = skillIdentityMatchKeys(a);
    if (!ak.length) return false;
    const set = new Set(ak);
    return skillIdentityMatchKeys(b).some((k) => set.has(k));
}

/**
 * Resolve the skill id used for RunNLSkillAsync.
 *
 * Coordinate model (authoring → plan → run):
 * 1. Declared appSkill.id / skill.id (stable hub/package id preferred at authoring)
 * 2. After PlanMaclawAppInstall: dependency.runtime_skill_ref (HubSkillID) or
 *    installed_name when the local registry display name is the only handle
 * 3. Fall back to declared id so cold start without a plan still works once
 *    backend MatchesName accepts HubSkillID
 *
 * Upload submission ids must never be returned as the run handle.
 */
function appRunnableSkillID(app?: AppEntry | null, plan?: BackendAppInstallPlan | null): string {
    const declaredRaw = String(app?.manifest?.appSkill?.id || app?.manifest?.skill?.id || '').trim();
    const declared = preferRunnableSkillRef(declaredRaw);
    if (!declared) return '';
    const deps = plan?.dependencies || [];
    if (!deps.length) return declared;
    const matchesDeclared = (dep: BackendAppInstallDependency) => {
        const candidates = [
            dep.id,
            dep.canonical_id,
            dep.canonicalID,
            dep.install_ref_target,
            dep.runtime_skill_ref,
            dep.runtimeSkillRef,
            dep.RuntimeSkillRef,
            dep.installed_name,
            ...(Array.isArray(dep.aliases) ? dep.aliases : []),
        ];
        return candidates.some((raw) => {
            const v = String(raw || '').trim();
            return !!v && (skillIdentityKeysOverlap(v, declared) || skillIdentityKeysOverlap(v, declaredRaw));
        });
    };
    // Prefer installed deps so a ready plan binds runtime_skill_ref.
    const dep = deps.find((d) => matchesDeclared(d) && d.installed) || deps.find((d) => matchesDeclared(d));
    if (!dep) return declared;
    return preferRunnableSkillRef(
        String(dep.runtime_skill_ref || dep.runtimeSkillRef || dep.RuntimeSkillRef || '').trim(),
        String(dep.canonical_id || dep.canonicalID || '').trim(),
        String(dep.installed_name || '').trim(),
        declared,
    );
}

function makeAutomationManifest(): AppManifestBinding {
    return {
        schema: 'maclaw.app.v1',
        installUnit: 'enterprise_app_pack',
        privateMarker: 'x_maclaw_apps',
        entryKind: 'automation_app',
        launchMode: 'automation_console',
    };
}

function readLayoutState(): AppLayoutState {
    if (typeof window === 'undefined') return {};
    try {
        const parsed = JSON.parse(window.localStorage.getItem(storageKey) || '{}') as AppLayoutState;
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch {
        return {};
    }
}

function applyLayoutState(apps: AppEntry[], layout: AppLayoutState): AppEntry[] {
    const pinned = new Set((layout.pinnedIds || []).slice(0, maxPinnedApps));
    const hidden = new Set(layout.hiddenIds || []);
    const disabled = new Set(layout.disabledIds || []);
    const disabledReasonsById = layout.disabledReasonsById || {};
    const disabledSourcesById = layout.disabledSourcesById || {};
    const recentUsedAtById = layout.recentUsedAtById || {};
    const editedApps = (layout.editedApps || []).map((app) => normalizeStoredAppEntry(app)).filter((app): app is AppEntry => !!app);
    const customApps = (layout.customApps || []).map((app) => normalizeStoredAppEntry(app, true)).filter((app): app is AppEntry => !!app);
    const editedById = new Map(editedApps.map((app) => [app.id, app]));
    const baseApps = apps.map((app) => ({ ...app, ...editedById.get(app.id), id: app.id }));
    const byId = new Map([...baseApps, ...customApps].filter((app) => !hidden.has(app.id)).map((app) => [app.id, {
        ...app,
        pinned: layout.pinnedIds ? pinned.has(app.id) : app.pinned,
        recentUsedAt: recentUsedAtById[app.id] || app.recentUsedAt,
        disabled: disabled.has(app.id) || app.disabled,
        disabledReason: disabledReasonsById[app.id] || app.disabledReason,
        disabledSource: disabledSourcesById[app.id] || app.disabledSource,
    }]));
    const ordered: AppEntry[] = [];
    for (const id of layout.orderedIds || []) {
        const app = byId.get(id);
        if (!app) continue;
        ordered.push(app);
        byId.delete(id);
    }
    return [...ordered, ...byId.values()];
}

function normalizeStoredAppEntry(app: Partial<AppEntry> | undefined, custom = false): AppEntry | null {
    if (!app?.id || !app?.name) return null;
    const kind = normalizeAppKind(app.kind);
    const source = app.source === 'builtin' || app.source === 'skill' || app.source === 'datasrv' || app.source === 'market' || app.source === 'local'
        ? app.source
        : undefined;
    const migratedSource = custom && (source === undefined || (source === 'market' && String(app.id).startsWith('local-app-'))) ? 'local' : source || 'local';
    const installEvidence = (app as any).installEvidence && typeof (app as any).installEvidence === 'object'
        ? (app as any).installEvidence as BackendAppInstallRecord
        : undefined;
    return {
        ...app,
        id: String(app.id),
        name: String(app.name),
        description: String(app.description || ''),
        category: String(app.category || '\u672a\u5206\u7c7b'),
        kind,
        icon: normalizeSkillAppIcon(app.icon),
        customIconDataUrl: normalizeCustomIconDataUrl((app as any).customIconDataUrl),
        accent: String(app.accent || defaultAccentForKind(kind)),
        version: normalizeAppVersion(app.version),
        source: migratedSource,
        importedRunEvidence: normalizeImportedRunEvidence((app as any).importedRunEvidence),
        versionSnapshot: normalizeVersionSnapshot((app as any).versionSnapshot),
        installEvidence,
        workflowContract: normalizeAppWorkflowContract((app as any).workflowContract, kind),
    };
}

function normalizeAppVersion(value: unknown) {
    const version = Number(value);
    return Number.isFinite(version) && version > 0 ? Math.floor(version) : 1;
}

function nextAppVersion(app: AppEntry) {
    return normalizeAppVersion(app.version) + 1;
}

function isEditedInitialApp(app: AppEntry) {
    const base = initialApps.find((item) => item.id === app.id);
    if (!base) return false;
    return base.name !== app.name ||
        base.description !== app.description ||
        base.category !== app.category ||
        base.kind !== app.kind ||
        base.icon !== app.icon ||
        base.customIconDataUrl !== app.customIconDataUrl ||
        base.accent !== app.accent ||
        normalizeAppVersion(base.version) !== normalizeAppVersion(app.version) ||
        base.source !== app.source ||
        JSON.stringify(base.manifest || null) !== JSON.stringify(app.manifest || null);
}

function persistLayoutState(apps: AppEntry[]) {
    if (typeof window === 'undefined') return;
    const visibleIds = new Set(apps.map((app) => app.id));
    const layout: AppLayoutState = {
        orderedIds: apps.map((app) => app.id),
        pinnedIds: apps.filter((app) => app.pinned).slice(0, maxPinnedApps).map((app) => app.id),
        hiddenIds: initialApps.filter((app) => !visibleIds.has(app.id)).map((app) => app.id),
        editedApps: apps.filter(isEditedInitialApp),
        customApps: apps.filter((app) => !initialApps.some((base) => base.id === app.id)),
        recentUsedAtById: Object.fromEntries(apps.filter((app) => app.recentUsedAt).map((app) => [app.id, app.recentUsedAt as string])),
        disabledIds: apps.filter((app) => app.disabled).map((app) => app.id),
        disabledReasonsById: Object.fromEntries(apps.filter((app) => app.disabled && app.disabledReason).map((app) => [app.id, app.disabledReason as string])),
        disabledSourcesById: Object.fromEntries(apps.filter((app) => app.disabled && app.disabledSource).map((app) => [app.id, app.disabledSource as 'local' | 'hub_governance'])),
    };
    window.localStorage.setItem(storageKey, JSON.stringify(layout));
}

const emptyDiscovery: DataSrvDiscovery = {
    status: 'idle',
    candidates: [],
    domains: 0,
    actions: 0,
    views: 0,
    reports: 0,
    dashboards: 0,
};

const emptySkillDiscovery: SkillAppDiscovery = {
    status: 'idle',
    candidates: [],
};

async function discoverDataSrvCapabilities(): Promise<DataSrvDiscovery> {
    const config = await Promise.resolve().then(() => GetMISDataConfig()) as MISDataConfig;
    const endpoint = String(config?.endpoint || 'http://127.0.0.1:18180').replace(/\/+$/, '');
    if (!config?.enabled) {
        return { ...emptyDiscovery, status: 'disabled', endpoint };
    }
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (config.token) headers.Authorization = `Bearer ${config.token}`;
    const response = await fetch(`${endpoint}/api/v1/data/capabilities`, { headers });
    if (!response.ok) {
        throw new Error(`GET /api/v1/data/capabilities ${response.status}`);
    }
    const caps = await response.json();
    const candidates = buildDataSrvAppCandidates(caps);
    return {
        status: 'ready',
        endpoint,
        service: String(caps?.service || 'MaClawDataSrv'),
        candidates,
        domains: Array.isArray(caps?.domains) ? caps.domains.length : 0,
        actions: Array.isArray(caps?.business_actions) ? caps.business_actions.length : 0,
        views: Array.isArray(caps?.business_views) ? caps.business_views.length : 0,
        reports: Array.isArray(caps?.reports) ? caps.reports.length : 0,
        dashboards: Array.isArray(caps?.dashboards) ? caps.dashboards.length : 0,
    };
}

type DataSrvCapabilityItem = {
    id?: string;
    domain?: string;
    title?: string;
    description?: string;
};

type DataSrvAppInstallationItem = {
    app_id?: string;
    appId?: string;
    blueprint_id?: string;
    blueprintID?: string;
    name?: string;
    version?: string | number;
    kind?: string;
    source?: string;
    updated_at?: string;
    updatedAt?: string;
    role_bindings?: Array<Record<string, any>>;
    roleBindings?: Array<Record<string, any>>;
    metadata?: Record<string, any>;
};

type DataSrvApprovalSummaryBucket = {
    key: string;
    label: string;
    query: Record<string, string>;
    count: number;
    apps: string[];
    items: DataSrvApprovalSummaryItem[];
};

type DataSrvApprovalSummaryItem = {
    appID: string;
    name: string;
    datasetID: string;
    objectRole: string;
    approvalID: string;
    workflowInstanceID: string;
    recordID: string;
    detailURL: string;
    status: string;
    decision: string;
    currentNode: string;
    resultTypes: string[];
    updatedAt: string;
};

type DataSrvApprovalSummaryState = {
    status: 'disabled' | 'loading' | 'ready' | 'error';
    endpoint?: string;
    error?: string;
    buckets: DataSrvApprovalSummaryBucket[];
};

function buildDataSrvAppCandidates(caps: any): AppEntry[] {
    const domains = Array.isArray(caps?.domains) ? caps.domains.filter((domain: any) => typeof domain === 'string' && domain.trim()) : [];
    const actions = Array.isArray(caps?.business_actions) ? caps.business_actions as DataSrvCapabilityItem[] : [];
    const views = Array.isArray(caps?.business_views) ? caps.business_views as DataSrvCapabilityItem[] : [];
    const reports = Array.isArray(caps?.reports) ? caps.reports as DataSrvCapabilityItem[] : [];
    const dashboards = Array.isArray(caps?.dashboards) ? caps.dashboards as DataSrvCapabilityItem[] : [];
    const installedApps = Array.isArray(caps?.app_installations) ? caps.app_installations as DataSrvAppInstallationItem[] : Array.isArray(caps?.appInstallations) ? caps.appInstallations as DataSrvAppInstallationItem[] : [];
    const installedCandidates = installedApps.map((item) => dataSrvInstalledAppCandidate(item)).filter((item): item is AppEntry => !!item);
    const installedDomains = new Set(installedCandidates.map((app) => app.manifest?.datasrv?.domain).filter(Boolean));
    const domainCandidates = domains.slice(0, 12).filter((domain: string) => !installedDomains.has(domain)).map((domain: string) => {
        const action = actions.find((item) => item.domain === domain);
        const view = views.find((item) => item.domain === domain);
        const report = reports.find((item) => item.domain === domain);
        const dashboard = dashboards.find((item) => item.domain === domain);
        const title = domainTitle(domain);
        return {
            id: `datasrv-domain-${domain}`,
            name: title,
            description: action?.description || view?.description || dashboard?.description || `${title} DataSrv dynamic application.`,
            category: domainCategory(domain),
            kind: 'enterprise_normal_app',
            icon: domainIcon(domain),
            accent: domainAccent(domain),
            source: 'datasrv',
            manifest: makeDataSrvManifest(domain, action?.id || '', view?.id || '', report?.id || '', dashboard?.id || ''),
        };
    });
    return [...installedCandidates, ...domainCandidates];
}

function dataSrvInstalledAppCandidate(item: DataSrvAppInstallationItem): AppEntry | null {
    const appID = String(item?.app_id || item?.appId || '').trim();
    const name = String(item?.name || appID).trim();
    if (!appID || !name) return null;
    const metadata = item.metadata && typeof item.metadata === 'object' ? item.metadata : {};
    const roleBindings = Array.isArray(item.role_bindings) ? item.role_bindings : Array.isArray(item.roleBindings) ? item.roleBindings : [];
    const primaryBinding = roleBindings[0] || {};
    const domain = String(primaryBinding.domain || metadata.domain || '').trim() || appID.split('.')[0] || 'business';
    const kind = normalizeAppKind(item.kind || metadata.kind || 'enterprise_normal_app');
    const workflowIDs = Array.isArray(metadata.workflow_skill_ids) ? metadata.workflow_skill_ids.map((id: unknown) => String(id || '').trim()).filter(Boolean) : [];
    const dependencies = normalizeAppDependencies({ skills: metadata.dependencies })?.skills || [];
    const appSkillID = String(metadata.app_skill_id || '').trim();
    const appSkillVersion = String(metadata.app_skill_version || '').trim();
    const versionSnapshot = dataSrvInstalledVersionSnapshot(metadata, item, workflowIDs, primaryBinding);
    const workflowContract = dataSrvInstalledWorkflowContract(metadata, kind);
    const resultContract = dataSrvInstalledResultContract(metadata, kind);
    const workflowMapping = dataSrvInstalledWorkflowMapping(metadata, kind, domain, String(primaryBinding.object_role || primaryBinding.objectRole || '').trim() || domain);
    const workflowVersionByID = new Map((versionSnapshot?.workflow_skills || []).map((skill) => [skill.id || '', skill.version || '']));
    const workflowDependencies: AppSkillDependency[] = workflowIDs.map((id) => ({ id, version: workflowVersionByID.get(id) || undefined, kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }));
    const mergedDependencies = mergeDataSrvInstalledDependencies(dependencies, workflowDependencies);
    const importedRunEvidence = dataSrvInstalledRunEvidence(metadata, appID);
    const installEvidence = dataSrvInstalledInstallEvidence(metadata, item, versionSnapshot, workflowMapping, workflowContract, resultContract);
    return {
        id: `datasrv-installed-${appID}`,
        name,
        description: String(metadata.description || `${name} DataSrv installed application.`),
        category: domainCategory(domain),
        kind,
        icon: domainIcon(domain),
        accent: domainAccent(domain),
        source: 'datasrv',
        version: normalizeAppVersion(item.version),
        importedRunEvidence,
        versionSnapshot,
        installEvidence,
        workflowContract,
        manifest: {
            schema: 'maclaw.app.v1',
            installUnit: 'enterprise_app_pack',
            privateMarker: 'x_maclaw_apps',
            entryKind: kind,
            launchMode: defaultLaunchModeForKind(kind),
            datasrv: {
                appID,
                domain,
                datasetID: String(primaryBinding.dataset_id || primaryBinding.datasetID || '').trim() || undefined,
                objectRole: String(primaryBinding.object_role || primaryBinding.objectRole || '').trim() || undefined,
                blueprintID: String(item.blueprint_id || item.blueprintID || '').trim() || undefined,
                templateID: String(primaryBinding.template_id || primaryBinding.templateID || '').trim() || undefined,
            },
            appSkill: appSkillID ? { id: appSkillID, version: appSkillVersion || undefined, source: 'hub' } : undefined,
            dependencies: mergedDependencies.length ? { skills: mergedDependencies } : undefined,
            mis: kind === 'enterprise_approval_app' && workflowIDs.length > 0 ? { approvalBindings: [dataSrvInstalledApprovalBinding(domain, workflowIDs[0], primaryBinding, versionSnapshot)] } : undefined,
            ui: dataSrvInstalledWorkspaceLayout(metadata, kind),
            resultContract,
            testProtocol: dataSrvInstalledTestProtocol(metadata, kind),
            workflow: workflowMapping,
        },
    };
}

async function fetchDataSrvApprovalAppSummary(text: typeof labels.zh): Promise<DataSrvApprovalSummaryState> {
    const config = await Promise.resolve().then(() => GetMISDataConfig()) as MISDataConfig;
    const endpoint = String(config?.endpoint || 'http://127.0.0.1:18180').replace(/\/+$/, '');
    if (!config?.enabled) return { status: 'disabled', endpoint, buckets: [] };
    const currentUser = String(config.user_id || 'current_user').trim() || 'current_user';
    const definitions: Array<{ key: string; label: string; query: Record<string, string> }> = [
        { key: 'all', label: text.allApprovalInstances, query: {} },
        { key: 'my_requests', label: text.myRequests, query: { applicant_id: currentUser } },
        { key: 'pending_my_approval', label: text.pendingMyApproval, query: { approver_id: currentUser } },
        { key: 'approved', label: text.approvedApprovals, query: { approval_status: 'approved' } },
        { key: 'rejected', label: text.rejectedApprovals, query: { approval_status: 'rejected' } },
        { key: 'attention', label: text.attentionApprovals, query: { approval_status: 'attention' } },
        { key: 'document', label: text.documentOutputs, query: { result_type: 'document' } },
        { key: 'inline_content', label: text.inlineContentOutputs, query: { result_type: 'inline_content' } },
    ];
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (config.token) headers.Authorization = `Bearer ${config.token}`;
    const buckets = await Promise.all(definitions.map(async (definition) => {
        const params = new URLSearchParams({ kind: 'enterprise_approval_app', limit: '20', ...definition.query });
        const response = await fetch(`${endpoint}/api/v1/data/app-installations?${params.toString()}`, { headers });
        if (!response.ok) throw new Error(`GET /api/v1/data/app-installations ${response.status}`);
        const payload = await response.json();
        const items = Array.isArray(payload?.items) ? payload.items as DataSrvAppInstallationItem[] : Array.isArray(payload) ? payload as DataSrvAppInstallationItem[] : [];
        const summaryItems = items.map(dataSrvApprovalSummaryItem).filter((item): item is DataSrvApprovalSummaryItem => !!item);
        return {
            ...definition,
            count: summaryItems.length,
            apps: summaryItems.slice(0, 3).map((item) => item.name).filter(Boolean),
            items: summaryItems,
        };
    }));
    return { status: 'ready', endpoint, buckets };
}
function dataSrvRegistrationFromInstallation(item: DataSrvAppInstallationItem): BackendAppDataSrvRegistration | undefined {
    const metadata = item.metadata && typeof item.metadata === 'object' ? item.metadata as Record<string, any> : {};
    const installEvidence = appEvidenceRecord(metadata.install_evidence) || appEvidenceRecord(metadata.installEvidence) || {};
    const registration = appEvidenceRecord(metadata.datasrv_registration)
        || appEvidenceRecord(metadata.dataSrvRegistration)
        || appEvidenceRecord(installEvidence.datasrv_registration)
        || appEvidenceRecord(installEvidence.dataSrvRegistration);
    return registration as BackendAppDataSrvRegistration | undefined;
}

async function fetchDataSrvInstallRegistrationAudit(record: BackendAppInstallRecord, text: typeof labels.zh): Promise<{ message: string; state: 'ready' | 'partial' | 'failed' | 'skipped' }> {
    const config = await Promise.resolve().then(() => GetMISDataConfig()) as MISDataConfig;
    const endpoint = String(config?.endpoint || 'http://127.0.0.1:18180').replace(/\/+$/, '');
    if (!config?.enabled) throw new Error(text.datasrvRegistrationSkipped);
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (config.token) headers.Authorization = `Bearer ${config.token}`;
    const appIDs = Array.from(new Set((record.apps || []).flatMap((app) => appInstallIdentityKeys(String(app.id || ''))).filter(Boolean)));
    if (appIDs.length === 0) throw new Error(text.dataSrvRegistrationAuditMissing);
    const responses = await Promise.all(appIDs.map(async (appID) => {
        const params = new URLSearchParams({ app_id: appID, limit: '10' });
        const response = await fetch(`${endpoint}/api/v1/data/app-installations?${params.toString()}`, { headers });
        if (!response.ok) throw new Error(`GET /api/v1/data/app-installations ${response.status}`);
        const payload = await response.json();
        return Array.isArray(payload?.items) ? payload.items as DataSrvAppInstallationItem[] : Array.isArray(payload) ? payload as DataSrvAppInstallationItem[] : [];
    }));
    const seenItems = new Set<string>();
    const items = responses.flat().filter((item, index) => {
        const key = String(item.app_id || item.appId || item.blueprint_id || item.blueprintID || index).trim() || String(index);
        if (seenItems.has(key)) return false;
        seenItems.add(key);
        return true;
    });
    if (items.length === 0) return { state: 'failed', message: `${text.dataSrvRegistrationAuditMissing}: ${appIDs.slice(0, 3).join(', ')}` };
    const states = items.map(dataSrvRegistrationFromInstallation).map(dataSrvRegistrationState).filter((item): item is NonNullable<ReturnType<typeof dataSrvRegistrationState>> => !!item);
    const ready = states.filter((item) => item.state === 'ready').length;
    const partial = states.filter((item) => item.state === 'partial').length;
    const failed = states.filter((item) => item.state === 'failed').length;
    const skipped = states.filter((item) => item.state === 'skipped').length + Math.max(0, items.length - states.length);
    const state = failed > 0 ? 'failed' : partial > 0 ? 'partial' : ready > 0 ? 'ready' : 'skipped';
    const parts = [
        `${text.dataSrvRegistrationAuditFound}: ${items.length}`,
        `${text.datasrvRegistrationReady}: ${ready}`,
        `${text.datasrvRegistrationPartial}: ${partial}`,
        `${text.datasrvRegistrationFailed}: ${failed}`,
    ];
    if (skipped > 0) parts.push(`${text.datasrvRegistrationSkipped}: ${skipped}`);
    return { state, message: parts.join(' · ') };
}


function dataSrvApprovalSummaryItem(item: DataSrvAppInstallationItem): DataSrvApprovalSummaryItem | null {
    const metadata = item.metadata && typeof item.metadata === 'object' ? item.metadata : {};
    const roleBindings = Array.isArray(item.role_bindings) ? item.role_bindings : Array.isArray(item.roleBindings) ? item.roleBindings : [];
    const primaryBinding = roleBindings[0] || {};
    const evidence = appEvidenceRecord(metadata.test_evidence) || {};
    const approval = appEvidenceRecord(evidence.approval_instance)
        || appEvidenceRecord(evidence.approvalInstance)
        || appEvidenceRecord(metadata.test_evidence_approval_instance)
        || appEvidenceRecord(metadata.approval_instance)
        || {};
    const resultPayload = appEvidenceRecord(evidence.result_payload)
        || appEvidenceRecord(evidence.resultPayload)
        || appEvidenceRecord(metadata.test_evidence_result_payload)
        || {};
    const appID = appEvidenceString(item.app_id, item.appId);
    const name = appEvidenceString(item.name, appID);
    if (!appID && !name) return null;
    const status = appEvidenceString(
        approval.status,
        approval.approval_status,
        metadata.test_evidence_approval_status,
        metadata.approval_status,
        resultPayload.result_status,
    );
    const decision = appEvidenceString(
        approval.decision,
        approval.approval_decision,
        resultPayload.decision,
        resultPayload.approval_result,
        metadata.approval_decision,
    );
    const currentNode = appEvidenceString(
        approval.current_node,
        approval.currentNode,
        approval.workflow_node,
        approval.workflowNode,
        metadata.workflow_node,
        metadata.workflow_result_node,
    );
    const datasetID = appEvidenceString(
        approval.dataset_id,
        approval.datasetID,
        resultPayload.dataset_id,
        resultPayload.datasetID,
        metadata.dataset_id,
        metadata.datasetID,
        primaryBinding.dataset_id,
        primaryBinding.datasetID,
    );
    const objectRole = appEvidenceString(
        approval.object_role,
        approval.objectRole,
        approval.approval_object_role,
        approval.approvalObjectRole,
        resultPayload.object_role,
        resultPayload.objectRole,
        metadata.object_role,
        metadata.objectRole,
        primaryBinding.object_role,
        primaryBinding.objectRole,
    );
    const approvalID = appEvidenceString(
        approval.approval_id,
        approval.approvalID,
        approval.record_approval_id,
        approval.recordApprovalID,
        resultPayload.approval_id,
        resultPayload.approvalID,
        metadata.approval_id,
        metadata.approvalID,
        metadata.record_approval_id,
        metadata.recordApprovalID,
    );
    const workflowInstanceID = appEvidenceString(
        approval.workflow_instance_id,
        approval.workflowInstanceID,
        approval.workflowInstanceId,
        resultPayload.workflow_instance_id,
        resultPayload.workflowInstanceID,
        resultPayload.workflowInstanceId,
        metadata.workflow_instance_id,
        metadata.workflowInstanceID,
        metadata.workflowInstanceId,
        metadata.test_evidence_workflow_instance_id,
    );
    const recordID = appEvidenceString(
        approval.record_id,
        approval.recordID,
        approval.business_record_id,
        approval.businessRecordID,
        resultPayload.record_id,
        resultPayload.recordID,
        resultPayload.business_record_id,
        resultPayload.businessRecordID,
        metadata.record_id,
        metadata.recordID,
        metadata.business_record_id,
        metadata.businessRecordID,
        metadata.test_evidence_record_id,
    );
    const detailURL = appEvidenceString(
        approval.detail_url,
        approval.detailURL,
        resultPayload.detail_url,
        resultPayload.detailURL,
        metadata.detail_url,
        metadata.detailURL,
    );
    const evidenceOutputs = normalizeApprovalOutputs(Array.isArray(evidence.outputs) ? evidence.outputs as Array<SkillRunOutputBlockView | ApprovalInstanceOutputView | null | undefined> : undefined) || [];
    const resultTypes = Array.from(new Set([
        ...parseStringList(metadata.result_contract_types),
        ...parseStringList(metadata.test_evidence_covered_types),
        ...parseStringList(metadata.test_evidence_output_kinds),
        ...parseStringList(metadata.test_evidence_output_types),
        ...parseStringList(metadata.test_evidence_artifact_types),
        appEvidenceString(metadata.result_contract_primary),
        appEvidenceString(metadata.test_evidence_result_type),
        appEvidenceString(metadata.test_evidence_output_type),
        appEvidenceString(metadata.test_evidence_artifact_type),
        appEvidenceString(evidence.primary_result, evidence.primaryResult, metadata.test_evidence_primary_result),
        ...evidenceOutputs.map(approvalOutputKind),
    ].map((value) => String(value || '').trim()).filter(Boolean)));
    return {
        appID,
        name: name || appID,
        datasetID,
        objectRole,
        approvalID,
        workflowInstanceID,
        recordID,
        detailURL,
        status,
        decision,
        currentNode,
        resultTypes,
        updatedAt: appEvidenceString(item.updated_at, item.updatedAt, metadata.updated_at, evidence.verified_at, evidence.verifiedAt),
    };
}

function dataSrvApprovalDetailURL(endpoint: string | undefined, item: DataSrvApprovalSummaryItem) {
    const base = String(endpoint || '').replace(/\/+$/, '');
    if (item.detailURL) return item.detailURL;
    if (!base) return '';
    if (item.approvalID) return `${base}/api/v1/data/approvals/${encodeURIComponent(item.approvalID)}`;
    const params = new URLSearchParams({ limit: '20' });
    if (item.datasetID) params.set('dataset_id', item.datasetID);
    if (item.recordID) params.set('record_id', item.recordID);
    if (item.workflowInstanceID) params.set('workflow_instance_id', item.workflowInstanceID);
    if (item.appID) params.set('app_id', item.appID);
    return `${base}/api/v1/data/approvals?${params.toString()}`;
}

function dataSrvBusinessRecordURL(endpoint: string | undefined, item: DataSrvApprovalSummaryItem) {
    const base = String(endpoint || '').replace(/\/+$/, '');
    if (!base || !item.datasetID || !item.recordID) return '';
    return `${base}/api/v1/data/datasets/${encodeURIComponent(item.datasetID)}/records/${encodeURIComponent(item.recordID)}`;
}

function mergeDataSrvInstalledDependencies(base: AppSkillDependency[], recovered: AppSkillDependency[]) {
    const byID = new Map<string, AppSkillDependency>();
    [...base, ...recovered].forEach((dep) => {
        const id = String(dep.id || '').trim();
        if (!id) return;
        const existing = byID.get(id);
        const depVersion = 'version' in dep ? dep.version : undefined;
        const depCapabilities = 'capabilities' in dep && Array.isArray(dep.capabilities) ? dep.capabilities : [];
        if (!existing) {
            byID.set(id, { ...dep, id });
            return;
        }
        byID.set(id, {
            ...existing,
            ...dep,
            version: existing.version || depVersion,
            kind: existing.kind || dep.kind,
            source: existing.source || dep.source,
            required: existing.required !== false && dep.required !== false,
            install_ref: appSkillDependencyInstallRef(existing) || appSkillDependencyInstallRef(dep) || undefined,
            capabilities: Array.from(new Set([...(existing.capabilities || []), ...depCapabilities])),
        });
    });
    return Array.from(byID.values());
}

function normalizeVersionSnapshotSkill(raw: unknown, fallback?: Partial<BackendAppInstallSkillVersionSnapshot>): BackendAppInstallSkillVersionSnapshot | undefined {
    const value = raw && typeof raw === 'object' ? raw as Record<string, unknown> : {};
    const id = String(value.id || fallback?.id || '').trim();
    if (!id) return undefined;
    const version = String(value.version || fallback?.version || '').trim();
    const kind = String(value.kind || fallback?.kind || '').trim();
    const source = String(value.source || fallback?.source || '').trim();
    return { id, version: version || undefined, kind: kind || undefined, source: source || undefined };
}

function normalizeVersionSnapshotBinding(raw: unknown, fallback?: Partial<BackendAppInstallApprovalBindingSnapshot>): BackendAppInstallApprovalBindingSnapshot | undefined {
    const value = raw && typeof raw === 'object' ? raw as Record<string, unknown> : {};
    const workflowSkillID = String(value.workflow_skill_id || value.workflowSkillId || fallback?.workflow_skill_id || '').trim();
    if (!workflowSkillID) return undefined;
    const event = String(value.event || fallback?.event || '').trim();
    const objectRole = String(value.object_role || value.objectRole || fallback?.object_role || '').trim();
    const workflowVersion = String(value.workflow_version || value.workflowVersion || fallback?.workflow_version || '').trim();
    return { event: event || undefined, object_role: objectRole || undefined, workflow_skill_id: workflowSkillID, workflow_version: workflowVersion || undefined };
}

function normalizeVersionSnapshot(raw: unknown): BackendAppInstallVersionSnapshot | undefined {
    const value = raw && typeof raw === 'object' ? raw as Record<string, unknown> : undefined;
    if (!value) return undefined;
    const snapshot: BackendAppInstallVersionSnapshot = {};
    const appEntryVersion = String(value.app_entry_version || value.appEntryVersion || '').trim();
    if (appEntryVersion) snapshot.app_entry_version = appEntryVersion;
    const appSkill = normalizeVersionSnapshotSkill(value.app_skill || value.appSkill);
    if (appSkill) snapshot.app_skill = appSkill;
    const workflowSkills = (Array.isArray(value.workflow_skills) ? value.workflow_skills : Array.isArray(value.workflowSkills) ? value.workflowSkills : [])
        .map((item) => normalizeVersionSnapshotSkill(item))
        .filter((item): item is BackendAppInstallSkillVersionSnapshot => !!item);
    if (workflowSkills.length) snapshot.workflow_skills = dedupeVersionSnapshotSkills(workflowSkills);
    const approvalBindings = (Array.isArray(value.approval_bindings) ? value.approval_bindings : Array.isArray(value.approvalBindings) ? value.approvalBindings : [])
        .map((item) => normalizeVersionSnapshotBinding(item))
        .filter((item): item is BackendAppInstallApprovalBindingSnapshot => !!item);
    if (approvalBindings.length) snapshot.approval_bindings = dedupeVersionSnapshotBindings(approvalBindings);
    return hasVersionSnapshotItems(snapshot) ? snapshot : undefined;
}

function hasVersionSnapshotItems(snapshot: BackendAppInstallVersionSnapshot | undefined) {
    return !!snapshot && !!(snapshot.app_entry_version || snapshot.app_skill?.id || snapshot.workflow_skills?.length || snapshot.approval_bindings?.length);
}

function parseVersionRef(value: string): { id: string; version?: string } | null {
    const trimmed = String(value || '').trim();
    if (!trimmed) return null;
    const at = trimmed.lastIndexOf('@');
    if (at <= 0) return { id: trimmed };
    const id = trimmed.slice(0, at).trim();
    const version = trimmed.slice(at + 1).replace(/^v/i, '').trim();
    return id ? { id, version: version || undefined } : null;
}

function parseApprovalBindingVersionRef(value: string, fallbackObjectRole: string): BackendAppInstallApprovalBindingSnapshot | undefined {
    const trimmed = String(value || '').trim();
    if (!trimmed) return undefined;
    const separator = trimmed.indexOf(':');
    const event = separator > 0 ? trimmed.slice(0, separator).trim() : '';
    const workflowRef = parseVersionRef(separator > 0 ? trimmed.slice(separator + 1) : trimmed);
    if (!workflowRef?.id) return undefined;
    return { event: event || undefined, object_role: fallbackObjectRole || undefined, workflow_skill_id: workflowRef.id, workflow_version: workflowRef.version };
}

function dedupeVersionSnapshotSkills(items: BackendAppInstallSkillVersionSnapshot[]) {
    const seen = new Set<string>();
    return items.filter((item) => {
        const key = [item.id, item.version || '', item.kind || '', item.source || ''].join('@');
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
    });
}

function dedupeVersionSnapshotBindings(items: BackendAppInstallApprovalBindingSnapshot[]) {
    const seen = new Set<string>();
    return items.filter((item) => {
        const key = [item.event || '', item.object_role || '', item.workflow_skill_id || '', item.workflow_version || ''].join('@');
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
    });
}

function normalizeAppWorkflowContract(value: unknown, kind: AppKind, fallback?: Partial<AppWorkflowContract>): AppWorkflowContract | undefined {
    if (kind !== 'enterprise_approval_app') return undefined;
    const raw = value && typeof value === 'object' ? value as Record<string, any> : {};
    const statusRaw = raw.statusMapping && typeof raw.statusMapping === 'object'
        ? raw.statusMapping as Record<string, unknown>
        : raw.status_mapping && typeof raw.status_mapping === 'object'
            ? raw.status_mapping as Record<string, unknown>
            : {};
    const workflowSkillId = String(raw.workflowSkillId || raw.workflow_skill_id || raw.workflowId || raw.workflow_id || fallback?.workflowSkillId || '').trim();
    const workflowVersion = String(raw.workflowVersion || raw.workflow_version || fallback?.workflowVersion || '').trim();
    const objectRole = String(raw.objectRole || raw.object_role || raw.businessObjectRole || raw.business_object_role || fallback?.objectRole || '').trim();
    if (!workflowSkillId || !objectRole) return undefined;
    const requiredInputs = parseStringList(raw.requiredInputs || raw.required_inputs || fallback?.requiredInputs);
    const decisionOutputs = parseStringList(raw.decisionOutputs || raw.decision_outputs || fallback?.decisionOutputs);
    const requiredOutputs = parseStringList(raw.requiredOutputs || raw.required_outputs || raw.outputContract || raw.output_contract || fallback?.requiredOutputs);
    const fallbackStatus: Partial<AppWorkflowContract['statusMapping']> = fallback?.statusMapping || {};
    const clean = (item: unknown, fallbackValue = '') => String(item || fallbackValue || '').trim();
    return {
        schema: 'maclaw.app.workflow_contract.v1',
        workflowSkillId,
        workflowVersion: workflowVersion || undefined,
        objectRole,
        requiredInputs: requiredInputs.length ? requiredInputs : ['record_ref', 'applicant', 'business_payload'],
        decisionOutputs: decisionOutputs.length ? decisionOutputs : ['approved', 'rejected', 'attention'],
        requiredOutputs: requiredOutputs.length ? requiredOutputs : ['workflow_result', 'approval_instance', 'outputs', 'artifacts'],
        statusMapping: {
            pending: clean(statusRaw.pending, fallbackStatus.pending || 'approval_pending'),
            approved: clean(statusRaw.approved, fallbackStatus.approved || 'approved'),
            rejected: clean(statusRaw.rejected, fallbackStatus.rejected || 'rejected'),
            attention: clean(statusRaw.attention, fallbackStatus.attention || 'attention'),
            requiresInput: clean(statusRaw.requiresInput ?? statusRaw.requires_input, fallbackStatus.requiresInput || 'requires_input'),
        },
    };
}

function dataSrvInstalledWorkflowContract(metadata: Record<string, any>, kind: AppKind): AppWorkflowContract | undefined {
    const rawGovernance = metadata.governance && typeof metadata.governance === 'object' ? metadata.governance as Record<string, any> : {};
    const raw = metadata.workflow_contract || metadata.workflowContract || rawGovernance.workflow_contract || rawGovernance.workflowContract;
    const statusMappingRaw = metadata.workflow_contract_status_mapping && typeof metadata.workflow_contract_status_mapping === 'object'
        ? metadata.workflow_contract_status_mapping as Record<string, unknown>
        : metadata.workflowContractStatusMapping && typeof metadata.workflowContractStatusMapping === 'object'
            ? metadata.workflowContractStatusMapping as Record<string, unknown>
            : undefined;
    const statusMapping = statusMappingRaw ? {
        pending: String(statusMappingRaw.pending || '').trim(),
        approved: String(statusMappingRaw.approved || '').trim(),
        rejected: String(statusMappingRaw.rejected || '').trim(),
        attention: String(statusMappingRaw.attention || '').trim(),
        requiresInput: String(statusMappingRaw.requiresInput || statusMappingRaw.requires_input || '').trim() || undefined,
    } : undefined;
    const fallback: Partial<AppWorkflowContract> = {
        workflowSkillId: String(metadata.workflow_contract_skill_id || '').trim(),
        workflowVersion: String(metadata.workflow_contract_version || '').trim() || undefined,
        objectRole: String(metadata.workflow_contract_object_role || '').trim(),
        requiredInputs: parseStringList(metadata.workflow_contract_required_inputs),
        decisionOutputs: parseStringList(metadata.workflow_contract_decision_outputs),
        statusMapping,
    };
    return normalizeAppWorkflowContract(raw, kind, fallback);
}

function dataSrvInstalledResultContract(metadata: Record<string, any>, kind: AppKind): AppResultContract {
    const rawGovernance = metadata.governance && typeof metadata.governance === 'object' ? metadata.governance as Record<string, any> : {};
    const raw = metadata.result_contract || metadata.resultContract || rawGovernance.result_contract || rawGovernance.resultContract;
    const deliveryModes = parseStringList(metadata.result_contract_delivery_modes);
    const deliverySummary = String(metadata.result_contract_delivery || '').trim();
    const deliveryModeSet = new Set([...deliveryModes, deliverySummary].map((item) => item.toLowerCase()));
    const delivery = {
        inlineContent: typeof metadata.result_contract_delivery_inline_content === 'boolean' ? metadata.result_contract_delivery_inline_content : deliveryModeSet.has('inline_content') || deliveryModeSet.has('content') || deliveryModeSet.has('text'),
        artifacts: typeof metadata.result_contract_delivery_artifacts === 'boolean' ? metadata.result_contract_delivery_artifacts : deliveryModeSet.has('artifact') || deliveryModeSet.has('artifacts') || deliveryModeSet.has('document') || deliveryModeSet.has('download'),
        businessRecord: typeof metadata.result_contract_delivery_business_record === 'boolean' ? metadata.result_contract_delivery_business_record : deliveryModeSet.has('business_record') || deliveryModeSet.has('businessrecord'),
        notifications: typeof metadata.result_contract_delivery_notifications === 'boolean' ? metadata.result_contract_delivery_notifications : deliveryModeSet.has('notification') || deliveryModeSet.has('notifications'),
    };
    const fallback = {
        schema: metadata.result_contract_schema || 'maclaw.app.result.v1',
        primary: metadata.result_contract_primary,
        types: metadata.result_contract_types,
        outputModes: metadata.result_contract_output_modes || metadata.result_contract_outputModes,
        approvalDecisions: metadata.result_contract_approval_decisions,
        delivery,
    };
    if (raw && typeof raw === 'object') {
        const rawRecord = raw as Record<string, any>;
        const rawDelivery = rawRecord.delivery && typeof rawRecord.delivery === 'object' ? rawRecord.delivery : undefined;
        return normalizeAppResultContract({ ...fallback, ...rawRecord, delivery: rawDelivery || delivery }, kind, []);
    }
    return normalizeAppResultContract(fallback, kind, []);
}

function dataSrvInstalledWorkflowMapping(metadata: Record<string, any>, kind: AppKind, domain: string, objectRole: string): AppWorkflowMapping | undefined {
    const rawGovernance = metadata.governance && typeof metadata.governance === 'object' ? metadata.governance as Record<string, any> : {};
    const raw = metadata.workflow_mapping || metadata.workflowMapping || rawGovernance.workflow_mapping || rawGovernance.workflowMapping;
    const fallback = {
        schema: metadata.workflow_mapping_schema || 'maclaw.app.workflow.v1',
        submitNode: metadata.workflow_submit_node,
        approvalNode: metadata.workflow_approval_node,
        resultNode: metadata.workflow_result_node,
        attentionNode: metadata.workflow_attention_node,
        statusMapping: metadata.workflow_status_mapping,
    };
    return normalizeAppWorkflowMapping(raw || fallback, kind, domain, objectRole);
}

function dataSrvInstalledVersionSnapshot(metadata: Record<string, any>, item: DataSrvAppInstallationItem, workflowIDs: string[], primaryBinding: Record<string, any>): BackendAppInstallVersionSnapshot | undefined {
    const explicit = normalizeVersionSnapshot(metadata.version_snapshot || metadata.versionSnapshot);
    if (explicit) return explicit;
    const snapshot: BackendAppInstallVersionSnapshot = {};
    const appEntryVersion = String(metadata.app_entry_version || metadata.appEntryVersion || item.version || '').trim();
    if (appEntryVersion) snapshot.app_entry_version = appEntryVersion;
    const appSkill = normalizeVersionSnapshotSkill(undefined, {
        id: String(metadata.app_skill_id || '').trim(),
        version: String(metadata.app_skill_version || '').trim(),
        kind: 'app_skill',
        source: String(metadata.app_skill_source || metadata.appSkillSource || 'hub').trim(),
    });
    if (appSkill) snapshot.app_skill = appSkill;
    const workflowVersionRefs = parseStringList(metadata.workflow_skill_versions || metadata.workflowSkillVersions)
        .map(parseVersionRef)
        .filter((item): item is { id: string; version?: string } => !!item?.id);
    const workflowSkills = workflowVersionRefs.map((ref) => normalizeVersionSnapshotSkill(undefined, { id: ref.id, version: ref.version, kind: 'workflow_skill', source: 'hub' })).filter((skill): skill is BackendAppInstallSkillVersionSnapshot => !!skill);
    workflowIDs.forEach((id) => {
        if (!workflowSkills.some((skill) => skill.id === id)) {
            const skill = normalizeVersionSnapshotSkill(undefined, { id, kind: 'workflow_skill', source: 'hub' });
            if (skill) workflowSkills.push(skill);
        }
    });
    if (workflowSkills.length) snapshot.workflow_skills = dedupeVersionSnapshotSkills(workflowSkills);
    const objectRole = String(primaryBinding.object_role || primaryBinding.objectRole || metadata.object_role || metadata.objectRole || '').trim();
    const approvalBindings = parseStringList(metadata.approval_binding_versions || metadata.approvalBindingVersions)
        .map((ref) => parseApprovalBindingVersionRef(ref, objectRole))
        .filter((binding): binding is BackendAppInstallApprovalBindingSnapshot => !!binding);
    if (!approvalBindings.length && snapshot.workflow_skills?.length) {
        const workflow = snapshot.workflow_skills[0];
        const event = String(metadata.approval_event || metadata.approvalEvent || '').trim();
        approvalBindings.push({ event: event || undefined, object_role: objectRole || undefined, workflow_skill_id: workflow.id, workflow_version: workflow.version });
    }
    if (approvalBindings.length) snapshot.approval_bindings = dedupeVersionSnapshotBindings(approvalBindings);
    return hasVersionSnapshotItems(snapshot) ? snapshot : undefined;
}

function dataSrvInstalledApprovalBinding(domain: string, workflowSkillID: string, primaryBinding: Record<string, any>, versionSnapshot: BackendAppInstallVersionSnapshot | undefined): AppApprovalBinding {
    const objectRole = String(primaryBinding.object_role || primaryBinding.objectRole || '').trim() || domain;
    const snapshotBinding = (versionSnapshot?.approval_bindings || []).find((binding) => binding.workflow_skill_id === workflowSkillID) || versionSnapshot?.approval_bindings?.[0];
    const workflowSkill = (versionSnapshot?.workflow_skills || []).find((skill) => skill.id === workflowSkillID);
    return {
        event: snapshotBinding?.event || domain + '.submitted',
        workflowSkillId: snapshotBinding?.workflow_skill_id || workflowSkillID,
        workflowVersion: snapshotBinding?.workflow_version || workflowSkill?.version || undefined,
        objectRole: snapshotBinding?.object_role || objectRole,
    };
}

function normalizeImportedRunEvidence(raw: unknown): AppRunHistoryEntry | undefined {
    const value = raw && typeof raw === 'object' ? raw as Partial<AppRunHistoryEntry> : undefined;
    const runID = String(value?.runID || '').trim();
    if (!runID) return undefined;
    const status = value?.status === 'error' || value?.status === 'cancelled' ? value.status : 'done';
    const at = String(value?.at || '').trim() || new Date().toISOString();
    const dependencyVerification = normalizeAppRunEvidenceDependencyVerification(value?.dependencyVerification);
    const artifacts = Array.isArray(value?.artifacts) ? value.artifacts : undefined;
    const primaryArtifact = artifacts?.[0] as (SkillRunArtifactView & Record<string, unknown>) | undefined;
    return {
        runID,
        appID: String(value?.appID || '').trim(),
        status,
        definitionHash: String(value?.definitionHash || '').trim() || undefined,
        testProtocolFingerprint: String(value?.testProtocolFingerprint || '').trim() || undefined,
        workspaceLayoutFingerprint: String(value?.workspaceLayoutFingerprint || '').trim() || undefined,
        outputMode: String(value?.outputMode || '').trim() || 'imported',
        inputSummary: String(value?.inputSummary || '').trim() || 'Imported DataSrv test evidence',
        message: String(value?.message || '').trim() || 'Imported DataSrv test evidence',
        artifactName: String(value?.artifactName || primaryArtifact?.name || primaryArtifact?.id || '').trim() || undefined,
        artifactPath: String(value?.artifactPath || primaryArtifact?.path || '').trim() || undefined,
        artifactURI: String(value?.artifactURI || primaryArtifact?.uri || primaryArtifact?.remote_url || '').trim() || undefined,
        artifactDownloadState: String(value?.artifactDownloadState || primaryArtifact?.download_state || '').trim() || undefined,
        artifacts,
        resultPayload: value?.resultPayload && typeof value.resultPayload === 'object' ? value.resultPayload as Record<string, unknown> : undefined,
        outputs: Array.isArray(value?.outputs) ? value.outputs : undefined,
        resultCoverage: value?.resultCoverage && typeof value.resultCoverage === 'object' ? value.resultCoverage as Record<string, unknown> : undefined,
        dependencyVerification,
        approvalInstance: normalizeAppRunApprovalInstanceEvidence((value as any)?.approvalInstance || (value as any)?.approval_instance || (value as any)?.approval),
        at,
    };
}

function appEvidenceRecord(value: unknown): Record<string, unknown> | undefined {
    return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
}

function firstAppEvidenceValue(...values: unknown[]) {
    return values.find((value) => value !== undefined && value !== null && value !== '');
}

function appEvidenceString(...values: unknown[]) {
    const value = firstAppEvidenceValue(...values);
    return String(value || '').trim();
}

function appEvidenceNumber(...values: unknown[]) {
    const value = firstAppEvidenceValue(...values);
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : undefined;
}

function appEvidenceBool(...values: unknown[]) {
    const value = firstAppEvidenceValue(...values);
    if (typeof value === 'boolean') return value;
    if (typeof value === 'number') return value !== 0;
    if (typeof value === 'string') {
        const normalized = value.trim().toLowerCase();
        if (['true', '1', 'yes', 'y'].includes(normalized)) return true;
        if (['false', '0', 'no', 'n'].includes(normalized)) return false;
    }
    return undefined;
}

function dependencyInstallTraceFromSummary(source: Record<string, unknown> | undefined): Record<string, unknown> | undefined {
    if (!source) return undefined;
    const existing = appEvidenceRecord(source.installTrace) || appEvidenceRecord(source.install_trace);
    if (existing) return existing;
    const dependencyCount = appEvidenceNumber(source.dependencyCount, source.dependency_count, source.test_evidence_dependency_count, source.dependency_count);
    const preflightChecked = appEvidenceNumber(source.preflightCheckedCount, source.preflight_checked_count, source.dependency_preflight_checked_count);
    const preflightReady = appEvidenceNumber(source.preflightReadyCount, source.preflight_ready_count, source.dependency_preflight_ready_count);
    const preflightFailed = appEvidenceNumber(source.preflightFailedCount, source.preflight_failed_count, source.dependency_preflight_failed_count);
    const integrityChecked = appEvidenceNumber(source.integrityCheckedCount, source.integrity_checked_count, source.dependency_integrity_checked_count);
    const integrityReady = appEvidenceNumber(source.integrityReadyCount, source.integrity_ready_count, source.dependency_integrity_ready_count);
    const integrityFailed = appEvidenceNumber(source.integrityFailedCount, source.integrity_failed_count, source.dependency_integrity_failed_count);
    const downloadAvailable = appEvidenceNumber(source.downloadAvailableCount, source.download_available_count, source.dependency_download_available_count);
    const signatureAvailable = appEvidenceNumber(source.signatureAvailableCount, source.signature_available_count, source.dependency_signature_available_count);
    const installErrors = appEvidenceNumber(source.installErrorCount, source.install_error_count, source.dependency_install_error_count);
    const ok = appEvidenceBool(source.ok, source.traceOk, source.trace_ok, source.dependency_install_trace_ok);
    const hasTrace = [dependencyCount, preflightChecked, preflightReady, preflightFailed, integrityChecked, integrityReady, integrityFailed, downloadAvailable, signatureAvailable, installErrors, ok]
        .some((value) => value !== undefined);
    if (!hasTrace) return undefined;
    return {
        schema: 'maclaw.app.dependency_install_trace.v1',
        ...(dependencyCount !== undefined ? { dependency_count: dependencyCount, dependencyCount } : {}),
        ...(preflightChecked !== undefined ? { preflight_checked_count: preflightChecked, preflightCheckedCount: preflightChecked } : {}),
        ...(preflightReady !== undefined ? { preflight_ready_count: preflightReady, preflightReadyCount: preflightReady } : {}),
        ...(preflightFailed !== undefined ? { preflight_failed_count: preflightFailed, preflightFailedCount: preflightFailed } : {}),
        ...(integrityChecked !== undefined ? { integrity_checked_count: integrityChecked, integrityCheckedCount: integrityChecked } : {}),
        ...(integrityReady !== undefined ? { integrity_ready_count: integrityReady, integrityReadyCount: integrityReady } : {}),
        ...(integrityFailed !== undefined ? { integrity_failed_count: integrityFailed, integrityFailedCount: integrityFailed } : {}),
        ...(downloadAvailable !== undefined ? { download_available_count: downloadAvailable, downloadAvailableCount: downloadAvailable } : {}),
        ...(signatureAvailable !== undefined ? { signature_available_count: signatureAvailable, signatureAvailableCount: signatureAvailable } : {}),
        ...(installErrors !== undefined ? { install_error_count: installErrors, installErrorCount: installErrors } : {}),
        ...(ok !== undefined ? { ok, traceOk: ok } : {}),
    };
}

function normalizeAppRunEvidenceDependencyVerification(raw: unknown): AppRunEvidenceDependencyVerification | undefined {
    const value = appEvidenceRecord(raw);
    if (!value) return undefined;
    const schema = appEvidenceString(value.schema) || 'maclaw.app.install_plan.v1';
    if (schema !== 'maclaw.app.install_plan.v1') return undefined;
    const dependencies = parseBackendAppInstallDependencies(value.dependencies);
    const workflowContractIssues = parseReviewIssues(value.workflowContractIssues || value.workflow_contract_issues);
    const governanceReviewIssues = parseReviewIssues(value.governanceReviewIssues || value.governance_review_issues);
    const dependencyCount = appEvidenceNumber(value.dependencyCount, value.dependency_count) ?? dependencies.length;
    const installTrace = dependencyInstallTraceFromSummary(value);
    return {
        schema: 'maclaw.app.install_plan.v1',
        verifiedAt: appEvidenceString(value.verifiedAt, value.verified_at) || new Date().toISOString(),
        appCount: appEvidenceNumber(value.appCount, value.app_count) ?? 1,
        dependencyCount,
        hasMissingRequired: appEvidenceBool(value.hasMissingRequired, value.has_missing_required) ?? false,
        hasBlockingDependency: appEvidenceBool(value.hasBlockingDependency, value.has_blocking_dependency) ?? false,
        hasWorkflowContractIssue: appEvidenceBool(value.hasWorkflowContractIssue, value.has_workflow_contract_issue) ?? workflowContractIssues.length > 0,
        workflowContractIssueCount: appEvidenceNumber(value.workflowContractIssueCount, value.workflow_contract_issue_count) ?? workflowContractIssues.length,
        hasGovernanceReviewIssue: appEvidenceBool(value.hasGovernanceReviewIssue, value.has_governance_review_issue) ?? governanceReviewIssues.length > 0,
        governanceReviewIssueCount: appEvidenceNumber(value.governanceReviewIssueCount, value.governance_review_issue_count) ?? governanceReviewIssues.length,
        workflowContractIssues,
        governanceReviewIssues,
        ...(installTrace ? { installTrace, install_trace: installTrace } : {}),
        dependencies,
    };
}

function dataSrvInstalledDependencyVerificationEvidence(metadata: Record<string, any>, evidence: Record<string, any>): AppRunEvidenceDependencyVerification | undefined {
    const installEvidence = appEvidenceRecord(metadata.install_evidence) || appEvidenceRecord(metadata.installEvidence) || {};
    const verification = appEvidenceRecord(evidence.dependencyVerification)
        || appEvidenceRecord(evidence.dependency_verification)
        || appEvidenceRecord(metadata.dependencyVerification)
        || appEvidenceRecord(metadata.dependency_verification)
        || appEvidenceRecord(installEvidence.dependencyVerification)
        || appEvidenceRecord(installEvidence.dependency_verification)
        || appEvidenceRecord(metadata.test_evidence_dependency_verification);
    const hasSummary = [
        metadata.test_evidence_dependency_verified_at,
        metadata.test_evidence_dependency_count,
        metadata.test_evidence_dependency_missing_required,
        metadata.test_evidence_dependency_blocking,
        metadata.test_evidence_workflow_contract_issue,
        metadata.test_evidence_workflow_contract_issue_count,
        metadata.test_evidence_governance_review_issue,
        metadata.test_evidence_governance_review_issue_count,
    ].some((value) => value !== undefined && value !== null && value !== '');
    if (!verification && !hasSummary) return undefined;
    const dependencies = firstAppEvidenceValue(verification?.dependencies, installEvidence.dependencies, metadata.dependencies);
    return normalizeAppRunEvidenceDependencyVerification({
        schema: appEvidenceString(verification?.schema) || 'maclaw.app.install_plan.v1',
        verifiedAt: appEvidenceString(verification?.verifiedAt, verification?.verified_at, metadata.test_evidence_dependency_verified_at),
        appCount: appEvidenceNumber(verification?.appCount, verification?.app_count) ?? 1,
        dependencyCount: appEvidenceNumber(verification?.dependencyCount, verification?.dependency_count, metadata.test_evidence_dependency_count),
        hasMissingRequired: appEvidenceBool(verification?.hasMissingRequired, verification?.has_missing_required, metadata.test_evidence_dependency_missing_required),
        hasBlockingDependency: appEvidenceBool(verification?.hasBlockingDependency, verification?.has_blocking_dependency, metadata.test_evidence_dependency_blocking),
        hasWorkflowContractIssue: appEvidenceBool(verification?.hasWorkflowContractIssue, verification?.has_workflow_contract_issue, metadata.test_evidence_workflow_contract_issue),
        workflowContractIssueCount: appEvidenceNumber(verification?.workflowContractIssueCount, verification?.workflow_contract_issue_count, metadata.test_evidence_workflow_contract_issue_count),
        hasGovernanceReviewIssue: appEvidenceBool(verification?.hasGovernanceReviewIssue, verification?.has_governance_review_issue, metadata.test_evidence_governance_review_issue),
        governanceReviewIssueCount: appEvidenceNumber(verification?.governanceReviewIssueCount, verification?.governance_review_issue_count, metadata.test_evidence_governance_review_issue_count),
        workflowContractIssues: firstAppEvidenceValue(verification?.workflowContractIssues, verification?.workflow_contract_issues, metadata.test_evidence_workflow_contract_issues),
        governanceReviewIssues: firstAppEvidenceValue(verification?.governanceReviewIssues, verification?.governance_review_issues, metadata.test_evidence_governance_review_issues),
        installTrace: firstAppEvidenceValue(verification?.installTrace, verification?.install_trace, metadata.dependency_install_trace, installEvidence.dependency_install_trace, installEvidence.dependencyInstallTrace),
        install_trace: firstAppEvidenceValue(verification?.install_trace, verification?.installTrace, metadata.dependency_install_trace, installEvidence.dependency_install_trace, installEvidence.dependencyInstallTrace),
        dependencies,
    });
}

function dataSrvInstalledResultCoverageEvidence(metadata: Record<string, any>, evidence: Record<string, any>): Record<string, unknown> | undefined {
    const coverage = appEvidenceRecord(evidence.resultCoverage)
        || appEvidenceRecord(evidence.result_coverage)
        || appEvidenceRecord(metadata.test_evidence_result_coverage);
    const ok = appEvidenceBool(coverage?.ok, metadata.test_evidence_result_coverage_ok);
    const primary = appEvidenceString(coverage?.primary, metadata.test_evidence_result_coverage_primary);
    const coveredTypes = parseStringList(firstAppEvidenceValue(coverage?.coveredTypes, coverage?.covered_types, metadata.test_evidence_covered_types));
    const coveredCount = appEvidenceNumber(coverage?.coveredCount, coverage?.covered_count, metadata.test_evidence_result_coverage_covered_count);
    const missingTypes = parseStringList(firstAppEvidenceValue(coverage?.missingTypes, coverage?.missing_types, metadata.test_evidence_missing_types));
    const missingCount = appEvidenceNumber(coverage?.missingCount, coverage?.missing_count, metadata.test_evidence_result_coverage_missing_count);
    if (coverage) {
        return {
            ...coverage,
            ...(ok !== undefined ? { ok } : {}),
            ...(primary ? { primary } : {}),
            ...(coveredTypes.length ? { covered_types: coveredTypes } : coveredCount !== undefined ? { covered_count: coveredCount } : {}),
            ...(missingTypes.length ? { missing_types: missingTypes } : missingCount !== undefined ? { missing_count: missingCount } : {}),
        };
    }
    if (ok === undefined && !primary && coveredTypes.length === 0 && coveredCount === undefined && missingTypes.length === 0 && missingCount === undefined) return undefined;
    return {
        ...(ok !== undefined ? { ok } : {}),
        ...(primary ? { primary } : {}),
        ...(coveredTypes.length ? { covered_types: coveredTypes } : coveredCount !== undefined ? { covered_count: coveredCount } : {}),
        ...(missingTypes.length ? { missing_types: missingTypes } : missingCount !== undefined ? { missing_count: missingCount } : {}),
    };
}

function dataSrvInstalledSubmissionEvidence(metadata: Record<string, any>, evidence: Record<string, any>): Record<string, unknown> | undefined {
    const submission = appEvidenceRecord(evidence.submission)
        || appEvidenceRecord(metadata.submission)
        || appEvidenceRecord(evidence.governance_submission)
        || appEvidenceRecord(metadata.governance_submission);
    const packageSignature = appEvidenceRecord(firstAppEvidenceValue(
        submission?.package_signature,
        submission?.packageSignature,
        evidence.hub_package_signature,
        metadata.hub_package_signature,
        evidence.package_signature,
        metadata.package_signature,
    ));
    const synthesized = {
        capability_id: appEvidenceString(
            submission?.capability_id,
            submission?.capabilityID,
            submission?.hub_capability_id,
            metadata.hub_capability_id,
            metadata.hub_market_capability_id,
        ),
        market_capability_id: appEvidenceString(
            submission?.market_capability_id,
            submission?.marketCapabilityID,
            metadata.hub_market_capability_id,
            metadata.hub_capability_id,
        ),
        submission_id: appEvidenceString(submission?.submission_id, submission?.submissionID, metadata.hub_submission_id),
        version_key: appEvidenceString(submission?.version_key, submission?.versionKey, metadata.hub_version_key),
        status: appEvidenceString(submission?.status, submission?.review_status, submission?.reviewStatus, metadata.hub_review_status),
        package_sha256: appEvidenceString(submission?.package_sha256, submission?.packageSHA256, metadata.hub_package_sha256),
        package_signature: packageSignature,
    };
    const compact = Object.fromEntries(Object.entries(synthesized).filter(([, value]) => value !== undefined && value !== null && value !== ''));
    if (submission) return { ...compact, ...submission };
    return Object.keys(compact).length > 0 ? compact : undefined;
}
function dataSrvInstalledInstallEvidence(
    metadata: Record<string, any>,
    item: DataSrvAppInstallationItem,
    versionSnapshot: BackendAppInstallVersionSnapshot | undefined,
    workflowMapping: AppWorkflowMapping | undefined,
    workflowContract: AppWorkflowContract | undefined,
    resultContract: AppResultContract | undefined,
): BackendAppInstallRecord | undefined {
    const appID = appEvidenceString(item.app_id, item.appId);
    const appName = appEvidenceString(item.name, appID);
    const kind = normalizeAppKind(item.kind || metadata.kind || 'enterprise_normal_app');
    const rawInstallEvidence = appEvidenceRecord(metadata.install_evidence) || appEvidenceRecord(metadata.installEvidence) || {};
    const rawTestEvidence = appEvidenceRecord(rawInstallEvidence.test_evidence)
        || appEvidenceRecord(rawInstallEvidence.testEvidence)
        || appEvidenceRecord(metadata.test_evidence)
        || {};
    const rawApprovalEvidence = appEvidenceRecord(firstAppEvidenceValue(rawTestEvidence.approval_instance, rawTestEvidence.approvalInstance, rawTestEvidence.approval, metadata.test_evidence_approval_instance, metadata.approval_instance, metadata.approvalInstance));
    const synthesizedTestEvidence = {
            run_id: metadata.test_evidence_run_id,
            verified_at: metadata.test_evidence_verified_at,
            definition_fingerprint: metadata.test_evidence_definition_fingerprint,
            test_protocol: firstAppEvidenceValue(rawTestEvidence.test_protocol, rawTestEvidence.testProtocol, metadata.test_evidence_test_protocol),
            test_protocol_fingerprint: metadata.test_evidence_test_protocol_fingerprint,
            artifact_present: metadata.test_evidence_artifact_present,
            artifact_name: metadata.test_evidence_artifact_name,
            artifact_count: metadata.test_evidence_artifact_count,
            output_count: metadata.test_evidence_output_count,
            primary_result: metadata.test_evidence_primary_result,
            result_payload: firstAppEvidenceValue(metadata.test_evidence_result_payload, rawApprovalEvidence?.result_payload, rawApprovalEvidence?.resultPayload),
            outputs: firstAppEvidenceValue(metadata.test_evidence_outputs, rawApprovalEvidence?.outputs, rawApprovalEvidence?.output_blocks, rawApprovalEvidence?.outputBlocks),
            artifacts: firstAppEvidenceValue(metadata.test_evidence_artifacts, rawApprovalEvidence?.artifacts),
            approval_instance: rawApprovalEvidence,
            result_coverage: dataSrvInstalledResultCoverageEvidence(metadata, rawTestEvidence),
        };
    const testEvidence = Object.keys(rawTestEvidence).length > 0
        ? {
            ...synthesizedTestEvidence,
            ...rawTestEvidence,
            result_payload: firstAppEvidenceValue(rawTestEvidence.result_payload, rawTestEvidence.resultPayload, synthesizedTestEvidence.result_payload),
            outputs: firstAppEvidenceValue(rawTestEvidence.outputs, rawTestEvidence.output_blocks, rawTestEvidence.outputBlocks, synthesizedTestEvidence.outputs),
            artifacts: firstAppEvidenceValue(rawTestEvidence.artifacts, synthesizedTestEvidence.artifacts),
            approval_instance: rawApprovalEvidence,
            result_coverage: firstAppEvidenceValue(rawTestEvidence.result_coverage, rawTestEvidence.resultCoverage, synthesizedTestEvidence.result_coverage),
        }
        : synthesizedTestEvidence;
    const rawWorkspaceLayout = appEvidenceRecord(rawInstallEvidence.workspace_layout)
        || appEvidenceRecord(rawInstallEvidence.workspaceLayout)
        || appEvidenceRecord(metadata.workspace_layout)
        || {};
    const workspaceLayout: Record<string, any> = Object.keys(rawWorkspaceLayout).length > 0
        ? { ...rawWorkspaceLayout }
        : {
            entry: metadata.workspace_layout_entry,
            template: metadata.workspace_layout_template,
            density: metadata.workspace_layout_density,
            primaryRegion: metadata.workspace_layout_primary_region,
            outputRegion: metadata.workspace_layout_output_region,
            region_count: metadata.workspace_layout_region_count,
            region_ids: metadata.workspace_layout_region_ids,
            navigation: metadata.workspace_layout_navigation,
            list: metadata.workspace_layout_list_columns ? { columns: metadata.workspace_layout_list_columns } : undefined,
        };
    if (Object.keys(workspaceLayout).length > 0) {
        const regionCount = firstAppEvidenceValue(workspaceLayout.region_count, workspaceLayout.regionCount, metadata.workspace_layout_region_count, metadata.workspaceLayoutRegionCount);
        if (regionCount !== undefined && regionCount !== null && regionCount !== '') {
            workspaceLayout.region_count = regionCount;
            workspaceLayout.regionCount = regionCount;
        }
        const regionIDs = firstAppEvidenceValue(workspaceLayout.region_ids, workspaceLayout.regionIds, metadata.workspace_layout_region_ids, metadata.workspaceLayoutRegionIds);
        if (Array.isArray(regionIDs) && regionIDs.length > 0) {
            workspaceLayout.region_ids = regionIDs;
            workspaceLayout.regionIds = regionIDs;
        }
    }
    const dependencyVerification = appEvidenceRecord(rawInstallEvidence.dependency_verification)
        || appEvidenceRecord(rawInstallEvidence.dependencyVerification)
        || appEvidenceRecord(metadata.dependency_verification)
        || appEvidenceRecord(rawTestEvidence.dependencyVerification)
        || appEvidenceRecord(rawTestEvidence.dependency_verification)
        || dataSrvInstalledDependencyVerificationEvidence(metadata, rawTestEvidence);
    const dependencyVerificationRecord = dependencyVerification as Record<string, any> | undefined;
    const dependencies = parseBackendAppInstallDependencies(dependencyVerificationRecord?.dependencies || rawInstallEvidence.dependencies || metadata.dependencies);
    const dataSrvRegistration = appEvidenceRecord(rawInstallEvidence.datasrv_registration)
        || appEvidenceRecord(rawInstallEvidence.dataSrvRegistration)
        || appEvidenceRecord(metadata.datasrv_registration)
        || appEvidenceRecord(metadata.dataSrvRegistration);
    const submission = dataSrvInstalledSubmissionEvidence(metadata, rawInstallEvidence);
    const hubPackageSignature = appEvidenceRecord(firstAppEvidenceValue(
        rawInstallEvidence.hub_package_signature,
        rawInstallEvidence.hubPackageSignature,
        rawInstallEvidence.package_signature,
        rawInstallEvidence.packageSignature,
        metadata.hub_package_signature,
        metadata.hubPackageSignature,
        metadata.package_signature,
        metadata.packageSignature,
        submission?.package_signature,
        submission?.packageSignature,
    ));
    const reviewEvidence = appEvidenceRecord(rawInstallEvidence.review_evidence)
        || appEvidenceRecord(rawInstallEvidence.reviewEvidence)
        || appEvidenceRecord(rawInstallEvidence.maclaw_app_review_evidence)
        || appEvidenceRecord(metadata.review_evidence)
        || appEvidenceRecord(metadata.reviewEvidence)
        || appEvidenceRecord(metadata.maclaw_app_review_evidence);
    const hasEvidence = [
        versionSnapshot,
        workflowMapping,
        workflowContract,
        resultContract,
        dependencyVerification,
        submission,
        hubPackageSignature,
        reviewEvidence,
        dependencies.length ? dependencies : undefined,
        Object.values(testEvidence).some((value) => value !== undefined && value !== null && value !== ''),
        Object.values(workspaceLayout).some((value) => value !== undefined && value !== null && value !== ''),
    ].some(Boolean);
    if (!hasEvidence) return undefined;
    return {
        schema: 'maclaw.app.install_record.v1',
        package_sha: appEvidenceString(metadata.package_sha, metadata.package_sha256),
        package_sha256: appEvidenceString(metadata.package_sha256, metadata.package_sha),
        hub_package_signature: hubPackageSignature,
        hub_package_signature_algorithm: appEvidenceString(rawInstallEvidence.hub_package_signature_algorithm, rawInstallEvidence.hubPackageSignatureAlgorithm, metadata.hub_package_signature_algorithm, metadata.hubPackageSignatureAlgorithm, hubPackageSignature?.algorithm),
        hub_package_signature_fingerprint: appEvidenceString(rawInstallEvidence.hub_package_signature_fingerprint, rawInstallEvidence.hubPackageSignatureFingerprint, metadata.hub_package_signature_fingerprint, metadata.hubPackageSignatureFingerprint, hubPackageSignature?.public_key_fingerprint, hubPackageSignature?.publicKeyFingerprint, hubPackageSignature?.fingerprint),
        hub_package_signature_signed_at: appEvidenceString(rawInstallEvidence.hub_package_signature_signed_at, rawInstallEvidence.hubPackageSignatureSignedAt, metadata.hub_package_signature_signed_at, metadata.hubPackageSignatureSignedAt, hubPackageSignature?.signed_at, hubPackageSignature?.signedAt),
        hub_package_signature_signed_by: appEvidenceString(rawInstallEvidence.hub_package_signature_signed_by, rawInstallEvidence.hubPackageSignatureSignedBy, metadata.hub_package_signature_signed_by, metadata.hubPackageSignatureSignedBy, hubPackageSignature?.signed_by, hubPackageSignature?.signedBy),
        source: appEvidenceString(item.source, metadata.source),
        installed_at: appEvidenceString(item.updated_at, item.updatedAt, metadata.installed_at, metadata.updated_at),
        app_count: 1,
        apps: Array.isArray(rawInstallEvidence.apps) ? rawInstallEvidence.apps as BackendAppInstallRecord['apps'] : appID ? [{ id: appID, name: appName, kind }] : undefined,
        dependencies,
        has_missing_required: appEvidenceBool(rawInstallEvidence.hasMissingRequired, rawInstallEvidence.has_missing_required, dependencyVerificationRecord?.hasMissingRequired, dependencyVerificationRecord?.has_missing_required, metadata.has_missing_required_dependency, metadata.has_missing_required),
        has_blocking_dependency: appEvidenceBool(rawInstallEvidence.hasBlockingDependency, rawInstallEvidence.has_blocking_dependency, dependencyVerificationRecord?.hasBlockingDependency, dependencyVerificationRecord?.has_blocking_dependency, metadata.has_blocking_dependency),
        version_snapshot: appEvidenceRecord(rawInstallEvidence.version_snapshot) as BackendAppInstallVersionSnapshot || appEvidenceRecord(rawInstallEvidence.versionSnapshot) as BackendAppInstallVersionSnapshot || versionSnapshot,
        workspace_layout: Object.keys(workspaceLayout).length > 0 ? workspaceLayout : undefined,
        result_contract: appEvidenceRecord(rawInstallEvidence.result_contract) as AppResultContract || appEvidenceRecord(rawInstallEvidence.resultContract) as AppResultContract || resultContract,
        workflow_mapping: appEvidenceRecord(rawInstallEvidence.workflow_mapping) as AppWorkflowMapping || appEvidenceRecord(rawInstallEvidence.workflowMapping) as AppWorkflowMapping || workflowMapping,
        workflow_contract: appEvidenceRecord(rawInstallEvidence.workflow_contract) as AppWorkflowContract || appEvidenceRecord(rawInstallEvidence.workflowContract) as AppWorkflowContract || workflowContract,
        test_evidence: Object.keys(testEvidence).length > 0 ? testEvidence : undefined,
        submission,
        review_evidence: reviewEvidence,
        dependency_verification: dependencyVerification,
        datasrv_registration: dataSrvRegistration as BackendAppDataSrvRegistration | undefined,
    };
}

function dataSrvInstalledRunEvidence(metadata: Record<string, any>, appID: string): AppRunHistoryEntry | undefined {
    const installEvidence = appEvidenceRecord(metadata.install_evidence) || appEvidenceRecord(metadata.installEvidence) || {};
    const evidence = appEvidenceRecord(metadata.test_evidence)
        || appEvidenceRecord(metadata.testEvidence)
        || appEvidenceRecord(installEvidence.test_evidence)
        || appEvidenceRecord(installEvidence.testEvidence)
        || {};
    const runID = String(evidence.run_id || evidence.runId || metadata.test_evidence_run_id || '').trim();
    if (!runID) return undefined;
    const approvalEvidence = appEvidenceRecord(firstAppEvidenceValue(evidence.approvalInstance, evidence.approval_instance, evidence.approval, metadata.test_evidence_approval_instance, metadata.approvalInstance, metadata.approval_instance));
    const progressEvidence = firstAppEvidenceValue(
        evidence.progressInstances,
        evidence.progress_instances,
        evidence.workflowProgress,
        evidence.workflow_progress,
        evidence.approvalProgress,
        evidence.approval_progress,
        metadata.test_evidence_progress_instances,
        metadata.test_evidence_workflow_progress,
        metadata.test_evidence_approval_progress,
        metadata.progressInstances,
        metadata.progress_instances,
        metadata.workflowProgress,
        metadata.workflow_progress,
        metadata.approvalProgress,
        metadata.approval_progress,
    );
    const approvalEvidenceWithProgress = approvalEvidence && progressEvidence !== undefined && approvalEvidence.progressInstances === undefined && approvalEvidence.progress_instances === undefined && approvalEvidence.workflowProgress === undefined && approvalEvidence.workflow_progress === undefined && approvalEvidence.approvalProgress === undefined && approvalEvidence.approval_progress === undefined
        ? { ...approvalEvidence, progressInstances: progressEvidence }
        : approvalEvidence;
    const artifactName = String(evidence.artifact_name || evidence.artifactName || metadata.test_evidence_artifact_name || '').trim();
    const rawEvidenceArtifacts = firstAppEvidenceValue(evidence.artifacts, approvalEvidence?.artifacts, metadata.test_evidence_artifacts);
    const evidenceArtifacts = Array.isArray(rawEvidenceArtifacts) ? rawEvidenceArtifacts as ApprovalInstanceArtifactView[] : [];
    const artifactCount = Number(evidence.artifact_count ?? evidence.artifactCount ?? metadata.test_evidence_artifact_count ?? 0) || 0;
    const artifactPresent = !!(evidence.artifact_present ?? evidence.artifactPresent ?? metadata.test_evidence_artifact_present) || evidenceArtifacts.length > 0 || artifactCount > 0;
    const resultPayload = appEvidenceRecord(firstAppEvidenceValue(evidence.result_payload, evidence.resultPayload, metadata.test_evidence_result_payload, approvalEvidence?.resultPayload, approvalEvidence?.result_payload));
    const outputCount = Number(evidence.output_count ?? evidence.outputCount ?? metadata.test_evidence_output_count ?? 0) || 0;
    const rawEvidenceOutputs = firstAppEvidenceValue(evidence.outputs, evidence.output_blocks, evidence.outputBlocks, approvalEvidence?.outputs, metadata.test_evidence_outputs, metadata.outputs);
    const evidenceOutputs = normalizeApprovalOutputs(Array.isArray(rawEvidenceOutputs) ? rawEvidenceOutputs as Array<SkillRunOutputBlockView | ApprovalInstanceOutputView | null | undefined> : undefined) || [];
    const primaryResult = String(evidence.primary_result || evidence.primaryResult || metadata.test_evidence_primary_result || '').trim();
    const testProtocolFingerprint = appEvidenceString(evidence.testProtocolFingerprint, evidence.test_protocol_fingerprint, evidence.testProtocolHash, evidence.test_protocol_hash, metadata.test_evidence_test_protocol_fingerprint);
    return normalizeImportedRunEvidence({
        runID,
        appID,
        status: 'done',
        definitionHash: String(evidence.definition_fingerprint || evidence.definitionFingerprint || evidence.definition_hash || evidence.definitionHash || metadata.test_evidence_definition_fingerprint || '').trim() || undefined,
        testProtocolFingerprint: testProtocolFingerprint || undefined,
        outputMode: primaryResult || 'imported',
        inputSummary: 'Imported DataSrv test evidence',
        message: primaryResult || 'Imported DataSrv test evidence',
        artifactName: artifactPresent ? artifactName || evidenceArtifacts[0]?.name || undefined : undefined,
        artifacts: evidenceArtifacts.length > 0 ? evidenceArtifacts : artifactPresent && artifactName ? [{ name: artifactName, status: 'ready' }] : undefined,
        resultPayload,
        outputs: evidenceOutputs.length > 0 ? evidenceOutputs : outputCount > 0 ? [{ type: 'summary', title: 'Imported outputs', text: String(outputCount) }] : undefined,
        resultCoverage: dataSrvInstalledResultCoverageEvidence(metadata, evidence),
        dependencyVerification: dataSrvInstalledDependencyVerificationEvidence(metadata, evidence),
        approvalInstance: approvalEvidenceWithProgress,
        at: String(evidence.verified_at || evidence.verifiedAt || metadata.test_evidence_verified_at || '').trim() || new Date().toISOString(),
    });
}

function dataSrvInstalledTestProtocol(metadata: Record<string, any>, kind: AppKind): AppTestProtocol | undefined {
    const evidence = appEvidenceRecord(metadata.test_evidence) || {};
    const installEvidence = appEvidenceRecord(metadata.install_evidence) || appEvidenceRecord(metadata.installEvidence) || {};
    const installTestEvidence = appEvidenceRecord(installEvidence.test_evidence) || appEvidenceRecord(installEvidence.testEvidence) || {};
    const governance = appEvidenceRecord(metadata.governance) || {};
    const governanceEvidence = appEvidenceRecord(governance.testEvidence) || appEvidenceRecord(governance.test_evidence) || {};
    const protocol = appEvidenceRecord(evidence.testProtocol)
        || appEvidenceRecord(evidence.test_protocol)
        || appEvidenceRecord(installTestEvidence.testProtocol)
        || appEvidenceRecord(installTestEvidence.test_protocol)
        || appEvidenceRecord(governance.testProtocol)
        || appEvidenceRecord(governance.test_protocol)
        || appEvidenceRecord(governanceEvidence.testProtocol)
        || appEvidenceRecord(governanceEvidence.test_protocol)
        || appEvidenceRecord(metadata.testProtocol)
        || appEvidenceRecord(metadata.test_protocol)
        || appEvidenceRecord(metadata.test_evidence_test_protocol);
    if (!protocol) return undefined;
    const fingerprint = appEvidenceString(
        protocol.fingerprint,
        protocol.hash,
        evidence.testProtocolFingerprint,
        evidence.test_protocol_fingerprint,
        installTestEvidence.testProtocolFingerprint,
        installTestEvidence.test_protocol_fingerprint,
        governanceEvidence.testProtocolFingerprint,
        governanceEvidence.test_protocol_fingerprint,
        metadata.test_evidence_test_protocol_fingerprint,
    );
    return appTestProtocolWithFingerprint(normalizeAppTestProtocol(
        fingerprint ? { ...protocol, fingerprint } : protocol,
        kind,
        [],
        normalizeAppResultContract(metadata.result_contract || metadata.resultContract, kind, []),
    ));
}

function dataSrvInstalledWorkspaceLayout(metadata: Record<string, any>, kind: AppKind): AppWorkspaceLayout {
    const base = defaultWorkspaceLayoutForKind(kind);
    const installEvidence = appEvidenceRecord(metadata.install_evidence) || appEvidenceRecord(metadata.installEvidence) || {};
    const installLayout = appEvidenceRecord(installEvidence.workspace_layout) || appEvidenceRecord(installEvidence.workspaceLayout);
    const raw = metadata.workspace_layout && typeof metadata.workspace_layout === 'object'
        ? metadata.workspace_layout
        : metadata.workspaceLayout && typeof metadata.workspaceLayout === 'object'
            ? metadata.workspaceLayout
            : installLayout || {};
    const entry = String(raw.entry || metadata.workspace_layout_entry || base.entry || workspaceEntryForKind(kind)).trim();
    const currentLayouts = base.layouts || {};
    const currentLayout = { ...(currentLayouts[entry] || currentLayouts[base.entry || workspaceEntryForKind(kind)] || {}) };
    const rawList = raw.list && typeof raw.list === 'object' ? raw.list : {};
    const navigation = normalizeUIStringList(raw.navigation || metadata.workspace_layout_navigation, []);
    const columns = normalizeUIStringList(rawList.columns || metadata.workspace_layout_list_columns, []);
    const layout = {
        ...currentLayout,
        ...(String(raw.template || metadata.workspace_layout_template || '').trim() ? { template: String(raw.template || metadata.workspace_layout_template).trim() } : {}),
        ...(String(raw.density || metadata.workspace_layout_density || '').trim() ? { density: String(raw.density || metadata.workspace_layout_density).trim() } : {}),
        ...(String(raw.primary_region || raw.primaryRegion || metadata.workspace_layout_primary_region || '').trim() ? { primaryRegion: String(raw.primary_region || raw.primaryRegion || metadata.workspace_layout_primary_region).trim() } : {}),
        ...(String(raw.output_region || raw.outputRegion || metadata.workspace_layout_output_region || '').trim() ? { outputRegion: String(raw.output_region || raw.outputRegion || metadata.workspace_layout_output_region).trim() } : {}),
        ...(navigation.length > 0 ? { navigation } : {}),
        ...(columns.length > 0 ? { list: { ...(currentLayout.list || {}), ...rawList, columns } } : {}),
        ...(Array.isArray(raw.regions) && raw.regions.length > 0 ? { regions: raw.regions } : {}),
        studio: { ...(currentLayout.studio || {}), savedInManifest: true, importedFromDataSrv: true },
    };
    return { ...base, entry, layouts: { ...currentLayouts, [entry]: layout } };
}

function domainTitle(domain: string) {
    const titles: Record<string, string> = {
        sales: 'CRM',
        procurement: '\u91c7\u8d2d',
        inventory: '\u5e93\u5b58',
        finance: '\u8d22\u52a1',
        hr: '\u4eba\u4e8b',
        company: '\u516c\u53f8\u6982\u89c8',
        assets: '\u8d44\u4ea7',
    };
    return titles[domain] || domain;
}

function domainCategory(domain: string) {
    const categories: Record<string, string> = {
        sales: 'CRM',
        procurement: '\u8fdb\u9500\u5b58',
        inventory: '\u8fdb\u9500\u5b58',
        finance: '\u8d22\u52a1',
        hr: 'OA',
        company: 'OA',
        assets: '\u8d44\u4ea7',
    };
    return categories[domain] || 'DataSrv';
}

function domainIcon(domain: string): AppIconName {
    const icons: Record<string, AppIconName> = {
        sales: 'customer',
        procurement: 'warehouse',
        inventory: 'inventory',
        finance: 'receipt',
        hr: 'customer',
        company: 'sheet',
        assets: 'warehouse',
    };
    return icons[domain] || 'sheet';
}

function domainAccent(domain: string) {
    const accents: Record<string, string> = {
        sales: '#8a5a44',
        procurement: '#657a42',
        inventory: '#6b7280',
        finance: '#2f5f98',
        hr: '#5b5ea6',
        company: '#4b6572',
        assets: '#28705f',
    };
    return accents[domain] || '#4b6572';
}

async function discoverSkillAppManifests(): Promise<SkillAppDiscovery> {
    const entries = await Promise.resolve().then(() => ListSkillAppManifests()) as SkillAppManifestEntry[];
    const candidates = (entries || []).map(skillManifestToApp).filter((app): app is AppEntry => !!app);
    const uniqueCandidates: AppEntry[] = [];
    const seenCandidateIds = new Set<string>();
    candidates.forEach((app) => {
        if (seenCandidateIds.has(app.id)) return;
        seenCandidateIds.add(app.id);
        uniqueCandidates.push(app);
    });
    return {
        status: 'ready',
        candidates: uniqueCandidates,
    };
}

function skillManifestToApp(entry: SkillAppManifestEntry): AppEntry | null {
    const fullDefinition = entry?.app_definition;
    if (fullDefinition?.schema === 'maclaw.app.v1' && fullDefinition?.privateMarker === 'x_maclaw_apps') {
        return skillDefinitionManifestToApp(fullDefinition, entry);
    }
    const id = String(entry?.id || '').trim();
    const name = String(entry?.name || '').trim();
    const skillID = String(entry?.skill_id || id).trim();
    if (!id || !name || !skillID) return null;
    const inputMode = entry.input_mode === 'form' || entry.input_mode === 'mixed' ? entry.input_mode : 'file';
    return {
        id: skillPanelAppID(skillID, id),
        name,
        description: String(entry.description || ''),
        category: String(entry.category || 'Skill'),
        kind: 'tool_app',
        icon: normalizeSkillAppIcon(entry.icon),
        customIconDataUrl: normalizeCustomIconDataUrl(entry.custom_icon_data_url || entry.customIconDataUrl),
        accent: '#7c3f58',
        version: normalizeAppVersion((entry as any).version),
        source: 'skill',
        manifest: makeSkillManifest(skillID, inputMode, entry.output_modes, entry.fields, !!entry.multiple_files, entry.app_definition_file || 'maclaw.apps.json'),
    };
}

function skillDefinitionManifestToApp(raw: Record<string, any>, entry: SkillAppManifestEntry): AppEntry | null {
    const app = raw?.app;
    const binding = app?.binding || {};
    const governance = app?.governance || {};
    const id = String(app?.id || entry?.id || '').trim();
    const name = String(app?.name || entry?.name || '').trim();
    const kind = normalizeAppKind(app?.kind || entry?.kind);
    const skillBinding = binding?.skill || app?.skill;
    const datasrvBinding = binding?.datasrv || app?.datasrv;
    const skillID = String(entry?.skill_id || binding?.appSkill?.id || app?.appSkill?.id || skillBinding?.id || id).trim();
    if (!id || !name || !skillID) return null;
    const launchMode = String(app?.launchMode || defaultLaunchModeForKind(kind)) as AppManifestBinding['launchMode'];
    const skillOutputModes = skillBinding?.outputModes || skillBinding?.output_modes || [];
    const resultContract = normalizeAppResultContract(binding?.resultContract || app?.resultContract || governance?.resultContract || governance?.result_contract, kind, skillOutputModes);
    const testEvidence = governance?.testEvidence || governance?.test_evidence;
    const importedRunEvidence = normalizeImportedRunEvidence(testEvidence ? {
        ...testEvidence,
        runID: testEvidence.runID || testEvidence.runId || testEvidence.run_id,
        at: testEvidence.at || testEvidence.verifiedAt || testEvidence.verified_at,
        appID: id,
    } : undefined);
    return {
        id: isEnterpriseAppKind(kind) ? id : skillPanelAppID(skillID, id),
        name,
        description: String(app?.description || entry?.description || ''),
        category: String(app?.category || entry?.category || (isEnterpriseAppKind(kind) ? 'Enterprise' : 'Skill')),
        kind,
        icon: normalizeSkillAppIcon(app?.icon || entry?.icon),
        customIconDataUrl: normalizeCustomIconDataUrl(app?.customIconDataUrl || app?.custom_icon_data_url || app?.panel?.customIconDataUrl || entry?.custom_icon_data_url || entry?.customIconDataUrl),
        accent: String(app?.panel?.accent || defaultAccentForKind(kind)),
        pinned: !!app?.panel?.pinned,
        version: normalizeAppVersion(app?.version || (raw as any)?.version),
        source: 'skill',
        importedRunEvidence,
        workflowContract: governance?.workflowContract || governance?.workflow_contract,
        aboutInfo: app?.aboutInfo && typeof app.aboutInfo === 'object' ? normalizeAppAboutInfo(app.aboutInfo) : undefined,
        manifest: {
            schema: 'maclaw.app.v1',
            installUnit: raw.installUnit === 'skill' || raw.installUnit === 'mcp' || raw.installUnit === 'builtin' || raw.installUnit === 'enterprise_app_pack' ? raw.installUnit : (isEnterpriseAppKind(kind) ? 'enterprise_app_pack' : 'skill'),
            privateMarker: 'x_maclaw_apps',
            entryKind: kind,
            launchMode,
            datasrv: normalizeAppDataSrv(datasrvBinding),
            mis: normalizeAppMIS(binding?.mis || app?.mis),
            appSkill: binding?.appSkill || app?.appSkill || { id: skillID, version: '1.0.0', source: 'local' },
            dependencies: normalizeAppDependencies(binding?.dependencies || app?.dependencies),
            ui: normalizeAppWorkspaceLayout(binding?.ui || app?.ui, kind),
            resultContract,
            testProtocol: appTestProtocolWithFingerprint(normalizeAppTestProtocol(binding?.testProtocol || app?.testProtocol || governance?.testProtocol || governance?.test_protocol || testEvidence?.testProtocol || testEvidence?.test_protocol, kind, skillOutputModes, resultContract)),
            workflow: normalizeAppWorkflowMapping(binding?.workflow || app?.workflow || governance?.workflow, kind, datasrvBinding?.domain || 'business', datasrvBinding?.objectRole || datasrvBinding?.domain || 'record'),
            skill: skillBinding ? {
                ...skillBinding,
                inputMode: skillBinding.inputMode === 'form' || skillBinding.inputMode === 'mixed' ? skillBinding.inputMode : 'file',
                multipleFiles: !!skillBinding.multipleFiles,
                outputModes: normalizeOutputModes(skillBinding.outputModes),
                fields: normalizeSkillAppFields(skillBinding.fields),
            } : undefined,
        },
    };
}

function skillPanelAppID(skillID: string, appID: string): string {
    return `skill-app-${skillID}-${appID}`;
}

function normalizeSkillAppFields(fields?: SkillAppField[]): SkillAppField[] {
    if (!Array.isArray(fields)) return [];
    const normalized: SkillAppField[] = [];
    for (const field of fields) {
        const name = String(field?.name || '').trim();
        if (!name) continue;
        const type = field.type === 'select' || field.type === 'boolean' ? field.type : 'text';
        const options = Array.isArray(field.options) ? field.options.map((option) => String(option || '').trim()).filter(Boolean) : [];
        const fallbackDefault = type === 'select' && !field.default && options.length > 0 ? options[0] : field.default;
        const defaultText = typeof fallbackDefault === 'boolean' ? '' : String(fallbackDefault || '').trim();
        const selectOptions = type === 'select' && defaultText ? Array.from(new Set([...options, defaultText])) : options;
        normalized.push({
            name,
            label: String(field.label || name),
            type,
            required: !!field.required,
            default: typeof fallbackDefault === 'boolean' ? fallbackDefault : String(fallbackDefault || ''),
            options: selectOptions,
        });
    }
    return normalized;
}

function normalizeOutputModes(outputModes?: string[]) {
    const normalized = (Array.isArray(outputModes) ? outputModes : []).map((item) => String(item || '').trim().toLowerCase()).filter((item) => allowedOutputModes.includes(item));
    return normalized.length > 0 ? Array.from(new Set(normalized)) : ['docx', 'pdf'];
}

function outputModeLabel(mode: string) {
    const labels: Record<string, string> = {
        docx: 'Word / DOCX',
        xlsx: 'Excel / XLSX',
        pdf: 'PDF',
        json: 'JSON',
        txt: 'TXT',
    };
    return labels[mode] || mode.toUpperCase();
}

function appContractTypeLabel(type: string, lang?: string) {
    const normalized = String(type || '').trim();
    const labels: Record<string, { zh: string; en: string }> = {
        approval_result: { zh: '审批结果', en: 'Approval result' },
        business_status: { zh: '业务状态', en: 'Business status' },
        business_record: { zh: '业务记录', en: 'Business record' },
        content: { zh: '内容', en: 'Content' },
        text: { zh: '文本', en: 'Text' },
        artifact: { zh: '产物', en: 'Artifact' },
        artifacts: { zh: '产物', en: 'Artifacts' },
        document: { zh: '文件', en: 'Document' },
        file: { zh: '文件', en: 'File' },
        notification: { zh: '通知', en: 'Notification' },
        workflow_result: { zh: '工作流结果', en: 'Workflow result' },
        approval_instance: { zh: '审批实例', en: 'Approval instance' },
        outputs: { zh: '输出', en: 'Outputs' },
    };
    const label = labels[normalized];
    return label ? label[isZh(lang) ? 'zh' : 'en'] : normalized;
}

function buildSkillFieldPayload(fields: SkillAppField[], values: Record<string, string | boolean>) {
    return fields.reduce<Record<string, string | boolean>>((payload, field) => {
        payload[field.name] = values[field.name] ?? field.default ?? (field.type === 'boolean' ? false : '');
        return payload;
    }, {});
}

/**
 * buildSkillArgsFromMapping constructs the clean runArgs for the skill runner
 * based on the App's paramMapping declaration. Only skill-relevant parameters
 * are included - no App framework metadata leaks into the skill's template vars.
 */
function buildSkillArgsFromMapping(
    paramMapping: Record<string, SkillParamMappingEntry>,
    context: {
        filePath: string;
        filePaths: string[];
        params: string;
        fieldValues: Record<string, string | boolean>;
        fileName: string;
    },
): Record<string, string> {
    const result: Record<string, string> = {};
    for (const [skillParam, mapping] of Object.entries(paramMapping)) {
        const resolved = resolveParamMappingValue(mapping, context);
        if (resolved !== '') {
            result[skillParam] = resolved;
            console.log(`[app-param-mapping] ${skillParam}: source=${mapping.source} -> "${resolved.slice(0, 120)}${resolved.length > 120 ? '...' : ''}"`);
        } else {
            console.warn(`[app-param-mapping] ${skillParam}: source=${mapping.source} -> (empty, skipped)`);
        }
    }
    console.log(`[app-param-mapping] final skill args: [${Object.keys(result).join(', ')}]`);
    return result;
}

function resolveParamMappingValue(
    mapping: SkillParamMappingEntry,
    context: { filePath: string; filePaths: string[]; params: string; fieldValues: Record<string, string | boolean>; fileName: string },
): string {
    const { source, value } = mapping;
    switch (source) {
        case 'file_path':
            return context.filePath;
        case 'file_paths':
            return context.filePaths.filter(Boolean).join(',');
        case 'params':
            return context.params;
        case 'literal':
            return value || '';
        case 'composite':
            return resolveCompositeTemplate(value || '', context);
        default:
            // source: "field:<name>" - extract from form field values
            if (source.startsWith('field:')) {
                const fieldName = source.slice(6);
                return String(context.fieldValues[fieldName] ?? '');
            }
            console.warn(`[app-param-mapping] unknown source type: ${source}`);
            return '';
    }
}

function resolveCompositeTemplate(
    template: string,
    context: { filePath: string; filePaths: string[]; params: string; fieldValues: Record<string, string | boolean>; fileName: string },
): string {
    return template
        .replace(/\{file_path\}/g, context.filePath)
        .replace(/\{file_paths\}/g, context.filePaths.filter(Boolean).join(','))
        .replace(/\{file_name\}/g, context.fileName)
        .replace(/\{params\}/g, context.params)
        .replace(/\{field:([^}]+)\}/g, (_match, fieldName) => String(context.fieldValues[fieldName] ?? ''));
}

function buildToolAppPrompt(app: AppEntry, params: string, fields: Record<string, string | boolean>, outputMode: string, fileName: string) {
    const fieldText = Object.entries(fields).map(([key, value]) => `${key}: ${String(value)}`).join('\n');
    return [
        `Run MaClaw tool app: ${app.name}`,
        app.description ? `Description: ${app.description}` : '',
        fileName ? `Input file: ${fileName}` : '',
        params ? `Parameters:\n${params}` : '',
        fieldText ? `Fields:\n${fieldText}` : '',
        `Expected output mode: ${outputMode}`,
    ].filter(Boolean).join('\n\n');
}

async function readSmallFileText(file: File) {
    if (file.size > 256 * 1024) return '';
    try {
        return await file.text();
    } catch {
        return '';
    }
}

function buildSkillFilePayload(file: File | null) {
    if (!file) return null;
    return {
        name: file.name,
        size: file.size,
        type: file.type || 'application/octet-stream',
        last_modified: file.lastModified,
        transfer: file.size <= 256 * 1024 ? 'inline_text_preview' : 'metadata_only',
    };
}

function arrayBufferToBase64(buffer: ArrayBuffer) {
    const bytes = new Uint8Array(buffer);
    let binary = '';
    const chunkSize = 0x8000;
    for (let index = 0; index < bytes.length; index += chunkSize) {
        binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize));
    }
    return btoa(binary);
}

async function stageSkillAppInputFile(file: File | null) {
    if (!file) return null;
    const content = arrayBufferToBase64(await file.arrayBuffer());
    return await StageSkillAppInputFile(file.name, file.type || 'application/octet-stream', file.lastModified, content) as SkillAppStagedFileRef;
}

function normalizeSkillRunLifecycle(status?: string): 'running' | 'done' | 'error' | 'cancelled' | 'timeout' {
    const value = String(status || '').trim().toLowerCase();
    if (['success', 'succeeded', 'completed', 'complete', 'done'].includes(value)) return 'done';
    if (['timeout', 'timed_out', 'timed-out'].includes(value)) return 'timeout';
    if (['failed', 'failure', 'error'].includes(value)) return 'error';
    if (['cancelled', 'canceled'].includes(value)) return 'cancelled';
    return 'running';
}

function skillRunErrorMessage(status?: SkillRunStatusView | null) {
	return String(status?.error || status?.summary?.last_error_snippet || '').trim();
}

function firstRuntimeErrorObject(value: unknown): Record<string, any> | null {
	if (!value) return null;
	if (typeof value === 'object') {
		const record = value as Record<string, any>;
		if (record.code || record.next_actions || record.nextActions) return record;
		for (const key of ['response', 'data', 'detail', 'cause']) {
			const nested = firstRuntimeErrorObject(record[key]);
			if (nested) return nested;
		}
		if (typeof record.message === 'string') {
			const nested = firstRuntimeErrorObject(record.message);
			if (nested) return nested;
		}
		if (record.message) return record;
		return null;
	}
	if (typeof value !== 'string') return null;
	const text = value.trim();
	if (!text) return null;
	const candidates = [text];
	const firstBrace = text.indexOf('{');
	const lastBrace = text.lastIndexOf('}');
	if (firstBrace >= 0 && lastBrace > firstBrace) candidates.push(text.slice(firstBrace, lastBrace + 1));
	for (const candidate of candidates) {
		try {
			const parsed = JSON.parse(candidate);
			if (parsed && typeof parsed === 'object') return parsed as Record<string, any>;
		} catch {
			// Continue probing; Wails errors often wrap JSON with a short prefix.
		}
	}
	return null;
}

function structuredBusinessErrorFromUnknown(error: unknown): StructuredBusinessErrorView | null {
	const raw = firstRuntimeErrorObject(error);
	if (!raw) return null;
	const code = String(raw.code || '').trim();
	const message = String(raw.message || raw.error || '').trim();
	const nextActionValues = Array.isArray(raw.next_actions) ? raw.next_actions : Array.isArray(raw.nextActions) ? raw.nextActions : [];
	const nextActions = nextActionValues.map((item: any) => ({
		label: String(item?.label || '').trim() || undefined,
		action: String(item?.action || '').trim(),
		args: item?.args && typeof item.args === 'object' ? item.args as Record<string, unknown> : undefined,
	})).filter((item: StructuredBusinessErrorActionView) => item.action);
	if (!code && !message && nextActions.length === 0) return null;
	return {
		code,
		message,
		actor: String(raw.actor || '').trim() || undefined,
		target: String(raw.target || '').trim() || undefined,
		required: String(raw.required || '').trim() || undefined,
		actual: String(raw.actual || '').trim() || undefined,
		nextActions,
		metadata: raw.metadata && typeof raw.metadata === 'object' ? raw.metadata as Record<string, unknown> : undefined,
	};
}

function structuredBusinessErrorMessage(error: StructuredBusinessErrorView | null, fallback: string) {
	if (!error) return fallback;
	return error.message || error.code || fallback;
}

function skillRunOutputSuffix(status?: SkillRunStatusView | null) {
	const snippet = String(status?.summary?.last_output_snippet || status?.session_progress?.last_result || '').trim();
	if (!snippet) return '';
	return ` · ${snippet.slice(0, 120)}`;
}

function skillRunArtifactKeys(artifact?: SkillRunArtifactView | null) {
    return [artifact?.id, artifact?.uri, artifact?.path, artifact?.remote_url, artifact?.name]
        .map((value) => String(value || '').trim())
        .filter(Boolean);
}

function skillRunArtifacts(status?: SkillRunStatusView | null): SkillRunArtifactView[] {
    const seen = new Set<string>();
    const artifacts: SkillRunArtifactView[] = [];
    const add = (artifact?: SkillRunArtifactView | null) => {
        const keys = skillRunArtifactKeys(artifact);
        if (!artifact || keys.length === 0 || keys.some((key) => seen.has(key))) return;
        keys.forEach((key) => seen.add(key));
        artifacts.push(artifact);
    };
    for (const artifact of status?.artifacts || []) add(artifact);
    for (const artifact of status?.summary?.artifacts || []) add(artifact);
    for (const block of skillRunOutputBlocks(status)) add(block.artifact);
    const artifactPath = String(status?.summary?.artifact_path || '').trim();
    if (artifactPath) add({ path: artifactPath, status: status?.summary?.artifact_status });
    return artifacts;
}

function skillRunPrimaryArtifact(status?: SkillRunStatusView | null): SkillRunArtifactView | null {
    return skillRunArtifacts(status)[0] || null;
}

function skillRunPrimaryArtifactPath(status?: SkillRunStatusView | null) {
    return String(skillRunPrimaryArtifact(status)?.path || '').trim();
}

function skillRunOutputBlocks(status?: SkillRunStatusView | null): SkillRunOutputBlockView[] {
    const seen = new Set<string>();
    const blocks: SkillRunOutputBlockView[] = [];
    for (const block of [...(status?.outputs || []), ...(status?.summary?.output_blocks || [])]) {
        const key = String(block?.id || `${block?.kind || ''}:${block?.title || ''}:${block?.text || ''}:${block?.artifact_id || ''}`).trim();
        if (!key || seen.has(key)) continue;
        seen.add(key);
        blocks.push(block);
    }
    return blocks;
}

function skillRunApprovalObjects(status?: SkillRunStatusView | null): Record<string, any>[] {
    const raw = status as any;
    const blocks = skillRunOutputBlocks(status);
    const values: unknown[] = [
        raw,
        raw?.summary,
        raw?.session_progress,
        raw?.result,
        raw?.output,
        raw?.approval_result,
        raw?.approval,
        raw?.summary?.last_output_snippet,
        raw?.summary?.last_error_snippet,
        raw?.session_progress?.last_result,
        raw?.session_progress?.progress_summary,
        ...(Array.isArray(raw?.outputs) ? raw.outputs : []),
        ...blocks,
        ...blocks.map((block) => block.text),
        ...blocks.map((block) => block.artifact),
    ];
    return values.flatMap((value) => parseSkillRunResultObjects(value));
}

function parseSkillRunResultObjects(value: unknown): Record<string, any>[] {
    if (!value) return [];
    if (Array.isArray(value)) return value.flatMap((item) => parseSkillRunResultObjects(item));
    if (typeof value === 'object') return [value as Record<string, any>];
    const text = String(value).trim();
    if (!text || (!text.startsWith('{') && !text.startsWith('['))) return [];
    try {
        return parseSkillRunResultObjects(JSON.parse(text));
    } catch {
        return [];
    }
}

function expandSkillRunApprovalObjects(objects: Record<string, any>[]): Record<string, any>[] {
    const out: Record<string, any>[] = [];
    const seen = new Set<Record<string, any>>();
    const add = (object: Record<string, any> | undefined | null) => {
        if (!object || typeof object !== 'object' || Array.isArray(object) || seen.has(object)) return;
        seen.add(object);
        out.push(object);
        for (const key of ['result_payload', 'resultPayload', 'approval_instance', 'approvalInstance', 'approval', 'record_ref', 'recordRef', 'business_payload', 'businessPayload']) {
            const child = object[key];
            if (child && typeof child === 'object' && !Array.isArray(child)) add(child as Record<string, any>);
        }
    };
    objects.forEach(add);
    return out;
}

function normalizeSkillRunResultStringList(value: unknown): string[] {
    if (value === undefined || value === null) return [];
    if (Array.isArray(value)) {
        return Array.from(new Set(value.flatMap((item) => normalizeSkillRunResultStringList(item)).filter(Boolean)));
    }
    if (typeof value === 'object') {
        const record = value as Record<string, unknown>;
        return normalizeSkillRunResultStringList(record.id || record.node_id || record.nodeId || record.name || record.key || record.value);
    }
    const text = String(value || '').trim();
    if (!text) return [];
    if (text.startsWith('[') || text.startsWith('{')) {
        try { return normalizeSkillRunResultStringList(JSON.parse(text)); } catch { /* fall through to delimiter parsing */ }
    }
    return Array.from(new Set(text.split(/\s*(?:->|→|,|;|>|\||\n)\s*/).map((item) => item.trim()).filter(Boolean)));
}

function skillRunWorkflowNodeIDs(objects: Record<string, any>[], currentNode: string): string[] | undefined {
    const keys = ['current_node_ids', 'currentNodeIDs', 'currentNodeIds', 'workflow_node_ids', 'workflowNodeIDs', 'workflowNodeIds', 'workflow_nodes', 'workflowNodes', 'node_ids', 'nodeIds', 'nodes'];
    const out: string[] = [];
    const seen = new Set<string>();
    const add = (value: string) => {
        const text = value.trim();
        if (!text || seen.has(text)) return;
        seen.add(text);
        out.push(text);
    };
    for (const object of objects) {
        for (const key of keys) normalizeSkillRunResultStringList(object?.[key]).forEach(add);
    }
    if (currentNode) add(currentNode);
    return out.length > 0 ? out : undefined;
}
function mergeApprovalWorkflowNodeIDs(...values: unknown[]): string[] | undefined {
    const out: string[] = [];
    const seen = new Set<string>();
    values.flatMap((value) => normalizeSkillRunResultStringList(value)).forEach((value) => {
        const text = value.trim();
        if (!text || seen.has(text)) return;
        seen.add(text);
        out.push(text);
    });
    return out.length > 0 ? out : undefined;
}

function firstSkillRunResultString(objects: Record<string, any>[], keys: string[]) {
    for (const object of objects) {
        for (const key of keys) {
            const value = object?.[key];
            if (value === undefined || value === null) continue;
            if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
                const text = String(value).trim();
                if (!text) continue;
                if ((text.startsWith('{') || text.startsWith('[')) && parseSkillRunResultObjects(text).length > 0) continue;
                return text;
            }
        }
    }
    return '';
}

const approvalResultPayloadKeyAliases: Record<string, string> = {
    approval_result: 'approval_result', approvalResult: 'approval_result', approval_status: 'approval_result', approvalStatus: 'approval_result', approval_decision: 'approval_result', approvalDecision: 'approval_result', decision: 'approval_result',
    business_status: 'business_status', businessStatus: 'business_status', result_status: 'result_status', resultStatus: 'result_status',
    business_record: 'business_record', businessRecord: 'business_record', business_record_id: 'record_id', businessRecordID: 'record_id', record_id: 'record_id', recordID: 'record_id',
    detail_url: 'detail_url', detailURL: 'detail_url', workflow_url: 'detail_url', workflowURL: 'detail_url', url: 'detail_url',
    result: 'text', text: 'text', content: 'text', message: 'text', result_message: 'text', resultMessage: 'text', approval_message: 'text', approvalMessage: 'text',
    table: 'table', dashboard: 'dashboard', notification: 'notification', external_receipt: 'external_receipt', externalReceipt: 'external_receipt', requires_input: 'requires_input', requiresInput: 'requires_input', error: 'error', artifact: 'artifact',
};

function isApprovalRecordValue(value: unknown): value is Record<string, unknown> {
    return !!value && typeof value === 'object' && !Array.isArray(value);
}

function compactApprovalResultValue(value: unknown): unknown {
    if (value === undefined || value === null) return undefined;
    if (typeof value === 'string') return value.trim();
    if (typeof value === 'number' || typeof value === 'boolean') return value;
    if (Array.isArray(value)) {
        const items = value.map((item) => compactApprovalResultValue(item)).filter((item) => item !== undefined && item !== '');
        return items.length > 0 ? items.slice(0, 80) : undefined;
    }
    if (typeof value === 'object') {
        const out: Record<string, unknown> = {};
        for (const [key, child] of Object.entries(value as Record<string, unknown>).slice(0, 80)) {
            const compact = compactApprovalResultValue(child);
            if (compact !== undefined && compact !== '') out[key] = compact;
        }
        return Object.keys(out).length > 0 ? out : undefined;
    }
    return undefined;
}

function compactApprovalRecord(value: unknown): Record<string, unknown> | undefined {
    const compact = compactApprovalResultValue(value);
    if (isApprovalRecordValue(compact)) return compact;
    if (Array.isArray(compact)) return { rows: compact };
    if (compact !== undefined && compact !== '') return { value: compact };
    return undefined;
}

function mergeApprovalPayloadValue(payload: Record<string, unknown>, key: string, value: unknown) {
    const compact = compactApprovalResultValue(value);
    if (compact === undefined || compact === '') return;
    if (payload[key] === undefined) payload[key] = compact;
}

function approvalWorkflowResultPayloadFromObjects(objects: Record<string, any>[], status?: SkillRunStatusView | null): Record<string, unknown> | undefined {
    const payload: Record<string, unknown> = {};
    for (const object of expandSkillRunApprovalObjects(objects)) {
        const directPayload = object?.result_payload || object?.resultPayload;
        if (isApprovalRecordValue(directPayload)) {
            for (const [key, value] of Object.entries(directPayload)) mergeApprovalPayloadValue(payload, key, value);
        }
        for (const [sourceKey, targetKey] of Object.entries(approvalResultPayloadKeyAliases)) {
            if (Object.prototype.hasOwnProperty.call(object, sourceKey)) mergeApprovalPayloadValue(payload, targetKey, object[sourceKey]);
        }
    }
    const snippet = skillRunOutputSuffix(status).replace(/^ · /, '');
    if (payload.text === undefined && snippet && parseSkillRunResultObjects(snippet).length === 0) payload.text = snippet.slice(0, 500);
    return Object.keys(payload).length > 0 ? payload : undefined;
}

function normalizeApprovalArtifact(artifact?: ApprovalInstanceArtifactView | null): ApprovalInstanceArtifactView | null {
    if (!artifact || typeof artifact !== 'object') return null;
    const raw = artifact as Record<string, unknown>;
    const size = Number(raw.size_bytes);
    const out: ApprovalInstanceArtifactView = { id: String(raw.id || '').trim() || undefined, uri: String(raw.uri || '').trim() || undefined, name: String(raw.name || '').trim() || undefined, path: String(raw.path || '').trim() || undefined, mime_type: String(raw.mime_type || '').trim() || undefined, size_bytes: Number.isFinite(size) ? size : undefined, remote_url: String(raw.remote_url || '').trim() || undefined, checksum: String(raw.checksum || '').trim() || undefined, download_state: String(raw.download_state || '').trim() || undefined, status: String(raw.status || '').trim() || undefined, presentation: String(raw.presentation || '').trim() || undefined };
    return Object.values(out).some((value) => value !== undefined && value !== '') ? out : null;
}

function approvalArtifactKey(artifact: ApprovalInstanceArtifactView) {
    return String(artifact.id || artifact.uri || artifact.path || artifact.remote_url || artifact.name || '').trim();
}

function normalizeApprovalArtifacts(artifacts?: Array<ApprovalInstanceArtifactView | null | undefined> | null): ApprovalInstanceArtifactView[] | undefined {
    if (!Array.isArray(artifacts)) return undefined;
    const seen = new Set<string>();
    const out: ApprovalInstanceArtifactView[] = [];
    for (const item of artifacts) {
        const artifact = normalizeApprovalArtifact(item);
        if (!artifact) continue;
        const key = approvalArtifactKey(artifact);
        if (!key || seen.has(key)) continue;
        seen.add(key);
        out.push(artifact);
    }
    return out.length > 0 ? out : undefined;
}

function approvalWorkflowArtifactsFromStatus(status?: SkillRunStatusView | null): ApprovalInstanceArtifactView[] | undefined {
    const objects = expandSkillRunApprovalObjects(skillRunApprovalObjects(status));
    const resultArtifacts = objects.flatMap((object) => {
        const artifacts = Array.isArray(object?.artifacts) ? object.artifacts : [];
        const artifact = object?.artifact ? [object.artifact] : [];
        return [...artifacts, ...artifact];
    });
    const items = [...(Array.isArray(status?.artifacts) ? status?.artifacts || [] : []), ...(Array.isArray(status?.summary?.artifacts) ? status?.summary?.artifacts || [] : []), ...skillRunOutputBlocks(status).map((block) => block.artifact), ...resultArtifacts, skillRunPrimaryArtifact(status)];
    return normalizeApprovalArtifacts(items);
}

function approvalOutputDataFromBlock(block: SkillRunOutputBlockView | ApprovalInstanceOutputView): Record<string, unknown> | undefined {
    const raw = block as Record<string, unknown>;
    const explicit = compactApprovalRecord(raw.data || raw.result_payload || raw.resultPayload || raw.payload);
    if (explicit) return explicit;
    const text = skillRunOutputBlockText(block as SkillRunOutputBlockView);
    if (!text || (!text.startsWith('{') && !text.startsWith('['))) return undefined;
    try { return compactApprovalRecord(JSON.parse(text)); } catch { return undefined; }
}

function normalizeApprovalOutput(block?: SkillRunOutputBlockView | ApprovalInstanceOutputView | null): ApprovalInstanceOutputView | null {
    if (!block || typeof block !== 'object') return null;
    const raw = block as Record<string, unknown>;
    const type = String(raw.type || raw.kind || '').trim();
    const kind = String(raw.kind || raw.type || '').trim();
    const title = String(raw.title || '').trim();
    const text = String(raw.text || '').trim();
    const status = String(raw.status || '').trim();
    const artifact = normalizeApprovalArtifact(raw.artifact as ApprovalInstanceArtifactView | undefined) || undefined;
    const artifactID = String(raw.artifact_id || artifact?.id || artifact?.uri || '').trim();
    const data = approvalOutputDataFromBlock(block);
    if (!type && !kind && !title && !text && !status && !artifactID && !artifact && !data) return null;
    return { type: type || kind || undefined, kind: kind || type || undefined, title: title || undefined, text: text || undefined, status: status || undefined, artifact_id: artifactID || undefined, artifact, data };
}

function normalizeApprovalOutputs(outputs?: Array<SkillRunOutputBlockView | ApprovalInstanceOutputView | null | undefined> | null): ApprovalInstanceOutputView[] | undefined {
    if (!Array.isArray(outputs)) return undefined;
    const seen = new Set<string>();
    const out: ApprovalInstanceOutputView[] = [];
    for (const item of outputs) {
        const output = normalizeApprovalOutput(item);
        if (!output) continue;
        const key = String(output.artifact_id || `${output.type || ''}:${output.title || ''}:${output.text || ''}:${JSON.stringify(output.data || '')}`).trim();
        if (!key || seen.has(key)) continue;
        seen.add(key);
        out.push(output);
    }
    return out.length > 0 ? out : undefined;
}

function approvalWorkflowOutputsFromStatus(status?: SkillRunStatusView | null): ApprovalInstanceOutputView[] | undefined {
    const objects = expandSkillRunApprovalObjects(skillRunApprovalObjects(status));
    const resultOutputs = objects.flatMap((object) => Array.isArray(object?.outputs) ? object.outputs : []);
    return normalizeApprovalOutputs([...skillRunOutputBlocks(status), ...resultOutputs]);
}

function approvalResultOutput(
    decision: 'approved' | 'rejected' | 'attention',
    resultText: string,
    businessStatus: string,
    resultStatus: string,
    resultPayload?: Record<string, unknown>,
    title = 'Approval result',
): ApprovalInstanceOutputView {
    return {
        type: 'content',
        kind: 'approval_result',
        title,
        text: resultText,
        status: resultStatus || decision,
        data: {
            ...(resultPayload || {}),
            approval_result: decision,
            business_status: businessStatus || decision,
            result_status: resultStatus || decision,
        },
    };
}

function approvalOutputsOrDefault(
    outputs: ApprovalInstanceOutputView[] | undefined,
    decision: 'approved' | 'rejected' | 'attention',
    resultText: string,
    businessStatus: string,
    resultStatus: string,
    resultPayload?: Record<string, unknown>,
    title = 'Approval result',
): ApprovalInstanceOutputView[] {
    const normalized = normalizeApprovalOutputs(outputs || []);
    return normalized?.length ? normalized : [approvalResultOutput(decision, resultText, businessStatus, resultStatus, resultPayload, title)];
}

function appRunResultPayloadFromStatus(status?: SkillRunStatusView | null): Record<string, unknown> | undefined {
    const payload = approvalWorkflowResultPayloadFromObjects(skillRunApprovalObjects(status), status);
    if (payload) return payload;
    const snippet = skillRunOutputSuffix(status).replace(/^ · /, '');
    return snippet ? { text: snippet.slice(0, 500) } : undefined;
}

function appRunPrimaryResultFromPayload(contract: AppResultContract, payload?: Record<string, unknown>, outputs?: ApprovalInstanceOutputView[]) {
    const primary = String(contract.primary || '').trim();
    const value = primary ? payload?.[primary] : undefined;
    const formatted = formatApprovalResultValue(value).trim();
    if (formatted) return formatted;
    const textValue = formatApprovalResultValue(payload?.text || payload?.content || payload?.message).trim();
    if (textValue) return textValue;
    const output = outputs?.find((item) => approvalOutputBody(item).trim());
    return output ? approvalOutputBody(output).trim() : '';
}

function formatApprovalResultValue(value: unknown): string {
    if (value === undefined || value === null || value === '') return '';
    if (typeof value === 'string') return value;
    if (typeof value === 'number' || typeof value === 'boolean') return String(value);
    try { return JSON.stringify(value, null, 2); } catch { return String(value); }
}

function approvalOutputKind(output: ApprovalInstanceOutputView) {
    return String(output.type || output.kind || 'text').trim().toLowerCase() || 'text';
}

function approvalOutputTitle(output: ApprovalInstanceOutputView, text: typeof labels.zh) {
    return String(output.title || output.type || output.kind || text.outputContent).trim();
}

function approvalOutputBody(output: ApprovalInstanceOutputView) {
    return String(output.text || '').trim() || formatApprovalResultValue(output.data);
}

function approvalArtifactReference(artifact: ApprovalInstanceArtifactView) {
    return String(artifact.uri || artifact.path || artifact.remote_url || artifact.name || artifact.id || '').trim();
}

function approvalResultPayloadText(payload: Record<string, unknown> | undefined, keys: string[]) {
    for (const key of keys) {
        const value = formatApprovalResultValue(payload?.[key]).trim();
        if (value) return value;
    }
    return '';
}

function approvalResultFeedbackSummary(instance: ApprovalInstanceView | undefined, lang?: string) {
    if (!instance) return null;
    const payload = instance.resultPayload;
    const decision = approvalResultPayloadText(payload, ['approval_result', 'approvalStatus', 'approval_status', 'approval_decision', 'decision'])
        || instance.result
        || approvalStatusLabel(instance.status, lang);
    const businessStatus = approvalResultPayloadText(payload, ['business_status', 'businessStatus'])
        || instance.businessStatus
        || instance.toStatus
        || '-';
    const resultStatus = approvalResultPayloadText(payload, ['result_status', 'resultStatus', 'status'])
        || instance.resultStatus
        || approvalStatusLabel(instance.status, lang);
    const content = approvalResultPayloadText(payload, ['text', 'content', 'message', 'result_message', 'approval_message', 'summary'])
        || approvalOutputBody((instance.outputs || []).find((output) => approvalOutputBody(output).trim()) || ({} as ApprovalInstanceOutputView));
    const primaryArtifact = (instance.artifacts || [])[0]
        || (instance.outputs || []).map((output) => output.artifact).find(Boolean);
    return {
        decision,
        businessStatus,
        resultStatus,
        content,
        outputCount: instance.outputs?.length || 0,
        artifactCount: instance.artifacts?.length || 0,
        primaryArtifact: primaryArtifact ? (primaryArtifact.name || approvalArtifactReference(primaryArtifact) || primaryArtifact.id || '') : '',
    };
}
function normalizeApprovalWorkflowDecision(value: string, lifecycle: 'done' | 'error' | 'cancelled' | 'timeout'): 'approved' | 'rejected' | 'attention' {
    if (lifecycle !== 'done') return 'attention';
    const text = value.trim().toLowerCase();
    if (!text) return 'approved';
    if (text.includes('not approved') || text.includes('reject') || text.includes('denied') || text.includes('deny') || text.includes('refuse') || text.includes('\u9a73\u56de') || text.includes('\u62d2\u7edd')) return 'rejected';
    if (text.includes('attention') || text.includes('needs_attention') || text.includes('needs review') || text.includes('review_only') || text.includes('warning') || text.includes('\u5173\u6ce8') || text.includes('\u67e5\u770b')) return 'attention';
    if (text.includes('approve') || text.includes('accepted') || text.includes('pass') || text.includes('success') || text.includes('complete') || text.includes('done') || text.includes('\u901a\u8fc7') || text.includes('\u540c\u610f') || text.includes('\u6279\u51c6')) return 'approved';
    return 'approved';
}

function approvalWorkflowProgressFromSkillRunStatus(status: SkillRunStatusView | null, lang?: string) {
    const zh = isZh(lang);
    const objects = expandSkillRunApprovalObjects(skillRunApprovalObjects(status));
    const currentNode = firstSkillRunResultString(objects, ['current_node', 'currentNode', 'approval_node', 'approvalNode', 'node']) || (zh ? '\u8fdb\u884c\u4e2d' : 'In progress');
    const currentNodeIDs = skillRunWorkflowNodeIDs(objects, currentNode);
    const progressText = firstSkillRunResultString(objects, ['progress', 'progress_summary', 'progressSummary', 'message', 'text', 'result'])
        || String(status?.session_progress?.progress_summary || status?.summary?.current_step_status || status?.summary?.last_completed_step || '').trim()
        || (currentNode ? (zh ? `\u6d41\u7a0b\u63a8\u8fdb\u5230 ${currentNode}` : `Workflow moved to ${currentNode}`) : '');
    const businessStatus = firstSkillRunResultString(objects, ['business_status', 'businessStatus', 'to_status', 'toStatus']) || 'approval_running';
    const resultStatus = firstSkillRunResultString(objects, ['result_status', 'resultStatus']) || 'running';
    if (!currentNode && !progressText && (!currentNodeIDs || currentNodeIDs.length === 0)) return null;
    return { currentNode, currentNodeIDs, progressText, businessStatus, resultStatus };
}
function approvalWorkflowResultFromSkillRunStatus(status: SkillRunStatusView | null, lifecycle: 'done' | 'error' | 'cancelled' | 'timeout', lang?: string): ApprovalWorkflowCompletion {
    const zh = isZh(lang);
    const objects = expandSkillRunApprovalObjects(skillRunApprovalObjects(status));
    const explicitDecision = firstSkillRunResultString(objects, ['approval_result', 'approvalResult', 'approval_status', 'approvalStatus', 'approval_decision', 'approvalDecision', 'decision']);
    const decision = normalizeApprovalWorkflowDecision(explicitDecision, lifecycle);
    const outputText = skillRunOutputSuffix(status).replace(/^ · /, '');
    const fallbackText = lifecycle === 'cancelled'
        ? (zh ? 'Skill \u5df2\u53d6\u6d88' : 'Skill cancelled')
        : lifecycle === 'timeout'
            ? (skillRunErrorMessage(status) || (zh ? '\u5de5\u4f5c\u6d41\u8fd0\u884c\u8d85\u65f6' : 'Workflow timed out'))
            : lifecycle === 'error'
                ? (skillRunErrorMessage(status) || (zh ? '\u5de5\u4f5c\u6d41\u8fd0\u884c\u5f02\u5e38\uff0c\u9700\u5173\u6ce8' : 'Workflow failed and needs attention'))
                : decision === 'approved'
                    ? (zh ? '\u5ba1\u6279\u5df2\u901a\u8fc7' : 'Approval approved')
                    : decision === 'rejected'
                        ? (zh ? '\u5ba1\u6279\u5df2\u9a73\u56de' : 'Approval rejected')
                        : (zh ? '\u9700\u5173\u6ce8' : 'Needs attention');
    const resultText = firstSkillRunResultString(objects, ['message', 'result_message', 'resultMessage', 'approval_message', 'approvalMessage', 'content', 'text', 'result']) || outputText || fallbackText;
    const resultStatus = firstSkillRunResultString(objects, ['result_status', 'resultStatus']) || (lifecycle === 'cancelled' ? 'cancelled' : lifecycle === 'timeout' ? 'timeout' : lifecycle === 'error' ? 'workflow_error' : decision);
    const businessStatus = firstSkillRunResultString(objects, ['business_status', 'businessStatus']) || resultStatus;
    const currentNode = firstSkillRunResultString(objects, ['current_node', 'currentNode', 'approval_node', 'approvalNode', 'node']) || (lifecycle === 'cancelled'
        ? (zh ? '\u5df2\u53d6\u6d88' : 'Cancelled')
        : lifecycle === 'timeout'
            ? (zh ? '\u5df2\u8d85\u65f6' : 'Timed out')
            : lifecycle === 'error'
                ? (zh ? '\u8fd0\u884c\u5f02\u5e38' : 'Workflow error')
                : decision === 'attention'
                    ? (zh ? '\u9700\u5173\u6ce8' : 'Needs attention')
                    : (zh ? '\u5df2\u5b8c\u6210' : 'Completed'));
    const currentNodeIDs = skillRunWorkflowNodeIDs(objects, currentNode);
    const resultPayload = {
        approval_result: decision,
        business_status: businessStatus,
        result_status: resultStatus,
        text: resultText,
        workflow_lifecycle: lifecycle,
        ...(approvalWorkflowResultPayloadFromObjects(objects, status) || {}),
    };
    const artifacts = approvalWorkflowArtifactsFromStatus(status);
    const outputs = approvalOutputsOrDefault(
        approvalWorkflowOutputsFromStatus(status),
        decision,
        resultText,
        businessStatus,
        resultStatus,
        resultPayload,
        zh ? '\u5ba1\u6279\u7ed3\u679c' : 'Approval result',
    );
    return {
        status: lifecycle === 'cancelled' ? 'cancelled' : lifecycle === 'timeout' ? 'timeout' : decision,
        lane: lifecycle === 'cancelled' || lifecycle === 'timeout' ? 'handled' : decision === 'attention' ? 'attention' : 'handled',
        currentNode,
        resultText,
        businessStatus,
        resultStatus,
        detailURL: firstSkillRunResultString(objects, ['detail_url', 'detailURL', 'workflow_url', 'workflowURL', 'url']) || undefined,
        recordID: firstSkillRunResultString(objects, ['record_id', 'recordID', 'business_record_id', 'businessRecordID']) || approvalPayloadRecordID(resultPayload) || undefined,
        resultPayload,
        outputs,
        artifacts,
        currentNodeIDs,
        eventAction: lifecycle === 'cancelled' ? 'workflow_cancelled' : lifecycle === 'timeout' ? 'workflow_timeout' : lifecycle === 'error' ? 'workflow_failed' : 'workflow_completed',
    };
}

function skillRunProgressMessage(status: SkillRunStatusView | null, fallback: string, runID: string) {
    const progress = String(status?.session_progress?.progress_summary || status?.session_progress?.current_task || status?.summary?.current_step || '').trim();
    const statusText = String(status?.status || '').trim();
    return [fallback, runID, statusText, progress].filter(Boolean).join(' · ');
}

function compactStepLabel(step: SkillRunStepView) {
    return String(step.name || step.action || (step.index !== undefined ? `Step ${step.index + 1}` : 'Step')).trim();
}

function compactStepDetail(step: SkillRunStepView) {
    return String(step.error || step.output || '').trim().slice(0, 120);
}

function skillRunOutputBlockText(block: SkillRunOutputBlockView) {
    return String(block.text || '').trim();
}

/** Format bytes to human-readable size (KB/MB/GB). */
function formatFileSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

/** Return a file-type icon based on extension or mime type. */
function fileTypeIcon(name: string, mimeType?: string): string {
    const ext = (name.split('.').pop() || '').toLowerCase();
    const mime = (mimeType || '').toLowerCase();
    // Documents
    if (ext === 'pdf' || mime.includes('pdf')) return 'PDF';
    if (['doc', 'docx'].includes(ext) || mime.includes('wordprocessing') || mime.includes('msword')) return 'DOC';
    if (['xls', 'xlsx'].includes(ext) || mime.includes('spreadsheet') || mime.includes('excel')) return 'XLS';
    if (['ppt', 'pptx'].includes(ext) || mime.includes('presentation') || mime.includes('powerpoint')) return 'PPT';
    if (['md', 'txt', 'rtf', 'csv'].includes(ext) || mime.includes('text/')) return 'TXT';
    // Images
    if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp', 'bmp', 'ico'].includes(ext) || mime.startsWith('image/')) return 'IMG';
    // Audio/Video
    if (['mp3', 'wav', 'ogg', 'flac', 'aac', 'm4a'].includes(ext) || mime.startsWith('audio/')) return 'AUD';
    if (['mp4', 'avi', 'mkv', 'mov', 'webm'].includes(ext) || mime.startsWith('video/')) return 'VID';
    // Archives
    if (['zip', 'tar', 'gz', '7z', 'rar', 'bz2'].includes(ext) || mime.includes('archive') || mime.includes('zip')) return 'ZIP';
    // Code/Data
    if (['json', 'xml', 'yaml', 'yml', 'toml'].includes(ext)) return 'DATA';
    if (['py', 'js', 'ts', 'go', 'rs', 'java', 'cpp', 'c', 'h', 'cs', 'rb', 'sh'].includes(ext)) return 'CODE';
    if (['html', 'htm', 'css'].includes(ext)) return 'WEB';
    // Default
    return 'FILE';
}

/** Render output text with file/folder paths as clickable links that reveal in explorer. */
function renderOutputTextWithPaths(text: string): (string | JSX.Element)[] {
    // Match Windows paths: drive letter + colon + backslash OR forward-slash, followed by path chars.
    // Match Unix absolute paths: common top-level directories, only when preceded by whitespace/start
    // (prevents matching /home inside URLs like https://example.com/home/...).
    const pathPattern = /([A-Z]:[\\\/][^\s<>"'`\n]+|(?<=^|[\s"'`({\[,;=:])\/(?:home|tmp|var|opt|usr|etc|root|mnt|data|srv|proc|sys|run|media)[^\s<>"'`\n]*)/gi;
    const parts: (string | JSX.Element)[] = [];
    let lastIndex = 0;
    let match: RegExpExecArray | null;
    let keyIndex = 0;
    while ((match = pathPattern.exec(text)) !== null) {
        const rawMatch = match[0];
        if (match.index > lastIndex) {
            parts.push(text.slice(lastIndex, match.index));
        }
        const pathStr = rawMatch.replace(/[,;)}\]:.]+$/, ''); // trim trailing punctuation
        if (!pathStr) {
            // Safety: if trimming removes everything, emit raw text and advance
            parts.push(rawMatch);
            lastIndex = match.index + rawMatch.length;
            continue;
        }
        parts.push(
            <a
                key={`path-${keyIndex++}`}
                className="apps-output-path-link"
                href="#"
                onClick={(e) => { e.preventDefault(); void ShowItemInFolder(pathStr); }}
                title={pathStr}
            >{pathStr}</a>
        );
        lastIndex = match.index + pathStr.length;
        // Adjust regex lastIndex in case we trimmed trailing chars
        pathPattern.lastIndex = lastIndex;
    }
    if (lastIndex < text.length) {
        parts.push(text.slice(lastIndex));
    }
    return parts.length > 0 ? parts : [text];
}

function businessOperationRows(value: unknown): Record<string, unknown>[] {
    if (Array.isArray(value)) return value.filter((item): item is Record<string, unknown> => !!item && typeof item === 'object' && !Array.isArray(item));
    if (!value || typeof value !== 'object') return [];
    const record = value as Record<string, unknown>;
    for (const key of ['rows', 'records', 'items', 'data', 'results', 'cards', 'widgets', 'charts']) {
        const candidate = record[key];
        if (Array.isArray(candidate)) return businessOperationRows(candidate);
    }
    const nestedRecord = record.record || record.business_record || record.businessRecord;
    if (nestedRecord && typeof nestedRecord === 'object' && !Array.isArray(nestedRecord)) return [nestedRecord as Record<string, unknown>];
    return Object.keys(record).length > 0 ? [record] : [];
}

function businessOperationColumns(rows: Record<string, unknown>[]): string[] {
    const columns: string[] = [];
    for (const row of rows) {
        for (const key of Object.keys(row)) {
            if (!columns.includes(key)) columns.push(key);
            if (columns.length >= 6) return columns;
        }
    }
    return columns;
}

function businessOperationResultKind(mode: string, primaryResult?: string, resultPayload?: Record<string, unknown>, response?: Record<string, unknown>): BusinessOperationResultKind {
    const primary = String(primaryResult || '').trim().toLowerCase();
    const status = String(resultPayload?.result_status || resultPayload?.status || resultPayload?.state || response?.result_status || response?.status || response?.state || '').trim().toLowerCase();
    if (['error', 'failed', 'failure', 'denied'].includes(status)) return 'error';
    if (['business_record', 'record'].includes(primary)) return 'business_record';
    if (['records', 'rows', 'table', 'report'].includes(primary)) return 'table';
    if (['dashboard', 'cards', 'widgets', 'charts'].includes(primary)) return 'dashboard';
    if (['content', 'text', 'message'].includes(primary)) return 'text';
    if (['external_receipt', 'receipt'].includes(primary)) return 'external_receipt';
    const view = resultPayload || response;
    if (mode === 'business_dashboard' || Array.isArray(view?.cards) || Array.isArray(view?.widgets) || Array.isArray(view?.charts)) return 'dashboard';
    if (mode === 'business_view' || mode === 'business_report' || Array.isArray(view?.rows) || Array.isArray(view?.records) || Array.isArray(view?.items) || Array.isArray(view?.results)) return 'table';
    if (view?.record_id || view?.record || view?.business_record || view?.businessRecord) return 'business_record';
    if (view?.receipt || view?.external_receipt || view?.externalReceipt) return 'external_receipt';
    if (view?.message || view?.summary || view?.result || view?.text) return 'text';
    return 'business_status';
}

function countBusinessOperationRecords(value: unknown): number {
    return businessOperationRows(value).length;
}

function businessOperationResponseSummary(response?: Record<string, unknown>): string {
    if (!response) return '';
    const direct = String(response.text || response.message || response.summary || response.result || '').trim();
    if (direct) return direct.slice(0, 180);
    const rows = businessOperationRows(response);
    if (rows?.length) {
        const first = rows[0];
        if (first && typeof first === 'object') {
            return Object.entries(first as Record<string, unknown>)
                .slice(0, 3)
                .map(([key, value]) => key + ': ' + String(value))
                .join(' ·  ')
                .slice(0, 180);
        }
        return String(first).slice(0, 180);
    }
    return JSON.stringify(response).slice(0, 180);
}

function buildBusinessOperationResult(result: Record<string, unknown> | undefined, actionRole: string, text: typeof labels.zh): BusinessOperationResultView {
    const response = result?.response && typeof result.response === 'object' ? result.response as Record<string, unknown> : undefined;
    const resultPayload = result?.result_payload && typeof result.result_payload === 'object' && !Array.isArray(result.result_payload)
        ? result.result_payload as Record<string, unknown>
        : undefined;
    const primaryResult = String(result?.primary_result || '').trim() || undefined;
    const mode = String(result?.mode || actionRole || 'DataSrv').trim();
    const target = String(result?.target || result?.endpoint || actionRole || mode).trim();
    const status = String(result?.result_status || resultPayload?.result_status || resultPayload?.status || response?.status || response?.state || 'done').trim();
    const payloadRows = businessOperationRows(resultPayload);
    const responseRows = businessOperationRows(response);
    const rows = payloadRows.length > 0 ? payloadRows : responseRows;
    const columns = businessOperationColumns(rows);
    const recordCount = rows.length;
    const kind = businessOperationResultKind(mode, primaryResult, resultPayload, response);
    const summary = businessOperationResponseSummary(resultPayload) || businessOperationResponseSummary(response);
    const message = summary || text.runCompleted + ': ' + mode + ' ·  ' + status;
    const outputs = normalizeApprovalOutputs(Array.isArray(result?.outputs) ? result.outputs as ApprovalInstanceOutputView[] : undefined);
    const artifacts = Array.isArray(result?.artifacts)
        ? result.artifacts.filter((item): item is SkillRunArtifactView => !!item && typeof item === 'object' && !Array.isArray(item))
        : undefined;
    return { mode, target, status, kind, message, recordCount, columns, rows, response, primaryResult, resultPayload, outputs, artifacts };
}

function businessOperationRunEvidence(result: BusinessOperationResultView): Pick<AppRunHistoryEntry, 'resultPayload' | 'outputs' | 'artifacts'> {
    const response = result.response && typeof result.response === 'object' ? result.response : {};
    const resultPayload: Record<string, unknown> = {
        ...response,
        ...(result.resultPayload || {}),
        mode: result.mode,
        target: result.target,
        result_status: result.status,
        status: String(result.resultPayload?.status || (response as Record<string, unknown>).status || result.status),
        business_status: String(result.resultPayload?.business_status || (response as Record<string, unknown>).business_status || result.status),
        response,
    };
    if (result.rows.length > 0 && !Array.isArray(resultPayload.rows)) resultPayload.rows = result.rows;
    const outputs: ApprovalInstanceOutputView[] = result.outputs && result.outputs.length > 0 ? result.outputs : [{
        type: result.kind,
        kind: result.kind,
        title: result.target || result.mode,
        text: result.message,
        status: result.status,
        data: resultPayload,
    }];
    return { resultPayload, outputs, artifacts: result.artifacts || [] };
}

async function lookupSkillRunArtifactFromUI(runID: string, artifactRef: string) {
    if (!runID || !artifactRef) return null;
    try {
        const entry = await GetSkillRunArtifact(runID, artifactRef) as SkillRunArtifactView | null;
        return entry || null;
    } catch {
        return null;
    }
}

async function fallbackSkillRunArtifactPathFromUI(runID: string, artifactRef: string, currentPath: string) {
    const registryEntry = await lookupSkillRunArtifactFromUI(runID, artifactRef);
    if (registryEntry?.available === false) return '';
    const registryPath = String(registryEntry?.path || '').trim();
    return registryPath || currentPath.trim();
}

async function openSkillRunArtifactFromUI(runID: string, artifactRef: string, artifactPath: string, remoteOnly: boolean) {
    let localPath = artifactPath.trim();
    if (runID && artifactRef) {
        if (remoteOnly) {
            await DownloadSkillRunArtifact(runID, artifactRef);
        }
        try {
            await OpenSkillRunArtifact(runID, artifactRef);
            return;
        } catch (error) {
            localPath = await fallbackSkillRunArtifactPathFromUI(runID, artifactRef, localPath);
            if (!localPath) {
                console.warn('[apps] open artifact unavailable', error);
                return;
            }
            console.warn('[apps] open artifact by registry failed; falling back to local path', error);
        }
    }
    if (!localPath) return;
    await OpenFileOrShowInFolder(localPath);
}

async function revealSkillRunArtifactFromUI(runID: string, artifactRef: string, artifactPath: string) {
    let localPath = artifactPath.trim();
    if (runID && artifactRef) {
        try {
            await RevealSkillRunArtifact(runID, artifactRef);
            return;
        } catch (error) {
            localPath = await fallbackSkillRunArtifactPathFromUI(runID, artifactRef, localPath);
            if (!localPath) {
                console.warn('[apps] reveal artifact unavailable', error);
                return;
            }
            console.warn('[apps] reveal artifact by registry failed; falling back to local path', error);
        }
    }
    if (!localPath) return;
    await ShowItemInFolder(localPath);
}

function artifactStatusLabel(status: SkillRunStatusView | null, text: typeof labels.zh) {
    const artifactPath = skillRunPrimaryArtifactPath(status);
    const artifactStatus = String(status?.summary?.artifact_status || '').trim();
    if (artifactPath) return text.artifactReady;
    if (status?.expected_artifact || artifactStatus || status?.summary?.needs_artifact_verification) return text.artifactPending;
    return '';
}

function loadAppRunHistory(appID: string): AppRunHistoryEntry[] {
    if (typeof window === 'undefined' || !appID) return [];
    try {
        const parsed = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, AppRunHistoryEntry[]>;
        return Array.isArray(parsed[appID]) ? parsed[appID].slice(0, 8) : [];
    } catch {
        return [];
    }
}

function loadAllAppRunHistory(): AppRunHistoryEntry[] {
    if (typeof window === 'undefined') return [];
    try {
        const parsed = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, AppRunHistoryEntry[]>;
        return Object.entries(parsed).flatMap(([appID, items]) => Array.isArray(items) ? items.map((item) => ({ ...item, appID: item.appID || appID })) : [])
            .sort((left, right) => String(right.at || '').localeCompare(String(left.at || '')));
    } catch {
        return [];
    }
}

function saveAppRunHistory(appID: string, history: AppRunHistoryEntry[]) {
    if (typeof window === 'undefined' || !appID) return;
    try {
        const parsed = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, AppRunHistoryEntry[]>;
        parsed[appID] = history.slice(0, 8);
        window.localStorage.setItem(runHistoryStorageKey, JSON.stringify(parsed));
    } catch {
        // History is nice-to-have; ignore storage failures.
    }
}

function clearAppRunHistory(appID: string) {
    if (typeof window === 'undefined' || !appID) return;
    try {
        const parsed = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, AppRunHistoryEntry[]>;
        delete parsed[appID];
        window.localStorage.setItem(runHistoryStorageKey, JSON.stringify(parsed));
    } catch {
        // History is nice-to-have; ignore storage failures.
    }
}

function normalizeActiveRunSession(raw: unknown): AppActiveRunSession | null {
    if (!raw || typeof raw !== 'object') return null;
    const value = raw as Partial<AppActiveRunSession>;
    const appID = String(value.appID || '').trim();
    const runID = String(value.runID || '').trim();
    const runKind = String(value.runKind || '').trim() as AppActiveRunKind;
    if (!appID || !runID || !['tool_skill', 'business_skill', 'approval_workflow'].includes(runKind)) return null;
    return {
        appID,
        runID,
        runKind,
        inputSummary: String(value.inputSummary || ''),
        outputMode: String(value.outputMode || ''),
        startedAt: String(value.startedAt || ''),
        definitionHash: String(value.definitionHash || '').trim() || undefined,
        approvalInstance: value.approvalInstance && typeof value.approvalInstance === 'object' ? value.approvalInstance as BackendApprovalInstance : undefined,
    };
}

function loadActiveAppRunSessions(): Record<string, AppActiveRunSession> {
    if (typeof window === 'undefined') return {};
    try {
        const parsed = JSON.parse(window.localStorage.getItem(activeRunStorageKey) || '{}') as Record<string, unknown>;
        if (!parsed || typeof parsed !== 'object') return {};
        const sessions: Record<string, AppActiveRunSession> = {};
        Object.entries(parsed).forEach(([appID, rawSession]) => {
            const session = normalizeActiveRunSession(rawSession);
            if (session) sessions[appID] = session;
        });
        return sessions;
    } catch {
        return {};
    }
}

function loadActiveAppRunSession(appID: string): AppActiveRunSession | null {
    return loadActiveAppRunSessions()[appID] || null;
}

function saveActiveAppRunSession(session: AppActiveRunSession) {
    if (typeof window === 'undefined' || !session.appID || !session.runID) return;
    try {
        const sessions = loadActiveAppRunSessions();
        sessions[session.appID] = session;
        window.localStorage.setItem(activeRunStorageKey, JSON.stringify(sessions));
    } catch {
        // Active run recovery is best-effort; the live UI still keeps state.
    }
}

function clearActiveAppRunSession(appID: string, runID?: string) {
    if (typeof window === 'undefined' || !appID) return;
    try {
        const sessions = loadActiveAppRunSessions();
        if (runID && sessions[appID]?.runID !== runID) return;
        delete sessions[appID];
        window.localStorage.setItem(activeRunStorageKey, JSON.stringify(sessions));
    } catch {
        // Active run recovery is best-effort; ignore storage failures.
    }
}

function activeRunSessionMatchesApp(app: AppEntry, session?: AppActiveRunSession | null) {
    if (!session || session.appID !== app.id || !session.runID) return false;
    const expectedHash = appDefinitionFingerprint(app);
    return !session.definitionHash || !expectedHash || session.definitionHash === expectedHash;
}

function isActiveRunMissingError(error: unknown) {
    const message = error instanceof Error ? error.message : String(error || '');
    return /(?:run|skill\s*run|nl\s*skill)\s+"?[\w-]+"?\s+(?:was\s+)?not\s+found/i.test(message)
        || /(?:run|skill\s*run|nl\s*skill).{0,80}\b404\b/i.test(message)
        || /\b404\b.{0,80}(?:run|skill\s*run|nl\s*skill)/i.test(message);
}

function readPublishSubmissions(): Record<string, AppPublishSubmission> {
    if (typeof window === 'undefined') return {};
    try {
        const parsed = JSON.parse(window.localStorage.getItem(publishSubmissionStorageKey) || '{}') as Record<string, AppPublishSubmission>;
        return parsed && typeof parsed === 'object' ? parsed : {};
    } catch {
        return {};
    }
}

function writePublishSubmissions(submissions: Record<string, AppPublishSubmission>) {
    if (typeof window === 'undefined') return;
    try {
        window.localStorage.setItem(publishSubmissionStorageKey, JSON.stringify(submissions));
    } catch {
        // Publish submission state is local UX state; ignore storage failures.
    }
}

function clearAppPublishSubmission(appID: string) {
    if (typeof window === 'undefined' || !appID) return;
    const submissions = readPublishSubmissions();
    if (!submissions[appID]) return;
    delete submissions[appID];
    writePublishSubmissions(submissions);
}

function markAppPublishSubmissionModified(appID: string, version?: number) {
    if (typeof window === 'undefined' || !appID) return;
    const submissions = readPublishSubmissions();
    const existing = submissions[appID];
    if (!existing) return;
    writePublishSubmissions({
        ...submissions,
        [appID]: {
            ...existing,
            modifiedAt: new Date().toISOString(),
            version: version || existing.version,
        },
    });
}

function readDismissedSkillAppIds(): Set<string> {
    if (typeof window === 'undefined') return new Set();
    try {
        const raw = window.localStorage.getItem(dismissedSkillAppsStorageKey);
        const parsed = raw ? JSON.parse(raw) : [];
        return new Set(Array.isArray(parsed) ? parsed : []);
    } catch {
        return new Set();
    }
}

function dismissSkillApp(appId: string) {
    if (typeof window === 'undefined' || !appId) return;
    try {
        const ids = readDismissedSkillAppIds();
        ids.add(appId);
        window.localStorage.setItem(dismissedSkillAppsStorageKey, JSON.stringify([...ids]));
    } catch {
        // Dismissed state is local UX state; ignore storage failures.
    }
}

function undismissSkillApp(appId: string) {
    if (typeof window === 'undefined' || !appId) return;
    try {
        const ids = readDismissedSkillAppIds();
        if (!ids.has(appId)) return;
        ids.delete(appId);
        window.localStorage.setItem(dismissedSkillAppsStorageKey, JSON.stringify([...ids]));
    } catch {
        // Dismissed state is local UX state; ignore storage failures.
    }
}

function publishSubmissionStatusLabel(submission: AppPublishSubmission, text: typeof labels.zh) {
    if (submission.modifiedAt) return text.localModifiedReview;
    if (submission.status === 'submitted' && submission.channel === 'local') return text.localReviewPending;
    switch (submission.status) {
        case 'pending_review':
            return text.pendingReview;
        case 'review_failed':
            return text.reviewFailed;
        case 'approved':
            return text.reviewApproved;
        case 'published':
            return text.reviewPublished;
        case 'deprecated':
            return text.reviewDeprecated;
        case 'revoked':
            return text.reviewRevoked;
        case 'submitted':
        default:
            return text.pendingReview;
    }
}

function normalizeFreshPublishSubmission(submission: AppPublishSubmission): AppPublishSubmission {
    return {
        id: submission.id,
        appID: submission.appID,
        submittedAt: submission.submittedAt,
        status: submission.status,
        channel: submission.channel,
        reviewedAt: submission.reviewedAt,
        publishedAt: submission.publishedAt,
        reviewer: submission.reviewer,
        riskLevel: submission.riskLevel,
        approvedScopes: submission.approvedScopes,
        reviewIssues: submission.reviewIssues,
        message: submission.message,
        version: submission.version,
    };
}

async function submitAppPackageToEnterpriseMarket(app: AppEntry, packageManifest: Record<string, unknown>): Promise<AppPublishSubmission | null> {
    const bridge = (globalThis as any)?.window?.go?.main?.App?.SubmitMaclawAppPackage;
    if (typeof bridge !== 'function') return null;
    const response = await bridge(JSON.stringify(packageManifest));
    const payload = response && typeof response === 'object' ? response : {};
    const now = new Date().toISOString();
    const approvedScopes = parseStringList(payload.approved_scopes || payload.approvedScopes);
    const reviewIssues = parseReviewIssues(payload.review_issues || payload.reviewIssues);
    return {
        id: String(payload.submission_id || payload.submissionID || payload.id || `market-review-${app.id}-${Date.now().toString(36)}`),
        appID: app.id,
        submittedAt: String(payload.submitted_at || payload.submittedAt || now),
        status: normalizePublishStatus(payload.status) || 'submitted',
        channel: payload.channel === 'local' ? 'local' : 'hub',
        reviewedAt: payload.reviewed_at || payload.reviewedAt,
        publishedAt: payload.published_at || payload.publishedAt,
        reviewer: payload.reviewer,
        riskLevel: normalizeRiskLevel(payload.risk_level || payload.riskLevel),
        approvedScopes: approvedScopes.length > 0 ? approvedScopes : undefined,
        reviewIssues: reviewIssues.length > 0 ? reviewIssues : undefined,
        version: normalizeAppVersion(payload.version || payload.app_version || payload.appVersion || app.version),
        message: payload.message,
    };
}

function parseStringList(value: unknown): string[] {
    if (!Array.isArray(value)) return [];
    return Array.from(new Set(value.map((item) => String(item || '').trim()).filter(Boolean)));
}

function parseBackendAppInstallDependencies(value: unknown): BackendAppInstallDependency[] {
    if (!Array.isArray(value)) return [];
    return value.reduce<BackendAppInstallDependency[]>((items, item) => {
        const dep = item && typeof item === 'object' ? item as Record<string, unknown> : {};
        const id = String(dep.id || dep.ID || '').trim();
        if (!id) return items;
        items.push({
            id,
            version: String(dep.version || dep.Version || '').trim() || undefined,
            kind: String(dep.kind || dep.Kind || '').trim() || undefined,
            required: dep.required === false || dep.Required === false ? false : true,
            source: String(dep.source || dep.Source || '').trim() || undefined,
            install_ref: String(dep.install_ref || dep.installRef || dep.InstallRef || '').trim() || undefined,
            canonical_id: String(dep.canonical_id || dep.canonicalID || dep.CanonicalID || '').trim() || undefined,
            aliases: parseStringList(dep.aliases || dep.Aliases),
            install_ref_kind: String(dep.install_ref_kind || dep.installRefKind || dep.InstallRefKind || '').trim() || undefined,
            install_ref_target: String(dep.install_ref_target || dep.installRefTarget || dep.InstallRefTarget || '').trim() || undefined,
            install_ref_version: String(dep.install_ref_version || dep.installRefVersion || dep.InstallRefVersion || '').trim() || undefined,
            install_ref_status: String(dep.install_ref_status || dep.installRefStatus || dep.InstallRefStatus || '').trim() || undefined,
            install_ref_message: String(dep.install_ref_message || dep.installRefMessage || dep.InstallRefMessage || '').trim() || undefined,
            app_ids: parseStringList(dep.app_ids || dep.appIDs || dep.AppIDs),
            installed: dep.installed === true || dep.Installed === true,
            installed_name: String(dep.installed_name || dep.installedName || dep.InstalledName || '').trim() || undefined,
            installed_version: String(dep.installed_version || dep.installedVersion || dep.InstalledVersion || '').trim() || undefined,
            required_version: String(dep.required_version || dep.requiredVersion || dep.RequiredVersion || '').trim() || undefined,
            version_status: String(dep.version_status || dep.versionStatus || dep.VersionStatus || '').trim() || undefined,
            runtime_skill_ref: String(dep.runtime_skill_ref || dep.runtimeSkillRef || dep.RuntimeSkillRef || '').trim() || undefined,
            installed_dir: String(dep.installed_dir || dep.installedDir || dep.InstalledDir || '').trim() || undefined,
            installed_status: String(dep.installed_status || dep.installedStatus || dep.InstalledStatus || '').trim() || undefined,
            health: String(dep.health || dep.Health || '').trim() || undefined,
            action: String(dep.action || dep.Action || '').trim() || undefined,
            message: String(dep.message || dep.Message || '').trim() || undefined,
            install_error_code: String(dep.install_error_code || dep.installErrorCode || dep.InstallErrorCode || '').trim() || undefined,
            install_error_stage: String(dep.install_error_stage || dep.installErrorStage || dep.InstallErrorStage || '').trim() || undefined,
            install_error_detail: String(dep.install_error_detail || dep.installErrorDetail || dep.InstallErrorDetail || '').trim() || undefined,
            preflight_status: String(dep.preflight_status || dep.preflightStatus || dep.PreflightStatus || '').trim() || undefined,
            preflight_code: String(dep.preflight_code || dep.preflightCode || dep.PreflightCode || '').trim() || undefined,
            preflight_stage: String(dep.preflight_stage || dep.preflightStage || dep.PreflightStage || '').trim() || undefined,
            preflight_message: String(dep.preflight_message || dep.preflightMessage || dep.PreflightMessage || '').trim() || undefined,
            package_sha256: String(dep.package_sha256 || dep.packageSHA256 || dep.PackageSHA256 || '').trim() || undefined,
            package_checksum: String(dep.package_checksum || dep.packageChecksum || dep.PackageChecksum || '').trim() || undefined,
            package_signature: String(dep.package_signature || dep.packageSignature || dep.PackageSignature || '').trim() || undefined,
            package_download_url: String(dep.package_download_url || dep.packageDownloadURL || dep.PackageDownloadURL || '').trim() || undefined,
            download_node: String(dep.download_node || dep.downloadNode || dep.DownloadNode || '').trim() || undefined,
            resolved_download_url: String(dep.resolved_download_url || dep.resolvedDownloadURL || dep.ResolvedDownloadURL || '').trim() || undefined,
            integrity_status: String(dep.integrity_status || dep.integrityStatus || dep.IntegrityStatus || '').trim() || undefined,
            integrity_code: String(dep.integrity_code || dep.integrityCode || dep.IntegrityCode || '').trim() || undefined,
            integrity_stage: String(dep.integrity_stage || dep.integrityStage || dep.IntegrityStage || '').trim() || undefined,
            integrity_message: String(dep.integrity_message || dep.integrityMessage || dep.IntegrityMessage || '').trim() || undefined,        });
        return items;
    }, []);
}

function appRunDependencyVerificationEvidence(app: AppEntry, plan: BackendAppInstallPlan | null | undefined, verifiedAt = new Date().toISOString()): AppRunEvidenceDependencyVerification | undefined {
    if (!plan) return undefined;
    const appIDs = [canonicalAppManifestID(app)];
    const allDependencies = parseBackendAppInstallDependencies(plan.dependencies);
    const scopedDependencies = allDependencies.filter((dep) => backendDependencyMatchesAppIDs(dep, appIDs));
    const dependencies = (scopedDependencies.length > 0 ? scopedDependencies : allDependencies.length === 1 ? allDependencies : scopedDependencies)
        .map((dep) => ({ ...dep, app_ids: Array.from(new Set([...(dep.app_ids || []), ...appIDs])) }));
    const selectedAppIDs = new Set(appIDs.flatMap(appInstallIdentityKeys));
    const apps = (plan.apps || []).filter((item) => appInstallIdentityKeys(String(item.id || '')).some((key) => selectedAppIDs.has(key)));
    const workflowContractIssues = workflowContractIssuesForAppIDs(plan, appIDs);
    const governanceReviewIssues = governanceReviewIssuesForAppIDs(plan, appIDs);
    const installTrace = dependencyInstallTraceFromSummary(plan as Record<string, unknown>)
        || dependencyInstallTraceFromSummary(latestAvailableAppRunEvidence(app)?.dependencyVerification as Record<string, unknown> | undefined)
        || dependencyInstallTraceFromSummary(app.installEvidence?.dependency_verification as Record<string, unknown> | undefined);
    return {
        schema: 'maclaw.app.install_plan.v1',
        verifiedAt,
        appCount: apps.length,
        dependencyCount: dependencies.length,
        hasMissingRequired: hasMissingRequiredBackendDependency(plan, appIDs),
        hasBlockingDependency: dependencies.some(isBlockingBackendDependency),
        hasWorkflowContractIssue: workflowContractHasIssueForAppIDs(plan, appIDs),
        workflowContractIssueCount: workflowContractIssues.length,
        hasGovernanceReviewIssue: governanceReviewHasIssueForAppIDs(plan, appIDs),
        governanceReviewIssueCount: governanceReviewIssues.length,
        workflowContractIssues,
        governanceReviewIssues,
        ...(installTrace ? { installTrace, install_trace: installTrace } : {}),
        dependencies,
    };
}
function normalizeRiskLevel(value: unknown): string {
    const risk = String(value || '').trim();
    return ['low', 'medium', 'high', 'critical'].includes(risk) ? risk : '';
}

function parseReviewIssues(value: unknown): AppReviewIssue[] {
    if (!Array.isArray(value)) return [];
    return value.map((item) => {
        const issue = item && typeof item === 'object' ? item as Record<string, unknown> : {};
        return {
            path: String(issue.path || '').trim() || undefined,
            severity: String(issue.severity || '').trim() || undefined,
            message: String(issue.message || '').trim(),
            suggestion: String(issue.suggestion || '').trim() || undefined,
            metadata: issue.metadata && typeof issue.metadata === 'object' ? issue.metadata as Record<string, unknown> : undefined,
        };
    }).filter((issue) => issue.message);
}

function reviewIssueSummary(issue: AppReviewIssue) {
    return [issue.severity, issue.path, issue.message, issue.suggestion]
        .map((value) => String(value || '').trim())
        .filter(Boolean)
        .join(' · ');
}

function reviewIssueMetadataText(issue: AppReviewIssue, key: string) {
    const value = issue.metadata?.[key];
    if (value == null) return '';
    return String(value).trim();
}

function workflowContractIssueDetails(issue: AppReviewIssue, text: typeof labels.zh) {
    const workflowID = reviewIssueMetadataText(issue, 'workflow_skill_id');
    const requiredVersion = reviewIssueMetadataText(issue, 'required_version');
    const installedVersion = reviewIssueMetadataText(issue, 'installed_version');
    const event = reviewIssueMetadataText(issue, 'binding_event');
    const objectRole = reviewIssueMetadataText(issue, 'object_role');
    const installedStatus = reviewIssueMetadataText(issue, 'installed_status');
    const health = reviewIssueMetadataText(issue, 'health');
    const version = [installedVersion && `v${installedVersion}`, requiredVersion && `v${requiredVersion}`].filter(Boolean).join(' -> ');
    const zh = text === labels.zh;
    return [
        workflowID ? { label: text.workflowSkill, value: workflowID } : null,
        version ? { label: text.appVersion, value: version } : null,
        event ? { label: 'Event', value: event } : null,
        objectRole ? { label: text.approvalObjectRoleLabel, value: objectRole } : null,
        installedStatus ? { label: 'Status', value: installedStatus } : null,
        health ? { label: 'Health', value: health } : null,
    ].filter((item): item is { label: string; value: string } => !!item);
}
function reviewIssuesSummary(issues: AppReviewIssue[], text: typeof labels.zh) {
    const visible = issues.slice(0, 3).map(reviewIssueSummary).filter(Boolean);
    const remaining = issues.length - visible.length;
    return [
        ...visible,
        remaining > 0 ? `${text.reviewIssuesMore} ${remaining} ${text.reviewIssuesMoreUnit}` : '',
    ].filter(Boolean).join(' ·  ');
}

function reviewIssueSeverity(issue: AppReviewIssue) {
    const severity = String(issue.severity || '').trim().toLowerCase();
    if (severity === 'critical' || severity === 'error') return 'error';
    if (severity === 'warning') return 'warning';
    if (severity === 'info') return 'info';
    return 'notice';
}

function reviewIssuesIncludeDependency(issues?: AppReviewIssue[]) {
    return (issues || []).some((issue) => /dependency|skill/i.test([issue.path, issue.message, issue.suggestion].filter(Boolean).join(' ')));
}
const ReviewIssuesPanel = ({ issues, text, compact = false }: { issues?: AppReviewIssue[]; text: typeof labels.zh; compact?: boolean }) => {
    const visible = (issues || []).filter((issue) => issue.message).slice(0, 4);
    const remaining = Math.max(0, (issues || []).length - visible.length);
    if (visible.length === 0) return null;
    return (
        <div className="apps-review-issues" data-compact={compact ? 'true' : 'false'} role="list" aria-label={text.reviewIssues}>
            {visible.map((issue, index) => (
                <div className="apps-review-issues__item" data-severity={reviewIssueSeverity(issue)} role="listitem" key={`${issue.path || 'issue'}-${issue.message}-${index}`}>
                    <strong>{String(issue.severity || 'notice').trim() || 'notice'}</strong>
                    {issue.path && <span>{issue.path}</span>}
                    <em>{issue.message}</em>
                    {issue.suggestion && <small>{issue.suggestion}</small>}
                </div>
            ))}
            {remaining > 0 && <div className="apps-review-issues__more" role="listitem">{text.reviewIssuesMore} {remaining} {text.reviewIssuesMoreUnit}</div>}
        </div>
    );
};
function packageAppNamesFromRecord(record: Record<string, unknown> | null): string[] {
    const pkg = record?.package || record?.Package;
    const apps = pkg && typeof pkg === 'object' && !Array.isArray(pkg) ? (pkg as Record<string, unknown>).apps : [];
    if (!Array.isArray(apps)) return [];
    return apps.map((item) => {
        const wrapper = item && typeof item === 'object' ? item as Record<string, unknown> : {};
        const app = wrapper.app && typeof wrapper.app === 'object' ? wrapper.app as Record<string, unknown> : {};
        return String(app.name || app.id || '').trim();
    }).filter(Boolean);
}

function packageWorkspaceLayoutSummariesFromRecord(record: Record<string, unknown> | null): string[] {
    const pkg = record?.package || record?.Package;
    const apps = pkg && typeof pkg === 'object' && !Array.isArray(pkg) ? (pkg as Record<string, unknown>).apps : [];
    if (!Array.isArray(apps)) return [];
    return apps.map((item) => {
        const wrapper = item && typeof item === 'object' ? item as Record<string, any> : {};
        const app = wrapper.app && typeof wrapper.app === 'object' ? wrapper.app as Record<string, any> : {};
        const appName = String(app.name || app.id || '').trim();
        const governance = app.governance && typeof app.governance === 'object' ? app.governance as Record<string, any> : {};
        const layout = governance.workspaceLayout && typeof governance.workspaceLayout === 'object'
            ? governance.workspaceLayout as Record<string, any>
            : {};
        const binding = app.binding && typeof app.binding === 'object' ? app.binding as Record<string, any> : {};
        const ui = (binding.ui && typeof binding.ui === 'object' ? binding.ui : app.ui && typeof app.ui === 'object' ? app.ui : {}) as Record<string, any>;
        const entry = String(layout.entry || ui.entry || '').trim();
        const layouts = ui.layouts && typeof ui.layouts === 'object' ? ui.layouts as Record<string, any> : {};
        const entryLayout = entry && layouts[entry] && typeof layouts[entry] === 'object' ? layouts[entry] as Record<string, any> : {};
        const template = String(layout.template || entryLayout.template || '').trim();
        const density = String(layout.density || entryLayout.density || '').trim();
        const fingerprint = String(layout.fingerprint || '').trim();
        const regionCount = String(layout.visibleRegionCount || layout.visible_region_count || layout.regionCount || layout.region_count || '').trim();
        const parts = [
            appName,
            entry,
            template,
            density,
            regionCount ? `${regionCount} regions` : '',
            fingerprint ? `fp:${fingerprint}` : '',
        ].filter(Boolean);
        return parts.length > 1 ? parts.join(' · ') : '';
    }).filter(Boolean);
}

function eventSummariesFromRecord(record: Record<string, unknown> | null): string[] {
    const events = record?.events || record?.Events;
    if (!Array.isArray(events)) return [];
    return events.slice(0, 3).map((item) => {
        const event = item && typeof item === 'object' ? item as Record<string, unknown> : {};
        return [event.at, event.status, event.channel, event.submission_id || event.submissionID]
            .map((value) => String(value || '').trim())
            .filter(Boolean)
            .join(' · ');
    }).filter(Boolean);
}

async function listMaclawAppPackageSubmissions(limit = 8): Promise<AppPackageSubmissionSummary[] | null> {
    const bridge = (globalThis as any)?.window?.go?.main?.App?.ListMaclawAppPackageSubmissions;
    if (typeof bridge !== 'function') return null;
    const response = await bridge(limit);
    if (!Array.isArray(response)) return [];
    return response.map((item) => ({
        submissionID: String(item?.submission_id || item?.submissionID || ''),
        hubCapabilityID: String(item?.hub_capability_id || item?.hubCapabilityID || ''),
        submittedAt: String(item?.submitted_at || item?.submittedAt || ''),
        status: String(item?.status || ''),
        channel: String(item?.channel || ''),
        appIDs: Array.isArray(item?.app_ids)
            ? item.app_ids.map((value: unknown) => String(value)).filter(Boolean)
            : Array.isArray(item?.appIDs)
                ? item.appIDs.map((value: unknown) => String(value)).filter(Boolean)
                : [],
        appNames: Array.isArray(item?.app_names)
            ? item.app_names.map((value: unknown) => String(value)).filter(Boolean)
            : Array.isArray(item?.appNames)
                ? item.appNames.map((value: unknown) => String(value)).filter(Boolean)
                : [],
        packageSHA: String(item?.package_sha || item?.package_sha256 || item?.packageSHA || ''),
        packageBytes: Number(item?.package_bytes || item?.packageBytes || 0) || 0,
        reviewedAt: String(item?.reviewed_at || item?.reviewedAt || ''),
        publishedAt: String(item?.published_at || item?.publishedAt || ''),
        reviewer: String(item?.reviewer || ''),
        riskLevel: normalizeRiskLevel(item?.risk_level || item?.riskLevel),
        approvedScopes: parseStringList(item?.approved_scopes || item?.approvedScopes),
        reviewIssues: parseReviewIssues(item?.review_issues || item?.reviewIssues),
        dependencies: parseBackendAppInstallDependencies(item?.dependencies || item?.Dependencies),
        submissionEvidence: submissionEvidenceRecordFromSummaryItem(item),
        reviewEvidence: appEvidenceRecord(item?.review_evidence || item?.reviewEvidence),
        eventCount: Number(item?.event_count || item?.eventCount || 0) || 0,
        lastEventAt: String(item?.last_event_at || item?.lastEventAt || ''),
        message: String(item?.message || ''),
    })).filter((item) => item.submissionID);
}

function submissionEvidenceRecordFromSummaryItem(item: any): BackendAppInstallRecord | undefined {
    const evidence = item?.submission_evidence || item?.submissionEvidence;
    if (!evidence || typeof evidence !== 'object') return undefined;
    const appIDs = parseStringList(item?.app_ids || item?.appIDs);
    return installEvidenceRecordForApp({ install_evidence: evidence as Record<string, unknown> }, appIDs[0]);
}

function hasMaclawAppPackageSubmissionDetailBridge() {
    return typeof (globalThis as any)?.window?.go?.main?.App?.GetMaclawAppPackageSubmission === 'function';
}

async function getMaclawAppPackageSubmissionPackage(submissionID: string): Promise<Record<string, unknown> | null> {
    const response = await getMaclawAppPackageSubmissionRecord(submissionID);
    if (!response) return null;
    const pkg = response.package || response.Package;
    return pkg && typeof pkg === 'object' && !Array.isArray(pkg) ? pkg as Record<string, unknown> : null;
}

async function getMaclawAppPackageSubmissionRecord(submissionID: string): Promise<Record<string, unknown> | null> {
    const bridge = (globalThis as any)?.window?.go?.main?.App?.GetMaclawAppPackageSubmission;
    if (typeof bridge !== 'function' || !submissionID) return null;
    const response = await bridge(submissionID);
    if (!response || typeof response !== 'object') return null;
    return response as Record<string, unknown>;
}

async function withdrawMaclawAppPackageSubmission(submissionID: string): Promise<boolean | null> {
    const bridge = (globalThis as any)?.window?.go?.main?.App?.WithdrawMaclawAppPackageSubmission;
    if (typeof bridge !== 'function' || !submissionID) return null;
    return !!(await bridge(submissionID));
}

function hasSyncMaclawAppPackageSubmissionBridge() {
    return typeof (globalThis as any)?.window?.go?.main?.App?.SyncMaclawAppPackageSubmissionToHub === 'function';
}

function hasRefreshMaclawAppPackageSubmissionBridge() {
    return typeof (globalThis as any)?.window?.go?.main?.App?.RefreshMaclawAppPackageSubmissionFromHub === 'function';
}

async function refreshMaclawAppPackageSubmissionFromHub(submissionID: string): Promise<Record<string, unknown> | null> {
    const bridge = (globalThis as any)?.window?.go?.main?.App?.RefreshMaclawAppPackageSubmissionFromHub;
    if (typeof bridge !== 'function' || !submissionID) return null;
    const response = await bridge(submissionID);
    return response && typeof response === 'object' ? response as Record<string, unknown> : {};
}

async function syncMaclawAppPackageSubmissionToHub(submissionID: string): Promise<Record<string, unknown> | null> {
    const bridge = (globalThis as any)?.window?.go?.main?.App?.SyncMaclawAppPackageSubmissionToHub;
    if (typeof bridge !== 'function' || !submissionID) return null;
    const response = await bridge(submissionID);
    return response && typeof response === 'object' ? response as Record<string, unknown> : {};
}

function normalizePublishStatus(value: unknown): AppPublishStatus | '' {
    const status = String(value || '').trim();
    return status === 'submitted' || status === 'pending_review' || status === 'review_failed' || status === 'approved' || status === 'published' || status === 'deprecated' || status === 'revoked'
        ? status
        : '';
}

function formatPackageBytes(bytes: number) {
    if (!Number.isFinite(bytes) || bytes <= 0) return '';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function mergePublishSubmissionsFromQueue(current: Record<string, AppPublishSubmission>, summaries: AppPackageSubmissionSummary[], appIds: Set<string>) {
    let changed = false;
    const next = { ...current };
    summaries.forEach((summary) => {
        const status = normalizePublishStatus(summary.status);
        if (!status) return;
        summary.appIDs.forEach((appID) => {
            if (!appIds.has(appID)) return;
            const existing = next[appID];
            const merged: AppPublishSubmission = {
                id: summary.submissionID,
                appID,
                submittedAt: summary.submittedAt || existing?.submittedAt || new Date().toISOString(),
                status,
                channel: summary.channel === 'hub' ? 'hub' : 'local',
                message: summary.message || existing?.message,
            reviewedAt: summary.reviewedAt || existing?.reviewedAt,
                publishedAt: summary.publishedAt || existing?.publishedAt,
                reviewer: summary.reviewer || existing?.reviewer,
                riskLevel: summary.riskLevel || existing?.riskLevel,
                approvedScopes: summary.approvedScopes.length > 0 ? summary.approvedScopes : existing?.approvedScopes,
                reviewIssues: summary.reviewIssues.length > 0 ? summary.reviewIssues : existing?.reviewIssues,
                modifiedAt: existing?.modifiedAt,
                version: existing?.version,
            };
            if (JSON.stringify(existing || null) !== JSON.stringify(merged)) {
                next[appID] = merged;
                changed = true;
            }
        });
    });
    return changed ? next : current;
}

function runEvidenceMatchesCurrentDefinition(evidence: AppRunHistoryEntry | undefined | null, expectedHash: string): evidence is AppRunHistoryEntry {
    return !!evidence && !!String(evidence.definitionHash || '').trim() && evidence.definitionHash === expectedHash;
}

function installEvidenceImportedRunEvidence(app: AppEntry): AppRunHistoryEntry | undefined {
    const installEvidence = app.installEvidence;
    const testEvidence = installEvidence?.test_evidence;
    if (!installEvidence || !testEvidence || typeof testEvidence !== 'object') return undefined;
    const evidence = testEvidence as Record<string, unknown>;
    const dependencyVerification = appEvidenceRecord(installEvidence.dependency_verification)
        || appEvidenceRecord(evidence.dependencyVerification)
        || appEvidenceRecord(evidence.dependency_verification);
    return normalizeImportedRunEvidence({
        ...evidence,
        appID: app.id,
        status: 'done',
        runID: firstAppEvidenceValue(evidence.runID, evidence.run_id),
        definitionHash: firstAppEvidenceValue(
            evidence.definitionHash,
            evidence.definition_hash,
            evidence.definitionFingerprint,
            evidence.definition_fingerprint,
        ),
        testProtocolFingerprint: firstAppEvidenceValue(
            evidence.testProtocolFingerprint,
            evidence.test_protocol_fingerprint,
            evidence.testProtocolHash,
            evidence.test_protocol_hash,
        ),
        workspaceLayoutFingerprint: firstAppEvidenceValue(
            evidence.workspaceLayoutFingerprint,
            evidence.workspace_layout_fingerprint,
            evidence.workspaceLayoutHash,
            evidence.workspace_layout_hash,
        ),
        outputMode: firstAppEvidenceValue(evidence.primaryResult, evidence.primary_result, 'imported'),
        inputSummary: 'Imported App install test evidence',
        message: firstAppEvidenceValue(evidence.primaryResult, evidence.primary_result, 'Imported App install test evidence'),
        artifactName: firstAppEvidenceValue(evidence.artifactName, evidence.artifact_name),
        artifactURI: firstAppEvidenceValue(evidence.artifactURI, evidence.artifact_uri),
        artifactPath: firstAppEvidenceValue(evidence.artifactPath, evidence.artifact_path),
        artifactDownloadState: firstAppEvidenceValue(evidence.artifactDownloadState, evidence.artifact_download_state),
        artifacts: firstAppEvidenceValue(evidence.artifacts, evidence.artifact),
        resultPayload: firstAppEvidenceValue(evidence.resultPayload, evidence.result_payload),
        outputs: firstAppEvidenceValue(evidence.outputs, evidence.output_blocks, evidence.outputBlocks),
        resultCoverage: firstAppEvidenceValue(evidence.resultCoverage, evidence.result_coverage),
        dependencyVerification,
        approvalInstance: firstAppEvidenceValue(evidence.approvalInstance, evidence.approval_instance, evidence.approval),
        at: firstAppEvidenceValue(evidence.at, evidence.verifiedAt, evidence.verified_at, installEvidence.installed_at),
    });
}

function latestAppRunEvidence(app: AppEntry): AppRunHistoryEntry | null {
    const expectedHash = appDefinitionFingerprint(app);
    const local = loadAppRunHistory(app.id).find((item) => item.status === 'done' && item.definitionHash === expectedHash);
    if (local) return local;
    const imported = normalizeImportedRunEvidence(app.importedRunEvidence);
    if (runEvidenceMatchesCurrentDefinition(imported, expectedHash)) return imported;
    const installed = installEvidenceImportedRunEvidence(app);
    return runEvidenceMatchesCurrentDefinition(installed, expectedHash) ? installed : null;
}

function latestAvailableAppRunEvidence(app: AppEntry): AppRunHistoryEntry | null {
    return latestAppRunEvidence(app)
        || loadAppRunHistory(app.id).find((item) => item.status === 'done')
        || normalizeImportedRunEvidence(app.importedRunEvidence)
        || installEvidenceImportedRunEvidence(app)
        || null;
}

function appSeedRunHistory(app: AppEntry | undefined): AppRunHistoryEntry[] {
    if (!app?.id) return [];
    const local = loadAppRunHistory(app.id);
    const seeds = [
        normalizeImportedRunEvidence(app.importedRunEvidence),
        installEvidenceImportedRunEvidence(app),
    ].filter(Boolean) as AppRunHistoryEntry[];
    if (seeds.length === 0) return local;
    const next = [...local];
    const seen = new Set(next.map((item) => item.runID));
    seeds.forEach((seed) => {
        if (!seed.runID || seen.has(seed.runID)) return;
        seen.add(seed.runID);
        next.push({ ...seed, appID: seed.appID || app.id });
    });
    return next.slice(0, 8);
}

function appRunEvidenceFreshnessCheck(app: AppEntry, lang?: string) {
    const zh = isZh(lang);
    const expectedHash = appDefinitionFingerprint(app);
    const evidence = latestAvailableAppRunEvidence(app);
    const actualHash = String(evidence?.definitionHash || '').trim();
    const ok = !!evidence && actualHash === expectedHash;
    return {
        ok,
        detail: !evidence
            ? (zh ? '请先运行当前应用再评审' : 'Run the current app before review')
            : !actualHash
                ? (zh ? '\u8fd0\u884c\u8bc1\u636e\u7f3a\u5c11\u5f53\u524d\u5e94\u7528\u5b9a\u4e49\u6307\u7eb9\uff1b\u8bf7\u91cd\u65b0\u8fd0\u884c\u6d4b\u8bd5' : 'Run evidence is missing the current app definition fingerprint; rerun the test')
                : ok
                    ? `${evidence.runID || ''} ·  ${actualHash}`.trim()
                    : (zh ? '\u8fd0\u884c\u8bc1\u636e\u5df2\u4e0d\u5339\u914d\u5f53\u524d\u5e94\u7528\u5b9a\u4e49\uff1b\u8bf7\u91cd\u65b0\u8fd0\u884c\u6d4b\u8bd5' : 'Run evidence is stale for the current app definition; rerun the test'),
    };
}

function appRunEvidenceDesignConsistencyCheck(app: AppEntry, evidence: AppRunHistoryEntry | null, lang?: string) {
    const zh = isZh(lang);
    const expectedProtocol = appTestProtocolFingerprint(appTestProtocolForManifest(app));
    const expectedLayout = appWorkspaceLayoutEvidence(app).fingerprint;
    const actualProtocol = String(evidence?.testProtocolFingerprint || '').trim();
    const actualLayout = String(evidence?.workspaceLayoutFingerprint || '').trim();
    if (!evidence) {
        return { ok: false, detail: zh ? '请先运行当前设计的测试用例' : 'Run the test case for the current design first' };
    }
    if (!actualProtocol) {
        return { ok: false, detail: zh ? '\u8fd0\u884c\u8bc1\u636e\u7f3a\u5c11\u6d4b\u8bd5\u534f\u8bae\u6307\u7eb9\uff1b\u8bf7\u91cd\u65b0\u6d4b\u8bd5' : 'Run evidence is missing the test protocol fingerprint; rerun the test' };
    }
    if (actualProtocol !== expectedProtocol) {
        return { ok: false, detail: zh ? '测试协议已变更；请重新运行测试' : 'Test protocol changed; rerun the test' };
    }
    if (!actualLayout) {
        return { ok: false, detail: zh ? '\u8fd0\u884c\u8bc1\u636e\u7f3a\u5c11\u754c\u9762\u5e03\u5c40\u6307\u7eb9\uff1b\u8bf7\u91cd\u65b0\u6d4b\u8bd5' : 'Run evidence is missing the workspace layout fingerprint; rerun the test' };
    }
    if (actualLayout !== expectedLayout) {
        return { ok: false, detail: zh ? '界面布局已变更；请重新运行测试' : 'Workspace layout changed; rerun the test' };
    }
    return {
        ok: true,
        detail: `${evidence.runID || ''} ·  protocol ${actualProtocol} ·  layout ${actualLayout}`.trim(),
    };
}

function appRunHistoryArtifacts(evidence?: AppRunHistoryEntry | null): SkillRunArtifactView[] {
    const seen = new Set<string>();
    const artifacts: SkillRunArtifactView[] = [];
    const add = (artifact?: SkillRunArtifactView | null) => {
        const keys = skillRunArtifactKeys(artifact);
        if (!artifact || keys.length === 0 || keys.some((key) => seen.has(key))) return;
        keys.forEach((key) => seen.add(key));
        artifacts.push(artifact);
    };
    for (const artifact of evidence?.artifacts || []) add(artifact);
    if (evidence?.artifactID || evidence?.artifactURI || evidence?.artifactName || evidence?.artifactPath) {
        add({
            id: evidence.artifactID,
            uri: evidence.artifactURI,
            name: evidence.artifactName,
            path: evidence.artifactPath,
            download_state: evidence.artifactDownloadState,
        });
    }
    return artifacts;
}

function appRunHistoryArtifactEvidence(artifact: SkillRunArtifactView) {
    return {
        id: artifact.id,
        uri: artifact.uri,
        name: artifact.name,
        path: artifact.path,
        remoteUrl: artifact.remote_url,
        checksum: artifact.checksum,
        downloadState: artifact.download_state,
        mimeType: artifact.mime_type,
        sizeBytes: artifact.size_bytes,
        status: artifact.status,
        presentation: artifact.presentation,
    };
}

function appRunHistoryOutputEvidence(output: ApprovalInstanceOutputView) {
    return {
        type: output.type,
        kind: output.kind,
        title: output.title,
        text: output.text,
        status: output.status,
        artifactId: output.artifact_id,
        data: output.data,
    };
}

type AppDependencyEvidence = BackendAppInstallDependency & { capabilities?: string[] };

function normalizeAppDependencyVerificationPlan(app: AppEntry, raw: unknown, fallbackDependencies?: unknown): BackendAppInstallPlan | undefined {
    const verification = raw && typeof raw === 'object' ? raw as Record<string, unknown> : {};
    const dependencies = parseBackendAppInstallDependencies(verification.dependencies || verification.Dependencies || fallbackDependencies);
    if (dependencies.length === 0) return undefined;
    const installTrace = dependencyInstallTraceFromSummary(verification);
    return {
        schema: String(verification.schema || 'maclaw.app.install_plan.v1'),
        apps: Array.isArray(verification.apps)
            ? verification.apps as BackendAppInstallPlan['apps']
            : [{ id: canonicalAppManifestID(app), name: app.name, kind: app.kind }],
        dependencies,
        workflow_contract_issues: parseReviewIssues(verification.workflow_contract_issues || verification.workflowContractIssues),
        has_workflow_contract_issue: verification.has_workflow_contract_issue === true || verification.hasWorkflowContractIssue === true,
        governance_review_issues: parseReviewIssues(verification.governance_review_issues || verification.governanceReviewIssues),
        has_governance_review_issue: verification.has_governance_review_issue === true || verification.hasGovernanceReviewIssue === true,
        has_missing_required: verification.has_missing_required === true || verification.hasMissingRequired === true || dependencies.some((dep) => isBlockingBackendDependency(dep) && dep.required !== false && !dep.installed),
        has_blocking_dependency: verification.has_blocking_dependency === true || verification.hasBlockingDependency === true || dependencies.some(isBlockingBackendDependency),
        ...(installTrace ? { install_trace: installTrace, installTrace } : {}),
    };
}

function appInstallEvidenceDependencyVerificationPlan(app: AppEntry): BackendAppInstallPlan | undefined {
    const verification = app.installEvidence?.dependency_verification && typeof app.installEvidence.dependency_verification === 'object'
        ? app.installEvidence.dependency_verification as Record<string, unknown>
        : undefined;
    return normalizeAppDependencyVerificationPlan(app, verification, app.installEvidence?.dependencies)
        || normalizeAppDependencyVerificationPlan(app, latestAppRunEvidence(app)?.dependencyVerification)
        || normalizeAppDependencyVerificationPlan(app, normalizeImportedRunEvidence(app.importedRunEvidence)?.dependencyVerification)
        || normalizeAppDependencyVerificationPlan(app, installEvidenceImportedRunEvidence(app)?.dependencyVerification);
}

function appInstallEvidenceDependencies(app: AppEntry): AppDependencyEvidence[] {
    const verification = appInstallEvidenceDependencyVerificationPlan(app);
    const verifiedDependencies = parseBackendAppInstallDependencies(verification?.dependencies);
    const directDependencies = parseBackendAppInstallDependencies(app.installEvidence?.dependencies);
    const dependencies = verifiedDependencies.length > 0 ? verifiedDependencies : directDependencies;
    return dependencies.map((dep) => ({ ...dep, capabilities: [] }));
}

function appDependencyEvidence(app: AppEntry): AppDependencyEvidence[] {
    const installedEvidence = appInstallEvidenceDependencies(app);
    if (installedEvidence.length > 0) return installedEvidence;
    return appSkillDependencies(app).map((dep) => ({
        id: dep.id,
        version: dep.version,
        kind: dep.kind || 'runtime_skill',
        required: dep.required !== false,
        source: dep.source || 'hub',
        install_ref: appSkillDependencyInstallRef(dep) || undefined,
        canonical_id: String(dep.canonical_id || dep.canonicalID || '').trim() || undefined,
        aliases: Array.isArray(dep.aliases) ? dep.aliases.map((alias) => String(alias || '').trim()).filter(Boolean) : undefined,
        capabilities: dep.capabilities || [],
    }));
}

function dependencyNormalizedText(value: unknown) {
    return String(value || '').trim().toLowerCase();
}

function backendDependencyInstallRef(dep?: BackendAppInstallDependency): string {
    const raw = dep as (BackendAppInstallDependency & Record<string, unknown>) | undefined;
    return String(raw?.install_ref || raw?.installRef || '').trim();
}

function backendDependencyMatchesDeclaredSkill(verified: BackendAppInstallDependency, declared: AppSkillDependency): boolean {
    if (dependencyNormalizedText(verified.id) !== dependencyNormalizedText(declared.id)) return false;
    const declaredKind = dependencyNormalizedText(declared.kind);
    const verifiedKind = dependencyNormalizedText(verified.kind);
    const compatibleSkillKind = (declaredKind === 'app_skill' && verifiedKind === 'runtime_skill') || (declaredKind === 'runtime_skill' && verifiedKind === 'app_skill');
    if (declaredKind && verifiedKind && declaredKind !== verifiedKind && !compatibleSkillKind) return false;
    const declaredSource = dependencyNormalizedText(declared.source);
    const verifiedSource = dependencyNormalizedText(verified.source);
    if (declaredSource && verifiedSource && declaredSource !== verifiedSource) return false;
    const declaredInstallRef = dependencyNormalizedText(appSkillDependencyInstallRef(declared));
    if (!declaredInstallRef) return true;
    return dependencyNormalizedText(backendDependencyInstallRef(verified)) === declaredInstallRef;
}

function appDependencyPublishSummary(app: AppEntry, lang?: string) {
    const zh = isZh(lang);
    const deps = appDependencyEvidence(app);
    if (deps.length === 0) return zh ? '\u65e0 Skill \u4f9d\u8d56' : 'No Skill dependencies';
    return deps.map((dep) => {
        const state = [dep.health, dep.action].map((item) => String(item || '').trim()).find(Boolean);
        return `${dep.id}${dep.version ? `@${dep.version}` : ''} (${[dep.kind, dep.required ? '' : 'optional', state].filter(Boolean).join(', ')})`;
    }).join(', ');
}

function appDependencyVerificationAppIDs(app: AppEntry): string[] {
    return Array.from(new Set([
        canonicalAppManifestID(app),
        app.id,
        app.manifest?.datasrv?.appID,
    ].map((id) => String(id || '').trim()).filter(Boolean)));
}

function appDependencyVerificationPublishCheckForPlan(app: AppEntry, plan: BackendAppInstallPlan | undefined, lang?: string) {
    const zh = isZh(lang);
    const declared = appSkillDependencies(app).filter((dep) => String(dep.id || '').trim());
    if (declared.length === 0) {
        return { ok: app.kind === 'automation_app', detail: zh ? '\u672a\u58f0\u660e Skill \u4f9d\u8d56' : 'No Skill dependencies declared' };
    }
    if (!plan) {
        return { ok: false, detail: zh ? '\u7f3a\u5c11\u4f9d\u8d56\u9a8c\u8bc1\u8bc1\u636e' : 'Missing dependency verification evidence' };
    }
    if (plan.schema && plan.schema !== 'maclaw.app.install_plan.v1') {
        return { ok: false, detail: zh ? '\u4f9d\u8d56\u9a8c\u8bc1 schema \u65e0\u6548' : 'Invalid dependency verification schema' };
    }
    const appIDs = appDependencyVerificationAppIDs(app);
    const verifiedDependencies = parseBackendAppInstallDependencies(plan.dependencies).filter((dep) => backendDependencyMatchesAppIDs(dep, appIDs));
    if (workflowContractHasIssueForAppIDs(plan, appIDs)) {
        return { ok: false, detail: zh ? '\u5ba1\u6279 workflow \u5408\u540c\u9a8c\u8bc1\u672a\u901a\u8fc7' : 'Approval workflow contract verification failed' };
    }
    if (governanceReviewHasIssueForAppIDs(plan, appIDs)) {
        return { ok: false, detail: zh ? '\u4f9d\u8d56\u6cbb\u7406\u590d\u6838\u672a\u901a\u8fc7' : 'Dependency governance review failed' };
    }
    if (hasMissingRequiredBackendDependency(plan, appIDs)) {
        return { ok: false, detail: zh ? '\u5fc5\u9700 Skill \u4f9d\u8d56\u7f3a\u5931\u6216\u88ab\u963b\u65ad' : 'Required Skill dependency is missing or blocked' };
    }
    const missing = declared.filter((dep) => !verifiedDependencies.some((verified) => backendDependencyMatchesDeclaredSkill(verified, dep)));
    if (missing.length > 0) {
        const names = missing.map((dep) => dep.id).join(', ');
        return { ok: false, detail: zh ? `\u4f9d\u8d56\u9a8c\u8bc1\u7f3a\u5c11\u58f0\u660e Skill: ${names}` : `Dependency verification is missing declared Skill: ${names}` };
    }
    return { ok: true, detail: appDependencyPublishSummary(app, lang) };
}

function appDependencyVerificationPublishCheck(app: AppEntry, lang?: string) {
    return appDependencyVerificationPublishCheckForPlan(app, appInstallEvidenceDependencyVerificationPlan(app), lang);
}

function appInstallPlanHasAppScopedSignals(plan: BackendAppInstallPlan | null | undefined, appIDs: string[]): boolean {
    if (!plan) return false;
    if (backendDependenciesForAppIDs(plan, appIDs).length > 0) return true;
    if (workflowContractIssuesForAppIDs(plan, appIDs).length > 0) return true;
    if (governanceReviewIssuesForAppIDs(plan, appIDs).length > 0) return true;
    if (hasMissingRequiredBackendDependency(plan, appIDs)) return true;
    return false;
}

function appWorkspaceLayoutEvidence(app: AppEntry) {
    const ui = normalizeAppWorkspaceLayout(app.manifest?.ui, app.kind);
    const entry = ui.entry || workspaceEntryForKind(app.kind);
    const layout = ui.layouts?.[entry] || {};
    const runtime = normalizeRuntimeWorkspaceLayout(layout, app.kind);
    const orderedRegions = canonicalRuntimeWorkspaceRegions(runtime.regions);
    const enterpriseUI = isEnterpriseAppKind(app.kind) ? {
        navigation: normalizeUIStringList(layout.navigation, defaultEnterpriseNavigation(app.kind)),
        list: {
            ...(layout.list || {}),
            columns: normalizeUIStringList(layout.list?.columns, defaultEnterpriseColumns(app.kind)),
        },
    } : {};
    return {
        schema: ui.schema,
        entry,
        template: runtime.template,
        density: runtime.density,
        primaryRegion: runtime.primaryRegion,
        outputRegion: runtime.outputRegion,
        regionCount: orderedRegions.length,
        visibleRegionCount: orderedRegions.filter((region) => region.visible !== false).length,
        regionIds: orderedRegions.map((region) => region.id),
        fingerprint: workspaceLayoutFingerprint(entry, { ...runtime, regions: orderedRegions }),
        regions: orderedRegions,
        savedInManifest: !!layout.studio?.savedInManifest || !!app.manifest?.ui,
        ...enterpriseUI,
    };
}

function requiredWorkspaceRegionRoles(kind: AppKind): string[] {
    if (kind === 'enterprise_approval_app') return ['input', 'instance_list', 'output'];
    if (kind === 'enterprise_normal_app') return ['input', 'record_list', 'output'];
    return ['input', 'output'];
}

function missingWorkspaceRegionRoles(app: AppEntry): string[] {
    const layout = appWorkspaceLayoutEvidence(app);
    const availableRoles = new Set(layout.regions.filter((region) => region.visible !== false).map((region) => region.role));
    return requiredWorkspaceRegionRoles(app.kind).filter((role) => !availableRoles.has(role));
}

function appWorkspaceLayoutPublishSummary(app: AppEntry, lang?: string) {
    const zh = isZh(lang);
    const layout = appWorkspaceLayoutEvidence(app);
    if (!layout.schema || !layout.entry || layout.regionCount <= 0) return zh ? '\u7f3a\u5c11 workspace layout' : 'Missing workspace layout';
    const missingRoles = missingWorkspaceRegionRoles(app);
    if (missingRoles.length > 0) return zh ? `\u7f3a\u5c11 workspace \u533a\u57df\u89d2\u8272: ${missingRoles.join(', ')}` : `Missing workspace region roles: ${missingRoles.join(', ')}`;
    return `${layout.entry} ·  ${layout.template} ·  ${layout.density} ·  ${layout.regionCount} regions`;
}
function buildAppResultContract(kind: AppKind, outputModes: string[] = []): AppResultContract {
    const normalizedOutputModes = normalizeOutputModes(outputModes);
    const types = new Set<string>();
    const add = (...items: string[]) => items.forEach((item) => item && types.add(item));
    if (kind === 'enterprise_approval_app') {
        add('approval_result', 'business_status', 'business_record', 'content', 'document', 'notification', 'requires_input');
    } else if (kind === 'enterprise_normal_app') {
        add('business_status', 'business_record', 'content', 'document', 'notification', 'action', 'requires_input');
    } else {
        add('content');
    }
    normalizedOutputModes.forEach((mode) => {
        if (mode === 'json' || mode === 'txt') add('content');
        if (mode === 'docx' || mode === 'pdf' || mode === 'xlsx') add('document', 'artifact');
    });
    const primary = kind === 'enterprise_approval_app'
        ? 'approval_result'
        : kind === 'enterprise_normal_app'
            ? 'business_status'
            : normalizedOutputModes.includes('json') || normalizedOutputModes.includes('txt')
                ? 'content'
                : 'artifact';
    return {
        schema: 'maclaw.app.result.v1',
        primary,
        types: Array.from(types),
        outputModes: normalizedOutputModes,
        approvalDecisions: kind === 'enterprise_approval_app' ? ['approved', 'rejected', 'attention', 'cancelled', 'timeout'] : undefined,
        delivery: {
            inlineContent: types.has('content'),
            artifacts: types.has('artifact') || types.has('document'),
            businessRecord: types.has('business_record'),
            notifications: types.has('notification'),
        },
    };
}

function normalizeAppResultContract(value: unknown, kind: AppKind, outputModes: string[] = []): AppResultContract {
    const fallback = buildAppResultContract(kind, outputModes);
    if (!value || typeof value !== 'object') return fallback;
    const raw = value as Partial<AppResultContract>;
    const types = Array.isArray(raw.types) ? raw.types.map((item) => String(item || '').trim()).filter(Boolean) : fallback.types;
    const rawOutputModes = Array.isArray(raw.outputModes) ? raw.outputModes : Array.isArray((raw as any).output_modes) ? (raw as any).output_modes : fallback.outputModes;
    const outputModesValue = normalizeOutputModes(rawOutputModes as string[]);
    const approvalDecisions = Array.isArray(raw.approvalDecisions)
        ? raw.approvalDecisions.map((item) => String(item || '').trim()).filter(Boolean)
        : Array.isArray((raw as any).approval_decisions)
            ? (raw as any).approval_decisions.map((item: unknown) => String(item || '').trim()).filter(Boolean)
            : fallback.approvalDecisions;
    const deliveryRaw = raw.delivery && typeof raw.delivery === 'object' ? raw.delivery as Partial<AppResultContract['delivery']> : {};
    return {
        schema: 'maclaw.app.result.v1',
        primary: String(raw.primary || fallback.primary).trim() || fallback.primary,
        types: types.length ? types : fallback.types,
        outputModes: outputModesValue,
        approvalDecisions: approvalDecisions?.length ? approvalDecisions : undefined,
        delivery: {
            inlineContent: typeof deliveryRaw.inlineContent === 'boolean' ? deliveryRaw.inlineContent : fallback.delivery.inlineContent,
            artifacts: typeof deliveryRaw.artifacts === 'boolean' ? deliveryRaw.artifacts : fallback.delivery.artifacts,
            businessRecord: typeof deliveryRaw.businessRecord === 'boolean' ? deliveryRaw.businessRecord : typeof (deliveryRaw as any).business_record === 'boolean' ? (deliveryRaw as any).business_record : fallback.delivery.businessRecord,
            notifications: typeof deliveryRaw.notifications === 'boolean' ? deliveryRaw.notifications : fallback.delivery.notifications,
        },
    };
}

function applyAppResultContract(manifest: AppManifestBinding, kind: AppKind, override?: AppResultContract): AppManifestBinding {
    const outputModes = normalizeOutputModes(manifest.skill?.outputModes);
    return {
        ...manifest,
        resultContract: normalizeAppResultContract(override, kind, outputModes),
    };
}

function appResultContractForManifest(app: AppEntry): AppResultContract {
    return normalizeAppResultContract(app.manifest?.resultContract, app.kind, app.manifest?.skill?.outputModes || []);
}

function buildAppTestProtocol(kind: AppKind, outputModes: string[] = [], resultContract?: AppResultContract): AppTestProtocol {
    const contract = resultContract || buildAppResultContract(kind, outputModes);
    const sampleInput = kind === 'enterprise_approval_app'
        ? { record_ref: 'sample-record', applicant: 'current_user', business_payload: { amount: 1280 } }
        : kind === 'enterprise_normal_app'
            ? { business_payload: { note: 'sample' } }
            : { file: 'sample.pdf', params: '' };
    const expectedOutput = kind === 'enterprise_approval_app'
        ? { approval_result: 'approved', primary: contract.primary }
        : kind === 'enterprise_normal_app'
            ? { business_status: 'ready', primary: contract.primary }
            : { status: 'ok', primary: contract.primary };
    return {
        schema: 'maclaw.app.test_protocol.v1',
        sampleInput,
        expectedOutput,
        ...(kind === 'enterprise_approval_app' ? {
            workflowRequiredInputs: ['record_ref', 'applicant', 'business_payload'],
            workflowDecisionOutputs: ['approved', 'rejected', 'attention'],
            workflowRequiredOutputs: ['workflow_result', 'approval_instance', 'outputs', 'artifacts'],
        } : {}),
        requiredRoles: kind === 'enterprise_approval_app' ? ['applicant', 'approver'] : kind === 'enterprise_normal_app' ? ['operator'] : [],
        requiredScopes: [],
        riskLevel: isEnterpriseAppKind(kind) ? 'medium' : 'low',
    };
}

function cleanStringList(value: unknown): string[] {
    return Array.isArray(value) ? value.map((item) => String(item || '').trim()).filter(Boolean) : [];
}

function plainObjectOrFallback(value: unknown, fallback: Record<string, unknown>): Record<string, unknown> {
    return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : fallback;
}

function normalizeAppTestProtocol(value: unknown, kind: AppKind, outputModes: string[] = [], resultContract?: AppResultContract): AppTestProtocol {
    const fallback = buildAppTestProtocol(kind, outputModes, resultContract);
    if (!value || typeof value !== 'object') return fallback;
    const raw = value as Partial<AppTestProtocol> & Record<string, unknown>;
    const sampleInput = raw.sampleInput ?? raw.sample_input;
    const expectedOutput = raw.expectedOutput ?? raw.expected_output ?? raw.expectedResult ?? raw.expected_result;
    const expectedToolCalls = Array.isArray(raw.expectedToolCalls)
        ? raw.expectedToolCalls.filter((item): item is Record<string, unknown> => !!item && typeof item === 'object' && !Array.isArray(item))
        : Array.isArray(raw.expected_tool_calls)
            ? raw.expected_tool_calls.filter((item): item is Record<string, unknown> => !!item && typeof item === 'object' && !Array.isArray(item))
            : undefined;
    const workflowRequiredInputs = cleanStringList(raw.workflowRequiredInputs ?? raw.workflow_required_inputs ?? fallback.workflowRequiredInputs);
    const workflowDecisionOutputs = cleanStringList(raw.workflowDecisionOutputs ?? raw.workflow_decision_outputs ?? fallback.workflowDecisionOutputs);
    const workflowRequiredOutputs = cleanStringList(raw.workflowRequiredOutputs ?? raw.workflow_required_outputs ?? fallback.workflowRequiredOutputs);
    const normalized: AppTestProtocol = {
        schema: 'maclaw.app.test_protocol.v1',
        sampleInput: plainObjectOrFallback(sampleInput, fallback.sampleInput),
        expectedOutput: plainObjectOrFallback(expectedOutput, fallback.expectedOutput),
        requiredRoles: cleanStringList(raw.requiredRoles ?? raw.required_roles),
        requiredScopes: cleanStringList(raw.requiredScopes ?? raw.required_scopes),
        riskLevel: String((raw.riskLevel ?? raw.risk_level ?? fallback.riskLevel) || '').trim() || fallback.riskLevel,
    };
    if (kind === 'enterprise_approval_app' || workflowRequiredInputs.length) normalized.workflowRequiredInputs = workflowRequiredInputs;
    if (kind === 'enterprise_approval_app' || workflowDecisionOutputs.length) normalized.workflowDecisionOutputs = workflowDecisionOutputs;
    if (kind === 'enterprise_approval_app' || workflowRequiredOutputs.length) normalized.workflowRequiredOutputs = workflowRequiredOutputs;
    if (expectedToolCalls?.length) normalized.expectedToolCalls = expectedToolCalls;
    const fingerprint = String(raw.fingerprint ?? raw.testProtocolFingerprint ?? raw.test_protocol_fingerprint ?? '').trim();
    if (fingerprint) normalized.fingerprint = fingerprint;
    return normalized;
}

function appTestProtocolFingerprint(protocol: AppTestProtocol): string {
    const { fingerprint: _fingerprint, ...stableProtocol } = protocol;
    return textHash(stableStringify(stableProtocol));
}

function appTestProtocolWithFingerprint(protocol: AppTestProtocol): AppTestProtocol {
    return { ...protocol, fingerprint: appTestProtocolFingerprint(protocol) };
}

function appTestProtocolForManifest(app: AppEntry): AppTestProtocol {
    return normalizeAppTestProtocol(app.manifest?.testProtocol, app.kind, app.manifest?.skill?.outputModes || [], appResultContractForManifest(app));
}

function applyAppTestProtocol(manifest: AppManifestBinding, kind: AppKind, override?: AppTestProtocol): AppManifestBinding {
    const outputModes = normalizeOutputModes(manifest.skill?.outputModes);
    const resultContract = normalizeAppResultContract(manifest.resultContract, kind, outputModes);
    return {
        ...manifest,
        testProtocol: appTestProtocolWithFingerprint(normalizeAppTestProtocol(override || manifest.testProtocol, kind, outputModes, resultContract)),
    };
}

function appTestProtocolPublishSummary(app: AppEntry, lang?: string): string {
    const zh = isZh(lang);
    const protocol = appTestProtocolForManifest(app);
    const fingerprint = appTestProtocolFingerprint(protocol);
    const sampleKeys = Object.keys(protocol.sampleInput || {}).length;
    const outputKeys = Object.keys(protocol.expectedOutput || {}).length;
    if (sampleKeys === 0 || outputKeys === 0) return zh ? '\u7f3a\u5c11 sampleInput \u6216 expectedOutput' : 'Missing sampleInput or expectedOutput';
    return `${zh ? '\u534f\u8bae\u6307\u7eb9' : 'Protocol fingerprint'} ${fingerprint} ·  ${protocol.riskLevel}`;
}

function appHasPublishableTestProtocol(app: AppEntry): boolean {
    const protocol = appTestProtocolForManifest(app);
    return protocol.schema === 'maclaw.app.test_protocol.v1' && Object.keys(protocol.sampleInput || {}).length > 0 && Object.keys(protocol.expectedOutput || {}).length > 0 && !!appTestProtocolFingerprint(protocol);
}
function appWorkflowContractForManifest(app: AppEntry): AppWorkflowContract | undefined {
    if (app.kind !== 'enterprise_approval_app') return undefined;
    const binding = appApprovalBinding(app);
    const workflowSkill = app.manifest?.dependencies?.skills?.find((dependency) => dependency.kind === 'workflow_skill' && (!binding?.workflowSkillId || dependency.id === binding.workflowSkillId));
    const workflowSkillId = String(binding?.workflowSkillId || workflowSkill?.id || '').trim();
    const workflowVersion = String(binding?.workflowVersion || workflowSkill?.version || '').trim();
    const objectRole = String(binding?.objectRole || app.manifest?.datasrv?.objectRole || app.manifest?.datasrv?.domain || '').trim();
    const workflow = normalizeAppWorkflowMapping(app.manifest?.workflow, app.kind, app.manifest?.datasrv?.domain || 'business', objectRole || 'record');
    if (!workflowSkillId || !objectRole || !workflow) return undefined;
    return normalizeAppWorkflowContract({
        workflowSkillId,
        workflowVersion: workflowVersion || undefined,
        objectRole,
        requiredInputs: ['record_ref', 'applicant', 'business_payload'],
        decisionOutputs: ['approved', 'rejected', 'attention'],
        requiredOutputs: ['workflow_result', 'approval_instance', 'outputs', 'artifacts'],
        statusMapping: workflow.statusMapping,
    }, app.kind);
}

function workflowContractForApp(app: AppEntry): AppWorkflowContract | undefined {
    return appWorkflowContractForManifest(app) || app.workflowContract;
}

function workflowContractIssueForApp(plan: BackendAppInstallPlan | null | undefined, app: AppEntry): AppReviewIssue | undefined {
    return workflowContractIssuesForAppIDs(plan, appDependencyVerificationAppIDs(app))[0];
}
function workflowContractIssuesForAppIDs(plan: BackendAppInstallPlan | null | undefined, appIDs: string[] = []): AppReviewIssue[] {
    const issues = plan?.workflow_contract_issues || [];
    if (issues.length === 0) return [];
    const selected = new Set(appIDs.flatMap(appInstallIdentityKeys));
    if (selected.size === 0) return issues;
    return issues.filter((issue) => {
        const match = String(issue.path || '').match(/^apps\[(\d+)\]/);
        if (!match) return true;
        const app = plan?.apps?.[Number(match[1])];
        return !!app && appInstallIdentityKeys(app.id).some((key) => selected.has(key));
    });
}

function workflowContractHasIssueForAppIDs(plan: BackendAppInstallPlan | null | undefined, appIDs: string[] = []): boolean {
    const issues = workflowContractIssuesForAppIDs(plan, appIDs);
    if ((plan?.workflow_contract_issues || []).length > 0) return issues.length > 0;
    return !!plan?.has_workflow_contract_issue;
}

function governanceReviewIssuesForAppIDs(plan: BackendAppInstallPlan | null | undefined, appIDs: string[] = []): AppReviewIssue[] {
    const issues = plan?.governance_review_issues || [];
    if (issues.length === 0) return [];
    const selected = new Set(appIDs.flatMap(appInstallIdentityKeys));
    if (selected.size === 0) return issues;
    return issues.filter((issue) => {
        const match = String(issue.path || '').match(/^apps\[(\d+)\]/);
        if (!match) return true;
        const app = plan?.apps?.[Number(match[1])];
        return !!app && appInstallIdentityKeys(app.id).some((key) => selected.has(key));
    });
}

function governanceReviewHasIssueForAppIDs(plan: BackendAppInstallPlan | null | undefined, appIDs: string[] = []): boolean {
    const issues = governanceReviewIssuesForAppIDs(plan, appIDs);
    if ((plan?.governance_review_issues || []).length > 0) return issues.length > 0;
    return !!plan?.has_governance_review_issue;
}

function governanceReviewIssueForApp(plan: BackendAppInstallPlan | null | undefined, app: AppEntry): AppReviewIssue | undefined {
    return governanceReviewIssuesForAppIDs(plan, appDependencyVerificationAppIDs(app))[0];
}

function governanceReviewIssueMessageForAppIDs(plan: BackendAppInstallPlan | null | undefined, appIDs: string[], text: typeof labels.zh): string {
    const issue = governanceReviewIssuesForAppIDs(plan, appIDs)[0];
    if (issue?.message) return `${text.reviewIssues}: ${issue.message}`;
    return text.reviewIssues;
}

function workflowContractIssueMessageForAppIDs(plan: BackendAppInstallPlan | null | undefined, appIDs: string[], text: typeof labels.zh): string {
    const issue = workflowContractIssuesForAppIDs(plan, appIDs)[0];
    if (issue?.message) return `${text.workflowContractBlocked}: ${issue.message}`;
    return text.workflowContractBlocked;
}

function marketInstallFriendlyError(rawMessage: string, plan: BackendAppInstallPlan | null | undefined, text: typeof labels.zh): string {
    if (!rawMessage) return text.installErrorFriendlyGeneric;
    // Prefer structured data from plan if available
    if (plan) {
        if (plan.has_governance_review_issue) return text.installErrorFriendlyGovernance;
        if (plan.has_workflow_contract_issue) return text.installErrorFriendlyWorkflow;
        if (plan.has_blocking_dependency || plan.has_missing_required) return text.installErrorFriendlyDependency;
    }
    // Fallback to message pattern matching for cases where plan is not available (e.g. catch block errors)
    const lower = rawMessage.toLowerCase();
    if (lower.includes('governance') || lower.includes('run evidence') || lower.includes('\u5ba1\u6838')) {
        return text.installErrorFriendlyGovernance;
    }
    if (lower.includes('workflow contract') || lower.includes('\u8fd0\u884c\u5951\u7ea6') || lower.includes('\u8fd0\u884c\u5408\u7ea6')) {
        return text.installErrorFriendlyWorkflow;
    }
    if (lower.includes('dependency') || lower.includes('download_failed')) {
        return text.installErrorFriendlyDependency;
    }
    return text.installErrorFriendlyGeneric;
}

function workflowContractStatus(contract: AppWorkflowContract | undefined, plan: BackendAppInstallPlan | null | undefined, app: AppEntry): 'ready' | 'blocked' | 'missing' {
    if (workflowContractIssueForApp(plan, app) || plan?.has_workflow_contract_issue) return 'blocked';
    return contract ? 'ready' : 'missing';
}
function workflowContractHasIssue(plan: BackendAppInstallPlan | null | undefined, app: AppEntry): boolean {
    return workflowContractHasIssueForAppIDs(plan, appDependencyVerificationAppIDs(app));
}

function runtimeInstallPlanBlocked(plan: BackendAppInstallPlan | null | undefined, app: AppEntry): boolean {
    const appIDs = appDependencyVerificationAppIDs(app);
    return hasMissingRequiredBackendDependency(plan, appIDs) || workflowContractHasIssue(plan, app);
}

function runtimeInstallPlanBlockMessage(app: AppEntry, plan: BackendAppInstallPlan | null | undefined, text: typeof labels.zh, lang?: string): string {
    const issue = workflowContractIssueForApp(plan, app);
    if (issue?.message) return `${text.workflowContractBlocked}: ${issue.message}`;
    if (workflowContractHasIssue(plan, app)) return text.workflowContractBlocked;
    return backendDependencyUnavailableMessage(app, plan, text, lang);
}

function workflowContractSummaryItems(contract: AppWorkflowContract | undefined, text: typeof labels.zh) {
    if (!contract) return [];
    const items: Array<{ label: string; value: string }> = [
        { label: text.workflowSkill, value: [contract.workflowSkillId, contract.workflowVersion ? 'v' + contract.workflowVersion : ''].filter(Boolean).join('@') },
        { label: text.approvalObjectRoleLabel, value: contract.objectRole },
    ];
    if (contract.requiredInputs.length) items.push({ label: text.workflowContractInputs, value: contract.requiredInputs.join(', ') });
    if (contract.decisionOutputs.length) items.push({ label: text.workflowContractOutputs, value: contract.decisionOutputs.join(', ') });
    if (contract.requiredOutputs.length) items.push({ label: text.resultContract, value: contract.requiredOutputs.join(', ') });
    return items.filter((item) => item.value);
}

const WorkflowContractSummary = ({ contract, state, issue, text }: { contract?: AppWorkflowContract; state: 'ready' | 'blocked' | 'missing'; issue?: AppReviewIssue; text: typeof labels.zh }) => {
    const items = workflowContractSummaryItems(contract, text);
    const stateLabel = state === 'blocked' ? text.workflowContractBlocked : state === 'ready' ? text.workflowContractReady : text.workflowContractMissing;
    if (items.length === 0 && state === 'missing') return null;
    return (
        <div className="apps-workflow-contract-summary" data-state={state} role={state === 'blocked' ? 'alert' : 'group'} aria-label={text.workflowContract}>
            <span className="apps-workflow-contract-summary__state">{stateLabel}</span>
            {issue?.message && <span className="apps-workflow-contract-summary__issue">{issue.message}</span>}
            {items.map((item, index) => (
                <span key={item.label + ':' + item.value + ':' + index}>
                    <strong>{item.label}</strong>
                    <em>{item.value}</em>
                </span>
            ))}
        </div>
    );
};

function appWorkflowContractPublishSummary(app: AppEntry, lang?: string) {
    const zh = isZh(lang);
    const contract = workflowContractForApp(app);
    if (!contract) return zh ? '\u672a\u58f0\u660e\u8fd0\u884c\u5951\u7ea6' : 'Runtime contract not declared';
    const workflow = [contract.workflowSkillId, contract.workflowVersion ? `v${contract.workflowVersion}` : ''].filter(Boolean).join('@');
    return [workflow, contract.objectRole, contract.requiredInputs.length ? `${zh ? '\u8f93\u5165' : 'inputs'} ${contract.requiredInputs.length}` : '', contract.decisionOutputs.length ? `${zh ? '\u8f93\u51fa' : 'outputs'} ${contract.decisionOutputs.length}` : ''].filter(Boolean).join(' ·  ');
}
function appResultContractPublishSummary(app: AppEntry, lang?: string) {
    const zh = isZh(lang);
    const contract = appResultContractForManifest(app);
    const types = contract.types.slice(0, 4).join(', ');
    return `${zh ? '\u7ed3\u679c' : 'Result'}: ${contract.primary}${types ? ` ·  ${types}` : ''}`;
}

function appRunEvidencePayloadValue(payload: Record<string, unknown> | undefined, keys: string[]): unknown {
    if (!payload) return undefined;
    for (const key of keys) {
        const value = payload[key];
        if (value !== undefined && value !== null && value !== '') return value;
    }
    return undefined;
}

function appRunEvidenceHasText(value: unknown): boolean {
    return formatApprovalResultValue(value).trim().length > 0;
}

function appRunEvidenceResultTypes(evidence?: AppRunHistoryEntry | null): string[] {
    if (!evidence) return [];
    const covered = new Set<string>();
    const payload = evidence.resultPayload;
    const outputs = normalizeApprovalOutputs(evidence.outputs) || [];
    const artifacts = appRunHistoryArtifacts(evidence);
    const outputMode = String(evidence.outputMode || '').trim().toLowerCase();
    const hasPayloadValue = (keys: string[]) => appRunEvidenceHasText(appRunEvidencePayloadValue(payload, keys));
    const hasOutputKind = (...kinds: string[]) => outputs.some((output) => {
        const kind = approvalOutputKind(output);
        return kinds.some((item) => kind === item || kind.includes(item));
    });
    const hasOutputBody = outputs.some((output) => appRunEvidenceHasText(approvalOutputBody(output)));
    if (artifacts.length > 0 || ['pdf', 'docx', 'xlsx', 'file', 'artifact', 'document'].includes(outputMode) || hasOutputKind('artifact', 'document', 'file')) {
        covered.add('artifact');
        covered.add('document');
    }
    if (hasPayloadValue(['approval_result', 'approvalResult', 'approval_status', 'approvalStatus', 'approval_decision', 'approvalDecision', 'decision']) || hasOutputKind('approval_result', 'approval', 'decision')) {
        covered.add('approval_result');
    }
    if (hasPayloadValue(['business_status', 'businessStatus', 'result_status', 'resultStatus', 'status']) || hasOutputKind('business_status', 'status')) {
        covered.add('business_status');
    }
    if (appRunEvidencePayloadValue(payload, ['business_record', 'businessRecord', 'record']) || hasPayloadValue(['record_id', 'recordID', 'business_record_id', 'businessRecordID']) || hasOutputKind('business_record', 'record')) {
        covered.add('business_record');
    }
    if (hasPayloadValue(['text', 'content', 'message', 'result', 'summary']) || hasOutputBody || hasOutputKind('text', 'content')) {
        covered.add('content');
        covered.add('text');
    }
    if (hasOutputKind('table') || businessOperationRows(payload).length > 1 || Array.isArray(payload?.rows) || Array.isArray(payload?.records) || Array.isArray(payload?.items)) {
        covered.add('table');
    }
    if (hasOutputKind('dashboard') || Array.isArray(payload?.cards) || Array.isArray(payload?.widgets) || Array.isArray(payload?.charts)) {
        covered.add('dashboard');
    }
    if (hasPayloadValue(['notification', 'notifications']) || hasOutputKind('notification')) covered.add('notification');
    if (hasPayloadValue(['receipt', 'external_receipt', 'externalReceipt']) || hasOutputKind('external_receipt', 'receipt')) covered.add('external_receipt');
    if (hasPayloadValue(['action', 'action_result', 'actionResult']) || hasOutputKind('action')) covered.add('action');
    if (String(payload?.status || payload?.kind || '').toLowerCase() === 'requires_input' || hasOutputKind('requires_input')) covered.add('requires_input');
    if (String(payload?.status || payload?.kind || '').toLowerCase() === 'error' || hasOutputKind('error')) covered.add('error');
    return Array.from(covered);
}

function normalizeAppRunApprovalInstanceEvidence(raw: unknown): AppRunApprovalInstanceEvidence | undefined {
    const value = appEvidenceRecord(raw);
    if (!value) return undefined;
    const approvalID = appEvidenceString(value.approvalID, value.approvalId, value.approval_id, value.recordApprovalID, value.record_approval_id);
    const instanceId = appEvidenceString(value.instanceId, value.approvalInstanceId, value.workflowInstanceId, value.instance_id, value.approval_instance_id, value.workflow_instance_id, approvalID);
    const status = appEvidenceString(value.status, value.approvalStatus, value.approval_status, value.resultStatus, value.result_status);
    if (!instanceId) return undefined;
    const approvalViews = appEvidenceRecord(value.approvalViews || value.approval_views);
    const approvalInstanceViewVerified = appEvidenceBool(value.approvalInstanceViewVerified, value.approval_instance_view_verified, value.approvalViewVerified, value.approval_view_verified, value.viewVerified, value.view_verified);
    const currentNodeIDs = parseStringList(firstAppEvidenceValue(value.currentNodeIDs, value.current_node_ids, value.workflowNodeIDs, value.workflow_node_ids, value.workflowNodes, value.workflow_nodes));
    const currentNode = appEvidenceString(value.currentNode, value.current_node, currentNodeIDs[0]) || undefined;
    const progressRaw = firstAppEvidenceValue(value.progressInstances, value.progress_instances, value.workflowProgress, value.workflow_progress, value.approvalProgress, value.approval_progress);
    const progressItems = Array.isArray(progressRaw) ? progressRaw : progressRaw && typeof progressRaw === 'object' ? [progressRaw] : [];
    const progressInstances = progressItems.map((item) => normalizeAppRunApprovalInstanceEvidence(item)).filter(Boolean) as AppRunApprovalInstanceEvidence[];
    return {
        instanceId,
        approvalInstanceId: appEvidenceString(value.approvalInstanceId, value.approval_instance_id) || instanceId,
        approvalID: approvalID || undefined,
        workflowInstanceId: appEvidenceString(value.workflowInstanceId, value.workflow_instance_id) || instanceId,
        status,
        lane: appEvidenceString(value.lane, value.approvalLane, value.approval_lane) || undefined,
        currentNode,
        currentNodeIDs: currentNodeIDs.length > 0 ? currentNodeIDs : currentNode ? [currentNode] : undefined,
        datasetID: appEvidenceString(value.datasetID, value.dataset_id) || undefined,
        blueprintID: appEvidenceString(value.blueprintID, value.blueprint_id) || undefined,
        objectRole: appEvidenceString(value.objectRole, value.object_role) || undefined,
        approvalObjectRole: appEvidenceString(value.approvalObjectRole, value.approval_object_role) || undefined,
        approvalEvent: appEvidenceString(value.approvalEvent, value.approval_event) || undefined,
        approvalWorkflowID: appEvidenceString(value.approvalWorkflowID, value.approvalWorkflowId, value.approval_workflow_id) || undefined,
        workflowSkillId: appEvidenceString(value.workflowSkillId, value.workflow_skill_id) || undefined,
        workflowVersion: appEvidenceString(value.workflowVersion, value.workflow_version) || undefined,
        businessStatus: appEvidenceString(value.businessStatus, value.business_status) || undefined,
        resultStatus: appEvidenceString(value.resultStatus, value.result_status) || undefined,
        result: appEvidenceString(value.result, value.message) || undefined,
        recordID: appEvidenceString(value.recordID, value.record_id) || undefined,
        detailURL: appEvidenceString(value.detailURL, value.detailUrl, value.detail_url) || undefined,
        resultPayload: appEvidenceRecord(value.resultPayload || value.result_payload),
        outputs: Array.isArray(value.outputs) ? value.outputs as ApprovalInstanceOutputView[] : undefined,
        artifacts: Array.isArray(value.artifacts) ? value.artifacts as ApprovalInstanceArtifactView[] : undefined,
        viewVerified: appEvidenceBool(value.viewVerified, value.view_verified),
        approvalInstanceViewVerified,
        approvalViews,
        progressInstances: progressInstances.length > 0 ? progressInstances : undefined,
        verifiedAt: appEvidenceString(value.verifiedAt, value.verified_at) || undefined,
    };
}

function approvalViewsHaveVerifiedLane(value: Record<string, unknown> | undefined): boolean {
    if (!value) return false;
    if (appEvidenceBool(value.verified, value.ok) === true) return true;
    return ['my_requests', 'pending_my_approval', 'handled', 'attention', 'all'].some((key) => value[key] !== undefined && value[key] !== null);
}

function appRunApprovalInstanceEvidenceFromBackend(instance: BackendApprovalInstance, verifiedAt: string): AppRunApprovalInstanceEvidence | undefined {
    const instanceId = String(instance.instance_id || instance.approval_id || instance.record_approval_id || '').trim();
    const approvalID = String(instance.approval_id || instance.record_approval_id || '').trim();
    if (!instanceId) return undefined;
    return {
        instanceId,
        approvalInstanceId: instanceId,
        approvalID: approvalID || undefined,
        workflowInstanceId: instanceId,
        status: String(instance.status || instance.result_status || '').trim(),
        lane: String(instance.lane || '').trim() || undefined,
        currentNode: String(instance.current_node || '').trim() || undefined,
        currentNodeIDs: normalizeApprovalCurrentNodeIDs(instance),
        datasetID: String(instance.dataset_id || '').trim() || undefined,
        blueprintID: String(instance.blueprint_id || '').trim() || undefined,
        objectRole: String(instance.object_role || '').trim() || undefined,
        approvalObjectRole: String(instance.approval_object_role || instance.object_role || '').trim() || undefined,
        approvalEvent: String(instance.approval_event || '').trim() || undefined,
        approvalWorkflowID: String(instance.approval_workflow_id || '').trim() || undefined,
        workflowSkillId: String(instance.workflow_skill_id || '').trim() || undefined,
        workflowVersion: String(instance.workflow_version || '').trim() || undefined,
        businessStatus: String(instance.business_status || '').trim() || undefined,
        resultStatus: String(instance.result_status || '').trim() || undefined,
        result: String(instance.result || '').trim() || undefined,
        recordID: String(instance.record_id || '').trim() || undefined,
        detailURL: String(instance.detail_url || '').trim() || undefined,
        resultPayload: instance.result_payload && typeof instance.result_payload === 'object' ? instance.result_payload : undefined,
        outputs: Array.isArray(instance.outputs) ? instance.outputs : undefined,
        artifacts: Array.isArray(instance.artifacts) ? instance.artifacts : undefined,
        viewVerified: true,
        approvalInstanceViewVerified: true,
        approvalViews: { my_requests: true, all: true, verified: true },
        verifiedAt,
	};
}

function approvalDecisionRunHistoryEntry(app: AppEntry | undefined, instance: BackendApprovalInstance, message: string, verifiedAt = new Date().toISOString()): AppRunHistoryEntry | null {
    if (!app?.id) return null;
    const approvalInstance = appRunApprovalInstanceEvidenceFromBackend(instance, verifiedAt);
    if (!approvalInstance) return null;
    const resultPayload = instance.result_payload && typeof instance.result_payload === 'object' && !Array.isArray(instance.result_payload)
        ? instance.result_payload as Record<string, unknown>
        : {};
    const entry: AppRunHistoryEntry = {
        runID: String(instance.workflow_decision_id || instance.approval_id || instance.record_approval_id || instance.instance_id || `approval-decision-${Date.now().toString(36)}`).trim(),
        appID: app.id,
        at: verifiedAt,
        status: 'done',
        definitionHash: appDefinitionFingerprint(app),
        testProtocolFingerprint: appTestProtocolFingerprint(appTestProtocolForManifest(app)),
        workspaceLayoutFingerprint: appWorkspaceLayoutEvidence(app).fingerprint,
        outputMode: 'approval',
        inputSummary: String(instance.title || instance.business_action || 'approval decision').trim(),
        message: String(message || instance.result || instance.status || 'approval decision').trim(),
        resultPayload,
        outputs: normalizeApprovalOutputs(instance.outputs as any),
        artifacts: normalizeApprovalArtifacts(instance.artifacts as any),
        approvalInstance,
    };
    const coverage = appRunEvidenceContractCoverage(app, entry);
    entry.resultCoverage = {
        ok: coverage.ok,
        primary: coverage.primary,
        coveredTypes: coverage.coveredTypes,
        missingTypes: coverage.missingTypes,
    };
    return entry;
}

function saveApprovalDecisionRunEvidence(app: AppEntry | undefined, instance: BackendApprovalInstance, message: string): AppRunHistoryEntry | null {
    const entry = approvalDecisionRunHistoryEntry(app, instance, message);
    if (!entry || !app?.id) return entry;
    const next = [entry, ...loadAppRunHistory(app.id).filter((item) => item.runID !== entry.runID)].slice(0, 8);
    saveAppRunHistory(app.id, next);
    return entry;
}

function appRunEvidenceApprovalInstanceCheck(app: AppEntry, evidence: AppRunHistoryEntry | null, lang?: string) {
    const zh = isZh(lang);
    if (!isEnterpriseApprovalAppKind(app.kind)) {
        return { ok: true, detail: zh ? '\u975e\u5ba1\u6279\u5e94\u7528\u65e0\u9700\u5ba1\u6279\u5b9e\u4f8b\u8bc1\u636e' : 'Not required for non-approval apps' };
    }
    const approvalInstance = normalizeAppRunApprovalInstanceEvidence(evidence?.approvalInstance);
    const hasStatus = !!String(approvalInstance?.status || '').trim();
    const hasCurrentNode = !!String(approvalInstance?.currentNode || '').trim();
    const hasWorkflowSkill = !!String(approvalInstance?.workflowSkillId || '').trim();
    const hasResultStatus = !!String(approvalInstance?.resultStatus || approvalInstance?.businessStatus || '').trim();
    const hasResultPackage = !!(approvalInstance?.resultPayload && Object.keys(approvalInstance.resultPayload).length > 0)
        || !!(approvalInstance?.outputs && approvalInstance.outputs.length > 0)
        || !!(approvalInstance?.artifacts && approvalInstance.artifacts.length > 0);
    const viewVerified = approvalInstance?.approvalInstanceViewVerified === true || approvalInstance?.viewVerified === true || approvalViewsHaveVerifiedLane(approvalInstance?.approvalViews);
    const ok = !!approvalInstance?.instanceId && hasStatus && hasCurrentNode && hasWorkflowSkill && hasResultStatus && hasResultPackage && viewVerified;
    const missingDetail = () => {
        if (!approvalInstance?.instanceId || !hasStatus || !viewVerified) {
            return zh ? '\u7f3a\u5c11 instanceId\u3001status \u6216\u5ba1\u6279\u5b9e\u4f8b\u89c6\u56fe\u9a8c\u8bc1' : 'Missing instanceId, status, or approval instance view verification';
        }
        if (!hasCurrentNode || !hasWorkflowSkill || !hasResultStatus) {
            return zh ? '\u7f3a\u5c11 currentNode\u3001workflowSkillId \u6216 businessStatus/resultStatus' : 'Missing currentNode, workflowSkillId, or businessStatus/resultStatus';
        }
        return zh ? '\u5ba1\u6279\u5b9e\u4f8b\u8bc1\u636e\u7f3a\u5c11 resultPayload\u3001outputs \u6216 artifacts' : 'Approval instance evidence is missing resultPayload, outputs, or artifacts';
    };
    return {
        ok,
        approvalInstance,
        detail: !evidence
            ? (zh ? '\u8bf7\u5148\u8fd0\u884c\u4e00\u6b21\u5ba1\u6279\u5de5\u4f5c\u6d41\u518d\u8bc4\u5ba1' : 'Run the approval workflow once before review')
            : ok
                ? `${approvalInstance?.instanceId} / ${approvalInstance?.status}${approvalInstance?.currentNode ? ` / ${approvalInstance.currentNode}` : ''}`
                : missingDetail(),
    };
}
function appRunEvidenceContractCoverage(app: AppEntry, evidence: AppRunHistoryEntry | null, lang?: string) {
    const zh = isZh(lang);
    const contract = appResultContractForManifest(app);
    const coveredTypes = appRunEvidenceResultTypes(evidence);
    const primary = String(contract.primary || '').trim();
    const explicitCoverage = evidence?.resultCoverage && typeof evidence.resultCoverage === 'object' ? evidence.resultCoverage as Record<string, unknown> : undefined;
    const explicitMissingTypes = parseStringList(firstAppEvidenceValue(explicitCoverage?.missingTypes, explicitCoverage?.missing_types));
    const explicitCoveredTypes = parseStringList(firstAppEvidenceValue(explicitCoverage?.coveredTypes, explicitCoverage?.covered_types));
    const effectiveCoveredTypes = explicitCoveredTypes.length ? explicitCoveredTypes : coveredTypes;
    const covered = new Set(effectiveCoveredTypes);
    const primaryCovered = !primary || covered.has(primary) || (primary === 'document' && covered.has('artifact')) || (primary === 'artifact' && covered.has('document')) || (primary === 'text' && covered.has('content')) || (primary === 'content' && covered.has('text'));
    const missingTypes = explicitMissingTypes.length ? explicitMissingTypes : primaryCovered || !primary ? [] : [primary];
    const evidenceLabel = evidence ? `${evidence.runID || ''} ·  ${evidence.at || ''}`.trim() : '';
    const coveredLabel = effectiveCoveredTypes.length > 0 ? effectiveCoveredTypes.join(', ') : (zh ? '\u672a\u8bc6\u522b\u7ed3\u679c\u7c7b\u578b' : 'No result type recognized');
    return {
        ok: !!evidence && primaryCovered && missingTypes.length === 0,
        primary,
        coveredTypes: effectiveCoveredTypes,
        missingTypes,
        detail: !evidence
            ? (zh ? '\u63d0\u4ea4\u5ba1\u6838\u524d\u5efa\u8bae\u5148\u8bd5\u8fd0\u884c\u4e00\u6b21' : 'Run the app once before review')
            : primaryCovered && missingTypes.length === 0
                ? `${evidenceLabel}${primary ? ` ·  primary: ${primary}` : ''} ·  ${zh ? '\u8986\u76d6' : 'covered'}: ${coveredLabel}`
                : `${evidenceLabel} ·  ${zh ? '\u8fd0\u884c\u8bc1\u636e\u672a\u8986\u76d6\u7ed3\u679c\u5951\u7ea6' : 'Run evidence does not cover result contract'}: ${missingTypes.join(', ')}`,
    };
}
function defaultAppWorkflowMapping(kind: AppKind, domain = 'business', objectRole = 'record'): AppWorkflowMapping | undefined {
    if (kind !== 'enterprise_approval_app') return undefined;
    const role = String(objectRole || domain || 'record').trim() || 'record';
    return {
        schema: 'maclaw.app.workflow.v1',
        submitNode: `${role}.submit`,
        approvalNode: `${role}.manager_approval`,
        resultNode: `${role}.result_feedback`,
        attentionNode: `${role}.attention_review`,
        statusMapping: {
            pending: 'approval_pending',
            approved: 'approved',
            rejected: 'rejected',
            attention: 'attention',
            requiresInput: 'requires_input',
        },
    };
}
function normalizeAppWorkflowMapping(value: unknown, kind: AppKind, domain = 'business', objectRole = 'record'): AppWorkflowMapping | undefined {
    const fallback = defaultAppWorkflowMapping(kind, domain, objectRole);
    if (!fallback) return undefined;
    if (!value || typeof value !== 'object') return fallback;
    const raw = value as Partial<AppWorkflowMapping> & { submit_node?: unknown; approval_node?: unknown; result_node?: unknown; attention_node?: unknown; status_mapping?: unknown };
    const statusRaw = raw.statusMapping && typeof raw.statusMapping === 'object'
        ? raw.statusMapping as Partial<AppWorkflowMapping['statusMapping']>
        : raw.status_mapping && typeof raw.status_mapping === 'object'
            ? raw.status_mapping as Partial<AppWorkflowMapping['statusMapping']>
            : {};
    const clean = (item: unknown, fallbackValue: string) => String(item || '').trim() || fallbackValue;
    return {
        schema: 'maclaw.app.workflow.v1',
        submitNode: clean(raw.submitNode ?? raw.submit_node, fallback.submitNode),
        approvalNode: clean(raw.approvalNode ?? raw.approval_node, fallback.approvalNode),
        resultNode: clean(raw.resultNode ?? raw.result_node, fallback.resultNode),
        attentionNode: clean(raw.attentionNode ?? raw.attention_node, fallback.attentionNode || fallback.approvalNode),
        statusMapping: {
            pending: clean(statusRaw.pending, fallback.statusMapping.pending),
            approved: clean(statusRaw.approved, fallback.statusMapping.approved),
            rejected: clean(statusRaw.rejected, fallback.statusMapping.rejected),
            attention: clean(statusRaw.attention, fallback.statusMapping.attention),
            requiresInput: clean(statusRaw.requiresInput ?? (statusRaw as any).requires_input, fallback.statusMapping.requiresInput || 'requires_input'),
        },
    };
}
function applyAppWorkflowMapping(manifest: AppManifestBinding, kind: AppKind, value?: AppWorkflowMapping): AppManifestBinding {
    const workflow = normalizeAppWorkflowMapping(value || manifest.workflow, kind, manifest.datasrv?.domain || 'business', manifest.datasrv?.objectRole || manifest.datasrv?.domain || 'record');
    if (!workflow) {
        const { workflow: _workflow, ...rest } = manifest;
        return rest;
    }
    return { ...manifest, workflow };
}
function appNeedsRuntimeDependencyCheck(app: AppEntry) {
    if (app.source !== 'market' && app.source !== 'skill' && app.source !== 'local' && app.source !== 'datasrv') return false;
    return appDependencyEvidence(app).length > 0;
}
function appNeedsAutomaticRuntimeDependencyCheck(app: AppEntry) {
    return (app.source === 'market' || app.source === 'skill' || app.source === 'datasrv' || app.studioOrigin === 'app_studio') && appDependencyEvidence(app).length > 0;
}
function appGovernanceForManifest(app: AppEntry, submission?: AppPublishSubmission, overrides: AppGovernanceOverrides = {}) {
    const evidence = latestAppRunEvidence(app);
    const evidenceArtifacts = appRunHistoryArtifacts(evidence);
    const evidenceOutputs = normalizeApprovalOutputs(evidence?.outputs) || [];
    const resultContract = appResultContractForManifest(app);
    const testProtocol = appTestProtocolWithFingerprint(appTestProtocolForManifest(app));
    const testProtocolFingerprint = appTestProtocolFingerprint(testProtocol);
    const workspaceLayout = appWorkspaceLayoutEvidence(app);
    const primaryResult = appRunPrimaryResultFromPayload(resultContract, evidence?.resultPayload, evidenceOutputs);
    const resultCoverage = appRunEvidenceContractCoverage(app, evidence);
    const designConsistency = appRunEvidenceDesignConsistencyCheck(app, evidence, undefined);
    const approvalInstanceEvidence = normalizeAppRunApprovalInstanceEvidence(evidence?.approvalInstance);
    const primaryArtifact = evidenceArtifacts[0];
    const artifactName = primaryArtifact?.name || (primaryArtifact?.path ? primaryArtifact.path.split(/[\\/]/).pop() : undefined);
    const dependencies = appDependencyEvidence(app);
    const dependencyVerificationPlan = overrides.dependencyVerification || appInstallEvidenceDependencyVerificationPlan(app);
    const dependencyVerificationEvidence = dependencyVerificationPlan ? appRunDependencyVerificationEvidence(app, dependencyVerificationPlan) : undefined;
    return {
        status: submission?.status || (evidence ? 'local_tested' : 'draft'),
        riskLevel: submission?.riskLevel || (isEnterpriseAppKind(app.kind) ? 'medium' : 'low'),
        requiredScopes: appRequiredScopes(app),
        dependencies: {
            installPolicy: 'install_on_app_install',
            skills: dependencies,
            requiredCount: dependencies.filter((dep) => dep.required).length,
            optionalCount: dependencies.filter((dep) => !dep.required).length,
        },
        dependencyVerification: dependencyVerificationEvidence,
        workspaceLayout,
        resultContract,
        testProtocol,
        workflowContract: appWorkflowContractForManifest(app),
        testEvidence: evidence ? {
            runId: evidence?.runID,
            definitionHash: evidence?.definitionHash,
            workspaceLayoutFingerprint: evidence?.workspaceLayoutFingerprint,
            currentWorkspaceLayoutFingerprint: workspaceLayout.fingerprint,
            artifactPresent: evidenceArtifacts.length > 0,
            artifactCount: evidenceArtifacts.length,
            artifactName,
            artifacts: evidenceArtifacts.map(appRunHistoryArtifactEvidence),
            outputCount: evidenceOutputs.length,
            outputs: evidenceOutputs.map(appRunHistoryOutputEvidence),
            resultPayload: evidence?.resultPayload,
            testProtocol,
            testProtocolFingerprint,
            dependencyVerification: evidence?.dependencyVerification,
            approvalInstance: approvalInstanceEvidence,
            primaryResult: primaryResult || undefined,
            resultCoverage: {
                ok: resultCoverage.ok,
                primary: resultCoverage.primary,
                coveredTypes: resultCoverage.coveredTypes,
                missingTypes: resultCoverage.missingTypes,
            },
            designConsistency: {
                ok: designConsistency.ok,
                detail: designConsistency.detail,
            },
            verifiedAt: evidence?.at || undefined,
        } : undefined,
        submission: submission ? {
            id: submission.id,
            status: submission.status,
            channel: submission.channel,
            submittedAt: submission.submittedAt,
            reviewedAt: submission.reviewedAt,
            publishedAt: submission.publishedAt,
            reviewer: submission.reviewer,
            riskLevel: submission.riskLevel,
            approvedScopes: submission.approvedScopes,
            reviewIssues: submission.reviewIssues,
            modifiedAt: submission.modifiedAt,
            version: submission.version || normalizeAppVersion(app.version),
            message: submission.message,
        } : undefined,
    };
}

function appRequiredScopes(app: AppEntry): string[] {
    const scopes = isEnterpriseAppKind(app.kind)
        ? [app.manifest?.datasrv?.preferredAction, app.manifest?.datasrv?.preferredView, app.manifest?.datasrv?.preferredReport, app.manifest?.datasrv?.preferredDashboard]
        : app.kind === 'tool_app'
            ? [app.manifest?.skill?.id]
            : [];
    return Array.from(new Set(scopes.map((scope) => String(scope || '').trim()).filter(Boolean)));
}

function highRiskScopes(scopes: string[]) {
    return scopes.filter((scope) => /finance|payment|admin|audit|approve|delete|write|upsert|commit/i.test(scope));
}

function formatRunHistoryTime(iso: string) {
    const date = new Date(iso);
    if (Number.isNaN(date.getTime())) return iso;
    return date.toLocaleString();
}

function appRunHistoryResultSummary(item: AppRunHistoryEntry, text: typeof labels.zh) {
    const payload = item.resultPayload;
    const primaryValue = payload?.business_status || payload?.result_status || payload?.text || payload?.content || payload?.message;
    const primaryText = formatApprovalResultValue(primaryValue).trim();
    if (primaryText) return primaryText.slice(0, 160);
    const output = item.outputs?.find((entry) => approvalOutputBody(entry).trim());
    if (output) {
        const title = approvalOutputTitle(output, text);
        const body = approvalOutputBody(output).trim();
        return [title, body.slice(0, 120)].filter(Boolean).join(': ');
    }
    return '';
}

function appRunHistoryShortArtifactLabel(value: unknown) {
    const text = String(value || '').trim();
    if (!text) return '';
    if (text.startsWith('artifact://')) {
        return text.split('/').filter(Boolean).pop() || text;
    }
    return text.split(/[\\/]/).pop() || text;
}

function appRunHistoryEvidenceSummary(item: AppRunHistoryEntry) {
    const artifactLabels = (item.artifacts || [])
        .map((artifact) => {
            const raw = artifact.name || artifact.path || artifact.uri || artifact.remote_url || artifact.id || '';
            return appRunHistoryShortArtifactLabel(raw);
        })
        .filter(Boolean);
    return Array.from(new Set([
        appRunHistoryShortArtifactLabel(item.artifactName),
        ...artifactLabels,
    ].map((value) => String(value || '').trim()).filter(Boolean))).slice(0, 4);
}

function appRunHistoryPrimaryArtifact(item: AppRunHistoryEntry) {
    const primary = appRunHistoryArtifacts(item)[0];
    return {
        id: item.artifactID || primary?.id || primary?.artifact_id || '',
        uri: item.artifactURI || primary?.uri || primary?.remote_url || '',
        path: item.artifactPath || primary?.path || '',
        name: item.artifactName || primary?.name || '',
    };
}

function appRunHistoryArtifactLabel(item: AppRunHistoryEntry) {
    const primary = appRunHistoryPrimaryArtifact(item);
    const raw = primary.name || primary.uri || primary.path || primary.id;
    return appRunHistoryShortArtifactLabel(raw);
}

function appRunHistoryTechnicalDetails(item: AppRunHistoryEntry) {
    const message = String(item.message || '').trim();
    const primaryArtifact = appRunHistoryPrimaryArtifact(item);
    const details: Array<{ label: string; value: string }> = [];
    const addDetail = (label: string, value?: string) => {
        const clean = String(value || '').trim();
        if (clean && !details.some((item) => item.label === label && item.value === clean)) {
            details.push({ label, value: clean });
        }
    };
    const shell = message.match(/\bshell:\s*([^\s,;]+)/i)?.[1];
    const elapsed = message.match(/\belapsed:\s*([^\s,;]+)/i)?.[1];
    const commandIndex = message.toLowerCase().indexOf(' command: ');
    if (shell) addDetail('shell', shell);
    if (elapsed) addDetail('elapsed', elapsed);
    const beforeCommand = commandIndex >= 0 ? message.slice(0, commandIndex) : message;
    const path = beforeCommand.match(/[A-Za-z]:\\[\s\S]+$/)?.[0]?.replace(/^[^\wA-Za-z:]+/, '').trim();
    addDetail('path', path);
    if (commandIndex >= 0) addDetail('command', message.slice(commandIndex + ' command: '.length));
    if (message && details.length === 0) addDetail('message', message);
    addDetail('runID', item.runID);
    addDetail('artifactID', primaryArtifact.id);
    addDetail('artifactPath', primaryArtifact.path);
    if (primaryArtifact.uri && primaryArtifact.uri !== primaryArtifact.path) addDetail('artifactURI', primaryArtifact.uri);
    return details;
}

function appRunHistoryMessageSummary(item: AppRunHistoryEntry) {
    const message = String(item.message || item.status || '').trim();
    if (!message) return item.status;
    const shell = message.match(/\bshell:\s*([^\s,;]+)/i)?.[1];
    const elapsed = message.match(/\belapsed:\s*([^\s,;]+)/i)?.[1];
    if (shell || elapsed) return [shell ? `shell: ${shell}` : '', elapsed ? `elapsed: ${elapsed}` : ''].filter(Boolean).join(' · ');
    return message;
}

const AppRunHistoryItem = ({ item, title, text, showInputSummary = true }: { item: AppRunHistoryEntry; title: string; text: typeof labels.zh; showInputSummary?: boolean }) => {
    const resultSummary = appRunHistoryResultSummary(item, text);
    const evidenceSummary = appRunHistoryEvidenceSummary(item);
    const primaryArtifact = appRunHistoryPrimaryArtifact(item);
    const artifactLabel = appRunHistoryArtifactLabel(item);
    const details = appRunHistoryTechnicalDetails(item);
    const artifactRef = primaryArtifact.id || primaryArtifact.uri;
    const canUseArtifact = item.status === 'done';
    const hasArtifact = canUseArtifact && !!(primaryArtifact.uri || primaryArtifact.path || primaryArtifact.id);
    const artifactLabelCanReveal = canUseArtifact && !!(primaryArtifact.uri || primaryArtifact.path);
    return (
        <article className="apps-run-history__item" data-state={item.status}>
            <div className="apps-run-history__main">
                <div className="apps-run-history__headline">
                    <strong>{title}</strong>
                    <em>{appRunHistoryMessageSummary(item)}</em>
                </div>
                <div className="apps-run-history__meta" aria-label={text.runHistory}>
                    <span>{formatRunHistoryTime(item.at)}</span>
                    <span>{String(item.outputMode || '').toUpperCase()}</span>
                    <span>{item.status}</span>
                </div>
                {showInputSummary && item.inputSummary && title !== item.inputSummary && <p className="apps-run-history__input">{item.inputSummary}</p>}
                {resultSummary && <p className="apps-run-history__result">{resultSummary}</p>}
                {evidenceSummary.length > 0 && (
                    <div className="apps-run-history__evidence">
                        {evidenceSummary.map((entry) => <span key={entry}>{entry}</span>)}
                    </div>
                )}
                {artifactLabel && artifactLabelCanReveal && (
                    <button className="apps-run-history__artifact apps-output-path-link" type="button" onClick={() => void revealSkillRunArtifactFromUI(item.runID, artifactRef, primaryArtifact.path)}>
                        {artifactLabel}
                    </button>
                )}
                {artifactLabel && !artifactLabelCanReveal && <span className="apps-run-history__artifact apps-run-history__artifact--plain">{artifactLabel}</span>}
                {details.length > 0 && (
                    <details className="apps-run-history__details">
                        <summary>{text.runDetails}</summary>
                        <dl>
                            {details.map((detail) => (
                                <div key={`${detail.label}:${detail.value}`}>
                                    <dt>{detail.label}</dt>
                                    <dd>{detail.value}</dd>
                                </div>
                            ))}
                        </dl>
                    </details>
                )}
            </div>
            {hasArtifact && (
                <div className="apps-run-history__actions">
                    <button className="apps-link-button" type="button" onClick={() => void openSkillRunArtifactFromUI(item.runID, artifactRef, primaryArtifact.path, item.artifactDownloadState === 'remote')}>{item.artifactDownloadState === 'remote' ? text.downloadArtifact : text.openArtifact}</button>
                    <button className="apps-link-button" type="button" onClick={() => void revealSkillRunArtifactFromUI(item.runID, artifactRef, primaryArtifact.path)}>{text.revealArtifact}</button>
                </div>
            )}
        </article>
    );
};

function formatRecentUsedAt(value?: string) {
    const timestamp = Number(String(value || '').slice(0, 13));
    if (!Number.isFinite(timestamp) || timestamp <= 0) return '';
    return new Date(timestamp).toLocaleString();
}

function appPanelStatusLabel(app: AppEntry, lang?: string) {
    if (app.source !== 'skill') return '';
    return isZh(lang) ? '\u5df2\u52a0\u5165\u9762\u677f' : 'In panel';
}

function buildAppTileTooltip(app: AppEntry, text: typeof labels.zh, statusLabel: string, lang?: string) {
    const recent = formatRecentUsedAt(app.recentUsedAt);
    const panelStatus = appPanelStatusLabel(app, lang);
    return [
        app.name,
        app.description,
        [appKinds[app.kind][isZh(lang) ? 'zh' : 'en'], sourceLabels[app.source][isZh(lang) ? 'zh' : 'en'], panelStatus].filter(Boolean).join(' · '),
        `${text.appStatus}: ${statusLabel}`,
        `${text.recentUsed}: ${recent || text.neverUsed}`,
    ].filter(Boolean).join('\n');
}

function buildAppTileAriaLabel(app: AppEntry, text: typeof labels.zh, statusLabel: string, lang?: string) {
    const locale = isZh(lang) ? 'zh' : 'en';
    return [app.name, appKinds[app.kind][locale], sourceLabels[app.source][locale], appPanelStatusLabel(app, lang), `${text.appStatus}: ${statusLabel}`].filter(Boolean).join(', ');
}

function buildAppSearchText(app: AppEntry, lang?: string) {
    const manifest = app.manifest;
    const dependencies = appSkillDependencies(app);
    const approvalBindings = manifest?.mis?.approvalBindings || [];
    return [
        app.id,
        app.name,
        app.description,
        app.category,
        appKinds[app.kind][isZh(lang) ? 'zh' : 'en'],
        app.kind,
        sourceLabels[app.source][isZh(lang) ? 'zh' : 'en'],
        app.source,
        appPanelStatusLabel(app, lang),
        manifest?.launchMode,
        manifest?.installUnit,
        manifest?.appSkill?.id,
        manifest?.appSkill?.source,
        manifest?.datasrv?.appID,
        manifest?.datasrv?.domain,
        manifest?.datasrv?.datasetID,
        manifest?.datasrv?.objectRole,
        manifest?.datasrv?.blueprintID,
        manifest?.datasrv?.templateID,
        manifest?.datasrv?.preferredAction,
        manifest?.datasrv?.preferredView,
        manifest?.datasrv?.preferredReport,
        manifest?.datasrv?.preferredDashboard,
        manifest?.workflow?.submitNode,
        manifest?.workflow?.approvalNode,
        manifest?.workflow?.resultNode,
        ...Object.values(manifest?.workflow?.statusMapping || {}),
        ...approvalBindings.flatMap((binding) => [binding.event, binding.workflowSkillId, binding.workflowVersion, binding.objectRole]),
        ...dependencies.flatMap((dependency) => [dependency.id, dependency.version, dependency.kind, dependency.source, dependency.installRef, ...(dependency.capabilities || [])]),
        manifest?.resultContract?.primary,
        ...(manifest?.resultContract?.types || []),
        manifest?.skill?.id,
        manifest?.skill?.inputMode,
        ...(manifest?.skill?.outputModes || []),
        ...(manifest?.skill?.fields || []).flatMap((field) => [field.name, field.label, field.type, ...(field.options || [])]),
    ].filter(Boolean).join(' ').toLowerCase();
}

function makeLocalAppId(name: string) {
    const slug = name.toLowerCase().replace(/[^a-z0-9]+/gi, '-').replace(/^-+|-+$/g, '').slice(0, 28);
    localAppIdSequence += 1;
    return `local-app-${Date.now().toString(36)}-${localAppIdSequence.toString(36)}-${slug || 'app'}`;
}

function makeSkillAppDefinitionId(name: string) {
    const slug = name.toLowerCase().replace(/[^a-z0-9]+/gi, '-').replace(/^-+|-+$/g, '').slice(0, 48);
    return `app-${slug || 'tool-app'}`;
}

function makeDuplicateAppName(sourceName: string, suffix: string, existingNames: Set<string>) {
    const baseName = `${sourceName} ${suffix}`;
    if (!existingNames.has(baseName)) return baseName;
    for (let index = 2; index < 1000; index += 1) {
        const candidate = `${baseName} ${index}`;
        if (!existingNames.has(candidate)) return candidate;
    }
    return `${baseName} ${Date.now().toString(36)}`;
}

function appWithAvailablePin(app: AppEntry, currentApps: AppEntry[]) {
    if (!app.pinned) return app;
    if (currentApps.filter((item) => item.pinned).length < maxPinnedApps) return app;
    return { ...app, pinned: false };
}

function countAppsByCategory(apps: AppEntry[]) {
    const counts = new Map<string, number>();
    apps.forEach((app) => counts.set(app.category, (counts.get(app.category) || 0) + 1));
    return counts;
}

function categoryOptionLabel(category: string, counts: Map<string, number>) {
    return `${category} (${counts.get(category) || 0})`;
}

function filterSummaryText({ query, category, count, lang, allLabel }: { query: string; category: string; count: number; lang?: string; allLabel: string }) {
    const trimmedQuery = query.trim();
    const zh = isZh(lang);
    const categoryText = category === 'all' ? allLabel : category;
    if (trimmedQuery && category !== 'all') {
        return zh ? `\u641c\u7d22\u201c${trimmedQuery}\u201d · ${categoryText} · ${count} \u4e2a\u5339\u914d` : `Search "${trimmedQuery}" · ${categoryText} · ${count} matches`;
    }
    if (trimmedQuery) {
        return zh ? `\u641c\u7d22\u201c${trimmedQuery}\u201d · ${count} \u4e2a\u5339\u914d` : `Search "${trimmedQuery}" · ${count} matches`;
    }
    if (category !== 'all') {
        return zh ? `${categoryText} · ${count} \u4e2a\u5e94\u7528` : `${categoryText} · ${count} apps`;
    }
    return '';
}
function normalizeSkillAppIcon(icon?: string): AppIconName {
    const normalized = String(icon || '').trim() as AppIconName;
    return appIconNames.includes(normalized) ? normalized : 'contract';
}

function normalizeCustomIconDataUrl(value: unknown): string | undefined {
    const icon = String(value || '').trim();
    return /^data:image\/(?:png|jpeg|webp);base64,[a-z0-9+/=]+$/i.test(icon) && icon.length < 260_000 ? icon : undefined;
}

async function fileToAppIconDataUrl(file: File, size = 96): Promise<string> {
    if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) throw new Error('unsupported image');
    if (file.size > maxAppIconUploadBytes) throw new Error('image too large');
    const objectUrl = URL.createObjectURL(file);
    try {
        const image = await new Promise<HTMLImageElement>((resolve, reject) => {
            const img = new Image();
            img.onload = () => resolve(img);
            img.onerror = () => reject(new Error('image load failed'));
            img.src = objectUrl;
        });
        const canvas = document.createElement('canvas');
        canvas.width = size;
        canvas.height = size;
        const context = canvas.getContext('2d');
        if (!context) throw new Error('canvas unavailable');
        context.clearRect(0, 0, size, size);
        const sourceSize = Math.min(image.naturalWidth || image.width, image.naturalHeight || image.height);
        const sourceX = ((image.naturalWidth || image.width) - sourceSize) / 2;
        const sourceY = ((image.naturalHeight || image.height) - sourceSize) / 2;
        context.drawImage(image, sourceX, sourceY, sourceSize, sourceSize, 0, 0, size, size);
        return canvas.toDataURL('image/png');
    } finally {
        URL.revokeObjectURL(objectUrl);
    }
}

const Icon = ({ name }: { name: AppIconName }) => {
    const common = { fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const };
    switch (name) {
        case 'receipt':
            return <svg viewBox="0 0 24 24" {...common}><path d="M7 3h10v18l-2-1.2-2 1.2-2-1.2-2 1.2-2-1.2V3z" /><path d="M9 8h6M9 12h6M9 16h4" /></svg>;
        case 'wallet':
            return <svg viewBox="0 0 24 24" {...common}><path d="M5 7h13a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h11" /><path d="M16 13h4v4h-4a2 2 0 0 1 0-4z" /></svg>;
        case 'invoice':
            return <svg viewBox="0 0 24 24" {...common}><path d="M6 3h12v18l-2-1-2 1-2-1-2 1-2-1-2 1z" /><path d="M9 8h6M9 12h6M9 16h3" /><path d="M15 16h.01" /></svg>;
        case 'warehouse':
            return <svg viewBox="0 0 24 24" {...common}><path d="M3 10l9-5 9 5" /><path d="M5 10v9h14v-9" /><path d="M8 19v-6h8v6M8 13h8" /></svg>;
        case 'inventory':
            return <svg viewBox="0 0 24 24" {...common}><path d="M4 7l8-4 8 4-8 4-8-4z" /><path d="M4 7v10l8 4 8-4V7" /><path d="M12 11v10" /></svg>;
        case 'customer':
            return <svg viewBox="0 0 24 24" {...common}><circle cx="12" cy="8" r="3" /><path d="M5 20c1.4-3.4 4-5 7-5s5.6 1.6 7 5" /></svg>;
        case 'users':
            return <svg viewBox="0 0 24 24" {...common}><circle cx="9" cy="8" r="3" /><path d="M3.5 20c1-3.2 3-5 5.5-5s4.5 1.8 5.5 5" /><circle cx="17" cy="9" r="2" /><path d="M15.5 15.5c2.2.4 3.8 1.9 5 4.5" /></svg>;
        case 'contract':
            return <svg viewBox="0 0 24 24" {...common}><path d="M7 3h7l3 3v15H7z" /><path d="M14 3v4h4M9 11h6M9 15h6M9 19h3" /></svg>;
        case 'pdf':
            return <svg viewBox="0 0 24 24" {...common}><path d="M6 3h9l3 3v15H6z" /><path d="M15 3v4h4" /><path d="M8 16h8" /><path d="M8 12h2.5a1.5 1.5 0 0 1 0 3H8v-3z" /></svg>;
        case 'shield':
            return <svg viewBox="0 0 24 24" {...common}><path d="M12 3l7 3v5c0 4.5-2.7 8-7 10-4.3-2-7-5.5-7-10V6z" /><path d="M9 12l2 2 4-5" /></svg>;
        case 'sheet':
            return <svg viewBox="0 0 24 24" {...common}><path d="M5 4h14v16H5z" /><path d="M5 9h14M5 14h14M10 4v16M15 4v16" /></svg>;
        case 'chart':
            return <svg viewBox="0 0 24 24" {...common}><path d="M4 19h16" /><path d="M7 16V9M12 16V5M17 16v-4" /><path d="M6 8l5-4 4 5 3-3" /></svg>;
        case 'dashboard':
            return <svg viewBox="0 0 24 24" {...common}><path d="M4 5h7v6H4zM13 5h7v4h-7zM13 11h7v8h-7zM4 13h7v6H4z" /></svg>;
        case 'database':
            return <svg viewBox="0 0 24 24" {...common}><ellipse cx="12" cy="6" rx="7" ry="3" /><path d="M5 6v6c0 1.7 3.1 3 7 3s7-1.3 7-3V6" /><path d="M5 12v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6" /></svg>;
        case 'eraser':
            return <svg viewBox="0 0 24 24" {...common}><path d="M4 16l8-8 6 6-5 5H7z" /><path d="M12 8l-2-2a2 2 0 0 1 2.8-2.8l6 6a2 2 0 0 1 0 2.8L18 14" /><path d="M13 19h7" /></svg>;
        case 'truck':
            return <svg viewBox="0 0 24 24" {...common}><path d="M3 6h11v10H3z" /><path d="M14 10h4l3 3v3h-7z" /><circle cx="7" cy="18" r="2" /><circle cx="17" cy="18" r="2" /></svg>;
        case 'calendar':
            return <svg viewBox="0 0 24 24" {...common}><path d="M5 5h14v15H5z" /><path d="M8 3v4M16 3v4M5 10h14" /><path d="M9 15l2 2 4-5" /></svg>;
        case 'web':
            return <svg viewBox="0 0 24 24" {...common}><circle cx="12" cy="12" r="8" /><path d="M4 12h16M12 4c2 2.2 3 4.8 3 8s-1 5.8-3 8M12 4c-2 2.2-3 4.8-3 8s1 5.8 3 8" /></svg>;
        case 'sync':
            return <svg viewBox="0 0 24 24" {...common}><path d="M20 7v5h-5" /><path d="M4 17v-5h5" /><path d="M18.2 9A7 7 0 0 0 6.1 7.2M5.8 15A7 7 0 0 0 17.9 16.8" /></svg>;
        case 'bot':
            return <svg viewBox="0 0 24 24" {...common}><path d="M12 4v3" /><rect x="5" y="7" width="14" height="11" rx="3" /><path d="M9 12h.01M15 12h.01M9 16h6" /><path d="M4 12H2M22 12h-2" /></svg>;
        case 'shop':
            return <svg viewBox="0 0 24 24" {...common}><path d="M3 9l1.5-5h15L21 9" /><path d="M3 9h18v11a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V9z" /><path d="M9 9v2a3 3 0 0 1-6 0V9M15 9v2a3 3 0 0 1-6 0V9M21 9v2a3 3 0 0 1-6 0V9" /></svg>;
        default:
            return null;
    }
};

const AppIcon = ({ icon, customIconDataUrl }: { icon: AppIconName; customIconDataUrl?: string }) => {
    const customIcon = normalizeCustomIconDataUrl(customIconDataUrl);
    return customIcon
        ? <img className="apps-custom-app-icon" src={customIcon} alt="" />
        : <Icon name={icon} />;
};

const isZh = (lang?: string) => !lang || lang.startsWith('zh');

export const AppsPage = ({ lang, onOpenMISDataSettings, onOpenManual }: AppsPageProps) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [apps, setApps] = useState(() => applyLayoutState(initialApps, readLayoutState()));
    const [query, setQuery] = useState('');
    const [category, setCategory] = useState('all');
    const [openTabs, setOpenTabs] = useState<string[]>([]);
    const [activeTabId, setActiveTabId] = useState('');
	const [studioOpen, setStudioOpen] = useState(false);
	const [studioTab, setStudioTab] = useState<StudioTab>('create');
	const [studioEditAppId, setStudioEditAppId] = useState('');
	const [marketInstallPrefill, setMarketInstallPrefill] = useState<{ key: number; manifestText: string }>({ key: 0, manifestText: '' });
    const [activeOperation, setActiveOperation] = useState<AppOperation | null>(null);
    const [approvalInitialAppFilter, setApprovalInitialAppFilter] = useState('all');
    const [tileMenu, setTileMenu] = useState<{ appId: string; x: number; y: number } | null>(null);
    const [datasrvDiscovery, setDataSrvDiscovery] = useState<DataSrvDiscovery>(emptyDiscovery);
    const [skillDiscovery, setSkillDiscovery] = useState<SkillAppDiscovery>(emptySkillDiscovery);
    const [activeRunRevision, setActiveRunRevision] = useState(0);
    const [workspaceLaunchHint, setWorkspaceLaunchHint] = useState('');
    /** Per-app one-click workspace open failure (tile badge + footer hint). */
    const [workspaceLaunchByAppId, setWorkspaceLaunchByAppId] = useState<Record<string, string>>({});
    const notifyActiveRunChanged = useCallback(() => setActiveRunRevision((value) => value + 1), []);

    useEffect(() => {
        persistLayoutState(apps);
    }, [apps]);

    useEffect(() => {
        if (!tileMenu) return;
        const close = () => setTileMenu(null);
        const closeOnEscape = (event: globalThis.KeyboardEvent) => {
            if (event.key === 'Escape') close();
        };
        document.addEventListener('click', close);
        document.addEventListener('keydown', closeOnEscape);
        window.addEventListener('blur', close);
        window.addEventListener('resize', close);
        window.addEventListener('scroll', close, true);
        return () => {
            document.removeEventListener('click', close);
            document.removeEventListener('keydown', closeOnEscape);
            window.removeEventListener('blur', close);
            window.removeEventListener('resize', close);
            window.removeEventListener('scroll', close, true);
        };
    }, [tileMenu]);

    useEffect(() => {
        let mounted = true;
        setDataSrvDiscovery({ ...emptyDiscovery, status: 'loading' });
        discoverDataSrvCapabilities()
            .then((next) => {
                if (mounted) setDataSrvDiscovery(next);
            })
            .catch((error: any) => {
                if (mounted) setDataSrvDiscovery({ ...emptyDiscovery, status: 'error', error: error?.message || String(error) });
            });
        return () => { mounted = false; };
    }, []);

    useEffect(() => {
        let mounted = true;
        setSkillDiscovery({ ...emptySkillDiscovery, status: 'loading' });
        discoverSkillAppManifests()
            .then((next) => {
                if (mounted) setSkillDiscovery(next);
            })
            .catch((error: any) => {
                if (mounted) setSkillDiscovery({ ...emptySkillDiscovery, status: 'error', error: error?.message || String(error) });
            });
        return () => { mounted = false; };
    }, []);

	useEffect(() => {
		if (skillDiscovery.status !== 'ready' || skillDiscovery.candidates.length === 0) return;
		const dismissed = readDismissedSkillAppIds();
		setApps((current) => {
            let changed = false;
            let next = current;
            for (const candidate of skillDiscovery.candidates) {
                if (next.some((app) => app.id === candidate.id)) continue;
                if (dismissed.has(candidate.id)) continue;
                next = [...next, appWithAvailablePin(candidate, next)];
                changed = true;
            }
			return changed ? next : current;
		});
	}, [skillDiscovery.status, skillDiscovery.candidates]);

    const categories = useMemo(() => Array.from(new Set(apps.map((app) => app.category))), [apps]);
    const normalizedQuery = query.trim().toLowerCase();
    const queryMatchedApps = useMemo(() => apps.filter((app) => !normalizedQuery || buildAppSearchText(app, lang).includes(normalizedQuery)), [apps, normalizedQuery, lang]);
    const categoryCounts = useMemo(() => countAppsByCategory(queryMatchedApps), [queryMatchedApps]);
    const recentAppCount = queryMatchedApps.filter((app) => !!app.recentUsedAt).length;
    useEffect(() => {
        if (!normalizedQuery || category === 'all') return;
        const hasMatches = category === 'recent' ? recentAppCount > 0 : (categoryCounts.get(category) || 0) > 0;
        if (!hasMatches) setCategory('all');
    }, [normalizedQuery, category, categoryCounts, recentAppCount]);
    const filteredApps = useMemo(() => {
        const filtered = apps.filter((app) => {
            const categoryMatches = category === 'all' || category === 'recent' ? true : app.category === category;
            const recentMatches = category !== 'recent' || !!app.recentUsedAt;
            const queryMatches = !normalizedQuery || buildAppSearchText(app, lang).includes(normalizedQuery);
            return categoryMatches && recentMatches && queryMatches;
        });
        if (category !== 'recent') return filtered;
        return [...filtered].sort((left, right) => String(right.recentUsedAt || '').localeCompare(String(left.recentUsedAt || '')));
    }, [apps, category, normalizedQuery, lang]);
    const hasSearchQuery = normalizedQuery.length > 0;
    const panelFilterActive = hasSearchQuery || category !== 'all';
    const showPinnedSection = !hasSearchQuery && category !== 'recent';
    const pinnedApps = showPinnedSection ? filteredApps.filter((app) => app.pinned).slice(0, maxPinnedApps) : [];
    const pinnedSectionIds = new Set(pinnedApps.map((app) => app.id));
    const visibleListApps = pinnedSectionIds.size > 0 ? filteredApps.filter((app) => !pinnedSectionIds.has(app.id)) : filteredApps;
    const panelFilterSummary = filterSummaryText({ query, category, count: filteredApps.length, lang, allLabel: text.all });
    const tileMenuApp = tileMenu ? apps.find((app) => app.id === tileMenu.appId) : undefined;
    const tileMenuPinDisabled = !!tileMenuApp && !tileMenuApp.pinned && apps.filter((app) => app.pinned).length >= maxPinnedApps;
    const activeRunSessions = useMemo(() => loadActiveAppRunSessions(), [activeRunRevision]);
    const openTabApps = openTabs.map((id) => apps.find((app) => app.id === id)).filter((app): app is AppEntry => !!app);
    const activeApp = apps.find((app) => app.id === activeTabId) ?? openTabApps[0];
    const hiddenApps = initialApps.filter((app) => !apps.some((item) => item.id === app.id));

    const openApp = (app: AppEntry) => {
        if (app.disabled) {
            setTileMenu(null);
            return;
        }
        markAppUsed(app.id);
        setTileMenu(null);
        setOpenTabs((current) => current.includes(app.id) ? current : [...current, app.id]);
        setActiveTabId(app.id);
        setStudioOpen(false);
        setActiveOperation(null);
        setWorkspaceLaunchHint('');
        setWorkspaceLaunchByAppId((current) => clearWorkspaceLaunchIssue(current, app.id));
        // One-click: open right-pane AppView workspace for enterprise apps (best-effort).
        const kind = String(app.kind || '');
        if (isEnterpriseAppKindForWorkspace(kind)) {
            const openFromInstall = typeof OpenMaclawAppWorkspaceFromInstall === 'function'
                ? OpenMaclawAppWorkspaceFromInstall
                : null;
            if (openFromInstall) {
                const rememberLaunchIssue = (message: string) => {
                    setWorkspaceLaunchHint(message);
                    setWorkspaceLaunchByAppId((current) => setWorkspaceLaunchIssue(current, app.id, message));
                };
                void Promise.resolve(openFromInstall({
                    app_id: canonicalAppManifestID(app),
                    app_name: app.name,
                    kind,
                })).then((result: any) => {
                    const issue = formatWorkspaceLaunchIssue(app.name, result, lang);
                    if (!issue) {
                        setWorkspaceLaunchHint('');
                        setWorkspaceLaunchByAppId((current) => clearWorkspaceLaunchIssue(current, app.id));
                        return;
                    }
                    rememberLaunchIssue(issue);
                }).catch((error: any) => {
                    rememberLaunchIssue(formatWorkspaceLaunchError(app.name, error, lang));
                });
            }
        }
    };

    const openOperation = (operation: AppOperation, options?: { appId?: string }) => {
        setTileMenu(null);
        if (activeOperation === operation && !options?.appId) {
            setActiveOperation(null);
            return;
        }
        setStudioOpen(false);
        setActiveOperation(operation);
        if (operation === 'approval_status') setApprovalInitialAppFilter(options?.appId || 'all');
    };

    const markAppUsed = (appId: string) => {
        recentUseSequence += 1;
        const usedAt = `${Date.now().toString().padStart(13, '0')}-${recentUseSequence.toString().padStart(6, '0')}`;
        setApps((current) => current.map((app) => app.id === appId ? { ...app, recentUsedAt: usedAt } : app));
    };

    const closeAppTab = (appId: string) => {
        setOpenTabs((current) => {
            const index = current.indexOf(appId);
            const next = current.filter((id) => id !== appId);
            if (activeTabId === appId) {
                setActiveTabId(next[Math.max(0, index - 1)] || next[0] || '');
            }
            return next;
        });
    };

    const togglePin = (appId: string) => {
        setApps((current) => {
            const pinCount = current.filter((app) => app.pinned).length;
            return current.map((app) => {
                if (app.id !== appId) return app;
                if (!app.pinned && pinCount >= maxPinnedApps) return app;
                return { ...app, pinned: !app.pinned };
            });
        });
    };

    const toggleDisableApp = (appId: string) => {
        setApps((current) => current.map((app) => app.id === appId ? {
            ...app,
            disabled: !app.disabled,
            disabledReason: app.disabled ? undefined : text.appDisabledReason,
            disabledSource: app.disabled ? undefined : 'local',
        } : app));
        setOpenTabs((current) => {
            const index = current.indexOf(appId);
            if (index < 0) return current;
            const next = current.filter((id) => id !== appId);
            if (activeTabId === appId) setActiveTabId(next[Math.max(0, index - 1)] || next[0] || '');
            return next;
        });
    };

    const syncHubAppGovernance = (summaries: AppPackageSubmissionSummary[]) => {
        if (summaries.length === 0) return;
        const governanceByKey = new Map<string, { status: AppPublishStatus; reason?: string }>();
        summaries.forEach((summary) => {
            const status = normalizePublishStatus(summary.status);
            if (!status || !['deprecated', 'revoked', 'approved', 'published'].includes(status)) return;
            const reason = status === 'revoked'
                ? text.appHubRevokedReason
                : status === 'deprecated'
                    ? text.appHubDeprecatedReason
                    : undefined;
            const keys = [...summary.appIDs, summary.hubCapabilityID]
                .filter((id): id is string => typeof id === 'string' && id.trim() !== '')
                .flatMap((id) => appInstallIdentityKeys(id));
            keys.forEach((key) => governanceByKey.set(key, { status, reason }));
        });
        if (governanceByKey.size === 0) return;
        setApps((current) => {
            let changed = false;
            const next = current.map((app) => {
                const appKeys = [...appInstallIdentityKeys(app.id), ...(app.marketCapabilityID ? appInstallIdentityKeys(app.marketCapabilityID) : [])];
                const governance = appKeys.map((key) => governanceByKey.get(key)).find(Boolean);
                if (!governance) return app;
                if (governance.status === 'deprecated' || governance.status === 'revoked') {
                    if (app.disabled && app.disabledSource === 'local') return app;
                    if (app.disabled && app.disabledSource === 'hub_governance' && app.disabledReason === governance.reason) return app;
                    changed = true;
                    return { ...app, disabled: true, disabledReason: governance.reason, disabledSource: 'hub_governance' as const };
                }
                if ((governance.status === 'approved' || governance.status === 'published') && app.disabled && app.disabledSource === 'hub_governance') {
                    changed = true;
                    return { ...app, disabled: false, disabledReason: undefined, disabledSource: undefined };
                }
                return app;
            });
            return changed ? next : current;
        });
    };
    const moveApp = (appId: string, direction: AppMoveTarget) => {
        setApps((current) => {
            const index = current.findIndex((app) => app.id === appId);
            if (index < 0) return current;
            const nextIndex = direction === 'top' ? 0 : direction === 'bottom' ? current.length - 1 : index + direction;
            if (nextIndex < 0 || nextIndex >= current.length || nextIndex === index) return current;
            const next = [...current];
            const [item] = next.splice(index, 1);
            next.splice(nextIndex, 0, item);
            return next;
        });
    };

    const updateApp = (appId: string, patch: Partial<AppEntry>) => {
        setApps((current) => current.map((app) => app.id === appId ? { ...app, ...patch } : app));
        markAppPublishSubmissionModified(appId, patch.version);
    };

    const duplicateApp = (appId: string) => {
        const source = apps.find((app) => app.id === appId);
        if (!source) return;
        const id = makeLocalAppId(`${source.name}-copy`);
        const suffix = text.duplicateSuffix;
        const name = makeDuplicateAppName(source.name, suffix, new Set(apps.map((app) => app.name)));
        const cloned: AppEntry = {
            ...source,
            id,
            name,
            source: 'local',
            version: 1,
            pinned: false,
            recentUsedAt: undefined,
            disabled: false,
            disabledReason: undefined,
            disabledSource: undefined,
            manifest: source.manifest ? {
                ...source.manifest,
                datasrv: source.manifest.datasrv ? { ...source.manifest.datasrv } : undefined,
                skill: source.manifest.skill ? { ...source.manifest.skill, fields: source.manifest.skill.fields ? source.manifest.skill.fields.map((field) => ({ ...field, options: field.options ? [...field.options] : undefined })) : undefined } : undefined,
            } : undefined,
        };
        setApps((current) => [...current, cloned]);
        setStudioTab('manage');
        setStudioEditAppId(id);
    };

    const removeApp = (appId: string) => {
        clearAppRunHistory(appId);
        clearActiveAppRunSession(appId);
        notifyActiveRunChanged();
        clearAppPublishSubmission(appId);
        const removedApp = apps.find((app) => app.id === appId);
        if (removedApp && removedApp.source === 'skill') {
            dismissSkillApp(appId);
        }
        setApps((current) => current.filter((app) => app.id !== appId));
        setOpenTabs((current) => {
            const index = current.indexOf(appId);
            const next = current.filter((id) => id !== appId);
            if (activeTabId === appId) {
                setActiveTabId(next[Math.max(0, index - 1)] || next[0] || '');
            }
            return next;
        });
    };

    const restoreApp = (appId: string) => {
        const app = initialApps.find((item) => item.id === appId);
        if (!app) return;
        setApps((current) => current.some((item) => item.id === appId) ? current : [...current, appWithAvailablePin(app, current)]);
    };

    const addDiscoveredApp = (app: AppEntry, options?: { keepStudioCreate?: boolean }) => {
        undismissSkillApp(app.id);
        setApps((current) => {
            if (current.some((item) => item.id === app.id)) return current;
            return [...current, appWithAvailablePin(app, current)];
        });
        if (!options?.keepStudioCreate) setStudioTab('manage');
    };

    const installMarketApp = (app: AppEntry) => {
        const existing = apps.find((item) => appInstallIdentityKeys(app.id).some((id) => appInstallIdentityKeys(item.id).includes(id)));
        setApps((current) => {
            const existingIndex = current.findIndex((item) => appInstallIdentityKeys(app.id).some((id) => appInstallIdentityKeys(item.id).includes(id)));
            if (existingIndex >= 0) {
                const existing = current[existingIndex];
                if (normalizeAppVersion(app.version) <= normalizeAppVersion(existing.version)) {
                    const evidencePatch: Partial<AppEntry> = {};
                    if (app.importedRunEvidence && !existing.importedRunEvidence) evidencePatch.importedRunEvidence = app.importedRunEvidence;
                    if (app.versionSnapshot && !existing.versionSnapshot) evidencePatch.versionSnapshot = app.versionSnapshot;
                    if (app.installEvidence && !existing.installEvidence) evidencePatch.installEvidence = app.installEvidence;
                    if (app.workflowContract && !existing.workflowContract) evidencePatch.workflowContract = app.workflowContract;
                    if (Object.keys(evidencePatch).length === 0) return current;
                    const next = [...current];
                    next[existingIndex] = { ...existing, ...evidencePatch };
                    return next;
                }
                const next = [...current];
                next[existingIndex] = {
                    ...app,
                    id: existing.id,
                    pinned: existing.pinned,
                    category: existing.category,
                    icon: existing.icon,
                    accent: existing.accent,
                    recentUsedAt: existing.recentUsedAt,
                    disabled: existing.disabled,
                    disabledReason: existing.disabledReason,
                    disabledSource: existing.disabledSource,
                };
                return next;
            }
            return [...current, appWithAvailablePin(app, current)];
        });
    };

    const editAppFromStudio = (appId: string) => {
        setStudioOpen(true);
        setStudioTab('manage');
        setStudioEditAppId(appId);
    };
	const installDependenciesFromPublish = (appId: string) => {
		const app = apps.find((item) => item.id === appId);
		if (!app) return;
		setStudioOpen(false);
		openOperation('market');
		setMarketInstallPrefill((current) => ({
			key: current.key + 1,
			manifestText: JSON.stringify(appToManifest(app), null, 2),
	        }));
	    };

    const appStatusInfo = (app: AppEntry): { key: 'available' | 'running' | 'loading' | 'disabled' | 'error'; label: string } => {
        if (app.disabled) return { key: 'disabled', label: app.disabledReason || text.appDisabled };
        if (activeRunSessionMatchesApp(app, activeRunSessions[app.id])) return { key: 'running', label: text.appRunning };
        if (openTabs.includes(app.id)) return { key: 'running', label: text.appOpened };
        if (app.source === 'datasrv') {
            if (datasrvDiscovery.status === 'loading') return { key: 'loading', label: text.datasrvLoading };
            if (datasrvDiscovery.status === 'disabled') return { key: 'disabled', label: text.datasrvDisabled };
            if (datasrvDiscovery.status === 'error') return { key: 'error', label: text.datasrvError };
        }
        return { key: 'available', label: text.appAvailable };
    };

    const openTileMenu = (appId: string, x: number, y: number) => {
        const width = 168;
        const height = 44;
        const maxX = typeof window === 'undefined' ? x : Math.max(8, window.innerWidth - width);
        const maxY = typeof window === 'undefined' ? y : Math.max(8, window.innerHeight - height);
        setTileMenu({
            appId,
            x: Math.min(Math.max(8, x), maxX),
            y: Math.min(Math.max(8, y), maxY),
        });
    };

    const renderTile = (app: AppEntry) => {
        const status = appStatusInfo(app);
        const launchIssue = workspaceLaunchByAppId[app.id] || '';
        const moveTileFocus = (event: KeyboardEvent<HTMLButtonElement>) => {
            if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
                event.preventDefault();
                const rect = event.currentTarget.getBoundingClientRect();
                openTileMenu(app.id, rect.left + 8, rect.bottom + 4);
                return;
            }
            const keyOffsets: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -4, ArrowDown: 4 };
            const offset = keyOffsets[event.key];
            if (offset === undefined && event.key !== 'Home' && event.key !== 'End') return;
            const list = event.currentTarget.closest('.apps-list');
            const tiles = Array.from(list?.querySelectorAll<HTMLButtonElement>('.apps-app-tile') || []);
            const index = tiles.indexOf(event.currentTarget);
            if (index < 0 || tiles.length === 0) return;
            event.preventDefault();
            const nextIndex = event.key === 'Home'
                ? 0
                : event.key === 'End'
                    ? tiles.length - 1
                    : Math.max(0, Math.min(tiles.length - 1, index + offset));
            tiles[nextIndex]?.focus();
        };
        const tooltip = launchIssue
            ? `${buildAppTileTooltip(app, text, status.label, lang)}\n${launchIssue}`
            : buildAppTileTooltip(app, text, status.label, lang);
        return (
            <button
                key={app.id}
                className={`apps-app-tile ${activeApp?.id === app.id && !studioOpen ? 'is-active' : ''}${launchIssue ? ' has-workspace-issue' : ''}`}
                data-status={status.key}
                data-workspace-issue={launchIssue ? 'true' : undefined}
                style={{ '--apps-icon-color': app.accent } as CSSProperties}
                title={tooltip}
                aria-label={launchIssue
                    ? `${buildAppTileAriaLabel(app, text, status.label, lang)}. ${launchIssue}`
                    : buildAppTileAriaLabel(app, text, status.label, lang)}
                onClick={() => openApp(app)}
                onContextMenu={(event) => {
                    event.preventDefault();
                    openTileMenu(app.id, event.clientX, event.clientY);
                }}
	                onKeyDown={moveTileFocus}
	            >
	                <span className="apps-app-icon">
                        <AppIcon icon={app.icon} customIconDataUrl={app.customIconDataUrl} />
                        {launchIssue ? (
                            <span
                                className="apps-app-tile__workspace-badge"
                                data-testid={`apps-workspace-badge-${app.id}`}
                                title={launchIssue}
                                aria-hidden="true"
                            >
                                !
                            </span>
                        ) : null}
                    </span>
	                <span className="apps-app-label">
                        <span className="apps-app-name">{app.name}</span>
                        <span className="apps-app-status">{status.label}</span>
                    </span>
	            </button>
        );
    };

    return (
        <div className="apps-page">
            <aside className="apps-panel" aria-label={text.apps}>
                <div className="apps-panel__top">
                    <div className="apps-search-row">
                        <div className="apps-search-wrap">
                            <input
                                className="apps-search"
                                value={query}
                                onChange={(event) => setQuery(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === 'Escape' && query) setQuery('');
                                }}
                                placeholder={text.search}
                            />
                            {query && (
                                <button className="apps-search-clear" type="button" title={text.clearSearch} aria-label={text.clearSearch} onClick={() => setQuery('')}>
                                    <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
                                </button>
                            )}
                        </div>
                        <button className="apps-studio-button" type="button" title={isZh(lang) ? '\u521b\u5efa\u5e94\u7528' : 'Create App'} aria-label={isZh(lang) ? '\u521b\u5efa\u5e94\u7528' : 'Create App'} onClick={() => { setActiveOperation(null); setStudioOpen(true); }}>
                            <span className="apps-studio-button__icon" aria-hidden="true">
                                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="18" height="18" rx="3" /><path d="M12 8v8M8 12h8" /></svg>
                            </span>
                            <span className="apps-studio-button__label">{isZh(lang) ? '\u521b\u5efa\u5e94\u7528' : 'Create App'}</span>
                        </button>
                    </div>
                    <div className="apps-filter-row">
                        <span className="apps-filter-label">{text.category}</span>
                        <select className="apps-category-select" value={category} onChange={(event) => setCategory(event.target.value)}>
                            <option value="all">{text.all} ({queryMatchedApps.length})</option>
                            <option value="recent" disabled={hasSearchQuery && recentAppCount === 0}>{text.recent} ({recentAppCount})</option>
                            {categories.map((item) => <option key={item} value={item} disabled={hasSearchQuery && (categoryCounts.get(item) || 0) === 0}>{categoryOptionLabel(item, categoryCounts)}</option>)}
                        </select>
                        <button
                            className="apps-secondary-button apps-filter-reset"
                            type="button"
                            disabled={!panelFilterActive}
                            title={text.resetFilter}
                            onClick={() => {
                                setQuery('');
                                setCategory('all');
                            }}
                        >{text.reset}</button>
                    </div>
                    {panelFilterSummary && <div className="apps-filter-summary" aria-live="polite">{panelFilterSummary}</div>}
                    <div className="apps-ops" aria-label={text.operations}>
                        <div className="apps-ops__title">{text.operations}</div>
                        <button className={`apps-ops__item ${activeOperation === 'approval_status' ? 'is-active' : ''}`} type="button" aria-pressed={activeOperation === 'approval_status'} onClick={() => openOperation('approval_status')}>
                            <Icon name="shield" />
                            <span><strong>{text.approvalStatus}</strong><small>{text.approvalStatusHint}</small></span>
                        </button>
                        <button className={`apps-ops__item ${activeOperation === 'run_history' ? 'is-active' : ''}`} type="button" aria-pressed={activeOperation === 'run_history'} onClick={() => openOperation('run_history')}>
                            <Icon name="sync" />
                            <span><strong>{text.runHistoryOps}</strong><small>{text.runHistoryOpsHint}</small></span>
                        </button>
                        <button className={`apps-ops__item ${activeOperation === 'market' ? 'is-active' : ''}`} type="button" aria-pressed={activeOperation === 'market'} onClick={() => openOperation('market')}>
                            <Icon name="shop" />
                            <span><strong>{text.marketOps}</strong><small>{text.marketOpsHint}</small></span>
                        </button>
                    </div>
                </div>
                <div className="apps-list elegant-scrollbar">
                    {pinnedApps.length > 0 && (
                        <section className="apps-section">
                            <div className="apps-section__title-row">
                                <h3 className="apps-section__title">{text.pinned}</h3>
                                <span className="apps-count">{pinnedApps.length}/{maxPinnedApps}</span>
                            </div>
                            <div className="apps-grid">{pinnedApps.map(renderTile)}</div>
                        </section>
                    )}
                    <section className="apps-section">
                        <div className="apps-section__title-row">
                            <h3 className="apps-section__title">{hasSearchQuery ? text.searchResults : category === 'recent' ? text.recent : pinnedApps.length > 0 ? text.otherApps : text.all}</h3>
                            <span className="apps-count">{visibleListApps.length}</span>
                        </div>
                        {visibleListApps.length > 0 ? <div className="apps-grid">{visibleListApps.map(renderTile)}</div> : <div className="apps-empty">{pinnedApps.length > 0 ? text.noMoreApps : text.noApps}</div>}
                    </section>
                    {workspaceLaunchHint && (
                        <div
                            className="apps-empty"
                            role="status"
                            data-testid="apps-workspace-launch-hint"
                            style={{ marginTop: 12, textAlign: 'left' }}
                        >
                            <span>{workspaceLaunchHint}</span>
                            <button
                                type="button"
                                style={{ marginLeft: 12 }}
                                onClick={() => setWorkspaceLaunchHint('')}
                            >
                                {isZh(lang) ? '关闭' : 'Dismiss'}
                            </button>
                        </div>
                    )}
                </div>
                {tileMenuApp && tileMenu && (
                    <div
                        className="apps-tile-menu"
                        role="menu"
                        style={{ left: tileMenu.x, top: tileMenu.y } as CSSProperties}
                        onClick={(event) => event.stopPropagation()}
                    >
                        <button
                            type="button"
                            role="menuitem"
                            autoFocus
                            disabled={tileMenuPinDisabled}
                            title={tileMenuPinDisabled ? text.pinLimitReached : tileMenuApp.pinned ? text.removeFromPinned : text.setAsPinned}
                            onClick={() => {
                                if (tileMenuPinDisabled) return;
                                togglePin(tileMenuApp.id);
                                setTileMenu(null);
                            }}
                        >
                            {tileMenuApp.pinned ? text.removeFromPinned : text.setAsPinned}
                        </button>
                    </div>
                )}
            </aside>

            <main className="apps-detail">
                {studioOpen ? (
                    <AppStudio
                        apps={apps}
                        lang={lang}
	                        tab={studioTab}
	                        setTab={setStudioTab}
	                        onClose={() => setStudioOpen(false)}
	                        onTogglePin={togglePin}
                        onUpdateApp={updateApp}
                        onDuplicateApp={duplicateApp}
                        onMoveApp={moveApp}
                        onToggleDisableApp={toggleDisableApp}
                        onRemoveApp={removeApp}
                        onRestoreApp={restoreApp}
                        pendingEditAppId={studioEditAppId}
                        onPendingEditConsumed={() => setStudioEditAppId('')}
                        hiddenApps={hiddenApps}
                        datasrvDiscovery={datasrvDiscovery}
                        skillDiscovery={skillDiscovery}
                        onOpenMISDataSettings={onOpenMISDataSettings}
                        onOpenManual={onOpenManual}
                        onAddDiscoveredApp={addDiscoveredApp}
                        onCreateApp={addDiscoveredApp}
	                        onInstallMarketApp={installMarketApp}
	                        onEditApp={editAppFromStudio}
	                        onInstallDependencies={installDependenciesFromPublish}
                        onSyncHubAppGovernance={syncHubAppGovernance}
                    />
                ) : activeOperation === 'approval_status' ? (
                    <ApprovalManager
                        apps={apps}
                        lang={lang}
                        initialAppFilter={approvalInitialAppFilter}
                    />
                ) : activeOperation === 'run_history' ? (
                    <RunHistoryManager apps={apps} lang={lang} />
                ) : null}
                <div className="apps-detail__body elegant-scrollbar apps-market-persistent" hidden={studioOpen || activeOperation !== 'market'}>
                    <MarketPane apps={apps} lang={lang} onInstallApp={installMarketApp} prefill={marketInstallPrefill} onManageApps={() => { setActiveOperation(null); setStudioOpen(true); setStudioTab('manage'); }} />
                </div>
                <div className="apps-runtime-persistent" hidden={studioOpen || !!activeOperation}>
                    <AppRuntime
                        tabs={openTabApps}
                        activeApp={activeApp}
                        lang={lang}
                        onActivate={(appId) => {
                            setActiveTabId(appId);
                        }}
                        onClose={closeAppTab}
                        onUse={markAppUsed}
                        onOpenApprovalManager={(appId) => openOperation('approval_status', { appId })}
                        onActiveRunChange={notifyActiveRunChanged}
                    />
                </div>
            </main>
        </div>
    );
};

const AppRuntime = ({ tabs, activeApp, lang, onActivate, onClose, onUse, onOpenApprovalManager, onActiveRunChange }: {
    tabs: AppEntry[];
    activeApp?: AppEntry;
    lang?: string;
    onActivate: (appId: string) => void;
    onClose: (appId: string) => void;
    onUse: (appId: string) => void;
    onOpenApprovalManager: (appId?: string) => void;
    onActiveRunChange: () => void;
}) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    if (tabs.length === 0) {
        return <EmptyRuntime text={text} />;
    }
    const activateTab = (appId: string, shouldFocus = false) => {
        onActivate(appId);
        if (!shouldFocus) return;
        const focusTab = () => document.getElementById(getRuntimeTabId(appId))?.focus();
        if (typeof window.requestAnimationFrame === 'function') window.requestAnimationFrame(focusTab);
        else window.setTimeout(focusTab, 0);
    };
    const handleTabKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
        const nextIndex = event.key === 'ArrowRight'
            ? (index + 1) % tabs.length
            : event.key === 'ArrowLeft'
                ? (index - 1 + tabs.length) % tabs.length
                : event.key === 'Home'
                    ? 0
                    : event.key === 'End'
                        ? tabs.length - 1
                        : -1;
        if (nextIndex < 0) return;
        event.preventDefault();
        activateTab(tabs[nextIndex].id, true);
    };
    return (
        <>
            <div className="apps-runtime-tabs" role="tablist" aria-label={text.apps}>
                {tabs.map((app, index) => {
                    const isActive = activeApp?.id === app.id;
                    return (
                    <div key={app.id} className={`apps-runtime-tab-wrap ${isActive ? 'is-active' : ''}`}>
                        <button
                            id={getRuntimeTabId(app.id)}
                            className={`apps-runtime-tab ${isActive ? 'is-active' : ''}`}
                            type="button"
                            role="tab"
                            aria-selected={isActive}
                            aria-controls={getRuntimePanelId(app.id)}
                            tabIndex={isActive ? 0 : -1}
                            onClick={() => activateTab(app.id)}
                            onKeyDown={(event) => handleTabKeyDown(event, index)}
                        >
                            <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><AppIcon icon={app.icon} customIconDataUrl={app.customIconDataUrl} /></span>
                            <span className="apps-runtime-tab__label">{app.name}</span>
                        </button>
                        <button
                            className="apps-runtime-tab__close"
                            type="button"
                            title={text.close}
                            aria-label={`${text.close} ${app.name}`}
                            onClick={() => onClose(app.id)}
                        >
                            &times;
                        </button>
                    </div>
                    );
                })}
            </div>
            <div className="apps-runtime-shell">
                {tabs.map((tabApp) => {
                    const isActive = activeApp?.id === tabApp.id;
                    return (
                        <div
                            key={tabApp.id}
                            id={getRuntimePanelId(tabApp.id)}
                            aria-labelledby={getRuntimeTabId(tabApp.id)}
                            role="tabpanel"
                            className="apps-runtime-panel"
                            hidden={!isActive}
                        >
                            <AppPreview app={tabApp} lang={lang} onUse={onUse} onOpenApprovalManager={onOpenApprovalManager} onActiveRunChange={onActiveRunChange} />
                        </div>
                    );
                })}
            </div>
        </>
    );
};

const EmptyRuntime = ({ text }: { text: typeof labels.zh }) => (
    <div className="apps-runtime-empty">
        <div className="apps-runtime-empty__icon" aria-hidden="true">
            <Icon name="dashboard" />
        </div>
        <h3>{text.noOpenAppTitle}</h3>
        <p>{text.noOpenAppHint}</p>
    </div>
);

const getRuntimeSafeId = (appId: string) => appId.replace(/[^A-Za-z0-9_-]/g, '-');
const getRuntimeTabId = (appId: string) => `app-tab-${getRuntimeSafeId(appId)}`;
const getRuntimePanelId = (appId: string) => `app-panel-${getRuntimeSafeId(appId)}`;
function buildApprovalInstances(app: AppEntry, runState: 'idle' | 'running' | 'done' | 'error' | 'cancelled', businessEntity: string, businessAction: string, businessNote: string, lang?: string): ApprovalInstanceView[] {
    const zh = isZh(lang);
    const title = businessEntity || app.category || app.name;
    const workflow = appApprovalWorkflowSkillID(app) || app.name;
    const workflowMapping = app.manifest?.workflow;
    const submitted = runState === 'done' || runState === 'running';
    const draftStatus = submitted ? 'pending' : 'draft';
    const draftNode = submitted ? (workflowMapping?.approvalNode || (zh ? '\u4e3b\u7ba1\u5ba1\u6279' : 'Manager approval')) : (workflowMapping?.submitNode || (zh ? '\u53d1\u8d77\u8282\u70b9' : 'Submit node'));
    const draftResult = submitted ? (zh ? '\u5ba1\u6279\u4e2d' : 'Pending') : (zh ? '\u5f85\u63d0\u4ea4' : 'Draft');
    return [
        {
            id: `${app.id}-current`,
            appID: app.id,
            title: submitted ? `${title} · ${businessAction}` : title,
            lane: 'my_requests',
            status: draftStatus,
            currentNode: draftNode,
            owner: zh ? '\u6211' : 'Me',
            approver: submitted ? (zh ? '\u76f4\u5c5e\u4e3b\u7ba1 ·  VE' : 'Manager ·  VE') : workflow,
            updatedAt: submitted ? new Date().toLocaleString() : '-',
            result: businessNote.trim() || draftResult,
            workflowSkillID: workflow,
            businessStatus: submitted ? 'approval_pending' : 'draft',
            resultStatus: draftStatus,
            amount: submitted ? (zh ? '\u672c\u6b21\u63d0\u4ea4' : 'Current submission') : undefined,
        },
        {
            id: `${app.id}-pending`,
            appID: app.id,
            title: zh ? `${app.name}\u5f85\u529e` : `${app.name} task`,
            lane: 'pending_my_approval',
            status: 'pending',
            currentNode: workflowMapping?.approvalNode || (zh ? '\u6211\u5ba1\u6279' : 'My approval'),
            owner: zh ? '\u540c\u4e8b' : 'Coworker',
            approver: zh ? '\u6211' : 'Me',
            updatedAt: zh ? '\u4eca\u5929' : 'Today',
            result: zh ? '\u7b49\u5f85\u5904\u7406' : 'Waiting',
            workflowSkillID: workflow,
            businessStatus: 'approval_pending',
            resultStatus: 'pending',
        },
        {
            id: `${app.id}-attention`,
            appID: app.id,
            title: zh ? `${app.name}\u9700\u5173\u6ce8` : `${app.name} attention`,
            lane: 'attention',
            status: 'attention',
            currentNode: workflowMapping?.attentionNode || (zh ? '\u98ce\u9669\u590d\u6838' : 'Risk review'),
            owner: zh ? '\u7cfb\u7edf' : 'System',
            approver: zh ? '\u4ec5\u67e5\u770b' : 'View only',
            updatedAt: zh ? '\u6628\u5929' : 'Yesterday',
            result: zh ? '\u89c4\u5219\u547d\u4e2d\uff0c\u9700\u67e5\u770b' : 'Rule matched, review needed',
            workflowSkillID: workflow,
            businessStatus: 'attention',
            resultStatus: 'attention',
        },
    ];
}

function normalizeApprovalCurrentNodeIDs(instance: BackendApprovalInstance): string[] {
    const raw = instance.current_node_ids || instance.currentNodeIDs || instance.workflow_node_ids || instance.workflowNodeIDs || [];
    const values = Array.isArray(raw) ? raw : [];
    const nodes: string[] = [];
    const seen = new Set<string>();
    const add = (value: unknown) => {
        const text = String(value ?? '').trim();
        if (!text) return;
        const key = text.toLowerCase();
        if (seen.has(key)) return;
        seen.add(key);
        nodes.push(text);
    };
    values.forEach(add);
    add(instance.current_node || instance.currentNode);
    return nodes;
}

function approvalCurrentNodeText(instance: Pick<ApprovalInstanceView, 'currentNode' | 'currentNodeIDs'> | undefined, lang?: string): string {
    if (!instance) return '-';
    const nodes = Array.isArray(instance.currentNodeIDs) ? instance.currentNodeIDs.filter((node) => String(node || '').trim()).map((node) => String(node).trim()) : [];
    if (nodes.length > 0) return nodes.join(' / ');
    return String(instance.currentNode || 'Current node').trim();
}
function normalizeApprovalNodeTasks(value: unknown): Record<string, unknown>[] {
    if (!Array.isArray(value)) return [];
    return value.filter((item): item is Record<string, unknown> => !!item && typeof item === 'object' && !Array.isArray(item));
}
function approvalNodeTaskCountText(instance: Pick<ApprovalInstanceView, 'nodeTasks'> | undefined, lang?: string): string {
    const count = Array.isArray(instance?.nodeTasks) ? instance.nodeTasks.length : 0;
    if (count <= 0) return '-';
    return isZh(lang) ? `${count} \u4e2a\u4efb\u52a1` : `${count} task${count > 1 ? 's' : ''}`;
}
function backendApprovalInstanceToView(instance: BackendApprovalInstance, lang?: string): ApprovalInstanceView | null {
    const id = String(instance.instance_id || instance.instanceID || instance.instanceId || '').trim();
    const appTitle = String(instance.title || id).trim();
    if (!id || !appTitle) return null;
    const lane = String(instance.lane || 'my_requests').trim() as ApprovalInstanceView['lane'];
    const status = String(instance.status || 'pending').trim() as ApprovalInstanceView['status'];
	const events = (instance.events || []).map((event): ApprovalInstanceEventView => ({
		at: String(event.at || '').trim(),
		node: String(event.node || '').trim() || undefined,
		actor: String(event.actor || '').trim() || undefined,
		decision: String(event.decision || event.action || '').trim() || undefined,
		action: String(event.action || '').trim() || undefined,
		message: String(event.message || event.note || '').trim() || undefined,
		metadata: event.metadata && typeof event.metadata === 'object' ? event.metadata : undefined,
	})).filter((event) => event.at || event.node || event.actor || event.decision || event.action || event.message);
    const appID = String(instance.app_id || instance.appID || '').trim();
    const approvalID = String(instance.approval_id || instance.approvalID || instance.approvalId || instance.record_approval_id || instance.recordApprovalID || instance.recordApprovalId || '').trim();
    const objectRole = String(instance.object_role || instance.objectRole || instance.approval_object_role || instance.approvalObjectRole || '').trim();
    const workflowSkillID = String(instance.workflow_skill_id || instance.workflowSkillID || instance.workflowSkillId || '').trim();
    const currentAssignee = String(instance.current_assignee || instance.currentAssignee || instance.approver || '').trim();
    const currentAssigneeType = String(instance.current_assignee_type || instance.currentAssigneeType || (currentAssignee ? 'user' : '')).trim();
    const applicant = String(instance.applicant || instance.submitted_by || instance.submittedBy || instance.owner || '').trim();
    const owner = String(instance.owner || applicant || '-').trim();
    const approver = String(instance.approver || currentAssignee || '-').trim();
    const resultPayload = compactApprovalRecord(instance.result_payload || instance.resultPayload);
    const currentNodeIDs = normalizeApprovalCurrentNodeIDs(instance);
    const currentNode = currentNodeIDs[0] || String(instance.current_node || instance.currentNode || 'Current node').trim();
    const nodeTasks = normalizeApprovalNodeTasks(instance.node_tasks || instance.nodeTasks || instance.approval_tasks || instance.approvalTasks);
    return {
        id,
        appID,
        appName: String(instance.app_name || instance.appName || '').trim(),
        blueprintID: String(instance.blueprint_id || instance.blueprintID || '').trim(),
        datasetID: String(instance.dataset_id || instance.datasetID || '').trim(),
        objectRole,
        approvalEvent: String(instance.approval_event || instance.approvalEvent || '').trim(),
        approvalWorkflowID: String(instance.approval_workflow_id || instance.approvalWorkflowID || instance.approvalWorkflowId || workflowSkillID || '').trim(),
        approvalID,
        title: appTitle,
        lane: lane === 'pending_my_approval' || lane === 'handled' || lane === 'attention' ? lane : 'my_requests',
        status: ['approved', 'rejected', 'attention', 'draft', 'requires_input', 'failed', 'cancelled', 'timeout', 'running', 'submitted', 'in_progress'].includes(status) ? status : 'pending',
        currentNode,
        currentNodeStatus: String(instance.current_node_status || instance.currentNodeStatus || instance.node_status || instance.nodeStatus || '').trim(),
        currentNodeIDs,
        nodeTasks: nodeTasks.length > 0 ? nodeTasks : undefined,
        owner,
        applicant,
        approver,
        currentAssignee,
        currentAssigneeType,
        updatedAt: String(instance.updated_at || instance.updatedAt || '-').trim(),
        result: String(instance.result || '-').trim(),
        workflowSkillID,
        workflowVersion: String(instance.workflow_version || instance.workflowVersion || '').trim(),
        workflowDecisionID: String(instance.workflow_decision_id || instance.workflowDecisionID || instance.workflowDecisionId || '').trim(),
        businessStatus: String(instance.business_status || instance.businessStatus || '').trim(),
        resultStatus: String(instance.result_status || instance.resultStatus || '').trim(),
        fromStatus: String(instance.from_status || instance.fromStatus || '').trim(),
        toStatus: String(instance.to_status || instance.toStatus || instance.business_status || instance.businessStatus || instance.status || '').trim(),
        recordID: String(instance.record_id || instance.recordID || instance.recordId || approvalPayloadRecordID(resultPayload) || '').trim(),
        detailURL: String(instance.detail_url || instance.detailURL || instance.detailUrl || '').trim(),
        resultPayload,
        outputs: normalizeApprovalOutputs(instance.outputs),
        artifacts: normalizeApprovalArtifacts(instance.artifacts),
        events,
    };
}
function approvalLaneFromEvidence(evidence: AppRunApprovalInstanceEvidence): ApprovalLaneFilter {
    const explicitLane = String(evidence.lane || '').trim();
    if (explicitLane === 'my_requests' || explicitLane === 'pending_my_approval' || explicitLane === 'handled' || explicitLane === 'attention') return explicitLane;
    const status = String(evidence.status || evidence.resultStatus || '').trim();
    if (status === 'attention') return 'attention';
    if (['approved', 'rejected', 'failed', 'cancelled', 'timeout'].includes(status)) return 'handled';
    return 'my_requests';
}
function approvalEvidenceToBackendInstance(app: AppEntry, evidence: AppRunApprovalInstanceEvidence, lang?: string): BackendApprovalInstance | null {
    const instanceID = String(evidence.instanceId || evidence.approvalInstanceId || evidence.workflowInstanceId || evidence.approvalID || '').trim();
    if (!instanceID) return null;
    const workflowSkillID = String(evidence.workflowSkillId || appApprovalWorkflowSkillID(app)).trim();
    const approvalID = String(evidence.approvalID || '').trim();
    const currentNodeIDs = Array.isArray(evidence.currentNodeIDs) ? evidence.currentNodeIDs.filter((node) => String(node || '').trim()).map((node) => String(node).trim()) : [];
    const currentNode = String(evidence.currentNode || currentNodeIDs[0] || app.manifest?.workflow?.approvalNode || '').trim();
    const status = String(evidence.status || evidence.resultStatus || 'pending').trim();
    return {
        instance_id: instanceID,
        approval_id: approvalID || undefined,
        record_approval_id: approvalID || undefined,
        workflow_decision_id: String(evidence.workflowInstanceId || '').trim() || undefined,
        app_id: canonicalAppManifestID(app),
        app_name: app.name,
        title: String(evidence.recordID || app.name).trim() || app.name,
        lane: approvalLaneFromEvidence(evidence),
        status,
        current_node: currentNode || undefined,
        current_node_ids: currentNodeIDs.length > 0 ? currentNodeIDs : currentNode ? [currentNode] : undefined,
        workflow_node_ids: currentNodeIDs.length > 0 ? currentNodeIDs : currentNode ? [currentNode] : undefined,
        owner: isZh(lang) ? '\u5f53\u524d\u7528\u6237' : 'Current user',
        applicant: isZh(lang) ? '\u5f53\u524d\u7528\u6237' : 'Current user',
        approver: isZh(lang) ? '\u5ba1\u6279\u4eba' : 'Approver',
        updated_at: evidence.verifiedAt || new Date().toISOString(),
        result: String(evidence.result || evidence.resultStatus || evidence.businessStatus || status).trim(),
        workflow_skill_id: workflowSkillID || undefined,
        workflow_version: evidence.workflowVersion,
        approval_workflow_id: evidence.approvalWorkflowID || workflowSkillID || undefined,
        approval_event: evidence.approvalEvent,
        approval_object_role: evidence.approvalObjectRole || evidence.objectRole,
        object_role: evidence.objectRole || evidence.approvalObjectRole,
        dataset_id: evidence.datasetID || appDataSrvDatasetID(app) || undefined,
        blueprint_id: evidence.blueprintID || app.manifest?.datasrv?.blueprintID || undefined,
        business_status: evidence.businessStatus,
        result_status: evidence.resultStatus || status,
        record_id: evidence.recordID || undefined,
        detail_url: evidence.detailURL,
        result_payload: evidence.resultPayload,
        outputs: evidence.outputs,
        artifacts: evidence.artifacts,
        events: [{
            at: evidence.verifiedAt || new Date().toISOString(),
            node: currentNode || undefined,
            actor: 'install_evidence',
            decision: status,
            action: 'approval_instance_seeded',
            message: String(evidence.result || evidence.resultStatus || evidence.businessStatus || status).trim(),
        }],
    };
}

function appSeedApprovalInstances(app: AppEntry, lang?: string): ApprovalInstanceView[] {
    if (!isEnterpriseApprovalAppKind(app.kind)) return [];
    const candidates = [
        normalizeAppRunApprovalInstanceEvidence(app.importedRunEvidence?.approvalInstance),
        normalizeAppRunApprovalInstanceEvidence(app.installEvidence?.test_evidence?.approval_instance || app.installEvidence?.test_evidence?.approvalInstance),
    ].filter(Boolean) as AppRunApprovalInstanceEvidence[];
    const views = candidates
        .map((evidence) => approvalEvidenceToBackendInstance(app, evidence, lang))
        .filter(Boolean)
        .map((instance) => backendApprovalInstanceToView(instance as BackendApprovalInstance, lang))
        .filter(Boolean) as ApprovalInstanceView[];
    return mergeApprovalInstanceViews([], views);
}
function approvalInstanceMergeKey(instance: ApprovalInstanceView): string {
    return [instance.approvalID, instance.id, instance.recordID].find((value) => String(value || '').trim()) || instance.id;
}

function mergeApprovalInstanceViews(current: ApprovalInstanceView[], incoming: ApprovalInstanceView[]) {
    const next: ApprovalInstanceView[] = [];
    const seen = new Set<string>();
    [...incoming, ...current].forEach((item) => {
        const key = approvalInstanceMergeKey(item);
        if (seen.has(key)) return;
        seen.add(key);
        next.push(item);
    });
    return next.slice(0, 50);
}

function appApprovalBinding(app: AppEntry): AppApprovalBinding | undefined {
    return app.manifest?.mis?.approvalBindings?.find((binding) => binding.workflowSkillId);
}

function appApprovalWorkflowSkillID(app: AppEntry): string {
    return appApprovalBinding(app)?.workflowSkillId || app.manifest?.dependencies?.skills?.find((dependency) => dependency.kind === 'workflow_skill')?.id || app.manifest?.appSkill?.id || app.manifest?.skill?.id || '';
}
function appApprovalWorkflowID(app: AppEntry): string {
    const binding = appApprovalBinding(app);
    return String(binding?.event || binding?.workflowSkillId || app.manifest?.appSkill?.id || app.manifest?.skill?.id || '').trim();
}

function appApprovalWorkflowVersion(app: AppEntry): string {
    const binding = appApprovalBinding(app);
    if (binding?.workflowVersion) return binding.workflowVersion;
    const workflowID = binding?.workflowSkillId || '';
    return app.manifest?.dependencies?.skills?.find((dependency) => dependency.kind === 'workflow_skill' && (!workflowID || dependency.id === workflowID))?.version || '';
}

function approvalWorkflowContractInputPayload(app: AppEntry, options: {
    instanceID: string;
    inputSummary: string;
    businessEntity: string;
    businessAction: string;
    businessNote: string;
    submittedAt: string;
    lang?: string;
}) {
    const appID = canonicalAppManifestID(app);
    const datasetID = appDataSrvDatasetID(app);
    const binding = appApprovalBinding(app);
    const objectRole = String(binding?.objectRole || app.manifest?.datasrv?.objectRole || '').trim();
    const blueprintID = String(app.manifest?.datasrv?.blueprintID || '').trim();
    const applicantName = isZh(options.lang) ? '\u5f53\u524d\u7528\u6237' : 'Current user';
    const recordRef = {
        app_id: appID,
        app_name: app.name,
        instance_id: options.instanceID,
        record_id: options.instanceID,
        dataset_id: datasetID,
        object_role: objectRole,
        blueprint_id: blueprintID,
        title: options.inputSummary,
    };
    const applicant = {
        id: 'current-user',
        name: applicantName,
        display_name: applicantName,
        type: 'user',
    };
    const businessPayload = {
        app_id: appID,
        app_name: app.name,
        approval_instance_id: options.instanceID,
        record_ref: recordRef,
        applicant,
        business_entity: options.businessEntity || app.category,
        business_action: options.businessAction || 'create',
        business_note: options.businessNote,
        dataset_id: datasetID,
        object_role: objectRole,
        blueprint_id: blueprintID,
        datasrv_domain: app.manifest?.datasrv?.domain || '',
        preferred_action: app.manifest?.datasrv?.preferredAction || '',
        preferred_view: app.manifest?.datasrv?.preferredView || '',
        submitted_at: options.submittedAt,
    };
    return {
        record_ref: recordRef,
        applicant,
        business_payload: businessPayload,
        workflow_contract: app.workflowContract,
        workflow_required_inputs: app.workflowContract?.requiredInputs || [],
        workflow_decision_outputs: app.workflowContract?.decisionOutputs || [],
        workflow_required_outputs: app.workflowContract?.requiredOutputs || [],
    };
}

function appDataSrvDatasetID(app: AppEntry): string {
    const datasrv = app.manifest?.datasrv;
    const explicit = String(datasrv?.datasetID || '').trim();
    if (explicit) return explicit;
    const domain = String(datasrv?.domain || '').trim();
    const actionName = String(datasrv?.preferredAction || '').split('.').pop() || '';
    const stem = actionName.replace(/_(upsert|create|submit|update|review|query|list).*$/i, '').trim();
    if (domain && stem) return `${domain}.${stem.endsWith('s') ? stem : `${stem}s`}`;
    return domain;
}

function approvalPayloadRecordID(payload?: Record<string, unknown>): string {
    const record = payload?.business_record;
    if (record && typeof record === 'object' && !Array.isArray(record)) {
        const value = (record as Record<string, unknown>).id || (record as Record<string, unknown>).record_id || (record as Record<string, unknown>).recordID;
        if (value !== undefined && value !== null) return String(value).trim();
    }
    const direct = payload?.record_id || payload?.recordID || payload?.business_record_id || payload?.businessRecordID;
    return direct === undefined || direct === null ? '' : String(direct).trim();
}

function appApprovalRecordID(instance: BackendApprovalInstance): string {
    return String(instance.record_id || approvalPayloadRecordID(instance.result_payload) || instance.instance_id || '').trim();
}

function approvalDataSrvSyncPayload(app: AppEntry, instance: BackendApprovalInstance) {
    const appID = canonicalAppManifestID(app);
	const datasetID = String(instance.dataset_id || appDataSrvDatasetID(app)).trim();
	const objectRole = String(instance.object_role || instance.approval_object_role || appApprovalBinding(app)?.objectRole || app.manifest?.datasrv?.objectRole || '').trim();
    const blueprintID = String(instance.blueprint_id || app.manifest?.datasrv?.blueprintID || '').trim();
    const recordID = appApprovalRecordID(instance);
    const binding = appApprovalBinding(app);
    const workflowSkillID = String(instance.workflow_skill_id || appApprovalWorkflowSkillID(app)).trim();
    const approvalWorkflowID = String(instance.approval_workflow_id || appApprovalWorkflowID(app) || workflowSkillID).trim();
    const triggerEvent = String(instance.approval_event || binding?.event || '').trim();
    const submittedBy = String(instance.submitted_by || instance.applicant || instance.owner || '').trim();
    const currentAssignee = String(instance.current_assignee || instance.approver || '').trim();
    const currentAssigneeType = String(instance.current_assignee_type || (currentAssignee ? 'user' : '')).trim();
    const fromStatus = String(instance.from_status || '').trim();
    const toStatus = String(instance.to_status || instance.business_status || instance.status || '').trim();
    const currentNodeIDs = normalizeApprovalCurrentNodeIDs(instance);
    if ((!datasetID && !objectRole) || !recordID) return null;
    return {
        dataset_id: datasetID,
        object_role: objectRole,
        app_id: appID,
        blueprint_id: blueprintID,
        record_id: recordID,
        approval_id: String(instance.approval_id || instance.record_approval_id || '').trim(),
        instance: {
            ...instance,
            app_id: appID,
            dataset_id: datasetID,
            object_role: objectRole,
            approval_object_role: instance.approval_object_role || objectRole,
            approval_workflow_id: approvalWorkflowID,
            approval_event: triggerEvent || instance.approval_event,
            trigger_event: triggerEvent,
            submitted_by: submittedBy,
            current_assignee: currentAssignee,
            current_assignee_type: currentAssigneeType,
            from_status: fromStatus,
            to_status: toStatus,
            current_node_ids: currentNodeIDs,
            workflow_node_ids: currentNodeIDs,
            workflow_skill_id: workflowSkillID || instance.workflow_skill_id,
            blueprint_id: blueprintID,
            record_id: recordID,
		},
	};
}

function approvalDataSrvSyncEvent(result: unknown, lang?: string) {
	const zh = isZh(lang);
	const record = result && typeof result === 'object' ? result as Record<string, any> : {};
	const synced = record.synced === true;
	const reason = String(record.reason || record.error || '').trim();
	return {
		at: new Date().toISOString(),
		node: 'datasrv_sync',
		actor: 'DataSrv',
		decision: synced ? 'synced' : 'sync_failed',
		action: synced ? 'datasrv_sync_ok' : 'datasrv_sync_failed',
		message: synced
            ? 'DataSrv sync completed'
            : (reason || 'DataSrv sync failed'),
		metadata: compactApprovalRecord(record),
	};
}

function firstNonEmptySyncString(...values: unknown[]) {
	for (const value of values) {
		const text = String(value || '').trim();
		if (text) return text;
	}
	return '';
}

function approvalDataSrvRecordFromSyncResult(result: unknown) {
	const record = result && typeof result === 'object' ? result as Record<string, any> : {};
	const response = record.response && typeof record.response === 'object' ? record.response as Record<string, any> : {};
	const responseItem = response.item && typeof response.item === 'object' ? response.item as Record<string, any> : {};
	return {
		approvalID: firstNonEmptySyncString(record.approval_id, record.approvalId, record.record_approval_id, record.recordApprovalID, response.approval_id, response.approvalId, response.record_approval_id, response.recordApprovalID, response.id, responseItem.approval_id, responseItem.id),
		recordID: firstNonEmptySyncString(record.record_id, record.recordID, response.record_id, response.recordID, responseItem.record_id, responseItem.recordID),
		datasetID: firstNonEmptySyncString(record.dataset_id, record.datasetID, response.dataset_id, response.datasetID, responseItem.dataset_id, responseItem.datasetID),
	};
}

function mergeApprovalInstanceDataSrvSyncResult(instance: BackendApprovalInstance, result: unknown): BackendApprovalInstance {
	const remote = approvalDataSrvRecordFromSyncResult(result);
	if (!remote.approvalID && !remote.recordID && !remote.datasetID) return instance;
	return {
		...instance,
		...(remote.approvalID ? { approval_id: remote.approvalID, record_approval_id: remote.approvalID } : {}),
		...(remote.recordID ? { record_id: remote.recordID } : {}),
		...(remote.datasetID ? { dataset_id: remote.datasetID } : {}),
	};
}
async function syncApprovalInstanceToDataSrvWithEvents(app: AppEntry, instance: BackendApprovalInstance, lang?: string): Promise<BackendApprovalInstance> {
	const syncPayload = approvalDataSrvSyncPayload(app, instance);
	if (!syncPayload) return instance;
	let syncResult: unknown;
	try {
		syncResult = await SyncMaclawAppApprovalInstanceToDataSrv(syncPayload);
	} catch (error: any) {
		syncResult = {
			synced: false,
            reason: error?.message || String(error || 'DataSrv sync failed'),
		};
	}
	const syncEvent = approvalDataSrvSyncEvent(syncResult, lang);
	const nextInstance: BackendApprovalInstance = {
		...mergeApprovalInstanceDataSrvSyncResult(instance, syncResult),
		updated_at: syncEvent.at,
		events: [...(instance.events || []), syncEvent],
	};
	try {
		return await RecordMaclawAppApprovalInstance(nextInstance) as BackendApprovalInstance || nextInstance;
	} catch {
		return nextInstance;
	}
}

function approvalStatusLabel(status: ApprovalInstanceView['status'], lang?: string) {
    const zh = isZh(lang);
    if (status === 'approved') return zh ? '\u5df2\u901a\u8fc7' : 'Approved';
    if (status === 'rejected') return zh ? '\u5df2\u9a73\u56de' : 'Rejected';
    if (status === 'attention') return zh ? '\u9700\u5173\u6ce8' : 'Needs attention';
    if (status === 'requires_input') return zh ? '\u5f85\u8865\u5145' : 'Needs input';
    if (status === 'failed') return zh ? '\u8fd0\u884c\u5931\u8d25' : 'Failed';
    if (status === 'cancelled') return zh ? '\u5df2\u53d6\u6d88' : 'Cancelled';
    if (status === 'timeout') return zh ? '\u5df2\u8d85\u65f6' : 'Timed out';
    if (status === 'pending' || status === 'running' || status === 'submitted' || status === 'in_progress') return zh ? '\u5ba1\u6279\u4e2d' : 'Pending';
    return zh ? '\u8349\u7a3f' : 'Draft';
}

function runStateFromApprovalLifecycleStatus(status?: string): 'running' | 'done' | 'error' | 'cancelled' {
    const value = String(status || '').trim().toLowerCase().replace('-', '_');
    if (['pending', 'running', 'submitted', 'requires_input', 'in_progress'].includes(value)) return 'running';
    if (['failed', 'error', 'timeout'].includes(value)) return 'error';
    if (['cancelled', 'canceled'].includes(value)) return 'cancelled';
    return 'done';
}

function approvalCurrentAssigneeText(instance: ApprovalInstanceView | undefined): string {
    return String(instance?.currentAssignee || instance?.approver || '').trim() || '-';
}

function approvalApplicantText(instance: ApprovalInstanceView | undefined): string {
    return String(instance?.applicant || instance?.owner || '').trim() || '-';
}

function approvalStatusTransitionText(instance: ApprovalInstanceView | undefined, lang?: string): string {
    if (!instance) return '-';
    const from = String(instance.fromStatus || '').trim();
    const to = String(instance.toStatus || instance.businessStatus || instance.resultStatus || '').trim();
    if (from && to) return `${from} -> ${to}`;
    return to || approvalStatusLabel(instance.status, lang);
}

function approvalInstanceCanDecide(instance: ApprovalInstanceView | undefined) {
    return !!instance && (instance.status === 'pending' || instance.status === 'draft' || instance.status === 'running' || instance.status === 'submitted' || instance.status === 'in_progress' || instance.lane === 'pending_my_approval');
}

function approvalRequiresInputInfo(instance: ApprovalInstanceView | undefined, lang?: string) {
    if (!instance) return null;
    const status = String(instance.status || instance.resultStatus || '').trim().toLowerCase().replace('-', '_');
    const payload = instance.resultPayload && typeof instance.resultPayload === 'object' && !Array.isArray(instance.resultPayload) ? instance.resultPayload : {};
    const raw = (payload as Record<string, unknown>).requires_input || (payload as Record<string, unknown>).requiresInput;
    const rawMap = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw as Record<string, unknown> : {};
    const fields = Array.isArray(rawMap.fields)
        ? rawMap.fields.map((item) => String(item || '').trim()).filter(Boolean)
        : Array.isArray((payload as Record<string, unknown>).missing_fields)
            ? ((payload as Record<string, unknown>).missing_fields as unknown[]).map((item) => String(item || '').trim()).filter(Boolean)
            : [];
    const output = (instance.outputs || []).find((item) => approvalOutputKind(item) === 'requires_input');
    const message = String(rawMap.message || rawMap.reason || (payload as Record<string, unknown>).text || output?.text || instance.result || '').trim();
    const active = status === 'requires_input' || !!raw || !!output;
    if (!active) return null;
    return {
        title: isZh(lang) ? '\u5f85\u8865\u5145\u6750\u6599' : 'Supplement required',
        message: message || (isZh(lang) ? '\u8bf7\u8865\u5145\u5fc5\u8981\u6750\u6599\u540e\u7ee7\u7eed\u6d41\u8f6c' : 'Add the missing information and continue the workflow.'),
        fields,
    };
}
function approvalDecisionResultPayload(instance: ApprovalInstanceView, decision: 'approved' | 'rejected' | 'attention', context?: {
    resultText?: string;
    currentNode?: string;
    workflowDecisionID?: string;
    currentNodeIDs?: string[];
    fromStatus?: string;
}) {
    const base = instance.resultPayload && typeof instance.resultPayload === 'object' && !Array.isArray(instance.resultPayload)
        ? { ...instance.resultPayload }
        : {};
    const currentNode = String(context?.currentNode || instance.currentNode || '').trim();
    const workflowDecisionID = String(context?.workflowDecisionID || instance.workflowDecisionID || '').trim();
    const resultText = String(context?.resultText || instance.result || '').trim();
    const currentNodeIDs = Array.isArray(context?.currentNodeIDs) && context.currentNodeIDs.length > 0
        ? context.currentNodeIDs
        : instance.currentNodeIDs || [];
    const fromStatus = String(context?.fromStatus || instance.businessStatus || instance.status || instance.fromStatus || '').trim();
    return {
        ...base,
        approval_result: decision,
        approval_status: decision,
        decision,
        business_status: decision,
        result_status: decision,
        ...(resultText ? { text: resultText } : {}),
        ...(currentNode ? { current_node: currentNode, approval_node: currentNode } : {}),
        ...(currentNodeIDs.length ? { current_node_ids: currentNodeIDs, workflow_node_ids: currentNodeIDs } : {}),
        ...(instance.workflowSkillID ? { workflow_skill_id: instance.workflowSkillID, workflowSkillId: instance.workflowSkillID } : {}),
        ...(instance.workflowVersion ? { workflow_version: instance.workflowVersion, workflowVersion: instance.workflowVersion } : {}),
        ...(instance.approvalWorkflowID ? { approval_workflow_id: instance.approvalWorkflowID, approvalWorkflowID: instance.approvalWorkflowID } : {}),
        ...(workflowDecisionID ? { workflow_decision_id: workflowDecisionID, workflowDecisionID } : {}),
        ...(instance.recordID ? { record_id: instance.recordID, recordID: instance.recordID } : {}),
        ...(instance.approvalID ? { approval_id: instance.approvalID, approvalID: instance.approvalID } : {}),
        ...(instance.datasetID ? { dataset_id: instance.datasetID, datasetID: instance.datasetID } : {}),
        ...(instance.objectRole ? { object_role: instance.objectRole, objectRole: instance.objectRole } : {}),
        ...(fromStatus ? { from_status: fromStatus, fromStatus } : {}),
        to_status: decision,
        toStatus: decision,
    };
}

function businessAppSkillID(app: AppEntry): string {
    return String(app.manifest?.appSkill?.id || '').trim();
}

function businessObjectRoleForApp(app: AppEntry, businessEntity: string): string {
    return String(app.manifest?.datasrv?.objectRole || app.manifest?.datasrv?.domain || businessEntity || app.category || '').trim();
}

function businessActionRoleForApp(app: AppEntry, businessAction: string): string {
    const datasrv = app.manifest?.datasrv;
    if (businessAction === 'query') return String(datasrv?.preferredView || datasrv?.preferredReport || businessAction).trim();
    if (businessAction === 'report') return String(datasrv?.preferredReport || datasrv?.preferredDashboard || datasrv?.preferredView || businessAction).trim();
    return String(datasrv?.preferredAction || businessAction || 'execute').trim();
}

function businessActionLabel(action: string, lang?: string) {
    const zh = isZh(lang);
    if (action === 'query') return zh ? '\u67e5\u8be2\u6570\u636e' : 'Query data';
    if (action === 'report') return zh ? '\u751f\u6210\u62a5\u8868' : 'Generate report';
    return zh ? '\u65b0\u5efa\u8bb0\u5f55' : 'Create record';
}

const BusinessWorkspace = ({ app, runState, businessEntity, businessAction, businessNote, lang, style, layoutRegion }: { app: AppEntry; runState: 'idle' | 'running' | 'done' | 'error' | 'cancelled'; businessEntity: string; businessAction: string; businessNote: string; lang?: string; style?: CSSProperties; layoutRegion?: string }) => {
    const zh = isZh(lang);
    const datasrv = app.manifest?.datasrv || { domain: 'DataSrv' };
    const objectRole = businessObjectRoleForApp(app, businessEntity);
    const actionRole = businessActionRoleForApp(app, businessAction);
    const viewRole = String(datasrv.preferredView || datasrv.preferredReport || datasrv.preferredDashboard || '-').trim();
    const appSkillID = businessAppSkillID(app);
    const datasetID = appDataSrvDatasetID(app);
    const navigationItems = appEnterpriseNavigation(app);
    const columnItems = appEnterpriseColumns(app);
    const columnLabel = (column: string) => {
        const labels: Record<string, string> = {
            title: zh ? '\u8bb0\u5f55' : 'Record',
            status: zh ? '\u72b6\u6001' : 'Status',
            owner: zh ? '\u8d1f\u8d23\u4eba' : 'Owner',
            updated_at: zh ? '\u66f4\u65b0' : 'Updated',
            current_node: zh ? '\u5f53\u524d\u8282\u70b9' : 'Current node',
        };
        return labels[column] || column;
    };
    const rowValue = (row: { id: string; name: string; status: string; owner: string }, column: string) => {
        if (column === 'title') return row.name;
        if (column === 'status') return row.status;
        if (column === 'owner') return row.owner;
        if (column === 'updated_at') return runState === 'done' ? (zh ? '\u521a\u521a' : 'Just now') : '-';
        if (column === 'current_node') return runState === 'running' ? (zh ? '\u6267\u884c\u4e2d' : 'Running') : (zh ? '\u5f85\u6267\u884c' : 'Ready');
        return '-';
    };
    const bindingItems = [
        { label: zh ? '\u4e1a\u52a1\u57df' : 'DataSrv', value: datasrv.domain || '-' },
        { label: zh ? '\u4e1a\u52a1\u5bf9\u8c61' : 'Object', value: objectRole || '-' },
        { label: zh ? '\u6570\u636e\u96c6' : 'Dataset', value: datasetID || '-' },
        { label: 'appSkill', value: appSkillID || '-' },
    ];
    const operationItems = [
        { label: zh ? '\u52a8\u4f5c' : 'Action', value: datasrv.preferredAction || '-' },
        { label: zh ? '\u89c6\u56fe' : 'View', value: datasrv.preferredView || '-' },
        { label: zh ? '\u62a5\u8868' : 'Report', value: datasrv.preferredReport || '-' },
        { label: zh ? '\u770b\u677f' : 'Dashboard', value: datasrv.preferredDashboard || '-' },
    ];
    const rows = [
        { id: `${datasrv.domain || app.id}-draft`, name: businessEntity || app.category, status: runState === 'idle' ? (zh ? '\u5f85\u5904\u7406' : 'Ready') : (zh ? '\u5904\u7406\u4e2d' : 'Processing'), owner: zh ? '\u5f53\u524d\u7528\u6237' : 'Current user' },
        { id: `${datasrv.domain || app.id}-last`, name: app.name, status: runState === 'done' ? (zh ? '\u5df2\u66f4\u65b0' : 'Updated') : (zh ? '\u53ef\u67e5\u8be2' : 'Queryable'), owner: datasrv.domain || 'DataSrv' },
    ];
    return (
        <section className="apps-runtime-section apps-business-workspace" aria-label={zh ? '\u4e1a\u52a1\u5de5\u4f5c\u53f0' : 'Business workspace'} data-region={layoutRegion || 'center'} style={style}>
            <div className="apps-preview-title-row">
                <div className="apps-runtime-section__title">{zh ? '\u4e1a\u52a1\u5de5\u4f5c\u53f0' : 'Business workspace'}</div>
                <span className="apps-count">{datasrv.domain || 'DataSrv'}</span>
            </div>
            <div className="apps-business-toolbar" aria-label={zh ? '\u4e1a\u52a1\u64cd\u4f5c' : 'Business actions'}>
                <button type="button" data-active={businessAction === 'create' ? 'true' : undefined}>{zh ? '\u65b0\u5efa' : 'New'}</button>
                <button type="button" data-active={businessAction === 'query' ? 'true' : undefined}>{zh ? '\u67e5\u8be2' : 'Query'}</button>
                <button type="button" data-active={businessAction === 'report' ? 'true' : undefined}>{zh ? '\u62a5\u8868' : 'Report'}</button>
                <button type="button">{zh ? '\u5bfc\u51fa' : 'Export'}</button>
            </div>
            <div className="apps-business-binding" aria-label={zh ? '\u4e1a\u52a1\u7ed1\u5b9a' : 'Business binding'}>
                <dl>
                    {bindingItems.map((item) => <div key={item.label}><dt>{item.label}</dt><dd>{item.value}</dd></div>)}
                </dl>
                <dl>
                    {operationItems.map((item) => <div key={item.label}><dt>{item.label}</dt><dd>{item.value}</dd></div>)}
                </dl>
            </div>
            <div className="apps-business-layout">
                <nav className="apps-business-nav" aria-label={zh ? '\u89c6\u56fe' : 'Views'}>
                    {navigationItems.map((item) => <button key={item} type="button">{item}</button>)}
                </nav>
                <div className="apps-business-table" role="table" aria-label={zh ? '\u4e1a\u52a1\u6570\u636e' : 'Business records'}>
                    <div role="row" className="apps-business-table__head">{columnItems.map((column) => <span key={column}>{columnLabel(column)}</span>)}</div>
                    {rows.map((row) => (
                        <div role="row" className="apps-business-table__row" data-state={runState} key={row.id}>
                            {columnItems.map((column) => <span key={row.id + '-' + column}>{rowValue(row, column)}</span>)}
                        </div>
                    ))}
                </div>
                <aside className="apps-business-detail">
                    <dl>
                        <div><dt>{zh ? '\u4e1a\u52a1\u5bf9\u8c61' : 'Object'}</dt><dd>{objectRole || '-'}</dd></div>
                        <div><dt>{zh ? '\u64cd\u4f5c\u89d2\u8272' : 'Action role'}</dt><dd>{actionRole || businessActionLabel(businessAction, lang)}</dd></div>
                        <div><dt>{zh ? '\u89c6\u56fe' : 'View'}</dt><dd>{viewRole || '-'}</dd></div>
                        <div><dt>{zh ? '\u8fd0\u884c\u72b6\u6001' : 'Run state'}</dt><dd>{runState === 'done' ? (zh ? '\u5df2\u5b8c\u6210' : 'Done') : runState === 'running' ? (zh ? '\u6267\u884c\u4e2d' : 'Running') : (zh ? '\u5f85\u6267\u884c' : 'Ready')}</dd></div>
                    </dl>
                    <div className="apps-business-note"><strong>{zh ? '\u64cd\u4f5c\u610f\u56fe' : 'Intent'}</strong><span>{businessNote.trim() || (zh ? '\u7b49\u5f85\u8f93\u5165\u4e1a\u52a1\u610f\u56fe' : 'Waiting for business intent')}</span></div>
                </aside>
            </div>
        </section>
    );
};
const AppPreview = ({ app, lang, onUse, onOpenApprovalManager, onActiveRunChange }: { app?: AppEntry; lang?: string; onUse?: (appId: string) => void; onOpenApprovalManager?: (appId?: string) => void; onActiveRunChange?: () => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [fileName, setFileName] = useState('');
    const [selectedFile, setSelectedFile] = useState<File | null>(null);
    const [selectedFiles, setSelectedFiles] = useState<File[]>([]);
    const [fileInputResetKey, setFileInputResetKey] = useState(0);
    const [toolParams, setToolParams] = useState('');
    const [fieldValues, setFieldValues] = useState<Record<string, string | boolean>>({});
    const [outputMode, setOutputMode] = useState('docx');
    const [businessEntity, setBusinessEntity] = useState(app?.category || '');
    const [businessAction, setBusinessAction] = useState('create');
    const [businessNote, setBusinessNote] = useState('');
    const [runState, setRunState] = useState<'idle' | 'running' | 'done' | 'error' | 'cancelled'>('idle');
	const [validationMessage, setValidationMessage] = useState('');
	const [runID, setRunID] = useState('');
	const [showAboutDialog, setShowAboutDialog] = useState(false);
	const [skillRunStatus, setSkillRunStatus] = useState<SkillRunStatusView | null>(null);
	const [businessResult, setBusinessResult] = useState<BusinessOperationResultView | null>(null);
	const [runtimeBusinessError, setRuntimeBusinessError] = useState<StructuredBusinessErrorView | null>(null);
	const [runHistory, setRunHistory] = useState<AppRunHistoryEntry[]>([]);
    const [approvalInstances, setApprovalInstances] = useState<ApprovalInstanceView[]>([]);
    const [approvalInstancesLoadState, setApprovalInstancesLoadState] = useState<'idle' | 'loading' | 'error'>('idle');
    const [currentRunContext, setCurrentRunContext] = useState({ inputSummary: '', outputMode: '' });
    const [dependencyRepairState, setDependencyRepairState] = useState<'idle' | 'installing'>('idle');
    const [runtimeDependencyPlan, setRuntimeDependencyPlan] = useState<BackendAppInstallPlan | null>(null);
    const [runtimeDependencyCheckState, setRuntimeDependencyCheckState] = useState<'idle' | 'checking' | 'ready' | 'blocked' | 'error'>('idle');
    const approvalRunContextRef = useRef<ApprovalRunContext | null>(null);
    const activeRunDependencyPlanRef = useRef<BackendAppInstallPlan | null>(null);
    const appDefinitionKey = useMemo(() => app ? appDefinitionFingerprint(app) : '', [app]);
    useEffect(() => {
        setFileName('');
        setSelectedFile(null);
        setSelectedFiles([]);
        setFileInputResetKey((key) => key + 1);
        setToolParams('');
        setFieldValues({});
        setOutputMode(normalizeOutputModes(app?.manifest?.skill?.outputModes)[0]);
        setBusinessEntity(app?.category || '');
        setBusinessAction('create');
        setBusinessNote('');
        const activeSession = app ? loadActiveAppRunSession(app.id) : null;
        const shouldRestoreActiveRun = !!app && activeRunSessionMatchesApp(app, activeSession);
        if (app && activeSession && !shouldRestoreActiveRun) {
            clearActiveAppRunSession(app.id, activeSession.runID);
            onActiveRunChange?.();
        }
        setRunState(shouldRestoreActiveRun ? 'running' : 'idle');
        setValidationMessage(shouldRestoreActiveRun ? text.skillRunRunning : '');
        setRunID(shouldRestoreActiveRun ? activeSession?.runID || '' : '');
        setSkillRunStatus(null);
        setBusinessResult(null);
        setRuntimeBusinessError(null);
        setCurrentRunContext(shouldRestoreActiveRun
            ? { inputSummary: activeSession?.inputSummary || '', outputMode: activeSession?.outputMode || normalizeOutputModes(app?.manifest?.skill?.outputModes)[0] }
            : { inputSummary: '', outputMode: '' });
        setDependencyRepairState('idle');
        setRuntimeDependencyPlan(null);
        setRuntimeDependencyCheckState('idle');
        approvalRunContextRef.current = shouldRestoreActiveRun && activeSession?.approvalInstance ? { instance: activeSession.approvalInstance } : null;
        activeRunDependencyPlanRef.current = null;
        setRunHistory(appSeedRunHistory(app));
        const restoredApprovalView = app && shouldRestoreActiveRun && activeSession?.approvalInstance
            ? backendApprovalInstanceToView(activeSession.approvalInstance, lang)
            : null;
        const seededApprovalInstances = app ? appSeedApprovalInstances(app, lang) : [];
        setApprovalInstances(restoredApprovalView ? mergeApprovalInstanceViews(seededApprovalInstances, [restoredApprovalView]) : seededApprovalInstances);
        setApprovalInstancesLoadState('idle');
    }, [app?.id, app?.manifest?.skill?.outputModes, appDefinitionKey, lang, onActiveRunChange, text.skillRunRunning]);
    useEffect(() => {
        if (!app || !appNeedsAutomaticRuntimeDependencyCheck(app)) {
            setRuntimeDependencyPlan(null);
            setRuntimeDependencyCheckState('idle');
            activeRunDependencyPlanRef.current = null;
            return;
        }
        let disposed = false;
        setRuntimeDependencyCheckState('checking');
        const checkDependencies = async () => {
            try {
                const dependencyPlan = await PlanMaclawAppInstall(JSON.stringify(appToManifest(app)));
                if (disposed) return;
                setRuntimeDependencyPlan(dependencyPlan || null);
                activeRunDependencyPlanRef.current = dependencyPlan || null;
                const blocked = runtimeInstallPlanBlocked(dependencyPlan, app);
                setRuntimeDependencyCheckState(blocked ? 'blocked' : 'ready');
            } catch {
                if (disposed) return;
                setRuntimeDependencyPlan(null);
                activeRunDependencyPlanRef.current = null;
                setRuntimeDependencyCheckState('error');
            }
        };
        void checkDependencies();
        return () => {
            disposed = true;
        };
    }, [app?.id, app?.kind, app?.source, app?.version]);
    const loadApprovalInstances = useCallback(async (lane: ApprovalLaneFilter = 'all') => {
        if (!app?.id || !isEnterpriseApprovalAppKind(app.kind)) {
            setApprovalInstances([]);
            setApprovalInstancesLoadState('idle');
            return;
        }
        setApprovalInstancesLoadState('loading');
        try {
            const records = await ListMaclawAppApprovalInstances(canonicalAppManifestID(app), lane, 50) as BackendApprovalInstance[];
            const views = (records || []).map((item) => backendApprovalInstanceToView(item, lang)).filter(Boolean) as ApprovalInstanceView[];
            setApprovalInstances((current) => lane === 'all' ? (views.length > 0 ? views : current) : mergeApprovalInstanceViews(current, views));
            setApprovalInstancesLoadState('idle');
        } catch {
            setApprovalInstancesLoadState('error');
            if (lane === 'all') setApprovalInstances([]);
        }
    }, [app?.id, app?.kind, lang]);
    useEffect(() => {
        void loadApprovalInstances('all');
    }, [loadApprovalInstances]);
	const recordRunHistory = (entry: Omit<AppRunHistoryEntry, 'appID' | 'at'>) => {
		const appID = app?.id || '';
		if (!appID) return;
			const verifiedAt = new Date().toISOString();
            const definitionHash = app ? appDefinitionFingerprint(app) : undefined;
            const protocolFingerprint = app ? appTestProtocolFingerprint(appTestProtocolForManifest(app)) : undefined;
            const workspaceLayoutFingerprint = app ? appWorkspaceLayoutEvidence(app).fingerprint : undefined;
            const dependencyVerificationPlan = activeRunDependencyPlanRef.current || runtimeDependencyPlan;
            const dependencyVerification = entry.dependencyVerification || (app && entry.status === 'done'
                ? appRunDependencyVerificationEvidence(app, dependencyVerificationPlan, verifiedAt)
                : undefined);
            const nextEntry: AppRunHistoryEntry = { ...entry, definitionHash: entry.definitionHash || definitionHash, testProtocolFingerprint: entry.testProtocolFingerprint || protocolFingerprint, workspaceLayoutFingerprint: entry.workspaceLayoutFingerprint || workspaceLayoutFingerprint, dependencyVerification, appID, at: verifiedAt };
        if (app && nextEntry.status === 'done' && !nextEntry.resultCoverage) {
            const coverage = appRunEvidenceContractCoverage(app, nextEntry);
            nextEntry.resultCoverage = {
                ok: coverage.ok,
                primary: coverage.primary,
                coveredTypes: coverage.coveredTypes,
                missingTypes: coverage.missingTypes,
            };
        }
        setRunHistory((current) => {
            const next = [nextEntry, ...current.filter((item) => item.runID !== nextEntry.runID)].slice(0, 8);
            saveAppRunHistory(appID, next);
            return next;
        });
    };
    const rememberActiveRun = (runKind: AppActiveRunKind, activeRunID: string, inputSummary: string, activeOutputMode: string, approvalInstance?: BackendApprovalInstance) => {
        if (!app?.id || !activeRunID) return;
        saveActiveAppRunSession({
            appID: app.id,
            runID: activeRunID,
            runKind,
            inputSummary,
            outputMode: activeOutputMode,
            startedAt: new Date().toISOString(),
            definitionHash: appDefinitionFingerprint(app),
            approvalInstance,
        });
        onActiveRunChange?.();
    };
    const forgetActiveRun = (activeRunID?: string) => {
        if (!app?.id) return;
        clearActiveAppRunSession(app.id, activeRunID);
        onActiveRunChange?.();
    };
    const updateActiveRunApprovalInstance = (activeRunID: string, approvalInstance?: BackendApprovalInstance) => {
        if (!app?.id || !activeRunID || !approvalInstance) return;
        const session = loadActiveAppRunSession(app.id);
        if (!session || session.runID !== activeRunID || session.runKind !== 'approval_workflow') return;
        saveActiveAppRunSession({ ...session, approvalInstance });
    };
    const setRuntimeError = (error: unknown, fallback: string) => {
        const businessError = structuredBusinessErrorFromUnknown(error);
        setRuntimeBusinessError(businessError);
        const message = structuredBusinessErrorMessage(businessError, error instanceof Error ? error.message : String(error || fallback));
        setValidationMessage(message || fallback);
        setRunState('error');
        return message || fallback;
    };
    const recordApprovalRuntimeFailure = async (base: BackendApprovalInstance, message: string, inputSummary: string, runIDOverride?: string) => {
        const now = new Date().toISOString();
        const failureRunID = String(runIDOverride || base.workflow_decision_id || base.workflowDecisionID || `failed-${Date.now().toString(36)}`).trim();
        const failureText = message || text.skillRunFailed;
        const resultPayload = {
            ...((base.result_payload || base.resultPayload || {}) as Record<string, unknown>),
            approval_result: 'failed',
            approval_status: 'failed',
            decision: 'failed',
            business_status: 'workflow_error',
            result_status: 'failed',
            text: failureText,
            workflow_lifecycle: 'error',
        };
        const payload: BackendApprovalInstance = {
            ...base,
            workflow_decision_id: failureRunID,
            lane: 'handled',
            status: 'failed',
            current_node: base.current_node || base.currentNode || (isZh(lang) ? '\u8fd0\u884c\u5931\u8d25' : 'Failed'),
            result: failureText,
            business_status: 'workflow_error',
            result_status: 'failed',
            to_status: 'failed',
            current_assignee: 'system',
            current_assignee_type: 'system',
            result_payload: resultPayload,
            outputs: [{ kind: 'approval_result', title: text.approvalResult, text: failureText, status: 'failed' }],
            updated_at: now,
            detail_url: base.detail_url || (failureRunID ? `approval-error://${failureRunID}` : undefined),
            events: [
                ...(base.events || []),
                { at: now, node: base.current_node || base.currentNode || '', actor: 'workflow', decision: 'failed', action: 'workflow_failed', message: failureText },
            ],
        };
        approvalRunContextRef.current = null;
        const fallbackView = backendApprovalInstanceToView(payload, lang);
        if (fallbackView) {
            setApprovalInstances((current) => mergeApprovalInstanceViews(current, [fallbackView]).slice(0, 50));
        }
        let savedInstance = payload;
        try {
            savedInstance = await RecordMaclawAppApprovalInstance(payload) as BackendApprovalInstance || payload;
            const savedView = backendApprovalInstanceToView(savedInstance, lang);
            if (savedView) {
                setApprovalInstances((current) => mergeApprovalInstanceViews(current, [savedView]).slice(0, 50));
            }
            if (app) {
                const syncedInstance = await syncApprovalInstanceToDataSrvWithEvents(app, savedInstance, lang);
                savedInstance = syncedInstance;
                const syncedView = backendApprovalInstanceToView(syncedInstance, lang);
                if (syncedView) {
                    setApprovalInstances((current) => mergeApprovalInstanceViews(current, [syncedView]).slice(0, 50));
                }
            }
        } catch {
            // The local failed instance is already visible; DataSrv recovery can be retried from the approval center.
        }
        const approvalInstance = appRunApprovalInstanceEvidenceFromBackend(savedInstance, now);
        recordRunHistory({
            runID: failureRunID,
            status: 'error',
            outputMode: 'approval',
            inputSummary,
            message: failureText,
            resultPayload,
            outputs: payload.outputs,
            approvalInstance,
        });
    };
	const updateApprovalRunProgressFromStatus = useCallback(async (status: SkillRunStatusView | null) => {
        const context = approvalRunContextRef.current;
        if (!context || !app?.id || !isEnterpriseApprovalAppKind(app.kind)) return;
        const progress = approvalWorkflowProgressFromSkillRunStatus(status, lang);
        if (!progress) return;
        const previous = context.instance;
        const currentNodeIDs = mergeApprovalWorkflowNodeIDs(previous.current_node_ids || previous.currentNodeIDs || previous.workflow_node_ids || previous.workflowNodeIDs, progress.currentNodeIDs, progress.currentNode || previous.current_node || previous.currentNode);
        const progressKey = JSON.stringify({ node: progress.currentNode || previous.current_node || previous.currentNode || '', nodes: currentNodeIDs || [], businessStatus: progress.businessStatus, resultStatus: progress.resultStatus, text: progress.progressText });
        if (context.lastProgressKey === progressKey) return;
        const now = new Date().toISOString();
        const nextInstance: BackendApprovalInstance = {
            ...previous,
            current_node: progress.currentNode || previous.current_node || previous.currentNode,
            current_node_ids: currentNodeIDs,
            workflow_node_ids: currentNodeIDs,
            business_status: progress.businessStatus || previous.business_status || previous.businessStatus || 'approval_running',
            result_status: progress.resultStatus || previous.result_status || previous.resultStatus || 'running',
            result: progress.progressText || previous.result,
            updated_at: now,
            events: [
                ...(previous.events || []),
                {
                    at: now,
                    node: progress.currentNode || previous.current_node || previous.currentNode,
                    actor: 'workflow',
                    decision: 'running',
                    action: 'workflow_progress',
                    message: progress.progressText,
                    metadata: { run_id: status?.run_id || runID, status: status?.status || 'running' },
                },
            ],
        };
        approvalRunContextRef.current = { instance: nextInstance, lastProgressKey: progressKey };
        updateActiveRunApprovalInstance(status?.run_id || runID, nextInstance);
        const view = backendApprovalInstanceToView(nextInstance, lang);
        if (view) {
            setApprovalInstances((current) => [view, ...current.filter((item) => item.id !== view.id)].slice(0, 50));
        }
        try {
            const saved = await RecordMaclawAppApprovalInstance(nextInstance) as BackendApprovalInstance;
            if (approvalRunContextRef.current?.lastProgressKey === progressKey) {
                approvalRunContextRef.current = { instance: saved || nextInstance, lastProgressKey: progressKey };
            }
            const savedInstance = saved || nextInstance;
            updateActiveRunApprovalInstance(status?.run_id || runID, savedInstance);
            const savedView = backendApprovalInstanceToView(savedInstance, lang);
            if (savedView) {
                setApprovalInstances((current) => [savedView, ...current.filter((item) => item.id !== savedView.id)].slice(0, 50));
            }
            if (String(savedInstance.approval_id || savedInstance.record_approval_id || '').trim()) {
                const syncedInstance = await syncApprovalInstanceToDataSrvWithEvents(app, savedInstance, lang);
                if (approvalRunContextRef.current?.lastProgressKey === progressKey) {
                    approvalRunContextRef.current = { instance: syncedInstance, lastProgressKey: progressKey };
                }
                updateActiveRunApprovalInstance(status?.run_id || runID, syncedInstance);
                const syncedView = backendApprovalInstanceToView(syncedInstance, lang);
                if (syncedView) {
                    setApprovalInstances((current) => [syncedView, ...current.filter((item) => item.id !== syncedView.id)].slice(0, 50));
                }
            }
        } catch {
            // Running progress is best-effort; final approval completion still records and syncs authoritatively.
        }
    }, [app, lang, runID]);	const finalizeApprovalRunFromStatus = useCallback(async (status: SkillRunStatusView | null, lifecycle: 'done' | 'error' | 'cancelled' | 'timeout') => {
        const context = approvalRunContextRef.current;
        if (!context || !app?.id || !isEnterpriseApprovalAppKind(app.kind)) return undefined;
        approvalRunContextRef.current = null;
        const completion = approvalWorkflowResultFromSkillRunStatus(status, lifecycle, lang);
        const now = new Date().toISOString();
        const previous = context.instance;
        const payload: BackendApprovalInstance = {
            ...previous,
            instance_id: previous.instance_id,
            app_id: canonicalAppManifestID(app),
            app_name: previous.app_name || app.name,
            workflow_decision_id: runID || previous.workflow_decision_id,
            lane: completion.lane,
            status: completion.status,
            current_node: completion.currentNode,
            current_node_ids: mergeApprovalWorkflowNodeIDs(previous.current_node_ids || previous.currentNodeIDs || previous.workflow_node_ids || previous.workflowNodeIDs, completion.currentNodeIDs, completion.currentNode),
            workflow_node_ids: mergeApprovalWorkflowNodeIDs(previous.workflow_node_ids || previous.workflowNodeIDs || previous.current_node_ids || previous.currentNodeIDs, completion.currentNodeIDs, completion.currentNode),
            result: completion.resultText,
            business_status: completion.businessStatus,
            result_status: completion.resultStatus,
            result_payload: completion.resultPayload || previous.result_payload,
            outputs: completion.outputs || previous.outputs,
            artifacts: completion.artifacts || previous.artifacts,
            updated_at: now,
            detail_url: completion.detailURL || previous.detail_url || (runID ? `skill-run://${runID}` : undefined),
            events: [
                ...(previous.events || []),
                {
                    at: now,
                    node: completion.currentNode,
                    actor: 'workflow',
                    decision: completion.status,
                    action: completion.eventAction,
                    message: completion.resultText,
                },
            ],
        };
        if (completion.recordID) payload.record_id = completion.recordID;
        const fallback = backendApprovalInstanceToView(payload, lang);
        if (fallback) {
            setApprovalInstances((current) => [fallback, ...current.filter((item) => item.id !== fallback.id)].slice(0, 50));
        }
        try {
            const saved = await RecordMaclawAppApprovalInstance(payload) as BackendApprovalInstance;
            const savedInstance = saved || payload;
            const view = backendApprovalInstanceToView(savedInstance, lang);
            if (view) {
                setApprovalInstances((current) => [view, ...current.filter((item) => item.id !== view.id)].slice(0, 50));
            }
            const syncedInstance = await syncApprovalInstanceToDataSrvWithEvents(app, savedInstance, lang);
            const syncedView = backendApprovalInstanceToView(syncedInstance, lang);
            if (syncedView) {
                setApprovalInstances((current) => [syncedView, ...current.filter((item) => item.id !== syncedView.id)].slice(0, 50));
            }
            return syncedInstance;
        } catch {
            // Completion itself is authoritative once the Skill run reaches a terminal state.
            // Approval persistence/DataSrv sync is best-effort here; keep the completed run UI intact.
            return payload;
        }
    }, [app, lang, runID]);
    useEffect(() => {
        if (!runID || runState !== 'running') return;
        let disposed = false;
        const poll = async () => {
            try {
                const status = await GetNLSkillRunStatus(runID) as SkillRunStatusView;
                if (disposed) return;
                setSkillRunStatus(status || null);
                const lifecycle = normalizeSkillRunLifecycle(status?.status);
                if (lifecycle === 'running') {
                    await updateApprovalRunProgressFromStatus(status || null);
                    return;
                }
                if (lifecycle === 'done') {
                    const artifacts = skillRunArtifacts(status);
                    const artifact = artifacts[0] || null;
                    const artifactPath = String(artifact?.path || '').trim();
                    const artifactID = String(artifact?.id || '').trim();
                    const artifactURI = String(artifact?.uri || '').trim();
                    const artifactName = String(artifact?.name || artifactPath.split(/[\\/]/).pop() || '').trim();
                    const artifactDownloadState = String(artifact?.download_state || '').trim();
                    const definitionHash = app ? appDefinitionFingerprint(app) : undefined;
                    const resultPayload = appRunResultPayloadFromStatus(status);
                    const outputs = normalizeApprovalOutputs(skillRunOutputBlocks(status));
                    const resultContract = app ? appResultContractForManifest(app) : buildAppResultContract('tool_app', [currentRunContext.outputMode]);
                    const primaryResult = appRunPrimaryResultFromPayload(resultContract, resultPayload, outputs);
                    const verifiedAt = new Date().toISOString();
                    const dependencyVerificationPlan = activeRunDependencyPlanRef.current || runtimeDependencyPlan;
                    const dependencyVerification = app ? appRunDependencyVerificationEvidence(app, dependencyVerificationPlan, verifiedAt) : undefined;
                    setValidationMessage('');
                    setRuntimeBusinessError(null);
                    setRunState('done');
                    forgetActiveRun(runID);
                    const finalizedApprovalInstance = await finalizeApprovalRunFromStatus(status || null, lifecycle);
                    const approvalInstance = app && isEnterpriseApprovalAppKind(app.kind) && finalizedApprovalInstance
                        ? appRunApprovalInstanceEvidenceFromBackend(finalizedApprovalInstance, verifiedAt)
                        : undefined;
                    recordRunHistory({
                        runID,
                        status: 'done',
                        definitionHash,
                        outputMode: currentRunContext.outputMode,
                        inputSummary: currentRunContext.inputSummary,
                        message: primaryResult.slice(0, 180) || skillRunOutputSuffix(status).replace(/^ · /, '') || text.skillRunCompleted,
                        artifactID,
                        artifactURI,
                        artifactName,
                        artifactPath,
                        artifactDownloadState,
                        artifacts,
                        resultPayload,
                        outputs,
                        dependencyVerification,
                        approvalInstance,
                    });
                    const evidenceSkillID = appRunnableSkillID(app, activeRunDependencyPlanRef.current || runtimeDependencyPlan || runtimeInstallEvidencePlan || null);
                    if (app?.source === 'skill' && evidenceSkillID) {
                        void RecordMaclawAppRunEvidenceForSkill(evidenceSkillID, app.id, definitionHash || '', runID, artifactPath || artifactName || artifactURI, verifiedAt).catch(() => undefined);
                    }
                } else if (lifecycle === 'error') {
                    const businessError = structuredBusinessErrorFromUnknown(skillRunErrorMessage(status));
                    const message = structuredBusinessErrorMessage(businessError, skillRunErrorMessage(status) || text.skillRunFailed);
                    setRuntimeBusinessError(businessError);
                    setValidationMessage(message);
                    setRunState('error');
                    forgetActiveRun(runID);
                    recordRunHistory({ runID, status: 'error', outputMode: currentRunContext.outputMode, inputSummary: currentRunContext.inputSummary, message });
                    await finalizeApprovalRunFromStatus(status || null, lifecycle);
                } else if (lifecycle === 'cancelled') {
                    setValidationMessage(text.skillRunCancelled);
                    setRunState('cancelled');
                    forgetActiveRun(runID);
                    recordRunHistory({ runID, status: 'cancelled', outputMode: currentRunContext.outputMode, inputSummary: currentRunContext.inputSummary, message: text.skillRunCancelled });
                    await finalizeApprovalRunFromStatus(status || null, lifecycle);
                } else if (lifecycle === 'timeout') {
                    const message = skillRunErrorMessage(status) || (isZh(lang) ? '\u5de5\u4f5c\u6d41\u8fd0\u884c\u8d85\u65f6' : 'Workflow timed out');
                    setRuntimeBusinessError(null);
                    setValidationMessage(message);
                    setRunState('error');
                    forgetActiveRun(runID);
                    recordRunHistory({ runID, status: 'error', outputMode: currentRunContext.outputMode, inputSummary: currentRunContext.inputSummary, message });
                    await finalizeApprovalRunFromStatus(status || null, lifecycle);
                }
            } catch (error: any) {
                if (disposed) return;
                if (isActiveRunMissingError(error)) {
                    setSkillRunStatus(null);
                    setRuntimeBusinessError(null);
                    setValidationMessage(text.activeRunLost);
                    setRunState('error');
                    forgetActiveRun(runID);
                    approvalRunContextRef.current = null;
                    return;
                }
                const message = setRuntimeError(error, text.skillRunFailed);
                setBusinessResult(null);
                forgetActiveRun(runID);
                recordRunHistory({ runID, status: 'error', outputMode: currentRunContext.outputMode, inputSummary: currentRunContext.inputSummary, message });
                await finalizeApprovalRunFromStatus({ run_id: runID, status: 'error', error: message }, 'error');
            }
        };
        void poll();
        const timer = window.setInterval(poll, 1500);
        return () => {
            disposed = true;
            window.clearInterval(timer);
        };
    }, [runID, runState, text.activeRunLost, text.skillRunCancelled, text.skillRunCompleted, text.skillRunFailed, currentRunContext, finalizeApprovalRunFromStatus, updateApprovalRunProgressFromStatus, runtimeDependencyPlan]);
    if (!app) return <div className="apps-empty">{text.noApps}</div>;
    const isTool = app.kind === 'tool_app';
    const isApproval = isEnterpriseApprovalAppKind(app.kind);
    const isBusiness = app.kind === 'enterprise_normal_app';
    const isAutomation = app.kind === 'automation_app';
    const outputModes = normalizeOutputModes(app.manifest?.skill?.outputModes);
    const approvalBinding = appApprovalBinding(app);
    const workflowSkillID = appApprovalWorkflowSkillID(app);
    const workflowVersion = appApprovalWorkflowVersion(app);
    const approvalDatasetID = appDataSrvDatasetID(app);
    const approvalObjectRole = String(approvalBinding?.objectRole || app.manifest?.datasrv?.objectRole || '').trim();
    const approvalBlueprintID = String(app.manifest?.datasrv?.blueprintID || '').trim();
    const workflowMapping = app.manifest?.workflow;
    const businessSkillID = isBusiness ? businessAppSkillID(app) : '';
    const businessObjectRole = isBusiness ? businessObjectRoleForApp(app, businessEntity) : '';
    const businessActionRole = isBusiness ? businessActionRoleForApp(app, businessAction) : '';
    const inputMode = app.manifest?.skill?.inputMode || 'file';
    const allowMultipleFiles = !!app.manifest?.skill?.multipleFiles;
    const skillFields = normalizeSkillAppFields(app.manifest?.skill?.fields);
    const showFileInput = isTool && (inputMode === 'file' || inputMode === 'mixed');
    const showParamInput = isTool && (inputMode === 'form' || inputMode === 'mixed');
    const fieldSummary = skillFields.map((field) => {
        const value = fieldValues[field.name] ?? field.default;
        return value === undefined || value === '' ? '' : `${field.label || field.name}: ${String(value)}`;
    }).filter(Boolean).join(', ');
    const selectedFileLabel = selectedFiles.length > 1 ? (isZh(lang) ? `${selectedFiles.length} \u4e2a\u6587\u4ef6` : `${selectedFiles.length} files`) : fileName;
    const toolInputSummary = showFileInput && showParamInput
        ? `${selectedFileLabel || text.noFile}${(fieldSummary || toolParams.trim()) ? ` · ${(fieldSummary || toolParams.trim()).slice(0, 42)}` : ''}`
        : showParamInput
            ? (fieldSummary || toolParams.trim() || (isZh(lang) ? '\u672a\u586b\u5199\u53c2\u6570' : 'No parameters'))
            : (selectedFileLabel || text.noFile);
    const resultText = isTool
        ? `${text.generatedOutput}: ${toolInputSummary} -> ${outputMode.toUpperCase()}${runID ? ` · ${text.skillRunCompleted}: ${runID}` : ''}${skillRunOutputSuffix(skillRunStatus)}`
        : isAutomation
            ? (isZh(lang) ? '\u81ea\u52a8\u5316\u63a7\u5236\u53f0\u5df2\u542f\u52a8\uff0cAgent \u5c06\u6309\u5e94\u7528\u5b9a\u4e49\u6267\u884c\u548c\u56de\u62a5\u3002' : 'Automation console started. Agent will run and report by app definition.')
            : `${text.submitted}: ${businessEntity || app.category} · ${businessAction} · ${isBusiness ? businessActionRole : (app.manifest?.datasrv?.preferredAction || app.manifest?.datasrv?.domain || 'DataSrv')}`;
	const markDirty = () => {
        if (runState === 'running') return;
		setRunState('idle');
		setValidationMessage('');
		setRuntimeBusinessError(null);
		setRunID('');
		setSkillRunStatus(null);
        setCurrentRunContext({ inputSummary: '', outputMode: '' });
        setDependencyRepairState('idle');
        setRuntimeDependencyPlan(null);
        setRuntimeDependencyCheckState('idle');
        approvalRunContextRef.current = null;
        activeRunDependencyPlanRef.current = null;
    };
	const runApp = async (options?: { skipDependencyCheck?: boolean }) => {
		setDependencyRepairState('idle');
		setRuntimeBusinessError(null);
		approvalRunContextRef.current = null;
        activeRunDependencyPlanRef.current = options?.skipDependencyCheck
            ? activeRunDependencyPlanRef.current || runtimeDependencyPlan || runtimeInstallEvidencePlan || null
            : runtimeDependencyPlan || runtimeInstallEvidencePlan || null;
        if (app?.id) onUse?.(app.id);
        if (!options?.skipDependencyCheck && app && appNeedsRuntimeDependencyCheck(app)) {
            const existingPlanBlocked = runtimeInstallPlanBlocked(runtimeDependencyPlan, app);
            if (existingPlanBlocked) {
                setRuntimeDependencyCheckState('blocked');
                setValidationMessage(runtimeInstallPlanBlockMessage(app, runtimeDependencyPlan, text, lang));
                setRunState('error');
                return;
            }
            try {
                setRuntimeDependencyCheckState('checking');
                const dependencyPlan = await PlanMaclawAppInstall(JSON.stringify(appToManifest(app)));
                setRuntimeDependencyPlan(dependencyPlan || null);
                activeRunDependencyPlanRef.current = dependencyPlan || null;
                if (runtimeInstallPlanBlocked(dependencyPlan, app)) {
                    setRuntimeDependencyCheckState('blocked');
                    setValidationMessage(runtimeInstallPlanBlockMessage(app, dependencyPlan, text, lang));
                    setRunState('error');
                    return;
                }
                // Auto-install uninstalled dependencies (action="install") before running.
                // Without this, the plan says "installable" but no install is triggered,
                // and RunNLSkillAsync fails with "skill not found".
                const appIDs = appDependencyVerificationAppIDs(app);
                const needsAutoInstall = (dependencyPlan?.dependencies || []).some((dep: any) => {
                    if (!backendDependencyMatchesAppIDs(dep, appIDs)) return false;
                    return !dep.installed && String(dep.action || '').trim() === 'install' && dep.required !== false;
                });
                if (needsAutoInstall) {
                    setValidationMessage(text.installingDependencies);
                    const installResult = await InstallMaclawAppDependencies(JSON.stringify(appToManifest(app)));
                    setRuntimeDependencyPlan(installResult || null);
                    activeRunDependencyPlanRef.current = installResult || null;
                    if (runtimeInstallPlanBlocked(installResult, app)) {
                        setRuntimeDependencyCheckState('blocked');
                        setValidationMessage(runtimeInstallPlanBlockMessage(app, installResult, text, lang));
                        setRunState('error');
                        return;
                    }
                }
                setRuntimeDependencyCheckState('ready');
            } catch (error: any) {
                setRuntimeDependencyPlan(null);
                setRuntimeDependencyCheckState('error');
                const depNames = app ? appDependencyEvidence(app).map((d) => d.id).filter(Boolean) : [];
                const detail = error?.message || '';
                const depHint = depNames.length > 0 ? (isZh(lang) ? `\uff08\u9700\u8981\uff1a${depNames.join(', ')}\uff09` : ` (requires: ${depNames.join(', ')})`) : '';
                setValidationMessage((detail || text.dependencyPlanError) + depHint);
                setRunState('error');
                return;
            }
        }
		if (isTool) {
            const missingRequiredField = skillFields.find((field) => {
                if (!field.required || field.type === 'boolean') return false;
                const value = fieldValues[field.name] ?? field.default ?? '';
                return String(value).trim() === '';
            });
            if ((showFileInput && selectedFiles.length === 0) || missingRequiredField) {
                setValidationMessage(text.validationMissing);
                setRunState('error');
                return;
            }
            const oversizedFile = selectedFiles.find((file) => file.size > maxSkillAppStagingBytes);
            if (oversizedFile) {
                setValidationMessage(text.fileTooLarge);
                setRunState('error');
                return;
            }
            const skillID = appRunnableSkillID(app, activeRunDependencyPlanRef.current || runtimeDependencyPlan || runtimeInstallEvidencePlan || null);
            if (skillID) {
                setRunState('running');
                setCurrentRunContext({ inputSummary: toolInputSummary, outputMode });
                try {
                    const fieldPayload = buildSkillFieldPayload(skillFields, fieldValues);
                    const primaryFile = selectedFiles[0] || null;
                    const fileText = primaryFile ? await readSmallFileText(primaryFile) : '';
                    const stagedFiles = await Promise.all(selectedFiles.map((file) => stageSkillAppInputFile(file)));
                    const filePayloads = stagedFiles.map((stagedFile, index) => stagedFile || buildSkillFilePayload(selectedFiles[index])).filter(Boolean);
                    const filePayload = filePayloads[0] || buildSkillFilePayload(primaryFile);
                    const stagedFilePath = stagedFiles[0]?.staged_path || '';
                    const stagedFilePaths = stagedFiles.map((file) => file?.staged_path || '').filter(Boolean);

                    // Build runArgs: use paramMapping if declared, otherwise fallback to legacy full-payload mode
                    const paramMapping = app.manifest?.skill?.paramMapping;
                    let skillRunArgs: Record<string, any>;
                    if (paramMapping && Object.keys(paramMapping).length > 0) {
                        // New mode: only pass skill-relevant parameters based on paramMapping
                        skillRunArgs = buildSkillArgsFromMapping(paramMapping, {
                            filePath: stagedFilePath,
                            filePaths: stagedFilePaths,
                            params: toolParams.trim(),
                            fieldValues,
                            fileName,
                        });
                        // Inject staged file refs for backend cleanup (these are control keys, filtered by isRunControlKey)
                        if (stagedFilePaths.length > 0) {
                            skillRunArgs._staged_cleanup_paths = stagedFilePaths;
                        }
                        console.log(`[app-run] using paramMapping for skill=${skillID}, keys=${Object.keys(skillRunArgs).join(',')}`);
                    } else {
                        // Legacy mode: pass everything (backward compatible with old apps)
                        skillRunArgs = {
                            _maclaw_app: true,
                            app_id: app.id,
                            app_name: app.name,
                            app_kind: app.kind,
                            input_mode: inputMode,
                            output_mode: outputMode,
                            params: toolParams.trim(),
                            fields: fieldPayload,
                            file: filePayload,
                            files: filePayloads,
                            file_path: stagedFilePath,
                            file_paths: stagedFilePaths,
                            input_file_path: stagedFilePath,
                            local_file_path: stagedFilePath,
                            uploaded_file_path: stagedFilePath,
                            file_name: fileName,
                            file_text: fileText,
                            prompt: buildToolAppPrompt(app, toolParams.trim(), fieldPayload, outputMode, fileName),
                        };
                        console.log(`[app-run] using legacy full-payload for skill=${skillID} (no paramMapping defined)`);
                    }

                    const runID = await RunNLSkillAsync(skillID, skillRunArgs);
                    const nextRunID = String(runID || '').trim();
                    if (!nextRunID) throw new Error(text.skillRunFailed);
                    setRunID(nextRunID);
                    rememberActiveRun('tool_skill', nextRunID, toolInputSummary, outputMode);
                    setValidationMessage('');
                    setRuntimeBusinessError(null);
                    setRunState('running');
                    return;
                } catch (error: any) {
                    const message = setRuntimeError(error, text.validationMissing);
					recordRunHistory({
						runID: `failed-${Date.now().toString(36)}`,
                        status: 'error',
                        outputMode,
                        inputSummary: toolInputSummary,
                        message,
                    });
                    return;
                }
            }
        }
        if (isApproval) {
            const now = new Date().toISOString();
            const fallbackID = `appr-${Date.now().toString(36)}`;
            const inputSummary = `${businessEntity || app.category} ·  ${businessAction || 'create'}`;
            if (!workflowSkillID) {
                setValidationMessage(text.missingRequiredDependency);
                setRunState('error');
                return;
            }
            setRunState('running');
            setCurrentRunContext({ inputSummary, outputMode: 'approval' });
            const contractInputs = approvalWorkflowContractInputPayload(app, {
                instanceID: fallbackID,
                inputSummary,
                businessEntity: businessEntity || app.category,
                businessAction: businessAction || 'create',
                businessNote,
                submittedAt: now,
                lang,
            });
            const workflowRunArgs = {
                _maclaw_app: true,
                app_id: canonicalAppManifestID(app),
                app_name: app.name,
                app_kind: app.kind,
                ...contractInputs,
                approval_instance_id: fallbackID,
                approval_event: approvalBinding?.event || '',
                approval_workflow_id: approvalBinding?.event || workflowSkillID,
                approval_object_role: approvalObjectRole,
                object_role: approvalObjectRole,
                submitted_by: isZh(lang) ? '\u5f53\u524d\u7528\u6237' : 'Current user',
                current_assignee: isZh(lang) ? '\u5ba1\u6279\u4eba' : 'Approver',
                current_assignee_type: 'user',
                from_status: 'submitted',
                to_status: 'approval_pending',
                approval_workflow_skill_id: workflowSkillID,
                approval_workflow_version: workflowVersion,
                workflow_skill_id: workflowSkillID,
                workflow_version: workflowVersion,
                current_node: workflowMapping?.approvalNode || '',
                workflow_mapping: workflowMapping,
                workflow_status_mapping: workflowMapping?.statusMapping,
                business_entity: businessEntity || app.category,
                business_action: businessAction || 'create',
                business_note: businessNote,
                dataset_id: approvalDatasetID,
                blueprint_id: approvalBlueprintID,
                datasrv_domain: app.manifest?.datasrv?.domain || '',
                preferred_action: app.manifest?.datasrv?.preferredAction || '',
                preferred_view: app.manifest?.datasrv?.preferredView || '',
                prompt: `Start MaClaw approval workflow: ${app.name} · ${inputSummary}`,
            };
            const fallbackPayload: BackendApprovalInstance = {
                instance_id: fallbackID,
                app_id: canonicalAppManifestID(app),
                app_name: app.name,
                workflow_skill_id: workflowSkillID,
                approval_workflow_id: approvalBinding?.event || workflowSkillID,
                workflow_version: workflowVersion || undefined,
                approval_event: approvalBinding?.event || undefined,
                approval_object_role: approvalObjectRole || undefined,
                object_role: approvalObjectRole || undefined,
                dataset_id: approvalDatasetID || undefined,
                blueprint_id: approvalBlueprintID || undefined,
                title: `${businessEntity || app.category} · ${businessAction || 'create'}`,
                lane: 'my_requests',
                status: 'pending',
                current_node: workflowMapping?.approvalNode || (isZh(lang) ? '经理审批' : 'Manager approval'),
                current_node_ids: [workflowMapping?.approvalNode || workflowSkillID].filter(Boolean) as string[],
                owner: isZh(lang) ? '当前用户' : 'Current user',
                applicant: isZh(lang) ? '当前用户' : 'Current user',
                approver: String(approvalObjectRole || '').trim() || (isZh(lang) ? '\u5ba1\u6279\u4eba' : 'Approver'),
                submitted_by: isZh(lang) ? '当前用户' : 'Current user',
                current_assignee: isZh(lang) ? '审批人' : 'Approver',
                current_assignee_type: 'user',
                result: isZh(lang) ? '待审批' : 'Pending approval',
                business_status: workflowMapping?.statusMapping?.pending || 'approval_pending',
                result_status: 'pending',
                from_status: 'submitted',
                to_status: workflowMapping?.statusMapping?.pending || 'approval_pending',
                record_id: fallbackID,
                business_entity: businessEntity || app.category,
                business_action: businessAction || 'create',
                business_note: businessNote,
                result_payload: {
                    text: businessNote || (isZh(lang) ? '待审批' : 'Pending approval'),
                    business_record: { id: fallbackID, status: workflowMapping?.statusMapping?.pending || 'approval_pending' },
                    business_payload: contractInputs.business_payload,
                    form_data: contractInputs,
                },
                created_at: now,
                updated_at: now,
                detail_url: '',
                events: [{
                    at: now,
                    actor: isZh(lang) ? '当前用户' : 'Current user',
                    node: workflowMapping?.submitNode,
                    action: isZh(lang) ? '已提交' : 'Submitted',
                    note: businessNote,
                }, {
                    at: now,
                    node: workflowMapping?.approvalNode || workflowSkillID,
                    actor: 'workflow',
                    action: 'workflow_started',
                    message: workflowSkillID,
                }],
            };
            approvalRunContextRef.current = { instance: fallbackPayload };
            try {
                const startApproval = typeof OpenMaclawAppApprovalWorkspace === 'function'
                    ? OpenMaclawAppApprovalWorkspace
                    : StartMaclawAppApprovalWorkflow;
                const started = await startApproval({
                    app_id: canonicalAppManifestID(app),
                    app_name: app.name,
                    dataset_id: approvalDatasetID || undefined,
                    object_role: approvalObjectRole || undefined,
                    blueprint_id: approvalBlueprintID || undefined,
                    record_id: fallbackID,
                    title: fallbackPayload.title,
                    applicant: fallbackPayload.applicant,
                    owner: fallbackPayload.owner,
                    approver: fallbackPayload.approver,
                    current_assignee: fallbackPayload.current_assignee,
                    current_assignee_type: fallbackPayload.current_assignee_type,
                    approval_event: approvalBinding?.event || undefined,
                    workflow_skill_id: workflowSkillID,
                    workflow_version: workflowVersion || undefined,
                    current_node: workflowMapping?.approvalNode || undefined,
                    current_node_ids: fallbackPayload.current_node_ids,
                    business_status: fallbackPayload.business_status,
                    result_status: fallbackPayload.result_status,
                    from_status: fallbackPayload.from_status,
                    to_status: fallbackPayload.to_status,
                    business_entity: fallbackPayload.business_entity,
                    business_action: fallbackPayload.business_action,
                    business_note: fallbackPayload.business_note,
                    form_data: contractInputs,
                    business_payload: contractInputs.business_payload,
                    result_payload: fallbackPayload.result_payload,
                    run_workflow_skill: true,
                    workflow_run_args: workflowRunArgs,
                }) as { instance?: BackendApprovalInstance; approval_id?: string; sync?: unknown; workflow_run?: { ran?: boolean; workflow_skill_id?: string; instance?: BackendApprovalInstance; progress_instances?: BackendApprovalInstance[]; progressInstances?: BackendApprovalInstance[] } } | undefined;
                const workflowRunInstance = started?.workflow_run?.instance;
                const workflowRunProgressInstances = (Array.isArray(started?.workflow_run?.progress_instances) ? started?.workflow_run?.progress_instances : Array.isArray(started?.workflow_run?.progressInstances) ? started?.workflow_run?.progressInstances : []) as BackendApprovalInstance[];
                const startedInstance = workflowRunInstance || started?.instance || workflowRunProgressInstances[workflowRunProgressInstances.length - 1] || fallbackPayload;
                const workflowRunID = String(
                    startedInstance.workflow_decision_id
                    || startedInstance.workflowDecisionID
                    || startedInstance.workflowDecisionId
                    || started?.approval_id
                    || startedInstance.approval_id
                    || startedInstance.record_approval_id
                    || fallbackID,
                ).trim();
                const savedInstance: BackendApprovalInstance = {
                    ...fallbackPayload,
                    ...startedInstance,
                    workflow_decision_id: workflowRunID,
                    detail_url: startedInstance.detail_url || fallbackPayload.detail_url || (workflowRunID ? `approval-run://${workflowRunID}` : ''),
                    approval_id: startedInstance.approval_id || started?.approval_id || fallbackPayload.approval_id,
                    record_approval_id: startedInstance.record_approval_id || startedInstance.approval_id || started?.approval_id || fallbackPayload.record_approval_id,
                    events: startedInstance.events || fallbackPayload.events,
                };
                approvalRunContextRef.current = { instance: savedInstance };
                const progressViews = workflowRunProgressInstances
                    .map((item) => backendApprovalInstanceToView({ ...fallbackPayload, ...item }, lang))
                    .filter(Boolean) as ApprovalInstanceView[];
                if (progressViews.length > 0) {
                    setApprovalInstances((current) => mergeApprovalInstanceViews(current, progressViews));
                }
                const view = backendApprovalInstanceToView(savedInstance, lang);
                if (view) {
                    setApprovalInstances((current) => [view, ...current.filter((item) => item.id !== view.id)].slice(0, 50));
                }
                const lifecycleStatus = String(savedInstance.status || savedInstance.result_status || savedInstance.resultStatus || '').trim().toLowerCase();
                const terminalRunState = runStateFromApprovalLifecycleStatus(lifecycleStatus);
                const stillRunning = terminalRunState === 'running';
                const verifiedAt = new Date().toISOString();
                const progressEvidence = workflowRunProgressInstances
                    .map((item) => appRunApprovalInstanceEvidenceFromBackend({ ...fallbackPayload, ...item }, verifiedAt))
                    .filter(Boolean) as AppRunApprovalInstanceEvidence[];
                const approvalInstanceEvidence = appRunApprovalInstanceEvidenceFromBackend(savedInstance, verifiedAt);
                if (approvalInstanceEvidence && progressEvidence.length > 0) {
                    approvalInstanceEvidence.progressInstances = progressEvidence;
                }
                const dependencyVerificationPlan = activeRunDependencyPlanRef.current || runtimeDependencyPlan;
                const dependencyVerification = appRunDependencyVerificationEvidence(app, dependencyVerificationPlan, verifiedAt);
                const resultPayload = savedInstance.result_payload || savedInstance.resultPayload || {};
                const outputs = normalizeApprovalOutputs(savedInstance.outputs || []);
                const artifacts = savedInstance.artifacts || [];
                const resultContract = appResultContractForManifest(app);
                const primaryResult = appRunPrimaryResultFromPayload(resultContract, resultPayload, outputs);
                if (!stillRunning) {
                    recordRunHistory({
                        runID: workflowRunID || `approval-${Date.now().toString(36)}`,
                        status: terminalRunState === 'cancelled' ? 'cancelled' : terminalRunState === 'error' ? 'error' : 'done',
                        definitionHash: appDefinitionFingerprint(app),
                        outputMode: 'approval',
                        inputSummary,
                        message: primaryResult.slice(0, 180) || savedInstance.result || text.runCompleted,
                        artifacts: terminalRunState === 'done' ? artifacts : undefined,
                        resultPayload,
                        outputs,
                        dependencyVerification,
                        approvalInstance: approvalInstanceEvidence,
                    });
                }
                setRunID(workflowRunID || fallbackID);
                if (stillRunning) {
                    rememberActiveRun('approval_workflow', workflowRunID || fallbackID, inputSummary, 'approval', savedInstance);
                } else {
                    forgetActiveRun(workflowRunID || fallbackID);
                }
                setRunState(terminalRunState);
                setValidationMessage(terminalRunState === 'error' || terminalRunState === 'cancelled'
                    ? String(savedInstance.result || savedInstance.result_status || savedInstance.resultStatus || '').trim()
                        || (terminalRunState === 'cancelled' ? text.skillRunCancelled : text.skillRunFailed)
                    : '');
                setRuntimeBusinessError(null);
            } catch (error: any) {
                const message = setRuntimeError(error, text.skillRunFailed);
                await recordApprovalRuntimeFailure(fallbackPayload, message, inputSummary, 'failed-' + Date.now().toString(36));
                return;
            }
            return;
        }
        if (isBusiness) {
            const inputSummary = `${businessEntity || app.category} ·  ${businessActionRole || businessAction || 'execute'}`;
            const businessPayload = {
                _maclaw_app: true,
                app_id: app.id,
                app_name: app.name,
                app_kind: app.kind,
                app_skill_id: businessSkillID,
                business_entity: businessEntity || app.category,
                business_action: businessAction || 'create',
                business_note: businessNote,
                object_role: businessObjectRole,
                action_role: businessActionRole,
                datasrv_domain: app.manifest?.datasrv?.domain || '',
                preferred_action: app.manifest?.datasrv?.preferredAction || '',
                preferred_view: app.manifest?.datasrv?.preferredView || '',
                preferred_report: app.manifest?.datasrv?.preferredReport || '',
                preferred_dashboard: app.manifest?.datasrv?.preferredDashboard || '',
                prompt: `Run MaClaw business app: ${app.name} ·  ${inputSummary}`,
            };
            if (businessSkillID && app.source !== 'datasrv') {
                setRunState('running');
                setCurrentRunContext({ inputSummary, outputMode: 'business' });
                try {
                    const runID = await RunNLSkillAsync(businessSkillID, businessPayload);
                    const nextRunID = String(runID || '').trim();
                    if (!nextRunID) throw new Error(text.skillRunFailed);
                    setRunID(nextRunID);
                    rememberActiveRun('business_skill', nextRunID, inputSummary, 'business');
                    setValidationMessage('');
                    setRuntimeBusinessError(null);
                    setRunState('running');
                    return;
                } catch (error: any) {
					const message = setRuntimeError(error, text.skillRunFailed);
					recordRunHistory({ runID: `failed-${Date.now().toString(36)}`, status: 'error', outputMode: 'business', inputSummary, message });
					return;
                }
            }
            setRunState('running');
            setBusinessResult(null);
            setCurrentRunContext({ inputSummary, outputMode: 'business' });
            try {
                // Open right-pane AppView workspace (DataSrv → table/result_browser) while
                // still returning the same business operation result package for AppsPage UI.
                const openWorkspace = typeof OpenMaclawAppBusinessWorkspace === 'function'
                    ? OpenMaclawAppBusinessWorkspace
                    : ExecuteMaclawAppBusinessOperation;
                const result = await openWorkspace({
                    app_id: app.id,
                    app_name: app.name,
                    dataset_id: app.manifest?.datasrv?.datasetID || '',
                    object_role: businessObjectRole,
                    blueprint_id: app.manifest?.datasrv?.blueprintID || '',
                    business_entity: businessEntity || app.category,
                    business_action: businessAction || 'create',
                    business_note: businessNote,
                    preferred_action: app.manifest?.datasrv?.preferredAction || '',
                    preferred_view: app.manifest?.datasrv?.preferredView || '',
                    preferred_report: app.manifest?.datasrv?.preferredReport || '',
                    preferred_dashboard: app.manifest?.datasrv?.preferredDashboard || '',
                    data: { note: businessNote, action_role: businessActionRole },
                    limit: 50,
                });
				const businessOperationResult = buildBusinessOperationResult(result as Record<string, unknown>, businessActionRole, text);
				setBusinessResult(businessOperationResult);
				setValidationMessage('');
				setRuntimeBusinessError(null);
				setRunState('done');
					recordRunHistory({ runID: `business-${Date.now().toString(36)}`, status: 'done', outputMode: 'business', inputSummary, message: text.runCompleted + ': ' + businessOperationResult.mode + ' ·  ' + businessOperationResult.status, ...businessOperationRunEvidence(businessOperationResult) });
				return;
			} catch (error: any) {
				const message = setRuntimeError(error, text.skillRunFailed);
				recordRunHistory({ runID: `failed-${Date.now().toString(36)}`, status: 'error', outputMode: 'business', inputSummary, message });
				return;
            }
        }
        setValidationMessage('');
        setRunState('done');
    };
    const installRuntimeDependenciesAndRun = async () => {
        if (!app || dependencyRepairState === 'installing') return;
        setDependencyRepairState('installing');
        setValidationMessage(text.installingDependencies);
        try {
            const dependencyInstallPlan = await InstallMaclawAppDependencies(JSON.stringify(appToManifest(app)));
            setRuntimeDependencyPlan(dependencyInstallPlan || null);
            activeRunDependencyPlanRef.current = dependencyInstallPlan || null;
            if (runtimeInstallPlanBlocked(dependencyInstallPlan, app)) {
                setValidationMessage(runtimeInstallPlanBlockMessage(app, dependencyInstallPlan, text, lang));
                setRunState('error');
                setDependencyRepairState('idle');
                return;
            }
            setRuntimeDependencyCheckState('ready');
            setDependencyRepairState('idle');
            await runApp({ skipDependencyCheck: true });
        } catch (error: any) {
            setRuntimeDependencyPlan(null);
            activeRunDependencyPlanRef.current = null;
            const depNames = app ? appDependencyEvidence(app).map((d) => d.id).filter(Boolean) : [];
            const detail = error?.message || '';
            const depHint = depNames.length > 0 ? (isZh(lang) ? `\uff08\u9700\u8981\uff1a${depNames.join(', ')}\uff09` : ` (requires: ${depNames.join(', ')})`) : '';
            setValidationMessage((detail || text.dependencyPlanError) + depHint);
            setRunState('error');
            setDependencyRepairState('idle');
        }
    };
    const continueApprovalInstanceWithSupplement = async (instance: ApprovalInstanceView, supplement: { note: string; reference: string }) => {
        if (!app?.id || !instance?.id || !isApproval) return;
        const note = supplement.note.trim();
        const reference = supplement.reference.trim();
        const recordID = instance.recordID || instance.id;
        const formData = {
            supplement_note: note,
            supplement_reference: reference,
            supplemental_input: note || reference,
        };
        const businessPayload = {
            supplement_note: note,
            supplement_reference: reference,
            approval_id: instance.approvalID,
            continue_from_instance_id: instance.id,
        };
        const resultPayload = {
            ...(instance.resultPayload || {}),
            supplemental_input: {
                form_data: formData,
                business_payload: businessPayload,
            },
        };
        const inputSummary = `${instance.title || businessEntity || app.category} ·  ${text.supplementContinue}`;
        setRunState('running');
        setValidationMessage(text.supplementRunning);
        setRuntimeBusinessError(null);
        setCurrentRunContext({ inputSummary, outputMode: 'approval' });
        try {
            const startApproval = typeof OpenMaclawAppApprovalWorkspace === 'function'
                ? OpenMaclawAppApprovalWorkspace
                : StartMaclawAppApprovalWorkflow;
            const started = await startApproval({
                app_id: canonicalAppManifestID(app),
                app_name: app.name,
                approval_id: instance.approvalID,
                instance_id: instance.id,
                continue_from_instance_id: instance.id,
                dataset_id: instance.datasetID || approvalDatasetID || undefined,
                object_role: instance.objectRole || approvalObjectRole || undefined,
                blueprint_id: instance.blueprintID || approvalBlueprintID || undefined,
                record_id: recordID,
                title: instance.title,
                applicant: instance.applicant || instance.owner,
                owner: instance.owner || instance.applicant,
                approver: instance.approver,
                current_assignee: instance.approver || instance.currentAssignee,
                current_assignee_type: instance.currentAssigneeType || 'user',
                approval_event: instance.approvalEvent || approvalBinding?.event || undefined,
                workflow_skill_id: instance.workflowSkillID || workflowSkillID,
                workflow_version: instance.workflowVersion || workflowVersion || undefined,
                current_node: instance.currentNode || workflowMapping?.approvalNode || undefined,
                current_node_ids: instance.currentNodeIDs && instance.currentNodeIDs.length > 0 ? instance.currentNodeIDs : undefined,
                business_status: 'supplemented',
                result_status: 'pending',
                from_status: instance.businessStatus || instance.status,
                to_status: 'supplemented',
                business_entity: instance.objectRole || businessEntity || app.category,
                business_action: 'supplement',
                business_note: note || text.supplementContinue,
                form_data: formData,
                business_payload: businessPayload,
                result_payload: resultPayload,
                run_workflow_skill: true,
                workflow_run_args: {
                    _maclaw_app: true,
                    app_id: canonicalAppManifestID(app),
                    app_name: app.name,
                    app_kind: app.kind,
                    approval_id: instance.approvalID,
                    approval_instance_id: instance.id,
                    continue_from_instance_id: instance.id,
                    supplemental_input: { form_data: formData, business_payload: businessPayload },
                    workflow_mapping: workflowMapping,
                    workflow_status_mapping: workflowMapping?.statusMapping,
                    workflow_skill_id: instance.workflowSkillID || workflowSkillID,
                    workflow_version: instance.workflowVersion || workflowVersion,
                    current_node: instance.currentNode || workflowMapping?.approvalNode || '',
                    business_entity: instance.objectRole || businessEntity || app.category,
                    business_action: 'supplement',
                    business_note: note || text.supplementContinue,
                    prompt: `Continue MaClaw approval workflow: ${app.name} ·  ${instance.title}`,
                },
            }) as { instance?: BackendApprovalInstance; approval_id?: string; workflow_run?: { ran?: boolean; workflow_skill_id?: string; instance?: BackendApprovalInstance; progress_instances?: BackendApprovalInstance[]; progressInstances?: BackendApprovalInstance[] } } | undefined;
            const workflowRunProgressInstances = (Array.isArray(started?.workflow_run?.progress_instances) ? started?.workflow_run?.progress_instances : Array.isArray(started?.workflow_run?.progressInstances) ? started?.workflow_run?.progressInstances : []) as BackendApprovalInstance[];
            const workflowRunInstance = started?.workflow_run?.instance;
            const startedInstance = workflowRunInstance || started?.instance || workflowRunProgressInstances[workflowRunProgressInstances.length - 1];
            const progressViews = workflowRunProgressInstances.map((item) => backendApprovalInstanceToView(item, lang)).filter(Boolean) as ApprovalInstanceView[];
            if (progressViews.length > 0) {
                setApprovalInstances((current) => mergeApprovalInstanceViews(current, progressViews));
            }
            const finalView = startedInstance ? backendApprovalInstanceToView(startedInstance, lang) : undefined;
            if (finalView) {
                setApprovalInstances((current) => mergeApprovalInstanceViews(current, [finalView]));
            }
            const finalStatus = String(startedInstance?.status || startedInstance?.result_status || startedInstance?.resultStatus || '').trim().toLowerCase().replace('-', '_');
            const terminalRunState = runStateFromApprovalLifecycleStatus(finalStatus);
            const stillRunning = terminalRunState === 'running';
            const supplementRunID = String(startedInstance?.workflow_decision_id || startedInstance?.workflowDecisionID || started?.approval_id || instance.approvalID || instance.id).trim();
            setRunID(supplementRunID);
            if (stillRunning && startedInstance) {
                rememberActiveRun('approval_workflow', supplementRunID, inputSummary, 'approval', startedInstance);
            } else {
                forgetActiveRun(supplementRunID);
            }
            setRunState(terminalRunState);
            setValidationMessage(terminalRunState === 'error' || terminalRunState === 'cancelled'
                ? String(startedInstance?.result || startedInstance?.result_status || startedInstance?.resultStatus || '').trim()
                    || (terminalRunState === 'cancelled' ? text.skillRunCancelled : text.skillRunFailed)
                : '');
            if (startedInstance) {
                const verifiedAt = new Date().toISOString();
                const approvalInstance = appRunApprovalInstanceEvidenceFromBackend(startedInstance, verifiedAt);
                const startedResultPayload = (startedInstance.result_payload || startedInstance.resultPayload || {}) as Record<string, unknown>;
                const historyResultPayload = { ...resultPayload, ...startedResultPayload };
                if (approvalInstance?.resultPayload && !approvalInstance.resultPayload.supplemental_input) {
                    approvalInstance.resultPayload = { ...approvalInstance.resultPayload, supplemental_input: resultPayload.supplemental_input };
                }
                if (approvalInstance && workflowRunProgressInstances.length > 0) {
                    approvalInstance.progressInstances = workflowRunProgressInstances.map((item) => appRunApprovalInstanceEvidenceFromBackend(item, verifiedAt)).filter(Boolean) as AppRunApprovalInstanceEvidence[];
                }
                recordRunHistory({
                    runID: supplementRunID || `supplement-${Date.now().toString(36)}`,
                    status: terminalRunState === 'cancelled' ? 'cancelled' : terminalRunState === 'error' ? 'error' : 'done',
                    outputMode: 'approval',
                    inputSummary,
                    message: String(startedInstance.result || startedInstance.result_status || startedInstance.resultStatus || text.supplementContinue).slice(0, 180),
                    resultPayload: historyResultPayload,
                    outputs: normalizeApprovalOutputs((startedInstance.outputs || []) as any[]),
                    artifacts: terminalRunState === 'done' ? normalizeApprovalArtifacts((startedInstance.artifacts || []) as any[]) : undefined,
                    approvalInstance,
                });
            }
            void loadApprovalInstances('all');
        } catch (error: any) {
            const message = setRuntimeError(error, text.skillRunFailed);
            await recordApprovalRuntimeFailure({
                instance_id: instance.id,
                app_id: canonicalAppManifestID(app),
                app_name: app.name,
                approval_id: instance.approvalID,
                record_approval_id: instance.approvalID,
                record_id: recordID,
                title: instance.title,
                lane: 'my_requests',
                status: 'requires_input',
                current_node: instance.currentNode,
                current_node_ids: instance.currentNodeIDs,
                workflow_node_ids: instance.currentNodeIDs,
                workflow_skill_id: instance.workflowSkillID || workflowSkillID,
                workflow_version: instance.workflowVersion || workflowVersion,
                approval_event: instance.approvalEvent || approvalBinding?.event,
                approval_workflow_id: instance.approvalWorkflowID || instance.workflowSkillID || workflowSkillID,
                object_role: instance.objectRole || approvalObjectRole,
                dataset_id: instance.datasetID || approvalDatasetID,
                blueprint_id: instance.blueprintID || approvalBlueprintID,
                owner: instance.owner || instance.applicant,
                applicant: instance.applicant || instance.owner,
                approver: instance.approver,
                current_assignee: instance.currentAssignee || instance.approver,
                current_assignee_type: instance.currentAssigneeType || 'user',
                result: instance.result,
                business_status: instance.businessStatus,
                result_status: instance.resultStatus,
                from_status: instance.businessStatus || instance.status,
                result_payload: resultPayload,
                outputs: instance.outputs,
                artifacts: instance.artifacts,
                events: instance.events,
            }, message, inputSummary, 'failed-' + Date.now().toString(36));
        }
    };
    const updateApprovalInstanceDecision = async (instance: ApprovalInstanceView, decision: 'approved' | 'rejected' | 'attention') => {
        if (!app?.id || !instance?.id) return;
        const now = new Date().toISOString();
        const zh = isZh(lang);
        const statusText = decision === 'approved'
            ? (zh ? '\u5df2\u901a\u8fc7' : 'Approved')
            : decision === 'rejected'
                ? (zh ? '\u5df2\u9a73\u56de' : 'Rejected')
                : (zh ? '\u9700\u5173\u6ce8' : 'Needs attention');
        const nextLane: ApprovalInstanceView['lane'] = decision === 'attention' ? 'attention' : 'handled';
        const nextNode = decision === 'attention' ? instance.currentNode : (zh ? '\u5df2\u5b8c\u6210' : 'Completed');
        const workflowDecisionID = `decision-${Date.now().toString(36)}`;
        const resultPayload = approvalDecisionResultPayload(instance, decision, {
            resultText: statusText,
            currentNode: nextNode,
            workflowDecisionID,
            currentNodeIDs: [nextNode],
            fromStatus: instance.businessStatus || instance.status,
        });
        const outputs = approvalOutputsOrDefault(
            instance.outputs,
            decision,
            statusText,
            decision,
            decision,
            resultPayload,
            zh ? '\u5ba1\u6279\u7ed3\u679c' : 'Approval result',
        );
        const payload: BackendApprovalInstance = {
            instance_id: instance.id,
            app_id: canonicalAppManifestID(app) || instance.appID || app.id,
            app_name: app.name,
            workflow_skill_id: instance.workflowSkillID || workflowSkillID,
            approval_workflow_id: instance.approvalWorkflowID || approvalBinding?.event || workflowSkillID || undefined,
            workflow_version: instance.workflowVersion || workflowVersion || undefined,
            workflow_decision_id: workflowDecisionID,
            approval_id: instance.approvalID,
            record_approval_id: instance.approvalID,
            approval_event: instance.approvalEvent || approvalBinding?.event || undefined,
            approval_object_role: instance.objectRole || approvalObjectRole || undefined,
            object_role: instance.objectRole || approvalObjectRole || undefined,
            dataset_id: instance.datasetID || approvalDatasetID || undefined,
            blueprint_id: instance.blueprintID || approvalBlueprintID || undefined,
            title: instance.title,
            lane: nextLane,
            status: decision,
            current_node: nextNode,
            owner: instance.owner,
            applicant: instance.owner,
            approver: instance.approver,
            current_assignee: decision === 'attention' ? (instance.currentAssignee || instance.approver) : 'completed',
            current_assignee_type: decision === 'attention' ? (instance.currentAssigneeType || 'user') : 'system',
            result: statusText,
            business_status: decision,
            result_status: decision,
            from_status: instance.businessStatus || instance.status,
            to_status: decision,
            record_id: instance.recordID,
            detail_url: instance.detailURL,
            result_payload: resultPayload,
            outputs,
            artifacts: instance.artifacts,
            business_entity: businessEntity || app.category,
            business_action: businessAction || 'approve',
            business_note: businessNote,
            updated_at: now,
            events: [
                ...(instance.events || []),
                {
                    at: now,
                    node: nextNode,
                    actor: instance.approver || (zh ? '\u5ba1\u6279\u4eba' : 'Approver'),
                    decision,
                    message: statusText,
                    action: decision,
                    note: businessNote,
                },
            ],
        };
        const fallback = backendApprovalInstanceToView(payload, lang);
        if (fallback) {
            setApprovalInstances((current) => [fallback, ...current.filter((item) => item.id !== fallback.id)].slice(0, 50));
        }
        try {
            const saved = await RecordMaclawAppApprovalInstance(payload) as BackendApprovalInstance;
            const savedInstance = saved || payload;
            const view = backendApprovalInstanceToView(savedInstance, lang);
            if (view) {
                setApprovalInstances((current) => [view, ...current.filter((item) => item.id !== view.id)].slice(0, 50));
            }
			const syncedInstance = await syncApprovalInstanceToDataSrvWithEvents(app, savedInstance, lang);
				const syncedView = backendApprovalInstanceToView(syncedInstance, lang);
				if (syncedView) {
					setApprovalInstances((current) => [syncedView, ...current.filter((item) => item.id !== syncedView.id)].slice(0, 50));
				}
                const evidenceEntry = approvalDecisionRunHistoryEntry(app, syncedInstance, statusText);
                if (evidenceEntry) {
                    setRunHistory((current) => {
                        const next = [evidenceEntry, ...current.filter((item) => item.runID !== evidenceEntry.runID)].slice(0, 8);
                        saveAppRunHistory(app.id, next);
                        return next;
                    });
                }
			} catch (error: any) {
				const businessError = structuredBusinessErrorFromUnknown(error);
				setRuntimeBusinessError(businessError);
				setValidationMessage(structuredBusinessErrorMessage(businessError, error?.message || String(error || text.validationMissing)));
			}
	};
    const cancelRun = async () => {
        if (!runID) return;
        try {
            await CancelNLSkillRun(runID);
        } catch {
            // Keep the UI responsive; the next poll may still report cancellation.
        }
        setValidationMessage(text.skillRunCancelled);
        setRunState('cancelled');
        forgetActiveRun(runID);
        recordRunHistory({
            runID,
            status: 'cancelled',
            outputMode: currentRunContext.outputMode || outputMode,
            inputSummary: currentRunContext.inputSummary || toolInputSummary,
            message: text.skillRunCancelled,
        });
        await finalizeApprovalRunFromStatus({ run_id: runID, status: 'cancelled', summary: { last_error_snippet: text.skillRunCancelled } }, 'cancelled');
    };
    const runtimeAppID = app ? canonicalAppManifestID(app) : '';
    const runtimeInstallEvidencePlan = appInstallEvidenceDependencyVerificationPlan(app);
    const runtimeDependencyPlanHasEvidence = !!runtimeDependencyPlan && (
        (runtimeDependencyPlan.dependencies?.length || 0) > 0 ||
        (runtimeDependencyPlan.workflow_contract_issues?.length || 0) > 0 ||
        (runtimeDependencyPlan.governance_review_issues?.length || 0) > 0 ||
        !!runtimeDependencyPlan.has_blocking_dependency ||
        !!runtimeDependencyPlan.has_missing_required ||
        !!runtimeDependencyPlan.has_workflow_contract_issue ||
        !!runtimeDependencyPlan.has_governance_review_issue
    );
    const runtimeVisibleDependencyPlan = runtimeDependencyPlanHasEvidence ? runtimeDependencyPlan : runtimeInstallEvidencePlan || runtimeDependencyPlan || null;
    const runtimeDependencyDetails = runtimeVisibleDependencyPlan ? backendDependenciesForApp(runtimeVisibleDependencyPlan, runtimeAppID) : [];
    const visibleRuntimeDependencyDetails = runtimeDependencyDetails.length > 0 ? runtimeDependencyDetails : runtimeVisibleDependencyPlan?.dependencies || [];
    const runtimeDependencyBlocked = !!app && runtimeInstallPlanBlocked(runtimeVisibleDependencyPlan, app);
    const runtimeDependencyNeedsRepair = !!app && hasMissingRequiredBackendDependency(runtimeVisibleDependencyPlan, appDependencyVerificationAppIDs(app));
    const runtimeDependencyChecking = runtimeDependencyCheckState === 'checking' || dependencyRepairState === 'installing';
    const runtimeDependencyReady = runtimeDependencyCheckState === 'ready' && visibleRuntimeDependencyDetails.length > 0 && !runtimeDependencyBlocked;
    const runtimeEvidenceReady = runtimeDependencyCheckState === 'idle' && !!runtimeInstallEvidencePlan && visibleRuntimeDependencyDetails.length > 0 && !runtimeDependencyBlocked;
    const runtimeDependencyMessage = app ? runtimeInstallPlanBlockMessage(app, runtimeVisibleDependencyPlan, text, lang) : backendDependencyUnavailableMessage(app, runtimeVisibleDependencyPlan, text, lang);
    // Only show dependency details inline when there's a problem (blocked/error/installing).
    // When everything is ready (runtimeDependencyReady), hide details by default; users can expand via detail controls.
    const showRuntimeDependencyDetails = visibleRuntimeDependencyDetails.length > 0 && (runState === 'error' || dependencyRepairState === 'installing' || runtimeDependencyBlocked);
    const canInstallRuntimeDependencies = !!app && appNeedsRuntimeDependencyCheck(app) && (runtimeDependencyNeedsRepair || dependencyRepairState === 'installing' || validationMessage === text.missingRequiredDependency);
    const runtimeDependencyPanelState = dependencyRepairState === 'installing'
        ? 'repairing'
        : runtimeDependencyCheckState === 'checking'
            ? 'loading'
            : runtimeDependencyCheckState === 'error' && !runtimeVisibleDependencyPlan
                ? 'error'
                : runtimeVisibleDependencyPlan
                    ? 'ready'
                    : 'idle';
    const showRunButton = runState === 'idle' && dependencyRepairState !== 'installing';
    const runtimeStatusMessage = runState === 'done'
        ? text.runCompleted
        : runState === 'running'
            ? skillRunProgressMessage(skillRunStatus, text.skillRunRunning, runID)
            : runState === 'error'
                ? validationMessage
                : runState === 'cancelled'
                    ? text.skillRunCancelled
                    : runtimeDependencyChecking
                        ? text.dependencyPlanLoading
                        : runtimeDependencyBlocked
                            ? runtimeDependencyMessage
                            : runtimeDependencyReady || runtimeEvidenceReady
                                ? text.dependencyReady
                                : text.readyOutput;
    const runtimeStatusState = runState !== 'idle'
        ? runState
        : runtimeDependencyChecking
            ? 'running'
            : runtimeDependencyBlocked
                ? 'error'
                : runtimeDependencyReady
                    ? 'done'
                    : runtimeEvidenceReady
                        ? 'done'
                        : 'idle';
    const runtimeWorkflowContract = workflowContractForApp(app);
    const runtimeWorkflowContractIssue = workflowContractIssueForApp(runtimeVisibleDependencyPlan, app);
    const runtimeWorkflowContractState = workflowContractStatus(runtimeWorkflowContract, runtimeVisibleDependencyPlan, app);
    const runtimeLayout = runtimeWorkspaceLayoutForApp(app);
    const runtimeOrder = runtimeWorkspaceOrder(runtimeLayout);
    const isRuntimeRoleVisible = (role: string) => runtimeLayout.regions.some((region) => region.role === role && region.visible !== false);
    const regionPlacementForRole = (role: string, fallback: string) => runtimeLayout.regions.find((region) => region.visible !== false && region.role === role)?.placement || fallback;
    const inputRegion = regionPlacementForRole('input', runtimeLayout.primaryRegion);
    const outputRegion = regionPlacementForRole('output', runtimeLayout.outputRegion);
    const secondaryRegion = runtimeLayout.primaryRegion === 'right' ? 'left' : 'right';
    const centerRegion = runtimeLayout.primaryRegion === 'center' ? secondaryRegion : 'center';
    const workspaceRegion = isApproval ? regionPlacementForRole('instance_list', centerRegion) : regionPlacementForRole('record_list', centerRegion);
    const statusRegion = outputRegion === 'bottom' ? centerRegion : outputRegion;
    const showInputRegion = isRuntimeRoleVisible('input') || isAutomation;
    const showWorkspaceRegion = isApproval ? isRuntimeRoleVisible('instance_list') : isBusiness ? isRuntimeRoleVisible('record_list') : isRuntimeRoleVisible('preview');
    const showStatusRegion = isRuntimeRoleVisible('detail') || isRuntimeRoleVisible('parameters') || isRuntimeRoleVisible('preview') || !!app.installEvidence || !!runtimeVisibleDependencyPlan || !!runtimeBusinessError;
    const showOutputRegion = isRuntimeRoleVisible('output');
    const outputRoleRegions = runtimeLayout.regions.filter((region) => region.role === 'output');
    const outputExplicitlyHidden = outputRoleRegions.length > 0 && outputRoleRegions.every((region) => region.visible === false);
    const showOutputShell = !outputExplicitlyHidden && (showOutputRegion || !!runtimeLayout.outputRegion);
    const showCompletedOutputRegion = showOutputRegion && runState === 'done';
    const inputLocked = runState === 'running';

    return (
        <>
            <div className="apps-detail__header">
                <div>
                    <h2 className="apps-detail__title">{app.name}</h2>
                    <p className="apps-detail__subtitle">{app.description}</p>
                    <InstallVersionSnapshot snapshot={app.versionSnapshot} text={text} />
                    {isApproval && <WorkflowContractSummary contract={runtimeWorkflowContract} state={runtimeWorkflowContractState} issue={runtimeWorkflowContractIssue} text={text} />}
                    {app.installEvidence && <InstallRecordEvidenceSnapshot record={app.installEvidence} text={text} />}
                </div>
                <div className="apps-detail__header-actions">
                    <button
                        className="apps-about-button"
                        type="button"
                        title={text.aboutDialogTitle}
                        aria-label={`${text.aboutDialogTitle}: ${app.name}`}
                        onClick={() => setShowAboutDialog(true)}
                    >
                        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="9" /><line x1="12" y1="16" x2="12" y2="12" /><line x1="12" y1="8" x2="12.01" y2="8" /></svg>
                    </button>
                </div>
            </div>
            {showAboutDialog && (
                <AppAboutDialog
                    app={app}
                    text={text}
                    onClose={() => setShowAboutDialog(false)}
                />
            )}
            <div className="apps-detail__body elegant-scrollbar">
                <div className={`apps-preview apps-preview--layout-${runtimeLayout.template}`}>
                    <div className="apps-preview__mock apps-runtime-layout" data-template={runtimeLayout.template} data-density={runtimeLayout.density} data-primary-region={runtimeLayout.primaryRegion} data-output-region={runtimeLayout.outputRegion} data-region-count={runtimeLayout.regions.length}>
                        {showInputRegion && <section className="apps-runtime-section apps-runtime-input" data-region={inputRegion} style={{ order: runtimeOrder.input }}>
                            <div className="apps-runtime-section__title">{text.runtimeInput}</div>
                            {isTool ? (
                                <>
                                    {showFileInput && (
                                        <label className="apps-drop-zone">
                                            <input
                                                key={fileInputResetKey}
                                                type="file"
                                                multiple={allowMultipleFiles}
                                                disabled={inputLocked}
                                                onChange={(event) => {
                                                    const files = Array.from(event.currentTarget.files || []);
                                                    const file = files[0] || null;
                                                    setSelectedFiles(allowMultipleFiles ? files : files.slice(0, 1));
                                                    setSelectedFile(file);
                                                    setFileName(allowMultipleFiles && files.length > 1 ? files.map((item) => item.name).join(', ') : file?.name || '');
                                                    markDirty();
                                                }}
                                            />
                                            <span>{selectedFileLabel ? `${text.selectedFile}: ${selectedFileLabel}` : text.upload}</span>
                                            <strong>{text.chooseFile}</strong>
                                        </label>
                                    )}
                                    {showParamInput && (
                                        skillFields.length > 0 ? (
                                            <div className="apps-tool-fields">
                                                {skillFields.map((field) => {
                                                    const value = fieldValues[field.name] ?? field.default ?? (field.type === 'boolean' ? false : '');
                                                    return (
                                                        <div className="apps-form-row" key={field.name}>
                                                            <label>{field.label || field.name}</label>
                                                            {field.type === 'boolean' ? (
                                                                <label className="apps-checkbox-field">
                                                                    <input type="checkbox" checked={!!value} disabled={inputLocked} onChange={(event) => {
                                                                        setFieldValues((current) => ({ ...current, [field.name]: event.target.checked }));
                                                                        markDirty();
                                                                    }} />
                                                                    <span>{isZh(lang) ? '\u542f\u7528' : 'Enabled'}</span>
                                                                </label>
                                                            ) : field.type === 'select' ? (
                                                                <select aria-label={field.label || field.name} value={String(value)} disabled={inputLocked} onChange={(event) => {
                                                                    setFieldValues((current) => ({ ...current, [field.name]: event.target.value }));
                                                                    markDirty();
                                                                }}>
                                                                    {(field.options || []).map((option) => <option key={option} value={option}>{option}</option>)}
                                                                </select>
                                                            ) : (
                                                                <input aria-label={field.label || field.name} value={String(value)} required={field.required} disabled={inputLocked} onChange={(event) => {
                                                                    setFieldValues((current) => ({ ...current, [field.name]: event.target.value }));
                                                                    markDirty();
                                                                }} />
                                                            )}
                                                        </div>
                                                    );
                                                })}
                                            </div>
                                        ) : (
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u53c2\u6570' : 'Parameters'}</label>
                                                <textarea value={toolParams} disabled={inputLocked} onChange={(event) => {
                                                    setToolParams(event.target.value);
                                                    markDirty();
                                                }} placeholder={isZh(lang) ? '\u8f93\u5165\u5904\u7406\u8981\u6c42\u6216\u8868\u5355\u53c2\u6570\u3002' : 'Enter processing instructions or form parameters.'} />
                                            </div>
                                        )
                                    )}
                                    <div className="apps-form-row">
                                        <label>{text.output}</label>
                                        <select value={outputMode} disabled={inputLocked} onChange={(event) => {
                                            setOutputMode(event.target.value);
                                            markDirty();
                                        }}>
                                            {outputModes.map((mode) => <option key={mode} value={mode}>{outputModeLabel(mode)}</option>)}
                                        </select>
                                    </div>
                                </>
                            ) : isAutomation ? (
                                <>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u8fd0\u884c\u6a21\u5f0f' : 'Mode'}</label><select defaultValue="manual" disabled={inputLocked}><option value="manual">{isZh(lang) ? '\u624b\u52a8\u6267\u884c' : 'Manual run'}</option><option value="schedule">{isZh(lang) ? '\u5b9a\u65f6\u6267\u884c' : 'Scheduled'}</option><option value="monitor">{isZh(lang) ? '\u6301\u7eed\u76d1\u63a7' : 'Continuous monitor'}</option></select></div>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u72b6\u6001' : 'Status'}</label><input readOnly value={runState === 'done' ? (isZh(lang) ? '\u8fd0\u884c\u4e2d' : 'Running') : text.readyOutput} /></div>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u5907\u6ce8' : 'Note'}</label><textarea disabled={inputLocked} defaultValue={isZh(lang) ? '\u7531 Agent \u7ef4\u62a4\u957f\u8fd0\u884c\u4efb\u52a1\uff0c\u5e76\u5728\u5e94\u7528 tab \u4e2d\u56de\u62a5\u7ed3\u679c\u3002' : 'Agent maintains the long-running task and reports results in the app tab.'} /></div>
                                </>
                            ) : (
                                <>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u4e1a\u52a1\u5bf9\u8c61' : 'Entity'}</label><input value={businessEntity} disabled={inputLocked} onChange={(event) => {
                                        setBusinessEntity(event.target.value);
                                        markDirty();
                                    }} /></div>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u64cd\u4f5c' : 'Action'}</label><select value={businessAction} disabled={inputLocked} onChange={(event) => {
                                        setBusinessAction(event.target.value);
                                        markDirty();
                                    }}><option value="create">{isZh(lang) ? '\u65b0\u5efa\u8bb0\u5f55' : 'Create record'}</option><option value="query">{isZh(lang) ? '\u67e5\u8be2\u6570\u636e' : 'Query data'}</option><option value="report">{isZh(lang) ? '\u751f\u6210\u62a5\u8868' : 'Generate report'}</option></select></div>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u5907\u6ce8' : 'Note'}</label><textarea value={businessNote} disabled={inputLocked} onChange={(event) => {
                                        setBusinessNote(event.target.value);
                                        markDirty();
                                    }} placeholder={isZh(lang) ? '\u8f93\u5165\u4e1a\u52a1\u610f\u56fe\uff0cAgent \u751f\u6210\u52a8\u6001\u754c\u9762\u5e76\u901a\u8fc7 DataSrv \u6267\u884c\u3002' : 'Enter business intent. Agent renders a dynamic UI and executes through DataSrv.'} /></div>
                                    <div className="apps-capability-strip">
                                        <span>{app.manifest?.datasrv?.domain || 'DataSrv'}</span>
                                        <span>{app.manifest?.datasrv?.preferredAction || businessAction}</span>
                                        <span>{app.manifest?.datasrv?.preferredView || app.manifest?.datasrv?.preferredReport || app.manifest?.datasrv?.preferredDashboard || '-'}</span>
                                    </div>
                                </>
                            )}
                        </section>}
                        {isApproval && showWorkspaceRegion && <ApprovalWorkspace app={app} runState={runState} businessEntity={businessEntity} businessAction={businessAction} businessNote={businessNote} backendInstances={approvalInstances} approvalLoadState={approvalInstancesLoadState} lang={lang} text={text} style={{ order: runtimeOrder.approval }} layoutRegion={workspaceRegion} onRefresh={loadApprovalInstances} onDecision={updateApprovalInstanceDecision} onSupplement={continueApprovalInstanceWithSupplement} />}
                        {isBusiness && showWorkspaceRegion && <BusinessWorkspace app={app} runState={runState} businessEntity={businessEntity} businessAction={businessAction} businessNote={businessNote} lang={lang} style={{ order: runtimeOrder.approval }} layoutRegion={workspaceRegion} />}
                        {showStatusRegion && <section className="apps-runtime-section apps-runtime-status" data-region={statusRegion} style={{ order: runtimeOrder.status }}>
                            <RuntimeStatusSection
                                runState={runState}
                                runtimeStatusState={runtimeStatusState}
                                runtimeStatusMessage={runtimeStatusMessage}
                                runtimeBusinessError={runtimeBusinessError}
                                runtimeDependencyBlocked={runtimeDependencyBlocked}
                                showRuntimeDependencyDetails={showRuntimeDependencyDetails}
                                visibleRuntimeDependencyDetails={visibleRuntimeDependencyDetails}
                                canInstallRuntimeDependencies={canInstallRuntimeDependencies}
                                dependencyRepairState={dependencyRepairState}
                                installRuntimeDependenciesAndRun={installRuntimeDependenciesAndRun}
                                isApproval={isApproval}
                                runtimeWorkflowContractState={runtimeWorkflowContractState}
                                runtimeWorkflowContract={runtimeWorkflowContract}
                                runtimeWorkflowContractIssue={runtimeWorkflowContractIssue}
                                app={app}
                                runtimeVisibleDependencyPlan={runtimeVisibleDependencyPlan}
                                runtimeDependencyPanelState={runtimeDependencyPanelState}
                                runtimeAppID={runtimeAppID}
                                isTool={isTool}
                                skillRunStatus={skillRunStatus}
                                text={text}
                            />
                        </section>}
                        {showInputRegion && <div className="apps-actions apps-runtime-actions" data-region={inputRegion} style={{ order: runtimeOrder.actions }}>
                            <button className="apps-secondary-button" type="button" disabled={runState === 'running'} onClick={() => {
                                setFileName('');
                                setSelectedFile(null);
                                setSelectedFiles([]);
                                setFileInputResetKey((key) => key + 1);
                                setToolParams('');
                                setFieldValues({});
                                setOutputMode(outputModes[0]);
                                setBusinessEntity(app.category);
                                setBusinessAction('create');
                                setBusinessNote('');
                                setRunState('idle');
                                setValidationMessage('');
								setRunID('');
								setSkillRunStatus(null);
								setBusinessResult(null);
								setRuntimeBusinessError(null);
								setCurrentRunContext({ inputSummary: '', outputMode: '' });
                                setDependencyRepairState('idle');
                                setRuntimeDependencyPlan(null);
                                approvalRunContextRef.current = null;
                            }}>{text.reset}</button>
                            {runState === 'running' && runID && <button className="apps-secondary-button" type="button" onClick={cancelRun}>{text.cancelRun}</button>}
                            {showRunButton && <button className="apps-primary-button" type="button" onClick={() => runApp()}>{text.run}</button>}
                        </div>}
                        {showOutputShell && <AppRunOutput status={skillRunStatus} runState={runState} resultText={resultText} businessResult={businessResult} isTool={isTool} text={text} style={{ order: runtimeOrder.output }} layoutRegion={outputRegion} hidden={!showCompletedOutputRegion} />}
                        {showOutputRegion && (isTool || isBusiness) && (
                            <section className="apps-run-history" data-region={outputRegion === 'bottom' ? 'bottom' : outputRegion} style={{ order: runtimeOrder.history }}>
                                <div className="apps-preview-title-row">
                                    <div className="apps-definition__title">{text.runHistory}</div>
                                    <div className="apps-run-history__tools">
                                        <span className="apps-count">{runHistory.length}</span>
                                        <button className="apps-secondary-button" type="button" disabled={runHistory.length === 0} onClick={() => {
                                            clearAppRunHistory(app.id);
                                            setRunHistory([]);
                                        }}>{text.clearHistory}</button>
                                    </div>
                                </div>
                                {runHistory.length === 0 ? (
                                    <div className="apps-run-history__empty">{text.noRunHistory}</div>
                                ) : (
                                    <div className="apps-run-history__list">
                                        {runHistory.map((item) => (
                                            <AppRunHistoryItem item={item} title={item.inputSummary || appRunHistoryArtifactLabel(item) || item.runID} text={text} key={`${item.runID}-${item.at}`} />
                                        ))}
                                    </div>
                                )}
                            </section>
                        )}
                    </div>
                </div>
            </div>
        </>
    );
};

const approvalLanes = (text: typeof labels.zh) => [
    { key: 'my_requests', label: text.myRequests },
    { key: 'pending_my_approval', label: text.pendingMyApproval },
    { key: 'handled', label: text.handledApprovals },
    { key: 'attention', label: text.attentionApprovals },
    { key: 'all', label: text.allApprovalInstances },
] as const;

function approvalSearchText(item: ApprovalInstanceView, appName: string, lang?: string) {
    const outputText = (item.outputs || []).flatMap((output) => [approvalOutputKind(output), output.kind, output.type, output.title, output.text, output.status, output.artifact_id]).filter(Boolean);
    const artifactText = (item.artifacts || []).flatMap((artifact) => [artifact.id, artifact.name, artifact.uri, artifact.path, artifact.status, artifact.mime_type]).filter(Boolean);
    return [appName, item.appName, item.appID, item.title, item.id, approvalCurrentNodeText(item, lang), item.currentNode, item.owner, item.approver, item.currentAssignee, item.currentAssigneeType, item.result, item.workflowSkillID, item.approvalWorkflowID, item.workflowVersion, item.workflowDecisionID, item.datasetID, item.objectRole, item.approvalID, item.businessStatus, item.resultStatus, item.fromStatus, item.toStatus, item.recordID, ...outputText, ...artifactText]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
}
function dataSrvSummaryItemMatchesApprovalInstance(summary: DataSrvApprovalSummaryItem, instance: ApprovalInstanceView) {
    const summaryIDs = [summary.approvalID, summary.workflowInstanceID, summary.recordID]
        .map((value) => String(value || '').trim().toLowerCase())
        .filter(Boolean);
    const instanceIDs = [instance.approvalID, instance.id, instance.workflowDecisionID, instance.recordID]
        .map((value) => String(value || '').trim().toLowerCase())
        .filter(Boolean);
    if (summaryIDs.some((id) => instanceIDs.includes(id))) return true;
    const summaryAppID = String(summary.appID || '').trim();
    const instanceAppID = String(instance.appID || '').trim();
    if (summaryAppID && instanceAppID && appInstallIdentityKeys(summaryAppID).some((key) => appInstallIdentityKeys(instanceAppID).includes(key))) {
        const summaryNode = String(summary.currentNode || '').trim().toLowerCase();
        const instanceNode = String(instance.currentNode || '').trim().toLowerCase();
        return !summaryNode || !instanceNode || summaryNode === instanceNode;
    }
    return false;
}

const ApprovalManager = ({ apps, lang, initialAppFilter }: { apps: AppEntry[]; lang?: string; initialAppFilter: string }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [lane, setLane] = useState<ApprovalLaneFilter>('all');
    const [query, setQuery] = useState('');
    const [appFilter, setAppFilter] = useState(initialAppFilter || 'all');
    const [statusFilter, setStatusFilter] = useState('all');
    const [instances, setInstances] = useState<ApprovalInstanceView[]>([]);
    const [selectedInstanceId, setSelectedInstanceId] = useState('');
    const [loadingState, setLoadingState] = useState<'idle' | 'loading' | 'error'>('idle');
    const [dataSrvSummary, setDataSrvSummary] = useState<DataSrvApprovalSummaryState>({ status: 'loading', buckets: [] });
    const appNameById = useMemo(() => {
        const names = new Map<string, string>();
        apps.forEach((app) => appEntryIdentityKeys(app).forEach((key) => names.set(key, app.name)));
        return names;
    }, [apps]);
    const approvalApps = useMemo(() => apps.filter((app) => isEnterpriseApprovalAppKind(app.kind)), [apps]);
    const localSeedInstances = useMemo(() => apps.flatMap((app) => appSeedApprovalInstances(app, lang)), [apps, lang]);

    useEffect(() => {
        setAppFilter(initialAppFilter || 'all');
        setSelectedInstanceId('');
    }, [initialAppFilter]);

    const loadInstances = useCallback(async () => {
        setLoadingState('loading');
        try {
            const records = await ListMaclawAppApprovalInstancesAll('all', 200) as BackendApprovalInstance[];
            const remoteViews = (records || []).map((item) => backendApprovalInstanceToView(item, lang)).filter(Boolean) as ApprovalInstanceView[];
            setInstances(mergeApprovalInstanceViews(localSeedInstances, remoteViews));
            setLoadingState('idle');
        } catch {
            setInstances(localSeedInstances);
            setLoadingState(localSeedInstances.length > 0 ? 'idle' : 'error');
        }
    }, [lang, localSeedInstances]);

    useEffect(() => {
        void loadInstances();
    }, [loadInstances]);

    const loadDataSrvSummary = useCallback(async () => {
        setDataSrvSummary((current) => ({ ...current, status: 'loading', error: undefined }));
        try {
            setDataSrvSummary(await fetchDataSrvApprovalAppSummary(text));
        } catch (error: any) {
            setDataSrvSummary({ status: 'error', error: error?.message || String(error), buckets: [] });
        }
    }, [text]);

    useEffect(() => {
        void loadDataSrvSummary();
    }, [loadDataSrvSummary]);

    const filteredInstances = useMemo(() => {
        const normalizedQuery = query.trim().toLowerCase();
        return instances.filter((item) => {
            const laneMatches = lane === 'all'
                || (lane === 'handled' ? ['approved', 'rejected', 'failed', 'cancelled', 'timeout'].includes(item.status) : item.lane === lane);
            const matchedApp = findAppByRuntimeID(apps, item.appID);
            const appMatches = appFilter === 'all' || (!!matchedApp && appEntryMatchesID(matchedApp, appFilter));
            const statusMatches = statusFilter === 'all' || item.status === statusFilter;
            const queryMatches = !normalizedQuery || approvalSearchText(item, appNameById.get(item.appID || '') || '', lang).includes(normalizedQuery);
            return laneMatches && appMatches && statusMatches && queryMatches;
        });
    }, [instances, lane, appFilter, statusFilter, query, appNameById, apps, lang]);
    const selected = filteredInstances.find((item) => item.id === selectedInstanceId) || filteredInstances[0];
    const selectedApp = selected?.appID ? findAppByRuntimeID(apps, selected.appID) : undefined;
    const selectedResultContract = selectedApp ? appResultContractForManifest(selectedApp) : undefined;
    const lanes = approvalLanes(text);
    const countForLane = (key: ApprovalLaneFilter) => key === 'all'
        ? instances.length
        : key === 'handled'
            ? instances.filter((item) => ['approved', 'rejected', 'failed', 'cancelled', 'timeout'].includes(item.status)).length
            : instances.filter((item) => item.lane === key).length;
    const applyDataSrvSummaryBucket = (bucket: DataSrvApprovalSummaryBucket) => {
        setSelectedInstanceId('');
        setAppFilter('all');
        setQuery(bucket.query.result_type || '');
        if (bucket.key === 'my_requests' || bucket.key === 'pending_my_approval' || bucket.key === 'attention') {
            setLane(bucket.key as ApprovalLaneFilter);
        } else {
            setLane('all');
        }
        if (bucket.query.approval_status) {
            setStatusFilter(bucket.query.approval_status);
        } else if (bucket.key === 'attention') {
            setStatusFilter('attention');
        } else {
            setStatusFilter('all');
        }
    };
    const applyDataSrvSummaryItem = (item: DataSrvApprovalSummaryItem) => {
        const matchedInstance = instances.find((instance) => dataSrvSummaryItemMatchesApprovalInstance(item, instance));
        setSelectedInstanceId(matchedInstance?.id || '');
        setLane('all');
        setAppFilter('all');
        setStatusFilter(item.status || 'all');
        setQuery(item.approvalID || item.workflowInstanceID || item.recordID || item.appID || item.name);
    };

    const updateApprovalInstanceDecision = async (instance: ApprovalInstanceView, decision: 'approved' | 'rejected' | 'attention') => {
        if (!instance?.id) return;
        const now = new Date().toISOString();
        const zh = isZh(lang);
        const app = findAppByRuntimeID(apps, instance.appID);
        const statusText = decision === 'approved' ? (zh ? '\u5df2\u901a\u8fc7' : 'Approved') : decision === 'rejected' ? (zh ? '\u5df2\u9a73\u56de' : 'Rejected') : (zh ? '\u9700\u5173\u6ce8' : 'Needs attention');
        const nextLane: ApprovalInstanceView['lane'] = decision === 'attention' ? 'attention' : 'handled';
        const nextNode = decision === 'attention' ? instance.currentNode : (zh ? '\u5df2\u5b8c\u6210' : 'Completed');
        const workflowDecisionID = `decision-${Date.now().toString(36)}`;
        const resultPayload = approvalDecisionResultPayload(instance, decision, {
            resultText: statusText,
            currentNode: nextNode,
            workflowDecisionID,
            currentNodeIDs: [nextNode],
            fromStatus: instance.businessStatus || instance.status,
        });
        const outputs = approvalOutputsOrDefault(
            instance.outputs,
            decision,
            statusText,
            decision,
            decision,
            resultPayload,
            zh ? '\u5ba1\u6279\u7ed3\u679c' : 'Approval result',
        );
        const payload: BackendApprovalInstance = {
            instance_id: instance.id,
            app_id: instance.appID,
            app_name: instance.appName || app?.name,
            workflow_skill_id: instance.workflowSkillID,
            approval_workflow_id: instance.approvalWorkflowID || instance.approvalEvent || instance.workflowSkillID,
            workflow_version: instance.workflowVersion,
            workflow_decision_id: workflowDecisionID,
            approval_id: instance.approvalID,
            record_approval_id: instance.approvalID,
            approval_event: instance.approvalEvent,
            approval_object_role: instance.objectRole,
            object_role: instance.objectRole,
            dataset_id: instance.datasetID,
            blueprint_id: instance.blueprintID,
            title: instance.title,
            lane: nextLane,
            status: decision,
            current_node: nextNode,
            owner: instance.owner,
            applicant: instance.owner,
            approver: instance.approver,
            current_assignee: decision === 'attention' ? (instance.currentAssignee || instance.approver) : 'completed',
            current_assignee_type: decision === 'attention' ? (instance.currentAssigneeType || 'user') : 'system',
            result: statusText,
            business_status: decision,
            result_status: decision,
            from_status: instance.businessStatus || instance.status,
            to_status: decision,
            record_id: instance.recordID,
            detail_url: instance.detailURL,
            result_payload: resultPayload,
            outputs,
            artifacts: instance.artifacts,
            updated_at: now,
            events: [...(instance.events || []), { at: now, node: nextNode, actor: instance.approver || (zh ? '\u5ba1\u6279\u4eba' : 'Approver'), decision, message: statusText, action: decision }],
        };
        const fallback = backendApprovalInstanceToView(payload, lang);
        if (fallback) setInstances((current) => [fallback, ...current.filter((item) => item.id !== fallback.id)]);
        try {
            const saved = await RecordMaclawAppApprovalInstance(payload) as BackendApprovalInstance;
            const savedInstance = saved || payload;
			const view = backendApprovalInstanceToView(savedInstance, lang);
			if (view) setInstances((current) => [view, ...current.filter((item) => item.id !== view.id)]);
				if (app) {
					const syncedInstance = await syncApprovalInstanceToDataSrvWithEvents(app, savedInstance, lang);
					const syncedView = backendApprovalInstanceToView(syncedInstance, lang);
					if (syncedView) setInstances((current) => [syncedView, ...current.filter((item) => item.id !== syncedView.id)]);
                    saveApprovalDecisionRunEvidence(app, syncedInstance, statusText);
				}
			} catch {
				if (fallback) setInstances((current) => [fallback, ...current.filter((item) => item.id !== fallback.id)]);
	        }
    };

    return (
        <>
            <div className="apps-detail__header">
                <div>
                    <h2 className="apps-detail__title">{text.approvalManagerTitle}</h2>
                    <p className="apps-detail__subtitle">{text.approvalManagerHint}</p>
                </div>
                <button className="apps-secondary-button" type="button" onClick={() => { void loadInstances(); void loadDataSrvSummary(); }}>{text.approvalRefresh}</button>
            </div>
            <div className="apps-detail__body elegant-scrollbar">
                <div className="apps-approval-manager">
                    <DataSrvApprovalSummaryPanel summary={dataSrvSummary} text={text} onBucketSelect={applyDataSrvSummaryBucket} onItemSelect={applyDataSrvSummaryItem} onOpenApproval={(item) => {
                        const url = dataSrvApprovalDetailURL(dataSrvSummary.endpoint, item);
                        if (url) BrowserOpenURL(url);
                    }} onOpenRecord={(item) => {
                        const url = dataSrvBusinessRecordURL(dataSrvSummary.endpoint, item);
                        if (url) BrowserOpenURL(url);
                    }} />
                    <div className="apps-approval-manager__filters">
                        <input className="apps-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={text.approvalSearch} />
                        <label><span>{text.approvalAppFilter}</span><select value={appFilter} onChange={(event) => { setAppFilter(event.target.value); setSelectedInstanceId(''); }}><option value="all">{text.approvalAllApps}</option>{approvalApps.map((app) => <option key={app.id} value={app.id}>{app.name}</option>)}</select></label>
                        <label><span>{text.approvalStatusFilter}</span><select value={statusFilter} onChange={(event) => { setStatusFilter(event.target.value); setSelectedInstanceId(''); }}><option value="all">{text.approvalAllStatuses}</option>{(['pending', 'requires_input', 'approved', 'rejected', 'attention', 'failed', 'cancelled', 'timeout', 'draft'] as ApprovalInstanceView['status'][]).map((status) => <option key={status} value={status}>{approvalStatusLabel(status, lang)}</option>)}</select></label>
                    </div>
                    <nav className="apps-approval-manager__lanes" aria-label={text.approvalWorkspace}>
                        {lanes.map((item) => <button key={item.key} className={lane === item.key ? 'is-active' : ''} type="button" aria-pressed={lane === item.key} onClick={() => { setLane(item.key); setSelectedInstanceId(''); }}><span>{item.label}</span><strong>{countForLane(item.key)}</strong></button>)}
                    </nav>
                    <div className="apps-approval-manager__section-head">
                        <div>
                            <strong>{text.approvalManagerLocalSection}</strong>
                            <span>{text.approvalManagerLocalHint}</span>
                        </div>
                    </div>
                    {loadingState === 'loading' && <div className="apps-approval-empty" role="status">{text.approvalLoading}</div>}
                    {loadingState === 'error' && <div className="apps-approval-empty" role="alert">{text.approvalLoadError}</div>}
                    {loadingState !== 'loading' && loadingState !== 'error' && (
                        <div className="apps-approval-manager__body">
                            <div className="apps-approval-list" role="list" aria-label={text.approvalInstanceData}>
                                {filteredInstances.length === 0 ? <div className="apps-approval-empty" role="status">{text.noApprovalInstances}</div> : filteredInstances.map((item) => (
                                    <button className="apps-approval-row" data-state={item.status} data-selected={selected?.id === item.id ? 'true' : 'false'} role="listitem" type="button" key={item.id} onClick={() => setSelectedInstanceId(item.id)} aria-pressed={selected?.id === item.id}>
                                        <div><strong>{item.title}</strong><span>{appNameById.get(item.appID || '') || item.appName || item.appID || '-'} ·  {text.currentApprovalNode}: {approvalCurrentNodeText(item, lang)}</span><small>{text.approvalInstanceId}: {item.id} ·  {item.updatedAt}</small><div className="apps-approval-row__meta"><span>{text.approvalApplicantLabel}: {approvalApplicantText(item)}</span><span>{text.currentAssigneeLabel}: {approvalCurrentAssigneeText(item)}</span><span>{text.statusTransitionLabel}: {approvalStatusTransitionText(item, lang)}</span></div></div>
                                        <em>{approvalStatusLabel(item.status, lang)}</em>
                                    </button>
                                ))}
                            </div>
                            <ApprovalDetail instance={selected} resultContract={selectedResultContract} lang={lang} text={text} onDecision={updateApprovalInstanceDecision} />
                        </div>
                    )}
                </div>
            </div>
        </>
    );
};

const DataSrvApprovalSummaryPanel = ({ summary, text, onBucketSelect, onItemSelect, onOpenApproval, onOpenRecord }: { summary: DataSrvApprovalSummaryState; text: typeof labels.zh; onBucketSelect?: (bucket: DataSrvApprovalSummaryBucket) => void; onItemSelect?: (item: DataSrvApprovalSummaryItem) => void; onOpenApproval?: (item: DataSrvApprovalSummaryItem) => void; onOpenRecord?: (item: DataSrvApprovalSummaryItem) => void }) => {
    const [selectedKey, setSelectedKey] = useState('all');
    if (summary.status === 'loading') {
        return <section className="apps-datasrv-approval-summary" aria-label={text.datasrvApprovalSummary}><div className="apps-approval-empty" role="status">{text.datasrvApprovalSummaryLoading}</div></section>;
    }
    if (summary.status === 'disabled') {
        return <section className="apps-datasrv-approval-summary" aria-label={text.datasrvApprovalSummary}><div className="apps-approval-empty" role="status">{text.datasrvApprovalSummaryDisabled}</div></section>;
    }
    if (summary.status === 'error') {
        return <section className="apps-datasrv-approval-summary" aria-label={text.datasrvApprovalSummary}><div className="apps-approval-empty" role="alert">{`${text.datasrvApprovalSummaryError}: ${summary.error || '-'}`}</div></section>;
    }
    const visibleBuckets = summary.buckets.filter((bucket) => bucket.count > 0);
    const selectedBucket = visibleBuckets.find((bucket) => bucket.key === selectedKey) || visibleBuckets[0];
    return (
        <section className="apps-datasrv-approval-summary" aria-label={text.datasrvApprovalSummary}>
            <div className="apps-datasrv-approval-summary__head">
                <div>
                    <strong>{text.datasrvApprovalSummary}</strong>
                    <span>{text.datasrvApprovalSummaryHint}</span>
                </div>
                {summary.endpoint && <code>{summary.endpoint}</code>}
            </div>
            {visibleBuckets.length === 0 ? (
                <div className="apps-approval-empty" role="status">{text.datasrvApprovalSummaryEmpty}</div>
            ) : (
                <>
                    <div className="apps-datasrv-approval-summary__grid">
                        {visibleBuckets.map((bucket) => (
                        <button className="apps-datasrv-approval-summary__item" data-selected={selectedBucket?.key === bucket.key ? 'true' : 'false'} type="button" key={bucket.key} onClick={() => { setSelectedKey(bucket.key); onBucketSelect?.(bucket); }} aria-pressed={selectedBucket?.key === bucket.key}>
                            <span>{bucket.label}</span>
                            <strong>{bucket.count}</strong>
                            <small>{bucket.apps.join(' ·  ') || '-'}</small>
                        </button>
                        ))}
                    </div>
                    {selectedBucket && (
                        <div className="apps-datasrv-approval-summary__details" aria-label={text.datasrvApprovalDetails}>
                            <div className="apps-datasrv-approval-summary__details-head">
                                <strong>{selectedBucket.label}</strong>
                                <span>{selectedBucket.count}</span>
                            </div>
                            {selectedBucket.items.slice(0, 6).map((item) => {
                                const approvalURL = dataSrvApprovalDetailURL(summary.endpoint, item);
                                const recordURL = dataSrvBusinessRecordURL(summary.endpoint, item);
                                return (
                                    <div className="apps-datasrv-approval-summary__row" key={`${selectedBucket.key}-${item.approvalID || item.workflowInstanceID || item.recordID || item.appID || item.name}`}>
                                        <button className="apps-datasrv-approval-summary__row-main" type="button" onClick={() => onItemSelect?.(item)}>
                                            <div>
                                                <strong>{item.name}</strong>
                                                <span>{[item.appID, item.currentNode].filter(Boolean).join(' / ') || '-'}</span>
                                                {(item.approvalID || item.workflowInstanceID || item.recordID) && <code>{[item.approvalID, item.workflowInstanceID, item.recordID].filter(Boolean).join(' / ')}</code>}
                                                {(item.datasetID || item.objectRole) && <code>{[item.datasetID, item.objectRole].filter(Boolean).join(' / ')}</code>}
                                            </div>
                                            <small>{[item.status || item.decision, item.resultTypes.slice(0, 3).join(', '), item.updatedAt].filter(Boolean).join(' · ') || '-'}</small>
                                        </button>
                                        <div className="apps-datasrv-approval-summary__row-actions">
                                            <button className="apps-link-button" type="button" disabled={!approvalURL} onClick={() => approvalURL && onOpenApproval?.(item)}>{text.openDataSrvApproval}</button>
                                            <button className="apps-link-button" type="button" disabled={!recordURL} onClick={() => recordURL && onOpenRecord?.(item)}>{text.openDataSrvRecord}</button>
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                    )}
                </>
            )}
        </section>
    );
};

const ApprovalSupplementPanel = ({ instance, lang, text, onSupplement }: { instance: ApprovalInstanceView; lang?: string; text: typeof labels.zh; onSupplement?: (instance: ApprovalInstanceView, supplement: { note: string; reference: string }) => void | Promise<void> }) => {
    const info = approvalRequiresInputInfo(instance, lang);
    const [note, setNote] = useState('');
    const [reference, setReference] = useState('');
    const [submitting, setSubmitting] = useState(false);
    if (!info) return null;
    const canSubmit = !!onSupplement && !submitting && (note.trim() || reference.trim());
    const submit = async () => {
        if (!onSupplement || !canSubmit) return;
        setSubmitting(true);
        try {
            await onSupplement(instance, { note: note.trim(), reference: reference.trim() });
            setNote('');
            setReference('');
        } finally {
            setSubmitting(false);
        }
    };
    return (
        <section className="apps-approval-supplement" aria-label={info.title}>
            <div className="apps-approval-supplement__head">
                <strong>{info.title}</strong>
                <span>{info.message}</span>
            </div>
            {info.fields.length > 0 && <div className="apps-approval-supplement__fields"><span>{text.supplementFieldsLabel}</span>{info.fields.map((field) => <code key={field}>{field}</code>)}</div>}
            {onSupplement && (
                <div className="apps-approval-supplement__form">
                    <label><span>{text.supplementNoteLabel}</span><input value={note} onChange={(event) => setNote(event.target.value)} placeholder={isZh(lang) ? '\u8bf4\u660e\u5df2\u8865\u5145\u7684\u6750\u6599' : 'Describe what was added'} /></label>
                    <label><span>{text.supplementReferenceLabel}</span><input value={reference} onChange={(event) => setReference(event.target.value)} placeholder="artifact://..." /></label>
                    <button className="apps-primary-button" type="button" disabled={!canSubmit} onClick={() => void submit()}>{submitting ? text.supplementRunning : text.supplementContinue}</button>
                </div>
            )}
        </section>
    );
};
const ApprovalResultFeedbackPanel = ({ instance, lang, text }: { instance: ApprovalInstanceView; lang?: string; text: typeof labels.zh }) => {
    const summary = approvalResultFeedbackSummary(instance, lang);
    if (!summary) return null;
    const rows = [
        { label: text.approvalResult, value: summary.decision },
        { label: text.businessStatusLabel, value: summary.businessStatus },
        { label: text.resultStatusLabel, value: summary.resultStatus },
        { label: text.approvalOutputCount, value: String(summary.outputCount) },
        { label: text.approvalArtifactCount, value: String(summary.artifactCount) },
        { label: text.approvalPrimaryArtifact, value: summary.primaryArtifact || '-' },
    ];
    return (
        <section className="apps-approval-feedback" aria-label={text.approvalResultFeedback}>
            <div className="apps-approval-feedback__head">
                <strong>{text.approvalResultFeedback}</strong>
                <span>{summary.resultStatus}</span>
            </div>
            <dl className="apps-approval-feedback__grid">
                {rows.map((row) => <div key={row.label}><dt>{row.label}</dt><dd>{row.value || '-'}</dd></div>)}
            </dl>
            {summary.content && <p className="apps-approval-feedback__content">{summary.content}</p>}
        </section>
    );
};
const ApprovalDetail = ({ instance, resultContract, lang, text, onDecision, onSupplement }: { instance?: ApprovalInstanceView; resultContract?: AppResultContract; lang?: string; text: typeof labels.zh; onDecision: (instance: ApprovalInstanceView, decision: 'approved' | 'rejected' | 'attention') => void | Promise<void>; onSupplement?: (instance: ApprovalInstanceView, supplement: { note: string; reference: string }) => void | Promise<void> }) => {
    const selectedOutputs = instance?.outputs || [];
    const selectedArtifacts = instance?.artifacts || [];
    const selectedPayloadEntries = instance?.resultPayload ? Object.entries(instance.resultPayload).filter(([, value]) => formatApprovalResultValue(value)) : [];
    const hasResultPackage = selectedOutputs.length > 0 || selectedArtifacts.length > 0 || selectedPayloadEntries.length > 0;
    const canDecide = approvalInstanceCanDecide(instance);
    return (
        <aside className="apps-approval-detail" aria-label={text.approvalDetailSection}>
            <div className="apps-approval-detail__section-head">
                <strong>{text.approvalDetailSection}</strong>
                <span>{text.approvalDetailHint}</span>
            </div>
            {instance ? (
                <>
                    <div className="apps-approval-detail__head"><strong>{instance.title}</strong><span>{text.currentApprovalNode}: {approvalCurrentNodeText(instance, lang)}{instance.currentNodeStatus ? ` · ${text.currentNodeStatus}: ${instance.currentNodeStatus}` : ''}</span></div>
                    <dl className="apps-approval-facts">
                        <div><dt>{text.approvalApplicantLabel}</dt><dd>{approvalApplicantText(instance)}</dd></div><div><dt>{text.approvalApproverLabel}</dt><dd>{instance.approver}</dd></div><div><dt>{text.currentAssigneeLabel}</dt><dd>{approvalCurrentAssigneeText(instance)}</dd></div><div><dt>{text.assigneeTypeLabel}</dt><dd>{instance.currentAssigneeType || '-'}</dd></div><div><dt>{text.statusTransitionLabel}</dt><dd>{approvalStatusTransitionText(instance, lang)}</dd></div><div><dt>{text.approvalResult}</dt><dd>{instance.result}</dd></div>
                        <div><dt>{text.workflowSkill}</dt><dd>{[instance.workflowSkillID, instance.workflowVersion ? 'v' + instance.workflowVersion : ''].filter(Boolean).join(' @ ') || '-'}</dd></div><div><dt>{text.dataSrvRecord}</dt><dd>{instance.recordID || '-'}</dd></div><div><dt>{text.approvalObjectRoleLabel}</dt><dd>{instance.objectRole || '-'}</dd></div>
                        <div><dt>{text.remoteApprovalLabel}</dt><dd>{instance.approvalID || '-'}</dd></div><div><dt>{text.businessStatusLabel}</dt><dd>{instance.businessStatus || '-'}</dd></div><div><dt>{text.resultStatusLabel}</dt><dd>{instance.resultStatus || approvalStatusLabel(instance.status, lang)}</dd></div>
                        <div><dt>{text.currentNodeStatus}</dt><dd>{instance.currentNodeStatus || '-'}</dd></div><div><dt>{text.approvalNodeTasks}</dt><dd>{approvalNodeTaskCountText(instance, lang)}</dd></div>
                    </dl>
	                    {instance.detailURL && <div className="apps-approval-detail__links"><button className="apps-link-button" type="button" onClick={() => instance.detailURL && BrowserOpenURL(instance.detailURL)}>{text.viewFullWorkflow}</button></div>}
	                    {resultContract && <ResultContractPreview contract={resultContract} lang={lang} />}
	                    <ApprovalResultFeedbackPanel instance={instance} lang={lang} text={text} />
	                    {hasResultPackage && (
                        <section className="apps-approval-results" aria-label={text.approvalResultPackage}>
                            <div className="apps-approval-results__head"><strong>{text.approvalResultPackage}</strong><span>{selectedOutputs.length + selectedArtifacts.length + selectedPayloadEntries.length}</span></div>
                            {selectedOutputs.map((output, index) => {
                                const kind = approvalOutputKind(output);
                                const body = approvalOutputBody(output);
                                const artifactRef = output.artifact ? approvalArtifactReference(output.artifact) : '';
                                return <div className="apps-approval-output" data-kind={kind} key={`${instance.id}-output-${index}-${approvalOutputTitle(output, text)}`}><div><strong>{approvalOutputTitle(output, text)}</strong><span>{[kind, output.status].filter(Boolean).join(' ·  ')}</span></div>{body && <pre>{body}</pre>}{artifactRef && <code>{artifactRef}</code>}</div>;
                            })}
                            {selectedArtifacts.map((artifact, index) => <div className="apps-approval-artifact" key={`${instance.id}-artifact-${index}-${approvalArtifactReference(artifact)}`}><div><strong>{artifact.name || artifact.id || text.runArtifacts}</strong><span>{[artifact.status, artifact.mime_type, artifact.size_bytes ? `${artifact.size_bytes} bytes` : ''].filter(Boolean).join(' ·  ')}</span></div><code>{approvalArtifactReference(artifact)}</code></div>)}
                            {selectedPayloadEntries.length > 0 && <div className="apps-approval-payload"><strong>{text.approvalOutputData}</strong>{selectedPayloadEntries.map(([key, value]) => <div key={`${instance.id}-payload-${key}`}><span>{key}</span><pre>{formatApprovalResultValue(value)}</pre></div>)}</div>}
                        </section>
                    )}
                    <ApprovalSupplementPanel instance={instance} lang={lang} text={text} onSupplement={onSupplement} />

                    <div className="apps-approval-timeline">
                        {(instance.events && instance.events.length > 0 ? instance.events : [{ node: isZh(lang) ? '\u53d1\u8d77\u8282\u70b9' : 'Submit node' }, { node: approvalCurrentNodeText(instance, lang) }, { node: isZh(lang) ? '\u7ed3\u679c\u53cd\u9988' : 'Result feedback' }] as ApprovalInstanceEventView[]).map((event, index) => {
                            const primary = [event.node, event.decision].filter(Boolean).join(' ·  ') || event.message || '-';
                            const secondary = [event.actor, event.at].filter(Boolean).join(' ·  ');
                            return <div key={`${instance.id}-event-${index}-${primary}`}><span /><p><strong>{primary}</strong>{secondary && <small>{secondary}</small>}{event.message && primary !== event.message && <small>{event.message}</small>}</p></div>;
                        })}
                    </div>
                </>
            ) : <div className="apps-approval-empty apps-approval-empty--detail" role="status">{text.approvalDetailEmpty}</div>}
            {instance && (
                <div className="apps-approval-actions" aria-label={text.approvalActions}>
                    <button className="apps-primary-button" type="button" onClick={() => onDecision(instance, 'approved')} disabled={!canDecide}>{text.approve}</button>
                    <button className="apps-secondary-button" type="button" onClick={() => onDecision(instance, 'rejected')} disabled={!canDecide}>{text.reject}</button>
                    <button className="apps-secondary-button" type="button" onClick={() => onDecision(instance, 'attention')} disabled={!canDecide}>{text.markAttention}</button>
                </div>
            )}
        </aside>
    );
};

const RunHistoryManager = ({ apps, lang }: { apps: AppEntry[]; lang?: string }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [appFilter, setAppFilter] = useState('all');
    const [items, setItems] = useState<AppRunHistoryEntry[]>(() => loadAllAppRunHistory());
    const appNameById = useMemo(() => {
        const names = new Map<string, string>();
        apps.forEach((app) => appEntryIdentityKeys(app).forEach((key) => names.set(key, app.name)));
        return names;
    }, [apps]);
    const filteredItems = appFilter === 'all' ? items : items.filter((item) => item.appID === appFilter);
    return (
        <>
            <div className="apps-detail__header"><div><h2 className="apps-detail__title">{text.runHistoryManager}</h2><p className="apps-detail__subtitle">{text.runHistoryManagerHint}</p></div><button className="apps-secondary-button" type="button" onClick={() => setItems(loadAllAppRunHistory())}>{text.approvalRefresh}</button></div>
            <div className="apps-detail__body elegant-scrollbar">
                <section className="apps-run-history-manager">
                    <div className="apps-approval-manager__filters"><label><span>{text.approvalAppFilter}</span><select value={appFilter} onChange={(event) => setAppFilter(event.target.value)}><option value="all">{text.runHistoryAllApps}</option>{apps.map((app) => <option key={app.id} value={app.id}>{app.name}</option>)}</select></label></div>
                    {filteredItems.length === 0 ? <div className="apps-run-history__empty">{text.noGlobalRunHistory}</div> : (
                        <div className="apps-run-history__list">
                            {filteredItems.map((item) => <AppRunHistoryItem item={item} title={appNameById.get(item.appID) || item.appID} text={text} key={`${item.appID}-${item.runID}-${item.at}`} />)}
                        </div>
                    )}
                </section>
            </div>
        </>
    );
};

const ApprovalWorkspace = ({ app, runState, businessEntity, businessAction, businessNote, backendInstances, approvalLoadState = 'idle', lang, text, style, layoutRegion, onRefresh, onDecision, onSupplement }: { app: AppEntry; runState: 'idle' | 'running' | 'done' | 'error' | 'cancelled'; businessEntity: string; businessAction: string; businessNote: string; backendInstances?: ApprovalInstanceView[]; approvalLoadState?: 'idle' | 'loading' | 'error'; lang?: string; text: typeof labels.zh; style?: CSSProperties; layoutRegion?: string; onRefresh?: (lane: ApprovalLaneFilter) => void | Promise<void>; onDecision?: (instance: ApprovalInstanceView, decision: 'approved' | 'rejected' | 'attention') => void | Promise<void>; onSupplement?: (instance: ApprovalInstanceView, supplement: { note: string; reference: string }) => void | Promise<void> }) => {
    const [lane, setLane] = useState<ApprovalLaneFilter>('my_requests');
    const [selectedInstanceId, setSelectedInstanceId] = useState('');
    const fallbackInstances = approvalLoadState === 'error' ? [] : buildApprovalInstances(app, runState, businessEntity, businessAction, businessNote, lang);
    const instances = backendInstances && backendInstances.length > 0 ? backendInstances : fallbackInstances;
    const visibleInstances = lane === 'all' ? instances : lane === 'handled' ? instances.filter((item) => ['approved', 'rejected', 'failed', 'cancelled', 'timeout'].includes(item.status)) : instances.filter((item) => item.lane === lane);
    const selected = visibleInstances.find((item) => item.id === selectedInstanceId) || visibleInstances[0];
    const resultContract = appResultContractForManifest(app);
    const lanes = approvalLanes(text);
    const countForLane = (key: typeof lanes[number]['key']) => key === 'all'
        ? instances.length
        : key === 'handled'
            ? instances.filter((item) => ['approved', 'rejected', 'failed', 'cancelled', 'timeout'].includes(item.status)).length
            : instances.filter((item) => item.lane === key).length;
    useEffect(() => {
        if (onRefresh) void onRefresh(lane);
    }, [lane, onRefresh]);
    const selectedOutputs = selected?.outputs || [];
    const selectedArtifacts = selected?.artifacts || [];
    const selectedPayloadEntries = selected?.resultPayload ? Object.entries(selected.resultPayload).filter(([, value]) => formatApprovalResultValue(value)) : [];
    const hasResultPackage = selectedOutputs.length > 0 || selectedArtifacts.length > 0 || selectedPayloadEntries.length > 0;
    const canDecide = approvalInstanceCanDecide(selected);
    return (
        <section className="apps-runtime-section apps-approval-workspace" aria-label={text.approvalWorkspace} data-region={layoutRegion || 'center'} style={style}>
            <div className="apps-runtime-section__title">{text.approvalWorkspace}</div>
            <div className="apps-approval-toolbar">
                <span role={approvalLoadState === 'error' ? 'alert' : 'status'}>{approvalLoadState === 'loading' ? text.approvalLoading : approvalLoadState === 'error' ? text.approvalLoadError : text.approvalInstanceData + ': ' + instances.length}</span>
                <button className="apps-link-button" type="button" disabled={approvalLoadState === 'loading'} onClick={() => onRefresh && void onRefresh(lane)}>{text.approvalRefresh}</button>
            </div>
            <div className="apps-approval-layout">
                <nav className="apps-approval-nav" aria-label={text.approvalWorkspace}>
                    {lanes.map((item) => (
                        <button key={item.key} className={lane === item.key ? 'is-active' : ''} type="button" aria-pressed={lane === item.key} onClick={() => {
                            setLane(item.key);
                            setSelectedInstanceId('');
                        }}>
                            <span>{item.label}</span>
                            <strong>{countForLane(item.key)}</strong>
                        </button>
                    ))}
                </nav>
                <div className="apps-approval-list" role="list" aria-label={text.approvalInstanceData}>
                    {visibleInstances.length === 0 ? (
                        <div className="apps-approval-empty" role="status">{text.noApprovalInstances}</div>
                    ) : visibleInstances.map((item) => (
                        <button className="apps-approval-row" data-state={item.status} data-selected={selected?.id === item.id ? 'true' : 'false'} role="listitem" type="button" key={item.id} onClick={() => setSelectedInstanceId(item.id)} aria-pressed={selected?.id === item.id}>
                            <div>
                                <strong>{item.title}</strong>
                                <span>{text.currentApprovalNode}: {approvalCurrentNodeText(item, lang)}{item.currentNodeStatus ? ` · ${text.currentNodeStatus}: ${item.currentNodeStatus}` : ''} · {text.approvalResult}: {approvalStatusLabel(item.status, lang)}</span>
                                <small>{text.approvalInstanceId}: {item.id} · {item.updatedAt}</small>
                                <div className="apps-approval-row__meta"><span>{text.approvalApplicantLabel}: {approvalApplicantText(item)}</span><span>{text.currentAssigneeLabel}: {approvalCurrentAssigneeText(item)}</span><span>{text.statusTransitionLabel}: {approvalStatusTransitionText(item, lang)}</span></div>
                            </div>
                            <em>{approvalStatusLabel(item.status, lang)}</em>
                        </button>
                    ))}
                </div>
                <aside className="apps-approval-detail" aria-label={text.approvalDetailSection}>
                    <div className="apps-approval-detail__section-head">
                        <strong>{text.approvalDetailSection}</strong>
                        <span>{text.approvalDetailHint}</span>
                    </div>
                    {selected ? (
                        <>
                            <div className="apps-approval-detail__head">
                                <strong>{selected.title}</strong>
                                <span>{text.currentApprovalNode}: {approvalCurrentNodeText(selected, lang)}{selected.currentNodeStatus ? ` · ${text.currentNodeStatus}: ${selected.currentNodeStatus}` : ''}</span>
                            </div>
                            <dl className="apps-approval-facts">
                                <div><dt>{text.approvalApplicantLabel}</dt><dd>{approvalApplicantText(selected)}</dd></div>
                                <div><dt>{text.approvalApproverLabel}</dt><dd>{selected.approver}</dd></div>
                                <div><dt>{text.currentAssigneeLabel}</dt><dd>{approvalCurrentAssigneeText(selected)}</dd></div>
                                <div><dt>{text.assigneeTypeLabel}</dt><dd>{selected.currentAssigneeType || '-'}</dd></div>
                                <div><dt>{text.statusTransitionLabel}</dt><dd>{approvalStatusTransitionText(selected, lang)}</dd></div>
                                <div><dt>{text.approvalResult}</dt><dd>{selected.result}</dd></div>
                                <div><dt>{text.workflowSkill}</dt><dd>{[selected.workflowSkillID, selected.workflowVersion ? 'v' + selected.workflowVersion : ''].filter(Boolean).join(' @ ') || '-'}</dd></div>
                                <div><dt>{text.dataSrvRecord}</dt><dd>{selected.recordID || '-'}</dd></div>
                                <div><dt>{text.approvalObjectRoleLabel}</dt><dd>{selected.objectRole || '-'}</dd></div>
                                <div><dt>{text.remoteApprovalLabel}</dt><dd>{selected.approvalID || '-'}</dd></div>
                                <div><dt>{text.businessStatusLabel}</dt><dd>{selected.businessStatus || '-'}</dd></div>
                                <div><dt>{text.resultStatusLabel}</dt><dd>{selected.resultStatus || approvalStatusLabel(selected.status, lang)}</dd></div>
                                <div><dt>{text.currentNodeStatus}</dt><dd>{selected.currentNodeStatus || '-'}</dd></div>
                                <div><dt>{text.approvalNodeTasks}</dt><dd>{approvalNodeTaskCountText(selected, lang)}</dd></div>
                            </dl>
                            {selected.detailURL && (
                                <div className="apps-approval-detail__links">
                                    <button className="apps-link-button" type="button" onClick={() => selected.detailURL && BrowserOpenURL(selected.detailURL)}>{text.viewFullWorkflow}</button>
                                </div>
	                            )}
	                            <ResultContractPreview contract={resultContract} lang={lang} />
	                            <ApprovalResultFeedbackPanel instance={selected} lang={lang} text={text} />
	                            {hasResultPackage && (
                                <section className="apps-approval-results" aria-label={text.approvalResultPackage}>
                                    <div className="apps-approval-results__head"><strong>{text.approvalResultPackage}</strong><span>{selectedOutputs.length + selectedArtifacts.length + selectedPayloadEntries.length}</span></div>
                                    {selectedOutputs.map((output, index) => {
                                        const kind = approvalOutputKind(output);
                                        const body = approvalOutputBody(output);
                                        const artifactRef = output.artifact ? approvalArtifactReference(output.artifact) : '';
                                        return (
                                            <div className="apps-approval-output" data-kind={kind} key={`${selected.id}-output-${index}-${approvalOutputTitle(output, text)}`}>
                                                <div><strong>{approvalOutputTitle(output, text)}</strong><span>{[kind, output.status].filter(Boolean).join(' · ')}</span></div>
                                                {body && <pre>{body}</pre>}
                                                {artifactRef && <code>{artifactRef}</code>}
                                            </div>
                                        );
                                    })}
                                    {selectedArtifacts.map((artifact, index) => (
                                        <div className="apps-approval-artifact" key={`${selected.id}-artifact-${index}-${approvalArtifactReference(artifact)}`}>
                                            <div><strong>{artifact.name || artifact.id || text.runArtifacts}</strong><span>{[artifact.status, artifact.mime_type, artifact.size_bytes ? `${artifact.size_bytes} bytes` : ''].filter(Boolean).join(' · ')}</span></div>
                                            <code>{approvalArtifactReference(artifact)}</code>
                                        </div>
                                    ))}
                                    {selectedPayloadEntries.length > 0 && (
                                        <div className="apps-approval-payload">
                                            <strong>{text.approvalOutputData}</strong>
                                            {selectedPayloadEntries.map(([key, value]) => (
                                                <div key={`${selected.id}-payload-${key}`}><span>{key}</span><pre>{formatApprovalResultValue(value)}</pre></div>
                                            ))}
                                        </div>
                                    )}
                                </section>
                            )}
                            <ApprovalSupplementPanel instance={selected} lang={lang} text={text} onSupplement={onSupplement} />

                            <div className="apps-approval-timeline">
                                {(selected.events && selected.events.length > 0 ? selected.events : [
                                    { node: isZh(lang) ? '\u53d1\u8d77\u8282\u70b9' : 'Submit node' },
                                    { node: approvalCurrentNodeText(selected, lang) },
                                    { node: isZh(lang) ? '\u7ed3\u679c\u53cd\u9988' : 'Result feedback' },
                                ] as ApprovalInstanceEventView[]).map((event, index) => {
                                    const primary = [event.node, event.decision].filter(Boolean).join(' · ') || event.message || '-';
                                    const secondary = [event.actor, event.at].filter(Boolean).join(' · ');
                                    return (
                                        <div key={`${selected.id}-event-${index}-${primary}`}><span /><p><strong>{primary}</strong>{secondary && <small>{secondary}</small>}{event.message && primary !== event.message && <small>{event.message}</small>}</p></div>
                                    );
                                })}
                            </div>
                        </>
                    ) : (
                        <div className="apps-approval-empty apps-approval-empty--detail" role="status">{text.approvalDetailEmpty}</div>
                    )}
                    {selected && (
                        <div className="apps-approval-actions" aria-label={text.approvalActions}>
                            <button className="apps-primary-button" type="button" onClick={() => onDecision?.(selected, 'approved')} disabled={!canDecide}>{text.approve}</button>
                            <button className="apps-secondary-button" type="button" onClick={() => onDecision?.(selected, 'rejected')} disabled={!canDecide}>{text.reject}</button>
                            <button className="apps-secondary-button" type="button" onClick={() => onDecision?.(selected, 'attention')} disabled={!canDecide}>{text.markAttention}</button>
                        </div>
                    )}
                </aside>
            </div>
        </section>
    );
};
const SkillRunEvidence = ({ status, runState, text }: { status: SkillRunStatusView | null; runState: 'idle' | 'running' | 'done' | 'error' | 'cancelled'; text: typeof labels.zh }) => {
	const steps = (status?.steps || []).slice(0, 6);
	if (runState === 'idle' && steps.length === 0) {
        return <div className="apps-run-evidence apps-run-evidence--empty">{text.noRunEvidence}</div>;
    }
    return (
        <section className="apps-run-evidence">
            <div className="apps-run-evidence__header">
                <strong>{text.runSteps}</strong>
                <span>{status?.total_steps ? `${steps.length}/${status.total_steps}` : steps.length}</span>
            </div>
            {steps.length > 0 ? (
                <div className="apps-run-steps">
                    {steps.map((step, index) => (
                        <div className="apps-run-step" data-state={normalizeSkillRunLifecycle(step.status)} key={`${step.index ?? index}-${compactStepLabel(step)}`}>
                            <span className="apps-run-step__dot" />
                            <div>
                                <strong>{compactStepLabel(step)}</strong>
                                <span>{[step.status, step.duration_ms ? `${step.duration_ms}ms` : '', compactStepDetail(step)].filter(Boolean).join(' · ')}</span>
                            </div>
                        </div>
                    ))}
                </div>
            ) : (
                <div className="apps-run-evidence__muted">{runState === 'running' ? text.skillRunRunning : text.noRunEvidence}</div>
            )}
        </section>
	);
};

const StructuredBusinessErrorDetails = ({ error, text }: { error: StructuredBusinessErrorView; text: typeof labels.zh }) => {
	const stateRows = [
		[text.businessErrorCode, error.code],
		[text.businessErrorTarget, error.target],
		[text.businessErrorRequired, error.required],
		[text.businessErrorActual, error.actual],
	].filter((row): row is [string, string] => !!row[1]);
	return (
		<div className="apps-business-error">
			{stateRows.length > 0 && (
				<dl>
					{stateRows.map(([label, value]) => (
						<div key={label}>
							<dt>{label}</dt>
							<dd>{value}</dd>
						</div>
					))}
				</dl>
			)}
			{error.nextActions.length > 0 && (
				<div className="apps-business-error__actions">
					<span>{text.businessErrorNextActions}</span>
					{error.nextActions.map((action) => <code key={`${action.action}-${action.label || ''}`}>{action.label || action.action}</code>)}
				</div>
			)}
		</div>
	);
};

const AppRunOutput = ({ status, runState, resultText, businessResult, isTool, text, style, layoutRegion, hidden }: { status: SkillRunStatusView | null; runState: 'idle' | 'running' | 'done' | 'error' | 'cancelled'; resultText: string; businessResult?: BusinessOperationResultView | null; isTool: boolean; text: typeof labels.zh; style?: CSSProperties; layoutRegion?: string; hidden?: boolean }) => {
    if (hidden) {
        return <section className="apps-runtime-section apps-runtime-output" data-region={layoutRegion || 'right'} style={style} hidden aria-hidden="true" />;
    }
	const artifacts = skillRunArtifacts(status);
	const runID = String(status?.run_id || '').trim();
    const hasArtifacts = artifacts.length > 0;
    const showTextOutput = runState === 'done' && !businessResult && (!isTool || !hasArtifacts);
    // Section title: "\u7ed3\u679c" when done with artifacts, "\u8f93\u51fa" otherwise
    const sectionTitle = hasArtifacts ? text.outputResult || text.runtimeOutput : text.runtimeOutput;
    return (
        <section className="apps-runtime-section apps-runtime-output" data-region={layoutRegion || 'right'} style={style}>
            <div className="apps-runtime-section__title">{sectionTitle}</div>
            {businessResult && (
                <div className="apps-business-result" role="status">
                    <div className="apps-business-result__head">
                        <strong>{businessResult.mode}</strong>
                        <span>{businessResult.status}</span>
                    </div>
                    <dl>
                        <div><dt>{text.businessResultTarget}</dt><dd>{businessResult.target || '-'}</dd></div>
                        <div><dt>{text.businessResultType}</dt><dd>{businessResult.kind}</dd></div>
                        <div><dt>{text.businessResultRecords}</dt><dd>{businessResult.recordCount}</dd></div>
                    </dl>
                    {businessResult.rows.length > 0 && businessResult.columns.length > 0 && (
                        <div className="apps-business-result__table" role="table" aria-label={text.businessResultType}>
                            <div className="apps-business-result__row apps-business-result__row--head" role="row">{businessResult.columns.slice(0, 4).map((column) => <span role="columnheader" key={column}>{column}</span>)}</div>
                            {businessResult.rows.slice(0, 4).map((row, rowIndex) => <div className="apps-business-result__row" role="row" key={rowIndex}>{businessResult.columns.slice(0, 4).map((column) => <span role="cell" key={column}>{String(row[column] ?? '-')}</span>)}</div>)}
                        </div>
                    )}
                    {businessResult.message && <pre>{businessResult.message}</pre>}
                </div>
            )}
            {hasArtifacts ? (
                <div className="apps-run-artifacts">
                    {artifacts.map((artifact, index) => {
                        const artifactPath = String(artifact.path || '').trim();
                        const artifactID = String(artifact.id || '').trim();
                        const artifactURI = String(artifact.uri || '').trim();
                        const artifactName = String(artifact.name || artifactPath.split(/[\\/]/).pop() || '').trim();
                        const artifactDownloadState = String(artifact.download_state || '').trim().toLowerCase();
                        const artifactRemoteOnly = artifactDownloadState === 'remote' && !artifactPath;
                        const artifactSize = artifact.size_bytes ? formatFileSize(artifact.size_bytes) : '';
                        const artifactRef = artifactID || artifactURI;
                        const artifactDisplayRef = artifactPath || artifactURI || artifactName || String(artifact.remote_url || artifactID || '').trim();
                        return (
                            <div className="apps-run-artifact" key={`${artifactRef || artifactPath || artifactDisplayRef || 'artifact'}-${index}`}>
                                <span>{fileTypeIcon(artifactName, artifact.mime_type)} {artifactName}{artifactSize ? ` (${artifactSize})` : ''}</span>
                                {artifactDisplayRef && <code className="apps-output-path-link" onClick={() => void revealSkillRunArtifactFromUI(runID, artifactRef, artifactPath)}>{artifactDisplayRef}</code>}
                                <div className="apps-run-artifact__actions">
                                    <button className="apps-link-button" type="button" disabled={!artifactRef && !artifactPath} onClick={() => void openSkillRunArtifactFromUI(runID, artifactRef, artifactPath, artifactRemoteOnly)}>{artifactRemoteOnly ? text.downloadArtifact : text.openArtifact}</button>
                                    <button className="apps-link-button" type="button" disabled={!artifactRef && !artifactPath} onClick={() => void revealSkillRunArtifactFromUI(runID, artifactRef, artifactPath)}>{text.revealArtifact}</button>
                                </div>
                            </div>
                        );
                    })}
                </div>
            ) : showTextOutput ? (
                <div className="apps-output-text">
                    <span>{text.outputText}</span>
                    <pre>{renderOutputTextWithPaths(resultText)}</pre>
                </div>
            ) : (
                <div className="apps-output-empty">{artifactStatusLabel(status, text) || text.noOutputYet}</div>
            )}
        </section>
    );
};

function stableStringify(value: any): string {
    if (Array.isArray(value)) return `[${value.map((item) => stableStringify(item)).join(',')}]`;
    if (value && typeof value === 'object') {
        return `{${Object.keys(value).sort().map((key) => `${JSON.stringify(key)}:${stableStringify(value[key])}`).join(',')}}`;
    }
    return JSON.stringify(value);
}

function textHash(value: string): string {
    let hash = 2166136261;
    for (let i = 0; i < value.length; i += 1) {
        hash ^= value.charCodeAt(i);
        hash = Math.imul(hash, 16777619);
    }
    return (hash >>> 0).toString(16).padStart(8, '0');
}

function appDefinitionFingerprint(app: AppEntry): string {
    const manifest = app.manifest;
    const runtimeManifest = manifest ? {
        schema: manifest.schema,
        installUnit: manifest.installUnit,
        privateMarker: manifest.privateMarker,
        entryKind: manifest.entryKind,
        launchMode: manifest.launchMode,
        ...(manifest.datasrv ? { datasrv: manifest.datasrv } : {}),
        ...(manifest.mis ? { mis: manifest.mis } : {}),
        ...(manifest.skill ? { skill: manifest.skill } : {}),
        ...(manifest.appSkill ? { appSkill: manifest.appSkill } : {}),
        ...(manifest.dependencies ? { dependencies: manifest.dependencies } : {}),
        ...(manifest.ui ? { ui: manifest.ui } : {}),
        ...(manifest.resultContract ? { resultContract: manifest.resultContract } : {}),
        ...(manifest.testProtocol ? { testProtocol: manifest.testProtocol } : {}),
        ...(manifest.workflow ? { workflow: manifest.workflow } : {}),
    } : undefined;
    return textHash(stableStringify({
        name: app.name,
        description: app.description,
        category: app.category,
        kind: app.kind,
        icon: app.icon,
        ...(app.customIconDataUrl ? { customIconDataUrl: normalizeCustomIconDataUrl(app.customIconDataUrl) } : {}),
        version: normalizeAppVersion(app.version),
        manifest: runtimeManifest,
    }));
}

function canonicalAppManifestID(app: AppEntry): string {
    const dataSrvAppID = String(app.manifest?.datasrv?.appID || '').trim();
    if (app.source === 'datasrv' && dataSrvAppID) return dataSrvAppID;
    if (app.source === 'datasrv' && app.id.startsWith('datasrv-installed-')) return app.id.slice('datasrv-installed-'.length);
    return app.id;
}

function appEntryIdentityKeys(app: AppEntry): string[] {
    const keys = [
        ...appInstallIdentityKeys(app.id),
        ...appInstallIdentityKeys(canonicalAppManifestID(app)),
        ...(app.marketCapabilityID ? appInstallIdentityKeys(app.marketCapabilityID) : []),
        ...(app.manifest?.datasrv?.appID ? appInstallIdentityKeys(app.manifest.datasrv.appID) : []),
    ];
    return Array.from(new Set(keys.filter(Boolean)));
}

function appEntryMatchesID(app: AppEntry, appID?: string): boolean {
    const target = new Set(appInstallIdentityKeys(String(appID || '')));
    if (target.size === 0) return false;
    return appEntryIdentityKeys(app).some((key) => target.has(key));
}

function findAppByRuntimeID(apps: AppEntry[], appID?: string): AppEntry | undefined {
    return apps.find((app) => appEntryMatchesID(app, appID));
}

function appWorkspaceUIForManifest(ui: AppWorkspaceLayout | undefined, kind: AppKind): AppWorkspaceLayout | undefined {
    if (!ui) return ui;
    const normalizedUI = normalizeAppWorkspaceLayout(ui, kind);
    const entry = normalizedUI.entry || workspaceEntryForKind(kind);
    const layouts = normalizedUI.layouts || {};
    const layout = layouts[entry] || {};
    const runtime = normalizeRuntimeWorkspaceLayout(layout, kind);
    const regions = canonicalRuntimeWorkspaceRegions(runtime.regions);
    const fingerprint = workspaceLayoutFingerprint(entry, { ...runtime, regions });
    return {
        ...normalizedUI,
        entry,
        layouts: {
            ...layouts,
            [entry]: {
                ...layout,
                template: runtime.template,
                density: runtime.density,
                primaryRegion: runtime.primaryRegion,
                outputRegion: runtime.outputRegion,
                regions,
                fingerprint,
            },
        },
    };
}

function appToManifest(app: AppEntry, submission?: AppPublishSubmission, governanceOverrides: AppGovernanceOverrides = {}) {
    const manifest = app.manifest;
    const appID = canonicalAppManifestID(app);
    const skillBinding = appSkillRuntimeBinding(manifest);
    const ui = appWorkspaceUIForManifest(manifest?.ui, app.kind);
    return {
        schema: manifest?.schema || 'maclaw.app.v1',
        privateMarker: manifest?.privateMarker || 'x_maclaw_apps',
        installUnit: manifest?.installUnit || 'builtin',
        app: {
            id: appID,
            name: app.name,
            version: normalizeAppVersion(app.version),
            description: app.description,
            category: app.category,
            kind: app.kind,
            icon: app.icon,
            customIconDataUrl: app.customIconDataUrl,
            source: app.source,
            importedRunEvidence: latestAppRunEvidence(app) || undefined,
            launchMode: manifest?.launchMode || defaultLaunchModeForKind(app.kind),
            ui,
            datasrv: manifest?.datasrv,
            mis: manifest?.mis,
            appSkill: manifest?.appSkill,
            dependencies: manifest?.dependencies,
            resultContract: manifest?.resultContract,
            testProtocol: manifest?.testProtocol,
            workflow: manifest?.workflow,
            binding: {
                datasrv: manifest?.datasrv,
                mis: manifest?.mis,
                skill: skillBinding,
                appSkill: manifest?.appSkill,
                dependencies: manifest?.dependencies,
                ui,
                resultContract: manifest?.resultContract,
                testProtocol: manifest?.testProtocol,
                workflow: manifest?.workflow,
            },
            panel: {
                pinned: !!app.pinned,
                accent: app.accent,
                customIconDataUrl: app.customIconDataUrl,
            },
            aboutInfo: normalizeAppAboutInfo(app.aboutInfo),
            governance: appGovernanceForManifest(app, submission, governanceOverrides),
        },
    };
}

function skillDefinitionAppId(app: AppEntry): string {
    const skillID = appRunnableSkillID(app);
    const prefixedID = skillID ? `skill-app-${skillID}-` : '';
    return prefixedID && app.id.startsWith(prefixedID) ? app.id.slice(prefixedID.length) : app.id;
}

function appSkillRuntimeBinding(manifest?: AppManifestBinding, skillIDOverride?: string): AppManifestBinding['skill'] | undefined {
    if (!manifest) return undefined;
    const existing = manifest.skill;
    const skillID = String(skillIDOverride || existing?.id || manifest.appSkill?.id || '').trim();
    if (!skillID) return existing;
    const outputModes = normalizeOutputModes(existing?.outputModes || manifest.resultContract?.outputModes);
    return {
        ...existing,
        id: skillID,
        appDefinitionFile: existing?.appDefinitionFile || 'maclaw.app.json',
        inputMode: existing?.inputMode || 'form',
        multipleFiles: existing?.multipleFiles || false,
        outputModes,
        fields: normalizeSkillAppFields(existing?.fields || []),
    };
}

function appToSkillDefinitionManifest(app: AppEntry) {
    return appToManifest({ ...app, id: skillDefinitionAppId(app) });
}

function appsToPackManifest(apps: AppEntry[], submissions: Record<string, AppPublishSubmission> = {}, governanceOverrides: Record<string, AppGovernanceOverrides> = {}) {
    return {
        schema: 'maclaw.app.pack.v1',
        privateMarker: 'x_maclaw_apps',
        apps: apps.map((app) => appToManifest(app, submissions[app.id], governanceOverrides[app.id])),
    };
}

function appsToInstallManifest(apps: AppEntry[]) {
    return apps.length === 1 ? appToManifest(apps[0]) : appsToPackManifest(apps);
}

async function copyTextToClipboard(text: string) {
    const clipboard = typeof navigator !== 'undefined' ? navigator.clipboard : undefined;
    if (clipboard?.writeText) {
        await clipboard.writeText(text);
    }
}

const appManifestIdPattern = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/;

function validateOutputModes(outputModes: any, path: string): string {
    if (outputModes === undefined) return '';
    if (!Array.isArray(outputModes)) return `${path} must be an array`;
    for (let index = 0; index < outputModes.length; index += 1) {
        if (!allowedOutputModes.includes(String(outputModes[index] || '').trim().toLowerCase())) return `${path}[${index}] is invalid`;
    }
    return '';
}

function validateSkillFields(fields: any, path: string): string {
    if (fields === undefined) return '';
    if (!Array.isArray(fields)) return `${path} must be an array`;
    for (let index = 0; index < fields.length; index += 1) {
        const field = fields[index];
        if (!String(field?.name || '').trim()) return `${path}[${index}].name is required`;
        if (field?.type && !allowedSkillFieldTypes.includes(String(field.type))) return `${path}[${index}].type is invalid`;
        if (field?.options !== undefined && !Array.isArray(field.options)) return `${path}[${index}].options must be an array`;
    }
    return '';
}

function validateAppManifest(raw: any, path = 'maclaw.app.v1'): string {
    if (raw?.schema !== 'maclaw.app.v1') return `${path} schema must be maclaw.app.v1`;
    if (raw?.privateMarker !== 'x_maclaw_apps') return `${path} privateMarker must be x_maclaw_apps`;
    const app = raw?.app;
    if (!app || typeof app !== 'object') return `${path} requires app`;
    const id = String(app.id || '').trim();
    const name = String(app.name || '').trim();
    if (!id || !name) return `${path} requires app.id and app.name`;
    if (!appManifestIdPattern.test(id)) return `${path} app.id is invalid`;
    if (name.length > 80) return `${path} app.name is too long`;
    if (app.kind && !['enterprise_approval_app', 'enterprise_normal_app', 'enterprise_app', 'tool_app', 'automation_app'].includes(String(app.kind))) return `${path} app.kind is invalid`;
    if (app.launchMode && !['agent_dynamic_ui', 'fixed_skill_ui', 'automation_console'].includes(String(app.launchMode))) return `${path} app.launchMode is invalid`;
    const kind = normalizeAppKind(app.kind);
    const expectedLaunchMode = defaultLaunchModeForKind(kind);
    if (app.launchMode && app.launchMode !== expectedLaunchMode) return `${path} app.launchMode must be ${expectedLaunchMode} for ${kind}`;
    if (raw.installUnit && !['enterprise_app_pack', 'skill', 'mcp', 'builtin'].includes(String(raw.installUnit))) return `${path} installUnit is invalid`;
    if (raw.installUnit === 'skill' && kind !== 'tool_app') return `${path} installUnit must be skill only for tool_app`;
    if (kind === 'tool_app' && raw.installUnit && raw.installUnit !== 'skill') return `${path} installUnit must be skill for tool_app`;
    const skill = app.binding?.skill;
    if (kind === 'tool_app' && !skill) return `${path} binding.skill is required for tool_app`;
    if (skill) {
        if (!String(skill.id || '').trim()) return `${path} binding.skill.id is required`;
        if (skill.appDefinitionFile && !['maclaw.apps.json', 'maclaw.app.json'].includes(String(skill.appDefinitionFile))) return `${path} binding.skill.appDefinitionFile must be maclaw.apps.json or maclaw.app.json`;
        if (skill.inputMode && !['file', 'form', 'mixed'].includes(String(skill.inputMode))) return `${path} binding.skill.inputMode is invalid`;
        const outputError = validateOutputModes(skill.outputModes, `${path} binding.skill.outputModes`);
        if (outputError) return outputError;
        const fieldsError = validateSkillFields(skill.fields, `${path} binding.skill.fields`);
        if (fieldsError) return fieldsError;
    }
    const datasrv = app.binding?.datasrv;
    if (isEnterpriseAppKind(kind) && !datasrv) return `${path} binding.datasrv is required for ${kind}`;
    if (datasrv && !String(datasrv.domain || '').trim()) return `${path} binding.datasrv.domain is required`;
    return '';
}

function defaultLaunchModeForKind(kind: AppKind): AppManifestBinding['launchMode'] {
    return isEnterpriseAppKind(kind) ? 'agent_dynamic_ui' : kind === 'automation_app' ? 'automation_console' : 'fixed_skill_ui';
}

function validateSkillAppsManifest(raw: any): string {
    if (raw?.x_maclaw_apps !== 'v1') return 'maclaw.apps.json x_maclaw_apps must be v1';
    if (!Array.isArray(raw.apps)) return 'maclaw.apps.json apps must be an array';
    if (raw.apps.length === 0) return 'maclaw.apps.json apps must not be empty';
    for (let index = 0; index < raw.apps.length; index += 1) {
        const app = raw.apps[index];
        const id = String(app?.id || '').trim();
        const name = String(app?.name || '').trim();
        const skillID = String(app?.skill_id || id).trim();
        if (!id || !name || !skillID) return `maclaw.apps.json apps[${index}] requires id and name`;
        if (!appManifestIdPattern.test(id)) return `maclaw.apps.json apps[${index}].id is invalid`;
        if (name.length > 80) return `maclaw.apps.json apps[${index}].name is too long`;
        if (app.input_mode && !['file', 'form', 'mixed'].includes(String(app.input_mode))) return `maclaw.apps.json apps[${index}].input_mode is invalid`;
        const outputError = validateOutputModes(app.output_modes, `maclaw.apps.json apps[${index}].output_modes`);
        if (outputError) return outputError;
        const fieldsError = validateSkillFields(app.fields, `maclaw.apps.json apps[${index}].fields`);
        if (fieldsError) return fieldsError;
    }
    return '';
}

function manifestToAppEntry(raw: any): AppEntry | null {
    const app = raw?.app;
    const id = String(app?.id || '').trim();
    const name = String(app?.name || '').trim();
    if (validateAppManifest(raw)) return null;
    const kind = normalizeAppKind(app.kind);
    const launchMode = app.launchMode || defaultLaunchModeForKind(kind);
    const icon = normalizeSkillAppIcon(app.icon);
    const customIconDataUrl = normalizeCustomIconDataUrl(app.customIconDataUrl || app.panel?.customIconDataUrl);
    const governance = app.governance || {};
    const testEvidence = governance.testEvidence || governance.test_evidence;
    const runtimeID = id.startsWith('market-') ? id : `market-${id}`;
    const importedRunEvidence = normalizeImportedRunEvidence(testEvidence ? {
        ...testEvidence,
        runID: testEvidence.runID || testEvidence.runId || testEvidence.run_id,
        at: testEvidence.at || testEvidence.verifiedAt || testEvidence.verified_at,
        appID: runtimeID,
    } : undefined);
    return {
        id: runtimeID,
        name,
        description: String(app.description || ''),
        category: String(app.category || 'Market'),
        kind,
        icon,
        customIconDataUrl,
        accent: String(app.panel?.accent || defaultAccentForKind(kind)),
        pinned: !!app.panel?.pinned,
        version: normalizeAppVersion(app.version || raw.version),
        source: 'market',
        importedRunEvidence,
        workflowContract: governance.workflowContract || governance.workflow_contract,
        aboutInfo: app.aboutInfo && typeof app.aboutInfo === 'object' ? normalizeAppAboutInfo(app.aboutInfo) : undefined,
        manifest: {
            schema: 'maclaw.app.v1',
            installUnit: raw.installUnit === 'skill' || raw.installUnit === 'mcp' || raw.installUnit === 'builtin' ? raw.installUnit : 'enterprise_app_pack',
            privateMarker: 'x_maclaw_apps',
            entryKind: kind,
            launchMode,
            datasrv: normalizeAppDataSrv(app.binding?.datasrv),
            mis: normalizeAppMIS(app.binding?.mis),
            appSkill: app.binding?.appSkill,
            dependencies: normalizeAppDependencies(app.binding?.dependencies),
            ui: normalizeAppWorkspaceLayout(app.ui || app.binding?.ui, kind),
            resultContract: normalizeAppResultContract(app.binding?.resultContract || app.governance?.resultContract, kind, app.binding?.skill?.outputModes || []),
            testProtocol: appTestProtocolWithFingerprint(normalizeAppTestProtocol(app.binding?.testProtocol || app.governance?.testProtocol || app.governance?.testEvidence?.testProtocol, kind, app.binding?.skill?.outputModes || [], normalizeAppResultContract(app.binding?.resultContract || app.governance?.resultContract, kind, app.binding?.skill?.outputModes || []))),
            workflow: normalizeAppWorkflowMapping(app.binding?.workflow || app.governance?.workflow, kind, app.binding?.datasrv?.domain || 'business', app.binding?.datasrv?.objectRole || app.binding?.datasrv?.domain || 'record'),
            skill: app.binding?.skill ? {
                ...app.binding.skill,
                inputMode: app.binding.skill.inputMode === 'form' || app.binding.skill.inputMode === 'mixed' ? app.binding.skill.inputMode : 'file',
                multipleFiles: !!app.binding.skill.multipleFiles,
                outputModes: normalizeOutputModes(app.binding.skill.outputModes),
                fields: normalizeSkillAppFields(app.binding.skill.fields),
            } : undefined,
        },
    };
}

function manifestToAppEntries(raw: any): { apps: AppEntry[]; error?: string } {
    if (raw?.schema === 'maclaw.app.pack.v1') {
        if (raw.privateMarker !== 'x_maclaw_apps') return { apps: [], error: 'maclaw.app.pack.v1 privateMarker must be x_maclaw_apps' };
        if (!Array.isArray(raw.apps)) return { apps: [], error: 'maclaw.app.pack.v1 apps must be an array' };
        if (raw.apps.length === 0) return { apps: [], error: 'maclaw.app.pack.v1 apps must not be empty' };
        for (let index = 0; index < raw.apps.length; index += 1) {
            const error = validateAppManifest(raw.apps[index], `maclaw.app.pack.v1 apps[${index}]`);
            if (error) return { apps: [], error };
        }
        const parsed = raw.apps.map(manifestToAppEntry).filter((app: AppEntry | null): app is AppEntry => !!app);
        return parsed.length > 0 ? { apps: parsed } : { apps: [], error: 'maclaw.app.pack.v1 has no valid apps' };
    }
    if (Object.prototype.hasOwnProperty.call(raw || {}, 'x_maclaw_apps')) {
        const error = validateSkillAppsManifest(raw);
        if (error) return { apps: [], error };
        const parsed = (raw.apps as SkillAppManifestEntry[]).map(skillManifestToApp).filter((app: AppEntry | null): app is AppEntry => !!app);
        return parsed.length > 0 ? { apps: parsed } : { apps: [], error: 'maclaw.apps.json has no valid apps' };
    }
    if (raw?.schema === 'maclaw.app.v1') {
        const error = validateAppManifest(raw);
        if (error) return { apps: [], error };
        const app = manifestToAppEntry(raw);
        return app ? { apps: [app] } : { apps: [], error: 'maclaw.app.v1 is invalid' };
    }
    return { apps: [], error: undefined };
}

function buildInstallPlan(parsedApps: AppEntry[], installedApps: AppEntry[]) {
    const installedByIdentity = new Map<string, AppEntry>();
    installedApps.forEach((app) => appInstallIdentityKeys(app.id).forEach((id) => installedByIdentity.set(id, app)));
    const seenIds = new Set<string>();
    return parsedApps.map((app, index) => {
        const identities = appInstallIdentityKeys(app.id);
        const installed = identities.map((id) => installedByIdentity.get(id)).find(Boolean);
        const currentScopes = new Set(installed ? appRequiredScopes(installed) : []);
        const addedScopes = appRequiredScopes(app).filter((scope) => !currentScopes.has(scope));
        const reason = installed
            ? normalizeAppVersion(app.version) > normalizeAppVersion(installed.version) ? 'upgrade' : 'installed'
            : identities.some((id) => seenIds.has(id)) ? 'duplicate' : 'install';
        identities.forEach((id) => seenIds.add(id));
        return { app, installed, addedScopes, dependencies: appSkillDependencies(app), highRiskScopes: highRiskScopes(addedScopes), key: `${app.id}:${index}`, action: reason as 'install' | 'upgrade' | 'installed' | 'duplicate' };
    });
}

function installSummaryMessage(installedCount: number, upgradedCount: number, skippedCount: number, text: typeof labels.zh) {
    const parts = [`${text.installedCount}: ${installedCount}`];
    if (upgradedCount > 0) parts.push(`${text.upgradedCount}: ${upgradedCount}`);
    parts.push(`${text.skippedCount}: ${skippedCount}`);
    return parts.join(' · ');
}

function formatHubInstallScopeSummary(sourceAppCount: number | undefined, installedAppCount: number | undefined, dependencyCount: number | undefined, text: typeof labels.zh) {
    const sourceCount = Number(sourceAppCount || 0);
    const installedCount = Number(installedAppCount || 0);
    const dependencies = Number(dependencyCount || 0);
    const parts: string[] = [];
    if (Number.isFinite(sourceCount) && sourceCount > 0 && Number.isFinite(installedCount) && installedCount > 0) {
        parts.push(text.hubInstallSummary.replace('{source}', String(sourceCount)).replace('{installed}', String(installedCount)));
    }
    if (Number.isFinite(dependencies) && dependencies > 0) {
        parts.push(text.hubInstallDependencySummary.replace('{count}', String(dependencies)));
    }
    return parts.join(' · ');
}
function dataSrvRegistrationNumber(value: unknown) {
    const numberValue = Number(value || 0);
    return Number.isFinite(numberValue) ? numberValue : 0;
}

type DataSrvRegistrationState = 'ready' | 'partial' | 'failed' | 'skipped';

function dataSrvRegistrationState(registration: BackendAppDataSrvRegistration | undefined | null): { state: DataSrvRegistrationState; eligibleCount: number; syncedCount: number; failedCount: number; reason: string } | null {
    const eligibleCount = dataSrvRegistrationNumber(registration?.eligible_count);
    if (!registration || eligibleCount <= 0) return null;
    const syncedCount = dataSrvRegistrationNumber(registration.synced_count);
    const failedCount = dataSrvRegistrationNumber(registration.failed_count);
    const reason = String(registration.reason || '').trim();
    if (registration.synced || (syncedCount >= eligibleCount && failedCount === 0)) return { state: 'ready', eligibleCount, syncedCount: syncedCount || eligibleCount, failedCount, reason };
    if (syncedCount > 0 && (failedCount > 0 || syncedCount < eligibleCount)) return { state: 'partial', eligibleCount, syncedCount, failedCount, reason };
    if (failedCount > 0) return { state: 'failed', eligibleCount, syncedCount, failedCount, reason };
    return { state: 'skipped', eligibleCount, syncedCount, failedCount, reason };
}

function dataSrvRegistrationSummary(registration: BackendAppDataSrvRegistration | undefined | null, text: typeof labels.zh) {
    const status = dataSrvRegistrationState(registration);
    if (!status) return '';
    const ratio = `${status.syncedCount}/${status.eligibleCount}`;
    if (status.state === 'ready') return `${text.datasrvRegistrationReady}: ${ratio}`;
    if (status.state === 'partial') return `${text.datasrvRegistrationPartial}: ${ratio}${status.reason ? ` · ${status.reason}` : ''}`;
    if (status.state === 'failed') return `${text.datasrvRegistrationFailed}: ${status.reason || ratio}`;
    return `${text.datasrvRegistrationSkipped}: ${status.reason || ratio}`;
}

function dataSrvRegistrationSummaryForApp(registration: BackendAppDataSrvRegistration | undefined | null, appID: string, text: typeof labels.zh) {
    if (!registration) return '';
    const items = Array.isArray(registration.items) ? registration.items : [];
    const appIDs = new Set(appInstallIdentityKeys(appID));
    const item = items.find((entry) => appInstallIdentityKeys(String(entry?.app_id || '')).some((id) => appIDs.has(id)));
    if (!item) return items.length === 0 ? dataSrvRegistrationSummary(registration, text) : '';
    const roleBindingCount = dataSrvRegistrationNumber(item.role_binding_count);
    if (item.synced) return roleBindingCount > 0 ? `${text.datasrvRegistrationReady}: ${roleBindingCount}` : text.datasrvRegistrationReady;
    const reason = String(item.reason || '').trim();
    return reason ? `${text.datasrvRegistrationFailed}: ${reason}` : text.datasrvRegistrationFailed;
}

function appHasDataSrvRegistrationCandidate(app: AppEntry) {
    const datasrv = app.manifest?.datasrv;
    const datasetID = String(datasrv?.datasetID || '').trim();
    if (!isEnterpriseAppKind(app.kind) || !datasetID) return false;
    if (String(datasrv?.objectRole || '').trim()) return true;
    if (app.kind === 'enterprise_normal_app') {
        return !!String(datasrv?.preferredAction || datasrv?.preferredView || datasrv?.preferredReport || datasrv?.preferredDashboard || datasrv?.domain || '').trim();
    }
    return app.kind === 'enterprise_approval_app' && !!(app.manifest?.mis?.approvalBindings || []).some((binding) => String(binding.objectRole || '').trim());
}

function installDetailWithDataSrvRegistration(detail: string, registration: BackendAppDataSrvRegistration | undefined | null, appID: string | undefined, dataSrvCandidate: boolean | undefined, text: typeof labels.zh) {
    if (!appID || !dataSrvCandidate) return detail;
    const summary = dataSrvRegistrationSummaryForApp(registration, appID, text);
    return summary ? `${detail} · ${summary}` : detail;
}

function backendDependencyMatchesAppIDs(dep: BackendAppInstallDependency, appIds: string[]) {
    if (appIds.length === 0) return false;
    if (!dep.app_ids?.length) return true;
    const selected = new Set(appIds.flatMap(appInstallIdentityKeys));
    return dep.app_ids.some((id) => appInstallIdentityKeys(id).some((key) => selected.has(key)));
}

function backendDependenciesForApp(plan: BackendAppInstallPlan | null | undefined, appId: string) {
    return backendDependenciesForAppIDs(plan, [appId]);
}

function backendDependenciesForAppIDs(plan: BackendAppInstallPlan | null | undefined, appIds: string[]) {
    return (plan?.dependencies || []).filter((dep) => backendDependencyMatchesAppIDs(dep, appIds));
}

function isBlockingBackendDependency(dep: BackendAppInstallDependency) {
    if (dep.required === false) return false;
    const action = String(dep.action || '').trim();
    const health = String(dep.health || '').trim();
    if (action === 'blocked' || action === 'failed') return true;
    if (!dep.installed) return action !== 'install';
    return !!health && health !== 'ready';
}

function hasMissingRequiredBackendDependency(plan: BackendAppInstallPlan | null | undefined, appIds: string[]) {
    if (appIds.length === 0) return false;
    return (plan?.dependencies || []).some((dep) => isBlockingBackendDependency(dep) && backendDependencyMatchesAppIDs(dep, appIds));
}
function firstBlockingBackendDependencyForApp(plan: BackendAppInstallPlan | null | undefined, appId: string) {
    return firstBlockingBackendDependencyForAppIDs(plan, [appId]);
}

function firstBlockingBackendDependencyForAppIDs(plan: BackendAppInstallPlan | null | undefined, appIds: string[]) {
    const appDependencies = backendDependenciesForAppIDs(plan, appIds);
    const candidates = appDependencies.length > 0 ? appDependencies : plan?.dependencies || [];
    return candidates.find(isBlockingBackendDependency) || null;
}

function backendDependencyUnavailableMessage(app: AppEntry, plan: BackendAppInstallPlan | null | undefined, text: typeof labels.zh, lang?: string) {
    const dep = firstBlockingBackendDependencyForAppIDs(plan, appDependencyVerificationAppIDs(app));
    if (!dep?.id) return text.missingRequiredDependency;
    const zh = isZh(lang);
    const rawState = String(dep.installed_status || dep.health || dep.action || '').trim();
    const normalizedState = rawState.toLowerCase();
    if (zh) {
        const reason = normalizedState === 'needs_review'
            ? '\u9700\u8981\u5728\u6280\u80fd\u7ba1\u7406\u4e2d\u5ba1\u6838\u901a\u8fc7\u540e\u624d\u80fd\u4f7f\u7528'
            : dep.installed ? (rawState ? '\u672a\u5b89\u88c5\u6216\u5df2\u505c\u7528' : '\u4e0d\u53ef\u7528') : '\u672a\u5b89\u88c5';
        return app.name + '\u6682\u4e0d\u53ef\u7528\uff1a' + dep.id + ' ' + reason + '\u3002';
    }
    const reason = normalizedState === 'needs_review'
        ? 'requires approval in Skill Management before use'
        : dep.installed ? (rawState ? 'is missing or disabled' : 'is unavailable') : 'is not installed';
    return app.name + ' is unavailable: ' + dep.id + ' ' + reason + '.';
}

function backendDependencySummary(dep: BackendAppInstallDependency, text: typeof labels.zh) {
    const health = String(dep.health || '').trim();
    const versionStatus = String(dep.version_status || '').trim();
    const status = versionStatus === 'mismatch'
        ? text.unavailableDependency
        : dep.installed && health && health !== 'ready'
        ? text.unavailableDependency
        : dep.installed ? text.installedDependency : text.missingDependency;
    const version = dep.version ? `@${dep.version}` : '';
    const installedVersion = String(dep.installed_version || '').trim();
    const requiredVersion = String(dep.required_version || dep.version || '').trim();
    const versionDetail = versionStatus === 'mismatch'
        ? ` (${installedVersion || 'unknown'} -> ${requiredVersion || 'unknown'})`
        : '';
    const installRef = backendDependencyInstallRef(dep);
    const ref = installRef ? ` ref:${installRef}` : '';
    const required = dep.required === false ? '' : ' *';
    const healthDetail = dep.installed && health && health !== 'ready' && versionStatus !== 'mismatch' ? ` (${dep.installed_status || health})` : '';
    return `${status}: ${dep.id}${version}${ref}${required}${versionDetail}${healthDetail}`;
}
function backendDependencyInstallTarget(dep: BackendAppInstallDependency) {
    const target = String(dep.install_ref_target || '').trim();
    if (!target) return '';
    const version = String(dep.install_ref_version || '').trim();
    return version ? `${target}@${version}` : target;
}

function backendDependencyDiagnosticMeta(dep: BackendAppInstallDependency) {
    const installRefStatus = String(dep.install_ref_status || '').trim();
    const installTarget = backendDependencyInstallTarget(dep);
    return [
        dep.install_ref_kind ? `ref-kind:${dep.install_ref_kind}` : '',
        installTarget ? `target:${installTarget}` : '',
        installRefStatus && installRefStatus !== 'ok' ? `ref-status:${installRefStatus}` : '',
    ].filter(Boolean);
}

function backendDependencyDiagnosticItems(dep: BackendAppInstallDependency) {
    const preflightStatus = String(dep.preflight_status || '').trim();
    const integrityStatus = String(dep.integrity_status || '').trim();
    const showPreflight = !!preflightStatus && preflightStatus !== 'ready';
    const showIntegrity = !!integrityStatus;
    return [
        showPreflight ? `preflight:${preflightStatus}` : '',
        showPreflight && dep.preflight_code ? `preflight-code:${dep.preflight_code}` : '',
        showPreflight && dep.preflight_stage ? `preflight-stage:${dep.preflight_stage}` : '',
        showIntegrity ? `integrity:${integrityStatus}` : '',
        showIntegrity && dep.integrity_code ? `integrity-code:${dep.integrity_code}` : '',
        showIntegrity && dep.integrity_stage ? `integrity-stage:${dep.integrity_stage}` : '',
        dep.package_sha256 || dep.package_checksum ? `sha:${dep.package_sha256 || dep.package_checksum}` : '',
        dep.package_signature ? 'signature:available' : '',
        dep.package_download_url ? 'download:available' : '',
        dep.download_node ? `download_node:${dep.download_node}` : '',
        dep.resolved_download_url ? `resolved:${dep.resolved_download_url}` : '',
        dep.install_error_code ? `code:${dep.install_error_code}` : '',
        dep.install_error_stage ? `stage:${dep.install_error_stage}` : '',
        dep.install_error_detail || (showPreflight ? dep.preflight_message : '') || dep.integrity_message || dep.install_ref_message || '',
    ].filter(Boolean);
}

type BackendDependencyTraceItem = {
    label: string;
    value: string;
    state: 'ready' | 'running' | 'blocked' | 'unknown';
};

function dependencyTraceState(value: string, fallback: BackendDependencyTraceItem['state'] = 'unknown'): BackendDependencyTraceItem['state'] {
    const normalized = value.trim().toLowerCase();
    if (['ok', 'ready', 'matched', 'installed', 'skip', 'success', 'verified', 'passed'].includes(normalized)) return 'ready';
    if (['blocked', 'failed', 'missing', 'disabled', 'needs_review', 'mismatch', 'error', 'download_failed', 'integrity_failed'].includes(normalized)) return 'blocked';
    if (['install', 'installing', 'queued', 'pending', 'checking', 'running'].includes(normalized)) return 'running';
    return fallback;
}

function dependencyDiagnosticStatusFailed(status: string) {
    const normalized = status.trim().toLowerCase();
    return ['blocked', 'failed', 'missing', 'disabled', 'needs_review', 'mismatch', 'error'].includes(normalized);
}

function dependencyDiagnosticCodeFailed(code: string) {
    const normalized = code.trim().toLowerCase();
    return normalized.includes('failed')
        || normalized.includes('mismatch')
        || normalized.includes('missing')
        || normalized.includes('needs_review')
        || normalized.includes('review_required')
        || normalized.includes('not_found')
        || normalized.includes('denied')
        || normalized.includes('rejected')
        || normalized.includes('invalid')
        || normalized.includes('untrusted');
}

function backendDependencyTraceItems(dep: BackendAppInstallDependency, text: typeof labels.zh): BackendDependencyTraceItem[] {
    const items: BackendDependencyTraceItem[] = [];
    const add = (label: string, value: string, state?: BackendDependencyTraceItem['state']) => {
        const clean = value.trim();
        if (!clean) return;
        items.push({ label, value: clean, state: state || dependencyTraceState(clean) });
    };
    const installRef = backendDependencyInstallRef(dep);
    const installTarget = backendDependencyInstallTarget(dep);
    add(text.dependencyResolutionStage, [
        installRef ? `ref:${installRef}` : '',
        dep.install_ref_status ? `status:${dep.install_ref_status}` : '',
        installTarget ? `target:${installTarget}` : '',
        dep.install_ref_message || '',
    ].filter(Boolean).join(' · '), dependencyTraceState(String(dep.install_ref_status || '').trim(), installRef ? 'ready' : 'unknown'));
    add(text.dependencyPreflightStage, [
        dep.preflight_status || '',
        dep.preflight_code ? `code:${dep.preflight_code}` : '',
        dep.preflight_stage ? `stage:${dep.preflight_stage}` : '',
        dep.preflight_message || '',
    ].filter(Boolean).join(' · '), dependencyTraceState(String(dep.preflight_status || '').trim()));
    add(text.dependencyDownloadStage, [
        dep.download_node ? `node:${dep.download_node}` : '',
        dep.resolved_download_url ? `resolved:${dep.resolved_download_url}` : '',
        dep.package_download_url ? (dep.download_node ? `preferred:${dep.package_download_url}` : 'available') : '',
        dep.package_sha256 || dep.package_checksum ? `sha:${dep.package_sha256 || dep.package_checksum}` : '',
        dep.package_signature ? 'signature:available' : '',
    ].filter(Boolean).join(' · '), dep.download_node || dep.resolved_download_url || dep.package_download_url || dep.package_sha256 || dep.package_checksum || dep.package_signature
        ? (dep.install_error_code === 'download_failed' ? 'blocked' : 'ready')
        : undefined);
    add(text.dependencyIntegrityStage, [
        dep.integrity_status || '',
        dep.integrity_code ? `code:${dep.integrity_code}` : '',
        dep.integrity_stage ? `stage:${dep.integrity_stage}` : '',
        dep.integrity_message || '',
    ].filter(Boolean).join(' · '), dependencyTraceState(String(dep.integrity_status || '').trim()));
    add(text.dependencyInstallStage, [
        dep.action || '',
        dep.health ? `health:${dep.health}` : '',
        dep.installed_status ? `status:${dep.installed_status}` : '',
        dep.installed_version ? `installed:${dep.installed_version}` : '',
    ].filter(Boolean).join(' · '), isBlockingBackendDependency(dep) ? 'blocked' : dep.installed ? 'ready' : dependencyTraceState(String(dep.action || '').trim()));
    add(text.dependencyErrorStage, [
        dep.install_error_code || '',
        dep.install_error_stage ? `stage:${dep.install_error_stage}` : '',
        dep.install_error_detail || '',
    ].filter(Boolean).join(' · '), 'blocked');
    return items;
}

function installRecordMissingDependencyCount(record: BackendAppInstallRecord) {
    return (record.dependencies || []).filter(isBlockingBackendDependency).length;
}

function installRecordPackageSHA(record: BackendAppInstallRecord | null | undefined) {
    return installRecordString(record?.package_sha || record?.package_sha256);
}

function installRecordDependencyState(dep: BackendAppInstallDependency) {
    if (isBlockingBackendDependency(dep)) return 'blocked';
    if (dep.installed) return 'ready';
    return 'missing';
}

function installRecordDependencyStatus(dep: BackendAppInstallDependency, text: typeof labels.zh) {
    const summary = backendDependencySummary(dep, text);
    const separator = summary.indexOf(':');
    return separator > 0 ? summary.slice(0, separator) : dep.installed ? text.installedDependency : text.missingDependency;
}

function versionSnapshotSkillText(skill: BackendAppInstallSkillVersionSnapshot | undefined | null) {
    if (!skill?.id) return '';
    const meta = [skill.kind, skill.source, skill.version ? 'v' + skill.version : ''].filter(Boolean).join(' · ');
    return meta ? skill.id + ' · ' + meta : skill.id;
}

function installVersionSnapshotItems(snapshot: BackendAppInstallVersionSnapshot | undefined | null, text: typeof labels.zh) {
    if (!snapshot) return [];
    const items: Array<{ label: string; value: string }> = [];
    if (snapshot.app_entry_version) items.push({ label: text.appVersion, value: 'v' + snapshot.app_entry_version });
    const appSkill = versionSnapshotSkillText(snapshot.app_skill);
    if (appSkill) items.push({ label: text.appSkill, value: appSkill });
    (snapshot.workflow_skills || []).forEach((skill) => {
        const value = versionSnapshotSkillText(skill);
        if (value) items.push({ label: text.workflowSkill, value });
    });
    (snapshot.approval_bindings || []).forEach((binding) => {
        const workflow = [binding.workflow_skill_id, binding.workflow_version ? 'v' + binding.workflow_version : ''].filter(Boolean).join('@');
        const value = [binding.event, binding.object_role, workflow].filter(Boolean).join(' · ');
        if (value) items.push({ label: text.approvalBinding, value });
    });
    return items;
}

const InstallVersionSnapshot = ({ snapshot, text }: { snapshot?: BackendAppInstallVersionSnapshot | null; text: typeof labels.zh }) => {
    const items = installVersionSnapshotItems(snapshot, text);
    if (items.length === 0) return null;
    return (
        <div className="apps-install-version-snapshot" role="list" aria-label={text.versionSnapshot}>
            {items.map((item, index) => (
                <span key={`${item.label}-${index}`} role="listitem">
                    <strong>{item.label}</strong>
                    <em>{item.value}</em>
                </span>
            ))}
        </div>
    );
};

/** App About Dialog - shows app name, version, author, copyright, etc. */
const AppAboutDialog = ({ app, text, onClose }: { app: AppEntry; text: typeof labels.zh; onClose: () => void }) => {
    const about = app.aboutInfo;
    const version = app.version ?? 1;
    const onCloseRef = useRef(onClose);
    onCloseRef.current = onClose;
    useEffect(() => {
        const onKey = (e: globalThis.KeyboardEvent) => { if (e.key === 'Escape') onCloseRef.current(); };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, []);
    const sanitizedWebsite = useMemo(() => {
        const url = about?.website;
        if (!url) return '';
        const trimmed = url.trim();
        if (/^https?:\/\//i.test(trimmed)) return trimmed;
        if (/^mailto:/i.test(trimmed)) return trimmed;
        if (/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z]{2,})+/i.test(trimmed) && !trimmed.includes(' ')) return `https://${trimmed}`;
        return '';
    }, [about?.website]);
    return (
        <div className="apps-about-dialog-backdrop" onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
            <div className="apps-about-dialog" role="dialog" aria-modal="true" aria-label={`${text.aboutDialogTitle}: ${app.name}`}>
                <div className="apps-about-dialog__header">
                    <span className="apps-about-dialog__icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}>
                        <AppIcon icon={app.icon} customIconDataUrl={app.customIconDataUrl} />
                    </span>
                    <div className="apps-about-dialog__title-group">
                        <h3 className="apps-about-dialog__name">{app.name}</h3>
                        <span className="apps-about-dialog__version">{text.aboutVersion} {version}</span>
                    </div>
                    <button className="apps-about-dialog__close" type="button" onClick={onClose} aria-label={text.close} autoFocus>&times;</button>
                </div>
                {app.description && <p className="apps-about-dialog__desc">{app.description}</p>}
                <div className="apps-about-dialog__body">
                    {about?.author && <div className="apps-about-dialog__row"><span className="apps-about-dialog__label">{text.aboutAuthor}</span><span>{about.author}</span></div>}
                    {about?.copyright && <div className="apps-about-dialog__row"><span className="apps-about-dialog__label">{text.aboutCopyright}</span><span>{about.copyright}</span></div>}
                    {sanitizedWebsite && <div className="apps-about-dialog__row"><span className="apps-about-dialog__label">{text.aboutWebsite}</span><a href={sanitizedWebsite} className="apps-about-dialog__link" onClick={(e) => { e.preventDefault(); BrowserOpenURL(sanitizedWebsite); }}>{about?.website}</a></div>}
                    {about?.email && <div className="apps-about-dialog__row"><span className="apps-about-dialog__label">{text.aboutEmail}</span><a href={`mailto:${about.email}`} className="apps-about-dialog__link" onClick={(e) => { e.preventDefault(); BrowserOpenURL(`mailto:${about.email}`); }}>{about.email}</a></div>}
                    {!about?.author && !about?.copyright && !sanitizedWebsite && !about?.email && <p className="apps-about-dialog__empty">{text.aboutNoInfo}</p>}
                </div>
                <div className="apps-about-dialog__footer">
                    <span className="apps-about-dialog__category">{app.category}</span>
                </div>
            </div>
        </div>
    );
};

function installRecordString(value: unknown) {
    return String(value || '').trim();
}

function installRecordStringList(value: unknown): string[] {
    return Array.isArray(value) ? value.map((item) => installRecordString(item)).filter(Boolean) : [];
}

function resultCoverageEvidenceSummary(coverage: Record<string, unknown> | undefined, text: typeof labels.zh): { value: string; state: DataSrvRegistrationState } | null {
    if (!coverage || typeof coverage !== 'object') return null;
    const primary = installRecordString(coverage.primary || coverage.primary_result);
    const covered = installRecordStringList(coverage.coveredTypes || coverage.covered_types);
    const coveredCount = appEvidenceNumber(coverage.coveredCount, coverage.covered_count);
    const missing = installRecordStringList(coverage.missingTypes || coverage.missing_types);
    const ok = appEvidenceBool(coverage.ok, coverage.covered);
    const shownCoveredCount = covered.length || coveredCount || 0;
    const parts = [
        primary,
        shownCoveredCount > 0 ? `${text.resultCovered}: ${shownCoveredCount}` : '',
        missing.length ? `${text.resultMissing}: ${missing.length}` : '',
        ok === true && shownCoveredCount === 0 && missing.length === 0 ? 'ok' : '',
    ].filter(Boolean);
    if (parts.length === 0 && ok === undefined) return null;
    const state: DataSrvRegistrationState = ok === false || missing.length > 0 ? 'failed' : ok === true || shownCoveredCount > 0 ? 'ready' : 'partial';
    return { value: parts.join(' · ') || (ok ? 'ok' : 'pending'), state };
}

function installRecordDependencyDiagnostics(record: BackendAppInstallRecord): { value: string; state: DataSrvRegistrationState } | null {
    const dependencyVerification = record.dependency_verification || {};
    const verificationDependencies = parseBackendAppInstallDependencies(dependencyVerification.dependencies);
    const dependencies = verificationDependencies.length > 0 ? verificationDependencies : record.dependencies || [];
    const diagnosticRows = dependencies.map((dep) => {
        const parts = [
            dep.id,
            ...backendDependencyDiagnosticMeta(dep),
            ...backendDependencyDiagnosticItems(dep),
        ].filter(Boolean);
        return parts.length > 1 ? parts.join(' · ') : '';
    }).filter(Boolean);
    if (diagnosticRows.length === 0) return null;
    const hasFailure = dependencies.some((dep) => {
        const preflightStatus = String(dep.preflight_status || '').trim();
        const preflightCode = String(dep.preflight_code || '').trim();
        const integrityStatus = String(dep.integrity_status || '').trim();
        const integrityCode = String(dep.integrity_code || '').trim();
        return isBlockingBackendDependency(dep)
            || !!dep.install_error_code
            || dependencyDiagnosticStatusFailed(preflightStatus)
            || dependencyDiagnosticCodeFailed(preflightCode)
            || dependencyDiagnosticStatusFailed(integrityStatus)
            || dependencyDiagnosticCodeFailed(integrityCode);
    });
    const shown = diagnosticRows.slice(0, 2);
    if (diagnosticRows.length > shown.length) shown.push(`+${diagnosticRows.length - shown.length}`);
    return { value: shown.join(' | '), state: hasFailure ? 'failed' : 'ready' };
}

function installRecordEvidenceItems(record: BackendAppInstallRecord, text: typeof labels.zh) {
    const items: Array<{ label: string; value: string; state?: DataSrvRegistrationState }> = [];
    const submission = record.submission || {};
    const reviewEvidence = record.review_evidence || {};
    const hubStatus = installRecordString(submission.status || submission.review_status || submission.reviewStatus || reviewEvidence.status || reviewEvidence.review_status || reviewEvidence.reviewStatus);
    const hubCapabilityID = installRecordString(submission.capability_id || submission.capabilityID || submission.hub_capability_id || submission.market_capability_id || submission.marketCapabilityID);
    const hubVersionKey = installRecordString(submission.version_key || submission.versionKey);
    const hubSubmissionID = installRecordString(submission.submission_id || submission.submissionID);
    const hubValue = [hubStatus, hubCapabilityID, hubVersionKey || hubSubmissionID].filter(Boolean).join(' · ');
    if (hubValue) items.push({ label: text.marketSource, value: hubValue, state: publishReviewEvidenceState(hubStatus || hubValue) });
    const packageEvidence = appEvidenceRecord(record.package);
    const packageSignature = appEvidenceRecord(record.hub_package_signature)
        || appEvidenceRecord(submission.package_signature)
        || appEvidenceRecord(submission.packageSignature)
        || appEvidenceRecord(packageEvidence?.package_signature)
        || appEvidenceRecord(packageEvidence?.packageSignature);
    const packageSignatureAlgorithm = installRecordString(record.hub_package_signature_algorithm || packageSignature?.algorithm);
    const packageSignatureFingerprint = installRecordString(
        record.hub_package_signature_fingerprint
            || packageSignature?.public_key_fingerprint
            || packageSignature?.publicKeyFingerprint
            || packageSignature?.key_fingerprint
            || packageSignature?.keyFingerprint
            || packageSignature?.fingerprint,
    );
    const packageSignatureSignedBy = installRecordString(record.hub_package_signature_signed_by || packageSignature?.signed_by || packageSignature?.signedBy);
    const packageSignatureSignedAt = installRecordString(record.hub_package_signature_signed_at || packageSignature?.signed_at || packageSignature?.signedAt);
    const packageSignatureValue = [
        packageSignatureAlgorithm,
        packageSignatureFingerprint,
        packageSignatureSignedBy,
        packageSignatureSignedAt,
    ].filter(Boolean).join(' · ');
    if (packageSignature || packageSignatureValue) {
        items.push({ label: text.packageSignature, value: packageSignatureValue || 'signed', state: 'ready' });
    }
    const layout = record.workspace_layout || {};
    const layoutValue = [layout.entry, layout.template, layout.density].map(installRecordString).filter(Boolean).join(' · ');
    if (layoutValue) items.push({ label: text.workspaceLayout, value: layoutValue });
    const contract = record.result_contract || {};
    const resultTypes = installRecordStringList(contract.types);
    const resultValue = [
        installRecordString(contract.primary),
        resultTypes.length ? `${resultTypes.length} types` : '',
    ].filter(Boolean).join(' · ');
    if (resultValue) items.push({ label: text.resultContract, value: resultValue });
    const evidence = record.test_evidence || {};
    const protocol = evidence.testProtocol && typeof evidence.testProtocol === 'object'
        ? evidence.testProtocol as Record<string, unknown>
        : evidence.test_protocol && typeof evidence.test_protocol === 'object'
            ? evidence.test_protocol as Record<string, unknown>
            : {};
    const evidenceValue = [
        installRecordString(evidence.runId || evidence.run_id),
        installRecordString(evidence.testProtocolFingerprint || evidence.test_protocol_fingerprint || protocol.fingerprint || protocol.hash),
    ].filter(Boolean).join(' · ');
    if (evidenceValue) items.push({ label: text.testEvidence, value: evidenceValue });
    const coverage = evidence.resultCoverage && typeof evidence.resultCoverage === 'object'
        ? evidence.resultCoverage as Record<string, unknown>
        : evidence.result_coverage && typeof evidence.result_coverage === 'object'
            ? evidence.result_coverage as Record<string, unknown>
            : undefined;
    const coverageSummary = resultCoverageEvidenceSummary(coverage, text);
    if (coverageSummary) items.push({ label: text.resultCoverage, value: coverageSummary.value, state: coverageSummary.state });
    const approvalInstance = evidence.approvalInstance && typeof evidence.approvalInstance === 'object'
        ? evidence.approvalInstance as Record<string, unknown>
        : evidence.approval_instance && typeof evidence.approval_instance === 'object'
            ? evidence.approval_instance as Record<string, unknown>
            : {};
    const approvalInstanceValue = [
        installRecordString(approvalInstance.instanceId || approvalInstance.instance_id || approvalInstance.workflowInstanceID || approvalInstance.workflow_instance_id || approvalInstance.approvalID || approvalInstance.approval_id || approvalInstance.recordApprovalID || approvalInstance.record_approval_id),
        installRecordString(approvalInstance.status),
        installRecordString(approvalInstance.currentNode || approvalInstance.current_node),
    ].filter(Boolean).join(' · ');
    if (approvalInstanceValue) items.push({ label: text.approvalInstanceId, value: approvalInstanceValue });
    const outputCount = Array.isArray(evidence.outputs)
        ? evidence.outputs.length
        : Array.isArray((evidence as Record<string, unknown>).output_blocks)
            ? ((evidence as Record<string, unknown>).output_blocks as unknown[]).length
            : 0;
    const artifactCount = Array.isArray(evidence.artifacts)
        ? evidence.artifacts.length
        : Array.isArray((evidence as Record<string, unknown>).artifact_files)
            ? ((evidence as Record<string, unknown>).artifact_files as unknown[]).length
            : 0;
    if (outputCount > 0 || artifactCount > 0) {
        items.push({ label: text.approvalResultPackage, value: `${text.output}: ${outputCount} · ${text.runArtifacts}: ${artifactCount}` });
    }
    const resultPayload = evidence.resultPayload && typeof evidence.resultPayload === 'object'
        ? evidence.resultPayload as Record<string, unknown>
        : evidence.result_payload && typeof evidence.result_payload === 'object'
            ? evidence.result_payload as Record<string, unknown>
            : {};
    const resultPayloadKeys = Object.keys(resultPayload).filter(Boolean);
    if (resultPayloadKeys.length > 0) {
        items.push({ label: text.approvalOutputData, value: resultPayloadKeys.slice(0, 4).join(', ') + (resultPayloadKeys.length > 4 ? ` +${resultPayloadKeys.length - 4}` : '') });
    }
    const dependencyVerification = record.dependency_verification || {};
    const verificationDependencies = parseBackendAppInstallDependencies(dependencyVerification.dependencies);
    const dependencyCount = appEvidenceNumber(dependencyVerification.dependencyCount, dependencyVerification.dependency_count)
        ?? (verificationDependencies.length || (record.dependencies || []).length);
    const blockingCountFromRecord = (record.dependencies || []).filter(isBlockingBackendDependency).length;
    const blockingCountFromVerification = verificationDependencies.filter(isBlockingBackendDependency).length;
    const hasBlockingDependency = appEvidenceBool(dependencyVerification.hasBlockingDependency, dependencyVerification.has_blocking_dependency)
        || appEvidenceBool(dependencyVerification.hasMissingRequired, dependencyVerification.has_missing_required)
        || record.has_blocking_dependency
        || record.has_missing_required;
    const blockingCount = blockingCountFromVerification || blockingCountFromRecord || (hasBlockingDependency ? 1 : 0);
    if (installRecordString(dependencyVerification.schema) || dependencyCount > 0 || blockingCount > 0) {
        items.push({ label: text.dependencyVerification, value: `${text.skillDependencies}: ${dependencyCount} · ${text.missingDependencyCount}: ${blockingCount}` });
    }
    const dependencyDiagnostics = installRecordDependencyDiagnostics(record);
    if (dependencyDiagnostics) {
        items.push({ label: text.dependencyDiagnostics, value: dependencyDiagnostics.value, state: dependencyDiagnostics.state });
    }
    const dataSrvStatus = dataSrvRegistrationState(record.datasrv_registration);
    const dataSrvValue = dataSrvRegistrationSummary(record.datasrv_registration, text);
    if (dataSrvValue) items.push({ label: 'DataSrv', value: dataSrvValue, state: dataSrvStatus?.state });
    return items;
}

const InstallRecordEvidenceSnapshot = ({ record, text }: { record: BackendAppInstallRecord; text: typeof labels.zh }) => {
		const items = installRecordEvidenceItems(record, text);
		if (items.length === 0) return null;
    return (
        <div className="apps-install-version-snapshot apps-install-evidence-snapshot" role="list" aria-label={text.testEvidence}>
            {items.map((item, index) => (
                <span role="listitem" data-state={item.state} key={item.label + ':' + item.value + ':' + index}>
                    <strong>{item.label}</strong>
                    <em>{item.value}</em>
                </span>
            ))}
        </div>
		);
};

const RuntimeStatusSection = ({ runState, runtimeStatusState, runtimeStatusMessage, runtimeBusinessError, runtimeDependencyBlocked, showRuntimeDependencyDetails, visibleRuntimeDependencyDetails, canInstallRuntimeDependencies, dependencyRepairState, installRuntimeDependenciesAndRun, isApproval, runtimeWorkflowContractState, runtimeWorkflowContract, runtimeWorkflowContractIssue, app, runtimeVisibleDependencyPlan, runtimeDependencyPanelState, runtimeAppID, isTool, skillRunStatus, text }: {
    runState: 'idle' | 'running' | 'done' | 'error' | 'cancelled';
    runtimeStatusState: string;
    runtimeStatusMessage: string;
    runtimeBusinessError: any;
    runtimeDependencyBlocked: boolean;
    showRuntimeDependencyDetails: boolean;
    visibleRuntimeDependencyDetails: any[];
    canInstallRuntimeDependencies: boolean;
    dependencyRepairState: string;
    installRuntimeDependenciesAndRun: () => Promise<void>;
    isApproval: boolean;
    runtimeWorkflowContractState: string;
    runtimeWorkflowContract: any;
    runtimeWorkflowContractIssue: any;
    app: AppEntry;
    runtimeVisibleDependencyPlan: BackendAppInstallPlan | null;
    runtimeDependencyPanelState: 'idle' | 'loading' | 'repairing' | 'ready' | 'error';
    runtimeAppID: string;
    isTool: boolean;
    skillRunStatus: SkillRunStatusView | null;
    text: typeof labels.zh;
}) => {
    // Auto-expand only when there are real blocking issues that require user attention.
    // Ready/healthy states keep details collapsed to reduce visual noise.
    const hasRealBlockingIssue = runtimeDependencyBlocked || !!runtimeBusinessError || (isApproval && runtimeWorkflowContractState === 'blocked');
    const shouldAutoExpand = hasRealBlockingIssue;
    const [detailOpen, setDetailOpen] = useState(shouldAutoExpand);
    const [execDetailOpen, setExecDetailOpen] = useState(false);

    // Sync auto-expand when blocking/evidence state changes.
    useEffect(() => {
        if (shouldAutoExpand) setDetailOpen(true);
    }, [shouldAutoExpand]);

    // Detail content exists when there are governance/dependency panels to show (only in idle/pre-run states)
    const isPreRun = runState !== 'running' && runState !== 'done';
    const hasReadyDependencyDetails = visibleRuntimeDependencyDetails.length > 0 && !runtimeDependencyBlocked && !runtimeBusinessError && dependencyRepairState !== 'installing' && runState !== 'error';
    const hasDetailContent = isPreRun && (!!app.installEvidence || !!runtimeVisibleDependencyPlan || hasReadyDependencyDetails);

    // Execution detail stays collapsed by default so routine runs keep the status row compact.
    const hasExecDetail = isTool && runState !== 'idle' && (skillRunStatus?.steps?.length || 0) > 0;

    return (
        <>
            <div className="apps-runtime-section__title">{text.runtimeStatus}</div>
            <div className={`apps-result-panel${showRuntimeDependencyDetails || runtimeBusinessError ? ' apps-result-panel--stacked' : ''}`} data-state={runtimeStatusState}>
                <div className="apps-result-panel__summary">
                    <span className="apps-result-panel__dot" aria-hidden="true" />
                    <span className="apps-result-panel__message">{runtimeStatusMessage}</span>
                    <span className="apps-result-panel__tools">
                        {hasExecDetail && (
                            <button className="apps-dependency-verification__detail-toggle" type="button" onClick={() => setExecDetailOpen((prev) => !prev)}>
                                {execDetailOpen ? text.hideExecDetail : text.showExecDetail}
                            </button>
                        )}
                        {hasDetailContent && (
                            <button className="apps-dependency-verification__detail-toggle" type="button" onClick={() => setDetailOpen((prev) => !prev)}>
                                {detailOpen ? text.installErrorHideDetail : text.installErrorShowDetail}
                            </button>
                        )}
                    </span>
                </div>
                {runtimeStatusState === 'running' && (
                    <div className="apps-result-progress" role="progressbar" aria-label={runtimeStatusMessage} aria-valuetext={runtimeStatusMessage}>
                        <span />
                    </div>
                )}
                {runtimeBusinessError && <StructuredBusinessErrorDetails error={runtimeBusinessError} text={text} />}
                {isApproval && runtimeWorkflowContractState === 'blocked' && <WorkflowContractSummary contract={runtimeWorkflowContract} state={runtimeWorkflowContractState} issue={runtimeWorkflowContractIssue} text={text} />}
                {showRuntimeDependencyDetails && isPreRun && <InstallRecordDependencies dependencies={visibleRuntimeDependencyDetails} text={text} />}
                {canInstallRuntimeDependencies && isPreRun && (
                    <button className="apps-secondary-button apps-result-panel__action" type="button" disabled={dependencyRepairState === 'installing'} onClick={() => void installRuntimeDependenciesAndRun()}>
                        {dependencyRepairState === 'installing' ? text.installingDependencies : text.installDependenciesAndRun}
                    </button>
                )}
            </div>
            {detailOpen && isPreRun && (
                <>
                    {hasReadyDependencyDetails && <InstallRecordDependencies dependencies={visibleRuntimeDependencyDetails} text={text} />}
                    <RuntimeInstallGovernancePanel record={app.installEvidence} text={text} />
                    {runtimeVisibleDependencyPlan && (
                        <DependencyVerificationPanel plan={runtimeVisibleDependencyPlan} state={runtimeDependencyPanelState} selectedAppIDs={[runtimeAppID]} text={text} defaultOpen={false} />
                    )}
                </>
            )}
            {isTool && execDetailOpen && (
                <>
                    <SkillRunEvidence status={skillRunStatus} runState={runState} text={text} />
                    {skillRunOutputBlocks(skillRunStatus).filter((block) => skillRunOutputBlockText(block)).map((block, index) => {
                        const blockKind = String(block.kind || 'text').trim().toLowerCase();
                        const blockText = skillRunOutputBlockText(block);
                        const blockTitle = String(block.title || (blockKind === 'error' ? 'Error' : text.outputContent)).trim();
                        return (
                            <div className="apps-output-block" data-kind={blockKind} key={block.id || `detail-${blockKind}-${index}`}>
                                <span>{blockTitle}</span>
                                {blockText && <pre>{renderOutputTextWithPaths(blockText)}</pre>}
                            </div>
                        );
                    })}
                </>
            )}
        </>
    );
};

const RuntimeInstallGovernancePanel = ({ record, text }: { record?: BackendAppInstallRecord; text: typeof labels.zh }) => {
    if (!record) return null;
    const items = installRecordEvidenceItems(record, text);
    if (items.length === 0) return null;
    return (
        <section className="apps-runtime-governance" aria-label={text.installGovernance}>
            <div className="apps-runtime-governance__head">
                <strong>{text.installGovernance}</strong>
                <span>{text.runtimeEvidence}</span>
            </div>
            <dl className="apps-runtime-governance__grid">
                {items.map((item, index) => (
                    <div data-state={item.state} key={item.label + ':' + item.value + ':' + index}>
                        <dt>{item.label}</dt>
                        <dd>{item.value}</dd>
                    </div>
                ))}
            </dl>
        </section>
    );
};

type PublishReviewEvidenceItem = { label: string; value: string; state?: DataSrvRegistrationState };

function publishReviewEvidenceState(status: string): DataSrvRegistrationState {
    const normalized = status.trim().toLowerCase();
    if (['approved', 'passed', 'pass', 'ready', 'ok', 'success', 'synced', 'complete', 'completed'].includes(normalized)) return 'ready';
    if (['rejected', 'failed', 'error', 'blocked', 'missing', 'deny', 'denied'].includes(normalized)) return 'failed';
    if (['pending', 'running', 'submitted', 'partial', 'in_progress', 'in-progress', 'attention', 'requires_input', 'requires-input'].includes(normalized)) return 'partial';
    return 'skipped';
}

function publishReviewEvidenceItems(evidence: Record<string, unknown> | undefined, text: typeof labels.zh): PublishReviewEvidenceItem[] {
    if (!evidence) return [];
    let records = Object.values(evidence)
        .map((value) => appEvidenceRecord(value))
        .filter(Boolean) as Record<string, unknown>[];
    if (records.length === 0) records = [evidence];
    const items: PublishReviewEvidenceItem[] = [];
    records.forEach((record, index) => {
        const suffix = records.length > 1 ? ` #${index + 1}` : '';
        const approvalStatus = appEvidenceString(record.approval_status, record.approvalStatus, record.status);
        if (appEvidenceBool(record.has_approval_instance, record.hasApprovalInstance) || approvalStatus) {
            const approvalValue = [approvalStatus, appEvidenceString(record.current_node, record.currentNode)].filter(Boolean).join(' · ') || 'ready';
            items.push({ label: `${text.approval}${suffix}`, value: approvalValue, state: publishReviewEvidenceState(approvalStatus || approvalValue) });
        }
        const progressCount = appEvidenceNumber(record.progress_count, record.progressCount);
        if (progressCount !== undefined) {
            items.push({ label: `${text.progress}${suffix}`, value: String(progressCount), state: progressCount > 0 ? 'partial' : 'skipped' });
        }
        const dependencyCount = appEvidenceNumber(record.dependency_count, record.dependencyCount);
        const hasDependencyVerification = appEvidenceBool(record.has_dependency_verification, record.hasDependencyVerification);
        const hasBlockingDependency = appEvidenceBool(record.has_blocking_dependency, record.hasBlockingDependency);
        if (hasDependencyVerification || dependencyCount !== undefined) {
            items.push({ label: `${text.skillDependencies}${suffix}`, value: String(dependencyCount ?? 0), state: hasBlockingDependency ? 'failed' : 'ready' });
        }
        const layoutRecord = appEvidenceRecord(record.workspace_layout) || appEvidenceRecord(record.workspaceLayout);
        const layoutStudio = appEvidenceRecord(layoutRecord?.studio);
        const layout = appEvidenceString(record.workspace_template, record.workspaceTemplate, layoutRecord?.template, layoutRecord?.entry, layoutRecord?.density);
        const layoutSaved = appEvidenceBool(record.workspace_saved_in_manifest, record.workspaceSavedInManifest, layoutRecord?.studio_saved_in_manifest, layoutRecord?.studioSavedInManifest, layoutStudio?.savedInManifest, layoutStudio?.saved_in_manifest);
        const layoutUpdatedBy = appEvidenceString(record.workspace_updated_by, record.workspaceUpdatedBy, layoutRecord?.studio_updated_by, layoutRecord?.studioUpdatedBy, layoutStudio?.updatedBy, layoutStudio?.updated_by);
        if (appEvidenceBool(record.has_workspace_layout, record.hasWorkspaceLayout) || layout || layoutSaved) {
            const layoutValue = [layout || 'ready', layoutSaved ? 'saved' : '', layoutUpdatedBy].filter(Boolean).join(' · ');
            items.push({ label: `${text.workspaceLayout}${suffix}`, value: layoutValue, state: 'ready' });
        }
        const dataSrvStatus = appEvidenceString(record.datasrv_registration_status, record.dataSrvRegistrationStatus, record.datasrvRegistrationStatus);
        if (dataSrvStatus) {
            items.push({ label: `DataSrv${suffix}`, value: dataSrvStatus, state: publishReviewEvidenceState(dataSrvStatus) });
        }
        const workflowContract = appEvidenceRecord(record.workflow_contract) || appEvidenceRecord(record.workflowContract);
        const workflowVersion = appEvidenceString(record.workflow_contract_version, record.workflowContractVersion, workflowContract?.version, workflowContract?.schema);
        if (appEvidenceBool(record.has_workflow_contract, record.hasWorkflowContract) || workflowVersion) {
            items.push({ label: `${text.workflow}${suffix}`, value: workflowVersion || 'ready', state: 'ready' });
        }
        const resultPrimary = appEvidenceString(record.result_contract_primary, record.resultContractPrimary);
        const resultTypeCount = appEvidenceNumber(record.result_contract_type_count, record.resultContractTypeCount);
        if (appEvidenceBool(record.has_result_contract, record.hasResultContract) || resultPrimary || resultTypeCount !== undefined) {
            const value = [resultPrimary || 'ready', resultTypeCount !== undefined ? `${resultTypeCount} types` : ''].filter(Boolean).join(' · ');
            items.push({ label: `${text.resultContract}${suffix}`, value, state: 'ready' });
        }
        const runID = appEvidenceString(record.run_id, record.runId);
        const protocolFingerprint = appEvidenceString(record.test_protocol_fingerprint, record.testProtocolFingerprint);
        if (appEvidenceBool(record.has_test_protocol, record.hasTestProtocol) || protocolFingerprint || runID) {
            items.push({ label: `${text.testEvidence}${suffix}`, value: [runID, protocolFingerprint || (!runID ? 'ready' : '')].filter(Boolean).join(' · '), state: 'ready' });
        }
        const coverageSummary = resultCoverageEvidenceSummary({
            ok: firstAppEvidenceValue(record.result_coverage_ok, record.resultCoverageOK),
            primary: firstAppEvidenceValue(record.result_coverage_primary, record.resultCoveragePrimary),
            covered_count: firstAppEvidenceValue(record.result_coverage_covered_count, record.resultCoverageCoveredCount),
            missingTypes: (() => {
                const count = appEvidenceNumber(record.result_coverage_missing_count, record.resultCoverageMissingCount);
                return count && count > 0 ? Array.from({ length: count }, (_, index) => String(index + 1)) : [];
            })(),
        }, text);
        if (coverageSummary) items.push({ label: `${text.resultCoverage}${suffix}`, value: coverageSummary.value, state: coverageSummary.state });
        const outputCount = appEvidenceNumber(record.output_count, record.outputCount);
        const artifactCount = appEvidenceNumber(record.artifact_count, record.artifactCount);
        if (outputCount !== undefined || artifactCount !== undefined) {
            items.push({ label: `${text.approvalResultPackage}${suffix}`, value: `${text.output}: ${outputCount ?? 0} · ${text.runArtifacts}: ${artifactCount ?? 0}`, state: 'ready' });
        }
    });
    return items;
}

const PublishReviewEvidenceStrip = ({ evidence, text }: { evidence?: Record<string, unknown>; text: typeof labels.zh }) => {
    const items = publishReviewEvidenceItems(evidence, text);
    if (items.length === 0) return null;
    return (
        <div className="apps-install-version-snapshot apps-install-evidence-snapshot apps-review-evidence-strip" role="list" aria-label={text.reviewEvidence}>
            {items.map((item, index) => (
                <span role="listitem" data-state={item.state} key={item.label + ':' + item.value + ':' + index}>
                    <strong>{item.label}</strong>
                    <em>{item.value}</em>
                </span>
            ))}
        </div>
    );
};
function installRecordVersionSnapshotForApp(installAudit: BackendAppInstallRecord | null | undefined, appID?: string): BackendAppInstallVersionSnapshot | undefined {
    const versions = installAudit?.app_versions;
    if (appID && versions && typeof versions === 'object') {
        for (const id of appInstallIdentityKeys(appID)) {
            const snapshot = versions[id];
            if (snapshot) return snapshot;
        }
    }
    return installAudit?.version_snapshot;
}

function installEvidenceRecordForApp(installAudit: BackendAppInstallRecord | null | undefined, appID?: string): BackendAppInstallRecord | undefined {
    const evidenceMap = installAudit?.install_evidence;
    let evidence: unknown;
    if (appID && evidenceMap && typeof evidenceMap === 'object') {
        for (const id of appInstallIdentityKeys(appID)) {
            evidence = (evidenceMap as Record<string, any>)[id];
            if (evidence) break;
        }
    }
    const record = evidence && typeof evidence === 'object' ? evidence as Record<string, any> : installAudit as Record<string, any> | undefined;
    if (!record || typeof record !== 'object') return undefined;
    const rawDependencyVerification = record.dependency_verification && typeof record.dependency_verification === 'object'
        ? record.dependency_verification as Record<string, any>
        : record.dependencyVerification && typeof record.dependencyVerification === 'object'
            ? record.dependencyVerification as Record<string, any>
            : undefined;
    const dependencies = parseBackendAppInstallDependencies(record.dependencies).filter((dep) => !appID || backendDependencyMatchesAppIDs(dep, appInstallIdentityKeys(appID)));
    const verificationDependencies = parseBackendAppInstallDependencies(rawDependencyVerification?.dependencies).filter((dep) => !appID || backendDependencyMatchesAppIDs(dep, appInstallIdentityKeys(appID)));
    const appIdentityKeys = appID ? new Set(appInstallIdentityKeys(appID)) : null;
    const apps = (record.apps || installAudit?.apps || []).filter((item: { id?: string; name?: string; kind?: string; schema?: string }) => {
        if (!appIdentityKeys) return true;
        return appInstallIdentityKeys(item.id || '').some((key) => appIdentityKeys.has(key));
    });
    const dependencyVerification = rawDependencyVerification
        ? {
            ...rawDependencyVerification,
            dependencies: verificationDependencies.length > 0 ? verificationDependencies : rawDependencyVerification.dependencies,
            dependency_count: verificationDependencies.length > 0 ? verificationDependencies.length : rawDependencyVerification.dependency_count,
        }
        : undefined;
    const hasEvidence = [
        record.version_snapshot,
        record.workspace_layout,
        record.result_contract,
        record.workflow_mapping,
        record.workflow_contract,
        record.test_evidence,
        record.review_evidence,
        record.submission,
        record.hub_package_signature,
        record.hub_package_signature_algorithm,
        record.hub_package_signature_fingerprint,
        dependencyVerification,
        dependencies.length ? dependencies : undefined,
        record.has_missing_required,
        record.has_blocking_dependency,
    ].some(Boolean);
    if (!hasEvidence) return undefined;
    return {
        schema: installRecordString(record.schema || installAudit?.schema) || 'maclaw.app.install_record.v1',
        package_sha: installRecordString(record.package_sha || record.package_sha256 || installAudit?.package_sha || installAudit?.package_sha256),
        package_sha256: installRecordString(record.package_sha256 || record.package_sha || installAudit?.package_sha256 || installAudit?.package_sha),
        hub_package_signature: record.hub_package_signature && typeof record.hub_package_signature === 'object'
            ? record.hub_package_signature as Record<string, unknown>
            : installAudit?.hub_package_signature,
        hub_package_signature_algorithm: installRecordString(record.hub_package_signature_algorithm || installAudit?.hub_package_signature_algorithm),
        hub_package_signature_fingerprint: installRecordString(record.hub_package_signature_fingerprint || installAudit?.hub_package_signature_fingerprint),
        hub_package_signature_signed_at: installRecordString(record.hub_package_signature_signed_at || installAudit?.hub_package_signature_signed_at),
        hub_package_signature_signed_by: installRecordString(record.hub_package_signature_signed_by || installAudit?.hub_package_signature_signed_by),
        source: installRecordString(record.source || installAudit?.source),
        installed_at: installRecordString(record.installed_at || installAudit?.installed_at),
        app_count: apps.length || record.app_count || installAudit?.app_count,
        apps,
        version_snapshot: normalizeVersionSnapshot(record.version_snapshot),
        workspace_layout: record.workspace_layout && typeof record.workspace_layout === 'object' ? record.workspace_layout : undefined,
        result_contract: record.result_contract && typeof record.result_contract === 'object' ? record.result_contract : undefined,
        workflow_mapping: record.workflow_mapping && typeof record.workflow_mapping === 'object' ? record.workflow_mapping : undefined,
        workflow_contract: record.workflow_contract && typeof record.workflow_contract === 'object' ? record.workflow_contract : undefined,
        test_evidence: record.test_evidence && typeof record.test_evidence === 'object' ? record.test_evidence : undefined,
        review_evidence: record.review_evidence && typeof record.review_evidence === 'object' ? record.review_evidence : undefined,
        submission: record.submission && typeof record.submission === 'object' ? record.submission : undefined,
        dependency_verification: dependencyVerification,
        dependencies,
        has_missing_required: !!record.has_missing_required,
        has_blocking_dependency: !!record.has_blocking_dependency,
        datasrv_registration: (record.datasrv_registration && typeof record.datasrv_registration === 'object' ? record.datasrv_registration as BackendAppDataSrvRegistration : undefined) || installAudit?.datasrv_registration,
    };
}

function installedAppWithInstallEvidence(app: AppEntry, installAudit: BackendAppInstallRecord | null | undefined): AppEntry {
    const installEvidence = installEvidenceRecordForApp(installAudit, app.id);
    const versionSnapshot = installEvidence?.version_snapshot || installRecordVersionSnapshotForApp(installAudit, app.id);
    const workflowContract = normalizeAppWorkflowContract(installEvidence?.workflow_contract, app.kind, workflowContractForApp(app));
    const importedRunEvidence = installEvidence?.test_evidence
        ? dataSrvInstalledRunEvidence({
            test_evidence: installEvidence.test_evidence,
            dependency_verification: installEvidence.dependency_verification,
            dependencies: installEvidence.dependencies,
        }, app.id)
        : undefined;
    return {
        ...app,
        versionSnapshot: versionSnapshot || app.versionSnapshot,
        installEvidence: installEvidence || app.installEvidence,
        workflowContract: workflowContract || app.workflowContract,
        importedRunEvidence: importedRunEvidence || app.importedRunEvidence,
    };
}

function MarketInstallFeedbackMessage({ feedback, text }: { feedback: { state: string; message: string; plan?: BackendAppInstallPlan | null }; text: typeof labels.zh }) {
    if (feedback.state !== 'error') {
        return <small className="apps-market-row__feedback" data-state={feedback.state}>{feedback.message}</small>;
    }
    const friendly = marketInstallFriendlyError(feedback.message, feedback.plan, text);
    const rawMessage = feedback.message || '';
    // When plan is available, DependencyVerificationPanel provides structured detail (collapsible).
    // When plan is absent (catch block errors), raw message is the only detail source; show it inline.
    const showRawInline = !feedback.plan && rawMessage && friendly !== rawMessage;
    return (
        <div className="apps-market-row__feedback-block" data-state="error">
            <small className="apps-market-row__feedback" data-state="error">{friendly}</small>
            {showRawInline && (
                <small className="apps-market-row__feedback-raw">{rawMessage}</small>
            )}
        </div>
    );
}

const DependencyVerificationPanel = ({ plan, state, error, selectedAppIDs, text, defaultOpen = true }: { plan?: BackendAppInstallPlan | null; state: 'idle' | 'loading' | 'repairing' | 'ready' | 'error'; error?: string; selectedAppIDs?: string[]; text: typeof labels.zh; defaultOpen?: boolean }) => {
    const [detailOpen, setDetailOpen] = useState(defaultOpen);
    if (state === 'idle' && !plan) return null;
    const appIDs = selectedAppIDs || [];
    const dependencies = appIDs.length > 0
        ? (plan?.dependencies || []).filter((dep) => backendDependencyMatchesAppIDs(dep, appIDs))
        : plan?.dependencies || [];
    const blockingCount = dependencies.filter(isBlockingBackendDependency).length;
    const dependencyCount = dependencies.length;
    const workflowIssues = workflowContractIssuesForAppIDs(plan, appIDs);
    const governanceIssues = governanceReviewIssuesForAppIDs(plan, appIDs);
    const hasWorkflowIssue = workflowContractHasIssueForAppIDs(plan, appIDs);
    const hasGovernanceIssue = governanceReviewHasIssueForAppIDs(plan, appIDs);
    const hasPlanWideBlockingDependency = appIDs.length === 0 && (!!plan?.has_blocking_dependency || !!plan?.has_missing_required);
    const hasBlockingDependency = blockingCount > 0 || hasPlanWideBlockingDependency || hasWorkflowIssue || hasGovernanceIssue;
    const status = state === 'loading' || state === 'repairing'
        ? text.dependencyPlanLoading
        : state === 'error'
            ? [text.dependencyPlanError, error].filter(Boolean).join(': ')
            : hasBlockingDependency
                ? text.dependencyVerificationBlocked
                : text.dependencyVerificationReady;
    const hasDetailContent = workflowIssues.length > 0 || (hasWorkflowIssue && workflowIssues.length === 0) || governanceIssues.length > 0 || (hasGovernanceIssue && governanceIssues.length === 0) || dependencyCount > 0;
    return (
        <div className="apps-dependency-verification" data-state={state === 'error' || hasBlockingDependency ? 'blocked' : state} role={state === 'error' || hasBlockingDependency ? 'alert' : 'group'} aria-label={text.dependencyVerification}>
            <div className="apps-dependency-verification__head">
                <strong>{text.dependencyVerification}</strong>
                <span>{status}</span>
                {state === 'ready' && <em>{text.skillDependencies}: {dependencyCount} · {text.missingDependencyCount}: {blockingCount}{hasWorkflowIssue ? ` · ${text.workflowContract}: ${workflowIssues.length || 1}` : ''}{hasGovernanceIssue ? ` · ${text.reviewIssues}: ${governanceIssues.length || 1}` : ''}</em>}
                {hasDetailContent && state !== 'loading' && state !== 'repairing' && (
                    <button className="apps-dependency-verification__detail-toggle" type="button" onClick={() => setDetailOpen((prev) => !prev)}>
                        {detailOpen ? text.installErrorHideDetail : text.installErrorShowDetail}
                    </button>
                )}
            </div>
            {detailOpen && state !== 'loading' && state !== 'repairing' && (
                <>
                    {workflowIssues.length > 0 && (
                        <div className="apps-dependency-verification__issues" role="list" aria-label={text.workflowContract}>
                            {workflowIssues.map((issue, index) => {
                                const details = workflowContractIssueDetails(issue, text);
                                return (
                                    <div className="apps-dependency-verification__issue" role="listitem" key={`${issue.path || 'workflow'}-${index}`}>
                                        <span>{reviewIssueSummary(issue)}</span>
                                        {details.length > 0 && (
                                            <small className="apps-dependency-verification__issue-details">
                                                {details.map((item) => <em key={item.label + item.value}><strong>{item.label}</strong>{item.value}</em>)}
                                            </small>
                                        )}
                                    </div>
                                );
                            })}
                        </div>
                    )}
                    {hasWorkflowIssue && workflowIssues.length === 0 && (
                        <div className="apps-dependency-verification__issues" role="list" aria-label={text.workflowContract}>
                            <span role="listitem">{text.workflowContractBlocked}</span>
                        </div>
                    )}
                    {governanceIssues.length > 0 && (
                        <div className="apps-dependency-verification__issues" role="list" aria-label={text.reviewIssues}>
                            {governanceIssues.map((issue, index) => (
                                <span role="listitem" key={`${issue.path || 'governance'}-${index}`}>{reviewIssueSummary(issue)}</span>
                            ))}
                        </div>
                    )}
                    {hasGovernanceIssue && governanceIssues.length === 0 && (
                        <div className="apps-dependency-verification__issues" role="list" aria-label={text.reviewIssues}>
                            <span role="listitem">{text.reviewIssues}</span>
                        </div>
                    )}
                    {dependencyCount > 0 && <InstallRecordDependencies dependencies={dependencies} text={text} />}
                </>
            )}
        </div>
    );
};
const InstallRecordDependencies = ({ dependencies, text }: { dependencies?: BackendAppInstallDependency[]; text: typeof labels.zh }) => {
    const items = (dependencies || []).filter((dep) => dep.id);
    if (items.length === 0) return null;
    return (
        <div className="apps-install-record__deps" role="list" aria-label={text.skillDependencies}>
            {items.map((dep, index) => {
                const state = installRecordDependencyState(dep);
                const health = String(dep.health || '').trim();
                const installedStatus = String(dep.installed_status || '').trim();
                const diagnostics = backendDependencyDiagnosticItems(dep);
                const traceItems = backendDependencyTraceItems(dep, text);
                const meta = [
                    dep.kind || 'skill',
                    dep.source,
                    dep.version ? `v${dep.version}` : '',
                    backendDependencyInstallRef(dep) ? `ref:${backendDependencyInstallRef(dep)}` : '',
                    ...backendDependencyDiagnosticMeta(dep),
                    state !== 'ready' ? installedStatus || health : '',
                ].filter(Boolean).join(' · ');
                const summary = [backendDependencySummary(dep, text), ...diagnostics].filter(Boolean).join(' · ');
                return (
                    <div className="apps-install-record__dep" data-state={state} role="listitem" key={`${dep.id}-${dep.version || ''}-${index}`} title={summary}>
                        <span className="apps-install-record__dep-main">
                            <strong>{dep.id}</strong>
                            {meta && <span>{meta}</span>}
                            <em>{installRecordDependencyStatus(dep, text)}</em>
                        </span>
                        {diagnostics.length > 0 && (
                            <small className="apps-install-record__dep-diagnostics">
                                {diagnostics.map((item, itemIndex) => <span key={`${dep.id}-diagnostic-${itemIndex}`}>{item}</span>)}
                            </small>
                        )}
                        {traceItems.length > 0 && (
                            <dl className="apps-install-record__dep-trace" aria-label={text.dependencyInstallTrace}>
                                {traceItems.map((item) => (
                                    <div data-state={item.state} key={`${dep.id}-trace-${item.label}-${item.value}`}>
                                        <dt>{item.label}</dt>
                                        <dd>{item.value}</dd>
                                    </div>
                                ))}
                            </dl>
                        )}
                    </div>
                );
            })}
        </div>
    );
};

function formatInstallRecordTime(value?: string) {
    if (!value) return '-';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;
    return date.toLocaleString();
}
function appInstallIdentityKeys(appId: string) {
    const id = String(appId || '').trim();
    if (!id) return [];
    const keys = [id];
    if (id.startsWith('market-')) keys.push(id.slice('market-'.length));
    else keys.push(`market-${id}`);
    if (id.startsWith('datasrv-installed-')) keys.push(id.slice('datasrv-installed-'.length));
    else keys.push(`datasrv-installed-${id}`);
    return Array.from(new Set(keys));
}

const AppStudio = ({ apps, hiddenApps, lang, tab, setTab, onClose, onTogglePin, onUpdateApp, onDuplicateApp, onMoveApp, onToggleDisableApp, onRemoveApp, onRestoreApp, pendingEditAppId, onPendingEditConsumed, datasrvDiscovery, skillDiscovery, onOpenMISDataSettings, onOpenManual, onAddDiscoveredApp, onCreateApp, onInstallMarketApp, onEditApp, onInstallDependencies, onSyncHubAppGovernance }: {
	apps: AppEntry[];
	hiddenApps: AppEntry[];
	lang?: string;
	tab: StudioTab;
	setTab: (tab: StudioTab) => void;
	onClose: () => void;
	onTogglePin: (appId: string) => void;
	onUpdateApp: (appId: string, patch: Partial<AppEntry>) => void;
	onDuplicateApp: (appId: string) => void;
    onMoveApp: (appId: string, direction: AppMoveTarget) => void;
    onToggleDisableApp: (appId: string) => void;
    onRemoveApp: (appId: string) => void;
    onRestoreApp: (appId: string) => void;
    pendingEditAppId: string;
    onPendingEditConsumed: () => void;
    datasrvDiscovery: DataSrvDiscovery;
    skillDiscovery: SkillAppDiscovery;
    onOpenMISDataSettings?: () => void;
    onOpenManual?: () => void;
	onAddDiscoveredApp: (app: AppEntry) => void;
	onCreateApp: (app: AppEntry, options?: { keepStudioCreate?: boolean }) => void;
	onInstallMarketApp: (app: AppEntry) => void;
	onEditApp: (appId: string) => void;
	onInstallDependencies: (appId: string) => void;
	onSyncHubAppGovernance: (summaries: AppPackageSubmissionSummary[]) => void;
}) => {
	const text = isZh(lang) ? labels.zh : labels.en;
	const studioTabs: Array<{ id: StudioTab; label: string }> = [
		{ id: 'create', label: text.createTab },
		{ id: 'manage', label: text.manageTab },
		{ id: 'publish', label: text.publishTab },
	];
    const activeStudioTabIndex = Math.max(0, studioTabs.findIndex((item) => item.id === tab));
    const activateStudioTab = (nextTab: StudioTab, shouldFocus = false) => {
        setTab(nextTab);
        if (!shouldFocus) return;
        const focusTab = () => document.getElementById(getStudioTabId(nextTab))?.focus();
        if (typeof window.requestAnimationFrame === 'function') window.requestAnimationFrame(focusTab);
        else window.setTimeout(focusTab, 0);
    };
    const handleStudioTabKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
        const nextIndex = event.key === 'ArrowRight'
            ? (index + 1) % studioTabs.length
            : event.key === 'ArrowLeft'
                ? (index - 1 + studioTabs.length) % studioTabs.length
                : event.key === 'Home'
                    ? 0
                    : event.key === 'End'
                        ? studioTabs.length - 1
                        : -1;
        if (nextIndex < 0) return;
        event.preventDefault();
        activateStudioTab(studioTabs[nextIndex].id, true);
    };
    const installApprovedHubApp = async (capabilityID: string, name: string): Promise<ApprovedHubAppInstallResult> => {
        const cleanID = String(capabilityID || '').trim();
        if (!cleanID) throw new Error(text.schemaError);
        const hubInstall = await InstallMaclawAppPackageFromHub(cleanID) as Record<string, any>;
        const rawPackage = hubInstall?.package || (hubInstall?.package_json ? JSON.parse(String(hubInstall.package_json)) : null);
        const parsed = manifestToAppEntries(rawPackage);
        if (parsed.apps.length === 0) throw new Error(parsed.error || text.schemaError);
        const installRecord = (hubInstall?.install_record || null) as BackendAppInstallRecord | null;
        const installedApps = parsed.apps.map((app) => installedAppWithInstallEvidence({ ...app, marketCapabilityID: cleanID, marketInstallSource: 'enterprise_hub' as const, marketSourceLabel: 'Enterprise Hub' }, installRecord));
        installedApps.forEach(onInstallMarketApp);
        const appIDs = installedApps.map((item) => item.id);
        const primaryAppID = appIDs[0] || String(name || cleanID).trim();
        return {
            appIDs,
            plan: (hubInstall?.install_plan || null) as BackendAppInstallPlan | null,
            installRecord,
            versionSnapshot: installRecordVersionSnapshotForApp(installRecord, primaryAppID),
            installEvidence: installEvidenceRecordForApp(installRecord, primaryAppID),
        };
    };
    return (
        <>
            <div className="apps-detail__header">
                <div className="apps-detail__heading">
                    <h2 className="apps-detail__title">{text.appStudio}</h2>
                    <p className="apps-detail__subtitle">{text.studioSubtitle}</p>
                </div>
	                <div className="apps-detail__actions">
	                    <DataSrvDiscoverySummary discovery={datasrvDiscovery} lang={lang} onOpenMISDataSettings={onOpenMISDataSettings} />
	                    {onOpenManual && <button className="apps-secondary-button apps-manual-button" type="button" onClick={onOpenManual} aria-label={text.manualAria} title={text.manualAria}>{text.manual}</button>}
	                    <button className="apps-secondary-button" type="button" onClick={onClose}>{isZh(lang) ? '\u5173\u95ed' : 'Close'}</button>
	                </div>
            </div>
	            <div className="apps-detail__body elegant-scrollbar">
	                <div className="apps-preview apps-preview--studio">
	                    <DataSrvDiscoveryPanel discovery={datasrvDiscovery} apps={apps} lang={lang} onAddApp={onAddDiscoveredApp} />
	                    {skillDiscovery.status === 'ready' && skillDiscovery.candidates.length > 0 && (
	                        <div className="apps-filter-summary apps-filter-summary--manage" aria-live="polite">{text.inPanel}</div>
	                    )}
	                    <div className="apps-studio-tabs" role="tablist" aria-label={text.appStudio}>
                        {studioTabs.map((item, index) => {
                            const isActive = activeStudioTabIndex === index;
                            return (
                                <button
                                    key={item.id}
                                    id={getStudioTabId(item.id)}
                                    className={`apps-studio-tab ${isActive ? 'is-active' : ''}`}
                                    type="button"
                                    role="tab"
                                    aria-selected={isActive}
                                    aria-controls={getStudioPanelId(item.id)}
                                    tabIndex={isActive ? 0 : -1}
                                    onClick={() => activateStudioTab(item.id)}
                                    onKeyDown={(event) => handleStudioTabKeyDown(event, index)}
                                >
                                    {item.label}
                                </button>
                            );
                        })}
                    </div>
                    <div
                        className="apps-studio-panel"
                        role="tabpanel"
                        id={getStudioPanelId(tab)}
                        aria-labelledby={getStudioTabId(tab)}
                    >
	                        {tab === 'create' && <CreateAppPane lang={lang} onCreateApp={onCreateApp} />}
	                        {tab === 'manage' && <ManageAppsPane apps={apps} hiddenApps={hiddenApps} skillDiscovery={skillDiscovery} lang={lang} onTogglePin={onTogglePin} onUpdateApp={onUpdateApp} onDuplicateApp={onDuplicateApp} onMoveApp={onMoveApp} onToggleDisableApp={onToggleDisableApp} onRemoveApp={onRemoveApp} onRestoreApp={onRestoreApp} onAddDiscoveredApp={onAddDiscoveredApp} pendingEditAppId={pendingEditAppId} onPendingEditConsumed={onPendingEditConsumed} />}
	                        {tab === 'publish' && <PublishPane apps={apps} lang={lang} onFixApp={onEditApp} onInstallDependencies={onInstallDependencies} onInstallApprovedHubApp={installApprovedHubApp} onSyncHubAppGovernance={onSyncHubAppGovernance} />}
                    </div>
                </div>
            </div>
        </>
    );
};

const getStudioTabId = (tab: StudioTab) => `apps-studio-tab-${tab}`;
const getStudioPanelId = (tab: StudioTab) => `apps-studio-panel-${tab}`;

const dataSrvDiscoveryStatusLabel = (discovery: DataSrvDiscovery, text: typeof labels.zh) => discovery.status === 'ready' ? text.datasrvReady :
    discovery.status === 'loading' ? text.datasrvLoading :
        discovery.status === 'disabled' ? text.datasrvDisabled :
            discovery.status === 'error' ? text.datasrvError : '-';

const DataSrvDiscoverySummary = ({ discovery, lang, onOpenMISDataSettings }: { discovery: DataSrvDiscovery; lang?: string; onOpenMISDataSettings?: () => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const zh = isZh(lang);
    const statusLabel = dataSrvDiscoveryStatusLabel(discovery, text);
    const metrics = [
        { label: zh ? '\u57df' : 'Domains', value: discovery.domains },
        { label: zh ? '\u52a8\u4f5c' : 'Actions', value: discovery.actions },
        { label: zh ? '\u89c6\u56fe' : 'Views', value: discovery.views },
        { label: zh ? '\u62a5\u8868' : 'Reports', value: discovery.reports },
        { label: zh ? '\u770b\u677f' : 'Dashboards', value: discovery.dashboards },
    ];
    const endpoint = discovery.endpoint || 'http://127.0.0.1:18180';
    const meta = discovery.error ? `${endpoint} \u00b7 ${discovery.error}` : endpoint;
    const settingsLabel = zh ? '\u6253\u5f00 MIS \u6570\u636e\u8bbe\u7f6e' : 'Open MIS data settings';
    const actionLabel = zh ? '\u8bbe\u7f6e' : 'Settings';
    return (
        <button className="apps-datasrv-summary" type="button" data-status={discovery.status} aria-label={`${text.datasrvDiscovery} \u00b7 ${settingsLabel}`} title={settingsLabel} onClick={onOpenMISDataSettings}>
            <span className="apps-datasrv-summary__main">
                <span className="apps-datasrv-summary__title">{text.datasrvDiscovery}</span>
                <span className="apps-datasrv-summary__status">{statusLabel}</span>
                <span className="apps-datasrv-summary__action">{actionLabel}</span>
            </span>
            <span className="apps-datasrv-summary__metrics" aria-label={zh ? '\u80fd\u529b\u7edf\u8ba1' : 'Capability counts'}>
                {metrics.map((metric) => (
                    <span key={metric.label}><strong>{metric.value}</strong>{metric.label}</span>
                ))}
            </span>
            <span className="apps-datasrv-summary__meta" title={meta}>
                {meta}
            </span>
        </button>
    );
};

const DataSrvDiscoveryPanel = ({ discovery, apps, lang, onAddApp }: { discovery: DataSrvDiscovery; apps: AppEntry[]; lang?: string; onAddApp: (app: AppEntry) => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const installedIds = new Set(apps.map((app) => app.id));
    if (discovery.candidates.length === 0) return null;
    return (
        <section className="apps-discovery" data-status={discovery.status}>
            <div className="apps-discovery__candidates">
                <div className="apps-discovery__candidate-title">{text.discoveredApps}</div>
                {discovery.candidates.map((candidate) => {
                    const installed = installedIds.has(candidate.id);
                    return (
                        <div className="apps-discovery__candidate" key={candidate.id}>
                            <span className="apps-app-icon" style={{ '--apps-icon-color': candidate.accent } as CSSProperties}><AppIcon icon={candidate.icon} customIconDataUrl={candidate.customIconDataUrl} /></span>
                            <div>
                                <strong>{candidate.name}</strong>
                                <span>{candidate.manifest?.datasrv?.preferredAction || candidate.category}</span>
                            </div>
                            <button className="apps-secondary-button" type="button" disabled={installed} onClick={() => onAddApp(candidate)}>
                                {installed ? text.added : text.addToPanel}
                            </button>
                        </div>
                    );
                })}
            </div>
        </section>
    );
};

function studioSkillSourceFromMixed(source?: string): AppSkillDependency['source'] {
    const normalized = String(source || '').toLowerCase();
    if (normalized.includes('github')) return 'github';
    if (normalized.includes('enterprise_hub')) return 'enterprise_hub';
    if (normalized.includes('skillmarket')) return 'market';
    if (normalized.includes('market')) return 'market';
    if (normalized.includes('hub')) return 'hub';
    if (normalized.includes('builtin')) return 'builtin';
    return 'local';
}

function appSkillDependencyInstallRef(dependency?: AppSkillDependency): string {
    const raw = dependency as (AppSkillDependency & Record<string, unknown>) | undefined;
    return String(
        raw?.install_ref ||
        raw?.installRef ||
        raw?.capability_id ||
        raw?.capabilityID ||
        raw?.hub_skill_id ||
        raw?.hubSkillID ||
        raw?.skill_id ||
        raw?.skillID ||
        raw?.raw_url ||
        raw?.rawURL ||
        raw?.repo_url ||
        raw?.repoURL ||
        '',
    ).trim();
}

type StudioSkillPickerMode = 'general' | 'app' | 'approvalWorkflow';

function normalizedCapabilities(value?: string[] | string) {
    if (Array.isArray(value)) return value.map((item) => String(item || '').trim().toLowerCase()).filter(Boolean);
    return String(value || '')
        .split(',')
        .map((item) => item.trim().toLowerCase())
        .filter(Boolean);
}

function normalizedProductKind(value: { product_kind?: string; productKind?: string }) {
    return String(value.product_kind || value.productKind || '').trim().toLowerCase();
}

function isApprovalWorkflowSkillLike(value: { capabilities?: string[] | string; product_kind?: string; productKind?: string; description?: string; name?: string; id?: string }) {
    const capabilities = normalizedCapabilities(value.capabilities);
    if (capabilities.includes('approval.workflow')) return true;
    const productKind = normalizedProductKind(value);
    if (productKind === 'workflow_skill' || productKind === 'approval_workflow_skill') return true;
    return false;
}

function isMaclawAppSkillLike(value: { product_kind?: string; productKind?: string; is_maclaw_app?: boolean; isMaclawApp?: boolean }) {
    return !!(value.is_maclaw_app || value.isMaclawApp) || normalizedProductKind(value) === 'maclaw_app_skill';
}

function isAppRuntimeSkillLike(value: { capabilities?: string[] | string; product_kind?: string; productKind?: string; is_maclaw_app?: boolean; isMaclawApp?: boolean }) {
    if (isMaclawAppSkillLike(value)) return false;
    const productKind = normalizedProductKind(value);
    if (productKind === 'workflow_skill' || productKind === 'approval_workflow_skill') return false;
    return !isApprovalWorkflowSkillLike(value);
}

function skillChoiceMatchesMode(choice: StudioSkillChoice, mode: StudioSkillPickerMode) {
    if (mode === 'approvalWorkflow') return isApprovalWorkflowSkillLike(choice);
    if (mode === 'app') return isAppRuntimeSkillLike(choice);
    return true;
}

function installedSkillChoices(skills: SkillSummary[], lang?: string, mode: StudioSkillPickerMode = 'general'): StudioSkillChoice[] {
    const zh = isZh(lang);
    return skills
        .map((skill): StudioSkillChoice | null => {
            const name = String(skill.name || '').trim();
            if (!name) return null;
            const choice = {
                id: name,
                name,
                description: String(skill.description || '').trim(),
                source: 'installed' as const,
                sourceLabel: zh ? '\u5df2\u5b89\u88c5' : 'Installed',
                installed: true,
                productKind: normalizedProductKind(skill),
                isMaclawApp: isMaclawAppSkillLike(skill),
                capabilities: normalizedCapabilities(skill.capabilities),
            };
            return skillChoiceMatchesMode(choice, mode) ? choice : null;
        })
        .filter((item): item is StudioSkillChoice => !!item);
}

function mixedSkillChoice(result: any, lang?: string): StudioSkillChoice | null {
    const installed = !!result?.installed;
    const id = String((installed && result?.installed_name) || result?.install_ref || result?.id || result?.name || '').trim();
    const name = String(result?.name || id).trim();
    if (!id || !name) return null;
    const source = installed ? 'installed' : studioSkillSourceFromMixed(result?.source);
    const zh = isZh(lang);
    return {
        id,
        name,
        description: String(result?.description || '').trim(),
        source,
        sourceLabel: installed ? (zh ? '\u5df2\u5b89\u88c5' : 'Installed') : String(result?.source_label || result?.source || (zh ? '\u80fd\u529b\u5e02\u573a' : 'Market')),
        installed,
        productKind: normalizedProductKind(result || {}),
        isMaclawApp: isMaclawAppSkillLike(result || {}),
        capabilities: normalizedCapabilities(result?.capabilities),
    };
}

function marketReviewEvidenceFromMixedSkillResult(result: any, appID: string, appName: string): Record<string, unknown> | undefined {
    const metadata = appEvidenceRecord(result?.metadata || result?.Metadata) || {};
    const raw = appEvidenceRecord(
        result?.review_evidence
        || result?.reviewEvidence
        || result?.maclaw_app_review_evidence
        || metadata.review_evidence
        || metadata.reviewEvidence
        || metadata.maclaw_app_review_evidence,
    );
    const nested = raw && (
        appEvidenceRecord(raw[appID])
        || appEvidenceRecord(raw[appName])
        || appEvidenceRecord(raw[`market-${appID}`])
    );
    const evidence = nested || raw;
    return evidence && Object.keys(evidence).length > 0 ? evidence : undefined;
}

function marketAppEntryFromMixedSkillResult(result: any, lang?: string): AppEntry | null {
    if (!isMaclawAppSkillLike(result || {})) return null;
    const capabilityID = String(result?.install_ref || result?.id || '').trim();
    const appID = String(result?.maclaw_app_id || result?.maclawAppID || '').trim();
    const name = String(result?.maclaw_app_name || result?.maclawAppName || result?.name || appID).trim();
    if (!capabilityID || !appID || !name) return null;
    const kind = normalizeAppKind(result?.maclaw_app_kind || result?.maclawAppKind || result?.app_kind || result?.kind);
    const sourceLabel = String(result?.source_label || result?.source || 'Enterprise Hub').trim();
    const marketReviewEvidence = marketReviewEvidenceFromMixedSkillResult(result, appID, name);
    return {
        id: appID.startsWith('market-') ? appID : `market-${appID}`,
        name,
        description: String(result?.maclaw_app_description || result?.maclawAppDescription || result?.description || '').trim(),
        category: String(result?.maclaw_app_category || result?.maclawAppCategory || (isEnterpriseAppKind(kind) ? 'Enterprise' : 'Market')).trim(),
        kind,
        icon: normalizeSkillAppIcon(result?.maclaw_app_icon || result?.maclawAppIcon || (isEnterpriseApprovalAppKind(kind) ? 'shield' : isEnterpriseAppKind(kind) ? 'database' : 'contract')),
        accent: defaultAccentForKind(kind),
        version: normalizeAppVersion(result?.version || 1),
        source: 'market',
        marketCapabilityID: capabilityID,
        marketInstallSource: 'enterprise_hub',
        marketSourceLabel: sourceLabel,
        marketReviewEvidence,
    };
}

function uniqueMarketApps(apps: AppEntry[]): AppEntry[] {
    const byID = new Map<string, AppEntry>();
    apps.forEach((app) => {
        const existing = byID.get(app.id);
        byID.set(app.id, existing ? { ...existing, ...app, marketReviewEvidence: app.marketReviewEvidence || existing.marketReviewEvidence } : app);
    });
    return Array.from(byID.values());
}
function uniqueSkillChoices(choices: StudioSkillChoice[]): StudioSkillChoice[] {
    return Array.from(new Map(choices.map((choice) => [`${choice.source}:${choice.id}`, choice])).values());
}

async function openApprovalWorkflowDesigner() {
    try {
        const cfg = await LoadConfig() as {
            remote_hub_url?: string;
            remote_machine_id?: string;
            remote_machine_token?: string;
        } | null;
        const hubUrl = String(cfg?.remote_hub_url || '').trim().replace(/\/+$/, '');
        if (!hubUrl) return;
        const auth = new URLSearchParams();
        const machineID = String(cfg?.remote_machine_id || '').trim();
        const machineToken = String(cfg?.remote_machine_token || '').trim();
        if (machineID) auth.set('machine_id', machineID);
        if (machineToken) auth.set('token', machineToken);
        const fragment = auth.toString();
        BrowserOpenURL(`${hubUrl}/approval_workflow${fragment ? `#${fragment}` : ''}`);
    } catch {
        // Keep the app studio responsive when the local config is unavailable.
    }
}

const StudioSkillPicker = ({
    label,
    value,
    installedSkills,
    lang,
    placeholder,
    testId,
    mode = 'general',
    onSelect,
}: {
    label: string;
    value: string;
    installedSkills: SkillSummary[];
    lang?: string;
    placeholder?: string;
    testId?: string;
    mode?: StudioSkillPickerMode;
    onSelect: (choice: StudioSkillChoice) => void;
}) => {
    const zh = isZh(lang);
    const [query, setQuery] = useState('');
    const [marketChoices, setMarketChoices] = useState<StudioSkillChoice[]>([]);
    const [searchState, setSearchState] = useState<'idle' | 'searching' | 'error'>('idle');
    const [searchError, setSearchError] = useState('');
    const [lastMarketQuery, setLastMarketQuery] = useState('');
    const [activeSearchQuery, setActiveSearchQuery] = useState('');
    const searchRequestRef = useRef(0);
    const installedChoices = useMemo(() => installedSkillChoices(installedSkills, lang, mode), [installedSkills, lang, mode]);
    const selectedChoice = useMemo(() => {
        const allChoices = [...installedChoices, ...marketChoices];
        return allChoices.find((choice) => choice.id === value) || null;
    }, [installedChoices, marketChoices, value]);
    const selectedSummary = selectedChoice || (value.trim()
        ? {
            id: value.trim(),
            name: value.trim(),
            sourceLabel: zh ? '\u5f53\u524d\u7ed1\u5b9a' : 'Current binding',
        }
        : null);
    const normalizedQuery = query.trim().toLowerCase();
    const visibleInstalled = normalizedQuery
        ? installedChoices.filter((choice) => `${choice.name} ${choice.id} ${choice.description || ''}`.toLowerCase().includes(normalizedQuery))
        : installedChoices;
    const searchMarket = async () => {
        const cleanQuery = query.trim();
        if (!cleanQuery) return;
        const requestID = searchRequestRef.current + 1;
        searchRequestRef.current = requestID;
        setSearchState('searching');
        setActiveSearchQuery(cleanQuery);
        setSearchError('');
        try {
            const results = await SearchMixedSkills(cleanQuery);
            if (requestID !== searchRequestRef.current) return;
            const nextChoices = (Array.isArray(results) ? results : [])
                .map((item) => mixedSkillChoice(item, lang))
                .filter((item): item is StudioSkillChoice => !!item && skillChoiceMatchesMode(item, mode));
            setMarketChoices(uniqueSkillChoices(nextChoices));
            setLastMarketQuery(cleanQuery);
            setActiveSearchQuery('');
            setSearchState('idle');
        } catch (error) {
            if (requestID !== searchRequestRef.current) return;
            setMarketChoices([]);
            setLastMarketQuery(cleanQuery);
            setActiveSearchQuery('');
            setSearchState('error');
            setSearchError(error instanceof Error ? error.message : String(error || 'Search failed'));
        }
    };
    const marketSearchIsCurrent = !!lastMarketQuery && lastMarketQuery === query.trim();
    const currentQueryIsSearching = searchState === 'searching' && activeSearchQuery === query.trim();
    const visibleMarket = marketSearchIsCurrent
        ? marketChoices.filter((choice) => !installedChoices.some((installed) => installed.id === choice.id))
        : [];
    const marketEmpty = marketSearchIsCurrent && searchState === 'idle' && visibleMarket.length === 0;
    return (
        <div className="apps-skill-picker" data-testid={testId}>
            <div className="apps-skill-picker__search">
                <input
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                    onKeyDown={(event) => {
                        if (event.key === 'Enter') {
                            event.preventDefault();
                            void searchMarket();
                        }
                    }}
                    placeholder={placeholder || (mode === 'approvalWorkflow'
                        ? (zh ? '\u641c\u5ba1\u6279\u5de5\u4f5c\u6d41 ·  Hub' : 'Search approval workflows ·  Hub')
                        : (zh ? '\u641c\u5df2\u5b89\u88c5 ·  SkillMarket' : 'Search installed ·  SkillMarket'))}
                    aria-label={label}
                />
                <button className="apps-secondary-button" type="button" disabled={!query.trim() || currentQueryIsSearching} onClick={() => void searchMarket()}>
                    {zh ? '\u641c\u7d22' : 'Search'}
                </button>
            </div>
            {selectedSummary && (
                <div className="apps-skill-picker__selected">
                    <strong>{selectedSummary.name}</strong>
                    <span>{selectedSummary.id} · {selectedSummary.sourceLabel}</span>
                </div>
            )}
            <div className="apps-skill-picker__list" role="listbox" aria-label={label}>
                {(visibleMarket.length > 0 || marketEmpty || currentQueryIsSearching || (marketSearchIsCurrent && searchState === 'error')) && <div className="apps-skill-picker__group apps-skill-picker__group--market">Hub / HubCenter</div>}
                {currentQueryIsSearching && <div className="apps-skill-picker__empty">{zh ? '\u641c\u7d22\u4e2d...' : 'Searching...'}</div>}
                {visibleMarket.map((choice) => (
                    <button
                        key={`${choice.source}:${choice.id}`}
                        className={`apps-skill-picker__option ${value === choice.id ? 'is-active' : ''}`}
                        type="button"
                        role="option"
                        aria-selected={value === choice.id}
                        title={choice.description || choice.id}
                        onClick={() => onSelect(choice)}
                    >
                        <strong>{choice.name}</strong>
                        <span>{choice.id}</span>
                        <em>{choice.sourceLabel}</em>
                    </button>
                ))}
                {marketEmpty && <div className="apps-skill-picker__empty">{zh ? '\u5e02\u573a\u6682\u65e0\u5339\u914d Skill' : 'No matching market skills'}</div>}
                {marketSearchIsCurrent && searchState === 'error' && <div className="apps-skill-picker__empty" role="alert">{searchError}</div>}
                <div className="apps-skill-picker__group apps-skill-picker__group--installed">{zh ? '\u5df2\u5b89\u88c5 Skill' : 'Installed skills'}</div>
                {visibleInstalled.length === 0 ? (
                    <div className="apps-skill-picker__empty">{normalizedQuery ? (zh ? '\u672c\u5730\u6ca1\u6709\u5339\u914d\uff0c\u53ef\u7ee7\u7eed\u641c SkillMarket / Hub' : 'No installed match. Search SkillMarket / Hub to continue.') : (zh ? '\u6ca1\u6709\u53ef\u7528\u7684\u5df2\u5b89\u88c5 Skill' : 'No installed skills available')}</div>
                ) : visibleInstalled.map((choice) => (
                    <button
                        key={`installed:${choice.id}`}
                        className={`apps-skill-picker__option ${value === choice.id ? 'is-active' : ''}`}
                        type="button"
                        role="option"
                        aria-selected={value === choice.id}
                        title={choice.description || choice.id}
                        onClick={() => onSelect(choice)}
                    >
                        <strong>{choice.name}</strong>
                        <span>{choice.id}</span>
                        <em>{choice.sourceLabel}</em>
                    </button>
                ))}
            </div>
        </div>
    );
};

const ResultContractPreview = ({ contract, lang, testId }: { contract: AppResultContract; lang?: string; testId?: string }) => {
    const zh = isZh(lang);
    const deliveryItems = [
        { id: 'inlineContent', label: zh ? '\u5185\u5bb9' : 'Content', enabled: contract.delivery.inlineContent },
        { id: 'artifacts', label: zh ? '\u6587\u4ef6' : 'Files', enabled: contract.delivery.artifacts },
        { id: 'businessRecord', label: zh ? '\u4e1a\u52a1\u8bb0\u5f55' : 'Business record', enabled: contract.delivery.businessRecord },
        { id: 'notifications', label: zh ? '\u901a\u77e5' : 'Notifications', enabled: contract.delivery.notifications },
    ];
    return (
        <section className="apps-result-contract" aria-label={zh ? '\u7ed3\u679c\u5951\u7ea6' : 'Result contract'} data-testid={testId}>
            <div className="apps-preview-title-row">
                <div className="apps-definition__title">{zh ? '\u7ed3\u679c\u5951\u7ea6' : 'Result contract'}</div>
                <span className="apps-count">maclaw.app.result.v1</span>
            </div>
            <div className="apps-result-contract__summary">
                <span>{zh ? '\u4e3b\u7ed3\u679c' : 'Primary'}</span>
                <strong title={contract.primary}>{appContractTypeLabel(contract.primary, lang)}</strong>
            </div>
            <div className="apps-result-contract__chips" aria-label={zh ? '\u7ed3\u679c\u7c7b\u578b' : 'Result types'}>
                {contract.types.map((type) => <span key={type} title={type}>{appContractTypeLabel(type, lang)}</span>)}
            </div>
            <div className="apps-result-contract__delivery" aria-label={zh ? '\u4ea4\u4ed8\u65b9\u5f0f' : 'Delivery'}>
                {deliveryItems.map((item) => (
                    <span key={item.id} data-enabled={item.enabled ? 'true' : 'false'}>{item.label}</span>
                ))}
            </div>
            {contract.approvalDecisions?.length ? (
                <div className="apps-result-contract__decisions">
                    <span>{zh ? '\u5ba1\u6279\u7ed3\u679c' : 'Approval decisions'}</span>
                    <small>{contract.approvalDecisions.join(' / ')}</small>
                </div>
            ) : null}
        </section>
    );
};
const ResultContractDesigner = ({ contract, onChange, lang, testIdPrefix = 'studio' }: { contract: AppResultContract; onChange: (contract: AppResultContract) => void; lang?: string; testIdPrefix?: string }) => {
    const zh = isZh(lang);
    const deliveryItems = [
        { id: 'inlineContent', label: zh ? '\u5185\u5bb9' : 'Content' },
        { id: 'artifacts', label: zh ? '\u6587\u4ef6' : 'Files' },
        { id: 'businessRecord', label: zh ? '\u4e1a\u52a1\u8bb0\u5f55' : 'Business record' },
        { id: 'notifications', label: zh ? '\u901a\u77e5' : 'Notifications' },
    ] as const;
    const updateDelivery = (id: typeof deliveryItems[number]['id'], enabled: boolean) => onChange({ ...contract, delivery: { ...contract.delivery, [id]: enabled } });
    const typeOptions = contract.types.length ? contract.types : [contract.primary];
    return (
        <section className="apps-result-contract-designer" aria-label={zh ? '\u7ed3\u679c\u5951\u7ea6\u8bbe\u8ba1' : 'Result contract designer'}>
            <ResultContractPreview contract={contract} lang={lang} testId={testIdPrefix + '-result-contract'} />
            <div className="apps-result-contract-designer__controls">
                <div className="apps-form-row">
                    <label>{zh ? '\u4e3b\u7ed3\u679c' : 'Primary result'}</label>
                    <select data-testid={testIdPrefix + '-result-primary'} value={contract.primary} onChange={(event) => onChange({ ...contract, primary: event.target.value })}>
                        {typeOptions.map((type) => <option value={type} key={type}>{appContractTypeLabel(type, lang)}</option>)}
                    </select>
                </div>
                <div className="apps-result-contract-designer__delivery" aria-label={zh ? '\u4ea4\u4ed8\u65b9\u5f0f' : 'Delivery channels'}>
                    {deliveryItems.map((item) => (
                        <label key={item.id}>
                            <input data-testid={testIdPrefix + '-result-delivery-' + item.id} type="checkbox" checked={!!contract.delivery[item.id]} onChange={(event) => updateDelivery(item.id, event.target.checked)} />
                            <span>{item.label}</span>
                        </label>
                    ))}
                </div>
            </div>
        </section>
    );
};
const TestProtocolDesigner = ({ protocol, onChange, lang, testIdPrefix = 'studio', kind }: { protocol: AppTestProtocol; onChange: (protocol: AppTestProtocol) => void; lang?: string; testIdPrefix?: string; kind?: AppKind }) => {
    const zh = isZh(lang);
    const fingerprint = appTestProtocolFingerprint(protocol);
    const sampleInputText = JSON.stringify(protocol.sampleInput || {}, null, 2);
    const expectedOutputText = JSON.stringify(protocol.expectedOutput || {}, null, 2);
    const isApprovalProtocol = kind === 'enterprise_approval_app' || (protocol.workflowRequiredOutputs || []).length > 0;
    const updateJson = (field: 'sampleInput' | 'expectedOutput', value: string) => {
        try {
            const parsed = JSON.parse(value);
            if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) onChange(appTestProtocolWithFingerprint({ ...protocol, [field]: parsed }));
        } catch {
            // Keep the last valid protocol until the user finishes typing valid JSON.
        }
    };
    const updateList = (field: 'requiredRoles' | 'requiredScopes' | 'workflowRequiredInputs' | 'workflowDecisionOutputs' | 'workflowRequiredOutputs', value: string) => onChange(appTestProtocolWithFingerprint({ ...protocol, [field]: value.split(',').map((item) => item.trim()).filter(Boolean) }));
    const updateRisk = (riskLevel: string) => onChange(appTestProtocolWithFingerprint({ ...protocol, riskLevel }));
    const workflowRequiredInputs = protocol.workflowRequiredInputs || [];
    const workflowDecisionOutputs = protocol.workflowDecisionOutputs || [];
    const workflowRequiredOutputs = protocol.workflowRequiredOutputs || [];
    return (
        <section className="apps-test-protocol" aria-label={zh ? '\u53ef\u590d\u73b0\u6d4b\u8bd5\u534f\u8bae' : 'Reproducible test protocol'} data-testid={testIdPrefix + '-test-protocol'}>
            <div className="apps-preview-title-row">
                <div className="apps-definition__title">{zh ? '\u53ef\u590d\u73b0\u6d4b\u8bd5\u534f\u8bae' : 'Reproducible test protocol'}</div>
                <span className="apps-count">maclaw.app.test_protocol.v1</span>
            </div>
            <div className="apps-test-protocol__summary">
                <span>{zh ? '\u6307\u7eb9' : 'Fingerprint'}</span>
                <code>{fingerprint}</code>
                <span>{zh ? '\u98ce\u9669' : 'Risk'}</span>
                <select data-testid={testIdPrefix + '-test-risk'} value={protocol.riskLevel} onChange={(event) => updateRisk(event.target.value)}>
                    {['low', 'medium', 'high', 'critical'].map((level) => <option value={level} key={level}>{level}</option>)}
                </select>
            </div>
            {isApprovalProtocol && (
                <div className="apps-test-protocol__workflow" aria-label={zh ? '\u5ba1\u6279\u5de5\u4f5c\u6d41\u8f93\u51fa\u5951\u7ea6' : 'Approval workflow output contract'}>
                    <div className="apps-test-protocol__workflow-head">
                        <strong>{zh ? '\u5ba1\u6279\u5de5\u4f5c\u6d41\u8f93\u51fa\u5951\u7ea6' : 'Approval workflow output contract'}</strong>
                        <span>{zh ? 'Hub \u8bc4\u5ba1\u95e8\u7981' : 'Hub review gate'}</span>
                    </div>
                    <div className="apps-test-protocol__workflow-grid">
                        <div className="apps-form-row">
                            <label>{zh ? '\u5fc5\u586b\u8f93\u5165' : 'Required inputs'}</label>
                            <input data-testid={testIdPrefix + '-test-workflow-required-inputs'} value={workflowRequiredInputs.join(', ')} onChange={(event) => updateList('workflowRequiredInputs', event.target.value)} />
                        </div>
                        <div className="apps-form-row">
                            <label>{zh ? '\u51b3\u7b56\u8f93\u51fa' : 'Decision outputs'}</label>
                            <input data-testid={testIdPrefix + '-test-workflow-decision-outputs'} value={workflowDecisionOutputs.join(', ')} onChange={(event) => updateList('workflowDecisionOutputs', event.target.value)} />
                        </div>
                        <div className="apps-form-row apps-form-row--wide">
                            <label>{zh ? '\u5fc5\u586b\u7ed3\u679c\u5b57\u6bb5' : 'Required result fields'}</label>
                            <input data-testid={testIdPrefix + '-test-workflow-required-outputs'} value={workflowRequiredOutputs.join(', ')} onChange={(event) => updateList('workflowRequiredOutputs', event.target.value)} />
                        </div>
                    </div>
                    <div className="apps-test-protocol__workflow-chips" aria-label={zh ? '\u5fc5\u586b\u7ed3\u679c\u5b57\u6bb5' : 'Required result fields'}>
                        {workflowRequiredOutputs.map((item, index) => <span data-testid={testIdPrefix + '-test-workflow-required-output-' + index} key={item + '-' + index}>{item}</span>)}
                    </div>
                </div>
            )}
            <div className="apps-test-protocol__grid">
                <div className="apps-form-row apps-form-row--description">
                    <label>{zh ? '\u793a\u4f8b\u8f93\u5165' : 'Sample input'}</label>
                    <textarea data-testid={testIdPrefix + '-test-sample-input'} value={sampleInputText} onChange={(event) => updateJson('sampleInput', event.target.value)} />
                </div>
                <div className="apps-form-row apps-form-row--description">
                    <label>{zh ? '\u9884\u671f\u8f93\u51fa' : 'Expected output'}</label>
                    <textarea data-testid={testIdPrefix + '-test-expected-output'} value={expectedOutputText} onChange={(event) => updateJson('expectedOutput', event.target.value)} />
                </div>
            </div>
            <div className="apps-test-protocol__grid apps-test-protocol__grid--compact">
                <div className="apps-form-row">
                    <label>{zh ? '\u89d2\u8272' : 'Roles'}</label>
                    <input data-testid={testIdPrefix + '-test-roles'} value={protocol.requiredRoles.join(', ')} onChange={(event) => updateList('requiredRoles', event.target.value)} />
                </div>
                <div className="apps-form-row">
                    <label>{zh ? '\u6743\u9650\u8303\u56f4' : 'Scopes'}</label>
                    <input data-testid={testIdPrefix + '-test-scopes'} value={protocol.requiredScopes.join(', ')} onChange={(event) => updateList('requiredScopes', event.target.value)} />
                </div>
            </div>
        </section>
    );
};
const StringListDesigner = ({ title, items, onChange, lang, testIdPrefix }: { title: string; items: string[]; onChange: (items: string[]) => void; lang?: string; testIdPrefix: string }) => {
    const zh = isZh(lang);
    const update = (index: number, value: string) => onChange(items.map((item, itemIndex) => itemIndex === index ? value : item));
    const remove = (index: number) => onChange(items.filter((_, itemIndex) => itemIndex !== index));
    return (
        <div className="apps-ui-list-designer" aria-label={title}>
            <div className="apps-preview-title-row"><div className="apps-definition__title">{title}</div><button className="apps-secondary-button" type="button" onClick={() => onChange([...items, ''])}>{zh ? '\u6dfb\u52a0' : 'Add'}</button></div>
            <div className="apps-ui-list-designer__items">
                {items.map((item, index) => (
                    <div className="apps-ui-list-designer__item" key={`${testIdPrefix}-${index}`}>
                        <input data-testid={`${testIdPrefix}-${index}`} value={item} onChange={(event) => update(index, event.target.value)} placeholder={zh ? '\u8f93\u5165 key' : 'Enter key'} />
                        <button className="apps-secondary-button" type="button" onClick={() => remove(index)}>{zh ? '\u5220\u9664' : 'Remove'}</button>
                    </div>
                ))}
            </div>
        </div>
    );
};

const EnterpriseUIConfigDesigner = ({ kind, navigation, columns, onNavigationChange, onColumnsChange, lang, testIdPrefix = 'studio' }: { kind: AppKind; navigation: string[]; columns: string[]; onNavigationChange: (items: string[]) => void; onColumnsChange: (items: string[]) => void; lang?: string; testIdPrefix?: string }) => {
    const zh = isZh(lang);
    return (
        <section className="apps-enterprise-ui-designer" aria-label={zh ? '\u4f01\u4e1a\u754c\u9762\u914d\u7f6e' : 'Enterprise UI configuration'}>
            <div className="apps-preview-title-row"><div className="apps-definition__title">{zh ? '\u5bfc\u822a\u548c\u5217' : 'Navigation and columns'}</div><span className="apps-count">{workspaceEntryForKind(kind)}</span></div>
            <div className="apps-enterprise-ui-designer__grid">
                <StringListDesigner title={zh ? '\u5bfc\u822a' : 'Navigation'} items={navigation} onChange={onNavigationChange} lang={lang} testIdPrefix={testIdPrefix + '-ui-navigation'} />
                <StringListDesigner title={zh ? '\u5217' : 'Columns'} items={columns} onChange={onColumnsChange} lang={lang} testIdPrefix={testIdPrefix + '-ui-column'} />
            </div>
        </section>
    );
};
const WorkflowMappingDesigner = ({ value, onChange, lang, testIdPrefix = 'studio' }: { value: AppWorkflowMapping; onChange: (value: AppWorkflowMapping) => void; lang?: string; testIdPrefix?: string }) => {
    const zh = isZh(lang);
    const update = (patch: Partial<AppWorkflowMapping>) => onChange(normalizeAppWorkflowMapping({ ...value, ...patch }, 'enterprise_approval_app') || value);
    const updateStatus = (key: keyof AppWorkflowMapping['statusMapping'], nextValue: string) => update({ statusMapping: { ...value.statusMapping, [key]: nextValue } });
    const nodeItems = [
        { id: 'submitNode', label: zh ? '\u53d1\u8d77\u8282\u70b9' : 'Submit node', value: value.submitNode },
        { id: 'approvalNode', label: zh ? '\u5ba1\u6279\u8282\u70b9' : 'Approval node', value: value.approvalNode },
        { id: 'resultNode', label: zh ? '\u7ed3\u679c\u8282\u70b9' : 'Result node', value: value.resultNode },
        { id: 'attentionNode', label: zh ? '\u9700\u5173\u6ce8\u8282\u70b9' : 'Attention node', value: value.attentionNode || '' },
    ] as const;
    const statusItems = [
        { id: 'pending', label: zh ? '\u5ba1\u6279\u4e2d' : 'Pending', value: value.statusMapping.pending },
        { id: 'approved', label: zh ? '\u901a\u8fc7' : 'Approved', value: value.statusMapping.approved },
        { id: 'rejected', label: zh ? '\u9a73\u56de' : 'Rejected', value: value.statusMapping.rejected },
        { id: 'attention', label: zh ? '\u9700\u5173\u6ce8' : 'Attention', value: value.statusMapping.attention },
        { id: 'requiresInput', label: zh ? '\u9700\u8865\u5145' : 'Needs input', value: value.statusMapping.requiresInput || '' },
    ] as const;
    return (
        <section className="apps-workflow-mapping" aria-label={zh ? '\u5de5\u4f5c\u6d41\u8282\u70b9\u6620\u5c04' : 'Workflow node mapping'} data-testid={`${testIdPrefix}-workflow-mapping`}>
            <div className="apps-preview-title-row">
                <div className="apps-definition__title">{zh ? '\u5de5\u4f5c\u6d41\u8282\u70b9\u6620\u5c04' : 'Workflow node mapping'}</div>
                <span className="apps-count">maclaw.app.workflow.v1</span>
            </div>
            <div className="apps-workflow-mapping__flow" aria-label={zh ? '\u8282\u70b9\u6d41\u8f6c' : 'Node flow'}>
                <span>{value.submitNode}</span>
                <span>{value.approvalNode}</span>
                <span>{value.resultNode}</span>
            </div>
            <div className="apps-workflow-mapping__grid">
                {nodeItems.map((item) => (
                    <div className="apps-form-row" key={item.id}>
                        <label>{item.label}</label>
                        <input data-testid={`${testIdPrefix}-workflow-${item.id}`} value={item.value} onChange={(event) => update({ [item.id]: event.target.value } as Partial<AppWorkflowMapping>)} />
                    </div>
                ))}
            </div>
            <div className="apps-workflow-mapping__grid apps-workflow-mapping__grid--status">
                {statusItems.map((item) => (
                    <div className="apps-form-row" key={item.id}>
                        <label>{item.label}</label>
                        <input data-testid={`${testIdPrefix}-workflow-status-${item.id}`} value={item.value} onChange={(event) => updateStatus(item.id, event.target.value)} />
                    </div>
                ))}
            </div>
        </section>
    );
};

type AppAboutInfoText = {
    aboutInfoSection: string;
    aboutInfoSectionDesc: string;
    aboutAuthor: string;
    aboutAuthorPlaceholder: string;
    aboutCopyright: string;
    aboutCopyrightPlaceholder: string;
    aboutWebsite: string;
    aboutWebsitePlaceholder: string;
    aboutEmail: string;
    aboutEmailPlaceholder: string;
};

const AppAboutInfoFields = ({ text, value, onChange, testIdPrefix }: { text: AppAboutInfoText; value?: AppAboutInfo; onChange: (patch: Partial<AppAboutInfo>) => void; testIdPrefix?: string }) => {
    const testId = (field: keyof AppAboutInfo) => testIdPrefix ? { 'data-testid': `${testIdPrefix}-about-${field}` } : {};
    return (
        <section className="apps-about-info-section apps-form-row--wide" aria-label={text.aboutInfoSection}>
            <div className="apps-definition__title">{text.aboutInfoSection}</div>
            <p className="apps-about-info-section__desc">{text.aboutInfoSectionDesc}</p>
            <div className="apps-form-row">
                <label>{text.aboutAuthor}</label>
                <input {...testId('author')} value={value?.author || ''} onChange={(event) => onChange({ author: event.target.value })} placeholder={text.aboutAuthorPlaceholder} />
            </div>
            <div className="apps-form-row">
                <label>{text.aboutCopyright}</label>
                <input {...testId('copyright')} value={value?.copyright || ''} onChange={(event) => onChange({ copyright: event.target.value })} placeholder={text.aboutCopyrightPlaceholder} />
            </div>
            <div className="apps-form-row">
                <label>{text.aboutWebsite}</label>
                <input {...testId('website')} type="url" value={value?.website || ''} onChange={(event) => onChange({ website: event.target.value })} placeholder={text.aboutWebsitePlaceholder} />
            </div>
            <div className="apps-form-row">
                <label>{text.aboutEmail}</label>
                <input {...testId('email')} type="email" value={value?.email || ''} onChange={(event) => onChange({ email: event.target.value })} placeholder={text.aboutEmailPlaceholder} />
            </div>
        </section>
    );
};

const CreateAppPane = ({ lang, onCreateApp }: { lang?: string; onCreateApp: (app: AppEntry, options?: { keepStudioCreate?: boolean }) => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const zh = isZh(lang);
    const [name, setName] = useState('');
    const [category, setCategory] = useState(zh ? '\u6587\u6863\u5904\u7406' : 'Document');
    const [kind, setKind] = useState<AppKind>('tool_app');
    const [icon, setIcon] = useState<AppIconName>('shield');
    const [accent, setAccent] = useState(defaultAccentForKind('tool_app'));
    const [inputMode, setInputMode] = useState<'file' | 'form' | 'mixed'>('file');
    const [multipleFiles, setMultipleFiles] = useState(false);
    const [outputModes, setOutputModes] = useState<string[]>(['docx', 'pdf']);
    const [skillFields, setSkillFields] = useState<SkillAppField[]>([]);
    const [description, setDescription] = useState('');
    const [draftPrompt, setDraftPrompt] = useState('');
    const [copyState, setCopyState] = useState<'idle' | 'copied'>('idle');
    const [aboutAuthor, setAboutAuthor] = useState('');
    const [aboutCopyright, setAboutCopyright] = useState('');
    const [aboutWebsite, setAboutWebsite] = useState('');
    const [aboutEmail, setAboutEmail] = useState('');
    const [availableSkills, setAvailableSkills] = useState<SkillSummary[]>([]);
    const [selectedSkill, setSelectedSkill] = useState('');
    const [selectedSkillSource, setSelectedSkillSource] = useState<StudioSkillChoice['source']>('installed');
    const [skillAppSaveState, setSkillAppSaveState] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle');
    const [skillAppSaveMessage, setSkillAppSaveMessage] = useState('');
    const [skillMarketUploadState, setSkillMarketUploadState] = useState<'idle' | 'uploading' | 'uploaded' | 'error'>('idle');
    const [manifestPreviewOpen, setManifestPreviewOpen] = useState(true);
    const [layoutTemplate, setLayoutTemplate] = useState<StudioLayoutTemplate>('document_workspace');
    const [layoutDensity, setLayoutDensity] = useState<StudioLayoutDensity>('comfortable');
    const [primaryRegion, setPrimaryRegion] = useState<StudioPrimaryRegion>('left');
    const [outputRegion, setOutputRegion] = useState<StudioOutputRegion>('right');
    const [layoutRegions, setLayoutRegions] = useState<RuntimeWorkspaceRegion[]>(() => defaultRuntimeWorkspaceLayout('tool_app').regions);
    const [businessDomain, setBusinessDomain] = useState('business');
    const [businessObjectRole, setBusinessObjectRole] = useState('record');
    const [businessPreferredAction, setBusinessPreferredAction] = useState('');
    const [businessPreferredView, setBusinessPreferredView] = useState('');
    const [businessPreferredReport, setBusinessPreferredReport] = useState('');
    const [businessPreferredDashboard, setBusinessPreferredDashboard] = useState('');
    const [appSkillID, setAppSkillID] = useState('');
    const [appSkillSource, setAppSkillSource] = useState<AppSkillDependency['source']>('local');
    const [workflowSkillID, setWorkflowSkillID] = useState('');
    const [workflowSkillVersion, setWorkflowSkillVersion] = useState('1.0.0');
    const [approvalEvent, setApprovalEvent] = useState('record.submitted');
    const [approvalObjectRole, setApprovalObjectRole] = useState('record');
    const [workflowMapping, setWorkflowMapping] = useState<AppWorkflowMapping>(defaultAppWorkflowMapping('enterprise_approval_app', 'business', 'record') as AppWorkflowMapping);
    const [dependencySource, setDependencySource] = useState<AppSkillDependency['source']>('hub');
    const [dependencyInstallRef, setDependencyInstallRef] = useState('');
    const [resultContractDraft, setResultContractDraft] = useState<AppResultContract | undefined>(undefined);
    const [testProtocolDraft, setTestProtocolDraft] = useState<AppTestProtocol | undefined>(undefined);
    const [uiNavigation, setUiNavigation] = useState<string[]>(defaultEnterpriseNavigation('tool_app'));
    const [uiColumns, setUiColumns] = useState<string[]>(defaultEnterpriseColumns('tool_app'));
    const appReadySkills = useMemo(() => availableSkills.filter(isAppRuntimeSkillLike), [availableSkills]);
    const approvalWorkflowSkills = useMemo(() => availableSkills.filter(isApprovalWorkflowSkillLike), [availableSkills]);
    const defaultInstalledSkillID = () => String(appReadySkills[0]?.name || '').trim();
    const defaultApprovalWorkflowSkillID = () => String(approvalWorkflowSkills[0]?.name || '').trim();
    useEffect(() => {
        let cancelled = false;
        ListNLSkills()
            .then((skills: SkillSummary[] = []) => {
                if (cancelled) return;
                const skillList = Array.isArray(skills) ? skills.filter((skill) => skill?.name) : [];
                const firstSkillID = String(skillList.find(isAppRuntimeSkillLike)?.name || '');
                const firstWorkflowSkillID = String(skillList.find(isApprovalWorkflowSkillLike)?.name || '');
                setAvailableSkills(skillList);
                setSelectedSkill((current) => current || firstSkillID);
                setAppSkillID((current) => current || (isEnterpriseAppKind(kind) ? firstSkillID : ''));
                setWorkflowSkillID((current) => current || (isEnterpriseApprovalAppKind(kind) ? firstWorkflowSkillID : ''));
            })
            .catch((error) => {
                if (cancelled) return;
                setSkillAppSaveState('error');
                setSkillAppSaveMessage(error instanceof Error ? error.message : String(error || 'Failed to read skills'));
            });
        return () => {
            cancelled = true;
        };
    }, [zh]);
    useEffect(() => {
        const firstSkillID = defaultInstalledSkillID();
        const firstWorkflowSkillID = defaultApprovalWorkflowSkillID();
        if (firstSkillID) {
            setSelectedSkill((current) => current || firstSkillID);
            if (isEnterpriseAppKind(kind)) setAppSkillID((current) => current || firstSkillID);
        }
        if (firstWorkflowSkillID && isEnterpriseApprovalAppKind(kind)) setWorkflowSkillID((current) => current || firstWorkflowSkillID);
    }, [appReadySkills, approvalWorkflowSkills, kind]);
    const selectKind = (nextKind: AppKind, applyPreset = false) => {
        setKind(nextKind);
        setAccent(defaultAccentForKind(nextKind));
        if (!applyPreset) return;
        setCategory(nextKind === 'tool_app' ? (zh ? '\u6587\u6863\u5904\u7406' : 'Document') : nextKind === 'automation_app' ? (zh ? '\u81ea\u52a8\u5316' : 'Automation') : 'OA');
        setIcon(nextKind === 'tool_app' ? 'shield' : nextKind === 'automation_app' ? 'sync' : 'sheet');
        setInputMode(nextKind === 'tool_app' ? 'file' : 'mixed');
        setMultipleFiles(false);
        setOutputModes(nextKind === 'tool_app' ? ['docx', 'pdf'] : ['json']);
        setSkillFields([]);
        setAppSkillID(isEnterpriseAppKind(nextKind) ? (appSkillID.trim() || defaultInstalledSkillID()) : '');
        setAppSkillSource('local');
        setWorkflowSkillID(isEnterpriseApprovalAppKind(nextKind) ? (workflowSkillID.trim() || defaultApprovalWorkflowSkillID()) : '');
        setWorkflowSkillVersion('1.0.0');
        setApprovalObjectRole(isEnterpriseApprovalAppKind(nextKind) ? (approvalObjectRole.trim() || 'record') : 'record');
        setApprovalEvent(isEnterpriseApprovalAppKind(nextKind) ? (approvalEvent.trim() || 'record.submitted') : 'record.submitted');
        setWorkflowMapping(defaultAppWorkflowMapping(nextKind, businessDomain.trim() || 'business', approvalObjectRole.trim() || businessObjectRole.trim() || 'record') || (defaultAppWorkflowMapping('enterprise_approval_app') as AppWorkflowMapping));
        setDependencySource('hub');
        setDependencyInstallRef('');
        setResultContractDraft(undefined);
        setTestProtocolDraft(undefined);
        setUiNavigation(defaultEnterpriseNavigation(nextKind));
        setUiColumns(defaultEnterpriseColumns(nextKind));
        setCopyState('idle');
        setSkillAppSaveState('idle');
        setSkillAppSaveMessage('');
        setSkillMarketUploadState('idle');
        setSelectedSkillSource('installed');
        const nextLayout = defaultRuntimeWorkspaceLayout(nextKind);
        setLayoutTemplate(nextLayout.template);
        setLayoutDensity(nextLayout.density);
        setPrimaryRegion(nextLayout.primaryRegion);
        setOutputRegion(nextLayout.outputRegion);
        setLayoutRegions(nextLayout.regions);
        setBusinessDomain(nextKind === 'enterprise_normal_app' ? 'business' : 'business');
        setBusinessObjectRole(nextKind === 'enterprise_normal_app' ? 'record' : 'record');
        setBusinessPreferredAction('');
        setBusinessPreferredView('');
        setBusinessPreferredReport('');
        setBusinessPreferredDashboard('');
    };
    const generateDraftFromPrompt = () => {
        const prompt = draftPrompt.trim();
        if (!prompt) return;
        const lower = prompt.toLowerCase();
        const isToolPrompt = /pdf|word|excel|upload|file|document|contract|sheet/.test(lower);
        const isAutomationPrompt = /schedule|sync|monitor|collect|automation/.test(lower);
        const isFinancePrompt = /finance|expense|invoice|payment|\u8d39\u7528|\u62a5\u9500|\u8d22\u52a1|\u53d1\u7968|\u4ed8\u6b3e/.test(lower);
        const isInventoryPrompt = /inventory|warehouse|stock|purchase/.test(lower);
        const isCrmPrompt = /crm|customer|sales|lead/.test(lower);
        const isOaPrompt = /oa|approve|approval|leave|hr|\u5ba1\u6279|\u8bf7\u5047|\u4eba\u4e8b/.test(lower);
        const nextKind: AppKind = isAutomationPrompt && !isToolPrompt ? 'automation_app' : isToolPrompt ? 'tool_app' : isOaPrompt ? 'enterprise_approval_app' : 'enterprise_normal_app';
        const nextMultipleFiles = /multi|multiple|batch|folder/.test(lower);
        const nextName = prompt.match(/(?:\u505a\u4e00\u4e2a|\u521b\u5efa|build|create)\s*([^,;\uff0c\uff1b]{2,16})/)?.[1]?.trim() || prompt.slice(0, 12);
        setName(nextName || (zh ? '\u65b0\u5e94\u7528' : 'New app'));
        setKind(nextKind);
        setInputMode(nextKind === 'tool_app' ? (/form|parameter/.test(lower) ? 'mixed' : 'file') : 'mixed');
        setMultipleFiles(nextKind === 'tool_app' && nextMultipleFiles);
        setOutputModes(/excel|xlsx/.test(lower) ? ['xlsx'] : /json/.test(lower) ? ['json'] : /txt|text/.test(lower) ? ['txt'] : ['docx', 'pdf']);
        setSkillFields(nextKind === 'tool_app' && /field|parameter|form/.test(lower)
            ? [{ name: 'requirement', label: zh ? '\u5904\u7406\u8981\u6c42' : 'Requirement', type: 'text', required: true }]
            : []);
        setCategory(nextKind === 'tool_app' ? (zh ? '\u6587\u6863\u5904\u7406' : 'Document') : nextKind === 'automation_app' ? (zh ? '\u81ea\u52a8\u5316' : 'Automation') : isFinancePrompt ? (zh ? '\u8d22\u52a1' : 'Finance') : isInventoryPrompt ? (zh ? '\u8fdb\u9500\u5b58' : 'Inventory') : isCrmPrompt ? 'CRM' : isOaPrompt ? 'OA' : 'OA');
        setIcon(nextKind === 'automation_app' ? 'sync' : nextKind === 'tool_app' ? (/contract/.test(lower) ? 'contract' : /pdf/.test(lower) ? 'pdf' : 'shield') : isFinancePrompt ? 'receipt' : isInventoryPrompt ? 'warehouse' : isCrmPrompt ? 'customer' : 'sheet');
        setAccent(defaultAccentForKind(nextKind));
        const nextLayoutTemplate: StudioLayoutTemplate = nextKind === 'tool_app' ? 'document_workspace' : /nav|menu|\u5bfc\u822a/.test(lower) ? 'left_nav' : 'classic_split';
        const nextLayoutDensity: StudioLayoutDensity = /compact|dense|\u7d27\u51d1/.test(lower) ? 'compact' : 'comfortable';
        const nextPrimaryRegion: StudioPrimaryRegion = nextKind === 'tool_app' ? 'left' : 'center';
        const nextOutputRegion: StudioOutputRegion = /modal|dialog|\u5f39\u7a97/.test(lower) ? 'modal' : nextKind === 'tool_app' ? 'right' : 'bottom';
        setLayoutTemplate(nextLayoutTemplate);
        setLayoutDensity(nextLayoutDensity);
        setPrimaryRegion(nextPrimaryRegion);
        setOutputRegion(nextOutputRegion);
        setLayoutRegions(studioRegionsForLayout(nextKind, nextLayoutTemplate, nextPrimaryRegion, nextOutputRegion));
        const nextAppSkillID = isEnterpriseAppKind(nextKind) ? defaultInstalledSkillID() : '';
        const nextWorkflowSkillID = isEnterpriseApprovalAppKind(nextKind) ? defaultApprovalWorkflowSkillID() : '';

        const nextApprovalObjectRole = isFinancePrompt ? 'finance' : isInventoryPrompt ? 'inventory' : isCrmPrompt ? 'crm' : isOaPrompt ? 'oa' : 'record';
        const nextBusinessDomain = isFinancePrompt ? 'finance' : isInventoryPrompt ? 'inventory' : isCrmPrompt ? 'sales' : isOaPrompt ? 'oa' : 'business';
        const nextBusinessObjectRole = isFinancePrompt ? 'expense_report' : isInventoryPrompt ? 'stock_item' : isCrmPrompt ? 'customer' : isOaPrompt ? 'request' : 'record';
        const nextPreferredAction = isFinancePrompt ? 'finance.expense_upsert' : isInventoryPrompt ? 'inventory.stock_update' : isCrmPrompt ? 'sales.customer_upsert' : isOaPrompt ? 'oa.request_upsert' : nextBusinessDomain + '.' + nextBusinessObjectRole + '_upsert';
        const nextPreferredView = isFinancePrompt ? 'finance.expense_by_status' : isInventoryPrompt ? 'inventory.stock_position' : isCrmPrompt ? 'sales.customer_directory' : isOaPrompt ? 'oa.request_directory' : nextBusinessDomain + '.' + nextBusinessObjectRole + '_directory';
        const nextPreferredReport = isFinancePrompt ? 'finance.expense_report' : isInventoryPrompt ? 'inventory.stock_by_warehouse' : isCrmPrompt ? 'sales.customer_activity' : isOaPrompt ? 'oa.request_status' : '';
        const nextPreferredDashboard = isFinancePrompt ? 'finance.overview' : isInventoryPrompt ? 'inventory.overview' : isCrmPrompt ? 'sales.overview' : isOaPrompt ? 'oa.overview' : '';
        setAppSkillID(nextAppSkillID);
        setAppSkillSource('local');
        setWorkflowSkillID(nextWorkflowSkillID);
        setWorkflowSkillVersion('1.0.0');
        setApprovalObjectRole(isEnterpriseApprovalAppKind(nextKind) ? nextApprovalObjectRole : 'record');
        setApprovalEvent(isEnterpriseApprovalAppKind(nextKind) ? `${nextApprovalObjectRole}.submitted` : 'record.submitted');
        setWorkflowMapping(defaultAppWorkflowMapping(nextKind, nextBusinessDomain, nextBusinessObjectRole) || (defaultAppWorkflowMapping('enterprise_approval_app') as AppWorkflowMapping));
        setBusinessDomain(isEnterpriseAppKind(nextKind) ? nextBusinessDomain : 'business');
        setBusinessObjectRole(isEnterpriseAppKind(nextKind) ? nextBusinessObjectRole : 'record');
        setBusinessPreferredAction(nextKind === 'enterprise_normal_app' ? nextPreferredAction : '');
        setBusinessPreferredView(nextKind === 'enterprise_normal_app' ? nextPreferredView : '');
        setBusinessPreferredReport(nextKind === 'enterprise_normal_app' ? nextPreferredReport : '');
        setBusinessPreferredDashboard(nextKind === 'enterprise_normal_app' ? nextPreferredDashboard : '');
        setDependencySource('hub');
        setDependencyInstallRef('');
        setResultContractDraft(undefined);
        setTestProtocolDraft(undefined);
        setUiNavigation(defaultEnterpriseNavigation(nextKind));
        setUiColumns(defaultEnterpriseColumns(nextKind));
        setDescription(prompt);
        setCopyState('idle');
    };
    const buildDraftApp = (id: string, cleanName = name.trim() || (zh ? '\u672a\u547d\u540d\u5e94\u7528' : 'Untitled app'), boundSkillID = id, appDefinitionFile = 'maclaw.apps.json'): AppEntry => {
        const enterpriseAppSkillID = isEnterpriseAppKind(kind) ? (appSkillID.trim() || `${id}-app`) : '';
        const workflowDependency: AppSkillDependency[] = isEnterpriseApprovalAppKind(kind) && workflowSkillID.trim()
            ? [{ id: workflowSkillID.trim(), version: workflowSkillVersion.trim() || undefined, kind: 'workflow_skill', required: true, source: dependencySource || 'hub', install_ref: dependencyInstallRef.trim() || undefined, capabilities: ['approval.workflow'] }]
            : [];
        const businessDomainValue = businessDomain.trim() || 'business';
        const enterpriseDomain = isEnterpriseApprovalAppKind(kind) ? (approvalObjectRole.trim() || businessDomainValue) : businessDomainValue;
        let manifest = isEnterpriseAppKind(kind)
            ? makeEnterpriseManifest(
                kind,
                enterpriseDomain,
                kind === 'enterprise_normal_app' ? businessPreferredAction.trim() : '',
                kind === 'enterprise_normal_app' ? businessPreferredView.trim() : '',
                kind === 'enterprise_normal_app' ? businessPreferredReport.trim() : '',
                kind === 'enterprise_normal_app' ? businessPreferredDashboard.trim() : '',
                enterpriseAppSkillID,
                workflowDependency,
                { event: approvalEvent, objectRole: approvalObjectRole },
                appSkillSource,
            )
            : kind === 'automation_app'
                ? makeAutomationManifest()
                : makeSkillManifest(boundSkillID, inputMode, outputModes, skillFields, multipleFiles && inputMode !== 'form', appDefinitionFile);
        if (kind === 'enterprise_normal_app' && manifest.datasrv) {
            const objectRole = businessObjectRole.trim();
            manifest = {
                ...manifest,
                datasrv: {
                    ...manifest.datasrv,
                    ...(objectRole ? { objectRole } : {}),
                },
            };
        }
        return {
            id,
            name: cleanName,
            description: description.trim() || (zh ? '\u7531\u5e94\u7528\u7a0b\u5e8f\u5de5\u4f5c\u5ba4\u521b\u5efa\u7684\u5e94\u7528\u5165\u53e3\u3002' : 'App entry created in App Studio.'),
            category: category.trim() || (zh ? '\u672a\u5206\u7c7b' : 'Uncategorized'),
            kind,
            icon,
            accent,
            version: 1,
            source: 'local',
            manifest: applyAppTestProtocol(applyAppResultContract(applyEnterpriseUIConfig(applyAppWorkflowMapping(applyStudioWorkspaceLayout(manifest, kind, { template: layoutTemplate, density: layoutDensity, primaryRegion, outputRegion, regions: layoutRegions }), kind, workflowMapping), kind, uiNavigation, uiColumns), kind, resultContractDraft), kind, testProtocolDraft),
            aboutInfo: normalizeAppAboutInfo({
                author: aboutAuthor,
                copyright: aboutCopyright,
                website: aboutWebsite,
                email: aboutEmail,
            }),
        };
    };
    const draftApp = buildDraftApp(
        'draft-app',
        undefined,
        kind === 'tool_app' && selectedSkill ? selectedSkill : 'draft-app',
        kind === 'tool_app' && selectedSkill ? 'maclaw.app.json' : 'maclaw.apps.json',
    );
    const draftResultContract = normalizeAppResultContract(resultContractDraft, kind, draftApp.manifest?.skill?.outputModes || outputModes);
    const draftTestProtocol = appTestProtocolWithFingerprint(normalizeAppTestProtocol(testProtocolDraft, kind, draftApp.manifest?.skill?.outputModes || outputModes, draftResultContract));
    const draftManifestText = JSON.stringify(appToManifest(draftApp), null, 2);
    const skillDefinitionTargetID = isEnterpriseAppKind(kind) ? appSkillID.trim() : selectedSkill.trim();
    const skillDefinitionTargetInstalled = kind === 'tool_app' ? selectedSkillSource === 'installed' : isEnterpriseAppKind(kind) && appSkillSource === 'local';
    const canWriteSkillDefinition = (kind === 'tool_app' || isEnterpriseAppKind(kind)) && !!name.trim() && !!skillDefinitionTargetID && skillDefinitionTargetInstalled;
    const studioLayoutValue: RuntimeWorkspaceLayout = normalizeRuntimeWorkspaceLayout({ template: layoutTemplate, density: layoutDensity, primaryRegion, outputRegion, regions: layoutRegions }, kind);
    const updateStudioLayout = (layout: RuntimeWorkspaceLayout) => {
        setLayoutTemplate(layout.template);
        setLayoutDensity(layout.density);
        setPrimaryRegion(layout.primaryRegion);
        setOutputRegion(layout.outputRegion);
        setLayoutRegions(layout.regions);
    };
    const updateAboutInfo = useCallback((patch: Partial<AppAboutInfo>) => {
        if ('author' in patch) setAboutAuthor(patch.author || '');
        if ('copyright' in patch) setAboutCopyright(patch.copyright || '');
        if ('website' in patch) setAboutWebsite(patch.website || '');
        if ('email' in patch) setAboutEmail(patch.email || '');
    }, []);
    useEffect(() => {
        setCopyState((current) => current === 'copied' ? 'idle' : current);
    }, [draftManifestText]);
    const studioKindOptions: Array<{ id: AppKind; title: string; short: string; description: string }> = [
        {
            id: 'enterprise_approval_app',
            title: appKinds.enterprise_approval_app[zh ? 'zh' : 'en'],
            short: zh ? '\u542b\u5ba1\u6279\u6d41\u7a0b\u548c\u72b6\u6001\u6d41\u8f6c' : 'Approval workflow and status tracking',
            description: zh ? '\u7528\u5bf9\u8bdd\u5b9a\u4e49\u6570\u636e\u5bf9\u8c61\u3001\u89c6\u56fe\u3001\u52a8\u4f5c\u548c\u9875\u9762\uff0c\u7531 Agent \u603b\u7ed3\u6210 App manifest\u3002' : 'Define entities, views, actions, and screens through chat; Agent writes an app manifest.',
        },
        {
            id: 'enterprise_normal_app',
            title: appKinds.enterprise_normal_app[zh ? 'zh' : 'en'],
            short: zh ? '\u4f01\u4e1a\u6570\u636e\u548c\u64cd\u4f5c\u5de5\u4f5c\u53f0' : 'Business data and action workspace',
            description: zh ? '\u5c06\u4f01\u4e1a\u540e\u53f0\u6570\u636e\u3001\u64cd\u4f5c\u548c\u67e5\u8be2\u5c01\u88c5\u6210\u53ef\u89c6\u5316\u5de5\u4f5c\u53f0\uff0c\u4e0d\u4ea7\u751f\u5ba1\u6279\u5b9e\u4f8b\u3002' : 'Wrap enterprise backend data, actions, and query views as a visual workbench without approval instances.',
        },
        {
            id: 'tool_app',
            title: appKinds.tool_app[zh ? 'zh' : 'en'],
            short: zh ? 'Skill \u56fa\u5b9a\u4e3a\u4e0a\u4f20\u3001\u53c2\u6570\u548c\u8f93\u51fa\u754c\u9762' : 'Skill UI with upload, parameters, and output',
            description: zh ? '\u628a\u590d\u6742 Skill \u56fa\u5b9a\u6210\u4e0a\u4f20\u3001\u53c2\u6570\u3001\u8f93\u51fa\u7684\u4f4e\u95e8\u69db\u754c\u9762\u3002' : 'Wrap a complex skill as upload, parameters, and output UI.',
        },
        {
            id: 'automation_app',
            title: appKinds.automation_app[zh ? 'zh' : 'en'],
            short: zh ? '\u540c\u6b65\u3001\u91c7\u96c6\u3001\u76d1\u63a7\u7b49\u957f\u8fd0\u884c\u5165\u53e3' : 'Long-running sync, collection, and monitor entry',
            description: zh ? '\u628a\u540c\u6b65\u3001\u91c7\u96c6\u3001\u76d1\u63a7\u7b49\u957f\u8fd0\u884c\u80fd\u529b\u56fa\u5b9a\u6210\u5e94\u7528\u5165\u53e3\u3002' : 'Expose sync, collection, and monitoring flows as app entries.',
        },
    ];
    const categorySuggestions = kind === 'tool_app'
        ? (zh ? ['\u6587\u6863\u5904\u7406', '\u6570\u636e\u5206\u6790', '\u6cd5\u52a1', '\u8d22\u52a1'] : ['Document', 'Analytics', 'Legal', 'Finance'])
        : kind === 'automation_app'
            ? (zh ? ['\u81ea\u52a8\u5316', '\u6570\u636e\u96c6\u6210', '\u6570\u636e\u91c7\u96c6', '\u8fd0\u7ef4'] : ['Automation', 'Integration', 'Collection', 'Operations'])
            : ['OA', zh ? '\u8d22\u52a1' : 'Finance', 'CRM', zh ? '\u8fdb\u9500\u5b58' : 'Inventory'];
    const createApp = () => {
        const cleanName = name.trim();
        if (!cleanName) return;
        const id = makeLocalAppId(cleanName);
        onCreateApp(buildDraftApp(id, cleanName));
        setName('');
        setDescription('');
        setAboutAuthor('');
        setAboutCopyright('');
        setAboutWebsite('');
        setAboutEmail('');
        setSkillFields([]);
        setMultipleFiles(false);
        setResultContractDraft(undefined);
        setTestProtocolDraft(undefined);
        setUiNavigation(defaultEnterpriseNavigation(kind));
        setUiColumns(defaultEnterpriseColumns(kind));
    };
    const persistSkillAppDefinition = async (runEvidence?: AppRunHistoryEntry | null) => {
        const cleanName = name.trim();
        const skillID = skillDefinitionTargetID;
        if (!cleanName || !skillID || !canWriteSkillDefinition) return null;
        const appID = makeSkillAppDefinitionId(cleanName);
        const app = buildDraftApp(appID, cleanName, skillID, 'maclaw.app.json');
        const skillBoundApp: AppEntry = {
            ...app,
            importedRunEvidence: runEvidence || app.importedRunEvidence,
            manifest: app.manifest ? {
                ...app.manifest,
                installUnit: app.manifest.installUnit === 'builtin' ? 'skill' : app.manifest.installUnit,
                skill: appSkillRuntimeBinding({ ...app.manifest, skill: app.manifest.skill ? { ...app.manifest.skill, appDefinitionFile: 'maclaw.app.json' } : app.manifest.skill }, skillID),
            } : app.manifest,
        };
        const manifestText = JSON.stringify(appToManifest(skillBoundApp), null, 2);
        await SaveMaclawAppDefinitionForSkill(skillID, manifestText);
        onCreateApp({
            ...skillBoundApp,
            id: skillPanelAppID(skillID, appID),
            source: 'skill',
            studioOrigin: 'app_studio',
        }, { keepStudioCreate: true });
        return skillID;
    };
    const saveAsSkillApp = async () => {
        if (skillAppSaveState === 'saving') return;
        setSkillAppSaveState('saving');
        setSkillAppSaveMessage('');
        try {
            const skillID = await persistSkillAppDefinition();
            if (!skillID) {
                setSkillAppSaveState('idle');
                return;
            }
            setSkillAppSaveState('saved');
            setSkillMarketUploadState('idle');
            setSkillAppSaveMessage(zh ? `\u5df2\u4fdd\u5b58\u5230 ${skillID}/maclaw.app.json` : `Saved to ${skillID}/maclaw.app.json`);
        } catch (error) {
            setSkillAppSaveState('error');
            setSkillAppSaveMessage(error instanceof Error ? error.message : String(error || 'Save failed'));
        }
    };
    const uploadSelectedSkillApp = async () => {
        const skillID = skillDefinitionTargetID;
        if (!skillID || skillMarketUploadState === 'uploading') return;
        if (!skillDefinitionTargetInstalled) {
            setSkillMarketUploadState('error');
            setSkillAppSaveState('error');
            setSkillAppSaveMessage(zh ? '\u8bf7\u5148\u9009\u62e9\u5df2\u5b89\u88c5 Skill\uff0c\u518d\u4fdd\u5b58\u6216\u4e0a\u4f20\u5e94\u7528\u5b9a\u4e49\u3002' : 'Choose an installed Skill before saving or uploading this app definition.');
            return;
        }
        const cleanName = name.trim();
        if (!cleanName) return;
        const appID = makeSkillAppDefinitionId(cleanName);
        const draftPanelApp = buildDraftApp(appID, cleanName, skillID, 'maclaw.app.json');
        const panelApp: AppEntry = {
            ...draftPanelApp,
            id: skillPanelAppID(skillID, appID),
            source: 'skill',
            studioOrigin: 'app_studio',
            manifest: draftPanelApp.manifest ? {
                ...draftPanelApp.manifest,
                installUnit: draftPanelApp.manifest.installUnit === 'builtin' ? 'skill' : draftPanelApp.manifest.installUnit,
                skill: appSkillRuntimeBinding({ ...draftPanelApp.manifest, skill: draftPanelApp.manifest.skill ? { ...draftPanelApp.manifest.skill, appDefinitionFile: 'maclaw.app.json' } : draftPanelApp.manifest.skill }, skillID),
            } : draftPanelApp.manifest,
        };
        const currentRunEvidence = latestAppRunEvidence(panelApp);
        if (!currentRunEvidence) {
            setSkillMarketUploadState('error');
            setSkillAppSaveState('error');
            setSkillAppSaveMessage(zh ? '\u8bf7\u5148\u4fdd\u5b58\u5230 Skill\uff0c\u5e76\u5728\u5e94\u7528\u9762\u677f\u6210\u529f\u6d4b\u8bd5\u4e00\u6b21\u5f53\u524d\u7248\u672c\uff0c\u518d\u4e0a\u4f20\u5230 SkillMarket\u3002' : 'Save to Skill and run this version successfully in the app panel before uploading to SkillMarket.');
            return;
        }
        setSkillMarketUploadState('uploading');
        setSkillAppSaveMessage('');
        try {
            setSkillAppSaveState('saving');
            const savedSkillID = await persistSkillAppDefinition(currentRunEvidence);
            if (!savedSkillID) {
                setSkillMarketUploadState('idle');
                setSkillAppSaveState('idle');
                return;
            }
            setSkillAppSaveState('saved');
            const submissionID = await UploadNLSkillToMarket(savedSkillID);
            setSkillMarketUploadState('uploaded');
            setSkillAppSaveMessage(zh ? `\u5df2\u63d0\u4ea4\u5230 SkillMarket: ${submissionID}` : `Submitted to SkillMarket: ${submissionID}`);
        } catch (error) {
            setSkillMarketUploadState('error');
            setSkillAppSaveState('error');
            setSkillAppSaveMessage(error instanceof Error ? error.message : String(error || 'Upload failed'));
        }
    };
    return (
        <>
            <section className="apps-create-form">
                <div className="apps-definition__title">{zh ? '\u5feb\u901f\u521b\u5efa\u9762\u677f\u5e94\u7528' : 'Quick create panel app'}</div>
                <section className="apps-studio-kind" aria-label={zh ? '\u5e94\u7528\u7c7b\u578b' : 'App type'}>
                    <div className="apps-studio-kind__header">
                        <div>
                            <div className="apps-definition__title">{zh ? '\u5148\u9009\u5e94\u7528\u7c7b\u578b' : 'Start with app type'}</div>
                            <span>{zh ? '\u7c7b\u578b\u4f1a\u51b3\u5b9a Skill\u3001\u8f93\u5165\u8f93\u51fa\u548c\u8fd0\u884c\u5e03\u5c40\u7684\u9ed8\u8ba4\u503c\u3002' : 'Type sets the default Skill, input/output, and runtime layout.'}</span>
                        </div>
                    </div>
                    <div className="apps-studio-kind__list" role="group" aria-label={zh ? '\u5e94\u7528\u7c7b\u578b' : 'App type'}>
                        {studioKindOptions.map((option) => (
                            <button
                                key={option.id}
                                className={`apps-studio-kind__choice ${kind === option.id ? 'is-active' : ''}`}
                                type="button"
                                aria-pressed={kind === option.id}
                                aria-label={option.title}
                                title={option.description}
                                onClick={() => selectKind(option.id, true)}
                            >
                                <strong>{option.title}</strong>
                                <span>{option.short}</span>
                            </button>
                        ))}
                    </div>
                </section>
                <div className="apps-prompt-draft">
                    <div className="apps-preview-title-row">
                        <div className="apps-definition__title">{text.promptDraft}</div>
                        <button className="apps-secondary-button" type="button" disabled={!draftPrompt.trim()} onClick={generateDraftFromPrompt}>{text.generateDraft}</button>
                    </div>
                    <textarea value={draftPrompt} onChange={(event) => setDraftPrompt(event.target.value)} placeholder={text.draftPromptPlaceholder} />
                </div>
                <div className="apps-form-row">
                    <label>{zh ? '\u540d\u79f0' : 'Name'}</label>
                    <input value={name} onChange={(event) => setName(event.target.value)} placeholder={zh ? '\u4f8b\uff1a\u5408\u540c\u5f52\u6863' : 'Example: Contract filing'} />
                </div>
                <div className="apps-form-row">
                    <label>{zh ? '\u5206\u7c7b' : 'Category'}</label>
                    <div className="apps-category-editor">
                        <input value={category} onChange={(event) => setCategory(event.target.value)} />
                        <div className="apps-category-chips" role="group" aria-label={zh ? '\u5e38\u7528\u5206\u7c7b' : 'Common categories'}>
                            {categorySuggestions.map((item) => (
                                <button
                                    key={item}
                                    className={category === item ? 'is-active' : ''}
                                    type="button"
                                    aria-pressed={category === item}
                                    onClick={() => setCategory(item)}
                                >
                                    {item}
                                </button>
                            ))}
                        </div>
                    </div>
                </div>
                <div className="apps-form-row">
                    <label>{zh ? '\u56fe\u6807' : 'Icon'}</label>
                    <div className="apps-icon-picker" role="group" aria-label={zh ? '\u56fe\u6807' : 'Icon'}>
                        {appIconNames.map((item) => {
                            const label = appIconLabel(item, lang);
                            return (
                                <button
                                    key={item}
                                    className={`apps-icon-choice ${icon === item ? 'is-active' : ''}`}
                                    type="button"
                                    title={label}
                                    aria-label={label}
                                    aria-pressed={icon === item}
                                    onClick={() => setIcon(item)}
                                >
                                    <Icon name={item} />
                                </button>
                            );
                        })}
                    </div>
                </div>
                <div className="apps-form-row">
                    <label>{text.appColor}</label>
                    <AppAccentPicker value={accent} lang={lang} onChange={setAccent} />
                </div>
                {kind === 'tool_app' && (
                    <>
                        <div className="apps-form-row">
                            <label>{zh ? '\u73b0\u6709 Skill' : 'Existing Skill'}</label>
                            <StudioSkillPicker
                                label={zh ? '\u73b0\u6709 Skill' : 'Existing Skill'}
                                value={selectedSkill}
                                installedSkills={appReadySkills}
                                lang={lang}
                                mode="app"
                                testId="studio-tool-skill-picker"
                                onSelect={(choice) => {
                                    setSelectedSkill(choice.id);
                                    setSelectedSkillSource(choice.source);
                                    setSkillAppSaveState('idle');
                                    setSkillAppSaveMessage('');
                                    setSkillMarketUploadState('idle');
                                }}
                            />
                        </div>
                        <div className="apps-form-row">
                            <label>{zh ? '\u8f93\u5165\u6a21\u5f0f' : 'Input mode'}</label>
                            <select value={inputMode} onChange={(event) => {
                                const nextMode = event.target.value as 'file' | 'form' | 'mixed';
                                setInputMode(nextMode);
                                if (nextMode === 'form') setMultipleFiles(false);
                            }}>
                                <option value="file">{zh ? '\u6587\u4ef6\u4e0a\u4f20' : 'File upload'}</option>
                                <option value="form">{zh ? '\u8868\u5355\u53c2\u6570' : 'Form parameters'}</option>
                                <option value="mixed">{zh ? '\u6587\u4ef6 + \u8868\u5355' : 'File + form'}</option>
                            </select>
                        </div>
                        {inputMode !== 'form' && (
                            <div className="apps-form-row">
                                <label>{zh ? '\u591a\u6587\u4ef6' : 'Multiple files'}</label>
                                <label className="apps-checkbox-field">
                                    <input type="checkbox" checked={multipleFiles} onChange={(event) => setMultipleFiles(event.target.checked)} />
                                    <span>{zh ? '\u5141\u8bb8\u4e00\u6b21\u9009\u62e9\u591a\u4e2a\u6587\u4ef6' : 'Allow selecting several files'}</span>
                                </label>
                            </div>
                        )}
                        <div className="apps-form-row">
                            <label>{zh ? '\u8f93\u51fa\u683c\u5f0f' : 'Output modes'}</label>
                            <div className="apps-output-mode-picker" role="group" aria-label={zh ? '\u8f93\u51fa\u683c\u5f0f' : 'Output modes'}>
                                {['docx', 'xlsx', 'pdf', 'json', 'txt'].map((mode) => (
                                    <label key={mode} className="apps-output-mode-choice">
                                        <input
                                            type="checkbox"
                                            checked={outputModes.includes(mode)}
                                            onChange={(event) => {
                                                setOutputModes((current) => {
                                                    const next = event.target.checked ? [...current, mode] : current.filter((item) => item !== mode);
                                                    return next.length > 0 ? normalizeOutputModes(next) : current;
                                                });
                                            }}
                                        />
                                        <span>{outputModeLabel(mode)}</span>
                                    </label>
                                ))}
                            </div>
                        </div>
                        {(inputMode === 'form' || inputMode === 'mixed') && (
                            <ToolFieldEditor fields={skillFields} lang={lang} onChange={setSkillFields} />
                        )}
                    </>
                )}
                {isEnterpriseAppKind(kind) && (
                    <section className="apps-layout-designer" aria-label={zh ? '\u80fd\u529b\u4e0e\u4f9d\u8d56' : 'Capabilities and dependencies'}>
                        <div className="apps-preview-title-row">
                            <div className="apps-definition__title">{zh ? '\u80fd\u529b\u4e0e\u4f9d\u8d56' : 'Capabilities and dependencies'}</div>
                            <span className="apps-count">appSkill ·  workflow_skill</span>
                        </div>
                        <div className="apps-capability-skill-grid">
                            <div className="apps-capability-skill-card">
                                <div className="apps-capability-skill-card__head">
                                    <label>{zh ? '\u5e94\u7528 Skill' : 'App Skill'}</label>
                                </div>
                                <StudioSkillPicker
                                    label={zh ? '\u5e94\u7528 Skill' : 'App Skill'}
                                    value={appSkillID}
                                    installedSkills={appReadySkills}
                                    lang={lang}
                                    mode="app"
                                    testId="studio-app-skill-id"
                                    onSelect={(choice) => {
                                        setAppSkillID(choice.id);
                                        setAppSkillSource(choice.source === 'installed' ? 'local' : choice.source);
                                    }}
                                />
                            </div>
                            {isEnterpriseApprovalAppKind(kind) && (
                                <div className="apps-capability-skill-card">
                                    <div className="apps-capability-skill-card__head">
                                        <label>{zh ? '\u5ba1\u6279 workflow Skill' : 'Approval workflow skill'}</label>
                                        <button className="apps-secondary-button apps-skill-design-button" type="button" title={zh ? '\u6253\u5f00\u5ba1\u6279\u5de5\u4f5c\u6d41\u8bbe\u8ba1\u5668' : 'Open approval workflow designer'} onClick={() => void openApprovalWorkflowDesigner()}>
                                            {zh ? '\u8bbe\u8ba1' : 'Design'}
                                        </button>
                                    </div>
                                    <StudioSkillPicker
                                        label={zh ? '\u5ba1\u6279 workflow Skill' : 'Approval workflow skill'}
                                        value={workflowSkillID}
                                        installedSkills={approvalWorkflowSkills}
                                        lang={lang}
                                        mode="approvalWorkflow"
                                        testId="studio-workflow-skill-id"
                                        onSelect={(choice) => {
                                            setWorkflowSkillID(choice.id);
                                            setDependencySource(choice.source === 'installed' ? 'local' : choice.source);
                                            setDependencyInstallRef(choice.source === 'installed' ? '' : choice.id);
                                        }}
                                    />
                                </div>
                            )}
                        </div>
                        <div className="apps-layout-designer__grid apps-capability-field-grid">
                            {kind === 'enterprise_normal_app' && (
                                <>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u4e1a\u52a1\u57df' : 'DataSrv domain'}</label>
                                        <input data-testid="studio-business-domain" value={businessDomain} onChange={(event) => setBusinessDomain(event.target.value)} placeholder="sales" />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u5bf9\u8c61\u89d2\u8272' : 'Object role'}</label>
                                        <input data-testid="studio-business-object-role" value={businessObjectRole} onChange={(event) => setBusinessObjectRole(event.target.value)} placeholder="customer" />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u9ed8\u8ba4\u52a8\u4f5c' : 'Default action'}</label>
                                        <input data-testid="studio-business-preferred-action" value={businessPreferredAction} onChange={(event) => setBusinessPreferredAction(event.target.value)} placeholder="sales.customer_upsert" />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u9ed8\u8ba4\u89c6\u56fe' : 'Default view'}</label>
                                        <input data-testid="studio-business-preferred-view" value={businessPreferredView} onChange={(event) => setBusinessPreferredView(event.target.value)} placeholder="sales.customer_directory" />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u62a5\u8868' : 'Report'}</label>
                                        <input data-testid="studio-business-preferred-report" value={businessPreferredReport} onChange={(event) => setBusinessPreferredReport(event.target.value)} placeholder="sales.customer_activity" />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u770b\u677f' : 'Dashboard'}</label>
                                        <input data-testid="studio-business-preferred-dashboard" value={businessPreferredDashboard} onChange={(event) => setBusinessPreferredDashboard(event.target.value)} placeholder="sales.overview" />
                                    </div>
                                </>
                            )}
                            {isEnterpriseApprovalAppKind(kind) && (
                                <>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u7248\u672c' : 'Version'}</label>
                                        <input data-testid="studio-workflow-skill-version" value={workflowSkillVersion} onChange={(event) => setWorkflowSkillVersion(event.target.value)} placeholder="1.0.0" />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u5ba1\u6279\u4e8b\u4ef6' : 'Approval event'}</label>
                                        <input data-testid="studio-approval-event" value={approvalEvent} onChange={(event) => setApprovalEvent(event.target.value)} placeholder="record.submitted" />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u5bf9\u8c61\u89d2\u8272' : 'Object role'}</label>
                                        <input data-testid="studio-approval-object-role" value={approvalObjectRole} onChange={(event) => setApprovalObjectRole(event.target.value)} placeholder="record" />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u4f9d\u8d56\u6765\u6e90' : 'Dependency source'}</label>
                                        <div className="apps-skill-source-readonly" data-testid="studio-dependency-source">{dependencySource || 'local'}</div>
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{zh ? '\u5b89\u88c5\u5f15\u7528' : 'Install ref'}</label>
                                        <input data-testid="studio-dependency-install-ref" value={dependencyInstallRef} onChange={(event) => setDependencyInstallRef(event.target.value)} placeholder={zh ? '\u80fd\u529b ID ·  GitHub URL' : 'Capability ID ·  GitHub URL'} />
                                    </div>
                                </>
                            )}
                        </div>
                    </section>
                )}
                {isEnterpriseApprovalAppKind(kind) && <WorkflowMappingDesigner value={workflowMapping} onChange={setWorkflowMapping} lang={lang} />}
                {isEnterpriseAppKind(kind) && <EnterpriseUIConfigDesigner kind={kind} navigation={uiNavigation} columns={uiColumns} onNavigationChange={setUiNavigation} onColumnsChange={setUiColumns} lang={lang} testIdPrefix="studio" />}
                <StudioLayoutDesigner kind={kind} value={studioLayoutValue} onChange={updateStudioLayout} lang={lang} />
                <ResultContractDesigner contract={draftResultContract} onChange={setResultContractDraft} lang={lang} testIdPrefix="studio" />
                <TestProtocolDesigner protocol={draftTestProtocol} onChange={setTestProtocolDraft} lang={lang} testIdPrefix="studio" kind={kind} />
                <div className="apps-form-row apps-form-row--description">
                    <label>{zh ? '\u63cf\u8ff0' : 'Description'}</label>
                    <textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder={zh ? '\u7528\u4e8e tooltip \u548c\u53f3\u4fa7\u8fd0\u884c\u533a\u8bf4\u660e' : 'Used in tooltip and right runtime area'} />
                </div>
                <AppAboutInfoFields
                    text={text}
                    value={{ author: aboutAuthor, copyright: aboutCopyright, website: aboutWebsite, email: aboutEmail }}
                    onChange={updateAboutInfo}
                />
                <div className="apps-actions">
                    {kind === 'tool_app' && canWriteSkillDefinition ? (
                        <>
                            <button className="apps-primary-button" type="button" disabled={!name.trim() || skillAppSaveState === 'saving'} onClick={() => void saveAsSkillApp()}>
                                {skillAppSaveState === 'saving' ? (zh ? '\u4fdd\u5b58\u4e2d...' : 'Saving...') : (zh ? '\u4fdd\u5b58\u5230 Skill' : 'Save to Skill')}
                            </button>
                            <button className="apps-secondary-button" type="button" disabled={!skillDefinitionTargetID || !skillDefinitionTargetInstalled || skillMarketUploadState === 'uploading'} onClick={() => void uploadSelectedSkillApp()}>
                                {skillMarketUploadState === 'uploading' ? (zh ? '\u4e0a\u4f20\u4e2d...' : 'Uploading...') : (zh ? '\u4e0a\u4f20\u5230 SkillMarket' : 'Upload to SkillMarket')}
                            </button>
                            <button className="apps-secondary-button" type="button" disabled={!name.trim()} onClick={createApp}>{zh ? '\u4ec5\u6dfb\u52a0\u5230\u9762\u677f' : 'Add to panel only'}</button>
                        </>
                    ) : (
                        <>
                            <button className="apps-primary-button" type="button" disabled={!name.trim()} onClick={createApp}>{text.createTab}</button>
                            {(kind === 'tool_app' || isEnterpriseAppKind(kind)) && (
                                <>
                                    <button className="apps-secondary-button" type="button" disabled={!canWriteSkillDefinition || skillAppSaveState === 'saving'} onClick={() => void saveAsSkillApp()}>
                                        {skillAppSaveState === 'saving' ? (zh ? '\u4fdd\u5b58\u4e2d...' : 'Saving...') : (zh ? '\u4fdd\u5b58\u5230 Skill' : 'Save to Skill')}
                                    </button>
                                    <button className="apps-secondary-button" type="button" disabled={!skillDefinitionTargetID || !skillDefinitionTargetInstalled || skillMarketUploadState === 'uploading'} onClick={() => void uploadSelectedSkillApp()}>
                                        {skillMarketUploadState === 'uploading' ? (zh ? '\u4e0a\u4f20\u4e2d...' : 'Uploading...') : (zh ? '\u4e0a\u4f20\u5230 SkillMarket' : 'Upload to SkillMarket')}
                                    </button>
                                </>
                            )}
                        </>
                    )}
                </div>
                {skillAppSaveMessage && (
                    <div className="apps-skill-save-message" data-state={skillAppSaveState} role={skillAppSaveState === 'error' ? 'alert' : 'status'}>{skillAppSaveMessage}</div>
                )}
                <section className="apps-create-preview" aria-label={text.manifestPreview}>
                    <div className="apps-create-preview__head">
                        <button
                            className="apps-create-preview__toggle"
                            type="button"
                            aria-expanded={manifestPreviewOpen}
                            aria-controls="apps-create-manifest-preview"
                            onClick={() => setManifestPreviewOpen((current) => !current)}
                        >
                            <span aria-hidden="true">{manifestPreviewOpen ? '\u25be' : '\u25b8'}</span>
                            <span>
                                <strong>{text.manifestPreview}</strong>
                                <small>{text.manifestPreviewHint}</small>
                            </span>
                        </button>
                        <button className="apps-secondary-button" type="button" onClick={async () => {
                            await copyTextToClipboard(draftManifestText);
                            setCopyState('copied');
                        }}>{copyState === 'copied' ? text.copied : text.copy}</button>
                    </div>
                    <pre id="apps-create-manifest-preview" hidden={!manifestPreviewOpen}>{draftManifestText}</pre>
                </section>
            </section>
        </>
    );
};

type PublishCheck = {
    label: string;
    ok: boolean;
    detail: string;
};

function buildPublishChecks(app: AppEntry, lang?: string): PublishCheck[] {
    const text = isZh(lang) ? labels.zh : labels.en;
    const zh = isZh(lang);
    const manifest = app.manifest;
    const expectedLaunch = defaultLaunchModeForKind(app.kind);
    const hasBinding = isEnterpriseAppKind(app.kind)
        ? !!manifest?.datasrv?.domain
        : app.kind === 'tool_app'
            ? !!manifest?.skill?.id
            : true;
    const evidence = latestAppRunEvidence(app) || latestAvailableAppRunEvidence(app);
    const evidenceFreshness = appRunEvidenceFreshnessCheck(app, lang);
    const resultCoverage = appRunEvidenceContractCoverage(app, evidence, lang);
    const designConsistency = appRunEvidenceDesignConsistencyCheck(app, evidence, lang);
    const approvalInstanceEvidence = appRunEvidenceApprovalInstanceCheck(app, evidence, lang);
    const dependencyVerification = appDependencyVerificationPublishCheck(app, lang);
    const workflowContract = workflowContractForApp(app);
    const hasWorkflowContract = app.kind !== 'enterprise_approval_app' || !!workflowContract;
    const workspaceLayout = appWorkspaceLayoutEvidence(app);
    const missingLayoutRoles = missingWorkspaceRegionRoles(app);
    const hasWorkspaceLayout = workspaceLayout.schema === 'maclaw.app.ui.v1' && !!workspaceLayout.entry && workspaceLayout.regionCount > 0 && missingLayoutRoles.length === 0;
    const hasTestProtocol = appHasPublishableTestProtocol(app);
    return [
        {
            label: zh ? '\u57fa\u672c\u4fe1\u606f' : 'Basic information',
            ok: !!app.id && !!app.name && !!app.category && !!app.icon,
            detail: zh ? '\u540d\u79f0\u3001\u5206\u7c7b\u3001\u56fe\u6807\u5b8c\u6574' : 'Name, category, and icon are present',
        },
        {
            label: zh ? 'Manifest \u7ed3\u6784' : 'Manifest structure',
            ok: !!manifest && manifest.privateMarker === 'x_maclaw_apps' && manifest.entryKind === app.kind && manifest.launchMode === expectedLaunch,
            detail: zh ? `${app.kind} -> ${expectedLaunch}` : `${app.kind} -> ${expectedLaunch}`,
        },
        {
            label: zh ? '\u7ed1\u5b9a\u80fd\u529b' : 'Capability binding',
            ok: hasBinding,
            detail: isEnterpriseAppKind(app.kind)
                ? (manifest?.datasrv?.domain || (zh ? '\u7f3a\u5c11 DataSrv domain' : 'Missing DataSrv domain'))
                : app.kind === 'tool_app'
                    ? (manifest?.skill?.id || (zh ? '\u7f3a\u5c11 Skill id' : 'Missing Skill id'))
                    : (zh ? '\u81ea\u52a8\u5316\u63a7\u5236\u53f0' : 'Automation console'),
        },
        {
            label: text.skillDependencies,
            ok: dependencyVerification.ok,
            detail: dependencyVerification.detail,
        },
        {
            label: zh ? 'Workspace layout' : 'Workspace layout',
            ok: hasWorkspaceLayout,
            detail: appWorkspaceLayoutPublishSummary(app, lang),
        },
        {
            label: text.workflowContract,
            ok: hasWorkflowContract,
            detail: app.kind === 'enterprise_approval_app' ? appWorkflowContractPublishSummary(app, lang) : (zh ? '\u975e\u5ba1\u6279\u5e94\u7528\u4e0d\u9700\u8981\u5ba1\u6279\u8fd0\u884c\u5951\u7ea6' : 'Not required for non-approval apps'),
        },
        {
            label: zh ? '\u7ed3\u679c\u5951\u7ea6' : 'Result contract',
            ok: appResultContractForManifest(app).types.length > 0,
            detail: appResultContractPublishSummary(app, lang),
        },
        {
            label: zh ? '\u6d4b\u8bd5\u534f\u8bae' : 'Test protocol',
            ok: hasTestProtocol,
            detail: appTestProtocolPublishSummary(app, lang),
        },
        {
            label: zh ? '\u8fd0\u884c\u8bc1\u636e' : 'Run evidence',
            ok: evidenceFreshness.ok && designConsistency.ok && resultCoverage.ok,
            detail: !evidenceFreshness.ok ? evidenceFreshness.detail : !designConsistency.ok ? designConsistency.detail : resultCoverage.detail,
        },
        {
            label: zh ? '\u5ba1\u6279\u5b9e\u4f8b\u8bc1\u636e' : 'Approval instance evidence',
            ok: approvalInstanceEvidence.ok,
            detail: approvalInstanceEvidence.detail,
        },
    ];
}

const PublishPane = ({ apps, lang, onFixApp, onInstallDependencies, onInstallApprovedHubApp, onSyncHubAppGovernance }: { apps: AppEntry[]; lang?: string; onFixApp: (appId: string) => void; onInstallDependencies: (appId: string) => void; onInstallApprovedHubApp: (capabilityID: string, name: string) => Promise<ApprovedHubAppInstallResult>; onSyncHubAppGovernance: (summaries: AppPackageSubmissionSummary[]) => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const zh = isZh(lang);
    const [copyState, setCopyState] = useState<'idle' | 'copied'>('idle');
    const [submissions, setSubmissions] = useState<Record<string, AppPublishSubmission>>(() => readPublishSubmissions());
    const [submittingAppId, setSubmittingAppId] = useState('');
    const [submitErrors, setSubmitErrors] = useState<Record<string, string>>({});
    const [queueStatus, setQueueStatus] = useState<'loading' | 'ready' | 'unsupported' | 'error'>('loading');
    const [queueRefreshing, setQueueRefreshing] = useState(false);
    const [queueRefreshedAt, setQueueRefreshedAt] = useState('');
    const [queueSummaries, setQueueSummaries] = useState<AppPackageSubmissionSummary[]>([]);
    const [queuePackageCopyingId, setQueuePackageCopyingId] = useState('');
    const [queuePackageCopiedId, setQueuePackageCopiedId] = useState('');
    const [queueAuditCopiedId, setQueueAuditCopiedId] = useState('');
    const [queuePackageErrorId, setQueuePackageErrorId] = useState('');
    const [queueSyncingId, setQueueSyncingId] = useState('');
    const [queueSyncErrorId, setQueueSyncErrorId] = useState('');
    const [queueInstallingId, setQueueInstallingId] = useState('');
    const [queueInstalledId, setQueueInstalledId] = useState('');
    const [queueInstallErrorId, setQueueInstallErrorId] = useState('');
    const [queueInstallErrorMessages, setQueueInstallErrorMessages] = useState<Record<string, string>>({});
    const [queueInstallResults, setQueueInstallResults] = useState<Record<string, ApprovedHubAppInstallResult>>({});
    const [queueDetailOpenId, setQueueDetailOpenId] = useState('');
    const [queueDetailLoadingId, setQueueDetailLoadingId] = useState('');
    const [queueDetailRecords, setQueueDetailRecords] = useState<Record<string, Record<string, unknown>>>({});
    const publishApps = apps.filter((app) => app.source === 'local' || app.studioOrigin === 'app_studio');
    const publishChecksById = new Map(publishApps.map((app) => [app.id, buildPublishChecks(app, lang)] as const));
    const readyPublishApps = publishApps.filter((app) => (publishChecksById.get(app.id) || []).every((item) => item.ok));
    const packageGovernanceOverrides = Object.fromEntries(
        readyPublishApps
            .map((app) => [app.id, { dependencyVerification: appInstallEvidenceDependencyVerificationPlan(app) }] as const)
            .filter(([, override]) => !!override.dependencyVerification),
    );
    const packageText = JSON.stringify(appsToPackManifest(readyPublishApps, submissions, packageGovernanceOverrides), null, 2);
    const refreshSubmissionQueue = async () => {
        setQueueRefreshing(true);
        try {
            const summaries = await listMaclawAppPackageSubmissions(8);
            if (summaries === null) {
                setQueueStatus('unsupported');
                setQueueSummaries([]);
                return;
            }
            setQueueStatus('ready');
            setQueueRefreshedAt(new Date().toISOString());
            setQueueSummaries(summaries);
            onSyncHubAppGovernance(summaries);
            const appIds = new Set(publishApps.map((app) => app.id));
            setSubmissions((current) => {
                const next = mergePublishSubmissionsFromQueue(current, summaries, appIds);
                if (next !== current) writePublishSubmissions(next);
                return next;
            });
        } catch {
            setQueueStatus('error');
            setQueueSummaries([]);
        } finally {
            setQueueRefreshing(false);
        }
    };
    useEffect(() => {
        void refreshSubmissionQueue();
    }, []);
    const submitApp = async (app: AppEntry) => {
        const submittedAt = new Date().toISOString();
        setSubmittingAppId(app.id);
        setSubmitErrors((current) => {
            const next = { ...current };
            delete next[app.id];
            return next;
        });
        const failedCheck = buildPublishChecks(app, lang).find((check) => !check.ok);
        if (failedCheck) {
            setSubmitErrors((current) => ({
                ...current,
                [app.id]: `${failedCheck.label}: ${failedCheck.detail}`,
            }));
            setSubmittingAppId('');
            return;
        }
        let dependencyPlan: BackendAppInstallPlan | undefined;
        try {
            const evidenceDependencyPlan = appInstallEvidenceDependencyVerificationPlan(app);
            const backendDependencyPlan = await PlanMaclawAppInstall(JSON.stringify(appToManifest(app)));
            const appIDs = appDependencyVerificationAppIDs(app);
            dependencyPlan = appInstallPlanHasAppScopedSignals(backendDependencyPlan, appIDs)
                ? backendDependencyPlan
                : evidenceDependencyPlan || backendDependencyPlan;
            if (workflowContractHasIssueForAppIDs(dependencyPlan, appIDs)) {
                throw new Error(workflowContractIssueMessageForAppIDs(dependencyPlan, appIDs, text));
            }
            if (hasMissingRequiredBackendDependency(dependencyPlan, appIDs)) {
                throw new Error(runtimeInstallPlanBlockMessage(app, dependencyPlan, text, lang));
            }
            if (governanceReviewHasIssueForAppIDs(dependencyPlan, appIDs)) {
                throw new Error(governanceReviewIssueMessageForAppIDs(dependencyPlan, appIDs, text));
            }
            const dependencyCheck = appDependencyVerificationPublishCheckForPlan(app, dependencyPlan, lang);
            if (!dependencyCheck.ok) {
                throw new Error(`${text.skillDependencies}: ${dependencyCheck.detail}`);
            }
        } catch (error) {
            setSubmitErrors((current) => ({
                ...current,
                [app.id]: error instanceof Error ? error.message : String(error || text.dependencyPlanError),
            }));
            setSubmittingAppId('');
            return;
        }
        let submission: AppPublishSubmission | null = null;
        try {
            const publishDependencyPlan = dependencyPlan?.dependencies?.length
                ? dependencyPlan
                : appInstallEvidenceDependencyVerificationPlan(app) || dependencyPlan;
            submission = await submitAppPackageToEnterpriseMarket(app, appsToPackManifest([app], {}, publishDependencyPlan ? { [app.id]: { dependencyVerification: publishDependencyPlan } } : {}));
        } catch (error) {
            submission = {
                id: `local-review-${app.id}-${Date.now().toString(36)}`,
                appID: app.id,
                submittedAt,
                status: 'submitted',
                channel: 'local',
                version: normalizeAppVersion(app.version),
                message: `${text.submitReviewLocalFallback} ${error instanceof Error ? error.message : String(error || '')}`.trim(),
            };
        }
        if (!submission) {
            submission = {
                id: `local-review-${app.id}-${Date.now().toString(36)}`,
                appID: app.id,
                submittedAt,
                status: 'submitted',
                channel: 'local',
                version: normalizeAppVersion(app.version),
                message: text.submitReviewLocalFallback,
            };
        }
        setSubmissions((current) => {
            const next = { ...current, [app.id]: normalizeFreshPublishSubmission(submission) };
            writePublishSubmissions(next);
            return next;
        });
        setSubmitErrors((current) => {
            const next = { ...current };
            delete next[app.id];
            return next;
        });
        setCopyState('idle');
        setSubmittingAppId('');
        void refreshSubmissionQueue();
    };
    const withdrawApp = async (appID: string) => {
        const submissionID = submissions[appID]?.id || '';
        try {
            await withdrawMaclawAppPackageSubmission(submissionID);
        } catch {
            // Local UI state can still be withdrawn; the durable queue remains visible if backend removal fails.
        }
        setSubmissions((current) => {
            const next = { ...current };
            delete next[appID];
            writePublishSubmissions(next);
            return next;
        });
        setCopyState('idle');
        void refreshSubmissionQueue();
    };
    const syncQueuedSubmission = async (submissionID: string) => {
        setQueueSyncingId(submissionID);
        setQueueSyncErrorId('');
        setQueuePackageErrorId('');
        try {
            await syncMaclawAppPackageSubmissionToHub(submissionID);
            await refreshSubmissionQueue();
        } catch {
            setQueueSyncErrorId(submissionID);
        } finally {
            setQueueSyncingId('');
        }
    };
    const refreshQueuedSubmissionFromHub = async (submissionID: string) => {
        setQueueSyncingId(submissionID);
        setQueueSyncErrorId('');
        setQueuePackageErrorId('');
        try {
            await refreshMaclawAppPackageSubmissionFromHub(submissionID);
            await refreshSubmissionQueue();
        } catch {
            setQueueSyncErrorId(submissionID);
        } finally {
            setQueueSyncingId('');
        }
    };
    const installApprovedQueuedApp = async (item: AppPackageSubmissionSummary) => {
        const capabilityID = String(item.hubCapabilityID || item.appIDs[0] || '').trim();
        if (!capabilityID) return;
        setQueueInstallingId(item.submissionID);
        setQueueInstallErrorId('');
        setQueueInstallErrorMessages((current) => {
            const next = { ...current };
            delete next[item.submissionID];
            return next;
        });
        setQueueInstalledId('');
        setQueueInstallResults((current) => {
            const next = { ...current };
            delete next[item.submissionID];
            return next;
        });
        try {
            const result = await onInstallApprovedHubApp(capabilityID, item.appNames[0] || item.appIDs[0] || capabilityID);
            setQueueInstallResults((current) => ({ ...current, [item.submissionID]: result }));
            setQueueInstalledId(item.submissionID);
            await refreshSubmissionQueue();
        } catch (error) {
            const message = error instanceof Error ? error.message : String(error || text.approvedHubAppInstallFailed);
            setQueueInstallErrorId(item.submissionID);
            setQueueInstallErrorMessages((current) => ({ ...current, [item.submissionID]: message || text.approvedHubAppInstallFailed }));
        } finally {
            setQueueInstallingId('');
        }
    };
    const copyQueuedPackage = async (submissionID: string) => {
        setQueuePackageCopyingId(submissionID);
        setQueuePackageCopiedId('');
        setQueueAuditCopiedId('');
        setQueuePackageErrorId('');
        try {
            const pkg = await getMaclawAppPackageSubmissionPackage(submissionID);
            if (!pkg) throw new Error(text.queuePackageUnavailable);
            await copyTextToClipboard(JSON.stringify(pkg, null, 2));
            setQueuePackageCopiedId(submissionID);
        } catch {
            setQueuePackageErrorId(submissionID);
        } finally {
            setQueuePackageCopyingId('');
        }
    };
    const copyQueuedAudit = async (submissionID: string) => {
        setQueuePackageCopyingId(submissionID);
        setQueuePackageCopiedId('');
        setQueueAuditCopiedId('');
        setQueuePackageErrorId('');
        try {
            const record = await getMaclawAppPackageSubmissionRecord(submissionID);
            if (!record) throw new Error(text.queuePackageUnavailable);
            await copyTextToClipboard(JSON.stringify(record, null, 2));
            setQueueAuditCopiedId(submissionID);
        } catch {
            setQueuePackageErrorId(submissionID);
        } finally {
            setQueuePackageCopyingId('');
        }
    };
    const toggleQueuedDetail = async (submissionID: string) => {
        setQueuePackageErrorId('');
        if (queueDetailOpenId === submissionID) {
            setQueueDetailOpenId('');
            return;
        }
        setQueueDetailOpenId(submissionID);
        if (queueDetailRecords[submissionID]) return;
        setQueueDetailLoadingId(submissionID);
        try {
            const record = await getMaclawAppPackageSubmissionRecord(submissionID);
            if (!record) throw new Error(text.queuePackageUnavailable);
            setQueueDetailRecords((current) => ({ ...current, [submissionID]: record }));
        } catch {
            setQueuePackageErrorId(submissionID);
        } finally {
            setQueueDetailLoadingId('');
        }
    };
    const canCopyQueuedPackage = hasMaclawAppPackageSubmissionDetailBridge();
    const canSyncQueuedPackage = hasSyncMaclawAppPackageSubmissionBridge();
    const canRefreshQueuedPackageFromHub = hasRefreshMaclawAppPackageSubmissionBridge();
    return (
        <section className="apps-publish">
            <div className="apps-preview-title-row">
                <div>
                    <div className="apps-definition__title">{text.publishChecklist}</div>
                    <p className="apps-publish__subtitle">{text.publishSubtitle}</p>
                </div>
                <button
                    className="apps-secondary-button"
                    type="button"
                    disabled={readyPublishApps.length === 0}
                    onClick={async () => {
                        await copyTextToClipboard(packageText);
                        setCopyState('copied');
                    }}
                >
                    {copyState === 'copied' ? text.copied : text.copySubmitPackage}
                </button>
            </div>
            {publishApps.length === 0 ? (
                <div className="apps-empty">{text.noPublishApps}</div>
            ) : (
                <div className="apps-publish__list">
                    {publishApps.map((app) => {
                        const checks = publishChecksById.get(app.id) || buildPublishChecks(app, lang);
                        const ready = checks.every((item) => item.ok);
                        const submission = submissions[app.id];
                        const submissionStatus = submission ? publishSubmissionStatusLabel(submission, text) : '';
                        const canResubmit = submission?.status === 'review_failed' || !!submission?.modifiedAt;
                        const canWithdraw = !!submission && ['submitted', 'review_failed', 'approved'].includes(submission.status);
                        const isSubmitting = submittingAppId === app.id;
                        const submitError = submitErrors[app.id] || '';
                        const hasDependencyReviewIssue = reviewIssuesIncludeDependency(submission?.reviewIssues);
                        const hasDependencyCheckIssue = checks.some((check) => check.label === text.skillDependencies && !check.ok);
                        return (
                            <article className="apps-publish-card" key={app.id} data-ready={ready ? 'true' : 'false'}>
                                <div className="apps-publish-card__head">
                                    <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><AppIcon icon={app.icon} customIconDataUrl={app.customIconDataUrl} /></span>
                                    <div>
                                        <strong>{app.name}</strong>
                                        <span>{app.category} · {appKinds[app.kind][zh ? 'zh' : 'en']}</span>
                                    </div>
                                    <em>{submission ? submissionStatus : ready ? text.readyToSubmit : text.needsWork}</em>
                                </div>
                                {submission && (
                                    <div className="apps-publish-submission">
                                        <strong>{submissionStatus}</strong>
                                        <span>{text.submissionId}: {submission.id}</span>
                                        <span>{text.appVersion}: v{submission.version || normalizeAppVersion(app.version)}</span>
                                        {submission.riskLevel && <span>{text.riskLevel}: {submission.riskLevel}</span>}
                                        {submission.approvedScopes && submission.approvedScopes.length > 0 && <span>{text.approvedScopes}: {submission.approvedScopes.join(', ')}</span>}
                                        {submission.modifiedAt && <span>{text.localModifiedAt}: {submission.modifiedAt}</span>}
                                        {submission.message && <span>{submission.message}</span>}
                                        {submission.reviewIssues && submission.reviewIssues.length > 0 && (
                                            <span>{text.reviewIssues}: {reviewIssuesSummary(submission.reviewIssues, text)}</span>
                                        )}
                                        <ReviewIssuesPanel issues={submission.reviewIssues} text={text} compact />
                                    </div>
                                )}
                                {submitError && (
                                    <div className="apps-publish-submission apps-publish-submission--error" role="alert">
                                        <strong>{text.dependencyPlanError}</strong>
                                        <span>{submitError}</span>
                                    </div>
                                )}
                                <div className="apps-publish-checks">
                                    <div className="apps-publish-checks__summary">
                                        {isZh(lang)
                                            ? `检查项：${checks.filter(c => c.ok).length}/${checks.length} 通过`
                                            : `Checks: ${checks.filter(c => c.ok).length}/${checks.length} passed`}
                                    </div>
                                    {checks.map((check) => (
                                        <div className="apps-publish-check" data-ok={check.ok ? 'true' : 'false'} key={check.label}>
                                            <span aria-hidden="true">{check.ok ? "OK" : "!"}</span>
                                            <div>
                                                <strong>{check.label}</strong>
                                                <small>{check.detail}</small>
                                            </div>
                                        </div>
                                    ))}
                                </div>
                                <div className="apps-actions">
                                    <button className="apps-primary-button" type="button" disabled={isSubmitting || !ready || (!!submission && !canResubmit)} onClick={() => void submitApp(app)}>
                                        {isSubmitting ? text.submitReviewBusy : submission && !canResubmit ? text.submittedReview : text.submitReview}
                                    </button>
                                    {canWithdraw && (
                                        <button className="apps-secondary-button" type="button" onClick={() => void withdrawApp(app.id)}>
                                            {text.withdrawSubmission}
                                        </button>
                                    )}
                                    {!ready && (
                                        <button className="apps-secondary-button" type="button" onClick={() => onFixApp(app.id)}>
                                            {text.fixReviewIssue}
                                        </button>
                                    )}
                                    {submission?.reviewIssues && submission.reviewIssues.length > 0 && ready && (
                                        <button className="apps-secondary-button" type="button" onClick={() => onFixApp(app.id)}>
                                            {text.fixReviewIssue}
                                        </button>
                                    )}
                                    {(hasDependencyReviewIssue || hasDependencyCheckIssue) && (
                                        <button className="apps-secondary-button" type="button" onClick={() => onInstallDependencies(app.id)}>
                                            {text.resolveReviewDependencies}
                                        </button>
                                    )}
                                </div>
                            </article>
                        );
                    })}
                </div>
            )}
            {queueStatus !== 'unsupported' && (
                <div className="apps-publish-queue">
                    <div className="apps-publish-queue__head">
                        <div>
                            <div className="apps-definition__title">{text.localSubmissionQueue}</div>
                            {queueRefreshedAt && <small className="apps-publish-queue__refreshed">{text.queueRefreshedAt}: {queueRefreshedAt}</small>}
                        </div>
                        <button className="apps-secondary-button" type="button" disabled={queueRefreshing} onClick={() => void refreshSubmissionQueue()}>
                            {queueRefreshing ? text.refreshingQueue : text.refreshQueue}
                        </button>
                    </div>
                    {queueStatus === 'loading' ? (
                        <div className="apps-publish-queue__empty">{text.localSubmissionQueueLoading}</div>
                    ) : queueStatus === 'error' ? (
                        <div className="apps-publish-queue__empty">{text.localSubmissionQueueError}</div>
                    ) : queueSummaries.length === 0 ? (
                        <div className="apps-publish-queue__empty">{text.noLocalSubmissionQueue}</div>
                    ) : (
                        <div className="apps-publish-queue__list">
                            {queueSummaries.map((item) => {
                                const detailRecord = queueDetailRecords[item.submissionID] || null;
                                const detailPackageApps = packageAppNamesFromRecord(detailRecord);
                                const detailWorkspaceLayouts = packageWorkspaceLayoutSummariesFromRecord(detailRecord);
                                const detailEvents = eventSummariesFromRecord(detailRecord);
                                const detailOpen = queueDetailOpenId === item.submissionID;
                                const dependencyCount = item.dependencies.length;
                                const missingDependencyCount = item.dependencies.filter(isBlockingBackendDependency).length;
                                const canSyncItemToHub = item.channel === 'local' && ['submitted', 'review_failed'].includes(String(item.status || 'submitted'));
                                const canRefreshItemFromHub = item.channel === 'hub' && ['pending_review', 'review_failed', 'approved'].includes(String(item.status || 'pending_review'));
                                const canInstallApprovedHubApp = item.channel === 'hub' && !!item.hubCapabilityID && ['approved', 'published'].includes(String(item.status || ''));
                                const queueInstallResult = queueInstallResults[item.submissionID];
                                return (
                                    <div className="apps-publish-queue__row" key={item.submissionID}>
                                        <div className="apps-publish-queue__body">
                                            <strong>{item.submissionID}</strong>
                                            <span>{item.appNames.join(', ') || item.appIDs.join(', ') || '-'} · {item.channel || 'local'} · {item.status || 'submitted'}</span>
                                            <small>
                                                {item.submittedAt}
                                                {item.packageSHA ? ` · sha256:${item.packageSHA.slice(0, 12)}` : ''}
                                                {item.packageBytes ? ` · ${formatPackageBytes(item.packageBytes)}` : ''}
                                                {dependencyCount ? ` | ${text.skillDependencies}:${dependencyCount} ${text.missingDependencyCount}:${missingDependencyCount}` : ''}
                                                {item.eventCount ? ` · ${text.eventHistory}:${item.eventCount}${item.lastEventAt ? ` ${item.lastEventAt}` : ''}` : ''}
                                                {item.message ? ` · ${item.message}` : ''}
                                            </small>
                                            {queuePackageErrorId === item.submissionID && <small>{text.queuePackageUnavailable}</small>}
                                            {queueSyncErrorId === item.submissionID && <small>{text.queueHubSyncFailed}</small>}
                                            {queueInstalledId === item.submissionID && <small>{text.approvedHubAppInstalled}</small>}
                                            {queueInstallErrorId === item.submissionID && <small>{queueInstallErrorMessages[item.submissionID] || text.approvedHubAppInstallFailed}</small>}
                                            {queueInstallResult?.plan && <DependencyVerificationPanel plan={queueInstallResult.plan} state="ready" selectedAppIDs={queueInstallResult.appIDs} text={text} />}
                                            <InstallVersionSnapshot snapshot={queueInstallResult?.versionSnapshot} text={text} />
                                            {queueInstallResult?.installEvidence && <InstallRecordEvidenceSnapshot record={queueInstallResult.installEvidence} text={text} />}
                                            {!queueInstallResult?.installEvidence && item.submissionEvidence && <InstallRecordEvidenceSnapshot record={item.submissionEvidence} text={text} />}
                                            <PublishReviewEvidenceStrip evidence={item.reviewEvidence} text={text} />
                                        </div>
                                        <div className="apps-publish-queue__tools">
                                            {canSyncItemToHub && (
                                                <button
                                                    className="apps-secondary-button apps-publish-queue__copy"
                                                    type="button"
                                                    disabled={!canSyncQueuedPackage || queueSyncingId === item.submissionID}
                                                    title={canSyncQueuedPackage ? text.syncQueueToHub : text.queuePackageUnavailable}
                                                    onClick={() => void syncQueuedSubmission(item.submissionID)}
                                                >
                                                    {queueSyncingId === item.submissionID ? text.syncingQueueToHub : text.syncQueueToHub}
                                                </button>
                                            )}
                                            {canRefreshItemFromHub && (
                                                <button
                                                    className="apps-secondary-button apps-publish-queue__copy"
                                                    type="button"
                                                    disabled={!canRefreshQueuedPackageFromHub || queueSyncingId === item.submissionID}
                                                    title={canRefreshQueuedPackageFromHub ? text.refreshQueueFromHub : text.queuePackageUnavailable}
                                                    onClick={() => void refreshQueuedSubmissionFromHub(item.submissionID)}
                                                >
                                                    {queueSyncingId === item.submissionID ? text.refreshingQueueFromHub : text.refreshQueueFromHub}
                                                </button>
                                            )}
                                            {canInstallApprovedHubApp && (
                                                <button
                                                    className="apps-primary-button apps-publish-queue__copy"
                                                    type="button"
                                                    disabled={queueInstallingId === item.submissionID}
                                                    onClick={() => void installApprovedQueuedApp(item)}
                                                >
                                                    {queueInstallingId === item.submissionID ? text.installingApprovedHubApp : text.installApprovedHubApp}
                                                </button>
                                            )}
                                            <button
                                                className="apps-secondary-button apps-publish-queue__copy"
                                                type="button"
                                                disabled={!canCopyQueuedPackage || queueDetailLoadingId === item.submissionID}
                                                title={canCopyQueuedPackage ? text.viewQueueDetail : text.queuePackageUnavailable}
                                                onClick={() => void toggleQueuedDetail(item.submissionID)}
                                            >
                                                {queueDetailLoadingId === item.submissionID
                                                    ? text.queueDetailLoading
                                                    : detailOpen
                                                        ? text.hideQueueDetail
                                                        : text.viewQueueDetail}
                                            </button>
                                            <button
                                                className="apps-secondary-button apps-publish-queue__copy"
                                                type="button"
                                                disabled={!canCopyQueuedPackage || queuePackageCopyingId === item.submissionID}
                                                title={canCopyQueuedPackage ? text.copyQueuePackage : text.queuePackageUnavailable}
                                                onClick={() => void copyQueuedPackage(item.submissionID)}
                                            >
                                                {queuePackageCopyingId === item.submissionID
                                                    ? text.copyingQueuePackage
                                                    : queuePackageCopiedId === item.submissionID
                                                        ? text.queuePackageCopied
                                                        : text.copyQueuePackage}
                                            </button>
                                            <button
                                                className="apps-secondary-button apps-publish-queue__copy"
                                                type="button"
                                                disabled={!canCopyQueuedPackage || queuePackageCopyingId === item.submissionID}
                                                title={canCopyQueuedPackage ? text.copyQueueAudit : text.queuePackageUnavailable}
                                                onClick={() => void copyQueuedAudit(item.submissionID)}
                                            >
                                                {queuePackageCopyingId === item.submissionID
                                                    ? text.copyingQueuePackage
                                                    : queueAuditCopiedId === item.submissionID
                                                        ? text.queueAuditCopied
                                                        : text.copyQueueAudit}
                                            </button>
                                        </div>
                                        {detailOpen && detailRecord && (
                                            <div className="apps-publish-queue__detail">
                                                <strong>{text.queueDetailTitle}</strong>
                                                <span>{text.submissionId}: {item.submissionID}</span>
                                                {item.reviewer && <span>{text.reviewer}: {item.reviewer}</span>}
                                                {item.riskLevel && <span>{text.riskLevel}: {item.riskLevel}</span>}
                                                {detailPackageApps.length > 0 && <span>{text.queueDetailPackageApps}: {detailPackageApps.join(', ')}</span>}
                                                {detailWorkspaceLayouts.length > 0 && <span>{text.workspaceLayout}: {detailWorkspaceLayouts.join(' | ')}</span>}
                                                {item.reviewIssues.length > 0 && <span>{text.reviewIssues}: {reviewIssuesSummary(item.reviewIssues, text)}</span>}
                                                <ReviewIssuesPanel issues={item.reviewIssues} text={text} compact />
                                                {detailEvents.length > 0 && <span>{text.queueDetailEvents}: {detailEvents.join(' ·  ')}</span>}
                                            </div>
                                        )}
                                    </div>
                                );
                            })}
                        </div>
                    )}
                </div>
            )}
            <div className="apps-manage-manifest-wrap">
                <div className="apps-definition__title">{text.submitPackage}</div>
                <pre className="apps-manage-manifest">{packageText}</pre>
            </div>
        </section>
    );
};

const AppAccentPicker = ({ value, lang, onChange }: { value: string; lang?: string; onChange: (value: string) => void }) => (
    <div className="apps-accent-picker" role="group" aria-label={isZh(lang) ? '\u56fe\u6807\u989c\u8272' : 'Icon color'}>
        {appAccentSwatches.map((swatch) => {
            const label = appAccentLabel(swatch, lang);
            return (
                <button
                    key={swatch.value}
                    className={`apps-accent-choice ${value === swatch.value ? 'is-active' : ''}`}
                    type="button"
                    title={label}
                    aria-label={label}
                    aria-pressed={value === swatch.value}
                    style={{ '--apps-swatch-color': swatch.value } as CSSProperties}
                    onClick={() => onChange(swatch.value)}
                />
            );
        })}
    </div>
);

const emptyToolField = (): SkillAppField => ({ name: '', label: '', type: 'text', required: false, default: '', options: [] });

const ToolFieldEditor = ({ fields, lang, onChange }: { fields: SkillAppField[]; lang?: string; onChange: (fields: SkillAppField[]) => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const updateField = (index: number, patch: Partial<SkillAppField>) => {
        onChange(fields.map((field, fieldIndex) => fieldIndex === index ? normalizeEditorField({ ...field, ...patch }) : field));
    };
    const removeField = (index: number) => onChange(fields.filter((_, fieldIndex) => fieldIndex !== index));
    return (
        <div className="apps-field-editor">
            <div className="apps-preview-title-row">
                <div className="apps-definition__title">{text.fields}</div>
                <button className="apps-secondary-button" type="button" onClick={() => onChange([...fields, emptyToolField()])}>{text.addField}</button>
            </div>
            {fields.length > 0 && (
                <div className="apps-field-editor__list">
                    {fields.map((field, index) => {
                        const type = field.type === 'select' || field.type === 'boolean' ? field.type : 'text';
                        return (
                            <div className="apps-field-editor__item" key={index}>
                                <div className="apps-form-row">
                                    <label>{text.fieldName}</label>
                                    <input value={field.name} onChange={(event) => updateField(index, { name: event.target.value })} placeholder="customer_id" />
                                </div>
                                <div className="apps-form-row">
                                    <label>{text.fieldLabel}</label>
                                    <input value={field.label || ''} onChange={(event) => updateField(index, { label: event.target.value })} placeholder={text.fieldLabel} />
                                </div>
                                <div className="apps-form-row">
                                    <label>{text.fieldType}</label>
                                    <select value={type} onChange={(event) => updateField(index, { type: event.target.value as SkillAppField['type'] })}>
                                        <option value="text">text</option>
                                        <option value="select">select</option>
                                        <option value="boolean">boolean</option>
                                    </select>
                                </div>
                                {type === 'boolean' ? (
                                    <label className="apps-checkbox-field apps-field-editor__checkbox">
                                        <input type="checkbox" checked={field.default === true} onChange={(event) => updateField(index, { default: event.target.checked })} />
                                        <span>{text.defaultValue}</span>
                                    </label>
                                ) : (
                                    <div className="apps-form-row">
                                        <label>{text.defaultValue}</label>
                                        <input value={String(field.default || '')} onChange={(event) => updateField(index, { default: event.target.value })} />
                                    </div>
                                )}
                                {type === 'select' && (
                                    <div className="apps-form-row apps-field-editor__wide">
                                        <label>{text.options}</label>
                                        <input value={(field.options || []).join(', ')} onChange={(event) => updateField(index, { options: event.target.value.split(',').map((item) => item.trim()).filter(Boolean) })} placeholder="A, B, C" />
                                    </div>
                                )}
                                <label className="apps-checkbox-field apps-field-editor__checkbox">
                                    <input type="checkbox" checked={!!field.required} onChange={(event) => updateField(index, { required: event.target.checked })} />
                                    <span>{text.fieldRequired}</span>
                                </label>
                                <button className="apps-secondary-button apps-field-editor__delete" type="button" onClick={() => removeField(index)}>{text.deleteField}</button>
                            </div>
                        );
                    })}
                </div>
            )}
        </div>
    );
};

function normalizeEditorField(field: SkillAppField): SkillAppField {
    const type = field.type === 'select' || field.type === 'boolean' ? field.type : 'text';
    return {
        name: field.name,
        label: field.label,
        type,
        required: !!field.required,
        default: type === 'boolean' ? field.default === true : String(field.default || ''),
        options: type === 'select' ? (field.options || []) : [],
    };
}

type SkillInputMode = 'file' | 'form' | 'mixed';

type AppEditDraft = Pick<AppEntry, 'name' | 'description' | 'category' | 'icon' | 'customIconDataUrl' | 'accent' | 'aboutInfo'> & {
    businessDomain: string;
    businessObjectRole: string;
    businessPreferredAction: string;
    businessPreferredView: string;
    businessPreferredReport: string;
    businessPreferredDashboard: string;
    skillID: string;
    skillSource: AppSkillDependency['source'];
    appSkillID: string;
    appSkillSource: AppSkillDependency['source'];
    appSkillVersion: string;
    workflowSkillID: string;
    workflowSkillSource: AppSkillDependency['source'];
    workflowSkillVersion: string;
    workflowSkillInstallRef: string;
    approvalEvent: string;
    approvalObjectRole: string;
    workflowMapping: AppWorkflowMapping;
    inputMode: SkillInputMode;
    multipleFiles: boolean;
    outputModes: string[];
    fields: SkillAppField[];
    layout: RuntimeWorkspaceLayout;
    resultContract: AppResultContract;
    testProtocol: AppTestProtocol;
    uiNavigation: string[];
    uiColumns: string[];
};

const ManageAppsPane = ({ apps, hiddenApps, skillDiscovery, lang, onTogglePin, onUpdateApp, onDuplicateApp, onMoveApp, onToggleDisableApp, onRemoveApp, onRestoreApp, onAddDiscoveredApp, pendingEditAppId, onPendingEditConsumed }: {
    apps: AppEntry[];
    hiddenApps: AppEntry[];
    skillDiscovery: SkillAppDiscovery;
    lang?: string;
    onTogglePin: (appId: string) => void;
    onUpdateApp: (appId: string, patch: Partial<AppEntry>) => void;
    onDuplicateApp: (appId: string) => void;
    onMoveApp: (appId: string, direction: AppMoveTarget) => void;
    onToggleDisableApp: (appId: string) => void;
    onRemoveApp: (appId: string) => void;
    onRestoreApp: (appId: string) => void;
    onAddDiscoveredApp: (app: AppEntry) => void;
    pendingEditAppId: string;
    onPendingEditConsumed: () => void;
}) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [manifestAppId, setManifestAppId] = useState('');
    const [editingAppId, setEditingAppId] = useState('');
    const emptyEditDraft = useMemo<AppEditDraft>(() => ({ name: '', description: '', category: '', icon: 'contract', customIconDataUrl: undefined, accent: defaultAccentForKind('tool_app'), aboutInfo: undefined, businessDomain: 'business', businessObjectRole: 'record', businessPreferredAction: '', businessPreferredView: '', businessPreferredReport: '', businessPreferredDashboard: '', skillID: '', skillSource: 'local', appSkillID: '', appSkillSource: 'local', appSkillVersion: '', workflowSkillID: '', workflowSkillSource: 'hub', workflowSkillVersion: '', workflowSkillInstallRef: '', approvalEvent: '', approvalObjectRole: '', workflowMapping: defaultAppWorkflowMapping('enterprise_approval_app') as AppWorkflowMapping, inputMode: 'file', multipleFiles: false, outputModes: ['docx', 'pdf'], fields: [], layout: defaultRuntimeWorkspaceLayout('tool_app'), resultContract: buildAppResultContract('tool_app', ['docx', 'pdf']), testProtocol: buildAppTestProtocol('tool_app', ['docx', 'pdf']), uiNavigation: defaultEnterpriseNavigation('tool_app'), uiColumns: defaultEnterpriseColumns('tool_app') }), []);
    const [editDraft, setEditDraft] = useState<AppEditDraft>(emptyEditDraft);
    const editDialogRef = useRef<HTMLDivElement | null>(null);
    const editNameInputRef = useRef<HTMLInputElement | null>(null);
    const editReturnFocusRef = useRef<HTMLElement | null>(null);
    const [editIconUploadState, setEditIconUploadState] = useState<'idle' | 'processing' | 'error'>('idle');
    const [editIconUploadMessage, setEditIconUploadMessage] = useState('');
    const [editSaveState, setEditSaveState] = useState<'idle' | 'saving' | 'error'>('idle');
    const [editSaveMessage, setEditSaveMessage] = useState('');
    const [availableSkills, setAvailableSkills] = useState<SkillSummary[]>([]);
    const appReadySkills = useMemo(() => availableSkills.filter(isAppRuntimeSkillLike), [availableSkills]);
    const approvalWorkflowSkills = useMemo(() => availableSkills.filter(isApprovalWorkflowSkillLike), [availableSkills]);
    const [copiedManifestId, setCopiedManifestId] = useState('');
    const [packCopied, setPackCopied] = useState(false);
    const pinnedCount = apps.filter((app) => app.pinned).length;
    const [manageQuery, setManageQuery] = useState('');
    const [manageCategory, setManageCategory] = useState('all');
    const [managePanelStatus, setManagePanelStatus] = useState<'all' | 'in_panel' | 'not_in_panel'>('all');
    const installedIds = useMemo(() => new Set(apps.map((app) => app.id)), [apps]);
    const hiddenIds = useMemo(() => new Set(hiddenApps.map((app) => app.id)), [hiddenApps]);
    const notInPanelApps = useMemo(() => (skillDiscovery.candidates || []).filter((app) => !installedIds.has(app.id) && !hiddenIds.has(app.id)), [skillDiscovery.candidates, installedIds, hiddenIds]);
    const manageCategories = useMemo(() => Array.from(new Set([...apps, ...hiddenApps, ...notInPanelApps].map((app) => app.category))), [apps, hiddenApps, notInPanelApps]);
    const normalizedManageQuery = manageQuery.trim().toLowerCase();
    const manageQueryMatchedApps = useMemo(() => [...apps, ...hiddenApps, ...notInPanelApps].filter((app) => !normalizedManageQuery || buildAppSearchText(app, lang).includes(normalizedManageQuery)), [apps, hiddenApps, notInPanelApps, normalizedManageQuery, lang]);
    const manageCategoryCounts = useMemo(() => countAppsByCategory(manageQueryMatchedApps), [manageQueryMatchedApps]);
    const manageFilterActive = normalizedManageQuery.length > 0 || manageCategory !== 'all' || managePanelStatus !== 'all';
    useEffect(() => {
        if (!normalizedManageQuery || manageCategory === 'all') return;
        if ((manageCategoryCounts.get(manageCategory) || 0) === 0) setManageCategory('all');
    }, [normalizedManageQuery, manageCategory, manageCategoryCounts]);
    const matchesManageFilter = (app: AppEntry) => {
        const categoryMatches = manageCategory === 'all' || app.category === manageCategory;
        const queryMatches = !normalizedManageQuery || buildAppSearchText(app, lang).includes(normalizedManageQuery);
        return categoryMatches && queryMatches;
    };
    const managedApps = useMemo(() => {
        if (managePanelStatus === 'not_in_panel') return [];
        return apps.filter(matchesManageFilter);
    }, [apps, manageCategory, normalizedManageQuery, managePanelStatus, lang]);
    const filteredNotInPanelApps = useMemo(() => {
        if (managePanelStatus === 'in_panel') return [];
        return notInPanelApps.filter(matchesManageFilter);
    }, [notInPanelApps, manageCategory, normalizedManageQuery, managePanelStatus, lang]);
    const filteredHiddenApps = useMemo(() => {
        if (managePanelStatus === 'not_in_panel') return [];
        return hiddenApps.filter(matchesManageFilter);
    }, [hiddenApps, manageCategory, normalizedManageQuery, managePanelStatus, lang]);
    const editingApp = useMemo(() => apps.find((item) => item.id === editingAppId) || null, [apps, editingAppId]);
    const manageMatchCount = managedApps.length + filteredHiddenApps.length + filteredNotInPanelApps.length;
    const manageTotalCount = apps.length + hiddenApps.length + notInPanelApps.length;
    const manageFilterSummary = filterSummaryText({ query: manageQuery, category: manageCategory, count: manageMatchCount, lang, allLabel: text.all });
    const startEdit = useCallback((app: AppEntry) => {
        const workflowBinding = appApprovalBinding(app);
        const workflowDependency = app.manifest?.dependencies?.skills?.find((dependency) => dependency.kind === 'workflow_skill' && dependency.id);
        editReturnFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        setEditIconUploadState('idle');
        setEditIconUploadMessage('');
        setEditSaveState('idle');
        setEditSaveMessage('');
        setEditingAppId(app.id);
        setEditDraft({
            name: app.name,
            description: app.description,
            category: app.category,
            icon: app.icon,
            customIconDataUrl: app.customIconDataUrl,
            accent: app.accent,
            aboutInfo: normalizeAppAboutInfo(app.aboutInfo),
            businessDomain: app.manifest?.datasrv?.domain || 'business',
            businessObjectRole: app.manifest?.datasrv?.objectRole || app.manifest?.datasrv?.domain || 'record',
            businessPreferredAction: app.manifest?.datasrv?.preferredAction || '',
            businessPreferredView: app.manifest?.datasrv?.preferredView || '',
            businessPreferredReport: app.manifest?.datasrv?.preferredReport || '',
            businessPreferredDashboard: app.manifest?.datasrv?.preferredDashboard || '',
            skillID: app.manifest?.skill?.id || '',
            skillSource: app.manifest?.appSkill?.source || 'local',
            appSkillID: app.manifest?.appSkill?.id || '',
            appSkillSource: app.manifest?.appSkill?.source || 'local',
            appSkillVersion: app.manifest?.appSkill?.version || '',
            workflowSkillID: workflowBinding?.workflowSkillId || workflowDependency?.id || '',
            workflowSkillSource: workflowDependency?.source || 'hub',
            workflowSkillVersion: workflowBinding?.workflowVersion || workflowDependency?.version || '',
            workflowSkillInstallRef: appSkillDependencyInstallRef(workflowDependency),
            approvalEvent: workflowBinding?.event || (app.manifest?.datasrv?.domain ? `${app.manifest.datasrv.domain}.submitted` : ''),
            approvalObjectRole: workflowBinding?.objectRole || app.manifest?.datasrv?.domain || '',
            workflowMapping: normalizeAppWorkflowMapping(app.manifest?.workflow, app.kind, app.manifest?.datasrv?.domain || 'business', workflowBinding?.objectRole || app.manifest?.datasrv?.objectRole || app.manifest?.datasrv?.domain || 'record') || (defaultAppWorkflowMapping('enterprise_approval_app') as AppWorkflowMapping),
            inputMode: app.manifest?.skill?.inputMode || 'file',
            multipleFiles: !!app.manifest?.skill?.multipleFiles,
            outputModes: normalizeOutputModes(app.manifest?.skill?.outputModes),
            fields: normalizeSkillAppFields(app.manifest?.skill?.fields),
            layout: runtimeWorkspaceLayoutForApp(app),
            resultContract: appResultContractForManifest(app),
            testProtocol: appTestProtocolForManifest(app),
            uiNavigation: appEnterpriseNavigation(app),
            uiColumns: appEnterpriseColumns(app),
        });
    }, []);
    useEffect(() => {
        let cancelled = false;
        ListNLSkills()
            .then((skills: SkillSummary[] = []) => {
                if (cancelled) return;
                setAvailableSkills(Array.isArray(skills) ? skills.filter((skill) => String(skill?.name || '').trim()) : []);
            })
            .catch(() => {
                if (!cancelled) setAvailableSkills([]);
            });
        return () => {
            cancelled = true;
        };
    }, []);
    useEffect(() => {
        if (!pendingEditAppId) return;
        const app = apps.find((item) => item.id === pendingEditAppId);
        if (!app) return;
        setManageQuery('');
        setManageCategory('all');
        startEdit(app);
        onPendingEditConsumed();
    }, [pendingEditAppId, apps, onPendingEditConsumed, startEdit]);
    const cancelEdit = useCallback(() => {
        setEditingAppId('');
        setEditDraft(emptyEditDraft);
        setEditIconUploadState('idle');
        setEditIconUploadMessage('');
        setEditSaveState('idle');
        setEditSaveMessage('');
        const returnTarget = editReturnFocusRef.current;
        editReturnFocusRef.current = null;
        if (returnTarget?.isConnected && typeof window.requestAnimationFrame === 'function') {
            window.requestAnimationFrame(() => returnTarget.focus());
        }
    }, [emptyEditDraft]);
    useEffect(() => {
        if (editingAppId && !editingApp) cancelEdit();
    }, [editingAppId, editingApp, cancelEdit]);
    useEffect(() => {
        if (!editingApp) return;
        const frame = window.requestAnimationFrame(() => {
            editNameInputRef.current?.focus();
            editNameInputRef.current?.select();
        });
        return () => window.cancelAnimationFrame(frame);
    }, [editingApp]);
    const handleEditDialogKeyDown = useCallback((event: KeyboardEvent<HTMLDivElement>) => {
        if (event.key === 'Escape') {
            event.preventDefault();
            cancelEdit();
            return;
        }
        if (event.key !== 'Tab') return;
        const focusable = Array.from(editDialogRef.current?.querySelectorAll<HTMLElement>('button:not([disabled]):not([tabindex="-1"]), input:not([disabled]):not([tabindex="-1"]), select:not([disabled]):not([tabindex="-1"]), textarea:not([disabled]):not([tabindex="-1"]), [tabindex]:not([tabindex="-1"])') || []);
        if (focusable.length === 0) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) {
            event.preventDefault();
            last.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault();
            first.focus();
        }
    }, [cancelEdit]);
    const setUploadedCustomIcon = useCallback(async (file: File) => {
        setEditIconUploadState('processing');
        setEditIconUploadMessage('');
        try {
            const customIconDataUrl = await fileToAppIconDataUrl(file);
            setEditDraft((current) => ({ ...current, customIconDataUrl }));
            setEditIconUploadState('idle');
        } catch {
            setEditIconUploadState('error');
            setEditIconUploadMessage(isZh(lang)
                ? '\u8bf7\u4e0a\u4f20 5 MB \u4ee5\u5185\u7684 PNG\u3001JPEG \u6216 WebP \u56fe\u7247\u3002'
                : 'Upload a PNG, JPEG, or WebP image under 5 MB.');
        }
    }, [lang]);
    const updateEditAboutInfo = useCallback((patch: Partial<AppAboutInfo>) => {
        setEditDraft((current) => ({ ...current, aboutInfo: normalizeAppAboutInfo({ ...current.aboutInfo, ...patch }) }));
    }, []);
    const saveEdit = async (app: AppEntry) => {
        const name = editDraft.name.trim();
        if (!name || editSaveState === 'saving') return;
        setEditSaveState('saving');
        setEditSaveMessage('');
        let manifest = app.manifest;
        if (app.kind === 'tool_app') {
            const baseManifest = app.manifest || makeSkillManifest(app.id, editDraft.inputMode, editDraft.outputModes);
            const skillID = editDraft.skillID.trim() || baseManifest.skill?.id || app.id;
            manifest = {
                ...baseManifest,
                appSkill: { id: skillID, version: baseManifest.appSkill?.version || '1.0.0', source: editDraft.skillSource },
                skill: {
                    id: skillID,
                    appDefinitionFile: baseManifest.skill?.appDefinitionFile || 'maclaw.apps.json' as const,
                    inputMode: editDraft.inputMode,
                    multipleFiles: editDraft.multipleFiles && editDraft.inputMode !== 'form',
                    outputModes: normalizeOutputModes(editDraft.outputModes),
                    fields: normalizeSkillAppFields(editDraft.fields),
                },
            };
        } else if (manifest && isEnterpriseAppKind(app.kind)) {
            const appSkillID = editDraft.appSkillID.trim();
            const workflowSkillID = editDraft.workflowSkillID.trim();
            const workflowVersion = editDraft.workflowSkillVersion.trim() || undefined;
            const workflowInstallRef = editDraft.workflowSkillInstallRef.trim();
            const existingSkills = manifest.dependencies?.skills || [];
            const nonWorkflowSkills = existingSkills.filter((dependency) => dependency.kind !== 'workflow_skill');
            const existingWorkflow = existingSkills.find((dependency) => dependency.kind === 'workflow_skill' && dependency.id);
            const workflowDependency: AppSkillDependency | undefined = workflowSkillID ? (() => {
                const { install_ref: _installRef, installRef: _installRefCamel, ...workflowBase } = existingWorkflow || {};
                return {
                    ...workflowBase,
                    id: workflowSkillID,
                    version: workflowVersion,
                    kind: 'workflow_skill',
                    required: existingWorkflow?.required !== false,
                    source: editDraft.workflowSkillSource || existingWorkflow?.source || 'hub',
                    install_ref: workflowInstallRef || undefined,
                    capabilities: existingWorkflow?.capabilities?.length ? existingWorkflow.capabilities : ['approval.workflow'],
                };
            })() : undefined;
            const skills = workflowDependency ? [...nonWorkflowSkills, workflowDependency] : nonWorkflowSkills;
            const dataSrvDomain = editDraft.businessDomain.trim() || manifest.datasrv?.domain || 'business';
            const nextDataSrv: NonNullable<AppManifestBinding['datasrv']> = { ...(manifest.datasrv || { domain: dataSrvDomain }), domain: dataSrvDomain };
            if (app.kind === 'enterprise_normal_app') {
                const objectRole = editDraft.businessObjectRole.trim();
                const preferredAction = editDraft.businessPreferredAction.trim();
                const preferredView = editDraft.businessPreferredView.trim();
                const preferredReport = editDraft.businessPreferredReport.trim();
                const preferredDashboard = editDraft.businessPreferredDashboard.trim();
                if (objectRole) nextDataSrv.objectRole = objectRole; else delete nextDataSrv.objectRole;
                if (preferredAction) nextDataSrv.preferredAction = preferredAction; else delete nextDataSrv.preferredAction;
                if (preferredView) nextDataSrv.preferredView = preferredView; else delete nextDataSrv.preferredView;
                if (preferredReport) nextDataSrv.preferredReport = preferredReport; else delete nextDataSrv.preferredReport;
                if (preferredDashboard) nextDataSrv.preferredDashboard = preferredDashboard; else delete nextDataSrv.preferredDashboard;
            }
            manifest = {
                ...manifest,
                datasrv: nextDataSrv,
                appSkill: appSkillID ? { id: appSkillID, version: editDraft.appSkillVersion.trim() || manifest.appSkill?.version || '1.0.0', source: editDraft.appSkillSource } : undefined,
                dependencies: skills.length ? { skills } : undefined,
                mis: app.kind === 'enterprise_approval_app' && workflowSkillID ? {
                    ...(manifest.mis || {}),
                    approvalBindings: [{
                        event: editDraft.approvalEvent.trim() || `${nextDataSrv.domain || 'record'}.submitted`,
                        workflowSkillId: workflowSkillID,
                        workflowVersion,
                        objectRole: editDraft.approvalObjectRole.trim() || nextDataSrv.objectRole || nextDataSrv.domain || undefined,
                    }],
                } : manifest.mis,
            };
        }
        if (manifest) {
            manifest = applyAppTestProtocol(applyAppResultContract(applyEnterpriseUIConfig(applyAppWorkflowMapping(applyStudioWorkspaceLayout(manifest, app.kind, editDraft.layout), app.kind, editDraft.workflowMapping), app.kind, editDraft.uiNavigation, editDraft.uiColumns), app.kind, editDraft.resultContract), app.kind, editDraft.testProtocol);
        }
        const aboutInfo = normalizeAppAboutInfo(editDraft.aboutInfo);
        const updatedApp: AppEntry = {
            ...app,
            name,
            category: editDraft.category.trim() || (isZh(lang) ? '\u672a\u5206\u7c7b' : 'Uncategorized'),
            description: editDraft.description.trim(),
            icon: editDraft.icon,
            customIconDataUrl: normalizeCustomIconDataUrl(editDraft.customIconDataUrl),
            accent: editDraft.accent,
            aboutInfo,
            version: nextAppVersion(app),
            manifest,
        };
        const patch: Partial<AppEntry> = {
            name: updatedApp.name,
            category: updatedApp.category,
            description: updatedApp.description,
            icon: updatedApp.icon,
            customIconDataUrl: updatedApp.customIconDataUrl,
            accent: updatedApp.accent,
            aboutInfo: updatedApp.aboutInfo,
            version: updatedApp.version,
            manifest: updatedApp.manifest,
            importedRunEvidence: undefined,
            versionSnapshot: undefined,
            installEvidence: undefined,
            workflowContract: undefined,
        };
        const skillID = appRunnableSkillID(updatedApp);
        const appDefinitionFile = String(updatedApp.manifest?.skill?.appDefinitionFile || '').trim();
        const isWritableSkillAppDefinition = updatedApp.kind === 'tool_app'
            ? (appDefinitionFile === 'maclaw.app.json' || appDefinitionFile === 'maclaw.apps.json')
            : isEnterpriseAppKind(updatedApp.kind) && appDefinitionFile === 'maclaw.app.json';
        const shouldWriteSkillDefinition = updatedApp.source === 'skill'
            && isWritableSkillAppDefinition
            && !!skillID;
        try {
            if (shouldWriteSkillDefinition) {
                await SaveMaclawAppDefinitionForSkill(skillID, JSON.stringify(appToSkillDefinitionManifest(updatedApp), null, 2));
            }
            onUpdateApp(app.id, patch);
            cancelEdit();
        } catch (error) {
            setEditSaveState('error');
            setEditSaveMessage(error instanceof Error ? error.message : String(error || 'Save failed'));
        }
    };
    return (
        <div className="apps-manage-list">
            <div className="apps-manage-toolbar">
                <div>
                    <div className="apps-definition__title">{text.apps}</div>
                    <span className="apps-count">{manageMatchCount}/{manageTotalCount}</span>
                </div>
                <button className="apps-secondary-button" type="button" onClick={async () => {
                    await copyTextToClipboard(JSON.stringify(appsToPackManifest(apps), null, 2));
                    setPackCopied(true);
                }}>{packCopied ? text.copied : text.exportPack}</button>
            </div>
            <div className="apps-manage-filter">
                <div className="apps-search-wrap">
                    <input
                        className="apps-search"
                        value={manageQuery}
                        onChange={(event) => setManageQuery(event.target.value)}
                        onKeyDown={(event) => {
                            if (event.key === 'Escape' && manageQuery) setManageQuery('');
                        }}
                        placeholder={text.search}
                    />
                    {manageQuery && (
                        <button className="apps-search-clear" type="button" title={text.clearSearch} aria-label={text.clearSearch} onClick={() => setManageQuery('')}>
                            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
                        </button>
                    )}
                </div>
                <select className="apps-manage-category-select" value={manageCategory} onChange={(event) => setManageCategory(event.target.value)}>
                    <option value="all">{text.all} ({manageQueryMatchedApps.length})</option>
                    {manageCategories.map((item) => <option key={item} value={item} disabled={!!normalizedManageQuery && (manageCategoryCounts.get(item) || 0) === 0}>{categoryOptionLabel(item, manageCategoryCounts)}</option>)}
                </select>
                <select className="apps-manage-category-select" value={managePanelStatus} onChange={(event) => setManagePanelStatus(event.target.value as 'all' | 'in_panel' | 'not_in_panel')} aria-label={isZh(lang) ? '\u9762\u677f\u72b6\u6001' : 'Panel status'}>
                    <option value="all">{isZh(lang) ? '\u5168\u90e8\u72b6\u6001' : 'All statuses'}</option>
                    <option value="in_panel">{isZh(lang) ? '\u5df2\u52a0\u5165\u9762\u677f' : 'In panel'}</option>
                    <option value="not_in_panel" disabled={notInPanelApps.length === 0}>{isZh(lang) ? '\u672a\u52a0\u5165\u9762\u677f' : 'Not in panel'}{notInPanelApps.length > 0 ? ` (${notInPanelApps.length})` : ''}</option>
                </select>
                <button
                    className="apps-secondary-button"
                    type="button"
                    disabled={!manageFilterActive}
                    title={text.resetFilter}
                    onClick={() => {
                        setManageQuery('');
                        setManageCategory('all');
                        setManagePanelStatus('all');
                    }}
                >{text.reset}</button>
            </div>
            {manageFilterSummary && <div className="apps-filter-summary apps-filter-summary--manage" aria-live="polite">{manageFilterSummary}</div>}
            {skillDiscovery.status === 'error' && (
                <div className="apps-filter-summary apps-filter-summary--manage" style={{ color: 'var(--error-color, #ef4444)' }}>
                    {isZh(lang) ? '检查已安装能力时遇到问题' : 'Could not check installed capabilities'}
                    {skillDiscovery.error && <span> · {skillDiscovery.error}</span>}
                </div>
            )}
            {manageMatchCount === 0 && <div className="apps-empty">{text.noApps}</div>}
            {managedApps.map((app) => {
                const index = apps.findIndex((item) => item.id === app.id);
                const pinDisabled = !app.pinned && pinnedCount >= maxPinnedApps;
                const pinTitle = app.pinned ? text.unpin : pinDisabled ? text.pinLimitReached : text.pin;
                const removalLabel = builtInAppIds.has(app.id) ? text.hidden : text.remove;
                return (
                <div key={app.id} className="apps-manage-item">
                    <div className="apps-manage-row">
                        <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><AppIcon icon={app.icon} customIconDataUrl={app.customIconDataUrl} /></span>
                        <div>
                            <div className="apps-manage-row__name">{app.name}</div>
	                            <div className="apps-manage-row__desc">{app.category} · {sourceLabels[app.source][isZh(lang) ? 'zh' : 'en']} · {isZh(lang) ? '已加入面板' : 'In panel'}</div>
                        </div>
                        <div className="apps-manage-actions">
                            <button className="apps-icon-button" type="button" disabled={manageFilterActive || index === 0} title={manageFilterActive ? text.clearFilterToSort : text.moveTop} onClick={() => onMoveApp(app.id, "top")}>{text.moveTopShort}</button>
                            <button className="apps-icon-button" type="button" disabled={manageFilterActive || index === 0} title={manageFilterActive ? text.clearFilterToSort : text.moveUp} onClick={() => onMoveApp(app.id, -1)}>{text.moveUpShort}</button>
                            <button className="apps-icon-button" type="button" disabled={manageFilterActive || index === apps.length - 1} title={manageFilterActive ? text.clearFilterToSort : text.moveDown} onClick={() => onMoveApp(app.id, 1)}>{text.moveDownShort}</button>
                            <button className="apps-icon-button" type="button" disabled={manageFilterActive || index === apps.length - 1} title={manageFilterActive ? text.clearFilterToSort : text.moveBottom} onClick={() => onMoveApp(app.id, "bottom")}>{text.moveBottomShort}</button>
                            <button className="apps-secondary-button" type="button" title={text.edit} onClick={() => startEdit(app)}>{text.edit}</button>
                            <button className="apps-secondary-button" type="button" title={text.duplicate} onClick={() => onDuplicateApp(app.id)}>{text.copy}</button>
                            <button className="apps-secondary-button" type="button" title={text.manifest} onClick={() => setManifestAppId((current) => current === app.id ? '' : app.id)}>{text.manifest}</button>
                            <button className="apps-secondary-button" type="button" title={app.disabled ? text.enable : text.disable} onClick={() => onToggleDisableApp(app.id)}>{app.disabled ? text.enable : text.disable}</button>
                            <button className="apps-secondary-button" type="button" disabled={pinDisabled} title={pinTitle} onClick={() => onTogglePin(app.id)}>{app.pinned ? text.unpin : text.pin}</button>
                            <button className="apps-secondary-button" type="button" title={removalLabel} onClick={() => onRemoveApp(app.id)}>{removalLabel}</button>
                        </div>
                    </div>
                    {manifestAppId === app.id && (
                        <div className="apps-manage-manifest-wrap">
                            <div className="apps-preview-title-row">
                                <div className="apps-definition__title">{text.manifest}</div>
                                <button className="apps-secondary-button" type="button" onClick={async () => {
                                    await copyTextToClipboard(JSON.stringify(appToManifest(app), null, 2));
                                    setCopiedManifestId(app.id);
                                }}>{copiedManifestId === app.id ? text.copied : text.copy}</button>
                            </div>
                            <pre className="apps-manage-manifest">{JSON.stringify(appToManifest(app), null, 2)}</pre>
                        </div>
                    )}
                </div>
            );})}
            {filteredHiddenApps.length > 0 && (
                <section className="apps-hidden-section">
                    <div className="apps-section__title-row">
                        <h3 className="apps-section__title">{text.hiddenApps}</h3>
                        <span className="apps-count">{filteredHiddenApps.length}/{hiddenApps.length}</span>
                    </div>
                    {filteredHiddenApps.map((app) => (
                        <div key={app.id} className="apps-manage-row apps-manage-row--hidden">
                            <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><AppIcon icon={app.icon} customIconDataUrl={app.customIconDataUrl} /></span>
                            <div>
                                <div className="apps-manage-row__name">{app.name}</div>
                                <div className="apps-manage-row__desc">{app.category} · {sourceLabels[app.source][isZh(lang) ? 'zh' : 'en']}</div>
                            </div>
                            <div className="apps-manage-actions">
                                <button className="apps-secondary-button" type="button" title={text.restore} onClick={() => onRestoreApp(app.id)}>{text.restore}</button>
                            </div>
                        </div>
                    ))}
                </section>
            )}
            {filteredNotInPanelApps.length > 0 && (
                <section className="apps-hidden-section">
                    <div className="apps-section__title-row">
                        <h3 className="apps-section__title">{isZh(lang) ? '未加入面板' : 'Not in panel'}</h3>
                        <span className="apps-count">{filteredNotInPanelApps.length}/{notInPanelApps.length}</span>
                    </div>
                    {filteredNotInPanelApps.map((app) => (
                        <div key={app.id} className="apps-manage-row apps-manage-row--not-in-panel">
                            <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><AppIcon icon={app.icon} customIconDataUrl={app.customIconDataUrl} /></span>
                            <div>
                                <div className="apps-manage-row__name">{app.name}</div>
                                <div className="apps-manage-row__desc">{app.category} · {sourceLabels[app.source]?.[isZh(lang) ? 'zh' : 'en'] || 'Skill'}</div>
                            </div>
                            <div className="apps-manage-actions">
                                <button className="apps-secondary-button" type="button" title={text.addToPanel} onClick={() => onAddDiscoveredApp(app)}>{text.addToPanel}</button>
                            </div>
                        </div>
                    ))}
                </section>
            )}
            {editingApp && (
                <div ref={editDialogRef} className="apps-manage-edit-dialog" role="dialog" aria-modal="true" aria-labelledby="apps-manage-edit-title" onKeyDown={handleEditDialogKeyDown}>
                    <div className="apps-manage-edit-dialog__backdrop" aria-hidden="true" onClick={cancelEdit} />
                    <div className="apps-manage-edit-dialog__panel">
                        <div className="apps-manage-edit-dialog__header">
                            <div>
                                <div className="apps-definition__title" id="apps-manage-edit-title">{text.edit}</div>
                                <div className="apps-manage-edit-dialog__subtitle">{editingApp.name} · {editingApp.category}</div>
                            </div>
                            <button className="apps-icon-button" type="button" title={text.cancel} aria-label={text.cancel} onClick={cancelEdit}>
                                <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M18 6 6 18M6 6l12 12" /></svg>
                            </button>
                        </div>
                        <div className="apps-manage-edit">
                            <div className="apps-manage-edit__section">
                            <div className="apps-manage-edit__section-title">{isZh(lang) ? '\u57fa\u672c\u4fe1\u606f' : 'Basic info'}</div>
                            <div className="apps-form-row">
                                <label>{isZh(lang) ? '\u540d\u79f0' : 'Name'}</label>
                                <input data-testid="edit-app-name" ref={editNameInputRef} value={editDraft.name} onChange={(event) => setEditDraft((current) => ({ ...current, name: event.target.value }))} />
                            </div>
                            <div className="apps-form-row">
                                <label>{text.category}</label>
                                <input data-testid="edit-app-category" value={editDraft.category} onChange={(event) => setEditDraft((current) => ({ ...current, category: event.target.value }))} />
                            </div>
                            <div className="apps-form-row">
                                <label>{isZh(lang) ? '\u56fe\u6807' : 'Icon'}</label>
                                <div className="apps-icon-picker" role="group" aria-label={isZh(lang) ? '\u56fe\u6807' : 'Icon'}>
                                    {appIconNames.map((item) => {
                                        const label = appIconLabel(item, lang);
                                        return (
                                            <button
                                                key={item}
                                                className={`apps-icon-choice ${editDraft.icon === item ? 'is-active' : ''}`}
                                                type="button"
                                                title={label}
                                                aria-label={label}
                                                aria-pressed={editDraft.icon === item}
                                                onClick={() => setEditDraft((current) => ({ ...current, icon: item }))}
                                            >
                                                <Icon name={item} />
                                            </button>
                                        );
                                    })}
                                </div>
                            </div>
                            <div className="apps-form-row apps-form-row--wide">
                                <label>{isZh(lang) ? '\u81ea\u5b9a\u4e49\u56fe\u6807' : 'Custom icon'}</label>
                                <div className="apps-custom-icon-field">
                                    <span className="apps-app-icon apps-custom-icon-preview" style={{ '--apps-icon-color': editDraft.accent } as CSSProperties}>
                                        <AppIcon icon={editDraft.icon} customIconDataUrl={editDraft.customIconDataUrl} />
                                    </span>
                                    <label className={`apps-secondary-button apps-custom-icon-upload ${editIconUploadState === 'processing' ? 'is-disabled' : ''}`}>
                                        <input
                                            type="file"
                                            accept="image/png,image/jpeg,image/webp"
                                            disabled={editIconUploadState === 'processing'}
                                            onChange={(event) => {
                                                const file = event.target.files?.[0];
                                                event.target.value = '';
                                                if (!file) return;
                                                void setUploadedCustomIcon(file);
                                            }}
                                        />
                                        <span>{editIconUploadState === 'processing' ? (isZh(lang) ? '\u5904\u7406\u4e2d...' : 'Processing...') : (isZh(lang) ? '\u4e0a\u4f20\u56fe\u7247' : 'Upload image')}</span>
                                    </label>
                                    {editDraft.customIconDataUrl && (
                                        <button className="apps-secondary-button" type="button" onClick={() => {
                                            setEditIconUploadState('idle');
                                            setEditIconUploadMessage('');
                                            setEditDraft((current) => ({ ...current, customIconDataUrl: undefined }));
                                        }}>{isZh(lang) ? '\u4f7f\u7528\u5185\u7f6e\u56fe\u6807' : 'Use built-in icon'}</button>
                                    )}
                                    <small className={editIconUploadState === 'error' ? 'is-error' : ''} role={editIconUploadState === 'error' ? 'alert' : undefined}>
                                        {editIconUploadMessage || (isZh(lang) ? '\u81ea\u52a8\u88c1\u526a\u4e3a 96\u00d796 PNG\uff0c\u652f\u6301 5 MB \u4ee5\u5185\u56fe\u7247\uff0c\u5185\u7f6e\u56fe\u6807\u4f5c\u4e3a\u56de\u9000\u3002' : 'Crops to a 96x96 PNG automatically. Supports images under 5 MB; built-in icon stays as fallback.')}
                                    </small>
                                </div>
                            </div>
                            <div className="apps-form-row">
                                <label>{text.appColor}</label>
                                <AppAccentPicker value={editDraft.accent} lang={lang} onChange={(accent) => setEditDraft((current) => ({ ...current, accent }))} />
                            </div>
                            <div className="apps-form-row apps-form-row--wide apps-form-row--description">
                                <label>{isZh(lang) ? '\u63cf\u8ff0' : 'Description'}</label>
                                <textarea data-testid="edit-app-description" value={editDraft.description} onChange={(event) => setEditDraft((current) => ({ ...current, description: event.target.value }))} />
                            </div>
                            <AppAboutInfoFields text={text} value={editDraft.aboutInfo} onChange={updateEditAboutInfo} testIdPrefix="edit-app" />
                            </div>
                            <div className="apps-manage-edit__section">
                            <div className="apps-manage-edit__section-title">{isZh(lang) ? '\u80fd\u529b\u7ed1\u5b9a' : 'Capability binding'}</div>
                            {editingApp.kind === 'tool_app' && (
                                <>
                                    <div className="apps-form-row">
                                        <label>{isZh(lang) ? 'Skill' : 'Skill ID'}</label>
                                        <StudioSkillPicker
                                            label={isZh(lang) ? 'Skill' : 'Skill ID'}
                                            value={editDraft.skillID}
                                            installedSkills={availableSkills}
                                            lang={lang}
                                            onSelect={(choice) => setEditDraft((current) => ({
                                                ...current,
                                                skillID: choice.id,
                                                skillSource: choice.source === 'installed' ? 'local' : choice.source,
                                            }))}
                                        />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{isZh(lang) ? '\u8f93\u5165\u6a21\u5f0f' : 'Input mode'}</label>
                                        <select value={editDraft.inputMode} onChange={(event) => setEditDraft((current) => ({ ...current, inputMode: event.target.value as SkillInputMode, multipleFiles: event.target.value === 'form' ? false : current.multipleFiles }))}>
                                            <option value="file">{isZh(lang) ? '\u6587\u4ef6\u4e0a\u4f20' : 'File upload'}</option>
                                            <option value="form">{isZh(lang) ? '\u8868\u5355\u53c2\u6570' : 'Form parameters'}</option>
                                            <option value="mixed">{isZh(lang) ? '\u6587\u4ef6 + \u8868\u5355' : 'File + form'}</option>
                                        </select>
                                    </div>
                                    {editDraft.inputMode !== 'form' && (
                                        <div className="apps-form-row">
                                            <label>{isZh(lang) ? '\u591a\u6587\u4ef6' : 'Multiple files'}</label>
                                            <label className="apps-checkbox-field">
                                                <input type="checkbox" checked={editDraft.multipleFiles} onChange={(event) => setEditDraft((current) => ({ ...current, multipleFiles: event.target.checked }))} />
                                                <span>{isZh(lang) ? '\u5141\u8bb8\u4e00\u6b21\u9009\u62e9\u591a\u4e2a\u6587\u4ef6' : 'Allow selecting several files'}</span>
                                            </label>
                                        </div>
                                    )}
                                    <div className="apps-form-row">
                                        <label>{isZh(lang) ? '\u8f93\u51fa\u683c\u5f0f' : 'Output modes'}</label>
                                        <div className="apps-output-mode-picker" role="group" aria-label={isZh(lang) ? '\u8f93\u51fa\u683c\u5f0f' : 'Output modes'}>
                                            {['docx', 'xlsx', 'pdf', 'json', 'txt'].map((mode) => (
                                                <label key={mode} className="apps-output-mode-choice">
                                                    <input
                                                        type="checkbox"
                                                        checked={editDraft.outputModes.includes(mode)}
                                                        onChange={(event) => {
                                                            setEditDraft((current) => {
                                                                const next = event.target.checked ? [...current.outputModes, mode] : current.outputModes.filter((item) => item !== mode);
                                                                return { ...current, outputModes: next.length > 0 ? normalizeOutputModes(next) : current.outputModes };
                                                            });
                                                        }}
                                                    />
                                                    <span>{outputModeLabel(mode)}</span>
                                                </label>
                                            ))}
                                        </div>
                                    </div>
                                    {(editDraft.inputMode === 'form' || editDraft.inputMode === 'mixed') && (
                                        <ToolFieldEditor fields={editDraft.fields} lang={lang} onChange={(fields) => setEditDraft((current) => ({ ...current, fields }))} />
                                    )}
                                </>
                            )}
                            {isEnterpriseAppKind(editingApp.kind) && (
                                <>
                                    {editingApp.kind === 'enterprise_normal_app' && (
                                        <>
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u4e1a\u52a1\u57df' : 'DataSrv domain'}</label>
                                                <input data-testid="edit-business-domain" value={editDraft.businessDomain} onChange={(event) => setEditDraft((current) => ({ ...current, businessDomain: event.target.value }))} placeholder="sales" />
                                            </div>
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u5bf9\u8c61\u89d2\u8272' : 'Object role'}</label>
                                                <input data-testid="edit-business-object-role" value={editDraft.businessObjectRole} onChange={(event) => setEditDraft((current) => ({ ...current, businessObjectRole: event.target.value }))} placeholder="customer" />
                                            </div>
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u9ed8\u8ba4\u52a8\u4f5c' : 'Default action'}</label>
                                                <input data-testid="edit-business-preferred-action" value={editDraft.businessPreferredAction} onChange={(event) => setEditDraft((current) => ({ ...current, businessPreferredAction: event.target.value }))} placeholder="sales.customer_upsert" />
                                            </div>
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u9ed8\u8ba4\u89c6\u56fe' : 'Default view'}</label>
                                                <input data-testid="edit-business-preferred-view" value={editDraft.businessPreferredView} onChange={(event) => setEditDraft((current) => ({ ...current, businessPreferredView: event.target.value }))} placeholder="sales.customer_directory" />
                                            </div>
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u62a5\u8868' : 'Report'}</label>
                                                <input data-testid="edit-business-preferred-report" value={editDraft.businessPreferredReport} onChange={(event) => setEditDraft((current) => ({ ...current, businessPreferredReport: event.target.value }))} placeholder="sales.customer_activity" />
                                            </div>
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u770b\u677f' : 'Dashboard'}</label>
                                                <input data-testid="edit-business-preferred-dashboard" value={editDraft.businessPreferredDashboard} onChange={(event) => setEditDraft((current) => ({ ...current, businessPreferredDashboard: event.target.value }))} placeholder="sales.overview" />
                                            </div>
                                        </>
                                    )}
                                    <div className="apps-capability-skill-grid">
                                        <div className="apps-capability-skill-card">
                                            <div className="apps-capability-skill-card__head">
                                                <label>{isZh(lang) ? '\u5e94\u7528 Skill' : 'appSkill'}</label>
                                            </div>
                                            <StudioSkillPicker
                                                label={isZh(lang) ? '\u5e94\u7528 Skill' : 'appSkill'}
                                                value={editDraft.appSkillID}
                                                installedSkills={appReadySkills}
                                                lang={lang}
                                                mode="app"
                                                onSelect={(choice) => setEditDraft((current) => ({
                                                    ...current,
                                                    appSkillID: choice.id,
                                                    appSkillSource: choice.source === 'installed' ? 'local' : choice.source,
                                                }))}
                                            />
                                        </div>
                                        {editingApp.kind === 'enterprise_approval_app' && (
                                            <div className="apps-capability-skill-card">
                                                <div className="apps-capability-skill-card__head">
                                                    <label>{isZh(lang) ? '\u5ba1\u6279 workflow Skill' : 'workflow skill'}</label>
                                                    <button className="apps-secondary-button apps-skill-design-button" type="button" title={isZh(lang) ? '\u6253\u5f00\u5ba1\u6279\u5de5\u4f5c\u6d41\u8bbe\u8ba1\u5668' : 'Open approval workflow designer'} onClick={() => void openApprovalWorkflowDesigner()}>
                                                        {isZh(lang) ? '\u8bbe\u8ba1' : 'Design'}
                                                    </button>
                                                </div>
                                                <StudioSkillPicker
                                                    label={isZh(lang) ? '\u5ba1\u6279 workflow Skill' : 'workflow skill'}
                                                    value={editDraft.workflowSkillID}
                                                    installedSkills={approvalWorkflowSkills}
                                                    lang={lang}
                                                    mode="approvalWorkflow"
                                                    onSelect={(choice) => setEditDraft((current) => ({
                                                        ...current,
                                                        workflowSkillID: choice.id,
                                                        workflowSkillSource: choice.source === 'installed' ? 'local' : choice.source,
                                                        workflowSkillInstallRef: choice.source === 'installed' ? '' : choice.id,
                                                    }))}
                                                />
                                            </div>
                                        )}
                                    </div>
                                    <div className="apps-form-row">
                                        <label>{isZh(lang) ? '\u5e94\u7528 Skill \u7248\u672c' : 'appSkill version'}</label>
                                        <input value={editDraft.appSkillVersion} onChange={(event) => setEditDraft((current) => ({ ...current, appSkillVersion: event.target.value }))} placeholder="1.0.0" />
                                    </div>
                                    {editingApp.kind === 'enterprise_approval_app' && (
                                        <>
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u5ba1\u6279 workflow \u7248\u672c' : 'workflow version'}</label>
                                                <input value={editDraft.workflowSkillVersion} onChange={(event) => setEditDraft((current) => ({ ...current, workflowSkillVersion: event.target.value }))} placeholder="1.0.0" />
                                            </div>
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u5b89\u88c5\u5f15\u7528' : 'Install ref'}</label>
                                                <input data-testid="edit-workflow-skill-install-ref" value={editDraft.workflowSkillInstallRef} onChange={(event) => setEditDraft((current) => ({ ...current, workflowSkillInstallRef: event.target.value }))} placeholder={isZh(lang) ? '\u80fd\u529b ID ·  GitHub URL' : 'Capability ID ·  GitHub URL'} />
                                            </div>
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u5ba1\u6279\u4e8b\u4ef6' : 'Approval event'}</label>
                                                <input value={editDraft.approvalEvent} onChange={(event) => setEditDraft((current) => ({ ...current, approvalEvent: event.target.value }))} placeholder={`${editingApp.manifest?.datasrv?.domain || 'record'}.submitted`} />
                                            </div>
                                            <div className="apps-form-row">
                                                <label>{isZh(lang) ? '\u5bf9\u8c61\u89d2\u8272' : 'Object role'}</label>
                                                <input value={editDraft.approvalObjectRole} onChange={(event) => setEditDraft((current) => ({ ...current, approvalObjectRole: event.target.value }))} placeholder={editingApp.manifest?.datasrv?.domain || 'record'} />
                                            </div>
                                        </>
                                    )}
                                </>
                            )}
                            {editingApp.kind === 'enterprise_approval_app' && <WorkflowMappingDesigner value={editDraft.workflowMapping} onChange={(workflowMapping) => setEditDraft((current) => ({ ...current, workflowMapping }))} lang={lang} testIdPrefix="edit" />}
                            {isEnterpriseAppKind(editingApp.kind) && <EnterpriseUIConfigDesigner kind={editingApp.kind} navigation={editDraft.uiNavigation} columns={editDraft.uiColumns} onNavigationChange={(uiNavigation) => setEditDraft((current) => ({ ...current, uiNavigation }))} onColumnsChange={(uiColumns) => setEditDraft((current) => ({ ...current, uiColumns }))} lang={lang} testIdPrefix="edit" />}
                            <StudioLayoutDesigner kind={editingApp.kind} value={editDraft.layout} onChange={(layout) => setEditDraft((current) => ({ ...current, layout }))} lang={lang} testIdPrefix="edit" />
                            <ResultContractDesigner contract={normalizeAppResultContract(editDraft.resultContract, editingApp.kind, editDraft.outputModes)} onChange={(resultContract) => setEditDraft((current) => ({ ...current, resultContract }))} lang={lang} testIdPrefix="edit" />
                            <TestProtocolDesigner protocol={normalizeAppTestProtocol(editDraft.testProtocol, editingApp.kind, editDraft.outputModes, normalizeAppResultContract(editDraft.resultContract, editingApp.kind, editDraft.outputModes))} onChange={(testProtocol) => setEditDraft((current) => ({ ...current, testProtocol }))} lang={lang} testIdPrefix="edit" kind={editingApp.kind} />
                            </div>
                            <div className="apps-actions apps-manage-edit__actions">
                                {editSaveMessage && <span className="apps-manage-edit__message" data-state={editSaveState} role="alert">{editSaveMessage}</span>}
                                <button className="apps-secondary-button" type="button" onClick={cancelEdit}>{text.cancel}</button>
                                <button className="apps-primary-button" type="button" disabled={!editDraft.name.trim() || editSaveState === 'saving'} onClick={() => void saveEdit(editingApp)}>{editSaveState === 'saving' ? (isZh(lang) ? '\u4fdd\u5b58\u4e2d...' : 'Saving...') : text.save}</button>
                            </div>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
};

const MarketPane = ({ apps, lang, onInstallApp, prefill, onInstallResultVisibleChange, onManageApps }: { apps: AppEntry[]; lang?: string; onInstallApp: (app: AppEntry) => void; prefill?: { key: number; manifestText: string }; onInstallResultVisibleChange?: (visible: boolean) => void; onManageApps?: () => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [manifestText, setManifestText] = useState('');
    const [installState, setInstallState] = useState<'idle' | 'installed' | 'error'>('idle');
    const [installMessage, setInstallMessage] = useState('');
    const [installResultItems, setInstallResultItems] = useState<AppInstallResultItem[]>([]);
    const [installResultDependencyPlan, setInstallResultDependencyPlan] = useState<BackendAppInstallPlan | null>(null);
    const [installResultAppIDs, setInstallResultAppIDs] = useState<string[]>([]);
    const [selectedInstallKeys, setSelectedInstallKeys] = useState<string[] | null>(null);
    const [confirmHighRiskInstall, setConfirmHighRiskInstall] = useState(false);    const [hubMarketQuery, setHubMarketQuery] = useState('');
    const [hubMarketApps, setHubMarketApps] = useState<AppEntry[]>([]);
    const [hubMarketSearchState, setHubMarketSearchState] = useState<'idle' | 'searching' | 'ready' | 'error'>('idle');
    const [hubMarketSearchError, setHubMarketSearchError] = useState('');
	const [marketInstallFeedback, setMarketInstallFeedback] = useState<{ appId: string; state: 'running' | 'done' | 'error'; message: string; plan?: BackendAppInstallPlan | null; appIDs?: string[]; dependencies?: BackendAppInstallDependency[]; sourceAppCount?: number; installedAppCount?: number; versionSnapshot?: BackendAppInstallVersionSnapshot; installEvidence?: BackendAppInstallRecord } | null>(null);
	const [marketInstallAppId, setMarketInstallAppId] = useState('');
    const [backendInstallPlan, setBackendInstallPlan] = useState<BackendAppInstallPlan | null>(null);
    const [backendInstallPlanState, setBackendInstallPlanState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
    const [backendInstallPlanError, setBackendInstallPlanError] = useState('');
    const [installRecords, setInstallRecords] = useState<BackendAppInstallRecord[]>([]);
    const [installRecordsState, setInstallRecordsState] = useState<'loading' | 'ready' | 'error'>('loading');
	    const [installRecordsError, setInstallRecordsError] = useState('');
	    const [installRecordChecks, setInstallRecordChecks] = useState<Record<string, { state: 'loading' | 'repairing' | 'ready' | 'error'; plan?: BackendAppInstallPlan | null; error?: string }>>({});
	    const [installRecordDataSrvAudits, setInstallRecordDataSrvAudits] = useState<Record<string, { state: 'loading' | 'ready' | 'error'; message?: string; error?: string; resultState?: 'ready' | 'partial' | 'failed' | 'skipped' }>>({});
	    useEffect(() => {
	        onInstallResultVisibleChange?.(installState !== 'idle');
	        return () => onInstallResultVisibleChange?.(false);
	    }, [installState, onInstallResultVisibleChange]);
	    const refreshInstallRecords = useCallback(async () => {
        setInstallRecordsState('loading');
        setInstallRecordsError('');
        try {
            const records = await ListMaclawAppInstalls(6);
            setInstallRecords(Array.isArray(records) ? records : []);
            setInstallRecordsState('ready');
        } catch (error: any) {
            setInstallRecords([]);
            setInstallRecordsError(error?.message || String(error || ''));
            setInstallRecordsState('error');
        }
    }, []);
    useEffect(() => {
        void refreshInstallRecords();
    }, [refreshInstallRecords]);
    useEffect(() => {
        const nextManifest = prefill?.manifestText || '';
        if (!nextManifest) return;
        setManifestText(nextManifest);
        setInstallState('idle');
        setInstallMessage('');
        setInstallResultItems([]);
        setInstallResultDependencyPlan(null);
        setInstallResultAppIDs([]);
        setConfirmHighRiskInstall(false);
        setSelectedInstallKeys(null);
    }, [prefill?.key, prefill?.manifestText]);
    const installPreview = useMemo(() => {
        const value = manifestText.trim();
        if (!value) return { apps: [] as AppEntry[], error: '' };
        try {
            const parsed = JSON.parse(value);
            const result = manifestToAppEntries(parsed);
            return { apps: result.apps, error: result.apps.length > 0 ? '' : (result.error || text.schemaError) };
        } catch {
            return { apps: [] as AppEntry[], error: text.parseError };
        }
    }, [manifestText, text.parseError, text.schemaError]);
    useEffect(() => {
        const value = manifestText.trim();
        setBackendInstallPlan(null);
        setBackendInstallPlanError('');
        if (!value || installPreview.error) {
            setBackendInstallPlanState('idle');
            return;
        }
        let parsed: any;
        try {
            parsed = JSON.parse(value);
        } catch {
            setBackendInstallPlanState('idle');
            return;
        }
        if (parsed?.schema !== 'maclaw.app.v1' && parsed?.schema !== 'maclaw.app.pack.v1') {
            setBackendInstallPlanState('idle');
            return;
        }
        let cancelled = false;
        setBackendInstallPlanState('loading');
        PlanMaclawAppInstall(value)
            .then((plan) => {
                if (cancelled) return;
                setBackendInstallPlan(plan || null);
                setBackendInstallPlanState('ready');
            })
            .catch((error) => {
                if (cancelled) return;
                setBackendInstallPlan(null);
                setBackendInstallPlanError(error?.message || String(error || ''));
                setBackendInstallPlanState('error');
            });
        return () => { cancelled = true; };
    }, [installPreview.error, manifestText]);
    const installPlan = useMemo(() => buildInstallPlan(installPreview.apps, apps), [apps, installPreview.apps]);
    const installableKeys = useMemo(() => installPlan.filter((item) => item.action === 'install' || item.action === 'upgrade').map((item) => item.key), [installPlan]);
    useEffect(() => {
        setSelectedInstallKeys(installableKeys);
    }, [installableKeys]);
    const selectedInstallSet = new Set(selectedInstallKeys ?? installableKeys);
    const selectedInstallCount = installPlan.filter((item) => (item.action === 'install' || item.action === 'upgrade') && selectedInstallSet.has(item.key)).length;
    const selectedInstallAppIDs = installPlan.filter((item) => (item.action === 'install' || item.action === 'upgrade') && selectedInstallSet.has(item.key)).map((item) => item.app.id);
    const selectedHasWorkflowContractIssue = backendInstallPlanState === 'ready' && workflowContractHasIssueForAppIDs(backendInstallPlan, selectedInstallAppIDs);
    const selectedHasGovernanceReviewIssue = backendInstallPlanState === 'ready' && governanceReviewHasIssueForAppIDs(backendInstallPlan, selectedInstallAppIDs);
    const selectedHasMissingRequiredDependency = backendInstallPlanState === 'ready' && hasMissingRequiredBackendDependency(backendInstallPlan, selectedInstallAppIDs);
    const upgradeableCount = installPlan.filter((item) => item.action === 'upgrade').length;
    const skippedPreviewCount = installPlan.length - selectedInstallCount;
    const hasLiveManifestError = !!manifestText.trim() && !!installPreview.error && installState === 'idle';
    const searchHubMarketApps = async () => {
        const cleanQuery = hubMarketQuery.trim();
        if (!cleanQuery) return;
        setHubMarketSearchState('searching');
        setHubMarketSearchError('');
        try {
            const results = await SearchMixedSkills(cleanQuery);
            const nextApps = uniqueMarketApps((Array.isArray(results) ? results : [])
                .map((item) => marketAppEntryFromMixedSkillResult(item, lang))
                .filter((item): item is AppEntry => !!item));
            setHubMarketApps(nextApps);
            setHubMarketSearchState('ready');
        } catch (error: any) {
            setHubMarketApps([]);
            setHubMarketSearchError(error?.message || String(error || ''));
            setHubMarketSearchState('error');
        }
    };
    const marketRows = useMemo(() => {
        const marketApps = uniqueMarketApps([...marketCatalogApps, ...hubMarketApps]);
        const plan = buildInstallPlan(marketApps, apps);
        return marketApps.map((app) => {
            const item = plan.find((entry) => entry.app.id === app.id);
            const installed = item?.action === 'installed' || item?.action === 'duplicate';
            const upgrade = item?.action === 'upgrade';
            const actionText = installed ? text.alreadyInstalled : upgrade ? text.willUpgrade : text.marketAdd;
            return { app, installed, upgrade, actionText };
        });
    }, [apps, hubMarketApps, text.alreadyInstalled, text.marketAdd, text.willUpgrade]);
    const addableMarketCount = marketRows.filter((item) => !item.installed && !item.upgrade).length;
    const upgradeableMarketCount = marketRows.filter((item) => item.upgrade).length;
    const installRecordKey = (record: BackendAppInstallRecord, index = 0) => [installRecordPackageSHA(record), record.installed_at || '', (record.apps || []).map((app) => app.id).join(','), String(index)].join(':');
    const checkInstallRecordDependencies = async (record: BackendAppInstallRecord, index: number) => {
        const key = installRecordKey(record, index);
        if (!record.package) {
            setInstallRecordChecks((current) => ({ ...current, [key]: { state: 'error', error: text.installRecordPackageMissing } }));
            return;
        }
        setInstallRecordChecks((current) => ({ ...current, [key]: { state: 'loading' } }));
        try {
            const plan = await PlanMaclawAppInstall(JSON.stringify(record.package));
            setInstallRecordChecks((current) => ({ ...current, [key]: { state: 'ready', plan: plan || null } }));
        } catch (error: any) {
            setInstallRecordChecks((current) => ({ ...current, [key]: { state: 'error', error: error?.message || String(error || '') } }));
        }
    };
    const repairInstallRecordDependencies = async (record: BackendAppInstallRecord, index: number) => {
        const key = installRecordKey(record, index);
        if (!record.package) {
            setInstallRecordChecks((current) => ({ ...current, [key]: { state: 'error', error: text.installRecordPackageMissing } }));
            return;
        }
        const previous = installRecordChecks[key];
        setInstallRecordChecks((current) => ({ ...current, [key]: { ...current[key], state: 'repairing', plan: current[key]?.plan || previous?.plan || null } }));
        try {
            const plan = await InstallMaclawAppDependencies(JSON.stringify(record.package));
            setInstallRecordChecks((current) => ({ ...current, [key]: { state: 'ready', plan: plan || null } }));
            void refreshInstallRecords();
        } catch (error: any) {
            setInstallRecordChecks((current) => ({ ...current, [key]: { state: 'error', plan: previous?.plan || null, error: error?.message || String(error || '') } }));
        }
    };
    const auditInstallRecordDataSrvRegistration = async (record: BackendAppInstallRecord, index: number) => {
        const key = installRecordKey(record, index);
        setInstallRecordDataSrvAudits((current) => ({ ...current, [key]: { state: 'loading' } }));
        try {
            const result = await fetchDataSrvInstallRegistrationAudit(record, text);
            setInstallRecordDataSrvAudits((current) => ({ ...current, [key]: { state: 'ready', message: result.message, resultState: result.state } }));
        } catch (error: any) {
            setInstallRecordDataSrvAudits((current) => ({ ...current, [key]: { state: 'error', error: error?.message || String(error || '') } }));
        }
    };
    const installSingleMarketApp = async (app: AppEntry) => {
        const manifestText = JSON.stringify(appToManifest(app));
        setMarketInstallAppId(app.id);
        setMarketInstallFeedback({ appId: app.id, state: 'running', message: text.dependencyPlanLoading, appIDs: [app.id] });
        try {
            if (app.marketInstallSource === 'enterprise_hub' && app.marketCapabilityID) {
                const selectedHubAppIDs = [app.id].map((id) => String(id || '').trim()).filter(Boolean);
                if (selectedHubAppIDs.length === 0) throw new Error(text.schemaError);
                const hubInstall = await InstallSelectedMaclawAppPackageFromHub(app.marketCapabilityID, selectedHubAppIDs) as Record<string, any>;
                const rawPackage = hubInstall?.package || (hubInstall?.package_json ? JSON.parse(String(hubInstall.package_json)) : null);
                const parsed = manifestToAppEntries(rawPackage);
                if (parsed.apps.length === 0) throw new Error(parsed.error || text.schemaError);
                const dependencyInstallPlan = (hubInstall?.install_plan || null) as BackendAppInstallPlan | null;
                const installAudit = (hubInstall?.install_record || null) as BackendAppInstallRecord | null;
                const installedApps = parsed.apps.map((installedApp) => ({
                    ...installedApp,
                    marketCapabilityID: app.marketCapabilityID,
                    marketInstallSource: app.marketInstallSource,
                    marketSourceLabel: app.marketSourceLabel,
                }));
                const installedAppIDs = installedApps.map((item) => item.id);
                if (installedApps.some((installedApp) => isEnterpriseAppKind(installedApp.kind)) && !installAudit) {
                    throw new Error(text.installAuditRequired);
                }
                installedApps.forEach((installedApp) => onInstallApp(installedAppWithInstallEvidence(installedApp, installAudit)));
                await refreshInstallRecords();
                const primaryAppID = installedAppIDs[0] || app.id;
                const dependencyDetails = dependencyInstallPlan?.dependencies || [];
                const sourceAppCount = Number(hubInstall?.source_app_count || 0) || undefined;
                const installedAppCount = Number(hubInstall?.app_count || installedAppIDs.length || 0) || undefined;
                const scopeSummary = formatHubInstallScopeSummary(sourceAppCount, installedAppCount, dependencyDetails.length, text);
                setMarketInstallFeedback({
                    appId: app.id,
                    state: 'done',
                    message: scopeSummary ? `${text.alreadyInstalled} · ${scopeSummary}` : text.alreadyInstalled,
                    plan: dependencyInstallPlan || null,
                    appIDs: installedAppIDs.length > 0 ? installedAppIDs : [app.id],
                    dependencies: dependencyDetails,
                    sourceAppCount,
                    installedAppCount,
                    versionSnapshot: installRecordVersionSnapshotForApp(installAudit, primaryAppID),
                    installEvidence: installEvidenceRecordForApp(installAudit, primaryAppID),
                });
                return;
            }
            const dependencyInstallPlan = await InstallMaclawAppDependencies(manifestText);
            const dependencyDetails = backendDependenciesForApp(dependencyInstallPlan, app.id);
            const installAppIDs = [app.id];
            if (governanceReviewHasIssueForAppIDs(dependencyInstallPlan, installAppIDs)) {
                setMarketInstallFeedback({
                    appId: app.id,
                    state: 'error',
                    message: governanceReviewIssueMessageForAppIDs(dependencyInstallPlan, installAppIDs, text),
                    plan: dependencyInstallPlan || null,
                    appIDs: installAppIDs,
                    dependencies: dependencyDetails.length > 0 ? dependencyDetails : dependencyInstallPlan?.dependencies || [],
                });
                return;
            }
            if (workflowContractHasIssueForAppIDs(dependencyInstallPlan, installAppIDs)) {
                setMarketInstallFeedback({
                    appId: app.id,
                    state: 'error',
                    message: workflowContractIssueMessageForAppIDs(dependencyInstallPlan, installAppIDs, text),
                    plan: dependencyInstallPlan || null,
                    appIDs: installAppIDs,
                    dependencies: dependencyDetails.length > 0 ? dependencyDetails : dependencyInstallPlan?.dependencies || [],
                });
                return;
            }
            if (runtimeInstallPlanBlocked(dependencyInstallPlan, app)) {
                setMarketInstallFeedback({
                    appId: app.id,
                    state: 'error',
                    message: runtimeInstallPlanBlockMessage(app, dependencyInstallPlan, text, lang),
                    plan: dependencyInstallPlan || null,
                    appIDs: [app.id],
                    dependencies: dependencyDetails.length > 0 ? dependencyDetails : dependencyInstallPlan?.dependencies || [],
                });
                return;
            }
            let installFeedbackMessage = text.alreadyInstalled;
            let installAudit: BackendAppInstallRecord | null = null;
            try {
                installAudit = await RecordMaclawAppInstall(manifestText, 'market') as BackendAppInstallRecord;
                const dataSrvSummary = appHasDataSrvRegistrationCandidate(app) ? dataSrvRegistrationSummary(installAudit?.datasrv_registration, text) : '';
                if (dataSrvSummary) installFeedbackMessage = `${installFeedbackMessage} · ${dataSrvSummary}`;
                await refreshInstallRecords();
            } catch (error: any) {
                if (isEnterpriseAppKind(app.kind)) {
                    throw new Error(error?.message || text.installAuditRequired);
                }
                // Dependency health is the install gate; audit refresh failures should not discard an otherwise valid local install.
            }
            onInstallApp(installedAppWithInstallEvidence(app, installAudit));
            setMarketInstallFeedback({
                appId: app.id,
                state: 'done',
                message: installFeedbackMessage,
                plan: dependencyInstallPlan || null,
				appIDs: [app.id],
				dependencies: dependencyDetails.length > 0 ? dependencyDetails : dependencyInstallPlan?.dependencies || [],
				versionSnapshot: installRecordVersionSnapshotForApp(installAudit, app.id),
				installEvidence: installEvidenceRecordForApp(installAudit, app.id),
			});
        } catch (error: any) {
            setMarketInstallFeedback({ appId: app.id, state: 'error', message: error?.message || text.installError, appIDs: [app.id] });
        } finally {
            setMarketInstallAppId('');
        }
    };
    const installManifest = async () => {
        try {
            let parsed: any;
            try {
                parsed = JSON.parse(manifestText);
            } catch {
                throw new Error(text.parseError);
            }
            const parsedResult = manifestToAppEntries(parsed);
            const parsedApps = parsedResult.apps;
            if (parsedApps.length === 0) throw new Error(parsedResult.error || text.schemaError);
            const plan = buildInstallPlan(parsedApps, apps);
            const selectableKeys = plan.filter((item) => item.action === 'install' || item.action === 'upgrade').map((item) => item.key);
            const selectedKeys = new Set(selectedInstallKeys ?? selectableKeys);
            const selectedAppIDs = plan.filter((item) => (item.action === 'install' || item.action === 'upgrade') && selectedKeys.has(item.key)).map((item) => item.app.id);
            const selectedApps = plan.filter((item) => (item.action === 'install' || item.action === 'upgrade') && selectedKeys.has(item.key)).map((item) => item.app);
            const selectedInstallManifestText = selectedApps.length > 0 ? JSON.stringify(appsToInstallManifest(selectedApps)) : '';
            const shouldUseBackendInstallGate = parsed?.schema === 'maclaw.app.v1' || parsed?.schema === 'maclaw.app.pack.v1';
            let dependencyPlanForResult: BackendAppInstallPlan | null = backendInstallPlanState === 'ready' ? backendInstallPlan : null;
            if (shouldUseBackendInstallGate && selectedInstallManifestText) {
                if (!dependencyPlanForResult) {
                    dependencyPlanForResult = await PlanMaclawAppInstall(selectedInstallManifestText);
                    setBackendInstallPlan(dependencyPlanForResult || null);
                    setBackendInstallPlanState('ready');
                }
                if (governanceReviewHasIssueForAppIDs(dependencyPlanForResult, selectedAppIDs)) {
                    throw new Error(governanceReviewIssueMessageForAppIDs(dependencyPlanForResult, selectedAppIDs, text));
                }
                if (workflowContractHasIssueForAppIDs(dependencyPlanForResult, selectedAppIDs)) {
                    throw new Error(workflowContractIssueMessageForAppIDs(dependencyPlanForResult, selectedAppIDs, text));
                }
            }
            if (shouldUseBackendInstallGate && selectedInstallManifestText && hasMissingRequiredBackendDependency(dependencyPlanForResult, selectedAppIDs)) {
                const dependencyInstallPlan = await InstallMaclawAppDependencies(selectedInstallManifestText);
                dependencyPlanForResult = dependencyInstallPlan || null;
                setBackendInstallPlan(dependencyInstallPlan || null);
                setBackendInstallPlanState('ready');
                if (governanceReviewHasIssueForAppIDs(dependencyInstallPlan, selectedAppIDs)) {
                    throw new Error(governanceReviewIssueMessageForAppIDs(dependencyInstallPlan, selectedAppIDs, text));
                }
                if (workflowContractHasIssueForAppIDs(dependencyInstallPlan, selectedAppIDs)) {
                    throw new Error(workflowContractIssueMessageForAppIDs(dependencyInstallPlan, selectedAppIDs, text));
                }
                if (hasMissingRequiredBackendDependency(dependencyInstallPlan, selectedAppIDs)) {
                    throw new Error(text.missingRequiredDependency);
                }
            }
            const selectedHasHighRiskUpgrade = plan.some((item) => item.action === 'upgrade' && selectedKeys.has(item.key) && item.highRiskScopes.length > 0);
            if (selectedHasHighRiskUpgrade && !confirmHighRiskInstall) {
                setConfirmHighRiskInstall(true);
                setInstallState('idle');
                setInstallMessage(text.highRiskInstallWarning);
                setInstallResultItems([]);
                setInstallResultDependencyPlan(null);
                setInstallResultAppIDs([]);
                return;
            }
            const resultItems = plan.map((item): AppInstallResultItem => {
                const selected = (item.action === 'install' || item.action === 'upgrade') && selectedKeys.has(item.key);
                if (selected && item.action === 'install') {
                    return { key: item.key, appID: item.app.id, dataSrvCandidate: appHasDataSrvRegistrationCandidate(item.app), name: item.app.name, icon: item.app.icon, customIconDataUrl: item.app.customIconDataUrl, accent: item.app.accent, action: 'installed', detail: text.installedCount };
                }
                if (selected && item.action === 'upgrade') {
                    return {
                        key: item.key,
                        appID: item.app.id,
                        dataSrvCandidate: appHasDataSrvRegistrationCandidate(item.app),
                        name: item.app.name,
                        icon: item.app.icon,
                        customIconDataUrl: item.app.customIconDataUrl,
                        accent: item.app.accent,
                        action: 'upgraded',
                        detail: `${text.upgradedItem} v${normalizeAppVersion(item.installed?.version)} -> v${normalizeAppVersion(item.app.version)}`,
                    };
                }
                const reason = item.action === 'installed'
                    ? text.alreadyInstalled
                    : item.action === 'duplicate'
                        ? text.duplicateApp
                        : text.notSelected;
                return { key: item.key, appID: item.app.id, dataSrvCandidate: appHasDataSrvRegistrationCandidate(item.app), name: item.app.name, icon: item.app.icon, customIconDataUrl: item.app.customIconDataUrl, accent: item.app.accent, action: 'skipped', detail: reason };
            });
            const nextApps = selectedApps;
            const installedActionCount = plan.filter((item) => item.action === 'install' && selectedKeys.has(item.key)).length;
            const upgradedActionCount = plan.filter((item) => item.action === 'upgrade' && selectedKeys.has(item.key)).length;
            const skippedActionCount = plan.length - nextApps.length;
            const installedResultKeys = new Set(resultItems.filter((item) => item.action !== 'skipped').map((item) => item.key));
            const hasEnterpriseInstall = nextApps.some((installedApp) => isEnterpriseAppKind(installedApp.kind));
            let installAudit: BackendAppInstallRecord | null = null;
            let dataSrvRegistration: BackendAppDataSrvRegistration | undefined;
            if (!hasEnterpriseInstall) {
                nextApps.forEach(onInstallApp);
                setInstallMessage(installSummaryMessage(installedActionCount, upgradedActionCount, skippedActionCount, text));
                setInstallResultItems(resultItems);
                setInstallResultDependencyPlan(dependencyPlanForResult);
                setInstallResultAppIDs(selectedAppIDs);
                setInstallState('installed');
                setConfirmHighRiskInstall(false);
                if (nextApps.length > 0) {
                    const installAuditPackage = appsToInstallManifest(nextApps);
                    try {
                        installAudit = await RecordMaclawAppInstall(JSON.stringify(installAuditPackage), 'market') as BackendAppInstallRecord;
                        dataSrvRegistration = installAudit?.datasrv_registration;
                        await refreshInstallRecords();
                        nextApps.forEach((installedApp) => onInstallApp(installedAppWithInstallEvidence(installedApp, installAudit)));
                        setInstallResultItems((current) => current.map((item) => {
                            if (!installedResultKeys.has(item.key)) return item;
                            const versionSnapshot = item.appID ? installRecordVersionSnapshotForApp(installAudit, item.appID) : undefined;
                            const installEvidence = installEvidenceRecordForApp(installAudit, item.appID);
                            return {
                                ...item,
                                detail: dataSrvRegistration ? installDetailWithDataSrvRegistration(item.detail, dataSrvRegistration, item.appID, item.dataSrvCandidate, text) : item.detail,
                                versionSnapshot: versionSnapshot || item.versionSnapshot,
                                installEvidence: installEvidence || item.installEvidence,
                            };
                        }));
                    } catch {
                        // Dependency health is the install gate for tool apps; audit refresh failures should not discard an otherwise valid local install.
                    }
                }
                return;
            }
            if (nextApps.length > 0) {
                const installAuditPackage = appsToInstallManifest(nextApps);
                try {
                    installAudit = await RecordMaclawAppInstall(JSON.stringify(installAuditPackage), 'market') as BackendAppInstallRecord;
                    dataSrvRegistration = installAudit?.datasrv_registration;
                    await refreshInstallRecords();
                } catch (error: any) {
                    throw new Error(error?.message || text.installAuditRequired);
                }
            }
            nextApps.forEach((installedApp) => onInstallApp(installedAppWithInstallEvidence(installedApp, installAudit)));
            setInstallMessage(installSummaryMessage(installedActionCount, upgradedActionCount, skippedActionCount, text));
            setInstallResultItems(resultItems.map((item) => {
                if (!installedResultKeys.has(item.key)) return item;
                const versionSnapshot = item.appID ? installRecordVersionSnapshotForApp(installAudit, item.appID) : undefined;
                const installEvidence = installEvidenceRecordForApp(installAudit, item.appID);
                return {
                    ...item,
                    detail: dataSrvRegistration ? installDetailWithDataSrvRegistration(item.detail, dataSrvRegistration, item.appID, item.dataSrvCandidate, text) : item.detail,
                    versionSnapshot: versionSnapshot || item.versionSnapshot,
                    installEvidence: installEvidence || item.installEvidence,
                };
            }));
            setInstallResultDependencyPlan(dependencyPlanForResult);
            setInstallResultAppIDs(selectedAppIDs);
            setInstallState('installed');
            setConfirmHighRiskInstall(false);
        } catch (error: any) {
            setInstallMessage(error?.message || text.installError);
            setInstallResultItems([]);
            setInstallResultDependencyPlan(null);
            setInstallResultAppIDs([]);
            setInstallState('error');
            setConfirmHighRiskInstall(false);
        }
    };
    return (
        <>
	            <section className="apps-market-list" aria-label={text.marketApps}>
	                <div className="apps-preview-title-row">
	                    <div>
	                        <div className="apps-definition__title">{text.marketApps}</div>
	                        <div className="apps-market-list__meta">{text.marketAddableCount} {addableMarketCount} · {text.marketUpgradeableCount} {upgradeableMarketCount}</div>
	                    </div>
	                    {onManageApps && <button className="apps-secondary-button" type="button" onClick={onManageApps}>{text.manageTab}</button>}
	                </div>
                <div className="apps-market-search">
                    <input
                        value={hubMarketQuery}
                        onChange={(event) => setHubMarketQuery(event.target.value)}
                        onKeyDown={(event) => {
                            if (event.key === 'Enter') {
                                event.preventDefault();
                                void searchHubMarketApps();
                            }
                        }}
                        placeholder={text.marketHubSearchPlaceholder}
                        aria-label={text.marketHubSearchPlaceholder}
                    />
                    <button className="apps-secondary-button" type="button" disabled={!hubMarketQuery.trim() || hubMarketSearchState === 'searching'} onClick={() => void searchHubMarketApps()}>
                        {hubMarketSearchState === 'searching' ? text.marketHubSearching : text.marketHubSearch}
                    </button>
                    {hubMarketSearchState === 'ready' && hubMarketQuery.trim() && hubMarketApps.length === 0 && <span role="status">{text.marketHubEmpty}</span>}
                    {hubMarketSearchState === 'error' && <span role="alert">{text.marketHubError}: {hubMarketSearchError}</span>}
                </div>
                <div className="apps-market-list__items">
                    {marketRows.map(({ app, installed, upgrade, actionText }) => {
                        const isInstalling = marketInstallAppId === app.id;
                        const feedback = marketInstallFeedback?.appId === app.id ? marketInstallFeedback : null;
                        const buttonText = isInstalling ? text.installing : actionText;
                        return (
                            <div className="apps-market-row" key={app.id} data-state={feedback?.state === 'error' ? 'blocked' : isInstalling ? 'running' : installed ? 'installed' : upgrade ? 'upgrade' : 'available'}>
                                <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><AppIcon icon={app.icon} customIconDataUrl={app.customIconDataUrl} /></span>
                                <div className="apps-market-row__main">
                                    <strong>{app.name}</strong>
                                    <span>{app.description}</span>
                                    <small>{app.category} · {appKinds[app.kind][isZh(lang) ? 'zh' : 'en']} · {app.marketSourceLabel || sourceLabels[app.source][isZh(lang) ? 'zh' : 'en']}</small>
                                    {app.marketReviewEvidence && <PublishReviewEvidenceStrip evidence={app.marketReviewEvidence} text={text} />}
	                                </div>
	                                <div className="apps-actions">
	                                    {feedback && feedback.state !== 'running' && (
	                                        <button className="apps-secondary-button" type="button" onClick={() => { setMarketInstallFeedback(null); setMarketInstallAppId(''); }}>{text.close}</button>
	                                    )}
	                                    <button className={installed ? 'apps-secondary-button' : 'apps-primary-button'} type="button" disabled={installed || isInstalling} title={`${buttonText}: ${app.name}`} aria-label={`${buttonText}: ${app.name}`} onClick={() => void installSingleMarketApp(app)}>
	                                        {buttonText}
	                                    </button>
	                                </div>
	                                {feedback && (
	                                    <div className="apps-market-row__install-detail" aria-live="polite">
	                                        <MarketInstallFeedbackMessage feedback={feedback} text={text} />
	                                        {feedback.plan ? (
	                                            <DependencyVerificationPanel
	                                                plan={feedback.plan}
	                                                state={feedback.state === 'running' ? 'loading' : feedback.state === 'error' ? 'ready' : 'ready'}
	                                                selectedAppIDs={feedback.appIDs || [app.id]}
	                                                text={text}
	                                                defaultOpen={false}
	                                            />
	                                        ) : feedback.dependencies?.length ? <InstallRecordDependencies dependencies={feedback.dependencies} text={text} /> : null}
	                                        <InstallVersionSnapshot snapshot={feedback.versionSnapshot} text={text} />
	                                        {feedback.installEvidence && <InstallRecordEvidenceSnapshot record={feedback.installEvidence} text={text} />}
	                                    </div>
	                                )}
	                            </div>
                        );
                    })}
                </div>
            </section>
            <section className="apps-install-records" aria-label={text.installRecords}>
                <div className="apps-preview-title-row">
                    <div>
                        <div className="apps-definition__title">{text.installRecords}</div>
                        <div className="apps-market-list__meta">{text.installRecordsHint}</div>
                    </div>
                    <button className="apps-secondary-button" type="button" onClick={() => void refreshInstallRecords()}>{text.refreshInstallRecords}</button>
                </div>
                {installRecordsState === 'loading' && <div className="apps-install-records__empty">{text.installRecordsLoading}</div>}
                {installRecordsState === 'error' && <div className="apps-install-records__empty" role="alert">{`${text.installRecordsError}: ${installRecordsError}`}</div>}
                {installRecordsState === 'ready' && installRecords.length === 0 && <div className="apps-install-records__empty">{text.noInstallRecords}</div>}
                {installRecordsState === 'ready' && installRecords.length > 0 && (
                    <div className="apps-install-records__list">
                        {installRecords.map((record, index) => {
                            const appNames = (record.apps || []).map((app) => app.name || app.id).filter(Boolean).join(', ') || '-';
                            const dependencies = record.dependencies || [];
                            const dependencyCount = dependencies.length;
                            const missingCount = installRecordMissingDependencyCount(record);
                            const sha = installRecordPackageSHA(record).slice(0, 12) || '-';
                            const recordKey = installRecordKey(record, index);
                            const check = installRecordChecks[recordKey];
                            const dataSrvAudit = installRecordDataSrvAudits[recordKey];
                            const selectedRecordAppIDs = (record.apps || []).map((app) => String(app.id || '')).filter(Boolean);
                            const canAuditDataSrvRegistration = !!record.datasrv_registration && selectedRecordAppIDs.length > 0;
                            const checkPanelState = check?.state === 'repairing' ? 'loading' : check?.state;
                            const canRepairCheckedDependencies = !!record.package && (check?.state === 'ready' || check?.state === 'repairing') && hasMissingRequiredBackendDependency(check.plan, selectedRecordAppIDs);
                            return (
                                <div className="apps-install-record" key={recordKey} data-missing={missingCount > 0 ? 'true' : 'false'}>
                                    <div className="apps-install-record__main">
                                        <strong>{appNames}</strong>
                                        <span>{text.installedAt}: {formatInstallRecordTime(record.installed_at)} · {text.marketSource}: {record.source || '-'}</span>
                                        <small>{text.packageSha}: {sha} · {text.skillDependencies}: {dependencyCount} · {text.missingDependencyCount}: {missingCount}</small>
                                        <InstallVersionSnapshot snapshot={record.version_snapshot} text={text} />
                                        <InstallRecordEvidenceSnapshot record={record} text={text} />
                                        <InstallRecordDependencies dependencies={dependencies} text={text} />
                                        {check && checkPanelState && <DependencyVerificationPanel plan={check.plan || null} state={checkPanelState} error={check.error} selectedAppIDs={selectedRecordAppIDs} text={text} />}
                                    </div>
                                    <div className="apps-install-record__actions">
                                        <button className="apps-secondary-button" type="button" disabled={check?.state === 'loading' || check?.state === 'repairing' || !record.package} onClick={() => void checkInstallRecordDependencies(record, index)}>
                                            {check?.state === 'loading' || check?.state === 'repairing' ? text.checkingInstallDependencies : text.recheckInstallDependencies}
                                        </button>
                                        {canAuditDataSrvRegistration && (
                                            <button className="apps-secondary-button" type="button" disabled={dataSrvAudit?.state === 'loading'} onClick={() => void auditInstallRecordDataSrvRegistration(record, index)}>
                                                {dataSrvAudit?.state === 'loading' ? text.auditingDataSrvRegistration : text.auditDataSrvRegistration}
                                            </button>
                                        )}
                                        {canRepairCheckedDependencies && (
                                            <button className="apps-primary-button" type="button" disabled={check?.state === 'repairing'} onClick={() => void repairInstallRecordDependencies(record, index)}>
                                                {check?.state === 'repairing' ? text.repairingInstallDependencies : text.repairInstallDependencies}
                                            </button>
                                        )}
                                        {dataSrvAudit?.state === 'ready' && <em data-state={dataSrvAudit.resultState} role="status">{dataSrvAudit.message}</em>}
                                        {dataSrvAudit?.state === 'error' && <em data-state="failed" role="alert">{dataSrvAudit.error}</em>}
                                        {check?.state === 'repairing' && <em>{text.repairingInstallDependencies}</em>}
                                        {check?.state !== 'repairing' && <em>{record.has_blocking_dependency || record.has_missing_required || missingCount > 0 ? text.unavailableDependency : text.installedDependency}</em>}
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                )}
            </section>
            <details className="apps-market-install" open={!!manifestText.trim() || undefined}>
                <summary>
                    <span>
                        <span className="apps-definition__title">{text.marketAdvancedImport}</span>
                        <span className="apps-market-list__meta">{text.marketAdvancedImportHint}</span>
                    </span>
                </summary>
                <div className="apps-market-install__body">
                    <div className="apps-preview-title-row">
                        <div className="apps-definition__title">{text.installManifest}</div>
                        <button className="apps-primary-button" type="button" disabled={!manifestText.trim() || !!installPreview.error || selectedHasGovernanceReviewIssue || selectedHasWorkflowContractIssue || (installPlan.length > 0 && selectedInstallCount === 0)} onClick={installManifest}>{confirmHighRiskInstall ? text.confirmHighRiskInstall : text.install}</button>
                    </div>
                    <textarea aria-label={text.installManifest} value={manifestText} onChange={(event) => {
                        setManifestText(event.target.value);
                        setInstallState('idle');
                        setInstallMessage('');
                        setInstallResultItems([]);
                        setInstallResultDependencyPlan(null);
                        setInstallResultAppIDs([]);
                        setConfirmHighRiskInstall(false);
                        setSelectedInstallKeys(null);
                    }} placeholder={text.pasteManifest} />
                    {hasLiveManifestError && (
                        <div className="apps-result-panel" data-state="error" role="alert">
                            <span>{`${text.installError}: ${installPreview.error}`}</span>
                        </div>
                    )}
                    {installPlan.length > 0 && (
                        <div className="apps-install-preview">
                            {confirmHighRiskInstall && <div className="apps-install-preview__warning" role="alert">{text.highRiskInstallWarning}</div>}
                            {backendInstallPlanState === 'loading' && <div className="apps-install-preview__warning">{text.dependencyPlanLoading}</div>}
                            {backendInstallPlanState === 'error' && <div className="apps-install-preview__warning" role="alert">{`${text.dependencyPlanError}: ${backendInstallPlanError}`}</div>}
                            {selectedHasGovernanceReviewIssue && <div className="apps-install-preview__warning" role="alert">{governanceReviewIssueMessageForAppIDs(backendInstallPlan, selectedInstallAppIDs, text)}</div>}
                            {selectedHasWorkflowContractIssue && <div className="apps-install-preview__warning" role="alert">{workflowContractIssueMessageForAppIDs(backendInstallPlan, selectedInstallAppIDs, text)}</div>}
                            {selectedHasMissingRequiredDependency && <div className="apps-install-preview__warning" role="alert">{text.missingRequiredDependency}</div>}
                            <DependencyVerificationPanel plan={backendInstallPlan} state={backendInstallPlanState} error={backendInstallPlanError} selectedAppIDs={selectedInstallAppIDs} text={text} />
                            <div className="apps-preview-title-row">
                                <div className="apps-definition__title">{text.installPreview}</div>
                                <div className="apps-install-preview__tools">
                                    <button className="apps-secondary-button" type="button" disabled={installableKeys.length === 0 || selectedInstallCount === installableKeys.length} onClick={() => { setConfirmHighRiskInstall(false); setSelectedInstallKeys(installableKeys); }}>{text.selectAll}</button>
                                    <button className="apps-secondary-button" type="button" disabled={selectedInstallCount === 0} onClick={() => { setConfirmHighRiskInstall(false); setSelectedInstallKeys([]); }}>{text.clearSelection}</button>
                                    <span className="apps-count">{`${text.installableCount} ${installableKeys.length - upgradeableCount} · ${text.upgradeableCount} ${upgradeableCount} · ${text.willSkip} ${skippedPreviewCount}`}</span>
                                    <span className="apps-count">{selectedInstallCount}/{installPlan.length}</span>
                                </div>
                            </div>
                            <div className="apps-install-preview__list">
                                {installPlan.map((item) => {
                                    const checked = (item.action === 'install' || item.action === 'upgrade') && selectedInstallSet.has(item.key);
                                    const statusText = checked
                                        ? item.action === 'upgrade' ? `${text.willUpgrade} v${normalizeAppVersion(item.installed?.version)} -> v${normalizeAppVersion(item.app.version)}` : text.willInstall
                                        : item.action === 'installed'
                                            ? `${text.willSkip} · ${text.alreadyInstalled}`
                                            : item.action === 'duplicate'
                                                ? `${text.willSkip} · ${text.duplicateApp}`
                                                : `${text.willSkip} · ${text.notSelected}`;
                                    const backendDependencies = backendDependenciesForApp(backendInstallPlan, item.app.id);
                                    const workflowIssue = backendInstallPlanState === 'ready' ? workflowContractIssueForApp(backendInstallPlan, item.app) : undefined;
                                    const governanceIssue = backendInstallPlanState === 'ready' ? governanceReviewIssueForApp(backendInstallPlan, item.app) : undefined;
                                    const hasBlockingDependencies = backendInstallPlanState === 'ready' && (backendDependencies.some(isBlockingBackendDependency) || !!workflowIssue || !!governanceIssue);
                                    const dependencyText = backendInstallPlanState === 'ready' && backendDependencies.length > 0
                                        ? backendDependencies.map((dep) => backendDependencySummary(dep, text)).join(', ')
                                        : item.dependencies.map((dep) => dep.version ? `${dep.id}@${dep.version}` : dep.id).join(', ');
                                    const checkboxLabel = `${item.app.name} · ${statusText}`;
                                    return (
                                    <label className="apps-install-preview__row" key={item.key} data-action={checked ? 'install' : 'skip'} data-dependency-state={hasBlockingDependencies ? 'blocked' : undefined} title={statusText}>
                                        <input
                                            type="checkbox"
                                            aria-label={checkboxLabel}
                                            checked={checked}
                                            disabled={item.action !== 'install' && item.action !== 'upgrade'}
                                            onChange={(event) => {
                                                setSelectedInstallKeys((current) => {
                                                    const nextKeys = current ?? installableKeys;
                                                    return event.target.checked
                                                        ? Array.from(new Set([...nextKeys, item.key]))
                                                        : nextKeys.filter((key) => key !== item.key);
                                                });
                                                setConfirmHighRiskInstall(false);
                                            }}
                                        />
                                        <span className="apps-app-icon" style={{ '--apps-icon-color': item.app.accent } as CSSProperties}><AppIcon icon={item.app.icon} customIconDataUrl={item.app.customIconDataUrl} /></span>
                                        <div>
                                            <strong>{item.app.name}</strong>
                                            <span>{item.app.category} · {appKinds[item.app.kind][isZh(lang) ? 'zh' : 'en']}</span>
                                            {dependencyText && <small>{text.skillDependencies}: {dependencyText}</small>}
                                            {governanceIssue && <small>{text.reviewIssues}: {reviewIssueSummary(governanceIssue)}</small>}
                                            {workflowIssue && <small>{text.workflowContract}: {reviewIssueSummary(workflowIssue)}</small>}
                                            {item.action === 'upgrade' && item.addedScopes.length > 0 && (
                                                <small>
                                                    {text.permissionChanges}: +{item.addedScopes.join(', ')}
                                                    {item.highRiskScopes.length > 0 ? ` · ${text.highRiskPermission}: ${item.highRiskScopes.join(', ')}` : ''}
                                                </small>
                                            )}
                                        </div>
                                        <em>{statusText}</em>
                                    </label>
                                );})}
                            </div>
                        </div>
                    )}
	                    {installState !== 'idle' && (
	                        <div className={`apps-result-panel${installState === 'installed' && (installResultItems.length > 0 || installResultDependencyPlan) ? ' apps-result-panel--stacked' : ''}`} data-state={installState === 'installed' ? 'done' : 'error'} role={installState === 'installed' ? 'status' : 'alert'} aria-live={installState === 'installed' ? 'polite' : undefined}>
	                            <span>{installState === 'installed' ? installMessage : `${text.installError}: ${installMessage}`}</span>
	                            <button className="apps-secondary-button" type="button" onClick={() => {
	                                setInstallState('idle');
	                                setInstallMessage('');
	                                setInstallResultItems([]);
	                                setInstallResultDependencyPlan(null);
	                                setInstallResultAppIDs([]);
	                                setManifestText('');
	                                setSelectedInstallKeys(null);
	                                setConfirmHighRiskInstall(false);
	                            }}>{text.close}</button>
	                            {installState === 'installed' && installResultItems.length > 0 && (
	                                <div className="apps-install-result" role="list" aria-label={text.installDetails}>
                                    {installResultItems.map((item) => (
                                        <div className="apps-install-result__row" data-action={item.action} role="listitem" key={item.key}>
                                            <span className="apps-app-icon" style={{ '--apps-icon-color': item.accent } as CSSProperties}><AppIcon icon={item.icon} customIconDataUrl={item.customIconDataUrl} /></span>
                                            <strong>{item.name}</strong>
                                            <em>{item.action === 'upgraded' ? text.upgradedItem : item.action === 'installed' ? text.installedCount : text.skippedItem}</em>
	                                            <small>{item.detail}</small>
	                                            <InstallVersionSnapshot snapshot={item.versionSnapshot} text={text} />
	                                            {item.installEvidence && <InstallRecordEvidenceSnapshot record={item.installEvidence} text={text} />}
	                                        </div>
                                    ))}
                                </div>
                            )}
                            {installState === 'installed' && installResultDependencyPlan && (
                                <DependencyVerificationPanel plan={installResultDependencyPlan} state="ready" selectedAppIDs={installResultAppIDs} text={text} />
                            )}
                        </div>
                    )}
                </div>
            </details>
        </>
    );
};

export default AppsPage;
