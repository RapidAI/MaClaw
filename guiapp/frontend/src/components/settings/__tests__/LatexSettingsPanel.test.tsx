import React from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { LatexSettingsPanel } from '../LatexSettingsPanel';

const mocks = vi.hoisted(() => ({
    status: vi.fn(),
    setEnabled: vi.fn(),
    download: vi.fn(),
    handlers: {} as Record<string, (data: any) => void>,
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetLatexTinyTeXStatus: mocks.status,
    SetLatexTinyTeXEnabled: mocks.setEnabled,
    DownloadLatexTinyTeX: mocks.download,
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: (name: string, handler: (data: any) => void) => {
        mocks.handlers[name] = handler;
    },
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
        vi.useRealTimers();
        vi.clearAllMocks();
        mocks.handlers = {};
    });

    it('shows scheme-small as ready and enabled by default', async () => {
        render(<LatexSettingsPanel lang="zh-Hans" />);
        expect(await screen.findByTestId('latex-ready')).toBeTruthy();
        expect(screen.getByText('scheme-small 已就绪')).toBeTruthy();
        expect(screen.getByText('XeTeX test')).toBeTruthy();
        const box = screen.getByRole('switch') as HTMLInputElement;
        expect(box.checked).toBe(true);
        expect(screen.getByText(/下载源:/)).toBeTruthy();
    });

    it('clears the initial loading gate under StrictMode double-mount', async () => {
        // Regression: StrictMode cancels the first tick and the
        // initialRequestRef guard skipped the second, so setLoading(false)
        // never ran and the panel stayed on "加载中..." forever.
        render(
            <React.StrictMode>
                <LatexSettingsPanel lang="en" />
            </React.StrictMode>,
        );
        expect(await screen.findByTestId('latex-ready')).toBeTruthy();
        expect(screen.queryByText('Loading...')).toBeNull();
    });

    it('shows the tlmgr update while that phase is active', async () => {
        mocks.status.mockResolvedValue({
            enabled: true,
            ready: false,
            phase: 'updating',
            scheme: 'scheme-small',
        });
        render(<LatexSettingsPanel lang="zh-Hans" />);
        expect(await screen.findByText('正在更新 tlmgr…')).toBeTruthy();
        expect(screen.getByTestId('latex-busy')).toBeTruthy();
        expect(document.querySelector('.model-status-box__progress-track')).toBeNull();
    });

    it('shows updater bytes on the tlmgr phase and keeps that phase when the event omits it', async () => {
        mocks.status.mockResolvedValue({
            enabled: true,
            ready: false,
            phase: 'updating',
            percent: 0,
            total: 0,
        });
        render(<LatexSettingsPanel lang="en" />);
        expect(await screen.findByText('Updating tlmgr…')).toBeTruthy();
        act(() => {
            mocks.handlers['latex-tinytex-progress']({
                phase: 'updating',
                percent: 40,
                downloaded: 400,
                total: 1000,
            });
        });
        expect(screen.getByText(/40%/)).toBeTruthy();
        expect(document.querySelector('.model-status-box__progress-track')).toBeTruthy();
        act(() => {
            mocks.handlers['latex-tinytex-progress']({
                percent: 55,
                downloaded: 550,
                total: 1000,
            });
        });
        expect(screen.getByText('Updating tlmgr…')).toBeTruthy();
        expect(screen.getByText(/55%/)).toBeTruthy();
        expect(screen.queryByText('Downloading TinyTeX-0…')).toBeNull();
    });

    it('does not keep a download percent after the phase changes', async () => {
        vi.useFakeTimers();
        mocks.status
            .mockResolvedValueOnce({
                enabled: true,
                ready: false,
                phase: 'downloading',
                percent: 40,
                downloaded: 400,
                total: 1000,
            })
            .mockResolvedValue({
                enabled: true,
                ready: false,
                phase: 'updating',
                percent: 0,
                downloaded: 0,
                total: 0,
            });
        render(<LatexSettingsPanel lang="en" />);
        await act(async () => { await Promise.resolve(); });
        expect(screen.getByText(/40%/)).toBeTruthy();
        await act(async () => {
            vi.advanceTimersByTime(2000);
            await Promise.resolve();
        });
        expect(screen.getByText('Updating tlmgr…')).toBeTruthy();
        expect(screen.queryByText(/40%/)).toBeNull();
        expect(document.querySelector('.model-status-box__progress-track')).toBeNull();
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
        const box = await screen.findByRole('switch');
        fireEvent.click(box);
        await waitFor(() => expect(mocks.setEnabled).toHaveBeenCalledWith(false));
    });

    it('does not keep polling a ready installation', async () => {
        vi.useFakeTimers();
        render(<LatexSettingsPanel lang="en" />);
        await act(async () => { await Promise.resolve(); });
        expect(mocks.status).toHaveBeenCalledTimes(1);
        await act(async () => {
            vi.advanceTimersByTime(6000);
            await Promise.resolve();
        });
        expect(mocks.status).toHaveBeenCalledTimes(1);
    });

    it('polls during installation and stops when ready', async () => {
        vi.useFakeTimers();
        mocks.status.mockResolvedValueOnce({
            enabled: true,
            ready: false,
            phase: 'downloading',
            percent: 25,
        });
        render(<LatexSettingsPanel lang="en" />);
        await act(async () => { await Promise.resolve(); });
        expect(mocks.status).toHaveBeenCalledTimes(1);
        await act(async () => {
            vi.advanceTimersByTime(2000);
            await Promise.resolve();
        });
        expect(mocks.status).toHaveBeenCalledTimes(2);
        await act(async () => {
            vi.advanceTimersByTime(6000);
            await Promise.resolve();
        });
        expect(mocks.status).toHaveBeenCalledTimes(2);
    });
});
