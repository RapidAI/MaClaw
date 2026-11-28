// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
    getCloudSrvChoice: vi.fn(),
    getMaclawLLMProfilePanelState: vi.fn(),
    setCloudSrvChoice: vi.fn(),
    probeCloudSrvSSH: vi.fn(),
    deployDesktopNode: vi.fn(),
    listCloudSrvNodes: vi.fn(),
    upgradeCloudSrvNode: vi.fn(),
    deleteCloudSrvNode: vi.fn(),
    showConfirm: vi.fn(),
    openSettingsTab: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetCloudSrvChoice: (...args: unknown[]) => mocks.getCloudSrvChoice(...args),
    GetMaclawLLMProfilePanelState: (...args: unknown[]) => mocks.getMaclawLLMProfilePanelState(...args),
    SetCloudSrvChoice: (...args: unknown[]) => mocks.setCloudSrvChoice(...args),
    ProbeCloudSrvSSH: (...args: unknown[]) => mocks.probeCloudSrvSSH(...args),
    DeployDesktopNode: (...args: unknown[]) => mocks.deployDesktopNode(...args),
    ListCloudSrvNodes: (...args: unknown[]) => mocks.listCloudSrvNodes(...args),
    UpgradeCloudSrvNode: (...args: unknown[]) => mocks.upgradeCloudSrvNode(...args),
    DeleteCloudSrvNode: (...args: unknown[]) => mocks.deleteCloudSrvNode(...args),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({ showConfirm: mocks.showConfirm }),
}));

vi.mock('../../../utils/settingsNavigation', () => ({
    openSettingsTab: (...args: unknown[]) => mocks.openSettingsTab(...args),
}));

import { CloudAgentSettingsPanel } from '../CloudAgentSettingsPanel';

const baseNodes = [
    { node_id: 'node-1', name: 'office-srv', address: '10.0.0.8', online: true },
    { node_id: 'node-2', name: 'home-srv', address: '10.0.0.9', online: false },
];

const cloudSummary = (overrides: Record<string, unknown>) => ({
    profile: 'cloud',
    ...overrides,
});

const renderPanel = () => {
    render(<CloudAgentSettingsPanel lang="zh-Hans" showToastMessage={vi.fn()} />);
};

beforeEach(() => {
    vi.clearAllMocks();
    mocks.getCloudSrvChoice.mockResolvedValue({ node_id: '', nodes: baseNodes });
    // A custom pick by default: the card names the provider · model and the
    // 设置 button is the only way into the editor.
    mocks.getMaclawLLMProfilePanelState.mockResolvedValue({
        cloud: cloudSummary({ provider_id: 'prov-1', provider_name: 'DeepSeek', model: 'deepseek-v3', inherit_assistant: false, configured: true }),
    });
    mocks.listCloudSrvNodes.mockResolvedValue([]);
});

describe('CloudAgentSettingsPanel', () => {
    it('renders sectioned cards with the read-only default-model line', async () => {
        renderPanel();
        await waitFor(() => expect(mocks.getCloudSrvChoice).toHaveBeenCalled());
        expect(screen.getByText('云端连接')).toBeTruthy();
        expect(screen.getByText('默认模型')).toBeTruthy();
        expect(screen.getByText('私有云端服务')).toBeTruthy();
        expect(screen.getByText('公有 maclawsrv（默认）')).toBeTruthy();
        // The current setting renders as chips: mode + provider · model.
        expect(screen.getByText('自定义')).toBeTruthy();
        expect(screen.getByText('DeepSeek · deepseek-v3')).toBeTruthy();
        // Exactly one 保存 button: the connection card's.
        expect(screen.getAllByRole('button', { name: '保存' }).length).toBe(1);
    });

    it('no longer lists instances or edits the default model; edits live in 大模型配置', async () => {
        renderPanel();
        await waitFor(() => expect(mocks.getMaclawLLMProfilePanelState).toHaveBeenCalled());
        expect(screen.queryByRole('button', { name: '保存并应用' })).toBeNull();
        expect(screen.queryByText('实例模型')).toBeNull();
        expect(screen.queryByText(/空闲|运行中|部署中/)).toBeNull();
        // The card only shows the resolved default plus the jump button.
        expect(screen.getByRole('button', { name: '设置' })).toBeTruthy();
        expect(screen.queryByRole('combobox')).toBeNull();
    });

    it('shows the follow-assistant default with the assistant live pick', async () => {
        mocks.getMaclawLLMProfilePanelState.mockResolvedValue({
            cloud: cloudSummary({ provider_id: 'prov-hub', provider_name: 'WorkBuddy 国内版', model: 'hy4flash-free', inherit_assistant: true, configured: true }),
        });
        renderPanel();
        expect(await screen.findByText('跟随普通 AI 助手')).toBeTruthy();
        expect(screen.getByText('WorkBuddy 国内版 · hy4flash-free')).toBeTruthy();
        expect(screen.queryByText('自定义')).toBeNull();
    });

    it('shows the MaClaw official pin without a provider chip', async () => {
        mocks.getMaclawLLMProfilePanelState.mockResolvedValue({
            cloud: cloudSummary({ inherit_assistant: false }),
        });
        renderPanel();
        expect(await screen.findByText('MaClaw 官方')).toBeTruthy();
        expect(screen.queryByText('跟随普通 AI 助手')).toBeNull();
    });

    it('navigates to the LLM settings tab from the 设置 button', async () => {
        renderPanel();
        fireEvent.click(await screen.findByRole('button', { name: '设置' }));
        expect(mocks.openSettingsTab).toHaveBeenCalledWith('llm');
    });

    it('falls back to a read-failure note when the panel state is unavailable', async () => {
        mocks.getMaclawLLMProfilePanelState.mockRejectedValue(new Error('offline'));
        renderPanel();
        expect(await screen.findByText('暂时无法读取当前模型设置。')).toBeTruthy();
        expect(screen.queryByRole('button', { name: '设置' })).toBeNull();
    });

    it('opens the install dialog from the entry button and keeps the SSH form working', async () => {
        renderPanel();
        const openButton = await screen.findByText('安装私有云端服务');
        fireEvent.click(openButton);
        expect(screen.getByText('服务器 IP 或主机名')).toBeTruthy();
        // Focus moves into the dialog's first field on open.
        expect(screen.getByLabelText('服务器 IP 或主机名')).toBe(document.activeElement);
        expect(screen.getByText('SSH 端口')).toBeTruthy();
        expect(screen.getByText('SSH 用户名')).toBeTruthy();
        expect(screen.getByText('SSH 密码')).toBeTruthy();

        // Install/Check stay disabled until host, user and password are filled.
        const installButton = screen.getByRole('button', { name: '安装' });
        expect((installButton as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(screen.getByLabelText('服务器 IP 或主机名'), { target: { value: '203.0.113.10' } });
        fireEvent.change(screen.getByLabelText('SSH 用户名'), { target: { value: 'root' } });
        fireEvent.change(screen.getByLabelText('SSH 密码'), { target: { value: 'secret' } });
        expect((installButton as HTMLButtonElement).disabled).toBe(false);

        mocks.probeCloudSrvSSH.mockResolvedValue({ ok: true, method: 'sudo -n true', message: 'sudo ok' });
        fireEvent.click(screen.getByRole('button', { name: '检测 sudo' }));
        await waitFor(() => expect(mocks.probeCloudSrvSSH).toHaveBeenCalledWith({ host: '203.0.113.10', port: 22, user: 'root', password: 'secret' }));
        await waitFor(() => expect(screen.getByText('sudo ok')).toBeTruthy());

        // Editing a field invalidates the stale probe result...
        fireEvent.change(screen.getByLabelText('服务器 IP 或主机名'), { target: { value: '203.0.113.11' } });
        expect(screen.queryByText('sudo ok')).toBeNull();

        // ...and closing the dialog restores focus to the entry button.
        const entry = screen.getByRole('button', { name: '安装私有云端服务' });
        entry.focus();
        fireEvent.click(entry);
        fireEvent.click(screen.getByRole('button', { name: '关闭' }));
        expect(screen.queryByText('SSH 密码')).toBeNull();
        expect(entry).toBe(document.activeElement);
    });

    it('spins the install button while the agent runs and unlocks when the node registers', async () => {
        mocks.deployDesktopNode.mockResolvedValue({ id: 'job-1', status: 'deploying' });
        renderPanel();
        fireEvent.click(await screen.findByText('安装私有云端服务'));
        fireEvent.change(screen.getByLabelText('服务器 IP 或主机名'), { target: { value: '203.0.113.10' } });
        fireEvent.change(screen.getByLabelText('SSH 用户名'), { target: { value: 'root' } });
        fireEvent.change(screen.getByLabelText('SSH 密码'), { target: { value: 'secret' } });
        expect(document.querySelector('.cloud-agent-spinner')).toBeNull();

        fireEvent.click(screen.getByRole('button', { name: '安装' }));
        await waitFor(() => expect(mocks.deployDesktopNode).toHaveBeenCalled());
        // Deploying: spinner + 安装中…, inputs and both actions disabled, so a
        // second install cannot be fired while the agent runs on the server.
        await waitFor(() => expect(screen.getByText(/安装 agent 已在 203.0.113.10 上启动/)).toBeTruthy());
        expect(document.querySelector('.cloud-agent-spinner')).toBeTruthy();
        const installButton = screen.getByRole('button', { name: '安装中…' }) as HTMLButtonElement;
        expect(installButton.disabled).toBe(true);
        expect((screen.getByRole('button', { name: '检测 sudo' }) as HTMLButtonElement).disabled).toBe(true);

        // The node registers on the Hub: the next refresh ends the deploy and
        // preselects the fresh node (still requiring 保存 to switch).
        mocks.getCloudSrvChoice.mockResolvedValue({
            node_id: '',
            nodes: [...baseNodes, { node_id: 'node-new', name: 'fresh-srv', address: '203.0.113.10', online: true }],
        });
        fireEvent.click(screen.getAllByRole('button', { name: '刷新' })[0]);
        await waitFor(() => expect(screen.getByText('安装完成：fresh-srv 已注册到 Hub。')).toBeTruthy());
        expect(screen.queryByText(/安装 agent 已在/)).toBeNull();
        expect(document.querySelector('.cloud-agent-spinner')).toBeNull();
        expect((screen.getByRole('button', { name: '检测 sudo' }) as HTMLButtonElement).disabled).toBe(false);
        // Display value matches the option text, not the node id.
        await waitFor(() => expect(screen.getByDisplayValue(/fresh-srv/)).toBeTruthy());
    });

    it('shows the offline status chip and warning when an offline node is selected', async () => {
        renderPanel();
        await waitFor(() => expect(mocks.getCloudSrvChoice).toHaveBeenCalled());
        fireEvent.click(screen.getByText('私有 maclawsrv'));
        const picker = await screen.findByDisplayValue('选择节点…');
        fireEvent.change(picker, { target: { value: 'node-2' } });
        await waitFor(() => expect(screen.getAllByText('离线').length).toBeGreaterThanOrEqual(1));
        expect(screen.getByText(/链路恢复前云端任务会执行失败/)).toBeTruthy();
    });

    it('saves the private node choice', async () => {
        mocks.setCloudSrvChoice.mockResolvedValue({ node_id: 'node-1', nodes: baseNodes });
        renderPanel();
        await waitFor(() => expect(mocks.getCloudSrvChoice).toHaveBeenCalled());
        fireEvent.click(screen.getByText('私有 maclawsrv'));
        const picker = await screen.findByDisplayValue('选择节点…');
        fireEvent.change(picker, { target: { value: 'node-1' } });
        fireEvent.click(screen.getAllByRole('button', { name: '保存' })[0]);
        await waitFor(() => expect(mocks.setCloudSrvChoice).toHaveBeenCalledWith('node-1'));
        await waitFor(() => expect(screen.getByText('在线')).toBeTruthy());
    });

    it('lists installed srv nodes and supports upgrade and confirmed delete', async () => {
        mocks.listCloudSrvNodes.mockResolvedValue([
            { node_id: 'edge-1', name: 'cloud-1', host: '10.0.0.8', port: 22, user: 'root', registered: true, online: true },
            { node_id: 'edge-2', name: 'cloud-2', host: '10.0.0.9', port: 22, user: 'root', registered: true, online: false, busy: true },
        ]);
        mocks.upgradeCloudSrvNode.mockResolvedValue({ node_id: 'edge-1', name: 'cloud-1', host: '10.0.0.8', user: 'root' });
        mocks.showConfirm.mockResolvedValue(true);
        renderPanel();
        await waitFor(() => expect(mocks.getCloudSrvChoice).toHaveBeenCalled());
        // The installed-node list lives in the deployment card, always visible.
        // Rows show name, address, and live status.
        await waitFor(() => expect(screen.getByText('cloud-1')).toBeTruthy());
        expect(screen.getByText('cloud-2')).toBeTruthy();
        expect(screen.getAllByText('任务进行中…').length).toBeGreaterThanOrEqual(1);

        // 更新 expands the password field; the confirm call carries it.
        fireEvent.click(screen.getAllByRole('button', { name: '更新' })[0]);
        fireEvent.change(screen.getByPlaceholderText('SSH 密码'), { target: { value: 'secret' } });
        fireEvent.click(screen.getByRole('button', { name: '确认升级' }));
        await waitFor(() => expect(mocks.upgradeCloudSrvNode).toHaveBeenCalledWith('edge-1', 'secret'));

        // 删除 goes through the confirm dialog, then unregisters.
        fireEvent.click(screen.getAllByRole('button', { name: '删除' })[0]);
        await waitFor(() => expect(mocks.showConfirm).toHaveBeenCalled());
        await waitFor(() => expect(mocks.deleteCloudSrvNode).toHaveBeenCalledWith('edge-1'));
    });

    it('delete stops when the confirmation is dismissed', async () => {
        mocks.listCloudSrvNodes.mockResolvedValue([
            { node_id: 'edge-1', name: 'cloud-1', host: '10.0.0.8', port: 22, user: 'root', registered: true, online: true },
        ]);
        mocks.showConfirm.mockResolvedValue(false);
        renderPanel();
        await waitFor(() => expect(mocks.getCloudSrvChoice).toHaveBeenCalled());
        await waitFor(() => expect(screen.getByText('cloud-1')).toBeTruthy());
        fireEvent.click(screen.getByRole('button', { name: '删除' }));
        await waitFor(() => expect(mocks.showConfirm).toHaveBeenCalled());
        expect(mocks.deleteCloudSrvNode).not.toHaveBeenCalled();
    });
});
