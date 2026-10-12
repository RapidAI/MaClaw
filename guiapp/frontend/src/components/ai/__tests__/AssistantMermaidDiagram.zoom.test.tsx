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
import { lightTheme } from "../aiAssistantPanelTheme";
import {
    AssistantMermaidDiagram,
    DEFAULT_MERMAID_ZOOM,
    MAX_MERMAID_ZOOM,
    MIN_MERMAID_ZOOM,
    clampMermaidZoom,
    scaleMermaidSvg,
    stepMermaidZoom,
} from "../AssistantMermaidDiagram";

// The shape Mermaid 11 emits for `flowchart` with the default config: the root
// carries width="100%" plus an inline max-width cap, so the drawing renders at
// its intrinsic 200px no matter how wide the chat column is. That cap is exactly
// what made small diagrams look small.
const NATURAL_WIDTH = 200;

vi.mock("mermaid", () => ({
    default: {
        initialize: () => undefined,
        render: async () => ({
            svg: '<svg id="mermaid-1" width="100%" xmlns="http://www.w3.org/2000/svg" class="flowchart" style="max-width: 200px;" viewBox="0 0 200 100" role="graphics-document document"><g><rect width="200" height="100" /></g></svg>',
        }),
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
    it("renders the diagram one step above natural size, without the max-width cap", async () => {
        const diagram = await renderDiagram();

        // Default is deliberately > 100%: the reader should not have to click
        // before the diagram stops looking undersized.
        expect(DEFAULT_MERMAID_ZOOM).toBeGreaterThan(1);
        expect(renderedSvg(diagram).getAttribute("width")).toBe(`${NATURAL_WIDTH * DEFAULT_MERMAID_ZOOM}px`);
        expect(renderedSvg(diagram).getAttribute("style") ?? "").not.toContain("max-width");
        expect(screen.getByTestId("assistant-mermaid-zoom-reset").textContent).toBe(`${Math.round(DEFAULT_MERMAID_ZOOM * 100)}%`);
    });

    it("grows and shrinks the rendered diagram with the + / - buttons", async () => {
        const diagram = await renderDiagram();

        fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-in"));
        await waitFor(() => expect(renderedSvg(diagram).getAttribute("width")).toBe(`${NATURAL_WIDTH * 1.5}px`));
        expect(screen.getByTestId("assistant-mermaid-zoom-reset").textContent).toBe("150%");

        fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-out"));
        await waitFor(() => expect(renderedSvg(diagram).getAttribute("width")).toBe(`${NATURAL_WIDTH * DEFAULT_MERMAID_ZOOM}px`));
    });

    it("returns to the default size when the percentage label is clicked", async () => {
        const diagram = await renderDiagram();

        fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-in"));
        await waitFor(() => expect(renderedSvg(diagram).getAttribute("width")).toBe(`${NATURAL_WIDTH * 1.5}px`));

        fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-reset"));
        await waitFor(() => expect(renderedSvg(diagram).getAttribute("width")).toBe(`${NATURAL_WIDTH * DEFAULT_MERMAID_ZOOM}px`));
    });

    it("stops at the lower bound and disables the - button there", async () => {
        const diagram = await renderDiagram();

        for (let i = 0; i < 8; i += 1) {
            fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-out"));
        }
        await waitFor(() =>
            expect(renderedSvg(diagram).getAttribute("width")).toBe(`${NATURAL_WIDTH * MIN_MERMAID_ZOOM}px`),
        );
        expect((screen.getByTestId("assistant-mermaid-zoom-out") as HTMLButtonElement).disabled).toBe(true);
    });

    it("stops at the upper bound and disables the + button there", async () => {
        const diagram = await renderDiagram();

        for (let i = 0; i < 12; i += 1) {
            fireEvent.click(screen.getByTestId("assistant-mermaid-zoom-in"));
        }
        await waitFor(() =>
            expect(renderedSvg(diagram).getAttribute("width")).toBe(`${NATURAL_WIDTH * MAX_MERMAID_ZOOM}px`),
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
    it("scales both axes when Mermaid emits numeric width and height", () => {
        const svg = '<svg id="m" width="300" height="150" viewBox="0 0 300 150"><g /></svg>';
        const scaled = scaleMermaidSvg(svg, 2);
        expect(scaled).toContain('width="600px"');
        expect(scaled).toContain('height="300"');
        expect(scaled).toContain('viewBox="0 0 300 150"');
        expect(scaled.endsWith("</svg>")).toBe(true);
    });

    it("drops only the max-width cap, keeping the rest of the inline style", () => {
        const svg = '<svg width="100%" style="max-width: 240px; background-color: red;" viewBox="0 0 240 120"><g /></svg>';
        const scaled = scaleMermaidSvg(svg, 1);
        expect(scaled).toContain('width="240px"');
        expect(scaled).toContain("background-color: red");
        expect(scaled).not.toContain("max-width");
    });

    it("falls back to a percentage width when the markup declares no size", () => {
        const svg = '<svg viewBox="0 0 10 10"><g /></svg>';
        expect(scaleMermaidSvg(svg, 1.5)).toContain('width="150%"');
    });

    it("clamps the factor instead of trusting the caller", () => {
        const svg = '<svg width="100%" style="max-width: 100px;" viewBox="0 0 100 50"><g /></svg>';
        expect(scaleMermaidSvg(svg, 99)).toContain(`width="${100 * MAX_MERMAID_ZOOM}px"`);
        expect(scaleMermaidSvg(svg, Number.NaN)).toContain(`width="${100 * DEFAULT_MERMAID_ZOOM}px"`);
    });

    it("leaves markup without a root svg untouched", () => {
        expect(scaleMermaidSvg("not an svg", 2)).toBe("not an svg");
    });
});

describe("zoom helpers", () => {
    it("steps by 0.25 without floating point drift", () => {
        expect(stepMermaidZoom(DEFAULT_MERMAID_ZOOM, 1)).toBe(1.5);
        expect(stepMermaidZoom(1.5, -1)).toBe(DEFAULT_MERMAID_ZOOM);
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
