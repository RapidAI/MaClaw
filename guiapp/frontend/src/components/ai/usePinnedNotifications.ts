import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { EventsOn } from "../../../wailsjs/runtime";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/**
 * PinnedMessage mirrors the Go backend PinnedNotification struct. The Hub
 * orders the list permanent-pin first, then the latest three regular pins.
 */
export interface PinnedMessage {
  id: string;
  title: string;
  content: string;
  link_url: string;
  button_text: string;
  pinned_forever: boolean;
  created_at: string;
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function normalizePinnedMessage(raw: unknown): PinnedMessage | null {
  if (!raw || typeof raw !== "object") return null;
  const record = raw as Record<string, unknown>;
  const id = asString(record.id) || asString(record.ID);
  if (!id) return null;
  return {
    id,
    title: asString(record.title) || asString(record.Title),
    content: asString(record.content) || asString(record.Content),
    link_url: asString(record.link_url) || asString(record.LinkURL),
    button_text: asString(record.button_text) || asString(record.ButtonText),
    pinned_forever: Boolean(record.pinned_forever ?? record.PinnedForever),
    created_at: asString(record.created_at) || asString(record.CreatedAt),
  };
}

function normalizePinnedList(raw: unknown): PinnedMessage[] {
  if (!Array.isArray(raw)) return [];
  const list: PinnedMessage[] = [];
  for (const item of raw) {
    const message = normalizePinnedMessage(item);
    if (message) list.push(message);
  }
  return list;
}

function getAppBinding(name: string): ((...args: any[]) => Promise<any>) | null {
  const appBindings = (window as any)?.go?.main?.App as
    | Record<string, ((...args: any[]) => Promise<any>) | undefined>
    | undefined;
  const binding = appBindings?.[name];
  return typeof binding === "function" ? binding : null;
}

// ---------------------------------------------------------------------------
// Session dismissal — in-memory only by design: closing the banner hides it
// until the app restarts, while a pinned message published later in the same
// session (a new id) still pops up.
// ---------------------------------------------------------------------------

const sessionDismissedIds = new Set<string>();

function dismissAllForSession(ids: string[]): void {
  for (const id of ids) sessionDismissedIds.add(id);
}

function filterVisible(messages: PinnedMessage[]): PinnedMessage[] {
  return messages.filter((message) => !sessionDismissedIds.has(message.id));
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * usePinnedNotifications — state for the sidebar floating banner.
 *
 * Loads the cached list from the Go backend on mount, then follows the
 * `notification:pinned` event that PullPinnedNotifications publishes after
 * every reconnect / pinned push / revoke.
 */
export function usePinnedNotifications(): {
  messages: PinnedMessage[];
  /** Messages still visible for this session (dismissed ones removed). */
  visibleMessages: PinnedMessage[];
  /** Dismiss every currently visible message until the app restarts. */
  dismissAll: () => void;
} {
  const [messages, setMessages] = useState<PinnedMessage[]>([]);
  const [dismissalEpoch, setDismissalEpoch] = useState(0);
  const mountedRef = useRef(true);
  // The `notification:pinned` event is always newer than the mount-time
  // binding read (the cache it reads may predate a revoke that just landed).
  // Once an event arrives, the stale binding resolution must not overwrite it.
  const eventSeenRef = useRef(false);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  useEffect(() => {
    const unsubscribe = EventsOn("notification:pinned", (payload: unknown) => {
      if (!mountedRef.current) return;
      eventSeenRef.current = true;
      setMessages(normalizePinnedList(payload));
    });
    return () => {
      if (typeof unsubscribe === "function") unsubscribe();
    };
  }, []);

  useEffect(() => {
    const binding = getAppBinding("GetPinnedNotifications");
    if (!binding) return;
    binding()
      .then((raw: unknown) => {
        if (!mountedRef.current || eventSeenRef.current) return;
        const list = normalizePinnedList(raw);
        if (list.length > 0) setMessages(list);
      })
      .catch(() => { /* backend not ready yet; the event will refresh it */ });
  }, []);

  const dismissAll = useCallback(() => {
    dismissAllForSession(messages.map((message) => message.id));
    setDismissalEpoch((epoch) => epoch + 1);
  }, [messages]);

  const visibleMessages = useMemo(
    // dismissalEpoch invalidates the memo after every dismissal.
    () => filterVisible(messages),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [messages, dismissalEpoch],
  );

  return { messages, visibleMessages, dismissAll };
}
