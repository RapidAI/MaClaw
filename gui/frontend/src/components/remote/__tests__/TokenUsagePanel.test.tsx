import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor, fireEvent } from '@testing-library/react';

const getMaclawLLMProvidersMock = vi.fn();
const getAllLLMTokenUsageMock = vi.fn();
const resetLLMTokenUsageMock = vi.fn();
const runtimeHandlers = new Map<string, (payload?: unknown) => void>();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetMaclawLLMProviders: (...args: unknown[]) => getMaclawLLMProvidersMock(...args),
    GetAllLLMTokenUsage: (...args: unknown[]) => getAllLLMTokenUsageMock(...args),
    ResetLLMTokenUsage: (...args: unknown[]) => resetLLMTokenUsageMock(...args),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((event: string, handler: (payload?: unknown) => void) => {
        runtimeHandlers.set(event, handler);
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
        resetLLMTokenUsageMock.mockReset();
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

        await waitFor(() => {
            expect(getByText('180')).toBeTruthy();
            expect(getByText('40')).toBeTruthy();
            expect(getByText('220')).toBeTruthy();
        });
        expect(getAllLLMTokenUsageMock).toHaveBeenCalledTimes(2);
    });
});
