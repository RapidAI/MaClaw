import { useEffect, useMemo, useRef, useState, type CSSProperties } from "react";
import { PreviewTaskResultFile } from "../../../wailsjs/go/main/App";
import type { CodePreviewTheme } from "./FileTabBar";
import { MarkdownPreview } from "./CodePreviewMarkdown";
import { isTaskResultPdfPreviewURL, localizeTaskResultPreviewError, type TaskResultPreviewPayload } from "./taskResultPreview";

type XLSXModule = typeof import("xlsx");

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

/** Cap the rendered grid so a whole-column format cannot freeze the pane. */
const MAX_PREVIEW_ROWS = 500;
const MAX_PREVIEW_COLS = 64;

type SheetGrid = {
    rows: string[][];
    /** Merge start → span; covered cells are skipped while rendering. */
    spans: Map<string, { rowSpan: number; colSpan: number }>;
    covered: Set<string>;
    colCount: number;
    /** Rows/cols requested from the sheet after clamping to the caps. */
    shownRows: number;
    shownCols: number;
    truncatedRows: number;
    truncatedCols: number;
};

type ParsedWorkbook = {
    XLSX: XLSXModule;
    wb: import("xlsx").WorkBook;
    names: string[];
};

/**
 * Build the visible grid from a worksheet. The read range is clamped to the
 * preview caps first so `sheet_to_json` never materializes a 1M-row array;
 * merged cells collapse into row/col spans on their top-left cell.
 */
export function buildSheetGrid(XLSX: XLSXModule, sheet: import("xlsx").WorkSheet): SheetGrid {
    const empty: SheetGrid = { rows: [], spans: new Map(), covered: new Set(), colCount: 0, shownRows: 0, shownCols: 0, truncatedRows: 0, truncatedCols: 0 };
    if (!sheet || !sheet["!ref"]) return empty;
    const full = XLSX.utils.decode_range(sheet["!ref"]);
    const totalRows = full.e.r - full.s.r + 1;
    const totalCols = full.e.c - full.s.c + 1;
    if (totalRows <= 0 || totalCols <= 0) return empty;
    const range = {
        s: { r: full.s.r, c: full.s.c },
        e: {
            r: Math.min(full.e.r, full.s.r + MAX_PREVIEW_ROWS - 1),
            c: Math.min(full.e.c, full.s.c + MAX_PREVIEW_COLS - 1),
        },
    };
    const shownRows = range.e.r - range.s.r + 1;
    const shownCols = range.e.c - range.s.c + 1;
    const raw = XLSX.utils.sheet_to_json<string[]>(sheet, {
        header: 1,
        defval: "",
        raw: false,
        range,
    }) as string[][];
    const width = range.e.c - range.s.c + 1;
    const rows = raw.map((row) => {
        const next = Array.isArray(row) ? row.map((cell) => String(cell ?? "")) : [];
        while (next.length < width) next.push("");
        return next;
    });
    // Template files often declare far more formatted-but-empty rows and
    // columns than they fill; trailing blanks read as a broken preview. The
    // content extent covers merge ranges too, so a wide merged title keeps
    // its span instead of being clipped to the last text cell.
    const merges = Array.isArray(sheet["!merges"]) ? sheet["!merges"] : [];
    let lastRow = -1;
    let lastCol = -1;
    for (let r = 0; r < rows.length; r += 1) {
        const row = rows[r];
        for (let c = row.length - 1; c > lastCol; c -= 1) {
            if (row[c] !== "") {
                lastCol = Math.max(lastCol, c);
                break;
            }
        }
        if (row.some((cell) => cell !== "")) lastRow = Math.max(lastRow, r);
    }
    for (const merge of merges) {
        if (merge.e.r < range.s.r || merge.e.c < range.s.c) continue;
        if (merge.s.r > range.e.r || merge.s.c > range.e.c) continue;
        lastRow = Math.max(lastRow, Math.min(merge.e.r, range.e.r) - range.s.r);
        lastCol = Math.max(lastCol, Math.min(merge.e.c, range.e.c) - range.s.c);
    }
    if (lastRow >= 0) rows.length = lastRow + 1;
    else rows.length = 0;
    const colCount = lastCol + 1;
    for (let r = 0; r < rows.length; r += 1) rows[r] = rows[r].slice(0, colCount);
    const spans = new Map<string, { rowSpan: number; colSpan: number }>();
    const covered = new Set<string>();
    for (const merge of merges) {
        const startR = merge.s.r - range.s.r;
        const startC = merge.s.c - range.s.c;
        const endR = merge.e.r - range.s.r;
        const endC = merge.e.c - range.s.c;
        if (endR < 0 || endC < 0 || startR >= rows.length || startC >= colCount) continue;
        const clipR = Math.min(endR, rows.length - 1);
        const clipC = Math.min(endC, colCount - 1);
        if (startR >= 0 && startC >= 0) {
            spans.set(`${startR}:${startC}`, { rowSpan: clipR - startR + 1, colSpan: clipC - startC + 1 });
        }
        for (let r = Math.max(0, startR); r <= clipR; r += 1) {
            for (let c = Math.max(0, startC); c <= clipC; c += 1) {
                if (r === startR && c === startC) continue;
                covered.add(`${r}:${c}`);
            }
        }
    }
    return {
        rows,
        spans,
        covered,
        colCount,
        // Truncation counts what the caps cut off the declared range, not the
        // trailing blanks trimmed above — those are merely empty, not hidden
        // content.
        shownRows,
        shownCols,
        truncatedRows: Math.max(0, totalRows - shownRows),
        truncatedCols: Math.max(0, totalCols - shownCols),
    };
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
    const [parsed, setParsed] = useState<ParsedWorkbook | null>(null);
    const [activeSheet, setActiveSheet] = useState(0);
    const [error, setError] = useState("");
    const [fallback, setFallback] = useState("");
    const [loading, setLoading] = useState(true);
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
        const fail = (message: string, extract: string) => {
            if (cancelled) return;
            if (String(extract || "").trim()) {
                setFallback(extract);
                setError("");
            } else {
                setError(localizeTaskResultPreviewError(message, lang) || message);
            }
            setLoading(false);
        };
        setLoading(true);
        setParsed(null);
        setActiveSheet(0);
        setError("");
        setFallback("");
        void (async () => {
            try {
                const result = await PreviewTaskResultFile(absPath) as TaskResultPreviewPayload;
                if (cancelled) return;
                const previewURL = String(result?.preview_url || "").trim();
                const extract = String(result?.content || "");
                if (!isXlsxPreviewURL(previewURL)) {
                    fail(isZh ? "无法预览该表格文件" : "This spreadsheet cannot be previewed", extract);
                    return;
                }
                const response = await fetch(previewURL);
                if (!response.ok) {
                    fail(isZh ? "无法读取该表格文件" : "This spreadsheet could not be read", extract);
                    return;
                }
                const bytes = await response.arrayBuffer();
                if (cancelled) return;
                const XLSX = await import("xlsx");
                if (cancelled) return;
                const wb = XLSX.read(new Uint8Array(bytes), { type: "array" });
                const names = wb.SheetNames.filter((name) => Boolean(wb.Sheets[name]));
                if (names.length === 0) {
                    fail(isZh ? "该表格文件没有可预览的工作表" : "This spreadsheet has no sheets to preview", extract);
                    return;
                }
                setParsed({ XLSX, wb, names });
                setLoading(false);
            } catch (err) {
                const detail = err instanceof Error ? err.message : String(err || "");
                // Raw parser errors are English ("Unsupported ZIP encryption");
                // keep the detail but lead with a sentence the user can read.
                const technical = /^[A-Za-z]/.test(detail.trim());
                fail(isZh && technical ? `无法解析该表格文件（${detail.trim()}）` : detail, fallbackContent);
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [absPath, fallbackContent, isZh]);

    const sheetName = parsed ? parsed.names[Math.min(activeSheet, parsed.names.length - 1)] || "" : "";
    const grid = useMemo<SheetGrid | null>(() => {
        if (!parsed || !sheetName) return null;
        try {
            return buildSheetGrid(parsed.XLSX, parsed.wb.Sheets[sheetName]);
        } catch {
            // One malformed sheet must not blank the whole preview.
            return { rows: [], spans: new Map(), covered: new Set(), colCount: 0, shownRows: 0, shownCols: 0, truncatedRows: 0, truncatedCols: 0 };
        }
    }, [parsed, sheetName]);

    const fill: CSSProperties = {
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
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
            <div data-testid="xlsx-preview-loading" role="status" style={{ ...fill, padding: 20, color: theme.textMuted, fontSize: 13 }}>
                {isZh ? "正在加载表格预览…" : "Loading spreadsheet preview…"}
            </div>
        );
    }
    const activeIndex = Math.min(activeSheet, parsed!.names.length - 1);
    const noticeBits: string[] = [];
    if (grid.truncatedRows > 0) noticeBits.push(isZh ? `仅显示前 ${grid.shownRows} 行` : `first ${grid.shownRows} rows`);
    if (grid.truncatedCols > 0) noticeBits.push(isZh ? `仅显示前 ${grid.shownCols} 列` : `first ${grid.shownCols} columns`);
    return (
        <div data-testid="xlsx-preview-panel" style={fill}>
            {parsed!.names.length > 1 ? (
                <div
                    data-testid="xlsx-preview-sheet-tabs"
                    role="tablist"
                    aria-label={isZh ? "工作表" : "Sheets"}
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
                    {isZh ? `「${sheetName}」没有可显示的内容` : `Sheet "${sheetName}" has no content`}
                </div>
            ) : (
                <div ref={gridScrollRef} data-testid="xlsx-preview-grid" className="ai-chat-scrollbar" style={{ flex: 1, minHeight: 0, overflow: "auto" }}>
                    <table
                        data-testid="xlsx-preview-table"
                        style={{ borderCollapse: "separate", borderSpacing: 0, fontSize: 12.5, tableLayout: "auto" }}
                    >
                        <tbody>
                            {grid.rows.map((row, r) => (
                                <tr key={r}>
                                    {row.map((cell, c) => {
                                        const key = `${r}:${c}`;
                                        if (grid.covered.has(key)) return null;
                                        const span = grid.spans.get(key);
                                        const style: CSSProperties = {
                                            borderRight: `1px solid ${theme.border}`,
                                            borderBottom: `1px solid ${theme.border}`,
                                            padding: "4px 8px",
                                            whiteSpace: "pre-wrap",
                                            verticalAlign: "top",
                                            textAlign: isNumericCell(cell) ? "right" : "left",
                                            fontVariantNumeric: "tabular-nums",
                                            maxWidth: 360,
                                            overflowWrap: "anywhere",
                                        };
                                        if (r === 0) {
                                            style.position = "sticky";
                                            style.top = 0;
                                            style.background = theme.lineNumBg;
                                            style.zIndex = 1;
                                        }
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
                    {isZh
                        ? `${noticeBits.join("、")}，完整内容请用 Excel 打开`
                        : `Showing ${noticeBits.join(", ")} — open in Excel for the full content`}
                </div>
            ) : null}
        </div>
    );
}
