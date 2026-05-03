// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';

const GetWebSearchProvidersMock = vi.fn();
const SaveWebSearchProvidersMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetWebSearchProviders: (...args: unknown[]) => GetWebSearchProvidersMock(...args),
    SaveWebSearchProviders: (...args: unknown[]) => SaveWebSearchProvidersMock(...args),
}));

import { WebSearchConfigPanel } from '../WebSearchConfigPanel';

describe('WebSearchConfigPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        GetWebSearchProvidersMock.mockResolvedValue({
            providers: [
                { name: 'Brave', type: 'brave', key: 'brave-key', base_url: 'https://api.search.brave.com/res/v1/web/search' },
                { name: 'Serper', type: 'serper', key: '', base_url: 'https://google.serper.dev/search' },
                { name: 'DuckDuckGo', type: 'duckduckgo' },
            ],
            current: 'brave',
        });
        SaveWebSearchProvidersMock.mockResolvedValue(undefined);
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('shows saved state briefly after saving and then resets', async () => {
        render(<WebSearchConfigPanel lang="zh-Hans" />);

        await waitFor(() => {
            expect(screen.getByDisplayValue('brave-key')).toBeTruthy();
        });

        expect(screen.getByText(/选择 AI 助手网页搜索使用的搜索引擎/)).toBeTruthy();
        expect(screen.queryByText(/Choose which search engine/)).toBeNull();

        fireEvent.click(screen.getByRole('button', { name: '保存' }));

        await waitFor(() => {
            expect(SaveWebSearchProvidersMock).toHaveBeenCalledWith(
                expect.arrayContaining([
                    expect.objectContaining({ type: 'brave', key: 'brave-key' }),
                    expect.objectContaining({ type: 'serper' }),
                    expect.objectContaining({ type: 'duckduckgo' }),
                ]),
                'brave',
            );
        });

        await waitFor(() => {
            expect(screen.getByRole('button', { name: '已保存 ✓' })).toBeTruthy();
        });

        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 1600));
        });

        await waitFor(() => {
            expect(screen.getByRole('button', { name: '保存' })).toBeTruthy();
        });
    }, 10000);
});
