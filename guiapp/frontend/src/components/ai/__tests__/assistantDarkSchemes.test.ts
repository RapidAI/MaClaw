import { afterEach, describe, expect, it } from "vitest";
import { darkTheme, relativeLuminance } from "../aiAssistantPanelTheme";
import {
    assistantDarkSchemes,
    getAssistantDarkScheme,
    isAssistantDarkSchemeId,
    onyxDarkScheme,
    readStoredAssistantDarkSchemeId,
    ASSISTANT_DARK_SCHEME_STORAGE_KEY,
} from "../assistantDarkSchemes";

function contrastRatio(fg: string, bg: string): number {
    const L1 = relativeLuminance(fg);
    const L2 = relativeLuminance(bg);
    if (L1 == null || L2 == null) throw new Error(`unparseable ${fg} / ${bg}`);
    const lighter = Math.max(L1, L2);
    const darker = Math.min(L1, L2);
    return (lighter + 0.05) / (darker + 0.05);
}

describe("assistant dark schemes", () => {
    afterEach(() => {
        window.localStorage.removeItem(ASSISTANT_DARK_SCHEME_STORAGE_KEY);
    });

    it("puts Onyx first and uses it as the fallback scheme", () => {
        expect(assistantDarkSchemes[0]?.id).toBe("onyx");
        expect(getAssistantDarkScheme("unknown").id).toBe("onyx");
        expect(getAssistantDarkScheme(undefined).id).toBe("onyx");
        expect(isAssistantDarkSchemeId("onyx")).toBe(true);
        expect(isAssistantDarkSchemeId("not-a-scheme")).toBe(false);
    });

    it("defaults to Onyx when no preference is stored", () => {
        expect(readStoredAssistantDarkSchemeId()).toBe("onyx");
    });

    it("preserves valid stored preferences", () => {
        window.localStorage.setItem(ASSISTANT_DARK_SCHEME_STORAGE_KEY, "graphite");
        expect(readStoredAssistantDarkSchemeId()).toBe("graphite");
        window.localStorage.setItem(ASSISTANT_DARK_SCHEME_STORAGE_KEY, "classic");
        expect(readStoredAssistantDarkSchemeId()).toBe("classic");
        window.localStorage.setItem(ASSISTANT_DARK_SCHEME_STORAGE_KEY, "aurora");
        expect(readStoredAssistantDarkSchemeId()).toBe("aurora");
        window.localStorage.setItem(ASSISTANT_DARK_SCHEME_STORAGE_KEY, "ember");
        expect(readStoredAssistantDarkSchemeId()).toBe("ember");
        window.localStorage.setItem(ASSISTANT_DARK_SCHEME_STORAGE_KEY, "violet");
        expect(readStoredAssistantDarkSchemeId()).toBe("violet");
        window.localStorage.setItem(ASSISTANT_DARK_SCHEME_STORAGE_KEY, "onyx");
        expect(readStoredAssistantDarkSchemeId()).toBe("onyx");
    });

    it("falls back to Onyx for legacy/unknown stored values", () => {
        window.localStorage.setItem(ASSISTANT_DARK_SCHEME_STORAGE_KEY, "some-legacy-scheme");
        expect(readStoredAssistantDarkSchemeId()).toBe("onyx");
    });

    it("exposes the Onyx palette tokens", () => {
        const scheme = getAssistantDarkScheme("onyx");
        expect(scheme.cssVars.pageBg).toBe("#050505");
        expect(scheme.cssVars.textPrimary).toBe("#c9c9c9");
        expect(scheme.assistantTheme.sendBtnBg).toBe("#b3b3b3");
    });

    it("keeps the dark runtime fallback aligned with the Onyx scheme", () => {
        expect(darkTheme).toEqual(onyxDarkScheme.assistantTheme);
    });

    it("keeps text, links and send chrome readable on every dark scheme", () => {
        for (const scheme of assistantDarkSchemes) {
            const v = scheme.cssVars;
            const t = scheme.assistantTheme;
            expect(contrastRatio(v.textPrimary, v.pageBg), `${scheme.id} textPrimary`).toBeGreaterThanOrEqual(4.5);
            expect(contrastRatio(v.textSecondary, v.pageBg), `${scheme.id} textSecondary`).toBeGreaterThanOrEqual(4.5);
            expect(contrastRatio(v.textMuted, v.pageBg), `${scheme.id} textMuted`).toBeGreaterThanOrEqual(4.5);
            expect(contrastRatio(v.linkColor, v.pageBg), `${scheme.id} linkColor`).toBeGreaterThanOrEqual(4.5);
            expect(contrastRatio(v.onPrimary, v.primary), `${scheme.id} onPrimary/primary`).toBeGreaterThanOrEqual(4.5);
            expect(contrastRatio(t.sendBtnColor, t.sendBtnBg), `${scheme.id} sendBtn`).toBeGreaterThanOrEqual(4.5);
        }
    });
});
