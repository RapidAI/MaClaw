import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { usePinnedNotifications } from "./usePinnedNotifications";

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

const permanentPin = {
  id: "pin-permanent",
  title: "Hy4 flash 正式发布",
  content: "Agent 任务又快又省！",
  link_url: "https://example.com/launch",
  button_text: "立即使用",
  pinned_forever: true,
  created_at: "2026-11-01T00:00:00Z",
};

const regularPin = {
  id: "pin-1",
  title: "优化 hubcenter 服务器 CPU 占用",
  content: "已完成",
  link_url: "",
  button_text: "查看详情",
  pinned_forever: false,
  created_at: "2026-11-02T00:00:00Z",
};

function emitPinned(payload: unknown): void {
  for (const handler of eventHandlers.get("notification:pinned") || []) {
    handler(payload);
  }
}

describe("usePinnedNotifications", () => {
  beforeEach(() => {
    eventHandlers.clear();
    (window as any).go = {
      main: {
        App: {
          GetPinnedNotifications: vi.fn().mockResolvedValue([]),
        },
      },
    };
  });

  it("seeds the list from the backend cache and follows notification:pinned", async () => {
    (window as any).go.main.App.GetPinnedNotifications =
      vi.fn().mockResolvedValue([permanentPin, regularPin]);
    const { result } = renderHook(() => usePinnedNotifications());

    await waitFor(() =>
      expect((window as any).go.main.App.GetPinnedNotifications).toHaveBeenCalled()
    );
    await waitFor(() => expect(result.current.messages).toHaveLength(2));

    act(() => {
      emitPinned([permanentPin]);
    });
    expect(result.current.messages).toHaveLength(1);
    expect(result.current.messages[0].id).toBe("pin-permanent");
    expect(result.current.visibleMessages).toHaveLength(1);
  });

  it("dismissAll hides the visible messages for the session, new ids still pop", async () => {
    const { result } = renderHook(() => usePinnedNotifications());
    await waitFor(() =>
      expect((window as any).go.main.App.GetPinnedNotifications).toHaveBeenCalled()
    );

    act(() => {
      emitPinned([permanentPin, regularPin]);
    });
    expect(result.current.visibleMessages).toHaveLength(2);

    act(() => {
      result.current.dismissAll();
    });
    expect(result.current.visibleMessages).toHaveLength(0);

    // A pinned message published later carries a fresh id, so it must
    // still surface even though the banner was closed this session.
    const freshPin = { ...regularPin, id: "pin-2-new", title: "新消息" };
    act(() => {
      emitPinned([permanentPin, regularPin, freshPin]);
    });
    expect(result.current.messages).toHaveLength(3);
    expect(result.current.visibleMessages.map((m) => m.id)).toEqual(["pin-2-new"]);
  });

  it("keeps dismissal across remounts (session scope, not hook scope)", async () => {
    const first = renderHook(() => usePinnedNotifications());
    await waitFor(() =>
      expect((window as any).go.main.App.GetPinnedNotifications).toHaveBeenCalled()
    );
    act(() => {
      emitPinned([permanentPin]);
    });
    act(() => {
      first.result.current.dismissAll();
    });
    expect(first.result.current.visibleMessages).toHaveLength(0);
    first.unmount();

    const second = renderHook(() => usePinnedNotifications());
    await waitFor(() =>
      expect((window as any).go.main.App.GetPinnedNotifications).toHaveBeenCalled()
    );
    // The cache still holds the message; the dismissal set hides it.
    act(() => {
      emitPinned([permanentPin]);
    });
    expect(second.result.current.messages).toHaveLength(1);
    expect(second.result.current.visibleMessages).toHaveLength(0);
  });

  it("a stale binding resolution never overwrites an event that already landed", async () => {
    // The cache was populated with the pin before the frontend mounted; while
    // the binding read is still in flight, a revoke clears the list. The late
    // binding result (still holding the revoked row) must not resurrect it.
    let resolveBinding: ((value: unknown) => void) | null = null;
    (window as any).go.main.App.GetPinnedNotifications = vi.fn().mockImplementation(
      () => new Promise((resolve) => { resolveBinding = resolve; }),
    );
    const { result } = renderHook(() => usePinnedNotifications());

    act(() => {
      emitPinned([]);
    });
    expect(result.current.messages).toHaveLength(0);

    await act(async () => {
      resolveBinding?.([permanentPin]);
      // Flush the promise chain the .then handler runs on.
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(result.current.messages).toHaveLength(0);
  });
});
