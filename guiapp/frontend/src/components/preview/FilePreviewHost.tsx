/**
 * Drop-in file preview for any surface (assistant, mobile library, …).
 *
 * Visual documents (PPTX / PDF / image / media) render through FilePreviewView.
 * Source and markdown keep the assistant source-preview chrome (find, go-to-line,
 * minimap) via CodePreviewPanel in embedded mode.
 */
import { useMemo } from 'react';
import { CodePreviewPanel } from '../ai/CodePreviewPanel';
import type { CodePreviewTheme } from '../ai/FileTabBar';
import type { CodeFile } from '../ai/useCodePreviewState';
import { FilePreviewView, isVisualFilePreview } from './FilePreviewView';
import { languageFromFileName } from './filePreviewKind';

export type FilePreviewHostProps = {
    fileName: string;
    absPath?: string;
    content?: string;
    language?: string;
    dataUrl?: string;
    theme: CodePreviewTheme;
    lang?: string;
};

function toCodeFile(props: Pick<FilePreviewHostProps, 'fileName' | 'absPath' | 'content' | 'language'>): CodeFile {
    const fileName = String(props.fileName || '').trim() || 'document';
    const absPath = String(props.absPath || '').trim();
    const filePath = absPath || fileName;
    return {
        filePath,
        fileName,
        absPath: absPath || undefined,
        content: props.content || '',
        language: languageFromFileName(absPath || fileName, props.language),
        opType: 'read',
        updatedAt: Date.now(),
    };
}

export function FilePreviewHost({
    fileName,
    absPath,
    content,
    language,
    dataUrl,
    theme,
    lang = 'en',
}: FilePreviewHostProps) {
    const file = useMemo(
        () => ({ ...toCodeFile({ fileName, absPath, content, language }), dataUrl }),
        [absPath, content, dataUrl, fileName, language],
    );
    const files = useMemo(() => {
        const next = new Map<string, CodeFile>();
        next.set(file.filePath, file);
        return next;
    }, [file]);

    if (isVisualFilePreview(file)) {
        return (
            <div data-testid="file-preview-host" style={{ height: '100%', minHeight: 0, minWidth: 0, flex: 1, display: 'flex', flexDirection: 'column' }}>
                <FilePreviewView file={file} theme={theme} lang={lang} />
            </div>
        );
    }

    return (
        <div data-testid="file-preview-host" style={{ height: '100%', minHeight: 0, minWidth: 0, flex: 1 }}>
            <CodePreviewPanel
                files={files}
                activeFilePath={file.filePath}
                onSelectFile={() => undefined}
                onClose={() => undefined}
                hideHeaderClose
                embedded
                theme={theme}
                lang={lang}
            />
        </div>
    );
}
