import { afterEach, describe, expect, it } from "vitest";
import { applyCompanionBootTheme, publishStoredUIAppearance } from "../filecompanion/fileCompanionBootFlag";
import { publishUIAppearance } from "./uiAppearanceSync";

afterEach(() => {
    const root = document.documentElement;
    root.removeAttribute("data-ai-theme");
    root.removeAttribute("data-boot-theme");
    root.removeAttribute("data-ai-dark-scheme");
    root.removeAttribute("data-ai-light-scheme");
    root.style.colorScheme = "";
    localStorage.clear();
});

describe("applyCompanionBootTheme", () => {
    it("stamps a dark scheme before the companion window mounts", () => {
        localStorage.setItem("ai_assistant_theme_mode", "dark");
        localStorage.setItem("maclaw.ai.darkScheme", "violet");
        applyCompanionBootTheme();
        const root = document.documentElement;
        expect(root.getAttribute("data-ai-theme")).toBe("dark");
        expect(root.getAttribute("data-boot-theme")).toBe("dark");
        expect(root.getAttribute("data-ai-dark-scheme")).toBe("violet");
        expect(root.hasAttribute("data-ai-light-scheme")).toBe(false);
        expect(root.style.colorScheme).toBe("dark");
    });

    it("stamps a light scheme and clears a leftover dark scheme", () => {
        document.documentElement.setAttribute("data-ai-dark-scheme", "onyx");
        localStorage.setItem("ai_assistant_theme_mode", "light");
        localStorage.setItem("maclaw.ai.lightScheme", "notion");
        applyCompanionBootTheme();
        const root = document.documentElement;
        expect(root.getAttribute("data-ai-theme")).toBe("light");
        expect(root.getAttribute("data-ai-light-scheme")).toBe("notion");
        expect(root.hasAttribute("data-ai-dark-scheme")).toBe(false);
        expect(root.style.colorScheme).toBe("light");
    });

    it("keeps an explicit light choice when the legacy key is still dark", () => {
        localStorage.setItem("ai_assistant_theme_mode", "light");
        localStorage.setItem("maclaw.ai.themeMode", "dark");
        localStorage.setItem("maclaw.ai.lightScheme", "notion");
        applyCompanionBootTheme();
        const root = document.documentElement;
        expect(root.getAttribute("data-ai-theme")).toBe("light");
        expect(root.getAttribute("data-ai-light-scheme")).toBe("notion");
        expect(root.style.colorScheme).toBe("light");
    });
});

describe("publishUIAppearance", () => {
    afterEach(() => {
        delete (window as unknown as { go?: unknown }).go;
    });

    it("writes a later theme only after the earlier save finishes", async () => {
        const calls: string[] = [];
        let releaseFirst: () => void = () => {};
        const first = new Promise<void>((resolve) => {
            releaseFirst = resolve;
        });
        let started = 0;
        (window as unknown as { go: { main: { App: { SetUIAppearance: (value: { themeMode: string; darkScheme: string }) => Promise<void> } } } }).go = {
            main: {
                App: {
                    SetUIAppearance: (value) => {
                        calls.push(`${value.themeMode}:${value.darkScheme}`);
                        started += 1;
                        return started === 1 ? first : Promise.resolve();
                    },
                },
            },
        };
        publishUIAppearance({ themeMode: "dark", darkSchemeId: "onyx", lightSchemeId: "fluent" });
        publishUIAppearance({ themeMode: "light", darkSchemeId: "graphite", lightSchemeId: "notion" });
        const flush = async () => {
            for (let i = 0; i < 5; i += 1) await Promise.resolve();
        };
        await flush();
        expect(calls).toEqual(["dark:onyx"]);
        releaseFirst();
        await flush();
        expect(calls).toEqual(["dark:onyx", "light:graphite"]);
    });

    it("publishes the stored theme before a later choice", async () => {
        localStorage.setItem("ai_assistant_theme_mode", "dark");
        localStorage.setItem("maclaw.ai.darkScheme", "violet");
        localStorage.setItem("maclaw.ai.lightScheme", "fluent");
        const calls: string[] = [];
        let releaseFirst: () => void = () => {};
        const first = new Promise<void>((resolve) => {
            releaseFirst = resolve;
        });
        let started = 0;
        (window as unknown as { go: { main: { App: { SetUIAppearance: (value: { themeMode: string; darkScheme: string }) => Promise<void> } } } }).go = {
            main: {
                App: {
                    SetUIAppearance: (value) => {
                        calls.push(`${value.themeMode}:${value.darkScheme}`);
                        started += 1;
                        return started === 1 ? first : Promise.resolve();
                    },
                },
            },
        };
        publishStoredUIAppearance();
        publishUIAppearance({ themeMode: "light", darkSchemeId: "graphite", lightSchemeId: "notion" });
        const flush = async () => {
            for (let i = 0; i < 5; i += 1) await Promise.resolve();
        };
        await flush();
        expect(calls).toEqual(["dark:violet"]);
        releaseFirst();
        await flush();
        expect(calls).toEqual(["dark:violet", "light:graphite"]);
    });
});
