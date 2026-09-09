import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { SessionWorkingDirChip, truncatePathMiddle, workingDirDisplayLabel } from '../SessionWorkingDirChip';

const getTabWorkingDir = vi.fn();
const setTabWorkingDir = vi.fn();
const openProjectDirectory = vi.fn();
const selectWorkingDir = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetTabWorkingDir: (...args: unknown[]) => getTabWorkingDir(...args),
    SetTabWorkingDir: (...args: unknown[]) => setTabWorkingDir(...args),
    OpenProjectDirectory: (...args: unknown[]) => openProjectDirectory(...args),
    SelectWorkingDir: (...args: unknown[]) => selectWorkingDir(...args),
}));

const theme = {
    titleBarBorder: '#ddd',
    titleBarBg: '#fff',
    textMuted: '#667',
    linkColor: '#2563eb',
    fieldBg: '#f8fafc',
    text: '#111',
} as any;

afterEach(() => {
    cleanup();
});

describe('SessionWorkingDirChip', () => {
    beforeEach(() => {
        getTabWorkingDir.mockReset();
        setTabWorkingDir.mockReset();
        openProjectDirectory.mockReset();
        openProjectDirectory.mockResolvedValue(undefined);
        selectWorkingDir.mockReset();
    });

    it('shows a cloud label instead of the local cache path and hides directory switching', async () => {
        getTabWorkingDir.mockResolvedValue({
            path: 'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc',
            is_default: false,
        });
        render(<SessionWorkingDirChip tabId="proj-1" theme={theme} lang="zh" />);
        expect(await screen.findByText('云端工作区')).toBeTruthy();
        expect(screen.getByText('云端')).toBeTruthy();
        expect(screen.queryByText(/cloud-workspaces/i)).toBeNull();
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        expect(await screen.findByLabelText('打开云端工作区文件')).toBeTruthy();
        expect(screen.queryByLabelText('选择其他工作目录')).toBeNull();
    });

    it('keeps local directory switching for ordinary folders', async () => {
        getTabWorkingDir.mockResolvedValueOnce({ path: 'D:/work/app', is_default: false });
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/other', is_default: false });
        selectWorkingDir.mockResolvedValue('D:/work/other');
        setTabWorkingDir.mockResolvedValue(undefined);
        const onWorkingDirChange = vi.fn();
        const onWorkingDirResolved = vi.fn();
        render(<SessionWorkingDirChip tabId="proj-2" theme={theme} lang="zh" onWorkingDirChange={onWorkingDirChange} onWorkingDirResolved={onWorkingDirResolved} />);
        await waitFor(() => expect(screen.getByText('D:/work/app')).toBeTruthy());
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        fireEvent.click(await screen.findByLabelText('选择其他工作目录'));
        await waitFor(() => expect(setTabWorkingDir).toHaveBeenCalledWith('proj-2', 'D:/work/other'));
        await waitFor(() => expect(onWorkingDirChange).toHaveBeenCalled());
        // Listeners (e.g. the execution header meta row) must hear the new path
        // immediately, not only after a remount refetch.
        await waitFor(() => expect(onWorkingDirResolved).toHaveBeenCalledWith('D:/work/other', 'proj-2'));
    });

    it('reports the resolved directory with the tab that owns it', async () => {
        getTabWorkingDir.mockResolvedValue({
            path: 'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc',
            is_default: false,
        });
        const onWorkingDirResolved = vi.fn();
        render(<SessionWorkingDirChip tabId="proj-cloud-1" theme={theme} lang="zh" onWorkingDirResolved={onWorkingDirResolved} />);
        await waitFor(() => expect(onWorkingDirResolved).toHaveBeenCalledWith(
            'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc',
            'proj-cloud-1',
        ));
    });

    it('opens the in-app cloud file browser instead of Explorer', async () => {
        getTabWorkingDir.mockResolvedValue({
            path: 'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc',
            is_default: false,
        });
        const onOpenCloudFiles = vi.fn();
        render(<SessionWorkingDirChip tabId="proj-1" theme={theme} lang="zh" onOpenCloudFiles={onOpenCloudFiles} />);
        await screen.findByText('云端工作区');
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        fireEvent.click(await screen.findByLabelText('打开云端工作区文件'));
        expect(onOpenCloudFiles).toHaveBeenCalledTimes(1);
        expect(openProjectDirectory).not.toHaveBeenCalled();
    });

    it('opens the containing folder for a local directory', async () => {
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/app', is_default: false });
        render(<SessionWorkingDirChip tabId="proj-3" theme={theme} lang="zh" />);
        await waitFor(() => expect(screen.getByText('D:/work/app')).toBeTruthy());
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        fireEvent.click(await screen.findByLabelText('打开所在目录'));
        expect(openProjectDirectory).toHaveBeenCalledWith('D:/work/app');
    });

    it('copies the working directory path from the menu', async () => {
        const writeText = vi.fn().mockResolvedValue(undefined);
        Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/app', is_default: false });
        render(<SessionWorkingDirChip tabId="proj-4" theme={theme} lang="zh" />);
        await waitFor(() => expect(screen.getByText('D:/work/app')).toBeTruthy());
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        fireEvent.click(await screen.findByLabelText('复制工作目录路径'));
        expect(writeText).toHaveBeenCalledWith('D:/work/app');
        expect(screen.queryByTestId('working-dir-menu')).toBeNull();
    });

    it('closes the menu on Escape and returns focus to the chip', async () => {
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/app', is_default: false });
        render(<SessionWorkingDirChip tabId="proj-5" theme={theme} lang="zh" />);
        await waitFor(() => expect(screen.getByText('D:/work/app')).toBeTruthy());
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        expect(await screen.findByTestId('working-dir-menu')).toBeTruthy();
        fireEvent.keyDown(document, { key: 'Escape' });
        expect(screen.queryByTestId('working-dir-menu')).toBeNull();
        expect(document.activeElement).toBe(screen.getByTestId('working-dir-chip'));
    });

    it('closes the menu on outside click', async () => {
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/app', is_default: false });
        render(<SessionWorkingDirChip tabId="proj-6" theme={theme} lang="zh" />);
        await waitFor(() => expect(screen.getByText('D:/work/app')).toBeTruthy());
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        expect(await screen.findByTestId('working-dir-menu')).toBeTruthy();
        fireEvent.mouseDown(document.body);
        expect(screen.queryByTestId('working-dir-menu')).toBeNull();
    });
});

describe('workingDirDisplayLabel', () => {
    it('collapses cloud cache paths to a friendly label', () => {
        expect(workingDirDisplayLabel('C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc', 'zh')).toBe('云端工作区');
        expect(workingDirDisplayLabel('C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc', 'en')).toBe('Cloud workspace');
    });

    it('truncates long local paths in the middle', () => {
        const long = 'D:/very/long/path/that/keeps/going/inside/final/dir';
        const label = workingDirDisplayLabel(long, 'zh');
        expect(label.length).toBeLessThanOrEqual(42);
        expect(label).toContain('...');
        expect(workingDirDisplayLabel('D:/work/app', 'zh')).toBe('D:/work/app');
    });
});

describe('truncatePathMiddle', () => {
    it('keeps short paths intact', () => {
        expect(truncatePathMiddle('D:/work', 42)).toBe('D:/work');
    });
});
