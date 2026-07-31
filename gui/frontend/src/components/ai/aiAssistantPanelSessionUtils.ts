import type { ChatMessage } from "./useAIAssistant";

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
            const content = String(message.content || '').trim();
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

/** Session key for an expert tab conversation. Aligns with the backend userID (ExpertID branch). */
export function expertSessionKey(expertId?: string | null): string {
    const id = String(expertId || "").trim();
    return id ? `${EXPERT_SESSION_KEY_PREFIX}${id}` : "";
}

export function isExpertSessionKey(sessionKey?: string | null): boolean {
    return typeof sessionKey === "string" && sessionKey.trim().startsWith(EXPERT_SESSION_KEY_PREFIX);
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
    if (isExpertSessionKey(key)) return "";
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
        if (deletedIDs.size === 0) return;

        const keptTabs = tabs.filter(tab => !deletedIDs.has(String(tab?.id || "")));
        if (keptTabs.length === 0) localStorage.removeItem(PROJECT_TABS_STORAGE_KEY);
        else localStorage.setItem(PROJECT_TABS_STORAGE_KEY, JSON.stringify(keptTabs));

        const rawHistories = localStorage.getItem(PROJECT_TAB_HISTORY_STORAGE_KEY);
        if (!rawHistories) return;
        const histories = JSON.parse(rawHistories);
        if (!histories || typeof histories !== "object") return;
        for (const id of deletedIDs) delete histories[id];
        if (Object.keys(histories).length === 0) localStorage.removeItem(PROJECT_TAB_HISTORY_STORAGE_KEY);
        else localStorage.setItem(PROJECT_TAB_HISTORY_STORAGE_KEY, JSON.stringify(histories));
    } catch {
        // A malformed or unavailable browser cache must not block task deletion.
    }
}

export function normalizeAssistantSessionKey(sessionKey?: string | null): string {
    const key = typeof sessionKey === "string" ? sessionKey.trim() : "";
    if (!key) return "";
    const prefix = "desktop-user:";
    if (!key.startsWith(prefix)) return key;
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
