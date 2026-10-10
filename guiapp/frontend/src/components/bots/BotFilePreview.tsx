import { useCallback, useEffect, useRef, useState, type CSSProperties } from 'react';
import { darkCodePreviewTheme, lightCodePreviewTheme } from '../ai/CodePreviewPanel';
import type { CodePreviewTheme } from '../ai/FileTabBar';
import { usePreviewSlideLifecycle } from '../ai/previewSlide';
import {
    codeFileForImmediateTaskResultPreview,
    codeFileFromTaskResultPreview,
    localizeTaskResultPreviewError,
    PREVIEW_TASK_RESULT_EVENT,
    previewTaskResultPathFromEvent,
    taskResultPreviewKindFromPath,
    type TaskResultPreviewPayload,
} from '../ai/taskResultPreview';
import type { CodeFile } from '../ai/useCodePreviewState';
import { FilePreviewHost } from '../preview/FilePreviewHost';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { getWailsAppModule } from '../../utils/wailsAppModule';

// The result card's 预览 button only dispatches a window event. The assistant
// pane listens for it, but that pane is hidden while this page is open, so the
// bot keeps its own slide-out and the same file viewer.

type ShownFile = {
    fileName: string;
    absPath: string;
    content: string;
    language: string;
};

function shownFile(file: CodeFile): ShownFile {
    return {
        fileName: file.fileName || file.filePath.split(/[/\\]/).pop() || file.filePath,
        absPath: String(file.absPath || file.filePath || '').trim(),
        content: file.content || '',
        language: file.language || 'plaintext',
    };
}

function previewTheme(): CodePreviewTheme {
    if (typeof document === 'undefined') return lightCodePreviewTheme;
    const mode = document.querySelector('[data-ai-theme]')?.getAttribute('data-ai-theme');
    return mode === 'dark' ? darkCodePreviewTheme : lightCodePreviewTheme;
}

function closeLabel(lang: string): string {
    if (lang === 'en') return 'Close preview';
    if (lang === 'zh-Hant') return '關閉預覽';
    return '关闭预览';
}

function paneLabel(lang: string): string {
    if (lang === 'en') return 'File preview';
    if (lang === 'zh-Hant') return '檔案預覽';
    return '文件预览';
}

export function BotFilePreview({ lang, botId }: { lang: string; botId: string }) {
    const [open, setOpen] = useState(false);
    const [file, setFile] = useState<ShownFile | null>(null);
    // Monotonic ticket. Close, a bot switch, and a newer click all advance it,
    // so a file read that returns late cannot reopen a pane the user left.
    const requestRef = useRef(0);
    const dismiss = useCallback(() => {
        requestRef.current += 1;
        setOpen(false);
    }, []);
    const { present, entered, closing, settled, slideMs } = usePreviewSlideLifecycle(open, () => setFile(null));
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(dismiss);
    const botRef = useRef(botId);

    useEffect(() => {
        if (botRef.current === botId) return;
        botRef.current = botId;
        requestRef.current += 1;
        setOpen(false);
    }, [botId]);

    useEffect(() => {
        if (!open) return;
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== 'Escape') return;
            event.preventDefault();
            dismiss();
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [dismiss, open]);

    useEffect(() => {
        const show = (next: CodeFile, ticket: number) => {
            if (ticket !== requestRef.current) return;
            setFile(shownFile(next));
            setOpen(true);
        };
        const onPreview = (event: Event) => {
            const path = previewTaskResultPathFromEvent(event);
            if (!path) return;
            const ticket = ++requestRef.current;
            const immediate = taskResultPreviewKindFromPath(path);
            if (immediate) {
                show(codeFileForImmediateTaskResultPreview(path, immediate), ticket);
                return;
            }
            const name = path.split(/[/\\]/).pop() || path;
            setFile({ fileName: name, absPath: path, content: '', language: 'plaintext' });
            setOpen(true);
            void (async () => {
                try {
                    const { PreviewTaskResultFile } = await getWailsAppModule();
                    if (typeof PreviewTaskResultFile !== 'function') throw new Error('PreviewTaskResultFile unavailable');
                    const preview = await PreviewTaskResultFile(path) as TaskResultPreviewPayload;
                    show(codeFileFromTaskResultPreview(preview || { path }, path), ticket);
                } catch (err) {
                    const message = err instanceof Error ? err.message : String(err || '');
                    show({
                        filePath: path,
                        fileName: name,
                        absPath: path,
                        content: localizeTaskResultPreviewError(message, lang),
                        language: 'plaintext',
                        opType: 'read',
                        updatedAt: Date.now(),
                    }, ticket);
                }
            })();
        };
        window.addEventListener(PREVIEW_TASK_RESULT_EVENT, onPreview);
        return () => {
            requestRef.current += 1;
            window.removeEventListener(PREVIEW_TASK_RESULT_EVENT, onPreview);
        };
    }, [lang]);

    if (!present || !file) return null;

    const paneStyle: CSSProperties = {
        position: 'absolute',
        top: 0,
        right: 0,
        bottom: 0,
        width: 'min(100%, max(360px, 40%))',
        height: '100%',
        display: 'flex',
        flexDirection: 'row',
        padding: 0,
        background: 'transparent',
        zIndex: 40,
        transform: settled ? 'none' : (entered && !closing ? 'translateX(0)' : 'translateX(105%)'),
        transition: settled ? 'none' : `transform ${slideMs}ms ease`,
        ['--mc-preview-pane-bg' as string]: 'var(--theme-page-bg, #f5f5f5)',
        ['--mc-preview-surface-bg' as string]: 'var(--theme-surface, #fff)',
        ['--mc-preview-surface-border' as string]: 'var(--theme-divider, #e4e4e4)',
        ['--mc-preview-surface-border-active' as string]: 'var(--theme-border, #c8c8c8)',
        ['--mc-preview-surface-shadow' as string]: '0 1px 2px rgba(0, 0, 0, 0.04), 0 12px 28px -14px rgba(0, 0, 0, 0.10)',
    };

    return (
        <>
            <div
                role="presentation"
                data-testid="bot-file-preview-backdrop"
                style={{
                    position: 'absolute',
                    inset: 0,
                    zIndex: 39,
                    background: 'rgba(0, 0, 0, 0.45)',
                    opacity: entered && !closing ? 1 : 0,
                    transition: `opacity ${slideMs}ms ease`,
                    pointerEvents: entered && !closing ? 'auto' : 'none',
                }}
                {...backdropProps}
            />
            <div
                className="mc-assistant-preview-pane"
                data-testid="bot-file-preview"
                data-preview-mode="code"
                role="dialog"
                aria-label={paneLabel(lang)}
                style={paneStyle}
                {...dialogProps}
            >
                <div className="mc-assistant-preview-surface" style={{ borderRadius: '12px 0 0 12px', borderRight: 'none' }}>
                    <div className="mc-assistant-preview-content" style={{ display: 'flex', flexDirection: 'column' }}>
                        <div className="bot-file-preview__title" data-testid="bot-file-preview-title" title={file.absPath}>{file.fileName}</div>
                        <div style={{ flex: 1, minHeight: 0, minWidth: 0, display: 'flex' }}>
                            <FilePreviewHost
                                key={`${file.absPath}:${file.language}`}
                                fileName={file.fileName}
                                absPath={file.absPath}
                                content={file.content}
                                language={file.language}
                                theme={previewTheme()}
                                lang={lang}
                            />
                        </div>
                    </div>
                    <div
                        className="mc-assistant-preview-mode-tabs mc-assistant-preview-mode-tabs--single"
                        style={{
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                            padding: '8px 4px',
                            borderLeft: '1px solid var(--mc-preview-surface-border, #e4e4e4)',
                            background: 'var(--mc-preview-surface-bg, #fff)',
                            flexShrink: 0,
                            width: 32,
                        }}
                    >
                        <button
                            type="button"
                            data-testid="bot-file-preview-close"
                            aria-label={closeLabel(lang)}
                            title={closeLabel(lang)}
                            onClick={dismiss}
                            style={{
                                width: 26,
                                height: 26,
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                background: 'none',
                                border: 'none',
                                cursor: 'pointer',
                                fontSize: 14,
                                padding: 0,
                                borderRadius: 8,
                                color: 'var(--theme-text-muted, #64748b)',
                                lineHeight: 1,
                            }}
                        >
                            X
                        </button>
                    </div>
                </div>
            </div>
        </>
    );
}
