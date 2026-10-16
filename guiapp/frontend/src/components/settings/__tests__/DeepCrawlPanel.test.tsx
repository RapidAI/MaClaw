import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { EventsOn } from '../../../../wailsjs/runtime';
import { DeepCrawlPanel, previewTotalDiscovered } from '../DeepCrawlPanel';
import type { DeepCrawlConfig, DeepCrawlProgress, DeepCrawlRunResult } from '../DeepCrawlPanel';

let deepCrawlProgressHandler: ((data: DeepCrawlProgress) => void) | undefined;

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((event: string, cb: (data: DeepCrawlProgress) => void) => {
        if (event === 'knowledge:deep-crawl-progress') deepCrawlProgressHandler = cb;
        return vi.fn();
    }),
    EventsOff: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    KnowledgeDeepCrawlCancel: vi.fn(async () => undefined),
}));

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    deepCrawlProgressHandler = undefined;
});

describe('previewTotalDiscovered', () => {
    it('uses backend total_discovered when present', () => {
        expect(previewTotalDiscovered({
            total_discovered: 7,
            by_depth: [
                { depth: 0, total: 1, urls: ['https://example.com'] },
                { depth: 1, total: 2, urls: ['https://example.com/a', 'https://example.com/b'] },
            ],
        })).toBe(7);
    });

    it('falls back to summing depth totals for older preview payloads', () => {
        expect(previewTotalDiscovered({
            by_depth: [
                { depth: 0, total: 1, urls: ['https://example.com'] },
                { depth: 1, total: 3, urls: ['https://example.com/a'] },
            ],
        })).toBe(4);
    });

    it('falls back to URL list lengths when depth totals are missing', () => {
        expect(previewTotalDiscovered({
            by_depth: [
                { depth: 0, total: 0, urls: ['https://example.com'] },
                { depth: 1, total: 0, urls: ['https://example.com/a', 'https://example.com/b'] },
            ],
        })).toBe(3);
    });
});

describe('DeepCrawlPanel', () => {

    it('surfaces preview limit status from the backend result', async () => {
        const onPreview = vi.fn(async () => ({
            status: 'limit_reached' as const,
            total_discovered: 25,
            by_depth: [
                { depth: 0, total: 1, urls: ['https://example.com/docs'] },
                { depth: 1, total: 24, urls: ['https://example.com/docs/a'] },
            ],
        }));
        render(<DeepCrawlPanel lang="en" onPreview={onPreview} />);

        fireEvent.change(screen.getByPlaceholderText('https://example.com/docs'), {
            target: { value: 'https://example.com/docs' },
        });
        fireEvent.click(screen.getByRole('button', { name: 'Preview' }));

        await waitFor(() => expect(onPreview).toHaveBeenCalledTimes(1));
        expect(screen.getByText('Found 25 URLs across 2 levels')).toBeTruthy();
        expect(screen.getByText('Limit reached; showing the first discovered URLs')).toBeTruthy();
    });

    it('shows crawl progress immediately and keeps actions disabled while the backend is running', async () => {
        let resolveStart!: () => void;
        const onStartCrawl = vi.fn((_config: DeepCrawlConfig) => new Promise<void>(resolve => {
            resolveStart = resolve;
        }));
        render(<DeepCrawlPanel lang="en" onStartCrawl={onStartCrawl} />);

        fireEvent.change(screen.getByPlaceholderText('https://example.com/docs'), {
            target: { value: 'https://example.com/docs' },
        });
        fireEvent.click(screen.getByRole('button', { name: 'Start Crawl' }));

        await waitFor(() => expect(onStartCrawl).toHaveBeenCalledTimes(1));
        expect(screen.getByText('Crawling...')).toBeTruthy();
        expect(screen.getByText('https://example.com/docs')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Crawling...' }).hasAttribute('disabled')).toBe(true);

        resolveStart();
    });

    it('uses the returned crawl result when no final progress event arrives', async () => {
        const onStartCrawl = vi.fn(async (_config: DeepCrawlConfig): Promise<DeepCrawlRunResult> => ({
            job_id: 'returned-job',
            status: 'completed',
            total_discovered: 2,
            total_saved: 1,
            failed: 1,
            skipped: 0,
        }));
        render(<DeepCrawlPanel lang="en" onStartCrawl={onStartCrawl} />);

        fireEvent.change(screen.getByPlaceholderText('https://example.com/docs'), {
            target: { value: 'https://example.com/docs' },
        });
        fireEvent.click(screen.getByRole('button', { name: 'Start Crawl' }));

        await waitFor(() => expect(screen.getByRole('button', { name: 'Start Crawl' }).hasAttribute('disabled')).toBe(false));
        expect(screen.getAllByText((_content, element) => element?.textContent?.includes('Status: completed') ?? false).length).toBeGreaterThan(0);
        expect(screen.getByText('100%')).toBeTruthy();
    });

    it('ignores stale preview completion events while a crawl is running', async () => {
        let resolveStart!: () => void;
        const onStartCrawl = vi.fn((_config: DeepCrawlConfig) => new Promise<void>(resolve => {
            resolveStart = resolve;
        }));
        render(<DeepCrawlPanel lang="en" onStartCrawl={onStartCrawl} />);

        expect(EventsOn).toHaveBeenCalledWith('knowledge:deep-crawl-progress', expect.any(Function));

        fireEvent.change(screen.getByPlaceholderText('https://example.com/docs'), {
            target: { value: 'https://example.com/docs' },
        });
        fireEvent.click(screen.getByRole('button', { name: 'Start Crawl' }));

        await waitFor(() => expect(onStartCrawl).toHaveBeenCalledTimes(1));
        const activeClientRunID = onStartCrawl.mock.calls[0]?.[0]?.clientRunID;
        expect(activeClientRunID).toMatch(/^deep-crawl-/);

        act(() => {
            deepCrawlProgressHandler?.({
                mode: 'preview',
                job_id: '',
                status: 'completed',
                current_depth: 2,
                max_depth: 2,
                total_discovered: 1,
                completed: 0,
                pending: 0,
                failed: 0,
                skipped: 0,
            });
        });

        expect(screen.getByRole('button', { name: 'Crawling...' }).hasAttribute('disabled')).toBe(true);

        act(() => {
            deepCrawlProgressHandler?.({
                mode: 'crawl',
                client_run_id: 'older-run',
                job_id: 'old-job',
                status: 'completed',
                current_depth: 2,
                max_depth: 2,
                total_discovered: 1,
                completed: 1,
                pending: 0,
                failed: 0,
                skipped: 0,
            });
        });

        expect(screen.getByRole('button', { name: 'Crawling...' }).hasAttribute('disabled')).toBe(true);

        act(() => {
            deepCrawlProgressHandler?.({
                mode: 'crawl',
                job_id: 'legacy-job',
                status: 'completed',
                current_depth: 2,
                max_depth: 2,
                total_discovered: 1,
                completed: 1,
                pending: 0,
                failed: 0,
                skipped: 0,
            });
        });

        expect(screen.getByRole('button', { name: 'Crawling...' }).hasAttribute('disabled')).toBe(true);

        act(() => {
            deepCrawlProgressHandler?.({
                mode: 'crawl',
                client_run_id: activeClientRunID,
                job_id: 'job-1',
                status: 'completed',
                current_depth: 2,
                max_depth: 2,
                total_discovered: 1,
                completed: 1,
                pending: 0,
                failed: 0,
                skipped: 0,
            });
        });

        await waitFor(() => expect(screen.getByRole('button', { name: 'Start Crawl' }).hasAttribute('disabled')).toBe(false));
        expect(screen.getAllByText((_content, element) => element?.textContent?.includes('Status: completed') ?? false).length).toBeGreaterThan(0);
        expect(screen.getByText('100%')).toBeTruthy();
        expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();
        resolveStart();
    });
});
