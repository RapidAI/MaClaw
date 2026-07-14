import { describe, expect, it } from "vitest";
import {
    matchWelcomeTasksFromClipboard,
    pickClipboardPrefillLabel,
} from "../welcomeClipboardSuggest";

describe("matchWelcomeTasksFromClipboard", () => {
    it("returns empty for short or empty text", () => {
        expect(matchWelcomeTasksFromClipboard("")).toEqual([]);
        expect(matchWelcomeTasksFromClipboard("hello")).toEqual([]);
    });

    it("suggests ops/dev tasks for stack traces", () => {
        const text = `
Traceback (most recent call last):
  File "app.py", line 42, in handler
    raise RuntimeError("boom")
RuntimeError: boom
`;
        const hits = matchWelcomeTasksFromClipboard(text, 3);
        expect(hits.length).toBeGreaterThan(0);
        expect(hits.some((h) => h.tabId === "ops" || h.tabId === "dev")).toBe(true);
        expect(hits[0].score).toBeGreaterThan(0);
        expect(hits[0].reason).toBeTruthy();
    });

    it("suggests data tasks for CSV-like content", () => {
        const text = [
            "date,revenue,orders",
            "2026-01-01,1200,30",
            "2026-01-02,1500,40",
            "2026-01-03,900,20",
            "收入下降需要分析",
        ].join("\n");
        const hits = matchWelcomeTasksFromClipboard(text, 3);
        expect(hits.some((h) => h.tabId === "data" || h.tabId === "business")).toBe(true);
    });

    it("suggests academic tasks for grant materials", () => {
        const text = "国家自然科学基金 优青 立项依据 研究内容 技术路线 预期成果 NSFC application draft";
        const hits = matchWelcomeTasksFromClipboard(text, 3);
        expect(hits.some((h) => h.tabId === "academic-application" || h.tabId === "research")).toBe(true);
    });

    it("suggests writing tasks for emails", () => {
        const text = "Subject: Project update\n\nDear team,\nPlease review the following and reply.\nBest regards,\nAlex";
        const hits = matchWelcomeTasksFromClipboard(text, 3);
        expect(hits.some((h) => h.tabId === "writing")).toBe(true);
    });
});

describe("pickClipboardPrefillLabel", () => {
    it("prefers paste-like labeled fields", () => {
        const template = "主题：[主题]\n会议记录：[粘贴转写或要点]\n请输出纪要。";
        expect(pickClipboardPrefillLabel(template)).toBe("会议记录");
    });

    it("falls back to first labeled field", () => {
        expect(pickClipboardPrefillLabel("目标：[希望达成什么]\n约束：[时间]")).toBe("目标");
    });
});
