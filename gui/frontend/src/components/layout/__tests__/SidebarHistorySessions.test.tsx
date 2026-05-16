// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { SidebarHistorySessions, type HistoryDiscussionSummary } from '../SidebarHistorySessions';
import { GroupDiscussionListLocalHidden, GroupDiscussionListMine, GroupDiscussionSetLocalHidden } from '../../../../wailsjs/go/main/App';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GroupDiscussionListMine: vi.fn(),
    GroupDiscussionListLocalHidden: vi.fn(),
    GroupDiscussionSetLocalHidden: vi.fn(),
}));

const visibleSessions: HistoryDiscussionSummary[] = [
    { id: 'disc-1', topic: 'Contract review', local_relation: 'initiated_by_me', readonly: false, status: 'open', participant_ids: ['me', 've-a'] },
    { id: 'disc-2', topic: 'Vendor audit', local_relation: 'owned_ve_invited', readonly: true, status: 'open', participant_ids: ['other', 've-mine'] },
    { id: 'disc-3', topic: 'Archived research', local_relation: 'initiated_by_me', readonly: false, status: 'closed', participant_ids: ['me'] },
    { id: 'disc-4', topic: 'Role-only started', role: 'initiator', readonly: false, status: 'open', participant_ids: ['me'] },
    { id: 'disc-5', topic: 'Role-only invited', role: 'review', readonly: false, status: 'open', participant_ids: ['ve-mine'] },
];

const hiddenSessions: HistoryDiscussionSummary[] = [
    { id: 'disc-hidden', topic: 'Hidden audit', local_relation: 'owned_ve_invited', readonly: true },
];

const listMine = vi.mocked(GroupDiscussionListMine);
const listHidden = vi.mocked(GroupDiscussionListLocalHidden);
const setHidden = vi.mocked(GroupDiscussionSetLocalHidden);

beforeEach(() => {
    vi.clearAllMocks();
    listMine.mockResolvedValue(visibleSessions as any);
    listHidden.mockResolvedValue(hiddenSessions as any);
    setHidden.mockResolvedValue(undefined as any);
});

describe('SidebarHistorySessions', () => {
    it('marks initiated and invited sessions distinctly and opens by double-click', async () => {
        const onOpen = vi.fn();
        render(<SidebarHistorySessions lang="en" onOpenDiscussion={onOpen} />);

        expect(await screen.findByText('Contract review')).toBeTruthy();
        expect(screen.getByText('Vendor audit')).toBeTruthy();
        expect(screen.getByText('Archived research')).toBeTruthy();
        expect(screen.getAllByText('\u2197')).toHaveLength(3);
        expect(screen.getAllByText('Started by me')).toHaveLength(3);
        expect(screen.getAllByText('\u2199')).toHaveLength(2);
        expect(screen.getAllByText('My digital employee invited')).toHaveLength(2);
        expect(screen.getAllByText('Read-only')).toHaveLength(3);

        fireEvent.doubleClick(screen.getByText('Vendor audit'));
        expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ id: 'disc-2', local_relation: 'owned_ve_invited' }));
        expect(listMine).toHaveBeenCalledWith('all');
    });

    it('opens from context menu and can hide a visible session locally', async () => {
        const onOpen = vi.fn();
        render(<SidebarHistorySessions lang="en" onOpenDiscussion={onOpen} />);

        const item = await screen.findByText('Contract review');
        fireEvent.contextMenu(item, { clientX: 12, clientY: 18 });

        fireEvent.click(screen.getByRole('menuitem', { name: 'Open session' }));
        expect(onOpen).toHaveBeenCalledWith(expect.objectContaining({ id: 'disc-1' }));

        fireEvent.contextMenu(item, { clientX: 12, clientY: 18 });
        fireEvent.click(screen.getByRole('menuitem', { name: 'Hide locally' }));
        await waitFor(() => expect(setHidden).toHaveBeenCalledWith('disc-1', true));
    });

    it('loads hidden sessions and restores them from the context menu', async () => {
        render(<SidebarHistorySessions lang="en" />);

        fireEvent.click(await screen.findByText('Hidden'));
        expect(await screen.findByText('Hidden audit')).toBeTruthy();
        expect(listHidden).toHaveBeenCalled();

        fireEvent.contextMenu(screen.getByText('Hidden audit'), { clientX: 22, clientY: 30 });
        fireEvent.click(screen.getByRole('menuitem', { name: 'Restore' }));
        await waitFor(() => expect(setHidden).toHaveBeenCalledWith('disc-hidden', false));
    });

    it('keeps the newest list when visible and hidden loads finish out of order', async () => {
        let resolveVisible: (value: any) => void = () => {};
        let resolveHidden: (value: any) => void = () => {};
        listMine.mockImplementationOnce(() => new Promise((resolve) => { resolveVisible = resolve; }));
        listHidden.mockImplementationOnce(() => new Promise((resolve) => { resolveHidden = resolve; }));

        render(<SidebarHistorySessions lang="en" />);
        fireEvent.click(screen.getByText('Hidden'));

        await act(async () => {
            resolveHidden(hiddenSessions as any);
        });
        expect(await screen.findByText('Hidden audit')).toBeTruthy();

        await act(async () => {
            resolveVisible(visibleSessions as any);
        });
        await new Promise((resolve) => setTimeout(resolve, 0));

        expect(screen.getByText('Hidden audit')).toBeTruthy();
        expect(screen.queryByText('Contract review')).toBeNull();
    });

    it('renders nothing when group discussion history is disabled', () => {
        render(<SidebarHistorySessions lang="en" enabled={false} />);

        expect(screen.queryByText('History sessions')).toBeNull();
        expect(listMine).not.toHaveBeenCalled();
    });
});
