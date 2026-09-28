/**
 * Working-directory tab vs open-file tabs: selected tab must match the body.
 * A selected "READ snake.cpp" tab over a directory listing is the split this covers.
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { CodePreviewPanel, lightCodePreviewTheme } from '../CodePreviewPanel';
import { __resetWorkspaceDirectoryCacheForTests } from '../CodePreviewWorkspace';
import type { CodeFile } from '../useCodePreviewState';

const getDirectory = vi.fn(async () => ({
    root: 'F:/test-prog',
    entries: [
        { name: 'CMakeLists.txt', path: 'CMakeLists.txt', is_dir: false },
        { name: 'snake.cpp', path: 'snake.cpp', is_dir: false },
        { name: 'snake.exe', path: 'snake.exe', is_dir: false },
    ],
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetCodingWorkbenchDirectory: () => getDirectory(),
    GetCodingWorkbenchFilePreview: vi.fn(async () => ({ path: 'snake.cpp', content: 'int main() {}', language: 'cpp' })),
    GetCodingWorkbenchEntryProperties: vi.fn(async () => ({})),
    IsCodingWorkbenchVSCodeAvailable: vi.fn(async () => false),
    OpenCodingWorkbenchFileInVSCode: vi.fn(async () => false),
    OpenCodingWorkbenchFileLocally: vi.fn(async () => undefined),
    DownloadCodingWorkbenchEntry: vi.fn(async () => ''),
    DeleteCodingWorkbenchEntry: vi.fn(async () => undefined),
    CloudWorkspaceEntitlement: vi.fn(async () => ({ workspaces: [] })),
    PreviewTaskResultFile: vi.fn(async () => ({ kind: 'pdf', data_url: "data:" + "application/" + "pdf" + ";base64," + "JVBE" + "Ri0=" })),
    PptxPreviewEnsure: vi.fn(async () => ({ images: [] })),
    PptxSlideThumbnailDataURL: vi.fn(async () => ''),
    AIAssistantAttachmentFullDataURL: vi.fn(async () => ''),
}));

beforeEach(() => {
    getDirectory.mockClear();
    __resetWorkspaceDirectoryCacheForTests();
});

function snakeFile(overrides: Partial<CodeFile> = {}): CodeFile {
    return {
        filePath: 'snake.cpp',
        fileName: 'snake.cpp',
        content: 'int main() { return 0; }',
        opType: 'read',
        language: 'cpp',
        updatedAt: 1,
        absPath: 'F:\\test-prog\\snake.cpp',
        ...overrides,
    };
}

function cmakeFile(): CodeFile {
    return {
        filePath: 'CMakeLists.txt',
        fileName: 'CMakeLists.txt',
        content: 'cmake_minimum_required(VERSION 3.16)',
        opType: 'read',
        language: 'cmake',
        updatedAt: 2,
        absPath: 'F:\\test-prog\\CMakeLists.txt',
    };
}

function renderLocalPreview(files: Map<string, CodeFile>, activeFilePath: string, extra: Record<string, unknown> = {}) {
    return render(
        <CodePreviewPanel
            files={files}
            activeFilePath={activeFilePath}
            onSelectFile={vi.fn()}
            onClose={vi.fn()}
            theme={lightCodePreviewTheme}
            lang="zh-Hans"
            projectPath="F:/test-prog"
            hideHeaderClose
            {...extra}
        />,
    );
}

describe('CodePreviewPanel workspace vs file tabs', () => {
    it('selects the working-directory tab, not the open file, while the tree is showing', async () => {
        const file = snakeFile();
        renderLocalPreview(new Map([[file.filePath, file]]), file.filePath);

        const workspaceTab = screen.getByTestId('code-preview-workspace-tab');
        expect(workspaceTab.getAttribute('aria-selected')).toBe('true');
        expect(workspaceTab.textContent).toContain('工作目录');

        const fileTab = screen.getByTestId('file-tab');
        expect(fileTab.textContent).toContain('snake.cpp');
        expect(fileTab.getAttribute('aria-selected')).toBe('false');
        expect(fileTab.getAttribute('data-active')).toBe('false');

        expect(screen.getByTestId('code-preview-workspace')).toBeTruthy();
        expect(screen.queryByTestId('code-preview-active-path')).toBeNull();
        expect(screen.queryByTestId('code-preview-wrap-toggle')).toBeNull();
        expect(await screen.findByRole('button', { name: '刷新工作目录' })).toBeTruthy();
        expect(screen.getAllByTestId('code-preview-workspace-file').length).toBeGreaterThan(0);
        expect(screen.getByTestId('code-preview-tab-strip').style.display).toBe('flex');
        // Path bar keeps the directory, but the "工作目录" title lives only on the selected tab.
        expect(screen.getByTestId('code-preview-workspace-root-label').textContent).toBe('F:/test-prog');
        expect(screen.getAllByText('工作目录', { exact: true })).toHaveLength(1);
    });

    it('shows the file body after clicking its tab and does not bounce back to the tree', async () => {
        const file = snakeFile();
        const onSelectFile = vi.fn();
        renderLocalPreview(new Map([[file.filePath, file]]), file.filePath, { onSelectFile });

        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        fireEvent.click(screen.getByTestId('file-tab'));
        expect(onSelectFile).toHaveBeenCalledWith('snake.cpp');

        expect(screen.getByTestId('code-preview-workspace-tab').getAttribute('aria-selected')).toBe('false');
        expect(screen.getByTestId('file-tab').getAttribute('aria-selected')).toBe('true');
        expect(screen.getByTestId('code-preview-active-path').textContent).toContain('snake.cpp');
        expect(screen.queryByTestId('code-preview-workspace')).toBeNull();
        expect(screen.getByTestId('code-preview-wrap-toggle')).toBeTruthy();
        expect(screen.getByTestId('code-preview-plain-view').textContent).toContain('int main()');
    });

    it('keeps the open file visible when the parent switches the active file', async () => {
        const snake = snakeFile();
        const cmake = cmakeFile();
        const firstFiles = new Map([[snake.filePath, snake]]);
        const view = renderLocalPreview(firstFiles, snake.filePath);

        fireEvent.click(screen.getByTestId('file-tab'));
        expect(screen.getByTestId('code-preview-active-path').textContent).toContain('snake.cpp');

        const both = new Map([[snake.filePath, snake], [cmake.filePath, cmake]]);
        view.rerender(
            <CodePreviewPanel
                files={both}
                activeFilePath={cmake.filePath}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
                projectPath="F:/test-prog"
                hideHeaderClose
            />,
        );

        await waitFor(() => {
            expect(screen.getByTestId('code-preview-active-path').textContent).toContain('CMakeLists.txt');
        });
        expect(screen.queryByTestId('code-preview-workspace')).toBeNull();
        const tabs = screen.getAllByTestId('file-tab');
        const cmakeTab = tabs.find((tab) => tab.getAttribute('data-file-path') === 'CMakeLists.txt');
        expect(cmakeTab?.getAttribute('aria-selected')).toBe('true');
    });

    it('Ctrl+Tab from the working directory returns to the last-focused file', async () => {
        const snake = snakeFile();
        const cmake = cmakeFile();
        const onSelectFile = vi.fn();
        renderLocalPreview(new Map([[snake.filePath, snake], [cmake.filePath, cmake]]), snake.filePath, { onSelectFile });

        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        fireEvent.keyDown(screen.getByTestId('code-preview-panel'), { key: 'Tab', ctrlKey: true });
        expect(onSelectFile).toHaveBeenCalledWith('snake.cpp');
        expect(screen.getByTestId('code-preview-active-path').textContent).toContain('snake.cpp');
        expect(screen.queryByTestId('code-preview-workspace')).toBeNull();
    });

    it('arrow keys move between the working-directory tab and file tabs', async () => {
        const snake = snakeFile();
        const cmake = cmakeFile();
        renderLocalPreview(new Map([[snake.filePath, snake], [cmake.filePath, cmake]]), snake.filePath);

        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        const workspaceTab = screen.getByTestId('code-preview-workspace-tab');
        workspaceTab.focus();
        fireEvent.keyDown(workspaceTab, { key: 'ArrowRight' });
        expect(screen.getByTestId('code-preview-active-path').textContent).toContain('snake.cpp');
        expect(screen.queryByTestId('code-preview-workspace')).toBeNull();
        const snakeTab = screen.getByRole('tab', { name: /snake\.cpp/ });
        expect(document.activeElement).toBe(snakeTab);

        fireEvent.keyDown(snakeTab, { key: 'ArrowLeft' });
        expect(screen.getByTestId('code-preview-workspace-tab').getAttribute('aria-selected')).toBe('true');
        expect(screen.getByTestId('code-preview-workspace')).toBeTruthy();
        expect(document.activeElement).toBe(screen.getByTestId('code-preview-workspace-tab'));
    });

    it('owns a single tablist for the working-directory tab and file tabs', async () => {
        const file = snakeFile();
        renderLocalPreview(new Map([[file.filePath, file]]), file.filePath);
        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        const strip = screen.getByTestId('code-preview-tab-strip');
        expect(strip.getAttribute('role')).toBe('tablist');
        expect(strip.querySelector('[data-testid="code-preview-file-list-toggle"]')).toBeNull();
        expect(screen.getByTestId('code-preview-file-list-toggle')).toBeTruthy();
        expect(screen.getByTestId('file-tab-bar').getAttribute('role')).toBeNull();
        expect(screen.getAllByRole('tab').map((tab) => tab.getAttribute('data-testid'))).toEqual([
            'code-preview-workspace-tab',
            'file-tab',
        ]);
    });

    it('renders the PDF viewer for a generated pdf document', async () => {
        const file: CodeFile = {
            filePath: 'F:\\docs\\report.pdf',
            fileName: 'report.pdf',
            absPath: 'F:\\docs\\report.pdf',
            content: '',
            language: 'pdf',
            opType: 'read',
            updatedAt: 1,
        };
        render(
            <CodePreviewPanel
                files={new Map([[file.filePath, file]])}
                activeFilePath={file.filePath}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
                hideHeaderClose
            />,
        );
        expect(await screen.findByTestId('pdf-preview-panel')).toBeTruthy();
        expect(screen.queryByTestId('code-preview-wrap-toggle')).toBeNull();
    });

    it('shows a cloud result preview instead of leaving the file tree selected', async () => {
        const file: CodeFile = {
            filePath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws/papers/report.pdf',
            fileName: 'report.pdf',
            absPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws/papers/report.pdf',
            content: '',
            language: 'pdf',
            opType: 'read',
            updatedAt: 1,
        };
        const view = render(
            <CodePreviewPanel
                files={new Map([[file.filePath, file]])}
                activeFilePath={file.filePath}
                projectPath="C:/Users/me/.maclaw/data/cloud-workspaces/t/cws"
                cloudMode
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
                hideHeaderClose
            />,
        );
        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        view.rerender(
            <CodePreviewPanel
                files={new Map([[file.filePath, file]])}
                activeFilePath={file.filePath}
                projectPath="C:/Users/me/.maclaw/data/cloud-workspaces/t/cws"
                cloudMode
                fileFocusNonce={1}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
                hideHeaderClose
            />,
        );
        expect(await screen.findByTestId('pdf-preview-panel')).toBeTruthy();
        expect(screen.queryByTestId('code-preview-workspace')).toBeNull();
        fireEvent.click(screen.getByTestId('code-preview-workspace-tab'));
        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        expect(screen.queryByTestId('pdf-preview-panel')).toBeNull();
    });

    it('returns to the cloud file tree when the previewed file is closed', async () => {
        const file: CodeFile = {
            filePath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws/papers/report.pdf',
            fileName: 'report.pdf',
            absPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws/papers/report.pdf',
            content: '',
            language: 'pdf',
            opType: 'read',
            updatedAt: 1,
        };
        const props = {
            projectPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws',
            cloudMode: true,
            onSelectFile: vi.fn(),
            onClose: vi.fn(),
            theme: lightCodePreviewTheme,
            lang: 'zh-Hans',
            hideHeaderClose: true,
        };
        const view = render(
            <CodePreviewPanel
                files={new Map([[file.filePath, file]])}
                activeFilePath={file.filePath}
                {...props}
            />,
        );
        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        view.rerender(
            <CodePreviewPanel
                files={new Map([[file.filePath, file]])}
                activeFilePath={file.filePath}
                fileFocusNonce={1}
                {...props}
            />,
        );
        expect(await screen.findByTestId('pdf-preview-panel')).toBeTruthy();
        view.rerender(
            <CodePreviewPanel
                files={new Map()}
                activeFilePath=""
                {...props}
            />,
        );
        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        expect(screen.queryByTestId('pdf-preview-panel')).toBeNull();
        view.rerender(
            <CodePreviewPanel
                files={new Map([[file.filePath, file]])}
                activeFilePath={file.filePath}
                {...props}
            />,
        );
        expect(screen.getByTestId('code-preview-workspace')).toBeTruthy();
        expect(screen.queryByTestId('pdf-preview-panel')).toBeNull();
    });

    it('keeps the directory tree when the open file list refreshes', async () => {
        const file: CodeFile = {
            filePath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws/papers/report.pdf',
            fileName: 'report.pdf',
            absPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws/papers/report.pdf',
            content: '',
            language: 'pdf',
            opType: 'read',
            updatedAt: 1,
        };
        const props = {
            projectPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws',
            cloudMode: true,
            onSelectFile: vi.fn(),
            onClose: vi.fn(),
            theme: lightCodePreviewTheme,
            lang: 'zh-Hans' as const,
            hideHeaderClose: true,
        };
        const view = render(
            <CodePreviewPanel
                files={new Map([[file.filePath, file]])}
                activeFilePath={file.filePath}
                fileFocusNonce={1}
                {...props}
            />,
        );
        expect(await screen.findByTestId('pdf-preview-panel')).toBeTruthy();
        fireEvent.click(screen.getByTestId('code-preview-workspace-tab'));
        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        view.rerender(
            <CodePreviewPanel
                files={new Map([[file.filePath, { ...file, updatedAt: 2 }]])}
                activeFilePath={file.filePath}
                fileFocusNonce={1}
                {...props}
            />,
        );
        expect(screen.getByTestId('code-preview-workspace')).toBeTruthy();
        expect(screen.queryByTestId('pdf-preview-panel')).toBeNull();
    });

    it('opens the directory tree when a file focus outlives the closed file', async () => {
        render(
            <CodePreviewPanel
                files={new Map()}
                activeFilePath=""
                projectPath="C:/Users/me/.maclaw/data/cloud-workspaces/t/cws"
                cloudMode
                fileFocusNonce={1}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
                hideHeaderClose
            />,
        );
        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        expect(screen.queryByTestId('pdf-preview-panel')).toBeNull();
    });

    it('follows the newer focus when the tree and the file trade places', async () => {
        const file: CodeFile = {
            filePath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws/papers/report.pdf',
            fileName: 'report.pdf',
            absPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws/papers/report.pdf',
            content: '',
            language: 'pdf',
            opType: 'read',
            updatedAt: 1,
        };
        const props = {
            files: new Map([[file.filePath, file]]),
            activeFilePath: file.filePath,
            projectPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws',
            cloudMode: true,
            onSelectFile: vi.fn(),
            onClose: vi.fn(),
            theme: lightCodePreviewTheme,
            lang: 'zh-Hans' as const,
            hideHeaderClose: true,
        };
        const view = render(
            <CodePreviewPanel fileFocusNonce={1} treeFocusNonce={4} {...props} />,
        );
        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
        expect(screen.queryByTestId('pdf-preview-panel')).toBeNull();
        view.rerender(<CodePreviewPanel fileFocusNonce={5} treeFocusNonce={4} {...props} />);
        expect(await screen.findByTestId('pdf-preview-panel')).toBeTruthy();
        expect(screen.queryByTestId('code-preview-workspace')).toBeNull();
    });

    it('shows the directory after a tree focus before the project path exists', async () => {
        const file: CodeFile = {
            filePath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws/papers/report.pdf',
            fileName: 'report.pdf',
            absPath: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws/papers/report.pdf',
            content: '',
            language: 'pdf',
            opType: 'read',
            updatedAt: 1,
        };
        const props = {
            files: new Map([[file.filePath, file]]),
            activeFilePath: file.filePath,
            onSelectFile: vi.fn(),
            onClose: vi.fn(),
            theme: lightCodePreviewTheme,
            lang: 'zh-Hans' as const,
            hideHeaderClose: true,
        };
        const view = render(<CodePreviewPanel fileFocusNonce={1} {...props} />);
        expect(await screen.findByTestId('pdf-preview-panel')).toBeTruthy();
        view.rerender(<CodePreviewPanel fileFocusNonce={1} treeFocusNonce={2} {...props} />);
        expect(await screen.findByTestId('code-preview-workspace-status')).toBeTruthy();
        expect(screen.queryByTestId('pdf-preview-panel')).toBeNull();
    });

    it('closes the preview when the last file is gone and there is no working directory', () => {
        const onClose = vi.fn();
        const file = snakeFile();
        const view = render(
            <CodePreviewPanel
                files={new Map([[file.filePath, file]])}
                activeFilePath={file.filePath}
                onSelectFile={vi.fn()}
                onClose={onClose}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
                hideHeaderClose
            />,
        );
        expect(onClose).not.toHaveBeenCalled();

        view.rerender(
            <CodePreviewPanel
                files={new Map()}
                activeFilePath=""
                onSelectFile={vi.fn()}
                onClose={onClose}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
                hideHeaderClose
            />,
        );
        expect(onClose).toHaveBeenCalledTimes(1);
        expect(screen.queryByText('工作目录不可用')).toBeNull();

        view.rerender(
            <CodePreviewPanel
                files={new Map()}
                activeFilePath=""
                onSelectFile={vi.fn()}
                onClose={onClose}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
                hideHeaderClose
            />,
        );
        expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('keeps the workspace tree when files are empty but a project path is set', async () => {
        const onClose = vi.fn();
        renderLocalPreview(new Map(), '', { onClose });
        expect(onClose).not.toHaveBeenCalled();
        expect(await screen.findByTestId('code-preview-workspace')).toBeTruthy();
    });
});
