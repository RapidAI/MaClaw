import { useCallback, useEffect, useMemo, useSyncExternalStore } from "react";
import { localizeText } from "./aiAssistantI18n";
import type { AdminNotification } from "./useNotifications";

const IGNORED_KEY = "maclaw.companion-group-invite.ignored";

type GroupInvite = {
    id: string;
    name: string;
    owner: string;
    contact: string;
    invitedAt: string;
};

export type GroupInviteAction = "accept" | "decline" | "ignore";

export type GroupInviteError = { id: string; name: string; message: string };

type InviteStore = {
    invites: GroupInvite[];
    busyId: string;
    error: GroupInviteError | null;
    // Last list fetch failed. Empty after a successful fetch, including an empty inbox.
    listError: string;
};

const emptyStore: InviteStore = { invites: [], busyId: "", error: null, listError: "" };

// The welcome bell and the task-header bell can both be mounted (the new-task
// wizard covers the header). One store keeps a single pending list, so accepting
// on the visible bell does not leave the other bell showing that invite.
let store: InviteStore = emptyStore;
const listeners = new Set<() => void>();
let pollers = 0;
let pollTimer = 0;
let focusBound = false;
let epoch = 0;
let flight = 0;
let refreshTicket = 0;
const seenAt = new Map<string, string>();
const busyGuard = { current: "" };
const langRef = { current: "zh" };

function emit() {
    listeners.forEach((listener) => listener());
}

function publish(next: InviteStore) {
    if (next.invites === store.invites && next.busyId === store.busyId && next.error === store.error && next.listError === store.listError) return;
    store = next;
    emit();
}

function resetStore() {
    epoch += 1;
    flight += 1;
    store = emptyStore;
    seenAt.clear();
    busyGuard.current = "";
}

function subscribeStore(listener: () => void) {
    listeners.add(listener);
    return () => {
        listeners.delete(listener);
        if (listeners.size === 0 && pollers === 0) resetStore();
    };
}

function getSnapshot(): InviteStore {
    return store;
}

function appBinding(name: string): ((...args: unknown[]) => Promise<unknown>) | null {
    const app = (window as unknown as { go?: { main?: { App?: Record<string, unknown> } } }).go?.main?.App;
    const binding = app?.[name];
    return typeof binding === "function" ? binding as (...args: unknown[]) => Promise<unknown> : null;
}

function readInvites(raw: unknown): GroupInvite[] {
    const record = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
    const list = Array.isArray(raw) ? raw : Array.isArray(record.invites) ? record.invites : Array.isArray(record.Invites) ? record.Invites : [];
    const out: GroupInvite[] = [];
    const seen = new Set<string>();
    for (const item of list) {
        if (!item || typeof item !== "object") continue;
        const row = item as Record<string, unknown>;
        const id = String(row.id ?? row.ID ?? "").trim();
        if (!id || seen.has(id)) continue;
        seen.add(id);
        const name = String(row.name ?? row.Name ?? "").trim() || "阅读群组";
        const owner = String(row.owner ?? row.Owner ?? "").trim() || "一位用户";
        const contact = String(row.contact ?? row.Contact ?? "").trim();
        const invitedAt = String(row.invited_at ?? row.InvitedAt ?? row.invitedAt ?? "").trim();
        out.push({ id, name, owner, contact, invitedAt });
    }
    // The hub walks a map, so the payload order changes between polls.
    // Newest invitation first; the id breaks ties.
    out.sort(compareInvites);
    return out;
}

function compareInvites(a: GroupInvite, b: GroupInvite): number {
    if (a.invitedAt !== b.invitedAt) return a.invitedAt < b.invitedAt ? 1 : -1;
    if (a.id !== b.id) return a.id < b.id ? -1 : 1;
    return 0;
}

function sameInvites(prev: GroupInvite[], next: GroupInvite[]): boolean {
    if (prev.length !== next.length) return false;
    for (let i = 0; i < prev.length; i += 1) {
        if (prev[i].id !== next[i].id || prev[i].name !== next[i].name || prev[i].owner !== next[i].owner || prev[i].contact !== next[i].contact || prev[i].invitedAt !== next[i].invitedAt) return false;
    }
    return true;
}

function inviteError(err: unknown, fallback: string): string {
    const raw = err instanceof Error ? err.message : typeof err === "string" ? err : "";
    return raw.trim() || fallback;
}

function readIgnored(): Set<string> {
    try {
        const raw = JSON.parse(localStorage.getItem(IGNORED_KEY) || "[]");
        if (!Array.isArray(raw)) return new Set();
        return new Set(raw.filter((item) => typeof item === "string" && item.trim()));
    } catch {
        return new Set();
    }
}

function writeIgnored(ids: Set<string>): boolean {
    try {
        localStorage.setItem(IGNORED_KEY, JSON.stringify([...ids]));
        return true;
    } catch {
        return false;
    }
}

function ignoreKey(invite: Pick<GroupInvite, "contact" | "id" | "invitedAt">): string {
    return `${invite.contact}\t${invite.id}\t${invite.invitedAt}`;
}

// Older builds stored the group id, or the id plus invited_at, with no account.
// An empty contact is the same shape written before the hub sent one.
// A key that already names a contact belongs to that account and is never rewritten.
function isLegacyIgnore(key: string, invite: GroupInvite): boolean {
    if (key === invite.id) return true;
    if (key === `${invite.id}\t${invite.invitedAt}`) return true;
    return key === `\t${invite.id}\t${invite.invitedAt}`;
}

// Drop an ignore only when this signed-in contact's list proves that invitation is gone.
// An empty list is also what we get when nobody is logged in, so it must not forget ignores.
function syncIgnored(ignored: Set<string>, next: GroupInvite[]): boolean {
    let changed = false;
    if (next.length === 0) return changed;
    for (const item of next) {
        const canon = ignoreKey(item);
        for (const key of [...ignored]) {
            if (key === canon || !isLegacyIgnore(key, item)) continue;
            ignored.delete(key);
            ignored.add(canon);
            changed = true;
        }
    }
    const contacts = new Set(next.map((item) => item.contact).filter(Boolean));
    const live = new Set(next.map(ignoreKey));
    for (const key of [...ignored]) {
        const contact = key.split("\t")[0];
        if (!contact || !contacts.has(contact) || key.split("\t").length < 3 || live.has(key)) continue;
        ignored.delete(key);
        changed = true;
    }
    return changed;
}

function stampInvites(invites: GroupInvite[]) {
    const now = new Date().toISOString();
    const live = new Set(invites.map(ignoreKey));
    for (const key of seenAt.keys()) {
        if (!live.has(key)) seenAt.delete(key);
    }
    for (const invite of invites) {
        const key = ignoreKey(invite);
        if (!seenAt.has(key)) seenAt.set(key, now);
    }
}

function toNotice(invite: GroupInvite, createdAt: string, lang: string): AdminNotification {
    const owner = invite.owner;
    const name = invite.name;
    const invitedAt = Date.parse(invite.invitedAt);
    return {
        id: `companion-group:${invite.id}`,
        groupInviteId: invite.id,
        title: localizeText(lang, "Group invitation", "入群邀请", "入群邀請"),
        content: localizeText(
            lang,
            `${owner} invited you to the reading group “${name}”. After you accept, the group's documents show up in the companion library.`,
            `${owner} 邀请你加入阅读群组「${name}」。同意后，可以在伴读的文档管理里看到群组文章。`,
            `${owner} 邀請你加入閱讀群組「${name}」。同意後，可以在伴讀的文件管理裡看到群組文章。`,
        ),
        category: "custom",
        priority: "important",
        is_read: false,
        created_at: Number.isNaN(invitedAt) ? createdAt : new Date(invitedAt).toISOString(),
    };
}

async function refreshInvites() {
    const seen = epoch;
    // Overlapping polls and focus refreshes used to publish in completion order.
    // Only the newest request may change the list or the load error.
    const ticket = ++refreshTicket;
    const list = appBinding("ListFileCompanionGroupInvites");
    if (!list) {
        if (seen !== epoch || ticket !== refreshTicket) return;
        if (store.invites.length > 0 || store.listError) publish({ ...store, invites: [], listError: "" });
        return;
    }
    try {
        const next = readInvites(await list());
        if (seen !== epoch || ticket !== refreshTicket) return;
        const ignored = readIgnored();
        if (syncIgnored(ignored, next)) writeIgnored(ignored);
        const filtered = next.filter((item) => !ignored.has(ignoreKey(item)));
        const invites = sameInvites(store.invites, filtered) ? store.invites : filtered;
        stampInvites(invites);
        // A decision error belongs to a row. Once that invitation is gone, the
        // sentence would have nothing to point at.
        const previousError = store.error;
        const error = previousError && invites.some((item) => item.id === previousError.id) ? previousError : null;
        publish({ ...store, invites, listError: "", error });
    } catch (err) {
        if (seen !== epoch || ticket !== refreshTicket) return;
        // A failed refresh is not an empty inbox. Keep the invitations already shown.
        const message = inviteError(err, localizeText(langRef.current, "Couldn't load group invitations", "入群邀请暂时无法读取", "入群邀請暫時無法讀取"));
        publish({ ...store, listError: message });
    }
}

function onWindowFocus() {
    void refreshInvites();
}

function startPolling() {
    if (!pollTimer) pollTimer = window.setInterval(() => { void refreshInvites(); }, 30_000);
    if (!focusBound) {
        window.addEventListener("focus", onWindowFocus);
        focusBound = true;
    }
}

function stopPolling() {
    if (pollTimer) {
        window.clearInterval(pollTimer);
        pollTimer = 0;
    }
    if (focusBound) {
        window.removeEventListener("focus", onWindowFocus);
        focusBound = false;
    }
}

function decideGroupInvite(groupId: string, action: GroupInviteAction) {
    const id = groupId.trim();
    if (!id || busyGuard.current) return;
    if (action === "ignore") {
        const current = store.invites.find((item) => item.id === id);
        if (!current) return;
        const ignored = readIgnored();
        ignored.add(ignoreKey(current));
        if (!writeIgnored(ignored)) {
            publish({
                ...store,
                error: {
                    id,
                    name: current.name,
                    message: localizeText(langRef.current, "Couldn't ignore this invitation", "暂时无法忽略邀请", "暫時無法忽略邀請"),
                },
            });
            return;
        }
        epoch += 1;
        seenAt.delete(ignoreKey(current));
        publish({
            ...store,
            error: store.error?.id === id ? null : store.error,
            invites: store.invites.filter((item) => item.id !== id),
        });
        return;
    }
    const ticket = ++flight;
    const current = store.invites.find((item) => item.id === id);
    busyGuard.current = id;
    publish({ ...store, busyId: id, error: null });
    const call = appBinding(action === "accept" ? "AcceptFileCompanionGroupInvite" : "DeclineFileCompanionGroupInvite");
    const fallback = action === "accept" ? "暂时无法加入群组" : "暂时无法拒绝邀请";
    void (async () => {
        try {
            if (!call) throw new Error(fallback);
            await call(id);
            if (ticket !== flight) return;
            epoch += 1;
            if (current) seenAt.delete(ignoreKey(current));
            publish({
                ...store,
                error: store.error?.id === id ? null : store.error,
                invites: store.invites.filter((item) => item.id !== id),
            });
        } catch (err) {
            if (ticket !== flight) return;
            publish({ ...store, error: { id, name: current?.name || "", message: inviteError(err, fallback) } });
        } finally {
            if (ticket !== flight) return;
            busyGuard.current = "";
            if (store.busyId === id) publish({ ...store, busyId: "" });
        }
    })();
}

// Pending reading-group invites for the main-window notification bell.
// The companion process cannot host this confirm, so the list is polled here.
export function useCompanionGroupInvites(active: boolean, lang = "zh") {
    const snapshot = useSyncExternalStore(subscribeStore, getSnapshot, getSnapshot);
    langRef.current = lang || "zh";
    useEffect(() => {
        if (!active) return;
        pollers += 1;
        if (pollers === 1) startPolling();
        void refreshInvites();
        return () => {
            pollers -= 1;
            if (pollers <= 0) {
                pollers = 0;
                stopPolling();
                if (listeners.size === 0) resetStore();
            }
        };
    }, [active]);
    const notices = useMemo(
        () => snapshot.invites.map((invite) => toNotice(invite, seenAt.get(ignoreKey(invite)) || "", lang || "zh")),
        [snapshot.invites, lang],
    );
    const decide = useCallback((groupId: string, action: GroupInviteAction) => {
        decideGroupInvite(groupId, action);
    }, []);
    return { notices, decide, busyId: snapshot.busyId, error: snapshot.error, listError: snapshot.listError };
}
