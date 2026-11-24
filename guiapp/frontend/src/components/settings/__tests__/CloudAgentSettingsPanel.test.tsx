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
    patchConfigFields: vi.fn(),
    listCloudSrvNodes: vi.fn(),
    upgradeCloudSrvNode: vi.fn(),
    deleteCloudSrvNode: vi.fn(),
    showConfirm: vi.fn(),
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
    PatchConfigFields: (...args: unknown[]) => mocks.patchConfigFields(...args),
    ListCloudSrvNodes: (...args: unknown[]) => mocks.listCloudSrvNodes(...args),
    UpgradeCloudSrvNode: (...args: unknown[]) => mocks.upgradeCloudSrvNode(...args),
    DeleteCloudSrvNode: (...args: unknown[]) => mocks.deleteCloudSrvNode(...args),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({ showConfirm: mocks.showConfirm }),
}));

import { CloudAgentSettingsPanel } from '../CloudAgentSettingsPanel';
import { corelib } from '../../../../wailsjs/go/models';

const baseNodes = [
    { node_id: 'node-1', name: 'office-srv', address: '10.0.0.8', online: true },
    { node_id: 'node-2', name: 'home-srv', address: '10.0.0.9', online: false },
];

const baseInstances = [
    { id: 'a1', name: 'builder', kind: 'coding', status: 'running', llm_mode: 'official', llm_label: '', llm_model: '' },
    { id: 'a2', name: 'chat', kind: 'session', status: 'idle', llm_mode: 'custom', llm_label: 'DeepSeek', llm_model: 'deepseek-v3' },
];

const providersPayload = {
    providers: [
        { id: 'prov-1', name: 'DeepSeek', url: 'https://api.deepseek.com', key: 'sk-test', model: 'deepseek-chat', models: ['deepseek-chat', 'deepseek-v3'], is_hub_service: false, protocol: 'openai', agent_type: 'openclaw', context_length: 128000 },
        { id: 'prov-hub', name: 'Hub 内置', url: 'https://hub', key: 'k', model: 'm', is_hub_service: true },
    ],
    current: 'DeepSeek',
};

const emptyConfig = new corelib.AppConfig({ projects: [] });

const renderPanel = (config: corelib.AppConfig | null = emptyConfig) => {
    const setConfig = vi.fn();
    render(<CloudAgentSettingsPanel lang="zh-Hans" showToastMessage={vi.fn()} config={config} setConfig={setConfig} />);
    return { setConfig };
};

const configWithDefault = (provider: string, model: string) => {
    const cfg = new corelib.AppConfig({ projects: [] });
    cfg.cloud_agent_llm_provider = provider;
    cfg.cloud_agent_llm_model = model;
    return cfg;
};

beforeEach(() => {
    vi.clearAllMocks();
    mocks.getCloudSrvChoice.mockResolvedValue({ node_id: '', nodes: baseNodes });
    mocks.listCloudAgents.mockResolvedValue({ enabled: true, instances: baseInstances });
    mocks.getMaclawLLMProviders.mockResolvedValue(providersPayload);
    mocks.listCloudSrvNodes.mockResolvedValue([]);
    mocks.patchConfigFields.mockImplementation((patch: Record<string, unknown>) => {
        const cfg = new corelib.AppConfig({ projects: [] });
        cfg.cloud_agent_llm_provider = String(patch.cloud_agent_llm_provider ?? '');
        cfg.cloud_agent_llm_model = String(patch.cloud_agent_llm_model ?? '');
        return Promise.resolve(cfg);
    });
});

describe('CloudAgentSettingsPanel', () => {
    it('renders sectioned cards, read-only instance rows, and no per-instance save buttons', async () => {
        renderPanel();
        await waitFor(() => expect(mocks.getCloudSrvChoice).toHaveBeenCalled());
        expect(screen.getByText('云端连接')).toBeTruthy();
        expect(screen.getByText('默认模型')).toBeTruthy();
        expect(screen.getByText('私有云端服务')).toBeTruthy();
        expect(screen.getByText('公有 maclawsrv（默认）')).toBeTruthy();
        // Read-only rows keep status/kind/model chips…
        expect(screen.getByText('运行中')).toBeTruthy();
        expect(screen.getByText('空闲')).toBeTruthy();
        expect(screen.getAllByText(/DeepSeek/).length).toBeGreaterThanOrEqual(1);
        // …but expose exactly two 保存 buttons: connection card + default model.
        expect(screen.getAllByRole('button', { name: '保存' }).length).toBe(1);
        expect(screen.getByRole('button', { name: '保存并应用' })).toBeTruthy();
    });

    it('seeds the default-model pick from the saved desktop config', async () => {
        renderPanel(configWithDefault('prov-1', 'deepseek-v3'));
        // Display value matches the option TEXT ("DeepSeek"), while the select
        // value stays the stable provider id ("prov-1").
        await waitFor(() => expect(screen.getByDisplayValue('DeepSeek')).toBeTruthy());
        await waitFor(() => expect(screen.getByDisplayValue('deepseek-v3')).toBeTruthy());
    });

    it('saves a custom default, persists config, and fans out to every instance', async () => {
        // Model the Hub truthfully: the instance list reflects each applied LLM,
        // so the panel's post-save refresh keeps the updated rows.
        const hubInstances = baseInstances.map((row) => ({ ...row }));
        mocks.setCloudAgentLLM.mockImplementation((id: string, input: { label: string; model: string }) => {
            const row = hubInstances.find((r) => r.id === id);
            if (row) {
                row.llm_mode = 'custom';
                row.llm_label = input.label;
                row.llm_model = input.model;
            }
            return Promise.resolve({ id, name: 'x', kind: 'session', status: 'idle', llm_mode: 'custom', llm_label: input.label, llm_model: input.model });
        });
        mocks.listCloudAgents.mockResolvedValue({ enabled: true, instances: hubInstances });

        renderPanel();
        await waitFor(() => expect(screen.getByRole('button', { name: '保存并应用' })).toBeTruthy());

        const providerSelect = screen.getAllByRole('combobox')[0];
        fireEvent.change(providerSelect, { target: { value: 'prov-1' } });
        // Provider pick auto-advances the model to the provider's first model.
        await waitFor(() => expect(screen.getByDisplayValue('deepseek-chat')).toBeTruthy());

        fireEvent.click(screen.getByRole('button', { name: '保存并应用' }));
        await waitFor(() => expect(mocks.patchConfigFields).toHaveBeenCalledWith({
            cloud_agent_llm_provider: 'prov-1',
            cloud_agent_llm_model: 'deepseek-chat',
        }));
        // Both instances move to the custom default; official rows get pushed too.
        await waitFor(() => expect(mocks.setCloudAgentLLM).toHaveBeenCalledTimes(2));
        expect(mocks.setCloudAgentLLM).toHaveBeenCalledWith('a1', {
            url: 'https://api.deepseek.com', key: 'sk-test', model: 'deepseek-chat', label: 'DeepSeek',
            protocol: 'openai', wire_api: '', agent_type: 'openclaw', context_length: 128000,
            timeout_sec: 0, max_output_tokens: 0,
        });
        expect(mocks.resetCloudAgentLLM).not.toHaveBeenCalled();
        // After the refresh, instances reflect the applied model in their chips.
        await waitFor(() => expect(screen.getAllByText('DeepSeek · deepseek-chat').length).toBe(2));
    });

    it('switching back to official resets only the custom instances', async () => {
        renderPanel(configWithDefault('prov-1', 'deepseek-v3'));
        await waitFor(() => expect(screen.getByDisplayValue('deepseek-v3')).toBeTruthy());
        const providerSelect = screen.getAllByRole('combobox')[0];
        fireEvent.change(providerSelect, { target: { value: '' } });
        fireEvent.click(screen.getByRole('button', { name: '保存并应用' }));
        await waitFor(() => expect(mocks.patchConfigFields).toHaveBeenCalledWith({
            cloud_agent_llm_provider: '',
            cloud_agent_llm_model: '',
        }));
        await waitFor(() => expect(mocks.resetCloudAgentLLM).toHaveBeenCalledTimes(1));
        expect(mocks.resetCloudAgentLLM).toHaveBeenCalledWith('a2');
        expect(mocks.setCloudAgentLLM).not.toHaveBeenCalled();
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
        // The first 保存 button is the connection card's (the default model uses 保存并应用).
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
