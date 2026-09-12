import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { AssistantPreviewPane } from '../AssistantPreviewPane';
import type { Theme } from '../aiAssistantPanelTheme';
import type { CodeFile } from '../useCodePreviewState';

const theme = {
    bg: '#ffffff',
    titleBarBg: '#f8fafc',
    titleBarBorder: '#e5e7eb',
    titleText: '#111827',
    text: '#111827',
    textMuted: '#6b7280',
    inputBarBg: '#ffffff',
    inputBarBorder: '#6366f1',
    inputText: '#111827',
    codeBg: '#f1f5f9',
    codeText: '#111827',
    codeBlockBg: '#0f172a',
    codeBlockBorder: '#334155',
    codeBlockLang: '#94a3b8',
    borderLeft: '#e5e7eb',
    responseBorderLeft: '#6366f1',
    headingColor: '#4f46e5',
    linkColor: '#2563eb',
    pathColor: '#059669',
    promptColor: '#4f46e5',
    userColor: '#4f46e5',
    divider: '#e5e7eb',
    fieldBg: '#f9fafb',
    fieldBorder: '#d1d5db',
    fieldLabel: '#6b7280',
    errorText: '#dc2626',
    errorBg: '#fef2f2',
    errorBorder: '#fecaca',
    emptyHint: '#9ca3af',
    boldColor: '#111827',
    italicColor: '#374151',
    bulletColor: '#4f46e5',
    quoteBorder: '#c7d2fe',
    quoteText: '#374151',
    btnColor: '#111827',
    btnBorder: '#d1d5db',
    actionBtnColor: '#4f46e5',
    closeBtnColor: '#dc2626',
    sendBtnColor: '#ffffff',
    sendBtnBorder: '#4f46e5',
    sendBtnBg: '#4f46e5',
} as Theme;

const file: CodeFile = {
    filePath: '/src/main.ts',
    fileName: 'main.ts',
    content: 'const answer = 42;',
    opType: 'modify',
    language: 'typescript',
    updatedAt: 1,
};

const emptyCodePreviewState = {
    active: false,
    files: new Map<string, CodeFile>(),
    activeFilePath: '',
    sessionID: 'session-1',
    sessionActive: true,
    userClosed: false,
    pinnedPaths: [] as string[],
    mruOrder: [] as string[],
};

const activeEmptyCodePreviewState = {
    ...emptyCodePreviewState,
    active: true,
};

const activeCodePreviewState = {
    active: true,
    files: new Map([[file.filePath, file]]),
    activeFilePath: file.filePath,
    sessionID: 'session-1',
    sessionActive: true,
    userClosed: false,
    pinnedPaths: [] as string[],
    mruOrder: [file.filePath],
};

const agentView = {
    type: 'progress' as const,
    id: 'agent-view-1',
    title: 'Task',
    steps: [],
    actions: [],
};

const workflowState = {
    active: true,
    splitMode: true,
    splitRatio: 0.42,
    workflowType: 'coding',
    currentPhaseID: 'requirements',
    latestDocumentPhaseID: 'requirements',
    phaseDocuments: new Map([['requirements', '# Requirements']]),
    gateResults: new Map(),
    phases: [],
    suggestMaximize: false,
    suggestMaximizeType: '',
    awaitingForm: false,
    transientText: '',
    workingDir: '',
    workflowID: '',
    docUpdatePhaseIDs: new Set<string>(),
};

function renderPane() {
    return render(
        <AssistantPreviewPane
            codePreviewState={activeCodePreviewState}
            closeCodePreview={vi.fn()}
            closeDocPreview={vi.fn()}
            lang="en"
            selectCodeFile={vi.fn()}
            showAgentView={false}
            showCodePreview={true}
            showWorkflowPreview={true}
            splitRatio={0.42}
            startPreviewResize={vi.fn()}
            theme={theme}
            workflowState={workflowState}
        />,
    );
}

function renderPaneWithCodeState(codePreviewState: typeof emptyCodePreviewState | typeof activeCodePreviewState) {
    return render(
        <AssistantPreviewPane
            codePreviewState={codePreviewState}
            closeCodePreview={vi.fn()}
            closeDocPreview={vi.fn()}
            lang="en"
            selectCodeFile={vi.fn()}
            showAgentView={false}
            showCodePreview={codePreviewState.active}
            showWorkflowPreview={true}
            splitRatio={0.42}
            startPreviewResize={vi.fn()}
            theme={theme}
            workflowState={workflowState}
        />,
    );
}

describe('AssistantPreviewPane', () => {
    it('keeps the embedded agent splitter wired when preview tabs are combined', async () => {
        const startPreviewResize = vi.fn();
        render(
            <AssistantPreviewPane
                agentView={agentView}
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={true}
                showCodePreview={true}
                showWorkflowPreview={true}
                splitRatio={0.42}
                startPreviewResize={startPreviewResize}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        const agentTab = screen.getByRole('tab', { name: 'Agent Task' });
        fireEvent.click(agentTab);
        await waitFor(() => expect(agentTab.getAttribute('aria-selected')).toBe('true'));
        const separators = screen.getAllByRole('separator');
        expect(separators.length).toBeGreaterThanOrEqual(2);
        fireEvent.keyDown(separators[separators.length - 1], { key: 'ArrowRight' });
        expect(startPreviewResize).toHaveBeenLastCalledWith(0.44);
    });

    it('exposes a pointer and keyboard resize handle for the preview split', () => {
        const startPreviewResize = vi.fn();
        render(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={true}
                splitRatio={0.42}
                startPreviewResize={startPreviewResize}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        const handle = screen.getByTestId('assistant-preview-resize-handle');
        expect(handle.getAttribute('role')).toBe('separator');
        expect(handle.getAttribute('aria-valuenow')).toBe('42');
        fireEvent.pointerDown(handle, { pointerId: 7, clientX: 640 });
        expect(startPreviewResize).toHaveBeenCalledWith(expect.objectContaining({ pointerId: 7 }));
        fireEvent.keyDown(handle, { key: 'ArrowRight' });
        expect(startPreviewResize).toHaveBeenLastCalledWith(0.44);
    });

    it('keeps workflow progress and source preview available behind tabs', () => {
        renderPane();

        expect(screen.getByTestId('assistant-preview-mode-tabs')).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Progress' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('tab', { name: 'Source' })).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Progress' }).getAttribute('aria-controls')).toBe('assistant-preview-panel-workflow');
        expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe('assistant-preview-tab-workflow');
        expect(screen.getAllByText('Requirements').length).toBeGreaterThan(0);

        fireEvent.click(screen.getByRole('tab', { name: 'Source' }));

        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('tabindex')).toBe('0');
        expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe('assistant-preview-tab-code');
        expect(screen.getByTestId('code-preview-header').style.getPropertyValue('--wails-draggable')).toBe('no-drag');
        expect(screen.getByText('answer')).toBeTruthy();
    });

    it('drops the duplicate panel header for the empty local source preview', () => {
        renderPaneWithCodeState(activeEmptyCodePreviewState);

        fireEvent.click(screen.getByRole('tab', { name: 'Source' }));

        expect(screen.queryByTestId('code-preview-header')).toBeNull();
        expect(screen.getByTestId('code-preview-workspace-status')).toBeTruthy();
        expect(screen.getByText('Working directory unavailable')).toBeTruthy();
    });

    it('keeps Working directory as the default source tab when files are already open', () => {
        renderPane();

        fireEvent.click(screen.getByRole('tab', { name: 'Source' }));

        const workspaceTab = screen.getByTestId('code-preview-workspace-tab');
        expect(workspaceTab.getAttribute('aria-selected')).toBe('false');
        fireEvent.click(workspaceTab);
        expect(workspaceTab.getAttribute('aria-selected')).toBe('true');
        expect(screen.getByTestId('code-preview-workspace-status')).toBeTruthy();
    });

    it('auto-switches to source when code preview opens after workflow progress', () => {
        const { rerender } = renderPaneWithCodeState(emptyCodePreviewState);

        expect(screen.queryByRole('tab', { name: 'Source' })).toBeNull();
        expect(screen.getAllByText('Requirements').length).toBeGreaterThan(0);

        rerender(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                    lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={true}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByText('answer')).toBeTruthy();

        fireEvent.click(screen.getByRole('tab', { name: 'Progress' }));
        expect(screen.getByRole('tab', { name: 'Progress' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getAllByText('Requirements').length).toBeGreaterThan(0);
    });

    it('prefers source when workflow and code preview open together', () => {
        const { rerender } = render(
            <AssistantPreviewPane
                codePreviewState={emptyCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={false}
                showWorkflowPreview={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={{ ...workflowState, splitMode: false }}
            />,
        );

        rerender(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={true}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByText('answer')).toBeTruthy();
    });

    it('keeps source preview available when an agent task view is visible', () => {
        render(
            <AssistantPreviewPane
                agentView={agentView}
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                dismissAgentView={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={true}
                showCodePreview={true}
                showWorkflowPreview={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                submitAgentView={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        expect(screen.getByRole('tab', { name: 'Agent Task' })).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Source' })).toBeTruthy();
        // Agent form wins initial focus so the submit UI is visible.
        expect(screen.getByRole('tab', { name: 'Agent Task' }).getAttribute('aria-selected')).toBe('true');

        fireEvent.click(screen.getByRole('tab', { name: 'Source' }));

        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByText('answer')).toBeTruthy();
    });

    it('does not steal focus from an open agent form when source preview becomes active', () => {
        const { rerender } = render(
            <AssistantPreviewPane
                agentView={agentView}
                codePreviewState={emptyCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                dismissAgentView={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={true}
                showCodePreview={false}
                showWorkflowPreview={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                submitAgentView={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        expect(screen.getByTestId('agent-task-panel')).toBeTruthy();

        rerender(
            <AssistantPreviewPane
                agentView={agentView}
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                dismissAgentView={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={true}
                showCodePreview={true}
                showWorkflowPreview={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                submitAgentView={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        expect(screen.getByRole('tab', { name: 'Agent Task' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByTestId('agent-task-panel')).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Source' })).toBeTruthy();
    });

    it('switches to agent form when it opens over an active source preview', () => {
        const { rerender } = render(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        // Single-mode code preview has no mode tab rail — content is enough.
        expect(screen.getByText('answer')).toBeTruthy();
        expect(screen.queryByRole('tab', { name: 'Agent Task' })).toBeNull();

        rerender(
            <AssistantPreviewPane
                agentView={agentView}
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                dismissAgentView={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={true}
                showCodePreview={true}
                showWorkflowPreview={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                submitAgentView={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        expect(screen.getByRole('tab', { name: 'Agent Task' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByTestId('agent-task-panel')).toBeTruthy();
        // Source remains available as a secondary mode tab.
        expect(screen.getByRole('tab', { name: 'Source' })).toBeTruthy();
    });

    it('tabs Conflicts with Source and prefers Conflicts when newly opened', () => {
        const onCloseConflict = vi.fn();
        const { rerender } = render(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={false}
                showConflict={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        expect(screen.queryByRole('tab', { name: 'Conflicts' })).toBeNull();
        expect(screen.getByText('answer')).toBeTruthy();

        rerender(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={false}
                showConflict={true}
                conflictCount={2}
                conflictContent={<div data-testid="coding-conflict-side-panel">conflict body</div>}
                onCloseConflict={onCloseConflict}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        expect(screen.getByRole('tab', { name: /Conflicts/ }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByTestId('coding-conflict-side-panel')).toBeTruthy();
        expect(screen.getByTestId('assistant-preview-conflict-badge').textContent).toBe('2');

        fireEvent.click(screen.getByRole('tab', { name: 'Source' }));
        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByText('answer')).toBeTruthy();
        // Conflict panel stays mounted (hidden) when switching tabs.
        expect(screen.getByTestId('assistant-preview-conflict-slot').getAttribute('aria-hidden')).toBe('true');

        fireEvent.click(screen.getByRole('tab', { name: /Conflicts/ }));
        expect(screen.getByRole('tab', { name: /Conflicts/ }).getAttribute('aria-selected')).toBe('true');
        // Attribute omitted when CF is the active tab.
        expect(screen.getByTestId('assistant-preview-conflict-slot').getAttribute('aria-hidden')).toBeNull();
    });

    it('uses the active scheme error tokens for the conflict tab and badge', () => {
        const themedConflict = {
            ...theme,
            errorText: '#8b2747',
            errorBg: '#f8e8ee',
            errorBorder: '#c88da1',
        } as Theme;

        render(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={false}
                showConflict
                conflictCount={2}
                conflictContent={<div>conflict body</div>}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={themedConflict}
                workflowState={workflowState}
            />,
        );

        const conflictTab = screen.getByRole('tab', { name: /Conflicts/ });
        expect(conflictTab.style.borderColor).toBe('rgb(200, 141, 161)');
        expect(conflictTab.style.color).toBe('rgb(139, 39, 71)');
        expect(screen.getByTestId('assistant-preview-conflict-badge').style.background).toBe('rgb(139, 39, 71)');
    });

    it('supports keyboard switching between preview tabs', () => {
        renderPane();

        const progressTab = screen.getByRole('tab', { name: 'Progress' });
        progressTab.focus();
        fireEvent.keyDown(progressTab, { key: 'ArrowRight' });

        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByText('answer')).toBeTruthy();

        const sourceTab = screen.getByRole('tab', { name: 'Source' });
        sourceTab.focus();
        fireEvent.keyDown(sourceTab, { key: 'ArrowLeft' });

        expect(screen.getByRole('tab', { name: 'Progress' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getAllByText('Requirements').length).toBeGreaterThan(0);

        fireEvent.keyDown(screen.getByRole('tab', { name: 'Progress' }), { key: 'End' });
        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('aria-selected')).toBe('true');

        fireEvent.keyDown(screen.getByRole('tab', { name: 'Source' }), { key: 'Home' });
        expect(screen.getByRole('tab', { name: 'Progress' }).getAttribute('aria-selected')).toBe('true');
    });

    it('opens the file list from the leftmost header button, paginated behind show-more', () => {
        const selectCodeFile = vi.fn();
        const manyFiles = new Map(Array.from({ length: 7 }, (_, i) => {
            const p = `/src/f${i}.go`;
            return [p, { ...file, filePath: p, fileName: `f${i}.go` }];
        }));
        render(
            <AssistantPreviewPane
                codePreviewState={{ ...activeCodePreviewState, files: manyFiles }}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={selectCodeFile}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        fireEvent.click(screen.getByTestId('code-preview-file-list-toggle'));

        expect(screen.getByTestId('code-preview-file-list-panel').textContent).toContain('Artifacts (7)');
        expect(screen.getAllByTestId('code-preview-file-list-item')).toHaveLength(5);
        fireEvent.click(screen.getByTestId('code-preview-file-list-more'));
        expect(screen.getAllByTestId('code-preview-file-list-item')).toHaveLength(7);

        fireEvent.click(screen.getAllByTestId('code-preview-file-list-item')[2]);
        expect(selectCodeFile).toHaveBeenCalledWith('/src/f2.go');
        // Unpinned: selecting a file closes the panel.
        expect(screen.queryByTestId('code-preview-file-list-panel')).toBeNull();
    });

    it('keeps the file list open across selections when pinned', () => {
        const selectCodeFile = vi.fn();
        render(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={selectCodeFile}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        fireEvent.click(screen.getByTestId('code-preview-file-list-toggle'));
        fireEvent.click(screen.getByTestId('code-preview-file-list-pin'));
        fireEvent.click(screen.getByTestId('code-preview-file-list-item'));

        expect(selectCodeFile).toHaveBeenCalledWith('/src/main.ts');
        expect(screen.getByTestId('code-preview-file-list-panel')).toBeTruthy();
    });

    it('shows upload / share / reveal actions for a previewed local file', () => {
        const localFile: CodeFile = { ...file, absPath: 'D:\\proj\\src\\main.ts' };
        render(
            <AssistantPreviewPane
                codePreviewState={{
                    ...activeCodePreviewState,
                    files: new Map([[localFile.filePath, localFile]]),
                }}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="zh"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        expect(screen.getByTestId('preview-file-action-upload').getAttribute('aria-label')).toBe('上传到文稿库');
        expect(screen.getByTestId('preview-file-action-share').getAttribute('aria-label')).toBe('分享（复制文件路径）');
        expect(screen.getByTestId('preview-file-action-reveal').getAttribute('aria-label')).toBe('打开文件夹');
    });

    it('hides the file actions for cloud workspace files, which have no local path', () => {
        render(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={false}
                cloudMode
                cloudWorkspaceName="ws-1"
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        expect(screen.queryByTestId('preview-file-actions')).toBeNull();
    });

    it('expands the pane to full width from the code preview toolbar and restores it', async () => {
        render(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={vi.fn()}
                closeDocPreview={vi.fn()}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={false}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        const pane = document.querySelector('.mc-assistant-preview-pane') as HTMLElement;
        // splitRatio 0.42 → split width 58%.
        expect(pane.style.width).toBe('58%');

        const toggle = screen.getByTestId('code-preview-expand-toggle');
        expect(toggle.textContent).toBe('⤢');
        fireEvent.click(toggle);

        expect(pane.style.width).toBe('100%');
        expect(toggle.textContent).toBe('⤡');
        expect(toggle.getAttribute('data-active')).toBe('true');
        // The resize handle is hidden while expanded — it would have no effect.
        expect(screen.queryByTestId('assistant-preview-resize-handle')).toBeNull();

        fireEvent.click(toggle);
        expect(pane.style.width).toBe('58%');
        expect(screen.getByTestId('assistant-preview-resize-handle')).toBeTruthy();
    });

    it('presents as a right-edge slide-in overlay over a dimmed backdrop', async () => {
        renderPane();

        const pane = document.querySelector('.mc-assistant-preview-pane') as HTMLElement;
        expect(pane.style.position).toBe('absolute');
        expect(pane.style.right).toBe('0px');
        expect(pane.style.transition).toContain('transform');
        expect(screen.getByTestId('assistant-preview-backdrop')).toBeTruthy();
        // After the slide-in the transform is cleared so fixed-position context
        // menus inside the panel keep their viewport anchoring.
        await waitFor(() => expect(pane.style.transform).toBe('none'));
    });

    it('closes open preview surfaces when the backdrop is clicked', () => {
        const closeCodePreview = vi.fn();
        const closeDocPreview = vi.fn();
        render(
            <AssistantPreviewPane
                codePreviewState={activeCodePreviewState}
                closeCodePreview={closeCodePreview}
                closeDocPreview={closeDocPreview}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={true}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                workflowState={workflowState}
            />,
        );

        const backdrop = screen.getByTestId('assistant-preview-backdrop');
        fireEvent.mouseDown(backdrop);
        fireEvent.click(backdrop);

        expect(closeCodePreview).toHaveBeenCalled();
        expect(closeDocPreview).toHaveBeenCalled();
    });

    it('keeps the panel mounted while it slides out after the last preview closes', async () => {
        const props = {
            codePreviewState: activeCodePreviewState,
            closeCodePreview: vi.fn(),
            closeDocPreview: vi.fn(),
            lang: 'en',
            selectCodeFile: vi.fn(),
            showAgentView: false,
            showWorkflowPreview: false,
            splitRatio: 0.42,
            startPreviewResize: vi.fn(),
            theme,
            workflowState,
        };
        const { rerender } = render(<AssistantPreviewPane {...props} showCodePreview={true} />);
        expect(document.querySelector('.mc-assistant-preview-pane')).toBeTruthy();

        // Fully entered: the backdrop intercepts clicks to dismiss the preview.
        await waitFor(() => expect(screen.getByTestId('assistant-preview-backdrop').style.pointerEvents).toBe('auto'));

        rerender(<AssistantPreviewPane {...props} showCodePreview={false} />);

        // Still mounted right after the flag flips — the exit animation plays
        // first, and the now-invisible backdrop lets clicks fall through.
        expect(document.querySelector('.mc-assistant-preview-pane')).toBeTruthy();
        expect(screen.getByTestId('assistant-preview-backdrop').style.pointerEvents).toBe('none');
        await waitFor(() => expect(document.querySelector('.mc-assistant-preview-pane')).toBeNull());
    });
});
