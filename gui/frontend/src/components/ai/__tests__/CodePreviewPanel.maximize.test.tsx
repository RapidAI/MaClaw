/**
 * Code preview header double-click maximize + dirty path badge.
 */
import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { CodePreviewPanel, lightCodePreviewTheme } from '../CodePreviewPanel';
import type { CodeFile } from '../useCodePreviewState';

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
        expect(screen.getByTestId('code-preview-dirty-badge').textContent).toMatch(/changed/i);
    });
});
