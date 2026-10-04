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
const markdownSensitiveSpanPattern = /(\\\([^\r\n]*?\\\))|(\$(?!\$)[^\r\n$]+\$(?!\$))|(!?\[[^\]\n]+\]\([^)\n]+\))|(`[^`\n]+`)|(\*\*[^*\n]+\*\*)|(\*[^\s*\n][^*\n]*\*)|(https?:\/\/[^\s<>()]+)|([A-Za-z]:\\[^\n\r\s*?"<>|]+)|(~[/\\][^\n\r\s*?"<>|]+)/g;
const compactPipeTableSeparatorPattern = /(\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?)/g;
const bareHeadingMarkerLinePattern = /^(#{1,6})(?:\s+#{1,6})*$/;
// Include GFM "+" and common unicode bullets so bare-heading attach refuses list
// lines even if stripListMarkerForHeadingAttach misses a variant.
const markdownBlockStructureLinePattern =
    /^(?:#{1,6}\s+|>\s+|(?:[-*+]|\u2022|\u00b7)\s+|\d+[.)]\s+|[-*_]{3,}\s*$|\[KB_IMAGE:)/;
const markdownTableStructureLinePattern = /^\|.*\|$|^\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?$/;
const compactHeadingMarkerPattern = /([^#\n\s])\s*(#{2,6})(?=[^\s#\d.,;:!?，。；：！？、)\]）}])/gu;

type ProtectedDisplayMathSegment = { text: string; math: boolean };

type MarkdownFenceSegment = { text: string; fenced: boolean };

/**
 * Split Markdown into source and fenced-code segments without mistaking a
 * shorter run of backticks (or tildes) for the closer of a longer fence.
 * A fence line inside an open display formula stays in the source segment;
 * the formula splitter protects it. A fence still hides `$` lines inside code.
 */
function splitMarkdownFenceSegments(content: string): MarkdownFenceSegment[] {
    // List repair splits display math out of the source segment. Replies with
    // no fence run have nothing to hide here.
    if (!lineHasFenceRun(content)) {
        return content ? [{ text: content, fenced: false }] : [];
    }

    const segments: MarkdownFenceSegment[] = [];
    let source = "";
    let fence = "";
    let fenced = "";
    let math: DisplayMathDelimiter | "" = "";

    const pushSource = () => {
        if (source) segments.push({ text: source, fenced: false });
        source = "";
    };
    const pushFence = () => {
        if (fenced) segments.push({ text: fenced, fenced: true });
        fenced = "";
    };

    for (const line of content.match(/.*(?:\r\n|\n|\r|$)/g) || []) {
        if (!line) continue;
        if (math) {
            source += line;
            math = displayMathAfterLine(line, math);
            continue;
        }
        // Openers and closers both contain a fence run. Other lines skip the match.
        if (!lineHasFenceRun(line)) {
            if (fence) fenced += line;
            else {
                source += line;
                math = displayMathAfterLine(line, "");
            }
            continue;
        }
        const marker = line.trimStart().match(/^(`{3,}|~{3,})([^\r\n]*)/);
        if (!fence) {
            if (marker) {
                pushSource();
                fence = marker[1];
                fenced = line;
            } else {
                source += line;
                math = displayMathAfterLine(line, "");
            }
            continue;
        }

        fenced += line;
        if (
            marker
            && marker[1][0] === fence[0]
            && marker[1].length >= fence.length
            && !marker[2].trim()
        ) {
            pushFence();
            fence = "";
        }
    }

    // An unfinished streaming fence remains opaque until its closer arrives.
    if (fence) pushFence();
    else pushSource();
    return segments;
}

// A fence marker glued to earlier text on the same line (`偏高``` `, or a
// closer glued to the last code line). Optional info is a single language
// token with no spaces. A spaced mention such as "type ``` " is left alone.
const gluedFenceEndingPattern = /^(.*\S)(`{3,}|~{3,})([A-Za-z0-9_+#.-]*)[ \t]*\r?$/;
// True when some non-space character sits immediately before a fence run.
// Line-start fences (optional indent) fail this, so well-formed replies skip
// the line walk. Not global: a shared /g regexp would keep lastIndex.
const gluedFenceProbePattern = /[^\s`~](?:`{3,}|~{3,})/;

type GluedFenceSplit = { prose: string; marker: string; info: string };
type DisplayMathDelimiter = "$$" | "\\[";

function lineHasFenceRun(line: string): boolean {
    return line.includes("```") || line.includes("~~~");
}

function mayContainGluedFence(content: string): boolean {
    return lineHasFenceRun(content) && gluedFenceProbePattern.test(content);
}

function splitGluedFenceLine(line: string): GluedFenceSplit | null {
    if (!lineHasFenceRun(line)) return null;
    // A real fence already occupies the line start (indent allowed). Callers
    // track that separately; this only repairs a marker stuck to prose.
    if (/^[ \t]*(?:`{3,}|~{3,})/.test(line)) return null;
    const match = line.match(gluedFenceEndingPattern);
    if (!match) return null;
    return { prose: match[1], marker: match[2], info: match[3] || "" };
}

/**
 * Display-math state after `line`. Same open/close rules as the chat renderer:
 * a complete `$$...$$` stays closed, and a fence run inside the formula is
 * formula source.
 */
export function displayMathAfterLine(line: string, open: DisplayMathDelimiter | ""): DisplayMathDelimiter | "" {
    // An open `\[` formula closes on `\]`, which does not contain `\[`.
    if (!open && !line.includes("$$") && !line.includes("\\[")) return "";
    const trimmed = line.trim();
    if (open) {
        const close = open === "$$" ? "$$" : "\\]";
        return trimmed === close || trimmed.endsWith(close) ? "" : open;
    }
    if (trimmed.startsWith("$$")) {
        if (trimmed.endsWith("$$") && trimmed.length > 4) return "";
        if (trimmed.length > 2 || trimmed === "$$") return "$$";
        return "";
    }
    if (trimmed.startsWith("\\[")) {
        if (trimmed.endsWith("\\]") && trimmed.length > 4) return "";
        if (trimmed.length > 2 || trimmed === "\\[") return "\\[";
    }
    return "";
}

/**
 * True when the trailing backtick run closes an inline code span opened in
 * `prose` (for example `` See ```code``` ``). That run is not a fence.
 */
function trailingFenceClosesInlineCode(prose: string, marker: string): boolean {
    if (!marker.startsWith("`") || !prose.includes("`")) return false;
    const opens: number[] = [];
    let index = 0;
    while (index < prose.length) {
        if (prose[index] !== "`") {
            index++;
            continue;
        }
        let end = index + 1;
        while (end < prose.length && prose[end] === "`") end++;
        const length = end - index;
        let closed = false;
        for (let openIndex = opens.length - 1; openIndex >= 0; openIndex--) {
            if (opens[openIndex] !== length) continue;
            opens.splice(openIndex, 1);
            closed = true;
            break;
        }
        if (!closed) opens.push(length);
        index = end;
    }
    return opens.includes(marker.length);
}

function lineOpensOrClosesFence(line: string, openFence: string): { next: string; glued: GluedFenceSplit | null } {
    const started = line.trimStart().match(/^(`{3,}|~{3,})(.*)$/);
    if (!openFence) {
        if (started) return { next: started[1], glued: null };
        const glued = splitGluedFenceLine(line);
        if (glued && !trailingFenceClosesInlineCode(glued.prose, glued.marker)) return { next: glued.marker, glued };
        return { next: "", glued: null };
    }
    if (started && started[1][0] === openFence[0] && started[1].length >= openFence.length && !started[2].trim()) {
        return { next: "", glued: null };
    }
    const glued = splitGluedFenceLine(line);
    if (
        glued
        && !glued.info
        && glued.marker[0] === openFence[0]
        && glued.marker.length >= openFence.length
        && !trailingFenceClosesInlineCode(glued.prose, glued.marker)
    ) {
        return { next: "", glued };
    }
    return { next: openFence, glued: null };
}

/**
 * Fence marker that is open after `line`. A marker glued to prose
 * (`heading``` `) opens a fence; a closer glued to the last code line
 * (`echo ok``` `) closes one. Inline spans such as `` ```code``` `` do not.
 * A display-math opener (`$$ x``` `) does not open a fence.
 */
export function markdownFenceAfterLine(line: string, openFence: string): string {
    // Prose, inline code, and strikethrough never open or close a fence.
    if (!lineHasFenceRun(line)) return openFence;
    // The renderer keeps a formula opener intact, backticks included.
    if (!openFence && displayMathAfterLine(line, "")) return openFence;
    return lineOpensOrClosesFence(line, openFence).next;
}

/**
 * Move a fence marker that is glued to prose onto its own line so the
 * line-start fence parser sees the opener. Without this, the following
 * closer is treated as an opener and the rest of the reply renders as code.
 * Display-only: stored text is unchanged. Idempotent once fences are split.
 */
export function detachGluedMarkdownFences(content: string): string {
    if (!mayContainGluedFence(content)) return content;
    const lines = content.split("\n");
    let openFence = "";
    let math: DisplayMathDelimiter | "" = "";
    let changed = false;
    const out: string[] = [];
    for (const line of lines) {
        // Formula source can end in backticks. Splitting it would open a fence
        // the renderer never sees, because display math hides fence markers.
        if (math) {
            out.push(line);
            math = displayMathAfterLine(line, math);
            continue;
        }
        // `$$ x``` ` opens a formula. The glued run stays inside that line.
        if (!openFence) {
            const nextMath = displayMathAfterLine(line, "");
            if (nextMath) {
                math = nextMath;
                out.push(line);
                continue;
            }
        }
        const { next, glued } = lineOpensOrClosesFence(line, openFence);
        if (glued) {
            changed = true;
            out.push(glued.prose);
            out.push(glued.info ? `${glued.marker}${glued.info}` : glued.marker);
        } else {
            out.push(line);
        }
        openFence = next;
    }
    return changed ? out.join("\n") : content;
}

/**
 * Keep display-math source intact while repairing conversational Markdown.
 * In particular, TeX commands such as `\\newline` must never be mistaken for
 * serialized line breaks before KaTeX sees them.
 */
function splitDisplayMathSegments(content: string): ProtectedDisplayMathSegment[] {
    // Most replies have no display formula. The line walk below is only needed
    // to keep TeX opaque while list markers are repaired.
    if (!content.includes("$$") && !content.includes("\\[")) {
        return content ? [{ text: content, math: false }] : [];
    }
    const segments: ProtectedDisplayMathSegment[] = [];
    let source = "";
    let delimiter: "$$" | "\\[" | "" = "";
    let math = "";

    const pushSource = () => {
        if (source) segments.push({ text: source, math: false });
        source = "";
    };
    const pushMath = () => {
        if (math) segments.push({ text: math, math: true });
        math = "";
    };

    for (const line of content.match(/.*(?:\r\n|\n|\r|$)/g) || []) {
        if (!line) continue;
        const trimmed = line.trim();
        if (!delimiter) {
            if (trimmed.startsWith("$$")) {
                pushSource();
                if (!(trimmed.endsWith("$$") && trimmed.length > 4)) delimiter = "$$";
                math = line;
                if (!delimiter) pushMath();
            } else if (trimmed.startsWith("\\[")) {
                pushSource();
                if (!(trimmed.endsWith("\\]") && trimmed.length > 4)) delimiter = "\\[";
                math = line;
                if (!delimiter) pushMath();
            } else {
                source += line;
            }
            continue;
        }

        math += line;
        const closeDelimiter = delimiter === "$$" ? "$$" : "\\]";
        if (trimmed.endsWith(closeDelimiter)) {
            pushMath();
            delimiter = "";
        }
    }

    if (delimiter) pushMath();
    else pushSource();
    return segments;
}

/** True when `text` can contain an Extended_Pictographic (BMP U+00A9–U+3299, or a non-BMP surrogate). */
function mayContainCapabilityPictograph(text: string): boolean {
    for (let i = 0; i < text.length; i++) {
        const c = text.charCodeAt(i);
        if (c >= 0xd800 && c <= 0xdbff) return true;
        if (c >= 0xa9 && c < 0x4e00) return true;
    }
    return false;
}

function hasMultipleCapabilityIcons(text: string): boolean {
    if (!mayContainCapabilityPictograph(text)) return false;
    const matches = text.match(digitalEmployeeCapabilityIconScanPattern);
    return (matches?.length || 0) >= 2;
}

function needsMarkdownSpanProtection(text: string): boolean {
    return text.includes("\\")
        || text.includes("$")
        || text.includes("[")
        || text.includes("`")
        || text.includes("*")
        || text.includes("http")
        || text.includes("~");
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
    const parts = splitMarkdownFenceSegments(detachGluedMarkdownFences(content));
    for (const part of parts) {
        if (part.fenced) {
            if (part.text.includes("\\")) {
                part.text = part.text
                    .replace(/^(```[^\n\r\\]*)\\r\\n/, "$1\n")
                    .replace(/^(```[^\n\r\\]*)\\n/, "$1\n")
                    .replace(/^(```[^\n\r\\]*)\\r/, "$1\n")
                    .replace(/\\r\\n(```\s*)$/, "\n$1")
                    .replace(/\\n(```\s*)$/, "\n$1")
                    .replace(/\\r(```\s*)$/, "\n$1");
            }
            continue;
        }
        // Count dense capability lists on the original segment — after the first
        // pictograph is rewritten, fewer than 2 remain and mid-list items would be skipped.
        const displayMathSegments = splitDisplayMathSegments(part.text);
        part.text = displayMathSegments.map((displayMathSegment) => {
            if (displayMathSegment.math) return displayMathSegment.text;
            const mayHaveIcons = mayContainCapabilityPictograph(displayMathSegment.text);
            const denseCapabilityList = mayHaveIcons && hasMultipleCapabilityIcons(displayMathSegment.text);
            const repairLists = (segment: string) => {
                let out = segment;
                if (out.includes("\\")) out = out.replace(escapedNewlinePattern, "\n");
                if (out.includes("||")) out = out.replace(/\|\|(?=\s*[^|\s])/g, "\n|");
                if (out.includes("#")) {
                    out = out
                        .replace(/([\uff1a:;\uff1b.!?\uff01\uff1f\u3002,%\uff05)\uff09\]])\s*(#{1,6}\s+)/g, "$1\n$2")
                        .replace(/([\uff1a:;\uff1b.!?\uff01\uff1f\u3002,%\uff05)\uff09\]])\s*(#{2,6})(?=[^#\s])/g, "$1\n$2 ")
                        .replace(compactHeadingMarkerPattern, "$1\n$2 ")
                        .replace(/([^#\n\s])\s*(#{2,6})(?=[\p{Emoji_Presentation}\p{So}])/gu, "$1\n$2 ")
                        .replace(/(^|\n)(#{2,6})(?=[^#\s])/g, "$1$2 ");
                }
                if (out.includes("：") || out.includes(":")) {
                    out = out.replace(/([\uff1a:])\s*(-\s+)/g, "$1\n$2");
                }
                // A dash after a table-cell delimiter is cell content, not an inline list.
                if (out.includes("- ")) {
                    out = out.replace(/([^\n\s|])(- (?:[\p{Emoji_Presentation}\p{So}]|[*]{2}|\p{L}))/gu, "$1\n$2");
                }
                out = splitMidLineOrderedListMarkers(out);
                if (mayHaveIcons) {
                    out = out.replace(capabilityIconAfterPunctuationPattern, "$1\n- ");
                    if (denseCapabilityList) out = out.replace(capabilityIconMidSentencePattern, "$1\n- ");
                }
                return out;
            };
            const normalized = needsMarkdownSpanProtection(displayMathSegment.text)
                ? withMarkdownSensitiveSpansProtected(displayMathSegment.text, repairLists)
                : repairLists(displayMathSegment.text);
            return normalizeCompactPipeTables(normalized);
        }).join("");
    }
    return parts.map((part) => part.text).join("");
}

export function attachBareHeadingMarkers(lines: string[]): string[] {
    const attached: string[] = [];
    let inCodeBlock = false;
    let codeFenceMarker = "";
    let displayMathDelimiter: "$$" | "\\[" | "" = "";
    for (let index = 0; index < lines.length; index++) {
        const line = lines[index];
        if (displayMathDelimiter) {
            attached.push(line);
            const closeDelimiter = displayMathDelimiter === "$$" ? "$$" : "\\]";
            if (line.trim().endsWith(closeDelimiter)) displayMathDelimiter = "";
            continue;
        }
        const fenceMatch = lineHasFenceRun(line)
            ? line.trimStart().match(/^(`{3,}|~{3,})(.*)$/)
            : null;
        if (fenceMatch) {
            const marker = fenceMatch[1];
            if (!inCodeBlock) {
                inCodeBlock = true;
                codeFenceMarker = marker;
            } else if (
                marker[0] === codeFenceMarker[0]
                && marker.length >= codeFenceMarker.length
                && !fenceMatch[2].trim()
            ) {
                inCodeBlock = false;
                codeFenceMarker = "";
            }
            attached.push(line);
            continue;
        }
        if (inCodeBlock) {
            attached.push(line);
            continue;
        }

        const trimmed = line.trim();
        // Keep TeX source opaque, but resume ordinary heading repair as soon
        // as a complete display formula ends.
        if (trimmed.startsWith("$$")) {
            attached.push(line);
            if (!(trimmed.endsWith("$$") && trimmed.length > 4)) displayMathDelimiter = "$$";
            continue;
        }
        if (trimmed.startsWith("\\[")) {
            attached.push(line);
            if (!(trimmed.endsWith("\\]") && trimmed.length > 4)) displayMathDelimiter = "\\[";
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
