// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';

const GetHubLLMServiceStatusMock = vi.fn();
const RefreshHubLLMServiceStatusMock = vi.fn();
const RedeemHubLLMServiceMock = vi.fn();
const LoadConfigMock = vi.fn();
const BrowserOpenURLMock = vi.fn();
const EventsOnMock = vi.fn();
const EventsOffMock = vi.fn();
const eventHandlers = new Map<string, (...args: unknown[]) => void>();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetHubLLMServiceStatus: (...args: unknown[]) => GetHubLLMServiceStatusMock(...args),
    RefreshHubLLMServiceStatus: (...args: unknown[]) => RefreshHubLLMServiceStatusMock(...args),
    RedeemHubLLMService: (...args: unknown[]) => RedeemHubLLMServiceMock(...args),
    LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: (...args: unknown[]) => BrowserOpenURLMock(...args),
    EventsOn: (...args: unknown[]) => EventsOnMock(...args),
    EventsOff: (...args: unknown[]) => EventsOffMock(...args),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showAlert: vi.fn(),
        showConfirm: vi.fn(),
        showPrompt: vi.fn(),
    }),
}));

import { HubServiceRedeemPanel } from '../HubServiceRedeemPanel';

describe('HubServiceRedeemPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        eventHandlers.clear();
        EventsOnMock.mockImplementation((eventName: string, callback: (...args: unknown[]) => void) => {
            eventHandlers.set(eventName, callback);
            return () => {
                if (eventHandlers.get(eventName) === callback) eventHandlers.delete(eventName);
            };
        });
        EventsOffMock.mockImplementation((...eventNames: string[]) => {
            eventNames.forEach((eventName) => eventHandlers.delete(eventName));
        });
        const defaultStatus = {
            active: true,
            skip_llm_config: true,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
        };
        GetHubLLMServiceStatusMock.mockResolvedValue(defaultStatus);
        RefreshHubLLMServiceStatusMock.mockImplementation(() => GetHubLLMServiceStatusMock());
        LoadConfigMock.mockResolvedValue({ remote_hub_url: 'https://hub.example.com/', remote_viewer_token: 'viewer token', remote_tenant_id: 'tenant acme', remote_email: 'dev@example.com' });
        RedeemHubLLMServiceMock.mockResolvedValue({ active: true, skip_llm_config: true });
    });

    afterEach(() => {
        cleanup();
    });

    it('shows period-limit status and recovery time instead of generic inactive state', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            skip_llm_config: false,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            nearest_expires_at: '2026-05-06T00:00:00Z',
            credit_grants: [{
                service_group_id: 'coding-basic',
                source: 'card',
                expires_at: '2026-05-06T00:00:00Z',
                active: false,
                status: 'period_limited',
                credits_total: 100,
                credits_used: 10,
                credits_remaining: 90,
                retry_after_seconds: 3600,
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('Period limited')).toBeTruthy();
        expect(screen.getByText(/current period quota is exhausted/i)).toBeTruthy();
        expect(screen.getAllByText(/recovers in about 1h/i).length).toBeGreaterThan(0);
        expect(screen.queryByText('Not Active')).toBeNull();
        expect(screen.getByText('Grant credit details')).toBeTruthy();
        expect(screen.queryByText('Active Grants')).toBeNull();
    });

    it('does not show tenant compute cards as personal grant credits', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                id: 'auth-card-1',
                card_order_id: 'HC-ORDER-1',
                service_group_id: 'maclaw_official_group',
                source: 'hubcenter_compute',
                starts_at: '2026-05-01T00:00:00Z',
                expires_at: '2026-06-01T00:00:00Z',
                active: true,
                status: 'active',
                credits_total: 1000,
                credits_used: 123.45,
                credits_remaining: 876.55,
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('Authorization Breakdown')).toBeTruthy();
        expect(screen.queryByText('HC-ORDER-1')).toBeNull();
        expect(screen.queryByText('Compute Card')).toBeNull();
        expect(screen.queryByText('123.45')).toBeNull();
    });

    it('shows queued authorization status and start time instead of generic inactive state', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            skip_llm_config: false,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'coding-basic',
                source: 'card',
                expires_at: '2026-05-06T00:00:00Z',
                active: false,
                status: 'queued',
                credits_total: 100,
                credits_used: 0,
                credits_remaining: 100,
                retry_after_seconds: 7200,
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('Not active yet')).toBeTruthy();
        expect(screen.getByText(/authorization starts in about 2h/i)).toBeTruthy();
        expect(screen.getAllByText(/starts in about 2h/i).length).toBeGreaterThan(0);
        expect(screen.queryByText('Not Active')).toBeNull();
    });

    it('prioritizes queued authorization over exhausted older grants', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            skip_llm_config: false,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'coding-old',
                source: 'card',
                expires_at: '2026-05-06T00:00:00Z',
                active: false,
                status: 'exhausted',
            }, {
                service_group_id: 'coding-next',
                source: 'card',
                expires_at: '2026-05-06T00:00:00Z',
                active: false,
                status: 'queued',
                retry_after_seconds: 7200,
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('Not active yet')).toBeTruthy();
        expect(screen.getByText(/authorization starts in about 2h/i)).toBeTruthy();
        expect(screen.getByText('Credits exhausted')).toBeTruthy();
    });

    it('keeps overall status active when one official grant remains usable', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'coding-basic',
                source: 'card',
                expires_at: '2026-05-06T00:00:00Z',
                active: false,
                status: 'period_limited',
                credits_total: 100,
                credits_used: 10,
                credits_remaining: 90,
                retry_after_seconds: 3600,
            }, {
                service_group_id: 'coding-plus',
                source: 'card',
                expires_at: '2026-05-06T00:00:00Z',
                active: true,
                status: 'active',
                credits_total: 100,
                credits_used: 1,
                credits_remaining: 99,
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        expect((await screen.findAllByText('Active')).length).toBeGreaterThan(0);
        expect(screen.queryByText('Period limited')).toBeNull();
        expect(screen.queryByText(/current period quota is exhausted/i)).toBeNull();
        expect(screen.getByText(/Period limit exhausted/i)).toBeTruthy();
    });

    it('uses available period credits in grant detail rows when total remaining is unlimited', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credits_available: 25,
            credit_grants: [{
                service_group_id: 'coding-unlimited',
                source: 'card',
                expires_at: '2026-05-06T00:00:00Z',
                active: true,
                status: 'active',
                credits_total: 0,
                credits_used: 5,
                credits_remaining: 0,
                credits_available: 25,
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('coding-unlimited')).toBeTruthy();
        expect(screen.getAllByText('25').length).toBeGreaterThan(0);
    });

    it('uses grant-level available credits in summary when status totals are missing', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['newbie'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'newbie',
                source: 'card',
                expires_at: '2026-06-06T00:00:00Z',
                active: true,
                status: 'active',
                credits_total: 0,
                credits_used: 0,
                credits_remaining: 0,
                credits_available: 479.211,
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        expect((await screen.findAllByText('newbie')).length).toBeGreaterThan(0);
        const totalText = screen.getByText('Total credits').parentElement?.textContent || '';
        const remainingText = screen.getByText('Remaining credits').parentElement?.textContent || '';
        expect(totalText).toContain('479.21');
        expect(remainingText).toContain('479.21');
        expect(totalText).not.toContain('479.211');
        expect(remainingText).not.toContain('479.211');
    });

    it('localizes known redeem errors in Chinese', async () => {
        RedeemHubLLMServiceMock.mockRejectedValue(new Error('redeem code already used'));
        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        const codeInput = await screen.findByLabelText('兑换码');
        fireEvent.change(codeInput, { target: { value: 'USED-CODE' } });
        fireEvent.click(screen.getByRole('button', { name: '立即兑换' }));

        expect(await screen.findByText('该兑换码已被使用。')).toBeTruthy();
        expect(screen.queryByText('redeem code already used')).toBeNull();
    });

    it('localizes redeem code format errors in Chinese', async () => {
        RedeemHubLLMServiceMock.mockRejectedValue(new Error('redeem code must be 24 letters or digits'));
        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        const codeInput = await screen.findByLabelText('兑换码');
        fireEvent.change(codeInput, { target: { value: 'SHORT' } });
        fireEvent.click(screen.getByRole('button', { name: '立即兑换' }));

        expect(await screen.findByText('兑换码必须是 24 位字母或数字。')).toBeTruthy();
        expect(screen.queryByText(/Error:/)).toBeNull();
    });

    it('localizes service load errors and inactive reasons in Chinese', async () => {
        GetHubLLMServiceStatusMock.mockRejectedValueOnce(new Error('hub access token is missing'));
        const { unmount } = render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);
        expect(await screen.findByText('Hub 访问令牌缺失，请重新连接 Hub 后重试。')).toBeTruthy();
        unmount();

        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            skip_llm_config: false,
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            inactive_reasons: ['grant credits are exhausted'],
        });
        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('- 授权额度已用尽。')).toBeTruthy();
        expect(screen.queryByText(/grant credits are exhausted/)).toBeNull();
    });

    it('relocates existing load errors on language change without refetching status', async () => {
        GetHubLLMServiceStatusMock.mockRejectedValue(new Error('hub access token is missing'));
        const { rerender } = render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('Hub access token is missing. Reconnect Hub and try again.')).toBeTruthy();
        expect(GetHubLLMServiceStatusMock).toHaveBeenCalledTimes(1);

        rerender(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('Hub 访问令牌缺失，请重新连接 Hub 后重试。')).toBeTruthy();
        expect(GetHubLLMServiceStatusMock).toHaveBeenCalledTimes(1);
    });

    it('localizes account status query errors in Chinese', async () => {
        GetHubLLMServiceStatusMock.mockRejectedValue(new Error('account status query failed: 401 Unauthorized: viewer token expired'));

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('Hub 授权已过期，请重新连接 Hub 后重试。')).toBeTruthy();
        expect(screen.queryByText(/viewer token expired/)).toBeNull();
        expect(screen.queryByText(/account status query failed/)).toBeNull();
    });

    it('falls back to service group IDs and available models when display names are absent', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_ids: ['newbie'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'newbie',
                source: 'card',
                expires_at: '2026-06-06T00:00:00Z',
                active: true,
                status: 'active',
                credits_available: 479.211,
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('Authorized Groups')).toBeTruthy();
        expect(screen.getByText('Authorized Groups').parentElement?.textContent).toContain('newbie');
        expect(screen.getByText('Authorized Models').parentElement?.textContent).toContain('auto');
        expect(screen.getByText('Authorized Models').parentElement?.textContent).toContain('newbie');
        expect(screen.queryByText('No model permissions yet')).toBeNull();
    });

    it('shows active free services as unlimited instead of zero credits', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_ids: ['free'],
            service_group_names: ['企业免费服务'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credits_total: 0,
            credits_used: 0,
            credits_remaining: 0,
            credits_available: 0,
            authorized_models: [{ name: 'auto', service_group_ids: ['free'] }],
        });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('企业免费服务')).toBeTruthy();
        expect(screen.getByText('有效期至').parentElement?.textContent).toContain('长期有效');
        expect(screen.getByText('服务权益').parentElement?.textContent).toContain('不限');
        expect(screen.queryByText('总额度')).toBeNull();
        expect(screen.queryByText('剩余额度')).toBeNull();
        expect(screen.getByText('免费服务无需授权额度明细。')).toBeTruthy();
        expect(screen.queryByText('暂无授权额度明细')).toBeNull();
    });

    it('shows a new-user limit card as recurring period allowance, not a 500-credit balance', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['系统免费服务组'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'system-free',
                source: 'new_user_limit_card',
                starts_at: '2026-08-23T09:00:00Z',
                expires_at: '0001-01-01T00:00:00Z',
                permanent: true,
                active: true,
                status: 'active',
                credits_total: 0,
                credits_used: 0,
                credits_remaining: 0,
                credits_available: 500,
                period_limits: { five_hour: 500, daily: 1000 },
                period_usage: {
                    five_hour: { credits_used: 0, rolling: true, window_end: '2026-08-23T14:00:00Z' },
                    daily: { credits_used: 0, window_end: '2026-08-24T00:00:00Z' },
                },
            }],
        });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        expect((await screen.findAllByText('新用户周期福利')).length).toBeGreaterThan(0);
        expect(screen.getByText('这是可循环使用的限额，不是账户总点数。')).toBeTruthy();
        expect(screen.getByText('5小时滚动限额')).toBeTruthy();
        expect(screen.getByText('今日累计限额')).toBeTruthy();
        expect(screen.getAllByText('长期有效').length).toBeGreaterThan(0);
        expect(screen.queryByText('总额度')).toBeNull();
        expect(screen.getByText('账户点数').closest('table')?.querySelector('.hub-service-redeem__cell--muted')?.textContent).toBe('不适用');
        expect(screen.getByText('5小时滚动限额').parentElement?.textContent).toContain('500剩余/上限500');
        expect(screen.getByRole('group', { name: '5小时滚动限额：剩余 500，上限 500，已用 0。' }).getAttribute('data-kind')).toBe('normal');
    });

    it('reloads and renders the latest tenant-defined benefit limits when Hub notifies a status change', async () => {
        GetHubLLMServiceStatusMock
            .mockResolvedValueOnce({
                active: true,
                skip_llm_config: true,
                service_group_names: ['系统免费服务组'],
                available_models: ['auto'],
                credit_grants: [{
                    service_group_id: 'system-free', source: 'new_user_limit_card', active: true, status: 'active', permanent: true,
                    period_limits: { five_hour: 500 },
                    period_usage: { five_hour: { credits_used: 100, rolling: true } },
                }],
            })
            .mockResolvedValueOnce({
                active: true,
                skip_llm_config: true,
                service_group_names: ['系统免费服务组'],
                available_models: ['auto'],
                credit_grants: [{
                    service_group_id: 'system-free', source: 'new_user_limit_card', active: true, status: 'active', permanent: true,
                    period_limits: { five_hour: 800, daily: 1200 },
                    period_usage: {
                        five_hour: { credits_used: 125, rolling: true },
                        daily: { credits_used: 200 },
                    },
                }],
            });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        await screen.findByText('5小时滚动限额');
        expect(screen.getByText('5小时滚动限额').parentElement?.textContent).toContain('400剩余/上限500');
        const handler = eventHandlers.get('hub-llm-service-changed');
        expect(handler).toBeTypeOf('function');

        handler?.();
        await new Promise((resolve) => window.setTimeout(resolve, 200));

        await waitFor(() => {
            expect(GetHubLLMServiceStatusMock).toHaveBeenCalledTimes(2);
            expect(screen.getByText('5小时滚动限额').parentElement?.textContent).toContain('675剩余/上限800');
            expect(screen.getByText('今日累计限额').parentElement?.textContent).toContain('1000剩余/上限1200');
        });
        expect(RefreshHubLLMServiceStatusMock).toHaveBeenCalledTimes(1);
    });

    it('does not let a delayed initial status overwrite a newer event reload', async () => {
        let resolveInitial: ((value: unknown) => void) | undefined;
        const initialStatus = new Promise<unknown>((resolve) => {
            resolveInitial = resolve;
        });
        GetHubLLMServiceStatusMock
            .mockReturnValueOnce(initialStatus)
            .mockResolvedValueOnce({
                active: true,
                skip_llm_config: true,
                service_group_names: ['系统免费服务组'],
                available_models: ['auto'],
                credit_grants: [{
                    service_group_id: 'system-free', source: 'new_user_limit_card', active: true, status: 'active', permanent: true,
                    period_limits: { five_hour: 800 },
                    period_usage: { five_hour: { credits_used: 125, rolling: true } },
                }],
            });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);
        const handler = eventHandlers.get('hub-llm-service-changed');
        expect(handler).toBeTypeOf('function');

        handler?.();
        await new Promise((resolve) => window.setTimeout(resolve, 200));
        await screen.findByText('5小时滚动限额');
        expect(screen.getByText('5小时滚动限额').parentElement?.textContent).toContain('675剩余/上限800');

        resolveInitial?.({
            active: true,
            skip_llm_config: true,
            service_group_names: ['系统免费服务组'],
            available_models: ['auto'],
            credit_grants: [{
                service_group_id: 'system-free', source: 'new_user_limit_card', active: true, status: 'active', permanent: true,
                period_limits: { five_hour: 500 },
                period_usage: { five_hour: { credits_used: 100, rolling: true } },
            }],
        });

        await waitFor(() => {
            expect(screen.getByText('5小时滚动限额').parentElement?.textContent).toContain('675剩余/上限800');
            expect(screen.queryByText('400剩余')).toBeNull();
        });
    });

    it('coalesces bursts of usage events into one trailing status reload', async () => {
        vi.useFakeTimers();
        try {
            render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);
            await vi.advanceTimersByTimeAsync(0);
            expect(GetHubLLMServiceStatusMock).toHaveBeenCalledTimes(1);

            const handler = eventHandlers.get('llm-token-usage-changed');
            expect(handler).toBeTypeOf('function');
            handler?.();
            handler?.();
            handler?.();

            await vi.advanceTimersByTimeAsync(149);
            expect(GetHubLLMServiceStatusMock).toHaveBeenCalledTimes(1);
            await vi.advanceTimersByTimeAsync(1);
            expect(GetHubLLMServiceStatusMock).toHaveBeenCalledTimes(2);
            await vi.advanceTimersByTimeAsync(2500);
            expect(GetHubLLMServiceStatusMock).toHaveBeenCalledTimes(3);
        } finally {
            vi.useRealTimers();
        }
    });

    it('uses the benefit grant validity instead of an unrelated card expiry', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['系统免费服务组'],
            default_model: 'auto',
            available_models: ['auto'],
            effective_expires_at: '2027-01-01T00:00:00Z',
            credit_grants: [{
                service_group_id: 'system-free', source: 'new_user_limit_card', active: true, status: 'active',
                starts_at: '2026-08-23T09:00:00Z', expires_at: '2026-09-01T00:00:00Z', permanent: false,
                period_limits: { five_hour: 500 },
                period_usage: { five_hour: { credits_used: 125, rolling: true, window_end: '2026-08-23T14:00:00Z' } },
            }, {
                service_group_id: 'paid', source: 'card', active: true, status: 'active',
                starts_at: '2026-08-23T09:00:00Z', expires_at: '2027-01-01T00:00:00Z',
                credits_total: 1000, credits_used: 0, credits_remaining: 1000,
            }],
        });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        const benefit = (await screen.findAllByText('新用户周期福利'))[0].closest('.hub-service-redeem__period-section');
        expect(benefit?.textContent).toContain('2026/09/01');
        expect(benefit?.textContent).not.toContain('2027/01/01');
        expect(screen.getByText('5小时滚动限额').parentElement?.textContent).toContain('375剩余/上限500');
    });

    it('keeps new-user period benefits separate for each service group', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['基础服务', '高级服务'],
            default_model: 'auto',
            available_models: ['auto'],
            credit_grants: [{
                service_group_id: 'basic', source: 'new_user_limit_card', active: true, status: 'active',
                expires_at: '2026-09-01T00:00:00Z',
                period_limits: { five_hour: 500, daily: 1000 },
                period_usage: {
                    five_hour: { credits_used: 125, rolling: true, window_end: '2026-08-23T14:00:00Z' },
                    daily: { credits_used: 200, window_end: '2026-08-24T00:00:00Z' },
                },
            }, {
                service_group_id: 'advanced', source: 'new_user_limit_card', active: true, status: 'active',
                expires_at: '2026-10-01T00:00:00Z',
                period_limits: { five_hour: 300, daily: 600 },
                period_usage: {
                    five_hour: { credits_used: 50, rolling: true, window_end: '2026-08-23T14:00:00Z' },
                    daily: { credits_used: 100, window_end: '2026-08-24T00:00:00Z' },
                },
            }],
        });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        await screen.findByText('每个服务组的限额彼此独立，以下数值不会相加。');
        const basic = document.querySelector('[data-service-group="basic"]');
        const advanced = document.querySelector('[data-service-group="advanced"]');
        expect(basic?.textContent).toContain('375剩余/上限500');
        expect(basic?.textContent).toContain('800剩余/上限1000');
        expect(advanced?.textContent).toContain('250剩余/上限300');
        expect(advanced?.textContent).toContain('500剩余/上限600');
        expect(basic?.textContent).not.toContain('625剩余/上限800');
        expect(advanced?.textContent).not.toContain('625剩余/上限800');
    });

    it('does not render NaN when a benefit response contains malformed numeric fields', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['系统免费服务组'],
            default_model: 'auto',
            available_models: ['auto'],
            credit_grants: [{
                service_group_id: 'system-free', source: 'new_user_limit_card', active: true, status: 'active', permanent: true,
                period_limits: { five_hour: '500', daily: 'bad' },
                period_usage: { five_hour: { credits_used: '-200', rolling: true } },
            }],
        });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        const benefit = (await screen.findAllByText('新用户周期福利'))[0].closest('.hub-service-redeem__period-section');
        expect(benefit?.textContent).toContain('500剩余/上限500');
        expect(benefit?.textContent).toContain('已用 0 · 0%');
        expect(benefit?.textContent).not.toContain('NaN');
        expect(benefit?.textContent).not.toContain('日限额');
    });

    it('keeps a validity-only benefit from rendering a misleading zero-credit wallet', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['系统免费服务组'],
            default_model: 'auto',
            available_models: ['auto'],
            credit_grants: [{
                service_group_id: 'system-free', source: 'new_user_limit_card', active: true, status: 'active', permanent: true,
                period_limits: { five_hour: 0, daily: 0 },
            }],
        });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('服务权益')).toBeTruthy();
        expect(screen.getByText('服务权益').parentElement?.textContent).toContain('不限');
        expect(screen.queryByText('账户点数')?.closest('.hub-service-redeem__wallet-section')).toBeNull();
        expect(screen.queryByText('新用户周期福利')?.closest('.hub-service-redeem__period-section')).toBeNull();
    });

    it('keeps a paid card balance separate from its period allowance', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['充值服务组'],
            default_model: 'auto',
            available_models: ['auto'],
            credit_grants: [{
                service_group_id: 'paid', source: 'card', active: true, status: 'active',
                starts_at: '2026-08-23T09:00:00Z', expires_at: '2026-09-01T00:00:00Z',
                credits_total: 1000, credits_used: 125, credits_remaining: 875,
                period_limits: { daily: 300 }, period_usage: { daily: { credits_used: 75, window_end: '2026-08-24T00:00:00Z' } },
            }],
        });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        const grantRow = (await screen.findByText('paid')).closest('tr');
        expect(grantRow?.textContent).toContain('1000 · 已用 125');
        expect(grantRow?.textContent).toContain('今日 剩余 225 / 上限 300');
        expect(grantRow?.textContent).not.toContain('剩余 875');
    });

    it('localizes grant source values in Chinese authorization details', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['充值服务组'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'redeem',
                source: 'card',
                starts_at: '2026-06-20T00:00:00Z',
                expires_at: '2027-06-20T00:00:00Z',
                active: true,
                status: 'active',
                credits_total: 10000,
                credits_used: 10,
                credits_remaining: 9990,
            }, {
                service_group_id: 'redeem',
                source: 'card',
                starts_at: '2026-06-20T00:00:00Z',
                expires_at: '2026-07-20T00:00:00Z',
                active: true,
                status: 'active',
                credits_total: 300,
                credits_used: 20,
                credits_remaining: 280,
                period_limits: { monthly: 300 },
            }, {
                service_group_id: 'redeem',
                source: 'card',
                starts_at: '2026-06-20T00:00:00Z',
                expires_at: '2027-06-20T00:00:00Z',
                active: true,
                status: 'active',
                credits_total: 3000,
                credits_used: 30,
                credits_remaining: 2970,
                period_limits: { monthly: 300 },
            }, {
                service_group_id: 'redeem',
                source: 'default_new_user_backfill',
                starts_at: '2026-06-18T00:00:00Z',
                expires_at: '2026-07-18T00:00:00Z',
                active: true,
                status: 'exhausted',
                credits_total: 1000,
                credits_used: 1000,
                credits_remaining: 0,
            }],
        });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        expect(await screen.findByText('授权额度明细')).toBeTruthy();
        expect(screen.getByText('充值卡（点卡）')).toBeTruthy();
        expect(screen.getByText('充值卡（包月卡）')).toBeTruthy();
        expect(screen.getByText('充值卡（包年卡）')).toBeTruthy();
        expect(screen.getByText('新用户赠送')).toBeTruthy();
        expect(screen.queryByText('default_new_user_backfill')).toBeNull();
    });

    it('keeps Total ≈ Used + Remaining including queued point-card balances', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credits_remaining: 4900,
            credits_available: 10000,
            credit_grants: [{
                service_group_id: 'coding-monthly',
                source: 'card',
                expires_at: '2026-05-31T00:00:00Z',
                active: false,
                status: 'period_limited',
                credits_total: 5000,
                credits_used: 100,
                credits_remaining: 4900,
            }, {
                service_group_id: 'coding-point-card',
                source: 'card',
                expires_at: '2027-05-31T00:00:00Z',
                active: false,
                status: 'queued',
                credits_total: 10000,
                credits_used: 0,
                credits_remaining: 10000,
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        await screen.findByText('coding-monthly');
        // Lifetime remaining: 4900 (period-limited left) + 10000 (queued) = 14900.
        expect(screen.getByText('Remaining credits').parentElement?.textContent).toContain('14900');
        expect(screen.getByText('Total credits').parentElement?.textContent).toContain('15000');
    });

    it('includes queued future grants in the displayed total credits', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['充值服务组'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credits_total: 55000,
            credits_used: 5672.116,
            credits_available: 49148.916,
            credit_grants: [{
                service_group_id: 'newbie',
                source: 'card',
                starts_at: '2026-05-07T06:44:00Z',
                expires_at: '2026-06-06T06:44:00Z',
                active: true,
                status: 'active',
                credits_total: 5000,
                credits_used: 4607.093,
                credits_remaining: 392.907,
            }, {
                service_group_id: 'newbie',
                source: 'card',
                starts_at: '2026-06-02T09:36:00Z',
                expires_at: '2026-08-01T09:36:00Z',
                active: true,
                status: 'active',
                credits_total: 50000,
                credits_used: 1065.023,
                credits_remaining: 48934.977,
            }, {
                service_group_id: 'newbie',
                source: 'card',
                starts_at: '2026-08-05T06:44:00Z',
                expires_at: '2026-08-06T06:44:00Z',
                active: false,
                status: 'queued',
                credits_total: 1,
                credits_used: 0,
                credits_remaining: 1,
            }, {
                service_group_id: 'newbie',
                source: 'card',
                starts_at: '2026-08-06T06:44:00Z',
                expires_at: '2026-08-07T06:44:00Z',
                active: false,
                status: 'queued',
                credits_total: 300,
                credits_used: 0,
                credits_remaining: 300,
            }],
        });

        render(<HubServiceRedeemPanel lang="zh-Hans" onStatusChange={vi.fn()} />);

        expect((await screen.findAllByText('充值服务组')).length).toBeGreaterThan(0);
        expect(screen.getByText('总额度').parentElement?.textContent).toContain('55301');
        const usedText = screen.getByText('已用额度').parentElement?.textContent || '';
        const remainingText = screen.getByText('剩余额度').parentElement?.textContent || '';
        expect(usedText).toContain('5672.12');
        // Lifetime remaining includes queued (1 + 300) so Total ≈ Used + Remaining.
        expect(remainingText).toContain('49628.88');
        expect(usedText).not.toContain('5672.116');
        expect(remainingText).not.toContain('49148.916');
    });

    it('falls back to the longest grant expiry when status has no effective expiry', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'coding-basic',
                source: 'card',
                expires_at: '2026-05-06T00:00:00Z',
                active: true,
                status: 'active',
            }, {
                service_group_id: 'coding-next',
                source: 'card',
                expires_at: '2026-06-06T00:00:00Z',
                active: false,
                status: 'queued',
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        await screen.findByText('coding-basic');
        expect(screen.getAllByText(/06\/06\/2026/).length).toBeGreaterThan(0);
    });

    it('ignores invalid grant expiry values when choosing the fallback service expiry', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'coding-invalid',
                source: 'card',
                expires_at: 'not-a-date',
                active: true,
                status: 'active',
            }, {
                service_group_id: 'coding-valid',
                source: 'card',
                expires_at: '2026-06-06T00:00:00Z',
                active: false,
                status: 'queued',
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        await screen.findByText('coding-invalid');
        expect(screen.getAllByText(/06\/06\/2026/).length).toBeGreaterThan(0);
    });

    it('guards malformed numeric fields from leaking NaN into credit totals', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credits_total: 'bad',
            credits_used: 'bad',
            credits_remaining: 'bad',
            credits_available: '25',
            credit_grants: [{
                service_group_id: 'coding-basic',
                source: 'card',
                expires_at: '2026-06-06T00:00:00Z',
                active: true,
                status: 'active',
                credits_total: 'bad',
                credits_remaining: 'bad',
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        await screen.findByText('coding-basic');
        expect(screen.getByText('Total credits').parentElement?.textContent).toContain('25');
        expect(screen.getByText('Used credits').parentElement?.textContent).toContain('0');
        expect(screen.getByText('Remaining credits').parentElement?.textContent).toContain('25');
    });

    it('shows expired grant expiry while reporting no currently available credits', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: false,
            skip_llm_config: false,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credits_available: 0,
            credit_grants: [{
                service_group_id: 'coding-expired',
                source: 'card',
                starts_at: '2026-05-01T00:00:00Z',
                expires_at: '2026-05-05T12:13:17Z',
                active: false,
                status: 'expired',
                credits_total: 100,
                credits_used: 10,
                credits_remaining: 90,
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        expect((await screen.findAllByText('Expired')).length).toBeGreaterThan(0);
        expect(screen.getByText(/Official authorization has expired/i)).toBeTruthy();
        expect(screen.getAllByText(/05\/05\/2026/).length).toBeGreaterThan(0);
        expect(screen.getByText('Remaining credits').parentElement?.textContent).toContain('0');
    });

    it('keeps redemption layout compact and the code field accessible', async () => {
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            authorized_models: [{ name: 'auto', service_group_ids: ['LLM'] }],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
            credit_grants: [{
                service_group_id: 'coding-basic',
                source: 'card',
                expires_at: '2026-05-06T00:00:00Z',
                active: true,
                status: 'active',
            }],
        });

        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        const codeInput = await screen.findByLabelText('Redeem Code');
        expect(codeInput.getAttribute('id')).toBe('hub-service-redeem-code');
        expect(codeInput.getAttribute('autocomplete')).toBe('off');
        expect(codeInput.getAttribute('spellcheck')).toBe('false');
        expect(screen.getByText('Authorization Breakdown')).toBeTruthy();
        expect(screen.getAllByRole('columnheader').every((header) => header.getAttribute('scope') === 'col')).toBe(true);
        expect(screen.queryByText('Current Authorization Details')).toBeNull();
        expect(screen.queryByText('Service Status')).toBeNull();
    });

    it('opens card store and credits page from service status actions before refresh', async () => {
        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        const buyCredits = await screen.findByRole('button', { name: 'Buy Credits' });
        const viewCredits = screen.getByRole('button', { name: 'View Credits' });
        const refresh = screen.getByRole('button', { name: 'Refresh' });
        expect(buyCredits.getAttribute('title')).toContain('Open card store');
        expect(viewCredits.getAttribute('title')).toContain('view balance');
        expect(buyCredits.querySelector('.hub-service-redeem__buy-icon')).toBeTruthy();
        expect(buyCredits.compareDocumentPosition(viewCredits) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(viewCredits.compareDocumentPosition(refresh) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

        fireEvent.click(buyCredits);
        await waitFor(() => {
            expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://hub.example.com/card_store?tenant_id=tenant%20acme&account=dev%40example.com&email=dev%40example.com#token=viewer%20token');
        });

        BrowserOpenURLMock.mockClear();
        fireEvent.click(viewCredits);

        await waitFor(() => {
            expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://hub.example.com/get-credits?tenant_id=tenant%20acme&account=dev%40example.com&email=dev%40example.com#token=viewer%20token');
        });
    });

    it('opens card store with viewer token fallback when configured email is unavailable', async () => {
        LoadConfigMock.mockResolvedValue({ remote_hub_url: 'https://hub.example.com/', remote_viewer_token: 'viewer token', remote_tenant_id: 'tenant acme' });
        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        const buyCredits = await screen.findByRole('button', { name: 'Buy Credits' });
        fireEvent.click(buyCredits);

        await waitFor(() => {
            expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://hub.example.com/card_store?tenant_id=tenant%20acme#token=viewer%20token');
        });
    });

    it('prefers Hub card_store over HubCenter compute-store even when hub_id is configured', async () => {
        LoadConfigMock.mockResolvedValue({
            remote_hub_id: 'hub_1',
            remote_hub_url: 'https://hub.example.com/',
            remote_hubcenter_url: 'https://hubs.example.com/',
            remote_viewer_token: 'viewer token',
            remote_tenant_id: 'tenant acme',
            remote_email: 'dev@example.com',
        });
        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        const buyCredits = await screen.findByRole('button', { name: 'Buy Credits' });
        fireEvent.click(buyCredits);

        await waitFor(() => {
            expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://hub.example.com/card_store?tenant_id=tenant%20acme&account=dev%40example.com&email=dev%40example.com#token=viewer%20token');
        });
    });

    it('opens card store with stable user ID and mobile for phone-registered accounts', async () => {
        LoadConfigMock.mockResolvedValue({
            remote_hub_url: 'https://hub.example.com/',
            remote_viewer_token: 'viewer token',
            remote_tenant_id: 'tenant acme',
            remote_email: 'phone:19900001111',
            remote_user_id: 'usr_phone',
            remote_mobile: '19900001111',
        });
        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        const buyCredits = await screen.findByRole('button', { name: 'Buy Credits' });
        fireEvent.click(buyCredits);

        await waitFor(() => {
            // Prefer phone/email as account identity; keep user_id as a separate stable key.
            expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://hub.example.com/card_store?tenant_id=tenant%20acme&account=phone%3A19900001111&user_id=usr_phone&email=phone%3A19900001111&mobile=19900001111#token=viewer%20token');
        });
    });

    it('opens the credits center even when viewer token is unavailable', async () => {
        LoadConfigMock.mockResolvedValue({ remote_hub_url: 'https://hub.example.com/', remote_tenant_id: 'tenant acme' });
        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        const viewCredits = await screen.findByRole('button', { name: 'View Credits' });
        fireEvent.click(viewCredits);

        await waitFor(() => {
            expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://hub.example.com/get-credits?tenant_id=tenant%20acme');
        });
    });
});
