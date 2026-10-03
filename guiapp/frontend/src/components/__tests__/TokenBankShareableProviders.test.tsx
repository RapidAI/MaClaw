import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const listProvidersMock = vi.fn();
const listSharesMock = vi.fn();
const listModelsMock = vi.fn();
const fetchModelsMock = vi.fn();

vi.mock('../../../wailsjs/go/main/App', () => ({
    GetMaclawLLMProviders: (...args: unknown[]) => listProvidersMock(...args),
    TokenBankListShares: (...args: unknown[]) => listSharesMock(...args),
    TokenBankListShareModels: (...args: unknown[]) => listModelsMock(...args),
    TokenBankCreateShare: vi.fn(),
    TokenBankListShareAudiences: vi.fn().mockResolvedValue({ hubs: [], tenants: [] }),
    FetchProviderModels: (...args: unknown[]) => fetchModelsMock(...args),
}));

const { showAlertMock, showConfirmMock } = vi.hoisted(() => ({
    showAlertMock: vi.fn().mockResolvedValue(undefined),
    showConfirmMock: vi.fn().mockResolvedValue(true),
}));

vi.mock('../CustomDialog', () => ({
    useDialog: () => ({
        showAlert: showAlertMock,
        showConfirm: showConfirmMock,
        showPrompt: vi.fn(),
    }),
}));

import { controlWidthCap, fitMenuWidth, placeProviderMenu, TokenBankShareableProviders } from '../TokenBankShareableProviders';

async function openProviderMenu() {
    const picker = await screen.findByRole('combobox', { name: /要存入的服务商/ });
    if (!screen.queryByRole('listbox')) fireEvent.click(picker);
    await screen.findByRole('listbox');
    return picker;
}

const kimi = {
    id: 'kimi',
    name: 'Kimi Code',
    url: 'https://api.kimi.com/coding/v1',
    key: 'sk-kimi',
    model: 'kimi-for-coding',
    protocol: 'openai',
    connection_test_passed: true,
};

describe('TokenBankShareableProviders', () => {
    beforeEach(() => {
        listProvidersMock.mockReset();
        listSharesMock.mockReset();
        listModelsMock.mockReset();
        fetchModelsMock.mockReset();
        showConfirmMock.mockReset();
        showAlertMock.mockReset();
        showConfirmMock.mockResolvedValue(true);
        listSharesMock.mockResolvedValue({ shares: [] });
        listModelsMock.mockResolvedValue({ models: [] });
        fetchModelsMock.mockResolvedValue([{ id: 'kimi-for-coding', name: 'Kimi' }]);
    });

    it('lists only tested non-hub providers in one dropdown', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [
                kimi,
                { ...kimi, id: 'plain', name: 'My Relay', connection_test_passed: true },
                { ...kimi, id: 'idle', name: 'DeepSeek', connection_test_passed: false },
                { ...kimi, id: 'nokey', name: 'Bare', key: '  ', connection_test_passed: true },
                { ...kimi, id: 'hub-name', name: 'MaClaw官方', connection_test_passed: true },
                { ...kimi, id: 'hub-flag', name: 'OpenAI', connection_test_passed: true, is_hub_service: true },
            ],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await openProviderMenu();
        expect(screen.getAllByRole('option').map((option) => option.querySelector('.tbk-provider-pick__option-label')?.textContent)).toEqual(['Kimi Code', 'My Relay']);
        expect(screen.getByRole('option', { name: 'Kimi Code' }).querySelector('.tbk-provider-pick__mark svg')).toBeTruthy();
        expect(screen.getByRole('option', { name: 'My Relay' }).querySelector('.tbk-provider-pick__monogram')?.textContent).toBe('M');
        expect(screen.queryByRole('option', { name: 'DeepSeek' })).toBeNull();
        expect(screen.queryByRole('option', { name: 'Bare' })).toBeNull();
        expect(screen.queryByRole('option', { name: 'MaClaw官方' })).toBeNull();
        expect(screen.queryByRole('option', { name: 'OpenAI' })).toBeNull();
        expect(screen.queryByRole('list')).toBeNull();
        const face = picker.parentElement?.querySelector('.tbk-provider-pick__face');
        expect(face?.querySelector('.tbk-provider-pick__mark svg')).toBeTruthy();
        expect(face?.querySelector('.tbk-provider-pick__label')?.textContent).toBe('Kimi Code');

        fireEvent.click(screen.getByRole('option', { name: 'My Relay' }));
        expect(face?.querySelector('.tbk-provider-pick__label')?.textContent).toBe('My Relay');
        const mark = picker.closest('.tbk-provider-pick')?.querySelector('.tbk-provider-pick__mark');
        expect(mark?.className).toContain('tbk-provider-pick__mark--fallback');
        expect(mark?.querySelector('.tbk-provider-pick__monogram')?.textContent).toBe('M');
        expect(mark?.querySelector('svg')).toBeNull();
        expect(screen.getAllByText('存入银行')).toHaveLength(1);
        expect(picker.getAttribute('title')).toBe('https://api.kimi.com/coding/v1');
    });

    it('labels providers that share a name with their endpoint', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [
                { ...kimi, id: 'relay-a', name: 'My Relay', url: 'https://a.example/v1' },
                { ...kimi, id: 'relay-b', name: 'My Relay', url: 'https://b.example/coding/v1/' },
            ],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await openProviderMenu();
        expect(screen.getByRole('option', { name: 'My Relay · a.example/v1' })).toBeTruthy();
        expect(screen.getByRole('option', { name: 'My Relay · b.example/coding/v1' })).toBeTruthy();
        expect(picker.parentElement?.querySelector('.tbk-provider-pick__label')?.textContent).toBe('My Relay · a.example/v1');
        expect(picker.getAttribute('title')).toBe('https://a.example/v1');
        fireEvent.click(screen.getByRole('option', { name: 'My Relay · b.example/coding/v1' }));
        expect(screen.getByRole('combobox', { name: /要存入的服务商/ }).parentElement?.querySelector('.tbk-provider-pick__label')?.textContent).toBe('My Relay · b.example/coding/v1');
        expect(screen.getByRole('combobox', { name: /要存入的服务商/ }).getAttribute('title')).toBe('https://b.example/coding/v1/');
    });

    it('tells same-name providers apart when the host and path match', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [
                { ...kimi, id: 'http', name: 'My Relay', url: 'http://a.example/v1' },
                { ...kimi, id: 'https', name: 'My Relay', url: 'https://a.example/v1' },
                { ...kimi, id: 'secret-a', name: 'My Relay', url: 'https://user:one@b.example/v1' },
                { ...kimi, id: 'secret-b', name: 'My Relay', url: 'https://user:two@b.example/v1' },
            ],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await openProviderMenu();
        expect(screen.getByRole('option', { name: 'My Relay · http://a.example/v1' })).toBeTruthy();
        expect(screen.getByRole('option', { name: 'My Relay · https://a.example/v1' })).toBeTruthy();
        expect(screen.getByRole('option', { name: 'My Relay · https://b.example/v1 · 1' })).toBeTruthy();
        expect(screen.getByRole('option', { name: 'My Relay · https://b.example/v1 · 2' })).toBeTruthy();
        const text = screen.getAllByRole('option').map((option) => option.textContent).join('\n');
        expect(text).not.toContain('one');
        expect(text).not.toContain('two');
    });

    it('keeps a non-secret query in the option label and hides a key query', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [
                { ...kimi, id: 'q1', name: 'My Relay', url: 'https://a.example/v1?x=1' },
                { ...kimi, id: 'q2', name: 'My Relay', url: 'https://a.example/v1?x=2' },
                { ...kimi, id: 'k1', name: 'Keyed', url: 'https://b.example/v1?api_key=sk-a&x=1' },
                { ...kimi, id: 'k2', name: 'Keyed', url: 'https://b.example/v1?api_key=sk-b&x=1' },
            ],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await openProviderMenu();
        expect(screen.getByRole('option', { name: 'My Relay · https://a.example/v1?x=1' })).toBeTruthy();
        expect(screen.getByRole('option', { name: 'My Relay · https://a.example/v1?x=2' })).toBeTruthy();
        expect(screen.getByRole('option', { name: 'Keyed · https://b.example/v1?x=1 · 1' })).toBeTruthy();
        expect(screen.getByRole('option', { name: 'Keyed · https://b.example/v1?x=1 · 2' })).toBeTruthy();
        const text = screen.getAllByRole('option').map((option) => option.textContent).join('\n');
        expect(text).not.toContain('sk-a');
        expect(text).not.toContain('sk-b');
    });

    it('uses a brand mark only when the name starts with that brand', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [
                { ...kimi, id: 'alias', name: 'Kimi (月之暗面)' },
                { ...kimi, id: 'not', name: 'NotKimi' },
                { ...kimi, id: 'blank-url', name: 'DeepSeek', url: '  ' },
            ],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await openProviderMenu();
        expect(picker.closest('.tbk-provider-pick')?.querySelector('svg')).toBeTruthy();
        expect(screen.getByRole('option', { name: 'Kimi (月之暗面)' }).querySelector('svg')).toBeTruthy();
        expect(screen.getByRole('option', { name: 'NotKimi' }).querySelector('svg')).toBeNull();
        expect(screen.queryByRole('option', { name: 'DeepSeek' })).toBeNull();
        fireEvent.click(screen.getByRole('option', { name: 'NotKimi' }));
        const mark = picker.closest('.tbk-provider-pick')?.querySelector('.tbk-provider-pick__mark');
        expect(mark?.querySelector('svg')).toBeNull();
        expect(mark?.querySelector('.tbk-provider-pick__monogram')?.textContent).toBe('N');
    });

    it('deposits an OAuth provider with its access token, and hides Copilot when only the GitHub token is saved', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [
                {
                    ...kimi,
                    id: 'grok',
                    name: 'xAI-Grok',
                    url: 'https://api.x.ai/v1',
                    key: '  ',
                    oauth_access_token: 'oauth-grok-token',
                },
                {
                    ...kimi,
                    id: 'copilot',
                    name: 'GitHub Copilot',
                    url: 'https://api.githubcopilot.com',
                    key: '',
                    oauth_access_token: 'github-long-token',
                },
            ],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await openProviderMenu();
        expect(screen.getByRole('option', { name: 'xAI-Grok' }).querySelector('svg')).toBeTruthy();
        expect(screen.queryByRole('option', { name: 'GitHub Copilot' })).toBeNull();
        fireEvent.click(screen.getByRole('button', { name: '将xAI-Grok存入银行' }));
        await waitFor(() => expect(fetchModelsMock).toHaveBeenCalledWith(
            'https://api.x.ai/v1',
            'oauth-grok-token',
            'openai',
            expect.any(String),
        ));
        expect(screen.getByRole('dialog', { name: '分享服务商' })).toBeTruthy();
        expect(fetchModelsMock).toHaveBeenCalledTimes(1);
    });

    it('ignores a second click while the deposit confirm is still open', async () => {
        listProvidersMock.mockResolvedValue({ providers: [kimi] });
        let resolveConfirm: (value: boolean) => void = () => {};
        showConfirmMock.mockImplementation(() => new Promise<boolean>((resolve) => {
            resolveConfirm = resolve;
        }));
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const button = await screen.findByRole('button', { name: '将Kimi Code存入银行' });
        fireEvent.click(button);
        fireEvent.click(button);
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalledTimes(1));
        expect(button.textContent).toBe('请稍候');
        expect(button.getAttribute('aria-label')).toBe('将Kimi Code存入银行');
        resolveConfirm(false);
        await waitFor(() => expect(button.hasAttribute('disabled')).toBe(false));
        expect(screen.queryByRole('dialog')).toBeNull();
    });

    it('keeps the list when a later refresh fails, and does not call an empty list a missing provider', async () => {
        listProvidersMock.mockResolvedValue({ providers: [kimi] });
        const { rerender } = render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await openProviderMenu();
        expect(screen.getByRole('option', { name: 'Kimi Code' })).toBeTruthy();
        listProvidersMock.mockRejectedValue(new Error('providers offline'));
        rerender(<TokenBankShareableProviders lang="zh-Hans" reloadToken={2} />);
        await waitFor(() => expect(screen.getByText('providers offline')).toBeTruthy());
        await openProviderMenu();
        expect(screen.getByRole('option', { name: 'Kimi Code' })).toBeTruthy();
        expect(screen.queryByText(/还没有测试可用的服务商/)).toBeNull();
    });

    it('opens the Token Bank share dialog from 存入银行', async () => {
        listProvidersMock.mockResolvedValue({ providers: [kimi] });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await waitFor(() => expect(screen.getByRole('button', { name: '将Kimi Code存入银行' })).toBeTruthy());
        fireEvent.click(screen.getByRole('button', { name: '将Kimi Code存入银行' }));
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        await waitFor(() => {
            const dialog = screen.getByRole('dialog', { name: '分享服务商' });
            expect(dialog.contains(document.activeElement)).toBe(true);
        });
        expect(screen.getByText('开始探测')).toBeTruthy();
        expect(fetchModelsMock).toHaveBeenCalledWith(
            'https://api.kimi.com/coding/v1',
            'sk-kimi',
            'openai',
            expect.any(String),
        );
        const picker = screen.getByRole('combobox', { name: /要存入的服务商/ });
        expect(picker.hasAttribute('disabled')).toBe(true);
        fireEvent.click(screen.getByRole('button', { name: '取消' }));
        await waitFor(() => expect(screen.queryByRole('dialog', { name: '分享服务商' })).toBeNull());
        expect(picker.hasAttribute('disabled')).toBe(false);
        await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('button', { name: '将Kimi Code存入银行' })));
    });

    it('leaves the share dialog closed when the deposit confirm is cancelled', async () => {
        listProvidersMock.mockResolvedValue({ providers: [kimi] });
        showConfirmMock.mockResolvedValue(false);
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await waitFor(() => expect(screen.getByRole('button', { name: '将Kimi Code存入银行' })).toBeTruthy());
        fireEvent.click(screen.getByRole('button', { name: '将Kimi Code存入银行' }));
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        expect(screen.queryByRole('dialog', { name: '分享服务商' })).toBeNull();
        expect(fetchModelsMock).not.toHaveBeenCalled();
    });

    it('shows a load error instead of the empty-provider copy', async () => {
        listProvidersMock.mockRejectedValue(new Error('providers offline'));
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('providers offline'));
        expect(screen.queryByText(/还没有测试可用的服务商/)).toBeNull();
    });

    it('says when no tested provider is ready', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [{ name: 'DeepSeek', url: 'https://api.deepseek.com/v1', key: '', connection_test_passed: false }],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await waitFor(() => expect(screen.getByText(/还没有测试可用的服务商/)).toBeTruthy());
        expect(screen.queryByText('存入银行')).toBeNull();
    });

    it('keeps the chosen provider while a deposit confirm is open', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [
                kimi,
                { ...kimi, id: 'plain', name: 'My Relay', url: 'https://relay.example/v1', key: 'sk-relay' },
            ],
        });
        let resolveConfirm: (value: boolean) => void = () => {};
        showConfirmMock.mockImplementation(() => new Promise<boolean>((resolve) => {
            resolveConfirm = resolve;
        }));
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await screen.findByRole('combobox', { name: /要存入的服务商/ });
        fireEvent.click(screen.getByRole('button', { name: '将Kimi Code存入银行' }));
        await waitFor(() => expect(picker.hasAttribute('disabled')).toBe(true));
        fireEvent.click(picker);
        expect(screen.queryByRole('listbox')).toBeNull();
        expect(screen.getByRole('button', { name: '将Kimi Code存入银行' })).toBeTruthy();
        expect(showConfirmMock).toHaveBeenCalledTimes(1);
        expect(fetchModelsMock).not.toHaveBeenCalled();
        resolveConfirm(false);
        await waitFor(() => expect(picker.hasAttribute('disabled')).toBe(false));
        showConfirmMock.mockResolvedValue(true);
        fireEvent.click(picker);
        fireEvent.click(await screen.findByRole('option', { name: 'My Relay' }));
        fireEvent.click(screen.getByRole('button', { name: '将My Relay存入银行' }));
        await waitFor(() => expect(fetchModelsMock).toHaveBeenCalledWith(
            'https://relay.example/v1',
            'sk-relay',
            'openai',
            expect.any(String),
        ));
    });

    it('treats a non-array provider payload as unavailable and keeps the previous choice', async () => {
        listProvidersMock.mockResolvedValue({ providers: [kimi] });
        const { rerender } = render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await openProviderMenu();
        expect(screen.getByRole('option', { name: 'Kimi Code' })).toBeTruthy();
        listProvidersMock.mockResolvedValue({ providers: null });
        rerender(<TokenBankShareableProviders lang="zh-Hans" reloadToken={2} />);
        await waitFor(() => expect(screen.getByText('服务商列表不可用。')).toBeTruthy());
        await openProviderMenu();
        expect(screen.getByRole('option', { name: 'Kimi Code' })).toBeTruthy();
        expect(screen.queryByText(/还没有测试可用的服务商/)).toBeNull();
    });

    it('shows the unavailable copy when the first payload is not a list', async () => {
        listProvidersMock.mockResolvedValue({ providers: { name: 'Kimi Code' } });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await waitFor(() => expect(screen.getByText('服务商列表不可用。')).toBeTruthy());
        expect(screen.queryByText(/还没有测试可用的服务商/)).toBeNull();
        expect(screen.queryByRole('combobox')).toBeNull();
    });

    it('trims the provider url and name before sharing', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [{ ...kimi, name: '  Kimi Code  ', url: ' https://api.kimi.com/coding/v1 ', protocol: '  anthropic  ' }],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await openProviderMenu();
        expect(screen.getByRole('option', { name: 'Kimi Code' })).toBeTruthy();
        fireEvent.click(screen.getByRole('button', { name: '将Kimi Code存入银行' }));
        await waitFor(() => expect(fetchModelsMock).toHaveBeenCalledWith(
            'https://api.kimi.com/coding/v1',
            'sk-kimi',
            'anthropic',
            expect.any(String),
        ));
    });

    it('does not show a URL password in the provider tooltip, and lists each model once', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [{ ...kimi, url: 'https://user:secret@api.kimi.com/coding/v1' }],
        });
        fetchModelsMock.mockResolvedValue([
            { id: 'kimi-for-coding', name: 'Kimi' },
            { id: 'kimi-for-coding', name: 'Kimi again' },
            { id: 'kimi-k2', name: 'K2' },
            { id: 'Kimi-K2', name: 'K2 again' },
        ]);
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await screen.findByRole('combobox', { name: /要存入的服务商/ });
        expect(picker.getAttribute('title')).toBe('https://api.kimi.com/coding/v1');
        expect(picker.getAttribute('title')).not.toContain('secret');
        fireEvent.click(screen.getByRole('button', { name: '将Kimi Code存入银行' }));
        const dialog = await screen.findByRole('dialog', { name: '分享服务商' });
        expect(dialog.textContent).toContain('https://api.kimi.com/coding/v1');
        expect(dialog.textContent).not.toContain('secret');
        expect(fetchModelsMock).toHaveBeenCalledWith(
            'https://user:secret@api.kimi.com/coding/v1',
            'sk-kimi',
            'openai',
            expect.any(String),
        );
        expect(screen.getAllByRole('checkbox', { name: 'kimi-for-coding' })).toHaveLength(1);
        expect(screen.getAllByRole('checkbox', { name: 'kimi-k2' })).toHaveLength(1);
        expect(screen.queryByRole('checkbox', { name: 'Kimi-K2' })).toBeNull();
    });

    it('keeps a provider that has no id selected when a refresh inserts one above it', async () => {
        const relay = {
            name: 'My Relay',
            url: 'https://relay.example/v1',
            key: 'sk-relay',
            protocol: 'openai',
            connection_test_passed: true,
        };
        const kimiNoId = { ...kimi, id: '' };
        listProvidersMock.mockResolvedValue({ providers: [kimiNoId, relay] });
        const { rerender } = render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await openProviderMenu();
        fireEvent.click(screen.getByRole('option', { name: 'My Relay' }));
        expect(screen.getByRole('button', { name: '将My Relay存入银行' })).toBeTruthy();

        listProvidersMock.mockResolvedValue({
            providers: [
                { ...relay, name: 'OpenAI', url: 'https://api.openai.com/v1', key: 'sk-o' },
                kimiNoId,
                relay,
            ],
        });
        rerender(<TokenBankShareableProviders lang="zh-Hans" reloadToken={2} />);
        await openProviderMenu();
        expect(screen.getByRole('option', { name: 'OpenAI' })).toBeTruthy();
        expect(screen.getByRole('option', { name: 'My Relay' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('button', { name: '将My Relay存入银行' })).toBeTruthy();
    });

    it('does not fetch models when the confirm is answered after the list is gone', async () => {
        listProvidersMock.mockResolvedValue({ providers: [kimi] });
        let resolveConfirm: (value: boolean) => void = () => {};
        showConfirmMock.mockImplementation(() => new Promise<boolean>((resolve) => {
            resolveConfirm = resolve;
        }));
        const { unmount } = render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        fireEvent.click(await screen.findByRole('button', { name: '将Kimi Code存入银行' }));
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalledTimes(1));
        unmount();
        await act(async () => {
            resolveConfirm(true);
        });
        expect(fetchModelsMock).not.toHaveBeenCalled();

        showConfirmMock.mockResolvedValue(true);
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        fireEvent.click(await screen.findByRole('button', { name: '将Kimi Code存入银行' }));
        await waitFor(() => expect(fetchModelsMock).toHaveBeenCalledTimes(1));
    });

    it('draws a mark on every open row, including both WorkBuddy logos at once', async () => {
        const names = [
            'Claude Code',
            'Codex',
            'xAI-Grok',
            'OpenCode',
            '智谱编程',
            'Kimi Code',
            'WorkBuddy 国内版',
            'WorkBuddy 国际版',
            'Custom1',
        ];
        listProvidersMock.mockResolvedValue({
            providers: names.map((name, index) => ({ ...kimi, id: `row-${index}`, name })),
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await openProviderMenu();
        const faceSvg = picker.querySelector('svg');
        const claudeSvg = screen.getByRole('option', { name: 'Claude Code' }).querySelector('svg');
        expect(faceSvg).toBeTruthy();
        expect(claudeSvg).toBeTruthy();
        expect(faceSvg).not.toBe(claudeSvg);
        for (const name of names) {
            const option = screen.getByRole('option', { name });
            if (name === 'Custom1') {
                expect(option.querySelector('svg')).toBeNull();
                expect(option.querySelector('.tbk-provider-pick__monogram')?.textContent).toBe('C');
            } else {
                expect(option.querySelector('.tbk-provider-pick__mark svg')).toBeTruthy();
            }
        }
        const china = screen.getByRole('option', { name: 'WorkBuddy 国内版' }).querySelector('svg');
        const global = screen.getByRole('option', { name: 'WorkBuddy 国际版' }).querySelector('svg');
        expect(china).toBeTruthy();
        expect(global).toBeTruthy();
        expect(china).not.toBe(global);
    });

    it('selects the highlighted provider with the keyboard', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [kimi, { ...kimi, id: 'plain', name: 'My Relay' }],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await screen.findByRole('combobox', { name: /要存入的服务商/ });
        fireEvent.keyDown(picker, { key: 'ArrowDown' });
        expect(await screen.findByRole('listbox')).toBeTruthy();
        expect(screen.getByRole('option', { name: 'Kimi Code' }).className).toContain('tbk-provider-pick__option--active');
        fireEvent.keyDown(picker, { key: 'ArrowDown' });
        expect(screen.getByRole('option', { name: 'My Relay' }).className).toContain('tbk-provider-pick__option--active');
        fireEvent.keyDown(picker, { key: 'Enter' });
        expect(screen.queryByRole('listbox')).toBeNull();
        expect(picker.parentElement?.querySelector('.tbk-provider-pick__label')?.textContent).toBe('My Relay');
        expect(picker.parentElement?.querySelector('.tbk-provider-pick__monogram')?.textContent).toBe('M');
    });

    it('keeps option buttons out of the tab order and closes when focus leaves', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [kimi, { ...kimi, id: 'plain', name: 'My Relay' }],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await openProviderMenu();
        const option = screen.getByRole('option', { name: 'My Relay' });
        expect(option.getAttribute('tabindex')).toBe('-1');
        fireEvent.blur(picker, { relatedTarget: screen.getByRole('button', { name: '将Kimi Code存入银行' }) });
        expect(screen.queryByRole('listbox')).toBeNull();
    });

    it('jumps to a provider by the letters typed on the closed control', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [kimi, { ...kimi, id: 'plain', name: 'My Relay' }],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await screen.findByRole('combobox', { name: '要存入的服务商 Kimi Code' });
        fireEvent.keyDown(picker, { key: 'm' });
        expect(screen.getByRole('combobox', { name: '要存入的服务商 My Relay' })).toBeTruthy();
        expect(picker.parentElement?.querySelector('.tbk-provider-pick__label')?.textContent).toBe('My Relay');
        expect(screen.queryByRole('listbox')).toBeNull();
        fireEvent.click(picker);
        fireEvent.keyDown(picker, { key: 'k' });
        expect(screen.getByRole('listbox')).toBeTruthy();
        expect(screen.getByRole('option', { name: 'Kimi Code' }).className).toContain('tbk-provider-pick__option--active');
        expect(picker.parentElement?.querySelector('.tbk-provider-pick__label')?.textContent).toBe('Kimi Code');
    });

    it('narrows a quick second letter and ignores space', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [
                { ...kimi, id: 'ka', name: 'Ka' },
                { ...kimi, id: 'ki', name: 'Ki' },
                { ...kimi, id: 'plain', name: 'My Relay' },
            ],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await screen.findByRole('combobox', { name: /要存入的服务商/ });
        const label = () => picker.parentElement?.querySelector('.tbk-provider-pick__label')?.textContent;
        fireEvent.keyDown(picker, { key: 'k' });
        expect(label()).toBe('Ki');
        fireEvent.keyDown(picker, { key: 'a' });
        expect(label()).toBe('Ka');
        expect(screen.queryByRole('listbox')).toBeNull();
    });

    it('does not treat space as part of the typed name', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [kimi, { ...kimi, id: 'plain', name: 'My Relay' }],
        });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        const picker = await screen.findByRole('combobox', { name: /要存入的服务商/ });
        fireEvent.keyDown(picker, { key: ' ' });
        fireEvent.keyDown(picker, { key: 'm' });
        expect(picker.parentElement?.querySelector('.tbk-provider-pick__label')?.textContent).toBe('My Relay');
    });

    it('drops the open list before the deposit confirm is on screen', async () => {
        listProvidersMock.mockResolvedValue({ providers: [kimi] });
        render(<TokenBankShareableProviders lang="zh-Hans" reloadToken={1} />);
        await openProviderMenu();
        fireEvent.click(screen.getByRole('button', { name: '将Kimi Code存入银行' }));
        await waitFor(() => expect(screen.getByRole('dialog', { name: '分享服务商' })).toBeTruthy());
        expect(screen.queryByRole('listbox')).toBeNull();
    });

    it('reads the closed control cap in pixels', () => {
        const el = document.createElement('div');
        document.body.appendChild(el);
        expect(controlWidthCap(el)).toBe(0);
        el.style.maxWidth = '288px';
        expect(controlWidthCap(el)).toBe(288);
        el.style.maxWidth = 'none';
        expect(controlWidthCap(el)).toBe(0);
        el.remove();
    });

    it('grows the menu to the label and keeps it inside the viewport', () => {
        const wide = placeProviderMenu(
            { left: 700, top: 100, bottom: 136, width: 240 },
            420,
            800,
            600,
        );
        expect(wide.minWidth).toBe(420);
        expect(wide.width).toBe(420);
        const stretched = placeProviderMenu(
            { left: 40, top: 100, bottom: 136, width: 900 },
            220,
            1200,
            600,
        );
        expect(stretched.width).toBe(220);
        expect(stretched.minWidth).toBe(220);
        const unmeasured = placeProviderMenu(
            { left: 40, top: 100, bottom: 136, width: 240 },
            0,
            1200,
            600,
        );
        expect(unmeasured.width).toBe(240);
        const aligned = placeProviderMenu(
            { left: 40, top: 100, bottom: 136, width: 200 },
            160,
            1200,
            600,
            288,
        );
        expect(aligned.width).toBe(200);
        const capped = placeProviderMenu(
            { left: 40, top: 100, bottom: 136, width: 900 },
            220,
            1200,
            600,
            288,
        );
        expect(capped.width).toBe(288);
        const hinted = placeProviderMenu(
            { left: 40, top: 100, bottom: 136, width: 200 },
            400,
            1200,
            600,
            288,
        );
        expect(hinted.width).toBe(400);
        expect(wide.left + wide.width).toBeLessThanOrEqual(800 - 8);
        expect(wide.left).toBeGreaterThanOrEqual(8);
        expect(wide.top).toBe(140);

        const upward = placeProviderMenu(
            { left: 20, top: 500, bottom: 536, width: 200 },
            200,
            800,
            560,
        );
        expect(upward.top).toBe('auto');
        expect(upward.bottom).toBe(560 - 500 + 4);
        expect(wide.maxHeight).toBe(360);

        const tight = placeProviderMenu(
            { left: 20, top: 40, bottom: 76, width: 200 },
            200,
            800,
            100,
        );
        expect(tight.top).toBe('auto');
        expect(tight.maxHeight).toBe(40 - 4 - 8);
        expect(tight.maxHeight).toBeLessThanOrEqual(100 - 8);
        expect(fitMenuWidth(455, 80, 200, 240, 238, 2)).toBe(455);
        expect(fitMenuWidth(455, 360, 103, 240, 223, 2)).toBe(455 + 15);
        expect(fitMenuWidth(455, 360, 103, 240, 238, 2)).toBe(455);
        expect(fitMenuWidth(0, 360, 103, 240, 223, 2)).toBe(0);
    });
});
