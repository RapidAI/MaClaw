import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
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
            onPermissionModeChange={vi.fn()}
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
    it("shows iconed permission modes and reports changes", () => {
        const onPermissionModeChange = vi.fn();
        renderLeft({ permissionMode: "request", onPermissionModeChange });

        const selector = screen.getByTestId("ai-permission-mode");
        expect(selector.className).toContain("ai-permission-mode-trigger");
        expect(selector.textContent).toContain("请求授权");
        fireEvent.click(selector);
        expect(screen.getByRole("menuitemradio", { name: "请求授权" })).toBeTruthy();
        const fullControl = screen.getByRole("menuitemradio", { name: "完全控制" });
        expect(fullControl.className).toContain("ai-permission-mode-item");
        expect((fullControl as HTMLElement).style.color).toBe("rgb(185, 28, 28)");

        fireEvent.click(fullControl);
        // Full control requires an explicit risk acknowledgment first.
        expect(onPermissionModeChange).not.toHaveBeenCalled();
        const accept = screen.getByTestId("ai-full-control-confirm-accept") as HTMLButtonElement;
        expect(accept.disabled).toBe(true);
        fireEvent.click(screen.getByRole("checkbox"));
        expect((screen.getByTestId("ai-full-control-confirm-accept") as HTMLButtonElement).disabled).toBe(false);
        fireEvent.click(screen.getByTestId("ai-full-control-confirm-accept"));
        expect(onPermissionModeChange).toHaveBeenCalledWith("full");
        expect(document.activeElement).toBe(selector);
    });

    it("keeps the current mode when the full-control confirmation is cancelled", () => {
        const onPermissionModeChange = vi.fn();
        renderLeft({ permissionMode: "request", onPermissionModeChange });

        const selector = screen.getByTestId("ai-permission-mode");
        fireEvent.click(selector);
        fireEvent.click(screen.getByRole("menuitemradio", { name: "完全控制" }));
        expect(screen.getByTestId("ai-full-control-confirm-dialog")).toBeTruthy();

        fireEvent.click(screen.getByTestId("ai-full-control-confirm-cancel"));
        expect(onPermissionModeChange).not.toHaveBeenCalled();
        expect(screen.queryByTestId("ai-full-control-confirm-dialog")).toBeNull();
        expect(document.activeElement).toBe(selector);
    });

    it("renders the full risk-warning content inside the confirmation dialog", () => {
        const onPermissionModeChange = vi.fn();
        renderLeft({ permissionMode: "request", onPermissionModeChange });

        fireEvent.click(screen.getByTestId("ai-permission-mode"));
        fireEvent.click(screen.getByRole("menuitemradio", { name: "完全控制" }));

        const dialog = screen.getByTestId("ai-full-control-confirm-dialog");
        expect(dialog.getAttribute("role")).toBe("alertdialog");
        expect(dialog.textContent).toContain("确认允许完全访问?");
        expect(dialog.textContent).toContain("文件操作");
        expect(dialog.textContent).toContain("读取、创建、修改、上传或删除此计算机上任意位置的文件");
        expect(dialog.textContent).toContain("终端命令");
        expect(dialog.textContent).toContain("访问互联网");
        expect(dialog.textContent).toContain("我已了解风险，并对自己的数据安全负责");
        expect(dialog.textContent).toContain("全局持久生效");
        // Without a workspace option in the menu, the copy must not reference it.
        expect(dialog.textContent).not.toContain("工作区信任");
        expect(dialog.getAttribute("aria-describedby")).toBeTruthy();
    });

    it("requires checking the acknowledgment box before allowing full access", () => {
        const onPermissionModeChange = vi.fn();
        renderLeft({ permissionMode: "request", onPermissionModeChange });

        fireEvent.click(screen.getByTestId("ai-permission-mode"));
        fireEvent.click(screen.getByRole("menuitemradio", { name: "完全控制" }));

        const checkbox = screen.getByRole("checkbox", { name: "我已了解风险，并对自己的数据安全负责" });
        const accept = screen.getByTestId("ai-full-control-confirm-accept") as HTMLButtonElement;
        expect(accept.disabled).toBe(true);
        fireEvent.click(accept);
        expect(onPermissionModeChange).not.toHaveBeenCalled();

        fireEvent.click(checkbox);
        expect((screen.getByTestId("ai-full-control-confirm-accept") as HTMLButtonElement).disabled).toBe(false);
    });

    it("mentions Workspace trust in the warning only when the menu offers it", () => {
        renderLeft({ permissionMode: "request", showWorkspacePermissionOption: true });

        fireEvent.click(screen.getByTestId("ai-permission-mode"));
        // With the workspace option the item's accessible name includes its hint.
        fireEvent.click(screen.getByRole("menuitemradio", { name: /完全控制/ }));

        expect(screen.getByTestId("ai-full-control-confirm-dialog").textContent).toContain("工作区信任");
    });

    it("dismisses the full-control confirmation with Escape or overlay click", () => {
        const onPermissionModeChange = vi.fn();
        renderLeft({ permissionMode: "request", onPermissionModeChange });

        const selector = screen.getByTestId("ai-permission-mode");
        fireEvent.click(selector);
        fireEvent.click(screen.getByRole("menuitemradio", { name: "完全控制" }));

        fireEvent.keyDown(window, { key: "Escape" });
        expect(screen.queryByTestId("ai-full-control-confirm-dialog")).toBeNull();
        expect(onPermissionModeChange).not.toHaveBeenCalled();

        fireEvent.click(selector);
        fireEvent.click(screen.getByRole("menuitemradio", { name: "完全控制" }));
        fireEvent.pointerDown(screen.getByTestId("ai-full-control-confirm-overlay"));
        expect(screen.queryByTestId("ai-full-control-confirm-dialog")).toBeNull();
        expect(onPermissionModeChange).not.toHaveBeenCalled();
    });

    it("focuses Cancel by default and keeps Tab cycling inside the full-control confirmation", async () => {        const onPermissionModeChange = vi.fn();
        renderLeft({ permissionMode: "request", onPermissionModeChange });

        const selector = screen.getByTestId("ai-permission-mode");
        fireEvent.click(selector);
        fireEvent.click(screen.getByRole("menuitemradio", { name: "完全控制" }));

        await vi.waitFor(() => expect(document.activeElement).toBe(screen.getByTestId("ai-full-control-confirm-cancel")));
        // While the acknowledgment is unchecked, the disabled accept button is
        // skipped and Tab cycles between Cancel and the checkbox.
        fireEvent.keyDown(document.activeElement!, { key: "Tab" });
        expect(document.activeElement).toBe(screen.getByRole("checkbox"));
        fireEvent.keyDown(document.activeElement!, { key: "Tab" });
        expect(document.activeElement).toBe(screen.getByTestId("ai-full-control-confirm-cancel"));
        expect(onPermissionModeChange).not.toHaveBeenCalled();
    });

    it("hides the permission selector when the host surface does not support changing it", () => {
        renderLeft({ showPermissionMode: false });

        expect(screen.queryByTestId("ai-permission-mode")).toBeNull();
    });

    it("does not render an inert permission selector without a change handler", () => {
        renderLeft({ onPermissionModeChange: undefined });

        expect(screen.queryByTestId("ai-permission-mode")).toBeNull();
    });

    it("supports keyboard navigation and restores focus when permission menu closes", () => {
        renderLeft({ permissionMode: "request" });
        const selector = screen.getByTestId("ai-permission-mode");
        selector.focus();
        fireEvent.keyDown(selector, { key: "ArrowDown" });
        const request = screen.getByTestId("ai-permission-mode-request");
        const full = screen.getByTestId("ai-permission-mode-full");
        expect(document.activeElement).toBe(request);
        fireEvent.keyDown(request, { key: "ArrowDown" });
        expect(document.activeElement).toBe(full);
        fireEvent.keyDown(document, { key: "Escape" });
        expect(screen.queryByTestId("ai-permission-mode-menu")).toBeNull();
        expect(document.activeElement).toBe(selector);
    });

    it("marks the trigger as dangerous red when full control is selected", () => {
        renderLeft({ permissionMode: "full" });

        const trigger = screen.getByTestId("ai-permission-mode") as HTMLElement;
        // Red text/icon come from the inline theme color; the mc-input-stack
        // neutral button locks (which would repaint every composer button
        // grey/blue) are countered by the [data-permission-mode='full'] rule
        // in App.css, so the attribute is part of the contract.
        expect(trigger.getAttribute("data-permission-mode")).toBe("full");
        expect(trigger.style.color).toBe("rgb(185, 28, 28)");
    });

    it("keeps full control red above the neutral composer button locks", () => {
        const here = dirname(fileURLToPath(import.meta.url));
        const css = readFileSync(resolve(here, "../../../App.css"), "utf8");
        expect(css).toContain(".ai-permission-mode-trigger[data-testid='ai-permission-mode'][data-permission-mode='full']:hover");
        expect(css).toMatch(/\[data-permission-mode='full'\][^{]*\{[^}]*color:\s*var\(--theme-danger/);
    });

    it("renders the permission menu in a viewport-level layer", () => {
        renderLeft();

        fireEvent.click(screen.getByTestId("ai-permission-mode"));
        const menu = screen.getByTestId("ai-permission-mode-menu");
        expect(menu.style.position).toBe("fixed");
        expect(menu.style.zIndex).toBe("40000");
        expect(menu.parentElement).toBe(document.body);
        expect(screen.getByTestId("ai-permission-mode").getAttribute("aria-controls")).toBe(menu.id);
    });

    it("places + before the attachment button and lists iconed commands", () => {
        renderLeft();

        const plus = screen.getByTestId("ai-plus-menu-button");
        const attach = screen.getByTestId("ai-attach-button");
        expect(plus.compareDocumentPosition(attach) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

        fireEvent.click(plus);
        const menu = screen.getByTestId("ai-plus-menu");
        expect(menu).toBeTruthy();
        expect(plus.getAttribute("aria-controls")).toBe(menu.id);
        expect(menu.parentElement).toBe(document.body);
        expect(screen.getByTestId("ai-plus-menu-new-conversation")).toBeTruthy();
        expect(screen.getByTestId("ai-plus-menu-goal")).toBeTruthy();
        expect(screen.getByTestId("ai-plus-menu-btw")).toBeTruthy();
        expect(screen.getByTestId("ai-plus-menu-moa")).toBeTruthy();
        expect(screen.getByTestId("ai-plus-menu-computer")).toBeTruthy();
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

    it("selects goal, btw, moa, and Computer Use compose modes", () => {
        const onComposeActionChange = vi.fn();
        renderLeft({ onComposeActionChange });

        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-plus-menu-goal"));
        expect(onComposeActionChange).toHaveBeenCalledWith("goal");

        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-plus-menu-btw"));
        expect(onComposeActionChange).toHaveBeenCalledWith("btw");

        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-plus-menu-moa"));
        expect(onComposeActionChange).toHaveBeenCalledWith("moa");

        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-plus-menu-computer"));
        expect(onComposeActionChange).toHaveBeenCalledWith("computer");
    });

    it("exposes compose modes as checked menu radios", () => {
        renderLeft({ composeAction: "computer" });
        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));

        expect(screen.getByRole("menuitemradio", { name: /桌面操控/ }).getAttribute("aria-checked")).toBe("true");
        expect(screen.getByRole("menuitemradio", { name: "目标" }).getAttribute("aria-checked")).toBe("false");
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

    it("closes the portaled menu on an outside touch or pointer interaction", () => {
        renderLeft();
        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        expect(screen.getByTestId("ai-plus-menu")).toBeTruthy();

        fireEvent.pointerDown(document.body, { pointerType: "touch" });
        expect(screen.queryByTestId("ai-plus-menu")).toBeNull();
    });

    it("closes floating input menus when the retained panel becomes inactive", () => {
        const { rerender } = renderLeft({ active: true });
        fireEvent.click(screen.getByTestId("ai-plus-menu-button"));
        fireEvent.click(screen.getByTestId("ai-permission-mode"));
        expect(screen.getByTestId("ai-plus-menu")).toBeTruthy();
        expect(screen.getByTestId("ai-permission-mode-menu")).toBeTruthy();

        rerender(
            <AssistantInputActionsLeft
                active={false}
                browseFile={vi.fn()}
                composeAction={null}
                inputLocked={false}
                lang="zh-Hans"
                onComposeActionChange={vi.fn()}
                onPermissionModeChange={vi.fn()}
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
            />,
        );

        expect(screen.queryByTestId("ai-plus-menu")).toBeNull();
        expect(screen.queryByTestId("ai-permission-mode-menu")).toBeNull();
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
        expect(pos.maxHeight).toBe(360);
    });

    it("flips downward near the top edge and clamps horizontally", () => {
        const pos = clampMenuPosition(
            { left: 990, top: 20, bottom: 44, width: 24 },
            { width: 1000, height: 800 },
        );
        expect(pos.openUp).toBe(false);
        expect(pos.top).toBe(50);
        expect(pos.maxHeight).toBe(360);
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

    it("uses the larger side and constrains the menu when neither side fits", () => {
        const pos = clampMenuPosition(
            { left: 40, top: 190, bottom: 214, width: 24 },
            { width: 1000, height: 400 },
        );

        expect(pos.openUp).toBe(true);
        expect(pos.maxHeight).toBe(176);
    });

    it("reports no usable menu height when the trigger is flush with a viewport edge", () => {
        const pos = clampMenuPosition(
            { left: 40, top: 0, bottom: 24, width: 24 },
            { width: 1000, height: 32 },
        );

        expect(pos.maxHeight).toBe(0);
    });
});
