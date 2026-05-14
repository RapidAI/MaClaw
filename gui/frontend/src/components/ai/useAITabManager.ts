import { useState, useCallback, useRef } from "react";
import type { AITab, AITabState, AIAssistantPanelTabState } from "./AITabTypes";
import { createInitialTabState, DEFAULT_MAX_VE_TABS } from "./AITabTypes";

export interface UseAITabManagerOptions {
    maxVETabs?: number;
    /** Called when a VE tab is closed; should end the A2A session */
    onCloseVESession?: (sessionId: string) => Promise<void>;
}

export interface CreateGroupTabOptions {
    discussionId?: string;
    readOnly?: boolean;
    role?: string;
}

export interface UseAITabManagerResult {
    tabState: AIAssistantPanelTabState;
    activeTab: AITab;
    /** Switch to a tab by ID */
    activateTab: (tabId: string) => void;
    /** Create a new VE conversation tab. Returns the tab or null if limit reached. */
    createVETab: (veId: string, veName: string, sessionId?: string) => AITab | null;
    /** Create a new group chat tab */
    createGroupTab: (id: string, title: string, participants: string[], options?: CreateGroupTabOptions) => AITab | null;
    /** Close a tab by ID */
    closeTab: (tabId: string) => void;
    /** Save state for the current active tab before switching */
    saveTabState: (tabId: string, state: Partial<AITabState>) => void;
    /** Get saved state for a tab */
    getTabState: (tabId: string) => AITabState | undefined;
    /** Error message when max tabs exceeded (cleared after reading) */
    tabLimitError: string | null;
    /** Clear the tab limit error */
    clearTabLimitError: () => void;
}

/**
 * Hook managing the AI Assistant Panel tab system.
 * Handles tab creation, switching, closing, duplicate detection, and max limit.
 */
export function useAITabManager(options: UseAITabManagerOptions = {}): UseAITabManagerResult {
    const { maxVETabs = DEFAULT_MAX_VE_TABS, onCloseVESession } = options;

    // Use a ref to track the current state synchronously for createVETab/createGroupTab
    const tabStateRef = useRef<AIAssistantPanelTabState>(createInitialTabState(maxVETabs));

    const [tabState, setTabState] = useState<AIAssistantPanelTabState>(() =>
        createInitialTabState(maxVETabs)
    );
    const [tabLimitError, setTabLimitError] = useState<string | null>(null);

    // Keep ref in sync with state
    const updateTabState = useCallback((updater: (prev: AIAssistantPanelTabState) => AIAssistantPanelTabState) => {
        setTabState(prev => {
            const next = updater(prev);
            tabStateRef.current = next;
            return next;
        });
    }, []);

    // Store tab states (history, scroll, input) keyed by tab ID
    const tabStatesRef = useRef<Map<string, AITabState>>(new Map());

    const activeTab = tabState.tabs.find(t => t.id === tabState.activeTabId) || tabState.tabs[0];

    const activateTab = useCallback((tabId: string) => {
        updateTabState(prev => {
            if (!prev.tabs.some(t => t.id === tabId)) return prev;
            if (prev.activeTabId === tabId) return prev;
            return { ...prev, activeTabId: tabId };
        });
    }, [updateTabState]);

    const createVETab = useCallback((veId: string, veName: string, sessionId?: string): AITab | null => {
        const prev = tabStateRef.current;

        // Check for duplicate: if a tab with same veId exists, activate it
        const existing = prev.tabs.find(t => t.type === "ve" && t.veId === veId);
        if (existing) {
            updateTabState(() => ({ ...prev, activeTabId: existing.id }));
            return existing;
        }

        // Check max VE tab limit
        const veTabCount = prev.tabs.filter(t => t.type === "ve").length;
        if (veTabCount >= prev.maxVETabs) {
            setTabLimitError(`Digital employee tab limit reached (max ${prev.maxVETabs})`);
            return null;
        }

        // Create new tab
        const newTab: AITab = {
            id: `ve-${veId}-${Date.now()}`,
            type: "ve",
            title: veName,
            veId,
            closable: true,
        };

        // Store session ID in tab state
        if (sessionId) {
            tabStatesRef.current.set(newTab.id, {
                history: [],
                scrollTop: 0,
                inputText: "",
                sessionId,
            });
        }

        updateTabState(() => ({
            ...prev,
            tabs: [...prev.tabs, newTab],
            activeTabId: newTab.id,
        }));

        return newTab;
    }, [updateTabState]);

    const createGroupTab = useCallback((id: string, title: string, participants: string[], options: CreateGroupTabOptions = {}): AITab | null => {
        const prev = tabStateRef.current;

        // Check for duplicate
        const existing = prev.tabs.find(t => t.id === id);
        if (existing) {
            updateTabState(() => ({ ...prev, activeTabId: existing.id }));
            return existing;
        }

        const newTab: AITab = {
            id,
            type: "group",
            title,
            participants,
            discussionId: options.discussionId,
            readOnly: options.readOnly,
            role: options.role,
            closable: true,
        };

        updateTabState(() => ({
            ...prev,
            tabs: [...prev.tabs, newTab],
            activeTabId: newTab.id,
        }));

        return newTab;
    }, [updateTabState]);

    const closeTab = useCallback((tabId: string) => {
        updateTabState(prev => {
            const tab = prev.tabs.find(t => t.id === tabId);
            if (!tab || !tab.closable) return prev;

            // End A2A session if applicable
            const savedState = tabStatesRef.current.get(tabId);
            if (savedState?.sessionId && onCloseVESession) {
                onCloseVESession(savedState.sessionId).catch(() => {});
            }

            // Remove tab state
            tabStatesRef.current.delete(tabId);

            const newTabs = prev.tabs.filter(t => t.id !== tabId);
            const newActiveId = prev.activeTabId === tabId
                ? "local" // Fall back to local tab
                : prev.activeTabId;

            return {
                ...prev,
                tabs: newTabs,
                activeTabId: newActiveId,
            };
        });
    }, [onCloseVESession, updateTabState]);

    const saveTabState = useCallback((tabId: string, state: Partial<AITabState>) => {
        const existing = tabStatesRef.current.get(tabId) || {
            history: [],
            scrollTop: 0,
            inputText: "",
        };
        tabStatesRef.current.set(tabId, { ...existing, ...state });
    }, []);

    const getTabState = useCallback((tabId: string): AITabState | undefined => {
        return tabStatesRef.current.get(tabId);
    }, []);

    const clearTabLimitError = useCallback(() => {
        setTabLimitError(null);
    }, []);

    return {
        tabState,
        activeTab,
        activateTab,
        createVETab,
        createGroupTab,
        closeTab,
        saveTabState,
        getTabState,
        tabLimitError,
        clearTabLimitError,
    };
}
