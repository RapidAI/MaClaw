import { useEffect, useRef, useState, type CSSProperties } from "react";
import { PreviewTaskResultFile } from "../../../wailsjs/go/main/App";
import type { CodePreviewTheme } from "./FileTabBar";
import { isTaskResultPdfPreviewURL, localizeTaskResultPreviewError, type TaskResultPreviewPayload } from "./taskResultPreview";
import { isPreviewAbortError, readPreviewBody } from "./previewBytes";
import { PREVIEW_BAR_DELAY_MS, PREVIEW_HEAVY_PARSE_BYTES, PreviewLoadingStatus, yieldPreviewPaint, type PreviewProgress } from "./PreviewProgressBar";
import { tailText } from "../../i18n/mnTail"

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
    const [progress, setProgress] = useState<PreviewProgress>({ phase: "wait", ratio: null });
    const [barOn, setBarOn] = useState(false);

    useEffect(() => {
        let cancelled = false;
        const controller = new AbortController();
        const zh = () => langRef.current.startsWith("zh");
        const barTimer = window.setTimeout(() => {
            if (!cancelled) setBarOn(true);
        }, PREVIEW_BAR_DELAY_MS);
        const fail = (message: string) => {
            if (cancelled) return;
            window.clearTimeout(barTimer);
            setError(localizeTaskResultPreviewError(message, langRef.current));
            setLoading(false);
        };
        const note = (phase: PreviewProgress["phase"], ratio: number | null) => {
            if (!cancelled) setProgress({ phase, ratio });
        };
        setLoading(true);
        setError("");
        setProgress({ phase: "wait", ratio: null });
        setBarOn(false);
        const host = hostRef.current;
        if (host) host.replaceChildren();
        void (async () => {
            let styleHost: HTMLDivElement | null = null;
            let bodyHost: HTMLDivElement | null = null;
            let target: HTMLDivElement | null = null;
            try {
                const result = await PreviewTaskResultFile(absPath) as TaskResultPreviewPayload;
                if (cancelled) return;
                const previewURL = String(result?.preview_url || "").trim();
                if (!isDocxPreviewURL(previewURL)) {
                    fail(zh() ? "无法预览该 Word 文档" : "This Word document cannot be previewed");
                    return;
                }
                const response = await fetch(previewURL, { signal: controller.signal });
                if (!response.ok) {
                    fail(zh() ? "无法读取该 Word 文档" : "This Word document could not be read");
                    return;
                }
                note("read", null);
                const bytes = await readPreviewBody(response, {
                    signal: controller.signal,
                    onProgress: (ratio) => note("read", ratio),
                });
                target = hostRef.current;
                if (cancelled || !target) return;
                const { renderAsync } = await import("docx-preview");
                if (cancelled || hostRef.current !== target) return;
                note("parse", null);
                // A long synchronous stretch inside the renderer cannot paint
                // a bar that was only scheduled. Large documents show it first.
                if (bytes.byteLength >= PREVIEW_HEAVY_PARSE_BYTES) {
                    if (!cancelled) setBarOn(true);
                    await yieldPreviewPaint();
                    if (cancelled || hostRef.current !== target) return;
                }
                styleHost = document.createElement("div");
                bodyHost = document.createElement("div");
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
                const pane = scrollRef.current;
                fitDocxToPane(pane, bodyHost);
                if (pane) pane.scrollTop = 0;
                window.clearTimeout(barTimer);
                setLoading(false);
            } catch (err) {
                if (cancelled || isPreviewAbortError(err)) return;
                fail(err instanceof Error ? err.message : String(err || ""));
            } finally {
                // renderAsync keeps writing into these nodes after the preview
                // is gone, including when it throws partway through.
                if (bodyHost && styleHost && (cancelled || hostRef.current !== target)) {
                    bodyHost.replaceChildren();
                    styleHost.replaceChildren();
                }
            }
        })();
        const scroll = scrollRef.current;
        let fittedWidth = scroll?.clientWidth ?? -1;
        const observer = scroll && typeof ResizeObserver !== "undefined"
            ? new ResizeObserver(() => {
                const pane = scrollRef.current;
                const host = hostRef.current;
                if (!pane || !host) return;
                const width = pane.clientWidth;
                // Height changes, including the scrollbar, are not a new layout.
                if (width <= 0 || width === fittedWidth) return;
                fittedWidth = width;
                const top = pane.scrollTop;
                const body = host.querySelector(":scope > div:last-child");
                if (body instanceof HTMLElement) fitDocxToPane(pane, body);
                if (top > 0) pane.scrollTop = top;
            })
            : null;
        observer?.observe(scroll!);
        return () => {
            cancelled = true;
            controller.abort();
            window.clearTimeout(barTimer);
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
                <PreviewLoadingStatus
                    testId="docx-preview-loading"
                    style={{ padding: 20, color: theme.textMuted, fontSize: 13 }}
                    phase={progress.phase}
                    ratio={progress.ratio}
                    showBar={barOn}
                    phrases={{
                        wait: tailText(lang, "Loading Word preview…", "正在加载 Word 预览…"),
                        read: tailText(lang, "Reading Word…", "正在读取 Word…"),
                        parse: tailText(lang, "Parsing Word…", "正在解析 Word…"),
                    }}
                />
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
    // Horizontal pin only. The reader may already be on a later page;
    // a width refit must not send them back to the top.
    scroll.scrollLeft = 0;
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
