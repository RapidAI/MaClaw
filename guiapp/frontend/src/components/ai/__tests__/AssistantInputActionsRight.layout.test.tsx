import { render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AssistantInputActionsRight } from "../AssistantInputActions";
import { COMPOSER_CONTROL_HEIGHT_PX, type Theme } from "../aiAssistantPanelTheme";

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
    divider: "#e2e8f0",
    sendBtnBg: "#2f6fbc",
    sendBtnBorder: "#2f6fbc",
    sendBtnColor: "#ffffff",
} as Theme;

function renderRight(overrides: Partial<Parameters<typeof AssistantInputActionsRight>[0]> = {}) {
    return render(
        <AssistantInputActionsRight
            canSend={true}
            handleCancel={vi.fn()}
            handleClearInput={vi.fn()}
            handleSend={vi.fn()}
            inputValue="你好"
            isBusy={false}
            lang="zh-Hans"
            ready={true}
            showBusySpinner={false}
            theme={theme}
            themeMode="light"
            {...overrides}
        />,
    );
}

const CONTROL_HEIGHT = `${COMPOSER_CONTROL_HEIGHT_PX}px`;

describe("AssistantInputActionsRight", () => {
    // The send button used to be forced to 40x40 while clear stayed 30x28, so
    // the two trailing controls never lined up with the left icon group.
    it("keeps send and clear on the shared composer control footprint", () => {
        const { container } = renderRight();
        const send = container.querySelector<HTMLButtonElement>(".ai-composer-send");
        const clear = container.querySelector<HTMLButtonElement>('[data-testid="ai-clear-input"]');
        expect(send).not.toBeNull();
        expect(clear).not.toBeNull();

        for (const button of [send!, clear!]) {
            expect(button.style.width).toBe("30px");
            expect(button.style.minWidth).toBe("30px");
            expect(button.style.height).toBe(CONTROL_HEIGHT);
            expect(button.style.minHeight).toBe(CONTROL_HEIGHT);
        }
        expect(send!.style.borderRadius).toBe(clear!.style.borderRadius);
    });

    it("keeps send and clear in one pair with the primary action last", () => {
        const { container } = renderRight();
        const send = container.querySelector<HTMLButtonElement>(".ai-composer-send")!;
        const clear = container.querySelector<HTMLButtonElement>('[data-testid="ai-clear-input"]')!;

        expect(send.parentElement).not.toBeNull();
        expect(send.parentElement).toBe(clear.parentElement);
        expect(send.parentElement!.children).toHaveLength(2);
        expect(send.parentElement!.lastElementChild).toBe(send);
        expect(send.parentElement!.style.gap).toBe("4px");
    });

    it("disables clear and send in the same empty-input state", () => {
        const { container } = renderRight({ canSend: false, inputValue: "" });
        expect(container.querySelector<HTMLButtonElement>('[data-testid="ai-clear-input"]')!.disabled).toBe(true);
        expect(container.querySelector<HTMLButtonElement>(".ai-composer-send")!.disabled).toBe(true);
    });

    it("keeps the stop control on the same footprint and replaces the pair while busy", () => {
        const { container } = renderRight({ isBusy: true, cancelSession: {} });
        const stop = container.querySelector<HTMLButtonElement>('[data-testid="ai-cancel-progress"]');
        expect(stop).not.toBeNull();
        expect(stop!.style.width).toBe("30px");
        expect(stop!.style.height).toBe(CONTROL_HEIGHT);
        expect(container.querySelector(".ai-composer-send")).toBeNull();
        expect(container.querySelector('[data-testid="ai-clear-input"]')).toBeNull();
    });

    it("still lets a surface own the send styling", () => {
        const { container } = renderRight({ sendButtonStyle: { width: "54px", minWidth: "54px" } });
        const send = container.querySelector<HTMLButtonElement>(".ai-composer-send")!;
        expect(send.style.width).toBe("54px");
        expect(send.style.minWidth).toBe("54px");
    });
});
