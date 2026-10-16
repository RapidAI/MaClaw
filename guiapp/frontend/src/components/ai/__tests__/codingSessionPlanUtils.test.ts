import { describe, expect, it } from "vitest";
import {
    formatGoalSessionPlan,
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

    it("formats a goal understanding and numbered steps for the session plan", () => {
        const plan = formatGoalSessionPlan({
            objective: "实现登录",
            understanding: "交付可登录的接口",
            steps: [{ index: 1, title: "编写接口" }, { title: "补充测试" }],
        });
        expect(plan).toBe("交付可登录的接口\n1. 编写接口\n2. 补充测试");
    });

    it("truncates long plans", () => {
        const long = "x".repeat(900);
        const got = truncateSessionPlan(long, 100);
        expect(got.length).toBeLessThanOrEqual(100);
        expect(got.endsWith("…")).toBe(true);
    });
});
