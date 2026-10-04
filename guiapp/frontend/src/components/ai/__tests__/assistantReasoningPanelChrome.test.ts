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
        const lightEdge = `1px solid ${lightTheme.fieldBorder}`;
        expect(light.background).toBe(lightTheme.fieldBg);
        expect(light.borderTop).toBe(lightEdge);
        expect(light.borderRight).toBe(lightEdge);
        expect(light.borderBottom).toBe(lightEdge);
        expect(light.borderLeft).toBe(`2px solid ${lightTheme.responseBorderLeft}`);
        expect(light.border).toBeUndefined();
        expect(light.borderRadius).toBe(8);
        expect(light.color).toBe(lightTheme.textMuted);
        expect(light.boxSizing).toBe("border-box");

        const dark = reasoningPanelChrome(darkTheme);
        const darkEdge = `1px solid ${darkTheme.fieldBorder}`;
        expect(dark.background).toBe(darkTheme.fieldBg);
        expect(dark.borderTop).toBe(darkEdge);
        expect(dark.borderLeft).toBe(`2px solid ${darkTheme.responseBorderLeft}`);
        expect(dark.border).toBeUndefined();
    });
});

describe("assistant reasoning panel CSS", () => {
    it("does not force a card fill or drop-shadow onto the thinking wash", () => {
        expect(css).not.toMatch(/\.assistant-reasoning-panel[\s,{][^}]*!important/);
    });
});
