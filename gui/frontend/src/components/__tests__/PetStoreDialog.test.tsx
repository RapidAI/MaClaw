/** @vitest-environment jsdom */
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import * as AppAPI from '../../../wailsjs/go/main/App';
import { DialogProvider } from '../CustomDialog';
import { PetStoreDialog } from '../PetStoreDialog';

vi.mock('../../../wailsjs/go/main/App', () => ({
    GetPetStoreAccount: vi.fn().mockResolvedValue({ credits: 25, user: { email: 'reader@example.test' }, uploads: [], purchases: [] }),
    GetPetStoreRankings: vi.fn().mockResolvedValue({ creators: [], downloads: [], sales: [] }),
    InstallPetStorePack: vi.fn().mockResolvedValue('pet-one'),
    ListPetStorePacks: vi.fn().mockResolvedValue({
        packs: [{ id: 'pet-one', name: 'Pet One', description: 'A test pet.', price: 3, download_count: 0, sales_amount: 0 }],
        total_pages: 1,
    }),
    PurchasePetStorePack: vi.fn().mockResolvedValue(undefined),
    SubmitPetStorePack: vi.fn().mockResolvedValue(undefined),
    WithdrawPetStorePack: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: vi.fn().mockReturnValue(() => {}) }));

describe('PetStoreDialog', () => {
    beforeEach(() => {
        vi.clearAllMocks();
        vi.mocked(AppAPI.GetPetStoreAccount).mockResolvedValue({ credits: 25, user: { email: 'reader@example.test' }, uploads: [], purchases: [] });
        vi.mocked(AppAPI.GetPetStoreRankings).mockResolvedValue({ creators: [], downloads: [], sales: [] });
        vi.mocked(AppAPI.ListPetStorePacks).mockResolvedValue({
            packs: [{ id: 'pet-one', name: 'Pet One', description: 'A test pet.', price: 3, download_count: 0, sales_amount: 0 }],
            total_pages: 1,
        });
    });
    afterEach(() => cleanup());

    it('keeps the market open when Escape dismisses its purchase confirmation', async () => {
        const onClose = vi.fn();
        render(
            <DialogProvider>
                <PetStoreDialog lang="en" onClose={onClose} />
            </DialogProvider>,
        );

        await waitFor(() => expect(screen.getByRole('button', { name: 'Get' })).toBeTruthy());
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Get' })); });
        expect(screen.getByRole('dialog', { name: 'Confirm purchase' })).toBeTruthy();

        await act(async () => { fireEvent.keyDown(window, { key: 'Escape' }); });

        await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Confirm purchase' })).toBeNull());
        expect(screen.getByRole('dialog', { name: 'Pet Store' })).toBeTruthy();
        expect(onClose).not.toHaveBeenCalled();
        expect(AppAPI.PurchasePetStorePack).not.toHaveBeenCalled();
    });

    it('shows an account retry affordance without replacing the browsing view', async () => {
        vi.mocked(AppAPI.GetPetStoreAccount).mockRejectedValueOnce(new Error('Hub unavailable'));
        render(
            <DialogProvider>
                <PetStoreDialog lang="en" onClose={vi.fn()} />
            </DialogProvider>,
        );

        await waitFor(() => expect(screen.getByRole('button', { name: 'Get' })).toBeTruthy());
        await act(async () => { fireEvent.click(screen.getByRole('button', { name: /Account/ })); });

        await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('Hub unavailable'));
        expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Get' })).toBeTruthy();
    });
});
