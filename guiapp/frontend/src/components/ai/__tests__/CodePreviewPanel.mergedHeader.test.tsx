/**
 * Merged preview header: the panel header hosts the refresh button and the
 * workspace tree's own header row is suppressed (empty-files view).
 */
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { CodePreviewPanel, lightCodePreviewTheme } from '../CodePreviewPanel';
import { __resetWorkspaceDirectoryCacheForTests } from '../CodePreviewWorkspace';
import { __resetCloudWorkspaceDisplayNamesForTests } from '../codingTaskMode';

const getDirectory = vi.fn(async () => ({ root: 'C:/proj', entries: [{ name: 'a.go', path: 'a.go', is_dir: false }] }));
const cloudEntitlement = vi.fn(async () => ({ workspaces: [] as Array<{ id: string; name: string }> }));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    GetCodingWorkbenchDirectory: () => getDirectory(),
    GetCodingWorkbenchFilePreview: vi.fn(async () => ({ path: 'a.go', content: '', language: 'go' })),
    GetCodingWorkbenchEntryProperties: vi.fn(async () => ({})),
    IsCodingWorkbenchVSCodeAvailable: vi.fn(async () => false),
    OpenCodingWorkbenchFileInVSCode: vi.fn(async () => false),
    OpenCodingWorkbenchFileLocally: vi.fn(async () => undefined),
    DownloadCodingWorkbenchEntry: vi.fn(async () => ''),
    DeleteCodingWorkbenchEntry: vi.fn(async () => undefined),
    CloudWorkspaceEntitlement: () => cloudEntitlement(),
    PreviewTaskResultFile: vi.fn(async () => ({ kind: 'pdf' })),
    PptxPreviewEnsure: vi.fn(async () => ({ images: [] })),
    PptxSlideThumbnailDataURL: vi.fn(async () => ''),
    AIAssistantAttachmentFullDataURL: vi.fn(async () => ''),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showAlert: vi.fn(),
        showConfirm: vi.fn(async () => true),
        showPrompt: vi.fn(),
    }),
}));

beforeEach(() => {
    getDirectory.mockClear();
    cloudEntitlement.mockReset();
    cloudEntitlement.mockResolvedValue({ workspaces: [] });
    __resetWorkspaceDirectoryCacheForTests();
    __resetCloudWorkspaceDisplayNamesForTests();
});

function renderPanel(extra: Record<string, unknown> = {}) {
    return render(
        <CodePreviewPanel
            files={new Map()}
            activeFilePath=""
            onSelectFile={vi.fn()}
            onClose={vi.fn()}
            theme={lightCodePreviewTheme}
            lang="zh-Hans"
            projectPath="cloud-task"
            {...extra}
        />,
    );
}

describe('CodePreviewPanel merged header', () => {
    it('cloud mode: header hosts the only refresh button and suppresses the workspace row', async () => {
        renderPanel({ cloudMode: true });
        expect(await screen.findByText('a.go')).toBeTruthy();
        expect(screen.getAllByRole('button', { name: '刷新云端文件' })).toHaveLength(1);
        expect(screen.queryByTestId('code-preview-workspace-root-label')).toBeNull();
        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByTestId('code-preview-header-refresh'));
        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(2));
    });

    it('local mode: header shows the working directory path and refresh reloads the tree', async () => {
        renderPanel({ projectPath: 'F:/test-prog' });
        expect(await screen.findByText('a.go')).toBeTruthy();
        expect(screen.getByTestId('code-preview-header').textContent).toContain('F:/test-prog');
        expect(screen.getAllByRole('button', { name: '刷新工作目录' })).toHaveLength(1);
        expect(screen.queryByTestId('code-preview-workspace-root-label')).toBeNull();
        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(1));
        fireEvent.click(screen.getByTestId('code-preview-header-refresh'));
        await waitFor(() => expect(getDirectory).toHaveBeenCalledTimes(2));
    });

    it('disables the header refresh when there is no project path', () => {
        renderPanel({ projectPath: undefined });
        const refresh = screen.getByTestId('code-preview-header-refresh') as HTMLButtonElement;
        expect(refresh.disabled).toBe(true);
    });
});
