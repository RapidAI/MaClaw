import { describe, expect, it } from "vitest";
import { companionCodeMathToTex, looksLikeCompanionMath } from "./companionMathMarkdown";

describe("companionCodeMathToTex", () => {
    it("leaves text without backticks untouched", () => {
        const text = "公式 $x_i = f(I, q, d_i)$ 已经用 TeX 写好。";
        expect(companionCodeMathToTex(text)).toBe(text);
    });

    it("turns a backtick-wrapped inline formula into inline TeX", () => {
        expect(companionCodeMathToTex("输入按相关性评估提示拼成 `x_i = f(I, q, d_i)`，每个查询独立前向。"))
            .toBe("输入按相关性评估提示拼成 $x_i = f(I, q, d_i)$，每个查询独立前向。");
    });

    it("recognizes a subscripted call without an equals sign", () => {
        expect(companionCodeMathToTex("激活值 `a_j(q, d_i)` 是一个实数。"))
            .toBe("激活值 $a_j(q, d_i)$ 是一个实数。");
    });

    it("recognizes unicode operators and braced superscripts", () => {
        expect(companionCodeMathToTex("`a(q, d_i) = SiLU(W_gate·h_i) ⊙ (W_up·h_i) ∈ R^{D_l}`"))
            .toBe("$a(q, d_i) = SiLU(W_gate·h_i) ⊙ (W_up·h_i) ∈ R^{D_l}$");
        expect(companionCodeMathToTex("`r_i^{(1,j)} ∈ {1..N}`"))
            .toBe("$r_i^{(1,j)} ∈ {1..N}$");
    });

    it("recognizes a TeX command", () => {
        expect(companionCodeMathToTex("`\\frac{a}{b}`")).toBe("$\\frac{a}{b}$");
        expect(companionCodeMathToTex("`\\mathrm{Var}(X)`")).toBe("$\\mathrm{Var}(X)$");
        expect(companionCodeMathToTex("`\\operatorname{Var}(X)`")).toBe("$\\operatorname{Var}(X)$");
        // Consecutive commands are mathematics, not a Windows path.
        expect(companionCodeMathToTex("`\\alpha\\beta`")).toBe("$\\alpha\\beta$");
        // A formula colon without a space is not a drive letter.
        expect(companionCodeMathToTex("`note:\\frac{a}{b}`")).toBe("$note:\\frac{a}{b}$");
    });

    it("keeps document-level LaTeX source in code", () => {
        for (const source of [
            "\\documentclass{article}",
            "\\usepackage{amsmath}",
            "\\section{方法}",
            "\\begin{document}正文\\end{document}",
        ]) {
            expect(companionCodeMathToTex(`\`${source}\``)).toBe(`\`${source}\``);
        }
        const fence = "```latex\n\\documentclass{article}\n\\begin{document}\nHi\n\\end{document}\n```";
        expect(companionCodeMathToTex(fence)).toBe(fence);
        // A drawing is source too, even though the fence says latex.
        const tikz = "```latex\n\\begin{tikzpicture}\n\\draw (0,0) -- (1,1);\n\\end{tikzpicture}\n```";
        expect(companionCodeMathToTex(tikz)).toBe(tikz);
        // A math environment is still a formula.
        expect(companionCodeMathToTex("`\\begin{aligned} x_i &= y \\end{aligned}`"))
            .toBe("$\\begin{aligned} x_i &= y \\end{aligned}$");
    });

    it("unwraps a backtick around an already-delimited formula", () => {
        expect(companionCodeMathToTex("`$x_i = f(I, q, d_i)$`")).toBe("$x_i = f(I, q, d_i)$");
        expect(companionCodeMathToTex("`$$r_i^{(1,j)} \\in \\{1..N\\}$$`")).toBe("$$r_i^{(1,j)} \\in \\{1..N\\}$$");
        expect(companionCodeMathToTex("`\\(x_i\\)`")).toBe("\\(x_i\\)");
        // A dollar-led identifier is code, not a delimited formula.
        expect(companionCodeMathToTex("`$HOME`")).toBe("`$HOME`");
        expect(companionCodeMathToTex("`${x}`")).toBe("`${x}`");
    });

    it("unfences a math-labelled block that already has display delimiters", () => {
        const text = "```latex\n$$\na_j(q) = [a_j(q,d_1), ..., a_j(q,d_N)]\n$$\n```";
        expect(companionCodeMathToTex(text))
            .toBe("$$\na_j(q) = [a_j(q,d_1), ..., a_j(q,d_N)]\n$$");
    });

    it("keeps code spans that are not mathematics literal", () => {
        for (const code of [
            "get_user_by_id(user_id)",
            "const price = $5;",
            "foo_bar",
            "C:\\new\\file",
            "dir=C:\\Users\\name",
            "print(\"a\")",
            "return user_id",
            "SELECT * FROM users",
            "user_id",
        ]) {
            expect(companionCodeMathToTex(`代码 \`${code}\` 保持原样。`)).toBe(`代码 \`${code}\` 保持原样。`);
        }
    });

    it("keeps multi-backtick spans literal", () => {
        expect(companionCodeMathToTex("``const price = $5;`` 且 $x$ 是公式。"))
            .toBe("``const price = $5;`` 且 $x$ 是公式。");
        // An unclosed multi-backtick span hides the rest of the line.
        expect(companionCodeMathToTex("``x_i = f(I, q, d_i)")).toBe("``x_i = f(I, q, d_i)");
    });

    it("turns a bare fence holding one formula into display TeX", () => {
        const text = "第一步：\n\n```\na_j(q) = [a_j(q,d_1), ..., a_j(q,d_N)]\n```\n\n然后排序。";
        expect(companionCodeMathToTex(text))
            .toBe("第一步：\n\n$$\na_j(q) = [a_j(q,d_1), ..., a_j(q,d_N)]\n$$\n\n然后排序。");
    });

    it("turns a math-labelled fence into display TeX", () => {
        const text = "```latex\na(q, d_i) = SiLU(W_gate·h_i) ⊙ (W_up·h_i) ∈ R^{D_l}\n```";
        expect(companionCodeMathToTex(text))
            .toBe("$$\na(q, d_i) = SiLU(W_gate·h_i) ⊙ (W_up·h_i) ∈ R^{D_l}\n$$");
        // Tilde fences are code too.
        expect(companionCodeMathToTex("~~~latex\nx_i = y\n~~~"))
            .toBe("$$\nx_i = y\n$$");
    });

    it("keeps code fences and their contents literal", () => {
        const text = "```ts\nconst ok = true;\n```";
        expect(companionCodeMathToTex(text)).toBe(text);
        const bare = "```\nconst ok = true;\n```";
        expect(companionCodeMathToTex(bare)).toBe(bare);
        const assignment = "```\nuser_id = row[0]\n```";
        expect(companionCodeMathToTex(assignment)).toBe(assignment);
    });

    it("converts a fenced one-letter assignment", () => {
        const text = "```\nr*_i = i\n```";
        expect(companionCodeMathToTex(text)).toBe("$$\nr*_i = i\n$$");
    });

    it("does not touch a displaced formula inside a longer prose fence", () => {
        const text = "```\n这一段是说明文字，不是公式。\n```";
        expect(companionCodeMathToTex(text)).toBe(text);
    });

    it("leaves an unfinished fence alone while streaming", () => {
        const text = "```\nx_i = f(I, q, d_i)";
        expect(companionCodeMathToTex(text)).toBe(text);
    });

    it("does not convert a formula that is already delimited", () => {
        const text = "```\n$$x_i = f(I, q, d_i)$$\n```";
        expect(companionCodeMathToTex(text)).toBe(text);
    });

    it("is idempotent", () => {
        const input = "把 `x_i = f(I, q, d_i)` 代进去：\n\n```\na_j(q) = [a_j(q,d_1), ..., a_j(q,d_N)]\n```";
        const once = companionCodeMathToTex(input);
        expect(once).not.toBe(input);
        expect(companionCodeMathToTex(once)).toBe(once);
    });
});

describe("looksLikeCompanionMath", () => {
    it("accepts the formulas from a paper explanation", () => {
        for (const formula of [
            "x_i = f(I, q, d_i)",
            "a(q, d_i) = SiLU(W_gate·h_i) ⊙ (W_up·h_i) ∈ R^{D_l}",
            "a_j(q) = [a_j(q,d_1), ..., a_j(q,d_N)]",
            "r_i^{(1,j)} ∈ {1..N}",
            "r*_i = i",
            "a_j(q, d_i)",
        ]) {
            expect(looksLikeCompanionMath(formula)).toBe(true);
        }
    });

    it("rejects code, paths, prose and empty input", () => {
        for (const source of [
            "",
            "const price = $5;",
            "get_user_by_id(user_id)",
            "C:\\new\\file",
            "这一段是说明文字。",
            "user_id",
            "rows = []",
        ]) {
            expect(looksLikeCompanionMath(source)).toBe(false);
        }
    });
});
