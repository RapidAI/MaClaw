import { useEffect, useRef, useState, type CSSProperties } from "react";
import { PreviewTaskResultFile } from "../../../wailsjs/go/main/App";
import type { CodePreviewTheme } from "./FileTabBar";
import { isTaskResultPdfPreviewURL, localizeTaskResultPreviewError, type TaskResultPreviewPayload } from "./taskResultPreview";

/** .docx is the OOXML word processor file the in-app renderer can lay out. */
export function isDocxFileName(name: string): boolean {
    return name.toLowerCase().endsWith(".docx");
}

export interface DocxPreviewPanelProps {
    absPath: string;
    theme: CodePreviewTheme;
    lang: string;
}

/**
 * In-app Word preview. The bytes are the original .docx; docx-preview lays
 * out pages, styles, tables, and images. The text extract used to drop all
 * of that and show a line-numbered dump.
 */
export function DocxPreviewPanel({ absPath, theme, lang }: DocxPreviewPanelProps) {
    const isZh = lang.startsWith("zh");
    const langRef = useRef(lang);
    useEffect(() => {
        langRef.current = lang;
    }, [lang]);
    const hostRef = useRef<HTMLDivElement>(null);
    const scrollRef = useRef<HTMLDivElement>(null);
    const [error, setError] = useState("");
    const [loading, setLoading] = useState(true);

    useEffect(() => {
        let cancelled = false;
        const zh = () => langRef.current.startsWith("zh");
        const fail = (message: string) => {
            if (cancelled) return;
            setError(localizeTaskResultPreviewError(message, langRef.current));
            setLoading(false);
        };
        setLoading(true);
        setError("");
        const host = hostRef.current;
        if (host) host.replaceChildren();
        void (async () => {
            try {
                const result = await PreviewTaskResultFile(absPath) as TaskResultPreviewPayload;
                if (cancelled) return;
                const previewURL = String(result?.preview_url || "").trim();
                if (!isDocxPreviewURL(previewURL)) {
                    fail(zh() ? "无法预览该 Word 文档" : "This Word document cannot be previewed");
                    return;
                }
                const response = await fetch(previewURL);
                if (!response.ok) {
                    fail(zh() ? "无法读取该 Word 文档" : "This Word document could not be read");
                    return;
                }
                const bytes = await response.arrayBuffer();
                const target = hostRef.current;
                if (cancelled || !target) return;
                const { renderAsync } = await import("docx-preview");
                if (cancelled || hostRef.current !== target) return;
                const styleHost = document.createElement("div");
                const bodyHost = document.createElement("div");
                target.replaceChildren(styleHost, bodyHost);
                await renderAsync(bytes, bodyHost, styleHost, {
                    className: "maclaw-docx",
                    inWrapper: true,
                    ignoreWidth: false,
                    ignoreHeight: false,
                    breakPages: true,
                    ignoreLastRenderedPageBreak: false,
                    useBase64URL: true,
                });
                if (cancelled || hostRef.current !== target) return;
                if (!target.querySelector("section")) {
                    fail(zh() ? "该 Word 文档没有可预览的页面" : "This Word document has no pages to preview");
                    return;
                }
                fitDocxToPane(scrollRef.current, bodyHost);
                setLoading(false);
            } catch (err) {
                fail(err instanceof Error ? err.message : String(err || ""));
            }
        })();
        const scroll = scrollRef.current;
        const observer = scroll && typeof ResizeObserver !== "undefined"
            ? new ResizeObserver(() => {
                const body = hostRef.current?.querySelector(":scope > div:last-child");
                if (body instanceof HTMLElement) fitDocxToPane(scroll, body);
            })
            : null;
        observer?.observe(scroll!);
        return () => {
            cancelled = true;
            observer?.disconnect();
        };
    }, [absPath]);

    const fill: CSSProperties = {
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
        background: theme.lineNumBg,
    };
    if (error) {
        return (
            <div data-testid="docx-preview-error" style={{ ...fill, padding: 20, color: theme.diffDeleteText, fontSize: 13, lineHeight: 1.6, boxSizing: "border-box" }}>
                {error}
            </div>
        );
    }
    return (
        <div
            ref={scrollRef}
            data-testid="docx-preview-panel"
            style={{ ...fill, width: "100%", minWidth: 0, overflow: "auto" }}
        >
            {loading ? (
                <div data-testid="docx-preview-loading" role="status" style={{ padding: 20, color: theme.textMuted, fontSize: 13 }}>
                    {isZh ? "正在加载 Word 预览…" : "Loading Word preview…"}
                </div>
            ) : null}
            <div ref={hostRef} />
        </div>
    );
}

/**
 * The renderer centers each page (`align-items: center`). In a pane narrower
 * than the page, the extra width spills equally to both sides and the left
 * overflow cannot be scrolled back into view. Pin the pages to the left edge,
 * then scale the whole sheet — padding included — into the pane.
 */
export function fitDocxToPane(scroll: HTMLElement | null, body: HTMLElement) {
    if (!scroll) return;
    const wrapper = body.querySelector(".maclaw-docx-wrapper");
    if (wrapper instanceof HTMLElement) {
        wrapper.style.alignItems = "flex-start";
        wrapper.style.width = "fit-content";
        wrapper.style.maxWidth = "none";
    }
    const page = body.querySelector("section");
    const box = wrapper instanceof HTMLElement ? wrapper : page;
    if (!(box instanceof HTMLElement)) {
        body.style.removeProperty("zoom");
        return;
    }
    const natural = docxNaturalWidth(box, body);
    const avail = scroll.clientWidth;
    if (natural <= 0 || avail <= 0) {
        body.style.removeProperty("zoom");
        return;
    }
    const scale = Math.min(1, Math.max(0.2, (avail - 8) / natural));
    if (scale < 0.995) body.style.setProperty("zoom", String(scale));
    else body.style.removeProperty("zoom");
    scroll.scrollLeft = 0;
    scroll.scrollTop = 0;
}

function docxNaturalWidth(box: HTMLElement, zoomed: HTMLElement): number {
    const stored = Number(box.dataset.docxNaturalWidth || "");
    if (stored > 0) return stored;
    const zoom = parseFloat(zoomed.style.getPropertyValue("zoom")) || 1;
    const width = (box.scrollWidth || box.offsetWidth) / zoom;
    if (width > 0) box.dataset.docxNaturalWidth = String(width);
    return width;
}

function isDocxPreviewURL(value: string): boolean {
    // Same short-lived file lease as PDF, served with the Word content type.
    return isTaskResultPdfPreviewURL(value);
}
