import { useEffect, useRef, useState, type CSSProperties, type ReactElement } from "react";
import { AIAssistantAttachmentOpeningPreview, PreviewTaskResultFile } from "../../../wailsjs/go/main/App";
import { attachmentHasDocumentPreview, filePreviewKindFromName } from "../preview/filePreviewKind";
import type { CodePreviewTheme } from "./FileTabBar";
import { MarkdownPreview } from "./CodePreviewMarkdown";
import { fitDocxToPane } from "./DocxPreviewPanel";
import type { Theme } from "./aiAssistantPanelTheme";
import {
    ATTACHMENT_DOC_PREVIEW_MAX_HEIGHT,
    ATTACHMENT_DOC_PREVIEW_MAX_WIDTH,
    attachmentPreviewRuneCount,
    openingPreviewPageCount,
    openingPreviewTexts,
    splitTextIntoPreviewPages,
} from "./attachmentOpeningPreview";
import { isTaskResultPdfPreviewURL, type TaskResultPreviewPayload } from "./taskResultPreview";

const FOOTER_HEIGHT = 28;
const CARD_BORDER = 1;
// The card is border-box. Leave the border out of the page window so the
// pages fill it exactly and a 1px overflow does not open a scrollbar.
const PAGE_VIEW_HEIGHT = ATTACHMENT_DOC_PREVIEW_MAX_HEIGHT - FOOTER_HEIGHT - CARD_BORDER * 2;
const SHEET_MEASURE_ROWS = 40;
const SHEET_MEASURE_COLS = 12;
const SHEET_SHOW_ROWS = 16;
const SHEET_SHOW_COLS = 8;

type OpeningPage = {
    index?: number;
    text?: string;
    width?: number;
    height?: number;
    image_data_url?: string;
};

type OpeningPayload = {
    kind?: string;
    preview_url?: string;
    pages?: OpeningPage[];
};

type SheetPage = { name: string; rows: string[][] };

type PreviewReady =
    | { mode: "pdf"; src: string; pageCount: number }
    | { mode: "docx"; previewURL: string }
    | { mode: "images"; pages: { index: number; src: string }[] }
    | { mode: "sheets"; sheets: SheetPage[] }
    | { mode: "html"; markup: string; pageCount: number }
    | { mode: "text"; pages: string[]; rich: boolean; mono: boolean };

export function AttachmentDocumentPreview({
    filePath,
    fileName,
    extension,
    theme,
    lang,
    onFail,
}: {
    filePath: string;
    fileName: string;
    extension?: string;
    theme: Theme;
    lang: string;
    onFail: () => void;
}) {
    const isZh = lang.startsWith("zh");
    const onFailRef = useRef(onFail);
    onFailRef.current = onFail;
    const [ready, setReady] = useState<PreviewReady | null>(null);
    const [docxPages, setDocxPages] = useState(1);
    const [near, setNear] = useState(false);
    const frameRef = useRef<HTMLDivElement>(null);
    const loadedFor = useRef("");
    const name = fileName || filePath;
    const reservesWindow = previewReservesWindow(name);

    useEffect(() => {
        const node = frameRef.current;
        if (!node || typeof IntersectionObserver === "undefined") {
            setNear(true);
            return;
        }
        // The chat list and the composer strip scroll inside their own boxes.
        // A viewport margin never looks past those clips, so the root has to be
        // the scrollport that actually hides the card.
        const root = attachmentPreviewScrollRoot(node);
        const observer = new IntersectionObserver((entries) => {
            setNear(entries.some((entry) => entry.isIntersecting));
        }, { root, rootMargin: "480px" });
        observer.observe(node);
        return () => observer.disconnect();
    }, [filePath, name]);

    useEffect(() => {
        if (!near) return;
        const key = `${filePath}\0${name}`;
        if (loadedFor.current === key) return;
        let cancelled = false;
        setReady(null);
        setDocxPages(1);
        const fail = () => {
            if (!cancelled) onFailRef.current();
        };
        void (async () => {
            try {
                const next = await loadAttachmentPreviewCached(filePath, name);
                if (cancelled) return;
                if (!next) {
                    fail();
                    return;
                }
                loadedFor.current = key;
                setReady(next);
            } catch {
                fail();
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [near, filePath, name]);

    let card: ReactElement;
    if (!ready) {
        card = (
            <div
                data-testid="attachment-document-preview"
                data-state="loading"
                data-preview-max-width={ATTACHMENT_DOC_PREVIEW_MAX_WIDTH}
                data-preview-max-height={ATTACHMENT_DOC_PREVIEW_MAX_HEIGHT}
                aria-hidden="true"
                style={cardStyle(theme, reservesWindow ? "window" : "loading")}
            />
        );
    } else {
        const fixedWindow = ready.mode === "pdf" || ready.mode === "docx" || ready.mode === "html";
        const pageCount = ready.mode === "docx"
            ? docxPages
            : ready.mode === "pdf" || ready.mode === "html"
                ? ready.pageCount
                : ready.mode === "images"
                    ? ready.pages.length
                    : ready.mode === "sheets"
                        ? ready.sheets.length
                        : ready.pages.length;
        card = (
            <div
                data-testid="attachment-document-preview"
                data-state="ready"
                data-preview-kind={ready.mode}
                data-page-count={pageCount}
                data-preview-max-width={ATTACHMENT_DOC_PREVIEW_MAX_WIDTH}
                data-preview-max-height={ATTACHMENT_DOC_PREVIEW_MAX_HEIGHT}
                aria-label={isZh ? `预览 ${name}` : `Preview ${name}`}
                title={filePath}
                style={cardStyle(theme, fixedWindow ? "window" : "content")}
            >
                <div style={pageViewportStyle(fixedWindow)}>
                    {ready.mode === "pdf" && near ? <PdfPageFrame src={ready.src} title={name} pageCount={ready.pageCount} /> : null}
                    {ready.mode === "docx" ? (
                        <DocxPageFrame
                            previewURL={ready.previewURL}
                            onPages={setDocxPages}
                            onFail={() => onFailRef.current()}
                        />
                    ) : null}
                    {ready.mode === "images" ? <ImagePages pages={ready.pages} /> : null}
                    {ready.mode === "sheets" ? <SheetPages sheets={ready.sheets} /> : null}
                    {ready.mode === "html" && near ? <HtmlPageFrame markup={ready.markup} title={name} /> : null}
                    {ready.mode === "text" ? <TextPages pages={ready.pages} rich={ready.rich} mono={ready.mono} isZh={isZh} /> : null}
                </div>
                <div style={footerStyle(theme)}>
                    <span style={footerNameStyle}>{compactAttachmentLabel(name, extension || "")}</span>
                    <span>{pageCaption(pageCount, isZh)}</span>
                </div>
            </div>
        );
    }

    return (
        <div ref={frameRef} style={{ width: "100%", maxWidth: "100%", minWidth: 0 }}>
            {card}
        </div>
    );
}

function cardStyle(theme: Theme, layout: "loading" | "window" | "content"): CSSProperties {
    return {
        display: "flex",
        flexDirection: "column",
        width: ATTACHMENT_DOC_PREVIEW_MAX_WIDTH,
        maxWidth: "100%",
        height: layout === "window" ? ATTACHMENT_DOC_PREVIEW_MAX_HEIGHT : layout === "loading" ? 320 : "auto",
        maxHeight: ATTACHMENT_DOC_PREVIEW_MAX_HEIGHT,
        boxSizing: "border-box",
        overflow: "hidden",
        borderRadius: 10,
        border: `1px solid ${theme.codeBlockBorder}`,
        background: "#fff",
        flexShrink: 0,
    };
}

function pageViewportStyle(fixedWindow: boolean): CSSProperties {
    return {
        flex: "1 1 auto",
        minHeight: 0,
        height: fixedWindow ? PAGE_VIEW_HEIGHT : "auto",
        maxHeight: PAGE_VIEW_HEIGHT,
        overflow: "auto",
        background: "#fff",
    };
}

function footerStyle(theme: Theme): CSSProperties {
    return {
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 8,
        height: FOOTER_HEIGHT,
        flex: "0 0 auto",
        padding: "0 8px",
        boxSizing: "border-box",
        background: theme.codeBlockBg,
        color: theme.textMuted,
        fontSize: 11,
        lineHeight: 1.2,
    };
}

const footerNameStyle: CSSProperties = {
    minWidth: 0,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
};

function pageCaption(pageCount: number, isZh: boolean): string {
    if (pageCount <= 1) return isZh ? "第 1 页" : "Page 1";
    return isZh ? `第 1–${pageCount} 页` : `Pages 1–${pageCount}`;
}

function compactAttachmentLabel(fileName: string, extension: string): string {
    const base = fileName.split(/[/\\]/).pop() || fileName;
    return base || extension || fileName;
}

function PdfPageFrame({ src, title, pageCount }: { src: string; title: string; pageCount: number }) {
    const count = Math.max(1, pageCount);
    // One page uses the whole window and can scroll inside it. Extra pages,
    // pulled in because earlier pages had little text, share that same window
    // so each top stays visible. Those frames ignore the pointer so the wheel
    // moves the card instead of getting stuck inside one PDF plugin.
    const each = Math.max(160, Math.floor(PAGE_VIEW_HEIGHT / count));
    return (
        <div style={{ display: "flex", flexDirection: "column", minHeight: "100%" }}>
            {Array.from({ length: count }, (_, index) => {
                const page = index + 1;
                const single = count === 1;
                return (
                    <iframe
                        key={page}
                        data-testid="attachment-document-pdf"
                        title={single ? title : `${title} ${page}`}
                        src={pdfFrameSrc(src, page, single)}
                        loading={index === 0 ? "eager" : "lazy"}
                        style={{
                            width: "100%",
                            height: each,
                            flex: "0 0 auto",
                            boxSizing: "border-box",
                            border: "none",
                            borderTop: index === 0 ? "none" : "1px solid #e7e5e4",
                            background: "#fff",
                            display: "block",
                            pointerEvents: count === 1 ? "auto" : "none",
                        }}
                    />
                );
            })}
        </div>
    );
}

function ImagePages({ pages }: { pages: { index: number; src: string }[] }) {
    return (
        <div style={{ display: "flex", flexDirection: "column", gap: 8, padding: 8, boxSizing: "border-box" }}>
            {pages.map((page) => (
                <img
                    key={page.index}
                    data-testid="attachment-document-page"
                    alt=""
                    src={page.src}
                    style={{ width: "100%", height: "auto", display: "block", background: "#fff" }}
                />
            ))}
        </div>
    );
}

function SheetPages({ sheets }: { sheets: SheetPage[] }) {
    return (
        <div style={{ display: "flex", flexDirection: "column", gap: 10, padding: 8, boxSizing: "border-box" }}>
            {sheets.map((sheet) => (
                <div key={sheet.name}>
                    {sheets.length > 1 ? (
                        <div style={{ fontSize: 12, fontWeight: 650, color: "#44403c", marginBottom: 4 }}>{sheet.name}</div>
                    ) : null}
                    <table style={{ borderCollapse: "collapse", width: "100%", fontSize: 13, lineHeight: 1.35, color: "#1c1917" }}>
                        <tbody>
                            {sheet.rows.slice(0, SHEET_SHOW_ROWS).map((row, rowIndex) => (
                                <tr key={rowIndex}>
                                    {row.slice(0, SHEET_SHOW_COLS).map((cell, cellIndex) => (
                                        <td key={cellIndex} style={{ border: "1px solid #e7e5e4", padding: "3px 6px", verticalAlign: "top", whiteSpace: "pre-wrap" }}>
                                            {cell}
                                        </td>
                                    ))}
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            ))}
        </div>
    );
}

function HtmlPageFrame({ markup, title }: { markup: string; title: string }) {
    return (
        <iframe
            data-testid="attachment-document-html"
            title={title}
            sandbox=""
            srcDoc={markup}
            style={{ width: "100%", height: "100%", minHeight: PAGE_VIEW_HEIGHT, border: "none", background: "#fff", display: "block" }}
        />
    );
}

function trimPreviewPage(page: string): string {
    const trimmed = page.replace(/^\n+|\n+$/g, "");
    return trimmed || " ";
}

function TextPages({ pages, rich, mono, isZh }: { pages: string[]; rich: boolean; mono: boolean; isZh: boolean }) {
    return (
        <div>
            {pages.map((page, index) => (
                <section key={index} data-testid="attachment-document-page" style={{ borderTop: index === 0 ? "none" : "1px solid #e7e5e4" }}>
                    {pages.length > 1 ? (
                        <div style={{ padding: "8px 16px 0", fontSize: 11, color: "#78716c" }}>
                            {isZh ? `第 ${index + 1} 页` : `Page ${index + 1}`}
                        </div>
                    ) : null}
                    {rich ? (
                        <MarkdownPreview content={trimPreviewPage(page)} theme={paperTheme} />
                    ) : (
                        <pre style={{ margin: 0, padding: "14px 16px", whiteSpace: "pre-wrap", wordBreak: "break-word", fontSize: 14, lineHeight: 1.5, color: "#1c1917", fontFamily: mono ? "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace" : "ui-sans-serif, system-ui, sans-serif" }}>
                            {trimPreviewPage(page)}
                        </pre>
                    )}
                </section>
            ))}
        </div>
    );
}

/**
 * Keep the opening Word pages and remove the full-viewer frame.
 * docx-preview pads the wrapper by 30px and shadows each page. That frame is
 * inside the measured width, so the page is scaled down to fit the gutter.
 */
export function settleAttachmentDocxHost(host: HTMLElement): number {
    const sections = [...host.querySelectorAll("section.maclaw-docx")].filter((node): node is HTMLElement => node instanceof HTMLElement);
    if (sections.length === 0) return 0;
    const count = openingPreviewPageCount(sections.map((section) => section.textContent || ""));
    sections.slice(count).forEach((section) => section.remove());
    const wrapper = host.querySelector(".maclaw-docx-wrapper");
    if (wrapper instanceof HTMLElement) {
        wrapper.style.padding = "0";
        wrapper.style.background = "transparent";
        wrapper.style.gap = "0";
    }
    sections.slice(0, count).forEach((section, index) => {
        section.style.margin = "0";
        section.style.boxShadow = "none";
        section.style.borderTop = index === 0 ? "none" : "1px solid #e7e5e4";
    });
    return count;
}

/** Keep each included Word page's top inside the same window. A single page stays full height so the card can still scroll. */
function clipDocxPreviewPages(host: HTMLElement, pageCount: number) {
    if (pageCount <= 1) return;
    const each = Math.max(160, Math.floor(PAGE_VIEW_HEIGHT / pageCount));
    const body = host.querySelector(":scope > div:last-child");
    const zoom = body instanceof HTMLElement ? parseFloat(body.style.getPropertyValue("zoom")) || 1 : 1;
    const local = `${each / (zoom > 0 ? zoom : 1)}px`;
    host.querySelectorAll("section.maclaw-docx").forEach((node) => {
        if (!(node instanceof HTMLElement)) return;
        node.style.minHeight = "0";
        node.style.height = local;
        node.style.maxHeight = local;
        node.style.overflow = "hidden";
    });
}

const paperTheme: CodePreviewTheme = {
    bg: "#ffffff",
    text: "#1c1917",
    textMuted: "#78716c",
    border: "#e7e5e4",
    lineNumBg: "#fafaf9",
    lineNumText: "#a8a29e",
    tabBg: "#ffffff",
    tabActiveBg: "#ffffff",
    tabActiveText: "#1c1917",
    tabHoverBg: "#fafaf9",
    diffAddBg: "#ecfdf3",
    diffAddText: "#166534",
    diffDeleteBg: "#fef2f2",
    diffDeleteText: "#991b1b",
    syntaxKeyword: "#9f1239",
    syntaxString: "#1e3a8a",
    syntaxComment: "#78716c",
    syntaxNumber: "#1d4ed8",
    syntaxFunction: "#6d28d9",
    syntaxType: "#9a3412",
    syntaxOperator: "#1c1917",
};

function DocxPageFrame({
    previewURL,
    onPages,
    onFail,
}: {
    previewURL: string;
    onPages: (count: number) => void;
    onFail: () => void;
}) {
    const hostRef = useRef<HTMLDivElement>(null);
    const scrollRef = useRef<HTMLDivElement>(null);
    const onFailRef = useRef(onFail);
    const onPagesRef = useRef(onPages);
    onFailRef.current = onFail;
    onPagesRef.current = onPages;

    useEffect(() => {
        let cancelled = false;
        const host = hostRef.current;
        if (host) host.replaceChildren();
        void (async () => {
            try {
                const response = await fetch(previewURL);
                if (!response.ok) {
                    if (!cancelled) onFailRef.current();
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
                // The library draws a gray desk, page shadow, and 30px gutter
                // for the full viewer. Inside this card that gutter is what
                // shrinks the page, so the opening pages fill the card instead.
                const count = settleAttachmentDocxHost(target);
                if (count === 0) {
                    onFailRef.current();
                    return;
                }
                clipDocxPreviewPages(target, count);
                onPagesRef.current(count);
                fitDocxToPane(scrollRef.current, bodyHost);
                clipDocxPreviewPages(target, count);
            } catch {
                if (!cancelled) onFailRef.current();
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
                // Height changes from clipping this same card must not refit.
                // fitDocxToPane also jumps back to the top of the page.
                if (width <= 0 || width === fittedWidth) return;
                fittedWidth = width;
                const top = pane.scrollTop;
                const body = host.querySelector(":scope > div:last-child");
                if (body instanceof HTMLElement) fitDocxToPane(pane, body);
                const kept = host.querySelectorAll("section.maclaw-docx").length;
                if (kept > 1) clipDocxPreviewPages(host, kept);
                if (top > 0) pane.scrollTop = top;
            })
            : null;
        observer?.observe(scroll!);
        return () => {
            cancelled = true;
            observer?.disconnect();
        };
    }, [previewURL]);

    return (
        <div ref={scrollRef} data-testid="attachment-document-docx" style={{ width: "100%", height: "100%", minHeight: PAGE_VIEW_HEIGHT, overflow: "auto", background: "#f5f5f4" }}>
            <div ref={hostRef} />
        </div>
    );
}

async function loadAttachmentDocumentPreview(filePath: string, fileName: string): Promise<PreviewReady | null> {
    if (!attachmentHasDocumentPreview(fileName)) return null;
    const kind = filePreviewKindFromName(fileName);
    if (kind === "pdf" || kind === "docx" || kind === "pptx") {
        const payload = await AIAssistantAttachmentOpeningPreview(filePath) as OpeningPayload;
        return readyFromOpening(kind, payload);
    }
    const result = await PreviewTaskResultFile(filePath) as TaskResultPreviewPayload;
    if (kind === "xlsx") return readyFromWorkbook(result);
    const content = String(result?.content || "");
    if (!content.trim()) return null;
    if (kind === "html" || ((kind === "text" || kind === "code") && looksLikeHtmlDocument(content))) {
        // One sandboxed frame shows the document. A made-up page count would
        // claim pages the frame does not split.
        return { mode: "html", markup: content, pageCount: 1 };
    }
    const pages = openingPreviewTexts(splitTextIntoPreviewPages(content));
    if (pages.length === 0 || (pages.length === 1 && attachmentPreviewRuneCount(pages[0]) === 0 && !content.trim())) {
        return null;
    }
    return { mode: "text", pages, rich: kind === "markdown" || kind === "office", mono: kind === "code" || kind === "latex" };
}

function readyFromOpening(kind: string, payload: OpeningPayload): PreviewReady | null {
    const pages = Array.isArray(payload?.pages) ? payload.pages : [];
    const previewURL = String(payload?.preview_url || "").trim();
    if (kind === "pdf" && isTaskResultPdfPreviewURL(previewURL)) {
        return { mode: "pdf", src: previewURL, pageCount: Math.max(1, pages.length) };
    }
    if (kind === "docx" && isTaskResultPdfPreviewURL(previewURL)) {
        return { mode: "docx", previewURL };
    }
    if (kind === "pptx") {
        const images = pages
            .map((page, index) => ({ index: page.index || index + 1, src: String(page.image_data_url || "").trim() }))
            .filter((page) => page.src.startsWith("data:image/"));
        if (images.length > 0) return { mode: "images", pages: images };
        const texts = openingPreviewTexts(pages.map((page) => String(page.text || "")));
        if (texts.some((text) => text.trim())) return { mode: "text", pages: texts, rich: false, mono: false };
    }
    const texts = pages.map((page) => String(page.text || "")).filter((text) => text.trim());
    if (texts.length > 0) return { mode: "text", pages: texts, rich: false, mono: false };
    return null;
}

async function readyFromWorkbook(result: TaskResultPreviewPayload): Promise<PreviewReady | null> {
    const previewURL = String(result?.preview_url || "").trim();
    const extract = String(result?.content || "");
    if (isTaskResultPdfPreviewURL(previewURL)) {
        try {
            const response = await fetch(previewURL);
            if (response.ok) {
                const bytes = await response.arrayBuffer();
                const XLSX = await import("xlsx");
                const workbook = XLSX.read(new Uint8Array(bytes), { type: "array", sheetRows: SHEET_MEASURE_ROWS });
                const names = workbook.SheetNames.filter((name) => Boolean(workbook.Sheets[name]));
                const measured = names.map((name) => ({
                    name,
                    rows: worksheetPreviewRows(XLSX, workbook.Sheets[name]),
                }));
                const count = openingPreviewPageCount(measured.map((sheet) => sheet.rows.map((row) => row.join(" ")).join("\n")));
                const sheets = measured.slice(0, Math.max(count, names.length > 0 ? 1 : 0));
                if (sheets.length > 0 && sheets.some((sheet) => sheet.rows.some((row) => row.some((cell) => cell.trim())))) {
                    return { mode: "sheets", sheets };
                }
            }
        } catch {
            // Fall through to the text extract.
        }
    }
    if (!extract.trim()) return null;
    return { mode: "text", pages: openingPreviewTexts(splitTextIntoPreviewPages(extract)), rich: false, mono: false };
}

function worksheetPreviewRows(XLSX: typeof import("xlsx"), sheet: import("xlsx").WorkSheet | undefined): string[][] {
    if (!sheet || !sheet["!ref"]) return [];
    const full = XLSX.utils.decode_range(sheet["!ref"]);
    const range = {
        s: { r: full.s.r, c: full.s.c },
        e: {
            r: Math.min(full.e.r, full.s.r + SHEET_MEASURE_ROWS - 1),
            c: Math.min(full.e.c, full.s.c + SHEET_MEASURE_COLS - 1),
        },
    };
    const raw = XLSX.utils.sheet_to_json<string[]>(sheet, {
        header: 1,
        defval: "",
        raw: false,
        range,
    });
    return raw.map((row) => (Array.isArray(row) ? row.map((cell) => String(cell ?? "")) : []));
}

const PREVIEW_CACHE_MS = 15_000;
const PREVIEW_CACHE_MAX = 8;

type PreviewCacheEntry = { at: number; pending: Promise<PreviewReady | null> };

const previewLoads = new Map<string, PreviewCacheEntry>();

function loadAttachmentPreviewCached(filePath: string, fileName: string): Promise<PreviewReady | null> {
    const key = `${filePath}\0${fileName}`;
    const now = Date.now();
    const hit = previewLoads.get(key);
    if (hit && now - hit.at < PREVIEW_CACHE_MS) {
        previewLoads.delete(key);
        previewLoads.set(key, hit);
        return hit.pending;
    }
    const entry: PreviewCacheEntry = { at: now, pending: Promise.resolve(null) };
    entry.pending = loadAttachmentDocumentPreview(filePath, fileName).then(
        (value) => {
            if (!value && previewLoads.get(key) === entry) previewLoads.delete(key);
            return value;
        },
        (error: unknown) => {
            if (previewLoads.get(key) === entry) previewLoads.delete(key);
            throw error;
        },
    );
    previewLoads.delete(key);
    previewLoads.set(key, entry);
    while (previewLoads.size > PREVIEW_CACHE_MAX) {
        const oldest = previewLoads.keys().next().value;
        if (oldest === undefined) break;
        previewLoads.delete(oldest);
    }
    return entry.pending;
}

/** The scrollport that clips this card. The document viewport is the fallback. */
function attachmentPreviewScrollRoot(node: HTMLElement): Element | null {
    let parent = node.parentElement;
    while (parent) {
        const style = getComputedStyle(parent);
        const y = style.overflowY;
        if (y === "auto" || y === "scroll" || style.overflow === "auto" || style.overflow === "scroll") return parent;
        parent = parent.parentElement;
    }
    return null;
}

function previewReservesWindow(fileName: string): boolean {
    const kind = filePreviewKindFromName(fileName);
    return kind === "pdf" || kind === "docx" || kind === "html";
}

function pdfFrameSrc(src: string, page: number, single: boolean): string {
    const base = src.split("#")[0];
    const joiner = base.includes("?") ? "&" : "?";
    // A distinct query keeps each page's PDF plugin from sharing one document.
    // The preview server only reads the token.
    const hash = single
        ? `#page=${page}&toolbar=0&navpanes=0&view=FitH`
        : `#page=${page}&toolbar=0&navpanes=0&scrollbar=0&view=FitH`;
    return `${base}${joiner}p=${page}${hash}`;
}

function looksLikeHtmlDocument(value: string): boolean {
    const head = value.trim().slice(0, 512);
    if (!head) return false;
    return /<!doctype\s+html|<html[\s>]|<head[\s>]|<body[\s>]/i.test(head);
}


