import { describe, expect, it } from "vitest";
import mermaid from "mermaid";
import { paintMermaidKatexInk, svgWithRasterLabels } from "../AssistantMermaidDiagram";
import { typesetMermaidLabelMath } from "../mermaidLabelMath";

const LOSS = [
    "flowchart LR",
    '  A["英文锚点 safe/JB/INJ<br/>→ 五语翻译 en zh es hi sw"] --> B["同一主干编码成 z(x)"]',
    '  B --> C["L = L_CE + 0.5·L_InfoNCE + 0.2·L_SupCon<br/>focal CE / 翻译对 τ=0.07 / 类间隔 m=0.1"]',
    '  C --> D["意图向量 z(x) ∈ R^512"]',
    '  D --> E["安全门 p_safe ≥ t* ?"]',
].join("\n");

function quotes(text: string): number {
    return text.match(/"/g)?.length ?? 0;
}

/** The export path keeps the original formula on data-plain. Assertions about
 *  the drawn markup should ignore that attribute. */
function drawn(text: string): string {
    return text.replace(/ data-plain='[^']*'/g, "");
}

describe("typesetMermaidLabelMath", () => {
    it("typesets underscore subscripts and leaves the surrounding Chinese", () => {
        const next = typesetMermaidLabelMath(LOSS);
        const loss = next.split("\n")[2];
        expect(loss).toContain("class='katex'");
        expect(loss).toContain("msupsub");
        expect(loss).not.toContain("white-space:nowrap");
        expect(loss).toContain("data-plain='L = L_CE + 0.5·L_InfoNCE + 0.2·L_SupCon'");
        expect(drawn(loss)).not.toContain("L_CE");
        expect(drawn(loss)).not.toContain("L_InfoNCE");
        expect(drawn(loss)).not.toContain("L_SupCon");
        expect(loss).toContain("翻译对");
        expect(loss).toContain("focal CE");
        expect(drawn(loss)).not.toContain("τ=0.07");
        expect(drawn(loss)).not.toContain("m=0.1");
        expect(drawn(next)).not.toContain("p_safe");
        expect(drawn(next)).not.toContain("R^512");
        expect(quotes(next)).toBe(quotes(LOSS));
    });

    it("keeps names, versions, and prose arrows as text", () => {
        const source = 'flowchart LR\n  A["Qwen2.5-1.5B 主干<br/>冻结 + LoRA r=32 α=64<br/>load user_id"]';
        const next = typesetMermaidLabelMath(source);
        expect(next).toContain("Qwen2.5-1.5B");
        expect(next).toContain("user_id");
        expect(next).toContain("冻结 + LoRA");
        expect(drawn(next)).not.toContain("r=32");
        expect(drawn(next)).not.toContain("α=64");
        expect(quotes(next)).toBe(quotes(source));
    });

    it("typesets an explicit formula without leaving the dollar marks", () => {
        const source = 'flowchart LR\n  A["门 $$p_{safe} \\ge t^{*}$$"]';
        const next = typesetMermaidLabelMath(source);
        expect(next).toContain("class='katex'");
        expect(next).not.toContain("$$");
        expect(next).toContain("门");
        expect(quotes(next)).toBe(quotes(source));
    });

    it("is stable when applied twice and still parses", async () => {
        const once = typesetMermaidLabelMath(LOSS);
        expect(typesetMermaidLabelMath(once)).toBe(once);
        mermaid.initialize({ startOnLoad: false, securityLevel: "strict", suppressErrorRendering: true });
        await expect(mermaid.parse(once)).resolves.toBeTruthy();
    });

    it("leaves a diagram with no quoted labels unchanged", () => {
        const source = "flowchart LR\n  A --> B";
        expect(typesetMermaidLabelMath(source)).toBe(source);
    });

    it("keeps the typeset formula in the drawn label", async () => {
        const box = () => ({ x: 0, y: 0, width: 180, height: 28, top: 0, left: 0, right: 180, bottom: 28 });
        // jsdom has no layout: stub the SVG text metrics these prototypes lack.
        const svgProto = SVGElement.prototype as unknown as Record<string, unknown>;
        svgProto.getBBox = box;
        svgProto.getComputedTextLength = () => 40;
        Element.prototype.getBoundingClientRect = box as unknown as () => DOMRect;
        mermaid.initialize({
            startOnLoad: false,
            securityLevel: "strict",
            legacyMathML: true,
            forceLegacyMathML: true,
            suppressErrorRendering: true,
        });
        const { svg } = await mermaid.render("math-probe", typesetMermaidLabelMath('flowchart LR\n  A["L = L_CE"]'));
        // Mermaid reserializes the injected markup, so attribute quotes come back as `"`.
        // KaTeX MathML keeps `L_{CE}` as the accessible fallback; the drawn glyph is `msupsub`.
        expect(svg).toContain('class="katex"');
        expect(svg).toContain("msupsub");
        expect(svg).toContain("<msub>");
        expect(svg).toContain('data-plain="L = L_CE"');
        expect(svg).not.toContain("white-space:nowrap");
    });

    it("typesets comparisons and a numeric power", () => {
        const source = 'flowchart LR\n  A["p < t* and n>1 and 10^6"]';
        const next = typesetMermaidLabelMath(source);
        expect(next).toContain("data-plain='p &lt; t*'");
        expect(next).toContain("data-plain='n&gt;1'");
        expect(next).toContain("data-plain='10^6'");
        expect(drawn(next)).not.toContain("10^6");
        expect(drawn(next)).not.toContain("t*");
    });

    it("keeps a long loss name inside the product", () => {
        const source = 'flowchart TD\n  SUM["L = L_CE + 0.5·InfoNCE + 0.2·SupCon"]\n  P["padding-aware<br/>in-batch<br/>MLP+GELU<br/>Gemma-2-9B"]';
        const next = typesetMermaidLabelMath(source);
        expect(next).toContain("data-plain='L = L_CE + 0.5·InfoNCE + 0.2·SupCon'");
        expect(next).toContain("mathrm");
        expect(next).toContain("padding-aware");
        expect(next).toContain("in-batch");
        expect(next).toContain("MLP+GELU");
        expect(next).toContain("Gemma-2-9B");
        expect(drawn(next)).not.toContain("0.5·InfoNCE");
    });

    it("typesets a spaced unary minus", () => {
        const source = 'flowchart LR\n  A["L = - L_CE"]';
        const next = typesetMermaidLabelMath(source);
        expect(next).toContain("data-plain='L = - L_CE'");
        expect(drawn(next)).not.toContain("L_CE");
    });

    it("typesets greek subscripts, nested calls, and a leading relation", async () => {
        const source = [
            "flowchart TD",
            '  A["x → τ_g(x)"]',
            '  B["不变性：d(z(x), z(τ_g x)) ≈ 0"]',
            '  C["类标签 y ∈ {safe,JB,INJ}"]',
            '  D["可分性：类间角间隔 ≥ m"]',
            '  E["Δ性能：unseen −0.034"]',
            '  F["Δ几何：drift ↑3–7× 预测 ΔF1"]',
            '  G["同一主干编码成 z(x)"]',
            '  H["线性头 W → softmax"]',
            '  I["意图向量 z(x) ∈ R^512"]',
            '  J["干预 λ→0（消融）"]',
            '  K["语言 ℓ + 扰动 g"]',
            '  L["A → B"]',
            '  M["→ 五语翻译"]',
            '  N["同一主干编码 → z(x)"]',
        ].join("\n");
        const next = typesetMermaidLabelMath(source);
        expect(next).toContain("data-plain='x → τ_g(x)'");
        expect(next).toContain("data-plain='λ→0'");
        expect(next).toContain("data-plain='ℓ'");
        expect(next).toContain("data-plain='d(z(x), z(τ_g x)) ≈ 0'");
        expect(next).toContain("data-plain='y ∈ {safe,JB,INJ}'");
        expect(next).toContain("data-plain='≥ m'");
        expect(next).toContain("data-plain='−0.034'");
        expect(next).toContain("data-plain='ΔF1'");
        expect(next).toContain("data-plain='z(x) ∈ R^512'");
        expect(next).toContain("3–7×");
        expect(next).toContain("编码成 z(x)");
        expect(next).toContain("W → softmax");
        expect(next).toContain("A → B");
        expect(next).toContain("→ 五语翻译");
        expect(next).toContain("编码 → z(x)");
        expect(next).toContain("+ 扰动 g");
        expect(next).toContain("（消融）");
        expect(next).toContain("性能");
        expect(drawn(next)).not.toContain("τ_g");
        expect(drawn(next)).not.toContain("λ→0");
        expect(drawn(next)).not.toContain("≥ m");
        expect(quotes(next)).toBe(quotes(source));
        expect(typesetMermaidLabelMath(next)).toBe(next);
        mermaid.initialize({ startOnLoad: false, securityLevel: "strict", suppressErrorRendering: true });
        await expect(mermaid.parse(next)).resolves.toBeTruthy();
    });

    it("keeps a short word in front of a slash and still typesets the relation", () => {
        const source = 'flowchart LR\n  A["focal CE / τ=0.07"]';
        const next = typesetMermaidLabelMath(source);
        expect(next).toContain("focal CE / ");
        expect(drawn(next)).not.toContain("τ=0.07");
        expect(next).toContain("data-plain='τ=0.07'");
    });

    it("typesets a leading minus after a relation", () => {
        const source = 'flowchart LR\n  A["τ=-0.07 · x=-L_CE"]';
        const next = typesetMermaidLabelMath(source);
        expect(next).toContain("data-plain='τ=-0.07 · x=-L_CE'");
        expect(drawn(next)).not.toContain("τ=-0.07");
        expect(drawn(next)).not.toContain("L_CE");
    });

    it("leaves click targets and label urls literal", () => {
        const source = [
            "flowchart LR",
            '  click A "https://ex.test/a?id=1" "open id=2"',
            '  A["docs https://ex.test/a?id=1 plus L_CE"]',
        ].join("\n");
        const next = typesetMermaidLabelMath(source);
        expect(next).toContain('click A "https://ex.test/a?id=1" "open id=2"');
        expect(next).toContain("https://ex.test/a?id=1 plus");
        expect(drawn(next)).not.toContain("L_CE");
        expect(quotes(next)).toBe(quotes(source));
    });

    it("paints workflow katex glyphs with the node ink", () => {
        const svg = '<svg id="m-1" width="10" height="10"></svg>';
        const next = paintMermaidKatexInk(svg, "#c9c9c9");
        expect(next).toContain("#m-1 .katex{color:inherit}");
        expect(next).toContain("#m-1 .node .katex path{fill:#c9c9c9;stroke:#c9c9c9;stroke-width:0.04em}");
        expect(paintMermaidKatexInk(svg, "var(--text)")).toBe(svg);
    });

    it("prints the original formula once when a saved image flattens the label", () => {
        const label = typesetMermaidLabelMath('flowchart LR\n  A["L = L_CE + 0.5·L_InfoNCE"]');
        const inner = label.slice(label.indexOf('"') + 1, label.lastIndexOf('"'));
        const svg = `<svg id="m" width="420" height="40"><foreignObject x="0" y="0" width="400" height="28"><div>${inner}</div></foreignObject></svg>`;
        const next = svgWithRasterLabels(svg);
        expect(next).toContain("L = L_CE + 0.5·L_InfoNCE");
        expect(next.match(/L_CE/g)?.length).toBe(1);
        expect(next.match(/L_InfoNCE/g)?.length).toBe(1);
        expect(next).not.toContain("katex");
    });
});
