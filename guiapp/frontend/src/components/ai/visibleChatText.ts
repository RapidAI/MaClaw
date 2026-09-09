const VE_TOFU_RE = /[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F-\u009F\u25A1\uE000-\uF8FF\uFFF0-\uFFFF]/g;

/**
 * Strip stream markers and non-displayable runes from assembled visible text.
 * Unlike stream-delta helpers, a leading U+0001 does not drop the whole
 * string — that would hide a persisted answer that still carries the sentinel
 * from an older backend.
 */
export function sanitizeVisibleChatText(value: unknown): string {
    const content = typeof value === "string" ? value : "";
    return content.replace(VE_TOFU_RE, "");
}

export function sanitizeVisibleVEText(value: unknown): string {
    return sanitizeVisibleChatText(value);
}

/**
 * VE chat does not render a reasoning panel. The shared agent loop prefixes
 * private reasoning deltas with \x01, which Chromium displays as a square when
 * it leaks through an older backend or a remote Hub. Drop those deltas and any
 * remaining non-whitespace control characters as a client-side safety net.
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

export function visibleHistoryMessageContent(kind: string, ...values: unknown[]): string {
    const raw = firstVEStreamText(...values);
    switch (String(kind || "").trim().toLowerCase()) {
        case "stream_chunk":
        case "stream_end":
            return visibleVEStreamContent(raw);
        default:
            return sanitizeVisibleChatText(raw);
    }
}
