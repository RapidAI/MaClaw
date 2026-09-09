import { describe, expect, it } from "vitest";
import type { ChatMessage } from "../useAIAssistant";
import {
    coalesceCodingAgentToolLifecycle,
    compactCodingAgentProgressMessages,
    groupCodingAgentProgressForRender,
} from "../compactCodingAgentProgressMessages";
import { isCodingAgentBoardProgressContent, reasoningHasCodingStatusMilestone, stripCodingAgentAuditSections, stripCodingWorkbenchStatusReasoning } from "../codingAgentUserFinish";

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

        // Audit banners stay off the chat trail; keep the file-change card.
        expect(compactCodingAgentProgressMessages(messages).map(m => m.id)).toEqual(["diff"]);
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

        expect(compactCodingAgentProgressMessages(messages).map(m => m.id)).toEqual(["user", "running", "assistant"]);
    });

    it("keeps a recent tool trail for the latest turn (not only failures)", () => {
        const messages = [
            message("t1", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file","outcome":"success"}'),
            message("t2", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"go test"}'),
            message("t3", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"failed","command":"where cl.exe"}'),
        ];
        expect(compactCodingAgentProgressMessages(messages).map(m => m.id)).toEqual(["t1", "t2", "t3"]);
    });

    it("drops diff_updated when a write/edit tool already covers the file", () => {
        const messages = [
            message("write", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file","outcome":"success","files":["hello_world.cpp"],"added":8,"removed":0}'),
            message("diff", 'Coding Agent Event: {"version":1,"agent":"coding","event":"diff_updated","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"Edited hello_world.cpp (+8 -0)","files":["hello_world.cpp"]}'),
            message("bash", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"cl"}'),
        ];
        expect(coalesceCodingAgentToolLifecycle(messages).map((m) => m.id)).toEqual(["write", "bash"]);
    });

    it("drops diff_summary when a write/edit tool already covers the files", () => {
        const messages = [
            message("write", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file","outcome":"success","files":["hello_world.cpp"],"added":8,"removed":0}'),
            message("summary", 'Coding Agent Event: {"version":1,"agent":"coding","event":"diff_summary","phase":"result","task_id":"T1","title":"Fix","turn_id":"turn-1","files":["hello_world.cpp"],"file_changes":[{"path":"hello_world.cpp","added":8,"removed":0}]}'),
            message("bash", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"cl"}'),
        ];
        expect(coalesceCodingAgentToolLifecycle(messages).map((m) => m.id)).toEqual(["write", "bash"]);
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
        const messages = Array.from({ length: 21 }, (_, i) =>
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
        expect(ids).not.toContain("old-fail");
        expect(ids.filter((id) => id.startsWith("ok-")).length).toBe(20);
    });

    it("does not retain older critical tool failures", () => {
        const fails = Array.from({ length: 10 }, (_, i) =>
            message(
                `fail-${i}`,
                `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"failed","command":"cmd ${i}"}`,
            ),
        );
        const oks = Array.from({ length: 21 }, (_, i) =>
            message(
                `ok-${i}`,
                `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"echo ${i}"}`,
            ),
        );
        const ids = compactCodingAgentProgressMessages([...fails, ...oks]).map((m) => m.id);
        const keptFails = ids.filter((id) => id.startsWith("fail-"));
        expect(keptFails.length).toBe(0);
        expect(ids.filter((id) => id.startsWith("ok-")).length).toBe(20);
    });

    it("does not retain older diagnostic probes outside the recent window", () => {
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
        const oks = Array.from({ length: 21 }, (_, i) =>
            message(
                `ok-${i}`,
                `Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success","command":"echo ${i}"}`,
            ),
        );
        const ids = compactCodingAgentProgressMessages([...probes, realFail, ...oks]).map((m) => m.id);
        expect(ids).not.toContain("real-fail");
        expect(ids.filter((id) => id.startsWith("probe-")).length).toBe(0);
    });

    it("hides audit banners and keeps the tool trail plus terminal status", () => {
        const messages = [
            message("tool-1", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"read_file","outcome":"success"}'),
            message("summary-1", 'Coding Agent Event: {"version":1,"agent":"coding","event":"exploration_summary","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","outcome":"missing"}'),
            message("tool-2", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"write_file","outcome":"success"}'),
            message("summary-2", 'Coding Agent Event: {"version":1,"agent":"coding","event":"quality_summary","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","outcome":"failed"}'),
            message("summary-3", 'Coding Agent Event: {"version":1,"agent":"coding","event":"command_summary","phase":"result","task_id":"T1","title":"Fix","turn_id":"turn-1","outcome":"failed","summary":"1 failed"}'),
            message("tool-3", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success"}'),
            message("done", 'Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"completed","task_id":"T1","title":"Fix","turn_id":"turn-1"}'),
        ];

        expect(compactCodingAgentProgressMessages(messages).map((m) => m.id)).toEqual([
            "tool-1",
            "tool-2",
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

    it("keeps mid-turn engineer notes and drops leftover audit notes", () => {
        const messages = [
            message("note-ok", 'Coding Agent Event: {"version":1,"agent":"coding","event":"assistant_note","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"Compiling the new hello world."}'),
            message("note-audit", 'Coding Agent Event: {"version":1,"agent":"coding","event":"assistant_note","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"## 执行报告 总计：1"}'),
            message("tool", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success"}'),
            message("done", 'Coding Agent Event: {"version":1,"agent":"coding","event":"task_status","phase":"completed","task_id":"T1","title":"Fix","turn_id":"turn-1"}'),
        ];
        expect(compactCodingAgentProgressMessages(messages).map((m) => m.id)).toEqual([
            "note-ok",
            "tool",
            "done",
        ]);
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

    it("drops workbench board banners and keeps coding-agent trail lines", () => {
        const messages = [
            message("banner", "全功能编程工作台：开始执行"),
            message("checklist", "执行步骤：\n☐ T1 write files"),
            message("step", "T1/2: write files"),
            message("tool", 'Coding Agent Event: {"version":1,"agent":"coding","event":"tool_finished","phase":"running","task_id":"T1","title":"Fix","turn_id":"turn-1","detail":"bash","outcome":"success"}'),
        ];
        expect(compactCodingAgentProgressMessages(messages).map((m) => m.id)).toEqual(["tool"]);
        expect(isCodingAgentBoardProgressContent("全功能远程编程：使用 SSH 会话 ssh_1 开始执行")).toBe(true);
        expect(isCodingAgentBoardProgressContent("Coding Agent: running T1 - write files")).toBe(false);
        expect(isCodingAgentBoardProgressContent("正在生成报告")).toBe(false);
    });
});

describe("stripCodingAgentAuditSections", () => {
    it("cuts streamed audit and plan-board headings from assistant text", () => {
        expect(stripCodingAgentAuditSections("Created hello.cpp.\n\n## 验证结果\ncl passed\n\n## 涉及文件\nhello.cpp")).toBe("Created hello.cpp.");
        expect(stripCodingAgentAuditSections("Created hello.cpp.\n\n### T2: build\n状态: success")).toBe("Created hello.cpp.");
        expect(stripCodingAgentAuditSections("Created hello.cpp.\n\n## Summary\nall good")).toBe("Created hello.cpp.\n\n## Summary\nall good");
    });

    it("keeps pending plan-approval steps", () => {
        const card = "## \u9700\u8981\u786e\u8ba4\u6267\u884c\u8ba1\u5212\n\nTwo steps.\n\n### T1: write\n### T2: build\n\n`/plan approve`";
        expect(stripCodingAgentAuditSections(card)).toBe(card);
    });
});

describe("stripCodingWorkbenchStatusReasoning", () => {
    it("drops chat status bullets including the early acknowledgement", () => {
        const reasoning = "\u2022 \u6536\u5230\uff0c\u6b63\u5728\u5904\u7406\n\u2022 Task received\nI'll write hello.cpp.";
        expect(reasoningHasCodingStatusMilestone(reasoning)).toBe(true);
        expect(stripCodingWorkbenchStatusReasoning(reasoning)).toBe("I'll write hello.cpp.");
        expect(stripCodingWorkbenchStatusReasoning("\u2022 \u6536\u5230\uff0c\u6b63\u5728\u5904\u7406")).toBe("");
        expect(stripCodingWorkbenchStatusReasoning("\u6536\u5230\uff0c\u6b63\u5728\u5904\u7406")).toBe("");
        expect(stripCodingWorkbenchStatusReasoning("[Status] Task received")).toBe("");
    });
});
