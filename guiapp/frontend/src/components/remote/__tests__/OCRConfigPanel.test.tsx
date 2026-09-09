import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { OCRConfigPanel } from '../OCRConfigPanel';

const mocks = vi.hoisted(() => ({
    check: vi.fn(),
    enabled: vi.fn(),
    setTier: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CheckOCRModel: mocks.check,
    DownloadOCRModel: vi.fn(),
    GetOCREnabled: mocks.enabled,
    SetOCREnabled: vi.fn(),
    SetOCRModelTier: mocks.setTier,
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

describe('OCRConfigPanel tier selector', () => {
    beforeEach(() => {
        mocks.check.mockResolvedValue({ exists: true, size: 1024, tier: 'small' });
        mocks.enabled.mockResolvedValue(true);
        mocks.setTier.mockResolvedValue(undefined);
    });

    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
    });

    it('shows the current tier from CheckOCRModel', async () => {
        render(<OCRConfigPanel lang="en" />);

        const select = await screen.findByRole('combobox', { name: 'Model tier' }) as HTMLSelectElement;
        expect(select.value).toBe('small');
        expect(screen.getByRole('option', { name: 'Tiny (~6MB, fastest)' })).toBeTruthy();
        expect(screen.getByRole('option', { name: 'Medium (~139MB, most accurate)' })).toBeTruthy();
    });

    it('persists a tier switch via SetOCRModelTier', async () => {
        render(<OCRConfigPanel lang="en" />);
        const select = await screen.findByRole('combobox', { name: 'Model tier' }) as HTMLSelectElement;

        fireEvent.change(select, { target: { value: 'medium' } });

        await waitFor(() => expect(mocks.setTier).toHaveBeenCalledWith('medium'));
    });

    it('reverts the selection when SetOCRModelTier fails', async () => {
        mocks.setTier.mockRejectedValue(new Error('nope'));
        render(<OCRConfigPanel lang="en" />);
        const select = await screen.findByRole('combobox', { name: 'Model tier' }) as HTMLSelectElement;

        fireEvent.change(select, { target: { value: 'tiny' } });

        await waitFor(() => expect(select.value).toBe('small'));
    });

    it('reflects the kicked download when the new tier is missing', async () => {
        mocks.check
            .mockResolvedValueOnce({ exists: true, size: 1024, tier: 'small' })
            .mockResolvedValue({ exists: false, size: 0, tier: 'tiny' });
        render(<OCRConfigPanel lang="zh-Hans" />);
        const select = await screen.findByRole('combobox', { name: '模型档位' }) as HTMLSelectElement;

        fireEvent.change(select, { target: { value: 'tiny' } });

        await waitFor(() => expect(mocks.setTier).toHaveBeenCalledWith('tiny'));
        // Downloading state disables the selector until progress completes.
        await waitFor(() => expect(select.disabled).toBe(true));
    });
});
