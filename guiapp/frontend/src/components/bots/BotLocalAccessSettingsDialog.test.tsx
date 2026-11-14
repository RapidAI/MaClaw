import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { BotLocalAccessSettingsDialog } from './BotLocalAccessSettingsDialog';

const { getAccess, saveAccess } = vi.hoisted(() => ({
    getAccess: vi.fn(),
    saveAccess: vi.fn(),
}));

vi.mock('../../../wailsjs/go/main/App', () => ({
    GetBotLocalAccess: () => getAccess(),
    SaveBotLocalAccess: (settings: unknown) => saveAccess(settings),
    PickBotLocalAccessFolder: () => Promise.resolve(''),
}));

const settings = {
    files_enabled: true,
    roots: [{ path: 'C:\\Users\\ma139\\MaClaw', write: true }],
    max_transfer_mb: 200,
    network_enabled: true,
    net_allow: [],
    tunnel_idle_minutes: 30,
    confirm_seconds: 120,
    approved: [],
};

describe('Bot local access permission', () => {
    beforeEach(() => {
        getAccess.mockReset();
        saveAccess.mockReset();
        saveAccess.mockImplementation(async (next: unknown) => ({ settings: next, connected: true, home: 'C:\\Users\\ma139' }));
    });

    it('defaults to ask when an older policy has no permission', async () => {
        getAccess.mockResolvedValue({ settings, connected: true, home: 'C:\\Users\\ma139' });
        render(<BotLocalAccessSettingsDialog heading="设置" lang="zh" onClose={() => {}} />);
        const ask = await screen.findByTestId('bot-local-permission-ask') as HTMLInputElement;
        expect(ask.checked).toBe(true);
        expect((screen.getByTestId('bot-local-permission-full') as HTMLInputElement).checked).toBe(false);
        expect((screen.getByTestId('bot-local-confirm') as HTMLInputElement).disabled).toBe(false);
        expect(screen.getByTestId('bot-local-permission').getAttribute('role')).toBe('radiogroup');
        expect(screen.getByText('完全权限')).toBeTruthy();
    });

    it('saves full access and no longer waits for an answer', async () => {
        getAccess.mockResolvedValue({ settings: { ...settings, permission: 'ask' }, connected: false, home: '' });
        const onClose = vi.fn();
        render(<BotLocalAccessSettingsDialog heading="设置" lang="zh" onClose={onClose} />);
        fireEvent.click(await screen.findByTestId('bot-local-permission-full'));
        expect((screen.getByTestId('bot-local-permission-full') as HTMLInputElement).checked).toBe(true);
        expect(screen.queryByTestId('bot-local-confirm')).toBeNull();
        fireEvent.click(screen.getByTestId('bot-local-save'));
        await waitFor(() => expect(saveAccess).toHaveBeenCalled());
        expect(saveAccess.mock.calls[0][0]).toMatchObject({ permission: 'full', files_enabled: true, network_enabled: true });
        await waitFor(() => expect(onClose).toHaveBeenCalled());
    });
});
