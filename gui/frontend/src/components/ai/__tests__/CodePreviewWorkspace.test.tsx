import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
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
        await waitFor(() => expect(screen.getByRole('status').textContent).toContain('SSH reconnecting'));
        expect(screen.getByText('cached.go')).toBeTruthy();
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
