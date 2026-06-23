const displayRolePrefixPattern = /^[ \t>*\-]*(?:\d+\.[ \t]*)?(Browser|Tool)[ \t]*(?::[ \t]?|：)/m;

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
