import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    DeleteSession: vi.fn(),
    GetSessionCount: vi.fn(),
    GetSessionFullText: vi.fn(),
    ListSessionHistory: vi.fn(),
    SearchSessionHistory: vi.fn(),
}));

import {
    DeleteSession,
    GetSessionCount,
    GetSessionFullText,
    ListSessionHistory,
    SearchSessionHistory,
} from '../../../../wailsjs/go/main/App';
import { SessionHistoryTab } from '../MemorySessionHistoryTab';

type Deferred<T> = { promise: Promise<T>; resolve: (value: T) => void };

function deferred<T>(): Deferred<T> {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((done) => { resolve = done; });
    return { promise, resolve };
}

const t = (en: string) => en;
const session = (id: string) => ({ session_id: id, timestamp: '2026-01-01T00:00:00Z', platform: 'gui', topic: id, text_len: 20 });

describe('SessionHistoryTab', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(ListSessionHistory).mockResolvedValue([] as any);
        vi.mocked(GetSessionCount).mockResolvedValue(0);
        vi.mocked(GetSessionFullText).mockResolvedValue('');
    });

    it('keeps the newest search result when responses arrive out of order', async () => {
        const first = deferred<any>();
        const second = deferred<any>();
        vi.mocked(SearchSessionHistory)
            .mockReturnValueOnce(first.promise)
            .mockReturnValueOnce(second.promise);
        render(<SessionHistoryTab t={t} lang="en" />);
        await waitFor(() => expect(vi.mocked(ListSessionHistory)).toHaveBeenCalled());
        const input = screen.getByRole('textbox', { name: 'Search sessions' });
        fireEvent.change(input, { target: { value: 'first' } });
        fireEvent.keyDown(input, { key: 'Enter' });
        fireEvent.change(input, { target: { value: 'second' } });
        fireEvent.keyDown(input, { key: 'Enter' });
        await act(async () => {
            second.resolve([{ ...session('newest'), snippet: '<b>newest</b>' }]);
            await second.promise;
        });
        expect((await screen.findAllByText('newest')).length).toBeGreaterThan(0);
        await act(async () => {
            first.resolve([{ ...session('stale'), snippet: '<b>stale</b>' }]);
            await first.promise;
        });
        expect(screen.queryByText('stale')).toBeNull();
        expect(screen.getAllByText('newest').length).toBeGreaterThan(0);
    });

    it('ignores a previous session full-text response after switching viewers', async () => {
        const first = deferred<string>();
        const second = deferred<string>();
        vi.mocked(ListSessionHistory).mockResolvedValue([session('a'), session('b')] as any);
        vi.mocked(GetSessionCount).mockResolvedValue(2);
        vi.mocked(GetSessionFullText).mockImplementation((id: string) => id === 'a' ? first.promise : second.promise);
        render(<SessionHistoryTab t={t} lang="en" />);
        expect(await screen.findByText('a')).toBeTruthy();
        fireEvent.click(screen.getAllByRole('button', { name: 'VIEW' })[0]);
        fireEvent.click(screen.getByRole('button', { name: 'X' }));
        fireEvent.click(screen.getAllByRole('button', { name: 'VIEW' })[1]);
        await act(async () => {
            second.resolve('newest full text');
            await second.promise;
        });
        expect(await screen.findByText('newest full text')).toBeTruthy();
        await act(async () => {
            first.resolve('stale full text');
            await first.promise;
        });
        expect(screen.queryByText('stale full text')).toBeNull();
        expect(screen.getByText('newest full text')).toBeTruthy();
    });

    it('loads another hundred sessions while older records remain', async () => {
        const firstPage = Array.from({ length: 100 }, (_, index) => session(`session-${index}`));
        vi.mocked(ListSessionHistory).mockResolvedValue(firstPage as any);
        vi.mocked(GetSessionCount).mockResolvedValue(150);
        render(<SessionHistoryTab t={t} lang="en" />);
        const button = await screen.findByRole('button', { name: 'Load more' });
        fireEvent.click(button);
        await waitFor(() => expect(vi.mocked(ListSessionHistory)).toHaveBeenLastCalledWith(200));
    });
});

describe('SessionHistoryTab deletion reconciliation', () => {
    it('does not restore a deleted row from an older list response', async () => {
        vi.clearAllMocks();
        const stalePage = deferred<any>();
        vi.mocked(ListSessionHistory)
            .mockResolvedValueOnce([session('a')] as any)
            .mockReturnValueOnce(stalePage.promise);
        vi.mocked(GetSessionCount).mockResolvedValue(2);
        vi.mocked(DeleteSession).mockResolvedValue(undefined);
        render(<SessionHistoryTab t={t} lang="en" />);
        expect(await screen.findByText('a')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
        fireEvent.click(screen.getByRole('button', { name: 'DEL' }));
        fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
        await waitFor(() => expect(screen.queryByText('a')).toBeNull());
        await act(async () => {
            stalePage.resolve([session('a'), session('b')]);
            await stalePage.promise;
        });
        expect(screen.queryByText('a')).toBeNull();
    });
});
