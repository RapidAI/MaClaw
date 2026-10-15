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
