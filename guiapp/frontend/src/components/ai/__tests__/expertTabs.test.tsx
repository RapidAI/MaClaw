// @vitest-environment jsdom
/**
 * Tests for expert conversation tabs:
 * - useAITabManager.createExpertTab (structure / dedupe / limit / persistence)
 * - usePendingAssistantTabOpen welcome seed (first-open only)
 * - session key helpers for the expert session namespace
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { useState } from "react";
import { useAITabManager } from "../useAITabManager";
import { usePendingAssistantTabOpen, type PendingExpertOpen } from "../usePendingAssistantTabOpen";
import type { ExpertDefinition } from "../expertTypes";
import { expertTabId, expertWelcomeMessageText, DEFAULT_EXPERT_ICON } from "../expertTypes";
import {
    expertSessionKey,
    isExpertSessionKey,
    expertIdFromSessionKey,
    messageIsLocalSession,
    projectPathFromSessionKey,
} from "../aiAssistantPanelSessionUtils";
import { AbandonUnopenedFreshLatexTask, ClearAIAssistantHistoryForSession, CloseAssistantTabSession, CreateProjectTabSession, LoadProjectTabConversation, LoadProjectTabIndex, SaveProjectTabConversation } from "../../../../wailsjs/go/main/App";
import { resetLatexDocumentRequestForTests, takePendingLatexDocument } from "../latexDocumentOpen";
import { LATEX_EXPERT_ID } from "../../../utils/latexTemplates";

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    LoadProjectTabIndex: vi.fn().mockResolvedValue([]),
    CloseAssistantTabSession: vi.fn().mockResolvedValue(undefined),
    CreateProjectTabSession: vi.fn().mockResolvedValue(undefined),
    SaveProjectTabConversation: vi.fn().mockResolvedValue(undefined),
    LoadProjectTabConversation: vi.fn().mockResolvedValue([]),
    ClearAIAssistantHistoryForSession: vi.fn().mockResolvedValue(undefined),
    AbandonUnopenedFreshLatexTask: vi.fn().mockResolvedValue(undefined),
}));

const expertA: ExpertDefinition = {
    id: "exp-paper-polish",
    name: "论文润色",
    description: "学术语言润色",
    icon: "📝",
    system_prompt: "你是论文润色专家",
    tools: [],
    skills: [],
    builtin: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
};

const expertB: ExpertDefinition = { ...expertA, id: "exp-ppt", name: "PPT 制作", icon: "", description: "" };

const expertC: ExpertDefinition = { ...expertA, id: "exp-translate", name: "论文翻译", icon: "🌐" };

describe("expert session key helpers", () => {
    it("builds and reverses expert session keys", () => {
        expect(expertSessionKey("exp-1")).toBe("desktop-user:expert:exp-1");
        expect(expertSessionKey("")).toBe("");
        expect(isExpertSessionKey("desktop-user:expert:exp-1")).toBe(true);
        expect(isExpertSessionKey("desktop-user")).toBe(false);
        expect(expertIdFromSessionKey("desktop-user:expert:exp-1")).toBe("exp-1");
        expect(expertIdFromSessionKey("desktop-user:/tmp/proj")).toBe("");
    });

    it("expert session keys never leak into project path routing", () => {
        expect(projectPathFromSessionKey("desktop-user:expert:exp-1")).toBe("");
        expect(projectPathFromSessionKey("desktop-user:D:/work/x")).toBe("D:/work/x");
    });

    it("expert messages are not treated as local session messages", () => {
        expect(messageIsLocalSession({ sessionKey: "desktop-user:expert:exp-1" } as any)).toBe(false);
        expect(messageIsLocalSession({ sessionKey: "desktop-user" } as any)).toBe(true);
        expect(messageIsLocalSession({} as any)).toBe(true);
    });

    it("welcome text follows the panel i18n languages", () => {
        expect(expertWelcomeMessageText(expertA, "zh-Hans")).toBe("你好，我是论文润色。学术语言润色 有什么可以帮你？");
        expect(expertWelcomeMessageText(expertA, "zh-Hant")).toBe("你好，我是论文润色。学术语言润色 有什麼可以幫你？");
        expect(expertWelcomeMessageText(expertA, "en")).toBe("Hi, I'm 论文润色. 学术语言润色 How can I help you?");
        // No description → single clean sentence.
        expect(expertWelcomeMessageText(expertB, "zh-Hans")).toBe("你好，我是PPT 制作。有什么可以帮你？");
    });
});

describe("useAITabManager.createExpertTab", () => {
    beforeEach(() => {
        localStorage.clear();
        vi.clearAllMocks();
        vi.mocked(LoadProjectTabIndex).mockResolvedValue([]);
        vi.mocked(CloseAssistantTabSession).mockResolvedValue(undefined as any);
        vi.mocked(CreateProjectTabSession).mockResolvedValue("" as any);
        vi.mocked(LoadProjectTabConversation).mockResolvedValue([]);
        vi.mocked(SaveProjectTabConversation).mockResolvedValue(undefined as any);
    });

    it("creates an expert tab with expert-<id> id, expert fields and closable=true", () => {
        const { result } = renderHook(() => useAITabManager());
        let tab: ReturnType<typeof result.current.createExpertTab> = null;
        act(() => {
            tab = result.current.createExpertTab(expertA);
        });
        expect(tab).not.toBeNull();
        expect(tab!.id).toBe("expert-exp-paper-polish");
        expect(tab!.type).toBe("expert");
        expect(tab!.title).toBe("论文润色");
        expect(tab!.expertId).toBe("exp-paper-polish");
        expect(tab!.expertIcon).toBe("📝");
        expect(tab!.closable).toBe(true);
        expect(result.current.tabState.activeTabId).toBe(tab!.id);
    });

    it("releases an expert tab's private runtime directory when closed", async () => {
        const { result } = renderHook(() => useAITabManager());
        act(() => {
            result.current.createExpertTab(expertA);
        });
        act(() => {
            result.current.closeTab(expertTabId(expertA.id));
        });
        await Promise.resolve();
        expect(CloseAssistantTabSession).toHaveBeenCalledWith(expertTabId(expertA.id));
    });

    it("dedupes by expertId: second call activates the same tab instead of creating a new one", () => {
        const { result } = renderHook(() => useAITabManager());
        let first: ReturnType<typeof result.current.createExpertTab> = null;
        let second: ReturnType<typeof result.current.createExpertTab> = null;
        act(() => {
            first = result.current.createExpertTab(expertA);
        });
        act(() => {
            result.current.activateTab("local");
        });
        act(() => {
            second = result.current.createExpertTab({ ...expertA, name: "改名后" });
        });
        expect(second).not.toBeNull();
        expect(second!.id).toBe(first!.id);
        const expertTabs = result.current.tabState.tabs.filter(t => t.type === "expert");
        expect(expertTabs.length).toBe(1);
        expect(result.current.tabState.activeTabId).toBe(first!.id);
        // Metadata refresh keeps the newest name.
        expect(result.current.tabState.tabs.find(t => t.id === first!.id)?.title).toBe("改名后");
    });

    it("enforces the maxVETabs cap for expert tabs", () => {
        const { result } = renderHook(() => useAITabManager({ maxVETabs: 2 }));
        act(() => {
            expect(result.current.createExpertTab(expertA)).not.toBeNull();
        });
        act(() => {
            expect(result.current.createExpertTab(expertB)).not.toBeNull();
        });
        let third: ReturnType<typeof result.current.createExpertTab> = null;
        act(() => {
            third = result.current.createExpertTab(expertC);
        });
        expect(third).toBeNull();
        expect(result.current.tabLimitError).toContain("Expert tab limit reached");
        expect(result.current.tabState.tabs.filter(t => t.type === "expert").length).toBe(2);
    });

    it("rejects an expert without id", () => {
        const { result } = renderHook(() => useAITabManager());
        let tab: ReturnType<typeof result.current.createExpertTab> = null;
        act(() => {
            tab = result.current.createExpertTab({ ...expertA, id: " " });
        });
        expect(tab).toBeNull();
    });

    it("persists expert tabs + history to localStorage and restores them on remount", async () => {
        const { result, unmount } = renderHook(() => useAITabManager());
        act(() => {
            result.current.createExpertTab(expertA, { projectPath: "D:/tasks/paper", relativePath: "thesis.tex" });
        });
        act(() => {
            result.current.saveTabState(expertTabId(expertA.id), {
                history: [{ id: "m1", role: "assistant", content: "hello", timestamp: 1 }],
            });
        });
        // History persistence is debounced (500ms) — let it flush before unmount.
        await act(async () => {
            await new Promise(resolve => setTimeout(resolve, 650));
        });
        unmount();

        const raw = localStorage.getItem("ai_assistant_project_tabs");
        expect(raw).toBeTruthy();
        expect(raw).toContain("exp-paper-polish");

        const restored = renderHook(() => useAITabManager());
        const restoredTab = restored.result.current.tabState.tabs.find(t => t.type === "expert" && t.expertId === expertA.id);
        expect(restoredTab).toBeTruthy();
        expect(restoredTab!.title).toBe("论文润色");
        expect(restoredTab!.expertIcon).toBe("📝");
        expect(restoredTab!.projectPath).toBe("D:/tasks/paper");
        expect(restoredTab!.latexRelativePath).toBe("thesis.tex");
        const restoredState = restored.result.current.getTabState(expertTabId(expertA.id));
        expect(Array.isArray(restoredState?.history)).toBe(true);
        expect((restoredState?.history as any[])[0]?.content).toBe("hello");
        restored.unmount();
    });
});

describe("usePendingAssistantTabOpen expert welcome seed", () => {
    beforeEach(() => {
        localStorage.clear();
        vi.clearAllMocks();
        vi.mocked(LoadProjectTabIndex).mockResolvedValue([]);
        vi.mocked(CreateProjectTabSession).mockResolvedValue("" as any);
        vi.mocked(LoadProjectTabConversation).mockResolvedValue([]);
        vi.mocked(SaveProjectTabConversation).mockResolvedValue(undefined as any);
    });

    function renderExpertOpen(lang = "zh-Hans") {
        const sendExpertMessage = vi.fn();
        const hook = renderHook(() => {
            const manager = useAITabManager();
            const [pending, setPending] = useState<PendingExpertOpen | null>(null);
            usePendingAssistantTabOpen({
                lang,
                createVETab: manager.createVETab,
                createGroupTab: manager.createGroupTab,
                createProjectTab: manager.createProjectTab,
                createExpertTab: manager.createExpertTab,
                activateTab: manager.activateTab,
                getTabState: manager.getTabState,
                saveTabState: manager.saveTabState,
                getTabList: manager.getTabs,
                pendingExpertOpen: pending,
                onPendingExpertOpenHandled: () => setPending(null),
                sendExpertMessage,
            });
            return { manager, setPending };
        });
        return { ...hook, sendExpertMessage };
    }

    it("seeds exactly one local welcome message on first open (no duplicate on re-open)", () => {
        const { result } = renderExpertOpen();
        act(() => {
            result.current.setPending({ expert: expertA });
        });
        const tabId = expertTabId(expertA.id);
        expect(result.current.manager.tabState.activeTabId).toBe(tabId);
        const firstState = result.current.manager.getTabState(tabId);
        expect(firstState?.history?.length).toBe(1);
        const welcome = (firstState!.history as any[])[0];
        expect(welcome.role).toBe("assistant");
        expect(welcome.content).toBe("你好，我是论文润色。学术语言润色 有什么可以帮你？");
        expect(welcome.sessionKey).toBe("desktop-user:expert:exp-paper-polish");

        // Simulate a conversation so we can prove re-open does not re-seed.
        act(() => {
            result.current.manager.saveTabState(tabId, {
                history: [...(firstState!.history as any[]), { id: "u1", role: "user", content: "hi", timestamp: 2 }],
            });
        });
        act(() => {
            result.current.manager.activateTab("local");
        });
        act(() => {
            result.current.setPending({ expert: expertA });
        });
        expect(result.current.manager.tabState.activeTabId).toBe(tabId);
        expect(result.current.manager.getTabState(tabId)?.history?.length).toBe(2);
        // Still exactly one welcome message.
        const welcomes = (result.current.manager.getTabState(tabId)!.history as any[])
            .filter(m => String(m.id || "").startsWith("expert-welcome-"));
        expect(welcomes.length).toBe(1);
    });

    it("binds the expert tab to its task directory and opens the LaTeX source", async () => {
        resetLatexDocumentRequestForTests();
        const sendExpertMessage = vi.fn();
        const { result } = renderHook(() => {
            const manager = useAITabManager();
            const [pending, setPending] = useState<PendingExpertOpen | null>(null);
            usePendingAssistantTabOpen({
                lang: "zh-Hans",
                createVETab: manager.createVETab,
                createGroupTab: manager.createGroupTab,
                createProjectTab: manager.createProjectTab,
                createExpertTab: manager.createExpertTab,
                activateTab: manager.activateTab,
                getTabState: manager.getTabState,
                saveTabState: manager.saveTabState,
                getTabList: manager.getTabs,
                pendingExpertOpen: pending,
                onPendingExpertOpenHandled: () => setPending(null),
                sendExpertMessage,
                onEnsureExpertTask: async () => ({ projectPath: "D:/tasks/paper", relativePath: "main.tex" }),
            });
            return { manager, setPending };
        });
        act(() => {
            result.current.setPending({ expert: expertA });
        });
        await waitFor(() => {
            const tab = result.current.manager.tabState.tabs.find(item => item.expertId === expertA.id);
            expect(tab?.projectPath).toBe("D:/tasks/paper");
        });
        expect(takePendingLatexDocument()).toMatchObject({
            projectPath: "D:/tasks/paper",
            relativePath: "main.tex",
        });
        expect(result.current.manager.tabState.tabs.find(item => item.expertId === expertA.id)?.latexRelativePath).toBe("main.tex");
    });

    it("reattaches a restored LaTeX expert tab that has no task directory", async () => {
        resetLatexDocumentRequestForTests();
        const latexExpert: ExpertDefinition = {
            ...expertA,
            id: LATEX_EXPERT_ID,
            name: "LaTeX",
        };
        const ensure = vi.fn(async (_expert: ExpertDefinition, existing?: { relativePath?: string }) => ({
            projectPath: "D:/tasks/paper",
            relativePath: existing?.relativePath || "main.tex",
        }));
        const { result } = renderHook(() => {
            const manager = useAITabManager();
            usePendingAssistantTabOpen({
                lang: "zh-Hans",
                createVETab: manager.createVETab,
                createGroupTab: manager.createGroupTab,
                createProjectTab: manager.createProjectTab,
                createExpertTab: manager.createExpertTab,
                activateTab: manager.activateTab,
                getTabState: manager.getTabState,
                saveTabState: manager.saveTabState,
                getTabList: manager.getTabs,
                onEnsureExpertTask: ensure,
            });
            return { manager };
        });
        act(() => {
            result.current.manager.createExpertTab(expertA);
            result.current.manager.createExpertTab(latexExpert, { relativePath: "thesis.tex" });
        });
        await waitFor(() => {
            const tab = result.current.manager.tabState.tabs.find(item => item.expertId === LATEX_EXPERT_ID);
            expect(tab?.projectPath).toBe("D:/tasks/paper");
            expect(tab?.latexRelativePath).toBe("thesis.tex");
        });
        expect(ensure).toHaveBeenCalledTimes(1);
        expect(ensure).toHaveBeenCalledWith(
            expect.objectContaining({ id: LATEX_EXPERT_ID }),
            { relativePath: "thesis.tex" },
        );
        expect(result.current.manager.tabState.tabs.find(item => item.expertId === expertA.id)?.projectPath).toBeUndefined();
    });

    it("reopens the template source instead of a blank main.tex", async () => {
        resetLatexDocumentRequestForTests();
        const ensure = vi.fn(async (_expert: ExpertDefinition, existing?: { relativePath?: string }) => ({
            projectPath: "D:/tasks/paper",
            relativePath: existing?.relativePath || "main.tex",
        }));
        const { result } = renderHook(() => {
            const manager = useAITabManager();
            const [pending, setPending] = useState<PendingExpertOpen | null>(null);
            usePendingAssistantTabOpen({
                lang: "zh-Hans",
                createVETab: manager.createVETab,
                createGroupTab: manager.createGroupTab,
                createProjectTab: manager.createProjectTab,
                createExpertTab: manager.createExpertTab,
                activateTab: manager.activateTab,
                getTabState: manager.getTabState,
                saveTabState: manager.saveTabState,
                getTabList: manager.getTabs,
                pendingExpertOpen: pending,
                onPendingExpertOpenHandled: () => setPending(null),
                onEnsureExpertTask: ensure,
            });
            return { manager, setPending };
        });
        act(() => {
            result.current.setPending({ expert: expertA, latexDocument: { relativePath: "thesis.tex" } });
        });
        await waitFor(() => {
            expect(ensure).toHaveBeenCalledWith(expertA, { relativePath: "thesis.tex" });
        });
        expect(takePendingLatexDocument()).toMatchObject({
            projectPath: "D:/tasks/paper",
            relativePath: "thesis.tex",
        });
    });

    it("seeds welcome for a restored tab whose history is empty", () => {
        const { result } = renderExpertOpen("en");
        act(() => {
            result.current.setPending({ expert: expertB });
        });
        const tabId = expertTabId(expertB.id);
        const state = result.current.manager.getTabState(tabId);
        expect(state?.history?.length).toBe(1);
        expect((state!.history as any[])[0].content).toBe("Hi, I'm PPT 制作. How can I help you?");
    });

    // Wizard handoff (new-task wizard → expert branch): the first message is
    // sent through the freshly opened expert tab via sendExpertMessage, on
    // BOTH sub-paths — tab created now and tab that already existed.
    it("wizard handoff sends the initial message through a newly created expert tab", () => {
        const { result, sendExpertMessage } = renderExpertOpen();
        act(() => {
            result.current.setPending({ expert: expertA, initialMessage: "帮我润色摘要" });
        });
        const tabId = expertTabId(expertA.id);
        expect(result.current.manager.tabState.activeTabId).toBe(tabId);
        // The local welcome seed still happens; the message itself travels the
        // expert chat channel, not the local history.
        expect(result.current.manager.getTabState(tabId)?.history?.length).toBe(1);
        expect(sendExpertMessage).toHaveBeenCalledTimes(1);
        expect(sendExpertMessage).toHaveBeenCalledWith("帮我润色摘要", "exp-paper-polish");
    });

    it("wizard handoff still sends the initial message when the expert tab already exists", () => {
        const { result, sendExpertMessage } = renderExpertOpen();
        act(() => {
            result.current.setPending({ expert: expertA });
        });
        const tabId = expertTabId(expertA.id);
        // Simulate an existing conversation so re-open must not re-seed.
        act(() => {
            result.current.manager.saveTabState(tabId, {
                history: [
                    ...(result.current.manager.getTabState(tabId)!.history as any[]),
                    { id: "u1", role: "user", content: "hi", timestamp: 2 },
                ],
            });
        });
        act(() => {
            result.current.manager.activateTab("local");
        });
        act(() => {
            result.current.setPending({ expert: expertA, initialMessage: "第二段也润色一下" });
        });
        expect(result.current.manager.tabState.activeTabId).toBe(tabId);
        expect(sendExpertMessage).toHaveBeenCalledTimes(1);
        expect(sendExpertMessage).toHaveBeenCalledWith("第二段也润色一下", "exp-paper-polish");
        // No re-seed: history keeps exactly the welcome + the prior user turn.
        expect(result.current.manager.getTabState(tabId)?.history?.length).toBe(2);
    });

    it("a new LaTeX wizard launch clears the previous paper before sending", async () => {
        const latexExpert: ExpertDefinition = { ...expertA, id: LATEX_EXPERT_ID, name: "LaTeX" };
        const order: string[] = [];
        const sendExpertMessage = vi.fn(() => { order.push("send"); });
        const { result } = renderHook(() => {
            const manager = useAITabManager();
            const [pending, setPending] = useState<PendingExpertOpen | null>(null);
            const resetExpertConversation = async (_expertId: string, previousProjectPath?: string) => {
                order.push("reset");
                order.push(previousProjectPath || "");
                const tabId = expertTabId(LATEX_EXPERT_ID);
                await manager.stashProjectTabHistory(previousProjectPath || "", manager.getTabState(tabId)?.history || []);
                manager.clearTabConversation(tabId);
            };
            usePendingAssistantTabOpen({
                lang: "zh-Hans",
                createVETab: manager.createVETab,
                createGroupTab: manager.createGroupTab,
                createProjectTab: manager.createProjectTab,
                createExpertTab: manager.createExpertTab,
                activateTab: manager.activateTab,
                getTabState: manager.getTabState,
                saveTabState: manager.saveTabState,
                getTabList: manager.getTabs,
                pendingExpertOpen: pending,
                onPendingExpertOpenHandled: () => setPending(null),
                sendExpertMessage,
                resetExpertConversation,
                onEnsureExpertTask: async () => {
                    manager.createExpertTab(latexExpert, { projectPath: "D:/tasks/new-paper" });
                    return { projectPath: "D:/tasks/new-paper", relativePath: "main.tex" };
                },
            });
            return { manager, setPending };
        });
        act(() => {
            result.current.manager.createExpertTab(latexExpert, { projectPath: "D:/tasks/old-paper" });
            result.current.manager.saveTabState(expertTabId(LATEX_EXPERT_ID), {
                history: [
                    { id: "u1", role: "user", content: "old paper", timestamp: 1 },
                    { id: "a1", role: "assistant", content: "compiled", timestamp: 2 },
                ],
            });
        });
        act(() => {
            result.current.setPending({ expert: latexExpert, initialMessage: "生成一份演示用学术论文" });
        });
        await waitFor(() => {
            expect(sendExpertMessage).toHaveBeenCalledWith("生成一份演示用学术论文", LATEX_EXPERT_ID);
        });
        expect(order).toEqual(["reset", "D:/tasks/old-paper", "send"]);
        expect(result.current.manager.getTabState(expertTabId(LATEX_EXPERT_ID))?.history).toEqual([]);
        const stashed = (SaveProjectTabConversation as unknown as ReturnType<typeof vi.fn>).mock.calls;
        expect(stashed.some(call => String(call[0]).startsWith("proj-") && Array.isArray(call[1]) && call[1].length === 2)).toBe(true);
    });

    it("a failed LaTeX open drops the paper that was never shown", async () => {
        const latexExpert: ExpertDefinition = { ...expertA, id: LATEX_EXPERT_ID, name: "LaTeX" };
        const abandon = AbandonUnopenedFreshLatexTask as unknown as ReturnType<typeof vi.fn>;
        abandon.mockClear();
        const sendExpertMessage = vi.fn();
        const { result } = renderHook(() => {
            const manager = useAITabManager();
            const [pending, setPending] = useState<PendingExpertOpen | null>(null);
            usePendingAssistantTabOpen({
                lang: "zh-Hans",
                createVETab: manager.createVETab,
                createGroupTab: manager.createGroupTab,
                createProjectTab: manager.createProjectTab,
                createExpertTab: manager.createExpertTab,
                activateTab: manager.activateTab,
                getTabState: manager.getTabState,
                saveTabState: manager.saveTabState,
                getTabList: manager.getTabs,
                pendingExpertOpen: pending,
                onPendingExpertOpenHandled: () => setPending(null),
                sendExpertMessage,
                onEnsureExpertTask: async () => {
                    throw new Error("task record was not saved");
                },
            });
            return { setPending };
        });
        act(() => {
            result.current.setPending({
                expert: latexExpert,
                initialMessage: "生成一份演示用学术论文",
                projectPath: "D:/tasks/new-paper",
            });
        });
        await waitFor(() => {
            expect(abandon).toHaveBeenCalledWith("D:/tasks/new-paper");
        });
        expect(sendExpertMessage).not.toHaveBeenCalled();
    });

    it("resuming the live LaTeX paper does not abandon it when registration fails", async () => {
        const latexExpert: ExpertDefinition = { ...expertA, id: LATEX_EXPERT_ID, name: "LaTeX" };
        const abandon = AbandonUnopenedFreshLatexTask as unknown as ReturnType<typeof vi.fn>;
        abandon.mockClear();
        const ensure = vi.fn(async () => {
            throw new Error("task record was not saved");
        });
        const { result } = renderHook(() => {
            const manager = useAITabManager();
            const [pending, setPending] = useState<PendingExpertOpen | null>(null);
            usePendingAssistantTabOpen({
                lang: "zh-Hans",
                createVETab: manager.createVETab,
                createGroupTab: manager.createGroupTab,
                createProjectTab: manager.createProjectTab,
                createExpertTab: manager.createExpertTab,
                activateTab: manager.activateTab,
                getTabState: manager.getTabState,
                saveTabState: manager.saveTabState,
                getTabList: manager.getTabs,
                pendingExpertOpen: pending,
                onPendingExpertOpenHandled: () => setPending(null),
                onEnsureExpertTask: ensure,
            });
            return { setPending };
        });
        act(() => {
            result.current.setPending({ expert: latexExpert, projectPath: "D:/tasks/live-paper" });
        });
        await waitFor(() => {
            expect(ensure).toHaveBeenCalled();
        });
        expect(abandon).not.toHaveBeenCalled();
    });

    it("expert open without an initial message never calls sendExpertMessage", () => {
        const { result, sendExpertMessage } = renderExpertOpen();
        act(() => {
            result.current.setPending({ expert: expertA });
        });
        act(() => {
            result.current.setPending({ expert: expertA });
        });
        expect(sendExpertMessage).not.toHaveBeenCalled();
    });
});

describe("expert tab defaults", () => {
    it("falls back to the default emoji icon constant", () => {
        expect(DEFAULT_EXPERT_ICON).toBe("🤖");
    });
});

describe("expert tab backend session-file persistence", () => {
    beforeEach(() => {
        localStorage.clear();
        vi.clearAllMocks();
        vi.mocked(LoadProjectTabIndex).mockResolvedValue([]);
        vi.mocked(CloseAssistantTabSession).mockResolvedValue(undefined as any);
        vi.mocked(CreateProjectTabSession).mockResolvedValue("" as any);
        vi.mocked(LoadProjectTabConversation).mockResolvedValue([]);
        vi.mocked(SaveProjectTabConversation).mockResolvedValue(undefined as any);
    });

    it("persists expert tab history to the backend session file (debounced)", async () => {
        const { result } = renderHook(() => useAITabManager());
        act(() => {
            result.current.createExpertTab(expertA);
        });
        act(() => {
            result.current.saveTabState(expertTabId(expertA.id), {
                history: [{ id: "m1", role: "assistant", content: "hello", timestamp: 1 }],
            });
        });
        await act(async () => {
            await new Promise(resolve => setTimeout(resolve, 650));
        });
        expect(SaveProjectTabConversation).toHaveBeenCalledWith(
            expertTabId(expertA.id),
            expect.arrayContaining([expect.objectContaining({ id: "m1" })]),
        );
    });

    it("hydrates expert tab history from the backend session file on mount", async () => {
        // Pre-seed localStorage metadata so the expert tab is restored on mount.
        localStorage.setItem("ai_assistant_project_tabs", JSON.stringify([
            { id: expertTabId(expertA.id), type: "expert", title: expertA.name, expertId: expertA.id, expertIcon: expertA.icon },
        ]));
        vi.mocked(LoadProjectTabConversation).mockImplementation(async (tabId: string) =>
            tabId === expertTabId(expertA.id)
                ? [{ id: "b1", role: "assistant", content: "from backend", timestamp: 2 }]
                : []);
        const { result } = renderHook(() => useAITabManager());
        // Hydration is async (fires after the backend index merge effect).
        await act(async () => {
            await new Promise(resolve => setTimeout(resolve, 50));
        });
        expect(LoadProjectTabConversation).toHaveBeenCalledWith(expertTabId(expertA.id));
        const state = result.current.getTabState(expertTabId(expertA.id));
        expect((state?.history as any[])?.[0]?.content).toBe("from backend");
    });

    it("clearTabConversation clears the expert backend session file too", () => {
        const { result } = renderHook(() => useAITabManager());
        act(() => {
            result.current.createExpertTab(expertA);
        });
        act(() => {
            result.current.clearTabConversation(expertTabId(expertA.id));
        });
        expect(SaveProjectTabConversation).toHaveBeenCalledWith(expertTabId(expertA.id), []);
    });
});

describe("expert tab sync with utilities-page expert changes", () => {
    beforeEach(() => {
        localStorage.clear();
        vi.clearAllMocks();
        vi.mocked(LoadProjectTabIndex).mockResolvedValue([]);
        vi.mocked(CloseAssistantTabSession).mockResolvedValue(undefined as any);
        vi.mocked(CreateProjectTabSession).mockResolvedValue("" as any);
        vi.mocked(LoadProjectTabConversation).mockResolvedValue([]);
        vi.mocked(SaveProjectTabConversation).mockResolvedValue(undefined as any);
    });

    it("maclaw:expert-deleted discards the open expert tab and its history", () => {
        const { result } = renderHook(() => useAITabManager());
        const tabId = expertTabId(expertA.id);
        act(() => {
            result.current.createExpertTab(expertA);
            result.current.saveTabState(tabId, {
                history: [{ id: "m1", role: "user", content: "should be wiped", timestamp: 1 }],
            });
        });
        expect(result.current.tabState.activeTabId).toBe(tabId);
        act(() => {
            window.dispatchEvent(new CustomEvent("maclaw:expert-deleted", { detail: { expertId: expertA.id } }));
        });
        expect(result.current.tabState.tabs.some(t => t.type === "expert")).toBe(false);
        expect(result.current.tabState.activeTabId).toBe("local");
        expect(result.current.getTabState(tabId)).toBeUndefined();
        expect(SaveProjectTabConversation).toHaveBeenCalledWith(tabId, []);
        expect(ClearAIAssistantHistoryForSession).toHaveBeenCalledWith(expertSessionKey(expertA.id));
    });

    it("maclaw:expert-updated patches title/icon in place without switching tabs", () => {
        const { result } = renderHook(() => useAITabManager());
        act(() => {
            result.current.createExpertTab(expertA);
        });
        act(() => {
            result.current.activateTab("local");
        });
        act(() => {
            window.dispatchEvent(new CustomEvent("maclaw:expert-updated", {
                detail: { expert: { ...expertA, name: "润色 Pro", icon: "✨", description: "新描述" } },
            }));
        });
        const tab = result.current.tabState.tabs.find(t => t.type === "expert");
        expect(tab?.title).toBe("润色 Pro");
        expect(tab?.expertIcon).toBe("✨");
        expect(tab?.expertDescription).toBe("新描述");
        // No foreground switch.
        expect(result.current.tabState.activeTabId).toBe("local");
    });

    it("ignores events for experts without an open tab", () => {
        const { result } = renderHook(() => useAITabManager());
        act(() => {
            window.dispatchEvent(new CustomEvent("maclaw:expert-deleted", { detail: { expertId: "nobody" } }));
            window.dispatchEvent(new CustomEvent("maclaw:expert-updated", { detail: { expert: expertA } }));
        });
        expect(result.current.tabState.tabs.length).toBe(1); // local only
    });
});
