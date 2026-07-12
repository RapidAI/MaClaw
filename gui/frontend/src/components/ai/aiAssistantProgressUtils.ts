import type { ChatMessage } from "./useAIAssistant";
import { legacyMarkVisual } from "../remote/remoteStreamMarks";

/**
 * Decorative pictograph ranges (no emoji literals in source).
 * Leading clusters may include ZWJ sequences; mid-label strip is single code points + VS16.
 */
// Regional indicators sit inside 1F300–1FAFF; listed ranges match corelib/textutil.
// U+2B50 (⭐) sits outside 2600–27BF; include explicitly for rating columns.
const PICTOGRAPH_UNIT = "[\\u{1F300}-\\u{1FAFF}\\u{2600}-\\u{27BF}\\u{2300}-\\u{23FF}\\u{2B50}]\\uFE0F?";
/** Source only — never share a single /g RegExp across calls (lastIndex races). */
const PICTOGRAPH_CLUSTER_SOURCE = `${PICTOGRAPH_UNIT}(?:\\u200D${PICTOGRAPH_UNIT})*`;
const LEADING_EMOJI_CLUSTER = new RegExp(
    `^(?:${PICTOGRAPH_UNIT}(?:\\u200D${PICTOGRAPH_UNIT})*\\s*)+`,
    "u",
);

const STAR_MARK = "\u2B50";

export type InlineMarkVisual =
    | { kind: "status"; status: "ok" | "error" | "pending" | "warn" | "tool" }
    | { kind: "bolt" }
    | { kind: "star" };

/** Map a single pictograph base (no FE0F) to an inline SVG visual, or null if decorative. */
export function inlineMarkVisual(base: string): InlineMarkVisual | null {
    if (base === STAR_MARK) return { kind: "star" };
    const legacy = legacyMarkVisual(base);
    if (!legacy) return null;
    if (legacy.kind === "bolt") return { kind: "bolt" };
    return { kind: "status", status: legacy.status };
}

/**
 * Resolve a matched pictograph cluster to a visual.
 * Multi-codepoint ZWJ sequences are always decorative (stripped).
 */
export function classifyPictographCluster(cluster: string): InlineMarkVisual | null {
    const bases: string[] = [];
    // Walk code points; collect pictograph bases (skip FE0F / ZWJ joiners).
    for (const ch of cluster) {
        const cp = ch.codePointAt(0)!;
        if (cp === 0xfe0f || cp === 0x200d) continue;
        bases.push(ch);
    }
    if (bases.length !== 1) return null;
    return inlineMarkVisual(bases[0]);
}

/** True when text may contain a pictograph cluster worth scanning at render time. */
export function textMayContainPictograph(text: string): boolean {
    return mayContainPictograph(text);
}

/** Fresh /gu matcher — safe under nested or concurrent scans. */
function pictographClusterMatches(text: string): IterableIterator<RegExpExecArray> {
    return text.matchAll(new RegExp(PICTOGRAPH_CLUSTER_SOURCE, "gu"));
}

/**
 * Split plain text into string segments and pictograph clusters (for SVG swap).
 * Decorative clusters are omitted; callers map semantic clusters via classifyPictographCluster.
 */
export function splitTextByPictographClusters(text: string): Array<string | { cluster: string; visual: InlineMarkVisual }> {
    if (!text || !mayContainPictograph(text)) return text ? [text] : [];
    const parts: Array<string | { cluster: string; visual: InlineMarkVisual }> = [];
    let lastIndex = 0;
    for (const match of pictographClusterMatches(text)) {
        if (match.index > lastIndex) parts.push(text.slice(lastIndex, match.index));
        const cluster = match[0];
        const visual = classifyPictographCluster(cluster);
        if (visual) parts.push({ cluster, visual });
        // Decorative: omit from output.
        lastIndex = match.index + cluster.length;
    }
    if (lastIndex < text.length) parts.push(text.slice(lastIndex));
    return parts;
}

export function stripLeadingEmojiCluster(text: string): string {
    return text.replace(LEADING_EMOJI_CLUSTER, "");
}

/** Markdown structural prefixes that may sit before a decorative line-start pictograph. */
const MD_LINE_PREFIX = /^(#{1,6}[ \t]+|[-*+][ \t]+|\d+\.[ \t]+|>[ \t]+)/;
const FENCE_LINE = /^[ \t]*(`{3,}|~{3,})/;

/**
 * Strip decorative pictograph clusters on a single line (leading + mid-sentence),
 * after optional indentation and a markdown structural prefix.
 * Semantic status/star marks are kept for SVG substitution at render time.
 */
export function stripLineLeadingEmoji(line: string): string {
    return stripLineDecorativePictographs(line);
}

/**
 * Outside fenced code: remove chatbot-style decorative pictographs everywhere on
 * the line, keep status/star marks for StatusGlyph / IconStar rendering.
 */
export function stripLineDecorativePictographs(line: string): string {
    if (!line || !mayContainPictograph(line)) return line;

    let wsEnd = 0;
    while (wsEnd < line.length) {
        const ch = line.charCodeAt(wsEnd);
        if (ch === 0x20 || ch === 0x09) wsEnd++;
        else break;
    }
    const afterWs = line.slice(wsEnd);
    const mdMatch = afterWs.match(MD_LINE_PREFIX);
    const mdPrefix = mdMatch?.[0] ?? "";
    const rest = mdPrefix ? afterWs.slice(mdPrefix.length) : afterWs;

    // Walk clusters; drop trailing padding spaces with decorative marks only.
    // Do NOT strip FE0F/ZWJ globally afterward — that would mutilate kept status marks
    // (e.g. U+26A0 + VS16) when a decorative mark also appears on the same line.
    let changed = false;
    let lastIndex = 0;
    const chunks: string[] = [];
    for (const match of pictographClusterMatches(rest)) {
        if (match.index > lastIndex) chunks.push(rest.slice(lastIndex, match.index));
        const cluster = match[0];
        let end = match.index + cluster.length;
        if (classifyPictographCluster(cluster)) {
            chunks.push(cluster);
            lastIndex = end;
            continue;
        }
        changed = true;
        // Drop spaces/tabs that trailed the decorative cluster (AI-flavor padding).
        while (end < rest.length) {
            const ch = rest.charCodeAt(end);
            if (ch === 0x20 || ch === 0x09) end++;
            else break;
        }
        lastIndex = end;
    }
    if (lastIndex < rest.length) chunks.push(rest.slice(lastIndex));

    if (!changed) return line;

    // Collapse residual double spaces introduced by mid-line removals only.
    const tidy = chunks
        .join("")
        .replace(/[ \t]{2,}/g, " ")
        .replace(/[ \t]+$/g, "");

    return line.slice(0, wsEnd) + mdPrefix + tidy;
}

/**
 * Line-array form of the chat-body display policy (avoids join/split when the
 * caller already has lines). Strips decorative pictographs outside fenced code
 * blocks; semantic status/star marks are preserved for SVG rendering.
 */
export function prepareChatBodyLines(lines: string[]): string[] {
    let inFence = false;
    let changed = false;
    const out = lines.map((line) => {
        if (FENCE_LINE.test(line)) {
            inFence = !inFence;
            return line;
        }
        if (inFence) return line;
        const cleaned = stripLineDecorativePictographs(line);
        if (cleaned !== line) changed = true;
        return cleaned;
    });
    // Preserve input array identity when nothing was stripped (streaming-friendly).
    return changed ? out : lines;
}

/** True when text may contain decorative pictograph bases (fast reject for clean text). */
function mayContainPictograph(text: string): boolean {
    // BMP dingbats/misc symbols + star + any non-BMP code unit (surrogate) as a cheap filter.
    for (let i = 0; i < text.length; i++) {
        const c = text.charCodeAt(i);
        if (c >= 0x2300 && c <= 0x27bf) return true;
        if (c === 0x2b50) return true; // ⭐ rating star (outside 2600–27BF)
        if (c >= 0xd800 && c <= 0xdbff) return true; // high surrogate → non-BMP (emoji planes)
    }
    return false;
}

/**
 * Chat-body display policy: strip decorative pictographs (line-leading and
 * mid-sentence "AI flavor") outside fenced code blocks. Semantic status marks
 * (check / warn / cross / …) and stars are kept so the renderer can swap them
 * for SVG glyphs. Display-only — does not mutate storage.
 */
export function prepareChatBodyForDisplay(text: string): string {
    if (!text || !mayContainPictograph(text)) return text;
    const lines = text.split("\n");
    const prepared = prepareChatBodyLines(lines);
    // No decorative pictographs removed → keep original text identity.
    if (prepared === lines) return text;
    return prepared.join("\n");
}

/** Find the latest tool-specific progress message while the assistant is sending. */
export function findLatestToolProgressText(progressMessages: ChatMessage[], sending: boolean): string {
    if (!sending || progressMessages.length === 0) return "";
    for (let i = progressMessages.length - 1; i >= 0; i--) {
        const msg = progressMessages[i];
        // isToolProgressMessage already checks role/content/prefix.
        if (isToolProgressMessage(msg)) return msg.content || "";
    }
    return "";
}

export function isToolProgressMessage(msg: ChatMessage): boolean {
    if (msg.role !== "progress") return false;
    const content = msg.content?.trimStart() || "";
    if (!content) return false;
    const withoutPrefix = stripLeadingEmojiCluster(content).trim();
    return isRunningToolStatus(withoutPrefix);
}

/**
 * Strip residual pictographs inside a status label (skill names, etc.).
 * Removes ALL pictographs including status marks (progress labels are text-only).
 */
export function stripDecorativePictographs(text: string): string {
    // Fresh /gu each call — no shared lastIndex.
    return text
        .replace(new RegExp(PICTOGRAPH_UNIT, "gu"), "")
        // Drop orphaned ZWJ / VS16 left after removing multi-codepoint sequences.
        .replace(/[\u200D\uFE0F]/g, "")
        .replace(/\s{2,}/g, " ")
        .trim();
}

export function formatToolProgressStatus(text: string, lang: string): string {
    let cleaned = stripLeadingEmojiCluster(text.trim()).trim();
    cleaned = cleaned
        .replace(/\s*[（(](?:可继续输入|you can type ahead)[）)]\s*$/i, "")
        .replace(/\s*(?:\.\.\.|…)\s*$/, "")
        .trim();
    const runningAction = lang === "en" ? "Running" : "\u6b63\u5728\u6267\u884c";
    const startingAction = lang === "en" ? "Starting" : "\u6b63\u5728\u542f\u52a8";
    const skillMatch = cleaned.match(/(\u6b63\u5728\u6267\u884c|\u6b63\u5728\u542f\u52a8)\s*Skill[\u300c"]([^\u300d"]+)[\u300d"]?/);
    if (skillMatch?.[2]) {
        return `${isStartingToolAction(skillMatch[1]) ? startingAction : runningAction} ${stripDecorativePictographs(skillMatch[2])}`;
    }
    const toolPathMatch = cleaned.match(/^(\u6b63\u5728\u6267\u884c|\u6b63\u5728\u542f\u52a8|running|executing|starting|launching)\s+(?:Shell|Skill)\s*\/\s*([^/]+?)(?:\s*\/.*)?$/i);
    if (toolPathMatch?.[2]) {
        return `${isStartingToolAction(toolPathMatch[1]) ? startingAction : runningAction} ${stripDecorativePictographs(toolPathMatch[2])}`;
    }
    const englishSkillMatch = cleaned.match(/(running|executing|starting|launching)\s+Skill\s*["“]?([^"”]+)["”]?/i);
    if (englishSkillMatch?.[2]) {
        return `${isStartingToolAction(englishSkillMatch[1]) ? "Starting" : "Running"} ${stripDecorativePictographs(englishSkillMatch[2])}`;
    }
    return stripDecorativePictographs(cleaned) || (lang === "en" ? "Working" : "\u6b63\u5728\u6267\u884c");
}

function isRunningToolStatus(text: string): boolean {
    return /^(?:\u6b63\u5728\u6267\u884c|\u6b63\u5728\u542f\u52a8)\s*(?:Skill[\u300c"]|(?:Shell|Skill)\s*\/)/.test(text)
        || /^(?:running|executing|starting|launching)\s+(?:Skill\b|(?:Shell|Skill)\s*\/)/i.test(text);
}

function isStartingToolAction(action: string): boolean {
    return /starting|launching|\u542f\u52a8/i.test(action);
}
