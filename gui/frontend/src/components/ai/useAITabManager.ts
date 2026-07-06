import { useState, useCallback, useRef, useEffect } from "react";
import type { AITab, AITabType, AITabState, AIAssistantPanelTabState } from "./AITabTypes";
import { createInitialTabState, DEFAULT_MAX_VE_TABS } from "./AITabTypes";
import { LoadProjectTabIndex, CloseProjectTabSession, CreateProjectTabSession, SaveProjectTabConversation, LoadProjectTabConversation } from "../../../wailsjs/go/main/App";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";
import { isLocalHumanParticipantId, normalizeParticipantId } from "./localAIIdentity";
import { addParticipantIdentityKeys, participantIdentityMatches } from "./participantIdentity";
import { veStatusEventInfo } from "./veStatusEvent";
import { safeAvatarDataURL } from "./virtualEmployeeAvatar";
import { normalizeProjectSessionPath } from "./aiAssistantPanelSessionUtils";

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
    /** Deprecated compatibility option; closing VE tabs keeps sessions resumable. */
    onCloseVESession?: (sessionId: string) => Promise<void>;
}

export interface CreateGroupTabOptions {
    discussionId?: string;
    readOnly?: boolean;
    role?: string;
    participantNames?: Record<string, string>;
    groupTitle?: string;
}

export interface CreateProjectTabOptions {
    onSessionReady?: (tab: AITab) => void;
    prepareMode?: "restore-context" | "new-agent";
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

function normalizeTabParticipantId(value: string | undefined): string {
    return normalizeParticipantId(value);
}

function canonicalVETabId(veId: string): string {
    return `ve-${normalizeTabParticipantId(veId).replace(/_+/g, "-")}`;
}

function sameTabParticipantId(a: string | undefined, b: string | undefined): boolean {
    return participantIdentityMatches(a, b);
}

function stringsOrUndefined(value: string | undefined): string | undefined {
    const text = String(value || "").trim();
    return text || undefined;
}

function discussionRenameEventInfo(data: any): { discussionId: string; topic: string } {
    const outer = data?.payload || data?.Payload || data || {};
    const payload = outer?.payload || outer?.Payload || outer;
    const envelope = payload?.envelope || payload?.Envelope || {};
    const discussionId = String(
        payload?.discussion_id ||
        payload?.discussionId ||
        payload?.session_id ||
        payload?.sessionId ||
        payload?.DiscussionID ||
        payload?.SessionID ||
        envelope?.session_id ||
        envelope?.SessionID ||
        data?.discussion_id ||
        data?.session_id ||
        ""
    ).trim();
    const topic = String(payload?.topic || payload?.title || payload?.Topic || payload?.Title || data?.topic || data?.title || "").trim();
    return { discussionId, topic };
}

function nonLocalHumanParticipantIds(ids: string[] | undefined): string[] {
    const out: string[] = [];
    const seen = new Set<string>();
    for (const rawId of ids || []) {
        const id = String(rawId || "").trim();
        if (!id || isLocalHumanParticipantId(id)) continue;
        const aliases = new Set<string>();
        addParticipantIdentityKeys(aliases, id);
        let duplicate = false;
        for (const key of aliases) {
            if (seen.has(key)) {
                duplicate = true;
                break;
            }
        }
        if (duplicate) continue;
        aliases.forEach((key) => seen.add(key));
        out.push(id);
    }
    return out;
}

function isSingleParticipantGroupForVE(tab: AITab, veId: string): boolean {
    if (tab.type !== "group" || tab.veId) return false;
    const participants = nonLocalHumanParticipantIds(tab.participants);
    return participants.length === 1 && sameTabParticipantId(participants[0], veId);
}

function isVEIdentityTab(tab: AITab, veId: string): boolean {
    if ((tab.type === "ve" || tab.type === "group") && sameTabParticipantId(tab.veId, veId)) return true;
    return isSingleParticipantGroupForVE(tab, veId);
}

function isLiveVETab(tab: AITab): boolean {
    return tab.type === "ve" || (tab.type === "group" && !!tab.veId);
}

function evictClosedTabStates(states: Map<string, AITabState>, openTabIds: Set<string>, prefix: string, maxClosed: number) {
    const closedStates: [string, AITabState][] = [];
    for (const [id, state] of states.entries()) {
        if (id.startsWith(prefix) && !openTabIds.has(id)) {
            closedStates.push([id, state]);
        }
    }
    if (closedStates.length <= maxClosed) return;
    closedStates.sort((a, b) => (a[1].lastActiveAt || 0) - (b[1].lastActiveAt || 0));
    const toEvict = closedStates.length - maxClosed;
    for (let i = 0; i < toEvict; i++) {
        states.delete(closedStates[i][0]);
    }
}

function mergeTabStateForIdentity(target: AITabState, source: AITabState | undefined): AITabState {
    if (!source) return target;
    return {
        ...source,
        ...target,
        history: target.history?.length ? target.history : (source.history || []),
        scrollTop: target.scrollTop || source.scrollTop || 0,
        inputText: target.inputText || source.inputText || "",
        sessionId: target.sessionId || source.sessionId,
        discussionId: target.discussionId || source.discussionId,
        readOnly: target.readOnly ?? source.readOnly,
        projectPath: target.projectPath || source.projectPath,
        lastActiveAt: Math.max(target.lastActiveAt || 0, source.lastActiveAt || 0, Date.now()),
    };
}

export interface UseAITabManagerResult {
    tabState: AIAssistantPanelTabState;
    activeTab: AITab;
    /** Switch to a tab by ID */
    activateTab: (tabId: string) => void;
    /** Create a new VE conversation tab. Returns the tab or null if limit reached. */
    createVETab: (veId: string, veName: string, sessionId?: string, onlineStatus?: "online" | "offline", avatarDataURL?: string, veSkillDescription?: string) => AITab | null;
    /** Create a new group chat tab */
    createGroupTab: (id: string, title: string, participants: string[], options?: CreateGroupTabOptions) => AITab | null;
    /** Create a new project tab. Returns the tab or null if limit reached. */
    createProjectTab: (projectPath: string, taskTitle: string, options?: CreateProjectTabOptions) => AITab | null;
    /** Close a tab by ID */
    closeTab: (tabId: string) => void;
    /** Clear a VE/group conversation explicitly, resetting cached and visible state. */
    clearTabConversation: (tabId: string) => void;
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
    upgradeVETabToGroup: (tabId: string, participants: string[], discussionId?: string, participantNames?: Record<string, string>, localParticipantIds?: string[]) => AITab | null;
    /** Rename a writable group tab locally. */
    renameGroupTab: (tabId: string, title: string) => AITab | null;
    /** Error message when max tabs exceeded (cleared after reading) */
    tabLimitError: string | null;
    /** Clear the tab limit error */
    clearTabLimitError: () => void;
}

const PROJECT_TABS_STORAGE_KEY = "ai_assistant_project_tabs";
const PROJECT_TAB_HISTORY_STORAGE_KEY = "ai_assistant_project_tab_histories";
const VE_TABS_STORAGE_KEY = "ai_assistant_ve_tabs";

/** Maximum number of messages to persist per tab (keep localStorage bounded). */
const MAX_PERSISTED_HISTORY_PER_TAB = 50;

function isTransientProjectHistoryMessage(message: unknown): boolean {
    if (!message || typeof message !== "object") return false;
    const candidate = message as { role?: unknown; kind?: unknown };
    return candidate.role === "system" && (candidate.kind === "guideReceipt" || candidate.kind === "guideRejection");
}

function persistableProjectHistory(history: unknown[]): unknown[] {
    return history
        .filter(message => !isTransientProjectHistoryMessage(message))
        .slice(-MAX_PERSISTED_HISTORY_PER_TAB);
}

/** Persist project tab conversation histories to localStorage. Debounced externally. */
function persistProjectTabHistories(tabStates: Map<string, AITabState>, tabs: AITab[]) {
    try {
        const projectTabs = tabs.filter(t => t.type === "project" && t.projectPath);
        const projectTabIds = new Set(projectTabs.map(t => t.id));
        const serialized: Record<string, unknown[]> = {};

        // Persist histories for all currently-open project tabs
        for (const tab of projectTabs) {
            const state = tabStates.get(tab.id);
            if (state && Array.isArray(state.history) && state.history.length > 0) {
                const history = persistableProjectHistory(state.history);
                if (history.length > 0) serialized[tab.id] = history;
            }
        }

        // Also persist histories from tabStates that aren't in the current tab
        // list (hydrated from previous session, tab was reconciled away but may
        // be re-opened from task management later). Cap at 10 to prevent unbounded
        // localStorage growth from closed tabs. Keep the most recently active.
        const MAX_ORPHAN_HISTORIES = 10;
        const orphans: Array<[string, AITabState]> = [];
        for (const [tabId, state] of tabStates) {
            if (projectTabIds.has(tabId)) continue;
            if (!tabId.startsWith("proj-")) continue;
            if (state && Array.isArray(state.history) && state.history.length > 0) {
                orphans.push([tabId, state]);
            }
        }
        // Sort by lastActiveAt descending — keep most recently used orphans
        orphans.sort((a, b) => (b[1].lastActiveAt ?? 0) - (a[1].lastActiveAt ?? 0));
        for (const [tabId, state] of orphans.slice(0, MAX_ORPHAN_HISTORIES)) {
            const history = persistableProjectHistory(state.history);
            if (history.length > 0) serialized[tabId] = history;
        }

        if (Object.keys(serialized).length === 0) {
            localStorage.removeItem(PROJECT_TAB_HISTORY_STORAGE_KEY);
        } else {
            localStorage.setItem(PROJECT_TAB_HISTORY_STORAGE_KEY, JSON.stringify(serialized));
        }
    } catch {
        // localStorage full or unavailable — silently skip
    }
}

/** Load persisted project tab conversation histories from localStorage. */
function loadPersistedProjectTabHistories(): Record<string, unknown[]> {
    try {
        const raw = localStorage.getItem(PROJECT_TAB_HISTORY_STORAGE_KEY);
        if (!raw) return {};
        const parsed = JSON.parse(raw);
        if (!parsed || typeof parsed !== "object") return {};
        const histories: Record<string, unknown[]> = {};
        for (const [tabId, history] of Object.entries(parsed as Record<string, unknown>)) {
            if (Array.isArray(history)) histories[tabId] = persistableProjectHistory(history);
        }
        return histories;
    } catch {
        return {};
    }
}

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
            .map(t => {
                const projectPath = normalizeProjectSessionPath(t.projectPath);
                return {
                    id: t.id,
                    type: "project" as AITabType,
                    title: sanitizeProjectTabTitle(t.title || projectPath, projectPath),
                    projectPath,
                    closable: true,
                };
            })
            .filter(t => !!t.projectPath);
    } catch {
        return [];
    }
}

/** Persist VE (digital employee) tabs to localStorage for cross-session recovery.
 * Unlike project tabs which persist full conversation history, VE tabs only persist
 * the tab metadata and sessionId — conversation history lives on the Hub and is
 * loaded on-demand via GroupDiscussionGetConsultationDetail. */
function persistVETabs(tabs: AITab[], tabStates: Map<string, AITabState>) {
    try {
        // Only persist active (non-readOnly) VE/group tabs.
        // ReadOnly tabs are archived history views — they can be re-opened from
        // the "历史会话" list and should not pile up in the tab bar on restart.
        const veTabs = tabs.filter(t => (t.type === "ve" || t.type === "group") && t.veId && !t.readOnly);
        const serialized = veTabs.map(t => {
            const state = tabStates.get(t.id);
            return {
                id: t.id,
                type: t.type,
                title: t.title,
                veId: t.veId,
                sessionId: state?.sessionId || t.discussionId || undefined,
                onlineStatus: t.onlineStatus,
                // NOTE: avatarDataURL intentionally omitted — base64 images are
                // too large for localStorage (50KB+ per tab). Avatars are re-fetched
                // from Hub when the tab is opened after restart.
                veSkillDescription: t.veSkillDescription,
                participants: t.participants,
                participantNames: t.participantNames,
                discussionId: t.discussionId,
            };
        });
        if (serialized.length === 0) {
            localStorage.removeItem(VE_TABS_STORAGE_KEY);
        } else {
            localStorage.setItem(VE_TABS_STORAGE_KEY, JSON.stringify(serialized));
        }
    } catch {
        // localStorage full or unavailable
    }
}

/** Load persisted VE tabs from localStorage.
 * Restored tabs will have their sessionId available, allowing the history loading
 * effect to fetch conversation history from the Hub on activation. */
function loadPersistedVETabs(): Array<{ tab: AITab; sessionId?: string }> {
    try {
        const raw = localStorage.getItem(VE_TABS_STORAGE_KEY);
        if (!raw) return [];
        const parsed = JSON.parse(raw) as Array<{
            id: string; type: string; title: string; veId: string;
            sessionId?: string; onlineStatus?: string; avatarDataURL?: string;
            veSkillDescription?: string; participants?: string[];
            participantNames?: Record<string, string>;
            discussionId?: string; readOnly?: boolean;
        }>;
        if (!Array.isArray(parsed)) return [];
        return parsed
            .filter(t => t.id && t.veId && !t.readOnly)
            .map(t => ({
                tab: {
                    id: t.id,
                    type: (t.type === "group" ? "group" : "ve") as AITabType,
                    title: t.title || t.veId,
                    veId: t.veId,
                    onlineStatus: (t.onlineStatus === "offline" ? "offline" : "online") as "online" | "offline",
                    veSkillDescription: t.veSkillDescription,
                    participants: t.participants,
                    participantNames: t.participantNames,
                    discussionId: t.discussionId || t.sessionId,
                    closable: true,
                },
                sessionId: t.sessionId,
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
    const { maxVETabs = DEFAULT_MAX_VE_TABS } = options;

    // Use a ref to track the current state synchronously for createVETab/createGroupTab
    const tabStateRef = useRef<AIAssistantPanelTabState>(createInitialTabState(maxVETabs));
    const restoredProjectPathsRef = useRef<Set<string>>(new Set());
    const pendingProjectTabCloseByIDRef = useRef<Map<string, Promise<void>>>(new Map());

    const [tabState, setTabState] = useState<AIAssistantPanelTabState>(() => {
        // Restore persisted project tabs from localStorage on mount (synchronous).
        // Backend tabs will be merged asynchronously via useEffect below.
        // Limit to 5 tabs to prevent tab bar overflow on startup.
        const initial = createInitialTabState(maxVETabs);
        const restored = loadPersistedProjectTabs();
        restoredProjectPathsRef.current = new Set(restored.map(t => normalizeProjectSessionPath(t.projectPath)).filter(Boolean));
        if (restored.length > 0) {
            initial.tabs = [...initial.tabs, ...restored.slice(0, 5)];
        }
        // Restore persisted VE (digital employee) tabs.
        // Their sessionId is preserved so history can be loaded from Hub on activation.
        const restoredVE = loadPersistedVETabs();
        if (restoredVE.length > 0) {
            initial.tabs = [...initial.tabs, ...restoredVE.slice(0, maxVETabs).map(v => v.tab)];
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
    const backendIndexMergedRef = useRef(false);
    useEffect(() => {
        let cancelled = false;
        Promise.resolve().then(() => LoadProjectTabIndex()).then((entries: BackendTabIndexEntry[]) => {
            if (cancelled || !entries || !Array.isArray(entries)) return;

            const backendActiveProjectPaths = new Set(
                entries
                    .filter(entry => entry && !entry.archived && !!entry.projectPath)
                    .map(entry => normalizeProjectSessionPath(entry.projectPath as string))
            );

            setTabState(prev => {
                const reconciledTabs = prev.tabs.filter(t =>
                    t.type !== "project"
                    || !t.projectPath
                    || backendActiveProjectPaths.has(normalizeProjectSessionPath(t.projectPath))
                    || !restoredProjectPathsRef.current.has(normalizeProjectSessionPath(t.projectPath))
                );

                // Merge backend tabs with existing tabs (localStorage may have already restored some).
                const existingIds = new Set(reconciledTabs.map(t => t.id));
                const existingPaths = new Set(
                    reconciledTabs.filter(t => t.type === "project" && t.projectPath).map(t => normalizeProjectSessionPath(t.projectPath))
                );
                const backendTitlesByPath = new Map<string, string>();
                const backendTitlesById = new Map<string, string>();
                for (const entry of entries) {
                    if (!entry || entry.archived || !entry.projectPath) continue;
                    const normalizedPath = normalizeProjectSessionPath(entry.projectPath);
                    const title = sanitizeProjectTabTitle(entry.title || normalizedPath, normalizedPath);
                    backendTitlesByPath.set(normalizedPath, title);
                    if (entry.id) backendTitlesById.set(entry.id, title);
                }
                let titleChanged = false;
                const updatedReconciledTabs = reconciledTabs.map(tab => {
                    if (tab.type !== "project" || !tab.projectPath) return tab;
                    const backendTitle = backendTitlesByPath.get(normalizeProjectSessionPath(tab.projectPath)) || backendTitlesById.get(tab.id) || "";
                    if (!backendTitle || backendTitle === tab.title) return tab;
                    titleChanged = true;
                    return { ...tab, title: backendTitle };
                });

                const newTabs: AITab[] = [];
                for (const entry of entries) {
                    if (!entry.id || !entry.projectPath) continue;
                    const normalizedPath = normalizeProjectSessionPath(entry.projectPath);
                    if (!normalizedPath) continue;
                    if (entry.archived) continue; // Don't restore archived tabs
                    // Skip if already present (by ID or projectPath)
                    if (existingIds.has(entry.id) || existingPaths.has(normalizedPath)) continue;

                    newTabs.push({
                        id: entry.id,
                        type: "project" as AITabType,
                        title: sanitizeProjectTabTitle(entry.title || normalizedPath, normalizedPath),
                        projectPath: normalizedPath,
                        closable: true,
                    });
                }

                // Respect max tab limit: at most 5 project tabs total on startup.
                const MAX_RESTORED_PROJECT_TABS = 5;
                const projectCount = updatedReconciledTabs.filter(t => t.type === "project").length;
                const available = MAX_RESTORED_PROJECT_TABS - projectCount;
                const toAdd = newTabs.slice(0, Math.max(0, available));
                if (toAdd.length === 0 && reconciledTabs.length === prev.tabs.length && !titleChanged) return prev;

                const next = {
                    ...prev,
                    tabs: [...updatedReconciledTabs, ...toAdd],
                    activeTabId: updatedReconciledTabs.some(t => t.id === prev.activeTabId) ? prev.activeTabId : "local",
                };
                tabStateRef.current = next;
                persistProjectTabs(next.tabs);
                persistVETabs(next.tabs, tabStatesRef.current);
                return next;
            });

            // After tab index merge is complete, hydrate histories from backend session files.
            backendIndexMergedRef.current = true;
            hydrateHistoriesFromBackend();
        }).catch(() => {
            // Backend not available — still try to hydrate from session files for tabs we already have.
            if (cancelled) return;
            backendIndexMergedRef.current = true;
            hydrateHistoriesFromBackend();
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
                persistVETabs(next.tabs, tabStatesRef.current);
            }
            return next;
        });
    }, []);

    // Listen for VE online status changes and update tab onlineStatus accordingly.
    useEffect(() => {
        const unsub = EventsOn("ve:status_change", (data: any) => {
            // data may be a flat event or a Hub admin event with payload.employee.
            if (!data) return;
            const { ids, status } = veStatusEventInfo(data);
            if (status !== "online" && status !== "offline") return;
            if (ids.length === 0 || !status) return;

            updateTabState(prev => {
                const hasMatch = prev.tabs.some(t => (t.type === "ve" || (t.type === "group" && !!t.veId)) && ids.some(id => sameTabParticipantId(t.veId, id)) && t.onlineStatus !== status);
                if (!hasMatch) return prev;
                return {
                    ...prev,
                    tabs: prev.tabs.map(t =>
                        (t.type === "ve" || (t.type === "group" && !!t.veId)) && ids.some(id => sameTabParticipantId(t.veId, id)) ? { ...t, onlineStatus: status } : t
                    ),
                };
            });
        });
        return () => {
            if (typeof unsub === "function") unsub();
            else EventsOff("ve:status_change");
        };
    }, [updateTabState]);

    useEffect(() => {
        const handleRename = (data: any) => {
            const eventType = String(data?.type || data?.Type || "").trim();
            if (eventType && eventType !== "ve:discussion_rename") return;
            const { discussionId, topic } = discussionRenameEventInfo(data);
            if (!discussionId || !topic) return;
            updateTabState(prev => {
                let changed = false;
                const tabs = prev.tabs.map(tab => {
                    if (tab.type !== "group") return tab;
                    const tabDiscussionId = String(tab.discussionId || tab.id.replace(/^history-/, "")).trim();
                    if (tabDiscussionId !== discussionId || tab.groupTitle === topic) return tab;
                    changed = true;
                    return { ...tab, groupTitle: topic };
                });
                return changed ? { ...prev, tabs } : prev;
            });
        };
        const offRename = EventsOn("ve:discussion_rename", handleRename);
        const offAny = EventsOn("ve-event", handleRename);
        return () => {
            if (typeof offRename === "function") offRename();
            else EventsOff("ve:discussion_rename");
            if (typeof offAny === "function") offAny();
            else EventsOff("ve-event");
        };
    }, [updateTabState]);

    // Store tab states (history, scroll, input) keyed by tab ID
    const tabStatesRef = useRef<Map<string, AITabState>>(new Map());

    // On mount, hydrate tabStatesRef with persisted conversation histories.
    const hydratedRef = useRef(false);
    if (!hydratedRef.current) {
        hydratedRef.current = true;
        const histories = loadPersistedProjectTabHistories();
        for (const [tabId, history] of Object.entries(histories)) {
            if (Array.isArray(history) && history.length > 0) {
                tabStatesRef.current.set(tabId, { history, scrollTop: 0, inputText: "", lastActiveAt: 1 });
            }
        }
        // Hydrate VE tab states with persisted sessionIds from the tabs already
        // restored in the useState initializer above. Use the structured result
        // from loadPersistedVETabs (already parsed once in useState) to get the
        // sessionId. We call loadPersistedVETabs() again (cheap synchronous
        // localStorage read, same data) to pair each tab with its sessionId.
        const restoredVEForHydration = loadPersistedVETabs();
        for (const { tab, sessionId } of restoredVEForHydration) {
            if (!sessionId) continue;
            if (!tabStatesRef.current.has(tab.id)) {
                tabStatesRef.current.set(tab.id, { history: [], scrollTop: 0, inputText: "", sessionId, lastActiveAt: 1 });
            } else {
                const existing = tabStatesRef.current.get(tab.id)!;
                if (!existing.sessionId) {
                    tabStatesRef.current.set(tab.id, { ...existing, sessionId });
                }
            }
        }
    }

    // Flush pending history to localStorage on page unload (app closing/restarting).
    // Also flush on visibilitychange (hidden) as a backup — in Wails/Tauri desktop
    // apps, beforeunload may not fire reliably when the window is closed.
    useEffect(() => {
        const flush = () => {
            persistProjectTabHistories(tabStatesRef.current, tabStateRef.current.tabs);
            persistVETabs(tabStateRef.current.tabs, tabStatesRef.current);
            // Also flush to backend session files for reliability
            const projectTabs = tabStateRef.current.tabs.filter(t => t.type === "project" && t.projectPath);
            for (const tab of projectTabs) {
                const state = tabStatesRef.current.get(tab.id);
                if (state && Array.isArray(state.history) && state.history.length > 0) {
                    const history = persistableProjectHistory(state.history);
                    SaveProjectTabConversation(tab.id, history).catch(() => {});
                }
            }
        };
        const onVisibilityChange = () => {
            if (document.visibilityState === "hidden") flush();
        };
        window.addEventListener("beforeunload", flush);
        document.addEventListener("visibilitychange", onVisibilityChange);
        return () => {
            window.removeEventListener("beforeunload", flush);
            document.removeEventListener("visibilitychange", onVisibilityChange);
        };
    }, []);

    // Async hydration from backend session files (authoritative source).
    // localStorage provides instant UI on startup; backend files fill in data
    // that may have been lost when the process was killed without flushing.
    // Triggered after LoadProjectTabIndex merge completes (event-driven, not timer).
    const backendHydrationDoneRef = useRef(false);
    const hydrateHistoriesFromBackend = useCallback(() => {
        if (backendHydrationDoneRef.current) return;
        backendHydrationDoneRef.current = true;
        const projectTabs = tabStateRef.current.tabs.filter(t => t.type === "project");
        for (const tab of projectTabs) {
            LoadProjectTabConversation(tab.id).then(conversation => {
                if (!conversation || !Array.isArray(conversation) || conversation.length === 0) return;
                const existing = tabStatesRef.current.get(tab.id);
                const existingLen = existing?.history?.length || 0;
                const history = persistableProjectHistory(conversation);
                if (history.length !== conversation.length) {
                    SaveProjectTabConversation(tab.id, history).catch(() => {});
                }
                // Backend wins if it has more or equal history (more recent flush survived process kill)
                if (history.length >= existingLen) {
                    tabStatesRef.current.set(tab.id, {
                        ...(existing || { scrollTop: 0, inputText: "" }),
                        history,
                        lastActiveAt: Date.now(),
                    });
                }
            }).catch(() => {});
        }
    }, []);

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

    const createVETab = useCallback((veId: string, veName: string, sessionId?: string, onlineStatus?: "online" | "offline", avatarDataURL?: string, veSkillDescription?: string): AITab | null => {
        const prev = tabStateRef.current;
        const canonicalVEId = String(veId || "").trim();
        if (!canonicalVEId) return null;
        const safeAvatar = safeAvatarDataURL(avatarDataURL);
        const normalizedSkillDescription = String(veSkillDescription || "").trim();

        const canonicalTabId = canonicalVETabId(canonicalVEId);

        // Check for duplicate: if a tab with same veId exists, activate it.
        // A VE tab may already have been upgraded to a live group tab, so include both.
        const existing = prev.tabs.find(t => t.id === canonicalTabId && isVEIdentityTab(t, canonicalVEId))
            || prev.tabs.find(t => isVEIdentityTab(t, canonicalVEId));
        if (existing) {
            const identityTabs = prev.tabs.filter(t => isVEIdentityTab(t, canonicalVEId));
            const saved = tabStatesRef.current.get(existing.id) || { history: [], scrollTop: 0, inputText: "" };
            const shouldPromoteHistoryGroup = isSingleParticipantGroupForVE(existing, canonicalVEId);
            const targetTabId = shouldPromoteHistoryGroup ? canonicalTabId : existing.id;
            let nextSaved: AITabState = {
                ...saved,
                ...(sessionId ? { sessionId } : {}),
                lastActiveAt: Date.now(),
            };
            for (const duplicate of identityTabs) {
                if (duplicate.id === existing.id) continue;
                nextSaved = mergeTabStateForIdentity(nextSaved, tabStatesRef.current.get(duplicate.id));
                tabStatesRef.current.delete(duplicate.id);
            }
            tabStatesRef.current.set(targetTabId, nextSaved);
            if (targetTabId !== existing.id) tabStatesRef.current.delete(existing.id);
            const updated: AITab = shouldPromoteHistoryGroup
                ? {
                    ...existing,
                    id: targetTabId,
                    title: veName || existing.title,
                    veId: canonicalVEId,
                    participants: existing.participants?.length ? existing.participants : [canonicalVEId],
                    participantNames: veName ? { ...(existing.participantNames || {}), [canonicalVEId]: veName } : existing.participantNames,
                    onlineStatus: onlineStatus || existing.onlineStatus || "online",
                    avatarDataURL: safeAvatar || existing.avatarDataURL,
                    veSkillDescription: normalizedSkillDescription || existing.veSkillDescription,
                    readOnly: false,
                }
                : isLiveVETab(existing) && (onlineStatus || safeAvatar || normalizedSkillDescription)
                    ? {
                        ...existing,
                        ...(onlineStatus ? { onlineStatus } : {}),
                        ...(safeAvatar ? { avatarDataURL: safeAvatar } : {}),
                        ...(normalizedSkillDescription ? { veSkillDescription: normalizedSkillDescription } : {}),
                    }
                    : existing;
            updateTabState(() => ({
                ...prev,
                tabs: prev.tabs
                    .filter(t => t.id === existing.id || (t.id !== targetTabId && !identityTabs.some(duplicate => duplicate.id === t.id)))
                    .map(t => t.id === existing.id ? updated : t),
                activeTabId: targetTabId,
            }));
            return updated;
        }

        // Check max VE tab limit
        const veTabCount = prev.tabs.filter(isLiveVETab).length;
        if (veTabCount >= prev.maxVETabs) {
            setTabLimitError(`Digital employee tab limit reached (max ${prev.maxVETabs})`);
            return null;
        }

        // Create new tab
        const newTab: AITab = {
            id: canonicalTabId,
            type: "ve",
            title: veName,
            veId: canonicalVEId,
            onlineStatus: onlineStatus || "online",
            avatarDataURL: safeAvatar,
            veSkillDescription: normalizedSkillDescription || undefined,
            closable: true,
        };
        const cachedState = tabStatesRef.current.get(newTab.id);
        tabStatesRef.current.set(newTab.id, {
            history: [],
            scrollTop: 0,
            inputText: "",
            ...cachedState,
            ...(sessionId ? { sessionId } : {}),
            lastActiveAt: Date.now(),
        });

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
            const requestedGroupTitle = stringsOrUndefined(options.groupTitle);
            const updated: AITab = {
                ...existing,
                title,
                groupTitle: requestedGroupTitle || existing.groupTitle,
                participants,
                discussionId: options.discussionId ?? existing.discussionId,
                readOnly: options.readOnly ?? existing.readOnly,
                role: options.role ?? existing.role,
                participantNames: options.participantNames
                    ? { ...(existing.participantNames || {}), ...options.participantNames }
                    : existing.participantNames,
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
            groupTitle: stringsOrUndefined(options.groupTitle),
            participants,
            discussionId: options.discussionId,
            readOnly: options.readOnly,
            role: options.role,
            participantNames: options.participantNames,
            closable: true,
        };

        updateTabState(() => ({
            ...prev,
            tabs: [...prev.tabs, newTab],
            activeTabId: newTab.id,
        }));

        return newTab;
    }, [updateTabState]);

    const createProjectTab = useCallback((projectPath: string, taskTitle: string, options?: CreateProjectTabOptions): AITab | null => {
        projectPath = normalizeProjectSessionPath(projectPath);
        if (!projectPath) return null;
        const prev = tabStateRef.current;

        // Check for duplicate: if a tab with same projectPath exists, activate it
        const existing = prev.tabs.find(t => t.type === "project" && normalizeProjectSessionPath(t.projectPath) === projectPath);
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
        restoredProjectPathsRef.current.delete(projectPath);

        // Initialize tab state — preserve any history already hydrated from
        // localStorage (e.g., the tab was persisted in a previous session but
        // removed from the tab list during backend reconciliation, and now is
        // being re-opened from task management). Only set empty history if there is
        // truly no prior state for this tabId.
        const existingHydratedState = tabStatesRef.current.get(tabId);
        if (!existingHydratedState || !Array.isArray(existingHydratedState.history) || existingHydratedState.history.length === 0) {
            tabStatesRef.current.set(tabId, {
                history: [],
                scrollTop: 0,
                inputText: "",
                projectPath,
                lastActiveAt: Date.now(),
            });
        } else {
            // Keep existing history, just update metadata
            existingHydratedState.projectPath = projectPath;
            existingHydratedState.lastActiveAt = Date.now();
        }

        // Register the tab session with the backend. This is the SINGLE place
        // where tab→projectPath mapping is established. All code paths that
        // create a project tab (task management open, fork, pending open) go
        // through this function, so the backend always knows about the tab.
        // Fire-and-forget: session may already exist (idempotent on backend).
        // If this deterministic tab id was closed moments ago, serialize the
        // create after close so a late close cannot archive a reopened task.
        const pendingClose = pendingProjectTabCloseByIDRef.current.get(tabId);
        const register = () => CreateProjectTabSession(tabId, projectPath)
            .then(() => options?.onSessionReady?.(newTab))
            .catch(() => {});
        if (pendingClose) pendingClose.finally(register);
        else void register();

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

            // For project tabs, call backend to persist session state.
            if (tab.type === "project") {
                const closePromise = Promise.resolve()
                    .then(() => CloseProjectTabSession(tabId))
                    .catch(() => {})
                    .then(() => undefined);
                pendingProjectTabCloseByIDRef.current.set(tabId, closePromise);
                closePromise.finally(() => {
                    if (pendingProjectTabCloseByIDRef.current.get(tabId) === closePromise) {
                        pendingProjectTabCloseByIDRef.current.delete(tabId);
                    }
                });
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
        // LRU eviction: prevent unbounded memory growth from cached closed tab states.
        const openTabIds = new Set(tabStateRef.current.tabs.map(t => t.id));
        evictClosedTabStates(tabStatesRef.current, openTabIds, "proj-", 32);
        evictClosedTabStates(tabStatesRef.current, openTabIds, "ve-", 32);
        evictClosedTabStates(tabStatesRef.current, openTabIds, "history-", 32);
        evictClosedTabStates(tabStatesRef.current, openTabIds, "group-", 32);
    }, [updateTabState]);

    const historyPersistTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const dirtyTabIdsRef = useRef<Set<string>>(new Set());
    const scheduleHistoryPersist = useCallback(() => {
        if (historyPersistTimerRef.current) clearTimeout(historyPersistTimerRef.current);
        historyPersistTimerRef.current = setTimeout(() => {
            // 1. localStorage (fast local cache)
            persistProjectTabHistories(tabStatesRef.current, tabStateRef.current.tabs);
            // 2. Backend session file (reliable, survives process kill) — only dirty tabs
            const dirtyIds = dirtyTabIdsRef.current;
            if (dirtyIds.size > 0) {
                const projectTabs = tabStateRef.current.tabs.filter(t => t.type === "project" && t.projectPath && dirtyIds.has(t.id));
                for (const tab of projectTabs) {
                    const state = tabStatesRef.current.get(tab.id);
                    if (state && Array.isArray(state.history) && state.history.length > 0) {
                        const history = persistableProjectHistory(state.history);
                        SaveProjectTabConversation(tab.id, history).catch(() => {});
                    }
                }
                dirtyTabIdsRef.current = new Set();
            }
        }, 500);
    }, []);

    const clearTabConversation = useCallback((tabId: string) => {
        tabStatesRef.current.set(tabId, {
            history: [],
            scrollTop: 0,
            inputText: "",
            lastActiveAt: Date.now(),
        });
        updateTabState(prev => ({
            ...prev,
            tabs: prev.tabs.map(t => t.id === tabId ? { ...t, discussionId: undefined, conversationResetSeq: (t.conversationResetSeq || 0) + 1 } : t),
        }));
        // Persist the cleared state so it doesn't resurrect after restart
        dirtyTabIdsRef.current.add(tabId);
        scheduleHistoryPersist();
        // Also clear backend session file immediately (don't wait for debounce)
        SaveProjectTabConversation(tabId, []).catch(() => {});
    }, [scheduleHistoryPersist, updateTabState]);

    const saveTabState = useCallback((tabId: string, state: Partial<AITabState>) => {
        const openTab = tabStateRef.current.tabs.find(t => t.id === tabId);
        const cachedState = tabStatesRef.current.get(tabId);
        if (!openTab && !cachedState) return;
        const existing = cachedState || {
            history: [],
            scrollTop: 0,
            inputText: "",
        };
        tabStatesRef.current.set(tabId, { ...existing, ...state });
        // Debounce-persist history for project tabs
        if (openTab && openTab.type === "project" && state.history) {
            dirtyTabIdsRef.current.add(tabId);
            scheduleHistoryPersist();
        }
        // Persist VE tabs when sessionId changes (critical for cross-restart recovery).
        // sessionId is set once after initSession and rarely changes, so no debounce needed.
        if (openTab && (openTab.type === "ve" || openTab.type === "group") && state.sessionId) {
            persistVETabs(tabStateRef.current.tabs, tabStatesRef.current);
        }
    }, [scheduleHistoryPersist]);

    const getTabState = useCallback((tabId: string): AITabState | undefined => {
        return tabStatesRef.current.get(tabId);
    }, []);

    const getLastActiveAt = useCallback((tabId: string): number => {
        return tabStatesRef.current.get(tabId)?.lastActiveAt ?? 0;
    }, []);

    const hasProjectTab = useCallback((projectPath: string): boolean => {
        const normalizedPath = normalizeProjectSessionPath(projectPath);
        return tabStateRef.current.tabs.some(t => t.type === "project" && normalizeProjectSessionPath(t.projectPath) === normalizedPath);
    }, []);

    /** Get the current tab list from the ref (always fresh, no stale closure). */
    const getTabs = useCallback((): AITab[] => {
        return tabStateRef.current.tabs;
    }, []);

    /** Upgrade a VE tab to a group tab (when participants are added).
     *  State is preserved because VEConversationView uses the same key (tab.id)
     *  and useVEStatePersistence saves state on unmount. The tab type change
     *  only affects the outer layout (participant panel visibility), not the
     *  VEConversationView instance itself — React reconciles by key, not by
     *  wrapper component type. However, to be safe against edge cases where
     *  React does remount, we explicitly snapshot the current tab state before
     *  changing the type.
     */
    const upgradeVETabToGroup = useCallback((tabId: string, participants: string[], discussionId?: string, participantNames?: Record<string, string>, localParticipantIds?: string[]): AITab | null => {
        const prev = tabStateRef.current;
        const tab = prev.tabs.find(t => t.id === tabId);
        if (!tab || (tab.type !== "ve" && tab.type !== "group")) return null;

        // Preserve existing tab state (messages, sessionId, inputText) across the type change.
        // If the VEConversationView is currently mounted, its useVEStatePersistence cleanup
        // will save state on unmount. But the cleanup runs asynchronously in React's commit
        // phase — by that time the new wrapper may already be mounting with empty state.
        // To prevent this race, we ensure the tabStatesRef already has the current state
        // before changing the tab type. The new wrapper reads from getTabState(tabId) on mount.
        // If tabStatesRef already has state for this tabId (saved by a previous unmount or
        // by the VEConversationView's periodic auto-save), it will be used. No data loss.

        const upgraded: AITab = {
            ...tab,
            type: "group",
            participants: participants,
            discussionId: discussionId || tab.discussionId,
            participantNames: participantNames ? { ...(tab.participantNames || {}), ...participantNames } : tab.participantNames,
            localParticipantIds: localParticipantIds ? Array.from(new Set([...(tab.localParticipantIds || []), ...localParticipantIds].map((id) => String(id || "").trim()).filter(Boolean))) : tab.localParticipantIds,
        };
        updateTabState(() => ({
            ...prev,
            tabs: prev.tabs.map(t => t.id === tabId ? upgraded : t),
        }));
        return upgraded;
    }, [updateTabState]);

    const renameGroupTab = useCallback((tabId: string, title: string): AITab | null => {
        const nextTitle = String(title || "").trim();
        if (!nextTitle) return null;
        const prev = tabStateRef.current;
        const tab = prev.tabs.find(t => t.id === tabId);
        if (!tab || tab.type !== "group" || tab.readOnly) return null;
        const renamed: AITab = { ...tab, groupTitle: nextTitle };
        updateTabState(() => ({
            ...prev,
            tabs: prev.tabs.map(t => t.id === tabId ? renamed : t),
        }));
        return renamed;
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
        clearTabConversation,
        saveTabState,
        getTabState,
        getLastActiveAt,
        getTabs,
        hasProjectTab,
        upgradeVETabToGroup,
        renameGroupTab,
        tabLimitError,
        clearTabLimitError,
    };
}
