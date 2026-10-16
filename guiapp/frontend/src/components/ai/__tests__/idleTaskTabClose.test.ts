import { describe, expect, it, vi } from "vitest";
import type { AITab, AITabState } from "../AITabTypes";
import { planIdleTaskTabCloses, releaseIdleTaskTabs, type IdleTaskSwitchContext } from "../idleTaskTabClose";

function projectTab(id: string, projectPath: string, extra: Partial<AITab> = {}): AITab {
    return { id, type: "project", title: id, projectPath, closable: true, ...extra };
}

function expertTab(id: string, expertId: string): AITab {
    return { id, type: "expert", title: id, expertId, closable: true };
}

function ctx(overrides: Partial<IdleTaskSwitchContext> & Pick<IdleTaskSwitchContext, "tabs" | "activeTabId">): IdleTaskSwitchContext & { closed: string[] } {
    const closed: string[] = [];
    return {
        tasks: [],
        maxTabs: 8,
        draftText: "",
        unsentAttachmentCount: 0,
        preparingTabIds: new Set(),
        recordingTabId: null,
        busySessionKeys: [],
        streamingSessionKeys: [],
        sendingSessionKey: "",
        inFlightSessionKeys: new Set(),
        activeExecutionBusy: false,
        activeAwaitingUser: false,
        scopeApprovalProjectPath: "",
        getTabState: () => undefined,
        closeTab: (id: string) => { closed.push(id); },
        closed,
        ...overrides,
    };
}

describe("planIdleTaskTabCloses", () => {
    it("closes the idle task being left when switching to another task", () => {
        const tabs = [projectTab("a", "D:/tasks/a"), projectTab("b", "D:/tasks/b")];
        const state = ctx({ tabs, activeTabId: "a" });
        expect(planIdleTaskTabCloses(state, { nextTabId: "b" })).toEqual(["a"]);
        releaseIdleTaskTabs(state, { nextTabId: "b" });
        expect(state.closed).toEqual(["a"]);
    });

    it("keeps a running, paused, or review task", () => {
        const tabs = [projectTab("a", "D:/tasks/a"), projectTab("b", "D:/tasks/b")];
        const running = ctx({
            tabs,
            activeTabId: "a",
            tasks: [{ project_path: "D:/tasks/a", active_workflow: { status: "running", phase: "execute" } }],
        });
        expect(planIdleTaskTabCloses(running, { nextTabId: "b" })).toEqual([]);
        const paused = ctx({
            tabs,
            activeTabId: "a",
            tasks: [{ project_path: "D:/tasks/a", active_workflow: { status: "paused" } }],
        });
        expect(planIdleTaskTabCloses(paused, { nextTabId: "b" })).toEqual([]);
        const review = ctx({
            tabs,
            activeTabId: "a",
            tasks: [{ project_path: "D:/tasks/a", active_workflow: { status: "review", pending_review: true } }],
        });
        expect(planIdleTaskTabCloses(review, { nextTabId: "b" })).toEqual([]);
    });

    it("keeps an in-progress workflow that the running-word bucket would miss", () => {
        const tabs = [projectTab("a", "D:/tasks/a"), projectTab("b", "D:/tasks/b")];
        const state = ctx({
            tabs,
            activeTabId: "a",
            tasks: [{ project_path: "D:/tasks/a", active_workflow: { status: "in_progress" } }],
        });
        expect(planIdleTaskTabCloses(state, { nextTabId: "b" })).toEqual([]);
    });

    it("closes a finished task and leaves other idle tabs alone", () => {
        const tabs = [projectTab("a", "D:/tasks/a"), projectTab("c", "D:/tasks/c"), projectTab("b", "D:/tasks/b")];
        const state = ctx({
            tabs,
            activeTabId: "a",
            tasks: [{ project_path: "D:/tasks/a", has_output: true }],
        });
        expect(planIdleTaskTabCloses(state, { nextTabId: "b" })).toEqual(["a"]);
    });

    it("keeps the tab when the composer has a draft, attachments, or the session is busy", () => {
        const tabs = [projectTab("a", "D:/tasks/a"), projectTab("b", "D:/tasks/b")];
        expect(planIdleTaskTabCloses(ctx({ tabs, activeTabId: "a", draftText: "not sent" }), { nextTabId: "b" })).toEqual([]);
        expect(planIdleTaskTabCloses(ctx({ tabs, activeTabId: "a", unsentAttachmentCount: 1 }), { nextTabId: "b" })).toEqual([]);
        expect(planIdleTaskTabCloses(ctx({ tabs, activeTabId: "a", activeExecutionBusy: true }), { nextTabId: "b" })).toEqual([]);
        expect(planIdleTaskTabCloses(ctx({ tabs, activeTabId: "a", busySessionKeys: ["desktop-user:D:/tasks/a"] }), { nextTabId: "b" })).toEqual([]);
        expect(planIdleTaskTabCloses(ctx({ tabs, activeTabId: "a", preparingTabIds: new Set(["a"]) }), { nextTabId: "b" })).toEqual([]);
        expect(planIdleTaskTabCloses(ctx({ tabs, activeTabId: "a", activeAwaitingUser: true }), { nextTabId: "b" })).toEqual([]);
    });

    it("does not close a cloud task that is only being rebound onto a new cache path", () => {
        const tabs = [projectTab("a", "D:/cache/cloud-workspaces/tenant/cws_abc", { cloudWorkspaceId: "cws_abc" })];
        const state = ctx({ tabs, activeTabId: "a" });
        expect(planIdleTaskTabCloses(state, { projectPath: "D:/cache/cloud-workspaces/tenant/cws_abc/workspace" })).toEqual([]);
    });

    it("does not close the task being switched to, or an ACP mirror", () => {
        const tabs = [projectTab("a", "D:/tasks/a"), projectTab("acp", "D:/tasks/acp", { sessionKey: "desktop-user:acp:1" })];
        expect(planIdleTaskTabCloses(ctx({ tabs, activeTabId: "a" }), { nextTabId: "a" })).toEqual([]);
        expect(planIdleTaskTabCloses(ctx({ tabs, activeTabId: "acp" }), { projectPath: "D:/tasks/new" })).toEqual([]);
    });

    it("closes an idle expert when leaving it, and frees the oldest idle project tab at the cap", () => {
        const expert = expertTab("expert-a", "code");
        const older = projectTab("old", "D:/tasks/old");
        const current = projectTab("a", "D:/tasks/a");
        const state = ctx({
            tabs: [expert, older, current],
            activeTabId: "a",
            maxTabs: 2,
            tasks: [{ project_path: "D:/tasks/a", active_workflow: { status: "running" } }],
            getTabState: (id) => (id === "old" ? { history: [], scrollTop: 0, inputText: "", lastActiveAt: 5 } : { history: [], scrollTop: 0, inputText: "", lastActiveAt: 50 }) as AITabState,
        });
        expect(planIdleTaskTabCloses(state, { projectPath: "D:/tasks/new" })).toEqual(["old"]);
        const leavingExpert = ctx({
            tabs: [expert, projectTab("b", "D:/tasks/b")],
            activeTabId: "expert-a",
        });
        expect(planIdleTaskTabCloses(leavingExpert, { nextTabId: "b" })).toEqual(["expert-a"]);
    });

    it("does not evict another idle tab when closing the one being left already frees a slot", () => {
        const tabs = [projectTab("a", "D:/tasks/a"), projectTab("c", "D:/tasks/c")];
        const state = ctx({ tabs, activeTabId: "a", maxTabs: 2 });
        expect(planIdleTaskTabCloses(state, { projectPath: "D:/tasks/new" })).toEqual(["a"]);
    });

    it("logs and closes through releaseIdleTaskTabs", () => {
        const info = vi.spyOn(console, "info").mockImplementation(() => {});
        const state = ctx({ tabs: [projectTab("a", "D:/tasks/a")], activeTabId: "a" });
        releaseIdleTaskTabs(state, { projectPath: "D:/tasks/b" });
        expect(state.closed).toEqual(["a"]);
        info.mockRestore();
    });
});
