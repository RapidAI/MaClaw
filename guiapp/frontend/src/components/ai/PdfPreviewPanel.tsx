import React, { useEffect, useRef, useState } from "react";
import { PreviewTaskResultFile } from "../../../wailsjs/go/main/App";
import type { CodePreviewTheme } from "./FileTabBar";
import {
    isPdfFileName,
    isPdfInlineDataURL,
    isTaskResultPdfPreviewURL,
    localizeTaskResultPreviewError,
    objectURLFromPdfDataURL,
    type TaskResultPreviewPayload,
} from "./taskResultPreview";

export { isPdfFileName };

export interface PdfPreviewPanelProps {
    absPath: string;
    /** When the host already fetched a PDF data URL, skip a second backend call. */
    dataUrl?: string;
    theme: CodePreviewTheme;
    lang: string;
}

/**
 * In-app PDF viewer. Production PDFs are served from a short-lived AssetServer
 * URL. Tests and fallbacks may still pass a data URL, which is converted to a
 * blob URL for the iframe.
 */
export function PdfPreviewPanel({ absPath, dataUrl, theme, lang }: PdfPreviewPanelProps) {
    const isZh = lang.startsWith("zh");
    const langRef = useRef(lang);
    useEffect(() => {
        langRef.current = lang;
    }, [lang]);
    const [src, setSrc] = useState("");
    const [text, setText] = useState("");
    const [error, setError] = useState("");
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        let cancelled = false;
        let objectUrl = "";
        const emptyLabel = () => langRef.current.startsWith("zh") ? "该 PDF 没有可预览的内容" : "This PDF has no content to preview";
        const fail = (message: string) => {
            if (cancelled) return;
            setError(localizeTaskResultPreviewError(message, langRef.current));
            setLoading(false);
        };
        const showSrc = (next: string) => {
            if (cancelled) return false;
            setSrc(next);
            setText("");
            setError("");
            setLoading(false);
            return true;
        };
        const applyDataURL = (url: string) => {
            const next = objectURLFromPdfDataURL(url) || url;
            if (cancelled) {
                if (next.startsWith("blob:")) URL.revokeObjectURL(next);
                return false;
            }
            if (next.startsWith("blob:")) objectUrl = next;
            return showSrc(next);
        };

        if (dataUrl && isPdfInlineDataURL(dataUrl)) {
            applyDataURL(dataUrl);
            return () => {
                cancelled = true;
                if (objectUrl) URL.revokeObjectURL(objectUrl);
            };
        }

        setLoading(true);
        setError("");
        setText("");
        setSrc("");
        void (async () => {
            try {
                const result = await PreviewTaskResultFile(absPath) as TaskResultPreviewPayload;
                if (cancelled) return;
                const previewURL = String(result?.preview_url || "").trim();
                if (isTaskResultPdfPreviewURL(previewURL) && showSrc(previewURL)) return;
                const url = String(result?.data_url || "");
                if (isPdfInlineDataURL(url) && applyDataURL(url)) return;
                const body = String(result?.content || "");
                if (body) {
                    setText(body);
                    setError("");
                    setLoading(false);
                    return;
                }
                fail(emptyLabel());
            } catch (err) {
                fail(err instanceof Error ? err.message : String(err || ""));
            } finally {
                if (!cancelled) setLoading(false);
            }
        })();
        return () => {
            cancelled = true;
            if (objectUrl) URL.revokeObjectURL(objectUrl);
        };
    }, [absPath, dataUrl]);

    const fill: React.CSSProperties = { display: "flex", flexDirection: "column", height: "100%", minHeight: 0, background: theme.bg };
    if (error) {
        return (
            <div data-testid="pdf-preview-error" style={{ ...fill, padding: 20, color: theme.diffDeleteText, fontSize: 13, lineHeight: 1.6, boxSizing: "border-box" }}>
                {error}
            </div>
        );
    }
    if (loading) {
        return (
            <div data-testid="pdf-preview-loading" role="status" style={{ ...fill, padding: 20, color: theme.textMuted, fontSize: 13, boxSizing: "border-box" }}>
                {isZh ? "正在加载 PDF 预览…" : "Loading PDF preview…"}
            </div>
        );
    }
    if (src) {
        return (
            <div style={fill}>
                <iframe
                    data-testid="pdf-preview-panel"
                    title={isZh ? "PDF 预览" : "PDF preview"}
                    src={src}
                    style={{ flex: 1, minHeight: 0, width: "100%", border: "none", background: theme.bg }}
                />
            </div>
        );
    }
    return (
        <pre
            data-testid="pdf-preview-text"
            style={{
                ...fill,
                margin: 0,
                padding: 16,
                boxSizing: "border-box",
                overflow: "auto",
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
                color: theme.text,
                fontSize: 13,
                lineHeight: 1.55,
                fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
            }}
        >
            {text}
        </pre>
    );
}
