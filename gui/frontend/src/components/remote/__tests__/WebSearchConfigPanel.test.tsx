// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';

const GetWebSearchProvidersMock = vi.fn();
const SaveWebSearchProvidersMock = vi.fn();
const TestWebSearchProviderMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetWebSearchProviders: (...args: unknown[]) => GetWebSearchProvidersMock(...args),
    SaveWebSearchProviders: (...args: unknown[]) => SaveWebSearchProvidersMock(...args),
    TestWebSearchProvider: (...args: unknown[]) => TestWebSearchProviderMock(...args),
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
        TestWebSearchProviderMock.mockResolvedValue(undefined);
        SaveWebSearchProvidersMock.mockResolvedValue(undefined);
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it('tests the selected provider before saving and then resets saved state', async () => {
        vi.useFakeTimers();
        render(<WebSearchConfigPanel lang="en" />);

        await waitFor(() => {
            expect(screen.getByDisplayValue('brave-key')).toBeTruthy();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(TestWebSearchProviderMock).toHaveBeenCalledWith(
                expect.objectContaining({ type: 'brave', key: 'brave-key' }),
            );
        });
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
            expect(screen.getByText('Test passed and configuration saved.')).toBeTruthy();
            expect(screen.getByRole('button', { name: 'Saved OK' })).toBeTruthy();
        });

        await act(async () => {
            vi.advanceTimersByTime(1600);
        });

        await waitFor(() => {
            expect(screen.getByRole('button', { name: 'Save' })).toBeTruthy();
        });
    }, 10000);

    it('does not save when the provider test fails', async () => {
        TestWebSearchProviderMock.mockRejectedValue(new Error('Brave returned HTTP 401'));

        render(<WebSearchConfigPanel lang="en" />);

        await waitFor(() => {
            expect(screen.getByDisplayValue('brave-key')).toBeTruthy();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(screen.getByText(/Search provider test failed:/)).toBeTruthy();
        });
        expect(SaveWebSearchProvidersMock).not.toHaveBeenCalled();
    });

    it('shows a helpful DuckDuckGo challenge error', async () => {
        GetWebSearchProvidersMock.mockResolvedValue({
            providers: [
                { name: 'DuckDuckGo', type: 'duckduckgo' },
            ],
            current: 'duckduckgo',
        });
        TestWebSearchProviderMock.mockRejectedValue(new Error('DuckDuckGo blocked this automated request with a human verification challenge (HTTP 202)'));

        render(<WebSearchConfigPanel lang="en" />);

        await waitFor(() => {
            expect(screen.getByRole('button', { name: /Save/i })).toBeTruthy();
        });

        fireEvent.click(screen.getByRole('button', { name: 'Save' }));

        await waitFor(() => {
            expect(screen.getByText(/DuckDuckGo blocked this request with a human verification challenge/)).toBeTruthy();
        });
        expect(SaveWebSearchProvidersMock).not.toHaveBeenCalled();
    });
});
