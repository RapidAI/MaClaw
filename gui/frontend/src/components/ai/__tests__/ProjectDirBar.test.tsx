import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ProjectDirBar } from '../ProjectDirBar';

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

describe('ProjectDirBar cloud workspace', () => {
    beforeEach(() => {
        getTabWorkingDir.mockReset();
        setTabWorkingDir.mockReset();
        openProjectDirectory.mockReset();
        selectWorkingDir.mockReset();
    });

    it('shows a cloud label instead of the local cache path and hides directory switching', async () => {
        getTabWorkingDir.mockResolvedValue({
            path: 'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc',
            is_default: false,
        });
        render(<ProjectDirBar tabId="proj-1" theme={theme} lang="zh" />);
        expect(await screen.findByText('云端工作区')).toBeTruthy();
        expect(screen.getByText('云端')).toBeTruthy();
        expect(screen.queryByText(/cloud-workspaces/i)).toBeNull();
        expect(screen.queryByText('切换目录')).toBeNull();
        expect(screen.getByLabelText('打开云端工作区文件')).toBeTruthy();
    });

    it('keeps local directory switching for ordinary folders', async () => {
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/app', is_default: false });
        render(<ProjectDirBar tabId="proj-2" theme={theme} lang="zh" />);
        await waitFor(() => expect(screen.getByText('D:/work/app')).toBeTruthy());
        expect(screen.getByText('切换目录')).toBeTruthy();
    });

    it('reports the resolved directory with the tab that owns it', async () => {
        getTabWorkingDir.mockResolvedValue({
            path: 'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc',
            is_default: false,
        });
        const onWorkingDirResolved = vi.fn();
        render(<ProjectDirBar tabId="proj-cloud-1" theme={theme} lang="zh" onWorkingDirResolved={onWorkingDirResolved} />);
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
        render(<ProjectDirBar tabId="proj-1" theme={theme} lang="zh" onOpenCloudFiles={onOpenCloudFiles} />);
        fireEvent.click(await screen.findByLabelText('打开云端工作区文件'));
        expect(onOpenCloudFiles).toHaveBeenCalledTimes(1);
        expect(openProjectDirectory).not.toHaveBeenCalled();
    });
});
