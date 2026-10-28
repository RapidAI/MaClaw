// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useShareActions } from "../AssistantTitleBarNotifications.shareActions";

type AppBindings = Record<string, (arg: string) => Promise<unknown>>;

function installBindings(bindings: AppBindings) {
    (window as unknown as { go?: { main?: { App?: AppBindings } } }).go = { main: { App: bindings } };
}

afterEach(() => {
    delete (window as unknown as { go?: unknown }).go;
    vi.restoreAllMocks();
});

describe("useShareActions", () => {
    it("opens a shared document and dismisses the notice", async () => {
        const open = vi.fn().mockResolvedValue(undefined);
        installBindings({ OpenFileCompanionShareFromNotification: open });
        const dismiss = vi.fn();
        const { result } = renderHook(() => useShareActions("zh-Hans", dismiss));

        await act(async () => { await result.current.openSharedDocument("tok_1"); });

        expect(open).toHaveBeenCalledWith("tok_1");
        expect(dismiss).toHaveBeenCalledTimes(1);
        expect(result.current.shareOpenError).toBe("");
        expect(result.current.shareOpenBusy).toBe(false);
    });

    it("maps a password-protected share failure to actionable copy", async () => {
        const open = vi.fn().mockRejectedValue(new Error("需要分享密码"));
        installBindings({ OpenFileCompanionShareFromNotification: open });
        const dismiss = vi.fn();
        const { result } = renderHook(() => useShareActions("zh-Hans", dismiss));

        await act(async () => { await result.current.openSharedDocument("tok_1"); });

        expect(result.current.shareOpenError).toContain("导入分享");
        // A failed open leaves the notice in place so the user can retry.
        expect(dismiss).not.toHaveBeenCalled();
        expect(result.current.shareOpenBusy).toBe(false);
    });

    it("reports a stale webview that lacks the binding", async () => {
        installBindings({});
        const dismiss = vi.fn();
        const { result } = renderHook(() => useShareActions("en", dismiss));

        await act(async () => { await result.current.openSharedDocument("tok_1"); });

        expect(result.current.shareOpenError).toContain("update");
        expect(dismiss).not.toHaveBeenCalled();
    });

    it("accepts and declines a host transfer through the matching binding", async () => {
        const accept = vi.fn().mockResolvedValue(undefined);
        const decline = vi.fn().mockResolvedValue(undefined);
        installBindings({ AcceptFileCompanionShareHost: accept, DeclineFileCompanionShareHost: decline });
        const dismiss = vi.fn();
        const { result } = renderHook(() => useShareActions("zh-Hans", dismiss));

        await act(async () => { await result.current.handleHostTransfer("tok_a", "accept"); });
        expect(accept).toHaveBeenCalledWith("tok_a");
        expect(decline).not.toHaveBeenCalled();

        await act(async () => { await result.current.handleHostTransfer("tok_d", "decline"); });
        expect(decline).toHaveBeenCalledWith("tok_d");
        expect(dismiss).toHaveBeenCalledTimes(2);
    });

    it("explains a refused accept when the nominee lost access", async () => {
        const accept = vi.fn().mockRejectedValue(new Error("你已不在这份文档的分享范围里"));
        installBindings({ AcceptFileCompanionShareHost: accept });
        const dismiss = vi.fn();
        const { result } = renderHook(() => useShareActions("en", dismiss));

        await act(async () => { await result.current.handleHostTransfer("tok_x", "accept"); });

        expect(result.current.hostTransferError).toContain("no longer have access");
        expect(dismiss).not.toHaveBeenCalled();
    });

    it("ignores a second click while the first call is still in flight", async () => {
        let release: () => void = () => {};
        const open = vi.fn(() => new Promise<void>((resolve) => { release = resolve; }));
        installBindings({ OpenFileCompanionShareFromNotification: open });
        const dismiss = vi.fn();
        const { result } = renderHook(() => useShareActions("zh-Hans", dismiss));

        let first: Promise<void> = Promise.resolve();
        act(() => { first = result.current.openSharedDocument("tok_1"); });
        await act(async () => { await result.current.openSharedDocument("tok_1"); });
        expect(open).toHaveBeenCalledTimes(1);

        await act(async () => { release(); await first; });
        expect(dismiss).toHaveBeenCalledTimes(1);
    });

    it("ignores an empty token", async () => {
        const open = vi.fn().mockResolvedValue(undefined);
        installBindings({ OpenFileCompanionShareFromNotification: open });
        const dismiss = vi.fn();
        const { result } = renderHook(() => useShareActions("zh-Hans", dismiss));

        await act(async () => { await result.current.openSharedDocument(""); });

        expect(open).not.toHaveBeenCalled();
        expect(dismiss).not.toHaveBeenCalled();
    });

    it("clears both errors together", async () => {
        installBindings({
            OpenFileCompanionShareFromNotification: vi.fn().mockRejectedValue(new Error("分享链接不存在")),
            AcceptFileCompanionShareHost: vi.fn().mockRejectedValue(new Error("已过期")),
        });
        const { result } = renderHook(() => useShareActions("zh-Hans", vi.fn()));

        await act(async () => { await result.current.openSharedDocument("tok_1"); });
        await act(async () => { await result.current.handleHostTransfer("tok_a", "accept"); });
        expect(result.current.shareOpenError).not.toBe("");
        expect(result.current.hostTransferError).not.toBe("");

        act(() => { result.current.resetShareErrors(); });
        expect(result.current.shareOpenError).toBe("");
        expect(result.current.hostTransferError).toBe("");
    });
});
