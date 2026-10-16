import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { CodePreviewFileListButton } from '../CodePreviewFileList';
import { lightCodePreviewTheme } from '../CodePreviewPanel';
import type { CodeFile } from '../useCodePreviewState';

function file(partial: Partial<CodeFile> & Pick<CodeFile, 'filePath' | 'content' | 'opType'>): CodeFile {
    return {
        fileName: partial.filePath.split('/').pop() || partial.filePath,
        language: 'typescript',
        updatedAt: 1,
        ...partial,
    };
}

const files = new Map<string, CodeFile>([
    ['/src/new.ts', file({ filePath: '/src/new.ts', content: 'a\nb\nc\n', opType: 'create' })],
    ['/src/edit.ts', file({
        filePath: '/src/edit.ts',
        content: 'keep\nnew\nkeep\nextra\n',
        original: 'keep\nold\nkeep\n',
        opType: 'modify',
    })],
    ['/src/read.ts', file({ filePath: '/src/read.ts', content: 'x\ny\n', original: 'x\ny\n', opType: 'read' })],
]);

describe('CodePreviewFileList — change overview', () => {
    it('marks each file with NEW / MOD plus per-file line counts', () => {
        render(
            <CodePreviewFileListButton
                files={files}
                pinnedPaths={[]}
                activeFilePath="/src/edit.ts"
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
                onSelectFile={vi.fn()}
            />,
        );
        fireEvent.click(screen.getByTestId('code-preview-file-list-toggle'));

        const badges = screen.getAllByTestId('code-preview-file-list-change');
        // read-only files carry no badge.
        expect(badges).toHaveLength(2);
        expect(badges.map((el) => el.getAttribute('data-change-kind'))).toEqual(['add', 'modify']);
        expect(badges[0].textContent).toContain('NEW');
        expect(badges[0].textContent).toContain('+3');
        expect(badges[1].textContent).toContain('MOD');
        expect(badges[1].textContent).toContain('+2');
        expect(badges[1].textContent).toContain('-1');
    });

    it('summarizes the session totals in the panel head', () => {
        render(
            <CodePreviewFileListButton
                files={files}
                pinnedPaths={[]}
                activeFilePath="/src/edit.ts"
                theme={lightCodePreviewTheme}
                lang="zh-Hans"
                onSelectFile={vi.fn()}
            />,
        );
        fireEvent.click(screen.getByTestId('code-preview-file-list-toggle'));
        const totals = screen.getByTestId('code-preview-file-list-totals');
        expect(totals.textContent).toContain('+5');
        expect(totals.textContent).toContain('-1');
    });
});