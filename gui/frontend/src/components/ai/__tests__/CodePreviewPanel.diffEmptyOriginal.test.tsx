import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { CodePreviewPanel, darkCodePreviewTheme } from '../CodePreviewPanel';
import type { CodeFile } from '../useCodePreviewState';

describe('CodePreviewPanel diff rendering', () => {
    it('renders a modify diff when original content is intentionally empty', () => {
        const file: CodeFile = {
            sessionID: 'session-1',
            filePath: 'src/new-from-empty.ts',
            fileName: 'new-from-empty.ts',
            content: 'export const value = true;',
            original: '',
            opType: 'modify',
            language: 'typescript',
            updatedAt: 1,
        };

        render(
            <CodePreviewPanel
                files={new Map([[file.filePath, file]])}
                activeFilePath={file.filePath}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={darkCodePreviewTheme}
            />,
        );

        expect(screen.getByText('+')).toBeTruthy();
        expect(screen.getByText('export const value = true;')).toBeTruthy();
    });

    it('renders markdown modifications as diff instead of markdown preview', () => {
        const file: CodeFile = {
            sessionID: 'session-1',
            filePath: 'README.md',
            fileName: 'README.md',
            content: '# New title',
            original: '# Old title',
            opType: 'modify',
            language: 'markdown',
            updatedAt: 1,
        };

        render(
            <CodePreviewPanel
                files={new Map([[file.filePath, file]])}
                activeFilePath={file.filePath}
                onSelectFile={vi.fn()}
                onClose={vi.fn()}
                theme={darkCodePreviewTheme}
            />,
        );

        expect(screen.getByText('-')).toBeTruthy();
        expect(screen.getByText('+')).toBeTruthy();
        expect(screen.getByText('# Old title')).toBeTruthy();
        expect(screen.getByText('# New title')).toBeTruthy();
    });
});
