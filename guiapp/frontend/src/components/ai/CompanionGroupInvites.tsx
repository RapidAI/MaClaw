import { useCallback, useEffect, useMemo, useSyncExternalStore } from "react";
import { localizeText } from "./aiAssistantI18n";
import type { AdminNotification } from "./useNotifications";

type GroupInvite = {
    id: string;
    name: string;
    owner: string;
    contact: string;
    invitedAt: string;
    // The Hub notification that delivered this invitation. It is what a client
    // that was offline when the invite was sent pulls on reconnect.
    notificationId: string;
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
        const notificationId = String(row.notification_id ?? row.NotificationID ?? row.notificationId ?? "").trim();
        out.push({ id, name, owner, contact, invitedAt, notificationId });
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
        if (prev[i].id !== next[i].id || prev[i].name !== next[i].name || prev[i].owner !== next[i].owner || prev[i].contact !== next[i].contact || prev[i].invitedAt !== next[i].invitedAt || prev[i].notificationId !== next[i].notificationId) return false;
    }
    return true;
}

function inviteError(err: unknown, fallback: string): string {
    const raw = err instanceof Error ? err.message : typeof err === "string" ? err : "";
    return raw.trim() || fallback;
}

// inviteKey identifies one invitation for the first-seen timestamp. Two
// invitations to the same group are distinguished by their invited_at.
function inviteKey(invite: Pick<GroupInvite, "contact" | "id" | "invitedAt">): string {
    return `${invite.contact}\t${invite.id}\t${invite.invitedAt}`;
}

function stampInvites(invites: GroupInvite[]) {
    const now = new Date().toISOString();
    const live = new Set(invites.map(inviteKey));
    for (const key of seenAt.keys()) {
        if (!live.has(key)) seenAt.delete(key);
    }
    for (const invite of invites) {
        const key = inviteKey(invite);
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
        // The Hub already leaves out invitations this account ignored, so the
        // list is shown exactly as returned.
        const invites = sameInvites(store.invites, next) ? store.invites : next;
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
        const ignore = appBinding("IgnoreFileCompanionGroupInvite");
        if (!ignore) {
            publish({
                ...store,
                error: {
                    id,
                    name: current?.name || "",
                    message: localizeText(langRef.current, "Couldn't ignore this invitation", "暂时无法忽略邀请", "暫時無法忽略邀請"),
                },
            });
            return;
        }
        const ticket = ++flight;
        busyGuard.current = id;
        publish({ ...store, busyId: id, error: null });
        void (async () => {
            try {
                await ignore(id);
                if (ticket !== flight) return;
                // The Hub muted the invitation for this account, so the list
                // will not return it again on any device.
                epoch += 1;
                if (current) seenAt.delete(inviteKey(current));
                publish({
                    ...store,
                    error: store.error?.id === id ? null : store.error,
                    invites: store.invites.filter((item) => item.id !== id),
                });
            } catch (err) {
                if (ticket !== flight) return;
                publish({
                    ...store,
                    error: {
                        id,
                        name: current?.name || "",
                        message: inviteError(err, localizeText(langRef.current, "Couldn't ignore this invitation", "暂时无法忽略邀请", "暫時無法忽略邀請")),
                    },
                });
            } finally {
                if (ticket !== flight) return;
                busyGuard.current = "";
                if (store.busyId === id) publish({ ...store, busyId: "" });
            }
        })();
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
            if (current) seenAt.delete(inviteKey(current));
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

// refreshCompanionGroupInvites re-reads the pending list. A pushed invitation
// notification calls this so the row appears without waiting for the next poll.
export function refreshCompanionGroupInvites() {
    if (pollers <= 0) return;
    void refreshInvites();
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
        () => snapshot.invites.map((invite) => toNotice(invite, seenAt.get(inviteKey(invite)) || "", lang || "zh")),
        [snapshot.invites, lang],
    );
    const decide = useCallback((groupId: string, action: GroupInviteAction) => {
        decideGroupInvite(groupId, action);
    }, []);
    return { notices, decide, busyId: snapshot.busyId, error: snapshot.error, listError: snapshot.listError };
}
