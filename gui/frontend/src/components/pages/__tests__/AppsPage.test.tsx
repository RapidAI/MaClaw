import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const getMISDataConfigMock = vi.hoisted(() => vi.fn());
const listSkillAppManifestsMock = vi.hoisted(() => vi.fn());
const runNLSkillAsyncMock = vi.hoisted(() => vi.fn());
const getNLSkillRunStatusMock = vi.hoisted(() => vi.fn());
const cancelNLSkillRunMock = vi.hoisted(() => vi.fn());
const stageSkillAppInputFileMock = vi.hoisted(() => vi.fn());
const openFileOrShowInFolderMock = vi.hoisted(() => vi.fn());
const showItemInFolderMock = vi.hoisted(() => vi.fn());

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CancelNLSkillRun: (...args: unknown[]) => cancelNLSkillRunMock(...args),
    GetMISDataConfig: (...args: unknown[]) => getMISDataConfigMock(...args),
    GetNLSkillRunStatus: (...args: unknown[]) => getNLSkillRunStatusMock(...args),
    ListSkillAppManifests: (...args: unknown[]) => listSkillAppManifestsMock(...args),
    OpenFileOrShowInFolder: (...args: unknown[]) => openFileOrShowInFolderMock(...args),
    RunNLSkillAsync: (...args: unknown[]) => runNLSkillAsyncMock(...args),
    ShowItemInFolder: (...args: unknown[]) => showItemInFolderMock(...args),
    StageSkillAppInputFile: (...args: unknown[]) => stageSkillAppInputFileMock(...args),
}));

import { AppsPage } from '../AppsPage';

const marketManifestPlaceholder = '粘贴 maclaw.app.v1 / maclaw.app.pack.v1 / maclaw.apps.json';

describe('AppsPage', () => {
    beforeEach(() => {
        window.localStorage.clear();
        getMISDataConfigMock.mockReset().mockResolvedValue({ enabled: false, endpoint: 'http://127.0.0.1:18180' });
        listSkillAppManifestsMock.mockReset().mockResolvedValue([]);
        runNLSkillAsyncMock.mockReset().mockResolvedValue('run-test-1');
        getNLSkillRunStatusMock.mockReset().mockResolvedValue({ run_id: 'run-test-1', status: 'success', summary: { last_output_snippet: 'done' } });
        cancelNLSkillRunMock.mockReset().mockResolvedValue(undefined);
        openFileOrShowInFolderMock.mockReset().mockResolvedValue(undefined);
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
        expect(screen.getByTitle('应用程序工作室')).not.toBeNull();
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('文档处理 (2)')).not.toBeNull();
        expect(within(document.querySelector('.apps-category-select') as HTMLSelectElement).getByText('全部应用 (10)')).not.toBeNull();
        expect(screen.getAllByText('常用应用').length).toBeGreaterThan(0);
        expect(container.querySelectorAll('.apps-app-tile').length).toBeGreaterThan(6);
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
        expect(document.activeElement?.textContent).toContain('合同审查');

        fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'End' });
        expect(document.activeElement?.textContent).toContain('数据同步');
    });

    it('shows app name, status, source, and recent usage in tile tooltips', () => {
        render(<AppsPage lang="zh-Hans" />);

        const expenseTile = screen.getAllByText('报销申请')[0].closest('.apps-app-tile') as HTMLButtonElement;
        expect(expenseTile.title).toContain('报销申请');
        expect(expenseTile.title).toContain('应用程序 · DataSrv');
        expect(expenseTile.title).toContain('状态:');
        expect(expenseTile.title).toContain('最近使用: 尚未使用');
        expect(expenseTile.getAttribute('aria-label')).toContain('报销申请, 应用程序, DataSrv, 状态:');

        fireEvent.click(expenseTile);
        const updatedTile = screen.getAllByText('报销申请')[0].closest('.apps-app-tile') as HTMLButtonElement;
        expect(updatedTile.dataset.status).toBe('running');
        expect(updatedTile.querySelector('.apps-app-status-dot')).not.toBeNull();
        expect(updatedTile.title).toContain('状态: 运行中');
        expect(updatedTile.getAttribute('aria-label')).toContain('状态: 运行中');
        expect(updatedTile.title).toContain('最近使用:');
        expect(updatedTile.title).not.toContain('最近使用: 尚未使用');
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

    it('shows the maclaw.apps.json manifest template in app studio create tab', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));

        expect(screen.getAllByText('应用 manifest 模板').length).toBeGreaterThan(1);
        expect(screen.getAllByText(/x_maclaw_apps/).length).toBeGreaterThan(0);
        expect(screen.getByText(/document-redaction/)).not.toBeNull();
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
        expect(screen.getByText(/"status": "draft"/)).not.toBeNull();
    });

    it('submits ready local apps into local review state', async () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '合同归档' } });
        fireEvent.click(screen.getAllByText('创建应用')[1]);
        fireEvent.click(screen.getAllByText('合同归档')[0]);
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
        window.localStorage.setItem('maclaw:apps-publish-submissions:v1', JSON.stringify({
            [reviewedApp.id]: {
                id: 'local-review-returned',
                appID: reviewedApp.id,
                submittedAt: '2026-06-17T00:00:00.000Z',
                reviewedAt: '2026-06-17T00:10:00.000Z',
                status: 'review_failed',
                reviewer: 'market-reviewer',
                message: '请补充运行证据',
            },
        }));

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));

        expect(screen.getAllByText('审核需修改').length).toBeGreaterThan(0);
        expect(screen.getByText('请补充运行证据')).not.toBeNull();
        expect(screen.getByText(/"status": "review_failed"/)).not.toBeNull();

        fireEvent.click(screen.getByText('提交审核'));
        await waitFor(() => expect(screen.getAllByText('本地待同步').length).toBeGreaterThan(0));
        expect(screen.getByText(/"status": "submitted"/)).not.toBeNull();
    });

    it('uses the enterprise market bridge when submitting app packages', async () => {
        const submitMaclawAppPackage = vi.fn().mockResolvedValue({
            submission_id: 'market-review-123',
            submitted_at: '2026-06-17T01:00:00.000Z',
            status: 'submitted',
            message: 'queued',
        });
        (window as any).go = { main: { App: { SubmitMaclawAppPackage: submitMaclawAppPackage } } };

        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '合同归档' } });
        fireEvent.click(screen.getAllByText('创建应用')[1]);
        fireEvent.click(screen.getAllByText('合同归档')[0]);
        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('审核/发布'));
        fireEvent.click(screen.getByText('提交审核'));

        await waitFor(() => expect(submitMaclawAppPackage).toHaveBeenCalledTimes(1));
        const payload = JSON.parse(submitMaclawAppPackage.mock.calls[0][0]);
        expect(payload.schema).toBe('maclaw.app.pack.v1');
        expect(payload.apps[0].app.name).toBe('合同归档');
        expect(screen.getAllByText('等待企业市场审核').length).toBeGreaterThan(0);
        expect(screen.getAllByText(/market-review-123/).length).toBeGreaterThan(0);
        expect(screen.getByText(/"channel": "hub"/)).not.toBeNull();
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '合同归档' } });
        fireEvent.click(screen.getAllByText('创建应用')[1]);
        fireEvent.click(screen.getAllByText('合同归档')[0]);
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

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.change(screen.getByPlaceholderText('例：合同归档'), { target: { value: '合同归档' } });
        fireEvent.click(screen.getAllByText('创建应用')[1]);
        fireEvent.click(screen.getAllByText('合同归档')[0]);
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
        expect(screen.getByText(/事件:2 2026-06-17T01:12:00Z/)).not.toBeNull();
        expect(screen.getByText(/queued locally for enterprise market sync/)).not.toBeNull();
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
        expect(screen.getByText(/最后刷新:/)).not.toBeNull();
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
            description: '用于验证队列状态回灌',
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
        expect(screen.getByText(/另 1 项/)).not.toBeNull();
        expect(screen.getByText(/"reviewIssues"/)).not.toBeNull();
        expect(screen.getByText(/"suggestion": "先运行一次应用"/)).not.toBeNull();
        expect(screen.getByText(/"message": "建议补充回滚说明"/)).not.toBeNull();

        fireEvent.click(screen.getByText('去修复'));
        await waitFor(() => expect(screen.getByRole('tab', { name: '应用管理' }).getAttribute('aria-selected')).toBe('true'));
        const nameInput = screen.getByDisplayValue('队列回写应用');
        expect(nameInput).not.toBeNull();
        fireEvent.change(nameInput, { target: { value: '队列回写应用修正版' } });
        fireEvent.click(screen.getByText('保存'));

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

    it('uses studio type cards as create-form presets', () => {
        render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        const cards = document.querySelectorAll('.apps-studio-card');
        const appCardButton = within(cards[0] as HTMLElement).getByRole('button', { name: '应用程序' }) as HTMLButtonElement;
        const toolCardButton = within(cards[1] as HTMLElement).getByRole('button', { name: '工具应用' }) as HTMLButtonElement;
        const automationCardButton = within(cards[2] as HTMLElement).getByRole('button', { name: '自动化应用' }) as HTMLButtonElement;

        expect(toolCardButton.getAttribute('aria-pressed')).toBe('true');

        fireEvent.click(appCardButton);
        expect(appCardButton.getAttribute('aria-pressed')).toBe('true');
        expect(screen.getByDisplayValue('应用程序')).not.toBeNull();
        expect(screen.getByDisplayValue('OA')).not.toBeNull();
        expect(screen.getByText(/"kind": "enterprise_app"/)).not.toBeNull();
        expect(screen.getByText(/"launchMode": "agent_dynamic_ui"/)).not.toBeNull();

        fireEvent.click(automationCardButton);
        expect(automationCardButton.getAttribute('aria-pressed')).toBe('true');
        expect(screen.getAllByDisplayValue('自动化').length).toBeGreaterThan(1);
        expect(screen.getByText(/"kind": "automation_app"/)).not.toBeNull();
        expect(screen.getByText(/"launchMode": "automation_console"/)).not.toBeNull();
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
        expect(screen.getByText(/enterprise_app/)).not.toBeNull();
        expect(screen.getByText(/agent_dynamic_ui/)).not.toBeNull();
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

    it('shows an empty runtime area until an app icon is clicked', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        expect(screen.getByText('选择应用')).not.toBeNull();
        expect(screen.getByText('点击左侧图标后，应用将在这里以 tab 打开。')).not.toBeNull();
        expect(container.querySelector('.apps-runtime-tabs')).toBeNull();

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        expect(container.querySelector('.apps-runtime-tabs')).not.toBeNull();
        expect(container.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('报销申请');
    });

    it('closes runtime tabs and activates the remaining app', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.click(screen.getAllByText('采购入库')[0]);
        fireEvent.click(screen.getByLabelText('关闭 采购入库'));

        expect(container.querySelectorAll('.apps-runtime-tab').length).toBe(1);
        expect(container.querySelector('.apps-runtime-tab.is-active')?.textContent).toContain('报销申请');
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
        expect(screen.getByText('请补充必填输入')).not.toBeNull();
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

        await waitFor(() => expect(screen.getByText(/模式: 快速.*JSON/)).not.toBeNull());
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

        unmount();
        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(screen.getAllByText('运行工具')[0]);
        expect(screen.getByText('运行历史')).not.toBeNull();
        expect(screen.getAllByText(/run-test-1/).length).toBeGreaterThan(0);
        fireEvent.click(screen.getByText('清空历史'));
        expect(screen.getByText('暂无运行记录')).not.toBeNull();
        expect(screen.queryByText(/run-test-1/)).toBeNull();
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

        expect(screen.getByText('文件超过 25MB，暂不支持此方式上传')).not.toBeNull();
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
        expect(screen.getByText('产物已生成')).not.toBeNull();
        expect(screen.getAllByText('/tmp/out.docx').length).toBeGreaterThan(1);
        fireEvent.click(screen.getAllByText('打开')[0]);
        fireEvent.click(screen.getAllByText('定位')[0]);
        expect(openFileOrShowInFolderMock).toHaveBeenCalledWith('/tmp/out.docx');
        expect(showItemInFolderMock).toHaveBeenCalledWith('/tmp/out.docx');

        expect(screen.getByText('运行历史')).not.toBeNull();
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

    it('runs an enterprise app with dynamic DataSrv capability binding', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getAllByText('报销申请')[0]);
        fireEvent.change(screen.getByDisplayValue('OA'), { target: { value: '费用报销' } });
        fireEvent.change(screen.getByDisplayValue('新建记录'), { target: { value: 'report' } });
        fireEvent.change(container.querySelector('.apps-preview__mock textarea') as HTMLTextAreaElement, { target: { value: '生成本月报销汇总' } });
        fireEvent.click(screen.getByText('执行'));

        expect(screen.getByText(/已提交/)).not.toBeNull();
        expect(screen.getByText(/费用报销 · report · finance.expense_upsert/)).not.toBeNull();
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

    it('edits built-in app metadata from app studio management and persists it', () => {
        const { unmount } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const manageRows = document.querySelectorAll('.apps-manage-row');
        fireEvent.click(within(manageRows[0] as HTMLElement).getByTitle('编辑'));
        fireEvent.change(screen.getByDisplayValue('报销申请'), { target: { value: '费用报销台' } });
        fireEvent.change(screen.getByDisplayValue('OA'), { target: { value: '财务' } });
        fireEvent.change(screen.getByDisplayValue('从发票、行程和政策自动生成报销单。'), { target: { value: '统一处理费用报销。' } });
        expect(screen.getByRole('button', { name: '表格/数据 (sheet)' })).not.toBeNull();
        fireEvent.click(screen.getByTitle('表格/数据 (sheet)'));
        fireEvent.click(screen.getByTitle('琥珀 #b45309'));
        fireEvent.click(screen.getByText('保存'));

        expect(screen.getAllByText('费用报销台').length).toBeGreaterThan(0);

        const editedRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('费用报销台')) as HTMLElement;
        expect(editedRow.textContent).toContain('财务');
        fireEvent.click(within(editedRow).getByTitle('Manifest'));
        const manifest = document.querySelector('.apps-manage-manifest')?.textContent || '';
        expect(manifest).toContain('"name": "费用报销台"');
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

    it('edits tool app runtime modes from app studio management', () => {
        const { container } = render(<AppsPage lang="zh-Hans" />);

        fireEvent.click(screen.getByTitle('应用程序工作室'));
        fireEvent.click(screen.getByText('应用管理'));

        const pdfWordRow = Array.from(document.querySelectorAll('.apps-manage-row')).find((row) => row.textContent?.includes('PDF 转 Word')) as HTMLElement;
        fireEvent.click(within(pdfWordRow).getByTitle('编辑'));
        const editPane = document.querySelector('.apps-manage-edit') as HTMLElement;
        fireEvent.change(within(editPane).getByDisplayValue('文件上传'), { target: { value: 'mixed' } });
        fireEvent.click(within(editPane).getByText('Excel / XLSX'));
        fireEvent.click(within(editPane).getByText('保存'));

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

    it('edits tool app structured fields from app studio management', () => {
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
        fireEvent.change(within(fieldEditor).getByPlaceholderText('A, B, C'), { target: { value: '普通, 严格' } });
        fireEvent.click(within(fieldEditor).getByText('必填'));
        fireEvent.click(within(editPane).getByText('保存'));

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

    it('installs an app from a pasted market manifest', () => {
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

        expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull();
        expect(screen.getAllByText('文档归档').length).toBeGreaterThan(0);
        fireEvent.click(screen.getByText('关闭'));
        expect(screen.getAllByText('文档归档').length).toBeGreaterThan(0);
    });

    it('does not exceed the pinned app limit when installing pinned market apps', () => {
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
        fireEvent.click(screen.getByText('应用管理'));

        expect(screen.getByText('6/6')).not.toBeNull();
        const row = Array.from(document.querySelectorAll('.apps-manage-row')).find((item) => item.textContent?.includes('市场置顶文档')) as HTMLElement;
        expect(within(row).getByTitle('常用应用已满 6 个，请先取消一个')).not.toBeNull();
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

    it('installs apps from a pasted app pack manifest', () => {
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

        expect(screen.getByText('已安装: 1 · 已跳过: 0')).not.toBeNull();
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
        expect(screen.getByLabelText('从 Manifest 安装')).not.toBeNull();
        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), { target: { value: '{bad json' } });

        expect(screen.getByText('无效 Manifest: JSON 解析失败')).not.toBeNull();
        expect(screen.getByText('无效 Manifest: JSON 解析失败').closest('[role="alert"]')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), { target: { value: JSON.stringify({ apps: [] }) } });

        expect(screen.getByText('无效 Manifest: 缺少 maclaw.app.v1、maclaw.app.pack.v1 或 x_maclaw_apps')).not.toBeNull();
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

        expect(screen.getByText('无效 Manifest: maclaw.app.v1 privateMarker must be x_maclaw_apps')).not.toBeNull();
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

        expect(screen.getByText('无效 Manifest: maclaw.app.v1 app.id is invalid')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    app: { id: 'bad-launch', name: 'Bad Launch', kind: 'enterprise_app', launchMode: 'fixed_skill_ui' },
                }),
            },
        });

        expect(screen.getByText('无效 Manifest: maclaw.app.v1 app.launchMode must be agent_dynamic_ui for enterprise_app')).not.toBeNull();
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

        expect(screen.getByText('无效 Manifest: maclaw.app.v1 app.launchMode must be automation_console for automation_app')).not.toBeNull();
        expect((screen.getByText('安装') as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByPlaceholderText(marketManifestPlaceholder), {
            target: {
                value: JSON.stringify({
                    schema: 'maclaw.app.v1',
                    privateMarker: 'x_maclaw_apps',
                    installUnit: 'enterprise_app_pack',
                    app: { id: 'missing-datasrv', name: 'Missing DataSrv', kind: 'enterprise_app', launchMode: 'agent_dynamic_ui' },
                }),
            },
        });

        expect(screen.getByText('无效 Manifest: maclaw.app.v1 binding.datasrv is required for enterprise_app')).not.toBeNull();
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

        expect(screen.getByText('无效 Manifest: maclaw.app.v1 binding.skill is required for tool_app')).not.toBeNull();
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

        expect(screen.getByText('无效 Manifest: maclaw.app.v1 installUnit must be skill for tool_app')).not.toBeNull();
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

        expect(screen.getByText('无效 Manifest: maclaw.app.v1 binding.skill.outputModes[0] is invalid')).not.toBeNull();
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

        expect(screen.getByText('无效 Manifest: maclaw.apps.json apps[0].fields[0].type is invalid')).not.toBeNull();
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

        expect(screen.getByText('无效 Manifest: maclaw.app.pack.v1 apps[0] privateMarker must be x_maclaw_apps')).not.toBeNull();
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

        const pinButton = within(document.querySelectorAll('.apps-manage-row')[6] as HTMLElement).getByTitle('常用应用已满 6 个，请先取消一个') as HTMLButtonElement;
        expect(pinButton.disabled).toBe(true);
        fireEvent.click(pinButton);

        expect(screen.getByText('6/6')).not.toBeNull();
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

    it('turns skill maclaw.apps.json entries into addable tool apps', async () => {
        listSkillAppManifestsMock.mockResolvedValue([
            { id: 'redact', skill_id: 'doc-tools', name: '文档脱敏 Plus', description: 'Redact files', category: '文档处理', icon: 'shield', input_mode: 'file' },
        ]);

        render(<AppsPage lang="zh-Hans" />);
        fireEvent.click(screen.getByTitle('应用程序工作室'));

        await waitFor(() => expect(screen.getByText('文档脱敏 Plus')).not.toBeNull());
        fireEvent.click(screen.getByText('加到面板'));

        expect(screen.getByText('已添加')).not.toBeNull();
        fireEvent.click(screen.getByText('关闭'));
        expect(screen.getAllByText('文档脱敏 Plus').length).toBeGreaterThan(0);
    });
});
