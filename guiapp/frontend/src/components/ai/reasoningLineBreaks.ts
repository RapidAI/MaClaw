/**
 * Reasoning streams insert hard newlines that are not paragraph breaks —
 * column-wraps inside parentheticals (`(lines 251-\n504)`) or immediately
 * inside a bracket (`（\n1996）`). The chat renderer treats every `\n` as a
 * block line, so those wraps become ragged leftover lines.
 *
 * Only join wraps inside parentheses and hyphenated numeric ranges. Keep
 * markdown lists, headings, fences, and blank-line paragraphs intact.
 */

import { displayMathAfterLine, markdownFenceAfterLine } from "./aiAssistantMarkdownNormalize";

const BLOCK_START = /^(?:#{1,6}\s+|>\s?|(?:[-*+]|\u2022|\u00b7)\s+|\d+[.)]\s+|[-*_]{3,}\s*$|\|)/;
/** `504) to confirm` after `251-` — a range closer, not a new `2)` list. */
const PAREN_CLOSER_ITEM = /^\d+\)/;
const JOIN_LEFT_NO_SPACE = /[-–—−‐‑/（(\[/{「『【]$/u;
const JOIN_RIGHT_NO_SPACE = /^[）)\]/}」』】.,;:!?。，、；：！？…]/u;
const HAN_END = /\p{Script=Han}$/u;
const HAN_START = /^\p{Script=Han}/u;
/** Two+ digits so `1-\n2) Compile` is not treated as a wrapped range. */
const RANGE_HYPHEN_END = /\d{2,}[-–—−‐‑]$/u;
const DIGIT_START = /^\d/;

function parenDelta(line: string): number {
    let delta = 0;
    for (const ch of line) {
        if (ch === "(" || ch === "（") delta += 1;
        else if (ch === ")" || ch === "）") delta -= 1;
    }
    return delta;
}

function shouldJoinWithoutSpace(left: string, right: string): boolean {
    if (JOIN_LEFT_NO_SPACE.test(left) || JOIN_RIGHT_NO_SPACE.test(right)) return true;
    return HAN_END.test(left) && HAN_START.test(right);
}

function joinWrappedLine(prev: string, next: string): string {
    const left = prev.trimEnd();
    const right = next.trimStart();
    if (shouldJoinWithoutSpace(left, right)) return `${left}${right}`;
    return `${left} ${right}`;
}

function canJoinLine(prev: string, line: string, parenDepth: number): boolean {
    const start = line.trimStart();
    const rangeCont = RANGE_HYPHEN_END.test(prev.trimEnd()) && DIGIT_START.test(start);
    if (rangeCont) return !BLOCK_START.test(start) || PAREN_CLOSER_ITEM.test(start);
    return parenDepth > 0 && !BLOCK_START.test(start);
}

/** Joins need an open parenthesis or a digit (hyphenated ranges such as `251-`). */
function mayJoinWrappedLine(text: string): boolean {
    for (let i = 0; i < text.length; i++) {
        const c = text.charCodeAt(i);
        if (c === 0x28 || c === 0xff08) return true;
        if (c >= 0x30 && c <= 0x39) return true;
    }
    return false;
}

export function repairReasoningLineBreaks(text: string): string {
    if (!text) return text;
    const normalized = text.replace(/\r\n?/g, "\n");
    if (!normalized.includes("\n") || !mayJoinWrappedLine(normalized)) return normalized;

    const out: string[] = [];
    let fenceMarker = "";
    let math: "$$" | "\\[" | "" = "";
    // Set when the previous emitted line belongs to a fence. A dropped blank
    // inside parentheses must not clear it, or the next line would join onto
    // the closer.
    let prevIsFence = false;
    let parenDepth = 0;
    for (const line of normalized.split("\n")) {
        if (math) {
            math = displayMathAfterLine(line, math);
        } else {
            const nextFence = markdownFenceAfterLine(line, fenceMarker);
            if (fenceMarker || nextFence) {
                out.push(line);
                fenceMarker = nextFence;
                prevIsFence = true;
                continue;
            }
            math = displayMathAfterLine(line, "");
        }
        if (!line.trim()) {
            if (parenDepth <= 0) {
                out.push(line);
                prevIsFence = false;
            }
            continue;
        }
        const prev = out[out.length - 1];
        if (prev?.trim() && !prevIsFence && canJoinLine(prev, line, parenDepth)) {
            out[out.length - 1] = joinWrappedLine(prev, line);
        } else {
            out.push(line);
            prevIsFence = false;
        }
        parenDepth = Math.max(0, parenDepth + parenDelta(line));
    }
    return out.join("\n");
}
