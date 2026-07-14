import { beforeEach, describe, expect, it } from "vitest";
import {
    loadWelcomeUserRole,
    resolveWelcomeDefaultTab,
    saveWelcomeUserRole,
    WELCOME_ROLE_DEFAULT_TAB,
    WELCOME_USER_ROLE_KEY,
    type WelcomeRecentEntry,
} from "../welcomeTaskMemory";

describe("welcome user role defaults", () => {
    beforeEach(() => {
        localStorage.clear();
    });

    it("loads and saves role", () => {
        expect(loadWelcomeUserRole()).toBe("auto");
        saveWelcomeUserRole("dev");
        expect(loadWelcomeUserRole()).toBe("dev");
        expect(localStorage.getItem(WELCOME_USER_ROLE_KEY)).toBe("dev");
    });

    it("prefers last-used tab when valid", () => {
        expect(resolveWelcomeDefaultTab("ops", "dev")).toBe("ops");
    });

    it("uses role default when no last tab", () => {
        expect(resolveWelcomeDefaultTab(null, "dev")).toBe(WELCOME_ROLE_DEFAULT_TAB.dev);
        expect(resolveWelcomeDefaultTab(null, "writing")).toBe("writing");
    });

    it("auto role uses most common recent tab", () => {
        const recent: WelcomeRecentEntry[] = [
            { key: "a", tabId: "writing", textEn: "a", usedAt: 1 },
            { key: "b", tabId: "dev", textEn: "b", usedAt: 2 },
            { key: "c", tabId: "dev", textEn: "c", usedAt: 3 },
        ];
        expect(resolveWelcomeDefaultTab(null, "auto", recent)).toBe("dev");
    });
});
