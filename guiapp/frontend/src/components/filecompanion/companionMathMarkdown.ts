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

/**
 * Normalize code-wrapped formulas in one companion reply. Text without a
 * backtick is returned as the same string without scanning.
 */
export function companionCodeMathToTex(text: string): string {
    if (!text || (!text.includes("`") && !text.includes("~~~"))) return text;
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
