// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup, act } from '@testing-library/react';

const GetMaclawLLMProvidersMock = vi.fn();
const SaveMaclawLLMProvidersMock = vi.fn();
const TestMaclawLLMMock = vi.fn();
const GetMaclawAgentMaxIterationsMock = vi.fn();
const GetHubLLMServiceStatusMock = vi.fn();
const FetchProviderModelsMock = vi.fn();
const LoadConfigMock = vi.fn();
const BrowserOpenURLMock = vi.fn();
const StartOpenAIOAuthMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetMaclawLLMProviders: (...args: unknown[]) => GetMaclawLLMProvidersMock(...args),
    SaveMaclawLLMProviders: (...args: unknown[]) => SaveMaclawLLMProvidersMock(...args),
    TestMaclawLLM: (...args: unknown[]) => TestMaclawLLMMock(...args),
    GetMaclawAgentMaxIterations: (...args: unknown[]) => GetMaclawAgentMaxIterationsMock(...args),
    GetHubLLMServiceStatus: (...args: unknown[]) => GetHubLLMServiceStatusMock(...args),
    LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
    SetMaclawAgentMaxIterations: vi.fn(),
    StartOpenAIOAuth: (...args: unknown[]) => StartOpenAIOAuthMock(...args),
    CancelOpenAIOAuth: vi.fn(),
    ImportCodexAuth: vi.fn(),
    FetchCodeGenModels: vi.fn(),
    FetchProviderModels: (...args: unknown[]) => FetchProviderModelsMock(...args),
    SaveCodeGenModelChoice: vi.fn(),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
    BrowserOpenURL: (...args: unknown[]) => BrowserOpenURLMock(...args),
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
import { hubOfficialStatus } from '../LLMConfigPanelShared';

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
        GetHubLLMServiceStatusMock.mockResolvedValue({ active: false });
        FetchProviderModelsMock.mockResolvedValue([{ id: 'gpt-test', name: 'GPT Test' }]);
        LoadConfigMock.mockResolvedValue({ remote_hub_url: 'https://hub.example.com/', remote_viewer_token: 'viewer token' });
    });

    afterEach(() => {
        vi.useRealTimers();
        cleanup();
    });

    it('tests first, then saves providers with final supports_vision', async () => {
        TestMaclawLLMMock.mockResolvedValue({
            message: 'hello',
            supports_vision: true,
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));

        fireEvent.change(await screen.findByPlaceholderText('https://api.openai.com/v1'), { target: { value: 'https://api.example.com/v1' } });
        fireEvent.change(screen.getByPlaceholderText('sk-...'), { target: { value: 'secret' } });
        fireEvent.click(screen.getByRole('button', { name: 'Fetch' }));

        await waitFor(() => {
            expect(FetchProviderModelsMock).toHaveBeenCalledWith('https://api.example.com/v1', 'secret', 'openai', 'openclaw');
        });

        fireEvent.change(screen.getAllByRole('combobox')[1], { target: { value: 'gpt-test' } });

        fireEvent.click(screen.getByRole('button', { name: 'Test & Save' }));

        await waitFor(() => {
            expect(TestMaclawLLMMock).toHaveBeenCalledWith({
                url: 'https://api.example.com/v1',
                key: 'secret',
                model: 'gpt-test',
                protocol: 'openai',
                agent_type: 'openclaw',
                wire_api: '',
            });
        });

        await waitFor(() => {
            expect(SaveMaclawLLMProvidersMock).toHaveBeenCalledWith(
                [expect.objectContaining({ name: 'Custom1', supports_vision: true, url: 'https://api.example.com/v1', key: 'secret', model: 'gpt-test' })],
                'Custom1',
            );
        });

        expect(TestMaclawLLMMock.mock.invocationCallOrder[0]).toBeLessThan(SaveMaclawLLMProvidersMock.mock.invocationCallOrder[0]);
        expect(await screen.findByText(/Vision support: enabled/)).toBeTruthy();
    });

    it('saves a custom User-Agent value', async () => {
        TestMaclawLLMMock.mockResolvedValue({ message: 'hello', supports_vision: false });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));
        fireEvent.click(screen.getByRole('button', { name: 'Custom' }));
        fireEvent.change(screen.getByPlaceholderText('Custom User-Agent'), { target: { value: 'myagent' } });
        fireEvent.change(await screen.findByPlaceholderText('https://api.openai.com/v1'), { target: { value: 'https://api.example.com/v1' } });
        fireEvent.change(screen.getByPlaceholderText('sk-...'), { target: { value: 'secret' } });
        fireEvent.click(screen.getByRole('button', { name: 'Fetch' }));

        await waitFor(() => {
            expect(FetchProviderModelsMock).toHaveBeenCalledWith('https://api.example.com/v1', 'secret', 'openai', 'myagent');
        });
        fireEvent.change(screen.getAllByRole('combobox')[1], { target: { value: 'gpt-test' } });
        fireEvent.click(screen.getByRole('button', { name: 'Test & Save' }));

        await waitFor(() => {
            expect(TestMaclawLLMMock).toHaveBeenCalledWith(expect.objectContaining({ agent_type: 'myagent' }));
        });
        await waitFor(() => {
            expect(SaveMaclawLLMProvidersMock).toHaveBeenCalledWith(
                [expect.objectContaining({ name: 'Custom1', agent_type: 'myagent' })],
                'Custom1',
            );
        });
    });

    it('keeps MaClaw Official visible when official grants are period-limited', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'coding-basic',
                active: false,
                status: 'period_limited',
                retry_after_seconds: 3600,
            }],
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));

        const officialButton = await screen.findByRole('button', { name: /MaClaw Official/i });
        expect(officialButton).toBeTruthy();
        expect(screen.getAllByText('Period limited').length).toBeGreaterThan(0);

        fireEvent.click(officialButton);

        expect(await screen.findByText(/Current period quota is exhausted/i)).toBeTruthy();
    });

    it('keeps MaClaw Official visible when the latest official grant is expired', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'coding-basic',
                active: false,
                status: 'expired',
            }],
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));

        const officialButton = await screen.findByRole('button', { name: /MaClaw Official/i });
        expect(officialButton).toBeTruthy();
        expect(screen.getAllByText('Grant expired').length).toBeGreaterThan(0);

        fireEvent.click(officialButton);

        expect(await screen.findByText(/Official authorization has expired/i)).toBeTruthy();
    });

    it('refreshes providers after saving the fallback MaClaw Official button', async () => {
        const onProviderChanged = vi.fn();
        GetMaclawLLMProvidersMock
            .mockResolvedValueOnce({
                providers: [
                    { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
                ],
                current: 'Custom1',
            })
            .mockResolvedValue({
                providers: [
                    { name: 'MaClaw\u5b98\u65b9', url: 'https://hub.example.com/api/llm/v1', key: 'viewer-token', model: 'auto', protocol: 'openai' },
                    { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
                ],
                current: 'MaClaw\u5b98\u65b9',
            });
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{ service_group_id: 'coding-basic', active: false, status: 'period_limited', retry_after_seconds: 3600 }],
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} onProviderChanged={onProviderChanged} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));
        fireEvent.click(await screen.findByRole('button', { name: /MaClaw Official/i }));
        fireEvent.click(screen.getByRole('button', { name: 'Use This Service' }));

        await waitFor(() => {
            expect(SaveMaclawLLMProvidersMock).toHaveBeenCalledWith(
                [expect.objectContaining({ name: 'Custom1' })],
                'MaClaw\u5b98\u65b9',
            );
        });
        await waitFor(() => {
            expect(GetMaclawLLMProvidersMock).toHaveBeenCalledTimes(2);
        });
        expect(onProviderChanged).toHaveBeenCalledTimes(1);
    });

    it('allows repairing MaClaw Official when current is official but provider is missing', async () => {
        GetMaclawLLMProvidersMock
            .mockResolvedValueOnce({
                providers: [
                    { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
                ],
                current: 'MaClaw\u5b98\u65b9',
            })
            .mockResolvedValue({
                providers: [
                    { name: 'MaClaw\u5b98\u65b9', url: 'https://hub.example.com/api/llm/v1', key: 'viewer-token', model: 'auto', protocol: 'openai' },
                    { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
                ],
                current: 'MaClaw\u5b98\u65b9',
            });
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{ service_group_id: 'coding-basic', active: false, status: 'period_limited', retry_after_seconds: 3600 }],
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));
        const repairButton = await screen.findByRole('button', { name: 'Use This Service' });
        expect((repairButton as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(repairButton);

        await waitFor(() => {
            expect(SaveMaclawLLMProvidersMock).toHaveBeenCalledWith(
                [expect.objectContaining({ name: 'Custom1' })],
                'MaClaw\u5b98\u65b9',
            );
        });
    });

    it('does not duplicate MaClaw Official when the synced provider already exists', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'MaClaw\u5b98\u65b9', url: 'https://hub.example.com/api/llm/v1', key: 'viewer-token', model: 'auto', protocol: 'openai' },
                { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
            ],
            current: 'Custom1',
        });
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{ service_group_id: 'coding-basic', active: false, status: 'period_limited', retry_after_seconds: 3600 }],
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));

        await screen.findByText('MaClaw\u5b98\u65b9');
        const maclawButtons = screen.getAllByRole('button').filter(button => button.textContent?.includes('MaClaw'));
        expect(maclawButtons).toHaveLength(1);
        expect(screen.getByText('Period limited')).toBeTruthy();
    });

    it('shows MaClaw Official as enabled when another official route is still active', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'MaClaw\u5b98\u65b9', url: 'https://hub.example.com/api/llm/v1', key: 'viewer-token', model: 'auto', protocol: 'openai' },
            ],
            current: 'MaClaw\u5b98\u65b9',
        });
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [
                { service_group_id: 'coding-basic', active: false, status: 'period_limited', retry_after_seconds: 3600 },
                { service_group_id: 'coding-plus', active: true, status: 'active' },
            ],
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));

        await waitFor(() => {
            expect(screen.getAllByText('MaClaw\u5b98\u65b9').length).toBeGreaterThan(0);
        });
        expect(screen.queryByText('Period limited')).toBeNull();
    });

    it('prioritizes queued grant status over exhausted older grants', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'MaClaw\u5b98\u65b9', url: 'https://hub.example.com/api/llm/v1', key: 'viewer-token', model: 'auto', protocol: 'openai' },
            ],
            current: 'MaClaw\u5b98\u65b9',
        });
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [
                { service_group_id: 'coding-old', active: false, status: 'exhausted' },
                { service_group_id: 'coding-next', active: false, status: 'queued', retry_after_seconds: 7200 },
            ],
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));

        expect((await screen.findAllByText('Not active yet')).length).toBeGreaterThan(0);
        expect(screen.queryByText('Credits exhausted')).toBeNull();
    });

    it('localizes inactive Hub service reasons in Chinese', () => {
        const t = (en: string, zhHans: string) => zhHans || en;
        const status = hubOfficialStatus({ active: false, inactive_reasons: ['grant credits are exhausted'] }, 'zh-Hans', t);

        expect(status.detail).toBe('授权额度已用尽。');
    });

    it('does not save when detection fails', async () => {
        TestMaclawLLMMock.mockRejectedValue(new Error('boom'));

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));

        fireEvent.change(await screen.findByPlaceholderText('https://api.openai.com/v1'), { target: { value: 'https://api.example.com/v1' } });
        fireEvent.change(screen.getByPlaceholderText('sk-...'), { target: { value: 'secret' } });
        fireEvent.click(screen.getByRole('button', { name: 'Fetch' }));

        await waitFor(() => {
            expect(FetchProviderModelsMock).toHaveBeenCalledWith('https://api.example.com/v1', 'secret', 'openai', 'openclaw');
        });

        fireEvent.change(screen.getAllByRole('combobox')[1], { target: { value: 'gpt-test' } });

        fireEvent.click(screen.getByRole('button', { name: 'Test & Save' }));

        await waitFor(() => {
            expect(TestMaclawLLMMock).toHaveBeenCalled();
        });

        expect(SaveMaclawLLMProvidersMock).not.toHaveBeenCalled();
        expect(await screen.findByText(/Connection failed, not saved/)).toBeTruthy();
    });

    it('hides the toast without clearing OAuth repair actions', async () => {
        vi.useFakeTimers({ shouldAdvanceTime: true });
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'OpenAI', url: 'https://api.openai.com/v1', key: '', model: 'gpt-4o', protocol: 'openai', auth_type: 'oauth' },
            ],
            current: 'OpenAI',
        });
        StartOpenAIOAuthMock.mockRejectedValue(new Error('oauth failed'));

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Configure' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Sign in with OpenAI' }));

        expect((await screen.findByRole('alert')).textContent).toMatch(/Connection failed, not saved/);
        expect(await screen.findByRole('button', { name: /Import from Codex CLI/i })).toBeTruthy();

        await act(async () => {
            vi.advanceTimersByTime(10000);
        });
        await waitFor(() => {
            expect(screen.queryByRole('alert')).toBeNull();
        });
        expect(screen.getByRole('button', { name: /Import from Codex CLI/i })).toBeTruthy();
    });

});
