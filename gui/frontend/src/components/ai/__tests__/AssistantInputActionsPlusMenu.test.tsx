import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AssistantInputActionsLeft, clampMenuPosition } from "../AssistantInputActions";
import type { Theme } from "../aiAssistantPanelTheme";
import type { UseVoiceInputResult } from "../useVoiceInput";

const theme = {
    text: "#111",
    textMuted: "#666",
    btnColor: "#2f5f98",
    btnBorder: "rgba(47,95,152,0.3)",
    fieldBg: "#fff",
    fieldBorder: "#ddd",
    inputBarBg: "#f8fafc",
    inputBarBorder: "#e2e8f0",
    bg: "#fff",
    errorText: "#b91c1c",
} as Theme;

const voiceInput = {
    state: "idle",
    asrReady: true,
    error: null,
    isSpeaking: false,
    onAudioLevelRef: { current: null },
} as unknown as UseVoiceInputResult;

function renderLeft(overrides: Partial<Parameters<typeof AssistantInputActionsLeft>[0]> = {}) {
    return render(
        <AssistantInputActionsLeft
            browseFile={vi.fn()}
            composeAction={null}
            inputLocked={false}
            lang="zh-Hans"
            onComposeActionChange={vi.fn()}
            onFireSlashCommand={vi.fn()}
            onInsertTemplate={vi.fn()}
            onPlusMenuAction={vi.fn()}
            ready={true}
            theme={theme}
            themeMode="light"
            voiceInput={voiceInput}
            showVoiceInput={false}
            handleVoiceClick={vi.fn()}
            handleVoicePointerDown={vi.fn()}
            handleVoicePointerLeave={vi.fn()}
            finishVoicePointer={vi.fn()}
            attachButtonTestId="ai-attach-button"
            {...overrides}
        />,
    );
}

describe("AssistantInputActionsLeft plus menu", () => {
    it("shows the permission mode selector and reports changes", () => {
        const onPermissionModeChange = vi.fn();
        renderLeft({ permissionMode: "request", onPermissionModeChange });

        const selector = screen.getByTestId("ai-permission-mode") as HTMLSelectElement;
        expect(selector.value).toBe("request");
        expect(screen.getByRole("option", { name: "请求授权" })).toBeTruthy();
        expect(screen.getByRole("option", { name: "完全控制" })).toBeTruthy();

        fireEvent.change(selector, { target: { value: "full" } });
        expect(onPermissionModeChange).toHaveBeenCalledWith("full");
    });

    it("places + before the attachment button and lists iconed commands", () => {
        renderLeft();

        const plus = screen.getByTestId("ai-plus-menu-button");
        const attach = screen.getByTestId("ai-attach-button");
        expect(plus.compareDocumentPosition(attach) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

        fireEvent.click(plus);
        expect(screen.getByTestId("ai-plus-menu")).toBeTruthy();
        expect(screen.getByTestId("ai-plus-menu-new-conversation")).toBeTruthy();
        expect(screen.getByTestId("ai-plus-menu-goal")).toBeTruthy();
        expect(screen.getByTestId("ai-plus-menu-btw")).toBeTruthy();
        expect(screen.getByTestId("ai-plus-menu-loop")).toBeTruthy();
        expect(screen.getByTestId("ai-plus-menu-memory")).toBeTruthy();
        expect(screen.getByTestId("ai-plus-menu-compress")).toBeTruthy();
        expect(screen.queryByTestId("ai-plus-menu-sessions")).toBeNull();
        expect(screen.getByTestId("ai-plus-menu-help")).toBeTruthy();
    });

    it("starts a new conversation via the plus menu action", () => {
        const onPlusMenuAction = vi.fn();
        renderLeft({ onPlusMenuAction });
        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-plus-menu-new-conversation"));
        expect(onPlusMenuAction).toHaveBeenCalledWith("newConversation");
    });

    it("disables new conversation while the agent is busy", () => {
        const onPlusMenuAction = vi.fn();
        renderLeft({ inputLocked: true, onPlusMenuAction });
        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        const item = screen.getByTestId("ai-plus-menu-new-conversation") as HTMLButtonElement;
        expect(item.disabled).toBe(true);
        fireEvent.click(item);
        expect(onPlusMenuAction).not.toHaveBeenCalled();
    });

    it("selects goal and btw compose modes", () => {
        const onComposeActionChange = vi.fn();
        renderLeft({ onComposeActionChange });

        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-plus-menu-goal"));
        expect(onComposeActionChange).toHaveBeenCalledWith("goal");

        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-plus-menu-btw"));
        expect(onComposeActionChange).toHaveBeenCalledWith("btw");
    });

    it("inserts the loop template", () => {
        const onInsertTemplate = vi.fn();
        renderLeft({ onInsertTemplate });

        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-plus-menu-loop"));
        expect(onInsertTemplate).toHaveBeenCalledWith(expect.stringContaining("/loop "));
    });

    it("fires status slash commands immediately", () => {
        const onFireSlashCommand = vi.fn();
        renderLeft({ onFireSlashCommand });

        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-plus-menu-memory"));
        expect(onFireSlashCommand).toHaveBeenCalledWith("/memory");

        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-plus-menu-help"));
        expect(onFireSlashCommand).toHaveBeenCalledWith("/help");
    });

    it("shows a dismissible chip for active compose mode", () => {
        const onComposeActionChange = vi.fn();
        renderLeft({ composeAction: "btw", onComposeActionChange, themeMode: "dark" });

        fireEvent.click(screen.getByTestId("ai-compose-btw-chip"));
        expect(onComposeActionChange).toHaveBeenCalledWith(null);
    });

    it("keeps the + menu available while the agent is busy (for /btw side queries)", () => {
        renderLeft({ inputLocked: true });
        const plus = screen.getByTestId("ai-plus-menu-button") as HTMLButtonElement;
        expect(plus.disabled).toBe(false);
        fireEvent.click(plus);
        expect(screen.getByTestId("ai-plus-menu-btw")).toBeTruthy();
    });

    it("hides fire items when the fire callback is not wired", () => {
        renderLeft({ onFireSlashCommand: undefined });
        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        expect(screen.getByTestId("ai-plus-menu-goal")).toBeTruthy();
        expect(screen.queryByTestId("ai-plus-menu-memory")).toBeNull();
    });

    it("supports arrow-key navigation and Escape restore focus", () => {
        renderLeft();
        const plus = screen.getByTestId("ai-plus-menu-button");
        fireEvent.click(plus);
        const goal = screen.getByTestId("ai-plus-menu-goal");
        const btw = screen.getByTestId("ai-plus-menu-btw");
        goal.focus();
        fireEvent.keyDown(document, { key: "ArrowDown" });
        expect(document.activeElement).toBe(btw);
        fireEvent.keyDown(document, { key: "ArrowUp" });
        expect(document.activeElement).toBe(goal);
        fireEvent.keyDown(document, { key: "Escape" });
        expect(screen.queryByTestId("ai-plus-menu")).toBeNull();
        expect(document.activeElement).toBe(plus);
    });

    it("closes the menu on Tab without trapping focus", () => {
        renderLeft();
        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        expect(screen.getByTestId("ai-plus-menu")).toBeTruthy();
        fireEvent.keyDown(document, { key: "Tab" });
        expect(screen.queryByTestId("ai-plus-menu")).toBeNull();
    });
});

describe("clampMenuPosition", () => {
    it("opens upward when there is room above the trigger", () => {
        const pos = clampMenuPosition(
            { left: 40, top: 400, bottom: 424, width: 24 },
            { width: 1000, height: 800 },
        );
        expect(pos.openUp).toBe(true);
        expect(pos.top).toBe(394);
        expect(pos.left).toBe(40);
    });

    it("flips downward near the top edge and clamps horizontally", () => {
        const pos = clampMenuPosition(
            { left: 990, top: 20, bottom: 44, width: 24 },
            { width: 1000, height: 800 },
        );
        expect(pos.openUp).toBe(false);
        expect(pos.top).toBe(50);
        // 1000 - 176 - 8 = 816
        expect(pos.left).toBe(816);
    });

    it("keeps left >= pad on very narrow viewports", () => {
        const pos = clampMenuPosition(
            { left: 40, top: 400, bottom: 424, width: 24 },
            { width: 120, height: 800 },
        );
        expect(pos.left).toBe(8);
    });
});
