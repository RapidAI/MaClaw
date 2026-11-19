// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AssistantInputComposer } from "../AssistantInputComposer";
import type { AssistantInputComposerProps } from "../AssistantInputComposerTypes";
import { overlayTheme } from "../aiAssistantPanelTheme";

vi.mock("../AssistantAttachmentsStrip", () => ({
    AssistantAttachmentsStrip: () => null,
}));

vi.mock("../AssistantInputActions", () => ({
    AssistantInputActionsLeft: () => null,
    AssistantInputActionsRight: () => null,
}));

vi.mock("../InputHistoryAutocomplete", () => ({
    InputHistoryAutocomplete: () => null,
}));

vi.mock("../MemoryUsageRing", () => ({
    MemoryUsageRing: () => null,
}));

function renderComposer(inputValue = "draft", handleSend = vi.fn()) {
    const updateInputValue = vi.fn();
    const rememberHistoryEdit = vi.fn();
    const resizeInput = vi.fn();
    const props: AssistantInputComposerProps = {
        browseFile: vi.fn(),
        canSend: true,
        cancelPending: false,
        exitHistoryBrowsing: vi.fn(() => false),
        finishVoicePointer: vi.fn(),
        handleCancel: vi.fn(),
        handleClearInput: vi.fn(),
        handleDragOver: vi.fn(),
        handleDrop: vi.fn(),
        handlePaste: vi.fn(),
        handleSend,
        handleVoiceClick: vi.fn(),
        handleVoicePointerDown: vi.fn(),
        handleVoicePointerLeave: vi.fn(),
        inputAreaHeight: null,
        inputLocked: false,
        inputRef: { current: null },
        inputValue,
        inline: false,
        isBusy: false,
        isSelectionCollapsedAtBoundary: vi.fn(() => false),
        lang: "zh-Hans",
        pendingAttachments: [],
        placeholderText: "Ask AI",
        ready: true,
        recallHistory: vi.fn(() => false),
        rememberHistoryEdit,
        resizeInput,
        selectedFilePaths: [],
        setPendingAttachments: vi.fn(),
        showBusySpinner: false,
        showMemoryUsage: false,
        showVoiceInput: false,
        theme: overlayTheme,
        themeMode: "dark",
        updateInputValue,
        voiceInput: {} as AssistantInputComposerProps["voiceInput"],
    };

    const { rerender } = render(<AssistantInputComposer {...props} />);
    return {
        handleSend,
        rememberHistoryEdit,
        resizeInput,
        updateInputValue,
        rerenderWithValue: (nextValue: string) => rerender(<AssistantInputComposer {...props} inputValue={nextValue} />),
    };
}

describe("AssistantInputComposer toolbar end slot", () => {
    it("pins toolbar end content in the gap before send", () => {
        const updateInputValue = vi.fn();
        const props: AssistantInputComposerProps = {
            browseFile: vi.fn(),
            canSend: true,
            cancelPending: false,
            exitHistoryBrowsing: vi.fn(() => false),
            finishVoicePointer: vi.fn(),
            handleCancel: vi.fn(),
            handleClearInput: vi.fn(),
            handleDragOver: vi.fn(),
            handleDrop: vi.fn(),
            handlePaste: vi.fn(),
            handleSend: vi.fn(),
            handleVoiceClick: vi.fn(),
            handleVoicePointerDown: vi.fn(),
            handleVoicePointerLeave: vi.fn(),
            inputAreaHeight: null,
            inputLocked: false,
            inputRef: { current: null },
            inputValue: "",
            inline: false,
            isBusy: false,
            isSelectionCollapsedAtBoundary: vi.fn(() => false),
            lang: "zh-Hans",
            pendingAttachments: [],
            placeholderText: "Ask AI",
            ready: true,
            recallHistory: vi.fn(() => false),
            rememberHistoryEdit: vi.fn(),
            resizeInput: vi.fn(),
            selectedFilePaths: [],
            setPendingAttachments: vi.fn(),
            showBusySpinner: false,
            showMemoryUsage: false,
            showVoiceInput: false,
            theme: overlayTheme,
            themeMode: "dark",
            toolbarEndContent: <button type="button" data-testid="composer-model-chip">助手 · auto</button>,
            updateInputValue,
            voiceInput: {} as AssistantInputComposerProps["voiceInput"],
        };
        render(<AssistantInputComposer {...props} />);
        const end = screen.getByTestId("ai-input-toolbar-end");
        expect(end.style.marginLeft).toBe("auto");
        expect(screen.getByTestId("composer-model-chip").textContent).toContain("auto");
        const toolbar = screen.getByTestId("ai-input-toolbar");
        const right = toolbar.lastElementChild;
        expect(right?.contains(end)).toBe(false);
        expect(end.compareDocumentPosition(right!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });
});

describe("AssistantInputComposer keyboard shortcuts", () => {
    it("resizes only after the controlled input value commits", () => {
        const { resizeInput, rerenderWithValue } = renderComposer("draft");
        const input = screen.getByRole("textbox") as HTMLTextAreaElement;
        expect(resizeInput).toHaveBeenCalledWith(input);
        resizeInput.mockClear();

        fireEvent.change(input, { target: { value: "next draft" } });
        expect(resizeInput).not.toHaveBeenCalled();

        rerenderWithValue("next draft");
        expect(resizeInput).toHaveBeenCalledTimes(1);
        expect(resizeInput).toHaveBeenCalledWith(input);
    });

    it("sends on plain Enter and never sends on modified Enter combinations", () => {
        const { handleSend } = renderComposer();
        const input = screen.getByRole("textbox");

        expect(fireEvent.keyDown(input, { key: "Enter", ctrlKey: true })).toBe(false);
        expect(fireEvent.keyDown(input, { key: "Enter", metaKey: true })).toBe(false);
        expect(fireEvent.keyDown(input, { key: "Enter", shiftKey: true })).toBe(true);
        expect(fireEvent.keyDown(input, { key: "Enter", altKey: true })).toBe(true);
        expect(handleSend).not.toHaveBeenCalled();

        expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(false);
        expect(handleSend).toHaveBeenCalledTimes(1);
    });

    it("inserts a newline at the selection for Ctrl/Cmd+Enter", () => {
        const { handleSend, rememberHistoryEdit, updateInputValue } = renderComposer("firstlast");
        const input = screen.getByRole("textbox") as HTMLTextAreaElement;
        input.setSelectionRange(5, 5);

        expect(fireEvent.keyDown(input, { key: "Enter", ctrlKey: true })).toBe(false);
        expect(updateInputValue).toHaveBeenLastCalledWith("first\nlast");
        expect(rememberHistoryEdit).toHaveBeenLastCalledWith("first\nlast");
        expect(handleSend).not.toHaveBeenCalled();

        input.setSelectionRange(0, 5);
        expect(fireEvent.keyDown(input, { key: "Enter", metaKey: true })).toBe(false);
        expect(updateInputValue).toHaveBeenLastCalledWith("\nlast");
    });

    it("uses the textarea's latest value when React has not rendered it yet", () => {
        const { updateInputValue } = renderComposer("stale");
        const input = screen.getByRole("textbox") as HTMLTextAreaElement;
        input.value = "latest";
        input.setSelectionRange(3, 3);

        fireEvent.keyDown(input, { key: "Enter", ctrlKey: true });

        expect(updateInputValue).toHaveBeenLastCalledWith("lat\nest");
    });
});

describe("AssistantInputComposer IME composition", () => {
    it("buffers composing keystrokes locally and syncs upstream once on compositionend", () => {
        const { updateInputValue, rememberHistoryEdit } = renderComposer("");
        const input = screen.getByRole("textbox") as HTMLTextAreaElement;

        fireEvent.compositionStart(input);
        fireEvent.change(input, { target: { value: "w" } });
        fireEvent.change(input, { target: { value: "wtt" } });
        expect(updateInputValue).not.toHaveBeenCalled();
        expect(rememberHistoryEdit).not.toHaveBeenCalled();
        // The controlled value tracks the DOM mid-composition so React's
        // controlled-input restore never writes a stale value back.
        expect(input.value).toBe("wtt");

        input.value = "汉";
        fireEvent.compositionEnd(input);

        expect(updateInputValue).toHaveBeenCalledTimes(1);
        expect(updateInputValue).toHaveBeenLastCalledWith("汉", { deferParent: true });
        expect(rememberHistoryEdit).toHaveBeenLastCalledWith("汉");
    });

    it("flushes through the normal path when a non-composing input arrives without compositionend", () => {
        const { updateInputValue } = renderComposer("");
        const input = screen.getByRole("textbox") as HTMLTextAreaElement;

        fireEvent.compositionStart(input);
        fireEvent.change(input, { target: { value: "wt" } });
        expect(updateInputValue).not.toHaveBeenCalled();

        // A committed (non-composing) input after a dropped compositionend.
        // setNativeValue keeps React's value tracker stale so the input event
        // actually dispatches onChange, like a real DOM mutation.
        const nativeSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!;
        nativeSetter.call(input, "汉");
        const committed = new Event("input", { bubbles: true });
        Object.defineProperty(committed, "isComposing", { value: false });
        fireEvent(input, committed);

        expect(updateInputValue).toHaveBeenLastCalledWith("汉", { deferParent: true });
    });

    it("buffers when the first composing input arrives before compositionstart", () => {
        const { updateInputValue } = renderComposer("");
        const input = screen.getByRole("textbox") as HTMLTextAreaElement;

        // Some WebViews deliver the first composing input before
        // compositionstart; it must seed the buffer, not flush upstream.
        const nativeSetter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!;
        nativeSetter.call(input, "w");
        const composing = new Event("input", { bubbles: true });
        Object.defineProperty(composing, "isComposing", { value: true });
        fireEvent(input, composing);

        expect(updateInputValue).not.toHaveBeenCalled();
        expect(input.value).toBe("w");

        nativeSetter.call(input, "汉");
        fireEvent.compositionEnd(input);
        expect(updateInputValue).toHaveBeenLastCalledWith("汉", { deferParent: true });
    });

    it("does not send on Enter while composing", () => {
        const { handleSend } = renderComposer("");
        const input = screen.getByRole("textbox");

        fireEvent.compositionStart(input);
        expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(true);
        expect(handleSend).not.toHaveBeenCalled();
    });
});
