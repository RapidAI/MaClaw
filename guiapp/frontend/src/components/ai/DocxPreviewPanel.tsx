import { useEffect, useRef, useState, type CSSProperties, type RefObject } from "react";
import { PreviewTaskResultFile } from "../../../wailsjs/go/main/App";
import type { CodePreviewTheme } from "./FileTabBar";
import { isTaskResultPdfPreviewURL, localizeTaskResultPreviewError, type TaskResultPreviewPayload } from "./taskResultPreview";
import { isPreviewAbortError, readPreviewBody } from "./previewBytes";
import { PREVIEW_BAR_DELAY_MS, PREVIEW_HEAVY_PARSE_BYTES, PreviewLoadingStatus, yieldPreviewPaint, type PreviewProgress } from "./PreviewProgressBar";
import { tailText } from "../../i18n/mnTail"
import { DocumentFindBar } from "./DocumentFindBar";
import { activateDomFindMark, clearDomFindMarks, collectFindSources, findInFolded, foldFindSources, paintDomFindMarks, scrollMatchIntoScroller, type FoldedText, type TextRun } from "./documentFind";

/** .docx is the OOXML word processor file the in-app renderer can lay out. */
export function isDocxFileName(name: string): boolean {
    return name.toLowerCase().endsWith(".docx");
}

export interface DocxPreviewPanelProps {
    absPath: string;
    theme: CodePreviewTheme;
    lang: string;
}

function foldedDocument(
    cache: { current: { texts: string[]; gaps: string[]; folded: FoldedText } | null },
    sources: readonly TextRun[],
): FoldedText {
    const cached = cache.current;
    if (cached && cached.texts.length === sources.length) {
        let same = true;
        for (let index = 0; index < sources.length; index += 1) {
            if (sources[index].text !== cached.texts[index] || (sources[index].gapAfter ?? "") !== cached.gaps[index]) {
                same = false;
                break;
            }
        }
        if (same) return cached.folded;
    }
    const folded = foldFindSources(sources);
    cache.current = {
        texts: sources.map((run) => run.text),
        gaps: sources.map((run) => run.gapAfter ?? ""),
        folded,
    };
    return folded;
}

/**
 * In-app Word preview. The bytes are the original .docx; docx-preview lays
 * out pages, styles, tables, and images. The text extract used to drop all
 * of that and show a line-numbered dump.
 */
export function DocxPreviewPanel({ absPath, theme, lang }: DocxPreviewPanelProps) {
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
    const [findQuery, setFindQuery] = useState("");
    const [findCount, setFindCount] = useState(0);
    const [findActive, setFindActive] = useState(0);
    const [findBusy, setFindBusy] = useState(false);
    const findActiveRef = useRef(0);
    findActiveRef.current = findActive;
    const findFoldRef = useRef<{ texts: string[]; gaps: string[]; folded: FoldedText } | null>(null);

    useEffect(() => {
        setFindQuery("");
        setFindCount(0);
        setFindActive(0);
        setFindBusy(false);
        findFoldRef.current = null;
    }, [absPath]);

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
            const host = hostRef.current;
            if (host) clearDomFindMarks(host);
        };
    }, [absPath]);

    useEffect(() => {
        const root = hostRef.current;
        if (!root || loading) return undefined;
        const needle = findQuery.trim();
        clearDomFindMarks(root);
        if (!needle) {
            setFindCount(0);
            setFindBusy(false);
            return undefined;
        }
        setFindBusy(true);
        setFindCount(0);
        let cancel = false;
        const timer = window.setTimeout(() => {
            if (cancel || !root.isConnected) return;
            const sources = collectFindSources(root);
            const hits = findInFolded(foldedDocument(findFoldRef, sources), needle);
            const shown = hits.length ? Math.min(Math.max(findActiveRef.current, 0), hits.length - 1) : -1;
            if (shown >= 0) {
                paintDomFindMarks(hits.flatMap((hit, index) => hit.parts.flatMap((part) => {
                    const node = sources[part.run]?.node;
                    if (!node) return [];
                    return [{ node, start: part.start, end: part.end, active: index === shown, index }];
                })));
            }
            setFindCount(hits.length);
            setFindActive(shown < 0 ? 0 : shown);
            setFindBusy(false);
        }, 120);
        return () => {
            cancel = true;
            window.clearTimeout(timer);
            clearDomFindMarks(root);
        };
    }, [findQuery, loading, absPath]);

    useEffect(() => {
        const root = hostRef.current;
        const scroller = scrollRef.current;
        if (!root || !scroller || loading || findCount <= 0) return;
        const mark = activateDomFindMark(root, findActive);
        if (mark) scrollMatchIntoScroller(scroller, mark);
    }, [findActive, findCount, loading]);

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
        <div data-testid="docx-preview-panel" style={fill}>
            <div className="companion-doc-toolbar">
                <DocxPager lang={lang} scrollerRef={scrollRef} hostRef={hostRef} ready={!loading} />
                <DocumentFindBar
                    lang={lang}
                    query={findQuery}
                    onQueryChange={(value) => {
                        setFindQuery(value);
                        setFindActive(0);
                        setFindCount(0);
                        setFindBusy(value.trim().length > 0);
                    }}
                    count={findCount}
                    active={findActive}
                    busy={findBusy}
                    onPrev={() => setFindActive((current) => (findCount <= 0 ? 0 : (Math.max(current, 0) - 1 + findCount) % findCount))}
                    onNext={() => setFindActive((current) => (findCount <= 0 ? 0 : (Math.max(current, 0) + 1) % findCount))}
                />
            </div>
            <div
                ref={scrollRef}
                data-testid="docx-preview-scroll"
                style={{ flex: 1, minHeight: 0, width: "100%", minWidth: 0, overflow: "auto" }}
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

function DocxPager({
    lang,
    scrollerRef,
    hostRef,
    ready,
}: {
    lang: string;
    scrollerRef: RefObject<HTMLDivElement | null>;
    hostRef: RefObject<HTMLDivElement | null>;
    ready: boolean;
}) {
    const [current, setCurrent] = useState(1);
    const [draft, setDraft] = useState("1");
    const editing = useRef(false);
    const draftRef = useRef("1");
    const draftAtFocus = useRef("1");
    const total = ready ? (hostRef.current?.querySelectorAll("section").length ?? 0) : 0;

    useEffect(() => {
        const scroller = scrollerRef.current;
        const host = hostRef.current;
        if (!ready || !scroller || !host || total < 1) return undefined;
        let seenPage = -1;
        const read = () => {
            const pages = [...host.querySelectorAll<HTMLElement>("section")];
            if (!pages.length) return;
            const edge = scroller.getBoundingClientRect().top + 8;
            let chosen = 0;
            for (let index = 0; index < pages.length; index += 1) {
                const box = pages[index].getBoundingClientRect();
                if (box.height < 1 && box.width < 1) continue;
                if (box.top <= edge) chosen = index;
                else break;
            }
            const page = chosen + 1;
            if (page === seenPage) return;
            seenPage = page;
            setCurrent(page);
            if (!editing.current) {
                draftRef.current = String(page);
                setDraft(String(page));
            }
        };
        read();
        scroller.addEventListener("scroll", read, { passive: true });
        return () => scroller.removeEventListener("scroll", read);
    }, [ready, scrollerRef, hostRef, total]);

    if (!ready || total < 1) return null;

    const jump = (page: number) => {
        const host = hostRef.current;
        const scroller = scrollerRef.current;
        if (!host || !scroller) return;
        const next = Math.min(total, Math.max(1, Math.trunc(page) || 1));
        const el = host.querySelectorAll<HTMLElement>("section")[next - 1];
        if (!el) return;
        const delta = el.getBoundingClientRect().top - scroller.getBoundingClientRect().top;
        if (Math.abs(delta) >= 1) scroller.scrollTop += delta;
        editing.current = false;
        draftRef.current = String(next);
        setDraft(String(next));
        setCurrent(next);
    };

    return (
        <form
            className="companion-pdf-pager"
            data-testid="docx-pager"
            aria-label={tailText(lang, `Word pages, page ${current} of ${total}`, `Word 页码，第 ${current} 页，共 ${total} 页`)}
            onSubmit={(event) => {
                event.preventDefault();
                jump(Number(draftRef.current));
            }}
        >
            <button
                type="button"
                data-testid="docx-page-prev"
                aria-label={tailText(lang, "Previous page", "前一页")}
                title={tailText(lang, "Previous page", "前一页")}
                disabled={current <= 1}
                onClick={() => jump(current - 1)}
            >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M14 6 8 12l6 6" />
                    <path d="M8 6v12" />
                </svg>
            </button>
            <label className="companion-pdf-pager-field">
                {lang.startsWith("zh") ? <span>第</span> : null}
                <input
                    className="companion-pdf-pager-input"
                    data-testid="docx-goto"
                    inputMode="numeric"
                    autoComplete="off"
                    spellCheck={false}
                    aria-label={tailText(lang, "Go to page", "跳转到页码")}
                    value={draft}
                    onChange={(event) => {
                        const value = event.target.value.replace(/\D/g, "").slice(0, 6);
                        editing.current = true;
                        draftRef.current = value;
                        setDraft(value);
                    }}
                    onFocus={(event) => {
                        editing.current = true;
                        draftAtFocus.current = event.currentTarget.value;
                        event.currentTarget.select();
                    }}
                    onBlur={() => {
                        editing.current = false;
                        if (draftRef.current.trim() === draftAtFocus.current.trim()) {
                            setDraft(String(current));
                            return;
                        }
                        jump(Number(draftRef.current));
                    }}
                />
                <span>/ {total}{tailText(lang, "", " 页")}</span>
            </label>
            <button
                type="button"
                className="companion-pdf-pager-go"
                data-testid="docx-goto-go"
                aria-label={tailText(lang, "Go", "前往")}
                title={tailText(lang, "Go to this page", "前往该页")}
                onClick={() => jump(Number(draftRef.current))}
            >
                {tailText(lang, "Go", "前往")}
            </button>
            <button
                type="button"
                data-testid="docx-page-next"
                aria-label={tailText(lang, "Next page", "后一页")}
                title={tailText(lang, "Next page", "后一页")}
                disabled={current >= total}
                onClick={() => jump(current + 1)}
            >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="m10 6 6 6-6 6" />
                    <path d="M16 6v12" />
                </svg>
            </button>
        </form>
    );
}
