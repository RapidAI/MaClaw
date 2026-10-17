import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const frontendSrc = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const responsive = readFileSync(resolve(frontendSrc, "styles/partials/120-app-responsive.css"), "utf8");
const webSearch = readFileSync(resolve(frontendSrc, "styles/partials/70-progress-web-im.css"), "utf8");

function extractRule(css: string, header: string): string {
    const start = css.indexOf(header);
    if (start < 0) throw new Error(`missing rule: ${header}`);
    const open = css.indexOf("{", start);
    let depth = 0;
    for (let i = open; i < css.length; i += 1) {
        if (css[i] === "{") depth += 1;
        else if (css[i] === "}") {
            depth -= 1;
            if (depth === 0) return css.slice(start, i + 1);
        }
    }
    throw new Error(`unclosed rule: ${header}`);
}

describe("settings button contrast", () => {
    it("keeps the settings ink fallback below each button's own foreground", () => {
        const fallback = extractRule(responsive, ":where(.settings-panel) button:where(:not(.btn-primary)):where(:not([type='submit']))");
        expect(fallback).toContain("color: var(--office-ink, var(--theme-text-primary))");
        expect(responsive).not.toContain(".settings-panel button:not(.btn-primary):not([type='submit'])");
        expect(responsive).not.toContain("@layer settings-button-fallback");

        const save = extractRule(webSearch, ".web-search-config__save {");
        expect(save).toContain("background: var(--theme-primary)");
        expect(save).toContain("color: var(--theme-on-primary, #ffffff)");
        expect(responsive).not.toContain(".settings-panel :is(");
        expect(responsive).not.toContain("color-mix(in srgb, var(--theme-primary) 75%, #000)");

        const grain = extractRule(webSearch, "#App:not([data-ai-theme='dark']) .settings-content.settings-panel button.token-usage-trend__grain.is-active");
        expect(grain).toContain("color-mix(in srgb, var(--theme-primary) 75%, #000)");

        const activeMode = extractRule(webSearch, ".prog-tools__mode-btn[data-active=\"true\"]");
        expect(activeMode).toContain("color: var(--theme-on-primary)");

        const activeTab = extractRule(webSearch, ".prog-tools__kb-tab[data-active=\"true\"]");
        expect(activeTab).toContain("color: var(--theme-on-primary)");

        const restingTab = extractRule(webSearch, ".prog-tools__kb-tab {");
        expect(restingTab).toContain("color: var(--theme-text-secondary)");
        expect(restingTab).not.toContain("--theme-text-tertiary");

        const refresh = extractRule(webSearch, ".usage-display__button {");
        expect(refresh).toContain("color: var(--theme-on-primary)");
        const refreshDisabled = extractRule(webSearch, ".usage-display__button:disabled");
        expect(refreshDisabled).toContain("background: var(--theme-surface-muted)");
        expect(refreshDisabled).toContain("color: var(--theme-text-muted)");
    });
});
