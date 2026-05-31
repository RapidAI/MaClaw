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

export function isMarkdownTableRow(line: string): boolean {
    const trimmed = line.trim();
    if (trimmed.startsWith("|") && trimmed.length > 1) return true;
    if (!trimmed.includes("|")) return false;
    return parseMarkdownTableCells(trimmed).length >= 2;
}

export function isMarkdownTableSeparatorRow(line: string): boolean {
    const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
    return /^[\s|:\-]+$/.test(trimmed) && trimmed.includes("-");
}

export function buildMarkdownTableModel(tableLines: string[]): MarkdownTableModel | null {
    const dataRows = tableLines.filter(line => !isMarkdownTableSeparatorRow(line));
    if (tableLines.length < 2 || dataRows.length === 0) return null;
    const hasSeparator = tableLines.some(isMarkdownTableSeparatorRow);
    const allRowsUseOuterPipes = tableLines.every(line => line.trim().startsWith("|"));
    if (!hasSeparator && !allRowsUseOuterPipes) return null;
    let headerCells = parseMarkdownTableCells(dataRows[0]);
    const separatorLine = tableLines.find(isMarkdownTableSeparatorRow) || "";
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
    const fallback: MarkdownTableRenderModel = { ...model, prefix: "", notes: [] };
    if (model.headerCells.length < 3) return fallback;
    const [firstHeader, ...restHeaders] = model.headerCells;
    if (!looksLikeNarrativeTablePrefix(firstHeader, restHeaders)) return fallback;

    const visibleColumnCount = restHeaders.length;
    const notes: string[] = [];
    const bodyRows = model.bodyRows.map((row) => {
        const cells = parseMarkdownTableCells(row);
        const visible = cells.slice(0, visibleColumnCount);
        const extra = cells.slice(visibleColumnCount).map(cell => cell.trim()).filter(Boolean).join(" ");
        if (extra) notes.push(extra);
        return `| ${visible.map(escapeMarkdownTableCell).join(" | ")} |`;
    });

    return {
        headerCells: restHeaders,
        bodyRows,
        columnAlignments: model.columnAlignments.slice(1, 1 + visibleColumnCount),
        minTableWidth: Math.max(280, visibleColumnCount * 120),
        prefix: firstHeader,
        notes,
    };
}

function escapeMarkdownTableCell(cell: string): string {
    return cell.replace(/\\/g, "\\\\").replace(/\|/g, "\\|");
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
