import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useInputHistoryAutocomplete } from "../useInputHistoryAutocomplete";
import type { KeyboardEvent } from "react";

function fakeKey(
    key: string,
    mods: Partial<Pick<KeyboardEvent, "shiftKey" | "ctrlKey" | "metaKey" | "altKey">> = {},
): KeyboardEvent<HTMLTextAreaElement> {
    return {
        key,
        shiftKey: false,
        ctrlKey: false,
        metaKey: false,
        altKey: false,
        preventDefault: vi.fn(),
        ...mods,
    } as unknown as KeyboardEvent<HTMLTextAreaElement>;
}

describe("useInputHistoryAutocomplete", () => {
    it("opens on prefix match and navigates with arrow keys", () => {
        const applyInputValue = vi.fn();
        const inputRef = { current: document.createElement("textarea") };
        const { result } = renderHook(() => useInputHistoryAutocomplete({
            inputValue: "实现用户",
            submittedPrompts: ["实现用户登录", "实现用户注册功能", "其他"],
            applyInputValue,
            inputRef,
        }));

        expect(result.current.open).toBe(true);
        expect(result.current.matches).toEqual(["实现用户注册功能", "实现用户登录"]);
        expect(result.current.selectedIndex).toBe(0);

        act(() => {
            expect(result.current.handleKeyDown(fakeKey("ArrowDown"))).toBe(true);
        });
        expect(result.current.selectedIndex).toBe(1);

        act(() => {
            expect(result.current.handleKeyDown(fakeKey("Enter"))).toBe(true);
        });
        expect(applyInputValue).toHaveBeenCalledWith("实现用户登录");
        expect(result.current.open).toBe(false);
    });

    it("accepts the selected history item via accept()", () => {
        const applyInputValue = vi.fn();
        const inputRef = { current: document.createElement("textarea") };
        const { result } = renderHook(() => useInputHistoryAutocomplete({
            inputValue: "实现用户",
            submittedPrompts: ["实现用户登录", "实现用户注册功能"],
            applyInputValue,
            inputRef,
        }));

        act(() => {
            expect(result.current.accept(0)).toBe(true);
        });
        expect(applyInputValue).toHaveBeenCalledWith("实现用户注册功能");
        expect(result.current.open).toBe(false);
    });

    it("stays closed after accepting a mid-length item that still has longer supersets", () => {
        const applyInputValue = vi.fn();
        const { result, rerender } = renderHook(
            ({ inputValue }) => useInputHistoryAutocomplete({
                inputValue,
                submittedPrompts: ["hello", "hello world"],
                applyInputValue,
                inputRef: { current: document.createElement("textarea") },
            }),
            { initialProps: { inputValue: "hel" } },
        );
        expect(result.current.open).toBe(true);
        expect(result.current.matches).toEqual(["hello world", "hello"]);

        act(() => {
            // Accept shorter item "hello" (index 1).
            expect(result.current.accept(1)).toBe(true);
        });
        expect(applyInputValue).toHaveBeenCalledWith("hello");

        // Parent applies the accepted value — must not immediately reopen for "hello world".
        rerender({ inputValue: "hello" });
        expect(result.current.open).toBe(false);
        expect(result.current.matches).toEqual(["hello world"]);

        // Further typing reopens supersets.
        rerender({ inputValue: "hello " });
        expect(result.current.open).toBe(true);
        expect(result.current.matches).toEqual(["hello world"]);
    });

    it("stays closed when disabled or empty", () => {
        const { result } = renderHook(() => useInputHistoryAutocomplete({
            inputValue: "实现",
            submittedPrompts: ["实现用户登录"],
            applyInputValue: vi.fn(),
            inputRef: { current: null },
            disabled: true,
        }));
        expect(result.current.open).toBe(false);
        expect(result.current.matches).toEqual([]);
    });

    it("temporary disable does not clear Esc sticky dismiss", () => {
        const { result, rerender } = renderHook(
            ({ disabled, inputValue }) => useInputHistoryAutocomplete({
                inputValue,
                submittedPrompts: ["hello world"],
                applyInputValue: vi.fn(),
                inputRef: { current: null },
                disabled,
            }),
            { initialProps: { disabled: false, inputValue: "hello" } },
        );
        expect(result.current.open).toBe(true);

        act(() => {
            result.current.handleKeyDown(fakeKey("Escape"));
        });
        expect(result.current.open).toBe(false);

        // IME / busy: hide list but keep sticky dismiss.
        rerender({ disabled: true, inputValue: "hello" });
        expect(result.current.open).toBe(false);
        expect(result.current.matches).toEqual([]);

        // Re-enable without draft change → still dismissed.
        rerender({ disabled: false, inputValue: "hello" });
        expect(result.current.open).toBe(false);

        // Draft change → reopen.
        rerender({ disabled: false, inputValue: "hello " });
        expect(result.current.open).toBe(true);
    });

    it("dismisses on Escape without applying and stays closed until draft changes", () => {
        const applyInputValue = vi.fn();
        const { result, rerender } = renderHook(
            ({ inputValue }) => useInputHistoryAutocomplete({
                inputValue,
                submittedPrompts: ["hello world", "hello there"],
                applyInputValue,
                inputRef: { current: null },
            }),
            { initialProps: { inputValue: "hello" } },
        );
        expect(result.current.open).toBe(true);

        act(() => {
            expect(result.current.handleKeyDown(fakeKey("Escape"))).toBe(true);
        });
        expect(result.current.open).toBe(false);
        expect(applyInputValue).not.toHaveBeenCalled();

        // Same match set: parent re-render must not force re-open.
        rerender({ inputValue: "hello" });
        expect(result.current.open).toBe(false);

        // Typing further changes the draft → open again even if suggestions stay similar.
        rerender({ inputValue: "hello " });
        expect(result.current.open).toBe(true);
        expect(result.current.matches).toEqual(["hello there", "hello world"]);
    });

    it("reopens after Esc → clear → retype the same prefix", () => {
        const { result, rerender } = renderHook(
            ({ inputValue }) => useInputHistoryAutocomplete({
                inputValue,
                submittedPrompts: ["hello world"],
                applyInputValue: vi.fn(),
                inputRef: { current: null },
            }),
            { initialProps: { inputValue: "hello" } },
        );
        expect(result.current.open).toBe(true);

        act(() => {
            result.current.handleKeyDown(fakeKey("Escape"));
        });
        expect(result.current.open).toBe(false);

        rerender({ inputValue: "" });
        expect(result.current.open).toBe(false);

        rerender({ inputValue: "hello" });
        expect(result.current.open).toBe(true);
    });

    it("keeps selection when draft grows but suggestion set is unchanged", () => {
        const { result, rerender } = renderHook(
            ({ inputValue }) => useInputHistoryAutocomplete({
                inputValue,
                submittedPrompts: ["hello world", "hello there"],
                applyInputValue: vi.fn(),
                inputRef: { current: null },
            }),
            { initialProps: { inputValue: "hello" } },
        );

        act(() => {
            result.current.handleKeyDown(fakeKey("ArrowDown"));
        });
        expect(result.current.selectedIndex).toBe(1);

        // "hello " still matches both → same match set → keep index.
        rerender({ inputValue: "hello " });
        expect(result.current.open).toBe(true);
        expect(result.current.selectedIndex).toBe(1);
    });

    it("does not intercept Enter / Tab when closed so send can proceed", () => {
        const applyInputValue = vi.fn();
        const { result } = renderHook(() => useInputHistoryAutocomplete({
            inputValue: "no-match",
            submittedPrompts: ["hello world"],
            applyInputValue,
            inputRef: { current: null },
        }));
        expect(result.current.open).toBe(false);
        expect(result.current.handleKeyDown(fakeKey("Enter"))).toBe(false);
        expect(result.current.handleKeyDown(fakeKey("Tab"))).toBe(false);
        expect(applyInputValue).not.toHaveBeenCalled();
    });

    it("wraps selection and clamps out-of-range index", () => {
        const { result } = renderHook(() => useInputHistoryAutocomplete({
            inputValue: "p",
            submittedPrompts: ["pa", "pb"],
            applyInputValue: vi.fn(),
            inputRef: { current: null },
        }));
        expect(result.current.matches).toHaveLength(2);

        act(() => {
            result.current.handleKeyDown(fakeKey("ArrowUp"));
        });
        expect(result.current.selectedIndex).toBe(1);

        act(() => {
            result.current.handleKeyDown(fakeKey("ArrowDown"));
        });
        expect(result.current.selectedIndex).toBe(0);

        act(() => {
            result.current.setSelectedIndex(99);
        });
        expect(result.current.selectedIndex).toBe(1);
    });

    it("ignores Shift/Ctrl/Meta/Alt+Enter so send/newline can fall through", () => {
        const applyInputValue = vi.fn();
        const { result } = renderHook(() => useInputHistoryAutocomplete({
            inputValue: "hello",
            submittedPrompts: ["hello world"],
            applyInputValue,
            inputRef: { current: null },
        }));
        expect(result.current.open).toBe(true);
        expect(result.current.handleKeyDown(fakeKey("Enter", { shiftKey: true }))).toBe(false);
        expect(result.current.handleKeyDown(fakeKey("Enter", { ctrlKey: true }))).toBe(false);
        expect(result.current.handleKeyDown(fakeKey("Enter", { metaKey: true }))).toBe(false);
        expect(result.current.handleKeyDown(fakeKey("Enter", { altKey: true }))).toBe(false);
        expect(applyInputValue).not.toHaveBeenCalled();
    });

    it("accepts plain Tab but lets Shift+Tab fall through", () => {
        const applyInputValue = vi.fn();
        const { result } = renderHook(() => useInputHistoryAutocomplete({
            inputValue: "hello",
            submittedPrompts: ["hello world"],
            applyInputValue,
            inputRef: { current: null },
        }));
        expect(result.current.open).toBe(true);

        expect(result.current.handleKeyDown(fakeKey("Tab", { shiftKey: true }))).toBe(false);
        expect(applyInputValue).not.toHaveBeenCalled();

        act(() => {
            expect(result.current.handleKeyDown(fakeKey("Tab"))).toBe(true);
        });
        expect(applyInputValue).toHaveBeenCalledWith("hello world");
    });
});
