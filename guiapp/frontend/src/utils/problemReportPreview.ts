/** Characters kept in the submitted-report row. CSS ellipsis clips this to the line. */
export const problemReportDescriptionPreviewLimit = 80;

/** Hover text stays short so a 32KB report body never becomes a tooltip. */
export const problemReportDescriptionTitleLimit = 240;

export interface ProblemReportDescriptionPreview {
    text: string;
    title: string;
}

function cutCodePoints(chars: string[], limit: number): string {
    if (chars.length <= limit) return chars.join('');
    return `${chars.slice(0, limit).join('').trimEnd()}…`;
}

const problemReportKindLine = /^【报告类型】(缺陷|新功能)$/;

function problemReportKindParts(description: unknown): { kind: string; body: string } {
    const text = String(description ?? '').replace(/\r\n/g, '\n');
    const newline = text.indexOf('\n');
    const line = (newline < 0 ? text : text.slice(0, newline)).trim();
    const match = problemReportKindLine.exec(line);
    if (!match) return { kind: '', body: String(description ?? '') };
    const body = newline < 0 ? '' : text.slice(newline + 1).trim();
    return { kind: match[1], body };
}

/** Stored type line, or "" for a report saved before types existed. */
export function problemReportStoredKind(description: unknown): string {
    return problemReportKindParts(description).kind;
}

/** Description without the stored type line. */
export function problemReportUserDescription(description: unknown): string {
    return problemReportKindParts(description).body;
}

/** Leading description excerpt for the submitted-report list. */
export function problemReportDescriptionPreview(description: unknown): ProblemReportDescriptionPreview {
    const collapsed = String(description ?? '').replace(/\s+/g, ' ').trim();
    if (!collapsed) return { text: '', title: '' };
    const chars = Array.from(collapsed);
    if (chars.length <= problemReportDescriptionPreviewLimit) return { text: collapsed, title: '' };
    return {
        text: cutCodePoints(chars, problemReportDescriptionPreviewLimit),
        title: cutCodePoints(chars, problemReportDescriptionTitleLimit),
    };
}
