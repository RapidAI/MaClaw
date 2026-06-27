import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { useAITabManager } from '../useAITabManager';
import { usePendingAssistantTabOpen } from '../usePendingAssistantTabOpen';
import type { PendingHistoryDiscussionOpen } from '../usePendingAssistantTabOpen';
import { createInitialTabState, DEFAULT_MAX_VE_TABS, LOCAL_TAB } from '../AITabTypes';
import type { AITab } from '../AITabTypes';

type PendingHistoryDiscussion = PendingHistoryDiscussionOpen | null;

const runtimeEvents = vi.hoisted(() => ({
    handlers: new Map<string, (data: any) => void>(),
}));

vi.mock('../../../../wailsjs/runtime', () => ({
    EventsOn: vi.fn((event: string, handler: (data: any) => void) => {
        runtimeEvents.handlers.set(event, handler);
        return () => runtimeEvents.handlers.delete(event);
    }),
    EventsOff: vi.fn(),
}));

vi.mock('../../../../wailsjs/go/main/App', () => ({
    LoadProjectTabIndex: vi.fn().mockResolvedValue([]),
    CloseProjectTabSession: vi.fn().mockResolvedValue(undefined),
    CreateProjectTabSession: vi.fn().mockResolvedValue(undefined),
    SaveProjectTabConversation: vi.fn().mockResolvedValue(undefined),
    LoadProjectTabConversation: vi.fn().mockResolvedValue(null),
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
    it('opens pending digital employees by machine id when available', async () => {
        const createVETab = vi.fn().mockReturnValue(null);
        const onHandled = vi.fn();

        renderHook(() => usePendingAssistantTabOpen({
            createVETab,
            createGroupTab: vi.fn(),
            createProjectTab: vi.fn(),
            pendingVEOpen: {
                id: 'profile-ve',
                machine_id: 'machine-ve',
                name: 'Machine VE',
                avatar_data_url: 'data:image/jpeg;base64,/9j/',
                skill_description: '',
                access_policy: 'public',
                status: 'active',
                online_status: 'online',
            },
            onPendingVEOpenHandled: onHandled,
        }));

        await waitFor(() => expect(onHandled).toHaveBeenCalledTimes(1));
        expect(createVETab).toHaveBeenCalledWith('machine-ve', 'Machine VE', undefined, 'online', 'data:image/jpeg;base64,/9j/', '');
    });

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

        it('stores safe avatars on VE tabs', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-123", "Avatar Agent", undefined, "online", "data:image/jpeg;base64,/9j/");
            });

            expect((tab as AITab | null)?.avatarDataURL).toBe("data:image/jpeg;base64,/9j/");
        });

        it('stores VE skill descriptions on tabs for local intro copy', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-123", "Avatar Agent", undefined, "online", undefined, "contract review");
            });

            expect((tab as AITab | null)?.veSkillDescription).toBe("contract review");
        });

        it('updates an existing live VE tab when only the skill description changes', () => {
            const { result } = renderHook(() => useAITabManager());

            let original: AITab | null = null;
            let reopened: AITab | null = null;
            act(() => {
                original = result.current.createVETab("ve-123", "Avatar Agent");
            });
            act(() => {
                reopened = result.current.createVETab("ve-123", "Avatar Agent", undefined, undefined, undefined, "contract review");
            });

            expect((original as AITab | null)?.id).toBe((reopened as AITab | null)?.id);
            expect((reopened as AITab | null)?.veSkillDescription).toBe("contract review");
            expect(result.current.tabState.tabs.find(tab => tab.id === reopened?.id)?.veSkillDescription).toBe("contract review");
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

        it('does not treat computed participant titles as explicit group names', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createGroupTab("group-1", "Agent A, Agent B", ["ve-a", "ve-b"], { discussionId: "disc-1" });
            });

            expect(result.current.activeTab.groupTitle).toBeUndefined();
        });

        it('renames writable group tabs and preserves the custom name on metadata refresh', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createGroupTab("group-1", "Agent A", ["ve-a"], { discussionId: "disc-1", participantNames: { "ve-a": "Agent A" } });
            });
            act(() => {
                result.current.renameGroupTab("group-1", "My group");
            });
            act(() => {
                result.current.createGroupTab("group-1", "Agent A, Agent B", ["ve-a", "ve-b"], { discussionId: "disc-1", participantNames: { "ve-b": "Agent B" } });
            });

            expect(result.current.activeTab.title).toBe("Agent A, Agent B");
            expect(result.current.activeTab.groupTitle).toBe("My group");
            expect(result.current.activeTab.participants).toEqual(["ve-a", "ve-b"]);
            expect(result.current.activeTab.participantNames).toEqual({ "ve-a": "Agent A", "ve-b": "Agent B" });
        });

        it('updates group names from pushed discussion rename events', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createGroupTab("group-1", "Agent A", ["ve-a"], { discussionId: "disc-1", participantNames: { "ve-a": "Agent A" } });
            });
            act(() => {
                runtimeEvents.handlers.get("ve:discussion_rename")?.({ type: "ve:discussion_rename", payload: { discussion_id: "disc-1", topic: "Remote group" } });
            });

            expect(result.current.activeTab.title).toBe("Agent A");
            expect(result.current.activeTab.groupTitle).toBe("Remote group");
        });

        it('updates group names from wrapped rename event payloads', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createGroupTab("group-1", "Agent A", ["ve-a"], { discussionId: "disc-1" });
            });
            act(() => {
                runtimeEvents.handlers.get("ve-event")?.({ type: "ve:discussion_rename", payload: { type: "ve:discussion_rename", payload: { discussionId: "disc-1", title: "Wrapped group" } } });
            });

            expect(result.current.activeTab.groupTitle).toBe("Wrapped group");
        });

        it('does not rename read-only group tabs', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createGroupTab("history-1", "History", ["ve-a"], { readOnly: true });
            });
            let renamed: AITab | null = null;
            act(() => {
                renamed = result.current.renameGroupTab("history-1", "New name");
            });

            expect(renamed).toBeNull();
            expect(result.current.activeTab.title).toBe("History");
            expect(result.current.activeTab.groupTitle).toBeUndefined();
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

        it('deduplicates VE tabs by normalized participant identity', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab1: AITab | null = null;
            let tab2: AITab | null = null;
            act(() => {
                tab1 = result.current.createVETab(" VE-A ", "Agent A");
            });
            act(() => {
                tab2 = result.current.createVETab("ve-a", "Agent A", "session-a", "offline");
            });

            expect(tab1!.id).toBe(tab2!.id);
            expect(result.current.tabState.tabs).toHaveLength(2);
            expect(result.current.activeTab.id).toBe(tab1!.id);
            expect(result.current.getTabState(tab1!.id)?.sessionId).toBe("session-a");
        });

        it('deduplicates VE tabs across generated participant aliases', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab1: AITab | null = null;
            let tab2: AITab | null = null;
            act(() => {
                tab1 = result.current.createVETab("machine-a", "Agent A");
            });
            act(() => {
                tab2 = result.current.createVETab("ve-machine-a", "Agent A", "session-a");
            });

            expect(tab1!.id).toBe(tab2!.id);
            expect(result.current.tabState.tabs).toHaveLength(2);
            expect(result.current.getTabState(tab1!.id)?.sessionId).toBe("session-a");
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

        it('refreshes avatars on upgraded VE group tabs', () => {
            const { result } = renderHook(() => useAITabManager());

            let tabId = "";
            act(() => {
                const tab = result.current.createVETab("ve-1", "Agent A");
                tabId = tab!.id;
                result.current.upgradeVETabToGroup(tabId, ["ve-1", "local-maclaw"]);
            });
            act(() => {
                result.current.createVETab("ve-1", "Agent A", "session-group", "online", "data:image/jpeg;base64,/9j/");
            });

            expect(result.current.tabState.tabs.find(t => t.id === tabId)?.avatarDataURL).toBe("data:image/jpeg;base64,/9j/");
        });

        it('keeps live group tab availability tied to the primary VE, not secondary participants', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                const tab = result.current.createVETab("machine-a", "Agent A");
                result.current.upgradeVETabToGroup(tab!.id, ["machine-a", "ve-machine-b"]);
            });
            act(() => {
                runtimeEvents.handlers.get("ve:status_change")?.({ payload: { employee: { machine_id: "machine-b", online_status: "offline" } } });
            });

            const groupTab = result.current.tabState.tabs.find(t => t.id === "ve-machine-a");
            expect(groupTab?.onlineStatus).toBe("online");

            act(() => {
                runtimeEvents.handlers.get("ve:status_change")?.({ payload: { employee: { machine_id: "machine-a", online_status: "offline" } } });
            });

            expect(result.current.tabState.tabs.find(t => t.id === "ve-machine-a")?.onlineStatus).toBe("offline");
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

        it('promotes an existing single-participant history tab when reopening the same VE', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createGroupTab("history-disc-1", "Agent A history", ["me", "ve-a"], { discussionId: "disc-1", readOnly: true });
                result.current.activateTab("local");
            });

            let reopened: AITab | null = null;
            act(() => {
                reopened = result.current.createVETab("ve-a", "Agent A", "disc-1");
            });

            expect(reopened!.id).toBe("ve-ve-a");
            expect(result.current.activeTab.id).toBe("ve-ve-a");
            expect(reopened!.type).toBe("group");
            expect(reopened!.veId).toBe("ve-a");
            expect(reopened!.title).toBe("Agent A");
            expect(reopened!.readOnly).toBe(false);
            expect(result.current.tabState.tabs.filter(t => t.veId === "ve-a")).toHaveLength(1);
            expect(result.current.tabState.tabs).toHaveLength(2);
            expect(result.current.getTabState("history-disc-1")).toBeUndefined();
            expect(result.current.getTabState("ve-ve-a")?.sessionId).toBe("disc-1");

            act(() => {
                result.current.saveTabState("ve-ve-a", { history: [{ role: "assistant", content: "saved" }] });
                result.current.closeTab("ve-ve-a");
            });
            act(() => {
                reopened = result.current.createVETab("ve-a", "Agent A");
            });

            expect(reopened!.id).toBe("ve-ve-a");
            expect(result.current.getTabState("ve-ve-a")?.history).toEqual([{ role: "assistant", content: "saved" }]);
        });

        it('promotes an existing single-participant history tab across generated aliases', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createGroupTab("history-disc-alias", "Agent A history", ["me", "machine-a"], { discussionId: "disc-alias", readOnly: true });
                result.current.activateTab("local");
            });

            let reopened: AITab | null = null;
            act(() => {
                reopened = result.current.createVETab("ve-machine-a", "Agent A", "disc-alias");
            });

            expect(reopened!.type).toBe("group");
            expect(reopened!.veId).toBe("ve-machine-a");
            expect(result.current.tabState.tabs.some(t => t.id === "history-disc-alias")).toBe(false);
            expect(result.current.tabState.tabs).toHaveLength(2);
        });

        it('promotes single-participant history tabs with duplicate participant aliases', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createGroupTab("history-disc-alias-dup", "Agent A history", ["me", "machine-a", "ve-machine-a"], { discussionId: "disc-alias-dup", readOnly: true });
                result.current.activateTab("local");
            });

            let reopened: AITab | null = null;
            act(() => {
                reopened = result.current.createVETab("ve-machine-a", "Agent A", "disc-alias-dup");
            });

            expect(reopened!.type).toBe("group");
            expect(reopened!.veId).toBe("ve-machine-a");
            expect(result.current.tabState.tabs.some(t => t.id === "history-disc-alias-dup")).toBe(false);
            expect(result.current.tabState.tabs).toHaveLength(2);
        });

        it('collapses stale duplicate VE/history identity tabs when reopening the VE', () => {
            const { result } = renderHook(() => useAITabManager());

            let direct: AITab | null = null;
            act(() => {
                direct = result.current.createVETab("ve-a", "Agent A");
            });
            act(() => {
                result.current.saveTabState(direct!.id, { history: [{ role: "assistant", content: "live" }] });
                result.current.createGroupTab("history-disc-1", "Agent A history", ["me", "ve-a"], { discussionId: "disc-1", readOnly: true });
            });
            act(() => {
                result.current.saveTabState("history-disc-1", { sessionId: "disc-1", history: [{ role: "assistant", content: "history" }] });
            });

            act(() => {
                result.current.createVETab("ve-a", "Agent A");
            });

            expect(result.current.activeTab.id).toBe(direct!.id);
            expect(result.current.tabState.tabs.some(t => t.id === "history-disc-1")).toBe(false);
            expect(result.current.tabState.tabs.filter(t => t.type === "ve" || t.type === "group")).toHaveLength(1);
            expect(result.current.getTabState(direct!.id)?.history).toEqual([{ role: "assistant", content: "live" }]);
            expect(result.current.getTabState(direct!.id)?.sessionId).toBe("disc-1");
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

        it('merges participant display names when refreshing an existing group tab', () => {
            const { result } = renderHook(() => useAITabManager());

            act(() => {
                result.current.createGroupTab("history-disc-names", "Open case", ["me", "ve-a"], {
                    discussionId: "disc-names",
                    participantNames: { me: "Alice" },
                });
            });
            act(() => {
                result.current.createGroupTab("history-disc-names", "Open case", ["me", "ve-a"], {
                    discussionId: "disc-names",
                    participantNames: { "ve-a": "Contract Bot" },
                });
            });

            expect(result.current.activeTab.participantNames).toEqual({ me: "Alice", "ve-a": "Contract Bot" });
        });

        it('uses a readable fallback title when history discussion has only a raw id', async () => {
            const onHandled = vi.fn();
            const { result, rerender } = renderHook<ReturnType<typeof useAITabManager>, { pending: PendingHistoryDiscussion }>(
                ({ pending }: { pending: PendingHistoryDiscussion }) => {
                    const manager = useAITabManager();
                    usePendingAssistantTabOpen({
                        lang: "zh-Hans",
                        createVETab: manager.createVETab,
                        createGroupTab: manager.createGroupTab,
                        createProjectTab: manager.createProjectTab,
                        activateTab: manager.activateTab,
                        getTabState: manager.getTabState,
                        saveTabState: manager.saveTabState,
                        getTabList: manager.getTabs,
                        pendingHistoryDiscussionOpen: pending,
                        onPendingHistoryDiscussionOpenHandled: onHandled,
                    });
                    return manager;
                },
                { initialProps: { pending: null } }
            );

            rerender({
                pending: {
                    id: "disc-raw-123",
                    status: "open",
                    participant_ids: ["m_b1821505498d817c"],
                },
            });

            await waitFor(() => expect(onHandled).toHaveBeenCalledTimes(1));
            expect(result.current.activeTab.title).toBe("\u7fa4\u7ec4\u8ba8\u8bba");
            expect(result.current.activeTab.title).not.toContain("disc-raw-123");
        });

        it('activates an existing direct VE tab by participant when history opens before session id is saved', async () => {
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
                        saveTabState: manager.saveTabState,
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
                veTab = result.current.createVETab("ve-a", "Agent A");
                result.current.activateTab("local");
            });

            rerender({
                pending: {
                    id: "disc-early",
                    topic: "Vendor audit",
                    local_relation: "owned_ve_invited",
                    readonly: true,
                    status: "open",
                    participant_ids: ["me", "ve-a"],
                },
            });

            await waitFor(() => expect(onHandled).toHaveBeenCalledTimes(1));
            expect(result.current.activeTab.id).toBe(veTab!.id);
            expect(result.current.tabState.tabs.filter(t => t.id === "history-disc-early")).toHaveLength(0);
            expect(result.current.tabState.tabs).toHaveLength(2);
            expect(result.current.getTabState(veTab!.id)?.sessionId).toBe("disc-early");
            expect(result.current.getTabState(veTab!.id)?.discussionId).toBe("disc-early");
        });

        it('activates an existing direct VE tab by generated participant alias', async () => {
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
                        saveTabState: manager.saveTabState,
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
                veTab = result.current.createVETab("machine-a", "Agent A");
                result.current.activateTab("local");
            });

            rerender({
                pending: {
                    id: "disc-alias-early",
                    topic: "Vendor audit",
                    local_relation: "owned_ve_invited",
                    readonly: true,
                    status: "open",
                    participant_ids: ["me", "machine-a", "ve-machine-a"],
                },
            });

            await waitFor(() => expect(onHandled).toHaveBeenCalledTimes(1));
            expect(result.current.activeTab.id).toBe(veTab!.id);
            expect(result.current.tabState.tabs.filter(t => t.id === "history-disc-alias-early")).toHaveLength(0);
            expect(result.current.getTabState(veTab!.id)?.sessionId).toBe("disc-alias-early");
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

        it('keeps a VE session alive when closing a tab so it can be resumed', async () => {
            const onCloseVESession = vi.fn().mockResolvedValue(undefined);
            const { result } = renderHook(() => useAITabManager({ onCloseVESession }));

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-1", "Assistant", "session-abc");
                result.current.saveTabState(tab!.id, {
                    history: [{ id: "m1", role: "user", content: "hello" }],
                    scrollTop: 0,
                    inputText: "draft",
                    sessionId: "session-abc",
                });
            });

            act(() => {
                result.current.closeTab(tab!.id);
            });

            expect(onCloseVESession).not.toHaveBeenCalled();

            let reopened: AITab | null = null;
            act(() => {
                reopened = result.current.createVETab("ve-1", "Assistant");
            });

            expect(reopened!.id).toBe(tab!.id);
            expect(result.current.getTabState(reopened!.id)?.sessionId).toBe("session-abc");
            expect(result.current.getTabState(reopened!.id)?.history).toHaveLength(1);
        });

        it('explicit clear resets cached VE state without closing the tab', async () => {
            const onCloseVESession = vi.fn().mockResolvedValue(undefined);
            const { result } = renderHook(() => useAITabManager({ onCloseVESession }));

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-1", "Assistant", "session-abc");
                result.current.saveTabState(tab!.id, {
                    history: [{ id: "m1", role: "user", content: "hello" }],
                    scrollTop: 10,
                    inputText: "draft",
                    sessionId: "session-abc",
                });
                result.current.clearTabConversation(tab!.id);
            });

            expect(result.current.tabState.tabs.some(t => t.id === tab!.id)).toBe(true);
            expect(result.current.tabState.tabs.find(t => t.id === tab!.id)?.conversationResetSeq).toBe(1);
            expect(result.current.getTabState(tab!.id)?.history).toEqual([]);
            expect(result.current.getTabState(tab!.id)?.sessionId).toBeUndefined();
            expect(onCloseVESession).not.toHaveBeenCalled();
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

        it('counts upgraded live group tabs toward the VE tab limit', () => {
            const { result } = renderHook(() => useAITabManager({ maxVETabs: 1 }));

            let tab2: AITab | null = null;
            act(() => {
                const tab1 = result.current.createVETab("ve-1", "Agent 1");
                result.current.upgradeVETabToGroup(tab1!.id, ["ve-1", "local-maclaw"]);
                tab2 = result.current.createVETab("ve-2", "Agent 2");
            });

            expect(tab2).toBeNull();
            expect(result.current.tabState.tabs).toHaveLength(2);
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

        it('keeps VE tab state when tab is closed', () => {
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

            expect(result.current.getTabState(tab!.id)?.inputText).toBe("test");
        });

        it('accepts late state saves after a closed VE tab unmounts for resume', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createVETab("ve-late", "Late Agent");
            });
            act(() => {
                result.current.closeTab(tab!.id);
            });
            act(() => {
                result.current.saveTabState(tab!.id, {
                    history: [{ role: "user", content: "late snapshot" }],
                    scrollTop: 10,
                    inputText: "late",
                });
            });

            expect(result.current.getTabState(tab!.id)?.inputText).toBe("late");
        });

        it('keeps allowing cached project tab state updates after close', () => {
            const { result } = renderHook(() => useAITabManager());

            let tab: AITab | null = null;
            act(() => {
                tab = result.current.createProjectTab("D:\\work\\sample", "Sample project");
            });
            act(() => {
                result.current.saveTabState(tab!.id, {
                    history: [{ role: "user", content: "before close" }],
                    scrollTop: 20,
                    inputText: "before",
                });
            });
            act(() => {
                result.current.closeTab(tab!.id);
            });
            act(() => {
                result.current.saveTabState(tab!.id, {
                    inputText: "after close",
                });
            });

            expect(result.current.getTabState(tab!.id)?.inputText).toBe("after close");
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
