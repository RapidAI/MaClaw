import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const listeners = vi.hoisted(() => new Map<string, (value?: unknown) => void>());

vi.mock('../../../../wailsjs/go/main/App', () => ({
    ListMemories: vi.fn(),
    SaveMemory: vi.fn(),
    UpdateMemory: vi.fn(),
    DeleteMemory: vi.fn(),
    DeleteMemories: vi.fn(),
    ListBotMemories: vi.fn(),
    SaveBotMemory: vi.fn(),
    UpdateBotMemory: vi.fn(),
    DeleteBotMemory: vi.fn(),
    DeleteBotMemories: vi.fn(),
    ListDesktopBots: vi.fn(),
    CompressMemories: vi.fn(),
    ListMemoryBackups: vi.fn(),
    RestoreMemoryBackup: vi.fn(),
    DeleteMemoryBackup: vi.fn(),
    SetAutoCompress: vi.fn(),
    GetAutoCompressStatus: vi.fn(),
    IsMemoryCompressing: vi.fn(),
    GetMemoryMaxBackups: vi.fn(),
    SetMemoryMaxBackups: vi.fn(),
    GetMemoryStatus: vi.fn(),
    GetExperienceLearningSnapshot: vi.fn(),
    GetExperienceGovernanceSummary: vi.fn(),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((name: string, handler: (value?: unknown) => void) => { listeners.set(name, handler); }),
    EventsOff: vi.fn((name: string) => { listeners.delete(name); }),
}));

import {
    GetExperienceGovernanceSummary,
    GetExperienceLearningSnapshot,
    GetMemoryStatus,
    IsMemoryCompressing,
    ListDesktopBots,
    ListMemoryBackups,
    ListMemories,
    RestoreMemoryBackup,
} from '../../../../wailsjs/go/main/App';
import { MemoryManagementPanel } from '../MemoryManagementPanel';

const status = {
    total_entries: 1,
    max_capacity: 2000,
    capacity_percent: 0.05,
    recallable_entries: 1,
    archived_entries: 0,
    stale_entries: 0,
    pinned_entries: 0,
    embedder_active: false,
    categories: [{ category: 'user_fact', count: 1, percent: 100 }],
};

describe('MemoryManagementPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        listeners.clear();
        vi.mocked(ListDesktopBots).mockResolvedValue([] as any);
        vi.mocked(ListMemories).mockResolvedValue([] as any);
        vi.mocked(GetExperienceLearningSnapshot).mockResolvedValue({} as any);
        vi.mocked(GetExperienceGovernanceSummary).mockResolvedValue({} as any);
    });

    it('refreshes the edit list when memory changes', async () => {
        render(<MemoryManagementPanel lang="en" />);
        await waitFor(() => expect(vi.mocked(ListMemories)).toHaveBeenCalledTimes(1));
        const handler = listeners.get('memory:changed');
        expect(handler).toBeTruthy();
        act(() => handler?.({ kind: 'create' }));
        await waitFor(() => expect(vi.mocked(ListMemories)).toHaveBeenCalledTimes(2));
    });

    it('shows status failures and retries instead of loading forever', async () => {
        vi.mocked(GetMemoryStatus).mockRejectedValueOnce(new Error('status failed'));
        render(<MemoryManagementPanel lang="en" />);
        fireEvent.click(screen.getByRole('tab', { name: /Memory Status/ }));
        expect((await screen.findByRole('alert')).textContent).toContain('status failed');
        vi.mocked(GetMemoryStatus).mockResolvedValueOnce(status as any);
        fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
        expect(await screen.findByText('Capacity')).toBeTruthy();
    });
});

describe('Memory Time Machine actions', () => {
    it('submits only one restore while an action is pending', async () => {
        vi.clearAllMocks();
        listeners.clear();
        vi.mocked(ListDesktopBots).mockResolvedValue([] as any);
        vi.mocked(ListMemories).mockResolvedValue([] as any);
        vi.mocked(IsMemoryCompressing).mockResolvedValue(false);
        vi.mocked(ListMemoryBackups).mockResolvedValue([{ name: 'backup-one', created_at: '2026-01-01T00:00:00Z', size_bytes: 100, entry_count: 1 }] as any);
        let finishRestore!: () => void;
        vi.mocked(RestoreMemoryBackup).mockImplementation(() => new Promise<void>((resolve) => { finishRestore = resolve; }));
        render(<MemoryManagementPanel lang="en" />);
        fireEvent.click(screen.getByRole('tab', { name: /Time Machine/ }));
        fireEvent.click(await screen.findByRole('button', { name: 'Restore backup-one' }));
        const confirm = screen.getByRole('button', { name: 'Confirm Restore' });
        fireEvent.click(confirm);
        fireEvent.click(confirm);
        expect(vi.mocked(RestoreMemoryBackup)).toHaveBeenCalledTimes(1);
        await act(async () => { finishRestore(); });
        await waitFor(() => expect(screen.queryByRole('button', { name: 'Restoring...' })).toBeNull());
    });
});
