/**
 * Zoom controls on inline Mermaid diagrams.
 *
 * jsdom implements no SVG layout engine, so Mermaid's real renderer always fails
 * here and the component stops at its documented error fallback — which means
 * the real module can never produce a diagram for the +/- controls to act on.
 * This file mocks the "mermaid" module with a realistic SVG string; that is what
 * makes the button → zoom → rendered-markup path observable at all.
 *
 * What is NOT covered here: whether a browser lays the zoomed markup out at that
 * size. The assertions are about the width/height the component hands to the
 * browser, which is the part the component owns.
 */
import React from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { darkTheme, lightTheme, relativeLuminance } from "../aiAssistantPanelTheme";
import {
    applyMermaidDiagramCitation,
    AssistantMermaidDiagram,
    DEFAULT_MERMAID_ZOOM,
    MAX_MERMAID_ZOOM,
    MIN_MERMAID_ZOOM,
    clampMermaidZoom,
    getMermaidTheme,
    mermaidHairlineStroke,
    mermaidMenuOpensUpward,
    mermaidSvgSize,
    reinforceMermaidSvg,
    scaleMermaidSvg,
    stepMermaidZoom,
} from "../AssistantMermaidDiagram";

// The shape Mermaid 11 emits for `flowchart` with the default config: the root
// carries width="100%" plus an inline max-width cap. The cap is Mermaid's
// user-unit width. The component must not copy it into the viewport — 100%
// means the reading column, and the viewBox (200×100 here) is the aspect.
const NATURAL_WIDTH = 200;

function zoomWidth(zoom: number): string {
    return `${Math.round(zoom * 100)}%`;
}

vi.mock("mermaid", () => ({
    default: {
        initialize: () => undefined,
        render: async (_id: string, _text: string, container?: Element) => {
            // Simulate Mermaid appending a scratch node to the render host it is
            // handed. The component must clear it after capturing the markup.
            if (container) {
                const probe = document.createElement("span");
                probe.setAttribute("data-mermaid-render-scratch", "1");
                container.appendChild(probe);
            }
            return {
                svg: '<svg id="mermaid-1" width="100%" xmlns="http://www.w3.org/2000/svg" class="flowchart" style="max-width: 200px;" viewBox="0 0 200 100" role="graphics-document document"><g><rect width="200" height="100" /></g></svg>',
            };
        },
    },
}));

async function renderDiagram() {
    render(<AssistantMermaidDiagram code={"flowchart TD\n  A --> B"} theme={lightTheme} />);
    return await screen.findByTestId("assistant-mermaid-diagram");
}

function renderedSvg(diagram: HTMLElement): SVGElement {
    const svg = diagram.querySelector("svg");
    if (!svg) throw new Error("diagram rendered without an svg");
    return svg;
}

describe("inline Mermaid zoom controls", () => {
    it("renders the diagram at the column width, without the max-width cap", async () => {
        const diagram = await renderDiagram();

        // 100% is the reading column. A multiple of Mermaid's user units left
        // the drawing at its intrinsic size and the rest of the column empty.
        expect(DEFAULT_MERMAID_ZOOM).toBe(1);
        const svg = renderedSvg(diagram);
        expect(svg.getAttribute("width")).toBe(zoomWidth(DEFAULT_MERMAID_ZOOM));
        expect(svg.getAttribute("style") ?? "").not.toContain("max-width");
        expect(svg.getAttribute("style") ?? "").toContain(`aspect-ratio: ${NATURAL_WIDTH} / 100`);
        expect(svg.getAttribute("style") ?? "").toContain("height: auto");
        expect(screen.getByTestId("assistant-mermaid-zoom-reset").textContent).toBe(`${Math.round(DEFAULT_MERMAID_ZOOM * 100)}%`);
    });

    it("grows and shrinks the rendered diagram with the + / - buttons", async () => {
        const diagram = await renderDiagram();

        fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-in"));
        await waitFor(() => expect(renderedSvg(diagram).getAttribute("width")).toBe(zoomWidth(1.25)));
        expect(screen.getByTestId("assistant-mermaid-zoom-reset").textContent).toBe("125%");

        fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-out"));
        await waitFor(() => expect(renderedSvg(diagram).getAttribute("width")).toBe(zoomWidth(DEFAULT_MERMAID_ZOOM)));
    });

    it("returns to the default size when the percentage label is clicked", async () => {
        const diagram = await renderDiagram();

        fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-in"));
        await waitFor(() => expect(renderedSvg(diagram).getAttribute("width")).toBe(zoomWidth(1.25)));

        fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-reset"));
        await waitFor(() => expect(renderedSvg(diagram).getAttribute("width")).toBe(zoomWidth(DEFAULT_MERMAID_ZOOM)));
    });

    it("stops at the lower bound and disables the - button there", async () => {
        const diagram = await renderDiagram();

        for (let i = 0; i < 8; i += 1) {
            fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-out"));
        }
        await waitFor(() =>
            expect(renderedSvg(diagram).getAttribute("width")).toBe(zoomWidth(MIN_MERMAID_ZOOM)),
        );
        expect((screen.getByTestId("assistant-mermaid-zoom-out") as HTMLButtonElement).disabled).toBe(true);
    });

    it("stops at the upper bound and disables the + button there", async () => {
        const diagram = await renderDiagram();

        for (let i = 0; i < 12; i += 1) {
            fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-in"));
        }
        await waitFor(() =>
            expect(renderedSvg(diagram).getAttribute("width")).toBe(zoomWidth(MAX_MERMAID_ZOOM)),
        );
        expect((screen.getByTestId("assistant-mermaid-zoom-in") as HTMLButtonElement).disabled).toBe(true);
    });

    it("labels the controls for assistive tech", async () => {
        await renderDiagram();

        // lang is optional on this component; with no lang the i18n helper
        // resolves to Simplified Chinese, the product's default surface.
        expect(screen.getByTestId("assistant-mermaid-zoom-in").getAttribute("aria-label")).toBe("放大图表");
        expect(screen.getByTestId("assistant-mermaid-zoom-out").getAttribute("aria-label")).toBe("缩小图表");
        expect(screen.getByTestId("assistant-mermaid-zoom-reset").getAttribute("aria-label")).toBe("重置图表大小");
    });
});

describe("scaleMermaidSvg", () => {
    it("sizes the viewport as a percentage of the column, locked to the viewBox", () => {
        const svg = '<svg id="m" width="300" height="150" viewBox="0 0 300 150"><g /></svg>';
        const scaled = scaleMermaidSvg(svg, 2);
        // Zoom 2 is twice the column, not twice Mermaid's pixel width. A pixel
        // height attribute would fight that percentage and is dropped.
        expect(scaled).toContain('width="200%"');
        expect(scaled).not.toContain('height="');
        expect(scaled).toContain("aspect-ratio: 300 / 150");
        expect(scaled).toContain("height: auto");
        expect(scaled).toContain('viewBox="0 0 300 150"');
        expect(scaled.endsWith("</svg>")).toBe(true);
    });

    it("drops only the max-width cap, keeping the rest of the inline style", () => {
        const svg = '<svg width="100%" style="max-width: 240px; background-color: red;" viewBox="0 0 240 120"><g /></svg>';
        const scaled = scaleMermaidSvg(svg, 1);
        expect(scaled).toContain('width="100%"');
        expect(scaled).toContain("aspect-ratio: 240 / 120");
        expect(scaled).toContain("background-color: red");
        expect(scaled).not.toContain("max-width");
    });

    it("falls back to a percentage width when the markup declares no size", () => {
        const svg = '<svg viewBox="0 0 10 10"><g /></svg>';
        expect(scaleMermaidSvg(svg, 1.5)).toContain('width="150%"');
        expect(scaleMermaidSvg(svg, 1.5)).toContain("aspect-ratio: 10 / 10");
    });

    it("clamps the factor instead of trusting the caller", () => {
        const svg = '<svg width="100%" style="max-width: 100px;" viewBox="0 0 100 50"><g /></svg>';
        expect(scaleMermaidSvg(svg, 99)).toContain(`width="${100 * MAX_MERMAID_ZOOM}%"`);
        expect(scaleMermaidSvg(svg, Number.NaN)).toContain(`width="${100 * DEFAULT_MERMAID_ZOOM}%"`);
        expect(scaleMermaidSvg(svg, 1)).not.toContain("max-width");
    });

    it("leaves markup without a root svg untouched", () => {
        expect(scaleMermaidSvg("not an svg", 2)).toBe("not an svg");
    });
});

describe("zoom helpers", () => {
    it("steps by 0.25 without floating point drift", () => {
        expect(stepMermaidZoom(1.25, 1)).toBe(1.5);
        expect(stepMermaidZoom(1.5, -1)).toBe(1.25);
        expect(stepMermaidZoom(DEFAULT_MERMAID_ZOOM, 1)).toBe(1.25);
        // 0.25 is not exactly representable; an unrounded sum drifts after a few
        // presses and the percentage label stops showing round numbers.
        let zoom = MIN_MERMAID_ZOOM;
        for (let i = 0; i < 4; i += 1) zoom = stepMermaidZoom(zoom, 1);
        expect(zoom).toBe(1.5);
    });

    it("clamps to the configured bounds", () => {
        expect(clampMermaidZoom(0.01)).toBe(MIN_MERMAID_ZOOM);
        expect(clampMermaidZoom(100)).toBe(MAX_MERMAID_ZOOM);
        expect(clampMermaidZoom(Number.NaN)).toBe(DEFAULT_MERMAID_ZOOM);
        expect(stepMermaidZoom(MIN_MERMAID_ZOOM, -1)).toBe(MIN_MERMAID_ZOOM);
        expect(stepMermaidZoom(MAX_MERMAID_ZOOM, 1)).toBe(MAX_MERMAID_ZOOM);
    });
});

describe("scaleMermaidSvg sizing robustness", () => {
    it("locks the box to the viewBox and drops the intrinsic size cap", () => {
        const svg = '<svg width="100%" style="max-width: 400px; height: 200px;" viewBox="0 0 400 200"><g /></svg>';
        const scaled = scaleMermaidSvg(svg, 1.5);
        expect(scaled).toContain('width="150%"');
        expect(scaled).toContain("aspect-ratio: 400 / 200");
        expect(scaled).toContain("height: auto");
        expect(scaled).toContain('viewBox="0 0 400 200"');
        expect(scaled).not.toContain("max-width");
        expect(scaled).not.toContain("height: 200px");
        expect(scaled).not.toContain('height="');
    });

    it("keeps non-sizing inline style declarations intact", () => {
        const svg = '<svg width="100%" style="max-width: 240px; background-color: red; font-size: 12px;" viewBox="0 0 240 120"><g /></svg>';
        const scaled = scaleMermaidSvg(svg, 1);
        expect(scaled).toContain("background-color: red");
        expect(scaled).toContain("font-size: 12px");
        expect(scaled).not.toContain("max-width");
    });
});

describe("scaleMermaidSvg edge cases", () => {
    it("scales correctly when content precedes the <svg> tag", () => {
        // A wrapper may emit an XML declaration or comment before the root tag.
        // The old anchored regex returned the markup unscaled in that case.
        const svg = '<?xml version="1.0" encoding="UTF-8"?>\n<svg width="100%" style="max-width: 320px;" viewBox="0 0 320 160"><g /></svg>';
        const scaled = scaleMermaidSvg(svg, 1.5);
        expect(scaled).toContain('width="150%"');
        expect(scaled).toContain("aspect-ratio: 320 / 160");
        expect(scaled).toContain('viewBox="0 0 320 160"');
        expect(scaled.startsWith("<?xml")).toBe(true);
    });

    it("does not misread a percentage width as a pixel natural size", () => {
        // width="100%" with no max-width cap still has a viewBox. The viewport
        // is a percentage of the column, never 125px from a misread "100%".
        const svg = '<svg width="100%" viewBox="0 0 400 200"><g /></svg>';
        const scaled = scaleMermaidSvg(svg, 1.25);
        expect(scaled).toContain('width="125%"');
        expect(scaled).toContain("aspect-ratio: 400 / 200");
        expect(scaled).not.toContain("125px");
    });

    it("replaces a sizing-only style with the viewport lock", () => {
        const svg = '<svg width="100%" style="max-width: 240px; height: 120px;" viewBox="0 0 240 120"><g /></svg>';
        const scaled = scaleMermaidSvg(svg, 1);
        expect(scaled).not.toContain("max-width");
        expect(scaled).not.toContain("height: 120px");
        expect(scaled).toContain("aspect-ratio: 240 / 120");
        expect(scaled).toContain('width="100%"');
    });
});

describe("mermaidSvgSize", () => {
    it("reads a numeric width and height", () => {
        expect(mermaidSvgSize('<svg width="300" height="150" viewBox="0 0 300 150"><g /></svg>')).toEqual({ width: 300, height: 150 });
    });

    it("falls back to the viewBox when the width is a percentage", () => {
        // Mermaid's default output has width="100%"; treating that as 100px would
        // distort the exported PNG's aspect ratio.
        const size = mermaidSvgSize('<svg width="100%" height="200" viewBox="0 0 400 200"><g /></svg>');
        expect(size).toEqual({ width: 400, height: 200 });
    });

    it("returns null for markup without a root svg", () => {
        expect(mermaidSvgSize("not an svg")).toBeNull();
    });

    it("reads size when content precedes the <svg> tag", () => {
        // A wrapper may emit an XML declaration or comment before the root tag;
        // the anchored regex returned null there and silently degraded the export
        // to a source-code block.
        const svg = '<?xml version="1.0" encoding="UTF-8"?>\n<svg width="100%" height="200" viewBox="0 0 400 200"><g /></svg>';
        expect(mermaidSvgSize(svg)).toEqual({ width: 400, height: 200 });
    });

    it("reads px-unit width and height attributes", () => {
        // Parity with scaleMermaidSvg, which already tolerates px units. Without
        // this the export path would reject "200px" and silently fall back to the
        // viewBox, diverging from what the reader saw on screen.
        expect(mermaidSvgSize('<svg width="200px" height="100px" viewBox="0 0 300 150"><g /></svg>')).toEqual({ width: 200, height: 100 });
    });
});

describe("render host hygiene", () => {
    it("clears the measure host after a successful render", async () => {
        const { container } = render(
            <AssistantMermaidDiagram code={"flowchart TD\n  A --> B"} theme={lightTheme} />,
        );
        await screen.findByTestId("assistant-mermaid-diagram");
        const host = container.querySelector(".amd-measure-host");
        expect(host).not.toBeNull();
        // The mock appended a scratch node during render; the component must have
        // dropped it once it captured the markup, so the host stays empty and does
        // not accumulate across re-renders.
        expect(host!.childElementCount).toBe(0);
        const diagram = screen.getByTestId("assistant-mermaid-diagram");
        expect(diagram.style.getPropertyValue("--amd-ink")).toBe(getMermaidTheme(lightTheme).themeVariables.primaryTextColor);
        expect(diagram.style.getPropertyValue("--amd-line")).toBe(getMermaidTheme(lightTheme).themeVariables.lineColor);
        expect(diagram.style.getPropertyValue("--amd-paper")).toBe(lightTheme.fieldBg);
        expect(diagram.style.getPropertyValue("--amd-chip")).toBe(getMermaidTheme(lightTheme).themeVariables.secondaryColor);
    });
});

describe("zoom lifecycle", () => {
    it("returns to the default size when the diagram code changes", async () => {
        const { rerender } = render(
            <AssistantMermaidDiagram code={"flowchart TD\n  A --> B"} theme={lightTheme} lang="en" />,
        );
        const diagram = await screen.findByTestId("assistant-mermaid-diagram");

        fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-in"));
        await waitFor(() => expect(renderedSvg(diagram).getAttribute("width")).toBe(zoomWidth(1.25)));

        // New diagram content reuses the instance, so the reader's previous
        // zoom must not leak onto a different picture. Re-query the live node
        // each tick: the code change briefly unmounts the figure (loading) and
        // remounts it, so a cached reference would read a detached svg.
        rerender(<AssistantMermaidDiagram code={"sequenceDiagram\n  A->>B: hi"} theme={lightTheme} lang="en" />);
        await waitFor(() =>
            expect(renderedSvg(screen.getByTestId("assistant-mermaid-diagram")).getAttribute("width"))
                .toBe(zoomWidth(DEFAULT_MERMAID_ZOOM)),
        );
    });

    it("keeps zoom and offers the diagram menu", async () => {
        render(<AssistantMermaidDiagram code={"flowchart TD\n  A --> B"} theme={lightTheme} />);
        expect(await screen.findByTestId("assistant-mermaid-zoom-in")).toBeTruthy();
        expect(screen.getByTestId("assistant-mermaid-cite").textContent).toBe("添加到对话");
        expect(screen.getByTestId("assistant-mermaid-download").textContent).toBe("下载到本地");
        fireEvent.click(screen.getByTestId("assistant-mermaid-more"));
        const menu = screen.getByRole("menu");
        expect(menu.textContent).toContain("保存为图片");
        expect(menu.textContent).toContain("复制代码");
        expect(menu.textContent).toContain("查看代码");
        fireEvent.click(screen.getByTestId("assistant-mermaid-view-code"));
        expect(screen.getByTestId("assistant-mermaid-source").textContent).toContain("flowchart TD");
        fireEvent.click(screen.getByTestId("assistant-mermaid-more"));
        expect(screen.getByTestId("assistant-mermaid-view-code").textContent).toBe("隐藏代码");
        fireEvent.click(screen.getByTestId("assistant-mermaid-view-code"));
        expect(screen.queryByTestId("assistant-mermaid-source")).toBeNull();
    });

    it("quotes a diagram into the composer without wiping typed text", () => {
        const source = "flowchart TD\n  A --> B";
        expect(applyMermaidDiagramCitation(source, "zh", "")).toContain("```mermaid\n" + source);
        expect(applyMermaidDiagramCitation(source, "zh", "把接入层拆开")).toBe(
            "把接入层拆开\n\n请基于这张图继续：\n```mermaid\n" + source + "\n```\n",
        );
        const quoted = applyMermaidDiagramCitation(source, "zh", "```mermaid\n" + source + "\n```");
        expect(quoted).toBe("```mermaid\n" + source + "\n```");
        const crlf = ("```mermaid\n" + source + "\n```").replace(/\n/g, "\r\n");
        expect(applyMermaidDiagramCitation(source, "zh", crlf)).toBe(crlf);
        expect(applyMermaidDiagramCitation(source, "zh", "图里有 A --> B")).toContain("请基于这张图继续：");
    });

    it("labels the controls in the UI language when lang is supplied", async () => {
        render(<AssistantMermaidDiagram code={"flowchart TD\n  A --> B"} theme={lightTheme} lang="en" />);
        expect((await screen.findByTestId("assistant-mermaid-zoom-in")).getAttribute("aria-label")).toBe("Zoom in");
        expect((await screen.findByTestId("assistant-mermaid-zoom-out")).getAttribute("aria-label")).toBe("Zoom out");
        expect((await screen.findByTestId("assistant-mermaid-zoom-reset")).getAttribute("aria-label")).toBe("Reset diagram size");
    });

    it("localizes the diagram figure label to the UI language", async () => {
        render(<AssistantMermaidDiagram code={"flowchart TD\n  A --> B"} theme={lightTheme} lang="en" />);
        expect((await screen.findByTestId("assistant-mermaid-diagram")).getAttribute("aria-label")).toBe("Mermaid diagram");
    });

    it("falls back to Simplified Chinese for the diagram figure label when lang is omitted", async () => {
        render(<AssistantMermaidDiagram code={"flowchart TD\n  A --> B"} theme={lightTheme} />);
        expect((await screen.findByTestId("assistant-mermaid-diagram")).getAttribute("aria-label")).toBe("Mermaid 图表");
    });
});

describe("mermaidMenuOpensUpward", () => {
    it("opens upward when the card fits above and the space below is shorter", () => {
        expect(mermaidMenuOpensUpward(400, 40, 120)).toBe(true);
        expect(mermaidMenuOpensUpward(400, 200, 120)).toBe(false);
        expect(mermaidMenuOpensUpward(20, 40, 120)).toBe(false);
        expect(mermaidMenuOpensUpward(80, 40, 120)).toBe(true);
        expect(mermaidMenuOpensUpward(400, 40, 0)).toBe(false);
    });
});

describe("mermaidHairlineStroke", () => {
    it("shrinks a border that the column would stretch past one pixel", () => {
        expect(mermaidHairlineStroke(400, 800)).toBe(0.5);
        expect(mermaidHairlineStroke(800, 400)).toBe(1);
        expect(mermaidHairlineStroke(0, 800)).toBe(1);
        expect(mermaidHairlineStroke(100, 2000)).toBe(0.05);
        expect(mermaidHairlineStroke(1, 10000)).toBe(0.0001);
    });
});

describe("reinforceMermaidSvg", () => {
    it("writes the chip and ink under the svg id", () => {
        const svg = '<svg id="mermaid-1" width="10" height="10"></svg>';
        const next = reinforceMermaidSvg(svg, lightTheme);
        const variables = getMermaidTheme(lightTheme).themeVariables;
        expect(next).toContain("#mermaid-1 .edgeLabel .labelBkg");
        expect(next).toContain(`background-color:${variables.secondaryColor}`);
        expect(next).toContain(`color:${variables.primaryTextColor}`);
        expect(next).toContain("opacity:1");
        expect(next).toContain('[class*="edge-depth-"]');
        expect(next).toContain(".mindmap-node line{stroke-width:1.25px}");
        expect(next).not.toContain("#mermaid-1 .edge{");
        expect(next).not.toContain("#mermaid-1 .edge,");
        expect(next).toContain("stroke-width:1px");
        expect(next).toContain("#mermaid-1 .node rect");
        expect(next).not.toContain("vector-effect");
        const hairline = reinforceMermaidSvg(svg, lightTheme, 0.5);
        expect(hairline).toContain("stroke-width:0.5px");
        expect(hairline).toContain("#mermaid-1 .transition{stroke-width:0.5px}");
        expect(hairline).toContain(".edgePath .path{stroke-width:1px}");
        expect(next.endsWith("</svg>")).toBe(true);
    });

    it("leaves a diagram without an id untouched", () => {
        const svg = '<svg width="10" height="10"></svg>';
        expect(reinforceMermaidSvg(svg, lightTheme)).toBe(svg);
    });
});

describe("getMermaidTheme", () => {
    // Mermaid's themeVariables type is string | boolean, so accept both and
    // stringify here; call sites already wrap some values in String(...).
    function spread(hex: string | boolean): number {
        const n = Number.parseInt(String(hex).slice(1), 16);
        const r = (n >> 16) & 255;
        const g = (n >> 8) & 255;
        const b = n & 255;
        return Math.max(r, g, b) - Math.min(r, g, b);
    }

    function contrast(foreground: string | boolean, background: string | boolean): number {
        const fg = relativeLuminance(String(foreground));
        const bg = relativeLuminance(String(background));
        if (fg == null || bg == null) return 0;
        const lighter = Math.max(fg, bg);
        const darker = Math.min(fg, bg);
        return (lighter + 0.05) / (darker + 0.05);
    }

    it("seeds a chromatic fill and lets Mermaid derive the other hues", () => {
        const light = getMermaidTheme(lightTheme).themeVariables;
        const dark = getMermaidTheme({ ...darkTheme, isDark: true }).themeVariables;
        expect(light.primaryColor).toMatch(/^#[0-9a-f]{6}$/);
        expect(spread(light.primaryColor)).toBeGreaterThan(24);
        expect(spread(dark.primaryColor)).toBeGreaterThan(24);
        expect(light.primaryColor).not.toBe(lightTheme.fieldBg);
        expect(light).not.toHaveProperty("mainBkg");
        expect(light).not.toHaveProperty("clusterBkg");
        expect(spread(String(light.secondaryColor))).toBeGreaterThan(24);
        expect(spread(String(light.tertiaryColor))).toBeGreaterThan(24);
        expect(spread(String(dark.secondaryColor))).toBeGreaterThan(24);
        expect(spread(String(dark.tertiaryColor))).toBeGreaterThan(24);
        expect(contrast(light.primaryTextColor, String(light.secondaryColor))).toBeGreaterThanOrEqual(4.5);
        expect(contrast(light.primaryTextColor, String(light.tertiaryColor))).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.primaryTextColor, String(dark.secondaryColor))).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.primaryTextColor, String(dark.tertiaryColor))).toBeGreaterThanOrEqual(4.5);
        expect(light.edgeLabelBackground).toBe(light.secondaryColor);
        expect(dark.edgeLabelBackground).toBe(dark.secondaryColor);
        expect(light.secondaryColor).not.toBe(light.primaryColor);
        expect(light.tertiaryColor).not.toBe(light.primaryColor);
        const channels = (hex: string) => ({
            r: Number.parseInt(hex.slice(1, 3), 16),
            g: Number.parseInt(hex.slice(3, 5), 16),
            b: Number.parseInt(hex.slice(5, 7), 16),
        });
        for (const sample of [light, dark]) {
            const node = channels(String(sample.primaryColor));
            const group = channels(String(sample.tertiaryColor));
            const chip = channels(String(sample.secondaryColor));
            expect(node.b).toBeGreaterThan(node.r);
            expect(group.b).toBeGreaterThan(group.r);
            expect(group.b).toBeGreaterThan(group.g);
            expect(chip.b).toBeGreaterThan(chip.r);
            expect(Math.abs(chip.g - chip.b)).toBeLessThan(90);
        }
        expect(relativeLuminance(String(light.tertiaryColor))).toBeGreaterThan(relativeLuminance(String(light.primaryColor)) ?? 1);
        expect(light.arrowheadColor).toBe(light.lineColor);
        expect(light.signalColor).toBe(light.lineColor);
        expect(dark.signalColor).toBe(dark.lineColor);
        expect(contrast(light.lineColor, light.primaryColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.lineColor, dark.primaryColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(light.lineColor, lightTheme.fieldBg)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.lineColor, darkTheme.fieldBg)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(light.lineColor, String(light.tertiaryColor))).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.lineColor, String(dark.tertiaryColor))).toBeGreaterThanOrEqual(4.5);
        expect(spread(String(light.lineColor))).toBeLessThanOrEqual(spread(lightTheme.fieldBg) + 2);
        expect(spread(String(dark.lineColor))).toBeLessThanOrEqual(spread(darkTheme.fieldBg) + 2);
        expect(light.primaryBorderColor).not.toBe(light.lineColor);
        expect(contrast(String(light.primaryBorderColor), String(light.primaryColor))).toBeGreaterThanOrEqual(3);
        expect(contrast(String(dark.primaryBorderColor), String(dark.primaryColor))).toBeGreaterThanOrEqual(3);
        expect(spread(String(light.primaryBorderColor))).toBeGreaterThan(24);
        expect(contrast(String(light.tertiaryBorderColor), String(light.tertiaryColor))).toBeGreaterThanOrEqual(3);
        expect(String(getMermaidTheme(lightTheme).fontFamily)).toContain("Segoe UI");
        expect(light.archEdgeColor).toBe(light.lineColor);
        expect(dark.archEdgeArrowColor).toBe(dark.lineColor);
        expect(light.archGroupBorderColor).toBe(light.lineColor);
        expect(contrast(String(light.archGroupBorderColor), lightTheme.fieldBg)).toBeGreaterThanOrEqual(3);
        expect(contrast(String(dark.archGroupBorderColor), darkTheme.fieldBg)).toBeGreaterThanOrEqual(3);
        expect(light.titleColor).toBe(light.primaryTextColor);
        expect(dark.titleColor).toBe(dark.primaryTextColor);
        expect(contrast(light.primaryTextColor, light.primaryColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.primaryTextColor, dark.primaryColor)).toBeGreaterThanOrEqual(4.5);
        expect(light.classText).toBe(light.primaryTextColor);
        expect(dark.classText).toBe(dark.primaryTextColor);
        expect(contrast(light.noteTextColor, light.noteBkgColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.noteTextColor, dark.noteBkgColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(light.sequenceNumberColor, light.lineColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.sequenceNumberColor, dark.lineColor)).toBeGreaterThanOrEqual(4.5);
        expect(light.transitionLabelColor).toBe(light.primaryTextColor);
        expect(dark.transitionLabelColor).toBe(dark.primaryTextColor);
        expect(light.altBackground).toBe(lightTheme.fieldBg);
        expect(dark.altBackground).toBe(darkTheme.fieldBg);
        expect(light.pieSectionTextColor).toBe(light.primaryTextColor);
        expect(dark.pieSectionTextColor).toBe(dark.primaryTextColor);
        expect(light.pieStrokeColor).toBe(light.lineColor);
        expect(dark.pieStrokeColor).toBe(dark.lineColor);
        expect(dark.pieOuterStrokeColor).toBe(dark.lineColor);
        expect(light.pieOpacity).toBe("1");
        expect(contrast(light.taskTextColor, light.primaryColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.taskTextColor, dark.primaryColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(light.taskTextDarkColor, light.activeTaskBkgColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.taskTextDarkColor, dark.activeTaskBkgColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(light.taskTextDarkColor, light.doneTaskBkgColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.taskTextDarkColor, dark.doneTaskBkgColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(light.activeTaskBorderColor, light.activeTaskBkgColor)).toBeGreaterThanOrEqual(3);
        expect(contrast(dark.activeTaskBorderColor, dark.activeTaskBkgColor)).toBeGreaterThanOrEqual(3);
        expect(contrast(light.doneTaskBorderColor, light.doneTaskBkgColor)).toBeGreaterThanOrEqual(3);
        expect(contrast(dark.doneTaskBorderColor, dark.doneTaskBkgColor)).toBeGreaterThanOrEqual(3);
        expect(contrast(light.taskTextColor, light.critBkgColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(dark.taskTextColor, dark.critBkgColor)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(light.critBorderColor, light.critBkgColor)).toBeGreaterThanOrEqual(3);
        expect(contrast(dark.critBorderColor, dark.critBkgColor)).toBeGreaterThanOrEqual(3);
        expect(contrast(light.gridColor, lightTheme.fieldBg)).toBeGreaterThanOrEqual(3);
        expect(contrast(dark.gridColor, darkTheme.fieldBg)).toBeGreaterThanOrEqual(3);
        expect(light.taskTextClickableColor).toBe(light.taskTextColor);
        expect(dark.taskTextClickableColor).toBe(dark.taskTextColor);
        for (const sample of [light, dark]) {
            for (const index of [0, 4, 8]) {
                const fill = String(sample[`cScale${index}`]);
                const label = String(sample[`cScaleLabel${index}`]);
                expect(fill).toMatch(/^#[0-9a-f]{6}$/);
                expect(spread(fill)).toBeGreaterThan(24);
                expect(contrast(label, fill)).toBeGreaterThanOrEqual(4.5);
            }
            expect(sample.cScale0).not.toBe(sample.cScale6);
            expect(sample.git0).toBe(sample.cScale0);
            expect(sample.gitBranchLabel0).toBe(sample.cScaleLabel0);
            expect(contrast(String(sample.gitBranchLabel0), String(sample.git0))).toBeGreaterThanOrEqual(4.5);
            for (const wedge of [1, 5, 9]) {
                const fill = String(sample[`pie${wedge}`]);
                expect(fill).toMatch(/^#[0-9a-f]{6}$/);
                expect(spread(fill)).toBeGreaterThan(24);
                expect(contrast(String(sample.pieSectionTextColor), fill)).toBeGreaterThanOrEqual(4.5);
            }
            expect(sample.pie1).not.toBe(sample.pie7);
            for (const index of [0, 3, 7]) {
                const fill = String(sample[`fillType${index}`]);
                expect(fill).toMatch(/^#[0-9a-f]{6}$/);
                expect(spread(fill)).toBeGreaterThan(24);
                expect(contrast(String(sample.primaryTextColor), fill)).toBeGreaterThanOrEqual(4.5);
            }
            expect(sample.fillType0).not.toBe(sample.fillType4);
        }
        expect(relativeLuminance(String(dark.excludeBkgColor))).toBeLessThan(0.25);
        expect(light.textColor).toBe(lightTheme.text);
        expect(dark.darkMode).toBe(true);
        expect(light.darkMode).toBe(false);
    });
});
