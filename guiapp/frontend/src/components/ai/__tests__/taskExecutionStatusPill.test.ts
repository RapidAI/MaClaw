import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const here = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(resolve(here, "../../../App.css"), "utf8");

describe("task execution header pending pill", () => {
    it("uses the title ink instead of the warning amber", () => {
        expect(css).toContain("--mc-wait: var(--mc-chrome-ink, var(--theme-text-secondary))");
        expect(css).toMatch(
            /#App \.mc-task-execution-status\[data-status='pending'\] \{\s*background: var\(--mc-wait-bg\) !important;\s*border-color: var\(--mc-wait-border\) !important;\s*color: var\(--mc-wait\) !important;/,
        );
        expect(css).not.toContain("rgba(243,154,36");
    });

    it("does not let the dark warning lock repaint the header pill", () => {
        expect(css).not.toContain("#App[data-ai-theme='dark'] .mc-task-execution-status[data-status='pending']");
        expect(css).not.toContain("#App[data-ai-theme='dark'] .mc-task-execution-status--pending");
    });

    it("leaves pending execution steps on the warning color", () => {
        expect(css).toMatch(
            /#App \.mc-execution-step\[data-status='pending'\] \{\s*background: color-mix\(in srgb, var\(--theme-warning\) 11%, var\(--mc-surface\)\) !important;/,
        );
    });
});
