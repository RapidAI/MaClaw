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

    it('shows currently available credits instead of blocked remaining credits while service is active', async () => {
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
        expect(screen.getByText('Remaining credits').parentElement?.textContent).toContain('10000');
        expect(screen.getByText('Total credits').parentElement?.textContent).toContain('10100');
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
            expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://hub.example.com/card_store?tenant_id=tenant%20acme&email=dev%40example.com#token=viewer%20token');
        });

        BrowserOpenURLMock.mockClear();
        fireEvent.click(viewCredits);

        await waitFor(() => {
            expect(BrowserOpenURLMock).toHaveBeenCalledWith('https://hub.example.com/get-credits?tenant_id=tenant%20acme&email=dev%40example.com#token=viewer%20token');
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
