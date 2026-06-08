import { fireEvent, render, screen } from '@testing-library/react';
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
            inline={false}
            lang="en"
            selectCodeFile={vi.fn()}
            showAgentView={false}
            showCodePreview={true}
            showWorkflowPreview={true}
            splitRatio={0.42}
            startPreviewResize={vi.fn()}
            theme={theme}
            themeMode="light"
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
            inline={false}
            lang="en"
            selectCodeFile={vi.fn()}
            showAgentView={false}
            showCodePreview={codePreviewState.active}
            showWorkflowPreview={true}
            splitRatio={0.42}
            startPreviewResize={vi.fn()}
            theme={theme}
            themeMode="light"
            workflowState={workflowState}
        />,
    );
}

describe('AssistantPreviewPane', () => {
    it('keeps workflow progress and source preview available behind tabs', () => {
        renderPane();

        expect(screen.getByTestId('assistant-preview-mode-tabs').style.getPropertyValue('--wails-draggable')).toBe('drag');
        expect(screen.getByRole('tab', { name: 'Progress' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('tab', { name: 'Progress' }).style.getPropertyValue('--wails-draggable')).toBe('no-drag');
        expect(screen.getByRole('tab', { name: 'Source' })).toBeTruthy();
        expect(screen.getByRole('tab', { name: 'Progress' }).getAttribute('aria-controls')).toBe('assistant-preview-panel-workflow');
        expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe('assistant-preview-tab-workflow');
        expect(screen.getAllByText('Requirements').length).toBeGreaterThan(0);

        fireEvent.click(screen.getByRole('tab', { name: 'Source' }));

        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('tabindex')).toBe('0');
        expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe('assistant-preview-tab-code');
        expect(screen.getByTestId('code-preview-header').style.getPropertyValue('--wails-draggable')).toBe('drag');
        expect(screen.getByText('const')).toBeTruthy();
        expect(screen.getByText('answer')).toBeTruthy();
    });

    it('keeps the empty source preview header draggable', () => {
        renderPaneWithCodeState(activeEmptyCodePreviewState);

        fireEvent.click(screen.getByRole('tab', { name: 'Source' }));

        expect(screen.getByTestId('code-preview-header').style.getPropertyValue('--wails-draggable')).toBe('drag');
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
                inline={false}
                lang="en"
                selectCodeFile={vi.fn()}
                showAgentView={false}
                showCodePreview={true}
                showWorkflowPreview={true}
                splitRatio={0.42}
                startPreviewResize={vi.fn()}
                theme={theme}
                themeMode="light"
                workflowState={workflowState}
            />,
        );

        expect(screen.getByRole('tab', { name: 'Source' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getByText('answer')).toBeTruthy();

        fireEvent.click(screen.getByRole('tab', { name: 'Progress' }));
        expect(screen.getByRole('tab', { name: 'Progress' }).getAttribute('aria-selected')).toBe('true');
        expect(screen.getAllByText('Requirements').length).toBeGreaterThan(0);
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
});
