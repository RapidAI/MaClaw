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

    it("typesets bare TeX subscripts and superscripts in a sentence", () => {
        expect(companionCodeMathToTex("Alignment: F^l_{0,c}, F^l_{1,c} = dilated conv (k=1, d=16) of F^l_0, F^l_1."))
            .toBe("Alignment: $F^l_{0,c}$, $F^l_{1,c}$ = dilated conv (k=1, d=16) of $F^l_0$, $F^l_1$.");
        expect(companionCodeMathToTex("Loss: L_total = L_ce + λ1 L_d^l + λ2 L_comp, with L_comp pulling."))
            .toBe("Loss: $L_total$ = $L_ce$ + λ1 $L_d^l$ + λ2 $L_comp$, with $L_comp$ pulling.");
        expect(companionCodeMathToTex("final Ŷ = σ(Ŷ_ag + Ŷ_aw)，令F^l_{0,c}成立。"))
            .toBe("final $Ŷ$ = $σ$($Ŷ_ag$ + $Ŷ_aw$)，令$F^l_{0,c}$成立。");
    });

    it("typesets unicode subscripts, superscripts, and lone Greek letters", () => {
        const yellow = "文中取 λ₁=λ₂=1、γ=1、τ=25%。";
        const yellowTex = "文中取 $λ_1$=$λ_2$=1、$γ$=1、$τ$=25%。";
        expect(companionCodeMathToTex(yellow)).toBe(yellowTex);
        expect(companionCodeMathToTex(yellowTex)).toBe(yellowTex);
        expect(companionCodeMathToTex("特征 F₀ˡ、F₁ˡ 与 F₀,cˡ。"))
            .toBe("特征 $F_0^l$、$F_1^l$ 与 $F_{0,c}^l$。");
        expect(companionCodeMathToTex("差异 Oˡ，掩码 Mˡ，Cat(F₀ᴺ, F₁ᴺ)，求和 Σᵢ。"))
            .toBe("差异 $O^l$，掩码 $M^l$，Cat($F_0^N$, $F_1^N$)，求和 $Σ_i$。");
        expect(companionCodeMathToTex("损失 L_total = L_ce + λ₁ Σᵢ L_dˡ + λ₂ L_comp。"))
            .toBe("损失 $L_total$ = $L_ce$ + $λ_1$ $Σ_i$ $L_d^l$ + $λ_2$ $L_comp$。");
        expect(companionCodeMathToTex("特征 F₀^l 与 F₀, convolution。"))
            .toBe("特征 $F_0^l$ 与 $F_0$, convolution。");
        // An ASCII digit after a Greek letter is still prose, as is a unit or a word.
        expect(companionCodeMathToTex("系数 λ1 与 λ2。")).toBe("系数 λ1 与 λ2。");
        expect(companionCodeMathToTex("单位 μm，文件 file²，浓度 H₂O。"))
            .toBe("单位 μm，文件 file²，浓度 $H_2$O。");
        expect(companionCodeMathToTex("字段 inₜₕₑ 保持原样。")).toBe("字段 inₜₕₑ 保持原样。");
        expect(companionCodeMathToTex("`λ₁` 与 F₀ˡ")).toBe("`λ₁` 与 $F_0^l$");
        expect(companionCodeMathToTex("![F₀ˡ](https://example.com/a.png) 与 λ₁"))
            .toBe("![F₀ˡ](https://example.com/a.png) 与 $λ_1$");
        expect(companionCodeMathToTex("见 [方法 λ₁](https://example.com/a_b) 后 γ。"))
            .toBe("见 [方法 $λ_1$](https://example.com/a_b) 后 $γ$。");
        expect(companionCodeMathToTex("$F_0 and λ₁")).toBe("$F_0 and λ₁");
        expect(companionCodeMathToTex("$x$ 与 λ₁")).toBe("$x$ 与 $λ_1$");
        // A prime sits in front of a unicode script the same way it does for `F'_0`.
        expect(companionCodeMathToTex("残差 F'₀ 与 F*₀。")).toBe("残差 $F'_0$ 与 $F*_0$。");
        expect(companionCodeMathToTex("记号 F₀^{l} 与 F₀^{l_{0}}。")).toBe("记号 $F_0^{l}$ 与 $F_0^{l_{0}}$。");
        // A failed or prose script stays attached to its letter.
        expect(companionCodeMathToTex("See σ^{the result of alignment} next.")).toBe("See σ^{the result of alignment} next.");
        expect(companionCodeMathToTex("α^")).toBe("α^");
        expect(companionCodeMathToTex("α^2")).toBe("$α^2$");
        expect(companionCodeMathToTex("F₀_{0")).toBe("F₀_{0");
        expect(companionCodeMathToTex("F₀_{0,c}")).toBe("F₀_{0,c}");
        expect(companionCodeMathToTex("σ(Ŷ_ag)")).toBe("$σ$($Ŷ_ag$)");
        expect(companionCodeMathToTex("Ŷ=1")).toBe("$Ŷ$=1");
        // The apostrophe is part of the variable. A star is multiplication, and a Latin prime is prose.
        expect(companionCodeMathToTex("导数 σ'(x) 与 σ′。")).toBe("导数 $σ'$(x) 与 $σ′$。");
        expect(companionCodeMathToTex(companionCodeMathToTex("导数 σ'(x) 与 σ′。"))).toBe("导数 $σ'$(x) 与 $σ′$。");
        expect(companionCodeMathToTex("σ'₀ 与 σ*v，函数 f'。")).toBe("$σ'_0$ 与 $σ$*v，函数 f'。");
        expect(companionCodeMathToTex("σ'^")).toBe("σ'^");
        expect(companionCodeMathToTex(`a${"ₐ".repeat(9)}`)).toBe(`a${"ₐ".repeat(9)}`);
        // A second apostrophe is still the variable. A dangling caret is unfinished.
        expect(companionCodeMathToTex("导数 σ''(x) 与 Ŷ''。")).toBe("导数 $σ''$(x) 与 $Ŷ''$。");
        expect(companionCodeMathToTex("σ''''")).toBe("σ''''");
        expect(companionCodeMathToTex("F''_0 与 F''₀ 与 σ''^2。")).toBe("$F''_0$ 与 $F''_0$ 与 $σ''^2$。");
        expect(companionCodeMathToTex("F₀^ 与 L_d^ 与 x²^。")).toBe("F₀^ 与 L_d^ 与 x²^。");
        expect(companionCodeMathToTex("残差 F₀' 与 F_0'。")).toBe("残差 $F_0'$ 与 $F_0'$。");
        expect(companionCodeMathToTex("F_0_1 与 x^2^3。")).toBe("F_0_1 与 x^2^3。");
        // A prime between scripts stays inside the formula. An unfinished one stays whole.
        expect(companionCodeMathToTex("导数 F_0'^l 与 F₀'^l 与 L_d'ˡ。")).toBe("导数 $F_0'^l$ 与 $F_0'^l$ 与 $L_d'^l$。");
        expect(companionCodeMathToTex("F_0'^")).toBe("F_0'^");
        expect(companionCodeMathToTex("F_0*x")).toBe("$F_0$*x");
        expect(companionCodeMathToTex("名称 in_the'^2 保持原样。")).toBe("名称 in_the'^2 保持原样。");
        expect(companionCodeMathToTex("见 [方法 F_0'^l](https://example.com/a_b) 后 x^*。"))
            .toBe("见 [方法 $F_0'^l$](https://example.com/a_b) 后 $x^*$。");
    });

    it("leaves names, code, urls and delimited formulas untouched", () => {
        const text = "公式 $F^l_{0,c}$ 已经写好。见 https://example.com/a_b 和 `user_id`。";
        expect(companionCodeMathToTex(text)).toBe(text);
        expect(companionCodeMathToTex("见 HTTP://example.com/a_b_c 即可。")).toBe("见 HTTP://example.com/a_b_c 即可。");
        for (const name of ["user_id", "feature_diff", "in_the", "id_name", "get_user_by_id", "py_v2"]) {
            expect(companionCodeMathToTex(`字段 ${name} 保持原样。`)).toBe(`字段 ${name} 保持原样。`);
        }
        const fence = "```ts\nconst value = F^l_0;\n```";
        expect(companionCodeMathToTex(fence)).toBe(fence);
        // A Windows newline must not hide the fence, or the formula inside is wrapped.
        expect(companionCodeMathToTex("```ts\r\nconst value = F^l_0;\r\n```")).toBe(fence);
        expect(companionCodeMathToTex("令F^l_0成立\r\n")).toBe("令$F^l_0$成立\n");
        // A phrase in braces is not a subscript, and an open `` ends at the line.
        expect(companionCodeMathToTex("See F^{the result of alignment} next.")).toBe("See F^{the result of alignment} next.");
        expect(companionCodeMathToTex("``keep\nF^l_0")).toBe("``keep\n$F^l_0$");
        expect(companionCodeMathToTex("面积 km^2，浓度 CO_2。")).toBe("面积 $km^2$，浓度 $CO_2$。");
        expect(companionCodeMathToTex("见 https://example.com/a_b。令F^l_0成立，it's_fine。"))
            .toBe("见 https://example.com/a_b。令$F^l_0$成立，it's_fine。");
        expect(companionCodeMathToTex("残差 F'_0。")).toBe("残差 $F'_0$。");
        expect(companionCodeMathToTex("对偶 x^* 与 F^*_0，记号 F*_0。")).toBe("对偶 $x^*$ 与 $F^*_0$，记号 $F*_0$。");
        expect(companionCodeMathToTex("文件 file^* 保持原样。")).toBe("文件 file^* 保持原样。");
        // A `$$` that is not a display block must not hide a later formula.
        expect(companionCodeMathToTex("see $$ later F^l_0")).toBe("see $$ later $F^l_0$");
        expect(companionCodeMathToTex("$$\nF^l_0")).toBe("$$\nF^l_0");
        expect(companionCodeMathToTex("$$\nF^l_0\n$$")).toBe("$$\nF^l_0\n$$");
        expect(companionCodeMathToTex("名称 in_the^2 保持原样。")).toBe("名称 in_the^2 保持原样。");
    });

    it("typesets one nested brace and leaves an open delimiter alone", () => {
        expect(companionCodeMathToTex("特征 F^{l_{0,c}} 与 x_{i_{j}}。")).toBe("特征 $F^{l_{0,c}}$ 与 $x_{i_{j}}$。");
        expect(companionCodeMathToTex("`F^{l_{0,c}}`")).toBe("$F^{l_{0,c}}$");
        // A closer that has not arrived yet must not grow a second dollar,
        // and must not pull a later formula on the same line into the span.
        expect(companionCodeMathToTex("$F^l_0")).toBe("$F^l_0");
        expect(companionCodeMathToTex("$F^l_0 and L_ce")).toBe("$F^l_0 and L_ce");
        expect(companionCodeMathToTex("$F^l_0$ and L_ce")).toBe("$F^l_0$ and $L_ce$");
        expect(companionCodeMathToTex("\\(F^l_0")).toBe("\\(F^l_0");
        expect(companionCodeMathToTex("see \\[F^l_0")).toBe("see \\[F^l_0");
        // A closer glued to the next letter is not a boundary, so both stay
        // source. A space is a boundary, and the next atom typesets.
        expect(companionCodeMathToTex("$x$F^l_0")).toBe("$x$F^l_0");
        expect(companionCodeMathToTex("$x$ 与 F^l_0")).toBe("$x$ 与 $F^l_0$");
        // A finished display block does not pair with a later `$$`.
        expect(companionCodeMathToTex("$$\nx\n$$\nsee $$F^l_0")).toBe("$$\nx\n$$\nsee $$F^l_0");
        expect(companionCodeMathToTex("$$x$$F^l_0")).toBe("$$x$$$F^l_0$");
        // A dollar inside code or a link is not an opener for the rest of the line.
        expect(companionCodeMathToTex("代码 `$total` 与 L_ce。")).toBe("代码 `$total` 与 $L_ce$。");
        expect(companionCodeMathToTex("见 [说明](https://example.com/a$b) 与 L_ce。")).toBe("见 [说明](https://example.com/a$b) 与 $L_ce$。");
        expect(companionCodeMathToTex("记号 `$$` 之后 $$F^l_0")).toBe("记号 `$$` 之后 $$F^l_0");
        // An inline label is typeset. The destination, an image alt, and a
        // reference label stay source, so the target remains attached.
        expect(companionCodeMathToTex("见 [方法 F^l_0](https://example.com/a_b) 后 F^l_1。"))
            .toBe("见 [方法 $F^l_0$](https://example.com/a_b) 后 $F^l_1$。");
        expect(companionCodeMathToTex("见 [文档 https://example.com/a_b](https://example.com/c)F^l_1。"))
            .toBe("见 [文档 https://example.com/a_b](https://example.com/c)$F^l_1$。");
        // A `)` inside the path is still the destination. The formula there stays
        // part of the target; the label and the following atom typeset.
        const parenthesized = "见 [方法 F^l_0](https://example.com/a_(b)/F^l_1) 后 L_ce。";
        const parenthesizedTex = "见 [方法 $F^l_0$](https://example.com/a_(b)/F^l_1) 后 $L_ce$。";
        expect(companionCodeMathToTex(parenthesized)).toBe(parenthesizedTex);
        expect(companionCodeMathToTex(parenthesizedTex)).toBe(parenthesizedTex);
        expect(companionCodeMathToTex("见 [方法 F^l_0](https://example.com/a_(b_(c))/F^l_1) 后 L_ce。"))
            .toBe("见 [方法 $F^l_0$](https://example.com/a_(b_(c))/F^l_1) 后 $L_ce$。");
        expect(companionCodeMathToTex("![F^l_0](https://example.com/a_(b).png) 与 L_ce"))
            .toBe("![F^l_0](https://example.com/a_(b).png) 与 $L_ce$");
        expect(companionCodeMathToTex("见 [项 F^{l_0](https://example.com/a) 后 L_ce。"))
            .toBe("见 [项 F^{l_0](https://example.com/a) 后 $L_ce$。");
        expect(companionCodeMathToTex("![F^l_0](https://example.com/a.png) 与 F^l_1"))
            .toBe("![F^l_0](https://example.com/a.png) 与 $F^l_1$");
        expect(companionCodeMathToTex("见 [F^l_0][fig] 与 F^l_1")).toBe("见 [F^l_0][fig] 与 $F^l_1$");
        // An unclosed or too-deep brace is not a shorter atom plus a stray tail.
        expect(companionCodeMathToTex("F^{l_{0,c}")).toBe("F^{l_{0,c}");
        expect(companionCodeMathToTex("项 x_{i_{j} 未写完")).toBe("项 x_{i_{j} 未写完");
        expect(companionCodeMathToTex("项 x_{i_{j_{k}}} 保持原样")).toBe("项 x_{i_{j_{k}}} 保持原样");
    });

    it("typesets a bare TeX command written into prose", () => {
        expect(companionCodeMathToTex("概率过置信（τ \\approx 1），实际覆盖率 \\geq 0.997。"))
            .toBe("概率过置信（$τ$ $\\approx$ 1），实际覆盖率 $\\geq$ 0.997。");
        expect(companionCodeMathToTex("误差 0.786\\pm0.004 与 0.725\\pm0.0205。"))
            .toBe("误差 $0.786\\pm0.004$ 与 $0.725\\pm0.0205$。");
        expect(companionCodeMathToTex("参数 \\lambda_i 与 \\frac{a}{b} 与 \\sqrt[3]{x}。"))
            .toBe("参数 $\\lambda_i$ 与 $\\frac{a}{b}$ 与 $\\sqrt[3]{x}$。");
        expect(companionCodeMathToTex("方差 \\sigma_i^2 不变。")).toBe("方差 $\\sigma_i^2$ 不变。");
        expect(companionCodeMathToTex("命令 \\alpha\\beta 连用。")).toBe("命令 $\\alpha\\beta$ 连用。");
    });

    it("keeps paired TeX delimiters and environments whole", () => {
        expect(companionCodeMathToTex("区间 \\left(0, 1\\right) 与 \\big(x。"))
            .toBe("区间 $\\left(0, 1\\right)$ 与 $\\big($x。");
        expect(companionCodeMathToTex("环境 \\begin{aligned} a &= b \\\\ c &= d \\end{aligned} 结束。"))
            .toBe("环境 $\\begin{aligned} a &= b \\\\ c &= d \\end{aligned}$ 结束。");
    });

    it("leaves a command whose arguments hold an inner $...$ formula alone", () => {
        const text = "见 \\frac{$x$}{y} 与 \\left(a $+$ b\\right) 保持原样。";
        expect(companionCodeMathToTex(text)).toBe(text);
    });

    it("leaves a multi-line environment or pair literal ($ cannot cross lines)", () => {
        const env = "环境：\n\\begin{aligned}\na &= b\n\\end{aligned}\n完。";
        expect(companionCodeMathToTex(env)).toBe(env);
        expect(companionCodeMathToTex("式 \\left(a\nb\\right) 保持原样。"))
            .toBe("式 \\left(a\nb\\right) 保持原样。");
    });

    it("tolerates whitespace inside \\begin / \\end environment names", () => {
        expect(companionCodeMathToTex("环境 \\begin { aligned } a &= b \\end { aligned } 结束。"))
            .toBe("环境 $\\begin { aligned } a &= b \\end { aligned }$ 结束。");
        expect(companionCodeMathToTex("环境 \\begin{aligned } a &= b \\end{aligned} 结束。"))
            .toBe("环境 $\\begin{aligned } a &= b \\end{aligned}$ 结束。");
    });

    it("leaves environments KaTeX cannot typeset as prose", () => {
        const text = "环境 \\begin{align} a &= b \\end{align} 与 \\begin{equation} x \\end{equation} 保持原样。";
        expect(companionCodeMathToTex(text)).toBe(text);
    });

    it("pairs nested \\left..\\right and same-name environments correctly", () => {
        expect(companionCodeMathToTex("见 \\left(a \\left(b \\right) c \\right) 即可。"))
            .toBe("见 $\\left(a \\left(b \\right) c \\right)$ 即可。");
        expect(companionCodeMathToTex("见 \\begin{matrix} a \\begin{matrix} x \\end{matrix} \\end{matrix} 即可。"))
            .toBe("见 $\\begin{matrix} a \\begin{matrix} x \\end{matrix} \\end{matrix}$ 即可。");
        // The inner balanced pair still promotes; the unbalanced outer opener
        // stays literal.
        expect(companionCodeMathToTex("见 \\left(a \\left(b \\right) 保持原样。"))
            .toBe("见 \\left(a $\\left(b \\right)$ 保持原样。");
    });

    it("accepts \\{ as a delimiter but not a bare {", () => {
        expect(companionCodeMathToTex("集合 \\left\\{ x \\right\\} 完。"))
            .toBe("集合 $\\left\\{ x \\right\\}$ 完。");
        const bare = "见 \\left{ x \\right} 完。";
        expect(companionCodeMathToTex(bare)).toBe(bare);
    });

    it("rejects a pair whose body has a delimiter-less \\left or \\right", () => {
        const innerRight = "见 \\left(a \\right x \\right) 完。";
        expect(companionCodeMathToTex(innerRight)).toBe(innerRight);
        const innerLeft = "见 \\left(a \\left x \\right) y \\right) 完。";
        expect(companionCodeMathToTex(innerLeft)).toBe(innerLeft);
    });

    it("rejects an environment whose delimiters do not balance", () => {
        const strayRight = "见 \\begin{matrix} a \\right) \\end{matrix} 完。";
        expect(companionCodeMathToTex(strayRight)).toBe(strayRight);
        const openLeft = "见 \\begin{matrix} \\left(a \\end{matrix} 完。";
        expect(companionCodeMathToTex(openLeft)).toBe(openLeft);
        expect(companionCodeMathToTex("见 \\begin{aligned} a &= \\left(x \\middle| y\\right) \\end{aligned} 完。"))
            .toBe("见 $\\begin{aligned} a &= \\left(x \\middle| y\\right) \\end{aligned}$ 完。");
    });

    it("handles a \\left..\\right pair that spans an environment boundary", () => {
        // A pair opened inside the env and closed outside is a KaTeX error.
        const insideOut = "见 \\begin{aligned} \\begin{matrix} \\left(a \\end{matrix} b\\right) \\end{aligned} 完。";
        expect(companionCodeMathToTex(insideOut)).toBe(insideOut);
        // A pair opened before the env and closed after is legal KaTeX.
        expect(companionCodeMathToTex("见 \\begin{aligned} \\left(a \\begin{matrix} x \\end{matrix} \\right) \\end{aligned} 完。"))
            .toBe("见 $\\begin{aligned} \\left(a \\begin{matrix} x \\end{matrix} \\right) \\end{aligned}$ 完。");
    });

    it("rejects an environment whose body has a malformed \\begin / \\end", () => {
        const text = "见 \\begin{aligned} a \\begin b \\end{aligned} 完。";
        expect(companionCodeMathToTex(text)).toBe(text);
    });

    it("rejects an environment whose body has a stray or unsupported \\end", () => {
        const stray = "见 \\begin{aligned} a \\end{matrix} b \\end{aligned} 完。";
        expect(companionCodeMathToTex(stray)).toBe(stray);
        const badNested = "见 \\begin{aligned} \\begin{foo} x \\end{foo} \\end{aligned} 完。";
        expect(companionCodeMathToTex(badNested)).toBe(badNested);
        // A properly nested different-name environment is legal KaTeX.
        expect(companionCodeMathToTex("见 \\begin{aligned} a &= \\begin{matrix} x \\end{matrix} \\end{aligned} 完。"))
            .toBe("见 $\\begin{aligned} a &= \\begin{matrix} x \\end{matrix} \\end{aligned}$ 完。");
    });

    it("accepts \\lbrace / \\rbrace as pair delimiters", () => {
        expect(companionCodeMathToTex("集合 \\left\\lbrace x \\right\\rbrace 完。"))
            .toBe("集合 $\\left\\lbrace x \\right\\rbrace$ 完。");
    });

    it("leaves a command glued to an unclosed \\( or \\[ opener alone", () => {
        for (const text of ["见 \\(\\approx 未完。", "见 \\[\\approx 未完。"]) {
            expect(companionCodeMathToTex(text)).toBe(text);
        }
    });

    it("requires the column argument on array-like environments", () => {
        expect(companionCodeMathToTex("表 \\begin{array}{cc} a & b \\end{array} 完。"))
            .toBe("表 $\\begin{array}{cc} a & b \\end{array}$ 完。");
        const missing = "表 \\begin{array} a & b \\end{array} 完。";
        expect(companionCodeMathToTex(missing)).toBe(missing);
    });

    it("leaves a lone \\middle as prose (it needs an enclosing \\left)", () => {
        const text = "中间 \\middle| 与 \\middle|x\\right) 保持原样。";
        expect(companionCodeMathToTex(text)).toBe(text);
        expect(companionCodeMathToTex("区间 \\left(a \\middle| b\\right) 即可。"))
            .toBe("区间 $\\left(a \\middle| b\\right)$ 即可。");
    });

    it("requires the full argument arity on two-argument commands", () => {
        for (const text of [
            "见 \\frac{a} 保持原样。",
            "见 \\overset{a} 保持原样。",
            "见 \\sqrt[3] 保持原样。",
            "见 \\binom{n} 保持原样。",
        ]) {
            expect(companionCodeMathToTex(text)).toBe(text);
        }
        expect(companionCodeMathToTex("见 \\frac{a}{b} 与 \\overset{*}{x} 即可。"))
            .toBe("见 $\\frac{a}{b}$ 与 $\\overset{*}{x}$ 即可。");
    });

    it("leaves a command that cannot stand alone as prose", () => {
        const text = "命令 \\frac 与 \\left( 与 \\end{aligned} 与 \\big 保持原样。";
        expect(companionCodeMathToTex(text)).toBe(text);
        // `\rightarrow` inside the body is not the `\right` closer.
        expect(companionCodeMathToTex("见 \\left(a \\rightarrow b\\right) 即可。"))
            .toBe("见 $\\left(a \\rightarrow b\\right)$ 即可。");
    });

    it("leaves stray backslashes, paths and unknown commands alone", () => {
        for (const text of [
            "路径 C:\\new\\file 保持原样。",
            "名字 foo\\bar 保持原样。",
            "命令 \\notacommand 保持原样。",
            "见 https://example.com/a\\b 保持原样。",
            "转义 \\\\approx 保持原样。",
        ]) {
            expect(companionCodeMathToTex(text)).toBe(text);
        }
    });

    it("treats a digit-led dollar span as math when it carries TeX", () => {
        const text = "达 $0.905\\pm0.013$ 与 $0.934$，而 $5 到 $10 是价格。";
        expect(companionCodeMathToTex(text)).toBe(text);
        // The span is opaque: atoms inside it are not promoted again.
        expect(companionCodeMathToTex("达 $0.5 F^l_0$ 与 L_ce。")).toBe("达 $0.5 F^l_0$ 与 $L_ce$。");
        // Currency is still not math, and a later atom still promotes.
        expect(companionCodeMathToTex("价格 $5 到 $10 与 L_ce。")).toBe("价格 $5 到 $10 与 $L_ce$。");
    });

    it("wraps a backtick TeX atom once", () => {
        expect(companionCodeMathToTex("`F^l_{0,c}`")).toBe("$F^l_{0,c}$");
        expect(companionCodeMathToTex(companionCodeMathToTex("`F^l_{0,c}`"))).toBe("$F^l_{0,c}$");
    });

    it("is idempotent", () => {
        const input = "把 `x_i = f(I, q, d_i)` 代进去：\n\n```\na_j(q) = [a_j(q,d_1), ..., a_j(q,d_N)]\n```";
        const once = companionCodeMathToTex(input);
        expect(once).not.toBe(input);
        expect(companionCodeMathToTex(once)).toBe(once);
    });

    it("is idempotent on promoted commands, pairs and environments", () => {
        for (const input of [
            "误差 0.786\\pm0.004 与 τ \\approx 1。",
            "环境 \\begin{aligned} a &= b \\end{aligned} 与 \\left(x\\right) 完。",
            "式 \\frac{a}{b} + \\sigma_i^2 + \\sqrt[3]{x} 即可。",
        ]) {
            const once = companionCodeMathToTex(input);
            expect(once).toContain("$");
            expect(companionCodeMathToTex(once)).toBe(once);
        }
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
