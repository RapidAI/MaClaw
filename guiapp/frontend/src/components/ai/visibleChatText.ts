const VE_TOFU_RE = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F-\u009F\u25A1\uE000-\uF8FF\uFFF0-\uFFFF]/g;

/** True when a UTF-16 code unit is one the tofu regex would drop. */
export function chatTextHasTofu(value: string, start = 0): boolean {
    for (let i = start; i < value.length; i++) {
        const c = value.charCodeAt(i);
        if (c < 0x20) {
            if (c !== 0x09 && c !== 0x0a && c !== 0x0d) return true;
            continue;
        }
        if ((c >= 0x7f && c <= 0x9f) || c === 0x25a1 || (c >= 0xe000 && c <= 0xf8ff) || c >= 0xfff0) return true;
    }
    return false;
}

/** Drop tofu code units. A clean string is returned unchanged. */
export function stripChatTofu(value: string): string {
    if (!value || !chatTextHasTofu(value)) return value;
    return value.replace(VE_TOFU_RE, "");
}

/**
 * Strip stream markers and non-displayable runes from assembled visible text.
 * Unlike stream-delta helpers, a leading U+0001 does not drop the whole
 * string — that would hide a persisted answer that still carries the sentinel
 * from an older backend.
 */
export function sanitizeVisibleChatText(value: unknown): string {
    return stripChatTofu(typeof value === "string" ? value : "");
}

/**
 * The host continuation header is a phase contract for the model.
 * A line that starts with it stays out of the transcript. A quoted marker
 * inside a sentence stays. Text without that line is returned unchanged.
 */
export function omitGoalContinuationHeader(text: string): string {
    if (!text || !text.includes("[系统续接]")) return text;
    const lines = text.split("\n");
    let changed = false;
    const kept: string[] = [];
    for (const line of lines) {
        if (line.trimStart().startsWith("[系统续接]")) {
            changed = true;
            continue;
        }
        kept.push(line);
    }
    if (!changed) return text;
    // Only the blank left by the removed header goes. Later blank lines,
    // including ones inside a code block, stay.
    return kept.join("\n").replace(/^\n+/, "");
}

export function sanitizeVisibleVEText(value: unknown): string {
    return sanitizeVisibleChatText(value);
}

/**
 * Answer lane for VE chat. The shared agent loop prefixes private reasoning
 * deltas with \x01. Those belong on ve:reasoning, not in the reply. Drop them
 * here so an older backend cannot paint a hollow square in the answer.
 */
export function visibleVEStreamContent(value: unknown): string {
    const content = typeof value === "string" ? value : "";
    if (content.startsWith("\x01")) return "";
    return sanitizeVisibleChatText(content);
}

export function firstVEStreamText(...values: unknown[]): unknown {
    for (const value of values) {
        if (typeof value === "string" && value !== "") return value;
    }
    return values[0];
}

// A shared run shorter than a clause is punctuation, not the same sentence.
// Counted in UTF-16 code units, the same unit as string.length.
const answerSliceOverlapMin = 16;

// The agent loop trims the settled answer. The token stream still has the
// newline that trim removed. Only those edges are ignored while matching.
function trimAnswerEdgeRight(value: string): string {
    let end = value.length;
    while (end > 0) {
        const code = value.charCodeAt(end - 1);
        if (code !== 0x20 && code !== 0x09 && code !== 0x0d && code !== 0x0a) break;
        end -= 1;
    }
    return end === value.length ? value : value.slice(0, end);
}

function trimAnswerEdgeLeft(value: string): string {
    let start = 0;
    while (start < value.length) {
        const code = value.charCodeAt(start);
        if (code !== 0x20 && code !== 0x09 && code !== 0x0d && code !== 0x0a) break;
        start += 1;
    }
    return start === 0 ? value : value.slice(start);
}

// Longest prefix of tail that is a suffix of prefix.
// The walk stays inside tail, so a NUL in the text cannot form a false edge.
function sharedAnswerEdge(prefix: string, tail: string): number {
    if (!prefix || !tail) return 0;
    const pi = new Array<number>(tail.length);
    pi[0] = 0;
    let k = 0;
    for (let i = 1; i < tail.length; i++) {
        while (k > 0 && tail.charCodeAt(i) !== tail.charCodeAt(k)) k = pi[k - 1];
        if (tail.charCodeAt(i) === tail.charCodeAt(k)) k += 1;
        pi[i] = k;
    }
    k = 0;
    for (let i = 0; i < prefix.length; i++) {
        const code = prefix.charCodeAt(i);
        while (k > 0 && (k === tail.length || tail.charCodeAt(k) !== code)) k = pi[k - 1];
        if (k < tail.length && tail.charCodeAt(k) === code) k += 1;
    }
    return k >= answerSliceOverlapMin ? k : 0;
}

/**
 * Join the streamed opening and the settled ending of one answer.
 * A shared run is kept once. A longer slice that already contains the other
 * replaces it. A short ending continues the same sentence. A short opening
 * stays apart from a different long answer. Anything else stays in order,
 * with a gap between the opening and the ending.
 */
export function projectAnswerSlices(opening: string, ending: string): string {
    if (!opening) return ending;
    if (!ending) return opening;
    if (ending.trimStart().startsWith("[系统提示]")) return `${opening}\n\n${ending}`;
    const openCmp = trimAnswerEdgeRight(opening);
    const endCmp = trimAnswerEdgeLeft(ending);
    if (!openCmp) return ending;
    if (!endCmp) return opening;
    if ((endCmp.startsWith(openCmp) || endCmp.endsWith(openCmp)) && endCmp.length >= openCmp.length) return ending;
    if (openCmp.startsWith(endCmp) || openCmp.endsWith(endCmp)) return opening;
    const overlap = sharedAnswerEdge(openCmp, endCmp);
    if (overlap > 0) return openCmp + endCmp.slice(overlap);
    if (ending.length < answerSliceOverlapMin) return opening + ending;
    return `${opening}\n\n…\n\n${ending}`;
}

export function visibleHistoryMessageContent(kind: string, ...values: unknown[]): string {
    const raw = firstVEStreamText(...values);
    switch (String(kind || "").trim().toLowerCase()) {
        case "stream_chunk":
        case "stream_end":
            return visibleVEStreamContent(raw);
        case "stream_status":
            return "";
        default:
            return sanitizeVisibleChatText(raw);
    }
}
