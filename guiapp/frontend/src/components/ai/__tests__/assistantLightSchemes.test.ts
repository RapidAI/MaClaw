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
import { buildBootThemePalette, renderBootThemeScript } from "../themeBootPalette";

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
    onPrimary: "--theme-on-primary",
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
        expect(scheme.cssVars.primary).toBe("#2e75cb");
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

    it("keeps light interface ink darker than the rail and off near-black", () => {
        const css = readFileSync(resolve(frontendSrc, "styles/partials/120-app-responsive.css"), "utf8");
        const shell = readFileSync(resolve(frontendSrc, "styles/partials/110-mc-app-shell.css"), "utf8");
        expect(css).toContain("[data-ai-light-scheme]:not([data-ai-theme='dark'])");
        expect(css).toContain("--theme-text-primary: var(--mc-chrome-ink)");
        expect(css).toContain("--theme-text-secondary: color-mix(in srgb, var(--mc-chrome-ink) 55%, var(--theme-text-muted))");
        expect(css).toContain("--mc-text-anchor");
        expect(css).not.toContain("--theme-text-muted: var(--office-muted)");
        const office = readFileSync(resolve(frontendSrc, "styles/partials/100-pet-store-mc.css"), "utf8");
        expect(office).not.toContain(":where(.muted, .secondary-text, [class*='muted']) { color: var(--office-muted) !important; }");
        expect(office).toContain(":where(.muted, [class*='muted']) { color: var(--theme-text-muted, var(--office-muted)); }");
        expect(office).toContain(":where(.secondary-text) { color: var(--theme-text-secondary, var(--office-muted)); }");
        expect(shell).not.toMatch(/\.stsm-row-title \{[^}]*office-muted/);
        expect(shell).toMatch(/\.stsm-row-title \{[^}]*color: var\(--theme-text-primary\)/);
        expect(shell).not.toMatch(/--theme-text-primary:\s*#26364a/);
        expect(shell).not.toMatch(/--theme-text-muted:\s*#8a9bb0/);
        const sidebarSurface = "#f0f6ff";
        for (const scheme of assistantLightSchemes) {
            const primary = relativeLuminance(scheme.cssVars.textPrimary);
            const secondary = relativeLuminance(scheme.cssVars.textSecondary);
            const muted = relativeLuminance(scheme.cssVars.textMuted);
            const quote = relativeLuminance(scheme.assistantTheme.quoteText);
            expect(primary, scheme.id).not.toBeNull();
            expect(secondary, scheme.id).not.toBeNull();
            expect(muted, scheme.id).not.toBeNull();
            expect(primary!, scheme.id).toBeLessThan(secondary!);
            expect(secondary!, scheme.id).toBeLessThan(muted!);
            expect(quote, scheme.id).not.toBeNull();
            expect(quote!, scheme.id).toBeGreaterThan(primary!);
            expect(quote!, scheme.id).toBeLessThan(muted!);
            expect(contrastRatio(scheme.cssVars.textPrimary, scheme.cssVars.pageBg), scheme.id).toBeGreaterThanOrEqual(4.5);
            expect(contrastRatio(scheme.cssVars.textSecondary, scheme.cssVars.pageBg), scheme.id).toBeGreaterThanOrEqual(4.5);
            expect(contrastRatio(scheme.cssVars.textMuted, scheme.cssVars.pageBg), scheme.id).toBeGreaterThanOrEqual(4.5);
            expect(contrastRatio(scheme.cssVars.textSecondary, sidebarSurface), scheme.id).toBeGreaterThanOrEqual(4.5);
            expect(contrastRatio(scheme.assistantTheme.text, scheme.assistantTheme.bg), scheme.id).toBeGreaterThanOrEqual(4.5);
            expect(primary!, scheme.id).toBeGreaterThan(relativeLuminance("#17263c")!);
            expect(scheme.assistantTheme.boldColor).toBe(scheme.assistantTheme.text);
            expect(scheme.assistantTheme.pathColor).toBe(scheme.cssVars.textSecondary);
            expect(scheme.assistantTheme.promptColor).toBe(scheme.cssVars.textSecondary);
            expect(scheme.assistantTheme.italicColor).toBe(scheme.cssVars.textSecondary);
        }
    });

    it("keeps CSS scheme tokens in sync with TypeScript palettes", () => {
        const css = readFileSync(resolve(frontendSrc, "styles/generated/themeSchemes.generated.css"), "utf8");
        for (const scheme of assistantLightSchemes) {
            const vars = parseSchemeBlock(css, scheme.id);
            for (const [key, cssName] of Object.entries(cssTokenMap)) {
                expect(vars[cssName], `${scheme.id} ${cssName}`).toBe(scheme.cssVars[key as keyof typeof scheme.cssVars]);
            }
        }
    });

    it("paints the first frame with the Fluent page background by default", () => {
        const html = readFileSync(resolve(frontendSrc, "../index.html"), "utf8");
        expect(html).toContain("background-color: var(--theme-page-bg, #ffffff)");
        // The boot palette is generated at build time from the scheme modules
        // (single source of truth); index.html only carries a placeholder.
        expect(html).toContain("<!-- @maclaw-boot-theme -->");
        const script = renderBootThemeScript(buildBootThemePalette());
        for (const scheme of assistantLightSchemes) {
            expect(script.toLowerCase(), scheme.id).toContain(scheme.cssVars.pageBg);
        }
        expect(script).not.toContain("aurora' ? '#071018'");
    });
});
