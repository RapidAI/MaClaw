import { StrictMode } from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor, fireEvent, act } from '@testing-library/react';

const summaryMock = vi.fn();
const listSharesMock = vi.fn();
const listModelsMock = vi.fn();
const setPausedMock = vi.fn();
const takeOutMock = vi.fn();
const listWithdrawalsMock = vi.fn();
const getAutoSettingsMock = vi.fn();
const saveAutoSettingsMock = vi.fn();
const withdrawMock = vi.fn();
const listGiftsMock = vi.fn();
const createGiftMock = vi.fn();
const revokeGiftMock = vi.fn();
const previewGiftMock = vi.fn();
const claimGiftMock = vi.fn();
const withdrawGiftMock = vi.fn();
const rotateKeyMock = vi.fn();
const addKeyMock = vi.fn();
const setVisibilityMock = vi.fn();
const listAudiencesMock = vi.fn();
const listProvidersMock = vi.fn();
const fetchModelsMock = vi.fn();
const syncModelsMock = vi.fn();

vi.mock('../../../wailsjs/go/main/App', () => ({
    TokenBankSummary: (...args: unknown[]) => summaryMock(...args),
    TokenBankListShares: (...args: unknown[]) => listSharesMock(...args),
    TokenBankListShareModels: (...args: unknown[]) => listModelsMock(...args),
    TokenBankSetSharePaused: (...args: unknown[]) => setPausedMock(...args),
    TokenBankTakeOutShare: (...args: unknown[]) => takeOutMock(...args),
    TokenBankListWithdrawals: (...args: unknown[]) => listWithdrawalsMock(...args),
    TokenBankGetAutoSettings: (...args: unknown[]) => getAutoSettingsMock(...args),
    TokenBankSaveAutoSettings: (...args: unknown[]) => saveAutoSettingsMock(...args),
    TokenBankWithdraw: (...args: unknown[]) => withdrawMock(...args),
    TokenBankListGiftLinks: (...args: unknown[]) => listGiftsMock(...args),
    TokenBankCreateGiftLink: (...args: unknown[]) => createGiftMock(...args),
    TokenBankRevokeGiftLink: (...args: unknown[]) => revokeGiftMock(...args),
    TokenBankPreviewGiftLink: (...args: unknown[]) => previewGiftMock(...args),
    TokenBankClaimGiftLink: (...args: unknown[]) => claimGiftMock(...args),
    TokenBankWithdrawGift: (...args: unknown[]) => withdrawGiftMock(...args),
    TokenBankRotateShareKey: (...args: unknown[]) => rotateKeyMock(...args),
    TokenBankAddShareKey: (...args: unknown[]) => addKeyMock(...args),
    TokenBankSetShareVisibility: (...args: unknown[]) => setVisibilityMock(...args),
    TokenBankListShareAudiences: (...args: unknown[]) => listAudiencesMock(...args),
    GetMaclawLLMProviders: (...args: unknown[]) => listProvidersMock(...args),
    FetchProviderModels: (...args: unknown[]) => fetchModelsMock(...args),
    TokenBankSyncShareModels: (...args: unknown[]) => syncModelsMock(...args),
    ProbeMaclawLLMProviderModel: vi.fn(),
}));

// useDialog renders through a context provider that does not exist in a bare
// render(); stubbing the hook keeps this test about the panel's own behaviour.
const showConfirmMock = vi.fn();
const showAlertMock = vi.fn();
const showPromptMock = vi.fn();
vi.mock('../CustomDialog', () => ({
    useDialog: () => ({ showAlert: showAlertMock, showConfirm: showConfirmMock, showPrompt: showPromptMock }),
}));

import { takeOutShareDialog, TokenBankPanel } from '../TokenBankPanel';
import { formatGiftInstant } from '../../utils/hubcenterTokenBank';

const summaryFixture = {
    earned_micro: 3_000_000,
    received_micro: 500_000,
    withdrawn_micro: 1_000_000,
    granted_micro: 0,
    frozen_micro: 0,
    available_micro: 2_500_000,
    gift_share_cap_micro: 1_250_000,
    hub_count: 1,
    auto_withdraw_limit_micro: 2_500_000,
    gift_share_percent: 50,
    gift_link_ttl_seconds: 604800,
};

const shareFixture = {
    id: 'sh_1',
    display_name: 'My OpenAI',
    status: 'active',
    key_fingerprint: 'sk-...abcd',
    total_earned_micro: 3_000_000,
    last_error: '',
};

describe('TokenBankPanel', () => {
    beforeEach(() => {
        summaryMock.mockReset();
        listSharesMock.mockReset();
        listModelsMock.mockReset();
        setPausedMock.mockReset();
        takeOutMock.mockReset();
        listWithdrawalsMock.mockReset();
        getAutoSettingsMock.mockReset();
        saveAutoSettingsMock.mockReset();
        withdrawMock.mockReset();
        listGiftsMock.mockReset();
        createGiftMock.mockReset();
        revokeGiftMock.mockReset();
        previewGiftMock.mockReset();
        claimGiftMock.mockReset();
        withdrawGiftMock.mockReset();
        showConfirmMock.mockReset();
        showAlertMock.mockReset();
        showPromptMock.mockReset();
        rotateKeyMock.mockReset();
        rotateKeyMock.mockResolvedValue({ ok: true });
        addKeyMock.mockReset();
        setVisibilityMock.mockReset();
        setVisibilityMock.mockResolvedValue({ ok: true });
        listAudiencesMock.mockReset();
        listAudiencesMock.mockResolvedValue({ hubs: [], tenants: [] });
        listProvidersMock.mockReset();
        listProvidersMock.mockResolvedValue({ providers: [] });
        fetchModelsMock.mockReset();
        fetchModelsMock.mockResolvedValue([]);
        syncModelsMock.mockReset();
        syncModelsMock.mockResolvedValue({ id: 'sh_1' });

        summaryMock.mockResolvedValue(summaryFixture);
        listSharesMock.mockResolvedValue({ ok: true, shares: [shareFixture] });
        listWithdrawalsMock.mockResolvedValue({ withdrawals: [] });
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 0 });
        saveAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 0 });
        listGiftsMock.mockResolvedValue({ share_links: [] });
        setPausedMock.mockResolvedValue({ ok: true });
        takeOutMock.mockResolvedValue({ ok: true });
    });

    it('keeps the newer earnings window when the older load finishes last', async () => {
        const resolvers: Array<(value: unknown) => void> = [];
        listSharesMock.mockImplementation(() => new Promise((resolve) => {
            resolvers.push(resolve);
        }));
        const { findByText, getByRole, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(resolvers.length).toBe(1));
        fireEvent.click(getByRole('button', { name: '今日' }));
        await waitFor(() => expect(resolvers.length).toBe(2));
        expect(listSharesMock).toHaveBeenLastCalledWith('today');

        resolvers[1]({ shares: [{ ...shareFixture, id: 'sh_today', display_name: 'Today share' }] });
        expect(await findByText('Today share')).toBeTruthy();

        resolvers[0]({ shares: [{ ...shareFixture, id: 'sh_all', display_name: 'All share' }] });
        await waitFor(() => {
            expect(queryByText('All share')).toBeNull();
            expect(queryByText('Today share')).toBeTruthy();
        });
    });

    it('hides the previous window consumption until the new window loads', async () => {
        const resolvers: Array<() => void> = [];
        listSharesMock.mockImplementation((range: string) => new Promise((resolve) => {
            resolvers.push(() => resolve({
                shares: [{
                    ...shareFixture,
                    stats_ready: true,
                    today_earned_micro: 200_000,
                    month_earned_micro: 500_000,
                    all_earned_micro: 3_000_000,
                    range_gross_micro: range === 'today' ? 300_000 : 4_000_000,
                    range_tokens: range === 'today' ? 1_200 : 9_000,
                }],
            }));
        }));
        const { findByText, getByRole, getByText, queryByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(resolvers).toHaveLength(1));
        resolvers[0]();
        expect(await findByText(/Consumed: 4/)).toBeTruthy();

        fireEvent.click(getByRole('button', { name: 'Today' }));
        await waitFor(() => expect(resolvers).toHaveLength(2));
        expect(queryByText(/Consumed: 4/)).toBeNull();
        expect(queryByText(/Consumed: 0\.3/)).toBeNull();
        expect(getByText('Today +0.2')).toBeTruthy();

        resolvers[1]();
        expect(await findByText(/Consumed: 0\.3/)).toBeTruthy();
        expect(queryByText(/Consumed: 4/)).toBeNull();
    });

    it('hides the previous window model rows until that window loads', async () => {
        const resolvers: Array<() => void> = [];
        listModelsMock.mockImplementation((_id: string, range: string) => new Promise((resolve) => {
            resolvers.push(() => resolve({
                models: [{
                    id: 'm1',
                    model_name: range === 'today' ? 'today-model' : 'all-model',
                    tier: 'mid',
                    available: true,
                    earned_micro: 1_000_000,
                }],
            }));
        }));
        const { findByText, getByRole, getByText, queryByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Details')).toBeTruthy());
        fireEvent.click(getByText('Details'));
        await waitFor(() => expect(resolvers).toHaveLength(1));
        resolvers[0]();
        expect(await findByText('all-model')).toBeTruthy();

        fireEvent.click(getByRole('button', { name: 'Today' }));
        await waitFor(() => expect(resolvers).toHaveLength(2));
        expect(listModelsMock).toHaveBeenLastCalledWith('sh_1', 'today');
        expect(queryByText('all-model')).toBeNull();

        resolvers[1]();
        expect(await findByText('today-model')).toBeTruthy();
        expect(queryByText('all-model')).toBeNull();
    });

    it('shows a model load failure instead of an empty share or a spinner that stays up', async () => {
        listModelsMock.mockRejectedValue(new Error('models down'));
        const { findByText, getByText, queryByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Details')).toBeTruthy());
        fireEvent.click(getByText('Details'));
        expect(await findByText('Could not load models for this window.')).toBeTruthy();
        expect(queryByText('No models on this share.')).toBeNull();
        expect(queryByText('Loading…')).toBeNull();
    });

    it('keeps a closed share on its own window when another share reloads', async () => {
        listSharesMock.mockResolvedValue({
            shares: [
                { ...shareFixture, id: 'sh_a', display_name: 'Share A' },
                { ...shareFixture, id: 'sh_b', display_name: 'Share B' },
            ],
        });
        const resolvers: Array<() => void> = [];
        listModelsMock.mockImplementation((id: string, range: string) => new Promise((resolve) => {
            resolvers.push(() => resolve({
                models: [{
                    id: `${id}-${range}`,
                    model_name: `${id}-${range}`,
                    tier: 'mid',
                    available: true,
                    earned_micro: 1_000_000,
                }],
            }));
        }));
        const { findByText, getAllByText, getByRole, getByText, queryByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Share A')).toBeTruthy());

        fireEvent.click(getAllByText('Details')[0]);
        await waitFor(() => expect(resolvers).toHaveLength(1));
        resolvers[0]();
        expect(await findByText('sh_a-all')).toBeTruthy();
        fireEvent.click(getByText('Hide details'));

        fireEvent.click(getAllByText('Details')[1]);
        await waitFor(() => expect(resolvers).toHaveLength(2));
        resolvers[1]();
        expect(await findByText('sh_b-all')).toBeTruthy();

        fireEvent.click(getByRole('button', { name: 'Today' }));
        await waitFor(() => expect(resolvers).toHaveLength(3));
        expect(listModelsMock).toHaveBeenLastCalledWith('sh_b', 'today');
        resolvers[2]();
        expect(await findByText('sh_b-today')).toBeTruthy();

        fireEvent.click(getAllByText('Details')[0]);
        await waitFor(() => expect(resolvers).toHaveLength(4));
        expect(listModelsMock).toHaveBeenLastCalledWith('sh_a', 'today');
        expect(queryByText('sh_a-all')).toBeNull();
        resolvers[3]();
        expect(await findByText('sh_a-today')).toBeTruthy();
        expect(queryByText('sh_a-all')).toBeNull();
    });

    it('renders the balance card from the snake_case summary payload', async () => {
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => {
            expect(getByText('2.5')).toBeTruthy(); // available_micro 2_500_000
            expect(getByText('3')).toBeTruthy(); // earned_micro 3_000_000
            expect(getByText('My OpenAI')).toBeTruthy();
        });
    });

    it('shows Pause for an active share — the store literal is "active", not "available"', async () => {
        const { getByText, queryByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('My OpenAI')).toBeTruthy());
        // D27 regression: a status of "available" never matches, so every share
        // would render "Resume" even while live.
        expect(getByText('Pause')).toBeTruthy();
        expect(queryByText('Resume')).toBeNull();
    });

    it('shows Resume for a paused share', async () => {
        listSharesMock.mockResolvedValue({ shares: [{ ...shareFixture, status: 'paused' }] });
        const { getByText, queryByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('My OpenAI')).toBeTruthy());
        expect(getByText('Resume')).toBeTruthy();
        expect(queryByText('Pause')).toBeNull();
    });

    it('labels an automatic token-cap pause as undo and explains it in Chinese', async () => {
        const toast = vi.fn();
        listSharesMock.mockResolvedValue({
            shares: [{
                ...shareFixture,
                status: 'paused',
                paused_reason: 'token cap: anomaly',
                last_error: 'token cap: anomaly',
            }],
        });
        const { getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" showToastMessage={toast} />);
        await waitFor(() => expect(getByText('My OpenAI')).toBeTruthy());
        expect(getByText('撤销暂停')).toBeTruthy();
        expect(queryByText('恢复')).toBeNull();
        expect(queryByText('token cap: anomaly')).toBeNull();
        expect(getByText('今日用量曾超过前 7 日均值的 5 倍，因此暂停。该规则已取消，撤销暂停后会保持可用。')).toBeTruthy();
        expect(getByText('取出')).toBeTruthy();
        fireEvent.click(getByText('撤销暂停'));
        await waitFor(() => expect(setPausedMock).toHaveBeenCalledWith('sh_1', false));
        expect(takeOutMock).not.toHaveBeenCalled();
        expect(toast).toHaveBeenCalledWith('已撤销暂停。用量尖峰不会再次暂停。');
    });

    it('keeps 恢复 when a manual pause still carries an old cap note', async () => {
        listSharesMock.mockResolvedValue({
            shares: [{ ...shareFixture, status: 'paused', paused_reason: '', last_error: 'token cap: anomaly' }],
        });
        const { getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByText('My OpenAI')).toBeTruthy());
        expect(getByText('恢复')).toBeTruthy();
        expect(queryByText('撤销暂停')).toBeNull();
        expect(queryByText('token cap: anomaly')).toBeNull();
        expect(getByText('上次因今日用量超过前 7 日均值的 5 倍而暂停。该规则已取消，不会再次因此暂停。')).toBeTruthy();
    });

    it('keeps 恢复 for a manual pause and still shows an unrelated error', async () => {
        listSharesMock.mockResolvedValue({
            shares: [{ ...shareFixture, status: 'paused', paused_reason: '', last_error: 'upstream timeout' }],
        });
        const { getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByText('My OpenAI')).toBeTruthy());
        expect(getByText('恢复')).toBeTruthy();
        expect(queryByText('撤销暂停')).toBeNull();
        expect(getByText('upstream timeout')).toBeTruthy();
    });

    it('translates a leftover cap note on a live share without calling it paused', async () => {
        listSharesMock.mockResolvedValue({
            shares: [{ ...shareFixture, status: 'active', last_error: 'token cap: daily' }],
        });
        const { getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByText('My OpenAI')).toBeTruthy());
        expect(getByText('暂停')).toBeTruthy();
        expect(queryByText('撤销暂停')).toBeNull();
        expect(queryByText('token cap: daily')).toBeNull();
        expect(getByText('上次因达到每日 token 上限而暂停。仍超标的下一笔会再次暂停。')).toBeTruthy();
    });

    it('hides the pause switch entirely on a revoked share', async () => {
        listSharesMock.mockResolvedValue({ shares: [{ ...shareFixture, status: 'revoked' }] });
        const { getByText, queryByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('My OpenAI')).toBeTruthy());
        // A revoked share is gone; a switch on it is a button that always fails.
        expect(queryByText('Pause')).toBeNull();
        expect(queryByText('Resume')).toBeNull();
        expect(queryByText('Update key')).toBeNull();
        expect(queryByText('Adjust models')).toBeNull();
        expect(getByText('Taken out')).toBeTruthy();
    });

    it('updates the key of a live share and keeps the share id', async () => {
        listSharesMock.mockResolvedValue({
            shares: [{ ...shareFixture, api_url: 'https://api.example/v1', protocol: 'openai' }],
        });
        showPromptMock.mockResolvedValue('  sk-rotated  ');
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Update key')).toBeTruthy());
        fireEvent.click(getByText('Update key'));
        await waitFor(() => expect(rotateKeyMock).toHaveBeenCalled());
        const [shareID, apiURL, apiKey, protocol, fingerprint] = rotateKeyMock.mock.calls[0];
        expect(shareID).toBe('sh_1');
        expect(apiURL).toBe('https://api.example/v1');
        expect(apiKey).toBe('sk-rotated');
        expect(protocol).toBe('openai');
        expect(typeof fingerprint).toBe('string');
        expect(String(fingerprint).length).toBeGreaterThan(0);
    });

    it('changes access by choosing public or private instead of typing it', async () => {
        const { getByRole, getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Change access')).toBeTruthy());
        fireEvent.click(getByText('Change access'));
        const dialog = getByRole('dialog', { name: 'Change who can call this share' });
        expect(showPromptMock).not.toHaveBeenCalled();
        expect(dialog.querySelector('input[type="text"], textarea')).toBeNull();
        expect((getByRole('radio', { name: 'Public' }) as HTMLInputElement).checked).toBe(true);
        fireEvent.click(getByRole('button', { name: 'Cancel' }));
        expect(setVisibilityMock).not.toHaveBeenCalled();
        expect(dialog.isConnected).toBe(false);
    });

    it('leaves the share alone when the key prompt is cancelled', async () => {
        showPromptMock.mockResolvedValue(null);
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Update key')).toBeTruthy());
        fireEvent.click(getByText('Update key'));
        await waitFor(() => expect(showPromptMock).toHaveBeenCalled());
        expect(rotateKeyMock).not.toHaveBeenCalled();
    });

    it('expands models lazily on demand', async () => {
        listModelsMock.mockResolvedValue({ models: [{ id: 'm1', model_name: 'gpt-4o', tier: 'high', tier_multiplier: 2, available: true, earned_micro: 1_000_000 }] });
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Details')).toBeTruthy());
        expect(listModelsMock).not.toHaveBeenCalled();
        fireEvent.click(getByText('Details'));
        await waitFor(() => expect(getByText('gpt-4o')).toBeTruthy());
        expect(listModelsMock).toHaveBeenCalledWith('sh_1', 'all');
    });

    it('refetches shares for the selected earnings window', async () => {
        listSharesMock.mockImplementation(async (range: string) => ({
            shares: [{
                ...shareFixture,
                stats_ready: true,
                today_earned_micro: 200_000,
                month_earned_micro: 500_000,
                all_earned_micro: 3_000_000,
                range_earned_micro: range === 'today' ? 200_000 : 3_000_000,
                range_gross_micro: range === 'today' ? 300_000 : 4_000_000,
                range_tokens: range === 'today' ? 1200 : 9000,
            }],
        }));
        const { getByRole, getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => {
            expect(listSharesMock).toHaveBeenCalledWith('all');
            expect(getByText('Today +0.2')).toBeTruthy();
            expect(getByText('This month +0.5')).toBeTruthy();
            expect(getByText('All +3')).toBeTruthy();
        });
        fireEvent.click(getByRole('button', { name: 'Today' }));
        await waitFor(() => {
            expect(listSharesMock).toHaveBeenCalledWith('today');
            expect(getByText(/Consumed: 0\.3/)).toBeTruthy();
        });
    });

    it('shows the settled fee split and a model that is no longer on the share', async () => {
        listModelsMock.mockResolvedValue({
            models: [{
                id: 'm1',
                model_name: 'gpt-4o',
                tier: 'high',
                tier_multiplier: 2,
                available: true,
                usage: {
                    calls: 2,
                    gross_micro: 1_100_000,
                    fee_micro: 100_000,
                    net_micro: 1_000_000,
                    charged_micro: 1_200_000,
                    input_tokens: 12340,
                    output_tokens: 20,
                    clamped_calls: 1,
                    tokens: 12360,
                },
            }],
            usage_models: [{
                model_name: 'gpt-old',
                usage: {
                    calls: 1,
                    gross_micro: 200_000,
                    fee_micro: 20_000,
                    net_micro: 180_000,
                    charged_micro: 180_000,
                    input_tokens: 1,
                    output_tokens: 1,
                    clamped_calls: 0,
                    tokens: 2,
                },
            }],
        });
        const { getByText, getByTestId } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Details')).toBeTruthy());
        fireEvent.click(getByText('Details'));
        await waitFor(() => expect(getByTestId('tbk-model-calc-gpt-4o')).toBeTruthy());
        const calc = getByTestId('tbk-model-calc-gpt-4o').textContent || '';
        expect(calc).toContain('Consumed 1.1');
        expect(calc).toContain('fee 0.1');
        expect(calc).toContain('you earn 1');
        expect(calc).toContain('buyer paid 1.2');
        expect(calc).toContain('group price was lower');
        expect(getByText('gpt-old')).toBeTruthy();
        expect(getByTestId('tbk-range-formula').textContent).toContain('minus the fee');
    });

    it('pauses a live share by writing paused=true', async () => {
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Pause')).toBeTruthy());
        fireEvent.click(getByText('Pause'));
        await waitFor(() => expect(setPausedMock).toHaveBeenCalledWith('sh_1', true));
    });

    it('resumes a paused share by writing paused=false', async () => {
        listSharesMock.mockResolvedValue({ shares: [{ ...shareFixture, status: 'paused' }] });
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Resume')).toBeTruthy());
        fireEvent.click(getByText('Resume'));
        await waitFor(() => expect(setPausedMock).toHaveBeenCalledWith('sh_1', false));
    });

    it('takes a share out only after the operator confirms', async () => {
        showConfirmMock.mockResolvedValue(false);
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Take out')).toBeTruthy());
        fireEvent.click(getByText('Take out'));
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        const [message, title, options] = showConfirmMock.mock.calls[0];
        const copy = takeOutShareDialog('en', 'My OpenAI', 'sh_1');
        expect(message).toBe(copy.message);
        expect(title).toBe(copy.title);
        expect(options).toMatchObject({ confirmText: 'Take out', cancelText: 'Cancel', confirmVariant: 'danger' });
        // Declined: nothing may have been deleted.
        expect(takeOutMock).not.toHaveBeenCalled();
    });

    it('names the shared provider in the Chinese take-out confirm', async () => {
        showConfirmMock.mockResolvedValue(false);
        const { getByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByText('取出')).toBeTruthy());
        fireEvent.click(getByText('取出'));
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        const [message, title, options] = showConfirmMock.mock.calls[0];
        const copy = takeOutShareDialog('zh-Hans', 'My OpenAI', 'sh_1');
        expect(message).toBe(copy.message);
        expect(title).toBe(copy.title);
        expect(options).toMatchObject({ confirmText: '取出', cancelText: '取消', confirmVariant: 'danger' });
        expect(takeOutMock).not.toHaveBeenCalled();
    });

    it('puts the provider name on the confirm card even when the name has quotes', async () => {
        const { presentDialogMessage } = await vi.importActual<typeof import('../CustomDialog')>('../CustomDialog');
        expect(presentDialogMessage(takeOutShareDialog('zh-Hans', '智谱「编程」', 'sh_1').message)).toEqual({
            lead: '确定从银行取出分享？',
            subject: '智谱编程',
            follow: '其模型不再可被调用。',
            notice: '此操作不可撤销',
        });
        expect(presentDialogMessage(takeOutShareDialog('zh-Hans', '此操作不可撤销', 'sh_1').message).subject).toBe('sh_1');
        expect(presentDialogMessage(takeOutShareDialog('zh-Hans', '注意此操作不可撤销。', 'sh_1').message)).toMatchObject({
            subject: '注意。',
            notice: '此操作不可撤销',
        });
        expect(presentDialogMessage(takeOutShareDialog('en', '"', '"').message).subject).toBe('—');
        expect(presentDialogMessage(takeOutShareDialog('zh-Hant', '智谱编程', 'sh_1').message)).toEqual({
            lead: '確定從銀行取出分享？',
            subject: '智谱编程',
            follow: '其模型不再可被呼叫。',
            notice: '此操作不可撤銷',
        });
        expect(presentDialogMessage(takeOutShareDialog('en', 'My "OpenAI"\nkey', 'sh_1').message)).toEqual({
            lead: 'Take this share out of the bank?',
            subject: 'My OpenAI key',
            follow: 'Its models will stop being reachable.',
            notice: 'This cannot be undone',
        });
        expect(takeOutShareDialog('en', '   ', 'sh_1').message).toContain('"sh_1"');
    });

    it('takes a share out when confirmed', async () => {
        showConfirmMock.mockResolvedValue(true);
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Take out')).toBeTruthy());
        fireEvent.click(getByText('Take out'));
        await waitFor(() => expect(takeOutMock).toHaveBeenCalledWith('sh_1'));
    });

    it('withdraws the typed amount with manual=true, defaulting to the full balance when the cap is blank', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 25_000_000 });
        withdrawMock.mockResolvedValue({ ok: true });
        const { getByRole, findByRole, getByLabelText } = render(<TokenBankPanel lang="en" />);
        const open = await findByRole('button', { name: 'Withdraw to this machine' }) as HTMLButtonElement;
        await waitFor(() => expect(open.disabled).toBe(false));
        fireEvent.click(open);
        const dialog = await findByRole('dialog', { name: 'Withdraw to this machine' });
        expect(showConfirmMock).not.toHaveBeenCalled();
        expect(showPromptMock).not.toHaveBeenCalled();
        expect(showAlertMock).not.toHaveBeenCalled();
        const input = getByLabelText('Credits to withdraw') as HTMLInputElement;
        expect(input.value).toBe('25');
        fireEvent.click(getByRole('button', { name: /^Withdraw$/ }));
        await waitFor(() => expect(withdrawMock).toHaveBeenCalled());
        const [requestID, amountMicro, manual] = withdrawMock.mock.calls[0];
        expect(typeof requestID).toBe('string');
        expect(requestID.length).toBeGreaterThan(0);
        expect(amountMicro).toBe(25_000_000);
        expect(manual).toBe(true);
        await waitFor(() => expect(dialog.isConnected).toBe(false));
    });

    it('defaults the withdraw box to the automatic cap and accepts a larger or smaller amount', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 6_971_760_000 });
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 500_000_000 });
        withdrawMock.mockResolvedValue({ ok: true });
        const { getByText, findByLabelText, getByLabelText } = render(<TokenBankPanel lang="zh-Hans" />);
        const cap = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(cap.value).toBe('500'));
        fireEvent.click(getByText('提取到本机'));
        const input = getByLabelText('提取数量') as HTMLInputElement;
        await waitFor(() => expect(input.value).toBe('500'));

        fireEvent.change(input, { target: { value: '800' } });
        fireEvent.click(getByText('提取'));
        await waitFor(() => expect(withdrawMock).toHaveBeenCalled());
        expect(withdrawMock.mock.calls[0][1]).toBe(800_000_000);
        expect(withdrawMock.mock.calls[0][2]).toBe(true);
    });

    it('keeps a smaller withdraw inside the dialog and sends that amount', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 6_971_760_000 });
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 500_000_000 });
        withdrawMock.mockResolvedValue({ ok: true });
        const { getByText, findByLabelText, getByLabelText } = render(<TokenBankPanel lang="zh-Hans" />);
        const cap = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(cap.value).toBe('500'));
        fireEvent.click(getByText('提取到本机'));
        const input = getByLabelText('提取数量') as HTMLInputElement;
        fireEvent.change(input, { target: { value: '12' } });
        fireEvent.click(getByText('提取'));
        await waitFor(() => expect(withdrawMock).toHaveBeenCalled());
        expect(withdrawMock.mock.calls[0][1]).toBe(12_000_000);
    });

    it('accepts a pasted grouped balance and fullwidth digits', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 6_971_760_000 });
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 500_000_000 });
        withdrawMock.mockResolvedValue({ ok: true });
        const { getByRole, findByLabelText, getByLabelText } = render(<TokenBankPanel lang="zh-Hans" />);
        const cap = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(cap.value).toBe('500'));
        fireEvent.click(getByRole('button', { name: '提取到本机' }));
        const input = getByLabelText('提取数量') as HTMLInputElement;
        fireEvent.change(input, { target: { value: '６，９７１．７６' } });
        expect((getByRole('button', { name: /^提取$/ }) as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(getByRole('button', { name: /^提取$/ }));
        await waitFor(() => expect(withdrawMock).toHaveBeenCalled());
        expect(withdrawMock.mock.calls[0][1]).toBe(6_971_760_000);
    });

    it('refuses a withdraw below 10 or above the available balance', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 6_971_760_000 });
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 500_000_000 });
        const { findByLabelText, getByLabelText, getByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        const cap = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(cap.value).toBe('500'));
        fireEvent.click(getByRole('button', { name: '提取到本机' }));
        const input = getByLabelText('提取数量') as HTMLInputElement;
        const submit = getByRole('button', { name: /^提取$/ }) as HTMLButtonElement;

        fireEvent.change(input, { target: { value: '9.5' } });
        expect(submit.disabled).toBe(true);
        expect(getByRole('alert').textContent).toContain('最少提取 10 积分');
        fireEvent.click(submit);
        expect(withdrawMock).not.toHaveBeenCalled();

        fireEvent.change(input, { target: { value: '7000' } });
        expect(submit.disabled).toBe(true);
        expect(getByRole('alert').textContent).toContain('不能超过当前可用余额');
        expect(withdrawMock).not.toHaveBeenCalled();
        expect(showAlertMock).not.toHaveBeenCalled();
    });

    it('clamps the default to the balance when the automatic cap is higher', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 80_000_000 });
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 500_000_000 });
        const { getByText, findByLabelText, getByLabelText } = render(<TokenBankPanel lang="zh-Hans" />);
        const cap = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(cap.value).toBe('500'));
        fireEvent.click(getByText('提取到本机'));
        const input = getByLabelText('提取数量') as HTMLInputElement;
        expect(input.value).toBe('80');
    });

    it('keeps focus inside the dialog while a withdrawal is in flight', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 25_000_000 });
        withdrawMock.mockReturnValue(new Promise(() => {}));
        const { getByRole, findByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        const open = await findByRole('button', { name: '提取到本机' }) as HTMLButtonElement;
        await waitFor(() => expect(open.disabled).toBe(false));
        fireEvent.click(open);
        const dialog = await findByRole('dialog', { name: '提取到本机' });
        fireEvent.click(getByRole('button', { name: /^提取$/ }));
        await waitFor(() => expect(getByRole('button', { name: '提取中…' })).toBeTruthy());
        expect(document.activeElement).toBe(dialog);
        fireEvent.keyDown(dialog, { key: 'Tab' });
        expect(document.activeElement).toBe(dialog);
        expect(withdrawMock).toHaveBeenCalledTimes(1);
    });

    it('does not withdraw when the dialog is cancelled', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 25_000_000 });
        const { getByRole, findByRole, queryByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        const open = await findByRole('button', { name: '提取到本机' }) as HTMLButtonElement;
        await waitFor(() => expect(open.disabled).toBe(false));
        fireEvent.click(open);
        expect(await findByRole('dialog', { name: '提取到本机' })).toBeTruthy();
        expect(open.disabled).toBe(true);
        fireEvent.click(getByRole('button', { name: '取消' }));
        await waitFor(() => expect(queryByRole('dialog', { name: '提取到本机' })).toBeNull());
        expect(withdrawMock).not.toHaveBeenCalled();
    });

    it('reuses the request id when a withdraw fails and the user tries again', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 25_000_000 });
        withdrawMock.mockRejectedValueOnce(new Error('credits are reserved for this Hub; retry this withdrawal to finish the grant'));
        withdrawMock.mockResolvedValueOnce({ ok: true });
        const { getByRole, findByRole } = render(<TokenBankPanel lang="en" />);
        const open = await findByRole('button', { name: 'Withdraw to this machine' }) as HTMLButtonElement;
        await waitFor(() => expect(open.disabled).toBe(false));
        fireEvent.click(open);
        await findByRole('dialog', { name: 'Withdraw to this machine' });
        fireEvent.click(getByRole('button', { name: /^Withdraw$/ }));
        await waitFor(() => {
            expect(withdrawMock).toHaveBeenCalledTimes(1);
            expect(getByRole('alert').textContent).toContain('credits are reserved');
            expect((getByRole('button', { name: /^Withdraw$/ }) as HTMLButtonElement).disabled).toBe(false);
        });
        fireEvent.click(getByRole('button', { name: /^Withdraw$/ }));
        await waitFor(() => expect(withdrawMock).toHaveBeenCalledTimes(2));
        expect(withdrawMock.mock.calls[0][0]).toBe(withdrawMock.mock.calls[1][0]);
        expect(withdrawMock.mock.calls[0][1]).toBe(withdrawMock.mock.calls[1][1]);
    });

    it('keeps the saved automatic cap when the field has an unsaved draft', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 6_971_760_000 });
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 500_000_000 });
        const { getByRole, findByLabelText, getByLabelText } = render(<TokenBankPanel lang="zh-Hans" />);
        const cap = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(cap.value).toBe('500'));
        fireEvent.change(cap, { target: { value: '80' } });
        fireEvent.click(getByRole('button', { name: '提取到本机' }));
        expect((getByLabelText('提取数量') as HTMLInputElement).value).toBe('500');
    });

    it('refuses a different amount after an unconfirmed withdrawal', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 6_971_760_000 });
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 500_000_000 });
        withdrawMock.mockRejectedValueOnce(new Error('credits are reserved for this Hub; retry this withdrawal to finish the grant'));
        withdrawMock.mockResolvedValueOnce({ ok: true });
        const { getByRole, findByLabelText, getByLabelText } = render(<TokenBankPanel lang="zh-Hans" />);
        const cap = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(cap.value).toBe('500'));
        fireEvent.click(getByRole('button', { name: '提取到本机' }));
        const input = getByLabelText('提取数量') as HTMLInputElement;
        const submit = () => getByRole('button', { name: /^提取$/ }) as HTMLButtonElement;
        fireEvent.click(submit());
        await waitFor(() => expect(withdrawMock).toHaveBeenCalledTimes(1));
        await waitFor(() => expect(submit().disabled).toBe(false));

        fireEvent.change(input, { target: { value: '12' } });
        expect(submit().disabled).toBe(true);
        expect(getByRole('alert').textContent).toContain('尚未确认');
        fireEvent.click(submit());
        expect(withdrawMock).toHaveBeenCalledTimes(1);

        fireEvent.change(input, { target: { value: '500' } });
        expect(submit().disabled).toBe(false);
        fireEvent.click(submit());
        await waitFor(() => expect(withdrawMock).toHaveBeenCalledTimes(2));
        expect(withdrawMock.mock.calls[0][0]).toBe(withdrawMock.mock.calls[1][0]);
        expect(withdrawMock.mock.calls[1][1]).toBe(500_000_000);
    });

    it('allows another amount after the hub refuses before any debit', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 6_971_760_000 });
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 500_000_000 });
        withdrawMock.mockRejectedValueOnce(new Error('Hub rejected the withdrawal: {"code":"SERVICE_GROUP_MISSING"}'));
        withdrawMock.mockResolvedValueOnce({ ok: true });
        const { getByRole, findByLabelText, getByLabelText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        const cap = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(cap.value).toBe('500'));
        fireEvent.click(getByRole('button', { name: '提取到本机' }));
        const input = getByLabelText('提取数量') as HTMLInputElement;
        fireEvent.click(getByRole('button', { name: /^提取$/ }));
        await waitFor(() => expect(withdrawMock).toHaveBeenCalledTimes(1));
        fireEvent.change(input, { target: { value: '12' } });
        await waitFor(() => expect(queryByText(/尚未确认/)).toBeNull());
        fireEvent.click(getByRole('button', { name: /^提取$/ }));
        await waitFor(() => expect(withdrawMock).toHaveBeenCalledTimes(2));
        expect(withdrawMock.mock.calls[0][0]).not.toBe(withdrawMock.mock.calls[1][0]);
        expect(withdrawMock.mock.calls[1][1]).toBe(12_000_000);
    });

    it('reopens an unfinished withdrawal after the balance has already been debited', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 500_000_000 });
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 500_000_000 });
        withdrawMock.mockRejectedValueOnce(new Error(
            'credits are reserved for this Hub; retry this withdrawal to finish the grant: Hub rejected the withdrawal: {"code":"GRANT_PENDING","message":"token bank grant was not confirmed; retry the same request id"}',
        ));
        withdrawMock.mockResolvedValueOnce({ ok: true });
        const { getByRole, findByLabelText, getByLabelText } = render(<TokenBankPanel lang="zh-Hans" />);
        const cap = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(cap.value).toBe('500'));
        fireEvent.click(getByRole('button', { name: '提取到本机' }));
        fireEvent.click(getByRole('button', { name: /^提取$/ }));
        await waitFor(() => expect(getByRole('alert').textContent).toContain('retry the same request id'));
        expect(getByRole('alert').textContent).not.toContain('"code"');
        fireEvent.click(getByRole('button', { name: '取消' }));

        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 0 });
        fireEvent.click(getByRole('button', { name: '刷新' }));
        const open = await findByLabelText('每次自动提取上限').then(() => getByRole('button', { name: '提取到本机' }) as HTMLButtonElement);
        await waitFor(() => expect(open.disabled).toBe(false));
        fireEvent.click(open);
        expect((getByLabelText('提取数量') as HTMLInputElement).value).toBe('500');
        fireEvent.click(getByRole('button', { name: /^提取$/ }));
        await waitFor(() => expect(withdrawMock).toHaveBeenCalledTimes(2));
        expect(withdrawMock.mock.calls[0][0]).toBe(withdrawMock.mock.calls[1][0]);
        expect(withdrawMock.mock.calls[1][1]).toBe(500_000_000);
    });

    it('explains that a balance under 10 credits cannot be withdrawn', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, available_micro: 2_500_000 });
        const { getByRole, findByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        const open = await findByRole('button', { name: '提取到本机' }) as HTMLButtonElement;
        await waitFor(() => expect(open.disabled).toBe(false));
        fireEvent.click(open);
        expect(await findByRole('alert')).toBeTruthy();
        expect(getByRole('alert').textContent).toContain('可用积分不足 10');
        expect((getByRole('button', { name: /^提取$/ }) as HTMLButtonElement).disabled).toBe(true);
        expect(withdrawMock).not.toHaveBeenCalled();
    });

    it('surfaces the sign-in error from the backend verbatim', async () => {
        summaryMock.mockRejectedValue(new Error('please sign in to HubCenter to use the Token Bank'));
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('please sign in to HubCenter to use the Token Bank')).toBeTruthy());
    });

    it('renders the empty state once a load has completed with no shares', async () => {
        listSharesMock.mockResolvedValue({ shares: [] });
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('You have not shared any model yet.')).toBeTruthy());
    });

    it('creates a whole-credit gift link and shows the claim URL once', async () => {
        createGiftMock.mockResolvedValue({
            id: 'gift_1',
            code: 'SECRETCODE',
            credits_micro: 1_000_000,
            status: 'active',
            claim_url: 'https://hub.example/c/SECRETCODE',
        });
        const { getByText, getByLabelText, getByTestId } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Gift credits')).toBeTruthy());
        fireEvent.click(getByText('Gift credits'));
        fireEvent.change(getByLabelText('Credits to gift'), { target: { value: '1' } });
        fireEvent.click(getByText('Create link'));
        await waitFor(() => expect(createGiftMock).toHaveBeenCalledWith(1, 0));
        const ticket = await waitFor(() => getByTestId('tbk-gift-ticket'));
        expect(ticket.textContent).toContain('Gift 1 credits');
        expect(ticket.textContent).toContain('https://hub.example/c/SECRETCODE');
        expect(ticket.querySelector('svg')).toBeTruthy();
    });

    it('shows who claimed a gift and whether they have withdrawn it', async () => {
        listGiftsMock.mockResolvedValue({
            share_links: [
                {
                    id: 'gift_held',
                    credits_micro: 17_140_000,
                    status: 'claimed',
                    claimed: true,
                    claimed_by_email: 'pat@example.com',
                    claimed_by_user_id: 'user-pat',
                    claimed_at: '2026-10-03T01:02:03Z',
                    expires_at: '2026-10-10T00:02:17Z',
                },
                {
                    id: 'gift_due',
                    credits_micro: 500_000,
                    status: 'claimed',
                    claimed: true,
                    claimed_by_email: 'late@example.com',
                    claimed_at: '2020-01-01T00:00:00Z',
                    expires_at: '2020-01-02T00:00:00Z',
                },
                {
                    id: 'gift_back',
                    credits_micro: 250_000,
                    status: 'expired',
                    claimed: true,
                    claimed_by_email: 'old@example.com',
                    claimed_at: '2020-01-01T00:00:00Z',
                    expires_at: '2020-01-08T00:00:00Z',
                },
                {
                    id: 'gift_done',
                    credits_micro: 1_000_000,
                    status: 'settled',
                    claimed: true,
                    claimed_by_email: 'sam@example.com',
                    claimed_at: '2026-10-02T00:00:00Z',
                },
                {
                    id: 'gift_revoked',
                    credits_micro: 17_140_000,
                    status: 'revoked',
                    claimed: true,
                    claimed_by_email: 'phone:17090134628',
                    claimed_at: '2026-10-03T00:19:00Z',
                    expires_at: '2026-10-10T00:02:00Z',
                },
                {
                    id: 'gift_shut',
                    credits_micro: 100_000,
                    status: 'active',
                    expires_at: '2020-06-01T00:00:00Z',
                },
            ],
        });
        const { getByTestId } = render(<TokenBankPanel lang="zh-Hans" />);
        const held = await waitFor(() => getByTestId('tbk-gift-claim-gift_held'));
        expect(held.textContent).toContain('领取人 pat@example.com');
        expect(held.textContent).toContain(`领取时间 ${formatGiftInstant('2026-10-03T01:02:03Z')}`);
        expect(held.textContent).toContain('积分仍冻结');
        expect(held.textContent).toContain(`退回时间 ${formatGiftInstant('2026-10-10T00:02:17Z')}`);
        expect(held.textContent).not.toContain('2026-10-10T00:02:17Z');
        const due = getByTestId('tbk-gift-claim-gift_due');
        expect(due.textContent).toContain('已过退回时间，积分尚未退回');
        expect(due.textContent).toContain('积分退回前对方仍可提取');
        expect(due.textContent).not.toContain(`退回时间 ${formatGiftInstant('2020-01-02T00:00:00Z')}`);
        const back = getByTestId('tbk-gift-claim-gift_back');
        expect(back.textContent).toContain('领取人 old@example.com');
        expect(back.textContent).toContain('对方未提取，积分已退回');
        const done = getByTestId('tbk-gift-claim-gift_done');
        expect(done.textContent).toContain('领取人 sam@example.com');
        expect(done.textContent).toContain('对方已提取到本机');
        const revoked = getByTestId('tbk-gift-claim-gift_revoked');
        expect(revoked.textContent).toContain('领取人 phone:17090134628');
        expect(revoked.textContent).toContain('你已撤销这笔转赠，积分已退回');
        expect(revoked.textContent).not.toContain('退回时间');
        expect(getByTestId('tbk-gift-gift_revoked').querySelector('button')).toBeNull();
        const list = getByTestId('tbk-gift-list');
        expect(list.classList.contains('tbk-gift-grid')).toBe(true);
        expect(Array.from(list.children).every((card) => card.classList.contains('tbk-share'))).toBe(true);
        expect(list.textContent).toContain('未提取');
        expect(list.textContent).toContain('已提取');
        const shut = getByTestId('tbk-gift-gift_shut');
        expect(shut.textContent).toContain('待退回');
        expect(shut.textContent).not.toContain('待领取');
        expect(shut.textContent).toContain('已过到期时间，积分尚未退回');
        expect(shut.textContent).toContain('现在已不能领取');
        expect(shut.textContent).not.toContain(`到期 ${formatGiftInstant('2020-06-01T00:00:00Z')}`);
        expect(shut.textContent).toContain('撤销');
        expect(shut.querySelector('.tbk-pill')?.className).toContain('tbk-pill--wait');
        expect(shut.querySelector('.tbk-pill')?.className).not.toContain('tbk-pill--on');
    });

    it('lists gift links I sent as twenty cards a page', async () => {
        listGiftsMock.mockResolvedValue({
            share_links: Array.from({ length: 21 }, (_, index) => ({
                id: `gift_${index + 1}`,
                credits_micro: (index + 1) * 1_000_000,
                status: 'active',
                expires_at: '2026-10-08T00:00:00Z',
            })),
        });
        const { getByRole, getByTestId, queryByTestId, queryByText } = render(<TokenBankPanel lang="en" />);
        const list = await waitFor(() => getByTestId('tbk-gift-list'));
        expect(list.classList.contains('tbk-gift-grid')).toBe(true);
        expect(list.querySelectorAll(':scope > li.tbk-share')).toHaveLength(20);
        expect(getByTestId('tbk-gift-gift_1')).toBeTruthy();
        expect(queryByTestId('tbk-gift-gift_21')).toBeNull();
        expect(getByTestId('tbk-gift-pager').textContent).toContain('Gift links, page 1 of 2');

        fireEvent.click(getByRole('button', { name: 'Next gift page' }));
        await waitFor(() => expect(getByTestId('tbk-gift-gift_21')).toBeTruthy());
        expect(queryByTestId('tbk-gift-gift_1')).toBeNull();
        expect(list.querySelectorAll(':scope > li.tbk-share')).toHaveLength(1);
        expect(getByTestId('tbk-gift-pager').textContent).toContain('Gift links, page 2 of 2');

        fireEvent.click(getByRole('button', { name: 'Previous gift page' }));
        await waitFor(() => expect(getByTestId('tbk-gift-gift_1')).toBeTruthy());
        expect(queryByTestId('tbk-gift-gift_21')).toBeNull();

        fireEvent.click(getByRole('button', { name: 'Next gift page' }));
        await waitFor(() => expect(getByTestId('tbk-gift-pager').textContent).toContain('page 2 of 2'));
        listGiftsMock.mockResolvedValue({
            share_links: [{ id: 'gift_only', credits_micro: 1_000_000, status: 'active' }],
        });
        fireEvent.click(getByRole('button', { name: 'Refresh' }));
        await waitFor(() => expect(getByTestId('tbk-gift-gift_only')).toBeTruthy());
        expect(queryByTestId('tbk-gift-gift_21')).toBeNull();
        expect(queryByText('Gift links, page 2 of 2')).toBeNull();
        expect(queryByTestId('tbk-gift-pager')).toBeNull();
        expect(list.querySelectorAll(':scope > li.tbk-share')).toHaveLength(1);
    });

    it('returns to the first gift page when a new link is created', async () => {
        const sent = Array.from({ length: 21 }, (_, index) => ({
            id: `gift_${index + 1}`,
            credits_micro: 1_000_000,
            status: 'active',
            expires_at: '2026-10-08T00:00:00Z',
        }));
        listGiftsMock.mockResolvedValue({ share_links: sent });
        createGiftMock.mockResolvedValue({
            id: 'gift_new',
            code: 'NEWCODE',
            credits_micro: 1_000_000,
            status: 'active',
            claim_url: 'https://hub.example/c/NEWCODE',
        });
        const { getByLabelText, getByRole, getByTestId, getByText, queryByTestId } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByTestId('tbk-gift-pager').textContent).toContain('page 1 of 2'));
        fireEvent.click(getByRole('button', { name: 'Next gift page' }));
        await waitFor(() => expect(getByTestId('tbk-gift-gift_21')).toBeTruthy());

        listGiftsMock.mockResolvedValue({
            share_links: [{ id: 'gift_new', credits_micro: 1_000_000, status: 'active', expires_at: '2026-10-08T00:00:00Z' }, ...sent],
        });
        fireEvent.click(getByText('Gift credits'));
        fireEvent.change(getByLabelText('Credits to gift'), { target: { value: '1' } });
        fireEvent.click(getByText('Create link'));
        await waitFor(() => expect(getByTestId('tbk-gift-gift_new')).toBeTruthy());
        expect(queryByTestId('tbk-gift-gift_21')).toBeNull();
        expect(getByTestId('tbk-gift-pager').textContent).toContain('Gift links, page 1 of 2');
    });

    it('does not put the claim code on the owner list', async () => {
        listGiftsMock.mockResolvedValue({
            share_links: [{ id: 'gift_1', credits_micro: 1_000_000, status: 'active', expires_at: '2026-10-08T00:00:00Z' }],
        });
        const { getByTestId, queryByText } = render(<TokenBankPanel lang="en" />);
        const list = await waitFor(() => getByTestId('tbk-gift-list'));
        expect(list.textContent).toContain('Waiting to be claimed');
        expect(list.textContent).not.toContain('SECRETCODE');
        expect(queryByText('https://hub.example/c/SECRETCODE')).toBeNull();
    });

    it('refuses a gift above the share cap before calling the server', async () => {
        const { getByText, getByLabelText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Gift credits')).toBeTruthy());
        fireEvent.click(getByText('Gift credits'));
        fireEvent.change(getByLabelText('Credits to gift'), { target: { value: '10' } });
        fireEvent.click(getByText('Create link'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            expect.stringContaining('gift at most'),
            'Gift credits',
        ));
        expect(createGiftMock).not.toHaveBeenCalled();
    });

    it('sends the fractional cap as credits_micro when sharing the maximum', async () => {
        createGiftMock.mockResolvedValue({
            id: 'gift_max',
            code: 'MAXCODE',
            credits_micro: 1_250_000,
            status: 'active',
            claim_url: 'https://hub.example/c/MAXCODE',
        });
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Gift credits')).toBeTruthy());
        fireEvent.click(getByText('Gift credits'));
        fireEvent.click(getByText('Gift the maximum'));
        fireEvent.click(getByText('Create link'));
        await waitFor(() => expect(createGiftMock).toHaveBeenCalledWith(0, 1_250_000));
    });

    it('revokes an active link only after confirmation', async () => {
        listGiftsMock.mockResolvedValue({
            share_links: [{ id: 'gift_1', credits_micro: 1_000_000, status: 'active' }],
        });
        showConfirmMock.mockResolvedValue(false);
        const { getByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Revoke')).toBeTruthy());
        fireEvent.click(getByText('Revoke'));
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        expect(revokeGiftMock).not.toHaveBeenCalled();

        showConfirmMock.mockResolvedValue(true);
        fireEvent.click(getByText('Revoke'));
        await waitFor(() => expect(revokeGiftMock).toHaveBeenCalledWith('gift_1'));
    });

    it('revokes a claimed gift that has not been withdrawn', async () => {
        listGiftsMock.mockResolvedValue({
            share_links: [
                {
                    id: 'gift_held',
                    credits_micro: 17_140_000,
                    status: 'claimed',
                    claimed: true,
                    claimed_by_email: 'phone:17090134628',
                    claimed_at: '2026-10-03T00:19:00Z',
                    expires_at: '2026-10-10T00:02:00Z',
                },
                {
                    id: 'gift_done',
                    credits_micro: 1_000_000,
                    status: 'settled',
                    claimed: true,
                    claimed_by_email: 'sam@example.com',
                },
            ],
        });
        showConfirmMock.mockResolvedValue(false);
        const { getByTestId, getByText } = render(<TokenBankPanel lang="zh-Hans" />);
        const held = await waitFor(() => getByTestId('tbk-gift-gift_held'));
        expect(held.textContent).toContain('未提取');
        expect(held.textContent).toContain('撤销');
        expect(getByTestId('tbk-gift-gift_done').textContent).not.toContain('撤销');
        fireEvent.click(getByText('撤销'));
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalled());
        const [message, title] = showConfirmMock.mock.calls[0];
        expect(String(title)).toContain('撤销这笔转赠');
        expect(String(message)).toContain('尚未提取');
        expect(String(message)).toContain(held.querySelector('.tbk-share__name')?.textContent || '17.14');
        expect(revokeGiftMock).not.toHaveBeenCalled();

        showConfirmMock.mockResolvedValue(true);
        fireEvent.click(getByText('撤销'));
        await waitFor(() => expect(revokeGiftMock).toHaveBeenCalledWith('gift_held'));
    });

    it('drops a held gift when the sender has revoked it', async () => {
        listGiftsMock
            .mockResolvedValueOnce({
                share_links: [],
                claimed_links: [{
                    id: 'gift_held',
                    credits_micro: 17_140_000,
                    status: 'claimed',
                    sender_masked: 'a***@x.com',
                }],
            })
            .mockResolvedValue({ share_links: [], claimed_links: [] });
        withdrawGiftMock.mockRejectedValue(new Error(
            'Hub rejected the withdrawal: {"code":"TOKEN_BANK_WITHDRAW_FAILED","message":"hub center token bank: gift_revoked: the sender revoked this gift"}',
        ));
        showConfirmMock.mockResolvedValue(true);
        const { findByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        fireEvent.click(await findByText('提取这份转赠'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('发送方已撤销这份转赠。', '提取失败'));
        await waitFor(() => expect(queryByText('提取这份转赠')).toBeNull());
    });

    it('claims from a pasted URL and withdraws that gift amount', async () => {
        previewGiftMock.mockResolvedValue({
            code: 'SECRETCODE',
            sender_masked: 'a***@x.com',
            credits_micro: 2_000_000,
            status: 'active',
            claimable: true,
        });
        claimGiftMock.mockResolvedValue({
            id: 'gift_9',
            code: 'SECRETCODE',
            credits_micro: 2_000_000,
            status: 'claimed',
        });
        withdrawGiftMock.mockRejectedValueOnce(new Error('Hub rejected the withdrawal'));
        withdrawGiftMock.mockResolvedValueOnce({ ok: true });
        showConfirmMock.mockResolvedValue(true);
        const { getByText, getByLabelText, getByRole, queryByText } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByLabelText('Gift code or link')).toBeTruthy());
        fireEvent.change(getByLabelText('Gift code or link'), { target: { value: 'https://hub.example/c/SECRETCODE' } });
        fireEvent.click(getByText('Preview'));
        await waitFor(() => expect(previewGiftMock).toHaveBeenCalledWith('SECRETCODE'));
        expect(getByText(/a\*\*\*@x.com/)).toBeTruthy();
        expect(getByRole('status').textContent).toContain('This link can be claimed.');
        fireEvent.click(getByText('Claim'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            'Claimed. Withdraw the gift to this machine to receive the credits.',
            'Claimed',
        ));
        expect(getByRole('status').textContent).toContain('Claimed. Withdraw the gift to this machine');
        expect(queryByText(/a\*\*\*@x.com/)).toBeNull();
        fireEvent.click(getByText('Withdraw this gift'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            'Hub rejected the withdrawal',
            'Withdrawal failed',
        ));
        expect(getByRole('status').textContent).toContain('Hub rejected the withdrawal');
        fireEvent.click(getByText('Withdraw this gift'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            'Gift withdrawn to this machine.',
            'Withdrawn',
        ));
        expect(withdrawGiftMock).toHaveBeenCalledTimes(2);
        const [firstID, firstLink, firstAmount] = withdrawGiftMock.mock.calls[0];
        const [secondID, secondLink, secondAmount] = withdrawGiftMock.mock.calls[1];
        expect(firstID).toBe(secondID);
        expect(firstLink).toBe('gift_9');
        expect(secondLink).toBe('gift_9');
        expect(firstAmount).toBe(2_000_000);
        expect(secondAmount).toBe(2_000_000);
    });

    it('finishes an unbound gift withdrawal with the stored request id', async () => {
        listWithdrawalsMock.mockResolvedValue({
            withdrawals: [
                {
                    id: 'w1',
                    request_id: 'req-server',
                    link_id: 'gift_held',
                    amount_micro: 17_140_000,
                    kind: 'gift',
                    state: 'issued',
                    grant_id: '',
                },
                {
                    id: 'w2',
                    request_id: 'req-done',
                    link_id: 'gift_done',
                    amount_micro: 1_000_000,
                    kind: 'gift',
                    state: 'bound',
                    grant_id: 'grant-1',
                },
                {
                    id: 'w3',
                    request_id: 'req-self',
                    amount_micro: 2_000_000,
                    kind: 'self',
                    state: 'issued',
                    grant_id: '',
                },
            ],
        });
        showConfirmMock.mockResolvedValue(true);
        withdrawGiftMock.mockResolvedValue({ ok: true });
        const { findByText, queryAllByText } = render(<TokenBankPanel lang="en" />);
        fireEvent.click(await findByText('Finish this withdrawal'));
        await waitFor(() => expect(withdrawGiftMock).toHaveBeenCalledWith('req-server', 'gift_held', 17_140_000));
        expect(queryAllByText('Finish this withdrawal')).toHaveLength(1);
    });

    it('opens one confirm when finish is clicked twice', async () => {
        listWithdrawalsMock.mockResolvedValue({
            withdrawals: [{
                id: 'w-gift',
                request_id: 'req-gift',
                amount_micro: 17_140_000,
                kind: 'gift',
                link_id: 'gift_held',
                state: 'issued',
                grant_id: '',
            }],
        });
        showConfirmMock.mockReturnValue(new Promise(() => {}));
        const { findByRole } = render(<TokenBankPanel lang="en" />);
        const button = await findByRole('button', { name: 'Finish withdrawal of 17.14' });
        fireEvent.click(button);
        fireEvent.click(button);
        await waitFor(() => expect(showConfirmMock).toHaveBeenCalledTimes(1));
        expect(withdrawGiftMock).not.toHaveBeenCalled();
    });

    it('says why a gift cannot be claimed when preview or claim is clicked', async () => {
        previewGiftMock.mockResolvedValue({
            code: 'LJNMLC640I',
            sender_masked: '3***@qq.com',
            credits_micro: 17_140_000,
            status: 'expired',
            claimable: false,
        });
        const { container, getByLabelText, getByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC640I' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('这份转赠已过期，积分已退回。', '不能领取'));
        await waitFor(() => expect((getByText('查验').closest('button') as HTMLButtonElement).disabled).toBe(false));
        const previewErrors = () => container.querySelectorAll('.tbk-gift-claim .tbk-error');
        expect(previewErrors()).toHaveLength(1);
        expect(previewErrors()[0].textContent).toContain('3***@qq.com');
        expect(previewErrors()[0].textContent).toContain('这份转赠已过期，积分已退回。');
        showAlertMock.mockClear();
        fireEvent.click(getByText('领取'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('这份转赠已过期，积分已退回。', '不能领取'));
        expect(claimGiftMock).not.toHaveBeenCalled();
        expect(previewErrors()).toHaveLength(1);
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: '  ' } });
        expect(previewErrors()).toHaveLength(0);
        showAlertMock.mockClear();
        fireEvent.click(getByText('领取'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('请粘贴兑换码或链接。', '领取转赠'));
        expect(claimGiftMock).not.toHaveBeenCalled();
    });

    it('does not say a claimed gift was returned before its status changes', async () => {
        previewGiftMock.mockResolvedValue({
            code: 'LJNMLC640I',
            sender_masked: '3***@qq.com',
            credits_micro: 17_140_000,
            status: 'claimed',
            claimable: false,
            expires_at: '2020-01-02T00:00:00Z',
        });
        const { getByLabelText, getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC640I' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('这份转赠已经被领取。', '不能领取'));
        expect(queryByText(/积分已退回/)).toBeNull();
        expect(claimGiftMock).not.toHaveBeenCalled();
    });

    it('does not say an unclaimed gift was returned before the sweep', async () => {
        previewGiftMock.mockResolvedValue({
            code: 'LJNMLC640I',
            sender_masked: '3***@qq.com',
            credits_micro: 17_140_000,
            status: 'active',
            claimable: false,
            expires_at: '2020-01-02T00:00:00Z',
        });
        const { getByLabelText, getByText, getByRole, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC640I' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            '已过到期时间，积分尚未退回。现在已不能领取。',
            '不能领取',
        ));
        const note = getByRole('status');
        expect(note.className).toContain('tbk-error');
        expect(note.textContent).toContain('已过到期时间，积分尚未退回。现在已不能领取。');
        expect(queryByText(/这份转赠已过期/)).toBeNull();
        showAlertMock.mockClear();
        fireEvent.click(getByText('领取'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            '已过到期时间，积分尚未退回。现在已不能领取。',
            '不能领取',
        ));
        expect(claimGiftMock).not.toHaveBeenCalled();
    });

    it('asks the server when a past gift has an unknown status', async () => {
        previewGiftMock.mockResolvedValue({
            code: 'LJNMLC640I',
            sender_masked: '3***@qq.com',
            credits_micro: 1_000_000,
            status: 'weird',
            claimable: false,
            expires_at: '2020-01-02T00:00:00Z',
        });
        claimGiftMock.mockResolvedValue({
            id: 'gift_x',
            code: 'LJNMLC640I',
            credits_micro: 1_000_000,
            status: 'claimed',
        });
        const { getByLabelText, getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC640I' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('这份转赠现在不能领取。', '不能领取'));
        expect(queryByText(/积分已退回/)).toBeNull();
        fireEvent.click(getByText('领取'));
        await waitFor(() => expect(claimGiftMock).toHaveBeenCalledWith('LJNMLC640I'));
    });

    it('offers withdraw when this account already claimed the gift', async () => {
        previewGiftMock.mockResolvedValue({
            id: 'gift_held',
            code: 'LJNMLC64OI',
            sender_masked: '3***@qq.com',
            credits_micro: 17_140_000,
            status: 'claimed',
            claimable: false,
            withdrawable: true,
        });
        showConfirmMock.mockResolvedValue(true);
        withdrawGiftMock.mockResolvedValue({ ok: true });
        const { getByLabelText, getByText, getByRole, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC64OI' } });
        fireEvent.click(getByText('查验'));
        const note = await waitFor(() => {
            const status = getByRole('status');
            expect(status.textContent).toContain('你已经领取了这份转赠');
            return status;
        });
        expect(note.className).not.toContain('tbk-error');
        expect(showAlertMock).not.toHaveBeenCalled();
        expect(queryByText('领取')).toBeNull();
        expect(claimGiftMock).not.toHaveBeenCalled();
        fireEvent.click(getByText('提取这份转赠'));
        await waitFor(() => expect(withdrawGiftMock).toHaveBeenCalledWith(expect.any(String), 'gift_held', 17_140_000));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('转赠积分已提取到本机。', '提取成功'));
        expect(queryByText(/你已经领取了这份转赠/)).toBeNull();
        expect(getByRole('status').textContent).toContain('转赠积分已提取到本机');
        expect(claimGiftMock).not.toHaveBeenCalled();
    });

    it('does not offer a withdrawn gift again when the list or a late lookup still says held', async () => {
        let resolvePreview: (value: unknown) => void = () => {};
        previewGiftMock.mockImplementationOnce(() => new Promise((resolve) => {
            resolvePreview = resolve;
        }));
        listGiftsMock.mockResolvedValue({
            share_links: [],
            claimed_links: [{
                id: 'gift_held',
                code: 'LJNMLC64OI',
                credits_micro: 17_140_000,
                status: 'claimed',
                sender_masked: '3***@qq.com',
            }],
        });
        showConfirmMock.mockResolvedValue(true);
        withdrawGiftMock.mockResolvedValue({ ok: true });
        const { getByText, queryByText, queryByTestId } = render(
            <TokenBankPanel lang="zh-Hans" initialClaimCode="maclaw://credit/LJNMLC64OI" />,
        );
        await waitFor(() => expect(previewGiftMock).toHaveBeenCalledWith('LJNMLC64OI'));
        await waitFor(() => expect(getByText('提取这份转赠')).toBeTruthy());
        fireEvent.click(getByText('提取这份转赠'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('转赠积分已提取到本机。', '提取成功'));
        expect(queryByTestId('tbk-held-gifts')).toBeNull();
        await act(async () => {
            resolvePreview({
                id: 'gift_held',
                code: 'LJNMLC64OI',
                sender_masked: '3***@qq.com',
                credits_micro: 17_140_000,
                status: 'claimed',
                claimable: false,
                withdrawable: true,
            });
        });
        expect(queryByText('提取这份转赠')).toBeNull();
        expect(queryByText(/你已经领取了这份转赠/)).toBeNull();
        expect(queryByTestId('tbk-held-gifts')).toBeNull();
    });

    it('drops the previous withdraw button when a later lookup can be claimed', async () => {
        previewGiftMock.mockResolvedValueOnce({
            id: 'gift_held',
            code: 'LJNMLC64OI',
            sender_masked: '3***@qq.com',
            credits_micro: 17_140_000,
            status: 'claimed',
            claimable: false,
            withdrawable: true,
        });
        previewGiftMock.mockResolvedValueOnce({
            code: 'NEWCODE234',
            sender_masked: 'a***@x.com',
            credits_micro: 2_000_000,
            status: 'active',
            claimable: true,
        });
        const { getByLabelText, getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC64OI' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(getByText('提取这份转赠')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'NEWCODE234' } });
        expect(queryByText('提取这份转赠')).toBeNull();
        expect(queryByText(/你已经领取了这份转赠/)).toBeNull();
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(getByText(/a\*\*\*@x.com 转赠了/)).toBeTruthy());
        expect(queryByText('提取这份转赠')).toBeNull();
        expect(queryByText(/你已经领取了这份转赠/)).toBeNull();
    });

    it('lists a claimed gift that still needs withdrawing', async () => {
        listGiftsMock.mockResolvedValue({
            share_links: [],
            claimed_links: [{
                id: 'gift_held',
                credits_micro: 17_140_000,
                status: 'claimed',
                sender_masked: '3***@qq.com',
                expires_at: '2026-10-10T00:02:00Z',
            }],
        });
        const { getByLabelText, getByTestId, getByText } = render(<TokenBankPanel lang="zh-Hans" />);
        const held = await waitFor(() => getByTestId('tbk-held-gifts'));
        expect(held.textContent).toContain('17.14');
        expect(held.textContent).toContain('3***@qq.com');
        expect(held.textContent).toContain('未提取');
        expect(held.textContent).toContain('退回时间');
        expect(getByText('提取这份转赠')).toBeTruthy();
        showAlertMock.mockClear();
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC64OI' } });
        previewGiftMock.mockResolvedValue({
            id: 'gift_held',
            code: 'LJNMLC64OI',
            sender_masked: '3***@qq.com',
            credits_micro: 17_140_000,
            status: 'claimed',
            claimable: false,
            withdrawable: true,
        });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(previewGiftMock).toHaveBeenCalledWith('LJNMLC64OI'));
        await waitFor(() => expect((getByText('查验').closest('button') as HTMLButtonElement).disabled).toBe(false));
        expect(showAlertMock).not.toHaveBeenCalled();
    });

    it('keeps a claimed gift the server still lists after its return time', async () => {
        listGiftsMock.mockResolvedValue({
            share_links: [],
            claimed_links: [{
                id: 'gift_past',
                credits_micro: 1_000_000,
                status: 'claimed',
                sender_masked: 'b***@x.com',
                expires_at: '2020-01-01T00:00:00Z',
            }],
        });
        const { getByTestId } = render(<TokenBankPanel lang="zh-Hans" />);
        const held = await waitFor(() => getByTestId('tbk-held-gifts'));
        expect(held.textContent).toContain('b***@x.com');
        expect(held.textContent).toContain('已过退回时间');
        expect(held.textContent).toContain('提取这份转赠');
    });

    it('reuses each held gift request id when the other gift is retried in between', async () => {
        listGiftsMock.mockResolvedValue({
            share_links: [],
            claimed_links: [
                {
                    id: 'gift_a',
                    credits_micro: 17_140_000,
                    status: 'claimed',
                    sender_masked: '3***@qq.com',
                    expires_at: '2026-10-10T00:02:00Z',
                },
                {
                    id: 'gift_b',
                    credits_micro: 2_000_000,
                    status: 'claimed',
                    sender_masked: 'a***@x.com',
                    expires_at: '2026-10-10T00:02:00Z',
                },
            ],
        });
        showConfirmMock.mockResolvedValue(true);
        withdrawGiftMock.mockRejectedValue(new Error('Hub rejected the withdrawal'));
        const { getAllByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getAllByText('提取这份转赠')).toHaveLength(2));
        fireEvent.click(getAllByText('提取这份转赠')[0]);
        await waitFor(() => expect(withdrawGiftMock).toHaveBeenCalledTimes(1));
        fireEvent.click(getAllByText('提取这份转赠')[1]);
        await waitFor(() => expect(withdrawGiftMock).toHaveBeenCalledTimes(2));
        fireEvent.click(getAllByText('提取这份转赠')[0]);
        await waitFor(() => expect(withdrawGiftMock).toHaveBeenCalledTimes(3));
        fireEvent.click(getAllByText('提取这份转赠')[1]);
        await waitFor(() => expect(withdrawGiftMock).toHaveBeenCalledTimes(4));
        const ids = withdrawGiftMock.mock.calls.map((call) => call[0]);
        expect(ids[0]).toBe(ids[2]);
        expect(ids[1]).toBe(ids[3]);
        expect(ids[0]).not.toBe(ids[1]);
        expect(withdrawGiftMock.mock.calls.map((call) => call[1])).toEqual(['gift_a', 'gift_b', 'gift_a', 'gift_b']);
        expect(withdrawGiftMock.mock.calls[0][2]).toBe(17_140_000);
        expect(withdrawGiftMock.mock.calls[1][2]).toBe(2_000_000);
    });

    it('says this account already withdrew the gift', async () => {
        previewGiftMock.mockResolvedValue({
            code: 'LJNMLC64OI',
            sender_masked: '3***@qq.com',
            credits_micro: 17_140_000,
            status: 'settled',
            claimable: false,
            withdrawn: true,
        });
        const { getByLabelText, getByText, queryByText, getByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC64OI' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            '你已经把这份转赠提取到本机。',
            '已经提取',
        ));
        const note = getByRole('status');
        expect(note.className).not.toContain('tbk-error');
        expect(queryByText('提取这份转赠')).toBeNull();
        expect(claimGiftMock).not.toHaveBeenCalled();
        showAlertMock.mockClear();
        fireEvent.click(getByText('领取'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            '你已经把这份转赠提取到本机。',
            '已经提取',
        ));
        expect(claimGiftMock).not.toHaveBeenCalled();
    });

    it('says someone else already withdrew a settled gift', async () => {
        previewGiftMock.mockResolvedValue({
            code: 'LJNMLC64OI',
            sender_masked: '3***@qq.com',
            credits_micro: 17_140_000,
            status: 'settled',
            claimable: false,
        });
        const { getByLabelText, getByText, queryByText, getByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC64OI' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            '对方已经把这份转赠提取到本机。',
            '不能领取',
        ));
        const note = getByRole('status');
        expect(note.className).toContain('tbk-error');
        expect(note.textContent).toContain('对方已经把这份转赠提取到本机');
        expect(queryByText(/已经被领取/)).toBeNull();
        showAlertMock.mockClear();
        fireEvent.click(getByText('领取'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            '对方已经把这份转赠提取到本机。',
            '不能领取',
        ));
        expect(claimGiftMock).not.toHaveBeenCalled();
    });

    it('drops a failed lookup when the code changes', async () => {
        previewGiftMock.mockRejectedValue(new Error('查不到这份转赠'));
        const { getByLabelText, getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC64OI' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(getByText('查不到这份转赠')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'NEWCODE234' } });
        expect(queryByText('查不到这份转赠')).toBeNull();
    });

    it('names a repeat claim as already claimed and still offers withdraw', async () => {
        claimGiftMock.mockResolvedValue({
            id: 'gift_held',
            code: 'LJNMLC64OI',
            credits_micro: 17_140_000,
            status: 'claimed',
            resume: true,
        });
        const { getByLabelText, getByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'LJNMLC64OI' } });
        fireEvent.click(getByText('领取'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            '你已经领取了这份转赠。提取到本机后，积分才会入账。',
            '已经领取',
        ));
        expect(getByText('提取这份转赠')).toBeTruthy();
        expect(showAlertMock.mock.calls.some((call) => call[1] === '提取这份转赠')).toBe(false);
    });

    it('keeps a claim withdraw button when a later lookup is a different code', async () => {
        claimGiftMock.mockResolvedValue({
            id: 'gift_9',
            code: 'SECRETCODE',
            credits_micro: 2_000_000,
            status: 'claimed',
        });
        previewGiftMock.mockResolvedValueOnce({
            code: 'NEWCODE234',
            sender_masked: 'a***@x.com',
            credits_micro: 1_000_000,
            status: 'active',
            claimable: true,
        });
        previewGiftMock.mockRejectedValueOnce(new Error('查不到这份转赠'));
        const { getByLabelText, getByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'SECRETCODE' } });
        fireEvent.click(getByText('领取'));
        await waitFor(() => expect(getByText('提取这份转赠')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'NEWCODE234' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(getByText(/a\*\*\*@x.com 转赠了/)).toBeTruthy());
        expect(getByText('提取这份转赠')).toBeTruthy();
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'OTHERCODE1' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(getByText('查不到这份转赠')).toBeTruthy());
        expect(getByText('提取这份转赠')).toBeTruthy();
    });

    it('does not paint a claim result onto a code typed while the claim is in flight', async () => {
        let resolveClaim: (value: unknown) => void = () => {};
        claimGiftMock.mockImplementationOnce(() => new Promise((resolve) => {
            resolveClaim = resolve;
        }));
        const { getByLabelText, getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'SECRETCODE' } });
        fireEvent.click(getByText('领取'));
        await waitFor(() => expect(getByText('正在领取…')).toBeTruthy());
        expect((getByText('查验').closest('button') as HTMLButtonElement).disabled).toBe(true);
        expect((getByText('领取').closest('button') as HTMLButtonElement).disabled).toBe(true);
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'NEWCODE234' } });
        expect(queryByText('正在领取…')).toBeNull();
        await act(async () => {
            resolveClaim({
                id: 'gift_9',
                code: 'SECRETCODE',
                credits_micro: 2_000_000,
                status: 'claimed',
            });
        });
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            '已领取。把这份转赠提取到本机后，积分才会入账。',
            '领取成功',
        ));
        expect(queryByText(/已领取/)).toBeNull();
        expect(queryByText(/正在领取/)).toBeNull();
        expect(getByText('提取这份转赠')).toBeTruthy();
        expect((getByText('领取').closest('button') as HTMLButtonElement).disabled).toBe(false);
        expect(claimGiftMock).toHaveBeenCalledTimes(1);
        expect(claimGiftMock).toHaveBeenCalledWith('SECRETCODE');
    });

    it('does not leave a claim failure under a code typed while the claim is in flight', async () => {
        let rejectClaim: (reason: Error) => void = () => {};
        claimGiftMock.mockImplementationOnce(() => new Promise((_, reject) => {
            rejectClaim = reject;
        }));
        const { getByLabelText, getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'SECRETCODE' } });
        fireEvent.click(getByText('领取'));
        await waitFor(() => expect(getByText('正在领取…')).toBeTruthy());
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'NEWCODE234' } });
        await act(async () => {
            rejectClaim(new Error('领取被拒绝'));
        });
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('领取被拒绝', '领取失败'));
        expect(queryByText('领取被拒绝')).toBeNull();
        expect(queryByText('正在领取…')).toBeNull();
    });

    it('keeps a newer gift lookup when an older claim returns', async () => {
        let resolveClaim: (value: unknown) => void = () => {};
        claimGiftMock.mockImplementationOnce(() => new Promise((resolve) => {
            resolveClaim = resolve;
        }));
        previewGiftMock.mockResolvedValue({
            code: 'NEWCODE234',
            sender_masked: 'a***@x.com',
            credits_micro: 1_000_000,
            status: 'active',
            claimable: true,
        });
        const view = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(view.getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(view.getByLabelText('兑换码或链接'), { target: { value: 'SECRETCODE' } });
        fireEvent.click(view.getByText('领取'));
        await waitFor(() => expect(view.getByText('正在领取…')).toBeTruthy());
        view.rerender(<TokenBankPanel lang="zh-Hans" initialClaimCode="maclaw://credit/NEWCODE234" />);
        await waitFor(() => expect(previewGiftMock).toHaveBeenCalledWith('NEWCODE234'));
        await waitFor(() => expect(view.getByText(/a\*\*\*@x.com 转赠了/)).toBeTruthy());
        expect(view.queryByText('正在领取…')).toBeNull();
        await act(async () => {
            resolveClaim({
                id: 'gift_9',
                code: 'SECRETCODE',
                credits_micro: 2_000_000,
                status: 'claimed',
            });
        });
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith(
            '已领取。把这份转赠提取到本机后，积分才会入账。',
            '领取成功',
        ));
        expect(view.getByText(/a\*\*\*@x.com 转赠了/)).toBeTruthy();
        expect(view.queryByText(/已领取。把这份转赠提取到本机/)).toBeNull();
        expect(view.getByText('提取这份转赠')).toBeTruthy();
        expect((view.getByLabelText('兑换码或链接') as HTMLInputElement).value).toBe('NEWCODE234');
    });

    it('ignores a deep-link lookup that returns after a newer one', async () => {
        let resolveFirst: (value: unknown) => void = () => {};
        previewGiftMock.mockResolvedValue({
            code: 'NEWCODE234',
            sender_masked: 'a***@x.com',
            credits_micro: 2_000_000,
            status: 'active',
            claimable: true,
        });
        previewGiftMock.mockImplementationOnce(() => new Promise((resolve) => {
            resolveFirst = resolve;
        }));
        const { getByLabelText, getByText, queryByText } = render(
            <TokenBankPanel lang="zh-Hans" initialClaimCode="maclaw://credit/OLDCODE234" />,
        );
        await waitFor(() => expect(previewGiftMock).toHaveBeenCalledWith('OLDCODE234'));
        expect((getByLabelText('兑换码或链接') as HTMLInputElement).value).toBe('OLDCODE234');
        fireEvent.change(getByLabelText('兑换码或链接'), { target: { value: 'NEWCODE234' } });
        fireEvent.click(getByText('查验'));
        await waitFor(() => expect(getByText(/a\*\*\*@x.com 转赠了/)).toBeTruthy());
        await act(async () => {
            resolveFirst({
                code: 'OLDCODE234',
                sender_masked: 'old@x.com',
                credits_micro: 1_000_000,
                status: 'expired',
                claimable: false,
            });
        });
        expect(queryByText(/old@x.com/)).toBeNull();
        expect(getByText(/a\*\*\*@x.com 转赠了/)).toBeTruthy();
        expect(queryByText(/这份转赠已过期/)).toBeNull();
    });

    it('previews a desktop gift link and leaves claiming to the button', async () => {
        previewGiftMock.mockResolvedValue({
            code: 'ABCDEF2345',
            sender_masked: 'a***@x.com',
            credits_micro: 2_000_000,
            status: 'active',
            claimable: true,
        });
        const { getByLabelText, getByText } = render(
            <TokenBankPanel lang="en" initialClaimCode="maclaw://credit/ABCDEF2345" />,
        );
        await waitFor(() => expect(previewGiftMock).toHaveBeenCalledWith('ABCDEF2345'));
        expect((getByLabelText('Gift code or link') as HTMLInputElement).value).toBe('ABCDEF2345');
        expect(getByText(/a\*\*\*@x.com/)).toBeTruthy();
        expect(claimGiftMock).not.toHaveBeenCalled();
    });

    it('still applies a deep-link lookup when the effect restarts', async () => {
        previewGiftMock.mockResolvedValue({
            code: 'ABCDEF2345',
            sender_masked: 'a***@x.com',
            credits_micro: 2_000_000,
            status: 'active',
            claimable: true,
        });
        const { getByText } = render(
            <StrictMode>
                <TokenBankPanel lang="en" initialClaimCode="maclaw://credit/ABCDEF2345" />
            </StrictMode>,
        );
        await waitFor(() => expect(getByText(/a\*\*\*@x.com/)).toBeTruthy());
        expect(previewGiftMock).toHaveBeenCalledWith('ABCDEF2345');
    });

    it('looks up the same gift link again when it is opened again', async () => {
        previewGiftMock.mockResolvedValue({
            code: 'ABCDEF2345',
            sender_masked: 'a***@x.com',
            credits_micro: 2_000_000,
            status: 'active',
            claimable: true,
        });
        const view = render(
            <TokenBankPanel lang="zh-Hans" initialClaimCode="maclaw://credit/ABCDEF2345" initialClaimSeq={1} />,
        );
        await waitFor(() => expect(previewGiftMock).toHaveBeenCalledTimes(1));
        fireEvent.change(view.getByLabelText('兑换码或链接'), { target: { value: 'OTHERCODE1' } });
        expect((view.getByLabelText('兑换码或链接') as HTMLInputElement).value).toBe('OTHERCODE1');
        view.rerender(
            <TokenBankPanel lang="zh-Hans" initialClaimCode="maclaw://credit/ABCDEF2345" initialClaimSeq={2} />,
        );
        await waitFor(() => expect(previewGiftMock).toHaveBeenCalledTimes(2));
        expect((view.getByLabelText('兑换码或链接') as HTMLInputElement).value).toBe('ABCDEF2345');
        expect(view.getByText(/a\*\*\*@x.com 转赠了/)).toBeTruthy();
    });

    it('shows a deep-link lookup failure on the form', async () => {
        previewGiftMock.mockRejectedValue(new Error('查不到这份转赠'));
        const { getByLabelText, getByText } = render(
            <TokenBankPanel lang="zh-Hans" initialClaimCode="maclaw://credit/LJNMLC64OI" />,
        );
        await waitFor(() => expect(showAlertMock).toHaveBeenCalledWith('查不到这份转赠', '查验失败'));
        expect(getByText('查不到这份转赠')).toBeTruthy();
        expect((getByLabelText('兑换码或链接') as HTMLInputElement).value).toBe('LJNMLC64OI');
    });

    it('releases the claim buttons when a newer link open finishes an older lookup', async () => {
        let resolveFirst: (value: unknown) => void = () => {};
        previewGiftMock.mockResolvedValue({
            code: 'NEWCODE234',
            sender_masked: 'a***@x.com',
            credits_micro: 1_000_000,
            status: 'active',
            claimable: true,
        });
        previewGiftMock.mockImplementationOnce(() => new Promise((resolve) => {
            resolveFirst = resolve;
        }));
        const view = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(view.getByLabelText('兑换码或链接')).toBeTruthy());
        fireEvent.change(view.getByLabelText('兑换码或链接'), { target: { value: 'OLDCODE234' } });
        fireEvent.click(view.getByText('查验'));
        await waitFor(() => expect(view.getByText('正在查验…')).toBeTruthy());
        expect((view.getByText('查验').closest('button') as HTMLButtonElement).disabled).toBe(true);
        view.rerender(<TokenBankPanel lang="zh-Hans" initialClaimCode="maclaw://credit/NEWCODE234" initialClaimSeq={1} />);
        await waitFor(() => expect(view.getByText(/a\*\*\*@x.com 转赠了/)).toBeTruthy());
        await act(async () => {
            resolveFirst({
                code: 'OLDCODE234',
                sender_masked: 'old@x.com',
                credits_micro: 1_000_000,
                status: 'expired',
                claimable: false,
            });
        });
        expect(view.queryByText(/old@x.com/)).toBeNull();
        expect(view.getByText(/a\*\*\*@x.com 转赠了/)).toBeTruthy();
        await waitFor(() => expect((view.getByText('查验').closest('button') as HTMLButtonElement).disabled).toBe(false));
        expect((view.getByText('领取').closest('button') as HTMLButtonElement).disabled).toBe(false);
    });

    it('shows the rank badge and a private share marker', async () => {
        summaryMock.mockResolvedValue({ ...summaryFixture, rank: 1, rank_badge: 'gold', lifetime_badge: 'pillar' });
        listSharesMock.mockResolvedValue({
            shares: [{ ...shareFixture, visibility: 'private', extra_key_count: 2 }],
        });
        const { getByText, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByText('金')).toBeTruthy());
        expect(getByText('支柱')).toBeTruthy();
        expect(getByText('#1')).toBeTruthy();
        expect(getByText('私有')).toBeTruthy();
        expect(getByText('额外密钥 2')).toBeTruthy();
        expect(queryByText('追加密钥')).toBeTruthy();
    });

    it('lists a tested provider between the balance cards and the sent links', async () => {
        listProvidersMock.mockResolvedValue({
            providers: [
                {
                    id: 'kimi',
                    name: 'Kimi Code',
                    url: 'https://api.kimi.com/coding/v1',
                    key: 'sk-kimi',
                    model: 'kimi-for-coding',
                    protocol: 'openai',
                    connection_test_passed: true,
                },
                {
                    id: 'deepseek',
                    name: 'DeepSeek',
                    url: 'https://api.deepseek.com/v1',
                    key: 'sk-ds',
                    connection_test_passed: false,
                },
                {
                    id: 'hub',
                    name: 'MaClaw官方',
                    url: 'https://hub.example',
                    key: 'hub-key',
                    connection_test_passed: true,
                    is_hub_service: true,
                },
            ],
        });
        const { getByText, queryByText, getByTestId, getByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        const picker = await waitFor(() => getByRole('combobox', { name: /要存入的服务商/ }));
        fireEvent.click(picker);
        await waitFor(() => expect(getByRole('option', { name: 'Kimi Code' })).toBeTruthy());
        expect(queryByText('DeepSeek')).toBeNull();
        expect(queryByText('MaClaw官方')).toBeNull();
        const section = getByTestId('tbk-shareable-providers');
        const links = getByText('我发出的转赠链接');
        const available = getByText('可用');
        expect(available.compareDocumentPosition(section) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(section.compareDocumentPosition(links) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(getByText('存入银行')).toBeTruthy();
    });

    it('lists my shares as nine cards a page', async () => {
        listSharesMock.mockResolvedValue({
            shares: Array.from({ length: 10 }, (_, index) => ({
                ...shareFixture,
                id: `sh_${index + 1}`,
                display_name: `Share ${index + 1}`,
            })),
        });
        const { getByRole, getByTestId, getByText, queryByText } = render(<TokenBankPanel lang="en" />);
        const grid = await waitFor(() => getByTestId('tbk-share-grid'));
        expect(grid.classList.contains('tbk-share-grid')).toBe(true);
        expect(grid.querySelectorAll(':scope > li.tbk-share')).toHaveLength(9);
        expect(grid.querySelectorAll('.tbk-share__fingerprint')).toHaveLength(9);
        expect(getByText('Share 1')).toBeTruthy();
        expect(queryByText('Share 10')).toBeNull();
        expect(getByText('Page 1 of 2')).toBeTruthy();

        fireEvent.click(getByRole('button', { name: 'Next' }));
        await waitFor(() => expect(getByText('Share 10')).toBeTruthy());
        expect(queryByText('Share 1')).toBeNull();
        expect(grid.querySelectorAll(':scope > li.tbk-share')).toHaveLength(1);
        expect(getByText('Page 2 of 2')).toBeTruthy();

        fireEvent.click(getByRole('button', { name: 'Previous' }));
        await waitFor(() => expect(getByText('Share 1')).toBeTruthy());
        expect(queryByText('Share 10')).toBeNull();

        fireEvent.click(getByRole('button', { name: 'Next' }));
        await waitFor(() => expect(getByText('Page 2 of 2')).toBeTruthy());
        listSharesMock.mockResolvedValue({ shares: [{ ...shareFixture, display_name: 'Only left' }] });
        fireEvent.click(getByRole('button', { name: 'Today' }));
        await waitFor(() => expect(getByText('Only left')).toBeTruthy());
        expect(queryByText('Share 10')).toBeNull();
        expect(queryByText('Page 2 of 2')).toBeNull();
        expect(grid.querySelectorAll(':scope > li.tbk-share')).toHaveLength(1);
    });

    it('shows withdrawal history as localized cards, twenty a page', async () => {
        const extras = Array.from({ length: 19 }, (_, index) => ({
            id: `w-extra-${index + 1}`,
            request_id: `req-extra-${index + 1}`,
            amount_micro: 1_000_000,
            kind: 'self',
            state: 'reissued',
            grant_id: `grant-extra-${index + 1}`,
            created_at: '2026-10-02T00:00:00Z',
        }));
        listWithdrawalsMock.mockResolvedValue({
            withdrawals: [
                {
                    id: 'w-bound',
                    request_id: 'req-bound',
                    hub_id: 'hub-home',
                    amount_micro: 58_500_000,
                    grant_id: 'grant-home',
                    kind: 'self',
                    state: 'bound',
                    created_at: '2026-10-03T01:52:14Z',
                },
                ...extras,
                {
                    id: 'w-gift',
                    request_id: 'req-gift',
                    hub_id: 'hub-home',
                    amount_micro: 17_140_000,
                    kind: 'Gift',
                    link_id: 'gift_held',
                    state: 'Issued',
                    created_at: '2026-10-03T01:05:44Z',
                },
            ],
        });
        const { getByRole, getByTestId, getByText, queryByText } = render(<TokenBankPanel lang="en" />);
        const grid = await waitFor(() => getByTestId('tbk-withdraw-grid'));
        expect(grid.classList.contains('tbk-withdraw-grid')).toBe(true);
        expect(grid.querySelectorAll(':scope > li.tbk-withdraw')).toHaveLength(20);
        const bound = getByTestId('tbk-withdraw-w-bound');
        expect(bound.textContent).toContain('Your own credits');
        expect(bound.textContent).toContain('Credited');
        expect(bound.textContent).toContain('The grant is recorded on that machine. The assistant there can spend it.');
        expect(bound.textContent).toContain(`Time ${formatGiftInstant('2026-10-03T01:52:14Z', true)}`);
        expect(bound.textContent).toContain('Machine hub-home');
        expect(bound.textContent).toContain('Grant grant-home');
        expect(bound.textContent).not.toContain('self');
        expect(bound.textContent).not.toContain('2026-10-03T01:52:14Z');
        // Date descending keeps this gift on page 1: it is newer than the October 2 rows.
        const gift = getByTestId('tbk-withdraw-w-gift');
        expect(gift.textContent).toContain('A claimed gift');
        expect(gift.textContent).toContain('Pending');
        expect(gift.textContent).toContain('The bank already deducted this amount. The grant is not recorded on the machine yet.');
        expect(gift.textContent).toContain('Gift record gift_held');
        expect(gift.textContent).toContain('Finish this withdrawal');
        expect(gift.textContent).not.toContain('Grant');
        expect(queryByText('Grant grant-extra-19')).toBeNull();
        expect(getByText('Withdrawals, page 1 of 2')).toBeTruthy();
        expect(getByTestId('tbk-withdraw-pending').textContent).toBe('1 still to finish');

        fireEvent.click(getByRole('button', { name: 'Next withdrawal page' }));
        await waitFor(() => expect(getByText('Grant grant-extra-19')).toBeTruthy());
        expect(grid.querySelectorAll(':scope > li.tbk-withdraw')).toHaveLength(1);
        expect(queryByText('Grant grant-home')).toBeNull();
        expect(queryByText('Finish this withdrawal')).toBeNull();
        expect(getByTestId('tbk-withdraw-pending').textContent).toBe('1 still to finish');
        expect(getByText('Reissued')).toBeTruthy();
        expect(getByText('The credited machine was reinstalled, and the grant was written again.')).toBeTruthy();

        fireEvent.click(getByRole('button', { name: 'Previous withdrawal page' }));
        await waitFor(() => expect(getByTestId('tbk-withdraw-w-bound')).toBeTruthy());
        expect(queryByText('Grant grant-extra-19')).toBeNull();
        expect(getByTestId('tbk-withdraw-w-gift')).toBeTruthy();
    });

    it('keeps an older unfinished withdrawal off the first page', async () => {
        const newer = Array.from({ length: 20 }, (_, index) => ({
            id: `w-new-${index + 1}`,
            request_id: `req-new-${index + 1}`,
            amount_micro: 1_000_000,
            kind: 'self',
            state: 'bound',
            grant_id: `grant-new-${index + 1}`,
            created_at: `2026-10-04T00:${String(index).padStart(2, '0')}:00Z`,
        }));
        listWithdrawalsMock.mockResolvedValue({
            withdrawals: [
                ...newer,
                {
                    id: 'w-old-gift',
                    request_id: 'req-old-gift',
                    hub_id: 'hub-home',
                    amount_micro: 4_000_000,
                    kind: 'gift',
                    link_id: 'gift_old',
                    state: 'issued',
                    created_at: '2026-09-01T00:00:00Z',
                },
            ],
        });
        const { getByRole, getByTestId, queryByTestId } = render(<TokenBankPanel lang="zh-Hans" />);
        const grid = await waitFor(() => getByTestId('tbk-withdraw-grid'));
        expect(grid.querySelectorAll(':scope > li.tbk-withdraw')).toHaveLength(20);
        expect(queryByTestId('tbk-withdraw-w-old-gift')).toBeNull();
        expect(getByTestId('tbk-withdraw-w-new-20')).toBeTruthy();
        expect(getByTestId('tbk-withdraw-pending').textContent).toBe('1 笔待完成');

        fireEvent.click(getByTestId('tbk-withdraw-pending'));
        await waitFor(() => expect(getByTestId('tbk-withdraw-w-old-gift')).toBeTruthy());
        expect(grid.querySelectorAll(':scope > li.tbk-withdraw')).toHaveLength(1);
        expect(queryByTestId('tbk-withdraw-w-new-1')).toBeNull();
    });

    it('does not call a failed withdrawal load an empty history', async () => {
        listWithdrawalsMock.mockRejectedValue(new Error('list down'));
        const { findByText, queryByText, queryByTestId } = render(<TokenBankPanel lang="zh-Hans" />);
        expect(await findByText('list down')).toBeTruthy();
        expect(queryByText('还没有提取记录。')).toBeNull();
        expect(queryByTestId('tbk-withdraw-empty')).toBeNull();
    });

    it('does not call an empty withdrawal list finished while it is still loading', async () => {
        let resolveList: (value: unknown) => void = () => {};
        listWithdrawalsMock.mockReturnValue(new Promise((resolve) => {
            resolveList = resolve;
        }));
        const { getByTestId, queryByText } = render(<TokenBankPanel lang="zh-Hans" />);
        expect(getByTestId('tbk-withdraw-empty').textContent).toContain('加载中');
        expect(queryByText('还没有提取记录。')).toBeNull();
        resolveList({ withdrawals: [] });
        await waitFor(() => expect(getByTestId('tbk-withdraw-empty').textContent).toContain('还没有提取记录。'));
    });

    it('localizes a withdrawal card into Chinese', async () => {
        listWithdrawalsMock.mockResolvedValue({
            withdrawals: [{
                id: 'w-bound',
                request_id: 'req-bound',
                hub_id: 'hub-home',
                amount_micro: 58_500_000,
                grant_id: 'grant-home',
                kind: 'self',
                state: 'bound',
                created_at: '2026-10-03T01:52:14Z',
            }],
        });
        const hans = render(<TokenBankPanel lang="zh-Hans" />);
        const card = await waitFor(() => hans.getByTestId('tbk-withdraw-w-bound'));
        expect(card.textContent).toContain('已入账');
        expect(card.textContent).toContain('来源 自己的积分');
        expect(card.textContent).not.toContain('自动提现');
        expect(card.textContent).toContain('发放单已写入该机器，那里的助手可以花费。');
        expect(card.textContent).toContain(`时间 ${formatGiftInstant('2026-10-03T01:52:14Z', true)}`);
        expect(card.textContent).toContain('机器 hub-home');
        expect(card.textContent).toContain('发放单 grant-home');
        expect(card.textContent).not.toContain('self');
        expect(card.textContent).not.toContain('bound');
        hans.unmount();

        const hant = render(<TokenBankPanel lang="zh-Hant" />);
        const traditional = await waitFor(() => hant.getByTestId('tbk-withdraw-w-bound'));
        expect(traditional.textContent).toContain('已入帳');
        expect(traditional.textContent).toContain('來源 自己的積分');
        expect(traditional.textContent).toContain('發放單已寫入該機器，那裡的助手可以花費。');
        expect(traditional.textContent).toContain('機器 hub-home');
        expect(traditional.textContent).not.toContain('bound');
    });

    it('does not say a self issued debit is missing from the machine', async () => {
        listWithdrawalsMock.mockResolvedValue({
            withdrawals: [{
                id: 'w-self-issued',
                request_id: 'tbk-auto:hub-home:owner@example.com:paid:0',
                hub_id: 'hub-home',
                amount_micro: 106_655_500,
                kind: 'self',
                state: 'issued',
                created_at: '2026-10-03T01:05:44Z',
            }],
        });
        const hans = render(<TokenBankPanel lang="zh-Hans" />);
        const card = await waitFor(() => hans.getByTestId('tbk-withdraw-w-self-issued'));
        expect(card.textContent).toContain('未确认');
        expect(card.textContent).toContain('自动提现');
        expect(card.textContent).toContain('银行已扣出这笔积分，该机器尚未把发放单号确认回去。');
        expect(card.textContent).not.toContain('尚未写入机器');
        expect(card.textContent).not.toContain('完成这次提取');
        hans.unmount();

        const en = render(<TokenBankPanel lang="en" />);
        const english = await waitFor(() => en.getByTestId('tbk-withdraw-w-self-issued'));
        expect(english.textContent).toContain('Unconfirmed');
        expect(english.textContent).toContain('Automatic withdrawal');
        expect(english.textContent).toContain('That machine has not confirmed the grant id back.');
        expect(english.textContent).not.toContain('not recorded on the machine yet');
        expect(english.textContent).not.toContain('Finish this withdrawal');
    });

    it('shows a ledger-only withdrawal as deducted, including an automatic one', async () => {
        listWithdrawalsMock.mockResolvedValue({
            withdrawals: [
                {
                    id: 'w-posted-manual',
                    request_id: 'req-posted-manual',
                    hub_id: 'hub-home',
                    amount_micro: 12_000_000,
                    kind: 'self',
                    state: 'posted',
                    created_at: '2026-10-04T01:00:00Z',
                },
                {
                    id: 'w-posted-auto',
                    request_id: 'tbk-auto:hub-home:owner@example.com:paid:4',
                    hub_id: 'hub-home',
                    amount_micro: 2_500_000,
                    kind: 'self',
                    state: 'posted',
                    automatic: true,
                    created_at: '2026-10-04T02:00:00Z',
                },
            ],
        });
        const hans = render(<TokenBankPanel lang="zh-Hans" />);
        const manual = await waitFor(() => hans.getByTestId('tbk-withdraw-w-posted-manual'));
        expect(manual.textContent).toContain('已扣款');
        expect(manual.textContent).toContain('来源 自己的积分');
        expect(manual.textContent).toContain('银行已扣出这笔积分。');
        expect(manual.textContent).not.toContain('自动提现');
        expect(manual.textContent).not.toContain('未确认');
        expect(manual.textContent).not.toContain('posted');
        const auto = hans.getByTestId('tbk-withdraw-w-posted-auto');
        expect(auto.textContent).toContain('已扣款');
        expect(auto.textContent).toContain('自动提现');
        expect(auto.textContent).toContain('机器 hub-home');
        expect(auto.textContent).not.toContain('未确认');
        expect(auto.textContent).not.toContain('完成这次提取');
    });

    it('saves a per-withdrawal cap and treats a blank field as no limit', async () => {
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 12_500_000 });
        const { findByLabelText, getByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        const input = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(input.value).toBe('12.5'));
        expect(input.placeholder).toBe('不限制');

        fireEvent.change(input, { target: { value: '20' } });
        fireEvent.click(getByRole('button', { name: '保存' }));
        await waitFor(() => expect(saveAutoSettingsMock).toHaveBeenCalledWith(20_000_000));

        fireEvent.change(input, { target: { value: '' } });
        fireEvent.click(getByRole('button', { name: '保存' }));
        await waitFor(() => expect(saveAutoSettingsMock).toHaveBeenLastCalledWith(0));
    });

    it('keeps a saved cap when an older refresh returns', async () => {
        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 12_500_000 });
        const { findByLabelText, getByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        const input = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        await waitFor(() => expect(input.value).toBe('12.5'));

        let resolveStale: (value: unknown) => void = () => {};
        getAutoSettingsMock.mockReturnValue(new Promise((resolve) => {
            resolveStale = resolve;
        }));
        fireEvent.click(getByRole('button', { name: '刷新' }));
        await waitFor(() => expect(getAutoSettingsMock).toHaveBeenCalledTimes(2));

        fireEvent.change(input, { target: { value: '20' } });
        fireEvent.click(getByRole('button', { name: '保存' }));
        await waitFor(() => expect(saveAutoSettingsMock).toHaveBeenCalledWith(20_000_000));
        expect(input.value).toBe('20');

        resolveStale({ enabled: true, max_per_withdraw_micro: 12_500_000 });
        await waitFor(() => expect(listGiftsMock).toHaveBeenCalledTimes(2));
        expect(input.value).toBe('20');

        getAutoSettingsMock.mockResolvedValue({ enabled: true, max_per_withdraw_micro: 3_000_000 });
        fireEvent.click(getByRole('button', { name: '刷新' }));
        await waitFor(() => expect(input.value).toBe('3'));
    });

    it('does not clear the cap before the stored value has loaded', async () => {
        let resolveSettings: (value: unknown) => void = () => {};
        getAutoSettingsMock.mockReturnValue(new Promise((resolve) => {
            resolveSettings = resolve;
        }));
        const { findByLabelText, getByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        const save = getByRole('button', { name: '保存' }) as HTMLButtonElement;
        expect(save.disabled).toBe(true);
        fireEvent.click(save);
        expect(saveAutoSettingsMock).not.toHaveBeenCalled();

        const input = await findByLabelText('每次自动提取上限') as HTMLInputElement;
        expect(input.disabled).toBe(true);
        fireEvent.change(input, { target: { value: '99' } });

        resolveSettings({ enabled: true, max_per_withdraw_micro: 12_500_000 });
        await waitFor(() => expect(input.value).toBe('12.5'));
        expect(input.disabled).toBe(false);
        expect(save.disabled).toBe(false);
        expect(saveAutoSettingsMock).not.toHaveBeenCalled();
    });

    it('moves through each page that still has an unfinished withdrawal', async () => {
        const middle = Array.from({ length: 20 }, (_, index) => ({
            id: `w-mid-${index + 1}`,
            request_id: `req-mid-${index + 1}`,
            amount_micro: 1_000_000,
            kind: 'self',
            state: 'bound',
            grant_id: `grant-mid-${index + 1}`,
            created_at: `2026-10-03T00:${String(index).padStart(2, '0')}:00Z`,
        }));
        listWithdrawalsMock.mockResolvedValue({
            withdrawals: [
                {
                    id: 'w-new-gift',
                    request_id: 'req-new-gift',
                    amount_micro: 2_000_000,
                    kind: 'gift',
                    link_id: 'gift_new',
                    state: 'issued',
                    created_at: '2026-10-04T00:00:00Z',
                },
                ...middle,
                {
                    id: 'w-old-gift',
                    request_id: 'req-old-gift',
                    amount_micro: 4_000_000,
                    kind: 'gift',
                    link_id: 'gift_old',
                    state: 'issued',
                    created_at: '2026-09-01T00:00:00Z',
                },
            ],
        });
        const { getByTestId, queryByTestId } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByTestId('tbk-withdraw-w-new-gift')).toBeTruthy());
        expect(queryByTestId('tbk-withdraw-w-old-gift')).toBeNull();
        expect(getByTestId('tbk-withdraw-pending').textContent).toBe('2 笔待完成');

        fireEvent.click(getByTestId('tbk-withdraw-pending'));
        await waitFor(() => expect(getByTestId('tbk-withdraw-w-old-gift')).toBeTruthy());
        expect(queryByTestId('tbk-withdraw-w-new-gift')).toBeNull();

        fireEvent.click(getByTestId('tbk-withdraw-pending'));
        await waitFor(() => expect(getByTestId('tbk-withdraw-w-new-gift')).toBeTruthy());
        expect(queryByTestId('tbk-withdraw-w-old-gift')).toBeNull();
    });

    it('explains an older hub that cannot store the cap', async () => {
        getAutoSettingsMock.mockRejectedValue(new Error('this Hub does not support the automatic withdrawal cap yet'));
        const { findByText, getByRole } = render(<TokenBankPanel lang="zh-Hans" />);
        expect(await findByText('当前 Hub 还不能保存自动提取上限。请先更新 Hub。')).toBeTruthy();
        expect((getByRole('button', { name: '保存' }) as HTMLButtonElement).disabled).toBe(true);
        expect(saveAutoSettingsMock).not.toHaveBeenCalled();
    });

    it('localizes into Chinese', async () => {
        const { getByText } = render(<TokenBankPanel lang="zh-Hans" />);
        await waitFor(() => expect(getByText('Token 银行')).toBeTruthy());
        expect(getByText('将服务商 Token 额度存入 Token 银行，换取终身有效、可转赠的积分。提取到本机后由助手使用，不可提现。')).toBeTruthy();
        expect(getByText('提取到本机')).toBeTruthy();
        expect(getByText('查验')).toBeTruthy();
        expect(getByText('暂停')).toBeTruthy();
        expect(getByText('调整模型')).toBeTruthy();
    });

    it('opens the share dialog to add and remove models for a matching provider', async () => {
        const { fingerprintProviderKey } = await import('../../utils/hubcenterTokenBank');
        const apiURL = 'https://api.example/v1';
        const apiKey = 'sk-live';
        const fingerprint = await fingerprintProviderKey(apiKey, apiURL);
        listSharesMock.mockResolvedValue({
            shares: [{ ...shareFixture, api_url: apiURL, protocol: 'openai', key_fingerprint: fingerprint }],
        });
        listProvidersMock.mockResolvedValue({
            providers: [{
                name: 'WorkBuddy',
                url: `${apiURL}/`,
                key: apiKey,
                model: 'hy3',
                protocol: 'openai',
                connection_test_passed: true,
            }],
        });
        fetchModelsMock.mockResolvedValue([{ id: 'hy3' }, { id: 'deepseek-v4' }]);
        listModelsMock.mockResolvedValue({
            models: [
                {
                    id: 'm1',
                    model_name: 'hy3',
                    enabled: true,
                    available: true,
                    tier: 'mid',
                    tier_multiplier: 1,
                    share_window: { days: [1, 2, 3, 4, 5], start: '22:00', end: '08:00' },
                },
                { id: 'm-old', model_name: 'retired', enabled: true, available: true },
                { id: 'm-off', model_name: 'gone', enabled: false, available: false },
            ],
        });
        const { getByRole, getByText, queryByRole } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Adjust models')).toBeTruthy());
        fireEvent.click(getByText('Adjust models'));
        const dialog = await waitFor(() => getByRole('dialog', { name: 'Adjust models' }));
        expect((getByRole('checkbox', { name: 'hy3' }) as HTMLInputElement).checked).toBe(true);
        expect((getByRole('checkbox', { name: 'retired' }) as HTMLInputElement).checked).toBe(true);
        expect((getByRole('checkbox', { name: 'deepseek-v4' }) as HTMLInputElement).checked).toBe(false);
        expect(queryByRole('checkbox', { name: 'gone' })).toBeNull();
        expect((dialog.querySelector('input[aria-label="hy3 start"]') as HTMLInputElement).value).toBe('22:00');

        fireEvent.click(getByRole('checkbox', { name: 'retired' }));
        fireEvent.click(getByRole('checkbox', { name: 'deepseek-v4' }));
        fireEvent.click(getByRole('button', { name: 'Save model range' }));
        await waitFor(() => expect(syncModelsMock).toHaveBeenCalledTimes(1));
        const [shareID, models] = syncModelsMock.mock.calls[0];
        expect(shareID).toBe('sh_1');
        expect(models.map((entry: { model: string }) => entry.model)).toEqual(['hy3', 'deepseek-v4']);
        expect(models[0].share_window).toEqual({ days: [1, 2, 3, 4, 5], start: '22:00', end: '08:00' });
        expect(models[1].share_window).toBeUndefined();
    });

    it('refuses to adjust models when this machine has no matching provider key', async () => {
        listSharesMock.mockResolvedValue({
            shares: [{ ...shareFixture, api_url: 'https://api.example/v1', key_fingerprint: 'not-the-local-key' }],
        });
        listProvidersMock.mockResolvedValue({
            providers: [{
                name: 'WorkBuddy',
                url: 'https://api.example/v1',
                key: 'sk-other',
                model: 'hy3',
                connection_test_passed: true,
            }],
        });
        const { getByText, queryByRole } = render(<TokenBankPanel lang="en" />);
        await waitFor(() => expect(getByText('Adjust models')).toBeTruthy());
        fireEvent.click(getByText('Adjust models'));
        await waitFor(() => expect(showAlertMock).toHaveBeenCalled());
        expect(String(showAlertMock.mock.calls[0][0])).toContain('no saved provider');
        expect(queryByRole('dialog', { name: 'Adjust models' })).toBeNull();
        expect(syncModelsMock).not.toHaveBeenCalled();
    });
});
