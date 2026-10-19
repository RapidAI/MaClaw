/**
 * The open coding thought cleans its whole body on every token flush.
 * Streaming only appends, and each stage of cleanReasoningTrailForBody rewrites
 * just the open tail, so the prefix already cleaned is reused. A rewrite or a
 * non-append resets and matches the full clean exactly.
 */

import {
    appendCodingAuditStrip,
    createCodingAuditCursor,
    isCodingWorkbenchStatusLine,
    type CodingAuditCursor,
} from "./codingAgentUserFinish";
import {
    appendReasoningRepair,
    createReasoningRepairCursor,
    type ReasoningRepairCursor,
} from "./reasoningLineBreaks";
import { findRolePrefixForDisplay, rolePrefixLineOpen, truncateRolePrefixForDisplay } from "./rolePrefixDisplay";
import { sanitizeVisibleChatText } from "./visibleChatText";

type RoleCursor = {
    input: string;
    output: string;
    frozen: boolean;
    fenceOpen: boolean;
    fenceAtLine: boolean;
    trailingTicks: number;
    lineStart: number;
    lineOpen: boolean;
};

type StatusCursor = {
    input: string;
    scanned: number;
    kept: string;
    pendingBlanks: number;
    started: boolean;
};

export type ReasoningTrailCleanCache = {
    id: string;
    raw: string;
    sanitized: string;
    role: RoleCursor;
    status: StatusCursor;
    audit: CodingAuditCursor;
    repair: ReasoningRepairCursor;
    roleOut: string;
    cleaned: string;
};

export function createReasoningTrailCleanCache(): ReasoningTrailCleanCache {
    return {
        id: "",
        raw: "",
        sanitized: "",
        role: emptyRole(),
        status: emptyStatus(),
        audit: createCodingAuditCursor(),
        repair: createReasoningRepairCursor(),
        roleOut: "",
        cleaned: "",
    };
}

/** Id keeps one thought's cursor off the next row when a component instance is reused. */
export function rememberReasoningTrailClean(cache: ReasoningTrailCleanCache, id: string, raw: string): string {
    const text = raw || "";
    if (cache.id === id && cache.raw === text) return cache.cleaned;
    if (cache.id !== id || (cache.raw && !text.startsWith(cache.raw))) resetReasoningTrailClean(cache);
    cache.id = id;
    const sanitized = appendSanitized(cache, text);
    const roleOut = appendRole(cache.role, sanitized);
    if (roleOut === cache.roleOut && cache.raw !== "") {
        cache.raw = text;
        return cache.cleaned;
    }
    cache.roleOut = roleOut;
    const statusOut = appendStatus(cache.status, roleOut);
    const auditOut = appendCodingAuditStrip(cache.audit, statusOut);
    cache.cleaned = appendReasoningRepair(cache.repair, auditOut);
    cache.raw = text;
    return cache.cleaned;
}

function resetReasoningTrailClean(cache: ReasoningTrailCleanCache): void {
    cache.raw = "";
    cache.sanitized = "";
    cache.role = emptyRole();
    cache.status = emptyStatus();
    cache.audit = createCodingAuditCursor();
    cache.repair = createReasoningRepairCursor();
    cache.roleOut = "";
    cache.cleaned = "";
}

function appendSanitized(cache: ReasoningTrailCleanCache, raw: string): string {
    if (!raw.startsWith(cache.raw)) {
        cache.sanitized = sanitizeVisibleChatText(raw);
        return cache.sanitized;
    }
    if (raw === cache.raw) return cache.sanitized;
    cache.sanitized += sanitizeVisibleChatText(raw.slice(cache.raw.length));
    return cache.sanitized;
}

function emptyRole(): RoleCursor {
    return {
        input: "",
        output: "",
        frozen: false,
        fenceOpen: false,
        fenceAtLine: false,
        trailingTicks: 0,
        lineStart: 0,
        lineOpen: true,
    };
}

function emptyStatus(): StatusCursor {
    return { input: "", scanned: 0, kept: "", pendingBlanks: 0, started: false };
}

function appendRole(cursor: RoleCursor, input: string): string {
    if (cursor.frozen && input.startsWith(cursor.input)) {
        cursor.input = input;
        return cursor.output;
    }
    if (!input.startsWith(cursor.input)) return settleRole(cursor, input);
    if (input === cursor.input) return cursor.output;
    const delta = input.slice(cursor.input.length);
    const newline = delta.indexOf("\n");
    const fenceBefore = cursor.fenceOpen;
    if (!cursor.lineOpen && !fenceBefore && newline < 0 && !delta.includes("`")) {
        cursor.input = input;
        cursor.output = input;
        // The delta is not a backtick, so a partial fence at the old end is closed.
        cursor.trailingTicks = 0;
        return input;
    }
    const suffixStart = cursor.lineOpen && !cursor.fenceAtLine
        ? cursor.lineStart
        : cursor.input.length + (newline < 0 ? delta.length : newline + 1);
    const fenceForSuffix = suffixStart === cursor.lineStart ? cursor.fenceAtLine : fenceBeforeLine(cursor, input, suffixStart);
    if (suffixStart < input.length) {
        const found = matchRoleSuffix(fenceForSuffix, input.slice(suffixStart));
        if (found) {
            cursor.output = input.slice(0, suffixStart + found.index).trimEnd();
            cursor.frozen = true;
            cursor.input = input;
            return cursor.output;
        }
    }
    noteRoleTail(cursor, input, delta);
    cursor.output = input;
    cursor.input = input;
    return input;
}

function settleRole(cursor: RoleCursor, input: string): string {
    cursor.output = truncateRolePrefixForDisplay(input);
    cursor.frozen = cursor.output !== input;
    cursor.input = input;
    if (!cursor.frozen) noteRoleTail(cursor, input, input);
    return cursor.output;
}

function noteRoleTail(cursor: RoleCursor, input: string, delta: string): void {
    const lineStart = input.lastIndexOf("\n") + 1;
    if (lineStart === cursor.lineStart && delta !== input) {
        const atEnd = tickFence(cursor.fenceOpen, cursor.trailingTicks, delta);
        cursor.fenceOpen = atEnd.open;
        cursor.trailingTicks = atEnd.ticks;
    } else {
        const atLine = tickFence(false, 0, input.slice(0, lineStart));
        const atEnd = tickFence(atLine.open, atLine.ticks, input.slice(lineStart));
        cursor.fenceAtLine = atLine.open;
        cursor.fenceOpen = atEnd.open;
        cursor.trailingTicks = atEnd.ticks;
        cursor.lineStart = lineStart;
    }
    cursor.lineOpen = !cursor.fenceAtLine && rolePrefixLineOpen(input.slice(cursor.lineStart));
}

function fenceBeforeLine(cursor: RoleCursor, input: string, lineAt: number): boolean {
    return tickFence(cursor.fenceOpen, cursor.trailingTicks, input.slice(cursor.input.length, lineAt)).open;
}

function matchRoleSuffix(fenceOpen: boolean, suffix: string): { index: number } | null {
    const pad = fenceOpen ? "```\n" : "";
    const found = findRolePrefixForDisplay(pad + suffix);
    if (!found) return null;
    const index = found.index - pad.length;
    if (index < 0) return null;
    return { index };
}

function tickFence(open: boolean, trailing: number, chunk: string): { open: boolean; ticks: number } {
    const probe = `${"`".repeat(trailing)}${chunk}`;
    let fence = open;
    let consumed = 0;
    let index = 0;
    while (index < probe.length) {
        const at = probe.indexOf("```", index);
        if (at < 0) break;
        fence = !fence;
        index = at + 3;
        consumed = index;
    }
    let ticks = 0;
    for (let mark = probe.length - 1; mark >= consumed && probe[mark] === "`" && ticks < 2; mark -= 1) ticks += 1;
    return { open: fence, ticks };
}

function appendStatus(cursor: StatusCursor, input: string): string {
    if (!input.startsWith(cursor.input)) {
        cursor.input = "";
        cursor.scanned = 0;
        cursor.kept = "";
        cursor.pendingBlanks = 0;
        cursor.started = false;
    }
    const tail = input.slice(cursor.scanned);
    const br = lastLineBreak(tail);
    if (!br) {
        cursor.input = input;
        return finishStatus(cursor, tail);
    }
    const completed = tail.slice(0, br.index);
    const incomplete = tail.slice(br.index + br.length);
    const lines = completed === "" && br.index === 0 ? [""] : completed.split(/\r?\n/);
    for (const line of lines) absorbStatusLine(cursor, line);
    cursor.scanned = input.length - incomplete.length;
    cursor.input = input;
    return finishStatus(cursor, incomplete);
}

function absorbStatusLine(cursor: StatusCursor, line: string): void {
    if (isCodingWorkbenchStatusLine(line)) return;
    if (line === "") {
        cursor.pendingBlanks += 1;
        return;
    }
    if (!cursor.started) {
        cursor.pendingBlanks = 0;
        cursor.kept = line;
        cursor.started = true;
        return;
    }
    cursor.kept += `${"\n".repeat(cursor.pendingBlanks + 1)}${line}`;
    cursor.pendingBlanks = 0;
}

function finishStatus(cursor: StatusCursor, incomplete: string): string {
    if (!incomplete || isCodingWorkbenchStatusLine(incomplete)) return cursor.kept;
    if (!cursor.started) return incomplete;
    return `${cursor.kept}${"\n".repeat(cursor.pendingBlanks + 1)}${incomplete}`;
}

function lastLineBreak(value: string): { index: number; length: number } | null {
    const nl = value.lastIndexOf("\n");
    if (nl < 0) return null;
    if (nl > 0 && value[nl - 1] === "\r") return { index: nl - 1, length: 2 };
    return { index: nl, length: 1 };
}
