import { afterEach, describe, expect, it, vi } from "vitest";
import { insertTextareaLineBreak, isLineBreakShortcut, isPlainEnter } from "../assistantInputShortcuts";

afterEach(() => {
    vi.unstubAllGlobals();
});

describe("isPlainEnter", () => {
    it("matches only Enter without modifier keys", () => {
        expect(isPlainEnter({ key: "Enter", altKey: false, ctrlKey: false, metaKey: false, shiftKey: false })).toBe(true);
        expect(isPlainEnter({ key: "Enter", altKey: false, ctrlKey: true, metaKey: false, shiftKey: false })).toBe(false);
        expect(isPlainEnter({ key: "Enter", altKey: false, ctrlKey: false, metaKey: true, shiftKey: false })).toBe(false);
        expect(isPlainEnter({ key: "Enter", altKey: false, ctrlKey: false, metaKey: false, shiftKey: true })).toBe(false);
        expect(isPlainEnter({ key: "Enter", altKey: true, ctrlKey: false, metaKey: false, shiftKey: false })).toBe(false);
        expect(isPlainEnter({ key: "Escape", altKey: false, ctrlKey: false, metaKey: false, shiftKey: false })).toBe(false);
    });
});

describe("isLineBreakShortcut", () => {
    it("matches Ctrl/Cmd+Enter but not plain, Alt, or other keys", () => {
        expect(isLineBreakShortcut({ key: "Enter", altKey: false, ctrlKey: true, metaKey: false })).toBe(true);
        expect(isLineBreakShortcut({ key: "Enter", altKey: false, ctrlKey: false, metaKey: true })).toBe(true);
        expect(isLineBreakShortcut({ key: "Enter", altKey: true, ctrlKey: true, metaKey: false })).toBe(false);
        expect(isLineBreakShortcut({ key: "Enter", altKey: false, ctrlKey: false, metaKey: false })).toBe(false);
    });
});

describe("insertTextareaLineBreak", () => {
    it("uses the DOM value, replaces the selection, and restores focus and caret", () => {
        const textarea = document.createElement("textarea");
        document.body.append(textarea);
        textarea.value = "firstlast";
        textarea.setSelectionRange(5, 5);
        const updateValue = vi.fn();
        const afterUpdate = vi.fn();
        let frame: FrameRequestCallback | undefined;
        vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
            frame = callback;
            return 1;
        });

        expect(insertTextareaLineBreak(textarea, updateValue, afterUpdate)).toBe("first\nlast");
        expect(updateValue).toHaveBeenCalledWith("first\nlast");
        expect(afterUpdate).not.toHaveBeenCalled();

        frame?.(0);
        expect(afterUpdate).toHaveBeenCalledOnce();
        expect(document.activeElement).toBe(textarea);
        expect(textarea.selectionStart).toBe(6);
        expect(textarea.selectionEnd).toBe(6);
        textarea.remove();
    });
});
