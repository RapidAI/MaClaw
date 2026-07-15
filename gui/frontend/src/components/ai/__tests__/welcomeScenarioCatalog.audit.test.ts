import { describe, expect, it } from "vitest";
import {
    auditWelcomeScenarioCatalog,
    WELCOME_PROMPT_ICON_NAMES,
    welcomeCatalogErrors,
} from "../welcomeScenarioCatalogGuards";
import { SCENARIO_TABS, WELCOME_SCENARIO_PROMPTS_PER_TAB } from "../welcomeScenarioTasks";
import { resolveWelcomeQuickHints, WELCOME_QUICK_HINTS } from "../welcomeQuickHints";

describe("welcome scenario catalog", () => {
    it(`has ${WELCOME_SCENARIO_PROMPTS_PER_TAB} prompts per tab and passes structural audit`, () => {
        const errors = welcomeCatalogErrors(SCENARIO_TABS);
        expect(
            errors,
            errors.map((e) => `${e.tabId}/${e.textEn || "-"}: ${e.message}`).join("\n"),
        ).toEqual([]);
        for (const tab of SCENARIO_TABS) {
            expect(tab.prompts.length).toBe(WELCOME_SCENARIO_PROMPTS_PER_TAB);
        }
    });

    it("icon name list is non-empty and covers every catalog icon", () => {
        expect(WELCOME_PROMPT_ICON_NAMES.length).toBeGreaterThan(10);
        const known = new Set<string>(WELCOME_PROMPT_ICON_NAMES);
        for (const tab of SCENARIO_TABS) {
            for (const p of tab.prompts) {
                expect(known.has(p.icon), `${tab.id}/${p.textEn} icon ${p.icon}`).toBe(true);
            }
        }
    });

    it("reports zero hard errors; soft warnings are allowed", () => {
        const all = auditWelcomeScenarioCatalog(SCENARIO_TABS);
        expect(all.filter((i) => i.level === "error")).toEqual([]);
        // Soft warnings (e.g. long labels) must not fail CI.
        expect(all.every((i) => i.level === "error" || i.level === "warn")).toBe(true);
    });

    it("quick hints all resolve to live scenario cards", () => {
        const resolved = resolveWelcomeQuickHints();
        expect(resolved.length).toBe(WELCOME_QUICK_HINTS.length);
        expect(resolved.length).toBeGreaterThan(0);
    });

    it("detects a broken fixture via the pure auditor", () => {
        const broken = structuredClone(SCENARIO_TABS);
        broken[0].prompts[0].template = "no slots and no deliverable";
        broken[0].prompts[0].templateEn = "no slots and no deliverable";
        const errors = welcomeCatalogErrors(broken);
        expect(errors.some((e) => e.message.includes("field count") || e.message.includes("Output"))).toBe(true);
    });
});
