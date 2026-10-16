import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useVEPresence } from "./useVEPresence";

const { eventHandlers } = vi.hoisted(() => ({
    eventHandlers: new Map<string, Array<(payload?: any) => void>>(),
}));

vi.mock("../../wailsjs/runtime", () => ({
    EventsOn: vi.fn((event: string, handler: (payload?: any) => void) => {
        const handlers = eventHandlers.get(event) || [];
        handlers.push(handler);
        eventHandlers.set(event, handlers);
        return () => eventHandlers.set(event, (eventHandlers.get(event) || []).filter((item) => item !== handler));
    }),
    EventsOff: vi.fn((event: string) => eventHandlers.delete(event)),
}));

describe("useVEPresence", () => {
    beforeEach(() => {
        eventHandlers.clear();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("runs one follow-up fetch when a VE event arrives during an in-flight fetch", async () => {
        let resolveFirst: ((value: unknown) => void) | undefined;
        const listVirtualEmployees = vi
            .fn()
            .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }))
            .mockResolvedValueOnce([
                { id: "profile-1", machine_id: "machine-1", online_status: "online" },
            ]);

        renderHook(() => useVEPresence({ hubConfigured: true, listVirtualEmployees }));

        await waitFor(() => expect(listVirtualEmployees).toHaveBeenCalledTimes(1));
        act(() => {
            for (const handler of eventHandlers.get("ve:list_update") || []) handler();
        });
        await act(async () => {
            resolveFirst?.([]);
            await Promise.resolve();
        });

        await waitFor(() => expect(listVirtualEmployees).toHaveBeenCalledTimes(2));
    });

    it("does not run a follow-up fetch after unmount", async () => {
        let resolveFirst: ((value: unknown) => void) | undefined;
        const listVirtualEmployees = vi
            .fn()
            .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }));

        const { unmount } = renderHook(() => useVEPresence({ hubConfigured: true, listVirtualEmployees }));

        await waitFor(() => expect(listVirtualEmployees).toHaveBeenCalledTimes(1));
        act(() => {
            for (const handler of eventHandlers.get("ve:status_change") || []) handler();
        });
        unmount();
        await act(async () => {
            resolveFirst?.([]);
            await Promise.resolve();
        });

        expect(listVirtualEmployees).toHaveBeenCalledTimes(1);
    });

    it("ignores an in-flight result after Hub presence polling is disabled", async () => {
        let resolveFirst: ((value: unknown) => void) | undefined;
        const listVirtualEmployees = vi
            .fn()
            .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }));

        const { result, rerender } = renderHook(
            ({ hubConfigured }) => useVEPresence({ hubConfigured, listVirtualEmployees }),
            { initialProps: { hubConfigured: true } },
        );

        await waitFor(() => expect(listVirtualEmployees).toHaveBeenCalledTimes(1));
        act(() => {
            for (const handler of eventHandlers.get("ve:list_update") || []) handler();
        });
        rerender({ hubConfigured: false });
        await act(async () => {
            resolveFirst?.([
                { id: "profile-1", machine_id: "machine-1", online_status: "online" },
            ]);
            await Promise.resolve();
        });

        expect(result.current.veList).toHaveLength(0);
        expect(listVirtualEmployees).toHaveBeenCalledTimes(1);
    });

    it("clears the last presence list when Hub presence polling is disabled", async () => {
        const listVirtualEmployees = vi.fn().mockResolvedValue([
            { id: "profile-1", machine_id: "machine-1", online_status: "online" },
        ]);

        const { result, rerender } = renderHook(
            ({ hubConfigured }) => useVEPresence({ hubConfigured, listVirtualEmployees }),
            { initialProps: { hubConfigured: true } },
        );

        await waitFor(() => expect(result.current.veList).toHaveLength(1));
        expect(result.current.getStatus("machine-1")).toBe("online");

        rerender({ hubConfigured: false });

        await waitFor(() => expect(result.current.veList).toHaveLength(0));
        expect(result.current.lastFetchAt).toBe(0);
        expect(result.current.getStatus("machine-1")).toBe("unknown");
    });

    it("ignores an older in-flight result after Hub presence polling restarts", async () => {
        let resolveOld: ((value: unknown) => void) | undefined;
        const listVirtualEmployees = vi
            .fn()
            .mockImplementationOnce(() => new Promise((resolve) => { resolveOld = resolve; }))
            .mockResolvedValueOnce([
                { id: "new-profile", machine_id: "new-machine", online_status: "online" },
            ]);

        const { result, rerender } = renderHook(
            ({ hubConfigured }) => useVEPresence({ hubConfigured, listVirtualEmployees }),
            { initialProps: { hubConfigured: true } },
        );

        await waitFor(() => expect(listVirtualEmployees).toHaveBeenCalledTimes(1));
        rerender({ hubConfigured: false });
        rerender({ hubConfigured: true });
        await act(async () => {
            resolveOld?.([
                { id: "old-profile", machine_id: "old-machine", online_status: "online" },
            ]);
            await Promise.resolve();
        });

        await waitFor(() => expect(listVirtualEmployees).toHaveBeenCalledTimes(2));
        await waitFor(() => expect(result.current.veList[0]?.id).toBe("new-profile"));
        expect(result.current.veList[0]?.id).toBe("new-profile");
    });

    it("keeps the last good list and marks presence unknown after a malformed response", async () => {
        const listVirtualEmployees = vi
            .fn()
            .mockResolvedValueOnce([
                { id: "profile-1", machine_id: "machine-1", online_status: "online" },
            ])
            .mockResolvedValueOnce({ employees: [] });

        const { result } = renderHook(() => useVEPresence({ hubConfigured: true, listVirtualEmployees }));

        await waitFor(() => expect(result.current.veList).toHaveLength(1));
        expect(result.current.getStatus("machine-1")).toBe("online");

        act(() => {
            for (const handler of eventHandlers.get("ve:list_update") || []) handler();
        });

        await waitFor(() => expect(listVirtualEmployees).toHaveBeenCalledTimes(2));
        await waitFor(() => expect(result.current.isStale).toBe(true));
        expect(result.current.veList).toHaveLength(1);
        expect(result.current.getStatus("machine-1")).toBe("unknown");
    });

    it("applies lightweight status events before the follow-up list refresh finishes", async () => {
        let resolveRefresh: ((value: unknown) => void) | undefined;
        const listVirtualEmployees = vi
            .fn()
            .mockResolvedValueOnce([
                { id: "profile-1", machine_id: "machine-1", online_status: "online" },
            ])
            .mockImplementationOnce(() => new Promise((resolve) => { resolveRefresh = resolve; }));

        const { result } = renderHook(() => useVEPresence({ hubConfigured: true, listVirtualEmployees }));

        await waitFor(() => expect(result.current.getStatus("machine-1")).toBe("online"));

        act(() => {
            for (const handler of eventHandlers.get("ve:status_change") || []) {
                handler({ ve_id: "ve-machine-1", online_status: "offline" });
            }
        });

        await waitFor(() => expect(result.current.getStatus("machine-1")).toBe("offline"));
        expect(result.current.veList[0]?.online_status).toBe("offline");
        expect(listVirtualEmployees).toHaveBeenCalledTimes(2);

        await act(async () => {
            resolveRefresh?.([
                { id: "profile-1", machine_id: "machine-1", online_status: "offline" },
            ]);
            await Promise.resolve();
        });
    });

    it("clears pending event refresh when polling is disabled before throttle fires", async () => {
        let resolveFirst: ((value: unknown) => void) | undefined;
        const listVirtualEmployees = vi
            .fn()
            .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve; }))
            .mockResolvedValueOnce([
                { id: "fresh-profile", machine_id: "fresh-machine", online_status: "online" },
            ]);

        const { rerender } = renderHook(
            ({ hubConfigured }) => useVEPresence({ hubConfigured, listVirtualEmployees }),
            { initialProps: { hubConfigured: true } },
        );

        await waitFor(() => expect(listVirtualEmployees).toHaveBeenCalledTimes(1));
        vi.useFakeTimers();
        act(() => {
            for (const handler of eventHandlers.get("ve:list_update") || []) handler();
            for (const handler of eventHandlers.get("ve:status_change") || []) handler();
        });
        rerender({ hubConfigured: false });
        await act(async () => {
            resolveFirst?.([]);
            await Promise.resolve();
        });

        rerender({ hubConfigured: true });
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(listVirtualEmployees).toHaveBeenCalledTimes(2);
        await act(async () => {
            await vi.advanceTimersByTimeAsync(1_600);
        });

        expect(listVirtualEmployees).toHaveBeenCalledTimes(2);
    });
});
