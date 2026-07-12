/**
 * Legacy coding-agent stream marks (emoji code points).
 * Matched for history compatibility; UI renders SVG — never emoji glyphs.
 * Pure module: no React imports (keeps unit tests light and avoids cycles).
 */

/** Code points that used to prefix tool/status lines in remote session output. */
export const LEGACY_STREAM_MARK_CLASS = "\u23FA\u23F3\u26A1\u2713\u2705\u2717\u26A0\u274C";

/**
 * Marks kept for product UI substitution (status + rating star), not decorative chat emoji.
 * Shared by assistant body display, capability-list normalize, and remote stream rendering.
 * All code points are BMP so the string is safe inside a JS character class.
 */
export const PRESERVED_INLINE_MARK_CLASS = `${LEGACY_STREAM_MARK_CLASS}\u2B50`;

const LEGACY_STREAM_MARK = new RegExp(`^([${LEGACY_STREAM_MARK_CLASS}])\\uFE0F?\\s*`);

/** Shared detector for markdown flush + list continuation (no global flag — safe for .test()). */
export const SPECIAL_LINE_PREFIX = new RegExp(
    `^(#{1,4}\\s|>\\s|[-*]\\s|\\d+[.)]\\s|[${LEGACY_STREAM_MARK_CLASS}]|[A-Za-z]:\\\\|~\\/|\\/[^/\\s]+\\/)`,
);

/** Claude Code / SDK user prompt prefix (legacy). */
export const USER_PROMPT_PREFIX = "\u276F ";

export type ParsedLegacyStreamMark = { mark: string; body: string };

/** Status kinds rendered via StatusGlyph (subset of WorkbenchIcons StatusGlyphKind). */
export type StreamStatusKind = "ok" | "error" | "pending" | "warn" | "tool";

export function parseLegacyStreamMark(text: string): ParsedLegacyStreamMark | null {
    const m = text.match(LEGACY_STREAM_MARK);
    if (!m) return null;
    return { mark: m[1], body: text.slice(m[0].length) };
}

export function stripUserPromptPrefix(line: string): string {
    return line.startsWith(USER_PROMPT_PREFIX) ? line.slice(USER_PROMPT_PREFIX.length) : line;
}

export function isUserPromptLine(line: string): boolean {
    return line.startsWith(USER_PROMPT_PREFIX);
}

/**
 * Map a legacy mark character to a StatusGlyph kind, or a special non-status visual.
 * - "bolt" is success-tinted action flash (not the same as ok check)
 */
export type LegacyStreamVisual =
    | { kind: "status"; status: StreamStatusKind }
    | { kind: "bolt" };

export function legacyMarkVisual(mark: string): LegacyStreamVisual | null {
    switch (mark) {
        case "\u23FA":
            return { kind: "status", status: "tool" };
        case "\u23F3":
            return { kind: "status", status: "pending" };
        case "\u26A1":
            return { kind: "bolt" };
        case "\u2713":
        case "\u2705":
            return { kind: "status", status: "ok" };
        case "\u26A0":
            return { kind: "status", status: "warn" };
        case "\u2717":
        case "\u274C":
            return { kind: "status", status: "error" };
        default:
            return null;
    }
}

export function isPathLine(text: string): boolean {
    // Windows drive, home, or absolute POSIX (any first path segment).
    return /^[A-Za-z]:\\/.test(text) || /^~\//.test(text) || /^\/[^/\s]+\//.test(text);
}
