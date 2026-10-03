import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const listSharesMock = vi.fn();
const listModelsMock = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    TokenBankListShares: (...args: unknown[]) => listSharesMock(...args),
    TokenBankListShareModels: (...args: unknown[]) => listModelsMock(...args),
    TokenBankCreateShare: vi.fn(),
    TokenBankListShareAudiences: vi.fn().mockResolvedValue({ hubs: [], tenants: [] }),
}));

const { showAlertMock, showConfirmMock } = vi.hoisted(() => ({
    showAlertMock: vi.fn().mockResolvedValue(undefined),
    showConfirmMock: vi.fn().mockResolvedValue(true),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showAlert: showAlertMock,
        showConfirm: showConfirmMock,
        showPrompt: vi.fn(),
    }),
}));

import { LLMConfigProviderShareHeading, TokenBankShareChipButton } from '../TokenBankShareChipButton';

describe('TokenBankShareChipButton new models', () => {
    beforeEach(() => {
        listSharesMock.mockReset();
        listModelsMock.mockReset();
        showConfirmMock.mockReset();
        showConfirmMock.mockResolvedValue(true);
        showAlertMock.mockReset();
    });

    it('shows a banner when the probe finds models this provider has not shared', async () => {
        listSharesMock.mockResolvedValue({
            shares: [{ id: 'sh_1', api_url: 'https://api.x/v1/', status: 'active' }],
        });
        listModelsMock.mockResolvedValue({ models: [{ model_name: 'model-a' }] });
        const probe = vi.fn().mockResolvedValue({ ok: true, latencyMs: 12 });
        render(
            <TokenBankShareChipButton
                lang="zh-Hans"
                providerName="OpenAI"
                apiURL="https://api.x/v1"
                apiKey="sk-secret"
                protocol="openai"
                listModels={async () => ['model-a', 'model-b']}
                probeModel={probe}
            />,
        );
        const chipIcon = screen.getByRole('button', { name: '分享此服务商到 Token 银行' }).querySelector('[data-icon="token-bank-deposit"]');
        expect(chipIcon?.getAttribute('width')).toBe('14');
        expect(chipIcon?.querySelector('[data-part="token"]')).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '分享此服务商到 Token 银行' }));
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        const confirmText = String(showConfirmMock.mock.calls[0][0]);
        expect(confirmText).toContain('不能购买算力卡');
        expect(confirmText).toContain('不可提现');
        expect(confirmText).not.toContain('算力卡、服务组');
        await waitFor(() => expect(screen.getByText('开始探测')).toBeTruthy());
        fireEvent.click(screen.getByText('开始探测'));
        await waitFor(() => expect(screen.getByText(/发现 1 个新模型/)).toBeTruthy());
        expect(listModelsMock).toHaveBeenCalledWith('sh_1', 'all');
    });

    it('opens the dialog without a new-model banner when the share list fails', async () => {
        listSharesMock.mockRejectedValue(new Error('offline'));
        const probe = vi.fn().mockResolvedValue({ ok: true, latencyMs: 12 });
        render(
            <TokenBankShareChipButton
                lang="zh-Hans"
                providerName="OpenAI"
                apiURL="https://api.x/v1"
                apiKey="sk-secret"
                protocol="openai"
                listModels={async () => ['model-a']}
                probeModel={probe}
            />,
        );
        fireEvent.click(screen.getByRole('button', { name: '分享此服务商到 Token 银行' }));
        await waitFor(() => expect(screen.getByText('开始探测')).toBeTruthy());
        fireEvent.click(screen.getByText('开始探测'));
        await waitFor(() => expect(screen.getByText('1/1')).toBeTruthy());
        expect(screen.queryByText(/新模型/)).toBeNull();
    });

    it('leaves focus on another control when the share dialog closes', async () => {
        listSharesMock.mockResolvedValue({ shares: [] });
        render(
            <>
                <button type="button">stay</button>
                <TokenBankShareChipButton
                    appearance="deposit"
                    lang="zh-Hans"
                    providerName="OpenAI"
                    apiURL="https://api.x/v1"
                    apiKey="sk-secret"
                    protocol="openai"
                    listModels={async () => ['model-a']}
                    probeModel={async () => ({ ok: true })}
                />
            </>,
        );
        fireEvent.click(screen.getByRole('button', { name: '将OpenAI存入银行' }));
        const cancel = await screen.findByRole('button', { name: '取消' });
        const stay = screen.getByRole('button', { name: 'stay' });
        cancel.addEventListener('click', () => stay.focus(), true);
        fireEvent.click(cancel);
        await act(async () => {
            await new Promise((resolve) => setTimeout(resolve, 0));
        });
        expect(screen.queryByRole('dialog', { name: '分享服务商' })).toBeNull();
        expect(document.activeElement).toBe(stay);
    });

    it('shows a token-into-bank icon and label on the config-title badge', async () => {
        listSharesMock.mockResolvedValue({ shares: [] });
        render(
            <TokenBankShareChipButton
                appearance="badge"
                lang="zh-Hans"
                providerName="Kimi Code"
                apiURL="https://api.kimi.com/coding/v1"
                apiKey="sk-secret"
                protocol="openai"
                listModels={async () => ['kimi-for-coding']}
                probeModel={async () => ({ ok: true })}
            />,
        );
        const button = screen.getByRole('button', { name: '将此服务商存入 Token 银行' });
        expect(button.textContent).toContain('Token 银行');
        const icon = button.querySelector('[data-icon="token-bank-deposit"]');
        expect(icon?.getAttribute('width')).toBe('16');
        expect(icon?.querySelector('[data-part="token"] circle')).toBeTruthy();
        expect(icon?.querySelector('[data-part="arrow"]')).toBeTruthy();
        expect(icon?.querySelector('[data-part="bank"]')).toBeTruthy();
        expect(icon?.innerHTML ?? '').not.toContain('--theme-surface');
        showConfirmMock.mockResolvedValueOnce(false);
        fireEvent.click(button);
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
    });
});

describe('LLMConfigProviderShareHeading', () => {
    it('puts the deposit badge on the title row only after a passed connection test', () => {
        const provider = {
            name: 'Kimi Code',
            url: 'https://api.kimi.com/coding/v1',
            key: 'oauth',
            model: 'kimi-for-coding',
            protocol: 'openai',
            connection_test_passed: true,
        };
        const { rerender } = render(
            <LLMConfigProviderShareHeading
                lang="zh-Hans"
                provider={provider}
                listModels={async () => []}
                probeModel={async () => ({ ok: true })}
            />,
        );
        const title = screen.getByText('Kimi Code 配置');
        expect(title.parentElement?.classList.contains('llm-config-form-card__heading')).toBe(true);
        expect(title.parentElement?.querySelector('[data-icon="token-bank-deposit"]')).toBeTruthy();

        rerender(
            <LLMConfigProviderShareHeading
                lang="zh-Hans"
                provider={{ ...provider, connection_test_passed: false }}
                listModels={async () => []}
                probeModel={async () => ({ ok: true })}
            />,
        );
        expect(screen.queryByRole('button', { name: '将此服务商存入 Token 银行' })).toBeNull();

        rerender(
            <LLMConfigProviderShareHeading
                lang="zh-Hans"
                provider={{ ...provider, name: 'MaClaw官方' }}
                listModels={async () => []}
                probeModel={async () => ({ ok: true })}
            />,
        );
        expect(screen.queryByRole('button', { name: '将此服务商存入 Token 银行' })).toBeNull();

        rerender(
            <LLMConfigProviderShareHeading
                lang="zh-Hans"
                provider={{ ...provider, name: 'DeepSeek', is_hub_service: true, connection_test_passed: true }}
                listModels={async () => []}
                probeModel={async () => ({ ok: true })}
            />,
        );
        expect(screen.queryByRole('button', { name: '将此服务商存入 Token 银行' })).toBeNull();
        expect(screen.getByText('DeepSeek 配置')).toBeTruthy();
    });
});
