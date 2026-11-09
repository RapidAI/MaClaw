// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAssistantLocalDraftSync } from "../useAssistantLocalDraftSync";
import { useNewTaskGuideDraft } from "../useNewTaskGuideDraft";
import { useResizableAssistantInput } from "../useResizableAssistantInput";

vi.mock("../../../../wailsjs/go/main/App", () => ({
    SelectAIAssistantFiles: vi.fn(async () => []),
}));

vi.mock("../usePastedImageAttachments", () => ({
    usePastedImageAttachments: () => ({
        handlePaste: vi.fn(),
        handleDragOver: vi.fn(),
        handleDrop: vi.fn(),
        pendingAttachments: [],
        setPendingAttachments: vi.fn(),
        settlePendingAttachments: vi.fn(async () => []),
    }),
}));

afterEach(() => {
    vi.useRealTimers();
});

describe("assistant input draft synchronization", () => {
    it("cancels a pending typed value when an immediate clear wins", () => {
        vi.useFakeTimers();
        const setDraftInputValue = vi.fn();
        const { result } = renderHook(() => useAssistantLocalDraftSync(setDraftInputValue));

        act(() => {
            result.current.syncLocalDraft("stale draft", true);
            result.current.syncLocalDraft("", false);
        });
        act(() => vi.advanceTimersByTime(500));

        expect(setDraftInputValue).toHaveBeenCalledTimes(1);
        expect(setDraftInputValue).toHaveBeenCalledWith("");
    });

    it("flushes pending text immediately before a tab switch", () => {
        vi.useFakeTimers();
        const setDraftInputValue = vi.fn();
        const { result } = renderHook(() => useAssistantLocalDraftSync(setDraftInputValue));

        act(() => result.current.syncLocalDraft("draft before switch", true));
        act(() => result.current.flushLocalDraft());
        act(() => vi.advanceTimersByTime(500));

        expect(setDraftInputValue).toHaveBeenCalledTimes(1);
        expect(setDraftInputValue).toHaveBeenCalledWith("draft before switch");
    });

    it("persists periodically during continuous typing", () => {
        vi.useFakeTimers();
        const setDraftInputValue = vi.fn();
        const { result } = renderHook(() => useAssistantLocalDraftSync(setDraftInputValue));

        for (let index = 0; index < 20; index += 1) {
            act(() => result.current.syncLocalDraft(`draft ${index}`, true));
            act(() => vi.advanceTimersByTime(100));
        }

        expect(setDraftInputValue).toHaveBeenCalledTimes(2);
        expect(setDraftInputValue).toHaveBeenLastCalledWith("draft 19");
    });

    it("flushes the latest pending draft on unmount", () => {
        vi.useFakeTimers();
        const setDraftInputValue = vi.fn();
        const { result, unmount } = renderHook(() => useAssistantLocalDraftSync(setDraftInputValue));

        act(() => result.current.syncLocalDraft("keep this draft", true));
        unmount();

        expect(setDraftInputValue).toHaveBeenCalledTimes(1);
        expect(setDraftInputValue).toHaveBeenCalledWith("keep this draft");
    });

    it("keeps guide input immediate while deferring its parent sync", () => {
        const syncDraftInputValue = vi.fn();
        const { result } = renderHook(() => useNewTaskGuideDraft("", syncDraftInputValue));

        act(() => result.current.updateGuideDraft("guide draft", { deferParent: true }));

        expect(result.current.guideInputValue).toBe("guide draft");
        expect(syncDraftInputValue).toHaveBeenCalledWith("guide draft", true);

        act(() => result.current.clearGuideDraft());

        expect(result.current.guideInputValue).toBe("");
        expect(syncDraftInputValue).toHaveBeenLastCalledWith("", false);
    });
});

describe("assistant input resizing", () => {
    it("resizes explicit targets and honors manual height changes", () => {
        const input = document.createElement("textarea");
        let scrollHeightReads = 0;
        Object.defineProperty(input, "scrollHeight", {
            configurable: true,
            get: () => {
                scrollHeightReads += 1;
                return 180;
            },
        });
        const inputRef = { current: input };
        const { result } = renderHook(() => useResizableAssistantInput(inputRef));
        expect(scrollHeightReads).toBe(1);

        input.value = "next draft";
        act(() => result.current.resizeInput(input));
        expect(scrollHeightReads).toBe(2);
        expect(input.style.height).toBe("120px");

        const handle = document.createElement("div");
        const stack = document.createElement("div");
        const parent = document.createElement("div");
        parent.append(handle, stack);
        act(() => {
            result.current.startInputResize({ currentTarget: handle, clientY: 200, preventDefault: vi.fn() } as never);
            document.dispatchEvent(new MouseEvent("mousemove", { clientY: 100 }));
        });
        expect(input.style.height).toBe("100%");

        act(() => document.dispatchEvent(new MouseEvent("mouseup")));
    });
});
