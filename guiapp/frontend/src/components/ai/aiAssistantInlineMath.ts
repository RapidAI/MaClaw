/**
 * Shared inline math protection for the assistant markdown renderer.
 *
 * Both the inline math splitter (`splitInlineMath`) and the markdown table cell
 * splitter (`parseMarkdownTableCells`) must agree on which `$` / `$$` / `\(` /
 * `\[` opens a TeX formula. A `|` inside any of those spans is a cell
 * separator, never a column cut. Keep the opener/close rules in one place so a
 * new delimiter cannot drift between the two consumers.
 */

export type InlineMathDelimiter = "$" | "$$" | "\\(" | "\\[";

export function isEscapedAt(text: string, index: number): boolean {
    let slashCount = 0;
    for (let cursor = index - 1; cursor >= 0 && text[cursor] === "\\"; cursor--) slashCount++;
    return slashCount % 2 === 1;
}

export function isInlineMathBoundaryBefore(text: string, index: number): boolean {
    const previous = text[index - 1] || "";
    // TeX delimiters do not begin in the middle of identifiers, prices, or a
    // word such as `US$`. This also prevents a lone closing dollar in prose from
    // swallowing a later formula.
    // Chinese prose commonly omits the space before a formula ("令$x$...").
    // Treat Han characters as natural prose boundaries while keeping `$` inside
    // Latin identifiers (for example `price$USD`) untouched.
    return !previous || !/[\p{L}\p{N}_]/u.test(previous) || /\p{Script=Han}/u.test(previous);
}

/**
 * Whether the `$` at `index` opens a formula whose very first token is a TeX
 * command: `$\alpha$`, `$\frac{1}{2}$`, `$\beta_1$`. A `\` followed by a letter
 * right after the opening dollar is a strong structural signal that this is
 * mathematics — currency shorthands and identifier-internal dollars never carry
 * a backslash there (`price$USD`, `$5`, `US$`).
 *
 * Such a formula legitimately abuts a Latin identifier on the left, for example
 * a gene/protein name glued to its Greek-letter subscript: `SIRP$\alpha$`,
 * `IL-36$\gamma$`. The prose-boundary heuristic would otherwise read the `P$`
 * junction as "dollar inside a word" and refuse the delimiter; the command
 * after the `$` disambiguates it back to math. This is a mechanism, not a
 * per-formula special case: it keys on the `\` + letter shape, not the literal
 * text, so every command-led formula benefits and non-math `$` stays rejected.
 */
export function dollarOpensTexCommand(text: string, index: number): boolean {
    return text[index] === "$" && text[index + 1] === "\\" && /[\p{L}]/u.test(text[index + 2] || "");
}

export function isInlineMathBoundaryAfter(text: string, index: number): boolean {
    const next = text[index + 1] || "";
    return !next || !/[\p{L}\p{N}_]/u.test(next) || /\p{Script=Han}/u.test(next);
}

export function findUnescapedDelimiter(text: string, delimiter: string, from: number): number {
    let index = text.indexOf(delimiter, from);
    while (index !== -1 && isEscapedAt(text, index)) index = text.indexOf(delimiter, index + delimiter.length);
    return index;
}

/**
 * A `$` followed by a digit is usually a price, not TeX — but a formula can
 * start with a number: `$0.786\pm0.004$`, `$2^n$`, `$0.934$`. A digit-led span
 * counts as math only when the inside carries TeX structure, or is nothing but
 * a number the author still wrapped as a formula. "$5 to $10" stays currency.
 */
export function digitLedSpanIsMath(inner: string): boolean {
    // A span holding another delimiter or Han prose is a currency pair that
    // ran into a later formula, not mathematics ("$5 到 $10，公式为 $x$").
    if (inner.includes("$") || /\p{Script=Han}/u.test(inner)) return false;
    return /[\\^_{}]/.test(inner) || /^\s*[-+]?\d[\d.,\s]*$/.test(inner);
}

/**
 * When `text` at `index` opens one of the supported inline math forms,
 * return the delimiter that just opened. Otherwise return `""`.
 *
 * Order matters: `$$` is checked before `$` so a display pair is not mistaken
 * for an empty single-dollar span, and `\(` / `\[` before `$` so a backslash
 * form wins on a `\` character.
 */
export function inlineMathDelimiterAt(text: string, index: number): InlineMathDelimiter | "" {
    const char = text[index];
    if (char === "\\" && !isEscapedAt(text, index)) {
        // Require a non-empty body so `\\(\\)` / `\\[\\]` do not masquerade as
        // math; the single-$ and $$ checks below impose the same constraint.
        const parenClose = text.startsWith("\\(", index) ? findUnescapedDelimiter(text, "\\)", index + 2) : -1;
        if (parenClose > index + 2) return "\\(";
        const bracketClose = text.startsWith("\\[", index) ? findUnescapedDelimiter(text, "\\]", index + 2) : -1;
        if (bracketClose > index + 2) return "\\[";
        return "";
    }
    if (char !== "$" || isEscapedAt(text, index)) return "";
    // A `$` that immediately opens a TeX command (`$\alpha$`) is math even when
    // it directly abuts a Latin identifier on the left (`SIRP$\alpha$`) — the
    // backslash-letter right after the dollar is structural proof, so the
    // identifier-boundary rejection must not veto it.
    if (!isInlineMathBoundaryBefore(text, index) && !dollarOpensTexCommand(text, index)) return "";
    if (text[index + 1] === "$") {
        const close = findUnescapedDelimiter(text, "$$", index + 2);
        if (close > index + 2 && isInlineMathBoundaryAfter(text, close + 1)) return "$$";
        return "";
    }
    // Single `$`: not math when followed by whitespace, and digit-led spans
    // must carry TeX structure so "$5 | $10" still splits into two cells.
    if (/\s/u.test(text[index + 1] || "")) return "";
    if (hasSingleDollarClose(text, index)) return "$";
    return "";
}

function hasSingleDollarClose(text: string, start: number): boolean {
    const digitLed = /\d/u.test(text[start + 1] || "");
    for (let index = start + 1; index < text.length && text[index] !== "\n"; index++) {
        if (
            text[index] === "$"
            && !isEscapedAt(text, index)
            && text[index + 1] !== "$"
            && isInlineMathBoundaryAfter(text, index)
            && !/\s/u.test(text[index - 1] || "")
        ) {
            return !digitLed || digitLedSpanIsMath(text.slice(start + 1, index));
        }
    }
    return false;
}

/**
 * Return the end index (exclusive) of the math span that `delimiter` opened at
 * `index`, or `-1` when there is no unescaped close. `delimiter` must have been
 * returned by `inlineMathDelimiterAt` (i.e. an open is guaranteed).
 */
export function inlineMathSpanEnd(text: string, index: number, delimiter: InlineMathDelimiter): number {
    switch (delimiter) {
        case "\\(": {
            const close = findUnescapedDelimiter(text, "\\)", index + 2);
            return close === -1 ? -1 : close + 2;
        }
        case "\\[": {
            const close = findUnescapedDelimiter(text, "\\]", index + 2);
            return close === -1 ? -1 : close + 2;
        }
        case "$$": {
            const close = findUnescapedDelimiter(text, "$$", index + 2);
            return close === -1 ? -1 : close + 2;
        }
        case "$": {
            for (let index2 = index + 1; index2 < text.length && text[index2] !== "\n"; index2++) {
                if (
                    text[index2] === "$"
                    && !isEscapedAt(text, index2)
                    && text[index2 + 1] !== "$"
                    && isInlineMathBoundaryAfter(text, index2)
                    && !/\s/u.test(text[index2 - 1] || "")
                ) {
                    return index2 + 1;
                }
            }
            return -1;
        }
    }
}

/**
 * Skip a finished inline math span starting at `index`. Returns the end index
 * (exclusive) of the whole span including delimiters, or `-1` when `index` does
 * not open a formula. Used by emphasis splitting and table cell splitting to
 * keep `|` / `*` inside TeX opaque.
 */
export function skipDelimitedMathSpan(text: string, index: number): number {
    const delimiter = inlineMathDelimiterAt(text, index);
    if (!delimiter) return -1;
    return inlineMathSpanEnd(text, index, delimiter);
}
