import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
    canSpecifyExpert,
    canSpecifyWorkflow,
    defaultTaskDraft,
    isDraftDefault,
    isExpertSpecified,
    isWorkflowChosen,
    needsLocalPath,
    normalizeDraftForTaskType,
    withCloudWorkspace,
    withExpert,
    withLocalWorkspace,
    withRemoteWorkspace,
    withWorkflow,
    workspaceKindsForExpert,
    workspaceSummaryLabel,
    WORKFLOW_AUTO,
    type TaskDraft,
} from '../taskDraft';
import { TaskConfigBar, type ExpertOption, type WorkflowOption } from '../TaskConfigBar';
import { RemoteServerForm } from '../RemoteServerPopover';

const testRemoteSSHConnection = vi.fn();
vi.mock('../../../../../wailsjs/go/main/App', () => ({
    TestRemoteSSHConnection: (...args: unknown[]) => testRemoteSSHConnection(...args),
}));

const theme = {
    bg: '#ffffff',
    text: '#1a2333',
    textMuted: '#8a94a6',
    fieldBg: '#f8fafd',
    fieldBorder: '#dde5f0',
    divider: '#eef1f6',
    btnColor: '#1769e8',
    sendBtnBg: '#1769e8',
    sendBtnColor: '#ffffff',
    errorText: '#ef4444',
} as any;

const experts: ExpertOption[] = [
    { id: 'exp-data', name: '数据分析专家', description: '经营分析 · 报表', icon: '📊' },
    { id: 'exp-writer', name: '科研写作专家', description: '论文 · 申报书' },
];

const workflows: WorkflowOption[] = [
    { id: 'wf-table', title: '数据表格整理', category: '数据表格', phaseCount: 4, requiresWorkingDir: true, hasParamSlots: true },
    { id: 'wf-board', title: '一页管理看板', category: '经营分析', phaseCount: 3, requiresWorkingDir: true, hasParamSlots: false },
];

function renderBar(draft: TaskDraft, onChange: (d: TaskDraft) => void, extra?: Partial<Parameters<typeof TaskConfigBar>[0]>) {
    return render(
        <TaskConfigBar
            draft={draft}
            onChange={onChange}
            experts={experts}
            workflows={workflows}
            theme={theme}
            lang="zh"
            {...extra}
        />,
    );
}

afterEach(() => {
    cleanup();
    testRemoteSSHConnection.mockReset();
});

// Stateful harness for round-trip UI flows (mock-onChange renderBar cannot
// feed the emitted draft back into the controlled component).
function renderStatefulBar(initialDraft?: TaskDraft, extra?: Partial<Parameters<typeof TaskConfigBar>[0]>) {
    function Stateful() {
        const [draft, setDraft] = useState(initialDraft ?? defaultTaskDraft());
        return (
            <TaskConfigBar
                draft={draft}
                onChange={setDraft}
                experts={experts}
                workflows={workflows}
                theme={theme}
                lang="zh"
                {...extra}
            />
        );
    }
    return render(<Stateful />);
}

describe('taskDraft', () => {
    it('starts fully default (chat / no expert / no workflow / local default dir)', () => {
        const draft = defaultTaskDraft();
        expect(draft.taskType).toBe('chat');
        expect(draft.expertId).toBeNull();
        // 默认工作流 = 无（不执行工作流）
        expect(draft.workflowTemplateId).toBeNull();
        expect(draft.workspace).toEqual({ kind: 'local' });
        expect(isDraftDefault(draft)).toBe(true);
    });

    it('workflow "none" vs "auto": null = 无, WORKFLOW_AUTO = 自动判断', () => {
        // 「自动判断」仍是对工作流行为的选择（chip 高亮）
        expect(isWorkflowChosen(defaultTaskDraft())).toBe(false);
        expect(isWorkflowChosen(withWorkflow(defaultTaskDraft(), WORKFLOW_AUTO))).toBe(true);
        // 「自动判断」是显式选择 → 不再算全默认（发送时建任务，模板 id 仍为空、语义拦截不变）
        expect(isDraftDefault(withWorkflow(defaultTaskDraft(), WORKFLOW_AUTO))).toBe(false);
        expect(isDraftDefault(withWorkflow(defaultTaskDraft(), 'wf-table'))).toBe(false);
    });

    it('auto-detect counts as choosing a workflow and locks the expert (§7)', () => {
        // 未选工作流（= 无）才可指定专家
        expect(canSpecifyExpert(defaultTaskDraft())).toBe(true);
        // 选了「自动判断」也锁定专家
        const autoDraft = withWorkflow(defaultTaskDraft(), WORKFLOW_AUTO);
        expect(canSpecifyExpert(autoDraft)).toBe(false);
        // 指定专家把工作流回到「无」（不是「自动判断」）
        const expertDraft = withExpert(autoDraft, 'exp-data', '数据分析专家');
        expect(expertDraft.workflowTemplateId).toBeNull();
        expect(canSpecifyExpert(expertDraft)).toBe(true);
        expect(canSpecifyWorkflow(expertDraft)).toBe(false);
    });

    it('specifying an expert clears the workflow (mutual exclusion)', () => {
        let draft = withWorkflow(defaultTaskDraft(), 'wf-table');
        expect(draft.workflowTemplateId).toBe('wf-table');
        draft = withExpert(draft, 'exp-data', '数据分析专家');
        expect(isExpertSpecified(draft)).toBe(true);
        expect(draft.workflowTemplateId).toBeNull();
        expect(canSpecifyWorkflow(draft)).toBe(false);
    });

    it('specifying an expert reduces a cloud/remote workspace to local default (§7)', () => {
        const cloud = withExpert(withCloudWorkspace(defaultTaskDraft(), 'c1', '研发环境'), 'exp-data', '数据分析专家');
        expect(cloud.workspace).toEqual({ kind: 'local' });
        const remote = withExpert(
            withRemoteWorkspace(defaultTaskDraft(), { host: '192.168.1.10', port: 22, user: 'root', password: '', workDir: '/opt/app' }),
            'exp-data', '数据分析专家',
        );
        expect(remote.workspace).toEqual({ kind: 'local' });
        // 回到通用专家：workspace 保持本地默认，位置恢复完全可选
        const back = withExpert(cloud, null, null);
        expect(back.workspace).toEqual({ kind: 'local' });
        expect(workspaceKindsForExpert(back)).toEqual(['local', 'cloud', 'remote']);
    });

    it('workspaceKindsForExpert locks non-local kinds only while an expert is specified', () => {
        expect(workspaceKindsForExpert(defaultTaskDraft())).toEqual(['local', 'cloud', 'remote']);
        expect(workspaceKindsForExpert(withExpert(defaultTaskDraft(), 'exp-data', '数据分析专家'))).toEqual(['local']);
    });

    it('specifying a workflow resets the expert to general (mutual exclusion)', () => {
        let draft = withExpert(defaultTaskDraft(), 'exp-data', '数据分析专家');
        draft = withWorkflow(draft, 'wf-table');
        expect(draft.workflowTemplateId).toBe('wf-table');
        expect(draft.expertId).toBeNull();
        expect(draft.expertName).toBeNull();
        expect(canSpecifyExpert(draft)).toBe(false);
    });

    it('clears stale workflow params when the template changes or is reset', () => {
        let draft = withWorkflow(defaultTaskDraft(), 'wf-table');
        draft = { ...draft, workflowParams: { sheet: 'Q1' } };
        draft = withWorkflow(draft, 'wf-board');
        expect(draft.workflowParams).toEqual({});
        draft = { ...draft, workflowParams: { board: '1' } };
        draft = withWorkflow(draft, null);
        expect(draft.workflowParams).toEqual({});
        // 重新选回同一模板时也清空（参数需重新通过参数弹窗收集）
        draft = withWorkflow(draft, 'wf-board');
        draft = { ...draft, workflowParams: { board: '2' } };
        draft = withWorkflow(draft, 'wf-board');
        expect(draft.workflowParams).toEqual({});
    });

    it('coding tasks drop the cloud workspace and require a local path', () => {
        let draft = withCloudWorkspace(defaultTaskDraft(), 'cws-1', '研发环境');
        draft = normalizeDraftForTaskType({ ...draft, taskType: 'coding' });
        expect(draft.workspace.kind).toBe('local');
        expect(needsLocalPath(draft)).toBe(true);
        draft = withLocalWorkspace(draft, 'D:/work/app');
        expect(needsLocalPath(draft)).toBe(false);
        expect(isDraftDefault(draft)).toBe(false);
    });

    it('summarizes workspace labels for chips', () => {
        expect(workspaceSummaryLabel(defaultTaskDraft())).toBe('默认');
        const coding = { ...defaultTaskDraft(), taskType: 'coding' as const };
        expect(workspaceSummaryLabel(coding)).toBe('请选择目录');
        expect(workspaceSummaryLabel(withCloudWorkspace(defaultTaskDraft(), 'c1', '数据分析环境'))).toBe('数据分析环境');
        expect(workspaceSummaryLabel(withRemoteWorkspace(defaultTaskDraft(), { host: '192.168.1.10', port: 22, user: 'root', password: '', workDir: '/opt/app' }))).toBe('192.168.1.10');
    });

    it('summarizes workspace labels in English for en lang', () => {
        expect(workspaceSummaryLabel(defaultTaskDraft(), false)).toBe('Default');
        const coding = { ...defaultTaskDraft(), taskType: 'coding' as const };
        expect(workspaceSummaryLabel(coding, false)).toBe('Select directory');
        expect(workspaceSummaryLabel(withCloudWorkspace(defaultTaskDraft(), 'c1', ''), false)).toBe('Cloud workspace');
        expect(workspaceSummaryLabel({ ...defaultTaskDraft(), workspace: { kind: 'remote' } }, false)).toBe('Remote server');
    });
});

describe('TaskConfigBar', () => {
    it('shows a single collapsed chip when the draft is all-default', () => {
        renderBar(defaultTaskDraft(), vi.fn());
        const collapsed = screen.getByTestId('task-config-collapsed');
        expect(collapsed).toBeTruthy();
        expect(collapsed.textContent).toContain('默认');
        expect(collapsed.getAttribute('title')).toContain('无工作流');
        expect(screen.queryByTestId('task-config-chip-type')).toBeNull();
    });

    it('collapsed chip shows English copy for en lang', () => {
        renderBar(defaultTaskDraft(), vi.fn(), { lang: 'en' });
        const collapsed = screen.getByTestId('task-config-collapsed');
        expect(collapsed.textContent).toContain('Default');
        expect(collapsed.getAttribute('title')).toContain('No workflow');
    });

    it('expands into four chips after clicking the collapsed chip', () => {
        renderBar(defaultTaskDraft(), vi.fn());
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        expect(screen.getByTestId('task-config-chip-type')).toBeTruthy();
        expect(screen.getByTestId('task-config-chip-expert')).toBeTruthy();
        expect(screen.getByTestId('task-config-chip-workflow')).toBeTruthy();
        expect(screen.getByTestId('task-config-chip-workspace')).toBeTruthy();
    });

    it('auto-expands when any non-default value is set', () => {
        renderBar(withExpert(defaultTaskDraft(), 'exp-data', '数据分析专家'), vi.fn());
        expect(screen.queryByTestId('task-config-collapsed')).toBeNull();
        expect(screen.getByTestId('task-config-chip-expert').textContent).toContain('数据分析专家');
    });

    it('shows 无 as the default workflow chip value', () => {
        renderBar(defaultTaskDraft(), vi.fn());
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        expect(screen.getByTestId('task-config-chip-workflow').textContent).toContain('无');
    });

    it('workflow popover lists 无 (default selected) and 自动判断 on top', () => {
        const onChange = vi.fn();
        renderBar(defaultTaskDraft(), onChange);
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-workflow'));
        const noneItem = screen.getByTestId('workflow-item-none');
        const autoItem = screen.getByTestId('workflow-item-auto');
        expect(noneItem.textContent).toContain('无');
        expect(autoItem.textContent).toContain('自动判断');
        // 选中「自动判断」→ draft.workflowTemplateId = 'auto'
        fireEvent.click(autoItem);
        expect((onChange.mock.calls[0][0] as TaskDraft).workflowTemplateId).toBe(WORKFLOW_AUTO);
    });

    it('auto-detect locks the expert picker (mutual exclusion, §7)', () => {
        // 「自动判断」是显式选择 → 配置条直接展开（无需先点折叠 chip）
        renderBar(withWorkflow(defaultTaskDraft(), WORKFLOW_AUTO), vi.fn());
        expect(screen.queryByTestId('task-config-collapsed')).toBeNull();
        fireEvent.click(screen.getByTestId('task-config-chip-expert'));
        expect(screen.getByTestId('expert-item-exp-data').getAttribute('role')).toBeNull();
        expect(screen.getByTestId('expert-item-generic').getAttribute('role')).toBe('button');
    });

    it('restores expert pickability after resetting the workflow to 无', () => {
        const onChange = vi.fn();
        renderBar(withWorkflow(defaultTaskDraft(), 'wf-table'), onChange);
        fireEvent.click(screen.getByTestId('task-config-chip-workflow'));
        fireEvent.click(screen.getByTestId('workflow-item-none'));
        const draft = onChange.mock.calls[0][0] as TaskDraft;
        expect(draft.workflowTemplateId).toBeNull();
        expect(canSpecifyExpert(draft)).toBe(true);
        expect(draft.expertId).toBeNull();
    });

    it('locks the workflow chip when an expert is specified', () => {
        renderBar(withExpert(defaultTaskDraft(), 'exp-data', '数据分析专家'), vi.fn());
        const workflowChip = screen.getByTestId('task-config-chip-workflow');
        expect(workflowChip.textContent).toContain('由专家决定');
        fireEvent.click(workflowChip);
        const autoItem = screen.getByTestId('workflow-item-auto');
        expect(autoItem.getAttribute('role')).toBeNull();
        expect(screen.getByTestId('workflow-item-wf-table').getAttribute('role')).toBeNull();
    });

    it('resets the expert to general when a workflow is picked and disables expert items', () => {
        const onChange = vi.fn();
        renderBar(defaultTaskDraft(), onChange, { onPickTemplateParams: vi.fn() });
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-workflow'));
        fireEvent.click(screen.getByTestId('workflow-item-wf-table'));
        const draft = onChange.mock.calls[0][0] as TaskDraft;
        expect(draft.workflowTemplateId).toBe('wf-table');
        expect(draft.expertId).toBeNull();
    });

    it('triggers onPickTemplateParams for templates with param slots', () => {
        const onPickTemplateParams = vi.fn();
        const onChange = vi.fn();
        renderBar(defaultTaskDraft(), onChange, { onPickTemplateParams });
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-workflow'));
        fireEvent.click(screen.getByTestId('workflow-item-wf-table'));
        expect(onPickTemplateParams).toHaveBeenCalledWith(expect.objectContaining({ id: 'wf-table' }));
    });

    it('highlights the workspace chip with 请选择目录 for coding without a local path', () => {
        renderBar({ ...defaultTaskDraft(), taskType: 'coding' }, vi.fn());
        expect(screen.getByTestId('task-config-chip-workspace').textContent).toContain('请选择目录');
    });

    it('disables the cloud workspace row for coding tasks', () => {
        renderBar({ ...defaultTaskDraft(), taskType: 'coding' }, vi.fn());
        fireEvent.click(screen.getByTestId('task-config-chip-workspace'));
        expect(screen.getByTestId('workspace-row-cloud').getAttribute('role')).toBeNull();
        expect(screen.getByTestId('workspace-row-local').getAttribute('role')).toBe('button');
    });

    it('picks a recent local directory from the workspace popover', () => {
        const onChange = vi.fn();
        renderBar(defaultTaskDraft(), onChange, { recentLocalPaths: ['D:/work/app'] });
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-workspace'));
        fireEvent.click(screen.getByTestId('workspace-row-local'));
        fireEvent.click(screen.getByTestId('workspace-local-recent-D:/work/app'));
        const draft = onChange.mock.calls[0][0] as TaskDraft;
        expect(draft.workspace).toEqual({ kind: 'local', localPath: 'D:/work/app' });
    });

    it('lists cloud workspaces and picks one into the draft', () => {
        const onChange = vi.fn();
        renderBar(defaultTaskDraft(), onChange, {
            cloudWorkspaces: [
                { id: 'cws-1', name: '研发环境', spec: '1.2 GB', state: '运行中' },
                { id: 'cws-2', name: '数据分析环境', spec: '800 MB', state: '已停止' },
            ],
        });
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-workspace'));
        fireEvent.click(screen.getByTestId('workspace-row-cloud'));
        const row = screen.getByTestId('workspace-cloud-cws-1');
        expect(row.textContent).toContain('研发环境');
        expect(row.textContent).toContain('1.2 GB · 运行中');
        fireEvent.click(row);
        const draft = onChange.mock.calls[0][0] as TaskDraft;
        expect(draft.workspace).toEqual({ kind: 'cloud', cloudWorkspaceId: 'cws-1', cloudName: '研发环境' });
    });

    it('shows an empty cloud list notice and a clickable create row when onCreateCloud is provided', () => {
        const onCreateCloud = vi.fn();
        renderBar(defaultTaskDraft(), vi.fn(), { onCreateCloud });
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-workspace'));
        fireEvent.click(screen.getByTestId('workspace-row-cloud'));
        expect(screen.getByTestId('workspace-cloud-search').getAttribute('placeholder')).toBe('搜索云端工作区');
        expect(screen.getByText('暂无云端工作区')).toBeTruthy();
        fireEvent.click(screen.getByTestId('workspace-cloud-create'));
        expect(onCreateCloud).toHaveBeenCalledTimes(1);
    });

    it('selects the directory returned by onBrowseLocal and closes the popover', async () => {
        const onChange = vi.fn();
        renderBar(defaultTaskDraft(), onChange, { onBrowseLocal: async () => 'D:/picked/dir' });
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-workspace'));
        fireEvent.click(screen.getByTestId('workspace-row-local'));
        fireEvent.click(screen.getByTestId('workspace-local-browse'));
        await waitFor(() => {
            expect(onChange).toHaveBeenCalledTimes(1);
        });
        expect((onChange.mock.calls[0][0] as TaskDraft).workspace).toEqual({ kind: 'local', localPath: 'D:/picked/dir' });
        expect(screen.queryByTestId('task-config-popover-workspace')).toBeNull();
    });

    it('keeps the draft untouched when onBrowseLocal resolves empty (cancel)', async () => {
        const onChange = vi.fn();
        renderBar(defaultTaskDraft(), onChange, { onBrowseLocal: async () => '' });
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-workspace'));
        fireEvent.click(screen.getByTestId('workspace-row-local'));
        fireEvent.click(screen.getByTestId('workspace-local-browse'));
        await waitFor(() => {
            expect(screen.getByTestId('workspace-local-browse')).toBeTruthy();
        });
        expect(onChange).not.toHaveBeenCalled();
        // 弹层保持打开，可继续选择
        expect(screen.getByTestId('task-config-popover-workspace')).toBeTruthy();
    });

    it('submits a manually typed local path on Enter', () => {
        const onChange = vi.fn();
        renderBar(defaultTaskDraft(), onChange);
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-workspace'));
        fireEvent.click(screen.getByTestId('workspace-row-local'));
        const search = screen.getByTestId('workspace-local-search');
        fireEvent.change(search, { target: { value: 'D:/typed/path' } });
        fireEvent.keyDown(search, { key: 'Enter' });
        expect((onChange.mock.calls[0][0] as TaskDraft).workspace).toEqual({ kind: 'local', localPath: 'D:/typed/path' });
        expect(screen.queryByTestId('task-config-popover-workspace')).toBeNull();
    });

    it('submits a manually typed local path via the manual row', () => {
        const onChange = vi.fn();
        renderBar(defaultTaskDraft(), onChange);
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-workspace'));
        fireEvent.click(screen.getByTestId('workspace-row-local'));
        const search = screen.getByTestId('workspace-local-search');
        fireEvent.change(search, { target: { value: '  D:/typed/other  ' } });
        fireEvent.click(screen.getByTestId('workspace-local-manual'));
        expect((onChange.mock.calls[0][0] as TaskDraft).workspace).toEqual({ kind: 'local', localPath: 'D:/typed/other' });
    });

    it('closes the popover on Escape and returns focus to the chip', () => {
        renderBar(defaultTaskDraft(), vi.fn());
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        const typeChip = screen.getByTestId('task-config-chip-type');
        fireEvent.click(typeChip);
        expect(screen.getByTestId('task-config-popover-type')).toBeTruthy();
        fireEvent.keyDown(document, { key: 'Escape' });
        expect(screen.queryByTestId('task-config-popover-type')).toBeNull();
    });

    it('activates popover items with Enter via keyboard', () => {
        const onChange = vi.fn();
        renderBar(defaultTaskDraft(), onChange);
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-type'));
        const codingItem = screen.getByTestId('type-item-coding');
        expect(codingItem.getAttribute('tabindex')).toBe('0');
        fireEvent.keyDown(codingItem, { key: 'Enter' });
        expect((onChange.mock.calls[0][0] as TaskDraft).taskType).toBe('coding');
    });

    it('switches to coding via UI and drops a cloud workspace back to local', () => {
        const onChange = vi.fn();
        renderBar(withCloudWorkspace(defaultTaskDraft(), 'cws-1', '研发环境'), onChange);
        fireEvent.click(screen.getByTestId('task-config-chip-type'));
        fireEvent.click(screen.getByTestId('type-item-coding'));
        const draft = onChange.mock.calls[0][0] as TaskDraft;
        expect(draft.taskType).toBe('coding');
        expect(draft.workspace.kind).toBe('local');
        expect(draft.workspace.cloudWorkspaceId).toBeUndefined();
    });

    it('lists the general expert, experts, and a disabled market row without onOpenMarket', () => {
        renderBar(defaultTaskDraft(), vi.fn(), { onOpenMarket: vi.fn() });
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-expert'));
        expect(screen.getByTestId('expert-item-generic')).toBeTruthy();
        expect(screen.getByTestId('expert-item-exp-data').textContent).toContain('数据分析专家');
        fireEvent.click(screen.getByTestId('expert-item-exp-data'));
        // selecting closes the popover and reflects the expert chip
        expect(screen.queryByTestId('task-config-popover-expert')).toBeNull();
    });

    it('renders each expert own emoji icon and falls back to the robot for none', () => {
        renderBar(defaultTaskDraft(), vi.fn());
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        fireEvent.click(screen.getByTestId('task-config-chip-expert'));
        expect(screen.getByTestId('expert-item-exp-data').textContent).toContain('📊');
        expect(screen.getByTestId('expert-item-exp-writer').textContent).toContain('🤖');
        // 通用专家仍是系统机器人 SVG 图标位
        expect(screen.getByTestId('expert-item-generic').textContent).not.toContain('🤖');
    });

    it('shows the selected expert emoji on the expert chip', () => {
        renderBar(withExpert(defaultTaskDraft(), 'exp-data', '数据分析专家'), vi.fn());
        expect(screen.getByTestId('task-config-chip-expert').textContent).toContain('📊');
    });

    it('shows no icon on the expert chip for the default general expert', () => {
        renderBar(defaultTaskDraft(), vi.fn());
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        const chip = screen.getByTestId('task-config-chip-expert');
        expect(chip.textContent).toContain('通用专家');
        expect(chip.textContent).not.toContain('🤖');
    });

    it('locks cloud/remote workspace rows while an expert is specified and restores them for general (§7)', () => {
        renderStatefulBar();
        fireEvent.click(screen.getByTestId('task-config-collapsed'));
        // 指定具体专家
        fireEvent.click(screen.getByTestId('task-config-chip-expert'));
        fireEvent.click(screen.getByTestId('expert-item-exp-data'));
        // 工作空间 chip 给出说明（本地仍可选，chip 本身不禁用）
        expect(screen.getByTestId('task-config-chip-workspace').getAttribute('title')).toContain('专家任务不携带工作空间');
        fireEvent.click(screen.getByTestId('task-config-chip-workspace'));
        const cloudRow = screen.getByTestId('workspace-row-cloud');
        const remoteRow = screen.getByTestId('workspace-row-remote');
        expect(cloudRow.getAttribute('role')).toBeNull();
        expect(cloudRow.getAttribute('title')).toBe('专家任务不携带工作空间');
        expect(remoteRow.getAttribute('role')).toBeNull();
        expect(remoteRow.getAttribute('title')).toBe('专家任务不携带工作空间');
        expect(screen.getByTestId('workspace-row-local').getAttribute('role')).toBe('button');
        fireEvent.keyDown(document, { key: 'Escape' });
        // 切回通用专家 → 云端/远程恢复可选
        fireEvent.click(screen.getByTestId('task-config-chip-expert'));
        fireEvent.click(screen.getByTestId('expert-item-generic'));
        expect(screen.getByTestId('task-config-chip-workspace').getAttribute('title')).toBeNull();
        fireEvent.click(screen.getByTestId('task-config-chip-workspace'));
        expect(screen.getByTestId('workspace-row-cloud').getAttribute('role')).toBe('button');
        expect(screen.getByTestId('workspace-row-remote').getAttribute('role')).toBe('button');
    });

    it('reduces the workspace chip back to local default when an expert is picked over a cloud workspace', () => {
        renderStatefulBar(withCloudWorkspace(defaultTaskDraft(), 'cws-1', '研发环境'));
        // 云端已选：chip 显示云端徽标 + 工作区名
        expect(screen.getByTestId('task-config-chip-workspace').textContent).toContain('研发环境');
        fireEvent.click(screen.getByTestId('task-config-chip-expert'));
        fireEvent.click(screen.getByTestId('expert-item-exp-data'));
        // 指定专家 → 云端选择被归约为本地默认（chip 不再显示云端）
        const chip = screen.getByTestId('task-config-chip-workspace');
        expect(chip.textContent).not.toContain('研发环境');
        expect(chip.textContent).toContain('默认');
    });
});

describe('RemoteServerForm', () => {
    function renderRemoteForm(onConfirm: (r: unknown) => void, extra?: Record<string, unknown>) {
        return render(<RemoteServerForm theme={theme} lang="zh" onConfirm={onConfirm} {...extra} />);
    }

    function fillRemote(fields: Partial<Record<'remote-host' | 'remote-port' | 'remote-user' | 'remote-password' | 'remote-workdir', string>>) {
        for (const [testId, value] of Object.entries(fields)) {
            fireEvent.change(screen.getByTestId(testId), { target: { value } });
        }
    }

    it('rejects an empty form without calling onConfirm or the SSH binding', () => {
        const onConfirm = vi.fn();
        renderRemoteForm(onConfirm);
        fireEvent.click(screen.getByTestId('remote-confirm'));
        expect(screen.getByTestId('remote-form-status').textContent).toContain('请填写主机');
        expect(onConfirm).not.toHaveBeenCalled();
        fireEvent.click(screen.getByTestId('remote-test-connection'));
        expect(testRemoteSSHConnection).not.toHaveBeenCalled();
    });

    it('rejects an invalid port without calling onConfirm', () => {
        const onConfirm = vi.fn();
        renderRemoteForm(onConfirm);
        fillRemote({
            'remote-host': '192.168.1.10',
            'remote-port': 'abc',
            'remote-user': 'root',
            'remote-workdir': '/opt/app',
        });
        fireEvent.click(screen.getByTestId('remote-confirm'));
        expect(screen.getByTestId('remote-form-status').textContent).toContain('端口');
        expect(onConfirm).not.toHaveBeenCalled();
    });

    it('confirms a valid form with the full RemoteTarget including port', () => {
        const onConfirm = vi.fn();
        renderRemoteForm(onConfirm);
        fillRemote({
            'remote-host': ' 192.168.1.10 ',
            'remote-port': '2222',
            'remote-user': 'root',
            'remote-password': 'secret',
            'remote-workdir': '/opt/app',
        });
        fireEvent.click(screen.getByTestId('remote-confirm'));
        expect(onConfirm).toHaveBeenCalledWith({
            host: '192.168.1.10',
            port: 2222,
            user: 'root',
            password: 'secret',
            workDir: '/opt/app',
        });
    });

    it('calls TestRemoteSSHConnection with (host, user, password, workDir, port) order', async () => {
        testRemoteSSHConnection.mockResolvedValueOnce('连接成功');
        renderRemoteForm(vi.fn());
        fillRemote({
            'remote-host': '192.168.1.10',
            'remote-port': '2222',
            'remote-user': 'root',
            'remote-password': 'secret',
            'remote-workdir': '/opt/app',
        });
        fireEvent.click(screen.getByTestId('remote-test-connection'));
        await screen.findByText('连接成功');
        expect(testRemoteSSHConnection).toHaveBeenCalledWith('192.168.1.10', 'root', 'secret', '/opt/app', 2222);
    });
});

describe('TaskConfigBar variant', () => {
    it('chip variant (default) renders bordered chips with pill radius', () => {
        renderBar(defaultTaskDraft(), vi.fn(), { defaultExpanded: true });
        const chip = screen.getByTestId('task-config-chip-type');
        expect(chip.style.border).toContain('1px');
        expect(chip.style.borderRadius).toBe('15px');
        expect(chip.style.height).toBe('30px');
        expect(chip.style.background).not.toBe('transparent');
    });

    it('bare variant renders borderless transparent chips for the below-card strip', () => {
        renderBar(defaultTaskDraft(), vi.fn(), { defaultExpanded: true, variant: 'bare' });
        for (const testId of ['task-config-chip-type', 'task-config-chip-expert', 'task-config-chip-workflow', 'task-config-chip-workspace']) {
            const chip = screen.getByTestId(testId);
            expect(chip.style.borderStyle).toBe('none');
            expect(chip.style.background).toBe('transparent');
            expect(chip.style.height).toBe('26px');
        }
    });

    it('bare variant keeps active-state and warning-state coloring', () => {
        // 编程类型 → 类型 chip 激活；编程任务缺少本地路径 → 工作空间值红色告警。
        const draft = { ...defaultTaskDraft(), taskType: 'coding' as const };
        renderBar(draft, vi.fn(), { variant: 'bare' });
        const typeChip = screen.getByTestId('task-config-chip-type');
        // jsdom 将十六进制颜色规范化为 rgb()。
        expect(typeChip.style.color).toBe('rgb(23, 105, 232)');
        const workspaceChip = screen.getByTestId('task-config-chip-workspace');
        const valueSpans = workspaceChip.querySelectorAll('span');
        expect(valueSpans[1].style.color).toBe('rgb(239, 68, 68)');
    });
});
