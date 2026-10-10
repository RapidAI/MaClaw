/**
 * Thinking-model replies often put the deliverable in reasoning_content and
 * leave content empty or as a short follow-up. After the stream ends, lift
 * that trail into the visible body so a collapsed 思考过程 panel is not the
 * only copy. Internal CoT next to a real answer stays in the thinking panel.
 */

import { stripCodingAgentAuditSections, stripCodingWorkbenchStatusReasoning, stripLeadingCodingWorkbenchStatus } from "./codingAgentUserFinish";
import { repairReasoningLineBreaks } from "./reasoningLineBreaks";
import { truncateRolePrefixForDisplay } from "./rolePrefixDisplay";
import { sanitizeVisibleChatText } from "./visibleChatText";

export const REASONING_BODY_MIN_CHARS = 600;
export const REASONING_BODY_MIN_RATIO = 3;
/** A body this long is already the user-facing answer, not a stub follow-up. */
export const REASONING_FOLLOWUP_MAX_CHARS = 80;

const TRAIL_TAIL_MIN_BODY_CHARS = 24;
/** Prefix still looks like English CoT when CJK share is at or below this. */
const CJK_PREFIX_MAX = 0.35;
/** Suffix looks like a Chinese deliverable when CJK share is at least this. */
const CJK_SUFFIX_MIN = 0.45;

const INTERNAL_MONOLOGUE_RE =
    /(?:^|\n)\s*(?:The user is asking\b|Let me think\b|The system prompt\b|No tools needed\b|Keep it reasonably concise\b|\u7528\u6237(?:\u5728\u95ee|\u95ee\u7684\u662f)|\u8ba9\u6211\u60f3\u60f3|\u6211\u6765\u601d\u8003|\u4e0d\u9700\u8981\u5de5\u5177)/i;

const MONOLOGUE_COMMIT_RE =
    /(?:I should answer[^\n]*|I(?:'ll| will) answer[^\n]*|Let me (?:now )?answer[^\n]*|\u76f4\u63a5(?:\u7528[^\n]{0,12})?\u56de\u7b54[^\n]*|\u4e0b\u9762(?:\u7ed9\u51fa|\u56de\u7b54)[^\n]*)\n+/gi;

/** Same trail cleaning as the thinking panel, so persist and render agree. */
export function cleanReasoningTrailForBody(reasoning: string): string {
    return repairReasoningLineBreaks(stripCodingAgentAuditSections(stripCodingWorkbenchStatusReasoning(
        truncateRolePrefixForDisplay(sanitizeVisibleChatText(reasoning || "")),
    )));
}

export function looksLikeInternalMonologue(text: string): boolean {
    const head = (text || "").slice(0, 1600);
    return INTERNAL_MONOLOGUE_RE.test(head);
}

export function shouldPromoteReasoningToBody(content: string, reasoning: string): boolean {
    const body = (content || "").trim();
    const think = (reasoning || "").trim();
    if (!think) return false;
    if (think.length < REASONING_BODY_MIN_CHARS) return false;
    if (!body) return true;
    // Verbose CoT beside a real answer stays in 思考过程. Do not glue it in
    // front of the official reply just because the trail is longer.
    if (looksLikeInternalMonologue(think)) return false;
    if (body.length >= REASONING_FOLLOWUP_MAX_CHARS) return false;
    return think.length >= body.length * REASONING_BODY_MIN_RATIO;
}

export function bodyAlreadyHasReasoning(content: string, reasoning: string): boolean {
    const body = content || "";
    const think = (reasoning || "").trim();
    if (think.length < REASONING_BODY_MIN_CHARS) return false;
    if (body.length < think.length) return false;
    // Lifted bodies start with the trail. A mid-body quote of CoT must not
    // hide the thinking panel.
    return body.startsWith(think) || body.trimStart().startsWith(think);
}

export function mergeReasoningIntoBody(content: string, reasoning: string): string {
    const body = (content || "").trim();
    const think = (reasoning || "").trim();
    if (!think) return body;
    if (!body) return think;
    if (bodyAlreadyHasReasoning(body, think)) return body;
    // Only treat the follow-up as already present when it is the trail's tail.
    // A short phrase appearing earlier in CoT must not drop the polished body.
    if (think.endsWith(body) || trailTailContainsBody(think, body)) return think;
    return `${think}\n\n${body}`;
}

export function liftReasoningIntoBody(content: string, reasoning: string): string {
    const body = content || "";
    const think = (reasoning || "").trim();
    if (!think) return body;
    if (!shouldPromoteReasoningToBody(body, think) && !bodyAlreadyHasReasoning(body, think)) {
        return body;
    }
    return mergeReasoningIntoBody(body, think);
}

/**
 * Drop host status bullets and peel a CoT prefix off a mixed body. Does not
 * lift a hidden deliverable into content — coding workbench uses this path.
 */
export function separateReasoningFromBody(
    content: string,
    reasoning: string,
): { content: string; reasoning: string } {
    const think = stripCodingWorkbenchStatusReasoning(reasoning || "").trim();
    const body = stripLeadingCodingWorkbenchStatus(content || "");
    if (!think) {
        return splitMonologueFromDeliverable(body) || { content: body, reasoning: "" };
    }
    const peeled = peelTrailPrefix(body, think);
    if (peeled !== null) {
        return { content: peeled, reasoning: think };
    }
    if (!body.trim()) {
        return splitMonologueFromDeliverable(think) || { content: "", reasoning: think };
    }
    const split = splitMonologueFromDeliverable(body);
    if (split) {
        return {
            content: split.content,
            reasoning: think.length >= split.reasoning.length ? think : split.reasoning,
        };
    }
    return { content: body, reasoning: think };
}

/** A tool-call turn's empty content means the draft was rejected, not that the answer lives in reasoning. */
export function visibleAssistantReplyForMessage(
    content: string,
    reasoning: string,
    opts?: { live?: boolean; coding?: boolean; toolCalls?: boolean },
): { content: string; reasoning: string } {
    if (opts?.coding) return separateReasoningFromBody(content, reasoning);
    if (opts?.toolCalls) return { content: content || "", reasoning: (reasoning || "").trim() };
    return resolveVisibleAssistantReply(content, reasoning, { live: opts?.live });
}

export function resolveVisibleAssistantReply(
    content: string,
    reasoning: string,
    opts?: { live?: boolean },
): { content: string; reasoning: string } {
    const body = content || "";
    const think = (reasoning || "").trim();
    if (opts?.live) {
        const visibleBody = stripLeadingCodingWorkbenchStatus(body);
        // A thinking-model often streams its plan as ordinary content. Keep that
        // plan inside 思考过程 while the round is live. A deliverable that
        // already follows the plan stays in the bubble, so the result is not
        // hidden until the stream ends.
        const split = splitLiveMonologue(visibleBody);
        if (split) {
            return {
                content: split.content,
                reasoning: mergeParkedThought(think, split.reasoning),
            };
        }
        if (!visibleBody.includes("<!--maclaw-tool:") && looksLikeInternalMonologue(visibleBody) && visibleBody.trim().length >= REASONING_FOLLOWUP_MAX_CHARS) {
            return { content: "", reasoning: mergeParkedThought(think, visibleBody.trim()) };
        }
        return {
            content: visibleBody,
            reasoning: think,
        };
    }
    const separated = separateReasoningFromBody(body, think);
    if (shouldPromoteReasoningToBody(separated.content, separated.reasoning)
        || bodyAlreadyHasReasoning(separated.content, separated.reasoning)) {
        return { content: liftReasoningIntoBody(separated.content, separated.reasoning), reasoning: "" };
    }
    return separated;
}

/**
 * When the finished reply replaces text that was streaming in the bubble,
 * keep that displaced text in 思考过程. It was the thinking the user already
 * saw, and dropping it makes the thought vanish as the result appears.
 */
export function parkReplacedStreamInReasoning(streamed: string, finalBody: string, reasoning: string): string {
    const prior = (reasoning || "").trim();
    const stream = stripLeadingCodingWorkbenchStatus(streamed || "").trim();
    const final = (finalBody || "").trim();
    if (!stream || stream === final) return prior;
    let displaced = "";
    if (final && stream.endsWith(final) && stream.length > final.length + 20) {
        displaced = stream.slice(0, stream.length - final.length).trim();
    } else if (
        final
        && stream.length >= REASONING_FOLLOWUP_MAX_CHARS
        && !final.startsWith(stream)
        && !stream.startsWith(final)
        && looksLikeInternalMonologue(stream)
        && !stream.startsWith("根据公开检索")
        && !stream.startsWith("Public web results for ")
    ) {
        displaced = stream;
    }
    if (displaced.length < 40) return prior;
    if (prior.includes(displaced)) return prior;
    if (!prior) return displaced;
    return `${prior}\n\n${displaced}`;
}

const TOOL_CALL_MARKER_AT = /<!--maclaw-tool:/;

/** A tool-call marker stays with the answer. Monologue peeling must not carry it into 思考过程. */
function preserveToolCallSuffix(body: string): { head: string; tail: string } {
    const at = body.search(TOOL_CALL_MARKER_AT);
    if (at < 0) return { head: body, tail: "" };
    return { head: body.slice(0, at), tail: body.slice(at) };
}

function reattachToolCallSuffix(content: string, tail: string): string {
    const suffix = tail.trim();
    if (!suffix) return content;
    const body = (content || "").trim();
    if (!body) return suffix;
    return `${body}\n\n${suffix}`;
}

function splitLiveMonologue(body: string): { content: string; reasoning: string } | null {
    const { head, tail } = preserveToolCallSuffix(body);
    const split = splitLiveMonologueUnchecked(head);
    if (split) {
        return { content: reattachToolCallSuffix(split.content, tail), reasoning: split.reasoning };
    }
    // The call arrived before the Chinese answer. Keep the marker in the bubble
    // and park only the English plan that came before it.
    if (tail && looksLikeInternalMonologue(head) && head.trim().length >= REASONING_FOLLOWUP_MAX_CHARS) {
        return { content: tail.trim(), reasoning: head.trim() };
    }
    return null;
}

function splitLiveMonologueUnchecked(body: string): { content: string; reasoning: string } | null {
    const strict = splitMonologueFromDeliverable(body);
    if (strict) return strict;
    if (!looksLikeInternalMonologue(body)) return null;
    const source = body.trim();
    const gap = source.search(/\n\n(?=[\u4e00-\u9fff])/);
    if (gap < 40) return null;
    const think = source.slice(0, gap).trim();
    const answer = source.slice(gap).trim();
    if (answer.length < 24 || cjkRatio(answer) < CJK_SUFFIX_MIN) return null;
    if (!looksLikeInternalMonologue(think)) return null;
    return { content: answer, reasoning: think };
}

function mergeParkedThought(existing: string, parked: string): string {
    const prior = (existing || "").trim();
    const next = (parked || "").trim();
    if (!next) return prior;
    if (!prior) return next;
    if (prior.includes(next)) return prior;
    return `${prior}\n\n${next}`;
}

function peelTrailPrefix(body: string, think: string): string | null {
    const prefix = (think || "").trim();
    if (!prefix) return null;
    const trimmed = body.trimStart();
    if (!trimmed.startsWith(prefix) && !body.startsWith(prefix)) return null;
    const rest = (trimmed.startsWith(prefix) ? trimmed.slice(prefix.length) : body.slice(prefix.length)).trim();
    // A short stub after the trail is still the same deliverable. A real
    // answer after CoT belongs in the body with the trail in 思考过程.
    if (rest.length < REASONING_FOLLOWUP_MAX_CHARS) return null;
    // A short shared opening sentence of the answer must not move into 思考过程.
    if (prefix.length < REASONING_BODY_MIN_CHARS && !looksLikeInternalMonologue(prefix)) return null;
    return rest;
}

function splitMonologueFromDeliverable(text: string): { content: string; reasoning: string } | null {
    const { head, tail } = preserveToolCallSuffix(text);
    const split = splitMonologueFromDeliverableUnchecked(head);
    if (split) {
        return { content: reattachToolCallSuffix(split.content, tail), reasoning: split.reasoning };
    }
    if (!tail || !looksLikeInternalMonologue(head) || head.trim().length < REASONING_FOLLOWUP_MAX_CHARS) return null;
    const answer = tail.replace(/<!--maclaw-tool:[A-Za-z0-9_-]+-->/g, "").trim();
    if (answer.length < REASONING_FOLLOWUP_MAX_CHARS && cjkRatio(answer) < CJK_SUFFIX_MIN) return null;
    return { content: tail.trim(), reasoning: head.trim() };
}

function splitMonologueFromDeliverableUnchecked(text: string): { content: string; reasoning: string } | null {
    const source = (text || "").trim();
    if (!looksLikeInternalMonologue(source)) return null;
    let cut = lastCommitCut(source);
    if (cut < 0) cut = firstLanguageShiftCut(source);
    if (cut < 0) return null;
    const think = source.slice(0, cut).trim();
    const body = source.slice(cut).trim();
    if (think.length < REASONING_FOLLOWUP_MAX_CHARS || body.length < REASONING_FOLLOWUP_MAX_CHARS) return null;
    return { content: body, reasoning: think };
}

function lastCommitCut(source: string): number {
    MONOLOGUE_COMMIT_RE.lastIndex = 0;
    let cut = -1;
    for (const match of source.matchAll(MONOLOGUE_COMMIT_RE)) {
        const at = (match.index ?? 0) + match[0].length;
        const remainder = source.slice(at).trim();
        if (remainder.length < REASONING_FOLLOWUP_MAX_CHARS) continue;
        // An early "I'll answer after I think" still leaves CoT in the remainder.
        if (looksLikeInternalMonologue(remainder)) continue;
        // "下面给出" inside an already-Chinese answer must not steal the opening.
        if (cjkRatio(source.slice(0, at)) > CJK_PREFIX_MAX && cjkRatio(remainder) >= CJK_SUFFIX_MIN) continue;
        cut = at;
    }
    return cut;
}

function firstLanguageShiftCut(source: string): number {
    // First CJK block whose tail is the deliverable. Later headings inside that
    // answer (流派归属) must not move the opening paragraph into 思考过程.
    // Keep markdown/list markers with the body so "## 泸州菜" still splits.
    // Build the regex per call so a leftover lastIndex cannot skip matches.
    for (const match of source.matchAll(/\n\n(?:#{1,6}[ \t]+|(?:[-*\u2022]|\d+\.)[ \t]+)?[\u4e00-\u9fff]/g)) {
        const at = (match.index ?? 0) + 2;
        if (at <= REASONING_FOLLOWUP_MAX_CHARS) continue;
        const suffix = source.slice(at).trim();
        if (suffix.length < REASONING_FOLLOWUP_MAX_CHARS) continue;
        if (cjkRatio(source.slice(0, at)) > CJK_PREFIX_MAX) continue;
        if (cjkRatio(suffix) < CJK_SUFFIX_MIN) continue;
        return at;
    }
    return -1;
}

function cjkRatio(text: string): number {
    let cjk = 0;
    let total = 0;
    for (const ch of text) {
        if (ch === " " || ch === "\n" || ch === "\t" || ch === "\r") continue;
        if (ch >= "\u4e00" && ch <= "\u9fff") {
            cjk += 1;
            total += 1;
            continue;
        }
        // Skip punctuation/symbols so "：。，" does not dilute a Chinese answer.
        if ((ch >= "!" && ch <= "/") || (ch >= ":" && ch <= "@") || (ch >= "[" && ch <= "`") || (ch >= "{" && ch <= "~")) continue;
        if (ch >= "\u3000" && ch <= "\u303f") continue;
        if (ch >= "\uff00" && ch <= "\uffef") continue;
        total += 1;
    }
    return total === 0 ? 0 : cjk / total;
}

function trailTailContainsBody(think: string, body: string): boolean {
    if (body.length < TRAIL_TAIL_MIN_BODY_CHARS) return false;
    const tailLen = Math.min(think.length, Math.max(body.length * 2, 400));
    return think.slice(think.length - tailLen).includes(body);
}
