// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
    StartUserDataMigrationCleanup,
    StartUserDataMigrationExport,
    StartUserDataMigrationImport,
    UserDataMigrationInstances,
    UserDataMigrationStatus,
} from '../../../../wailsjs/go/main/App';
import { MigrationSettingsPanel } from '../MigrationSettingsPanel';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetUserDataMigrationJob: vi.fn(async () => ({ id: 'job-1', status: 'succeeded', progress: 1 })),
    StartUserDataMigrationCleanup: vi.fn(async () => ({ id: 'job-cleanup', kind: 'migration.import.cleanup', status: 'running' })),
    StartUserDataMigrationExport: vi.fn(async () => ({ id: 'job-export', kind: 'migration.export', status: 'running' })),
    StartUserDataMigrationImport: vi.fn(async () => ({ id: 'job-import', kind: 'migration.import', status: 'running' })),
    UserDataMigrationInstances: vi.fn(),
    UserDataMigrationStatus: vi.fn(),
}));

const baseStatus = {
    configured: true,
    hub_url: 'https://hub.example.test',
    tenant_id: 'tenant-a',
    tenant_name: 'Tenant A',
    user_id: 'user-a',
    machine_id: 'machine-current',
    machine_name: 'Current Workstation',
    max_compressed_bytes: 100 * 1024 * 1024,
};

const migrationInstance = (status: string, claimedBy = '') => ({
    instance_id: 'machine-source',
    machine_id: 'machine-source',
    machine_name: 'Source Laptop',
    has_export: true,
    export_id: 'mig-source',
    export_status: status,
    export_claimed_by_machine_id: claimedBy,
    export_size: 2048,
    export_updated_at: '2026-06-20T12:00:00Z',
    export_manifest: {
        version: 'maclaw-gui-user-data-migration/v2',
        config_schema_version: 'corelib.AppConfig/v1',
        config_section_count: 24,
        secret_count: 5,
        memory_entries: 18,
    },
});

const renderMigrationPanel = async (instances: unknown[]) => {
    (UserDataMigrationStatus as any).mockResolvedValue(baseStatus);
    (UserDataMigrationInstances as any).mockResolvedValue({ instances });

    render(<MigrationSettingsPanel lang="en" showToastMessage={vi.fn()} />);

    await screen.findByRole('heading', { name: 'Move In' });
    await waitFor(() => expect(UserDataMigrationInstances).toHaveBeenCalledTimes(1));
};

const importPasswordInput = () => screen.getAllByLabelText('Password')[1] as HTMLInputElement;

beforeEach(() => {
    vi.clearAllMocks();
});

afterEach(() => {
    cleanup();
});

describe('MigrationSettingsPanel', () => {
    it('requires a strong password for new move-out packages but keeps legacy import passwords usable', async () => {
        await renderMigrationPanel([migrationInstance('ready')]);

        fireEvent.change(screen.getAllByLabelText('Password')[0], { target: { value: 'weak1' } });
        fireEvent.change(screen.getByLabelText('Confirm Password'), { target: { value: 'weak1' } });
        expect((screen.getByRole('button', { name: 'Start Move Out' }) as HTMLButtonElement).disabled).toBe(true);

        const elevenRunes = 'abcdefghi1😀';
        fireEvent.change(screen.getAllByLabelText('Password')[0], { target: { value: elevenRunes } });
        fireEvent.change(screen.getByLabelText('Confirm Password'), { target: { value: elevenRunes } });
        expect((screen.getByRole('button', { name: 'Start Move Out' }) as HTMLButtonElement).disabled).toBe(true);

        fireEvent.change(importPasswordInput(), { target: { value: 'old1' } });
        const importButton = screen.getByRole('button', { name: 'Start Move In' }) as HTMLButtonElement;
        await waitFor(() => expect(importButton.disabled).toBe(false));
    });

    it('does not load instances when Hub status already reports a configuration error', async () => {
        (UserDataMigrationStatus as any).mockResolvedValue({
            ...baseStatus,
            configuration_reason: 'Hub unavailable',
        });
        (UserDataMigrationInstances as any).mockResolvedValue({ instances: [migrationInstance('ready')] });

        render(<MigrationSettingsPanel lang="en" showToastMessage={vi.fn()} />);

        expect(await screen.findByText('Hub unavailable')).toBeTruthy();
        expect(UserDataMigrationInstances).not.toHaveBeenCalled();
    });

    it('starts a normal move-in for a ready package after password entry', async () => {
        await renderMigrationPanel([migrationInstance('ready')]);

        expect(screen.getByText('Preflight')).toBeTruthy();
        expect(screen.getByText(/24 settings, 5 secrets, and 18 memories/)).toBeTruthy();

        fireEvent.change(importPasswordInput(), { target: { value: 'secret-pass-2026' } });
        const button = screen.getByRole('button', { name: 'Start Move In' }) as HTMLButtonElement;
        await waitFor(() => expect(button.disabled).toBe(false));
        fireEvent.click(button);

        await waitFor(() => expect(StartUserDataMigrationImport).toHaveBeenCalledWith('mig-source', 'secret-pass-2026'));
        expect(StartUserDataMigrationCleanup).not.toHaveBeenCalled();
    });

    it('shows only machines that have migration packages in the package list', async () => {
        await renderMigrationPanel([
            migrationInstance('ready'),
            {
                instance_id: 'machine-empty',
                machine_id: 'machine-empty',
                machine_name: 'No Package PC',
                has_export: false,
            },
        ]);

        expect(screen.getByText('Source Laptop')).toBeTruthy();
        expect(screen.queryByText('No Package PC')).toBeNull();
        expect(screen.queryByText('None')).toBeNull();
    });

    it('shows move-out progress directly after the move-out action row', async () => {
        await renderMigrationPanel([migrationInstance('ready')]);

        fireEvent.change(screen.getAllByLabelText('Password')[0], { target: { value: 'secret-pass-2026' } });
        fireEvent.change(screen.getByLabelText('Confirm Password'), { target: { value: 'secret-pass-2026' } });
        const moveOutButton = screen.getByRole('button', { name: 'Start Move Out' }) as HTMLButtonElement;
        await waitFor(() => expect(moveOutButton.disabled).toBe(false));
        fireEvent.click(moveOutButton);

        await waitFor(() => expect(StartUserDataMigrationExport).toHaveBeenCalledWith('secret-pass-2026', 'secret-pass-2026', true));
        const section = screen.getByRole('heading', { name: 'Move Out' }).closest('section') as HTMLElement;
        const progress = within(section).getByText('Move-out Progress').closest('.migration-progress-inline') as HTMLElement;

        expect(Boolean(moveOutButton.compareDocumentPosition(progress) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
    });

    it('shows move-in progress directly after the move-in action row and before the package table', async () => {
        await renderMigrationPanel([migrationInstance('ready')]);

        fireEvent.change(importPasswordInput(), { target: { value: 'secret-pass-2026' } });
        const moveInButton = screen.getByRole('button', { name: 'Start Move In' }) as HTMLButtonElement;
        await waitFor(() => expect(moveInButton.disabled).toBe(false));
        fireEvent.click(moveInButton);

        await waitFor(() => expect(StartUserDataMigrationImport).toHaveBeenCalledWith('mig-source', 'secret-pass-2026'));
        const section = screen.getByRole('heading', { name: 'Move In' }).closest('section') as HTMLElement;
        const progress = within(section).getByText('Move-in Progress').closest('.migration-progress-inline') as HTMLElement;
        const table = section.querySelector('.migration-instance-table-wrap') as HTMLElement;

        expect(Boolean(moveInButton.compareDocumentPosition(progress) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
        expect(Boolean(progress.compareDocumentPosition(table) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
    });

    it('localizes migration process errors instead of showing raw backend text', async () => {
        (StartUserDataMigrationImport as any).mockRejectedValueOnce(new Error('migration password is incorrect or package is corrupted'));
        (UserDataMigrationStatus as any).mockResolvedValue(baseStatus);
        (UserDataMigrationInstances as any).mockResolvedValue({ instances: [migrationInstance('ready')] });

        render(<MigrationSettingsPanel lang="zh-Hans" showToastMessage={vi.fn()} />);
        await screen.findByRole('heading', { name: '迁入' });

        fireEvent.change(screen.getAllByLabelText('密码')[1], { target: { value: 'wrong-pass-2026' } });
        const button = screen.getByRole('button', { name: '开始迁入' }) as HTMLButtonElement;
        await waitFor(() => expect(button.disabled).toBe(false));
        fireEvent.click(button);

        expect(await screen.findByText('密码不正确，或迁移包已损坏。')).toBeTruthy();
        expect(screen.queryByText(/migration password is incorrect/i)).toBeNull();
    });

    it('allows the current machine to resume an importing package with the password', async () => {
        await renderMigrationPanel([migrationInstance('importing', 'machine-current')]);

        expect(screen.getByText(/already claimed by this machine/i)).toBeTruthy();
        fireEvent.change(importPasswordInput(), { target: { value: 'resume-pass-2026' } });
        const button = screen.getByRole('button', { name: 'Resume Move In' }) as HTMLButtonElement;
        await waitFor(() => expect(button.disabled).toBe(false));
        fireEvent.click(button);

        await waitFor(() => expect(StartUserDataMigrationImport).toHaveBeenCalledWith('mig-source', 'resume-pass-2026'));
        expect(StartUserDataMigrationCleanup).not.toHaveBeenCalled();
    });

    it('switches an importing package to cleanup retry after local restore succeeds with cleanup pending', async () => {
        (StartUserDataMigrationImport as any).mockResolvedValueOnce({
            id: 'job-import-pending-cleanup',
            kind: 'migration.import',
            status: 'succeeded',
            progress: 1,
            result: { export_id: 'mig-source', cleanup_pending: true },
        });
        await renderMigrationPanel([migrationInstance('importing', 'machine-current')]);

        fireEvent.change(importPasswordInput(), { target: { value: 'resume-pass-2026' } });
        const resumeButton = screen.getByRole('button', { name: 'Resume Move In' }) as HTMLButtonElement;
        await waitFor(() => expect(resumeButton.disabled).toBe(false));
        fireEvent.click(resumeButton);

        await waitFor(() => expect(StartUserDataMigrationImport).toHaveBeenCalledWith('mig-source', 'resume-pass-2026'));
        const cleanupButton = await screen.findByRole('button', { name: 'Retry Cleanup' });
        expect(importPasswordInput().disabled).toBe(true);
        fireEvent.click(cleanupButton);

        await waitFor(() => expect(StartUserDataMigrationCleanup).toHaveBeenCalledWith('mig-source'));
    });

    it('retries cleanup for an already restored package claimed by this machine', async () => {
        await renderMigrationPanel([migrationInstance('imported', 'machine-current')]);

        expect(importPasswordInput().disabled).toBe(true);
        expect(screen.getByText(/only completes Hub cleanup/i)).toBeTruthy();
        const button = screen.getByRole('button', { name: 'Retry Cleanup' }) as HTMLButtonElement;
        await waitFor(() => expect(button.disabled).toBe(false));
        fireEvent.click(button);

        await waitFor(() => expect(StartUserDataMigrationCleanup).toHaveBeenCalledWith('mig-source'));
        expect(StartUserDataMigrationImport).not.toHaveBeenCalled();
    });
});
