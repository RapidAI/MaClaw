// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({
    TokenBankListShareAudiences: vi.fn(),
    TokenBankSetShareVisibility: vi.fn(),
}));

const { showAlertMock } = vi.hoisted(() => ({
    showAlertMock: vi.fn().mockResolvedValue(true),
}));

vi.mock('../CustomDialog', () => ({
    useDialog: () => ({ showAlert: showAlertMock }),
}));

import { TokenBankListShareAudiences, TokenBankSetShareVisibility } from '../../../wailsjs/go/main/App';
import { TokenBankAccessDialog } from '../TokenBankAccessDialog';
import type { TokenBankShare } from '../../utils/hubcenterTokenBank';

const audiences = {
    hubs: [
        { id: 'hub-b', name: 'Beta' },
        { id: 'hub-a', name: '' },
    ],
    tenants: [{ id: 'ten-1', name: '市场', hub_id: 'hub-b', hub_name: 'Beta' }],
};

function share(overrides: Partial<TokenBankShare> = {}): TokenBankShare {
    return {
        id: 'sh_1',
        display_name: 'My OpenAI',
        visibility: 'public',
        audiences: [],
        ...overrides,
    } as TokenBankShare;
}

function renderAccess(overrides: Partial<TokenBankShare> = {}) {
    const onClose = vi.fn();
    const onSaved = vi.fn();
    render(
        <TokenBankAccessDialog
            lang="zh-Hans"
            share={share(overrides)}
            onClose={onClose}
            onSaved={onSaved}
        />,
    );
    return { onClose, onSaved };
}

beforeEach(() => {
    vi.mocked(TokenBankListShareAudiences).mockReset();
    vi.mocked(TokenBankListShareAudiences).mockResolvedValue(audiences);
    vi.mocked(TokenBankSetShareVisibility).mockReset();
    vi.mocked(TokenBankSetShareVisibility).mockResolvedValue({ ok: true });
    showAlertMock.mockReset();
    showAlertMock.mockResolvedValue(true);
});

describe('TokenBankAccessDialog', () => {
    it('asks the owner to choose public or private, with no text field', () => {
        renderAccess();
        const dialog = screen.getByRole('dialog', { name: '更改访问范围' });
        expect(dialog.querySelector('input[type="text"], textarea')).toBeNull();
        expect(screen.queryByText(/输入 public 或 private/)).toBeNull();
        expect((screen.getByRole('radio', { name: '公开' }) as HTMLInputElement).checked).toBe(true);
        expect(screen.queryByRole('checkbox')).toBeNull();
    });

    it('saves public access without asking for hub or tenant ids', async () => {
        const { onSaved } = renderAccess();
        fireEvent.click(screen.getByRole('button', { name: '确定' }));
        await waitFor(() => expect(TokenBankSetShareVisibility).toHaveBeenCalledWith('sh_1', 'public', []));
        expect(onSaved).toHaveBeenCalledTimes(1);
        expect(TokenBankListShareAudiences).not.toHaveBeenCalled();
    });

    it('does not save when the dialog is cancelled', () => {
        const { onClose, onSaved } = renderAccess();
        fireEvent.click(screen.getByRole('button', { name: '取消' }));
        expect(onClose).toHaveBeenCalledTimes(1);
        expect(onSaved).not.toHaveBeenCalled();
        expect(TokenBankSetShareVisibility).not.toHaveBeenCalled();
    });

    it('keeps the current private hubs and tenants checked, then saves them as separate lists', async () => {
        renderAccess({
            visibility: 'private',
            audiences: [
                { hub_id: 'hub-b', tenant_id: '' },
                { hub_id: '', tenant_id: 'ten-1' },
            ],
        });
        expect((screen.getByRole('radio', { name: '私有' }) as HTMLInputElement).checked).toBe(true);
        await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Beta' })).toBeTruthy());
        expect((screen.getByRole('checkbox', { name: 'Beta' }) as HTMLInputElement).checked).toBe(true);
        expect((screen.getByRole('checkbox', { name: 'hub-a' }) as HTMLInputElement).checked).toBe(false);
        expect((screen.getByRole('checkbox', { name: '市场' }) as HTMLInputElement).checked).toBe(true);
        expect(screen.getByRole('dialog').querySelector('input[type="text"], textarea')).toBeNull();

        fireEvent.click(screen.getByRole('checkbox', { name: 'hub-a' }));
        fireEvent.click(screen.getByRole('button', { name: '确定' }));

        await waitFor(() => expect(TokenBankSetShareVisibility).toHaveBeenCalledTimes(1));
        expect(TokenBankSetShareVisibility).toHaveBeenCalledWith('sh_1', 'private', [
            { hub_id: 'hub-b', tenant_id: '' },
            { hub_id: 'hub-a', tenant_id: '' },
            { hub_id: '', tenant_id: 'ten-1' },
        ]);
    });

    it('uses the loaded id casing for a grant that was stored in another case', async () => {
        vi.mocked(TokenBankListShareAudiences).mockResolvedValue({
            hubs: [{ id: 'Hub-B', name: 'Beta' }],
            tenants: [],
        });
        renderAccess({
            visibility: 'private',
            audiences: [{ hub_id: 'hub-b', tenant_id: '' }],
        });
        await waitFor(() => expect((screen.getByRole('checkbox', { name: 'Beta' }) as HTMLInputElement).checked).toBe(true));
        fireEvent.click(screen.getByRole('button', { name: '确定' }));
        await waitFor(() => expect(TokenBankSetShareVisibility).toHaveBeenCalledWith('sh_1', 'private', [
            { hub_id: 'Hub-B', tenant_id: '' },
        ]));
    });

    it('still shows a granted hub that the account list no longer returns', async () => {
        renderAccess({
            visibility: 'private',
            audiences: [{ hub_id: 'hub-old', tenant_id: '' }],
        });
        await waitFor(() => expect((screen.getByRole('checkbox', { name: 'hub-old' }) as HTMLInputElement).checked).toBe(true));
        fireEvent.click(screen.getByRole('button', { name: '确定' }));
        await waitFor(() => expect(TokenBankSetShareVisibility).toHaveBeenCalledWith('sh_1', 'private', [
            { hub_id: 'hub-old', tenant_id: '' },
        ]));
    });

    it('keeps a hub-and-tenant grant as one row', async () => {
        renderAccess({
            visibility: 'private',
            audiences: [{ hub_id: 'hub-b', tenant_id: 'ten-1' }],
        });
        await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Beta' })).toBeTruthy());
        expect((screen.getByRole('checkbox', { name: 'Beta' }) as HTMLInputElement).checked).toBe(false);
        expect((screen.getByRole('checkbox', { name: '市场' }) as HTMLInputElement).checked).toBe(false);
        const pair = screen.getByRole('checkbox', { name: 'Beta + 市场' }) as HTMLInputElement;
        expect(pair.checked).toBe(true);
        fireEvent.click(screen.getByRole('button', { name: '确定' }));
        await waitFor(() => expect(TokenBankSetShareVisibility).toHaveBeenCalledWith('sh_1', 'private', [
            { hub_id: 'hub-b', tenant_id: 'ten-1' },
        ]));
    });

    it('clears a tighter pair when its hub or tenant is checked, and the reverse', async () => {
        renderAccess({
            visibility: 'private',
            audiences: [{ hub_id: 'hub-b', tenant_id: 'ten-1' }],
        });
        await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Beta + 市场' })).toBeTruthy());
        const pair = () => screen.getByRole('checkbox', { name: 'Beta + 市场' }) as HTMLInputElement;
        const hub = () => screen.getByRole('checkbox', { name: 'Beta' }) as HTMLInputElement;
        const tenant = () => screen.getByRole('checkbox', { name: '市场' }) as HTMLInputElement;
        const save = () => screen.getByRole('button', { name: '确定' }) as HTMLButtonElement;

        fireEvent.click(hub());
        expect(hub().checked).toBe(true);
        expect(pair().checked).toBe(false);
        fireEvent.click(save());
        await waitFor(() => expect(TokenBankSetShareVisibility).toHaveBeenCalledWith('sh_1', 'private', [
            { hub_id: 'hub-b', tenant_id: '' },
        ]));
        await waitFor(() => expect(save().disabled).toBe(false));

        fireEvent.click(pair());
        expect(pair().checked).toBe(true);
        expect(hub().checked).toBe(false);
        fireEvent.click(save());
        await waitFor(() => expect(TokenBankSetShareVisibility).toHaveBeenLastCalledWith('sh_1', 'private', [
            { hub_id: 'hub-b', tenant_id: 'ten-1' },
        ]));
        await waitFor(() => expect(save().disabled).toBe(false));

        fireEvent.click(tenant());
        expect(tenant().checked).toBe(true);
        expect(pair().checked).toBe(false);
        fireEvent.click(save());
        await waitFor(() => expect(TokenBankSetShareVisibility).toHaveBeenLastCalledWith('sh_1', 'private', [
            { hub_id: '', tenant_id: 'ten-1' },
        ]));
    });

    it('opens a stored pair unchecked when a wider hub grant already covers it', async () => {
        renderAccess({
            visibility: 'private',
            audiences: [
                { hub_id: 'hub-b', tenant_id: '' },
                { hub_id: 'hub-b', tenant_id: 'ten-1' },
            ],
        });
        await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Beta + 市场' })).toBeTruthy());
        expect((screen.getByRole('checkbox', { name: 'Beta' }) as HTMLInputElement).checked).toBe(true);
        expect((screen.getByRole('checkbox', { name: 'Beta + 市场' }) as HTMLInputElement).checked).toBe(false);
        fireEvent.click(screen.getByRole('button', { name: '确定' }));
        await waitFor(() => expect(TokenBankSetShareVisibility).toHaveBeenCalledWith('sh_1', 'private', [
            { hub_id: 'hub-b', tenant_id: '' },
        ]));
    });

    it('leaves save disabled until a private share has a hub or tenant', async () => {
        renderAccess();
        fireEvent.click(screen.getByRole('radio', { name: '私有' }));
        await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Beta' })).toBeTruthy());
        const save = screen.getByRole('button', { name: '确定' }) as HTMLButtonElement;
        expect(save.disabled).toBe(true);
        fireEvent.click(save);
        expect(TokenBankSetShareVisibility).not.toHaveBeenCalled();
        expect(showAlertMock).not.toHaveBeenCalled();
    });

    it('keeps keyboard focus inside the dialog unless another dialog is in front', async () => {
        const outside = document.createElement('button');
        outside.type = 'button';
        document.body.appendChild(outside);
        const alert = document.createElement('div');
        alert.setAttribute('role', 'dialog');
        const alertButton = document.createElement('button');
        alertButton.type = 'button';
        alert.appendChild(alertButton);
        document.body.appendChild(alert);
        const { onClose } = renderAccess();
        const dialog = screen.getByRole('dialog', { name: '更改访问范围' });
        await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));

        const save = screen.getByRole('button', { name: '确定' });
        save.focus();
        fireEvent.keyDown(window, { key: 'Tab' });
        expect(document.activeElement).toBe(screen.getByRole('radio', { name: '公开' }));

        outside.focus();
        expect(dialog.contains(document.activeElement)).toBe(true);

        alertButton.focus();
        expect(document.activeElement).toBe(alertButton);
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(onClose).not.toHaveBeenCalled();

        screen.getByRole('radio', { name: '公开' }).focus();
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(onClose).toHaveBeenCalledTimes(1);
        outside.remove();
        alert.remove();
    });

    it('leaves save disabled while hubs and tenants are still loading', () => {
        vi.mocked(TokenBankListShareAudiences).mockReturnValue(new Promise(() => {}));
        renderAccess();
        fireEvent.click(screen.getByRole('radio', { name: '私有' }));
        const save = screen.getByRole('button', { name: '确定' }) as HTMLButtonElement;
        expect(save.disabled).toBe(true);
        fireEvent.click(save);
        expect(TokenBankSetShareVisibility).not.toHaveBeenCalled();
        expect(showAlertMock).not.toHaveBeenCalled();
    });
});
