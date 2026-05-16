import { useState, useCallback, useRef, useEffect } from "react";
import type { AITab, AITabType, AITabState, AIAssistantPanelTabState } from "./AITabTypes";
import { createInitialTabState, DEFAULT_MAX_VE_TABS } from "./AITabTypes";
import { LoadProjectTabIndex, CloseProjectTabSession, CreateProjectTabSession } from "../../../wailsjs/go/main/App";

/**
 * Generate a deterministic hex hash from a string using a simple
 * non-cryptographic hash (FNV-1a inspired, 64-bit range).
 * Used for generating stable tab IDs from project paths.
 */
function simpleHash(input: string): string {
    let h1 = 0xdeadbeef;
    let h2 = 0x41c6ce57;
    for (let i = 0; i < input.length; i++) {
        const ch = input.charCodeAt(i);
        h1 = Math.imul(h1 ^ ch, 2654435761);
        h2 = Math.imul(h2 ^ ch, 1597334677);
    }
    h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507);
    h1 ^= Math.imul(h2 ^ (h2 >>> 13), 3266489909);
    h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507);
    h2 ^= Math.imul(h1 ^ (h1 >>> 13), 3266489909);
    const combined = (h2 >>> 0) * 0x100000000 + (h1 >>> 0);
    return combined.toString(16).padStart(12, "0").slice(0, 12);
}

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

/** Tab index entry shape returned by the backend LoadProjectTabIndex binding. */
interface BackendTabIndexEntry {
    id: string;
    type: string;
    title: string;
    projectPath?: string;
    lastActiveAt?: number;
    archived?: boolean;
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
    /** Create a new project tab. Returns the tab or null if limit reached. */
    createProjectTab: (projectPath: string, taskTitle: string) => AITab | null;
    /** Close a tab by ID */
    closeTab: (tabId: string) => void;
    /** Save state for the current active tab before switching */
    saveTabState: (tabId: string, state: Partial<AITabState>) => void;
    /** Get saved state for a tab */
    getTabState: (tabId: string) => AITabState | undefined;
    /** Get the lastActiveAt timestamp for a tab (for overflow sorting) */
    getLastActiveAt: (tabId: string) => number;
    /** Check whether a project tab with the given path already exists */
    hasProjectTab: (projectPath: string) => boolean;
    /** Get the current tab list (reads from ref, always fresh) */
    getTabs: () => AITab[];
    /** Upgrade a VE tab to a group tab when participants are added */
    upgradeVETabToGroup: (tabId: string, participants: string[], discussionId?: string) => AITab | null;
    /** Error message when max tabs exceeded (cleared after reading) */
    tabLimitError: string | null;
    /** Clear the tab limit error */
    clearTabLimitError: () => void;
}

const PROJECT_TABS_STORAGE_KEY = "ai_assistant_project_tabs";

/** Persist project tabs to localStorage for cross-session recovery. */
function persistProjectTabs(tabs: AITab[]) {
    try {
        const projectTabs = tabs.filter(t => t.type === "project" && t.projectPath);
        const serialized = projectTabs.map(t => ({
            id: t.id,
            title: t.title,
            projectPath: t.projectPath,
        }));
        if (serialized.length === 0) {
            localStorage.removeItem(PROJECT_TABS_STORAGE_KEY);
        } else {
            localStorage.setItem(PROJECT_TABS_STORAGE_KEY, JSON.stringify(serialized));
        }
    } catch {
        // localStorage full or unavailable
    }
}

/** Load persisted project tabs from localStorage.
 * Note: restored tabs may have empty conversation if the backend session was
 * evicted (30-day TTL). The user will see an empty chat area but can continue
 * working — the backend creates a fresh session on the next message. */
function loadPersistedProjectTabs(): AITab[] {
    try {
        const raw = localStorage.getItem(PROJECT_TABS_STORAGE_KEY);
        if (!raw) return [];
        const parsed = JSON.parse(raw) as Array<{ id: string; title: string; projectPath: string }>;
        if (!Array.isArray(parsed)) return [];
        return parsed
            .filter(t => t.id && t.projectPath)
            .map(t => ({
                id: t.id,
                type: "project" as AITabType,
                title: sanitizeProjectTabTitle(t.title || t.projectPath, t.projectPath),
                projectPath: t.projectPath,
                closable: true,
            }));
    } catch {
        return [];
    }
}

/**
 * Sanitize a project tab title for display. If the title looks like a raw
 * file path or a long internal task ID, extract a friendlier short name.
 */
function sanitizeProjectTabTitle(title: string, projectPath?: string): string {
    if (!title) return projectPath ? sanitizeProjectTabTitle(projectPath) : "Task";
    // If title looks like a task ID (task-<digits>), shorten it
    if (/^task-\d{10,}$/.test(title)) {
        return "Task " + title.slice(5, 13) + "\u2026";
    }
    // If title looks like a file path (contains \ or / with multiple segments), extract last segment
    if ((title.includes("\\") || title.includes("/")) && title.length > 30) {
        const segments = title.replace(/\\/g, "/").split("/").filter(Boolean);
        const last = segments[segments.length - 1] || title;
        return last.replace(/\/$/, "");
    }
    return title;
}

/**
 * Hook managing the AI Assistant Panel tab system.
 * Handles tab creation, switching, closing, duplicate detection, and max limit.
 *
 * On mount, calls LoadProjectTabIndex() to restore Tab bar state from the backend
 * (more reliable than localStorage alone). Conversation history is NOT loaded
 * immediately — it's loaded on-demand when a Tab is activated.
 */
export function useAITabManager(options: UseAITabManagerOptions = {}): UseAITabManagerResult {
    const { maxVETabs = DEFAULT_MAX_VE_TABS, onCloseVESession } = options;

    // Use a ref to track the current state synchronously for createVETab/createGroupTab
    const tabStateRef = useRef<AIAssistantPanelTabState>(createInitialTabState(maxVETabs));

    const [tabState, setTabState] = useState<AIAssistantPanelTabState>(() => {
        // Restore persisted project tabs from localStorage on mount (synchronous).
        // Backend tabs will be merged asynchronously via useEffect below.
        const initial = createInitialTabState(maxVETabs);
        const restored = loadPersistedProjectTabs();
        if (restored.length > 0) {
            initial.tabs = [...initial.tabs, ...restored.slice(0, maxVETabs)];
        }
        tabStateRef.current = initial;
        return initial;
    });
    const [tabLimitError, setTabLimitError] = useState<string | null>(null);

    // On mount, call LoadProjectTabIndex() to restore Tab bar state from backend.
    // This provides more reliable persistence than localStorage alone (survives
    // localStorage clears, works across different browser contexts, etc.).
    // Don't immediately load conversation history — that happens on-demand when
    // a Tab is activated.
    useEffect(() => {
        let cancelled = false;
        Promise.resolve().then(() => LoadProjectTabIndex()).then((entries: BackendTabIndexEntry[]) => {
            if (cancelled || !entries || !Array.isArray(entries) || entries.length === 0) return;

            setTabState(prev => {
                // Merge backend tabs with existing tabs (localStorage may have already restored some).
                const existingIds = new Set(prev.tabs.map(t => t.id));
                const existingPaths = new Set(
                    prev.tabs.filter(t => t.type === "project" && t.projectPath).map(t => t.projectPath)
                );

                const newTabs: AITab[] = [];
                for (const entry of entries) {
                    if (!entry.id || !entry.projectPath) continue;
                    if (entry.archived) continue; // Don't restore archived tabs
                    // Skip if already present (by ID or projectPath)
                    if (existingIds.has(entry.id) || existingPaths.has(entry.projectPath)) continue;

                    newTabs.push({
                        id: entry.id,
                        type: "project" as AITabType,
                        title: sanitizeProjectTabTitle(entry.title || entry.projectPath, entry.projectPath),
                        projectPath: entry.projectPath,
                        closable: true,
                    });
                }

                if (newTabs.length === 0) return prev;

                // Respect max tab limit
                const projectCount = prev.tabs.filter(t => t.type === "project").length;
                const available = prev.maxVETabs - projectCount;
                const toAdd = newTabs.slice(0, Math.max(0, available));
                if (toAdd.length === 0) return prev;

                const next = {
                    ...prev,
                    tabs: [...prev.tabs, ...toAdd],
                };
                tabStateRef.current = next;
                persistProjectTabs(next.tabs);
                return next;
            });
        }).catch(() => {
            // Backend not available (e.g., during development) — localStorage fallback is sufficient.
        });
        return () => { cancelled = true; };
    }, []);

    // Keep ref in sync with state
    const updateTabState = useCallback((updater: (prev: AIAssistantPanelTabState) => AIAssistantPanelTabState) => {
        setTabState(prev => {
            const next = updater(prev);
            tabStateRef.current = next;
            // Persist project tabs only when the tab list actually changes.
            if (next.tabs !== prev.tabs) {
                persistProjectTabs(next.tabs);
            }
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
        // Track lastActiveAt for overflow sorting
        const existing = tabStatesRef.current.get(tabId);
        if (existing) {
            existing.lastActiveAt = Date.now();
        } else {
            tabStatesRef.current.set(tabId, { history: [], scrollTop: 0, inputText: "", lastActiveAt: Date.now() });
        }
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
                lastActiveAt: Date.now(),
            });
        } else {
            tabStatesRef.current.set(newTab.id, {
                history: [],
                scrollTop: 0,
                inputText: "",
                lastActiveAt: Date.now(),
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

        // Check for duplicate and refresh authoritative metadata.
        const existing = prev.tabs.find(t => t.id === id);
        if (existing) {
            const updated: AITab = {
                ...existing,
                title,
                participants,
                discussionId: options.discussionId ?? existing.discussionId,
                readOnly: options.readOnly ?? existing.readOnly,
                role: options.role ?? existing.role,
            };
            updateTabState(() => ({
                ...prev,
                tabs: prev.tabs.map(t => t.id === id ? updated : t),
                activeTabId: existing.id,
            }));
            return updated;
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

    const createProjectTab = useCallback((projectPath: string, taskTitle: string): AITab | null => {
        const prev = tabStateRef.current;

        // Check for duplicate: if a tab with same projectPath exists, activate it
        const existing = prev.tabs.find(t => t.type === "project" && t.projectPath === projectPath);
        if (existing) {
            updateTabState(() => ({ ...prev, activeTabId: existing.id }));
            return existing;
        }

        // Check limit: project tab count ≤ maxVETabs
        const projectTabCount = prev.tabs.filter(t => t.type === "project").length;
        if (projectTabCount >= prev.maxVETabs) {
            setTabLimitError(`Project tab limit reached (max ${prev.maxVETabs})`);
            return null;
        }

        // Generate deterministic tab ID from projectPath
        const tabId = `proj-${simpleHash(projectPath)}`;

        // Create tab with type="project"
        const newTab: AITab = {
            id: tabId,
            type: "project",
            title: sanitizeProjectTabTitle(taskTitle, projectPath),
            projectPath,
            closable: true,
        };

        // Initialize tab state
        tabStatesRef.current.set(tabId, {
            history: [],
            scrollTop: 0,
            inputText: "",
            projectPath,
            lastActiveAt: Date.now(),
        });

        // Register the tab session with the backend. This is the SINGLE place
        // where tab→projectPath mapping is established. All code paths that
        // create a project tab (recent task click, fork, pending open) go
        // through this function, so the backend always knows about the tab.
        // Fire-and-forget: session may already exist (idempotent on backend).
        CreateProjectTabSession(tabId, projectPath).catch(() => {});

        // Add tab and auto-activate
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

            // For project tabs, call backend to persist session state.
            if (tab.type === "project") {
                Promise.resolve().then(() => CloseProjectTabSession(tabId)).catch(() => {});
            }

            // Remove tab state for ve/group tabs (ephemeral sessions).
            // Project tabs retain their state in the cache so reopening the
            // same task restores the conversation without a backend round-trip.
            if (tab.type !== "project") {
                tabStatesRef.current.delete(tabId);
            }

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
        // LRU eviction: prevent unbounded memory growth from cached project tab states.
        // Keep at most 32 cached project states; evict the least recently active.
        const MAX_CACHED_PROJECT_STATES = 32;
        const openTabIds = new Set(tabStateRef.current.tabs.map(t => t.id));
        const closedProjectStates: [string, AITabState][] = [];
        for (const [id, state] of tabStatesRef.current.entries()) {
            if (id.startsWith("proj-") && !openTabIds.has(id)) {
                closedProjectStates.push([id, state]);
            }
        }
        if (closedProjectStates.length > MAX_CACHED_PROJECT_STATES) {
            closedProjectStates.sort((a, b) => (a[1].lastActiveAt || 0) - (b[1].lastActiveAt || 0));
            const toEvict = closedProjectStates.length - MAX_CACHED_PROJECT_STATES;
            for (let i = 0; i < toEvict; i++) {
                tabStatesRef.current.delete(closedProjectStates[i][0]);
            }
        }
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

    const getLastActiveAt = useCallback((tabId: string): number => {
        return tabStatesRef.current.get(tabId)?.lastActiveAt ?? 0;
    }, []);

    const hasProjectTab = useCallback((projectPath: string): boolean => {
        return tabStateRef.current.tabs.some(t => t.type === "project" && t.projectPath === projectPath);
    }, []);

    /** Get the current tab list from the ref (always fresh, no stale closure). */
    const getTabs = useCallback((): AITab[] => {
        return tabStateRef.current.tabs;
    }, []);

    /** Upgrade a VE tab to a group tab (when participants are added). */
    const upgradeVETabToGroup = useCallback((tabId: string, participants: string[], discussionId?: string): AITab | null => {
        const prev = tabStateRef.current;
        const tab = prev.tabs.find(t => t.id === tabId);
        if (!tab || (tab.type !== "ve" && tab.type !== "group")) return null;

        const upgraded: AITab = {
            ...tab,
            type: "group",
            participants: participants,
            discussionId: discussionId || tab.discussionId,
        };
        updateTabState(() => ({
            ...prev,
            tabs: prev.tabs.map(t => t.id === tabId ? upgraded : t),
        }));
        return upgraded;
    }, [updateTabState]);

    const clearTabLimitError = useCallback(() => {
        setTabLimitError(null);
    }, []);

    return {
        tabState,
        activeTab,
        activateTab,
        createVETab,
        createGroupTab,
        createProjectTab,
        closeTab,
        saveTabState,
        getTabState,
        getLastActiveAt,
        getTabs,
        hasProjectTab,
        upgradeVETabToGroup,
        tabLimitError,
        clearTabLimitError,
    };
}
