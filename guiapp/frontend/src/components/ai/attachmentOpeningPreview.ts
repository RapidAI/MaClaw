/**
 * How many opening pages an assistant attachment preview shows.
 *
 * The first page is always included. While that page has little text, the next
 * page is included too, and this repeats. A safety cap stops a run of blank
 * pages from growing without a bound.
 */

/** Non-whitespace runes below this count as "little text". */
export const ATTACHMENT_PREVIEW_SPARSE_RUNES = 80;

/** Safety cap for the sparse-page walk. */
export const ATTACHMENT_PREVIEW_MAX_PAGES = 8;

/**
 * Reading-size card. The assistant composer does not mount it. A chat shot
 * fits one whole page into the quarter-size box instead of cropping this card.
 */
export const ATTACHMENT_DOC_PREVIEW_MAX_WIDTH = 640;
export const ATTACHMENT_DOC_PREVIEW_MAX_HEIGHT = 480;

/**
 * Chat transcript shot. One quarter of the reading card on each side.
 * One whole page is fitted inside this box.
 */
export const ATTACHMENT_DOC_SHOT_SCALE = 0.25;
export const ATTACHMENT_DOC_SHOT_WIDTH = ATTACHMENT_DOC_PREVIEW_MAX_WIDTH * ATTACHMENT_DOC_SHOT_SCALE;
export const ATTACHMENT_DOC_SHOT_HEIGHT = ATTACHMENT_DOC_PREVIEW_MAX_HEIGHT * ATTACHMENT_DOC_SHOT_SCALE;

/** Lines that make one virtual page for text, source, and extracts. */
export const ATTACHMENT_PREVIEW_TEXT_PAGE_LINES = 28;

export function attachmentPreviewRuneCount(text: string): number {
    let count = 0;
    for (const char of String(text || "")) {
        if (!/\s/u.test(char)) count += 1;
    }
    return count;
}

export function attachmentPreviewPageIsSparse(text: string): boolean {
    return attachmentPreviewRuneCount(text) < ATTACHMENT_PREVIEW_SPARSE_RUNES;
}

/**
 * Leading pages to show. Always at least the first page. Each sparse page
 * pulls in the next one, including the first page that is no longer sparse.
 */
export function openingPreviewPageCount(pageTexts: readonly string[]): number {
    if (pageTexts.length === 0) return 0;
    let count = 1;
    const limit = Math.min(pageTexts.length, ATTACHMENT_PREVIEW_MAX_PAGES);
    while (count < limit && attachmentPreviewPageIsSparse(pageTexts[count - 1])) {
        count += 1;
    }
    return count;
}

/** Split a text extract into virtual pages. A form feed starts a new page. */
export function splitTextIntoPreviewPages(text: string): string[] {
    const normalized = String(text || "").replace(/\r\n/g, "\n").replace(/\r/g, "\n");
    const chunks = normalized.split("\f");
    const pages: string[] = [];
    for (const chunk of chunks) {
        const lines = chunk.split("\n");
        if (lines.length === 1 && lines[0] === "" && chunks.length > 1) {
            pages.push("");
            continue;
        }
        for (let i = 0; i < lines.length; i += ATTACHMENT_PREVIEW_TEXT_PAGE_LINES) {
            pages.push(lines.slice(i, i + ATTACHMENT_PREVIEW_TEXT_PAGE_LINES).join("\n"));
        }
    }
    while (pages.length > 1 && attachmentPreviewRuneCount(pages[pages.length - 1]) === 0) {
        pages.pop();
    }
    return pages.length > 0 ? pages : [""];
}

export function openingPreviewTexts(pageTexts: readonly string[]): string[] {
    return pageTexts.slice(0, openingPreviewPageCount(pageTexts));
}

/**
 * 0-based index of the page a chat shot shows.
 * Leading pages with little text are covers. The shot is the first page
 * with enough text. When every page is sparse, the shot stays on page 1.
 */
export function openingPreviewShotPageIndex(pageTexts: readonly string[]): number {
    const limit = Math.min(pageTexts.length, ATTACHMENT_PREVIEW_MAX_PAGES);
    for (let index = 0; index < limit; index += 1) {
        if (!attachmentPreviewPageIsSparse(pageTexts[index] || "")) return index;
    }
    return 0;
}
