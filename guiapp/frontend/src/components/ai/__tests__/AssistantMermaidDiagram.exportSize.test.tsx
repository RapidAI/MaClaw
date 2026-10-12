import { describe, expect, it } from "vitest";
import { mermaidSvgSize, prepareMermaidExportSource } from "../AssistantMermaidDiagram";

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
