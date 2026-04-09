// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';

const GetMaclawLLMProvidersMock = vi.fn();
const SaveMaclawLLMProvidersMock = vi.fn();
const TestMaclawLLMMock = vi.fn();
const GetMaclawAgentMaxIterationsMock = vi.fn();
const IsFreeProxyRunningMock = vi.fn();
const DetectBrowserMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetMaclawLLMProviders: (...args: unknown[]) => GetMaclawLLMProvidersMock(...args),
    SaveMaclawLLMProviders: (...args: unknown[]) => SaveMaclawLLMProvidersMock(...args),
    TestMaclawLLM: (...args: unknown[]) => TestMaclawLLMMock(...args),
    GetMaclawAgentMaxIterations: (...args: unknown[]) => GetMaclawAgentMaxIterationsMock(...args),
    SetMaclawAgentMaxIterations: vi.fn(),
    StartOpenAIOAuth: vi.fn(),
    StartFreeProxy: vi.fn(),
    StopFreeProxy: vi.fn(),
    IsFreeProxyRunning: (...args: unknown[]) => IsFreeProxyRunningMock(...args),
    DetectBrowser: (...args: unknown[]) => DetectBrowserMock(...args),
    DangbeiLogin: vi.fn(),
    DangbeiFinishLogin: vi.fn(),
    DangbeiEnsureAuth: vi.fn(),
    GetFreeProxyModels: vi.fn().mockResolvedValue([]),
    GetFreeProxyModel: vi.fn().mockResolvedValue(''),
    SetFreeProxyModel: vi.fn(),
}));

vi.mock('../../providerLogos', () => ({ PROVIDER_LOGOS: {} }));
vi.mock('../UsageDisplay', () => ({ UsageDisplay: () => null }));
vi.mock('../TokenUsagePanel', () => ({ TokenUsagePanel: () => null }));
vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showAlert: vi.fn(),
        showConfirm: vi.fn(),
    }),
}));

import { LLMConfigPanel } from '../LLMConfigPanel';

describe('LLMConfigPanel test-and-save flow', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
            ],
            current: 'Custom1',
        });
        SaveMaclawLLMProvidersMock.mockResolvedValue(undefined);
        GetMaclawAgentMaxIterationsMock.mockResolvedValue(12);
        IsFreeProxyRunningMock.mockResolvedValue(false);
        DetectBrowserMock.mockResolvedValue({ found: 'false' });
    });

    afterEach(() => {
        cleanup();
    });

    it('tests first, then saves providers with final supports_vision', async () => {
        TestMaclawLLMMock.mockResolvedValue({
            message: 'hello',
            supports_vision: true,
        });

        render(<LLMConfigPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: '配置' }));

        fireEvent.change(await screen.findByPlaceholderText('https://api.openai.com/v1'), { target: { value: 'https://api.example.com/v1' } });
        fireEvent.change(screen.getByPlaceholderText('gpt-5.4'), { target: { value: 'gpt-test' } });
        fireEvent.change(screen.getByPlaceholderText('sk-...'), { target: { value: 'secret' } });

        fireEvent.click(screen.getByRole('button', { name: '检测并保存' }));

        await waitFor(() => {
            expect(TestMaclawLLMMock).toHaveBeenCalledWith({
                url: 'https://api.example.com/v1',
                key: 'secret',
                model: 'gpt-test',
                protocol: 'openai',
                agent_type: 'openclaw',
            });
        });

        await waitFor(() => {
            expect(SaveMaclawLLMProvidersMock).toHaveBeenCalledWith(
                [expect.objectContaining({ name: 'Custom1', supports_vision: true, url: 'https://api.example.com/v1', key: 'secret', model: 'gpt-test' })],
                'Custom1',
            );
        });

        expect(TestMaclawLLMMock.mock.invocationCallOrder[0]).toBeLessThan(SaveMaclawLLMProvidersMock.mock.invocationCallOrder[0]);
        expect(await screen.findByText(/图片理解：支持/)).toBeTruthy();
    });

    it('does not save when detection fails', async () => {
        TestMaclawLLMMock.mockRejectedValue(new Error('boom'));

        render(<LLMConfigPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: '配置' }));

        fireEvent.change(await screen.findByPlaceholderText('https://api.openai.com/v1'), { target: { value: 'https://api.example.com/v1' } });
        fireEvent.change(screen.getByPlaceholderText('gpt-5.4'), { target: { value: 'gpt-test' } });
        fireEvent.change(screen.getByPlaceholderText('sk-...'), { target: { value: 'secret' } });

        fireEvent.click(screen.getByRole('button', { name: '检测并保存' }));

        await waitFor(() => {
            expect(TestMaclawLLMMock).toHaveBeenCalled();
        });

        expect(SaveMaclawLLMProvidersMock).not.toHaveBeenCalled();
        expect(await screen.findByText(/连接失败，未保存/)).toBeTruthy();
    });
});
