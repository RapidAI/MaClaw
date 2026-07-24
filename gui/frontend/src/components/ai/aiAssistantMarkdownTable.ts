export type MarkdownTableAlignment = "left" | "center" | "right";

export interface MarkdownTableModel {
    headerCells: string[];
    bodyRows: string[];
    columnAlignments: MarkdownTableAlignment[];
    minTableWidth: number;
}

export interface MarkdownTableRenderModel extends MarkdownTableModel {
    prefix: string;
    notes: string[];
}

/**
 * Digital employees sometimes emit table rows as list items, e.g.
 *   - | 今天 | 晴 |
 *   - 20日 (周一) | 雷阵雨转多云
 *   1. | tomorrow | rain |
 * Strip a single leading list marker when the remainder is still a pipe row.
 * Display-only normalize — safe to call more than once (idempotent for table lines).
 */
// ASCII list markers + digital-employee bullets (U+2022 •, U+00B7 ·).
// Escapes keep source ASCII-safe (same policy as heading-list strip).
const TABLE_LINE_LIST_MARKER_RE = /^(?:[-*+]|\u2022|\u00b7|\d+[.)])\s+(\S.*)$/;

export function normalizeMarkdownTableLine(line: string): string {
    const trimmed = line.trim();
    // -, *, +, •, ·, or ordered "1." / "1)"
    const listMatch = trimmed.match(TABLE_LINE_LIST_MARKER_RE);
    if (!listMatch) return line;
    const rest = listMatch[1];
    if (!rest.includes("|")) return line;
    if (rest.startsWith("|") && rest.length > 1) return rest;
    if (parseMarkdownTableCells(rest).length >= 2) return rest;
    return line;
}

export function isMarkdownTableRow(line: string): boolean {
    const trimmed = normalizeMarkdownTableLine(line).trim();
    if (trimmed.startsWith("|") && trimmed.length > 1) return true;
    if (!trimmed.includes("|")) return false;
    return parseMarkdownTableCells(trimmed).length >= 2;
}

export function isMarkdownTableSeparatorRow(line: string): boolean {
    const trimmed = normalizeMarkdownTableLine(line).trim().replace(/^\|/, "").replace(/\|$/, "");
    return /^[\s|:\-]+$/.test(trimmed) && trimmed.includes("-");
}

export function buildMarkdownTableModel(tableLines: string[]): MarkdownTableModel | null {
    // Normalize once at the model boundary so list-prefixed rows and callers
    // that skip pre-normalize still build a consistent table.
    const lines = tableLines.map(normalizeMarkdownTableLine);
    const dataRows = lines.filter(line => !isMarkdownTableSeparatorRow(line));
    if (lines.length < 2 || dataRows.length === 0) return null;
    const hasSeparator = lines.some(isMarkdownTableSeparatorRow);
    const allRowsUseOuterPipes = lines.every(line => line.trim().startsWith("|"));
    // Digital-employee weather tables often ship a pipe header without a GFM
    // separator, then body rows that omit the leading "|":
    //   | 日期 | 天气 | 温度 | 风力 |
    //   今天 (24日) | 雷阵雨转多云
    //   →| 30°C / 23°C | <3级 |
    let headerCells = parseMarkdownTableCells(dataRows[0]);
    const headerLed = dataRows[0].trim().startsWith("|") && headerCells.length >= 2;
    if (!hasSeparator && !allRowsUseOuterPipes && !headerLed) return null;
    const separatorLine = lines.find(isMarkdownTableSeparatorRow) || "";
    const separatorCells = parseMarkdownTableCells(separatorLine);
    if (headerCells.length === 1 && separatorCells.length >= 2) headerCells = [headerCells[0], ""];
    if (headerCells.length < 2) return null;
    const bodyRows = dataRows.slice(1);
    const columnCount = Math.max(headerCells.length, separatorCells.length, ...bodyRows.map(row => parseMarkdownTableCells(row).length));
    if (columnCount > headerCells.length) headerCells = [...headerCells, ...Array(columnCount - headerCells.length).fill("")];
    return { headerCells, bodyRows, columnAlignments: parseMarkdownTableAlignments(separatorLine, headerCells.length), minTableWidth: Math.max(360, headerCells.length * 120) };
}

export function parseMarkdownTableCells(line: string): string[] {
    let trimmed = line.trim();
    if (trimmed.startsWith("|")) trimmed = trimmed.slice(1);
    if (hasUnescapedTrailingPipe(trimmed)) trimmed = trimmed.slice(0, -1);

    const cells: string[] = [];
    let cell = "";
    let escaped = false;
    for (const char of trimmed) {
        if (escaped) {
            cell += char === "|" ? "|" : `\\${char}`;
            escaped = false;
        } else if (char === "\\") {
            escaped = true;
        } else if (char === "|") {
            cells.push(cell.trim());
            cell = "";
        } else {
            cell += char;
        }
    }
    cells.push((escaped ? `${cell}\\` : cell).trim());
    return cells;
}

export function parseMarkdownTableAlignments(separatorLine: string, columnCount: number): MarkdownTableAlignment[] {
    const cells = parseMarkdownTableCells(separatorLine);
    return Array.from({ length: columnCount }, (_, index) => parseAlignmentCell(cells[index] || ""));
}

export function repairMixedNarrativeTable(model: MarkdownTableModel): MarkdownTableRenderModel {
    const normalized = repairSplitTableRows(model);
    const fallback: MarkdownTableRenderModel = { ...normalized, prefix: "", notes: [] };
    if (normalized.headerCells.length < 3) return fallback;
    const [firstHeader, ...restHeaders] = normalized.headerCells;
    if (!looksLikeNarrativeTablePrefix(firstHeader, restHeaders)) return fallback;

    const visibleColumnCount = restHeaders.length;
    const notes: string[] = [];
    const bodyRows = normalized.bodyRows.map((row) => {
        const cells = parseMarkdownTableCells(row);
        const visible = cells.slice(0, visibleColumnCount);
        const extra = cells.slice(visibleColumnCount).map(cell => cell.trim()).filter(Boolean).join(" ");
        if (extra) notes.push(extra);
        return `| ${visible.map(escapeMarkdownTableCell).join(" | ")} |`;
    });

    return {
        headerCells: restHeaders,
        bodyRows,
        columnAlignments: normalized.columnAlignments.slice(1, 1 + visibleColumnCount),
        minTableWidth: Math.max(280, visibleColumnCount * 120),
        prefix: firstHeader,
        notes,
    };
}

/**
 * Some streamed answers split one logical table row across two lines. Common
 * shapes (for N >= 3 columns):
 *   1) label alone, then the remaining N-1 cells
 *      |今天|
 *      |阴转雷阵雨 | 24~29°C | 东风 1-3级|
 *   2) first k cells (1 < k < N), then a continuation line with the rest,
 *      often prefixed by a marker cell such as "→" or "-"
 *      |今天 (14日) | 多云转晴 |
 *      | → | 34°C / 22°C | <3级 |
 * Neither is valid Markdown; both are unambiguous enough to rejoin.
 */
function repairSplitTableRows(model: MarkdownTableModel): MarkdownTableModel {
    const columnCount = model.headerCells.length;
    if (columnCount < 3) return model;

    let repairedRows: string[] | null = null;
    for (let index = 0; index < model.bodyRows.length; index++) {
        const merged = tryMergeSplitTableRow(model.bodyRows[index], model.bodyRows[index + 1], columnCount);
        if (merged) {
            repairedRows ??= model.bodyRows.slice(0, index);
            repairedRows.push(merged);
            index++;
        } else if (repairedRows) {
            repairedRows.push(model.bodyRows[index]);
        }
    }
    return repairedRows ? { ...model, bodyRows: repairedRows } : model;
}

/**
 * Pure wrap glyphs (not real data). Keep this list tight: only tokens that
 * signal "row continues", never standalone values like temperatures or codes.
 */
function isTableRowContinuationGlyph(cell: string): boolean {
    // Keep glyph classes as escapes so this file stays ASCII-safe under any codepage.
    // \u2192 →, \u21d2 ⇒, \u2026 …, \u2014 —, \u2013 –, \u00b7 ·, \u2022 •
    return /^(?:\u2192|->|=>|\u21d2|\u2026|\.{2,3}|\u2014|\u2013|-|~|\u00b7|\u2022)$/.test(cell.trim());
}

/** Drop trailing empty cells so "| a | b | |" counts as two data cells. */
function trimTrailingEmptyCells(cells: string[]): string[] {
    let end = cells.length;
    while (end > 0 && !cells[end - 1].trim()) end--;
    return end === cells.length ? cells : cells.slice(0, end);
}

/**
 * If `line` is a partial row and `nextLine` holds the remainder (optionally
 * with a leading continuation marker), return a single joined markdown row.
 */
function tryMergeSplitTableRow(line: string, nextLine: string | undefined, columnCount: number): string | null {
    if (!nextLine) return null;

    // Lines are usually pre-normalized; normalize again so direct callers of
    // buildMarkdownTableModel / repair still work with list-prefixed input.
    const normalizedLeft = normalizeMarkdownTableLine(line);
    const normalizedNext = normalizeMarkdownTableLine(nextLine);
    const left = trimTrailingEmptyCells(parseMarkdownTableCells(normalizedLeft));
    if (left.length === 0 || left.length >= columnCount) return null;

    const nextCells = parseMarkdownTableCells(normalizedNext);
    if (nextCells.length === 0) return null;

    // Skip leading blanks (column padding) and pure wrap glyphs ("→", "-").
    // Only a real glyph unlocks k+(N-k) merges; blanks alone stay classic-only
    // so "| a | b |" + "|  | c | d |" does not get glued.
    let restStart = 0;
    let sawGlyphMarker = false;
    while (restStart < nextCells.length) {
        const cell = nextCells[restStart];
        if (!cell.trim()) {
            restStart++;
            continue;
        }
        if (isTableRowContinuationGlyph(cell)) {
            sawGlyphMarker = true;
            restStart++;
            continue;
        }
        break;
    }
    const rest = trimTrailingEmptyCells(nextCells.slice(restStart));
    if (rest.length === 0 || rest.length >= columnCount) return null;
    if (left.length + rest.length !== columnCount) return null;

    if (!sawGlyphMarker) {
        // Classic streamed form: one label cell, then exactly the remaining N-1 cells.
        const classicSplit = left.length === 1 && rest.length === columnCount - 1;
        // Weather-style without wrap glyph: multi-cell partial row omits leading
        // "|", continuation is a proper pipe row with the remaining cells.
        //   周一 (27日) | 多云
        //   | 33°C / 25°C | <3级 |
        // left.length >= 2 avoids overlapping classic 1+(N-1). Both-piped short
        // rows ("| a | 1 |" + "| b | 2 |") stay unmerged.
        const weatherSplit =
            left.length >= 2
            && !normalizedLeft.trim().startsWith("|")
            && normalizedNext.trim().startsWith("|");
        if (!classicSplit && !weatherSplit) return null;
    }

    return `| ${[...left, ...rest].map(escapeMarkdownTableCell).join(" | ")} |`;
}

function escapeMarkdownTableCell(cell: string): string {
    // parseMarkdownTableCells preserves ordinary backslashes, so re-escaping
    // them here would make paths display with doubled separators. Only escape
    // a pipe because it would otherwise become a column delimiter.
    return cell.replace(/\|/g, "\\|");
}

function looksLikeNarrativeTablePrefix(firstHeader: string, restHeaders: string[]): boolean {
    const prefix = firstHeader.trim();
    if (prefix.length < 18) return false;
    const compactRest = restHeaders.map(cell => cell.trim()).filter(Boolean);
    if (compactRest.length < 2 || compactRest.some(cell => cell.length > 24)) return false;
    const averageRestLength = compactRest.reduce((sum, cell) => sum + cell.length, 0) / compactRest.length;
    if (prefix.length < Math.max(18, averageRestLength * 2.5)) return false;
    return looksLikeProse(prefix);
}

function looksLikeProse(text: string): boolean {
    const cjkCount = (text.match(/[\u3400-\u9FFF]/g) || []).length;
    const wordCount = (text.match(/[A-Za-z0-9]+/g) || []).length;
    const hasSentencePunctuation = /[\uFF0C\u3001\u3002\uFF01\uFF1F,;.!?~]/.test(text);
    return hasSentencePunctuation || cjkCount >= 14 || wordCount >= 6;
}

function parseAlignmentCell(cell: string): MarkdownTableAlignment {
    const marker = cell.trim();
    const left = marker.startsWith(":");
    const right = marker.endsWith(":");
    if (left && right) return "center";
    if (right) return "right";
    return "left";
}

function hasUnescapedTrailingPipe(value: string): boolean {
    if (!value.endsWith("|")) return false;
    let slashCount = 0;
    for (let i = value.length - 2; i >= 0 && value[i] === "\\"; i--) slashCount++;
    return slashCount % 2 === 0;
}
