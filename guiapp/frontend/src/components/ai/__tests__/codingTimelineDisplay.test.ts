import { describe, expect, it } from "vitest";
import { codingLiveHeaderProgress, codingTimelineForDisplay, latestOpenCodingTool, progressMessagesForStage } from "../codingTimelineDisplay";

const bashStart = 'Coding Agent Event: {"agent":"coding","event":"tool_started","phase":"running","detail":"bash","command":"git status --short"}';
const bashFinish = 'Coding Agent Event: {"agent":"coding","event":"tool_finished","phase":"running","detail":"bash","outcome":"ok"}';
const writeStart = 'Coding Agent Event: {"agent":"coding","event":"tool_started","phase":"running","detail":"write_file"}';

describe("codingTimelineDisplay", () => {
    it("paints pending thoughts before a coding timeline exists", () => {
        const thought = { id: "t1", sequence: 1, kind: "thinking" as const, content: "先核对 AVX2 的测量路径。", timestamp: 1 };
        const pending = [thought];
        const first = codingTimelineForDisplay({ pendingCodingThoughts: pending });
        expect(first).toBe(pending);
        expect(codingTimelineForDisplay({ pendingCodingThoughts: pending })).toBe(first);
    });

    it("skips a blank pending thought and keeps an existing timeline", () => {
        const timeline = [{ id: "p1", sequence: 2, kind: "progress" as const, content: bashStart, timestamp: 2 }];
        expect(codingTimelineForDisplay({
            codingTimeline: timeline,
            pendingCodingThoughts: [{ id: "t1", sequence: 1, kind: "thinking", content: "---\n### T1: 标题\n", timestamp: 1 }],
        })).toBe(timeline);
        const mixed = [
            { id: "blank", sequence: 1, kind: "thinking" as const, content: "---\n### T1: 标题\n", timestamp: 1 },
            { id: "t2", sequence: 2, kind: "thinking" as const, content: "先核对测量路径。", timestamp: 2 },
        ];
        const visible = codingTimelineForDisplay({ pendingCodingThoughts: mixed });
        expect(visible?.map(item => item.id)).toEqual(["t2"]);
        expect(codingTimelineForDisplay({ pendingCodingThoughts: mixed })).toBe(visible);
        expect(codingTimelineForDisplay({
            pendingCodingThoughts: [{ id: "t1", sequence: 1, kind: "thinking", content: "---\n### T1: 标题\n", timestamp: 1 }],
        })).toBeUndefined();
    });

    it("keeps the tool a later finish has not closed", () => {
        expect(latestOpenCodingTool([
            { kind: "thinking", content: "先看目录。" },
            { kind: "progress", content: bashStart },
        ])?.detail).toBe("bash");
        expect(latestOpenCodingTool([
            { kind: "progress", content: bashStart },
            { kind: "progress", content: bashFinish },
        ])).toBeNull();
        expect(latestOpenCodingTool([
            { kind: "progress", content: bashStart },
            { kind: "progress", content: bashFinish },
            { kind: "progress", content: writeStart },
        ])?.detail).toBe("write_file");
    });

    it("keeps an open command until this stage starts a newer tool", () => {
        const timeline = [
            { kind: "progress" as const, content: bashStart },
        ];
        const foreign = { event: "tool_started", detail: "edit_file", phase: "running" as const, title: "" };
        expect(codingLiveHeaderProgress(foreign, timeline)?.detail).toBe("bash");
        const same = { event: "tool_started", detail: "bash", phase: "running" as const, title: "", command: "git status --short" };
        expect(codingLiveHeaderProgress(same, timeline)).toBe(same);
        const finished = { event: "tool_finished", detail: "bash", phase: "running" as const, title: "", outcome: "ok" };
        expect(codingLiveHeaderProgress(finished, timeline)?.detail).toBe("bash");
        expect(codingLiveHeaderProgress(finished, [{ kind: "progress", content: bashFinish }])).toBe(finished);
        expect(codingLiveHeaderProgress(foreign, timeline, { tool: "edit_file" })).toBe(foreign);
        expect(codingLiveHeaderProgress(null, undefined, { tool: "bash" })).toMatchObject({ event: "tool_started", detail: "bash" });
        expect(codingLiveHeaderProgress(same, timeline, { tool: "bash" })).toBe(same);
        const stage = { role: "assistant", sessionKey: "desktop-user:C:/tasks/textproc" };
        const kept = progressMessagesForStage([
            { sessionKey: "desktop-user:C:\\tasks\\textproc", content: bashStart },
            { sessionKey: "desktop-user:D:/tasks/other", content: writeStart },
            { content: bashFinish },
        ], [stage], "desktop-user:E:/workprj/app");
        expect(kept.map(row => row.content)).toEqual([bashStart, bashFinish]);
    });
});
