import { useEffect, useRef, useState, type CSSProperties } from "react";
import { PreviewTaskResultFile } from "../../../wailsjs/go/main/App";
import type { CodePreviewTheme } from "./FileTabBar";
import { MarkdownPreview } from "./CodePreviewMarkdown";
import { isTaskResultPdfPreviewURL, localizeTaskResultPreviewError, type TaskResultPreviewPayload } from "./taskResultPreview";
import { columnWrapsText, previewWorkbookGrids, readPreviewWorkbook, releasePreviewWorkbook, type ParsedWorkbook } from "./xlsxPreviewParse";
import { isPreviewAbortError, readPreviewBody } from "./previewBytes";
import { PREVIEW_BAR_DELAY_MS, PREVIEW_HEAVY_PARSE_BYTES, PreviewLoadingStatus, yieldPreviewPaint, type PreviewProgress } from "./PreviewProgressBar";
import { tailText } from "../../i18n/mnTail"

export { buildSheetGrid, previewWorkbookGrids, readPreviewWorkbook, releasePreviewWorkbook } from "./xlsxPreviewParse";


/** .xlsx and the spreadsheet family the in-app grid renderer can lay out. */
export function isXlsxFileName(name: string): boolean {
    return ["xlsx", "xlsm", "xlsb", "xls", "et", "ett", "ods"].includes(name.toLowerCase().split(".").pop() || "");
}

export interface XlsxPreviewPanelProps {
    /** Local absolute path of the workbook. */
    absPath: string;
    theme: CodePreviewTheme;
    lang: string;
    /** Text extract from the host; shown when the workbook cannot be parsed. */
    fallbackContent?: string;
}

function isNumericCell(cell: string): boolean {
    const trimmed = cell.trim();
    if (trimmed === "") return false;
    if (/^-?\d+(\.\d+)?%?$/.test(trimmed)) return true;
    // Thousands-grouped numbers: 1,234 / 12,345,678 / -1,234.56. "3,4,5" and
    // "1,2" are not valid groupings and stay text (left-aligned).
    return /^-?\d{1,3}(,\d{3})+(\.\d+)?%?$/.test(trimmed);
}

function isXlsxPreviewURL(value: string): boolean {
    // Same short-lived file lease as PDF/Word, served with a spreadsheet type.
    return isTaskResultPdfPreviewURL(value);
}

/**
 * In-app spreadsheet preview. The bytes are the original workbook; SheetJS
 * lays each sheet out as a real grid with sheet tabs, merged-cell spans, and
 * a sticky header row. The text extract used to flatten every cell onto its
 * own line.
 */
export function XlsxPreviewPanel({ absPath, theme, lang, fallbackContent = "" }: XlsxPreviewPanelProps) {
    const isZh = lang.startsWith("zh");
    // Language and the tab extract only change the failure text. They must not
    // download and parse the workbook again.
    const langRef = useRef(lang);
    const fallbackRef = useRef(fallbackContent);
    useEffect(() => {
        langRef.current = lang;
        fallbackRef.current = fallbackContent;
    }, [lang, fallbackContent]);
    const [parsed, setParsed] = useState<ParsedWorkbook | null>(null);
    const [activeSheet, setActiveSheet] = useState(0);
    const [error, setError] = useState("");
    const [fallback, setFallback] = useState("");
    const [loading, setLoading] = useState(true);
    const [progress, setProgress] = useState<PreviewProgress>({ phase: "wait", ratio: null });
    const [barOn, setBarOn] = useState(false);
    const gridScrollRef = useRef<HTMLDivElement | null>(null);

    // A new sheet renders into the same scroll container, so the previous
    // sheet's scroll offset would land the user mid-table otherwise. Effects
    // run after the new rows commit, so resetting here lands at the top-left.
    useEffect(() => {
        const el = gridScrollRef.current;
        if (el) {
            el.scrollTop = 0;
            el.scrollLeft = 0;
        }
    }, [activeSheet]);

    useEffect(() => {
        let cancelled = false;
        const controller = new AbortController();
        let parser: { parse: (bytes: Uint8Array) => Promise<ParsedWorkbook>; stop: () => void } | null = null;
        const zh = () => langRef.current.startsWith("zh");
        const barTimer = window.setTimeout(() => {
            if (!cancelled) setBarOn(true);
        }, PREVIEW_BAR_DELAY_MS);
        const fail = (message: string, extract: string) => {
            if (cancelled) return;
            window.clearTimeout(barTimer);
            if (String(extract || "").trim()) {
                setFallback(extract);
                setError("");
            } else {
                setError(localizeTaskResultPreviewError(message, langRef.current) || message);
            }
            setLoading(false);
        };
        const note = (phase: PreviewProgress["phase"], ratio: number | null) => {
            if (!cancelled) setProgress({ phase, ratio });
        };
        setLoading(true);
        setParsed(null);
        setActiveSheet(0);
        setError("");
        setFallback("");
        setProgress({ phase: "wait", ratio: null });
        setBarOn(false);
        void (async () => {
            // The companion tab does not carry an office extract. The preview
            // call does, for workbooks under the extract ceiling. A parser
            // throw happens after that call returns, so the catch has to keep
            // the server text instead of the empty tab content.
            let extract = fallbackRef.current;
            try {
                const result = await PreviewTaskResultFile(absPath) as TaskResultPreviewPayload;
                if (cancelled) return;
                const previewURL = String(result?.preview_url || "").trim();
                const serverExtract = String(result?.content || "");
                if (serverExtract.trim()) extract = serverExtract;
                if (!isXlsxPreviewURL(previewURL)) {
                    fail(zh() ? "无法预览该表格文件" : "This spreadsheet cannot be previewed", extract);
                    return;
                }
                const response = await fetch(previewURL, { signal: controller.signal });
                if (!response.ok) {
                    fail(zh() ? "无法读取该表格文件" : "This spreadsheet could not be read", extract);
                    return;
                }
                note("read", null);
                const bytes = await readPreviewBody(response, {
                    signal: controller.signal,
                    onProgress: (ratio) => note("read", ratio),
                });
                if (cancelled) return;
                const size = bytes.byteLength;
                const heavy = size >= PREVIEW_HEAVY_PARSE_BYTES;
                const env = import.meta.env as { VITEST?: boolean; MODE?: string };
                const skipWorker = env.VITEST === true || env.MODE === "test";
                if (heavy && !skipWorker) {
                    // SheetJS parses on the thread that calls it. A worker keeps
                    // this pane's progress bar moving and the window responsive.
                    note("parse", null);
                    try {
                        const mod = await import("./xlsxPreviewWorker");
                        if (!cancelled) parser = mod.startXlsxPreviewParser();
                    } catch {
                        parser = null;
                    }
                }
                if (cancelled) return;
                if (parser) {
                    try {
                        const preview = await parser.parse(bytes);
                        if (cancelled) return;
                        if (preview.names.length === 0) {
                            fail(zh() ? "该表格文件没有可预览的工作表" : "This spreadsheet has no sheets to preview", extract);
                            return;
                        }
                        window.clearTimeout(barTimer);
                        setParsed(preview);
                        setLoading(false);
                        return;
                    } catch (err) {
                        if (cancelled || isPreviewAbortError(err)) return;
                        // Bytes move only after the worker is ready. A later
                        // failure has already detached them.
                        if (heavy && bytes.byteLength === 0) throw err;
                    } finally {
                        parser.stop();
                        parser = null;
                    }
                }
                note("parse", null);
                if (heavy) {
                    if (!cancelled) setBarOn(true);
                    await yieldPreviewPaint();
                    if (cancelled) return;
                }
                const XLSX = await import("xlsx");
                if (cancelled) return;
                const wb = readPreviewWorkbook(XLSX, bytes);
                try {
                    if (cancelled) return;
                    const preview = previewWorkbookGrids(XLSX, wb);
                    if (cancelled) return;
                    if (preview.names.length === 0) {
                        fail(zh() ? "该表格文件没有可预览的工作表" : "This spreadsheet has no sheets to preview", extract);
                        return;
                    }
                    window.clearTimeout(barTimer);
                    setParsed(preview);
                    setLoading(false);
                } finally {
                    releasePreviewWorkbook(wb);
                }
            } catch (err) {
                if (cancelled || isPreviewAbortError(err)) return;
                const detail = err instanceof Error ? err.message : String(err || "");
                // Raw parser errors are English ("Unsupported ZIP encryption");
                // keep the detail but lead with a sentence the user can read.
                const technical = /^[A-Za-z]/.test(detail.trim());
                fail(zh() && technical ? `无法解析该表格文件（${detail.trim()}）` : detail, extract);
            }
        })();
        return () => {
            cancelled = true;
            controller.abort();
            window.clearTimeout(barTimer);
            parser?.stop();
        };
    }, [absPath]);

    const activeIndex = parsed ? Math.min(activeSheet, parsed.names.length - 1) : 0;
    const sheetName = parsed?.names[activeIndex] || "";
    const grid = parsed?.grids[activeIndex] ?? null;

    const fill: CSSProperties = {
        display: "flex",
        flexDirection: "column",
        height: "100%",
        width: "100%",
        minHeight: 0,
        // The sheet is wider than the pane. Without this the column flex item
        // grows to the table and the ancestor clips it instead of scrolling.
        minWidth: 0,
        background: theme.bg,
        color: theme.text,
        boxSizing: "border-box",
    };
    if (error) {
        return (
            <div data-testid="xlsx-preview-error" style={{ ...fill, padding: 20, color: theme.diffDeleteText, fontSize: 13, lineHeight: 1.6 }}>
                {error}
            </div>
        );
    }
    if (fallback) {
        return (
            <div data-testid="xlsx-preview-fallback" style={fill} className="ai-chat-scrollbar">
                <MarkdownPreview content={fallback} theme={theme} />
            </div>
        );
    }
    if (loading || !grid) {
        return (
            <PreviewLoadingStatus
                testId="xlsx-preview-loading"
                style={{ ...fill, padding: 20, color: theme.textMuted, fontSize: 13 }}
                phase={progress.phase}
                ratio={progress.ratio}
                showBar={barOn}
                phrases={{
                    wait: tailText(lang, "Loading spreadsheet preview…", "正在加载表格预览…"),
                    read: tailText(lang, "Reading spreadsheet…", "正在读取表格…"),
                    parse: tailText(lang, "Parsing spreadsheet…", "正在解析表格…"),
                }}
            />
        );
    }
    const noticeBits: string[] = [];
    if (grid.truncatedRows > 0) noticeBits.push(tailText(lang, `first ${grid.shownRows} rows`, `仅显示前 ${grid.shownRows} 行`));
    if (grid.truncatedCols > 0) noticeBits.push(tailText(lang, `first ${grid.shownCols} columns`, `仅显示前 ${grid.shownCols} 列`));
    const tableCh = grid.colWidths.reduce((sum, width) => sum + width, 0);
    // One style object per role. A 500×64 sheet must not allocate a style per cell.
    const textCell: CSSProperties = {
        borderRight: `1px solid ${theme.border}`,
        borderBottom: `1px solid ${theme.border}`,
        padding: "4px 1ch",
        boxSizing: "border-box",
        // Fits on one line. `pre` keeps a real newline without offering a
        // wrap point, so a squeezed column cannot stack the glyphs.
        whiteSpace: "pre",
        verticalAlign: "top",
        textAlign: "left",
        fontVariantNumeric: "tabular-nums",
        wordBreak: "normal",
        overflowWrap: "normal",
    };
    const wrapCell: CSSProperties = {
        ...textCell,
        whiteSpace: "pre-wrap",
        // Only past the column cap. `anywhere` would make the min-content
        // width one glyph and hand column sizing back to the pane.
        overflowWrap: "break-word",
    };
    const numberCell: CSSProperties = { ...textCell, textAlign: "right" };
    const wrapNumberCell: CSSProperties = { ...wrapCell, textAlign: "right" };
    const headerCell: CSSProperties = { ...textCell, position: "sticky", top: 0, background: theme.lineNumBg, zIndex: 1 };
    const headerWrapCell: CSSProperties = { ...wrapCell, position: "sticky", top: 0, background: theme.lineNumBg, zIndex: 1 };
    const headerNumberCell: CSSProperties = { ...headerCell, textAlign: "right" };
    const headerWrapNumberCell: CSSProperties = { ...headerWrapCell, textAlign: "right" };
    return (
        <div data-testid="xlsx-preview-panel" style={fill}>
            {parsed!.names.length > 1 ? (
                <div
                    data-testid="xlsx-preview-sheet-tabs"
                    role="tablist"
                    aria-label={tailText(lang, "Sheets", "工作表")}
                    style={{
                        display: "flex",
                        gap: 4,
                        padding: "6px 8px",
                        borderBottom: `1px solid ${theme.border}`,
                        background: theme.tabBg,
                        overflowX: "auto",
                        flexShrink: 0,
                    }}
                >
                    {parsed!.names.map((name, index) => {
                        const active = index === activeIndex;
                        return (
                            <button
                                key={`${index}:${name}`}
                                type="button"
                                role="tab"
                                aria-selected={active}
                                data-testid="xlsx-preview-sheet-tab"
                                data-active={active ? "true" : "false"}
                                onClick={() => setActiveSheet(index)}
                                style={{
                                    border: `1px solid ${active ? theme.tabActiveText : theme.border}`,
                                    borderRadius: 6,
                                    background: active ? theme.tabActiveBg : theme.bg,
                                    color: active ? theme.tabActiveText : theme.text,
                                    padding: "3px 10px",
                                    fontSize: 12,
                                    whiteSpace: "nowrap",
                                    cursor: "pointer",
                                    flexShrink: 0,
                                }}
                            >
                                {name}
                            </button>
                        );
                    })}
                </div>
            ) : null}
            {grid.rows.length === 0 ? (
                <div data-testid="xlsx-preview-empty" style={{ padding: 20, color: theme.textMuted, fontSize: 13 }}>
                    {tailText(lang, `Sheet "${sheetName}" has no content`, `「${sheetName}」没有可显示的内容`)}
                </div>
            ) : (
                <div ref={gridScrollRef} data-testid="xlsx-preview-grid" className="ai-chat-scrollbar" style={{ flex: 1, minHeight: 0, minWidth: 0, overflow: "auto" }}>
                    <table
                        data-testid="xlsx-preview-table"
                        style={{
                            borderCollapse: "separate",
                            borderSpacing: 0,
                            fontSize: 12.5,
                            // Widths are in `ch`. A proportional font makes "m"
                            // wider than "0", and the longest line wraps.
                            fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                            // Fixed to the content widths below. `auto` would size
                            // the table to this scrollport and crush the columns.
                            // min-width keeps a max-width:100% ancestor from
                            // scaling those columns back down to the pane.
                            tableLayout: "fixed",
                            width: tableCh > 0 ? `${tableCh}ch` : "max-content",
                            minWidth: tableCh > 0 ? `${tableCh}ch` : "max-content",
                            maxWidth: "none",
                        }}
                    >
                        <colgroup>
                            {grid.colWidths.map((width, index) => (
                                <col key={index} data-testid="xlsx-preview-col" style={{ width: `${width}ch` }} />
                            ))}
                        </colgroup>
                        <tbody>
                            {grid.rows.map((row, r) => (
                                <tr key={r}>
                                    {row.map((cell, c) => {
                                        const key = `${r}:${c}`;
                                        if (grid.covered.has(key)) return null;
                                        const span = grid.spans.get(key);
                                        const spanCols = span && span.colSpan > 1 ? span.colSpan : 1;
                                        let wrap = false;
                                        const spanEnd = Math.min(grid.colWidths.length, c + spanCols);
                                        for (let i = c; i < spanEnd; i += 1) {
                                            if (columnWrapsText(grid.colWidths[i])) {
                                                wrap = true;
                                                break;
                                            }
                                        }
                                        const numeric = isNumericCell(cell);
                                        const style = r === 0
                                            ? (numeric ? (wrap ? headerWrapNumberCell : headerNumberCell) : (wrap ? headerWrapCell : headerCell))
                                            : (numeric ? (wrap ? wrapNumberCell : numberCell) : (wrap ? wrapCell : textCell));
                                        return (
                                            <td
                                                key={c}
                                                rowSpan={span && span.rowSpan > 1 ? span.rowSpan : undefined}
                                                colSpan={span && span.colSpan > 1 ? span.colSpan : undefined}
                                                style={style}
                                            >
                                                {cell}
                                            </td>
                                        );
                                    })}
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
            {noticeBits.length > 0 ? (
                <div
                    data-testid="xlsx-preview-truncated"
                    style={{
                        padding: "4px 10px",
                        borderTop: `1px solid ${theme.border}`,
                        color: theme.textMuted,
                        fontSize: 11.5,
                        flexShrink: 0,
                    }}
                >
                    {tailText(lang, `Showing ${noticeBits.join(", ")} — open in Excel for the full content`, `${noticeBits.join("、")}，完整内容请用 Excel 打开`)}
                </div>
            ) : null}
        </div>
    );
}
