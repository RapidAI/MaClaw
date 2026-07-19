import type { ChatMessage } from "./useAIAssistant";

const MAX_PROJECT_CONTEXT_MESSAGES_TO_SEND = 12;

export function buildProjectTabRecentMessages(history: ChatMessage[] | undefined): Array<{ role: 'user' | 'assistant'; content: string }> {
    if (!Array.isArray(history) || history.length === 0) return [];
    return history
        .map(message => {
            if (message.role !== 'user' && message.role !== 'assistant') return null;
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
        if (leftMessage.id !== rightMessage.id) return false;
        if (leftMessage.content !== rightMessage.content) return false;
        if (leftMessage.role !== rightMessage.role) return false;
        if ((leftMessage.requestId || "") !== (rightMessage.requestId || "")) return false;
    }
    return true;
}
