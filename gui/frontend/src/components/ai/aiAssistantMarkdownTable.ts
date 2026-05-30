export type MarkdownTableAlignment = "left" | "center" | "right";

export interface MarkdownTableModel {
    headerCells: string[];
    bodyRows: string[];
    columnAlignments: MarkdownTableAlignment[];
    minTableWidth: number;
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
