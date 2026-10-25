import { describe, expect, it } from "vitest";
import { mermaidRasterPaper, mermaidSvgSize, prepareMermaidExportSource, svgForRaster, svgReadyForRaster, svgWithRasterLabels } from "../AssistantMermaidDiagram";

describe("mermaidSvgSize", () => {
    it("reads the viewBox when mermaid pins width to 100% (useMaxWidth default)", () => {
        // Root shape captured from a real mermaid 11.14.0 flowchart render:
        // width="100%" plus a max-width style, no numeric height attribute.
        const svg = '<svg id="t1" aria-roledescription="flowchart" viewBox="0 0 594.56 792.77" width="100%" style="max-width: 594.56px;" xmlns="http://www.w3.org/2000/svg">';
        expect(mermaidSvgSize(svg)).toEqual({ width: 594.56, height: 792.77 });
    });

    it("prefers numeric attributes when both axes state pixels", () => {
        const svg = '<svg width="480.5" height="320.25" viewBox="0 0 480.5 320.25">';
        expect(mermaidSvgSize(svg)).toEqual({ width: 480.5, height: 320.25 });
    });

    it("fills only the percentage axis from the viewBox", () => {
        const svg = '<svg width="640" height="100%" viewBox="0 0 640 480">';
        expect(mermaidSvgSize(svg)).toEqual({ width: 640, height: 480 });
    });

    it("returns null when nothing states a usable size", () => {
        expect(mermaidSvgSize('<svg width="100%" height="100%">')).toBeNull();
        expect(mermaidSvgSize("<svg>")).toBeNull();
        expect(mermaidSvgSize("not svg at all")).toBeNull();
        expect(mermaidSvgSize('<svg viewBox="0 0 0 480" width="100%">')).toBeNull();
    });

    it("writes pixel width and height before a bitmap decode", () => {
        const svg = '<svg id="t1" viewBox="0 0 594.56 792.77" width="100%" style="max-width: 594.56px; background: white" xmlns="http://www.w3.org/2000/svg"></svg>';
        const next = svgForRaster(svg);
        expect(next).toContain('width="594.56"');
        expect(next).toContain('height="792.77"');
        expect(next).not.toContain("max-width");
        expect(next).toContain("background: white");
    });
});

describe("svgWithRasterLabels", () => {
    it("turns html labels into svg text so a saved image keeps the words", () => {
        const svg = '<svg id="m" viewBox="0 0 200 80" width="200" height="80"><style>#m .nodeLabel{color:#112233}</style><foreignObject x="10" y="20" width="80" height="40"><div xmlns="http://www.w3.org/1999/xhtml"><p>接入层</p><p>网关</p></div></foreignObject></svg>';
        const next = svgWithRasterLabels(svg);
        expect(next).not.toContain("foreignObject");
        expect(next).toContain("接入层");
        expect(next).toContain("网关");
        expect(next).toContain('fill="#112233"');
        expect(next).toContain('text-anchor="middle"');
        expect(next).toContain("PingFang SC");
        expect(next).toContain("<tspan");
    });

    it("does not treat a background color as the label ink", () => {
        const svg = '<svg id="m" width="40" height="20"><style>#m .node{background-color:#ffffff;color:#222222}</style><foreignObject x="0" y="0" width="40" height="20"><div>A &amp; B</div></foreignObject></svg>';
        const next = svgWithRasterLabels(svg);
        expect(next).toContain('fill="#222222"');
        expect(next).toContain("A &amp; B");
        expect(next).toContain('y="10"');
        expect(next).not.toContain("foreignObject");
    });

    it("keeps a label transform and wraps a line that is wider than its box", () => {
        const svg = '<svg id="m" width="80" height="40"><style>.nodeLabel{color:#111111}</style><foreignObject transform="translate(10,4)" x="0" y="0" width="40" height="32"><div><p>接入层网关服务</p></div></foreignObject></svg>';
        const next = svgWithRasterLabels(svg);
        expect(next).toContain('transform="translate(10,4)"');
        expect(next.match(/<tspan/g)?.length).toBeGreaterThan(1);
    });

    it("breaks a saved label on spaces before it splits a word", () => {
        const svg = '<svg id="m" width="200" height="80"><foreignObject x="0" y="0" width="80" height="48"><div><p>Hello World</p></div></foreignObject></svg>';
        const next = svgWithRasterLabels(svg);
        expect(next).toContain(">Hello</tspan>");
        expect(next).toContain(">World</tspan>");
    });

    it("shrinks a long title until the stack fits the measured box", () => {
        const svg = '<svg id="m" width="80" height="40"><foreignObject x="0" y="0" width="36" height="28"><div><p>接入层网关服务节点</p></div></foreignObject></svg>';
        const next = svgWithRasterLabels(svg);
        const font = Number(next.match(/font-size="([0-9.]+)"/)?.[1]);
        const count = next.match(/<tspan/g)?.length ?? 1;
        expect(font).toBeGreaterThan(0);
        expect(count * font * 1.25).toBeLessThanOrEqual(28.05);
    });

    it("prints a typeset formula from data-plain instead of both KaTeX layers", () => {
        const katex = `<span class="katex" data-plain="L = L_CE"><span class="katex-mathml"><math><semantics><mrow><mi>L</mi></mrow><annotation encoding="application/x-tex">L = L_{CE}</annotation></semantics></math></span><span class="katex-html" aria-hidden="true"><span class="mord">LCE</span></span></span>`;
        const svg = `<svg id="m" width="200" height="40"><foreignObject x="0" y="0" width="180" height="28"><div>损失 ${katex}</div></foreignObject></svg>`;
        const next = svgWithRasterLabels(svg);
        expect(next).toContain("损失 L = L_CE");
        expect(next).not.toContain("L_{CE}");
        expect(next).not.toContain("LCE");
        expect(next).not.toContain("katex");
    });

    it("leaves a diagram without html labels unchanged", () => {
        const svg = '<svg id="m" width="10" height="10"><text>A</text></svg>';
        expect(svgWithRasterLabels(svg)).toBe(svg);
    });

    it("rewrites the lowercase foreignobject the html serializer emits", () => {
        const svg = '<svg id="m" width="80" height="40"><foreignobject x="0" y="0" width="80" height="40"><div><p>用户端<br>Web&nbsp;App</p></div></foreignobject></svg>';
        const next = svgWithRasterLabels(svg);
        expect(next.toLowerCase()).not.toContain("foreignobject");
        expect(next).not.toContain("<br");
        expect(next).not.toContain("&nbsp;");
        expect(next).toContain("用户端");
        expect(next).toContain("Web");
        expect(next).toContain("App");
    });

    it("drops an icon box whose html would not parse as xml", () => {
        const svg = '<svg id="m" width="20" height="20"><foreignobject width="16" height="16"><div><br><img src="x"></div></foreignobject><text>A</text></svg>';
        const next = svgWithRasterLabels(svg);
        expect(next.toLowerCase()).not.toContain("foreignobject");
        expect(next).not.toContain("<br");
        expect(next).not.toContain("<img");
        expect(next).toContain("<text>A</text>");
    });
});

describe("svgReadyForRaster", () => {
    it("emits xml an image decoder can parse", () => {
        const svg = '<svg id="m" viewBox="0 0 80 40" width="100%"><foreignobject x="0" y="0" width="16" height="16"><div><br><img src="x"></div></foreignobject><text>A&mdash;B & C</text></svg>';
        const next = svgReadyForRaster(svg);
        expect(next.toLowerCase()).not.toContain("foreignobject");
        expect(next).not.toContain("<br");
        expect(next).not.toContain("<img");
        expect(next).not.toContain("&mdash;");
        expect(next).not.toContain("& C");
        expect(next).toContain("&amp;");
        expect(next).toContain('xmlns="http://www.w3.org/2000/svg"');
        expect(next).toContain('width="80"');
        expect(next).toContain('height="40"');
        const parsed = new DOMParser().parseFromString(next, "image/svg+xml");
        expect(parsed.querySelector("parsererror")).toBeNull();
    });

    it("restores the viewBox an html serializer lowercased", () => {
        const svg = '<svg id="m" viewbox="0 0 80 40" width="100%"></svg>';
        expect(mermaidSvgSize(svg)).toEqual({ width: 80, height: 40 });
        const next = svgReadyForRaster(svg);
        expect(next).toContain('viewBox="0 0 80 40"');
        expect(next).not.toMatch(/\bviewbox=/);
        expect(next).toContain('width="80"');
        expect(next).toContain('height="40"');
    });
});

describe("mermaidRasterPaper", () => {
    it("keeps a six-digit chat surface and falls back otherwise", () => {
        expect(mermaidRasterPaper("#141414")).toBe("#141414");
        expect(mermaidRasterPaper("#f4f8fe")).toBe("#f4f8fe");
        expect(mermaidRasterPaper("#fff")).toBe("#ffffff");
        expect(mermaidRasterPaper(undefined)).toBe("#ffffff");
        expect(mermaidRasterPaper("red")).toBe("#ffffff");
    });
});

describe("prepareMermaidExportSource", () => {
    it("canonicalises diagram casing exactly like the on-screen pipeline", () => {
        // A lower-cased "sequencediagram" renders on screen (sanitize repairs it)
        // but fails mermaid detection raw — the export must go through the same
        // repair or the PDF silently degrades to a source-code block.
        expect(prepareMermaidExportSource("sequencediagram\n  A->>B: hi")).toContain("sequenceDiagram");
        expect(prepareMermaidExportSource("Graph TD\n  A --> B")).toContain("graph TD");
    });

    it("keeps a normal fence byte-for-byte", () => {
        expect(prepareMermaidExportSource("flowchart TD\n  A --> B")).toBe("flowchart TD\n  A --> B");
    });

    it("rejects over-budget sources the screen path would refuse", () => {
        const edges = Array.from({ length: 800 }, (_, i) => `n${i} --> m${i}`).join("\n");
        expect(prepareMermaidExportSource(`flowchart TD\n${edges}`)).toBeNull();
        expect(prepareMermaidExportSource("x".repeat(50_001))).toBeNull();
        expect(prepareMermaidExportSource("   ")).toBeNull();
    });
});
