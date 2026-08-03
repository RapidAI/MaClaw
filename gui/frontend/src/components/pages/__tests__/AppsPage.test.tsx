import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const executeMaclawAppBusinessOperationMock = vi.hoisted(() => vi.fn());
const getMISDataConfigMock = vi.hoisted(() => vi.fn());
const listNLSkillsMock = vi.hoisted(() => vi.fn());
const listSkillAppManifestsMock = vi.hoisted(() => vi.fn());
const listMaclawAppInstallsMock = vi.hoisted(() => vi.fn());
const listMaclawAppApprovalInstancesMock = vi.hoisted(() => vi.fn());
const listMaclawAppApprovalInstancesAllMock = vi.hoisted(() => vi.fn());
const recordMaclawAppApprovalInstanceMock = vi.hoisted(() => vi.fn());
const startMaclawAppApprovalWorkflowMock = vi.hoisted(() => vi.fn());
const syncMaclawAppApprovalInstanceToDataSrvMock = vi.hoisted(() => vi.fn());
const installMaclawAppDependenciesMock = vi.hoisted(() => vi.fn());
const installMaclawAppPackageFromHubMock = vi.hoisted(() => vi.fn());
const installSelectedMaclawAppPackageFromHubMock = vi.hoisted(() => vi.fn());
const installMixedSkillMock = vi.hoisted(() => vi.fn());
const recordMaclawAppInstallMock = vi.hoisted(() => vi.fn());
const planMaclawAppInstallMock = vi.hoisted(() => vi.fn());
const reviewMaclawAppPackageMock = vi.hoisted(() => vi.fn<(args: unknown[]) => Promise<any>>(async () => ({ review_issues: [] })));
const checkMaclawAppRuntimeHealthMock = vi.hoisted(() => vi.fn(async (packageJSON: string, appID: string) => {
    const plan = await planMaclawAppInstallMock(packageJSON);
    const blocked = !!(plan?.has_missing_required || plan?.has_blocking_dependency || plan?.has_workflow_contract_issue);
    return {
        schema: 'maclaw.app.runtime_health.v1',
        ok: !blocked,
        blocked,
        message: blocked ? 'runtime dependencies blocked' : 'runtime dependencies ready',
        plan,
        app_id: appID,
        has_missing_required: !!plan?.has_missing_required,
        has_blocking_dependency: !!plan?.has_blocking_dependency,
        has_workflow_contract_issue: !!plan?.has_workflow_contract_issue,
    };
}));
const saveMaclawAppDefinitionForSkillMock = vi.hoisted(() => vi.fn());
const recordMaclawAppRunEvidenceForSkillMock = vi.hoisted(() => vi.fn());
const recordMaclawAppRunHistoryMock = vi.hoisted(() => vi.fn<(entry: any) => Promise<any>>(async (entry) => entry));
const listMaclawAppRunHistoryMock = vi.hoisted(() => vi.fn<(appID: string, limit: number) => Promise<any[]>>(async () => []));
const clearMaclawAppRunHistoryMock = vi.hoisted(() => vi.fn<(appID: string) => Promise<boolean>>(async () => true));
const uploadNLSkillToMarketMock = vi.hoisted(() => vi.fn());
const searchMixedSkillsMock = vi.hoisted(() => vi.fn());
const loadConfigMock = vi.hoisted(() => vi.fn());
const browserOpenURLMock = vi.hoisted(() => vi.fn());
const runNLSkillAsyncMock = vi.hoisted(() => vi.fn());
const getNLSkillRunStatusMock = vi.hoisted(() => vi.fn());
const getSkillRunArtifactMock = vi.hoisted(() => vi.fn());
const cancelNLSkillRunMock = vi.hoisted(() => vi.fn());
const stageSkillAppInputFileMock = vi.hoisted(() => vi.fn());
const openFileOrShowInFolderMock = vi.hoisted(() => vi.fn());
const downloadSkillRunArtifactMock = vi.hoisted(() => vi.fn());
const openSkillRunArtifactMock = vi.hoisted(() => vi.fn());
const revealSkillRunArtifactMock = vi.hoisted(() => vi.fn());
const showItemInFolderMock = vi.hoisted(() => vi.fn());
const openMaclawAppWorkspaceFromInstallMock = vi.hoisted(() => vi.fn(async (..._args: unknown[]) => ({ ok: true, opened: true })));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CancelNLSkillRun: (...args: unknown[]) => cancelNLSkillRunMock(...args),
    ExecuteMaclawAppBusinessOperation: (...args: unknown[]) => executeMaclawAppBusinessOperationMock(...args),
    GetMISDataConfig: (...args: unknown[]) => getMISDataConfigMock(...args),
    GetNLSkillRunStatus: (...args: unknown[]) => getNLSkillRunStatusMock(...args),
    GetSkillRunArtifact: (...args: unknown[]) => getSkillRunArtifactMock(...args),
    ListNLSkills: (...args: unknown[]) => listNLSkillsMock(...args),
    ListSkillAppManifests: (...args: unknown[]) => listSkillAppManifestsMock(...args),
    LoadMaclawAppsPanelState: async () => window.localStorage.getItem('maclaw:apps-panel:v1') || '',
    LoadConfig: (...args: unknown[]) => loadConfigMock(...args),
    ListMaclawAppInstalls: (...args: unknown[]) => listMaclawAppInstallsMock(...args),
    ListMaclawAppApprovalInstances: (...args: unknown[]) => listMaclawAppApprovalInstancesMock(...args),
    ListMaclawAppApprovalInstancesAll: (...args: unknown[]) => listMaclawAppApprovalInstancesAllMock(...args),
    RecordMaclawAppApprovalInstance: (...args: unknown[]) => recordMaclawAppApprovalInstanceMock(...args),
    StartMaclawAppApprovalWorkflow: (...args: unknown[]) => startMaclawAppApprovalWorkflowMock(...args),
    SyncMaclawAppApprovalInstanceToDataSrv: (...args: unknown[]) => syncMaclawAppApprovalInstanceToDataSrvMock(...args),
    DownloadSkillRunArtifact: (...args: unknown[]) => downloadSkillRunArtifactMock(...args),
    OpenFileOrShowInFolder: (...args: unknown[]) => openFileOrShowInFolderMock(...args),
    InstallMaclawAppDependencies: (...args: unknown[]) => installMaclawAppDependenciesMock(...args),
    InstallMaclawAppPackageFromHub: (...args: unknown[]) => installMaclawAppPackageFromHubMock(...args),
    InstallSelectedMaclawAppPackageFromHub: (...args: unknown[]) => installSelectedMaclawAppPackageFromHubMock(...args),
    InstallMixedSkill: (...args: unknown[]) => installMixedSkillMock(...args),
    PlanMaclawAppInstall: (...args: unknown[]) => planMaclawAppInstallMock(...args),
    ReviewMaclawAppPackage: (...args: unknown[]) => reviewMaclawAppPackageMock(args),
    RecordMaclawAppInstall: (...args: unknown[]) => recordMaclawAppInstallMock(...args),
    RecordMaclawAppRunHistory: (entry: unknown) => recordMaclawAppRunHistoryMock(entry),
    SaveMaclawAppsPanelState: async (state: string) => { window.localStorage.setItem('maclaw:apps-panel:v1', state); },
    ListMaclawAppRunHistory: (appID: string, limit: number) => listMaclawAppRunHistoryMock(appID, limit),
    ClearMaclawAppRunHistory: (appID: string) => clearMaclawAppRunHistoryMock(appID),
    // Prefers runtime health API; default implementation wraps PlanMaclawAppInstall mocks.
    CheckMaclawAppRuntimeHealth: (...args: unknown[]) => checkMaclawAppRuntimeHealthMock(...(args as [string, string])),
    // Keep approval/business workspace openers undefined so runtime falls back to
    // StartMaclawAppApprovalWorkflow / ExecuteMaclawAppBusinessOperation in tests.
    OpenMaclawAppApprovalWorkspace: undefined,
    OpenMaclawAppBusinessWorkspace: undefined,
    OpenMaclawAppWorkspaceFromInstall: (...args: unknown[]) => openMaclawAppWorkspaceFromInstallMock(...args),
    OpenSkillRunArtifact: (...args: unknown[]) => openSkillRunArtifactMock(...args),
    RecordMaclawAppRunEvidenceForSkill: (...args: unknown[]) => recordMaclawAppRunEvidenceForSkillMock(...args),
    RevealSkillRunArtifact: (...args: unknown[]) => revealSkillRunArtifactMock(...args),
    RunNLSkillAsync: (...args: unknown[]) => runNLSkillAsyncMock(...args),
    SaveMaclawAppDefinitionForSkill: (...args: unknown[]) => saveMaclawAppDefinitionForSkillMock(...args),
    SearchMixedSkills: (...args: unknown[]) => searchMixedSkillsMock(...args),
    ShowItemInFolder: (...args: unknown[]) => showItemInFolderMock(...args),
    StageSkillAppInputFile: (...args: unknown[]) => stageSkillAppInputFileMock(...args),
    UploadNLSkillToMarket: (...args: unknown[]) => uploadNLSkillToMarketMock(...args),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: (...args: unknown[]) => browserOpenURLMock(...args),
}));

import { AppsPage } from '../AppsPage';
import { miniAppLabels } from '../../../i18n/maclawMiniAppLabels';

const marketManifestPlaceholder = /(?:Paste app package JSON \(maclaw\.app\.v1 \/ maclaw\.app\.pack\.v1 \/ maclaw\.apps\.json\)|粘贴应用包 JSON（maclaw\.app\.v1 \/ maclaw\.app\.pack\.v1 \/ maclaw\.apps\.json）)/;
const manageManifestTitle = /^(?:Manifest|清单)$/;
const runHistoryStorageKey = 'maclaw:apps-run-history:v1';
const activeRunStorageKey = 'maclaw:apps-active-runs:v1';

function getStudioButton() {
    return screen.queryByTitle('Create App')
        || screen.queryByTitle('创建应用')
        || screen.queryByTitle('建立應用')
        || screen.queryByTitle(miniAppLabels.studio.en)
        || screen.queryByTitle(miniAppLabels.studio.zhHans)
        || screen.queryByTitle(miniAppLabels.studio.zhHant)
        || screen.getByRole('button', { name: new RegExp(`Create App|创建应用|建立應用|${miniAppLabels.studio.en}|${miniAppLabels.studio.zhHans}|${miniAppLabels.studio.zhHant}`) });
}

async function findRuntimeGovernancePanel() {
    const existing = screen.queryByLabelText('Install governance');
    if (existing) return existing;
    screen.queryAllByRole('button', { name: 'Show details' }).forEach((button) => {
        fireEvent.click(button);
    });
    return screen.findByLabelText('Install governance');
}

function getManageTab() {
    return screen.queryByRole('tab', { name: /\u5e94\u7528\u7ba1\u7406|\u61c9\u7528\u7ba1\u7406|Manage/ })
        || screen.queryByRole('button', { name: /\u5e94\u7528\u7ba1\u7406|\u61c9\u7528\u7ba1\u7406|Manage apps/ })
        || screen.getByText(/\u5e94\u7528\u7ba1\u7406|\u61c9\u7528\u7ba1\u7406/);
}

function getCreateTab() {
    return screen.queryByRole('tab', { name: /Create app|\u521b\u5efa\u5e94\u7528|\u5efa\u7acb\u61c9\u7528/ })
        || screen.getByText(/\u521b\u5efa\u5e94\u7528|\u5efa\u7acb\u61c9\u7528/);
}

function getPublishTab() {
    return screen.queryByText('\u5ba1\u6838/\u53d1\u5e03')
        || screen.queryByText('\u7a3d\u6838/\u91cb\u51fa')
        || screen.getByRole('tab', { name: /Review \/ publish|\u5ba1\u6838\/\u53d1\u5e03|\u7a3d\u6838\/\u91cb\u51fa/ });
}

function getMarketTab() {
    return screen.queryByRole('button', { name: /App Market|\u5e94\u7528\u5e02\u573a|\u61c9\u7528\u5e02\u5834/ })
        || screen.queryByText('App Market')
        || screen.queryByText(/\u5e94\u7528\u5e02\u573a|\u61c9\u7528\u5e02\u5834/)
        || screen.getByText(/\u5e94\u7528\u5e02\u573a|\u61c9\u7528\u5e02\u5834/);
}

function getCreateAppNameInput() {
    return screen.queryByPlaceholderText('\u4f8b\uff1a\u5408\u540c\u5f52\u6863')
        || screen.queryByPlaceholderText('\u4f8b\uff1a\u5408\u540c\u6b78\u6a94')
        || screen.getByPlaceholderText('Example: Contract filing');
}

/** Primary create action in App Studio (not the studio entry button or create tab). */
function getCreateLocalAppButton() {
    const byPanelOnly = screen.queryByRole('button', { name: /仅添加到面板|僅新增到面板|Add to panel only/ });
    if (byPanelOnly) return byPanelOnly;
    const primary = document.querySelector('.apps-actions .apps-primary-button') as HTMLButtonElement | null;
    if (primary && /创建应用|建立應用|Create app/i.test(primary.textContent || '')) return primary;
    const matches = screen.getAllByRole('button', { name: /创建应用|建立應用|Create app/ });
    const action = matches.find((node) => node.classList.contains('apps-primary-button'));
    if (action) return action;
    throw new Error('local app create button not found');
}

function clickCreateLocalApp() {
    fireEvent.click(getCreateLocalAppButton());
}

function getDraftPromptInput() {
    return screen.getByPlaceholderText('\u4f8b\uff1a\u505a\u4e00\u4e2a\u5408\u540c\u5f52\u6863\u5e94\u7528\uff0c\u4e0a\u4f20 Word/PDF\uff0c\u8f93\u51fa\u5f52\u6863\u7f16\u53f7\u548c\u5ba1\u6838\u7ed3\u679c');
}

function stableStringify(value: any): string {
    if (Array.isArray(value)) return `[${value.map((item) => stableStringify(item)).join(',')}]`;
    if (value && typeof value === 'object') {
        // Mirror AppsPage.stableStringify: undefined-valued keys are skipped so
        // fingerprints survive the JSON round-trip to the backend package.
        return `{${Object.keys(value).sort().filter((key) => value[key] !== undefined).map((key) => `${stableJSONString(key)}:${stableStringify(value[key])}`).join(',')}}`;
    }
    return typeof value === 'string' ? stableJSONString(value) : JSON.stringify(value);
}

// Mirror AppsPage.stableJSONString: Go escapes U+2028/U+2029, JS does not.
function stableJSONString(value: string): string {
    return JSON.stringify(value).replace(/\u2028/g, '\\u2028').replace(/\u2029/g, '\\u2029');
}

function textHash(value: string): string {
    let hash = 2166136261;
    // Mirror AppsPage.textHash: code points (Go runes), not UTF-16 code units.
    for (const char of value) {
        hash ^= char.codePointAt(0) as number;
        hash = Math.imul(hash, 16777619);
    }
    return (hash >>> 0).toString(16).padStart(8, '0');
}

function normalizeTestAppVersion(value: unknown) {
    const version = Number(value);
    return Number.isFinite(version) && version > 0 ? Math.floor(version) : 1;
}

function testNormalizeOutputModes(outputModes?: string[]) {
    const allowed = ['docx', 'xlsx', 'pdf', 'json', 'txt'];
    const normalized = (Array.isArray(outputModes) ? outputModes : []).map((item) => String(item || '').trim().toLowerCase()).filter((item) => allowed.includes(item));
    return normalized.length > 0 ? Array.from(new Set(normalized)) : ['docx', 'pdf'];
}

function testNormalizeSkillAppFields(fields?: any[]) {
    if (!Array.isArray(fields)) return [];
    const normalized: any[] = [];
    for (const field of fields) {
        const name = String(field?.name || '').trim();
        if (!name) continue;
        const type = field.type === 'select' || field.type === 'boolean' ? field.type : 'text';
        const options = Array.isArray(field.options) ? field.options.map((option: any) => String(option || '').trim()).filter(Boolean) : [];
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

// Mirror AppsPage.appSkillRuntimeBinding: packaging fills binding defaults, and
// the backend hashes the packaged binding.
function testAppSkillRuntimeBinding(manifest: any): any {
    if (!manifest) return undefined;
    const existing = manifest.skill;
    const skillID = String(existing?.id || manifest.appSkill?.id || '').trim();
    if (!skillID) return existing;
    const outputModes = testNormalizeOutputModes(existing?.outputModes || manifest.resultContract?.outputModes);
    return {
        ...existing,
        id: skillID,
        appDefinitionFile: existing?.appDefinitionFile || 'maclaw.app.json',
        inputMode: existing?.inputMode || 'form',
        multipleFiles: existing?.multipleFiles || false,
        outputModes,
        fields: testNormalizeSkillAppFields(existing?.fields || []),
    };
}

function testAppDefinitionFingerprint(app: any): string {
    const manifest = app.manifest;
    // Mirror AppsPage.appDefinitionFingerprint: entryKind is excluded because the
    // backend derives the fingerprint from the serialized package (no entryKind),
    // and ui/skill are normalized the same way appToManifest writes them into
    // the package.
    const ui = testAppWorkspaceUIForManifest(manifest?.ui, app.kind);
    const skill = testAppSkillRuntimeBinding(manifest);
    const runtimeManifest = manifest ? {
        schema: manifest.schema,
        installUnit: manifest.installUnit,
        privateMarker: manifest.privateMarker,
        launchMode: manifest.launchMode,
        ...(manifest.datasrv ? { datasrv: manifest.datasrv } : {}),
        ...(manifest.mis ? { mis: manifest.mis } : {}),
        ...(skill ? { skill } : {}),
        ...(manifest.appSkill ? { appSkill: manifest.appSkill } : {}),
        ...(manifest.dependencies ? { dependencies: manifest.dependencies } : {}),
        ...(ui ? { ui } : {}),
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
        ...(app.customIconDataUrl ? { customIconDataUrl: app.customIconDataUrl } : {}),
        version: normalizeTestAppVersion(app.version),
        manifest: runtimeManifest,
    }));
}

function testToolAppResultContract(outputModes = ['docx', 'pdf']) {
    return {
        schema: 'maclaw.app.result.v1',
        primary: outputModes.includes('json') || outputModes.includes('txt') ? 'content' : 'artifact',
        types: ['content', 'document', 'artifact'],
        outputModes,
        approvalDecisions: undefined,
        delivery: { inlineContent: true, artifacts: true, businessRecord: false, notifications: false },
    };
}

function testToolAppProtocol(outputModes = ['docx', 'pdf']) {
    const contract = testToolAppResultContract(outputModes);
    const protocol = {
        schema: 'maclaw.app.test_protocol.v1',
        sampleInput: { file: 'sample.pdf', params: '' },
        expectedOutput: { status: 'ok', primary: contract.primary },
        requiredRoles: [],
        requiredScopes: [],
        riskLevel: 'low',
    };
    return { ...protocol, fingerprint: textHash(stableStringify(protocol)) };
}

function testProtocolWithFingerprint(protocol: any) {
    return { ...protocol, fingerprint: textHash(stableStringify(protocol)) };
}

function testWorkspaceLayoutFingerprint(app: any) {
    const entry = app?.manifest?.ui?.entry || (app?.kind === 'enterprise_approval_app' ? 'approval_workspace' : app?.kind === 'enterprise_normal_app' ? 'business_workspace' : 'tool_workspace');
    const layout = app?.manifest?.ui?.layouts?.[entry] || {};
    const regions = Array.isArray(layout.regions) ? [...layout.regions] : [];
    const orderedRegions = regions
        .sort((left, right) => (left.order || regions.indexOf(left) + 1) - (right.order || regions.indexOf(right) + 1))
        .map((region, index) => ({
            id: region.id,
            role: region.role,
            placement: region.placement,
            visible: region.visible !== false,
            order: region.order || index + 1,
        }));
    return textHash(stableStringify({
        entry,
        template: layout.template || (app?.kind === 'tool_app' ? 'document_workspace' : 'classic_split'),
        density: layout.density || 'comfortable',
        primaryRegion: layout.primaryRegion || 'left',
        outputRegion: layout.outputRegion || (app?.kind === 'tool_app' ? 'right' : 'bottom'),
        regions: orderedRegions,
    }));
}

function testAppTestProtocolFingerprint(app: any) {
    const raw = app?.manifest?.testProtocol || {};
    const { fingerprint: _fingerprint, ...protocol } = raw;
    const normalized = {
        schema: 'maclaw.app.test_protocol.v1',
        sampleInput: protocol.sampleInput || protocol.sample_input || {},
        expectedOutput: protocol.expectedOutput || protocol.expected_output || protocol.expectedResult || protocol.expected_result || {},
        requiredRoles: Array.isArray(protocol.requiredRoles || protocol.required_roles) ? protocol.requiredRoles || protocol.required_roles : [],
        requiredScopes: Array.isArray(protocol.requiredScopes || protocol.required_scopes) ? protocol.requiredScopes || protocol.required_scopes : [],
        riskLevel: protocol.riskLevel || protocol.risk_level || (app?.kind === 'tool_app' ? 'low' : 'medium'),
    } as Record<string, unknown>;
    if (app?.kind === 'enterprise_approval_app') {
        normalized.workflowRequiredInputs = Array.isArray(protocol.workflowRequiredInputs || protocol.workflow_required_inputs)
            ? protocol.workflowRequiredInputs || protocol.workflow_required_inputs
            : ['record_ref', 'applicant', 'business_payload'];
        normalized.workflowDecisionOutputs = Array.isArray(protocol.workflowDecisionOutputs || protocol.workflow_decision_outputs)
            ? protocol.workflowDecisionOutputs || protocol.workflow_decision_outputs
            : ['approved', 'rejected', 'attention'];
        normalized.workflowRequiredOutputs = Array.isArray(protocol.workflowRequiredOutputs || protocol.workflow_required_outputs)
            ? protocol.workflowRequiredOutputs || protocol.workflow_required_outputs
            : ['workflow_result', 'approval_instance', 'outputs', 'artifacts'];
    }
    return textHash(stableStringify(normalized));
}

function testStudioRegionsForLayout(kind: string, template: string, primaryRegion: string, outputRegion: string) {
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

function testDefaultWorkspaceLayoutForKind(kind: string) {
    const base = { schema: 'maclaw.app.ui.v1', generated: true };
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

// Mirror AppsPage.appWorkspaceUIForManifest: the definition fingerprint hashes
// the packaged (normalized) ui, not the raw in-memory layout.
function testAppWorkspaceUIForManifest(ui: any, kind: string): any {
    if (!ui) return ui;
    const normalizedUI = ui?.schema === 'maclaw.app.ui.v1'
        ? { ...ui }
        : testAppWorkspaceUIForManifest(testDefaultWorkspaceLayoutForKind(kind), kind);
    const entry = normalizedUI.entry || (kind === 'enterprise_approval_app' ? 'approval_workspace' : kind === 'enterprise_normal_app' ? 'business_workspace' : 'tool_workspace');
    const layouts = normalizedUI.layouts || {};
    const layout = layouts[entry] || {};
    const fallbackTemplate = kind === 'tool_app' ? 'document_workspace' : 'classic_split';
    const fallbackOutputRegion = kind === 'tool_app' ? 'right' : 'bottom';
    const template = ['classic_split', 'left_nav', 'document_workspace', 'dashboard'].includes(String(layout.template)) ? layout.template : fallbackTemplate;
    const density = ['compact', 'comfortable', 'spacious'].includes(String(layout.density)) ? layout.density : 'comfortable';
    const primaryRegion = ['left', 'center', 'right'].includes(String(layout.primaryRegion)) ? layout.primaryRegion : 'left';
    const outputRegion = ['right', 'bottom', 'modal'].includes(String(layout.outputRegion)) ? layout.outputRegion : fallbackOutputRegion;
    const allowedPlacements = new Set(['left', 'center', 'right', 'bottom', 'modal']);
    const rawRegions = (Array.isArray(layout.regions) ? layout.regions : []).reduce((regions: any[], item: any) => {
        const raw = item && typeof item === 'object' ? item : {};
        const id = String(raw.id || '').trim();
        const role = String(raw.role || '').trim();
        const placement = String(raw.placement || '').trim();
        if (!id || !role || !allowedPlacements.has(placement)) return regions;
        const region: any = { id, role, placement };
        if (raw.visible === false) region.visible = false;
        const order = Number(raw.order);
        if (Number.isFinite(order) && order > 0) region.order = Math.floor(order);
        regions.push(region);
        return regions;
    }, []);
    const normalized = rawRegions.length ? rawRegions : testStudioRegionsForLayout(kind, template, primaryRegion, outputRegion);
    const regions = [...normalized]
        .sort((a, b) => (a.order || normalized.indexOf(a) + 1) - (b.order || normalized.indexOf(b) + 1))
        .map((region, index) => ({ ...region, order: region.order || index + 1 }));
    const fingerprint = textHash(stableStringify({
        entry,
        template,
        density,
        primaryRegion,
        outputRegion,
        regions: regions.map((region: any, index: number) => ({
            id: region.id,
            role: region.role,
            placement: region.placement,
            visible: region.visible !== false,
            order: region.order || index + 1,
        })),
    }));
    return {
        ...normalizedUI,
        entry,
        layouts: {
            ...layouts,
            [entry]: {
                ...layout,
                template,
                density,
                primaryRegion,
                outputRegion,
                regions,
                fingerprint,
            },
        },
    };
}

function normalizeTestAppForFingerprint(app: any) {
    if (!app?.manifest) return app;
    const manifest = { ...app.manifest };
    if (!manifest.testProtocol && app.kind === 'enterprise_normal_app') {
        const primary = manifest.resultContract?.primary || 'business_status';
        manifest.testProtocol = testProtocolWithFingerprint({
            schema: 'maclaw.app.test_protocol.v1',
            sampleInput: { business_payload: { note: 'sample' } },
            expectedOutput: { business_status: 'ready', primary },
            requiredRoles: ['operator'],
            requiredScopes: [],
            riskLevel: 'medium',
        });
    }
    if (!manifest.testProtocol && app.kind === 'enterprise_approval_app') {
        const primary = manifest.resultContract?.primary || 'approval_result';
        manifest.testProtocol = testProtocolWithFingerprint({
            schema: 'maclaw.app.test_protocol.v1',
            sampleInput: { record_ref: 'sample-record', applicant: 'current_user', business_payload: { amount: 1280 } },
            expectedOutput: { approval_result: 'approved', primary },
            requiredRoles: ['applicant', 'approver'],
            requiredScopes: [],
            riskLevel: 'medium',
        });
    }
    const ui = manifest.ui ? { ...manifest.ui, layouts: { ...(manifest.ui.layouts || {}) } } : undefined;
    if (ui && app.kind === 'enterprise_normal_app') {
        const layout = { ...(ui.layouts.business_workspace || {}) };
        if (!Array.isArray(layout.regions)) {
            layout.regions = [
                { id: 'operation_form', role: 'input', placement: layout.primaryRegion || 'left' },
                { id: 'record_list', role: 'record_list', placement: layout.template === 'left_nav' ? 'left' : 'center' },
                { id: 'record_detail', role: 'detail', placement: 'center' },
                { id: 'output_panel', role: 'output', placement: layout.outputRegion || 'bottom' },
            ];
        }
        ui.layouts.business_workspace = layout;
        manifest.ui = ui;
    }
    if (ui && app.kind === 'enterprise_approval_app') {
        const layout = { ...(ui.layouts.approval_workspace || {}) };
        if (!Array.isArray(layout.regions)) {
            layout.regions = [
                { id: 'request_form', role: 'input', placement: layout.primaryRegion || 'left' },
                { id: 'approval_inbox', role: 'instance_list', placement: layout.template === 'left_nav' ? 'left' : 'center' },
                { id: 'approval_detail', role: 'detail', placement: 'center' },
                { id: 'result_panel', role: 'output', placement: layout.outputRegion || 'bottom' },
            ];
        }
        ui.layouts.approval_workspace = layout;
        manifest.ui = ui;
    }
    return { ...app, manifest };
}

type SeedRunOptions = {
    runID?: string;
    at?: string;
    outputMode?: string;
    inputSummary?: string;
    message?: string;
    artifacts?: any[];
    resultPayload?: Record<string, unknown>;
    outputs?: any[];
    resultCoverage?: Record<string, unknown>;
    workspaceLayoutFingerprint?: string;
    dependencyVerification?: any;
    approvalInstance?: any;
};

function testAppManifestID(app: any) {
    const dataSrvAppID = String(app?.manifest?.datasrv?.appID || '').trim();
    if (app?.source === 'datasrv' && dataSrvAppID) return dataSrvAppID;
    const appID = String(app?.id || '').trim();
    if (app?.source === 'datasrv' && appID.startsWith('datasrv-installed-')) return appID.slice('datasrv-installed-'.length);
    return appID;
}

function testAppIdentityIDs(app: any) {
    return Array.from(new Set([
        testAppManifestID(app),
        app?.id,
        app?.manifest?.datasrv?.appID,
    ].map((id) => String(id || '').trim()).filter(Boolean)));
}

function testDeclaredSkillDependencies(app: any) {
    const manifest = app?.manifest || {};
    const deps = Array.isArray(manifest.dependencies?.skills) ? manifest.dependencies.skills : [];
    const appSkill = manifest.appSkill?.id ? [{ id: manifest.appSkill.id, version: manifest.appSkill.version, kind: 'app_skill', required: true, source: manifest.appSkill.source || 'local' }] : [];
    const boundSkill = manifest.skill?.id ? [{ id: manifest.skill.id, kind: 'runtime_skill', required: true, source: 'hub' }] : [];
    const approvalWorkflowSkills = Array.isArray(manifest.mis?.approvalBindings)
        ? manifest.mis.approvalBindings.map((binding: any) => ({ id: binding.workflowSkillId, version: binding.workflowVersion, kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }))
        : [];
    const merged = new Map<string, any>();
    [...appSkill, ...boundSkill, ...deps, ...approvalWorkflowSkills].forEach((dep: any) => {
        const id = String(dep?.id || '').trim();
        if (!id) return;
        const existing = merged.get(id);
        if (!existing) {
            merged.set(id, { ...dep, id });
            return;
        }
        merged.set(id, {
            ...existing,
            version: existing.version || dep.version,
            kind: existing.kind || dep.kind,
            required: existing.required !== false || dep.required !== false,
            source: existing.source || dep.source,
            capabilities: Array.from(new Set([...(existing.capabilities || []), ...(Array.isArray(dep.capabilities) ? dep.capabilities : [])])),
        });
    });
    return Array.from(merged.values());
}

function testDependencyVerificationForApp(app: any) {
    const appID = testAppManifestID(app);
    const appIDs = testAppIdentityIDs(app);
    const dependencies = testDeclaredSkillDependencies(app).map((dep: any) => ({
        id: dep.id,
        version: dep.version,
        kind: dep.kind || 'runtime_skill',
        required: dep.required !== false,
        source: dep.source || 'hub',
        install_ref: dep.install_ref || dep.installRef,
        installed: true,
        health: 'ready',
        action: 'skip',
        app_ids: appIDs.length ? appIDs : undefined,
        capabilities: dep.capabilities,
    }));
    return {
        schema: 'maclaw.app.install_plan.v1',
        verifiedAt: '2026-06-17T00:04:00.000Z',
        appCount: appID ? 1 : 0,
        dependencyCount: dependencies.length,
        hasMissingRequired: false,
        hasBlockingDependency: false,
        hasWorkflowContractIssue: false,
        workflowContractIssueCount: 0,
        hasGovernanceReviewIssue: false,
        governanceReviewIssueCount: 0,
        dependencies,
    };
}

function seedSuccessfulLocalAppRun(app: any, options: SeedRunOptions = {}) {
    const normalizedApp = JSON.parse(JSON.stringify(normalizeTestAppForFingerprint(app)));
    const panelRaw = window.localStorage.getItem('maclaw:apps-panel:v1');
    if (panelRaw && normalizedApp?.id) {
        const panel = JSON.parse(panelRaw) as { customApps?: any[] };
        if (Array.isArray(panel.customApps)) {
            panel.customApps = panel.customApps.map((item) => item?.id === normalizedApp.id ? normalizedApp : item);
            window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify(panel));
        }
    }
    const evidenceApp = normalizedApp?.manifest?.resultContract ? {
        ...normalizedApp,
        manifest: {
            ...normalizedApp.manifest,
            resultContract: Object.prototype.hasOwnProperty.call(normalizedApp.manifest.resultContract, 'approvalDecisions')
                ? { ...normalizedApp.manifest.resultContract, approvalDecisions: normalizedApp.manifest.resultContract.approvalDecisions }
                : normalizedApp.manifest.resultContract,
        },
    } : normalizedApp;
    const artifacts = options.artifacts || [];
    const primaryArtifact = artifacts[0] || {};
    const dependencyVerification = Object.prototype.hasOwnProperty.call(options, 'dependencyVerification')
        ? options.dependencyVerification
        : testDependencyVerificationForApp(evidenceApp);
    const raw = window.localStorage.getItem(runHistoryStorageKey) || '{}';
    const history = JSON.parse(raw) as Record<string, any[]>;
    history[app.id] = [{
        runID: options.runID || `run-ok-${app.id}`,
        appID: app.id,
        status: 'done',
        definitionHash: testAppDefinitionFingerprint(evidenceApp),
        testProtocolFingerprint: testAppTestProtocolFingerprint(evidenceApp),
        workspaceLayoutFingerprint: options.workspaceLayoutFingerprint || testWorkspaceLayoutFingerprint(evidenceApp),
        outputMode: options.outputMode || 'pdf',
        inputSummary: options.inputSummary || 'sample.pdf',
        message: options.message || 'done',
        artifactID: primaryArtifact.id,
        artifactURI: primaryArtifact.uri,
        artifactName: primaryArtifact.name,
        artifactPath: primaryArtifact.path,
        artifactDownloadState: primaryArtifact.download_state,
        artifacts,
        resultPayload: options.resultPayload,
        outputs: options.outputs,
        resultCoverage: options.resultCoverage,
        dependencyVerification,
        approvalInstance: options.approvalInstance,
        at: options.at || '2026-06-17T00:05:00.000Z',
    }, ...(history[app.id] || [])];
    window.localStorage.setItem(runHistoryStorageKey, JSON.stringify(history));
}

function latestStoredCustomApp(name: string) {
    const panel = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}') as { customApps?: any[] };
    return (panel.customApps || []).find((app) => app.name === name);
}

async function createAndRunLocalToolApp(name = '合同归档') {
    fireEvent.click(document.querySelector('.apps-studio-button') as HTMLElement);
    fireEvent.change(screen.getByPlaceholderText('\u4f8b\uff1a\u5408\u540c\u5f52\u6863'), { target: { value: name } });
    clickCreateLocalApp();
    // Panel persistence is asynchronous (SQLite-backed): wait until the new app
    // is durable before reading it back for mock configuration.
    await waitFor(() => expect(latestStoredCustomApp(name)).toBeTruthy());
    const createdApp = latestStoredCustomApp(name);
    if (createdApp?.id) {
        planMaclawAppInstallMock.mockImplementation(async () => {
            const currentApp = latestStoredCustomApp(name) || createdApp;
            const dependencyEvidence = testDependencyVerificationForApp(currentApp);
            return {
                schema: 'maclaw.app.install_plan.v1',
                apps: [{ id: currentApp.id, name: currentApp.name, kind: currentApp.kind }],
                dependencies: dependencyEvidence.dependencies,
                dependency_count: dependencyEvidence.dependencies.length,
                has_missing_required: false,
                has_blocking_dependency: false,
                has_workflow_contract_issue: false,
                workflow_contract_issue_count: 0,
                has_governance_review_issue: false,
                governance_review_issue_count: 0,
            };
        });
    }
    fireEvent.click(screen.getAllByText(name)[0]);
    const fileInput = document.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
    const file = new File(['demo'], 'sample.pdf', { type: 'application/pdf' });
    fireEvent.change(fileInput, { target: { files: [file] } });
    fireEvent.click(screen.getByText('\u6267\u884c'));
    await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalled());
    await waitFor(() => {
        const raw = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
        const entries = Object.values(raw).flat();
        expect(entries.some((entry) => entry.runID === 'run-test-1' && entry.status === 'done')).toBe(true);
    });
    if (createdApp?.id) {
        const raw = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
        const currentApp = latestStoredCustomApp(name) || createdApp;
        const verified = testDependencyVerificationForApp(currentApp);
        const verifiedIDs = new Set((verified.dependencies || []).map((dep: any) => String(dep.id || '').trim()).filter(Boolean));
        raw[createdApp.id] = (raw[createdApp.id] || []).map((entry) => ({
            ...entry,
            dependencyVerification: testDeclaredSkillDependencies(currentApp).every((dep: any) => verifiedIDs.has(String(dep.id || '').trim()))
                ? verified
                : entry.dependencyVerification,
        }));
        window.localStorage.setItem(runHistoryStorageKey, JSON.stringify(raw));
        seedSuccessfulLocalAppRun(currentApp, {
            runID: 'run-test-1',
            outputMode: 'pdf',
            inputSummary: 'sample.pdf',
            message: 'done',
            artifacts: [{ id: 'artifact-run-test-1', uri: 'artifact://skill-run/run-test-1/artifact-run-test-1', name: 'sample.pdf', status: 'ready' }],
            outputs: [{ kind: 'artifact', title: 'Generated PDF', artifact_id: 'artifact-run-test-1', status: 'ready' }],
            resultPayload: { status: 'done', artifact_id: 'artifact-run-test-1' },
            resultCoverage: { coveredTypes: ['artifact', 'document'], missingTypes: [] },
            dependencyVerification: verified,
        });
        const refreshed = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
        refreshed[createdApp.id] = (refreshed[createdApp.id] || []).map((entry, index) => entry.runID === 'run-test-1'
            ? {
                ...entry,
                dependencyVerification: verified,
                // The seeded display entry doubles as a same-runID decoy: fingerprints are
                // now JSON-round-trip stable, so mark it explicitly stale to keep evidence
                // selection on the real run entry (previously implied by an undefined-key
                // asymmetry between in-memory and persisted app manifests).
                ...(index === 0 ? { definitionHash: '00000000' } : {}),
            }
            : entry);
        window.localStorage.setItem(runHistoryStorageKey, JSON.stringify(refreshed));
    }
}
function seedSuccessfulSkillAppRun(skillID = 'invoice-review', name = '发票审核') {

    const appID = `skill-app-${skillID}-app-tool-app`;
    const panel = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}') as { customApps?: any[] };
    const storedApp = (panel.customApps || []).find((item) => item.id === appID)
        || (panel.customApps || []).find((item) => item.manifest?.skill?.id === skillID);
    const app = storedApp ? {
        ...storedApp,
        manifest: {
            ...storedApp.manifest,
            resultContract: storedApp.manifest?.resultContract
                ? { ...storedApp.manifest.resultContract, approvalDecisions: undefined }
                : testToolAppResultContract(['docx', 'pdf']),
        },
    } : {
        id: appID,
        name,
        description: miniAppLabels.defaultStudioDescription.zhHans,
        category: '文档处理',
        kind: 'tool_app',
        icon: 'shield',
        version: 1,
        manifest: {
            schema: 'maclaw.app.v1',
            installUnit: 'skill',
            privateMarker: 'x_maclaw_apps',
            entryKind: 'tool_app',
            launchMode: 'fixed_skill_ui',
            appSkill: { id: skillID, version: '1.0.0' },
            ui: {
                schema: 'maclaw.app.ui.v1',
                generated: true,
                entry: 'tool_workspace',
                layouts: {
                    tool_workspace: {
                        type: 'tool_workspace',
                        toolbar: ['add_file', 'run', 'cancel', 'open_output'],
                        template: 'document_workspace',
                        density: 'comfortable',
                        primaryRegion: 'left',
                        outputRegion: 'right',
                        regions: [
                            { id: 'file_queue', role: 'input', placement: 'left' },
                            { id: 'settings_panel', role: 'parameters', placement: 'right' },
                            { id: 'preview_panel', role: 'preview', placement: 'center' },
                            { id: 'output_panel', role: 'output', placement: 'right' },
                        ],
                        studio: {
                            editable: true,
                            savedInManifest: true,
                            updatedBy: 'app_studio',
                        },
                    },
                },
            },
            skill: {
                id: skillID,
                appDefinitionFile: 'maclaw.app.json',
                inputMode: 'file',
                multipleFiles: false,
                outputModes: ['docx', 'pdf'],
                fields: [],
            },
            resultContract: testToolAppResultContract(['docx', 'pdf']),
            testProtocol: testToolAppProtocol(['docx', 'pdf']),
        },
    };
    const historyAppID = app.id || appID;
    const definitionHash = testAppDefinitionFingerprint(app);
    window.localStorage.setItem(runHistoryStorageKey, JSON.stringify({
        [historyAppID]: [{
            runID: 'run-ok-1',
            appID: historyAppID,
            status: 'done',
            definitionHash,
            outputMode: 'pdf',
            inputSummary: 'sample.pdf',
            message: 'done',
            artifactID: 'artifact-sample-pdf',
            artifactURI: 'artifact://skill-run/run-ok-1/artifact-sample-pdf',
            artifactName: 'sample-output.pdf',
            artifacts: [{ id: 'artifact-sample-pdf', uri: 'artifact://skill-run/run-ok-1/artifact-sample-pdf', name: 'sample-output.pdf', status: 'ready' }],
            outputs: [{ kind: 'artifact', title: 'Sample PDF', artifact_id: 'artifact-sample-pdf', status: 'ready' }],
            resultPayload: { status: 'ok' },
            at: new Date().toISOString(),
        }],
    }));
}

function dynamicApprovalApp() {
    return normalizeTestAppForFingerprint({
        id: 'expense',
        name: '报销申请',
        description: '提交费用报销并运行审批工作流',
        category: '财务审批',
        kind: 'enterprise_approval_app',
        icon: 'receipt',
        accent: '#2f7d68',
        pinned: true,
        source: 'datasrv',
        manifest: {
            schema: 'maclaw.app.v1',
            installUnit: 'enterprise_app_pack',
            privateMarker: 'x_maclaw_apps',
            entryKind: 'enterprise_approval_app',
            launchMode: 'agent_dynamic_ui',
            appSkill: { id: 'expense-approval-app', version: '1.0.0', source: 'local' },
            datasrv: { appID: 'expense', domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report', blueprintID: 'finance.expense.approval', preferredAction: 'finance.expense_upsert', preferredView: 'finance.expense_review' },
            workflow: { schema: 'maclaw.app.workflow.v1', submitNode: 'expense.submit', approvalNode: 'expense.manager_review', resultNode: 'expense.result_feedback', attentionNode: 'expense.attention', statusMapping: { pending: 'approval_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' } },
            mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'expense-approval-workflow', workflowVersion: '1.0.0', objectRole: 'expense_report' }] },
            dependencies: { skills: [{ id: 'expense-approval-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }] },
            ui: { schema: 'maclaw.app.ui.v1', generated: true, entry: 'approval_workspace', layouts: { approval_workspace: { template: 'dashboard', density: 'comfortable', primaryRegion: 'left', outputRegion: 'bottom' } } },
            resultContract: { schema: 'maclaw.app.result.v1', primary: 'approval_result', types: ['approval_result', 'business_status', 'document', 'artifact', 'content'], approvalDecisions: ['approved', 'rejected', 'attention'], delivery: { inlineContent: true, artifacts: true, businessRecord: true, notifications: true } },
        },
    });
}

function dynamicBusinessApp() {
    return normalizeTestAppForFingerprint({
        id: 'purchase-inbound',
        name: '采购入库',
        description: '处理采购入库和库存记录',
        category: 'supply',
        kind: 'enterprise_normal_app',
        icon: 'warehouse',
        accent: '#4668a8',
        pinned: true,
        source: 'datasrv',
        manifest: {
            schema: 'maclaw.app.v1',
            installUnit: 'enterprise_app_pack',
            privateMarker: 'x_maclaw_apps',
            entryKind: 'enterprise_normal_app',
            launchMode: 'agent_dynamic_ui',
            appSkill: { id: 'purchase-inbound-app', version: '1.0.0', source: 'local' },
            datasrv: { appID: 'purchase-inbound', domain: 'supply', datasetID: 'supply.purchase_orders', objectRole: 'purchase_order', preferredAction: 'purchase_order.upsert', preferredView: 'purchase_order.list', preferredReport: 'purchase_order.report' },
            dependencies: { skills: [{ id: 'purchase-inbound-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub' }] },
            ui: { schema: 'maclaw.app.ui.v1', generated: true, entry: 'business_workspace', layouts: { business_workspace: { template: 'left_nav', density: 'comfortable', primaryRegion: 'left', outputRegion: 'bottom' } } },
            resultContract: { schema: 'maclaw.app.result.v1', primary: 'business_status', types: ['business_status', 'content', 'artifact'], delivery: { inlineContent: true, artifacts: true, businessRecord: true, notifications: false } },
        },
    });
}

function dynamicToolApp(id: string, name: string, category = '文档处理', icon = 'pdf', outputModes = ['docx', 'pdf']) {
    return normalizeTestAppForFingerprint({
        id,
        name,
        description: `${name} 工具应用`,
        category,
        kind: 'tool_app',
        icon,
        accent: '#7a5c95',
        source: 'local',
        manifest: {
            schema: 'maclaw.app.v1',
            installUnit: 'skill',
            privateMarker: 'x_maclaw_apps',
            entryKind: 'tool_app',
            launchMode: 'agent_dynamic_ui',
            skill: { id, appDefinitionFile: 'maclaw.apps.json', inputMode: 'file', outputModes },
            appSkill: { id, version: '1.0.0', source: 'local' },
            ui: { schema: 'maclaw.app.ui.v1', generated: true, entry: 'tool_workspace', layouts: { tool_workspace: { template: 'document_workspace', density: 'comfortable', primaryRegion: 'left', outputRegion: 'right', regions: [{ id: 'file_input', role: 'input', placement: 'left' }, { id: 'preview', role: 'preview', placement: 'center' }, { id: 'result_panel', role: 'output', placement: 'right' }] } } },
            resultContract: testToolAppResultContract(outputModes),
            testProtocol: testToolAppProtocol(outputModes),
        },
    });
}

function dynamicAutomationApp() {
    return normalizeTestAppForFingerprint({
        id: 'data-sync',
        name: '数据同步',
        description: '同步外部系统数据',
        category: '数据工具',
        kind: 'automation_app',
        icon: 'sync',
        accent: '#6f7b3f',
        source: 'local',
        manifest: {
            schema: 'maclaw.app.v1',
            installUnit: 'enterprise_app_pack',
            privateMarker: 'x_maclaw_apps',
            entryKind: 'automation_app',
            launchMode: 'automation_console',
            appSkill: { id: 'data-sync-skill', version: '1.0.0', source: 'local' },
            resultContract: { schema: 'maclaw.app.result.v1', primary: 'business_status', types: ['business_status', 'content'], delivery: { inlineContent: true, artifacts: false, businessRecord: false, notifications: true } },
        },
    });
}

const testDynamicApps = [
    dynamicApprovalApp(),
    dynamicBusinessApp(),
    dynamicToolApp('inventory-count', '库存盘点', '库存管理', 'inventory', ['json']),
    dynamicToolApp('contract-review', '合同审查', '法务工具', 'contract', ['docx', 'pdf']),
    dynamicToolApp('sheet-analysis', '表格分析', '数据工具', 'sheet', ['xlsx', 'json']),
    dynamicToolApp('pdf-to-word', 'PDF 转 Word', '文档处理', 'pdf', ['docx', 'pdf']),
    dynamicToolApp('document-redaction', '文档脱敏', '文档处理', 'shield', ['pdf']),
    dynamicToolApp('invoice-audit', '发票审核', '财务审批', 'invoice', ['json', 'pdf']),
    dynamicToolApp('web-capture', '网页采集', '数据工具', 'web', ['html', 'json']),
    dynamicAutomationApp(),
];

function seedDynamicAppsPanel(apps = testDynamicApps) {
    window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
        orderedIds: apps.map((app) => app.id),
        pinnedIds: apps.filter((app) => app.pinned).map((app) => app.id),
        customApps: apps,
        recentUsedAtById: {},
    }));
}

function seedStaleRunHistoryArtifact() {
    window.localStorage.setItem(runHistoryStorageKey, JSON.stringify({
        'invoice-audit': [{
            runID: 'run-ok-1',
            appID: 'invoice-audit',
            status: 'done',
            outputMode: 'pdf',
            inputSummary: 'sample.pdf',
            message: 'done',
            artifactID: 'artifact-sample-pdf',
            artifactURI: 'artifact://skill-run/run-ok-1/artifact-sample-pdf',
            artifactName: 'sample-output.pdf',
            artifactPath: 'C:\\bad\\missing.pdf',
            artifacts: [{ id: 'artifact-sample-pdf', uri: 'artifact://skill-run/run-ok-1/artifact-sample-pdf', name: 'sample-output.pdf', path: 'C:\\bad\\missing.pdf', status: 'ready' }],
            at: new Date().toISOString(),
        }],
    }));
}

function seedFailedRunHistoryArtifact() {
    window.localStorage.setItem(runHistoryStorageKey, JSON.stringify({
        'invoice-audit': [{
            runID: 'run-failed-1',
            appID: 'invoice-audit',
            status: 'error',
            outputMode: 'pdf',
            inputSummary: 'sample.pdf',
            message: 'failed',
            artifactID: 'artifact-failed-pdf',
            artifactURI: 'artifact://skill-run/run-failed-1/artifact-failed-pdf',
            artifactName: 'failed-output.pdf',
            artifactPath: 'C:\\bad\\failed.pdf',
            artifacts: [{ id: 'artifact-failed-pdf', uri: 'artifact://skill-run/run-failed-1/artifact-failed-pdf', name: 'failed-output.pdf', path: 'C:\\bad\\failed.pdf', status: 'ready' }],
            at: new Date().toISOString(),
        }],
    }));
}

async function openStaleRunHistoryItem(action: '打开' | '定位') {
    render(<AppsPage lang="zh-Hans" />);
    fireEvent.click(screen.getAllByText('发票审核')[0]);
    const artifactNodes = await screen.findAllByText('sample-output.pdf');
    const historyItem = artifactNodes.map((node) => node.closest('article')).find(Boolean) as HTMLElement;
    fireEvent.click(within(historyItem).getByRole('button', { name: action }));
}

describe('AppsPage', () => {
    beforeEach(() => {
        window.localStorage.clear();
        seedDynamicAppsPanel();
        executeMaclawAppBusinessOperationMock.mockReset().mockResolvedValue({ synced: true, mode: 'business_action', target: 'datasrv.action', result_status: 'done', response: { status: 'done' } });
        getMISDataConfigMock.mockReset().mockResolvedValue({ enabled: false, endpoint: 'http://127.0.0.1:18180' });
        listNLSkillsMock.mockReset().mockResolvedValue([]);
        listSkillAppManifestsMock.mockReset().mockResolvedValue([]);
        listMaclawAppInstallsMock.mockReset().mockResolvedValue([]);
        listMaclawAppApprovalInstancesMock.mockReset().mockResolvedValue([]);
        listMaclawAppApprovalInstancesAllMock.mockReset().mockResolvedValue([]);
        recordMaclawAppApprovalInstanceMock.mockReset().mockImplementation(async (payload) => ({ ...payload, instance_id: payload.instance_id || 'appr-test-1', updated_at: '2026-06-19T00:00:00Z' }));
        startMaclawAppApprovalWorkflowMock.mockReset().mockImplementation(async (input) => {
            const runID = input.run_workflow_skill === false ? (input.record_id || 'approval-start-ui') : await runNLSkillAsyncMock(input.workflow_skill_id, input.workflow_run_args || {});
            const status = input.run_workflow_skill === false ? {} : await getNLSkillRunStatusMock(runID);
            let parsed: any = {};
            try {
                const snippet = status?.summary?.last_output_snippet || status?.summary?.last_error_snippet || '';
                parsed = snippet && String(snippet).trim().startsWith('{') ? JSON.parse(String(snippet)) : {};
            } catch {
                parsed = {};
            }
            const failed = String(status?.status || '').toLowerCase() === 'failed';
            const resultPayload = { ...(input.result_payload || {}), ...parsed };
            const approvalResult = failed ? 'attention' : String(resultPayload.approval_result || resultPayload.decision || 'approved');
            const finalStatus = approvalResult === 'rejected' ? 'rejected' : approvalResult === 'attention' ? 'attention' : 'approved';
            const outputs = [...(status?.outputs || []), ...(resultPayload.outputs || [])];
            const artifacts = [...(status?.artifacts || []), ...(resultPayload.artifacts || [])];
            const workflowMapping = input.workflow_run_args?.workflow_mapping || {};
            const pendingNode = input.current_node || workflowMapping.approvalNode || 'approval_node';
            const pendingNodeIDs = input.current_node_ids || [pendingNode];
            const pendingEvents = [
                { node: workflowMapping.submitNode || 'submit', action: 'submitted' },
                { node: pendingNode, action: 'workflow_started' },
            ];
            const pending = {
                instance_id: 'appr-test-1', app_id: input.app_id, app_name: input.app_name, title: input.title,
                lane: 'my_requests', status: 'pending', current_node: pendingNode, current_node_ids: pendingNodeIDs, workflow_node_ids: pendingNodeIDs,
                owner: input.owner, applicant: input.applicant, approver: input.approver, submitted_by: input.applicant,
                current_assignee: input.current_assignee, current_assignee_type: input.current_assignee_type,
                workflow_skill_id: input.workflow_skill_id, workflow_version: input.workflow_version, workflow_decision_id: runID,
                approval_event: input.approval_event, approval_workflow_id: input.approval_event || input.workflow_skill_id,
                approval_object_role: input.object_role, object_role: input.object_role, dataset_id: input.dataset_id, blueprint_id: input.blueprint_id,
                record_id: input.record_id, approval_id: 'approval-start-ui', record_approval_id: 'approval-start-ui',
                result: input.business_note || 'Pending approval', business_status: input.business_status || 'approval_pending', result_status: input.result_status || 'pending',
                from_status: input.from_status || 'submitted', to_status: input.to_status || input.business_status || 'approval_pending',
                result_payload: input.result_payload, detail_url: `skill-run://${runID}`, updated_at: '2026-06-19T00:00:00Z',
                events: pendingEvents,
            };
            await recordMaclawAppApprovalInstanceMock(pending);
            const pendingSync = await syncMaclawAppApprovalInstanceToDataSrvMock({ dataset_id: input.dataset_id, object_role: input.object_role, app_id: input.app_id, blueprint_id: input.blueprint_id, record_id: input.record_id, instance: pending });
            if (pendingSync?.approval_id || pendingSync?.record_approval_id) {
                pending.approval_id = pendingSync.approval_id || pendingSync.record_approval_id;
                pending.record_approval_id = pendingSync.record_approval_id || pendingSync.approval_id;
            }
            const progressNode = workflowMapping.approvalNode || pendingNode;
            const progressInstance = {
                ...pending,
                current_node: progressNode,
                current_node_ids: [workflowMapping.submitNode || 'submit', progressNode].filter(Boolean),
                workflow_node_ids: [workflowMapping.submitNode || 'submit', progressNode].filter(Boolean),
                status: 'running',
                result_status: 'running',
                business_status: input.business_status || 'approval_pending',
                result: 'manager review started',
                outputs: [{ type: 'content', title: 'Workflow Progress', text: 'manager review started', status: 'running' }],
                events: [...pendingEvents, { node: progressNode, action: 'workflow_progress', message: 'manager review started' }],
            };
            const finalInstance = {
                ...pending, ...(resultPayload.approval_instance || {}), lane: finalStatus === 'attention' ? 'attention' : 'handled', status: finalStatus,
                result: failed ? (status?.error || status?.summary?.last_error_snippet || 'workflow failed') : (resultPayload.text || status?.summary?.last_output_snippet || 'approved'),
                business_status: resultPayload.business_status || (failed ? 'workflow_error' : pending.business_status),
                result_status: resultPayload.result_status || (failed ? 'workflow_error' : finalStatus),
                result_payload: failed ? { ...resultPayload, approval_result: 'attention', business_status: 'workflow_error', result_status: 'workflow_error', text: status?.error || status?.summary?.last_error_snippet || 'workflow failed', workflow_lifecycle: 'error' } : resultPayload,
                current_node_ids: [(resultPayload.approval_instance || {}).current_node || pending.current_node],
                workflow_node_ids: [(resultPayload.approval_instance || {}).current_node || pending.current_node],
                outputs: failed ? [{ kind: 'approval_result', text: status?.error || status?.summary?.last_error_snippet || 'workflow failed', status: 'workflow_error' }] : outputs,
                artifacts,
                events: failed ? [{ action: 'workflow_failed', message: status?.error || status?.summary?.last_error_snippet || 'workflow failed' }] : [{ action: 'workflow_completed', message: 'workflow completed' }],
            };
            await recordMaclawAppApprovalInstanceMock(finalInstance);
            const finalSync = await syncMaclawAppApprovalInstanceToDataSrvMock({ dataset_id: input.dataset_id, object_role: input.object_role, app_id: input.app_id, blueprint_id: input.blueprint_id, record_id: finalInstance.record_id || input.record_id, instance: finalInstance });
            if (finalSync?.approval_id || finalSync?.record_approval_id) {
                finalInstance.approval_id = finalSync.approval_id || finalSync.record_approval_id;
                finalInstance.record_approval_id = finalSync.record_approval_id || finalSync.approval_id;
            }
            return { started: true, approval_id: finalInstance.approval_id, workflow_skill_id: input.workflow_skill_id, workflow_version: input.workflow_version, instance: pending, workflow_run: { ran: true, workflow_skill_id: input.workflow_skill_id, progress_instances: [progressInstance], instance: finalInstance } };
        });
        syncMaclawAppApprovalInstanceToDataSrvMock.mockReset().mockResolvedValue({ synced: true });
        installMaclawAppDependenciesMock.mockReset().mockResolvedValue({ schema: 'maclaw.app.install_plan.v1', apps: [], dependencies: [], has_missing_required: false });
        installMaclawAppPackageFromHubMock.mockReset().mockResolvedValue({});
        installSelectedMaclawAppPackageFromHubMock.mockReset().mockResolvedValue({});
        installMixedSkillMock.mockReset().mockResolvedValue(undefined);
        recordMaclawAppInstallMock.mockReset().mockResolvedValue({ schema: 'maclaw.app.installs.v1', app_count: 1 });
        planMaclawAppInstallMock.mockReset().mockResolvedValue({ schema: 'maclaw.app.install_plan.v1', apps: [], dependencies: [], has_missing_required: false });
        checkMaclawAppRuntimeHealthMock.mockReset().mockImplementation(async (packageJSON: string, appID: string) => {
            const plan = await planMaclawAppInstallMock(packageJSON);
            const blocked = !!(plan?.has_missing_required || plan?.has_blocking_dependency || plan?.has_workflow_contract_issue);
            return {
                schema: 'maclaw.app.runtime_health.v1',
                ok: !blocked,
                blocked,
                message: blocked ? 'runtime dependencies blocked' : 'runtime dependencies ready',
                plan,
                app_id: appID,
                has_missing_required: !!plan?.has_missing_required,
                has_blocking_dependency: !!plan?.has_blocking_dependency,
                has_workflow_contract_issue: !!plan?.has_workflow_contract_issue,
            };
        });
        recordMaclawAppRunHistoryMock.mockReset().mockImplementation(async (entry: any) => entry);
        reviewMaclawAppPackageMock.mockReset().mockResolvedValue({ review_issues: [] });
        listMaclawAppRunHistoryMock.mockReset().mockResolvedValue([]);
        clearMaclawAppRunHistoryMock.mockReset().mockResolvedValue(true);
        saveMaclawAppDefinitionForSkillMock.mockReset().mockResolvedValue({ app_definition_file: 'maclaw.app.json' });
        recordMaclawAppRunEvidenceForSkillMock.mockReset().mockResolvedValue({ app_definition_file: 'maclaw.app.json' });
        uploadNLSkillToMarketMock.mockReset().mockResolvedValue('submission-app-1');
        searchMixedSkillsMock.mockReset().mockResolvedValue([]);
        loadConfigMock.mockReset().mockResolvedValue({ remote_hub_url: 'https://hub.example.com', remote_machine_id: 'machine-1', remote_machine_token: 'token-1' });
        browserOpenURLMock.mockReset();
        runNLSkillAsyncMock.mockReset().mockResolvedValue('run-test-1');
        getNLSkillRunStatusMock.mockReset().mockResolvedValue({ run_id: 'run-test-1', status: 'success', summary: { last_output_snippet: 'done' } });
        getSkillRunArtifactMock.mockReset().mockResolvedValue(null);
        cancelNLSkillRunMock.mockReset().mockResolvedValue(undefined);
        openFileOrShowInFolderMock.mockReset().mockResolvedValue(undefined);
        downloadSkillRunArtifactMock.mockReset().mockResolvedValue({ available: true });
        openSkillRunArtifactMock.mockReset().mockResolvedValue(undefined);
        revealSkillRunArtifactMock.mockReset().mockResolvedValue(undefined);
        showItemInFolderMock.mockReset().mockResolvedValue(undefined);
        openMaclawAppWorkspaceFromInstallMock.mockReset().mockResolvedValue({ ok: true, opened: true });
        stageSkillAppInputFileMock.mockReset().mockImplementation(async (name: string, type: string, lastModified: number) => ({
            name,
            size: 4,
            type,
            last_modified: lastModified,
            staged_path: `/tmp/${name}`,
            transfer: 'staged_file',
        }));
        vi.stubGlobal('fetch', vi.fn());
        Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    });
    afterEach(() => {
        delete (window as any).go;
        vi.unstubAllGlobals();
        cleanup();
    });

    it('renders the app panel with search, category filter, pinned apps, and app studio entry', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        expect(screen.getByPlaceholderText('\u641c\u7d22\u5e94\u7528')).not.toBeNull();
        const studioEntry = getStudioButton();
        expect(studioEntry).not.toBeNull();
        expect(studioEntry.textContent).toBe('\u521b\u5efa\u5e94\u7528');
        expect(studioEntry.closest('.apps-search-row')).not.toBeNull();
        expect(studioEntry.closest('.apps-ops')).toBeNull();
        expect(container.querySelector('.apps-ops__item--studio')).toBeNull();
        expect(container.querySelector('.apps-studio-button__icon svg:not(.apps-studio-button__plus)')).not.toBeNull();
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('\u6587\u6863\u5904\u7406 (2)')).not.toBeNull();
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('\u5168\u90e8\u5e94\u7528 (10)')).not.toBeNull();
        expect(screen.getAllByText('\u5e38\u7528\u5e94\u7528').length).toBeGreaterThan(0);
        expect(container.querySelectorAll('.apps-app-tile').length).toBeGreaterThan(6);
    });

    it('opens MIS data settings from the DataSrv discovery summary', () => {
        const openMISDataSettings = vi.fn();
        render(<AppsPage lang="zh-Hans" onOpenMISDataSettings={openMISDataSettings} />);

        fireEvent.click(getStudioButton());
        const summaryButton = screen.getByRole('button', { name: /DataSrv \u80fd\u529b\u53d1\u73b0.*MIS \u6570\u636e\u8bbe\u7f6e/ });
        expect(within(summaryButton).getByText('\u8bbe\u7f6e')).not.toBeNull();
        fireEvent.click(summaryButton);

        expect(openMISDataSettings).toHaveBeenCalledTimes(1);
    });

    it('renders the app panel operation section without changing app category counts', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        expect(screen.getByText('\u64cd\u4f5c')).not.toBeNull();
        expect(screen.getByText('\u5ba1\u6279\u72b6\u6001')).not.toBeNull();
        expect(screen.getByText('\u8fd0\u884c\u8bb0\u5f55')).not.toBeNull();
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('\u5168\u90e8\u5e94\u7528 (10)')).not.toBeNull();
    });
    it('opens global approval management from the operation section', async () => {
        listMaclawAppApprovalInstancesAllMock.mockResolvedValue([{
            appID: 'expense',
            appName: 'Expense approval',
            instanceId: 'approval-global-1',
            title: 'Travel expense',
            lane: 'pending_my_approval',
            status: 'pending',
            workflowNodeIDs: ['manager_approval', 'finance_review'],
            owner: 'alice',
            approver: 'manager',
            updatedAt: '2026-06-20T00:00:00Z',
            result: 'waiting',
            workflowSkillId: 'expense-approval-workflow',
            approvalWorkflowId: 'expense_approval',
            currentAssignee: 'manager',
            currentAssigneeType: 'user',
            datasetID: 'finance.expenses',
            objectRole: 'expense_report',
            approvalId: 'approval-remote-global-1',
            recordId: 'EXP-1',
            resultPayload: { approval_result: 'pending', business_status: 'approval_pending' },
            outputs: [{ kind: 'approval_result', title: 'Decision', text: 'waiting', status: 'pending' }],
        }]);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByText('\u5ba1\u6279\u72b6\u6001'));

        await waitFor(() => expect(listMaclawAppApprovalInstancesAllMock).toHaveBeenCalledWith('all', 200));
        expect(screen.getByText('\u5ba1\u6279\u5b9e\u4f8b\u7ba1\u7406')).not.toBeNull();
        expect(screen.getAllByText('Travel expense').length).toBeGreaterThan(0);
        const detail = document.querySelector('.apps-approval-detail') as HTMLElement;
        expect(detail.getAttribute('aria-label')).toBe('审批实例详情');
        expect(within(detail).getByText('审批实例详情')).not.toBeNull();
        expect(within(detail).getByText('结果契约')).not.toBeNull();
        const feedback = within(detail).getByLabelText('结果反馈');
        expect(within(feedback).getByText('pending')).not.toBeNull();
        expect(within(feedback).getByText('approval_pending')).not.toBeNull();
        expect(within(feedback).getByText('输出数')).not.toBeNull();
        expect(within(detail).getAllByText('approval_result').length).toBeGreaterThan(0);
        expect(within(detail).getByText('manager_approval / finance_review')).not.toBeNull();
        expect(within(detail).getAllByText('manager').length).toBeGreaterThan(0);
        expect(within(detail).getByText('Decision')).not.toBeNull();
        expect(screen.getByText('EXP-1')).not.toBeNull();
    });
    it('shows approval instances across request, approval, attention, and handled lanes', async () => {
        listMaclawAppApprovalInstancesAllMock.mockResolvedValue([
            {
                app_id: 'expense.approval',
                app_name: 'Expense Approval Workbench',
                instance_id: 'approval-request-lane-1',
                title: 'Submitted travel request',
                lane: 'my_requests',
                status: 'pending',
                current_node: 'manager.review',
                current_node_ids: ['expense.submit', 'manager.review'],
                workflow_node_ids: ['expense.submit', 'manager.review'],
                owner: 'alice',
                applicant: 'alice',
                approver: 'manager_1',
                current_assignee: 'manager_1',
                current_assignee_type: 'user',
                workflow_skill_id: 'expense-approval-workflow',
                workflow_version: '4.0.0',
                approval_id: 'approval-request-lane-1',
                record_id: 'EXP-LANE-REQ-1',
                object_role: 'expense_report',
                business_status: 'approval_pending',
                result_status: 'pending',
                result: 'Waiting for manager review',
                updated_at: '2026-06-30T09:00:00Z',
                result_payload: { approval_result: 'pending', business_status: 'approval_pending', business_record: { id: 'EXP-LANE-REQ-1' } },
                outputs: [{ kind: 'approval_result', title: 'Request status', text: 'Waiting for manager review', status: 'pending' }],
            },
            {
                app_id: 'expense.approval',
                app_name: 'Expense Approval Workbench',
                instance_id: 'approval-pending-lane-1',
                title: 'Manager approval required',
                lane: 'pending_my_approval',
                status: 'pending',
                current_node: 'finance.review',
                current_node_ids: ['expense.submit', 'manager.review', 'finance.review'],
                workflow_node_ids: ['expense.submit', 'manager.review', 'finance.review'],
                owner: 'bob',
                applicant: 'bob',
                approver: 'finance_manager',
                current_assignee: 'finance_manager',
                current_assignee_type: 'role',
                workflow_skill_id: 'expense-approval-workflow',
                workflow_version: '4.0.0',
                approval_id: 'approval-pending-lane-1',
                record_id: 'EXP-LANE-PENDING-1',
                object_role: 'expense_report',
                business_status: 'manager_approved',
                result_status: 'pending',
                result: 'Finance review required',
                updated_at: '2026-06-30T09:05:00Z',
                result_payload: { approval_result: 'pending', business_status: 'manager_approved', business_record: { id: 'EXP-LANE-PENDING-1' } },
                outputs: [{ kind: 'approval_result', title: 'Approval task', text: 'Finance review required', status: 'pending' }],
            },
            {
                app_id: 'expense.approval',
                app_name: 'Expense Approval Workbench',
                instance_id: 'approval-attention-lane-1',
                title: 'Receipt mismatch requires attention',
                lane: 'attention',
                status: 'attention',
                current_node: 'applicant.supplement',
                current_node_ids: ['expense.submit', 'finance.review', 'applicant.supplement'],
                workflow_node_ids: ['expense.submit', 'finance.review', 'applicant.supplement'],
                owner: 'carol',
                applicant: 'carol',
                approver: 'finance_manager',
                current_assignee: 'carol',
                current_assignee_type: 'user',
                workflow_skill_id: 'expense-approval-workflow',
                workflow_version: '4.0.0',
                approval_id: 'approval-attention-lane-1',
                record_id: 'EXP-LANE-ATTN-1',
                object_role: 'expense_report',
                business_status: 'requires_supplement',
                result_status: 'attention',
                result: 'Receipt amount mismatch',
                updated_at: '2026-06-30T09:10:00Z',
                result_payload: { approval_result: 'attention', business_status: 'requires_supplement', attention_reason: 'Receipt amount mismatch' },
                outputs: [{ kind: 'approval_result', title: 'Attention note', text: 'Receipt amount mismatch', status: 'attention' }],
            },
            {
                app_id: 'expense.approval',
                app_name: 'Expense Approval Workbench',
                instance_id: 'approval-handled-lane-1',
                title: 'Approved quarterly expense',
                lane: 'handled',
                status: 'approved',
                current_node: 'expense.result_feedback',
                current_node_ids: ['expense.submit', 'finance.review', 'expense.result_feedback'],
                workflow_node_ids: ['expense.submit', 'finance.review', 'expense.result_feedback'],
                owner: 'diana',
                applicant: 'diana',
                approver: 'finance_manager',
                current_assignee: 'completed',
                current_assignee_type: 'system',
                workflow_skill_id: 'expense-approval-workflow',
                workflow_version: '4.0.0',
                approval_id: 'approval-handled-lane-1',
                record_id: 'EXP-LANE-APPROVED-1',
                object_role: 'expense_report',
                business_status: 'finance_approved',
                result_status: 'approved',
                result: 'Approved and archived',
                updated_at: '2026-06-30T09:15:00Z',
                result_payload: { approval_result: 'approved', business_status: 'finance_approved', business_record: { id: 'EXP-LANE-APPROVED-1', status: 'finance_approved' } },
                outputs: [{ kind: 'business_record', title: 'Approved expense record', text: 'EXP-LANE-APPROVED-1', status: 'ready' }],
                artifacts: [{ id: 'approval-pdf', uri: 'artifact://expense/approved-quarterly.pdf', name: 'approved-quarterly-expense.pdf', status: 'ready', mime_type: 'application/pdf' }],
            },
        ]);

        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByText('Approval status'));

        await waitFor(() => expect(listMaclawAppApprovalInstancesAllMock).toHaveBeenCalledWith('all', 200));
        const manager = await waitFor(() => document.querySelector('.apps-approval-manager') as HTMLElement);
        expect(within(manager).getByRole('button', { name: /My requests\s*1/ })).not.toBeNull();
        expect(within(manager).getByRole('button', { name: /Pending my approval\s*1/ })).not.toBeNull();
        expect(within(manager).getByRole('button', { name: /Needs attention\s*1/ })).not.toBeNull();
        expect(within(manager).getByRole('button', { name: /Handled\s*1/ })).not.toBeNull();

        fireEvent.click(within(manager).getByRole('button', { name: /My requests/ }));
        await waitFor(() => expect(within(manager).getAllByText('Submitted travel request').length).toBeGreaterThan(0));
        expect(manager.textContent).toContain('EXP-LANE-REQ-1');
        expect(manager.textContent).toContain('Waiting for manager review');
        expect(manager.textContent).not.toContain('Manager approval required');

        fireEvent.click(within(manager).getByRole('button', { name: /Pending my approval/ }));
        await waitFor(() => expect(within(manager).getAllByText('Manager approval required').length).toBeGreaterThan(0));
        expect(manager.textContent).toContain('finance.review');
        expect(manager.textContent).toContain('Approval task');
        expect(manager.textContent).not.toContain('Submitted travel request');

        fireEvent.click(within(manager).getByRole('button', { name: /Needs attention/ }));
        await waitFor(() => expect(within(manager).getAllByText('Receipt mismatch requires attention').length).toBeGreaterThan(0));
        expect(manager.textContent).toContain('Receipt amount mismatch');
        expect(manager.textContent).toContain('attention_reason');
        expect(manager.textContent).not.toContain('Approved quarterly expense');

        fireEvent.click(within(manager).getByRole('button', { name: /Handled/ }));
        await waitFor(() => expect(within(manager).getAllByText('Approved quarterly expense').length).toBeGreaterThan(0));
        expect(manager.textContent).toContain('EXP-LANE-APPROVED-1');
        expect(manager.textContent).toContain('Approved expense record');
        expect(manager.textContent).toContain('approved-quarterly-expense.pdf');
        expect(manager.textContent).toContain('artifact://expense/approved-quarterly.pdf');
        const handledFeedback = within(manager).getByLabelText('Result feedback');
        expect(within(handledFeedback).getAllByText('approved').length).toBeGreaterThan(0);
        expect(within(handledFeedback).getByText('finance_approved')).not.toBeNull();
        expect(within(handledFeedback).getByText('approved-quarterly-expense.pdf')).not.toBeNull();
        expect((within(manager).getByText('Approve') as HTMLButtonElement).disabled).toBe(true);
        expect((within(manager).getByText('Reject') as HTMLButtonElement).disabled).toBe(true);
        expect((within(manager).getByText('Mark attention') as HTMLButtonElement).disabled).toBe(true);
    });
    it('keeps the approval detail pane distinct when no instance is selected', async () => {
        listMaclawAppApprovalInstancesAllMock.mockResolvedValue([]);
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByText('\u5ba1\u6279\u72b6\u6001'));

        await waitFor(() => expect(listMaclawAppApprovalInstancesAllMock).toHaveBeenCalledWith('all', 200));
        const detail = document.querySelector('.apps-approval-detail') as HTMLElement;
        expect(detail.getAttribute('aria-label')).toBe('审批实例详情');
        expect(within(detail).getByText('审批实例详情')).not.toBeNull();
        expect(within(detail).getByText('右侧是实例详情和处理动作区，请先在左侧选择一条审批实例。')).not.toBeNull();
        expect(detail.querySelector('.apps-approval-actions')).toBeNull();
    });
    it('shows DataSrv approval app summary with approval result filters', async () => {
        const requested = new Set<string>();
        getMISDataConfigMock.mockResolvedValue({ enabled: true, endpoint: 'http://datasrv.test', token: 'data-token', user_id: 'user_1' });
        listMaclawAppApprovalInstancesAllMock.mockResolvedValue([
            {
                app_id: 'expense.document',
                app_name: 'Approval PDF exporter',
                instance_id: 'approval-document-1',
                title: 'Approval PDF instance',
                lane: 'handled',
                status: 'approved',
                current_node: 'document.result',
                owner: 'user_1',
                approver: 'manager_1',
                approval_id: 'approval-datasrv-document-1',
                record_id: 'EXP-DOC-1',
                updated_at: '2026-06-27T08:05:00Z',
                result: 'approved',
                outputs: [{ kind: 'document', title: 'Approval PDF', status: 'ready' }],
            },
            {
                app_id: 'expense.other',
                app_name: 'Other approval',
                instance_id: 'approval-other-1',
                title: 'Other approval instance',
                lane: 'handled',
                status: 'approved',
                current_node: 'other.result',
                owner: 'user_1',
                approver: 'manager_1',
                updated_at: '2026-06-27T08:06:00Z',
                result: 'approved',
            },
        ]);
        (globalThis.fetch as any).mockImplementation(async (input: string) => {
            const url = new URL(String(input));
            requested.add(`${url.pathname}?${url.searchParams.toString()}`);
            if (url.pathname === '/api/v1/data/capabilities') {
                return { ok: true, json: async () => ({ service: 'MaClawDataSrv', app_installations: [] }) };
            }
            if (url.pathname === '/api/v1/data/app-installations') {
                const label = url.searchParams.get('approval_status')
                    || url.searchParams.get('result_type')
                    || (url.searchParams.get('applicant_id') ? 'my_requests' : '')
                    || (url.searchParams.get('approver_id') ? 'pending_my_approval' : '')
                    || 'all';
                return {
                    ok: true,
                    json: async () => ({
                        items: label === 'rejected' ? [] : [{
                            app_id: `expense.${label}`,
                            name: label === 'document' ? 'Approval PDF exporter' : `Approval ${label}`,
                            kind: 'enterprise_approval_app',
                            updated_at: '2026-06-27T08:00:00Z',
                            metadata: {
                                test_evidence_approval_status: label === 'document' || label === 'inline_content' ? 'approved' : label,
                                workflow_result_node: `${label}.result`,
                                result_contract_types: label === 'document' ? ['approval_result', 'document'] : ['approval_result'],
                                dataset_id: 'finance.expenses',
                                object_role: 'expense_report',
                                approval_id: `approval-datasrv-${label}-1`,
                                record_id: `record-${label}-1`,
                                workflow_instance_id: `workflow-${label}-1`,
                            },
                        }],
                    }),
                };
            }
            return { ok: false, status: 404, json: async () => ({}) };
        });

        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(screen.getByText('\u5ba1\u6279\u72b6\u6001'));

        await waitFor(() => expect(screen.getByText('DataSrv \u5ba1\u6279\u6982\u89c8')).not.toBeNull());
        await waitFor(() => expect(screen.getByText('Approval PDF exporter')).not.toBeNull());
        fireEvent.click(screen.getByRole('button', { name: /\u6587\u6863\u8f93\u51fa/ }));
        const detail = document.querySelector('.apps-datasrv-approval-summary__details') as HTMLElement;
        expect(within(detail).getByText('Approval PDF exporter')).not.toBeNull();
        expect(within(detail).getByText('expense.document / document.result')).not.toBeNull();
        expect(within(detail).getByText('approval-datasrv-document-1 / workflow-document-1 / record-document-1')).not.toBeNull();
        expect(within(detail).getByText('finance.expenses / expense_report')).not.toBeNull();
        expect(within(detail).getByText(/approval_result, document/)).not.toBeNull();
        fireEvent.click(within(detail).getByRole('button', { name: '打开审批' }));
        expect(browserOpenURLMock).toHaveBeenCalledWith('http://datasrv.test/api/v1/data/approvals/approval-datasrv-document-1');
        fireEvent.click(within(detail).getByRole('button', { name: '打开记录' }));
        expect(browserOpenURLMock).toHaveBeenCalledWith('http://datasrv.test/api/v1/data/datasets/finance.expenses/records/record-document-1');
        fireEvent.click(within(detail).getByRole('button', { name: /Approval PDF exporter/ }));
        await waitFor(() => expect(screen.getAllByText('Approval PDF instance').length).toBeGreaterThan(0));
        expect(screen.queryByText('Other approval instance')).toBeNull();
        expect(Array.from(requested).some((url) => url.includes('applicant_id=user_1'))).toBe(true);
        expect(Array.from(requested).some((url) => url.includes('approver_id=user_1'))).toBe(true);
        expect(Array.from(requested).some((url) => url.includes('approval_status=approved'))).toBe(true);
        expect(Array.from(requested).some((url) => url.includes('approval_status=attention'))).toBe(true);
        expect(Array.from(requested).some((url) => url.includes('result_type=document'))).toBe(true);
        expect(Array.from(requested).some((url) => url.includes('result_type=inline_content'))).toBe(true);
        expect(Array.from(requested).every((url) => !url.includes('/api/v1/data/app-installations') || url.includes('kind=enterprise_approval_app'))).toBe(true);
    });
    it('does not repeat pinned apps in the main icon grid', () => {
        render(<AppsPage lang="zh-Hans" />);

        const sections = document.querySelectorAll('.apps-section');
        expect(sections.length).toBe(2);
        expect(within(sections[1] as HTMLElement).getByText('其他应用')).not.toBeNull();
        expect(within(sections[0] as HTMLElement).getByText('报销申请')).not.toBeNull();
        expect(within(sections[1] as HTMLElement).queryByText('报销申请')).toBeNull();
        expect(within(sections[1] as HTMLElement).getByText('文档脱敏')).not.toBeNull();
    });

    it('moves app tile focus with arrow keys', () => {
        render(<AppsPage lang="zh-Hans" />);

        const tiles = Array.from(document.querySelectorAll<HTMLButtonElement>('.apps-app-tile'));
        tiles[0].focus();
        expect(document.activeElement?.textContent).toContain('报销申请');

        fireEvent.keyDown(tiles[0], { key: 'ArrowRight' });
        expect(document.activeElement?.textContent).toContain('采购入库');

        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'ArrowDown' });
        expect(document.activeElement?.textContent).toContain('PDF 转 Word');

        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'End' });
        expect(document.activeElement?.textContent).toContain('数据同步');
    });

    it('shows pin actions from the app tile context menu', () => {
        render(<AppsPage lang="zh-Hans" />);

        const fullRedactTile = screen.getAllByText('文档脱敏')[0].closest('.apps-app-tile') as HTMLButtonElement;
        fireEvent.contextMenu(fullRedactTile, { clientX: 90, clientY: 180 });
        const firstPinAction = screen.getByRole('menuitem', { name: '设为常用' }) as HTMLButtonElement;
        expect(firstPinAction.disabled).toBe(false);
        fireEvent.click(firstPinAction);

        const expenseTile = screen.getAllByText('报销申请')[0].closest('.apps-app-tile') as HTMLButtonElement;
        fireEvent.contextMenu(expenseTile, { clientX: 120, clientY: 140 });
        const unpinAction = screen.getByRole('menuitem', { name: '移出常用' });
        fireEvent.click(unpinAction);

        let sections = document.querySelectorAll('.apps-section');
        expect(within(sections[0] as HTMLElement).queryByText('报销申请')).toBeNull();

        const sheetTile = screen.getAllByText('表格分析')[0].closest('.apps-app-tile') as HTMLButtonElement;
        fireEvent.keyDown(sheetTile, { key: 'F10', shiftKey: true });
        const pinAction = screen.getByRole('menuitem', { name: '设为常用' });
        expect((pinAction as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(pinAction);

        sections = document.querySelectorAll('.apps-section');
        expect(within(sections[0] as HTMLElement).getByText('表格分析')).not.toBeNull();

        const syncTile = screen.getAllByText('数据同步')[0].closest('.apps-app-tile') as HTMLButtonElement;
        fireEvent.contextMenu(syncTile, { clientX: 9999, clientY: 9999 });
        const menu = screen.getByRole('menu');
        expect(Number.parseFloat(menu.style.left)).toBeLessThan(9999);
        expect(Number.parseFloat(menu.style.top)).toBeLessThan(9999);
    });

    it('shows app name, status, source, and recent usage in tile tooltips', () => {
        render(<AppsPage lang="zh-Hans" />);

        const expenseTile = screen.getAllByText('报销申请')[0].closest('.apps-app-tile') as HTMLButtonElement;
        expect(expenseTile.title).toContain('报销申请');
        expect(expenseTile.title).toContain('企业审批型 · DataSrv');
        expect(expenseTile.title).toContain('状态');
        expect(expenseTile.title).toContain('最近使用: 尚未使用');
        expect(expenseTile.getAttribute('aria-label')).toContain('报销申请, 企业审批型, DataSrv, 状态');

        fireEvent.click(expenseTile);
        const updatedTile = screen.getAllByText('报销申请')[0].closest('.apps-app-tile') as HTMLButtonElement;
        expect(updatedTile.dataset.status).toBe('running');
        expect(updatedTile.querySelector('.apps-app-status-dot')).toBeNull();
        expect(updatedTile.title).toContain('状态: 已打开');
        expect(updatedTile.getAttribute('aria-label')).toContain('状态: 已打开');
        expect(updatedTile.title).toContain('最近使用');
        expect(updatedTile.title).not.toContain('最近使用: 尚未使用');
    });

    it('places approval workspaces before right-side output so the center column starts at the top', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByRole('button', { name: /\u62a5\u9500\u7533\u8bf7, \u4f01\u4e1a\u5ba1\u6279\u578b/ }));
        fireEvent.click(screen.getByRole('button', { name: '\u6267\u884c' }));
        await waitFor(() => expect(container.querySelector<HTMLElement>('.apps-approval-workspace')).not.toBeNull());
        const approval = container.querySelector<HTMLElement>('.apps-approval-workspace');
        const output = container.querySelector<HTMLElement>('.apps-runtime-output');

        expect(output).not.toBeNull();
        expect(container.querySelector('.apps-approval-summary')).toBeNull();
        expect(Number(approval?.style.order)).toBeLessThan(Number(output?.style.order));
    });

    it('reflects DataSrv dependency state in app tile tooltips', async () => {
        getMISDataConfigMock.mockResolvedValue({ enabled: false, endpoint: 'http://127.0.0.1:18180' });
        render(<AppsPage lang="zh-Hans" />);

        await waitFor(() => {
            const expenseTile = screen.getAllByText('报销申请')[0].closest('.apps-app-tile') as HTMLButtonElement;
            expect(expenseTile.dataset.status).toBe('disabled');
            expect(expenseTile.title).toContain('状态: 未启用');
        });
    });

    it('shows one live draft manifest preview in app studio create tab', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());

        expect(screen.getAllByText('当前草稿 manifest').length).toBe(1);
        expect(screen.getAllByText(/x_maclaw_apps/).length).toBeGreaterThan(0);
        expect(screen.queryByText(/document-redaction/)).toBeNull();
    });

    it('hides the Skill apps discovery card when no Skill apps are found', async () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());

        await waitFor(() => expect(listSkillAppManifestsMock).toHaveBeenCalled());
        await waitFor(() => {
            expect(screen.queryByText('Found apps')).toBeNull();
            expect(screen.queryByText(miniAppLabels.skillAppsSyncedMeta.en)).toBeNull();
        });
    });

    it('does not flash the Skill apps discovery card while scanning with no candidates yet', async () => {
        let finishDiscovery: (entries: unknown[]) => void = () => undefined;
        listSkillAppManifestsMock.mockReturnValue(new Promise((resolve) => {
            finishDiscovery = resolve;
        }));
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());

        await waitFor(() => expect(listSkillAppManifestsMock).toHaveBeenCalled());
        expect(screen.queryByText('Found apps')).toBeNull();
        expect(screen.queryByText(miniAppLabels.skillAppsSyncedMeta.en)).toBeNull();
        finishDiscovery([]);
        await waitFor(() => expect(screen.queryByText('Found apps')).toBeNull());
    });

    it('shows a clear Skill app discovery error without claiming apps were synced', async () => {
        listSkillAppManifestsMock.mockRejectedValue(new Error('scan failed'));
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        await waitFor(() => expect(screen.getByText('Could not check installed capabilities')).not.toBeNull());
        expect(screen.getByText(/scan failed/)).not.toBeNull();
        expect(screen.queryByText(miniAppLabels.skillAppsSyncedMeta.en)).toBeNull();
    });

    it('shows friendly Skill app discovery text when candidates exist', async () => {
        listSkillAppManifestsMock.mockResolvedValue([
            {
                id: 'invoice-review',
                skill_id: 'invoice-app',
                name: 'Invoice Review',
                description: 'Review invoice files',
                category: 'Finance',
                icon: 'invoice',
                input_mode: 'file',
                output_modes: ['pdf'],
                app_definition_file: 'maclaw.app.json',
            },
        ]);
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());

        await waitFor(() => expect(screen.getAllByText('Invoice Review').length).toBeGreaterThan(0));
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('Finance (1)')).not.toBeNull();
        expect(screen.queryByText('invoice-app')).toBeNull();
        expect(screen.queryByText('Add to panel')).toBeNull();
        expect(screen.queryByText('maclaw.app.json / maclaw.apps.json · x_maclaw_apps')).toBeNull();
        expect(screen.queryByTitle(/maclaw\.app\.json/)).toBeNull();
    });

    it('deduplicates repeated Skill app discovery entries', async () => {
        listSkillAppManifestsMock.mockResolvedValue([
            { id: 'invoice-review', skill_id: 'invoice-app', name: 'Invoice Review', description: 'Review invoice files', category: 'Finance', icon: 'invoice', input_mode: 'file', output_modes: ['pdf'] },
            { id: 'invoice-review', skill_id: 'invoice-app', name: 'Invoice Review Duplicate', description: 'Duplicate entry', category: 'Finance', icon: 'invoice', input_mode: 'file', output_modes: ['pdf'] },
        ]);
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());

        await waitFor(() => expect(screen.getAllByText('Invoice Review').length).toBe(1));
        expect(screen.queryByText('Invoice Review Duplicate')).toBeNull();
    });

    it('does not restore an uninstalled Skill app from stale panel state', async () => {
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: ['skill-app:pdf-translation:pdf-translation'],
            customApps: [{
                id: 'skill-app:pdf-translation:pdf-translation',
                name: 'PDF Translation Tool',
                description: 'Stale entry from an uninstalled Skill',
                category: 'Skill',
                kind: 'tool_app',
                icon: 'pdf',
                accent: '#2f5f98',
                source: 'skill',
            }],
        }));

        render(<AppsPage lang="en" />);

        await waitFor(() => expect(listSkillAppManifestsMock).toHaveBeenCalled());
        await waitFor(() => expect(screen.queryByText('PDF Translation Tool')).toBeNull());
    });

    it('uses the installed Skill definition over an older panel snapshot', async () => {
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: ['skill-app:invoice-review:invoice-review'],
            customApps: [{
                id: 'skill-app:invoice-review:invoice-review',
                name: 'Old Invoice Review',
                description: 'Old cached description',
                category: 'Skill',
                kind: 'tool_app',
                icon: 'invoice',
                accent: '#2f5f98',
                source: 'local',
            }],
        }));
        listSkillAppManifestsMock.mockResolvedValue([{
            id: 'invoice-review',
            skill_id: 'invoice-review',
            name: 'Installed Invoice Review',
            description: 'Definition currently installed on disk',
            category: 'Finance',
            icon: 'invoice',
            input_mode: 'file',
            output_modes: ['pdf'],
        }]);

        render(<AppsPage lang="en" />);

        await waitFor(() => expect(screen.getAllByText('Installed Invoice Review').length).toBeGreaterThan(0));
    });

    it('retains SkillMarket provenance when restoring a discovered mini app', async () => {
        const storedApp = {
            id: 'skill-app-pdf-translation-pdf-translation',
            name: 'PDF Translation Tool',
            description: 'Installed from SkillMarket',
            category: 'Skill',
            kind: 'tool_app',
            icon: 'pdf',
            accent: '#2f5f98',
            source: 'skill',
            marketCapabilityID: 'skill-pdf-translation',
            marketInstallSource: 'skillmarket',
            marketSourceLabel: 'HubCenter Skill Market',
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [storedApp.id],
            customApps: [storedApp],
        }));
        listSkillAppManifestsMock.mockResolvedValue([{
            id: 'pdf-translation',
            skill_id: 'pdf-translation',
            name: 'PDF Translation Tool',
            description: 'Definition currently installed on disk',
            category: 'Skill',
            icon: 'pdf',
            app_definition_file: 'maclaw.app.json',
            input_mode: 'file',
            app_definition: {
                schema: 'maclaw.app.v1',
                privateMarker: 'x_maclaw_apps',
                app: {
                    id: 'pdf-translation',
                    name: 'PDF Translation Tool',
                    kind: 'tool_app',
                    binding: {
                        skill: { id: 'pdf-translation', appDefinitionFile: 'maclaw.app.json', inputMode: 'file' },
                        dependencies: {
                            skills: [{ id: 'paper_pdf_translator', kind: 'runtime_skill', required: true, source: 'local' }],
                        },
                    },
                },
            },
        }]);
        checkMaclawAppRuntimeHealthMock.mockResolvedValue({
            schema: 'maclaw.app.runtime_health.v1',
            ok: false,
            blocked: true,
            message: 'legacy dependency is missing',
            app_id: storedApp.id,
            plan: {
                schema: 'maclaw.app.install_plan.v1',
                apps: [],
                dependencies: [{
                    id: 'paper_pdf_translator',
                    kind: 'runtime_skill',
                    required: true,
                    source: 'skillmarket',
                    installed: false,
                    health: 'missing',
                    action: 'install',
                    app_ids: [storedApp.id],
                }],
                has_missing_required: true,
                has_blocking_dependency: false,
            },
            has_missing_required: true,
            has_blocking_dependency: false,
            has_workflow_contract_issue: false,
        });

        render(<AppsPage lang="en" />);

        await waitFor(() => expect(screen.getAllByText('PDF Translation Tool').length).toBeGreaterThan(0));
        fireEvent.click((screen.getAllByText('PDF Translation Tool')[0].closest('.apps-app-tile') as HTMLElement));
        await waitFor(() => expect(checkMaclawAppRuntimeHealthMock).toHaveBeenCalled());
        const healthManifest = checkMaclawAppRuntimeHealthMock.mock.calls
            .map(([packageJSON]) => JSON.parse(String(packageJSON)))
            .find((candidate) => candidate?.app?.dependencies?.skills?.some((dep: any) => dep?.id === 'paper_pdf_translator'));
        expect(healthManifest).toBeTruthy();
        expect(healthManifest.app.dependency_source).toBeUndefined();
        expect(healthManifest.app.binding.market_install_source).toBeUndefined();
        expect(healthManifest.app.dependencies.skills).toEqual([
            expect.objectContaining({ id: 'paper_pdf_translator', source: 'local' }),
        ]);
    });

    it('saves a tool app definition into an existing skill', async () => {
        listNLSkillsMock.mockResolvedValue([
            { name: 'invoice-review', description: '审核发票' },
            { name: 'already-app', is_maclaw_app: true },
            { name: 'already-app-camel', isMaclawApp: true },
        ]);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        const toolSkillPicker = screen.getByTestId('studio-tool-skill-picker');
        await waitFor(() => expect(within(toolSkillPicker).getAllByText('invoice-review').length).toBeGreaterThan(0));
        expect(within(toolSkillPicker).queryByText('already-app')).toBeNull();
        expect(within(toolSkillPicker).queryByText('already-app-camel')).toBeNull();
        fireEvent.click(within(toolSkillPicker).getByRole('option', { name: /invoice-review/ }) as HTMLButtonElement);
        fireEvent.change(getCreateAppNameInput(), { target: { value: '发票审核' } });
        fireEvent.click(screen.getByText('保存到 Skill'));

        await waitFor(() => expect(saveMaclawAppDefinitionForSkillMock).toHaveBeenCalledTimes(1));
        expect(saveMaclawAppDefinitionForSkillMock.mock.calls[0][0]).toBe('invoice-review');
        const payload = JSON.parse(saveMaclawAppDefinitionForSkillMock.mock.calls[0][1]);
        expect(payload.schema).toBe('maclaw.app.v1');
        expect(payload.privateMarker).toBe('x_maclaw_apps');
        expect(payload.installUnit).toBe('skill');
        expect(payload.app.name).toBe('发票审核');
        expect(payload.app.kind).toBe('tool_app');
        expect(payload.app.binding.skill.id).toBe('invoice-review');
        expect(payload.app.binding.skill.appDefinitionFile).toBe('maclaw.app.json');
        await waitFor(() => expect(screen.getAllByText('发票审核').length).toBeGreaterThan(0));

        seedSuccessfulSkillAppRun();
        fireEvent.click(screen.getByText('上传到 SkillMarket'));
        await waitFor(() => expect(uploadNLSkillToMarketMock).toHaveBeenCalledWith('invoice-review'));
        await waitFor(() => expect(screen.getByText('已提交到 SkillMarket: submission-app-1')).not.toBeNull());
    });

    it('saves a newly created enterprise approval app into its app skill definition', async () => {
        listNLSkillsMock.mockResolvedValue([
            { name: 'expense-super-skill', description: '费用应用 Skill', capabilities: ['enterprise.app'] },
            { name: 'expense-workflow', description: '费用审批流程', product_kind: 'workflow_skill', capabilities: ['approval.workflow'] },
        ]);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        const kindPicker = screen.getByRole('group', { name: '应用类型' });
        fireEvent.click(within(kindPicker).getByRole('button', { name: /企业审批型/ }));
        fireEvent.change(getCreateAppNameInput(), { target: { value: '费用报销审批' } });

        const appSkillPicker = screen.getByTestId('studio-app-skill-id');
        await waitFor(() => expect(within(appSkillPicker).getAllByText('expense-super-skill').length).toBeGreaterThan(0));
        const workflowSkillPicker = screen.getByTestId('studio-workflow-skill-id');
        await waitFor(() => expect(within(workflowSkillPicker).getAllByText('expense-workflow').length).toBeGreaterThan(0));
        fireEvent.change(screen.getByTestId('studio-dependency-install-ref'), { target: { value: 'cap-hub-expense-workflow' } });
        fireEvent.click(screen.getByTestId('studio-layout-template-left_nav'));
        fireEvent.change(screen.getByTestId('studio-layout-density'), { target: { value: 'compact' } });
        fireEvent.click(screen.getByTestId('studio-layout-slot-right'));
        fireEvent.change(screen.getByTestId('studio-output-region'), { target: { value: 'bottom' } });
        fireEvent.change(screen.getByTestId('studio-layout-region-result_panel'), { target: { value: 'bottom' } });
        fireEvent.click(screen.getByTestId('studio-layout-region-visible-approval_detail'));
        fireEvent.click(screen.getByTestId('studio-layout-region-request_form-order-down'));
        expect(screen.getByTestId('studio-layout-evidence').textContent).toContain('left_nav');
        expect(screen.getByTestId('studio-layout-evidence').textContent).toContain('3/4');

        fireEvent.click(screen.getByText('保存到 Skill'));

        await waitFor(() => expect(saveMaclawAppDefinitionForSkillMock).toHaveBeenCalledTimes(1));
        const [skillID, manifestText] = saveMaclawAppDefinitionForSkillMock.mock.calls[0];
        expect(skillID).toBe('expense-super-skill');
        const payload = JSON.parse(String(manifestText));
        expect(payload.schema).toBe('maclaw.app.v1');
        expect(payload.privateMarker).toBe('x_maclaw_apps');
        expect(payload.app.name).toBe('费用报销审批');
        expect(payload.app.kind).toBe('enterprise_approval_app');
        expect(payload.app.launchMode).toBe('agent_dynamic_ui');
        expect(payload.app.binding.skill.id).toBe('expense-super-skill');
        expect(payload.app.binding.skill.appDefinitionFile).toBe('maclaw.app.json');
        expect(payload.app.binding.appSkill.id).toBe('expense-super-skill');
        expect(payload.app.binding.dependencies.skills[0].id).toBe('expense-workflow');
        expect(payload.app.binding.dependencies.skills[0].install_ref).toBe('cap-hub-expense-workflow');
        expect(payload.app.binding.dependencies.skills[0].kind).toBe('workflow_skill');
        expect(payload.app.binding.mis.approvalBindings[0].workflowSkillId).toBe('expense-workflow');
        expect(payload.app.binding.ui.layouts.approval_workspace).toMatchObject({
            template: 'left_nav',
            density: 'compact',
            primaryRegion: 'right',
            outputRegion: 'bottom',
            studio: expect.objectContaining({ editable: true, savedInManifest: true, updatedBy: 'app_studio' }),
        });
        expect(payload.app.binding.ui.layouts.approval_workspace.regions).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'request_form', role: 'input', placement: 'right' }),
            expect.objectContaining({ id: 'approval_detail', role: 'detail', visible: false }),
            expect.objectContaining({ id: 'result_panel', role: 'output', placement: 'bottom' }),
        ]));
        expect(payload.app.binding.ui.layouts.approval_workspace.regions[0]).toMatchObject({ id: 'approval_inbox', order: 1 });
        expect(payload.app.binding.ui.layouts.approval_workspace.regions[1]).toMatchObject({ id: 'request_form', order: 2 });
        expect(payload.app.governance.workspaceLayout).toMatchObject({
            entry: 'approval_workspace',
            template: 'left_nav',
            density: 'compact',
            primaryRegion: 'right',
            outputRegion: 'bottom',
            visibleRegionCount: 3,
            regionIds: ['approval_inbox', 'request_form', 'approval_detail', 'result_panel'],
            savedInManifest: true,
        });
        expect(payload.app.governance.workspaceLayout.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        expect(payload.app.binding.ui.layouts.approval_workspace.fingerprint).toBe(payload.app.governance.workspaceLayout.fingerprint);
        expect(payload.app.governance.workspaceLayout.regions).toEqual(payload.app.binding.ui.layouts.approval_workspace.regions);
        expect(payload.app.governance.workspaceLayout.regionIds).toEqual(payload.app.binding.ui.layouts.approval_workspace.regions.map((region: any) => region.id));
        expect(payload.app.binding.resultContract.primary).toBe('approval_result');
        expect(payload.app.binding.testProtocol.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        await waitFor(() => expect(screen.getByText('已保存到 expense-super-skill/maclaw.app.json')).not.toBeNull());
    });

    it('saves an enterprise normal app Studio layout into its app skill definition', async () => {
        listNLSkillsMock.mockResolvedValue([
            { name: 'customer-console-skill', description: 'Customer console app Skill', capabilities: ['enterprise.app', 'business.operation'] },
        ]);
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        const kindPicker = screen.getByRole('group', { name: 'App type' });
        fireEvent.click(within(kindPicker).getByRole('button', { name: /Business app/ }));
        fireEvent.change(getCreateAppNameInput(), { target: { value: 'Customer Console Studio' } });

        const appSkillPicker = screen.getByTestId('studio-app-skill-id');
        await waitFor(() => expect(within(appSkillPicker).getAllByText('customer-console-skill').length).toBeGreaterThan(0));
        fireEvent.change(screen.getByTestId('studio-business-domain'), { target: { value: 'sales' } });
        fireEvent.change(screen.getByTestId('studio-business-object-role'), { target: { value: 'customer' } });
        fireEvent.change(screen.getByTestId('studio-business-preferred-action'), { target: { value: 'sales.customer_upsert' } });
        fireEvent.change(screen.getByTestId('studio-business-preferred-view'), { target: { value: 'sales.customer_directory' } });
        fireEvent.change(screen.getByTestId('studio-business-preferred-report'), { target: { value: 'sales.customer_activity' } });
        fireEvent.change(screen.getByTestId('studio-business-preferred-dashboard'), { target: { value: 'sales.overview' } });
        fireEvent.click(screen.getByTestId('studio-layout-template-dashboard'));
        fireEvent.change(screen.getByTestId('studio-layout-density'), { target: { value: 'spacious' } });
        fireEvent.click(screen.getByTestId('studio-layout-slot-center'));
        fireEvent.change(screen.getByTestId('studio-output-region'), { target: { value: 'right' } });
        fireEvent.change(screen.getByTestId('studio-layout-region-output_panel'), { target: { value: 'right' } });
        fireEvent.change(screen.getByTestId('studio-layout-region-operation_form'), { target: { value: 'center' } });
        fireEvent.click(screen.getByTestId('studio-layout-region-record_detail-order-up'));
        expect(screen.getByTestId('studio-layout-evidence').textContent).toContain('dashboard');
        expect(screen.getByTestId('studio-layout-evidence').textContent).toContain('4/4');

        fireEvent.click(screen.getByText('Save to Skill'));

        await waitFor(() => expect(saveMaclawAppDefinitionForSkillMock).toHaveBeenCalledTimes(1));
        const [skillID, manifestText] = saveMaclawAppDefinitionForSkillMock.mock.calls[0];
        expect(skillID).toBe('customer-console-skill');
        const payload = JSON.parse(String(manifestText));
        expect(payload.schema).toBe('maclaw.app.v1');
        expect(payload.privateMarker).toBe('x_maclaw_apps');
        expect(payload.app.name).toBe('Customer Console Studio');
        expect(payload.app.kind).toBe('enterprise_normal_app');
        expect(payload.app.launchMode).toBe('agent_dynamic_ui');
        expect(payload.app.binding.skill).toMatchObject({ id: 'customer-console-skill', appDefinitionFile: 'maclaw.app.json' });
        expect(payload.app.binding.appSkill).toMatchObject({ id: 'customer-console-skill', version: '1.0.0', source: 'local' });
        expect(payload.app.binding.datasrv).toMatchObject({ domain: 'sales', objectRole: 'customer', preferredAction: 'sales.customer_upsert', preferredView: 'sales.customer_directory', preferredReport: 'sales.customer_activity', preferredDashboard: 'sales.overview' });
        expect(payload.app.binding.ui.layouts.business_workspace).toMatchObject({ template: 'dashboard', density: 'spacious', primaryRegion: 'center', outputRegion: 'right' });
        expect(payload.app.binding.ui.layouts.business_workspace.regions).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'operation_form', role: 'input', placement: 'center' }),
            expect.objectContaining({ id: 'record_list', role: 'record_list', placement: 'center' }),
            expect.objectContaining({ id: 'record_detail', role: 'detail' }),
            expect.objectContaining({ id: 'output_panel', role: 'output', placement: 'right' }),
        ]));
        expect(payload.app.binding.ui.layouts.business_workspace.regions[0]).toMatchObject({ id: 'operation_form', order: 1 });
        expect(payload.app.binding.ui.layouts.business_workspace.regions[1]).toMatchObject({ id: 'record_detail', order: 2 });
        expect(payload.app.governance.workspaceLayout).toMatchObject({
            entry: 'business_workspace',
            template: 'dashboard',
            density: 'spacious',
            primaryRegion: 'center',
            outputRegion: 'right',
            visibleRegionCount: 4,
            regionIds: ['operation_form', 'record_detail', 'record_list', 'output_panel'],
            savedInManifest: true,
        });
        expect(payload.app.governance.workspaceLayout.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        expect(payload.app.binding.ui.layouts.business_workspace.fingerprint).toBe(payload.app.governance.workspaceLayout.fingerprint);
        expect(payload.app.governance.workspaceLayout.regions).toEqual(payload.app.binding.ui.layouts.business_workspace.regions);
        expect(payload.app.governance.resultContract).toMatchObject({ primary: 'business_status' });
        expect(payload.app.governance.testProtocol.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        await waitFor(() => expect(screen.getByText('Saved to customer-console-skill/maclaw.app.json')).not.toBeNull());
    });

    it('creates, tests, and submits an enterprise approval app from App Studio', async () => {
        listNLSkillsMock.mockResolvedValue([
            { name: 'expense-super-skill', description: 'Expense app Skill', capabilities: ['enterprise.app'] },
            { name: 'expense-workflow', description: 'Expense approval workflow', product_kind: 'workflow_skill', capabilities: ['approval.workflow'] },
        ]);
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-created-approval-1',
            status: 'success',
            summary: {
                last_output_snippet: JSON.stringify({
                    approval_result: 'approved',
                    business_status: 'finance_approved',
                    business_record: { id: 'EXP-CREATED-1', status: 'finance_approved' },
                    approval_instance: {
                        workflow_instance_id: 'wf-created-approval-1',
                        approval_id: 'approval-created-approval-1',
                        record_id: 'EXP-CREATED-1',
                        current_node: 'expense.result_feedback',
                        business_status: 'finance_approved',
                        result_status: 'approved',
                    },
                    outputs: [{ kind: 'notification', title: 'Created approval notice', text: 'Finance notified', status: 'ready' }],
                    artifacts: [{ id: 'created-approval-pack', uri: 'artifact://created/approval-pack.zip', name: 'created-approval-pack.zip', status: 'ready' }],
                }),
            },
            outputs: [
                { id: 'created-record', kind: 'business_record', title: 'Created approval record', text: 'EXP-CREATED-1', status: 'ready', data: { id: 'EXP-CREATED-1', status: 'finance_approved' } },
                { id: 'created-document', kind: 'document', title: 'Created approval PDF', artifact_id: 'created-pdf', status: 'ready' },
            ],
            artifacts: [{ id: 'created-pdf', uri: 'artifact://created/approval.pdf', name: 'created-approval.pdf', status: 'ready' }],
        });
        syncMaclawAppApprovalInstanceToDataSrvMock
            .mockResolvedValueOnce({ synced: true, approval_id: 'approval-created-submit', dataset_id: 'expense_report.records' })
            .mockResolvedValueOnce({ synced: true, approval_id: 'approval-created-final', record_id: 'EXP-CREATED-1', dataset_id: 'expense_report.records' });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'created-approval-submission', submitted_at: '2026-06-30T12:00:00Z', status: 'submitted', message: 'queued' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        const kindPicker = screen.getByRole('group', { name: 'App type' });
        fireEvent.click(within(kindPicker).getByRole('button', { name: /Approval app/ }));
        fireEvent.change(getCreateAppNameInput(), { target: { value: 'Created Approval Flow' } });
        await waitFor(() => expect(within(screen.getByTestId('studio-app-skill-id')).getAllByText('expense-super-skill').length).toBeGreaterThan(0));
        await waitFor(() => expect(within(screen.getByTestId('studio-workflow-skill-id')).getAllByText('expense-workflow').length).toBeGreaterThan(0));
        fireEvent.change(screen.getByTestId('studio-approval-event'), { target: { value: 'expense.submitted' } });
        fireEvent.change(screen.getByTestId('studio-approval-object-role'), { target: { value: 'expense_report' } });
        fireEvent.change(screen.getByTestId('studio-dependency-install-ref'), { target: { value: 'cap-hub-expense-workflow' } });
        fireEvent.click(screen.getByTestId('studio-layout-template-left_nav'));
        fireEvent.change(screen.getByTestId('studio-layout-density'), { target: { value: 'compact' } });
        fireEvent.click(screen.getByTestId('studio-layout-slot-right'));
        fireEvent.change(screen.getByTestId('studio-output-region'), { target: { value: 'bottom' } });
        fireEvent.change(screen.getByTestId('studio-layout-region-result_panel'), { target: { value: 'bottom' } });
        fireEvent.click(screen.getByTestId('studio-layout-region-visible-approval_detail'));
        fireEvent.click(screen.getByTestId('studio-layout-region-request_form-order-down'));
        fireEvent.click(screen.getByText('Save to Skill'));

        await waitFor(() => expect(saveMaclawAppDefinitionForSkillMock).toHaveBeenCalledWith('expense-super-skill', expect.any(String)));
        // Skill-source apps are persisted via the Skill definition itself, not
        // customApps, so derive the deterministic panel identity instead:
        // skillPanelAppID(skillID, makeSkillAppDefinitionId(name)).
        const createdApp = {
            id: 'skill-app-expense-super-skill-app-created-approval-flow',
            name: 'Created Approval Flow',
            kind: 'enterprise_approval_app',
            source: 'skill',
            manifest: {
                appSkill: { id: 'expense-super-skill', version: '1.0.0' },
                dependencies: { skills: [{ id: 'expense-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', install_ref: 'cap-hub-expense-workflow' }] },
                mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'expense-workflow', workflowVersion: '1.0.0', objectRole: 'expense_report' }] },
            },
        };
        await waitFor(() => expect(screen.getAllByText('Created Approval Flow').length).toBeGreaterThan(0));
        planMaclawAppInstallMock.mockImplementation(async () => {
            const currentApp = createdApp;
            const verification = testDependencyVerificationForApp(currentApp);
            return {
                schema: 'maclaw.app.install_plan.v1',
                apps: [{ id: currentApp.id, name: currentApp.name, kind: currentApp.kind }],
                dependencies: verification.dependencies,
                dependency_count: verification.dependencies.length,
                has_missing_required: false,
                has_blocking_dependency: false,
                has_workflow_contract_issue: false,
                workflow_contract_issue_count: 0,
                has_governance_review_issue: false,
                governance_review_issue_count: 0,
            };
        });

        fireEvent.click(screen.getAllByText('Close')[0]);
        fireEvent.click(screen.getAllByText('Created Approval Flow')[0]);
        fireEvent.click(screen.getByText('Run'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('expense-workflow', expect.objectContaining({
            app_id: createdApp.id,
            app_kind: 'enterprise_approval_app',
            approval_workflow_skill_id: 'expense-workflow',
            approval_workflow_id: 'expense.submitted',
            approval_object_role: 'expense_report',
            object_role: 'expense_report',
            workflow_version: '1.0.0',
        })));
        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock.mock.calls.some((call) => call[0].status === 'approved')).toBe(true));

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Created Approval Flow')) as HTMLElement;
        expect(card).toBeTruthy();
        await waitFor(() => expect(within(card).getByText('Ready to submit')).not.toBeNull());
        await waitFor(() => expect(within(card).getByText('One-click publish')).not.toBeNull());
        fireEvent.click(within(card).getByText('One-click publish'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const packagePayload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        const appPayload = packagePayload.apps[0].app;
        expect(appPayload.name).toBe('Created Approval Flow');
        expect(appPayload.kind).toBe('enterprise_approval_app');
        expect(appPayload.binding.appSkill.id).toBe('expense-super-skill');
        expect(appPayload.binding.dependencies.skills).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'expense-workflow', kind: 'workflow_skill', install_ref: 'cap-hub-expense-workflow' }),
        ]));
        expect(appPayload.binding.mis.approvalBindings[0]).toMatchObject({ event: 'expense.submitted', objectRole: 'expense_report', workflowSkillId: 'expense-workflow' });
        expect(appPayload.binding.ui.layouts.approval_workspace).toMatchObject({
            template: 'left_nav',
            density: 'compact',
            primaryRegion: 'right',
            outputRegion: 'bottom',
        });
        expect(appPayload.binding.ui.layouts.approval_workspace.regions).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'approval_detail', visible: false }),
            expect.objectContaining({ id: 'request_form', order: 2, placement: 'right' }),
        ]));
        expect(appPayload.governance.workspaceLayout).toMatchObject({
            entry: 'approval_workspace',
            template: 'left_nav',
            density: 'compact',
            visibleRegionCount: 3,
            regionIds: ['approval_inbox', 'request_form', 'approval_detail', 'result_panel'],
            savedInManifest: true,
        });
        expect(appPayload.governance.workspaceLayout.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        expect(appPayload.binding.ui.layouts.approval_workspace.fingerprint).toBe(appPayload.governance.workspaceLayout.fingerprint);
        expect(appPayload.governance.workspaceLayout.regions).toEqual(appPayload.binding.ui.layouts.approval_workspace.regions);
        expect(appPayload.governance.workspaceLayout.regionIds).toEqual(appPayload.binding.ui.layouts.approval_workspace.regions.map((region: any) => region.id));
        expect(appPayload.governance.dependencyVerification).toMatchObject({ dependencyCount: 2, hasBlockingDependency: false });
        expect(appPayload.governance.resultContract).toMatchObject({ primary: 'approval_result' });
        expect(appPayload.governance.testProtocol.fingerprint).toBeTruthy();
        expect(appPayload.governance.testEvidence.approvalInstance).toMatchObject({
            approvalID: 'approval-created-final',
            workflowSkillId: 'expense-workflow',
            workflowVersion: '1.0.0',
            approvalEvent: 'expense.submitted',
            approvalObjectRole: 'expense_report',
            recordID: 'EXP-CREATED-1',
            resultStatus: 'approved',
            approvalInstanceViewVerified: true,
        });
        expect(appPayload.governance.testEvidence.resultCoverage).toMatchObject({
            ok: true,
            primary: 'approval_result',
            missingTypes: [],
        });
        expect(appPayload.governance.testEvidence.resultCoverage.coveredTypes).toEqual(expect.arrayContaining(['approval_result', 'business_status', 'business_record', 'document']));
        expect(appPayload.governance.testEvidence.outputs).toEqual(expect.arrayContaining([expect.objectContaining({ title: 'Created approval record' })]));
        expect(appPayload.governance.testEvidence.artifacts).toEqual(expect.arrayContaining([expect.objectContaining({ name: 'created-approval.pdf' })]));
    });
    it('requires a successful current-version test before uploading a skill app', async () => {
        listNLSkillsMock.mockResolvedValue([{ name: 'invoice-review', description: '审核发票' }]);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        const toolSkillPicker = screen.getByTestId('studio-tool-skill-picker');
        await waitFor(() => expect(within(toolSkillPicker).getAllByText('invoice-review').length).toBeGreaterThan(0));
        fireEvent.click(within(toolSkillPicker).getByRole('option', { name: /invoice-review/ }) as HTMLButtonElement);
        fireEvent.change(getCreateAppNameInput(), { target: { value: '发票审核' } });

        fireEvent.click(screen.getByText('上传到 SkillMarket'));

        await waitFor(() => expect(screen.getByText(miniAppLabels.publishRequiresPanelTest.zhHans)).not.toBeNull());
        expect(saveMaclawAppDefinitionForSkillMock).not.toHaveBeenCalled();
        expect(uploadNLSkillToMarketMock).not.toHaveBeenCalled();
    });

    it('saves the latest tool app layout and test evidence before uploading to SkillMarket', async () => {
        listNLSkillsMock.mockResolvedValue([{ name: 'invoice-review', description: '审核发票' }]);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        const toolSkillPicker = screen.getByTestId('studio-tool-skill-picker');
        await waitFor(() => expect(within(toolSkillPicker).getAllByText('invoice-review').length).toBeGreaterThan(0));
        fireEvent.click(within(toolSkillPicker).getByRole('option', { name: /invoice-review/ }) as HTMLButtonElement);
        fireEvent.change(getCreateAppNameInput(), { target: { value: '发票审核' } });
        fireEvent.click(screen.getByTestId('studio-layout-template-dashboard'));
        fireEvent.change(screen.getByTestId('studio-layout-density'), { target: { value: 'compact' } });
        fireEvent.click(screen.getByTestId('studio-layout-slot-center'));
        fireEvent.change(screen.getByTestId('studio-output-region'), { target: { value: 'bottom' } });
        fireEvent.change(screen.getByTestId('studio-layout-region-output_panel'), { target: { value: 'bottom' } });
        fireEvent.change(screen.getByTestId('studio-layout-region-file_queue'), { target: { value: 'center' } });
        fireEvent.click(screen.getByTestId('studio-layout-region-visible-settings_panel'));
        fireEvent.click(screen.getByTestId('studio-layout-region-output_panel-order-up'));
        expect(screen.getByTestId('studio-layout-evidence').textContent).toContain('dashboard');
        expect(screen.getByTestId('studio-layout-evidence').textContent).toContain('3/4');

        fireEvent.click(screen.getByText('保存到 Skill'));
        await waitFor(() => expect(saveMaclawAppDefinitionForSkillMock).toHaveBeenCalledWith('invoice-review', expect.any(String)));
        seedSuccessfulSkillAppRun();
        // Re-hash the seeded evidence against the current (layout-edited)
        // definition saved above: stale evidence now blocks the upload.
        const savedPackage = JSON.parse(saveMaclawAppDefinitionForSkillMock.mock.calls[0][1]);
        const savedApp = savedPackage.app;
        const currentDefinition = {
            name: savedApp.name,
            description: savedApp.description,
            category: savedApp.category,
            kind: savedApp.kind,
            icon: savedApp.icon,
            version: savedApp.version,
            manifest: {
                schema: savedPackage.schema,
                installUnit: savedPackage.installUnit,
                privateMarker: savedPackage.privateMarker,
                launchMode: savedApp.launchMode,
                skill: savedApp.binding?.skill,
                appSkill: savedApp.appSkill,
                dependencies: savedApp.dependencies,
                ui: savedApp.ui,
                resultContract: savedApp.resultContract,
                testProtocol: savedApp.testProtocol,
                workflow: savedApp.workflow,
            },
        };
        {
            const raw = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
            raw['skill-app-invoice-review-app-tool-app'] = (raw['skill-app-invoice-review-app-tool-app'] || [])
                .map((entry: any) => ({ ...entry, definitionHash: testAppDefinitionFingerprint(currentDefinition) }));
            window.localStorage.setItem(runHistoryStorageKey, JSON.stringify(raw));
        }
        saveMaclawAppDefinitionForSkillMock.mockClear();
        fireEvent.click(screen.getByText('上传到 SkillMarket'));

        await waitFor(() => expect(saveMaclawAppDefinitionForSkillMock).toHaveBeenCalledWith('invoice-review', expect.any(String)));
        await waitFor(() => expect(uploadNLSkillToMarketMock).toHaveBeenCalledWith('invoice-review'));
        const payload = JSON.parse(saveMaclawAppDefinitionForSkillMock.mock.calls[0][1]);
        expect(payload.schema).toBe('maclaw.app.v1');
        expect(payload.privateMarker).toBe('x_maclaw_apps');
        expect(payload.installUnit).toBe('skill');
        expect(payload.app.name).toBe('发票审核');
        expect(payload.app.kind).toBe('tool_app');
        expect(payload.app.launchMode).toBe('fixed_skill_ui');
        expect(payload.app.binding.skill.id).toBe('invoice-review');
        expect(payload.app.binding.skill.appDefinitionFile).toBe('maclaw.app.json');
        expect(payload.app.binding.ui.entry).toBe('tool_workspace');
        expect(payload.app.binding.ui.layouts.tool_workspace).toMatchObject({
            template: 'dashboard',
            density: 'compact',
            primaryRegion: 'center',
            outputRegion: 'bottom',
            studio: expect.objectContaining({ editable: true, savedInManifest: true, updatedBy: 'app_studio' }),
        });
        expect(payload.app.binding.ui.layouts.tool_workspace.regions).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'file_queue', role: 'input', placement: 'center' }),
            expect.objectContaining({ id: 'settings_panel', role: 'parameters', visible: false }),
            expect.objectContaining({ id: 'preview_panel', role: 'preview', placement: 'center' }),
            expect.objectContaining({ id: 'output_panel', role: 'output', placement: 'bottom' }),
        ]));
        expect(payload.app.binding.ui.layouts.tool_workspace.regions[0]).toMatchObject({ id: 'file_queue', order: 1 });
        expect(payload.app.binding.ui.layouts.tool_workspace.regions[2]).toMatchObject({ id: 'output_panel', order: 3 });
        expect(payload.app.governance.workspaceLayout).toMatchObject({
            entry: 'tool_workspace',
            template: 'dashboard',
            density: 'compact',
            primaryRegion: 'center',
            outputRegion: 'bottom',
            visibleRegionCount: 3,
            regionIds: ['file_queue', 'settings_panel', 'output_panel', 'preview_panel'],
            savedInManifest: true,
        });
        expect(payload.app.governance.workspaceLayout.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        expect(payload.app.binding.ui.layouts.tool_workspace.fingerprint).toBe(payload.app.governance.workspaceLayout.fingerprint);
        expect(payload.app.governance.workspaceLayout.regions).toEqual(payload.app.binding.ui.layouts.tool_workspace.regions);
        expect(payload.app.governance.resultContract).toMatchObject({ primary: 'artifact', outputModes: ['docx', 'pdf'] });
        expect(payload.app.governance.testProtocol.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        expect(payload.app.governance.testEvidence).toMatchObject({
            runId: 'run-ok-1',
            artifactPresent: true,
            artifactCount: 1,
            artifactName: 'sample-output.pdf',
            resultCoverage: { ok: true, primary: 'artifact', missingTypes: [] },
        });
        expect(payload.app.governance.testEvidence.outputs).toEqual(expect.arrayContaining([
            expect.objectContaining({ title: 'Sample PDF', kind: 'artifact' }),
        ]));
    });

    it('hides stale SkillMarket results when the picker query changes', async () => {
        searchMixedSkillsMock.mockResolvedValueOnce([{
            id: 'alpha-market-skill',
            name: 'Alpha Market Skill',
            description: 'Old result',
            source: 'skillmarket',
            source_label: 'SkillMarket',
            installed: false,
        }]);
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        const skillPicker = screen.getByTestId('studio-tool-skill-picker');
        fireEvent.change(skillPicker.querySelector('input') as HTMLInputElement, { target: { value: 'alpha' } });
        fireEvent.click(within(skillPicker).getByRole('button', { name: /^Search$/ }));
        await waitFor(() => expect(within(skillPicker).getByText('Alpha Market Skill')).not.toBeNull());

        fireEvent.change(skillPicker.querySelector('input') as HTMLInputElement, { target: { value: 'beta' } });
        expect(within(skillPicker).queryByText('Alpha Market Skill')).toBeNull();
    });

    it('shows an empty market state when search results only duplicate installed skills', async () => {
        listNLSkillsMock.mockResolvedValue([{ name: 'alpha-skill', description: 'Installed alpha' }]);
        searchMixedSkillsMock.mockResolvedValueOnce([{
            id: 'alpha-skill',
            name: 'Alpha Skill from Market',
            description: 'Duplicate market result',
            source: 'skillmarket',
            source_label: 'SkillMarket',
            installed: false,
        }]);
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        const skillPicker = screen.getByTestId('studio-tool-skill-picker');
        await waitFor(() => expect(within(skillPicker).getAllByText('alpha-skill').length).toBeGreaterThan(0));
        fireEvent.change(skillPicker.querySelector('input') as HTMLInputElement, { target: { value: 'alpha' } });
        fireEvent.click(within(skillPicker).getByRole('button', { name: /^Search$/ }));

        await waitFor(() => expect(within(skillPicker).getByText('No matching market skills')).not.toBeNull());
        expect(within(skillPicker).queryByText('Alpha Skill from Market')).toBeNull();
    });

    it('shows SkillMarket results above the installed skill list', async () => {
        listNLSkillsMock.mockResolvedValue([{ name: 'alpha-installed-skill', description: 'Installed alpha' }]);
        searchMixedSkillsMock.mockResolvedValueOnce([{
            id: 'alpha-market-skill',
            name: 'Alpha Market Skill',
            description: 'Market alpha',
            source: 'skillmarket',
            source_label: 'SkillMarket',
            installed: false,
        }]);
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        const skillPicker = screen.getByTestId('studio-tool-skill-picker');
        await waitFor(() => expect(within(skillPicker).getAllByText('alpha-installed-skill').length).toBeGreaterThan(0));
        fireEvent.change(skillPicker.querySelector('input') as HTMLInputElement, { target: { value: 'alpha' } });
        fireEvent.click(within(skillPicker).getByRole('button', { name: /^Search$/ }));

        const marketHeader = await within(skillPicker).findByText('Hub / HubCenter');
        const marketResult = within(skillPicker).getByText('Alpha Market Skill');
        const installedHeader = within(skillPicker).getByText('Installed skills');
        expect(marketHeader.compareDocumentPosition(installedHeader) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(marketResult.compareDocumentPosition(installedHeader) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it('ignores slower SkillMarket responses after a newer search completes', async () => {
        let resolveAlpha: (value: unknown[]) => void = () => undefined;
        searchMixedSkillsMock.mockImplementation((query: string) => {
            if (query === 'alpha') {
                return new Promise((resolve) => {
                    resolveAlpha = resolve;
                });
            }
            return Promise.resolve([{
                id: 'beta-market-skill',
                name: 'Beta Market Skill',
                description: 'New result',
                source: 'skillmarket',
                source_label: 'SkillMarket',
                installed: false,
            }]);
        });
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        const skillPicker = screen.getByTestId('studio-tool-skill-picker');
        fireEvent.change(skillPicker.querySelector('input') as HTMLInputElement, { target: { value: 'alpha' } });
        fireEvent.click(within(skillPicker).getByRole('button', { name: /^Search$/ }));
        fireEvent.change(skillPicker.querySelector('input') as HTMLInputElement, { target: { value: 'beta' } });
        fireEvent.click(within(skillPicker).getByRole('button', { name: /^Search$/ }));

        await waitFor(() => expect(within(skillPicker).getByText('Beta Market Skill')).not.toBeNull());
        resolveAlpha([{
            id: 'alpha-market-skill',
            name: 'Alpha Market Skill',
            description: 'Old result',
            source: 'skillmarket',
            source_label: 'SkillMarket',
            installed: false,
        }]);

        await waitFor(() => expect(within(skillPicker).queryByText('Alpha Market Skill')).toBeNull());
        expect(within(skillPicker).getByText('Beta Market Skill')).not.toBeNull();
    });

    it('exposes app studio sections as accessible tabs', () => {
        window.localStorage.clear();
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());

        const createTab = getCreateTab();
        expect(createTab.getAttribute('aria-selected')).toBe('true');
        expect(container.querySelector('.apps-preview--studio')).not.toBeNull();
        expect(container.querySelector('.apps-preview--studio > .apps-studio-panel')).not.toBeNull();
        expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe(createTab.id);

        fireEvent.keyDown(createTab, { key: 'ArrowRight' });

        const manageTab = screen.getByRole('tab', { name: '应用管理' });
        expect(manageTab.getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('tabpanel').getAttribute('id')).toBe(manageTab.getAttribute('aria-controls'));

        fireEvent.keyDown(manageTab, { key: 'End' });
        const publishTab = getPublishTab();
        expect(publishTab.getAttribute('aria-selected')).toBe('true');
        expect(screen.getByText('暂无本地应用可发布')).not.toBeNull();
    });

    it('shows local apps in the review and publish checklist', () => {
        window.localStorage.clear();
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getCreateAppNameInput(), { target: { value: '合同归档' } });
        clickCreateLocalApp();
        fireEvent.click(getPublishTab());

        expect(screen.getByText('发布检查')).not.toBeNull();
        expect(screen.getAllByText('合同归档').length).toBeGreaterThan(0);
        expect(screen.getByText('需补齐')).not.toBeNull();
        expect(screen.getByText('Manifest 结构')).not.toBeNull();
        expect(screen.getByText('绑定能力')).not.toBeNull();
        expect(screen.getByText('运行证据')).not.toBeNull();
        expect(screen.getByText('提交包预览')).not.toBeNull();
        expect(screen.getByText(/maclaw.app.pack.v1/)).not.toBeNull();
        const packagePreview = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(packagePreview.schema).toBe('maclaw.app.pack.v1');
        expect(packagePreview.apps).toEqual([]);
        expect(screen.getByText('Workspace layout')).not.toBeNull();
        expect(screen.getByText('结果契约')).not.toBeNull();
    });

    it('renders the publish checklist in zh-Hant', () => {
        window.localStorage.clear();
        render(<AppsPage lang="zh-Hant" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getCreateAppNameInput(), { target: { value: '合約歸檔' } });
        // Shared studio designers render zh-Hant (layout template + region pill).
        expect(screen.getAllByText('文件工作臺').length).toBeGreaterThan(0);
        expect(screen.getAllByText('檔案佇列').length).toBeGreaterThan(0);
        clickCreateLocalApp();
        fireEvent.click(getPublishTab());

        expect(screen.getByText('釋出檢查')).not.toBeNull();
        expect(screen.getByText('需補齊')).not.toBeNull();
        expect(screen.getByText('Manifest 結構')).not.toBeNull();
        expect(screen.getByText('執行證據')).not.toBeNull();
        expect(screen.getByText('依賴 Skill')).not.toBeNull();
        expect(screen.getByText(/尚無執行證據/)).not.toBeNull();
        expect(screen.getByText(/檢查項：\d+\/\d+ 透過/)).not.toBeNull();
        expect(screen.queryByText('运行证据')).toBeNull();
    });

    it('blocks publish readiness when workspace layout misses required region roles', () => {
        const app = {
            id: 'layout-missing-output-app',
            name: 'Layout Missing Output App',
            description: 'Publish check should catch missing output region role',
            category: 'Tools',
            kind: 'tool_app',
            icon: 'sheet',
            accent: '#4b6572',
            source: 'local',
            pinned: false,
            recentUsedAt: '2026-06-24T00:00:00.000Z',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'skill',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                appSkill: { id: 'layout-missing-output-skill', version: '1.0.0', source: 'local' },
                skill: { id: 'layout-missing-output-skill', inputMode: 'form', outputModes: ['json'], fields: [] },
                dependencies: { skills: [{ id: 'layout-missing-output-skill', kind: 'app_runtime_skill', required: true, source: 'local' }] },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    generated: true,
                    entry: 'tool_workspace',
                    layouts: {
                        tool_workspace: {
                            template: 'classic_split',
                            density: 'comfortable',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            regions: [{ id: 'file_queue', role: 'input', placement: 'left' }],
                            studio: { editable: true, savedInManifest: true },
                        },
                    },
                },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'content', types: ['content'], outputs: [{ id: 'content', type: 'content', required: true }] },
                testProtocol: { schema: 'maclaw.app.test_protocol.v1', requiredRuns: 1, cases: [{ id: 'smoke', name: 'Smoke', required: true, expectedOutputs: ['content'] }] },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Layout Missing Output App')) as HTMLElement;
        expect(within(card).getByText('Needs work')).not.toBeNull();
        expect(within(card).getByText(/Missing workspace region roles: output/)).not.toBeNull();
    });

    it('blocks publish readiness when run evidence belongs to an older workspace layout', () => {
        const app = dynamicToolApp('layout-stale-evidence-app', 'Layout Stale Evidence App', 'Tools', 'pdf', ['pdf']);
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            artifacts: [{ id: 'artifact-layout-stale', uri: 'artifact://skill-run/run-layout-stale/artifact-layout-stale', name: 'layout-stale.pdf', status: 'ready' }],
            outputs: [{ kind: 'artifact', title: 'Layout stale PDF', artifact_id: 'artifact-layout-stale', status: 'ready' }],
            resultPayload: { status: 'ok' },
            workspaceLayoutFingerprint: 'old-layout-fingerprint',
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Layout Stale Evidence App')) as HTMLElement;
        expect(within(card).getByText('Needs work')).not.toBeNull();
        expect(within(card).getByText(/Workspace layout changed; rerun the test/)).not.toBeNull();
    });

    it('submits ready local apps into local review state', async () => {
        window.localStorage.clear();
        render(<AppsPage lang="zh-Hans" />);

        await createAndRunLocalToolApp('合同归档');
        fireEvent.click(screen.getByTitle('创建应用'));
        fireEvent.click(screen.getByText('审核/发布'));

        await waitFor(() => expect(screen.getByText('\u53ef\u63d0\u4ea4')).not.toBeNull());
        fireEvent.click(screen.getByText('一键发布'));

        await waitFor(() => expect(screen.getAllByText('本地待同步').length).toBeGreaterThan(0));
        expect(screen.getAllByText('已提交').length).toBeGreaterThan(0);
        expect(screen.getAllByText(/local-review-/).length).toBeGreaterThan(0);
        expect(screen.getByText(/"status": "submitted"/)).not.toBeNull();
        expect(screen.getByText(/"channel": "local"/)).not.toBeNull();
        expect(window.localStorage.getItem('maclaw:apps-publish-submissions:v1')).toContain('local-review-');

        fireEvent.click(screen.getByText('撤回提交'));
        await waitFor(() => expect(screen.getByText('\u53ef\u63d0\u4ea4')).not.toBeNull());
        expect(screen.queryByText('等待企业市场审核')).toBeNull();
        expect(screen.getByText(/"status": "local_tested"/)).not.toBeNull();
        expect(window.localStorage.getItem('maclaw:apps-publish-submissions:v1')).not.toContain('local-review-');
    });

    it('shows returned review status and allows resubmission', async () => {
        const { approvalDecisions: _approvalDecisions, ...reviewedResultContract } = testToolAppResultContract(['docx', 'pdf']);
        const reviewedApp = {
            id: 'local-app-review-returned',
            name: '审核回退应用',
            description: '用于验证市场审核回写状态',
            category: '法务',
            kind: 'tool_app',
            icon: 'contract',
            accent: '#7c3f58',
            pinned: false,
            source: 'local',
            recentUsedAt: '2026-06-17T00:00:00.000Z',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'skill',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                appSkill: { id: 'contract-review', version: '1.0.0' },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    generated: true,
                    entry: 'tool_workspace',
                    layouts: {
                        tool_workspace: {
                            type: 'tool_workspace',
                            toolbar: ['add_file', 'run', 'cancel', 'open_output'],
                            template: 'document_workspace',
                            density: 'comfortable',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            regions: [
                                { id: 'file_queue', role: 'input', placement: 'left' },
                                { id: 'settings_panel', role: 'parameters', placement: 'right' },
                                { id: 'preview_panel', role: 'preview', placement: 'center' },
                                { id: 'output_panel', role: 'output', placement: 'right' },
                            ],
                            studio: { editable: true, savedInManifest: true, updatedBy: 'app_studio' },
                        },
                    },
                },
                skill: { id: 'contract-review', inputMode: 'mixed', multipleFiles: false, outputModes: ['docx', 'pdf'], fields: [] },
                resultContract: reviewedResultContract,
                testProtocol: testToolAppProtocol(['docx', 'pdf']),
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [reviewedApp.id],
            customApps: [reviewedApp],
            recentUsedAtById: { [reviewedApp.id]: reviewedApp.recentUsedAt },
        }));
        seedSuccessfulLocalAppRun(reviewedApp, {
            artifacts: [{ id: 'artifact-queue-doc', uri: 'artifact://skill-run/run-ok-reviewed/artifact-queue-doc', name: 'queue-review.pdf', status: 'ready' }],
            outputs: [{ kind: 'artifact', title: 'Queue review PDF', artifact_id: 'artifact-queue-doc', status: 'ready' }],
            resultPayload: { status: 'ok' },
        });
        window.localStorage.setItem('maclaw:apps-publish-submissions:v1', JSON.stringify({
            [reviewedApp.id]: {
                id: 'local-review-returned',
                appID: reviewedApp.id,
                submittedAt: '2026-06-17T00:00:00.000Z',
                reviewedAt: '2026-06-17T00:10:00.000Z',
                status: 'review_failed',
                reviewer: 'market-reviewer',
                message: '请补充运行证',
                reviewIssues: [{ path: 'apps[0].app.governance.testEvidence', severity: 'error', message: '缺少运行证据', suggestion: '先运行一次应用' }],
            },
        }));

        // The package preview only lists review-ready apps; the authoritative
        // dependency check must pass for the card to render the stored status.
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: reviewedApp.id, name: reviewedApp.name, kind: reviewedApp.kind }],
            dependencies: [{ id: 'contract-review', kind: 'runtime_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [reviewedApp.id] }],
            has_missing_required: false,
            has_blocking_dependency: false,
        });

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());

        expect(screen.getAllByText('\u5ba1\u6838\u9700\u4fee\u6539').length).toBeGreaterThan(0);
        expect(screen.getByText('请补充运行证')).not.toBeNull();
        expect(screen.getByText('apps[0].app.governance.testEvidence')).not.toBeNull();
        expect(screen.getByText('缺少运行证据')).not.toBeNull();
        expect(screen.getByText('先运行一次应用')).not.toBeNull();
        await waitFor(() => expect(screen.getByText(/"status": "review_failed"/)).not.toBeNull());

        await waitFor(() => expect(screen.getByText('一键发布')).not.toBeNull());
        fireEvent.click(screen.getByText('一键发布'));
        await waitFor(() => expect(screen.getAllByText('本地待同步').length).toBeGreaterThan(0));
        expect(screen.getByText(/"status": "submitted"/)).not.toBeNull();
    });

    it('verifies dependencies and writes back evidence when clicking 处理依赖', async () => {
        const reviewedApp = {
            id: 'local-app-review-dependency',
            name: '依赖回退应用',
            description: '用于验证市场审核依赖修复入口',
            category: '法务',
            kind: 'tool_app',
            icon: 'contract',
            accent: '#2f5f98',
            pinned: false,
            source: 'local',
            recentUsedAt: '2026-06-17T00:00:00.000Z',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'skill',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                skill: { id: 'contract-review', inputMode: 'mixed', multipleFiles: false, outputModes: ['docx', 'pdf'], fields: [] },
                dependencies: { skills: [{ id: 'contract-review', kind: 'runtime_skill', required: true, source: 'hub' }] },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [reviewedApp.id],
            customApps: [reviewedApp],
            recentUsedAtById: { [reviewedApp.id]: reviewedApp.recentUsedAt },
        }));
        seedSuccessfulLocalAppRun(reviewedApp);
        window.localStorage.setItem('maclaw:apps-publish-submissions:v1', JSON.stringify({
            [reviewedApp.id]: {
                id: 'local-review-dependency',
                appID: reviewedApp.id,
                submittedAt: '2026-06-17T00:00:00.000Z',
                reviewedAt: '2026-06-17T00:10:00.000Z',
                status: 'review_failed',
                reviewer: 'market-reviewer',
                message: '依赖验证过期',
                reviewIssues: [{ path: 'apps[0].app.governance.dependencyVerification', severity: 'error', message: 'dependency verification is stale', suggestion: '安装或重新检查依' }],
            },
        }));
        // The publish pane also verifies dependencies in the background via the
        // authoritative PlanMaclawAppInstall effect, so this must outlast one call.
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: reviewedApp.id, name: reviewedApp.name, kind: reviewedApp.kind }],
            dependencies: [{
                id: 'contract-review',
                kind: 'runtime_skill',
                required: true,
                source: 'hub',
                installed: true,
                health: 'ready',
                action: 'skip',
                app_ids: [reviewedApp.id],
            }],
            has_missing_required: false,
            has_blocking_dependency: false,
        });

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('处理依赖'));

        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalled());
        await waitFor(() => {
            const stored = latestStoredCustomApp('依赖回退应用');
            expect(stored?.installEvidence?.dependency_verification?.dependencies).toHaveLength(1);
        });
        await waitFor(() => {
            const bar = document.querySelector('[data-testid^="apps-dependency-action-"]');
            expect(bar?.textContent || '').toMatch(/依赖已就绪|依赖与运行证据|依赖验证已完成/);
        });
        await waitFor(() => expect(recordMaclawAppInstallMock).toHaveBeenCalledWith(expect.any(String), 'app_studio'));
        expect(installMaclawAppDependenciesMock).not.toHaveBeenCalled();
    });
    it('installs missing dependencies before writing back evidence when clicking 处理依赖', async () => {
        const reviewedApp = {
            id: 'local-app-install-dependency',
            name: '依赖安装应用',
            description: '用于验证处理依赖先安装再回写证据',
            category: '法务',
            kind: 'tool_app',
            icon: 'contract',
            accent: '#2f5f98',
            pinned: false,
            source: 'local',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'skill',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                skill: { id: 'contract-review', inputMode: 'mixed', multipleFiles: false, outputModes: ['docx', 'pdf'], fields: [] },
                dependencies: { skills: [{ id: 'contract-review', kind: 'runtime_skill', required: true, source: 'hub' }] },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [reviewedApp.id],
            customApps: [reviewedApp],
        }));
        seedSuccessfulLocalAppRun(reviewedApp, { dependencyVerification: undefined });
        // The authoritative background verification also calls PlanMaclawAppInstall;
        // keep returning the missing plan until the explicit install fixes it.
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: reviewedApp.id, name: reviewedApp.name, kind: reviewedApp.kind }],
            dependencies: [{
                id: 'contract-review',
                kind: 'runtime_skill',
                required: true,
                source: 'hub',
                installed: false,
                health: 'missing',
                action: 'install',
                app_ids: [reviewedApp.id],
            }],
            has_missing_required: true,
            has_blocking_dependency: false,
        });
        installMaclawAppDependenciesMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: reviewedApp.id, name: reviewedApp.name, kind: reviewedApp.kind }],
            dependencies: [{
                id: 'contract-review',
                kind: 'runtime_skill',
                required: true,
                source: 'hub',
                installed: true,
                health: 'ready',
                action: 'skip',
                app_ids: [reviewedApp.id],
            }],
            has_missing_required: false,
            has_blocking_dependency: false,
        });

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('处理依赖'));

        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalled());
        await waitFor(() => {
            const stored = latestStoredCustomApp('依赖安装应用');
            expect(stored?.installEvidence?.dependency_verification?.dependencies?.[0]?.installed).toBe(true);
        });
        await waitFor(() => {
            const bar = document.querySelector('[data-testid^="apps-dependency-action-"]');
            expect(bar?.textContent || '').toMatch(/依赖已就绪|依赖与运行证据|依赖验证已完成/);
        });
        await waitFor(() => expect(recordMaclawAppInstallMock).toHaveBeenCalledWith(expect.any(String), 'app_studio'));
    });
    it('restores the run evidence check from durable run history in the publish pane', async () => {
        window.localStorage.clear();
        render(<AppsPage lang="zh-Hans" />);

        await createAndRunLocalToolApp('durable 证据应用');
        const createdApp = latestStoredCustomApp('durable 证据应用');
        expect(createdApp?.id).toBeTruthy();
        const raw = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
        // The durable store dedupes by runID at write time (the Go backend
        // merges same-runID records), so it never holds the seeded same-runID
        // decoy ('00000000') — that entry only exists in the localStorage UI
        // cache. Mock the durable store with the real run records it would
        // actually have kept.
        const durableEntries = (raw[createdApp.id] || []).filter((entry) => entry.definitionHash !== '00000000');
        expect(durableEntries.length).toBeGreaterThan(0);
        // Simulate a localStorage wipe: only the durable store still has the evidence.
        window.localStorage.setItem(runHistoryStorageKey, '{}');
        listMaclawAppRunHistoryMock.mockResolvedValue(durableEntries);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLElement);
        fireEvent.click(screen.getByRole('tab', { name: /审核\/发布/ }));
        const publishCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('durable 证据应用')) as HTMLElement;
        expect(publishCard).toBeTruthy();
        await waitFor(() => expect(listMaclawAppRunHistoryMock).toHaveBeenCalledWith(createdApp.id, 8));
        await waitFor(() => expect(within(publishCard).getByText(/检查项：10\/10 通过/)).not.toBeNull());
    });
    it('navigates to the app run workspace from 去修复 when run evidence is missing', async () => {
        const base = dynamicToolApp('local-app-fix-run-evidence', '运行证据修复应用', '法务', 'contract', ['pdf']);
        const app = {
            ...base,
            manifest: { ...base.manifest, launchMode: 'fixed_skill_ui' },
            installEvidence: {
                dependency_verification: testDependencyVerificationForApp(base),
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
        }));

        render(<AppsPage lang="zh-Hans" />);

        await waitFor(() => expect(screen.getAllByText(app.name).length).toBeGreaterThan(0));
        fireEvent.click(getStudioButton());
        await waitFor(() => expect(document.querySelector('[role="tab"]')).not.toBeNull());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('去修复'));

        await waitFor(() => expect(screen.getByText('执行')).not.toBeNull());
        expect((screen.getByRole('button', { name: '返回审核/发布' }) as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(screen.getByRole('button', { name: /关闭.*运行证据修复应用/ }));
        expect(screen.queryByRole('button', { name: '返回审核/发布' })).toBeNull();

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('去修复'));
        const returnButton = await screen.findByRole('button', { name: '返回审核/发布' });
        expect((returnButton as HTMLButtonElement).disabled).toBe(true);

        const fileInput = document.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        fireEvent.change(fileInput, { target: { files: [new File(['demo'], 'evidence.pdf', { type: 'application/pdf' })] } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(recordMaclawAppRunHistoryMock).toHaveBeenCalledWith(expect.objectContaining({
            appID: app.id,
            runID: 'run-test-1',
            status: 'done',
        })));
        await waitFor(() => expect((returnButton as HTMLButtonElement).disabled).toBe(false));
        fireEvent.click(returnButton);
        await waitFor(() => expect(getPublishTab().getAttribute('aria-selected')).toBe('true'));
        expect(screen.getAllByText('运行证据修复应用').length).toBeGreaterThan(0);
        // Returning to review must immediately consume the just-persisted
        // evidence, rather than leaving the card on its old missing-evidence
        // snapshot until the page is reopened.
        const publishCard = Array.from(document.querySelectorAll('.apps-publish-card'))
            .find((item) => item.textContent?.includes('运行证据修复应用')) as HTMLElement;
        await waitFor(() => {
            const evidenceCheck = within(publishCard).getByText('运行证据').closest('.apps-publish-check');
            expect(evidenceCheck?.getAttribute('data-ok')).toBe('true');
        });
    });
    it('unlocks return only after the successful run evidence reaches durable storage', async () => {
        let resolveDurableWrite: ((value: any) => void) | undefined;
        const durableWrite = new Promise<any>((resolve) => { resolveDurableWrite = resolve; });
        recordMaclawAppRunHistoryMock.mockImplementationOnce(() => durableWrite);
        const base = dynamicToolApp('local-app-durable-return', '运行证据落盘应用', '法务', 'contract', ['pdf']);
        const app = {
            ...base,
            manifest: { ...base.manifest, launchMode: 'fixed_skill_ui' },
            installEvidence: { dependency_verification: testDependencyVerificationForApp(base) },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app] }));

        render(<AppsPage lang="zh-Hans" />);
        await waitFor(() => expect(screen.getAllByText(app.name).length).toBeGreaterThan(0));
        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('去修复'));
        const returnButton = await screen.findByRole('button', { name: '返回审核/发布' });
        const fileInput = document.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        fireEvent.change(fileInput, { target: { files: [new File(['demo'], 'evidence.pdf', { type: 'application/pdf' })] } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(recordMaclawAppRunHistoryMock).toHaveBeenCalled());
        expect((returnButton as HTMLButtonElement).disabled).toBe(true);
        await screen.findByText('运行完成 · 正在保存运行证据…');
        resolveDurableWrite?.({ appID: app.id, runID: 'run-test-1', status: 'done' });
        await waitFor(() => expect((returnButton as HTMLButtonElement).disabled).toBe(false));
        expect(screen.getByText('运行完成 · 运行证据已保存')).not.toBeNull();
    });
    it('keeps return blocked and shows the durable write error', async () => {
        recordMaclawAppRunHistoryMock.mockRejectedValueOnce(new Error('disk unavailable'));
        const base = dynamicToolApp('local-app-durable-error', '运行证据失败应用', '法务', 'contract', ['pdf']);
        const app = {
            ...base,
            manifest: { ...base.manifest, launchMode: 'fixed_skill_ui' },
            installEvidence: { dependency_verification: testDependencyVerificationForApp(base) },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app] }));

        render(<AppsPage lang="zh-Hans" />);
        await waitFor(() => expect(screen.getAllByText(app.name).length).toBeGreaterThan(0));
        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('去修复'));
        const returnButton = await screen.findByRole('button', { name: '返回审核/发布' });
        const fileInput = document.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        fireEvent.change(fileInput, { target: { files: [new File(['demo'], 'evidence.pdf', { type: 'application/pdf' })] } });
        fireEvent.click(screen.getByText('执行'));

        await screen.findByText(/运行证据未写入本机存储.*disk unavailable/);
        expect((returnButton as HTMLButtonElement).disabled).toBe(true);
    });
    it('keeps return blocked when durable storage acknowledges a different run', async () => {
        recordMaclawAppRunHistoryMock.mockResolvedValueOnce({ appID: 'wrong-app', runID: 'wrong-run', status: 'done' });
        const base = dynamicToolApp('local-app-durable-mismatch', '运行证据确认应用', '法务', 'contract', ['pdf']);
        const app = {
            ...base,
            manifest: { ...base.manifest, launchMode: 'fixed_skill_ui' },
            installEvidence: { dependency_verification: testDependencyVerificationForApp(base) },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app] }));

        render(<AppsPage lang="zh-Hans" />);
        await waitFor(() => expect(screen.getAllByText(app.name).length).toBeGreaterThan(0));
        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('去修复'));
        const returnButton = await screen.findByRole('button', { name: '返回审核/发布' });
        const fileInput = document.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        fireEvent.change(fileInput, { target: { files: [new File(['demo'], 'evidence.pdf', { type: 'application/pdf' })] } });
        fireEvent.click(screen.getByText('执行'));

        await screen.findByText(/运行证据未写入本机存储.*acknowledgement does not match/);
        expect((returnButton as HTMLButtonElement).disabled).toBe(true);
    });
    it('prefers the run workspace over dependency resolution from 去修复 when both checks fail', async () => {
        // Both 依赖 Skill (missing verification) and 运行证据 (no run) fail.
        // 去修复 prioritizes run workspace so the user executes once first.
        const base = dynamicToolApp('local-app-fix-both', '双检查修复应用', '法务', 'contract', ['pdf']);
        const app = {
            ...base,
            manifest: { ...base.manifest, launchMode: 'fixed_skill_ui' },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
        }));

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('去修复'));

        await waitFor(() => expect(screen.getByText('执行')).not.toBeNull());
        expect(document.querySelector(`[data-testid="apps-dependency-action-${app.id}"]`)).toBeNull();
    });
    it('does not block 处理依赖 on missing/stale run-evidence governance issues', async () => {
        // PlanMaclawAppInstall bundles full publish governance (including
        // testEvidence). 处理依赖 must still succeed when only run evidence
        // is missing — that is a separate publish checklist card.
        const base = dynamicToolApp('local-app-governance-detail', '治理复核明细应用', '法务', 'contract', ['pdf']);
        const app = {
            ...base,
            manifest: { ...base.manifest, launchMode: 'fixed_skill_ui' },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
        }));
        // The authoritative background verification consumes the same mock, so
        // this plan must be returned for every call, not just the first.
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{ id: app.id, kind: 'app_skill', required: true, source: 'local', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            governance_review_issues: [{
                path: 'apps[0].app.governance.testEvidence.definitionHash',
                severity: 'error',
                message: 'run evidence definition hash does not match current app definition',
                suggestion: 'run the current app definition again before submitting to the capability market',
            }, {
                path: 'apps[0].app.governance.testEvidence',
                severity: 'error',
                message: 'missing successful local run evidence',
                suggestion: 'run the app once in App Studio before submitting to the capability market',
            }],
            has_governance_review_issue: true,
            has_missing_required: false,
            has_blocking_dependency: false,
        });

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('处理依赖'));

        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalled());
        await waitFor(() => {
            const bar = document.querySelector(`[data-testid="apps-dependency-action-${app.id}"]`);
            expect(bar?.textContent || '').toMatch(/依赖已就绪|依赖与运行证据|依赖验证已完成/);
            expect(bar?.textContent || '').not.toContain('依赖治理复核未通过');
            expect(bar?.textContent || '').not.toContain('missing successful local run evidence');
        });
        const stored = latestStoredCustomApp('治理复核明细应用');
        expect(stored?.installEvidence?.dependency_verification?.has_governance_review_issue).toBe(false);
        expect(stored?.installEvidence?.dependency_verification?.governance_review_issues || []).toEqual([]);
    });
    it('auto-collects publish run evidence after 处理依赖 when sample input needs no file', async () => {
        const base = dynamicToolApp('local-app-auto-evidence', '自动证据应用', '法务', 'contract', ['json']);
        const app = {
            ...base,
            manifest: {
                ...base.manifest,
                launchMode: 'fixed_skill_ui',
                skill: { ...(base.manifest.skill || {}), id: 'auto-evidence-skill', inputMode: 'params', multipleFiles: false, outputModes: ['json'], fields: [] },
                appSkill: { id: 'auto-evidence-skill', version: '1.0.0', source: 'local' },
                dependencies: { skills: [{ id: 'auto-evidence-skill', kind: 'runtime_skill', required: true, source: 'local' }] },
                testProtocol: {
                    schema: 'maclaw.app.test_protocol.v1',
                    sampleInput: { params: 'hello', note: 'sample' },
                    expectedOutput: { status: 'ok', primary: 'content' },
                    requiredRoles: [],
                    requiredScopes: [],
                    riskLevel: 'low',
                },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
        }));
        planMaclawAppInstallMock.mockImplementation(async () => ({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{
                id: 'auto-evidence-skill',
                kind: 'runtime_skill',
                required: true,
                source: 'local',
                installed: true,
                health: 'ready',
                action: 'skip',
                app_ids: [app.id],
            }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        }));
        runNLSkillAsyncMock.mockReset().mockResolvedValue('run-auto-evidence-1');
        getNLSkillRunStatusMock.mockReset().mockResolvedValue({
            status: 'done',
            summary: { content: 'ok', primary_result: 'ok' },
            result: { content: 'ok' },
            artifacts: [],
            outputs: [{ kind: 'content', text: 'ok', status: 'ready' }],
        });

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('处理依赖'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalled(), { timeout: 5000 });
        await waitFor(() => {
            const bar = document.querySelector(`[data-testid="apps-dependency-action-${app.id}"]`);
            expect(bar?.textContent || '').toContain('依赖与运行证据已就绪');
        }, { timeout: 5000 });
        expect(recordMaclawAppRunHistoryMock).toHaveBeenCalled();
        const stored = latestStoredCustomApp('自动证据应用');
        expect(stored?.installEvidence?.dependency_verification).toBeTruthy();
        const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}');
        expect((history[app.id] || []).some((item: any) => item.runID === 'run-auto-evidence-1' && item.status === 'done')).toBe(true);
    });
    it('still blocks 处理依赖 on dependencyVerification governance issues', async () => {
        const base = dynamicToolApp('local-app-dep-gov-block', '依赖治理阻断应用', '法务', 'contract', ['pdf']);
        const app = {
            ...base,
            manifest: { ...base.manifest, launchMode: 'fixed_skill_ui' },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
        }));
        // The authoritative background verification consumes the same mock, so
        // this plan must be returned for every call, not just the first.
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{ id: app.id, kind: 'app_skill', required: true, source: 'local', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            governance_review_issues: [{
                path: 'apps[0].app.governance.dependencyVerification',
                severity: 'error',
                message: 'required dependency is not ready: blocked-skill',
                suggestion: 'install or enable the required Skill dependency before submitting',
            }],
            has_governance_review_issue: true,
            has_missing_required: false,
            has_blocking_dependency: false,
        });

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        fireEvent.click(screen.getByText('处理依赖'));

        await waitFor(() => {
            const bar = document.querySelector(`[data-testid="apps-dependency-action-${app.id}"]`);
            expect(bar?.textContent).toContain('依赖治理复核未通过');
            expect(bar?.textContent).toContain('required dependency is not ready');
        });
    });
    it('ignores stale missing-dependency-verification noise baked into run evidence', async () => {
        // Real user case: successful run embeds Plan-time governance issues
        // ("missing dependency verification") even though deps are ready — the
        // 依赖 Skill card must turn green after 处理依赖 / from run evidence.
        const base = dynamicToolApp('local-app-stale-dep-noise', '陈旧依赖噪声应用', '法务', 'contract', ['pdf']);
        const skillId = String(base.manifest?.skill?.id || base.id);
        const app = {
            ...base,
            manifest: {
                ...base.manifest,
                launchMode: 'fixed_skill_ui',
                skill: { ...(base.manifest?.skill || {}), id: skillId },
                dependencies: { skills: [{ id: skillId, kind: 'runtime_skill', required: true, source: 'local' }] },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
        }));
        seedSuccessfulLocalAppRun(app, {
            dependencyVerification: {
                schema: 'maclaw.app.install_plan.v1',
                dependencies: [{
                    id: skillId,
                    kind: 'runtime_skill',
                    required: true,
                    source: 'local',
                    installed: true,
                    health: 'ready',
                    action: 'skip',
                    app_ids: [app.id],
                }],
                hasMissingRequired: false,
                hasBlockingDependency: false,
                hasGovernanceReviewIssue: true,
                governanceReviewIssueCount: 2,
                governanceReviewIssues: [
                    {
                        path: 'apps[0].app.governance.testEvidence',
                        severity: 'error',
                        message: 'missing successful local run evidence',
                        suggestion: 'run the app once',
                    },
                    {
                        path: 'apps[0].app.governance.dependencyVerification',
                        severity: 'error',
                        message: 'missing dependency verification',
                        suggestion: 'run dependency verification before submitting to the capability market',
                    },
                ],
            },
            resultPayload: { status: 'ok', content: 'ready' },
            outputs: [{ kind: 'content', text: 'ready', status: 'ready' }],
        });
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{
                id: skillId,
                kind: 'runtime_skill',
                required: true,
                source: 'local',
                installed: true,
                health: 'ready',
                action: 'skip',
                app_ids: [app.id],
            }],
            governance_review_issues: [{
                path: 'apps[0].app.governance.dependencyVerification',
                severity: 'error',
                message: 'missing dependency verification',
                suggestion: 'run dependency verification before submitting to the capability market',
            }],
            has_governance_review_issue: true,
            has_missing_required: false,
            has_blocking_dependency: false,
        });

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        // Sanitize-on-read must treat ready deps + baked "missing dependency verification"
        // noise as a passing 依赖 Skill check (no need to click 处理依赖).
        await waitFor(() => {
            const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((el) =>
                el.textContent?.includes('陈旧依赖噪声应用'),
            ) as HTMLElement | undefined;
            expect(card).toBeTruthy();
            expect(card?.textContent || '').not.toContain('missing dependency verification');
            expect(card?.textContent || '').not.toContain('缺少依赖验证证据');
            // Both deps + run evidence should pass → full checklist green.
            expect(card?.textContent || '').toMatch(/检查项：\s*10\/10|10\/10/);
        });
    });
    it('blocks app package review submission when required dependencies are unavailable', async () => {
        const app = {
            ...dynamicToolApp('local-publish-blocked-dep', '依赖阻断应用', '法务', 'contract', ['json']),
            recentUsedAt: '2026-06-17T00:00:00.000Z',
        };
        app.manifest = {
            ...app.manifest,
            launchMode: 'fixed_skill_ui',
            skill: { ...(app.manifest.skill || {}), id: 'disabled-workflow', appDefinitionFile: 'maclaw.app.json', inputMode: 'file', multipleFiles: false, outputModes: ['json'], fields: [] },
            appSkill: { id: 'disabled-workflow', version: '1.0.0', source: 'local' },
            dependencies: { skills: [{ id: 'disabled-workflow', kind: 'runtime_skill', required: true, source: 'hub' }] },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
            recentUsedAtById: { [app.id]: app.recentUsedAt },
        }));
        seedSuccessfulLocalAppRun(app, { outputMode: 'json', resultPayload: { status: 'ok', content: 'ready' }, outputs: [{ kind: 'content', text: 'ready', status: 'ready' }] });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'should-not-submit' });
        const listMaclawAppPackageSubmissions = vi.fn().mockResolvedValue([]);
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage, ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{
                id: 'disabled-workflow',
                kind: 'runtime_skill',
                required: true,
                installed: true,
                installed_status: 'disabled',
                health: 'disabled',
                action: 'blocked',
                app_ids: [app.id],
            }],
            has_missing_required: false,
            has_blocking_dependency: true,
        });

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        await waitFor(() => expect(screen.getByText('暂无本机待同步提交')).not.toBeNull());
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('依赖阻断应用')) as HTMLElement;
        expect(card).toBeTruthy();

        // ac019880 gates the primary button on the authoritative checks:
        // a blocking plan now disables publishing before any submit attempt.
        await waitFor(() => expect(within(card).getByText(/必需 Skill 依赖缺失或被阻断/)).not.toBeNull());
        await waitFor(() => expect((within(card).getByText('一键发布') as HTMLButtonElement).disabled).toBe(true));
        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalled());
        expect(submitMaclawAppPackage).not.toHaveBeenCalled();
        expect(within(card).queryByText('等待企业市场审核')).toBeNull();
    });

    it('uses the enterprise market bridge when submitting app packages', async () => {
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-test-1',
            status: 'success',
            summary: {
                last_output_snippet: 'Translated contract ready',
                output_blocks: [{ id: 'summary-block', kind: 'content', title: 'Translation summary', text: 'Translated contract ready', status: 'ready' }],
            },
            outputs: [{ id: 'doc-block', kind: 'document', title: 'Translated DOCX', text: 'contract.docx', artifact_id: 'artifact-1', status: 'ready' }],
            artifacts: [
                { id: 'artifact-1', uri: 'artifact://skill-run/run-test-1/artifact-1', name: 'contract.docx', path: '/tmp/contract.docx', status: 'ready' },
                { id: 'artifact-2', uri: 'artifact://skill-run/run-test-1/artifact-2', name: 'report.pdf', path: '/tmp/report.pdf', status: 'ready', mime_type: 'application/pdf', size_bytes: 2048 },
            ],
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({
            submission_id: 'market-review-123',
            submitted_at: '2026-06-17T01:00:00.000Z',
            status: 'submitted',
            message: 'queued',
        });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        window.localStorage.clear();
        render(<AppsPage lang="zh-Hans" />);

        await createAndRunLocalToolApp('合同归档');
        const toolApp = latestStoredCustomApp('合同归档');
        expect(toolApp).toBeTruthy();
        const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
        const toolSkillID = String(toolApp.manifest?.skill?.id || toolApp.manifest?.appSkill?.id || `${toolApp.id}-app`);
        history[toolApp.id][0].dependencyVerification = {
            schema: 'maclaw.app.install_plan.v1',
            verifiedAt: '2026-06-17T00:04:00.000Z',
            appCount: 1,
            dependencyCount: 1,
            hasMissingRequired: false,
            hasBlockingDependency: false,
            hasWorkflowContractIssue: false,
            workflowContractIssueCount: 0,
            hasGovernanceReviewIssue: false,
            governanceReviewIssueCount: 0,
            dependencies: [{ id: toolSkillID, kind: 'app_skill', required: true, source: 'local', installed: true, health: 'ready', action: 'skip', app_ids: [toolApp.id] }],
        };
        window.localStorage.setItem(runHistoryStorageKey, JSON.stringify(history));
        expect(history[toolApp.id]?.[0]).toMatchObject({
            runID: 'run-test-1',
            status: 'done',
            definitionHash: expect.stringMatching(/^[0-9a-f]{8}$/),
            dependencyVerification: {
                schema: 'maclaw.app.install_plan.v1',
                dependencyCount: 1,
                hasBlockingDependency: false,
                dependencies: [expect.objectContaining({ id: toolSkillID, kind: 'app_skill', source: 'local', app_ids: [toolApp.id] })],
            },
            resultPayload: { status: 'done', artifact_id: 'artifact-run-test-1' },
            outputs: [
                expect.objectContaining({ kind: 'artifact', title: 'Generated PDF', artifact_id: 'artifact-run-test-1', status: 'ready' }),
            ],
            resultCoverage: {
                coveredTypes: expect.arrayContaining(['document', 'artifact']),
                missingTypes: [],
            },
        });
        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLElement);
        fireEvent.click(screen.getByRole('tab', { name: /\u5ba1\u6838\/\u53d1\u5e03/ }));
        const publishCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('合同归档')) as HTMLElement;
        expect(publishCard).toBeTruthy();
        await waitFor(() => expect(publishCard.getAttribute('data-ready')).toBe('true'));
        await waitFor(() => expect(within(publishCard).getByText('\u4e00\u952e\u53d1\u5e03')).not.toBeNull());
        fireEvent.click(within(publishCard).getByText('\u4e00\u952e\u53d1\u5e03'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        expect(payload.schema).toBe('maclaw.app.pack.v1');
        expect(payload.apps[0].app.name).toBe('合同归档');
        const layout = payload.apps[0].app.governance.workspaceLayout;
        expect(layout.schema).toBe('maclaw.app.ui.v1');
        expect(layout.entry).toBe('tool_workspace');
        expect(layout.template).toBe('document_workspace');
        expect(layout.regionCount).toBeGreaterThan(0);
        const resultContract = payload.apps[0].app.governance.resultContract;
        expect(resultContract.schema).toBe('maclaw.app.result.v1');
        expect(resultContract.primary).toBe('artifact');
        expect(resultContract.types).toEqual(expect.arrayContaining(['content', 'document', 'artifact']));
        expect(resultContract.delivery.artifacts).toBe(true);
        expect(payload.apps[0].app.binding.testProtocol.schema).toBe('maclaw.app.test_protocol.v1');
        expect(payload.apps[0].app.governance.testProtocol.schema).toBe('maclaw.app.test_protocol.v1');
        expect(payload.apps[0].app.governance.testProtocol.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        const evidence = payload.apps[0].app.governance.testEvidence;
        expect(evidence.runId).toBe('run-test-1');
        expect(evidence.definitionHash).toMatch(/^[0-9a-f]{8}$/);
        expect(evidence.testProtocol.schema).toBe('maclaw.app.test_protocol.v1');
        expect(evidence.testProtocolFingerprint).toBe(payload.apps[0].app.governance.testProtocol.fingerprint);
        expect(evidence.artifactPresent).toBe(true);
        expect(evidence.artifactCount).toBe(2);
        expect(evidence.artifacts).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'artifact-1', name: 'contract.docx' }),
            expect.objectContaining({ id: 'artifact-2', name: 'report.pdf' }),
        ]));
        expect(evidence.resultPayload).toEqual({ text: 'contract.docx' });
        expect(evidence.outputCount).toBe(2);
        expect(evidence.outputs).toEqual(expect.arrayContaining([
            expect.objectContaining({ kind: 'document', title: 'Translated DOCX', artifactId: 'artifact-1', status: 'ready' }),
        ]));
        expect(evidence.resultCoverage).toEqual(expect.objectContaining({
            ok: true,
            primary: 'artifact',
            coveredTypes: expect.arrayContaining(['document', 'artifact']),
            missingTypes: [],
        }));
        expect(evidence.dependencyVerification).toMatchObject({
            schema: 'maclaw.app.install_plan.v1',
            hasBlockingDependency: false,
        });
        expect(screen.getAllByText(/market-review-123/).length).toBeGreaterThan(0);
        // SubmitMaclawAppPackage / local queue channel is local when not synced to Hub yet.
        expect(screen.getByText(/"channel": "local"/)).not.toBeNull();
    });

    it('prefers one-click publish bridge over plain package submit', async () => {
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-one-click-1',
            status: 'success',
            summary: { last_output_snippet: 'ok' },
            outputs: [{ id: 'doc-block', kind: 'document', title: 'Doc', text: 'doc.docx', artifact_id: 'artifact-1', status: 'ready' }],
            artifacts: [{ id: 'artifact-1', uri: 'artifact://skill-run/run-one-click-1/artifact-1', name: 'doc.docx', path: '/tmp/doc.docx', status: 'ready' }],
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'should-not-use-plain-submit' });
        const publishMaclawAppOneClick = vi.fn().mockResolvedValue({
            schema: 'maclaw.app.one_click_publish.v1',
            ok: true,
            local_submission_id: 'one-click-sub-1',
            local_submission: {
                submission_id: 'one-click-sub-1',
                submitted_at: '2026-06-17T02:00:00.000Z',
                status: 'submitted',
                channel: 'local',
                message: 'queued',
            },
            targets: {
                enterprise_hub_pack: { ok: true },
                skill_market: { ok: true, submission_id: 'sm-1' },
            },
            message: 'queued locally; enterprise hub pack submitted; skill market upload ok (sm-1)',
        });
        const publishMaclawAppSubmissionOneClick = vi.fn().mockResolvedValue({
            schema: 'maclaw.app.one_click_publish.v1',
            ok: true,
            partial: false,
            local_submission_id: 'one-click-sub-1',
            local_submission: {
                submission_id: 'one-click-sub-1',
                submitted_at: '2026-06-17T02:00:00.000Z',
                status: 'submitted',
                channel: 'hub',
                message: 'retry ok',
            },
            message: 'queued locally; enterprise hub pack submitted; skill market upload ok (sm-retry)',
        });
        (window as any).go = {
            main: {
                App: {
                    SubmitMaclawAppPackage: submitMaclawAppPackage,
                    PublishMaclawAppOneClick: publishMaclawAppOneClick,
                    PublishMaclawAppSubmissionOneClick: publishMaclawAppSubmissionOneClick,
                },
            },
        };

        window.localStorage.clear();
        render(<AppsPage lang="zh-Hans" />);

        await createAndRunLocalToolApp('一键发布应用');
        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLElement);
        fireEvent.click(screen.getByRole('tab', { name: /\u5ba1\u6838\/\u53d1\u5e03/ }));
        const publishCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('一键发布应用')) as HTMLElement;
        expect(publishCard).toBeTruthy();
        await waitFor(() => expect(within(publishCard).getByText('\u4e00\u952e\u53d1\u5e03')).not.toBeNull());
        fireEvent.click(within(publishCard).getByText('\u4e00\u952e\u53d1\u5e03'));

        await waitFor(() => expect(publishMaclawAppOneClick).toHaveBeenCalledTimes(1));
        expect(submitMaclawAppPackage).not.toHaveBeenCalled();
        const payload = JSON.parse(publishMaclawAppOneClick.mock.calls[0][0]);
        expect(payload.schema).toBe('maclaw.app.pack.v1');
        expect(payload.apps[0].app.name).toBe('一键发布应用');
        expect(screen.getAllByText(/one-click-sub-1/).length).toBeGreaterThan(0);
        expect(screen.getAllByText(/queued locally; enterprise hub pack submitted; skill market upload ok \(sm-1\)/).length).toBeGreaterThan(0);
    });

    it('retries partial one-click via existing submission id instead of re-queuing', async () => {
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-one-click-retry',
            status: 'success',
            summary: { last_output_snippet: 'ok' },
            outputs: [{ id: 'doc-block', kind: 'document', title: 'Doc', text: 'doc.docx', artifact_id: 'artifact-1', status: 'ready' }],
            artifacts: [{ id: 'artifact-1', uri: 'artifact://skill-run/run-one-click-retry/artifact-1', name: 'doc.docx', path: '/tmp/doc.docx', status: 'ready' }],
        });
        const publishMaclawAppOneClick = vi.fn().mockResolvedValue({
            schema: 'maclaw.app.one_click_publish.v1',
            ok: true,
            partial: true,
            local_submission_id: 'partial-sub-1',
            local_submission: {
                submission_id: 'partial-sub-1',
                submitted_at: '2026-06-17T02:00:00.000Z',
                status: 'submitted',
                channel: 'local',
                message: 'queued locally; enterprise hub pack failed: offline; skill market failed: no remote_email',
            },
            message: 'queued locally; enterprise hub pack failed: offline; skill market failed: no remote_email',
        });
        const publishMaclawAppSubmissionOneClick = vi.fn().mockResolvedValue({
            schema: 'maclaw.app.one_click_publish.v1',
            ok: true,
            partial: false,
            local_submission_id: 'partial-sub-1',
            local_submission: {
                submission_id: 'partial-sub-1',
                submitted_at: '2026-06-17T02:00:00.000Z',
                status: 'submitted',
                channel: 'hub',
                message: 'queued locally; enterprise hub pack submitted; skill market upload ok (sm-2)',
            },
            message: 'queued locally; enterprise hub pack submitted; skill market upload ok (sm-2)',
        });
        (window as any).go = {
            main: {
                App: {
                    PublishMaclawAppOneClick: publishMaclawAppOneClick,
                    PublishMaclawAppSubmissionOneClick: publishMaclawAppSubmissionOneClick,
                },
            },
        };

        window.localStorage.clear();
        render(<AppsPage lang="zh-Hans" />);
        await createAndRunLocalToolApp('局部重试应用');
        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('局部重试应用')) as HTMLElement;
        expect(card).toBeTruthy();

        await waitFor(() => expect(within(card).getByText('\u4e00\u952e\u53d1\u5e03')).not.toBeNull());

        fireEvent.click(within(card).getByText('\u4e00\u952e\u53d1\u5e03'));
        await waitFor(() => expect(publishMaclawAppOneClick).toHaveBeenCalledTimes(1));
        expect(publishMaclawAppSubmissionOneClick).not.toHaveBeenCalled();
        await waitFor(() => expect(screen.getAllByText(/enterprise hub pack failed: offline/).length).toBeGreaterThan(0));

        // Partial failure keeps the button enabled and retries the durable row.
        const retryButton = within(card).getByText('\u4e00\u952e\u53d1\u5e03') as HTMLButtonElement;
        expect(retryButton.disabled).toBe(false);
        fireEvent.click(retryButton);
        await waitFor(() => expect(publishMaclawAppSubmissionOneClick).toHaveBeenCalledWith('partial-sub-1'));
        expect(publishMaclawAppOneClick).toHaveBeenCalledTimes(1);
        expect(screen.getAllByText(/skill market upload ok \(sm-2\)/).length).toBeGreaterThan(0);
    });

    it('requires run evidence to cover the declared primary result contract', async () => {
        const app = {
            id: 'publish-contract-coverage-app',
            name: 'Contract Coverage App',
            description: 'Checks publish evidence against result contract',
            category: 'CRM',
            kind: 'enterprise_normal_app',
            icon: 'customer',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_normal_app',
                launchMode: 'agent_dynamic_ui',
                appSkill: { id: 'contract-coverage-skill', version: '1.0.0', source: 'hub' },
                datasrv: { domain: 'sales', objectRole: 'customer', preferredAction: 'sales.customer_upsert' },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'business_workspace',
                    layouts: { business_workspace: { template: 'classic_split', density: 'compact', primaryRegion: 'left', outputRegion: 'right', studio: { savedInManifest: true } } },
                },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'business_status', types: ['business_status', 'business_record', 'content'], delivery: { inlineContent: true, artifacts: false, businessRecord: true, notifications: false } },
            },
            installEvidence: {
                dependency_verification: {
                    schema: 'maclaw.app.install_plan.v1',
                    dependencies: [{ id: 'contract-coverage-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['publish-contract-coverage-app'] }],
                },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-contract-text-only',
            outputMode: 'business',
            resultPayload: { text: 'plain completion only' },
            outputs: [{ kind: 'text', title: 'Plain result', text: 'plain completion only', status: 'ready' }],
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const blockedCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Contract Coverage App')) as HTMLElement;
        expect(blockedCard).toBeTruthy();
        expect(within(blockedCard).getByText('Needs work')).not.toBeNull();
        expect(within(blockedCard).getByText(/Run evidence does not cover result contract: business_status/)).not.toBeNull();
        await waitFor(() => expect((within(blockedCard).getByText('One-click publish') as HTMLButtonElement).disabled).toBe(true));

        cleanup();
        window.localStorage.clear();
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-contract-explicit-missing',
            outputMode: 'business',
            resultPayload: { business_status: 'renewal_ready', text: 'renewal ready' },
            outputs: [{ kind: 'business_status', title: 'Customer status', text: 'renewal_ready', status: 'ready' }],
            resultCoverage: { ok: true, primary: 'business_status', coveredTypes: ['business_status'], missingTypes: ['business_record'] },
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const explicitMissingCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Contract Coverage App')) as HTMLElement;
        expect(explicitMissingCard).toBeTruthy();
        expect(within(explicitMissingCard).getByText('Needs work')).not.toBeNull();
        expect(within(explicitMissingCard).getByText(/Run evidence does not cover result contract: business_record/)).not.toBeNull();
        await waitFor(() => expect((within(explicitMissingCard).getByText('One-click publish') as HTMLButtonElement).disabled).toBe(true));

        cleanup();
        window.localStorage.clear();
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-contract-covered',
            outputMode: 'business',
            resultPayload: { business_status: 'renewal_ready', business_record: { id: 'customer-1' }, text: 'renewal ready' },
            outputs: [{ kind: 'business_record', title: 'Customer renewal', text: '{"id":"customer-1"}', status: 'ready', data: { id: 'customer-1' } }],
            resultCoverage: { ok: true, primary: 'business_status', coveredTypes: ['business_status', 'business_record', 'content'], missingTypes: [] },
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'market-contract-covered', submitted_at: '2026-06-17T01:00:00.000Z', status: 'submitted', message: 'queued' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        // The authoritative dependency check gates readiness; echo the declared
        // app skill as installed/ready for every PlanMaclawAppInstall call.
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{ id: 'contract-coverage-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            has_missing_required: false,
            has_blocking_dependency: false,
        });
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const readyCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Contract Coverage App')) as HTMLElement;
        expect(readyCard).toBeTruthy();
        await waitFor(() => expect(within(readyCard).getByText('Ready to submit')).not.toBeNull());
        expect(within(readyCard).getByText(/Result: business_status/)).not.toBeNull();
        await waitFor(() => expect(within(readyCard).getByText('One-click publish')).not.toBeNull());
        fireEvent.click(within(readyCard).getByText('One-click publish'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        const evidence = payload.apps[0].app.governance.testEvidence;
        expect(evidence.resultPayload).toEqual(expect.objectContaining({
            business_status: 'renewal_ready',
            business_record: { id: 'customer-1' },
            text: 'renewal ready',
        }));
        expect(evidence.outputCount).toBe(1);
        expect(evidence.outputs).toEqual([expect.objectContaining({
            kind: 'business_record',
            title: 'Customer renewal',
            text: '{"id":"customer-1"}',
            status: 'ready',
            data: { id: 'customer-1' },
        })]);
        expect(payload.apps[0].app.governance.testEvidence.resultCoverage).toEqual(expect.objectContaining({
            ok: true,
            primary: 'business_status',
            coveredTypes: expect.arrayContaining(['business_status', 'business_record', 'content']),
            missingTypes: [],
        }));
    });

    it('requires dependency verification to cover declared app Skill dependencies before publishing', async () => {
        const app = {
            id: 'publish-dependency-verification-app',
            name: 'Dependency Verification App',
            description: 'Checks publish evidence against declared Skill dependencies',
            category: 'CRM',
            kind: 'enterprise_normal_app',
            icon: 'customer',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_normal_app',
                launchMode: 'agent_dynamic_ui',
                appSkill: { id: 'declared-business-skill', version: '1.0.0', source: 'hub' },
                dependencies: { skills: [{ id: 'declared-business-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', install_ref: 'cap-hub-declared-business-skill' }] },
                datasrv: { domain: 'sales', objectRole: 'customer', preferredAction: 'sales.customer_upsert' },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'business_workspace',
                    layouts: { business_workspace: { template: 'classic_split', density: 'compact', primaryRegion: 'left', outputRegion: 'right', studio: { savedInManifest: true } } },
                },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'business_status', types: ['business_status', 'business_record', 'content'], delivery: { inlineContent: true, artifacts: false, businessRecord: true, notifications: false } },
            },
            installEvidence: {
                dependency_verification: {
                    schema: 'maclaw.app.install_plan.v1',
                    dependencies: [{ id: 'declared-business-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', install_ref: 'cap-hub-wrong-business-skill', installed: true, health: 'ready', action: 'skip', app_ids: ['publish-dependency-verification-app'] }],
                },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-dependency-covered',
            outputMode: 'business',
            resultPayload: { business_status: 'ready', business_record: { id: 'customer-1' }, text: 'ready' },
            outputs: [{ kind: 'business_record', title: 'Customer', text: '{"id":"customer-1"}', status: 'ready', data: { id: 'customer-1' } }],
            resultCoverage: { ok: true, primary: 'business_status', coveredTypes: ['business_status', 'business_record', 'content'], missingTypes: [] },
        });
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [],
            dependencies: [{ id: 'declared-business-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', install_ref: 'cap-hub-wrong-business-skill', installed: true, health: 'ready', action: 'skip', app_ids: ['publish-dependency-verification-app'] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_workflow_contract_issue: false,
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = await waitFor(() => {
            const candidate = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Dependency Verification App')) as HTMLElement | undefined;
            expect(candidate).toBeTruthy();
            expect(within(candidate as HTMLElement).getByText(/Dependency verification is missing declared Skill: declared-business-skill/)).not.toBeNull();
            return candidate as HTMLElement;
        });
        expect(within(card).getByText('Needs work')).not.toBeNull();
        await waitFor(() => expect((within(card).getByText('One-click publish') as HTMLButtonElement).disabled).toBe(true));

        cleanup();
        window.localStorage.clear();
        const appVerifiedByRun = { ...app, installEvidence: undefined };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [appVerifiedByRun], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(appVerifiedByRun, {
            runID: 'run-dependency-verified',
            outputMode: 'business',
            resultPayload: { business_status: 'ready', business_record: { id: 'customer-1' }, text: 'ready' },
            outputs: [{ kind: 'business_record', title: 'Customer', text: '{"id":"customer-1"}', status: 'ready', data: { id: 'customer-1' } }],
            resultCoverage: { ok: true, primary: 'business_status', coveredTypes: ['business_status', 'business_record', 'content'], missingTypes: [] },
            dependencyVerification: {
                schema: 'maclaw.app.install_plan.v1',
                verifiedAt: '2026-06-17T00:04:00.000Z',
                appCount: 1,
                dependencyCount: 1,
                hasMissingRequired: false,
                hasBlockingDependency: false,
                hasWorkflowContractIssue: false,
                workflowContractIssueCount: 0,
                hasGovernanceReviewIssue: false,
                governanceReviewIssueCount: 0,
                dependencies: [{ id: 'declared-business-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', install_ref: 'cap-hub-declared-business-skill', installed: true, health: 'ready', action: 'skip', app_ids: ['publish-dependency-verification-app'] }],
            },
        });
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [],
            dependencies: [{ id: 'declared-business-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', install_ref: 'cap-hub-declared-business-skill', installed: true, health: 'ready', action: 'skip', app_ids: ['publish-dependency-verification-app'] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_workflow_contract_issue: false,
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'market-dependency-verified', submitted_at: '2026-06-17T01:00:00.000Z', status: 'submitted', message: 'queued' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const readyCard = await waitFor(() => {
            const candidate = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Dependency Verification App')) as HTMLElement | undefined;
            expect(candidate).toBeTruthy();
            expect(within(candidate as HTMLElement).getByText('Ready to submit')).not.toBeNull();
            return candidate as HTMLElement;
        });
        const packagePreviewText = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(packagePreviewText).toContain('dependencyVerification');
        expect(packagePreviewText).toContain('cap-hub-declared-business-skill');
        expect(packagePreviewText).toContain('run-dependency-verified');
        const previewPayload = JSON.parse(packagePreviewText);
        const previewGovernance = previewPayload.apps[0].app.governance;
        expect(previewGovernance.testEvidence).toMatchObject({
            runId: 'run-dependency-verified',
            resultPayload: { business_status: 'ready', business_record: { id: 'customer-1' }, text: 'ready' },
            outputCount: 1,
            dependencyVerification: expect.objectContaining({
                schema: 'maclaw.app.install_plan.v1',
                dependencyCount: 1,
                dependencies: [expect.objectContaining({ id: 'declared-business-skill', install_ref: 'cap-hub-declared-business-skill', installed: true, health: 'ready' })],
            }),
        });
        await waitFor(() => expect(within(readyCard).getByText('One-click publish')).not.toBeNull());
        fireEvent.click(within(readyCard).getByText('One-click publish'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        const submittedGovernance = payload.apps[0].app.governance;
        expect(submittedGovernance.testEvidence).toEqual(previewGovernance.testEvidence);
        expect(submittedGovernance.dependencyVerification).toMatchObject({
            schema: 'maclaw.app.install_plan.v1',
            hasMissingRequired: false,
            hasBlockingDependency: false,
            dependencies: [expect.objectContaining({ id: 'declared-business-skill', install_ref: 'cap-hub-declared-business-skill', installed: true, health: 'ready' })],
        });
        expect(submittedGovernance.dependencies.skills[0]).toEqual(expect.objectContaining({ id: 'declared-business-skill', install_ref: 'cap-hub-declared-business-skill' }));
    });

    it('requires approval instance evidence before publishing approval apps', async () => {
        const app = {
            id: 'publish-approval-instance-evidence',
            name: 'Approval Instance Evidence App',
            description: 'Checks approval publish evidence against instance data',
            category: 'Finance',
            kind: 'enterprise_approval_app',
            icon: 'receipt',
            accent: '#2f5f98',
            pinned: false,
            source: 'local',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_approval_app',
                launchMode: 'agent_dynamic_ui',
                appSkill: { id: 'expense-approval-app', version: '1.0.0', source: 'hub' },
                datasrv: { domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report', preferredAction: 'finance.expense_upsert' },
                workflow: { schema: 'maclaw.app.workflow.v1', submitNode: 'expense_report.submit', approvalNode: 'expense_report.manager_approval', resultNode: 'expense_report.result_feedback', attentionNode: 'expense_report.attention_review', statusMapping: { pending: 'approval_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' } },
                mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'expense-workflow', workflowVersion: '1.0.0', objectRole: 'expense_report' }] },
                dependencies: { skills: [{ id: 'expense-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }] },
                ui: { schema: 'maclaw.app.ui.v1', entry: 'approval_workspace', layouts: { approval_workspace: { template: 'dashboard', density: 'compact', primaryRegion: 'left', outputRegion: 'right', studio: { savedInManifest: true } } } },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'approval_result', types: ['approval_result', 'business_status', 'content'], delivery: { inlineContent: true, artifacts: false, businessRecord: true, notifications: true } },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-approval-no-instance',
            outputMode: 'approval_result',
            resultPayload: { decision: 'approved', business_status: 'approved', text: 'approved' },
            outputs: [{ kind: 'approval_result', title: 'Approval decision', text: 'approved', status: 'approved' }],
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const blockedCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Approval Instance Evidence App')) as HTMLElement;
        expect(blockedCard).toBeTruthy();
        expect(within(blockedCard).getByText('Approval instance evidence')).not.toBeNull();
        expect(within(blockedCard).getByText('Needs work')).not.toBeNull();
        expect(within(blockedCard).getByText(/Missing instanceId, status, or approval instance view verification/)).not.toBeNull();
        await waitFor(() => expect((within(blockedCard).getByText('One-click publish') as HTMLButtonElement).disabled).toBe(true));

        cleanup();
        window.localStorage.clear();
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-approval-instance-without-result-package',
            outputMode: 'approval_result',
            resultPayload: { decision: 'approved', business_status: 'approved', text: 'approved' },
            outputs: [{ kind: 'approval_result', title: 'Approval decision', text: 'approved', status: 'approved' }],
            approvalInstance: {
                instanceId: 'appr-no-result-package',
                approvalID: 'approval-remote-no-result-package',
                status: 'approved',
                currentNode: 'expense_report.result_feedback',
                workflowSkillId: 'expense-workflow',
                workflowVersion: '1.0.0',
                businessStatus: 'approved',
                resultStatus: 'approved',
                approvalInstanceViewVerified: true,
                approvalViews: { handled: true, all: true },
            },
        });
        render(<AppsPage lang="en" />);
        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const incompleteCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Approval Instance Evidence App')) as HTMLElement;
        expect(incompleteCard).toBeTruthy();
        expect(within(incompleteCard).getByText('Needs work')).not.toBeNull();
        expect(within(incompleteCard).getByText(/missing resultPayload, outputs, or artifacts/)).not.toBeNull();
        await waitFor(() => expect((within(incompleteCard).getByText('One-click publish') as HTMLButtonElement).disabled).toBe(true));

        cleanup();
        window.localStorage.clear();
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-approval-with-instance',
            outputMode: 'approval_result',
            resultPayload: { decision: 'approved', business_status: 'approved', text: 'approved' },
            outputs: [{ kind: 'approval_result', title: 'Approval decision', text: 'approved', status: 'approved' }],
            approvalInstance: {
                instanceId: 'appr-publish-evidence-1',
                approvalID: 'approval-remote-publish-1',
                recordID: 'expense-1',
                datasetID: 'finance.expenses',
                objectRole: 'expense_report',
                approvalEvent: 'expense.submitted',
                approvalWorkflowID: 'expense-workflow',
                status: 'approved',
                currentNode: 'expense_report.result_feedback',
                workflowSkillId: 'expense-workflow',
                workflowVersion: '1.0.0',
                businessStatus: 'approved',
                resultStatus: 'approved',
                resultPayload: { decision: 'approved', business_status: 'approved', text: 'approved' },
                outputs: [{ kind: 'approval_result', title: 'Approval decision', text: 'approved', status: 'approved' }],
                approvalInstanceViewVerified: true,
                approvalViews: { my_requests: true, pending_my_approval: true, handled: true, all: true },
            },
        });

        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'market-approval-instance-evidence', submitted_at: '2026-06-17T01:00:00.000Z', status: 'submitted', message: 'queued' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };
        // The authoritative dependency check gates readiness; echo the declared
        // app + workflow skills as installed/ready for every plan call.
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [
                { id: 'expense-approval-app', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] },
                { id: 'expense-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] },
            ],
            has_missing_required: false,
            has_blocking_dependency: false,
        });
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const readyCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Approval Instance Evidence App')) as HTMLElement;
        expect(readyCard).toBeTruthy();
        await waitFor(() => expect(within(readyCard).getByText('Ready to submit')).not.toBeNull());
        expect(within(readyCard).getByText(/appr-publish-evidence-1 \/ approved \/ expense_report.result_feedback/)).not.toBeNull();
        await waitFor(() => expect((within(readyCard).getByText('One-click publish') as HTMLButtonElement).disabled).toBe(false));
        await waitFor(() => expect(within(readyCard).getByText('One-click publish')).not.toBeNull());
        fireEvent.click(within(readyCard).getByText('One-click publish'));
        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        expect(payload.apps[0].app.governance.testEvidence.approvalInstance).toMatchObject({
            instanceId: 'appr-publish-evidence-1',
            approvalID: 'approval-remote-publish-1',
            status: 'approved',
            currentNode: 'expense_report.result_feedback',
            workflowSkillId: 'expense-workflow',
            businessStatus: 'approved',
            resultStatus: 'approved',
            resultPayload: { decision: 'approved', business_status: 'approved', text: 'approved' },
            approvalInstanceViewVerified: true,
        });    });
    it('blocks approval app publish submission when workflow contract verification fails', async () => {
        const app = {
            id: 'publish-approval-contract-drift',
            name: 'Approval Contract Publish Drift',
            description: 'Approval publish contract verification',
            category: 'Finance',
            kind: 'enterprise_approval_app',
            icon: 'receipt',
            accent: '#2f5f98',
            pinned: false,
            source: 'local',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_approval_app',
                launchMode: 'agent_dynamic_ui',
                appSkill: { id: 'expense-approval-app', version: '1.0.0', source: 'hub' },
                datasrv: { domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report', preferredAction: 'finance.expense_upsert' },
                workflow: { schema: 'maclaw.app.workflow.v1', submitNode: 'expense_report.submit', approvalNode: 'expense_report.manager_approval', resultNode: 'expense_report.result_feedback', attentionNode: 'expense_report.attention_review', statusMapping: { pending: 'approval_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' } },
                mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'expense-workflow', workflowVersion: '1.0.0', objectRole: 'expense_report' }] },
                dependencies: { skills: [{ id: 'expense-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }] },
                ui: { schema: 'maclaw.app.ui.v1', entry: 'approval_workspace', layouts: { approval_workspace: { template: 'dashboard', density: 'compact', primaryRegion: 'left', outputRegion: 'right', studio: { savedInManifest: true } } } },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'approval_result', types: ['approval_result', 'business_status', 'content'], delivery: { inlineContent: true, artifacts: false, businessRecord: true, notifications: true } },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-approval-contract-drift',
            outputMode: 'approval_result',
            resultPayload: { decision: 'approved', business_status: 'approved', text: 'approved' },
            outputs: [{ kind: 'approval_result', title: 'Approval decision', text: 'approved', status: 'approved' }],
            approvalInstance: {
                instanceId: 'appr-contract-drift',
                approvalID: 'approval-contract-drift',
                status: 'approved',
                currentNode: 'expense_report.result_feedback',
                workflowSkillId: 'expense-workflow',
                workflowVersion: '1.0.0',
                businessStatus: 'approved',
                resultStatus: 'approved',
                resultPayload: { decision: 'approved', business_status: 'approved', text: 'approved' },
                outputs: [{ kind: 'approval_result', title: 'Approval decision', text: 'approved', status: 'approved' }],
                approvalInstanceViewVerified: true,
                approvalViews: { my_requests: true, all: true },
            },
        });
        // The background authoritative verification consumes the same mock.
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{ id: 'expense-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            workflow_contract_issues: [{ path: 'apps[0].app.governance.workflowContract.workflowSkillId', severity: 'error', message: 'approval workflow contract does not match approval binding' }],
            has_workflow_contract_issue: true,
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'should-not-submit' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Approval Contract Publish Drift')) as HTMLElement;
        expect(card).toBeTruthy();
        expect(within(card).getByText('Runtime contract')).not.toBeNull();
        // The authoritative plan surfaces the workflow contract issue on the
        // card itself and disables publishing before any submit attempt.
        await waitFor(() => expect(within(card).getByText(/Approval workflow contract verification failed/)).not.toBeNull());
        await waitFor(() => expect((within(card).getByText('One-click publish') as HTMLButtonElement).disabled).toBe(true));
        expect(submitMaclawAppPackage).not.toHaveBeenCalled();
    });
    it('includes enterprise visual UI metadata in market submission packages', async () => {
        const app = {
            id: 'publish-enterprise-ui-app',
            name: '客户续约工作',
            description: 'Publish enterprise UI metadata',
            category: 'CRM',
            kind: 'enterprise_normal_app',
            icon: 'customer',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_normal_app',
                launchMode: 'agent_dynamic_ui',
                appSkill: { id: 'customer-renewal-skill', version: '1.0.0', source: 'hub' },
                datasrv: { domain: 'sales', objectRole: 'customer', preferredAction: 'sales.customer_upsert' },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'business_workspace',
                    layouts: {
                        business_workspace: {
                            template: 'classic_split',
                            density: 'compact',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            navigation: ['customers', 'renewals'],
                            list: { columns: ['customer_name', 'status', 'updated_at'] },
                            studio: { savedInManifest: true },
                        },
                    },
                },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'business_status', types: ['business_status', 'business_record', 'content'], delivery: { inlineContent: true, artifacts: false, businessRecord: true, notifications: false } },
            },
            installEvidence: {
                dependency_verification: {
                    schema: 'maclaw.app.install_plan.v1',
                    dependencies: [{ id: 'customer-renewal-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['publish-enterprise-ui-app'] }],
                },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-enterprise-ui',
            outputMode: 'business',
            resultPayload: { business_status: 'renewal_ready', business_record: { id: 'customer-1', status: 'renewal_ready' }, text: 'renewal package ready' },
            outputs: [{ kind: 'business_record', title: 'Customer renewal', text: '{"id":"customer-1","status":"renewal_ready"}', status: 'ready', data: { id: 'customer-1', status: 'renewal_ready' } }],
            dependencyVerification: {
                schema: 'maclaw.app.install_plan.v1',
                verifiedAt: '2026-06-17T00:04:00.000Z',
                appCount: 1,
                dependencyCount: 1,
                hasMissingRequired: false,
                hasBlockingDependency: false,
                hasWorkflowContractIssue: false,
                workflowContractIssueCount: 0,
                hasGovernanceReviewIssue: false,
                governanceReviewIssueCount: 0,
                dependencies: [{ id: 'customer-renewal-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            },
        });
        // The background authoritative verification consumes the same mock.
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }, { id: 'other-blocked-app', name: 'Other blocked app', kind: 'tool_app' }],
            dependencies: [
                { id: 'customer-renewal-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] },
                { id: 'other-blocked-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub', installed: false, health: 'missing', action: 'blocked', app_ids: ['other-blocked-app'] },
            ],
            has_missing_required: false,
            has_blocking_dependency: true,
            has_governance_review_issue: false,
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'market-enterprise-ui', submitted_at: '2026-06-17T01:00:00.000Z', status: 'submitted', message: 'queued' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLElement);
        fireEvent.click(screen.getByRole('tab', { name: /\u5ba1\u6838\/\u53d1\u5e03/ }));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('客户续约工作')) as HTMLElement;
        expect(card).toBeTruthy();
        await waitFor(() => expect(within(card).getByText('customer-renewal-skill@1.0.0 (app_skill, ready)')).not.toBeNull());
        await waitFor(() => expect(within(card).getByText('\u4e00\u952e\u53d1\u5e03')).not.toBeNull());
        fireEvent.click(within(card).getByText('\u4e00\u952e\u53d1\u5e03'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        expect(payload.apps[0].app.ui.entry).toBe('business_workspace');
        expect(payload.apps[0].app.ui.layouts.business_workspace).toMatchObject({
            template: 'classic_split',
            density: 'compact',
            primaryRegion: 'left',
            outputRegion: 'right',
        });
        expect(payload.apps[0].app.ui.layouts.business_workspace.navigation).toEqual(['customers', 'renewals']);
        expect(payload.apps[0].app.ui.layouts.business_workspace.list.columns).toEqual(['customer_name', 'status', 'updated_at']);
        expect(payload.apps[0].app.binding.ui.layouts.business_workspace.navigation).toEqual(['customers', 'renewals']);
        const layout = payload.apps[0].app.governance.workspaceLayout;
        expect(layout.entry).toBe('business_workspace');
        expect(layout.navigation).toEqual(['customers', 'renewals']);
        expect(layout.list.columns).toEqual(['customer_name', 'status', 'updated_at']);
        expect(payload.apps[0].app.governance.resultContract.primary).toBe('business_status');
        expect(payload.apps[0].app.governance.dependencies.skills[0]).toEqual(expect.objectContaining({ id: 'customer-renewal-skill', installed: true, health: 'ready', action: 'skip' }));
        const dependencyVerification = payload.apps[0].app.governance.dependencyVerification;
        expect(dependencyVerification.schema).toBe('maclaw.app.install_plan.v1');
        expect(dependencyVerification.appCount).toBe(1);
        expect(dependencyVerification.dependencyCount).toBe(1);
        expect(dependencyVerification.hasMissingRequired).toBe(false);
        expect(dependencyVerification.hasBlockingDependency).toBe(false);
        expect(dependencyVerification.hasGovernanceReviewIssue).toBe(false);
        expect(dependencyVerification.governanceReviewIssueCount).toBe(0);
        expect(dependencyVerification.dependencies[0]).toEqual(expect.objectContaining({ id: 'customer-renewal-skill', installed: true, health: 'ready' }));
        expect(JSON.stringify(dependencyVerification)).not.toContain('other-blocked-skill');
        const evidence = payload.apps[0].app.governance.testEvidence;
        expect(evidence.outputCount).toBe(1);
        expect(evidence.resultPayload.business_record).toEqual({ id: 'customer-1', status: 'renewal_ready' });
        expect(evidence.primaryResult).toBe('renewal_ready');
        expect(evidence.outputs[0]).toEqual(expect.objectContaining({ kind: 'business_record', title: 'Customer renewal', status: 'ready' }));
        expect(evidence.dependencyVerification).toEqual(expect.objectContaining({
            schema: 'maclaw.app.install_plan.v1',
            dependencyCount: 1,
            hasMissingRequired: false,
            hasBlockingDependency: false,
            hasWorkflowContractIssue: false,
            hasGovernanceReviewIssue: false,
            governanceReviewIssueCount: 0,
        }));
        expect(evidence.dependencyVerification.dependencies[0]).toEqual(expect.objectContaining({ id: 'customer-renewal-skill', installed: true, health: 'ready' }));
    });

    it('keeps imported DataSrv dependency evidence when republishing app packages', async () => {
        const app = {
            id: 'datasrv-installed-republish-app',
            name: 'Installed Evidence Republish App',
            description: 'Republish imported DataSrv evidence',
            category: 'Finance',
            kind: 'tool_app',
            icon: 'sheet',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            importedRunEvidence: {
                runID: 'run-imported-republish',
                appID: 'datasrv-installed-republish-app',
                status: 'done',
                outputMode: 'content',
                inputSummary: 'Imported DataSrv test evidence',
                message: 'Imported DataSrv test evidence',
                outputs: [{ kind: 'content', title: 'Imported content', text: 'ready', status: 'ready' }],
                resultPayload: { text: 'ready' },
                dependencyVerification: {
                    schema: 'maclaw.app.install_plan.v1',
                    verifiedAt: '2026-06-21T10:58:00Z',
                    appCount: 1,
                    dependencyCount: 1,
                    hasMissingRequired: false,
                    hasBlockingDependency: false,
                    hasWorkflowContractIssue: false,
                    workflowContractIssueCount: 0,
                    hasGovernanceReviewIssue: false,
                    governanceReviewIssueCount: 0,
                    dependencies: [{ id: 'imported-tool-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['datasrv-installed-republish-app'] }],
                },
                at: '2026-06-21T11:00:00Z',
            },
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                appSkill: { id: 'imported-tool-skill', version: '1.0.0', source: 'hub' },
                skill: { id: 'imported-tool-skill', inputMode: 'form', outputModes: ['text'], fields: [] },
                dependencies: { skills: [{ id: 'imported-tool-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub' }] },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'tool_workspace',
                    layouts: {
                        tool_workspace: {
                            template: 'classic_split',
                            density: 'compact',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            regions: [{ id: 'input', role: 'input', placement: 'left' }, { id: 'output', role: 'output', placement: 'right' }],
                            studio: { savedInManifest: true },
                        },
                    },
                },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'content', types: ['content'], delivery: { inlineContent: true, artifacts: false, businessRecord: false, notifications: false } },
	                testProtocol: testProtocolWithFingerprint({ schema: 'maclaw.app.test_protocol.v1', sampleInput: { text: 'sample' }, expectedOutput: { content: 'ready' }, requiredRoles: [], requiredScopes: [], riskLevel: 'low' }),
	            },
	        };
	        (app.importedRunEvidence as any).definitionHash = testAppDefinitionFingerprint(app);
	        (app.importedRunEvidence as any).testProtocolFingerprint = testAppTestProtocolFingerprint(app);
	        (app.importedRunEvidence as any).workspaceLayoutFingerprint = testWorkspaceLayoutFingerprint(app);
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{ id: 'imported-tool-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'republish-imported-evidence', submitted_at: '2026-06-21T11:05:00Z', status: 'submitted' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Installed Evidence Republish App')) as HTMLElement;
        expect(card).toBeTruthy();
        await waitFor(() => expect(within(card).getByText('One-click publish')).not.toBeNull());
        fireEvent.click(within(card).getByText('One-click publish'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        const evidence = payload.apps[0].app.governance.testEvidence;
        expect(evidence.definitionHash).toBe(testAppDefinitionFingerprint(app));
        expect(evidence.dependencyVerification).toEqual(expect.objectContaining({
            schema: 'maclaw.app.install_plan.v1',
            verifiedAt: '2026-06-21T10:58:00Z',
            dependencyCount: 1,
            hasGovernanceReviewIssue: false,
            governanceReviewIssueCount: 0,
        }));
        expect(evidence.dependencyVerification.dependencies[0]).toEqual(expect.objectContaining({ id: 'imported-tool-skill', installed: true, health: 'ready' }));
    });

    it('uses install evidence test records when republishing cold-start app packages', async () => {
        const app = {
            id: 'install-evidence-only-republish-app',
            name: 'Install Evidence Only Republish App',
            description: 'Republish stored install test evidence',
            category: 'Finance',
            kind: 'tool_app',
            icon: 'sheet',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            installEvidence: {
                schema: 'maclaw.app.install_record.v1',
                package_sha: 'sha-install-evidence-only',
                source: 'enterprise_hub',
                installed_at: '2026-06-21T12:00:00Z',
                apps: [{ id: 'install-evidence-only-republish-app', name: 'Install Evidence Only Republish App', kind: 'tool_app' }],
                dependencies: [{ id: 'install-evidence-tool-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['install-evidence-only-republish-app'] }],
	                dependency_verification: {
	                    schema: 'maclaw.app.install_plan.v1',
	                    verifiedAt: '2026-06-21T11:58:00Z',
	                    appCount: 1,
	                    dependencyCount: 1,
	                    hasMissingRequired: false,
	                    hasBlockingDependency: false,
	                    hasWorkflowContractIssue: false,
	                    workflowContractIssueCount: 0,
	                    hasGovernanceReviewIssue: false,
	                    governanceReviewIssueCount: 0,
	                    install_trace: {
	                        schema: 'maclaw.app.dependency_install_trace.v1',
	                        dependency_count: 1,
	                        preflight_checked_count: 1,
	                        preflight_ready_count: 1,
	                        integrity_checked_count: 1,
	                        integrity_ready_count: 1,
	                        signature_available_count: 1,
	                        install_error_count: 0,
	                        ok: true,
	                    },
	                    dependencies: [{ id: 'install-evidence-tool-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['install-evidence-only-republish-app'] }],
	                },
                test_evidence: {
                    run_id: 'run-install-evidence-only',
                    verified_at: '2026-06-21T11:59:00Z',
                    primary_result: 'content',
                    result_payload: { content: 'ready' },
                    outputs: [{ type: 'content', title: 'Imported content', text: 'ready', status: 'ready' }],
                    result_coverage: { ok: true, primary: 'content', covered_types: ['content'], missing_types: [] },
                },
            },
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                appSkill: { id: 'install-evidence-tool-skill', version: '1.0.0', source: 'hub' },
                skill: { id: 'install-evidence-tool-skill', inputMode: 'form', outputModes: ['text'], fields: [] },
                dependencies: { skills: [{ id: 'install-evidence-tool-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub' }] },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'tool_workspace',
                    layouts: {
                        tool_workspace: {
                            template: 'classic_split',
                            density: 'compact',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            regions: [{ id: 'input', role: 'input', placement: 'left' }, { id: 'output', role: 'output', placement: 'right' }],
                            studio: { savedInManifest: true },
                        },
                    },
                },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'content', types: ['content'], delivery: { inlineContent: true, artifacts: false, businessRecord: false, notifications: false } },
	                testProtocol: testProtocolWithFingerprint({ schema: 'maclaw.app.test_protocol.v1', sampleInput: { text: 'sample' }, expectedOutput: { content: 'ready' }, requiredRoles: [], requiredScopes: [], riskLevel: 'low' }),
	            },
	        };
	        (app.installEvidence.test_evidence as any).definition_fingerprint = testAppDefinitionFingerprint(app);
	        (app.installEvidence.test_evidence as any).test_protocol_fingerprint = testAppTestProtocolFingerprint(app);
	        (app.installEvidence.test_evidence as any).workspace_layout_fingerprint = testWorkspaceLayoutFingerprint(app);
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        planMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{ id: 'install-evidence-tool-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'republish-install-evidence-only', submitted_at: '2026-06-21T12:05:00Z', status: 'submitted' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Install Evidence Only Republish App')) as HTMLElement;
        expect(card).toBeTruthy();
        await waitFor(() => expect((within(card).getByText('One-click publish') as HTMLButtonElement).disabled).toBe(false));
        await waitFor(() => expect(within(card).getByText('One-click publish')).not.toBeNull());
        fireEvent.click(within(card).getByText('One-click publish'));

	        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
	        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
	        const packagedApp = payload.apps[0].app;
	        const evidence = packagedApp.governance.testEvidence;
	        const governanceDependencyVerification = packagedApp.governance.dependencyVerification;
	        expect(evidence.runId).toBe('run-install-evidence-only');
	        expect(evidence.definitionHash).toBe(testAppDefinitionFingerprint(app));
	        expect(evidence.resultCoverage).toEqual(expect.objectContaining({ ok: true, primary: 'content' }));
	        expect(governanceDependencyVerification.installTrace).toEqual(expect.objectContaining({
	            schema: 'maclaw.app.dependency_install_trace.v1',
	            dependency_count: 1,
	            preflight_checked_count: 1,
	            integrity_ready_count: 1,
	            signature_available_count: 1,
	            install_error_count: 0,
	            ok: true,
	        }));
	        expect(evidence.dependencyVerification.dependencies[0]).toEqual(expect.objectContaining({ id: 'install-evidence-tool-skill', installed: true, health: 'ready' }));
	        expect(evidence.dependencyVerification.installTrace).toEqual(expect.objectContaining({
	            schema: 'maclaw.app.dependency_install_trace.v1',
	            dependency_count: 1,
	            preflight_checked_count: 1,
	            integrity_ready_count: 1,
	            signature_available_count: 1,
	            install_error_count: 0,
	            ok: true,
	        }));
	    });

    it('republishes cold-start approval apps from Hub install evidence', async () => {
        const app = {
            id: 'install-evidence-approval-republish-app',
            name: 'Install Evidence Approval Republish App',
            description: 'Republish approval app from restored Hub install evidence',
            category: 'Finance',
            kind: 'enterprise_approval_app',
            icon: 'receipt',
            accent: '#2f5f98',
            pinned: false,
            source: 'local',
            version: 1,
            installEvidence: {
                schema: 'maclaw.app.install_record.v1',
                package_sha: 'sha-install-evidence-approval',
                source: 'enterprise_hub',
                installed_at: '2026-06-28T03:00:00Z',
                apps: [{ id: 'install-evidence-approval-republish-app', name: 'Install Evidence Approval Republish App', kind: 'enterprise_approval_app' }],
                dependencies: [
                    { id: 'install-approval-super-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['install-evidence-approval-republish-app'] },
                    { id: 'install-approval-workflow', version: '2.0.0', kind: 'workflow_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['install-evidence-approval-republish-app'] },
                ],
	                dependency_verification: {
	                    schema: 'maclaw.app.install_plan.v1',
	                    verifiedAt: '2026-06-28T02:58:00Z',
	                    appCount: 1,
	                    dependencyCount: 2,
                    hasMissingRequired: false,
                    hasBlockingDependency: false,
                    hasWorkflowContractIssue: false,
                    workflowContractIssueCount: 0,
                    hasGovernanceReviewIssue: false,
	                    governanceReviewIssueCount: 0,
	                    install_trace: {
	                        schema: 'maclaw.app.dependency_install_trace.v1',
	                        dependency_count: 2,
	                        preflight_checked_count: 2,
	                        preflight_ready_count: 2,
	                        integrity_checked_count: 2,
	                        integrity_ready_count: 2,
	                        signature_available_count: 2,
	                        install_error_count: 0,
	                        ok: true,
	                    },
	                    dependencies: [
	                        { id: 'install-approval-super-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['install-evidence-approval-republish-app'] },
	                        { id: 'install-approval-workflow', version: '2.0.0', kind: 'workflow_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['install-evidence-approval-republish-app'] },
	                    ],
                },
                test_evidence: {
                    run_id: 'run-install-approval-evidence',
                    verified_at: '2026-06-28T02:59:00Z',
                    primary_result: 'approval_result',
                    result_payload: { approval_result: 'approved', business_status: 'finance_approved', business_record: { id: 'EXP-INSTALL-1' } },
                    outputs: [{ kind: 'approval_result', title: 'Decision', text: 'approved', status: 'approved' }],
                    artifacts: [{ id: 'install-approval-pdf', uri: 'artifact://install/approval.pdf', name: 'install-approval.pdf', status: 'ready' }],
                    result_coverage: { ok: true, primary: 'approval_result', covered_types: ['approval_result', 'business_status', 'business_record', 'document'], missing_types: [] },
                    approval_instance: {
                        instanceId: 'wf-install-approval-1',
                        approvalID: 'approval-install-remote-1',
                        recordID: 'EXP-INSTALL-1',
                        datasetID: 'finance.expenses',
                        objectRole: 'expense_report',
                        approvalEvent: 'finance.expense.submitted',
                        approvalWorkflowID: 'finance.expense.submitted',
                        status: 'approved',
                        currentNode: 'expense.result_feedback',
                        workflowSkillId: 'install-approval-workflow',
                        workflowVersion: '2.0.0',
                        businessStatus: 'finance_approved',
                        resultStatus: 'approved',
                        resultPayload: { approval_result: 'approved', business_status: 'finance_approved', business_record: { id: 'EXP-INSTALL-1' } },
                        outputs: [{ kind: 'approval_result', title: 'Decision', text: 'approved', status: 'approved' }],
                        artifacts: [{ id: 'install-approval-pdf', uri: 'artifact://install/approval.pdf', name: 'install-approval.pdf', status: 'ready' }],
                        approvalInstanceViewVerified: true,
                        approvalViews: { my_requests: true, pending_my_approval: true, handled: true, all: true },
                    },
	                    dependencyVerification: {
	                        schema: 'maclaw.app.install_plan.v1',
	                        verifiedAt: '2026-06-28T02:58:00Z',
	                        dependencyCount: 2,
	                        hasMissingRequired: false,
	                        hasBlockingDependency: false,
	                        installTrace: {
	                            schema: 'maclaw.app.dependency_install_trace.v1',
	                            dependencyCount: 2,
	                            preflightCheckedCount: 2,
	                            preflightReadyCount: 2,
	                            integrityCheckedCount: 2,
	                            integrityReadyCount: 2,
	                            signatureAvailableCount: 2,
	                            installErrorCount: 0,
	                            ok: true,
	                        },
	                        dependencies: [
	                            { id: 'install-approval-super-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['install-evidence-approval-republish-app'] },
	                            { id: 'install-approval-workflow', version: '2.0.0', kind: 'workflow_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['install-evidence-approval-republish-app'] },
	                        ],
                    },
                },
            },
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_approval_app',
                launchMode: 'agent_dynamic_ui',
                appSkill: { id: 'install-approval-super-skill', version: '1.0.0', source: 'hub' },
                dependencies: {
                    skills: [
                        { id: 'install-approval-super-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub' },
                        { id: 'install-approval-workflow', version: '2.0.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] },
                    ],
                },
	                datasrv: { domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report', blueprintID: 'finance.expense.approval', preferredAction: 'finance.expense_upsert', preferredView: 'finance.expense_review' },
                mis: { approvalBindings: [{ event: 'finance.expense.submitted', workflowSkillId: 'install-approval-workflow', workflowVersion: '2.0.0', objectRole: 'expense_report' }] },
                workflow: { schema: 'maclaw.app.workflow.v1', submitNode: 'expense.submit', approvalNode: 'expense.manager_review', resultNode: 'expense.result_feedback', attentionNode: 'expense.attention', statusMapping: { pending: 'approval_pending', approved: 'finance_approved', rejected: 'finance_rejected', attention: 'finance_attention', requiresInput: 'finance_more_input' } },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'approval_workspace',
                    layouts: {
                        approval_workspace: {
                            template: 'classic_split',
                            density: 'compact',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            navigation: ['my_requests', 'pending_my_approval', 'handled', 'attention'],
                            regions: [{ id: 'request', role: 'input', placement: 'left' }, { id: 'inbox', role: 'instance_list', placement: 'center' }, { id: 'detail', role: 'detail', placement: 'center' }, { id: 'result', role: 'output', placement: 'right' }],
                            studio: { savedInManifest: true },
                        },
                    },
                },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'approval_result', types: ['approval_result', 'business_status', 'business_record', 'document'], delivery: { inlineContent: true, artifacts: true, businessRecord: true, notifications: true } },
                testProtocol: testProtocolWithFingerprint({ schema: 'maclaw.app.test_protocol.v1', sampleInput: { record_ref: 'EXP-INSTALL-1' }, expectedOutput: { approval_result: 'approved' }, requiredRoles: ['applicant', 'approver'], requiredScopes: ['finance.expense_upsert'], riskLevel: 'medium' }),
            },
	        };
	        (app.installEvidence.test_evidence as any).definition_fingerprint = testAppDefinitionFingerprint(app);
	        (app.installEvidence.test_evidence as any).test_protocol_fingerprint = testAppTestProtocolFingerprint(app);
	        (app.installEvidence.test_evidence as any).workspace_layout_fingerprint = testWorkspaceLayoutFingerprint(app);
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: app.installEvidence.dependency_verification.dependencies,
            has_missing_required: false,
            has_blocking_dependency: false,
            has_workflow_contract_issue: false,
            has_governance_review_issue: false,
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'republish-install-approval-evidence', submitted_at: '2026-06-28T03:05:00Z', status: 'submitted' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Install Evidence Approval Republish App')) as HTMLElement;
        expect(card).toBeTruthy();
        await waitFor(() => expect(within(card).getByText('Ready to submit')).not.toBeNull());
        await waitFor(() => expect(within(card).getByText('One-click publish')).not.toBeNull());
        fireEvent.click(within(card).getByText('One-click publish'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
	        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
		        const packagedApp = payload.apps[0].app;
		        const evidence = packagedApp.governance.testEvidence;
		        const governanceDependencyVerification = packagedApp.governance.dependencyVerification;
		        expect(packagedApp.binding.datasrv).toMatchObject({
		            domain: 'finance',
		            datasetID: 'finance.expenses',
		            objectRole: 'expense_report',
		            blueprintID: 'finance.expense.approval',
		        });
		        expect(packagedApp.binding.mis.approvalBindings[0]).toMatchObject({
		            event: 'finance.expense.submitted',
		            workflowSkillId: 'install-approval-workflow',
		            workflowVersion: '2.0.0',
		            objectRole: 'expense_report',
		        });
	        expect(governanceDependencyVerification.installTrace).toEqual(expect.objectContaining({
	            schema: 'maclaw.app.dependency_install_trace.v1',
	            dependency_count: 2,
	            preflight_checked_count: 2,
	            integrity_ready_count: 2,
	            signature_available_count: 2,
	            install_error_count: 0,
	            ok: true,
	        }));
	        expect(evidence.runId).toBe('run-install-approval-evidence');
        expect(evidence.definitionHash).toBe(testAppDefinitionFingerprint(app));
        expect(evidence.approvalInstance).toMatchObject({
            instanceId: 'wf-install-approval-1',
            approvalID: 'approval-install-remote-1',
            workflowSkillId: 'install-approval-workflow',
            workflowVersion: '2.0.0',
            approvalEvent: 'finance.expense.submitted',
            businessStatus: 'finance_approved',
            resultStatus: 'approved',
            approvalInstanceViewVerified: true,
            resultPayload: expect.objectContaining({ approval_result: 'approved', business_status: 'finance_approved', business_record: { id: 'EXP-INSTALL-1' } }),
            outputs: expect.arrayContaining([expect.objectContaining({ title: 'Decision', text: 'approved' })]),
            artifacts: expect.arrayContaining([expect.objectContaining({ name: 'install-approval.pdf' })]),
        });
        expect(evidence.resultPayload).toEqual(expect.objectContaining({ approval_result: 'approved', business_status: 'finance_approved', business_record: { id: 'EXP-INSTALL-1' } }));
        expect(evidence.resultCoverage).toEqual(expect.objectContaining({ ok: true, primary: 'approval_result', missingTypes: [] }));
	        expect(evidence.dependencyVerification.dependencies).toEqual(expect.arrayContaining([
	            expect.objectContaining({ id: 'install-approval-super-skill', installed: true, health: 'ready' }),
	            expect.objectContaining({ id: 'install-approval-workflow', installed: true, health: 'ready' }),
	        ]));
	        expect(evidence.dependencyVerification.installTrace).toEqual(expect.objectContaining({
	            schema: 'maclaw.app.dependency_install_trace.v1',
	            dependency_count: 2,
	            preflight_checked_count: 2,
	            integrity_ready_count: 2,
	            signature_available_count: 2,
	            install_error_count: 0,
	            ok: true,
	        }));
	    });

    it('treats imported run evidence as stale after dynamic UI layout changes', async () => {
        const app = {
            id: 'datasrv-installed-stale-layout-app',
            name: 'Installed Stale Layout App',
            description: 'Imported evidence should expire when UI layout changes',
            category: 'Finance',
            kind: 'tool_app',
            icon: 'sheet',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            importedRunEvidence: {
                runID: 'run-imported-layout',
                appID: 'datasrv-installed-stale-layout-app',
                status: 'done',
                outputMode: 'content',
                inputSummary: 'Imported DataSrv test evidence',
                message: 'Imported DataSrv test evidence',
                outputs: [{ kind: 'content', title: 'Imported content', text: 'ready', status: 'ready' }],
                resultPayload: { text: 'ready' },
                at: '2026-06-21T11:00:00Z',
            },
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                appSkill: { id: 'imported-tool-skill', version: '1.0.0', source: 'hub' },
                skill: { id: 'imported-tool-skill', inputMode: 'form', outputModes: ['text'], fields: [] },
                dependencies: { skills: [{ id: 'imported-tool-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub' }] },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'tool_workspace',
                    layouts: {
                        tool_workspace: {
                            template: 'classic_split',
                            density: 'compact',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            regions: [{ id: 'input', role: 'input', placement: 'left' }, { id: 'output', role: 'output', placement: 'right' }],
                            studio: { savedInManifest: true },
                        },
                    },
                },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'content', types: ['content'], delivery: { inlineContent: true, artifacts: false, businessRecord: false, notifications: false } },
                testProtocol: { schema: 'maclaw.app.test_protocol.v1', requiredRuns: 1, cases: [{ id: 'smoke', name: 'Smoke', required: true, expectedOutputs: ['content'] }] },
            },
        };
        (app.importedRunEvidence as any).definitionHash = testAppDefinitionFingerprint(app);
        (app.manifest.ui.layouts.tool_workspace as any).density = 'comfortable';
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{ id: 'imported-tool-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        });
        // The backend package review owns staleness decisions now; a clean
        // review would forgive the outdated browser-side evidence.
        reviewMaclawAppPackageMock.mockResolvedValue({ review_issues: [{ path: 'apps[0].app.governance.testEvidence', severity: 'error', message: 'Definition changed; run evidence is stale — rerun the app to refresh it' }] });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'stale-layout-evidence', submitted_at: '2026-06-21T11:05:00Z', status: 'submitted' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Installed Stale Layout App')) as HTMLElement;
        expect(card).toBeTruthy();
        await waitFor(() => expect(within(card).getByText('One-click publish')).not.toBeNull());
        await waitFor(() => expect(within(card).getByText('Definition changed; run evidence is stale — rerun the app to refresh it')).not.toBeNull());
        await waitFor(() => expect((within(card).getByText('One-click publish') as HTMLButtonElement).disabled).toBe(true));
        expect(submitMaclawAppPackage).not.toHaveBeenCalled();
    });

    it.each([
        ['result contract', (app: any) => {
            app.manifest.resultContract.types = ['artifact'];
            app.manifest.resultContract.primary = 'artifact';
            app.manifest.resultContract.delivery = { inlineContent: false, artifacts: true, businessRecord: false, notifications: false };
        }],
        ['test protocol', (app: any) => {
            app.manifest.testProtocol = testProtocolWithFingerprint({
                schema: 'maclaw.app.test_protocol.v1',
                sampleInput: { query: 'updated sample' },
                expectedOutput: { status: 'ok', primary: 'content', reviewed: true },
                requiredRoles: ['operator'],
                requiredScopes: ['app.run'],
                riskLevel: 'medium',
            });
        }],
    ])('treats imported run evidence as stale after %s changes', async (_label, mutate) => {
        const slug = String(_label).replace(/\s+/g, '-');
        const title = String(_label).replace(/\b\w/g, (char) => char.toUpperCase());
        const app: any = {
            id: `datasrv-installed-stale-${slug}-app`,
            name: `Installed Stale ${title} App`,
            description: 'Imported evidence should expire when the definition contract changes',
            category: 'Finance',
            kind: 'tool_app',
            icon: 'sheet',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            importedRunEvidence: {
                runID: `run-imported-${slug}`,
                appID: `datasrv-installed-stale-${slug}-app`,
                status: 'done',
                outputMode: 'content',
                inputSummary: 'Imported DataSrv test evidence',
                message: 'Imported DataSrv test evidence',
                outputs: [{ kind: 'content', title: 'Imported content', text: 'ready', status: 'ready' }],
                resultPayload: { content: 'ready', text: 'ready' },
                at: '2026-06-21T11:00:00Z',
            },
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                appSkill: { id: 'imported-contract-skill', version: '1.0.0', source: 'hub' },
                skill: { id: 'imported-contract-skill', inputMode: 'form', outputModes: ['text'], fields: [] },
                dependencies: { skills: [{ id: 'imported-contract-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub' }] },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'tool_workspace',
                    layouts: {
                        tool_workspace: {
                            template: 'classic_split',
                            density: 'compact',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            regions: [{ id: 'input', role: 'input', placement: 'left' }, { id: 'output', role: 'output', placement: 'right' }],
                            studio: { savedInManifest: true },
                        },
                    },
                },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'content', types: ['content'], delivery: { inlineContent: true, artifacts: false, businessRecord: false, notifications: false } },
                testProtocol: testProtocolWithFingerprint({
                    schema: 'maclaw.app.test_protocol.v1',
                    sampleInput: { query: 'sample' },
                    expectedOutput: { status: 'ok', primary: 'content' },
                    requiredRoles: [],
                    requiredScopes: ['app.run'],
                    riskLevel: 'low',
                }),
            },
        };
        app.importedRunEvidence.definitionHash = testAppDefinitionFingerprint(app);
        mutate(app);
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{ id: 'imported-contract-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        });
        // The backend package review owns staleness decisions now; a clean
        // review would forgive the outdated browser-side evidence.
        reviewMaclawAppPackageMock.mockResolvedValue({ review_issues: [{ path: 'apps[0].app.governance.testEvidence', severity: 'error', message: 'Definition changed; run evidence is stale — rerun the app to refresh it' }] });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'stale-contract-evidence', submitted_at: '2026-06-21T11:05:00Z', status: 'submitted' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes(app.name)) as HTMLElement;
        expect(card).toBeTruthy();
        await waitFor(() => expect(within(card).getByText('Definition changed; run evidence is stale — rerun the app to refresh it')).not.toBeNull());
        await waitFor(() => expect((within(card).getByText('One-click publish') as HTMLButtonElement).disabled).toBe(true));
        expect(submitMaclawAppPackage).not.toHaveBeenCalled();
    });

    it('keeps local channel when the app package bridge queues locally', async () => {
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({
            submission_id: 'local-review-queued',
            submitted_at: '2026-06-17T01:05:00.000Z',
            status: 'submitted',
            channel: 'local',
            message: 'queued locally for enterprise market sync',
        });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        window.localStorage.clear();
        render(<AppsPage lang="zh-Hans" />);

        await createAndRunLocalToolApp('合同归档');
        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        await waitFor(() => expect(screen.getByText('一键发布')).not.toBeNull());
        fireEvent.click(screen.getByText('一键发布'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        expect(screen.getAllByText('本地待同步').length).toBeGreaterThan(0);
        expect(screen.getAllByText(/queued locally for enterprise market sync/).length).toBeGreaterThan(0);
        expect(screen.getByText(/"channel": "local"/)).not.toBeNull();
    });

    it('withdraws local app package submissions from the durable queue', async () => {
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({
            submission_id: 'local-review-withdraw',
            submitted_at: '2026-06-17T01:05:00.000Z',
            status: 'submitted',
            channel: 'local',
            message: 'queued locally for enterprise market sync',
        });
        const withdrawMaclawAppPackageSubmission = vi.fn().mockResolvedValue(true);
        const listMaclawAppPackageSubmissions = vi.fn()
            .mockResolvedValueOnce([])
            .mockResolvedValueOnce([{ submission_id: 'local-review-withdraw', submitted_at: '2026-06-17T01:05:00.000Z', status: 'submitted', channel: 'local', app_ids: ['withdraw-app'], message: 'queued locally for enterprise market sync' }])
            .mockResolvedValueOnce([]);
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage, WithdrawMaclawAppPackageSubmission: withdrawMaclawAppPackageSubmission, ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };

        window.localStorage.clear();
        render(<AppsPage lang="zh-Hans" />);

        await createAndRunLocalToolApp('合同归档');
        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        await waitFor(() => expect(screen.getByText('一键发布')).not.toBeNull());
        fireEvent.click(screen.getByText('一键发布'));

        await waitFor(() => expect(screen.getAllByText('本地待同步').length).toBeGreaterThan(0));
        fireEvent.click(screen.getByText('撤回提交'));

        await waitFor(() => expect(withdrawMaclawAppPackageSubmission).toHaveBeenCalledWith('local-review-withdraw'));
        expect(screen.getByText('\u53ef\u63d0\u4ea4')).not.toBeNull();
        await waitFor(() => expect(screen.getByText('暂无本机待同步提交')).not.toBeNull());
    });

    it('shows local app package submission queue summaries', async () => {
        const listMaclawAppPackageSubmissions = vi.fn().mockResolvedValue([{
            submission_id: 'local-review-existing',
            submitted_at: '2026-06-17T01:10:00Z',
            status: 'submitted',
            channel: 'local',
            app_ids: ['queued-app'],
            app_names: ['队列应用'],
            package_sha: '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
            package_bytes: 1536,
            dependencies: [{ id: 'queued-app-skill', required: true, installed: true }, { id: 'queued-workflow', kind: 'workflow_skill', required: true, installed: false }],
            submission_evidence: {
                'queued-app': {
                    workspace_layout: { entry: 'enterprise_workspace', template: 'approval_console', density: 'compact' },
                    result_contract: { primary: 'approval_result', types: ['approval_result', 'artifact', 'content'] },
                    test_evidence: { runId: 'run-queued', testProtocolFingerprint: 'proto-queued' },
                    dependencies: [{ id: 'queued-app-skill', required: true, installed: true }, { id: 'queued-workflow', kind: 'workflow_skill', required: true, installed: false }],
                },
            },
            review_evidence: {
                'queued-app': {
                    has_result_contract: true,
                    result_contract_primary: 'approval_result',
                    result_contract_type_count: 3,
                    has_test_protocol: true,
                    test_protocol_fingerprint: 'proto-queued',
                    result_coverage_ok: true,
                    result_coverage_primary: 'approval_result',
                    result_coverage_covered_count: 3,
                    result_coverage_missing_count: 0,
                    output_count: 2,
                    artifact_count: 1,
                },
            },
            event_count: 2,
            last_event_at: '2026-06-17T01:12:00Z',
            message: 'queued locally for enterprise market sync',
        }]);
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());

        await waitFor(() => expect(listMaclawAppPackageSubmissions).toHaveBeenCalledWith(8));
        expect(screen.getByText('本机提交队列')).not.toBeNull();
        expect(screen.getByText('local-review-existing')).not.toBeNull();
        expect(screen.getByText(/队列应用/)).not.toBeNull();
        expect(screen.getByText(/sha256:0123456789ab/)).not.toBeNull();
        expect(screen.getByText(/1.5 KB/)).not.toBeNull();
        expect(screen.getByText(/:2 .*:1/)).not.toBeNull();
        expect(screen.getByText(/事件:2 2026-06-17T01:12:00Z/)).not.toBeNull();
        expect(screen.getByText(/queued locally for enterprise market sync/)).not.toBeNull();
        const evidenceSnapshot = document.querySelector('.apps-install-evidence-snapshot') as HTMLElement;
        expect(evidenceSnapshot).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('界面布局')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('enterprise_workspace · approval_console · compact')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('结果契约')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('approval_result · 3 types')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('run-queued · proto-queued')).not.toBeNull();
        expect(screen.getByText('结果覆盖')).not.toBeNull();
        expect(screen.getByText('approval_result · 已覆盖: 3')).not.toBeNull();
        expect(screen.getByText('输出结果: 2 · 输出产物: 1')).not.toBeNull();
    });

    it('shows a loading state while reading the local submission queue', async () => {
        let resolveQueue: (value: unknown[]) => void = () => {};
        const queuePromise = new Promise<unknown[]>((resolve) => {
            resolveQueue = resolve;
        });
        const listMaclawAppPackageSubmissions = vi.fn().mockReturnValue(queuePromise);
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());

        expect(screen.getByText('提交队列读取中')).not.toBeNull();

        resolveQueue([]);
        await waitFor(() => expect(screen.getByText('暂无本机待同步提交')).not.toBeNull());
    });

    it('installs an approved Hub app directly from the publish queue', async () => {
        const listMaclawAppPackageSubmissions = vi.fn().mockResolvedValue([{
            submission_id: 'hub-review-approved',
            hub_capability_id: 'cap-approved-app',
            submitted_at: '2026-06-17T01:15:00Z',
            status: 'approved',
            channel: 'hub',
            app_ids: ['approved-app'],
            app_names: ['Approved Queue App'],
            message: 'approved by enterprise market',
        }]);
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };
        installMaclawAppPackageFromHubMock.mockResolvedValueOnce({
            schema: 'maclaw.app.hub_install.v1',
            capability_id: 'cap-approved-app',
            app_count: 1,
            package: {
                schema: 'maclaw.app.pack.v1',
                privateMarker: 'x_maclaw_apps',
                apps: [{
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'enterprise_app_pack',
                    app: {
                        id: 'approved-app',
                        name: 'Approved Queue App',
                        description: 'Installed from approved review queue',
                        category: 'enterprise',
                        kind: 'enterprise_normal_app',
                        icon: 'dashboard',
                        accent: '#4b6572',
                        launchMode: 'agent_dynamic_ui',
                        binding: {
                            datasrv: {
                                domain: 'operations',
                                datasetID: 'operations.approved_apps',
                                objectRole: 'approved_app',
                                preferredAction: 'operations.approved_app_open',
                            },
                            dependencies: {
                                skills: [{ id: 'approved-app-skill', kind: 'app_skill', source: 'hub', required: true }],
                            },
                        },
                    },
                }],
            },
            install_plan: {
                schema: 'maclaw.app.install_plan.v1',
                apps: [{ id: 'approved-app', name: 'Approved Queue App', kind: 'enterprise_normal_app' }],
                dependencies: [{ id: 'approved-app-skill', kind: 'app_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed' }],
                has_missing_required: false,
                has_blocking_dependency: false,
            },
            install_record: {
                schema: 'maclaw.app.install_record.v1',
                app_count: 1,
                apps: [{ id: 'approved-app', name: 'Approved Queue App', kind: 'enterprise_normal_app' }],
                dependencies: [{ id: 'approved-app-skill', kind: 'app_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed' }],
                datasrv_registration: {
                    synced: true,
                    eligible_count: 1,
                    synced_count: 1,
                    failed_count: 0,
                    items: [{ app_id: 'approved-app', synced: true, role_binding_count: 2 }],
                },
                app_versions: {
                    'approved-app': {
                        app_entry_version: '7',
                        app_skill: { id: 'approved-app-skill', version: '2.0.0', kind: 'app_skill', source: 'hub' },
                    },
                },
                install_evidence: {
                    'approved-app': {
                        workspace_layout: { entry: 'business_workspace', template: 'dashboard', density: 'compact' },
                        result_contract: { primary: 'business_record', types: ['business_record', 'content'] },
                        test_evidence: { runId: 'run-approved-install', testProtocolFingerprint: 'proto-approved-install' },
                        dependencies: [{ id: 'approved-app-skill', kind: 'app_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed' }],
                    },
                },
            },
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));

        await waitFor(() => expect(screen.getByText('hub-review-approved')).not.toBeNull());
        fireEvent.click(screen.getByText('Install approved app'));

        await waitFor(() => expect(installMaclawAppPackageFromHubMock).toHaveBeenCalledWith('cap-approved-app'));
        await waitFor(() => expect(screen.getAllByText('Installed').length).toBeGreaterThan(0));
        const approvedRow = screen.getByText('hub-review-approved').closest('.apps-publish-queue__row') as HTMLElement;
        expect(approvedRow).not.toBeNull();
        const dependencyVerification = approvedRow.querySelector('.apps-dependency-verification') as HTMLElement;
        expect(dependencyVerification).not.toBeNull();
        expect(within(dependencyVerification).getByText('Dependency verification')).not.toBeNull();
        expect(within(approvedRow).getByText('approved-app-skill')).not.toBeNull();
        expect(within(approvedRow).getByText('v7')).not.toBeNull();
        expect(within(approvedRow).getByText('Workspace layout')).not.toBeNull();
        expect(within(approvedRow).getByText('business_workspace · dashboard · compact')).not.toBeNull();
        expect(within(approvedRow).getByText('Test evidence')).not.toBeNull();
        expect(within(approvedRow).getByText('run-approved-install · proto-approved-install')).not.toBeNull();
        expect(within(approvedRow).getByText('DataSrv')).not.toBeNull();
        expect(within(approvedRow).getByText('DataSrv bindings registered: 1/1')).not.toBeNull();
        fireEvent.click(getManageTab());
        await waitFor(() => expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('Approved Queue App'))).toBe(true));
    });
    it('installs an approved Hub approval app with runtime install evidence', async () => {
        const listMaclawAppPackageSubmissions = vi.fn().mockResolvedValue([{
            submission_id: 'hub-review-approved-approval',
            hub_capability_id: 'cap-approved-approval-app',
            submitted_at: '2026-06-30T13:00:00Z',
            status: 'approved',
            channel: 'hub',
            app_ids: ['approved-approval-app'],
            app_names: ['Approved Approval Queue App'],
            message: 'approved by enterprise market',
        }]);
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };
        const dependencies = [
            { id: 'approved-approval-super-skill', version: '3.0.0', kind: 'app_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed', app_ids: ['approved-approval-app'] },
            { id: 'approved-approval-workflow', version: '4.0.0', kind: 'workflow_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed', install_ref: 'cap-hub-approved-approval-workflow', app_ids: ['approved-approval-app'] },
        ];
        installMaclawAppPackageFromHubMock.mockResolvedValueOnce({
            schema: 'maclaw.app.hub_install.v1',
            capability_id: 'cap-approved-approval-app',
            app_count: 1,
            package: {
                schema: 'maclaw.app.pack.v1',
                privateMarker: 'x_maclaw_apps',
                apps: [{
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'enterprise_app_pack',
                    app: {
                        id: 'approved-approval-app',
                        name: 'Approved Approval Queue App',
                        description: 'Installed approved approval app from Hub review queue',
                        category: 'Finance',
                        kind: 'enterprise_approval_app',
                        icon: 'receipt',
                        accent: '#2f5f98',
                        version: 7,
                        launchMode: 'agent_dynamic_ui',
                        binding: {
                            appSkill: { id: 'approved-approval-super-skill', version: '3.0.0', source: 'hub' },
                            datasrv: { domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report', preferredAction: 'finance.expense_upsert' },
                            dependencies: { skills: dependencies.map((dep) => ({ id: dep.id, version: dep.version, kind: dep.kind, source: dep.source, required: dep.required, install_ref: dep.install_ref })) },
                            mis: { approvalBindings: [{ event: 'finance.expense.submitted', workflowSkillId: 'approved-approval-workflow', workflowVersion: '4.0.0', objectRole: 'expense_report' }] },
                            workflow: { schema: 'maclaw.app.workflow.v1', submitNode: 'expense.submit', approvalNode: 'finance.review', resultNode: 'expense.result_feedback', statusMapping: { pending: 'approval_pending', approved: 'finance_approved', rejected: 'finance_rejected', attention: 'finance_attention', requiresInput: 'requires_input' } },
                            ui: { schema: 'maclaw.app.ui.v1', entry: 'approval_workspace', layouts: { approval_workspace: { template: 'left_nav', density: 'compact', primaryRegion: 'right', outputRegion: 'bottom', regions: [{ id: 'request_form', role: 'input', placement: 'right' }, { id: 'approval_inbox', role: 'instance_list', placement: 'left' }, { id: 'approval_detail', role: 'detail', placement: 'center' }, { id: 'result_panel', role: 'output', placement: 'bottom' }], studio: { savedInManifest: true } } } },
                            resultContract: { schema: 'maclaw.app.result.v1', primary: 'approval_result', types: ['approval_result', 'business_status', 'business_record', 'document'], delivery: { inlineContent: true, artifacts: true, businessRecord: true, notifications: true } },
                            testProtocol: { schema: 'maclaw.app.test_protocol.v1', fingerprint: 'proto-approved-approval', sampleInput: { record_ref: 'EXP-HUB-APPROVED-1' }, expectedOutput: { approval_result: 'approved' }, requiredRoles: ['applicant', 'approver'], requiredScopes: ['app.run'], riskLevel: 'medium' },
                        },
                    },
                }],
            },
            install_plan: {
                schema: 'maclaw.app.install_plan.v1',
                apps: [{ id: 'approved-approval-app', name: 'Approved Approval Queue App', kind: 'enterprise_approval_app' }],
                dependencies,
                has_missing_required: false,
                has_blocking_dependency: false,
            },
            install_record: {
                schema: 'maclaw.app.install_record.v1',
                hub_package_signature: { algorithm: 'ed25519', public_key_fingerprint: 'sha256:approved-approval-key', signed_by: 'enterprise-market', signed_at: '2026-06-30T13:03:00Z' },
                hub_package_signature_algorithm: 'ed25519',
                hub_package_signature_fingerprint: 'sha256:approved-approval-key',
                hub_package_signature_signed_at: '2026-06-30T13:03:00Z',
                hub_package_signature_signed_by: 'enterprise-market',
                app_count: 1,
                apps: [{ id: 'approved-approval-app', name: 'Approved Approval Queue App', kind: 'enterprise_approval_app' }],
                dependencies,
                datasrv_registration: {
                    synced: true,
                    eligible_count: 1,
                    synced_count: 1,
                    failed_count: 0,
                    items: [{ app_id: 'approved-approval-app', synced: true, role_binding_count: 1 }],
                },
                app_versions: {
                    'approved-approval-app': {
                        app_entry_version: '7',
                        app_skill: { id: 'approved-approval-super-skill', version: '3.0.0', kind: 'app_skill', source: 'hub' },
                        workflow_skills: [{ id: 'approved-approval-workflow', version: '4.0.0', kind: 'workflow_skill', source: 'hub' }],
                        approval_bindings: [{ event: 'finance.expense.submitted', object_role: 'expense_report', workflow_skill_id: 'approved-approval-workflow', workflow_version: '4.0.0' }],
                    },
                },
                install_evidence: {
                    'approved-approval-app': {
                        hub_package_signature: { algorithm: 'ed25519', public_key_fingerprint: 'sha256:approved-approval-key', signed_by: 'enterprise-market', signed_at: '2026-06-30T13:03:00Z' },
                        hub_package_signature_algorithm: 'ed25519',
                        hub_package_signature_fingerprint: 'sha256:approved-approval-key',
                        hub_package_signature_signed_at: '2026-06-30T13:03:00Z',
                        hub_package_signature_signed_by: 'enterprise-market',
                        submission: { status: 'approved', capability_id: 'cap-approved-approval-app', version_key: 'enterprise_hub:skill:maclaw-app:approved-approval-app@7', submission_id: 'hub-review-approved-approval', package_signature: { algorithm: 'ed25519', public_key_fingerprint: 'sha256:approved-approval-key' } },
                        review_evidence: { status: 'approved', approval_status: 'approved', reviewer: 'enterprise-market' },
                        workspace_layout: { entry: 'approval_workspace', template: 'left_nav', density: 'compact', primaryRegion: 'right', outputRegion: 'bottom' },
                        result_contract: { primary: 'approval_result', types: ['approval_result', 'business_status', 'business_record', 'document'] },
                        workflow_contract: { schema: 'maclaw.app.workflow_contract.v1', workflowSkillId: 'approved-approval-workflow', workflowVersion: '4.0.0', objectRole: 'expense_report', requiredInputs: ['record_ref', 'applicant', 'business_payload'], decisionOutputs: ['approved', 'rejected', 'attention'], requiredOutputs: ['workflow_result', 'approval_instance', 'outputs', 'artifacts'] },
                        test_evidence: {
                            run_id: 'run-approved-approval-install',
                            verified_at: '2026-06-30T13:02:00Z',
                            definition_fingerprint: 'sha256:approved-approval-app',
                            test_protocol_fingerprint: 'proto-approved-approval',
                            primary_result: 'approval_result',
                            result_payload: { approval_result: 'approved', business_status: 'finance_approved', business_record: { id: 'EXP-HUB-APPROVED-1', status: 'finance_approved' } },
                            outputs: [{ id: 'approved-output', kind: 'business_record', title: 'Approved expense', text: 'EXP-HUB-APPROVED-1', status: 'ready', data: { id: 'EXP-HUB-APPROVED-1', status: 'finance_approved' } }],
                            artifacts: [{ id: 'approved-pdf', uri: 'artifact://approved/approval.pdf', name: 'approved-approval.pdf', status: 'ready' }],
                            result_coverage: { ok: true, primary: 'approval_result', covered_types: ['approval_result', 'business_status', 'business_record', 'document'], missing_types: [] },
                            approval_instance: {
                                workflow_instance_id: 'wf-approved-approval-1',
                                approval_id: 'approval-approved-approval-1',
                                record_id: 'EXP-HUB-APPROVED-1',
                                status: 'approved',
                                current_node: 'expense.result_feedback',
                                workflow_skill_id: 'approved-approval-workflow',
                                workflow_version: '4.0.0',
                                approval_event: 'finance.expense.submitted',
                                approval_object_role: 'expense_report',
                                business_status: 'finance_approved',
                                result_status: 'approved',
                                result_payload: { approval_result: 'approved', business_status: 'finance_approved', business_record: { id: 'EXP-HUB-APPROVED-1', status: 'finance_approved' } },
                                outputs: [{ id: 'approved-output', kind: 'business_record', title: 'Approved expense', text: 'EXP-HUB-APPROVED-1', status: 'ready' }],
                                artifacts: [{ id: 'approved-pdf', uri: 'artifact://approved/approval.pdf', name: 'approved-approval.pdf', status: 'ready' }],
                                approval_instance_view_verified: true,
                            },
                        },
                        dependency_verification: { schema: 'maclaw.app.install_plan.v1', dependency_count: 2, has_missing_required: false, has_blocking_dependency: false, dependencies },
                        dependencies,
                    },
                },
            },
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        await waitFor(() => expect(screen.getByText('hub-review-approved-approval')).not.toBeNull());
        fireEvent.click(screen.getByText('Install approved app'));

        await waitFor(() => expect(installMaclawAppPackageFromHubMock).toHaveBeenCalledWith('cap-approved-approval-app'));
        await waitFor(() => expect(screen.getAllByText('Installed').length).toBeGreaterThan(0));
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            const installed = (stored.customApps || []).find((item: any) => item.id === 'market-approved-approval-app');
            expect(installed?.versionSnapshot?.app_entry_version).toBe('7');
            expect(installed?.versionSnapshot?.workflow_skills?.[0]?.id).toBe('approved-approval-workflow');
            expect(installed?.installEvidence?.datasrv_registration?.synced).toBe(true);
            expect(installed?.installEvidence?.submission).toMatchObject({ status: 'approved', capability_id: 'cap-approved-approval-app', version_key: 'enterprise_hub:skill:maclaw-app:approved-approval-app@7' });
            expect(installed?.installEvidence?.hub_package_signature_fingerprint).toBe('sha256:approved-approval-key');
            expect(installed?.installEvidence?.hub_package_signature_signed_by).toBe('enterprise-market');
            expect(installed?.installEvidence?.review_evidence).toMatchObject({ approval_status: 'approved', reviewer: 'enterprise-market' });
            expect(installed?.installEvidence?.dependency_verification?.dependencies).toHaveLength(2);
            expect(installed?.workflowContract?.workflowSkillId).toBe('approved-approval-workflow');
            expect(installed?.importedRunEvidence?.approvalInstance?.approvalID).toBe('approval-approved-approval-1');
            expect(installed?.importedRunEvidence?.approvalInstance?.resultPayload?.business_record?.id).toBe('EXP-HUB-APPROVED-1');
            expect(installed?.importedRunEvidence?.outputs?.[0]?.title).toBe('Approved expense');
            expect(installed?.importedRunEvidence?.artifacts?.[0]?.name).toBe('approved-approval.pdf');
        });

        fireEvent.click(screen.getAllByText('Close')[0]);
        fireEvent.click(screen.getAllByText('Approved Approval Queue App')[0]);
        const runtimeVersionSnapshot = await waitFor(() => {
            const snapshot = document.querySelector('.apps-detail__header .apps-install-version-snapshot') as HTMLElement | null;
            expect(snapshot).not.toBeNull();
            return snapshot as HTMLElement;
        });
        expect(within(runtimeVersionSnapshot).getByText('approved-approval-super-skill · app_skill · hub · v3.0.0')).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText('approved-approval-workflow · workflow_skill · hub · v4.0.0')).not.toBeNull();
        const runtimeWorkflowContract = document.querySelector('.apps-detail__header .apps-workflow-contract-summary') as HTMLElement | null;
        expect(runtimeWorkflowContract).not.toBeNull();
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('Runtime contract aligned')).not.toBeNull();
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('approved-approval-workflow@v4.0.0')).not.toBeNull();
        const runtimeGovernance = await findRuntimeGovernancePanel();
        expect(within(runtimeGovernance).getByText('Package signature')).not.toBeNull();
        expect(within(runtimeGovernance).getByText(/ed25519.*sha256:approved-approval-key.*enterprise-market/)).not.toBeNull();
        expect(screen.getAllByText('approved · cap-approved-approval-app · enterprise_hub:skill:maclaw-app:approved-approval-app@7').length).toBeGreaterThan(0);
        expect(screen.getAllByText('run-approved-approval-install · proto-approved-approval').length).toBeGreaterThan(0);
        expect(screen.getAllByText('wf-approved-approval-1 · approved · expense.result_feedback').length).toBeGreaterThan(0);
        const workspace = await waitFor(() => document.querySelector('.apps-approval-workspace') as HTMLElement);
        fireEvent.click(within(workspace).getByText('Handled'));
        await waitFor(() => expect(within(workspace).getAllByText('EXP-HUB-APPROVED-1').length).toBeGreaterThan(0));
        expect(workspace.textContent).toContain('wf-approved-approval-1');
        expect(within(workspace).getAllByText('Approved expense').length).toBeGreaterThan(0);
        expect(within(workspace).getAllByText('approved-approval.pdf').length).toBeGreaterThan(0);
        expect(within(workspace).queryByText('No approval instances yet.')).toBeNull();

        fireEvent.click(screen.getByText('Approval status'));
        await waitFor(() => expect(listMaclawAppApprovalInstancesAllMock).toHaveBeenCalledWith('all', 200));
        const manager = await waitFor(() => document.querySelector('.apps-approval-manager') as HTMLElement);
        fireEvent.click(within(manager).getByText('Handled'));
        await waitFor(() => expect(within(manager).getAllByText('EXP-HUB-APPROVED-1').length).toBeGreaterThan(0));
        expect(manager.textContent).toContain('Approved Approval Queue App');
        expect(manager.textContent).toContain('wf-approved-approval-1');
        expect(within(manager).getAllByText('Approved expense').length).toBeGreaterThan(0);
        expect(within(manager).getAllByText('approved-approval.pdf').length).toBeGreaterThan(0);
    });
    it('shows detailed Hub app install errors from the backend review gate', async () => {
        const listMaclawAppPackageSubmissions = vi.fn().mockResolvedValue([{
            submission_id: 'hub-review-workflow-blocked',
            hub_capability_id: 'cap-workflow-blocked-app',
            submitted_at: '2026-06-17T01:15:00Z',
            status: 'approved',
            channel: 'hub',
            app_ids: ['workflow-blocked-app'],
            app_names: ['Workflow Blocked App'],
            message: 'approved by enterprise market',
        }]);
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };
        installMaclawAppPackageFromHubMock.mockRejectedValueOnce(new Error('cannot install MaClaw App from Hub: approval workflow contract is invalid: approval workflow Skill expense-flow version 2.1.0 does not match required 9.9.9'));

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        await waitFor(() => expect(screen.getByText('hub-review-workflow-blocked')).not.toBeNull());
        fireEvent.click(screen.getByText('Install approved app'));

        await waitFor(() => expect(installMaclawAppPackageFromHubMock).toHaveBeenCalledWith('cap-workflow-blocked-app'));
        await waitFor(() => expect(screen.getByText(/approval workflow contract is invalid/)).not.toBeNull());
        expect(screen.getByText(/does not match required 9\.9\.9/)).not.toBeNull();
    });
    it('refreshes local app package submission queue summaries on demand', async () => {
        const listMaclawAppPackageSubmissions = vi.fn()
            .mockResolvedValueOnce([])
            .mockResolvedValueOnce([{
                submission_id: 'local-review-refreshed',
                submitted_at: '2026-06-17T01:15:00Z',
                status: 'published',
                channel: 'hub',
                app_ids: ['refreshed-app'],
                app_names: ['刷新应用'],
                message: 'published by enterprise market',
            }]);
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());

        await waitFor(() => expect(screen.getByText('暂无本机待同步提交')).not.toBeNull());
        fireEvent.click(screen.getByText('刷新'));

        await waitFor(() => expect(listMaclawAppPackageSubmissions).toHaveBeenCalledTimes(2));
        expect(screen.getByText('local-review-refreshed')).not.toBeNull();
        expect(screen.getByText(/published by enterprise market/)).not.toBeNull();
        expect(screen.getByText(/刷新应用/)).not.toBeNull();
        expect(screen.getByText(/published by enterprise market/)).not.toBeNull();
    });

    it('syncs a local submission to Hub, refreshes published status, then installs it from market search', async () => {
        const reviewEvidence = {
            'flow-approval-app': {
                run_id: 'run-flow-review',
                test_protocol_fingerprint: 'proto-flow-review',
                result_coverage_primary: 'approval_result',
                result_coverage_ok: true,
                result_coverage_covered_count: 3,
                result_coverage_missing_count: 0,
                output_count: 1,
                artifact_count: 1,
                approval_status: 'approved',
                current_node: 'flow.result_feedback',
            },
        };
        const studioWorkspaceLayout = {
            schema: 'maclaw.app.ui.v1',
            entry: 'approval_workspace',
            template: 'left_nav',
            density: 'compact',
            primaryRegion: 'right',
            outputRegion: 'bottom',
            visibleRegionCount: 3,
            regionCount: 4,
            regionIds: ['approval_inbox', 'request_form', 'approval_detail', 'result_panel'],
            fingerprint: 'layoutflow1',
            savedInManifest: true,
        };
        const studioUIBinding = {
            schema: 'maclaw.app.ui.v1',
            entry: 'approval_workspace',
            generated: true,
            layouts: {
                approval_workspace: {
                    template: 'left_nav',
                    density: 'compact',
                    primaryRegion: 'right',
                    outputRegion: 'bottom',
                    regions: [
                        { id: 'approval_inbox', role: 'instance_list', placement: 'left', order: 1 },
                        { id: 'request_form', role: 'input', placement: 'right', order: 2 },
                        { id: 'approval_detail', role: 'detail', placement: 'center', visible: false, order: 3 },
                        { id: 'result_panel', role: 'output', placement: 'bottom', order: 4 },
                    ],
                    studio: { editable: true, savedInManifest: true, updatedBy: 'app_studio' },
                },
            },
        };
        const listMaclawAppPackageSubmissions = vi.fn()
            .mockResolvedValueOnce([{
                submission_id: 'local-review-flow',
                submitted_at: '2026-06-30T14:00:00Z',
                status: 'submitted',
                channel: 'local',
                app_ids: ['flow-approval-app'],
                app_names: ['Flow Approval App'],
                package_sha: 'flowsha1234567890',
                message: 'queued locally for enterprise market sync',
                review_evidence: reviewEvidence,
            }])
            .mockResolvedValueOnce([{
                submission_id: 'hub-review-flow',
                hub_capability_id: 'cap-flow-approval-app',
                submitted_at: '2026-06-30T14:00:00Z',
                status: 'pending_review',
                channel: 'hub',
                app_ids: ['flow-approval-app'],
                app_names: ['Flow Approval App'],
                message: 'submitted to enterprise Hub for review',
                review_evidence: reviewEvidence,
            }])
            .mockResolvedValueOnce([{
                submission_id: 'hub-review-flow',
                hub_capability_id: 'cap-flow-approval-app',
                submitted_at: '2026-06-30T14:00:00Z',
                status: 'published',
                channel: 'hub',
                app_ids: ['flow-approval-app'],
                app_names: ['Flow Approval App'],
                reviewed_at: '2026-06-30T14:05:00Z',
                published_at: '2026-06-30T14:08:00Z',
                reviewer: 'enterprise-reviewer',
                risk_level: 'medium',
                approved_scopes: ['app.run', 'approval.review'],
                message: 'published by enterprise market',
                review_evidence: reviewEvidence,
            }]);
        const syncMaclawAppPackageSubmissionToHub = vi.fn().mockResolvedValue({
            submission_id: 'hub-review-flow',
            source_submission_id: 'local-review-flow',
            hub_capability_id: 'cap-flow-approval-app',
            status: 'pending_review',
            channel: 'hub',
        });
        const refreshMaclawAppPackageSubmissionFromHub = vi.fn().mockResolvedValue({
            submission_id: 'hub-review-flow',
            hub_capability_id: 'cap-flow-approval-app',
            status: 'published',
            channel: 'hub',
        });
        const getMaclawAppPackageSubmission = vi.fn().mockResolvedValue({
            submission_id: 'hub-review-flow',
            status: 'published',
            channel: 'hub',
            reviewer: 'enterprise-reviewer',
            risk_level: 'medium',
            events: [
                { at: '2026-06-30T14:00:00Z', status: 'submitted', channel: 'local' },
                { at: '2026-06-30T14:08:00Z', status: 'published', channel: 'hub' },
            ],
            package: {
                schema: 'maclaw.app.pack.v1',
                privateMarker: 'x_maclaw_apps',
                apps: [{
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: {
                        id: 'flow-approval-app',
                        name: 'Flow Approval App',
                        binding: { ui: studioUIBinding },
                        governance: { workspaceLayout: studioWorkspaceLayout },
                    },
                }],
            },
        });
        (window as any).go = {
            main: {
                App: {
                    ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions,
                    SyncMaclawAppPackageSubmissionToHub: syncMaclawAppPackageSubmissionToHub,
                    RefreshMaclawAppPackageSubmissionFromHub: refreshMaclawAppPackageSubmissionFromHub,
                    GetMaclawAppPackageSubmission: getMaclawAppPackageSubmission,
                },
            },
        };
        searchMixedSkillsMock.mockResolvedValueOnce([{
            id: 'cap-flow-approval-app',
            install_ref: 'cap-flow-approval-app',
            name: 'Flow Approval App',
            description: 'Published approval app from Hub review queue',
            source: 'enterprise_hub',
            source_label: 'Enterprise Hub',
            product_kind: 'maclaw_app_skill',
            is_maclaw_app: true,
            maclaw_app_id: 'flow-approval-app',
            maclaw_app_name: 'Flow Approval App',
            maclaw_app_description: 'Published approval app from Hub review queue',
            maclaw_app_kind: 'enterprise_approval_app',
            maclaw_app_category: 'Finance',
            maclaw_app_icon: 'receipt',
            review_evidence: reviewEvidence,
        }]);
        const hubPackage = {
            schema: 'maclaw.app.pack.v1',
            privateMarker: 'x_maclaw_apps',
            source: 'enterprise_hub',
            package_signature: {
                schema: 'maclaw.app.package_signature.v1',
                algorithm: 'ed25519',
                public_key_fingerprint: 'sha256:contract-package-key',
                signed_at: '2026-07-01T02:00:00Z',
                signed_by: 'enterprise-market',
                package_sha256: 'pkg-contract-approval',
            },
            apps: [{
                schema: 'maclaw.app.v1',
                privateMarker: 'x_maclaw_apps',
                installUnit: 'enterprise_app_pack',
                app: {
                    id: 'flow-approval-app',
                    name: 'Flow Approval App',
                    description: 'Published approval app from Hub review queue',
                    category: 'Finance',
                    kind: 'enterprise_approval_app',
                    icon: 'receipt',
                    launchMode: 'agent_dynamic_ui',
                    binding: {
                        datasrv: { domain: 'finance', datasetID: 'finance.flow', objectRole: 'flow_request', preferredAction: 'finance.flow_submit' },
                        dependencies: { skills: [{ id: 'flow-workflow', kind: 'workflow_skill', source: 'hub', required: true }] },
                        mis: { approvalBindings: [{ event: 'finance.flow.submitted', objectRole: 'flow_request', workflowSkillId: 'flow-workflow' }] },
                        ui: studioUIBinding,
                    },
                    governance: { workspaceLayout: studioWorkspaceLayout },
                },
            }],
        };
        installSelectedMaclawAppPackageFromHubMock.mockResolvedValueOnce({
            schema: 'maclaw.app.hub_install.v1',
            capability_id: 'cap-flow-approval-app',
            package: hubPackage,
            package_json: JSON.stringify(hubPackage),
            source_app_count: 1,
            source_app_ids: ['flow-approval-app'],
            app_count: 1,
            app_ids: ['flow-approval-app'],
            install_plan: {
                schema: 'maclaw.app.install_plan.v1',
                apps: [{ id: 'flow-approval-app', name: 'Flow Approval App', kind: 'enterprise_approval_app' }],
                dependencies: [{ id: 'flow-workflow', kind: 'workflow_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed', app_ids: ['market-flow-approval-app'] }],
                has_missing_required: false,
                has_blocking_dependency: false,
            },
            install_record: {
                schema: 'maclaw.app.install_record.v1',
                app_count: 1,
                apps: [{ id: 'market-flow-approval-app', name: 'Flow Approval App', kind: 'enterprise_approval_app' }],
                dependencies: [{ id: 'flow-workflow', kind: 'workflow_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed' }],
                install_evidence: {
                    'market-flow-approval-app': {
                        workspace_layout: {
                            entry: 'approval_workspace',
                            template: 'left_nav',
                            density: 'compact',
                            primaryRegion: 'right',
                            outputRegion: 'bottom',
                            visibleRegionCount: 3,
                            regionCount: 4,
                            regionIds: ['approval_inbox', 'request_form', 'approval_detail', 'result_panel'],
                            fingerprint: 'layoutflow1',
                            savedInManifest: true,
                        },
                        result_contract: { primary: 'approval_result', types: ['approval_result', 'artifact', 'content'] },
                        test_evidence: {
                            run_id: 'run-flow-install',
                            test_protocol_fingerprint: 'proto-flow-install',
                            result_coverage: { ok: true, primary: 'approval_result', covered_types: ['approval_result', 'artifact', 'content'], missing_types: [] },
                            outputs: [{ kind: 'approval_result', title: 'Flow approved', text: 'approved', status: 'approved' }],
                            artifacts: [{ id: 'flow-artifact', uri: 'artifact://flow/result.pdf', name: 'flow-approval.pdf', status: 'ready' }],
                            approval_instance: {
                                workflow_instance_id: 'wf-flow-installed-1',
                                approval_id: 'approval-flow-installed-1',
                                record_id: 'FLOW-1',
                                status: 'approved',
                                current_node: 'flow.result_feedback',
                                workflow_skill_id: 'flow-workflow',
                                result_status: 'approved',
                                business_status: 'flow_approved',
                                result_payload: { approval_result: 'approved', business_status: 'flow_approved' },
                                outputs: [{ kind: 'approval_result', title: 'Flow approved', text: 'approved', status: 'approved' }],
                                artifacts: [{ id: 'flow-artifact', uri: 'artifact://flow/result.pdf', name: 'flow-approval.pdf', status: 'ready' }],
                                approval_instance_view_verified: true,
                            },
                        },
                    },
                },
            },
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        await waitFor(() => expect(screen.getByText('local-review-flow')).not.toBeNull());
        fireEvent.click(screen.getByText('Sync to Hub'));

        await waitFor(() => expect(syncMaclawAppPackageSubmissionToHub).toHaveBeenCalledWith('local-review-flow'));
        await waitFor(() => expect(screen.getByText('hub-review-flow')).not.toBeNull());
        expect(screen.getByText(/pending_review/)).not.toBeNull();
        fireEvent.click(screen.getByText('Refresh Hub Status'));

        await waitFor(() => expect(refreshMaclawAppPackageSubmissionFromHub).toHaveBeenCalledWith('hub-review-flow'));
        await waitFor(() => expect(screen.getByText(/published by enterprise market/)).not.toBeNull());
        fireEvent.click(screen.getByText('View details'));
        await waitFor(() => expect(getMaclawAppPackageSubmission).toHaveBeenCalledWith('hub-review-flow'));
        expect(screen.getByText(/enterprise-reviewer/)).not.toBeNull();
        expect(screen.getByText(/Risk: medium/)).not.toBeNull();
        expect(screen.getByText(/Workspace layout: Flow Approval App · approval_workspace · left_nav · compact · 3 regions · fp:layoutflow1/)).not.toBeNull();
        expect(screen.getByText('run-flow-review · proto-flow-review')).not.toBeNull();
        expect(screen.getByText('approval_result · Covered: 3')).not.toBeNull();
        expect(screen.getByText('Output: 1 · Output artifacts: 1')).not.toBeNull();

        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText('Search enterprise Hub apps'), { target: { value: 'flow approval' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search Hub' }));
        const marketRow = (await screen.findByText('Flow Approval App')).closest('.apps-market-row') as HTMLElement;
        expect(marketRow).toBeTruthy();
        const marketReviewEvidence = within(marketRow).getByLabelText('Review evidence');
        expect(within(marketReviewEvidence).getByText('run-flow-review · proto-flow-review')).not.toBeNull();
        expect(within(marketReviewEvidence).getByText('approval_result · Covered: 3')).not.toBeNull();
        fireEvent.click(within(marketRow).getByRole('button', { name: 'Add: Flow Approval App' }));

        await waitFor(() => expect(installSelectedMaclawAppPackageFromHubMock).toHaveBeenCalledWith('cap-flow-approval-app', ['market-flow-approval-app']));
        expect(installMaclawAppPackageFromHubMock).not.toHaveBeenCalled();
        expect(installMaclawAppDependenciesMock).not.toHaveBeenCalled();
        await waitFor(() => expect(within(marketRow).getByText('Already installed · Source package 1 · installed 1 · 1 dependencies')).not.toBeNull());
        const installed = latestStoredCustomApp('Flow Approval App');
        expect(installed.marketCapabilityID).toBe('cap-flow-approval-app');
        expect(installed.installEvidence.workspace_layout).toMatchObject({
            entry: 'approval_workspace',
            template: 'left_nav',
            density: 'compact',
            primaryRegion: 'right',
            outputRegion: 'bottom',
            visibleRegionCount: 3,
            fingerprint: 'layoutflow1',
            savedInManifest: true,
        });
        expect(installed.importedRunEvidence.approvalInstance).toMatchObject({
            instanceId: 'wf-flow-installed-1',
            approvalID: 'approval-flow-installed-1',
            status: 'approved',
            currentNode: 'flow.result_feedback',
        });
    });

    it('installs HubCenter skillmarket maclaw apps via InstallMixedSkill instead of enterprise package API', async () => {
        searchMixedSkillsMock.mockResolvedValueOnce([{
            id: 'skill-hc-invoice-app',
            install_ref: 'skill-hc-invoice-app',
            name: 'HubCenter Invoice App',
            description: 'Published from HubCenter skill market',
            source: 'skillmarket',
            source_label: 'Hub / HubCenter',
            product_kind: 'maclaw_app_skill',
            is_maclaw_app: true,
            maclaw_app_id: 'hc-invoice-app',
            maclaw_app_name: 'HubCenter Invoice App',
            maclaw_app_description: 'Published from HubCenter skill market',
            maclaw_app_kind: 'tool_app',
            maclaw_app_category: 'Finance',
            maclaw_app_icon: 'receipt',
        }]);
        installMixedSkillMock.mockResolvedValue(undefined);
        // Page mount also calls ListSkillAppManifests — use sticky mock, not Once.
        listSkillAppManifestsMock.mockResolvedValue([{
            id: 'hc-invoice-app',
            name: 'HubCenter Invoice App',
            description: 'Published from HubCenter skill market',
            category: 'Finance',
            icon: 'receipt',
            skill_id: 'skill-hc-invoice-app',
            app_definition_file: 'maclaw.app.json',
            input_mode: 'file',
            app_definition: {
                schema: 'maclaw.app.v1',
                privateMarker: 'x_maclaw_apps',
                app: {
                    id: 'hc-invoice-app',
                    name: 'HubCenter Invoice App',
                    description: 'Published from HubCenter skill market',
                    category: 'Finance',
                    kind: 'tool_app',
                    icon: 'receipt',
                    binding: {
                        skill: { id: 'skill-hc-invoice-app', appDefinitionFile: 'maclaw.app.json', inputMode: 'file' },
                        // Legacy SkillMarket app definitions can retain `local`
                        // for a public runtime skill. The reconstructed install
                        // manifest must preserve that declaration while carrying
                        // trusted market provenance for the backend's allowlisted
                        // compatibility mapping.
                        dependencies: {
                            skills: [{ id: 'paper_pdf_translator', kind: 'runtime_skill', required: true, source: 'local' }],
                        },
                    },
                },
            },
        }]);
        installMaclawAppDependenciesMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'hc-invoice-app', name: 'HubCenter Invoice App', kind: 'tool_app' }],
            dependencies: [],
            has_missing_required: false,
            has_blocking_dependency: false,
        });
        recordMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_record.v1',
            app_id: 'hc-invoice-app',
            source: 'skillmarket',
        });

        render(<AppsPage lang="en" />);
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText('Search enterprise Hub apps'), { target: { value: 'hubcenter invoice' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search Hub' }));

        const marketRow = (await screen.findByText('HubCenter Invoice App')).closest('.apps-market-row') as HTMLElement;
        expect(marketRow).toBeTruthy();
        fireEvent.click(within(marketRow).getByRole('button', { name: 'Add: HubCenter Invoice App' }));

        await waitFor(() => expect(installMixedSkillMock).toHaveBeenCalledWith('skillmarket', 'skill-hc-invoice-app', 'skill-hc-invoice-app'));
        expect(installSelectedMaclawAppPackageFromHubMock).not.toHaveBeenCalled();
        expect(installMaclawAppPackageFromHubMock).not.toHaveBeenCalled();
        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalled());
        const plannedManifest = JSON.parse(String(installMaclawAppDependenciesMock.mock.calls[0][0]));
        expect(plannedManifest.app.dependency_source).toBeUndefined();
        expect(plannedManifest.app.market_install_source).toBeUndefined();
        expect(plannedManifest.app.binding.dependency_source).toBeUndefined();
        expect(plannedManifest.app.dependencies.skills).toEqual([
            expect.objectContaining({ id: 'paper_pdf_translator', source: 'local' }),
        ]);
        await waitFor(() => expect(recordMaclawAppInstallMock).toHaveBeenCalled());
        const recordArgs = recordMaclawAppInstallMock.mock.calls[0];
        expect(recordArgs[1]).toBe('skillmarket');
    });

    it('copies full queued app package details from the durable queue', async () => {
        const listMaclawAppPackageSubmissions = vi.fn().mockResolvedValue([{
            submission_id: 'local-review-detail',
            submitted_at: '2026-06-17T01:10:00Z',
            status: 'submitted',
            channel: 'local',
            app_ids: ['queued-detail-app'],
            message: 'queued locally for enterprise market sync',
        }]);
        const getMaclawAppPackageSubmission = vi.fn().mockResolvedValue({
            submission_id: 'local-review-detail',
            status: 'review_failed',
            events: [{ at: '2026-06-17T01:10:00Z', status: 'submitted', channel: 'local', submission_id: 'local-review-detail' }],
            review_issues: [{ path: 'apps[0]', severity: 'error', message: '缺少运行证据' }],
            package: {
                schema: 'maclaw.app.pack.v1',
                privateMarker: 'x_maclaw_apps',
                apps: [{
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: { id: 'queued-detail-app', name: '队列详情应用' },
                }],
            },
        });
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions, GetMaclawAppPackageSubmission: getMaclawAppPackageSubmission } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());

        await waitFor(() => expect(screen.getByText('local-review-detail')).not.toBeNull());
        fireEvent.click(screen.getByText('复制队列包'));

        await waitFor(() => expect(getMaclawAppPackageSubmission).toHaveBeenCalledWith('local-review-detail'));
        await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledTimes(1));
        const copied = JSON.parse((navigator.clipboard.writeText as any).mock.calls[0][0]);
        expect(copied.schema).toBe('maclaw.app.pack.v1');
        expect(copied.apps[0].app.id).toBe('queued-detail-app');
        expect(screen.getByText('已复制')).not.toBeNull();

        fireEvent.click(screen.getByText('复制审计'));
        await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledTimes(2));
        const audit = JSON.parse((navigator.clipboard.writeText as any).mock.calls[1][0]);
        expect(audit.submission_id).toBe('local-review-detail');
        expect(audit.events[0].status).toBe('submitted');
        expect(audit.review_issues[0].message).toBe('缺少运行证据');
        expect(audit.package.apps[0].app.id).toBe('queued-detail-app');
        expect(screen.getByText('审计已复制')).not.toBeNull();
    });

    it('shows inline queued app package audit details', async () => {
        const listMaclawAppPackageSubmissions = vi.fn().mockResolvedValue([{
            submission_id: 'local-review-inline-detail',
            submitted_at: '2026-06-17T01:18:00Z',
            status: 'review_failed',
            channel: 'local',
            app_ids: ['queued-inline-app'],
            app_names: ['队列内联应用'],
            reviewer: 'market-reviewer',
            risk_level: 'medium',
            review_issues: [{ path: 'apps[0].app.governance.testEvidence', severity: 'error', message: '缺少运行证据' }],
        }]);
        const getMaclawAppPackageSubmission = vi.fn().mockResolvedValue({
            submission_id: 'local-review-inline-detail',
            status: 'review_failed',
            events: [
                { at: '2026-06-17T01:18:00Z', status: 'submitted', channel: 'local', submission_id: 'local-review-inline-detail' },
                { at: '2026-06-17T01:20:00Z', status: 'review_failed', channel: 'hub', submission_id: 'local-review-inline-detail' },
            ],
            package: {
                schema: 'maclaw.app.pack.v1',
                privateMarker: 'x_maclaw_apps',
                apps: [{
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: { id: 'queued-inline-app', name: '队列内联应用' },
                }],
            },
        });
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions, GetMaclawAppPackageSubmission: getMaclawAppPackageSubmission } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());

        await waitFor(() => expect(screen.getByText('local-review-inline-detail')).not.toBeNull());
        fireEvent.click(screen.getByText('查看详情'));

        await waitFor(() => expect(getMaclawAppPackageSubmission).toHaveBeenCalledWith('local-review-inline-detail'));
        expect(screen.getByText('提交详情')).not.toBeNull();
        expect(screen.getByText(/market-reviewer/)).not.toBeNull();
        expect(screen.getByText(/风险: medium/)).not.toBeNull();
        expect(screen.getByText(/包含应用: 队列内联应用/)).not.toBeNull();
        expect(screen.getByText(/审核问题: error · apps\[0\]\.app\.governance\.testEvidence · 缺少运行证据/)).not.toBeNull();
        expect(screen.getByText(/审计事件: 2026-06-17T01:18:00Z · submitted · local/)).not.toBeNull();

        fireEvent.click(screen.getByText('收起详情'));
        expect(screen.queryByText('提交详情')).toBeNull();
    });

    it('merges durable queue review status into local publish cards', async () => {
	        const reviewedApp = {
	            ...dynamicToolApp('local-app-queue-published', '队列回写应用', '法务', 'contract', ['docx', 'pdf']),
	            recentUsedAt: '2026-06-17T00:00:00.000Z',
	        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [reviewedApp.id],
            customApps: [reviewedApp],
            recentUsedAtById: { [reviewedApp.id]: reviewedApp.recentUsedAt },
        }));
	        seedSuccessfulLocalAppRun(reviewedApp, {
	            artifacts: [{ id: 'artifact-queue-published', uri: 'artifact://skill-run/run-ok-queue-published/artifact-queue-published', name: 'queue-published.pdf', status: 'ready' }],
	            outputs: [{ kind: 'artifact', title: 'Queue published PDF', artifact_id: 'artifact-queue-published', status: 'ready' }],
	            resultPayload: { status: 'ok' },
	        });
        const listMaclawAppPackageSubmissions = vi.fn().mockResolvedValueOnce([{
            submission_id: 'market-review-published',
            submitted_at: '2026-06-17T01:20:00Z',
            status: 'published',
            channel: 'hub',
            app_ids: [reviewedApp.id],
            reviewed_at: '2026-06-17T01:25:00Z',
            published_at: '2026-06-17T01:30:00Z',
            reviewer: 'market-reviewer',
            risk_level: 'high',
            approved_scopes: ['finance.expense_submit', 'finance.audit'],
            review_issues: [
                { path: 'apps[0].app.governance.testEvidence', severity: 'error', message: '缺少运行证据', suggestion: '先运行一次应用' },
                { path: 'apps[0].app.permissions', severity: 'warning', message: '权限范围偏宽', suggestion: '缩小到财务单' },
                { path: 'apps[0].app.support.owner', severity: 'info', message: '建议补充负责' },
                { path: 'apps[0].app.runtime', severity: 'info', message: '建议补充回滚说明' },
            ],
            message: 'published by enterprise market',
        }]).mockResolvedValue([]);
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({
            submission_id: 'market-review-resubmitted',
            submitted_at: '2026-06-17T02:00:00Z',
            status: 'submitted',
            channel: 'hub',
            message: 'submitted updated app',
        });
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions, SubmitMaclawAppPackage: submitMaclawAppPackage } } };
        // A green authoritative dependency check keeps the fix route on the editor.
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: reviewedApp.id, name: reviewedApp.name, kind: reviewedApp.kind }],
            dependencies: [{ id: reviewedApp.id, kind: 'runtime_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [reviewedApp.id] }],
            has_missing_required: false,
            has_blocking_dependency: false,
        });

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());

	        await waitFor(() => expect(screen.getAllByText('已发布').length).toBeGreaterThan(0));
	        expect(screen.getAllByText(/market-review-published/).length).toBeGreaterThan(0);
	        expect(screen.getAllByText(/published by enterprise market/).length).toBeGreaterThan(0);
	        expect(screen.getAllByText(/风险: high/).length).toBeGreaterThan(0);
	        expect(screen.getAllByText(/批准权限: finance\.expense_submit, finance\.audit/).length).toBeGreaterThan(0);
	        expect(screen.getAllByText(/apps\[0\]\.app\.governance\.testEvidence/).length).toBeGreaterThan(0);
	        expect(screen.getAllByText(/apps\[0\]\.app\.permissions/).length).toBeGreaterThan(0);
	        expect(screen.getAllByText(/apps\[0\]\.app\.support\.owner/).length).toBeGreaterThan(0);
	        const storedSubmission = JSON.parse(window.localStorage.getItem('maclaw:apps-publish-submissions:v1') || '{}')[reviewedApp.id];
	        expect(storedSubmission).toEqual(expect.objectContaining({
	            status: 'published',
	            channel: 'hub',
	            reviewedAt: '2026-06-17T01:25:00Z',
	            publishedAt: '2026-06-17T01:30:00Z',
	            reviewer: 'market-reviewer',
	            riskLevel: 'high',
	            approvedScopes: ['finance.expense_submit', 'finance.audit'],
	        }));
	        expect(storedSubmission.reviewIssues).toEqual(expect.arrayContaining([
	            expect.objectContaining({ path: 'apps[0].app.runtime', message: '建议补充回滚说明' }),
	        ]));

        fireEvent.click(screen.getByText('去修复'));
        await waitFor(() => expect(screen.getByRole('tab', { name: '应用管理' }).getAttribute('aria-selected')).toBe('true'));
        const nameInput = screen.getByDisplayValue('队列回写应用');
        expect(nameInput).not.toBeNull();
        fireEvent.change(nameInput, { target: { value: '队列回写应用修正' } });
        fireEvent.click(screen.getByText('保存'));
        await waitFor(() => expect(latestStoredCustomApp('队列回写应用修正')).toBeTruthy());
        const updatedQueueApp = latestStoredCustomApp('队列回写应用修正');
        seedSuccessfulLocalAppRun(updatedQueueApp, {
            artifacts: [{ id: 'artifact-queue-doc-v2', uri: 'artifact://skill-run/run-ok-reviewed-v2/artifact-queue-doc-v2', name: 'queue-review-v2.pdf', status: 'ready' }],
            outputs: [{ kind: 'artifact', title: 'Queue review PDF v2', artifact_id: 'artifact-queue-doc-v2', status: 'ready' }],
            resultPayload: { status: 'ok' },
        });

        cleanup();
        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(getStudioButton());
	        fireEvent.click(getPublishTab());
	        await waitFor(() => expect(screen.getAllByText('本地已修改，需重新提交').length).toBeGreaterThan(0));
	        expect(screen.getByText(/本地修改:/)).not.toBeNull();
	        const modifiedSubmission = JSON.parse(window.localStorage.getItem('maclaw:apps-publish-submissions:v1') || '{}')[reviewedApp.id];
	        expect(modifiedSubmission.modifiedAt).toBeTruthy();
	        expect(modifiedSubmission.version).toBe(2);
	        const resubmitButton = screen.getByRole('button', { name: '一键发布' }) as HTMLButtonElement;
	        expect(resubmitButton.disabled).toBe(true);
	        expect(submitMaclawAppPackage).not.toHaveBeenCalled();
    });

    it('updates the draft manifest preview while creating an app', () => {
        render(<AppsPage lang="zh-Hans" />);

	    fireEvent.click(getStudioButton());
	    fireEvent.change(getCreateAppNameInput(), { target: { value: '合同归档' } });

	    const preview = document.querySelector('#apps-create-manifest-preview') as HTMLElement;
	    expect(preview.textContent).toContain('合同归档');
	    expect(preview.textContent).toContain('fixed_skill_ui');
	    expect(preview.textContent).toContain('draft-app');
	});

    it('offers expanded semantic app icons in app studio', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getCreateAppNameInput(), { target: { value: '自动化巡检' } });
        fireEvent.click(screen.getByTitle('Agent/自动化 (bot)'));
        fireEvent.click(screen.getByTitle('靛蓝 #5b5ea6'));

        expect(screen.getByRole('button', { name: '付款/财务 (wallet)' })).not.toBeNull();
        expect(screen.getByRole('button', { name: '看板/指标 (dashboard)' })).not.toBeNull();
        expect(screen.getByText(/"icon": "bot"/)).not.toBeNull();
        expect(screen.getByText(/"accent": "#5b5ea6"/)).not.toBeNull();
    });

    it('generates a draft app definition from a natural language prompt', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getDraftPromptInput(), {
            target: { value: '做一个合同归档应用，上传 Word/PDF，输出归档编号和审核结果' },
        });
        fireEvent.click(screen.getByText('生成草稿'));

        expect(screen.getByDisplayValue('合同归档应用')).not.toBeNull();
        expect(screen.getByDisplayValue('文档处理')).not.toBeNull();
        expect(screen.getByText(/fixed_skill_ui/)).not.toBeNull();
        expect(screen.getAllByText(/合同归档应用/).length).toBeGreaterThan(0);
    });

    it('uses studio type choices as create-form presets', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        const kindPicker = document.querySelector('.apps-studio-kind') as HTMLElement;
        const appCardButton = within(kindPicker).getByRole('button', { name: /企业审批型/ }) as HTMLButtonElement;
        const toolCardButton = within(kindPicker).getByRole('button', { name: /工具应用/ }) as HTMLButtonElement;
        const automationCardButton = within(kindPicker).getByRole('button', { name: /自动化/ }) as HTMLButtonElement;

        expect(toolCardButton.getAttribute('aria-pressed')).toBe('true');

        fireEvent.click(appCardButton);
        expect(appCardButton.getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByDisplayValue('OA')).not.toBeNull();
        expect(screen.getByText(/"kind": "enterprise_approval_app"/)).not.toBeNull();
        expect(screen.getByText(/"launchMode": "agent_dynamic_ui"/)).not.toBeNull();

        fireEvent.click(automationCardButton);
        expect(automationCardButton.getAttribute('aria-pressed')).toBe('true');
        expect(screen.getAllByDisplayValue('自动化').length).toBeGreaterThan(0);
        expect(screen.getByText(/"kind": "automation_app"/)).not.toBeNull();
        expect(screen.getByText(/"launchMode": "automation_console"/)).not.toBeNull();
    });

    it('opens the current Hub approval workflow designer from App Studio', async () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        const kindPicker = document.querySelector('.apps-studio-kind') as HTMLElement;
        fireEvent.click(within(kindPicker).getByRole('button', { name: /Approval app/ }));
        const designButton = screen.getByRole('button', { name: /^Design$|^设计$/ });

        expect(designButton.getAttribute('title')).toBe('Open approval workflow designer');
        fireEvent.click(designButton);

        await waitFor(() => expect(browserOpenURLMock).toHaveBeenCalledWith('https://hub.example.com/approval_workflow#machine_id=machine-1&token=token-1'));
    });

    it('writes enterprise app skill dependencies from selected installed and market skills', async () => {
        listNLSkillsMock.mockResolvedValue([
            { name: 'expense-super-app', description: 'Expense app runtime' },
            { name: 'installed-approval-flow', description: 'Installed approval workflow', productKind: 'workflow_skill' },
        ]);
        searchMixedSkillsMock.mockResolvedValue([{
            id: 'expense-approval-flow',
            name: 'Expense Approval Flow',
            description: 'Approval workflow from SkillMarket',
            source: 'skillmarket',
            source_label: 'SkillMarket',
            installed: false,
            productKind: 'approval_workflow_skill',
        }]);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLElement);
        fireEvent.change(document.querySelector('.apps-create-form input[placeholder]') as HTMLInputElement, { target: { value: 'approval-workbench' } });
        const kindPicker = document.querySelector('.apps-studio-kind') as HTMLElement;
        fireEvent.click(within(kindPicker).getByRole('button', { name: /企业审批型/ }));
        const appSkillPicker = screen.getByTestId('studio-app-skill-id');
        await waitFor(() => expect(within(appSkillPicker).getAllByText('expense-super-app').length).toBeGreaterThan(0));
        expect(within(appSkillPicker).queryByText('installed-approval-flow')).toBeNull();
        fireEvent.click(within(appSkillPicker).getByRole('option', { name: /expense-super-app/ }) as HTMLButtonElement);
        const workflowSkillPicker = screen.getByTestId('studio-workflow-skill-id');
        fireEvent.change(workflowSkillPicker.querySelector('input') as HTMLInputElement, { target: { value: 'expense approval' } });
        fireEvent.click(within(workflowSkillPicker).getByRole('button', { name: /搜索/ }));
        await waitFor(() => expect(searchMixedSkillsMock).toHaveBeenCalledWith('expense approval'));
        fireEvent.click(within(workflowSkillPicker).getByText('Expense Approval Flow').closest('button') as HTMLButtonElement);
        fireEvent.change(screen.getByTestId('studio-workflow-skill-version'), { target: { value: '2.1.0' } });
        fireEvent.change(screen.getByTestId('studio-approval-event'), { target: { value: 'finance.expense.submitted' } });
        fireEvent.change(screen.getByTestId('studio-approval-object-role'), { target: { value: 'expense_report' } });

        expect(screen.getByText(/"appSkill"/)).not.toBeNull();
        expect(screen.getByText(/"id": "expense-super-app"/)).not.toBeNull();
        expect(screen.getByText(/"kind": "workflow_skill"/)).not.toBeNull();
        expect(screen.getByText(/"id": "expense-approval-flow"/)).not.toBeNull();
        expect(screen.getByText(/"source": "market"/)).not.toBeNull();
        expect(screen.getByText(/"approvalBindings"/)).not.toBeNull();

        fireEvent.click(document.querySelector('.apps-create-form .apps-actions .apps-primary-button') as HTMLElement);
        fireEvent.click(document.getElementById('apps-studio-tab-manage') as HTMLElement);
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('approval-workbench')) as HTMLElement;
        fireEvent.click(within(row).getByTitle(manageManifestTitle));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');

        expect(manifest.app.binding.appSkill.id).toBe('expense-super-app');
        expect(manifest.app.binding.appSkill.source).toBe('local');
        expect(manifest.app.binding.dependencies.skills[0].id).toBe('expense-approval-flow');
        expect(manifest.app.binding.dependencies.skills[0].version).toBe('2.1.0');
        expect(manifest.app.binding.dependencies.skills[0].source).toBe('market');
        expect(manifest.app.binding.dependencies.skills[0].kind).toBe('workflow_skill');
        expect(manifest.app.binding.dependencies.skills[0].source).toBe('market');
        expect(manifest.app.binding.mis.approvalBindings[0]).toMatchObject({ event: 'finance.expense.submitted', workflowSkillId: 'expense-approval-flow', workflowVersion: '2.1.0', objectRole: 'expense_report' });
        expect(manifest.app.governance.workflowContract).toMatchObject({
            schema: 'maclaw.app.workflow_contract.v1',
            workflowSkillId: 'expense-approval-flow',
            workflowVersion: '2.1.0',
            objectRole: 'expense_report',
            requiredInputs: ['record_ref', 'applicant', 'business_payload'],
            decisionOutputs: ['approved', 'rejected', 'attention'],
            statusMapping: expect.objectContaining({ pending: 'approval_pending', approved: 'approved', rejected: 'rejected', attention: 'attention' }),
        });
    });

    it('saves visual approval workflow node mappings into created manifests', async () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(document.querySelector('.apps-create-form input[placeholder]') as HTMLInputElement, { target: { value: 'approval-nodes' } });
        const kindPicker = document.querySelector('.apps-studio-kind') as HTMLElement;
        fireEvent.click(within(kindPicker).getByRole('button', { name: /Approval app/ }));
        fireEvent.change(screen.getByTestId('studio-workflow-submitNode'), { target: { value: 'expense.intake' } });
        fireEvent.change(screen.getByTestId('studio-workflow-approvalNode'), { target: { value: 'finance.director_review' } });
        fireEvent.change(screen.getByTestId('studio-workflow-resultNode'), { target: { value: 'expense.result_pack' } });
        fireEvent.change(screen.getByTestId('studio-workflow-status-approved'), { target: { value: 'finance_approved' } });

        expect(screen.getByText(/"workflow"/)).not.toBeNull();
        expect(screen.getByText(/"approvalNode": "finance.director_review"/)).not.toBeNull();
        expect(screen.getByText(/"approved": "finance_approved"/)).not.toBeNull();

        fireEvent.click(document.querySelector('.apps-create-form .apps-actions .apps-primary-button') as HTMLElement);
        fireEvent.click(document.getElementById('apps-studio-tab-manage') as HTMLElement);
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('approval-nodes')) as HTMLElement;
        fireEvent.click(within(row).getByTitle(manageManifestTitle));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(manifest.app.binding.workflow).toMatchObject({
            schema: 'maclaw.app.workflow.v1',
            submitNode: 'expense.intake',
            approvalNode: 'finance.director_review',
            resultNode: 'expense.result_pack',
            statusMapping: expect.objectContaining({ approved: 'finance_approved' }),
        });
    });

    it('includes tool app input and output modes in draft manifests', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getCreateAppNameInput(), { target: { value: '表格清洗' } });
        fireEvent.change(screen.getByDisplayValue('文件上传'), { target: { value: 'mixed' } });
        fireEvent.click(screen.getByText('Excel / XLSX'));

        expect(screen.getByText(/"inputMode": "mixed"/)).not.toBeNull();
        expect(screen.getByText(/"outputModes"/)).not.toBeNull();
        expect(screen.getByText(/"xlsx"/)).not.toBeNull();
    });

    it('saves app studio layout choices into app manifests', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLElement);
        fireEvent.change(document.querySelector('.apps-create-form input[placeholder]') as HTMLInputElement, { target: { value: 'layout-workbench' } });
        fireEvent.click(screen.getByTestId('studio-layout-template-left_nav'));
        fireEvent.change(screen.getByTestId('studio-layout-density'), { target: { value: 'compact' } });
        fireEvent.click(screen.getByTestId('studio-layout-slot-center'));
        fireEvent.change(screen.getByTestId('studio-output-region'), { target: { value: 'bottom' } });
        fireEvent.change(screen.getByTestId('studio-layout-region-output_panel'), { target: { value: 'left' } });

        expect(screen.getByText(/"template": "left_nav"/)).not.toBeNull();
        expect(screen.getByText(/"density": "compact"/)).not.toBeNull();
        expect(screen.getByText(/"primaryRegion": "center"/)).not.toBeNull();
        expect(screen.getByText(/"outputRegion": "bottom"/)).not.toBeNull();
        expect(screen.getByText(/"savedInManifest": true/)).not.toBeNull();

        fireEvent.click(document.querySelector('.apps-create-form .apps-actions .apps-primary-button') as HTMLElement);
        fireEvent.click(document.getElementById('apps-studio-tab-manage') as HTMLElement);
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('layout-workbench')) as HTMLElement;
        fireEvent.click(within(row).getByTitle(manageManifestTitle));
        const manifestText = document.querySelector('.apps-manage-manifest')?.textContent || '';
        const manifest = JSON.parse(manifestText);
        const layout = manifest.app.binding.ui.layouts.tool_workspace;

        expect(layout.template).toBe('left_nav');
        expect(layout.density).toBe('compact');
        expect(layout.primaryRegion).toBe('center');
        expect(layout.outputRegion).toBe('bottom');
        expect(layout.regions).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'file_queue', role: 'input', placement: 'center' }),
            expect.objectContaining({ id: 'output_panel', role: 'output', placement: 'left' }),
        ]));
        expect(layout.studio.savedInManifest).toBe(true);

        const closeButton = Array.from(document.querySelectorAll('.apps-detail__actions .apps-secondary-button')).pop() as HTMLElement;
        fireEvent.click(closeButton);
        fireEvent.click(screen.getAllByText('layout-workbench')[0]);

        await waitFor(() => expect(document.querySelector('.apps-runtime-layout')).not.toBeNull());
        const runtimeLayout = document.querySelector('.apps-runtime-layout') as HTMLElement;
        expect(runtimeLayout.dataset.template).toBe('left_nav');
        expect(runtimeLayout.dataset.density).toBe('compact');
        expect(runtimeLayout.dataset.primaryRegion).toBe('center');
        expect(runtimeLayout.dataset.outputRegion).toBe('bottom');
        expect(runtimeLayout.dataset.regionCount).toBe('4');
        expect(document.querySelector('.apps-runtime-input')?.getAttribute('data-region')).toBe('center');
        expect(document.querySelector('.apps-runtime-output')?.getAttribute('data-region')).toBe('left');
    });
    it('honors saved workspace region placements over summary layout fields', async () => {
        const app = {
            id: 'region-placement-tool',
            name: 'Region Placement Tool',
            description: 'Saved region placement runtime check',
            category: 'Tools',
            kind: 'tool_app',
            icon: 'sheet',
            accent: '#4b6572',
            source: 'local',
            pinned: false,
            recentUsedAt: '2026-06-24T00:00:00.000Z',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'skill',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                skill: { id: 'region-placement-skill', inputMode: 'form', outputModes: ['json'], fields: [] },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    generated: true,
                    entry: 'tool_workspace',
                    layouts: {
                        tool_workspace: {
                            template: 'classic_split',
                            density: 'compact',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            regions: [
                                { id: 'file_queue', role: 'input', placement: 'center' },
                                { id: 'settings_panel', role: 'parameters', placement: 'left' },
                                { id: 'preview_panel', role: 'preview', placement: 'right' },
                                { id: 'output_panel', role: 'output', placement: 'bottom' },
                            ],
                            studio: { editable: true, savedInManifest: true },
                        },
                    },
                },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getAllByText('Region Placement Tool')[0]);

        await waitFor(() => expect(document.querySelector('.apps-runtime-layout')).not.toBeNull());
        const runtimeLayout = document.querySelector('.apps-runtime-layout') as HTMLElement;
        expect(runtimeLayout.dataset.primaryRegion).toBe('left');
        expect(runtimeLayout.dataset.outputRegion).toBe('right');
        expect(runtimeLayout.dataset.regionCount).toBe('4');
        expect(document.querySelector('.apps-runtime-input')?.getAttribute('data-region')).toBe('center');
        expect(document.querySelector('.apps-runtime-output')?.getAttribute('data-region')).toBe('bottom');
    });
    it('creates draft tool apps with multiple file input enabled', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getCreateAppNameInput(), { target: { value: '批量合同归档' } });
        fireEvent.click(screen.getByText('允许一次选择多个文件'));

        expect(screen.getByText(/"multipleFiles": true/)).not.toBeNull();

        clickCreateLocalApp();
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('批量合同归档')[0]);

        const fileInput = container.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        expect(fileInput.multiple).toBe(true);
    });

    it('adds structured fields to draft tool app manifests', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getCreateAppNameInput(), { target: { value: '合同归档' } });
        fireEvent.change(screen.getByDisplayValue('文件上传'), { target: { value: 'mixed' } });
        fireEvent.click(screen.getByText('添加字段'));
        const fieldEditor = document.querySelector('.apps-field-editor') as HTMLElement;
        fireEvent.change(within(fieldEditor).getByPlaceholderText('customer_id'), { target: { value: 'archive_no' } });
        fireEvent.change(within(fieldEditor).getByPlaceholderText('显示名'), { target: { value: '归档编号' } });
        fireEvent.click(within(fieldEditor).getByText('必填'));

        expect(screen.getAllByText(/"fields"/).length).toBeGreaterThan(0);
        expect(screen.getByText(/"archive_no"/)).not.toBeNull();
        expect(screen.getAllByText(/"required": true/).length).toBeGreaterThan(0);
    });

    it('generates an enterprise app draft from MIS-style prompts', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getDraftPromptInput(), {
            target: { value: '创建费用报销审批应用，录入发票和付款信息，生成财务报' },
        });
        fireEvent.click(screen.getByText('生成草稿'));

        expect(screen.getByDisplayValue('费用报销审批应用')).not.toBeNull();
        expect(screen.getByDisplayValue('财务')).not.toBeNull();
        expect(screen.getByText(/enterprise_approval_app/)).not.toBeNull();
        expect(screen.getByText(/agent_dynamic_ui/)).not.toBeNull();
    });

    it('shows and saves the visual result contract in App Studio drafts', () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        expect(within(screen.getByTestId('studio-result-contract')).getByText('Result contract')).not.toBeNull();
        expect(within(screen.getByTestId('studio-result-contract')).getAllByText('Artifact').length).toBeGreaterThan(0);
        expect(screen.getByText(/"resultContract"/)).not.toBeNull();
        expect(screen.getByText(/"schema": "maclaw.app.result.v1"/)).not.toBeNull();
        expect(screen.getAllByText(/"primary": "artifact"/).length).toBeGreaterThan(0);
        expect(screen.getByTestId('studio-test-protocol')).not.toBeNull();
        expect(screen.getByText(/"testProtocol"/)).not.toBeNull();
        expect(screen.getByText(/"schema": "maclaw.app.test_protocol.v1"/)).not.toBeNull();
        fireEvent.click(screen.getByRole('button', { name: /^Business app$|^企业普通应用?/ }));
        expect(within(screen.getByTestId('studio-result-contract')).getAllByText('Business status').length).toBeGreaterThan(0);
        fireEvent.click(screen.getByRole('button', { name: /^Approval app$|^企业审批型?/ }));
        expect(within(screen.getByTestId('studio-result-contract')).getAllByText('Approval result').length).toBeGreaterThan(0);
        expect(within(screen.getByTestId('studio-result-contract')).getByText(/approved \/ rejected/)).not.toBeNull();
    });

    it('saves visual result contract edits from App Studio', async () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(screen.getByPlaceholderText('Example: Contract filing'), { target: { value: 'Contract Console' } });
        fireEvent.change(screen.getByTestId('studio-result-primary'), { target: { value: 'content' } });
        fireEvent.click(screen.getByTestId('studio-result-delivery-artifacts'));
        fireEvent.change(screen.getByTestId('studio-test-risk'), { target: { value: 'high' } });
        fireEvent.change(screen.getByTestId('studio-test-roles'), { target: { value: 'operator, reviewer' } });
        fireEvent.click(screen.getByRole('button', { name: 'Create app' }));

        let created: any;
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            created = (stored.customApps || []).find((app: any) => app.name === 'Contract Console');
            expect(created).toBeTruthy();
        });
        expect(created.manifest.resultContract.primary).toBe('content');
        expect(created.manifest.resultContract.delivery.artifacts).toBe(false);
        expect(created.manifest.resultContract.delivery.inlineContent).toBe(true);
        expect(created.manifest.testProtocol.schema).toBe('maclaw.app.test_protocol.v1');
        expect(created.manifest.testProtocol.riskLevel).toBe('high');
        expect(created.manifest.testProtocol.requiredRoles).toEqual(['operator', 'reviewer']);
        expect(created.manifest.testProtocol.fingerprint).toMatch(/^[0-9a-f]{8}$/);
    });

    it('saves visual workspace region visibility and applies it in the runtime panel', async () => {
        const { container } = render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByRole('button', { name: /^Business app$|^企业普通应用?/ }));
        fireEvent.change(screen.getByPlaceholderText('Example: Contract filing'), { target: { value: 'Operations Desk' } });
        fireEvent.click(screen.getByTestId('studio-layout-region-visible-output_panel'));
        fireEvent.click(screen.getByRole('button', { name: 'Create app' }));

        let created: any;
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            created = (stored.customApps || []).find((app: any) => app.name === 'Operations Desk');
            expect(created).toBeTruthy();
        });
        const layout = created.manifest.ui.layouts.business_workspace;
        expect(layout.regions.find((region: any) => region.id === 'output_panel')).toMatchObject({ role: 'output', visible: false });

        const tile = Array.from(container.querySelectorAll<HTMLButtonElement>('.apps-app-tile')).find((item) => item.textContent?.includes('Operations Desk'));
        expect(tile).not.toBeNull();
        fireEvent.click(tile as HTMLButtonElement);
        await waitFor(() => expect(container.querySelector('.apps-business-workspace')).not.toBeNull());
        expect(container.querySelector('.apps-runtime-output')).toBeNull();
        expect(container.querySelector('.apps-run-history')).toBeNull();
    });

    it('moves workspace regions from the visual layout preview into saved manifest regions', async () => {
        const { container } = render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByRole('button', { name: /^Business app$|^企业普通应用?/ }));
        fireEvent.change(screen.getByPlaceholderText('Example: Contract filing'), { target: { value: 'Visual Layout Desk' } });
        fireEvent.change(screen.getByTestId('studio-layout-region-record_list'), { target: { value: 'right' } });
        fireEvent.change(screen.getByTestId('studio-layout-region-output_panel'), { target: { value: 'bottom' } });
        fireEvent.click(screen.getByRole('button', { name: 'Create app' }));

        let created: any;
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            created = (stored.customApps || []).find((app: any) => app.name === 'Visual Layout Desk');
            expect(created).toBeTruthy();
        });
        const layout = created.manifest.ui.layouts.business_workspace;
        expect(layout.regions).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'record_list', role: 'record_list', placement: 'right' }),
            expect.objectContaining({ id: 'output_panel', role: 'output', placement: 'bottom' }),
        ]));
        expect(layout.outputRegion).toBe('bottom');
        expect(layout.studio.updatedBy).toBe('app_studio');
        seedSuccessfulLocalAppRun(created, {
            resultPayload: { business_status: 'ready', business_record: { id: created.id }, content: 'ready' },
            outputs: [{ kind: 'content', title: 'Business status', body: 'ready', status: 'ready' }],
            resultCoverage: { coveredTypes: ['content', 'business_status'], missingTypes: [] },
        });

        const tile = Array.from(container.querySelectorAll<HTMLButtonElement>('.apps-app-tile')).find((item) => item.textContent?.includes('Visual Layout Desk'));
        expect(tile).not.toBeNull();
        fireEvent.click(tile as HTMLButtonElement);
        await waitFor(() => expect(container.querySelector('.apps-business-workspace')).not.toBeNull());
        expect(container.querySelector('.apps-business-workspace')?.getAttribute('data-region')).toBe('right');
        expect(container.querySelector('.apps-runtime-output')?.getAttribute('data-region')).toBe('bottom');

        cleanup();
        // The publish preview only lists apps whose authoritative dependency
        // check passes; echo the packaged bindings as installed/ready.
        planMaclawAppInstallMock.mockImplementation(async (packageJSON: string) => {
            const pkg = JSON.parse(String(packageJSON));
            const appID = String(pkg?.app?.id || '');
            const declared = [
                ...(pkg?.app?.binding?.appSkill?.id ? [{ id: pkg.app.binding.appSkill.id, kind: 'app_skill' }] : []),
                ...((pkg?.app?.binding?.dependencies?.skills || []).map((dep: any) => ({ id: dep.id, kind: dep.kind || 'runtime_skill' }))),
            ];
            const seen = new Set<string>();
            const dependencies = declared.filter((dep) => dep.id && !seen.has(dep.id) && seen.add(dep.id))
                .map((dep) => ({ ...dep, required: true, source: 'local', installed: true, health: 'ready', action: 'skip', app_ids: [appID] }));
            return { schema: 'maclaw.app.install_plan.v1', apps: [{ id: appID, name: pkg?.app?.name, kind: pkg?.app?.kind }], dependencies, dependency_count: dependencies.length, has_missing_required: false, has_blocking_dependency: false, has_workflow_contract_issue: false, has_governance_review_issue: false };
        });
        render(<AppsPage lang="en" />);
        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        let packagePreview: any;
        await waitFor(() => {
            packagePreview = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
            expect((packagePreview.apps || []).some((entry: any) => entry.app.name === 'Visual Layout Desk')).toBe(true);
        });
        const packagedApp = packagePreview.apps.find((entry: any) => entry.app.name === 'Visual Layout Desk');
        expect(packagedApp.app.ui.layouts.business_workspace).toMatchObject({
            template: 'classic_split',
            outputRegion: 'bottom',
            studio: expect.objectContaining({ savedInManifest: true, updatedBy: 'app_studio' }),
        });
        expect(packagedApp.app.ui.layouts.business_workspace.regions).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'record_list', role: 'record_list', placement: 'right' }),
            expect.objectContaining({ id: 'output_panel', role: 'output', placement: 'bottom' }),
        ]));
        expect(packagedApp.app.governance.workspaceLayout).toMatchObject({
            schema: 'maclaw.app.ui.v1',
            entry: 'business_workspace',
            outputRegion: 'bottom',
            regionCount: expect.any(Number),
            savedInManifest: true,
        });
        expect(packagedApp.app.governance.workspaceLayout.regions).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'record_list', role: 'record_list', placement: 'right' }),
            expect.objectContaining({ id: 'output_panel', role: 'output', placement: 'bottom' }),
        ]));
        expect(packagedApp.app.ui.layouts.business_workspace.fingerprint).toBe(packagedApp.app.governance.workspaceLayout.fingerprint);
        expect(packagedApp.app.binding.ui.layouts.business_workspace.fingerprint).toBe(packagedApp.app.governance.workspaceLayout.fingerprint);
        expect(packagedApp.app.governance.workspaceLayout.regions).toEqual(packagedApp.app.ui.layouts.business_workspace.regions);
        expect(packagedApp.app.governance.workspaceLayout.regions).toEqual(packagedApp.app.binding.ui.layouts.business_workspace.regions);
        expect(packagedApp.app.governance.workspaceLayout.regionIds).toEqual(packagedApp.app.ui.layouts.business_workspace.regions.map((region: any) => region.id));
    });

    it('copies the draft manifest preview to clipboard', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getCreateAppNameInput(), { target: { value: '合同归档' } });
        const copyButtons = screen.getAllByText('\u590d\u5236');
        fireEvent.click(copyButtons[0]);

        await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalled());
        expect(String((navigator.clipboard.writeText as any).mock.calls[0][0])).toContain('合同归档');
        expect(screen.getByText('已复制')).not.toBeNull();
    });

    it('resets the draft manifest copy state after the draft changes', async () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(screen.getByPlaceholderText('Example: Contract filing'), { target: { value: 'Contract filing' } });
        const preview = document.querySelector('.apps-create-preview') as HTMLElement;
        fireEvent.click(within(preview).getByText('Copy'));

        await waitFor(() => expect(within(preview).getByText('Copied')).not.toBeNull());
        fireEvent.change(screen.getByPlaceholderText('Example: Contract filing'), { target: { value: 'Contract archive' } });

        await waitFor(() => expect(within(preview).getByText('Copy')).not.toBeNull());
    });

    it('groups the create pane into collapsible sections, all open by default', () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        const sections = Array.from(document.querySelectorAll('.apps-create-form > details.apps-create-section')) as HTMLDetailsElement[];
        expect(sections.length).toBeGreaterThanOrEqual(4);
        expect(sections.every((section) => section.open)).toBe(true);
        // The sticky action bar stays a direct child of the form, outside the sections.
        expect(document.querySelector('.apps-create-form > .apps-actions')).not.toBeNull();

        fireEvent.click(sections[0].querySelector('summary') as HTMLElement);
        expect(sections[0].open).toBe(false);
    });

    it('cycles workspace region placement by clicking the region pill', () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByRole('button', { name: /^Business app$|^企业普通应用?/ }));
        expect((screen.getByTestId('studio-layout-region-record_list') as HTMLSelectElement).value).toBe('center');

        fireEvent.click(screen.getByTestId('studio-layout-region-record_list-cycle'));

        // The pill remounts into the target slot; assert on freshly queried nodes.
        expect((screen.getByTestId('studio-layout-region-record_list') as HTMLSelectElement).value).toBe('right');
        expect(screen.getByTestId('studio-layout-region-record_list-cycle').getAttribute('aria-label')).toContain('Placement: Right');
    });

    it('keeps the draft manifest preview target mounted when collapsed', () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        const preview = document.querySelector('.apps-create-preview') as HTMLElement;
        const toggle = within(preview).getByRole('button', { name: /Current draft manifest/ });
        const manifest = document.getElementById('apps-create-manifest-preview') as HTMLPreElement;

        // Collapsed by default so the sticky action bar stays in view.
        expect(toggle.getAttribute('aria-expanded')).toBe('false');
        expect(toggle.getAttribute('aria-controls')).toBe('apps-create-manifest-preview');
        expect(manifest.hidden).toBe(true);

        fireEvent.click(toggle);

        expect(toggle.getAttribute('aria-expanded')).toBe('true');
        expect(document.getElementById('apps-create-manifest-preview')).toBe(manifest);
        expect(manifest.hidden).toBe(false);
    });

    it('creates a local app entry from app studio', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getCreateAppNameInput(), { target: { value: '合同归档' } });
        clickCreateLocalApp();
        fireEvent.click(screen.getByText('\u5173\u95ed'));

        expect(screen.getAllByText('合同归档').length).toBeGreaterThan(0);
    });

    it('creates schema-safe ascii ids for local apps with Chinese names', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.change(getCreateAppNameInput(), { target: { value: '合同归档' } });
        clickCreateLocalApp();
        fireEvent.click(getManageTab());

        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('合同归档')) as HTMLElement;
        fireEvent.click(within(row).getByTitle(manageManifestTitle));
        const manifestText = document.querySelector('.apps-manage-manifest')?.textContent || '';
        const manifest = JSON.parse(manifestText);

        expect(manifest.app.id).toMatch(/^local-app-[a-z0-9]+-[a-z0-9]+-app$/);
        expect(manifest.app.id).toMatch(/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/);
        expect(manifest.app.source).toBe('local');
        expect(row.textContent).toContain('本地');
    });

    it('migrates legacy locally-created apps from Local/Market source to Local', () => {
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            customApps: [
                {
                    id: 'local-app-legacy-app',
                    name: '旧版本地应用',
                    description: 'Legacy local app',
                    category: '文档处理',
                    kind: 'tool_app',
                    icon: 'shield',
                    accent: '#28705f',
                    source: 'market',
                    manifest: {
                        schema: 'maclaw.app.v1',
                        installUnit: 'skill',
                        privateMarker: 'x_maclaw_apps',
                        entryKind: 'tool_app',
                        launchMode: 'fixed_skill_ui',
                        skill: { id: 'legacy-local', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file', outputModes: ['pdf'], fields: [] },
                    },
                },
            ],
        }));
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('旧版本地应用')) as HTMLElement;
        expect(row.textContent).toContain('本地');
        fireEvent.click(within(row).getByTitle(manageManifestTitle));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(manifest.app.source).toBe('local');
    });

    it('filters apps by category', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.change(screen.getByPlaceholderText('搜索应用'), { target: { value: '脱敏' } });
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('全部应用 (1)')).not.toBeNull();
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('文档处理 (1)')).not.toBeNull();
        expect((within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('财务审批 (0)') as HTMLOptionElement).disabled).toBe(true);
        expect(screen.getByText('搜索“脱敏” · 1 个匹配')).not.toBeNull();
        fireEvent.click(within(document.querySelector('.apps-filter-row') as HTMLElement).getByTitle('重置筛选'));

        fireEvent.change(document.querySelector('.apps-category-select') as HTMLSelectElement, { target: { value: '财务审批' } });
        fireEvent.change(screen.getByPlaceholderText('搜索应用'), { target: { value: '脱敏' } });
        expect((document.querySelector('.apps-category-select') as HTMLSelectElement).value).toBe('all');
        expect(screen.getAllByText('文档脱敏').length).toBeGreaterThan(0);
        fireEvent.click(within(document.querySelector('.apps-filter-row') as HTMLElement).getByTitle('重置筛选'));

        fireEvent.change(document.querySelector('.apps-category-select') as HTMLSelectElement, { target: { value: '文档处理' } });

        expect(screen.getByText('文档处理 · 2 个应用')).not.toBeNull();
        expect(screen.getAllByText('PDF 转 Word').length).toBeGreaterThan(0);
        expect(screen.getAllByText('文档脱敏').length).toBeGreaterThan(0);
        expect(screen.queryByText('采购入库')).toBeNull();

        fireEvent.click(within(document.querySelector('.apps-filter-row') as HTMLElement).getByTitle('重置筛选'));
        expect((document.querySelector('.apps-category-select') as HTMLSelectElement).value).toBe('all');
        expect((within(document.querySelector('.apps-filter-row') as HTMLElement).getByTitle('重置筛选') as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getAllByText('采购入库').length).toBeGreaterThan(0);
    });

    it('searches app bindings, source, type, and output modes', () => {
        render(<AppsPage lang="zh-Hans" />);

        const search = screen.getByPlaceholderText('搜索应用');
        fireEvent.change(search, { target: { value: 'finance.expense_upsert' } });
        expect(screen.getByText('搜索结果')).not.toBeNull();
        expect(Array.from(document.querySelectorAll('.apps-section__title')).map((item) => item.textContent)).not.toContain('常用应用');
        expect(screen.getAllByText('报销申请').length).toBeGreaterThan(0);
        expect(screen.queryByText('采购入库')).toBeNull();

        fireEvent.click(screen.getByTitle('清空搜索'));
        expect((search as HTMLInputElement).value).toBe('');
        expect(screen.getAllByText('常用应用').length).toBeGreaterThan(0);

        fireEvent.change(search, { target: { value: 'skill' } });
        expect(screen.getAllByText('PDF 转 Word').length).toBeGreaterThan(0);
        expect(screen.getAllByText('合同审查').length).toBeGreaterThan(0);

        fireEvent.keyDown(search, { key: 'Escape' });
        expect((search as HTMLInputElement).value).toBe('');

        fireEvent.change(search, { target: { value: 'xlsx' } });
        expect(screen.getAllByText('表格分析').length).toBeGreaterThan(0);
        fireEvent.click(within(document.querySelector('.apps-filter-row') as HTMLElement).getByTitle('重置筛选'));
        expect((search as HTMLInputElement).value).toBe('');
    });

    it('tracks recently used apps from opening and execution', async () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('采购入库')[0]);
        fireEvent.change(document.querySelector('.apps-category-select') as HTMLSelectElement, { target: { value: 'recent' } });

        expect(document.querySelectorAll('.apps-section__title').length).toBeGreaterThan(0);
        expect(Array.from(document.querySelectorAll('.apps-section__title')).map((item) => item.textContent)).not.toContain('常用应用');
        expect(screen.getAllByText('采购入库').length).toBeGreaterThan(0);
        expect(screen.queryByText('报销申请')).toBeNull();

        fireEvent.change(document.querySelector('.apps-category-select') as HTMLSelectElement, { target: { value: 'all' } });
        fireEvent.click(screen.getAllByText('\u62a5\u9500\u7533\u8bf7')[0]);
        fireEvent.click(within(document.querySelector('.apps-runtime-actions') as HTMLElement).getByText('\u6267\u884c'));
        fireEvent.change(document.querySelector('.apps-category-select') as HTMLSelectElement, { target: { value: 'recent' } });

        const recentTiles = Array.from(document.querySelectorAll('.apps-section:last-child .apps-app-name')).map((item) => item.textContent || '');
        expect(recentTiles).toContain('报销申请');
        expect(recentTiles).toContain('采购入库');

        // Recency persists asynchronously (SQLite-gated write).
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            expect(stored.recentUsedAtById?.expense).toBeTruthy();
            expect(stored.recentUsedAtById?.['purchase-inbound']).toBeTruthy();
        });
        unmount();
        render(<AppsPage lang="zh-Hans" />);
        fireEvent.change(document.querySelector('.apps-category-select') as HTMLSelectElement, { target: { value: 'recent' } });
        expect(screen.getAllByText('报销申请').length).toBeGreaterThan(0);
        expect(screen.getAllByText('采购入库').length).toBeGreaterThan(0);
    });

    it('opens clicked apps as runtime tabs on the right', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.click(screen.getAllByText('采购入库')[0]);

        const tabs = container.querySelector('.apps-runtime-tabs') as HTMLElement;
        expect(tabs).not.toBeNull();
        expect(within(tabs).getByText('报销申请')).not.toBeNull();
        expect(within(tabs).getByText('采购入库')).not.toBeNull();
        expect(container.querySelectorAll('.apps-runtime-tab').length).toBe(2);
        expect(container.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('采购入库');
    });

    it('exposes runtime tabs as accessible tabs with a labelled panel', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.click(screen.getAllByText('采购入库')[0]);

        const expenseTab = screen.getByRole('tab', { name: '报销申请' });
        const purchaseTab = screen.getByRole('tab', { name: '采购入库' });
        const panel = screen.getByRole('tabpanel');
        const closePurchase = screen.getByRole('button', { name: '关闭 采购入库' });

        expect(expenseTab.getAttribute('aria-selected')).toBe('false');
        expect(purchaseTab.getAttribute('aria-selected')).toBe('true');
        expect(panel.getAttribute('id')).toBe(purchaseTab.getAttribute('aria-controls'));
        expect(panel.getAttribute('aria-labelledby')).toBe(purchaseTab.id);
        expect(closePurchase.closest('[role="tab"]')).toBeNull();
    });

    it('moves between runtime tabs from the keyboard', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.click(screen.getAllByText('采购入库')[0]);

        fireEvent.keyDown(screen.getByRole('tab', { name: '采购入库' }), { key: 'ArrowLeft' });

        expect(screen.getByRole('tab', { name: '报销申请' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe(screen.getByRole('tab', { name: '报销申请' }).id);
    });

    it('records and completes an approval instance when running an approval app', async () => {
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-test-1',
            status: 'success',
            summary: {
                last_output_snippet: JSON.stringify({
                    approval_result: 'approved',
                    business_status: 'pending_payment',
                    result_status: 'approved',
                    business_record: { id: 'exp-1', status: 'pending_payment' },
                    approval_instance: {
                        record_id: 'exp-1',
                        current_node: 'expense.result_feedback',
                        business_status: 'pending_payment',
                        result_status: 'approved',
                    },
                    outputs: [{ kind: 'notification', title: 'Nested notice', text: 'Finance notified', status: 'ready' }],
                    artifacts: [{ id: 'artifact-nested', uri: 'artifact://skill-run/run-test-1/artifact-nested', name: 'nested-result.zip', status: 'ready' }],
                    text: 'approved with note',
                }),
            },
            outputs: [
                { id: 'record-1', kind: 'business_record', title: 'Expense record', text: JSON.stringify({ id: 'exp-1', status: 'pending_payment' }), status: 'ready' },
                { id: 'artifact-1', kind: 'artifact', title: 'Approval PDF', artifact_id: 'artifact-1', artifact: { id: 'artifact-1', uri: 'artifact://skill-run/run-test-1/artifact-1', name: 'approval.pdf', status: 'ready' } },
            ],
            artifacts: [{ id: 'artifact-1', uri: 'artifact://skill-run/run-test-1/artifact-1', name: 'approval.pdf', status: 'ready' }],
        });
        syncMaclawAppApprovalInstanceToDataSrvMock
            .mockResolvedValueOnce({ synced: true, approval_id: 'approval-remote-submit', dataset_id: 'finance.expenses' })
            .mockResolvedValueOnce({ synced: true, approval_id: 'approval-remote-final', record_id: 'exp-1', dataset_id: 'finance.expenses' });
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('\u62a5\u9500\u7533\u8bf7')[0]);
        fireEvent.click(screen.getByText('\u6267\u884c'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('expense-approval-workflow', expect.objectContaining({
            _maclaw_app: true,
            app_id: 'expense',
            app_kind: 'enterprise_approval_app',
            approval_workflow_skill_id: 'expense-approval-workflow',
            approval_workflow_id: 'expense.submitted',
            approval_workflow_version: '1.0.0',
            workflow_version: '1.0.0',
            approval_object_role: 'expense_report',
            submitted_by: '\u5f53\u524d\u7528\u6237',
            current_assignee: '\u5ba1\u6279\u4eba',
            current_assignee_type: 'user',
            from_status: 'submitted',
            to_status: 'approval_pending',
            object_role: 'expense_report',
            dataset_id: 'finance.expenses',
            datasrv_domain: 'finance',
        })));
        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock.mock.calls.some((call) => call[0].status === 'approved')).toBe(true));
        const pendingPayload = recordMaclawAppApprovalInstanceMock.mock.calls[0][0];
        expect(pendingPayload.app_id).toBe('expense');
        expect(pendingPayload.status).toBe('pending');
        expect(pendingPayload.workflow_skill_id).toBe('expense-approval-workflow');
        expect(pendingPayload.approval_workflow_id).toBe('expense.submitted');
        expect(pendingPayload.workflow_version).toBe('1.0.0');
        expect(pendingPayload.workflow_decision_id).toBe('run-test-1');
        expect(pendingPayload.dataset_id).toBe('finance.expenses');
        expect(pendingPayload.object_role).toBe('expense_report');
        expect(pendingPayload.approval_object_role).toBe('expense_report');
        expect(pendingPayload.submitted_by).toBe('\u5f53\u524d\u7528\u6237');
        expect(pendingPayload.current_assignee).toBe('\u5ba1\u6279\u4eba');
        expect(pendingPayload.current_assignee_type).toBe('user');
        expect(pendingPayload.from_status).toBe('submitted');
        expect(pendingPayload.to_status).toBe('approval_pending');
        expect(pendingPayload.detail_url).toBe('skill-run://run-test-1');
        const completedPayload = recordMaclawAppApprovalInstanceMock.mock.calls.map((call) => call[0]).find((payload) => payload.status === 'approved');
        expect(completedPayload.instance_id).toBe('appr-test-1');
        expect(completedPayload.status).toBe('approved');
        expect(completedPayload.lane).toBe('handled');
        expect(completedPayload.workflow_decision_id).toBe('run-test-1');
        expect(completedPayload.workflow_version).toBe('1.0.0');
        expect(completedPayload.business_status).toBe('pending_payment');
        expect(completedPayload.result_status).toBe('approved');
        expect(completedPayload.record_id).toBe('exp-1');
        expect(completedPayload.dataset_id).toBe('finance.expenses');
        expect(completedPayload.object_role).toBe('expense_report');
        expect(completedPayload.current_node).toBe('expense.result_feedback');
        expect(completedPayload.result_payload.business_record).toEqual({ id: 'exp-1', status: 'pending_payment' });
        expect(completedPayload.outputs).toHaveLength(3);
        expect(completedPayload.outputs).toEqual(expect.arrayContaining([expect.objectContaining({ title: 'Nested notice', text: 'Finance notified' })]));
        expect(completedPayload.artifacts[0].name).toBe('approval.pdf');
        expect(completedPayload.artifacts).toEqual(expect.arrayContaining([expect.objectContaining({ name: 'nested-result.zip' })]));
        expect(completedPayload.events.map((event: any) => event.action)).toContain('workflow_completed');
        await waitFor(() => {
            const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
            expect(history.expense?.[0]?.approvalInstance).toMatchObject({
                instanceId: 'appr-test-1',
                status: 'approved',
                currentNode: 'expense.result_feedback',
                approvalID: 'approval-remote-final',
                datasetID: 'finance.expenses',
                objectRole: 'expense_report',
                approvalObjectRole: 'expense_report',
                approvalEvent: 'expense.submitted',
                approvalWorkflowID: 'expense.submitted',
                workflowSkillId: 'expense-approval-workflow',
                workflowVersion: '1.0.0',
                businessStatus: 'pending_payment',
                resultStatus: 'approved',
                recordID: 'exp-1',
                detailURL: 'skill-run://run-test-1',
                resultPayload: expect.objectContaining({ business_record: { id: 'exp-1', status: 'pending_payment' } }),
                outputs: expect.arrayContaining([expect.objectContaining({ title: 'Expense record' })]),
                artifacts: expect.arrayContaining([expect.objectContaining({ name: 'approval.pdf' })]),
                approvalInstanceViewVerified: true,
            });
        });
        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalledTimes(2));
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].dataset_id).toBe('finance.expenses');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].object_role).toBe('expense_report');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].record_id).toMatch(/^appr-/);
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.record_id).toBe(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].record_id);
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.workflow_version).toBe('1.0.0');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.approval_workflow_id).toBe('expense.submitted');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.current_assignee).toBe('\u5ba1\u6279\u4eba');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.current_assignee_type).toBe('user');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.from_status).toBe('submitted');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.to_status).toBe('approval_pending');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.current_node_ids).toEqual([syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.current_node]);
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.workflow_node_ids).toEqual([syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.current_node]);
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].record_id).toBe('exp-1');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].instance.status).toBe('approved');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].instance.current_node_ids).toEqual([syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].instance.current_node]);
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].instance.workflow_node_ids).toEqual([syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].instance.current_node]);
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].instance.result_payload.business_record.id).toBe('exp-1');

        listMaclawAppApprovalInstancesAllMock.mockResolvedValue([completedPayload]);
        fireEvent.click(within(document.querySelector('.apps-ops') as HTMLElement).getByText('\u5ba1\u6279\u72b6\u6001'));
        await waitFor(() => expect(listMaclawAppApprovalInstancesAllMock).toHaveBeenCalledWith('all', 200));
        expect(screen.getByText('\u5ba1\u6279\u5b9e\u4f8b\u7ba1\u7406')).not.toBeNull();
        await waitFor(() => expect(screen.getByText('\u7ed3\u679c\u5305')).not.toBeNull());
        expect(screen.getByText('\u7ed3\u679c\u53cd\u9988')).not.toBeNull();
        expect(screen.getAllByText('Expense record').length).toBeGreaterThan(0);
        expect(screen.getAllByText('approval.pdf').length).toBeGreaterThan(0);
        expect(screen.getAllByText(/pending_payment/).length).toBeGreaterThan(0);
        expect(screen.getAllByText('approved with note').length).toBeGreaterThan(0);

    });
    it('publishes approval app evidence from an actual workflow run', async () => {
        const app = {
            id: 'approval-run-publish-app',
            name: 'Approval Run Publish',
            description: 'Approval app actual run evidence enters publish package',
            category: 'Finance',
            kind: 'enterprise_approval_app',
            icon: 'receipt',
            accent: '#2f5f98',
            pinned: false,
            source: 'local',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_approval_app',
                launchMode: 'agent_dynamic_ui',
                appSkill: { id: 'approval-run-super-skill', version: '1.0.0', source: 'hub' },
                dependencies: {
                    skills: [
                        { id: 'approval-run-super-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', install_ref: 'cap-approval-run-super-skill' },
                        { id: 'approval-run-workflow', version: '2.0.0', kind: 'workflow_skill', required: true, source: 'hub', install_ref: 'cap-approval-run-workflow', capabilities: ['approval.workflow'] },
                    ],
                },
                datasrv: { domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report', preferredAction: 'finance.expense_upsert', preferredView: 'finance.expense_review' },
                mis: { approvalBindings: [{ event: 'finance.expense.submitted', workflowSkillId: 'approval-run-workflow', workflowVersion: '2.0.0', objectRole: 'expense_report' }] },
                workflow: {
                    schema: 'maclaw.app.workflow.v1',
                    submitNode: 'expense.submit',
                    approvalNode: 'expense.manager_review',
                    resultNode: 'expense.result_feedback',
                    attentionNode: 'expense.attention',
                    statusMapping: { pending: 'approval_pending', approved: 'finance_approved', rejected: 'finance_rejected', attention: 'finance_attention', requiresInput: 'finance_more_input' },
                },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'approval_workspace',
                    layouts: {
                        approval_workspace: {
                            template: 'classic_split',
                            density: 'compact',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            navigation: ['my_requests', 'pending_my_approval', 'handled', 'attention'],
                            regions: [
                                { id: 'request_form', role: 'input', placement: 'left' },
                                { id: 'approval_inbox', role: 'instance_list', placement: 'center' },
                                { id: 'approval_detail', role: 'detail', placement: 'center' },
                                { id: 'result_panel', role: 'output', placement: 'right' },
                            ],
                            studio: { savedInManifest: true },
                        },
                    },
                },
                resultContract: {
                    schema: 'maclaw.app.result.v1',
                    primary: 'approval_result',
                    types: ['approval_result', 'business_status', 'business_record', 'document', 'notification'],
                    delivery: { inlineContent: true, artifacts: true, businessRecord: true, notifications: true },
                },
            },
        };
        const dependencyPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [
                { id: 'approval-run-super-skill', version: '1.0.0', kind: 'app_skill', required: true, source: 'hub', install_ref: 'cap-approval-run-super-skill', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] },
                { id: 'approval-run-workflow', version: '2.0.0', kind: 'workflow_skill', required: true, source: 'hub', install_ref: 'cap-approval-run-workflow', installed: true, health: 'ready', action: 'skip', app_ids: [app.id], capabilities: ['approval.workflow'] },
            ],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_workflow_contract_issue: false,
            has_governance_review_issue: false,
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        planMaclawAppInstallMock.mockResolvedValue(dependencyPlan);
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-approval-publish-1',
            status: 'success',
            summary: {
                last_output_snippet: JSON.stringify({
                    approval_result: 'approved',
                    business_status: 'finance_approved',
                    result_status: 'approved',
                    business_record: { id: 'EXP-PUBLISH-1', status: 'finance_approved' },
                    approval_instance: {
                        record_id: 'EXP-PUBLISH-1',
                        current_node: 'expense.result_feedback',
                        business_status: 'finance_approved',
                        result_status: 'approved',
                    },
                    outputs: [{ kind: 'notification', title: 'Approval notice', text: 'Finance notified', status: 'ready' }],
                    artifacts: [{ id: 'approval-pack', uri: 'artifact://approval/pack.zip', name: 'approval-pack.zip', status: 'ready' }],
                    text: 'approval complete',
                }),
            },
            outputs: [
                { id: 'approval-record', kind: 'business_record', title: 'Approved expense', text: 'EXP-PUBLISH-1', status: 'ready', data: { id: 'EXP-PUBLISH-1', status: 'finance_approved' } },
                { id: 'approval-document', kind: 'document', title: 'Approval PDF', artifact_id: 'approval-pdf', status: 'ready' },
            ],
            artifacts: [{ id: 'approval-pdf', uri: 'artifact://approval/approval.pdf', name: 'approval.pdf', status: 'ready', mime_type: 'application/pdf', size_bytes: 8192 }],
        });
        recordMaclawAppApprovalInstanceMock.mockImplementation(async (payload) => ({ ...payload, instance_id: payload.instance_id || 'approval-publish-local', updated_at: '2026-06-28T02:00:00Z' }));
        syncMaclawAppApprovalInstanceToDataSrvMock
            .mockResolvedValueOnce({ synced: true, approval_id: 'approval-publish-remote-pending', dataset_id: 'finance.expenses' })
            .mockResolvedValueOnce({ synced: true, approval_id: 'approval-publish-remote-final', record_id: 'EXP-PUBLISH-1', dataset_id: 'finance.expenses' });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'approval-run-publish-submission', submitted_at: '2026-06-28T02:10:00Z', status: 'submitted', message: 'queued' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getAllByText('Approval Run Publish')[0]);
        fireEvent.click(screen.getByText('Run'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('approval-run-workflow', expect.objectContaining({
            app_id: app.id,
            app_kind: 'enterprise_approval_app',
            approval_workflow_skill_id: 'approval-run-workflow',
            approval_workflow_version: '2.0.0',
            approval_event: 'finance.expense.submitted',
            object_role: 'expense_report',
            dataset_id: 'finance.expenses',
        })));
        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock.mock.calls.some((call) => call[0].status === 'approved')).toBe(true));
        await waitFor(() => {
            const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
            expect(history[app.id]?.[0]).toMatchObject({
                status: 'done',
                definitionHash: expect.stringMatching(/^[0-9a-f]{8}$/),
                dependencyVerification: {
                    schema: 'maclaw.app.install_plan.v1',
                    dependencyCount: 2,
                    hasBlockingDependency: false,
                    dependencies: expect.arrayContaining([
                        expect.objectContaining({ id: 'approval-run-super-skill', kind: 'app_skill', app_ids: [app.id] }),
                        expect.objectContaining({ id: 'approval-run-workflow', kind: 'workflow_skill', app_ids: [app.id] }),
                    ]),
                },
                approvalInstance: expect.objectContaining({
                    status: 'approved',
                    approvalID: 'approval-publish-remote-final',
                    workflowSkillId: 'approval-run-workflow',
                    workflowVersion: '2.0.0',
                    approvalEvent: 'finance.expense.submitted',
                    businessStatus: 'finance_approved',
                    resultStatus: 'approved',
                    recordID: 'EXP-PUBLISH-1',
                    resultPayload: expect.objectContaining({
                        approval_result: 'approved',
                        business_status: 'finance_approved',
                        business_record: { id: 'EXP-PUBLISH-1', status: 'finance_approved' },
                    }),
                    outputs: expect.arrayContaining([expect.objectContaining({ title: 'Approved expense' })]),
                    artifacts: expect.arrayContaining([expect.objectContaining({ name: 'approval.pdf' })]),
                    progressInstances: expect.arrayContaining([expect.objectContaining({
                        currentNode: 'expense.manager_review',
                        resultStatus: 'running',
                        outputs: expect.arrayContaining([expect.objectContaining({ title: 'Workflow Progress' })]),
                    })]),
                    approvalInstanceViewVerified: true,
                }),
            });
        });

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Approval Run Publish')) as HTMLElement;
        expect(card).toBeTruthy();
        await waitFor(() => expect(within(card).getByText('Ready to submit')).not.toBeNull());
        await waitFor(() => expect(within(card).getByText('One-click publish')).not.toBeNull());
        fireEvent.click(within(card).getByText('One-click publish'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        const governance = payload.apps[0].app.governance;
        expect(governance.dependencyVerification).toMatchObject({
            schema: 'maclaw.app.install_plan.v1',
            dependencyCount: 2,
            hasBlockingDependency: false,
            dependencies: expect.arrayContaining([
                expect.objectContaining({ id: 'approval-run-super-skill', install_ref: 'cap-approval-run-super-skill' }),
                expect.objectContaining({ id: 'approval-run-workflow', install_ref: 'cap-approval-run-workflow' }),
            ]),
        });
        expect(governance.testEvidence.approvalInstance).toMatchObject({
            status: 'approved',
            approvalID: 'approval-publish-remote-final',
            workflowSkillId: 'approval-run-workflow',
            workflowVersion: '2.0.0',
            approvalEvent: 'finance.expense.submitted',
            businessStatus: 'finance_approved',
            resultStatus: 'approved',
            recordID: 'EXP-PUBLISH-1',
            approvalInstanceViewVerified: true,
            resultPayload: expect.objectContaining({
                approval_result: 'approved',
                business_status: 'finance_approved',
                business_record: { id: 'EXP-PUBLISH-1', status: 'finance_approved' },
            }),
            outputs: expect.arrayContaining([expect.objectContaining({ title: 'Approved expense' })]),
            artifacts: expect.arrayContaining([expect.objectContaining({ name: 'approval.pdf' })]),
            progressInstances: expect.arrayContaining([expect.objectContaining({
                currentNode: 'expense.manager_review',
                resultStatus: 'running',
                outputs: expect.arrayContaining([expect.objectContaining({ title: 'Workflow Progress' })]),
            })]),
        });
        expect(governance.testEvidence.resultPayload).toEqual(expect.objectContaining({
            approval_result: 'approved',
            business_status: 'finance_approved',
            business_record: { id: 'EXP-PUBLISH-1', status: 'finance_approved' },
        }));
        expect(governance.testEvidence.resultCoverage).toEqual(expect.objectContaining({
            ok: true,
            primary: 'approval_result',
            coveredTypes: expect.arrayContaining(['approval_result', 'business_status', 'business_record', 'document']),
            missingTypes: [],
        }));
    });
    it('uses approvalBindings as the runtime workflow source for approval apps', async () => {
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            customApps: [{
                id: 'bound-approval',
                name: 'Bound Approval',
                description: 'Approval app with explicit MIS binding',
                category: 'Finance',
                kind: 'enterprise_approval_app',
                icon: 'receipt',
                accent: '#2f5f98',
                version: 1,
                source: 'local',
                manifest: {
                    schema: 'maclaw.app.v1',
                    installUnit: 'enterprise_app_pack',
                    privateMarker: 'x_maclaw_apps',
                    entryKind: 'enterprise_approval_app',
                    launchMode: 'agent_dynamic_ui',
                    datasrv: { domain: 'finance', datasetID: 'finance.expenses' },
                    mis: { approvalBindings: [{ event: 'finance.submitted', workflowSkillId: 'binding-workflow', workflowVersion: '3.0.0', objectRole: 'expense' }] },
                    ui: { schema: 'maclaw.app.ui.v1' },
                },
            }],
        }));

        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getAllByText('Bound Approval')[0]);
        fireEvent.click(screen.getByRole('button', { name: 'Run' }));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('binding-workflow', expect.objectContaining({
            approval_event: 'finance.submitted',
            approval_workflow_version: '3.0.0',
            workflow_version: '3.0.0',
            approval_object_role: 'expense',
            business_entity: 'Finance',
            dataset_id: 'finance.expenses',
            datasrv_domain: 'finance',
        })));
        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalled());
        const payload = recordMaclawAppApprovalInstanceMock.mock.calls[0][0];
        expect(payload.workflow_skill_id).toBe('binding-workflow');
        expect(payload.workflow_version).toBe('3.0.0');
        expect(payload.approval_event).toBe('finance.submitted');
        expect(payload.approval_object_role).toBe('expense');
        expect(payload.object_role).toBe('expense');
        expect(payload.dataset_id).toBe('finance.expenses');
        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalled());
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].dataset_id).toBe('finance.expenses');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].object_role).toBe('expense');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].app_id).toBe('bound-approval');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].instance.workflow_version).toBe('3.0.0');
    });

    it('passes workflow node mapping into approval skill runs and approval instances', async () => {
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            customApps: [{
                id: 'mapped-approval-runtime',
                name: 'Mapped Approval Runtime',
                description: 'Approval app with custom workflow nodes',
                category: 'Finance',
                kind: 'enterprise_approval_app',
                icon: 'receipt',
                accent: '#2f5f98',
                version: 1,
                source: 'local',
                manifest: {
                    schema: 'maclaw.app.v1',
                    installUnit: 'enterprise_app_pack',
                    privateMarker: 'x_maclaw_apps',
                    entryKind: 'enterprise_approval_app',
                    launchMode: 'agent_dynamic_ui',
                    datasrv: { domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report' },
                    mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'mapped-flow', workflowVersion: '1.0.0', objectRole: 'expense_report' }] },
                    workflow: { schema: 'maclaw.app.workflow.v1', submitNode: 'expense.intake', approvalNode: 'finance.director_review', resultNode: 'expense.result_pack', attentionNode: 'expense.attention', statusMapping: { pending: 'finance_pending', approved: 'finance_approved', rejected: 'finance_rejected', attention: 'finance_attention', requiresInput: 'finance_more_input' } },
                    ui: { schema: 'maclaw.app.ui.v1' },
                },
            }],
        }));
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getAllByText('Mapped Approval Runtime')[0]);
        fireEvent.click(screen.getByRole('button', { name: 'Run' }));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('mapped-flow', expect.objectContaining({
            current_node: 'finance.director_review',
            workflow_mapping: expect.objectContaining({ approvalNode: 'finance.director_review', resultNode: 'expense.result_pack' }),
            workflow_status_mapping: expect.objectContaining({ approved: 'finance_approved' }),
        })));
        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalled());
        const pendingPayload = recordMaclawAppApprovalInstanceMock.mock.calls[0][0];
        expect(pendingPayload.current_node).toBe('finance.director_review');
        expect(pendingPayload.events.map((event: any) => event.node)).toEqual(expect.arrayContaining(['expense.intake', 'finance.director_review']));
    });

    it('records approval workflow launch failures as failed instances', async () => {
        runNLSkillAsyncMock.mockRejectedValueOnce(new Error('workflow unavailable'));
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('\u62a5\u9500\u7533\u8bf7')[0]);
        fireEvent.click(screen.getByText('\u6267\u884c'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('expense-approval-workflow', expect.any(Object)));
        await waitFor(() => expect(screen.getByText('workflow unavailable')).not.toBeNull());
        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock.mock.calls.some((call) => call[0].status === 'failed')).toBe(true));
        const failedPayload = recordMaclawAppApprovalInstanceMock.mock.calls.map((call) => call[0]).find((payload) => payload.status === 'failed');
        expect(failedPayload.lane).toBe('handled');
        expect(failedPayload.result).toBe('workflow unavailable');
        expect(failedPayload.business_status).toBe('workflow_error');
        expect(failedPayload.result_status).toBe('failed');
        expect(failedPayload.result_payload).toEqual(expect.objectContaining({ approval_result: 'failed', workflow_lifecycle: 'error', text: 'workflow unavailable' }));
        expect(failedPayload.outputs).toEqual(expect.arrayContaining([expect.objectContaining({ kind: 'approval_result', text: 'workflow unavailable', status: 'failed' })]));
        expect(failedPayload.events.map((event: any) => event.action)).toContain('workflow_failed');
        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalled());

        const workspace = document.querySelector('.apps-approval-workspace') as HTMLElement;
        fireEvent.click(within(workspace).getByText('\u5df2\u5904\u7406'));
        await waitFor(() => expect(within(workspace).getAllByText('workflow unavailable').length).toBeGreaterThan(0));
    });
    it('records DataSrv start failures as failed approval instances in the runtime workspace', async () => {
        startMaclawAppApprovalWorkflowMock.mockRejectedValueOnce(new Error('DataSrv offline while syncing approval'));
        syncMaclawAppApprovalInstanceToDataSrvMock.mockRejectedValueOnce(new Error('DataSrv offline while syncing approval'));
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByRole('button', { name: /报销申请/ }));
        fireEvent.click(screen.getByText('Run'));

        await waitFor(() => expect(startMaclawAppApprovalWorkflowMock).toHaveBeenCalled());
        await waitFor(() => expect(screen.getByText('DataSrv offline while syncing approval')).not.toBeNull());
        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock.mock.calls.some((call) => call[0].status === 'failed')).toBe(true));
        const failedPayload = recordMaclawAppApprovalInstanceMock.mock.calls.map((call) => call[0]).find((payload) => payload.status === 'failed');
        expect(failedPayload).toMatchObject({
            app_id: 'expense',
            lane: 'handled',
            status: 'failed',
            result: 'DataSrv offline while syncing approval',
            business_status: 'workflow_error',
            result_status: 'failed',
        });
        expect(failedPayload.result_payload).toEqual(expect.objectContaining({ approval_result: 'failed', workflow_lifecycle: 'error' }));
        expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalled();

        const workspace = document.querySelector('.apps-approval-workspace') as HTMLElement;
        fireEvent.click(within(workspace).getByText('Handled'));
        await waitFor(() => expect(within(workspace).getAllByText('DataSrv offline while syncing approval').length).toBeGreaterThan(0));
    });
    it('shows timeout and cancelled workflow results in the handled approval workspace', async () => {
        const finalInstance = {
            instance_id: 'wf-timeout-ui-1',
            app_id: 'expense',
            app_name: '报销申请',
            approval_id: 'approval-timeout-ui-1',
            record_approval_id: 'approval-timeout-ui-1',
            record_id: 'expense-timeout-ui-1',
            dataset_id: 'finance.expenses',
            object_role: 'expense_report',
            title: 'Expense approval timed out',
            lane: 'handled',
            status: 'timeout',
            current_node: 'Timed out',
            current_node_ids: ['expense.submit', 'Timed out'],
            workflow_node_ids: ['expense.submit', 'Timed out'],
            applicant: 'Current user',
            approver: 'Manager',
            current_assignee: 'system',
            current_assignee_type: 'system',
            workflow_skill_id: 'expense-approval-workflow',
            workflow_version: '1.0.0',
            result: 'Workflow timed out waiting for approval',
            business_status: 'timeout',
            result_status: 'timeout',
            result_payload: { approval_result: 'attention', result_status: 'timeout', workflow_lifecycle: 'timeout', text: 'Workflow timed out waiting for approval' },
            outputs: [{ kind: 'approval_result', title: 'Approval result', text: 'Workflow timed out waiting for approval', status: 'timeout' }],
            events: [{ action: 'workflow_timeout', message: 'Workflow timed out waiting for approval' }],
            updated_at: '2026-06-30T12:00:00Z',
        };
        startMaclawAppApprovalWorkflowMock.mockResolvedValueOnce({
            started: true,
            approval_id: 'approval-timeout-ui-1',
            workflow_run: { ran: true, workflow_skill_id: 'expense-approval-workflow', instance: finalInstance, progress_instances: [] },
        });
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByRole('button', { name: /报销申请/ }));
        fireEvent.click(screen.getByText('Run'));

        await waitFor(() => expect(startMaclawAppApprovalWorkflowMock).toHaveBeenCalled());
        const workspace = document.querySelector('.apps-approval-workspace') as HTMLElement;
        fireEvent.click(within(workspace).getByText('Handled'));
        await waitFor(() => expect(within(workspace).getAllByText('Expense approval timed out').length).toBeGreaterThan(0));
        expect(within(workspace).getAllByText('Timed out').length).toBeGreaterThan(0);
        expect(within(workspace).getAllByText('Workflow timed out waiting for approval').length).toBeGreaterThan(0);
        expect(within(workspace).getByText('workflow_timeout')).not.toBeNull();
        expect(document.querySelector('.apps-result-panel[data-state="error"]')).not.toBeNull();
        expect(document.querySelector('.apps-runtime-output')?.hasAttribute('hidden')).toBe(true);
        const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, Array<{ runID: string; status: string; artifacts?: unknown[] }>>;
        const entry = history.expense?.find((item) => item.runID === 'approval-timeout-ui-1');
        expect(entry?.status).toBe('error');
        expect(entry?.artifacts).toBeUndefined();
    });

    it('shows cancelled workflow results in the handled approval workspace', async () => {
        const finalInstance = {
            instance_id: 'wf-cancel-ui-1',
            app_id: 'expense',
            app_name: '报销申请',
            approval_id: 'approval-cancel-ui-1',
            record_approval_id: 'approval-cancel-ui-1',
            record_id: 'expense-cancel-ui-1',
            dataset_id: 'finance.expenses',
            object_role: 'expense_report',
            title: 'Expense approval cancelled',
            lane: 'handled',
            status: 'cancelled',
            current_node: 'Cancelled',
            current_node_ids: ['expense.submit', 'Cancelled'],
            workflow_node_ids: ['expense.submit', 'Cancelled'],
            applicant: 'Current user',
            approver: 'Manager',
            current_assignee: 'system',
            current_assignee_type: 'system',
            workflow_skill_id: 'expense-approval-workflow',
            workflow_version: '1.0.0',
            result: 'Requester cancelled the workflow',
            business_status: 'cancelled',
            result_status: 'cancelled',
            result_payload: { approval_result: 'attention', result_status: 'cancelled', workflow_lifecycle: 'cancelled', text: 'Requester cancelled the workflow' },
            outputs: [{ kind: 'approval_result', title: 'Approval result', text: 'Requester cancelled the workflow', status: 'cancelled' }],
            events: [{ action: 'workflow_cancelled', message: 'Requester cancelled the workflow' }],
            updated_at: '2026-06-30T12:05:00Z',
        };
        startMaclawAppApprovalWorkflowMock.mockResolvedValueOnce({
            started: true,
            approval_id: 'approval-cancel-ui-1',
            workflow_run: { ran: true, workflow_skill_id: 'expense-approval-workflow', instance: finalInstance, progress_instances: [] },
        });
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByRole('button', { name: /报销申请/ }));
        fireEvent.click(screen.getByText('Run'));

        await waitFor(() => expect(startMaclawAppApprovalWorkflowMock).toHaveBeenCalled());
        const workspace = document.querySelector('.apps-approval-workspace') as HTMLElement;
        fireEvent.click(within(workspace).getByText('Handled'));
        await waitFor(() => expect(within(workspace).getAllByText('Expense approval cancelled').length).toBeGreaterThan(0));
        expect(within(workspace).getAllByText('Cancelled').length).toBeGreaterThan(0);
        expect(within(workspace).getAllByText('Requester cancelled the workflow').length).toBeGreaterThan(0);
        expect(within(workspace).getByText('workflow_cancelled')).not.toBeNull();
    });
    it('marks approval workflow failures as attention results', async () => {
        getNLSkillRunStatusMock.mockResolvedValueOnce({
            run_id: 'run-test-1',
            status: 'failed',
            error: 'policy engine failed',
            summary: { last_error_snippet: 'policy engine failed' },
        });
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('\u62a5\u9500\u7533\u8bf7')[0]);
        fireEvent.click(screen.getByText('\u6267\u884c'));

	        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock.mock.calls.some((call) => call[0].status === 'attention')).toBe(true));
	        const failedPayload = recordMaclawAppApprovalInstanceMock.mock.calls.map((call) => call[0]).find((payload) => payload.status === 'attention');
	        expect(failedPayload.instance_id).toBe('appr-test-1');
        expect(failedPayload.status).toBe('attention');
        expect(failedPayload.lane).toBe('attention');
        expect(failedPayload.result).toBe('policy engine failed');
        expect(failedPayload.business_status).toBe('workflow_error');
        expect(failedPayload.result_status).toBe('workflow_error');
        expect(failedPayload.result_payload).toEqual(expect.objectContaining({
            approval_result: 'attention',
            business_status: 'workflow_error',
            result_status: 'workflow_error',
            text: 'policy engine failed',
            workflow_lifecycle: 'error',
        }));
        expect(failedPayload.outputs).toEqual(expect.arrayContaining([expect.objectContaining({ kind: 'approval_result', text: 'policy engine failed', status: 'workflow_error' })]));
        expect(failedPayload.events.map((event: any) => event.action)).toContain('workflow_failed');
        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalledTimes(2));
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].instance.status).toBe('attention');
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].instance.result_payload).toEqual(expect.objectContaining({
            approval_result: 'attention',
            workflow_lifecycle: 'error',
        }));
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].instance.outputs[0].kind).toBe('approval_result');
    });
    it('keeps approval instance detail scoped to the selected lane and row', async () => {
        const pendingInstances = [
            {
                app_id: 'expense',
                instance_id: 'approval-pending-7',
                title: 'Travel expense',
                lane: 'pending_my_approval',
                status: 'pending',
                current_node: 'manager_approval',
                owner: 'alice',
                approver: 'manager',
                updated_at: '2026-06-19T00:00:00Z',
                result: 'waiting',
                workflow_skill_id: 'expense-approval-workflow',
                dataset_id: 'finance.expenses',
                object_role: 'expense_report',
                approval_id: 'approval-remote-7',
                record_id: 'EXP-20260619-001',
                detail_url: 'approval://instances/approval-pending-7',
                business_status: 'pending_manager_approval',
                result_status: 'pending',
                events: [{ at: '2026-06-19T00:00:00Z', node: 'submit', actor: 'alice', decision: 'submitted', message: 'submitted expense' }],
            },
            {
                app_id: 'expense',
                instance_id: 'approval-pending-8',
                title: 'Office expense',
                lane: 'pending_my_approval',
                status: 'pending',
                current_node: 'finance_review',
                owner: 'bob',
                approver: 'finance',
                updated_at: '2026-06-20T00:00:00Z',
                result: 'needs review',
                workflow_skill_id: 'expense-approval-workflow',
                dataset_id: 'finance.expenses',
                object_role: 'expense_report',
                approval_id: 'approval-remote-8',
                record_id: 'EXP-20260620-002',
                detail_url: 'approval://instances/approval-pending-8',
                business_status: 'pending_finance_review',
                result_status: 'pending',
                events: [{ at: '2026-06-20T00:00:00Z', node: 'finance_review', actor: 'finance', decision: 'reviewing', message: 'needs receipt check' }],
            },
        ];
        listMaclawAppApprovalInstancesAllMock.mockImplementation(async (lane) => lane === 'my_requests' ? [] : pendingInstances);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByText('\u5ba1\u6279\u72b6\u6001'));
        await waitFor(() => expect(listMaclawAppApprovalInstancesAllMock).toHaveBeenCalledWith('all', 200));
        const nav = document.querySelector('.apps-approval-manager__lanes') as HTMLElement;
        const detail = document.querySelector('.apps-approval-detail') as HTMLElement;
        const actions = screen.getByLabelText('\u5ba1\u6279\u64cd\u4f5c');

        await waitFor(() => expect(within(detail).getByText('Travel expense')).not.toBeNull());
        fireEvent.click(screen.getByText('Office expense').closest('.apps-approval-row') as HTMLElement);
        expect(within(detail).getByText('Office expense')).not.toBeNull();
        expect(within(detail).getAllByText(/finance_review/).length).toBeGreaterThan(0);
        expect(within(detail).getByText('EXP-20260620-002')).not.toBeNull();
        expect(within(detail).getByText('expense_report')).not.toBeNull();
        expect(within(detail).getByText('approval-remote-8')).not.toBeNull();
        expect(within(detail).getAllByText('pending_finance_review').length).toBeGreaterThan(0);
        expect(within(detail).getByText(/needs receipt check/)).not.toBeNull();
        const workflowLink = within(detail).getByRole('button', { name: '\u67e5\u770b\u5b8c\u6574\u6d41\u7a0b' });
        fireEvent.click(workflowLink);
        expect(browserOpenURLMock).toHaveBeenCalledWith('approval://instances/approval-pending-8');
        expect((within(actions).getByText('\u901a\u8fc7') as HTMLButtonElement).disabled).toBe(false);

        expect(within(nav).getByRole('button', { name: /\u5f85\u6211\u5ba1\u6279\s*2/ })).not.toBeNull();
        expect(within(nav).getByRole('button', { name: /\u6211\u7684\u7533\u8bf7\s*0/ })).not.toBeNull();
        fireEvent.click(within(nav).getByText('待我审批'));
        expect(listMaclawAppApprovalInstancesAllMock).not.toHaveBeenCalledWith('pending_my_approval', 200);
        expect(screen.getAllByText('Office expense').length).toBeGreaterThan(0);
        expect(within(detail).getByText('Travel expense')).not.toBeNull();

        fireEvent.click(within(nav).getByText('\u6211\u7684\u7533\u8bf7'));
        expect(listMaclawAppApprovalInstancesAllMock).not.toHaveBeenCalledWith('my_requests', 200);
        await waitFor(() => expect(within(document.querySelector('.apps-approval-detail') as HTMLElement).getByText('\u53f3\u4fa7\u662f\u5b9e\u4f8b\u8be6\u60c5\u548c\u5904\u7406\u52a8\u4f5c\u533a\uff0c\u8bf7\u5148\u5728\u5de6\u4fa7\u9009\u62e9\u4e00\u6761\u5ba1\u6279\u5b9e\u4f8b\u3002')).not.toBeNull());
        expect(screen.queryByLabelText('\u5ba1\u6279\u64cd\u4f5c')).toBeNull();
    });
	    it('records approval decisions with workflow result fields', async () => {
	        listMaclawAppApprovalInstancesAllMock.mockResolvedValue([{
	            app_id: 'expense',
            instance_id: 'approval-pending-7',
            title: 'Travel expense',
            lane: 'pending_my_approval',
            status: 'pending',
            current_node: 'manager_approval',
            owner: 'alice',
            approver: 'manager',
            updated_at: '2026-06-19T00:00:00Z',
            result: 'waiting',
            workflow_skill_id: 'expense-approval-workflow',
            approval_workflow_id: 'expense_approval',
            current_assignee: 'manager',
            current_assignee_type: 'user',
            dataset_id: 'finance.expenses',
            object_role: 'expense_report',
            approval_id: 'approval-remote-7',
            business_status: 'approval_pending',
            result_status: 'pending',
            from_status: 'submitted',
            to_status: 'approval_pending',
        }]);
        recordMaclawAppApprovalInstanceMock.mockImplementation(async (payload) => payload);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByText('\u5ba1\u6279\u72b6\u6001'));
        await waitFor(() => expect(screen.getAllByText('Travel expense').length).toBeGreaterThan(0));
        const actions = screen.getByLabelText('\u5ba1\u6279\u64cd\u4f5c');
        fireEvent.click(within(actions).getByText('\u901a\u8fc7'));

        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalled());
        const payload = recordMaclawAppApprovalInstanceMock.mock.calls[0][0];
        expect(payload.instance_id).toBe('approval-pending-7');
        expect(payload.status).toBe('approved');
        expect(payload.lane).toBe('handled');
        expect(payload.workflow_skill_id).toBe('expense-approval-workflow');
        expect(payload.approval_workflow_id).toBe('expense_approval');
        expect(payload.workflow_decision_id).toMatch(/^decision-/);
        expect(payload.approval_id).toBe('approval-remote-7');
        expect(payload.dataset_id).toBe('finance.expenses');
        expect(payload.object_role).toBe('expense_report');
        expect(payload.business_status).toBe('approved');
        expect(payload.result_status).toBe('approved');
        expect(payload.current_assignee).toBe('completed');
        expect(payload.current_assignee_type).toBe('system');
        expect(payload.from_status).toBe('approval_pending');
        expect(payload.to_status).toBe('approved');
        expect(payload.outputs).toHaveLength(1);
        expect(payload.outputs[0]).toEqual(expect.objectContaining({
            kind: 'approval_result',
            text: '\u5df2\u901a\u8fc7',
            status: 'approved',
        }));
        expect(payload.outputs[0].data).toEqual(expect.objectContaining({
            approval_result: 'approved',
            business_status: 'approved',
            result_status: 'approved',
        }));
        expect(payload.events[0].decision).toBe('approved');
        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalledTimes(1));
        const syncPayload = syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0];
        expect(syncPayload.dataset_id).toBe('finance.expenses');
        expect(syncPayload.object_role).toBe('expense_report');
        expect(syncPayload.approval_id).toBe('approval-remote-7');
        expect(syncPayload.record_id).toBe('approval-pending-7');
        expect(syncPayload.instance.workflow_decision_id).toMatch(/^decision-/);
        expect(syncPayload.instance.approval_workflow_id).toBe('expense_approval');
        expect(syncPayload.instance.current_assignee).toBe('completed');
        expect(syncPayload.instance.current_assignee_type).toBe('system');
        expect(syncPayload.instance.outputs[0].kind).toBe('approval_result');
	        expect(syncPayload.instance.outputs[0].data.approval_result).toBe('approved');
		        expect(syncPayload.instance.from_status).toBe('approval_pending');
		        expect(syncPayload.instance.to_status).toBe('approved');
	        await waitFor(() => {
	            const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}');
	            expect(history.expense?.[0]?.outputMode).toBe('approval');
	            expect(history.expense?.[0]?.approvalInstance?.approvalID).toBe('approval-remote-7');
	            expect(history.expense?.[0]?.approvalInstance?.status).toBe('approved');
	            expect(history.expense?.[0]?.resultPayload?.approval_result).toBe('approved');
	            expect(history.expense?.[0]?.outputs?.[0]?.kind).toBe('approval_result');
	        });
		    });
	    it('records DataSrv sync failures in approval instance timelines', async () => {
	        listMaclawAppApprovalInstancesAllMock.mockResolvedValue([{
	            app_id: 'expense',
	            instance_id: 'approval-sync-fail-1',
	            title: 'Expense sync failure',
	            lane: 'pending_my_approval',
	            status: 'pending',
	            current_node: 'manager_approval',
	            owner: 'alice',
	            approver: 'manager',
	            updated_at: '2026-06-19T00:00:00Z',
	            result: 'waiting',
	            workflow_skill_id: 'expense-approval-workflow',
	            dataset_id: 'finance.expenses',
	            object_role: 'expense_report',
	            approval_id: 'approval-remote-sync-fail',
	            business_status: 'approval_pending',
	            result_status: 'pending',
	        }]);
	        recordMaclawAppApprovalInstanceMock.mockImplementation(async (payload) => payload);
	        syncMaclawAppApprovalInstanceToDataSrvMock.mockResolvedValueOnce({ synced: false, reason: 'approval_status = approved', action: 'review_record_approval' });
	        render(<AppsPage lang="zh-Hans" />);

	        fireEvent.click(screen.getByText('\u5ba1\u6279\u72b6\u6001'));
	        await waitFor(() => expect(screen.getAllByText('Expense sync failure').length).toBeGreaterThan(0));
	        fireEvent.click(within(screen.getByLabelText('\u5ba1\u6279\u64cd\u4f5c')).getByText('\u901a\u8fc7'));

	        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalledTimes(1));
	        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalledTimes(2));
	        const syncedPayload = recordMaclawAppApprovalInstanceMock.mock.calls[1][0];
	        const syncEvent = syncedPayload.events.find((event: any) => event.action === 'datasrv_sync_failed');
	        expect(syncEvent).toBeTruthy();
	        expect(syncEvent.message).toBe('approval_status = approved');
	        expect(syncEvent.metadata.reason).toBe('approval_status = approved');
	        const detail = document.querySelector('.apps-approval-detail') as HTMLElement;
	        await waitFor(() => expect(within(detail).getByText(/approval_status = approved/)).not.toBeNull());
	    });
	    it('keeps approval result package when a pending item is manually approved', async () => {
        listMaclawAppApprovalInstancesAllMock.mockResolvedValue([{
            app_id: 'expense',
            instance_id: 'approval-pending-package',
            title: 'Expense with outputs',
            lane: 'pending_my_approval',
            status: 'pending',
            current_node: 'finance_review',
            owner: 'alice',
            approver: 'finance',
            current_assignee: 'finance_queue',
            current_assignee_type: 'queue',
            updated_at: '2026-06-20T00:00:00Z',
            result: 'waiting',
            workflow_skill_id: 'expense-approval-workflow',
            business_status: 'approval_pending',
            result_status: 'pending',
            from_status: 'submitted',
            to_status: 'approval_pending',
            result_payload: { business_record: { id: 'exp-99', status: 'pending_finance_review' }, content: 'review finance receipt' },
            outputs: [
                { id: 'out-1', kind: 'text', title: 'Approval note', text: 'needs finance confirmation', status: 'ready' },
                { id: 'out-2', kind: 'requires_input', title: 'Missing materials', text: 'missing payment screenshot', status: 'waiting' },
            ],
            artifacts: [{ id: 'artifact-99', uri: 'artifact://approval-pending-package/report', name: 'finance-report.pdf', status: 'ready' }],
        }]);
        recordMaclawAppApprovalInstanceMock.mockImplementation(async (payload) => payload);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByText('\u5ba1\u6279\u72b6\u6001'));
        await waitFor(() => expect(screen.getAllByText('Expense with outputs').length).toBeGreaterThan(0));
        const detail = document.querySelector('.apps-approval-detail') as HTMLElement;
        expect(within(detail).getByText('\u7ed3\u679c\u5305')).not.toBeNull();
        expect(within(detail).getByText('Approval note')).not.toBeNull();
        expect(within(detail).getByText('Missing materials')).not.toBeNull();
        expect(within(detail).getAllByText('finance-report.pdf').length).toBeGreaterThan(0);
        expect(within(detail).getByText(/pending_finance_review/)).not.toBeNull();
        expect(within(detail).getByText('finance_queue')).not.toBeNull();
        expect(within(detail).getByText('queue')).not.toBeNull();
        expect(within(detail).getByText('submitted -> approval_pending')).not.toBeNull();

        fireEvent.click(within(screen.getByLabelText('\u5ba1\u6279\u64cd\u4f5c')).getByText('\u901a\u8fc7'));

        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalledTimes(1));
        const payload = recordMaclawAppApprovalInstanceMock.mock.calls[0][0];
        expect(payload.instance_id).toBe('approval-pending-package');
        expect(payload.status).toBe('approved');
        expect(payload.result_payload.approval_result).toBe('approved');
        expect(payload.result_payload.business_status).toBe('approved');
        expect(payload.result_payload.result_status).toBe('approved');
        expect(payload.result_payload.business_record.id).toBe('exp-99');
        expect(payload.outputs).toHaveLength(2);
        expect(payload.outputs[1].kind).toBe('requires_input');
        expect(payload.artifacts[0].name).toBe('finance-report.pdf');
        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalledTimes(1));
        const syncPayload = syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0];
	        expect(syncPayload.instance.result_payload.business_record.status).toBe('pending_finance_review');
	        expect(syncPayload.instance.result_payload.approval_status).toBe('approved');
	        expect(syncPayload.instance.outputs[1].kind).toBe('requires_input');
	        expect(syncPayload.instance.artifacts[0].id).toBe('artifact-99');
	        await waitFor(() => {
	            const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}');
	            expect(history.expense?.[0]?.outputMode).toBe('approval');
	            expect(history.expense?.[0]?.approvalInstance?.instanceId).toBe('approval-pending-package');
	            expect(history.expense?.[0]?.approvalInstance?.status).toBe('approved');
	            expect(history.expense?.[0]?.approvalInstance?.artifacts?.[0]?.name).toBe('finance-report.pdf');
	            expect(history.expense?.[0]?.resultPayload?.business_record?.id).toBe('exp-99');
	        });
	    });
    it('shows the approval instance workspace for approval apps', async () => {
        listMaclawAppApprovalInstancesMock.mockImplementation(async (_appID, lane) => [{
            app_id: 'expense',
            app_name: '报销申请',
            instance_id: lane === 'pending_my_approval' ? 'approval-workspace-2' : 'approval-workspace-1',
            title: lane === 'pending_my_approval' ? 'Lane refreshed expense' : 'Travel expense summary',
            lane: 'pending_my_approval',
            status: 'pending',
            current_node: lane === 'pending_my_approval' ? 'finance_review' : 'manager_approval',
            applicant: lane === 'pending_my_approval' ? 'request_only_applicant' : 'workspace_applicant',
            current_assignee: lane === 'pending_my_approval' ? 'finance_queue' : 'manager',
            current_assignee_type: lane === 'pending_my_approval' ? 'queue' : 'user',
            from_status: 'submitted',
            to_status: 'approval_pending',
            updated_at: '2026-06-20T00:00:00Z',
            result_payload: { business_record: { id: lane === 'pending_my_approval' ? 'EXP-WORKSPACE-2' : 'EXP-WORKSPACE-1', status: 'approval_pending' } },
            outputs: [{ id: 'workspace-output', kind: 'text', title: 'Workspace output', text: 'review package ready', status: 'ready' }],
            artifacts: [{ id: 'workspace-artifact', name: 'workspace-approval.pdf', status: 'ready' }],
            dataset_id: 'finance.expenses',
            object_role: 'expense_report',
            approval_id: lane === 'pending_my_approval' ? 'approval-remote-workspace-2' : 'approval-remote-workspace-1',
            record_id: lane === 'pending_my_approval' ? 'EXP-WORKSPACE-2' : 'EXP-WORKSPACE-1',
        }]);
        recordMaclawAppApprovalInstanceMock.mockImplementation(async (payload) => payload);
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        await waitFor(() => expect(listMaclawAppApprovalInstancesMock).toHaveBeenCalledWith('expense', 'all', 50));
        await waitFor(() => expect(document.querySelector('.apps-approval-workspace')).not.toBeNull());
        fireEvent.click(screen.getByRole('button', { name: '执行' }));

        await waitFor(() => expect(document.querySelector('.apps-approval-workspace')).not.toBeNull());
        const workspace = document.querySelector('.apps-approval-workspace') as HTMLElement;
        expect(within(workspace).getByText('审批实例')).not.toBeNull();
        expect(within(workspace).getByRole('button', { name: /我的申请/ })).not.toBeNull();
        const pendingLane = within(workspace).getByRole('button', { name: /待我审批/ });
        expect(pendingLane).not.toBeNull();
        expect(within(workspace).getByRole('button', { name: /已处理/ })).not.toBeNull();
        expect(within(workspace).getByRole('button', { name: /^需关注/ })).not.toBeNull();
        expect(within(workspace).getByRole('button', { name: /全部/ })).not.toBeNull();
        fireEvent.click(pendingLane);
        await waitFor(() => expect(listMaclawAppApprovalInstancesMock).toHaveBeenCalledWith('expense', 'pending_my_approval', 50));
        await waitFor(() => expect(within(workspace).getAllByText('Lane refreshed expense').length).toBeGreaterThan(0));
        expect(within(workspace).getByText('结果契约')).not.toBeNull();
        expect(within(workspace).getAllByText('审批结果').length).toBeGreaterThan(0);
        expect(within(workspace).getAllByText(/finance_queue/).length).toBeGreaterThan(0);
        expect(within(workspace).getAllByText(/request_only_applicant/).length).toBeGreaterThan(0);
        expect(within(workspace).getAllByText(/submitted -> approval_pending/).length).toBeGreaterThan(0);
        fireEvent.click(within(within(workspace).getByLabelText('\u5ba1\u6279\u64cd\u4f5c')).getByText('\u6807\u8bb0\u5173\u6ce8'));
        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalled());
        const payload = recordMaclawAppApprovalInstanceMock.mock.calls.map((call) => call[0]).find((item) => item.status === 'attention') as any;
        expect(payload).toBeTruthy();
        expect(payload.status).toBe('attention');
        expect(payload.result_payload.approval_result).toBe('attention');
	        expect(payload.result_payload.business_record.id).toBe('EXP-WORKSPACE-2');
	        expect(payload.outputs[0].title).toBe('Workspace output');
	        expect(payload.artifacts[0].name).toBe('workspace-approval.pdf');
	        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls.some((call) => call[0]?.instance?.status === 'attention')).toBe(true));
	        const syncPayload = syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls.map((call) => call[0]).find((item) => item?.instance?.status === 'attention');
	        expect(syncPayload.approval_id).toBe('approval-remote-workspace-2');
	        expect(syncPayload.instance.status).toBe('attention');
	        expect(syncPayload.instance.result_payload.approval_result).toBe('attention');
	        await waitFor(() => {
	            const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}');
	            expect(history.expense?.[0]?.outputMode).toBe('approval');
	            expect(history.expense?.[0]?.approvalInstance?.approvalID).toBe('approval-remote-workspace-2');
	            expect(history.expense?.[0]?.approvalInstance?.status).toBe('attention');
	            expect(history.expense?.[0]?.resultPayload?.approval_result).toBe('attention');
	        });
	        expect(document.querySelector('.apps-approval-summary')).toBeNull();
	        expect(document.querySelector('.apps-approval-manager')).toBeNull();
	    });

    it('shows approval workspace load failures without fallback approval rows and recovers on refresh', async () => {
        const recoveredInstance = {
            app_id: 'expense',
            app_name: '报销申请',
            instance_id: 'approval-recovered-1',
            title: 'Recovered expense approval',
            lane: 'my_requests',
            status: 'pending',
            current_node: 'manager_approval',
            applicant: 'alice',
            current_assignee: 'manager',
            current_assignee_type: 'user',
            updated_at: '2026-06-30T11:00:00Z',
            result_payload: { business_record: { id: 'EXP-RECOVERED-1', status: 'approval_pending' } },
            outputs: [{ id: 'out-recovered', kind: 'text', title: 'Recovered package', text: 'loaded after retry', status: 'ready' }],
            dataset_id: 'finance.expenses',
            object_role: 'expense_report',
            approval_id: 'approval-recovered-remote-1',
            record_id: 'EXP-RECOVERED-1',
        };
        listMaclawAppApprovalInstancesMock
            .mockRejectedValueOnce(new Error('DataSrv offline'))
            .mockRejectedValueOnce(new Error('DataSrv offline'))
            .mockResolvedValue([recoveredInstance]);

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        const workspace = await waitFor(() => document.querySelector('.apps-approval-workspace') as HTMLElement);
        await waitFor(() => expect(within(workspace).getByRole('alert').textContent).toContain('审批实例读取失败'));
        expect(within(workspace).getByText(/暂无审批实例/)).not.toBeNull();
        expect(within(workspace).queryByText('发起节点')).toBeNull();
        expect(within(workspace).queryByText('Recovered expense approval')).toBeNull();

        fireEvent.click(within(workspace).getByText('刷新'));
        await waitFor(() => expect(within(workspace).getAllByText('Recovered expense approval').length).toBeGreaterThan(0));
        expect(within(workspace).getByText('Recovered package')).not.toBeNull();
    });
    it('keeps handled approval workspace items read-only', async () => {
        listMaclawAppApprovalInstancesMock.mockImplementation(async () => [{
            app_id: 'expense',
            app_name: '报销申请',
            instance_id: 'approval-workspace-handled',
            title: 'Handled expense decision',
            lane: 'handled',
            status: 'approved',
            current_node: 'completed',
            owner: 'alice',
            approver: 'finance',
            current_assignee: 'finance_queue',
            current_assignee_type: 'queue',
            from_status: 'approval_pending',
            to_status: 'approved',
            updated_at: '2026-06-20T01:00:00Z',
            result: 'approved by finance',
            result_payload: { approval_result: 'approved', business_status: 'approved' },
            outputs: [{ id: 'out-approved', kind: 'approval_result', title: 'Approval decision', text: 'approved', status: 'approved' }],
        }]);

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        await waitFor(() => expect(document.querySelector('.apps-approval-workspace')).not.toBeNull());
        const workspace = document.querySelector('.apps-approval-workspace') as HTMLElement;
        fireEvent.click(within(workspace).getByRole('button', { name: /已处理/ }));

        await waitFor(() => expect(within(workspace).getAllByText('Handled expense decision').length).toBeGreaterThan(0));
        expect(within(workspace).getByText('approved by finance')).not.toBeNull();
        expect(within(workspace).getByText('Approval decision')).not.toBeNull();
        expect((within(workspace).getByText('通过') as HTMLButtonElement).disabled).toBe(true);
        expect((within(workspace).getByText('驳回') as HTMLButtonElement).disabled).toBe(true);
        expect((within(workspace).getByText('标记关注') as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(within(workspace).getByText('通过'));
        expect(recordMaclawAppApprovalInstanceMock).not.toHaveBeenCalled();
    });
    it('shows an empty runtime area until an app icon is clicked', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        expect(screen.getByText('选择应用')).not.toBeNull();
        expect(screen.getByText('点击左侧应用图标，以打开应用。')).not.toBeNull();
        expect(screen.queryByRole('button', { name: '打开应用' })).toBeNull();
        expect(container.querySelector('.apps-runtime-tabs')).toBeNull();

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        expect(container.querySelector('.apps-runtime-tabs')).not.toBeNull();
        expect(container.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('报销申请');
        expect(container.querySelector('.apps-preview__summary')).toBeNull();
        expect(screen.queryByRole('button', { name: '打开应用' })).toBeNull();
    });

    it('closes runtime tabs and activates the remaining app', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.click(screen.getAllByText('采购入库')[0]);
        fireEvent.click(screen.getByLabelText('关闭 采购入库'));

        expect(container.querySelectorAll('.apps-runtime-tab').length).toBe(1);
        expect(container.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('报销申请');
    });

    it('returns to the empty runtime when the last app tab is closed', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.click(screen.getByLabelText('关闭 报销申请'));

        expect(screen.getByText('选择应用')).not.toBeNull();
        expect(container.querySelector('.apps-runtime-tabs')).toBeNull();
        expect(container.querySelector('.apps-app-tile.is-active')).toBeNull();
    });

    it('runs a fixed skill app with file input and output mode', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        const fileInput = container.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        const file = new File(['demo'], 'demo.pdf', { type: 'application/pdf' });
        fireEvent.change(fileInput, { target: { files: [file] } });
        fireEvent.change(screen.getByDisplayValue('Word / DOCX'), { target: { value: 'pdf' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(document.querySelector('.apps-run-history')).not.toBeNull());
        expect(screen.getByText(/demo.pdf -> PDF/)).not.toBeNull();
    });

    it('renders form-only tool apps without a file drop zone', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'form-tool', skill_id: 'form-tools', name: '参数工具', description: 'Form only', category: '工具', icon: 'sheet', input_mode: 'form', output_modes: ['json'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('参数工具')[0]);

        expect(container.querySelector('.apps-drop-zone')).toBeNull();
        fireEvent.change(screen.getByPlaceholderText('输入处理要求或表单参数。'), { target: { value: '生成 JSON 摘要' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getByText(/生成 JSON 摘要 -> JSON/)).not.toBeNull());
    });

    it('renders declared tool app fields as a structured form', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'field-tool',
                            skill_id: 'field-tools',
                            name: '字段工具',
                            description: 'Fields',
                            category: '工具',
                            icon: 'sheet',
                            input_mode: 'form',
                            output_modes: ['json'],
                            fields: [
                                { name: 'title', label: '标题', type: 'text', required: true },
                                { name: 'format', label: '格式', type: 'select', default: '鎽樿', options: ['鎽樿', '娓呭'] },
                                { name: 'include_refs', label: '包含来源', type: 'boolean', default: true },
                            ],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('字段工具')[0]);

        expect(container.querySelector('.apps-drop-zone')).toBeNull();
        fireEvent.click(screen.getByText('执行'));
        await waitFor(() => expect(screen.getByText('请补充必填输入')).not.toBeNull());
        fireEvent.change(screen.getByLabelText('标题'), { target: { value: '季度报告' } });
        fireEvent.change(screen.getByDisplayValue('鎽樿'), { target: { value: '娓呭' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getByText(/标题: 季度报告.*JSON/)).not.toBeNull());
    });

    it('uses the first select option as the default field value', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'select-tool',
                            skill_id: 'select-tools',
                            name: '选择工具',
                            description: 'Select fields',
                            category: '工具',
                            icon: 'sheet',
                            input_mode: 'form',
                            output_modes: ['json'],
                            fields: [
                                { name: 'mode', label: '模式', type: 'select', required: true, options: ['快', '完整'] },
                            ],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('选择工具')[0]);

        expect(container.querySelector('.apps-drop-zone')).toBeNull();
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getByText(/模式: .*JSON/)).not.toBeNull());
    });

    it('adds a select default value to field options when installing apps', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'select-default-tool',
                            skill_id: 'select-default-tools',
                            name: '默认选项工具',
                            description: 'Select default',
                            category: '工具',
                            icon: 'sheet',
                            input_mode: 'form',
                            output_modes: ['json'],
                            fields: [
                                { name: 'mode', label: '模式', type: 'select', required: true, default: '快', options: ['完整'] },
                            ],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('默认选项工具')[0]);

        const modeSelect = screen.getByDisplayValue('快') as HTMLSelectElement;
        expect(Array.from(modeSelect.options).map((option) => option.value)).toEqual(['完整', '快']);
    });

    it('hydrates durable evidence recorded under the canonical Skill app identity', async () => {
        const base = dynamicToolApp('skill-app-paper_pdf_translator-app-pdf', 'PDF Translation Tool', 'Documents', 'paper_pdf_translator', ['pdf']);
        const app = { ...base, source: 'skill' as const, manifest: { ...base.manifest, launchMode: 'fixed_skill_ui' as const } };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app] }));
        listSkillAppManifestsMock.mockResolvedValue([{
            id: 'app-pdf', skill_id: 'paper_pdf_translator', name: app.name, description: app.description, category: app.category,
            icon: app.icon, input_mode: 'file', output_modes: ['pdf'],
        }]);
        listMaclawAppRunHistoryMock.mockImplementation(async (appID) => appID === 'app-pdf' ? [{
            runID: 'run-canonical-skill-app', appID: 'app-pdf', status: 'done', definitionHash: testAppDefinitionFingerprint(app),
            testProtocolFingerprint: app.manifest.testProtocol?.fingerprint, workspaceLayoutFingerprint: testWorkspaceLayoutFingerprint(app),
            outputMode: 'pdf', inputSummary: 'sample.pdf', message: 'translated', artifactName: 'translated.pdf',
            resultPayload: { artifact: { name: 'translated.pdf' }, text: 'translated' }, outputs: [{ kind: 'artifact', title: 'translated.pdf', status: 'ready' }],
            resultCoverage: { ok: true, primary: 'artifact', coveredTypes: ['artifact', 'document', 'content'], missingTypes: [] }, at: '2026-07-30T00:00:00.000Z',
        }] : []);

        render(<AppsPage lang="en" />);
        // Durable evidence hydration now happens when the app workspace opens,
        // not in the publish pane.
        await screen.findByText('PDF Translation Tool');
        fireEvent.click(screen.getAllByText('PDF Translation Tool')[0]);
        await waitFor(() => expect(listMaclawAppRunHistoryMock).toHaveBeenCalledWith('app-pdf', 8));
        await waitFor(() => {
            const cache = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}');
            expect(cache[app.id]?.[0]?.runID).toBe('run-canonical-skill-app');
        });
    });

    it('hydrates durable evidence recorded under the runtime Skill identity', async () => {
        const base = dynamicToolApp('skill-app-paper_pdf_translator-app-pdf', 'PDF Runtime Evidence Tool', 'Documents', 'pdf', ['pdf']);
        const app = {
            ...base,
            source: 'skill' as const,
            manifest: {
                ...base.manifest,
                launchMode: 'fixed_skill_ui' as const,
                skill: { ...base.manifest.skill!, id: 'paper_pdf_translator' },
                appSkill: { id: 'paper_pdf_translator', version: '1.0.0', source: 'local' as const },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app] }));
        listSkillAppManifestsMock.mockResolvedValue([{
            id: 'app-pdf', skill_id: 'paper_pdf_translator', name: app.name, description: app.description, category: app.category,
            icon: app.icon, input_mode: 'file', output_modes: ['pdf'],
        }]);
        listMaclawAppRunHistoryMock.mockImplementation(async (appID) => appID === 'paper_pdf_translator' ? [{
            runID: 'run-runtime-skill-id', appID: 'paper_pdf_translator', status: 'done', definitionHash: testAppDefinitionFingerprint(app),
            testProtocolFingerprint: app.manifest.testProtocol?.fingerprint, workspaceLayoutFingerprint: testWorkspaceLayoutFingerprint(app),
            outputMode: 'pdf', inputSummary: 'sample.pdf', message: 'translated', artifactName: 'translated.pdf',
            resultPayload: { artifact: { name: 'translated.pdf' }, text: 'translated' }, outputs: [{ kind: 'artifact', title: 'translated.pdf', status: 'ready' }],
            resultCoverage: { ok: true, primary: 'artifact', coveredTypes: ['artifact', 'document', 'content'], missingTypes: [] }, at: '2026-07-30T00:00:00.000Z',
        }] : []);

        render(<AppsPage lang="en" />);
        // Durable evidence hydration now happens when the app workspace opens,
        // not in the publish pane.
        await screen.findByText('PDF Runtime Evidence Tool');
        fireEvent.click(screen.getAllByText('PDF Runtime Evidence Tool')[0]);

        await waitFor(() => expect(listMaclawAppRunHistoryMock).toHaveBeenCalledWith('paper_pdf_translator', 8));
        await waitFor(() => {
            const cache = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}');
            expect(cache[app.id]?.[0]?.runID).toBe('run-runtime-skill-id');
        });
    });

    it('starts the bound skill when running a tool app', async () => {
        const customIconDataUrl = 'data:image/png;base64,iVBORw0KGgo=';
        const { unmount } = render(<AppsPage lang="zh-Hans" />);
        // After the install, the skill app is rediscovered from its installed
        // Skill definition (it is not cached in panel state), mirroring production.
        listSkillAppManifestsMock.mockResolvedValue([{ id: 'run-tool', skill_id: 'run-tools', custom_icon_data_url: customIconDataUrl, name: '运行工具', description: 'Run fields', category: '工具', icon: 'sheet', input_mode: 'form', output_modes: ['json'], fields: [{ name: 'title', label: '标题', type: 'text', required: true }] }]);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'run-tool',
                            skill_id: 'run-tools',
                            custom_icon_data_url: customIconDataUrl,
                            name: '运行工具',
                            description: 'Run fields',
                            category: '工具',
                            icon: 'sheet',
                            input_mode: 'form',
                            output_modes: ['json'],
                            fields: [
                                { name: 'title', label: '标题', type: 'text', required: true },
                            ],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        await screen.findByText('运行工具');
        fireEvent.click(screen.getAllByText('运行工具')[0]);
        fireEvent.change(screen.getByLabelText('标题'), { target: { value: '季度报告' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('run-tools', expect.objectContaining({
            _maclaw_app: true,
            app_id: 'skill-app-run-tools-run-tool',
            output_mode: 'json',
            fields: { title: '季度报告' },
            file: null,
        })));
        expect(String(runNLSkillAsyncMock.mock.calls[0][1].prompt)).toContain('Run MaClaw tool app: 运行工具');
        expect(screen.getAllByText(/run-test-1/).length).toBeGreaterThan(0);
        expect(screen.getByText('运行历史')).not.toBeNull();
        expect(screen.getAllByText(/done/).length).toBeGreaterThan(0);
        // Skill-source apps are no longer cached in panel state (they are
        // rediscovered from installed Skill definitions), so reconstruct the
        // installed entry to verify the recorded definition fingerprint.
        const installedApp = {
            id: 'skill-app-run-tools-run-tool',
            name: '运行工具',
            description: 'Run fields',
            category: '工具',
            kind: 'tool_app',
            icon: 'sheet',
            version: 1,
            customIconDataUrl,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'skill',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                appSkill: { id: 'run-tools', version: '1.0.0' },
                ui: testDefaultWorkspaceLayoutForKind('tool_app'),
                skill: { id: 'run-tools', appDefinitionFile: 'maclaw.apps.json', inputMode: 'form', multipleFiles: false, outputModes: ['json'], fields: testNormalizeSkillAppFields([{ name: 'title', label: '标题', type: 'text', required: true }]) },
            },
        };
        expect(installedApp.customIconDataUrl).toBe(customIconDataUrl);
        const expectedDefinitionHash = testAppDefinitionFingerprint(installedApp);
        await waitFor(() => expect(recordMaclawAppRunEvidenceForSkillMock).toHaveBeenCalledWith('run-tools', 'skill-app-run-tools-run-tool', expectedDefinitionHash, 'run-test-1', '', expect.any(String)));
        // Durable backend store is authoritative for publish evidence.
        await waitFor(() => expect(recordMaclawAppRunHistoryMock).toHaveBeenCalledWith(expect.objectContaining({
            runID: 'run-test-1',
            appID: 'skill-app-run-tools-run-tool',
            status: 'done',
        })));

        unmount();
        render(<AppsPage lang="zh-Hans" />);
        await screen.findByText('运行工具');
        fireEvent.click(screen.getAllByText('运行工具')[0]);
        expect(screen.getByText('运行历史')).not.toBeNull();
        expect(screen.getAllByText(/run-test-1/).length).toBeGreaterThan(0);
        fireEvent.click(screen.getByText('清空历史'));
        expect(screen.getByText('暂无运行记录')).not.toBeNull();
        expect(screen.queryByText(/run-test-1/)).toBeNull();
        await waitFor(() => expect(clearMaclawAppRunHistoryMock).toHaveBeenCalledWith('skill-app-run-tools-run-tool'));
    });

    it('surfaces durable run-history persistence failures after a successful skill run', async () => {
        recordMaclawAppRunHistoryMock.mockRejectedValueOnce(new Error('disk full'));
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'run-tool-durable-fail',
                            skill_id: 'run-tools',
                            name: '持久化失败工具',
                            description: 'Run fields',
                            category: '工具',
                            icon: 'sheet',
                            input_mode: 'form',
                            output_modes: ['json'],
                            fields: [
                                { name: 'title', label: '标题', type: 'text', required: true },
                            ],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('持久化失败工具')[0]);
        fireEvent.change(screen.getByLabelText('标题'), { target: { value: '报告' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalled());
        await waitFor(() => expect(recordMaclawAppRunHistoryMock).toHaveBeenCalled());
        await waitFor(() => {
            expect(document.body.textContent || '').toMatch(/disk full/);
            expect(document.body.textContent || '').toMatch(/运行证据未写入本机存储|Run evidence was not saved to durable store/);
        });
    });

    it('merges backend durable history rows when opening an app runtime tab', async () => {
        listMaclawAppRunHistoryMock.mockResolvedValue([
            {
                runID: 'durable-run-9',
                appID: 'skill-app-run-tools-run-tool',
                status: 'done',
                outputMode: 'json',
                inputSummary: 'from durable',
                message: 'durable history item',
                at: '2026-07-15T10:00:00.000Z',
            },
        ]);
        // Pre-install app via market so id is stable.
        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'run-tool',
                            skill_id: 'run-tools',
                            name: '运行工具',
                            description: 'Run fields',
                            category: '工具',
                            icon: 'sheet',
                            input_mode: 'form',
                            output_modes: ['json'],
                            fields: [{ name: 'title', label: '标题', type: 'text', required: true }],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('运行工具')[0]);

        await waitFor(() => expect(listMaclawAppRunHistoryMock).toHaveBeenCalledWith('skill-app-run-tools-run-tool', 8));
        await waitFor(() => {
            expect(screen.getAllByText(/durable history item/).length).toBeGreaterThan(0);
            expect(screen.getAllByText(/durable-run-9/).length).toBeGreaterThan(0);
        });
    });

    it('keeps the rich local run summary when the durable governance stamp shares a runID', async () => {
        const appID = 'skill-app-run-tools-run-tool';
        listMaclawAppRunHistoryMock.mockResolvedValue([
            {
                runID: 'run-dup-1',
                appID,
                status: 'done',
                message: 'skill governance testEvidence recorded',
                at: '2026-07-15T10:00:01.000Z',
            },
        ]);
        window.localStorage.setItem(runHistoryStorageKey, JSON.stringify({
            [appID]: [{
                runID: 'run-dup-1',
                appID,
                status: 'done',
                outputMode: 'json',
                inputSummary: 'sample.pdf',
                message: 'translated.pdf',
                at: '2026-07-15T10:00:00.000Z',
            }],
        }));
        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'run-tool',
                            skill_id: 'run-tools',
                            name: '运行工具',
                            description: 'Run fields',
                            category: '工具',
                            icon: 'sheet',
                            input_mode: 'form',
                            output_modes: ['json'],
                            fields: [{ name: 'title', label: '标题', type: 'text', required: true }],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('运行工具')[0]);

        await waitFor(() => expect(screen.getAllByText(/translated\.pdf/).length).toBeGreaterThan(0));
        expect(screen.queryByText(/skill governance testEvidence recorded/)).toBeNull();
    });

    it('surfaces skill governance evidence write failures instead of swallowing them', async () => {
        recordMaclawAppRunEvidenceForSkillMock.mockRejectedValueOnce(new Error('skill dir locked'));
        render(<AppsPage lang="zh-Hans" />);
        // The installed skill app is rediscovered from its Skill definition
        // (it is not cached in panel state), mirroring production reloads.
        listSkillAppManifestsMock.mockResolvedValue([{ id: 'run-tool-evidence-fail', skill_id: 'run-tools', name: '证据失败工具', description: 'Run fields', category: '工具', icon: 'sheet', input_mode: 'form', output_modes: ['json'], fields: [{ name: 'title', label: '标题', type: 'text', required: true }] }]);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'run-tool-evidence-fail',
                            skill_id: 'run-tools',
                            name: '证据失败工具',
                            description: 'Run fields',
                            category: '工具',
                            icon: 'sheet',
                            input_mode: 'form',
                            output_modes: ['json'],
                            fields: [{ name: 'title', label: '标题', type: 'text', required: true }],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        await screen.findByText('证据失败工具');
        fireEvent.click(screen.getAllByText('证据失败工具')[0]);
        fireEvent.change(screen.getByLabelText('标题'), { target: { value: '报告' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(recordMaclawAppRunEvidenceForSkillMock).toHaveBeenCalled());
        await waitFor(() => {
            expect(document.body.textContent || '').toMatch(/skill dir locked/);
            expect(document.body.textContent || '').toMatch(/写入 Skill 运行证据失败|Failed to record skill run evidence/);
        });
    });

    it('prefers CheckMaclawAppRuntimeHealth over PlanMaclawAppInstall when blocking a run', async () => {
        const blockedHealth = {
            schema: 'maclaw.app.runtime_health.v1',
            ok: false,
            blocked: true,
            message: 'required skill dependencies are missing or unavailable: health-skill',
            plan: {
                schema: 'maclaw.app.install_plan.v1',
                apps: [{ id: 'skill-app-health-skill-health-tool', name: '健康检查工具', kind: 'tool_app' }],
                dependencies: [{
                    id: 'health-skill',
                    kind: 'runtime_skill',
                    source: 'hub',
                    required: true,
                    installed: false,
                    health: 'missing',
                    action: 'blocked',
                    app_ids: ['skill-app-health-skill-health-tool'],
                }],
                has_missing_required: true,
                has_blocking_dependency: true,
            },
            app_id: 'skill-app-health-skill-health-tool',
            has_missing_required: true,
            has_blocking_dependency: true,
            has_workflow_contract_issue: false,
        };
        // Stable for open-time auto check + run-time check (not Once).
        checkMaclawAppRuntimeHealthMock.mockResolvedValue(blockedHealth);
        planMaclawAppInstallMock.mockClear();

        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'health-tool', skill_id: 'health-skill', name: '健康检查工具', description: 'Health API check', category: '工具', icon: 'sheet', input_mode: 'form', output_modes: ['json'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        await waitFor(() => expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull());
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('健康检查工具')[0]);
        await waitFor(() => expect(checkMaclawAppRuntimeHealthMock).toHaveBeenCalled());
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => {
            expect(document.body.textContent || '').toMatch(/health-skill|required skill dependencies are missing|暂不可用/);
        });
        expect(runNLSkillAsyncMock).not.toHaveBeenCalled();
        // Health path should be preferred; Plan may still be used by default wrapper in other tests,
        // but this mock returns plan inline without calling Plan.
        expect(checkMaclawAppRuntimeHealthMock.mock.calls.length).toBeGreaterThan(0);
    });

    it('falls back to the registry path when opening a stale run-history artifact', async () => {
        seedStaleRunHistoryArtifact();
        openSkillRunArtifactMock.mockRejectedValueOnce(new Error('missing registered file'));
        getSkillRunArtifactMock.mockResolvedValueOnce({ path: 'C:\\good\\sample-output.pdf', available: true });

        await openStaleRunHistoryItem('打开');

        await waitFor(() => expect(getSkillRunArtifactMock).toHaveBeenCalledWith('run-ok-1', 'artifact-sample-pdf'));
        await waitFor(() => expect(openFileOrShowInFolderMock).toHaveBeenCalledWith('C:\\good\\sample-output.pdf'));
        expect(openFileOrShowInFolderMock).not.toHaveBeenCalledWith('C:\\bad\\missing.pdf');
    });

    it('does not open a stale run-history artifact when the registry marks it unavailable', async () => {
        seedStaleRunHistoryArtifact();
        openSkillRunArtifactMock.mockRejectedValueOnce(new Error('missing registered file'));
        getSkillRunArtifactMock.mockResolvedValueOnce({ path: 'C:\\bad\\missing.pdf', available: false });

        await openStaleRunHistoryItem('打开');

        await waitFor(() => expect(getSkillRunArtifactMock).toHaveBeenCalledWith('run-ok-1', 'artifact-sample-pdf'));
        expect(openFileOrShowInFolderMock).not.toHaveBeenCalled();
    });

    it('does not expose artifact actions for failed run-history records', async () => {
        seedFailedRunHistoryArtifact();
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('发票审核')[0]);
        const artifactNodes = await screen.findAllByText('failed-output.pdf');
        const historyItem = artifactNodes.map((node) => node.closest('article')).find(Boolean) as HTMLElement;

        expect(within(historyItem).queryByRole('button', { name: '打开' })).toBeNull();
        expect(within(historyItem).queryByRole('button', { name: '定位' })).toBeNull();
        expect(within(historyItem).queryByRole('button', { name: 'failed-output.pdf' })).toBeNull();
        expect(openSkillRunArtifactMock).not.toHaveBeenCalled();
        expect(revealSkillRunArtifactMock).not.toHaveBeenCalled();
    });

    it('falls back to the registry path when locating a stale run-history artifact', async () => {
        seedStaleRunHistoryArtifact();
        revealSkillRunArtifactMock.mockRejectedValueOnce(new Error('missing registered file'));
        getSkillRunArtifactMock.mockResolvedValueOnce({ path: 'C:\\good\\sample-output.pdf', available: true });

        await openStaleRunHistoryItem('定位');

        await waitFor(() => expect(getSkillRunArtifactMock).toHaveBeenCalledWith('run-ok-1', 'artifact-sample-pdf'));
        await waitFor(() => expect(showItemInFolderMock).toHaveBeenCalledWith('C:\\good\\sample-output.pdf'));
        expect(showItemInFolderMock).not.toHaveBeenCalledWith('C:\\bad\\missing.pdf');
    });

    it('does not locate a stale run-history artifact when the registry marks it unavailable', async () => {
        seedStaleRunHistoryArtifact();
        revealSkillRunArtifactMock.mockRejectedValueOnce(new Error('missing registered file'));
        getSkillRunArtifactMock.mockResolvedValueOnce({ path: 'C:\\bad\\missing.pdf', available: false });

        await openStaleRunHistoryItem('定位');

        await waitFor(() => expect(getSkillRunArtifactMock).toHaveBeenCalledWith('run-ok-1', 'artifact-sample-pdf'));
        expect(showItemInFolderMock).not.toHaveBeenCalled();
    });

    it('checks bound skill dependencies before running installed tool apps', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'runtime-dep-tool', skill_id: 'disabled-runtime-tool', name: '运行依赖工具', description: 'Runtime dependency check', category: '工具', icon: 'sheet', input_mode: 'form', output_modes: ['json'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        await waitFor(() => expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull());
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('运行依赖工具')[0]);
        planMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'skill-app-disabled-runtime-tool-runtime-dep-tool', name: '运行依赖工具', kind: 'tool_app' }],
            dependencies: [{
                id: 'disabled-runtime-tool',
                version: '1.0.0',
                kind: 'runtime_skill',
                source: 'hub',
                required: true,
                installed: true,
                installed_status: 'disabled',
                health: 'disabled',
                action: 'blocked',
                app_ids: ['skill-app-disabled-runtime-tool-runtime-dep-tool'],
            }],
            has_missing_required: false,
            has_blocking_dependency: true,
        });

        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(checkMaclawAppRuntimeHealthMock).toHaveBeenCalled());
        await waitFor(() => expect(screen.getAllByText('运行依赖工具暂不可用：disabled-runtime-tool 未安装或已停用。').length).toBeGreaterThan(0));
        const runtimeStatus = document.querySelector('.apps-runtime-status') as HTMLElement;
        const dependencyList = runtimeStatus.querySelector('.apps-install-record__deps') as HTMLElement;
        expect(dependencyList).not.toBeNull();
        const dependency = within(dependencyList).getByText('disabled-runtime-tool').closest('.apps-install-record__dep') as HTMLElement;
        expect(dependency.dataset.state).toBe('blocked');
        expect(within(dependency).getByText('不可用')).not.toBeNull();
        expect(dependency.textContent).toContain('runtime_skill · hub · v1.0.0 · disabled');
        expect(runNLSkillAsyncMock).not.toHaveBeenCalled();
    });

    it('explains needs-review runtime skill dependencies before running installed tool apps', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'runtime-review-tool', skill_id: 'review-runtime-tool', name: '审核依赖工具', description: 'Runtime dependency review check', category: '工具', icon: 'sheet', input_mode: 'form', output_modes: ['json'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        await waitFor(() => expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull());
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('审核依赖工具')[0]);
        planMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'skill-app-review-runtime-tool-runtime-review-tool', name: '审核依赖工具', kind: 'tool_app' }],
            dependencies: [{
                id: 'review-runtime-tool',
                version: '1.0.0',
                kind: 'runtime_skill',
                source: 'hub',
                required: true,
                installed: true,
                installed_status: 'needs_review',
                health: 'needs_review',
                action: 'blocked',
                app_ids: ['skill-app-review-runtime-tool-runtime-review-tool'],
            }],
            has_missing_required: false,
            has_blocking_dependency: true,
        });

        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getAllByText('审核依赖工具暂不可用：review-runtime-tool 需要在技能管理中审核通过后才能使用。').length).toBeGreaterThan(0));
        const runtimeStatus = document.querySelector('.apps-runtime-status') as HTMLElement;
        const dependencyList = runtimeStatus.querySelector('.apps-install-record__deps') as HTMLElement;
        expect(dependencyList).not.toBeNull();
        const dependency = within(dependencyList).getByText('review-runtime-tool').closest('.apps-install-record__dep') as HTMLElement;
        expect(dependency.dataset.state).toBe('blocked');
        expect(dependency.textContent).toContain('runtime_skill · hub · v1.0.0 · needs_review');
        expect(runNLSkillAsyncMock).not.toHaveBeenCalled();
    });

    it('installs missing runtime dependencies and continues the installed app run', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'runtime-dep-tool', skill_id: 'disabled-runtime-tool', name: '运行依赖工具', description: 'Runtime dependency check', category: '工具', icon: 'sheet', input_mode: 'form', output_modes: ['json'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        await waitFor(() => expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull());
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('运行依赖工具')[0]);
        planMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'skill-app-disabled-runtime-tool-runtime-dep-tool', name: '运行依赖工具', kind: 'tool_app' }],
            dependencies: [{
                id: 'disabled-runtime-tool',
                kind: 'runtime_skill',
                required: true,
                installed: false,
                health: 'missing',
                action: 'blocked',
                app_ids: ['skill-app-disabled-runtime-tool-runtime-dep-tool'],
            }],
            has_missing_required: true,
            has_blocking_dependency: true,
        });
        installMaclawAppDependenciesMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'skill-app-disabled-runtime-tool-runtime-dep-tool', name: '运行依赖工具', kind: 'tool_app' }],
            dependencies: [{ id: 'disabled-runtime-tool', kind: 'runtime_skill', required: true, installed: true, action: 'installed', app_ids: ['skill-app-disabled-runtime-tool-runtime-dep-tool'] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        });

        fireEvent.click(screen.getByText('执行'));
        await waitFor(() => expect(screen.getByText('安装依赖并执行')).not.toBeNull());
        fireEvent.click(screen.getByText('安装依赖并执行'));

        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalled());
        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('disabled-runtime-tool', expect.objectContaining({
            app_id: 'skill-app-disabled-runtime-tool-runtime-dep-tool',
            app_kind: 'tool_app',
        })));
        await waitFor(() => {
            const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
            const entry = history['skill-app-disabled-runtime-tool-runtime-dep-tool']?.find((item) => item.runID === 'run-test-1');
            expect(entry?.dependencyVerification).toMatchObject({
                schema: 'maclaw.app.install_plan.v1',
                hasMissingRequired: false,
                hasBlockingDependency: false,
            });
            expect(entry?.dependencyVerification?.dependencies?.[0]).toMatchObject({
                id: 'disabled-runtime-tool',
                installed: true,
                action: 'installed',
            });
        });
    });
    it('blocks approval app runs when the workflow contract plan reports drift', async () => {
        const app = {
            id: 'local-approval-contract-drift-app',
            name: 'Approval Contract Drift',
            description: 'Approval app workflow contract drift',
            category: 'Finance',
            kind: 'enterprise_approval_app',
            icon: 'receipt',
            accent: '#2f5f98',
            source: 'local',
            pinned: false,
            recentUsedAt: '2026-06-17T00:00:00.000Z',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_approval_app',
                launchMode: 'approval_workspace',
                datasrv: { domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report', preferredAction: 'finance.expense_upsert' },
                workflow: { schema: 'maclaw.app.workflow.v1', submitNode: 'expense_report.submit', approvalNode: 'expense_report.manager_approval', resultNode: 'expense_report.result_feedback', attentionNode: 'expense_report.attention_review', statusMapping: { pending: 'approval_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' } },
                mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'expense-workflow', workflowVersion: '1.0.0', objectRole: 'expense_report' }] },
                dependencies: { skills: [{ id: 'expense-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }] },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
            recentUsedAtById: { [app.id]: app.recentUsedAt },
        }));
        planMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: 'enterprise_approval_app' }],
            dependencies: [{ id: 'expense-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            workflow_contract_issues: [{ path: 'apps[0].app.governance.workflowContract.workflowSkillId', severity: 'error', message: 'approval workflow contract does not match approval binding' }],
            has_workflow_contract_issue: true,
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        });
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getAllByText('Approval Contract Drift')[0]);
        fireEvent.click(screen.getByText('Run'));

        await waitFor(() => expect(screen.getAllByText(/Runtime contract needs attention/).length).toBeGreaterThan(0));
        expect(screen.getAllByText('approval workflow contract does not match approval binding').length).toBeGreaterThan(0);
        const runtimeStatus = document.querySelector('.apps-runtime-status') as HTMLElement;
        const workflowContract = runtimeStatus.querySelector('.apps-workflow-contract-summary') as HTMLElement;
        expect(workflowContract).not.toBeNull();
        expect(workflowContract.dataset.state).toBe('blocked');
        expect(within(workflowContract).getByText('Approval workflow')).not.toBeNull();
        expect(within(workflowContract).getByText('expense-workflow@v1.0.0')).not.toBeNull();
        expect(screen.queryByText('Install dependencies and run')).toBeNull();
        expect(installMaclawAppDependenciesMock).not.toHaveBeenCalled();
        expect(runNLSkillAsyncMock).not.toHaveBeenCalled();
        expect(recordMaclawAppApprovalInstanceMock).not.toHaveBeenCalled();
    });
    it('allows app runs even when the backend install plan reports governance review issues', async () => {
        const app = {
            id: 'local-governance-blocked-tool',
            name: 'Governance Blocked Tool',
            description: 'Tool app with stale governance evidence',
            category: 'Docs',
            kind: 'tool_app',
            icon: 'pdf',
            accent: '#2f5f98',
            source: 'local',
            pinned: false,
            recentUsedAt: '2026-06-17T00:00:00.000Z',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'skill',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                skill: {
                    id: 'governance-blocked-skill',
                    inputMode: 'form',
                    outputModes: ['pdf'],
                    fields: [{ name: 'title', label: 'Title', type: 'text', required: true }],
                },
                dependencies: { skills: [{ id: 'governance-blocked-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub' }] },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
            recentUsedAtById: { [app.id]: app.recentUsedAt },
        }));
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: 'tool_app' }],
            dependencies: [{ id: 'governance-blocked-skill', version: '1.0.0', kind: 'runtime_skill', required: true, installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            governance_review_issues: [{ path: 'apps[0].app.governance.testEvidence', severity: 'error', message: 'missing local run evidence' }],
            has_governance_review_issue: true,
            has_missing_required: false,
            has_blocking_dependency: false,
        });
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getAllByText('Governance Blocked Tool')[0]);
        fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Quarterly report' } });
        fireEvent.click(screen.getByText('Run'));

        // Governance review issues are publish-time quality gates and must NOT
        // block local runtime execution. The app should proceed to run since
        // all required dependencies are installed and healthy.
        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalled());
    });    it('installs missing workflow dependencies before starting approval app instances', async () => {
        const app = {
            id: 'local-approval-dep-app',
            name: '依赖审批应用',
            description: 'Approval app dependency repair',
            category: 'OA',
            kind: 'enterprise_approval_app',
            icon: 'receipt',
            accent: '#2f5f98',
            source: 'local',
            pinned: false,
            recentUsedAt: '2026-06-17T00:00:00.000Z',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_approval_app',
                launchMode: 'approval_workspace',
                datasrv: { domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report', preferredAction: 'finance.expense_upsert' },
                mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'missing-expense-workflow', workflowVersion: '1.0.0', objectRole: 'expense_report' }] },
                dependencies: { skills: [{ id: 'missing-expense-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }] },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
            recentUsedAtById: { [app.id]: app.recentUsedAt },
        }));
        planMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: 'enterprise_approval_app' }],
            dependencies: [{ id: 'missing-expense-workflow', kind: 'workflow_skill', required: true, installed: false, health: 'missing', action: 'blocked', app_ids: [app.id] }],
            has_missing_required: true,
            has_blocking_dependency: true,
        });
        installMaclawAppDependenciesMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: 'enterprise_approval_app' }],
            dependencies: [{ id: 'missing-expense-workflow', kind: 'workflow_skill', required: true, installed: true, action: 'installed', app_ids: [app.id] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        });
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('依赖审批应用')[0]);
        fireEvent.click(screen.getByText('执行'));
        await waitFor(() => expect(screen.getByText('安装依赖并执行')).not.toBeNull());
        fireEvent.click(screen.getByText('安装依赖并执行'));

        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('missing-expense-workflow', expect.objectContaining({
            app_id: app.id,
            app_kind: 'enterprise_approval_app',
            approval_event: 'expense.submitted',
            approval_object_role: 'expense_report',
            approval_workflow_skill_id: 'missing-expense-workflow',
        })));
        expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalled();
    });

    it('blocks enterprise normal app MIS operations when only appSkill is unavailable', async () => {
        const app = {
            id: 'local-business-dep-app',
            name: '依赖普通应用',
            description: 'Business app dependency gate',
            category: 'CRM',
            kind: 'enterprise_normal_app',
            icon: 'briefcase',
            accent: '#2f5f98',
            source: 'local',
            pinned: false,
            recentUsedAt: '2026-06-17T00:00:00.000Z',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_normal_app',
                launchMode: 'business_workspace',
                datasrv: { domain: 'crm', datasetID: 'crm.customers', objectRole: 'customer', preferredAction: 'crm.customer_upsert' },
                appSkill: { id: 'disabled-crm-action', name: 'CRM 写入动作', version: '1.0.0' },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
            recentUsedAtById: { [app.id]: app.recentUsedAt },
        }));
        planMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: 'enterprise_normal_app' }],
            dependencies: [{
                id: 'disabled-crm-action',
                version: '1.0.0',
                kind: 'runtime_skill',
                source: 'hub',
                required: true,
                installed: true,
                installed_status: 'disabled',
                health: 'disabled',
                action: 'blocked',
                app_ids: [app.id],
            }],
            has_missing_required: false,
            has_blocking_dependency: true,
        });
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('依赖普通应用')[0]);
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getAllByText('依赖普通应用暂不可用：disabled-crm-action 未安装或已停用。').length).toBeGreaterThan(0));
        const runtimeStatus = document.querySelector('.apps-runtime-status') as HTMLElement;
        const dependency = runtimeStatus.querySelector('.apps-result-panel .apps-install-record__dep[data-state="blocked"]') as HTMLElement;
        expect(dependency.textContent).toContain('disabled-crm-action');
        expect(dependency.dataset.state).toBe('blocked');
        expect(runNLSkillAsyncMock).not.toHaveBeenCalled();
    });

    it('does not block runtime when a blocking dependency belongs to another app', async () => {
        const app = {
            id: 'local-business-scoped-dep-app',
            name: 'Scoped Dependency App',
            description: 'Business app dependency scope gate',
            category: 'CRM',
            kind: 'enterprise_normal_app',
            icon: 'briefcase',
            accent: '#2f5f98',
            source: 'local',
            pinned: false,
            recentUsedAt: '2026-06-17T00:00:00.000Z',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_normal_app',
                launchMode: 'business_workspace',
                datasrv: { domain: 'crm', datasetID: 'crm.customers', objectRole: 'customer', preferredAction: 'crm.customer_upsert' },
                appSkill: { id: 'crm-action-ready', name: 'CRM action', version: '1.0.0' },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
            recentUsedAtById: { [app.id]: app.recentUsedAt },
        }));
        planMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [
                { id: app.id, name: app.name, kind: 'enterprise_normal_app' },
                { id: 'other-app', name: 'Other App', kind: 'enterprise_normal_app' },
            ],
            dependencies: [{
                id: 'other-blocked-skill',
                version: '1.0.0',
                kind: 'runtime_skill',
                source: 'hub',
                required: true,
                installed: false,
                health: 'missing',
                action: 'blocked',
                app_ids: ['other-app'],
            }],
            has_missing_required: true,
            has_blocking_dependency: true,
        });
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getAllByText('Scoped Dependency App')[0]);
        fireEvent.click(screen.getByText('Run'));

        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('crm-action-ready', expect.objectContaining({
            app_id: app.id,
            app_kind: 'enterprise_normal_app',
            datasrv_domain: 'crm',
            preferred_action: 'crm.customer_upsert',
        })));
        expect(screen.queryByText(/Scoped Dependency App is unavailable/)).toBeNull();
    });
    it('passes selected file metadata to skill app runs', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        const fileInput = container.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        const file = new File(['demo'], 'demo.pdf', { type: 'application/pdf' });
        fireEvent.change(fileInput, { target: { files: [file] } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(stageSkillAppInputFileMock).toHaveBeenCalledWith('demo.pdf', 'application/pdf', expect.any(Number), 'ZGVtbw=='));
        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('pdf-to-word', expect.objectContaining({
            file_name: 'demo.pdf',
            file_text: 'demo',
            file_path: '/tmp/demo.pdf',
            input_file_path: '/tmp/demo.pdf',
            file: expect.objectContaining({
                name: 'demo.pdf',
                size: 4,
                type: 'application/pdf',
                staged_path: '/tmp/demo.pdf',
                transfer: 'staged_file',
            }),
        })));
    });

    it('shows a run-again button after a skill app run completes', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        const fileInput = container.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        const file = new File(['demo'], 'demo.pdf', { type: 'application/pdf' });
        fireEvent.change(fileInput, { target: { files: [file] } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('pdf-to-word', expect.any(Object)));
        await waitFor(() => expect(document.querySelector('.apps-result-panel[data-state="done"]')).not.toBeNull());
        const actions = document.querySelector('.apps-runtime-actions') as HTMLElement;
        const runAgainButton = within(actions).getByText('重新执行') as HTMLButtonElement;
        expect(runAgainButton.disabled).toBe(false);
        expect(screen.queryByRole('progressbar')).toBeNull();
        await waitFor(() => {
            const activeRuns = JSON.parse(window.localStorage.getItem(activeRunStorageKey) || '{}') as Record<string, unknown>;
            expect(activeRuns['pdf-to-word']).toBeUndefined();
        });
    });

    it('disables the run button and shows running progress while a skill app is executing', async () => {
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-test-1',
            status: 'running',
            summary: { progress_text: '正在处理文件', artifact_path: '/tmp/pending.pdf', artifact_status: 'ready' },
            artifacts: [{ id: 'artifact-pending', uri: 'artifact://skill-run/run-test-1/artifact-pending', name: 'pending.pdf', path: '/tmp/pending.pdf', status: 'ready' }],
        });
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        const fileInput = container.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        const file = new File(['demo'], 'demo.pdf', { type: 'application/pdf' });
        fireEvent.change(fileInput, { target: { files: [file] } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('pdf-to-word', expect.any(Object)));
        const actions = document.querySelector('.apps-runtime-actions') as HTMLElement;
        // While a run is active the run button is hidden (not merely
        // disabled); cancellation stays available instead.
        expect(within(actions).queryByText('执行')).toBeNull();
        expect(within(actions).getByText('取消执行')).not.toBeNull();
        expect(document.querySelector('.apps-result-panel[data-state="running"] .apps-result-progress')).not.toBeNull();
        expect(screen.getByRole('progressbar')).not.toBeNull();
        expect(document.querySelector('.apps-runtime-output')?.hasAttribute('hidden')).toBe(true);
        expect(screen.queryByText('pending.pdf')).toBeNull();
        expect(screen.queryByText('/tmp/pending.pdf')).toBeNull();
        expect(screen.queryByRole('button', { name: '打开' })).toBeNull();
        expect(screen.queryByRole('button', { name: '定位' })).toBeNull();
        await waitFor(() => {
            const activeRuns = JSON.parse(window.localStorage.getItem(activeRunStorageKey) || '{}') as Record<string, any>;
            expect(activeRuns['pdf-to-word']).toMatchObject({
                appID: 'pdf-to-word',
                runID: 'run-test-1',
                runKind: 'tool_skill',
            });
        });
    });

    it('keeps the result region hidden when a skill app run fails with partial artifacts', async () => {
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-test-1',
            status: 'failed',
            error: '转换失败',
            summary: { last_error_snippet: '转换失败', artifact_path: '/tmp/failed.pdf', artifact_status: 'ready' },
            artifacts: [{ id: 'artifact-failed', uri: 'artifact://skill-run/run-test-1/artifact-failed', name: 'failed.pdf', path: '/tmp/failed.pdf', status: 'ready' }],
        });
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        const fileInput = container.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        const file = new File(['demo'], 'demo.pdf', { type: 'application/pdf' });
        fireEvent.change(fileInput, { target: { files: [file] } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(document.querySelector('.apps-result-panel[data-state="error"]')).not.toBeNull());
        expect(screen.getAllByText('转换失败').length).toBeGreaterThan(0);
        expect(document.querySelector('.apps-runtime-output')?.hasAttribute('hidden')).toBe(true);
        expect(screen.queryByText('failed.pdf')).toBeNull();
        expect(screen.queryByText('/tmp/failed.pdf')).toBeNull();
        expect(screen.queryByRole('button', { name: '打开' })).toBeNull();
        expect(screen.queryByRole('button', { name: '定位' })).toBeNull();
    });

    it('treats empty skill run ids as failed starts', async () => {
        runNLSkillAsyncMock.mockResolvedValueOnce('');
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        const fileInput = document.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        const file = new File(['demo'], 'demo.pdf', { type: 'application/pdf' });
        fireEvent.change(fileInput, { target: { files: [file] } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getAllByText('Skill 执行失败').length).toBeGreaterThan(0));
        expect(screen.queryByText('Skill 执行')).toBeNull();
        expect(screen.getByText('运行历史')).not.toBeNull();
        expect(screen.getByText(/failed-/)).not.toBeNull();
    });

    it('passes multiple selected files to skill app runs when enabled', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'multi-doc', skill_id: 'multi-tools', name: '多文档处', description: 'Multi files', category: '文档处理', icon: 'contract', input_mode: 'file', multiple_files: true, output_modes: ['pdf'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('多文档处')[0]);
        const fileInput = container.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        const first = new File(['one'], 'one.pdf', { type: 'application/pdf' });
        const second = new File(['two'], 'two.pdf', { type: 'application/pdf' });
        fireEvent.change(fileInput, { target: { files: [first, second] } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(stageSkillAppInputFileMock).toHaveBeenCalledTimes(2));
        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('multi-tools', expect.objectContaining({
            file_name: 'one.pdf, two.pdf',
            file_path: '/tmp/one.pdf',
            file_paths: ['/tmp/one.pdf', '/tmp/two.pdf'],
            files: [
                expect.objectContaining({ name: 'one.pdf', staged_path: '/tmp/one.pdf', transfer: 'staged_file' }),
                expect.objectContaining({ name: 'two.pdf', staged_path: '/tmp/two.pdf', transfer: 'staged_file' }),
            ],
        })));
    });

    it('blocks oversized files before staging tool app runs', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        const fileInput = container.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        const file = new File(['x'], 'huge.pdf', { type: 'application/pdf' });
        Object.defineProperty(file, 'size', { value: 26 * 1024 * 1024 });
        fireEvent.change(fileInput, { target: { files: [file] } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getByText('文件超过 25MB，暂不支持此方式上传')).not.toBeNull());
        expect(stageSkillAppInputFileMock).not.toHaveBeenCalled();
        expect(runNLSkillAsyncMock).not.toHaveBeenCalled();
    });

    it('renders skill run steps and artifact status', async () => {
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-test-1',
            status: 'success',
            total_steps: 2,
            expected_artifact: true,
            steps: [
                { index: 0, name: '读取文件', action: 'read', status: 'success', output: 'loaded', duration_ms: 12 },
                { index: 1, name: '生成文档', action: 'write', status: 'success', output: 'created', duration_ms: 34 },
            ],
            summary: { last_output_snippet: 'done', artifact_path: '/tmp/out.docx', artifact_status: 'ready' },
            artifacts: [
                { id: 'artifact-1', uri: 'artifact://skill-run/run-test-1/artifact-1', name: 'out.docx', path: '/tmp/out.docx', status: 'ready' },
                { id: 'artifact-2', uri: 'artifact://skill-run/run-test-1/artifact-2', name: 'report.pdf', path: '/tmp/report.pdf', status: 'ready', mime_type: 'application/pdf', size_bytes: 2048 },
            ],
            outputs: [
                { id: 'artifact-1', kind: 'artifact', title: '文件产物', artifact_id: 'artifact-1', artifact: { id: 'artifact-1', uri: 'artifact://skill-run/run-test-1/artifact-1', name: 'out.docx', path: '/tmp/out.docx', status: 'ready' } },
                { id: 'file-1', kind: 'file', title: '文件输出', artifact_id: 'artifact-1', artifact: { id: 'artifact-1', uri: 'artifact://skill-run/run-test-1/artifact-1', name: 'out.docx', path: '/tmp/out.docx', status: 'ready' } },
	                { id: 'summary-1', kind: 'text', title: '摘要', text: '生成完成摘要', status: 'success' },
            ],
        });
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'evidence-tool', skill_id: 'evidence-tools', name: '证据工具', description: 'Evidence', category: '工具', icon: 'sheet', input_mode: 'form', output_modes: ['docx'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('证据工具')[0]);
        fireEvent.change(screen.getByPlaceholderText('输入处理要求或表单参数。'), { target: { value: '生成文档' } });
        fireEvent.click(screen.getByText('执行'));

	        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('evidence-tools', expect.objectContaining({
	            prompt: expect.stringContaining('生成文档'),
	        })));
	        expect(screen.queryByText('文件产物')).toBeNull();
	        expect(screen.queryByText('文件输出')).toBeNull();
	        expect(screen.getByText('运行历史')).not.toBeNull();
	    });

    it('downloads remote-only artifact before opening it', async () => {
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-test-1',
            status: 'success',
            expected_artifact: true,
            summary: { last_output_snippet: 'done', artifact_status: 'ready' },
            artifacts: [{
                id: 'remote-1',
                uri: 'artifact://skill-run/run-test-1/remote-1',
                name: 'remote.pdf',
                remote_url: 'https://hub.example/artifacts/remote.pdf',
                download_state: 'remote',
                status: 'ready',
            }],
        });
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'remote-tool', skill_id: 'remote-tools', name: '远端产物工具', description: 'Remote', category: '工具', icon: 'sheet', input_mode: 'form', output_modes: ['pdf'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('远端产物工具')[0]);
        fireEvent.change(screen.getByPlaceholderText('输入处理要求或表单参数。'), { target: { value: '生成 PDF' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getAllByText('下载并打开').length).toBeGreaterThan(0));
        fireEvent.click(screen.getAllByText('下载并打开')[0]);
        await waitFor(() => expect(downloadSkillRunArtifactMock).toHaveBeenCalledWith('run-test-1', 'remote-1'));
        expect(openSkillRunArtifactMock).toHaveBeenCalledWith('run-test-1', 'remote-1');
        expect(recordMaclawAppRunEvidenceForSkillMock).toHaveBeenCalledWith('remote-tools', expect.any(String), expect.any(String), 'run-test-1', 'remote.pdf', expect.any(String));
    });

    it('keeps tool app run history capped at eight entries', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'history-tool', skill_id: 'history-tools', name: '历史工具', description: 'History', category: '工具', icon: 'sheet', input_mode: 'form', output_modes: ['txt'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('历史工具')[0]);

        for (let index = 0; index < 9; index += 1) {
            runNLSkillAsyncMock.mockResolvedValueOnce(`run-history-${index}`);
            getNLSkillRunStatusMock.mockResolvedValueOnce({ run_id: `run-history-${index}`, status: 'success', summary: { last_output_snippet: `done-${index}` } });
            fireEvent.change(screen.getByPlaceholderText('输入处理要求或表单参数。'), { target: { value: `任务 ${index}` } });
            fireEvent.click(screen.getByText('执行'));
            await waitFor(() => expect(screen.getByText(new RegExp(`run-history-${index}`))).not.toBeNull());
        }

        expect(screen.getByText('8')).not.toBeNull();
        expect(screen.getAllByText(/run-history-8/).length).toBeGreaterThan(0);
        expect(screen.queryByText(/run-history-0/)).toBeNull();
    });

    it('can cancel a running tool app skill run', async () => {
        let resolveCancel: (() => void) | undefined;
        cancelNLSkillRunMock.mockImplementationOnce(() => new Promise<void>((resolve) => { resolveCancel = resolve; }));
        getNLSkillRunStatusMock.mockReturnValue(new Promise(() => undefined));
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'long-tool', skill_id: 'long-tools', name: '长任务工', description: 'Long run', category: '工具', icon: 'sync', input_mode: 'form', output_modes: ['txt'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('长任务工')[0]);
        fireEvent.change(screen.getByPlaceholderText('输入处理要求或表单参数。'), { target: { value: '持续处理' } });
        fireEvent.click(screen.getByText('执行'));

        const cancelButton = await screen.findByText('取消执行');
        expect(screen.queryByText('执行')).toBeNull();
        fireEvent.click(cancelButton);
        fireEvent.click(cancelButton);

        await waitFor(() => expect(cancelNLSkillRunMock).toHaveBeenCalledWith('run-test-1'));
        expect(cancelNLSkillRunMock).toHaveBeenCalledTimes(1);
        expect((cancelButton as HTMLButtonElement).disabled).toBe(true);
        await act(async () => { resolveCancel?.(); });
        await waitFor(() => expect(screen.getAllByText('Skill 已取消').length).toBeGreaterThan(0));
    });

    it('keeps the run active when cancellation is rejected', async () => {
        cancelNLSkillRunMock.mockRejectedValueOnce(new Error('connection lost'));
        getNLSkillRunStatusMock.mockReturnValue(new Promise(() => undefined));
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: { value: JSON.stringify({ x_maclaw_apps: 'v1', apps: [{ id: 'cancel-failure-tool', skill_id: 'cancel-failure-tools', name: 'Cancel failure tool', description: 'Cancellation failure', category: 'Tools', icon: 'sync', input_mode: 'form', output_modes: ['txt'] }] }) },
        });
        fireEvent.click(screen.getByText('Install'));
        fireEvent.click(screen.getByText('Close'));
        fireEvent.click(screen.getAllByText('Cancel failure tool')[0]);
        fireEvent.change(screen.getByPlaceholderText('Enter processing instructions or form parameters.'), { target: { value: 'Continue processing' } });
        fireEvent.click(screen.getByText('Run'));

        const cancelButton = await screen.findByText('Cancel run');
        fireEvent.click(cancelButton);

        await waitFor(() => expect(cancelNLSkillRunMock).toHaveBeenCalledWith('run-test-1'));
        await screen.findByText('Cancellation failed: connection lost');
        expect(screen.queryByText('Run')).toBeNull();
        expect(screen.getByText('Cancel run')).not.toBeNull();
    });

    it('renders mixed tool apps with file and parameter inputs', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'mixed-tool', skill_id: 'mixed-tools', name: '混合工具', description: 'Mixed input', category: '工具', icon: 'contract', input_mode: 'mixed', output_modes: ['pdf'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('混合工具')[0]);

        expect(container.querySelector('.apps-drop-zone')).not.toBeNull();
        expect(screen.getByPlaceholderText('输入处理要求或表单参数。')).not.toBeNull();
    });

    it('runs an enterprise app with dynamic DataSrv capability binding', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.change(screen.getByDisplayValue('财务审批'), { target: { value: '费用报销' } });
        fireEvent.change(screen.getByDisplayValue('新建记录'), { target: { value: 'report' } });
        fireEvent.change(container.querySelector('.apps-preview__mock textarea') as HTMLTextAreaElement, { target: { value: '生成本月报销汇' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalled());
        expect(screen.getByText(/费用报销 · report · finance.expense_upsert/)).not.toBeNull();
    });

	it('renders enterprise normal apps as business workspaces without approval instances', async () => {
		executeMaclawAppBusinessOperationMock.mockResolvedValueOnce({
			synced: true,
            mode: 'business_view',
            target: 'purchase_order.list',
            result_status: 'done',
            primary_result: 'business_record',
            result_payload: { business_status: 'done', business_record: { id: 'PO-100', status: 'open' }, text: 'PO review ready' },
            outputs: [{ kind: 'business_record', title: 'PO review package', text: 'PO review ready', status: 'ready', data: { id: 'PO-100', status: 'open' } }],
            artifacts: [{ id: 'po-review-export', name: 'po-review.xlsx', uri: 'artifact://po/review.xlsx', status: 'ready' }],
            response: { legacy: true },
        });
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('采购入库')[0]);

        expect(container.querySelector('.apps-business-workspace')).not.toBeNull();
        expect(container.querySelector('.apps-approval-workspace')).toBeNull();
        expect(screen.getByText('业务工作台')).not.toBeNull();
        expect(screen.getAllByText('purchase_order').length).toBeGreaterThan(0);
        expect(screen.getAllByText('supply.purchase_orders').length).toBeGreaterThan(0);
        expect(screen.getAllByText('purchase_order.upsert').length).toBeGreaterThan(0);

        fireEvent.change(screen.getByDisplayValue('新建记录'), { target: { value: 'query' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(executeMaclawAppBusinessOperationMock).toHaveBeenCalledWith(expect.objectContaining({
            app_id: 'purchase-inbound',
            object_role: 'purchase_order',
            business_action: 'query',
            preferred_action: 'purchase_order.upsert',
            preferred_view: 'purchase_order.list',
            preferred_report: 'purchase_order.report',
            preferred_dashboard: '',
        })));
        expect(recordMaclawAppApprovalInstanceMock).not.toHaveBeenCalled();
        await waitFor(() => expect(screen.getByText('PO review ready')).not.toBeNull());
        expect(screen.getByText('business_view')).not.toBeNull();
        expect(screen.getByText('结果类型')).not.toBeNull();
        expect(screen.getAllByText('business_record').length).toBeGreaterThan(0);
        expect(screen.getByText('记录')).not.toBeNull();
        expect(screen.getByText('PO-100')).not.toBeNull();
        expect(screen.queryByText('PO-101')).toBeNull();
		expect(container.querySelector('.apps-run-history')).not.toBeNull();
        const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
		expect(history['purchase-inbound']?.[0]).toMatchObject({
            status: 'done',
            outputMode: 'business',
            resultPayload: {
                mode: 'business_view',
                target: 'purchase_order.list',
                result_status: 'done',
                business_status: 'done',
                business_record: { id: 'PO-100', status: 'open' },
                text: 'PO review ready',
            },
            outputs: [expect.objectContaining({ kind: 'business_record', title: 'PO review package', status: 'ready' })],
            artifacts: [expect.objectContaining({ id: 'po-review-export', name: 'po-review.xlsx', uri: 'artifact://po/review.xlsx' })],
            resultCoverage: {
                ok: true,
                primary: 'business_status',
                coveredTypes: expect.arrayContaining(['business_status', 'business_record', 'content', 'artifact', 'document']),
                missingTypes: [],
            },
        });
		});

    it('publishes enterprise normal app evidence from an actual DataSrv business run', async () => {
        const app = {
            id: 'normal-run-publish-app',
            name: '客户续约工作',
            description: 'Run and publish a normal enterprise app with standard result package',
            category: 'CRM',
            kind: 'enterprise_normal_app',
            icon: 'customer',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_normal_app',
                launchMode: 'agent_dynamic_ui',
                dependencies: { skills: [{ id: 'customer-renewal-runtime', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub', install_ref: 'cap-customer-renewal-runtime' }] },
	                datasrv: { domain: 'sales', datasetID: 'sales.customers', objectRole: 'customer', blueprintID: 'sales.customer.renewal', preferredAction: 'sales.customer_renewal_upsert', preferredView: 'sales.customer_renewal_review' },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'business_workspace',
                    layouts: {
                        business_workspace: {
                            template: 'classic_split',
                            density: 'compact',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            regions: [
                                { id: 'operation_form', role: 'input', placement: 'left' },
                                { id: 'record_list', role: 'record_list', placement: 'center' },
                                { id: 'record_detail', role: 'detail', placement: 'center' },
                                { id: 'output_panel', role: 'output', placement: 'right' },
                            ],
                            studio: { savedInManifest: true },
                        },
                    },
                },
                resultContract: {
                    schema: 'maclaw.app.result.v1',
                    primary: 'business_status',
                    types: ['business_status', 'business_record', 'content', 'artifact'],
                    delivery: { inlineContent: true, artifacts: true, businessRecord: true, notifications: false },
                },
            },
        };
        const dependencyPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{ id: 'customer-renewal-runtime', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub', install_ref: 'cap-customer-renewal-runtime', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_workflow_contract_issue: false,
            has_governance_review_issue: false,
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        planMaclawAppInstallMock.mockResolvedValue(dependencyPlan);
        executeMaclawAppBusinessOperationMock.mockResolvedValueOnce({
            synced: true,
            mode: 'business_view',
            target: 'sales.customer_renewal_review',
            result_status: 'ready',
            business_status: 'ready',
            primary_result: 'business_record',
            result_payload: {
                business_status: 'ready',
                result_status: 'ready',
                business_record: { id: 'CUS-100', renewalStage: 'legal_review' },
                text: 'Customer renewal package ready',
            },
            outputs: [{ kind: 'business_record', title: 'Renewal record', text: 'Customer renewal package ready', status: 'ready', data: { id: 'CUS-100', renewalStage: 'legal_review' } }],
            artifacts: [{ id: 'renewal-report', name: 'renewal-report.pdf', uri: 'artifact://customer/renewal-report.pdf', status: 'ready', mime_type: 'application/pdf', size_bytes: 4096 }],
            response: { legacy: 'kept' },
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'normal-run-result-package', submitted_at: '2026-06-17T01:00:00.000Z', status: 'submitted', message: 'queued' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('客户续约工作')[0]);
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(executeMaclawAppBusinessOperationMock).toHaveBeenCalled());
        await waitFor(() => {
            const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
            expect(history[app.id]?.[0]).toMatchObject({
                status: 'done',
                definitionHash: expect.stringMatching(/^[0-9a-f]{8}$/),
                dependencyVerification: {
                    schema: 'maclaw.app.install_plan.v1',
                    dependencyCount: 1,
                    hasBlockingDependency: false,
                    dependencies: [expect.objectContaining({ id: 'customer-renewal-runtime', installed: true, health: 'ready' })],
                },
                resultPayload: expect.objectContaining({
                    business_status: 'ready',
                    result_status: 'ready',
                    business_record: { id: 'CUS-100', renewalStage: 'legal_review' },
                    text: 'Customer renewal package ready',
                }),
                outputs: [expect.objectContaining({ kind: 'business_record', title: 'Renewal record', status: 'ready' })],
                artifacts: [expect.objectContaining({ id: 'renewal-report', name: 'renewal-report.pdf' })],
            });
        });

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        const readyCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('客户续约工作')) as HTMLElement;
        expect(readyCard).toBeTruthy();
        await waitFor(() => expect(within(readyCard).getByText('\u53ef\u63d0\u4ea4')).not.toBeNull());
        await waitFor(() => expect(within(readyCard).getByText('一键发布')).not.toBeNull());
        fireEvent.click(within(readyCard).getByText('一键发布'));

	        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
	        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
	        const packagedApp = payload.apps[0].app;
	        const governance = packagedApp.governance;
	        expect(packagedApp.binding.datasrv).toMatchObject({
	            domain: 'sales',
	            datasetID: 'sales.customers',
	            objectRole: 'customer',
	            blueprintID: 'sales.customer.renewal',
	            preferredAction: 'sales.customer_renewal_upsert',
	            preferredView: 'sales.customer_renewal_review',
	        });
	        expect(governance.dependencyVerification).toMatchObject({
            schema: 'maclaw.app.install_plan.v1',
            dependencyCount: 1,
            hasBlockingDependency: false,
            dependencies: [expect.objectContaining({ id: 'customer-renewal-runtime', install_ref: 'cap-customer-renewal-runtime' })],
        });
        expect(governance.testEvidence.resultPayload).toEqual(expect.objectContaining({
            business_status: 'ready',
            result_status: 'ready',
            business_record: { id: 'CUS-100', renewalStage: 'legal_review' },
            text: 'Customer renewal package ready',
        }));
        expect(governance.testEvidence.outputs).toEqual([expect.objectContaining({
            kind: 'business_record',
            title: 'Renewal record',
            status: 'ready',
            data: { id: 'CUS-100', renewalStage: 'legal_review' },
        })]);
        expect(governance.testEvidence.artifacts).toEqual([expect.objectContaining({
            id: 'renewal-report',
            name: 'renewal-report.pdf',
            mimeType: 'application/pdf',
            sizeBytes: 4096,
        })]);
        expect(governance.testEvidence.resultCoverage).toEqual(expect.objectContaining({
            ok: true,
            primary: 'business_status',
            coveredTypes: expect.arrayContaining(['business_status', 'business_record', 'content', 'artifact']),
            missingTypes: [],
        }));
    });

		it('shows structured business errors for enterprise normal app operations', async () => {
		executeMaclawAppBusinessOperationMock.mockRejectedValueOnce(new Error(JSON.stringify({
			error: 'invalid input: Approval approval-1 cannot be reviewed because it is already approved.',
			code: 'approval_not_pending',
			message: 'Approval approval-1 cannot be reviewed because it is already approved.',
			target: 'Approve confirmed sales order',
			required: 'approval_status = pending',
			actual: 'approval_status = approved',
			next_actions: [
				{ label: 'View approval detail', action: 'get_record_approval', args: { approval_id: 'approval-1' } },
				{ label: 'Refresh approval list', action: 'list_record_approvals', args: { status: 'approved' } },
			],
			metadata: { approval_id: 'approval-1' },
		})));
		render(<AppsPage lang="zh-Hans" />);

		fireEvent.click(screen.getAllByText('采购入库')[0]);
		fireEvent.change(screen.getByDisplayValue('新建记录'), { target: { value: 'query' } });
		fireEvent.click(screen.getByText('执行'));

		await waitFor(() => expect(screen.getByText('approval_not_pending')).not.toBeNull());
		const runtimeStatus = document.querySelector('.apps-runtime-status') as HTMLElement;
		expect(within(runtimeStatus).getByText('Approval approval-1 cannot be reviewed because it is already approved.')).not.toBeNull();
		expect(within(runtimeStatus).getByText('approval_status = pending')).not.toBeNull();
		expect(within(runtimeStatus).getByText('approval_status = approved')).not.toBeNull();
		expect(within(runtimeStatus).getByText('View approval detail')).not.toBeNull();
		expect(within(runtimeStatus).getByText('Refresh approval list')).not.toBeNull();
	});

	it('runs enterprise normal appSkills with MIS business payloads', async () => {
		render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'enterprise_app_pack',
                    app: {
                        id: 'customer-op',
                        name: '客户操作',
                        description: 'Run customer operations through a business skill',
                        category: 'CRM',
                        kind: 'enterprise_normal_app',
                        icon: 'customer',
                        launchMode: 'agent_dynamic_ui',
                        binding: {
                            appSkill: { id: 'customer-business-skill', version: '1.0.0', source: 'hub' },
                            datasrv: { domain: 'sales', objectRole: 'customer', preferredAction: 'sales.customer_upsert', preferredView: 'sales.customer_directory' },
                        },
                    },
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        await waitFor(() => expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull());
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('客户操作')[0]);
        fireEvent.change(screen.getByPlaceholderText('输入业务意图，Agent 生成动态界面并通过 DataSrv 执行。'), { target: { value: 'complete customer contact' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('customer-business-skill', expect.objectContaining({
            app_id: 'market-customer-op',
            app_kind: 'enterprise_normal_app',
            business_entity: 'CRM',
            business_action: 'create',
            business_note: 'complete customer contact',
            object_role: 'customer',
            action_role: 'sales.customer_upsert',
            datasrv_domain: 'sales',
        })));
    });

    it('renders backend dashboard workspace layouts and keeps them editable', async () => {
        const app = {
            id: 'ops-dashboard-app',
            name: 'Operations Dashboard',
            description: 'Business workspace with dashboard layout',
            category: 'Operations',
            kind: 'enterprise_normal_app',
            icon: 'dashboard',
            accent: '#4b6572',
            pinned: true,
            source: 'market',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_normal_app',
                launchMode: 'agent_dynamic_ui',
                datasrv: { domain: 'ops', objectRole: 'ticket' },
                appSkill: { id: 'ops-dashboard-skill', version: '1.0.0', source: 'hub' },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'business_workspace',
                    layouts: {
                        business_workspace: {
                            type: 'split_view',
                            template: 'dashboard',
                            density: 'spacious',
                            primaryRegion: 'center',
                            outputRegion: 'modal',
                            regions: [
                                { id: 'operation_form', role: 'input', placement: 'center' },
                                { id: 'record_list', role: 'record_list', placement: 'left' },
                                { id: 'record_detail', role: 'detail', placement: 'center' },
                                { id: 'output_panel', role: 'output', placement: 'modal' },
                            ],
                            studio: { savedInManifest: true },
                        },
                    },
                },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getAllByText('Operations Dashboard')[0]);
        await waitFor(() => expect(document.querySelector('.apps-runtime-layout')).not.toBeNull());
        const runtimeLayout = document.querySelector('.apps-runtime-layout') as HTMLElement;
        expect(runtimeLayout.dataset.template).toBe('dashboard');
        expect(runtimeLayout.dataset.density).toBe('spacious');
        expect(runtimeLayout.dataset.primaryRegion).toBe('center');
        expect(runtimeLayout.dataset.outputRegion).toBe('modal');
        expect(document.querySelector('.apps-runtime-input')?.getAttribute('data-region')).toBe('center');
        expect(document.querySelector('.apps-runtime-output')?.getAttribute('data-region')).toBe('modal');

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('Operations Dashboard')) as HTMLElement;
        fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
        const dialog = screen.getByRole('dialog');
        expect(within(dialog).getByTestId('edit-layout-template-dashboard').getAttribute('aria-pressed')).toBe('true');
        expect((within(dialog).getByTestId('edit-layout-density') as HTMLSelectElement).value).toBe('spacious');
    });
    it('saves enterprise normal business bindings from App Studio into the manifest', async () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByRole('button', { name: /^Business app$|^企业普通应用?/ }));
        fireEvent.change(screen.getByPlaceholderText('Example: Contract filing'), { target: { value: 'Customer Console' } });
        fireEvent.change(screen.getByTestId('studio-business-domain'), { target: { value: 'sales' } });
        fireEvent.change(screen.getByTestId('studio-business-object-role'), { target: { value: 'customer' } });
        fireEvent.change(screen.getByTestId('studio-business-preferred-action'), { target: { value: 'sales.customer_upsert' } });
        fireEvent.change(screen.getByTestId('studio-business-preferred-view'), { target: { value: 'sales.customer_directory' } });
        fireEvent.change(screen.getByTestId('studio-business-preferred-report'), { target: { value: 'sales.customer_activity' } });
        fireEvent.change(screen.getByTestId('studio-business-preferred-dashboard'), { target: { value: 'sales.overview' } });
        fireEvent.change(screen.getByTestId('studio-ui-navigation-0'), { target: { value: 'customers' } });
        fireEvent.change(screen.getByTestId('studio-ui-column-0'), { target: { value: 'customer_name' } });
        fireEvent.click(screen.getByRole('button', { name: 'Create app' }));

        let created: any;
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            created = (stored.customApps || []).find((app: any) => app.name === 'Customer Console');
            expect(created).toBeTruthy();
        });
        expect(created.kind).toBe('enterprise_normal_app');
        expect(created.manifest.datasrv).toMatchObject({
            domain: 'sales',
            objectRole: 'customer',
            preferredAction: 'sales.customer_upsert',
            preferredView: 'sales.customer_directory',
            preferredReport: 'sales.customer_activity',
            preferredDashboard: 'sales.overview',
        });
        expect(created.manifest.resultContract).toMatchObject({
            schema: 'maclaw.app.result.v1',
            primary: 'business_status',
        });
        expect(created.manifest.resultContract.types).toEqual(expect.arrayContaining(['business_status', 'business_record', 'document']));
        expect(created.manifest.ui.layouts.business_workspace.studio.savedInManifest).toBe(true);
        expect(created.manifest.ui.layouts.business_workspace.navigation).toEqual(expect.arrayContaining(['customers']));
        expect(created.manifest.ui.layouts.business_workspace.list.columns).toEqual(expect.arrayContaining(['customer_name']));
    });

    it('uses visual enterprise UI navigation and columns at runtime', () => {
        const app = {
            id: 'visual-business-ui-app',
            name: 'Visual Business UI',
            description: 'Runtime reads visual UI metadata',
            category: 'CRM',
            kind: 'enterprise_normal_app',
            icon: 'customer',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_normal_app',
                launchMode: 'agent_dynamic_ui',
                appSkill: { id: 'visual-business-skill', version: '1.0.0', source: 'local' },
                datasrv: { domain: 'sales', objectRole: 'customer', preferredAction: 'sales.customer_upsert' },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    entry: 'business_workspace',
                    layouts: {
                        business_workspace: {
                            navigation: ['customers', 'renewals'],
                            list: { columns: ['customer_name', 'status', 'updated_at'] },
                        },
                    },
                },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getAllByText('Visual Business UI')[0]);

        const workspace = document.querySelector('.apps-business-workspace') as HTMLElement;
        expect(within(workspace).getByRole('button', { name: 'customers' })).not.toBeNull();
        expect(within(workspace).getByRole('button', { name: 'renewals' })).not.toBeNull();
        expect(within(workspace).getByText('customer_name')).not.toBeNull();
        expect(within(workspace).getByText('Updated')).not.toBeNull();
    });

    it('edits enterprise normal business bindings and uses them in appSkill payloads', async () => {
        const app = {
            id: 'ops-console',
            name: 'Ops Console',
            description: 'Operate service records through a business skill',
            category: 'Operations',
            kind: 'enterprise_normal_app',
            icon: 'sheet',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_normal_app',
                launchMode: 'agent_dynamic_ui',
                appSkill: { id: 'ops-business-skill', version: '1.0.0', source: 'local' },
                datasrv: { domain: 'ops', objectRole: 'ticket', preferredAction: 'ops.ticket_upsert', preferredView: 'ops.ticket_list' },
                ui: { schema: 'maclaw.app.ui.v1' },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        const { unmount } = render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('Ops Console')) as HTMLElement;
        fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
        fireEvent.change(screen.getByTestId('edit-business-domain'), { target: { value: 'service' } });
        fireEvent.change(screen.getByTestId('edit-business-object-role'), { target: { value: 'case' } });
        fireEvent.change(screen.getByTestId('edit-business-preferred-action'), { target: { value: 'service.case_upsert' } });
        fireEvent.change(screen.getByTestId('edit-business-preferred-view'), { target: { value: 'service.case_queue' } });
        fireEvent.change(screen.getByTestId('edit-business-preferred-report'), { target: { value: 'service.case_report' } });
        fireEvent.change(screen.getByTestId('edit-business-preferred-dashboard'), { target: { value: 'service.overview' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            const updated = stored.customApps.find((item: any) => item.id === 'ops-console');
            expect(updated.manifest.datasrv).toMatchObject({
                domain: 'service',
                objectRole: 'case',
                preferredAction: 'service.case_upsert',
                preferredView: 'service.case_queue',
                preferredReport: 'service.case_report',
                preferredDashboard: 'service.overview',
            });
            expect(updated.manifest.resultContract.primary).toBe('business_status');
            expect(updated.manifest.resultContract.delivery.businessRecord).toBe(true);
        });

        unmount();
        render(<AppsPage lang="en" />);
        fireEvent.click(screen.getAllByText('Ops Console')[0]);
        fireEvent.click(screen.getByRole('button', { name: 'Run' }));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('ops-business-skill', expect.objectContaining({
            app_id: 'ops-console',
            app_kind: 'enterprise_normal_app',
            object_role: 'case',
            action_role: 'service.case_upsert',
            datasrv_domain: 'service',
            preferred_action: 'service.case_upsert',
            preferred_view: 'service.case_queue',
            preferred_report: 'service.case_report',
            preferred_dashboard: 'service.overview',
        })));
    });

    it('edits visual result contract settings and persists them', async () => {
        const app = {
            id: 'result-contract-edit-app',
            name: 'Result Contract Editor',
            description: 'Edit result contract visually',
            category: 'Document',
            kind: 'tool_app',
            icon: 'contract',
            accent: '#4b6572',
            pinned: false,
            source: 'local',
            version: 1,
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'skill_app',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'skill_app',
                skill: { id: 'contract-tool', inputMode: 'file', outputModes: ['pdf'] },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'artifact', types: ['content', 'document', 'artifact'], outputModes: ['pdf'], delivery: { inlineContent: true, artifacts: true, businessRecord: false, notifications: false } },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('Result Contract Editor')) as HTMLElement;
        fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
        fireEvent.change(screen.getByTestId('edit-result-primary'), { target: { value: 'content' } });
        fireEvent.click(screen.getByTestId('edit-result-delivery-artifacts'));
        fireEvent.change(screen.getByTestId('edit-test-risk'), { target: { value: 'medium' } });
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            const updated = stored.customApps.find((item: any) => item.id === app.id);
            expect(updated.manifest.resultContract.primary).toBe('content');
            expect(updated.manifest.resultContract.delivery.artifacts).toBe(false);
            expect(updated.manifest.testProtocol.schema).toBe('maclaw.app.test_protocol.v1');
            expect(updated.manifest.testProtocol.riskLevel).toBe('medium');
            expect(updated.manifest.testProtocol.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        });
    });

    it('persists app order changes from app studio management', async () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const manageRows = document.querySelectorAll('.apps-manage-row');
        expect(manageRows[0]?.textContent).toContain('报销申请');
        expect(manageRows[1]?.textContent).toContain('采购入库');
        fireEvent.click(within(manageRows[1] as HTMLElement).getByTitle('上移'));

        // Panel persistence is asynchronous (SQLite-gated); wait for the new
        // order to be durable before remounting.
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            expect((stored.orderedIds || [])[0]).toBe('purchase-inbound');
        });
        unmount();
        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const nextRows = document.querySelectorAll('.apps-manage-row');
        expect(nextRows[0]?.textContent).toContain('采购入库');
        expect(nextRows[1]?.textContent).toContain('报销申请');
    });

    it('moves apps directly to the top and bottom from app studio management', async () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const inventoryRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('库存盘点')) as HTMLElement;
        fireEvent.click(within(inventoryRow).getByTitle('移到顶部'));

        let rows = document.querySelectorAll('.apps-manage-row');
        expect(rows[0]?.textContent).toContain('库存盘点');

        fireEvent.click(within(rows[0] as HTMLElement).getByTitle('移到底部'));

        rows = document.querySelectorAll('.apps-manage-row');
        expect(rows[rows.length - 1]?.textContent).toContain('库存盘点');

        // Wait for the asynchronous panel persistence before remounting.
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            const ids = stored.orderedIds || [];
            expect(ids[ids.length - 1]).toBe('inventory-count');
        });
        unmount();
        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const persistedRows = document.querySelectorAll('.apps-manage-row');
        expect(persistedRows[persistedRows.length - 1]?.textContent).toContain('库存盘点');
    });

    it('filters app studio management by search and category', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const manageSearch = document.querySelector('.apps-manage-filter .apps-search') as HTMLInputElement;
        expect(within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('文档处理 (2)')).not.toBeNull();
        fireEvent.change(manageSearch, { target: { value: 'pdf' } });
        expect(within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('全部应用 (4)')).not.toBeNull();
        expect(within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('文档处理 (2)')).not.toBeNull();
        expect(within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('财务审批 (1)')).not.toBeNull();
        expect(within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('法务工具 (1)')).not.toBeNull();
        expect((within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('supply (0)') as HTMLOptionElement).disabled).toBe(true);
        expect(screen.getByText('搜索“pdf” · 4 个匹配')).not.toBeNull();

        expect(screen.getAllByText('PDF 转 Word').length).toBeGreaterThan(0);
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((row) => row.textContent?.includes('报销申请'))).toBe(false);
        const disabledSortButtons = within(document.querySelector('.apps-manage-row') as HTMLElement).getAllByTitle('清空筛选后调整顺序') as HTMLButtonElement[];
        expect(disabledSortButtons.length).toBe(4);
        expect(disabledSortButtons.every((button) => button.disabled)).toBe(true);

        fireEvent.click(screen.getByTitle('清空搜索'));
        expect(manageSearch.value).toBe('');
        expect(within(document.querySelector('.apps-manage-row') as HTMLElement).queryByTitle('清空筛选后调整顺序')).toBeNull();

        fireEvent.change(document.querySelector('.apps-manage-category-select') as HTMLSelectElement, { target: { value: '文档处理' } });
        expect(screen.getByText('文档处理 · 2 个应用')).not.toBeNull();
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).map((row) => row.textContent || '').join('\n')).toContain('文档脱敏');
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((row) => row.textContent?.includes('采购入库'))).toBe(false);

        fireEvent.change(manageSearch, { target: { value: '采购' } });
        expect((document.querySelector('.apps-manage-category-select') as HTMLSelectElement).value).toBe('all');
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((row) => row.textContent?.includes('采购入库'))).toBe(true);

        fireEvent.change(manageSearch, { target: { value: 'no-such-app' } });
        expect(screen.getByText('没有匹配的应用')).not.toBeNull();
        fireEvent.click(within(document.querySelector('.apps-manage-filter') as HTMLElement).getByTitle('重置筛选'));
        expect(manageSearch.value).toBe('');
        expect((document.querySelector('.apps-manage-category-select') as HTMLSelectElement).value).toBe('all');
        expect((within(document.querySelector('.apps-manage-filter') as HTMLElement).getByTitle('重置筛选') as HTMLButtonElement).disabled).toBe(true);
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((row) => row.textContent?.includes('报销申请'))).toBe(true);
        fireEvent.change(manageSearch, { target: { value: 'xlsx' } });
        fireEvent.keyDown(manageSearch, { key: 'Escape' });
        expect(manageSearch.value).toBe('');
    });

    it('shows app manifest definitions from app studio management', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const manageRows = document.querySelectorAll('.apps-manage-row');
        fireEvent.click(within(manageRows[0] as HTMLElement).getByTitle(manageManifestTitle));

        expect(screen.getByText(/maclaw.app.v1/)).not.toBeNull();
        expect(screen.getByText(/agent_dynamic_ui/)).not.toBeNull();
        expect(screen.getByText(/finance.expense_upsert/)).not.toBeNull();
    });

    it('opens app editing in a dialog instead of expanding the management list', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);

        await waitFor(() => {
            const firstRow = document.querySelector('.apps-manage-row') as HTMLElement | null;
            const button = firstRow?.querySelector('.apps-manage-actions .apps-tonal-button') as HTMLButtonElement | null;
            expect(button).not.toBeNull();
        });
        const editButton = document.querySelector('.apps-manage-row .apps-manage-actions .apps-tonal-button') as HTMLButtonElement;
        editButton.focus();
        fireEvent.click(editButton);

        const dialog = screen.getByRole('dialog');
        const editForm = dialog.querySelector('.apps-manage-edit');
        const editActions = dialog.querySelector('.apps-manage-edit-dialog__actions');
        expect(editForm).not.toBeNull();
        expect(editActions).not.toBeNull();
        expect(editActions?.parentElement).toBe(dialog.querySelector('.apps-manage-edit-dialog__panel'));
        expect(editForm?.contains(editActions)).toBe(false);
        expect(editForm?.classList.contains('apps-manage-edit')).toBe(true);
        expect(document.querySelector('.apps-manage-item > .apps-manage-edit')).toBeNull();
        await waitFor(() => expect(document.activeElement).toBe(dialog.querySelector('.apps-form-row input')));

        const tabbableControls = dialog.querySelectorAll<HTMLElement>('button:not([disabled]):not([tabindex="-1"]), input:not([disabled]):not([tabindex="-1"]), select:not([disabled]):not([tabindex="-1"]), textarea:not([disabled]):not([tabindex="-1"])');
        const firstTabbableControl = tabbableControls[0];
        const lastTabbableControl = tabbableControls[tabbableControls.length - 1];
        expect(firstTabbableControl.classList.contains('apps-manage-edit-dialog__backdrop')).toBe(false);
        lastTabbableControl.focus();
        fireEvent.keyDown(dialog, { key: 'Tab' });
        expect(document.activeElement).toBe(firstTabbableControl);
        firstTabbableControl.focus();
        fireEvent.keyDown(dialog, { key: 'Tab', shiftKey: true });
        expect(document.activeElement).toBe(lastTabbableControl);

        fireEvent.keyDown(dialog, { key: 'Escape' });

        expect(screen.queryByRole('dialog')).toBeNull();
        await waitFor(() => expect(document.activeElement).toBe(editButton));
    });

    it('edits built-in app metadata from app studio management and persists it', async () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const manageRows = document.querySelectorAll('.apps-manage-row');
        fireEvent.click(within(manageRows[0] as HTMLElement).getByTitle('编辑'));
        const editPanel = document.querySelector('.apps-manage-edit') as HTMLElement;
        fireEvent.change(within(editPanel).getByTestId('edit-app-name'), { target: { value: '费用报销' } });
        fireEvent.change(within(editPanel).getByTestId('edit-app-category'), { target: { value: '财务' } });
        fireEvent.change(within(editPanel).getByTestId('edit-app-description'), { target: { value: 'updated built-in description' } });
        fireEvent.change(within(editPanel).getByTestId('edit-app-about-author'), { target: { value: 'MaClaw Team' } });
        fireEvent.change(within(editPanel).getByTestId('edit-app-about-copyright'), { target: { value: 'Copyright 2026 MaClaw' } });
        fireEvent.change(within(editPanel).getByTestId('edit-app-about-website'), { target: { value: 'https://maclaw.example.com' } });
        fireEvent.change(within(editPanel).getByTestId('edit-app-about-email'), { target: { value: 'support@maclaw.example.com' } });
        expect(screen.getByRole('button', { name: '表格/数据 (sheet)' })).not.toBeNull();
        fireEvent.click(screen.getByTitle('表格/数据 (sheet)'));
        fireEvent.click(screen.getByTitle('琥珀 #b45309'));
        fireEvent.click(within(screen.getByRole('dialog')).getByText('保存'));

        // Save jumps to 审核/发布; return to manage for manifest assertions.
        await waitFor(() => expect(screen.getByRole('tab', { name: '审核/发布' }).getAttribute('aria-selected')).toBe('true'));
        fireEvent.click(getManageTab());
        await waitFor(() => expect(screen.getAllByText('费用报销').length).toBeGreaterThan(0));

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('费用报销')) as HTMLElement;
        expect(editedRow.textContent).toContain('财务');
        fireEvent.click(within(editedRow).getByTitle(manageManifestTitle));
        const manifest = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(manifest).toContain('"name": "费用报销');
        expect(manifest).toContain('"category": "财务"');
        expect(manifest).toContain('"icon": "sheet"');
        expect(manifest).toContain('"accent": "#b45309"');
        expect(manifest).toContain('"aboutInfo"');
        expect(manifest).toContain('"author": "MaClaw Team"');
        expect(manifest).toContain('"copyright": "Copyright 2026 MaClaw"');
        expect(manifest).toContain('"website": "https://maclaw.example.com"');
        expect(manifest).toContain('"email": "support@maclaw.example.com"');

        unmount();
        render(<AppsPage lang="zh-Hans" />);

        expect(screen.getAllByText('费用报销').length).toBeGreaterThan(0);
        expect(screen.queryByText('报销申请')).toBeNull();
        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        const reloadedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('费用报销')) as HTMLElement;
        fireEvent.click(within(reloadedRow).getByTitle(manageManifestTitle));
        const reloadedManifest = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(reloadedManifest).toContain('"author": "MaClaw Team"');
        expect(reloadedManifest).toContain('"email": "support@maclaw.example.com"');
    });

    it('duplicates an app from app studio management and keeps the source skill binding', async () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('\u590d\u5236\u5e94\u7528'));

        const copiedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word 副本')) as HTMLElement;
        expect(copiedRow).not.toBeNull();
        expect(copiedRow.textContent).not.toContain('常用应用');
        expect(copiedRow.textContent).toContain('本地');
        expect(screen.getByDisplayValue('PDF 转 Word 副本')).not.toBeNull();

        fireEvent.click(within(pdfWordRow).getByTitle('\u590d\u5236\u5e94\u7528'));
        expect(screen.getByDisplayValue('PDF 转 Word 副本 2')).not.toBeNull();
        fireEvent.change(screen.getByDisplayValue('PDF 转 Word 副本 2'), { target: { value: 'PDF 转 Word 快速版' } });
        fireEvent.click(screen.getByText('保存'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
        fireEvent.click(getManageTab());

        const renamedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word 快速版')) as HTMLElement;
        fireEvent.click(within(renamedRow).getByTitle(manageManifestTitle));
        const manifest = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(manifest).toContain('"name": "PDF 转 Word 快速版"');
        expect(manifest).toContain('"id": "pdf-to-word"');
        expect(manifest).toContain('"source": "local"');
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((row) => row.textContent?.includes('PDF 转 Word 副本'))).toBe(true);

        unmount();
        render(<AppsPage lang="zh-Hans" />);
        expect(screen.getAllByText('PDF 转 Word 快速版').length).toBeGreaterThan(0);
    });

    it('deletes duplicated local apps and clears their run history', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('\u590d\u5236\u5e94\u7528'));

        const copiedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word 副本')) as HTMLElement;
        expect(within(copiedRow).getByTitle('移除')).not.toBeNull();
        fireEvent.click(within(copiedRow).getByTitle(manageManifestTitle));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        window.localStorage.setItem('maclaw:apps-run-history:v1', JSON.stringify({
            [manifest.app.id]: [{ appID: manifest.app.id, runID: 'run-copy-old', status: 'done', outputMode: 'pdf', inputSummary: 'old', message: 'old', at: new Date().toISOString() }],
        }));
        window.localStorage.setItem('maclaw:apps-publish-submissions:v1', JSON.stringify({
            [manifest.app.id]: { id: 'local-review-copy-old', appID: manifest.app.id, submittedAt: new Date().toISOString(), status: 'submitted' },
        }));
        fireEvent.click(within(copiedRow).getByTitle('移除'));

        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((row) => row.textContent?.includes('PDF 转 Word 副本'))).toBe(false);
        expect(Array.from(document.querySelectorAll('.apps-manage-row--hidden')).some((row) => row.textContent?.includes('PDF 转 Word 副本'))).toBe(false);
        expect(JSON.parse(window.localStorage.getItem('maclaw:apps-run-history:v1') || '{}')[manifest.app.id]).toBeUndefined();
        expect(JSON.parse(window.localStorage.getItem('maclaw:apps-publish-submissions:v1') || '{}')[manifest.app.id]).toBeUndefined();
    });

    it('edits tool app runtime modes from app studio management', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('编辑'));
        const editPane = document.querySelector('.apps-manage-edit') as HTMLElement;
        fireEvent.change(within(editPane).getByDisplayValue('文件上传'), { target: { value: 'mixed' } });
        fireEvent.click(within(editPane).getByText('Excel / XLSX'));
        fireEvent.click(within(screen.getByRole('dialog')).getByText('保存'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
        fireEvent.click(getManageTab());

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(editedRow).getByTitle(manageManifestTitle));
        const manifest = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(manifest).toContain('"inputMode": "mixed"');
        expect(manifest).toContain('"xlsx"');

        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        const outputSelect = container.querySelector('.apps-form-row select') as HTMLSelectElement;
        expect(Array.from(outputSelect.options).map((option) => option.value)).toContain('xlsx');
    });

    it('edits app studio layout visually and persists it', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('编辑'));
        const dialog = screen.getByRole('dialog');
        fireEvent.click(within(dialog).getByTestId('edit-layout-template-left_nav'));
        fireEvent.click(within(dialog).getByTestId('edit-layout-slot-center'));
        fireEvent.change(within(dialog).getByTestId('edit-output-region'), { target: { value: 'bottom' } });
        fireEvent.click(within(dialog).getByText('保存'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
        fireEvent.click(getManageTab());

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(editedRow).getByTitle(manageManifestTitle));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        const layout = manifest.app.binding.ui.layouts.tool_workspace;
        expect(layout.template).toBe('left_nav');
        expect(layout.primaryRegion).toBe('center');
        expect(layout.outputRegion).toBe('bottom');
        expect(layout.regions).toEqual(expect.arrayContaining([
            expect.objectContaining({ id: 'file_queue', role: 'input', placement: 'center' }),
            expect.objectContaining({ id: 'output_panel', role: 'output', placement: 'bottom' }),
        ]));
        expect(layout.studio.savedInManifest).toBe(true);

        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        await waitFor(() => expect(container.querySelector('.apps-runtime-layout')).not.toBeNull());
        const runtimeLayout = container.querySelector('.apps-runtime-layout') as HTMLElement;
        expect(runtimeLayout.dataset.template).toBe('left_nav');
        expect(runtimeLayout.dataset.primaryRegion).toBe('center');
        expect(runtimeLayout.dataset.outputRegion).toBe('bottom');
    });

    it('invalidates installed evidence snapshots when visual layout edits change the app definition', async () => {
        const staleApp = {
            id: 'editable-evidence-app',
            name: 'Editable Evidence App',
            description: 'Has stale install evidence',
            category: 'Finance',
            kind: 'enterprise_approval_app',
            icon: 'receipt',
            accent: '#2f5f98',
            version: 2,
            source: 'local',
            importedRunEvidence: { runID: 'run-old', status: 'done', definitionHash: 'old-definition', outputMode: 'approval', inputSummary: 'old', at: '2026-06-20T00:00:00Z' },
            versionSnapshot: { app_entry_version: '2', workflow_skills: [{ id: 'old-flow', version: '1.0.0', kind: 'workflow_skill', source: 'hub' }] },
            installEvidence: {
                schema: 'maclaw.app.install_record.v1',
                apps: [{ id: 'editable-evidence-app', name: 'Editable Evidence App', kind: 'enterprise_approval_app' }],
                dependency_verification: { schema: 'maclaw.app.install_plan.v1', dependencies: [{ id: 'old-flow', kind: 'workflow_skill', required: true, installed: true, health: 'ready' }] },
            },
            workflowContract: { schema: 'maclaw.app.workflow_contract.v1', workflowSkillId: 'old-flow', workflowVersion: '1.0.0', objectRole: 'old_record', requiredInputs: ['old'], decisionOutputs: ['approved'], statusMapping: { pending: 'old_pending', approved: 'old_approved', rejected: 'old_rejected', attention: 'old_attention' } },
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_approval_app',
                launchMode: 'agent_dynamic_ui',
                appSkill: { id: 'expense-app-skill', version: '1.0.0', source: 'hub' },
                dependencies: { skills: [{ id: 'expense-flow', version: '2.0.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }] },
                datasrv: { domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report' },
                mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'expense-flow', workflowVersion: '2.0.0', objectRole: 'expense_report' }] },
                ui: { schema: 'maclaw.app.ui.v1', generated: true, entry: 'approval_workspace', layouts: { approval_workspace: { template: 'classic_split', density: 'comfortable', primaryRegion: 'left', outputRegion: 'right', regions: [{ id: 'request_form', role: 'input', placement: 'left' }, { id: 'approval_inbox', role: 'instance_list', placement: 'center' }, { id: 'result_panel', role: 'output', placement: 'right' }], studio: { savedInManifest: true } } } },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [staleApp.id], customApps: [staleApp], recentUsedAtById: {} }));

        render(<AppsPage lang="en" />);
        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('Editable Evidence App')) as HTMLElement;
        fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
        const dialog = screen.getByRole('dialog');
        fireEvent.click(within(dialog).getByTestId('edit-layout-template-dashboard'));
        fireEvent.change(within(dialog).getByTestId('edit-output-region'), { target: { value: 'bottom' } });
        fireEvent.click(within(dialog).getByText('Save'));

        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            const edited = stored.customApps.find((app: any) => app.id === 'editable-evidence-app');
            expect(edited.version).toBe(3);
            expect(edited.importedRunEvidence).toBeUndefined();
            expect(edited.versionSnapshot).toBeUndefined();
            expect(edited.installEvidence).toBeUndefined();
            expect(edited.workflowContract).toBeUndefined();
            expect(edited.manifest.ui.layouts.approval_workspace.template).toBe('dashboard');
            expect(edited.manifest.ui.layouts.approval_workspace.outputRegion).toBe('bottom');
        });
    });

    it('edits approval workflow node mappings visually and persists them', async () => {
        const app = {
            id: 'editable-approval-nodes',
            name: 'Editable Approval Nodes',
            description: 'Approval workflow mapping editor',
            category: 'Finance',
            kind: 'enterprise_approval_app',
            icon: 'receipt',
            accent: '#2f5f98',
            source: 'local',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'enterprise_approval_app',
                launchMode: 'agent_dynamic_ui',
                datasrv: { domain: 'finance', objectRole: 'expense_report' },
                dependencies: { skills: [{ id: 'expense-flow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', install_ref: 'cap-hub-expense-flow', capabilities: ['approval.workflow'] }] },
                mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'expense-flow', workflowVersion: '1.0.0', objectRole: 'expense_report' }] },
                workflow: { schema: 'maclaw.app.workflow.v1', submitNode: 'expense.submit', approvalNode: 'expense.manager_review', resultNode: 'expense.result', attentionNode: 'expense.attention', statusMapping: { pending: 'approval_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' } },
                ui: { schema: 'maclaw.app.ui.v1' },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        render(<AppsPage lang="en" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('Editable Approval Nodes')) as HTMLElement;
        fireEvent.click((row.querySelector('.apps-manage-actions .apps-tonal-button') as HTMLButtonElement));
        const dialog = screen.getByRole('dialog');
        expect((within(dialog).getByTestId('edit-workflow-skill-install-ref') as HTMLInputElement).value).toBe('cap-hub-expense-flow');
        fireEvent.change(within(dialog).getByTestId('edit-workflow-skill-install-ref'), { target: { value: 'cap-hub-expense-flow-v2' } });
        fireEvent.change(within(dialog).getByTestId('edit-workflow-approvalNode'), { target: { value: 'finance.director_review' } });
        fireEvent.change(within(dialog).getByTestId('edit-workflow-status-rejected'), { target: { value: 'finance_rejected' } });
        fireEvent.click(within(dialog).getByText('Save'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
        fireEvent.click(getManageTab());

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('Editable Approval Nodes')) as HTMLElement;
        fireEvent.click(within(editedRow).getByTitle(manageManifestTitle));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(manifest.app.binding.workflow.approvalNode).toBe('finance.director_review');
        expect(manifest.app.binding.workflow.statusMapping.rejected).toBe('finance_rejected');
        expect(manifest.app.binding.dependencies.skills[0].install_ref).toBe('cap-hub-expense-flow-v2');
        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        expect(stored.customApps[0].manifest.workflow.approvalNode).toBe('finance.director_review');
        expect(stored.customApps[0].manifest.dependencies.skills[0].install_ref).toBe('cap-hub-expense-flow-v2');
    });

    it('edits a tool app skill binding from app studio management', async () => {
        listNLSkillsMock.mockResolvedValue([{ name: 'pdf-word-v2', description: 'Updated converter' }]);
        render(<AppsPage lang="en" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF')) as HTMLElement;
        fireEvent.click((pdfWordRow.querySelector('.apps-manage-actions .apps-tonal-button') as HTMLButtonElement));

        const dialog = screen.getByRole('dialog');
        await waitFor(() => expect(within(dialog).getByRole('option', { name: /pdf-word-v2/ })).not.toBeNull());
        fireEvent.click(within(dialog).getByRole('option', { name: /pdf-word-v2/ }) as HTMLButtonElement);
        fireEvent.click(within(dialog).getByText('Save'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
        fireEvent.click(getManageTab());

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF')) as HTMLElement;
        fireEvent.click(within(editedRow).getByTitle(manageManifestTitle));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(manifest.app.binding.skill.id).toBe('pdf-word-v2');
        expect(manifest.app.binding.appSkill.id).toBe('pdf-word-v2');
    });

    it('shows an error when a custom app icon upload is unsupported', async () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);

        const firstRow = document.querySelector('.apps-manage-row') as HTMLElement;
        fireEvent.click((firstRow.querySelector('.apps-manage-actions .apps-tonal-button') as HTMLButtonElement));

        const dialog = screen.getByRole('dialog');
        const fileInput = dialog.querySelector('.apps-custom-icon-upload input[type="file"]') as HTMLInputElement;
        fireEvent.change(fileInput, { target: { files: [new File(['not an icon'], 'icon.txt', { type: 'text/plain' })] } });

        await waitFor(() => expect(within(dialog).getByRole('alert').textContent).toContain('PNG, JPEG, or WebP'));
    });

    it('writes custom app icons back into single-file skill app definitions', async () => {
        const customIconDataUrl = 'data:image/png;base64,iVBORw0KGgo=';
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            customApps: [{
                id: 'skill-app-invoice-app-invoice-review',
                name: 'Portable Icon App',
                description: 'A skill app with a portable icon',
                category: 'Finance',
                kind: 'tool_app',
                icon: 'receipt',
                customIconDataUrl,
                accent: '#2f5f98',
                source: 'skill',
                version: 1,
                manifest: {
                    schema: 'maclaw.app.v1',
                    installUnit: 'skill',
                    privateMarker: 'x_maclaw_apps',
                    entryKind: 'tool_app',
                    launchMode: 'fixed_skill_ui',
                    appSkill: { id: 'invoice-app', version: '1.0.0' },
                    skill: {
                        id: 'invoice-app',
                        appDefinitionFile: 'maclaw.app.json',
                        inputMode: 'file',
                        multipleFiles: false,
                        outputModes: ['pdf'],
                        fields: [],
                    },
                },
            }],
        }));
        // Skill apps enter the panel through discovery, not panel snapshots.
        listSkillAppManifestsMock.mockResolvedValue([{ id: 'invoice-review', skill_id: 'invoice-app', name: 'Portable Icon App', description: 'A skill app with a portable icon', category: 'Finance', icon: 'receipt', custom_icon_data_url: customIconDataUrl, input_mode: 'file', output_modes: ['pdf'], app_definition_file: 'maclaw.app.json' }]);
        render(<AppsPage lang="en" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);

        let row: HTMLElement | undefined;
        await waitFor(() => {
            const item = Array.from(document.querySelectorAll('.apps-manage-item')).find((entry) => entry.textContent?.includes('Portable Icon App')) as HTMLElement | undefined;
            row = item?.querySelector('.apps-manage-row') as HTMLElement | undefined;
            expect(row).toBeTruthy();
        });
        expect(row).toBeTruthy();
        fireEvent.click(row!.querySelector('.apps-manage-actions .apps-tonal-button') as HTMLButtonElement);
        fireEvent.click(within(screen.getByRole('dialog')).getByText('Save'));

        await waitFor(() => expect(saveMaclawAppDefinitionForSkillMock).toHaveBeenCalledTimes(1));
        const [skillID, manifestText] = saveMaclawAppDefinitionForSkillMock.mock.calls[0];
        expect(skillID).toBe('invoice-app');
        const manifest = JSON.parse(String(manifestText));
        expect(manifest.app.id).toBe('invoice-review');
        expect(manifest.app.customIconDataUrl).toBe(customIconDataUrl);
        expect(manifest.app.panel.customIconDataUrl).toBe(customIconDataUrl);
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    });

    it('writes enterprise approval skill apps back with dynamic layout and workflow bindings', async () => {
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            customApps: [{
                id: 'skill-app-reimbursement-app-reimbursement-approval',
                name: 'Reimbursement Approval',
                description: 'Submit and approve reimbursement requests',
                category: 'Finance',
                kind: 'enterprise_approval_app',
                icon: 'receipt',
                accent: '#2f5f98',
                source: 'skill',
                version: 2,
                manifest: {
                    schema: 'maclaw.app.v1',
                    installUnit: 'skill',
                    privateMarker: 'x_maclaw_apps',
                    entryKind: 'enterprise_approval_app',
                    launchMode: 'agent_dynamic_ui',
                    datasrv: { domain: 'reimbursement', objectRole: 'expense_claim' },
                    mis: {
                        approvalBindings: [{
                            event: 'reimbursement.submitted',
                            workflowSkillId: 'expense-approval-flow',
                            workflowVersion: '1.2.0',
                            objectRole: 'expense_claim',
                        }],
                    },
                    skill: { id: 'reimbursement-app', appDefinitionFile: 'maclaw.app.json' },
                    appSkill: { id: 'reimbursement-app', version: '1.0.0', source: 'local' },
                    dependencies: {
                        skills: [{
                            id: 'expense-approval-flow',
                            version: '1.2.0',
                            kind: 'workflow_skill',
                            required: true,
                            source: 'hub',
                            capabilities: ['approval.workflow'],
                        }],
                    },
                    ui: {
                        schema: 'maclaw.app.ui.v1',
                        layouts: {
                            approval_workspace: {
                                template: 'split_workbench',
                                primaryRegion: 'left',
                                outputRegion: 'bottom',
                                regions: [
                                    { id: 'request_form', role: 'input', placement: 'left' },
                                    { id: 'approval_inbox', role: 'instance_list', placement: 'center' },
                                    { id: 'approval_detail', role: 'detail', placement: 'center' },
                                    { id: 'result_panel', role: 'output', placement: 'bottom' },
                                ],
                            },
                        },
                    },
                    resultContract: {
                        schema: 'maclaw.app.result.v1',
                        primary: 'approval_result',
                        types: ['approval_result', 'business_status', 'content'],
                        approvalDecisions: ['approved', 'rejected', 'needs_attention'],
                        delivery: { inlineContent: true, artifacts: true, businessRecord: true, notifications: true },
                    },
                    testProtocol: {
                        schema: 'maclaw.app.test_protocol.v1',
                        fingerprint: 'approval-test-fingerprint',
                        sampleInput: { amount: 1280, applicant: 'current_user' },
                        expectedOutput: { approval_result: 'approved', primary: 'approval_result' },
                        requiredRoles: ['applicant', 'approver'],
                        requiredScopes: ['datasrv:write'],
                        riskLevel: 'medium',
                    },
                },
            }],
        }));
        // Skill apps enter the panel through discovery, not panel snapshots.
        listSkillAppManifestsMock.mockResolvedValue([{
            id: 'reimbursement-approval',
            skill_id: 'reimbursement-app',
            name: 'Reimbursement Approval',
            description: 'Submit and approve reimbursement requests',
            category: 'Finance',
            icon: 'receipt',
            version: 2,
            app_definition: {
                schema: 'maclaw.app.v1',
                privateMarker: 'x_maclaw_apps',
                installUnit: 'skill',
                app: {
                    id: 'reimbursement-approval',
                    name: 'Reimbursement Approval',
                    description: 'Submit and approve reimbursement requests',
                    category: 'Finance',
                    kind: 'enterprise_approval_app',
                    icon: 'receipt',
                    launchMode: 'agent_dynamic_ui',
                    datasrv: { domain: 'reimbursement', objectRole: 'expense_claim' },
                    mis: { approvalBindings: [{ event: 'reimbursement.submitted', workflowSkillId: 'expense-approval-flow', workflowVersion: '1.2.0', objectRole: 'expense_claim' }] },
                    appSkill: { id: 'reimbursement-app', version: '1.0.0', source: 'local' },
                    skill: { id: 'reimbursement-app', appDefinitionFile: 'maclaw.app.json' },
                    dependencies: { skills: [{ id: 'expense-approval-flow', version: '1.2.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }] },
                    ui: {
                        schema: 'maclaw.app.ui.v1',
                        layouts: {
                            approval_workspace: {
                                template: 'split_workbench',
                                primaryRegion: 'left',
                                outputRegion: 'bottom',
                                regions: [
                                    { id: 'request_form', role: 'input', placement: 'left' },
                                    { id: 'approval_inbox', role: 'instance_list', placement: 'center' },
                                    { id: 'approval_detail', role: 'detail', placement: 'center' },
                                    { id: 'result_panel', role: 'output', placement: 'bottom' },
                                ],
                            },
                        },
                    },
                    resultContract: {
                        schema: 'maclaw.app.result.v1',
                        primary: 'approval_result',
                        types: ['approval_result', 'business_status', 'content'],
                        approvalDecisions: ['approved', 'rejected', 'needs_attention'],
                        delivery: { inlineContent: true, artifacts: true, businessRecord: true, notifications: true },
                    },
                    testProtocol: {
                        schema: 'maclaw.app.test_protocol.v1',
                        fingerprint: 'approval-test-fingerprint',
                        sampleInput: { amount: 1280, applicant: 'current_user' },
                        expectedOutput: { approval_result: 'approved', primary: 'approval_result' },
                        requiredRoles: ['applicant', 'approver'],
                        requiredScopes: ['datasrv:write'],
                        riskLevel: 'medium',
                    },
                },
            },
        }]);
        render(<AppsPage lang="en" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);

        let row: HTMLElement | undefined;
        await waitFor(() => {
            const item = Array.from(document.querySelectorAll('.apps-manage-item')).find((entry) => entry.textContent?.includes('Reimbursement Approval')) as HTMLElement | undefined;
            expect(item).toBeTruthy();
            row = item?.querySelector('.apps-manage-row') as HTMLElement | undefined;
        });
        expect(row).toBeTruthy();
        fireEvent.click(row!.querySelector('.apps-manage-actions .apps-tonal-button') as HTMLButtonElement);
        fireEvent.click(within(screen.getByRole('dialog')).getByText('Save'));

        await waitFor(() => expect(saveMaclawAppDefinitionForSkillMock).toHaveBeenCalledTimes(1));
        const [skillID, manifestText] = saveMaclawAppDefinitionForSkillMock.mock.calls[0];
        expect(skillID).toBe('reimbursement-app');
        const manifest = JSON.parse(String(manifestText));
        expect(manifest.schema).toBe('maclaw.app.v1');
        expect(manifest.privateMarker).toBe('x_maclaw_apps');
        expect(manifest.app.id).toBe('reimbursement-approval');
        expect(manifest.app.kind).toBe('enterprise_approval_app');
        expect(manifest.app.launchMode).toBe('agent_dynamic_ui');
        expect(manifest.app.binding.skill.appDefinitionFile).toBe('maclaw.app.json');
        expect(manifest.app.binding.datasrv.objectRole).toBe('expense_claim');
        expect(manifest.app.binding.appSkill.id).toBe('reimbursement-app');
        expect(manifest.app.binding.dependencies.skills[0].id).toBe('expense-approval-flow');
        expect(manifest.app.binding.mis.approvalBindings[0].workflowSkillId).toBe('expense-approval-flow');
        expect(manifest.app.binding.ui.layouts.approval_workspace.regions).toEqual([
            { id: 'request_form', role: 'input', placement: 'left', order: 1 },
            { id: 'approval_inbox', role: 'instance_list', placement: 'center', order: 2 },
            { id: 'approval_detail', role: 'detail', placement: 'center', order: 3 },
            { id: 'result_panel', role: 'output', placement: 'bottom', order: 4 },
        ]);
        expect(manifest.app.binding.resultContract.approvalDecisions).toEqual(['approved', 'rejected', 'needs_attention']);
        expect(manifest.app.binding.testProtocol.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        expect(manifest.app.binding.testProtocol.expectedOutput.approval_result).toBe('approved');
        expect(manifest.app.binding.testProtocol.requiredRoles).toEqual(['applicant', 'approver']);
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    });

    it('edits tool app structured fields from app studio management', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('编辑'));
        const editPane = document.querySelector('.apps-manage-edit') as HTMLElement;
        fireEvent.change(within(editPane).getByDisplayValue('文件上传'), { target: { value: 'mixed' } });
        fireEvent.click(within(editPane).getByText('添加字段'));
        const fieldEditor = within(editPane).getByPlaceholderText('customer_id').closest('.apps-field-editor') as HTMLElement;
        fireEvent.change(within(fieldEditor).getByPlaceholderText('customer_id'), { target: { value: 'review_level' } });
        fireEvent.change(within(fieldEditor).getByPlaceholderText('显示名'), { target: { value: '审核等级' } });
        fireEvent.change(within(fieldEditor).getByDisplayValue('text'), { target: { value: 'select' } });
        fireEvent.change(within(fieldEditor).getByPlaceholderText('A, B, C'), { target: { value: '普通, 严格' } });
        fireEvent.click(within(fieldEditor).getByText('必填'));
        fireEvent.click(within(screen.getByRole('dialog')).getByText('保存'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
        fireEvent.click(getManageTab());

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(editedRow).getByTitle(manageManifestTitle));
        const manifest = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(manifest).toContain('"fields"');
        expect(manifest).toContain('"review_level"');
        expect(manifest).toContain('"options"');
    });

    it('navigates to review/publish after saving an app edit', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('编辑'));
        const editPane = document.querySelector('.apps-manage-edit') as HTMLElement;
        fireEvent.change(within(editPane).getByTestId('edit-app-name'), { target: { value: 'PDF 转 Word 发布版' } });
        fireEvent.click(within(screen.getByRole('dialog')).getByText('保存'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());

        await waitFor(() => expect(screen.getByRole('tab', { name: '审核/发布' }).getAttribute('aria-selected')).toBe('true'));
        expect(screen.getAllByText('PDF 转 Word 发布版').length).toBeGreaterThan(0);
        expect(document.querySelector('.apps-publish')).not.toBeNull();
        const focusedCard = document.querySelector('.apps-publish-card.is-focus-target') as HTMLElement | null;
        expect(focusedCard).not.toBeNull();
        expect(focusedCard?.textContent).toContain('PDF 转 Word 发布版');
        // Manage rows keep name/desc as sibling grid columns for horizontal alignment.
        fireEvent.click(getManageTab());
        const alignedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word 发布版')) as HTMLElement;
        expect(alignedRow.querySelector(':scope > .apps-manage-row__name')?.textContent).toContain('PDF 转 Word 发布版');
        expect(alignedRow.querySelector(':scope > .apps-manage-row__desc')?.textContent).toMatch(/Skill|本地|已加入面板/);
    });

    it('copies the installed apps as an app pack manifest', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        fireEvent.click(screen.getByText('复制应用包'));

        await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalled());
        const copied = String((navigator.clipboard.writeText as any).mock.calls.at(-1)?.[0] || '');
        expect(copied).toContain('maclaw.app.pack.v1');
        expect(copied).toContain('报销申请');
        expect(copied).toContain('"governance"');
        expect(screen.getByText('已复制')).not.toBeNull();
    });

    it('keeps explicit enterprise skill dependency metadata when exporting an app pack', async () => {
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            customApps: [{
                id: 'expense-approval-export',
                name: 'Expense Approval Export',
                description: 'Approval app with explicit dependency sources',
                category: 'Finance',
                kind: 'enterprise_approval_app',
                icon: 'receipt',
                accent: '#2f5f98',
                source: 'local',
                version: 1,
                manifest: {
                    schema: 'maclaw.app.v1',
                    installUnit: 'enterprise_app_pack',
                    privateMarker: 'x_maclaw_apps',
                    entryKind: 'enterprise_approval_app',
                    launchMode: 'agent_dynamic_ui',
                    datasrv: { domain: 'expense', objectRole: 'expense_report' },
                    skill: { id: 'expense-super-skill', appDefinitionFile: 'maclaw.app.json' },
                    appSkill: { id: 'expense-super-skill', version: '1.3.0', source: 'local' },
                    dependencies: {
                        skills: [{
                            id: 'expense-approval-flow',
                            version: '2.1.0',
                            kind: 'workflow_skill',
                            required: true,
                            source: 'market',
                            capabilities: ['approval.workflow'],
                        }],
                    },
                    mis: { approvalBindings: [{ event: 'expense.submitted', objectRole: 'expense_report', workflowSkillId: 'expense-approval-flow', workflowVersion: '2.1.0' }] },
                },
            }],
        }));
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        fireEvent.click(screen.getByText('复制应用包'));

        await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalled());
        const copied = JSON.parse(String((navigator.clipboard.writeText as any).mock.calls.at(-1)?.[0] || '{}'));
        const exported = copied.apps.find((item: any) => item.app?.id === 'expense-approval-export');
        expect(exported).toBeTruthy();
        const dependencies = exported.app.governance.dependencies.skills;
        expect(dependencies.find((dep: any) => dep.id === 'expense-super-skill')).toMatchObject({ version: '1.3.0', kind: 'app_skill', source: 'local' });
        expect(dependencies.find((dep: any) => dep.id === 'expense-approval-flow')).toMatchObject({ version: '2.1.0', kind: 'workflow_skill', source: 'market' });
    });

    it('shows market apps as a list and adds one to the panel', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());

        expect(screen.getByRole('button', { name: /\u5e94\u7528\u5e02\u573a/, pressed: true })).not.toBeNull();
        expect(screen.getByText('合同归档')).not.toBeNull();
        expect(screen.getByText('可添加 4 · 可升级 0')).not.toBeNull();
        const importPackage = screen.getByText('导入应用包').closest('details') as HTMLDetailsElement;
        expect(importPackage.open).toBe(false);

        const row = screen.getByText('合同归档').closest('.apps-market-row') as HTMLElement;
        fireEvent.click(within(row).getByRole('button', { name: '添加: 合同归档' }));

        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalledTimes(1));
        expect(recordMaclawAppInstallMock).toHaveBeenCalledTimes(1);
        await waitFor(() => expect((within(row).getByRole('button', { name: '已安装: 合同归档' }) as HTMLButtonElement).disabled).toBe(true));
        expect(screen.getByText('可添加 3 · 可升级 0')).not.toBeNull();
        fireEvent.click(getManageTab());
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('合同归档'))).toBe(true);
    });

    it('installs approved Hub MaClaw Apps from market search results', async () => {
        const hubPackage = {
            schema: 'maclaw.app.pack.v1',
            privateMarker: 'x_maclaw_apps',
            source: 'enterprise_hub',
            apps: [{
                schema: 'maclaw.app.v1',
                privateMarker: 'x_maclaw_apps',
                installUnit: 'enterprise_app_pack',
                app: {
                    id: 'contract-approval',
                    name: 'Contract Approval',
                    description: 'Submit and approve contracts',
                    category: 'Legal',
                    kind: 'enterprise_approval_app',
                    icon: 'shield',
                    launchMode: 'agent_dynamic_ui',
                    ui: {
                        schema: 'maclaw.app.ui.v1',
                        entry: 'approval_workspace',
                        generated: true,
                        layouts: {
                            approval_workspace: {
                                template: 'classic_split',
                                density: 'compact',
                                primaryRegion: 'left',
                                outputRegion: 'right',
                                regions: [
                                    { id: 'contract_form', role: 'input', placement: 'left', visible: true },
                                    { id: 'contract_instances', role: 'instance_list', placement: 'center', visible: true },
                                    { id: 'contract_result', role: 'output', placement: 'right', visible: true },
                                ],
                            },
                        },
                    },
                    binding: {
                        datasrv: { domain: 'legal', datasetID: 'legal.contracts', objectRole: 'contract', preferredAction: 'legal.contract_submit' },
                        dependencies: { skills: [{ id: 'contract-workflow', kind: 'workflow_skill', source: 'hub', required: true }] },
                        mis: { approvalBindings: [{ event: 'contract.submitted', objectRole: 'contract', workflowSkillId: 'contract-workflow' }] },
                    },
                },
            }],
        };
        searchMixedSkillsMock.mockResolvedValueOnce([{
            id: 'cap-hub-contract-approval',
            install_ref: 'cap-hub-contract-approval',
            name: 'Contract Approval',
            description: 'Submit and approve contracts',
            source: 'enterprise_hub',
            source_label: 'Enterprise Hub',
            product_kind: 'maclaw_app_skill',
            is_maclaw_app: true,
            maclaw_app_id: 'contract-approval-reviewed',
            maclaw_app_name: 'Reviewed Contract Approval',
            maclaw_app_description: 'Submit and approve contracts',
            maclaw_app_kind: 'enterprise_approval_app',
            maclaw_app_category: 'Legal',
            maclaw_app_icon: 'shield',
            review_evidence: {
                'contract-approval-reviewed': {
                    run_id: 'run-market-contract-review',
                    test_protocol_fingerprint: 'proto-market-contract-review',
                    result_coverage_primary: 'approval_result',
                    result_coverage_ok: true,
                    result_coverage_covered_count: 3,
                    result_coverage_missing_count: 0,
                    output_count: 1,
                    artifact_count: 1,
                    approval_status: 'approved',
                    current_node: 'contract.result_feedback',
                },
	            },
	        }]);
	        const contractWorkflowDependency = {
	            id: 'contract-workflow',
	            kind: 'workflow_skill',
	            source: 'hub',
	            required: true,
	            installed: true,
	            health: 'ready',
	            action: 'installed',
	            app_ids: ['market-contract-approval'],
	            install_ref: 'hub://skills/contract-workflow@1.0.0',
	            install_ref_kind: 'hub',
	            install_ref_target: 'contract-workflow',
	            install_ref_version: '1.0.0',
	            install_ref_status: 'ok',
	            preflight_status: 'ready',
	            preflight_code: 'skillhub_target_ready',
	            preflight_stage: 'skillhub_preflight',
	            package_sha256: 'sha-contract-workflow',
	            package_signature: 'sig-contract-workflow',
	            package_download_url: 'https://hub.example/skills/contract-workflow/download',
	            integrity_status: 'ready',
	            integrity_code: 'package_integrity_metadata_ready',
	            integrity_stage: 'skillhub_preflight',
	        };
	        installSelectedMaclawAppPackageFromHubMock.mockResolvedValueOnce({
	            schema: 'maclaw.app.hub_install.v1',
            capability_id: 'cap-hub-contract-approval',
            package: hubPackage,
            package_json: JSON.stringify(hubPackage),
            source_app_count: 2,
            source_app_ids: ['contract-approval', 'contract-archive'],
            app_count: 1,
            app_ids: ['contract-approval'],
            install_plan: {
	                schema: 'maclaw.app.install_plan.v1',
	                apps: [{ id: 'contract-approval', name: 'Contract Approval', kind: 'enterprise_approval_app' }],
	                dependencies: [contractWorkflowDependency],
	                has_missing_required: false,
	                has_blocking_dependency: false,
            },
            install_record: {
                schema: 'maclaw.app.install_record.v1',
                package_sha: 'pkg-contract-approval',
                package_sha256: 'pkg-contract-approval',
                hub_package_signature: {
                    schema: 'maclaw.app.package_signature.v1',
                    algorithm: 'ed25519',
                    public_key_fingerprint: 'sha256:contract-package-key',
                    signed_at: '2026-07-01T02:00:00Z',
                    signed_by: 'enterprise-market',
                    package_sha256: 'pkg-contract-approval',
                },
                hub_package_signature_algorithm: 'ed25519',
                hub_package_signature_fingerprint: 'sha256:contract-package-key',
                hub_package_signature_signed_at: '2026-07-01T02:00:00Z',
	                hub_package_signature_signed_by: 'enterprise-market',
	                app_count: 1,
	                apps: [{ id: 'market-contract-approval', name: 'Contract Approval', kind: 'enterprise_approval_app' }],
	                dependencies: [contractWorkflowDependency],
                app_versions: { 'market-contract-approval': { app_entry_version: '1', workflow_skills: [{ id: 'contract-workflow', kind: 'workflow_skill', source: 'hub' }] } },
                install_evidence: {
                    'market-contract-approval': {
                        workspace_layout: {
                            schema: 'maclaw.app.ui.v1',
                            entry: 'approval_workspace',
                            template: 'classic_split',
                            density: 'compact',
                            primary_region: 'left',
                            output_region: 'right',
                            region_count: 3,
                            regions: [
                                { id: 'contract_form', role: 'input', placement: 'left', visible: true },
                                { id: 'contract_instances', role: 'instance_list', placement: 'center', visible: true },
                                { id: 'contract_result', role: 'output', placement: 'right', visible: true },
                            ],
                        },
                        result_contract: { primary: 'approval_result', types: ['approval_result', 'artifact', 'content'] },
                        submission: {
                            status: 'published',
                            capability_id: 'cap-hub-contract-approval',
                            market_capability_id: 'contract-approval',
                            submission_id: 'enterprise_hub:skill:maclaw-app:contract-approval@pkg',
                            version_key: 'enterprise_hub:skill:maclaw-app:contract-approval@pkg',
                            package_sha256: 'pkg-contract-approval',
                            package_signature: {
                                schema: 'maclaw.app.package_signature.v1',
                                algorithm: 'ed25519',
                                public_key_fingerprint: 'sha256:contract-package-key',
                                signed_at: '2026-07-01T02:00:00Z',
                                signed_by: 'enterprise-market',
                                package_sha256: 'pkg-contract-approval',
                            },
                        },
                        review_evidence: {
                            status: 'approved',
                            approval_status: 'approved',
                            current_node: 'contract.result_feedback',
                            run_id: 'run-market-contract-review',
                            result_coverage_primary: 'approval_result',
                            result_coverage_covered_count: 3,
                            result_coverage_missing_count: 0,
                        },
                        version_snapshot: { app_entry_version: '1', workflow_skills: [{ id: 'contract-workflow', kind: 'workflow_skill', source: 'hub' }] },
	                        dependencies: [contractWorkflowDependency],
                        dependency_verification: {
                            schema: 'maclaw.app.install_plan.v1',
                            verified_at: '2026-06-26T08:00:00.000Z',
                            app_count: 1,
                            dependency_count: 1,
                            has_missing_required: false,
                            has_blocking_dependency: false,
	                            dependencies: [contractWorkflowDependency],
                        },
                        test_evidence: {
                            run_id: 'run-hub-contract-approval',
                            verified_at: '2026-06-26T08:01:00.000Z',
                            test_protocol_fingerprint: 'proto-hub-contract-approval',
                            primary_result: 'approved',
                            result_coverage: { ok: true, primary: 'approval_result', covered_types: ['approval_result', 'artifact', 'content'], missing_types: [] },
                            outputs: [{ kind: 'approval_result', title: 'Approval decision', text: 'approved', status: 'approved' }],
                            artifacts: [{ id: 'artifact-contract-approval', uri: 'artifact://contract/approval.pdf', name: 'contract-approval.pdf', status: 'ready' }],
                            approval_instance: {
                                instanceId: 'approval-hub-contract-1',
                                approvalID: 'approval-remote-hub-contract-1',
                                recordID: 'contract-1',
                                datasetID: 'legal.contracts',
                                objectRole: 'contract',
                                approvalEvent: 'contract.submitted',
                                approvalWorkflowID: 'contract-workflow',
                                status: 'approved',
                                currentNode: 'contract.result_feedback',
                                workflowSkillId: 'contract-workflow',
                                workflowVersion: '1.0.0',
                                businessStatus: 'executed',
                                resultStatus: 'approved',
                                resultPayload: { approval_result: 'approved', business_status: 'executed', business_record: { id: 'contract-1' } },
                                outputs: [{ kind: 'approval_result', title: 'Approval decision', text: 'approved', status: 'approved' }],
                                artifacts: [{ id: 'artifact-contract-approval', uri: 'artifact://contract/approval.pdf', name: 'contract-approval.pdf', status: 'ready' }],
                                approvalInstanceViewVerified: true,
                                approvalViews: { my_requests: true, pending_my_approval: true, handled: true, all: true },
                            },
                        },
                        datasrv_registration: { synced: true, eligible_count: 1, synced_count: 1, failed_count: 0 },
                    },
                },
            },
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText('Search enterprise Hub apps'), { target: { value: 'contract' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search Hub' }));

        const row = (await screen.findByText('Reviewed Contract Approval')).closest('.apps-market-row') as HTMLElement;
        expect(row).toBeTruthy();
        expect(within(row).getByText(/Enterprise Hub/)).not.toBeNull();
        const marketReviewEvidence = within(row).getByLabelText('Review evidence');
        expect(within(marketReviewEvidence).getByText('Test evidence')).not.toBeNull();
        expect(within(marketReviewEvidence).getByText('run-market-contract-review · proto-market-contract-review')).not.toBeNull();
        expect(within(marketReviewEvidence).getByText('Result coverage')).not.toBeNull();
        expect(within(marketReviewEvidence).getByText('approval_result · Covered: 3')).not.toBeNull();
        expect(within(marketReviewEvidence).getByText('Result package')).not.toBeNull();
        expect(within(marketReviewEvidence).getByText('Output: 1 · Output artifacts: 1')).not.toBeNull();
        expect(within(marketReviewEvidence).getByText('Approval')).not.toBeNull();
        expect(within(marketReviewEvidence).getByText('approved · contract.result_feedback')).not.toBeNull();
        fireEvent.click(within(row).getByRole('button', { name: 'Add: Reviewed Contract Approval' }));

        await waitFor(() => expect(installSelectedMaclawAppPackageFromHubMock).toHaveBeenCalledWith('cap-hub-contract-approval', ['market-contract-approval-reviewed']));
        expect(installMaclawAppPackageFromHubMock).not.toHaveBeenCalled();
        expect(installMaclawAppDependenciesMock).not.toHaveBeenCalled();
        await waitFor(() => expect(within(row).getByText('Already installed · Source package 2 · installed 1 · 1 dependencies')).not.toBeNull());
        const installEvidenceSnapshot = row.querySelector('.apps-install-evidence-snapshot:not(.apps-review-evidence-strip)') as HTMLElement;
        expect(installEvidenceSnapshot).not.toBeNull();
        expect(within(installEvidenceSnapshot).getByText('Package signature')).not.toBeNull();
        expect(within(installEvidenceSnapshot).getByText(/ed25519.*sha256:contract-package-key.*enterprise-market/)).not.toBeNull();
        expect(within(installEvidenceSnapshot).getByText('Result contract')).not.toBeNull();
        expect(within(installEvidenceSnapshot).getByText('approval_result · 3 types')).not.toBeNull();
        expect(within(installEvidenceSnapshot).getByText('run-hub-contract-approval · proto-hub-contract-approval')).not.toBeNull();
	        expect(within(installEvidenceSnapshot).getByText('Result coverage')).not.toBeNull();
	        expect(within(installEvidenceSnapshot).getByText('approval_result · Covered: 3')).not.toBeNull();
	        expect(within(installEvidenceSnapshot).getByText('Output: 1 · Output artifacts: 1')).not.toBeNull();
	        expect(within(installEvidenceSnapshot).getByText('Dependency diagnostics')).not.toBeNull();
	        expect(within(installEvidenceSnapshot).getByText(/contract-workflow.*target:contract-workflow@1\.0\.0.*integrity:ready.*sha:sha-contract-workflow.*signature:available/)).not.toBeNull();
	        const dependencyVerification = within(row).getByLabelText('Dependency verification');
	        fireEvent.click(within(dependencyVerification).getByText('Show details'));
	        const installTrace = within(dependencyVerification).getByLabelText('Install trace');
	        expect(within(installTrace).getByText('Preflight')).not.toBeNull();
	        expect(within(installTrace).getByText(/ready.*code:skillhub_target_ready.*stage:skillhub_preflight/)).not.toBeNull();
	        expect(within(installTrace).getByText('Download')).not.toBeNull();
	        expect(within(installTrace).getByText(/available.*sha:sha-contract-workflow.*signature:available/)).not.toBeNull();
	        expect(within(installTrace).getByText('Integrity')).not.toBeNull();
	        fireEvent.click(getManageTab());
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('Contract Approval'))).toBe(true);
        const storedApp = latestStoredCustomApp('Contract Approval');
        expect(storedApp.importedRunEvidence.approvalInstance).toMatchObject({
            instanceId: 'approval-hub-contract-1',
            approvalID: 'approval-remote-hub-contract-1',
            recordID: 'contract-1',
            datasetID: 'legal.contracts',
            objectRole: 'contract',
            approvalEvent: 'contract.submitted',
            approvalWorkflowID: 'contract-workflow',
            status: 'approved',
            currentNode: 'contract.result_feedback',
            workflowSkillId: 'contract-workflow',
            workflowVersion: '1.0.0',
            businessStatus: 'executed',
            resultStatus: 'approved',
            resultPayload: { approval_result: 'approved', business_status: 'executed', business_record: { id: 'contract-1' } },
            outputs: [{ kind: 'approval_result', title: 'Approval decision', text: 'approved', status: 'approved' }],
            artifacts: [{ id: 'artifact-contract-approval', uri: 'artifact://contract/approval.pdf', name: 'contract-approval.pdf', status: 'ready' }],
            approvalInstanceViewVerified: true,
        });
        expect(storedApp.importedRunEvidence.dependencyVerification).toMatchObject({
            schema: 'maclaw.app.install_plan.v1',
            dependencyCount: 1,
            hasMissingRequired: false,
            hasBlockingDependency: false,
        });
        expect(storedApp.manifest.ui.entry).toBe('approval_workspace');
        expect(storedApp.manifest.ui.layouts.approval_workspace).toMatchObject({
            template: 'classic_split',
            density: 'compact',
            primaryRegion: 'left',
            outputRegion: 'right',
        });
        expect(storedApp.manifest.ui.layouts.approval_workspace.regions).toEqual([
            { id: 'contract_form', role: 'input', placement: 'left', visible: true },
            { id: 'contract_instances', role: 'instance_list', placement: 'center', visible: true },
            { id: 'contract_result', role: 'output', placement: 'right', visible: true },
        ]);
        expect(storedApp.installEvidence.workspace_layout).toMatchObject({
            entry: 'approval_workspace',
            template: 'classic_split',
            primary_region: 'left',
            output_region: 'right',
            region_count: 3,
        });
        expect(storedApp.installEvidence.workspace_layout.regions).toEqual([
            { id: 'contract_form', role: 'input', placement: 'left', visible: true },
            { id: 'contract_instances', role: 'instance_list', placement: 'center', visible: true },
            { id: 'contract_result', role: 'output', placement: 'right', visible: true },
        ]);
	        expect(storedApp.installEvidence.hub_package_signature).toMatchObject({
	            algorithm: 'ed25519',
	            public_key_fingerprint: 'sha256:contract-package-key',
	            signed_by: 'enterprise-market',
	        });
	        expect(storedApp.installEvidence.dependency_verification.dependencies[0]).toMatchObject({
	            install_ref_target: 'contract-workflow',
	            install_ref_version: '1.0.0',
	            preflight_status: 'ready',
	            package_sha256: 'sha-contract-workflow',
	            integrity_status: 'ready',
	        });
	        expect(storedApp.versionSnapshot).toMatchObject({ app_entry_version: '1' });

        const installedAppID = storedApp.id;
        const installedRequiresInput = {
            instance_id: 'wf-market-contract-input-1',
            app_id: installedAppID,
            app_name: 'Contract Approval',
            approval_id: 'approval-market-contract-input-1',
            record_approval_id: 'approval-market-contract-input-1',
            record_id: 'contract-input-1',
            dataset_id: 'legal.contracts',
            object_role: 'contract',
            title: 'Contract needs supplement',
            lane: 'my_requests',
            status: 'requires_input',
            current_node: 'contract.requester_supplement',
            current_node_ids: ['contract.submit', 'contract.requester_supplement'],
            workflow_node_ids: ['contract.submit', 'contract.requester_supplement'],
            applicant: 'Legal user',
            approver: 'Legal manager',
            current_assignee: 'Legal user',
            current_assignee_type: 'user',
            workflow_skill_id: 'contract-workflow',
            workflow_version: '1.0.0',
            approval_event: 'contract.submitted',
            result: 'missing signed contract attachment',
            business_status: 'waiting_for_requester',
            result_status: 'requires_input',
            result_payload: {
                approval_result: 'requires_input',
                text: 'missing signed contract attachment',
                requires_input: { fields: ['signed_contract'], message: 'missing signed contract attachment' },
            },
            outputs: [{ id: 'out-contract-input', kind: 'requires_input', title: 'Missing materials', text: 'missing signed contract attachment', status: 'waiting' }],
            updated_at: '2026-06-30T10:00:00Z',
        };
        const installedApprovedAfterSupplement = {
            ...installedRequiresInput,
            lane: 'handled',
            status: 'approved',
            current_node: 'contract.result_feedback',
            current_node_ids: ['contract.submit', 'contract.requester_supplement', 'contract.manager_review', 'contract.result_feedback'],
            workflow_node_ids: ['contract.submit', 'contract.requester_supplement', 'contract.manager_review', 'contract.result_feedback'],
            current_assignee: 'Legal manager',
            current_assignee_type: 'user',
            business_status: 'executed',
            result_status: 'approved',
            result: 'approved after supplement',
            result_payload: {
                approval_result: 'approved',
                business_status: 'executed',
                business_record: { id: 'contract-input-1', status: 'executed' },
                supplemental_input: { form_data: { supplement_note: 'attached signed contract', supplement_reference: 'artifact://contract/signed.pdf' } },
            },
            outputs: [{ id: 'out-contract-final', kind: 'approval_result', title: 'Approval decision', text: 'approved after supplement', status: 'approved' }],
            artifacts: [{ id: 'artifact-contract-final', uri: 'artifact://contract/final.pdf', name: 'contract-final-approval.pdf', status: 'ready' }],
            updated_at: '2026-06-30T10:08:00Z',
        };
        listMaclawAppApprovalInstancesMock.mockImplementation(async (appID) => appID === installedAppID ? [installedRequiresInput] : []);
        startMaclawAppApprovalWorkflowMock.mockResolvedValueOnce({
            started: true,
            approval_id: 'approval-market-contract-input-1',
            instance: { ...installedRequiresInput, status: 'pending', result_status: 'pending', business_status: 'supplemented' },
            workflow_run: {
                ran: true,
                workflow_skill_id: 'contract-workflow',
                progress_instances: [{ ...installedRequiresInput, status: 'running', result_status: 'running', current_node: 'contract.manager_review' }],
                instance: installedApprovedAfterSupplement,
            },
        });

        fireEvent.click(screen.getAllByText('Close')[0]);
        fireEvent.click(screen.getByRole('button', { name: /Contract Approval/ }));
        await waitFor(() => expect(listMaclawAppApprovalInstancesMock).toHaveBeenCalledWith(installedAppID, 'all', expect.any(Number)));
        const runtimeGovernance = await findRuntimeGovernancePanel();
        expect(within(runtimeGovernance).getByText('Source')).not.toBeNull();
        expect(within(runtimeGovernance).getByText(/published · cap-hub-contract-approval/)).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Package signature')).not.toBeNull();
        expect(within(runtimeGovernance).getByText(/ed25519.*sha256:contract-package-key.*enterprise-market/)).not.toBeNull();
	        expect(within(runtimeGovernance).getByText('Dependency verification')).not.toBeNull();
	        expect(within(runtimeGovernance).getByText('Skill dependencies: 1 · Blocking deps: 0')).not.toBeNull();
	        expect(within(runtimeGovernance).getByText('Dependency diagnostics')).not.toBeNull();
	        expect(within(runtimeGovernance).getByText(/contract-workflow.*download:available/)).not.toBeNull();
	        expect(within(runtimeGovernance).getByText('DataSrv')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('DataSrv bindings registered: 1/1')).not.toBeNull();
        await waitFor(() => expect(screen.getByText('Missing materials')).not.toBeNull());
        expect(screen.getByText('signed_contract')).not.toBeNull();
        fireEvent.change(screen.getByPlaceholderText('Describe what was added'), { target: { value: 'attached signed contract' } });
        fireEvent.change(screen.getByPlaceholderText('artifact://...'), { target: { value: 'artifact://contract/signed.pdf' } });
        fireEvent.click(screen.getByText('Continue with supplement'));

        await waitFor(() => expect(startMaclawAppApprovalWorkflowMock).toHaveBeenCalled());
        const continuePayload = startMaclawAppApprovalWorkflowMock.mock.calls.at(-1)?.[0];
        expect(continuePayload).toMatchObject({
            app_id: installedAppID,
            approval_id: 'approval-market-contract-input-1',
            instance_id: 'wf-market-contract-input-1',
            continue_from_instance_id: 'wf-market-contract-input-1',
            dataset_id: 'legal.contracts',
            object_role: 'contract',
            record_id: 'contract-input-1',
            workflow_skill_id: 'contract-workflow',
            workflow_version: '1.0.0',
            business_action: 'supplement',
            run_workflow_skill: true,
        });
        expect(continuePayload.form_data).toMatchObject({ supplement_note: 'attached signed contract', supplement_reference: 'artifact://contract/signed.pdf' });
        expect(continuePayload.result_payload.supplemental_input.form_data).toMatchObject({ supplement_note: 'attached signed contract' });
        listMaclawAppApprovalInstancesMock.mockImplementation(async (appID, lane) => {
            if (appID !== installedAppID) return [];
            return lane === 'handled' || lane === 'all' ? [installedApprovedAfterSupplement] : [];
        });
        listMaclawAppApprovalInstancesAllMock.mockImplementation(async (lane) => lane === 'handled' || lane === 'all' ? [installedApprovedAfterSupplement] : []);

        const workspace = await waitFor(() => document.querySelector('.apps-approval-workspace') as HTMLElement);
        fireEvent.click(within(workspace).getByText('Handled'));
        await waitFor(() => expect(listMaclawAppApprovalInstancesMock).toHaveBeenCalledWith(installedAppID, 'handled', expect.any(Number)));
        await waitFor(() => expect(within(workspace).getAllByText('contract-input-1').length).toBeGreaterThan(0));
        expect(workspace.textContent).toContain('approval-market-contract-input-1');
        expect(workspace.textContent).toContain('wf-market-contract-input-1');
        expect(within(workspace).getAllByText('Approval decision').length).toBeGreaterThan(0);
        expect(within(workspace).getAllByText('contract-final-approval.pdf').length).toBeGreaterThan(0);

        fireEvent.click(screen.getByText('Approval status'));
        await waitFor(() => expect(listMaclawAppApprovalInstancesAllMock).toHaveBeenCalledWith('all', 200));
        const manager = await waitFor(() => document.querySelector('.apps-approval-manager') as HTMLElement);
        fireEvent.click(within(manager).getByText('Handled'));
        await waitFor(() => expect(within(manager).getAllByText('contract-input-1').length).toBeGreaterThan(0));
        expect(manager.textContent).toContain('Contract Approval');
        expect(manager.textContent).toContain('approval-market-contract-input-1');
        expect(manager.textContent).toContain('wf-market-contract-input-1');
        expect(within(manager).getAllByText('Approval decision').length).toBeGreaterThan(0);
        expect(within(manager).getAllByText('contract-final-approval.pdf').length).toBeGreaterThan(0);
    });
    it('shows Hub MaClaw App package signature failures in the market row', async () => {
        searchMixedSkillsMock.mockResolvedValueOnce([{
            id: 'cap-hub-bad-signature-app',
            install_ref: 'cap-hub-bad-signature-app',
            name: 'Signed Contract Intake',
            description: 'Contract approval app distributed through Enterprise Hub',
            source: 'enterprise_hub',
            source_label: 'Enterprise Hub',
            product_kind: 'maclaw_app_skill',
            is_maclaw_app: true,
            maclaw_app_id: 'bad-signature-app',
            maclaw_app_name: 'Signed Contract Intake',
            maclaw_app_description: 'Contract approval app distributed through Enterprise Hub',
            maclaw_app_kind: 'enterprise_approval_app',
            maclaw_app_category: 'Legal',
            maclaw_app_icon: 'shield',
            review_evidence: {
                'contract-approval-reviewed': {
                    run_id: 'run-market-contract-review',
                    test_protocol_fingerprint: 'proto-market-contract-review',
                    result_coverage_primary: 'approval_result',
                    result_coverage_ok: true,
                    result_coverage_covered_count: 3,
                    result_coverage_missing_count: 0,
                    output_count: 1,
                    artifact_count: 1,
                    approval_status: 'approved',
                    current_node: 'contract.result_feedback',
                },
            },
        }]);
        installSelectedMaclawAppPackageFromHubMock.mockRejectedValueOnce(new Error('maclaw app package signature verification failed'));

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText('Search enterprise Hub apps'), { target: { value: 'signed contract' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search Hub' }));

        const row = (await screen.findByText('Signed Contract Intake')).closest('.apps-market-row') as HTMLElement;
        fireEvent.click(within(row).getByRole('button', { name: 'Add: Signed Contract Intake' }));

        await waitFor(() => expect(installSelectedMaclawAppPackageFromHubMock).toHaveBeenCalledWith('cap-hub-bad-signature-app', ['market-bad-signature-app']));
        await waitFor(() => expect(row.getAttribute('data-state')).toBe('blocked'));
        expect(row.textContent).toContain('maclaw app package signature verification failed');
        expect(installMaclawAppDependenciesMock).not.toHaveBeenCalled();
        expect(recordMaclawAppInstallMock).not.toHaveBeenCalled();
        fireEvent.click(getManageTab());
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('Signed Contract Intake'))).toBe(false);
    });
    it('shows Hub MaClaw App dependency install diagnostics when one-click install fails', async () => {
        searchMixedSkillsMock.mockResolvedValueOnce([{
            id: 'cap-hub-approval-dep-fail',
            install_ref: 'cap-hub-approval-dep-fail',
            name: 'Expense Approval Pro',
            description: 'Approval app with a required workflow dependency',
            source: 'enterprise_hub',
            source_label: 'Enterprise Hub',
            product_kind: 'maclaw_app_skill',
            is_maclaw_app: true,
            maclaw_app_id: 'approval-dep-fail',
            maclaw_app_name: 'Expense Approval Pro',
            maclaw_app_description: 'Approval app with a required workflow dependency',
            maclaw_app_kind: 'enterprise_approval_app',
            maclaw_app_category: 'Finance',
            maclaw_app_icon: 'receipt',
        }]);
        installSelectedMaclawAppPackageFromHubMock.mockRejectedValueOnce(new Error('cannot install MaClaw App from Hub: required Skill dependencies are missing or unavailable: signed-workflow: package_integrity_failed at skillhub_download: signature verification failed: public key fingerprint not trusted'));

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText('Search enterprise Hub apps'), { target: { value: 'approval dep' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search Hub' }));

        const row = (await screen.findByText('Expense Approval Pro')).closest('.apps-market-row') as HTMLElement;
        fireEvent.click(within(row).getByRole('button', { name: 'Add: Expense Approval Pro' }));

        await waitFor(() => expect(installSelectedMaclawAppPackageFromHubMock).toHaveBeenCalledWith('cap-hub-approval-dep-fail', ['market-approval-dep-fail']));
        await waitFor(() => expect(row.getAttribute('data-state')).toBe('blocked'));
        expect(row.textContent).toContain('signed-workflow');
        expect(row.textContent).toContain('package_integrity_failed');
        expect(row.textContent).toContain('skillhub_download');
        expect(row.textContent).toContain('signature verification failed');
        expect(row.textContent).toContain('public key fingerprint not trusted');
        expect(installMaclawAppDependenciesMock).not.toHaveBeenCalled();
        fireEvent.click(getManageTab());
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('Expense Approval Pro'))).toBe(false);
    });
    it('ignores Hub MaClaw App search results without an app id', async () => {
        searchMixedSkillsMock.mockResolvedValueOnce([{
            id: 'cap-hub-missing-app-id',
            install_ref: 'cap-hub-missing-app-id',
            name: 'Missing App ID',
            description: 'Hub package metadata without a concrete app id',
            source: 'enterprise_hub',
            source_label: 'Enterprise Hub',
            product_kind: 'maclaw_app_skill',
            is_maclaw_app: true,
            maclaw_app_name: 'Missing App ID',
            maclaw_app_kind: 'enterprise_approval_app',
        }]);

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText('Search enterprise Hub apps'), { target: { value: 'missing' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search Hub' }));

        await waitFor(() => expect(searchMixedSkillsMock).toHaveBeenCalledWith('missing'));
        expect(screen.queryByText('Missing App ID')).toBeNull();
        expect(installSelectedMaclawAppPackageFromHubMock).not.toHaveBeenCalled();
        expect(installMaclawAppPackageFromHubMock).not.toHaveBeenCalled();
    });
    it('blocks one-click market install when a required dependency is unavailable', async () => {
        installMaclawAppDependenciesMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'market-contract-archive', name: '合同归档', kind: 'tool_app' }],
            dependencies: [{
                id: 'contract-archive',
                version: '1.2.0',
                kind: 'runtime_skill',
                source: 'hub',
                required: true,
                installed: true,
                installed_status: 'disabled',
                health: 'disabled',
                action: 'blocked',
                app_ids: ['contract-archive'],
            }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        });
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        const row = screen.getByText('合同归档').closest('.apps-market-row') as HTMLElement;
        fireEvent.click(within(row).getByRole('button', { name: '添加: 合同归档' }));

        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalledTimes(1));
        expect(recordMaclawAppInstallMock).not.toHaveBeenCalled();
        expect(within(row).getByText('依赖验证发现阻断项')).not.toBeNull();
        const dependencyPanel = row.querySelector('.apps-dependency-verification') as HTMLElement;
        expect(dependencyPanel).not.toBeNull();
        fireEvent.click(within(dependencyPanel).getByText('查看详情'));
        expect(within(dependencyPanel).getByText('contract-archive')).not.toBeNull();
        expect(dependencyPanel.textContent).toContain('runtime_skill');
        expect(dependencyPanel.textContent).toContain('hub');
        expect(dependencyPanel.textContent).toContain('v1.2.0');
        expect(dependencyPanel.textContent).toContain('disabled');
        expect(row.getAttribute('data-state')).toBe('blocked');
        fireEvent.click(getManageTab());
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('合同归档'))).toBe(false);
    });

    it('installs an app from a pasted market manifest', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'market-doc-archive',
                        name: '文档归档',
                        description: 'Archive documents',
                        category: '文档处理',
                        kind: 'tool_app',
                        icon: 'contract',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'doc-archive', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                    },
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));

        await waitFor(() => expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull());
        expect(screen.getAllByText('文档归档').length).toBeGreaterThan(0);
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        expect(screen.getAllByText('文档归档').length).toBeGreaterThan(0);
    });

    it('shows DataSrv registration status after installing an enterprise normal DataSrv app manifest', async () => {
        recordMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.installs.v1',
            app_count: 1,
            datasrv_registration: {
                synced: true,
                eligible_count: 1,
                synced_count: 1,
                failed_count: 0,
                items: [{ app_id: 'customer-import', synced: true, role_binding_count: 1 }],
            },
        });
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'enterprise_app_pack',
                    app: {
                        id: 'customer-import',
                        name: '客户导入',
                        description: 'Import customer profiles',
                        category: 'CRM',
                        kind: 'enterprise_normal_app',
                        icon: 'customer',
                        launchMode: 'agent_dynamic_ui',
                        binding: {
                            datasrv: { domain: 'sales', datasetID: 'sales.customers', preferredAction: 'sales.customer_upsert' },
                        },
                    },
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));

        await waitFor(() => expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull());
        const installResult = document.querySelector('.apps-install-result') as HTMLElement;
        expect(within(installResult).getByText('客户导入')).not.toBeNull();
        await waitFor(() => expect(within(installResult).getByText(/DataSrv/)).not.toBeNull());
    });
    it('blocks enterprise app installation when install evidence cannot be saved', async () => {
        recordMaclawAppInstallMock.mockRejectedValueOnce(new Error('DataSrv registration failed'));
        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(/Paste app package JSON/), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'enterprise_app_pack',
                    app: {
                        id: 'customer-import-audit-required',
                        name: 'Customer Import Audit Required',
                        description: 'Import customer profiles',
                        category: 'CRM',
                        kind: 'enterprise_normal_app',
                        icon: 'customer',
                        launchMode: 'agent_dynamic_ui',
                        binding: {
                            datasrv: { domain: 'sales', datasetID: 'sales.customers', preferredAction: 'sales.customer_upsert' },
                            appSkill: { id: 'customer-import-skill', version: '1.0.0', source: 'hub' },
                        },
                    },
                }),
            },
        });
        fireEvent.click(screen.getByText('Install'));

        await waitFor(() => expect(recordMaclawAppInstallMock).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(screen.getByText(/Invalid app package: DataSrv registration failed/)).not.toBeNull());
        expect(screen.queryByText('Installed: 1 · Skipped: 0')).toBeNull();
        fireEvent.click(getManageTab());
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('Customer Import Audit Required'))).toBe(false);
    });

    it('does not exceed the pinned app limit when installing pinned market apps', async () => {
        render(<AppsPage lang="zh-Hans" />);

        for (const name of ['文档脱敏', '表格分析', '库存盘点', '合同审查', '发票审核', '网页采集']) {
            const tile = screen.getAllByText(name)[0].closest('.apps-app-tile') as HTMLButtonElement;
            fireEvent.contextMenu(tile, { clientX: 90, clientY: 180 });
            fireEvent.click(screen.getByRole('menuitem', { name: '设为常用' }));
        }

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'pinned-market-doc',
                        name: '市场置顶文档',
                        description: 'Pinned market app',
                        category: '文档处理',
                        kind: 'tool_app',
                        icon: 'contract',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'pinned-market-doc', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                        panel: { pinned: true, accent: '#7c3f58' },
                    },
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        await waitFor(() => expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull());
        fireEvent.click(getManageTab());

        expect(screen.getByText('8/8')).not.toBeNull();
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('市场置顶文档')) as HTMLElement;
        expect(within(row).getByTitle('常用应用已满 8 个，请先取消一个')).not.toBeNull();
        fireEvent.click(within(row).getByTitle(manageManifestTitle));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(manifest.app.panel.pinned).toBe(false);
    });

    it('installs tool apps from a pasted maclaw.apps.json manifest', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'doc-stamp',
                            skill_id: 'doc-tools',
                            name: '文档盖章',
                            description: 'Stamp documents',
                            category: '文档处理',
                            icon: 'contract',
                            input_mode: 'file',
                        },
                        {
                            id: 'doc-summary',
                            skill_id: 'doc-tools',
                            name: '文档摘要',
                            description: 'Summarize documents',
                            category: '文档处理',
                            icon: 'sheet',
                            input_mode: 'file',
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));

        expect(screen.getByText('已安装: 2 · 已跳过: 0')).not.toBeNull();
        expect(screen.getAllByText('文档盖章').length).toBeGreaterThan(0);
        expect(screen.getAllByText('文档摘要').length).toBeGreaterThan(0);
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        expect(screen.getAllByText('文档盖章').length).toBeGreaterThan(0);
        expect(screen.getAllByText('文档摘要').length).toBeGreaterThan(0);
    });

    it('upgrades installed market apps when a higher manifest version is pasted', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'document-redaction',
                        name: '文档脱敏增强',
                        version: 2,
                        description: 'Updated redaction workflow',
                        category: '市场分类',
                        kind: 'tool_app',
                        icon: 'pdf',
                        source: 'market',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'admin-doc-redact-v2', appDefinitionFile: 'maclaw.apps.json', inputMode: 'mixed', outputModes: ['pdf'] } },
                        panel: { pinned: false, accent: '#b45309' },
                    },
                }),
            },
        });

        expect(screen.getByText(/v1 -> v2/)).not.toBeNull();
        expect(screen.getByText(/v1 -> v2/)).not.toBeNull();
        const upgradePreviewRow = screen.getByText('文档脱敏增强').closest('.apps-install-preview__row') as HTMLElement;
        expect(within(upgradePreviewRow).getAllByText(/admin-doc-redact-v2/).length).toBeGreaterThan(0);
        const marketInstall = document.querySelector('.apps-market-install') as HTMLElement;
        fireEvent.click(within(marketInstall).getByText('安装'));
        await waitFor(() => expect(within(marketInstall).getByText('确认安装')).not.toBeNull());
        expect(within(marketInstall).getByText(/选中的升级包包含高风险新权限/).closest('[role="alert"]')).not.toBeNull();
        expect(screen.queryByText('已安装: 0 · 已升级: 1 · 已跳过: 0')).toBeNull();
        fireEvent.click(within(marketInstall).getByText('确认安装'));
        expect(screen.getByText('已安装: 0 · 已升级: 1 · 已跳过: 0')).not.toBeNull();
        expect(screen.getByRole('status').getAttribute('aria-live')).toBe('polite');
        const upgradeResult = document.querySelector('.apps-install-result') as HTMLElement;
        expect(within(upgradeResult).getByText('文档脱敏增强')).not.toBeNull();
        expect(within(upgradeResult).getByText('已升级')).not.toBeNull();
        expect(within(upgradeResult).getByText('已升级 v1 -> v2')).not.toBeNull();

        fireEvent.click(getManageTab());
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('文档脱敏增强')) as HTMLElement;
        expect(row).toBeTruthy();
        expect(row.textContent).toContain('文档处理');
        fireEvent.click(within(row).getByTitle(manageManifestTitle));
        expect(screen.getByText(/"version": 2/)).not.toBeNull();
        expect(screen.getByText(/"id": "admin-doc-redact-v2"/)).not.toBeNull();
        expect(screen.getByText(/"icon": "shield"/)).not.toBeNull();
        expect(screen.getByText(/"accent": "#7a5c95"/)).not.toBeNull();
    });


    it('shows dependency verification in market package preview and install results', async () => {
        const installPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'market-dep-preview-app', name: 'Dependency Preview App', kind: 'tool_app' }],
            dependencies: [{ id: 'dep-preview-skill', version: '1.0.0', kind: 'runtime_skill', source: 'hub', install_ref: 'cap-hub-dep-preview-skill', required: true, installed: true, health: 'ready', action: 'skip', app_ids: ['market-dep-preview-app'] }],
            has_missing_required: false,
            has_blocking_dependency: false,
        };
        planMaclawAppInstallMock.mockResolvedValue(installPlan);

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.click(await screen.findByText('Import app package'));
        fireEvent.change(document.querySelector('.apps-market-install textarea') as HTMLTextAreaElement, {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'market-dep-preview-app',
                        name: 'Dependency Preview App',
                        description: 'Preview dependency verification',
                        category: 'Legal',
                        kind: 'tool_app',
                        icon: 'contract',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'dep-preview-skill', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                    },
                }),
            },
        });

        await waitFor(() => expect(screen.getByText('Dependency verification complete')).not.toBeNull());
        const previewVerification = document.querySelector('.apps-install-preview .apps-dependency-verification') as HTMLElement;
        expect(previewVerification).not.toBeNull();
        expect(within(previewVerification).getByText(/Skill dependencies: 1/)).not.toBeNull();
        expect(within(previewVerification).getByText(/Blocking deps: 0/)).not.toBeNull();
        expect(within(previewVerification).getByText('dep-preview-skill')).not.toBeNull();
	        expect(within(previewVerification).getAllByText(/ref:cap-hub-dep-preview-skill/).length).toBeGreaterThan(0);

        fireEvent.click(screen.getByText('Install'));

        await waitFor(() => expect(screen.getByText('Installed: 1 · Skipped: 0')).not.toBeNull());
        const resultPanel = document.querySelector('.apps-result-panel') as HTMLElement;
        expect(within(resultPanel).getByText('Dependency verification complete')).not.toBeNull();
        expect(within(resultPanel).getByText('dep-preview-skill')).not.toBeNull();
	        expect(within(resultPanel).getAllByText(/ref:cap-hub-dep-preview-skill/).length).toBeGreaterThan(0);
        expect(recordMaclawAppInstallMock).toHaveBeenCalledTimes(1);
    });

    it('previews manifest apps before installing from market', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'preview-doc', skill_id: 'doc-tools', name: '预览文档', description: 'Preview doc', category: '文档处理', icon: 'contract', input_mode: 'file' },
                        { id: 'preview-doc', skill_id: 'doc-tools', name: '预览文档副本', description: 'Preview duplicate', category: '文档处理', icon: 'contract', input_mode: 'file' },
                    ],
                }),
            },
        });

        expect(screen.getByText('安装预览')).not.toBeNull();
        expect(screen.getByText('1/2')).not.toBeNull();
        expect(screen.getByText('可安装 1 · 可升级 0 · 将跳过 1')).not.toBeNull();
        expect(screen.getByText('预览文档')).not.toBeNull();
        expect(screen.getByText('预览文档副本')).not.toBeNull();
        expect(screen.getByText('将安装')).not.toBeNull();
        expect(screen.getByText('将跳过 · 重复应用')).not.toBeNull();
        const duplicateRow = screen.getByText('预览文档副本').closest('.apps-install-preview__row') as HTMLElement;
        expect(duplicateRow.title).toBe('将跳过 · 重复应用');
        expect(within(duplicateRow).getByRole('checkbox', { name: '预览文档副本 · 将跳过 · 重复应用' })).not.toBeNull();
    });

    it('installs only selected apps from the market preview', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'optional-doc', skill_id: 'doc-tools', name: '可选文', description: 'Optional doc', category: '文档处理', icon: 'contract', input_mode: 'file' },
                        { id: 'kept-doc', skill_id: 'doc-tools', name: '保留文档', description: 'Kept doc', category: '文档处理', icon: 'sheet', input_mode: 'file' },
                    ],
                }),
            },
        });

        const preview = document.querySelector('.apps-install-preview') as HTMLElement;
        fireEvent.click(within(preview).getAllByRole('checkbox')[0]);
        expect(screen.getByText('1/2')).not.toBeNull();
        expect(screen.getByText('可安装 2 · 可升级 0 · 将跳过 1')).not.toBeNull();
        expect(screen.getByText('将跳过 · 未选择')).not.toBeNull();
        fireEvent.click(screen.getByText('安装'));

        expect(screen.getByText('已安装: 1 · 已跳过: 1')).not.toBeNull();
        const installResult = document.querySelector('.apps-install-result') as HTMLElement;
        expect(within(installResult).getByText('可选文')).not.toBeNull();
        expect(within(installResult).getByText('已跳过')).not.toBeNull();
        expect(within(installResult).getByText('未选择')).not.toBeNull();
        expect(within(installResult).getByText('保留文档')).not.toBeNull();
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        expect(screen.queryByText('可选文')).toBeNull();
        expect(screen.getAllByText('保留文档').length).toBeGreaterThan(0);
    });

    it('installs dependencies and records audit only for selected apps in a pasted pack', async () => {
        const fullPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [
                { id: 'pack-unselected', name: 'Unselected Pack App', kind: 'tool_app' },
                { id: 'pack-kept', name: 'Kept Pack App', kind: 'tool_app' },
            ],
            dependencies: [
                { id: 'unselected-skill', kind: 'runtime_skill', source: 'hub', required: true, installed: false, health: 'missing', action: 'blocked', app_ids: ['pack-unselected'] },
                { id: 'kept-skill', kind: 'runtime_skill', source: 'hub', required: true, installed: false, health: 'missing', action: 'blocked', app_ids: ['pack-kept'] },
            ],
            has_missing_required: true,
            has_blocking_dependency: true,
        };
        const selectedPlan = {
            ...fullPlan,
            apps: [{ id: 'pack-kept', name: 'Kept Pack App', kind: 'tool_app' }],
            dependencies: [{ id: 'kept-skill', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed', app_ids: ['pack-kept'] }],
            has_missing_required: false,
            has_blocking_dependency: false,
        };
        planMaclawAppInstallMock.mockResolvedValue(fullPlan);
        installMaclawAppDependenciesMock.mockResolvedValueOnce(selectedPlan);

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.click(await screen.findByText('Import app package'));
        fireEvent.change(screen.getByPlaceholderText('Paste app package JSON (maclaw.app.v1 / maclaw.app.pack.v1 / maclaw.apps.json)'), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.pack.v1',
                    privateMarker: 'x_maclaw_apps',
                    apps: [
                        {
                            schema: 'maclaw.app.v1',
                            privateMarker: 'x_maclaw_apps',
                            installUnit: 'skill',
                            app: {
                                id: 'pack-unselected',
                                name: 'Unselected Pack App',
                                description: 'Do not install this app',
                                category: 'Legal',
                                kind: 'tool_app',
                                icon: 'contract',
                                launchMode: 'fixed_skill_ui',
                                binding: { skill: { id: 'unselected-skill', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                            },
                        },
                        {
                            schema: 'maclaw.app.v1',
                            privateMarker: 'x_maclaw_apps',
                            installUnit: 'skill',
                            app: {
                                id: 'pack-kept',
                                name: 'Kept Pack App',
                                description: 'Install this app',
                                category: 'Legal',
                                kind: 'tool_app',
                                icon: 'sheet',
                                launchMode: 'fixed_skill_ui',
                                binding: { skill: { id: 'kept-skill', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                            },
                        },
                    ],
                }),
            },
        });

        await waitFor(() => expect(screen.getByText('Required Skill dependencies are missing or unavailable. Install or enable them first.')).not.toBeNull());
        const preview = document.querySelector('.apps-install-preview') as HTMLElement;
        fireEvent.click(within(preview).getByRole('checkbox', { name: 'Unselected Pack App · Will install' }));
        fireEvent.click(screen.getByText('Install'));

        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalledTimes(1));
        const dependencyPackage = JSON.parse(String(installMaclawAppDependenciesMock.mock.calls[0][0]));
        expect(dependencyPackage.schema).toBe('maclaw.app.v1');
        expect(dependencyPackage.app.id).toBe('market-pack-kept');
        expect(JSON.stringify(dependencyPackage)).not.toContain('pack-unselected');
        expect(JSON.stringify(dependencyPackage)).not.toContain('market-pack-unselected');
        await waitFor(() => expect(recordMaclawAppInstallMock).toHaveBeenCalledTimes(1));
        const auditPackage = JSON.parse(String(recordMaclawAppInstallMock.mock.calls[0][0]));
        expect(auditPackage.schema).toBe('maclaw.app.v1');
        expect(auditPackage.app.id).toBe('market-pack-kept');
        expect(JSON.stringify(auditPackage)).not.toContain('pack-unselected');
        expect(JSON.stringify(auditPackage)).not.toContain('market-pack-unselected');
        expect(screen.getByText('Installed: 1 · Skipped: 1')).not.toBeNull();
    });

    it('scopes pasted pack dependency verification to selected apps', async () => {
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [
                { id: 'pack-unselected-ready-scope', name: 'Unselected Scoped App', kind: 'tool_app' },
                { id: 'pack-kept-ready-scope', name: 'Kept Scoped App', kind: 'tool_app' },
            ],
            dependencies: [
                { id: 'unselected-scope-skill', kind: 'runtime_skill', source: 'hub', required: true, installed: false, health: 'missing', action: 'blocked', app_ids: ['pack-unselected-ready-scope'] },
                { id: 'kept-scope-skill', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'skip', app_ids: ['pack-kept-ready-scope'] },
            ],
            has_missing_required: true,
            has_blocking_dependency: true,
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.click(await screen.findByText('Import app package'));
        fireEvent.change(screen.getByPlaceholderText('Paste app package JSON (maclaw.app.v1 / maclaw.app.pack.v1 / maclaw.apps.json)'), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.pack.v1',
                    privateMarker: 'x_maclaw_apps',
                    apps: [
                        {
                            schema: 'maclaw.app.v1',
                            privateMarker: 'x_maclaw_apps',
                            installUnit: 'skill',
                            app: {
                                id: 'pack-unselected-ready-scope',
                                name: 'Unselected Scoped App',
                                description: 'Missing dependency should not block after deselect',
                                category: 'Legal',
                                kind: 'tool_app',
                                icon: 'contract',
                                launchMode: 'fixed_skill_ui',
                                binding: { skill: { id: 'unselected-scope-skill', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                            },
                        },
                        {
                            schema: 'maclaw.app.v1',
                            privateMarker: 'x_maclaw_apps',
                            installUnit: 'skill',
                            app: {
                                id: 'pack-kept-ready-scope',
                                name: 'Kept Scoped App',
                                description: 'Dependency is ready',
                                category: 'Legal',
                                kind: 'tool_app',
                                icon: 'sheet',
                                launchMode: 'fixed_skill_ui',
                                binding: { skill: { id: 'kept-scope-skill', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                            },
                        },
                    ],
                }),
            },
        });

        await waitFor(() => expect(screen.getByText('Required Skill dependencies are missing or unavailable. Install or enable them first.')).not.toBeNull());
        const preview = document.querySelector('.apps-install-preview') as HTMLElement;
        fireEvent.click(within(preview).getByRole('checkbox', { name: 'Unselected Scoped App · Will install' }));

        await waitFor(() => expect(within(preview).getByText('Dependency verification complete')).not.toBeNull());
        expect(within(preview).queryByText('Dependency verification found blocking items')).toBeNull();
    });

    it('does not bypass dependency installation when market preview plan is still loading', async () => {
        const selectedPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'market-race-install', name: 'Race Install App', kind: 'tool_app' }],
            dependencies: [{ id: 'race-install-skill', kind: 'runtime_skill', source: 'hub', required: true, installed: false, health: 'missing', action: 'blocked', app_ids: ['market-race-install'] }],
            has_missing_required: true,
            has_blocking_dependency: true,
        };
        const repairedPlan = {
            ...selectedPlan,
            dependencies: [{ id: 'race-install-skill', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed', app_ids: ['market-race-install'] }],
            has_missing_required: false,
            has_blocking_dependency: false,
        };
        let resolvePreviewPlan: (value: unknown) => void = () => undefined;
        planMaclawAppInstallMock
            .mockImplementationOnce(() => new Promise((resolve) => { resolvePreviewPlan = resolve; }))
            .mockResolvedValueOnce(selectedPlan);
        installMaclawAppDependenciesMock.mockResolvedValueOnce(repairedPlan);

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.click(await screen.findByText('Import app package'));
        fireEvent.change(screen.getByPlaceholderText('Paste app package JSON (maclaw.app.v1 / maclaw.app.pack.v1 / maclaw.apps.json)'), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'race-install',
                        name: 'Race Install App',
                        description: 'Install before preview plan resolves',
                        category: 'Legal',
                        kind: 'tool_app',
                        icon: 'sheet',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'race-install-skill', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                    },
                }),
            },
        });
        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalledTimes(1));

        fireEvent.click(screen.getByText('Install'));

        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalledTimes(2));
        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalledTimes(1));
        const dependencyPackage = JSON.parse(String(installMaclawAppDependenciesMock.mock.calls[0][0]));
        expect(dependencyPackage.schema).toBe('maclaw.app.v1');
        expect(dependencyPackage.app.id).toBe('market-race-install');
        await waitFor(() => expect(recordMaclawAppInstallMock).toHaveBeenCalledTimes(1));
        expect(screen.getByText('Installed: 1 · Skipped: 0')).not.toBeNull();

        resolvePreviewPlan(selectedPlan);
    });

    it('keeps dependency verification visible after single market app install', async () => {
        const installPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'market-contract-archive', name: '合同归档', kind: 'tool_app' }],
	            dependencies: [{
	                id: 'contract-archive-skill',
	                version: '1.0.0',
	                kind: 'runtime_skill',
	                source: 'hub',
	                required: true,
	                installed: true,
	                health: 'ready',
	                action: 'installed',
	                app_ids: ['market-contract-archive'],
	                install_ref: 'cap-hub-contract-archive-skill',
	                install_ref_status: 'ok',
	                install_ref_target: 'contract-archive-skill',
	                install_ref_version: '1.0.0',
	                preflight_status: 'ready',
	                preflight_code: 'skillhub_target_ready',
	                preflight_stage: 'skillhub_preflight',
	                package_download_url: 'https://hub.example/skills/contract-archive/download',
	                package_sha256: 'sha-contract-archive',
	                package_signature: 'sig-contract-archive',
	                integrity_status: 'ready',
	                integrity_code: 'package_integrity_verified',
	                integrity_stage: 'skillhub_download',
	            }],
            has_missing_required: false,
            has_blocking_dependency: false,
        };
        installMaclawAppDependenciesMock.mockResolvedValueOnce(installPlan);
        recordMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_record.v1',
            app_count: 1,
            dependencies: installPlan.dependencies,
            app_versions: {
                'market-contract-archive': {
                    app_entry_version: '3',
                    app_skill: { id: 'contract-archive-skill', version: '1.0.0', kind: 'app_skill', source: 'hub' },
                },
            },
            install_evidence: {
                'market-contract-archive': {
                    workspace_layout: { entry: 'tool_workspace', template: 'document_workspace', density: 'compact' },
                    result_contract: { primary: 'document', types: ['document', 'content', 'artifact'] },
                    test_evidence: {
                        runId: 'run-contract-archive',
                        testProtocolFingerprint: 'proto-contract-archive',
                        primaryResult: 'document',
                        resultPayload: { document: 'contract-archive.pdf', content: 'archive ready' },
                        outputs: [{ kind: 'document', title: 'Archived contract', text: 'contract-archive.pdf', status: 'ready' }],
                        artifacts: [{ id: 'contract-archive-pdf', name: 'contract-archive.pdf', uri: 'artifact://contract/archive.pdf', status: 'ready' }],
                        resultCoverage: { ok: true, primary: 'document', coveredTypes: ['document', 'content', 'artifact'], missingTypes: [] },
                    },
                    dependencies: installPlan.dependencies,
                },
            },
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        const row = Array.from(document.querySelectorAll('.apps-market-row')).find((item) => item.textContent?.includes('合同归档')) as HTMLElement;
        expect(row).toBeTruthy();
        fireEvent.click(within(row).getByRole('button', { name: /Add:/ }));

        await waitFor(() => expect(within(row).getByText('Dependency verification complete')).not.toBeNull());
        const verification = row.querySelector('.apps-dependency-verification') as HTMLElement;
        expect(verification).not.toBeNull();
	        expect(within(verification).getByText(/Skill dependencies: 1/)).not.toBeNull();
	        expect(within(verification).getByText(/Blocking deps: 0/)).not.toBeNull();
	        fireEvent.click(within(verification).getByText('Show details'));
	        expect(within(verification).getByText('contract-archive-skill')).not.toBeNull();
	        const trace = within(verification).getByLabelText('Install trace');
	        expect(within(trace).getByText('Resolve')).not.toBeNull();
	        expect(within(trace).getByText(/ref:cap-hub-contract-archive-skill.*status:ok.*target:contract-archive-skill@1\.0\.0/)).not.toBeNull();
	        expect(within(trace).getByText('Preflight')).not.toBeNull();
	        expect(within(trace).getByText(/ready.*code:skillhub_target_ready.*stage:skillhub_preflight/)).not.toBeNull();
	        expect(within(trace).getByText('Download')).not.toBeNull();
	        expect(within(trace).getByText(/available.*sha:sha-contract-archive.*signature:available/)).not.toBeNull();
	        expect(within(trace).getByText('Integrity')).not.toBeNull();
	        const versionSnapshot = row.querySelector('.apps-install-version-snapshot') as HTMLElement;
        expect(versionSnapshot).not.toBeNull();
        expect(versionSnapshot.getAttribute('aria-label')).toBe('Version snapshot');
        expect(within(versionSnapshot).getByText('Version')).not.toBeNull();
        expect(within(versionSnapshot).getByText('v3')).not.toBeNull();
        expect(within(versionSnapshot).getByText(miniAppLabels.skillField.en)).not.toBeNull();
        expect(within(versionSnapshot).getByText('contract-archive-skill · app_skill · hub · v1.0.0')).not.toBeNull();
        const evidenceSnapshot = row.querySelector('.apps-install-evidence-snapshot') as HTMLElement;
        expect(evidenceSnapshot).not.toBeNull();
        expect(evidenceSnapshot.getAttribute('aria-label')).toBe('Test evidence');
        expect(within(evidenceSnapshot).getByText('Workspace layout')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('tool_workspace · document_workspace · compact')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('Result contract')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('document · 3 types')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('run-contract-archive · proto-contract-archive')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('Result coverage')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('document · Covered: 3')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('Output: 1 · Output artifacts: 1')).not.toBeNull();
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            const installed = (stored.customApps || []).find((item: any) => item.id === 'market-contract-archive');
            expect(installed?.installEvidence?.test_evidence?.runId).toBe('run-contract-archive');
            expect(installed?.installEvidence?.dependencies?.[0]?.id).toBe('contract-archive-skill');
            expect(installed?.installEvidence?.test_evidence?.artifacts?.[0]?.name).toBe('contract-archive.pdf');
        });
        fireEvent.click(screen.getAllByText('Close')[0]);
        fireEvent.click(screen.getAllByText('合同归档')[0]);
        const runtimeGovernance = await findRuntimeGovernancePanel();
        expect(within(runtimeGovernance).getByText('Workspace layout')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('tool_workspace · document_workspace · compact')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Result contract')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('document · 3 types')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Test evidence')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('run-contract-archive · proto-contract-archive')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Result coverage')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('document · Covered: 3')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Result package')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Output: 1 · Output artifacts: 1')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Dependency verification')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Skill dependencies: 1 · Blocking deps: 0')).not.toBeNull();
        expect(recordMaclawAppInstallMock).toHaveBeenCalledTimes(1);
    });

    it('blocks single market app install when backend governance review fails', async () => {
        const blockedPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'market-contract-archive', name: '合同归档', kind: 'tool_app' }],
            dependencies: [{ id: 'contract-archive-skill', version: '1.0.0', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'skip', app_ids: ['market-contract-archive'] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: true,
            governance_review_issues: [{ path: 'apps[0].governance.testEvidence', severity: 'error', message: 'missing successful local run evidence' }],
        };
        installMaclawAppDependenciesMock.mockResolvedValueOnce(blockedPlan);

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        const row = Array.from(document.querySelectorAll('.apps-market-row')).find((item) => item.textContent?.includes('合同归档')) as HTMLElement;
        expect(row).toBeTruthy();
        fireEvent.click(within(row).getByRole('button', { name: /Add:/ }));

        await waitFor(() => expect(within(row).getByText('Dependency verification found blocking items')).not.toBeNull());
        const blockedVerification = row.querySelector('.apps-dependency-verification') as HTMLElement;
        fireEvent.click(within(blockedVerification).getByText('Show details'));
        expect(within(blockedVerification).getByText(/missing successful local run evidence/)).not.toBeNull();
        expect(recordMaclawAppInstallMock).not.toHaveBeenCalled();
        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        expect((stored.customApps || []).some((item: any) => item.id === 'market-contract-archive')).toBe(false);
    });

    it('restores single app install evidence from top-level market install records', async () => {
        const installPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'market-contract-archive', name: '合同归档', kind: 'tool_app' }],
            dependencies: [
                { id: 'contract-archive-skill', version: '1.0.0', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'skip', app_ids: ['market-contract-archive'] },
                { id: 'other-app-skill', version: '2.0.0', kind: 'runtime_skill', source: 'hub', required: true, installed: false, health: 'missing', action: 'blocked', app_ids: ['other-market-app'] },
            ],
            has_missing_required: false,
            has_blocking_dependency: false,
        };
        installMaclawAppDependenciesMock.mockResolvedValueOnce(installPlan);
        recordMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_record.v1',
            package_sha256: 'sha256-top-level-install',
            source: 'market',
            installed_at: '2026-06-27T09:05:00Z',
            app_count: 1,
            apps: [{ id: 'market-contract-archive', name: '合同归档', kind: 'tool_app' }],
            dependencies: installPlan.dependencies,
            dependency_verification: {
                schema: 'maclaw.app.install_plan.v1',
                verified_at: '2026-06-27T09:00:00Z',
                dependency_count: 2,
                has_missing_required: false,
                has_blocking_dependency: false,
                dependencies: installPlan.dependencies,
            },
            workspace_layout: { entry: 'tool_workspace', template: 'document_workspace', density: 'compact' },
            result_contract: { primary: 'artifact', types: ['artifact', 'content'] },
            test_evidence: {
                run_id: 'run-top-level-install',
                test_protocol_fingerprint: 'proto-top-level-install',
                result_coverage: { ok: true, primary: 'artifact', coveredTypes: ['artifact', 'content'], missingTypes: [] },
                approval_instance: {
                    record_approval_id: 'approval-top-level-install',
                    status: 'approved',
                    current_node: 'contract.result',
                    workflow_skill_id: 'contract-approval-flow',
                    result_status: 'approved',
                    result_payload: { approval_result: 'approved', business_status: 'archived', business_record: { id: 'contract-archive-1' } },
                    outputs: [{ kind: 'approval_result', text: 'contract archived', status: 'ready' }],
                    artifacts: [{ id: 'artifact-contract-archive', name: 'contract-archive.pdf', status: 'ready' }],
                    approval_instance_view_verified: true,
                },
            },
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        const row = Array.from(document.querySelectorAll('.apps-market-row')).find((item) => item.textContent?.includes('合同归档')) as HTMLElement;
        expect(row).toBeTruthy();
        fireEvent.click(within(row).getByRole('button', { name: /Add:/ }));

        await waitFor(() => expect(within(row).getByText('Dependency verification complete')).not.toBeNull());
        const evidenceSnapshot = row.querySelector('.apps-install-evidence-snapshot') as HTMLElement;
        expect(evidenceSnapshot).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('run-top-level-install · proto-top-level-install')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('Skill dependencies: 1 · Blocking deps: 0')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('Result coverage')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('artifact · Covered: 2')).not.toBeNull();
        expect(row.textContent).not.toContain('other-app-skill');
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            const installed = (stored.customApps || []).find((item: any) => item.id === 'market-contract-archive');
            expect(installed?.installEvidence?.schema).toBe('maclaw.app.install_record.v1');
            expect(installed?.installEvidence?.package_sha).toBe('sha256-top-level-install');
            expect(installed?.installEvidence?.package_sha256).toBe('sha256-top-level-install');
            expect(installed?.installEvidence?.source).toBe('market');
            expect(installed?.installEvidence?.installed_at).toBe('2026-06-27T09:05:00Z');
            expect(installed?.installEvidence?.apps).toEqual([{ id: 'market-contract-archive', name: '合同归档', kind: 'tool_app' }]);
            expect(installed?.installEvidence?.test_evidence?.run_id).toBe('run-top-level-install');
            expect(installed?.installEvidence?.dependency_verification?.dependencies).toHaveLength(1);
            expect(installed?.installEvidence?.dependency_verification?.dependencies?.[0]?.id).toBe('contract-archive-skill');
            expect(installed?.importedRunEvidence?.approvalInstance?.instanceId).toBe('approval-top-level-install');
            expect(installed?.importedRunEvidence?.approvalInstance?.approvalID).toBe('approval-top-level-install');
            expect(installed?.importedRunEvidence?.approvalInstance?.resultPayload?.business_status).toBe('archived');
            expect(installed?.importedRunEvidence?.resultPayload?.business_record?.id).toBe('contract-archive-1');
            expect(installed?.importedRunEvidence?.outputs?.[0]?.text).toBe('contract archived');
            expect(installed?.importedRunEvidence?.artifacts?.[0]?.name).toBe('contract-archive.pdf');
            expect(JSON.stringify(installed?.installEvidence)).not.toContain('other-app-skill');
        });
    });

    it('restores dynamic layout and install evidence from stored app entries after cold start', async () => {
        const app = {
            id: 'cold-start-layout-app',
            name: 'Cold Start Console',
            description: 'Restored local app',
            category: 'Tools',
            kind: 'tool_app',
            icon: 'pdf',
            accent: '#28705f',
            version: 4,
            source: 'market',
            manifest: {
                schema: 'maclaw.app.v1',
                installUnit: 'enterprise_app_pack',
                privateMarker: 'x_maclaw_apps',
                entryKind: 'tool_app',
                launchMode: 'agent_dynamic_ui',
                dependencies: { skills: [{ id: 'cold-layout-skill', version: '1.2.0', kind: 'runtime_skill', required: true, source: 'hub' }] },
                ui: {
                    schema: 'maclaw.app.ui.v1',
                    generated: true,
                    entry: 'tool_workspace',
                    layouts: {
                        tool_workspace: {
                            template: 'dashboard',
                            density: 'compact',
                            primaryRegion: 'right',
                            outputRegion: 'bottom',
                            regions: [
                                { id: 'settings_panel', role: 'parameters', placement: 'right' },
                                { id: 'preview_panel', role: 'preview', placement: 'center' },
                                { id: 'output_panel', role: 'output', placement: 'bottom' },
                            ],
                            studio: { editable: true, savedInManifest: true, updatedBy: 'app_studio' },
                        },
                    },
                },
            },
            versionSnapshot: {
                app_entry_version: '4',
                app_skill: { id: 'cold-layout-skill', version: '1.2.0', kind: 'app_skill', source: 'hub' },
            },
            installEvidence: {
                workspace_layout: { entry: 'tool_workspace', template: 'dashboard', density: 'compact' },
                result_contract: { primary: 'content', types: ['content'] },
                test_evidence: { runId: 'run-cold-start', testProtocolFingerprint: 'proto-cold-start' },
                dependency_verification: {
                    schema: 'maclaw.app.install_plan.v1',
                    apps: [{ id: 'cold-start-layout-app', name: 'Cold Start Console', kind: 'tool_app' }],
                    dependencies: [{ id: 'cold-layout-skill', version: '1.2.0', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'skip', app_ids: ['cold-start-layout-app'] }],
                    has_missing_required: false,
                    has_blocking_dependency: false,
                },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));

        render(<AppsPage lang="en" />);

        await waitFor(() => expect(screen.getAllByText('Cold Start Console').length).toBeGreaterThan(0));
        fireEvent.click(screen.getAllByText('Cold Start Console')[0]);

        const runtimeGovernance = await findRuntimeGovernancePanel();
        expect(within(runtimeGovernance).getByText('Dependency verification')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Skill dependencies: 1 · Blocking deps: 0')).not.toBeNull();
        expect(screen.getAllByText('cold-layout-skill').length).toBeGreaterThan(0);
        expect(within(runtimeGovernance).getByText('tool_workspace · dashboard · compact')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('run-cold-start · proto-cold-start')).not.toBeNull();
    });

    it('shows recent app install records in the market pane', async () => {
        const installedPackage = {
            schema: 'maclaw.app.v1',
            app: {
                id: 'contract-audit',
                name: 'Contract Audit',
                kind: 'tool_app',
                binding: { skill: { id: 'contract-super-app', appDefinitionFile: 'maclaw.apps.json' } },
                dependencies: { skills: [{ id: 'policy-skill', version: '2.0.0', kind: 'workflow_skill', source: 'hub', required: true }] },
            },
        };
        const blockedInstallPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'contract-audit', name: 'Contract Audit', kind: 'tool_app' }],
            dependencies: [{ id: 'policy-skill', version: '2.0.0', kind: 'workflow_skill', source: 'hub', required: true, installed: false, health: 'missing', action: 'blocked', app_ids: ['contract-audit'] }],
            has_missing_required: true,
            has_blocking_dependency: true,
        };
        const repairedInstallPlan = {
            ...blockedInstallPlan,
            dependencies: [{ id: 'policy-skill', version: '2.0.0', kind: 'workflow_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed', app_ids: ['contract-audit'] }],
            has_missing_required: false,
            has_blocking_dependency: false,
        };
        planMaclawAppInstallMock.mockResolvedValueOnce(blockedInstallPlan);
        installMaclawAppDependenciesMock.mockResolvedValueOnce(repairedInstallPlan);
        listMaclawAppInstallsMock.mockResolvedValue([
            {
                schema: 'maclaw.app.install_record.v1',
                package_sha256: 'abcdef1234567890',
                source: 'market',
                installed_at: '2026-06-19T08:30:00.000Z',
                app_count: 1,
                apps: [{ id: 'contract-audit', name: 'Contract Audit', kind: 'tool_app' }],
                dependencies: [
                    { id: 'contract-skill', version: '1.0.0', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'skip' },
                    { id: 'policy-skill', version: '2.0.0', kind: 'workflow_skill', source: 'hub', required: true, installed: false, health: 'missing', action: 'blocked' },
                ],
                version_snapshot: {
                    app_entry_version: '4',
                    app_skill: { id: 'contract-super-app', version: '1.1.0', kind: 'app_skill', source: 'hub' },
                    workflow_skills: [{ id: 'policy-skill', version: '2.0.0', kind: 'workflow_skill', source: 'hub' }],
                    approval_bindings: [{ event: 'contract.submitted', object_role: 'contract', workflow_skill_id: 'policy-skill', workflow_version: '2.0.0' }],
                },
                workspace_layout: { entry: 'tool_workspace', template: 'document_workspace', density: 'compact' },
                result_contract: { schema: 'maclaw.app.result.v1', primary: 'content', types: ['content'] },
                test_evidence: {
                    runId: 'run-contract-audit',
                    testProtocolFingerprint: 'proto-contract',
                    testProtocol: { schema: 'maclaw.app.test_protocol.v1', fingerprint: 'proto-contract' },
                },
                dependency_verification: {
                    schema: 'maclaw.app.install_plan.v1',
                    dependency_count: 2,
                    has_missing_required: true,
                    has_blocking_dependency: true,
                    dependencies: [
                        { id: 'contract-skill', version: '1.0.0', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'skip' },
                        { id: 'policy-skill', version: '2.0.0', kind: 'workflow_skill', source: 'hub', required: true, installed: false, health: 'missing', action: 'blocked' },
                    ],
                },
                has_missing_required: true,
                datasrv_registration: {
                    synced: false,
                    eligible_count: 2,
                    synced_count: 1,
                    failed_count: 1,
                    reason: 'policy role binding pending',
                    items: [{ app_id: 'contract-audit', synced: false, reason: 'policy role binding pending', role_binding_count: 1 }],
                },
                package: installedPackage,
            },
        ]);

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());

        await waitFor(() => expect(screen.getByText('Contract Audit')).not.toBeNull());
        expect(screen.getByText(/Package SHA: abcdef123456/)).not.toBeNull();
        expect(screen.getAllByText(/Skill dependencies: 2/).length).toBeGreaterThanOrEqual(1);
        expect(screen.getAllByText(/Blocking deps: 1/).length).toBeGreaterThanOrEqual(1);
        const dependencyList = document.querySelector('.apps-install-record__deps') as HTMLElement;
        expect(dependencyList).not.toBeNull();
        expect(dependencyList.getAttribute('aria-label')).toBe('Skill dependencies');
        const readyDependency = Array.from(dependencyList.querySelectorAll('.apps-install-record__dep'))
            .find((item) => item.textContent?.includes('contract-skill')) as HTMLElement;
        const blockedDependency = Array.from(dependencyList.querySelectorAll('.apps-install-record__dep'))
            .find((item) => item.textContent?.includes('policy-skill')) as HTMLElement;
        expect(readyDependency?.dataset.state).toBe('ready');
        expect(blockedDependency?.dataset.state).toBe('blocked');
        expect(within(readyDependency).getByText('Installed')).not.toBeNull();
        expect(within(blockedDependency).getByText('Missing')).not.toBeNull();
        expect(readyDependency.textContent).toContain('runtime_skill · hub · v1.0.0');
        expect(blockedDependency.textContent).toContain('workflow_skill · hub · v2.0.0');
        const versionSnapshot = document.querySelector('.apps-install-version-snapshot') as HTMLElement;
        expect(versionSnapshot).not.toBeNull();
        expect(versionSnapshot.getAttribute('aria-label')).toBe('Version snapshot');
        expect(within(versionSnapshot).getByText('Version')).not.toBeNull();
        expect(within(versionSnapshot).getByText('v4')).not.toBeNull();
        expect(within(versionSnapshot).getByText(miniAppLabels.skillField.en)).not.toBeNull();
        expect(within(versionSnapshot).getByText('contract-super-app · app_skill · hub · v1.1.0')).not.toBeNull();
        expect(within(versionSnapshot).getByText('Approval workflow')).not.toBeNull();
        expect(within(versionSnapshot).getByText('policy-skill · workflow_skill · hub · v2.0.0')).not.toBeNull();
        expect(within(versionSnapshot).getByText('Approval binding')).not.toBeNull();
        expect(within(versionSnapshot).getByText('contract.submitted · contract · policy-skill@v2.0.0')).not.toBeNull();
        const evidenceSnapshot = document.querySelector('.apps-install-evidence-snapshot') as HTMLElement;
        expect(evidenceSnapshot).not.toBeNull();
        expect(evidenceSnapshot.getAttribute('aria-label')).toBe('Test evidence');
        expect(within(evidenceSnapshot).getByText('Workspace layout')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('tool_workspace · document_workspace · compact')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('Result contract')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('content · 1 types')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('Test evidence')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('run-contract-audit · proto-contract')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('Dependency verification')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('Skill dependencies: 2 · Blocking deps: 1')).not.toBeNull();
        const dataSrvEvidence = within(evidenceSnapshot).getByText(/DataSrv bindings partially registered/).closest('[role="listitem"]') as HTMLElement;
        expect(dataSrvEvidence).not.toBeNull();
        expect(dataSrvEvidence.dataset.state).toBe('partial');
        expect(dataSrvEvidence.textContent).toContain('1/2');
        expect(dataSrvEvidence.textContent).toContain('policy role binding pending');
        getMISDataConfigMock.mockResolvedValue({ enabled: true, endpoint: 'http://datasrv.test', token: 'token' });
        const fetchMock = vi.fn().mockResolvedValue({
            ok: true,
            json: async () => ({
                items: [{
                    app_id: 'contract-audit',
                    metadata: {
                        datasrv_registration: {
                            synced: false,
                            eligible_count: 2,
                            synced_count: 1,
                            failed_count: 1,
                        },
                    },
                }],
            }),
        });
        vi.stubGlobal('fetch', fetchMock);
        fireEvent.click(screen.getByText('Audit DataSrv'));
        await waitFor(() => expect(fetchMock).toHaveBeenCalled());
        expect(fetchMock.mock.calls.some(([url]) => String(url).includes('/api/v1/data/app-installations') && String(url).includes('app_id=contract-audit'))).toBe(true);
        await waitFor(() => expect(screen.getByText(/DataSrv app installations: 1/)).not.toBeNull());
        expect(screen.getByText('DataSrv app installations: 1 · DataSrv bindings registered: 0 · DataSrv bindings partially registered: 1 · DataSrv binding registration failed: 0')).not.toBeNull();
        expect(listMaclawAppInstallsMock).toHaveBeenCalledWith(6);

        fireEvent.click(screen.getByText('Check dependencies'));
        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalledWith(JSON.stringify(installedPackage)));
        expect(screen.getByText('Repair dependencies')).not.toBeNull();
        expect(document.body.textContent).toContain('policy-skill');

        fireEvent.click(screen.getByText('Repair dependencies'));
        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalledWith(JSON.stringify(installedPackage)));
        await waitFor(() => expect(document.body.textContent).toContain('Blocking deps: 0'));
        expect(document.body.textContent).toContain('policy-skill');
        expect(document.body.textContent).toContain('Installed');
    });
    it('blocks market install when a required dependency has a version mismatch', async () => {
        const blockedPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'inactive-market-app', name: 'Inactive Market App', kind: 'tool_app' }],
            dependencies: [{
                id: 'disabled-workflow',
                version: '2.1.0',
                kind: 'runtime_skill',
                required: true,
                installed: true,
                installed_version: '1.0.0',
                required_version: '2.1.0',
                version_status: 'mismatch',
                installed_status: 'active',
                health: 'version_mismatch',
                action: 'blocked',
                app_ids: ['inactive-market-app'],
                message: 'required skill dependency version 1.0.0 is installed, but 2.1.0 is required',
            }],
            has_missing_required: false,
            has_blocking_dependency: true,
        };
        planMaclawAppInstallMock.mockResolvedValue(blockedPlan);
        installMaclawAppDependenciesMock.mockResolvedValue(blockedPlan);

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'inactive-market-app',
                        name: 'Dependency Version App',
                        description: 'Dependency is disabled',
                        category: 'Documents',
                        kind: 'tool_app',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'disabled-workflow', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                    },
                }),
            },
        });

        await waitFor(() => expect(screen.getByText(/Required Skill dependencies are missing or unavailable/)).not.toBeNull());
        expect(screen.getAllByText(/disabled-workflow/).length).toBeGreaterThan(0);
        expect(screen.getAllByText(/1\.0\.0 -> 2\.1\.0/).length).toBeGreaterThan(0);
        expect(document.querySelector('.apps-install-preview__row[data-dependency-state="blocked"]')).not.toBeNull();

        fireEvent.click(screen.getByText('Install'));

        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalledTimes(1));
        expect(screen.getAllByText(/1\.0\.0 -> 2\.1\.0/).length).toBeGreaterThan(0);
        expect(screen.queryByText('Installed 1 · skipped 0')).toBeNull();
    });
    it('blocks pasted approval app installation when workflow contract verification fails', async () => {
        const contractIssuePlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'contract-drift-install', name: 'Contract Drift Install', kind: 'enterprise_approval_app' }],
            dependencies: [{ id: 'expense-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, installed: true, health: 'ready', action: 'skip', app_ids: ['contract-drift-install'] }],
            workflow_contract_issues: [{ path: 'apps[0].app.governance.workflowContract.workflowSkillId', severity: 'error', message: 'approval workflow contract does not match approval binding', metadata: { workflow_skill_id: 'expense-workflow', installed_version: '1.0.0', required_version: '2.1.0', binding_event: 'expense.submitted', object_role: 'expense_report', health: 'ready' } }],
            has_workflow_contract_issue: true,
            has_missing_required: false,
            has_blocking_dependency: false,
        };
        planMaclawAppInstallMock.mockResolvedValue(contractIssuePlan);
        installMaclawAppDependenciesMock.mockResolvedValue(contractIssuePlan);

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(document.querySelector('.apps-market-install textarea') as HTMLTextAreaElement, {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'enterprise_app_pack',
                    app: {
                        id: 'contract-drift-install',
                        name: 'Contract Drift Install',
                        description: 'Approval workflow contract mismatch',
                        category: 'Finance',
                        kind: 'enterprise_approval_app',
                        icon: 'receipt',
                        launchMode: 'agent_dynamic_ui',
                        binding: {
                            datasrv: { domain: 'finance', datasetID: 'finance.expenses', objectRole: 'expense_report', preferredAction: 'finance.expense_upsert' },
                            workflow: { schema: 'maclaw.app.workflow.v1', submitNode: 'expense_report.submit', approvalNode: 'expense_report.manager_approval', resultNode: 'expense_report.result_feedback', attentionNode: 'expense_report.attention_review', statusMapping: { pending: 'approval_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' } },
                            mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'expense-workflow', workflowVersion: '1.0.0', objectRole: 'expense_report' }] },
                            dependencies: { skills: [{ id: 'expense-workflow', version: '1.0.0', kind: 'workflow_skill', required: true, source: 'hub', capabilities: ['approval.workflow'] }] },
                        },
                    },
                }),
            },
        });

        await waitFor(() => expect(screen.getAllByText(/Runtime contract needs attention/).length).toBeGreaterThan(0));
        expect(screen.getAllByText(/approval workflow contract does not match approval binding/).length).toBeGreaterThan(0);
        const verification = document.querySelector('.apps-dependency-verification') as HTMLElement;
        expect(verification).not.toBeNull();
        expect(verification.dataset.state).toBe('blocked');
        expect(within(verification).getByText(/Runtime contract: 1/)).not.toBeNull();
        expect(within(verification).getAllByText('expense-workflow').length).toBeGreaterThan(0);
        expect(within(verification).getByText('v1.0.0 -> v2.1.0')).not.toBeNull();
        expect(within(verification).getByText('expense.submitted')).not.toBeNull();
        expect(within(verification).getByText('expense_report')).not.toBeNull();
        expect(document.querySelector('.apps-install-preview__row[data-dependency-state="blocked"]')).not.toBeNull();

        const installButton = document.querySelector('.apps-market-install .apps-primary-button') as HTMLButtonElement;
        expect(installButton.disabled).toBe(true);
        fireEvent.click(installButton);

        expect(installMaclawAppDependenciesMock).not.toHaveBeenCalled();
        expect(recordMaclawAppInstallMock).not.toHaveBeenCalled();
        expect(screen.queryByText('Installed: 1 · Skipped: 0')).toBeNull();
        fireEvent.click(getManageTab());
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('Contract Drift Install'))).toBe(false);
    });
    it('blocks pasted app installation when governance review fails', async () => {
        const governanceIssuePlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'bad-layout-install', name: 'Bad Layout Install', kind: 'tool_app' }],
            dependencies: [],
            governance_review_issues: [{
                path: 'apps[0].app.governance.workspaceLayout',
                severity: 'error',
                message: 'missing workspace layout evidence',
            }],
            has_governance_review_issue: true,
            has_missing_required: false,
            has_blocking_dependency: false,
        };
        planMaclawAppInstallMock.mockResolvedValue(governanceIssuePlan);
        installMaclawAppDependenciesMock.mockResolvedValue(governanceIssuePlan);

        render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(document.querySelector('.apps-market-install textarea') as HTMLTextAreaElement, {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'bad-layout-install',
                        name: 'Bad Layout Install',
                        description: 'Saved layout misses required output region',
                        category: 'Tools',
                        kind: 'tool_app',
                        icon: 'sheet',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'bad-layout-skill', appDefinitionFile: 'maclaw.app.json', inputMode: 'form' } },
                        governance: {
                            workspaceLayout: { schema: 'maclaw.app.ui.v1', entry: 'tool_workspace', template: 'document_workspace', regionCount: 1, regions: [{ id: 'file_queue', role: 'input', placement: 'left' }] },
                            resultContract: { schema: 'maclaw.app.result.v1', primary: 'content', types: ['content'] },
                            testEvidence: { testProtocol: { schema: 'maclaw.app.test_protocol.v1', fingerprint: 'proto-layout', sampleInput: { sample: true }, expectedOutput: { content: 'ok' }, requiredRoles: ['tester'], requiredScopes: ['app.run'], riskLevel: 'low' }, testProtocolFingerprint: 'proto-layout', runId: 'run-layout', verifiedAt: '2026-06-17T01:00:00Z', resultPayload: { content: 'ok' } },
                        },
                    },
                }),
            },
        });

        await waitFor(() => expect(screen.getAllByText(/Review issues/).length).toBeGreaterThan(0));
        expect(screen.getAllByText(/missing workspace layout evidence/).length).toBeGreaterThan(0);
        const verification = document.querySelector('.apps-dependency-verification') as HTMLElement;
        expect(verification).not.toBeNull();
        expect(verification.dataset.state).toBe('blocked');
        expect(within(verification).getByText(/Review issues: 1/)).not.toBeNull();
        expect(document.querySelector('.apps-install-preview__row[data-dependency-state="blocked"]')).not.toBeNull();

        const installButton = document.querySelector('.apps-market-install .apps-primary-button') as HTMLButtonElement;
        expect(installButton.disabled).toBe(true);
        fireEvent.click(installButton);

        expect(installMaclawAppDependenciesMock).not.toHaveBeenCalled();
        expect(recordMaclawAppInstallMock).not.toHaveBeenCalled();
        expect(screen.queryByText('Installed: 1 · Skipped: 0')).toBeNull();
        fireEvent.click(getManageTab());
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('Bad Layout Install'))).toBe(false);
    });
    it('supports select all and select none in market install preview', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'select-a', skill_id: 'doc-tools', name: '选择 A', description: 'Select A', category: '文档处理', icon: 'contract', input_mode: 'file' },
                        { id: 'select-b', skill_id: 'doc-tools', name: '选择 B', description: 'Select B', category: '文档处理', icon: 'sheet', input_mode: 'file' },
                    ],
                }),
            },
        });

        expect(screen.getByText('2/2')).not.toBeNull();
        expect(document.body.textContent).toMatch(/可安装\s*2 · 可升级\s*0 · 将跳过\s*0/);
        fireEvent.click(screen.getByText('全不选'));
        expect(screen.getByText('0/2')).not.toBeNull();
        expect(document.body.textContent).toMatch(/可安装\s*2 · 可升级\s*0 · 将跳过\s*2/);
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(screen.getByText('全选'));
        expect(screen.getByText('2/2')).not.toBeNull();
        expect(document.body.textContent).toMatch(/可安装\s*2 · 可升级\s*0 · 将跳过\s*0/);
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(false);
    });

    it('preserves custom uploaded-style app icons from installed manifests', async () => {
        const customIconDataUrl = 'data:image/png;base64,iVBORw0KGgo=';
        render(<AppsPage lang="en" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[2] as HTMLButtonElement);
        fireEvent.change(document.querySelector('.apps-market-install textarea') as HTMLTextAreaElement, {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'custom-icon-tool',
                        name: 'Custom Icon Tool',
                        description: 'Uses a custom app icon',
                        category: 'Tools',
                        kind: 'tool_app',
                        icon: 'bot',
                        customIconDataUrl,
                        launchMode: 'fixed_skill_ui',
                        panel: { accent: '#2f5f98', customIconDataUrl },
                        binding: { skill: { id: 'custom-icon-tool', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                    },
                }),
            },
        });
        fireEvent.click(screen.getByText('Install'));

        await waitFor(() => expect(document.querySelector(`img.apps-custom-app-icon[src="${customIconDataUrl}"]`)).not.toBeNull());
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);
        const row = await waitFor(() => {
            const found = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('Custom Icon Tool')) as HTMLElement | undefined;
            expect(found).toBeTruthy();
            return found as HTMLElement;
        });
        fireEvent.click(within(row).getByTitle(manageManifestTitle));
        expect(document.querySelector('.apps-manage-manifest')?.textContent).toContain(customIconDataUrl);
    });

    it('uses installed skill app output modes in the runtime UI', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'sheet-clean',
                            skill_id: 'sheet-tools',
                            name: '表格清洗',
                            description: 'Clean sheets',
                            category: '数据分析',
                            icon: 'sheet',
                            input_mode: 'file',
                            output_modes: ['xlsx'],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('表格清洗')[0]);

        const outputSelect = container.querySelector('.apps-form-row select') as HTMLSelectElement;
        expect(Array.from(outputSelect.options).map((option) => option.value)).toEqual(['xlsx']);
    });

    it('restores installed tool app workspace layout and test evidence in the runtime UI', async () => {
        const { container } = render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(document.querySelector('.apps-market-install textarea') as HTMLTextAreaElement, {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'tool-layout-install',
                        name: 'Tool Layout Install',
                        description: 'Restores the Studio layout after market install',
                        category: 'Tools',
                        kind: 'tool_app',
                        icon: 'pdf',
                        launchMode: 'fixed_skill_ui',
                        binding: {
                            skill: { id: 'tool-layout-skill', appDefinitionFile: 'maclaw.app.json', inputMode: 'file', outputModes: ['pdf'] },
                            ui: {
                                schema: 'maclaw.app.ui.v1',
                                generated: true,
                                entry: 'tool_workspace',
                                layouts: {
                                    tool_workspace: {
                                        type: 'tool_workspace',
                                        template: 'dashboard',
                                        density: 'compact',
                                        primaryRegion: 'center',
                                        outputRegion: 'bottom',
                                        regions: [
                                            { id: 'file_queue', role: 'input', placement: 'center', order: 1 },
                                            { id: 'settings_panel', role: 'parameters', placement: 'left', visible: false, order: 2 },
                                            { id: 'output_panel', role: 'output', placement: 'bottom', order: 3 },
                                            { id: 'preview_panel', role: 'preview', placement: 'right', order: 4 },
                                        ],
                                        fingerprint: 'layout123',
                                        studio: { editable: true, savedInManifest: true, updatedBy: 'app_studio' },
                                    },
                                },
                            },
                            resultContract: { schema: 'maclaw.app.result.v1', primary: 'artifact', types: ['artifact', 'document'], outputModes: ['pdf'], delivery: { inlineContent: false, artifacts: true, businessRecord: false, notifications: false } },
                            testProtocol: { schema: 'maclaw.app.test_protocol.v1', fingerprint: 'proto-tool-layout', sampleInput: { file: 'sample.pdf' }, expectedOutput: { primary: 'artifact' }, requiredRoles: [], requiredScopes: [], riskLevel: 'low' },
                        },
                        governance: {
                            workspaceLayout: { entry: 'tool_workspace', template: 'dashboard', density: 'compact', primaryRegion: 'center', outputRegion: 'bottom', fingerprint: 'layout123', savedInManifest: true },
                            resultContract: { schema: 'maclaw.app.result.v1', primary: 'artifact', types: ['artifact', 'document'], outputModes: ['pdf'] },
                            testEvidence: {
                                runId: 'run-tool-layout-install',
                                definitionHash: 'hash-tool-layout-install',
                                verifiedAt: '2026-07-01T02:00:00Z',
                                artifactPresent: true,
                                artifactCount: 1,
                                artifactName: 'translated-output.pdf',
                                artifacts: [{ id: 'artifact-tool-layout', uri: 'artifact://tool-layout/output.pdf', name: 'translated-output.pdf', status: 'ready' }],
                                outputs: [{ kind: 'artifact', title: 'Translated PDF', artifact_id: 'artifact-tool-layout', status: 'ready' }],
                                resultPayload: { status: 'ok', artifact_id: 'artifact-tool-layout' },
                                resultCoverage: { ok: true, primary: 'artifact', coveredTypes: ['artifact', 'document'], missingTypes: [] },
                                testProtocol: { schema: 'maclaw.app.test_protocol.v1', fingerprint: 'proto-tool-layout', sampleInput: { file: 'sample.pdf' }, expectedOutput: { primary: 'artifact' }, requiredRoles: [], requiredScopes: [], riskLevel: 'low' },
                                testProtocolFingerprint: 'proto-tool-layout',
                            },
                        },
                    },
                }),
            },
        });
        fireEvent.click(screen.getByText('Install'));

        await waitFor(() => expect(screen.getByText('Installed: 1 · Skipped: 0')).not.toBeNull());
        fireEvent.click(screen.getAllByText('Close')[0]);
        fireEvent.click(screen.getAllByText('Tool Layout Install')[0]);

        const runtimeLayout = await waitFor(() => {
            const layout = container.querySelector('.apps-runtime-layout') as HTMLElement | null;
            expect(layout).not.toBeNull();
            return layout as HTMLElement;
        });
        expect(runtimeLayout.dataset.template).toBe('dashboard');
        expect(runtimeLayout.dataset.density).toBe('compact');
        expect(runtimeLayout.dataset.primaryRegion).toBe('center');
        expect(runtimeLayout.dataset.outputRegion).toBe('bottom');
        expect(runtimeLayout.dataset.regionCount).toBe('4');
        expect((container.querySelector('.apps-runtime-input') as HTMLElement).dataset.region).toBe('center');
        expect((container.querySelector('.apps-runtime-status') as HTMLElement).dataset.region).toBe('right');
        expect((container.querySelector('.apps-runtime-output') as HTMLElement).dataset.region).toBe('bottom');
        expect((container.querySelector('.apps-run-history') as HTMLElement).dataset.region).toBe('bottom');
        expect(container.querySelector('.apps-runtime-status')?.textContent).toContain('Waiting to run');
        expect(container.querySelector('.apps-run-history')?.textContent).toContain('run-tool-layout-install');
        expect(container.querySelector('.apps-run-history')?.textContent).toContain('translated-output.pdf');

        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        const restored = [...(stored.customApps || []), ...(stored.editedApps || [])].find((app: any) => app.id === 'market-tool-layout-install');
        expect(restored.importedRunEvidence).toMatchObject({
            runID: 'run-tool-layout-install',
            definitionHash: 'hash-tool-layout-install',
            artifactName: 'translated-output.pdf',
        });
        expect(restored.manifest.ui.layouts.tool_workspace).toMatchObject({ template: 'dashboard', density: 'compact', primaryRegion: 'center', outputRegion: 'bottom' });
    });

    it('auto-registers installed MaClaw App skills in the app panel', async () => {
        listSkillAppManifestsMock.mockResolvedValue([
            {
                id: 'invoice-review',
                skill_id: 'invoice-app',
                name: '发票审核',
                description: 'Review invoice files',
                category: '财务',
                icon: 'invoice',
                input_mode: 'file',
                output_modes: ['pdf'],
                app_definition_file: 'maclaw.app.json',
            },
        ]);
        const { container } = render(<AppsPage lang="zh-Hans" />);

        await waitFor(() => expect(screen.getAllByText('发票审核').length).toBeGreaterThan(0));
        fireEvent.click(screen.getAllByText('发票审核')[0]);

        expect(container.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('发票审核');
        const outputSelect = container.querySelector('.apps-form-row select') as HTMLSelectElement;
        expect(Array.from(outputSelect.options).map((option) => option.value)).toEqual(['json', 'pdf']);
    });

    it('restores enterprise MaClaw App skill definitions without downgrading them to tool apps', async () => {
        listSkillAppManifestsMock.mockResolvedValue([
            {
                id: 'expense-approval',
                skill_id: 'expense-super-skill',
                name: '费用审批',
                description: 'Submit and approve expenses',
                category: '财务',
                kind: 'enterprise_approval_app',
                app_definition_file: 'maclaw.app.json',
                app_definition: {
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'enterprise_app_pack',
                    app: {
                        id: 'expense-approval',
                        name: '费用审批',
                        description: 'Submit and approve expenses',
                        kind: 'enterprise_approval_app',
                        icon: 'receipt',
                        binding: {
                            appSkill: { id: 'expense-super-skill', version: '1.0.0', source: 'local' },
                            dependencies: { skills: [{ id: 'expense-workflow', kind: 'workflow_skill', version: '2.0.0', required: true, source: 'hub' }] },
                            ui: {
                                schema: 'maclaw.app.ui.v1',
                                entry: 'approval_workspace',
                                layouts: {
                                    approval_workspace: {
                                        template: 'classic_split',
                                        density: 'compact',
                                        primaryRegion: 'center',
                                        outputRegion: 'bottom',
                                        regions: [
                                            { id: 'request_form', role: 'input', placement: 'center' },
                                            { id: 'approval_inbox', role: 'instance_list', placement: 'left' },
                                            { id: 'result_panel', role: 'output', placement: 'bottom' },
                                        ],
                                    },
                                },
                            },
                            mis: { approvalBindings: [{ event: 'expense.submitted', workflowSkillId: 'expense-workflow', workflowVersion: '2.0.0', objectRole: 'expense_report' }] },
                        },
                        governance: {
                            resultContract: { schema: 'maclaw.app.result.v1', primary: 'approval_result', types: ['approval_result', 'business_status'] },
                            workflowContract: { schema: 'maclaw.app.workflow_contract.v1', workflowSkillId: 'expense-workflow', workflowVersion: '2.0.0', objectRole: 'expense_report' },
                            testEvidence: {
                                runId: 'run-expense-1',
                                definitionHash: 'hash-expense-1',
                                resultPayload: { approval_result: 'approved', business_status: 'finance_approved' },
                                approvalInstance: {
                                    instanceId: 'wf-expense-1',
                                    approvalID: 'approval-expense-1',
                                    recordID: 'expense-1',
                                    status: 'approved',
                                    currentNode: 'expense.result',
                                    workflowSkillId: 'expense-workflow',
                                    businessStatus: 'finance_approved',
                                    resultStatus: 'approved',
                                    resultPayload: { approval_result: 'approved' },
                                    outputs: [{ kind: 'approval_result', text: 'approved', status: 'ready' }],
                                },
                            },
                        },
                    },
                },
            },
        ]);
        render(<AppsPage lang="zh-Hans" />);

        await waitFor(() => expect(screen.getAllByText('费用审批').length).toBeGreaterThan(0));

        // The enterprise definition survives discovery without a tool_app
        // downgrade (skill apps are rediscovered, not cached in panel state).
        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        let row: HTMLElement | undefined;
        await waitFor(() => {
            const item = Array.from(document.querySelectorAll('.apps-manage-item')).find((entry) => entry.textContent?.includes('费用审批')) as HTMLElement | undefined;
            expect(item).toBeTruthy();
            row = item?.querySelector('.apps-manage-row') as HTMLElement | undefined;
        });
        fireEvent.click(within(row as HTMLElement).getByTitle(manageManifestTitle));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(manifest.app.kind).toBe('enterprise_approval_app');
        expect(manifest.app.appSkill).toEqual(expect.objectContaining({ id: 'expense-super-skill', source: 'local' }));
        expect(manifest.app.dependencies.skills[0]).toEqual(expect.objectContaining({ id: 'expense-workflow', kind: 'workflow_skill', version: '2.0.0' }));
        expect(manifest.app.mis.approvalBindings[0]).toEqual(expect.objectContaining({ workflowSkillId: 'expense-workflow', workflowVersion: '2.0.0', objectRole: 'expense_report' }));
        expect(manifest.app.ui.layouts.approval_workspace.primaryRegion).toBe('center');
        expect(manifest.app.ui.layouts.approval_workspace.outputRegion).toBe('bottom');
        expect(manifest.app.ui.layouts.approval_workspace.regions).toHaveLength(3);

        // Imported run evidence is restored into the runtime workspace.
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('费用审批')[0]);
        await waitFor(() => expect(screen.getAllByText('全部').length).toBeGreaterThan(0));
        fireEvent.click(screen.getByText('全部'));
        await waitFor(() => expect(screen.getAllByText('expense-1').length).toBeGreaterThan(0));
        fireEvent.click(screen.getAllByText('expense-1')[0]);
        await waitFor(() => expect(screen.getAllByText(/approval-expense-1/).length).toBeGreaterThan(0));
        expect(screen.getAllByText(/expense-workflow/).length).toBeGreaterThan(0);
        expect(screen.getAllByText(/expense\.result/).length).toBeGreaterThan(0);
        expect(screen.getAllByText(/finance_approved/).length).toBeGreaterThan(0);
        expect(screen.getAllByText(/wf-expense-1/).length).toBeGreaterThan(0);
    });

    it('installs apps from a pasted app pack manifest', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.pack.v1',
                    privateMarker: 'x_maclaw_apps',
                    apps: [
                        {
                            schema: 'maclaw.app.v1',
                            privateMarker: 'x_maclaw_apps',
                            installUnit: 'skill',
                            app: {
                                id: 'pack-doc-check',
                                name: '文档校验',
                                description: 'Check documents',
                                category: '文档处理',
                                kind: 'tool_app',
                                icon: 'shield',
                                launchMode: 'fixed_skill_ui',
                                binding: { skill: { id: 'doc-check', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                            },
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));

        await waitFor(() => expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull());
        expect(screen.getAllByText('文档校验').length).toBeGreaterThan(0);
    });

    it('skips duplicate apps when installing pasted manifests', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        const manifest = JSON.stringify({
            x_maclaw_apps: 'v1',
            apps: [
                { id: 'doc-stamp', skill_id: 'doc-tools', name: '文档盖章', description: 'Stamp documents', category: '文档处理', icon: 'contract', input_mode: 'file' },
                { id: 'doc-stamp', skill_id: 'doc-tools', name: '文档盖章', description: 'Stamp documents', category: '文档处理', icon: 'contract', input_mode: 'file' },
            ],
        });
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), { target: { value: manifest } });
        fireEvent.click(screen.getByText('安装'));

        expect(screen.getByText('已安装: 1 · 已跳过: 1')).not.toBeNull();

        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), { target: { value: manifest } });

        expect(screen.getByText('0/2')).not.toBeNull();
        expect(screen.getAllByText('将跳过 · 已安装').length).toBeGreaterThan(0);
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);
    });

    it('treats market-prefixed and built-in app ids as the same install identity', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: {
                        id: 'pdf-to-word',
                        name: 'PDF 转 Word',
                        description: 'Duplicate built-in app',
                        category: '文档处理',
                        kind: 'tool_app',
                        icon: 'pdf',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'pdf-to-word', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                    },
                }),
            },
        });

        expect(screen.getByText('0/1')).not.toBeNull();
        expect(screen.getByText('将跳过 · 已安装')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);
    });

    it('shows detailed errors for invalid pasted manifests', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        expect(screen.getByLabelText('安装应用包')).not.toBeNull();
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), { target: { value: '{bad json' } });

        expect(screen.getByText('应用包无效: JSON 解析失败')).not.toBeNull();
        expect(screen.getByText('应用包无效: JSON 解析失败').closest('[role="alert"]')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), { target: { value: JSON.stringify({ apps: [] }) } });

        expect(screen.getByText('应用包无效: 未识别应用包格式')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'wrong',
                    app: { id: 'bad', name: 'Bad' },
                }),
            },
        });

        expect(screen.getByText('应用包无效: maclaw.app.v1 privateMarker must be x_maclaw_apps')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: { id: '-bad', name: 'Bad', launchMode: 'fixed_skill_ui' },
                }),
            },
        });

        expect(screen.getByText('应用包无效: maclaw.app.v1 app.id is invalid')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: { id: 'bad-launch', name: 'Bad Launch', kind: 'enterprise_normal_app', launchMode: 'fixed_skill_ui' },
                }),
            },
        });

        expect(screen.getByText('应用包无效: maclaw.app.v1 app.launchMode must be agent_dynamic_ui for enterprise_normal_app')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: { id: 'bad-automation-launch', name: 'Bad Automation Launch', kind: 'automation_app', launchMode: 'agent_dynamic_ui' },
                }),
            },
        });

        expect(screen.getByText('应用包无效: maclaw.app.v1 app.launchMode must be automation_console for automation_app')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'enterprise_app_pack',
                    app: { id: 'missing-datasrv', name: 'Missing DataSrv', kind: 'enterprise_normal_app', launchMode: 'agent_dynamic_ui' },
                }),
            },
        });

        expect(screen.getByText('应用包无效: maclaw.app.v1 binding.datasrv is required for enterprise_normal_app')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: { id: 'missing-skill', name: 'Missing Skill', kind: 'tool_app', launchMode: 'fixed_skill_ui' },
                }),
            },
        });

        expect(screen.getByText('应用包无效: maclaw.app.v1 binding.skill is required for tool_app')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'builtin',
                    app: {
                        id: 'wrong-install-unit',
                        name: 'Wrong Install Unit',
                        kind: 'tool_app',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'wrong-install-unit', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                    },
                }),
            },
        });

        expect(screen.getByText('应用包无效: maclaw.app.v1 installUnit must be skill for tool_app')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: {
                        id: 'bad-output',
                        name: 'Bad Output',
                        binding: { skill: { id: 'bad-output', appDefinitionFile: 'maclaw.apps.json', outputModes: ['exe'] } },
                    },
                }),
            },
        });

        expect(screen.getByText('应用包无效: maclaw.app.v1 binding.skill.outputModes[0] is invalid')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        {
                            id: 'bad-field',
                            skill_id: 'bad-field',
                            name: 'Bad Field',
                            input_mode: 'form',
                            output_modes: ['json'],
                            fields: [{ name: 'mode', type: 'number' }],
                        },
                    ],
                }),
            },
        });

        expect(screen.getByText('应用包无效: maclaw.apps.json apps[0].fields[0].type is invalid')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);
    });

    it('shows detailed errors for invalid app pack manifests', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getMarketTab());
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.pack.v1',
                    privateMarker: 'x_maclaw_apps',
                    apps: [{ schema: 'maclaw.app.v1', privateMarker: 'bad' }],
                }),
            },
        });

        expect(screen.getByText('应用包无效: maclaw.app.pack.v1 apps[0] privateMarker must be x_maclaw_apps')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);
    });

    it('removes apps from the panel, closes their runtime tabs, and persists hidden built-ins', async () => {
        const { container, unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByText('数据同步'));
        expect(container.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('数据同步');
        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        const dataSyncRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('\u6570\u636e\u540c\u6b65')) as HTMLElement;
        fireEvent.click(within(dataSyncRow).getByTitle('移除'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));

        expect(container.querySelector('.apps-runtime-tab')?.textContent || '').not.toContain('数据同步');
        expect(screen.queryByText('数据同步')).toBeNull();

        // Removal persists asynchronously (SQLite-gated write).
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            expect((stored.customApps || []).some((app: any) => app.id === 'data-sync')).toBe(false);
        });
        unmount();
        render(<AppsPage lang="zh-Hans" />);

        expect(screen.queryByText('数据同步')).toBeNull();
    });

    it('syncs Hub revoked and republished queue states into installed app availability', async () => {
        const app = {
            id: 'market-revoked-app',
            name: 'Hub Revoked App',
            description: 'Installed from Hub and governed by queue state',
            category: 'Hub',
            kind: 'tool_app',
            icon: 'pdf',
            accent: '#4b6572',
            source: 'market',
            marketCapabilityID: 'cap-hub-revoked-app',
            marketInstallSource: 'enterprise_hub',
            marketSourceLabel: 'Enterprise Hub',
            manifest: {
                schema: 'maclaw.app.v1',
                privateMarker: 'x_maclaw_apps',
                installUnit: 'enterprise_app_pack',
                entryKind: 'tool_app',
                launchMode: 'fixed_skill_ui',
                appSkill: { id: 'hub-revoked-skill', version: '1.0.0', source: 'hub' },
                skill: { id: 'hub-revoked-skill', inputMode: 'form', outputModes: ['text'], fields: [] },
                dependencies: { skills: [{ id: 'hub-revoked-skill', kind: 'runtime_skill', required: true, source: 'hub' }] },
                ui: { schema: 'maclaw.app.ui.v1', entry: 'tool_workspace', layouts: { tool_workspace: { template: 'classic_split', density: 'compact', primaryRegion: 'left', outputRegion: 'right', regions: [{ id: 'input', role: 'input', placement: 'left' }, { id: 'output', role: 'output', placement: 'right' }] } } },
                resultContract: { schema: 'maclaw.app.result.v1', primary: 'content', types: ['content'] },
                testProtocol: { schema: 'maclaw.app.test_protocol.v1', requiredRuns: 1, cases: [{ id: 'smoke', name: 'Smoke', required: true, expectedOutputs: ['content'] }] },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        const listMaclawAppPackageSubmissions = vi.fn()
            .mockResolvedValueOnce([{
                submission_id: 'hub-revoked-state',
                hub_capability_id: 'cap-hub-revoked-app',
                submitted_at: '2026-06-22T01:00:00Z',
                status: 'revoked',
                channel: 'hub',
                app_ids: ['market-revoked-app'],
                app_names: ['Hub Revoked App'],
                message: 'revoked by enterprise market',
            }])
            .mockResolvedValueOnce([{
                submission_id: 'hub-revoked-state',
                hub_capability_id: 'cap-hub-revoked-app',
                submitted_at: '2026-06-22T01:00:00Z',
                status: 'published',
                channel: 'hub',
                app_ids: ['market-revoked-app'],
                app_names: ['Hub Revoked App'],
                message: 'republished by enterprise market',
            }]);
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };
        const { container } = render(<AppsPage lang="en" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));

        const revokedTile = await waitFor(() => {
            const tile = screen.getAllByText('Hub Revoked App')[0].closest('.apps-app-tile') as HTMLButtonElement;
            expect(tile.getAttribute('data-status')).toBe('disabled');
            return tile;
        });
        expect(revokedTile.title).toContain('Revoked by the enterprise capability market');
        fireEvent.click(screen.getAllByText('Close')[0]);
        fireEvent.click(revokedTile);
        expect(container.querySelector('.apps-runtime-tab')?.textContent || '').not.toContain('Hub Revoked App');
        const revokedStored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        expect(revokedStored.disabledSourcesById['market-revoked-app']).toBe('hub_governance');

        fireEvent.click(getStudioButton());
        fireEvent.click(screen.getByText('Review / publish'));
        fireEvent.click(document.querySelector('.apps-publish-queue__head .apps-secondary-button') as HTMLButtonElement);

        const restoredTile = await waitFor(() => {
            const tile = screen.getAllByText('Hub Revoked App')[0].closest('.apps-app-tile') as HTMLButtonElement;
            expect(tile.getAttribute('data-status')).toBe('available');
            return tile;
        });
        fireEvent.click(screen.getAllByText('Close')[0]);
        fireEvent.click(restoredTile);
        expect(document.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('Hub Revoked App');
    });
    it('disables apps without hiding entries, blocks launch, and persists the disabled state', async () => {
        const { container, unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        const dataSyncRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('数据同步')) as HTMLElement;
        fireEvent.click(within(dataSyncRow).getByTitle('停用'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));

        const disabledTile = screen.getAllByText('数据同步')[0].closest('.apps-app-tile') as HTMLButtonElement;
        expect(disabledTile).not.toBeNull();
        expect(disabledTile.getAttribute('data-status')).toBe('disabled');
        expect(disabledTile.title).toContain('企业管理员已停用此应用');
        fireEvent.click(disabledTile);
        expect(container.querySelector('.apps-runtime-tab')?.textContent || '').not.toContain('数据同步');

        // Disabled state persists asynchronously (SQLite-gated write).
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            expect(stored.disabledIds).toContain('data-sync');
            expect(stored.hiddenIds || []).not.toContain('data-sync');
        });

        unmount();
        render(<AppsPage lang="zh-Hans" />);
        const persistedTile = screen.getAllByText('数据同步')[0].closest('.apps-app-tile') as HTMLButtonElement;
        expect(persistedTile.getAttribute('data-status')).toBe('disabled');

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        const persistedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('数据同步')) as HTMLElement;
        fireEvent.click(within(persistedRow).getByTitle('启用'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));

        const enabledTile = screen.getAllByText('数据同步')[0].closest('.apps-app-tile') as HTMLButtonElement;
        expect(enabledTile.getAttribute('data-status')).toBe('available');
        fireEvent.click(enabledTile);
        expect(document.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('数据同步');
    });
    it('removes dynamic local apps from app studio management', async () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        const webCollectRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('网页采集')) as HTMLElement;
        fireEvent.click(within(webCollectRow).getByTitle('移除'));
        fireEvent.click(screen.getByText('\u5173\u95ed'));

        expect(screen.queryByText('网页采集')).toBeNull();
        // Removal persists asynchronously (SQLite-gated write).
        await waitFor(() => {
            const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
            expect((stored.customApps || []).some((app: any) => app.id === 'web-capture')).toBe(false);
            expect(stored.hiddenIds || []).not.toContain('web-capture');
        });

        unmount();
        render(<AppsPage lang="zh-Hans" />);

        expect(screen.queryByText('网页采集')).toBeNull();
    });

    it('filters removed dynamic apps out of app studio management', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());
        const dataSyncRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('\u6570\u636e\u540c\u6b65')) as HTMLElement;
        fireEvent.click(within(dataSyncRow).getByTitle('移除'));

        const manageSearch = document.querySelector('.apps-manage-filter .apps-search') as HTMLInputElement;
        fireEvent.change(manageSearch, { target: { value: 'sync' } });

        expect(screen.queryByText('\u5df2\u9690\u85cf\u5e94\u7528')).toBeNull();
        expect(screen.getByText('\u6ca1\u6709\u5339\u914d\u7684\u5e94\u7528')).not.toBeNull();
        expect(document.querySelector('.apps-manage-toolbar .apps-count')?.textContent).toBe('0/9');
        expect(Array.from(document.querySelectorAll('.apps-manage-row--hidden')).some((row) => row.textContent?.includes('\u6570\u636e\u540c\u6b65'))).toBe(false);
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((row) => row.textContent?.includes('\u6570\u636e\u540c\u6b65'))).toBe(false);
    });

    it('keeps pinned apps capped at two rows', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(getStudioButton());
        fireEvent.click(getManageTab());

        for (const name of ['文档脱敏', '表格分析', '库存盘点', '合同审查', '发票审核', '网页采集']) {
            const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes(name)) as HTMLElement;
            fireEvent.click(within(row).getByTitle('\u7f6e\u9876'));
        }

        const nextRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('数据同步')) as HTMLElement;
        const pinButton = within(nextRow).getByTitle('\u5e38\u7528\u5e94\u7528\u5df2\u6ee1 8 \u4e2a\uff0c\u8bf7\u5148\u53d6\u6d88\u4e00\u4e2a') as HTMLButtonElement;
        expect(pinButton.disabled).toBe(true);
        fireEvent.click(pinButton);

        expect(screen.getByText('8/8')).not.toBeNull();
    });

    it('turns DataSrv capabilities into addable app candidates', async () => {
        getMISDataConfigMock.mockResolvedValue({ enabled: true, endpoint: 'http://datasrv.test', token: 'token' });
        const fetchMock = vi.fn().mockResolvedValue({
            ok: true,
            json: async () => ({
                service: 'MaClawDataSrv',
                domains: ['hr'],
                business_actions: [{ id: 'hr.leave_request_upsert', domain: 'hr', title: 'Manage leave requests', description: 'Leave flow' }],
                business_views: [{ id: 'hr.leave_request_review', domain: 'hr', title: 'Leave review' }],
                reports: [{ id: 'hr.leave_by_status', domain: 'hr', title: 'Leave by status' }],
                dashboards: [{ id: 'hr.overview', domain: 'hr', title: 'HR overview' }],
            }),
        });
        vi.stubGlobal('fetch', fetchMock);

        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(getStudioButton());

        await waitFor(() => expect(screen.getByText('\u53ef\u751f\u6210\u5e94\u7528')).not.toBeNull());
        fireEvent.click(screen.getByText('\u52a0\u5230\u9762\u677f'));

        expect(screen.getByText('\u5df2\u6dfb\u52a0')).not.toBeNull();
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        expect(screen.getAllByText('\u4eba\u4e8b').length).toBeGreaterThan(0);
    });

    it('restores DataSrv installed enterprise normal app run evidence into app candidates', async () => {
        getMISDataConfigMock.mockResolvedValue({ enabled: true, endpoint: 'http://datasrv.test', token: 'token' });
        const fetchMock = vi.fn().mockResolvedValue({
            ok: true,
            json: async () => ({
                service: 'MaClawDataSrv',
                domains: ['sales'],
                app_installations: [{
                    app_id: 'sales.customer.console',
                    blueprint_id: 'sales.customer.console',
                    name: 'Customer Console Installed',
                    version: '5',
                    kind: 'enterprise_normal_app',
                    source: 'hub',
                    role_bindings: [{ object_role: 'customer', domain: 'sales', dataset_id: 'sales.customers', required: true }],
                    metadata: {
                        hub_capability_id: 'cap-sales-customer-console',
                        hub_market_capability_id: 'cap-sales-customer-console',
                        hub_submission_id: 'hub-review-customer-console',
                        hub_version_key: 'enterprise_hub:skill:maclaw-app:sales.customer.console@5',
                        hub_review_status: 'published',
                        hub_package_sha256: 'sha256-customer-console-package',
                        hub_package_signature: {
                            schema: 'maclaw.app.package_signature.v1',
                            algorithm: 'ed25519',
                            public_key_fingerprint: 'sha256:customer-console-key',
                            signed_at: '2026-07-01T01:00:00Z',
                            signed_by: 'enterprise-market',
                            package_sha256: 'sha256-customer-console-package',
                        },
                        hub_package_signature_algorithm: 'ed25519',
                        hub_package_signature_fingerprint: 'sha256:customer-console-key',
                        hub_package_signature_signed_at: '2026-07-01T01:00:00Z',
                        hub_package_signature_signed_by: 'enterprise-market',
                        review_evidence: { status: 'published', reviewer: 'enterprise-market' },
                        app_skill_id: 'customer-console-skill',
                        app_skill_version: '2.0.0',
                        app_skill_source: 'hub',
                        result_contract: {
                            schema: 'maclaw.app.result.v1',
                            primary: 'business_status',
                            types: ['business_status', 'business_record', 'content'],
                        },
                        result_contract_delivery_modes: ['inline_content', 'business_record'],
                        result_contract_delivery_inline_content: true,
                        result_contract_delivery_artifacts: false,
                        result_contract_delivery_business_record: true,
                        result_contract_delivery_notifications: false,
                        workspace_layout: {
                            entry: 'business_workspace',
                            template: 'classic_split',
                            density: 'compact',
                            primaryRegion: 'left',
                            outputRegion: 'right',
                            fingerprint: 'layout-customer-console-installed',
                            visibleRegionCount: 3,
                            regions: [
                                { id: 'customer_filters', role: 'input', placement: 'left', visible: true },
                                { id: 'customer_list', role: 'record_list', placement: 'center', visible: true },
                                { id: 'customer_output', role: 'output', placement: 'right', visible: true },
                            ],
                            studio: { savedInManifest: true, editable: true, updatedBy: 'app_studio' },
                        },
                        dependency_verification: {
                            schema: 'maclaw.app.install_plan.v1',
                            dependency_count: 1,
                            has_missing_required: false,
                            has_blocking_dependency: false,
                            dependencies: [{ id: 'customer-console-skill', version: '2.0.0', kind: 'app_skill', source: 'hub', installed: true, health: 'ready', action: 'skip' }],
                        },
                        test_evidence: {
                            run_id: 'run-customer-imported',
                            verified_at: '2026-06-22T09:30:00Z',
                            definition_hash: 'sha256:customer-console',
                            test_protocol_fingerprint: 'proto-customer',
                            primary_result: 'business_status',
                            output_count: 1,
                            result_payload: { business_status: 'renewal_ready', business_record: { id: 'customer-1', status: 'renewal_ready' }, text: 'renewal ready' },
                            outputs: [{ kind: 'business_record', title: 'Customer renewal', text: '{"id":"customer-1"}', status: 'ready', data: { id: 'customer-1', status: 'renewal_ready' } }],
                            artifacts: [{ id: 'customer-renewal-pack', name: 'customer-renewal.pdf', uri: 'artifact://customer/renewal.pdf', status: 'ready' }],
                        },
                        test_evidence_result_coverage_ok: true,
                        test_evidence_result_coverage_primary: 'business_status',
                        test_evidence_result_coverage_covered_count: 3,
                        test_evidence_result_coverage_missing_count: 0,
                        test_evidence_test_protocol: {
                            schema: 'maclaw.app.test_protocol.v1',
                            fingerprint: 'proto-customer',
                            sample_input: { customer_id: 'customer-1' },
                            expected_output: { business_status: 'renewal_ready' },
                            required_roles: ['operator'],
                            required_scopes: ['app.run'],
                            risk_level: 'medium',
                        },
                        test_evidence_test_protocol_fingerprint: 'proto-customer',
                        datasrv_registration: {
                            synced: true,
                            eligible_count: 1,
                            synced_count: 1,
                            failed_count: 0,
                            items: [{ app_id: 'sales.customer.console', synced: true, role_binding_count: 1 }],
                        },
                    },
                }],
            }),
        });
        vi.stubGlobal('fetch', fetchMock);

        render(<AppsPage lang="en" />);
        fireEvent.click(getStudioButton());

        await waitFor(() => expect(screen.getByText('Customer Console Installed')).not.toBeNull());
        fireEvent.click(screen.getByText('Add to panel'));
        fireEvent.click(screen.getAllByText('Close')[0]);

        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        const added = stored.customApps.find((app: any) => app.name === 'Customer Console Installed');
        expect(added.kind).toBe('enterprise_normal_app');
        expect(added.manifest.datasrv).toMatchObject({ appID: 'sales.customer.console', domain: 'sales', datasetID: 'sales.customers', objectRole: 'customer', blueprintID: 'sales.customer.console' });
        expect(added.manifest.appSkill).toMatchObject({ id: 'customer-console-skill', version: '2.0.0', source: 'hub' });
        expect(added.manifest.resultContract).toMatchObject({
            schema: 'maclaw.app.result.v1',
            primary: 'business_status',
            types: ['business_status', 'business_record', 'content'],
            delivery: { inlineContent: true, artifacts: false, businessRecord: true, notifications: false },
        });
        expect(added.manifest.testProtocol).toMatchObject({ schema: 'maclaw.app.test_protocol.v1', sampleInput: { customer_id: 'customer-1' }, expectedOutput: { business_status: 'renewal_ready' }, requiredRoles: ['operator'], requiredScopes: ['app.run'], riskLevel: 'medium' });
        expect(added.importedRunEvidence).toMatchObject({
            runID: 'run-customer-imported',
            appID: 'sales.customer.console',
            definitionHash: 'sha256:customer-console',
            testProtocolFingerprint: 'proto-customer',
            outputMode: 'business_status',
            resultPayload: { business_status: 'renewal_ready', business_record: { id: 'customer-1', status: 'renewal_ready' }, text: 'renewal ready' },
            outputs: [{ kind: 'business_record', title: 'Customer renewal', text: '{"id":"customer-1"}', status: 'ready', data: { id: 'customer-1', status: 'renewal_ready' } }],
            artifacts: [{ id: 'customer-renewal-pack', name: 'customer-renewal.pdf', uri: 'artifact://customer/renewal.pdf', status: 'ready' }],
            resultCoverage: { ok: true, primary: 'business_status', covered_count: 3, missing_count: 0 },
        });
        expect(added.installEvidence).toMatchObject({
            schema: 'maclaw.app.install_record.v1',
            apps: [{ id: 'sales.customer.console', name: 'Customer Console Installed', kind: 'enterprise_normal_app' }],
            version_snapshot: { app_entry_version: '5', app_skill: { id: 'customer-console-skill', version: '2.0.0', kind: 'app_skill', source: 'hub' } },
            result_contract: { primary: 'business_status', types: ['business_status', 'business_record', 'content'] },
            workspace_layout: { entry: 'business_workspace', template: 'classic_split', density: 'compact', fingerprint: 'layout-customer-console-installed' },
            dependency_verification: { schema: 'maclaw.app.install_plan.v1', dependency_count: 1, has_blocking_dependency: false },
            test_evidence: { run_id: 'run-customer-imported', definition_hash: 'sha256:customer-console', test_protocol_fingerprint: 'proto-customer' },
            submission: { status: 'published', capability_id: 'cap-sales-customer-console', version_key: 'enterprise_hub:skill:maclaw-app:sales.customer.console@5', submission_id: 'hub-review-customer-console', package_sha256: 'sha256-customer-console-package', package_signature: { algorithm: 'ed25519', public_key_fingerprint: 'sha256:customer-console-key' } },
            hub_package_signature: { algorithm: 'ed25519', public_key_fingerprint: 'sha256:customer-console-key', signed_by: 'enterprise-market' },
            hub_package_signature_algorithm: 'ed25519',
            hub_package_signature_fingerprint: 'sha256:customer-console-key',
            hub_package_signature_signed_at: '2026-07-01T01:00:00Z',
            hub_package_signature_signed_by: 'enterprise-market',
            review_evidence: { status: 'published', reviewer: 'enterprise-market' },
            datasrv_registration: { synced: true, eligible_count: 1, synced_count: 1, failed_count: 0, items: [{ app_id: 'sales.customer.console', synced: true, role_binding_count: 1 }] },
        });
        expect(added.installEvidence.test_evidence.result_coverage).toMatchObject({ ok: true, primary: 'business_status', covered_count: 3, missing_count: 0 });
        expect(added.installEvidence.test_evidence.artifacts[0]).toMatchObject({ id: 'customer-renewal-pack', name: 'customer-renewal.pdf' });
        expect(added.installEvidence.test_evidence.test_protocol).toMatchObject({
            schema: 'maclaw.app.test_protocol.v1',
            fingerprint: 'proto-customer',
            sample_input: { customer_id: 'customer-1' },
            expected_output: { business_status: 'renewal_ready' },
        });

        fireEvent.click(screen.getByRole('button', { name: /Customer Console Installed/ }));
        const runtimeGovernance = await findRuntimeGovernancePanel();
        expect(within(runtimeGovernance).getByText('Source')).not.toBeNull();
        expect(within(runtimeGovernance).getByText(/published · cap-sales-customer-console/)).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Package signature')).not.toBeNull();
        expect(within(runtimeGovernance).getByText(/ed25519.*sha256:customer-console-key.*enterprise-market/)).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Workspace layout')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('business_workspace · classic_split · compact')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Result contract')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('business_status · 3 types')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Test evidence')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('run-customer-imported · proto-customer')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Result coverage')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('business_status · Covered: 3')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Result package')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Output: 1 · Output artifacts: 1')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Dependency verification')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Skill dependencies: 1 · Blocking deps: 0')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('DataSrv')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('DataSrv bindings registered: 1/1')).not.toBeNull();
    });

    it('turns DataSrv installed MaClaw apps into addable app candidates with layout metadata', async () => {
        getMISDataConfigMock.mockResolvedValue({ enabled: true, endpoint: 'http://datasrv.test', token: 'token' });
        const fetchMock = vi.fn().mockResolvedValue({
            ok: true,
            json: async () => ({
                service: 'MaClawDataSrv',
                domains: ['finance'],
                business_actions: [{ id: 'finance.expense_upsert', domain: 'finance', title: 'Expense upsert' }],
                app_installations: [{
                    app_id: 'mis.expense',
                    blueprint_id: 'mis.expense.approval',
                    name: 'Expense Approval Installed',
                    version: '3',
                    kind: 'enterprise_approval_app',
                    source: 'hub',
                    updated_at: '2026-06-21T11:05:00Z',
                    role_bindings: [{ object_role: 'expense_report', domain: 'finance', dataset_id: 'finance.expense_forms', template_id: 'finance.expenses', required: true }],
                    metadata: {
                        package_sha: 'sha-datasrv-install',
                        package_sha256: 'sha256-datasrv-install',
                        app_skill_id: 'expense-super-skill',
                        app_skill_version: '1.0.0',
                        workflow_skill_ids: ['expense-workflow'],
                        workflow_skill_versions: ['expense-workflow@2.1.0'],
                        approval_binding_versions: ['expense.submitted:expense-workflow@2.1.0'],
                        workflow_contract: {
                            schema: 'maclaw.app.workflow_contract.v1',
                            workflow_skill_id: 'expense-workflow',
                            workflow_version: '2.1.0',
                            object_role: 'expense_report',
                            required_inputs: ['record_ref', 'applicant', 'business_payload'],
                            decision_outputs: ['approved', 'rejected', 'attention'],
                        },
                        workflow_contract_status_mapping: { pending: 'finance_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requires_input: 'requires_input' },
                        workflow_mapping: {
                            schema: 'maclaw.app.workflow.v1',
                            submit_node: 'expense_report.submit',
                            approval_node: 'finance.director_review',
                            result_node: 'expense_report.result_pack',
                            attention_node: 'finance.attention_review',
                            status_mapping: { pending: 'finance_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requires_input: 'requires_input' },
                        },
                        result_contract: {
                            schema: 'maclaw.app.result.v1',
                            primary: 'approval_result',
                            types: ['approval_result', 'business_status', 'artifact', 'notification'],
                            approval_decisions: ['approved', 'rejected', 'attention'],
                            delivery: { inlineContent: true, artifacts: true, businessRecord: true, notifications: true },
                        },
                        workspace_layout_entry: 'approval_workspace',
                        workspace_layout_template: 'dashboard',
                        workspace_layout_density: 'spacious',
                        workspace_layout_primary_region: 'request_form',
                        workspace_layout_output_region: 'result_panel',
                        workspace_layout_region_count: 3,
                        workspace_layout_region_ids: ['request_form', 'approval_inbox', 'result_panel'],
                        workspace_layout_navigation: ['my_requests', 'pending_my_approval', 'attention'],
                        workspace_layout_list_columns: ['title', 'applicant', 'current_node', 'status'],
                        workspace_layout: {
                            entry: 'approval_workspace',
                            template: 'dashboard',
                            density: 'spacious',
                            primaryRegion: 'request_form',
                            outputRegion: 'result_panel',
                            navigation: ['my_requests', 'pending_my_approval', 'attention'],
                            list: { columns: ['title', 'applicant', 'current_node', 'status'] },
                            regions: [
                                { id: 'request_form', role: 'input', placement: 'left' },
                                { id: 'approval_inbox', role: 'instance_list', placement: 'center' },
                                { id: 'result_panel', role: 'output', placement: 'bottom' },
                            ],
                        },
                        governance_status: 'local_tested',
                        test_evidence: {
                            run_id: 'run-expense-imported',
                            verified_at: '2026-06-21T11:00:00Z',
                            definition_fingerprint: 'sha256:expense-app',
                            test_protocol: {
                                schema: 'maclaw.app.test_protocol.v1',
                                fingerprint: 'proto-expense',
                                sample_input: { record_ref: 'expense-1', amount: 1280 },
                                expected_output: { approval_result: 'approved' },
                                required_roles: ['applicant', 'approver'],
                                required_scopes: ['app.run'],
                                risk_level: 'medium',
                            },
                            test_protocol_fingerprint: 'proto-expense',
                            artifact_present: true,
                            artifact_name: 'expense-approval-evidence.zip',
                            output_count: 1,
                            primary_result: 'approval_result',
                            approval_instance: {
                                approval_id: 'approval-remote-imported',
                                workflow_instance_id: 'wf-remote-imported',
                                record_id: 'expense-1',
                                status: 'approved',
                                current_node: 'expense_report.result_feedback',
                                current_node_ids: ['expense_report.result_feedback', 'finance.archive'],
                                approval_instance_view_verified: true,
                                approval_views: { my_requests: true, handled: true, all: true },
                                result_payload: { decision: 'approved', business_status: 'finance_approved' },
                                outputs: [{ kind: 'table', title: 'Approval rows', text: 'expense approved', status: 'ready', data: { rows: [{ id: 'expense-1', status: 'finance_approved' }] } }],
                                artifacts: [{ id: 'artifact-expense-evidence', uri: 'artifact://expense/evidence.zip', name: 'expense-approval-evidence.zip', status: 'ready' }],
                            },
                            progress_instances: [{
                                approval_id: 'approval-remote-imported',
                                workflow_instance_id: 'wf-remote-imported',
                                record_id: 'expense-1',
                                status: 'running',
                                current_node: 'finance.director_review',
                                current_node_ids: ['expense_report.submit', 'finance.director_review'],
                                result_status: 'running',
                                business_status: 'finance_pending',
                                outputs: [{ kind: 'content', title: 'Workflow Progress', text: 'director review started', status: 'running' }],
                            }],
                        },
                        dependency_verification: {
                            schema: 'maclaw.app.install_plan.v1',
                            verified_at: '2026-06-21T10:58:00Z',
                            dependency_count: 1,
                            has_missing_required: false,
                            has_blocking_dependency: false,
                            has_workflow_contract_issue: false,
                            workflow_contract_issue_count: 0,
                            has_governance_review_issue: false,
                            governance_review_issue_count: 0,
                            dependencies: [{ id: 'expense-workflow', version: '2.1.0', kind: 'workflow_skill', required: true, source: 'hub', install_ref: 'cap-hub-expense-workflow', installed: true, health: 'ready', action: 'skip', app_ids: ['datasrv-installed-mis.expense'] }],
                        },
                        install_evidence: {
                            apps: [{ id: 'mis.expense', name: 'Expense Approval Package Entry', kind: 'enterprise_approval_app' }],
                        },
                        test_evidence_dependency_verified_at: '2026-06-21T10:58:00Z',
                        test_evidence_dependency_count: 1,
                        test_evidence_dependency_missing_required: false,
                        test_evidence_dependency_blocking: false,
                        test_evidence_result_coverage_ok: true,
                        test_evidence_result_coverage_primary: 'approval_result',
                        test_evidence_covered_types: ['approval_result', 'business_status', 'artifact'],
                        test_evidence_missing_types: ['notification'],
                        test_evidence_workflow_contract_issue: false,
                        test_evidence_workflow_contract_issue_count: 0,
                        test_evidence_governance_review_issue: false,
                        test_evidence_governance_review_issue_count: 0,
                        has_missing_required_dependency: true,
                        has_blocking_dependency: true,
                        dependencies: [{ id: 'expense-workflow', kind: 'workflow_skill', required: true, source: 'hub', install_ref: 'cap-hub-expense-workflow' }],
                    },
                }],
            }),
        });
        vi.stubGlobal('fetch', fetchMock);

        render(<AppsPage lang="en" />);
        fireEvent.click(getStudioButton());

        await waitFor(() => expect(screen.getByText('Expense Approval Installed')).not.toBeNull());
        fireEvent.click(screen.getByText('Add to panel'));
        fireEvent.click(screen.getAllByText('Close')[0]);

        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        const added = stored.customApps.find((app: any) => app.name === 'Expense Approval Installed');
        expect(added.kind).toBe('enterprise_approval_app');
        expect(added.id).toBe('datasrv-installed-mis.expense');
        expect(added.manifest.datasrv).toMatchObject({ appID: 'mis.expense', domain: 'finance', datasetID: 'finance.expense_forms', objectRole: 'expense_report', blueprintID: 'mis.expense.approval', templateID: 'finance.expenses' });
        expect(added.manifest.appSkill).toMatchObject({ id: 'expense-super-skill', version: '1.0.0' });
        expect(added.manifest.dependencies.skills.find((dep: any) => dep.id === 'expense-workflow')).toMatchObject({ version: '2.1.0', kind: 'workflow_skill', install_ref: 'cap-hub-expense-workflow' });
        expect(added.manifest.resultContract).toMatchObject({
            schema: 'maclaw.app.result.v1',
            primary: 'approval_result',
            types: ['approval_result', 'business_status', 'artifact', 'notification'],
            approvalDecisions: ['approved', 'rejected', 'attention'],
            delivery: { inlineContent: true, artifacts: true, businessRecord: true, notifications: true },
        });
        expect(added.manifest.workflow).toMatchObject({
            schema: 'maclaw.app.workflow.v1',
            submitNode: 'expense_report.submit',
            approvalNode: 'finance.director_review',
            resultNode: 'expense_report.result_pack',
            attentionNode: 'finance.attention_review',
            statusMapping: { pending: 'finance_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' },
        });
        expect(added.manifest.testProtocol).toMatchObject({
            schema: 'maclaw.app.test_protocol.v1',
            sampleInput: { record_ref: 'expense-1', amount: 1280 },
            expectedOutput: { approval_result: 'approved' },
            requiredRoles: ['applicant', 'approver'],
            requiredScopes: ['app.run'],
            riskLevel: 'medium',
        });
        expect(added.manifest.testProtocol.fingerprint).toMatch(/^[0-9a-f]{8}$/);
        expect(added.manifest.mis.approvalBindings[0]).toMatchObject({ event: 'expense.submitted', workflowSkillId: 'expense-workflow', workflowVersion: '2.1.0', objectRole: 'expense_report' });
        expect(added.workflowContract).toMatchObject({
            schema: 'maclaw.app.workflow_contract.v1',
            workflowSkillId: 'expense-workflow',
            workflowVersion: '2.1.0',
            objectRole: 'expense_report',
            requiredInputs: ['record_ref', 'applicant', 'business_payload'],
            decisionOutputs: ['approved', 'rejected', 'attention'],
            statusMapping: { pending: 'finance_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' },
        });
        expect(added.versionSnapshot).toMatchObject({
            app_entry_version: '3',
            app_skill: { id: 'expense-super-skill', version: '1.0.0', kind: 'app_skill', source: 'hub' },
            workflow_skills: [{ id: 'expense-workflow', version: '2.1.0', kind: 'workflow_skill', source: 'hub' }],
            approval_bindings: [{ event: 'expense.submitted', object_role: 'expense_report', workflow_skill_id: 'expense-workflow', workflow_version: '2.1.0' }],
        });
        expect(added.installEvidence).toMatchObject({
            schema: 'maclaw.app.install_record.v1',
            package_sha: 'sha-datasrv-install',
            package_sha256: 'sha256-datasrv-install',
            source: 'hub',
            installed_at: '2026-06-21T11:05:00Z',
            apps: [{ id: 'mis.expense', name: 'Expense Approval Package Entry', kind: 'enterprise_approval_app' }],
            version_snapshot: { app_entry_version: '3' },
            workspace_layout: { entry: 'approval_workspace', template: 'dashboard', density: 'spacious' },
            result_contract: { primary: 'approval_result', types: ['approval_result', 'business_status', 'artifact', 'notification'] },
            workflow_mapping: { schema: 'maclaw.app.workflow.v1', approvalNode: 'finance.director_review', resultNode: 'expense_report.result_pack', statusMapping: { pending: 'finance_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' } },
            workflow_contract: { workflowSkillId: 'expense-workflow', workflowVersion: '2.1.0', objectRole: 'expense_report', statusMapping: { pending: 'finance_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' } },
            test_evidence: { run_id: 'run-expense-imported', definition_fingerprint: 'sha256:expense-app', test_protocol_fingerprint: 'proto-expense' },
            dependency_verification: { schema: 'maclaw.app.install_plan.v1', dependency_count: 1, has_blocking_dependency: false },
            has_missing_required: false,
            has_blocking_dependency: false,
        });
        expect(added.installEvidence.test_evidence.result_payload).toEqual({ decision: 'approved', business_status: 'finance_approved' });
        expect(added.installEvidence.test_evidence.outputs[0]).toMatchObject({ kind: 'table', title: 'Approval rows', text: 'expense approved' });
        expect(added.installEvidence.test_evidence.artifacts[0]).toMatchObject({ id: 'artifact-expense-evidence', name: 'expense-approval-evidence.zip' });
        expect(added.installEvidence.test_evidence.result_coverage).toMatchObject({
            ok: true,
            primary: 'approval_result',
            covered_types: ['approval_result', 'business_status', 'artifact'],
            missing_types: ['notification'],
        });
        expect(added.installEvidence.test_evidence.approval_instance.result_payload).toEqual({ decision: 'approved', business_status: 'finance_approved' });
        expect(added.installEvidence.test_evidence.approval_instance.outputs[0]).toMatchObject({ kind: 'table', title: 'Approval rows', text: 'expense approved' });
        expect(added.installEvidence.test_evidence.approval_instance.artifacts[0]).toMatchObject({ id: 'artifact-expense-evidence', name: 'expense-approval-evidence.zip' });
        expect(added.installEvidence.dependencies[0]).toMatchObject({ id: 'expense-workflow', install_ref: 'cap-hub-expense-workflow', installed: true, health: 'ready' });
        expect(added.manifest.ui.layouts.approval_workspace.template).toBe('dashboard');
        expect(added.manifest.ui.layouts.approval_workspace.density).toBe('spacious');
        expect(added.manifest.ui.layouts.approval_workspace.primaryRegion).toBe('request_form');
        expect(added.manifest.ui.layouts.approval_workspace.outputRegion).toBe('result_panel');
        expect(added.manifest.ui.layouts.approval_workspace.navigation).toEqual(['my_requests', 'pending_my_approval', 'attention']);
        expect(added.manifest.ui.layouts.approval_workspace.list.columns).toEqual(['title', 'applicant', 'current_node', 'status']);
        expect(added.manifest.ui.layouts.approval_workspace.regions).toEqual([
            { id: 'request_form', role: 'input', placement: 'left' },
            { id: 'approval_inbox', role: 'instance_list', placement: 'center' },
            { id: 'result_panel', role: 'output', placement: 'bottom' },
        ]);
        expect(added.manifest.ui.layouts.approval_workspace.studio.importedFromDataSrv).toBe(true);
        expect(added.importedRunEvidence).toMatchObject({
            runID: 'run-expense-imported',
            definitionHash: 'sha256:expense-app',
            testProtocolFingerprint: 'proto-expense',
            outputMode: 'approval_result',
            artifactName: 'expense-approval-evidence.zip',
            artifacts: [{ id: 'artifact-expense-evidence', uri: 'artifact://expense/evidence.zip', name: 'expense-approval-evidence.zip', status: 'ready' }],
            resultPayload: { decision: 'approved', business_status: 'finance_approved' },
            outputs: [{ kind: 'table', title: 'Approval rows', text: 'expense approved', status: 'ready', data: { rows: [{ id: 'expense-1', status: 'finance_approved' }] } }],
            dependencyVerification: {
                schema: 'maclaw.app.install_plan.v1',
                verifiedAt: '2026-06-21T10:58:00Z',
                dependencyCount: 1,
                hasMissingRequired: false,
                hasBlockingDependency: false,
                hasWorkflowContractIssue: false,
                workflowContractIssueCount: 0,
                hasGovernanceReviewIssue: false,
                governanceReviewIssueCount: 0,
            },
        });
        expect(added.importedRunEvidence.dependencyVerification.dependencies[0]).toMatchObject({ id: 'expense-workflow', install_ref: 'cap-hub-expense-workflow', installed: true, health: 'ready' });
        expect(added.importedRunEvidence.approvalInstance).toMatchObject({
            instanceId: 'wf-remote-imported',
            approvalID: 'approval-remote-imported',
            workflowInstanceId: 'wf-remote-imported',
            progressInstances: [expect.objectContaining({
                currentNode: 'finance.director_review',
                resultStatus: 'running',
                businessStatus: 'finance_pending',
                outputs: [expect.objectContaining({ title: 'Workflow Progress' })],
            })],
            recordID: 'expense-1',
            status: 'approved',
            currentNode: 'expense_report.result_feedback',
            currentNodeIDs: ['expense_report.result_feedback', 'finance.archive'],
            approvalInstanceViewVerified: true,
            approvalViews: { my_requests: true, handled: true, all: true },
            resultPayload: { decision: 'approved', business_status: 'finance_approved' },
            outputs: [{ kind: 'table', title: 'Approval rows', text: 'expense approved', status: 'ready', data: { rows: [{ id: 'expense-1', status: 'finance_approved' }] } }],
            artifacts: [{ id: 'artifact-expense-evidence', uri: 'artifact://expense/evidence.zip', name: 'expense-approval-evidence.zip', status: 'ready' }],
        });
        expect(added.importedRunEvidence.outputs[0]).toMatchObject({ kind: 'table', title: 'Approval rows', text: 'expense approved' });

        fireEvent.click(screen.getByText('Expense Approval Installed'));
        await waitFor(() => expect(listMaclawAppApprovalInstancesMock).toHaveBeenCalledWith('mis.expense', 'all', 50));
        expect(listMaclawAppApprovalInstancesMock).not.toHaveBeenCalledWith('datasrv-installed-mis.expense', 'all', 50);
        const runtimeVersionSnapshot = await waitFor(() => {
            const snapshot = document.querySelector('.apps-detail__header .apps-install-version-snapshot') as HTMLElement | null;
            expect(snapshot).not.toBeNull();
            return snapshot as HTMLElement;
        });
        expect(runtimeVersionSnapshot.getAttribute('aria-label')).toBe('Version snapshot');
        expect(within(runtimeVersionSnapshot).getByText('Version')).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText('v3')).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText(miniAppLabels.skillField.en)).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText('expense-super-skill · app_skill · hub · v1.0.0')).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText('Approval workflow')).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText('expense-workflow · workflow_skill · hub · v2.1.0')).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText('Approval binding')).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText('expense.submitted · expense_report · expense-workflow@v2.1.0')).not.toBeNull();
        const runtimeWorkflowContract = document.querySelector('.apps-detail__header .apps-workflow-contract-summary') as HTMLElement | null;
        expect(runtimeWorkflowContract).not.toBeNull();
        expect(runtimeWorkflowContract?.getAttribute('aria-label')).toBe('Runtime contract');
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('Runtime contract aligned')).not.toBeNull();
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('Approval workflow')).not.toBeNull();
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('expense-workflow@v2.1.0')).not.toBeNull();
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('Business object')).not.toBeNull();
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('expense_report')).not.toBeNull();
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('Inputs')).not.toBeNull();
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('record_ref, applicant, business_payload')).not.toBeNull();
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('Outputs')).not.toBeNull();
        expect(within(runtimeWorkflowContract as HTMLElement).getByText('approved, rejected, attention')).not.toBeNull();
        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalled());
        const runtimePlanPayload = JSON.parse(planMaclawAppInstallMock.mock.calls.at(-1)?.[0] as string);
        expect(runtimePlanPayload.app.id).toBe('mis.expense');
        getNLSkillRunStatusMock.mockResolvedValueOnce({
            run_id: 'run-test-1',
            status: 'success',
            summary: {
                last_output_snippet: JSON.stringify({
                    approval_result: 'approved',
                    business_status: 'finance_approved',
                    result_status: 'approved',
                    business_record: { id: 'expense-restored-1', status: 'finance_approved' },
                    approval_instance: {
                        record_id: 'expense-restored-1',
                        current_node: 'expense_report.result_pack',
                        current_node_ids: ['expense_report.result_pack', 'finance.archive'],
                    },
                    outputs: [{ kind: 'notification', title: 'Restored approval notice', text: 'Finance approved restored run', status: 'ready' }],
                    artifacts: [{ id: 'restored-approval-pack', uri: 'artifact://expense/restored.zip', name: 'restored-approval-pack.zip', status: 'ready' }],
                    text: 'restored approval completed',
                }),
            },
            outputs: [{ id: 'restored-approval-record', kind: 'business_record', title: 'Restored expense record', text: 'expense-restored-1', status: 'ready', data: { id: 'expense-restored-1', status: 'finance_approved' } }],
            artifacts: [{ id: 'restored-approval-pdf', uri: 'artifact://expense/restored.pdf', name: 'restored-approval.pdf', status: 'ready', mime_type: 'application/pdf', size_bytes: 4096 }],
        });
        syncMaclawAppApprovalInstanceToDataSrvMock
            .mockResolvedValueOnce({ synced: true, approval_id: 'approval-restored-pending', dataset_id: 'finance.expense_forms' })
            .mockResolvedValueOnce({ synced: true, approval_id: 'approval-restored-final', record_id: 'expense-restored-1', dataset_id: 'finance.expense_forms' });
        fireEvent.click(screen.getByText('Run'));
        await waitFor(() => expect(startMaclawAppApprovalWorkflowMock).toHaveBeenCalledWith(expect.objectContaining({
            app_id: 'mis.expense',
            app_name: 'Expense Approval Installed',
            dataset_id: 'finance.expense_forms',
            object_role: 'expense_report',
            blueprint_id: 'mis.expense.approval',
            approval_event: 'expense.submitted',
            workflow_skill_id: 'expense-workflow',
            workflow_version: '2.1.0',
            current_node: 'finance.director_review',
            business_status: 'finance_pending',
            result_status: 'pending',
            run_workflow_skill: true,
        })));
        const startPayload = startMaclawAppApprovalWorkflowMock.mock.calls.at(-1)?.[0];
        expect(startPayload.workflow_run_args.record_ref).toMatchObject({
            app_id: 'mis.expense',
            dataset_id: 'finance.expense_forms',
            object_role: 'expense_report',
            blueprint_id: 'mis.expense.approval',
        });
        expect(startPayload.workflow_run_args.workflow_contract).toMatchObject({
            workflowSkillId: 'expense-workflow',
            workflowVersion: '2.1.0',
            objectRole: 'expense_report',
            requiredInputs: ['record_ref', 'applicant', 'business_payload'],
            decisionOutputs: ['approved', 'rejected', 'attention'],
            statusMapping: { pending: 'finance_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' },
        });
        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('expense-workflow', expect.objectContaining({ app_id: 'mis.expense' })));
        const workflowPayload = runNLSkillAsyncMock.mock.calls.at(-1)?.[1];
        expect(workflowPayload.record_ref).toMatchObject({
            app_id: 'mis.expense',
            dataset_id: 'finance.expense_forms',
            object_role: 'expense_report',
            blueprint_id: 'mis.expense.approval',
        });
        expect(workflowPayload.applicant).toMatchObject({ id: 'current-user', type: 'user' });
        expect(workflowPayload.business_payload).toMatchObject({
            app_id: 'mis.expense',
            business_entity: added.category,
            business_action: 'create',
            dataset_id: 'finance.expense_forms',
            object_role: 'expense_report',
        });
        expect(workflowPayload.business_payload.record_ref).toMatchObject({ app_id: 'mis.expense' });
        expect(workflowPayload.business_payload.applicant).toMatchObject({ id: 'current-user' });
        expect(workflowPayload.workflow_contract).toMatchObject({
            workflowSkillId: 'expense-workflow',
            requiredInputs: ['record_ref', 'applicant', 'business_payload'],
            statusMapping: { pending: 'finance_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requiresInput: 'requires_input' },
        });
        expect(workflowPayload.workflow_required_inputs).toEqual(['record_ref', 'applicant', 'business_payload']);
        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalledWith(expect.objectContaining({ app_id: 'mis.expense' })));
        expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalledWith(expect.objectContaining({ app_id: 'mis.expense', record_id: expect.stringMatching(/^appr-/) }));
        expect(recordMaclawAppApprovalInstanceMock).not.toHaveBeenCalledWith(expect.objectContaining({ app_id: 'datasrv-installed-mis.expense' }));
        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalledWith(expect.objectContaining({
            app_id: 'mis.expense',
            dataset_id: 'finance.expense_forms',
            object_role: 'expense_report',
            blueprint_id: 'mis.expense.approval',
        })));
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls.at(-1)?.[0]).toMatchObject({
            app_id: 'mis.expense',
            dataset_id: 'finance.expense_forms',
            object_role: 'expense_report',
            record_id: 'expense-restored-1',
            instance: expect.objectContaining({
                app_id: 'mis.expense',
                status: 'approved',
                current_node: 'expense_report.result_pack',
                current_node_ids: ['expense_report.result_pack'],
                business_status: 'finance_approved',
                result_status: 'approved',
                result_payload: expect.objectContaining({
                    approval_result: 'approved',
                    business_record: { id: 'expense-restored-1', status: 'finance_approved' },
                    approval_instance: expect.objectContaining({
                        current_node_ids: ['expense_report.result_pack', 'finance.archive'],
                    }),
                }),
                outputs: expect.arrayContaining([expect.objectContaining({ title: 'Restored expense record' })]),
                artifacts: expect.arrayContaining([expect.objectContaining({ name: 'restored-approval.pdf' })]),
            }),
        });
        await waitFor(() => {
            const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
            expect(history['datasrv-installed-mis.expense']?.[0]).toMatchObject({
                status: 'done',
                outputMode: 'approval',
                message: 'approved',
                approvalInstance: expect.objectContaining({
                    status: 'approved',
                    approvalID: 'approval-restored-final',
                    workflowSkillId: 'expense-workflow',
                    workflowVersion: '2.1.0',
                    approvalEvent: 'expense.submitted',
                    datasetID: 'finance.expense_forms',
                    objectRole: 'expense_report',
                    blueprintID: 'mis.expense.approval',
                    currentNode: 'expense_report.result_pack',
                    currentNodeIDs: ['expense_report.result_pack'],
                    businessStatus: 'finance_approved',
                    resultStatus: 'approved',
                    recordID: 'expense-restored-1',
                    resultPayload: expect.objectContaining({
                        approval_result: 'approved',
                        business_record: { id: 'expense-restored-1', status: 'finance_approved' },
                        approval_instance: expect.objectContaining({
                            current_node_ids: ['expense_report.result_pack', 'finance.archive'],
                        }),
                    }),
                    outputs: expect.arrayContaining([expect.objectContaining({ title: 'Restored expense record' })]),
                    artifacts: expect.arrayContaining([expect.objectContaining({ name: 'restored-approval.pdf' })]),
                    progressInstances: expect.arrayContaining([expect.objectContaining({
                        currentNode: 'finance.director_review',
                        resultStatus: 'running',
                        businessStatus: 'finance_pending',
                    })]),
                    approvalInstanceViewVerified: true,
                }),
            });
        });
    });

    it('restores DataSrv reopened install evidence into runtime layout and evidence panels', async () => {
        getMISDataConfigMock.mockResolvedValue({ enabled: true, endpoint: 'http://datasrv.test', token: 'token' });
        const fetchMock = vi.fn().mockResolvedValue({
            ok: true,
            json: async () => ({
                service: 'MaClawDataSrv',
                domains: ['finance'],
                app_installations: [{
                    app_id: 'mis.reopen.approval',
                    blueprint_id: 'mis.reopen.approval.blueprint',
                    name: 'Reopened Approval App',
                    version: '7',
                    kind: 'enterprise_approval_app',
                    source: 'hub',
                    updated_at: '2026-07-01T09:30:00Z',
                    role_bindings: [{ object_role: 'expense_report', domain: 'finance', dataset_id: 'finance.expenses', required: true }],
                    metadata: {
                        app_skill_id: 'reopen-approval-app-skill',
                        app_skill_version: '7.0.0',
                        workflow_skill_ids: ['reopen-approval-workflow'],
                        workflow_skill_versions: ['reopen-approval-workflow@3.0.0'],
                        approval_binding_versions: ['expense.submitted:reopen-approval-workflow@3.0.0'],
                        workflow_contract: {
                            schema: 'maclaw.app.workflow_contract.v1',
                            workflowSkillId: 'reopen-approval-workflow',
                            workflowVersion: '3.0.0',
                            objectRole: 'expense_report',
                        },
                        result_contract: {
                            schema: 'maclaw.app.result.v1',
                            primary: 'approval_result',
                            types: ['approval_result', 'document', 'inline_content'],
                        },
                        install_evidence: {
                            apps: [{ id: 'mis.reopen.approval', name: 'Reopened Approval App', kind: 'enterprise_approval_app' }],
                            workspace_layout: {
                                schema: 'maclaw.app.ui.v1',
                                entry: 'approval_workspace',
                                template: 'dashboard',
                                density: 'compact',
                                primaryRegion: 'center',
                                outputRegion: 'bottom',
                                fingerprint: 'layout-reopened-install',
                                navigation: ['my_requests', 'pending_my_approval', 'handled', 'attention'],
                                list: { columns: ['title', 'applicant', 'current_node', 'status'] },
                                regions: [
                                    { id: 'request_form', role: 'input', placement: 'center' },
                                    { id: 'approval_inbox', role: 'instance_list', placement: 'left' },
                                    { id: 'result_panel', role: 'output', placement: 'bottom' },
                                ],
                            },
                            test_evidence: {
                                run_id: 'run-reopen-install',
                                verified_at: '2026-07-01T09:20:00Z',
                                definition_fingerprint: 'sha256:reopen-install',
                                test_protocol: {
                                    schema: 'maclaw.app.test_protocol.v1',
                                    fingerprint: 'proto-reopen',
                                    sample_input: { record_ref: 'expense-77' },
                                    expected_output: { approval_result: 'approved' },
                                    required_roles: ['applicant', 'approver'],
                                    required_scopes: ['app.run'],
                                    risk_level: 'medium',
                                },
                                test_protocol_fingerprint: 'proto-reopen',
                                primary_result: 'approval_result',
                                result_payload: { approval_result: 'approved', business_status: 'finance_approved' },
                                outputs: [{ kind: 'inline_content', title: 'Approval summary', text: 'approved from reopened install evidence', status: 'ready' }],
                                artifacts: [{ id: 'reopen-approval-pdf', name: 'reopened-approval.pdf', uri: 'artifact://reopen/approval.pdf', type: 'document', status: 'ready' }],
                                approval_instance: {
                                    approval_id: 'approval-reopen-install',
                                    workflow_instance_id: 'wf-reopen-install',
                                    record_id: 'expense-77',
                                    status: 'approved',
                                    current_node: 'expense.result',
                                    approval_instance_view_verified: true,
                                },
                                result_coverage: { ok: true, primary: 'approval_result', covered_types: ['approval_result', 'document', 'inline_content'], missing_types: [] },
                            },
                            dependency_verification: {
                                schema: 'maclaw.app.install_plan.v1',
                                verified_at: '2026-07-01T09:10:00Z',
                                dependency_count: 1,
                                has_missing_required: false,
                                has_blocking_dependency: false,
                                dependencies: [{ id: 'reopen-approval-workflow', version: '3.0.0', kind: 'workflow_skill', source: 'hub', installed: true, health: 'ready', install_ref: 'hub://skills/reopen-approval-workflow@3.0.0' }],
                            },
                        },
                    },
                }],
            }),
        });
        vi.stubGlobal('fetch', fetchMock);

        render(<AppsPage lang="en" />);
        fireEvent.click(getStudioButton());

        await waitFor(() => expect(screen.getByText('Reopened Approval App')).not.toBeNull());
        fireEvent.click(screen.getByText('Add to panel'));
        fireEvent.click(screen.getAllByText('Close')[0]);

        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        const added = stored.customApps.find((app: any) => app.name === 'Reopened Approval App');
        expect(added.manifest.ui.layouts.approval_workspace).toMatchObject({
            template: 'dashboard',
            density: 'compact',
            primaryRegion: 'center',
            outputRegion: 'bottom',
            navigation: ['my_requests', 'pending_my_approval', 'handled', 'attention'],
        });
        expect(added.manifest.ui.layouts.approval_workspace.regions).toEqual([
            { id: 'request_form', role: 'input', placement: 'center' },
            { id: 'approval_inbox', role: 'instance_list', placement: 'left' },
            { id: 'result_panel', role: 'output', placement: 'bottom' },
        ]);
        expect(added.importedRunEvidence).toMatchObject({
            runID: 'run-reopen-install',
            definitionHash: 'sha256:reopen-install',
            testProtocolFingerprint: 'proto-reopen',
            artifacts: [{ id: 'reopen-approval-pdf', name: 'reopened-approval.pdf', uri: 'artifact://reopen/approval.pdf', type: 'document', status: 'ready' }],
            dependencyVerification: {
                dependencyCount: 1,
                hasBlockingDependency: false,
                dependencies: [{ id: 'reopen-approval-workflow', install_ref: 'hub://skills/reopen-approval-workflow@3.0.0', installed: true, health: 'ready' }],
            },
        });
        expect(added.installEvidence).toMatchObject({
            workspace_layout: { entry: 'approval_workspace', template: 'dashboard', density: 'compact', fingerprint: 'layout-reopened-install' },
            test_evidence: { run_id: 'run-reopen-install', definition_fingerprint: 'sha256:reopen-install', test_protocol_fingerprint: 'proto-reopen' },
            dependency_verification: { dependency_count: 1, has_blocking_dependency: false },
        });

        fireEvent.click(screen.getByText('Reopened Approval App'));
        const runtimeLayout = await waitFor(() => {
            const node = document.querySelector('.apps-runtime-layout') as HTMLElement | null;
            expect(node).not.toBeNull();
            return node as HTMLElement;
        });
        expect(runtimeLayout.dataset.template).toBe('dashboard');
        expect(runtimeLayout.dataset.density).toBe('compact');
        expect(runtimeLayout.dataset.primaryRegion).toBe('center');
        expect(runtimeLayout.dataset.outputRegion).toBe('bottom');
        expect(runtimeLayout.dataset.regionCount).toBe('3');
        expect((runtimeLayout.querySelector('.apps-runtime-input') as HTMLElement).dataset.region).toBe('center');
        expect((runtimeLayout.querySelector('.apps-approval-workspace') as HTMLElement).dataset.region).toBe('left');
        expect((runtimeLayout.querySelector('.apps-runtime-output') as HTMLElement).dataset.region).toBe('bottom');

        const runtimeGovernance = await findRuntimeGovernancePanel();
        expect(within(runtimeGovernance).getByText('Workspace layout')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('approval_workspace · dashboard · compact')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Test evidence')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('run-reopen-install · proto-reopen')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Dependency verification')).not.toBeNull();
        expect(within(runtimeGovernance).getByText('Skill dependencies: 1 · Blocking deps: 0')).not.toBeNull();
    });

    it('continues a requires-input approval instance with supplemental input from the runtime workbench', async () => {
        const designedApprovalApp = { ...dynamicApprovalApp(), source: 'local', studioOrigin: 'app_studio' };
        seedDynamicAppsPanel([designedApprovalApp, ...testDynamicApps.filter((app) => app.id !== designedApprovalApp.id)]);
        const requiresInputInstance = {
            instance_id: 'wf-input-ui-1',
            app_id: 'expense',
            app_name: '报销申请',
            approval_id: 'approval-input-ui-1',
            record_approval_id: 'approval-input-ui-1',
            record_id: 'expense-record-input-1',
            dataset_id: 'finance.expenses',
            object_role: 'expense_report',
            title: 'Expense needs supplement',
            lane: 'my_requests',
            status: 'requires_input',
            current_node: 'expense.requester_supplement',
            current_node_ids: ['expense.submit', 'expense.requester_supplement'],
            workflow_node_ids: ['expense.submit', 'expense.requester_supplement'],
            owner: 'Current user',
            applicant: 'Current user',
            approver: 'Manager',
            current_assignee: 'Current user',
            current_assignee_type: 'user',
            workflow_skill_id: 'expense-approval-workflow',
            workflow_version: '1.0.0',
            approval_event: 'expense.submitted',
            result: 'missing payment screenshot',
            business_status: 'waiting_for_requester',
            result_status: 'requires_input',
            from_status: 'approval_pending',
            to_status: 'requires_input',
            result_payload: {
                approval_result: 'requires_input',
                text: 'missing payment screenshot',
                requires_input: { fields: ['invoice_attachment'], message: 'missing payment screenshot' },
            },
            outputs: [{ id: 'out-input', kind: 'requires_input', title: 'Missing materials', text: 'missing payment screenshot', status: 'waiting' }],
            updated_at: '2026-06-30T09:00:00Z',
            events: [{ node: 'expense.requester_supplement', action: 'requires_input', message: 'missing payment screenshot' }],
        };
        listMaclawAppApprovalInstancesMock.mockResolvedValue([requiresInputInstance]);
        startMaclawAppApprovalWorkflowMock.mockResolvedValue({
            started: true,
            approval_id: 'approval-input-ui-1',
            instance: { ...requiresInputInstance, status: 'pending', result_status: 'pending', business_status: 'supplemented' },
            workflow_run: {
                ran: true,
                workflow_skill_id: 'expense-approval-workflow',
                progress_instances: [{ ...requiresInputInstance, status: 'running', result_status: 'running', current_node: 'expense.manager_review' }],
                instance: { ...requiresInputInstance, status: 'approved', lane: 'handled', result_status: 'approved', business_status: 'approved', result: 'approved after supplement' },
            },
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'supplement-publish-submission', submitted_at: '2026-06-30T09:15:00Z', status: 'submitted', message: 'queued' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };
        const dependencyEvidence = testDependencyVerificationForApp(designedApprovalApp);
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: designedApprovalApp.id, name: designedApprovalApp.name, kind: designedApprovalApp.kind }],
            dependencies: dependencyEvidence.dependencies,
            dependency_count: dependencyEvidence.dependencies.length,
            has_missing_required: false,
            has_blocking_dependency: false,
            has_workflow_contract_issue: false,
            workflow_contract_issue_count: 0,
            has_governance_review_issue: false,
            governance_review_issue_count: 0,
        });

        render(<AppsPage lang="en" />);
        fireEvent.click(screen.getByRole('button', { name: /报销申请/ }));

        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalled());
        await waitFor(() => expect(screen.getByText('Missing materials')).not.toBeNull());
        expect(screen.getByText('invoice_attachment')).not.toBeNull();
        const continueButton = screen.getByRole('button', { name: 'Continue with supplement' }) as HTMLButtonElement;
        expect(continueButton.disabled).toBe(true);
        fireEvent.change(screen.getByPlaceholderText('Describe what was added'), { target: { value: 'uploaded signed invoice' } });
        expect(continueButton.disabled).toBe(false);
        fireEvent.change(screen.getByPlaceholderText('artifact://...'), { target: { value: 'artifact://expense/invoice.pdf' } });
        fireEvent.click(continueButton);

        await waitFor(() => expect(startMaclawAppApprovalWorkflowMock).toHaveBeenCalled());
        const payload = startMaclawAppApprovalWorkflowMock.mock.calls.at(-1)?.[0];
        expect(payload).toMatchObject({
            app_id: 'expense',
            approval_id: 'approval-input-ui-1',
            instance_id: 'wf-input-ui-1',
            continue_from_instance_id: 'wf-input-ui-1',
            record_id: 'expense-record-input-1',
            business_action: 'supplement',
            run_workflow_skill: true,
        });
        expect(payload.form_data).toMatchObject({ supplement_note: 'uploaded signed invoice', supplement_reference: 'artifact://expense/invoice.pdf' });
        expect(payload.business_payload).toMatchObject({ approval_id: 'approval-input-ui-1', continue_from_instance_id: 'wf-input-ui-1', supplement_reference: 'artifact://expense/invoice.pdf' });
        expect(payload.result_payload.supplemental_input.form_data).toMatchObject({ supplement_note: 'uploaded signed invoice' });
        expect(payload.workflow_run_args.supplemental_input.business_payload).toMatchObject({ continue_from_instance_id: 'wf-input-ui-1' });
        await waitFor(() => {
            const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
            expect(history.expense?.[0]).toMatchObject({
                status: 'done',
                outputMode: 'approval',
                approvalInstance: {
                    instanceId: 'wf-input-ui-1',
                    approvalID: 'approval-input-ui-1',
                    status: 'approved',
                    resultStatus: 'approved',
                    progressInstances: [expect.objectContaining({ status: 'running', currentNode: 'expense.manager_review' })],
                },
            });
            expect(history.expense?.[0]?.resultPayload?.supplemental_input?.form_data).toMatchObject({ supplement_note: 'uploaded signed invoice' });
        });

        fireEvent.click(getStudioButton());
        fireEvent.click(getPublishTab());
        const publishCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('报销申请')) as HTMLElement;
        expect(publishCard).toBeTruthy();
        await waitFor(() => expect(within(publishCard).getByText('Ready to submit')).not.toBeNull());
        await waitFor(() => expect(within(publishCard).getByText('One-click publish')).not.toBeNull());
        fireEvent.click(within(publishCard).getByText('One-click publish'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const publishPayload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        const packagedApp = publishPayload.apps[0].app;
        const evidence = packagedApp.governance.testEvidence;
        expect(packagedApp.binding.datasrv).toMatchObject({
            domain: 'finance',
            datasetID: 'finance.expenses',
            objectRole: 'expense_report',
            blueprintID: 'finance.expense.approval',
        });
        expect(packagedApp.binding.mis.approvalBindings[0]).toMatchObject({
            event: 'expense.submitted',
            workflowSkillId: 'expense-approval-workflow',
            workflowVersion: '1.0.0',
            objectRole: 'expense_report',
        });
        expect(evidence.runId).toBe('approval-input-ui-1');
        expect(evidence.definitionHash).toMatch(/^[0-9a-f]{8}$/);
        expect(evidence.resultPayload.supplemental_input.form_data).toMatchObject({
            supplement_note: 'uploaded signed invoice',
            supplement_reference: 'artifact://expense/invoice.pdf',
        });
        expect(evidence.approvalInstance).toMatchObject({
            instanceId: 'wf-input-ui-1',
            approvalID: 'approval-input-ui-1',
            status: 'approved',
            resultStatus: 'approved',
            workflowSkillId: 'expense-approval-workflow',
            workflowVersion: '1.0.0',
            approvalEvent: 'expense.submitted',
            recordID: 'expense-record-input-1',
            approvalInstanceViewVerified: true,
            resultPayload: expect.objectContaining({
                supplemental_input: expect.objectContaining({
                    form_data: expect.objectContaining({ supplement_note: 'uploaded signed invoice' }),
                    business_payload: expect.objectContaining({ continue_from_instance_id: 'wf-input-ui-1' }),
                }),
            }),
            progressInstances: [expect.objectContaining({ status: 'running', currentNode: 'expense.manager_review' })],
        });
        expect(evidence.resultCoverage).toEqual(expect.objectContaining({
            ok: true,
            primary: 'approval_result',
            missingTypes: [],
        }));
    });
    it('blocks DataSrv installed MaClaw apps at runtime when dependency verification fails', async () => {
        getMISDataConfigMock.mockResolvedValue({ enabled: true, endpoint: 'http://datasrv.test', token: 'token' });
        planMaclawAppInstallMock.mockResolvedValue({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'mis.blocked', name: 'Blocked DataSrv Approval', kind: 'enterprise_approval_app' }],
            dependencies: [{ id: 'blocked-workflow', version: '9.9.9', kind: 'workflow_skill', required: true, source: 'hub', installed: false, health: 'missing', action: 'blocked', app_ids: ['mis.blocked'] }],
            has_missing_required: true,
            has_blocking_dependency: true,
        });
        const fetchMock = vi.fn().mockResolvedValue({
            ok: true,
            json: async () => ({
                service: 'MaClawDataSrv',
                domains: ['finance'],
                app_installations: [{
                    app_id: 'mis.blocked',
                    blueprint_id: 'mis.blocked.approval',
                    name: 'Blocked DataSrv Approval',
                    version: '1',
                    kind: 'enterprise_approval_app',
                    role_bindings: [{ object_role: 'expense_report', domain: 'finance', dataset_id: 'finance.expense_forms', required: true }],
                    metadata: {
                        workflow_skill_ids: ['blocked-workflow'],
                        workflow_skill_versions: ['blocked-workflow@9.9.9'],
                        approval_binding_versions: ['expense.submitted:blocked-workflow@9.9.9'],
                        dependencies: [{ id: 'blocked-workflow', kind: 'workflow_skill', required: true, source: 'hub' }],
                    },
                }],
            }),
        });
        vi.stubGlobal('fetch', fetchMock);

        render(<AppsPage lang="en" />);
        fireEvent.click(getStudioButton());

        await waitFor(() => expect(screen.getByText('Blocked DataSrv Approval')).not.toBeNull());
        fireEvent.click(screen.getByText('Add to panel'));
        fireEvent.click(screen.getAllByText('Close')[0]);
        fireEvent.click(screen.getByText('Blocked DataSrv Approval'));

        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalled());
        const blockedPlanPayload = JSON.parse(planMaclawAppInstallMock.mock.calls.at(-1)?.[0] as string);
        expect(blockedPlanPayload.app.id).toBe('mis.blocked');
        await waitFor(() => expect(screen.getByText(/Blocked DataSrv Approval is unavailable: blocked-workflow is not installed\./)).not.toBeNull());
        fireEvent.click(screen.getByText('Run'));

        expect(runNLSkillAsyncMock).not.toHaveBeenCalled();
        expect(screen.getByText(/Blocked DataSrv Approval is unavailable: blocked-workflow is not installed\./)).not.toBeNull();
    });
    it('turns skill maclaw.apps.json entries into registered tool apps', async () => {
        listSkillAppManifestsMock.mockResolvedValue([
            { id: 'redact', skill_id: 'doc-tools', name: '文档脱敏 Plus', description: 'Redact files', category: '文档处理', icon: 'shield', input_mode: 'file' },
        ]);

        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(getStudioButton());

        await waitFor(() => expect(screen.getByText('文档脱敏 Plus')).not.toBeNull());
        await waitFor(() => expect(screen.getByText('\u5df2\u52a0\u5165\u9762\u677f')).not.toBeNull());
        expect(screen.queryByText('\u52a0\u5230\u9762\u677f')).toBeNull();
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        expect(screen.getAllByText('文档脱敏 Plus').length).toBeGreaterThan(0);
    });

    it('runs skill maclaw.app.json wrappers with the installed skill id, not the bound dependency id', async () => {
        listSkillAppManifestsMock.mockResolvedValue([
            {
                id: 'rapidocr-app-tool-app',
                skill_id: 'rapidocr-wrapper',
                name: '图片文字识别',
                description: 'OCR wrapper app',
                category: '工具',
                icon: 'pdf',
                input_mode: 'form',
                output_modes: ['txt', 'json'],
                app_definition_file: 'maclaw.app.json',
                app_definition: {
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: {
                        id: 'rapidocr-app-tool-app',
                        name: '图片文字识别',
                        kind: 'tool_app',
                        binding: {
                            skill: {
                                id: 'RapidOCR',
                                source: 'local',
                                inputMode: 'form',
                                outputModes: ['txt', 'json'],
                            },
                        },
                    },
                },
            },
        ]);

        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(getStudioButton());

        await waitFor(() => expect(screen.getByText('图片文字识别')).not.toBeNull());
        await waitFor(() => expect(screen.getByText('\u5df2\u52a0\u5165\u9762\u677f')).not.toBeNull());
        fireEvent.click(screen.getByText('\u5173\u95ed'));
        fireEvent.click(screen.getAllByText('图片文字识别')[0]);
        fireEvent.click(screen.getByText('\u6267\u884c'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('rapidocr-wrapper', expect.objectContaining({
            app_id: 'skill-app-rapidocr-wrapper-rapidocr-app-tool-app',
            app_kind: 'tool_app',
        })));
        expect(runNLSkillAsyncMock).not.toHaveBeenCalledWith('RapidOCR', expect.anything());
        const planPayload = JSON.parse(planMaclawAppInstallMock.mock.calls.at(-1)?.[0] as string);
        expect(planPayload.app.appSkill.id).toBe('rapidocr-wrapper');
        expect(planPayload.app.binding.skill.id).toBe('RapidOCR');
        expect(planPayload.app.binding.skill.source).toBe('local');
        await waitFor(() => expect(recordMaclawAppRunEvidenceForSkillMock).toHaveBeenCalledWith(
            'rapidocr-wrapper',
            'skill-app-rapidocr-wrapper-rapidocr-app-tool-app',
            expect.any(String),
            'run-test-1',
            expect.any(String),
            expect.any(String),
        ));
        expect(recordMaclawAppRunEvidenceForSkillMock).not.toHaveBeenCalledWith('RapidOCR', expect.anything(), expect.anything(), expect.anything(), expect.anything(), expect.anything());
    });
});
