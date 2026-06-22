import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { CSSProperties, KeyboardEvent } from 'react';
import { CancelNLSkillRun, DownloadSkillRunArtifact, ExecuteMaclawAppBusinessOperation, GetMISDataConfig, GetNLSkillRunStatus, ListMaclawAppApprovalInstances, ListMaclawAppApprovalInstancesAll, ListMaclawAppInstalls, ListNLSkills, ListSkillAppManifests, LoadConfig, OpenFileOrShowInFolder, InstallMaclawAppDependencies, PlanMaclawAppInstall, RecordMaclawAppApprovalInstance, RecordMaclawAppInstall, SyncMaclawAppApprovalInstanceToDataSrv, OpenSkillRunArtifact, RecordMaclawAppRunEvidenceForSkill, RevealSkillRunArtifact, RunNLSkillAsync, SaveMaclawAppDefinitionForSkill, SearchMixedSkills, ShowItemInFolder, StageSkillAppInputFile, UploadNLSkillToMarket } from '../../../wailsjs/go/main/App';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import './AppsPage.css';

type AppKind = 'enterprise_approval_app' | 'enterprise_normal_app' | 'tool_app' | 'automation_app';
type StudioTab = 'create' | 'manage' | 'market' | 'publish';
type AppMoveTarget = -1 | 1 | 'top' | 'bottom';
type AppOperation = 'approval_status' | 'run_history';
type ApprovalLaneFilter = 'my_requests' | 'pending_my_approval' | 'handled' | 'attention' | 'all';

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
};

type AppIconName = 'receipt' | 'wallet' | 'invoice' | 'warehouse' | 'inventory' | 'customer' | 'users' | 'contract' | 'pdf' | 'shield' | 'sheet' | 'chart' | 'dashboard' | 'database' | 'eraser' | 'truck' | 'calendar' | 'web' | 'sync' | 'bot';

type AppsPageProps = {
    lang?: string;
};
type AppSkillDependency = {
    id: string;
    version?: string;
    kind?: 'runtime_skill' | 'workflow_skill' | 'ui_component_skill' | 'connector_skill' | 'policy_skill';
    required?: boolean;
    source?: 'local' | 'hub' | 'market' | 'builtin';
    capabilities?: string[];
};
type StudioLayoutTemplate = 'classic_split' | 'left_nav' | 'document_workspace' | 'dashboard';
type StudioLayoutDensity = 'comfortable' | 'compact' | 'spacious';
type StudioPrimaryRegion = 'left' | 'center' | 'right';
type StudioOutputRegion = 'right' | 'bottom' | 'modal';
type RuntimeWorkspaceLayout = {
    template: StudioLayoutTemplate;
    density: StudioLayoutDensity;
    primaryRegion: StudioPrimaryRegion;
    outputRegion: StudioOutputRegion;
};

type BackendAppInstallDependency = {
    id: string;
    version?: string;
    kind?: string;
    required?: boolean;
    source?: string;
    app_ids?: string[];
    installed?: boolean;
    installed_name?: string;
    installed_dir?: string;
    installed_status?: string;
    health?: 'ready' | 'missing' | 'disabled' | 'needs_setup' | 'unknown' | string;
    action?: 'skip' | 'installed' | 'blocked' | 'failed' | 'optional_missing' | 'optional_unhealthy' | string;
    message?: string;
};

type BackendAppInstallPlan = {
    schema?: string;
    apps?: Array<{ id: string; name?: string; kind?: string; schema?: string }>;
    dependencies?: BackendAppInstallDependency[];
    has_missing_required?: boolean;
    has_blocking_dependency?: boolean;
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
    source?: string;
    installed_at?: string;
    app_count?: number;
    apps?: Array<{ id?: string; name?: string; kind?: string; schema?: string }>;
    dependencies?: BackendAppInstallDependency[];
    has_missing_required?: boolean;
    has_blocking_dependency?: boolean;
    datasrv_registration?: BackendAppDataSrvRegistration;
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
    workflow?: AppWorkflowMapping;
    mis?: {
        approvalBindings?: AppApprovalBinding[];
    };
    datasrv?: {
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
    at: string;
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
};

type ApprovalInstanceEventView = {
    at: string;
    node?: string;
    actor?: string;
    decision?: string;
    message?: string;
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
    approvalID?: string;
    title: string;
    lane: 'my_requests' | 'pending_my_approval' | 'handled' | 'attention';
    status: 'draft' | 'pending' | 'approved' | 'rejected' | 'attention';
    currentNode: string;
    owner: string;
    approver: string;
    updatedAt: string;
    result: string;
    workflowSkillID?: string;
    workflowDecisionID?: string;
    businessStatus?: string;
    resultStatus?: string;
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
    app_name?: string;
    blueprint_id?: string;
    dataset_id?: string;
    object_role?: string;
    instance_id?: string;
    title?: string;
    lane?: string;
    status?: string;
    current_node?: string;
    owner?: string;
    applicant?: string;
    approver?: string;
    updated_at?: string;
    result?: string;
    workflow_skill_id?: string;
    workflow_decision_id?: string;
    approval_event?: string;
    approval_object_role?: string;
    business_status?: string;
    result_status?: string;
    business_entity?: string;
    business_action?: string;
    business_note?: string;
    created_at?: string;
    events?: Array<{ at?: string; node?: string; actor?: string; decision?: string; message?: string; action?: string; note?: string }>;
    record_id?: string;
    approval_id?: string;
    record_approval_id?: string;
    detail_url?: string;
    result_payload?: Record<string, unknown>;
    outputs?: ApprovalInstanceOutputView[];
    artifacts?: ApprovalInstanceArtifactView[];
};

type ApprovalRunContext = {
    instance: BackendApprovalInstance;
};

type ApprovalWorkflowCompletion = {
    status: 'approved' | 'rejected' | 'attention';
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
    eventAction: string;
};

type AppLayoutState = {
    orderedIds?: string[];
    pinnedIds?: string[];
    hiddenIds?: string[];
    editedApps?: AppEntry[];
    customApps?: AppEntry[];
    recentUsedAtById?: Record<string, string>;
};

type AppPublishStatus = 'submitted' | 'review_failed' | 'approved' | 'published' | 'deprecated' | 'revoked';

type AppReviewIssue = {
    path?: string;
    severity?: string;
    message: string;
    suggestion?: string;
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
    eventCount: number;
    lastEventAt: string;
    message: string;
};

type MISDataConfig = {
    enabled?: boolean;
    endpoint?: string;
    token?: string;
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
    icon?: string;
    custom_icon_data_url?: string;
    customIconDataUrl?: string;
    input_mode?: 'file' | 'form' | 'mixed';
    multiple_files?: boolean;
    output_modes?: string[];
    fields?: SkillAppField[];
    app_definition_file?: string;
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
const publishSubmissionStorageKey = 'maclaw:apps-publish-submissions:v1';
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
        approvalStatusHint: '\u7edf\u4e00\u67e5\u770b\u6240\u6709\u5ba1\u6279\u5b9e\u4f8b',
        runHistoryOps: '\u8fd0\u884c\u8bb0\u5f55',
        runHistoryOpsHint: '\u67e5\u770b\u5e94\u7528\u6267\u884c\u5386\u53f2',
        all: '\u5168\u90e8\u5e94\u7528',
        otherApps: '\u5176\u4ed6\u5e94\u7528',
        pinned: '\u5e38\u7528\u5e94\u7528',
        recent: '\u6700\u8fd1\u4f7f\u7528',
        searchResults: '\u641c\u7d22\u7ed3\u679c',
        recentUsed: '\u6700\u8fd1\u4f7f\u7528',
        neverUsed: '\u5c1a\u672a\u4f7f\u7528',
        appStatus: '\u72b6\u6001',
        appAvailable: '\u53ef\u7528',
        appRunning: '\u8fd0\u884c\u4e2d',
        apps: '\u5e94\u7528',
        appStudio: '\u5e94\u7528\u7a0b\u5e8f\u5de5\u4f5c\u5ba4',
        studioSubtitle: '\u521b\u5efa\u3001\u7ba1\u7406\u3001\u4ece\u4f01\u4e1a\u80fd\u529b\u5e02\u573a\u6dfb\u52a0\u5e94\u7528\u3002',
        createTab: '\u521b\u5efa\u5e94\u7528',
        promptDraft: '\u7528\u5bf9\u8bdd\u751f\u6210\u8349\u7a3f',
        generateDraft: '\u751f\u6210\u8349\u7a3f',
        draftPromptPlaceholder: '\u4f8b\uff1a\u505a\u4e00\u4e2a\u5408\u540c\u5f52\u6863\u5e94\u7528\uff0c\u4e0a\u4f20 Word/PDF\uff0c\u8f93\u51fa\u5f52\u6863\u7f16\u53f7\u548c\u5ba1\u6838\u7ed3\u679c',
        manageTab: '\u5e94\u7528\u7ba1\u7406',
        marketTab: '\u4ece\u5e02\u573a\u6dfb\u52a0',
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
        refreshQueue: '\u5237\u65b0',
        refreshingQueue: '\u5237\u65b0\u4e2d',
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
        runSteps: '\u6267\u884c\u6b65\u9aa4',
        runArtifacts: '\u8f93\u51fa\u4ea7\u7269',
        runtimeInput: '\u8f93\u5165',
        runtimeStatus: '\u6267\u884c\u72b6\u6001',
        runtimeOutput: '\u8f93\u51fa',
        outputText: '\u6587\u672c\u7ed3\u679c',
        outputContent: '\u8f93\u51fa\u5185\u5bb9',
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
        runHistoryManager: '\u8fd0\u884c\u8bb0\u5f55',
        runHistoryManagerHint: '\u805a\u5408\u5c55\u793a\u672c\u673a\u4fdd\u5b58\u7684\u5e94\u7528\u6267\u884c\u5386\u53f2\u3002',
        runHistoryAllApps: '\u5168\u90e8\u5e94\u7528',
        noGlobalRunHistory: '\u6682\u65e0\u5e94\u7528\u8fd0\u884c\u8bb0\u5f55',
        approvalWorkspace: '\u5ba1\u6279\u5b9e\u4f8b',
        approvalManagerTitle: '\u5ba1\u6279\u5b9e\u4f8b\u7ba1\u7406',
        approvalManagerHint: '\u6240\u6709\u5ba1\u6279\u578b\u5e94\u7528\u7684\u7533\u8bf7\u3001\u5f85\u529e\u548c\u7ed3\u679c\u5728\u8fd9\u91cc\u7edf\u4e00\u7ba1\u7406\u3002',
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
        attentionApprovals: '\u9700\u5173\u6ce8',
        allApprovalInstances: '\u5168\u90e8',
        approvalInstanceData: '\u5b9e\u4f8b\u6570\u636e',
        currentApprovalNode: '\u5f53\u524d\u8282\u70b9',
        approvalTimeline: '\u5ba1\u6279\u8f68\u8ff9',
        approvalActions: '\u5ba1\u6279\u64cd\u4f5c',
        approve: '\u901a\u8fc7',
        reject: '\u9a73\u56de',
        markAttention: '\u6807\u8bb0\u5173\u6ce8',
        approvalResult: '\u5ba1\u6279\u7ed3\u679c',
        approvalResultPackage: '\u7ed3\u679c\u5305',
        approvalOutputData: '\u7ed3\u6784\u5316\u6570\u636e',
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
        clearFilterToSort: '\u6e05\u7a7a\u7b5b\u9009\u540e\u8c03\u6574\u987a\u5e8f',
        hidden: '\u9690\u85cf',
        remove: '\u79fb\u9664',
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
        manifest: 'Manifest',
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
        marketApps: '\u5e94\u7528\u5e02\u573a',
        marketAdvancedImport: '\u5bfc\u5165\u5e94\u7528\u5305',
        marketAdvancedImportHint: '\u7c98\u8d34\u5df2\u5ba1\u6838\u7684\u5e94\u7528\u5305 JSON\uff0c\u9002\u5408\u7ba1\u7406\u5458\u6216\u79c1\u6709\u5e94\u7528\u3002',
        marketSource: '\u6765\u6e90',
        marketAddableCount: '\u53ef\u6dfb\u52a0',
        marketUpgradeableCount: '\u53ef\u5347\u7ea7',
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
        dependencyVerificationReady: '\u4f9d\u8d56\u9a8c\u8bc1\u5df2\u5b8c\u6210',
        dependencyVerificationBlocked: '\u4f9d\u8d56\u9a8c\u8bc1\u53d1\u73b0\u963b\u65ad\u9879',
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
        installRecords: '\u6700\u8fd1\u5b89\u88c5',
        installRecordsHint: '\u672c\u673a\u5e94\u7528\u5305\u5b89\u88c5\u548c Skill \u4f9d\u8d56\u5ba1\u8ba1',
        datasrvRegistrationReady: '\u0044\u0061\u0074\u0061\u0053\u0072\u0076 \u7ed1\u5b9a\u5df2\u6ce8\u518c',
        datasrvRegistrationSkipped: '\u0044\u0061\u0074\u0061\u0053\u0072\u0076 \u7ed1\u5b9a\u672a\u6ce8\u518c',
        datasrvRegistrationFailed: '\u0044\u0061\u0074\u0061\u0053\u0072\u0076 \u7ed1\u5b9a\u6ce8\u518c\u5931\u8d25',
        installRecordsLoading: '\u6b63\u5728\u8bfb\u53d6\u5b89\u88c5\u8bb0\u5f55',
        installRecordsError: '\u5b89\u88c5\u8bb0\u5f55\u8bfb\u53d6\u5931\u8d25',
        noInstallRecords: '\u6682\u65e0\u5e94\u7528\u5b89\u88c5\u8bb0\u5f55',
        refreshInstallRecords: '\u5237\u65b0\u8bb0\u5f55',
        packageSha: '\u5305\u6307\u7eb9',
        installedAt: '\u5b89\u88c5\u65f6\u95f4',
        missingDependencyCount: '\u963b\u65ad\u4f9d\u8d56',
        skippedCount: '\u5df2\u8df3\u8fc7',
        installError: '\u5e94\u7528\u5305\u65e0\u6548',
        parseError: 'JSON \u89e3\u6790\u5931\u8d25',
        schemaError: '\u672a\u8bc6\u522b\u5e94\u7528\u5305\u683c\u5f0f',
        close: '\u5173\u95ed',
    },
    en: {
        search: 'Search apps',
        clearSearch: 'Clear search',
        resetFilter: 'Reset filters',
        create: 'Create',
        category: 'Category',
        operations: 'Actions',
        approvalStatus: 'Approval status',
        approvalStatusHint: 'Review every approval instance in one place',
        runHistoryOps: 'Run history',
        runHistoryOpsHint: 'Review app execution history',
        all: 'All apps',
        otherApps: 'Other apps',
        pinned: 'Pinned apps',
        recent: 'Recent',
        searchResults: 'Search results',
        recentUsed: 'Recent',
        neverUsed: 'Never used',
        appStatus: 'Status',
        appAvailable: 'Available',
        appRunning: 'Running',
        apps: 'Apps',
        appStudio: 'App Studio',
        studioSubtitle: 'Create, manage, and add apps from the capability market.',
        createTab: 'Create app',
        promptDraft: 'Generate draft from chat',
        generateDraft: 'Generate draft',
        draftPromptPlaceholder: 'Example: build a contract filing app, upload Word/PDF, output archive number and review result',
        manageTab: 'Manage apps',
        marketTab: 'Add from market',
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
        refreshQueue: 'Refresh',
        refreshingQueue: 'Refreshing',
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
        runSteps: 'Run steps',
        runArtifacts: 'Output artifacts',
        runtimeInput: 'Input',
        runtimeStatus: 'Run status',
        runtimeOutput: 'Output',
        outputText: 'Text result',
        outputContent: 'Output content',
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
        runHistoryManager: 'Run history',
        runHistoryManagerHint: 'Aggregated execution history saved on this device.',
        runHistoryAllApps: 'All apps',
        noGlobalRunHistory: 'No app run history yet',
        approvalWorkspace: 'Approval instances',
        approvalManagerTitle: 'Approval instance management',
        approvalManagerHint: 'Requests, pending work, and outcomes from every approval app are managed here.',
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
        attentionApprovals: 'Needs attention',
        allApprovalInstances: 'All',
        approvalInstanceData: 'Instance data',
        currentApprovalNode: 'Current node',
        approvalTimeline: 'Approval timeline',
        approvalActions: 'Approval actions',
        approve: 'Approve',
        reject: 'Reject',
        markAttention: 'Mark attention',
        approvalResult: 'Approval result',
        approvalResultPackage: 'Result package',
        approvalOutputData: 'Structured data',
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
        clearFilterToSort: 'Clear filters before sorting',
        hidden: 'Hide',
        remove: 'Remove',
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
        marketApps: 'App market',
        marketAdvancedImport: 'Import app package',
        marketAdvancedImportHint: 'Paste reviewed app package JSON for admin installs or private apps.',
        marketSource: 'Source',
        marketAddableCount: 'Addable',
        marketUpgradeableCount: 'Upgradable',
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
        dependencyVerificationReady: 'Dependency verification complete',
        dependencyVerificationBlocked: 'Dependency verification found blocking items',
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
        installRecords: 'Recent installs',
        installRecordsHint: 'Local app package installs and Skill dependency audit',
        datasrvRegistrationReady: 'DataSrv bindings registered',
        datasrvRegistrationSkipped: 'DataSrv bindings not registered',
        datasrvRegistrationFailed: 'DataSrv binding registration failed',
        installRecordsLoading: 'Reading install records',
        installRecordsError: 'Failed to read install records',
        noInstallRecords: 'No app install records yet',
        refreshInstallRecords: 'Refresh records',
        packageSha: 'Package SHA',
        installedAt: 'Installed at',
        missingDependencyCount: 'Blocking deps',
        skippedCount: 'Skipped',
        installError: 'Invalid app package',
        parseError: 'JSON parse failed',
        schemaError: 'Unrecognized app package format',
        close: 'Close',
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

const initialApps: AppEntry[] = [
    { id: 'expense', name: '\u62a5\u9500\u7533\u8bf7', description: '\u4ece\u53d1\u7968\u3001\u884c\u7a0b\u548c\u653f\u7b56\u81ea\u52a8\u751f\u6210\u62a5\u9500\u5355\u3002', category: 'OA', kind: 'enterprise_approval_app', icon: 'receipt', accent: '#2f5f98', pinned: true, source: 'datasrv', manifest: makeEnterpriseManifest('enterprise_approval_app', 'finance', 'finance.expense_upsert', 'finance.expense_review', 'finance.expense_by_status', 'finance.overview', 'expense-approval-app', [{ id: 'expense-approval-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow', 'expense.policy'] }], { event: 'expense.submitted', objectRole: 'expense_report' }) },
    { id: 'purchase-inbound', name: '\u91c7\u8d2d\u5165\u5e93', description: '\u5bf9\u63a5\u91c7\u8d2d\u5355\u3001\u5165\u5e93\u5355\u548c\u5e93\u5b58\u53d8\u52a8\u3002', category: '\u8fdb\u9500\u5b58', kind: 'enterprise_normal_app', icon: 'warehouse', accent: '#657a42', pinned: true, source: 'datasrv', manifest: makeDataSrvManifest('procurement', 'procurement.purchase_order_upsert', 'procurement.purchase_order_review', 'procurement.purchase_by_status', 'procurement.overview') },
    { id: 'inventory-count', name: '\u5e93\u5b58\u76d8\u70b9', description: '\u6309\u4ed3\u5e93\u6216\u7269\u6599\u751f\u6210\u76d8\u70b9\u8868\u5e76\u56de\u5199\u5dee\u5f02\u3002', category: '\u8fdb\u9500\u5b58', kind: 'enterprise_normal_app', icon: 'inventory', accent: '#6b7280', pinned: true, source: 'datasrv', manifest: makeDataSrvManifest('inventory', 'inventory.stock_update', 'inventory.stock_position', 'inventory.stock_by_warehouse', 'inventory.overview') },
    { id: 'customer-profile', name: '\u5ba2\u6237\u5efa\u6863', description: '\u4ece\u540d\u7247\u3001\u90ae\u4ef6\u6216\u804a\u5929\u8bb0\u5f55\u6574\u7406\u5ba2\u6237\u8d44\u6599\u3002', category: 'CRM', kind: 'enterprise_normal_app', icon: 'customer', accent: '#8a5a44', pinned: true, source: 'datasrv', manifest: makeDataSrvManifest('sales', 'sales.customer_upsert', 'sales.customer_directory', 'sales.customer_activity', 'sales.overview') },
    { id: 'contract-review', name: '\u5408\u540c\u5ba1\u67e5', description: '\u68c0\u67e5\u5408\u540c\u6761\u6b3e\u3001\u98ce\u9669\u70b9\u548c\u4fee\u8ba2\u5efa\u8bae\u3002', category: '\u6cd5\u52a1', kind: 'tool_app', icon: 'contract', accent: '#7c3f58', pinned: true, source: 'skill', manifest: makeSkillManifest('contract-review', 'mixed') },
    { id: 'pdf-word', name: 'PDF \u8f6c Word', description: '\u4e0a\u4f20 PDF\uff0c\u4fdd\u7559\u7248\u5f0f\u8f93\u51fa Word \u6587\u6863\u3002', category: '\u6587\u6863\u5904\u7406', kind: 'tool_app', icon: 'pdf', accent: '#b45309', pinned: true, source: 'skill', manifest: makeSkillManifest('pdf-word', 'file') },
    { id: 'doc-redact', name: '\u6587\u6863\u8131\u654f', description: '\u8bc6\u522b\u8eab\u4efd\u8bc1\u3001\u7535\u8bdd\u3001\u5730\u5740\u7b49\u654f\u611f\u4fe1\u606f\u5e76\u8f93\u51fa\u65b0\u6587\u6863\u3002', category: '\u6587\u6863\u5904\u7406', kind: 'tool_app', icon: 'shield', accent: '#28705f', source: 'skill', manifest: makeSkillManifest('doc-redact', 'file') },
    { id: 'sheet-analysis', name: '\u8868\u683c\u5206\u6790', description: '\u4e0a\u4f20 Excel\uff0c\u751f\u6210\u5206\u6790\u7ed3\u8bba\u3001\u56fe\u8868\u548c\u6e05\u6d17\u540e\u8868\u683c\u3002', category: '\u6570\u636e\u5206\u6790', kind: 'tool_app', icon: 'sheet', accent: '#3f6f4f', source: 'skill', manifest: makeSkillManifest('sheet-analysis', 'file', ['xlsx', 'pdf']) },
    { id: 'web-collect', name: '\u7f51\u9875\u91c7\u96c6', description: '\u56fa\u5b9a\u7f51\u7ad9\u7684\u5468\u671f\u91c7\u96c6\u548c\u7ed3\u679c\u6821\u9a8c\u3002', category: '\u6570\u636e\u91c7\u96c6', kind: 'automation_app', icon: 'web', accent: '#5b5ea6', source: 'market', manifest: makeAutomationManifest() },
    { id: 'data-sync', name: '\u6570\u636e\u540c\u6b65', description: '\u5728 DataSrv \u3001\u6587\u4ef6\u548c\u4e1a\u52a1\u7cfb\u7edf\u95f4\u8fd0\u884c\u53ef\u76d1\u63a7\u540c\u6b65\u3002', category: '\u6570\u636e\u96c6\u6210', kind: 'automation_app', icon: 'sync', accent: '#4b6572', source: 'market', manifest: makeAutomationManifest() },
];

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

function studioRegionsForLayout(kind: AppKind, template: StudioLayoutTemplate, primaryRegion: StudioPrimaryRegion, outputRegion: StudioOutputRegion) {
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

function applyStudioWorkspaceLayout(manifest: AppManifestBinding, kind: AppKind, options: { template: StudioLayoutTemplate; density: StudioLayoutDensity; primaryRegion: StudioPrimaryRegion; outputRegion: StudioOutputRegion }): AppManifestBinding {
    const baseUI = manifest.ui || defaultWorkspaceLayoutForKind(kind);
    const entry = baseUI.entry || workspaceEntryForKind(kind);
    const currentLayouts = baseUI.layouts || {};
    const currentLayout = currentLayouts[entry] || {};
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
                    regions: studioRegionsForLayout(kind, options.template, options.primaryRegion, options.outputRegion),
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
    return {
        template: kind === 'tool_app' ? 'document_workspace' : 'classic_split',
        density: 'comfortable',
        primaryRegion: 'left',
        outputRegion: kind === 'tool_app' ? 'right' : 'bottom',
    };
}

function normalizeRuntimeWorkspaceLayout(raw: any, kind: AppKind): RuntimeWorkspaceLayout {
    const fallback = defaultRuntimeWorkspaceLayout(kind);
    return {
        template: ['classic_split', 'left_nav', 'document_workspace', 'dashboard'].includes(String(raw?.template)) ? raw.template as StudioLayoutTemplate : fallback.template,
        density: ['compact', 'comfortable', 'spacious'].includes(String(raw?.density)) ? raw.density as StudioLayoutDensity : fallback.density,
        primaryRegion: ['left', 'center', 'right'].includes(String(raw?.primaryRegion)) ? raw.primaryRegion as StudioPrimaryRegion : fallback.primaryRegion,
        outputRegion: ['right', 'bottom', 'modal'].includes(String(raw?.outputRegion)) ? raw.outputRegion as StudioOutputRegion : fallback.outputRegion,
    };
}

function runtimeWorkspaceLayoutForApp(app: AppEntry): RuntimeWorkspaceLayout {
    const entry = app.manifest?.ui?.entry || workspaceEntryForKind(app.kind);
    const layout = app.manifest?.ui?.layouts?.[entry] || {};
    return normalizeRuntimeWorkspaceLayout(layout, app.kind);
}

function runtimeWorkspaceOrder(layout: RuntimeWorkspaceLayout) {
    const outputEarly = layout.outputRegion === 'right' || layout.outputRegion === 'modal';
    return {
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
    const updateLayout = (patch: Partial<RuntimeWorkspaceLayout>) => onChange({ ...value, ...patch });
    const regions = studioRegionsForLayout(kind, value.template, value.primaryRegion, value.outputRegion);
    const regionsForPlacement = (placement: StudioPrimaryRegion | 'bottom') => regions.filter((region) => region.placement === placement || (placement === 'right' && region.placement === 'modal'));
    const renderRegionPill = (region: { id: string; role: string; placement: string }) => (
        <span className="apps-layout-designer__region" data-role={region.role} key={region.id}>
            <strong>{studioLayoutRegionLabel(kind, region.id, lang)}</strong>
            <small>{studioLayoutRoleLabel(region.role, lang)}</small>
        </span>
    );
    const renderSlot = (slot: typeof studioLayoutSlotIds[number]) => {
        const slotRegions = regionsForPlacement(slot);
        const isPrimary = value.primaryRegion === slot;
        const hasOutput = value.outputRegion === slot || (slot === 'right' && value.outputRegion === 'modal');
        return (
            <button
                className="apps-layout-designer__slot"
                data-slot={slot}
                data-primary={isPrimary ? 'true' : undefined}
                data-output={hasOutput ? value.outputRegion : undefined}
                data-testid={`${testIdPrefix}-layout-slot-${slot}`}
                type="button"
                aria-pressed={isPrimary}
                onClick={() => updateLayout({ primaryRegion: slot })}
            >
                <span className="apps-layout-designer__slot-title">
                    {studioLayoutOptionLabel(studioPrimaryRegionOptions, slot, lang)}
                    {isPrimary && <em>{zh ? '\u4e3b\u64cd\u4f5c' : 'Primary'}</em>}
                </span>
                <span className="apps-layout-designer__slot-body">
                    {slotRegions.length ? slotRegions.map(renderRegionPill) : <span className="apps-layout-designer__empty-slot">{zh ? '\u53ef\u653e\u7f6e\u533a\u57df' : 'Available region'}</span>}
                </span>
            </button>
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
                    <div className="apps-layout-designer__canvas" data-testid={`${testIdPrefix}-layout-canvas`} data-template={value.template} data-density={value.density}>
                        {studioLayoutSlotIds.map(renderSlot)}
                        <button
                            className="apps-layout-designer__slot apps-layout-designer__slot--bottom"
                            data-slot="bottom"
                            data-output={value.outputRegion === 'bottom' ? 'bottom' : undefined}
                            data-testid={`${testIdPrefix}-layout-slot-bottom`}
                            type="button"
                            aria-pressed={value.outputRegion === 'bottom'}
                            onClick={() => updateLayout({ outputRegion: 'bottom' })}
                        >
                            <span className="apps-layout-designer__slot-title">
                                {zh ? '\u5e95\u90e8' : 'Bottom'}
                                {value.outputRegion === 'bottom' && <em>{zh ? '\u8f93\u51fa' : 'Output'}</em>}
                            </span>
                            <span className="apps-layout-designer__slot-body">
                                {regionsForPlacement('bottom').length ? regionsForPlacement('bottom').map(renderRegionPill) : <span className="apps-layout-designer__empty-slot">{zh ? '\u7ed3\u679c\u533a\u6216\u65e5\u5fd7\u533a' : 'Results or log lane'}</span>}
                            </span>
                        </button>
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
    const appSkill = app.manifest?.appSkill?.id ? [{ id: app.manifest.appSkill.id, version: app.manifest.appSkill.version, kind: 'runtime_skill' as const, required: true, source: (app.manifest.appSkill.source || 'local') as AppSkillDependency['source'] }] : [];
    const boundSkill = app.manifest?.skill?.id ? [{ id: app.manifest.skill.id, kind: 'runtime_skill' as const, required: true, source: 'hub' as const }] : [];
    const approvalWorkflowSkills = (app.manifest?.mis?.approvalBindings || []).map((binding) => ({
        id: binding.workflowSkillId,
        version: binding.workflowVersion,
        kind: 'workflow_skill' as const,
        required: true,
        source: 'hub' as const,
        capabilities: ['approval.workflow'],
    })).filter((dep) => dep.id);
    return Array.from(new Map([...appSkill, ...boundSkill, ...deps, ...approvalWorkflowSkills].map((dep) => [dep.id, dep])).values());
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
    const recentUsedAtById = layout.recentUsedAtById || {};
    const editedApps = (layout.editedApps || []).map((app) => normalizeStoredAppEntry(app)).filter((app): app is AppEntry => !!app);
    const customApps = (layout.customApps || []).map((app) => normalizeStoredAppEntry(app, true)).filter((app): app is AppEntry => !!app);
    const editedById = new Map(editedApps.map((app) => [app.id, app]));
    const baseApps = apps.map((app) => ({ ...app, ...editedById.get(app.id), id: app.id }));
    const byId = new Map([...baseApps, ...customApps].filter((app) => !hidden.has(app.id)).map((app) => [app.id, { ...app, pinned: layout.pinnedIds ? pinned.has(app.id) : app.pinned, recentUsedAt: recentUsedAtById[app.id] || app.recentUsedAt }]));
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
    const source = app.source === 'builtin' || app.source === 'skill' || app.source === 'datasrv' || app.source === 'market' || app.source === 'local'
        ? app.source
        : undefined;
    const migratedSource = custom && (source === undefined || (source === 'market' && String(app.id).startsWith('local-app-'))) ? 'local' : source || 'local';
    return {
        ...app,
        id: String(app.id),
        name: String(app.name),
        description: String(app.description || ''),
        category: String(app.category || '\u672a\u5206\u7c7b'),
        kind: normalizeAppKind(app.kind),
        icon: normalizeSkillAppIcon(app.icon),
        customIconDataUrl: normalizeCustomIconDataUrl((app as any).customIconDataUrl),
        accent: String(app.accent || defaultAccentForKind(normalizeAppKind(app.kind))),
        version: normalizeAppVersion(app.version),
        source: migratedSource,
        importedRunEvidence: normalizeImportedRunEvidence((app as any).importedRunEvidence),
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
    role_bindings?: Array<Record<string, any>>;
    roleBindings?: Array<Record<string, any>>;
    metadata?: Record<string, any>;
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
    const workflowDependencies: AppSkillDependency[] = workflowIDs.map((id) => ({ id, kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }));
    const appSkillID = String(metadata.app_skill_id || '').trim();
    const appSkillVersion = String(metadata.app_skill_version || '').trim();
    const importedRunEvidence = dataSrvInstalledRunEvidence(metadata, appID);
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
        manifest: {
            schema: 'maclaw.app.v1',
            installUnit: 'enterprise_app_pack',
            privateMarker: 'x_maclaw_apps',
            entryKind: kind,
            launchMode: defaultLaunchModeForKind(kind),
            datasrv: {
                domain,
                datasetID: String(primaryBinding.dataset_id || primaryBinding.datasetID || '').trim() || undefined,
                objectRole: String(primaryBinding.object_role || primaryBinding.objectRole || '').trim() || undefined,
                blueprintID: String(item.blueprint_id || item.blueprintID || '').trim() || undefined,
                templateID: String(primaryBinding.template_id || primaryBinding.templateID || '').trim() || undefined,
            },
            appSkill: appSkillID ? { id: appSkillID, version: appSkillVersion || undefined, source: 'hub' } : undefined,
            dependencies: dependencies.length || workflowDependencies.length ? { skills: [...dependencies, ...workflowDependencies] } : undefined,
            mis: kind === 'enterprise_approval_app' && workflowIDs.length > 0 ? { approvalBindings: [{ event: `${domain}.submitted`, workflowSkillId: workflowIDs[0], objectRole: String(primaryBinding.object_role || primaryBinding.objectRole || '').trim() || domain }] } : undefined,
            ui: dataSrvInstalledWorkspaceLayout(metadata, kind),
        },
    };
}

function normalizeImportedRunEvidence(raw: unknown): AppRunHistoryEntry | undefined {
    const value = raw && typeof raw === 'object' ? raw as Partial<AppRunHistoryEntry> : undefined;
    const runID = String(value?.runID || '').trim();
    if (!runID) return undefined;
    const status = value?.status === 'error' || value?.status === 'cancelled' ? value.status : 'done';
    const at = String(value?.at || '').trim() || new Date().toISOString();
    return {
        runID,
        appID: String(value?.appID || '').trim(),
        status,
        definitionHash: String(value?.definitionHash || '').trim() || undefined,
        outputMode: String(value?.outputMode || '').trim() || 'imported',
        inputSummary: String(value?.inputSummary || '').trim() || 'Imported DataSrv test evidence',
        message: String(value?.message || '').trim() || 'Imported DataSrv test evidence',
        artifactName: String(value?.artifactName || '').trim() || undefined,
        artifactPath: String(value?.artifactPath || '').trim() || undefined,
        artifactURI: String(value?.artifactURI || '').trim() || undefined,
        artifactDownloadState: String(value?.artifactDownloadState || '').trim() || undefined,
        artifacts: Array.isArray(value?.artifacts) ? value.artifacts : undefined,
        resultPayload: value?.resultPayload && typeof value.resultPayload === 'object' ? value.resultPayload as Record<string, unknown> : undefined,
        outputs: Array.isArray(value?.outputs) ? value.outputs : undefined,
        at,
    };
}

function dataSrvInstalledRunEvidence(metadata: Record<string, any>, appID: string): AppRunHistoryEntry | undefined {
    const evidence = metadata.test_evidence && typeof metadata.test_evidence === 'object' ? metadata.test_evidence : {};
    const runID = String(evidence.run_id || evidence.runId || metadata.test_evidence_run_id || '').trim();
    if (!runID) return undefined;
    const artifactName = String(evidence.artifact_name || evidence.artifactName || metadata.test_evidence_artifact_name || '').trim();
    const artifactPresent = !!(evidence.artifact_present ?? evidence.artifactPresent ?? metadata.test_evidence_artifact_present);
    const resultPayload = (evidence.result_payload || evidence.resultPayload || metadata.test_evidence_result_payload) as Record<string, unknown> | undefined;
    const outputCount = Number(evidence.output_count ?? evidence.outputCount ?? metadata.test_evidence_output_count ?? 0) || 0;
    const primaryResult = String(evidence.primary_result || evidence.primaryResult || metadata.test_evidence_primary_result || '').trim();
    return normalizeImportedRunEvidence({
        runID,
        appID,
        status: 'done',
        definitionHash: String(evidence.definition_fingerprint || evidence.definitionFingerprint || evidence.definition_hash || evidence.definitionHash || metadata.test_evidence_definition_fingerprint || '').trim() || undefined,
        outputMode: primaryResult || 'imported',
        inputSummary: 'Imported DataSrv test evidence',
        message: primaryResult || 'Imported DataSrv test evidence',
        artifactName: artifactPresent ? artifactName || undefined : undefined,
        artifacts: artifactPresent && artifactName ? [{ name: artifactName, status: 'ready' }] : undefined,
        resultPayload: resultPayload && typeof resultPayload === 'object' ? resultPayload : undefined,
        outputs: outputCount > 0 ? [{ type: 'summary', title: 'Imported outputs', text: String(outputCount) }] : undefined,
        at: String(evidence.verified_at || evidence.verifiedAt || metadata.test_evidence_verified_at || '').trim() || new Date().toISOString(),
    });
}

function dataSrvInstalledWorkspaceLayout(metadata: Record<string, any>, kind: AppKind): AppWorkspaceLayout {
    const base = defaultWorkspaceLayoutForKind(kind);
    const raw = metadata.workspace_layout && typeof metadata.workspace_layout === 'object' ? metadata.workspace_layout : metadata.workspaceLayout && typeof metadata.workspaceLayout === 'object' ? metadata.workspaceLayout : {};
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
        ...(String(raw.primary_region || raw.primaryRegion || '').trim() ? { primaryRegion: String(raw.primary_region || raw.primaryRegion).trim() } : {}),
        ...(String(raw.output_region || raw.outputRegion || '').trim() ? { outputRegion: String(raw.output_region || raw.outputRegion).trim() } : {}),
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

function buildSkillFieldPayload(fields: SkillAppField[], values: Record<string, string | boolean>) {
    return fields.reduce<Record<string, string | boolean>>((payload, field) => {
        payload[field.name] = values[field.name] ?? field.default ?? (field.type === 'boolean' ? false : '');
        return payload;
    }, {});
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

function normalizeSkillRunLifecycle(status?: string): 'running' | 'done' | 'error' | 'cancelled' {
    const value = String(status || '').trim().toLowerCase();
    if (['success', 'succeeded', 'completed', 'complete', 'done'].includes(value)) return 'done';
    if (['failed', 'failure', 'error', 'timeout'].includes(value)) return 'error';
    if (['cancelled', 'canceled'].includes(value)) return 'cancelled';
    return 'running';
}

function skillRunErrorMessage(status?: SkillRunStatusView | null) {
    return String(status?.error || status?.summary?.last_error_snippet || '').trim();
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
    for (const object of objects) {
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
    const items = [...(Array.isArray(status?.artifacts) ? status?.artifacts || [] : []), ...(Array.isArray(status?.summary?.artifacts) ? status?.summary?.artifacts || [] : []), ...skillRunOutputBlocks(status).map((block) => block.artifact), skillRunPrimaryArtifact(status)];
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
    return normalizeApprovalOutputs(skillRunOutputBlocks(status));
}

function appRunResultPayloadFromStatus(status?: SkillRunStatusView | null): Record<string, unknown> | undefined {
    const payload = approvalWorkflowResultPayloadFromObjects(skillRunApprovalObjects(status), status);
    if (payload) return payload;
    const snippet = skillRunOutputSuffix(status).replace(/^ 路 /, '');
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
function normalizeApprovalWorkflowDecision(value: string, lifecycle: 'done' | 'error' | 'cancelled'): ApprovalWorkflowCompletion['status'] {
    if (lifecycle !== 'done') return 'attention';
    const text = value.trim().toLowerCase();
    if (!text) return 'approved';
    if (text.includes('not approved') || text.includes('reject') || text.includes('denied') || text.includes('deny') || text.includes('refuse') || text.includes('\u9a73\u56de') || text.includes('\u62d2\u7edd')) return 'rejected';
    if (text.includes('attention') || text.includes('needs_attention') || text.includes('needs review') || text.includes('review_only') || text.includes('warning') || text.includes('\u5173\u6ce8') || text.includes('\u67e5\u770b')) return 'attention';
    if (text.includes('approve') || text.includes('accepted') || text.includes('pass') || text.includes('success') || text.includes('complete') || text.includes('done') || text.includes('\u901a\u8fc7') || text.includes('\u540c\u610f') || text.includes('\u6279\u51c6')) return 'approved';
    return 'approved';
}

function approvalWorkflowResultFromSkillRunStatus(status: SkillRunStatusView | null, lifecycle: 'done' | 'error' | 'cancelled', lang?: string): ApprovalWorkflowCompletion {
    const zh = isZh(lang);
    const objects = skillRunApprovalObjects(status);
    const explicitDecision = firstSkillRunResultString(objects, ['approval_result', 'approvalResult', 'approval_status', 'approvalStatus', 'approval_decision', 'approvalDecision', 'decision']);
    const decision = normalizeApprovalWorkflowDecision(explicitDecision, lifecycle);
    const outputText = skillRunOutputSuffix(status).replace(/^ · /, '');
    const fallbackText = lifecycle === 'cancelled'
        ? (zh ? 'Skill \u5df2\u53d6\u6d88' : 'Skill cancelled')
        : lifecycle === 'error'
            ? (skillRunErrorMessage(status) || (zh ? '\u5de5\u4f5c\u6d41\u8fd0\u884c\u5f02\u5e38\uff0c\u9700\u5173\u6ce8' : 'Workflow failed and needs attention'))
            : decision === 'approved'
                ? (zh ? '\u5ba1\u6279\u5df2\u901a\u8fc7' : 'Approval approved')
                : decision === 'rejected'
                    ? (zh ? '\u5ba1\u6279\u5df2\u9a73\u56de' : 'Approval rejected')
                    : (zh ? '\u9700\u5173\u6ce8' : 'Needs attention');
    const resultText = firstSkillRunResultString(objects, ['message', 'result_message', 'resultMessage', 'approval_message', 'approvalMessage', 'content', 'text', 'result']) || outputText || fallbackText;
    const resultStatus = firstSkillRunResultString(objects, ['result_status', 'resultStatus']) || (lifecycle === 'cancelled' ? 'cancelled' : lifecycle === 'error' ? 'workflow_error' : decision);
    const businessStatus = firstSkillRunResultString(objects, ['business_status', 'businessStatus']) || resultStatus;
    const currentNode = firstSkillRunResultString(objects, ['current_node', 'currentNode', 'approval_node', 'approvalNode', 'node']) || (lifecycle === 'cancelled'
        ? (zh ? '\u5df2\u53d6\u6d88' : 'Cancelled')
        : lifecycle === 'error'
            ? (zh ? '\u8fd0\u884c\u5f02\u5e38' : 'Workflow error')
            : decision === 'attention'
                ? (zh ? '\u9700\u5173\u6ce8' : 'Needs attention')
                : (zh ? '\u5df2\u5b8c\u6210' : 'Completed'));
    const resultPayload = approvalWorkflowResultPayloadFromObjects(objects, status);
    const outputs = approvalWorkflowOutputsFromStatus(status);
    const artifacts = approvalWorkflowArtifactsFromStatus(status);
    return {
        status: decision,
        lane: decision === 'attention' ? 'attention' : 'handled',
        currentNode,
        resultText,
        businessStatus,
        resultStatus,
        detailURL: firstSkillRunResultString(objects, ['detail_url', 'detailURL', 'workflow_url', 'workflowURL', 'url']) || undefined,
        recordID: firstSkillRunResultString(objects, ['record_id', 'recordID', 'business_record_id', 'businessRecordID']) || approvalPayloadRecordID(resultPayload) || undefined,
        resultPayload,
        outputs,
        artifacts,
        eventAction: lifecycle === 'cancelled' ? 'workflow_cancelled' : lifecycle === 'error' ? 'workflow_failed' : 'workflow_completed',
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

function businessOperationResultKind(mode: string, response?: Record<string, unknown>): BusinessOperationResultKind {
    const status = String(response?.status || response?.state || '').trim().toLowerCase();
    if (['error', 'failed', 'failure', 'denied'].includes(status)) return 'error';
    if (mode === 'business_dashboard' || Array.isArray(response?.cards) || Array.isArray(response?.widgets) || Array.isArray(response?.charts)) return 'dashboard';
    if (mode === 'business_view' || mode === 'business_report' || Array.isArray(response?.rows) || Array.isArray(response?.records) || Array.isArray(response?.items) || Array.isArray(response?.results)) return 'table';
    if (response?.record_id || response?.record || response?.business_record || response?.businessRecord) return 'business_record';
    if (response?.receipt || response?.external_receipt || response?.externalReceipt) return 'external_receipt';
    if (response?.message || response?.summary || response?.result || response?.text) return 'text';
    return 'business_status';
}

function countBusinessOperationRecords(value: unknown): number {
    return businessOperationRows(value).length;
}

function businessOperationResponseSummary(response?: Record<string, unknown>): string {
    if (!response) return '';
    const direct = String(response.message || response.summary || response.result || '').trim();
    if (direct) return direct.slice(0, 180);
    const rows = ['rows', 'records', 'items', 'data', 'results']
        .map((key) => response[key])
        .find((value) => Array.isArray(value)) as unknown[] | undefined;
    if (rows?.length) {
        const first = rows[0];
        if (first && typeof first === 'object') {
            return Object.entries(first as Record<string, unknown>)
                .slice(0, 3)
                .map(([key, value]) => key + ': ' + String(value))
                .join(' / ')
                .slice(0, 180);
        }
        return String(first).slice(0, 180);
    }
    return JSON.stringify(response).slice(0, 180);
}

function buildBusinessOperationResult(result: Record<string, unknown> | undefined, actionRole: string, text: typeof labels.zh): BusinessOperationResultView {
    const response = result?.response && typeof result.response === 'object' ? result.response as Record<string, unknown> : undefined;
    const mode = String(result?.mode || actionRole || 'DataSrv').trim();
    const target = String(result?.target || result?.endpoint || actionRole || mode).trim();
    const status = String(result?.result_status || response?.status || response?.state || 'done').trim();
    const rows = businessOperationRows(response);
    const columns = businessOperationColumns(rows);
    const recordCount = rows.length;
    const kind = businessOperationResultKind(mode, response);
    const summary = businessOperationResponseSummary(response);
    const message = summary || text.runCompleted + ': ' + mode + ' / ' + status;
    return { mode, target, status, kind, message, recordCount, columns, rows, response };
}

async function openSkillRunArtifactFromUI(runID: string, artifactRef: string, artifactPath: string, remoteOnly: boolean) {
    if (runID && artifactRef) {
        if (remoteOnly) {
            await DownloadSkillRunArtifact(runID, artifactRef);
        }
        await OpenSkillRunArtifact(runID, artifactRef);
        return;
    }
    await OpenFileOrShowInFolder(artifactPath);
}

async function revealSkillRunArtifactFromUI(runID: string, artifactRef: string, artifactPath: string) {
    if (runID && artifactRef) {
        await RevealSkillRunArtifact(runID, artifactRef);
        return;
    }
    await ShowItemInFolder(artifactPath);
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

function publishSubmissionStatusLabel(submission: AppPublishSubmission, text: typeof labels.zh) {
    if (submission.modifiedAt) return text.localModifiedReview;
    if (submission.status === 'submitted' && submission.channel === 'local') return text.localReviewPending;
    switch (submission.status) {
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
            app_ids: parseStringList(dep.app_ids || dep.appIDs || dep.AppIDs),
            installed: dep.installed === true || dep.Installed === true,
            installed_name: String(dep.installed_name || dep.installedName || dep.InstalledName || '').trim() || undefined,
            installed_dir: String(dep.installed_dir || dep.installedDir || dep.InstalledDir || '').trim() || undefined,
            installed_status: String(dep.installed_status || dep.installedStatus || dep.InstalledStatus || '').trim() || undefined,
            health: String(dep.health || dep.Health || '').trim() || undefined,
            action: String(dep.action || dep.Action || '').trim() || undefined,
            message: String(dep.message || dep.Message || '').trim() || undefined,
        });
        return items;
    }, []);
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
        };
    }).filter((issue) => issue.message);
}

function reviewIssueSummary(issue: AppReviewIssue) {
    return [issue.severity, issue.path, issue.message, issue.suggestion]
        .map((value) => String(value || '').trim())
        .filter(Boolean)
        .join(' · ');
}

function reviewIssuesSummary(issues: AppReviewIssue[], text: typeof labels.zh) {
    const visible = issues.slice(0, 3).map(reviewIssueSummary).filter(Boolean);
    const remaining = issues.length - visible.length;
    return [
        ...visible,
        remaining > 0 ? `${text.reviewIssuesMore} ${remaining} ${text.reviewIssuesMoreUnit}` : '',
    ].filter(Boolean).join(' / ');
}

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
        packageSHA: String(item?.package_sha256 || item?.packageSHA || ''),
        packageBytes: Number(item?.package_bytes || item?.packageBytes || 0) || 0,
        reviewedAt: String(item?.reviewed_at || item?.reviewedAt || ''),
        publishedAt: String(item?.published_at || item?.publishedAt || ''),
        reviewer: String(item?.reviewer || ''),
        riskLevel: normalizeRiskLevel(item?.risk_level || item?.riskLevel),
        approvedScopes: parseStringList(item?.approved_scopes || item?.approvedScopes),
        reviewIssues: parseReviewIssues(item?.review_issues || item?.reviewIssues),
        dependencies: parseBackendAppInstallDependencies(item?.dependencies || item?.Dependencies),
        eventCount: Number(item?.event_count || item?.eventCount || 0) || 0,
        lastEventAt: String(item?.last_event_at || item?.lastEventAt || ''),
        message: String(item?.message || ''),
    })).filter((item) => item.submissionID);
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

function normalizePublishStatus(value: unknown): AppPublishStatus | '' {
    const status = String(value || '').trim();
    return status === 'submitted' || status === 'review_failed' || status === 'approved' || status === 'published' || status === 'deprecated' || status === 'revoked'
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

function latestAppRunEvidence(app: AppEntry): AppRunHistoryEntry | null {
    const expectedHash = appDefinitionFingerprint(app);
    const local = loadAppRunHistory(app.id).find((item) => item.status === 'done' && item.definitionHash === expectedHash);
    return local || normalizeImportedRunEvidence(app.importedRunEvidence) || null;
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

function appDependencyEvidence(app: AppEntry) {
    return appSkillDependencies(app).map((dep) => ({
        id: dep.id,
        version: dep.version,
        kind: dep.kind || 'runtime_skill',
        required: dep.required !== false,
        source: dep.source || 'hub',
        capabilities: dep.capabilities || [],
    }));
}

function appDependencyPublishSummary(app: AppEntry, lang?: string) {
    const zh = isZh(lang);
    const deps = appDependencyEvidence(app);
    if (deps.length === 0) return zh ? '\u65e0 Skill \u4f9d\u8d56' : 'No Skill dependencies';
    return deps.map((dep) => `${dep.id}${dep.version ? `@${dep.version}` : ''} (${dep.kind}${dep.required ? '' : ', optional'})`).join(', ');
}

function appWorkspaceLayoutEvidence(app: AppEntry) {
    const ui = normalizeAppWorkspaceLayout(app.manifest?.ui, app.kind);
    const entry = ui.entry || workspaceEntryForKind(app.kind);
    const layout = ui.layouts?.[entry] || {};
    const runtime = normalizeRuntimeWorkspaceLayout(layout, app.kind);
    const regions = Array.isArray(layout.regions) ? layout.regions : studioRegionsForLayout(app.kind, runtime.template, runtime.primaryRegion, runtime.outputRegion);
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
        regionCount: regions.length,
        regions,
        savedInManifest: !!layout.studio?.savedInManifest || !!app.manifest?.ui,
        ...enterpriseUI,
    };
}

function appWorkspaceLayoutPublishSummary(app: AppEntry, lang?: string) {
    const zh = isZh(lang);
    const layout = appWorkspaceLayoutEvidence(app);
    if (!layout.schema || !layout.entry || layout.regionCount <= 0) return zh ? '\u7f3a\u5c11 workspace layout' : 'Missing workspace layout';
    return `${layout.entry} / ${layout.template} / ${layout.density} / ${layout.regionCount} regions`;
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
function appResultContractPublishSummary(app: AppEntry, lang?: string) {
    const zh = isZh(lang);
    const contract = appResultContractForManifest(app);
    const types = contract.types.slice(0, 4).join(', ');
    return `${zh ? '\u7ed3\u679c' : 'Result'}: ${contract.primary}${types ? ` / ${types}` : ''}`;
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

function appRunEvidenceContractCoverage(app: AppEntry, evidence: AppRunHistoryEntry | null, lang?: string) {
    const zh = isZh(lang);
    const contract = appResultContractForManifest(app);
    const coveredTypes = appRunEvidenceResultTypes(evidence);
    const covered = new Set(coveredTypes);
    const primary = String(contract.primary || '').trim();
    const primaryCovered = !primary || covered.has(primary) || (primary === 'document' && covered.has('artifact')) || (primary === 'artifact' && covered.has('document')) || (primary === 'text' && covered.has('content')) || (primary === 'content' && covered.has('text'));
    const missingTypes = primaryCovered || !primary ? [] : [primary];
    const evidenceLabel = evidence ? `${evidence.runID || ''} / ${evidence.at || ''}`.trim() : '';
    const coveredLabel = coveredTypes.length > 0 ? coveredTypes.join(', ') : (zh ? '\u672a\u8bc6\u522b\u7ed3\u679c\u7c7b\u578b' : 'No result type recognized');
    return {
        ok: !!evidence && primaryCovered,
        primary,
        coveredTypes,
        missingTypes,
        detail: !evidence
            ? (zh ? '\u63d0\u4ea4\u5ba1\u6838\u524d\u5efa\u8bae\u5148\u8bd5\u8fd0\u884c\u4e00\u6b21' : 'Run the app once before review')
            : primaryCovered
                ? `${evidenceLabel}${primary ? ` / primary: ${primary}` : ''} / ${zh ? '\u8986\u76d6' : 'covered'}: ${coveredLabel}`
                : `${evidenceLabel} / ${zh ? '\u8fd0\u884c\u8bc1\u636e\u672a\u8986\u76d6\u7ed3\u679c\u5951\u7ea6' : 'Run evidence does not cover result contract'}: ${missingTypes.join(', ')}`,
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
    if (app.source !== 'market' && app.source !== 'skill' && app.source !== 'local') return false;
    return appDependencyEvidence(app).length > 0;
}
function appNeedsAutomaticRuntimeDependencyCheck(app: AppEntry) {
    return (app.source === 'market' || app.source === 'skill') && appDependencyEvidence(app).length > 0;
}
function appGovernanceForManifest(app: AppEntry, submission?: AppPublishSubmission, overrides: AppGovernanceOverrides = {}) {
    const evidence = latestAppRunEvidence(app);
    const evidenceArtifacts = appRunHistoryArtifacts(evidence);
    const evidenceOutputs = normalizeApprovalOutputs(evidence?.outputs) || [];
    const resultContract = appResultContractForManifest(app);
    const primaryResult = appRunPrimaryResultFromPayload(resultContract, evidence?.resultPayload, evidenceOutputs);
    const resultCoverage = appRunEvidenceContractCoverage(app, evidence);
    const primaryArtifact = evidenceArtifacts[0];
    const artifactName = primaryArtifact?.name || (primaryArtifact?.path ? primaryArtifact.path.split(/[\\/]/).pop() : undefined);
    const dependencies = appDependencyEvidence(app);
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
        dependencyVerification: overrides.dependencyVerification ? {
            schema: overrides.dependencyVerification.schema || 'maclaw.app.install_plan.v1',
            verifiedAt: new Date().toISOString(),
            appCount: overrides.dependencyVerification.apps?.length || 0,
            dependencyCount: overrides.dependencyVerification.dependencies?.length || 0,
            hasMissingRequired: !!overrides.dependencyVerification.has_missing_required,
            hasBlockingDependency: !!overrides.dependencyVerification.has_blocking_dependency,
            dependencies: parseBackendAppInstallDependencies(overrides.dependencyVerification.dependencies),
        } : undefined,
        workspaceLayout: appWorkspaceLayoutEvidence(app),
        resultContract,
        testEvidence: {
            runId: evidence?.runID,
            definitionHash: evidence?.definitionHash,
            artifactPresent: evidenceArtifacts.length > 0,
            artifactCount: evidenceArtifacts.length,
            artifactName,
            artifacts: evidenceArtifacts.map(appRunHistoryArtifactEvidence),
            outputCount: evidenceOutputs.length,
            outputs: evidenceOutputs.map(appRunHistoryOutputEvidence),
            resultPayload: evidence?.resultPayload,
            primaryResult: primaryResult || undefined,
            resultCoverage: {
                ok: resultCoverage.ok,
                primary: resultCoverage.primary,
                coveredTypes: resultCoverage.coveredTypes,
                missingTypes: resultCoverage.missingTypes,
            },
            verifiedAt: evidence?.at || undefined,
        },
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

function formatRecentUsedAt(value?: string) {
    const timestamp = Number(String(value || '').slice(0, 13));
    if (!Number.isFinite(timestamp) || timestamp <= 0) return '';
    return new Date(timestamp).toLocaleString();
}

function buildAppTileTooltip(app: AppEntry, text: typeof labels.zh, statusLabel: string, lang?: string) {
    const recent = formatRecentUsedAt(app.recentUsedAt);
    return [
        app.name,
        app.description,
        `${appKinds[app.kind][isZh(lang) ? 'zh' : 'en']} · ${sourceLabels[app.source][isZh(lang) ? 'zh' : 'en']}`,
        `${text.appStatus}: ${statusLabel}`,
        `${text.recentUsed}: ${recent || text.neverUsed}`,
    ].filter(Boolean).join('\n');
}

function buildAppTileAriaLabel(app: AppEntry, text: typeof labels.zh, statusLabel: string, lang?: string) {
    const locale = isZh(lang) ? 'zh' : 'en';
    return `${app.name}, ${appKinds[app.kind][locale]}, ${sourceLabels[app.source][locale]}, ${text.appStatus}: ${statusLabel}`;
}

function buildAppSearchText(app: AppEntry, lang?: string) {
    const manifest = app.manifest;
    return [
        app.id,
        app.name,
        app.description,
        app.category,
        appKinds[app.kind][isZh(lang) ? 'zh' : 'en'],
        app.kind,
        sourceLabels[app.source][isZh(lang) ? 'zh' : 'en'],
        app.source,
        manifest?.launchMode,
        manifest?.installUnit,
        manifest?.datasrv?.domain,
        manifest?.datasrv?.preferredAction,
        manifest?.datasrv?.preferredView,
        manifest?.datasrv?.preferredReport,
        manifest?.datasrv?.preferredDashboard,
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
        return zh ? `搜索“${trimmedQuery}” · ${categoryText} · ${count} \u4e2a\u5339\u914d` : `Search "${trimmedQuery}" · ${categoryText} · ${count} matches`;
    }
    if (trimmedQuery) {
        return zh ? `搜索“${trimmedQuery}” · ${count} \u4e2a\u5339\u914d` : `Search "${trimmedQuery}" · ${count} matches`;
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

export const AppsPage = ({ lang }: AppsPageProps) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [apps, setApps] = useState(() => applyLayoutState(initialApps, readLayoutState()));
    const [query, setQuery] = useState('');
    const [category, setCategory] = useState('all');
    const [openTabs, setOpenTabs] = useState<string[]>([]);
    const [activeTabId, setActiveTabId] = useState('');
    const [studioOpen, setStudioOpen] = useState(false);
    const [studioTab, setStudioTab] = useState<StudioTab>('create');
    const [studioEditAppId, setStudioEditAppId] = useState('');
    const [activeOperation, setActiveOperation] = useState<AppOperation | null>(null);
    const [approvalInitialAppFilter, setApprovalInitialAppFilter] = useState('all');
    const [tileMenu, setTileMenu] = useState<{ appId: string; x: number; y: number } | null>(null);
    const [datasrvDiscovery, setDataSrvDiscovery] = useState<DataSrvDiscovery>(emptyDiscovery);
    const [skillDiscovery, setSkillDiscovery] = useState<SkillAppDiscovery>(emptySkillDiscovery);

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
        setApps((current) => {
            let changed = false;
            let next = current;
            for (const candidate of skillDiscovery.candidates) {
                if (next.some((app) => app.id === candidate.id)) continue;
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
    const openTabApps = openTabs.map((id) => apps.find((app) => app.id === id)).filter((app): app is AppEntry => !!app);
    const activeApp = apps.find((app) => app.id === activeTabId) ?? openTabApps[0];
    const hiddenApps = initialApps.filter((app) => !apps.some((item) => item.id === app.id));

    const openApp = (app: AppEntry) => {
        markAppUsed(app.id);
        setTileMenu(null);
        setOpenTabs((current) => current.includes(app.id) ? current : [...current, app.id]);
        setActiveTabId(app.id);
        setStudioOpen(false);
        setActiveOperation(null);
    };

    const openOperation = (operation: AppOperation, options?: { appId?: string }) => {
        setTileMenu(null);
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
        clearAppPublishSubmission(appId);
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
                if (normalizeAppVersion(app.version) <= normalizeAppVersion(existing.version)) return current;
                const next = [...current];
                next[existingIndex] = {
                    ...app,
                    id: existing.id,
                    pinned: existing.pinned,
                    category: existing.category,
                    icon: existing.icon,
                    accent: existing.accent,
                    recentUsedAt: existing.recentUsedAt,
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

    const appStatusInfo = (app: AppEntry): { key: 'available' | 'running' | 'loading' | 'disabled' | 'error'; label: string } => {
        if (openTabs.includes(app.id)) return { key: 'running', label: text.appRunning };
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
        return (
            <button
                key={app.id}
                className={`apps-app-tile ${activeApp?.id === app.id && !studioOpen ? 'is-active' : ''}`}
                data-status={status.key}
                style={{ '--apps-icon-color': app.accent } as CSSProperties}
                title={buildAppTileTooltip(app, text, status.label, lang)}
                aria-label={buildAppTileAriaLabel(app, text, status.label, lang)}
                onClick={() => openApp(app)}
                onContextMenu={(event) => {
                    event.preventDefault();
                    openTileMenu(app.id, event.clientX, event.clientY);
                }}
                onKeyDown={moveTileFocus}
            >
                <span className="apps-app-icon"><AppIcon icon={app.icon} customIconDataUrl={app.customIconDataUrl} /></span>
                <span className="apps-app-name">{app.name}</span>
            </button>
        );
    };

    return (
        <div className="apps-page">
            <aside className="apps-panel" aria-label={text.apps}>
                <div className="apps-panel__top">
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
                    <button className="apps-studio-button" type="button" title={text.appStudio} aria-label={text.appStudio} onClick={() => { setActiveOperation(null); setStudioOpen(true); }}>
                        <span className="apps-studio-button__icon" aria-hidden="true">
                            <Icon name="dashboard" />
                            <svg className="apps-studio-button__plus" viewBox="0 0 12 12" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M6 2v8M2 6h8" /></svg>
                        </span>
                    </button>
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
                        onRemoveApp={removeApp}
                        onRestoreApp={restoreApp}
                        pendingEditAppId={studioEditAppId}
                        onPendingEditConsumed={() => setStudioEditAppId('')}
                        hiddenApps={hiddenApps}
                        datasrvDiscovery={datasrvDiscovery}
                        skillDiscovery={skillDiscovery}
                        onAddDiscoveredApp={addDiscoveredApp}
                        onCreateApp={addDiscoveredApp}
                        onInstallMarketApp={installMarketApp}
                        onEditApp={editAppFromStudio}
                    />
                ) : activeOperation === 'approval_status' ? (
                    <ApprovalManager
                        apps={apps}
                        lang={lang}
                        initialAppFilter={approvalInitialAppFilter}
                    />
                ) : activeOperation === 'run_history' ? (
                    <RunHistoryManager apps={apps} lang={lang} />
                ) : (
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
                    />
                )}
            </main>
        </div>
    );
};

const AppRuntime = ({ tabs, activeApp, lang, onActivate, onClose, onUse, onOpenApprovalManager }: {
    tabs: AppEntry[];
    activeApp?: AppEntry;
    lang?: string;
    onActivate: (appId: string) => void;
    onClose: (appId: string) => void;
    onUse: (appId: string) => void;
    onOpenApprovalManager: (appId?: string) => void;
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
                            x                        </button>
                    </div>
                    );
                })}
            </div>
            <div
                className="apps-runtime-shell"
                role="tabpanel"
                id={activeApp ? getRuntimePanelId(activeApp.id) : undefined}
                aria-labelledby={activeApp ? getRuntimeTabId(activeApp.id) : undefined}
            >
                <AppPreview app={activeApp} lang={lang} onUse={onUse} onOpenApprovalManager={onOpenApprovalManager} />
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
            approver: submitted ? (zh ? '\u76f4\u5c5e\u4e3b\u7ba1 / VE' : 'Manager / VE') : workflow,
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

function backendApprovalInstanceToView(instance: BackendApprovalInstance, lang?: string): ApprovalInstanceView | null {
    const id = String(instance.instance_id || '').trim();
    const appTitle = String(instance.title || id).trim();
    if (!id || !appTitle) return null;
    const lane = String(instance.lane || 'my_requests').trim() as ApprovalInstanceView['lane'];
    const status = String(instance.status || 'pending').trim() as ApprovalInstanceView['status'];
    const events = (instance.events || []).map((event): ApprovalInstanceEventView => ({
        at: String(event.at || '').trim(),
        node: String(event.node || '').trim() || undefined,
        actor: String(event.actor || '').trim() || undefined,
        decision: String(event.decision || event.action || '').trim() || undefined,
        message: String(event.message || event.note || '').trim() || undefined,
    })).filter((event) => event.at || event.node || event.actor || event.decision || event.message);
    const approvalID = String(instance.approval_id || instance.record_approval_id || '').trim();
    const objectRole = String(instance.object_role || instance.approval_object_role || '').trim();
    return {
        id,
        appID: String(instance.app_id || '').trim(),
        appName: String(instance.app_name || '').trim(),
        blueprintID: String(instance.blueprint_id || '').trim(),
        datasetID: String(instance.dataset_id || '').trim(),
        objectRole,
        approvalEvent: String(instance.approval_event || '').trim(),
        approvalID,
        title: appTitle,
        lane: lane === 'pending_my_approval' || lane === 'handled' || lane === 'attention' ? lane : 'my_requests',
        status: status === 'approved' || status === 'rejected' || status === 'attention' || status === 'draft' ? status : 'pending',
        currentNode: String(instance.current_node || (isZh(lang) ? '\u5f53\u524d\u8282\u70b9' : 'Current node')).trim(),
        owner: String(instance.owner || '-').trim(),
        approver: String(instance.approver || '-').trim(),
        updatedAt: String(instance.updated_at || '-').trim(),
        result: String(instance.result || '-').trim(),
        workflowSkillID: String(instance.workflow_skill_id || '').trim(),
        workflowDecisionID: String(instance.workflow_decision_id || '').trim(),
        businessStatus: String(instance.business_status || '').trim(),
        resultStatus: String(instance.result_status || '').trim(),
        recordID: String(instance.record_id || approvalPayloadRecordID(instance.result_payload) || '').trim(),
        detailURL: String(instance.detail_url || '').trim(),
        resultPayload: compactApprovalRecord(instance.result_payload),
        outputs: normalizeApprovalOutputs(instance.outputs),
        artifacts: normalizeApprovalArtifacts(instance.artifacts),
        events,
    };
}
function approvalInstanceMergeKey(instance: ApprovalInstanceView): string {
    return [instance.approvalID, instance.id, instance.recordID].find((value) => String(value || '').trim()) || instance.id;
}

function mergeApprovalInstanceViews(current: ApprovalInstanceView[], incoming: ApprovalInstanceView[]) {
    const next = [...incoming];
    const seen = new Set(next.map(approvalInstanceMergeKey));
    current.forEach((item) => {
        const key = approvalInstanceMergeKey(item);
        if (!seen.has(key)) next.push(item);
    });
    return next.slice(0, 50);
}

function appApprovalBinding(app: AppEntry): AppApprovalBinding | undefined {
    return app.manifest?.mis?.approvalBindings?.find((binding) => binding.workflowSkillId);
}

function appApprovalWorkflowSkillID(app: AppEntry): string {
    return appApprovalBinding(app)?.workflowSkillId || app.manifest?.dependencies?.skills?.find((dependency) => dependency.kind === 'workflow_skill')?.id || app.manifest?.appSkill?.id || app.manifest?.skill?.id || '';
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
    const datasetID = String(instance.dataset_id || appDataSrvDatasetID(app)).trim();
    const objectRole = String(instance.object_role || instance.approval_object_role || appApprovalBinding(app)?.objectRole || app.manifest?.datasrv?.objectRole || '').trim();
    const blueprintID = String(instance.blueprint_id || app.manifest?.datasrv?.blueprintID || '').trim();
    const recordID = appApprovalRecordID(instance);
    if ((!datasetID && !objectRole) || !recordID) return null;
    return {
        dataset_id: datasetID,
        object_role: objectRole,
        app_id: app.id,
        blueprint_id: blueprintID,
        record_id: recordID,
        approval_id: String(instance.approval_id || instance.record_approval_id || '').trim(),
        instance: {
            ...instance,
            app_id: instance.app_id || app.id,
            dataset_id: datasetID,
            object_role: objectRole,
            approval_object_role: instance.approval_object_role || objectRole,
            blueprint_id: blueprintID,
            record_id: recordID,
        },
    };
}

function approvalStatusLabel(status: ApprovalInstanceView['status'], lang?: string) {
    const zh = isZh(lang);
    if (status === 'approved') return zh ? '\u5df2\u901a\u8fc7' : 'Approved';
    if (status === 'rejected') return zh ? '\u5df2\u9a73\u56de' : 'Rejected';
    if (status === 'attention') return zh ? '\u9700\u5173\u6ce8' : 'Needs attention';
    if (status === 'pending') return zh ? '\u5ba1\u6279\u4e2d' : 'Pending';
    return zh ? '\u8349\u7a3f' : 'Draft';
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
const AppPreview = ({ app, lang, onUse, onOpenApprovalManager }: { app?: AppEntry; lang?: string; onUse?: (appId: string) => void; onOpenApprovalManager?: (appId?: string) => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [fileName, setFileName] = useState('');
    const [selectedFile, setSelectedFile] = useState<File | null>(null);
    const [selectedFiles, setSelectedFiles] = useState<File[]>([]);
    const [toolParams, setToolParams] = useState('');
    const [fieldValues, setFieldValues] = useState<Record<string, string | boolean>>({});
    const [outputMode, setOutputMode] = useState('docx');
    const [businessEntity, setBusinessEntity] = useState(app?.category || '');
    const [businessAction, setBusinessAction] = useState('create');
    const [businessNote, setBusinessNote] = useState('');
    const [runState, setRunState] = useState<'idle' | 'running' | 'done' | 'error' | 'cancelled'>('idle');
    const [validationMessage, setValidationMessage] = useState('');
    const [runID, setRunID] = useState('');
    const [skillRunStatus, setSkillRunStatus] = useState<SkillRunStatusView | null>(null);
    const [businessResult, setBusinessResult] = useState<BusinessOperationResultView | null>(null);
    const [runHistory, setRunHistory] = useState<AppRunHistoryEntry[]>([]);
    const [approvalInstances, setApprovalInstances] = useState<ApprovalInstanceView[]>([]);
    const [approvalInstancesLoadState, setApprovalInstancesLoadState] = useState<'idle' | 'loading' | 'error'>('idle');
    const [currentRunContext, setCurrentRunContext] = useState({ inputSummary: '', outputMode: '' });
    const [dependencyRepairState, setDependencyRepairState] = useState<'idle' | 'installing'>('idle');
    const [runtimeDependencyPlan, setRuntimeDependencyPlan] = useState<BackendAppInstallPlan | null>(null);
    const [runtimeDependencyCheckState, setRuntimeDependencyCheckState] = useState<'idle' | 'checking' | 'ready' | 'blocked' | 'error'>('idle');
    const approvalRunContextRef = useRef<ApprovalRunContext | null>(null);
    useEffect(() => {
        setFileName('');
        setSelectedFile(null);
        setSelectedFiles([]);
        setToolParams('');
        setFieldValues({});
        setOutputMode(normalizeOutputModes(app?.manifest?.skill?.outputModes)[0]);
        setBusinessEntity(app?.category || '');
        setBusinessAction('create');
        setBusinessNote('');
        setRunState('idle');
        setValidationMessage('');
        setRunID('');
        setSkillRunStatus(null);
        setBusinessResult(null);
        setCurrentRunContext({ inputSummary: '', outputMode: '' });
        setDependencyRepairState('idle');
        setRuntimeDependencyPlan(null);
        setRuntimeDependencyCheckState('idle');
        approvalRunContextRef.current = null;
        setRunHistory(loadAppRunHistory(app?.id || ''));
        setApprovalInstances([]);
        setApprovalInstancesLoadState('idle');
    }, [app?.id, app?.manifest?.skill?.outputModes]);
    useEffect(() => {
        if (!app || !appNeedsAutomaticRuntimeDependencyCheck(app)) {
            setRuntimeDependencyPlan(null);
            setRuntimeDependencyCheckState('idle');
            return;
        }
        let disposed = false;
        setRuntimeDependencyCheckState('checking');
        const checkDependencies = async () => {
            try {
                const dependencyPlan = await PlanMaclawAppInstall(JSON.stringify(appToManifest(app)));
                if (disposed) return;
                setRuntimeDependencyPlan(dependencyPlan || null);
                const blocked = !!dependencyPlan?.has_blocking_dependency || hasMissingRequiredBackendDependency(dependencyPlan, [app.id]);
                setRuntimeDependencyCheckState(blocked ? 'blocked' : 'ready');
            } catch {
                if (disposed) return;
                setRuntimeDependencyPlan(null);
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
            const records = await ListMaclawAppApprovalInstances(app.id, lane, 50) as BackendApprovalInstance[];
            const views = (records || []).map((item) => backendApprovalInstanceToView(item, lang)).filter(Boolean) as ApprovalInstanceView[];
            setApprovalInstances((current) => lane === 'all' ? views : mergeApprovalInstanceViews(current, views));
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
        const nextEntry: AppRunHistoryEntry = { ...entry, appID, at: new Date().toISOString() };
        setRunHistory((current) => {
            const next = [nextEntry, ...current.filter((item) => item.runID !== nextEntry.runID)].slice(0, 8);
            saveAppRunHistory(appID, next);
            return next;
        });
    };
    const finalizeApprovalRunFromStatus = useCallback(async (status: SkillRunStatusView | null, lifecycle: 'done' | 'error' | 'cancelled') => {
        const context = approvalRunContextRef.current;
        if (!context || !app?.id || !isEnterpriseApprovalAppKind(app.kind)) return;
        approvalRunContextRef.current = null;
        const completion = approvalWorkflowResultFromSkillRunStatus(status, lifecycle, lang);
        const now = new Date().toISOString();
        const previous = context.instance;
        const payload: BackendApprovalInstance = {
            ...previous,
            instance_id: previous.instance_id,
            app_id: previous.app_id || app.id,
            app_name: previous.app_name || app.name,
            workflow_decision_id: runID || previous.workflow_decision_id,
            lane: completion.lane,
            status: completion.status,
            current_node: completion.currentNode,
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
            const syncPayload = approvalDataSrvSyncPayload(app, savedInstance);
            if (syncPayload) {
                void SyncMaclawAppApprovalInstanceToDataSrv(syncPayload).catch(() => undefined);
            }
        } catch (error: any) {
            setValidationMessage(error?.message || String(error || 'approval sync failed'));
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
                    setValidationMessage('');
                    setRunState('done');
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
                    });
                    if (app?.source === 'skill' && app.manifest?.skill?.id) {
                        void RecordMaclawAppRunEvidenceForSkill(app.manifest.skill.id, app.id, definitionHash || '', runID, artifactPath || artifactName || artifactURI, verifiedAt).catch(() => undefined);
                    }
                    await finalizeApprovalRunFromStatus(status || null, lifecycle);
                } else if (lifecycle === 'error') {
                    const message = skillRunErrorMessage(status) || text.skillRunFailed;
                    setValidationMessage(message);
                    setRunState('error');
                    recordRunHistory({ runID, status: 'error', outputMode: currentRunContext.outputMode, inputSummary: currentRunContext.inputSummary, message });
                    await finalizeApprovalRunFromStatus(status || null, lifecycle);
                } else if (lifecycle === 'cancelled') {
                    setValidationMessage(text.skillRunCancelled);
                    setRunState('cancelled');
                    recordRunHistory({ runID, status: 'cancelled', outputMode: currentRunContext.outputMode, inputSummary: currentRunContext.inputSummary, message: text.skillRunCancelled });
                    await finalizeApprovalRunFromStatus(status || null, lifecycle);
                }
            } catch (error: any) {
                if (disposed) return;
                const message = error?.message || String(error || text.skillRunFailed);
                setValidationMessage(message);
                setBusinessResult(null);
                setRunState('error');
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
    }, [runID, runState, text.skillRunCancelled, text.skillRunCompleted, text.skillRunFailed, currentRunContext, finalizeApprovalRunFromStatus]);
    if (!app) return <div className="apps-empty">{text.noApps}</div>;
    const isTool = app.kind === 'tool_app';
    const isApproval = isEnterpriseApprovalAppKind(app.kind);
    const isBusiness = app.kind === 'enterprise_normal_app';
    const isAutomation = app.kind === 'automation_app';
    const outputModes = normalizeOutputModes(app.manifest?.skill?.outputModes);
    const approvalBinding = appApprovalBinding(app);
    const workflowSkillID = appApprovalWorkflowSkillID(app);
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
        setRunState('idle');
        setValidationMessage('');
        setRunID('');
        setSkillRunStatus(null);
        setCurrentRunContext({ inputSummary: '', outputMode: '' });
        setDependencyRepairState('idle');
        setRuntimeDependencyPlan(null);
        setRuntimeDependencyCheckState('idle');
        approvalRunContextRef.current = null;
    };
    const runApp = async (options?: { skipDependencyCheck?: boolean }) => {
        setDependencyRepairState('idle');
        approvalRunContextRef.current = null;
        if (app?.id) onUse?.(app.id);
        if (!options?.skipDependencyCheck && app && appNeedsRuntimeDependencyCheck(app)) {
            const existingPlanBlocked = !!runtimeDependencyPlan?.has_blocking_dependency || hasMissingRequiredBackendDependency(runtimeDependencyPlan, [app.id]);
            if (existingPlanBlocked) {
                setRuntimeDependencyCheckState('blocked');
                setValidationMessage(backendDependencyUnavailableMessage(app, runtimeDependencyPlan, text, lang));
                setRunState('error');
                return;
            }
            try {
                setRuntimeDependencyCheckState('checking');
                const dependencyPlan = await PlanMaclawAppInstall(JSON.stringify(appToManifest(app)));
                setRuntimeDependencyPlan(dependencyPlan || null);
                if (dependencyPlan?.has_blocking_dependency || hasMissingRequiredBackendDependency(dependencyPlan, [app.id])) {
                    setRuntimeDependencyCheckState('blocked');
                    setValidationMessage(backendDependencyUnavailableMessage(app, dependencyPlan, text, lang));
                    setRunState('error');
                    return;
                }
                setRuntimeDependencyCheckState('ready');
            } catch (error: any) {
                setRuntimeDependencyPlan(null);
                setRuntimeDependencyCheckState('error');
                setValidationMessage(error?.message || text.dependencyPlanError);
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
            if ((showFileInput && !fileName) || missingRequiredField) {
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
            const skillID = app.manifest?.skill?.id;
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
                    const runID = await RunNLSkillAsync(skillID, {
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
                    });
                    const nextRunID = String(runID || '').trim();
                    if (!nextRunID) throw new Error(text.skillRunFailed);
                    setRunID(nextRunID);
                    setValidationMessage('');
                    setRunState('running');
                    return;
                } catch (error: any) {
                    const message = error?.message || String(error || text.validationMissing);
                    setValidationMessage(message);
                    setRunState('error');
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
            const inputSummary = `${businessEntity || app.category} / ${businessAction || 'create'}`;
            if (!workflowSkillID) {
                setValidationMessage(text.missingRequiredDependency);
                setRunState('error');
                return;
            }
            setRunState('running');
            setCurrentRunContext({ inputSummary, outputMode: 'approval' });
            let workflowRunID = '';
            try {
                const runID = await RunNLSkillAsync(workflowSkillID, {
                    _maclaw_app: true,
                    app_id: app.id,
                    app_name: app.name,
                    app_kind: app.kind,
                    approval_instance_id: fallbackID,
                    approval_event: approvalBinding?.event || '',
                    approval_object_role: approvalObjectRole,
                    object_role: approvalObjectRole,
                    approval_workflow_skill_id: workflowSkillID,
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
                    prompt: `Start MaClaw approval workflow: ${app.name} / ${inputSummary}`,
                });
                workflowRunID = String(runID || '').trim();
                if (!workflowRunID) throw new Error(text.skillRunFailed);
            } catch (error: any) {
                const message = error?.message || String(error || text.skillRunFailed);
                setValidationMessage(message);
                setRunState('error');
                recordRunHistory({
                    runID: `failed-${Date.now().toString(36)}`,
                    status: 'error',
                    outputMode: 'approval',
                    inputSummary,
                    message,
                });
                return;
            }
            const payload: BackendApprovalInstance = {
                instance_id: fallbackID,
                app_id: app.id,
                app_name: app.name,
                workflow_skill_id: workflowSkillID,
                workflow_decision_id: workflowRunID,
                approval_event: approvalBinding?.event || undefined,
                approval_object_role: approvalObjectRole || undefined,
                object_role: approvalObjectRole || undefined,
                dataset_id: approvalDatasetID || undefined,
                blueprint_id: approvalBlueprintID || undefined,
                title: `${businessEntity || app.category} / ${businessAction || 'create'}`,
                lane: 'my_requests',
                status: 'pending',
                current_node: workflowMapping?.approvalNode || (isZh(lang) ? '\u4e3b\u7ba1\u5ba1\u6279' : 'Manager approval'),
                owner: isZh(lang) ? '\u5f53\u524d\u7528\u6237' : 'Current user',
                applicant: isZh(lang) ? '\u5f53\u524d\u7528\u6237' : 'Current user',
                approver: isZh(lang) ? '\u5ba1\u6279\u4eba' : 'Approver',
                result: isZh(lang) ? '\u7b49\u5f85\u5ba1\u6279' : 'Pending approval',
                business_status: 'approval_pending',
                result_status: 'pending',
                business_entity: businessEntity || app.category,
                business_action: businessAction || 'create',
                business_note: businessNote,
                created_at: now,
                updated_at: now,
                detail_url: `skill-run://${workflowRunID}`,
                events: [{
                    at: now,
                    actor: isZh(lang) ? '\u5f53\u524d\u7528\u6237' : 'Current user',
                    node: workflowMapping?.submitNode,
                    action: isZh(lang) ? '\u53d1\u8d77\u7533\u8bf7' : 'Submitted',
                    note: businessNote,
                }, {
                    at: now,
                    node: workflowMapping?.approvalNode || workflowSkillID,
                    actor: 'workflow',
                    action: 'workflow_started',
                    message: workflowRunID,
                }],
            };
            approvalRunContextRef.current = { instance: payload };
            try {
                const saved = await RecordMaclawAppApprovalInstance(payload) as BackendApprovalInstance;
                const savedInstance = saved || payload;
                approvalRunContextRef.current = { instance: savedInstance };
                const view = backendApprovalInstanceToView(savedInstance, lang);
                if (view) {
                    setApprovalInstances((current) => [view, ...current.filter((item) => item.id !== view.id)].slice(0, 50));
                }
                const syncPayload = approvalDataSrvSyncPayload(app, savedInstance);
                if (syncPayload) {
                    void SyncMaclawAppApprovalInstanceToDataSrv(syncPayload).catch(() => undefined);
                }
            } catch {
                const view = backendApprovalInstanceToView(payload, lang);
                if (view) {
                    setApprovalInstances((current) => [view, ...current.filter((item) => item.id !== view.id)].slice(0, 50));
                }
            }
            setRunID(workflowRunID);
            setValidationMessage('');
            setRunState('running');
            return;
        }
        if (isBusiness) {
            const inputSummary = `${businessEntity || app.category} / ${businessActionRole || businessAction || 'execute'}`;
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
                prompt: `Run MaClaw business app: ${app.name} / ${inputSummary}`,
            };
            if (businessSkillID) {
                setRunState('running');
                setCurrentRunContext({ inputSummary, outputMode: 'business' });
                try {
                    const runID = await RunNLSkillAsync(businessSkillID, businessPayload);
                    const nextRunID = String(runID || '').trim();
                    if (!nextRunID) throw new Error(text.skillRunFailed);
                    setRunID(nextRunID);
                    setValidationMessage('');
                    setRunState('running');
                    return;
                } catch (error: any) {
                    const message = error?.message || String(error || text.skillRunFailed);
                    setValidationMessage(message);
                    setRunState('error');
                    recordRunHistory({ runID: `failed-${Date.now().toString(36)}`, status: 'error', outputMode: 'business', inputSummary, message });
                    return;
                }
            }
            setRunState('running');
            setBusinessResult(null);
            setCurrentRunContext({ inputSummary, outputMode: 'business' });
            try {
                const result = await ExecuteMaclawAppBusinessOperation({
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
                setRunState('done');
                recordRunHistory({ runID: `business-${Date.now().toString(36)}`, status: 'done', outputMode: 'business', inputSummary, message: text.runCompleted + ': ' + businessOperationResult.mode + ' / ' + businessOperationResult.status, resultPayload: businessOperationResult.response });
                return;
            } catch (error: any) {
                const message = error?.message || String(error || text.skillRunFailed);
                setValidationMessage(message);
                setRunState('error');
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
            if (dependencyInstallPlan?.has_blocking_dependency || hasMissingRequiredBackendDependency(dependencyInstallPlan, [app.id])) {
                setValidationMessage(backendDependencyUnavailableMessage(app, dependencyInstallPlan, text, lang));
                setRunState('error');
                setDependencyRepairState('idle');
                return;
            }
            setRuntimeDependencyPlan(null);
            setDependencyRepairState('idle');
            await runApp({ skipDependencyCheck: true });
        } catch (error: any) {
            setRuntimeDependencyPlan(null);
            setValidationMessage(error?.message || text.dependencyPlanError);
            setRunState('error');
            setDependencyRepairState('idle');
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
        const payload: BackendApprovalInstance = {
            instance_id: instance.id,
            app_id: instance.appID || app.id,
            app_name: app.name,
            workflow_skill_id: instance.workflowSkillID || workflowSkillID,
            workflow_decision_id: `decision-${Date.now().toString(36)}`,
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
            result: statusText,
            business_status: decision,
            result_status: decision,
            record_id: instance.recordID,
            detail_url: instance.detailURL,
            result_payload: instance.resultPayload,
            outputs: instance.outputs,
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
            const syncPayload = approvalDataSrvSyncPayload(app, savedInstance);
            if (syncPayload) {
                void SyncMaclawAppApprovalInstanceToDataSrv(syncPayload).catch(() => undefined);
            }
        } catch (error: any) {
            setValidationMessage(error?.message || String(error || text.validationMissing));
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
        recordRunHistory({ runID, status: 'cancelled', outputMode, inputSummary: toolInputSummary, message: text.skillRunCancelled });
        await finalizeApprovalRunFromStatus({ run_id: runID, status: 'cancelled', summary: { last_error_snippet: text.skillRunCancelled } }, 'cancelled');
    };
    const runtimeDependencyDetails = runtimeDependencyPlan ? backendDependenciesForApp(runtimeDependencyPlan, app.id) : [];
    const visibleRuntimeDependencyDetails = runtimeDependencyDetails.length > 0 ? runtimeDependencyDetails : runtimeDependencyPlan?.dependencies || [];
    const runtimeDependencyBlocked = !!app && (!!runtimeDependencyPlan?.has_blocking_dependency || hasMissingRequiredBackendDependency(runtimeDependencyPlan, [app.id]));
    const runtimeDependencyChecking = runtimeDependencyCheckState === 'checking' || dependencyRepairState === 'installing';
    const runtimeDependencyReady = runtimeDependencyCheckState === 'ready' && visibleRuntimeDependencyDetails.length > 0 && !runtimeDependencyBlocked;
    const runtimeDependencyMessage = backendDependencyUnavailableMessage(app, runtimeDependencyPlan, text, lang);
    const showRuntimeDependencyDetails = visibleRuntimeDependencyDetails.length > 0 && (runState === 'error' || dependencyRepairState === 'installing' || runtimeDependencyBlocked || runtimeDependencyReady);
    const canInstallRuntimeDependencies = !!app && appNeedsRuntimeDependencyCheck(app) && (runtimeDependencyBlocked || dependencyRepairState === 'installing' || validationMessage === text.missingRequiredDependency);
    const runDisabled = runState === 'running' || dependencyRepairState === 'installing';
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
                            : runtimeDependencyReady
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
                    : 'idle';
    const runtimeLayout = runtimeWorkspaceLayoutForApp(app);
    const runtimeOrder = runtimeWorkspaceOrder(runtimeLayout);
    const inputRegion = runtimeLayout.primaryRegion;
    const outputRegion = runtimeLayout.outputRegion;
    const secondaryRegion = runtimeLayout.primaryRegion === 'right' ? 'left' : 'right';
    const centerRegion = runtimeLayout.primaryRegion === 'center' ? secondaryRegion : 'center';

    return (
        <>
            <div className="apps-detail__header">
                <div>
                    <h2 className="apps-detail__title">{app.name}</h2>
                    <p className="apps-detail__subtitle">{app.description}</p>
                </div>
            </div>
            <div className="apps-detail__body elegant-scrollbar">
                <div className={`apps-preview apps-preview--layout-${runtimeLayout.template}`}>
                    <div className="apps-preview__mock apps-runtime-layout" data-template={runtimeLayout.template} data-density={runtimeLayout.density} data-primary-region={runtimeLayout.primaryRegion} data-output-region={runtimeLayout.outputRegion}>
                        <section className="apps-runtime-section apps-runtime-input" data-region={inputRegion} style={{ order: runtimeOrder.input }}>
                            <div className="apps-runtime-section__title">{text.runtimeInput}</div>
                            {isTool ? (
                                <>
                                    {showFileInput && (
                                        <label className="apps-drop-zone">
                                            <input
                                                type="file"
                                                multiple={allowMultipleFiles}
                                                onChange={(event) => {
                                                    const files = Array.from(event.currentTarget.files || []);
                                                    const file = files[0] || null;
                                                    setSelectedFiles(allowMultipleFiles ? files : files.slice(0, 1));
                                                    setSelectedFile(file);
                                                    setFileName(allowMultipleFiles && files.length > 1 ? files.map((item) => item.name).join(', ') : file?.name || '');
                                                    markDirty();
                                                }}
                                            />
                                            <span>{fileName ? `${text.selectedFile}: ${fileName}` : text.upload}</span>
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
                                                                    <input type="checkbox" checked={!!value} onChange={(event) => {
                                                                        setFieldValues((current) => ({ ...current, [field.name]: event.target.checked }));
                                                                        markDirty();
                                                                    }} />
                                                                    <span>{isZh(lang) ? '\u542f\u7528' : 'Enabled'}</span>
                                                                </label>
                                                            ) : field.type === 'select' ? (
                                                                <select aria-label={field.label || field.name} value={String(value)} onChange={(event) => {
                                                                    setFieldValues((current) => ({ ...current, [field.name]: event.target.value }));
                                                                    markDirty();
                                                                }}>
                                                                    {(field.options || []).map((option) => <option key={option} value={option}>{option}</option>)}
                                                                </select>
                                                            ) : (
                                                                <input aria-label={field.label || field.name} value={String(value)} required={field.required} onChange={(event) => {
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
                                                <textarea value={toolParams} onChange={(event) => {
                                                    setToolParams(event.target.value);
                                                    markDirty();
                                                }} placeholder={isZh(lang) ? '\u8f93\u5165\u5904\u7406\u8981\u6c42\u6216\u8868\u5355\u53c2\u6570\u3002' : 'Enter processing instructions or form parameters.'} />
                                            </div>
                                        )
                                    )}
                                    <div className="apps-form-row">
                                        <label>{text.output}</label>
                                        <select value={outputMode} onChange={(event) => {
                                            setOutputMode(event.target.value);
                                            markDirty();
                                        }}>
                                            {outputModes.map((mode) => <option key={mode} value={mode}>{outputModeLabel(mode)}</option>)}
                                        </select>
                                    </div>
                                </>
                            ) : isAutomation ? (
                                <>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u8fd0\u884c\u6a21\u5f0f' : 'Mode'}</label><select defaultValue="manual"><option value="manual">{isZh(lang) ? '\u624b\u52a8\u6267\u884c' : 'Manual run'}</option><option value="schedule">{isZh(lang) ? '\u5b9a\u65f6\u6267\u884c' : 'Scheduled'}</option><option value="monitor">{isZh(lang) ? '\u6301\u7eed\u76d1\u63a7' : 'Continuous monitor'}</option></select></div>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u72b6\u6001' : 'Status'}</label><input readOnly value={runState === 'done' ? (isZh(lang) ? '\u8fd0\u884c\u4e2d' : 'Running') : text.readyOutput} /></div>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u5907\u6ce8' : 'Note'}</label><textarea defaultValue={isZh(lang) ? '\u7531 Agent \u7ef4\u62a4\u957f\u8fd0\u884c\u4efb\u52a1\uff0c\u5e76\u5728\u5e94\u7528 tab \u4e2d\u56de\u62a5\u7ed3\u679c\u3002' : 'Agent maintains the long-running task and reports results in the app tab.'} /></div>
                                </>
                            ) : (
                                <>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u4e1a\u52a1\u5bf9\u8c61' : 'Entity'}</label><input value={businessEntity} onChange={(event) => {
                                        setBusinessEntity(event.target.value);
                                        markDirty();
                                    }} /></div>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u64cd\u4f5c' : 'Action'}</label><select value={businessAction} onChange={(event) => {
                                        setBusinessAction(event.target.value);
                                        markDirty();
                                    }}><option value="create">{isZh(lang) ? '\u65b0\u5efa\u8bb0\u5f55' : 'Create record'}</option><option value="query">{isZh(lang) ? '\u67e5\u8be2\u6570\u636e' : 'Query data'}</option><option value="report">{isZh(lang) ? '\u751f\u6210\u62a5\u8868' : 'Generate report'}</option></select></div>
                                    <div className="apps-form-row"><label>{isZh(lang) ? '\u5907\u6ce8' : 'Note'}</label><textarea value={businessNote} onChange={(event) => {
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
                        </section>
                        {isApproval && <ApprovalWorkspace app={app} runState={runState} businessEntity={businessEntity} businessAction={businessAction} businessNote={businessNote} backendInstances={approvalInstances} approvalLoadState={approvalInstancesLoadState} lang={lang} text={text} style={{ order: runtimeOrder.approval }} layoutRegion={centerRegion} onRefresh={loadApprovalInstances} onDecision={updateApprovalInstanceDecision} />}
                        {isBusiness && <BusinessWorkspace app={app} runState={runState} businessEntity={businessEntity} businessAction={businessAction} businessNote={businessNote} lang={lang} style={{ order: runtimeOrder.approval }} layoutRegion={centerRegion} />}
                        <section className="apps-runtime-section apps-runtime-status" data-region={outputRegion === 'bottom' ? centerRegion : outputRegion} style={{ order: runtimeOrder.status }}>
                            <div className="apps-runtime-section__title">{text.runtimeStatus}</div>
                            <div className={`apps-result-panel${showRuntimeDependencyDetails ? ' apps-result-panel--stacked' : ''}`} data-state={runtimeStatusState}>
                                <span>{runtimeStatusMessage}</span>
                                {showRuntimeDependencyDetails && <InstallRecordDependencies dependencies={visibleRuntimeDependencyDetails} text={text} />}
                                {canInstallRuntimeDependencies && (
                                    <button className="apps-secondary-button apps-result-panel__action" type="button" disabled={dependencyRepairState === 'installing'} onClick={() => void installRuntimeDependenciesAndRun()}>
                                        {dependencyRepairState === 'installing' ? text.installingDependencies : text.installDependenciesAndRun}
                                    </button>
                                )}
                            </div>
                            {isTool && <SkillRunEvidence status={skillRunStatus} runState={runState} text={text} />}
                        </section>
                        <div className="apps-actions apps-runtime-actions" data-region={inputRegion} style={{ order: runtimeOrder.actions }}>
                            <button className="apps-secondary-button" type="button" onClick={() => {
                                setFileName('');
                                setSelectedFile(null);
                                setSelectedFiles([]);
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
                                setCurrentRunContext({ inputSummary: '', outputMode: '' });
                                setDependencyRepairState('idle');
                                setRuntimeDependencyPlan(null);
                                approvalRunContextRef.current = null;
                            }}>{text.reset}</button>
                            {runState === 'running' && runID && <button className="apps-secondary-button" type="button" onClick={cancelRun}>{text.cancelRun}</button>}
                            <button className="apps-primary-button" type="button" disabled={runDisabled} onClick={() => runApp()}>{text.run}</button>
                        </div>
                        <AppRunOutput status={skillRunStatus} runState={runState} resultText={resultText} businessResult={businessResult} isTool={isTool} text={text} style={{ order: runtimeOrder.output }} layoutRegion={outputRegion} />
                        {(isTool || isBusiness) && (
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
                                            <div className="apps-run-history__item" data-state={item.status} key={`${item.runID}-${item.at}`}>
                                                <div>
                                                    <strong>{item.inputSummary || item.runID}</strong>
                                                    <span>{formatRunHistoryTime(item.at)} · {item.outputMode.toUpperCase()} · {item.runID}</span>
                                                    {appRunHistoryResultSummary(item, text) && <small className="apps-run-history__result">{appRunHistoryResultSummary(item, text)}</small>}
                                                    {(item.artifactURI || item.artifactPath) && <code>{item.artifactURI || item.artifactPath}</code>}
                                                </div>
                                            <div className="apps-run-history__side">
                                                <em>{item.message || item.status}</em>
                                                {(item.artifactURI || item.artifactPath) && (
                                                    <div className="apps-run-history__actions">
                                                            <button className="apps-link-button" type="button" onClick={() => void openSkillRunArtifactFromUI(item.runID, item.artifactID || item.artifactURI || '', item.artifactPath || '', item.artifactDownloadState === 'remote')}>{item.artifactDownloadState === 'remote' ? text.downloadArtifact : text.openArtifact}</button>
                                                            <button className="apps-link-button" type="button" onClick={() => void revealSkillRunArtifactFromUI(item.runID, item.artifactID || item.artifactURI || '', item.artifactPath || '')}>{text.revealArtifact}</button>
                                                    </div>
                                                )}
                                            </div>
                                            </div>
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

function approvalSearchText(item: ApprovalInstanceView, appName: string) {
    return [appName, item.appName, item.title, item.id, item.currentNode, item.owner, item.approver, item.result, item.workflowSkillID, item.workflowDecisionID, item.datasetID, item.objectRole, item.approvalID, item.businessStatus, item.resultStatus, item.recordID]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
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
    const appNameById = useMemo(() => new Map(apps.map((app) => [app.id, app.name])), [apps]);
    const approvalApps = useMemo(() => apps.filter((app) => isEnterpriseApprovalAppKind(app.kind)), [apps]);

    useEffect(() => {
        setAppFilter(initialAppFilter || 'all');
        setSelectedInstanceId('');
    }, [initialAppFilter]);

    const loadInstances = useCallback(async () => {
        setLoadingState('loading');
        try {
            const records = await ListMaclawAppApprovalInstancesAll(lane, 200) as BackendApprovalInstance[];
            setInstances((records || []).map((item) => backendApprovalInstanceToView(item, lang)).filter(Boolean) as ApprovalInstanceView[]);
            setLoadingState('idle');
        } catch {
            setInstances([]);
            setLoadingState('error');
        }
    }, [lane, lang]);

    useEffect(() => {
        void loadInstances();
    }, [loadInstances]);

    const filteredInstances = useMemo(() => {
        const normalizedQuery = query.trim().toLowerCase();
        return instances.filter((item) => {
            const appMatches = appFilter === 'all' || item.appID === appFilter;
            const statusMatches = statusFilter === 'all' || item.status === statusFilter;
            const queryMatches = !normalizedQuery || approvalSearchText(item, appNameById.get(item.appID || '') || '').includes(normalizedQuery);
            return appMatches && statusMatches && queryMatches;
        });
    }, [instances, appFilter, statusFilter, query, appNameById]);
    const selected = filteredInstances.find((item) => item.id === selectedInstanceId) || filteredInstances[0];
    const selectedApp = selected?.appID ? apps.find((item) => item.id === selected.appID) : undefined;
    const selectedResultContract = selectedApp ? appResultContractForManifest(selectedApp) : undefined;
    const lanes = approvalLanes(text);
    const countForLane = (key: ApprovalLaneFilter) => key === 'all'
        ? instances.length
        : key === 'handled'
            ? instances.filter((item) => item.status === 'approved' || item.status === 'rejected').length
            : instances.filter((item) => item.lane === key).length;

    const updateApprovalInstanceDecision = async (instance: ApprovalInstanceView, decision: 'approved' | 'rejected' | 'attention') => {
        if (!instance?.id) return;
        const now = new Date().toISOString();
        const zh = isZh(lang);
        const app = apps.find((item) => item.id === instance.appID);
        const statusText = decision === 'approved' ? (zh ? '\u5df2\u901a\u8fc7' : 'Approved') : decision === 'rejected' ? (zh ? '\u5df2\u9a73\u56de' : 'Rejected') : (zh ? '\u9700\u5173\u6ce8' : 'Needs attention');
        const nextLane: ApprovalInstanceView['lane'] = decision === 'attention' ? 'attention' : 'handled';
        const nextNode = decision === 'attention' ? instance.currentNode : (zh ? '\u5df2\u5b8c\u6210' : 'Completed');
        const payload: BackendApprovalInstance = {
            instance_id: instance.id,
            app_id: instance.appID,
            app_name: instance.appName || app?.name,
            workflow_skill_id: instance.workflowSkillID,
            workflow_decision_id: `decision-${Date.now().toString(36)}`,
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
            result: statusText,
            business_status: decision,
            result_status: decision,
            record_id: instance.recordID,
            detail_url: instance.detailURL,
            result_payload: instance.resultPayload,
            outputs: instance.outputs,
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
                const syncPayload = approvalDataSrvSyncPayload(app, savedInstance);
                if (syncPayload) void SyncMaclawAppApprovalInstanceToDataSrv(syncPayload).catch(() => undefined);
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
                <button className="apps-secondary-button" type="button" onClick={() => void loadInstances()}>{text.approvalRefresh}</button>
            </div>
            <div className="apps-detail__body elegant-scrollbar">
                <div className="apps-approval-manager">
                    <div className="apps-approval-manager__filters">
                        <input className="apps-search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={text.approvalSearch} />
                        <label><span>{text.approvalAppFilter}</span><select value={appFilter} onChange={(event) => { setAppFilter(event.target.value); setSelectedInstanceId(''); }}><option value="all">{text.approvalAllApps}</option>{approvalApps.map((app) => <option key={app.id} value={app.id}>{app.name}</option>)}</select></label>
                        <label><span>{text.approvalStatusFilter}</span><select value={statusFilter} onChange={(event) => { setStatusFilter(event.target.value); setSelectedInstanceId(''); }}><option value="all">{text.approvalAllStatuses}</option>{(['pending', 'approved', 'rejected', 'attention', 'draft'] as ApprovalInstanceView['status'][]).map((status) => <option key={status} value={status}>{approvalStatusLabel(status, lang)}</option>)}</select></label>
                    </div>
                    <nav className="apps-approval-manager__lanes" aria-label={text.approvalWorkspace}>
                        {lanes.map((item) => <button key={item.key} className={lane === item.key ? 'is-active' : ''} type="button" aria-pressed={lane === item.key} onClick={() => { setLane(item.key); setSelectedInstanceId(''); }}><span>{item.label}</span><strong>{countForLane(item.key)}</strong></button>)}
                    </nav>
                    {loadingState === 'loading' && <div className="apps-approval-empty" role="status">{text.approvalLoading}</div>}
                    {loadingState === 'error' && <div className="apps-approval-empty" role="alert">{text.approvalLoadError}</div>}
                    {loadingState !== 'loading' && loadingState !== 'error' && (
                        <div className="apps-approval-manager__body">
                            <div className="apps-approval-list" role="list" aria-label={text.approvalInstanceData}>
                                {filteredInstances.length === 0 ? <div className="apps-approval-empty" role="status">{text.noApprovalInstances}</div> : filteredInstances.map((item) => (
                                    <button className="apps-approval-row" data-state={item.status} data-selected={selected?.id === item.id ? 'true' : 'false'} role="listitem" type="button" key={item.id} onClick={() => setSelectedInstanceId(item.id)} aria-pressed={selected?.id === item.id}>
                                        <div><strong>{item.title}</strong><span>{appNameById.get(item.appID || '') || item.appName || item.appID || '-'} / {text.currentApprovalNode}: {item.currentNode}</span><small>{text.approvalInstanceId}: {item.id} / {item.updatedAt}</small></div>
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

const ApprovalDetail = ({ instance, resultContract, lang, text, onDecision }: { instance?: ApprovalInstanceView; resultContract?: AppResultContract; lang?: string; text: typeof labels.zh; onDecision: (instance: ApprovalInstanceView, decision: 'approved' | 'rejected' | 'attention') => void | Promise<void> }) => {
    const selectedOutputs = instance?.outputs || [];
    const selectedArtifacts = instance?.artifacts || [];
    const selectedPayloadEntries = instance?.resultPayload ? Object.entries(instance.resultPayload).filter(([, value]) => formatApprovalResultValue(value)) : [];
    const hasResultPackage = selectedOutputs.length > 0 || selectedArtifacts.length > 0 || selectedPayloadEntries.length > 0;
    const canDecide = !!instance && (instance.status === 'pending' || instance.status === 'draft' || instance.lane === 'pending_my_approval');
    return (
        <aside className="apps-approval-detail" aria-label={text.approvalTimeline}>
            {instance ? (
                <>
                    <div className="apps-approval-detail__head"><strong>{instance.title}</strong><span>{text.currentApprovalNode}: {instance.currentNode}</span></div>
                    <dl className="apps-approval-facts">
                        <div><dt>{text.myRequests}</dt><dd>{instance.owner}</dd></div><div><dt>{text.pendingMyApproval}</dt><dd>{instance.approver}</dd></div><div><dt>{text.approvalResult}</dt><dd>{instance.result}</dd></div>
                        <div><dt>{text.workflowSkill}</dt><dd>{instance.workflowSkillID || '-'}</dd></div><div><dt>{text.dataSrvRecord}</dt><dd>{instance.recordID || '-'}</dd></div><div><dt>{text.approvalObjectRoleLabel}</dt><dd>{instance.objectRole || '-'}</dd></div>
                        <div><dt>{text.remoteApprovalLabel}</dt><dd>{instance.approvalID || '-'}</dd></div><div><dt>{text.businessStatusLabel}</dt><dd>{instance.businessStatus || '-'}</dd></div><div><dt>{text.resultStatusLabel}</dt><dd>{instance.resultStatus || approvalStatusLabel(instance.status, lang)}</dd></div>
                    </dl>
                    {instance.detailURL && <div className="apps-approval-detail__links"><a className="apps-link-button" href={instance.detailURL} target="_blank" rel="noreferrer">{text.viewFullWorkflow}</a></div>}
                    {resultContract && <ResultContractPreview contract={resultContract} lang={lang} />}
                    {hasResultPackage && (
                        <section className="apps-approval-results" aria-label={text.approvalResultPackage}>
                            <div className="apps-approval-results__head"><strong>{text.approvalResultPackage}</strong><span>{selectedOutputs.length + selectedArtifacts.length + selectedPayloadEntries.length}</span></div>
                            {selectedOutputs.map((output, index) => {
                                const kind = approvalOutputKind(output);
                                const body = approvalOutputBody(output);
                                const artifactRef = output.artifact ? approvalArtifactReference(output.artifact) : '';
                                return <div className="apps-approval-output" data-kind={kind} key={`${instance.id}-output-${index}-${approvalOutputTitle(output, text)}`}><div><strong>{approvalOutputTitle(output, text)}</strong><span>{[kind, output.status].filter(Boolean).join(' / ')}</span></div>{body && <pre>{body}</pre>}{artifactRef && <code>{artifactRef}</code>}</div>;
                            })}
                            {selectedArtifacts.map((artifact, index) => <div className="apps-approval-artifact" key={`${instance.id}-artifact-${index}-${approvalArtifactReference(artifact)}`}><div><strong>{artifact.name || artifact.id || text.runArtifacts}</strong><span>{[artifact.status, artifact.mime_type, artifact.size_bytes ? `${artifact.size_bytes} bytes` : ''].filter(Boolean).join(' / ')}</span></div><code>{approvalArtifactReference(artifact)}</code></div>)}
                            {selectedPayloadEntries.length > 0 && <div className="apps-approval-payload"><strong>{text.approvalOutputData}</strong>{selectedPayloadEntries.map(([key, value]) => <div key={`${instance.id}-payload-${key}`}><span>{key}</span><pre>{formatApprovalResultValue(value)}</pre></div>)}</div>}
                        </section>
                    )}
                    <div className="apps-approval-timeline">
                        {(instance.events && instance.events.length > 0 ? instance.events : [{ node: isZh(lang) ? '\u53d1\u8d77\u8282\u70b9' : 'Submit node' }, { node: instance.currentNode }, { node: isZh(lang) ? '\u7ed3\u679c\u53cd\u9988' : 'Result feedback' }] as ApprovalInstanceEventView[]).map((event, index) => {
                            const primary = [event.node, event.decision].filter(Boolean).join(' / ') || event.message || '-';
                            const secondary = [event.actor, event.at].filter(Boolean).join(' / ');
                            return <div key={`${instance.id}-event-${index}-${primary}`}><span /><p><strong>{primary}</strong>{secondary && <small>{secondary}</small>}{event.message && primary !== event.message && <small>{event.message}</small>}</p></div>;
                        })}
                    </div>
                </>
            ) : <div className="apps-approval-empty" role="status">{text.noApprovalInstances}</div>}
            <div className="apps-approval-actions" aria-label={text.approvalActions}>
                <button className="apps-primary-button" type="button" onClick={() => instance && onDecision(instance, 'approved')} disabled={!canDecide}>{text.approve}</button>
                <button className="apps-secondary-button" type="button" onClick={() => instance && onDecision(instance, 'rejected')} disabled={!canDecide}>{text.reject}</button>
                <button className="apps-secondary-button" type="button" onClick={() => instance && onDecision(instance, 'attention')} disabled={!instance}>{text.markAttention}</button>
            </div>
        </aside>
    );
};

const RunHistoryManager = ({ apps, lang }: { apps: AppEntry[]; lang?: string }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [appFilter, setAppFilter] = useState('all');
    const [items, setItems] = useState<AppRunHistoryEntry[]>(() => loadAllAppRunHistory());
    const appNameById = useMemo(() => new Map(apps.map((app) => [app.id, app.name])), [apps]);
    const filteredItems = appFilter === 'all' ? items : items.filter((item) => item.appID === appFilter);
    return (
        <>
            <div className="apps-detail__header"><div><h2 className="apps-detail__title">{text.runHistoryManager}</h2><p className="apps-detail__subtitle">{text.runHistoryManagerHint}</p></div><button className="apps-secondary-button" type="button" onClick={() => setItems(loadAllAppRunHistory())}>{text.approvalRefresh}</button></div>
            <div className="apps-detail__body elegant-scrollbar">
                <section className="apps-run-history-manager">
                    <div className="apps-approval-manager__filters"><label><span>{text.approvalAppFilter}</span><select value={appFilter} onChange={(event) => setAppFilter(event.target.value)}><option value="all">{text.runHistoryAllApps}</option>{apps.map((app) => <option key={app.id} value={app.id}>{app.name}</option>)}</select></label></div>
                    {filteredItems.length === 0 ? <div className="apps-run-history__empty">{text.noGlobalRunHistory}</div> : (
                        <div className="apps-run-history__list">
                            {filteredItems.map((item) => <div className="apps-run-history__item" data-state={item.status} key={`${item.appID}-${item.runID}-${item.at}`}><div><strong>{appNameById.get(item.appID) || item.appID}</strong><span>{formatRunHistoryTime(item.at)} / {item.outputMode.toUpperCase()} / {item.runID}</span>{item.inputSummary && <code>{item.inputSummary}</code>}</div><div className="apps-run-history__side"><em>{item.message || item.status}</em>{(item.artifactURI || item.artifactPath) && <div className="apps-run-history__actions"><button className="apps-link-button" type="button" onClick={() => void openSkillRunArtifactFromUI(item.runID, item.artifactID || item.artifactURI || '', item.artifactPath || '', item.artifactDownloadState === 'remote')}>{item.artifactDownloadState === 'remote' ? text.downloadArtifact : text.openArtifact}</button><button className="apps-link-button" type="button" onClick={() => void revealSkillRunArtifactFromUI(item.runID, item.artifactID || item.artifactURI || '', item.artifactPath || '')}>{text.revealArtifact}</button></div>}</div></div>)}
                        </div>
                    )}
                </section>
            </div>
        </>
    );
};

const ApprovalWorkspace = ({ app, runState, businessEntity, businessAction, businessNote, backendInstances, approvalLoadState = 'idle', lang, text, style, layoutRegion, onRefresh, onDecision }: { app: AppEntry; runState: 'idle' | 'running' | 'done' | 'error' | 'cancelled'; businessEntity: string; businessAction: string; businessNote: string; backendInstances?: ApprovalInstanceView[]; approvalLoadState?: 'idle' | 'loading' | 'error'; lang?: string; text: typeof labels.zh; style?: CSSProperties; layoutRegion?: string; onRefresh?: (lane: ApprovalLaneFilter) => void | Promise<void>; onDecision?: (instance: ApprovalInstanceView, decision: 'approved' | 'rejected' | 'attention') => void | Promise<void> }) => {
    const [lane, setLane] = useState<ApprovalLaneFilter>('my_requests');
    const [selectedInstanceId, setSelectedInstanceId] = useState('');
    const fallbackInstances = buildApprovalInstances(app, runState, businessEntity, businessAction, businessNote, lang);
    const instances = backendInstances && backendInstances.length > 0 ? backendInstances : fallbackInstances;
    const visibleInstances = lane === 'all' ? instances : lane === 'handled' ? instances.filter((item) => item.status === 'approved' || item.status === 'rejected') : instances.filter((item) => item.lane === lane);
    const selected = visibleInstances.find((item) => item.id === selectedInstanceId) || visibleInstances[0];
    const resultContract = appResultContractForManifest(app);
    const lanes = approvalLanes(text);
    const countForLane = (key: typeof lanes[number]['key']) => key === 'all'
        ? instances.length
        : key === 'handled'
            ? instances.filter((item) => item.status === 'approved' || item.status === 'rejected').length
            : instances.filter((item) => item.lane === key).length;
    useEffect(() => {
        if (onRefresh) void onRefresh(lane);
    }, [lane, onRefresh]);
    const selectedOutputs = selected?.outputs || [];
    const selectedArtifacts = selected?.artifacts || [];
    const selectedPayloadEntries = selected?.resultPayload ? Object.entries(selected.resultPayload).filter(([, value]) => formatApprovalResultValue(value)) : [];
    const hasResultPackage = selectedOutputs.length > 0 || selectedArtifacts.length > 0 || selectedPayloadEntries.length > 0;
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
                                <span>{text.currentApprovalNode}: {item.currentNode} · {text.approvalResult}: {approvalStatusLabel(item.status, lang)}</span>
                                <small>{text.approvalInstanceId}: {item.id} · {item.updatedAt}</small>
                            </div>
                            <em>{approvalStatusLabel(item.status, lang)}</em>
                        </button>
                    ))}
                </div>
                <aside className="apps-approval-detail" aria-label={text.approvalTimeline}>
                    {selected ? (
                        <>
                            <div className="apps-approval-detail__head">
                                <strong>{selected.title}</strong>
                                <span>{text.currentApprovalNode}: {selected.currentNode}</span>
                            </div>
                            <dl className="apps-approval-facts">
                                <div><dt>{text.myRequests}</dt><dd>{selected.owner}</dd></div>
                                <div><dt>{text.pendingMyApproval}</dt><dd>{selected.approver}</dd></div>
                                <div><dt>{text.approvalResult}</dt><dd>{selected.result}</dd></div>
                                <div><dt>{text.workflowSkill}</dt><dd>{selected.workflowSkillID || '-'}</dd></div>
                                <div><dt>{text.dataSrvRecord}</dt><dd>{selected.recordID || '-'}</dd></div>
                                <div><dt>{text.approvalObjectRoleLabel}</dt><dd>{selected.objectRole || '-'}</dd></div>
                                <div><dt>{text.remoteApprovalLabel}</dt><dd>{selected.approvalID || '-'}</dd></div>
                                <div><dt>{text.businessStatusLabel}</dt><dd>{selected.businessStatus || '-'}</dd></div>
                                <div><dt>{text.resultStatusLabel}</dt><dd>{selected.resultStatus || approvalStatusLabel(selected.status, lang)}</dd></div>
                            </dl>
                            {selected.detailURL && (
                                <div className="apps-approval-detail__links">
                                    <a className="apps-link-button" href={selected.detailURL} target="_blank" rel="noreferrer">{text.viewFullWorkflow}</a>
                                </div>
                            )}
                            <ResultContractPreview contract={resultContract} lang={lang} />
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
                            <div className="apps-approval-timeline">
                                {(selected.events && selected.events.length > 0 ? selected.events : [
                                    { node: isZh(lang) ? '\u53d1\u8d77\u8282\u70b9' : 'Submit node' },
                                    { node: selected.currentNode },
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
                        <div className="apps-approval-empty" role="status">{text.noApprovalInstances}</div>
                    )}
                    <div className="apps-approval-actions" aria-label={text.approvalActions}>
                        <button className="apps-primary-button" type="button" onClick={() => selected && onDecision?.(selected, 'approved')} disabled={!selected}>{text.approve}</button>
                        <button className="apps-secondary-button" type="button" onClick={() => selected && onDecision?.(selected, 'rejected')} disabled={!selected}>{text.reject}</button>
                        <button className="apps-secondary-button" type="button" onClick={() => selected && onDecision?.(selected, 'attention')} disabled={!selected}>{text.markAttention}</button>
                    </div>
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

const AppRunOutput = ({ status, runState, resultText, businessResult, isTool, text, style, layoutRegion }: { status: SkillRunStatusView | null; runState: 'idle' | 'running' | 'done' | 'error' | 'cancelled'; resultText: string; businessResult?: BusinessOperationResultView | null; isTool: boolean; text: typeof labels.zh; style?: CSSProperties; layoutRegion?: string }) => {
    const artifacts = skillRunArtifacts(status);
    const runID = String(status?.run_id || '').trim();
    const hasArtifacts = artifacts.length > 0;
    const outputBlocks = skillRunOutputBlocks(status).filter((block) => skillRunOutputBlockText(block));
    const showTextOutput = runState === 'done' && !businessResult && (!isTool || (!hasArtifacts && outputBlocks.length === 0));
    return (
        <section className="apps-runtime-section apps-runtime-output" data-region={layoutRegion || 'right'} style={style}>
            <div className="apps-runtime-section__title">{text.runtimeOutput}</div>
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
            {outputBlocks.length > 0 && (
                <div className="apps-output-blocks">
                    {outputBlocks.map((block, index) => {
                        const blockKind = String(block.kind || 'text').trim().toLowerCase();
                        const blockText = skillRunOutputBlockText(block);
                        const blockTitle = String(block.title || (blockKind === 'error' ? 'Error' : text.outputContent)).trim();
                        const blockArtifact = block.artifact;
                        const blockArtifactRef = String(block.artifact_id || blockArtifact?.id || blockArtifact?.uri || '').trim();
                        const blockArtifactPath = String(blockArtifact?.path || '').trim();
                        const blockRemoteOnly = String(blockArtifact?.download_state || '').trim().toLowerCase() === 'remote' && !blockArtifactPath;
                        return (
                            <div className="apps-output-block" data-kind={blockKind} key={block.id || `${blockKind}-${index}`}>
                                <span>{blockTitle}</span>
                                {blockText && <pre>{blockText}</pre>}
                                {blockArtifactRef && (
                                    <div className="apps-output-block__actions">
                                        <button className="apps-link-button" type="button" onClick={() => void openSkillRunArtifactFromUI(runID, blockArtifactRef, blockArtifactPath, blockRemoteOnly)}>{blockRemoteOnly ? text.downloadArtifact : text.openArtifact}</button>
                                    </div>
                                )}
                            </div>
                        );
                    })}
                </div>
            )}
            {hasArtifacts ? (
                <div className="apps-run-artifacts">
                    {artifacts.map((artifact, index) => {
                        const artifactPath = String(artifact.path || '').trim();
                        const artifactID = String(artifact.id || '').trim();
                        const artifactURI = String(artifact.uri || '').trim();
                        const artifactStatus = String(artifact.status || (index === 0 ? status?.summary?.artifact_status : '') || '').trim();
                        const artifactName = String(artifact.name || artifactPath.split(/[\\/]/).pop() || '').trim();
                        const artifactDownloadState = String(artifact.download_state || '').trim().toLowerCase();
                        const artifactRemoteOnly = artifactDownloadState === 'remote' && !artifactPath;
                        const artifactMeta = [artifactName, artifact.mime_type, artifact.size_bytes ? `${artifact.size_bytes} bytes` : '', artifactRemoteOnly ? 'remote' : ''].filter(Boolean).join(' · ');
                        const artifactRef = artifactID || artifactURI;
                        const artifactDisplayRef = artifactURI || artifactName || artifactPath || String(artifact.remote_url || artifactID || '').trim();
                        const normalizedArtifactStatus = artifactStatus.toLowerCase();
                        const artifactStatusText = ['ready', 'success', 'succeeded', 'done', 'completed', 'complete'].includes(normalizedArtifactStatus)
                            ? text.artifactReady
                            : ['pending', 'waiting', 'running'].includes(normalizedArtifactStatus)
                                ? text.artifactPending
                                : artifactStatus;
                        const artifactLabel = artifactStatusText || (artifactRef || artifactPath ? text.artifactReady : artifactStatusLabel(status, text) || text.artifactPending);
                        return (
                            <div className="apps-run-artifact" key={`${artifactRef || artifactPath || artifactDisplayRef || 'artifact'}-${index}`}>
                                <span>{text.runArtifacts}</span>
                                <strong>{artifactLabel}</strong>
                                {artifactMeta && <span>{artifactMeta}</span>}
                                {artifactDisplayRef && <code>{artifactDisplayRef}</code>}
                                <div className="apps-run-artifact__actions">
                                    <button className="apps-link-button" type="button" disabled={!artifactRef && !artifactPath} onClick={() => void openSkillRunArtifactFromUI(runID, artifactRef, artifactPath, artifactRemoteOnly)}>{artifactRemoteOnly ? text.downloadArtifact : text.openArtifact}</button>
                                    <button className="apps-link-button" type="button" disabled={!artifactRef && !artifactPath} onClick={() => void revealSkillRunArtifactFromUI(runID, artifactRef, artifactPath)}>{text.revealArtifact}</button>
                                </div>
                            </div>
                        );
                    })}
                </div>
            ) : outputBlocks.length === 0 && showTextOutput ? (
                <div className="apps-output-text">
                    <span>{text.outputText}</span>
                    <pre>{resultText}</pre>
                </div>
            ) : outputBlocks.length === 0 ? (
                <div className="apps-output-empty">{artifactStatusLabel(status, text) || text.noOutputYet}</div>
            ) : null}
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

function appToManifest(app: AppEntry, submission?: AppPublishSubmission, governanceOverrides: AppGovernanceOverrides = {}) {
    const manifest = app.manifest;
    return {
        schema: manifest?.schema || 'maclaw.app.v1',
        privateMarker: manifest?.privateMarker || 'x_maclaw_apps',
        installUnit: manifest?.installUnit || 'builtin',
        app: {
            id: app.id,
            name: app.name,
            version: normalizeAppVersion(app.version),
            description: app.description,
            category: app.category,
            kind: app.kind,
            icon: app.icon,
            customIconDataUrl: app.customIconDataUrl,
            source: app.source,
            importedRunEvidence: app.importedRunEvidence,
            launchMode: manifest?.launchMode || defaultLaunchModeForKind(app.kind),
            binding: {
                datasrv: manifest?.datasrv,
                mis: manifest?.mis,
                skill: manifest?.skill,
                appSkill: manifest?.appSkill,
                dependencies: manifest?.dependencies,
                ui: manifest?.ui,
                resultContract: manifest?.resultContract,
                workflow: manifest?.workflow,
            },
            panel: {
                pinned: !!app.pinned,
                accent: app.accent,
                customIconDataUrl: app.customIconDataUrl,
            },
            governance: appGovernanceForManifest(app, submission, governanceOverrides),
        },
    };
}

function skillDefinitionAppId(app: AppEntry): string {
    const skillID = String(app.manifest?.skill?.id || '').trim();
    const prefixedID = skillID ? `skill-app-${skillID}-` : '';
    return prefixedID && app.id.startsWith(prefixedID) ? app.id.slice(prefixedID.length) : app.id;
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
    return {
        id: id.startsWith('market-') ? id : `market-${id}`,
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
            ui: normalizeAppWorkspaceLayout(app.binding?.ui, kind),
            resultContract: normalizeAppResultContract(app.binding?.resultContract || app.governance?.resultContract, kind, app.binding?.skill?.outputModes || []),
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

function dataSrvRegistrationNumber(value: unknown) {
    const numberValue = Number(value || 0);
    return Number.isFinite(numberValue) ? numberValue : 0;
}

function dataSrvRegistrationSummary(registration: BackendAppDataSrvRegistration | undefined | null, text: typeof labels.zh) {
    const eligibleCount = dataSrvRegistrationNumber(registration?.eligible_count);
    if (!registration || eligibleCount <= 0) return '';
    const syncedCount = dataSrvRegistrationNumber(registration.synced_count);
    const reason = String(registration.reason || '').trim();
    if (registration.synced) return `${text.datasrvRegistrationReady}: ${syncedCount || eligibleCount}/${eligibleCount}`;
    if (syncedCount > 0) return `${text.datasrvRegistrationFailed}: ${syncedCount}/${eligibleCount}${reason ? ` · ${reason}` : ''}`;
    return `${text.datasrvRegistrationSkipped}: ${reason || `${syncedCount}/${eligibleCount}`}`;
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

function backendDependenciesForApp(plan: BackendAppInstallPlan | null, appId: string) {
    return (plan?.dependencies || []).filter((dep) => backendDependencyMatchesAppIDs(dep, [appId]));
}

function isBlockingBackendDependency(dep: BackendAppInstallDependency) {
    if (dep.required === false) return false;
    const action = String(dep.action || '').trim();
    const health = String(dep.health || '').trim();
    if (action === 'blocked' || action === 'failed') return true;
    if (!dep.installed) return true;
    return !!health && health !== 'ready';
}

function hasMissingRequiredBackendDependency(plan: BackendAppInstallPlan | null, appIds: string[]) {
    if (appIds.length === 0) return false;
    const selected = new Set(appIds);
    return (plan?.dependencies || []).some((dep) => isBlockingBackendDependency(dep) && backendDependencyMatchesAppIDs(dep, appIds));
}
function firstBlockingBackendDependencyForApp(plan: BackendAppInstallPlan | null, appId: string) {
    const appDependencies = backendDependenciesForApp(plan, appId);
    const candidates = appDependencies.length > 0 ? appDependencies : plan?.dependencies || [];
    return candidates.find(isBlockingBackendDependency) || null;
}

function backendDependencyUnavailableMessage(app: AppEntry, plan: BackendAppInstallPlan | null, text: typeof labels.zh, lang?: string) {
    const dep = firstBlockingBackendDependencyForApp(plan, app.id);
    if (!dep?.id) return text.missingRequiredDependency;
    const zh = isZh(lang);
    const rawState = String(dep.installed_status || dep.health || dep.action || '').trim();
    if (zh) {
        const reason = dep.installed ? (rawState ? '\u672a\u5b89\u88c5\u6216\u5df2\u505c\u7528' : '\u4e0d\u53ef\u7528') : '\u672a\u5b89\u88c5';
        return app.name + '\u6682\u4e0d\u53ef\u7528\uff1a' + dep.id + ' ' + reason + '\u3002';
    }
    const reason = dep.installed ? (rawState ? 'is missing or disabled' : 'is unavailable') : 'is not installed';
    return app.name + ' is unavailable: ' + dep.id + ' ' + reason + '.';
}

function backendDependencySummary(dep: BackendAppInstallDependency, text: typeof labels.zh) {
    const health = String(dep.health || '').trim();
    const status = dep.installed && health && health !== 'ready'
        ? text.unavailableDependency
        : dep.installed ? text.installedDependency : text.missingDependency;
    const version = dep.version ? `@${dep.version}` : '';
    const required = dep.required === false ? '' : ' *';
    const healthDetail = dep.installed && health && health !== 'ready' ? ` (${dep.installed_status || health})` : '';
    return `${status}: ${dep.id}${version}${required}${healthDetail}`;
}
function installRecordMissingDependencyCount(record: BackendAppInstallRecord) {
    return (record.dependencies || []).filter(isBlockingBackendDependency).length;
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

const DependencyVerificationPanel = ({ plan, state, error, selectedAppIDs, text }: { plan: BackendAppInstallPlan | null; state: 'idle' | 'loading' | 'ready' | 'error'; error?: string; selectedAppIDs?: string[]; text: typeof labels.zh }) => {
    if (state === 'idle' && !plan) return null;
    const appIDs = selectedAppIDs || [];
    const dependencies = appIDs.length > 0
        ? (plan?.dependencies || []).filter((dep) => backendDependencyMatchesAppIDs(dep, appIDs))
        : plan?.dependencies || [];
    const blockingCount = dependencies.filter(isBlockingBackendDependency).length;
    const dependencyCount = dependencies.length;
    const hasBlockingDependency = blockingCount > 0 || !!plan?.has_blocking_dependency || !!plan?.has_missing_required;
    const status = state === 'loading'
        ? text.dependencyPlanLoading
        : state === 'error'
            ? [text.dependencyPlanError, error].filter(Boolean).join(': ')
            : hasBlockingDependency
                ? text.dependencyVerificationBlocked
                : text.dependencyVerificationReady;
    return (
        <div className="apps-dependency-verification" data-state={state === 'error' || hasBlockingDependency ? 'blocked' : state} role={state === 'error' || hasBlockingDependency ? 'alert' : 'group'} aria-label={text.dependencyVerification}>
            <div className="apps-dependency-verification__head">
                <strong>{text.dependencyVerification}</strong>
                <span>{status}</span>
                {state === 'ready' && <em>{text.skillDependencies}: {dependencyCount} · {text.missingDependencyCount}: {blockingCount}</em>}
            </div>
            {dependencyCount > 0 && <InstallRecordDependencies dependencies={dependencies} text={text} />}
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
                const meta = [
                    dep.kind || 'skill',
                    dep.source,
                    dep.version ? `v${dep.version}` : '',
                    state !== 'ready' ? installedStatus || health : '',
                ].filter(Boolean).join(' · ');
                return (
                    <div className="apps-install-record__dep" data-state={state} role="listitem" key={`${dep.id}-${dep.version || ''}-${index}`} title={backendDependencySummary(dep, text)}>
                        <strong>{dep.id}</strong>
                        {meta && <span>{meta}</span>}
                        <em>{installRecordDependencyStatus(dep, text)}</em>
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
    return Array.from(new Set(keys));
}

const AppStudio = ({ apps, hiddenApps, lang, tab, setTab, onClose, onTogglePin, onUpdateApp, onDuplicateApp, onMoveApp, onRemoveApp, onRestoreApp, pendingEditAppId, onPendingEditConsumed, datasrvDiscovery, skillDiscovery, onAddDiscoveredApp, onCreateApp, onInstallMarketApp, onEditApp }: {
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
    onRemoveApp: (appId: string) => void;
    onRestoreApp: (appId: string) => void;
    pendingEditAppId: string;
    onPendingEditConsumed: () => void;
    datasrvDiscovery: DataSrvDiscovery;
    skillDiscovery: SkillAppDiscovery;
    onAddDiscoveredApp: (app: AppEntry) => void;
    onCreateApp: (app: AppEntry, options?: { keepStudioCreate?: boolean }) => void;
    onInstallMarketApp: (app: AppEntry) => void;
    onEditApp: (appId: string) => void;
}) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const studioTabs: Array<{ id: StudioTab; label: string }> = [
        { id: 'create', label: text.createTab },
        { id: 'manage', label: text.manageTab },
        { id: 'market', label: text.marketTab },
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
    return (
        <>
            <div className="apps-detail__header">
                <div className="apps-detail__heading">
                    <h2 className="apps-detail__title">{text.appStudio}</h2>
                    <p className="apps-detail__subtitle">{text.studioSubtitle}</p>
                </div>
                <div className="apps-detail__actions">
                    <DataSrvDiscoverySummary discovery={datasrvDiscovery} lang={lang} />
                    <button className="apps-secondary-button" type="button" onClick={onClose}>{isZh(lang) ? '\u5173\u95ed' : 'Close'}</button>
                </div>
            </div>
            <div className="apps-detail__body elegant-scrollbar">
                <div className="apps-preview">
                    <DataSrvDiscoveryPanel discovery={datasrvDiscovery} apps={apps} lang={lang} onAddApp={onAddDiscoveredApp} />
                    <SkillDiscoveryPanel discovery={skillDiscovery} apps={apps} lang={lang} />
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
                        role="tabpanel"
                        id={getStudioPanelId(tab)}
                        aria-labelledby={getStudioTabId(tab)}
                    >
                        {tab === 'create' && <CreateAppPane lang={lang} onCreateApp={onCreateApp} />}
                        {tab === 'manage' && <ManageAppsPane apps={apps} hiddenApps={hiddenApps} lang={lang} onTogglePin={onTogglePin} onUpdateApp={onUpdateApp} onDuplicateApp={onDuplicateApp} onMoveApp={onMoveApp} onRemoveApp={onRemoveApp} onRestoreApp={onRestoreApp} pendingEditAppId={pendingEditAppId} onPendingEditConsumed={onPendingEditConsumed} />}
                        {tab === 'market' && <MarketPane apps={apps} lang={lang} onInstallApp={onInstallMarketApp} />}
                        {tab === 'publish' && <PublishPane apps={apps} lang={lang} onFixApp={onEditApp} />}
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

const DataSrvDiscoverySummary = ({ discovery, lang }: { discovery: DataSrvDiscovery; lang?: string }) => {
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
    const meta = discovery.error ? `${endpoint} · ${discovery.error}` : endpoint;
    return (
        <div className="apps-datasrv-summary" data-status={discovery.status} aria-label={text.datasrvDiscovery}>
            <div className="apps-datasrv-summary__main">
                <span className="apps-datasrv-summary__title">{text.datasrvDiscovery}</span>
                <span className="apps-datasrv-summary__status">{statusLabel}</span>
            </div>
            <div className="apps-datasrv-summary__metrics" aria-label={zh ? '\u80fd\u529b\u7edf\u8ba1' : 'Capability counts'}>
                {metrics.map((metric) => (
                    <span key={metric.label}><strong>{metric.value}</strong>{metric.label}</span>
                ))}
            </div>
            <div className="apps-datasrv-summary__meta" title={meta}>
                {meta}
            </div>
        </div>
    );
};

const SkillDiscoveryPanel = ({ discovery, apps, lang }: { discovery: SkillAppDiscovery; apps: AppEntry[]; lang?: string }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    if (discovery.candidates.length === 0 && discovery.status !== 'error') return null;
    const installedIds = new Set(apps.map((app) => app.id));
    const zh = isZh(lang);
    const foundCount = discovery.candidates.length;
    const statusLabel = discovery.status === 'loading' ? text.datasrvLoading :
        discovery.status === 'error' ? text.datasrvError :
            discovery.status === 'ready' ? (zh ? `${foundCount} \u4e2a\u5e94\u7528` : `${foundCount} ${foundCount === 1 ? 'app' : 'apps'}`) : '-';
    const metaText = discovery.status === 'error' ? text.skillAppsErrorMeta : text.skillAppsMeta;
    return (
        <section className="apps-discovery" data-status={discovery.status === 'ready' ? 'ready' : discovery.status}>
            <div>
                <div className="apps-discovery__title">{text.skillApps}</div>
                <div className="apps-discovery__meta">{metaText}</div>
                {discovery.error && <div className="apps-discovery__error">{discovery.error}</div>}
            </div>
            <div className="apps-discovery__status">{statusLabel}</div>
            {discovery.candidates.length > 0 && (
                <div className="apps-discovery__candidates">
                    {discovery.candidates.map((candidate) => {
                        const installed = installedIds.has(candidate.id);
                        return (
                            <div className="apps-discovery__candidate" key={candidate.id}>
                                <span className="apps-app-icon" style={{ '--apps-icon-color': candidate.accent } as CSSProperties}><AppIcon icon={candidate.icon} customIconDataUrl={candidate.customIconDataUrl} /></span>
                                <div>
                                    <strong>{candidate.name}</strong>
                                    <span>{candidate.category}</span>
                                </div>
                                <span className="apps-discovery__candidate-state" data-state={installed ? 'synced' : 'syncing'}>
                                    {installed ? text.inPanel : text.addingToPanel}
                                </span>
                            </div>
                        );
                    })}
                </div>
            )}
        </section>
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
    if (normalized.includes('market')) return 'market';
    if (normalized.includes('hub')) return 'hub';
    if (normalized.includes('builtin')) return 'builtin';
    return 'local';
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
                        ? (zh ? '\u641c\u5ba1\u6279\u5de5\u4f5c\u6d41 / Hub' : 'Search approval workflows / Hub')
                        : (zh ? '\u641c\u5df2\u5b89\u88c5 / SkillMarket' : 'Search installed / SkillMarket'))}
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
                {(visibleMarket.length > 0 || marketEmpty || currentQueryIsSearching || (marketSearchIsCurrent && searchState === 'error')) && <div className="apps-skill-picker__group apps-skill-picker__group--market">{zh ? 'SkillMarket / Hub' : 'SkillMarket / Hub'}</div>}
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
                <strong>{contract.primary}</strong>
            </div>
            <div className="apps-result-contract__chips" aria-label={zh ? '\u7ed3\u679c\u7c7b\u578b' : 'Result types'}>
                {contract.types.map((type) => <span key={type}>{type}</span>)}
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
                        {typeOptions.map((type) => <option value={type} key={type}>{type}</option>)}
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
    const [resultContractDraft, setResultContractDraft] = useState<AppResultContract | undefined>(undefined);
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
        setResultContractDraft(undefined);
        setUiNavigation(defaultEnterpriseNavigation(nextKind));
        setUiColumns(defaultEnterpriseColumns(nextKind));
        setCopyState('idle');
        setSkillAppSaveState('idle');
        setSkillAppSaveMessage('');
        setSkillMarketUploadState('idle');
        setSelectedSkillSource('installed');
        setLayoutTemplate(nextKind === 'tool_app' ? 'document_workspace' : 'classic_split');
        setLayoutDensity('comfortable');
        setPrimaryRegion('left');
        setOutputRegion(nextKind === 'tool_app' ? 'right' : 'bottom');
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
        setLayoutTemplate(nextKind === 'tool_app' ? 'document_workspace' : /nav|menu|\u5bfc\u822a/.test(lower) ? 'left_nav' : 'classic_split');
        setLayoutDensity(/compact|dense|\u7d27\u51d1/.test(lower) ? 'compact' : 'comfortable');
        setPrimaryRegion(nextKind === 'tool_app' ? 'left' : 'center');
        setOutputRegion(/modal|dialog|\u5f39\u7a97/.test(lower) ? 'modal' : nextKind === 'tool_app' ? 'right' : 'bottom');
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
        setResultContractDraft(undefined);
        setUiNavigation(defaultEnterpriseNavigation(nextKind));
        setUiColumns(defaultEnterpriseColumns(nextKind));
        setDescription(prompt);
        setCopyState('idle');
    };
    const buildDraftApp = (id: string, cleanName = name.trim() || (zh ? '\u672a\u547d\u540d\u5e94\u7528' : 'Untitled app'), boundSkillID = id, appDefinitionFile = 'maclaw.apps.json'): AppEntry => {
        const enterpriseAppSkillID = isEnterpriseAppKind(kind) ? (appSkillID.trim() || `${id}-app`) : '';
        const workflowDependency: AppSkillDependency[] = isEnterpriseApprovalAppKind(kind) && workflowSkillID.trim()
            ? [{ id: workflowSkillID.trim(), version: workflowSkillVersion.trim() || undefined, kind: 'workflow_skill', required: true, source: dependencySource || 'hub', capabilities: ['approval.workflow'] }]
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
            manifest: applyAppResultContract(applyEnterpriseUIConfig(applyAppWorkflowMapping(applyStudioWorkspaceLayout(manifest, kind, { template: layoutTemplate, density: layoutDensity, primaryRegion, outputRegion }), kind, workflowMapping), kind, uiNavigation, uiColumns), kind, resultContractDraft),
        };
    };
    const draftApp = buildDraftApp(
        'draft-app',
        undefined,
        kind === 'tool_app' && selectedSkill ? selectedSkill : 'draft-app',
        kind === 'tool_app' && selectedSkill ? 'maclaw.app.json' : 'maclaw.apps.json',
    );
    const draftResultContract = normalizeAppResultContract(resultContractDraft, kind, draftApp.manifest?.skill?.outputModes || outputModes);
    const draftManifestText = JSON.stringify(appToManifest(draftApp), null, 2);
    const studioLayoutValue: RuntimeWorkspaceLayout = { template: layoutTemplate, density: layoutDensity, primaryRegion, outputRegion };
    const updateStudioLayout = (layout: RuntimeWorkspaceLayout) => {
        setLayoutTemplate(layout.template);
        setLayoutDensity(layout.density);
        setPrimaryRegion(layout.primaryRegion);
        setOutputRegion(layout.outputRegion);
    };
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
        setSkillFields([]);
        setMultipleFiles(false);
        setResultContractDraft(undefined);
        setUiNavigation(defaultEnterpriseNavigation(kind));
        setUiColumns(defaultEnterpriseColumns(kind));
    };
    const persistSkillAppDefinition = async () => {
        const cleanName = name.trim();
        const skillID = selectedSkill.trim();
        if (!cleanName || !skillID || kind !== 'tool_app' || selectedSkillSource !== 'installed') return null;
        const appID = makeSkillAppDefinitionId(cleanName);
        const app = buildDraftApp(appID, cleanName, skillID, 'maclaw.app.json');
        const manifestText = JSON.stringify(appToManifest(app), null, 2);
        await SaveMaclawAppDefinitionForSkill(skillID, manifestText);
        onCreateApp({
            ...app,
            id: skillPanelAppID(skillID, appID),
            source: 'skill',
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
        const skillID = selectedSkill.trim();
        if (!skillID || skillMarketUploadState === 'uploading') return;
        if (selectedSkillSource !== 'installed') {
            setSkillMarketUploadState('error');
            setSkillAppSaveState('error');
            setSkillAppSaveMessage(zh ? '\u8bf7\u5148\u9009\u62e9\u5df2\u5b89\u88c5 Skill\uff0c\u518d\u4fdd\u5b58\u6216\u4e0a\u4f20\u5e94\u7528\u5b9a\u4e49\u3002' : 'Choose an installed Skill before saving or uploading this app definition.');
            return;
        }
        const cleanName = name.trim();
        if (!cleanName) return;
        const appID = makeSkillAppDefinitionId(cleanName);
        const panelApp: AppEntry = {
            ...buildDraftApp(appID, cleanName, skillID, 'maclaw.app.json'),
            id: skillPanelAppID(skillID, appID),
            source: 'skill',
        };
        if (!latestAppRunEvidence(panelApp)) {
            setSkillMarketUploadState('error');
            setSkillAppSaveState('error');
            setSkillAppSaveMessage(zh ? '\u8bf7\u5148\u4fdd\u5b58\u5230 Skill\uff0c\u5e76\u5728\u5e94\u7528\u9762\u677f\u6210\u529f\u6d4b\u8bd5\u4e00\u6b21\u5f53\u524d\u7248\u672c\uff0c\u518d\u4e0a\u4f20\u5230 SkillMarket\u3002' : 'Save to Skill and run this version successfully in the app panel before uploading to SkillMarket.');
            return;
        }
        setSkillMarketUploadState('uploading');
        setSkillAppSaveMessage('');
        try {
            setSkillAppSaveState('saving');
            const savedSkillID = await persistSkillAppDefinition();
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
                            <span className="apps-count">appSkill / workflow_skill</span>
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
                                </>
                            )}
                        </div>
                    </section>
                )}
                {isEnterpriseApprovalAppKind(kind) && <WorkflowMappingDesigner value={workflowMapping} onChange={setWorkflowMapping} lang={lang} />}
                {isEnterpriseAppKind(kind) && <EnterpriseUIConfigDesigner kind={kind} navigation={uiNavigation} columns={uiColumns} onNavigationChange={setUiNavigation} onColumnsChange={setUiColumns} lang={lang} testIdPrefix="studio" />}
                <StudioLayoutDesigner kind={kind} value={studioLayoutValue} onChange={updateStudioLayout} lang={lang} />
                <ResultContractDesigner contract={draftResultContract} onChange={setResultContractDraft} lang={lang} testIdPrefix="studio" />
                <div className="apps-form-row apps-form-row--description">
                    <label>{zh ? '\u63cf\u8ff0' : 'Description'}</label>
                    <textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder={zh ? '\u7528\u4e8e tooltip \u548c\u53f3\u4fa7\u8fd0\u884c\u533a\u8bf4\u660e' : 'Used in tooltip and right runtime area'} />
                </div>
                <div className="apps-actions">
                    <button className="apps-primary-button" type="button" disabled={!name.trim()} onClick={createApp}>{text.createTab}</button>
                    {kind === 'tool_app' && (
                        <>
                            <button className="apps-secondary-button" type="button" disabled={!name.trim() || !selectedSkill.trim() || selectedSkillSource !== 'installed' || skillAppSaveState === 'saving'} onClick={() => void saveAsSkillApp()}>
                                {skillAppSaveState === 'saving' ? (zh ? '\u4fdd\u5b58\u4e2d...' : 'Saving...') : (zh ? '\u4fdd\u5b58\u5230 Skill' : 'Save to Skill')}
                            </button>
                            <button className="apps-secondary-button" type="button" disabled={!selectedSkill.trim() || selectedSkillSource !== 'installed' || skillMarketUploadState === 'uploading'} onClick={() => void uploadSelectedSkillApp()}>
                                {skillMarketUploadState === 'uploading' ? (zh ? '\u4e0a\u4f20\u4e2d...' : 'Uploading...') : (zh ? '\u4e0a\u4f20\u5230 SkillMarket' : 'Upload to SkillMarket')}
                            </button>
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
    const evidence = latestAppRunEvidence(app);
    const resultCoverage = appRunEvidenceContractCoverage(app, evidence, lang);
    const dependencies = appDependencyEvidence(app);
    const hasRequiredDependencies = app.kind === 'automation_app' || dependencies.some((dep) => dep.required && dep.id);
    const workspaceLayout = appWorkspaceLayoutEvidence(app);
    const hasWorkspaceLayout = workspaceLayout.schema === 'maclaw.app.ui.v1' && !!workspaceLayout.entry && workspaceLayout.regionCount > 0;
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
            ok: hasRequiredDependencies,
            detail: appDependencyPublishSummary(app, lang),
        },
        {
            label: zh ? 'Workspace layout' : 'Workspace layout',
            ok: hasWorkspaceLayout,
            detail: appWorkspaceLayoutPublishSummary(app, lang),
        },
        {
            label: zh ? '\u7ed3\u679c\u5951\u7ea6' : 'Result contract',
            ok: appResultContractForManifest(app).types.length > 0,
            detail: appResultContractPublishSummary(app, lang),
        },
        {
            label: zh ? '\u8fd0\u884c\u8bc1\u636e' : 'Run evidence',
            ok: resultCoverage.ok,
            detail: resultCoverage.detail,
        },
    ];
}

const PublishPane = ({ apps, lang, onFixApp }: { apps: AppEntry[]; lang?: string; onFixApp: (appId: string) => void }) => {
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
    const [queueDetailOpenId, setQueueDetailOpenId] = useState('');
    const [queueDetailLoadingId, setQueueDetailLoadingId] = useState('');
    const [queueDetailRecords, setQueueDetailRecords] = useState<Record<string, Record<string, unknown>>>({});
    const publishApps = apps.filter((app) => app.source === 'local');
    const packageText = JSON.stringify(appsToPackManifest(publishApps, submissions), null, 2);
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
            dependencyPlan = await PlanMaclawAppInstall(JSON.stringify(appToManifest(app)));
            if (dependencyPlan?.has_blocking_dependency || hasMissingRequiredBackendDependency(dependencyPlan, [app.id])) {
                throw new Error(text.missingRequiredDependency);
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
            submission = await submitAppPackageToEnterpriseMarket(app, appsToPackManifest([app], {}, dependencyPlan ? { [app.id]: { dependencyVerification: dependencyPlan } } : {}));
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
                    disabled={publishApps.length === 0}
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
                        const checks = buildPublishChecks(app, lang);
                        const ready = checks.every((item) => item.ok);
                        const submission = submissions[app.id];
                        const submissionStatus = submission ? publishSubmissionStatusLabel(submission, text) : '';
                        const canResubmit = submission?.status === 'review_failed' || !!submission?.modifiedAt;
                        const canWithdraw = !!submission && ['submitted', 'review_failed', 'approved'].includes(submission.status);
                        const isSubmitting = submittingAppId === app.id;
                        const submitError = submitErrors[app.id] || '';
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
                                    </div>
                                )}
                                {submitError && (
                                    <div className="apps-publish-submission apps-publish-submission--error" role="alert">
                                        <strong>{text.dependencyPlanError}</strong>
                                        <span>{submitError}</span>
                                    </div>
                                )}
                                <div className="apps-publish-checks">
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
                                    {submission?.reviewIssues && submission.reviewIssues.length > 0 && (
                                        <button className="apps-secondary-button" type="button" onClick={() => onFixApp(app.id)}>
                                            {text.fixReviewIssue}
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
                                const detailEvents = eventSummariesFromRecord(detailRecord);
                                const detailOpen = queueDetailOpenId === item.submissionID;
                                const dependencyCount = item.dependencies.length;
                                const missingDependencyCount = item.dependencies.filter(isBlockingBackendDependency).length;
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
                                        </div>
                                        <div className="apps-publish-queue__tools">
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
                                                {item.reviewIssues.length > 0 && <span>{text.reviewIssues}: {reviewIssuesSummary(item.reviewIssues, text)}</span>}
                                                {detailEvents.length > 0 && <span>{text.queueDetailEvents}: {detailEvents.join(' / ')}</span>}
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

type AppEditDraft = Pick<AppEntry, 'name' | 'description' | 'category' | 'icon' | 'customIconDataUrl' | 'accent'> & {
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
    approvalEvent: string;
    approvalObjectRole: string;
    workflowMapping: AppWorkflowMapping;
    inputMode: SkillInputMode;
    multipleFiles: boolean;
    outputModes: string[];
    fields: SkillAppField[];
    layout: RuntimeWorkspaceLayout;
    resultContract: AppResultContract;
    uiNavigation: string[];
    uiColumns: string[];
};

const ManageAppsPane = ({ apps, hiddenApps, lang, onTogglePin, onUpdateApp, onDuplicateApp, onMoveApp, onRemoveApp, onRestoreApp, pendingEditAppId, onPendingEditConsumed }: {
    apps: AppEntry[];
    hiddenApps: AppEntry[];
    lang?: string;
    onTogglePin: (appId: string) => void;
    onUpdateApp: (appId: string, patch: Partial<AppEntry>) => void;
    onDuplicateApp: (appId: string) => void;
    onMoveApp: (appId: string, direction: AppMoveTarget) => void;
    onRemoveApp: (appId: string) => void;
    onRestoreApp: (appId: string) => void;
    pendingEditAppId: string;
    onPendingEditConsumed: () => void;
}) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [manifestAppId, setManifestAppId] = useState('');
    const [editingAppId, setEditingAppId] = useState('');
    const emptyEditDraft = useMemo<AppEditDraft>(() => ({ name: '', description: '', category: '', icon: 'contract', customIconDataUrl: undefined, accent: defaultAccentForKind('tool_app'), businessDomain: 'business', businessObjectRole: 'record', businessPreferredAction: '', businessPreferredView: '', businessPreferredReport: '', businessPreferredDashboard: '', skillID: '', skillSource: 'local', appSkillID: '', appSkillSource: 'local', appSkillVersion: '', workflowSkillID: '', workflowSkillSource: 'hub', workflowSkillVersion: '', approvalEvent: '', approvalObjectRole: '', workflowMapping: defaultAppWorkflowMapping('enterprise_approval_app') as AppWorkflowMapping, inputMode: 'file', multipleFiles: false, outputModes: ['docx', 'pdf'], fields: [], layout: defaultRuntimeWorkspaceLayout('tool_app'), resultContract: buildAppResultContract('tool_app', ['docx', 'pdf']), uiNavigation: defaultEnterpriseNavigation('tool_app'), uiColumns: defaultEnterpriseColumns('tool_app') }), []);
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
    const manageCategories = useMemo(() => Array.from(new Set([...apps, ...hiddenApps].map((app) => app.category))), [apps, hiddenApps]);
    const normalizedManageQuery = manageQuery.trim().toLowerCase();
    const manageQueryMatchedApps = useMemo(() => [...apps, ...hiddenApps].filter((app) => !normalizedManageQuery || buildAppSearchText(app, lang).includes(normalizedManageQuery)), [apps, hiddenApps, normalizedManageQuery, lang]);
    const manageCategoryCounts = useMemo(() => countAppsByCategory(manageQueryMatchedApps), [manageQueryMatchedApps]);
    const manageFilterActive = normalizedManageQuery.length > 0 || manageCategory !== 'all';
    useEffect(() => {
        if (!normalizedManageQuery || manageCategory === 'all') return;
        if ((manageCategoryCounts.get(manageCategory) || 0) === 0) setManageCategory('all');
    }, [normalizedManageQuery, manageCategory, manageCategoryCounts]);
    const matchesManageFilter = (app: AppEntry) => {
        const categoryMatches = manageCategory === 'all' || app.category === manageCategory;
        const queryMatches = !normalizedManageQuery || buildAppSearchText(app, lang).includes(normalizedManageQuery);
        return categoryMatches && queryMatches;
    };
    const managedApps = useMemo(() => apps.filter(matchesManageFilter), [apps, manageCategory, normalizedManageQuery, lang]);
    const filteredHiddenApps = useMemo(() => hiddenApps.filter(matchesManageFilter), [hiddenApps, manageCategory, normalizedManageQuery, lang]);
    const editingApp = useMemo(() => apps.find((item) => item.id === editingAppId) || null, [apps, editingAppId]);
    const manageMatchCount = managedApps.length + filteredHiddenApps.length;
    const manageTotalCount = apps.length + hiddenApps.length;
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
            approvalEvent: workflowBinding?.event || (app.manifest?.datasrv?.domain ? `${app.manifest.datasrv.domain}.submitted` : ''),
            approvalObjectRole: workflowBinding?.objectRole || app.manifest?.datasrv?.domain || '',
            workflowMapping: normalizeAppWorkflowMapping(app.manifest?.workflow, app.kind, app.manifest?.datasrv?.domain || 'business', workflowBinding?.objectRole || app.manifest?.datasrv?.objectRole || app.manifest?.datasrv?.domain || 'record') || (defaultAppWorkflowMapping('enterprise_approval_app') as AppWorkflowMapping),
            inputMode: app.manifest?.skill?.inputMode || 'file',
            multipleFiles: !!app.manifest?.skill?.multipleFiles,
            outputModes: normalizeOutputModes(app.manifest?.skill?.outputModes),
            fields: normalizeSkillAppFields(app.manifest?.skill?.fields),
            layout: runtimeWorkspaceLayoutForApp(app),
            resultContract: appResultContractForManifest(app),
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
            const existingSkills = manifest.dependencies?.skills || [];
            const nonWorkflowSkills = existingSkills.filter((dependency) => dependency.kind !== 'workflow_skill');
            const existingWorkflow = existingSkills.find((dependency) => dependency.kind === 'workflow_skill' && dependency.id);
            const workflowDependency: AppSkillDependency | undefined = workflowSkillID ? {
                ...(existingWorkflow || {}),
                id: workflowSkillID,
                version: workflowVersion,
                kind: 'workflow_skill',
                required: existingWorkflow?.required !== false,
                source: editDraft.workflowSkillSource || existingWorkflow?.source || 'hub',
                capabilities: existingWorkflow?.capabilities?.length ? existingWorkflow.capabilities : ['approval.workflow'],
            } : undefined;
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
            manifest = applyAppResultContract(applyEnterpriseUIConfig(applyAppWorkflowMapping(applyStudioWorkspaceLayout(manifest, app.kind, editDraft.layout), app.kind, editDraft.workflowMapping), app.kind, editDraft.uiNavigation, editDraft.uiColumns), app.kind, editDraft.resultContract);
        }
        const updatedApp: AppEntry = {
            ...app,
            name,
            category: editDraft.category.trim() || (isZh(lang) ? '\u672a\u5206\u7c7b' : 'Uncategorized'),
            description: editDraft.description.trim(),
            icon: editDraft.icon,
            customIconDataUrl: normalizeCustomIconDataUrl(editDraft.customIconDataUrl),
            accent: editDraft.accent,
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
            version: updatedApp.version,
            manifest: updatedApp.manifest,
        };
        const skillID = String(updatedApp.manifest?.skill?.id || '').trim();
        const appDefinitionFile = String(updatedApp.manifest?.skill?.appDefinitionFile || '').trim();
        const shouldWriteSkillDefinition = updatedApp.source === 'skill' && updatedApp.kind === 'tool_app' && !!skillID && (appDefinitionFile === 'maclaw.app.json' || appDefinitionFile === 'maclaw.apps.json');
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
                <button
                    className="apps-secondary-button"
                    type="button"
                    disabled={!manageFilterActive}
                    title={text.resetFilter}
                    onClick={() => {
                        setManageQuery('');
                        setManageCategory('all');
                    }}
                >{text.reset}</button>
            </div>
            {manageFilterSummary && <div className="apps-filter-summary apps-filter-summary--manage" aria-live="polite">{manageFilterSummary}</div>}
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
                            <div className="apps-manage-row__desc">{app.category} · {sourceLabels[app.source][isZh(lang) ? 'zh' : 'en']}</div>
                        </div>
                        <div className="apps-manage-actions">
                            <button className="apps-icon-button" type="button" disabled={manageFilterActive || index === 0} title={manageFilterActive ? text.clearFilterToSort : text.moveTop} onClick={() => onMoveApp(app.id, "top")}>Top</button>
                            <button className="apps-icon-button" type="button" disabled={manageFilterActive || index === 0} title={manageFilterActive ? text.clearFilterToSort : text.moveUp} onClick={() => onMoveApp(app.id, -1)}>Up</button>
                            <button className="apps-icon-button" type="button" disabled={manageFilterActive || index === apps.length - 1} title={manageFilterActive ? text.clearFilterToSort : text.moveDown} onClick={() => onMoveApp(app.id, 1)}>Down</button>
                            <button className="apps-icon-button" type="button" disabled={manageFilterActive || index === apps.length - 1} title={manageFilterActive ? text.clearFilterToSort : text.moveBottom} onClick={() => onMoveApp(app.id, "bottom")}>Bottom</button>
                            <button className="apps-secondary-button" type="button" title={text.edit} onClick={() => startEdit(app)}>{text.edit}</button>
                            <button className="apps-secondary-button" type="button" title={text.duplicate} onClick={() => onDuplicateApp(app.id)}>{text.copy}</button>
                            <button className="apps-secondary-button" type="button" title={text.manifest} onClick={() => setManifestAppId((current) => current === app.id ? '' : app.id)}>{text.manifest}</button>
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
                            <div className="apps-form-row">
                                <label>{isZh(lang) ? '\u540d\u79f0' : 'Name'}</label>
                                <input ref={editNameInputRef} value={editDraft.name} onChange={(event) => setEditDraft((current) => ({ ...current, name: event.target.value }))} />
                            </div>
                            <div className="apps-form-row">
                                <label>{text.category}</label>
                                <input value={editDraft.category} onChange={(event) => setEditDraft((current) => ({ ...current, category: event.target.value }))} />
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
                                <textarea value={editDraft.description} onChange={(event) => setEditDraft((current) => ({ ...current, description: event.target.value }))} />
                            </div>
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

const MarketPane = ({ apps, lang, onInstallApp }: { apps: AppEntry[]; lang?: string; onInstallApp: (app: AppEntry) => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const [manifestText, setManifestText] = useState('');
    const [installState, setInstallState] = useState<'idle' | 'installed' | 'error'>('idle');
    const [installMessage, setInstallMessage] = useState('');
    const [installResultItems, setInstallResultItems] = useState<AppInstallResultItem[]>([]);
    const [installResultDependencyPlan, setInstallResultDependencyPlan] = useState<BackendAppInstallPlan | null>(null);
    const [installResultAppIDs, setInstallResultAppIDs] = useState<string[]>([]);
    const [selectedInstallKeys, setSelectedInstallKeys] = useState<string[] | null>(null);
    const [confirmHighRiskInstall, setConfirmHighRiskInstall] = useState(false);
    const [marketInstallAppId, setMarketInstallAppId] = useState('');
    const [marketInstallFeedback, setMarketInstallFeedback] = useState<{ appId: string; state: 'running' | 'done' | 'error'; message: string; plan?: BackendAppInstallPlan | null; appIDs?: string[]; dependencies?: BackendAppInstallDependency[] } | null>(null);
    const [backendInstallPlan, setBackendInstallPlan] = useState<BackendAppInstallPlan | null>(null);
    const [backendInstallPlanState, setBackendInstallPlanState] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
    const [backendInstallPlanError, setBackendInstallPlanError] = useState('');
    const [installRecords, setInstallRecords] = useState<BackendAppInstallRecord[]>([]);
    const [installRecordsState, setInstallRecordsState] = useState<'loading' | 'ready' | 'error'>('loading');
    const [installRecordsError, setInstallRecordsError] = useState('');
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
    const selectedHasMissingRequiredDependency = backendInstallPlanState === 'ready' && hasMissingRequiredBackendDependency(backendInstallPlan, selectedInstallAppIDs);
    const upgradeableCount = installPlan.filter((item) => item.action === 'upgrade').length;
    const skippedPreviewCount = installPlan.length - selectedInstallCount;
    const hasLiveManifestError = !!manifestText.trim() && !!installPreview.error && installState === 'idle';
    const marketRows = useMemo(() => {
        const plan = buildInstallPlan(marketCatalogApps, apps);
        return marketCatalogApps.map((app) => {
            const item = plan.find((entry) => entry.app.id === app.id);
            const installed = item?.action === 'installed' || item?.action === 'duplicate';
            const upgrade = item?.action === 'upgrade';
            const actionText = installed ? text.alreadyInstalled : upgrade ? text.willUpgrade : text.marketAdd;
            return { app, installed, upgrade, actionText };
        });
    }, [apps, text.alreadyInstalled, text.marketAdd, text.willUpgrade]);
    const addableMarketCount = marketRows.filter((item) => !item.installed && !item.upgrade).length;
    const upgradeableMarketCount = marketRows.filter((item) => item.upgrade).length;
    const installSingleMarketApp = async (app: AppEntry) => {
        const manifestText = JSON.stringify(appToManifest(app));
        setMarketInstallAppId(app.id);
        setMarketInstallFeedback({ appId: app.id, state: 'running', message: text.dependencyPlanLoading, appIDs: [app.id] });
        try {
            const dependencyInstallPlan = await InstallMaclawAppDependencies(manifestText);
            const dependencyDetails = backendDependenciesForApp(dependencyInstallPlan, app.id);
            if (dependencyInstallPlan?.has_blocking_dependency || hasMissingRequiredBackendDependency(dependencyInstallPlan, [app.id])) {
                setMarketInstallFeedback({
                    appId: app.id,
                    state: 'error',
                    message: text.missingRequiredDependency,
                    plan: dependencyInstallPlan || null,
                    appIDs: [app.id],
                    dependencies: dependencyDetails.length > 0 ? dependencyDetails : dependencyInstallPlan?.dependencies || [],
                });
                return;
            }
            let installFeedbackMessage = text.alreadyInstalled;
            try {
                const installAudit = await RecordMaclawAppInstall(manifestText, 'market') as BackendAppInstallRecord;
                const dataSrvSummary = appHasDataSrvRegistrationCandidate(app) ? dataSrvRegistrationSummary(installAudit?.datasrv_registration, text) : '';
                if (dataSrvSummary) installFeedbackMessage = `${installFeedbackMessage} · ${dataSrvSummary}`;
                await refreshInstallRecords();
            } catch {
                // Dependency health is the install gate; audit refresh failures should not discard an otherwise valid local install.
            }
            onInstallApp(app);
            setMarketInstallFeedback({
                appId: app.id,
                state: 'done',
                message: installFeedbackMessage,
                plan: dependencyInstallPlan || null,
                appIDs: [app.id],
                dependencies: dependencyDetails.length > 0 ? dependencyDetails : dependencyInstallPlan?.dependencies || [],
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
            let dependencyPlanForResult: BackendAppInstallPlan | null = backendInstallPlanState === 'ready' ? backendInstallPlan : null;
            if (backendInstallPlanState === 'ready' && hasMissingRequiredBackendDependency(backendInstallPlan, selectedAppIDs)) {
                const dependencyInstallPlan = await InstallMaclawAppDependencies(manifestText);
                dependencyPlanForResult = dependencyInstallPlan || null;
                setBackendInstallPlan(dependencyInstallPlan || null);
                setBackendInstallPlanState('ready');
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
            const nextApps = plan.filter((item) => (item.action === 'install' || item.action === 'upgrade') && selectedKeys.has(item.key)).map((item) => item.app);
            const installedActionCount = plan.filter((item) => item.action === 'install' && selectedKeys.has(item.key)).length;
            const upgradedActionCount = plan.filter((item) => item.action === 'upgrade' && selectedKeys.has(item.key)).length;
            const skippedActionCount = plan.length - nextApps.length;
            nextApps.forEach(onInstallApp);
            setInstallMessage(installSummaryMessage(installedActionCount, upgradedActionCount, skippedActionCount, text));
            setInstallResultItems(resultItems);
            setInstallResultDependencyPlan(dependencyPlanForResult);
            setInstallResultAppIDs(selectedAppIDs);
            setInstallState('installed');
            setConfirmHighRiskInstall(false);
            const installedResultKeys = new Set(resultItems.filter((item) => item.action !== 'skipped').map((item) => item.key));
            if (nextApps.length > 0) {
                const installAuditPackage = nextApps.length === 1 ? appToManifest(nextApps[0]) : appsToPackManifest(nextApps);
                try {
                    const installAudit = await RecordMaclawAppInstall(JSON.stringify(installAuditPackage), 'market') as BackendAppInstallRecord;
                    const dataSrvRegistration = installAudit?.datasrv_registration;
                    await refreshInstallRecords();
                    if (dataSrvRegistration) {
                        setInstallResultItems((current) => current.map((item) => !installedResultKeys.has(item.key) ? item : {
                            ...item,
                            detail: installDetailWithDataSrvRegistration(item.detail, dataSrvRegistration, item.appID, item.dataSrvCandidate, text),
                        }));
                    }
                } catch {
                    // Dependency health is the install gate; audit refresh failures should not discard an otherwise valid local install.
                }
            }
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
                                    <small>{app.category} · {appKinds[app.kind][isZh(lang) ? 'zh' : 'en']} · {sourceLabels[app.source][isZh(lang) ? 'zh' : 'en']}</small>
                                    {feedback && <small className="apps-market-row__feedback" data-state={feedback.state}>{feedback.message}</small>}
                                    {feedback?.plan ? (
                                        <DependencyVerificationPanel
                                            plan={feedback.plan}
                                            state={feedback.state === 'running' ? 'loading' : feedback.state === 'error' ? 'ready' : 'ready'}
                                            selectedAppIDs={feedback.appIDs || [app.id]}
                                            text={text}
                                        />
                                    ) : feedback?.dependencies?.length ? <InstallRecordDependencies dependencies={feedback.dependencies} text={text} /> : null}
                                </div>
                                <button className={installed ? 'apps-secondary-button' : 'apps-primary-button'} type="button" disabled={installed || isInstalling} title={`${buttonText}: ${app.name}`} aria-label={`${buttonText}: ${app.name}`} onClick={() => void installSingleMarketApp(app)}>
                                    {buttonText}
                                </button>
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
                            const sha = String(record.package_sha || '').slice(0, 12) || '-';
                            return (
                                <div className="apps-install-record" key={`${record.package_sha || 'record'}:${record.installed_at || index}`} data-missing={missingCount > 0 ? 'true' : 'false'}>
                                    <div className="apps-install-record__main">
                                        <strong>{appNames}</strong>
                                        <span>{text.installedAt}: {formatInstallRecordTime(record.installed_at)} · {text.marketSource}: {record.source || '-'}</span>
                                        <small>{text.packageSha}: {sha} · {text.skillDependencies}: {dependencyCount} · {text.missingDependencyCount}: {missingCount}</small>
                                        <InstallRecordDependencies dependencies={dependencies} text={text} />
                                    </div>
                                    <em>{record.has_blocking_dependency || record.has_missing_required || missingCount > 0 ? text.unavailableDependency : text.installedDependency}</em>
                                </div>
                            );
                        })}
                    </div>
                )}
            </section>
            <details className="apps-market-install">
                <summary>
                    <span>
                        <span className="apps-definition__title">{text.marketAdvancedImport}</span>
                        <span className="apps-market-list__meta">{text.marketAdvancedImportHint}</span>
                    </span>
                </summary>
                <div className="apps-market-install__body">
                    <div className="apps-preview-title-row">
                        <div className="apps-definition__title">{text.installManifest}</div>
                        <button className="apps-primary-button" type="button" disabled={!manifestText.trim() || !!installPreview.error || (installPlan.length > 0 && selectedInstallCount === 0)} onClick={installManifest}>{confirmHighRiskInstall ? text.confirmHighRiskInstall : text.install}</button>
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
                                    const hasBlockingDependencies = backendInstallPlanState === 'ready' && backendDependencies.some(isBlockingBackendDependency);
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
                            {installState === 'installed' && installResultItems.length > 0 && (
                                <div className="apps-install-result" role="list" aria-label={text.installDetails}>
                                    {installResultItems.map((item) => (
                                        <div className="apps-install-result__row" data-action={item.action} role="listitem" key={item.key}>
                                            <span className="apps-app-icon" style={{ '--apps-icon-color': item.accent } as CSSProperties}><AppIcon icon={item.icon} customIconDataUrl={item.customIconDataUrl} /></span>
                                            <strong>{item.name}</strong>
                                            <em>{item.action === 'upgraded' ? text.upgradedItem : item.action === 'installed' ? text.installedCount : text.skippedItem}</em>
                                            <small>{item.detail}</small>
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
