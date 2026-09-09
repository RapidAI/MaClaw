import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useNotifications } from "./useNotifications";

const { eventHandlers } = vi.hoisted(() => ({
  eventHandlers: new Map<string, Array<(payload?: any) => void>>(),
}));

vi.mock("../../../wailsjs/runtime", () => ({
  EventsOn: vi.fn((event: string, handler: (payload?: any) => void) => {
    const handlers = eventHandlers.get(event) || [];
    handlers.push(handler);
    eventHandlers.set(event, handlers);
    return () =>
      eventHandlers.set(
        event,
        (eventHandlers.get(event) || []).filter((item) => item !== handler)
      );
  }),
  EventsOff: vi.fn((event: string) => eventHandlers.delete(event)),
}));

const unreadNotice = {
  id: "n-sync",
  title: "Hub broadcast",
  content: "hello",
  category: "system_announcement" as const,
  priority: "normal" as const,
  is_read: false,
  created_at: "2026-07-03T00:00:00Z",
};

describe("useNotifications", () => {
  beforeEach(() => {
    eventHandlers.clear();
    (window as any).go = {
      main: {
        App: {
          GetUnreadNotifications: vi.fn().mockResolvedValue([]),
          MarkNotificationRead: vi.fn().mockResolvedValue(undefined),
          MarkAllNotificationsRead: vi.fn().mockResolvedValue(undefined),
        },
      },
    };
  });

  it("updates badge state when backend emits reconnect sync", async () => {
    const { result } = renderHook(() => useNotifications());

    await waitFor(() =>
      expect((window as any).go.main.App.GetUnreadNotifications).toHaveBeenCalled()
    );

    act(() => {
      for (const handler of eventHandlers.get("notification:sync") || []) {
        handler([unreadNotice]);
      }
    });

    expect(result.current.notifications).toHaveLength(1);
    expect(result.current.notifications[0].id).toBe("n-sync");
    expect(result.current.unreadCount).toBe(1);
    expect(result.current.displayCount).toBe(1);
    expect(result.current.shouldAnimate).toBe(true);
  });

  it("deduplicates real-time pushed notifications and tracks urgent toast", async () => {
    const { result } = renderHook(() => useNotifications());

    await waitFor(() =>
      expect((window as any).go.main.App.GetUnreadNotifications).toHaveBeenCalled()
    );

    const urgentNotice = {
      ...unreadNotice,
      id: "n-urgent",
      priority: "urgent" as const,
    };

    act(() => {
      for (const handler of eventHandlers.get("notification:new") || []) {
        handler(urgentNotice);
        handler({ ...urgentNotice, title: "Updated urgent notice" });
      }
      for (const handler of eventHandlers.get("notification:urgent-toast") || []) {
        handler(urgentNotice);
      }
    });

    expect(result.current.notifications).toHaveLength(1);
    expect(result.current.notifications[0].title).toBe("Updated urgent notice");
    expect(result.current.unreadCount).toBe(1);
    expect(result.current.urgentToast?.id).toBe("n-urgent");
  });

  it("clears the urgent toast when that notice is revoked or marked read", async () => {
    const { result } = renderHook(() => useNotifications());

    await waitFor(() =>
      expect((window as any).go.main.App.GetUnreadNotifications).toHaveBeenCalled()
    );

    const urgentNotice = {
      ...unreadNotice,
      id: "n-urgent",
      priority: "urgent" as const,
    };

    act(() => {
      for (const handler of eventHandlers.get("notification:urgent-toast") || []) {
        handler(urgentNotice);
      }
      for (const handler of eventHandlers.get("notification:new") || []) {
        handler(urgentNotice);
      }
    });
    expect(result.current.urgentToast?.id).toBe("n-urgent");

    act(() => {
      result.current.markRead("n-urgent");
    });
    expect(result.current.urgentToast).toBeNull();

    act(() => {
      for (const handler of eventHandlers.get("notification:urgent-toast") || []) {
        handler(urgentNotice);
      }
      for (const handler of eventHandlers.get("notification:revoke") || []) {
        handler("n-urgent");
      }
    });
    expect(result.current.urgentToast).toBeNull();
  });

  it("accepts Wails PascalCase notification payloads", async () => {
    const { result } = renderHook(() => useNotifications());

    await waitFor(() =>
      expect((window as any).go.main.App.GetUnreadNotifications).toHaveBeenCalled()
    );

    act(() => {
      for (const handler of eventHandlers.get("notification:new") || []) {
        handler({
          ID: "n-pascal",
          Title: "Hub broadcast",
          Content: "hello",
          Category: "system_announcement",
          Priority: "urgent",
          IsRead: false,
          CreatedAt: "2026-07-03T00:00:00Z",
        });
      }
      for (const handler of eventHandlers.get("notification:urgent-toast") || []) {
        handler({
          ID: "n-pascal",
          Title: "Hub broadcast",
          Content: "hello",
          Priority: "urgent",
        });
      }
    });

    expect(result.current.notifications[0]?.id).toBe("n-pascal");
    expect(result.current.notifications[0]?.title).toBe("Hub broadcast");
    expect(result.current.urgentToast?.id).toBe("n-pascal");

    act(() => {
      for (const handler of eventHandlers.get("notification:revoke") || []) {
        handler({ ID: "n-pascal" });
      }
    });
    expect(result.current.notifications).toHaveLength(0);
    expect(result.current.urgentToast).toBeNull();
  });
});
