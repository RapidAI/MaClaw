import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { reasoningPanelChrome } from "../AssistantReasoningPanel";
import { darkTheme, lightTheme } from "../aiAssistantPanelTheme";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(resolve(here, "../../../App.css"), "utf8");

describe("reasoningPanelChrome", () => {
    it("washes muted ink into the theme surface instead of transparent black", () => {
        const light = reasoningPanelChrome(lightTheme);
        expect(light.background).toBe(`color-mix(in srgb, ${lightTheme.textMuted} 4.5%, ${lightTheme.bg})`);
        expect(light.borderLeft).toBe(`2px solid color-mix(in srgb, ${lightTheme.textMuted} 32%, ${lightTheme.bg})`);
        expect(String(light.background)).not.toContain("transparent");
        expect(String(light.borderLeft)).not.toContain("transparent");

        const dark = reasoningPanelChrome(darkTheme);
        expect(dark.background).toBe(`color-mix(in srgb, ${darkTheme.textMuted} 8%, ${darkTheme.bg})`);
        expect(dark.borderLeft).toBe(`2px solid color-mix(in srgb, ${darkTheme.textMuted} 48%, ${darkTheme.bg})`);
        expect(String(dark.background)).not.toContain("transparent");
        expect(String(dark.borderLeft)).not.toMatch(/rgba\(/);
    });

    it("honors an explicit isDark flag over background luminance", () => {
        const forcedDark = reasoningPanelChrome({ ...lightTheme, isDark: true });
        expect(forcedDark.background).toBe(`color-mix(in srgb, ${lightTheme.textMuted} 8%, ${lightTheme.bg})`);
        const forcedLight = reasoningPanelChrome({ ...darkTheme, isDark: false });
        expect(forcedLight.background).toBe(`color-mix(in srgb, ${darkTheme.textMuted} 4.5%, ${darkTheme.bg})`);
    });
});

describe("assistant reasoning panel CSS", () => {
    it("does not force a card fill or drop-shadow onto the thinking wash", () => {
        expect(css).not.toMatch(/\.assistant-reasoning-panel[\s,{][^}]*!important/);
    });
});
