import { describe, expect, it } from "vitest";
import type { ChatMessage } from "../useAIAssistant";
import {
    coalesceCodingAgentToolLifecycle,
    compactCodingAgentProgressMessages,
    groupCodingAgentProgressForRender,
} from "../compactCodingAgentProgressMessages";

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

        // task_status is dropped once activity/summary lines exist for the turn.
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

    it("keeps a recent tool trail for the latest turn (not only failures)", () => {
        const messages = [
            message("t1", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file","outcome":"success"}'),
            message("t2", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"go test"}'),
            message("t3", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"failed","command":"where cl.exe"}'),
        ];
        expect(compactCodingAgentProgressMessages(messages).map(m => m.id)).toEqual(["t1", "t2", "t3"]);
    });

    it("coalesces tool_started into tool_finished for the same tool", () => {
        const messages = [
            message("start-bash", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash"}'),
            message("fin-bash", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"go test","duration_ms":1200}'),
            message("start-write", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file"}'),
        ];
        expect(coalesceCodingAgentToolLifecycle(messages).map(m => m.id)).toEqual(["fin-bash", "start-write"]);
        expect(compactCodingAgentProgressMessages(messages).map(m => m.id)).toEqual(["fin-bash", "start-write"]);
    });

    it("drops intermediate task_status when tool lines exist", () => {
        const messages = [
            message("status", 'Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1"}'),
            message("tool", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success"}'),
        ];
        expect(compactCodingAgentProgressMessages(messages).map(m => m.id)).toEqual(["tool"]);
    });

    it("keeps terminal task_status after tools so phase is not stuck on running", () => {
        const messages = [
            message("running", 'Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1"}'),
            message("tool", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success"}'),
            message("done", 'Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"completed","task_id":"T1","title":"Fix","turn_id":"turn-1"}'),
        ];
        expect(compactCodingAgentProgressMessages(messages).map(m => m.id)).toEqual(["tool", "done"]);
        expect(coalesceCodingAgentToolLifecycle(messages).map(m => m.id)).toEqual(["tool", "done"]);
    });

    it("pairs multiple same-name tools LIFO when coalescing starts", () => {
        const messages = [
            message("s1", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash"}'),
            message("f1", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success"}'),
            message("s2", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_started","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash"}'),
            message("f2", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"failed","command":"cl"}'),
        ];
        expect(coalesceCodingAgentToolLifecycle(messages).map(m => m.id)).toEqual(["f1", "f2"]);
    });

    it("caps the tool trail after coalesce without retaining older failures", () => {
        const messages = Array.from({ length: 15 }, (_, i) =>
            message(
                `ok-${i}`,
                `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"echo ${i}"}`,
            ),
        );
        messages.unshift(
            message(
                "old-fail",
                'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"failed","command":"cl"}',
            ),
        );
        const ids = compactCodingAgentProgressMessages(messages).map((m) => m.id);
        // The live tray only retains the latest three operations.
        expect(ids).not.toContain("old-fail");
        expect(ids.filter((id) => id.startsWith("ok-")).length).toBe(3);
    });

    it("does not retain older critical tool failures", () => {
        const fails = Array.from({ length: 10 }, (_, i) =>
            message(
                `fail-${i}`,
                `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"failed","command":"cmd ${i}"}`,
            ),
        );
        const oks = Array.from({ length: 12 }, (_, i) =>
            message(
                `ok-${i}`,
                `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"echo ${i}"}`,
            ),
        );
        const ids = compactCodingAgentProgressMessages([...fails, ...oks]).map((m) => m.id);
        const keptFails = ids.filter((id) => id.startsWith("fail-"));
        // Older failures do not expand the compact live tray.
        expect(keptFails.length).toBe(0);
        expect(ids.filter((id) => id.startsWith("ok-")).length).toBe(3);
    });

    it("does not retain older diagnostic probes", () => {
        const probes = Array.from({ length: 8 }, (_, i) =>
            message(
                `probe-${i}`,
                `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"failed","command":"where.exe cl.exe","severity":"diagnostic"}`,
            ),
        );
        const realFail = message(
            "real-fail",
            'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"failed","command":"go test ./..."}',
        );
        const oks = Array.from({ length: 12 }, (_, i) =>
            message(
                `ok-${i}`,
                `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"echo ${i}"}`,
            ),
        );
        const ids = compactCodingAgentProgressMessages([...probes, realFail, ...oks]).map((m) => m.id);
        // Nothing outside the newest three rows is retained, including failures.
        expect(ids).not.toContain("real-fail");
        // Probes outside the recent window are not force-preserved as "critical".
        expect(ids.filter((id) => id.startsWith("probe-")).length).toBe(0);
    });

    it("caps summaries and tools together at three visible activity rows", () => {
        const messages = [
            message("tool-1", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"read_file","outcome":"success"}'),
            message("summary-1", 'Coding Agent Event: {"version":1,"agent":"coding","event":"exploration_summary","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","outcome":"missing"}'),
            message("tool-2", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file","outcome":"success"}'),
            message("summary-2", 'Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","outcome":"failed"}'),
            message("tool-3", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success"}'),
            message("done", 'Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"completed","task_id":"T1","title":"Fix","turn_id":"turn-1"}'),
        ];

        expect(compactCodingAgentProgressMessages(messages).map((m) => m.id)).toEqual([
            "tool-2",
            "summary-2",
            "tool-3",
            "done",
        ]);
    });

    it("groups consecutive coding progress into a feed item (including singles)", () => {
        const messages = [
            { ...message("user", "hi"), role: "user" as const },
            message("t1", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"failed","command":"cl"}'),
            message("t2", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file","outcome":"success"}'),
            { ...message("assistant", "done"), role: "assistant" as const },
        ];
        const grouped = groupCodingAgentProgressForRender(messages);
        expect(grouped.map((g) => g.kind)).toEqual(["message", "coding-feed", "message"]);
        if (grouped[1].kind === "coding-feed") {
            expect(grouped[1].messages.map((m) => m.id)).toEqual(["t1", "t2"]);
        }

        const single = groupCodingAgentProgressForRender([messages[1]]);
        expect(single.map((g) => g.kind)).toEqual(["coding-feed"]);
    });

    it("does not merge scoped historical turns into an unscoped latest event", () => {
        const messages = [
            message(
                "old-scoped",
                'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Old","turn_id":"turn-old","detail":"bash","outcome":"failed","command":"go test"}',
            ),
            // Latest has no turn/task identifiers (malformed / partial) — must not revive turn-old.
            message(
                "unscoped",
                'Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"running","title":"Now"}',
            ),
        ];
        expect(compactCodingAgentProgressMessages(messages).map((m) => m.id)).toEqual(["unscoped"]);
    });

    it("keeps a stable feed key across streaming tool events in the same turn", () => {
        const a = message(
            "a",
            'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-99","detail":"bash","outcome":"success"}',
        );
        const b = message(
            "b",
            'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-99","detail":"write_file","outcome":"success"}',
        );
        const g1 = groupCodingAgentProgressForRender([a]);
        const g2 = groupCodingAgentProgressForRender([a, b]);
        expect(g1[0].kind).toBe("coding-feed");
        expect(g2[0].kind).toBe("coding-feed");
        if (g1[0].kind === "coding-feed" && g2[0].kind === "coding-feed") {
            expect(g1[0].key).toBe(g2[0].key);
            expect(g1[0].key).toBe("feed-turn-turn-99");
        }
    });
});
