import { describe, expect, it } from "vitest";
import {
    normalizeSessionPlanCandidate,
    suggestSessionPlanFromMessages,
    truncateSessionPlan,
} from "../codingSessionPlanUtils";

describe("codingSessionPlanUtils", () => {
    it("normalizes whitespace and strips fenced code", () => {
        const got = normalizeSessionPlanCandidate("  Fix auth\n\n```go\npackage main\n```\n");
        expect(got).toBe("Fix auth");
    });

    it("prefers the earliest substantial user message as the plan", () => {
        const plan = suggestSessionPlanFromMessages([
            { role: "assistant", content: "hello" },
            { role: "user", content: "Implement JWT auth for the API" },
            { role: "user", content: "also add refresh tokens" },
        ]);
        expect(plan).toContain("Implement JWT auth");
    });

    it("truncates long plans", () => {
        const long = "x".repeat(900);
        const got = truncateSessionPlan(long, 100);
        expect(got.length).toBeLessThanOrEqual(100);
        expect(got.endsWith("…")).toBe(true);
    });
});
