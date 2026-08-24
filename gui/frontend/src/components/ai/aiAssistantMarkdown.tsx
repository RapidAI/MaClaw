import React from "react";
import katex from "katex";
import "katex/dist/katex.min.css";
import { AIAssistantAttachmentPreviewDataURL, OpenFileOrShowInFolder, ShowItemInFolder } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import type { ChatAction, ChatConfirmation, ChatMessage, ChatRecoverableSession, ChatUnfinishedSlot, CodingAgentTimelineItem } from "./useAIAssistant";
import { renderCodingAgentProgressStatus } from "./CodingAgentProgressStatus";
import { attachBareHeadingMarkers, normalizeInlineListMarkers } from "./aiAssistantMarkdownNormalize";
import { buildMarkdownTableModel, isMarkdownTableRow, isMarkdownTableSeparatorRow, normalizeMarkdownTableLine, parseMarkdownTableCells, repairMixedNarrativeTable } from "./aiAssistantMarkdownTable";
import { localAssistantTabTitle, localizeText } from "./aiAssistantI18n";
import { baseInputBtnStyle, type Theme } from "./aiAssistantPanelTheme";
import { ChatBubbleFrame, CHAT_SPEAKER_LABEL_GAP, userChatBubbleBackground } from "./ChatBubbleFrame";
import { renderScreenshotPreview } from "./aiAssistantMarkdownMedia";
import { getWailsAppModule } from "../../utils/wailsAppModule";
import { stripRolePrefixForDisplay, truncateRolePrefixForDisplay } from "./rolePrefixDisplay";
import { stripCodingAgentAuditSections, stripCodingWorkbenchStatusReasoning } from "./codingAgentUserFinish";
import {
    prepareChatBodyForDisplay,
    prepareChatBodyLines,
    splitTextByPictographClusters,
    textMayContainPictograph,
    type InlineMarkVisual,
} from "./aiAssistantProgressUtils";
import {
    leadingIndentColumns,
    orderedListIndentPadding,
    orderedListMarkerLayoutStyle,
    parseOrderedListLine,
} from "./orderedListMarkdown";
import { AssistantReplyCopyButton } from "./AssistantReplyCopyButton";
import { IconBolt, IconSearch, IconStar, StatusGlyph } from "./WorkbenchIcons";
import {
    RecordingSessionCard,
    type RecordingCompleteResult,
} from "./RecordingSessionCard";
import { AssistantMermaidDiagram, isMermaidCodeFence } from "./AssistantMermaidDiagram";
import { AttachmentImageThumbnail } from "./AttachmentImagePreview";
import { useNestedPinnedScroll } from "./useNestedPinnedScroll";

export type { Theme } from "./aiAssistantPanelTheme";
export type { RecordingCompleteResult } from "./RecordingSessionCard";
export { AssistantReplyCopyButton, copyTextToClipboard } from "./AssistantReplyCopyButton";

/**
 * Build the plain-text payload for "copy reply": same body the user sees,
 * without decorative role prefixes. Does not include collapsed "thinking" text.
 */
export function buildAssistantReplyCopyText(
    content: string | undefined,
    unfinishedSlot?: ChatUnfinishedSlot,
    lang = "en",
): string {
    const raw = stripCodingAgentAuditSections(prepareChatBodyForDisplay(
        formatUnfinishedSlotNotice(content || "", unfinishedSlot, lang),
    ));
    return stripRolePrefixForDisplay(raw || "").replace(/\s+$/u, "");
}

/* Themed inline markdown rendering */
const pathQuoteCharsPattern = /[`'"\u2018\u2019\u201c\u201d]/;
const pathLeadingWrappingPattern = /^[`'"\u2018\u2019\u201c\u201d]+/;
const pathTrailingPunctuationPattern = /[\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]+$/;
const pathTrailingWrappingPattern = /[`'"\u2018\u2019\u201c\u201d]+$/;
const pathTrailingWrapperPunctuationPattern = /([`'"\u2018\u2019\u201c\u201d])[\s.,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]+$/;
const inlineWrapStyle: React.CSSProperties = { overflowWrap: "anywhere", wordBreak: "break-word" };
const blockWrapStyle: React.CSSProperties = { minWidth: 0, ...inlineWrapStyle };
/** Inline SVG status/star marks sit on the text baseline without looking like chat emoji. */
const inlineGlyphWrapStyle: React.CSSProperties = {
    display: "inline-flex",
    alignItems: "center",
    verticalAlign: "text-bottom",
    margin: "0 2px",
    lineHeight: 0,
};

function renderInlineMarkGlyph(visual: InlineMarkVisual, key: React.Key): React.ReactNode {
    if (visual.kind === "star") {
        return (
            <span key={key} data-testid="inline-star-glyph" style={inlineGlyphWrapStyle} aria-hidden>
                <IconStar size={13} color="var(--theme-warning, #d97706)" filled />
            </span>
        );
    }
    if (visual.kind === "bolt") {
        return (
            <span key={key} data-testid="inline-bolt-glyph" style={inlineGlyphWrapStyle} aria-hidden>
                <IconBolt size={13} color="var(--theme-success, #16a34a)" />
            </span>
        );
    }
    return (
        <span key={key} data-testid="inline-status-glyph" data-status={visual.status} style={inlineGlyphWrapStyle} aria-hidden>
            <StatusGlyph kind={visual.status} size={14} />
        </span>
    );
}

/**
 * Expand plain text: semantic pictographs → SVG glyphs; residual decorative clusters omitted.
 * Used for prose, table cells, bold/italic inners (not fenced code / inline code).
 */
function expandPlainTextWithIcons(text: string, keyPrefix: string): React.ReactNode[] {
    if (!text) return [];
    if (!textMayContainPictograph(text)) return [text];
    const segments = splitTextByPictographClusters(text);
    const out: React.ReactNode[] = [];
    let i = 0;
    for (const seg of segments) {
        if (typeof seg === "string") {
            if (seg) out.push(seg);
            continue;
        }
        out.push(renderInlineMarkGlyph(seg.visual, `${keyPrefix}-mk-${i++}`));
    }
    return out;
}

/** Push plain text into parts, expanding status/star marks to SVG. */
function pushPlainText(parts: React.ReactNode[], text: string, keyPrefix: string, keyRef: { n: number }): void {
    if (!text) return;
    if (!textMayContainPictograph(text)) {
        parts.push(text);
        return;
    }
    for (const node of expandPlainTextWithIcons(text, `${keyPrefix}-${keyRef.n++}`)) {
        parts.push(node);
    }
}

function stripPathWrapping(s: string): string {
    let value = s.trim();
    let previous = "";
    while (value !== previous) {
        previous = value;
        value = value.replace(pathLeadingWrappingPattern, "");
        value = value.replace(pathTrailingWrapperPunctuationPattern, "$1");
        value = value.replace(pathTrailingPunctuationPattern, "");
        value = value.replace(pathTrailingWrappingPattern, "");
    }
    return value;
}

function isPathQuoteChar(value: string): boolean {
    return value.length === 1 && pathQuoteCharsPattern.test(value);
}

function looksLikeFilePath(s: string): boolean {
    const value = stripPathWrapping(s);
    if (/^[A-Za-z]:\\/.test(value)) return true;
    if (/^(~|\/(?:Users|home|tmp|var|opt|etc|usr))[/\\]/.test(value)) return true;
    return false;
}
/**
 * Extract a file path from a string that may contain a path with surrounding noise.
 * Uses the same patterns as the inline regex path groups to find the path substring.
 */
function extractPathFromContent(s: string): string | null {
    // Home paths accept both ~/… and ~\… (Windows-style portable paths).
    const m = s.match(/([A-Za-z]:\\[^\n\r*?"<>|,\u3000-\u303f\u4e00-\u9fff\uff00-\uffef]+\\)(?=[`'"\u2018\u2019\u201c\u201d\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]|$)|([A-Za-z]:\\[^\n\r*?"<>|:,\u3000-\u303f\uff00-\uffef]+\.\w+)|([A-Za-z]:\\[^\n\r\s*?"<>|:,\u3000-\u303f\u4e00-\u9fff\uff00-\uffef]+[^\n\r\s*?"<>|:,\u3000-\u303f\u4e00-\u9fff\uff00-\uffef\\])|((~[/\\]|\/(?:Users|home|tmp|var|opt|etc|usr)\/)[^\n\r*?"<>|:,\u3000-\u303f\uff00-\uffef]+\.\w+)|((~[/\\]|\/(?:Users|home|tmp|var|opt|etc|usr)\/)[\w/.\-\\]+)/);
    return m ? m[0] : null;
}

function renderPathLink(filePath: string, key: number, t: Theme): React.ReactNode {
    const display = stripPathWrapping(filePath);
    const style: React.CSSProperties = {
        color: t.pathColor,
        textDecoration: "underline",
        textDecorationStyle: "dotted",
        textUnderlineOffset: "2px",
        cursor: "pointer",
        ...inlineWrapStyle,
    };
    return (
        <a key={key}
           href="#"
           onClick={(event) => openFileInFolder(event, display)}
           style={style}
           title={display}
        >{display}</a>
    );
}
// CJK exclusion ranges used in path-detection regexes below:
//   \u3000-\u303f  CJK symbols & punctuation (、。「」【】)
//   \u4e00-\u9fff  CJK unified ideographs (common Chinese/Japanese/Korean characters)
//   \uff00-\uffef  fullwidth forms (，。（）：！etc)
// These prevent path regexes from greedily matching through Chinese text.
//
// EXCEPTION: Groups anchored by a file extension (`.ext` at the end) ALLOW CJK ideographs
// (\u4e00-\u9fff) in the path, but still exclude CJK punctuation (\u3000-\u303f) and
// fullwidth forms (\uff00-\uffef). The extension provides a reliable termination point,
// and CJK punctuation serves as natural sentence delimiters in Chinese text.
// Only directory groups (ending with `\`) and bare groups (no anchor) exclude ALL CJK.
// Windows paths + home/Unix paths (with extension OR bare dir like ~/.maclaw/workspace/).
const codeBlockPathPattern = /([A-Za-z]:\\[^\n\r*?"<>|,\u3000-\u303f\u4e00-\u9fff\uff00-\uffef]+\\)(?=[`'"\u2018\u2019\u201c\u201d\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]|$)|([A-Za-z]:\\[^\n\r*?"<>|:,\u3000-\u303f\uff00-\uffef]+\.\w+)|((~[/\\]|\/(?:Users|home|tmp|var|opt|etc|usr)\/)[^\n\r*?"<>|:,\u3000-\u303f\uff00-\uffef]+\.\w+)|((~[/\\]|\/(?:Users|home|tmp|var|opt|etc|usr)\/)[\w/.\-\\]+)/g;
function renderCodePathLink(filePath: string, key: string, t: Theme): React.ReactNode {
    const display = stripPathWrapping(filePath);
    return <a key={key} href="#" onClick={(event) => openFileInFolder(event, display)} style={{ color: t.pathColor, textDecoration: "underline", textDecorationStyle: "dotted", textUnderlineOffset: "2px", cursor: "pointer" }} title={display}>{display}</a>;
}
function renderCodeBlockText(text: string, t: Theme): React.ReactNode[] {
    const parts: React.ReactNode[] = [];
    let lastIndex = 0;
    let idx = 0;
    const removeTrailingQuoteFromPreviousText = () => {
        const last = parts[parts.length - 1];
        if (typeof last !== "string") return;
        parts[parts.length - 1] = last.replace(pathTrailingWrappingPattern, "");
    };
    codeBlockPathPattern.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = codeBlockPathPattern.exec(text)) !== null) {
        if (match.index > lastIndex) parts.push(text.slice(lastIndex, match.index));
        const raw = match[0];
        const nextChar = text[codeBlockPathPattern.lastIndex] || "";
        if (isPathQuoteChar(text[match.index - 1] || "") && isPathQuoteChar(nextChar)) {
            removeTrailingQuoteFromPreviousText();
            codeBlockPathPattern.lastIndex += 1;
        }
        const display = stripPathWrapping(raw);
        if (display.length !== raw.length) codeBlockPathPattern.lastIndex -= raw.length - display.length;
        parts.push(renderCodePathLink(display, "code-path-" + idx++, t));
        lastIndex = codeBlockPathPattern.lastIndex;
    }
    if (lastIndex < text.length) parts.push(text.slice(lastIndex));
    return parts;
}
function renderInlineMarkdownRestored(text: string, t: Theme): React.ReactNode[] {
    if (!text) return ["\u00A0"];
    const parts: React.ReactNode[] = [];
    // Priority order matters:
    // 1. Backtick-wrapped content containing a file path (detected via path pattern inside backticks)
    // 2. Generic backtick inline code
    // 3. Bold, italic, links
    // 4. Bare file paths
    //
    // Mechanism: Instead of matching all backtick content with one group then guessing if it's a path,
    // we match "backtick containing a path" as a SEPARATE higher-priority group. The regex engine
    // guarantees alternation order = priority. This eliminates the need for post-match heuristics.
    //
    // Bare path groups (7-12):
    //   Group 7: Windows dir — ends with \ (e.g. C:\Users\test\)     — allows internal spaces, excludes CJK
    //   Group 8: Windows file — ends with .ext (e.g. C:\file.txt)    — allows CJK ideographs, excludes CJK punctuation/fullwidth
    //   Group 9: Windows bare — no \ or .ext anchor (e.g. D:\game2)  — NO spaces, excludes CJK
    //   Group 10-11: Home/Unix file — ends with .ext (e.g. ~/file.txt or ~\file.txt)
    //   Group 12-13: Home/Unix bare — e.g. ~/projects or ~\.maclaw\workspace
    const re = /(`[^`]*[A-Za-z]:\\[^`]+`)|(`[^`]*(?:~[/\\]|\/(?:Users|home|tmp|var|opt|etc|usr)\/)[^`]+`)|(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^\s*][^*]*?\*)|(\[[^\]]+\]\([^)]+\))|([A-Za-z]:\\[^\n\r*?"<>|,\u3000-\u303f\u4e00-\u9fff\uff00-\uffef]+\\)(?=[`'"\u2018\u2019\u201c\u201d\s,;:!?\u3002\uff0c\uff1b\uff1a\uff01\uff1f\uff09\]]|$)|([A-Za-z]:\\[^\n\r*?"<>|:,\u3000-\u303f\uff00-\uffef]+\.\w+)|([A-Za-z]:\\[^\n\r\s*?"<>|:,\u3000-\u303f\u4e00-\u9fff\uff00-\uffef]+[^\n\r\s*?"<>|:,\u3000-\u303f\u4e00-\u9fff\uff00-\uffef\\])(?=[\s,;:!?\u3000-\u303f\u4e00-\u9fff\uff00-\uffef`'"\u2018\u2019\u201c\u201d()\[\]]|$)|((~[/\\]|\/(?:Users|home|tmp|var|opt|etc|usr)\/)[^\n\r*?"<>|:,\u3000-\u303f\uff00-\uffef]+\.\w+)|((~[/\\]|\/(?:Users|home|tmp|var|opt|etc|usr)\/)[\w/.\-\\]+)/g;
    let lastIndex = 0;
    let idx = 0;
    const keyRef = { n: 0 };
    const removeTrailingQuoteFromPreviousText = () => {
        const last = parts[parts.length - 1];
        if (typeof last !== "string") return;
        parts[parts.length - 1] = last.replace(pathTrailingWrappingPattern, "");
    };
    while (true) {
        const match = re.exec(text);
        if (!match) break;
        if (match.index > lastIndex) pushPlainText(parts, text.slice(lastIndex, match.index), "t", keyRef);
        const m = match[0];
        if (match[1] || match[2]) {
            // Backtick-wrapped content that contains a file path pattern.
            // Distinguish "content IS a path" from "content CONTAINS a path as a substring".
            // If the extracted path covers at least 70% of the inner content, the content's primary
            // purpose is to denote a file path. Otherwise it's code that happens to mention a path.
            const inner = m.slice(1, -1);
            const path = extractPathFromContent(inner);
            if (path && path.length >= inner.trimStart().length * 0.7) {
                parts.push(renderPathLink(path, idx++, t));
            } else {
                // Path is a minor substring; render as inline code (keep raw; no emoji swap in code).
                parts.push(<code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em", ...inlineWrapStyle }}>{inner}</code>);
            }
        } else if (match[3]) {
            // Generic inline code (no path inside) — preserve source glyphs inside code.
            const inner = m.slice(1, -1);
            parts.push(<code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em", ...inlineWrapStyle }}>{inner}</code>);
        } else if (match[4]) {
            // Bold
            const inner = m.slice(2, -2);
            if (looksLikeFilePath(inner)) {
                parts.push(renderPathLink(inner, idx++, t));
            } else if (inner.startsWith("`") && inner.endsWith("`") && inner.length > 2) {
                // Bold-wrapped inline code: **`code`** → render as bold code (strip backticks)
                const codeContent = inner.slice(1, -1);
                parts.push(<code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em", fontWeight: 700, ...inlineWrapStyle }}>{codeContent}</code>);
            } else {
                parts.push(
                    <strong key={idx++} style={{ color: t.boldColor, fontWeight: 700, ...inlineWrapStyle }}>
                        {expandPlainTextWithIcons(inner, `b-${keyRef.n++}`)}
                    </strong>,
                );
            }
        } else if (match[5]) {
            const inner = m.slice(1, -1);
            if (inner.startsWith("`") && inner.endsWith("`") && inner.length > 2) {
                // Italic-wrapped inline code: *`code`* → render as italic code (strip backticks)
                const codeContent = inner.slice(1, -1);
                parts.push(<code key={idx++} style={{ background: t.codeBg, color: t.codeText, padding: "1px 4px", borderRadius: "3px", fontSize: "0.92em", fontStyle: "italic", ...inlineWrapStyle }}>{codeContent}</code>);
            } else {
                parts.push(
                    <em key={idx++} style={{ color: t.italicColor, ...inlineWrapStyle }}>
                        {expandPlainTextWithIcons(inner, `i-${keyRef.n++}`)}
                    </em>,
                );
            }
        } else if (match[6]) {
            const lm = m.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
            if (lm) {
                const href = lm[2];
                const label = expandPlainTextWithIcons(lm[1], `a-${keyRef.n++}`);
                if (/^https?:\/\//i.test(href)) {
                    parts.push(<a key={idx++} href="#" onClick={(e) => { e.preventDefault(); BrowserOpenURL(href); }} style={{ color: t.linkColor, textDecoration: "underline", cursor: "pointer", ...inlineWrapStyle }}>{label}</a>);
                } else if (looksLikeFilePath(href)) {
                    const filePath = stripPathWrapping(href);
                    parts.push(<a key={idx++} href="#" onClick={(event) => openFileInFolder(event, filePath)} style={{ color: t.pathColor, textDecoration: "underline", textDecorationStyle: "dotted", textUnderlineOffset: "2px", cursor: "pointer", ...inlineWrapStyle }} title={filePath}>{label}</a>);
                } else {
                    parts.push(<span key={idx++} style={{ color: t.linkColor, ...inlineWrapStyle }}>{label}</span>);
                }
            } else {
                pushPlainText(parts, m, "t", keyRef);
            }
        } else if (match[7] || match[8] || match[9] || match[10] || match[12]) {
            const nextChar = text[re.lastIndex] || "";
            if (isPathQuoteChar(text[match.index - 1] || "") && isPathQuoteChar(nextChar)) {
                removeTrailingQuoteFromPreviousText();
                re.lastIndex += 1;
            }
            const filePath = stripPathWrapping(m);
            if (filePath.length !== m.length) re.lastIndex -= (m.length - filePath.length);
            parts.push(renderPathLink(filePath, idx++, t));
        }
        lastIndex = re.lastIndex;
    }
    if (lastIndex < text.length) pushPlainText(parts, text.slice(lastIndex), "t", keyRef);
    return parts.length > 0 ? parts : ["\u00A0"];
}

type InlineMathSegment =
    | { kind: "text"; value: string }
    | { kind: "math"; value: string };

function isEscapedAt(text: string, index: number): boolean {
    let slashCount = 0;
    for (let cursor = index - 1; cursor >= 0 && text[cursor] === "\\"; cursor--) slashCount++;
    return slashCount % 2 === 1;
}

function isInlineMathBoundaryBefore(text: string, index: number): boolean {
    const previous = text[index - 1] || "";
    // TeX delimiters do not begin in the middle of identifiers, prices, or a
    // word such as `US$`. This also prevents a lone closing dollar in prose from
    // swallowing a later formula.
    // Chinese prose commonly omits the space before a formula ("令$x$...").
    // Treat Han characters as natural prose boundaries while keeping `$` inside
    // Latin identifiers (for example `price$USD`) untouched.
    return !previous || !/[\p{L}\p{N}_]/u.test(previous) || /\p{Script=Han}/u.test(previous);
}

function isInlineMathBoundaryAfter(text: string, index: number): boolean {
    const next = text[index + 1] || "";
    return !next || !/[\p{L}\p{N}_]/u.test(next) || /\p{Script=Han}/u.test(next);
}

function findUnescapedDelimiter(text: string, delimiter: string, from: number): number {
    let index = text.indexOf(delimiter, from);
    while (index !== -1 && isEscapedAt(text, index)) index = text.indexOf(delimiter, index + delimiter.length);
    return index;
}

/**
 * Split only the two inline LaTex forms we support. Backtick code spans remain
 * opaque here so `$...$` in source code is never interpreted as mathematics.
 */
function splitInlineMath(text: string): InlineMathSegment[] {
    const segments: InlineMathSegment[] = [];
    let plainStart = 0;
    let index = 0;

    const pushMath = (end: number, value: string) => {
        if (plainStart < index) segments.push({ kind: "text", value: text.slice(plainStart, index) });
        segments.push({ kind: "math", value });
        index = end;
        plainStart = end;
    };

    while (index < text.length) {
        if (text[index] === "`") {
            let fenceLength = 1;
            while (text[index + fenceLength] === "`") fenceLength++;
            const close = text.indexOf("`".repeat(fenceLength), index + fenceLength);
            index = close === -1 ? text.length : close + fenceLength;
            continue;
        }

        if (text.startsWith("\\(", index) && !isEscapedAt(text, index)) {
            const close = findUnescapedDelimiter(text, "\\)", index + 2);
            if (close !== -1 && close > index + 2) {
                pushMath(close + 2, text.slice(index + 2, close));
                continue;
            }
        }

        if (
            text[index] === "$"
            && !isEscapedAt(text, index)
            && text[index + 1] !== "$"
            && !/\s/u.test(text[index + 1] || "")
            && isInlineMathBoundaryBefore(text, index)
            // A dollar amount ("$5" / "$1,200") is prose, not TeX. Requiring
            // an explicit closing delimiter already avoids most false positives;
            // this keeps currency with a trailing "$" legible as well.
            && !/\d/u.test(text[index + 1] || "")
        ) {
            let close = index + 1;
            while (close < text.length) {
                if (
                    text[close] === "$"
                    && !isEscapedAt(text, close)
                    && text[close + 1] !== "$"
                    && isInlineMathBoundaryAfter(text, close)
                ) break;
                close++;
            }
            if (close < text.length && close > index + 1 && !/\s/u.test(text[close - 1])) {
                pushMath(close + 1, text.slice(index + 1, close));
                continue;
            }
        }

        index++;
    }

    if (segments.length === 0) return [{ kind: "text", value: text }];
    if (plainStart < text.length) segments.push({ kind: "text", value: text.slice(plainStart) });
    return segments;
}

function renderMath(latex: string, displayMode: boolean, key: React.Key): React.ReactNode {
    let html: string;
    try {
        html = katex.renderToString(latex, {
            displayMode,
            throwOnError: false,
            strict: "ignore",
            trust: false,
            output: "htmlAndMathml",
        });
    } catch {
        // Keep malformed streaming content legible rather than losing the reply.
        return displayMode
            ? <div key={key} data-testid="assistant-display-math" style={{ margin: "6px 0", ...blockWrapStyle }}>{`$$${latex}$$`}</div>
            : <span key={key} data-testid="assistant-inline-math" style={{ verticalAlign: "middle" }}>{`$${latex}$`}</span>;
    }

    const style: React.CSSProperties = displayMode
        ? { margin: "6px 0", maxWidth: "100%", overflowX: "auto", overflowY: "hidden", ...blockWrapStyle }
        // An inline expression is part of a prose line, not an independent
        // scroll area. On Windows, the latter reserves a scrollbar that can
        // obscure short formulas even when they otherwise fit.
        : { verticalAlign: "middle" };
    return displayMode
        ? <div key={key} data-testid="assistant-display-math" style={style} dangerouslySetInnerHTML={{ __html: html }} />
        : <span key={key} data-testid="assistant-inline-math" style={style} dangerouslySetInnerHTML={{ __html: html }} />;
}

export function renderInlineMarkdown(text: string, t: Theme): React.ReactNode[] {
    const segments = splitInlineMath(text);
    if (segments.length === 1 && segments[0].kind === "text") return renderInlineMarkdownRestored(text, t);
    return segments.flatMap((segment, index) => (
        segment.kind === "math"
            ? [renderMath(segment.value, false, `math-${index}`)]
            : renderInlineMarkdownRestored(segment.value, t)
    ));
}

// Shared across every chat body line (streaming-friendly: no per-call recompile).
// GFM unordered markers (-/*/+ ) plus digital-employee bullets (U+2022 •, U+00B7 ·).
const UNORDERED_LIST_LINE_RE = /^(?:[-*+]|\u2022|\u00b7)\s+(.*)$/;

// KB image markers are emitted by the local knowledge-image tool. Accept only
// the thumbnail data URLs generated by that tool: letting model-authored marker
// text point img.src at an arbitrary http(s) URL would silently make a network
// request while rendering a chat reply.
const KB_IMAGE_DATA_URL_RE = /^data:image\/jpeg;base64,[A-Za-z0-9+/]+={0,2}$/i;
const KB_IMAGE_MARKER_RE = /^\[KB_IMAGE:([A-Za-z0-9_-]{1,200})\|([^|\]]+)\]$/;
// Mirrors knowledge.MaxKBImageMarkerDataBytes. A marker is model-visible text,
// so syntax validation alone must not allow an arbitrary data URL to be handed
// to the browser image decoder. This is intentionally a character ceiling to
// avoid decoding untrusted base64 in the WebView.
const KB_IMAGE_MAX_DECODED_BYTES = 256 * 1024;

function isSafeKBImageDataURL(value: string): boolean {
    if (!KB_IMAGE_DATA_URL_RE.test(value)) return false;
    const payload = value.slice("data:image/jpeg;base64,".length);
    if (payload.length % 4 !== 0) return false;
    const padding = payload.endsWith("==") ? 2 : payload.endsWith("=") ? 1 : 0;
    // The regex above has already guaranteed a valid quartet length. Computing
    // decoded size from its final padding avoids allocating a model-authored
    // base64 string just to enforce the same byte ceiling as the Go producer.
    if (Math.floor(payload.length / 4) * 3 - padding > KB_IMAGE_MAX_DECODED_BYTES) return false;
    // A syntactically valid data URL is not sufficient: before it reaches an
    // img element, require the same bounded JPEG-thumbnail shape produced by
    // the managed knowledge asset pipeline. The browser's onLoad below still
    // performs a complete decode before the opaque asset can be opened.
    try {
        return isBoundedKBImageJPEG(atob(payload));
    } catch {
        return false;
    }
}

// isBoundedKBImageJPEG performs a bounded JPEG header scan after base64 decode.
// It is not a replacement for browser decoding; it rejects non-JPEG bytes and
// oversized SOF dimensions synchronously, while KBImageThumbnail waits for
// onLoad before enabling the asset-ID-only open action. This mirrors the Go
// producer's JPEG and ThumbSize boundary without accepting arbitrary base64 as
// an image.
function isBoundedKBImageJPEG(bytes: string): boolean {
    const length = bytes.length;
    if (length < 12 || bytes.charCodeAt(0) !== 0xff || bytes.charCodeAt(1) !== 0xd8 || bytes.charCodeAt(length - 2) !== 0xff || bytes.charCodeAt(length - 1) !== 0xd9) {
        return false;
    }
    // Look only at the first frame marker. This intentionally avoids treating
    // a later SOF-looking byte sequence as an alternate small image after an
    // oversized frame declaration. Browser onLoad remains the definitive
    // decoder check before the asset-ID action is enabled.
    for (let offset = 2; offset + 8 < length; offset++) {
        if (bytes.charCodeAt(offset) !== 0xff) continue;
        const marker = bytes.charCodeAt(offset + 1);
        const isFrame = (marker >= 0xc0 && marker <= 0xc3)
            || (marker >= 0xc5 && marker <= 0xc7)
            || (marker >= 0xc9 && marker <= 0xcb)
            || (marker >= 0xcd && marker <= 0xcf);
        if (!isFrame) continue;
        const segmentLength = (bytes.charCodeAt(offset + 2) << 8) | bytes.charCodeAt(offset + 3);
        if (segmentLength < 8 || offset + 2 + segmentLength > length) return false;
        const height = (bytes.charCodeAt(offset + 5) << 8) | bytes.charCodeAt(offset + 6);
        const width = (bytes.charCodeAt(offset + 7) << 8) | bytes.charCodeAt(offset + 8);
        return width > 0 && height > 0 && width <= 120 && height <= 120;
    }
    return false;
}

function KBImageThumbnail({ assetId, dataUrl, theme: t }: { assetId: string; dataUrl: string; theme: Theme }) {
    const [decoded, setDecoded] = React.useState(false);
    const [failed, setFailed] = React.useState(false);
    if (failed) return null;
    return (
        <div style={{ margin: "6px 0", maxWidth: "100%", minWidth: 0, boxSizing: "border-box", overflow: "hidden" }}>
            <img
                src={dataUrl}
                alt={assetId}
                title={`Click to open: ${assetId}`}
                loading="lazy"
                style={{
                    width: "120px",
                    maxWidth: "100%",
                    maxHeight: "120px",
                    borderRadius: "6px",
                    border: `1px solid ${t.divider}`,
                    boxSizing: "border-box",
                    display: "block",
                    cursor: decoded ? "pointer" : "default",
                    transition: "transform 0.15s",
                }}
                onLoad={() => setDecoded(true)}
                onError={() => setFailed(true)}
                onClick={() => {
                    // Model-authored text may name a syntactically safe asset
                    // ID, but it cannot trigger a local asset open until the
                    // displayed bytes have completed browser JPEG decoding.
                    if (!decoded) return;
                    getWailsAppModule().then(mod => {
                        mod.KnowledgeOpenImageAsset(assetId).catch(() => {});
                    });
                }}
                onMouseEnter={(e) => { if (decoded) (e.target as HTMLElement).style.transform = "scale(1.05)"; }}
                onMouseLeave={(e) => { (e.target as HTMLElement).style.transform = ""; }}
            />
        </div>
    );
}

function renderMarkdownLine(text: string, key: string | number, t: Theme): React.ReactNode {
    const trimmed = text.trimStart();

    // KB_IMAGE marker: render an inline thumbnail. The marker contains an
    // opaque asset ID, never a local path, so agent-authored content cannot
    // cause arbitrary files to be opened.
    // Match the exact, current two-field protocol only. In particular, do not
    // revive the deprecated third path field even if it is ignored locally:
    // model output must never normalize a legacy path-carrying marker into a
    // renderable image.
    const kbImageMatch = trimmed.match(KB_IMAGE_MARKER_RE);
    if (kbImageMatch && isSafeKBImageDataURL(kbImageMatch[2])) {
        const [, assetId, dataUrl] = kbImageMatch;
        return <KBImageThumbnail key={key} assetId={assetId} dataUrl={dataUrl} theme={t} />;
    }

    const headingMatch = trimmed.match(/^(#{1,6})\s+(.+)$/);
    if (headingMatch) {
        const level = headingMatch[1].length;
        const sizes: Record<number, string> = { 1: "1.25em", 2: "1.12em", 3: "1.0em", 4: "0.92em", 5: "0.9em", 6: "0.88em" };
        const weights: Record<number, number> = { 1: 700, 2: 700, 3: 600, 4: 600, 5: 600, 6: 600 };
        const margins: Record<number, string> = { 1: "0.6em 0 0.3em", 2: "0.5em 0 0.25em", 3: "0.4em 0 0.2em", 4: "0.3em 0 0.15em", 5: "0.25em 0 0.12em", 6: "0.2em 0 0.1em" };
        return (
            <div key={key} style={{
                fontSize: sizes[level] || "1em",
                fontWeight: weights[level] || 600,
                color: t.headingColor,
                margin: margins[level] || "0.4em 0 0.2em",
                letterSpacing: level === 1 ? "0.01em" : undefined,
                ...blockWrapStyle,
            }}>
                {renderInlineMarkdown(headingMatch[2], t)}
            </div>
        );
    }

    if (/^>\s/.test(trimmed)) {
        return (
            <div key={key} style={{
                borderLeft: `1px solid ${t.quoteBorder}`,
                paddingLeft: "10px",
                color: t.quoteText,
                fontStyle: "italic",
                minHeight: "1.4em",
                margin: "2px 0",
                ...blockWrapStyle,
            }}>
                {renderInlineMarkdown(trimmed.slice(2), t)}
            </div>
        );
    }

    if (/^[-*_]{3,}\s*$/.test(trimmed)) {
        return <hr key={key} style={{ border: "none", borderTop: `1px solid ${t.divider}`, margin: "8px 0" }} />;
    }

    const unorderedListMatch = trimmed.match(UNORDERED_LIST_LINE_RE);
    if (unorderedListMatch) {
        const indentPad = orderedListIndentPadding(leadingIndentColumns(text));
        return (
            <div key={key} style={{
                paddingLeft: indentPad ? `calc(1em + ${indentPad})` : "1em",
                textIndent: "-0.7em",
                minHeight: "1.4em",
                ...blockWrapStyle,
            }}>
                <span style={{ color: t.bulletColor }}>{"\u2022"}</span>{" "}
                {renderInlineMarkdown(unorderedListMatch[1], t)}
            </div>
        );
    }

    const ordered = parseOrderedListLine(text);
    if (ordered) {
        // Keep wrap styles off the marker span (and off the flex row) so indices never
        // inherit word-break from prose and reflow mid-number. Preserve "." vs ")".
        const indentPad = orderedListIndentPadding(ordered.indentCols);
        return (
            <div key={key} style={{
                display: "flex",
                minHeight: "1.4em",
                minWidth: 0,
                paddingLeft: indentPad,
            }}>
                <span style={{ color: t.bulletColor, ...orderedListMarkerLayoutStyle }}>{ordered.marker}</span>
                <span style={{ flex: 1, ...blockWrapStyle }}>{renderInlineMarkdown(ordered.body, t)}</span>
            </div>
        );
    }

    return (
        <div key={key} style={{ minHeight: "1.4em", ...blockWrapStyle }}>
            {renderInlineMarkdown(text, t) || "\u00A0"}
        </div>
    );
}

/* Structured response rendering */

// A streamed table can leave its row label as the only cell on a line.  Keep
// that line in the current table so the table-model repair can join it with the
// following N-1 cells.
function isSplitTableRowLabel(line: string): boolean {
    const normalized = normalizeMarkdownTableLine(line);
    const cells = parseMarkdownTableCells(normalized);
    return normalized.includes("|") && cells.length === 1 && Boolean(cells[0]);
}

function renderTable(tableLines: string[], key: string, t: Theme): React.ReactNode {
    const model = buildMarkdownTableModel(tableLines);
    if (!model) return null;
    const repaired = repairMixedNarrativeTable(model);
    const { headerCells, bodyRows, columnAlignments, minTableWidth, prefix, notes } = repaired;
    const lastCol = headerCells.length - 1;
    // Inner grid lines only — the rounded shell draws the outer edge, so cells
    // skip their outer-side borders to avoid doubled 2px seams.
    const cellStyle: React.CSSProperties = { boxSizing: "border-box", overflowWrap: "anywhere", padding: "6px 10px", textAlign: "left", verticalAlign: "top", wordBreak: "break-word", fontSize: "0.9em", lineHeight: 1.5 };
    const rowHoverBg = `color-mix(in srgb, ${t.btnColor} 7%, transparent)`;
    return (
        <div key={key} style={{ width: "100%", maxWidth: "100%", minWidth: 0, boxSizing: "border-box", margin: "6px 0", whiteSpace: "normal" }}>
            {prefix && <div data-testid="markdown-table-prefix" style={{ marginBottom: 6, ...blockWrapStyle }}>{renderInlineMarkdown(prefix, t)}</div>}
            {/* Rounded, bordered shell; the scrollport clips the table corners. */}
            <div data-testid="markdown-table-block" style={{ width: "100%", maxWidth: "100%", minWidth: 0, boxSizing: "border-box", overflowX: "auto", overscrollBehaviorX: "contain", border: `1px solid ${t.fieldBorder}`, borderRadius: "10px" }}>
                <table data-testid="markdown-table" style={{ borderCollapse: "collapse", minWidth: minTableWidth, tableLayout: "fixed", width: "100%", color: t.text, whiteSpace: "normal", wordBreak: "normal" }}>
                    <thead><tr>{headerCells.map((cell, ci) => <th key={ci} style={{ ...cellStyle, textAlign: columnAlignments[ci], fontWeight: 600, background: t.codeBlockBg, color: t.headingColor, fontSize: "0.88em", letterSpacing: "0.02em", borderBottom: bodyRows.length > 0 ? `1px solid ${t.fieldBorder}` : undefined, borderRight: ci < lastCol ? `1px solid ${t.fieldBorder}` : undefined }}>{renderInlineMarkdown(cell, t)}</th>)}</tr></thead>
                    {bodyRows.length > 0 && <tbody>{bodyRows.map((row, ri) => { const cells = parseMarkdownTableCells(row); const zebraBg = ri % 2 === 1 ? t.fieldBg : ""; return <tr key={ri} style={{ background: zebraBg || undefined, transition: "background-color 140ms cubic-bezier(0.22, 1, 0.36, 1)" }} onMouseEnter={(e) => { e.currentTarget.style.background = rowHoverBg; }} onMouseLeave={(e) => { e.currentTarget.style.background = zebraBg; }}>{headerCells.map((_, ci) => <td key={ci} style={{ ...cellStyle, textAlign: columnAlignments[ci], borderBottom: ri < bodyRows.length - 1 ? `1px solid ${t.fieldBorder}` : undefined, borderRight: ci < lastCol ? `1px solid ${t.fieldBorder}` : undefined }}>{renderInlineMarkdown(cells[ci] || "", t)}</td>)}</tr>; })}</tbody>}
                </table>
            </div>
            {notes.map((note, index) => <div key={`note-${index}`} data-testid="markdown-table-note" style={{ marginTop: 6, ...blockWrapStyle }}>{renderInlineMarkdown(note, t)}</div>)}
        </div>
    );
}

export function renderContentWithCodeBlocks(content: string, t: Theme): React.ReactNode[] {
    const elements: React.ReactNode[] = [];
    // Normalize compact LLM output outside fenced code blocks.
    const normalized = normalizeInlineListMarkers(content);
    const rawLines = normalized.split("\n");
    // Compact-heading / bare-marker attach can introduce line-leading pictographs
    // after a markdown prefix (e.g. "### <pictograph> Title"); strip them for display.
    // Use line-array form to avoid an extra join/split on long streaming messages.
    // attachBareHeadingMarkers tracks code fences and display-math blocks, so
    // ordinary headings after a formula still receive the streamed repair.
    const structureLines = normalized.includes("#")
        ? attachBareHeadingMarkers(rawLines)
        : rawLines;
    const lines = prepareChatBodyLines(structureLines);
    let inCodeBlock = false;
    let codeFenceMarker = "";
    let codeBlockLines: string[] = [];
    let codeBlockLang = "";
    let tableLines: string[] = [];
    let displayMathDelimiter: "$$" | "\\[" | "" = "";
    let displayMathLines: string[] = [];
    let lineIdx = 0;

    const flushDisplayMath = () => {
        elements.push(renderMath(displayMathLines.join("\n"), true, `math-${elements.length}`));
        displayMathDelimiter = "";
        displayMathLines = [];
    };

    const flushCodeBlock = (allowMermaidRender = true) => {
        if (inCodeBlock || codeBlockLines.length > 0) {
            // A streaming response may expose an unfinished Mermaid fence for
            // several frames. Keep it as source until the closing fence arrives
            // instead of repeatedly invoking Mermaid on partial syntax.
            if (allowMermaidRender && isMermaidCodeFence(codeBlockLang)) {
                elements.push(
                    <AssistantMermaidDiagram
                        key={`mermaid-${elements.length}`}
                        code={codeBlockLines.join("\n")}
                        theme={t}
                    />,
                );
                codeBlockLines = [];
                codeBlockLang = "";
                return;
            }
            elements.push(
                <pre key={`code-${elements.length}`} style={{
                    background: t.codeBlockBg,
                    border: `1px solid ${t.codeBlockBorder}`,
                    borderRadius: "10px",
                    padding: "10px 12px",
                    margin: "6px 0",
                    fontSize: "0.88em",
                    width: "100%",
                    maxWidth: "100%",
                    minWidth: 0,
                    boxSizing: "border-box",
                    overflowX: "auto",
                    overscrollBehaviorX: "contain",
                    color: t.codeText,
                    lineHeight: 1.6,
                }}>
                    {codeBlockLang && <div style={{ color: t.codeBlockLang, fontSize: "0.8em", marginBottom: "6px", fontWeight: 500, textTransform: "uppercase", letterSpacing: "0.05em", opacity: 0.8 }}>{codeBlockLang}</div>}
                    <code>{codeBlockLines.length > 0 ? renderCodeBlockText(codeBlockLines.join("\n"), t) : "\u00A0"}</code>
                </pre>
            );
        }
        codeBlockLines = [];
        codeBlockLang = "";
        codeFenceMarker = "";
    };

    const flushTable = () => {
        if (tableLines.length === 0) return;
        // Orphan GFM separator rows (common noise before a real table) should not
        // fall through as raw "|---|---|" text in the chat bubble.
        if (tableLines.every((line) => isMarkdownTableSeparatorRow(line) || !line.trim())) {
            tableLines = [];
            return;
        }
        const rendered = renderTable(tableLines, `tbl-${elements.length}`, t);
        if (rendered) {
            elements.push(rendered);
        } else {
            for (const tableLine of tableLines) {
                if (isMarkdownTableSeparatorRow(tableLine)) continue;
                elements.push(renderMarkdownLine(tableLine, `md-fallback-${elements.length}`, t));
            }
        }
        tableLines = [];
    };

    for (const line of lines) {
        if (displayMathDelimiter) {
            const trimmed = line.trim();
            const closeDelimiter = displayMathDelimiter === "$$" ? "$$" : "\\]";
            if (trimmed === closeDelimiter) {
                flushDisplayMath();
            } else if (trimmed.endsWith(closeDelimiter)) {
                // TeX generators often put the final expression and its
                // delimiter on one line (for example `\\end{aligned}$$`).
                // Keep the expression, remove only the terminal delimiter,
                // then render the complete display block.
                displayMathLines.push(line.slice(0, line.lastIndexOf(closeDelimiter)));
                flushDisplayMath();
            } else {
                displayMathLines.push(line);
            }
            lineIdx++;
            continue;
        }
        if (!inCodeBlock && /^\|+$/.test(line.trim())) {
            continue;
        }
        const fenceMatch = line.trimStart().match(/^(`{3,}|~{3,})(.*)$/);
        if (fenceMatch) {
            flushTable();
            if (inCodeBlock) {
                const marker = fenceMatch[1];
                // A closing fence must use the same character and be at least
                // as long as its opener. Otherwise it belongs to the code body.
                if (marker[0] === codeFenceMarker[0] && marker.length >= codeFenceMarker.length && !fenceMatch[2].trim()) {
                    flushCodeBlock();
                    inCodeBlock = false;
                } else {
                    codeBlockLines.push(line);
                }
            } else {
                inCodeBlock = true;
                codeFenceMarker = fenceMatch[1];
                codeBlockLang = fenceMatch[2].trim();
            }
        } else if (inCodeBlock) {
            codeBlockLines.push(line);
        } else if (line.trim().startsWith("$$") && line.trim().endsWith("$$") && line.trim().length > 4) {
            flushTable();
            const trimmed = line.trim();
            elements.push(renderMath(trimmed.slice(2, -2), true, `math-${elements.length}`));
        } else if (line.trim().startsWith("$$") && line.trim().length > 2) {
            flushTable();
            displayMathDelimiter = "$$";
            displayMathLines = [line.trim().slice(2)];
        } else if (line.trim().startsWith("\\[") && line.trim().endsWith("\\]") && line.trim().length > 4) {
            flushTable();
            const trimmed = line.trim();
            elements.push(renderMath(trimmed.slice(2, -2), true, `math-${elements.length}`));
        } else if (line.trim().startsWith("\\[") && line.trim().length > 2) {
            flushTable();
            displayMathDelimiter = "\\[";
            displayMathLines = [line.trim().slice(2)];
        } else if (line.trim() === "$$" || line.trim() === "\\[") {
            flushTable();
            displayMathDelimiter = line.trim() as "$$" | "\\[";
            displayMathLines = [];
        } else if (isMarkdownTableRow(line) || (tableLines.length > 0 && isSplitTableRowLabel(line))) {
            // Strip list markers so "- | a | b |" stays inside the table model.
            // buildMarkdownTableModel also normalizes; doing it here keeps the
            // in-progress buffer consistent while streaming.
            tableLines.push(normalizeMarkdownTableLine(line));
        } else {
            flushTable();
            elements.push(renderMarkdownLine(line, `md-${lineIdx}`, t));
        }
        lineIdx++;
    }
    if (displayMathDelimiter) {
        // The response is still streaming or malformed. Preserve the source instead
        // of attempting KaTeX on a partial block.
        elements.push(renderMarkdownLine(displayMathDelimiter, `md-${lineIdx++}`, t));
        for (const line of displayMathLines) elements.push(renderMarkdownLine(line, `md-${lineIdx++}`, t));
    }
    if (inCodeBlock) flushCodeBlock(false);
    flushTable();
    return elements;
}

function renderFields(fields: Array<{ label: string; value: string }>, t: Theme): React.ReactNode {
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", margin: "4px 0" }}>
            {fields.map((f, i) => {
                const isRecovery = f.label === "Recovery";
                const isTurn = f.label === "Turn";
                const recoveryTone = String(f.value || '').toLowerCase();
                const recoveryStyle: React.CSSProperties = isRecovery
                    ? {
                        display: "inline-flex",
                        alignItems: "center",
                        padding: "2px 8px",
                        borderRadius: "999px",
                        fontWeight: 700,
                        background: recoveryTone.includes('failed')
                            ? "rgba(180, 35, 24, 0.10)"
                            : recoveryTone.includes('partial')
                                ? "rgba(100, 116, 139, 0.10)"
                                : "rgba(79, 127, 111, 0.12)",
                        color: recoveryTone.includes('failed')
                            ? "#b42318"
                            : recoveryTone.includes('partial')
                                ? (t.isDark ? "#8a9ab0" : "#64748b")
                                : "#4f7f6f",
                    }
                    : { color: t.text };
                const turnChipStyle: React.CSSProperties = isTurn
                    ? {
                        background: t.isDark ? "rgba(52, 152, 219, 0.14)" : "rgba(52, 152, 219, 0.10)",
                        border: `1px solid ${t.isDark ? "rgba(52, 152, 219, 0.35)" : "rgba(52, 152, 219, 0.28)"}`,
                        borderRadius: "999px",
                        padding: "3px 10px",
                        fontSize: "11px",
                        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
                    }
                    : {
                        background: t.fieldBg,
                        border: `1px solid ${t.fieldBorder}`,
                        borderRadius: "4px",
                        padding: "4px 8px",
                        fontSize: "12px",
                    };
                return (
                    <div key={`field-${i}`} data-testid={isTurn ? "turn-meta-chip" : "field-card"} style={turnChipStyle}>
                        <span style={{ color: t.fieldLabel, marginRight: "6px" }}>{f.label}:</span>
                        <span data-testid={isRecovery ? 'recovery-badge' : undefined} style={recoveryStyle}>{f.value}</span>
                    </div>
                );
            })}
        </div>
    );
}

function ActionButtons({ actions, executeAction, theme, lang = "en" }: {
    actions: ChatAction[];
    executeAction: (command: string) => void;
    theme: Theme;
    lang?: string;
}): React.ReactElement {
    const [firedIndex, setFiredIndex] = React.useState<number | null>(null);
    const t = theme;
    return (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "6px", margin: "4px 0" }}>
            {actions.map((a, i) => {
                const isPrimary = a.style === "primary";
                const fired = firedIndex !== null;
                const isThis = firedIndex === i;
                return (
                    <button
                        key={`action-${i}`}
                        data-testid="action-button"
                        disabled={fired}
                        onClick={() => {
                            if (firedIndex !== null) return;
                            setFiredIndex(i);
                            executeAction(a.command);
                        }}
                        style={{
                            ...baseInputBtnStyle,
                            background: isPrimary ? t.sendBtnBg : "transparent",
                            color: isPrimary ? t.sendBtnColor : (a.style === "danger" ? t.errorText : t.btnColor),
                            borderColor: a.style === "danger" ? t.errorText : (isPrimary ? t.sendBtnBg : t.btnBorder),
                            fontSize: "12px",
                            padding: "4px 10px",
                            width: "auto",
                            height: "auto",
                            maxWidth: "100%",
                            minWidth: "36px",
                            minHeight: "28px",
                            lineHeight: 1.35,
                            overflowWrap: "anywhere",
                            textAlign: "center",
                            whiteSpace: "normal",
                            opacity: fired ? (isThis ? 0.7 : 0.4) : 1,
                            cursor: fired ? "default" : "pointer",
                        }}
                    >
                        {formatActionLabel(a, lang)}
                    </button>
                );
            })}
        </div>
    );
}

function renderActions(
    actions: ChatAction[],
    executeAction: (command: string) => void,
    t: Theme,
    lang = "en",
): React.ReactNode {
    return <ActionButtons actions={actions} executeAction={executeAction} theme={t} lang={lang} />;
}

function formatActionLabel(action: ChatAction, lang: string): string {
    const normalizedLabel = (action.label || '').trim().toLowerCase();
    if (/^__resume_unfinished__\s+\S+$/.test(action.command)) {
        return localizeText(lang, "Resume previous task", "\u7ee7\u7eed\u4e0a\u6b21\u4efb\u52a1", "\u7e7c\u7e8c\u4e0a\u6b21\u4efb\u52d9");
    }
    if (/^__dismiss_unfinished__\s+\S+$/.test(action.command) || action.command === "__start_new_task__") {
        return localizeText(lang, "Start new task", "\u5f00\u59cb\u65b0\u4efb\u52a1", "\u958b\u59cb\u65b0\u4efb\u52d9");
    }
    if (/^__resume_session__\s+\S+$/.test(action.command)) {
        return localizeText(lang, "Resume session", "\u6062\u590d\u4f1a\u8bdd", "\u6062\u5fa9\u6703\u8a71");
    }
    if (/^__dismiss_recoverable_session__\s+\S+$/.test(action.command)) {
        return localizeText(lang, "Dismiss session", "\u5ffd\u7565\u4f1a\u8bdd", "\u5ffd\u7565\u6703\u8a71");
    }
    if (/^__confirm_execution__\s+\S+$/.test(action.command) && (!normalizedLabel || normalizedLabel === 'confirm and start')) {
        return localizeText(lang, "Confirm and start", "\u786e\u8ba4\u5e76\u5f00\u59cb", "\u78ba\u8a8d\u4e26\u958b\u59cb");
    }
    if (/^__cancel_execution__\s+\S+$/.test(action.command) && (!normalizedLabel || normalizedLabel === 'cancel')) {
        return localizeText(lang, "Cancel", "\u53d6\u6d88", "\u53d6\u6d88");
    }
    return action.label;
}

function renderConfirmationList(testId: string, title: string, items: string[], t: Theme): React.ReactNode {
    if (items.length === 0) return null;
    return (
        <div data-testid={testId} style={{ marginTop: "8px" }}>
            <div style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "4px" }}>{title}</div>
            <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
                {items.map((item, index) => (
                    <div key={`${testId}-${index}`} style={{ minHeight: "1.4em", color: t.text }}>
                        <span style={{ color: t.bulletColor }}>{"\u2022"}</span>{" "}
                        {renderInlineMarkdown(item, t)}
                    </div>
                ))}
            </div>
        </div>
    );
}

function renderConfirmationCard(
    confirmation: ChatConfirmation,
    actions: ChatAction[] | undefined,
    executeAction: (command: string) => void,
    t: Theme,
    lang: string,
): React.ReactNode {
    const targetPaths = confirmation.targetPaths || [];
    const plannedActions = confirmation.plannedActions || [];
    const riskFlags = confirmation.riskFlags || [];
    const revisionHints = confirmation.revisionHints || [];
    const taskType = confirmation.taskType?.trim() || '';
    const status = confirmation.status?.trim() || '';
    const labels = confirmation.labels;
    const titleLabel = labels?.title || localizeText(lang, "Pre-execution confirmation", "\u6267\u884c\u524d\u786e\u8ba4", "\u57f7\u884c\u524d\u78ba\u8a8d");
    const statusLabel = labels?.status || localizeText(lang, "Status", "\u72b6\u6001", "\u72c0\u614b");
    const targetPathsLabel = labels?.target_paths || localizeText(lang, "Target paths", "\u76ee\u6807\u8def\u5f84", "\u76ee\u6a19\u8def\u5f91");
    const plannedActionsLabel = labels?.planned_actions || localizeText(lang, "Planned actions", "\u8ba1\u5212\u64cd\u4f5c", "\u8a08\u5283\u64cd\u4f5c");
    const riskFlagsLabel = labels?.risk_flags || localizeText(lang, "Risk flags", "\u98ce\u9669\u6807\u8bb0", "\u98a8\u96aa\u6a19\u8a18");
    const revisionHintsLabel = labels?.revision_hints || localizeText(lang, "Revision hints", "\u4fee\u8ba2\u63d0\u793a", "\u4fee\u8a02\u63d0\u793a");
    return (
        <div
            data-testid="confirmation-card"
            style={{
                marginTop: "8px",
                padding: "10px 12px",
                borderRadius: "8px",
                border: `1px solid ${t.inputBarBorder}`,
                background: t.fieldBg,
            }}
        >
            <div style={{ color: t.headingColor, fontWeight: 700, marginBottom: "6px" }}>
                {taskType ? `${titleLabel} - ${formatConfirmationTaskType(taskType, lang)}` : titleLabel}
            </div>
            {status && (
                <div data-testid="confirmation-status" style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "6px" }}>
                    {statusLabel}: {formatConfirmationStatus(status, lang)}
                </div>
            )}
            <div data-testid="confirmation-summary" style={{ color: t.text, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>
                {renderContentWithCodeBlocks(confirmation.summary, t)}
            </div>
            {renderConfirmationList("confirmation-target-paths", targetPathsLabel, targetPaths, t)}
            {renderConfirmationList("confirmation-planned-actions", plannedActionsLabel, plannedActions, t)}
            {renderConfirmationList("confirmation-risk-flags", riskFlagsLabel, riskFlags, t)}
            {renderConfirmationList("confirmation-revision-hints", revisionHintsLabel, revisionHints, t)}
            {actions && actions.length > 0 && renderActions(actions, executeAction, t, lang)}
        </div>
    );
}

function formatConfirmationStatus(status: string, lang: string): string {
    const normalized = status.trim().toLowerCase();
    const labels: Record<string, string> = {
        pending: localizeText(lang, "Pending", "\u5f85\u786e\u8ba4", "\u5f85\u78ba\u8a8d"),
        running: localizeText(lang, "Running", "\u6267\u884c\u4e2d", "\u57f7\u884c\u4e2d"),
        confirmed: localizeText(lang, "Confirmed", "\u5df2\u786e\u8ba4", "\u5df2\u78ba\u8a8d"),
        cancelled: localizeText(lang, "Cancelled", "\u5df2\u53d6\u6d88", "\u5df2\u53d6\u6d88"),
        canceled: localizeText(lang, "Cancelled", "\u5df2\u53d6\u6d88", "\u5df2\u53d6\u6d88"),
        expired: localizeText(lang, "Expired", "\u5df2\u8fc7\u671f", "\u5df2\u904e\u671f"),
    };
    return labels[normalized] || status;
}

function formatConfirmationTaskType(taskType: string, lang: string): string {
    const normalized = taskType.trim().toLowerCase();
    const labels: Record<string, string> = {
        coding: localizeText(lang, "Coding", "\u4ee3\u7801\u4efb\u52a1", "\u7a0b\u5f0f\u78bc\u4efb\u52d9"),
        ssh: localizeText(lang, "SSH", "\u8fdc\u7a0b\u4efb\u52a1", "\u9060\u7aef\u4efb\u52d9"),
        ambiguous: localizeText(lang, "Ambiguous", "\u5f85\u6f84\u6e05\u4efb\u52a1", "\u5f85\u91d0\u6e05\u4efb\u52d9"),
    };
    return labels[normalized] || taskType;
}

function formatUnfinishedSlotStatus(status: string, lang: string) {
    const key = status.trim().toLowerCase();
    const normalized = key.replace(/_/g, " ");
    if (!lang.trim().toLowerCase().startsWith("zh")) return `Status: ${normalized}`;
    const labels: Record<string, string> = {
        pending_resume: localizeText(lang, "Pending resume", "\u5f85\u7ee7\u7eed", "\u5f85\u7e7c\u7e8c"),
        interrupted: localizeText(lang, "Interrupted", "\u5df2\u4e2d\u65ad", "\u5df2\u4e2d\u65b7"),
        max_rounds_reached: localizeText(lang, "Max rounds reached", "\u8fbe\u5230\u6700\u5927\u8f6e\u6b21", "\u9054\u5230\u6700\u5927\u8f2a\u6b21"),
        resumed: localizeText(lang, "Resumed", "\u5df2\u6062\u590d", "\u5df2\u6062\u5fa9"),
        dismissed: localizeText(lang, "Dismissed", "\u5df2\u5ffd\u7565", "\u5df2\u5ffd\u7565"),
        completed: localizeText(lang, "Completed", "\u5df2\u5b8c\u6210", "\u5df2\u5b8c\u6210"),
        failed: localizeText(lang, "Failed", "\u5df2\u5931\u8d25", "\u5df2\u5931\u6557"),
        cancelled: localizeText(lang, "Cancelled", "\u5df2\u53d6\u6d88", "\u5df2\u53d6\u6d88"),
        canceled: localizeText(lang, "Cancelled", "\u5df2\u53d6\u6d88", "\u5df2\u53d6\u6d88"),
    };
    return `\u72b6\u6001\uff1a${labels[key] || localizeText(lang, "Unknown", "\u672a\u77e5", "\u672a\u77e5")}`;
}

function formatUnfinishedSlotSummary(summary: string, lang: string): string {
    const normalized = summary.trim();
    if (/^Previous task stopped making progress and was moved to recovery\.?$/i.test(normalized)) {
        return localizeText(
            lang,
            "Previous task stopped making progress and was moved to recovery.",
            "\u4e0a\u6b21\u4efb\u52a1\u505c\u6b62\u63a8\u8fdb\uff0c\u5df2\u79fb\u5165\u6062\u590d\u72b6\u6001\u3002",
            "\u4e0a\u6b21\u4efb\u52d9\u505c\u6b62\u63a8\u9032\uff0c\u5df2\u79fb\u5165\u6062\u5fa9\u72c0\u614b\u3002",
        );
    }
    if (/^The previous task stopped before completion\.?$/i.test(normalized)) {
        return localizeText(
            lang,
            "The previous task stopped before completion.",
            "\u4e0a\u6b21\u4efb\u52a1\u5c1a\u672a\u5b8c\u6210\u5c31\u5df2\u505c\u6b62\u3002",
            "\u4e0a\u6b21\u4efb\u52d9\u5c1a\u672a\u5b8c\u6210\u5c31\u5df2\u505c\u6b62\u3002",
        );
    }
    return summary;
}

function formatUnfinishedSlotNotice(content: string, slot: ChatUnfinishedSlot | undefined, lang: string): string {
    if (!slot) return content;
    const normalized = content.trim();
    const isUnfinishedNotice = /^Detected an unfinished task:/i.test(normalized)
        || /^\u68c0\u6d4b\u5230\u672a\u5b8c\u6210\u4efb\u52a1/.test(normalized)
        || /^\u5075\u6e2c\u5230\u672a\u5b8c\u6210\u4efb\u52d9/.test(normalized);
    if (!isUnfinishedNotice) return content;
    const rawTitle = (slot.title || slot.summary || slot.projectPath || localizeText(lang, "Previous unfinished task", "\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52a1", "\u4e0a\u6b21\u672a\u5b8c\u6210\u4efb\u52d9")).trim();
    const title = (slot.title ? rawTitle : formatUnfinishedSlotSummary(rawTitle, lang)).replace(/[.\u3002]+$/u, "");
    return localizeText(
        lang,
        `Detected an unfinished task: ${title}. Choose resume to continue it.`,
        `\u68c0\u6d4b\u5230\u672a\u5b8c\u6210\u4efb\u52a1\uff1a${title}\u3002\u9009\u62e9\u201c\u7ee7\u7eed\u4e0a\u6b21\u4efb\u52a1\u201d\u53ef\u7ee7\u7eed\u3002`,
        `\u5075\u6e2c\u5230\u672a\u5b8c\u6210\u4efb\u52d9\uff1a${title}\u3002\u9078\u64c7\u300c\u7e7c\u7e8c\u4e0a\u6b21\u4efb\u52d9\u300d\u53ef\u7e7c\u7e8c\u3002`,
    );
}

function renderUnfinishedSlotCard(
    slot: ChatUnfinishedSlot,
    executeAction: (command: string) => void,
    t: Theme,
    lang: string,
): React.ReactNode {
    const actions = slot.actions || [];
    const status = slot.status?.trim().toLowerCase();
    const cardTitle = status === 'resumed'
        ? localizeText(lang, "Task resumed", "\u4efb\u52a1\u5df2\u7ee7\u7eed", "\u4efb\u52d9\u5df2\u7e7c\u7e8c")
        : status === 'completed'
            ? localizeText(lang, "Task completed", "\u4efb\u52a1\u5df2\u5b8c\u6210", "\u4efb\u52d9\u5df2\u5b8c\u6210")
            : localizeText(lang, "Unfinished item", "\u672a\u5b8c\u6210\u9879", "\u672a\u5b8c\u6210\u9805");
    return (
        <div
            data-testid="unfinished-slot-card"
            style={{
                marginTop: "8px",
                padding: "10px 12px",
                borderRadius: "8px",
                border: `1px solid ${t.inputBarBorder}`,
                background: t.fieldBg,
            }}
        >
            <div style={{ color: t.headingColor, fontWeight: 700, marginBottom: "6px" }}>
                {cardTitle}
            </div>
            {slot.status && (
                <div data-testid="unfinished-slot-status" style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "6px" }}>
                    {formatUnfinishedSlotStatus(slot.status, lang)}
                </div>
            )}
            {slot.title && (
                <div data-testid="unfinished-slot-title" style={{ color: t.text, fontWeight: 600, marginBottom: "4px" }}>
                    {slot.title}
                </div>
            )}
            {slot.summary && (
                <div data-testid="unfinished-slot-summary" style={{ color: t.text, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>
                    {renderContentWithCodeBlocks(formatUnfinishedSlotSummary(slot.summary, lang), t)}
                </div>
            )}
            {slot.recoveryMode === 'requires_review' && (
                <div data-testid="unfinished-slot-review-required" style={{ color: t.fieldLabel, marginTop: "6px", whiteSpace: "pre-wrap" }}>
                    {localizeText(lang, "Review required: the previous task may have changed the workspace or caused an external side effect. Continuing restores context only; it does not retry that action.", "需要审阅：上次任务可能已修改工作区或产生外部副作用。继续只恢复上下文，不会重试该操作。", "需要審閱：上次任務可能已修改工作區或產生外部副作用。繼續只恢復上下文，不會重試該操作。")}
                </div>
            )}
            {slot.recoveryMode === 'requires_review' && slot.sideEffectState === 'local_committed' && (
                <div data-testid="unfinished-slot-workspace-review" style={{ color: t.fieldLabel, marginTop: "6px", whiteSpace: "pre-wrap" }}>
                    {localizeText(lang, "Local changes may already exist; review the workspace before continuing.", "本地修改可能已存在；继续前请检查工作区。", "本機修改可能已存在；繼續前請檢查工作區。")}
                </div>
            )}
            {slot.projectPath && (
                <div data-testid="unfinished-slot-project" style={{ color: t.pathColor, marginTop: "6px", wordBreak: "break-all" }}>
                    <a
                        href="#"
                        onClick={(event) => openFileInFolder(event, slot.projectPath!)}
                        style={{ color: t.pathColor, textDecoration: "underline", textDecorationStyle: "dotted", textUnderlineOffset: "2px", cursor: "pointer", wordBreak: "break-all" }}
                        title={slot.projectPath}
                    >
                        {slot.projectPath}
                    </a>
                </div>
            )}
            {actions.length > 0 && renderActions(actions, executeAction, t, lang)}
        </div>
    );
}

function formatSessionStatus(status: string, lang: string): string {
    const key = status.trim().toLowerCase();
    const normalized = key.replace(/_/g, " ");
    const labels: Record<string, string> = {
        starting: localizeText(lang, "Starting", "\u542f\u52a8\u4e2d", "\u555f\u52d5\u4e2d"),
        running: localizeText(lang, "Running", "\u8fd0\u884c\u4e2d", "\u57f7\u884c\u4e2d"),
        busy: localizeText(lang, "Busy", "\u5fd9\u788c", "\u5fd9\u788c"),
        waiting_input: localizeText(lang, "Waiting for input", "\u7b49\u5f85\u8f93\u5165", "\u7b49\u5f85\u8f38\u5165"),
        exited: localizeText(lang, "Exited", "\u5df2\u9000\u51fa", "\u5df2\u9000\u51fa"),
        error: localizeText(lang, "Error", "\u51fa\u9519", "\u51fa\u932f"),
        stopped: localizeText(lang, "Stopped", "\u5df2\u505c\u6b62", "\u5df2\u505c\u6b62"),
        completed: localizeText(lang, "Completed", "\u5df2\u5b8c\u6210", "\u5df2\u5b8c\u6210"),
        failed: localizeText(lang, "Failed", "\u5df2\u5931\u8d25", "\u5df2\u5931\u6557"),
        cancelled: localizeText(lang, "Cancelled", "\u5df2\u53d6\u6d88", "\u5df2\u53d6\u6d88"),
        canceled: localizeText(lang, "Cancelled", "\u5df2\u53d6\u6d88", "\u5df2\u53d6\u6d88"),
    };
    if (!lang.trim().toLowerCase().startsWith("zh")) return normalized;
    return labels[key] || localizeText(lang, "Unknown", "\u672a\u77e5", "\u672a\u77e5");
}

function labelSeparator(lang: string): string {
    return lang.trim().toLowerCase().startsWith("zh") ? "\uff1a" : ": ";
}

function renderRecoverableSessionCard(
    session: ChatRecoverableSession,
    executeAction: (command: string) => void,
    t: Theme,
    lang: string,
): React.ReactNode {
    const actions = session.actions || [];
    const progress = session.summary || session.lastProgress;
    return (
        <div
            data-testid="recoverable-session-card"
            style={{
                marginTop: "8px",
                padding: "10px 12px",
                borderRadius: "8px",
                border: `1px solid ${t.inputBarBorder}`,
                background: t.fieldBg,
            }}
        >
            <div style={{ color: t.headingColor, fontWeight: 700, marginBottom: "6px" }}>
                {localizeText(lang, "Recoverable session", "\u53ef\u6062\u590d\u4f1a\u8bdd", "\u53ef\u6062\u5fa9\u6703\u8a71")}
            </div>
            {session.status && (
                <div data-testid="recoverable-session-status" style={{ color: t.fieldLabel, fontSize: "11px", marginBottom: "6px" }}>
                    {localizeText(lang, "Status", "\u72b6\u6001", "\u72c0\u614b")}{labelSeparator(lang)}{formatSessionStatus(session.status, lang)}
                </div>
            )}
            {session.title && (
                <div data-testid="recoverable-session-title" style={{ color: t.text, fontWeight: 600, marginBottom: "4px" }}>
                    {session.title}
                </div>
            )}
            {progress && (
                <div data-testid="recoverable-session-summary" style={{ color: t.text, whiteSpace: "pre-wrap", overflowWrap: "break-word" }}>
                    {renderContentWithCodeBlocks(formatUnfinishedSlotSummary(progress, lang), t)}
                </div>
            )}
            {session.projectPath && (
                <div data-testid="recoverable-session-project" style={{ color: t.pathColor, marginTop: "6px", wordBreak: "break-all" }}>
                    <a
                        href="#"
                        onClick={(event) => openFileInFolder(event, session.projectPath!)}
                        style={{ color: t.pathColor, textDecoration: "underline", textDecorationStyle: "dotted", textUnderlineOffset: "2px", cursor: "pointer", wordBreak: "break-all" }}
                        title={session.projectPath}
                    >
                        {session.projectPath}
                    </a>
                </div>
            )}
            {actions.length > 0 && renderActions(actions, executeAction, t, lang)}
        </div>
    );
}

function openFileInFolder(event: React.MouseEvent, filePath: string) {
    event.preventDefault();
    void OpenFileOrShowInFolder(filePath).catch(() => ShowItemInFolder(filePath));
}

/*function parseGuideReceiptContent(content: string): { title: string; detail: string; quote: string } {
    const lines = content.split(/\r?\n/).map(line => line.trim()).filter(Boolean);
    const titleIndex = lines.findIndex(line => !line.startsWith('>'));
    const title = titleIndex >= 0 ? lines[titleIndex].replace(/[:：]\s*$/, '') : '';
    const detail = titleIndex >= 0
        ? (lines.slice(titleIndex + 1).find(line => !line.startsWith('>')) || '')
        : '';
    const quote = lines
        .filter(line => line.startsWith('>'))
        .map(line => line.replace(/^>\s?/, ''))
        .join('\n');
    return { title, detail, quote };
}

const GUIDE_RECEIPT_QUOTE_PREVIEW_MAX_CHARS = 96;

function compactGuideReceiptQuotePreview(quote: string): string {
    const preview = quote
        .split(/\r?\n/)
        .map(line => line.trim())
        .filter(Boolean)
        .join(" ");
    if (preview.length <= GUIDE_RECEIPT_QUOTE_PREVIEW_MAX_CHARS) return preview;
    return `${preview.slice(0, GUIDE_RECEIPT_QUOTE_PREVIEW_MAX_CHARS - 1).trimEnd()}…`;
}

function renderGuideReceipt(msg: ChatMessage, t: Theme): React.ReactNode {
    const { title, detail, quote } = parseGuideReceiptContent(msg.content);
    const quotePreview = quote ? compactGuideReceiptQuotePreview(quote) : "";
    return (
        <div
            key={msg.id}
            data-testid="guide-receipt"
            role="status"
            aria-live="polite"
            style={{
                margin: "2px 0 6px",
                padding: "2px 2px",
                color: t.textMuted,
                fontSize: "12px",
                lineHeight: 1.45,
            }}
        >
            {title && <span style={{ color: t.fieldLabel, fontWeight: 600 }}>{title}</span>}
            {detail && <span>{` · ${detail}`}</span>}
            {quotePreview && (
                <div
                    style={{
                        marginTop: "2px",
                        color: t.text,
                        whiteSpace: "nowrap",
                        textOverflow: "ellipsis",
                        overflow: "hidden",
                    }}
                    aria-label={quotePreview}
                >
                    {quotePreview}
                </div>
            )}
        </div>
    );
}*/

/* Render a single ChatMessage */

function compactAttachmentLabel(fileName: string, extension: string): string {
    const raw = (extension || fileName.match(/\.[^./\\]+$/)?.[0] || "").replace(/^\./, "").trim();
    return raw ? raw.slice(0, 4).toUpperCase() : "FILE";
}

function UserAttachmentChip({ attachment, theme, lang }: { attachment: NonNullable<ChatMessage["attachments"]>[number]; theme: Theme; lang: string }) {
    // Composer object URLs are revoked when the message is sent, so they must
    // not cross into the transcript. Always resolve a fresh data URL from the
    // saved local attachment for reliable rendering and history restoration.
    const [thumbnail, setThumbnail] = React.useState("");
    const [previewFailed, setPreviewFailed] = React.useState(false);

    React.useEffect(() => {
        if (!attachment.isImage) {
            setThumbnail("");
            setPreviewFailed(false);
            return;
        }
        let active = true;
        setPreviewFailed(false);
        void AIAssistantAttachmentPreviewDataURL(attachment.filePath)
            .then(dataUrl => {
                if (!active) return;
                const resolved = String(dataUrl || "");
                setThumbnail(resolved);
                setPreviewFailed(!resolved);
            })
            .catch(() => {
                if (!active) return;
                setThumbnail("");
                setPreviewFailed(true);
            });
        return () => { active = false; };
    }, [attachment.filePath, attachment.isImage]);

    const chipStyle: React.CSSProperties = {
        display: "inline-flex",
        width: 30,
        height: 30,
        alignItems: "center",
        justifyContent: "center",
        overflow: "hidden",
        flexShrink: 0,
        borderRadius: 4,
        background: theme.codeBlockBg,
        border: `1px solid ${theme.codeBlockBorder}`,
        color: theme.pathColor,
        fontSize: 8,
        fontWeight: 800,
        lineHeight: 1,
    };

    if (thumbnail) {
        return (
            <AttachmentImageThumbnail
                src={thumbnail}
                filePath={attachment.filePath}
                fileName={attachment.fileName}
                lang={lang}
                theme={theme}
                frameStyle={chipStyle}
                title={attachment.filePath}
            />
        );
    }
    if (attachment.isImage && !previewFailed) {
        return <span aria-label={attachment.fileName} style={chipStyle} />;
    }
    return (
        <span title={attachment.filePath} aria-label={attachment.fileName} style={chipStyle}>
            {compactAttachmentLabel(attachment.fileName, attachment.extension)}
        </span>
    );
}

function AssistantReasoningPanel({
    defaultOpen,
    label,
    theme: t,
    contentKey,
    children,
}: {
    defaultOpen: boolean;
    label: string;
    theme: Theme;
    contentKey: string;
    children: React.ReactNode;
}) {
    const [isOpen, setIsOpen] = React.useState(defaultOpen);
    const { bodyRef, contentRef, handleScroll, handleUserScrollIntent } = useNestedPinnedScroll(isOpen, contentKey);
    const detailsRef = React.useRef<HTMLDetailsElement | null>(null);
    React.useLayoutEffect(() => {
        setIsOpen(defaultOpen);
    }, [defaultOpen]);
    return (
        <details
            ref={detailsRef}
            open={isOpen}
            onToggle={(event) => setIsOpen(event.currentTarget.open)}
            style={{ margin: "2px 0 4px 0", fontSize: "12px", color: t.textMuted }}
        >
            <summary style={{ cursor: "pointer", opacity: 0.8 }}>{label}</summary>
            <div
                ref={bodyRef}
                data-testid="assistant-reasoning-body"
                data-nested-scroll=""
                onWheel={(event) => {
                    event.stopPropagation();
                    handleUserScrollIntent(event);
                }}
                onTouchMove={(event) => {
                    event.stopPropagation();
                    handleUserScrollIntent(event);
                }}
                onPointerDown={handleUserScrollIntent}
                onScroll={handleScroll}
                style={{ padding: "4px 8px", color: t.text, opacity: 0.75, maxHeight: "400px", overflow: "auto" }}
            >
                <div ref={contentRef}>{children}</div>
            </div>
        </details>
    );
}

/** One collapsed reasoning node at its actual position in a coding turn. */
export function renderCodingAgentThinkingTimelineItem(
    item: CodingAgentTimelineItem,
    t: Theme,
    lang: string,
): React.ReactNode {
    const displayReasoning = stripCodingAgentAuditSections(
        stripCodingWorkbenchStatusReasoning(truncateRolePrefixForDisplay(item.content || "")),
    );
    if (!displayReasoning.trim()) return null;
    return (
        <AssistantReasoningPanel
            key={item.id}
            defaultOpen={false}
            label={lang === "en" ? "Thought" : "思考"}
            theme={t}
            contentKey={displayReasoning}
        >
            {renderContentWithCodeBlocks(displayReasoning, t)}
        </AssistantReasoningPanel>
    );
}

/** Visible assistant chrome: prose, thinking, cards, or attachments. */
export function assistantMessageHasVisibleBody(msg: ChatMessage): boolean {
    const savedPaths = msg.localFilePaths && msg.localFilePaths.length > 0
        ? msg.localFilePaths
        : (msg.localFilePath ? [msg.localFilePath] : []);
    const visibleFields = (msg.fields || []).filter((field) => {
        const label = String(field?.label || "").toLowerCase();
        return label !== "recording_title" && label !== "recording_purpose";
    });
    // In the coding workbench reasoning is rendered as ordered timeline nodes.
    // Do not leave an empty assistant bubble at the original placeholder.
    const reasoningVisibleHere = !msg.codingTimeline?.length
        && stripCodingAgentAuditSections(stripCodingWorkbenchStatusReasoning((msg.reasoning || "").trim()));
    return !!(
        stripCodingAgentAuditSections((msg.content || "").trim())
        || reasoningVisibleHere
        || visibleFields.length
        || msg.thumbnailBase64
        || msg.imageKey
        || savedPaths.length
        || msg.actions?.length
        || msg.confirmation
        || msg.unfinishedSlot
        || msg.recoverableSession
        || msg.recordingSession
    );
}

export function renderMessage(
    msg: ChatMessage,
    executeAction: (cmd: string) => void,
    t: Theme,
    isLastAssistant: boolean,
    savedFileLabel: string,
    lang = "en",
    isStreaming = false,
    incrementalContentRenderer?: (formattedContent: string) => React.ReactNode[],
    onRecordingComplete?: (result: RecordingCompleteResult, messageId: string) => void,
    collapseReasoningByDefault = false,
): React.ReactNode {
    switch (msg.role) {
        case "user":
            const isGuideInjection = msg.kind === "guideInjection";
            return (
                <div key={msg.id} role="group" data-testid={`assistant-chat-user-${msg.id}`} aria-label={isGuideInjection ? (lang === "en" ? "Your injected guidance" : "我已注入的引导") : (lang === "en" ? "Your message" : "我的消息")} style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", margin: "10px 0" }}>
                    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, margin: `0 4px ${CHAT_SPEAKER_LABEL_GAP}px`, color: t.textMuted, fontSize: 11, lineHeight: 1.2 }}>
                        <span>{lang === "en" ? "You" : "我"}</span>
                        {isGuideInjection && (
                            <span
                                data-testid="guide-injection-badge"
                                style={{
                                    padding: "1px 5px",
                                    borderRadius: 999,
                                    background: `color-mix(in srgb, ${t.sendBtnBg} 14%, transparent)`,
                                    color: t.text,
                                    fontSize: 10,
                                    fontWeight: 600,
                                    lineHeight: 1.4,
                                }}
                            >
                                {lang === "en" ? "Injected" : "已注入"}
                            </span>
                        )}
                    </span>
                    <ChatBubbleFrame
                        side="right"
                        background={userChatBubbleBackground(t.sendBtnBg, t.fieldBg)}
                        borderColor={t.sendBtnBorder}
                        data-testid={`assistant-chat-user-bubble-${msg.id}`}
                        tailTestId={`assistant-chat-tail-user-${msg.id}`}
                        style={{
                            maxWidth: "76%",
                            color: t.text,
                            fontWeight: 400,
                            lineHeight: 1.55,
                            overflowWrap: "anywhere",
                            whiteSpace: "pre-wrap",
                            // Tail-side corner stays tight (14/14/4/14 — small corner at bottom-right).
                            borderRadius: "14px 14px 4px 14px",
                        }}
                    >
                        {msg.content && <div>{msg.content}</div>}
                        {!!msg.attachments?.length && (
                            <div aria-label={lang === "en" ? "Attached files" : "已附加文件"} style={{ display: "flex", flexWrap: "wrap", gap: 4, marginTop: msg.content ? 7 : 0 }}>
                                {msg.attachments.map((attachment, index) => <UserAttachmentChip key={`${attachment.filePath}-${index}`} attachment={attachment} theme={t} lang={lang} />)}
                            </div>
                        )}
                    </ChatBubbleFrame>
                </div>
            );
        case "assistant": {
            const savedPaths = msg.localFilePaths && msg.localFilePaths.length > 0
                ? msg.localFilePaths
                : (msg.localFilePath ? [msg.localFilePath] : []);
            const screenshotBase64 = msg.thumbnailBase64 || msg.imageKey;
            // Coding workbench: empty placeholder is the trail's job (· Working),
            // not a chat bubble that says 处理中 / 思考中.
            if (collapseReasoningByDefault && !assistantMessageHasVisibleBody(msg)) {
                return null;
            }
            return (
                <div key={msg.id} role="group" data-testid={`assistant-chat-ai-${msg.id}`} aria-label={localizeText(lang, "AI assistant message", "AI 助手消息")} style={{
                    display: "flex",
                    flexDirection: "column",
                    alignItems: "flex-start",
                    justifyContent: "flex-start",
                    margin: "10px 0",
                }}>
                    <span style={{ margin: `0 4px ${CHAT_SPEAKER_LABEL_GAP}px`, color: t.textMuted, fontSize: 11, lineHeight: 1.2 }}>{localAssistantTabTitle(lang)}</span>
                    {(() => {
                        const copyPayload = buildAssistantReplyCopyText(msg.content, msg.unfinishedSlot, lang);
                        const showCopy = copyPayload.trim().length > 0;
                        return (
                    <ChatBubbleFrame
                        side="left"
                        background={t.fieldBg}
                        borderColor={t.fieldBorder}
                        data-testid={`assistant-chat-ai-bubble-${msg.id}`}
                        tailTestId={`assistant-chat-tail-ai-${msg.id}`}
                        topRight={showCopy ? (
                            <AssistantReplyCopyButton
                                text={copyPayload}
                                theme={t}
                                lang={lang}
                                messageId={msg.id}
                            />
                        ) : undefined}
                        style={{
                            width: "fit-content",
                            maxWidth: "84%",
                            minWidth: 0,
                            // Extra right padding so long first lines don't sit under the copy control.
                            padding: showCopy ? "9px 30px 9px 12px" : "9px 12px",
                            color: t.text,
                            lineHeight: 1.55,
                            overflowWrap: "anywhere",
                            // Tail-side corner stays tight (14/14/14/4 — small corner at bottom-left).
                            borderRadius: "14px 14px 14px 4px",
                        }}
                    >
                        {/* Ordinary chat only: coding workbench uses the · Working trail. */}
                        {isLastAssistant && !collapseReasoningByDefault && !msg.content && !msg.fields && !screenshotBase64 && savedPaths.length === 0 && !msg.reasoning && (
                            <span style={{ color: t.textMuted, fontSize: "12px", fontStyle: "italic", opacity: 0.8, animation: "blink 1.2s step-end infinite" }}>
                                {lang === "en" ? "Working..." : "\u5904\u7406\u4e2d\u2026"}
                            </span>
                        )}
                        {screenshotBase64 && renderScreenshotPreview(screenshotBase64, msg.localFilePath, t, lang)}
                        {/* Keep ordinary-chat thinking open for the whole active turn (including
                            tool execution), then fold it once the final answer arrives. The
                            coding workbench stays folded. */}
                        {!msg.codingTimeline?.length && msg.reasoning && (() => {
                            const reasoningLabel = lang === "en" ? "Thinking process..." : "思考过程...";
                            const shouldOpen = isLastAssistant && isStreaming && !collapseReasoningByDefault;
                            // Role-prefix only here; pictograph strip runs inside renderContentWithCodeBlocks.
                            const displayReasoning = stripCodingAgentAuditSections(stripCodingWorkbenchStatusReasoning(truncateRolePrefixForDisplay(msg.reasoning || "")));
                            if (!displayReasoning.trim()) return null;
                            return (
                                <AssistantReasoningPanel
                                    key="reasoning"
                                    defaultOpen={shouldOpen}
                                    label={reasoningLabel}
                                    theme={t}
                                    contentKey={displayReasoning}
                                >
                                    {renderContentWithCodeBlocks(displayReasoning, t)}
                                </AssistantReasoningPanel>
                            );
                        })()}
                        {(() => {
                            // Strip line-leading pictographs once up front so /btw heading match
                            // works on legacy history that still prefixes decorative marks.
                            // renderContentWithCodeBlocks re-strips after compact-heading normalize
                            // (idempotent; that second pass catches "### <pictograph> …").
                            const rawFormattedContent = stripCodingAgentAuditSections(prepareChatBodyForDisplay(
                                formatUnfinishedSlotNotice(msg.content, msg.unfinishedSlot, lang),
                            ));
                            // /btw side query results are collapsible to reduce space.
                            // Detection: requestId starts with "btw-" (set by sendBtwMessage)
                            // OR content starts with the backend prefix (fallback for history reload).
                            // Prefer requestId; content may still carry a legacy "/btw result" heading from history.
                            const btwHeadingMatch = rawFormattedContent?.match(
                                /^\*\*\/btw (?:查询结果|query result)\*\*\n\n/u,
                            );
                            const matchedBtwPrefix = btwHeadingMatch?.[0];
                            const isBtwResult = msg.requestId?.startsWith("btw-") || !!matchedBtwPrefix;
                            if (isBtwResult && rawFormattedContent) {
                                const rawBtwBody = matchedBtwPrefix ? rawFormattedContent.slice(matchedBtwPrefix.length) : rawFormattedContent;
                                const btwBody = stripRolePrefixForDisplay(rawBtwBody);
                                // Extract first non-empty line as preview in the collapsed summary.
                                const firstLine = btwBody.split('\n').find(l => l.trim()) || '';
                                // Strip markdown formatting for plain-text summary display.
                                const plainFirstLine = firstLine.replace(/\*\*/g, '').replace(/[*_`#]/g, '');
                                const preview = plainFirstLine.length > 60 ? plainFirstLine.slice(0, 60) + '…' : plainFirstLine;
                                return (
                                    <details open style={{ margin: "2px 0 4px 0" }}>
                                        <summary style={{ cursor: "pointer", color: t.textMuted, fontSize: "12px", userSelect: "none", display: "inline-flex", alignItems: "center", gap: 6 }}>
                                            <IconSearch size={12} color="currentColor" /> <strong>/btw</strong>{preview ? ` — ${preview}` : ""}
                                        </summary>
                                        <div style={{ padding: "4px 0 0 0" }}>
                                            {renderContentWithCodeBlocks(btwBody, t)}
                                        </div>
                                    </details>
                                );
                            }
                            const formattedContent = stripRolePrefixForDisplay(rawFormattedContent);
                            // Use incremental renderer when provided (streaming long messages)
                            // to avoid O(content.length) full re-parse every 33ms token flush.
                            if (incrementalContentRenderer && formattedContent) {
                                return incrementalContentRenderer(formattedContent);
                            }
                            return renderContentWithCodeBlocks(formattedContent, t);
                        })()}
                        {msg.confirmation && renderConfirmationCard(msg.confirmation, msg.actions, executeAction, t, lang)}
                        {msg.unfinishedSlot && renderUnfinishedSlotCard(msg.unfinishedSlot, executeAction, t, lang)}
                        {msg.recoverableSession && renderRecoverableSessionCard(msg.recoverableSession, executeAction, t, lang)}
                        {msg.recordingSession && (
                            <RecordingSessionCard
                                title={msg.recordingSession.title}
                                purpose={msg.recordingSession.purpose}
                                theme={t}
                                lang={lang}
                                active={!!msg.recordingSession.active && isLastAssistant && !isStreaming}
                                onComplete={(result) => {
                                    if (onRecordingComplete) {
                                        onRecordingComplete(result, msg.id);
                                    }
                                }}
                            />
                        )}
                        {savedPaths.length > 0 && <div style={{ margin: "4px 0" }}>{savedPaths.map((fp, i) => (
                            <div key={i} style={{ padding: "2px 0" }}><a href="#" onClick={(event) => openFileInFolder(event, fp)} style={{ color: t.pathColor, textDecoration: "underline", textDecorationStyle: "dotted", textUnderlineOffset: "2px", cursor: "pointer", wordBreak: "break-all" }} title={fp}>{savedFileLabel}: {fp}</a></div>
                        ))}</div>}
                        {(() => {
                            const visibleFields = (msg.fields || []).filter((f) => {
                                const label = String(f?.label || "").toLowerCase();
                                return label !== "recording_title" && label !== "recording_purpose";
                            });
                            return visibleFields.length > 0 ? renderFields(visibleFields, t) : null;
                        })()}
                        {!msg.confirmation && !msg.recordingSession && msg.actions && msg.actions.length > 0 && renderActions(msg.actions, executeAction, t, lang)}
                    </ChatBubbleFrame>
                        );
                    })()}
                </div>
            );
        }
        case "progress":
            {
                const codingAgentProgress = renderCodingAgentProgressStatus(msg, t, lang);
                if (codingAgentProgress) return codingAgentProgress;
            }
            return (
                <div key={msg.id} role="status" aria-live="polite" data-testid={`assistant-chat-progress-${msg.id}`} style={{ display: "flex", justifyContent: "flex-start", margin: "4px 0" }}>
                    <span style={{
                        maxWidth: "84%",
                        minWidth: 0,
                        display: "inline-flex",
                        alignItems: "baseline",
                        gap: 7,
                        color: t.textMuted,
                        fontSize: "11px",
                        lineHeight: 1.45,
                        overflow: "hidden",
                    }}>
                        <span aria-hidden="true" style={{ width: 5, height: 5, flex: "0 0 auto", borderRadius: "50%", background: t.headingColor, opacity: 0.72 }} />
                        <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={msg.content}>
                            {prepareChatBodyForDisplay(msg.content)}
                        </span>
                    </span>
                </div>
            );
        case "system":
            if (msg.kind === "taskContext") {
                return (
                    <div key={msg.id} role="status" data-testid={`assistant-task-context-${msg.id}`} style={{ display: "flex", justifyContent: "flex-start", margin: "10px 0" }}>
                        <div style={{ maxWidth: "84%", boxSizing: "border-box", padding: "9px 12px", borderRadius: "8px", background: `color-mix(in srgb, ${t.sendBtnBg} 8%, ${t.fieldBg})`, border: `1px solid color-mix(in srgb, ${t.sendBtnBorder} 44%, ${t.fieldBorder})`, color: t.text, fontSize: "12px", lineHeight: "1.6", overflowWrap: "anywhere" }}>
                            <div style={{ marginBottom: 3, color: t.textMuted, fontSize: 11, fontWeight: 700 }}>{lang === "en" ? "CURRENT TASK" : lang === "zh-Hant" ? "目前任務資訊" : "当前任务信息"}</div>
                            {renderContentWithCodeBlocks(msg.content, t)}
                        </div>
                    </div>
                );
            }
            return (
                <div key={msg.id} role="status" data-testid={`assistant-chat-system-${msg.id}`} style={{ display: "flex", justifyContent: "flex-start", margin: "10px 0" }}>
                    <div style={{ maxWidth: "84%", boxSizing: "border-box", padding: "8px 12px", borderRadius: "8px", background: t.fieldBg, border: `1px solid ${t.fieldBorder}`, color: t.text, fontSize: "12px", lineHeight: "1.6", overflowWrap: "anywhere" }}>
                        {msg.kind === 'trace' && msg.fields && msg.fields.length > 0 && renderFields(msg.fields, t)}
                        {renderContentWithCodeBlocks(msg.content, t)}
                    </div>
                </div>
            );
        case "error":
            return (
                <div key={msg.id} role="alert" data-testid={`assistant-chat-error-${msg.id}`} style={{ display: "flex", justifyContent: "flex-start", margin: "10px 0" }}>
                    <div style={{
                        maxWidth: "84%",
                        boxSizing: "border-box",
                        color: t.errorText,
                        background: t.errorBg,
                        border: `1px solid ${t.errorBorder}`,
                        padding: "8px 12px",
                        borderRadius: "8px",
                        fontSize: "12px",
                        lineHeight: 1.55,
                        overflowWrap: "anywhere",
                    }}>
                        {prepareChatBodyForDisplay(msg.content)}
                    </div>
                </div>
            );
        default:
            return null;
    }
}
