import { describe, expect, it } from "vitest";
import { removeContextWindowUsageForSession, type ContextWindowUsage } from "../useContextWindowUsage";

describe("removeContextWindowUsageForSession", () => {
    it("drops only the requested session and preserves other live samples", () => {
        const state: Record<string, ContextWindowUsage> = {
            "desktop-user:D:/repo": { used: 42500, max: 102400, estimated: true },
            "desktop-user:D:/other": { used: 1200, max: 102400 },
        };
        const next = removeContextWindowUsageForSession(state, "desktop-user:D:/repo");
        expect(next).not.toBe(state);
        expect(next["desktop-user:D:/repo"]).toBeUndefined();
        expect(next["desktop-user:D:/other"]).toEqual({ used: 1200, max: 102400 });
        expect(state["desktop-user:D:/repo"]).toEqual({ used: 42500, max: 102400, estimated: true });
    });

    it("returns the same object when the session has no live sample", () => {
        const state: Record<string, ContextWindowUsage> = {
            "desktop-user:D:/repo": { used: 42500, max: 102400 },
        };
        expect(removeContextWindowUsageForSession(state, "desktop-user:D:/missing")).toBe(state);
        expect(removeContextWindowUsageForSession(state, "")).toBe(state);
    });
});
