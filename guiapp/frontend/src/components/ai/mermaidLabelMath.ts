import katex from "katex";
import "katex/dist/katex.min.css";

/**
 * Typeset formulas inside Mermaid node labels.
 *
 * Mermaid only recognises `$$...$$`, and it draws that as display math. The
 * diagrams models actually write put the formula in the label as plain text:
 * `L_CE`, `p_safe ≥ t*`, `z(x) ∈ R^512`, `τ=0.07`. Those characters survive
 * into the box unchanged, which is the unrendered formula.
 *
 * Short bases (one or two letters) become subscripts. A greek base keeps its
 * subscript (`τ_g`). A longer base such as `user_id` stays a name. A slash is
 * a prose separator (`CE / τ`), not a join. An arrow joins a formula
 * (`λ→0`, `x → τ_g(x)`) and leaves `W → softmax` as text. `click` lines and `http://` targets
 * stay literal. The KaTeX markup uses single quotes because the label itself
 * is wrapped in double quotes; a `"` inside it would end the node.
 */

const GREEK: Record<string, string> = {
    α: "\\alpha",
    β: "\\beta",
    γ: "\\gamma",
    δ: "\\delta",
    ε: "\\epsilon",
    ζ: "\\zeta",
    η: "\\eta",
    θ: "\\theta",
    λ: "\\lambda",
    μ: "\\mu",
    ν: "\\nu",
    π: "\\pi",
    ρ: "\\rho",
    σ: "\\sigma",
    τ: "\\tau",
    φ: "\\phi",
    ψ: "\\psi",
    ω: "\\omega",
    Δ: "\\Delta",
    Σ: "\\Sigma",
    Ω: "\\Omega",
    ℓ: "\\ell",
};

const OP_LATEX: Record<string, string> = {
    "·": "\\cdot",
    "⋅": "\\cdot",
    "×": "\\times",
    "≥": "\\ge",
    ">=": "\\ge",
    "≤": "\\le",
    "<=": "\\le",
    "≠": "\\neq",
    "!=": "\\neq",
    "≈": "\\approx",
    "∈": "\\in",
    "→": "\\to",
};

const EXPLICIT_MATH = /\$\$([\s\S]+?)\$\$|\\\[([\s\S]+?)\\\]|\\\(([\s\S]+?)\\\)|(?<!\$)\$(?!\$)([^$\n]+?)\$(?!\$)/g;
const OP_RE = /^(\s*)(>=|<=|!=|[<>→]|[=+\-·⋅×≥≤≠≈∈/,])(\s*)/;
const RELATION = new Set(["=", ">=", "<=", "!=", "<", ">", "≥", "≤", "≠", "≈", "∈"]);
// A clause may open on these. Bare `=` / `<` / `>` stay out so `W → softmax` and tags do.
const LEAD_RE = /^(>=|<=|!=|≥|≤|≠|≈|∈)(\s*)/;
// A product or sum may attach a coefficient to a real formula (`0.5·L_CE`).
// Slash and comma separate prose (`focal CE / τ`) and must not.
const JOIN = new Set(["+", "-", "·", "⋅", "×"]);
const URL_TOKEN = /[a-z][a-z0-9+.-]*:\/\/[^\s<>'"]+/gi;
const MATH_HINT = /[=_^*≥≤≠≈∈·⋅×α-ωΔΣΩℓ$\\−]/;

type Atom = { end: number; latex: string; marker: boolean };

function isBoundary(text: string, index: number): boolean {
    if (index === 0) return true;
    return !/[A-Za-z0-9_]/.test(text.charAt(index - 1));
}

function subscriptLatex(base: string, underscored: string): string {
    let latex = base;
    for (const part of underscored.slice(1).split("_")) {
        if (part) latex += `_{${part}}`;
    }
    return latex;
}

/** `(`…`)` or `{`…`}` starting at `openAt`, bounded so a label cannot run away. */
function readBalanced(text: string, openAt: number, open: string, close: string, max: number): { inner: string; end: number } | null {
    if (text.charAt(openAt) !== open) return null;
    let depth = 0;
    const limit = Math.min(text.length, openAt + max);
    for (let i = openAt; i < limit; i += 1) {
        const ch = text.charAt(i);
        if (ch === open) depth += 1;
        else if (ch === close) {
            depth -= 1;
            if (depth === 0) return { inner: text.slice(openAt + 1, i), end: i + 1 };
        }
    }
    return null;
}

/** Replace marked atoms inside an argument list. Unmarked letters stay as written. */
function atomsToLatex(text: string): string {
    let out = "";
    let index = 0;
    while (index < text.length) {
        const atom = readAtom(text, index);
        if (atom?.marker && atom.end > index) {
            out += atom.latex;
            index = atom.end;
            continue;
        }
        out += text.charAt(index);
        index += 1;
    }
    return out;
}

function readAtom(text: string, index: number): Atom | null {
    if (!isBoundary(text, index)) return null;
    const rest = text.slice(index);

    const sub = /^([A-Za-z]{1,2})((?:_[A-Za-z0-9]+)+)(?:\^([0-9A-Za-z]+))?(\*)?/.exec(rest);
    if (sub) {
        let latex = subscriptLatex(sub[1], sub[2]);
        if (sub[3]) latex += `^{${sub[3]}}`;
        if (sub[4]) latex += "^{*}";
        return { end: index + sub[0].length, latex, marker: true };
    }

    // `τ_g(x)` is one atom. Consuming the greek letter alone would leave `_g`.
    const greek = GREEK[text.charAt(index)];
    if (greek) {
        let end = index + 1;
        let latex = greek;
        const tail = /^((?:_[A-Za-z0-9]+)+)?(?:\^([0-9A-Za-z]+))?(\*)?/.exec(text.slice(end));
        if (tail && tail[0]) {
            if (tail[1]) latex = subscriptLatex(latex, tail[1]);
            if (tail[2]) latex += `^{${tail[2]}}`;
            if (tail[3]) latex += "^{*}";
            end += tail[0].length;
        }
        const metric = /^([A-Za-z]\d+)(?![A-Za-z0-9_])/.exec(text.slice(end));
        if (metric) {
            latex += `\\mathrm{${metric[1]}}`;
            end += metric[0].length;
        }
        if (text.charAt(end) === "(") {
            const args = readBalanced(text, end, "(", ")", 48);
            if (args && !args.inner.includes("(")) {
                latex += `(${atomsToLatex(args.inner)})`;
                end = args.end;
            }
        }
        return { end, latex, marker: true };
    }

    const callName = /^([A-Za-z]{1,2})\(/.exec(rest);
    if (callName) {
        const args = readBalanced(text, index + callName[1].length, "(", ")", 48);
        if (args) {
            // Nested calls (`d(z(x), z(τ_g x))`) stay one atom when the inside is math.
            const marker = /[_^α-ωΔΣΩ]/.test(args.inner);
            return { end: args.end, latex: `${callName[1]}(${atomsToLatex(args.inner)})`, marker };
        }
    }

    const sup = /^([A-Za-z])\^([0-9A-Za-z]+)/.exec(rest);
    if (sup) return { end: index + sup[0].length, latex: `${sup[1]}^{${sup[2]}}`, marker: true };

    const star = /^([A-Za-z])\*/.exec(rest);
    if (star) return { end: index + star[0].length, latex: `${star[1]}^{*}`, marker: true };

    // U+2212 in `−0.034`. The dot lookahead keeps `Gemma-2-9B` a version, not a power.
    const signed = /^(?:−|-)(\d+\.\d+|\.\d+|\d+)(?![A-Za-z.])/.exec(rest);
    if (signed) return { end: index + signed[0].length, latex: `-${signed[1]}`, marker: true };

    const num = /^(\d+\.\d+|\.\d+|\d+)(?:\^([0-9A-Za-z]+))?(?![A-Za-z])/.exec(rest);
    if (num) {
        const latex = num[2] ? `${num[1]}^{${num[2]}}` : num[1];
        return { end: index + num[0].length, latex, marker: Boolean(num[2]) };
    }

    const ident = /^([A-Za-z]{1,2})(?![A-Za-z0-9_])/.exec(rest);
    if (ident) return { end: index + ident[0].length, latex: ident[1], marker: false };

    return null;
}

/** `{safe,JB,INJ}` after a relation. Other braces stay text. */
function readSet(text: string, index: number): Atom | null {
    const group = readBalanced(text, index, "{", "}", 40);
    if (!group || !/^[A-Za-z0-9,\s]+$/.test(group.inner)) return null;
    const parts = group.inner.split(",").map((part) => part.trim()).filter(Boolean);
    if (parts.length === 0 || parts.some((part) => part.length > 12)) return null;
    const latex = `\\lbrace ${parts.map((part) => `\\mathrm{${part}}`).join(", ")} \\rbrace`;
    return { end: group.end, latex, marker: true };
}

/** A multi-letter operand such as `InfoNCE`. Upright, so it does not become `I n f o`. */
function readName(text: string, index: number): { end: number; latex: string } | null {
    if (!isBoundary(text, index)) return null;
    const match = /^[A-Za-z][A-Za-z0-9]{2,}(?![A-Za-z0-9_])/.exec(text.slice(index));
    if (!match) return null;
    return { end: index + match[0].length, latex: `\\mathrm{${match[0]}}` };
}

function readFormula(text: string, index: number): { end: number; latex: string } | null {
    const first = readAtom(text, index);
    let end: number;
    let latex: string;
    // An unmarked word may open a formula only through a relation (`m=0.1`)
    // or by joining a marked atom (`0.5·L_CE`). A clause may also open on `≥ m`.
    // Once open, later + and · stay in.
    let opened: boolean;
    if (first) {
        end = first.end;
        latex = first.latex;
        opened = first.marker;
    } else {
        if (!isBoundary(text, index)) return null;
        const lead = LEAD_RE.exec(text.slice(index));
        if (!lead) return null;
        const rhs = readAtom(text, index + lead[0].length) ?? readSet(text, index + lead[0].length);
        if (!rhs) return null;
        if (!rhs.marker && !/^[0-9-]/.test(rhs.latex) && rhs.latex.length > 2) return null;
        end = rhs.end;
        latex = `${OP_LATEX[lead[1]] ?? lead[1]} ${rhs.latex}`;
        opened = true;
    }
    while (end < text.length) {
        const op = OP_RE.exec(text.slice(end));
        if (!op) break;
        const kind = op[2];
        let pos = end + op[0].length;
        let sign = "";
        if (text.charAt(pos) === "+" || text.charAt(pos) === "-") {
            let look = pos + 1;
            while (text.charAt(look) === " ") look += 1;
            if (readAtom(text, look) || readName(text, look)) {
                sign = text.charAt(pos);
                pos = look;
            }
        }
        const atom = readAtom(text, pos) ?? readSet(text, pos);
        // `0.5·InfoNCE` is one term. `in-batch` is a hyphenated word: a minus
        // must not open a formula onto the following name.
        const nameOp = opened
            ? JOIN.has(kind) || RELATION.has(kind)
            : kind === "·" || kind === "⋅" || kind === "×" || kind === "+";
        const named = atom || !nameOp ? null : readName(text, pos);
        const next = atom ?? named;
        if (!next) break;
        // `→` pulls a marked right-hand side (`x → τ_g(x)`). An open formula
        // also keeps a following atom (`λ→0`). `W → softmax` and `A → B` stay text.
        const mapsTo = kind === "→" && Boolean(atom?.marker);
        if (!opened && !RELATION.has(kind) && !(JOIN.has(kind) && (atom?.marker || named)) && !mapsTo) break;
        const symbol = OP_LATEX[kind] ?? kind;
        latex += ` ${symbol} ${sign}${next.latex}`;
        opened = true;
        end = next.end;
    }
    if (!opened) return null;
    return { end, latex };
}

function escapeAttr(value: string): string {
    return value
        .replace(/&/g, "&amp;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;");
}

function katexSpan(latex: string, plain?: string): string | null {
    const trimmed = latex.trim();
    if (!trimmed) return null;
    try {
        const html = katex.renderToString(trimmed, {
            displayMode: false,
            throwOnError: true,
            strict: "ignore",
            trust: false,
            output: "htmlAndMathml",
        });
        // data-plain is the label text the PNG/PDF path prints. KaTeX itself is
        // MathML plus a visible span, and stripping both tags would print the
        // formula twice. Do not set nowrap on this root: each `.base` is already
        // an inline-block, and Mermaid wraps a label at 200px by switching the
        // parent to break-spaces. A nowrap root would glue those bases into one
        // line and the foreignObject would clip it. The quoted label ends at `"`.
        const stamp = plain
            ? `<span class="katex" data-plain="${escapeAttr(plain)}"`
            : `<span class="katex"`;
        return html
            .replace('<span class="katex"', stamp)
            .replace(/"/g, "'")
            .replace(/\s*\n\s*/g, "");
    } catch {
        return null;
    }
}

function typesetHeuristic(text: string): string {
    let out = "";
    let index = 0;
    while (index < text.length) {
        const span = readFormula(text, index);
        if (!span || span.end <= index) {
            out += text.charAt(index);
            index += 1;
            continue;
        }
        const raw = text.slice(index, span.end);
        out += katexSpan(span.latex, raw) ?? raw;
        index = span.end;
    }
    return out;
}

function alreadyTypeset(text: string): boolean {
    // The quote has to close the class. `katex-mathml` is not a root span.
    return /class=['"]katex['"]/.test(text);
}

function typesetPlain(text: string): string {
    if (!text || alreadyTypeset(text)) return text;
    if (!MATH_HINT.test(text)) return text;
    EXPLICIT_MATH.lastIndex = 0;
    let out = "";
    let last = 0;
    for (const match of text.matchAll(EXPLICIT_MATH)) {
        const start = match.index ?? 0;
        out += typesetHeuristic(text.slice(last, start));
        const latex = match[1] ?? match[2] ?? match[3] ?? match[4] ?? "";
        out += katexSpan(latex, latex) ?? match[0];
        last = start + match[0].length;
    }
    out += typesetHeuristic(text.slice(last));
    return out;
}

function typesetOutsideUrls(text: string): string {
    URL_TOKEN.lastIndex = 0;
    let out = "";
    let last = 0;
    for (const match of text.matchAll(URL_TOKEN)) {
        const start = match.index ?? 0;
        out += typesetPlain(text.slice(last, start));
        out += match[0];
        last = start + match[0].length;
    }
    out += typesetPlain(text.slice(last));
    return out;
}

function typesetLabelInterior(inner: string): string {
    const parts = inner.split(/(<br\s*\/?>)/gi);
    return parts.map((part) => {
        if (/^<br/i.test(part) || alreadyTypeset(part)) return part;
        // KaTeX MathML carries xmlns="http://www.w3.org/...". Splitting URLs
        // before the already-typeset check would cut that span open and typeset
        // the annotation on the next pass.
        return typesetOutsideUrls(part);
    }).join("");
}

function lineIsClick(source: string, quoteIndex: number): boolean {
    const start = source.lastIndexOf("\n", quoteIndex - 1) + 1;
    const lineEnd = source.indexOf("\n", quoteIndex);
    const line = source.slice(start, lineEnd < 0 ? source.length : lineEnd);
    return /^\s*click\b/i.test(line);
}

/** Replace formula text inside double-quoted Mermaid labels. Other syntax stays. */
export function typesetMermaidLabelMath(source: string): string {
    if (!source || !source.includes('"')) return source;
    let out = "";
    let index = 0;
    while (index < source.length) {
        const open = source.indexOf('"', index);
        if (open < 0) {
            out += source.slice(index);
            break;
        }
        out += source.slice(index, open + 1);
        const close = source.indexOf('"', open + 1);
        if (close < 0) {
            out += source.slice(open + 1);
            break;
        }
        // `click A "https://host/a?id=1"` is a link, not a node label.
        if (lineIsClick(source, open)) {
            out += source.slice(open + 1, close + 1);
            index = close + 1;
            continue;
        }
        out += typesetLabelInterior(source.slice(open + 1, close));
        out += '"';
        index = close + 1;
    }
    return out;
}
