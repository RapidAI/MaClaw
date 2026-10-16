import { render, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { lightCodePreviewTheme } from '../../ai/CodePreviewPanel';
import { FilePreviewHost } from '../FilePreviewHost';

vi.mock('../../../../wailsjs/go/main/App', () => ({
    PreviewTaskResultFile: vi.fn(async () => ({
        kind: 'pdf',
        preview_url: '/maclaw-preview/v1/file?t=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
    })),
    AIAssistantAttachmentFullDataURL: vi.fn(async () => ''),
    PptxPreviewEnsure: vi.fn(async () => ({ images: ['D:/decks/slide_001.png'] })),
    PptxSlideThumbnailDataURL: vi.fn(async () => 'data:image/png;base64,thumb'),
    CloudWorkspaceEntitlement: vi.fn(async () => ({ workspaces: [] })),
    GetCodingWorkbenchDirectory: vi.fn(async () => ({ root: '', entries: [] })),
    GetCodingWorkbenchFilePreview: vi.fn(async () => ({ path: '', content: '', language: 'go' })),
    GetCodingWorkbenchEntryProperties: vi.fn(async () => ({})),
    IsCodingWorkbenchVSCodeAvailable: vi.fn(async () => false),
    OpenCodingWorkbenchFileInVSCode: vi.fn(async () => false),
    OpenCodingWorkbenchFileLocally: vi.fn(async () => undefined),
    DownloadCodingWorkbenchEntry: vi.fn(async () => ''),
    DeleteCodingWorkbenchEntry: vi.fn(async () => undefined),
}));

vi.mock('../../CustomDialog', () => ({
    useDialog: () => ({
        showAlert: vi.fn(),
        showConfirm: vi.fn(async () => true),
        showPrompt: vi.fn(),
    }),
}));

describe('FilePreviewHost', () => {
    it('opens PPTX through the shared visual preview, not the code chrome', async () => {
        const { getByTestId, queryByTestId } = render(
            <FilePreviewHost
                fileName="布偶小猫5岁生日.pptx"
                absPath="D:/docs/cat.pptx"
                theme={lightCodePreviewTheme}
                lang="zh"
            />,
        );
        expect(getByTestId('file-preview-host')).toBeTruthy();
        await waitFor(() => expect(queryByTestId('pptx-preview-panel') || queryByTestId('pptx-preview-loading')).toBeTruthy());
        expect(queryByTestId('code-preview-panel')).toBeNull();
    });

    it('opens source files with find / go-to-line chrome', () => {
        const { getByTestId } = render(
            <FilePreviewHost
                fileName="main.go"
                content={'package main\n\nfunc main() {}\n'}
                language="go"
                theme={lightCodePreviewTheme}
                lang="en"
            />,
        );
        expect(getByTestId('code-preview-panel')).toBeTruthy();
        expect(getByTestId('code-preview-plain-view')).toBeTruthy();
        expect(getByTestId('code-preview-embedded-toolbar')).toBeTruthy();
    });
});
