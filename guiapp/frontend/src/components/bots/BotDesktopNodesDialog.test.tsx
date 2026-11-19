import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { BotDesktopNodesDialog } from './BotDesktopNodesDialog';

const { listNodes, upgradeNode, deployNode, configureToken, redeployNode, clearResult, activateNode, useCloud } = vi.hoisted(() => ({
    listNodes: vi.fn(),
    upgradeNode: vi.fn(),
    deployNode: vi.fn(),
    configureToken: vi.fn(),
    redeployNode: vi.fn(),
    clearResult: vi.fn(),
    activateNode: vi.fn(),
    useCloud: vi.fn(),
}));

vi.mock('../../../wailsjs/go/main/App', () => ({
    ListDesktopNodes: () => listNodes(),
    UpgradeDesktopNode: (id: string, password: string) => upgradeNode(id, password),
    DeployDesktopNode: (req: unknown) => deployNode(req),
    ConfigureDesktopNodeToken: (id: string, password: string) => configureToken(id, password),
    RedeployDesktopNode: (id: string, password: string, withSrv: boolean) => redeployNode(id, password, withSrv),
    ClearDesktopNodeResult: (id: string) => clearResult(id),
    ActivateDesktopNode: (id: string) => activateNode(id),
    UseCloudDesktop: () => useCloud(),
}));

vi.mock('../../../wailsjs/runtime', () => ({
    EventsOn: () => () => {},
}));

const sshNode = {
    id: 'dn_1',
    name: 'lab',
    mode: 'ssh',
    host: '10.0.0.8',
    port: 22,
    user: 'deploy',
    status: 'ready',
    registered: true,
    online: true,
    active: true,
};

describe('intranet desktop update', () => {
    beforeEach(() => {
        listNodes.mockReset();
        upgradeNode.mockReset();
        deployNode.mockReset();
        configureToken.mockReset();
        redeployNode.mockReset();
        clearResult.mockReset();
        activateNode.mockReset();
        useCloud.mockReset();
        activateNode.mockResolvedValue(undefined);
        useCloud.mockResolvedValue(undefined);
        listNodes.mockResolvedValue({ enabled: true, nodes: [sshNode] });
        upgradeNode.mockResolvedValue({ ...sshNode, status: 'upgrading' });
    });

    it('puts an update button beside a configured desktop and sends the password to the agent job', async () => {
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        expect(await screen.findByText('lab')).toBeTruthy();
        expect(screen.getByText(/deploy@10\.0\.0\.8/)).toBeTruthy();
        fireEvent.click(screen.getByTestId('bot-desktop-update-dn_1'));
        fireEvent.change(screen.getByTestId('bot-desktop-update-password-dn_1'), { target: { value: 's3cret' } });
        fireEvent.click(screen.getByTestId('bot-desktop-update-go-dn_1'));
        await waitFor(() => expect(upgradeNode).toHaveBeenCalledWith('dn_1', 's3cret'));
    });

    it('keeps a failed update separate from a hub warning', async () => {
        listNodes.mockResolvedValue({ enabled: true, hub_error: 'hub down', nodes: [sshNode] });
        upgradeNode.mockRejectedValue(new Error('SSH 连接失败'));
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        expect(await screen.findByTestId('bot-desktop-nodes-hub')).toBeTruthy();
        fireEvent.click(screen.getByTestId('bot-desktop-update-dn_1'));
        fireEvent.change(screen.getByTestId('bot-desktop-update-password-dn_1'), { target: { value: 's3cret' } });
        fireEvent.click(screen.getByTestId('bot-desktop-update-go-dn_1'));
        expect(await screen.findByTestId('bot-desktop-nodes-notice')).toBeTruthy();
        expect(screen.getByTestId('bot-desktop-nodes-notice').textContent).toContain('SSH 连接失败');
        expect(screen.getByTestId('bot-desktop-nodes-hub').textContent).toContain('hub down');
    });

    it('does not offer update when the desktop has no SSH login', async () => {
        listNodes.mockResolvedValue({
            enabled: true,
            nodes: [{ id: 'hub-s1', name: 'other', mode: '', status: 'ready', registered: true, online: false }],
        });
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        const button = await screen.findByTestId('bot-desktop-update-hub-s1') as HTMLButtonElement;
        expect(button.disabled).toBe(true);
        expect(screen.getByText(/没有保存 SSH/)).toBeTruthy();
    });

    it('sends desktop-only unless MaClawSrv is chosen', async () => {
        deployNode.mockResolvedValue({ id: 'dn_2', status: 'deploying' });
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('bot-desktop-deploy-toggle'));
        fireEvent.change(screen.getByTestId('bot-desktop-deploy-host'), { target: { value: '10.0.0.9' } });
        fireEvent.change(screen.getByTestId('bot-desktop-deploy-user'), { target: { value: 'deploy' } });
        fireEvent.change(screen.getByTestId('bot-desktop-deploy-password'), { target: { value: 's3cret' } });
        fireEvent.click(screen.getByTestId('bot-desktop-deploy-go'));
        await waitFor(() => expect(deployNode).toHaveBeenCalledWith(expect.objectContaining({ host: '10.0.0.9', with_srv: false })));

        fireEvent.click(screen.getByTestId('bot-desktop-deploy-toggle'));
        fireEvent.click(screen.getByTestId('bot-desktop-install-srv'));
        expect(screen.getByText(/不会显示出来/)).toBeTruthy();
        fireEvent.change(screen.getByTestId('bot-desktop-deploy-password'), { target: { value: 's3cret' } });
        fireEvent.click(screen.getByTestId('bot-desktop-deploy-go'));
        await waitFor(() => expect(deployNode).toHaveBeenLastCalledWith(expect.objectContaining({ with_srv: true })));
    });

    it('keeps a deploy in the background and shows a result that can be cleared', async () => {
        listNodes.mockResolvedValue({
            enabled: true,
            nodes: [{ ...sshNode, status: 'deploying', message: 'agent 正在部署 desktopd', node_id: '', registered: false, online: false, active: false }],
        });
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        expect(await screen.findByTestId('bot-desktop-background')).toBeTruthy();
        expect(screen.getByTestId('bot-desktop-update-dn_1').textContent).toContain('部署中');
        expect(screen.getAllByRole('button', { name: '关闭' }).some(button => (button as HTMLButtonElement).disabled)).toBe(false);
        cleanup();
        listNodes.mockResolvedValue({
            enabled: true,
            nodes: [{ ...sshNode, last_kind: 'deploy', last_result: '部署完成。', status: 'ready' }],
        });
        clearResult.mockResolvedValue(undefined);
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        expect(await screen.findByTestId('bot-desktop-result-dn_1')).toBeTruthy();
        fireEvent.click(screen.getByTestId('bot-desktop-clear-dn_1'));
        await waitFor(() => expect(clearResult).toHaveBeenCalledWith('dn_1'));
    });

    it('asks what to install when deploying a desktop that is not registered yet', async () => {
        listNodes.mockResolvedValue({
            enabled: true,
            nodes: [{ ...sshNode, status: 'failed', message: '失败', node_id: '', registered: false, online: false, active: false, last_result: '失败' }],
        });
        redeployNode.mockResolvedValue({ id: 'dn_1', status: 'deploying' });
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('bot-desktop-update-dn_1'));
        fireEvent.click(screen.getByTestId('bot-desktop-row-srv-dn_1'));
        fireEvent.change(screen.getByTestId('bot-desktop-update-password-dn_1'), { target: { value: 's3cret' } });
        fireEvent.click(screen.getByTestId('bot-desktop-update-go-dn_1'));
        await waitFor(() => expect(redeployNode).toHaveBeenCalledWith('dn_1', 's3cret', true));
    });

    it('configures the srv token from the desktop row', async () => {
        listNodes.mockResolvedValue({ enabled: true, nodes: [{ ...sshNode, with_srv: true, srv_token_set: false }] });
        configureToken.mockResolvedValue({ ...sshNode, with_srv: true, srv_token_set: true });
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        expect(await screen.findByText(/令牌未配置/)).toBeTruthy();
        fireEvent.click(screen.getByTestId('bot-desktop-token-dn_1'));
        fireEvent.change(screen.getByTestId('bot-desktop-update-password-dn_1'), { target: { value: 's3cret' } });
        fireEvent.click(screen.getByTestId('bot-desktop-update-go-dn_1'));
        await waitFor(() => expect(configureToken).toHaveBeenCalledWith('dn_1', 's3cret'));
    });

    it('shows the finished-run local time next to the last-result label', async () => {
        listNodes.mockResolvedValue({
            enabled: true,
            nodes: [{ ...sshNode, last_kind: 'deploy', last_result: '部署完成。', last_at: '2026-11-15T08:30:00Z' }],
        });
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        expect(await screen.findByTestId('bot-desktop-result-dn_1')).toBeTruthy();
        // RFC3339 原文（含 T/Z）不直出；渲染成当地 YYYY-MM-DD HH:mm。
        expect(screen.getByTestId('bot-desktop-result-dn_1').textContent).toMatch(/20\d{2}-\d{2}-\d{2} \d{2}:\d{2}/);
    });

    it('shows the desktopd port beside each desktop and flags a loopback bind', async () => {
        listNodes.mockResolvedValue({
            enabled: true,
            nodes: [
                { ...sshNode, desktopd_port: 18081, desktopd_addr: ':18081' },
                { ...sshNode, id: 'dn_2', name: 'old', desktopd_addr: '127.0.0.1:18081', active: false },
                { ...sshNode, id: 'dn_v6', name: 'v6', desktopd_addr: '[::1]:18081', active: false },
                { ...sshNode, id: 'dn_4', name: 'custom', desktopd_addr: '10.0.0.5:18082', active: false },
            ],
        });
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        expect(await screen.findByTestId('bot-desktop-port-dn_1')).toBeTruthy();
        expect(screen.getByTestId('bot-desktop-port-dn_1').textContent).toBe('desktopd :18081');
        expect(screen.getByTestId('bot-desktop-port-dn_2').textContent).toBe('desktopd 127.0.0.1:18081');
        expect(screen.getByTestId('bot-desktop-port-dn_2').getAttribute('title')).toBe('只监听本机——要直连需改为内网绑定');
        expect(screen.getByTestId('bot-desktop-port-dn_1').getAttribute('title')).toBeNull();
        // A bracketed IPv6 loopback keeps its brackets in the label and is flagged too.
        expect(screen.getByTestId('bot-desktop-port-dn_v6').textContent).toBe('desktopd [::1]:18081');
        expect(screen.getByTestId('bot-desktop-port-dn_v6').getAttribute('title')).toBe('只监听本机——要直连需改为内网绑定');
        // No desktopd_port field: the port still comes out of the stored bind.
        expect(screen.getByTestId('bot-desktop-port-dn_4').textContent).toBe('desktopd 10.0.0.5:18082');
        expect(screen.getByText(/放行.*18081/)).toBeTruthy();
    });

    it('reads the desktopd port out of a direct-mode URL instead of duplicating it', async () => {
        listNodes.mockResolvedValue({
            enabled: true,
            nodes: [{ id: 'dn_3', name: 'near', mode: 'direct', desktopd_url: 'http://10.0.0.9:18082', status: 'ready', registered: true, online: true }],
        });
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        expect(await screen.findByText('near')).toBeTruthy();
        expect(screen.queryByTestId('bot-desktop-port-dn_3')).toBeNull();
        expect(screen.getByText('http://10.0.0.9:18082')).toBeTruthy();
    });

    it('switches the computer in use between this desktop and the cloud one', async () => {
        const idle = { ...sshNode, id: 'dn_2', name: 'spare', active: false, registered: true };
        listNodes.mockResolvedValue({ enabled: true, nodes: [sshNode, idle] });
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        fireEvent.click(await screen.findByTestId('bot-desktop-cloud-dn_1'));
        await waitFor(() => expect(useCloud).toHaveBeenCalled());
        fireEvent.click(screen.getByTestId('bot-desktop-use-dn_2'));
        await waitFor(() => expect(activateNode).toHaveBeenCalledWith('dn_2'));
    });

    it('does not render the target twice when the node has no name', async () => {
        listNodes.mockResolvedValue({ enabled: true, nodes: [{ ...sshNode, name: '' }] });
        render(<BotDesktopNodesDialog heading="设置" lang="zh" onClose={() => {}} />);
        // 名称缺省时目标充当名称；再渲染一个目标 span 就会重复同一行文字。
        expect(await screen.findAllByText(/deploy@10\.0\.0\.8/)).toHaveLength(1);
    });
});
