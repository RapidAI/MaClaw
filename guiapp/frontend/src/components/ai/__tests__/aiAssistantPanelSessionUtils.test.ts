import { describe, expect, it } from "vitest";
import { ACP_ASSISTANT_TAB_IDENTITY, activeAssistantTaskIdentity, archiveExpertTranscriptForProject, assistantSessionIsSending, buildProjectTabRecentMessages, chatHistoriesEquivalent, collapseHostGoalChecklist, coerceActiveAssistantTask, expertIDFromTaskTags, fileTerminalResponseWithoutRound, formatGoalChecklistFromRecord, goalChecklistMessageId, isACPAssistantSessionKey, isAutoACPAssistantTabTaskItem, isVEAssistantTabTaskItem, messageAfterSessionFloor, messageBelongsToSession, nextGoalChecklistHistory, noteProjectTabLiveHistory, normalizeAssistantSessionKey, normalizeProjectSessionPath, projectPathFromSessionKey, projectSessionKey, projectTabLiveMessages, purgeDeletedExpertTabLocalCache, purgeDeletedProjectTabLocalCache, sameActiveAssistantTask, shouldBlockAssistantTabOpenOnTaskRegistration, transcriptLiveWorkSessionKey, upsertGoalChecklistInHistory, visibleProjectTranscript } from "../aiAssistantPanelSessionUtils";
import type { ChatMessage } from "../useAIAssistant";

describe("aiAssistantPanelSessionUtils", () => {
    it("does not treat another session's busy flag as this tab sending", () => {
        const sessionKey = "desktop-user:D:/tasks/goal-trail";
        expect(assistantSessionIsSending({
            sessionKey,
            sending: true,
            sendingSessionKey: "desktop-user",
            busySessionKeys: ["desktop-user"],
        })).toBe(false);
        expect(assistantSessionIsSending({
            sessionKey,
            sending: true,
            sendingSessionKey: "desktop-user:D:\\tasks\\goal-trail",
            busySessionKeys: ["desktop-user:D:\\tasks\\goal-trail"],
        })).toBe(true);
        expect(assistantSessionIsSending({
            sessionKey,
            sending: true,
            sendingSessionKey: sessionKey,
        })).toBe(true);
        expect(assistantSessionIsSending({
            sessionKey,
            sending: true,
            sendingSessionKey: "desktop-user",
        })).toBe(false);
        expect(assistantSessionIsSending({
            sessionKey,
            sending: true,
            fallbackRoundActive: true,
        })).toBe(true);
        expect(assistantSessionIsSending({
            sessionKey,
            sending: false,
            panelInFlightKeys: [sessionKey],
            busySessionKeys: [],
        })).toBe(true);
    });

    it("finds the live round painting this transcript under another session key", () => {
        const tabKey = "desktop-user:D:/tasks/indra-live";
        const roundKey = "desktop-user:D:/tasks/indra-live/.maclaw/data/tasks/qnuni/workspace";
        const rows: Array<Pick<ChatMessage, "role" | "sessionKey">> = [
            { role: "user", sessionKey: roundKey },
            { role: "assistant", sessionKey: roundKey },
        ];
        expect(transcriptLiveWorkSessionKey(rows, [roundKey])).toBe(roundKey);
        // The same row under the tab key is the ordinary case.
        expect(transcriptLiveWorkSessionKey(rows, [tabKey])).toBe("");
        expect(transcriptLiveWorkSessionKey(rows, [])).toBe("");
    });

    it("still finds the live assistant row when a steer or follow-up user row sits above it", () => {
        const liveKey = "desktop-user:D:/tasks/indra-live";
        const rows: Array<Pick<ChatMessage, "role" | "sessionKey">> = [
            { role: "user", sessionKey: liveKey },
            { role: "assistant", sessionKey: liveKey },
            // A steer injection and a later follow-up append user rows after
            // the assistant row that the running round is still painting.
            { role: "user", sessionKey: liveKey },
            { role: "user", sessionKey: "desktop-user:D:/tasks/other-tab" },
        ];
        expect(transcriptLiveWorkSessionKey(rows, [liveKey])).toBe(liveKey);
    });

    it("ignores assistant rows whose session is not running", () => {
        const rows: Array<Pick<ChatMessage, "role" | "sessionKey">> = [
            { role: "user", sessionKey: "desktop-user:D:/tasks/finished" },
            { role: "assistant", sessionKey: "desktop-user:D:/tasks/finished" },
        ];
        expect(transcriptLiveWorkSessionKey(rows, ["desktop-user:D:/tasks/running"])).toBe("");
    });

    it("does not scan past the bounded tail window", () => {
        const liveKey = "desktop-user:D:/tasks/old";
        const rows: Array<Pick<ChatMessage, "role" | "sessionKey">> = [
            { role: "assistant", sessionKey: liveKey },
            ...Array.from({ length: 24 }, () => ({ role: "assistant" as const, sessionKey: "desktop-user:D:/tasks/other" })),
        ];
        expect(transcriptLiveWorkSessionKey(rows, [liveKey])).toBe("");
    });

    it("persists an async guide-injection marker and its session owner", () => {
        const plain: ChatMessage = {
            id: "same-message",
            role: "user",
            content: "redirect the running task",
            sessionKey: "desktop-user:D:/tasks/demo",
            timestamp: 1,
        };
        const injected: ChatMessage = { ...plain, kind: "guideInjection" };

        expect(chatHistoriesEquivalent([plain], [injected])).toBe(false);
        expect(chatHistoriesEquivalent([plain], [{ ...plain, sessionKey: "desktop-user:D:/tasks/other" }])).toBe(false);
        expect(chatHistoriesEquivalent([plain], [{ ...plain, reasoning: "a later streamed detail" }])).toBe(false);
        expect(chatHistoriesEquivalent([plain], [{ ...plain, actions: [{ label: "Apply", command: "apply", style: "primary" }] }])).toBe(false);
        expect(chatHistoriesEquivalent([plain], [plain])).toBe(true);
        expect(chatHistoriesEquivalent([plain], [{ ...plain, content: "redirect the RUNNING task" }])).toBe(false);
        const thought = { id: "t1", sequence: 1, kind: "thinking" as const, content: "short", timestamp: 1 };
        const before: ChatMessage = {
            id: "a",
            role: "assistant",
            content: "",
            reasoning: "short",
            timestamp: 1,
            codingTimeline: [thought],
        };
        const grownThought = { ...thought, content: "short more" };
        const after: ChatMessage = { ...before, reasoning: "short more", codingTimeline: [grownThought] };
        expect(chatHistoriesEquivalent([before], [after])).toBe(false);
        expect(chatHistoriesEquivalent([after], [{ ...after, codingTimeline: [...(after.codingTimeline || [])] }])).toBe(true);
    });

    it("shows one goal checklist above the current reply", () => {
        const userGoal = "/goal  设计并实现一个愤怒的小鸟游戏，图形版，使用c++语言，有物理效果，画面要精致。";
        const collapsed = collapseHostGoalChecklist([
            { id: "ask", role: "user", content: userGoal, timestamp: 1 },
            { id: "created", role: "assistant", content: "目标已创建", timestamp: 2 },
            { id: "plan", role: "user", content: "/goal 分解目标：设计并实现一个愤怒的小鸟游戏\n进度：第 1/50 轮", timestamp: 3 },
            { id: "plan-reply", role: "assistant", content: "已分解", timestamp: 4 },
            { id: "step1", role: "user", content: "/goal 执行步骤 1：环境与依赖盘点，锁定技术栈\n进度：第 2/50 轮", timestamp: 5 },
            { id: "step1-reply", role: "assistant", content: "环境已盘点", timestamp: 6 },
            { id: "step3", role: "user", content: "/goal 执行步骤 3：物理内核实现\n进度：第 4/50 轮", timestamp: 7 },
            { id: "step3-reply", role: "assistant", content: "物理内核已完成", timestamp: 8 },
            { id: "step4", role: "user", content: "/goal 执行步骤 4：渲染与美术表现层\n进度：第 5/50 轮", timestamp: 9 },
            { id: "step4-reply", role: "assistant", content: "正在画渲染层", timestamp: 10 },
        ]);
        expect(collapsed.map(message => message.id)).toEqual([
            "ask", "created", "plan-reply", "step1-reply", "step3-reply", "step4", "step4-reply",
        ]);
        expect(collapsed.find(message => message.id === "ask")?.role).toBe("user");
        expect(collapsed.find(message => message.id === "ask")?.content).toBe(userGoal);
        const card = collapsed.find(message => message.id === "step4");
        expect(card?.role).toBe("system");
        expect(card?.kind).toBe("goalChecklist");
        expect(card?.content).toBe([
            "目标清单",
            "1. 环境与依赖盘点，锁定技术栈",
            "3. 物理内核实现",
            "→ 4. 渲染与美术表现层",
            "进度：第 5/50 轮",
        ].join("\n"));
        expect(card?.content).not.toContain("/goal");
    });

    it("lists a later step without a status mark when the run returns to an earlier one", () => {
        const collapsed = collapseHostGoalChecklist([
            { id: "step4", role: "user", content: "/goal 执行步骤 4：渲染与美术表现层\n进度：第 5/50 轮", timestamp: 1 },
            { id: "step4-reply", role: "assistant", content: "渲染了一半", timestamp: 2 },
            { id: "step2", role: "user", content: "/goal 继续步骤 2/9：工程骨架\n进度：第 6/50 轮", timestamp: 3 },
            { id: "step2-reply", role: "assistant", content: "补做骨架", timestamp: 4 },
        ]);
        expect(collapsed.find(message => message.kind === "goalChecklist")?.content).toBe([
            "目标清单",
            "→ 2. 工程骨架",
            "4. 渲染与美术表现层",
            "进度：第 6/50 轮",
        ].join("\n"));
    });

    it("formats a goal checklist from the goal record", () => {
        expect(formatGoalChecklistFromRecord({
            phase: "execute",
            objective: "愤怒的小鸟",
            steps: [
                { index: 1, title: "环境", status: "done" },
                { index: 2, title: "工程骨架", status: "in_progress" },
                { index: 3, title: "物理内核", status: "pending" },
                { index: 4, title: "渲染", status: "failed" },
            ],
            turns_used: 3,
            max_turns: 50,
            tokens_used: 12,
            token_budget: 100,
        })).toBe([
            "目标清单",
            "✓ 1. 环境",
            "→ 2. 工程骨架",
            "○ 3. 物理内核",
            "× 4. 渲染",
            "进度：第 4/50 轮 | Token: 12/100",
        ].join("\n"));
        expect(formatGoalChecklistFromRecord({
            phase: "execute",
            steps: [
                { index: 1, title: "环境", status: "done" },
                { index: 2, title: "渲染", status: "complete" },
            ],
            turns_used: 4,
            max_turns: 50,
        })).toBe([
            "目标清单",
            "✓ 1. 环境",
            "✓ 2. 渲染",
            "正在核对验收",
            "进度：第 5/50 轮",
        ].join("\n"));
        // A completed goal is not still checking acceptance.
        expect(formatGoalChecklistFromRecord({
            phase: "execute",
            status: "complete",
            steps: [
                { index: 1, title: "环境", status: "done" },
                { index: 2, title: "渲染", status: "done" },
            ],
            turns_used: 4,
        })).toBe([
            "目标清单",
            "✓ 1. 环境",
            "✓ 2. 渲染",
            "已完成",
        ].join("\n"));
        expect(formatGoalChecklistFromRecord({
            phase: "execute",
            status: "failed",
            summary: "依赖安装失败\n换镜像再试",
            steps: [
                { index: 1, title: "环境", status: "done" },
                { index: 2, title: "渲染", status: "failed" },
            ],
        })).toBe([
            "目标清单",
            "✓ 1. 环境",
            "× 2. 渲染",
            "目标失败：依赖安装失败",
        ].join("\n"));
        expect(formatGoalChecklistFromRecord({
            phase: "plan",
            objective: "实现登录",
            turns_used: 0,
            max_turns: 50,
        })).toBe("目标清单\n正在分解：实现登录\n进度：第 1/50 轮");
        expect(formatGoalChecklistFromRecord({ phase: "execute", steps: null, objective: "实现登录" })).toBe("");
        const history = [{ id: "ask", role: "user" as const, content: "/goal  设计", timestamp: 1 }];
        const card = "目标清单\n→ 2. 工程骨架\n○ 3. 物理内核";
        const once = upsertGoalChecklistInHistory(history, "desktop-user:D:/tasks/bird", card);
        expect(once[1]?.id).toBe(goalChecklistMessageId("desktop-user:D:\\tasks\\bird"));
        expect(upsertGoalChecklistInHistory(once, "desktop-user:D:/tasks/bird", card)).toBe(once);
        const snapshot = [{ id: "ask", role: "user" as const, content: "/goal  设计", timestamp: 1 }];
        const live = [...snapshot, { id: "stream", role: "assistant" as const, content: "正在写物理", timestamp: 2 }];
        const kept = nextGoalChecklistHistory({
            history: snapshot,
            liveHistory: live,
            sessionKey: "desktop-user:D:/tasks/bird",
            content: card,
        });
        expect(kept?.map(message => message.id)).toEqual(["ask", "stream", goalChecklistMessageId("desktop-user:D:/tasks/bird")]);
        const trail = [{ id: "t1", sequence: 1, kind: "thinking" as const, content: "Inspect quality.h", timestamp: 2 }];
        const storedTrail = [
            { id: "ask", role: "user" as const, content: "/goal  设计", timestamp: 1 },
            { id: "goal", role: "assistant" as const, content: "", requestId: "goal-cont-1", timestamp: 2, codingTimeline: trail },
        ];
        const staleLive = [{ id: "ask", role: "user" as const, content: "/goal  设计", timestamp: 1 }];
        const keptTrail = nextGoalChecklistHistory({
            history: storedTrail,
            liveHistory: staleLive,
            sessionKey: "desktop-user:D:/tasks/bird",
            content: card,
        });
        expect(keptTrail?.find(message => message.id === "goal")?.codingTimeline).toEqual(trail);
        expect(keptTrail?.some(message => message.kind === "goalChecklist")).toBe(true);
        expect(nextGoalChecklistHistory({ history: [], sessionKey: "desktop-user", content: card })).toBeNull();
        expect(nextGoalChecklistHistory({ history: live, cleared: true, sessionKey: "desktop-user", content: card })).toBeNull();
        const bird = [{ id: "ask", role: "user" as const, content: "/goal  设计", timestamp: 1 }];
        const other = [{ id: "other", role: "user" as const, content: "另一件事", timestamp: 2 }];
        const initial = noteProjectTabLiveHistory({ tabId: "bird", messages: [] }, "bird", bird);
        expect(initial).toEqual({ tabId: "bird", messages: bird });
        const switched = noteProjectTabLiveHistory(initial, "other", bird);
        expect(switched).toBe(initial);
        expect(projectTabLiveMessages(switched, "other")).toBeUndefined();
        expect(projectTabLiveMessages(switched, "bird")).toBe(bird);
        const repeated = noteProjectTabLiveHistory(switched, "other", bird);
        expect(repeated).toBe(switched);
        const settled = noteProjectTabLiveHistory(repeated, "other", other);
        expect(settled).toEqual({ tabId: "other", messages: other });
        expect(projectTabLiveMessages(settled, "other")).toBe(other);
    });

    it("keeps the published checklist and moves it above the current reply", () => {
        const published = "目标清单\n✓ 1. 环境\n→ 4. 渲染与美术表现层\n进度：第 5/50 轮";
        const collapsed = collapseHostGoalChecklist([
            { id: "list", role: "system", kind: "goalChecklist", content: published, timestamp: 1 },
            { id: "early", role: "assistant", content: "环境已盘点", timestamp: 2 },
            { id: "old", role: "user", content: "/goal 执行步骤 4：渲染与美术表现层\n进度：第 5/50 轮", timestamp: 3 },
            { id: "current", role: "assistant", content: "正在画渲染层", timestamp: 4 },
            { id: "placeholder", role: "system", kind: "goalChecklist", content: "目标清单\n正在继续当前步骤", timestamp: 5 },
        ]);
        expect(collapsed.map(message => message.id)).toEqual(["early", "list", "current"]);
        expect(collapsed.find(message => message.id === "list")?.content).toBe(published);
    });

    it("keeps a stored step line when recovery only has the generic placeholder", () => {
        const collapsed = collapseHostGoalChecklist([
            { id: "step", role: "user", content: "/goal 执行步骤 4：渲染与美术表现层\n进度：第 5/50 轮", timestamp: 1 },
            { id: "reply", role: "assistant", content: "正在画渲染层", timestamp: 2 },
            { id: "placeholder", role: "system", kind: "goalChecklist", content: "目标清单\n正在继续当前步骤", timestamp: 3 },
        ]);
        expect(collapsed.map(message => message.id)).toEqual(["step", "reply"]);
        expect(collapsed[0].content).toBe("目标清单\n→ 4. 渲染与美术表现层\n进度：第 5/50 轮");
    });

    it("keeps the checklist on the goal reply when the user speaks afterwards", () => {
        const published = "目标清单\n→ 4. 渲染与美术表现层";
        const collapsed = collapseHostGoalChecklist([
            { id: "ask", role: "user", content: "/goal  设计并实现一个愤怒的小鸟游戏", timestamp: 1 },
            { id: "created", role: "assistant", content: "目标已创建", requestId: "desktop-ai-1", timestamp: 2 },
            { id: "list", role: "system", kind: "goalChecklist", content: published, timestamp: 3 },
            { id: "goal-reply", role: "assistant", content: "正在画渲染层", requestId: "goal-cont-step4", timestamp: 4 },
            { id: "note", role: "user", content: "先暂停一下", timestamp: 5 },
            { id: "note-reply", role: "assistant", content: "已暂停", requestId: "desktop-ai-2", timestamp: 6 },
        ]);
        expect(collapsed.map(message => message.id)).toEqual(["ask", "created", "list", "goal-reply", "note", "note-reply"]);
        expect(collapsed.find(message => message.id === "list")?.content).toBe(published);
    });

    it("follows a newer goal continuation that starts after the user note", () => {
        const collapsed = collapseHostGoalChecklist([
            { id: "list", role: "system", kind: "goalChecklist", content: "目标清单\n→ 4. 渲染与美术表现层", timestamp: 1 },
            { id: "goal-reply", role: "assistant", content: "渲染层写了一半", requestId: "goal-cont-step4", timestamp: 2 },
            { id: "note", role: "user", content: "继续做渲染", timestamp: 3 },
            { id: "next", role: "assistant", content: "继续渲染", requestId: "goal-cont-step4b", timestamp: 4 },
        ]);
        expect(collapsed.map(message => message.id)).toEqual(["goal-reply", "note", "list", "next"]);
    });

    it("does not replay a host goal checklist as a later user turn", () => {
        expect(buildProjectTabRecentMessages([
            { id: "ask", role: "user", content: "设计并实现一个愤怒的小鸟游戏", timestamp: 1 },
            { id: "step", role: "user", content: "/goal 执行步骤 3：物理内核实现\n进度：第 4/50 轮", timestamp: 2 },
            { id: "list", role: "system", kind: "goalChecklist", content: "目标清单\n→ 3. 物理内核实现", timestamp: 3 },
            { id: "assistant", role: "assistant", content: "物理内核已完成", timestamp: 4 },
        ])).toEqual([
            { role: "user", content: "设计并实现一个愤怒的小鸟游戏" },
            { role: "assistant", content: "物理内核已完成" },
        ]);
    });

    it("does not replay an injected guide as a later project-tab user turn", () => {
        expect(buildProjectTabRecentMessages([
            { id: "guide", role: "user", kind: "guideInjection", content: "redirect this live task", timestamp: 1 },
            { id: "assistant", role: "assistant", content: "continuing with the revised scope", timestamp: 2 },
        ])).toEqual([
            { role: "assistant", content: "continuing with the revised scope" },
        ]);
    });
    it("normalizes project session paths for stable task identity", () => {
        expect(normalizeProjectSessionPath(" d:\\workprj\\task\\. ")).toBe("D:/workprj/task");
        expect(projectSessionKey("d:/workprj/task/.")).toBe("desktop-user:D:/workprj/task");
        expect(projectPathFromSessionKey("desktop-user:d:\\workprj\\task\\")).toBe("D:/workprj/task");
    });

    it("matches legacy slash variants to the normalized session", () => {
        const message = { id: "m1", role: "assistant", content: "ok", sessionKey: "desktop-user:d:\\workprj\\task\\" } as ChatMessage;
        expect(messageBelongsToSession(message, "desktop-user:D:/workprj/task")).toBe(true);
    });

    it("keeps ACP owners opaque and outside path-based routing", () => {
        const owner = "desktop-user:acp:acp_gui_session_42";
        expect(isACPAssistantSessionKey(owner)).toBe(true);
        expect(normalizeAssistantSessionKey(owner)).toBe(owner);
        expect(projectPathFromSessionKey(owner)).toBe("");
    });

    it("does not block the ACP mirror tab when sidebar registration fails", () => {
        expect(ACP_ASSISTANT_TAB_IDENTITY).toBe("acp");
        expect(shouldBlockAssistantTabOpenOnTaskRegistration("acp")).toBe(false);
        expect(shouldBlockAssistantTabOpenOnTaskRegistration("ve")).toBe(false);
        expect(shouldBlockAssistantTabOpenOnTaskRegistration("discussion")).toBe(true);
        expect(shouldBlockAssistantTabOpenOnTaskRegistration("project")).toBe(true);
    });

    it("detects digital-employee chat rows that must stay off the task list", () => {
        expect(isVEAssistantTabTaskItem({
            tags: ["task_management", "source:assistant_tab:ve:abc123"],
        })).toBe(true);
        expect(isVEAssistantTabTaskItem({
            tags: ["task_management", "source:assistant_tab:discussion:abc123"],
        })).toBe(false);
        expect(isVEAssistantTabTaskItem({ tags: ["task_management"] })).toBe(false);
    });

    it("detects VS Code / ACP sidebar snapshot rows", () => {
        expect(isAutoACPAssistantTabTaskItem({
            project_path: "C:/Users/me/.maclaw/data/tasks/vs-code-acp-1787000000001",
            tags: ["task_management"],
        })).toBe(true);
        expect(isAutoACPAssistantTabTaskItem({
            project_path: "C:/Users/me/.maclaw/data/tasks/vendor-review-1",
            tags: ["source:assistant_tab:acp:deadbeef"],
        })).toBe(true);
        expect(isAutoACPAssistantTabTaskItem({
            project_path: "C:/Users/me/.maclaw/data/tasks/vendor-review-1787000000001",
            tags: ["task_management"],
        })).toBe(false);
        expect(isAutoACPAssistantTabTaskItem({
            project_path: "C:/Users/me/.maclaw/data/tasks/vs-code-acp-1787000000001",
            name: "贪吃蛇",
            tags: ["task_management", "coding_dev"],
        })).toBe(false);
    });

    it("reads expert ids from durable task-management source tags", () => {
        expect(expertIDFromTaskTags(["task_management", "source:expert:paper-review"])).toBe("paper-review");
        expect(expertIDFromTaskTags(["source:expert:", "task_management"])).toBe("");
        expect(expertIDFromTaskTags(["task_management"])).toBe("");
    });

    it("maps the visible assistant tab to a task-list identity, and clears it for the local tab", () => {
        expect(activeAssistantTaskIdentity({ type: "local" })).toBeNull();
        expect(activeAssistantTaskIdentity({ type: "local", projectPath: "D:/work/math" })).toBeNull();
        expect(activeAssistantTaskIdentity({ type: "ve" })).toBeNull();
        expect(activeAssistantTaskIdentity({ type: "group" })).toBeNull();
        expect(activeAssistantTaskIdentity({ type: "project", projectPath: "d:\\work\\math" })).toEqual({
            projectPath: "D:/work/math",
        });
        expect(activeAssistantTaskIdentity({
            type: "project",
            projectPath: "C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math",
        })).toEqual({
            projectPath: "C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math",
            cloudWorkspaceId: "cws_math",
        });
        expect(activeAssistantTaskIdentity(
            { type: "project", projectPath: "D:/work/tasks/math-book" },
            "C:/Users/me/.maclaw/data/cloud-workspaces/tenant/cws_math",
        )).toEqual({
            projectPath: "D:/work/tasks/math-book",
            cloudWorkspaceId: "cws_math",
        });
        expect(activeAssistantTaskIdentity({ type: "expert", expertId: "paper-review" })).toEqual({
            expertId: "paper-review",
        });
        expect(activeAssistantTaskIdentity({
            type: "expert",
            expertId: "builtin-latex-paper",
            projectPath: "d:/tasks/latex-paper",
        })).toEqual({
            expertId: "builtin-latex-paper",
            projectPath: "D:/tasks/latex-paper",
        });
        expect(activeAssistantTaskIdentity({ type: "expert" })).toBeNull();
        expect(coerceActiveAssistantTask({})).toBeNull();
        expect(coerceActiveAssistantTask({ projectPath: "  " })).toBeNull();
        expect(sameActiveAssistantTask(null, null)).toBe(true);
        expect(sameActiveAssistantTask(null, {})).toBe(true);
        expect(sameActiveAssistantTask({ projectPath: "D:/a" }, { projectPath: "d:\\a" })).toBe(true);
        expect(sameActiveAssistantTask({ projectPath: "D:/a" }, null)).toBe(false);
    });

    it("purges expert tab metadata and history that project-path purge cannot see", () => {
        localStorage.setItem("ai_assistant_project_tabs", JSON.stringify([
            { id: "expert-paper-review", type: "expert", title: "Paper", expertId: "paper-review" },
            { id: "proj-keep", type: "project", projectPath: "D:/p/keep" },
        ]));
        localStorage.setItem("ai_assistant_project_tab_histories", JSON.stringify({
            "expert-paper-review": [{ id: "h1", role: "user", content: "old" }],
            "proj-keep": [{ id: "h2", role: "user", content: "keep" }],
        }));

        // Path-based purge must leave expert rows alone.
        purgeDeletedProjectTabLocalCache("D:/tasks/expert-workspace");
        expect(localStorage.getItem("ai_assistant_project_tabs") || "").toContain("expert-paper-review");
        expect(localStorage.getItem("ai_assistant_project_tab_histories") || "").toContain("old");

        purgeDeletedExpertTabLocalCache("paper-review");
        expect(localStorage.getItem("ai_assistant_project_tabs") || "").not.toContain("expert-paper-review");
        expect(localStorage.getItem("ai_assistant_project_tabs") || "").toContain("D:/p/keep");
        expect(localStorage.getItem("ai_assistant_project_tab_histories") || "").not.toContain("expert-paper-review");
        expect(localStorage.getItem("ai_assistant_project_tab_histories") || "").toContain("keep");
    });

    it("purges cloud-bound tab cache when deletion arrives with a sibling list-row path", () => {
        // The live tab persists under the local cache path; the deletion
        // arrives with the durable list path. Both embed workspace cws_x.
        localStorage.setItem("ai_assistant_project_tabs", JSON.stringify([
            { id: "cws-tab", type: "project", projectPath: "C:/cache/cloud-workspaces/tenant/cws_x", cloudWorkspaceId: "cws_x" },
            { id: "proj-keep", type: "project", projectPath: "D:/p/keep" },
        ]));
        localStorage.setItem("ai_assistant_project_tab_histories", JSON.stringify({
            "cws-tab": [{ id: "h1", role: "user", content: "cloud history" }],
            "proj-keep": [{ id: "h2", role: "user", content: "keep" }],
        }));

        purgeDeletedProjectTabLocalCache("D:/tasks/list-rows/cloud-workspaces/tenant/cws_x");
        expect(localStorage.getItem("ai_assistant_project_tabs") || "").not.toContain("cws-tab");
        expect(localStorage.getItem("ai_assistant_project_tab_histories") || "").not.toContain("cloud history");
        expect(localStorage.getItem("ai_assistant_project_tab_histories") || "").toContain("keep");
    });

    it("archives an expert transcript onto the previous paper and ignores turns before the fresh-session floor", () => {
        const archived = archiveExpertTranscriptForProject("D:/tasks/old-paper", [
            { id: "u1", role: "user", content: "old request", sessionKey: "desktop-user:expert:builtin-latex-paper", timestamp: 10 },
            { id: "a1", role: "assistant", content: "old reply", sessionKey: "desktop-user:expert:builtin-latex-paper", timestamp: 11 },
        ]);
        expect(archived?.projectPath).toBe("D:/tasks/old-paper");
        expect(archived?.history[0]?.sessionKey).toBe(projectSessionKey("D:/tasks/old-paper"));
        expect(archiveExpertTranscriptForProject("D:/tasks/old-paper", [
            { id: "w", role: "assistant", content: "welcome", timestamp: 1 },
        ])).toBeNull();
        expect(messageAfterSessionFloor({ timestamp: 10 }, 20)).toBe(false);
        expect(messageAfterSessionFloor({ timestamp: 20 }, 20)).toBe(true);
        expect(messageAfterSessionFloor({ timestamp: 10 }, 0)).toBe(true);
    });
});

describe("fileTerminalResponseWithoutRound", () => {
    const taskKey = "desktop-user:C:/Users/me/.maclaw/data/tasks/indra";

    it("files a renderable payload onto the task that owns it", () => {
        expect(fileTerminalResponseWithoutRound({ hasRound: false, sessionKey: taskKey, renderable: true })).toBe(true);
    });

    it("leaves a settled round and a session-less payload alone", () => {
        expect(fileTerminalResponseWithoutRound({ hasRound: true, sessionKey: taskKey, renderable: true })).toBe(false);
        expect(fileTerminalResponseWithoutRound({ hasRound: false, sessionKey: taskKey, renderable: false })).toBe(false);
        expect(fileTerminalResponseWithoutRound({ hasRound: false, sessionKey: "desktop-user", renderable: true })).toBe(false);
        expect(fileTerminalResponseWithoutRound({ hasRound: false, sessionKey: "", renderable: true })).toBe(false);
    });
});

describe("visibleProjectTranscript", () => {
    const taskKey = "desktop-user:C:/Users/me/.maclaw/data/tasks/textproc";
    const essay = { id: "s0", role: "assistant" as const, content: "下一轮只执行 S0。", sessionKey: "desktop-user:C:\\Users\\me\\.maclaw\\data\\tasks\\textproc", timestamp: 1 };
    const report = {
        id: "s2",
        role: "assistant" as const,
        content: "第 3 步（S2）已完成并归档。",
        sessionKey: taskKey,
        timestamp: 2,
        fields: [{ label: "session_tokens", value: "in=1" }, { label: "Turn", value: "reasoning" }],
    };

    it("keeps the stage report whose session key is already on the essay", () => {
        const shown = visibleProjectTranscript({
            history: [essay],
            live: [report],
            activeSessionKey: "desktop-user:E:/workprj/mousika/textproc/qmsrcdet26",
        });
        expect(shown.map(message => message.id)).toEqual(["s0", "s2"]);
        expect(shown[0].content).toContain("S0");
        expect(shown[1].content).toContain("S2");
        expect(shown[1].fields?.map(field => field.label)).toEqual(["session_tokens", "Turn"]);
    });

    it("shows live rows on an uncleared empty tab and keeps a cleared tab empty", () => {
        expect(visibleProjectTranscript({
            history: [],
            live: [report],
            activeSessionKey: taskKey,
        }).map(message => message.id)).toEqual(["s2"]);
        expect(visibleProjectTranscript({
            history: [],
            live: [report],
            activeSessionKey: taskKey,
            cleared: true,
        })).toEqual([]);
    });

    it("keeps the saved essay when this paint has no live rows for the tab", () => {
        const other = { id: "other", role: "assistant" as const, content: "别的任务", sessionKey: "desktop-user:D:/tasks/other", timestamp: 3 };
        const unowned = { id: "bare", role: "assistant" as const, content: "无主", timestamp: 4 };
        const shown = visibleProjectTranscript({
            history: [essay],
            live: [other, unowned],
            activeSessionKey: "desktop-user:E:/workprj/mousika/textproc/qmsrcdet26",
        });
        expect(shown.map(message => message.id)).toEqual(["s0"]);
        expect(visibleProjectTranscript({ history: [essay], live: [], activeSessionKey: taskKey })[0].content).toContain("S0");
    });

    it("joins the next stage by goal id when the session key changed", () => {
        const workspaceKey = "desktop-user:E:/workprj/mousika/textproc/qmsrcdet26";
        const stamped = {
            id: "s0",
            role: "assistant" as const,
            content: "下一轮只执行 S0。",
            sessionKey: workspaceKey,
            requestId: "goal-cont-f7575c4e-1792978972557-2",
            timestamp: 1,
        };
        const report = {
            id: "s2",
            role: "assistant" as const,
            content: "第 3 步（S2）已完成并归档。",
            sessionKey: taskKey,
            requestId: "goal-cont-f7575c4e-1792980790226-4",
            timestamp: 2,
            fields: [{ label: "Turn", value: "reasoning" }],
        };
        const otherGoal = {
            id: "other-goal",
            role: "assistant" as const,
            content: "别的目标",
            sessionKey: "desktop-user:C:/Users/me/.maclaw/data/tasks/other",
            requestId: "goal-cont-aaaaaaaa-1792980790999-1",
            timestamp: 3,
        };
        const shown = visibleProjectTranscript({
            history: [stamped],
            live: [report, otherGoal],
            activeSessionKey: workspaceKey,
        });
        expect(shown.map(message => message.id)).toEqual(["s0", "s2"]);
        expect(shown[1].fields?.[0]?.label).toBe("Turn");
        const shortEssay = { ...stamped, id: "short", requestId: "goal-cont-test-1" };
        const otherShort = { ...otherGoal, requestId: "goal-cont-test-2" };
        expect(visibleProjectTranscript({
            history: [shortEssay],
            live: [otherShort],
            activeSessionKey: workspaceKey,
        }).map(message => message.id)).toEqual(["short"]);
    });

    it("does not import the local desktop transcript from a bare owner", () => {
        const bare = { id: "s0", role: "assistant" as const, content: "下一轮只执行 S0。", sessionKey: "desktop-user", timestamp: 1 };
        const local = { id: "local", role: "assistant" as const, content: "本地闲聊", sessionKey: "desktop-user", timestamp: 2 };
        const shown = visibleProjectTranscript({
            history: [bare],
            live: [local],
            activeSessionKey: "desktop-user:E:/workprj/mousika/textproc/qmsrcdet26",
        });
        expect(shown.map(message => message.id)).toEqual(["s0"]);
    });
});
