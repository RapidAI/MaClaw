import { beforeEach, describe, expect, it } from "vitest";
import {
    loadWelcomeCodingEnv,
    loadWelcomeFieldValues,
    loadWelcomeRecentEntries,
    mergeWelcomeStoredCodingEnv,
    normalizeWelcomeStoredCodingEnv,
    recordWelcomeRecent,
    resolveWelcomeCodingEnvForSave,
    resolveWelcomeRecentPrompts,
    saveWelcomeCodingEnv,
    saveWelcomeFieldValues,
    stripCodingEnvPassword,
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

    it("stores coding env including password locally", () => {
        saveWelcomeCodingEnv({
            workingDir: "D:/work/proj",
            remote: { host: "10.0.0.1", port: 22, user: "u", workDir: "/app", password: "p@ss" },
        });
        expect(loadWelcomeCodingEnv()).toEqual({
            workingDir: "D:/work/proj",
            remote: { host: "10.0.0.1", port: 22, user: "u", workDir: "/app", password: "p@ss" },
        });
        expect(localStorage.getItem(WELCOME_CODING_ENV_KEY)).toContain("p@ss");
        // Same host+user partial update without password keeps previous password.
        saveWelcomeCodingEnv({
            remote: { host: "10.0.0.1", port: 22, user: "u", workDir: "/srv" },
        });
        expect(loadWelcomeCodingEnv().remote).toEqual({
            host: "10.0.0.1",
            port: 22,
            user: "u",
            workDir: "/srv",
            password: "p@ss",
        });
        // Different host must not inherit the previous password.
        saveWelcomeCodingEnv({
            remote: { host: "10.0.0.2", port: 22, user: "u2", workDir: "/srv" },
        });
        expect(loadWelcomeCodingEnv().remote).toEqual({
            host: "10.0.0.2",
            port: 22,
            user: "u2",
            workDir: "/srv",
        });
        expect(loadWelcomeCodingEnv().remote?.password).toBeUndefined();
    });

    it("merges coding env password only when host+user match", () => {
        const preferred = {
            remote: { host: "192.168.1.10", port: 22, user: "ubuntu", workDir: "/app" },
        };
        const sameHost = {
            remote: {
                host: "192.168.1.10",
                port: 22,
                user: "ubuntu",
                workDir: "/old",
                password: "secret",
            },
        };
        expect(mergeWelcomeStoredCodingEnv(preferred, sameHost)?.remote?.password).toBe("secret");
        expect(mergeWelcomeStoredCodingEnv(preferred, {
            remote: { host: "10.0.0.1", port: 22, user: "ubuntu", workDir: "/app", password: "secret" },
        })?.remote?.password).toBeUndefined();
        expect(stripCodingEnvPassword(sameHost)?.remote?.password).toBeUndefined();
        expect(stripCodingEnvPassword(sameHost)?.remote?.host).toBe("192.168.1.10");
    });

    it("resolveWelcomeCodingEnvForSave distinguishes omit vs explicit clear", () => {
        const previous = {
            remote: {
                host: "192.168.1.10",
                port: 22,
                user: "ubuntu",
                workDir: "/app",
                password: "secret",
            },
        };
        // Omitted password → keep previous.
        expect(resolveWelcomeCodingEnvForSave({
            remote: { host: "192.168.1.10", port: 22, user: "ubuntu", workDir: "/app" },
        }, previous)?.remote?.password).toBe("secret");
        // Explicit empty → clear.
        expect(resolveWelcomeCodingEnvForSave({
            remote: { host: "192.168.1.10", port: 22, user: "ubuntu", workDir: "/app", password: "" },
        }, previous)?.remote?.password).toBeUndefined();
        // No input env → keep previous entirely.
        expect(resolveWelcomeCodingEnvForSave(undefined, previous)).toEqual(previous);
    });

    it("drops orphan password without host/user and clamps port", () => {
        expect(normalizeWelcomeStoredCodingEnv({
            remote: { host: "", port: 22, user: "", workDir: "", password: "lonely" },
        })).toBeUndefined();
        expect(normalizeWelcomeStoredCodingEnv({
            remote: { host: "10.0.0.1", port: 99999, user: "u", workDir: "/a", password: "x" },
        })?.remote).toEqual({
            host: "10.0.0.1",
            port: 22,
            user: "u",
            workDir: "/a",
            password: "x",
        });
    });
});
