/**
 * Shared ordered-list line parsing + marker column layout for assistant / remote markdown.
 * Keep pure (no React runtime deps) so RemoteSessionConsole can import without cycles.
 */

/**
 * Line-start ordered item: "10. body" / "10) body" / bare "10." (streaming).
 * Body is optional so incomplete stream frames still parse as list rows.
 */
export const ORDERED_LIST_ITEM_LINE = /^(\d+)([.)])(?:\s+(.*))?$/;

/** Count display columns in a pure indent string (tab = 4). */
function indentColumnsOf(indentText: string): number {
    let cols = 0;
    for (let i = 0; i < indentText.length; i++) {
        cols += indentText.charCodeAt(i) === 0x09 ? 4 : 1;
    }
    return cols;
}

/** Leading whitespace substring (spaces/tabs only). */
export function leadingIndentText(line: string): string {
    let i = 0;
    while (i < line.length) {
        const ch = line.charCodeAt(i);
        if (ch === 0x20 || ch === 0x09) i++;
        else break;
    }
    return i > 0 ? line.slice(0, i) : "";
}

/** Leading spaces/tabs as display columns (tab = 4). */
export function leadingIndentColumns(line: string): number {
    return indentColumnsOf(leadingIndentText(line));
}

export type ParsedOrderedListLine = {
    indentCols: number;
    /** Original leading whitespace (preserve tabs vs spaces). */
    indentText: string;
    /** e.g. "10." or "10)" */
    marker: string;
    body: string;
};

/** Parse an ordered-list line, preserving leading indent for nested display. */
export function parseOrderedListLine(line: string): ParsedOrderedListLine | null {
    const indentText = leadingIndentText(line);
    const rest = indentText ? line.slice(indentText.length) : line;
    const m = rest.match(ORDERED_LIST_ITEM_LINE);
    if (!m) return null;
    return {
        indentCols: indentColumnsOf(indentText),
        indentText,
        marker: m[1] + m[2],
        body: m[3] ?? "",
    };
}

/** Nested list padding from leading whitespace (capped). */
export function orderedListIndentPadding(indentCols: number): string | undefined {
    if (indentCols <= 0) return undefined;
    const capped = Math.min(indentCols, 32);
    return `${capped * 0.5}em`;
}

/**
 * Mid-line ordered markers glued to preceding text (no intervening space).
 * Preceding class excludes:
 * - digits: multi-digit "10." must not become "1" + "0."
 * - ".": version decimals "v2.0. 3." must not peel into "0."
 * - currency symbols: "$10. 00" must not break after "$"
 */
const MID_LINE_ORDERED_GLUED =
    /([^\n\s\d.$€£¥￥])(\d+[.)]\s+)/g;

/**
 * Sentence / closer punctuation, then horizontal space, then an ordered marker.
 * Handles "完成。 1. 项" and "如下： 10. 标题" without touching bare "摘要 10. 详情".
 */
const MID_LINE_ORDERED_AFTER_PUNCT_SPACE =
    /([。！？；：、!?;:）)\]】」』…])[ \t]+(\d+[.)]\s+)/g;

/**
 * Ordered marker whose body does not start with a digit.
 * Avoids treating "2. 5 yuan" as an ordered item.
 * Used only via String#matchAll (starts from index 0 each call).
 */
const ORDERED_MARKER_WITH_NON_DIGIT_BODY =
    /\d+[.)]\s+(?=[^\d\s\n])/g;

/**
 * On a line that already starts as an ordered item, later " 2. …" / " 10. …"
 * items are usually compact multi-item lines.
 * Require a non-space before the gap so leading indent ("  12. nested") is kept.
 */
const CONSECUTIVE_ORDERED_ON_LIST_LINE =
    /(?<=\S)[ \t]+(\d+[.)]\s+)(?=[^\d\s\n])/g;

/** Line begins with an ordered list marker (optional indent; body optional). */
const LINE_STARTS_ORDERED_ITEM = /^\s*\d+[.)](?:\s|$)/;

/** List line may contain a later packed marker (not just leading indent). */
const LIST_LINE_HAS_MID_MARKER = /(?<=\S)[ \t]+\d+[.)]\s/;

/** Cheap gate: skip mid-line work when the segment has no marker shape. */
const HAS_ORDERED_LIST_MARKER_SHAPE = /\d[.)]/;

const LATIN_LETTER = /[A-Za-z]/;

function splitGluedMidLineOrderedMarkers(text: string): string {
    return text.replace(MID_LINE_ORDERED_GLUED, (full, prev: string, marker: string) => {
        const digitLen = marker.match(/^\d+/)?.[0].length ?? 0;
        // Latin letter + multi-digit is usually amount/id (USD10. 00), not list glue.
        if (digitLen >= 2 && LATIN_LETTER.test(prev)) return full;
        return `${prev}\n${marker}`;
    });
}

/**
 * Whether a run of indices looks like a packed list rather than section refs.
 * - starts at 1 (classic "1. … 2. … 10. …"), or
 * - 3+ consecutive ascending steps (e.g. "9. … 10. … 11. …")
 */
export function packedOrderedLooksLikeList(nums: number[]): boolean {
    if (nums.length < 2) return false;
    if (nums[0] === 1) return true;
    if (nums.length < 3) return false;
    for (let i = 1; i < nums.length; i++) {
        if (nums[i] !== nums[i - 1] + 1) return false;
    }
    return true;
}

/**
 * Single line-wise pass:
 * - expand compact multi-item list lines ("1. a 2. b 10. c"), preserving indent
 * - expand packed ordered items in prose ("要闻 1. a 2. b 10. c")
 */
function expandOrderedItemsOnLines(text: string): string {
    const lines = text.split("\n");
    const out: string[] = [];
    let changed = false;

    for (const line of lines) {
        if (LINE_STARTS_ORDERED_ITEM.test(line)) {
            // Only pay for replace when a mid-line marker gap is plausible.
            if (!LIST_LINE_HAS_MID_MARKER.test(line)) {
                out.push(line);
                continue;
            }
            const lead = leadingIndentText(line);
            const expanded = line.replace(CONSECUTIVE_ORDERED_ON_LIST_LINE, "\n$1");
            if (expanded === line) {
                out.push(line);
                continue;
            }
            changed = true;
            const parts = expanded.split("\n");
            out.push(parts[0]);
            // Keep nested compact items at the same indent as the first item.
            for (let i = 1; i < parts.length; i++) {
                if (parts[i]) out.push(lead + parts[i]);
            }
            continue;
        }

        // Prose with 2+ packed markers.
        const matches = [...line.matchAll(ORDERED_MARKER_WITH_NON_DIGIT_BODY)];
        if (matches.length < 2) {
            out.push(line);
            continue;
        }
        const nums = matches.map((m) => Number.parseInt(m[0], 10));
        if (!packedOrderedLooksLikeList(nums)) {
            out.push(line);
            continue;
        }
        const firstIdx = matches[0].index ?? 0;
        const prefix = line.slice(0, firstIdx).trimEnd();
        if (prefix) out.push(prefix);
        for (let i = 0; i < matches.length; i++) {
            const start = matches[i].index ?? 0;
            const end = i + 1 < matches.length ? (matches[i + 1].index ?? line.length) : line.length;
            const item = line.slice(start, end).trimEnd();
            if (item) out.push(item);
        }
        changed = true;
    }

    return changed ? out.join("\n") : text;
}

/**
 * Insert newlines before mid-line ordered markers (outside fenced/protected spans).
 *
 * Order:
 * 1) punctuation + spaces + marker
 * 2) glued marker (no space)
 * 3) line-wise compact / prose packed expansion
 */
export function splitMidLineOrderedListMarkers(text: string): string {
    if (!text || !HAS_ORDERED_LIST_MARKER_SHAPE.test(text)) return text;
    // Normalize real CRLF/CR so line-wise expand does not leave trailing \r on markers.
    // Lone CR between items ("1. a\r2. b") becomes a real newline. Pipeline always continues
    // from this LF form so a no-op expand still returns LF text, not the original CR bytes.
    let out = text.includes("\r") ? text.replace(/\r\n/g, "\n").replace(/\r/g, "\n") : text;
    out = out.replace(MID_LINE_ORDERED_AFTER_PUNCT_SPACE, "$1\n$2");
    out = splitGluedMidLineOrderedMarkers(out);
    out = expandOrderedItemsOnLines(out);
    return out;
}

/**
 * Index column layout (color applied by caller).
 * nowrap + no prose word-break: "10." must never reflow into "1" / "0.".
 * minWidth 3ch covers two-digit "10."; longer indices grow with content.
 */
export const orderedListMarkerLayoutStyle = {
    flexShrink: 0,
    minWidth: "3ch",
    textAlign: "right",
    paddingRight: "0.5em",
    whiteSpace: "nowrap",
    wordBreak: "normal",
    overflowWrap: "normal",
    fontVariantNumeric: "tabular-nums",
} as const;
