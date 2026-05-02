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
