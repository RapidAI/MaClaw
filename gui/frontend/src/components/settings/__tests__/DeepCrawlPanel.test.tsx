import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup } from '@testing-library/react';
import { DeepCrawlPanel, previewTotalDiscovered } from '../DeepCrawlPanel';

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    KnowledgeDeepCrawlCancel: vi.fn(async () => undefined),
}));

afterEach(() => {
    cleanup();
    vi.clearAllMocks();
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
    it('re-enables crawl actions when the start call completes without a final progress event', async () => {
        const onStartCrawl = vi.fn(async () => undefined);
        render(<DeepCrawlPanel lang="en" onStartCrawl={onStartCrawl} />);

        fireEvent.change(screen.getByPlaceholderText('https://example.com/docs'), {
            target: { value: 'https://example.com/docs' },
        });
        fireEvent.click(screen.getByRole('button', { name: 'Start Crawl' }));

        await waitFor(() => expect(onStartCrawl).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(screen.getByRole('button', { name: 'Start Crawl' }).hasAttribute('disabled')).toBe(false));
    });
});
