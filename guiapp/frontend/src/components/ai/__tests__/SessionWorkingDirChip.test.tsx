import { readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { __resetCloudWorkspaceDisplayNamesForTests, rememberCloudWorkspaceDisplayName } from '../codingTaskMode';
import { composerExpertFromTab, composerWorkspaceKindLabel, SessionWorkingDirChip, truncatePathMiddle, workingDirDisplayLabel } from '../SessionWorkingDirChip';

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
    __resetCloudWorkspaceDisplayNamesForTests();
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
        expect(screen.queryByText('云端')).toBeNull();
        expect(screen.queryByText(/cloud-workspaces/i)).toBeNull();
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        expect(await screen.findByLabelText('打开云端工作区文件')).toBeTruthy();
        expect(screen.queryByLabelText('选择其他工作目录')).toBeNull();
    });

    it('shows remote host and directory instead of the local sandbox path', async () => {
        getTabWorkingDir.mockResolvedValue({
            path: 'C:\\Users\\me\\.maclaw\\data\\你好呀-1789995819852879500\\workspace',
            is_default: true,
        });
        const writeText = vi.fn().mockResolvedValue(undefined);
        Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
        render(<SessionWorkingDirChip
            tabId="proj-remote-1"
            theme={theme}
            lang="zh"
            remoteHost="www.driverdevelopment.com"
            remoteWorkDir="/home/ubuntu/app"
        />);
        expect(await screen.findByText('远程')).toBeTruthy();
        expect(screen.queryByText(/你好呀/)).toBeNull();
        expect(screen.queryByText(/driverdevelopment/)).toBeNull();
        expect(screen.queryByText('默认')).toBeNull();
        expect(screen.getByTestId('working-dir-chip').getAttribute('title')).toBe('www.driverdevelopment.com/home/ubuntu/app');
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        expect(screen.queryByLabelText('选择其他工作目录')).toBeNull();
        fireEvent.click(await screen.findByLabelText('复制工作目录路径'));
        expect(writeText).toHaveBeenCalledWith('www.driverdevelopment.com/home/ubuntu/app');
    });

    it('keeps local directory switching for ordinary folders', async () => {
        getTabWorkingDir.mockResolvedValueOnce({ path: 'D:/work/app', is_default: false });
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/other', is_default: false });
        selectWorkingDir.mockResolvedValue('D:/work/other');
        setTabWorkingDir.mockResolvedValue(undefined);
        const onWorkingDirChange = vi.fn();
        const onWorkingDirResolved = vi.fn();
        render(<SessionWorkingDirChip tabId="proj-2" theme={theme} lang="zh" onWorkingDirChange={onWorkingDirChange} onWorkingDirResolved={onWorkingDirResolved} />);
        await waitFor(() => expect(screen.getByText('本地')).toBeTruthy());
        // The whole chip opens the menu. A trailing caret would only repeat that.
        expect((screen.getByTestId('working-dir-chip').textContent || '').replace(/\s+/g, '')).toBe('本地');
        expect(screen.getByTestId('working-dir-chip').querySelector('.swdc-chip-caret')).toBeNull();
        expect(screen.getByTestId('working-dir-chip').getAttribute('title')).toBe('D:/work/app');
        expect(screen.queryByText('默认')).toBeNull();
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
        await waitFor(() => expect(screen.getByText('本地')).toBeTruthy());
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        fireEvent.click(await screen.findByLabelText('打开所在目录'));
        expect(openProjectDirectory).toHaveBeenCalledWith('D:/work/app');
    });

    it('copies the cloud workspace name from the menu', async () => {
        const cache = 'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc';
        rememberCloudWorkspaceDisplayName('cws_abc', '标书项目');
        getTabWorkingDir.mockResolvedValue({ path: cache, is_default: false });
        const writeText = vi.fn().mockResolvedValue(undefined);
        Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
        render(<SessionWorkingDirChip tabId="proj-cloud-copy" theme={theme} lang="zh" />);
        expect(await screen.findByText('云端工作区')).toBeTruthy();
        expect(screen.getByTestId('working-dir-chip').getAttribute('title')).toBe('标书项目');
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        fireEvent.click(await screen.findByLabelText('复制工作区名称'));
        expect(writeText).toHaveBeenCalledWith('标书项目');
        expect(writeText.mock.calls.some((call) => String(call[0]).includes('cloud-workspaces'))).toBe(false);
    });

    it('copies the working directory path from the menu', async () => {
        const writeText = vi.fn().mockResolvedValue(undefined);
        Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true });
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/app', is_default: false });
        render(<SessionWorkingDirChip tabId="proj-4" theme={theme} lang="zh" />);
        await waitFor(() => expect(screen.getByText('本地')).toBeTruthy());
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        fireEvent.click(await screen.findByLabelText('复制工作目录路径'));
        expect(writeText).toHaveBeenCalledWith('D:/work/app');
        expect(screen.queryByTestId('working-dir-menu')).toBeNull();
    });

    it('closes the menu on Escape and returns focus to the chip', async () => {
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/app', is_default: false });
        render(<SessionWorkingDirChip tabId="proj-5" theme={theme} lang="zh" />);
        await waitFor(() => expect(screen.getByText('本地')).toBeTruthy());
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        expect(await screen.findByTestId('working-dir-menu')).toBeTruthy();
        fireEvent.keyDown(document, { key: 'Escape' });
        expect(screen.queryByTestId('working-dir-menu')).toBeNull();
        expect(document.activeElement).toBe(screen.getByTestId('working-dir-chip'));
    });

    it('closes the menu on outside click', async () => {
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/app', is_default: false });
        render(<SessionWorkingDirChip tabId="proj-6" theme={theme} lang="zh" />);
        await waitFor(() => expect(screen.getByText('本地')).toBeTruthy());
        fireEvent.click(screen.getByTestId('working-dir-chip'));
        expect(await screen.findByTestId('working-dir-menu')).toBeTruthy();
        fireEvent.mouseDown(document.body);
        expect(screen.queryByTestId('working-dir-menu')).toBeNull();
    });

    it('shows the expert name and icon to the right of the folder', async () => {
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/app', is_default: true });
        render(<SessionWorkingDirChip
            tabId="expert-translate"
            theme={theme}
            lang="zh"
            expert={{ name: '论文翻译专家', icon: '🌐' }}
        />);
        const chip = await screen.findByTestId('working-dir-chip');
        const badge = screen.getByTestId('composer-expert-badge');
        expect(badge.textContent).toContain('🌐');
        expect(badge.textContent).toContain('论文翻译专家');
        expect(badge.getAttribute('aria-label')).toBe('专家：论文翻译专家');
        expect(chip.compareDocumentPosition(badge) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(screen.queryByText('通用')).toBeNull();
        cleanup();
        render(<SessionWorkingDirChip tabId="expert-plain" theme={theme} lang="zh" expert={{ name: '论文润色专家' }} />);
        expect((await screen.findByTestId('composer-expert-badge')).textContent).toContain('🤖');
    });

    it('shows an expert only for an expert tab', () => {
        expect(composerExpertFromTab({ type: 'expert', expertId: 'builtin-paper-translate', title: '论文翻译专家', expertIcon: '🌐' })).toEqual({ name: '论文翻译专家', icon: '🌐' });
        expect(composerExpertFromTab({ type: 'expert', expertId: 'builtin-paper-polish', title: '  ' })).toEqual({ name: 'builtin-paper-polish' });
        expect(composerExpertFromTab({ type: 'expert', expertId: '  ', title: '通用' })).toBeNull();
        expect(composerExpertFromTab({ type: 'local', title: '新建任务' })).toBeNull();
        expect(composerExpertFromTab({ type: 'project', title: '编程任务' })).toBeNull();
        expect(composerExpertFromTab(null)).toBeNull();
    });

    it('leaves the folder alone when no expert is assigned', async () => {
        getTabWorkingDir.mockResolvedValue({ path: 'D:/work/app', is_default: true });
        render(<SessionWorkingDirChip tabId="proj-plain" theme={theme} lang="zh" expert={{ name: '  ', icon: '🌐' }} />);
        await screen.findByTestId('working-dir-chip');
        expect(screen.queryByTestId('composer-expert-badge')).toBeNull();
    });

    it('keeps the open chip highlighted above the composer button locks', () => {
        const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../../../App.css'), 'utf8');
        expect(css).toContain('button.mc-working-dir-chip[aria-expanded="true"]');
        expect(css).toMatch(/button\.mc-working-dir-chip\[aria-expanded="true"\][^{]*\{[^}]*border-color:\s*var\(--mc-accent\)/);
        // The utility lock must not stretch this text chip, and the chip, the
        // expert mark and the icon buttons must all share one control height.
        expect(css).toMatch(/button\.mc-working-dir-chip,\s*#App \.mc-input-stack button\.mc-working-dir-chip\s*\{[^}]*min-height:\s*var\(--mc-composer-control-height/);
        expect(css).toMatch(/\.swdc-expert\s*\{[^}]*height:\s*var\(--mc-composer-control-height[^}]*border-radius:\s*var\(--radius-md\)[^}]*var\(--mc-muted-surface,\s*var\(--theme-surface\)\)/);
        expect(css).toMatch(/--mc-composer-control-height:\s*28px/);
        expect(css).toMatch(/\.swdc-bar\s*\{[^}]*flex:\s*0 0 auto/);
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

    it('shows the remote host and directory instead of the sandbox path', () => {
        expect(workingDirDisplayLabel(
            'C:\\Users\\me\\.maclaw\\data\\你好呀-1\\workspace',
            'zh',
            { host: 'www.driverdevelopment.com', workDir: '/srv/app' },
        )).toBe('www.driverdevelopment.com/srv/app');
        expect(workingDirDisplayLabel(
            'C:\\Users\\me\\.maclaw\\data\\你好呀-1\\workspace',
            'zh',
            { host: 'home.rapidai.tech', workDir: '/home/rapidrec', port: 22 },
        )).toBe('home.rapidai.tech/home/rapidrec');
        expect(workingDirDisplayLabel(
            'C:\\Users\\me\\.maclaw\\data\\你好呀-1\\workspace',
            'zh',
            { host: 'home.rapidai.tech', workDir: '/home/rapidrec', port: 55 },
        )).toBe('home.rapidai.tech:55/home/rapidrec');
    });

    it('uses a known cloud workspace name', () => {
        const cache = 'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc';
        expect(workingDirDisplayLabel(cache, 'zh', null, '标书项目')).toBe('标书项目');
    });
});

describe('composerWorkspaceKindLabel', () => {
    it('names the kind without a path', () => {
        expect(composerWorkspaceKindLabel('D:/work/app', 'zh')).toBe('本地');
        expect(composerWorkspaceKindLabel('D:/work/app', 'en')).toBe('Local');
        expect(composerWorkspaceKindLabel('D:/work/app', 'mn')).toBe('Локал');
        expect(composerWorkspaceKindLabel(
            'C:\\Users\\me\\.maclaw\\data\\你好呀-1\\workspace',
            'mn',
            { host: 'www.driverdevelopment.com', workDir: '/srv/app' },
        )).toBe('Алсын');
        expect(composerWorkspaceKindLabel(
            'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc',
            'mn',
        )).toBe('Үүлэн ажлын талбар');
        expect(composerWorkspaceKindLabel(
            'C:\\Users\\me\\.maclaw\\data\\你好呀-1\\workspace',
            'zh',
            { host: 'www.driverdevelopment.com', workDir: '/srv/app' },
        )).toBe('远程');
        expect(composerWorkspaceKindLabel(
            'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc',
            'zh',
        )).toBe('云端工作区');
        expect(composerWorkspaceKindLabel(
            'C:\\Users\\me\\.maclaw\\data\\cloud-workspaces\\tenant_default\\cws_abc',
            'zh-Hant',
        )).toBe('雲端工作區');
    });
});

describe('truncatePathMiddle', () => {
    it('keeps short paths intact', () => {
        expect(truncatePathMiddle('D:/work', 42)).toBe('D:/work');
    });

    it('keeps the ending folder when the middle form is still too long', () => {
        const leaf = `workspace-name-${'1234567890'.repeat(4)}`;
        const path = `C:/Users/ma139/.maclaw/data/tasks/${leaf}/workspace`;
        const label = truncatePathMiddle(path, 42);
        expect(label.length).toBeLessThanOrEqual(42);
        expect(label.startsWith('...')).toBe(true);
        expect(label.endsWith('workspace')).toBe(true);
        expect(label.startsWith('C:')).toBe(false);
    });
});
