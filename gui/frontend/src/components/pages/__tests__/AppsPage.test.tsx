import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const executeMaclawAppBusinessOperationMock = vi.hoisted(() => vi.fn());
const getMISDataConfigMock = vi.hoisted(() => vi.fn());
const listNLSkillsMock = vi.hoisted(() => vi.fn());
const listSkillAppManifestsMock = vi.hoisted(() => vi.fn());
const listMaclawAppInstallsMock = vi.hoisted(() => vi.fn());
const listMaclawAppApprovalInstancesMock = vi.hoisted(() => vi.fn());
const listMaclawAppApprovalInstancesAllMock = vi.hoisted(() => vi.fn());
const recordMaclawAppApprovalInstanceMock = vi.hoisted(() => vi.fn());
const syncMaclawAppApprovalInstanceToDataSrvMock = vi.hoisted(() => vi.fn());
const installMaclawAppDependenciesMock = vi.hoisted(() => vi.fn());
const installMaclawAppPackageFromHubMock = vi.hoisted(() => vi.fn());
const installSelectedMaclawAppPackageFromHubMock = vi.hoisted(() => vi.fn());
const recordMaclawAppInstallMock = vi.hoisted(() => vi.fn());
const planMaclawAppInstallMock = vi.hoisted(() => vi.fn());
const saveMaclawAppDefinitionForSkillMock = vi.hoisted(() => vi.fn());
const recordMaclawAppRunEvidenceForSkillMock = vi.hoisted(() => vi.fn());
const uploadNLSkillToMarketMock = vi.hoisted(() => vi.fn());
const searchMixedSkillsMock = vi.hoisted(() => vi.fn());
const loadConfigMock = vi.hoisted(() => vi.fn());
const browserOpenURLMock = vi.hoisted(() => vi.fn());
const runNLSkillAsyncMock = vi.hoisted(() => vi.fn());
const getNLSkillRunStatusMock = vi.hoisted(() => vi.fn());
const cancelNLSkillRunMock = vi.hoisted(() => vi.fn());
const stageSkillAppInputFileMock = vi.hoisted(() => vi.fn());
const openFileOrShowInFolderMock = vi.hoisted(() => vi.fn());
const downloadSkillRunArtifactMock = vi.hoisted(() => vi.fn());
const openSkillRunArtifactMock = vi.hoisted(() => vi.fn());
const revealSkillRunArtifactMock = vi.hoisted(() => vi.fn());
const showItemInFolderMock = vi.hoisted(() => vi.fn());

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CancelNLSkillRun: (...args: unknown[]) => cancelNLSkillRunMock(...args),
    ExecuteMaclawAppBusinessOperation: (...args: unknown[]) => executeMaclawAppBusinessOperationMock(...args),
    GetMISDataConfig: (...args: unknown[]) => getMISDataConfigMock(...args),
    GetNLSkillRunStatus: (...args: unknown[]) => getNLSkillRunStatusMock(...args),
    ListNLSkills: (...args: unknown[]) => listNLSkillsMock(...args),
    ListSkillAppManifests: (...args: unknown[]) => listSkillAppManifestsMock(...args),
    LoadConfig: (...args: unknown[]) => loadConfigMock(...args),
    ListMaclawAppInstalls: (...args: unknown[]) => listMaclawAppInstallsMock(...args),
    ListMaclawAppApprovalInstances: (...args: unknown[]) => listMaclawAppApprovalInstancesMock(...args),
    ListMaclawAppApprovalInstancesAll: (...args: unknown[]) => listMaclawAppApprovalInstancesAllMock(...args),
    RecordMaclawAppApprovalInstance: (...args: unknown[]) => recordMaclawAppApprovalInstanceMock(...args),
    SyncMaclawAppApprovalInstanceToDataSrv: (...args: unknown[]) => syncMaclawAppApprovalInstanceToDataSrvMock(...args),
    DownloadSkillRunArtifact: (...args: unknown[]) => downloadSkillRunArtifactMock(...args),
    OpenFileOrShowInFolder: (...args: unknown[]) => openFileOrShowInFolderMock(...args),
    InstallMaclawAppDependencies: (...args: unknown[]) => installMaclawAppDependenciesMock(...args),
    InstallMaclawAppPackageFromHub: (...args: unknown[]) => installMaclawAppPackageFromHubMock(...args),
    InstallSelectedMaclawAppPackageFromHub: (...args: unknown[]) => installSelectedMaclawAppPackageFromHubMock(...args),
    PlanMaclawAppInstall: (...args: unknown[]) => planMaclawAppInstallMock(...args),
    RecordMaclawAppInstall: (...args: unknown[]) => recordMaclawAppInstallMock(...args),
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

const marketManifestPlaceholder = '粘贴应用包 JSON（maclaw.app.v1 / maclaw.app.pack.v1 / maclaw.apps.json）';
const runHistoryStorageKey = 'maclaw:apps-run-history:v1';

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

function normalizeTestAppVersion(value: unknown) {
    const version = Number(value);
    return Number.isFinite(version) && version > 0 ? Math.floor(version) : 1;
}

function testAppDefinitionFingerprint(app: any): string {
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
        ...(app.customIconDataUrl ? { customIconDataUrl: app.customIconDataUrl } : {}),
        version: normalizeTestAppVersion(app.version),
        manifest: runtimeManifest,
    }));
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
    dependencyVerification?: any;
    approvalInstance?: any;
};

function seedSuccessfulLocalAppRun(app: any, options: SeedRunOptions = {}) {
    const artifacts = options.artifacts || [];
    const primaryArtifact = artifacts[0] || {};
    const raw = window.localStorage.getItem(runHistoryStorageKey) || '{}';
    const history = JSON.parse(raw) as Record<string, any[]>;
    history[app.id] = [{
        runID: options.runID || `run-ok-${app.id}`,
        appID: app.id,
        status: 'done',
        definitionHash: testAppDefinitionFingerprint(app),
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
        dependencyVerification: options.dependencyVerification,
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
    fireEvent.click(screen.getByTitle('应用程序工作室'));
    fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: name } });
    fireEvent.click(screen.getAllByText('创建应用')[1]);
    fireEvent.click(screen.getAllByText(name)[0]);
    const fileInput = document.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
    const file = new File(['demo'], 'sample.pdf', { type: 'application/pdf' });
    fireEvent.change(fileInput, { target: { files: [file] } });
    fireEvent.click(screen.getByText('执行'));
    await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalled());
    await waitFor(() => {
        const raw = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
        const entries = Object.values(raw).flat();
        expect(entries.some((entry) => entry.runID === 'run-test-1' && entry.status === 'done')).toBe(true);
    });
}
function seedSuccessfulSkillAppRun(skillID = 'invoice-review', name = '发票审核') {

    const appID = `skill-app-${skillID}-app-tool-app`;
    const definitionHash = textHash(stableStringify({
        name,
        description: '由应用程序工作室创建的应用入口。',
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
            skill: {
                id: skillID,
                appDefinitionFile: 'maclaw.app.json',
                inputMode: 'file',
                multipleFiles: false,
                outputModes: ['docx', 'pdf'],
                fields: [],
            },
        },
    }));
    window.localStorage.setItem(runHistoryStorageKey, JSON.stringify({
        [appID]: [{
            runID: 'run-ok-1',
            appID,
            status: 'done',
            definitionHash,
            outputMode: 'pdf',
            inputSummary: 'sample.pdf',
            message: 'done',
            at: new Date().toISOString(),
        }],
    }));
}

describe('AppsPage', () => {
    beforeEach(() => {
        window.localStorage.clear();
        executeMaclawAppBusinessOperationMock.mockReset().mockResolvedValue({ synced: true, mode: 'business_action', target: 'datasrv.action', result_status: 'done', response: { status: 'done' } });
        getMISDataConfigMock.mockReset().mockResolvedValue({ enabled: false, endpoint: 'http://127.0.0.1:18180' });
        listNLSkillsMock.mockReset().mockResolvedValue([]);
        listSkillAppManifestsMock.mockReset().mockResolvedValue([]);
        listMaclawAppInstallsMock.mockReset().mockResolvedValue([]);
        listMaclawAppApprovalInstancesMock.mockReset().mockResolvedValue([]);
        listMaclawAppApprovalInstancesAllMock.mockReset().mockResolvedValue([]);
        recordMaclawAppApprovalInstanceMock.mockReset().mockImplementation(async (payload) => ({ ...payload, instance_id: 'appr-test-1', updated_at: '2026-06-19T00:00:00Z' }));
        syncMaclawAppApprovalInstanceToDataSrvMock.mockReset().mockResolvedValue({ synced: true });
        installMaclawAppDependenciesMock.mockReset().mockResolvedValue({ schema: 'maclaw.app.install_plan.v1', apps: [], dependencies: [], has_missing_required: false });
        installMaclawAppPackageFromHubMock.mockReset().mockResolvedValue({});
        installSelectedMaclawAppPackageFromHubMock.mockReset().mockResolvedValue({});
        recordMaclawAppInstallMock.mockReset().mockResolvedValue({ schema: 'maclaw.app.installs.v1', app_count: 1 });
        planMaclawAppInstallMock.mockReset().mockResolvedValue({ schema: 'maclaw.app.install_plan.v1', apps: [], dependencies: [], has_missing_required: false });
        saveMaclawAppDefinitionForSkillMock.mockReset().mockResolvedValue({ app_definition_file: 'maclaw.app.json' });
        recordMaclawAppRunEvidenceForSkillMock.mockReset().mockResolvedValue({ app_definition_file: 'maclaw.app.json' });
        uploadNLSkillToMarketMock.mockReset().mockResolvedValue('submission-app-1');
        searchMixedSkillsMock.mockReset().mockResolvedValue([]);
        loadConfigMock.mockReset().mockResolvedValue({ remote_hub_url: 'https://hub.example.com', remote_machine_id: 'machine-1', remote_machine_token: 'token-1' });
        browserOpenURLMock.mockReset();
        runNLSkillAsyncMock.mockReset().mockResolvedValue('run-test-1');
        getNLSkillRunStatusMock.mockReset().mockResolvedValue({ run_id: 'run-test-1', status: 'success', summary: { last_output_snippet: 'done' } });
        cancelNLSkillRunMock.mockReset().mockResolvedValue(undefined);
        openFileOrShowInFolderMock.mockReset().mockResolvedValue(undefined);
        downloadSkillRunArtifactMock.mockReset().mockResolvedValue({ available: true });
        openSkillRunArtifactMock.mockReset().mockResolvedValue(undefined);
        revealSkillRunArtifactMock.mockReset().mockResolvedValue(undefined);
        showItemInFolderMock.mockReset().mockResolvedValue(undefined);
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

        expect(screen.getByPlaceholderText('搜索应用')).not.toBeNull();
        const studioEntry = screen.getByTitle('应用程序工作室');
        expect(studioEntry).not.toBeNull();
        expect(studioEntry.textContent).toBe('');
        expect(container.querySelector('.apps-studio-button__icon svg:not(.apps-studio-button__plus)')).not.toBeNull();
        expect(container.querySelector('.apps-studio-button__plus')).not.toBeNull();
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('文档处理 (2)')).not.toBeNull();
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('全部应用 (10)')).not.toBeNull();
        expect(screen.getAllByText('常用应用').length).toBeGreaterThan(0);
        expect(container.querySelectorAll('.apps-app-tile').length).toBeGreaterThan(6);
    });

    it('renders the app panel operation section without changing app category counts', () => {
        render(<AppsPage lang="zh-Hans" />);

        expect(screen.getByText('\u64cd\u4f5c')).not.toBeNull();
        expect(screen.getByText('\u5ba1\u6279\u72b6\u6001')).not.toBeNull();
        expect(screen.getByText('\u8fd0\u884c\u8bb0\u5f55')).not.toBeNull();
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('\u5168\u90e8\u5e94\u7528 (10)')).not.toBeNull();
    });
    it('opens global approval management from the operation section', async () => {
        listMaclawAppApprovalInstancesAllMock.mockResolvedValue([{
            app_id: 'expense',
            app_name: 'Expense approval',
            instance_id: 'approval-global-1',
            title: 'Travel expense',
            lane: 'pending_my_approval',
            status: 'pending',
            current_node: 'manager_approval',
            owner: 'alice',
            approver: 'manager',
            updated_at: '2026-06-20T00:00:00Z',
            result: 'waiting',
            workflow_skill_id: 'expense-approval-workflow',
            approval_workflow_id: 'expense_approval',
            current_assignee: 'manager',
            current_assignee_type: 'user',
            dataset_id: 'finance.expenses',
            object_role: 'expense_report',
            approval_id: 'approval-remote-global-1',
            record_id: 'EXP-1',
        }]);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByText('\u5ba1\u6279\u72b6\u6001'));

        await waitFor(() => expect(listMaclawAppApprovalInstancesAllMock).toHaveBeenCalledWith('all', 200));
        expect(screen.getByText('\u5ba1\u6279\u5b9e\u4f8b\u7ba1\u7406')).not.toBeNull();
        expect(screen.getAllByText('Travel expense').length).toBeGreaterThan(0);
        const detail = document.querySelector('.apps-approval-detail') as HTMLElement;
        expect(within(detail).getByText('结果契约')).not.toBeNull();
        expect(within(detail).getAllByText('approval_result').length).toBeGreaterThan(0);
        expect(screen.getByText('EXP-1')).not.toBeNull();
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
        expect(updatedTile.title).toContain('状态: 运行中');
        expect(updatedTile.getAttribute('aria-label')).toContain('状态: 运行中');
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));

        expect(screen.getAllByText('当前草稿 manifest').length).toBe(1);
        expect(screen.getAllByText(/x_maclaw_apps/).length).toBeGreaterThan(0);
        expect(screen.queryByText(/document-redaction/)).toBeNull();
    });

    it('hides the Skill apps discovery card when no Skill apps are found', async () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));

        await waitFor(() => expect(listSkillAppManifestsMock).toHaveBeenCalled());
        await waitFor(() => {
            expect(screen.queryByText('Found apps')).toBeNull();
            expect(screen.queryByText('Found from installed capabilities and synced to the app panel')).toBeNull();
        });
    });

    it('does not flash the Skill apps discovery card while scanning with no candidates yet', async () => {
        let finishDiscovery: (entries: unknown[]) => void = () => undefined;
        listSkillAppManifestsMock.mockReturnValue(new Promise((resolve) => {
            finishDiscovery = resolve;
        }));
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));

        await waitFor(() => expect(listSkillAppManifestsMock).toHaveBeenCalled());
        expect(screen.queryByText('Found apps')).toBeNull();
        expect(screen.queryByText('Found from installed capabilities and synced to the app panel')).toBeNull();
        finishDiscovery([]);
        await waitFor(() => expect(screen.queryByText('Found apps')).toBeNull());
    });

    it('shows a clear Skill app discovery error without claiming apps were synced', async () => {
        listSkillAppManifestsMock.mockRejectedValue(new Error('scan failed'));
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));

        await waitFor(() => expect(screen.getByText('Could not check installed capabilities')).not.toBeNull());
        expect(screen.getByText('scan failed')).not.toBeNull();
        expect(screen.queryByText('Found from installed capabilities and synced to the app panel')).toBeNull();
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

        fireEvent.click(screen.getByTitle('App Studio'));

        await waitFor(() => expect(screen.getByText('Found from installed capabilities and synced to the app panel')).not.toBeNull());
        expect(screen.getAllByText('Invoice Review').length).toBeGreaterThan(0);
        const skillCandidate = screen.getAllByText('Invoice Review')
            .map((item) => item.closest('.apps-discovery__candidate'))
            .find((item): item is HTMLElement => item instanceof HTMLElement);
        if (!skillCandidate) throw new Error('skill discovery candidate was not rendered');
        expect(within(skillCandidate).getByText('Finance')).not.toBeNull();
        expect(screen.queryByText('invoice-app')).toBeNull();
        expect(screen.getByText('1 app')).not.toBeNull();
        await waitFor(() => expect(screen.getByText('In panel')).not.toBeNull());
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

        fireEvent.click(screen.getByTitle('App Studio'));

        await waitFor(() => expect(screen.getByText('1 app')).not.toBeNull());
        expect(screen.getAllByText('Invoice Review').length).toBe(2);
        expect(screen.queryByText('Invoice Review Duplicate')).toBeNull();
    });

    it('saves a tool app definition into an existing skill', async () => {
        listNLSkillsMock.mockResolvedValue([
            { name: 'invoice-review', description: '审核发票' },
            { name: 'already-app', is_maclaw_app: true },
            { name: 'already-app-camel', isMaclawApp: true },
        ]);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        const toolSkillPicker = screen.getByTestId('studio-tool-skill-picker');
        await waitFor(() => expect(within(toolSkillPicker).getAllByText('invoice-review').length).toBeGreaterThan(0));
        expect(within(toolSkillPicker).queryByText('already-app')).toBeNull();
        expect(within(toolSkillPicker).queryByText('already-app-camel')).toBeNull();
        fireEvent.click(within(toolSkillPicker).getByRole('option', { name: /invoice-review/ }) as HTMLButtonElement);
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '发票审核' } });
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

    it('requires a successful current-version test before uploading a skill app', async () => {
        listNLSkillsMock.mockResolvedValue([{ name: 'invoice-review', description: '审核发票' }]);
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        const toolSkillPicker = screen.getByTestId('studio-tool-skill-picker');
        await waitFor(() => expect(within(toolSkillPicker).getAllByText('invoice-review').length).toBeGreaterThan(0));
        fireEvent.click(within(toolSkillPicker).getByRole('option', { name: /invoice-review/ }) as HTMLButtonElement);
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '发票审核' } });

        fireEvent.click(screen.getByText('上传到 SkillMarket'));

        await waitFor(() => expect(screen.getByText('请先保存到 Skill，并在应用面板成功测试一次当前版本，再上传到 SkillMarket。')).not.toBeNull());
        expect(saveMaclawAppDefinitionForSkillMock).not.toHaveBeenCalled();
        expect(uploadNLSkillToMarketMock).not.toHaveBeenCalled();
    });

    it('saves the latest tool app definition before uploading to SkillMarket', async () => {
        listNLSkillsMock.mockResolvedValue([{ name: 'invoice-review', description: '审核发票' }]);
        seedSuccessfulSkillAppRun();
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        const toolSkillPicker = screen.getByTestId('studio-tool-skill-picker');
        await waitFor(() => expect(within(toolSkillPicker).getAllByText('invoice-review').length).toBeGreaterThan(0));
        fireEvent.click(within(toolSkillPicker).getByRole('option', { name: /invoice-review/ }) as HTMLButtonElement);
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '发票审核' } });

        fireEvent.click(screen.getByText('上传到 SkillMarket'));

        await waitFor(() => expect(saveMaclawAppDefinitionForSkillMock).toHaveBeenCalledWith('invoice-review', expect.any(String)));
        await waitFor(() => expect(uploadNLSkillToMarketMock).toHaveBeenCalledWith('invoice-review'));
        const payload = JSON.parse(saveMaclawAppDefinitionForSkillMock.mock.calls[0][1]);
        expect(payload.app.name).toBe('发票审核');
        expect(payload.app.binding.skill.appDefinitionFile).toBe('maclaw.app.json');
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

        fireEvent.click(screen.getByTitle('App Studio'));
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

        fireEvent.click(screen.getByTitle('App Studio'));
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

        fireEvent.click(screen.getByTitle('App Studio'));
        const skillPicker = screen.getByTestId('studio-tool-skill-picker');
        await waitFor(() => expect(within(skillPicker).getAllByText('alpha-installed-skill').length).toBeGreaterThan(0));
        fireEvent.change(skillPicker.querySelector('input') as HTMLInputElement, { target: { value: 'alpha' } });
        fireEvent.click(within(skillPicker).getByRole('button', { name: /^Search$/ }));

        const marketHeader = await within(skillPicker).findByText('SkillMarket / Hub');
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

        fireEvent.click(screen.getByTitle('App Studio'));
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
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));

        const createTab = screen.getByRole('tab', { name: '创建应用' });
        expect(createTab.getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe(createTab.id);

        fireEvent.keyDown(createTab, { key: 'ArrowRight' });

        const manageTab = screen.getByRole('tab', { name: '应用管理' });
        expect(manageTab.getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('tabpanel').getAttribute('id')).toBe(manageTab.getAttribute('aria-controls'));

        fireEvent.keyDown(manageTab, { key: 'End' });
        const publishTab = screen.getByRole('tab', { name: '审核/发布' });
        expect(publishTab.getAttribute('aria-selected')).toBe('true');
        expect(screen.getByText('暂无本地应用可发布')).not.toBeNull();
    });

    it('shows local apps in the review and publish checklist', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '合同归档' } });
        fireEvent.click(screen.getAllByText('创建应用')[1]);
        fireEvent.click(screen.getByText('审核/发布'));

        expect(screen.getByText('发布检查')).not.toBeNull();
        expect(screen.getAllByText('合同归档').length).toBeGreaterThan(0);
        expect(screen.getByText('需补齐')).not.toBeNull();
        expect(screen.getByText('Manifest 结构')).not.toBeNull();
        expect(screen.getByText('绑定能力')).not.toBeNull();
        expect(screen.getByText('运行证据')).not.toBeNull();
        expect(screen.getByText('提交包预览')).not.toBeNull();
        expect(screen.getByText(/maclaw.app.pack.v1/)).not.toBeNull();
        expect(screen.getByText(/"governance"/)).not.toBeNull();
        expect(screen.getByText(/"dependencies"/)).not.toBeNull();
        expect(screen.getByText('Workspace layout')).not.toBeNull();
        expect(screen.getByText(/"workspaceLayout"/)).not.toBeNull();
        expect(screen.getByText(/"entry": "tool_workspace"/)).not.toBeNull();
        expect(screen.getByText('结果契约')).not.toBeNull();
        expect(screen.getByText(/"resultContract"/)).not.toBeNull();
        expect(screen.getByText(/"schema": "maclaw.app.result.v1"/)).not.toBeNull();
        expect(screen.getByText(/"testProtocol"/)).not.toBeNull();
        expect(screen.getByText(/"schema": "maclaw.app.test_protocol.v1"/)).not.toBeNull();
        expect(screen.getByText(/"primary": "artifact"/)).not.toBeNull();
        expect(screen.getByText(/"installPolicy": "install_on_app_install"/)).not.toBeNull();
        expect(screen.getByText(/"requiredCount": 1/)).not.toBeNull();
        expect(screen.getByText(/"status": "draft"/)).not.toBeNull();
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Layout Missing Output App')) as HTMLElement;
        expect(within(card).getByText('Needs work')).not.toBeNull();
        expect(within(card).getByText(/Missing workspace region roles: output/)).not.toBeNull();
    });
    it('submits ready local apps into local review state', async () => {
        render(<AppsPage lang="zh-Hans" />);

        await createAndRunLocalToolApp('合同归档');
        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));

        expect(screen.getByText('可提交')).not.toBeNull();
        fireEvent.click(screen.getByText('提交审核'));

        await waitFor(() => expect(screen.getAllByText('本地待同步').length).toBeGreaterThan(0));
        expect(screen.getAllByText('已提交').length).toBeGreaterThan(0);
        expect(screen.getAllByText(/local-review-/).length).toBeGreaterThan(0);
        expect(screen.getByText(/"status": "submitted"/)).not.toBeNull();
        expect(screen.getByText(/"channel": "local"/)).not.toBeNull();
        expect(window.localStorage.getItem('maclaw:apps-publish-submissions:v1')).toContain('local-review-');

        fireEvent.click(screen.getByText('撤回提交'));
        await waitFor(() => expect(screen.getByText('可提交')).not.toBeNull());
        expect(screen.queryByText('等待企业市场审核')).toBeNull();
        expect(screen.getByText(/"status": "local_tested"/)).not.toBeNull();
        expect(window.localStorage.getItem('maclaw:apps-publish-submissions:v1')).not.toContain('local-review-');
    });

    it('shows returned review status and allows resubmission', async () => {
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
                skill: { id: 'contract-review', inputMode: 'mixed', multipleFiles: false, outputModes: ['docx', 'pdf'], fields: [] },
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
                id: 'local-review-returned',
                appID: reviewedApp.id,
                submittedAt: '2026-06-17T00:00:00.000Z',
                reviewedAt: '2026-06-17T00:10:00.000Z',
                status: 'review_failed',
                reviewer: 'market-reviewer',
                message: '请补充运行证据',
                reviewIssues: [{ path: 'apps[0].app.governance.testEvidence', severity: 'error', message: '缺少运行证据', suggestion: '先运行一次应用' }],
            },
        }));

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));

        expect(screen.getAllByText('审核需修改').length).toBeGreaterThan(0);
        expect(screen.getByText('请补充运行证据')).not.toBeNull();
        expect(screen.getByText('apps[0].app.governance.testEvidence')).not.toBeNull();
        expect(screen.getByText('缺少运行证据')).not.toBeNull();
        expect(screen.getByText('先运行一次应用')).not.toBeNull();
        expect(screen.getByText(/"status": "review_failed"/)).not.toBeNull();

        fireEvent.click(screen.getByText('提交审核'));
        await waitFor(() => expect(screen.getAllByText('本地待同步').length).toBeGreaterThan(0));
        expect(screen.getByText(/"status": "submitted"/)).not.toBeNull();
    });

    it('opens market import with the current app manifest when review dependency issues need repair', async () => {
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
                reviewIssues: [{ path: 'apps[0].app.governance.dependencyVerification', severity: 'error', message: 'dependency verification is stale', suggestion: '安装或重新检查依赖' }],
            },
        }));

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));
        fireEvent.click(screen.getByText('处理依赖'));

        const marketTab = screen.getByRole('tab', { name: '从市场添加' });
        expect(marketTab.getAttribute('aria-selected')).toBe('true');
        const textarea = screen.getByLabelText('安装应用包') as HTMLTextAreaElement;
        await waitFor(() => expect(textarea.value).toContain('maclaw.app.v1'));
        expect(textarea.value).toContain('local-app-review-dependency');
        expect(textarea.value).toContain('contract-review');
        expect((textarea.closest('details') as HTMLDetailsElement).open).toBe(true);
    });
    it('blocks app package review submission when required dependencies are unavailable', async () => {
        const app = {
            id: 'local-publish-blocked-dep',
            name: '依赖阻断应用',
            description: '用于验证发布依赖门禁',
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
                skill: { id: 'disabled-workflow', appDefinitionFile: 'maclaw.app.json', inputMode: 'file', multipleFiles: false, outputModes: ['pdf'], fields: [] },
                dependencies: { skills: [{ id: 'disabled-workflow', kind: 'runtime_skill', required: true, source: 'hub' }] },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [app.id],
            customApps: [app],
            recentUsedAtById: { [app.id]: app.recentUsedAt },
        }));
        seedSuccessfulLocalAppRun(app);
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'should-not-submit' });
        const listMaclawAppPackageSubmissions = vi.fn().mockResolvedValue([]);
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage, ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };
        planMaclawAppInstallMock.mockResolvedValueOnce({
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));
        await waitFor(() => expect(screen.getByText('暂无本机待同步提交')).not.toBeNull());
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('依赖阻断应用')) as HTMLElement;
        expect(card).toBeTruthy();
        fireEvent.click(within(card).getByText('提交审核'));

        await waitFor(() => expect(planMaclawAppInstallMock).toHaveBeenCalledTimes(1));
        expect(submitMaclawAppPackage).not.toHaveBeenCalled();
        expect(within(card).getByText('依赖检查失败')).not.toBeNull();
        expect(within(card).getByText('必需 Skill 依赖缺失或不可用，请先安装或启用依赖')).not.toBeNull();
        expect(within(card).queryByText('等待企业市场审核')).toBeNull();
    });

    it('uses the enterprise market bridge when submitting app packages', async () => {
        getNLSkillRunStatusMock.mockResolvedValue({
            run_id: 'run-test-1',
            status: 'success',
            summary: { last_output_snippet: 'done' },
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

        render(<AppsPage lang="zh-Hans" />);

        await createAndRunLocalToolApp('合同归档');
        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));
        fireEvent.click(screen.getByText('提交审核'));

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
            expect.objectContaining({ id: 'artifact-1', name: 'contract.docx', path: '/tmp/contract.docx' }),
            expect.objectContaining({ id: 'artifact-2', name: 'report.pdf', mimeType: 'application/pdf', sizeBytes: 2048 }),
        ]));
        expect(screen.getAllByText('等待企业市场审核').length).toBeGreaterThan(0);
        expect(screen.getAllByText(/market-review-123/).length).toBeGreaterThan(0);
        expect(screen.getByText(/"channel": "hub"/)).not.toBeNull();
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
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-contract-text-only',
            outputMode: 'business',
            resultPayload: { text: 'plain completion only' },
            outputs: [{ kind: 'text', title: 'Plain result', text: 'plain completion only', status: 'ready' }],
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));
        const blockedCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Contract Coverage App')) as HTMLElement;
        expect(blockedCard).toBeTruthy();
        expect(within(blockedCard).getByText('Needs work')).not.toBeNull();
        expect(within(blockedCard).getByText(/Run evidence does not cover result contract: business_status/)).not.toBeNull();
        expect((within(blockedCard).getByText('Submit for review') as HTMLButtonElement).disabled).toBe(true);

        cleanup();
        window.localStorage.clear();
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        seedSuccessfulLocalAppRun(app, {
            runID: 'run-contract-covered',
            outputMode: 'business',
            resultPayload: { business_status: 'renewal_ready', business_record: { id: 'customer-1' }, text: 'renewal ready' },
            outputs: [{ kind: 'business_record', title: 'Customer renewal', text: '{"id":"customer-1"}', status: 'ready', data: { id: 'customer-1' } }],
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'market-contract-covered', submitted_at: '2026-06-17T01:00:00.000Z', status: 'submitted', message: 'queued' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));
        const readyCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Contract Coverage App')) as HTMLElement;
        expect(readyCard).toBeTruthy();
        expect(within(readyCard).getByText('Ready to submit')).not.toBeNull();
        expect(within(readyCard).getByText(/primary: business_status/)).not.toBeNull();
        fireEvent.click(within(readyCard).getByText('Submit for review'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        expect(payload.apps[0].app.governance.testEvidence.resultCoverage).toEqual(expect.objectContaining({
            ok: true,
            primary: 'business_status',
            coveredTypes: expect.arrayContaining(['business_status', 'business_record', 'content']),
            missingTypes: [],
        }));
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));
        const blockedCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Approval Instance Evidence App')) as HTMLElement;
        expect(blockedCard).toBeTruthy();
        expect(within(blockedCard).getByText('Approval instance evidence')).not.toBeNull();
        expect(within(blockedCard).getByText('Needs work')).not.toBeNull();
        expect(within(blockedCard).getByText(/Missing instanceId, status, or approval instance view verification/)).not.toBeNull();
        expect((within(blockedCard).getByText('Submit for review') as HTMLButtonElement).disabled).toBe(true);

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
        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));
        const incompleteCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Approval Instance Evidence App')) as HTMLElement;
        expect(incompleteCard).toBeTruthy();
        expect(within(incompleteCard).getByText('Needs work')).not.toBeNull();
        expect(within(incompleteCard).getByText(/missing resultPayload, outputs, or artifacts/)).not.toBeNull();
        expect((within(incompleteCard).getByText('Submit for review') as HTMLButtonElement).disabled).toBe(true);

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
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));
        const readyCard = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Approval Instance Evidence App')) as HTMLElement;
        expect(readyCard).toBeTruthy();
        expect(within(readyCard).getByText('Ready to submit')).not.toBeNull();
        expect(within(readyCard).getByText(/appr-publish-evidence-1 \/ approved \/ expense_report.result_feedback/)).not.toBeNull();
        expect((within(readyCard).getByText('Submit for review') as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(within(readyCard).getByText('Submit for review'));
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
        planMaclawAppInstallMock.mockResolvedValueOnce({
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Approval Contract Publish Drift')) as HTMLElement;
        expect(card).toBeTruthy();
        expect(within(card).getByText('Runtime contract')).not.toBeNull();
        expect(within(card).getByText(/expense-workflow@v1.0.0/)).not.toBeNull();
        expect(within(card).getByText('Ready to submit')).not.toBeNull();
        fireEvent.click(within(card).getByText('Submit for review'));

        await waitFor(() => expect(within(card).getByText(/Runtime contract needs attention: approval workflow contract does not match approval binding/)).not.toBeNull());
        expect(submitMaclawAppPackage).not.toHaveBeenCalled();
    });
    it('includes enterprise visual UI metadata in market submission packages', async () => {
        const app = {
            id: 'publish-enterprise-ui-app',
            name: '客户续约工作台',
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
                dependencies: [{ id: 'customer-renewal-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            },
        });
        planMaclawAppInstallMock.mockResolvedValueOnce({
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: app.id, name: app.name, kind: app.kind }],
            dependencies: [{ id: 'customer-renewal-skill', version: '1.0.0', kind: 'runtime_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: [app.id] }],
            has_missing_required: false,
            has_blocking_dependency: false,
            has_governance_review_issue: false,
        });
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({ submission_id: 'market-enterprise-ui', submitted_at: '2026-06-17T01:00:00.000Z', status: 'submitted', message: 'queued' });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('客户续约工作台')) as HTMLElement;
        expect(card).toBeTruthy();
        fireEvent.click(within(card).getByText('提交审核'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        const layout = payload.apps[0].app.governance.workspaceLayout;
        expect(layout.entry).toBe('business_workspace');
        expect(layout.navigation).toEqual(['customers', 'renewals']);
        expect(layout.list.columns).toEqual(['customer_name', 'status', 'updated_at']);
        expect(payload.apps[0].app.governance.resultContract.primary).toBe('business_status');
        const dependencyVerification = payload.apps[0].app.governance.dependencyVerification;
        expect(dependencyVerification.schema).toBe('maclaw.app.install_plan.v1');
        expect(dependencyVerification.dependencyCount).toBe(1);
        expect(dependencyVerification.hasMissingRequired).toBe(false);
        expect(dependencyVerification.hasGovernanceReviewIssue).toBe(false);
        expect(dependencyVerification.governanceReviewIssueCount).toBe(0);
        expect(dependencyVerification.dependencies[0]).toEqual(expect.objectContaining({ id: 'customer-renewal-skill', installed: true, health: 'ready' }));
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
                testProtocol: { schema: 'maclaw.app.test_protocol.v1', requiredRuns: 1, cases: [{ id: 'smoke', name: 'Smoke', required: true, expectedOutputs: ['content'] }] },
            },
        };
        (app.importedRunEvidence as any).definitionHash = testAppDefinitionFingerprint(app);
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({ orderedIds: [app.id], customApps: [app], recentUsedAtById: {} }));
        planMaclawAppInstallMock.mockResolvedValueOnce({
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));
        const card = Array.from(document.querySelectorAll('.apps-publish-card')).find((item) => item.textContent?.includes('Installed Evidence Republish App')) as HTMLElement;
        expect(card).toBeTruthy();
        fireEvent.click(within(card).getByText('Submit for review'));

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

    it('keeps local channel when the app package bridge queues locally', async () => {
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({
            submission_id: 'local-review-queued',
            submitted_at: '2026-06-17T01:05:00.000Z',
            status: 'submitted',
            channel: 'local',
            message: 'queued locally for enterprise market sync',
        });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="zh-Hans" />);

        await createAndRunLocalToolApp('合同归档');
        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));
        fireEvent.click(screen.getByText('提交审核'));

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

        render(<AppsPage lang="zh-Hans" />);

        await createAndRunLocalToolApp('合同归档');
        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));
        fireEvent.click(screen.getByText('提交审核'));

        await waitFor(() => expect(screen.getAllByText('本地待同步').length).toBeGreaterThan(0));
        fireEvent.click(screen.getByText('撤回提交'));

        await waitFor(() => expect(withdrawMaclawAppPackageSubmission).toHaveBeenCalledWith('local-review-withdraw'));
        expect(screen.getByText('可提交')).not.toBeNull();
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
            package_sha256: '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
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
            event_count: 2,
            last_event_at: '2026-06-17T01:12:00Z',
            message: 'queued locally for enterprise market sync',
        }]);
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));

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
    });

    it('shows a loading state while reading the local submission queue', async () => {
        let resolveQueue: (value: unknown[]) => void = () => {};
        const queuePromise = new Promise<unknown[]>((resolve) => {
            resolveQueue = resolve;
        });
        const listMaclawAppPackageSubmissions = vi.fn().mockReturnValue(queuePromise);
        (window as any).go = { main: { App: { ListMaclawAppPackageSubmissions: listMaclawAppPackageSubmissions } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));

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
                                skills: [{ id: 'approved-app-skill', kind: 'runtime_skill', source: 'hub', required: true }],
                            },
                        },
                    },
                }],
            },
            install_plan: {
                schema: 'maclaw.app.install_plan.v1',
                apps: [{ id: 'approved-app', name: 'Approved Queue App', kind: 'enterprise_normal_app' }],
                dependencies: [{ id: 'approved-app-skill', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed' }],
                has_missing_required: false,
                has_blocking_dependency: false,
            },
            install_record: {
                schema: 'maclaw.app.install_record.v1',
                app_count: 1,
                apps: [{ id: 'approved-app', name: 'Approved Queue App', kind: 'enterprise_normal_app' }],
                dependencies: [{ id: 'approved-app-skill', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed' }],
                app_versions: {
                    'approved-app': {
                        app_entry_version: '7',
                        app_skill: { id: 'approved-app-skill', version: '2.0.0', kind: 'runtime_skill', source: 'hub' },
                    },
                },
                install_evidence: {
                    'approved-app': {
                        workspace_layout: { entry: 'business_workspace', template: 'dashboard', density: 'compact' },
                        result_contract: { primary: 'business_record', types: ['business_record', 'content'] },
                        test_evidence: { runId: 'run-approved-install', testProtocolFingerprint: 'proto-approved-install' },
                        dependencies: [{ id: 'approved-app-skill', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed' }],
                    },
                },
            },
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));

        await waitFor(() => expect(screen.getByText('hub-review-approved')).not.toBeNull());
        fireEvent.click(screen.getByText('Install approved app'));

        await waitFor(() => expect(installMaclawAppPackageFromHubMock).toHaveBeenCalledWith('cap-approved-app'));
        await waitFor(() => expect(screen.getAllByText('Installed').length).toBeGreaterThan(0));
        const approvedRow = screen.getByText('hub-review-approved').closest('.apps-publish-queue__row') as HTMLElement;
        expect(approvedRow).not.toBeNull();
        expect(within(approvedRow).getByText('Dependency verification')).not.toBeNull();
        expect(within(approvedRow).getByText('approved-app-skill')).not.toBeNull();
        expect(within(approvedRow).getByText('v7')).not.toBeNull();
        expect(within(approvedRow).getByText('Workspace layout')).not.toBeNull();
        expect(within(approvedRow).getByText('business_workspace · dashboard · compact')).not.toBeNull();
        expect(within(approvedRow).getByText('Test evidence')).not.toBeNull();
        expect(within(approvedRow).getByText('run-approved-install · proto-approved-install')).not.toBeNull();
        fireEvent.click(screen.getByText('Manage apps'));
        await waitFor(() => expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('Approved Queue App'))).toBe(true));
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

        fireEvent.click(screen.getByTitle('App Studio'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));

        await waitFor(() => expect(screen.getByText('暂无本机待同步提交')).not.toBeNull());
        fireEvent.click(screen.getByText('刷新'));

        await waitFor(() => expect(listMaclawAppPackageSubmissions).toHaveBeenCalledTimes(2));
        expect(screen.getByText('local-review-refreshed')).not.toBeNull();
        expect(screen.getByText(/最后刷新/)).not.toBeNull();
        expect(screen.getByText(/刷新应用/)).not.toBeNull();
        expect(screen.getByText(/published by enterprise market/)).not.toBeNull();
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));

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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));

        await waitFor(() => expect(screen.getByText('local-review-inline-detail')).not.toBeNull());
        fireEvent.click(screen.getByText('查看详情'));

        await waitFor(() => expect(getMaclawAppPackageSubmission).toHaveBeenCalledWith('local-review-inline-detail'));
        expect(screen.getByText('提交详情')).not.toBeNull();
        expect(screen.getByText(/审核人: market-reviewer/)).not.toBeNull();
        expect(screen.getByText(/风险: medium/)).not.toBeNull();
        expect(screen.getByText(/包含应用: 队列内联应用/)).not.toBeNull();
        expect(screen.getByText(/审核问题: error · apps\[0\]\.app\.governance\.testEvidence · 缺少运行证据/)).not.toBeNull();
        expect(screen.getByText(/审计事件: 2026-06-17T01:18:00Z · submitted · local/)).not.toBeNull();

        fireEvent.click(screen.getByText('收起详情'));
        expect(screen.queryByText('提交详情')).toBeNull();
    });

    it('merges durable queue review status into local publish cards', async () => {
        const reviewedApp = {
            id: 'local-app-queue-published',
            name: '队列回写应用',
            description: '用于验证队列状态回写',
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
                skill: { id: 'contract-review', inputMode: 'mixed', multipleFiles: false, outputModes: ['docx', 'pdf'], fields: [] },
            },
        };
        window.localStorage.setItem('maclaw:apps-panel:v1', JSON.stringify({
            orderedIds: [reviewedApp.id],
            customApps: [reviewedApp],
            recentUsedAtById: { [reviewedApp.id]: reviewedApp.recentUsedAt },
        }));
        seedSuccessfulLocalAppRun(reviewedApp);
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
                { path: 'apps[0].app.permissions', severity: 'warning', message: '权限范围偏宽', suggestion: '缩小到财务单据' },
                { path: 'apps[0].app.support.owner', severity: 'info', message: '建议补充负责人' },
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

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));

        await waitFor(() => expect(screen.getAllByText('已发布').length).toBeGreaterThan(0));
        expect(screen.getAllByText(/market-review-published/).length).toBeGreaterThan(0);
        expect(screen.getAllByText(/published by enterprise market/).length).toBeGreaterThan(0);
        expect(screen.getByText(/"status": "published"/)).not.toBeNull();
        expect(screen.getByText(/"channel": "hub"/)).not.toBeNull();
        expect(screen.getByText(/"reviewedAt": "2026-06-17T01:25:00Z"/)).not.toBeNull();
        expect(screen.getByText(/"publishedAt": "2026-06-17T01:30:00Z"/)).not.toBeNull();
        expect(screen.getByText(/"reviewer": "market-reviewer"/)).not.toBeNull();
        expect(screen.getAllByText(/风险: high/).length).toBeGreaterThan(0);
        expect(screen.getAllByText(/批准权限: finance\.expense_submit, finance\.audit/).length).toBeGreaterThan(0);
        expect(screen.getByText(/"riskLevel": "high"/)).not.toBeNull();
        expect(screen.getByText(/"approvedScopes"/)).not.toBeNull();
        expect(screen.getByText(/审核问题: error · apps\[0\]\.app\.governance\.testEvidence · 缺少运行证据 · 先运行一次应用/)).not.toBeNull();
        expect(screen.getByText(/warning · apps\[0\]\.app\.permissions · 权限范围偏宽 · 缩小到财务单据/)).not.toBeNull();
        expect(screen.getByText(/info · apps\[0\]\.app\.support\.owner · 建议补充负责人/)).not.toBeNull();
        expect(screen.getByText(/"reviewIssues"/)).not.toBeNull();
        expect(screen.getByText(/"reviewIssues"/)).not.toBeNull();
        expect(screen.getByText(/"suggestion": "先运行一次应用/)).not.toBeNull();
        expect(screen.getByText(/"message": "建议补充回滚说明"/)).not.toBeNull();

        fireEvent.click(screen.getByText('去修复'));
        await waitFor(() => expect(screen.getByRole('tab', { name: '应用管理' }).getAttribute('aria-selected')).toBe('true'));
        const nameInput = screen.getByDisplayValue('队列回写应用');
        expect(nameInput).not.toBeNull();
        fireEvent.change(nameInput, { target: { value: '队列回写应用修正版' } });
        fireEvent.click(screen.getByText('保存'));
        await waitFor(() => expect(latestStoredCustomApp('队列回写应用修正版')).toBeTruthy());
        seedSuccessfulLocalAppRun(latestStoredCustomApp('队列回写应用修正版'));

        fireEvent.click(screen.getByText('审核/发布'));
        await waitFor(() => expect(screen.getAllByText('本地已修改，需重新提交').length).toBeGreaterThan(0));
        expect(screen.getByText(/本地修改:/)).not.toBeNull();
        expect(screen.getByText(/"modifiedAt"/)).not.toBeNull();
        expect(screen.getByText(/"version": 2/)).not.toBeNull();
        const resubmitButton = screen.getByRole('button', { name: '提交审核' }) as HTMLButtonElement;
        expect(resubmitButton.disabled).toBe(false);

        fireEvent.click(resubmitButton);
        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const submittedPackage = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        expect(JSON.stringify(submittedPackage)).not.toContain('modifiedAt');
        expect(JSON.stringify(submittedPackage)).not.toContain('reviewIssues');
        expect(submittedPackage.apps[0].app.version).toBe(2);
        await waitFor(() => expect(screen.queryByText('本地已修改，需重新提交')).toBeNull());
        expect(screen.getAllByText('等待企业市场审核').length).toBeGreaterThan(0);
        expect(screen.getAllByText(/版本: v2/).length).toBeGreaterThan(0);
        expect(screen.getAllByText(/market-review-resubmitted/).length).toBeGreaterThan(0);
    });

    it('updates the draft manifest preview while creating an app', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '合同归档' } });

        expect(screen.getByText(/合同归档/)).not.toBeNull();
        expect(screen.getByText(/fixed_skill_ui/)).not.toBeNull();
        expect(screen.getByText(/draft-app/)).not.toBeNull();
    });

    it('offers expanded semantic app icons in app studio', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '自动巡检' } });
        fireEvent.click(screen.getByTitle('Agent/自动化 (bot)'));
        fireEvent.click(screen.getByTitle('靛蓝 #5b5ea6'));

        expect(screen.getByRole('button', { name: '付款/财务 (wallet)' })).not.toBeNull();
        expect(screen.getByRole('button', { name: '看板/指标 (dashboard)' })).not.toBeNull();
        expect(screen.getByText(/"icon": "bot"/)).not.toBeNull();
        expect(screen.getByText(/"accent": "#5b5ea6"/)).not.toBeNull();
    });

    it('generates a draft app definition from a natural language prompt', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：做一个合同归档应用，上传 Word/PDF，输出归档编号和审核结果'), {
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
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

        fireEvent.click(screen.getByTitle('App Studio'));
        const kindPicker = document.querySelector('.apps-studio-kind') as HTMLElement;
        fireEvent.click(within(kindPicker).getByRole('button', { name: /Approval app/ }));
        const designButton = screen.getByRole('button', { name: 'Design' });

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
        fireEvent.click(within(row).getByTitle('Manifest'));
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

        fireEvent.click(screen.getByTitle('App Studio'));
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
        fireEvent.click(within(row).getByTitle('Manifest'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '表格清洗' } });
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
        fireEvent.change(screen.getByTestId('studio-layout-template'), { target: { value: 'left_nav' } });
        fireEvent.change(screen.getByTestId('studio-layout-density'), { target: { value: 'compact' } });
        fireEvent.click(screen.getByTestId('studio-layout-slot-center'));
        fireEvent.click(screen.getByTestId('studio-layout-output-bottom'));
        fireEvent.change(screen.getByTestId('studio-layout-region-output_panel'), { target: { value: 'left' } });

        expect(screen.getByText(/"template": "left_nav"/)).not.toBeNull();
        expect(screen.getByText(/"density": "compact"/)).not.toBeNull();
        expect(screen.getByText(/"primaryRegion": "center"/)).not.toBeNull();
        expect(screen.getByText(/"outputRegion": "bottom"/)).not.toBeNull();
        expect(screen.getByText(/"savedInManifest": true/)).not.toBeNull();

        fireEvent.click(document.querySelector('.apps-create-form .apps-actions .apps-primary-button') as HTMLElement);
        fireEvent.click(document.getElementById('apps-studio-tab-manage') as HTMLElement);
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('layout-workbench')) as HTMLElement;
        fireEvent.click(within(row).getByTitle('Manifest'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '批量合同归档' } });
        fireEvent.click(screen.getByText('允许一次选择多个文件'));

        expect(screen.getByText(/"multipleFiles": true/)).not.toBeNull();

        fireEvent.click(screen.getAllByText('创建应用')[1]);
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('批量合同归档')[0]);

        const fileInput = container.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        expect(fileInput.multiple).toBe(true);
    });

    it('adds structured fields to draft tool app manifests', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '合同归档' } });
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：做一个合同归档应用，上传 Word/PDF，输出归档编号和审核结果'), {
            target: { value: '创建费用报销审批应用，录入发票和付款信息，生成财务报表' },
        });
        fireEvent.click(screen.getByText('生成草稿'));

        expect(screen.getByDisplayValue('费用报销审批应用')).not.toBeNull();
        expect(screen.getByDisplayValue('财务')).not.toBeNull();
        expect(screen.getByText(/enterprise_approval_app/)).not.toBeNull();
        expect(screen.getByText(/agent_dynamic_ui/)).not.toBeNull();
    });

    it('shows and saves the visual result contract in App Studio drafts', () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        expect(within(screen.getByTestId('studio-result-contract')).getByText('Result contract')).not.toBeNull();
        expect(within(screen.getByTestId('studio-result-contract')).getAllByText('artifact').length).toBeGreaterThan(0);
        expect(screen.getByText(/"resultContract"/)).not.toBeNull();
        expect(screen.getByText(/"schema": "maclaw.app.result.v1"/)).not.toBeNull();
        expect(screen.getByTestId('studio-test-protocol')).not.toBeNull();
        expect(screen.getByText(/"testProtocol"/)).not.toBeNull();
        expect(screen.getByText(/"schema": "maclaw.app.test_protocol.v1"/)).not.toBeNull();
        fireEvent.click(screen.getByRole('button', { name: 'Business app' }));
        expect(within(screen.getByTestId('studio-result-contract')).getAllByText('business_status').length).toBeGreaterThan(0);
        fireEvent.click(screen.getByRole('button', { name: 'Approval app' }));
        expect(within(screen.getByTestId('studio-result-contract')).getAllByText('approval_result').length).toBeGreaterThan(0);
        expect(within(screen.getByTestId('studio-result-contract')).getByText(/approved \/ rejected/)).not.toBeNull();
    });

    it('saves visual result contract edits from App Studio', () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.change(screen.getByPlaceholderText('Example: Contract filing'), { target: { value: 'Contract Console' } });
        fireEvent.change(screen.getByTestId('studio-result-primary'), { target: { value: 'content' } });
        fireEvent.click(screen.getByTestId('studio-result-delivery-artifacts'));
        fireEvent.change(screen.getByTestId('studio-test-risk'), { target: { value: 'high' } });
        fireEvent.change(screen.getByTestId('studio-test-roles'), { target: { value: 'operator, reviewer' } });
        fireEvent.click(screen.getByRole('button', { name: 'Create app' }));

        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        const created = stored.customApps.find((app: any) => app.name === 'Contract Console');
        expect(created.manifest.resultContract.primary).toBe('content');
        expect(created.manifest.resultContract.delivery.artifacts).toBe(false);
        expect(created.manifest.resultContract.delivery.inlineContent).toBe(true);
        expect(created.manifest.testProtocol.schema).toBe('maclaw.app.test_protocol.v1');
        expect(created.manifest.testProtocol.riskLevel).toBe('high');
        expect(created.manifest.testProtocol.requiredRoles).toEqual(['operator', 'reviewer']);
        expect(created.manifest.testProtocol.fingerprint).toMatch(/^[0-9a-f]{8}$/);
    });

    it('copies the draft manifest preview to clipboard', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '合同归档' } });
        const copyButtons = screen.getAllByText('复制');
        fireEvent.click(copyButtons[0]);

        await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalled());
        expect(String((navigator.clipboard.writeText as any).mock.calls[0][0])).toContain('合同归档');
        expect(screen.getByText('已复制')).not.toBeNull();
    });

    it('resets the draft manifest copy state after the draft changes', async () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.change(screen.getByPlaceholderText('Example: Contract filing'), { target: { value: 'Contract filing' } });
        const preview = document.querySelector('.apps-create-preview') as HTMLElement;
        fireEvent.click(within(preview).getByText('Copy'));

        await waitFor(() => expect(within(preview).getByText('Copied')).not.toBeNull());
        fireEvent.change(screen.getByPlaceholderText('Example: Contract filing'), { target: { value: 'Contract archive' } });

        await waitFor(() => expect(within(preview).getByText('Copy')).not.toBeNull());
    });

    it('keeps the draft manifest preview target mounted when collapsed', () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        const preview = document.querySelector('.apps-create-preview') as HTMLElement;
        const toggle = within(preview).getByRole('button', { name: /Current draft manifest/ });
        const manifest = document.getElementById('apps-create-manifest-preview') as HTMLPreElement;

        expect(toggle.getAttribute('aria-expanded')).toBe('true');
        expect(toggle.getAttribute('aria-controls')).toBe('apps-create-manifest-preview');
        expect(manifest.hidden).toBe(false);

        fireEvent.click(toggle);

        expect(toggle.getAttribute('aria-expanded')).toBe('false');
        expect(document.getElementById('apps-create-manifest-preview')).toBe(manifest);
        expect(manifest.hidden).toBe(true);
    });

    it('creates a local app entry from app studio', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '合同归档' } });
        fireEvent.click(screen.getAllByText('创建应用')[1]);
        fireEvent.click(screen.getByText('关闭'));

        expect(screen.getAllByText('合同归档').length).toBeGreaterThan(0);
    });

    it('creates schema-safe ascii ids for local apps with Chinese names', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '合同归档' } });
        fireEvent.click(screen.getAllByText('创建应用')[1]);
        fireEvent.click(screen.getByText('应用管理'));

        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('合同归档')) as HTMLElement;
        fireEvent.click(within(row).getByTitle('Manifest'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('旧版本地应用')) as HTMLElement;
        expect(row.textContent).toContain('本地');
        fireEvent.click(within(row).getByTitle('Manifest'));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(manifest.app.source).toBe('local');
    });

    it('filters apps by category', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.change(screen.getByPlaceholderText('搜索应用'), { target: { value: '脱敏' } });
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('全部应用 (1)')).not.toBeNull();
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('文档处理 (1)')).not.toBeNull();
        expect((within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('OA (0)') as HTMLOptionElement).disabled).toBe(true);
        expect(screen.getByText('搜索“脱敏” · 1 个匹配')).not.toBeNull();
        fireEvent.click(within(document.querySelector('.apps-filter-row') as HTMLElement).getByTitle('重置筛选'));

        fireEvent.change(document.querySelector('.apps-category-select') as HTMLSelectElement, { target: { value: 'OA' } });
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

    it('tracks recently used apps from opening and execution', () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('采购入库')[0]);
        fireEvent.change(document.querySelector('.apps-category-select') as HTMLSelectElement, { target: { value: 'recent' } });

        expect(screen.getAllByText(/最近使用/).length).toBeGreaterThan(1);
        expect(Array.from(document.querySelectorAll('.apps-section__title')).map((item) => item.textContent)).not.toContain('常用应用');
        expect(screen.getAllByText('采购入库').length).toBeGreaterThan(0);
        expect(screen.queryByText('报销申请')).toBeNull();

        fireEvent.change(document.querySelector('.apps-category-select') as HTMLSelectElement, { target: { value: 'all' } });
        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.click(screen.getByText('执行'));
        fireEvent.change(document.querySelector('.apps-category-select') as HTMLSelectElement, { target: { value: 'recent' } });

        const recentTiles = Array.from(document.querySelectorAll('.apps-section:last-child .apps-app-name')).map((item) => item.textContent || '');
        expect(recentTiles[0]).toBe('报销申请');
        expect(recentTiles).toContain('采购入库');

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
                    current_node: 'expense.result_feedback',
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

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.click(screen.getByText('执行'));

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
        expect(completedPayload.result_payload.business_record).toEqual({ id: 'exp-1', status: 'pending_payment' });
        expect(completedPayload.outputs).toHaveLength(2);
        expect(completedPayload.artifacts[0].name).toBe('approval.pdf');
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
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0].record_id).toBe('appr-test-1');
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
        expect(screen.getAllByText('Expense record').length).toBeGreaterThan(0);
        expect(screen.getAllByText('approval.pdf').length).toBeGreaterThan(0);
        expect(screen.getAllByText(/pending_payment/).length).toBeGreaterThan(0);

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

    it('does not record approval instance when workflow skill launch fails', async () => {
        runNLSkillAsyncMock.mockRejectedValueOnce(new Error('workflow unavailable'));
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('expense-approval-workflow', expect.any(Object)));
        await waitFor(() => expect(screen.getByText('workflow unavailable')).not.toBeNull());
        expect(recordMaclawAppApprovalInstanceMock).not.toHaveBeenCalled();
        expect(syncMaclawAppApprovalInstanceToDataSrvMock).not.toHaveBeenCalled();
    });
    it('marks approval workflow failures as attention results', async () => {
        getNLSkillRunStatusMock.mockResolvedValueOnce({
            run_id: 'run-test-1',
            status: 'failed',
            error: 'policy engine failed',
            summary: { last_error_snippet: 'policy engine failed' },
        });
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.click(screen.getByText('执行'));

	        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock.mock.calls.some((call) => call[0].status === 'attention')).toBe(true));
	        const failedPayload = recordMaclawAppApprovalInstanceMock.mock.calls.map((call) => call[0]).find((payload) => payload.status === 'attention');
	        expect(failedPayload.instance_id).toBe('appr-test-1');
        expect(failedPayload.status).toBe('attention');
        expect(failedPayload.lane).toBe('attention');
        expect(failedPayload.result).toBe('policy engine failed');
        expect(failedPayload.business_status).toBe('workflow_error');
        expect(failedPayload.result_status).toBe('workflow_error');
        expect(failedPayload.events.map((event: any) => event.action)).toContain('workflow_failed');
        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalledTimes(2));
        expect(syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[1][0].instance.status).toBe('attention');
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

        fireEvent.click(within(nav).getByText('\u6211\u7684\u7533\u8bf7'));
        await waitFor(() => expect(within(document.querySelector('.apps-approval-detail') as HTMLElement).getByText('\u5f53\u524d\u5206\u7c7b\u6682\u65e0\u5ba1\u6279\u5b9e\u4f8b')).not.toBeNull());
        expect((within(screen.getByLabelText('\u5ba1\u6279\u64cd\u4f5c')).getByText('\u901a\u8fc7') as HTMLButtonElement).disabled).toBe(true);
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
	        expect(syncPayload.instance.from_status).toBe('approval_pending');
	        expect(syncPayload.instance.to_status).toBe('approved');
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
        expect(within(detail).getByText('finance-report.pdf')).not.toBeNull();
        expect(within(detail).getByText(/pending_finance_review/)).not.toBeNull();
        expect(within(detail).getByText('finance_queue')).not.toBeNull();
        expect(within(detail).getByText('queue')).not.toBeNull();
        expect(within(detail).getByText('submitted -> approval_pending')).not.toBeNull();

        fireEvent.click(within(screen.getByLabelText('\u5ba1\u6279\u64cd\u4f5c')).getByText('\u901a\u8fc7'));

        await waitFor(() => expect(recordMaclawAppApprovalInstanceMock).toHaveBeenCalledTimes(1));
        const payload = recordMaclawAppApprovalInstanceMock.mock.calls[0][0];
        expect(payload.instance_id).toBe('approval-pending-package');
        expect(payload.status).toBe('approved');
        expect(payload.result_payload.business_record.id).toBe('exp-99');
        expect(payload.outputs).toHaveLength(2);
        expect(payload.outputs[1].kind).toBe('requires_input');
        expect(payload.artifacts[0].name).toBe('finance-report.pdf');
        await waitFor(() => expect(syncMaclawAppApprovalInstanceToDataSrvMock).toHaveBeenCalledTimes(1));
        const syncPayload = syncMaclawAppApprovalInstanceToDataSrvMock.mock.calls[0][0];
        expect(syncPayload.instance.result_payload.business_record.status).toBe('pending_finance_review');
        expect(syncPayload.instance.outputs[1].kind).toBe('requires_input');
        expect(syncPayload.instance.artifacts[0].id).toBe('artifact-99');
    });
    it('shows the approval instance workspace for approval apps', async () => {
        listMaclawAppApprovalInstancesMock.mockImplementation(async (_appID, lane) => [{
            app_id: 'expense',
            app_name: '鎶ラ攢鐢宠',
            instance_id: lane === 'pending_my_approval' ? 'approval-workspace-2' : 'approval-workspace-1',
            title: lane === 'pending_my_approval' ? 'Lane refreshed expense' : 'Travel expense summary',
            lane: 'pending_my_approval',
            status: 'pending',
            current_node: lane === 'pending_my_approval' ? 'finance_review' : 'manager_approval',
            current_assignee: lane === 'pending_my_approval' ? 'finance_queue' : 'manager',
            current_assignee_type: lane === 'pending_my_approval' ? 'queue' : 'user',
            from_status: 'submitted',
            to_status: 'approval_pending',
            updated_at: '2026-06-20T00:00:00Z',
            dataset_id: 'finance.expenses',
            object_role: 'expense_report',
            approval_id: lane === 'pending_my_approval' ? 'approval-remote-workspace-2' : 'approval-remote-workspace-1',
            record_id: lane === 'pending_my_approval' ? 'EXP-WORKSPACE-2' : 'EXP-WORKSPACE-1',
        }]);
        render(<AppsPage lang="zh-Hans" />);

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
        expect(within(workspace).getAllByText('approval_result').length).toBeGreaterThan(0);
        expect(within(workspace).getAllByText(/finance_queue/).length).toBeGreaterThan(0);
        expect(within(workspace).getAllByText(/submitted -> approval_pending/).length).toBeGreaterThan(0);
        expect(document.querySelector('.apps-approval-summary')).toBeNull();
        expect(document.querySelector('.apps-approval-manager')).toBeNull();
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

        await waitFor(() => expect(screen.getByText(/已生成输出/)).not.toBeNull());
        expect(screen.getByText(/demo.pdf -> PDF/)).not.toBeNull();
    });

    it('renders form-only tool apps without a file drop zone', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('参数工具')[0]);

        expect(container.querySelector('.apps-drop-zone')).toBeNull();
        fireEvent.change(screen.getByPlaceholderText('输入处理要求或表单参数。'), { target: { value: '生成 JSON 摘要' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getByText(/生成 JSON 摘要 -> JSON/)).not.toBeNull());
    });

    it('renders declared tool app fields as a structured form', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
                                { name: 'format', label: '格式', type: 'select', default: '摘要', options: ['摘要', '清单'] },
                                { name: 'include_refs', label: '包含来源', type: 'boolean', default: true },
                            ],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('字段工具')[0]);

        expect(container.querySelector('.apps-drop-zone')).toBeNull();
        fireEvent.click(screen.getByText('执行'));
        await waitFor(() => expect(screen.getByText('请补充必填输入')).not.toBeNull());
        fireEvent.change(screen.getByLabelText('标题'), { target: { value: '季度报告' } });
        fireEvent.change(screen.getByDisplayValue('摘要'), { target: { value: '清单' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getByText(/标题: 季度报告.*JSON/)).not.toBeNull());
    });

    it('uses the first select option as the default field value', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
                                { name: 'mode', label: '模式', type: 'select', required: true, options: ['快速', '完整'] },
                            ],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('选择工具')[0]);

        expect(container.querySelector('.apps-drop-zone')).toBeNull();
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getByText(/模式: .*JSON/)).not.toBeNull());
    });

    it('adds a select default value to field options when installing apps', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
                                { name: 'mode', label: '模式', type: 'select', required: true, default: '快速', options: ['完整'] },
                            ],
                        },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('默认选项工具')[0]);

        const modeSelect = screen.getByDisplayValue('快速') as HTMLSelectElement;
        expect(Array.from(modeSelect.options).map((option) => option.value)).toEqual(['完整', '快速']);
    });

    it('starts the bound skill when running a tool app', async () => {
        const customIconDataUrl = 'data:image/png;base64,iVBORw0KGgo=';
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
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
        const installedAppsState = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        const installedApp = installedAppsState.customApps.find((item: any) => item.id === 'skill-app-run-tools-run-tool');
        expect(installedApp.customIconDataUrl).toBe(customIconDataUrl);
        const runtimeManifest = {
            schema: installedApp.manifest.schema,
            installUnit: installedApp.manifest.installUnit,
            privateMarker: installedApp.manifest.privateMarker,
            entryKind: installedApp.manifest.entryKind,
            launchMode: installedApp.manifest.launchMode,
            skill: installedApp.manifest.skill,
        };
        const expectedDefinitionHash = textHash(stableStringify({
            name: installedApp.name,
            description: installedApp.description,
            category: installedApp.category,
            kind: installedApp.kind,
            icon: installedApp.icon,
            customIconDataUrl: installedApp.customIconDataUrl,
            version: installedApp.version,
            manifest: runtimeManifest,
        }));
        await waitFor(() => expect(recordMaclawAppRunEvidenceForSkillMock).toHaveBeenCalledWith('run-tools', 'skill-app-run-tools-run-tool', expectedDefinitionHash, 'run-test-1', '', expect.any(String)));

        unmount();
        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(screen.getAllByText('运行工具')[0]);
        expect(screen.getByText('运行历史')).not.toBeNull();
        expect(screen.getAllByText(/run-test-1/).length).toBeGreaterThan(0);
        fireEvent.click(screen.getByText('清空历史'));
        expect(screen.getByText('暂无运行记录')).not.toBeNull();
        expect(screen.queryByText(/run-test-1/)).toBeNull();
    });

    it('checks bound skill dependencies before running installed tool apps', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
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

    it('installs missing runtime dependencies and continues the installed app run', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
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
    it('blocks app runs when the backend install plan reports governance review issues', async () => {
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

        await waitFor(() => expect(screen.getAllByText(/Review issues: missing local run evidence/).length).toBeGreaterThan(0));
        expect(screen.queryByText('Install dependencies and run')).toBeNull();
        expect(installMaclawAppDependenciesMock).not.toHaveBeenCalled();
        expect(runNLSkillAsyncMock).not.toHaveBeenCalled();
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
        const dependency = within(runtimeStatus).getByText('disabled-crm-action').closest('.apps-install-record__dep') as HTMLElement;
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
        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('pdf-word', expect.objectContaining({
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

    it('treats empty skill run ids as failed starts', async () => {
        runNLSkillAsyncMock.mockResolvedValueOnce('');
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        const fileInput = document.querySelector('.apps-drop-zone input[type="file"]') as HTMLInputElement;
        const file = new File(['demo'], 'demo.pdf', { type: 'application/pdf' });
        fireEvent.change(fileInput, { target: { files: [file] } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getAllByText('Skill 执行失败').length).toBeGreaterThan(0));
        expect(screen.queryByText('Skill 执行中')).toBeNull();
        expect(screen.getByText('运行历史')).not.toBeNull();
        expect(screen.getByText(/failed-/)).not.toBeNull();
    });

    it('passes multiple selected files to skill app runs when enabled', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'multi-doc', skill_id: 'multi-tools', name: '多文档处理', description: 'Multi files', category: '文档处理', icon: 'contract', input_mode: 'file', multiple_files: true, output_modes: ['pdf'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('多文档处理')[0]);
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
                { id: 'artifact-1', kind: 'artifact', title: '文件产物卡', artifact_id: 'artifact-1', artifact: { id: 'artifact-1', uri: 'artifact://skill-run/run-test-1/artifact-1', name: 'out.docx', path: '/tmp/out.docx', status: 'ready' } },
                { id: 'file-1', kind: 'file', title: '文件输出卡', artifact_id: 'artifact-1', artifact: { id: 'artifact-1', uri: 'artifact://skill-run/run-test-1/artifact-1', name: 'out.docx', path: '/tmp/out.docx', status: 'ready' } },
                { id: 'summary-1', kind: 'text', title: '摘要', text: '生成完成摘要', status: 'success' },
            ],
        });
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('证据工具')[0]);
        fireEvent.change(screen.getByPlaceholderText('输入处理要求或表单参数。'), { target: { value: '生成文档' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getByText('执行步骤')).not.toBeNull());
        expect(screen.getByText('2/2')).not.toBeNull();
        expect(screen.getByText('读取文件')).not.toBeNull();
        expect(screen.getAllByText('生成文档').length).toBeGreaterThan(0);
        expect(screen.getAllByText('产物已生成').length).toBeGreaterThanOrEqual(2);
        expect(screen.queryByText('文件产物卡')).toBeNull();
        expect(screen.queryByText('文件输出卡')).toBeNull();
        expect(screen.getByText('摘要')).not.toBeNull();
        expect(screen.getAllByText('生成完成摘要').length).toBeGreaterThan(0);
        expect(screen.getAllByText('artifact://skill-run/run-test-1/artifact-1').length).toBeGreaterThan(0);
        expect(screen.getByText('artifact://skill-run/run-test-1/artifact-2')).not.toBeNull();
        expect(screen.getByText('report.pdf · application/pdf · 2048 bytes')).not.toBeNull();
        fireEvent.click(screen.getAllByText('打开')[0]);
        fireEvent.click(screen.getAllByText('定位')[0]);
        expect(openSkillRunArtifactMock).toHaveBeenCalledWith('run-test-1', 'artifact-1');
        expect(revealSkillRunArtifactMock).toHaveBeenCalledWith('run-test-1', 'artifact-1');
        expect(openFileOrShowInFolderMock).not.toHaveBeenCalled();
        expect(showItemInFolderMock).not.toHaveBeenCalled();

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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
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
        getNLSkillRunStatusMock.mockReturnValue(new Promise(() => undefined));
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'long-tool', skill_id: 'long-tools', name: '长任务工具', description: 'Long run', category: '工具', icon: 'sync', input_mode: 'form', output_modes: ['txt'] },
                    ],
                }),
            },
        });
        fireEvent.click(screen.getByText('安装'));
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('长任务工具')[0]);
        fireEvent.change(screen.getByPlaceholderText('输入处理要求或表单参数。'), { target: { value: '持续处理' } });
        fireEvent.click(screen.getByText('执行'));

        const cancelButton = await screen.findByText('取消执行');
        fireEvent.click(cancelButton);

        await waitFor(() => expect(cancelNLSkillRunMock).toHaveBeenCalledWith('run-test-1'));
        expect(screen.getAllByText('Skill 已取消').length).toBeGreaterThan(0);
    });

    it('renders mixed tool apps with file and parameter inputs', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('混合工具')[0]);

        expect(container.querySelector('.apps-drop-zone')).not.toBeNull();
        expect(screen.getByPlaceholderText('输入处理要求或表单参数。')).not.toBeNull();
    });

    it('runs an enterprise app with dynamic DataSrv capability binding', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.change(screen.getByDisplayValue('OA'), { target: { value: '费用报销' } });
        fireEvent.change(screen.getByDisplayValue('新建记录'), { target: { value: 'report' } });
        fireEvent.change(container.querySelector('.apps-preview__mock textarea') as HTMLTextAreaElement, { target: { value: '生成本月报销汇总' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(screen.getByText(/已提交/)).not.toBeNull());
        expect(screen.getByText(/费用报销 · report · finance.expense_upsert/)).not.toBeNull();
    });

	it('renders enterprise normal apps as business workspaces without approval instances', async () => {
		executeMaclawAppBusinessOperationMock.mockResolvedValueOnce({
			synced: true,
            mode: 'business_view',
            target: 'procurement.purchase_order_review',
            result_status: 'done',
            response: { rows: [{ id: 'PO-100', status: 'open' }, { id: 'PO-101', status: 'closed' }] },
        });
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('采购入库')[0]);

        expect(container.querySelector('.apps-business-workspace')).not.toBeNull();
        expect(container.querySelector('.apps-approval-workspace')).toBeNull();
        expect(screen.getByText('业务工作台')).not.toBeNull();
        expect(screen.getAllByText('procurement').length).toBeGreaterThan(0);
        expect(screen.getAllByText('procurement.purchase_orders').length).toBeGreaterThan(0);
        expect(screen.getAllByText('procurement.purchase_order_upsert').length).toBeGreaterThan(0);

        fireEvent.change(screen.getByDisplayValue('新建记录'), { target: { value: 'query' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(executeMaclawAppBusinessOperationMock).toHaveBeenCalledWith(expect.objectContaining({
            app_id: 'purchase-inbound',
            object_role: 'procurement',
            business_action: 'query',
            preferred_action: 'procurement.purchase_order_upsert',
            preferred_view: 'procurement.purchase_order_review',
            preferred_report: 'procurement.purchase_by_status',
            preferred_dashboard: 'procurement.overview',
        })));
        await waitFor(() => expect(screen.getByText(/已完成/)).not.toBeNull());
        await waitFor(() => expect(screen.getByText('id: PO-100 / status: open')).not.toBeNull());
        expect(screen.getByText('business_view')).not.toBeNull();
        expect(screen.getByText('结果类型')).not.toBeNull();
        expect(screen.getByText('table')).not.toBeNull();
        expect(screen.getByText('记录数')).not.toBeNull();
        expect(screen.getByText('PO-101')).not.toBeNull();
		expect(container.querySelector('.apps-run-history')).not.toBeNull();
        const history = JSON.parse(window.localStorage.getItem(runHistoryStorageKey) || '{}') as Record<string, any[]>;
        expect(history['purchase-inbound']?.[0]).toMatchObject({
            status: 'done',
            outputMode: 'business',
            resultPayload: {
                mode: 'business_view',
                target: 'procurement.purchase_order_review',
                result_status: 'done',
                business_status: 'done',
                rows: [{ id: 'PO-100', status: 'open' }, { id: 'PO-101', status: 'closed' }],
            },
            outputs: [expect.objectContaining({ kind: 'table', status: 'done' })],
        });
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'enterprise_app_pack',
                    app: {
                        id: 'customer-op',
                        name: '客户操作台',
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
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('客户操作台')[0]);
        fireEvent.change(screen.getByPlaceholderText('输入业务意图，Agent 生成动态界面并通过 DataSrv 执行。'), { target: { value: '补全客户联系人' } });
        fireEvent.click(screen.getByText('执行'));

        await waitFor(() => expect(runNLSkillAsyncMock).toHaveBeenCalledWith('customer-business-skill', expect.objectContaining({
            app_id: 'market-customer-op',
            app_kind: 'enterprise_normal_app',
            business_entity: 'CRM',
            business_action: 'create',
            business_note: '补全客户联系人',
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Manage apps'));
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('Operations Dashboard')) as HTMLElement;
        fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
        const dialog = screen.getByRole('dialog');
        expect((within(dialog).getByTestId('edit-layout-template') as HTMLSelectElement).value).toBe('dashboard');
        expect((within(dialog).getByTestId('edit-layout-density') as HTMLSelectElement).value).toBe('spacious');
    });
    it('saves enterprise normal business bindings from App Studio into the manifest', () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByRole('button', { name: 'Business app' }));
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

        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        const created = stored.customApps.find((app: any) => app.name === 'Customer Console');
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Manage apps'));
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Manage apps'));
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

    it('persists app order changes from app studio management', () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const manageRows = document.querySelectorAll('.apps-manage-row');
        expect(manageRows[0]?.textContent).toContain('报销申请');
        expect(manageRows[1]?.textContent).toContain('采购入库');
        fireEvent.click(within(manageRows[1] as HTMLElement).getByTitle('上移'));

        unmount();
        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const nextRows = document.querySelectorAll('.apps-manage-row');
        expect(nextRows[0]?.textContent).toContain('采购入库');
        expect(nextRows[1]?.textContent).toContain('报销申请');
    });

    it('moves apps directly to the top and bottom from app studio management', () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const inventoryRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('库存盘点')) as HTMLElement;
        fireEvent.click(within(inventoryRow).getByTitle('移到顶部'));

        let rows = document.querySelectorAll('.apps-manage-row');
        expect(rows[0]?.textContent).toContain('库存盘点');

        fireEvent.click(within(rows[0] as HTMLElement).getByTitle('移到底部'));

        rows = document.querySelectorAll('.apps-manage-row');
        expect(rows[rows.length - 1]?.textContent).toContain('库存盘点');

        unmount();
        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const persistedRows = document.querySelectorAll('.apps-manage-row');
        expect(persistedRows[persistedRows.length - 1]?.textContent).toContain('库存盘点');
    });

    it('filters app studio management by search and category', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const manageSearch = document.querySelector('.apps-manage-filter .apps-search') as HTMLInputElement;
        expect(within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('文档处理 (2)')).not.toBeNull();
        fireEvent.change(manageSearch, { target: { value: 'pdf' } });
        expect(within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('全部应用 (4)')).not.toBeNull();
        expect(within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('文档处理 (2)')).not.toBeNull();
        expect(within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('数据分析 (1)')).not.toBeNull();
        expect(within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('法务 (1)')).not.toBeNull();
        expect((within(document.querySelector('.apps-manage-category-select') as HTMLSelectElement).getByText('OA (0)') as HTMLOptionElement).disabled).toBe(true);
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const manageRows = document.querySelectorAll('.apps-manage-row');
        fireEvent.click(within(manageRows[0] as HTMLElement).getByTitle('Manifest'));

        expect(screen.getByText(/maclaw.app.v1/)).not.toBeNull();
        expect(screen.getByText(/agent_dynamic_ui/)).not.toBeNull();
        expect(screen.getByText(/finance.expense_upsert/)).not.toBeNull();
    });

    it('opens app editing in a dialog instead of expanding the management list', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);

        const manageRows = document.querySelectorAll('.apps-manage-row');
        const editButton = (manageRows[0] as HTMLElement).querySelector('.apps-manage-actions .apps-secondary-button') as HTMLButtonElement;
        editButton.focus();
        fireEvent.click(editButton);

        const dialog = screen.getByRole('dialog');
        expect(dialog.querySelector('.apps-manage-edit')).not.toBeNull();
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

    it('edits built-in app metadata from app studio management and persists it', () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const manageRows = document.querySelectorAll('.apps-manage-row');
        fireEvent.click(within(manageRows[0] as HTMLElement).getByTitle('编辑'));
        fireEvent.change(screen.getByDisplayValue('报销申请'), { target: { value: '费用报销台' } });
        fireEvent.change(screen.getByDisplayValue('OA'), { target: { value: '财务' } });
        fireEvent.change(screen.getByDisplayValue('从发票、行程和政策自动生成报销单。'), { target: { value: '统一处理费用报销台' } });
        expect(screen.getByRole('button', { name: '表格/数据 (sheet)' })).not.toBeNull();
        fireEvent.click(screen.getByTitle('表格/数据 (sheet)'));
        fireEvent.click(screen.getByTitle('琥珀 #b45309'));
        fireEvent.click(screen.getByText('保存'));

        expect(screen.getAllByText('费用报销台').length).toBeGreaterThan(0);

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('费用报销台')) as HTMLElement;
        expect(editedRow.textContent).toContain('财务');
        fireEvent.click(within(editedRow).getByTitle('Manifest'));
        const manifest = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(manifest).toContain('"name": "费用报销台');
        expect(manifest).toContain('"category": "财务"');
        expect(manifest).toContain('"icon": "sheet"');
        expect(manifest).toContain('"accent": "#b45309"');

        unmount();
        render(<AppsPage lang="zh-Hans" />);

        expect(screen.getAllByText('费用报销台').length).toBeGreaterThan(0);
        expect(screen.queryByText('报销申请')).toBeNull();
    });

    it('duplicates an app from app studio management and keeps the source skill binding', () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('复制应用'));

        const copiedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word 副本')) as HTMLElement;
        expect(copiedRow).not.toBeNull();
        expect(copiedRow.textContent).not.toContain('常用应用');
        expect(copiedRow.textContent).toContain('本地');
        expect(screen.getByDisplayValue('PDF 转 Word 副本')).not.toBeNull();

        fireEvent.click(within(pdfWordRow).getByTitle('复制应用'));
        expect(screen.getByDisplayValue('PDF 转 Word 副本 2')).not.toBeNull();
        fireEvent.change(screen.getByDisplayValue('PDF 转 Word 副本 2'), { target: { value: 'PDF 转 Word 快速版' } });
        fireEvent.click(screen.getByText('保存'));

        const renamedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word 快速版')) as HTMLElement;
        fireEvent.click(within(renamedRow).getByTitle('Manifest'));
        const manifest = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(manifest).toContain('"name": "PDF 转 Word 快速版"');
        expect(manifest).toContain('"id": "pdf-word"');
        expect(manifest).toContain('"source": "local"');
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((row) => row.textContent?.includes('PDF 转 Word 副本'))).toBe(true);

        unmount();
        render(<AppsPage lang="zh-Hans" />);
        expect(screen.getAllByText('PDF 转 Word 快速版').length).toBeGreaterThan(0);
    });

    it('deletes duplicated local apps and clears their run history', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('复制应用'));

        const copiedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word 副本')) as HTMLElement;
        expect(within(copiedRow).getByTitle('移除')).not.toBeNull();
        fireEvent.click(within(copiedRow).getByTitle('Manifest'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('编辑'));
        const editPane = document.querySelector('.apps-manage-edit') as HTMLElement;
        fireEvent.change(within(editPane).getByDisplayValue('文件上传'), { target: { value: 'mixed' } });
        fireEvent.click(within(editPane).getByText('Excel / XLSX'));
        fireEvent.click(within(editPane).getByText('保存'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(editedRow).getByTitle('Manifest'));
        const manifest = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(manifest).toContain('"inputMode": "mixed"');
        expect(manifest).toContain('"xlsx"');

        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        const outputSelect = container.querySelector('.apps-form-row select') as HTMLSelectElement;
        expect(Array.from(outputSelect.options).map((option) => option.value)).toContain('xlsx');
    });

    it('edits app studio layout visually and persists it', async () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('编辑'));
        const dialog = screen.getByRole('dialog');
        fireEvent.click(within(dialog).getByTestId('edit-layout-template-left_nav'));
        fireEvent.click(within(dialog).getByTestId('edit-layout-slot-center'));
        fireEvent.click(within(dialog).getByTestId('edit-layout-output-bottom'));
        fireEvent.click(within(dialog).getByText('保存'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(editedRow).getByTitle('Manifest'));
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

        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('PDF 转 Word')[0]);
        await waitFor(() => expect(container.querySelector('.apps-runtime-layout')).not.toBeNull());
        const runtimeLayout = container.querySelector('.apps-runtime-layout') as HTMLElement;
        expect(runtimeLayout.dataset.template).toBe('left_nav');
        expect(runtimeLayout.dataset.primaryRegion).toBe('center');
        expect(runtimeLayout.dataset.outputRegion).toBe('bottom');
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
        fireEvent.click((row.querySelector('.apps-manage-actions .apps-secondary-button') as HTMLButtonElement));
        const dialog = screen.getByRole('dialog');
        fireEvent.change(within(dialog).getByTestId('edit-workflow-approvalNode'), { target: { value: 'finance.director_review' } });
        fireEvent.change(within(dialog).getByTestId('edit-workflow-status-rejected'), { target: { value: 'finance_rejected' } });
        fireEvent.click(within(dialog).getByText('Save'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('Editable Approval Nodes')) as HTMLElement;
        fireEvent.click(within(editedRow).getByTitle('Manifest'));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(manifest.app.binding.workflow.approvalNode).toBe('finance.director_review');
        expect(manifest.app.binding.workflow.statusMapping.rejected).toBe('finance_rejected');
        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        expect(stored.customApps[0].manifest.workflow.approvalNode).toBe('finance.director_review');
    });

    it('edits a tool app skill binding from app studio management', async () => {
        listNLSkillsMock.mockResolvedValue([{ name: 'pdf-word-v2', description: 'Updated converter' }]);
        render(<AppsPage lang="en" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF')) as HTMLElement;
        fireEvent.click((pdfWordRow.querySelector('.apps-manage-actions .apps-secondary-button') as HTMLButtonElement));

        const dialog = screen.getByRole('dialog');
        await waitFor(() => expect(within(dialog).getByRole('option', { name: /pdf-word-v2/ })).not.toBeNull());
        fireEvent.click(within(dialog).getByRole('option', { name: /pdf-word-v2/ }) as HTMLButtonElement);
        fireEvent.click(within(dialog).getByText('Save'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF')) as HTMLElement;
        fireEvent.click(within(editedRow).getByTitle('Manifest'));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(manifest.app.binding.skill.id).toBe('pdf-word-v2');
        expect(manifest.app.binding.appSkill.id).toBe('pdf-word-v2');
    });

    it('shows an error when a custom app icon upload is unsupported', async () => {
        render(<AppsPage lang="en" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);

        const firstRow = document.querySelector('.apps-manage-row') as HTMLElement;
        fireEvent.click((firstRow.querySelector('.apps-manage-actions .apps-secondary-button') as HTMLButtonElement));

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
        render(<AppsPage lang="en" />);

        fireEvent.click(document.querySelector('.apps-studio-button') as HTMLButtonElement);
        fireEvent.click(document.querySelectorAll('[role="tab"]')[1] as HTMLButtonElement);

        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('Portable Icon App')) as HTMLElement;
        fireEvent.click(row.querySelector('.apps-manage-actions .apps-secondary-button') as HTMLButtonElement);
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

    it('edits tool app structured fields from app studio management', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('编辑'));
        const editPane = document.querySelector('.apps-manage-edit') as HTMLElement;
        fireEvent.change(within(editPane).getByDisplayValue('文件上传'), { target: { value: 'mixed' } });
        fireEvent.click(within(editPane).getByText('添加字段'));
        const fieldEditor = within(editPane).getByPlaceholderText('customer_id').closest('.apps-field-editor') as HTMLElement;
        fireEvent.change(within(fieldEditor).getByPlaceholderText('customer_id'), { target: { value: 'review_level' } });
        fireEvent.change(within(fieldEditor).getByPlaceholderText('显示名'), { target: { value: '审核等级' } });
        fireEvent.change(within(fieldEditor).getByDisplayValue('text'), { target: { value: 'select' } });
        fireEvent.change(within(fieldEditor).getByPlaceholderText('A, B, C'), { target: { value: '普通 严格' } });
        fireEvent.click(within(fieldEditor).getByText('必填'));
        fireEvent.click(within(editPane).getByText('保存'));
        await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(editedRow).getByTitle('Manifest'));
        const manifest = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(manifest).toContain('"fields"');
        expect(manifest).toContain('"review_level"');
        expect(manifest).toContain('"options"');
    });

    it('copies the installed apps as an app pack manifest', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));
        fireEvent.click(screen.getByText('复制应用包'));

        await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalled());
        const copied = String((navigator.clipboard.writeText as any).mock.calls.at(-1)?.[0] || '');
        expect(copied).toContain('maclaw.app.pack.v1');
        expect(copied).toContain('报销申请');
        expect(copied).toContain('"governance"');
        expect(screen.getByText('已复制')).not.toBeNull();
    });

    it('shows market apps as a list and adds one to the panel', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));

        expect(screen.getByText('应用市场')).not.toBeNull();
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
        fireEvent.click(screen.getByText('应用管理'));
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
            maclaw_app_id: 'contract-approval',
            maclaw_app_name: 'Contract Approval',
            maclaw_app_description: 'Submit and approve contracts',
            maclaw_app_kind: 'enterprise_approval_app',
            maclaw_app_category: 'Legal',
            maclaw_app_icon: 'shield',
        }]);
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
                dependencies: [{ id: 'contract-workflow', kind: 'workflow_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed', app_ids: ['market-contract-approval'] }],
                has_missing_required: false,
                has_blocking_dependency: false,
            },
            install_record: {
                schema: 'maclaw.app.install_record.v1',
                app_count: 1,
                apps: [{ id: 'market-contract-approval', name: 'Contract Approval', kind: 'enterprise_approval_app' }],
                dependencies: [{ id: 'contract-workflow', kind: 'workflow_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed' }],
                app_versions: { 'market-contract-approval': { app_entry_version: '1', workflow_skills: [{ id: 'contract-workflow', kind: 'workflow_skill', source: 'hub' }] } },
                install_evidence: {
                    'market-contract-approval': {
                        version_snapshot: { app_entry_version: '1', workflow_skills: [{ id: 'contract-workflow', kind: 'workflow_skill', source: 'hub' }] },
                        dependencies: [{ id: 'contract-workflow', kind: 'workflow_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed' }],
                        dependency_verification: {
                            schema: 'maclaw.app.install_plan.v1',
                            verified_at: '2026-06-26T08:00:00.000Z',
                            app_count: 1,
                            dependency_count: 1,
                            has_missing_required: false,
                            has_blocking_dependency: false,
                            dependencies: [{ id: 'contract-workflow', kind: 'workflow_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'installed' }],
                        },
                        test_evidence: {
                            run_id: 'run-hub-contract-approval',
                            verified_at: '2026-06-26T08:01:00.000Z',
                            primary_result: 'approved',
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
                    },
                },
            },
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Add from market'));
        fireEvent.change(screen.getByPlaceholderText('Search enterprise Hub apps'), { target: { value: 'contract' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search Hub' }));

        const row = (await screen.findByText('Contract Approval')).closest('.apps-market-row') as HTMLElement;
        expect(row).toBeTruthy();
        expect(within(row).getByText(/Enterprise Hub/)).not.toBeNull();
        fireEvent.click(within(row).getByRole('button', { name: 'Add: Contract Approval' }));

        await waitFor(() => expect(installSelectedMaclawAppPackageFromHubMock).toHaveBeenCalledWith('cap-hub-contract-approval', ['market-contract-approval']));
        expect(installMaclawAppPackageFromHubMock).not.toHaveBeenCalled();
        expect(installMaclawAppDependenciesMock).not.toHaveBeenCalled();
        await waitFor(() => expect(within(row).getByText('Already installed · Source package 2 · installed 1 · 1 dependencies')).not.toBeNull());
        fireEvent.click(screen.getByText('Manage apps'));
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
        expect(storedApp.versionSnapshot).toMatchObject({ app_entry_version: '1' });
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Add from market'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
        const row = screen.getByText('合同归档').closest('.apps-market-row') as HTMLElement;
        fireEvent.click(within(row).getByRole('button', { name: '添加: 合同归档' }));

        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalledTimes(1));
        expect(recordMaclawAppInstallMock).not.toHaveBeenCalled();
        expect(within(row).getByText('合同归档暂不可用：contract-archive 未安装或已停用。')).not.toBeNull();
        const dependencyList = row.querySelector('.apps-install-record__deps') as HTMLElement;
        expect(dependencyList).not.toBeNull();
        const dependency = within(dependencyList).getByText('contract-archive').closest('.apps-install-record__dep') as HTMLElement;
        expect(dependency.dataset.state).toBe('blocked');
        expect(within(dependency).getByText('不可用')).not.toBeNull();
        expect(dependency.textContent).toContain('runtime_skill · hub · v1.2.0 · disabled');
        expect(row.getAttribute('data-state')).toBe('blocked');
        fireEvent.click(screen.getByText('应用管理'));
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('合同归档'))).toBe(false);
    });

    it('installs an app from a pasted market manifest', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        await waitFor(() => expect(within(installResult).getByText(/DataSrv 绑定已注册: 1/)).not.toBeNull());
    });
    it('does not exceed the pinned app limit when installing pinned market apps', async () => {
        render(<AppsPage lang="zh-Hans" />);

        for (const name of ['文档脱敏', '表格分析']) {
            const tile = screen.getAllByText(name)[0].closest('.apps-app-tile') as HTMLButtonElement;
            fireEvent.contextMenu(tile, { clientX: 90, clientY: 180 });
            fireEvent.click(screen.getByRole('menuitem', { name: '设为常用' }));
        }

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('应用管理'));

        expect(screen.getByText('8/8')).not.toBeNull();
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('市场置顶文档')) as HTMLElement;
        expect(within(row).getByTitle('常用应用已满 8 个，请先取消一个')).not.toBeNull();
        fireEvent.click(within(row).getByTitle('Manifest'));
        const manifest = JSON.parse(document.querySelector('.apps-manage-manifest')?.textContent || '{}');
        expect(manifest.app.panel.pinned).toBe(false);
    });

    it('installs tool apps from a pasted maclaw.apps.json manifest', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
        expect(screen.getAllByText('文档盖章').length).toBeGreaterThan(0);
        expect(screen.getAllByText('文档摘要').length).toBeGreaterThan(0);
    });

    it('upgrades installed market apps when a higher manifest version is pasted', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'doc-redact',
                        name: '文档脱敏增强版',
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

        expect(screen.getByText(/可安装 0 · 可升级 1 · 将跳过 0/)).not.toBeNull();
        expect(screen.getByText(/将升级 v1 -> v2/)).not.toBeNull();
        expect(screen.getByText(/权限变化: \+admin-doc-redact-v2 · 高风险: admin-doc-redact-v2/)).not.toBeNull();
        fireEvent.click(screen.getByText('安装'));
        await waitFor(() => expect(screen.getByText('选中的升级包包含高风险新权限，需再次确认。')).not.toBeNull());
        expect(screen.getByText('选中的升级包包含高风险新权限，需再次确认。').closest('[role="alert"]')).not.toBeNull();
        expect(screen.queryByText('已安装: 0 · 已升级: 1 · 已跳过: 0')).toBeNull();
        fireEvent.click(screen.getByText('确认安装'));
        expect(screen.getByText('已安装: 0 · 已升级: 1 · 已跳过: 0')).not.toBeNull();
        expect(screen.getByRole('status').getAttribute('aria-live')).toBe('polite');
        const upgradeResult = document.querySelector('.apps-install-result') as HTMLElement;
        expect(within(upgradeResult).getByText('文档脱敏增强版')).not.toBeNull();
        expect(within(upgradeResult).getByText('已升级')).not.toBeNull();
        expect(within(upgradeResult).getByText('已升级 v1 -> v2')).not.toBeNull();

        fireEvent.click(screen.getByText('应用管理'));
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('文档脱敏增强版')) as HTMLElement;
        expect(row).toBeTruthy();
        expect(row.textContent).toContain('文档处理');
        fireEvent.click(within(row).getByTitle('Manifest'));
        expect(screen.getByText(/"version": 2/)).not.toBeNull();
        expect(screen.getByText(/"id": "admin-doc-redact-v2"/)).not.toBeNull();
        expect(screen.getByText(/"icon": "shield"/)).not.toBeNull();
        expect(screen.getByText(/"accent": "#28705f"/)).not.toBeNull();
    });


    it('shows dependency verification in market package preview and install results', async () => {
        const installPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'market-dep-preview-app', name: 'Dependency Preview App', kind: 'tool_app' }],
            dependencies: [{ id: 'dep-preview-skill', version: '1.0.0', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'skip', app_ids: ['market-dep-preview-app'] }],
            has_missing_required: false,
            has_blocking_dependency: false,
        };
        planMaclawAppInstallMock.mockResolvedValue(installPlan);

        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(await screen.findByRole('tab', { name: 'Add from market' }));
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

        fireEvent.click(screen.getByText('Install'));

        await waitFor(() => expect(screen.getByText('Installed: 1 · Skipped: 0')).not.toBeNull());
        const resultPanel = document.querySelector('.apps-result-panel') as HTMLElement;
        expect(within(resultPanel).getByText('Dependency verification complete')).not.toBeNull();
        expect(within(resultPanel).getByText('dep-preview-skill')).not.toBeNull();
        expect(recordMaclawAppInstallMock).toHaveBeenCalledTimes(1);
    });

    it('previews manifest apps before installing from market', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    x_maclaw_apps: 'v1',
                    apps: [
                        { id: 'optional-doc', skill_id: 'doc-tools', name: '可选文档', description: 'Optional doc', category: '文档处理', icon: 'contract', input_mode: 'file' },
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
        expect(within(installResult).getByText('可选文档')).not.toBeNull();
        expect(within(installResult).getByText('已跳过')).not.toBeNull();
        expect(within(installResult).getByText('未选择')).not.toBeNull();
        expect(within(installResult).getByText('保留文档')).not.toBeNull();
        fireEvent.click(screen.getByText('关闭'));
        expect(screen.queryByText('可选文档')).toBeNull();
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Add from market'));
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Add from market'));
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
            dependencies: [{ id: 'contract-archive-skill', version: '1.0.0', kind: 'runtime_skill', source: 'hub', required: true, installed: true, health: 'ready', action: 'skip', app_ids: ['market-contract-archive'] }],
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
                    app_skill: { id: 'contract-archive-skill', version: '1.0.0', kind: 'runtime_skill', source: 'hub' },
                },
            },
            install_evidence: {
                'market-contract-archive': {
                    workspace_layout: { entry: 'tool_workspace', template: 'document_workspace', density: 'compact' },
                    result_contract: { primary: 'artifact', types: ['artifact', 'content'] },
                    test_evidence: { runId: 'run-contract-archive', testProtocolFingerprint: 'proto-contract-archive' },
                    dependencies: installPlan.dependencies,
                },
            },
        });

        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Add from market'));
        const row = Array.from(document.querySelectorAll('.apps-market-row')).find((item) => item.textContent?.includes('合同归档')) as HTMLElement;
        expect(row).toBeTruthy();
        fireEvent.click(within(row).getByRole('button', { name: /Add:/ }));

        await waitFor(() => expect(within(row).getByText('Dependency verification complete')).not.toBeNull());
        const verification = row.querySelector('.apps-dependency-verification') as HTMLElement;
        expect(verification).not.toBeNull();
        expect(within(verification).getByText(/Skill dependencies: 1/)).not.toBeNull();
        expect(within(verification).getByText(/Blocking deps: 0/)).not.toBeNull();
        expect(within(verification).getByText('contract-archive-skill')).not.toBeNull();
        const versionSnapshot = row.querySelector('.apps-install-version-snapshot') as HTMLElement;
        expect(versionSnapshot).not.toBeNull();
        expect(versionSnapshot.getAttribute('aria-label')).toBe('Version snapshot');
        expect(within(versionSnapshot).getByText('Version')).not.toBeNull();
        expect(within(versionSnapshot).getByText('v3')).not.toBeNull();
        expect(within(versionSnapshot).getByText('App Skill')).not.toBeNull();
        expect(within(versionSnapshot).getByText('contract-archive-skill · runtime_skill · hub · v1.0.0')).not.toBeNull();
        const evidenceSnapshot = row.querySelector('.apps-install-evidence-snapshot') as HTMLElement;
        expect(evidenceSnapshot).not.toBeNull();
        expect(evidenceSnapshot.getAttribute('aria-label')).toBe('Test evidence');
        expect(within(evidenceSnapshot).getByText('Workspace layout')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('tool_workspace · document_workspace · compact')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('Result contract')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('artifact · 2 types')).not.toBeNull();
        expect(within(evidenceSnapshot).getByText('run-contract-archive · proto-contract-archive')).not.toBeNull();
        expect(recordMaclawAppInstallMock).toHaveBeenCalledTimes(1);
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
                package_sha: 'abcdef1234567890',
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
                    app_skill: { id: 'contract-super-app', version: '1.1.0', kind: 'runtime_skill', source: 'hub' },
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
                has_missing_required: true,
                package: installedPackage,
            },
        ]);

        render(<AppsPage lang="en" />);

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Add from market'));

        await waitFor(() => expect(screen.getByText('Contract Audit')).not.toBeNull());
        expect(screen.getByText(/Package SHA: abcdef123456/)).not.toBeNull();
        expect(screen.getByText(/Skill dependencies: 2/)).not.toBeNull();
        expect(screen.getByText(/Blocking deps: 1/)).not.toBeNull();
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
        expect(within(versionSnapshot).getByText('App Skill')).not.toBeNull();
        expect(within(versionSnapshot).getByText('contract-super-app · runtime_skill · hub · v1.1.0')).not.toBeNull();
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
    it('blocks market install when a required dependency is installed but unavailable', async () => {
        const blockedPlan = {
            schema: 'maclaw.app.install_plan.v1',
            apps: [{ id: 'inactive-market-app', name: 'Inactive Market App', kind: 'tool_app' }],
            dependencies: [{
                id: 'disabled-workflow',
                kind: 'runtime_skill',
                required: true,
                installed: true,
                installed_status: 'disabled',
                health: 'disabled',
                action: 'blocked',
                app_ids: ['inactive-market-app'],
                message: 'required skill dependency is installed but not active (status: disabled)',
            }],
            has_missing_required: false,
            has_blocking_dependency: true,
        };
        planMaclawAppInstallMock.mockResolvedValue(blockedPlan);
        installMaclawAppDependenciesMock.mockResolvedValue(blockedPlan);

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'skill',
                    app: {
                        id: 'inactive-market-app',
                        name: '不可用依赖应用',
                        description: 'Dependency is disabled',
                        category: '文档处理',
                        kind: 'tool_app',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'disabled-workflow', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
                    },
                }),
            },
        });

        await waitFor(() => expect(screen.getByText('必需 Skill 依赖缺失或不可用，请先安装或启用依赖')).not.toBeNull());
        expect(screen.getByText(/不可用: disabled-workflow \* \(disabled\)/)).not.toBeNull();
        expect(document.querySelector('.apps-install-preview__row[data-dependency-state="blocked"]')).not.toBeNull();

        fireEvent.click(screen.getByText('安装'));

        await waitFor(() => expect(installMaclawAppDependenciesMock).toHaveBeenCalledTimes(1));
        expect(screen.getByText(/应用包无效: 必需 Skill 依赖缺失或不可用/)).not.toBeNull();
        expect(screen.queryByText('已安装: 1 · 已跳过: 0')).toBeNull();
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Add from market'));
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
        fireEvent.click(screen.getByText('Manage apps'));
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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Add from market'));
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
        expect(screen.queryByText('Installed: 1 路 Skipped: 0')).toBeNull();
        fireEvent.click(screen.getByText('Manage apps'));
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((item) => item.textContent?.includes('Bad Layout Install'))).toBe(false);
    });
    it('supports select all and select none in market install preview', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        expect(screen.getByText('可安装 2 · 可升级 0 · 将跳过 0')).not.toBeNull();
        fireEvent.click(screen.getByText('全不选'));
        expect(screen.getByText('0/2')).not.toBeNull();
        expect(screen.getByText('可安装 2 · 可升级 0 · 将跳过 2')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);
        fireEvent.click(screen.getByText('全选'));
        expect(screen.getByText('2/2')).not.toBeNull();
        expect(screen.getByText('可安装 2 · 可升级 0 · 将跳过 0')).not.toBeNull();
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
        fireEvent.click(within(row).getByTitle('Manifest'));
        expect(document.querySelector('.apps-manage-manifest')?.textContent).toContain(customIconDataUrl);
    });

    it('uses installed skill app output modes in the runtime UI', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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
        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getAllByText('表格清洗')[0]);

        const outputSelect = container.querySelector('.apps-form-row select') as HTMLSelectElement;
        expect(Array.from(outputSelect.options).map((option) => option.value)).toEqual(['xlsx']);
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
        expect(Array.from(outputSelect.options).map((option) => option.value)).toEqual(['pdf']);
    });

    it('installs apps from a pasted app pack manifest', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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

        fireEvent.click(screen.getByText('关闭'));
        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), { target: { value: manifest } });

        expect(screen.getByText('0/2')).not.toBeNull();
        expect(screen.getAllByText('将跳过 · 已安装').length).toBeGreaterThan(0);
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);
    });

    it('treats market-prefixed and built-in app ids as the same install identity', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: {
                        id: 'pdf-word',
                        name: 'PDF 转 Word',
                        description: 'Duplicate built-in app',
                        category: '文档处理',
                        kind: 'tool_app',
                        icon: 'pdf',
                        launchMode: 'fixed_skill_ui',
                        binding: { skill: { id: 'pdf-word', appDefinitionFile: 'maclaw.apps.json', inputMode: 'file' } },
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('从市场添加'));
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

    it('removes apps from the panel, closes their runtime tabs, and persists hidden built-ins', () => {
        const { container, unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByText('数据同步'));
        expect(container.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('数据同步');
        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const dataSyncRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('数据同步')) as HTMLElement;
        fireEvent.click(within(dataSyncRow).getByTitle('隐藏'));
        fireEvent.click(screen.getByText('关闭'));

        expect(container.querySelector('.apps-runtime-tab')?.textContent || '').not.toContain('数据同步');
        expect(screen.queryByText('数据同步')).toBeNull();

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

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));

        const revokedTile = await waitFor(() => {
            const tile = screen.getAllByText('Hub Revoked App')[0].closest('.apps-app-tile') as HTMLButtonElement;
            expect(tile.getAttribute('data-status')).toBe('disabled');
            return tile;
        });
        expect(revokedTile.title).toContain('Revoked by the enterprise capability market');
        fireEvent.click(screen.getByText('Close'));
        fireEvent.click(revokedTile);
        expect(container.querySelector('.apps-runtime-tab')?.textContent || '').not.toContain('Hub Revoked App');
        const revokedStored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        expect(revokedStored.disabledSourcesById['market-revoked-app']).toBe('hub_governance');

        fireEvent.click(screen.getByTitle('App Studio'));
        fireEvent.click(screen.getByText('Review / publish'));
        fireEvent.click(document.querySelector('.apps-publish-queue__head .apps-secondary-button') as HTMLButtonElement);

        const restoredTile = await waitFor(() => {
            const tile = screen.getAllByText('Hub Revoked App')[0].closest('.apps-app-tile') as HTMLButtonElement;
            expect(tile.getAttribute('data-status')).toBe('available');
            return tile;
        });
        fireEvent.click(screen.getByText('Close'));
        fireEvent.click(restoredTile);
        expect(document.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('Hub Revoked App');
    });
    it('disables apps without hiding entries, blocks launch, and persists the disabled state', () => {
        const { container, unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));
        const dataSyncRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('数据同步')) as HTMLElement;
        fireEvent.click(within(dataSyncRow).getByTitle('停用'));
        fireEvent.click(screen.getByText('关闭'));

        const disabledTile = screen.getAllByText('数据同步')[0].closest('.apps-app-tile') as HTMLButtonElement;
        expect(disabledTile).not.toBeNull();
        expect(disabledTile.getAttribute('data-status')).toBe('disabled');
        expect(disabledTile.title).toContain('企业管理员已停用此应用');
        fireEvent.click(disabledTile);
        expect(container.querySelector('.apps-runtime-tab')?.textContent || '').not.toContain('数据同步');

        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        expect(stored.disabledIds).toContain('data-sync');
        expect(stored.hiddenIds || []).not.toContain('data-sync');

        unmount();
        render(<AppsPage lang="zh-Hans" />);
        const persistedTile = screen.getAllByText('数据同步')[0].closest('.apps-app-tile') as HTMLButtonElement;
        expect(persistedTile.getAttribute('data-status')).toBe('disabled');

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));
        const persistedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('数据同步')) as HTMLElement;
        fireEvent.click(within(persistedRow).getByTitle('启用'));
        fireEvent.click(screen.getByText('关闭'));

        const enabledTile = screen.getAllByText('数据同步')[0].closest('.apps-app-tile') as HTMLButtonElement;
        expect(enabledTile.getAttribute('data-status')).toBe('available');
        fireEvent.click(enabledTile);
        expect(document.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('数据同步');
    });
    it('restores hidden built-in apps from app studio management', () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));
        const webCollectRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('网页采集')) as HTMLElement;
        fireEvent.click(within(webCollectRow).getByTitle('隐藏'));

        expect(screen.getByText('已隐藏应用')).not.toBeNull();
        const hiddenRow = Array.from(document.querySelectorAll('.apps-manage-row--hidden')).find((row) => row.textContent?.includes('网页采集')) as HTMLElement;
        fireEvent.click(within(hiddenRow).getByTitle('恢复'));
        fireEvent.click(screen.getByText('关闭'));

        expect(screen.getAllByText('网页采集').length).toBeGreaterThan(0);

        unmount();
        render(<AppsPage lang="zh-Hans" />);

        expect(screen.getAllByText('网页采集').length).toBeGreaterThan(0);
    });

    it('filters hidden apps in app studio management', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));
        const dataSyncRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('数据同步')) as HTMLElement;
        fireEvent.click(within(dataSyncRow).getByTitle('隐藏'));

        const manageSearch = document.querySelector('.apps-manage-filter .apps-search') as HTMLInputElement;
        fireEvent.change(manageSearch, { target: { value: 'sync' } });

        expect(screen.getByText('已隐藏应用')).not.toBeNull();
        expect(screen.queryByText('没有匹配的应用')).toBeNull();
        expect(document.querySelector('.apps-manage-toolbar .apps-count')?.textContent).toBe('1/10');
        expect(Array.from(document.querySelectorAll('.apps-manage-row--hidden')).some((row) => row.textContent?.includes('数据同步'))).toBe(true);
        expect(Array.from(document.querySelectorAll('.apps-manage-row')).some((row) => row.textContent?.includes('网页采集'))).toBe(false);

        fireEvent.click(screen.getByTitle('清空搜索'));
        fireEvent.change(document.querySelector('.apps-manage-category-select') as HTMLSelectElement, { target: { value: '数据集成' } });
        expect(Array.from(document.querySelectorAll('.apps-manage-row--hidden')).some((row) => row.textContent?.includes('数据同步'))).toBe(true);
    });

    it('keeps pinned apps capped at two rows', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        fireEvent.click(within(document.querySelectorAll('.apps-manage-row')[6] as HTMLElement).getByTitle('置顶'));
        fireEvent.click(within(document.querySelectorAll('.apps-manage-row')[7] as HTMLElement).getByTitle('置顶'));

        const pinButton = within(document.querySelectorAll('.apps-manage-row')[8] as HTMLElement).getByTitle('常用应用已满 8 个，请先取消一个') as HTMLButtonElement;
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
        fireEvent.click(screen.getByTitle('应用程序工作室'));

        await waitFor(() => expect(screen.getByText('可生成应用')).not.toBeNull());
        fireEvent.click(screen.getByText('加到面板'));

        expect(screen.getByText('已添加')).not.toBeNull();
        fireEvent.click(screen.getByText('关闭'));
        expect(screen.getAllByText('人事').length).toBeGreaterThan(0);
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
                    role_bindings: [{ object_role: 'expense_report', domain: 'finance', dataset_id: 'finance.expense_forms', template_id: 'finance.expenses', required: true }],
                    metadata: {
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
                            status_mapping: { pending: 'finance_pending', approved: 'approved', rejected: 'rejected', attention: 'attention', requires_input: 'requires_input' },
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
                            artifacts: [{ id: 'artifact-expense-evidence', uri: 'artifact://expense/evidence.zip', name: 'expense-approval-evidence.zip', status: 'ready' }],
                            output_count: 3,
                            outputs: [{ kind: 'table', title: 'Approval rows', text: 'expense approved', status: 'ready', data: { rows: [{ id: 'expense-1', status: 'finance_approved' }] } }],
                            primary_result: 'approval_result',
                            result_payload: { decision: 'approved', business_status: 'finance_approved' },
                            approval_instance: { approval_id: 'approval-remote-imported', record_id: 'expense-1', status: 'approved', current_node: 'expense_report.result_feedback', approval_instance_view_verified: true, approval_views: { my_requests: true, handled: true, all: true } },
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
                            dependencies: [{ id: 'expense-workflow', version: '2.1.0', kind: 'workflow_skill', required: true, source: 'hub', installed: true, health: 'ready', action: 'skip', app_ids: ['mis.expense'] }],
                        },
                        test_evidence_dependency_verified_at: '2026-06-21T10:58:00Z',
                        test_evidence_dependency_count: 1,
                        test_evidence_dependency_missing_required: false,
                        test_evidence_dependency_blocking: false,
                        test_evidence_workflow_contract_issue: false,
                        test_evidence_workflow_contract_issue_count: 0,
                        test_evidence_governance_review_issue: false,
                        test_evidence_governance_review_issue_count: 0,
                        dependencies: [{ id: 'expense-workflow', kind: 'workflow_skill', required: true, source: 'hub' }],
                    },
                }],
            }),
        });
        vi.stubGlobal('fetch', fetchMock);

        render(<AppsPage lang="en" />);
        fireEvent.click(screen.getByTitle('App Studio'));

        await waitFor(() => expect(screen.getByText('Expense Approval Installed')).not.toBeNull());
        fireEvent.click(screen.getByText('Add to panel'));
        fireEvent.click(screen.getByText('Close'));

        const stored = JSON.parse(window.localStorage.getItem('maclaw:apps-panel:v1') || '{}');
        const added = stored.customApps.find((app: any) => app.name === 'Expense Approval Installed');
        expect(added.kind).toBe('enterprise_approval_app');
        expect(added.id).toBe('datasrv-installed-mis.expense');
        expect(added.manifest.datasrv).toMatchObject({ appID: 'mis.expense', domain: 'finance', datasetID: 'finance.expense_forms', objectRole: 'expense_report', blueprintID: 'mis.expense.approval', templateID: 'finance.expenses' });
        expect(added.manifest.appSkill).toMatchObject({ id: 'expense-super-skill', version: '1.0.0' });
        expect(added.manifest.dependencies.skills.find((dep: any) => dep.id === 'expense-workflow')).toMatchObject({ version: '2.1.0', kind: 'workflow_skill' });
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
            app_skill: { id: 'expense-super-skill', version: '1.0.0', kind: 'runtime_skill', source: 'hub' },
            workflow_skills: [{ id: 'expense-workflow', version: '2.1.0', kind: 'workflow_skill', source: 'hub' }],
            approval_bindings: [{ event: 'expense.submitted', object_role: 'expense_report', workflow_skill_id: 'expense-workflow', workflow_version: '2.1.0' }],
        });
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
        expect(added.importedRunEvidence.dependencyVerification.dependencies[0]).toMatchObject({ id: 'expense-workflow', installed: true, health: 'ready' });
        expect(added.importedRunEvidence.approvalInstance).toMatchObject({
            instanceId: 'approval-remote-imported',
            approvalID: 'approval-remote-imported',
            recordID: 'expense-1',
            status: 'approved',
            currentNode: 'expense_report.result_feedback',
            approvalInstanceViewVerified: true,
            approvalViews: { my_requests: true, handled: true, all: true },
        });
        expect(added.importedRunEvidence.outputs[0]).toMatchObject({ kind: 'table', title: 'Approval rows', text: 'expense approved' });

        fireEvent.click(screen.getByText('Expense Approval Installed'));
        const runtimeVersionSnapshot = await waitFor(() => {
            const snapshot = document.querySelector('.apps-detail__header .apps-install-version-snapshot') as HTMLElement | null;
            expect(snapshot).not.toBeNull();
            return snapshot as HTMLElement;
        });
        expect(runtimeVersionSnapshot.getAttribute('aria-label')).toBe('Version snapshot');
        expect(within(runtimeVersionSnapshot).getByText('Version')).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText('v3')).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText('App Skill')).not.toBeNull();
        expect(within(runtimeVersionSnapshot).getByText('expense-super-skill · runtime_skill · hub · v1.0.0')).not.toBeNull();
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
        fireEvent.click(screen.getByTitle('App Studio'));

        await waitFor(() => expect(screen.getByText('Blocked DataSrv Approval')).not.toBeNull());
        fireEvent.click(screen.getByText('Add to panel'));
        fireEvent.click(screen.getByText('Close'));
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
        fireEvent.click(screen.getByTitle('应用程序工作室'));

        await waitFor(() => expect(screen.getByText('文档脱敏 Plus')).not.toBeNull());
        await waitFor(() => expect(screen.getByText('\u5df2\u52a0\u5165\u9762\u677f')).not.toBeNull());
        expect(screen.queryByText('\u52a0\u5230\u9762\u677f')).toBeNull();
        fireEvent.click(screen.getByText('关闭'));
        expect(screen.getAllByText('文档脱敏 Plus').length).toBeGreaterThan(0);
    });
});
