import { buildOutgoingMessageMulti, type ChatMessage } from "./useAIAssistant";
import { expertTabId } from "./expertTypes";
import { cloudWorkspaceIdFromPath } from "./codingTaskMode";

const MAX_PROJECT_CONTEXT_MESSAGES_TO_SEND = 12;
const PROJECT_TABS_STORAGE_KEY = "ai_assistant_project_tabs";
const PROJECT_TAB_HISTORY_STORAGE_KEY = "ai_assistant_project_tab_histories";

export function buildProjectTabRecentMessages(history: ChatMessage[] | undefined): Array<{ role: 'user' | 'assistant'; content: string }> {
    if (!Array.isArray(history) || history.length === 0) return [];
    return history
        .map(message => {
            if (message.role !== 'user' && message.role !== 'assistant') return null;
            // A guide-injection bubble records steering that was consumed by the
            // active loop. Do not replay it as an ordinary historical user turn
            // when this project/expert tab starts a later request.
            if (message.kind === 'guideInjection') return null;
            const visibleContent = String(message.content || '').trim();
            const attachmentPaths = message.role === 'user'
                ? (message.attachments || []).map(attachment => attachment.filePath).filter(Boolean)
                : [];
            const content = attachmentPaths.length > 0
                ? [visibleContent, buildOutgoingMessageMulti('', attachmentPaths)].filter(Boolean).join('\n\n')
                : visibleContent;
            if (!content) return null;
            return { role: message.role, content };
        })
        .filter((message): message is { role: 'user' | 'assistant'; content: string } => message !== null)
        .slice(-MAX_PROJECT_CONTEXT_MESSAGES_TO_SEND);
}

export function messageBelongsToSession(message: ChatMessage, sessionKey: string): boolean {
    const owner = normalizeAssistantSessionKey(message.sessionKey);
    return owner === normalizeAssistantSessionKey(sessionKey);
}

export function messageBelongsToSessionOrLegacy(message: ChatMessage, sessionKey: string): boolean {
    const owner = normalizeAssistantSessionKey(message.sessionKey);
    return !owner || owner === normalizeAssistantSessionKey(sessionKey);
}

export function projectSessionKey(projectPath?: string | null): string {
    const path = normalizeProjectSessionPath(projectPath);
    return path ? `desktop-user:${path}` : "";
}

/** Session key prefix shared by all expert conversations: desktop-user:expert:<id>. */
const EXPERT_SESSION_KEY_PREFIX = "desktop-user:expert:";
const ACP_SESSION_KEY_PREFIX = "desktop-user:acp:";
/** Durable task-management tag that binds a workspace row to an expert id. */
const EXPERT_TASK_SOURCE_PREFIX = "source:expert:";

/** Return the expert identity carried by a durable task-management row. */
export function expertIDFromTaskTags(tags?: string[] | null): string {
    for (const rawTag of tags || []) {
        const tag = String(rawTag || "").trim();
        if (!tag.startsWith(EXPERT_TASK_SOURCE_PREFIX)) continue;
        const expertID = tag.slice(EXPERT_TASK_SOURCE_PREFIX.length).trim();
        if (expertID) return expertID;
    }
    return "";
}

/**
 * The durable task-list row that corresponds to the currently visible AI tab.
 * Local / VE / group tabs have no task-list row, so this is null and the
 * sidebar highlight must clear.
 */
export type ActiveAssistantTaskIdentity = {
    projectPath?: string;
    expertId?: string;
    cloudWorkspaceId?: string;
};

/** Collapse empty / mixed identities to null so the sidebar highlight can clear. */
export function coerceActiveAssistantTask(
    identity?: ActiveAssistantTaskIdentity | null,
): ActiveAssistantTaskIdentity | null {
    if (!identity) return null;
    const expertId = String(identity.expertId || "").trim();
    if (expertId) return { expertId };
    const projectPath = normalizeProjectSessionPath(identity.projectPath);
    if (!projectPath) return null;
    const cloudWorkspaceId = String(identity.cloudWorkspaceId || "").trim() || cloudWorkspaceIdFromPath(projectPath);
    return cloudWorkspaceId ? { projectPath, cloudWorkspaceId } : { projectPath };
}

export function sameActiveAssistantTask(
    a?: ActiveAssistantTaskIdentity | null,
    b?: ActiveAssistantTaskIdentity | null,
): boolean {
    const left = coerceActiveAssistantTask(a);
    const right = coerceActiveAssistantTask(b);
    return (left?.expertId || "") === (right?.expertId || "")
        && (left?.projectPath || "") === (right?.projectPath || "")
        && (left?.cloudWorkspaceId || "") === (right?.cloudWorkspaceId || "");
}

export function activeAssistantTaskIdentity(tab: {
    type?: string;
    projectPath?: string;
    expertId?: string;
} | null | undefined, workingDir?: string | null): ActiveAssistantTaskIdentity | null {
    if (!tab) return null;
    if (tab.type === "expert") return coerceActiveAssistantTask({ expertId: tab.expertId });
    if (tab.type === "project") {
        return coerceActiveAssistantTask({
            projectPath: tab.projectPath || workingDir || undefined,
            cloudWorkspaceId: cloudWorkspaceIdFromPath(tab.projectPath) || cloudWorkspaceIdFromPath(workingDir),
        });
    }
    return null;
}

/** Session key for an expert tab conversation. Aligns with the backend userID (ExpertID branch). */
export function expertSessionKey(expertId?: string | null): string {
    const id = String(expertId || "").trim();
    return id ? `${EXPERT_SESSION_KEY_PREFIX}${id}` : "";
}

export function isExpertSessionKey(sessionKey?: string | null): boolean {
    return typeof sessionKey === "string" && sessionKey.trim().startsWith(EXPERT_SESSION_KEY_PREFIX);
}

export function isACPAssistantSessionKey(sessionKey?: string | null): boolean {
    return typeof sessionKey === "string" && sessionKey.trim().startsWith(ACP_SESSION_KEY_PREFIX);
}

/** Reverse of expertSessionKey: extract the expert id, or "" for non-expert keys. */
export function expertIdFromSessionKey(sessionKey?: string | null): string {
    const key = typeof sessionKey === "string" ? sessionKey.trim() : "";
    return key.startsWith(EXPERT_SESSION_KEY_PREFIX) ? key.slice(EXPERT_SESSION_KEY_PREFIX.length).trim() : "";
}

export function projectPathFromSessionKey(sessionKey?: string | null): string {
    const key = typeof sessionKey === "string" ? sessionKey.trim() : "";
    const prefix = "desktop-user:";
    if (!key.startsWith(prefix)) return "";
    // Expert session keys carry no project path — never hand "expert:<id>" to
    // path-based routing (it would be mistaken for a real project path).
    if (isExpertSessionKey(key) || isACPAssistantSessionKey(key)) return "";
    return normalizeProjectSessionPath(key.slice(prefix.length));
}

export function normalizeProjectSessionPath(projectPath?: string | null): string {
    let path = typeof projectPath === "string" ? projectPath.trim() : "";
    if (!path) return "";
    path = path.replace(/\\+/g, "/").replace(/\/+/g, "/");
    const drive = path.match(/^([a-zA-Z]):(\/.*)?$/);
    if (drive) path = `${drive[1].toUpperCase()}:${drive[2] || "/"}`;
    const parts: string[] = [];
    const absolute = path.startsWith("/") || /^[A-Z]:\//.test(path);
    const prefix = /^[A-Z]:\//.test(path) ? path.slice(0, 3) : (path.startsWith("/") ? "/" : "");
    const body = prefix ? path.slice(prefix.length) : path;
    for (const part of body.split("/")) {
        if (!part || part === ".") continue;
        if (part === ".." && parts.length > 0 && parts[parts.length - 1] !== "..") {
            parts.pop();
            continue;
        }
        if (part === ".." && absolute) continue;
        parts.push(part);
    }
    const joined = parts.join("/");
    if (prefix) return joined ? `${prefix}${joined}` : prefix.replace(/\/$/, "") || "/";
    return joined || ".";
}

/** Remove matching tab metadata and history blobs from localStorage. */
function purgeLocalTabCacheByIDs(deletedIDs: Set<string>): void {
    if (deletedIDs.size === 0 || typeof localStorage === "undefined") return;
    try {
        const rawTabs = localStorage.getItem(PROJECT_TABS_STORAGE_KEY);
        if (rawTabs) {
            const tabs = JSON.parse(rawTabs);
            if (Array.isArray(tabs)) {
                const keptTabs = tabs.filter(tab => !deletedIDs.has(String(tab?.id || "")));
                if (keptTabs.length === 0) localStorage.removeItem(PROJECT_TABS_STORAGE_KEY);
                else if (keptTabs.length !== tabs.length) {
                    localStorage.setItem(PROJECT_TABS_STORAGE_KEY, JSON.stringify(keptTabs));
                }
            }
        }

        const rawHistories = localStorage.getItem(PROJECT_TAB_HISTORY_STORAGE_KEY);
        if (!rawHistories) return;
        const histories = JSON.parse(rawHistories);
        if (!histories || typeof histories !== "object") return;
        let changed = false;
        for (const id of deletedIDs) {
            if (Object.prototype.hasOwnProperty.call(histories, id)) {
                delete histories[id];
                changed = true;
            }
        }
        if (!changed) return;
        if (Object.keys(histories).length === 0) localStorage.removeItem(PROJECT_TAB_HISTORY_STORAGE_KEY);
        else localStorage.setItem(PROJECT_TAB_HISTORY_STORAGE_KEY, JSON.stringify(histories));
    } catch {
        // A malformed or unavailable browser cache must not block task deletion.
    }
}

// A task deletion is stronger than closing a tab: no browser-side tab metadata
// or orphaned history for that project may survive to be restored later.
export function purgeDeletedProjectTabLocalCache(projectPath?: string | null): void {
    const normalizedPath = normalizeProjectSessionPath(projectPath);
    if (!normalizedPath || typeof localStorage === "undefined") return;
    try {
        const rawTabs = localStorage.getItem(PROJECT_TABS_STORAGE_KEY);
        if (!rawTabs) return;
        const tabs = JSON.parse(rawTabs);
        if (!Array.isArray(tabs)) return;

        const deletedIDs = new Set(
            tabs
                .filter(tab => normalizeProjectSessionPath(tab?.projectPath) === normalizedPath)
                .map(tab => String(tab.id || ""))
                .filter(Boolean),
        );
        purgeLocalTabCacheByIDs(deletedIDs);
    } catch {
        // A malformed or unavailable browser cache must not block task deletion.
    }
}

/**
 * Expert tabs are keyed by expert id (tab id `expert-<id>`), not by the durable
 * task workspace path. Deleting an expert task must purge this separate cache or
 * the next open resurrects the closed conversation from localStorage.
 */
export function purgeDeletedExpertTabLocalCache(expertId?: string | null): void {
    const id = String(expertId || "").trim();
    if (!id || typeof localStorage === "undefined") return;
    const tabId = expertTabId(id);
    try {
        const rawTabs = localStorage.getItem(PROJECT_TABS_STORAGE_KEY);
        const deletedIDs = new Set<string>([tabId]);
        if (rawTabs) {
            const tabs = JSON.parse(rawTabs);
            if (Array.isArray(tabs)) {
                for (const tab of tabs) {
                    const tabExpertId = String(tab?.expertId || "").trim();
                    const storedId = String(tab?.id || "").trim();
                    if (tab?.type === "expert" && (tabExpertId === id || storedId === tabId)) {
                        if (storedId) deletedIDs.add(storedId);
                    }
                }
            }
        }
        purgeLocalTabCacheByIDs(deletedIDs);
    } catch {
        // A malformed or unavailable browser cache must not block task deletion.
    }
}

export function normalizeAssistantSessionKey(sessionKey?: string | null): string {
    const key = typeof sessionKey === "string" ? sessionKey.trim() : "";
    if (!key) return "";
    const prefix = "desktop-user:";
    if (!key.startsWith(prefix)) return key;
	// Expert and ACP owners are opaque identifiers, not project paths. Normalizing
	// them as paths changes their identity and can merge separate sessions.
    if (isExpertSessionKey(key) || isACPAssistantSessionKey(key)) return key;
    const path = normalizeProjectSessionPath(key.slice(prefix.length));
    return path ? `${prefix}${path}` : "desktop-user";
}

export function logAIPanelDiagnostic(payload: Record<string, unknown>) {
    const logFrontendDiagnostic = typeof window !== "undefined"
        ? (window as any).go?.main?.App?.LogFrontendDiagnostic
        : undefined;
    if (typeof logFrontendDiagnostic !== "function") return;
    try {
        void Promise.resolve(logFrontendDiagnostic({ tag: "ai-panel", ...payload })).catch(() => {});
    } catch {
        // diagnostics only
    }
}

export function messageIsLocalSession(message: ChatMessage): boolean {
    const owner = normalizeAssistantSessionKey(message.sessionKey);
    return !owner || owner === "desktop-user";
}

export function chatHistoriesEquivalent(left: ChatMessage[] | undefined, right: ChatMessage[]): boolean {
    const a = Array.isArray(left) ? left : [];
    if (a.length !== right.length) return false;
    for (let i = 0; i < a.length; i += 1) {
        const leftMessage = a[i];
        const rightMessage = right[i];
        // The common live-sync path reuses message objects that did not change.
        // Avoid serializing large markdown/tool payloads for those entries.
        if (leftMessage === rightMessage) continue;
        // This predicate guards a persistence write, not just a text rerender.
        // Comparing a selected subset silently loses later updates such as an
        // injection label, streamed reasoning, task actions, or file evidence
        // after switching tabs. ChatMessage is intentionally data-only, so a
        // structural comparison is safe and keeps saved history faithful to the
        // bubble the user actually saw.
        if (JSON.stringify(leftMessage) !== JSON.stringify(rightMessage)) return false;
    }
    return true;
}
