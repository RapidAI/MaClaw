/** @vitest-environment jsdom */
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import * as AppAPI from '../../../wailsjs/go/main/App';
import { DialogProvider } from '../CustomDialog';
import { PetStoreDialog } from '../PetStoreDialog';
import { OPEN_SETTINGS_EVENT } from '../../utils/settingsNavigation';

const account = { credits: 25, user: { email: 'reader@example.test' }, uploads: [], purchases: [] };
const report = { paid_summary: { sales_amount: 0, sales_count: 0, paid_pack_count: 0 }, previous_paid_summary: { sales_amount: 0, sales_count: 0 }, paid_packs: [], free_download_packs: [] };
const pack = { id: 'pet-one', source_pack_id: 'pet-one-source', name: 'Pet One', description: 'A test pet.', price: 3, download_count: 0, sales_amount: 0 };

vi.mock('../../../wailsjs/go/main/App', () => ({
    GetPetStoreAccount: vi.fn().mockResolvedValue({ credits: 25, user: { email: 'reader@example.test' }, uploads: [], purchases: [] }),
    GetPetStoreCreatorReport: vi.fn().mockResolvedValue({ paid_summary: { sales_amount: 0, sales_count: 0, paid_pack_count: 0 }, previous_paid_summary: { sales_amount: 0, sales_count: 0 }, paid_packs: [], free_download_packs: [] }),
    GetPetStoreRankings: vi.fn().mockResolvedValue({ creators: [], downloads: [], sales: [] }),
    InstallPetStorePack: vi.fn().mockResolvedValue('pet-one'),
    IsPetStorePackInstalled: vi.fn().mockResolvedValue(false),
    ListPetStorePacks: vi.fn().mockResolvedValue({ packs: [{ id: 'pet-one', source_pack_id: 'pet-one-source', name: 'Pet One', description: 'A test pet.', price: 3, download_count: 0, sales_amount: 0 }], total_pages: 1 }),
    PurchasePetStorePack: vi.fn().mockResolvedValue(undefined),
    WithdrawPetStorePack: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: vi.fn().mockReturnValue(() => {}) }));

function renderStore(lang = 'en') {
    return render(<DialogProvider><PetStoreDialog lang={lang} onClose={vi.fn()} /></DialogProvider>);
}

function accountButton() {
    return document.querySelector<HTMLButtonElement>('.pet-store-account-button')!;
}

describe('PetStoreDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue(account);
        vi.mocked(AppAPI.GetPetStoreCreatorReport).mockResolvedValue(report);
        vi.mocked(AppAPI.GetPetStoreRankings).mockResolvedValue({ creators: [], downloads: [], sales: [] });
        vi.mocked(AppAPI.InstallPetStorePack).mockResolvedValue('pet-one');
        vi.mocked(AppAPI.IsPetStorePackInstalled).mockResolvedValue(false);
        vi.mocked(AppAPI.ListPetStorePacks).mockResolvedValue({ packs: [pack], total_pages: 1 });
    });
    afterEach(() => cleanup());

    it('opens Settings > Asset Management from the credits balance', () => {
        const onClose = vi.fn();
        const listener = vi.fn();
        const trigger = document.createElement('button');
        document.body.appendChild(trigger);
        trigger.focus();
        window.addEventListener(OPEN_SETTINGS_EVENT, listener, { once: true });
        const view = render(<DialogProvider><PetStoreDialog lang="en" onClose={onClose} /></DialogProvider>);

        const assetButton = screen.getByRole('button', { name: 'Asset Management' });
        assetButton.focus();
        fireEvent.click(assetButton);
        view.unmount();

        expect(onClose).toHaveBeenCalledTimes(1);
        expect(listener).toHaveBeenCalledTimes(1);
        expect((listener.mock.calls[0][0] as CustomEvent).detail).toEqual({ tab: 'assetManagement' });
        expect(document.activeElement).not.toBe(trigger);
        trigger.remove();
    });

    it('keeps the market open when Escape dismisses its purchase confirmation', async () => {
        const onClose = vi.fn();
        render(<DialogProvider><PetStoreDialog lang="en" onClose={onClose} /></DialogProvider>);
        await waitFor(() => expect(screen.getByRole('button', { name: 'Get' })).toBeTruthy());
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Get' })); });
        await act(async () => { fireEvent.keyDown(window, { key: 'Escape' }); });
        expect(screen.getByRole('dialog', { name: 'Pet Store' })).toBeTruthy();
        expect(onClose).not.toHaveBeenCalled();
    });

    it('localizes the duplicate-purchase error in the Pet Store', async () => {
        vi.mocked(AppAPI.PurchasePetStorePack).mockRejectedValue(new Error('Pet Store request failed: you already own this pet pack'));
        renderStore('zh-Hans');
        await waitFor(() => expect(screen.getByRole('button', { name: '获取' })).toBeTruthy());
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: '获取' })); });
        await act(async () => { fireEvent.click(screen.getByRole('dialog', { name: '确认购买' }).querySelector('.modal-footer button:last-child')!); });
        await waitFor(() => expect(screen.getByRole('dialog', { name: '购买失败' }).textContent).toContain('您已拥有此宠物包，无需重复获取。'));
    });

    it('keeps keyboard navigation within the visible account tabs', async () => {
        renderStore();
        await act(async () => { fireEvent.click(accountButton()); });
        const uploads = await screen.findByRole('tab', { name: 'Uploads' });
        const tablist = screen.getByRole('tablist', { name: 'Account panels' });
        expect(tablist.children).toHaveLength(2);
        expect(screen.getByRole('tabpanel', { name: 'Uploads' })).toBeTruthy();
        uploads.focus();
        await act(async () => { fireEvent.keyDown(uploads, { key: 'ArrowRight' }); });
        const purchased = screen.getByRole('tab', { name: 'Purchased' });
        expect(document.activeElement).toBe(purchased);
        expect(purchased.getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('tabpanel', { name: 'Purchased' })).toBeTruthy();
    });

    it('shows account contact only once and never exposes an internal user ID', async () => {
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue({
            ...account,
            user: { id: 'u_1783155920697276064_9', email: 'creator@example.test' },
        });
        renderStore();
        expect(await screen.findByText('creator@example.test')).toBeTruthy();
        expect(screen.getAllByText('creator@example.test')).toHaveLength(1);
        expect(screen.queryByText('u_1783155920697276064_9')).toBeNull();
        expect(accountButton().textContent).toBe('My market');
    });

    it('uses a phone number when HubCenter has no usable email address', async () => {
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue({
            ...account,
            user: { id: 'u_internal', email: 'u_internal', phone_number: '+86 138 0013 8000' },
        });
        renderStore();
        expect(await screen.findByText('+86 138 0013 8000')).toBeTruthy();
        expect(screen.queryByText('u_internal')).toBeNull();
    });

    it('defers preview decoding for pet cards below the first row', async () => {
        vi.mocked(AppAPI.ListPetStorePacks).mockResolvedValue({
            packs: Array.from({ length: 5 }, (_, index) => ({
                ...pack,
                id: `pet-${index}`,
                source_pack_id: `pet-source-${index}`,
                name: `Pet ${index + 1}`,
                preview_data_url: 'data:image/svg+xml;base64,PHN2Zy8+',
            })),
            total_pages: 1,
        });
        renderStore();
        expect((await screen.findByAltText('Pet 1 preview')).getAttribute('loading')).toBe('eager');
        expect(screen.getByAltText('Pet 5 preview').getAttribute('loading')).toBe('lazy');
        expect(screen.getByAltText('Pet 5 preview').getAttribute('decoding')).toBe('async');
    });

    it('does not repeat native installation checks when account data refreshes without new packs', async () => {
        renderStore();
        await waitFor(() => expect(AppAPI.IsPetStorePackInstalled).toHaveBeenCalledWith('pet-one-source'));
        expect(AppAPI.IsPetStorePackInstalled).toHaveBeenCalledTimes(1);
        await act(async () => { fireEvent.click(accountButton()); });
        await waitFor(() => expect(AppAPI.GetPetStoreAccount).toHaveBeenCalledTimes(2));
        expect(AppAPI.IsPetStorePackInstalled).toHaveBeenCalledTimes(1);
    });

    it('reuses installation checks for packs that appear on another result page', async () => {
        vi.mocked(AppAPI.ListPetStorePacks).mockImplementation(async (_query, _sort, _order, page) => ({
            packs: page === 1
                ? [pack]
                : [pack, { ...pack, id: 'pet-two', source_pack_id: 'pet-two-source', name: 'Pet Two' }],
            total_pages: 2,
        }));
        renderStore();
        await waitFor(() => expect(AppAPI.IsPetStorePackInstalled).toHaveBeenCalledWith('pet-one-source'));
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Next' })); });
        await waitFor(() => expect(screen.getByText('Pet Two')).toBeTruthy());
        expect(AppAPI.IsPetStorePackInstalled).toHaveBeenCalledTimes(2);
        expect(AppAPI.IsPetStorePackInstalled).toHaveBeenCalledWith('pet-two-source');
    });

    it('does not render deleted listings in My uploads when older account data includes them', async () => {
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue({
            ...account,
            uploads: [
                { id: 'upload-active', source_pack_id: 'upload-active-source', name: 'Visible upload', status: 'active', download_count: 2 },
                { id: 'upload-deleted', source_pack_id: 'upload-deleted-source', name: 'Deleted upload', status: 'deleted', download_count: 0 },
            ], purchases: [{ pack: { id: 'purchase-withdrawn', source_pack_id: 'purchase-withdrawn-source', name: 'Unavailable purchase', status: 'withdrawn' } }],
        });
        renderStore();
        await act(async () => { fireEvent.click(accountButton()); });
        expect(await screen.findByText('Visible upload')).toBeTruthy();
        expect(screen.queryByText('Deleted upload')).toBeNull();
        expect(AppAPI.IsPetStorePackInstalled).not.toHaveBeenCalledWith('upload-deleted-source');
        // Withdrawn purchases stay downloadable for buyers, so their native
        // installation state is probed like any installable pack.
        expect(AppAPI.IsPetStorePackInstalled).toHaveBeenCalledWith('purchase-withdrawn-source');
    });

    it('recognizes a re-issued listing as owned by its stable source ID', async () => {
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue({
            ...account,
            purchases: [{ pack: { id: 'old-listing', source_pack_id: 'pet-one-source', name: 'Earlier release', status: 'withdrawn' } }],
        });
        renderStore();
        await waitFor(() => expect(screen.getByRole('button', { name: 'Install' })).toBeTruthy());
        expect(screen.queryByRole('button', { name: 'Get' })).toBeNull();
    });

    it('keeps the newest creator report when earlier requests finish later', async () => {
        let resolveFirst: (value: Record<string, any>) => void;
        const first = new Promise<Record<string, any>>(resolve => { resolveFirst = resolve; });
        vi.mocked(AppAPI.GetPetStoreCreatorReport)
            .mockReturnValueOnce(first)
            .mockResolvedValueOnce({ ...report, paid_summary: { sales_amount: 9, sales_count: 1, paid_pack_count: 1 } });
        renderStore();
        await waitFor(() => expect(AppAPI.GetPetStoreCreatorReport).toHaveBeenCalledTimes(1));
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Refresh' })); });
        await waitFor(() => expect(screen.getAllByText((_, element) => element?.tagName === 'STRONG' && element.textContent === '9 Credits').length).toBe(1));
        await act(async () => { resolveFirst!({ ...report, paid_summary: { sales_amount: 1, sales_count: 1, paid_pack_count: 1 } }); });
        expect(screen.getAllByText((_, element) => element?.tagName === 'STRONG' && element.textContent === '9 Credits').length).toBe(1);
    });

    it('localizes the removed-pack install failure', async () => {
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue({
            ...account,
            purchases: [{ pack: { id: 'pet-one', source_pack_id: 'pet-one-source', name: 'Pet One', status: 'active' } }],
        });
        vi.mocked(AppAPI.InstallPetStorePack).mockRejectedValue(new Error('Pet Store request failed: status 410: pet pack has been removed'));
        renderStore('zh-Hans');
        await waitFor(() => expect(screen.getByRole('button', { name: '安装' })).toBeTruthy());
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: '安装' })); });
        await waitFor(() => expect(screen.getByRole('dialog', { name: '安装失败' }).textContent).toContain('该宠物包已被作者移除，无法安装。'));
    });

    it('shows a sign-in empty state instead of an error when the creator report needs sign-in', async () => {
        vi.mocked(AppAPI.GetPetStoreCreatorReport).mockRejectedValue(new Error('please sign in to HubCenter before using the Pet Store'));
        renderStore('zh-Hans');
        await waitFor(() => expect(screen.getByText('登录后查看创作者报表')).toBeTruthy());
        expect(screen.queryByRole('alert')).toBeNull();
        expect(screen.queryByText('please sign in to HubCenter before using the Pet Store')).toBeNull();
    });

    it('shows the report sign-in prompt in English', async () => {
        vi.mocked(AppAPI.GetPetStoreCreatorReport).mockRejectedValue(new Error('please sign in to HubCenter before using the Pet Store'));
        renderStore();
        await waitFor(() => expect(screen.getByText('Sign in to view your creator report')).toBeTruthy());
        expect(screen.queryByRole('alert')).toBeNull();
    });

    it('shows sign-in empty states instead of errors in the browse list and account area', async () => {
        vi.mocked(AppAPI.ListPetStorePacks).mockRejectedValue(new Error('please sign in to HubCenter before using the Pet Store'));
        vi.mocked(AppAPI.GetPetStoreAccount).mockRejectedValue(new Error('please sign in to HubCenter before using the Pet Store'));
        renderStore('zh-Hans');
        await waitFor(() => expect(screen.getByText('登录后使用宠物市场')).toBeTruthy());
        expect(screen.queryByRole('alert')).toBeNull();
        expect(screen.queryByText('重试')).toBeNull();
        await act(async () => { fireEvent.click(accountButton()); });
        await waitFor(() => expect(screen.getAllByText('登录后使用宠物市场')).toHaveLength(2));
        expect(screen.queryByRole('alert')).toBeNull();
    });

    it('keeps the HubCenter-missing error as an alert in the browse list', async () => {
        vi.mocked(AppAPI.ListPetStorePacks).mockRejectedValue(new Error('未配置 HubCenter'));
        renderStore('zh-Hans');
        await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('未配置 HubCenter，宠物市场不可用。'));
        expect(screen.queryByText('登录后使用宠物市场')).toBeNull();
    });
});
