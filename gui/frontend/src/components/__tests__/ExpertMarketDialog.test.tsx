/** @vitest-environment jsdom */
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import * as AppAPI from '../../../wailsjs/go/main/App';
import { DialogProvider } from '../CustomDialog';
import { ExpertMarketDialog } from '../ExpertMarketDialog';

vi.mock('../../../wailsjs/go/main/App', () => ({
    GetExpertMarketAccount: vi.fn(),
    InstallExpertMarketListing: vi.fn(),
    ListExpertMarketListings: vi.fn(),
    PurchaseExpertMarketListing: vi.fn(),
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

    it('does not present an account failure as an empty submitted-experts library', async () => {
        vi.mocked(AppAPI.GetExpertMarketAccount).mockRejectedValue(new Error('account service unavailable'));
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);
        const alert = await screen.findByRole('alert');
        expect(alert.textContent).toContain('account service unavailable');
        expect(screen.queryByText('No expert submissions yet.')).toBeNull();
        fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
        await waitFor(() => expect(AppAPI.GetExpertMarketAccount).toHaveBeenCalledTimes(2));
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
        expect(screen.getByText('unlisted')).toBeTruthy();
    });

    it('keeps the last confirmed library visible when a background account refresh fails', async () => {
        vi.mocked(AppAPI.GetExpertMarketAccount)
            .mockResolvedValueOnce({ credits: 80, uploads: [{ ...listing, id: 'expert-submitted', name: 'My pending expert', status: 'pending_review' }], purchases: [listing] })
            .mockRejectedValueOnce(new Error('temporary account outage'));
        render(<DialogProvider><ExpertMarketDialog lang="en" initialTab="library" onClose={vi.fn()} /></DialogProvider>);
        const install = await screen.findByRole('button', { name: 'Install' });
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
