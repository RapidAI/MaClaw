import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { __resetWorkspaceDirectoryCacheForTests, CodePreviewWorkspace, workspaceFileIconKind } from '../CodePreviewWorkspace';

const getDirectory = vi.fn();
const getFilePreview = vi.fn();
const getEntryProperties = vi.fn();
const isVSCodeAvailable = vi.fn();
const openFileInVSCode = vi.fn();

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetCodingWorkbenchDirectory: (...args: unknown[]) => getDirectory(...args),
    GetCodingWorkbenchFilePreview: (...args: unknown[]) => getFilePreview(...args),
    GetCodingWorkbenchEntryProperties: (...args: unknown[]) => getEntryProperties(...args),
    IsCodingWorkbenchVSCodeAvailable: (...args: unknown[]) => isVSCodeAvailable(...args),
    OpenCodingWorkbenchFileInVSCode: (...args: unknown[]) => openFileInVSCode(...args),
}));

const theme = {
    bg: '#fff', text: '#182230', textMuted: '#607080', border: '#d8dee8', lineNumBg: '#f8fafc', lineNumText: '#94a3b8',
    tabBg: '#f8fafc', tabActiveBg: '#fff', tabActiveText: '#123b65', tabHoverBg: '#eef2f7', diffAddBg: '#eef7f2',
    diffAddText: '#3f685b', diffDeleteBg: '#fff0ef', diffDeleteText: '#c43d34', syntaxKeyword: '#2f5f98',
    syntaxString: '#4f7f6f', syntaxComment: '#64748b', syntaxNumber: '#2f5f98', syntaxFunction: '#334155', syntaxType: '#2f5f98', syntaxOperator: '#334155',
};

beforeEach(() => {
    __resetWorkspaceDirectoryCacheForTests();
    getDirectory.mockReset();
    getFilePreview.mockReset();
    getEntryProperties.mockReset();
    isVSCodeAvailable.mockReset();
    isVSCodeAvailable.mockResolvedValue(false);
    openFileInVSCode.mockReset();
});

afterEach(() => {
    cleanup();
    __resetWorkspaceDirectoryCacheForTests();
});

describe('workspaceFileIconKind', () => {
    it('assigns distinct SVG badges to common file families', () => {
        expect(workspaceFileIconKind('component.tsx')).toEqual({ badge: 'TS', kind: 'code' });
        expect(workspaceFileIconKind('settings.json')).toEqual({ badge: '{}', kind: 'data' });
        expect(workspaceFileIconKind('README.md')).toEqual({ badge: 'TXT', kind: 'text' });
        expect(workspaceFileIconKind('layout.html')).toEqual({ badge: '<>', kind: 'markup' });
        expect(workspaceFileIconKind('LICENSE')).toEqual({ badge: 'FILE', kind: 'file' });
    });
});

describe('CodePreviewWorkspace context menu', () => {
    it('shows properties for a file from the right-click menu', async () => {
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        getEntryProperties.mockResolvedValue({ name: 'main.go', path: 'main.go', abs_path: '/remote/app/main.go', is_dir: false, size: 1536, size_known: true, modified_at: 1_720_000_000, mode: '0644', extension: 'go' });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const file = await screen.findByTestId('code-preview-workspace-file');
        fireEvent.contextMenu(file, { clientX: 30, clientY: 30 });
        fireEvent.mouseDown(screen.getByTestId('code-preview-workspace-context-properties'));
        expect(screen.getByTestId('code-preview-workspace-context-menu')).toBeTruthy();
        fireEvent.click(screen.getByTestId('code-preview-workspace-context-properties'));
        expect((await screen.findByTestId('code-preview-workspace-properties')).textContent).toContain('main.go');
        expect(screen.getByTestId('code-preview-workspace-properties').textContent).toContain('File');
        expect(screen.getByTestId('code-preview-workspace-properties').textContent).toContain('/remote/app/main.go');
        expect(screen.getByTestId('code-preview-workspace-properties').textContent).toContain('1.5 KB');
        expect(screen.getByTestId('code-preview-workspace-properties').textContent).toContain('0644');
    });

    it('localizes the context menu and file properties in simplified Chinese', async () => {
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        getEntryProperties.mockResolvedValue({ name: 'main.go', path: 'main.go', abs_path: '/remote/app/main.go', is_dir: false, size: 1536, size_known: true, modified_at: 1_720_000_000, mode: '0644', extension: 'go' });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="zh-CN" theme={theme} onOpenFile={vi.fn()} />);
        const file = await screen.findByTestId('code-preview-workspace-file');
        fireEvent.contextMenu(file, { clientX: 30, clientY: 30 });
        expect(screen.getByTestId('code-preview-workspace-context-preview').textContent).toBe('预览');
        expect(screen.getByTestId('code-preview-workspace-context-properties').textContent).toBe('属性');
        fireEvent.click(screen.getByTestId('code-preview-workspace-context-properties'));
        const properties = await screen.findByTestId('code-preview-workspace-properties');
        expect(properties.textContent).toContain('属性');
        expect(properties.textContent).toContain('名称');
        expect(properties.textContent).toContain('main.go');
        expect(properties.textContent).toContain('类型');
        expect(properties.textContent).toContain('文件 (.go)');
        expect(properties.textContent).toContain('完整路径');
        expect(properties.textContent).toContain('/remote/app/main.go');
        expect(properties.textContent).toContain('修改时间');
        expect(properties.textContent).toContain('权限');
        expect(properties.textContent).toContain('0644');
    });

    it('uses traditional Chinese for the context menu and folder properties', async () => {
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'src', path: 'src', is_dir: true }] });
        getEntryProperties.mockResolvedValue({ name: 'src', path: 'src', abs_path: '/remote/app/src', is_dir: true, size_known: false, modified_at: 1_720_000_000, mode: '0755' });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="zh-Hant" theme={theme} onOpenFile={vi.fn()} />);
        const directory = await screen.findByTestId('code-preview-workspace-directory');
        fireEvent.contextMenu(directory, { clientX: 30, clientY: 30 });
        expect(screen.getByTestId('code-preview-workspace-context-preview').textContent).toBe('預覽資料夾');
        expect(screen.getByTestId('code-preview-workspace-context-properties').textContent).toBe('屬性');
        fireEvent.click(screen.getByTestId('code-preview-workspace-context-properties'));
        const properties = await screen.findByTestId('code-preview-workspace-properties');
        expect(properties.textContent).toContain('類型');
        expect(properties.textContent).toContain('資料夾');
        expect(properties.textContent).toContain('完整路徑');
        expect(properties.textContent).toContain('/remote/app/src');
        expect(properties.textContent).toContain('未計算');
        expect(properties.textContent).toContain('權限');
        expect(properties.textContent).toContain('0755');
    });

    it('keeps a distinct properties region while metadata is loading and closes it on request', async () => {
        let resolveProperties: ((value: unknown) => void) | undefined;
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        getEntryProperties.mockImplementationOnce(() => new Promise((resolve) => { resolveProperties = resolve; }));
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        fireEvent.contextMenu(await screen.findByTestId('code-preview-workspace-file'), { clientX: 30, clientY: 30 });
        fireEvent.click(screen.getByTestId('code-preview-workspace-context-properties'));
        const properties = await screen.findByTestId('code-preview-workspace-properties');
        expect(properties.tagName).toBe('SECTION');
        expect(properties.getAttribute('role')).toBe('region');
        expect(screen.getByRole('status').textContent).toContain('Loading properties');
        fireEvent.click(screen.getByRole('button', { name: 'Close properties' }));
        expect(screen.queryByTestId('code-preview-workspace-properties')).toBeNull();
        resolveProperties?.({ name: 'main.go', path: 'main.go', abs_path: '/remote/app/main.go', is_dir: false, size: 2, size_known: true, mode: '0644' });
    });

    it('opens the context menu from the keyboard and restores focus after Escape', async () => {
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const file = await screen.findByTestId('code-preview-workspace-file');
        file.focus();
        fireEvent.keyDown(file, { key: 'ContextMenu' });
        const menu = await screen.findByTestId('code-preview-workspace-context-menu');
        expect(menu.getAttribute('role')).toBe('menu');
        fireEvent.keyDown(window, { key: 'Escape' });
        await waitFor(() => expect(screen.queryByTestId('code-preview-workspace-context-menu')).toBeNull());
        await waitFor(() => expect(document.activeElement).toBe(file));
    });

    it('opens a file from the right-click preview action', async () => {
        const onOpenFile = vi.fn();
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        getFilePreview.mockResolvedValue({ path: 'main.go', abs_path: '/remote/app/main.go', content: 'package main', language: 'go' });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={onOpenFile} />);
        const file = await screen.findByTestId('code-preview-workspace-file');
        fireEvent.contextMenu(file, { clientX: 30, clientY: 30 });
        fireEvent.click(screen.getByTestId('code-preview-workspace-context-preview'));
        await waitFor(() => expect(onOpenFile).toHaveBeenCalledWith(expect.objectContaining({ filePath: 'main.go', content: 'package main' })));
    });

    it('shows and invokes Open with VS Code only when VS Code is available', async () => {
        isVSCodeAvailable.mockResolvedValue(true);
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const file = await screen.findByTestId('code-preview-workspace-file');
        fireEvent.contextMenu(file, { clientX: 30, clientY: 30 });
        fireEvent.click(await screen.findByTestId('code-preview-workspace-context-open-vscode'));
        await waitFor(() => expect(openFileInVSCode).toHaveBeenCalledWith('remote-task', 'main.go'));
    });

    it('explains that remote VS Code files are local temporary copies', async () => {
        isVSCodeAvailable.mockResolvedValue(true);
        openFileInVSCode.mockResolvedValue(true);
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        fireEvent.contextMenu(await screen.findByTestId('code-preview-workspace-file'), { clientX: 30, clientY: 30 });
        fireEvent.click(await screen.findByTestId('code-preview-workspace-context-open-vscode'));
        const notice = await screen.findByTestId('code-preview-workspace-notice');
        expect(notice.textContent).toMatch(/local temporary copy/i);
        expect(notice.style.fontSize).toBe('11px');
        expect(notice.style.fontWeight).toBe('400');
        expect(notice.style.background).toBe('rgb(248, 250, 252)');
        expect(notice.getAttribute('role')).toBe('status');
    });

    it('allows the workspace notice to be dismissed', async () => {
        isVSCodeAvailable.mockResolvedValue(true);
        openFileInVSCode.mockResolvedValue(true);
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="zh-CN" theme={theme} onOpenFile={vi.fn()} />);
        fireEvent.contextMenu(await screen.findByTestId('code-preview-workspace-file'), { clientX: 30, clientY: 30 });
        fireEvent.click(await screen.findByTestId('code-preview-workspace-context-open-vscode'));
        await screen.findByTestId('code-preview-workspace-notice');

        fireEvent.click(screen.getByRole('button', { name: '关闭提示' }));

        expect(screen.queryByTestId('code-preview-workspace-notice')).toBeNull();
    });

    it('automatically dismisses an informational workspace notice', async () => {
        isVSCodeAvailable.mockResolvedValue(true);
        openFileInVSCode.mockResolvedValue(true);
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const file = await screen.findByTestId('code-preview-workspace-file');
        vi.useFakeTimers();
        try {
            fireEvent.contextMenu(file, { clientX: 30, clientY: 30 });
            fireEvent.click(screen.getByTestId('code-preview-workspace-context-open-vscode'));
            await act(async () => { await Promise.resolve(); });
            expect(screen.getByTestId('code-preview-workspace-notice')).toBeTruthy();

            await act(async () => { await vi.advanceTimersByTimeAsync(8_000); });

            expect(screen.queryByTestId('code-preview-workspace-notice')).toBeNull();
        } finally {
            vi.useRealTimers();
        }
    });

    it('does not show a completed VS Code download notice after switching projects', async () => {
        let resolveOpen: ((value: boolean) => void) | undefined;
        isVSCodeAvailable.mockResolvedValue(true);
        openFileInVSCode.mockImplementationOnce(() => new Promise<boolean>((resolve) => { resolveOpen = resolve; }));
        getDirectory
            .mockResolvedValueOnce({ root: '/remote/old', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] })
            .mockResolvedValueOnce({ root: '/remote/new', entries: [] });
        const view = render(<CodePreviewWorkspace projectPath="old-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        fireEvent.contextMenu(await screen.findByTestId('code-preview-workspace-file'), { clientX: 30, clientY: 30 });
        fireEvent.click(await screen.findByTestId('code-preview-workspace-context-open-vscode'));

        view.rerender(<CodePreviewWorkspace projectPath="new-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        await screen.findByText('/remote/new');
        resolveOpen?.(true);
        await Promise.resolve();

        expect(screen.queryByText(/local temporary copy/i)).toBeNull();
    });

    it('does not show Open with VS Code when VS Code is unavailable', async () => {
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const file = await screen.findByTestId('code-preview-workspace-file');
        fireEvent.contextMenu(file, { clientX: 30, clientY: 30 });
        expect(screen.queryByTestId('code-preview-workspace-context-open-vscode')).toBeNull();
    });

    it('does not show Open with VS Code for non-source files', async () => {
        isVSCodeAvailable.mockResolvedValue(true);
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [{ name: 'README.md', path: 'README.md', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const file = await screen.findByTestId('code-preview-workspace-file');
        fireEvent.contextMenu(file, { clientX: 30, clientY: 30 });
        expect(screen.queryByTestId('code-preview-workspace-context-open-vscode')).toBeNull();
    });

    it('does not let a stale file preview replace a newer selection', async () => {
        let resolveFirst: ((value: unknown) => void) | undefined;
        const onOpenFile = vi.fn();
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [
            { name: 'first.go', path: 'first.go', is_dir: false },
            { name: 'second.go', path: 'second.go', is_dir: false },
        ] });
        getFilePreview
            .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }))
            .mockResolvedValueOnce({ path: 'second.go', abs_path: '/remote/app/second.go', content: 'second', language: 'go' });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={onOpenFile} />);
        const [first, second] = await screen.findAllByTestId('code-preview-workspace-file');
        fireEvent.click(first);
        fireEvent.click(second);
        await waitFor(() => expect(onOpenFile).toHaveBeenCalledWith(expect.objectContaining({ filePath: 'second.go', content: 'second' })));
        resolveFirst?.({ path: 'first.go', abs_path: '/remote/app/first.go', content: 'first', language: 'go' });
        await Promise.resolve();
        expect(onOpenFile).toHaveBeenCalledTimes(1);
    });

    it('keeps cached directory entries visible when background revalidation fails', async () => {
        getDirectory.mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'cached.go', path: 'cached.go', is_dir: false }] });
        const first = render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        await screen.findByText('cached.go');
        first.unmount();

        getDirectory.mockRejectedValueOnce(new Error('SSH reconnecting'));
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        expect(screen.getByText('cached.go')).toBeTruthy();
        await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('SSH reconnecting'));
        expect(screen.getByText('cached.go')).toBeTruthy();
    });

    it('reloads the root directory when a remote SSH reconnect is reported', async () => {
        getDirectory
            .mockRejectedValueOnce(new Error('SSH session not connected'))
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'main.go', path: 'main.go', is_dir: false }] });
        const view = render(<CodePreviewWorkspace projectPath="remote-task" refreshToken={0} lang="en" theme={theme} onOpenFile={vi.fn()} />);
        await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('SSH session not connected'));

        view.rerender(<CodePreviewWorkspace projectPath="remote-task" refreshToken={1} lang="en" theme={theme} onOpenFile={vi.fn()} />);

        expect(await screen.findByText('main.go')).toBeTruthy();
        expect(getDirectory.mock.calls.length).toBeGreaterThanOrEqual(2);
    });

    it('keeps the refresh control available after the initial remote directory request fails', async () => {
        getDirectory
            .mockRejectedValueOnce(new Error('SSH session not connected'))
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'recovered.go', path: 'recovered.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        await screen.findByText('SSH session not connected');

        fireEvent.click(screen.getByRole('button', { name: 'Refresh working directory' }));

        expect(await screen.findByText('recovered.go')).toBeTruthy();
        expect(screen.queryByText('SSH session not connected')).toBeNull();
    });

    it('shows a recoverable inline error when a remote child directory fails to load', async () => {
        getDirectory
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'src', path: 'src', is_dir: true }] })
            .mockRejectedValueOnce(new Error('remote directory timed out'))
            .mockResolvedValueOnce({ root: '/remote/app', path: 'src', entries: [{ name: 'main.go', path: 'src/main.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        fireEvent.click(await screen.findByTestId('code-preview-workspace-directory'));
        const error = await screen.findByTestId('code-preview-workspace-directory-error');
        expect(error.textContent).toContain('remote directory timed out');

        fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

        expect(await screen.findByText('main.go')).toBeTruthy();
        expect(screen.queryByTestId('code-preview-workspace-directory-error')).toBeNull();
    });

    it('lets a manual refresh supersede a pending remote root request', async () => {
        let resolveOld: ((value: unknown) => void) | undefined;
        getDirectory
            .mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve; }))
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'fresh.go', path: 'fresh.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(1));

        const refresh = screen.getByRole('button', { name: 'Refresh working directory' });
        expect(refresh.getAttribute('aria-busy')).toBe('true');
        fireEvent.click(refresh);

        expect(await screen.findByText('fresh.go')).toBeTruthy();
        expect(getDirectory).toHaveBeenCalledTimes(2);
        resolveOld?.({ root: '/remote/app', entries: [{ name: 'stale.go', path: 'stale.go', is_dir: false }] });
        await Promise.resolve();
        expect(screen.queryByText('stale.go')).toBeNull();
    });

    it('clears stale child loading state when the remote root is manually refreshed', async () => {
        let resolveOldChild: ((value: unknown) => void) | undefined;
        getDirectory
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'src', path: 'src', is_dir: true }] })
            .mockImplementationOnce(() => new Promise((resolve) => { resolveOldChild = resolve; }))
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'src', path: 'src', is_dir: true }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        fireEvent.click(await screen.findByTestId('code-preview-workspace-directory'));
        expect(await screen.findByText('Loading...')).toBeTruthy();

        fireEvent.click(screen.getByRole('button', { name: 'Refresh working directory' }));

        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(3));
        await waitFor(() => expect(screen.queryByText('Loading...')).toBeNull());
        resolveOldChild?.({ root: '/remote/app', path: 'src', entries: [{ name: 'stale.go', path: 'src/stale.go', is_dir: false }] });
        await Promise.resolve();
        expect(screen.queryByText('stale.go')).toBeNull();
    });

    it('renders remote directory loading states as subtle status text', async () => {
        let resolveRoot: ((value: unknown) => void) | undefined;
        getDirectory.mockImplementationOnce(() => new Promise((resolve) => { resolveRoot = resolve; }));
        render(<CodePreviewWorkspace projectPath="remote-task" lang="zh-CN" theme={theme} onOpenFile={vi.fn()} />);

        const loading = await screen.findByTestId('code-preview-workspace-root-loading');
        expect(loading.getAttribute('role')).toBe('status');
        expect(loading.textContent).toContain('正在加载工作目录');
        expect(loading.style.fontSize).toBe('10.5px');
        expect(loading.style.fontWeight).toBe('400');
        expect(loading.style.color).toBe('rgb(96, 112, 128)');
        const rootLabel = screen.getByTestId('code-preview-workspace-root-label');
        expect(rootLabel.textContent).toContain('正在定位目录');
        expect(rootLabel.style.fontSize).toBe('10.5px');
        expect(rootLabel.style.color).toBe('rgb(148, 163, 184)');

        resolveRoot?.({ root: '/remote/app', entries: [] });
    });

    it('renders an empty remote directory as a quiet status instead of body text', async () => {
        getDirectory.mockResolvedValue({ root: '/remote/empty', entries: [] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);

        const empty = await screen.findByTestId('code-preview-workspace-empty');
        expect(empty.getAttribute('role')).toBe('status');
        expect(empty.style.fontSize).toBe('10.5px');
        expect(empty.style.fontWeight).toBe('400');
        expect(screen.getByTestId('code-preview-workspace-root-label').textContent).toBe('/remote/empty');
    });

    it('retries immediately after reconnect even if the old SSH directory request is still pending', async () => {
        let resolveOld: ((value: unknown) => void) | undefined;
        getDirectory
            .mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve; }))
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'fresh.go', path: 'fresh.go', is_dir: false }] });
        const view = render(<CodePreviewWorkspace projectPath="remote-task" refreshToken={0} lang="en" theme={theme} onOpenFile={vi.fn()} />);

        view.rerender(<CodePreviewWorkspace projectPath="remote-task" refreshToken={1} lang="en" theme={theme} onOpenFile={vi.fn()} />);
        expect(await screen.findByText('fresh.go')).toBeTruthy();

        resolveOld?.({ root: '/remote/app', entries: [{ name: 'stale.go', path: 'stale.go', is_dir: false }] });
        await Promise.resolve();
        expect(screen.queryByText('stale.go')).toBeNull();
        expect(screen.getByText('fresh.go')).toBeTruthy();
    });

    it('does not issue another refresh just because the expanded tree re-renders', async () => {
        getDirectory
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'src', path: 'src', is_dir: true }] })
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'src', path: 'src', is_dir: true }] })
            .mockResolvedValueOnce({ root: '/remote/app', path: 'src', entries: [] });
        const view = render(<CodePreviewWorkspace projectPath="remote-task" refreshToken={0} lang="en" theme={theme} onOpenFile={vi.fn()} />);
        await screen.findByTestId('code-preview-workspace-directory');
        view.rerender(<CodePreviewWorkspace projectPath="remote-task" refreshToken={1} lang="en" theme={theme} onOpenFile={vi.fn()} />);
        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(2));

        fireEvent.click(screen.getByTestId('code-preview-workspace-directory'));
        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(3));
    });

    it('does not recurse forever when a remote directory listing contains a path cycle', async () => {
        getDirectory
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'loop', path: 'loop', is_dir: true }] })
            .mockResolvedValueOnce({ root: '/remote/app', path: 'loop', entries: [{ name: 'workspace', path: '', is_dir: true }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        fireEvent.click(await screen.findByTestId('code-preview-workspace-directory'));
        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(2));
        expect(screen.getByText('workspace')).toBeTruthy();
    });

    it('waits for the refreshed root before revalidating expanded folders', async () => {
        let reconnecting = false;
        let resolveRoot: ((value: unknown) => void) | undefined;
        const refreshedChildren: string[] = [];
        getDirectory.mockImplementation((_projectPath: string, path: string) => {
            if (!path) {
                if (!reconnecting) return Promise.resolve({ root: '/remote/app', entries: [
                    { name: 'one', path: 'one', is_dir: true },
                    { name: 'two', path: 'two', is_dir: true },
                ] });
                return new Promise((resolve) => { resolveRoot = resolve; });
            }
            if (reconnecting) refreshedChildren.push(path);
            return Promise.resolve({ root: '/remote/app', path, entries: [] });
        });
        const view = render(<CodePreviewWorkspace projectPath="remote-task" refreshToken={0} lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const directories = await screen.findAllByTestId('code-preview-workspace-directory');
        for (const directory of directories) fireEvent.click(directory);
        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(3));

        reconnecting = true;
        view.rerender(<CodePreviewWorkspace projectPath="remote-task" refreshToken={1} lang="en" theme={theme} onOpenFile={vi.fn()} />);
        await waitFor(() => expect(resolveRoot).toBeTypeOf('function'));
        expect(refreshedChildren).toEqual([]);

        resolveRoot?.({ root: '/remote/app', entries: directories.map((_, index) => ({ name: index ? 'two' : 'one', path: index ? 'two' : 'one', is_dir: true })) });
        await waitFor(() => expect(refreshedChildren).toEqual(['one', 'two']));
    });

    it('limits reconnect refreshes of expanded folders to three concurrent requests', async () => {
        let reconnecting = false;
        let activeChildRequests = 0;
        let peakChildRequests = 0;
        const pendingChildren: Array<() => void> = [];
        getDirectory.mockImplementation((_projectPath: string, path: string) => {
            if (!path) return Promise.resolve({ root: '/remote/app', entries: [
                { name: 'one', path: 'one', is_dir: true },
                { name: 'two', path: 'two', is_dir: true },
                { name: 'three', path: 'three', is_dir: true },
                { name: 'four', path: 'four', is_dir: true },
            ] });
            if (!reconnecting) return Promise.resolve({ root: '/remote/app', path, entries: [] });
            activeChildRequests++;
            peakChildRequests = Math.max(peakChildRequests, activeChildRequests);
            return new Promise((resolve) => {
                pendingChildren.push(() => { activeChildRequests--; resolve({ root: '/remote/app', path, entries: [] }); });
            });
        });
        const view = render(<CodePreviewWorkspace projectPath="remote-task" refreshToken={0} lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const directories = await screen.findAllByTestId('code-preview-workspace-directory');
        for (const directory of directories) fireEvent.click(directory);
        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(5));

        reconnecting = true;
        view.rerender(<CodePreviewWorkspace projectPath="remote-task" refreshToken={1} lang="en" theme={theme} onOpenFile={vi.fn()} />);
        await waitFor(() => expect(pendingChildren.length).toBe(3));
        expect(peakChildRequests).toBeLessThanOrEqual(3);
        pendingChildren.splice(0, 3).forEach(resolve => resolve());
        await waitFor(() => expect(pendingChildren.length).toBe(1));
        pendingChildren.shift()?.();
        await waitFor(() => expect(activeChildRequests).toBe(0));
    });

    it('refreshes an already expanded folder from its context-menu preview action', async () => {
        getDirectory
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'src', path: 'src', is_dir: true }] })
            .mockResolvedValueOnce({ root: '/remote/app', path: 'src', entries: [{ name: 'old.go', path: 'src/old.go', is_dir: false }] })
            .mockResolvedValueOnce({ root: '/remote/app', path: 'src', entries: [{ name: 'new.go', path: 'src/new.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const directory = await screen.findByTestId('code-preview-workspace-directory');
        fireEvent.click(directory);
        await screen.findByText('old.go');
        fireEvent.contextMenu(directory, { clientX: 30, clientY: 30 });
        fireEvent.click(screen.getByTestId('code-preview-workspace-context-preview'));
        await screen.findByText('new.go');
        expect(screen.queryByText('old.go')).toBeNull();
    });

    it('reuses a loaded child directory when it is collapsed and reopened', async () => {
        getDirectory
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'src', path: 'src', is_dir: true }] })
            .mockResolvedValueOnce({ root: '/remote/app', path: 'src', entries: [{ name: 'main.go', path: 'src/main.go', is_dir: false }] });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const directory = await screen.findByTestId('code-preview-workspace-directory');
        fireEvent.click(directory);
        await screen.findByText('main.go');
        fireEvent.click(directory);
        expect(screen.queryByText('main.go')).toBeNull();
        fireEvent.click(directory);
        expect(await screen.findByText('main.go')).toBeTruthy();
        expect(getDirectory).toHaveBeenCalledTimes(2);
    });

    it('refreshes a cached child directory after its freshness window expires', async () => {
        let now = 1_000;
        const nowSpy = vi.spyOn(Date, 'now').mockImplementation(() => now);
        getDirectory
            .mockResolvedValueOnce({ root: '/remote/app', entries: [{ name: 'src', path: 'src', is_dir: true }] })
            .mockResolvedValueOnce({ root: '/remote/app', path: 'src', entries: [{ name: 'old.go', path: 'src/old.go', is_dir: false }] })
            .mockResolvedValueOnce({ root: '/remote/app', path: 'src', entries: [{ name: 'new.go', path: 'src/new.go', is_dir: false }] });
        try {
            render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
            const directory = await screen.findByTestId('code-preview-workspace-directory');
            fireEvent.click(directory);
            await screen.findByText('old.go');
            fireEvent.click(directory);
            now += 15_001;
            fireEvent.click(directory);
            await screen.findByText('new.go');
            expect(getDirectory).toHaveBeenCalledTimes(3);
        } finally {
            nowSpy.mockRestore();
        }
    });

    it('does not let a stale properties request overwrite a newer selection', async () => {
        let resolveFirst: ((value: unknown) => void) | undefined;
        getDirectory.mockResolvedValue({ root: '/remote/app', entries: [
            { name: 'first.go', path: 'first.go', is_dir: false },
            { name: 'second.go', path: 'second.go', is_dir: false },
        ] });
        getEntryProperties
            .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }))
            .mockResolvedValueOnce({ name: 'second.go', path: 'second.go', abs_path: '/remote/app/second.go', is_dir: false, size: 2, size_known: true, modified_at: 1_720_000_000, mode: '0644', extension: 'go' });
        render(<CodePreviewWorkspace projectPath="remote-task" lang="en" theme={theme} onOpenFile={vi.fn()} />);
        const [first, second] = await screen.findAllByTestId('code-preview-workspace-file');
        fireEvent.contextMenu(first, { clientX: 30, clientY: 30 });
        fireEvent.click(screen.getByTestId('code-preview-workspace-context-properties'));
        fireEvent.contextMenu(second, { clientX: 30, clientY: 30 });
        fireEvent.click(screen.getByTestId('code-preview-workspace-context-properties'));
        await waitFor(() => expect(screen.getByTestId('code-preview-workspace-properties').textContent).toContain('/remote/app/second.go'));
        resolveFirst?.({ name: 'first.go', path: 'first.go', abs_path: '/remote/app/first.go', is_dir: false, size: 1, size_known: true, modified_at: 1_720_000_000, mode: '0644', extension: 'go' });
        await Promise.resolve();
        expect(screen.getByTestId('code-preview-workspace-properties').textContent).toContain('/remote/app/second.go');
        expect(screen.getByTestId('code-preview-workspace-properties').textContent).not.toContain('/remote/app/first.go');
    });
});
