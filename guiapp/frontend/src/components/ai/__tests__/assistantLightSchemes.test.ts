import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, it } from "vitest";
import { getInputActionButtonStyle, lightTheme, overlayTheme, relativeLuminance } from "../aiAssistantPanelTheme";
import {
    assistantLightSchemes,
    getAssistantLightScheme,
    isAssistantLightSchemeId,
    readStoredAssistantLightSchemeId,
    ASSISTANT_LIGHT_SCHEME_STORAGE_KEY,
} from "../assistantLightSchemes";

const here = dirname(fileURLToPath(import.meta.url));
const frontendSrc = resolve(here, "../../..");

function contrastRatio(fg: string, bg: string): number {
    const L1 = relativeLuminance(fg);
    const L2 = relativeLuminance(bg);
    if (L1 == null || L2 == null) throw new Error(`unparseable ${fg} / ${bg}`);
    const lighter = Math.max(L1, L2);
    const darker = Math.min(L1, L2);
    return (lighter + 0.05) / (darker + 0.05);
}

function parseSchemeBlock(css: string, schemeId: string): Record<string, string> {
    const re = new RegExp(`\\[data-ai-light-scheme='${schemeId}'\\]\\s*\\{([^}]+)\\}`);
    const match = re.exec(css);
    expect(match, `missing CSS block for ${schemeId}`).toBeTruthy();
    const vars: Record<string, string> = {};
    for (const line of match![1].split(";")) {
        const trimmed = line.trim();
        if (!trimmed.startsWith("--theme-")) continue;
        const [name, value] = trimmed.split(":");
        if (name && value) vars[name.trim()] = value.trim();
    }
    return vars;
}

const cssTokenMap = {
    primary: "--theme-primary",
    primaryStrong: "--theme-primary-strong",
    primarySoft: "--theme-primary-soft",
    pageBg: "--theme-page-bg",
    surface: "--theme-surface",
    surfaceMuted: "--theme-surface-muted",
    textPrimary: "--theme-text-primary",
    textSecondary: "--theme-text-secondary",
    textMuted: "--theme-text-muted",
    border: "--theme-border",
    borderSubtle: "--theme-border-subtle",
    success: "--theme-success",
    successBg: "--theme-success-bg",
    warning: "--theme-warning",
    warningBg: "--theme-warning-bg",
    danger: "--theme-danger",
    dangerBg: "--theme-danger-bg",
    linkColor: "--theme-link-color",
    infoBg: "--theme-info-bg",
} as const;

describe("assistant light schemes", () => {
    afterEach(() => {
        window.localStorage.removeItem(ASSISTANT_LIGHT_SCHEME_STORAGE_KEY);
    });

    it("puts Fluent first and uses it as the fallback scheme", () => {
        expect(assistantLightSchemes[0]?.id).toBe("fluent");
        expect(getAssistantLightScheme("unknown").id).toBe("fluent");
        expect(isAssistantLightSchemeId("fluent")).toBe(true);
        expect(isAssistantLightSchemeId("not-a-scheme")).toBe(false);
    });

    it("defaults to Fluent when no preference is stored", () => {
        expect(readStoredAssistantLightSchemeId()).toBe("fluent");
    });

    it("preserves valid stored preferences", () => {
        window.localStorage.setItem(ASSISTANT_LIGHT_SCHEME_STORAGE_KEY, "notion");
        expect(readStoredAssistantLightSchemeId()).toBe("notion");
        window.localStorage.setItem(ASSISTANT_LIGHT_SCHEME_STORAGE_KEY, "github");
        expect(readStoredAssistantLightSchemeId()).toBe("github");
        window.localStorage.setItem(ASSISTANT_LIGHT_SCHEME_STORAGE_KEY, "fluent");
        expect(readStoredAssistantLightSchemeId()).toBe("fluent");
    });

    it("exposes the Fluent Azure palette tokens", () => {
        const scheme = getAssistantLightScheme("fluent");
        expect(scheme.cssVars.pageBg).toBe("#ffffff");
        expect(scheme.cssVars.primary).toBe("#2f78d0");
        expect(scheme.cssVars.surface).toBe("#ffffff");
        expect(scheme.assistantTheme.sendBtnBg).toBe("#1769e8");
    });

    it("keeps overlay/light fallbacks aligned with Fluent", () => {
        const fluent = getAssistantLightScheme("fluent").assistantTheme;
        expect(lightTheme).toEqual(fluent);
        expect(overlayTheme).toEqual(fluent);
    });

    it("uses the selected scheme for light-mode send chrome", () => {
        const fluent = getAssistantLightScheme("fluent").assistantTheme;
        const send = getInputActionButtonStyle(fluent, "light", "send");
        expect(send.background).toBe(fluent.sendBtnBg);
        expect(send.color).toBe(fluent.sendBtnColor);
        const github = getAssistantLightScheme("github").assistantTheme;
        expect(getInputActionButtonStyle(github, "light", "send").background).toBe(github.sendBtnBg);
        expect(getInputActionButtonStyle(github, "light", "send").background).not.toBe(fluent.sendBtnBg);
    });

    it("keeps Fluent muted text and send fill readable", () => {
        const scheme = getAssistantLightScheme("fluent");
        expect(contrastRatio(scheme.cssVars.textMuted, scheme.cssVars.pageBg)).toBeGreaterThanOrEqual(4.5);
        expect(contrastRatio(scheme.assistantTheme.sendBtnColor, scheme.assistantTheme.sendBtnBg)).toBeGreaterThanOrEqual(4.5);
    });

    it("keeps CSS scheme tokens in sync with TypeScript palettes", () => {
        const css = readFileSync(resolve(frontendSrc, "App.css"), "utf8");
        for (const scheme of assistantLightSchemes) {
            const vars = parseSchemeBlock(css, scheme.id);
            for (const [key, cssName] of Object.entries(cssTokenMap)) {
                expect(vars[cssName], `${scheme.id} ${cssName}`).toBe(scheme.cssVars[key as keyof typeof scheme.cssVars]);
            }
        }
    });

    it("paints the first frame with the Fluent page background by default", () => {
        const html = readFileSync(resolve(frontendSrc, "../index.html"), "utf8");
        expect(html).toContain(": '#ffffff'");
        expect(html).toContain("background-color: var(--theme-page-bg, #ffffff)");
        for (const scheme of assistantLightSchemes) {
            expect(html.toLowerCase(), scheme.id).toContain(scheme.cssVars.pageBg);
        }
    });
});
