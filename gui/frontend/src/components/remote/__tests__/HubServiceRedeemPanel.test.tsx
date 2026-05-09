// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';

const GetHubLLMServiceStatusMock = vi.fn();
const RedeemHubLLMServiceMock = vi.fn();
const LoadConfigMock = vi.fn();
const BrowserOpenURLMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetHubLLMServiceStatus: (...args: unknown[]) => GetHubLLMServiceStatusMock(...args),
    RedeemHubLLMService: (...args: unknown[]) => RedeemHubLLMServiceMock(...args),
    LoadConfig: (...args: unknown[]) => LoadConfigMock(...args),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    BrowserOpenURL: (...args: unknown[]) => BrowserOpenURLMock(...args),
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showAlert: vi.fn(),
        showConfirm: vi.fn(),
    }),
}));

import { HubServiceRedeemPanel } from '../HubServiceRedeemPanel';

describe('HubServiceRedeemPanel', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        GetHubLLMServiceStatusMock.mockResolvedValue({
            active: true,
            skip_llm_config: true,
            service_group_names: ['LLM'],
            default_model: 'auto',
            available_models: ['auto'],
            hub_llm_base_url: 'https://hub.example.com/api/llm/v1',
        });
        LoadConfigMock.mockResolvedValue({ remote_hub_url: 'https://hub.example.com/', remote_viewer_token: 'viewer token' });
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
        expect(screen.getAllByText(/05\/05\/2026/).length).toBeGreaterThan(1);
        expect(screen.getByText('Remaining credits').parentElement?.textContent).toContain('0');
    });

    it('opens credits page from service status actions before refresh', async () => {
        render(<HubServiceRedeemPanel lang="en" onStatusChange={vi.fn()} />);

        const viewCredits = await screen.findByRole('button', { name: 'View Credits' });
        const refresh = screen.getByRole('button', { name: 'Refresh' });
        expect(viewCredits.compareDocumentPosition(refresh) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

        fireEvent.click(viewCredits);

        await waitFor(() => {
            expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://hub.example.com/get-credits#token=viewer%20token');
        });
    });
});
