import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { relativeLuminance } from "../components/ai/aiAssistantPanelTheme";
import { assistantDarkSchemes } from "../components/ai/assistantDarkSchemes";
import { assistantLightSchemes } from "../components/ai/assistantLightSchemes";

const shell = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "partials/110-mc-app-shell.css"), "utf8");

function ruleStartingAt(css: string, selector: string): string {
    const start = css.indexOf(`\n${selector}`);
    if (start < 0) throw new Error(`missing rule: ${selector}`);
    const open = css.indexOf("{", start);
    let depth = 0;
    for (let i = open; i < css.length; i += 1) {
        if (css[i] === "{") depth += 1;
        else if (css[i] === "}") {
            depth -= 1;
            if (depth === 0) return css.slice(start + 1, i + 1);
        }
    }
    throw new Error(`unclosed rule: ${selector}`);
}

function contrastRatio(foreground: string, background: string): number {
    const fg = relativeLuminance(foreground);
    const bg = relativeLuminance(background);
    if (fg == null || bg == null) throw new Error(`unparseable ${foreground} / ${background}`);
    const lighter = Math.max(fg, bg);
    const darker = Math.min(fg, bg);
    return (lighter + 0.05) / (darker + 0.05);
}

describe("artifact preview button contrast", () => {
    it("keeps the filled preview label off the accent hover", () => {
        const hover = ruleStartingAt(shell, ".mc-task-artifact-item__actions .mc-artifact-btn--fill:hover:not(:disabled)");
        expect(hover).toContain("color: var(--theme-on-primary)");
        expect(hover).toContain("border-color: transparent");
        expect(hover).toContain("background: var(--theme-primary-strong)");

        const generic = ruleStartingAt(shell, ".mc-task-result-card__actions button:hover:not(.mc-artifact-btn--fill)");
        expect(generic).toContain("color: var(--mc-accent, #2e75cb)");
        expect(generic).not.toContain("!important");
        expect(shell).not.toContain(".mc-task-result-card__actions button:hover {");

        const darkGeneric = "#App[data-ai-theme='dark'] .mc-task-artifacts .mc-task-artifact-item__actions button:hover:not(:disabled):not(.mc-artifact-btn--fill)";
        expect(ruleStartingAt(shell, darkGeneric)).toContain("color: var(--theme-primary-strong)");
        const darkFill = ruleStartingAt(shell, "#App[data-ai-theme='dark'] .mc-task-artifacts .mc-task-artifact-item__actions .mc-artifact-btn--fill");
        expect(darkFill).toContain("color: var(--theme-on-primary)");
    });

    it("keeps on-primary readable on the preview fill in every scheme", () => {
        for (const scheme of [...assistantLightSchemes, ...assistantDarkSchemes]) {
            const ink = scheme.cssVars.onPrimary;
            expect(contrastRatio(ink, scheme.cssVars.primary), `${scheme.id} primary`).toBeGreaterThanOrEqual(4.5);
            expect(contrastRatio(ink, scheme.cssVars.primaryStrong), `${scheme.id} primary-strong`).toBeGreaterThanOrEqual(4.5);
        }
    });
});
