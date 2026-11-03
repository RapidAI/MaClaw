/** @vitest-environment jsdom */
import { StrictMode } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import * as AppAPI from '../../../wailsjs/go/main/App';
import { DialogProvider } from '../CustomDialog';
import { ExpertMarketDialog } from '../ExpertMarketDialog';
import { OPEN_SETTINGS_EVENT } from '../../utils/settingsNavigation';

vi.mock('../../../wailsjs/go/main/App', () => ({
	GetExpertMarketAccount: vi.fn(),
	InstallExpertMarketListing: vi.fn(),
	ListExpertMarketListings: vi.fn(),
	DeletePrivateExpertMarketListing: vi.fn(),
    MakeExpertMarketListingPrivate: vi.fn(),
    PurchaseExpertMarketListing: vi.fn(),
    PublishExpertMarketListing: vi.fn(),
    SubmitExpertMarketListing: vi.fn(),
    UninstallExpertMarketListing: vi.fn(),
    WithdrawExpertMarketListing: vi.fn(),
}));

vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: vi.fn(() => () => {}) }));

const listing = {
    id: 'expert-owned', name: 'Reviewed analyst', description: 'Turns source data into reviewable analysis.',
    icon: 'A', version: '1.0.0', price: 20, owned: true,
};

describe('ExpertMarketDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(AppAPI.ListExpertMarketListings).mockResolvedValue({ experts: [listing] });
        vi.mocked(AppAPI.GetExpertMarketAccount).mockResolvedValue({ credits: 80, uploads: [], purchases: [] });
        vi.mocked(AppAPI.InstallExpertMarketListing).mockResolvedValue({ expert: { id: 'pkgexp-owned' } } as any);
    });
    afterEach(() => cleanup());

    it('opens Settings > Asset Management from the credits balance', () => {
        const onClose = vi.fn();
        const listener = vi.fn();
        const trigger = document.createElement('button');
        document.body.appendChild(trigger);
        trigger.focus();
        window.addEventListener(OPEN_SETTINGS_EVENT, listener, { once: true });
        const view = render(<DialogProvider><ExpertMarketDialog lang="en" onClose={onClose} /></DialogProvider>);

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

    it('hands the installed expert to the local list as soon as installation returns', async () => {
        const onInstalled = vi.fn();
        const installedListener = vi.fn();
        window.addEventListener('maclaw:expert-installed', installedListener);
        vi.mocked(AppAPI.InstallExpertMarketListing).mockResolvedValue({
            expert: { id: 'pkgexp-owned', name: 'Reviewed analyst', description: 'Turns source data into reviewable analysis.', icon: 'A' },
        } as any);
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} onInstalled={onInstalled} /></DialogProvider>);
        const install = await screen.findByRole('button', { name: 'Install' });
        await act(async () => { fireEvent.click(install); });
        await waitFor(() => expect(onInstalled).toHaveBeenCalledWith(expect.objectContaining({
            id: 'pkgexp-owned',
            name: 'Reviewed analyst',
            description: 'Turns source data into reviewable analysis.',
        })));
        expect(installedListener).toHaveBeenCalledTimes(1);
        expect((installedListener.mock.calls[0][0] as CustomEvent).detail.expert).toMatchObject({ id: 'pkgexp-owned', name: 'Reviewed analyst' });
        window.removeEventListener('maclaw:expert-installed', installedListener);
    });

    it('uses the catalogue entitlement flag to offer install without another purchase', async () => {
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const install = await screen.findByRole('button', { name: 'Install' });
        await act(async () => { fireEvent.click(install); });
        await waitFor(() => expect(AppAPI.InstallExpertMarketListing).toHaveBeenCalledWith('expert-owned'));
        expect(AppAPI.PurchaseExpertMarketListing).not.toHaveBeenCalled();
    });

    it('keeps the compact market card footprint while listings are loading', () => {
        vi.mocked(AppAPI.ListExpertMarketListings).mockReturnValue(new Promise(() => {}));
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        expect(screen.getByRole('status', { name: 'Loading AI Expert Market' })).toBeTruthy();
        expect(document.querySelector('.expert-market-card--skeleton')).toBeTruthy();
    });

    it('loads market data when mounted through React Strict Mode', async () => {
        render(<StrictMode><DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider></StrictMode>);
        expect(await screen.findByRole('button', { name: 'Install' })).toBeTruthy();
        expect(AppAPI.ListExpertMarketListings).toHaveBeenCalled();
        expect(AppAPI.GetExpertMarketAccount).toHaveBeenCalled();
    });

    it('switches an installed expert to Uninstall immediately without waiting for account refresh', async () => {
        let resolveAccount!: (value: Record<string, any>) => void;
        vi.mocked(AppAPI.GetExpertMarketAccount).mockReturnValue(new Promise(resolve => { resolveAccount = resolve; }) as any);
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const install = await screen.findByRole('button', { name: 'Install' });
        await act(async () => { fireEvent.click(install); });
        expect(await screen.findByRole('button', { name: 'Uninstall' })).toBeTruthy();
        await act(async () => { resolveAccount({ credits: 80, uploads: [], purchases: [] }); });
    });

    it('keeps the local install state when a stale account refresh finishes after installation', async () => {
        vi.mocked(AppAPI.GetExpertMarketAccount)
            .mockResolvedValueOnce({ credits: 80, uploads: [], purchases: [] })
            .mockResolvedValueOnce({ credits: 80, uploads: [], purchases: [] });
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const install = await screen.findByRole('button', { name: 'Install' });
        await act(async () => { fireEvent.click(install); });
        await waitFor(() => expect(AppAPI.GetExpertMarketAccount).toHaveBeenCalledTimes(2));
        expect(await screen.findByRole('button', { name: 'Uninstall' })).toBeTruthy();
    });

    it('does not report success or hide the retry action when installation lacks a local expert ID', async () => {
        vi.mocked(AppAPI.InstallExpertMarketListing).mockResolvedValue({ expert: {} } as any);
        const onInstalled = vi.fn();
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} onInstalled={onInstalled} /></DialogProvider>);

        fireEvent.click(await screen.findByRole('button', { name: 'Install' }));
        expect((await screen.findByRole('dialog', { name: 'Install failed' })).textContent).toContain('Installation did not return a local expert ID. Please try again.');
        expect(onInstalled).not.toHaveBeenCalled();
        expect(screen.getByRole('button', { name: 'Install' })).toBeTruthy();
    });


    it('shows Uninstall for a locally installed market expert and removes only its local definition', async () => {
        vi.mocked(AppAPI.ListExpertMarketListings).mockResolvedValue({ experts: [{ ...listing, installed: true, local_expert_id: 'pkgexp-owned' }] });
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const uninstall = await screen.findByRole('button', { name: 'Uninstall' });
        await act(async () => { fireEvent.click(uninstall); });
        const confirm = await screen.findByRole('dialog', { name: 'Uninstall AI Expert' });
        await act(async () => { fireEvent.click(confirm.querySelector('.modal-footer button:last-child')!); });
        await waitFor(() => expect(AppAPI.UninstallExpertMarketListing).toHaveBeenCalledWith('pkgexp-owned'));
        expect(AppAPI.InstallExpertMarketListing).not.toHaveBeenCalled();
        expect(await screen.findByRole('button', { name: 'Install' })).toBeTruthy();
    });


    it('reports the removed local expert ID to the owning utilities surface', async () => {
        const onUninstalled = vi.fn();
        vi.mocked(AppAPI.ListExpertMarketListings).mockResolvedValue({ experts: [{ ...listing, installed: true, local_expert_id: 'pkgexp-owned' }] });
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} onUninstalled={onUninstalled} /></DialogProvider>);
        await waitFor(() => expect(screen.getByRole('button', { name: 'Uninstall' })).toBeTruthy());
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Uninstall' })); });
        const confirm = await screen.findByRole('dialog', { name: 'Uninstall AI Expert' });
        await act(async () => { fireEvent.click(confirm.querySelector('.modal-footer button:last-child')!); });
        await waitFor(() => expect(onUninstalled).toHaveBeenCalledWith('pkgexp-owned'));
    });

    it('does not notify the installer callback after an uninstall', async () => {
        const onInstalled = vi.fn();
        vi.mocked(AppAPI.ListExpertMarketListings).mockResolvedValue({ experts: [{ ...listing, installed: true, local_expert_id: 'pkgexp-owned' }] });
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} onInstalled={onInstalled} /></DialogProvider>);
        await waitFor(() => expect(screen.getByRole('button', { name: 'Uninstall' })).toBeTruthy());
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Uninstall' })); });
        const confirm = await screen.findByRole('dialog', { name: 'Uninstall AI Expert' });
        await act(async () => { fireEvent.click(confirm.querySelector('.modal-footer button:last-child')!); });
        await waitFor(() => expect(AppAPI.UninstallExpertMarketListing).toHaveBeenCalledWith('pkgexp-owned'));
        expect(onInstalled).not.toHaveBeenCalled();
    });

    it('continues from a successful purchase into installation in one action', async () => {
        vi.mocked(AppAPI.ListExpertMarketListings).mockResolvedValue({ experts: [{ ...listing, owned: false }] });
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const get = await screen.findByRole('button', { name: 'Get' });
        await act(async () => { fireEvent.click(get); });
        const confirm = await screen.findByRole('dialog', { name: 'Get AI Expert' });
        await act(async () => { fireEvent.click(confirm.querySelector('.modal-footer button:last-child')!); });
        await waitFor(() => expect(AppAPI.PurchaseExpertMarketListing).toHaveBeenCalledWith('expert-owned'));
        await waitFor(() => expect(AppAPI.InstallExpertMarketListing).toHaveBeenCalledWith('expert-owned'));
    });

    it('prevents duplicate get requests while its confirmation is open', async () => {
        vi.mocked(AppAPI.ListExpertMarketListings).mockResolvedValue({ experts: [{ ...listing, owned: false }] });
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const get = await screen.findByRole('button', { name: 'Get' });
        fireEvent.click(get);
        fireEvent.click(get);
        expect(await screen.findByRole('dialog', { name: 'Get AI Expert' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Get' }).hasAttribute('disabled')).toBe(true);
        const confirm = document.querySelector('.modal-footer button:last-child')!;
        await act(async () => { fireEvent.click(confirm); });
        await waitFor(() => expect(AppAPI.PurchaseExpertMarketListing).toHaveBeenCalledTimes(1));
    });

    it('renders installation errors as dialog content instead of the dialog title', async () => {
        vi.mocked(AppAPI.InstallExpertMarketListing).mockRejectedValue(new Error('dependency policy blocked installation'));
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const install = await screen.findByRole('button', { name: 'Install' });
        await act(async () => { fireEvent.click(install); });
        const alert = await screen.findByRole('dialog', { name: 'Install failed' });
        expect(alert.textContent).toContain('dependency policy blocked installation');
    });

    it('keeps a completed purchase installable when the local installation fails', async () => {
        vi.mocked(AppAPI.ListExpertMarketListings).mockResolvedValue({ experts: [{ ...listing, owned: false }] });
        vi.mocked(AppAPI.GetExpertMarketAccount)
            .mockResolvedValueOnce({ credits: 80, uploads: [], purchases: [] })
            .mockResolvedValueOnce({ credits: 60, uploads: [], purchases: [listing] });
        vi.mocked(AppAPI.InstallExpertMarketListing).mockRejectedValue(new Error('dependency policy blocked installation'));
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const get = await screen.findByRole('button', { name: 'Get' });
        await act(async () => { fireEvent.click(get); });
        const confirm = await screen.findByRole('dialog', { name: 'Get AI Expert' });
        await act(async () => { fireEvent.click(confirm.querySelector('.modal-footer button:last-child')!); });
        await waitFor(() => expect(AppAPI.InstallExpertMarketListing).toHaveBeenCalledWith('expert-owned'));
        await waitFor(() => expect(AppAPI.GetExpertMarketAccount).toHaveBeenCalledTimes(2));
        expect(await screen.findByRole('button', { name: 'Install' })).toBeTruthy();
    });

    it('offers a retryable error state when the catalogue cannot load', async () => {
        vi.mocked(AppAPI.ListExpertMarketListings).mockRejectedValue(new Error('network unavailable'));
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const alert = await screen.findByRole('alert');
        expect(alert.textContent).toContain('network unavailable');
        fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
        await waitFor(() => expect(AppAPI.ListExpertMarketListings).toHaveBeenCalledTimes(2));
    });

    it('explains a Hub-verified email that is already bound to another market account', async () => {
        vi.mocked(AppAPI.ListExpertMarketListings).mockRejectedValue(new Error('Expert Market session refresh failed: account email is already bound to another user'));
        render(<DialogProvider><ExpertMarketDialog lang="zh-Hans" onClose={vi.fn()} /></DialogProvider>);
        const alert = await screen.findByRole('alert');
        expect(alert.textContent).toContain('该邮箱已绑定其他市场账号');
    });

    it('keeps the newest catalogue response when an earlier refresh finishes late', async () => {
        let resolveFirst!: (value: Record<string, any>) => void;
        const first = new Promise<Record<string, any>>(resolve => { resolveFirst = resolve; });
        vi.mocked(AppAPI.ListExpertMarketListings)
            .mockReturnValueOnce(first)
            .mockResolvedValueOnce({ experts: [{ ...listing, id: 'expert-new', name: 'Newest result' }] });
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        await waitFor(() => expect(AppAPI.ListExpertMarketListings).toHaveBeenCalledTimes(1));
        fireEvent.change(screen.getByRole('textbox', { name: 'Search experts' }), { target: { value: 'new' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search' }));
        expect(await screen.findByText('Newest result')).toBeTruthy();
        await act(async () => { resolveFirst({ experts: [{ ...listing, id: 'expert-old', name: 'Stale result' }] }); });
        expect(screen.queryByText('Stale result')).toBeNull();
        expect(screen.getByText('Newest result')).toBeTruthy();
    });

    it('does not reload account data for catalogue-only searches', async () => {
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        await screen.findByRole('button', { name: 'Install' });
        await waitFor(() => expect(AppAPI.GetExpertMarketAccount).toHaveBeenCalledTimes(1));
        fireEvent.change(screen.getByRole('textbox', { name: 'Search experts' }), { target: { value: 'analysis' } });
        fireEvent.click(screen.getByRole('button', { name: 'Search' }));
        await waitFor(() => expect(AppAPI.ListExpertMarketListings).toHaveBeenCalledTimes(2));
        expect(AppAPI.GetExpertMarketAccount).toHaveBeenCalledTimes(1);
    });

    it('exposes the active market section as an accessible tab panel', async () => {
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const explore = screen.getByRole('tab', { name: 'Explore' });
        const library = screen.getByRole('tab', { name: 'My library' });

        expect(explore.getAttribute('aria-selected')).toBe('true');
        expect(library.getAttribute('aria-selected')).toBe('false');
        expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe(explore.id);

        fireEvent.click(library);
        expect(library.getAttribute('aria-selected')).toBe('true');
        expect(explore.getAttribute('aria-selected')).toBe('false');
        expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe(library.id);
    });

    it('supports arrow and home/end navigation for market tabs', () => {
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const explore = screen.getByRole('tab', { name: 'Explore' });
        const library = screen.getByRole('tab', { name: 'My library' });

        fireEvent.keyDown(explore, { key: 'ArrowRight' });
        expect(library.getAttribute('aria-selected')).toBe('true');
        expect(library.tabIndex).toBe(0);
        expect(explore.tabIndex).toBe(-1);

        fireEvent.keyDown(library, { key: 'Home' });
        expect(explore.getAttribute('aria-selected')).toBe('true');

        fireEvent.keyDown(explore, { key: 'End' });
        expect(library.getAttribute('aria-selected')).toBe('true');
    });

    it('keeps the inactive roving tab out of the dialog focus cycle', () => {
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        const explore = screen.getByRole('tab', { name: 'Explore' });
        const library = screen.getByRole('tab', { name: 'My library' });
        const close = screen.getByRole('button', { name: 'Close' });

        explore.focus();
        fireEvent.keyDown(window, { key: 'Tab', shiftKey: true });
        expect(document.activeElement).toBe(close);
        expect(document.activeElement).not.toBe(library);
    });

    it('does not present an account failure as an empty submitted-experts library', async () => {
        vi.mocked(AppAPI.GetExpertMarketAccount).mockRejectedValue(new Error('account service unavailable'));
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);
        const alert = await screen.findByRole('alert');
        expect(alert.textContent).toContain('account service unavailable');
        expect(screen.queryByText('No expert submissions yet.')).toBeNull();
        fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
        await waitFor(() => expect(AppAPI.GetExpertMarketAccount).toHaveBeenCalledTimes(2));
    });

    it('does not treat a failed account request as a zero Credits balance', async () => {
        vi.mocked(AppAPI.GetExpertMarketAccount).mockRejectedValue(new Error('account service unavailable'));
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        await screen.findByRole('button', { name: 'Install' });
        fireEvent.click(screen.getByRole('tab', { name: 'My library' }));
        await screen.findByRole('alert');
        expect(document.querySelector('.expert-market-balance')?.textContent).toContain('—');
        expect(document.querySelector('.expert-market-balance')?.textContent).not.toContain('0 Credits');
    });

    it('installs an expert shared by phone or email without purchasing it', async () => {
        vi.mocked(AppAPI.GetExpertMarketAccount).mockResolvedValue({
            credits: 80,
            purchases: [],
            uploads: [],
            received: [{ ...listing, id: 'expert-shared', name: 'Shared analyst', owner_email: 'phone:8613800138000', visibility: 'users', status: 'shared' }],
        });
        render(<DialogProvider><ExpertMarketDialog lang="zh-Hans" initialTab="library" onClose={vi.fn()} /></DialogProvider>);
        expect(await screen.findByText('分享给我的专家')).toBeTruthy();
        const shared = screen.getByText('Shared analyst').closest('.expert-market-card');
        expect(shared).toBeTruthy();
        expect(shared?.textContent).toMatch(/来自 13800138000/);
        expect(shared?.closest('.expert-market-grid')).toBeTruthy();
        fireEvent.click(within(shared as HTMLElement).getByRole('button', { name: '安装' }));
        await waitFor(() => expect(AppAPI.InstallExpertMarketListing).toHaveBeenCalledWith('expert-shared'));
        expect(AppAPI.PurchaseExpertMarketListing).not.toHaveBeenCalled();
    });

    it('shows how many people a specified-user submission was shared with', async () => {
        vi.mocked(AppAPI.GetExpertMarketAccount).mockResolvedValue({
            credits: 80,
            purchases: [],
            uploads: [{ ...listing, id: 'expert-shared-out', name: 'Shared out', status: 'shared', visibility: 'users', share_targets: ['8613800138000', 'friend@example.com'] }],
        });
        render(<DialogProvider><ExpertMarketDialog lang="zh-Hans" initialTab="library" onClose={vi.fn()} /></DialogProvider>);
        const status = await screen.findByText('指定用户 · 2 人');
        expect(status.getAttribute('title')).toBe('13800138000、friend@example.com');
        expect(screen.getByRole('button', { name: '删除指定分享' })).toBeTruthy();
    });

    it('shows a submitted expert from the account response in My library', async () => {
        vi.mocked(AppAPI.GetExpertMarketAccount).mockResolvedValue({
            credits: 80,
            purchases: [],
            uploads: [{ ...listing, id: 'expert-submitted', name: 'My pending expert', status: 'pending_review' }],
        });
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);
        expect(await screen.findByText('My pending expert')).toBeTruthy();
        expect(screen.getByText('Pending review')).toBeTruthy();
    });

    it('offers Install for a submitted expert that is no longer installed locally', async () => {
        vi.mocked(AppAPI.GetExpertMarketAccount).mockResolvedValue({
            credits: 80,
            purchases: [],
            uploads: [{ ...listing, id: 'expert-submitted', name: 'My private expert', status: 'private' }],
        });
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);

        fireEvent.click(await screen.findByRole('button', { name: 'Install' }));
        await waitFor(() => expect(AppAPI.InstallExpertMarketListing).toHaveBeenCalledWith('expert-submitted'));
        expect(within(screen.getByRole('group', { name: 'Actions for My private expert' })).queryByRole('button', { name: 'Install' })).toBeNull();
    });

    it('keeps a restored submitted expert installed when a stale account refresh completes', async () => {
        const submitted = { ...listing, id: 'expert-submitted', name: 'My submitted expert', status: 'private' };
        vi.mocked(AppAPI.GetExpertMarketAccount)
            .mockResolvedValueOnce({ credits: 80, purchases: [], uploads: [submitted] })
            .mockResolvedValueOnce({ credits: 80, purchases: [], uploads: [submitted] });
        vi.mocked(AppAPI.InstallExpertMarketListing).mockResolvedValue({ expert: { id: 'pkgexp-submitted' } } as any);
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);

        fireEvent.click(await screen.findByRole('button', { name: 'Install' }));
        await waitFor(() => expect(AppAPI.GetExpertMarketAccount).toHaveBeenCalledTimes(2));
        expect(within(screen.getByRole('group', { name: 'Actions for My submitted expert' })).queryByRole('button', { name: 'Install' })).toBeNull();
    });

    it('shows each library list as three rows per page and paginates the rest', async () => {
        const uploads = Array.from({ length: 13 }, (_, index) => ({
            ...listing,
            id: `expert-submitted-${index + 1}`,
            name: `Submitted expert ${index + 1}`,
            status: 'listed',
        }));
        const received = Array.from({ length: 13 }, (_, index) => ({
            ...listing,
            id: `expert-received-${index + 1}`,
            name: `Received expert ${index + 1}`,
            owner_email: 'friend@example.com',
            visibility: 'users',
            status: 'shared',
        }));
        vi.mocked(AppAPI.GetExpertMarketAccount).mockResolvedValue({ credits: 80, purchases: [], uploads, received });
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);
        expect(await screen.findByText('Submitted expert 12')).toBeTruthy();
        expect(screen.getByText('Received expert 12')).toBeTruthy();
        expect(screen.queryByText('Submitted expert 13')).toBeNull();
        expect(screen.queryByText('Received expert 13')).toBeNull();
        expect(screen.getByText('Received expert 1').closest('.expert-market-card')).toBeTruthy();
        expect(screen.getAllByText('13 total')).toHaveLength(2);

        fireEvent.click(within(screen.getByRole('navigation', { name: 'My submissions pages' })).getByRole('button', { name: 'Next' }));
        expect(await screen.findByText('Submitted expert 13')).toBeTruthy();
        expect(screen.queryByText('Submitted expert 1')).toBeNull();
        expect(screen.getByText('Received expert 1')).toBeTruthy();

        fireEvent.click(within(screen.getByRole('navigation', { name: 'Shared with me pages' })).getByRole('button', { name: 'Next' }));
        expect(await screen.findByText('Received expert 13')).toBeTruthy();
        expect(screen.queryByText('Received expert 1')).toBeNull();
    });

    it('returns to the last catalogue page instead of leaving the grid empty', async () => {
        let requests = 0;
        vi.mocked(AppAPI.ListExpertMarketListings).mockImplementation(async (_query, _sort, page) => {
            requests += 1;
            if (requests === 1) return { experts: [{ ...listing, name: 'First page expert' }], total_pages: 2 };
            if (Number(page) > 1) return { experts: [], total_pages: 1 };
            return { experts: [{ ...listing, name: 'First page expert' }], total_pages: 1 };
        });
        render(<DialogProvider><ExpertMarketDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        expect(await screen.findByText('First page expert')).toBeTruthy();
        fireEvent.click(within(screen.getByRole('navigation', { name: 'Expert market pages' })).getByRole('button', { name: 'Next' }));
        await waitFor(() => {
            const calls = vi.mocked(AppAPI.ListExpertMarketListings).mock.calls;
            expect(calls.length).toBeGreaterThanOrEqual(3);
            expect(calls.at(-1)?.[2]).toBe(1);
        });
        expect(screen.getByText('First page expert')).toBeTruthy();
        expect(screen.queryByText('No AI experts match your search.')).toBeNull();
    });

    it('returns to the last valid submissions page after account reconciliation shrinks the list', async () => {
        const nineUploads = Array.from({ length: 13 }, (_, index) => ({
            ...listing,
            id: `expert-submitted-${index + 1}`,
            name: `Submitted expert ${index + 1}`,
            status: 'listed',
        }));
        const eightUploads = nineUploads.slice(0, 12);
        vi.mocked(AppAPI.GetExpertMarketAccount)
            .mockResolvedValueOnce({ credits: 80, purchases: [listing], uploads: nineUploads })
            .mockResolvedValueOnce({ credits: 80, purchases: [listing], uploads: eightUploads });
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);
        fireEvent.click(await screen.findByRole('button', { name: 'Next' }));
        expect(await screen.findByText('Submitted expert 13')).toBeTruthy();

        fireEvent.click(within(screen.getByText('Reviewed analyst').closest('.expert-market-card')!).getByRole('button', { name: 'Install' }));
        await waitFor(() => expect(AppAPI.GetExpertMarketAccount).toHaveBeenCalledTimes(2));
        expect(await screen.findByText('Submitted expert 1')).toBeTruthy();
        expect(screen.queryByText('Submitted expert 13')).toBeNull();
        expect(screen.queryByLabelText('My submissions pages')).toBeNull();
    });

    it('keeps a withdrawn submission non-actionable when its account refresh is stale', async () => {
        let resolveAccount!: (value: Record<string, any>) => void;
        const submitted = { ...listing, id: 'expert-submitted', name: 'My listed expert', status: 'listed' };
        vi.mocked(AppAPI.GetExpertMarketAccount)
            .mockResolvedValueOnce({ credits: 80, purchases: [], uploads: [submitted] })
            .mockReturnValueOnce(new Promise(resolve => { resolveAccount = resolve; }) as any);
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);
        fireEvent.click(await screen.findByRole('button', { name: 'Unlist' }));
        const confirm = await screen.findByRole('dialog', { name: 'Unlist expert' });
        fireEvent.click(confirm.querySelector('.modal-footer button:last-child')!);
        await waitFor(() => expect(AppAPI.WithdrawExpertMarketListing).toHaveBeenCalledWith('expert-submitted'));

        resolveAccount({ credits: 80, purchases: [], uploads: [submitted] });
        await waitFor(() => expect(screen.queryByRole('button', { name: 'Unlist' })).toBeNull());
        expect(screen.getByText('Unlisted')).toBeTruthy();
    });

    it('makes a listed submission private immediately and keeps the owner-only state after refresh', async () => {
        const submitted = { ...listing, id: 'expert-private', name: 'My listed expert', status: 'listed', visibility: 'public' };
        vi.mocked(AppAPI.GetExpertMarketAccount).mockResolvedValue({ credits: 80, purchases: [], uploads: [submitted] });
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);

        fireEvent.click(await screen.findByRole('button', { name: 'Make private' }));
        const confirm = await screen.findByRole('dialog', { name: 'Make private' });
        fireEvent.click(confirm.querySelector('.modal-footer button:last-child')!);

        await waitFor(() => expect(AppAPI.MakeExpertMarketListingPrivate).toHaveBeenCalledWith('expert-private'));
        expect(await screen.findByText('Private · only you can see it')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Make public' })).toBeTruthy();
        expect(screen.queryByRole('button', { name: 'Unlist' })).toBeNull();
    });

	it('submits a private expert for review when it is made public', async () => {
        const submitted = { ...listing, id: 'expert-public', name: 'My private expert', status: 'private', visibility: 'private' };
        vi.mocked(AppAPI.GetExpertMarketAccount).mockResolvedValue({ credits: 80, purchases: [], uploads: [submitted] });
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);

        fireEvent.click(await screen.findByRole('button', { name: 'Make public' }));
        const confirm = await screen.findByRole('dialog', { name: 'Make AI Expert public' });
        fireEvent.click(confirm.querySelector('.modal-footer button:last-child')!);

        await waitFor(() => expect(AppAPI.PublishExpertMarketListing).toHaveBeenCalledWith('expert-public'));
        expect(await screen.findByText('Pending review')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Make private' })).toBeTruthy();
		expect(screen.queryByRole('button', { name: 'Make public' })).toBeNull();
	});

	it('permanently removes a private share after confirmation', async () => {
		const submitted = { ...listing, id: 'expert-private-delete', name: 'My private expert', status: 'private', visibility: 'private' };
		vi.mocked(AppAPI.GetExpertMarketAccount).mockResolvedValue({ credits: 80, purchases: [], uploads: [submitted] });
		render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);

		fireEvent.click(await screen.findByRole('button', { name: 'Delete private share' }));
		const confirm = await screen.findByRole('dialog', { name: 'Delete private share' });
		fireEvent.click(confirm.querySelector('.modal-footer button:last-child')!);

		await waitFor(() => expect(AppAPI.DeletePrivateExpertMarketListing).toHaveBeenCalledWith('expert-private-delete'));
		expect(screen.queryByText('My private expert')).toBeNull();
	});

    it('keeps the last confirmed library visible when a background account refresh fails', async () => {
        vi.mocked(AppAPI.GetExpertMarketAccount)
            .mockResolvedValueOnce({ credits: 80, uploads: [{ ...listing, id: 'expert-submitted', name: 'My pending expert', status: 'pending_review' }], purchases: [listing] })
            .mockRejectedValueOnce(new Error('temporary account outage'));
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);
        const install = within((await screen.findByText('Reviewed analyst')).closest('.expert-market-card')!).getByRole('button', { name: 'Install' });
        await act(async () => { fireEvent.click(install); });
        await waitFor(() => expect(AppAPI.GetExpertMarketAccount).toHaveBeenCalledTimes(2));
        expect(screen.getByText('My pending expert')).toBeTruthy();
        expect(screen.queryByRole('alert')).toBeNull();
    });

    it('closes on Escape and restores focus to the market entry point', async () => {
        const onClose = vi.fn();
        const trigger = document.createElement('button');
        document.body.appendChild(trigger);
        trigger.focus();
        const view = render(<DialogProvider><ExpertMarketDialog lang="en" onClose={onClose} /></DialogProvider>);
        await screen.findByRole('button', { name: 'Install' });
        await act(async () => { fireEvent.keyDown(window, { key: 'Escape' }); });
        expect(onClose).toHaveBeenCalledTimes(1);
        view.unmount();
        expect(document.activeElement).toBe(trigger);
        trigger.remove();
    });
});
