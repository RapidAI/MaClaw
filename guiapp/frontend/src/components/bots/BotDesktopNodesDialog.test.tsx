import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { BotDesktopNodesDialog } from './BotDesktopNodesDialog';

const { listNodes, upgradeNode, deployNode } = vi.hoisted(() => ({
    listNodes: vi.fn(),
    upgradeNode: vi.fn(),
    deployNode: vi.fn(),
}));

vi.mock('../../../wailsjs/go/main/App', () => ({
    ListDesktopNodes: () => listNodes(),
    UpgradeDesktopNode: (id: string, password: string) => upgradeNode(id, password),
    DeployDesktopNode: (req: unknown) => deployNode(req),
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
});
