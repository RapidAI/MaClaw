import { beforeEach, describe, expect, it } from "vitest";
import {
    loadWelcomeCodingEnv,
    loadWelcomeFieldValues,
    loadWelcomeRecentEntries,
    recordWelcomeRecent,
    resolveWelcomeRecentPrompts,
    saveWelcomeCodingEnv,
    saveWelcomeFieldValues,
    welcomePromptKey,
    WELCOME_CODING_ENV_KEY,
    WELCOME_FIELD_VALUES_KEY,
    WELCOME_RECENT_KEY,
    WELCOME_RECENT_MAX,
} from "../welcomeTaskMemory";
import { SCENARIO_TABS } from "../welcomeScenarioTasks";

describe("welcomeTaskMemory", () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it("saves and loads field values by task key and label", () => {
        const key = welcomePromptKey("business", "Prepare an executive competitor brief");
        saveWelcomeFieldValues(key, { "行业/赛道": "SaaS", "我方产品": "" });
        expect(loadWelcomeFieldValues(key)).toEqual({ "行业/赛道": "SaaS" });
        expect(localStorage.getItem(WELCOME_FIELD_VALUES_KEY)).toContain("行业/赛道");
    });

    it("records recent tasks newest-first and caps length", () => {
        const samples = SCENARIO_TABS.flatMap((tab) =>
            tab.prompts.map((p) => ({ tabId: tab.id, textEn: p.textEn })),
        ).slice(0, WELCOME_RECENT_MAX + 2);

        for (const s of samples) {
            recordWelcomeRecent(s.tabId, s.textEn);
        }

        const recent = loadWelcomeRecentEntries();
        expect(recent.length).toBe(WELCOME_RECENT_MAX);
        expect(recent[0].textEn).toBe(samples[samples.length - 1].textEn);
        expect(localStorage.getItem(WELCOME_RECENT_KEY)).toBeTruthy();

        // Dedup moves existing entry to front
        const first = samples[0];
        recordWelcomeRecent(first.tabId, first.textEn);
        expect(loadWelcomeRecentEntries()[0].textEn).toBe(first.textEn);
    });

    it("resolves recent entries to live prompts", () => {
        const tab = SCENARIO_TABS[0];
        const prompt = tab.prompts[0];
        recordWelcomeRecent(tab.id, prompt.textEn);
        const resolved = resolveWelcomeRecentPrompts();
        expect(resolved).toHaveLength(1);
        expect(resolved[0].prompt.textEn).toBe(prompt.textEn);
        expect(resolved[0].tabId).toBe(tab.id);
    });

    it("stores coding env without password", () => {
        saveWelcomeCodingEnv({
            workingDir: "D:/work/proj",
            remote: { host: "10.0.0.1", port: 22, user: "u", workDir: "/app" },
        });
        expect(loadWelcomeCodingEnv()).toEqual({
            workingDir: "D:/work/proj",
            remote: { host: "10.0.0.1", port: 22, user: "u", workDir: "/app" },
        });
        expect(localStorage.getItem(WELCOME_CODING_ENV_KEY)).not.toContain("password");
    });
});
