/**
 * Code preview header double-click maximize + dirty path badge.
 */
import { beforeEach, describe, it, expect, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { CodePreviewPanel, lightCodePreviewTheme } from '../CodePreviewPanel';
import type { CodeFile } from '../useCodePreviewState';
import { __resetCloudWorkspaceDisplayNamesForTests, FOCUS_CLOUD_WORKSPACE_TREE_EVENT, rememberCloudWorkspaceDisplayName } from '../codingTaskMode';

const cloudEntitlement = vi.fn(async () => ({ workspaces: [] as Array<{ id: string; name: string }> }));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetCodingWorkbenchDirectory: vi.fn(async () => ({ root: 'C:/Users/me/.maclaw/data/cloud-workspaces/t/cws', entries: [] })),
    GetCodingWorkbenchFilePreview: vi.fn(async () => ({ path: 'main.ts', content: '', language: 'typescript' })),
    GetCodingWorkbenchEntryProperties: vi.fn(async () => ({})),
    IsCodingWorkbenchVSCodeAvailable: vi.fn(async () => false),
    OpenCodingWorkbenchFileInVSCode: vi.fn(async () => false),
    OpenCodingWorkbenchFileLocally: vi.fn(async () => undefined),
    DownloadCodingWorkbenchEntry: vi.fn(async () => ''),
    DeleteCodingWorkbenchEntry: vi.fn(async () => undefined),
    CloudWorkspaceEntitlement: () => cloudEntitlement(),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showAlert: vi.fn(),
        showConfirm: vi.fn(async () => false),
        showPrompt: vi.fn(),
    }),
}));

beforeEach(() => {
    cloudEntitlement.mockReset();
    cloudEntitlement.mockResolvedValue({ workspaces: [] });
    __resetCloudWorkspaceDisplayNamesForTests();
});

function makeFiles(): Map<string, CodeFile> {
    const file: CodeFile = {
        filePath: '/src/main.ts',
        fileName: 'main.ts',
        content: 'const x = 2;',
        original: 'const x = 1;',
        opType: 'modify',
        language: 'typescript',
        updatedAt: 1,
        absPath: 'D:\\proj\\src\\main.ts',
    };
    return new Map([[file.filePath, file]]);
}

describe('CodePreviewPanel maximize + dirty', () => {
    it('double-clicks empty header area to toggle maximize', () => {
        const onToggleMaximize = vi.fn();
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                onToggleMaximize={onToggleMaximize}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        fireEvent.doubleClick(screen.getByTestId('code-preview-header'));
        expect(onToggleMaximize).toHaveBeenCalledTimes(1);
    });

    it('does not maximize when double-clicking the close button', () => {
        const onToggleMaximize = vi.fn();
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                onToggleMaximize={onToggleMaximize}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        const closeBtn = screen.getByTitle('Close code preview');
        fireEvent.doubleClick(closeBtn);
        expect(onToggleMaximize).not.toHaveBeenCalled();
    });

    it('shows dirty badge on the path bar for modified files', () => {
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );

        expect(screen.getByTestId('code-preview-active-path')).toBeTruthy();
        expect(screen.getByTestId('code-preview-diff-stat').textContent).toContain('+1');
        expect(screen.getByTestId('code-preview-diff-stat').textContent).toContain('-1');
        expect(screen.queryByTestId('code-preview-dirty-badge')).toBeNull();
    });

    it('hides the local cache path on a cloud file breadcrumb', () => {
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                cloudMode
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
            />,
        );
        expect(screen.getByTestId('code-preview-active-path').getAttribute('title')).toBe('/src/main.ts');
        expect(screen.getByTestId('code-preview-active-path').textContent).toContain('/src/main.ts');
        expect(screen.queryByText(/cloud-workspaces/i)).toBeNull();
        expect(screen.queryByText(/D:\\proj/i)).toBeNull();
    });

    it('switches a cloud workspace back to the file tree on focus', () => {
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                projectPath="C:/Users/me/.maclaw/data/cloud-workspaces/t/cws"
                cloudMode
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
            />,
        );
        expect(screen.getByTestId('code-preview-workspace-tab').textContent).toContain('云端文件');
        fireEvent.click(screen.getByTestId('file-tab'));
        expect(screen.getByTestId('code-preview-active-path')).toBeTruthy();
        act(() => {
            window.dispatchEvent(new Event(FOCUS_CLOUD_WORKSPACE_TREE_EVENT));
        });
        expect(screen.getByTestId('code-preview-workspace').getAttribute('data-cloud-mode')).toBe('true');
        expect(screen.queryByText(/cloud-workspaces/i)).toBeNull();
    });

    it('keeps an open file visible when the cloud workspace listing refreshes', () => {
        const view = render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                projectPath="C:/Users/me/.maclaw/data/cloud-workspaces/t/cws"
                cloudMode
                workspaceRefreshToken={1}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
            />,
        );
        fireEvent.click(screen.getByTestId('file-tab'));
        expect(screen.getByTestId('code-preview-active-path')).toBeTruthy();
        view.rerender(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                projectPath="C:/Users/me/.maclaw/data/cloud-workspaces/t/cws"
                cloudMode
                workspaceRefreshToken={2}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
            />,
        );
        expect(screen.getByTestId('code-preview-active-path')).toBeTruthy();
        expect(screen.queryByTestId('code-preview-workspace')).toBeNull();
    });

    it('keeps an open file visible when the cloud workspace root identity resolves', () => {
        const view = render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                projectPath="C:/Users/me/.maclaw/data/tasks/cloud-1"
                cloudMode
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
            />,
        );
        fireEvent.click(screen.getByTestId('file-tab'));
        expect(screen.getByTestId('code-preview-active-path')).toBeTruthy();
        view.rerender(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                projectPath="C:/Users/me/.maclaw/data/cloud-workspaces/t/cws"
                cloudMode
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
            />,
        );
        expect(screen.getByTestId('code-preview-active-path')).toBeTruthy();
        expect(screen.queryByTestId('code-preview-workspace')).toBeNull();
    });

    it('shows the Hub workspace name in the empty cloud preview header', async () => {
        cloudEntitlement.mockResolvedValue({
            workspaces: [{ id: 'cws_abc', name: '标书项目' }],
        });
        render(
            <CodePreviewPanel
                files={new Map()}
                activeFilePath=""
                projectPath="C:/Users/me/.maclaw/data/cloud-workspaces/tenant_default/cws_abc"
                cloudMode
                hideHeaderClose
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
            />,
        );
        expect(screen.getByTestId('code-preview-header').textContent).toContain('云端工作区');
        await waitFor(() => {
            expect(screen.getByTestId('code-preview-cloud-workspace-name').textContent).toBe('标书项目');
        });
        expect(screen.getByTestId('code-preview-header').textContent).toContain('·');
        expect(screen.queryByTitle('Close code preview')).toBeNull();
        expect(screen.queryByText(/cloud-workspaces/i)).toBeNull();
    });

    it('paints a cached Hub workspace name before entitlement returns', () => {
        rememberCloudWorkspaceDisplayName('cws_abc', '标书项目');
        cloudEntitlement.mockImplementation(() => new Promise(() => {}));
        render(
            <CodePreviewPanel
                files={new Map()}
                activeFilePath=""
                projectPath="C:/Users/me/.maclaw/data/cloud-workspaces/tenant_default/cws_abc"
                cloudMode
                cloudWorkspaceName="云端工作区任务1"
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
            />,
        );
        expect(screen.getByTestId('code-preview-cloud-workspace-name').textContent).toBe('标书项目');
        expect(screen.queryByText('云端工作区任务1')).toBeNull();
    });

    it('shows the Hub workspace name on the cloud files tab after a file is open', async () => {
        cloudEntitlement.mockResolvedValue({
            workspaces: [{ id: 'cws', name: '工作区 1' }],
        });
        render(
            <CodePreviewPanel
                files={makeFiles()}
                activeFilePath="/src/main.ts"
                projectPath="C:/Users/me/.maclaw/data/cloud-workspaces/t/cws"
                cloudMode
                hideHeaderClose
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
            />,
        );
        await waitFor(() => {
            expect(screen.getByTestId('code-preview-cloud-workspace-name').textContent).toBe('工作区 1');
        });
        expect(screen.queryByTitle('Close code preview')).toBeNull();
        expect(screen.getByTestId('code-preview-workspace-tab').textContent).toContain('云端文件');
    });
});
