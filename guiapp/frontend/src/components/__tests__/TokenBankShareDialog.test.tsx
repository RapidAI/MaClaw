// @vitest-environment jsdom
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('../../../wailsjs/go/main/App', () => ({
    TokenBankCreateShare: vi.fn(),
    TokenBankSyncShareModels: vi.fn(),
    TokenBankListShareAudiences: vi.fn(),
}));

const { showAlertMock, showConfirmMock } = vi.hoisted(() => ({
    showAlertMock: vi.fn().mockResolvedValue(true),
    showConfirmMock: vi.fn().mockResolvedValue(true),
}));

vi.mock('../CustomDialog', () => ({
    useDialog: () => ({
        showAlert: showAlertMock,
        showConfirm: showConfirmMock,
    }),
}));

import { TokenBankCreateShare, TokenBankListShareAudiences, TokenBankSyncShareModels } from '../../../wailsjs/go/main/App';
import { TokenBankShareDialog, type TokenBankShareAdjustment, type TokenBankShareRequest } from '../TokenBankShareDialog';

const baseRequest: TokenBankShareRequest = {
    providerName: 'OpenAI',
    apiURL: 'https://api.x/v1',
    apiKey: 'sk-secret-value',
    protocol: 'openai',
    models: ['model-a', 'model-b'],
};

function renderDialog(
    overrides: Partial<TokenBankShareRequest> = {},
    probe = vi.fn(),
    onRequestVerification?: () => void,
    alreadySharedModels?: string[],
) {
    const onClose = vi.fn();
    const onShared = vi.fn();
    const view = render(
        <TokenBankShareDialog
            lang="zh-Hans"
            request={{ ...baseRequest, ...overrides }}
            probe={probe}
            onClose={onClose}
            onShared={onShared}
            onRequestVerification={onRequestVerification}
            alreadySharedModels={alreadySharedModels}
        />,
    );
    return { onClose, onShared, probe, unmount: view.unmount };
}

beforeEach(() => {
    vi.mocked(TokenBankCreateShare).mockReset();
    vi.mocked(TokenBankCreateShare).mockResolvedValue({ id: 'share_1' });
    vi.mocked(TokenBankSyncShareModels).mockReset();
    vi.mocked(TokenBankSyncShareModels).mockResolvedValue({ id: 'share_9' });
    vi.mocked(TokenBankListShareAudiences).mockReset();
    vi.mocked(TokenBankListShareAudiences).mockResolvedValue({
        hubs: [
            { id: 'hub-b', name: 'Beta' },
            { id: 'hub-a', name: '' },
        ],
        tenants: [{ id: 'ten-1', name: '市场', hub_id: 'hub-b', hub_name: 'Beta' }],
    });
    showAlertMock.mockReset();
    showAlertMock.mockResolvedValue(true);
    showConfirmMock.mockReset();
    showConfirmMock.mockResolvedValue(true);
});

describe('TokenBankShareDialog', () => {
    it('selects every model by default', () => {
        renderDialog();
        const modelA = screen.getByRole('checkbox', { name: 'model-a' }) as HTMLInputElement;
        const modelB = screen.getByRole('checkbox', { name: 'model-b' }) as HTMLInputElement;
        expect(modelA.checked).toBe(true);
        expect(modelB.checked).toBe(true);
        expect((screen.getByRole('checkbox', { name: 'model-a 周一' }) as HTMLInputElement).checked).toBe(true);
        expect((screen.getByRole('checkbox', { name: 'model-a 周日' }) as HTMLInputElement).checked).toBe(true);
        expect((screen.getByLabelText('model-a 开始') as HTMLInputElement).value).toBe('00:00');
        expect((screen.getByLabelText('model-a 结束') as HTMLInputElement).value).toBe('24:00');
        expect(screen.getByText('已选 2 个')).toBeTruthy();
    });

    it('clears and restores the selection with the All / None controls', () => {
        renderDialog();

        fireEvent.click(screen.getByText('全不选'));
        expect((screen.getAllByRole('checkbox') as HTMLInputElement[]).every((box) => !box.checked)).toBe(true);
        expect(screen.getByText('已选 0 个')).toBeTruthy();

        fireEvent.click(screen.getByText('全选'));
        expect((screen.getAllByRole('checkbox') as HTMLInputElement[]).every((box) => box.checked)).toBe(true);
        expect(screen.getByText('已选 2 个')).toBeTruthy();
    });

    it('shows probes as pending until Start probe is pressed', () => {
        renderDialog();
        expect(screen.getAllByText(/待探测/).length).toBeGreaterThan(0);
    });

    it('advances the progress counter only for finished probes', async () => {
        const probe = vi
            .fn()
            .mockResolvedValueOnce({ ok: true, latencyMs: 312 })
            .mockResolvedValueOnce({ ok: false, error: '404 model not found' });
        renderDialog({}, probe);

        fireEvent.click(screen.getByText('开始探测'));

        await waitFor(() => expect(screen.getByText('2/2')).toBeTruthy());
        expect(probe).toHaveBeenCalledTimes(2);
        expect(screen.getByText(/312 ms/)).toBeTruthy();
        expect(screen.getByText('404 model not found')).toBeTruthy();
        // Both rows stay checked. 已选 matches the footer: a model the probe
        // proved unusable no longer counts. The header is a count, not the
        // probe fraction rendered as 2/2.
        expect(screen.getByText('已选 1 个')).toBeTruthy();
    });

    it('probes only the checked models', async () => {
        const probe = vi.fn().mockResolvedValue({ ok: true, latencyMs: 40 });
        renderDialog({}, probe);
        fireEvent.click(screen.getByRole('checkbox', { name: 'model-b' }));

        fireEvent.click(screen.getByText('开始探测'));

        await waitFor(() => expect(screen.getByText('1/1')).toBeTruthy());
        expect(probe).toHaveBeenCalledTimes(1);
        expect(probe).toHaveBeenCalledWith('model-a');
        expect(screen.getAllByText(/待探测/)).toHaveLength(1);
        expect(screen.getByText('已选 1 个')).toBeTruthy();
    });

    it('does not share while a probe is still running', async () => {
        const pending: Array<(value: { ok: boolean; latencyMs: number }) => void> = [];
        const probe = vi.fn().mockImplementation(() => new Promise((resolve) => pending.push(resolve)));
        renderDialog({}, probe);
        fireEvent.click(screen.getByText('开始探测'));
        await waitFor(() => expect(probe).toHaveBeenCalledTimes(1));

        const confirm = screen.getByText('确认分享').closest('button') as HTMLButtonElement;
        expect(confirm.disabled).toBe(true);
        fireEvent.click(confirm);
        expect(TokenBankCreateShare).not.toHaveBeenCalled();

        pending[0]({ ok: true, latencyMs: 1 });
        await waitFor(() => expect(probe).toHaveBeenCalledTimes(2));
        pending[1]({ ok: true, latencyMs: 1 });
        await waitFor(() => expect((screen.getByText('确认分享').closest('button') as HTMLButtonElement).disabled).toBe(false));
    });

    it('does not probe a model unchecked before its turn', async () => {
        let release: (value: { ok: boolean; latencyMs: number }) => void = () => {};
        const probe = vi.fn()
            .mockImplementationOnce(() => new Promise((resolve) => {
                release = resolve;
            }))
            .mockResolvedValue({ ok: true, latencyMs: 5 });
        renderDialog({}, probe);
        fireEvent.click(screen.getByText('开始探测'));
        await waitFor(() => expect(probe).toHaveBeenCalledTimes(1));
        expect(probe).toHaveBeenCalledWith('model-a');
        expect(screen.getAllByText('待探测')).toHaveLength(1);

        fireEvent.click(screen.getByRole('checkbox', { name: 'model-b' }));
        release({ ok: true, latencyMs: 5 });

        await waitFor(() => expect(screen.getByText('开始探测')).toBeTruthy());
        expect(probe).toHaveBeenCalledTimes(1);
        expect(screen.getByText('1/1')).toBeTruthy();
    });

    it('allows submitting models that have not been probed yet', async () => {
        // Regression: the dialog opens before probing, so a `pending` model must
        // still be submittable. Counting only `available` models disabled the
        // Confirm button on open, which is the state the user acts from.
        renderDialog();

        const confirm = screen.getByText('确认分享').closest('button') as HTMLButtonElement;
        expect(confirm.disabled).toBe(false);

        fireEvent.click(confirm);
        await waitFor(() => expect(TokenBankCreateShare).toHaveBeenCalledTimes(1));
        const payload = vi.mocked(TokenBankCreateShare).mock.calls[0][0] as unknown as Record<string, unknown>;
        // Unprobed models are published. The server only routes rows marked
        // available, and "not probed yet" is not a failed probe. A failed
        // probe is the row that stays available:false.
        const models = payload.Models as Array<Record<string, unknown>>;
        expect(models.map((entry) => entry.model)).toEqual(['model-a', 'model-b']);
        expect(models.every((entry) => entry.available === true)).toBe(true);
        expect(models.every((entry) => entry.probe_error === '')).toBe(true);
        // Every day 0:00–24:00 is the always-on default. It is omitted so an
        // older share with no window and a new untouched row stay the same.
        expect(models.every((entry) => entry.share_window === undefined)).toBe(true);
    });

    it('refuses to submit when nothing is selected', async () => {
        renderDialog();
        fireEvent.click(screen.getByText('全不选'));

        const confirm = screen.getByText('确认分享').closest('button') as HTMLButtonElement;
        expect(confirm.disabled).toBe(true);

        fireEvent.click(confirm);
        await waitFor(() => expect(TokenBankCreateShare).not.toHaveBeenCalled());
    });

    it('submits the selected models with their probe verdicts', async () => {
        const probe = vi
            .fn()
            .mockResolvedValueOnce({ ok: true, latencyMs: 100 })
            .mockResolvedValueOnce({ ok: false, error: 'boom' });
        const { onShared, onClose } = renderDialog({}, probe);

        fireEvent.click(screen.getByText('开始探测'));
        await waitFor(() => expect(screen.getByText('2/2')).toBeTruthy());

        fireEvent.click(screen.getByText('确认分享'));

        await waitFor(() => expect(TokenBankCreateShare).toHaveBeenCalledTimes(1));
        const payload = vi.mocked(TokenBankCreateShare).mock.calls[0][0] as unknown as Record<string, unknown>;
        const models = payload.Models as Array<Record<string, unknown>>;
        expect(models).toHaveLength(2);
        expect(models[0]).toMatchObject({ model: 'model-a', available: true });
        expect(models[1]).toMatchObject({ model: 'model-b', available: false, probe_error: 'boom' });
        // The name, endpoint, and key travel with the submission. Wails binds
        // DisplayName by that exact key; dropping it fails with
        // "provider name is required" even though the subtitle shows the name.
        // The key is encrypted by the Go side, not here.
        expect(payload.DisplayName).toBe('OpenAI');
        expect(payload.APIURL).toBe('https://api.x/v1');
        expect(payload.APIKey).toBe('sk-secret-value');
        expect(String(payload.KeyFingerprint)).not.toContain('sk-secret-value');
        expect(onShared).toHaveBeenCalledWith('share_1');
        expect(onClose).toHaveBeenCalled();
    });

    it('keeps the dialog open and surfaces the server error when sharing fails', async () => {
        vi.mocked(TokenBankCreateShare).mockRejectedValue(new Error('verify your account before sharing a provider'));
        const { onClose, onShared } = renderDialog();

        fireEvent.click(screen.getByText('确认分享'));

        await waitFor(() => expect(TokenBankCreateShare).toHaveBeenCalled());
        expect(onShared).not.toHaveBeenCalled();
        expect(onClose).not.toHaveBeenCalled();
        expect(showAlertMock).toHaveBeenCalledWith('verify your account before sharing a provider', '分享失败');
    });

    it('opens the existing verification flow when the account is unverified', async () => {
        vi.mocked(TokenBankCreateShare).mockRejectedValue(
            new Error('HubCenter rejected the request: {"code":"identity_not_verified","message":"verify your account before sharing a provider"}'),
        );
        const onRequestVerification = vi.fn();
        const { onClose, onShared } = renderDialog({}, vi.fn(), onRequestVerification);

        fireEvent.click(screen.getByText('确认分享'));

        await waitFor(() => expect(onRequestVerification).toHaveBeenCalledTimes(1));
        expect(showAlertMock).toHaveBeenCalledWith(
            'HubCenter rejected the request: {"code":"identity_not_verified","message":"verify your account before sharing a provider"}',
            '分享失败',
        );
        expect(showConfirmMock).toHaveBeenCalled();
        expect(onShared).not.toHaveBeenCalled();
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('stays on the share dialog when the user declines verification', async () => {
        showConfirmMock.mockResolvedValue(false);
        vi.mocked(TokenBankCreateShare).mockRejectedValue(new Error('verify your account before sharing a provider'));
        const onRequestVerification = vi.fn();
        const { onClose } = renderDialog({}, vi.fn(), onRequestVerification);

        fireEvent.click(screen.getByText('确认分享'));

        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        expect(onRequestVerification).not.toHaveBeenCalled();
        expect(onClose).not.toHaveBeenCalled();
    });

    it('does not offer verification for an unrelated share failure', async () => {
        vi.mocked(TokenBankCreateShare).mockRejectedValue(new Error('token bank unavailable'));
        const onRequestVerification = vi.fn();
        renderDialog({}, vi.fn(), onRequestVerification);

        fireEvent.click(screen.getByText('确认分享'));

        await waitFor(() => expect(showAlertMock).toHaveBeenCalled());
        expect(showConfirmMock).not.toHaveBeenCalled();
        expect(onRequestVerification).not.toHaveBeenCalled();
    });

    it('closes on Escape', () => {
        const { onClose } = renderDialog();
        fireEvent.keyDown(window, { key: 'Escape' });
        expect(onClose).toHaveBeenCalled();
    });

    it('does not announce new models on the first share', async () => {
        const probe = vi.fn().mockResolvedValue({ ok: true, latencyMs: 10 });
        renderDialog({}, probe, undefined, []);
        fireEvent.click(screen.getByText('开始探测'));
        await waitFor(() => expect(screen.getByText('2/2')).toBeTruthy());
        expect(screen.queryByText(/新模型/)).toBeNull();
    });

    it('imports only the models the local probe marks available', async () => {
        const probe = vi
            .fn()
            .mockResolvedValueOnce({ ok: true, latencyMs: 20 })
            .mockResolvedValueOnce({ ok: false, error: 'down' });
        renderDialog({}, probe);

        fireEvent.click(screen.getByText('一键导入全部可用模型'));

        await waitFor(() => expect(TokenBankCreateShare).toHaveBeenCalledTimes(1));
        expect(probe).toHaveBeenCalledTimes(2);
        const payload = vi.mocked(TokenBankCreateShare).mock.calls[0][0] as unknown as Record<string, unknown>;
        const models = payload.Models as Array<Record<string, unknown>>;
        expect(models).toHaveLength(1);
        expect(models[0]).toMatchObject({ model: 'model-a', available: true });
    });

    it('does not share or keep probing after the dialog closes', async () => {
        let resolveProbe: (value: { ok: boolean }) => void = () => {};
        const probe = vi.fn().mockImplementation(() => new Promise((resolve) => {
            resolveProbe = resolve;
        }));
        const { unmount } = renderDialog({}, probe);
        fireEvent.click(screen.getByText('一键导入全部可用模型'));
        await waitFor(() => expect(probe).toHaveBeenCalledTimes(1));
        unmount();
        await act(async () => {
            resolveProbe({ ok: true });
        });
        expect(probe).toHaveBeenCalledTimes(1);
        expect(TokenBankCreateShare).not.toHaveBeenCalled();
        expect(showAlertMock).not.toHaveBeenCalled();
    });

    it('sends a private audience as selected hub and tenant lists', async () => {
        renderDialog();
        expect(screen.getByText('公开').closest('label')?.className).toBe('tbk-visibility__option');
        expect(screen.getByLabelText('私有').closest('label')?.className).toBe('tbk-visibility__option');
        expect(screen.queryByLabelText('Hub 编号')).toBeNull();
        fireEvent.click(screen.getByLabelText('私有'));

        await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Beta' })).toBeTruthy());
        expect(screen.getByText('hub-b')).toBeTruthy();
        expect(screen.getByRole('checkbox', { name: '市场' })).toBeTruthy();
        fireEvent.click(screen.getByRole('checkbox', { name: 'Beta' }));
        fireEvent.click(screen.getByRole('checkbox', { name: 'hub-a' }));
        fireEvent.click(screen.getByRole('checkbox', { name: '市场' }));
        fireEvent.click(screen.getByText('确认分享'));

        await waitFor(() => expect(TokenBankCreateShare).toHaveBeenCalledTimes(1));
        const payload = vi.mocked(TokenBankCreateShare).mock.calls[0][0] as unknown as Record<string, unknown>;
        expect(payload.Visibility).toBe('private');
        expect(payload.HubIDs).toBe('hub-b, hub-a');
        expect(payload.TenantIDs).toBe('ten-1');
        expect(payload.SeparateAudiences).toBe(true);
    });

    it('does not submit a private share while hubs and tenants are still loading', async () => {
        vi.mocked(TokenBankListShareAudiences).mockReturnValue(new Promise(() => {}));
        renderDialog();
        fireEvent.click(screen.getByLabelText('私有'));
        fireEvent.click(screen.getByText('确认分享'));

        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('Hub 和租户还在加载。', 'Token 银行'));
        expect(TokenBankCreateShare).not.toHaveBeenCalled();
    });

    it('keeps a private selection when the list reloads with different id casing', async () => {
        vi.mocked(TokenBankListShareAudiences).mockReset();
        vi.mocked(TokenBankListShareAudiences)
            .mockResolvedValueOnce({
                hubs: [{ id: 'hub-b', name: 'Beta' }],
                tenants: [{ id: 'ten-1', name: '市场', hub_id: 'hub-b', hub_name: 'Beta' }],
            })
            .mockResolvedValueOnce({
                hubs: [{ id: 'Hub-B', name: 'Beta' }],
                tenants: [{ id: 'Ten-1', name: '市场', hub_id: 'Hub-B', hub_name: 'Beta' }],
            });
        renderDialog();
        fireEvent.click(screen.getByLabelText('私有'));
        await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Beta' })).toBeTruthy());
        fireEvent.click(screen.getByRole('checkbox', { name: 'Beta' }));
        fireEvent.click(screen.getByRole('checkbox', { name: '市场' }));
        fireEvent.click(screen.getByLabelText('公开'));
        fireEvent.click(screen.getByLabelText('私有'));

        await waitFor(() => expect(screen.getByText('Hub-B')).toBeTruthy());
        expect((screen.getByRole('checkbox', { name: 'Beta' }) as HTMLInputElement).checked).toBe(true);
        expect((screen.getByRole('checkbox', { name: '市场' }) as HTMLInputElement).checked).toBe(true);
        fireEvent.click(screen.getByText('确认分享'));

        await waitFor(() => expect(TokenBankCreateShare).toHaveBeenCalledTimes(1));
        const payload = vi.mocked(TokenBankCreateShare).mock.calls[0][0] as unknown as Record<string, unknown>;
        expect(payload.HubIDs).toBe('Hub-B');
        expect(payload.TenantIDs).toBe('Ten-1');
        expect(payload.SeparateAudiences).toBe(true);
    });

    it('asks the user to choose a hub or tenant before a private share', async () => {
        renderDialog();
        fireEvent.click(screen.getByLabelText('私有'));
        await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Beta' })).toBeTruthy());
        fireEvent.click(screen.getByText('确认分享'));

        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('私有分享至少要选择一个 hub 或租户。', 'Token 银行'));
        expect(TokenBankCreateShare).not.toHaveBeenCalled();
    });

    it('shows an empty picker when the account has no hubs or tenants', async () => {
        vi.mocked(TokenBankListShareAudiences).mockResolvedValue({ hubs: [], tenants: [] });
        renderDialog();
        fireEvent.click(screen.getByLabelText('私有'));

        await waitFor(() => expect(screen.getByText('没有可选择的 Hub')).toBeTruthy());
        expect(screen.getByText('没有可选择的租户')).toBeTruthy();
        expect(screen.queryByLabelText('Hub 编号')).toBeNull();
        expect(screen.queryByLabelText('租户编号')).toBeNull();
    });

    it('retries the hub and tenant list after a load error', async () => {
        vi.mocked(TokenBankListShareAudiences).mockReset();
        vi.mocked(TokenBankListShareAudiences)
            .mockRejectedValueOnce(new Error('HubCenter rejected the request: 404'))
            .mockResolvedValue({ hubs: [{ id: 'hub-a', name: 'Alpha' }], tenants: [] });
        renderDialog();
        fireEvent.click(screen.getByLabelText('私有'));

        await waitFor(() => expect(screen.getByText('HubCenter rejected the request: 404')).toBeTruthy());
        fireEvent.click(screen.getByText('重试'));
        await waitFor(() => expect(screen.getByRole('checkbox', { name: 'Alpha' })).toBeTruthy());
        expect(screen.getByText('没有可选择的租户')).toBeTruthy();
    });

    it('sends a per-model Beijing window when the owner narrows it', async () => {
        renderDialog();
        fireEvent.click(screen.getByRole('checkbox', { name: 'model-a 周六' }));
        fireEvent.click(screen.getByRole('checkbox', { name: 'model-a 周日' }));
        fireEvent.change(screen.getByLabelText('model-a 开始'), { target: { value: '22:00' } });
        fireEvent.change(screen.getByLabelText('model-a 结束'), { target: { value: '8:00' } });

        fireEvent.click(screen.getByText('确认分享'));
        await waitFor(() => expect(TokenBankCreateShare).toHaveBeenCalledTimes(1));
        const payload = vi.mocked(TokenBankCreateShare).mock.calls[0][0] as unknown as Record<string, unknown>;
        const models = payload.Models as Array<Record<string, unknown>>;
        expect(models[0].share_window).toEqual({ days: [1, 2, 3, 4, 5], start: '22:00', end: '08:00' });
        expect(models[1].share_window).toBeUndefined();
    });

    it('does not share a model whose clock cannot be read', async () => {
        renderDialog();
        fireEvent.change(screen.getByLabelText('model-b 结束'), { target: { value: '25:00' } });
        fireEvent.click(screen.getByText('确认分享'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalled());
        expect(TokenBankCreateShare).not.toHaveBeenCalled();
        expect(String(showAlertMock.mock.calls[0][0])).toContain('model-b');
    });

    it('announces probed models that are not already shared', async () => {
        const probe = vi.fn().mockResolvedValue({ ok: true, latencyMs: 10 });
        renderDialog({}, probe, undefined, ['model-a']);
        fireEvent.click(screen.getByText('开始探测'));
        await waitFor(() => expect(screen.getByText(/发现 1 个新模型/)).toBeTruthy());
    });

    it('saves an adjusted model range with the stored window and without creating a share', async () => {
        const adjustment: TokenBankShareAdjustment = {
            shareID: 'share_9',
            enabledModels: ['model-a'],
            windows: { 'model-a': { days: [1, 2, 3, 4, 5], start: '22:00', end: '08:00' } },
        };
        const onClose = vi.fn();
        const onShared = vi.fn();
        render(
            <TokenBankShareDialog
                lang="zh-Hans"
                request={baseRequest}
                probe={vi.fn()}
                onClose={onClose}
                onShared={onShared}
                adjustment={adjustment}
            />,
        );
        expect((screen.getByRole('checkbox', { name: 'model-a' }) as HTMLInputElement).checked).toBe(true);
        expect((screen.getByRole('checkbox', { name: 'model-b' }) as HTMLInputElement).checked).toBe(false);
        expect((screen.getByRole('checkbox', { name: 'model-a 周六' }) as HTMLInputElement).checked).toBe(false);
        expect((screen.getByRole('checkbox', { name: 'model-a 周一' }) as HTMLInputElement).checked).toBe(true);
        expect(screen.queryByText('谁可以调用')).toBeNull();
        expect(screen.queryByText('确认分享')).toBeNull();

        fireEvent.click(screen.getByRole('checkbox', { name: 'model-b' }));
        fireEvent.click(screen.getByText('保存模型范围'));
        await waitFor(() => expect(TokenBankSyncShareModels).toHaveBeenCalledTimes(1));
        expect(TokenBankCreateShare).not.toHaveBeenCalled();
        const synced = vi.mocked(TokenBankSyncShareModels).mock.calls[0][1] as unknown as Array<Record<string, unknown>>;
        expect(TokenBankSyncShareModels).toHaveBeenCalledWith('share_9', synced);
        expect(synced.map((entry) => entry.model)).toEqual(['model-a', 'model-b']);
        expect(synced[0].share_window).toEqual({ days: [1, 2, 3, 4, 5], start: '22:00', end: '08:00' });
        expect(synced[1].share_window).toBeUndefined();
        expect(onShared).toHaveBeenCalledWith('share_9');
        expect(onClose).toHaveBeenCalled();
    });
});
