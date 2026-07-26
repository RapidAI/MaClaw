import React from "react";
import { OpenFileOrShowInFolder, ShowItemInFolder } from "../../../wailsjs/go/main/App";
import { BrowserOpenURL } from "../../../wailsjs/runtime";
import type { ChatAction, ChatConfirmation, ChatMessage, ChatRecoverableSession, ChatUnfinishedSlot } from "./useAIAssistant";
import { renderCodingAgentProgressStatus } from "./CodingAgentProgressStatus";
import { attachBareHeadingMarkers, normalizeInlineListMarkers } from "./aiAssistantMarkdownNormalize";
import { buildMarkdownTableModel, isMarkdownTableRow, isMarkdownTableSeparatorRow, normalizeMarkdownTableLine, parseMarkdownTableCells, repairMixedNarrativeTable } from "./aiAssistantMarkdownTable";
import { localAssistantTabTitle, localizeText } from "./aiAssistantI18n";
import { baseInputBtnStyle, type Theme } from "./aiAssistantPanelTheme";
import { ChatBubbleFrame, CHAT_SPEAKER_LABEL_GAP, userChatBubbleBackground } from "./ChatBubbleFrame";
import { renderScreenshotPreview } from "./aiAssistantMarkdownMedia";
import { getWailsAppModule } from "../../utils/wailsAppModule";
import { stripRolePrefixForDisplay, truncateRolePrefixForDisplay } from "./rolePrefixDisplay";
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
import { IconBolt, IconCheck, IconClipboard, IconSearch, IconStar, StatusGlyph } from "./WorkbenchIcons";
import {
    RecordingSessionCard,
    type RecordingCompleteResult,
} from "./RecordingSessionCard";

export type { Theme } from "./aiAssistantPanelTheme";
export type { RecordingCompleteResult } from "./RecordingSessionCard";

/** Copy plain text to the system clipboard (with execCommand fallback). */
export async function copyTextToClipboard(text: string): Promise<boolean> {
    const value = String(text ?? "");
    // Reject empty / whitespace-only so the control never pastes blank noise.
    if (!value.trim()) return false;
    try {
        if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
            await navigator.clipboard.writeText(value);
            return true;
        }
    } catch {
        // fall through to legacy path
    }
    try {
        if (typeof document === "undefined") return false;
        const ta = document.createElement("textarea");
        ta.value = value;
        ta.setAttribute("readonly", "");
        ta.style.position = "fixed";
        ta.style.left = "-9999px";
        ta.style.top = "0";
        document.body.appendChild(ta);
        ta.focus();
        ta.select();
        ta.setSelectionRange(0, value.length);
        const ok = document.execCommand("copy");
        document.body.removeChild(ta);
        return ok;
    } catch {
        return false;
    }
}

/**
 * Build the plain-text payload for "copy reply": same body the user sees,
 * without decorative role prefixes. Does not include collapsed "thinking" text.
 */
export function buildAssistantReplyCopyText(
    content: string | undefined,
    unfinishedSlot?: ChatUnfinishedSlot,
    lang = "en",
): string {
    const raw = prepareChatBodyForDisplay(
        formatUnfinishedSlotNotice(content || "", unfinishedSlot, lang),
    );
    return stripRolePrefixForDisplay(raw || "").replace(/\s+$/u, "");
}

/**
 * Top-right control on AI reply bubbles: copy the full reply text.
 * Shown only when there is non-empty content (hidden while empty streaming placeholder).
 */
export function AssistantReplyCopyButton({
    text,
    theme: t,
    lang = "en",
    messageId,
}: {
    text: string;
    theme: Theme;
    lang?: string;
    messageId?: string;
}) {
    const [state, setState] = React.useState<"idle" | "busy" | "ok" | "err">("idle");
    const [hovered, setHovered] = React.useState(false);
    const timerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);
    const mountedRef = React.useRef(true);
    const copyLabel = localizeText(lang, "Copy reply", "复制回复", "複製回覆");
    const copiedLabel = localizeText(lang, "Copied", "已复制", "已複製");
    const failedLabel = localizeText(lang, "Copy failed", "复制失败", "複製失敗");

    React.useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            if (timerRef.current) clearTimeout(timerRef.current);
        };
    }, []);

    // Hooks must run unconditionally; hide after hooks when there is nothing to copy.
    const body = String(text ?? "");
    const hasBody = body.trim().length > 0;
    const title = state === "ok" ? copiedLabel : state === "err" ? failedLabel : copyLabel;

    if (!hasBody) return null;

    const emphasize = hovered || state === "ok" || state === "err";

    return (
        <button
            type="button"
            data-testid={messageId ? `assistant-chat-copy-${messageId}` : "assistant-chat-copy"}
            aria-label={title}
            title={title}
            disabled={state === "busy"}
            onMouseEnter={() => setHovered(true)}
            onMouseLeave={() => setHovered(false)}
            onFocus={() => setHovered(true)}
            onBlur={() => setHovered(false)}
            onClick={async (e) => {
                e.preventDefault();
                e.stopPropagation();
                if (state === "busy") return;
                setState("busy");
                const ok = await copyTextToClipboard(body);
                if (!mountedRef.current) return;
                setState(ok ? "ok" : "err");
                if (timerRef.current) clearTimeout(timerRef.current);
                timerRef.current = setTimeout(() => {
                    if (mountedRef.current) setState("idle");
                }, 1600);
            }}
            style={{
                position: "relative",
                display: "inline-flex",
                alignItems: "center",
                justifyContent: "center",
                width: 22,
                height: 22,
                padding: 0,
                borderRadius: 6,
                border: `1px solid ${t.fieldBorder}`,
                background: state === "ok"
                    ? "color-mix(in srgb, var(--theme-success, #16a34a) 14%, transparent)"
                    : "color-mix(in srgb, var(--theme-surface, #fff) 92%, transparent)",
                color: state === "ok"
                    ? "var(--theme-success, #16a34a)"
                    : state === "err"
                        ? "var(--theme-danger, #dc2626)"
                        : t.textMuted,
                cursor: state === "busy" ? "wait" : "pointer",
                opacity: state === "busy" ? 0.65 : emphasize ? 1 : 0.72,
                boxShadow: emphasize
                    ? "0 1px 3px color-mix(in srgb, #000 12%, transparent)"
                    : "0 1px 2px color-mix(in srgb, #000 6%, transparent)",
                transition: "background 120ms ease, color 120ms ease, opacity 120ms ease, box-shadow 120ms ease",
            }}
        >
            {/* Live region announces copy result without moving focus. */}
            <span
                aria-live="polite"
                style={{
                    position: "absolute",
                    width: 1,
                    height: 1,
                    padding: 0,
                    margin: -1,
                    overflow: "hidden",
                    clip: "rect(0, 0, 0, 0)",
                    whiteSpace: "nowrap",
                    border: 0,
                }}
            >
                {state === "ok" || state === "err" ? title : ""}
            </span>
            {state === "ok"
                ? <IconCheck size={13} color="currentColor" />
                : <IconClipboard size={13} color="currentColor" />}
        </button>
    );
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

export function renderInlineMarkdown(text: string, t: Theme): React.ReactNode[] {
    return renderInlineMarkdownRestored(text, t);
}

// Shared across every chat body line (streaming-friendly: no per-call recompile).
// GFM unordered markers (-/*/+ ) plus digital-employee bullets (U+2022 •, U+00B7 ·).
const UNORDERED_LIST_LINE_RE = /^(?:[-*+]|\u2022|\u00b7)\s+(.*)$/;

function renderMarkdownLine(text: string, key: string | number, t: Theme): React.ReactNode {
    const trimmed = text.trimStart();

    // KB_IMAGE marker: render as inline clickable thumbnail
    const kbImageMatch = trimmed.match(/\[KB_IMAGE:([^|]+)\|([^|]+)\|([^\]]*)\]/);
    if (kbImageMatch) {
        const [, assetId, dataUrl, originalPath] = kbImageMatch;
        return (
            <div key={key} style={{ margin: "6px 0", maxWidth: "100%", minWidth: 0, boxSizing: "border-box", overflow: "hidden" }}>
                <img
                    src={dataUrl}
                    alt={assetId}
                    title={originalPath ? `Click to open: ${originalPath}` : assetId}
                    loading="lazy"
                    style={{
                        width: "120px",
                        maxWidth: "100%",
                        maxHeight: "120px",
                        borderRadius: "6px",
                        border: `1px solid ${t.divider}`,
                        boxSizing: "border-box",
                        display: "block",
                        cursor: originalPath ? "pointer" : "default",
                        transition: "transform 0.15s",
                    }}
                    onClick={() => {
                        if (originalPath) {
                            getWailsAppModule().then(mod => {
                                mod.KnowledgeOpenImageFile(originalPath).catch(() => {});
                            });
                        }
                    }}
                    onMouseEnter={(e) => { (e.target as HTMLElement).style.transform = "scale(1.05)"; }}
                    onMouseLeave={(e) => { (e.target as HTMLElement).style.transform = ""; }}
                />
            </div>
        );
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
    const cellStyle: React.CSSProperties = { border: `1px solid ${t.fieldBorder}`, boxSizing: "border-box", overflowWrap: "anywhere", padding: "6px 10px", textAlign: "left", verticalAlign: "top", wordBreak: "break-word", fontSize: "0.9em", lineHeight: 1.5 };
    return (
        <div key={key} data-testid="markdown-table-block" style={{ width: "100%", maxWidth: "100%", minWidth: 0, boxSizing: "border-box", overflowX: "auto", overscrollBehaviorX: "contain", margin: "6px 0", whiteSpace: "normal" }}>
            {prefix && <div data-testid="markdown-table-prefix" style={{ marginBottom: 6, ...blockWrapStyle }}>{renderInlineMarkdown(prefix, t)}</div>}
            <table data-testid="markdown-table" style={{ borderCollapse: "collapse", minWidth: minTableWidth, tableLayout: "fixed", width: "100%", color: t.text, whiteSpace: "normal", wordBreak: "normal" }}>
                <thead><tr>{headerCells.map((cell, ci) => <th key={ci} style={{ ...cellStyle, textAlign: columnAlignments[ci], fontWeight: 600, background: t.fieldBg, color: t.headingColor, fontSize: "0.88em", letterSpacing: "0.02em" }}>{renderInlineMarkdown(cell, t)}</th>)}</tr></thead>
                {bodyRows.length > 0 && <tbody>{bodyRows.map((row, ri) => { const cells = parseMarkdownTableCells(row); return <tr key={ri} style={{ background: ri % 2 === 1 ? t.fieldBg : undefined }}>{headerCells.map((_, ci) => <td key={ci} style={{ ...cellStyle, textAlign: columnAlignments[ci] }}>{renderInlineMarkdown(cells[ci] || "", t)}</td>)}</tr>; })}</tbody>}
            </table>
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
    const structureLines = normalized.includes("#") ? attachBareHeadingMarkers(rawLines) : rawLines;
    const lines = prepareChatBodyLines(structureLines);
    let inCodeBlock = false;
    let codeBlockLines: string[] = [];
    let codeBlockLang = "";
    let tableLines: string[] = [];
    let lineIdx = 0;

    const flushCodeBlock = () => {
        if (inCodeBlock || codeBlockLines.length > 0) {
            elements.push(
                <pre key={`code-${elements.length}`} style={{
                    background: t.codeBlockBg,
                    border: `1px solid ${t.codeBlockBorder}`,
                    borderRadius: "6px",
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
        if (!inCodeBlock && /^\|+$/.test(line.trim())) {
            continue;
        }
        if (/^```/.test(line.trimStart())) {
            flushTable();
            if (inCodeBlock) {
                flushCodeBlock();
                inCodeBlock = false;
            } else {
                inCodeBlock = true;
                codeBlockLang = line.trimStart().slice(3).trim();
            }
        } else if (inCodeBlock) {
            codeBlockLines.push(line);
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
    if (inCodeBlock) flushCodeBlock();
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
                        }}
                    >
                        {msg.content}
                    </ChatBubbleFrame>
                </div>
            );
        case "assistant": {
            const savedPaths = msg.localFilePaths && msg.localFilePaths.length > 0
                ? msg.localFilePaths
                : (msg.localFilePath ? [msg.localFilePath] : []);
            const screenshotBase64 = msg.thumbnailBase64 || msg.imageKey;
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
                        }}
                    >
                        {/* Streaming: show thinking indicator on the last assistant message placeholder */}
                        {isLastAssistant && !msg.content && !msg.fields && !screenshotBase64 && savedPaths.length === 0 && !msg.reasoning && (
                            <span style={{ color: t.textMuted, fontSize: "12px", fontStyle: "italic", opacity: 0.8, animation: "blink 1.2s step-end infinite" }}>
                                {lang === "en" ? "Working..." : "\u5904\u7406\u4e2d\u2026"}
                            </span>
                        )}
                        {screenshotBase64 && renderScreenshotPreview(screenshotBase64, msg.localFilePath, openFileInFolder, t)}
                        {/* Reasoning/thinking content from reasoning models —
                            expanded while this turn is still in progress, then collapsed once the final result is ready.
                            Some Responses providers return a final summary without text-streaming, so use the turn's busy
                            state rather than the narrower stream state. The key forces the native details element to adopt
                            the automatic close at completion while keeping user-controlled toggles within each phase. */}
                        {msg.reasoning && (() => {
                            const shouldOpen = isLastAssistant && isStreaming;
                            const reasoningLabel = lang === "en" ? "Thinking process..." : "思考过程...";
                            // Role-prefix only here; pictograph strip runs inside renderContentWithCodeBlocks.
                            const displayReasoning = truncateRolePrefixForDisplay(msg.reasoning || "");
                            if (!displayReasoning.trim()) return null;
                            return (
                                <details key={shouldOpen ? "reasoning-open" : "reasoning-closed"} open={shouldOpen || undefined} style={{ margin: "2px 0 4px 0", fontSize: "12px", color: t.textMuted }}>
                                    <summary style={{ cursor: "pointer", opacity: 0.8 }}>{reasoningLabel}</summary>
                                    <div style={{ padding: "4px 8px", color: t.text, opacity: 0.75, maxHeight: "400px", overflow: "auto" }}>
                                        {renderContentWithCodeBlocks(displayReasoning, t)}
                                    </div>
                                </details>
                            );
                        })()}
                        {(() => {
                            // Strip line-leading pictographs once up front so /btw heading match
                            // works on legacy history that still prefixes decorative marks.
                            // renderContentWithCodeBlocks re-strips after compact-heading normalize
                            // (idempotent; that second pass catches "### <pictograph> …").
                            const rawFormattedContent = prepareChatBodyForDisplay(
                                formatUnfinishedSlotNotice(msg.content, msg.unfinishedSlot, lang),
                            );
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
                <div key={msg.id} role="status" aria-live="polite" data-testid={`assistant-chat-progress-${msg.id}`} style={{ display: "flex", justifyContent: "flex-start", margin: "8px 0" }}>
                    <span style={{
                        maxWidth: "84%",
                        boxSizing: "border-box",
                        padding: "4px 8px",
                        borderRadius: "999px",
                        background: t.fieldBg,
                        border: `1px solid ${t.fieldBorder}`,
                        color: t.textMuted,
                        fontSize: "11px",
                        lineHeight: 1.4,
                        overflowWrap: "anywhere",
                    }}>
                        {prepareChatBodyForDisplay(msg.content)}
                    </span>
                </div>
            );
        case "system":
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
