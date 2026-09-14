import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(resolve(here, "../../../App.css"), "utf8");
const sheenStart = css.indexOf("/* Live activity title:");
const sheenEnd = css.indexOf(".ai-chat-scrollbar", sheenStart);
const sheenCss = css.slice(sheenStart, sheenEnd);

describe("assistant reasoning live sheen", () => {
    it("sweeps a highlight through live title glyphs without restyling the row", () => {
        expect(sheenStart).toBeGreaterThanOrEqual(0);
        expect(sheenEnd).toBeGreaterThan(sheenStart);
        expect(sheenCss).toContain(".assistant-reasoning-live-label");
        expect(sheenCss).toContain("-webkit-mask-image: linear-gradient");
        expect(sheenCss).toContain("animation: assistant-reasoning-shimmer");
        expect(sheenCss).not.toContain("color-mix");
        expect(sheenCss).not.toContain("assistant-reasoning-live-label::after");
        expect(sheenCss).not.toMatch(/background:\s*#cfd8e4/);
    });

    it("keeps a static label when motion is reduced", () => {
        expect(sheenCss).toMatch(
            /@media \(prefers-reduced-motion: reduce\) \{[\s\S]*?-webkit-mask-image: none/,
        );
        expect(sheenCss).toMatch(
            /@media \(prefers-reduced-motion: reduce\) \{[\s\S]*?animation: none/,
        );
    });
});
