import { describe, expect, it } from "vitest";
import type { ChatMessage } from "../useAIAssistant";
import { compactCodingAgentProgressMessages } from "../compactCodingAgentProgressMessages";

const message = (id: string, content: string): ChatMessage => ({
    id,
    role: "progress",
    content,
    timestamp: 1,
});

describe("compactCodingAgentProgressMessages", () => {
    it("keeps critical quality failures from the latest coding turn", () => {
        const messages = [
            message("start", 'Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"running","task_id":"T1","title":"Fix task","run_id":"run-1","turn_id":"turn-1"}'),
            message("quality", 'Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"result","task_id":"T1","title":"Fix task","run_id":"run-1","turn_id":"turn-1","outcome":"failed","summary":"verification not run","count":1}'),
            message("diff", 'Coding Agent Event: {"version":1,"agent":"coding","event":"diff_summary","phase":"result","task_id":"T1","title":"Fix task","run_id":"run-1","turn_id":"turn-1","detail":"2 files","count":2}'),
        ];

        expect(compactCodingAgentProgressMessages(messages).map(m => m.id)).toEqual(["quality", "diff"]);
    });

    it("does not keep non-critical summaries or older-turn failures", () => {
        const messages = [
            message("old-quality", 'Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"result","task_id":"T1","title":"Old task","run_id":"run-1","turn_id":"turn-1","outcome":"failed","summary":"verification not run","count":1}'),
            message("new-quality-pass", 'Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"result","task_id":"T2","title":"New task","run_id":"run-2","turn_id":"turn-2","outcome":"passed","summary":"clean","count":0}'),
            message("new-diff", 'Coding Agent Event: {"version":1,"agent":"coding","event":"diff_summary","phase":"result","task_id":"T2","title":"New task","run_id":"run-2","turn_id":"turn-2","detail":"1 file","count":1}'),
        ];

        expect(compactCodingAgentProgressMessages(messages).map(m => m.id)).toEqual(["new-diff"]);
    });

    it("keeps ordinary messages around compacted progress rows", () => {
        const messages = [
            { ...message("user", "Please fix this"), role: "user" as const },
            message("running", 'Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"running","task_id":"T1","title":"Fix task","turn_id":"turn-1"}'),
            message("quality", 'Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"result","task_id":"T1","title":"Fix task","turn_id":"turn-1","outcome":"failed","summary":"verification not run","count":1}'),
            { ...message("assistant", "I will summarize the failure."), role: "assistant" as const },
        ];

        expect(compactCodingAgentProgressMessages(messages).map(m => m.id)).toEqual(["user", "quality", "assistant"]);
    });
});
