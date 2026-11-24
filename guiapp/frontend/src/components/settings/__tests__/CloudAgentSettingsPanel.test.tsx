// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({
    getCloudSrvChoice: vi.fn(),
    listCloudAgents: vi.fn(),
    getMaclawLLMProviders: vi.fn(),
    setCloudSrvChoice: vi.fn(),
    probeCloudSrvSSH: vi.fn(),
    deployDesktopNode: vi.fn(),
    setCloudAgentLLM: vi.fn(),
    resetCloudAgentLLM: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetCloudSrvChoice: (...args: unknown[]) => mocks.getCloudSrvChoice(...args),
    ListCloudAgents: (...args: unknown[]) => mocks.listCloudAgents(...args),
    GetMaclawLLMProviders: (...args: unknown[]) => mocks.getMaclawLLMProviders(...args),
    SetCloudSrvChoice: (...args: unknown[]) => mocks.setCloudSrvChoice(...args),
    ProbeCloudSrvSSH: (...args: unknown[]) => mocks.probeCloudSrvSSH(...args),
    DeployDesktopNode: (...args: unknown[]) => mocks.deployDesktopNode(...args),
    SetCloudAgentLLM: (...args: unknown[]) => mocks.setCloudAgentLLM(...args),
    ResetCloudAgentLLM: (...args: unknown[]) => mocks.resetCloudAgentLLM(...args),
}));

import { CloudAgentSettingsPanel } from '../CloudAgentSettingsPanel';

const baseNodes = [
    { node_id: 'node-1', name: 'office-srv', address: '10.0.0.8', online: true },
    { node_id: 'node-2', name: 'home-srv', address: '10.0.0.9', online: false },
];

const renderPanel = () => render(
    <CloudAgentSettingsPanel lang="zh-Hans" showToastMessage={vi.fn()} />,
);

beforeEach(() => {
    vi.clearAllMocks();
    mocks.getCloudSrvChoice.mockResolvedValue({ node_id: '', nodes: baseNodes });
    mocks.listCloudAgents.mockResolvedValue({
        enabled: true,
        instances: [
            { id: 'a1', name: 'builder', kind: 'coding', status: 'running', llm_mode: 'official', llm_label: '', llm_model: '' },
            { id: 'a2', name: 'chat', kind: 'session', status: 'idle', llm_mode: 'custom', llm_label: 'DeepSeek', llm_model: 'deepseek-v3' },
        ],
    });
    mocks.getMaclawLLMProviders.mockResolvedValue({
        providers: [
            { name: 'DeepSeek', url: 'https://api.deepseek.com', key: 'sk-test', model: 'deepseek-chat', models: ['deepseek-chat', 'deepseek-v3'], is_hub_service: false },
            { name: 'Hub 内置', url: 'https://hub', key: 'k', model: 'm', is_hub_service: true },
        ],
        current: 'DeepSeek',
    });
});

describe('CloudAgentSettingsPanel', () => {
    it('renders sectioned cards with choice tiles', async () => {
        renderPanel();
        await waitFor(() => expect(mocks.getCloudSrvChoice).toHaveBeenCalled());
        expect(screen.getByText('云端连接')).toBeTruthy();
        expect(screen.getByText('实例模型')).toBeTruthy();
        expect(screen.getByText('私有云端服务')).toBeTruthy();
        expect(screen.getByText('公有 maclawsrv（默认）')).toBeTruthy();
        expect(screen.getByText('私有 maclawsrv')).toBeTruthy();
        // Current effective model chip is shown per instance row.
        expect(screen.getAllByText('MaClaw 官方（默认）').length).toBeGreaterThanOrEqual(1);
        expect(screen.getAllByText(/DeepSeek/).length).toBeGreaterThanOrEqual(1);
        // Runtime status chips: running → 运行中, idle → 空闲.
        expect(screen.getByText('运行中')).toBeTruthy();
        expect(screen.getByText('空闲')).toBeTruthy();
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
        // The first 保存 button is the connection card's (instance rows have their own).
        fireEvent.click(screen.getAllByRole('button', { name: '保存' })[0]);
        await waitFor(() => expect(mocks.setCloudSrvChoice).toHaveBeenCalledWith('node-1'));
        await waitFor(() => expect(screen.getByText('在线')).toBeTruthy());
    });
});
