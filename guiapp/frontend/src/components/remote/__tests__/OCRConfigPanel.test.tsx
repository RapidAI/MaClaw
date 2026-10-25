import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { OCRConfigPanel } from '../OCRConfigPanel';

const mocks = vi.hoisted(() => ({
    check: vi.fn(),
    enabled: vi.fn(),
    setTier: vi.fn(),
    checkFormula: vi.fn(),
    downloadFormula: vi.fn(),
    formulaEnabled: vi.fn(),
    setFormulaEnabled: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    CheckOCRModel: mocks.check,
    DownloadOCRModel: vi.fn(),
    GetOCREnabled: mocks.enabled,
    SetOCREnabled: vi.fn(),
    SetOCRModelTier: mocks.setTier,
    CheckFormulaModel: mocks.checkFormula,
    DownloadFormulaModel: mocks.downloadFormula,
    GetFormulaEnabled: mocks.formulaEnabled,
    SetFormulaEnabled: mocks.setFormulaEnabled,
}));

const formulaProgress = vi.hoisted(() => ({
    emit: null as null | ((data: { percent?: number; downloaded?: number; total?: number; error?: string }) => void),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((event: string, cb: (data: { percent?: number; downloaded?: number; total?: number; error?: string }) => void) => {
        if (event === 'formula-download-progress') formulaProgress.emit = cb;
        return () => {
            if (event === 'formula-download-progress') formulaProgress.emit = null;
        };
    }),
    EventsOff: vi.fn(),
}));

describe('OCRConfigPanel tier selector', () => {
    beforeEach(() => {
        mocks.check.mockResolvedValue({ exists: true, size: 1024, tier: 'small' });
        mocks.enabled.mockResolvedValue(true);
        mocks.setTier.mockResolvedValue(undefined);
        mocks.checkFormula.mockResolvedValue({ exists: false, size: 0 });
        mocks.downloadFormula.mockResolvedValue(undefined);
        mocks.formulaEnabled.mockResolvedValue(true);
        mocks.setFormulaEnabled.mockResolvedValue(undefined);
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

    it('shows formula recognition in the OCR section and persists the toggle', async () => {
        render(<OCRConfigPanel lang="zh-Hans" />);
        expect(await screen.findByText('公式识别')).toBeTruthy();
        const box = await screen.findByRole('checkbox', { name: '启用公式识别' });
        expect((box as HTMLInputElement).checked).toBe(true);
        fireEvent.click(box);
        await waitFor(() => expect(mocks.setFormulaEnabled).toHaveBeenCalledWith(false));
    });

    it('shows a formula download in progress at 0 percent', async () => {
        render(<OCRConfigPanel lang="zh-Hans" />);
        expect(await screen.findByText('模型文件未找到，需要下载。')).toBeTruthy();
        act(() => {
            formulaProgress.emit?.({ percent: 0, downloaded: 0, total: 1000, error: '' });
        });
        expect(await screen.findByText('正在下载模型...')).toBeTruthy();
    });

    it('re-downloads formula files that already report as present', async () => {
        mocks.checkFormula.mockResolvedValue({ exists: true, size: 100396067 });
        render(<OCRConfigPanel lang="zh-Hans" />);
        const button = await screen.findByRole('button', { name: '重新下载' });
        fireEvent.click(button);
        await waitFor(() => expect(mocks.downloadFormula).toHaveBeenCalled());
    });

    it('clears the formula spinner when enabling finds the files already present', async () => {
        render(<OCRConfigPanel lang="zh-Hans" />);
        expect(await screen.findByText('模型文件未找到，需要下载。')).toBeTruthy();
        act(() => {
            formulaProgress.emit?.({ percent: 40, downloaded: 40, total: 100, error: '' });
        });
        expect(await screen.findByText('正在下载模型...')).toBeTruthy();

        const box = await screen.findByRole('checkbox', { name: '启用公式识别' });
        fireEvent.click(box);
        await waitFor(() => expect(mocks.setFormulaEnabled).toHaveBeenCalledWith(false));

        mocks.checkFormula.mockResolvedValue({ exists: true, size: 100396067 });
        fireEvent.click(box);
        await waitFor(() => expect(mocks.setFormulaEnabled).toHaveBeenCalledWith(true));
        expect(await screen.findByRole('button', { name: '重新下载' })).toBeTruthy();
        expect(screen.getByText('95.7 MB')).toBeTruthy();
        expect(screen.queryByText('正在下载模型...')).toBeNull();
    });
});
