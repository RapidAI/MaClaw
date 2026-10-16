import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { LatexSettingsPanel } from '../LatexSettingsPanel';

const mocks = vi.hoisted(() => ({
    status: vi.fn(),
    setEnabled: vi.fn(),
    download: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetLatexTinyTeXStatus: mocks.status,
    SetLatexTinyTeXEnabled: mocks.setEnabled,
    DownloadLatexTinyTeX: mocks.download,
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(),
    EventsOff: vi.fn(),
}));

describe('LatexSettingsPanel', () => {
    beforeEach(() => {
        mocks.status.mockResolvedValue({
            enabled: true,
            ready: true,
            phase: 'ready',
            scheme: 'scheme-small',
            bundle: 'TinyTeX-0',
            version: 'XeTeX test',
            source: 'https://github.com/rstudio/tinytex-releases/releases/download/daily/TinyTeX-0-windows.exe',
            install_dir: 'D:/maclaw/data/tinytex/dist/TinyTeX',
        });
        mocks.setEnabled.mockResolvedValue(undefined);
        mocks.download.mockResolvedValue(undefined);
    });

    afterEach(() => {
        cleanup();
        vi.clearAllMocks();
    });

    it('shows scheme-small as ready and enabled by default', async () => {
        render(<LatexSettingsPanel lang="zh-Hans" />);
        expect(await screen.findByTestId('latex-ready')).toBeTruthy();
        expect(screen.getByText('scheme-small 已就绪')).toBeTruthy();
        expect(screen.getByText('XeTeX test')).toBeTruthy();
        const box = screen.getByRole('checkbox') as HTMLInputElement;
        expect(box.checked).toBe(true);
        expect(screen.getByText(/下载源:/)).toBeTruthy();
    });

    it('offers a download when scheme-small is missing', async () => {
        mocks.status.mockResolvedValue({
            enabled: true,
            ready: false,
            phase: 'idle',
            scheme: 'scheme-small',
            source: 'https://github.com/rstudio/tinytex-releases/releases/download/daily/TinyTeX-0-windows.exe',
        });
        render(<LatexSettingsPanel lang="en" />);
        const button = await screen.findByRole('button', { name: 'Download and verify' });
        fireEvent.click(button);
        await waitFor(() => expect(mocks.download).toHaveBeenCalled());
    });

    it('persists turning TinyTeX off', async () => {
        render(<LatexSettingsPanel lang="en" />);
        const box = await screen.findByRole('checkbox');
        fireEvent.click(box);
        await waitFor(() => expect(mocks.setEnabled).toHaveBeenCalledWith(false));
    });
});
