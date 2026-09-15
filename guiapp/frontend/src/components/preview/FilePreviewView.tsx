/**
 * Content dispatcher for in-app file preview.
 *
 * Visual types (PPTX slides, PDF reader, images, audio/video, HTML, Word/Excel
 * as formatted markdown) render here. Callers pass `children` for source/diff.
 */
import React, { useEffect, useState } from 'react';
import { AIAssistantAttachmentFullDataURL, PreviewTaskResultFile } from '../../../wailsjs/go/main/App';
import { MarkdownPreview } from '../ai/CodePreviewMarkdown';
import type { CodePreviewTheme } from '../ai/FileTabBar';
import { PdfPreviewPanel } from '../ai/PdfPreviewPanel';
import { PptxPreviewPanel } from '../ai/PptxPreviewPanel';
import {
    isTaskResultPdfPreviewURL,
    localizeTaskResultPreviewError,
    type TaskResultPreviewPayload,
} from '../ai/taskResultPreview';
import {
    fileExtFromName,
    filePreviewKindFromName,
    isChromeLessPreviewKind,
    previewSourceName,
    type FilePreviewKind,
} from './filePreviewKind';

export type FilePreviewSource = {
    fileName?: string;
    filePath?: string;
    absPath?: string;
    content?: string;
    language?: string;
    dataUrl?: string;
};

export type FilePreviewViewProps = {
    file?: FilePreviewSource | null;
    theme: CodePreviewTheme;
    lang?: string;
    matchLineIndexes?: number[];
    activeMatchLine?: number;
    children?: React.ReactNode;
};

const EMPTY_MATCHES: number[] = [];

function previewName(file: FilePreviewSource | null | undefined): string {
    return previewSourceName(file?.absPath, file?.filePath, file?.fileName);
}

function isTokenizedPreviewURL(value: string): boolean {
    return isTaskResultPdfPreviewURL(value);
}

function isInlineImageSrc(value: string): boolean {
    if (value.startsWith('blob:') || isTokenizedPreviewURL(value)) return true;
    return value.startsWith('data:image/') && !/^data:image\/svg\+xml/i.test(value);
}

function isInlineMediaSrc(value: string): boolean {
    if (value.startsWith('blob:') || isTokenizedPreviewURL(value)) return true;
    return value.startsWith('data:video/') || value.startsWith('data:audio/');
}

function looksLikeHtmlDocument(value: string): boolean {
    const head = value.trim().slice(0, 512);
    if (!head) return false;
    return /<!doctype\s+html|<html[\s>]|<head[\s>]|<body[\s>]/i.test(head);
}

function useProvidedOrFetchedText(
    absPath: string | undefined,
    content: string | undefined,
    lang: string,
    usable: boolean,
) {
    const provided = String(content || '');
    const [text, setText] = useState(usable ? provided : '');
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(!usable && Boolean(String(absPath || '').trim()));

    useEffect(() => {
        if (usable) {
            setText(provided);
            setError('');
            setLoading(false);
            return;
        }
        const path = String(absPath || '').trim();
        if (!path) {
            setText(provided);
            setLoading(false);
            return;
        }
        let cancelled = false;
        setLoading(true);
        setError('');
        void (async () => {
            try {
                const result = await PreviewTaskResultFile(path) as TaskResultPreviewPayload;
                if (cancelled) return;
                setText(String(result?.content || provided));
                setLoading(false);
            } catch (err) {
                if (cancelled) return;
                if (provided.trim()) {
                    setText(provided);
                    setError('');
                    setLoading(false);
                    return;
                }
                setError(localizeTaskResultPreviewError(err instanceof Error ? err.message : String(err || ''), lang));
                setLoading(false);
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [absPath, lang, provided, usable]);

    return { text, error, loading };
}

function fillStyle(theme: CodePreviewTheme): React.CSSProperties {
    return {
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        minHeight: 0,
        background: theme.bg,
        color: theme.text,
        boxSizing: 'border-box',
    };
}

function PreviewStatus({
    testId,
    theme,
    children,
    danger,
}: {
    testId: string;
    theme: CodePreviewTheme;
    children: React.ReactNode;
    danger?: boolean;
}) {
    return (
        <div
            data-testid={testId}
            role="status"
            style={{
                ...fillStyle(theme),
                padding: 20,
                color: danger ? theme.diffDeleteText : theme.textMuted,
                fontSize: 13,
                lineHeight: 1.6,
            }}
        >
            {children}
        </div>
    );
}

function previewSrcFromPayload(
    result: TaskResultPreviewPayload | null | undefined,
    inline: (value: string) => boolean,
): string {
    const previewURL = String(result?.preview_url || '').trim();
    if (isTokenizedPreviewURL(previewURL)) return previewURL;
    const url = String(result?.data_url || '').trim();
    return inline(url) ? url : '';
}

function ImagePreviewPanel({
    absPath,
    dataUrl,
    theme,
    lang,
}: {
    absPath?: string;
    dataUrl?: string;
    theme: CodePreviewTheme;
    lang: string;
}) {
    const isZh = lang.startsWith('zh');
    const [src, setSrc] = useState('');
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        let cancelled = false;
        const fail = (message: string) => {
            if (cancelled) return;
            setError(localizeTaskResultPreviewError(message, lang) || message);
            setLoading(false);
        };
        const show = (next: string) => {
            if (cancelled || !next) return false;
            setSrc(next);
            setError('');
            setLoading(false);
            return true;
        };
        if (dataUrl && isInlineImageSrc(dataUrl)) {
            show(dataUrl);
            return () => {
                cancelled = true;
            };
        }
        setLoading(true);
        setSrc('');
        setError('');
        const path = String(absPath || '').trim();
        if (!path) {
            fail(isZh ? '没有可预览的图片' : 'No image to preview');
            return () => {
                cancelled = true;
            };
        }
        void (async () => {
            try {
                const result = await PreviewTaskResultFile(path) as TaskResultPreviewPayload;
                if (cancelled) return;
                if (show(previewSrcFromPayload(result, isInlineImageSrc))) return;
                const fallback = String(await AIAssistantAttachmentFullDataURL(path) || '');
                if (fallback && isInlineImageSrc(fallback) && show(fallback)) return;
                fail(isZh ? '无法预览该图片' : 'This image cannot be previewed');
            } catch (err) {
                fail(err instanceof Error ? err.message : String(err || ''));
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [absPath, dataUrl, isZh, lang]);

    if (error) return <PreviewStatus testId="image-preview-error" theme={theme} danger>{error}</PreviewStatus>;
    if (loading) {
        return (
            <PreviewStatus testId="image-preview-loading" theme={theme}>
                {isZh ? '正在加载图片预览…' : 'Loading image preview…'}
            </PreviewStatus>
        );
    }
    return (
        <div data-testid="image-preview-panel" style={{ ...fillStyle(theme), alignItems: 'center', justifyContent: 'center', overflow: 'auto', padding: 16 }}>
            <img
                src={src}
                alt=""
                style={{ maxWidth: '100%', maxHeight: '100%', objectFit: 'contain', borderRadius: 8 }}
            />
        </div>
    );
}

function MediaPreviewPanel({
    kind,
    absPath,
    theme,
    lang,
}: {
    kind: 'video' | 'audio';
    absPath?: string;
    theme: CodePreviewTheme;
    lang: string;
}) {
    const isZh = lang.startsWith('zh');
    const [src, setSrc] = useState('');
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        let cancelled = false;
        const fail = (message: string) => {
            if (cancelled) return;
            setError(localizeTaskResultPreviewError(message, lang) || message);
            setLoading(false);
        };
        setLoading(true);
        setSrc('');
        setError('');
        const path = String(absPath || '').trim();
        if (!path) {
            fail(isZh ? '没有可预览的媒体文件' : 'No media file to preview');
            return () => {
                cancelled = true;
            };
        }
        void (async () => {
            try {
                const result = await PreviewTaskResultFile(path) as TaskResultPreviewPayload;
                if (cancelled) return;
                const src = previewSrcFromPayload(result, isInlineMediaSrc);
                if (src) {
                    setSrc(src);
                    setLoading(false);
                    return;
                }
                fail(isZh ? '无法预览该媒体文件' : 'This media file cannot be previewed');
            } catch (err) {
                fail(err instanceof Error ? err.message : String(err || ''));
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [absPath, isZh, lang]);

    if (error) return <PreviewStatus testId="media-preview-error" theme={theme} danger>{error}</PreviewStatus>;
    if (loading) {
        return (
            <PreviewStatus testId="media-preview-loading" theme={theme}>
                {isZh ? '正在加载媒体预览…' : 'Loading media preview…'}
            </PreviewStatus>
        );
    }
    const mediaStyle: React.CSSProperties = { width: '100%', maxHeight: '100%', outline: 'none' };
    return (
        <div data-testid="media-preview-panel" data-kind={kind} style={{ ...fillStyle(theme), alignItems: 'center', justifyContent: 'center', padding: 16 }}>
            {kind === 'video' ? (
                <video controls src={src} style={mediaStyle} />
            ) : (
                <audio controls src={src} style={{ width: '100%' }} />
            )}
        </div>
    );
}

function HtmlPreviewPanel({
    absPath,
    content,
    theme,
    lang,
}: {
    absPath?: string;
    content?: string;
    theme: CodePreviewTheme;
    lang: string;
}) {
    const isZh = lang.startsWith('zh');
    const provided = String(content || '');
    const { text: markup, error, loading } = useProvidedOrFetchedText(
        absPath,
        content,
        lang,
        looksLikeHtmlDocument(provided),
    );

    if (error) return <PreviewStatus testId="html-preview-error" theme={theme} danger>{error}</PreviewStatus>;
    if (loading) {
        return (
            <PreviewStatus testId="html-preview-loading" theme={theme}>
                {isZh ? '正在加载 HTML 预览…' : 'Loading HTML preview…'}
            </PreviewStatus>
        );
    }
    if (!markup.trim()) {
        return (
            <PreviewStatus testId="html-preview-empty" theme={theme}>
                {isZh ? '该 HTML 文件没有可预览的内容' : 'This HTML file has no content to preview'}
            </PreviewStatus>
        );
    }
    return (
        <div data-testid="html-preview-frame" style={fillStyle(theme)}>
            <iframe
                data-testid="html-preview-panel"
                title={isZh ? 'HTML 预览' : 'HTML preview'}
                sandbox=""
                srcDoc={markup}
                style={{ flex: 1, minHeight: 0, width: '100%', height: '100%', border: 'none', background: theme.bg }}
            />
        </div>
    );
}

function OfficePreviewPanel({
    absPath,
    content,
    theme,
    lang,
    matchLineIndexes,
    activeMatchLine,
}: {
    absPath?: string;
    content?: string;
    theme: CodePreviewTheme;
    lang: string;
    matchLineIndexes?: number[];
    activeMatchLine?: number;
}) {
    const isZh = lang.startsWith('zh');
    const { text, error, loading } = useProvidedOrFetchedText(
        absPath,
        content,
        lang,
        Boolean(String(content || '').trim()),
    );

    if (error) return <PreviewStatus testId="office-preview-error" theme={theme} danger>{error}</PreviewStatus>;
    if (loading) {
        return (
            <PreviewStatus testId="office-preview-loading" theme={theme}>
                {isZh ? '正在加载文档预览…' : 'Loading document preview…'}
            </PreviewStatus>
        );
    }
    if (!text.trim()) {
        return (
            <PreviewStatus testId="office-preview-empty" theme={theme}>
                {isZh ? '该文档没有可预览的内容' : 'This document has no content to preview'}
            </PreviewStatus>
        );
    }
    return (
        <div data-testid="office-preview-panel">
            <MarkdownPreview
                content={text}
                theme={theme}
                matchLineIndexes={matchLineIndexes}
                activeMatchLine={activeMatchLine}
            />
        </div>
    );
}

export function filePreviewKindOf(file?: FilePreviewSource | null): FilePreviewKind {
    if (!file) return 'text';
    return filePreviewKindFromName(previewName(file), file.language);
}

export function isVisualFilePreview(file?: FilePreviewSource | null): boolean {
    if (!file) return false;
    const kind = filePreviewKindOf(file);
    if (kind === 'pptx') return Boolean(file.absPath);
    return isChromeLessPreviewKind(kind);
}

/** HTML / SVG stay source files in the assistant; the library still uses a visual host. */
export function isAssistantSourcePreview(file?: FilePreviewSource | null): boolean {
    if (!file) return false;
    if (filePreviewKindOf(file) === 'html') return true;
    return fileExtFromName(previewName(file)) === '.svg';
}

/**
 * True when FilePreviewView should own the body in the assistant.
 * HTML/SVG stay false so CodePreviewPanel can pass source / diff children.
 */
export function filePreviewUsesSpecialRenderer(file?: FilePreviewSource | null): boolean {
    if (!file || isAssistantSourcePreview(file)) return false;
    const kind = filePreviewKindOf(file);
    if (kind === 'pptx') return Boolean(file.absPath);
    return kind === 'pdf' || kind === 'image' || kind === 'video' || kind === 'audio'
        || kind === 'office' || kind === 'markdown';
}

export function FilePreviewView({
    file,
    theme,
    lang = 'en',
    matchLineIndexes = EMPTY_MATCHES,
    activeMatchLine = -1,
    children,
}: FilePreviewViewProps) {
    if (!file) return <>{children}</>;
    const name = previewName(file);
    const kind = filePreviewKindOf(file);
    const diskPath = String(file.absPath || '').trim() || String(file.filePath || '').trim();
    const absPath = kind === 'pptx' ? String(file.absPath || '').trim() : diskPath;
    const fileKey = `${name}:${absPath}:${file.language || ''}`;
    // Assistant source/diff wins for HTML, SVG, Markdown, and Office extracts.
    // Raster images, PDF, PPTX, and media keep their visual viewers.
    const yieldToParent = Boolean(children) && (
        kind === 'html' || kind === 'office' || kind === 'markdown'
        || (kind === 'image' && isAssistantSourcePreview(file))
    );
    if (yieldToParent) return <>{children}</>;

    if (kind === 'pptx' && file.absPath) {
        return <PptxPreviewPanel key={fileKey} absPath={file.absPath} theme={theme} lang={lang} />;
    }
    if (kind === 'pdf') {
        return (
            <PdfPreviewPanel
                key={fileKey}
                absPath={absPath || name}
                dataUrl={file.dataUrl}
                theme={theme}
                lang={lang}
            />
        );
    }
    if (kind === 'image') {
        return <ImagePreviewPanel key={fileKey} absPath={absPath} dataUrl={file.dataUrl} theme={theme} lang={lang} />;
    }
    if (kind === 'video' || kind === 'audio') {
        return <MediaPreviewPanel key={fileKey} kind={kind} absPath={absPath} theme={theme} lang={lang} />;
    }
    if (kind === 'html') {
        return <HtmlPreviewPanel key={fileKey} absPath={absPath} content={file.content} theme={theme} lang={lang} />;
    }
    if (kind === 'office') {
        return (
            <OfficePreviewPanel
                key={fileKey}
                absPath={absPath}
                content={file.content}
                theme={theme}
                lang={lang}
                matchLineIndexes={matchLineIndexes}
                activeMatchLine={activeMatchLine}
            />
        );
    }
    if (kind === 'markdown') {
        return (
            <MarkdownPreview
                content={file.content || ''}
                theme={theme}
                matchLineIndexes={matchLineIndexes}
                activeMatchLine={activeMatchLine}
            />
        );
    }
    return <>{children}</>;
}
