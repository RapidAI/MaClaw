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

    render(<AssistantInputComposer {...props} />);
    return { handleSend, rememberHistoryEdit, resizeInput, updateInputValue };
}

describe("AssistantInputComposer keyboard shortcuts", () => {
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
