import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor, fireEvent } from '@testing-library/react';

const getMaclawLLMProvidersMock = vi.fn();
const getAllLLMTokenUsageMock = vi.fn();
const getLLMTokenUsageByDayMock = vi.fn();
const resetLLMTokenUsageMock = vi.fn();
const runtimeHandlers = new Map<string, (payload?: unknown) => void>();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetMaclawLLMProviders: (...args: unknown[]) => getMaclawLLMProvidersMock(...args),
    GetAllLLMTokenUsage: (...args: unknown[]) => getAllLLMTokenUsageMock(...args),
    GetLLMTokenUsageByDay: (...args: unknown[]) => getLLMTokenUsageByDayMock(...args),
    ResetLLMTokenUsage: (...args: unknown[]) => resetLLMTokenUsageMock(...args),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((event: string, handler: (payload?: unknown) => void) => {
        runtimeHandlers.set(event, handler);
        return () => {
            if (runtimeHandlers.get(event) === handler) runtimeHandlers.delete(event);
        };
    }),
    EventsOff: vi.fn((event: string) => {
        runtimeHandlers.delete(event);
    }),
}));

import { TokenUsagePanel } from '../TokenUsagePanel';

describe('TokenUsagePanel', () => {
    beforeEach(() => {
        runtimeHandlers.clear();
        getMaclawLLMProvidersMock.mockReset();
        getAllLLMTokenUsageMock.mockReset();
        getLLMTokenUsageByDayMock.mockReset();
        resetLLMTokenUsageMock.mockReset();
        getLLMTokenUsageByDayMock.mockResolvedValue([]);
        getMaclawLLMProvidersMock.mockResolvedValue({
            Providers: [{ Name: '智谱' }],
            Current: '智谱',
        });
        resetLLMTokenUsageMock.mockResolvedValue(undefined);
    });

    it('renders token stats when provider state uses PascalCase fields', async () => {
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': {
                InputTokens: 100,
                OutputTokens: 20,
                TotalTokens: 120,
            },
        });

        const { getByText } = render(<TokenUsagePanel lang="en" />);

        await waitFor(() => {
            expect(getByText('100')).toBeTruthy();
            expect(getByText('20')).toBeTruthy();
            expect(getByText('120')).toBeTruthy();
        });
    });

    it('falls back to summing input and output when total is missing', async () => {
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': {
                InputTokens: 100,
                OutputTokens: 25,
            },
        });

        const { getByText } = render(<TokenUsagePanel lang="en" />);

        await waitFor(() => {
            expect(getByText('100')).toBeTruthy();
            expect(getByText('25')).toBeTruthy();
            expect(getByText('125')).toBeTruthy();
        });
    });

    it('renders prompt cache read, write, and request hit-rate stats', async () => {
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': {
                InputTokens: 1000,
                OutputTokens: 200,
                TotalTokens: 1200,
                CachedInputTokens: 600,
                CacheWriteTokens: 128,
                Requests: 5,
                CachedRequests: 3,
            },
        });

        const { getByText } = render(<TokenUsagePanel lang="en" />);

        await waitFor(() => {
            expect(getByText('Cache Read')).toBeTruthy();
            expect(getByText('Cache Write')).toBeTruthy();
            expect(getByText('Cache Hit Rate')).toBeTruthy();
            expect(getByText('600')).toBeTruthy();
            expect(getByText('128')).toBeTruthy();
            expect(getByText('60%')).toBeTruthy();
        });
    });

    it('adds lifetime usage stored under an older alias name', async () => {
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': { InputTokens: 100, OutputTokens: 20, TotalTokens: 120 },
            'GLM (智谱)': { InputTokens: 10, OutputTokens: 0, TotalTokens: 10 },
        });

        const { getByText } = render(<TokenUsagePanel lang="en" />);

        await waitFor(() => {
            expect(getByText('110')).toBeTruthy();
            expect(getByText('20')).toBeTruthy();
            expect(getByText('130')).toBeTruthy();
        });
    });

    it('resets the selected provider together with its older names', async () => {
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': { InputTokens: 100, OutputTokens: 20, TotalTokens: 120 },
        });

        const { getByRole } = render(<TokenUsagePanel lang="zh-Hans" />);
        await waitFor(() => {
            expect(getByRole('button', { name: '重置当前' })).toBeTruthy();
        });
        fireEvent.click(getByRole('button', { name: '重置当前' }));

        await waitFor(() => {
            expect(resetLLMTokenUsageMock).toHaveBeenCalledWith('智谱');
            expect(resetLLMTokenUsageMock).toHaveBeenCalledWith('智谱龙芯');
            expect(resetLLMTokenUsageMock).toHaveBeenCalledWith('GLM(智谱)');
            expect(resetLLMTokenUsageMock).toHaveBeenCalledWith('GLM (智谱)');
        });
        expect(resetLLMTokenUsageMock).toHaveBeenCalledTimes(4);
    });

    it('matches token stats when provider and usage keys use different Zhipu aliases', async () => {
        getAllLLMTokenUsageMock.mockResolvedValue({
            'GLM (智谱)': {
                InputTokens: 100,
                OutputTokens: 20,
                TotalTokens: 120,
            },
        });

        const { getByText } = render(<TokenUsagePanel lang="en" />);

        await waitFor(() => {
            expect(getByText('100')).toBeTruthy();
            expect(getByText('20')).toBeTruthy();
            expect(getByText('120')).toBeTruthy();
        });
    });

    it('prefers provider with usage when current provider has no accumulated tokens', async () => {
        getMaclawLLMProvidersMock.mockResolvedValue({
            Providers: [{ Name: 'MiniMax' }, { Name: 'GLM (智谱)' }],
            Current: 'MiniMax',
        });
        getAllLLMTokenUsageMock.mockResolvedValue({
            'GLM (智谱)': { InputTokens: 100, OutputTokens: 20, TotalTokens: 120 },
        });

        const { container, getByText } = render(<TokenUsagePanel lang="en" />);

        await waitFor(() => {
            expect((container.querySelector('select') as HTMLSelectElement).value).toBe('GLM (智谱)');
            expect(getByText('100')).toBeTruthy();
            expect(getByText('20')).toBeTruthy();
            expect(getByText('120')).toBeTruthy();
        });
    });

    it('prefers provider returned by LLM config when usage exists but remote provider list is unavailable', async () => {
        getMaclawLLMProvidersMock.mockResolvedValue({
            Providers: [{ Name: 'GLM (智谱)' }, { Name: 'MiniMax' }],
            Current: 'GLM (智谱)',
        });
        getAllLLMTokenUsageMock.mockResolvedValue({
            'GLM (智谱)': { InputTokens: 180, OutputTokens: 40, TotalTokens: 220 },
        });

        const { container, getByText } = render(<TokenUsagePanel lang="en" />);

        await waitFor(() => {
            expect((container.querySelector('select') as HTMLSelectElement).value).toBe('GLM (智谱)');
            expect(getByText('180')).toBeTruthy();
            expect(getByText('40')).toBeTruthy();
            expect(getByText('220')).toBeTruthy();
        });
    });

    it('does not surface remote tool usage-only keys as Maclaw providers', async () => {
        getMaclawLLMProvidersMock.mockResolvedValue({
            Providers: [{ Name: 'GLM (智谱)' }],
            Current: 'GLM (智谱)',
        });
        getAllLLMTokenUsageMock.mockResolvedValue({
            'codex:gpt-5.4': { InputTokens: 5000, OutputTokens: 300, TotalTokens: 5300 },
            'GLM (智谱)': { InputTokens: 180, OutputTokens: 40, TotalTokens: 220 },
        });

        const { container, getByText, queryByText } = render(<TokenUsagePanel lang="en" />);

        await waitFor(() => {
            expect((container.querySelector('select') as HTMLSelectElement).value).toBe('GLM (智谱)');
            expect(queryByText('codex:gpt-5.4')).toBeNull();
            expect(getByText('220')).toBeTruthy();
        });
    });

    it('reloads stats when token usage changed event fires', async () => {
        getAllLLMTokenUsageMock
            .mockResolvedValueOnce({
                'GLM(智谱)': { InputTokens: 100, OutputTokens: 20, TotalTokens: 120 },
            })
            .mockResolvedValueOnce({
                'GLM(智谱)': { InputTokens: 180, OutputTokens: 40, TotalTokens: 220 },
            });

        const { getByText } = render(<TokenUsagePanel lang="en" />);

        await waitFor(() => {
            expect(getByText('120')).toBeTruthy();
        });

        runtimeHandlers.get('llm-token-usage-changed')?.('GLM(智谱)');
        runtimeHandlers.get('llm-token-usage-changed')?.('GLM(智谱)');
        expect(getAllLLMTokenUsageMock).toHaveBeenCalledTimes(1);

        await waitFor(() => {
            expect(getByText('180')).toBeTruthy();
            expect(getByText('40')).toBeTruthy();
            expect(getByText('220')).toBeTruthy();
        }, { timeout: 4000 });
        expect(getAllLLMTokenUsageMock).toHaveBeenCalledTimes(2);
    });

    it('switches the usage trend between day, week, and month', async () => {
        const today = new Date();
        const iso = (offset: number) => {
            const date = new Date(today.getFullYear(), today.getMonth(), today.getDate() + offset);
            const month = String(date.getMonth() + 1).padStart(2, '0');
            const day = String(date.getDate()).padStart(2, '0');
            return `${date.getFullYear()}-${month}-${day}`;
        };
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': { InputTokens: 100, OutputTokens: 20, TotalTokens: 120 },
        });
        getLLMTokenUsageByDayMock.mockResolvedValue([
            { date: iso(0), provider: '智谱', input_tokens: 100, output_tokens: 20, total_tokens: 120 },
            { date: iso(-1), provider: '智谱', input_tokens: 10, output_tokens: 0, total_tokens: 10 },
        ]);

        const { getByRole, getByTestId } = render(<TokenUsagePanel lang="zh-Hans" />);

        await waitFor(() => {
            expect(getByTestId('token-usage-trend').getAttribute('data-total')).toBe('130');
        });
        expect(getByTestId('token-usage-trend').getAttribute('data-grain')).toBe('day');
        expect(getByTestId('token-usage-trend').textContent).toContain('近30日');
        expect(getByTestId('token-usage-trend').querySelector('.token-usage-trend__line')).toBeTruthy();
        expect(getByTestId('token-usage-trend').querySelector('.token-usage-trend__bar')).toBeNull();

        fireEvent.click(getByRole('button', { name: '周' }));
        expect(getByTestId('token-usage-trend').getAttribute('data-grain')).toBe('week');
        expect(getByTestId('token-usage-trend').textContent).toContain('近8周');
        expect(Number(getByTestId('token-usage-trend').getAttribute('data-total'))).toBeGreaterThanOrEqual(130);

        fireEvent.click(getByRole('button', { name: '月' }));
        expect(getByTestId('token-usage-trend').getAttribute('data-grain')).toBe('month');
        expect(getByTestId('token-usage-trend').textContent).toMatch(/近\d+个月/);
        expect(getByTestId('token-usage-trend').getAttribute('data-total')).toBe('130');
    });

    it('marks days without a stored bucket as no record instead of zero usage', async () => {
        const today = new Date();
        const iso = (offset: number) => {
            const date = new Date(today.getFullYear(), today.getMonth(), today.getDate() + offset);
            const month = String(date.getMonth() + 1).padStart(2, '0');
            const day = String(date.getDate()).padStart(2, '0');
            return `${date.getFullYear()}-${month}-${day}`;
        };
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': { InputTokens: 100, OutputTokens: 20, TotalTokens: 120 },
        });
        // Exactly one recorded day: the case that used to render as a flat zero line.
        getLLMTokenUsageByDayMock.mockResolvedValue([
            { date: iso(0), provider: '智谱', input_tokens: 100, output_tokens: 20, total_tokens: 120 },
        ]);

        const { getByTestId } = render(<TokenUsagePanel lang="zh-Hans" />);

        const trend = await waitFor(() => {
            const node = getByTestId('token-usage-trend');
            expect(node.getAttribute('data-total')).toBe('120');
            return node;
        });

        // Days absent from the day store must be flagged, not silently drawn as 0.
        const keys = Array.from(trend.querySelectorAll('[data-trend-key]'));
        const unrecorded = keys.filter((node) => node.getAttribute('data-recorded') === 'false');
        expect(unrecorded.length).toBe(29);
        expect(unrecorded.every((node) => (node.querySelector('title')?.textContent || '').includes('无记录'))).toBe(true);
        expect(keys.filter((node) => node.getAttribute('data-recorded') === 'true')).toHaveLength(1);

        // The empty stretch gets its own dashed baseline rather than a solid curve.
        expect(trend.querySelector('.token-usage-trend__gap')).toBeTruthy();
        // The note names the first recorded day instead of the vague "this update".
        expect(trend.querySelector('.token-usage-trend__note')?.textContent).toContain(`按日记录自 ${iso(0)} 起`);
    });

    it('hides the cache write row when no provider reports write tokens', async () => {
        getAllLLMTokenUsageMock.mockResolvedValue({
            // Real shape: cache reads present, cache writes absent/zero.
            '智谱': { InputTokens: 100, OutputTokens: 20, CachedInputTokens: 300, Requests: 4, CachedRequests: 1 },
        });

        const { findByText, queryByText } = render(<TokenUsagePanel lang="zh-Hans" />);

        await findByText('300');
        expect(queryByText('缓存读取')).toBeTruthy();
        // A permanent zero row reads as a real measurement; it must not render.
        expect(queryByText('缓存写入')).toBeNull();
        expect(findByText('25%')).toBeTruthy();
    });

    it('shows the cache write row once write tokens are reported', async () => {
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': { InputTokens: 100, OutputTokens: 20, CachedInputTokens: 300, CacheWriteTokens: 50 },
        });

        const { findByText } = render(<TokenUsagePanel lang="zh-Hans" />);

        await findByText('缓存读取');
        expect(findByText('缓存写入')).toBeTruthy();
        expect(findByText('50')).toBeTruthy();
    });

    it('never reports a cache hit rate above 100 percent', async () => {
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': { InputTokens: 100, OutputTokens: 20, Requests: 2, CachedRequests: 9 },
        });

        const { findByText, queryByText } = render(<TokenUsagePanel lang="zh-Hans" />);

        await findByText('100%');
        expect(queryByText('450%')).toBeNull();
    });

    it('drops point markers on a dense fully recorded series', async () => {
        const iso = (offset: number) => {
            const base = new Date();
            const date = new Date(base.getFullYear(), base.getMonth(), base.getDate() + offset);
            const month = String(date.getMonth() + 1).padStart(2, '0');
            const day = String(date.getDate()).padStart(2, '0');
            return `${date.getFullYear()}-${month}-${day}`;
        };
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': { InputTokens: 3000, OutputTokens: 300, TotalTokens: 3300 },
        });
        // Every one of the 30 day buckets recorded: 30 dots would be visual noise.
        getLLMTokenUsageByDayMock.mockResolvedValue(
            Array.from({ length: 30 }, (_, index) => ({
                date: iso(index - 29),
                provider: '智谱',
                input_tokens: 100,
                output_tokens: 10,
                total_tokens: 110,
            })),
        );

        const { getByTestId } = render(<TokenUsagePanel lang="zh-Hans" />);

        const trend = await waitFor(() => {
            const node = getByTestId('token-usage-trend');
            expect(node.getAttribute('data-total')).toBe('3300');
            return node;
        });
        // The line still shows the shape; only the per-point dots are suppressed.
        expect(trend.querySelector('.token-usage-trend__line')).toBeTruthy();
        expect(trend.querySelectorAll('.token-usage-trend__dot')).toHaveLength(0);
        // Nothing is unrecorded here, so no dashed gap is drawn.
        expect(trend.querySelector('.token-usage-trend__gap')).toBeNull();
    });

    it('keeps point markers when the series is sparse', async () => {
        const iso = (offset: number) => {
            const base = new Date();
            const date = new Date(base.getFullYear(), base.getMonth(), base.getDate() + offset);
            const month = String(date.getMonth() + 1).padStart(2, '0');
            const day = String(date.getDate()).padStart(2, '0');
            return `${date.getFullYear()}-${month}-${day}`;
        };
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': { InputTokens: 100, OutputTokens: 20, TotalTokens: 120 },
        });
        getLLMTokenUsageByDayMock.mockResolvedValue([
            { date: iso(0), provider: '智谱', input_tokens: 100, output_tokens: 20, total_tokens: 120 },
        ]);

        const { getByTestId } = render(<TokenUsagePanel lang="zh-Hans" />);

        const trend = await waitFor(() => {
            const node = getByTestId('token-usage-trend');
            expect(node.getAttribute('data-total')).toBe('120');
            return node;
        });
        // A single recorded day must stay visible as a marker.
        expect(trend.querySelectorAll('.token-usage-trend__dot')).toHaveLength(1);
    });

    it('breaks the curve at unrecorded days instead of drawing usage through them', async () => {
        const iso = (offset: number) => {
            const base = new Date();
            const date = new Date(base.getFullYear(), base.getMonth(), base.getDate() + offset);
            const month = String(date.getMonth() + 1).padStart(2, '0');
            const day = String(date.getDate()).padStart(2, '0');
            return `${date.getFullYear()}-${month}-${day}`;
        };
        getAllLLMTokenUsageMock.mockResolvedValue({
            '智谱': { InputTokens: 700, OutputTokens: 70, TotalTokens: 770 },
        });
        // Two isolated recorded days; everything between them has no record.
        getLLMTokenUsageByDayMock.mockResolvedValue([
            { date: iso(-3), provider: '智谱', input_tokens: 500, output_tokens: 50, total_tokens: 550 },
            { date: iso(0), provider: '智谱', input_tokens: 200, output_tokens: 20, total_tokens: 220 },
        ]);

        const { getByTestId } = render(<TokenUsagePanel lang="zh-Hans" />);

        const trend = await waitFor(() => {
            const node = getByTestId('token-usage-trend');
            expect(node.getAttribute('data-total')).toBe('770');
            return node;
        });

        const lines = Array.from(trend.querySelectorAll('.token-usage-trend__line'));
        // One curve per contiguous recorded run: a single joined curve would
        // imply usage on the unrecorded days in between.
        expect(lines).toHaveLength(2);
        // Two stretches have no record: the leading history and the middle span.
        expect(trend.querySelectorAll('.token-usage-trend__gap')).toHaveLength(2);
        // Each run must stay a single connected subpath (no cross-gap join).
        for (const line of lines) {
            expect((line.getAttribute('d')?.match(/M /g) || []).length).toBe(1);
        }
    });
});
