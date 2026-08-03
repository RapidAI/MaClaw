/** @vitest-environment jsdom */
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { DialogProvider } from '../CustomDialog';
import { PetStoreDialog } from '../PetStoreDialog';

vi.mock('../../../wailsjs/go/main/App', () => ({
    GetPetStoreAccount: vi.fn().mockResolvedValue({ credits: 25, user: { email: 'creator@example.test' }, uploads: [], purchases: [] }),
    GetPetStoreCreatorReport: vi.fn().mockResolvedValue({
        start_at: '2026-08-01T00:00:00Z',
        paid_summary: { sales_amount: 18, sales_count: 3, paid_pack_count: 1 },
        previous_paid_summary: { sales_amount: 9, sales_count: 1 },
        paid_packs: [{ id: 'paid-one', name: 'Paid One', sales_count: 3, sales_amount: 18 }],
        free_download_packs: [{ id: 'free-one', name: 'Free One', download_count: 7 }],
    }),
    GetPetStoreRankings: vi.fn().mockResolvedValue({ creators: [], downloads: [], sales: [] }),
    InstallPetStorePack: vi.fn().mockResolvedValue('pet-one'),
    IsPetStorePackInstalled: vi.fn().mockResolvedValue(false),
    ListPetStorePacks: vi.fn().mockResolvedValue({ packs: [], total_pages: 1 }),
    PurchasePetStorePack: vi.fn().mockResolvedValue(undefined),
    WithdrawPetStorePack: vi.fn().mockResolvedValue(undefined),
}));

vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: vi.fn().mockReturnValue(() => {}) }));

describe('PetStoreDialog creator report', () => {
    afterEach(() => cleanup());

    it('keeps paid sales and free-pet downloads in separate report sections', async () => {
        render(<DialogProvider><PetStoreDialog lang="en" onClose={vi.fn()} /></DialogProvider>);

        expect(await screen.findByRole('complementary', { name: 'My market' })).toBeTruthy();
        await waitFor(() => expect(screen.getByText('Paid pet sales')).toBeTruthy());
        expect(screen.getByText('Free pet downloads')).toBeTruthy();
        expect(screen.getByText('Paid One')).toBeTruthy();
        expect(screen.getByText('3 · 18 Credits')).toBeTruthy();
        expect(screen.getByText('Free One')).toBeTruthy();
        expect(screen.getByText('7 downloads')).toBeTruthy();
    });

    it('shows a report loading state and marks the selected period', async () => {
        let resolveReport: (value: Record<string, any>) => void;
        const pendingReport = new Promise<Record<string, any>>(resolve => { resolveReport = resolve; });
        const { GetPetStoreCreatorReport } = await import('../../../wailsjs/go/main/App');
        vi.mocked(GetPetStoreCreatorReport).mockReturnValueOnce(pendingReport);

        render(<DialogProvider><PetStoreDialog lang="en" onClose={vi.fn()} /></DialogProvider>);

        expect((await screen.findByRole('status')).textContent).toContain('Loading sales report…');
        expect(document.querySelector('.pet-store-report-skeleton')).toBeTruthy();
        expect(screen.getByRole('button', { name: 'Month' }).getAttribute('aria-pressed')).toBe('true');
        fireEvent.click(screen.getByRole('button', { name: 'Day' }));
        expect(screen.getByRole('button', { name: 'Day' }).getAttribute('aria-pressed')).toBe('true');

        resolveReport!({
            start_at: '2026-08-01T00:00:00Z',
            paid_summary: { sales_amount: 0, sales_count: 0, paid_pack_count: 0 },
            previous_paid_summary: { sales_amount: 0, sales_count: 0 },
            paid_packs: [],
            free_download_packs: [],
        });
        await waitFor(() => expect(screen.queryByRole('status')).toBeNull());
    });

    it('formats the date label for the selected report period', async () => {
        render(<DialogProvider><PetStoreDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        await screen.findByText('Paid pet sales');

        expect(document.querySelector('.pet-store-report-date time')?.textContent).toMatch(/2026.*8|August 2026/);
        fireEvent.click(screen.getByRole('button', { name: 'Year' }));
        await waitFor(() => expect(document.querySelector('.pet-store-report-date time')?.textContent).toBe('2026'));
        fireEvent.click(screen.getByRole('button', { name: 'Day' }));
        await waitFor(() => expect(document.querySelector('.pet-store-report-date time')?.textContent).toMatch(/Aug 1, 2026|2026.*8.*1/));
    });

    it('does not show the previous period’s data while a new period loads', async () => {
        let resolveDayReport: (value: Record<string, any>) => void;
        const dayReport = new Promise<Record<string, any>>(resolve => { resolveDayReport = resolve; });
        const { GetPetStoreCreatorReport } = await import('../../../wailsjs/go/main/App');
        vi.mocked(GetPetStoreCreatorReport).mockResolvedValueOnce({
            start_at: '2026-08-01T00:00:00Z',
            paid_summary: { sales_amount: 18, sales_count: 3, paid_pack_count: 1 },
            previous_paid_summary: { sales_amount: 9, sales_count: 1 },
            paid_packs: [{ id: 'paid-one', name: 'Paid One', sales_count: 3, sales_amount: 18 }],
            free_download_packs: [],
        }).mockReturnValueOnce(dayReport);

        render(<DialogProvider><PetStoreDialog lang="en" onClose={vi.fn()} /></DialogProvider>);
        await screen.findByText('Paid One');
        fireEvent.click(screen.getByRole('button', { name: 'Day' }));
        expect(screen.queryByText('Paid One')).toBeNull();
        expect(document.querySelector('.pet-store-report-skeleton')).toBeTruthy();

        resolveDayReport!({
            start_at: '2026-08-01T00:00:00Z',
            paid_summary: { sales_amount: 2, sales_count: 1, paid_pack_count: 1 },
            previous_paid_summary: { sales_amount: 0, sales_count: 0 },
            paid_packs: [{ id: 'paid-day', name: 'Day Sale', sales_count: 1, sales_amount: 2 }],
            free_download_packs: [],
        });
        expect(await screen.findByText('Day Sale')).toBeTruthy();
    });
});
