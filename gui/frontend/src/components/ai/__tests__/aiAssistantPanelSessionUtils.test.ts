import { describe, expect, it } from "vitest";
import { activeAssistantTaskIdentity, buildProjectTabRecentMessages, chatHistoriesEquivalent, coerceActiveAssistantTask, expertIDFromTaskTags, isACPAssistantSessionKey, messageBelongsToSession, normalizeAssistantSessionKey, normalizeProjectSessionPath, projectPathFromSessionKey, projectSessionKey, purgeDeletedExpertTabLocalCache, purgeDeletedProjectTabLocalCache, sameActiveAssistantTask } from "../aiAssistantPanelSessionUtils";
import type { ChatMessage } from "../useAIAssistant";

describe("aiAssistantPanelSessionUtils", () => {
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
});
