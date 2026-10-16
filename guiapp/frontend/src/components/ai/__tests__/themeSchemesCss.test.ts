import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { AI_THEME_MODE_STORAGE_KEY } from "../aiAssistantPanelTheme";
import { assistantDarkSchemes, onyxDarkScheme, ASSISTANT_DARK_SCHEME_STORAGE_KEY } from "../assistantDarkSchemes";
import { assistantLightSchemes, DEFAULT_ASSISTANT_LIGHT_SCHEME_ID, ASSISTANT_LIGHT_SCHEME_STORAGE_KEY } from "../assistantLightSchemes";
import {
    BOOT_THEME_MODE_STORAGE_KEY,
    buildBootThemePalette,
    renderBootThemeScript,
} from "../themeBootPalette";
import { renderThemeSchemesCss } from "../themeSchemesCss";

const here = dirname(fileURLToPath(import.meta.url));
const generatedCssPath = resolve(here, "../../../styles/generated/themeSchemes.generated.css");

function parseBlockVars(css: string, selector: string): Record<string, string> {
    const escaped = selector.replace(/[[\]()'.]/g, (ch) => `\\${ch}`);
    const match = new RegExp(escaped + "\\s*\\{([^}]+)\\}").exec(css);
    expect(match, `missing CSS block ${selector}`).toBeTruthy();
    // Strip comments first: a comment line without a trailing ";" would
    // otherwise glue itself to the following declaration.
    const body = match![1].replace(/\/\*[\s\S]*?\*\//g, "");
    const vars: Record<string, string> = {};
    for (const line of body.split(";")) {
        const trimmed = line.trim();
        if (!trimmed.startsWith("--")) continue;
        const idx = trimmed.indexOf(":");
        if (idx > 0) vars[trimmed.slice(0, idx).trim()] = trimmed.slice(idx + 1).trim();
    }
    return vars;
}

const tokenToCssVar: Record<string, string> = {
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
};

describe("theme schemes CSS (generated)", () => {
    it("committed generated file is in sync with the scheme modules", () => {
        const committed = readFileSync(generatedCssPath, "utf8");
        expect(committed).toBe(renderThemeSchemesCss());
    });

    it("dark scheme blocks carry the TypeScript palette values", () => {
        const css = renderThemeSchemesCss();
        for (const scheme of assistantDarkSchemes) {
            const vars = parseBlockVars(css, `[data-ai-theme='dark'][data-ai-dark-scheme='${scheme.id}']`);
            for (const [key, cssVar] of Object.entries(tokenToCssVar)) {
                expect(vars[cssVar], `${scheme.id} ${cssVar}`).toBe(scheme.cssVars[key as keyof typeof scheme.cssVars]);
            }
        }
    });

    it("base dark block matches onyx (default) plus the dark shadow scale", () => {
        const css = renderThemeSchemesCss();
        const vars = parseBlockVars(css, "[data-ai-theme='dark']");
        for (const [key, cssVar] of Object.entries(tokenToCssVar)) {
            expect(vars[cssVar], `base ${cssVar}`).toBe(onyxDarkScheme.cssVars[key as keyof typeof onyxDarkScheme.cssVars]);
        }
        for (const shadow of ["--shadow-sm", "--shadow-md", "--shadow-lg", "--shadow-xl"]) {
            expect(vars[shadow], shadow).toBeTruthy();
        }
    });

    it("light scheme blocks carry the TypeScript palette values", () => {
        const css = renderThemeSchemesCss();
        for (const scheme of assistantLightSchemes) {
            const vars = parseBlockVars(css, `[data-ai-light-scheme='${scheme.id}']`);
            for (const [key, cssVar] of Object.entries(tokenToCssVar)) {
                expect(vars[cssVar], `${scheme.id} ${cssVar}`).toBe(scheme.cssVars[key as keyof typeof scheme.cssVars]);
            }
        }
    });
});

describe("boot theme palette", () => {
    it("mirrors the storage keys used by the runtime theme", () => {
        expect(BOOT_THEME_MODE_STORAGE_KEY).toBe(AI_THEME_MODE_STORAGE_KEY);
    });

    it("carries every scheme page background plus the defaults", () => {
        const palette = buildBootThemePalette();
        for (const scheme of assistantDarkSchemes) {
            expect(palette.dark[scheme.storageValue], scheme.id).toBe(scheme.cssVars.pageBg);
        }
        for (const scheme of assistantLightSchemes) {
            expect(palette.light[scheme.storageValue], scheme.id).toBe(scheme.cssVars.pageBg);
        }
        expect(palette.defaultDark).toBe(onyxDarkScheme.cssVars.pageBg);
        expect(palette.defaultLight).toBe(
            assistantLightSchemes.find((s) => s.id === DEFAULT_ASSISTANT_LIGHT_SCHEME_ID)!.cssVars.pageBg,
        );
    });

    it("paints the expected background for each stored scheme and falls back to defaults", () => {
        const palette = buildBootThemePalette();
        const script = renderBootThemeScript(palette);

        const run = (mode: string | null, darkScheme: string | null, lightScheme: string | null): string => {
            const store: Record<string, string> = {};
            if (mode !== null) store[BOOT_THEME_MODE_STORAGE_KEY] = mode;
            if (darkScheme !== null) store[ASSISTANT_DARK_SCHEME_STORAGE_KEY] = darkScheme;
            if (lightScheme !== null) store[ASSISTANT_LIGHT_SCHEME_STORAGE_KEY] = lightScheme;
            const el: { style: Record<string, string>; attrs: Record<string, string> } = { style: {}, attrs: {} };
            const documentStub = {
                documentElement: {
                    style: {
                        set backgroundColor(v: string) {
                            el.style.backgroundColor = v;
                        },
                        set colorScheme(v: string) {
                            el.style.colorScheme = v;
                        },
                        setProperty(name: string, v: string) {
                            el.style[name] = v;
                        },
                    },
                    setAttribute(name: string, v: string) {
                        el.attrs[name] = v;
                    },
                },
            };
            const fn = new Function("localStorage", "document", script);
            fn(
                { getItem: (k: string) => (k in store ? store[k] : null) },
                documentStub,
            );
            expect(el.style["--theme-page-bg"]).toBe(el.style.backgroundColor);
            return el.style.backgroundColor;
        };

        for (const scheme of assistantDarkSchemes) {
            expect(run("dark", scheme.storageValue, null), scheme.id).toBe(scheme.cssVars.pageBg);
        }
        for (const scheme of assistantLightSchemes) {
            expect(run("light", null, scheme.storageValue), scheme.id).toBe(scheme.cssVars.pageBg);
        }
        // Unset / legacy / unknown values fall back to the scheme defaults.
        expect(run("dark", null, null)).toBe(palette.defaultDark);
        expect(run("dark", "some-legacy-scheme", null)).toBe(palette.defaultDark);
        expect(run("light", null, null)).toBe(palette.defaultLight);
        expect(run("light", null, "some-legacy-scheme")).toBe(palette.defaultLight);
        expect(run(null, null, null)).toBe(palette.defaultLight);
    });
});
