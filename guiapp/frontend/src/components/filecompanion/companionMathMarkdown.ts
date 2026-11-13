/**
 * Companion replies sometimes wrap a formula in backticks or a bare fence
 * instead of TeX delimiters. The Markdown painter deliberately keeps code
 * spans literal (`` `$x$` `` must not become mathematics), so such a formula
 * reaches the reader as raw source: `x_i = f(I, q, d_i)` in a code chip and
 * `R^{D_l}` inside a grey well.
 *
 * Rewrite only spans and fences that are unmistakably mathematics into $...$
 * or $$...$$ before the renderer sees them. A formula that already carries TeX
 * delimiters is unwrapped instead of wrapped twice. Real code, paths, LaTeX
 * source and non-math code-labelled fences stay as they are.
 *
 * A reply also writes the same notation straight into the sentence
 * (`F^l_{0,c}`, `L_ce`, `O^l`). Those carets and underscores are not Markdown,
 * and the chat column will break them mid-symbol. Wrap each such atom in
 * `$...$` so KaTeX typesets it. Words, snake_case names and URLs stay text.
 *
 * Bare TeX commands get the same treatment (`τ \approx 1`, `0.786\pm0.004`):
 * a run of known command names — with their arguments, scripts, `\left..\right`
 * pairs and `\begin..\end` environments — is wrapped in `$...$`. Commands that
 * cannot stand alone (`\frac` with no group, a lone `\left`) stay prose.
 */

const MATH_FENCE_INFO = /^(?:math|latex|tex|katex|mathjax|amsmath)\b/i;
const FENCE_LINE = /^(\s*)(`{3,}|~{3,})([^\n\r]*)$/;
const FENCE_CLOSER = /^(\s*)(`{3,}|~{3,})\s*$/;

// Punctuation that only source code carries. A formula can contain a TeX
// command and a word such as `\text{return}`, so these stay separate from the
// keyword list below and are checked before the TeX override.
const HARD_CODE_SIGNAL = /[;"]|=>|\/\/|\/\*|#include/;
// Case-sensitive on purpose: `\mathrm{Var}` is statistics, `Var` in a code
// sample is not; SQL keywords are conventionally uppercase.
const KEYWORD_CODE_SIGNAL = /\b(?:def|function|return|class|import|package|const|let|var|public|private|static|SELECT|INSERT|UPDATE|DELETE|printf|console\.log|fmt\.Print|System\.out)\b/;
// A drive letter anywhere means a path, even when a backslash run such as
// `\Users` looks like a TeX command. The letter must start a token, so a prose
// or formula colon (`note:\frac`) is not read as a drive. A Unix home/system
// prefix does the same.
const WINDOWS_PATH = /(?:^|[^A-Za-z0-9])[A-Za-z]:\\/;
const UNIX_PATH = /(?:^|\s)~?\/?(?:Users|home|tmp|var|opt|etc|usr)\//;
// Document-level LaTeX is source code the reader may want to see, not a
// formula to typeset. Math environments (`aligned`, `cases`, ...) stay math.
const LATEX_SOURCE_SIGNAL = /\\(?:documentclass|usepackage|newcommand|renewcommand|section|subsection|subsubsection|chapter|maketitle|tableofcontents|bibliography|bibliographystyle|input|include|draw|node|path|fill|addplot|usetikzlibrary)\b|\\begin\{(?:document|tikzpicture|figure|table|tabular|itemize|enumerate|description|center|quote|quotation|verbatim|lstlisting|thebibliography)\}|\\end\{document\}/;

const TEX_COMMAND = /\\[A-Za-z]{2,}/;
const MATH_SYMBOL = /[∈⊙∑∏∫√≤≥≈≠≡∝∞∂∇±×·÷→←↔∪∩⊂⊆∀∃⟶⟹⇒⇔⟨⟩]/u;
const SCRIPT_MARK = /[_^]/;
const SCRIPT_MARK_ALL = /[_^]/g;
const OPERATOR = /[=<>]/;

/** A short variable with a subscript or superscript, as in `a_j(q, d_i)`. */
const SUBSCRIPTED_CALL = /(?:^|[^A-Za-z0-9_])[A-Za-z]_\{?\w+\}?\s*\([^()]*_[^()]*\)/;
/** A one-letter variable assignment, as in `r*_i = i`. */
const SIMPLE_SCRIPT_ASSIGNMENT = /^[A-Za-z]\*?[_^]\{?[A-Za-z0-9]\}?\s*[=<>]/;

/** True when the text is already one complete TeX-delimited expression. */
function isDelimitedMath(text: string): boolean {
    if (text.length < 3) return false;
    if (text.startsWith("$$") && text.endsWith("$$")) return !text.slice(2, -2).includes("$");
    if (text.startsWith("$") && text.endsWith("$")) return !text.slice(1, -1).includes("$");
    if (text.startsWith("\\(") && text.endsWith("\\)")) return !text.includes("$");
    if (text.startsWith("\\[") && text.endsWith("\\]")) return !text.includes("$");
    return false;
}

/**
 * True when `source` is a formula the author wrote as code, not code itself.
 * `block` is true for a fenced body, where an assignment needs stronger
 * evidence: `user_id = row[0]` is code, `a_j(q) = [a_j(q,d_1), ...]` is math.
 */
export function looksLikeCompanionMath(source: string, block = false): boolean {
    const text = source.trim();
    if (!text) return false;
    if (text.length > (block ? 2000 : 240)) return false;
    if (text.includes("$") || text.includes("`")) return false;
    if (WINDOWS_PATH.test(text) || UNIX_PATH.test(text)) return false;
    if (LATEX_SOURCE_SIGNAL.test(text)) return false;
    // `return x + 1` is code; `\mathrm{Var}(X)` is mathematics. The command
    // wins over keywords, but never over code punctuation or a path.
    if (HARD_CODE_SIGNAL.test(text)) return false;
    if (TEX_COMMAND.test(text)) return true;
    if (KEYWORD_CODE_SIGNAL.test(text)) return false;
    if (MATH_SYMBOL.test(text)) return true;
    // `F^l_{0,c}` is a formula even with no equals sign. `user_id` is not.
    if (isPlainTexAtom(text)) return true;
    if (!SCRIPT_MARK.test(text)) return false;

    if (!OPERATOR.test(text)) return SUBSCRIPTED_CALL.test(text);
    if (!block) return true;
    const scripts = text.match(SCRIPT_MARK_ALL)?.length ?? 0;
    return scripts >= 2 || SIMPLE_SCRIPT_ASSIGNMENT.test(text) || SUBSCRIPTED_CALL.test(text);
}

/**
 * Rewrite single-backtick spans that hold a formula. Multi-backtick spans are
 * the author's explicit literal-code form and are copied through untouched.
 */
function inlineCodeMath(line: string): string {
    if (!line.includes("`")) return line;
    let out = "";
    let index = 0;
    while (index < line.length) {
        const open = line.indexOf("`", index);
        if (open === -1) {
            out += line.slice(index);
            break;
        }
        out += line.slice(index, open);
        let run = 1;
        while (line[open + run] === "`") run++;
        const fence = "`".repeat(run);
        const close = line.indexOf(fence, open + run);
        if (run > 1) {
            // ``...`` is the author's literal-code span. An unclosed run means
            // the rest of the line is still that literal span.
            if (close === -1) {
                out += line.slice(open);
                break;
            }
            out += line.slice(open, close + run);
            index = close + run;
            continue;
        }
        if (close !== -1) {
            const inner = line.slice(open + 1, close);
            const trimmed = inner.trim();
            // The author already wrote a complete formula; the backticks only
            // protected it from the Markdown parser. Keep the delimiters.
            if (isDelimitedMath(trimmed)) {
                out += trimmed;
                index = close + 1;
                continue;
            }
            if (looksLikeCompanionMath(inner)) {
                out += `$${trimmed}$`;
                index = close + 1;
                continue;
            }
        }
        out += fence;
        index = open + run;
    }
    return out;
}

// A short base plus a TeX script: `F^l_{0,c}`, `L_ce`, `O^l`, `Ŷ_ag`, `F0^N`, `x^*`.
// One nested brace level covers `F^{l_{0,c}}` and `x_{i_{j}}`. Deeper nesting
// stays source; a half-written brace must not be wrapped as a shorter atom.
// The base stays short so `user_id` and `in_the` are names, not subscripts.
const PLAIN_TEX_BASE = /(?:[A-Za-z]{1,2}\d*|[\u0391-\u03A9\u03B1-\u03C9\u2113\u0176\u0177])\p{M}*/u;
const PLAIN_TEX_BRACE = "(?:[^{}\\n]|\\{[^{}\\n]*\\})+";
const PLAIN_TEX_SCRIPT = new RegExp(`(?:\\^\\{${PLAIN_TEX_BRACE}\\}|\\^[\\*∗]|\\^[A-Za-z0-9]+|_\\{${PLAIN_TEX_BRACE}\\}|_[A-Za-z0-9]+)`, "u");
// A prime may sit before each script (`F'_0`, `F_0'^l`, `σ''^2`). A star prime is `F*_0`.
const PLAIN_TEX_PRIME = "(?:['′]{1,3}|[*∗])";
const PLAIN_TEX_ATOM_SRC = `${PLAIN_TEX_BASE.source}(?:${PLAIN_TEX_PRIME}?${PLAIN_TEX_SCRIPT.source})+`;
const PLAIN_TEX_ATOM = new RegExp(`^${PLAIN_TEX_ATOM_SRC}`, "u");
// Sticky, and not anchored, so a match is attempted at lastIndex without copying the tail.
const PLAIN_TEX_ATOM_AT = new RegExp(PLAIN_TEX_ATOM_SRC, "uy");
const PLAIN_TEX_SCRIPT_AT = new RegExp(PLAIN_TEX_SCRIPT.source, "uy");
const PLAIN_TEX_BASE_AT = new RegExp(`^${PLAIN_TEX_BASE.source}`, "u");
// `{the result}` is a phrase. `{0, c}` and `^{(1, j)}` are scripts.
const PROSE_BRACE = /\{[^{}]* [A-Za-z]{3,}/;

function isPlainTexAtom(text: string): boolean {
    const match = PLAIN_TEX_ATOM.exec(text);
    return !!match && match[0].length === text.length && plainTexAtomAccept(match[0]);
}

/**
 * A two-letter word with a word subscript (`in_the`, `py_v2`) is a name.
 * `CO_2` and `L_total` are not. A brace that opens onto a word is a phrase.
 */
function plainTexAtomAccept(atom: string): boolean {
    if (atom.length > 48 || PROSE_BRACE.test(atom)) return false;
    // `F_0_1` and `x^2^3` are a second script of the same kind. KaTeX rejects them.
    if (topLevelScriptCount(atom, "_") > 1 || topLevelScriptCount(atom, "^") > 1) return false;
    const base = PLAIN_TEX_BASE_AT.exec(atom);
    if (!base) return false;
    const letters = base[0].replace(/\p{M}/gu, "");
    // A caret does not rescue a word (`in_the^2`). `CO_2` and `km^2` stay formulas.
    if (/^[A-Za-z]{2}$/.test(letters)) {
        for (const match of atom.matchAll(/_\{([^{}]+)\}|_([A-Za-z0-9]+)/g)) {
            const part = match[1] || match[2];
            if (!/^[A-Za-z]$/.test(part) && !/^\d+$/.test(part)) return false;
        }
    }
    if (atom.includes("^") || atom.includes("{")) return true;
    const scripts = atom.slice(base[0].length).replace(/^['′*∗]+/u, "");
    const parts = scripts.split("_").filter(Boolean);
    if (parts.length === 0 || parts.some((part) => part.length > 8)) return false;
    return true;
}

// Unicode subscripts and superscripts models emit instead of `^` / `_`:
// `λ₁`, `F₀ˡ`, `F₀,cˡ`, `Σᵢ`, `F₀ᴺ`.
const UNICODE_SUBSCRIPT = new Map<string, string>([
    ["₀", "0"], ["₁", "1"], ["₂", "2"], ["₃", "3"], ["₄", "4"],
    ["₅", "5"], ["₆", "6"], ["₇", "7"], ["₈", "8"], ["₉", "9"],
    ["₊", "+"], ["₋", "-"], ["₌", "="], ["₍", "("], ["₎", ")"],
    ["ₐ", "a"], ["ₑ", "e"], ["ₒ", "o"], ["ₓ", "x"], ["ₔ", "ə"],
    ["ₕ", "h"], ["ₖ", "k"], ["ₗ", "l"], ["ₘ", "m"], ["ₙ", "n"],
    ["ₚ", "p"], ["ₛ", "s"], ["ₜ", "t"],
    ["ᵢ", "i"], ["ᵣ", "r"], ["ᵤ", "u"], ["ᵥ", "v"], ["ⱼ", "j"],
]);
const UNICODE_SUPERSCRIPT = new Map<string, string>([
    ["⁰", "0"], ["¹", "1"], ["²", "2"], ["³", "3"], ["⁴", "4"],
    ["⁵", "5"], ["⁶", "6"], ["⁷", "7"], ["⁸", "8"], ["⁹", "9"],
    ["⁺", "+"], ["⁻", "-"], ["⁼", "="], ["⁽", "("], ["⁾", ")"],
    ["ⁿ", "n"], ["ⁱ", "i"],
    ["ᵃ", "a"], ["ᵇ", "b"], ["ᶜ", "c"], ["ᵈ", "d"], ["ᵉ", "e"],
    ["ᶠ", "f"], ["ᵍ", "g"], ["ʰ", "h"], ["ʲ", "j"], ["ᵏ", "k"],
    ["ˡ", "l"], ["ᵐ", "m"], ["ᵒ", "o"], ["ᵖ", "p"], ["ʳ", "r"],
    ["ˢ", "s"], ["ᵗ", "t"], ["ᵘ", "u"], ["ᵛ", "v"], ["ʷ", "w"],
    ["ˣ", "x"], ["ʸ", "y"], ["ᶻ", "z"],
    ["ᴬ", "A"], ["ᴮ", "B"], ["ᴰ", "D"], ["ᴱ", "E"], ["ᴳ", "G"],
    ["ᴴ", "H"], ["ᴵ", "I"], ["ᴶ", "J"], ["ᴷ", "K"], ["ᴸ", "L"],
    ["ᴹ", "M"], ["ᴺ", "N"], ["ᴼ", "O"], ["ᴾ", "P"], ["ᴿ", "R"],
    ["ᵀ", "T"], ["ᵁ", "U"], ["ⱽ", "V"], ["ᵂ", "W"],
]);

function isUnicodeSubscript(ch: string): boolean {
    return UNICODE_SUBSCRIPT.has(ch);
}

function isUnicodeSuperscript(ch: string): boolean {
    return UNICODE_SUPERSCRIPT.has(ch);
}

function textMayContainUnicodeMath(text: string): boolean {
    for (let i = 0; i < text.length; i++) {
        const code = text.charCodeAt(i);
        // ASCII and Latin-1 below ² never carry these scripts. Han text does not either.
        if (code < 0x00b2 || (code >= 0x4e00 && code <= 0x9fff)) continue;
        if (code === 0x0176 || code === 0x0177 || code === 0x2113) return true;
        if ((code >= 0x0391 && code <= 0x03a9) || (code >= 0x03b1 && code <= 0x03c9)) return true;
        const ch = text[i];
        if (isUnicodeSubscript(ch) || isUnicodeSuperscript(ch)) return true;
    }
    return false;
}

/**
 * Unicode scripts after `start`, plus a short `,c` tail (`F₀,cˡ` → sub `0,c`, sup `l`).
 * Returns null when no unicode script was consumed.
 */
function consumeUnicodeMathTail(text: string, start: number): { end: number; sup: string; sub: string } | null {
    let i = start;
    let sup = "";
    let sub = "";
    let sawUnicode = false;
    let sawSub = false;
    while (i < text.length) {
        const ch = text[i];
        const asSub = UNICODE_SUBSCRIPT.get(ch);
        if (asSub) {
            sawUnicode = true;
            sawSub = true;
            sub += asSub;
            i++;
            continue;
        }
        const asSup = UNICODE_SUPERSCRIPT.get(ch);
        if (asSup) {
            sawUnicode = true;
            sup += asSup;
            i++;
            continue;
        }
        if (ch === "," && sawSub && /[A-Za-z0-9]/u.test(text[i + 1] || "")) {
            const after = text[i + 2] || "";
            if (!after || isUnicodeSubscript(after) || isUnicodeSuperscript(after) || after === "," || !/[\p{L}\p{N}_]/u.test(after)) {
                sub += "," + text[i + 1];
                i += 2;
                continue;
            }
        }
        break;
    }
    if (!sawUnicode) return null;
    return { end: i, sup, sub };
}

function formatUnicodeScripts(sup: string, sub: string): string {
    let out = "";
    if (sub) out += sub.length === 1 ? `_${sub}` : `_{${sub}}`;
    if (sup) out += sup.length === 1 ? `^${sup}` : `^{${sup}}`;
    return out;
}

/** Apostrophes glued to a variable (`σ''`, `F_0'`). Four or more stay a quote. */
function apostropheRun(text: string, index: number, limit: number): number {
    let count = 0;
    while (index + count < limit && count < 3 && (text[index + count] === "'" || text[index + count] === "′")) count++;
    if (count === 3 && index + 3 < limit && (text[index + 3] === "'" || text[index + 3] === "′")) return 0;
    return count;
}

/** A star prime, or one to three apostrophes (`F''₀`, `F_0'`). Four or more stay a quote. */
function primeRun(text: string, index: number, limit: number): number {
    if (index >= limit) return 0;
    const ch = text[index];
    if (ch === "*" || ch === "∗") return 1;
    return apostropheRun(text, index, limit);
}

/** A `^` / `_` still glued on, including one written after a prime (`F_0'^`). */
function gluedUnfinishedScript(text: string, end: number, limit: number): boolean {
    if (end < limit && (text[end] === "^" || text[end] === "_")) return true;
    const run = primeRun(text, end, limit);
    if (!run) return false;
    const after = text[end + run] || "";
    return after === "^" || after === "_" || isUnicodeSubscript(after) || isUnicodeSuperscript(after);
}

/**
 * A Greek letter, `Ŷ`, or `ℓ` standing alone (`γ=1`, `σ(`).
 * An apostrophe stays inside (`σ'(x)`, `σ''`). `λ1`, `μm`, `α^`, and `σ*` stay as they are.
 */
function matchLoneMathLetter(text: string, index: number, limit: number): string | null {
    const code = text.charCodeAt(index);
    const greek = (code >= 0x0391 && code <= 0x03a9) || (code >= 0x03b1 && code <= 0x03c9);
    if (!greek && code !== 0x2113 && code !== 0x0176 && code !== 0x0177) return null;
    const next = text[index + 1] || "";
    // `^` and `_` belong to this letter. A failed atom (`α^`, `σ^{the result}`) stays whole.
    if (next === "^" || next === "_") return null;
    if (next && (/[\p{L}\p{N}]/u.test(next) || isUnicodeSubscript(next) || isUnicodeSuperscript(next))) return null;
    if (next === "'" || next === "′") {
        const run = apostropheRun(text, index + 1, limit);
        // `σ'_0` is an atom. `σ'^` is unfinished, and `σ''''` is a quote.
        if (run === 0) return null;
        const after = text[index + 1 + run] || "";
        if (after === "^" || after === "_" || isUnicodeSubscript(after) || isUnicodeSuperscript(after)) return null;
        return text.slice(index, index + 1 + run);
    }
    return index + 1 <= limit ? text[index] : null;
}

/** `CO₂` is a formula. `inₜₕₑ` is a word, same as `in_the`. A subscript longer than 8 is a name. */
function unicodeTailAccept(base: string, sub: string): boolean {
    if (sub && sub.split(",").some((part) => part.length > 8)) return false;
    const letters = base.replace(/\p{M}/gu, "");
    if (!/^[A-Za-z]{2}$/.test(letters) || !sub) return true;
    return sub.split(",").every((part) => /^[A-Za-z]$/.test(part) || /^\d+$/.test(part));
}

function readAsciiScriptEnd(text: string, index: number, limit: number): number {
    let end = index;
    while (end < limit) {
        const run = primeRun(text, end, limit);
        const scriptAt = end + run;
        if (scriptAt >= limit) break;
        PLAIN_TEX_SCRIPT_AT.lastIndex = scriptAt;
        if (!PLAIN_TEX_SCRIPT_AT.exec(text)) break;
        if (PLAIN_TEX_SCRIPT_AT.lastIndex <= scriptAt || PLAIN_TEX_SCRIPT_AT.lastIndex > limit) break;
        end = PLAIN_TEX_SCRIPT_AT.lastIndex;
    }
    return end;
}

/** How many `^` or `_` sit outside braces. `^{l_{0}}` has a nested subscript. */
function topLevelScriptCount(ascii: string, mark: "_" | "^"): number {
    let depth = 0;
    let count = 0;
    for (let i = 0; i < ascii.length; i++) {
        const ch = ascii[i];
        if (ch === "{") depth++;
        else if (ch === "}" && depth > 0) depth--;
        else if (depth === 0 && ch === mark) count++;
    }
    return count;
}

/** A finished atom keeps a trailing apostrophe (`F_0'`). A following script does not. */
function appendLoosePrimes(text: string, limit: number, end: number, tex: string): { end: number; tex: string } {
    const run = apostropheRun(text, end, limit);
    if (!run) return { end, tex };
    const after = text[end + run] || "";
    if (after === "^" || after === "_" || isUnicodeSubscript(after) || isUnicodeSuperscript(after)) return { end, tex };
    return { end: end + run, tex: tex + text.slice(end, end + run) };
}

/**
 * Unicode scripts already in `tex`, then a following ASCII script (`F₀^{l}`).
 * An unclosed `^{` / `_{`, a dangling `^` (`F₀^`), a prose brace, or a second
 * copy of the same script (`F₀_{0,c}` → `F_0_{0,c}`) stays source.
 */
function finishScripted(text: string, limit: number, tex: string, end: number): { end: number; tex: string } | null {
    const asciiEnd = readAsciiScriptEnd(text, end, limit);
    if (scriptBraceFollows(text, asciiEnd)) return null;
    if (asciiEnd === end) {
        if (gluedUnfinishedScript(text, end, limit)) return null;
        if (tex.length > 48) return null;
        return appendLoosePrimes(text, limit, end, tex);
    }
    if (asciiEnd > limit) return null;
    const ascii = text.slice(end, asciiEnd);
    if ((tex.includes("_") && topLevelScriptCount(ascii, "_") > 0) || (tex.includes("^") && topLevelScriptCount(ascii, "^") > 0)) return null;
    const withAscii = tex + ascii;
    if (withAscii.length > 48 || PROSE_BRACE.test(withAscii)) return null;
    return appendLoosePrimes(text, limit, asciiEnd, withAscii);
}

function matchUnicodeAtom(text: string, index: number, limit: number): { end: number; tex: string } | null {
    const base = PLAIN_TEX_BASE_AT.exec(text.slice(index, Math.min(limit, index + 16)));
    if (!base || base.index !== 0) return null;
    let cursor = index + base[0].length;
    // `F'_0` already matches as ASCII. `F''₀` needs the same primes in front of the unicode script.
    let mark = "";
    let tail = null as ReturnType<typeof consumeUnicodeMathTail>;
    const primeLen = primeRun(text, cursor, limit);
    if (primeLen > 0) {
        tail = consumeUnicodeMathTail(text, cursor + primeLen);
        if (tail) {
            mark = text.slice(cursor, cursor + primeLen);
            cursor += primeLen;
        }
    }
    if (!tail) tail = consumeUnicodeMathTail(text, cursor);
    if (!tail || tail.end > limit || !unicodeTailAccept(base[0], tail.sub)) return null;
    return finishScripted(text, limit, base[0] + mark + formatUnicodeScripts(tail.sup, tail.sub), tail.end);
}

/**
 * A bare atom at `index`, rewritten with ASCII scripts.
 * `limit` is the end of an inline link label, so a destination is not consumed.
 */
function matchPromotedAtom(text: string, index: number, limit: number): { end: number; tex: string } | null {
    if (hasScriptSoon(text, index)) {
        const atom = matchPlainTexAtom(text, index);
        if (atom) {
            const atomEnd = index + atom.length;
            if (atomEnd > limit) return null;
            // `L_d'ˡ` keeps the prime between the ASCII subscript and the unicode superscript.
            let cursor = atomEnd;
            let primes = "";
            const run = primeRun(text, atomEnd, limit);
            const afterPrime = text[atomEnd + run] || "";
            if (run && (isUnicodeSubscript(afterPrime) || isUnicodeSuperscript(afterPrime))) {
                primes = text.slice(atomEnd, atomEnd + run);
                cursor = atomEnd + run;
            }
            const tail = consumeUnicodeMathTail(text, cursor);
            if (tail) {
                if (tail.end > limit) return primes ? null : { end: atomEnd, tex: atom };
                return finishScripted(text, limit, atom + primes + formatUnicodeScripts(tail.sup, tail.sub), tail.end);
            }
            // `L_d^` and `F_0'^` are still being written. Wrapping the finished part would leave the caret outside.
            if (primes || scriptBraceFollows(text, atomEnd) || gluedUnfinishedScript(text, atomEnd, limit)) return null;
            return appendLoosePrimes(text, limit, atomEnd, atom);
        }
        const unicode = matchUnicodeAtom(text, index, limit);
        if (unicode) return unicode;
    }
    const lone = matchLoneMathLetter(text, index, limit);
    if (!lone) return null;
    return { end: index + lone.length, tex: lone };
}

function canStartPlainTexAtom(text: string, index: number): boolean {
    const code = text.charCodeAt(index);
    if ((code >= 0x41 && code <= 0x5a) || (code >= 0x61 && code <= 0x7a)) return true;
    if (code === 0x2113 || code === 0x0176 || code === 0x0177) return true;
    return (code >= 0x0391 && code <= 0x03a9) || (code >= 0x03b1 && code <= 0x03c9);
}

/** A `_{` / `^{` still sitting on the token means the match cut a brace script short. */
function scriptBraceFollows(text: string, end: number): boolean {
    return (text[end] === "_" || text[end] === "^") && text[end + 1] === "{";
}

function matchPlainTexAtom(text: string, index: number): string | null {
    PLAIN_TEX_ATOM_AT.lastIndex = index;
    const match = PLAIN_TEX_ATOM_AT.exec(text);
    if (!match || scriptBraceFollows(text, index + match[0].length) || !plainTexAtomAccept(match[0])) return null;
    return match[0];
}

function plainMathLeftBoundary(text: string, index: number): boolean {
    if (index === 0) return true;
    const code = text.charCodeAt(index - 1);
    // ASCII is the common case: a formula does not start inside a word, a
    // number, a path (`\Users`), or a contraction (`it's_fine`).
    if (code < 0x80) {
        if ((code >= 0x41 && code <= 0x5a) || (code >= 0x61 && code <= 0x7a)) return false;
        if (code >= 0x30 && code <= 0x39) return false;
        return code !== 0x5f && code !== 0x5c && code !== 0x27;
    }
    // Chinese prose sits against a formula with no space ("令F^l").
    if (code >= 0x4e00 && code <= 0x9fff) return true;
    if (code === 0x2019) return false;
    const previous = text[index - 1];
    if (/\p{Script=Han}/u.test(previous)) return true;
    return !/[\p{L}\p{N}_\\]/u.test(previous);
}

function lineStartAt(text: string, index: number): boolean {
    return index === 0 || text[index - 1] === "\n";
}

/** A fenced code block still waiting, or already classified as code. Skip it. */
function readCodeFence(text: string, index: number): number {
    if (!lineStartAt(text, index)) return -1;
    let probe = index;
    while (text[probe] === " " || text[probe] === "\t") probe++;
    const opener = text[probe];
    if (opener !== "`" && opener !== "~") return -1;
    const lineEnd = text.indexOf("\n", index);
    const line = text.slice(index, lineEnd === -1 ? text.length : lineEnd);
    const open = line.match(FENCE_LINE);
    if (!open || open[3].includes("`") || open[3].includes("~")) return -1;
    const marker = open[2];
    let cursor = lineEnd === -1 ? text.length : lineEnd + 1;
    while (cursor < text.length) {
        const nextEnd = text.indexOf("\n", cursor);
        const row = text.slice(cursor, nextEnd === -1 ? text.length : nextEnd);
        const closer = row.match(FENCE_CLOSER);
        if (closer && closer[2][0] === marker[0] && closer[2].length >= marker.length) {
            return nextEnd === -1 ? text.length : nextEnd + 1;
        }
        if (nextEnd === -1) return text.length;
        cursor = nextEnd + 1;
    }
    return text.length;
}

function readInlineCodeSpan(text: string, index: number): number {
    if (text[index] !== "`") return -1;
    let run = 1;
    while (text[index + run] === "`") run++;
    const close = text.indexOf("`".repeat(run), index + run);
    if (close !== -1) return close + run;
    // A single unmatched backtick is literal. An unmatched `` only hides the
    // rest of its line; the next line is prose again.
    if (run === 1) return -1;
    const lineEnd = text.indexOf("\n", index);
    return lineEnd === -1 ? text.length : lineEnd;
}

function readUrl(text: string, index: number): number {
    if (text[index] !== "h" && text[index] !== "H") return -1;
    if (!/^https?:\/\//i.test(text.slice(index, index + 8))) return -1;
    let end = index + 8;
    while (end < text.length) {
        const code = text.charCodeAt(end);
        // A Chinese sentence often continues straight after the URL
        // (`https://example.com/a_b。令F^l`). The formula is not part of the link.
        if (code <= 0x20 || (code >= 0x3000 && code <= 0x303f) || (code >= 0x4e00 && code <= 0x9fff) || (code >= 0xff00 && code <= 0xffef)) break;
        end++;
    }
    return end;
}

/**
 * A finished Markdown link or image, so the destination is not scanned.
 * An inline link's label is scanned separately: `F^l_0` there becomes
 * `$F^l_0$`, and the renderer typesets that without leaving the target behind.
 * Image alts and reference labels stay source.
 */
function readMarkdownLinkSpan(text: string, index: number): number {
    let cursor = index;
    if (text[cursor] === "!" && text[cursor + 1] === "[") cursor++;
    if (text[cursor] !== "[") return -1;
    const labelEnd = text.indexOf("]", cursor + 1);
    if (labelEnd === -1 || labelEnd - cursor > 300) return -1;
    const after = text[labelEnd + 1];
    if (after === "(") {
        // The first `)` is not the end when the path itself has parentheses
        // (`/a_(b)/F^l_1`). Stopping there would wrap the tail and break the target.
        let depth = 1;
        const limit = Math.min(text.length, labelEnd + 2000);
        for (let i = labelEnd + 2; i < limit; i++) {
            const c = text[i];
            if (c === "\n") return -1;
            if (c === "(") depth++;
            else if (c === ")") {
                depth--;
                if (depth === 0) return i + 1;
            }
        }
        return -1;
    }
    if (after === "[") {
        const refEnd = text.indexOf("]", labelEnd + 2);
        if (refEnd === -1 || refEnd - labelEnd > 200) return -1;
        return refEnd + 1;
    }
    return -1;
}

/**
 * The `$$` glued to this atom closes an earlier `$$` on the same line.
 * A `$$` inside code or a link is not a delimiter, and a `$$` from a previous
 * line already ended. Pairing either one would turn `$$F^l_0` into `$$$F^l_0$`.
 */
function doubledDollarCloses(text: string, atomIndex: number): boolean {
    if (atomIndex < 2 || !text.startsWith("$$", atomIndex - 2) || isEscapedAt(text, atomIndex - 2)) return false;
    let line = atomIndex - 2;
    while (line > 0 && text[line - 1] !== "\n") line--;
    let open = false;
    for (let cursor = line; cursor < atomIndex - 2; ) {
        if (text[cursor] === "`") {
            const code = readInlineCodeSpan(text, cursor);
            if (code !== -1 && code <= atomIndex) {
                cursor = code;
                continue;
            }
        } else if (text[cursor] === "[" || (text[cursor] === "!" && text[cursor + 1] === "[")) {
            const link = readMarkdownLinkSpan(text, cursor);
            if (link !== -1 && link <= atomIndex) {
                cursor = link;
                continue;
            }
        }
        if (text.startsWith("$$", cursor) && !isEscapedAt(text, cursor)) {
            open = !open;
            cursor += 2;
            continue;
        }
        cursor++;
    }
    return open;
}

/** A `$` that would open inline math and has no closer yet. `$5` does not. */
function isUnclosedSingleDollar(text: string, index: number): boolean {
    if (text[index] !== "$" || text[index + 1] === "$" || isEscapedAt(text, index)) return false;
    const next = text[index + 1] || "";
    return !!next && !/\s/u.test(next) && !/\d/u.test(next) && plainMathLeftBoundary(text, index);
}

/**
 * True when the atom is the first token of a delimiter that has not closed
 * yet (`$$F^l_0`, `\(F^l_0`). Wrapping it would invent a second `$`.
 */
function followsUnclosedMathOpener(text: string, index: number): boolean {
    if (index <= 0) return false;
    const prev = text[index - 1];
    if (prev === "$" && index >= 2 && text[index - 2] === "$") return !doubledDollarCloses(text, index);
    return (prev === "(" || prev === "[") && index >= 2 && text[index - 2] === "\\" && !isEscapedAt(text, index - 2);
}

/** A script within a short base. Prose words never reach the atom regex. */
function hasScriptSoon(text: string, index: number): boolean {
    const end = Math.min(text.length, index + 24);
    for (let cursor = index + 1; cursor < end; cursor++) {
        const ch = text[cursor];
        const code = text.charCodeAt(cursor);
        if (code === 0x5e || code === 0x5f) return true;
        if (code <= 0x20) return false;
        // ASCII and Latin-1 below ² are never a unicode script.
        if (code < 0x00b2) continue;
        if (isUnicodeSubscript(ch) || isUnicodeSuperscript(ch)) return true;
    }
    return false;
}

/** True when `index` is the first non-space on its line. */
function isLineStartDelimiter(text: string, index: number): boolean {
    for (let cursor = index - 1; cursor >= 0 && text[cursor] !== "\n"; cursor--) {
        const code = text.charCodeAt(cursor);
        if (code !== 0x20 && code !== 0x09) return false;
    }
    return true;
}

/**
 * An existing `$...$` / `$$...$$` / `\(...\)` span. A currency dollar (`$5`)
 * is not a span. An unclosed `$$` owns the rest of the reply only when it
 * opens a display block (first token on the line); the painter keeps that
 * block as source until its closer arrives. A `$$` mid-sentence does not,
 * or a later formula stays raw text.
 */
function readExistingMath(text: string, index: number): number {
    if (isEscapedAt(text, index)) return -1;
    if (text.startsWith("$$", index) || text.startsWith("\\[", index)) {
        const delimiter = text.startsWith("$$", index) ? "$$" : "\\]";
        const close = findUnescaped(text, delimiter, index + 2);
        if (close !== -1) return close + delimiter.length;
        return isLineStartDelimiter(text, index) ? text.length : -1;
    }
    if (text.startsWith("\\(", index)) {
        const close = findUnescaped(text, "\\)", index + 2);
        return close === -1 ? -1 : close + 2;
    }
    if (text[index] !== "$" || text[index + 1] === "$") return -1;
    const next = text[index + 1] || "";
    if (!next || /\s/u.test(next) || !plainMathLeftBoundary(text, index)) return -1;
    // `$5` is a price; `$0.786\pm0.004$` is a formula that starts with a number.
    const digitLed = /\d/u.test(next);
    for (let close = index + 1; close < text.length; close++) {
        if (text[close] === "\n") return -1;
        if (
            text[close] === "$"
            && text[close + 1] !== "$"
            && !isEscapedAt(text, close)
            && !/\s/u.test(text[close - 1] || "")
            && plainMathRightBoundary(text, close)
        ) {
            if (digitLed && !digitLedSpanIsMath(text.slice(index + 1, close))) return -1;
            return close + 1;
        }
    }
    return -1;
}

/**
 * A digit-led `$...$` span is math when the inside carries TeX structure, or
 * is nothing but a number the author still wrapped (`$0.934$`). Mirrors the
 * renderer's rule so both sides agree on what is a formula.
 */
function digitLedSpanIsMath(inner: string): boolean {
    // A span holding another delimiter or Han prose is a currency pair that
    // ran into a later formula, not mathematics ("$5 到 $10，公式为 $x$").
    if (inner.includes("$") || /\p{Script=Han}/u.test(inner)) return false;
    return /[\\^_{}]/.test(inner) || /^\s*[-+]?\d[\d.,\s]*$/.test(inner);
}

function plainMathRightBoundary(text: string, index: number): boolean {
    const next = text[index + 1] || "";
    if (!next || /\p{Script=Han}/u.test(next)) return true;
    return !/[\p{L}\p{N}_]/u.test(next);
}

function findUnescaped(text: string, delimiter: string, from: number): number {
    let index = text.indexOf(delimiter, from);
    while (index !== -1 && isEscapedAt(text, index)) index = text.indexOf(delimiter, index + delimiter.length);
    return index;
}

/**
 * TeX names a reply may write without delimiters (`τ \approx 1`,
 * `0.786\pm0.004`). The list matters: only a name on it is promoted, so
 * `\new\file` in a path fragment and other stray backslashes stay text.
 */
const BARE_TEX_COMMANDS = new Set([
    "alpha", "beta", "gamma", "delta", "epsilon", "varepsilon", "zeta", "eta",
    "theta", "vartheta", "iota", "kappa", "lambda", "mu", "nu", "xi", "pi",
    "varpi", "rho", "varrho", "sigma", "varsigma", "tau", "upsilon", "phi",
    "varphi", "chi", "psi", "omega",
    "Gamma", "Delta", "Theta", "Lambda", "Xi", "Pi", "Sigma", "Upsilon", "Phi",
    "Psi", "Omega",
    "pm", "mp", "times", "div", "cdot", "cdots", "ast", "star", "circ", "bullet",
    "oplus", "ominus", "otimes", "oslash", "odot", "dagger", "ddagger", "amalg",
    "approx", "sim", "simeq", "cong", "equiv", "propto", "neq", "ne", "leq", "le",
    "geq", "ge", "ll", "gg", "prec", "succ", "preceq", "succeq", "subset",
    "supset", "subseteq", "supseteq", "sqsubseteq", "sqsupseteq", "in", "notin",
    "ni", "cup", "cap", "setminus", "smallsetminus", "mid", "nmid", "parallel",
    "nparallel", "models", "vdash", "dashv", "perp", "bot", "top", "asymp",
    "doteq", "to", "gets", "rightarrow", "leftarrow", "Rightarrow", "Leftarrow",
    "leftrightarrow", "Leftrightarrow", "mapsto", "longmapsto", "longrightarrow",
    "longleftarrow", "Longrightarrow", "Longleftarrow", "uparrow", "downarrow",
    "updownarrow", "nearrow", "searrow", "swarrow", "nwarrow", "hookrightarrow",
    "hookleftarrow", "rightharpoonup", "leftharpoonup", "rightleftharpoons",
    "partial", "nabla", "infty", "ell", "hbar", "hslash", "imath", "jmath", "Re",
    "Im", "wp", "aleph", "beth", "gimel", "prime", "backprime", "emptyset",
    "varnothing", "forall", "exists", "nexists", "neg", "lnot", "angle",
    "measuredangle", "triangle", "triangledown", "square", "blacksquare",
    "diamond", "lozenge", "surd", "checkmark", "sum", "prod", "coprod", "int",
    "iint", "iiint", "oint", "oiint", "bigcup", "bigcap", "bigsqcup", "bigoplus",
    "bigotimes", "bigodot", "biguplus", "bigvee", "bigwedge", "log", "lg", "ln",
    "exp", "sin", "cos", "tan", "cot", "sec", "csc", "arcsin", "arccos", "arctan",
    "sinh", "cosh", "tanh", "coth", "lim", "limsup", "liminf", "sup", "inf",
    "max", "min", "arg", "deg", "det", "dim", "gcd", "hom", "ker", "Pr", "bmod",
    "pmod", "mod", "sqrt", "frac", "dfrac", "tfrac", "cfrac", "binom", "dbinom",
    "tbinom", "hat", "widehat", "tilde", "widetilde", "bar", "overline",
    "underline", "vec", "overrightarrow", "overleftarrow", "overbrace",
    "underbrace", "dot", "ddot", "dddot", "breve", "check", "grave", "acute",
    "mathring", "not", "stackrel", "overset", "underset", "operatorname",
    "mathrm", "mathbf", "mathit", "mathsf", "mathtt", "mathcal", "mathbb",
    "mathfrak", "mathscr", "boldsymbol", "bm", "text", "textbf", "textit",
    "textrm", "textsf", "texttt", "boxed", "fbox", "phantom", "hphantom",
    "vphantom", "quad", "qquad", "thinspace", "medspace", "thickspace", "hspace",
    "mkern", "kern", "displaystyle", "textstyle", "scriptstyle", "limits",
    "nolimits", "left", "big", "Big", "bigg", "Bigg", "bigl", "bigr",
    "Bigl", "Bigr", "biggl", "biggr", "Biggl", "Biggr", "bigm", "Bigm", "biggm",
    "Biggm", "lfloor", "rfloor", "lceil", "rceil", "langle",
    "rangle", "lvert", "rvert", "lVert", "rVert", "vert", "Vert", "ldots",
    "dots", "dotsc", "dotsb", "dotsi", "vdots", "ddots", "begin",
    "implies", "iff", "impliedby", "colon", "smallint", "xrightarrow",
    "xleftarrow", "xRightarrow", "xLeftarrow", "substack", "mathop", "mathbin",
    "mathrel", "mathord", "mathopen", "mathclose", "mathpunct", "mathinner",
    "therefore", "because", "triangleq", "color",
]);

/**
 * Commands that only make sense with arguments: name → required `{...}`
 * group count. A lone `\frac` or a one-group `\frac{a}` is prose about TeX —
 * promoting it would paint a KaTeX error. `[...]` options are extra and do
 * not count: `\sqrt[3]{x}` needs the `{x}`.
 */
const TEX_ARG_COMMANDS = new Map([
    ["frac", 2], ["dfrac", 2], ["tfrac", 2], ["cfrac", 2], ["binom", 2],
    ["dbinom", 2], ["tbinom", 2], ["overset", 2], ["underset", 2],
    ["stackrel", 2],
    ["sqrt", 1], ["text", 1], ["textbf", 1], ["textit", 1], ["textrm", 1],
    ["textsf", 1], ["texttt", 1], ["mathrm", 1], ["mathbf", 1], ["mathit", 1],
    ["mathsf", 1], ["mathtt", 1], ["mathcal", 1], ["mathbb", 1], ["mathfrak", 1],
    ["mathscr", 1], ["boldsymbol", 1], ["bm", 1], ["operatorname", 1],
    ["hat", 1], ["widehat", 1], ["tilde", 1], ["widetilde", 1], ["bar", 1],
    ["overline", 1], ["underline", 1], ["vec", 1], ["overrightarrow", 1],
    ["overleftarrow", 1], ["overbrace", 1], ["underbrace", 1], ["dot", 1],
    ["ddot", 1], ["dddot", 1], ["breve", 1], ["check", 1], ["grave", 1],
    ["acute", 1], ["mathring", 1], ["boxed", 1], ["fbox", 1], ["phantom", 1],
    ["hphantom", 1], ["vphantom", 1], ["hspace", 1], ["mkern", 1], ["kern", 1],
    ["xrightarrow", 1], ["xleftarrow", 1], ["xRightarrow", 1],
    ["xLeftarrow", 1], ["substack", 1], ["mathop", 1], ["color", 1], ["not", 1],
]);

/** `\big(`-style sizers need exactly one delimiter token, or they error out. */
const TEX_DELIM_COMMANDS = new Set([
    "big", "Big", "bigg", "Bigg", "bigl", "bigr", "Bigl", "Bigr",
    "biggl", "biggr", "Biggl", "Biggr", "bigm", "Bigm", "biggm", "Biggm",
]);

/** Environments KaTeX typesets. `\begin{env}` only promotes with its `\end`. */
const MATH_ENVIRONMENTS = new Set([
    "aligned", "gathered", "cases", "dcases", "rcases", "matrix", "pmatrix",
    "bmatrix", "Bmatrix", "vmatrix", "Vmatrix", "smallmatrix", "array",
    "subarray", "darray", "alignedat", "matrix*", "pmatrix*", "bmatrix*",
    "Bmatrix*", "vmatrix*", "Vmatrix*",
]);

/**
 * Environments whose `\begin{env}` must be followed by a `{cols}`/`{n}`
 * argument — `\begin{array}` without one is a KaTeX error, not math.
 */
const TEX_ENV_ARG_REQUIRED = new Set(["array", "darray", "subarray", "alignedat"]);

/** End of a `_x` / `^{...}` script glued at `index`, or `index` when none. */
function scriptTailEnd(text: string, index: number, limit: number): number {
    const mark = text[index];
    if (mark !== "_" && mark !== "^") return index;
    const next = text[index + 1];
    if (next === "{") {
        let depth = 0;
        for (let cursor = index + 1; cursor < limit && cursor - index < 200; cursor++) {
            const ch = text[cursor];
            if (ch === "{") depth++;
            else if (ch === "}") {
                depth--;
                if (depth === 0) return cursor + 1;
            } else if (ch === "\n") return index;
        }
        return index;
    }
    return next && /[A-Za-z0-9*+-]/.test(next) ? index + 2 : index;
}

/** End of the balanced `{...}` opened at `index`, or `index` when unbalanced. */
function braceGroupEnd(text: string, index: number, limit: number): number {
    let depth = 0;
    for (let cursor = index; cursor < limit && cursor - index < 500; cursor++) {
        const ch = text[cursor];
        if (ch === "{") depth++;
        else if (ch === "}") {
            depth--;
            if (depth === 0) return cursor + 1;
        } else if (ch === "\n") return index;
    }
    return index;
}

/** One `_x` and one `^x` in either order (`\sigma_i^2`, `\lambda^2_i`). */
function scriptTailsEnd(text: string, index: number, limit: number): number {
    let sub = false;
    let sup = false;
    let end = index;
    while (end < limit) {
        const mark = text[end];
        if (mark !== "_" && mark !== "^") break;
        if ((mark === "_" && sub) || (mark === "^" && sup)) break;
        const next = scriptTailEnd(text, end, limit);
        if (next === end) break;
        if (mark === "_") sub = true;
        else sup = true;
        end = next;
    }
    return end;
}

/** Commands valid as a `\left` / `\right` / `\big` delimiter. */
const TEX_DELIM_NAMES = new Set([
    "vert", "Vert", "lvert", "rvert", "lVert", "rVert", "langle", "rangle",
    "lfloor", "rfloor", "lceil", "rceil", "lbrace", "rbrace", "backslash",
    "uparrow", "downarrow",
    "Uparrow", "Downarrow", "updownarrow", "Updownarrow", "lmoustache",
    "rmoustache", "lgroup", "rgroup", "bracevert", "arrowvert", "Arrowvert",
]);
// `{` and `}` are group tokens, not delimiters — `\left{` errors; `\left\{` is the form.
const TEX_DELIM_CHARS = new Set(["(", ")", "[", "]", "|", ".", "<", ">", "/"]);

/** One delimiter token after `\left` / `\big`: `(`, `\vert`, `\{`, `.` … */
function delimTokenEnd(text: string, index: number, limit: number): number {
    if (index >= limit) return index;
    if (text[index] === "\\") {
        const next = text[index + 1] || "";
        if (!next || index + 1 >= limit) return index;
        if (/[A-Za-z]/.test(next)) {
            let end = index + 2;
            while (end < limit && /[A-Za-z]/.test(text[end])) end++;
            return TEX_DELIM_NAMES.has(text.slice(index + 1, end)) ? end : index;
        }
        return Math.min(index + 2, limit);
    }
    return TEX_DELIM_CHARS.has(text[index]) ? index + 1 : index;
}

/**
 * End of `...\right<delim>` searched from `from`, or -1. `\rightarrow` is
 * not a closer, nested `\left` openers must close before ours does
 * (`\left(a \left(b \right) c \right)` pairs with the second `\right`), and
 * a `\begin` / `\end` or a delimiter-less token inside means the span is
 * not math.
 */
function rightDelimitedEnd(text: string, from: number, limit: number): number {
    let search = from;
    let depth = 0;
    while (search < limit) {
        const at = text.indexOf("\\", search);
        if (at === -1 || at >= limit) return -1;
        if (isEscapedAt(text, at)) {
            search = at + 1;
            continue;
        }
        const word = /^\\(left|right|middle|begin|end)/.exec(text.slice(at, at + 8));
        if (!word || /[A-Za-z]/.test(text[at + word[0].length] || "")) {
            search = at + 1;
            continue;
        }
        const name = word[1];
        // An environment cannot sit inside a `\left..\right` pair.
        if (name === "begin" || name === "end") return -1;
        const delimStart = skipInlineSpaces(text, at + word[0].length, limit);
        const delimEnd = delimTokenEnd(text, delimStart, limit);
        // A `\left` / `\right` / `\middle` without a delimiter is invalid TeX.
        if (delimEnd === delimStart) return -1;
        if (name === "left") {
            depth++;
            search = delimEnd;
            continue;
        }
        if (name === "middle") {
            search = delimEnd;
            continue;
        }
        // The promoted span is wrapped in `$...$`, which cannot cross a line.
        if (text.slice(from, at).includes("\n")) return -1;
        if (depth > 0) {
            depth--;
            search = delimEnd;
            continue;
        }
        return delimEnd;
    }
    return -1;
}

/**
 * Parse `\begin` / `\end` + optional spaces + `{name}` at `at`. Returns the
 * trimmed name and the index just past `}`, or null when the token is a
 * different command (`\endnote`) or has no name group.
 */
function envTokenAt(text: string, at: number, limit: number): { name: string; end: number } | null {
    const isBegin = text.startsWith("\\begin", at);
    const wordEnd = at + (isBegin ? 6 : 4);
    if (!isBegin && !text.startsWith("\\end", at)) return null;
    const after = text[wordEnd] || "";
    if (/[A-Za-z]/.test(after)) return null;
    const open = skipInlineSpaces(text, wordEnd, limit);
    if (text[open] !== "{") return null;
    const end = braceGroupEnd(text, open, limit);
    if (end === open) return null;
    return { name: text.slice(open + 1, end - 1).trim(), end };
}

/**
 * End of `\end{env}` — tolerating `\end { env }` whitespace — searched from
 * `from`, or -1. An escaped `\\end` is not a closer. Nested environments of
 * any name must balance (`\begin{matrix}` inside `aligned` is legal, but a
 * stray `\end{matrix}` or a mismatched closer means the span is not math),
 * and `\left` / `\right` / `\middle` inside the body must carry delimiters
 * and balance before the closer.
 */
function envCloseEnd(text: string, env: string, from: number, limit: number): number {
    let search = from;
    // Each open environment remembers the delimiter depth it started at:
    // a `\left..\right` pair cannot cross a `\begin` / `\end` boundary.
    const stack: { name: string; delimiters: number }[] = [];
    let delimiters = 0;
    while (search < limit) {
        const at = text.indexOf("\\", search);
        if (at === -1 || at >= limit) return -1;
        if (isEscapedAt(text, at)) {
            search = at + 1;
            continue;
        }
        const word = /^\\(begin|end|left|right|middle)/.exec(text.slice(at, at + 8));
        if (word && !/[A-Za-z]/.test(text[at + word[0].length] || "")) {
            const name = word[1];
            if (name === "begin" || name === "end") {
                const token = envTokenAt(text, at, limit);
                // A `\begin` / `\end` word without a `{name}` group is a parse
                // error waiting to happen — the whole span stays prose.
                if (!token) return -1;
                search = token.end;
                if (name === "begin") {
                    // An environment KaTeX cannot typeset fails the whole span.
                    if (!MATH_ENVIRONMENTS.has(token.name)) return -1;
                    stack.push({ name: token.name, delimiters });
                    continue;
                }
                if (stack.length === 0) {
                    // `env` opened before `from`, so the stack is empty here
                    // only for our own closer — anything else is a stray `\end`.
                    return token.name === env && delimiters === 0 ? token.end : -1;
                }
                const top = stack[stack.length - 1];
                // The env closes only if the pairs it opened inside did too.
                if (top.name !== token.name || top.delimiters !== delimiters) return -1;
                stack.pop();
                continue;
            }
            // `\left` / `\right` / `\middle` each need a delimiter token.
            const delimStart = skipInlineSpaces(text, at + word[0].length, limit);
            const delimEnd = delimTokenEnd(text, delimStart, limit);
            if (delimEnd === delimStart) return -1;
            search = delimEnd;
            if (name === "left") delimiters++;
            else if (name === "right") {
                if (delimiters === 0) return -1;
                delimiters--;
            } else if (delimiters === 0) return -1;
            continue;
        }
        search = at + 1;
    }
    return -1;
}

/**
 * A run of TeX commands written straight into the sentence, without `$`
 * (`τ \approx 1`, `0.786\pm0.004`, `\frac{a}{b}`). Every command name must sit
 * in BARE_TEX_COMMANDS; a script (`\lambda_i`) travels with its command.
 * Commands that cannot stand alone only promote with their payload:
 * `\frac{a}{b}` needs its groups, `\big(` its delimiter, `\left...\right` the
 * pair, and `\begin{env}...\end{env}` the whole environment.
 *
 * The left boundary rejects a letter (`foo\bar`), a colon (`C:\docs`), an
 * underscore, a quote and a backslash — but not a digit, so `0.786\pm0.004`
 * still typesets. Han prose counts as a boundary, matching the atom rules.
 */
/** Spaces and tabs TeX tolerates between a command and its argument. */
function skipInlineSpaces(text: string, index: number, limit: number): number {
    while (index < limit && (text[index] === " " || text[index] === "\t")) index++;
    return index;
}

function matchBareTexCommands(text: string, index: number, limit: number): { end: number; tex: string } | null {
    if (text[index] !== "\\" || isEscapedAt(text, index)) return null;
    const prev = text[index - 1] || "";
    if (prev && !/\d/u.test(prev) && (
        prev === "_" || prev === ":" || prev === "'" || prev === "\\" || prev === "~"
        || (/\p{L}/u.test(prev) && !/\p{Script=Han}/u.test(prev))
    )) return null;
    let cursor = index;
    let matched = false;
    while (cursor < limit && text[cursor] === "\\") {
        let nameEnd = cursor + 1;
        while (nameEnd < limit) {
            const code = text.charCodeAt(nameEnd);
            if (!((code >= 0x41 && code <= 0x5a) || (code >= 0x61 && code <= 0x7a))) break;
            nameEnd++;
        }
        const name = text.slice(cursor + 1, nameEnd);
        if (!BARE_TEX_COMMANDS.has(name)) break;
        if (name === "begin") {
            const envOpen = skipInlineSpaces(text, nameEnd, limit);
            const envEnd = text[envOpen] === "{" ? braceGroupEnd(text, envOpen, limit) : nameEnd;
            if (envEnd === nameEnd) break;
            const env = text.slice(envOpen + 1, envEnd - 1).trim();
            if (!MATH_ENVIRONMENTS.has(env)) break;
            let bodyStart = envEnd;
            if (TEX_ENV_ARG_REQUIRED.has(env)) {
                const argOpen = skipInlineSpaces(text, envEnd, limit);
                const argEnd = text[argOpen] === "{" ? braceGroupEnd(text, argOpen, limit) : envEnd;
                if (argEnd === envEnd) break;
                bodyStart = argEnd;
            }
            const closeEnd = envCloseEnd(text, env, bodyStart, limit);
            if (
                closeEnd === -1
                || closeEnd - envEnd > 4000
                // `$...$` cannot cross a line, so only a one-line env promotes.
                || text.slice(envEnd, closeEnd).includes("\n")
            ) break;
            matched = true;
            cursor = scriptTailsEnd(text, closeEnd, limit);
            continue;
        }
        if (name === "left") {
            const delimStart = skipInlineSpaces(text, nameEnd, limit);
            const delimEnd = delimTokenEnd(text, delimStart, limit);
            if (delimEnd === delimStart) break;
            const end = rightDelimitedEnd(text, delimEnd, limit);
            if (end === -1) break;
            matched = true;
            cursor = scriptTailsEnd(text, end, limit);
            continue;
        }
        if (TEX_DELIM_COMMANDS.has(name)) {
            const delimStart = skipInlineSpaces(text, nameEnd, limit);
            const end = delimTokenEnd(text, delimStart, limit);
            if (end === delimStart) break;
            matched = true;
            cursor = scriptTailsEnd(text, end, limit);
            continue;
        }
        let cmdEnd = nameEnd;
        const required = TEX_ARG_COMMANDS.get(name);
        if (required !== undefined) {
            let braces = 0;
            while (cmdEnd < limit) {
                const open = skipInlineSpaces(text, cmdEnd, limit);
                const mark = text[open];
                if (mark === "{") {
                    const end = braceGroupEnd(text, open, limit);
                    if (end === open) break;
                    cmdEnd = end;
                    braces++;
                    continue;
                }
                if (mark === "[") {
                    const close = text.indexOf("]", open + 1);
                    if (close === -1 || close >= limit || close - open > 200 || text.slice(open, close).includes("\n")) break;
                    cmdEnd = close + 1;
                    continue;
                }
                break;
            }
            if (braces < required) break;
        }
        matched = true;
        cursor = scriptTailsEnd(text, cmdEnd, limit);
    }
    if (!matched) return null;
    const tex = text.slice(index, cursor);
    // A `$` inside arguments or a pair body is an inner formula. Wrapping it
    // would nest delimiters; the inner `$...$` renders better left alone.
    if (tex.includes("$")) return null;
    return { end: cursor, tex };
}

function isEscapedAt(text: string, index: number): boolean {
    let slashCount = 0;
    for (let cursor = index - 1; cursor >= 0 && text[cursor] === "\\"; cursor--) slashCount++;
    return slashCount % 2 === 1;
}

/**
 * Wrap bare TeX atoms and known bare TeX commands in `$...$`. Regions that
 * are already math, code, a URL, or a Markdown destination are copied through.
 * An inline link label is scanned. Text with neither `^`, `_`, `\`, a Greek
 * letter, nor a unicode script is returned as-is.
 */
function promoteCompanionPlainMath(text: string): string {
    if (!text.includes("^") && !text.includes("_") && !text.includes("\\") && !textMayContainUnicodeMath(text)) return text;
    const braces = text.includes("{");
    const parts: string[] = [];
    let cursor = 0;
    let index = 0;
    let changed = false;
    // An unclosed `$` owns the rest of its line. A `$` inside code or a link
    // does not: the walker never visits those characters.
    let dollarOpen = false;
    // Depth of a `^{` / `_{` that has not closed. Kept while walking so a
    // candidate does not rescan the line (`l_{0,c}` inside `F^{l_{0,c}`).
    let scriptDepth = 0;
    // While scanning an inline link label, spans must not run through the
    // closing `]`. The destination is copied as-is once the label ends.
    let labelStop = -1;
    let labelLinkEnd = -1;
    const bounded = (end: number) => (labelStop !== -1 && end > labelStop ? labelStop : end);
    const crossLine = (end: number) => {
        if (dollarOpen || scriptDepth > 0) {
            const nl = text.indexOf("\n", index);
            if (nl !== -1 && nl < end) {
                dollarOpen = false;
                scriptDepth = 0;
            }
        }
    };
    const take = (end: number) => {
        crossLine(end);
        if (end > cursor) parts.push(text.slice(cursor, end));
        cursor = end;
        index = end;
    };
    const step = () => {
        const ch = text[index];
        if (ch === "\n") {
            dollarOpen = false;
            scriptDepth = 0;
        } else if (braces) {
            if (ch === "{") {
                const prev = text[index - 1];
                if (scriptDepth > 0 || prev === "^" || prev === "_") scriptDepth++;
            } else if (ch === "}" && scriptDepth > 0) scriptDepth--;
        }
        index++;
    };
    while (index < text.length) {
        if (labelStop !== -1 && index >= labelStop) {
            const end = labelLinkEnd;
            labelStop = -1;
            labelLinkEnd = -1;
            // An unclosed `$` or `^{` inside the label does not own the
            // destination, or the following formula.
            dollarOpen = false;
            scriptDepth = 0;
            take(end);
            continue;
        }
        if (lineStartAt(text, index)) {
            dollarOpen = false;
            scriptDepth = 0;
            const fence = readCodeFence(text, index);
            if (fence !== -1) {
                take(bounded(fence));
                continue;
            }
        }
        const ch = text.charCodeAt(index);
        if (ch === 0x60) {
            const code = readInlineCodeSpan(text, index);
            if (code !== -1) {
                take(bounded(code));
                continue;
            }
        } else if (ch === 0x24 || ch === 0x5c) {
            const math = readExistingMath(text, index);
            if (math !== -1) {
                take(bounded(math));
                continue;
            }
            if (ch === 0x24 && isUnclosedSingleDollar(text, index)) dollarOpen = true;
            // A bare TeX command in prose (`τ \approx 1`) is promoted like an
            // atom. Inside an unclosed `$` / `\(` span it is already math.
            if (ch === 0x5c && scriptDepth === 0 && !dollarOpen && !followsUnclosedMathOpener(text, index)) {
                const limit = labelStop === -1 ? text.length : labelStop;
                const run = matchBareTexCommands(text, index, limit);
                if (run) {
                    let start = index;
                    let end = run.end;
                    // `0.786\pm0.004` is one formula. Wrapping only `\pm`
                    // (`0.786$\pm$0.004`) puts `$` right after a digit, which
                    // the renderer treats as currency and leaves literal —
                    // the number travels inside the span instead.
                    if (/\d/u.test(text[index - 1] || "")) {
                        while (start > 0) {
                            const ch2 = text[start - 1];
                            if (/\d/u.test(ch2) || ((ch2 === "." || ch2 === ",") && /\d/u.test(text[start - 2] || ""))) start--;
                            else break;
                        }
                        while (end < limit) {
                            const ch2 = text[end];
                            if (/\d/u.test(ch2) || ((ch2 === "." || ch2 === ",") && /\d/u.test(text[end + 1] || ""))) end++;
                            else break;
                        }
                    }
                    if (start > cursor) parts.push(text.slice(cursor, start));
                    parts.push("$", text.slice(start, end), "$");
                    index = end;
                    cursor = index;
                    changed = true;
                    continue;
                }
            }
        } else if (ch === 0x68 || ch === 0x48) {
            const url = readUrl(text, index);
            if (url !== -1) {
                take(bounded(url));
                continue;
            }
        } else if (labelStop === -1 && (ch === 0x5b || (ch === 0x21 && text[index + 1] === "["))) {
            const link = readMarkdownLinkSpan(text, index);
            if (link !== -1) {
                const image = text[index] === "!";
                const open = image ? index + 1 : index;
                const labelEnd = text.indexOf("]", open + 1);
                // `[![alt](img)](page)` closes its first `]` inside the image.
                // Scanning that fragment would rewrite the image syntax.
                if (!image && text[open + 1] !== "!" && labelEnd !== -1 && text[labelEnd + 1] === "(" && labelEnd < link) {
                    take(open + 1);
                    labelStop = labelEnd;
                    labelLinkEnd = link;
                    continue;
                }
                take(link);
                continue;
            }
        }
        if (scriptDepth === 0 && canStartPlainTexAtom(text, index) && plainMathLeftBoundary(text, index)) {
            const promoted = matchPromotedAtom(text, index, labelStop === -1 ? text.length : labelStop);
            if (promoted) {
                // An opener that has not closed owns this whole atom. Skipping
                // only the base would typeset the superscript on its own
                // (`$F^$l_0$`). A `$` earlier on the line does the same to a
                // later atom (`$F^l_0 and L_ce`).
                if (dollarOpen || followsUnclosedMathOpener(text, index)) {
                    crossLine(promoted.end);
                    index = promoted.end;
                    continue;
                }
                if (index > cursor) parts.push(text.slice(cursor, index));
                parts.push("$", promoted.tex, "$");
                index = promoted.end;
                cursor = index;
                changed = true;
                continue;
            }
        }
        step();
    }
    if (!changed) return text;
    if (cursor < text.length) parts.push(text.slice(cursor));
    return parts.join("");
}

export function companionCodeMathToTex(text: string): string {
    if (!text) return text;
    // Fences are matched per line. A trailing `\r` makes the line fail that
    // match, and a formula inside the block is then wrapped as math.
    const normalized = text.indexOf("\r") === -1 ? text : text.replace(/\r\n?/g, "\n");
    const rewritten = normalized.includes("`") || normalized.includes("~~~") ? rewriteCompanionCodeMath(normalized) : normalized;
    return promoteCompanionPlainMath(rewritten);
}

function rewriteCompanionCodeMath(text: string): string {
    const lines = text.split("\n");
    const out: string[] = [];
    let index = 0;
    while (index < lines.length) {
        const open = lines[index].match(FENCE_LINE);
        // A fence run that reappears later on the same line is a same-line
        // inline span (`` ```x``` ``), not a block opener.
        if (!open || open[3].includes("`") || open[3].includes("~")) {
            out.push(inlineCodeMath(lines[index]));
            index++;
            continue;
        }
        const marker = open[2];
        const info = open[3].trim();
        const body: string[] = [];
        let close = -1;
        for (let cursor = index + 1; cursor < lines.length; cursor++) {
            const closer = lines[cursor].match(FENCE_CLOSER);
            if (closer && closer[2][0] === marker[0] && closer[2].length >= marker.length) {
                close = cursor;
                break;
            }
            body.push(lines[cursor]);
        }
        if (close === -1) {
            // An unfinished (streaming) fence stays source until its closer
            // arrives; the renderer already shows it as an open code block.
            for (let cursor = index; cursor < lines.length; cursor++) out.push(lines[cursor]);
            break;
        }
        const bodyText = body.join("\n");
        // An explicit ```math fence is an instruction, so it converts without
        // the heuristic. A document-level source fence stays code even when
        // labelled `latex`, and so does everything else that is not math.
        const explicitMath = MATH_FENCE_INFO.test(info)
            && bodyText.length <= 4000
            && !LATEX_SOURCE_SIGNAL.test(bodyText);
        // A body that already carries `$$`/`\[` delimiters is unfenced as is;
        // wrapping it again would nest the delimiters.
        const alreadyDelimited = explicitMath && isDelimitedMath(bodyText.trim());
        const mathFence = alreadyDelimited
            || (explicitMath && !bodyText.includes("$"))
            || (!info
                && bodyText.length <= 4000
                && !LATEX_SOURCE_SIGNAL.test(bodyText)
                && looksLikeCompanionMath(bodyText, true));
        if (mathFence) {
            if (!alreadyDelimited) out.push("$$");
            for (const line of body) out.push(line);
            if (!alreadyDelimited) out.push("$$");
        } else {
            for (let cursor = index; cursor <= close; cursor++) out.push(lines[cursor]);
        }
        index = close + 1;
    }
    return out.join("\n");
}
