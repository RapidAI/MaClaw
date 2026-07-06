/**
 * Property tests for useAITabManager - tab creation (Properties 1-7)
 *
 * **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 2.1, 2.2, 2.3, 2.4**
 *
 * These tests validate universal correctness properties of the tab system
 * using fast-check for property-based testing.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { renderHook, act, waitFor } from "@testing-library/react";
import fc from "fast-check";
import { useAITabManager } from "../useAITabManager";
import { normalizeProjectSessionPath } from "../aiAssistantPanelSessionUtils";
import { CloseProjectTabSession, CreateProjectTabSession, LoadProjectTabConversation, LoadProjectTabIndex, SaveProjectTabConversation } from "../../../../wailsjs/go/main/App";

vi.mock("../../../../wailsjs/runtime", () => ({
    EventsOn: vi.fn(() => vi.fn()),
    EventsOff: vi.fn(),
}));

vi.mock("../../../../wailsjs/go/main/App", () => ({
    LoadProjectTabIndex: vi.fn().mockResolvedValue([]),
    CloseProjectTabSession: vi.fn().mockResolvedValue(undefined),
    CreateProjectTabSession: vi.fn().mockResolvedValue(undefined),
    SaveProjectTabConversation: vi.fn().mockResolvedValue(undefined),
    LoadProjectTabConversation: vi.fn().mockResolvedValue([]),
}));

// Arbitrary for generating valid project paths (non-empty strings)
const projectPathArb = fc.string({ minLength: 1, maxLength: 200 }).filter(s => s.trim().length > 0);
const taskTitleArb = fc.string({ minLength: 1, maxLength: 100 }).filter(s => s.trim().length > 0);

describe("useAITabManager - Property Tests for Tab Creation", () => {
    // Clear localStorage between tests to prevent cross-test pollution
    // (loadPersistedProjectTabs reads from localStorage on hook mount)
    beforeEach(() => {
        localStorage.clear();
        vi.useRealTimers();
        vi.clearAllMocks();
        vi.mocked(LoadProjectTabIndex).mockResolvedValue([]);
        vi.mocked(CloseProjectTabSession).mockResolvedValue(undefined as any);
        vi.mocked(CreateProjectTabSession).mockResolvedValue("");
        vi.mocked(LoadProjectTabConversation).mockResolvedValue([]);
        vi.mocked(SaveProjectTabConversation).mockResolvedValue(undefined as any);
    });
    /**
     * Property 1: Tab creation produces correct structure
     * createProjectTab returns a tab with type="project", correct projectPath, closable=true
     *
     * **Validates: Requirements 1.1**
     */
    describe("Property 1: Tab creation produces correct structure", () => {
        it("createProjectTab returns a tab with type='project', correct projectPath, closable=true", () => {
            fc.assert(
                fc.property(projectPathArb, taskTitleArb, (projectPath, taskTitle) => {
                    localStorage.clear();
                    const { result } = renderHook(() => useAITabManager());

                    let tab: ReturnType<typeof result.current.createProjectTab> = null;
                    act(() => {
                        tab = result.current.createProjectTab(projectPath, taskTitle);
                    });

                    expect(tab).not.toBeNull();
                    expect(tab!.type).toBe("project");
                    expect(tab!.projectPath).toBe(normalizeProjectSessionPath(projectPath));
                    expect(tab!.closable).toBe(true);
                    expect(tab!.title).toBe(taskTitle);
                    expect(tab!.id).toMatch(/^proj-[0-9a-f]{12}$/);
                }),
                { numRuns: 50 }
            );
        });
    });

    /**
     * Property 2: Tab auto-activation
     * After createProjectTab, the new tab is the active tab
     *
     * **Validates: Requirements 1.2**
     */
    describe("Property 2: Tab auto-activation", () => {
        it("after createProjectTab, the new tab is the active tab", () => {
            fc.assert(
                fc.property(projectPathArb, taskTitleArb, (projectPath, taskTitle) => {
                    localStorage.clear();
                    const { result } = renderHook(() => useAITabManager());

                    let tab: ReturnType<typeof result.current.createProjectTab> = null;
                    act(() => {
                        tab = result.current.createProjectTab(projectPath, taskTitle);
                    });

                    expect(tab).not.toBeNull();
                    expect(result.current.tabState.activeTabId).toBe(tab!.id);
                    expect(result.current.activeTab.id).toBe(tab!.id);
                }),
                { numRuns: 50 }
            );
        });
    });

    /**
     * Property 3: Tab deduplication (idempotence)
     * Calling createProjectTab twice with same projectPath returns the same tab
     * and doesn't create duplicates
     *
     * **Validates: Requirements 1.3**
     */
    describe("Property 3: Tab deduplication (idempotence)", () => {
        it("calling createProjectTab twice with same projectPath returns the same tab and doesn't create duplicates", () => {
            fc.assert(
                fc.property(projectPathArb, taskTitleArb, taskTitleArb, (projectPath, title1, title2) => {
                    localStorage.clear();
                    const { result } = renderHook(() => useAITabManager());

                    let tab1: ReturnType<typeof result.current.createProjectTab> = null;
                    let tab2: ReturnType<typeof result.current.createProjectTab> = null;

                    act(() => {
                        tab1 = result.current.createProjectTab(projectPath, title1);
                    });
                    act(() => {
                        tab2 = result.current.createProjectTab(projectPath, title2);
                    });

                    // Same tab returned (same id)
                    expect(tab1).not.toBeNull();
                    expect(tab2).not.toBeNull();
                    expect(tab1!.id).toBe(tab2!.id);

                    // No duplicates in tab list
                    const normalizedPath = normalizeProjectSessionPath(projectPath);
                    const projectTabs = result.current.tabState.tabs.filter(
                        t => t.type === "project" && t.projectPath === normalizedPath
                    );
                    expect(projectTabs.length).toBe(1);
                }),
                { numRuns: 50 }
            );
        });
    });

    /**
     * Property 4: Local tab invariant
     * The local tab is never removed or modified by project tab operations
     *
     * **Validates: Requirements 1.4**
     */
    describe("Property 4: Local tab invariant", () => {
        it("the local tab is never removed or modified by project tab operations", () => {
            fc.assert(
                fc.property(
                    fc.array(fc.tuple(projectPathArb, taskTitleArb), { minLength: 1, maxLength: 8 }),
                    (tabSpecs) => {
                        localStorage.clear();
                    const { result } = renderHook(() => useAITabManager());

                        // Create multiple project tabs
                        act(() => {
                            for (const [path, title] of tabSpecs) {
                                result.current.createProjectTab(path, title);
                            }
                        });

                        // Local tab must still exist
                        const localTab = result.current.tabState.tabs.find(t => t.id === "local");
                        expect(localTab).toBeDefined();
                        expect(localTab!.type).toBe("local");
                        expect(localTab!.closable).toBe(false);

                        // Close all project tabs
                        act(() => {
                            const projectTabs = result.current.tabState.tabs.filter(t => t.type === "project");
                            for (const tab of projectTabs) {
                                result.current.closeTab(tab.id);
                            }
                        });

                        // Local tab still exists after closing all project tabs
                        const localTabAfter = result.current.tabState.tabs.find(t => t.id === "local");
                        expect(localTabAfter).toBeDefined();
                        expect(localTabAfter!.type).toBe("local");
                        expect(localTabAfter!.closable).toBe(false);
                    }
                ),
                { numRuns: 30 }
            );
        });
    });

    /**
     * Property 5: Tab state round-trip
     * saveTabState followed by getTabState returns the same data
     *
     * **Validates: Requirements 2.1, 2.2**
     */
    describe("Property 5: Tab state round-trip", () => {
        it("saveTabState followed by getTabState returns the same data", () => {
            fc.assert(
                fc.property(
                    projectPathArb,
                    taskTitleArb,
                    fc.nat(10000),
                    fc.string(),
                    (projectPath, taskTitle, scrollTop, inputText) => {
                        localStorage.clear();
                    const { result } = renderHook(() => useAITabManager());

                        let tab: ReturnType<typeof result.current.createProjectTab> = null;
                        act(() => {
                            tab = result.current.createProjectTab(projectPath, taskTitle);
                        });

                        expect(tab).not.toBeNull();

                        // Save state
                        const stateToSave = { scrollTop, inputText };
                        act(() => {
                            result.current.saveTabState(tab!.id, stateToSave);
                        });

                        // Get state
                        const retrieved = result.current.getTabState(tab!.id);
                        expect(retrieved).toBeDefined();
                        expect(retrieved!.scrollTop).toBe(scrollTop);
                        expect(retrieved!.inputText).toBe(inputText);
                    }
                ),
                { numRuns: 50 }
            );
        });
    });

    /**
     * Property 6: Tab state isolation
     * Different tabs have independent states
     *
     * **Validates: Requirements 2.3**
     */
    describe("Property 6: Tab state isolation", () => {
        it("different tabs have independent states", () => {
            fc.assert(
                fc.property(
                    projectPathArb,
                    projectPathArb,
                    taskTitleArb,
                    taskTitleArb,
                    fc.nat(10000),
                    fc.nat(10000),
                    fc.string(),
                    fc.string(),
                    (path1, path2, title1, title2, scroll1, scroll2, input1, input2) => {
                        // Ensure paths are different
                        fc.pre(path1 !== path2);

                        localStorage.clear();
                    const { result } = renderHook(() => useAITabManager());

                        let tab1: ReturnType<typeof result.current.createProjectTab> = null;
                        let tab2: ReturnType<typeof result.current.createProjectTab> = null;

                        act(() => {
                            tab1 = result.current.createProjectTab(path1, title1);
                        });
                        act(() => {
                            tab2 = result.current.createProjectTab(path2, title2);
                        });

                        expect(tab1).not.toBeNull();
                        expect(tab2).not.toBeNull();

                        // Save different states to each tab
                        act(() => {
                            result.current.saveTabState(tab1!.id, { scrollTop: scroll1, inputText: input1 });
                            result.current.saveTabState(tab2!.id, { scrollTop: scroll2, inputText: input2 });
                        });

                        // Verify states are independent
                        const state1 = result.current.getTabState(tab1!.id);
                        const state2 = result.current.getTabState(tab2!.id);

                        expect(state1).toBeDefined();
                        expect(state2).toBeDefined();
                        expect(state1!.scrollTop).toBe(scroll1);
                        expect(state1!.inputText).toBe(input1);
                        expect(state2!.scrollTop).toBe(scroll2);
                        expect(state2!.inputText).toBe(input2);
                    }
                ),
                { numRuns: 50 }
            );
        });
    });

    /**
     * Property 7: Tab close cleanup
     * After closing a tab, it's no longer in the tab list and active falls back to local.
     * Note: Project tabs intentionally retain their cached state for quick restoration.
     *
     * **Validates: Requirements 2.4**
     */
    describe("Property 7: Tab close cleanup", () => {
        it("after closing a tab, it's no longer in the tab list and active falls back to local", () => {
            fc.assert(
                fc.property(projectPathArb, taskTitleArb, (projectPath, taskTitle) => {
                    localStorage.clear();
                    const { result } = renderHook(() => useAITabManager());

                    let tab: ReturnType<typeof result.current.createProjectTab> = null;
                    act(() => {
                        tab = result.current.createProjectTab(projectPath, taskTitle);
                    });

                    expect(tab).not.toBeNull();
                    const tabId = tab!.id;

                    // Verify tab exists
                    expect(result.current.tabState.tabs.some(t => t.id === tabId)).toBe(true);
                    expect(result.current.getTabState(tabId)).toBeDefined();

                    // Close the tab
                    act(() => {
                        result.current.closeTab(tabId);
                    });

                    // Tab is no longer in the list
                    expect(result.current.tabState.tabs.some(t => t.id === tabId)).toBe(false);

                    // Project tabs retain cached state for quick restoration (by design)
                    // so getTabState may still return a value.

                    // Active tab falls back to local
                    expect(result.current.tabState.activeTabId).toBe("local");
                }),
                { numRuns: 50 }
            );
        });
    });

    it("prunes localStorage-restored project tabs absent from the backend active index", async () => {
        localStorage.setItem("ai_assistant_project_tabs", JSON.stringify([
            { id: "proj-stale", title: "Stale task", projectPath: "D:/tasks/stale" },
            { id: "proj-active", title: "Active task", projectPath: "D:/tasks/active" },
        ]));
        vi.mocked(LoadProjectTabIndex).mockResolvedValue([
            { id: "proj-active", type: "project", title: "Active task", projectPath: "D:/tasks/active", lastActiveAt: 1, archived: false },
        ] as any);

        const { result } = renderHook(() => useAITabManager());
        expect(result.current.tabState.tabs.some(t => t.projectPath === "D:/tasks/stale")).toBe(true);

        await waitFor(() => {
            expect(result.current.tabState.tabs.some(t => t.projectPath === "D:/tasks/stale")).toBe(false);
            expect(result.current.tabState.tabs.some(t => t.projectPath === "D:/tasks/active")).toBe(true);
        });
    });

    it("updates a localStorage-restored project tab title from the backend index", async () => {
        localStorage.setItem("ai_assistant_project_tabs", JSON.stringify([
            { id: "proj-weather", title: "Task 17804916...", projectPath: "D:/tasks/weather-fork" },
        ]));
        vi.mocked(LoadProjectTabIndex).mockResolvedValue([
            { id: "proj-weather", type: "project", title: "北京天气", projectPath: "D:/tasks/weather-fork", lastActiveAt: 1, archived: false },
        ] as any);

        const { result } = renderHook(() => useAITabManager());
        expect(result.current.tabState.tabs.find(t => t.projectPath === "D:/tasks/weather-fork")?.title).toBe("Task 17804916...");

        await waitFor(() => {
            expect(result.current.tabState.tabs.find(t => t.projectPath === "D:/tasks/weather-fork")?.title).toBe("北京天气");
        });
    });

    it("does not restore transient guide receipts from persisted project tab history", async () => {
        const realMessage = { id: "assistant-1", role: "assistant", content: "继续处理", timestamp: 2 };
        localStorage.setItem("ai_assistant_project_tabs", JSON.stringify([
            { id: "proj-guide", title: "Guide task", projectPath: "D:/tasks/guide" },
        ]));
        localStorage.setItem("ai_assistant_project_tab_histories", JSON.stringify({
            "proj-guide": [
                { id: "receipt-1", role: "system", kind: "guideReceipt", content: "这条补充已接上当前任务", timestamp: 1 },
                realMessage,
            ],
        }));
        vi.mocked(LoadProjectTabIndex).mockResolvedValue([
            { id: "proj-guide", type: "project", title: "Guide task", projectPath: "D:/tasks/guide", lastActiveAt: 1, archived: false },
        ] as any);

        const { result } = renderHook(() => useAITabManager());

        expect(result.current.getTabState("proj-guide")?.history).toEqual([realMessage]);
    });

    it("does not persist transient guide receipts in project tab conversation history", async () => {
        vi.useFakeTimers();
        const realMessage = { id: "assistant-1", role: "assistant", content: "继续处理", timestamp: 2 };
        const { result } = renderHook(() => useAITabManager());

        let tab: ReturnType<typeof result.current.createProjectTab> = null;
        act(() => {
            tab = result.current.createProjectTab("D:/tasks/guide-save", "Guide save");
        });
        expect(tab).not.toBeNull();

        act(() => {
            result.current.saveTabState(tab!.id, {
                history: [
                    { id: "receipt-1", role: "system", kind: "guideReceipt", content: "这条补充已接上当前任务", timestamp: 1 },
                    { id: "rejection-1", role: "system", kind: "guideRejection", content: "我听到了，但这条补充暂时还没接上当前任务", timestamp: 1 },
                    realMessage,
                ],
            });
        });

        await act(async () => {
            vi.advanceTimersByTime(600);
            await Promise.resolve();
        });

        expect(SaveProjectTabConversation).toHaveBeenCalledWith(tab!.id, [realMessage]);
    });

    it("cleans transient guide receipts from backend-restored project tab history", async () => {
        const realMessage = { id: "assistant-1", role: "assistant", content: "继续处理", timestamp: 2 };
        vi.mocked(LoadProjectTabIndex).mockResolvedValue([
            { id: "proj-guide-backend", type: "project", title: "Guide backend", projectPath: "D:/tasks/guide-backend", lastActiveAt: 1, archived: false },
        ] as any);
        vi.mocked(LoadProjectTabConversation).mockResolvedValue([
            { id: "receipt-1", role: "system", kind: "guideReceipt", content: "这条补充已接上当前任务", timestamp: 1 },
            { id: "rejection-1", role: "system", kind: "guideRejection", content: "我听到了，但这条补充暂时还没接上当前任务", timestamp: 1 },
            realMessage,
        ] as any);

        const { result } = renderHook(() => useAITabManager());

        await waitFor(() => {
            expect(result.current.getTabState("proj-guide-backend")?.history).toEqual([realMessage]);
            expect(SaveProjectTabConversation).toHaveBeenCalledWith("proj-guide-backend", [realMessage]);
        });
    });

    it("keeps a freshly opened project tab when backend restore resolves late", async () => {
        let resolveIndex!: (entries: any[]) => void;
        vi.mocked(LoadProjectTabIndex).mockReturnValueOnce(new Promise(resolve => {
            resolveIndex = resolve;
        }) as any);

        const { result } = renderHook(() => useAITabManager());

        act(() => {
            result.current.createProjectTab("D:/tasks/fresh-open", "Fresh open");
        });
        expect(result.current.tabState.tabs.some(t => t.projectPath === "D:/tasks/fresh-open")).toBe(true);

        await act(async () => {
            resolveIndex([]);
        });

        await waitFor(() => {
            expect(result.current.tabState.tabs.some(t => t.projectPath === "D:/tasks/fresh-open")).toBe(true);
            expect(result.current.tabState.activeTabId).not.toBe("local");
        });
    });

    it("serializes project tab reopen after pending close for the same deterministic tab", async () => {
        let resolveClose!: () => void;
        vi.mocked(CloseProjectTabSession).mockImplementation(() => new Promise<void>(resolve => {
            resolveClose = resolve;
        }) as any);

        const { result } = renderHook(() => useAITabManager());

        act(() => {
            result.current.createProjectTab("D:/tasks/reopen-race", "Reopen race");
        });
        expect(CreateProjectTabSession).toHaveBeenCalledTimes(1);

        const tabId = result.current.tabState.tabs.find(t => t.projectPath === "D:/tasks/reopen-race")?.id;
        expect(tabId).toBeTruthy();

        act(() => {
            result.current.closeTab(tabId!);
        });
        await waitFor(() => expect(CloseProjectTabSession).toHaveBeenCalledWith(tabId));

        act(() => {
            result.current.createProjectTab("D:/tasks/reopen-race", "Reopen race");
        });
        expect(result.current.tabState.tabs.some(t => t.projectPath === "D:/tasks/reopen-race")).toBe(true);
        expect(CreateProjectTabSession).toHaveBeenCalledTimes(1);

        await act(async () => {
            resolveClose();
            await Promise.resolve();
            await Promise.resolve();
        });

        await waitFor(() => expect(CreateProjectTabSession).toHaveBeenCalledTimes(2));
        expect(vi.mocked(CloseProjectTabSession).mock.invocationCallOrder[0]).toBeLessThan(
            vi.mocked(CreateProjectTabSession).mock.invocationCallOrder[1],
        );
    });
});
