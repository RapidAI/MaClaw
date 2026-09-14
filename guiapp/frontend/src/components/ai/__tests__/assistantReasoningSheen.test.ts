import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { ASSISTANT_LIVE_SHEEN_SPOT_LIGHT } from "../AssistantReasoningPanel";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(resolve(here, "../../../App.css"), "utf8");
const sheenStart = css.indexOf("/* Live activity title");
const sheenEnd = css.indexOf(".ai-chat-scrollbar", sheenStart);
const sheenCss = css.slice(sheenStart, sheenEnd);

describe("assistant reasoning live sheen", () => {
    it("sweeps a highlight through the live activity label, not the whole summary row", () => {
        expect(sheenStart).toBeGreaterThanOrEqual(0);
        expect(sheenEnd).toBeGreaterThan(sheenStart);
        expect(sheenCss).toContain(".assistant-reasoning-live-label");
        expect(sheenCss).toContain("background-clip: text");
        expect(sheenCss).toContain("-webkit-text-fill-color: transparent");
        expect(sheenCss).toContain("animation: assistant-reasoning-shimmer");
        expect(sheenCss).toContain("currentColor");
        expect(sheenCss).toContain("var(--assistant-sheen-spot)");
        expect(sheenCss).toContain(`--assistant-sheen-spot: ${ASSISTANT_LIVE_SHEEN_SPOT_LIGHT}`);
        expect(sheenCss).not.toContain("color-mix");
        expect(sheenCss).not.toContain("[data-ai-theme='dark'] .assistant-reasoning-live-label");
        expect(sheenCss).not.toMatch(/linear-gradient\([^)]*#fff 50%/);
        expect(sheenCss).not.toContain("assistant-reasoning-live-label::after");
        expect(sheenCss).not.toMatch(/\.assistant-reasoning-summary--live::after\s*\{/);
    });

    it("keeps a static label when motion or contrast is reduced", () => {
        expect(sheenCss).toMatch(
            /@media \(prefers-reduced-motion: reduce\), \(prefers-contrast: more\) \{[\s\S]*?-webkit-text-fill-color: currentColor/,
        );
        expect(sheenCss).toMatch(
            /@media \(prefers-reduced-motion: reduce\), \(prefers-contrast: more\) \{[\s\S]*?animation: none/,
        );
    });
});
