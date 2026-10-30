// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SystemDoctorDialog } from '../SystemDoctorDialog';

const t = (key: string) => ({
    systemDoctorTitle: 'System Doctor',
    loading: 'Loading',
    systemDoctorUnavailable: 'Could not load the doctor report.',
    systemDoctorRefresh: 'Refresh',
    systemDoctorReady: 'Ready',
    systemDoctorNotReady: 'Not ready',
}[key] ?? key);

function installBackend(overrides: Record<string, unknown>) {
    (window as unknown as { go: unknown }).go = {
        main: {
            App: {
                GetSharedAgentLoopStatus: vi.fn().mockResolvedValue(null),
                ...overrides,
            },
        },
    };
}

afterEach(() => {
    cleanup();
    delete (window as unknown as { go?: unknown }).go;
    vi.restoreAllMocks();
});

describe('SystemDoctorDialog', () => {
    it('offers a refresh path instead of an empty shell when the doctor report is null', async () => {
        const runDoctor = vi.fn().mockResolvedValue(null);
        installBackend({ RunDoctor: runDoctor });
        render(<SystemDoctorDialog open onClose={() => {}} t={t} />);

        expect(await screen.findByText('Could not load the doctor report.')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
        await waitFor(() => expect(runDoctor.mock.calls.length).toBe(2));
    });

    it('shows the doctor banner when a report arrives and skips the empty state', async () => {
        installBackend({
            RunDoctor: vi.fn().mockResolvedValue({ ok: true, summary: 'all good', blockers: 0, warnings: 0, checks: [] }),
        });
        render(<SystemDoctorDialog open onClose={() => {}} t={t} />);

        expect(await screen.findByText('Ready')).toBeTruthy();
        expect(screen.getByText('all good')).toBeTruthy();
        expect(screen.queryByText('Could not load the doctor report.')).toBeNull();
    });

    it('surfaces the backend error instead of the empty state when RunDoctor rejects', async () => {
        installBackend({ RunDoctor: vi.fn().mockRejectedValue(new Error('boom')) });
        render(<SystemDoctorDialog open onClose={() => {}} t={t} />);

        expect(await screen.findByText('boom')).toBeTruthy();
        expect(screen.queryByText('Could not load the doctor report.')).toBeNull();
    });

    it('renders nothing while closed', () => {
        installBackend({ RunDoctor: vi.fn() });
        render(<SystemDoctorDialog open={false} onClose={() => {}} t={t} />);
        expect(screen.queryByText('System Doctor')).toBeNull();
    });
});
