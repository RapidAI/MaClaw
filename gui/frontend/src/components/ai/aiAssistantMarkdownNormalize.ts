import { PRESERVED_INLINE_MARK_CLASS } from "../remote/remoteStreamMarks";
import { splitMidLineOrderedListMarkers } from "./orderedListMarkdown";

const escapedNewlinePattern = /\\r\\n|\\n|\\r/g;
// Older digital-employee copy used pictographs as list markers.
// Detect via Unicode property (no emoji literals in source), rewrite as plain "-" lists.
// Exclude semantic status/star marks (kept for SVG glyphs) from capability-list rewriting.
const digitalEmployeeCapabilityIconPattern = `(?:(?![${PRESERVED_INLINE_MARK_CLASS}])\\p{Extended_Pictographic})`;
const digitalEmployeeCapabilityIconScanPattern = new RegExp(digitalEmployeeCapabilityIconPattern, "gu");
// Horizontal space only — do not let \\s eat newlines and rewrite the next line's leading mark.
const capabilityIconAfterPunctuationPattern = new RegExp(`([\\uff1a:;\\uff1b])[ \\t]*${digitalEmployeeCapabilityIconPattern}[ \\t]*`, "gu");
const capabilityIconMidSentencePattern = new RegExp(`([^\\n\\s])[ \\t]+${digitalEmployeeCapabilityIconPattern}[ \\t]*`, "gu");
// Protect path-like spans before escaped-newline rewriting. Without this,
// Windows/home paths containing "\n…" (e.g. \notes, ~\name) get a hard line break.
const markdownSensitiveSpanPattern = /(!?\[[^\]\n]+\]\([^)\n]+\))|(`[^`\n]+`)|(\*\*[^*\n]+\*\*)|(\*[^\s*\n][^*\n]*\*)|(https?:\/\/[^\s<>()]+)|([A-Za-z]:\\[^\n\r\s*?"<>|]+)|(~[/\\][^\n\r\s*?"<>|]+)/g;
const compactPipeTableSeparatorPattern = /(\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?)/g;
const bareHeadingMarkerLinePattern = /^(#{1,6})(?:\s+#{1,6})*$/;
// Include GFM "+" and common unicode bullets so bare-heading attach refuses list
// lines even if stripListMarkerForHeadingAttach misses a variant.
const markdownBlockStructureLinePattern =
    /^(?:#{1,6}\s+|>\s+|(?:[-*+]|\u2022|\u00b7)\s+|\d+[.)]\s+|[-*_]{3,}\s*$|\[KB_IMAGE:)/;
const markdownTableStructureLinePattern = /^\|.*\|$|^\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?$/;
const compactHeadingMarkerPattern = /([^#\n\s])\s*(#{2,6})(?=[^\s#\d.,;:!?，。；：！？、)\]）}])/gu;

function hasMultipleCapabilityIcons(text: string): boolean {
    const matches = text.match(digitalEmployeeCapabilityIconScanPattern);
    return (matches?.length || 0) >= 2;
}

function withMarkdownSensitiveSpansProtected(text: string, transform: (value: string) => string): string {
    const spans: string[] = [];
    let tokenPrefix = "__MACLAW_MD_PROTECTED__";
    while (text.includes(tokenPrefix)) tokenPrefix = `${tokenPrefix}_`;
    const protectedText = text.replace(markdownSensitiveSpanPattern, (span) => {
        const token = `${tokenPrefix}${spans.length}__`;
        spans.push(span);
        return token;
    });
    const transformed = transform(protectedText);
    const tokenPattern = new RegExp(`${escapeRegExp(tokenPrefix)}(\\d+)__`, "g");
    return transformed.replace(tokenPattern, (_token, indexText) => {
        const index = Number(indexText);
        return spans[index] ?? _token;
    });
}

function escapeRegExp(value: string): string {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function normalizeCompactPipeTables(text: string): string {
    if (!text.includes("|") || !text.includes("---")) return text;
    return text
        .replace(/\|\|(?=\s*[^|\s])/g, "\n|")
        .replace(/([^\n])\s*(\|[^\n|]+\|[^\n]*?)(\|\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?)/g, (_match, prefix, header, separator) => `${prefix}\n${header.trim()}\n${separator.trim()}`)
        .replace(compactPipeTableSeparatorPattern, (separator, _inner, offset, fullText) => {
            const before = offset > 0 && fullText[offset - 1] !== "\n" ? "\n" : "";
            const afterIndex = offset + separator.length;
            const after = afterIndex < fullText.length && fullText[afterIndex] !== "\n" ? "\n" : "";
            return `${before}${separator.trim()}${after}`;
        });
}

function hasUnescapedPipe(value: string): boolean {
    let escaped = false;
    for (const char of value) {
        if (escaped) {
            escaped = false;
            continue;
        }
        if (char === "\\") {
            escaped = true;
            continue;
        }
        if (char === "|") return true;
    }
    return false;
}

function canAttachBareHeadingMarkerToLine(line: string, followingLine?: string): boolean {
    const trimmed = line.trimStart();
    const followingTrimmed = followingLine?.trimStart() || "";
    return trimmed !== ""
        && !trimmed.startsWith("```")
        && !markdownBlockStructureLinePattern.test(trimmed)
        && !markdownTableStructureLinePattern.test(trimmed)
        && !(hasUnescapedPipe(trimmed) && markdownTableStructureLinePattern.test(followingTrimmed));
}

function getBareHeadingMarker(line: string): string | null {
    const trimmed = line.trim();
    if (!bareHeadingMarkerLinePattern.test(trimmed)) return null;
    const markers = trimmed.split(/\s+/);
    return markers[markers.length - 1] || null;
}

// Module-level: attachBareHeadingMarkers can walk many lines on long replies.
const unorderedListMarkerForHeadingAttach = /^(?:[-*+]|\u2022|\u00b7)\s+(\S.*)$/;
const orderedListMarkerForHeadingAttach = /^\d+[.)]\s+(\S.*)$/;

/**
 * Digital employees often emit section titles as a bare heading marker on its
 * own line followed by a list-marked title:
 *   ###
 *   - 北京城区天气预报
 *   • 今日生活指数
 * Strip the list marker so the title can be attached as real heading text
 * instead of leaving a raw "###" in the bubble.
 */
function stripListMarkerForHeadingAttach(line: string): string | null {
    const trimmed = line.trimStart();
    // -, *, +, • (U+2022), · (U+00B7) — keep as escapes so source encoding stays ASCII-safe.
    const unordered = trimmed.match(unorderedListMarkerForHeadingAttach);
    if (unordered) return unordered[1];
    const ordered = trimmed.match(orderedListMarkerForHeadingAttach);
    if (ordered) return ordered[1];
    return null;
}

/**
 * Insert newline before list markers that appear mid-line, but only outside
 * fenced code blocks. Prevents corrupting code content (e.g., YAML lists).
 */
export function normalizeInlineListMarkers(content: string): string {
    const parts = content.split(/(```[\s\S]*?```|```[\s\S]*$)/);
    for (let i = 0; i < parts.length; i++) {
        if (i % 2 === 1) {
            parts[i] = parts[i]
                .replace(/^(```[^\n\r\\]*)\\r\\n/, "$1\n")
                .replace(/^(```[^\n\r\\]*)\\n/, "$1\n")
                .replace(/^(```[^\n\r\\]*)\\r/, "$1\n")
                .replace(/\\r\\n(```\s*)$/, "\n$1")
                .replace(/\\n(```\s*)$/, "\n$1")
                .replace(/\\r(```\s*)$/, "\n$1");
            continue;
        }
        // Count dense capability lists on the original segment — after the first
        // pictograph is rewritten, fewer than 2 remain and mid-list items would be skipped.
        const denseCapabilityList = hasMultipleCapabilityIcons(parts[i]);
        let normalized = withMarkdownSensitiveSpansProtected(parts[i], (segment) => {
            let out = segment
                .replace(escapedNewlinePattern, "\n")
                .replace(/\|\|(?=\s*[^|\s])/g, "\n|")
                .replace(/([\uff1a:;\uff1b.!?\uff01\uff1f\u3002,%\uff05)\uff09\]])\s*(#{1,6}\s+)/g, "$1\n$2")
                .replace(/([\uff1a:;\uff1b.!?\uff01\uff1f\u3002,%\uff05)\uff09\]])\s*(#{2,6})(?=[^#\s])/g, "$1\n$2 ")
                .replace(compactHeadingMarkerPattern, "$1\n$2 ")
                .replace(/([^#\n\s])\s*(#{2,6})(?=[\p{Emoji_Presentation}\p{So}])/gu, "$1\n$2 ")
                .replace(/(^|\n)(#{2,6})(?=[^#\s])/g, "$1$2 ")
                .replace(/([\uff1a:])\s*(-\s+)/g, "$1\n$2")
                // A dash after a table-cell delimiter is cell content, not an inline list.
                .replace(/([^\n\s|])(- (?:[\p{Emoji_Presentation}\p{So}]|[*]{2}|\p{L}))/gu, "$1\n$2");
            out = splitMidLineOrderedListMarkers(out);
            out = out
                // Rewrite capability pictograph markers to plain list dashes (no emoji in UI).
                .replace(capabilityIconAfterPunctuationPattern, "$1\n- ");
            if (denseCapabilityList) {
                out = out.replace(capabilityIconMidSentencePattern, "$1\n- ");
            }
            return out;
        });
        normalized = normalizeCompactPipeTables(normalized);
        parts[i] = normalized;
    }
    return parts.join("");
}

export function attachBareHeadingMarkers(lines: string[]): string[] {
    const attached: string[] = [];
    let inCodeBlock = false;
    for (let index = 0; index < lines.length; index++) {
        const line = lines[index];
        if (/^```/.test(line.trimStart())) {
            inCodeBlock = !inCodeBlock;
            attached.push(line);
            continue;
        }
        if (inCodeBlock) {
            attached.push(line);
            continue;
        }

        const marker = getBareHeadingMarker(line);
        if (!marker) {
            attached.push(line);
            continue;
        }

        let headingMarker = marker;
        let nextIndex = index + 1;
        const pendingLines = [line];
        while (nextIndex < lines.length) {
            const nextTrimmed = lines[nextIndex].trim();
            if (nextTrimmed === "") {
                pendingLines.push(lines[nextIndex]);
                nextIndex++;
                continue;
            }
            const nextMarker = getBareHeadingMarker(nextTrimmed);
            if (nextMarker) {
                headingMarker = nextMarker;
                pendingLines.push(lines[nextIndex]);
                nextIndex++;
                continue;
            }
            break;
        }

        if (nextIndex < lines.length) {
            const nextLine = lines[nextIndex];
            // Prefer list-stripped title: bare "###" + "• 标题" / "- 标题" → "### 标题".
            // Must run before the plain-line path so the list marker is consumed
            // into heading text (not left as a bullet under a raw "###").
            const listBody = stripListMarkerForHeadingAttach(nextLine);
            if (listBody && canAttachBareHeadingMarkerToLine(listBody, lines[nextIndex + 1])) {
                attached.push(`${headingMarker} ${listBody}`);
                index = nextIndex;
                continue;
            }
            if (canAttachBareHeadingMarkerToLine(nextLine, lines[nextIndex + 1])) {
                attached.push(`${headingMarker} ${nextLine.trimStart()}`);
                index = nextIndex;
                continue;
            }
        }

        attached.push(...pendingLines);
        index = nextIndex - 1;
    }
    return attached;
}
