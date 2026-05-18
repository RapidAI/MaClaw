import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { useAITabManager } from '../useAITabManager';
import { usePendingAssistantTabOpen } from '../usePendingAssistantTabOpen';
import type { PendingHistoryDiscussionOpen } from '../usePendingAssistantTabOpen';
import { createInitialTabState, DEFAULT_MAX_VE_TABS, LOCAL_TAB } from '../AITabTypes';
import type { AITab } from '../AITabTypes';

type PendingHistoryDiscussion = PendingHistoryDiscussionOpen | null;

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn(() => () => {}),
    EventsOff: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    LoadProjectTabIndex: vi.fn().mockResolvedValue([]),
    CloseProjectTabSession: vi.fn().mockResolvedValue(undefined),
    CreateProjectTabSession: vi.fn().mockResolvedValue(undefined),
}));

describe('AITabTypes', () => {
    it('createInitialTabState returns correct default state', () => {
        const state = createInitialTabState();
        expect(state.tabs).toHaveLength(1);
        expect(state.tabs[0]).toEqual(LOCAL_TAB);
        expect(state.activeTabId).toBe("local");
        expect(state.maxVETabs).toBe(DEFAULT_MAX_VE_TABS);
    });

    it('createInitialTabState respects custom maxVETabs', () => {
        const state = createInitialTabState(4);
        expect(state.maxVETabs).toBe(4);
    });

    it('LOCAL_TAB is not closable', () => {
        expect(LOCAL_TAB.closable).toBe(false);
        expect(LOCAL_TAB.type).toBe("local");
        expect(LOCAL_TAB.id).toBe("local");
    });
});

describe('useAITabManager', () => {
    describe('initial state', () => {
        it('starts with only the local tab active', () => {
            const { result } = renderHook(() => useAITabManager());
            expect(result.current.tabState.tabs).toHaveLength(1);
            expect(result.current.activeTab.id).toBe("local");
            expect(result.current.tabLimitError).toBeNull();
        });
    });

    describe('tab creation', () => {
        it('creates a VE tab and activates it', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-123", "翻译助手");
            });

            expect(tab).not.toBeNull();
            expect(tab!.type).toBe("ve");
            expect(tab!.veId).toBe("ve-123");
            expect(tab!.title).toBe("翻译助手");
            expect(tab!.closable).toBe(true);
            expect(result.current.tabState.tabs).toHaveLength(2);
            expect(result.current.activeTab.id).toBe(tab!.id);
        });

        it('creates a group tab', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createGroupTab("group-1", "Tech discussion", ["ve-1", "ve-2"]);
            });

            expect(tab).not.toBeNull();
            expect(tab!.type).toBe("group");
            expect(tab!.participants).toEqual(["ve-1", "ve-2"]);
            expect(result.current.tabState.tabs).toHaveLength(2);
        });
    });

    describe('duplicate tab detection', () => {
        it('activates existing tab instead of creating duplicate for same veId', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab1: AITab | null = null;
            let tab2: AITab | null = null;
            act(() => {
                tab1 = result.current.createVETab("ve-123", "翻译助手");
            });
            act(() => {
                tab2 = result.current.createVETab("ve-123", "翻译助手");
            });

            expect(tab1!.id).toBe(tab2!.id);
            expect(result.current.tabState.tabs).toHaveLength(2); // local + 1 VE tab
        });

        it('allows different veIds to create separate tabs', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createVETab("ve-1", "助手A");
            });
            act(() => {
                result.current.createVETab("ve-2", "助手B");
            });

            expect(result.current.tabState.tabs).toHaveLength(3); // local + 2 digital employee tabs
        });

        it('updates saved session id when reopening an existing VE tab', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-1", "Agent A");
            });
            act(() => {
                result.current.createVETab("ve-1", "Agent A", "session-next", "offline");
            });

            expect(result.current.tabState.tabs).toHaveLength(2);
            expect(result.current.activeTab.id).toBe(tab!.id);
            expect(result.current.getTabState(tab!.id)?.sessionId).toBe("session-next");
            expect(result.current.tabState.tabs.find(t => t.id === tab!.id)?.onlineStatus).toBe("offline");
        });

        it('activates upgraded live group tab when reopening the same VE', () => {
            const { result } = renderHook(() => useAITabManager());

            let tabId = "";
            act(() => {
                const tab = result.current.createVETab("ve-1", "Agent A");
                tabId = tab!.id;
                result.current.upgradeVETabToGroup(tabId, ["ve-1", "local-maclaw"]);
                result.current.activateTab("local");
            });
            act(() => {
                result.current.createVETab("ve-1", "Agent A", "session-group");
            });

            expect(result.current.tabState.tabs).toHaveLength(2);
            expect(result.current.activeTab.id).toBe(tabId);
            expect(result.current.activeTab.type).toBe("group");
            expect(result.current.getTabState(tabId)?.sessionId).toBe("session-group");
        });

        it('refreshes metadata when reopening an existing group tab', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createGroupTab("history-disc-1", "Open case", ["me"], { discussionId: "disc-1", readOnly: false, role: "initiated_by_me" });
            });
            act(() => {
                result.current.createGroupTab("history-disc-1", "Closed case", ["me", "ve-1"], { discussionId: "disc-1", readOnly: true, role: "initiated_by_me" });
            });

            expect(result.current.tabState.tabs).toHaveLength(2);
            const tab = result.current.activeTab;
            expect(tab.title).toBe("Closed case");
            expect(tab.readOnly).toBe(true);
            expect(tab.participants).toEqual(["me", "ve-1"]);
        });

        it('activates an existing VE session tab when opening the same history discussion', async () => {
            const onHandled = vi.fn();
            const { result, rerender } = renderHook<ReturnType<typeof useAITabManager>, { pending: PendingHistoryDiscussion }>(
                ({ pending }: { pending: PendingHistoryDiscussion }) => {
                    const manager = useAITabManager();
                    usePendingAssistantTabOpen({
                        createVETab: manager.createVETab,
                        createGroupTab: manager.createGroupTab,
                        createProjectTab: manager.createProjectTab,
                        activateTab: manager.activateTab,
                        getTabState: manager.getTabState,
                        getTabList: manager.getTabs,
                        pendingHistoryDiscussionOpen: pending,
                        onPendingHistoryDiscussionOpenHandled: onHandled,
                    });
                    return manager;
                },
                { initialProps: { pending: null } }
            );

            let veTab: AITab | null = null;
            act(() => {
                veTab = result.current.createVETab("ve-a", "Agent A", "disc-1");
                result.current.activateTab("local");
            });

            rerender({
                pending: {
                    id: "disc-1",
                    topic: "Vendor audit",
                    local_relation: "owned_ve_invited",
                    readonly: true,
                    status: "open",
                    participant_ids: ["ve-a"],
                },
            });

            await waitFor(() => expect(onHandled).toHaveBeenCalledTimes(1));
            expect(result.current.activeTab.id).toBe(veTab!.id);
            expect(result.current.tabState.tabs.filter(t => t.id === "history-disc-1")).toHaveLength(0);
            expect(result.current.tabState.tabs).toHaveLength(2);
        });

        it('activates an existing live group tab by saved session id', async () => {
            const onHandled = vi.fn();
            const { result, rerender } = renderHook<ReturnType<typeof useAITabManager>, { pending: PendingHistoryDiscussion }>(
                ({ pending }: { pending: PendingHistoryDiscussion }) => {
                    const manager = useAITabManager();
                    usePendingAssistantTabOpen({
                        createVETab: manager.createVETab,
                        createGroupTab: manager.createGroupTab,
                        createProjectTab: manager.createProjectTab,
                        activateTab: manager.activateTab,
                        getTabState: manager.getTabState,
                        getTabList: manager.getTabs,
                        pendingHistoryDiscussionOpen: pending,
                        onPendingHistoryDiscussionOpenHandled: onHandled,
                    });
                    return manager;
                },
                { initialProps: { pending: null } }
            );

            let liveGroupTabId = "";
            act(() => {
                const veTab = result.current.createVETab("ve-a", "Agent A");
                liveGroupTabId = veTab!.id;
                result.current.upgradeVETabToGroup(liveGroupTabId, ["ve-a", "local-maclaw"]);
                result.current.saveTabState(liveGroupTabId, { sessionId: "disc-live" });
                result.current.activateTab("local");
            });

            rerender({
                pending: {
                    id: "disc-live",
                    topic: "Live group",
                    local_relation: "owned_ve_invited",
                    readonly: false,
                    status: "open",
                    participant_ids: ["ve-a", "local-maclaw"],
                },
            });

            await waitFor(() => expect(onHandled).toHaveBeenCalledTimes(1));
            expect(result.current.activeTab.id).toBe(liveGroupTabId);
            expect(result.current.tabState.tabs.filter(t => t.id === "history-disc-live")).toHaveLength(0);
        });

        it('activates an existing group tab by saved discussion id state', async () => {
            const onHandled = vi.fn();
            const { result, rerender } = renderHook<ReturnType<typeof useAITabManager>, { pending: PendingHistoryDiscussion }>(
                ({ pending }: { pending: PendingHistoryDiscussion }) => {
                    const manager = useAITabManager();
                    usePendingAssistantTabOpen({
                        createVETab: manager.createVETab,
                        createGroupTab: manager.createGroupTab,
                        createProjectTab: manager.createProjectTab,
                        activateTab: manager.activateTab,
                        getTabState: manager.getTabState,
                        getTabList: manager.getTabs,
                        pendingHistoryDiscussionOpen: pending,
                        onPendingHistoryDiscussionOpenHandled: onHandled,
                    });
                    return manager;
                },
                { initialProps: { pending: null } }
            );

            act(() => {
                result.current.createGroupTab("group-custom", "Custom group", ["ve-a"], { readOnly: true });
                result.current.saveTabState("group-custom", { discussionId: "disc-state" });
                result.current.activateTab("local");
            });

            rerender({
                pending: {
                    id: "disc-state",
                    topic: "State-backed discussion",
                    local_relation: "owned_ve_invited",
                    readonly: true,
                    status: "open",
                    participant_ids: ["ve-a"],
                },
            });

            await waitFor(() => expect(onHandled).toHaveBeenCalledTimes(1));
            expect(result.current.activeTab.id).toBe("group-custom");
            expect(result.current.tabState.tabs.filter(t => t.id === "history-disc-state")).toHaveLength(0);
        });

        it('activates an existing group history tab when opening the same discussion again', async () => {
            const onHandled = vi.fn();
            const { result, rerender } = renderHook<ReturnType<typeof useAITabManager>, { pending: PendingHistoryDiscussion }>(
                ({ pending }: { pending: PendingHistoryDiscussion }) => {
                    const manager = useAITabManager();
                    usePendingAssistantTabOpen({
                        createVETab: manager.createVETab,
                        createGroupTab: manager.createGroupTab,
                        createProjectTab: manager.createProjectTab,
                        activateTab: manager.activateTab,
                        getTabState: manager.getTabState,
                        getTabList: manager.getTabs,
                        pendingHistoryDiscussionOpen: pending,
                        onPendingHistoryDiscussionOpenHandled: onHandled,
                    });
                    return manager;
                },
                { initialProps: { pending: null } }
            );

            act(() => {
                result.current.createGroupTab("history-disc-2", "Earlier title", ["ve-a"], { discussionId: "disc-2", readOnly: true, role: "owned_ve_invited" });
                result.current.activateTab("local");
            });

            rerender({
                pending: {
                    id: "disc-2",
                    topic: "Vendor audit",
                    local_relation: "owned_ve_invited",
                    readonly: true,
                    status: "open",
                    participant_ids: ["ve-a"],
                },
            });

            await waitFor(() => expect(onHandled).toHaveBeenCalledTimes(1));
            expect(result.current.activeTab.id).toBe("history-disc-2");
            expect(result.current.tabState.tabs.filter(t => t.discussionId === "disc-2")).toHaveLength(1);
        });

        it('dedupes group history tabs even when tab state lookup is unavailable', async () => {
            const onHandled = vi.fn();
            const { result, rerender } = renderHook<ReturnType<typeof useAITabManager>, { pending: PendingHistoryDiscussion }>(
                ({ pending }: { pending: PendingHistoryDiscussion }) => {
                    const manager = useAITabManager();
                    usePendingAssistantTabOpen({
                        createVETab: manager.createVETab,
                        createGroupTab: manager.createGroupTab,
                        createProjectTab: manager.createProjectTab,
                        activateTab: manager.activateTab,
                        getTabList: manager.getTabs,
                        pendingHistoryDiscussionOpen: pending,
                        onPendingHistoryDiscussionOpenHandled: onHandled,
                    });
                    return manager;
                },
                { initialProps: { pending: null } }
            );

            act(() => {
                result.current.createGroupTab("history-disc-3", "Earlier title", ["ve-a"], { discussionId: "disc-3", readOnly: true, role: "owned_ve_invited" });
                result.current.activateTab("local");
            });

            rerender({
                pending: {
                    id: "disc-3",
                    topic: "Vendor audit",
                    local_relation: "owned_ve_invited",
                    readonly: true,
                    status: "open",
                    participant_ids: ["ve-a"],
                },
            });

            await waitFor(() => expect(onHandled).toHaveBeenCalledTimes(1));
            expect(result.current.activeTab.id).toBe("history-disc-3");
            expect(result.current.tabState.tabs.filter(t => t.discussionId === "disc-3")).toHaveLength(1);
        });
    });

    describe('tab switching', () => {
        it('switches active tab', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-1", "助手");
            });
            act(() => {
                result.current.activateTab("local");
            });

            expect(result.current.activeTab.id).toBe("local");

            act(() => {
                result.current.activateTab(tab!.id);
            });

            expect(result.current.activeTab.id).toBe(tab!.id);
        });

        it('ignores activation of non-existent tab', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.activateTab("non-existent");
            });

            expect(result.current.activeTab.id).toBe("local");
        });
    });

    describe('tab close', () => {
        it('closes a VE tab and falls back to local', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-1", "助手");
            });

            expect(result.current.activeTab.id).toBe(tab!.id);

            act(() => {
                result.current.closeTab(tab!.id);
            });

            expect(result.current.tabState.tabs).toHaveLength(1);
            expect(result.current.activeTab.id).toBe("local");
        });

        it('cannot close the local tab', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.closeTab("local");
            });

            expect(result.current.tabState.tabs).toHaveLength(1);
            expect(result.current.activeTab.id).toBe("local");
        });

        it('calls onCloseVESession when closing a tab with sessionId', async () => {
            const onCloseVESession = vi.fn().mockResolvedValue(undefined);
            const { result } = renderHook(() => useAITabManager({ onCloseVESession }));

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-1", "助手", "session-abc");
            });

            act(() => {
                result.current.closeTab(tab!.id);
            });

            expect(onCloseVESession).toHaveBeenCalledWith("session-abc");
        });

        it('does not affect other tabs when closing one', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab1: AITab | null = null;
            let tab2: AITab | null = null;
            act(() => {
                tab1 = result.current.createVETab("ve-1", "助手A");
            });
            act(() => {
                tab2 = result.current.createVETab("ve-2", "助手B");
            });

            // Activate tab1, then close tab2
            act(() => {
                result.current.activateTab(tab1!.id);
            });
            act(() => {
                result.current.closeTab(tab2!.id);
            });

            expect(result.current.tabState.tabs).toHaveLength(2); // local + tab1
            expect(result.current.activeTab.id).toBe(tab1!.id);
        });
    });

    describe('max tab limit', () => {
        it('enforces max VE tab limit', () => {
            const { result } = renderHook(() => useAITabManager({ maxVETabs: 2 }));

            act(() => {
                result.current.createVETab("ve-1", "助手1");
            });
            act(() => {
                result.current.createVETab("ve-2", "助手2");
            });

            let tab3: AITab | null = null;
            act(() => {
                tab3 = result.current.createVETab("ve-3", "助手3");
            });

            expect(tab3).toBeNull();
            expect(result.current.tabState.tabs).toHaveLength(3); // local + 2 VE
            expect(result.current.tabLimitError).not.toBeNull();
        });

        it('allows creating after closing a tab', () => {
            const { result } = renderHook(() => useAITabManager({ maxVETabs: 2 }));

            let tab1: AITab | null = null;
            act(() => {
                tab1 = result.current.createVETab("ve-1", "助手1");
            });
            act(() => {
                result.current.createVETab("ve-2", "助手2");
            });

            // Close tab1
            act(() => {
                result.current.closeTab(tab1!.id);
            });

            // Now should be able to create a new one
            let tab3: AITab | null = null;
            act(() => {
                tab3 = result.current.createVETab("ve-3", "助手3");
            });

            expect(tab3).not.toBeNull();
            expect(result.current.tabState.tabs).toHaveLength(3); // local + ve-2 + ve-3
        });

        it('clearTabLimitError clears the error', () => {
            const { result } = renderHook(() => useAITabManager({ maxVETabs: 1 }));

            act(() => {
                result.current.createVETab("ve-1", "助手1");
            });
            act(() => {
                result.current.createVETab("ve-2", "助手2"); // exceeds limit
            });

            expect(result.current.tabLimitError).not.toBeNull();

            act(() => {
                result.current.clearTabLimitError();
            });

            expect(result.current.tabLimitError).toBeNull();
        });
    });

    describe('state isolation', () => {
        it('saves and retrieves tab state independently', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab1: AITab | null = null;
            let tab2: AITab | null = null;
            act(() => {
                tab1 = result.current.createVETab("ve-1", "助手A");
            });
            act(() => {
                tab2 = result.current.createVETab("ve-2", "助手B");
            });

            // Save different states for each tab
            act(() => {
                result.current.saveTabState(tab1!.id, {
                    history: [{ role: "user", content: "hello from tab1" }],
                    scrollTop: 100,
                    inputText: "draft1",
                });
                result.current.saveTabState(tab2!.id, {
                    history: [{ role: "user", content: "hello from tab2" }],
                    scrollTop: 200,
                    inputText: "draft2",
                });
            });

            const state1 = result.current.getTabState(tab1!.id);
            const state2 = result.current.getTabState(tab2!.id);

            expect(state1?.inputText).toBe("draft1");
            expect(state1?.scrollTop).toBe(100);
            expect(state2?.inputText).toBe("draft2");
            expect(state2?.scrollTop).toBe(200);
        });

        it('returns undefined for non-existent tab state', () => {
            const { result } = renderHook(() => useAITabManager());
            expect(result.current.getTabState("non-existent")).toBeUndefined();
        });

        it('removes tab state when tab is closed', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-1", "助手");
            });
            act(() => {
                result.current.saveTabState(tab!.id, {
                    history: [],
                    scrollTop: 50,
                    inputText: "test",
                });
            });

            expect(result.current.getTabState(tab!.id)).toBeDefined();

            act(() => {
                result.current.closeTab(tab!.id);
            });

            expect(result.current.getTabState(tab!.id)).toBeUndefined();
        });
    });

    describe('default max digital employee tabs is 8', () => {
        it('allows up to 8 digital employee tabs by default', () => {
            const { result } = renderHook(() => useAITabManager());

            for (let i = 1; i <= 8; i++) {
                act(() => {
                    result.current.createVETab(`ve-${i}`, `助手${i}`);
                });
            }

            expect(result.current.tabState.tabs).toHaveLength(9); // local + 8 digital employee tabs

            let tab9: AITab | null = null;
            act(() => {
                tab9 = result.current.createVETab("ve-9", "助手9");
            });

            expect(tab9).toBeNull();
            expect(result.current.tabLimitError).not.toBeNull();
        });
    });
});
