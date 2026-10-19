const displayRolePrefixPattern = /^[ \t>*\-]*(?:\d+[\.)][ \t]*)?(Browser|Tool)[ \t]*(?::[ \t]?|：)/m;

type RolePrefixDisplayMode = "keep-body" | "truncate";

export interface RolePrefixDisplayMatch {
    kind: "Browser" | "Tool";
    index: number;
    prefixEnd: number;
    atStart: boolean;
}

export function findRolePrefixForDisplay(text: string): RolePrefixDisplayMatch | null {
    if (!text) return null;
    if (!text.includes("Browser") && !text.includes("Tool")) return null;
    if (!text.includes(":") && !text.includes("：")) return null;

    const parts: Array<{ text: string; isCode: boolean }> = [];
    let rest = text;
    while (rest.length > 0) {
        const open = rest.indexOf("```");
        if (open < 0) {
            parts.push({ text: rest, isCode: false });
            break;
        }
        if (open > 0) parts.push({ text: rest.slice(0, open), isCode: false });
        const close = rest.indexOf("```", open + 3);
        if (close < 0) {
            parts.push({ text: rest.slice(open), isCode: true });
            break;
        }
        const end = close + 3;
        parts.push({ text: rest.slice(open, end), isCode: true });
        rest = rest.slice(end);
    }

    let offset = 0;
    for (const part of parts) {
        if (!part.isCode) {
            const match = displayRolePrefixPattern.exec(part.text);
            if (match && match.index !== undefined) {
                const start = offset + match.index;
                const prefixEnd = start + match[0].length;
                return {
                    kind: match[1] as "Browser" | "Tool",
                    index: start,
                    prefixEnd,
                    atStart: text.slice(0, start).trim().length === 0,
                };
            }
        }
        offset += part.text.length;
    }
    return null;
}

function stripRolePrefixForDisplayMode(text: string, mode: RolePrefixDisplayMode): string {
    const match = findRolePrefixForDisplay(text);
    if (!match) return text;
    const before = text.slice(0, match.index).trimEnd();
    if (before || mode === "truncate") return before;
    return text.slice(match.prefixEnd).trimStart();
}

export function stripRolePrefixForDisplay(text: string): string {
    return stripRolePrefixForDisplayMode(text, "keep-body");
}

export function truncateRolePrefixForDisplay(text: string): string {
    return stripRolePrefixForDisplayMode(text, "truncate");
}

export type RolePrefixStreamCursor = {
    mode: "keep-body" | "truncate";
    input: string;
    output: string;
    frozen: boolean;
    keepBody: boolean;
    fenceOpen: boolean;
    trailingTicks: number;
    lineStart: number;
    tailStart: number;
    tailInCode: boolean;
    lineOpen: boolean;
};

export function createRolePrefixStreamCursor(mode: "keep-body" | "truncate"): RolePrefixStreamCursor {
    return {
        mode,
        input: "",
        output: "",
        frozen: false,
        keepBody: false,
        fenceOpen: false,
        trailingTicks: 0,
        lineStart: 0,
        tailStart: 0,
        tailInCode: false,
        lineOpen: true,
    };
}

/**
 * Same result as strip/truncateRolePrefixForDisplay. A token that stays on a
 * dead line does not rescan the answer; a newline, carriage return, backtick,
 * or a line that can still become Browser/Tool falls back to the full scan.
 */
export function appendRolePrefixStrip(cursor: RolePrefixStreamCursor, text: string): string {
    const input = text || "";
    if (input === cursor.input) return cursor.output;
    if (cursor.input && !input.startsWith(cursor.input)) {
        const mode = cursor.mode;
        const fresh = createRolePrefixStreamCursor(mode);
        cursor.mode = fresh.mode;
        cursor.input = fresh.input;
        cursor.output = fresh.output;
        cursor.frozen = fresh.frozen;
        cursor.keepBody = fresh.keepBody;
        cursor.fenceOpen = fresh.fenceOpen;
        cursor.trailingTicks = fresh.trailingTicks;
        cursor.lineStart = fresh.lineStart;
        cursor.tailStart = fresh.tailStart;
        cursor.tailInCode = fresh.tailInCode;
        cursor.lineOpen = fresh.lineOpen;
        return appendRolePrefixStrip(cursor, input);
    }
    if (cursor.frozen) {
        const delta = input.slice(cursor.input.length);
        if (cursor.keepBody) cursor.output = cursor.output ? cursor.output + delta : delta.trimStart();
        cursor.input = input;
        return cursor.output;
    }
    const delta = input.slice(cursor.input.length);
    // A dead line cannot become Browser/Tool. Newlines in the new piece only
    // need a scan of that piece; a backtick can retoggle an earlier fence.
    if (!cursor.frozen && !delta.includes("`") && (cursor.tailInCode || !cursor.lineOpen)) {
        const hasBreak = delta.includes("\n") || delta.includes("\r");
        if (!hasBreak) {
            cursor.input = input;
            cursor.output = input;
            cursor.trailingTicks = 0;
            return input;
        }
        if (cursor.tailInCode || !rolePrefixAfterBreak(delta)) {
            const previousLength = cursor.input.length;
            advanceRoleFence(cursor, previousLength, input);
            cursor.input = input;
            cursor.output = input;
            cursor.lineOpen = !cursor.tailInCode && rolePrefixLineOpen(input.slice(cursor.tailStart));
            return input;
        }
    }
    const applied = applyRolePrefixMode(cursor.mode, input);
    const previousLength = cursor.input.length;
    cursor.output = applied.output;
    cursor.frozen = applied.frozen;
    cursor.keepBody = applied.keepBody;
    advanceRoleFence(cursor, previousLength, input);
    cursor.input = input;
    if (!cursor.frozen) cursor.lineOpen = !cursor.tailInCode && rolePrefixLineOpen(input.slice(cursor.tailStart));
    return cursor.output;
}

/** True when a line that starts inside this delta is a Browser/Tool prefix. */
function rolePrefixAfterBreak(delta: string): boolean {
    const at = delta.search(/[\n\r]/);
    if (at < 0) return false;
    return findRolePrefixForDisplay(delta.slice(at)) !== null;
}

function applyRolePrefixMode(mode: "keep-body" | "truncate", input: string): { output: string; frozen: boolean; keepBody: boolean } {
    const match = findRolePrefixForDisplay(input);
    if (!match) return { output: input, frozen: false, keepBody: false };
    const before = input.slice(0, match.index).trimEnd();
    if (before || mode === "truncate") return { output: before, frozen: true, keepBody: false };
    return { output: input.slice(match.prefixEnd).trimStart(), frozen: true, keepBody: true };
}

function advanceRoleFence(cursor: RolePrefixStreamCursor, previousLength: number, input: string): void {
    const delta = input.slice(previousLength);
    let open = cursor.fenceOpen;
    let ticks = cursor.trailingTicks;
    let lineStart = cursor.lineStart;
    let tailStart = cursor.tailStart;
    let tailInCode = cursor.tailInCode;
    let index = 0;
    while (index < delta.length) {
        const ch = delta[index];
        if (ch === "\n" || ch === "\r") {
            lineStart = previousLength + index + 1;
            tailStart = lineStart;
            tailInCode = open;
            ticks = 0;
            index += 1;
            continue;
        }
        if (ch !== "`") {
            ticks = 0;
            index += 1;
            continue;
        }
        let run = ticks;
        while (index < delta.length && delta[index] === "`") {
            run += 1;
            index += 1;
        }
        if (Math.floor(run / 3) % 2 === 1) open = !open;
        ticks = run % 3;
        tailInCode = open;
        if (!open) tailStart = previousLength + index;
    }
    cursor.fenceOpen = open;
    cursor.trailingTicks = ticks;
    cursor.lineStart = lineStart;
    cursor.tailStart = tailStart;
    cursor.tailInCode = tailInCode;
}

/**
 * True when this line does not yet match a Browser/Tool prefix, but more
 * characters on the same line still could. A dead line stays dead: the
 * prefix only matches at the line start.
 */
export function rolePrefixLineOpen(line: string): boolean {
    if (!line) return true;
    if (displayRolePrefixPattern.test(line)) return false;
    let index = 0;
    while (index < line.length && isRolePrefixLead(line.charCodeAt(index))) index += 1;
    if (index >= line.length) return true;
    const code = line.charCodeAt(index);
    if (code >= 48 && code <= 57) {
        while (index < line.length && line.charCodeAt(index) >= 48 && line.charCodeAt(index) <= 57) index += 1;
        if (index >= line.length) return true;
        const mark = line[index];
        if (mark !== "." && mark !== ")") return false;
        index += 1;
        if (index >= line.length) return true;
        while (index < line.length && (line[index] === " " || line[index] === "\t")) index += 1;
        if (index >= line.length) return true;
    }
    const rest = line.slice(index);
    for (const word of ["Browser", "Tool"]) {
        if (word.startsWith(rest)) return true;
        if (!rest.startsWith(word)) continue;
        let after = word.length;
        while (after < rest.length && (rest[after] === " " || rest[after] === "\t")) after += 1;
        if (after >= rest.length) return true;
        return false;
    }
    return false;
}

function isRolePrefixLead(code: number): boolean {
    return code === 32 || code === 9 || code === 62 || code === 42 || code === 45;
}

// Response chips used this label to repeat the answer. It is not a tool result.
export function isBrowserEchoFieldLabel(label: string): boolean {
    return label.trim().toLowerCase() === "browser";
}
