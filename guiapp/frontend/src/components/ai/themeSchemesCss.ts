import {
    assistantDarkSchemes,
    graphiteDarkScheme,
    type AssistantDarkScheme,
} from "./assistantDarkSchemes";
import {
    assistantLightSchemes,
    type AssistantLightScheme,
} from "./assistantLightSchemes";

/**
 * Renders the theme scheme CSS blocks ([data-ai-dark-scheme=…] /
 * [data-ai-light-scheme=…]) from the assistant scheme modules, so the TS
 * palette data is the single source of truth for both the runtime chat theme
 * and the application chrome. The output is written to
 * src/styles/generated/themeSchemes.generated.css by
 * scripts/gen-theme-schemes-css.mjs (wired into prebuild) and, at the same
 * cascade position, into src/App.css by scripts/assemble-app-css.mjs per
 * src/styles/app-css.manifest.json. A vitest guard (themeSchemesCss.test.ts)
 * re-renders and asserts the committed artifacts stay in sync with the
 * scheme modules.
 *
 * This module is loaded by the Node-based generator, so it must stay free of
 * React/DOM imports.
 */

const ONBOARDING_COMMENT = `/* ── Onboarding Wizard dark theme ──
 * The OnboardingWizard uses createPortal(... , document.body) and renders
 * outside #App, so it cannot inherit dark-theme CSS variables from
 * #App[data-ai-theme='dark']. Duplicate the variables on its own
 * data-ai-theme attribute. Keep in sync with the #App dark theme block.
 */`;

const CLASSIC_COMMENT = `/* Classic Slate keeps the pre-2026-08 dark palette. The base dark blocks above
   now hold the default (graphite) navy palette, so classic needs its own
   override block to stay pixel-identical for users who selected it. */`;

const LIGHT_HEADER_COMMENT = `/* ── Light Mode Palette Schemes ── */

/* Keep the assistant's TypeScript palette and the shared application chrome
 * on the same source of truth.  Fluent is the default workbench look; the
 * explicit classic scheme remains available for users who prefer the older
 * blue-gray surfaces. */`;

const LIGHT_SCHEME_COMMENTS: Record<AssistantLightScheme["id"], string> = {
    fluent: "/* WCAG AA contrast fixes (lightness-only): primary #2f78d0→#2e75cb (was 4.45:1 on white), success #16a34a→#11813a, warning #d97706→#aa5d05, danger #e5484d→#dc1f25. */",
    default: "/* WCAG AA contrast fix (lightness-only): success #4f7f6f→#4b796a (was 4.23:1 on success-bg). */",
    notion: "/* WCAG AA contrast fixes (lightness-only): warning #d9730d→#ad5c0a, danger #e03e3e→#da2323. */",
    linear: "/* WCAG AA contrast fixes (lightness-only): success #26b583→#1b805d, warning #f2994a→#b25a0d, danger #eb5757→#de1a1a. */",
    github: "/* WCAG AA contrast fix (lightness-only): warning #9a6700→#976500 (was 4.45:1 on warning-bg). */",
    stripe: "/* WCAG AA contrast fixes (lightness-only): success #0d9f6e→#0b8159, warning #cb7e1a→#a06314. */",
    vercel: "/* WCAG AA contrast fixes (lightness-only): success #17b169→#11824d, warning #f5a623→#a06607, danger #e5484d→#dd1f25. */",
};

const DARK_SHADOWS = `    /* UI polish: dark-mode shadow scale (black-based, mirrors :root polish tokens). */
    --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.35);
    --shadow-md: 0 1px 2px rgba(0, 0, 0, 0.30), 0 4px 12px -2px rgba(0, 0, 0, 0.40);
    --shadow-lg: 0 8px 28px -6px rgba(0, 0, 0, 0.55);
    --shadow-xl: 0 12px 40px -8px rgba(0, 0, 0, 0.60);`;

function renderSchemeVars(vars: AssistantDarkScheme["cssVars"] | AssistantLightScheme["cssVars"]): string {
    return [
        `    --theme-primary: ${vars.primary};`,
        `    --theme-primary-strong: ${vars.primaryStrong};`,
        `    --theme-primary-soft: ${vars.primarySoft};`,
        `    --theme-page-bg: ${vars.pageBg};`,
        `    --theme-surface: ${vars.surface};`,
        `    --theme-surface-muted: ${vars.surfaceMuted};`,
        `    --theme-text-primary: ${vars.textPrimary};`,
        `    --theme-text-secondary: ${vars.textSecondary};`,
        `    --theme-text-muted: ${vars.textMuted};`,
        `    --theme-border: ${vars.border};`,
        `    --theme-border-subtle: ${vars.borderSubtle};`,
        `    --theme-success: ${vars.success};`,
        `    --theme-success-bg: ${vars.successBg};`,
        `    --theme-warning: ${vars.warning};`,
        `    --theme-warning-bg: ${vars.warningBg};`,
        `    --theme-danger: ${vars.danger};`,
        `    --theme-danger-bg: ${vars.dangerBg};`,
        `    --theme-link-color: ${vars.linkColor};`,
        `    --theme-info-bg: ${vars.infoBg};`,
        `    --theme-on-primary: ${vars.onPrimary};`,
    ].join("\n");
}

export function renderThemeSchemesCss(): string {
    const blocks: string[] = [];

    // Base dark block (onboarding wizard portal + default graphite chrome),
    // plus the dark-mode shadow scale that only exists on this block.
    blocks.push(
        `${ONBOARDING_COMMENT}\n[data-ai-theme='dark'] {\n${renderSchemeVars(graphiteDarkScheme.cssVars)}\n${DARK_SHADOWS}\n}`
    );

    for (const scheme of assistantDarkSchemes) {
        const comment = scheme.id === "classic" ? `${CLASSIC_COMMENT}\n` : "";
        blocks.push(
            `${comment}[data-ai-theme='dark'][data-ai-dark-scheme='${scheme.id}'] {\n${renderSchemeVars(scheme.cssVars)}\n}`
        );
    }

    blocks.push(LIGHT_HEADER_COMMENT);
    for (const scheme of assistantLightSchemes) {
        blocks.push(
            `${LIGHT_SCHEME_COMMENTS[scheme.id]}\n[data-ai-light-scheme='${scheme.id}'] {\n${renderSchemeVars(scheme.cssVars)}\n}`
        );
    }

    return `${blocks.join("\n\n")}\n`;
}
