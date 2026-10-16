import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { MISDataSettingsPanel } from '../MISDataSettingsPanel';

const getMISDataConfigMock = vi.hoisted(() => vi.fn());
const saveMISDataConfigMock = vi.hoisted(() => vi.fn());
const testMISDataConnectionMock = vi.hoisted(() => vi.fn());

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetMISDataConfig: (...args: unknown[]) => getMISDataConfigMock(...args),
    SaveMISDataConfig: (...args: unknown[]) => saveMISDataConfigMock(...args),
    TestMISDataConnection: (...args: unknown[]) => testMISDataConnectionMock(...args),
}));

describe('MISDataSettingsPanel', () => {
    beforeEach(() => {
        getMISDataConfigMock.mockReset().mockResolvedValue({
            enabled: false,
            endpoint: 'http://127.0.0.1:18180',
            token: 'mcd_test',
            tenant_id: 'default',
            user_id: 'maclaw',
            role: 'data_user',
        });
        saveMISDataConfigMock.mockReset().mockResolvedValue(undefined);
        testMISDataConnectionMock.mockReset();
    });

    it('does not mark MIS data as enabled when saving before a successful connection test', async () => {
        render(<MISDataSettingsPanel lang="en" />);

        await screen.findByRole('heading', { name: 'MIS Data' });
        fireEvent.click(screen.getByLabelText('Enable MIS data tools for MaClaw agents'));
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => expect(saveMISDataConfigMock).toHaveBeenCalledTimes(1));
        expect(saveMISDataConfigMock).toHaveBeenCalledWith(expect.objectContaining({ enabled: false }));
        expect(screen.getByText('Disabled')).toBeTruthy();
        expect(screen.getByText(/Test connection successfully before enabling/)).toBeTruthy();
    });

    it('shows verified pending save after a successful test, then enabled after save', async () => {
        testMISDataConnectionMock.mockResolvedValue({ ok: true, auth_ok: true, status: 'ready', engine: 'sqlite', schema_version: 1 });
        render(<MISDataSettingsPanel lang="en" />);

        await screen.findByRole('heading', { name: 'MIS Data' });
        fireEvent.click(screen.getByLabelText('Enable MIS data tools for MaClaw agents'));
        fireEvent.click(screen.getByRole('button', { name: 'Test Connection' }));

        await screen.findByText('Connection verified');
        expect(screen.queryByText('Enabled')).toBeNull();
        expect(screen.getByText('Verified, save to enable')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));
        await waitFor(() => expect(saveMISDataConfigMock).toHaveBeenCalledWith(expect.objectContaining({ enabled: true })));
        expect(screen.getByText('Enabled')).toBeTruthy();
        expect(screen.getByText('Enabled and saved')).toBeTruthy();
    });

    it('keeps MIS data disabled when the connection test fails', async () => {
        testMISDataConnectionMock.mockResolvedValue({ ok: true, auth_ok: false, error: 'authenticated API returned HTTP 401' });
        render(<MISDataSettingsPanel lang="en" />);

        await screen.findByRole('heading', { name: 'MIS Data' });
        fireEvent.click(screen.getByLabelText('Enable MIS data tools for MaClaw agents'));
        fireEvent.click(screen.getByRole('button', { name: 'Test Connection' }));

        await screen.findByText('authenticated API returned HTTP 401');
        expect(screen.getByText('Disabled')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));
        await waitFor(() => expect(saveMISDataConfigMock).toHaveBeenCalledWith(expect.objectContaining({ enabled: false })));
    });

    it('returns to disabled when a saved enabled connection is edited', async () => {
        testMISDataConnectionMock.mockResolvedValue({ ok: true, auth_ok: true, status: 'ready', engine: 'sqlite', schema_version: 1 });
        render(<MISDataSettingsPanel lang="en" />);

        await screen.findByRole('heading', { name: 'MIS Data' });
        fireEvent.click(screen.getByLabelText('Enable MIS data tools for MaClaw agents'));
        fireEvent.click(screen.getByRole('button', { name: 'Test Connection' }));
        await screen.findByText('Verified, save to enable');
        fireEvent.click(screen.getByRole('button', { name: 'Save' }));
        await screen.findByText('Enabled');

        fireEvent.change(screen.getByPlaceholderText('http://127.0.0.1:18180'), { target: { value: 'http://127.0.0.1:18181' } });

        expect(screen.getByText('Disabled')).toBeTruthy();
        expect(screen.queryByText('Enabled')).toBeNull();
    });
});
