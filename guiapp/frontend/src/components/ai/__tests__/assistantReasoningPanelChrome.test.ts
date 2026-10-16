import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { reasoningPanelChrome } from "../AssistantReasoningPanel";
import { darkTheme, lightTheme } from "../aiAssistantPanelTheme";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(resolve(here, "../../../App.css"), "utf8");

describe("reasoningPanelChrome", () => {
    it("uses the scheme recessed surface, field border, and response rail", () => {
        const light = reasoningPanelChrome(lightTheme);
        expect(light.background).toBe(lightTheme.fieldBg);
        expect(light.background).not.toBe(lightTheme.bg);
        expect(light.border).toBe(`1px solid ${lightTheme.fieldBorder}`);
        expect(light.borderLeft).toBeUndefined();
        expect(light.boxShadow).toBe(`inset 2px 0 0 ${lightTheme.responseBorderLeft}`);
        expect(light.borderRadius).toBe(8);
        expect(light.color).toBe(lightTheme.textMuted);
        expect(light.boxSizing).toBe("border-box");

        const dark = reasoningPanelChrome(darkTheme);
        expect(dark.background).toBe(darkTheme.fieldBg);
        expect(dark.background).not.toBe(darkTheme.bg);
        expect(dark.background).not.toBe(darkTheme.titleBarBg);
        expect(dark.border).toBe(`1px solid ${darkTheme.fieldBorder}`);
        expect(dark.boxShadow).toBe(`inset 2px 0 0 ${darkTheme.responseBorderLeft}`);
    });
});

describe("assistant reasoning panel CSS", () => {
    it("does not let a stylesheet !important repaint the thinking panel", () => {
        expect(css).not.toMatch(/\.assistant-reasoning-panel[\s,{][^}]*!important/);
    });
});
