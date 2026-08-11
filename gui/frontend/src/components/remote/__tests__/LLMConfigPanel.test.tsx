// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup, act } from '@testing-library/react';

const GetMaclawLLMProvidersMock = vi.fn();
const SaveMaclawLLMProvidersMock = vi.fn();
const TestMaclawLLMMock = vi.fn();
const GetMaclawAgentMaxIterationsMock = vi.fn();
const GetMaclawLLMThinkingModeMock = vi.fn();
const SetMaclawLLMThinkingModeMock = vi.fn();
const GetSubAgentConcurrencyMock = vi.fn();
const GetHubLLMServiceStatusMock = vi.fn();
const FetchProviderModelsMock = vi.fn();
const CreateMobileLLMDesktopQRSessionMock = vi.fn();
const LoadConfigMock = vi.fn();
const BrowserOpenURLMock = vi.fn();
const EventsOnMock = vi.fn();
let xaiOAuthEventHandler: ((payload: Record<string, unknown>) => unknown) | undefined;
const StartOpenAIOAuthMock = vi.fn();
const StartXAIOAuthMock = vi.fn();
const CancelXAIOAuthURLMock = vi.fn();
const GetMoAConfigMock = vi.fn();
const SaveMoAConfigMock = vi.fn();
const GetMaclawLLMProfilePanelStateMock = vi.fn();
const SaveMaclawLLMProfilesMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetMaclawLLMProviders: (...args: unknown[]) => GetMaclawLLMProvidersMock(...args),
    SaveMaclawLLMProviders: (...args: unknown[]) => SaveMaclawLLMProvidersMock(...args),
    TestMaclawLLM: (...args: unknown[]) => TestMaclawLLMMock(...args),
    GetMaclawAgentMaxIterations: (...args: unknown[]) => GetMaclawAgentMaxIterationsMock(...args),
    GetMaclawLLMThinkingMode: (...args: unknown[]) => GetMaclawLLMThinkingModeMock(...args),
    GetSubAgentConcurrency: (...args: unknown[]) => GetSubAgentConcurrencyMock(...args),
    GetHubLLMServiceStatus: (...args: unknown[]) => GetHubLLMServiceStatusMock(...args),
    LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
    SetMaclawAgentMaxIterations: vi.fn(),
    SetMaclawLLMThinkingMode: (...args: unknown[]) => SetMaclawLLMThinkingModeMock(...args),
    SetSubAgentConcurrency: vi.fn(),
    StartOpenAIOAuth: (...args: unknown[]) => StartOpenAIOAuthMock(...args),
    StartXAIOAuth: (...args: unknown[]) => StartXAIOAuthMock(...args),
    CancelXAIOAuthURL: (...args: unknown[]) => CancelXAIOAuthURLMock(...args),
    CancelOpenAIOAuth: vi.fn(),
    ImportCodexAuth: vi.fn(),
    FetchCodeGenModels: vi.fn(),
    FetchProviderModels: (...args: unknown[]) => FetchProviderModelsMock(...args),
    CreateMobileLLMDesktopQRSession: (...args: unknown[]) => CreateMobileLLMDesktopQRSessionMock(...args),
    SaveCodeGenModelChoice: vi.fn(),
    GetMoAConfig: (...args: unknown[]) => GetMoAConfigMock(...args),
    SaveMoAConfig: (...args: unknown[]) => SaveMoAConfigMock(...args),
    GetMaclawLLMProfilePanelState: (...args: unknown[]) => GetMaclawLLMProfilePanelStateMock(...args),
    SaveMaclawLLMProfiles: (...args: unknown[]) => SaveMaclawLLMProfilesMock(...args),
    GetMoASessionState: vi.fn().mockResolvedValue({ sticky: false }),
    SetMoASticky: vi.fn(),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: (...args: unknown[]) => {
        EventsOnMock(...args);
        if (args[0] === 'xai-oauth-complete' && typeof args[1] === 'function') {
            xaiOAuthEventHandler = args[1] as (payload: Record<string, unknown>) => unknown;
        }
        return vi.fn();
    },
    EventsOff: vi.fn(),
    BrowserOpenURL: (...args: unknown[]) => BrowserOpenURLMock(...args),
    ClipboardSetText: vi.fn(),
}));

vi.mock('../../providerLogos', () => ({ PROVIDER_LOGOS: {} }));
vi.mock('../UsageDisplay', () => ({ UsageDisplay: () => null }));
vi.mock('../TokenUsagePanel', () => ({ TokenUsagePanel: () => null }));
vi.mock('../LLMProfileAssignments', () => ({ LLMProfileAssignments: () => null }));
vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showAlert: vi.fn(),
        showConfirm: vi.fn(),
        showPrompt: vi.fn(),
    }),
}));

import { LLMConfigPanel } from '../LLMConfigPanel';
import { hubOfficialStatus } from '../LLMConfigPanelShared';

describe('LLMConfigPanel test-and-save flow', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        xaiOAuthEventHandler = undefined;
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
            ],
            current: 'Custom1',
        });
        SaveMaclawLLMProvidersMock.mockResolvedValue(undefined);
        GetMaclawAgentMaxIterationsMock.mockResolvedValue(12);
        GetMaclawLLMThinkingModeMock.mockResolvedValue('');
        SetMaclawLLMThinkingModeMock.mockResolvedValue(undefined);
        GetSubAgentConcurrencyMock.mockResolvedValue(2);
        GetHubLLMServiceStatusMock.mockResolvedValue({ active: false });
        GetMoAConfigMock.mockResolvedValue({ enabled: false, presets: {} });
        SaveMoAConfigMock.mockResolvedValue(undefined);
        GetMaclawLLMProfilePanelStateMock.mockResolvedValue({
            providers: [{ id: 'custom1', name: 'Custom1', model: '', models: [], supports_vision: false }],
            profiles: { version: 1, assistant: { provider_id: 'custom1', model: '' }, coding: { inherit_assistant: true } },
            assistant: { profile: 'assistant', provider_id: 'custom1', provider_name: 'Custom1', model: '', health: 'unverified' },
            coding: { profile: 'coding', provider_id: 'custom1', provider_name: 'Custom1', model: '', inherit_assistant: true, health: 'unverified' },
            revision: 'test-revision',
        });
        SaveMaclawLLMProfilesMock.mockResolvedValue(undefined);
        FetchProviderModelsMock.mockResolvedValue([{ id: 'gpt-test', name: 'GPT Test' }]);
        CreateMobileLLMDesktopQRSessionMock.mockResolvedValue({
            status: 'created',
            session_id: 'mlqr_test',
            expires_at: '2026-07-02T12:00:00Z',
            qr_payload: '{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_test","hub_url":"https://tenant-a.maclaw.top"}',
        });
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

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));

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
                provider_name: 'Custom1',
                auth_type: '',
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

    it('keeps configuration unavailable until a slow provider read has completed', async () => {
        let resolveProviders: ((value: unknown) => void) | undefined;
        GetMaclawLLMProvidersMock.mockImplementationOnce(() => new Promise(resolve => {
            resolveProviders = resolve;
        }));

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        // The independent settings calls finish first, as they can on a slow
        // desktop. The action is disabled until the local config is known, so
        // a pending read cannot be mistaken for an empty provider list.
        const configureButton = await screen.findByRole('button', { name: 'Manage providers' });
        expect((configureButton as HTMLButtonElement).disabled).toBe(true);
        expect(screen.getByText('Reading saved providers…')).toBeTruthy();

        await act(async () => {
            resolveProviders?.({
                providers: [{ name: 'Slow provider', url: 'https://api.example.com/v1', key: 'secret', model: 'model-1' }],
                current: 'Slow provider',
            });
        });

        await waitFor(() => expect((configureButton as HTMLButtonElement).disabled).toBe(false));
        fireEvent.click(configureButton);
        expect(await screen.findByRole('button', { name: 'Slow provider' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'None' })).toBeTruthy();
    });

    it('keeps the settings surface available while the provider read is pending', async () => {
        GetMaclawLLMProvidersMock.mockImplementationOnce(() => new Promise(() => {}));

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('Agent Max Iterations')).toBeTruthy();
        expect(screen.getByText('Reading saved providers…')).toBeTruthy();
        expect((screen.getByRole('button', { name: 'Manage providers' }) as HTMLButtonElement).disabled).toBe(true);
        expect((screen.getByRole('button', { name: 'Mobile QR' }) as HTMLButtonElement).disabled).toBe(true);
    });

    it('keeps waiting after five seconds and explains that the provider read is slow', async () => {
        vi.useFakeTimers();
        let resolveProviders: ((value: unknown) => void) | undefined;
        GetMaclawLLMProvidersMock.mockImplementationOnce(() => new Promise(resolve => {
            resolveProviders = resolve;
        }));

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);
        await act(async () => { await vi.advanceTimersByTimeAsync(5000); });

        expect(screen.getByText('Still reading saved providers…')).toBeTruthy();
        expect((screen.getByRole('button', { name: 'Manage providers' }) as HTMLButtonElement).disabled).toBe(true);

        await act(async () => {
            resolveProviders?.({
                providers: [{ name: 'Slow provider', url: 'https://api.example.com/v1', key: 'secret', model: 'model-1' }],
                current: 'Slow provider',
            });
            await Promise.resolve();
        });
        expect((screen.getByRole('button', { name: 'Manage providers' }) as HTMLButtonElement).disabled).toBe(false);
    });

    it('offers retry when the provider bridge throws synchronously', async () => {
        GetMaclawLLMProvidersMock.mockImplementationOnce(() => {
            throw new Error('bridge unavailable');
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        expect(await screen.findByText("Couldn't read LLM providers. Click retry.")).toBeTruthy();
        const configureButton = screen.getByRole('button', { name: 'Manage providers' }) as HTMLButtonElement;
        expect(configureButton.disabled).toBe(true);
        fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
        await waitFor(() => expect(GetMaclawLLMProvidersMock).toHaveBeenCalledTimes(2));
        await waitFor(() => expect((screen.getByRole('button', { name: 'Manage providers' }) as HTMLButtonElement).disabled).toBe(false));
    });

    it('does not let an older Hub status response overwrite a newer one', async () => {
        let resolveFirstStatus: ((value: unknown) => void) | undefined;
        GetHubLLMServiceStatusMock
            .mockImplementationOnce(() => new Promise(resolve => { resolveFirstStatus = resolve; }))
            .mockResolvedValueOnce({ active: true });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);
        await waitFor(() => expect(GetHubLLMServiceStatusMock).toHaveBeenCalledTimes(2));
        await act(async () => {
            resolveFirstStatus?.({ active: false });
            await Promise.resolve();
        });

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        expect(await screen.findByRole('button', { name: 'MaClaw Official' })).toBeTruthy();
    });

    it('does not apply a provider result after the panel has unmounted', async () => {
        let resolveProviders: ((value: unknown) => void) | undefined;
        GetMaclawLLMProvidersMock.mockImplementationOnce(() => new Promise(resolve => {
            resolveProviders = resolve;
        }));

        const view = render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);
        view.unmount();
        await act(async () => {
            resolveProviders?.({
                providers: [{ name: 'Late provider', url: 'https://api.example.com/v1', key: 'secret', model: 'model-1' }],
                current: 'Late provider',
            });
            await Promise.resolve();
        });

        expect(screen.queryByText('Late provider')).toBeNull();
    });

    it('preserves confirmed vision support when the probe is inconclusive', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'Custom1', url: 'https://api.example.com/v1', key: 'secret', model: 'gpt-test', protocol: 'openai', is_custom: true, supports_vision: true },
            ],
            current: 'Custom1',
        });
        TestMaclawLLMMock.mockResolvedValue({
            message: 'hello',
            supports_vision: false,
            vision_probe_status: 'inconclusive',
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);
        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        fireEvent.change(await screen.findByPlaceholderText('sk-...'), { target: { value: 'updated-secret' } });
        fireEvent.click(screen.getByRole('button', { name: 'Test & Save' }));

        await waitFor(() => {
            expect(SaveMaclawLLMProvidersMock).toHaveBeenCalledWith(
                [expect.objectContaining({ name: 'Custom1', supports_vision: true })],
                'Custom1',
            );
        });
        expect(await screen.findByText(/Vision support: not confirmed/)).toBeTruthy();
    });

    it('persists the global thinking mode via the thinking card', async () => {
        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        const onButton = await screen.findByTestId('thinking-mode-enabled');
        const autoButton = screen.getByTestId('thinking-mode-auto');

        fireEvent.click(onButton);
        await waitFor(() => {
            expect(SetMaclawLLMThinkingModeMock).toHaveBeenCalledWith('enabled');
        });

        fireEvent.click(autoButton);
        await waitFor(() => {
            expect(SetMaclawLLMThinkingModeMock).toHaveBeenCalledWith('');
        });
    });

    it('reflects the loaded global thinking mode as active', async () => {
        GetMaclawLLMThinkingModeMock.mockResolvedValue('disabled');

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        const offButton = await screen.findByTestId('thinking-mode-disabled');
        await waitFor(() => {
            expect(offButton.style.background).not.toBe('');
        });
    });

    it('saves a custom User-Agent value', async () => {
        TestMaclawLLMMock.mockResolvedValue({ message: 'hello', supports_vision: false });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
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

    it('shows the Volcengine Agent Plan provider chip without protocol variants', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'MaClaw\u5b98\u65b9', url: 'https://hub.example.com/api/llm/v1', key: 'viewer-token', model: 'auto', protocol: 'openai' },
                { name: '\u706b\u5c71\u5f15\u64ce Agent Plan', url: 'https://ark.cn-beijing.volces.com/api/plan/v3', key: '', model: 'glm-5.2', protocol: 'openai', wire_api: 'responses', supports_vision: false },
                { name: 'Custom1', url: '', key: '', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
            ],
            current: 'Custom1',
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));

        expect(await screen.findByRole('button', { name: '\u706b\u5c71\u5f15\u64ce Agent Plan' })).toBeTruthy();
    });

    it('rolls back the reasoning selection and reports an error when saving fails', async () => {
        SetMaclawLLMThinkingModeMock.mockRejectedValueOnce(new Error('offline'));

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        const onButton = await screen.findByTestId('thinking-mode-enabled');
        fireEvent.click(onButton);

        await waitFor(() => {
            expect(SetMaclawLLMThinkingModeMock).toHaveBeenCalledWith('enabled');
            expect(screen.getByRole('alert').textContent).toContain("Couldn't save the reasoning setting");
        });
        expect(screen.getByTestId('thinking-mode-auto').getAttribute('aria-pressed')).toBe('true');
    });

    it('prevents duplicate reasoning saves before React applies the disabled state', async () => {
        let resolveSave: (() => void) | undefined;
        SetMaclawLLMThinkingModeMock.mockImplementationOnce(() => new Promise<void>(resolve => { resolveSave = resolve; }));

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        const onButton = await screen.findByTestId('thinking-mode-enabled');
        fireEvent.click(onButton);
        fireEvent.click(onButton);
        expect(SetMaclawLLMThinkingModeMock).toHaveBeenCalledTimes(1);

        act(() => { resolveSave?.(); });
        await waitFor(() => expect((onButton as HTMLButtonElement).disabled).toBe(false));
    });

    it('disables model fetch for Volcengine Agent Plan (preset model, enter manually)', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: '\u706b\u5c71\u5f15\u64ce Agent Plan', url: 'https://ark.cn-beijing.volces.com/api/plan/v3', key: 'secret', model: 'glm-5.2', protocol: 'openai', wire_api: 'responses', supports_vision: false },
            ],
            current: '\u706b\u5c71\u5f15\u64ce Agent Plan',
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        // Product intentionally disables Fetch for this preset provider.
        const fetchBtn = screen.queryByRole('button', { name: 'Fetch' });
        if (fetchBtn) {
            expect((fetchBtn as HTMLButtonElement).disabled).toBe(true);
            fireEvent.click(fetchBtn);
        }
        expect(FetchProviderModelsMock).not.toHaveBeenCalled();
        expect(screen.getByText(/preset model, enter manually/i)).toBeTruthy();
    });

    it('fetches OAuth provider models without requiring a visible API Key field', async () => {
        FetchProviderModelsMock.mockResolvedValue([{ id: 'claude-sonnet-4', name: 'Claude Sonnet 4' }]);
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                {
                    name: 'GitHub Copilot',
                    url: 'https://api.githubcopilot.com',
                    // Hydrated token from backend credential store — no API Key UI for OAuth.
                    key: 'managed-oauth-token',
                    model: 'claude-sonnet-4',
                    protocol: 'openai',
                    auth_type: 'oauth',
                },
            ],
            current: 'GitHub Copilot',
        });

        render(<LLMConfigPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: '服务商管理' }));
        // OAuth authenticated state should be shown, not an API Key input.
        expect(await screen.findByText(/OAuth 已认证|OAuth authenticated/)).toBeTruthy();
        expect(screen.queryByPlaceholderText('sk-...')).toBeNull();

        fireEvent.click(screen.getByRole('button', { name: '获取' }));

        await waitFor(() => {
            expect(FetchProviderModelsMock).toHaveBeenCalledWith(
                'https://api.githubcopilot.com',
                'managed-oauth-token',
                'openai',
                'openclaw',
            );
        });
        expect(screen.queryByText(/请先填写 API Key/)).toBeNull();
    });

    it('lets backend resolve OAuth models when frontend key is empty', async () => {
        FetchProviderModelsMock.mockResolvedValue([{ id: 'claude-sonnet-4', name: 'Claude Sonnet 4' }]);
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                {
                    name: 'GitHub Copilot',
                    url: 'https://api.githubcopilot.com',
                    key: '', // not hydrated yet; backend must resolve from credential store
                    model: 'claude-sonnet-4',
                    protocol: 'openai',
                    auth_type: 'oauth',
                },
            ],
            current: 'GitHub Copilot',
        });

        render(<LLMConfigPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: '服务商管理' }));
        fireEvent.click(screen.getByRole('button', { name: '获取' }));

        await waitFor(() => {
            // Empty key is OK for OAuth — backend resolves internally.
            expect(FetchProviderModelsMock).toHaveBeenCalledWith(
                'https://api.githubcopilot.com',
                '',
                'openai',
                'openclaw',
            );
        });
        expect(screen.queryByText(/请先填写 API Key/)).toBeNull();
    });

    it('quick-fills Volcengine Agent Plan endpoint with the tested model defaults', async () => {
        TestMaclawLLMMock.mockResolvedValue({ message: 'hello', supports_vision: false });
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                {
                    name: 'Custom1',
                    url: '',
                    key: '',
                    model: '',
                    protocol: 'anthropic',
                    agent_type: 'claude code 2.0',
                    wire_api: 'anthropic',
                    is_custom: true,
                    supports_vision: false,
                },
            ],
            current: 'Custom1',
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        fireEvent.change(screen.getAllByRole('combobox')[0], { target: { value: '\u706b\u5c71\u5f15\u64ce Agent Plan' } });
        fireEvent.change(screen.getByPlaceholderText('sk-...'), { target: { value: 'secret' } });
        fireEvent.click(screen.getByRole('button', { name: 'Test & Save' }));

        await waitFor(() => {
            expect(TestMaclawLLMMock).toHaveBeenCalledWith({
                url: 'https://ark.cn-beijing.volces.com/api/plan/v3',
                key: 'secret',
                model: 'glm-5.2',
                protocol: 'openai',
                agent_type: 'openclaw',
                wire_api: 'responses',
                provider_name: '火山引擎 Agent Plan',
                auth_type: '',
            });
        });
    });

    it('quick-fills xAI-Grok Build as an OAuth provider', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValueOnce({
            providers: [
                { name: 'Custom1', url: '', key: 'previous-api-key', model: '', protocol: 'openai', is_custom: true, supports_vision: false },
            ],
            current: 'Custom1',
        }).mockResolvedValueOnce({
            providers: [
                { name: 'xAI-Grok', url: 'https://api.x.ai/v1', key: 'oauth-token', model: 'grok-4.5', protocol: 'openai', auth_type: 'oauth', wire_api: 'responses', is_custom: true },
            ],
            current: 'xAI-Grok',
        });
        StartXAIOAuthMock.mockResolvedValue('https://auth.x.ai/authorize?state=test');

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        fireEvent.change(screen.getAllByRole('combobox')[0], { target: { value: 'xAI-Grok' } });
        expect(screen.getByRole('button', { name: 'Sign in with xAI' })).toBeTruthy();
        expect(screen.queryByPlaceholderText('sk-...')).toBeNull();
        fireEvent.click(screen.getByRole('button', { name: 'Sign in with xAI' }));

        await waitFor(() => {
            expect(StartXAIOAuthMock).toHaveBeenCalledTimes(1);
        });
        expect(TestMaclawLLMMock).not.toHaveBeenCalled();
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

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));

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

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));

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

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
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

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
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

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));

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

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));

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

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));

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

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));

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

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
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

    it('uses the dedicated xAI OAuth flow', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'xAI-Grok', url: 'https://api.x.ai/v1', key: '', model: 'grok-4.5', protocol: 'openai', auth_type: 'oauth', wire_api: 'responses' },
            ],
            current: 'xAI-Grok',
        });
        StartXAIOAuthMock.mockResolvedValue('https://auth.x.ai/authorize?state=test');

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Sign in with xAI' }));

        await waitFor(() => {
            expect(StartXAIOAuthMock).toHaveBeenCalledTimes(1);
        });
        expect(StartOpenAIOAuthMock).not.toHaveBeenCalled();
        expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://auth.x.ai/authorize?state=test');
    });

    it('keeps the xAI OAuth recovery controls available when opening the browser fails', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'xAI-Grok', url: 'https://api.x.ai/v1', key: '', model: 'grok-4.5', protocol: 'openai', auth_type: 'oauth', wire_api: 'responses' },
            ],
            current: 'xAI-Grok',
        });
        StartXAIOAuthMock.mockResolvedValue('https://auth.x.ai/authorize?state=test');
        BrowserOpenURLMock.mockImplementation(() => { throw new Error('browser bridge unavailable'); });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Sign in with xAI' }));

        expect((await screen.findByRole('alert')).textContent).toMatch(/Couldn't open the browser automatically/);
        expect(screen.getByRole('button', { name: 'Open browser again' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Copy sign-in link' })).toBeTruthy();
    });

    it('ignores a completion event from a superseded xAI OAuth session', async () => {
        const onStatusChange = vi.fn();
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'xAI-Grok', url: 'https://api.x.ai/v1', key: '', model: 'grok-4.5', protocol: 'openai', auth_type: 'oauth', wire_api: 'responses' },
            ],
            current: 'xAI-Grok',
        });
        StartXAIOAuthMock.mockResolvedValue('https://auth.x.ai/authorize?state=current');

        render(<LLMConfigPanel lang="en" onStatusChange={onStatusChange} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Sign in with xAI' }));
        await waitFor(() => expect(xaiOAuthEventHandler).toBeTypeOf('function'));

        await act(async () => {
            await xaiOAuthEventHandler?.({ ok: true, authorization_url: 'https://auth.x.ai/authorize?state=stale' });
        });
        expect(screen.getByRole('button', { name: 'Cancel OAuth login' })).toBeTruthy();
        expect(onStatusChange).not.toHaveBeenCalled();

        await act(async () => {
            await xaiOAuthEventHandler?.({ ok: true, authorization_url: 'https://auth.x.ai/authorize?state=current' });
        });
        await waitFor(() => expect(onStatusChange).toHaveBeenCalledWith(true, true));
    });

    it('does not offer Codex CLI import after xAI OAuth fails', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'xAI-Grok', url: 'https://api.x.ai/v1', key: '', model: 'grok-4.5', protocol: 'openai', auth_type: 'oauth', wire_api: 'responses' },
            ],
            current: 'xAI-Grok',
        });
        StartXAIOAuthMock.mockRejectedValue(new Error('xAI OAuth failed'));

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Sign in with xAI' }));

        expect((await screen.findByRole('alert')).textContent).toMatch(/Connection failed, not saved/);
        expect(screen.queryByRole('button', { name: /Import from Codex CLI/i })).toBeNull();
    });

    it('cancels only its own in-progress xAI OAuth flow through the dedicated binding', async () => {
        const authorizationURL = 'https://auth.x.ai/authorize?state=owned-by-panel';
        StartXAIOAuthMock.mockResolvedValue(authorizationURL);
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'xAI-Grok', url: 'https://api.x.ai/v1', key: '', model: 'grok-4.5', protocol: 'openai', auth_type: 'oauth', wire_api: 'responses' },
            ],
            current: 'xAI-Grok',
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Sign in with xAI' }));
        await waitFor(() => expect(BrowserOpenURLMock).toHaveBeenCalledWith(authorizationURL));
        fireEvent.click(await screen.findByRole('button', { name: 'Cancel OAuth login' }));

        expect(CancelXAIOAuthURLMock).toHaveBeenCalledWith(authorizationURL);
    });

    it('ignores an xAI authorization URL that resolves after the user cancels', async () => {
        let resolveAuthorizationURL: ((value: string) => void) | undefined;
        StartXAIOAuthMock.mockImplementation(() => new Promise<string>((resolve) => {
            resolveAuthorizationURL = resolve;
        }));
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'xAI-Grok', url: 'https://api.x.ai/v1', key: '', model: 'grok-4.5', protocol: 'openai', auth_type: 'oauth', wire_api: 'responses' },
            ],
            current: 'xAI-Grok',
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);

        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Sign in with xAI' }));
        fireEvent.click(await screen.findByRole('button', { name: 'Cancel OAuth login' }));
        await act(async () => resolveAuthorizationURL?.('https://auth.x.ai/authorize?state=late'));

        expect(BrowserOpenURLMock).not.toHaveBeenCalledWith('https://auth.x.ai/authorize?state=late');
        expect(CancelXAIOAuthURLMock).not.toHaveBeenCalled();
    });

    it('does not save an unsigned xAI OAuth provider', async () => {
        GetMaclawLLMProvidersMock.mockResolvedValue({
            providers: [
                { name: 'xAI-Grok', url: 'https://api.x.ai/v1', key: '', model: 'grok-4.5', protocol: 'openai', auth_type: 'oauth', wire_api: 'responses' },
            ],
            current: 'xAI-Grok',
        });

        render(<LLMConfigPanel lang="en" onStatusChange={vi.fn()} />);
        fireEvent.click(await screen.findByRole('button', { name: 'Manage providers' }));
        fireEvent.click(screen.getByRole('button', { name: 'Test & Save' }));

        expect(await screen.findByText('Please complete OAuth login before saving')).toBeTruthy();
        expect(SaveMaclawLLMProvidersMock).not.toHaveBeenCalled();
    });

});
