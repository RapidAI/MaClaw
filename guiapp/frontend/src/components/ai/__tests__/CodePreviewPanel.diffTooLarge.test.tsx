import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { CodePreviewPanel, lightCodePreviewTheme } from '../CodePreviewPanel';
import type { CodeFile } from '../useCodePreviewState';

/**
 * computeDiff refuses files above its size guard, which drops the preview back
 * to the plain view. The panel must still report the change totals.
 */
describe('CodePreviewPanel — oversized diff fallback', () => {
    it('shows change totals instead of a line diff for very large files', () => {
        const oldLines: string[] = [];
        const newLines: string[] = [];
        for (let i = 0; i < 2600; i++) {
            oldLines.push(`const value${i} = ${i};`);
            newLines.push(`const value${i} = ${i === 42 ? 4242 : i};`);
        }
        const file: CodeFile = {
            sessionID: 's1',
            filePath: 'src/huge.ts',
            fileName: 'huge.ts',
            content: newLines.join('\n'),
            original: oldLines.join('\n'),
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
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
            />,
        );

        const notice = screen.getByTestId('code-preview-diff-too-large');
        expect(notice.textContent).toContain('变更统计');
        expect(notice.textContent).toContain('+1');
        expect(screen.queryByTestId('code-preview-diff-view')).toBeNull();
    });
});