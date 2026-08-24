import { useState, useEffect, useCallback, useRef } from "react";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/**
 * AdminNotification mirrors the ClientNotification struct from the Go backend.
 */
export interface AdminNotification {
  id: string;
  title: string;
  content: string; // Markdown
  category:
    | "system_announcement"
    | "feature_update"
    | "security_alert"
    | "maintenance"
    | "custom";
  priority: "normal" | "important" | "urgent";
  is_read: boolean;
  created_at: string; // ISO8601
}

interface NotificationState {
  notifications: AdminNotification[];
  unreadCount: number;
  panelOpen: boolean;
  categoryFilter: string | null;
}

export interface UseNotificationsReturn extends NotificationState {
  /** Badge display count: min(unreadCount, 10) */
  displayCount: number;
  /** Whether the bell icon should animate (unreadCount > 0) */
  shouldAnimate: boolean;
  /** Toggle the notification panel open/closed */
  togglePanel: () => void;
  /** Set category filter (null = show all) */
  setCategoryFilter: (category: string | null) => void;
  /** Mark a single notification as read */
  markRead: (notificationId: string) => void;
  /** Mark all notifications as read */
  markAllRead: () => void;
  /** The latest urgent notification for toast display (consumed once read) */
  urgentToast: AdminNotification | null;
  /** Dismiss the urgent toast */
  dismissUrgentToast: () => void;
}

// ---------------------------------------------------------------------------
// Go Binding Wrappers
// ---------------------------------------------------------------------------

/**
 * Calls the Go binding `App.GetUnreadNotifications()`.
 * Returns the cached unread notification list from the backend.
 */
function getAppBinding(name: string): ((...args: any[]) => Promise<any>) | null {
  const appBindings = (window as any)?.go?.main?.App as
    | Record<string, ((...args: any[]) => Promise<any>) | undefined>
    | undefined;
  const binding = appBindings?.[name];
  return typeof binding === "function" ? binding : null;
}

function callGetUnreadNotifications(): Promise<AdminNotification[]> {
  const binding = getAppBinding("GetUnreadNotifications");
  return binding ? binding() : Promise.resolve([]);
}

/**
 * Calls the Go binding `App.GetUnreadCount()`.
 */
function callGetUnreadCount(): Promise<number> {
  const binding = getAppBinding("GetUnreadCount");
  return binding ? binding() : Promise.resolve(0);
}

/**
 * Calls the Go binding `App.MarkNotificationRead(notificationID)`.
 */
function callMarkNotificationRead(notificationID: string): Promise<void> {
  const binding = getAppBinding("MarkNotificationRead");
  return binding ? binding(notificationID) : Promise.resolve();
}

/**
 * Calls the Go binding `App.MarkAllNotificationsRead()`.
 */
function callMarkAllNotificationsRead(): Promise<void> {
  const binding = getAppBinding("MarkAllNotificationsRead");
  return binding ? binding() : Promise.resolve();
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function asNotificationId(value: unknown): string {
  if (typeof value === "string") return value;
  if (value && typeof value === "object") {
    const raw = value as { id?: unknown; ID?: unknown };
    return asString(raw.id) || asString(raw.ID);
  }
  return "";
}

function normalizeNotification(raw: unknown): AdminNotification | null {
  if (!raw || typeof raw !== "object") return null;
  const record = raw as Record<string, unknown>;
  const id = asString(record.id) || asString(record.ID);
  if (!id) return null;
  const category = asString(record.category) || asString(record.Category) || "custom";
  const priority = asString(record.priority) || asString(record.Priority) || "normal";
  return {
    id,
    title: asString(record.title) || asString(record.Title),
    content: asString(record.content) || asString(record.Content),
    category: category as AdminNotification["category"],
    priority: priority as AdminNotification["priority"],
    is_read: Boolean(record.is_read ?? record.IsRead),
    created_at: asString(record.created_at) || asString(record.CreatedAt),
  };
}

function normalizeNotificationList(raw: unknown): AdminNotification[] {
  if (!Array.isArray(raw)) return [];
  const list: AdminNotification[] = [];
  for (const item of raw) {
    const notice = normalizeNotification(item);
    if (notice) list.push(notice);
  }
  return list;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * useNotifications — state management hook for the notification system.
 *
 * Listens to backend events:
 * - `notification:new` — a new notification was pushed
 * - `notification:revoke` — a notification was revoked (removed)
 * - `notification:sync` — bulk sync after reconnect (replaces list)
 * - `notification:urgent-toast` — an urgent notification requiring toast display
 *
 * On mount, loads unread notifications from the Go backend cache.
 *
 * Validates: Requirements FR-4
 */
export function useNotifications(): UseNotificationsReturn {
  const [state, setState] = useState<NotificationState>({
    notifications: [],
    unreadCount: 0,
    panelOpen: false,
    categoryFilter: null,
  });

  const [urgentToast, setUrgentToast] = useState<AdminNotification | null>(
    null
  );

  // Ref to track whether initial load has been performed.
  const initialLoadDone = useRef(false);
  // Ref to track mount state — prevents setState on unmounted component.
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  // -------------------------------------------------------------------------
  // Initial load
  // -------------------------------------------------------------------------

  const loadUnread = useCallback(async () => {
    try {
      const notifications = await callGetUnreadNotifications();
      if (!mountedRef.current) return;
      const list = normalizeNotificationList(notifications);
      const unreadCount = list.filter((n) => !n.is_read).length;
      setState((prev) => ({
        ...prev,
        notifications: list,
        unreadCount,
      }));
    } catch (err) {
      // Silently fail — backend may not be ready yet. Will sync on next event.
      console.warn("[useNotifications] loadUnread failed:", err);
    }
  }, []);

  useEffect(() => {
    if (initialLoadDone.current) return;
    initialLoadDone.current = true;

    loadUnread();
  }, [loadUnread]);

  // -------------------------------------------------------------------------
  // Event handlers
  // -------------------------------------------------------------------------

  const handleNew = useCallback((notification: AdminNotification) => {
    const notice = normalizeNotification(notification);
    if (!notice) return;
    setState((prev) => {
      // Deduplicate
      const filtered = prev.notifications.filter(
        (n) => n.id !== notice.id
      );
      // Prepend (newest first), cap at 10
      const updated = [notice, ...filtered].slice(0, 10);
      const unreadCount = updated.filter((n) => !n.is_read).length;
      return { ...prev, notifications: updated, unreadCount };
    });
  }, []);

  const handleRevoke = useCallback((notificationId: unknown) => {
    const id = asNotificationId(notificationId);
    if (!id) return;
    setState((prev) => {
      const updated = prev.notifications.filter((n) => n.id !== id);
      const unreadCount = updated.filter((n) => !n.is_read).length;
      return { ...prev, notifications: updated, unreadCount };
    });
    setUrgentToast((prev) => (prev?.id === id ? null : prev));
  }, []);

  const handleSync = useCallback((notifications: AdminNotification[]) => {
    const list = normalizeNotificationList(notifications);
    const capped = list.slice(0, 10);
    const unreadCount = capped.filter((n) => !n.is_read).length;
    setState((prev) => ({
      ...prev,
      notifications: capped,
      unreadCount,
    }));
    setUrgentToast((prev) =>
      prev && !capped.some((item) => item.id === prev.id) ? null : prev
    );
  }, []);

  const handleUrgentToast = useCallback((notification: AdminNotification) => {
    const notice = normalizeNotification(notification);
    if (!notice) return;
    setUrgentToast(notice);
  }, []);

  // -------------------------------------------------------------------------
  // Event subscriptions
  // -------------------------------------------------------------------------

  useEffect(() => {
    const unsub1 = EventsOn("notification:new", handleNew);
    const unsub2 = EventsOn("notification:revoke", handleRevoke);
    const unsub3 = EventsOn("notification:sync", handleSync);
    const unsub4 = EventsOn("notification:urgent-toast", handleUrgentToast);

    return () => {
      if (typeof unsub1 === "function") unsub1();
      else EventsOff("notification:new");

      if (typeof unsub2 === "function") unsub2();
      else EventsOff("notification:revoke");

      if (typeof unsub3 === "function") unsub3();
      else EventsOff("notification:sync");

      if (typeof unsub4 === "function") unsub4();
      else EventsOff("notification:urgent-toast");
    };
  }, [handleNew, handleRevoke, handleSync, handleUrgentToast]);

  // -------------------------------------------------------------------------
  // Actions
  // -------------------------------------------------------------------------

  const togglePanel = useCallback(() => {
    setState((prev) => ({ ...prev, panelOpen: !prev.panelOpen }));
  }, []);

  const setCategoryFilter = useCallback((category: string | null) => {
    setState((prev) => ({ ...prev, categoryFilter: category }));
  }, []);

  const markRead = useCallback((notificationId: string) => {
    if (!notificationId) return;

    // Optimistic local update
    setState((prev) => {
      const updated = prev.notifications.map((n) =>
        n.id === notificationId ? { ...n, is_read: true } : n
      );
      const unreadCount = updated.filter((n) => !n.is_read).length;
      return { ...prev, notifications: updated, unreadCount };
    });

    // Call backend (fire-and-forget with error logging)
    callMarkNotificationRead(notificationId).catch((err) => {
      console.warn("[useNotifications] markRead failed:", err);
    });
    setUrgentToast((prev) => (prev?.id === notificationId ? null : prev));
  }, []);

  const markAllRead = useCallback(() => {
    // Optimistic local update
    setState((prev) => {
      const updated = prev.notifications.map((n) => ({
        ...n,
        is_read: true,
      }));
      return { ...prev, notifications: updated, unreadCount: 0 };
    });

    // Call backend
    callMarkAllNotificationsRead().catch((err) => {
      console.warn("[useNotifications] markAllRead failed:", err);
    });
    setUrgentToast(null);
  }, []);

  const dismissUrgentToast = useCallback(() => {
    setUrgentToast(null);
  }, []);

  // -------------------------------------------------------------------------
  // Derived values
  // -------------------------------------------------------------------------

  const displayCount = Math.min(state.unreadCount, 10);
  const shouldAnimate = state.unreadCount > 0;

  return {
    ...state,
    displayCount,
    shouldAnimate,
    togglePanel,
    setCategoryFilter,
    markRead,
    markAllRead,
    urgentToast,
    dismissUrgentToast,
  };
}
