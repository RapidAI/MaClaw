import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const srcRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const markdownCss = readFileSync(resolve(srcRoot, "styles/partials/90-im-channels-markdown.css"), "utf8");

function ruleBody(selector: string): string {
    const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const match = markdownCss.match(new RegExp(escaped + "\\s*\\{([^}]*)\\}"));
    expect(match, selector).toBeTruthy();
    return match![1];
}

describe("mermaid diagram centering", () => {
    it("centres a zoomed-out chat diagram instead of pinning it to the left", () => {
        // The zoom is the root svg's width as a percentage of the column, so
        // anything under 100% leaves empty space beside the drawing. Without
        // auto margins that space all lands on the right.
        const body = ruleBody(".amd-diagram > svg");
        expect(body).toContain("display: block");
        expect(body).toContain("margin-left: auto");
        expect(body).toContain("margin-right: auto");
    });

    it("keeps the chat diagram box itself full width", () => {
        // Centring has to happen on the drawing, not on its box: the box is the
        // percentage's reference, so shrinking it would shrink the diagram again.
        expect(ruleBody(".amd-diagram")).toContain("max-width: 100%");
        expect(ruleBody(".amd-figure")).toContain("width: 100%");
    });

    it("centres the document-preview diagram too", () => {
        // Mermaid caps this surface's svg at its own pixel width, so a compact
        // drawing sits at the left of a wide preview column. Auto margins only
        // centre a block-level box, hence display: block next to them.
        const body = ruleBody(".wdp-diagram > svg");
        expect(body).toContain("display: block");
        expect(body).toContain("margin-left: auto");
        expect(body).toContain("margin-right: auto");
    });
});
