import { useCallback, useEffect, useMemo, useState } from 'react';
import type { CSSProperties, KeyboardEvent } from 'react';
import { CancelNLSkillRun, DownloadSkillRunArtifact, GetMISDataConfig, GetNLSkillRunStatus, ListMaclawAppApprovalInstances, ListMaclawAppInstalls, ListNLSkills, ListSkillAppManifests, OpenFileOrShowInFolder, InstallMaclawAppDependencies, PlanMaclawAppInstall, RecordMaclawAppApprovalInstance, RecordMaclawAppInstall, SyncMaclawAppApprovalInstanceToDataSrv, OpenSkillRunArtifact, RecordMaclawAppRunEvidenceForSkill, RevealSkillRunArtifact, RunNLSkillAsync, SaveMaclawAppDefinitionForSkill, ShowItemInFolder, StageSkillAppInputFile, UploadNLSkillToMarket } from '../../../wailsjs/go/main/App';
import './AppsPage.css';

type AppKind = 'enterprise_approval_app' | 'enterprise_normal_app' | 'tool_app' | 'automation_app';
type StudioTab = 'create' | 'manage' | 'market' | 'publish';
type AppMoveTarget = -1 | 1 | 'top' | 'bottom';

type AppEntry = {
    id: string;
    name: string;
    description: string;
    category: string;
    kind: AppKind;
    icon: AppIconName;
    accent: string;
    pinned?: boolean;
    recentUsedAt?: string;
    version?: number;
    source: 'builtin' | 'skill' | 'datasrv' | 'market' | 'local';
    manifest?: AppManifestBinding;
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
type StudioLayoutTemplate = 'classic_split' | 'left_nav' | 'document_workspace';
type StudioLayoutDensity = 'comfortable' | 'compact';
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
    action?: 'skip' | 'blocked' | 'optional_missing' | string;
    message?: string;
};

type BackendAppInstallPlan = {
    schema?: string;
    apps?: Array<{ id: string; name?: string; kind?: string; schema?: string }>;
    dependencies?: BackendAppInstallDependency[];
    has_missing_required?: boolean;
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
};

type AppWorkspaceLayout = {
    schema: 'maclaw.app.ui.v1';
    generated?: boolean;
    entry?: string;
    layouts?: Record<string, any>;
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
    };
    dependencies?: {
        skills?: AppSkillDependency[];
    };
    ui?: AppWorkspaceLayout;
    datasrv?: {
        domain: string;
        datasetID?: string;
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
    kind?: string;
    title?: string;
    text?: string;
    status?: string;
    artifact_id?: string;
    artifact?: SkillRunArtifactView;
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
    at: string;
};

type ApprovalInstanceView = {
    id: string;
    appID?: string;
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
    amount?: string;
};

type BackendApprovalInstance = {
    app_id?: string;
    app_name?: string;
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
    name: string;
    icon: AppIconName;
    accent: string;
    action: 'installed' | 'upgraded' | 'skipped';
    detail: string;
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
    is_maclaw_app?: boolean;
};

const storageKey = 'maclaw:apps-panel:v1';
const runHistoryStorageKey = 'maclaw:apps-run-history:v1';
const publishSubmissionStorageKey = 'maclaw:apps-publish-submissions:v1';
const maxPinnedApps = 8;
const maxSkillAppStagingBytes = 25 * 1024 * 1024;
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
        approvalWorkspace: '\u5ba1\u6279\u5b9e\u4f8b',
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
        approvalInstanceId: '\u5b9e\u4f8b\u7f16\u53f7',
        noRunHistory: '\u6682\u65e0\u8fd0\u884c\u8bb0\u5f55',
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
        discoveredApps: '\u53ef\u751f\u6210\u5e94\u7528',
        skillApps: 'Skill \u5e94\u7528',
        skillAppsReady: '\u5df2\u53d1\u73b0',
        manifestPreview: '\u5e94\u7528 manifest \u6a21\u677f',
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
        confirmHighRiskInstall: '\u786e\u8ba4\u5b89\u88c5',
        highRiskInstallWarning: '\u9009\u4e2d\u7684\u5347\u7ea7\u5305\u5305\u542b\u9ad8\u98ce\u9669\u65b0\u6743\u9650\uff0c\u9700\u518d\u6b21\u786e\u8ba4\u3002',
        dependencyPlanLoading: '\u6b63\u5728\u68c0\u67e5 Skill \u4f9d\u8d56',
        missingRequiredDependency: '\u7f3a\u5c11\u5fc5\u9700 Skill \u4f9d\u8d56\uff0c\u8bf7\u5148\u5b89\u88c5\u4f9d\u8d56',
        dependencyPlanError: '\u4f9d\u8d56\u68c0\u67e5\u5931\u8d25',
        skillDependencies: '\u4f9d\u8d56 Skill',
        installedDependency: '\u5df2\u5b89\u88c5',
        missingDependency: '\u7f3a\u5931',
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
        installRecordsLoading: '\u6b63\u5728\u8bfb\u53d6\u5b89\u88c5\u8bb0\u5f55',
        installRecordsError: '\u5b89\u88c5\u8bb0\u5f55\u8bfb\u53d6\u5931\u8d25',
        noInstallRecords: '\u6682\u65e0\u5e94\u7528\u5b89\u88c5\u8bb0\u5f55',
        refreshInstallRecords: '\u5237\u65b0\u8bb0\u5f55',
        packageSha: '\u5305\u6307\u7eb9',
        installedAt: '\u5b89\u88c5\u65f6\u95f4',
        missingDependencyCount: '\u7f3a\u5931\u4f9d\u8d56',
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
        approvalWorkspace: 'Approval instances',
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
        approvalInstanceId: 'Instance ID',        noRunHistory: 'No runs yet',
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
        discoveredApps: 'Discovered apps',
        skillApps: 'Skill apps',
        skillAppsReady: 'Discovered',
        manifestPreview: 'App manifest template',
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
        confirmHighRiskInstall: 'Confirm install',
        highRiskInstallWarning: 'Selected upgrades include high-risk new permissions; confirm once more.',
        dependencyPlanLoading: 'Checking Skill dependencies',
        missingRequiredDependency: 'Required Skill dependencies are missing. Install dependencies first.',
        dependencyPlanError: 'Dependency check failed',
        skillDependencies: 'Skill dependencies',
        installedDependency: 'Installed',
        missingDependency: 'Missing',
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
        installRecordsLoading: 'Reading install records',
        installRecordsError: 'Failed to read install records',
        noInstallRecords: 'No app install records yet',
        refreshInstallRecords: 'Refresh records',
        packageSha: 'Package SHA',
        installedAt: 'Installed at',
        missingDependencyCount: 'Missing deps',
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
    { id: 'expense', name: '\u62a5\u9500\u7533\u8bf7', description: '\u4ece\u53d1\u7968\u3001\u884c\u7a0b\u548c\u653f\u7b56\u81ea\u52a8\u751f\u6210\u62a5\u9500\u5355\u3002', category: 'OA', kind: 'enterprise_approval_app', icon: 'receipt', accent: '#2f5f98', pinned: true, source: 'datasrv', manifest: makeEnterpriseManifest('enterprise_approval_app', 'finance', 'finance.expense_upsert', 'finance.expense_review', 'finance.expense_by_status', 'finance.overview', 'expense-approval-app', [{ id: 'expense-approval-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow', 'expense.policy'] }]) },
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

function makeEnterpriseManifest(kind: 'enterprise_approval_app' | 'enterprise_normal_app', domain: string, preferredAction: string, preferredView: string, preferredReport: string, preferredDashboard: string, appSkillID = '', dependencies: AppSkillDependency[] = []): AppManifestBinding {
    return {
        schema: 'maclaw.app.v1',
        installUnit: 'enterprise_app_pack',
        privateMarker: 'x_maclaw_apps',
        entryKind: kind,
        launchMode: 'agent_dynamic_ui',
        datasrv: { domain, preferredAction, preferredView, preferredReport, preferredDashboard },
        appSkill: appSkillID ? { id: appSkillID, version: '1.0.0' } : undefined,
        dependencies: dependencies.length ? { skills: dependencies } : undefined,
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

function runtimeWorkspaceLayoutForApp(app: AppEntry): RuntimeWorkspaceLayout {
    const entry = app.manifest?.ui?.entry || workspaceEntryForKind(app.kind);
    const layout = app.manifest?.ui?.layouts?.[entry] || {};
    const template = ['classic_split', 'left_nav', 'document_workspace'].includes(String(layout.template)) ? layout.template as StudioLayoutTemplate : app.kind === 'tool_app' ? 'document_workspace' : 'classic_split';
    const density = layout.density === 'compact' ? 'compact' : 'comfortable';
    const primaryRegion = ['left', 'center', 'right'].includes(String(layout.primaryRegion)) ? layout.primaryRegion as StudioPrimaryRegion : 'left';
    const outputRegion = ['right', 'bottom', 'modal'].includes(String(layout.outputRegion)) ? layout.outputRegion as StudioOutputRegion : 'right';
    return { template, density, primaryRegion, outputRegion };
}

function runtimeWorkspaceOrder(layout: RuntimeWorkspaceLayout) {
    const outputEarly = layout.outputRegion === 'right' || layout.outputRegion === 'modal';
    return {
        input: 10,
        approval: outputEarly ? 30 : 20,
        status: outputEarly ? 40 : 30,
        actions: outputEarly ? 50 : 40,
        output: outputEarly ? 20 : 50,
        history: 60,
    };
}
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

function normalizeAppWorkspaceLayout(raw: unknown, kind: AppKind): AppWorkspaceLayout {
    const value = raw && typeof raw === 'object' ? raw as Partial<AppWorkspaceLayout> : undefined;
    return value?.schema === 'maclaw.app.ui.v1' ? { ...value, schema: 'maclaw.app.ui.v1' } : defaultWorkspaceLayoutForKind(kind);
}

function appSkillDependencies(app: AppEntry): AppSkillDependency[] {
    const deps = app.manifest?.dependencies?.skills || [];
    const appSkill = app.manifest?.appSkill?.id ? [{ id: app.manifest.appSkill.id, version: app.manifest.appSkill.version, kind: 'runtime_skill' as const, required: true, source: 'hub' as const }] : [];
    const boundSkill = app.manifest?.skill?.id ? [{ id: app.manifest.skill.id, kind: 'runtime_skill' as const, required: true, source: 'hub' as const }] : [];
    return Array.from(new Map([...appSkill, ...boundSkill, ...deps].map((dep) => [dep.id, dep])).values());
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
        accent: String(app.accent || defaultAccentForKind(normalizeAppKind(app.kind))),
        version: normalizeAppVersion(app.version),
        source: migratedSource,
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

function buildDataSrvAppCandidates(caps: any): AppEntry[] {
    const domains = Array.isArray(caps?.domains) ? caps.domains.filter((domain: any) => typeof domain === 'string' && domain.trim()) : [];
    const actions = Array.isArray(caps?.business_actions) ? caps.business_actions as DataSrvCapabilityItem[] : [];
    const views = Array.isArray(caps?.business_views) ? caps.business_views as DataSrvCapabilityItem[] : [];
    const reports = Array.isArray(caps?.reports) ? caps.reports as DataSrvCapabilityItem[] : [];
    const dashboards = Array.isArray(caps?.dashboards) ? caps.dashboards as DataSrvCapabilityItem[] : [];
    return domains.slice(0, 12).map((domain: string) => {
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
    return {
        status: 'ready',
        candidates: (entries || []).map(skillManifestToApp).filter((app): app is AppEntry => !!app),
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

function skillRunPrimaryArtifact(status?: SkillRunStatusView | null): SkillRunArtifactView | null {
    const hasArtifactRef = (item?: SkillRunArtifactView) => !!String(item?.path || item?.uri || item?.id || item?.remote_url || '').trim();
    const fromTop = Array.isArray(status?.artifacts) ? status?.artifacts?.find(hasArtifactRef) : undefined;
    if (fromTop) return fromTop;
    const fromSummary = Array.isArray(status?.summary?.artifacts) ? status?.summary?.artifacts?.find(hasArtifactRef) : undefined;
    if (fromSummary) return fromSummary;
    const fromOutput = skillRunOutputBlocks(status).map((block) => block.artifact).find(hasArtifactRef);
    if (fromOutput) return fromOutput;
    const artifactPath = String(status?.summary?.artifact_path || '').trim();
    if (!artifactPath) return null;
    return { path: artifactPath, status: status?.summary?.artifact_status };
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
    return loadAppRunHistory(app.id).find((item) => item.status === 'done' && item.definitionHash === expectedHash) || null;
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
    if (deps.length === 0) return zh ? '无 Skill 依赖' : 'No Skill dependencies';
    return deps.map((dep) => `${dep.id}${dep.version ? `@${dep.version}` : ''} (${dep.kind}${dep.required ? '' : ', optional'})`).join(', ');
}
function appGovernanceForManifest(app: AppEntry, submission?: AppPublishSubmission) {
    const evidence = latestAppRunEvidence(app);
    const localEvidenceAt = evidence?.at || app.recentUsedAt || '';
    return {
        status: submission?.status || (localEvidenceAt ? 'local_tested' : 'draft'),
        riskLevel: submission?.riskLevel || (isEnterpriseAppKind(app.kind) ? 'medium' : 'low'),
        requiredScopes: appRequiredScopes(app),
        dependencies: {
            installPolicy: 'install_on_app_install',
            skills: appDependencyEvidence(app),
            requiredCount: appDependencyEvidence(app).filter((dep) => dep.required).length,
            optionalCount: appDependencyEvidence(app).filter((dep) => !dep.required).length,
        },
        testEvidence: {
            runId: evidence?.runID,
            definitionHash: evidence?.definitionHash,
            artifactPresent: !!(evidence?.artifactURI || evidence?.artifactName || evidence?.artifactPath),
            artifactName: evidence?.artifactName || (evidence?.artifactPath ? evidence.artifactPath.split(/[\\/]/).pop() : undefined),
            verifiedAt: localEvidenceAt || undefined,
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
                <span className="apps-app-icon"><Icon name={app.icon} /></span>
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
                    <button className="apps-studio-button" type="button" title={text.appStudio} aria-label={text.appStudio} onClick={() => setStudioOpen(true)}>
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
                    />
                )}
            </main>
        </div>
    );
};

const AppRuntime = ({ tabs, activeApp, lang, onActivate, onClose, onUse }: {
    tabs: AppEntry[];
    activeApp?: AppEntry;
    lang?: string;
    onActivate: (appId: string) => void;
    onClose: (appId: string) => void;
    onUse: (appId: string) => void;
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
                            <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><Icon name={app.icon} /></span>
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
                <AppPreview app={activeApp} lang={lang} onUse={onUse} />
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
    const workflow = app.manifest?.dependencies?.skills?.find((dep) => dep.kind === 'workflow_skill')?.id || app.manifest?.appSkill?.id || app.name;
    const submitted = runState === 'done' || runState === 'running';
    const draftStatus = submitted ? 'pending' : 'draft';
    const draftNode = submitted ? (zh ? '主管审批' : 'Manager approval') : (zh ? '发起节点' : 'Submit node');
    const draftResult = submitted ? (zh ? '审批中' : 'Pending') : (zh ? '待提交' : 'Draft');
    return [
        {
            id: `${app.id}-current`,
            appID: app.id,
            title: submitted ? `${title} · ${businessAction}` : title,
            lane: 'my_requests',
            status: draftStatus,
            currentNode: draftNode,
            owner: zh ? '\u6211' : 'Me',
            approver: submitted ? (zh ? '直属主管 / VE' : 'Manager / VE') : workflow,
            updatedAt: submitted ? new Date().toLocaleString() : '-',
            result: businessNote.trim() || draftResult,
            workflowSkillID: workflow,
            businessStatus: submitted ? 'approval_pending' : 'draft',
            resultStatus: draftStatus,
            amount: submitted ? (zh ? '鏈鎻愪氦' : 'Current submission') : undefined,
        },
        {
            id: `${app.id}-pending`,
            appID: app.id,
            title: zh ? `${app.name}寰呭姙` : `${app.name} task`,
            lane: 'pending_my_approval',
            status: 'pending',
            currentNode: zh ? '\u6211\u5ba1\u6279' : 'My approval',
            owner: zh ? '鍚屼簨' : 'Coworker',
            approver: zh ? '\u6211' : 'Me',
            updatedAt: zh ? '浠婂ぉ' : 'Today',
            result: zh ? '绛夊緟澶勭悊' : 'Waiting',
            workflowSkillID: workflow,
            businessStatus: 'approval_pending',
            resultStatus: 'pending',
        },
        {
            id: `${app.id}-attention`,
            appID: app.id,
            title: zh ? `${app.name}需关注` : `${app.name} attention`,
            lane: 'attention',
            status: 'attention',
            currentNode: zh ? '椋庨櫓澶嶆牳' : 'Risk review',
            owner: zh ? '绯荤粺' : 'System',
            approver: zh ? '\u4ec5\u67e5\u770b' : 'View only',
            updatedAt: zh ? '鏄ㄥぉ' : 'Yesterday',
            result: zh ? '瑙勫垯鍛戒腑锛岄渶鏌ョ湅' : 'Rule matched, review needed',
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
    return {
        id,
        appID: String(instance.app_id || '').trim(),
        title: appTitle,
        lane: lane === 'pending_my_approval' || lane === 'handled' || lane === 'attention' ? lane : 'my_requests',
        status: status === 'approved' || status === 'rejected' || status === 'attention' || status === 'draft' ? status : 'pending',
        currentNode: String(instance.current_node || (isZh(lang) ? '当前节点' : 'Current node')).trim(),
        owner: String(instance.owner || '-').trim(),
        approver: String(instance.approver || '-').trim(),
        updatedAt: String(instance.updated_at || '-').trim(),
        result: String(instance.result || '-').trim(),
        workflowSkillID: String(instance.workflow_skill_id || '').trim(),
        workflowDecisionID: String(instance.workflow_decision_id || '').trim(),
        businessStatus: String(instance.business_status || '').trim(),
        resultStatus: String(instance.result_status || '').trim(),
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

function appApprovalRecordID(instance: BackendApprovalInstance): string {
    return String(instance.record_id || instance.instance_id || '').trim();
}

function approvalDataSrvSyncPayload(app: AppEntry, instance: BackendApprovalInstance) {
    const datasetID = appDataSrvDatasetID(app);
    const recordID = appApprovalRecordID(instance);
    if (!datasetID || !recordID) return null;
    return {
        dataset_id: datasetID,
        record_id: recordID,
        approval_id: String(instance.approval_id || instance.record_approval_id || '').trim(),
        instance,
    };
}

function approvalStatusLabel(status: ApprovalInstanceView['status'], lang?: string) {
    const zh = isZh(lang);
    if (status === 'approved') return zh ? '已通过' : 'Approved';
    if (status === 'rejected') return zh ? '\u5df2\u9a73\u56de' : 'Rejected';
    if (status === 'attention') return zh ? '需关注' : 'Needs attention';
    if (status === 'pending') return zh ? '\u5ba1\u6279\u4e2d' : 'Pending';
    return zh ? '鑽夌' : 'Draft';
}

const AppPreview = ({ app, lang, onUse }: { app?: AppEntry; lang?: string; onUse?: (appId: string) => void }) => {
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
    const [runHistory, setRunHistory] = useState<AppRunHistoryEntry[]>([]);
    const [approvalInstances, setApprovalInstances] = useState<ApprovalInstanceView[]>([]);
    const [currentRunContext, setCurrentRunContext] = useState({ inputSummary: '', outputMode: '' });
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
        setCurrentRunContext({ inputSummary: '', outputMode: '' });
        setRunHistory(loadAppRunHistory(app?.id || ''));
        setApprovalInstances([]);
    }, [app?.id, app?.manifest?.skill?.outputModes]);
    useEffect(() => {
        if (!app?.id || !isEnterpriseApprovalAppKind(app.kind)) {
            setApprovalInstances([]);
            return;
        }
        let disposed = false;
        const loadInstances = async () => {
            try {
                const records = await ListMaclawAppApprovalInstances(app.id, 'all', 50) as BackendApprovalInstance[];
                if (disposed) return;
                setApprovalInstances((records || []).map((item) => backendApprovalInstanceToView(item, lang)).filter(Boolean) as ApprovalInstanceView[]);
            } catch {
                if (!disposed) setApprovalInstances([]);
            }
        };
        void loadInstances();
        return () => {
            disposed = true;
        };
    }, [app?.id, app?.kind, lang]);
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
                    const artifact = skillRunPrimaryArtifact(status);
                    const artifactPath = String(artifact?.path || '').trim();
                    const artifactID = String(artifact?.id || '').trim();
                    const artifactURI = String(artifact?.uri || '').trim();
                    const artifactName = String(artifact?.name || artifactPath.split(/[\\/]/).pop() || '').trim();
                    const artifactDownloadState = String(artifact?.download_state || '').trim();
                    const definitionHash = app ? appDefinitionFingerprint(app) : undefined;
                    const verifiedAt = new Date().toISOString();
                    setValidationMessage('');
                    setRunState('done');
                    recordRunHistory({
                        runID,
                        status: 'done',
                        definitionHash,
                        outputMode: currentRunContext.outputMode,
                        inputSummary: currentRunContext.inputSummary,
                        message: skillRunOutputSuffix(status).replace(/^ · /, '') || text.skillRunCompleted,
                        artifactID,
                        artifactURI,
                        artifactName,
                        artifactPath,
                        artifactDownloadState,
                    });
                    if (app?.source === 'skill' && app.manifest?.skill?.id) {
                        void RecordMaclawAppRunEvidenceForSkill(app.manifest.skill.id, app.id, definitionHash || '', runID, artifactPath || artifactName || artifactURI, verifiedAt).catch(() => undefined);
                    }
                } else if (lifecycle === 'error') {
                    const message = skillRunErrorMessage(status) || text.skillRunFailed;
                    setValidationMessage(message);
                    setRunState('error');
                    recordRunHistory({ runID, status: 'error', outputMode: currentRunContext.outputMode, inputSummary: currentRunContext.inputSummary, message });
                } else if (lifecycle === 'cancelled') {
                    setValidationMessage(text.skillRunCancelled);
                    setRunState('cancelled');
                    recordRunHistory({ runID, status: 'cancelled', outputMode: currentRunContext.outputMode, inputSummary: currentRunContext.inputSummary, message: text.skillRunCancelled });
                }
            } catch (error: any) {
                if (disposed) return;
                const message = error?.message || String(error || text.skillRunFailed);
                setValidationMessage(message);
                setRunState('error');
                recordRunHistory({ runID, status: 'error', outputMode: currentRunContext.outputMode, inputSummary: currentRunContext.inputSummary, message });
            }
        };
        void poll();
        const timer = window.setInterval(poll, 1500);
        return () => {
            disposed = true;
            window.clearInterval(timer);
        };
    }, [runID, runState, text.skillRunCancelled, text.skillRunCompleted, text.skillRunFailed, currentRunContext]);
    if (!app) return <div className="apps-empty">{text.noApps}</div>;
    const isTool = app.kind === 'tool_app';
    const isApproval = isEnterpriseApprovalAppKind(app.kind);
    const isAutomation = app.kind === 'automation_app';
    const outputModes = normalizeOutputModes(app.manifest?.skill?.outputModes);
    const workflowSkillID = app.manifest?.dependencies?.skills?.find((dependency) => dependency.kind === 'workflow_skill')?.id || app.manifest?.appSkill?.id || app.manifest?.skill?.id || '';
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
            : `${text.submitted}: ${businessEntity || app.category} · ${businessAction} · ${app.manifest?.datasrv?.preferredAction || app.manifest?.datasrv?.domain || 'DataSrv'}`;
    const markDirty = () => {
        setRunState('idle');
        setValidationMessage('');
        setRunID('');
        setSkillRunStatus(null);
        setCurrentRunContext({ inputSummary: '', outputMode: '' });
    };
    const runApp = async () => {
        if (app?.id) onUse?.(app.id);
        if (app && (app.source === 'market' || app.source === 'skill' || app.source === 'local') && (app.manifest?.appSkill || (app.manifest?.dependencies?.skills?.length || 0) > 0)) {
            try {
                const dependencyPlan = await PlanMaclawAppInstall(JSON.stringify(appToManifest(app)));
                if (dependencyPlan?.has_missing_required) {
                    setValidationMessage(text.missingRequiredDependency);
                    setRunState('error');
                    return;
                }
            } catch (error: any) {
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
        setValidationMessage('');
        setRunState('done');
        if (isApproval) {
            const now = new Date().toISOString();
            const fallbackID = `appr-${Date.now().toString(36)}`;
            const payload: BackendApprovalInstance = {
                instance_id: fallbackID,
                app_id: app.id,
                app_name: app.name,
                workflow_skill_id: workflowSkillID,
                title: `${businessEntity || app.category} / ${businessAction || 'create'}`,
                lane: 'my_requests',
                status: 'pending',
                current_node: isZh(lang) ? '主管审批' : 'Manager approval',
                owner: isZh(lang) ? '当前用户' : 'Current user',
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
                events: [{
                    at: now,
                    actor: isZh(lang) ? '\u5f53\u524d\u7528\u6237' : 'Current user',
                    action: isZh(lang) ? '\u53d1\u8d77\u7533\u8bf7' : 'Submitted',
                    note: businessNote,
                }],
            };
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
            } catch {
                const view = backendApprovalInstanceToView(payload, lang);
                if (view) {
                    setApprovalInstances((current) => [view, ...current.filter((item) => item.id !== view.id)].slice(0, 50));
                }
            }
        }
    };
    const updateApprovalInstanceDecision = async (instance: ApprovalInstanceView, decision: 'approved' | 'rejected' | 'attention') => {
        if (!app?.id || !instance?.id) return;
        const now = new Date().toISOString();
        const zh = isZh(lang);
        const statusText = decision === 'approved'
            ? (zh ? '已通过' : 'Approved')
            : decision === 'rejected'
                ? (zh ? '\u5df2\u9a73\u56de' : 'Rejected')
                : (zh ? '需关注' : 'Needs attention');
        const nextLane: ApprovalInstanceView['lane'] = decision === 'attention' ? 'attention' : 'handled';
        const nextNode = decision === 'attention' ? instance.currentNode : (zh ? '\u5df2\u5b8c\u6210' : 'Completed');
        const payload: BackendApprovalInstance = {
            instance_id: instance.id,
            app_id: instance.appID || app.id,
            app_name: app.name,
            workflow_skill_id: instance.workflowSkillID || workflowSkillID,
            workflow_decision_id: `decision-${Date.now().toString(36)}`,
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
            business_entity: businessEntity || app.category,
            business_action: businessAction || 'approve',
            business_note: businessNote,
            updated_at: now,
            events: [{
                at: now,
                node: nextNode,
                actor: instance.approver || (zh ? '\u5ba1\u6279\u4eba' : 'Approver'),
                decision,
                message: statusText,
                action: decision,
                note: businessNote,
            }],
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
    };
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
                        {isApproval && <ApprovalWorkspace app={app} runState={runState} businessEntity={businessEntity} businessAction={businessAction} businessNote={businessNote} backendInstances={approvalInstances} lang={lang} text={text} style={{ order: runtimeOrder.approval }} layoutRegion={centerRegion} onDecision={updateApprovalInstanceDecision} />}
                        <section className="apps-runtime-section apps-runtime-status" data-region={outputRegion === 'bottom' ? centerRegion : outputRegion} style={{ order: runtimeOrder.status }}>
                            <div className="apps-runtime-section__title">{text.runtimeStatus}</div>
                            <div className="apps-result-panel" data-state={runState}>
                                <span>{runState === 'done' ? text.runCompleted : runState === 'running' ? skillRunProgressMessage(skillRunStatus, text.skillRunRunning, runID) : runState === 'error' ? validationMessage : runState === 'cancelled' ? text.skillRunCancelled : text.readyOutput}</span>
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
                                setCurrentRunContext({ inputSummary: '', outputMode: '' });
                            }}>{text.reset}</button>
                            {runState === 'running' && runID && <button className="apps-secondary-button" type="button" onClick={cancelRun}>{text.cancelRun}</button>}
                            <button className="apps-primary-button" type="button" disabled={runState === 'running'} onClick={runApp}>{text.run}</button>
                        </div>
                        <AppRunOutput status={skillRunStatus} runState={runState} resultText={resultText} isTool={isTool} text={text} style={{ order: runtimeOrder.output }} layoutRegion={outputRegion} />
                        {isTool && (
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

const ApprovalWorkspace = ({ app, runState, businessEntity, businessAction, businessNote, backendInstances, lang, text, style, layoutRegion, onDecision }: { app: AppEntry; runState: 'idle' | 'running' | 'done' | 'error' | 'cancelled'; businessEntity: string; businessAction: string; businessNote: string; backendInstances?: ApprovalInstanceView[]; lang?: string; text: typeof labels.zh; style?: CSSProperties; layoutRegion?: string; onDecision?: (instance: ApprovalInstanceView, decision: 'approved' | 'rejected' | 'attention') => void | Promise<void> }) => {
    const [lane, setLane] = useState<'my_requests' | 'pending_my_approval' | 'handled' | 'attention' | 'all'>('my_requests');
    const fallbackInstances = buildApprovalInstances(app, runState, businessEntity, businessAction, businessNote, lang);
    const instances = backendInstances && backendInstances.length > 0 ? backendInstances : fallbackInstances;
    const visibleInstances = lane === 'all' ? instances : lane === 'handled' ? instances.filter((item) => item.status === 'approved' || item.status === 'rejected') : instances.filter((item) => item.lane === lane);
    const selected = visibleInstances[0] || instances[0];
    const lanes = [
        { key: 'my_requests', label: text.myRequests },
        { key: 'pending_my_approval', label: text.pendingMyApproval },
        { key: 'handled', label: text.handledApprovals },
        { key: 'attention', label: text.attentionApprovals },
        { key: 'all', label: text.allApprovalInstances },
    ] as const;
    const countForLane = (key: typeof lanes[number]['key']) => key === 'all'
        ? instances.length
        : key === 'handled'
            ? instances.filter((item) => item.status === 'approved' || item.status === 'rejected').length
            : instances.filter((item) => item.lane === key).length;
    return (
        <section className="apps-runtime-section apps-approval-workspace" aria-label={text.approvalWorkspace} data-region={layoutRegion || 'center'} style={style}>
            <div className="apps-runtime-section__title">{text.approvalWorkspace}</div>
            <div className="apps-approval-layout">
                <nav className="apps-approval-nav" aria-label={text.approvalWorkspace}>
                    {lanes.map((item) => (
                        <button key={item.key} className={lane === item.key ? 'is-active' : ''} type="button" aria-pressed={lane === item.key} onClick={() => setLane(item.key)}>
                            <span>{item.label}</span>
                            <strong>{countForLane(item.key)}</strong>
                        </button>
                    ))}
                </nav>
                <div className="apps-approval-list" role="list" aria-label={text.approvalInstanceData}>
                    {visibleInstances.map((item) => (
                        <div className="apps-approval-row" data-state={item.status} role="listitem" key={item.id}>
                            <div>
                                <strong>{item.title}</strong>
                                <span>{text.currentApprovalNode}: {item.currentNode} · {text.approvalResult}: {approvalStatusLabel(item.status, lang)}</span>
                                <small>{text.approvalInstanceId}: {item.id} · {item.updatedAt}</small>
                            </div>
                            <em>{approvalStatusLabel(item.status, lang)}</em>
                        </div>
                    ))}
                </div>
                <aside className="apps-approval-detail" aria-label={text.approvalTimeline}>
                    <div className="apps-approval-detail__head">
                        <strong>{selected.title}</strong>
                        <span>{text.currentApprovalNode}: {selected.currentNode}</span>
                    </div>
                    <dl className="apps-approval-facts">
                        <div><dt>{text.myRequests}</dt><dd>{selected.owner}</dd></div>
                        <div><dt>{text.pendingMyApproval}</dt><dd>{selected.approver}</dd></div>
                        <div><dt>{text.approvalResult}</dt><dd>{selected.result}</dd></div>
                    </dl>
                    <div className="apps-approval-timeline">
                        <div><span />{isZh(lang) ? '发起节点' : 'Submit node'}</div>
                        <div><span />{selected.currentNode}</div>
                        <div><span />{isZh(lang) ? '缁撴灉鍙嶉' : 'Result feedback'}</div>
                    </div>
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

const AppRunOutput = ({ status, runState, resultText, isTool, text, style, layoutRegion }: { status: SkillRunStatusView | null; runState: 'idle' | 'running' | 'done' | 'error' | 'cancelled'; resultText: string; isTool: boolean; text: typeof labels.zh; style?: CSSProperties; layoutRegion?: string }) => {
    const artifact = skillRunPrimaryArtifact(status);
    const artifactPath = String(artifact?.path || '').trim();
    const artifactID = String(artifact?.id || '').trim();
    const artifactURI = String(artifact?.uri || '').trim();
    const runID = String(status?.run_id || '').trim();
    const artifactStatus = String(artifact?.status || status?.summary?.artifact_status || '').trim();
    const artifactLabel = artifactStatusLabel(status, text);
    const artifactName = String(artifact?.name || artifactPath.split(/[\\/]/).pop() || '').trim();
    const artifactDownloadState = String(artifact?.download_state || '').trim().toLowerCase();
    const artifactRemoteOnly = artifactDownloadState === 'remote' && !artifactPath;
    const artifactMeta = [artifactName, artifact?.mime_type, artifact?.size_bytes ? `${artifact.size_bytes} bytes` : '', artifactRemoteOnly ? 'remote' : ''].filter(Boolean).join(' · ');
    const artifactRef = artifactID || artifactURI;
    const hasArtifact = !!(artifactRef || artifactPath);
    const outputBlocks = skillRunOutputBlocks(status).filter((block) => skillRunOutputBlockText(block));
    const showTextOutput = runState === 'done' && (!isTool || (!hasArtifact && outputBlocks.length === 0));
    return (
        <section className="apps-runtime-section apps-runtime-output" data-region={layoutRegion || 'right'} style={style}>
            <div className="apps-runtime-section__title">{text.runtimeOutput}</div>
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
            {hasArtifact ? (
                <div className="apps-run-artifact">
                    <span>{text.runArtifacts}</span>
                    <strong>{artifactLabel || artifactStatus || text.artifactReady}</strong>
                    {artifactMeta && <span>{artifactMeta}</span>}
                    <code>{artifactURI || artifactName || artifactPath}</code>
                    <div className="apps-run-artifact__actions">
                        <button className="apps-link-button" type="button" disabled={!artifactRef && !artifactPath} onClick={() => void openSkillRunArtifactFromUI(runID, artifactRef, artifactPath, artifactRemoteOnly)}>{artifactRemoteOnly ? text.downloadArtifact : text.openArtifact}</button>
                        <button className="apps-link-button" type="button" disabled={!artifactRef && !artifactPath} onClick={() => void revealSkillRunArtifactFromUI(runID, artifactRef, artifactPath)}>{text.revealArtifact}</button>
                    </div>
                </div>
            ) : outputBlocks.length === 0 && showTextOutput ? (
                <div className="apps-output-text">
                    <span>{text.outputText}</span>
                    <pre>{resultText}</pre>
                </div>
            ) : outputBlocks.length === 0 ? (
                <div className="apps-output-empty">{artifactStatus || artifactLabel || text.noOutputYet}</div>
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
        ...(manifest.skill ? { skill: manifest.skill } : {}),
    } : undefined;
    return textHash(stableStringify({
        name: app.name,
        description: app.description,
        category: app.category,
        kind: app.kind,
        icon: app.icon,
        version: normalizeAppVersion(app.version),
        manifest: runtimeManifest,
    }));
}

function appToManifest(app: AppEntry, submission?: AppPublishSubmission) {
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
            source: app.source,
            launchMode: manifest?.launchMode || defaultLaunchModeForKind(app.kind),
            binding: {
                datasrv: manifest?.datasrv,
                skill: manifest?.skill,
                appSkill: manifest?.appSkill,
                dependencies: manifest?.dependencies,
                ui: manifest?.ui,
            },
            panel: {
                pinned: !!app.pinned,
                accent: app.accent,
            },
            governance: appGovernanceForManifest(app, submission),
        },
    };
}

function appsToPackManifest(apps: AppEntry[], submissions: Record<string, AppPublishSubmission> = {}) {
    return {
        schema: 'maclaw.app.pack.v1',
        privateMarker: 'x_maclaw_apps',
        apps: apps.map((app) => appToManifest(app, submissions[app.id])),
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
    return {
        id: id.startsWith('market-') ? id : `market-${id}`,
        name,
        description: String(app.description || ''),
        category: String(app.category || 'Market'),
        kind,
        icon,
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
            datasrv: app.binding?.datasrv,
            appSkill: app.binding?.appSkill,
            dependencies: normalizeAppDependencies(app.binding?.dependencies),
            ui: normalizeAppWorkspaceLayout(app.binding?.ui, kind),
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

function backendDependenciesForApp(plan: BackendAppInstallPlan | null, appId: string) {
    return (plan?.dependencies || []).filter((dep) => !dep.app_ids?.length || dep.app_ids.includes(appId));
}

function hasMissingRequiredBackendDependency(plan: BackendAppInstallPlan | null, appIds: string[]) {
    const selected = new Set(appIds);
    return (plan?.dependencies || []).some((dep) => dep.required !== false && !dep.installed && (!dep.app_ids?.length || dep.app_ids.some((id) => selected.has(id))));
}

function backendDependencySummary(dep: BackendAppInstallDependency, text: typeof labels.zh) {
    const status = dep.installed ? text.installedDependency : text.missingDependency;
    const version = dep.version ? `@${dep.version}` : '';
    const required = dep.required === false ? '' : ' *';
    return `${status}: ${dep.id}${version}${required}`;
}
function installRecordMissingDependencyCount(record: BackendAppInstallRecord) {
    return (record.dependencies || []).filter((dep) => dep.required !== false && !dep.installed).length;
}

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
                    <SkillDiscoveryPanel discovery={skillDiscovery} apps={apps} lang={lang} onAddApp={onAddDiscoveredApp} />
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

const SkillDiscoveryPanel = ({ discovery, apps, lang, onAddApp }: { discovery: SkillAppDiscovery; apps: AppEntry[]; lang?: string; onAddApp: (app: AppEntry) => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const installedIds = new Set(apps.map((app) => app.id));
    const statusLabel = discovery.status === 'loading' ? text.datasrvLoading :
        discovery.status === 'error' ? text.datasrvError :
            discovery.status === 'ready' ? text.skillAppsReady : '-';
    return (
        <section className="apps-discovery" data-status={discovery.status === 'ready' ? 'ready' : discovery.status}>
            <div>
                <div className="apps-discovery__title">{text.skillApps}</div>
                <div className="apps-discovery__meta">maclaw.app.json / maclaw.apps.json · x_maclaw_apps</div>
                {discovery.error && <div className="apps-discovery__error">{discovery.error}</div>}
            </div>
            <div className="apps-discovery__status">{statusLabel}</div>
            {discovery.candidates.length > 0 && (
                <div className="apps-discovery__candidates">
                    {discovery.candidates.map((candidate) => {
                        const installed = installedIds.has(candidate.id);
                        return (
                            <div className="apps-discovery__candidate" key={candidate.id}>
                                <span className="apps-app-icon" style={{ '--apps-icon-color': candidate.accent } as CSSProperties}><Icon name={candidate.icon} /></span>
                                <div>
                                    <strong>{candidate.name}</strong>
                                    <span>{candidate.manifest?.skill?.id || candidate.category}</span>
                                </div>
                                <button className="apps-secondary-button" type="button" disabled={installed} onClick={() => onAddApp(candidate)}>
                                    {installed ? text.added : text.addToPanel}
                                </button>
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
                            <span className="apps-app-icon" style={{ '--apps-icon-color': candidate.accent } as CSSProperties}><Icon name={candidate.icon} /></span>
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
    const [skillAppSaveState, setSkillAppSaveState] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle');
    const [skillAppSaveMessage, setSkillAppSaveMessage] = useState('');
    const [skillMarketUploadState, setSkillMarketUploadState] = useState<'idle' | 'uploading' | 'uploaded' | 'error'>('idle');
    const [layoutTemplate, setLayoutTemplate] = useState<StudioLayoutTemplate>('document_workspace');
    const [layoutDensity, setLayoutDensity] = useState<StudioLayoutDensity>('comfortable');
    const [primaryRegion, setPrimaryRegion] = useState<StudioPrimaryRegion>('left');
    const [outputRegion, setOutputRegion] = useState<StudioOutputRegion>('right');
    const [appSkillID, setAppSkillID] = useState('');
    const [workflowSkillID, setWorkflowSkillID] = useState('');
    const [workflowSkillVersion, setWorkflowSkillVersion] = useState('1.0.0');
    const [dependencySource, setDependencySource] = useState<AppSkillDependency['source']>('hub');
    useEffect(() => {
        let cancelled = false;
        ListNLSkills()
            .then((skills: SkillSummary[] = []) => {
                if (cancelled) return;
                const appReadySkills = skills.filter((skill) => skill?.name && !skill.is_maclaw_app);
                setAvailableSkills(appReadySkills);
                setSelectedSkill((current) => current || String(appReadySkills[0]?.name || ''));
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
        setAppSkillID(isEnterpriseAppKind(nextKind) ? appSkillID.trim() : '');
        setWorkflowSkillID(isEnterpriseApprovalAppKind(nextKind) ? (workflowSkillID.trim() || 'approval-workflow') : '');
        setWorkflowSkillVersion('1.0.0');
        setDependencySource('hub');
        setCopyState('idle');
        setSkillAppSaveState('idle');
        setSkillAppSaveMessage('');
        setSkillMarketUploadState('idle');
        setLayoutTemplate(nextKind === 'tool_app' ? 'document_workspace' : 'classic_split');
        setLayoutDensity('comfortable');
        setPrimaryRegion('left');
        setOutputRegion(nextKind === 'tool_app' ? 'right' : 'bottom');
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
        const nextAppSkillID = isEnterpriseAppKind(nextKind) ? `${makeLocalAppId(nextName || 'enterprise-app')}-app` : '';
        setAppSkillID(nextAppSkillID);
        setWorkflowSkillID(isEnterpriseApprovalAppKind(nextKind) ? `${makeLocalAppId(nextName || 'approval')}-workflow` : '');
        setWorkflowSkillVersion('1.0.0');
        setDependencySource('hub');
        setDescription(prompt);
        setCopyState('idle');
    };
    const buildDraftApp = (id: string, cleanName = name.trim() || (zh ? '\u672a\u547d\u540d\u5e94\u7528' : 'Untitled app'), boundSkillID = id, appDefinitionFile = 'maclaw.apps.json'): AppEntry => {
        const enterpriseAppSkillID = isEnterpriseAppKind(kind) ? (appSkillID.trim() || `${id}-app`) : '';
        const workflowDependency: AppSkillDependency[] = isEnterpriseApprovalAppKind(kind) && workflowSkillID.trim()
            ? [{ id: workflowSkillID.trim(), version: workflowSkillVersion.trim() || undefined, kind: 'workflow_skill', required: true, source: dependencySource || 'hub', capabilities: ['approval.workflow'] }]
            : [];
        const manifest = isEnterpriseAppKind(kind)
            ? makeEnterpriseManifest(kind, 'custom', '', '', '', '', enterpriseAppSkillID, workflowDependency)
            : kind === 'automation_app'
                ? makeAutomationManifest()
                : makeSkillManifest(boundSkillID, inputMode, outputModes, skillFields, multipleFiles && inputMode !== 'form', appDefinitionFile);
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
            manifest: applyStudioWorkspaceLayout(manifest, kind, { template: layoutTemplate, density: layoutDensity, primaryRegion, outputRegion }),
        };
    };
    const draftApp = buildDraftApp(
        'draft-app',
        undefined,
        kind === 'tool_app' && selectedSkill ? selectedSkill : 'draft-app',
        kind === 'tool_app' && selectedSkill ? 'maclaw.app.json' : 'maclaw.apps.json',
    );
    const draftManifestText = JSON.stringify(appToManifest(draftApp), null, 2);
    const createApp = () => {
        const cleanName = name.trim();
        if (!cleanName) return;
        const id = makeLocalAppId(cleanName);
        onCreateApp(buildDraftApp(id, cleanName));
        setName('');
        setDescription('');
        setSkillFields([]);
        setMultipleFiles(false);
    };
    const persistSkillAppDefinition = async () => {
        const cleanName = name.trim();
        const skillID = selectedSkill.trim();
        if (!cleanName || !skillID || kind !== 'tool_app') return null;
        const appID = makeSkillAppDefinitionId(cleanName);
        const app = buildDraftApp(appID, cleanName, skillID, 'maclaw.app.json');
        const manifestText = JSON.stringify(appToManifest(app), null, 2);
        await SaveMaclawAppDefinitionForSkill(skillID, manifestText);
        onCreateApp({
            ...app,
            id: skillPanelAppID(skillID, appID),
            source: 'skill',
            manifest: makeSkillManifest(skillID, inputMode, outputModes, skillFields, multipleFiles && inputMode !== 'form', 'maclaw.app.json'),
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
        const cleanName = name.trim();
        if (!cleanName) return;
        const appID = makeSkillAppDefinitionId(cleanName);
        const panelApp: AppEntry = {
            ...buildDraftApp(appID, cleanName, skillID, 'maclaw.app.json'),
            id: skillPanelAppID(skillID, appID),
            source: 'skill',
            manifest: makeSkillManifest(skillID, inputMode, outputModes, skillFields, multipleFiles && inputMode !== 'form', 'maclaw.app.json'),
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
                    <label>{zh ? '\u7c7b\u578b' : 'Type'}</label>
                    <select value={kind} onChange={(event) => {
                        const nextKind = event.target.value as AppKind;
                        selectKind(nextKind);
                    }}>
                        <option value="enterprise_approval_app">{appKinds.enterprise_approval_app[zh ? 'zh' : 'en']}</option>
                        <option value="enterprise_normal_app">{appKinds.enterprise_normal_app[zh ? 'zh' : 'en']}</option>
                        <option value="tool_app">{appKinds.tool_app[zh ? 'zh' : 'en']}</option>
                        <option value="automation_app">{appKinds.automation_app[zh ? 'zh' : 'en']}</option>
                    </select>
                </div>
                <div className="apps-form-row">
                    <label>{zh ? '\u5206\u7c7b' : 'Category'}</label>
                    <input value={category} onChange={(event) => setCategory(event.target.value)} />
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
                            <select value={selectedSkill} onChange={(event) => {
                                setSelectedSkill(event.target.value);
                                setSkillAppSaveState('idle');
                                setSkillAppSaveMessage('');
                                setSkillMarketUploadState('idle');
                            }}>
                                {availableSkills.length === 0 && <option value="">{zh ? '\u6ca1\u6709\u53ef\u7528 Skill' : 'No skills available'}</option>}
                                {availableSkills.map((skill) => {
                                    const skillName = String(skill.name || '');
                                    return <option key={skillName} value={skillName}>{skillName}</option>;
                                })}
                            </select>
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
                    <section className="apps-layout-designer" aria-label="Capabilities and dependencies">
                        <div className="apps-preview-title-row">
                            <div className="apps-definition__title">Capabilities and dependencies</div>
                            <span className="apps-count">appSkill / workflow_skill</span>
                        </div>
                        <div className="apps-layout-designer__grid">
                            <div className="apps-form-row">
                                <label>App Skill</label>
                                <input data-testid="studio-app-skill-id" value={appSkillID} onChange={(event) => setAppSkillID(event.target.value)} placeholder={`${makeLocalAppId(name.trim() || 'enterprise-app')}-app`} />
                            </div>
                            {isEnterpriseApprovalAppKind(kind) && (
                                <>
                                    <div className="apps-form-row">
                                        <label>Approval workflow skill</label>
                                        <input data-testid="studio-workflow-skill-id" value={workflowSkillID} onChange={(event) => setWorkflowSkillID(event.target.value)} placeholder={`${makeLocalAppId(name.trim() || 'approval')}-workflow`} />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>Version</label>
                                        <input data-testid="studio-workflow-skill-version" value={workflowSkillVersion} onChange={(event) => setWorkflowSkillVersion(event.target.value)} placeholder="1.0.0" />
                                    </div>
                                    <div className="apps-form-row">
                                        <label>Dependency source</label>
                                        <select data-testid="studio-dependency-source" value={dependencySource || 'hub'} onChange={(event) => setDependencySource(event.target.value as AppSkillDependency['source'])}>
                                            <option value="hub">Hub</option>
                                            <option value="market">SkillMarket</option>
                                            <option value="local">Local</option>
                                            <option value="builtin">Built-in</option>
                                        </select>
                                    </div>
                                </>
                            )}
                        </div>
                    </section>
                )}
                <section className="apps-layout-designer" aria-label={zh ? '\u754c\u9762\u5e03\u5c40' : 'UI layout'}>
                    <div className="apps-preview-title-row">
                        <div className="apps-definition__title">{zh ? '\u754c\u9762\u5e03\u5c40' : 'UI layout'}</div>
                        <span className="apps-count">{zh ? '\u4fdd\u5b58\u5230 manifest' : 'Saved in manifest'}</span>
                    </div>
                    <div className="apps-layout-designer__grid">
                        <div className="apps-form-row">
                            <label>{zh ? '\u5e03\u5c40\u6a21\u677f' : 'Layout template'}</label>
                            <select data-testid="studio-layout-template" value={layoutTemplate} onChange={(event) => setLayoutTemplate(event.target.value as StudioLayoutTemplate)}>
                                <option value="document_workspace">{zh ? '\u6587\u6863\u5de5\u4f5c\u53f0' : 'Document workspace'}</option>
                                <option value="classic_split">{zh ? '\u7ecf\u5178\u5206\u680f' : 'Classic split'}</option>
                                <option value="left_nav">{zh ? '\u5de6\u4fa7\u5bfc\u822a' : 'Left navigation'}</option>
                            </select>
                        </div>
                        <div className="apps-form-row">
                            <label>{zh ? '\u5bc6\u5ea6' : 'Density'}</label>
                            <select data-testid="studio-layout-density" value={layoutDensity} onChange={(event) => setLayoutDensity(event.target.value as StudioLayoutDensity)}>
                                <option value="comfortable">{zh ? '\u6807\u51c6' : 'Comfortable'}</option>
                                <option value="compact">{zh ? '\u7d27\u51d1' : 'Compact'}</option>
                            </select>
                        </div>
                        <div className="apps-form-row">
                            <label>{zh ? '\u4e3b\u64cd\u4f5c\u533a' : 'Primary region'}</label>
                            <select data-testid="studio-primary-region" value={primaryRegion} onChange={(event) => setPrimaryRegion(event.target.value as StudioPrimaryRegion)}>
                                <option value="left">{zh ? '\u5de6\u4fa7' : 'Left'}</option>
                                <option value="center">{zh ? '\u4e2d\u95f4' : 'Center'}</option>
                                <option value="right">{zh ? '\u53f3\u4fa7' : 'Right'}</option>
                            </select>
                        </div>
                        <div className="apps-form-row">
                            <label>{zh ? '\u8f93\u51fa\u533a' : 'Output region'}</label>
                            <select data-testid="studio-output-region" value={outputRegion} onChange={(event) => setOutputRegion(event.target.value as StudioOutputRegion)}>
                                <option value="right">{zh ? '\u53f3\u4fa7' : 'Right'}</option>
                                <option value="bottom">{zh ? '\u5e95\u90e8' : 'Bottom'}</option>
                                <option value="modal">{zh ? '\u5f39\u7a97' : 'Modal'}</option>
                            </select>
                        </div>
                    </div>
                </section>
                <div className="apps-form-row">
                    <label>{zh ? '\u63cf\u8ff0' : 'Description'}</label>
                    <textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder={zh ? '\u7528\u4e8e tooltip \u548c\u53f3\u4fa7\u8fd0\u884c\u533a\u8bf4\u660e' : 'Used in tooltip and right runtime area'} />
                </div>
                <div className="apps-actions">
                    <button className="apps-primary-button" type="button" disabled={!name.trim()} onClick={createApp}>{text.createTab}</button>
                    {kind === 'tool_app' && (
                        <>
                            <button className="apps-secondary-button" type="button" disabled={!name.trim() || !selectedSkill.trim() || skillAppSaveState === 'saving'} onClick={() => void saveAsSkillApp()}>
                                {skillAppSaveState === 'saving' ? (zh ? '\u4fdd\u5b58\u4e2d...' : 'Saving...') : (zh ? '\u4fdd\u5b58\u5230 Skill' : 'Save to Skill')}
                            </button>
                            <button className="apps-secondary-button" type="button" disabled={!selectedSkill.trim() || skillMarketUploadState === 'uploading'} onClick={() => void uploadSelectedSkillApp()}>
                                {skillMarketUploadState === 'uploading' ? (zh ? '\u4e0a\u4f20\u4e2d...' : 'Uploading...') : (zh ? '\u4e0a\u4f20\u5230 SkillMarket' : 'Upload to SkillMarket')}
                            </button>
                        </>
                    )}
                </div>
                {skillAppSaveMessage && (
                    <div className="apps-skill-save-message" data-state={skillAppSaveState} role={skillAppSaveState === 'error' ? 'alert' : 'status'}>{skillAppSaveMessage}</div>
                )}
                <div className="apps-create-preview">
                    <div className="apps-preview-title-row">
                        <div className="apps-definition__title">{text.manifestPreview}</div>
                        <button className="apps-secondary-button" type="button" onClick={async () => {
                            await copyTextToClipboard(draftManifestText);
                            setCopyState('copied');
                        }}>{copyState === 'copied' ? text.copied : text.copy}</button>
                    </div>
                    <pre>{draftManifestText}</pre>
                </div>
            </section>
            <div className="apps-studio-grid">
                <StudioCard title={isZh(lang) ? '\u4f01\u4e1a\u5ba1\u6279\u578b\u5e94\u7528' : 'Approval app'} description={isZh(lang) ? '\u7528\u5bf9\u8bdd\u5b9a\u4e49\u6570\u636e\u5bf9\u8c61\u3001\u89c6\u56fe\u3001\u52a8\u4f5c\u548c\u9875\u9762\uff0c\u7531 Agent \u603b\u7ed3\u6210 App manifest\u3002' : 'Define entities, views, actions, and screens through chat; Agent writes an app manifest.'} selected={kind === 'enterprise_approval_app'} onSelect={() => selectKind('enterprise_approval_app', true)} />
                <StudioCard title={isZh(lang) ? '\u4f01\u4e1a\u666e\u901a\u5e94\u7528' : 'Business app'} description={isZh(lang) ? '\u5c06\u4f01\u4e1a\u540e\u53f0\u6570\u636e\u3001\u64cd\u4f5c\u548c\u67e5\u8be2\u5c01\u88c5\u6210\u53ef\u89c6\u5316\u5de5\u4f5c\u53f0\uff0c\u4e0d\u4ea7\u751f\u5ba1\u6279\u5b9e\u4f8b\u3002' : 'Wrap enterprise backend data, actions, and query views as a visual workbench without approval instances.'} selected={kind === 'enterprise_normal_app'} onSelect={() => selectKind('enterprise_normal_app', true)} />
                <StudioCard title={isZh(lang) ? '\u5de5\u5177\u5e94\u7528' : 'Tool app'} description={isZh(lang) ? '\u628a\u590d\u6742 skill \u56fa\u5b9a\u6210\u4e0a\u4f20\u3001\u53c2\u6570\u3001\u8f93\u51fa\u7684\u50bb\u74dc\u5f0f\u754c\u9762\u3002' : 'Wrap a complex skill as upload, parameters, and output UI.'} selected={kind === 'tool_app'} onSelect={() => selectKind('tool_app', true)} />
                <StudioCard title={isZh(lang) ? '\u81ea\u52a8\u5316\u5e94\u7528' : 'Automation app'} description={isZh(lang) ? '\u628a\u540c\u6b65\u3001\u91c7\u96c6\u3001\u76d1\u63a7\u7b49\u957f\u8fd0\u884c\u80fd\u529b\u56fa\u5b9a\u6210\u5e94\u7528\u5165\u53e3\u3002' : 'Expose sync, collection, and monitoring flows as app entries.'} selected={kind === 'automation_app'} onSelect={() => selectKind('automation_app', true)} />
            </div>
            <section className="apps-manifest-preview">
                <div className="apps-definition__title">{text.manifestPreview}</div>
                <pre>{JSON.stringify(skillManifestTemplate, null, 2)}</pre>
            </section>
        </>
    );
};

const skillManifestTemplate = {
    x_maclaw_apps: 'v1',
    apps: [
        {
            id: 'document-redaction',
            name: 'Document Redaction',
            description: 'Upload a document and return a redacted copy.',
            category: 'Document',
            icon: 'shield',
            input_mode: 'file',
            output_modes: ['docx', 'pdf'],
            fields: [
                { name: 'scope', label: 'Redaction scope', type: 'select', required: true, default: 'PII', options: ['PII', 'Financial', 'Custom'] },
            ],
        },
    ],
};

const StudioCard = ({ title, description, selected, onSelect }: { title: string; description: string; selected?: boolean; onSelect?: () => void }) => (
    <article className={`apps-studio-card ${selected ? 'is-active' : ''}`}>
        <h4>{title}</h4>
        <p>{description}</p>
        {onSelect && <button className="apps-primary-button" type="button" aria-pressed={selected} onClick={onSelect}>{title}</button>}
    </article>
);

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
    const hasTestEvidence = !!evidence || !!app.recentUsedAt;
    const dependencies = appDependencyEvidence(app);
    const hasRequiredDependencies = app.kind === 'automation_app' || dependencies.some((dep) => dep.required && dep.id);
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
            label: zh ? '\u8fd0\u884c\u8bc1\u636e' : 'Run evidence',
            ok: hasTestEvidence,
            detail: hasTestEvidence
                ? evidence
                    ? `${evidence.runID} · ${evidence.at}`
                    : (zh ? '\u5df2\u6709\u6700\u8fd1\u4f7f\u7528\u8bb0\u5f55' : 'Recent use recorded')
                : (zh ? '\u63d0\u4ea4\u5ba1\u6838\u524d\u5efa\u8bae\u5148\u8bd5\u8fd0\u884c\u4e00\u6b21' : 'Run the app once before review'),
        },
    ];
}

const PublishPane = ({ apps, lang, onFixApp }: { apps: AppEntry[]; lang?: string; onFixApp: (appId: string) => void }) => {
    const text = isZh(lang) ? labels.zh : labels.en;
    const zh = isZh(lang);
    const [copyState, setCopyState] = useState<'idle' | 'copied'>('idle');
    const [submissions, setSubmissions] = useState<Record<string, AppPublishSubmission>>(() => readPublishSubmissions());
    const [submittingAppId, setSubmittingAppId] = useState('');
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
        let submission: AppPublishSubmission | null = null;
        try {
            submission = await submitAppPackageToEnterpriseMarket(app, appsToPackManifest([app]));
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
                        return (
                            <article className="apps-publish-card" key={app.id} data-ready={ready ? 'true' : 'false'}>
                                <div className="apps-publish-card__head">
                                    <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><Icon name={app.icon} /></span>
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
                                const missingDependencyCount = item.dependencies.filter((dep) => dep.required !== false && !dep.installed).length;
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

type AppEditDraft = Pick<AppEntry, 'name' | 'description' | 'category' | 'icon' | 'accent'> & {
    inputMode: SkillInputMode;
    multipleFiles: boolean;
    outputModes: string[];
    fields: SkillAppField[];
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
    const emptyEditDraft: AppEditDraft = { name: '', description: '', category: '', icon: 'contract', accent: defaultAccentForKind('tool_app'), inputMode: 'file', multipleFiles: false, outputModes: ['docx', 'pdf'], fields: [] };
    const [editDraft, setEditDraft] = useState<AppEditDraft>(emptyEditDraft);
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
    const startEdit = (app: AppEntry) => {
        setEditingAppId(app.id);
        setEditDraft({
            name: app.name,
            description: app.description,
            category: app.category,
            icon: app.icon,
            accent: app.accent,
            inputMode: app.manifest?.skill?.inputMode || 'file',
            multipleFiles: !!app.manifest?.skill?.multipleFiles,
            outputModes: normalizeOutputModes(app.manifest?.skill?.outputModes),
            fields: normalizeSkillAppFields(app.manifest?.skill?.fields),
        });
    };
    useEffect(() => {
        if (!pendingEditAppId) return;
        const app = apps.find((item) => item.id === pendingEditAppId);
        if (!app) return;
        setManageQuery('');
        setManageCategory('all');
        startEdit(app);
        onPendingEditConsumed();
    }, [pendingEditAppId, apps, onPendingEditConsumed]);
    const cancelEdit = () => {
        setEditingAppId('');
        setEditDraft(emptyEditDraft);
    };
    useEffect(() => {
        if (editingAppId && !editingApp) cancelEdit();
    }, [editingAppId, editingApp]);
    const saveEdit = (app: AppEntry) => {
        const name = editDraft.name.trim();
        if (!name) return;
        const manifest = app.kind === 'tool_app'
            ? {
                ...(app.manifest || makeSkillManifest(app.id, editDraft.inputMode, editDraft.outputModes)),
                skill: {
                    id: app.manifest?.skill?.id || app.id,
                    appDefinitionFile: 'maclaw.apps.json' as const,
                    inputMode: editDraft.inputMode,
                    multipleFiles: editDraft.multipleFiles && editDraft.inputMode !== 'form',
                    outputModes: normalizeOutputModes(editDraft.outputModes),
                    fields: normalizeSkillAppFields(editDraft.fields),
                },
            }
            : app.manifest;
        onUpdateApp(app.id, {
            name,
            category: editDraft.category.trim() || (isZh(lang) ? '\u672a\u5206\u7c7b' : 'Uncategorized'),
            description: editDraft.description.trim(),
            icon: editDraft.icon,
            accent: editDraft.accent,
            version: nextAppVersion(app),
            manifest,
        });
        cancelEdit();
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
                        <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><Icon name={app.icon} /></span>
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
                            <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><Icon name={app.icon} /></span>
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
                <div className="apps-manage-edit-dialog" role="dialog" aria-modal="true" aria-labelledby="apps-manage-edit-title" onKeyDown={(event) => {
                    if (event.key === 'Escape') cancelEdit();
                }}>
                    <button className="apps-manage-edit-dialog__backdrop" type="button" aria-label={text.cancel} onClick={cancelEdit} />
                    <div className="apps-manage-edit-dialog__panel">
                        <div className="apps-manage-edit-dialog__header">
                            <div>
                                <div className="apps-definition__title" id="apps-manage-edit-title">{text.edit}</div>
                                <div className="apps-manage-edit-dialog__subtitle">{editingApp.name} · {editingApp.category}</div>
                            </div>
                            <button className="apps-icon-button" type="button" title={text.cancel} aria-label={text.cancel} onClick={cancelEdit}>×</button>
                        </div>
                        <div className="apps-manage-edit">
                            <div className="apps-form-row">
                                <label>{isZh(lang) ? '\u540d\u79f0' : 'Name'}</label>
                                <input value={editDraft.name} onChange={(event) => setEditDraft((current) => ({ ...current, name: event.target.value }))} />
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
                            <div className="apps-form-row">
                                <label>{text.appColor}</label>
                                <AppAccentPicker value={editDraft.accent} lang={lang} onChange={(accent) => setEditDraft((current) => ({ ...current, accent }))} />
                            </div>
                            <div className="apps-form-row">
                                <label>{isZh(lang) ? '\u63cf\u8ff0' : 'Description'}</label>
                                <textarea value={editDraft.description} onChange={(event) => setEditDraft((current) => ({ ...current, description: event.target.value }))} />
                            </div>
                            {editingApp.kind === 'tool_app' && (
                                <>
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
                            <div className="apps-actions">
                                <button className="apps-secondary-button" type="button" onClick={cancelEdit}>{text.cancel}</button>
                                <button className="apps-primary-button" type="button" disabled={!editDraft.name.trim()} onClick={() => saveEdit(editingApp)}>{text.save}</button>
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
    const [selectedInstallKeys, setSelectedInstallKeys] = useState<string[] | null>(null);
    const [confirmHighRiskInstall, setConfirmHighRiskInstall] = useState(false);
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
        try {
            void RecordMaclawAppInstall(JSON.stringify(appToManifest(app)), 'market').then(() => refreshInstallRecords()).catch(() => undefined);
        } catch {
            // Keep catalog one-click install responsive; advanced import surfaces audit errors.
        }
        onInstallApp(app);
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
            if (backendInstallPlanState === 'ready' && hasMissingRequiredBackendDependency(backendInstallPlan, selectedAppIDs)) {
                const dependencyInstallPlan = await InstallMaclawAppDependencies(manifestText);
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
                return;
            }
            const resultItems = plan.map((item): AppInstallResultItem => {
                const selected = (item.action === 'install' || item.action === 'upgrade') && selectedKeys.has(item.key);
                if (selected && item.action === 'install') {
                    return { key: item.key, name: item.app.name, icon: item.app.icon, accent: item.app.accent, action: 'installed', detail: text.installedCount };
                }
                if (selected && item.action === 'upgrade') {
                    return {
                        key: item.key,
                        name: item.app.name,
                        icon: item.app.icon,
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
                return { key: item.key, name: item.app.name, icon: item.app.icon, accent: item.app.accent, action: 'skipped', detail: reason };
            });
            const nextApps = plan.filter((item) => (item.action === 'install' || item.action === 'upgrade') && selectedKeys.has(item.key)).map((item) => item.app);
            const installedActionCount = plan.filter((item) => item.action === 'install' && selectedKeys.has(item.key)).length;
            const upgradedActionCount = plan.filter((item) => item.action === 'upgrade' && selectedKeys.has(item.key)).length;
            const skippedActionCount = plan.length - nextApps.length;
            if (nextApps.length > 0) {
                const installAuditPackage = nextApps.length === 1 ? appToManifest(nextApps[0]) : appsToPackManifest(nextApps);
                void RecordMaclawAppInstall(JSON.stringify(installAuditPackage), 'market').then(() => refreshInstallRecords()).catch(() => undefined);
            }
            nextApps.forEach(onInstallApp);
            setInstallMessage(installSummaryMessage(installedActionCount, upgradedActionCount, skippedActionCount, text));
            setInstallResultItems(resultItems);
            setInstallState('installed');
            setConfirmHighRiskInstall(false);
        } catch (error: any) {
            setInstallMessage(error?.message || text.installError);
            setInstallResultItems([]);
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
                    {marketRows.map(({ app, installed, upgrade, actionText }) => (
                        <div className="apps-market-row" key={app.id} data-state={installed ? 'installed' : upgrade ? 'upgrade' : 'available'}>
                            <span className="apps-app-icon" style={{ '--apps-icon-color': app.accent } as CSSProperties}><Icon name={app.icon} /></span>
                            <div className="apps-market-row__main">
                                <strong>{app.name}</strong>
                                <span>{app.description}</span>
                                <small>{app.category} · {appKinds[app.kind][isZh(lang) ? 'zh' : 'en']} · {sourceLabels[app.source][isZh(lang) ? 'zh' : 'en']}</small>
                            </div>
                            <button className={installed ? 'apps-secondary-button' : 'apps-primary-button'} type="button" disabled={installed} title={`${actionText}: ${app.name}`} aria-label={`${actionText}: ${app.name}`} onClick={() => void installSingleMarketApp(app)}>
                                {actionText}
                            </button>
                        </div>
                    ))}
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
                            const dependencyCount = (record.dependencies || []).length;
                            const missingCount = installRecordMissingDependencyCount(record);
                            const sha = String(record.package_sha || '').slice(0, 12) || '-';
                            return (
                                <div className="apps-install-record" key={`${record.package_sha || 'record'}:${record.installed_at || index}`} data-missing={missingCount > 0 ? 'true' : 'false'}>
                                    <div className="apps-install-record__main">
                                        <strong>{appNames}</strong>
                                        <span>{text.installedAt}: {formatInstallRecordTime(record.installed_at)} · {text.marketSource}: {record.source || '-'}</span>
                                        <small>{text.packageSha}: {sha} · {text.skillDependencies}: {dependencyCount} · {text.missingDependencyCount}: {missingCount}</small>
                                    </div>
                                    <em>{record.has_missing_required || missingCount > 0 ? text.missingDependency : text.installedDependency}</em>
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
                                    const dependencyText = backendInstallPlanState === 'ready' && backendDependencies.length > 0
                                        ? backendDependencies.map((dep) => backendDependencySummary(dep, text)).join(', ')
                                        : item.dependencies.map((dep) => dep.version ? `${dep.id}@${dep.version}` : dep.id).join(', ');
                                    const checkboxLabel = `${item.app.name} · ${statusText}`;
                                    return (
                                    <label className="apps-install-preview__row" key={item.key} data-action={checked ? 'install' : 'skip'} title={statusText}>
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
                                        <span className="apps-app-icon" style={{ '--apps-icon-color': item.app.accent } as CSSProperties}><Icon name={item.app.icon} /></span>
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
                        <div className={`apps-result-panel${installState === 'installed' && installResultItems.length > 0 ? ' apps-result-panel--stacked' : ''}`} data-state={installState === 'installed' ? 'done' : 'error'} role={installState === 'installed' ? 'status' : 'alert'} aria-live={installState === 'installed' ? 'polite' : undefined}>
                            <span>{installState === 'installed' ? installMessage : `${text.installError}: ${installMessage}`}</span>
                            {installState === 'installed' && installResultItems.length > 0 && (
                                <div className="apps-install-result" role="list" aria-label={text.installDetails}>
                                    {installResultItems.map((item) => (
                                        <div className="apps-install-result__row" data-action={item.action} role="listitem" key={item.key}>
                                            <span className="apps-app-icon" style={{ '--apps-icon-color': item.accent } as CSSProperties}><Icon name={item.icon} /></span>
                                            <strong>{item.name}</strong>
                                            <em>{item.action === 'upgraded' ? text.upgradedItem : item.action === 'installed' ? text.installedCount : text.skippedItem}</em>
                                            <small>{item.detail}</small>
                                        </div>
                                    ))}
                                </div>
                            )}
                        </div>
                    )}
                </div>
            </details>
        </>
    );
};

export default AppsPage;













