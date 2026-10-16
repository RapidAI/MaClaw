/**
 * The pixel size an exported diagram is drawn at.
 *
 * Sibling file: AssistantMermaidDiagram.exportSize.test.tsx covers reading the
 * size out of the SVG and preparing the source for export. This one covers what
 * is done with that size — how large to draw the picture.
 *
 * Only the sizing rule is checked here. Turning the SVG into pixels needs a
 * real canvas, which jsdom does not provide, so the drawing itself is exercised
 * through the export flow instead (FileCompanionWindow.test.tsx stubs the
 * renderer). What this file pins is the decision the component owns: how large
 * to draw, given the diagram's own size.
 *
 * The reason it matters: the document layer stretches every picture to the
 * width of the text column. A diagram sized for the screen — twice a small one,
 * say — is then interpolated up on paper and reads soft.
 */
import { describe, expect, it } from "vitest";
import { mermaidExportCanvasSize } from "../AssistantMermaidDiagram";

describe("mermaidExportCanvasSize", () => {
    it("draws a small diagram at print size rather than twice its screen size", () => {
        const size = mermaidExportCanvasSize(200, 100);
        // Twice 200 would be 400, which stretched across an A4 text column is
        // roughly 120dpi. The target is well past that.
        expect(size).not.toBeNull();
        expect(size!.width).toBeGreaterThanOrEqual(1800);
        // Aspect ratio is what the diagram looks like; it must not change.
        expect(size!.width / size!.height).toBeCloseTo(2, 1);
    });

    it("keeps a large diagram inside the ceiling", () => {
        const size = mermaidExportCanvasSize(4000, 3000);
        expect(size).not.toBeNull();
        expect(Math.max(size!.width, size!.height)).toBeLessThanOrEqual(2600);
        expect(size!.width / size!.height).toBeCloseTo(4 / 3, 1);
    });

    it("never draws a diagram below twice its own size", () => {
        // A wide, short diagram: the longest edge is already past the target,
        // so the ceiling applies, but the result is still an enlargement.
        const size = mermaidExportCanvasSize(1000, 120);
        expect(size).not.toBeNull();
        expect(size!.width).toBeGreaterThan(1000);
    });

    it("refuses a size that cannot be drawn", () => {
        expect(mermaidExportCanvasSize(0, 100)).toBeNull();
        expect(mermaidExportCanvasSize(100, 0)).toBeNull();
        expect(mermaidExportCanvasSize(-4, 10)).toBeNull();
        expect(mermaidExportCanvasSize(Number.NaN, 10)).toBeNull();
    });
});
