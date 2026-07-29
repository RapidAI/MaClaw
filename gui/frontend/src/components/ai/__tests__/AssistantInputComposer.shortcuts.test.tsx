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

function renderComposer(handleSend = vi.fn()) {
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
        inputValue: "draft",
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
        updateInputValue: vi.fn(),
        voiceInput: {} as AssistantInputComposerProps["voiceInput"],
    };

    render(<AssistantInputComposer {...props} />);
    return handleSend;
}

describe("AssistantInputComposer keyboard shortcuts", () => {
    it("sends on plain Enter but leaves modified Enter combinations to the textarea", () => {
        const handleSend = renderComposer();
        const input = screen.getByRole("textbox");

        expect(fireEvent.keyDown(input, { key: "Enter", ctrlKey: true })).toBe(true);
        expect(fireEvent.keyDown(input, { key: "Enter", metaKey: true })).toBe(true);
        expect(fireEvent.keyDown(input, { key: "Enter", shiftKey: true })).toBe(true);
        expect(fireEvent.keyDown(input, { key: "Enter", altKey: true })).toBe(true);
        expect(handleSend).not.toHaveBeenCalled();

        expect(fireEvent.keyDown(input, { key: "Enter" })).toBe(false);
        expect(handleSend).toHaveBeenCalledTimes(1);
    });
});
