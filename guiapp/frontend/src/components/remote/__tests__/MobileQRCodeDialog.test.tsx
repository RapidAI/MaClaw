// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup } from '@testing-library/react';

const CreateMobileLLMDesktopQRSessionMock = vi.fn();
const FetchProviderModelsMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CreateMobileLLMDesktopQRSession: (...args: unknown[]) => CreateMobileLLMDesktopQRSessionMock(...args),
    FetchProviderModels: (...args: unknown[]) => FetchProviderModelsMock(...args),
}));

import { MobileQRCodeDialog } from '../MobileQRCodeDialog';

describe('MobileQRCodeDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        CreateMobileLLMDesktopQRSessionMock.mockResolvedValue({
            status: 'created',
            session_id: 'mlqr_test',
            expires_at: '2026-07-02T12:00:00Z',
            qr_payload: '{"v":2,"type":"maclaw_mobile_llm_authorization","session_id":"mlqr_test","hub_url":"https://tenant-a.maclaw.top"}',
        });
    });

    afterEach(() => {
        cleanup();
    });

    it('creates a one-time Hub QR session instead of embedding the API key', async () => {
        render(
            <MobileQRCodeDialog
                open
                onClose={vi.fn()}
                currentName="Custom1"
                providers={[
                    {
                        name: 'Custom1',
                        url: 'https://llm.example.com/v1',
                        key: 'sk-secret',
                        model: 'gpt-4.1-mini',
                        protocol: 'openai',
                        is_custom: true,
                        supports_vision: false,
                    },
                ]}
                lang="en"
            />,
        );

        await waitFor(() => {
            expect(CreateMobileLLMDesktopQRSessionMock).toHaveBeenCalledWith(
                'Custom1',
                'https://llm.example.com/v1',
                'sk-secret',
                'gpt-4.1-mini',
                ['gpt-4.1-mini'],
                'openai',
            );
        });
        expect(screen.getByText(/one-time Hub authorization session/)).toBeTruthy();
        expect(screen.queryByText(/contains your API Key in plaintext/)).toBeNull();
        await expect(CreateMobileLLMDesktopQRSessionMock.mock.results[0].value).resolves.not.toMatchObject({
            qr_payload: expect.stringContaining('sk-secret'),
        });
    });
});
