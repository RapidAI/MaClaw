import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";
import {
    WELCOME_TEMPLATE_OFFER_AUTO_DISMISS_MS,
    WelcomeTemplateSaveOfferBanner,
} from "../WelcomeTemplateSaveOffer";
import { lightTheme } from "../aiAssistantPanelTheme";

describe("WelcomeTemplateSaveOfferBanner", () => {
    beforeEach(() => {
        vi.useFakeTimers();
    });
    afterEach(() => {
        vi.useRealTimers();
    });

    it("renders title and wires save / dismiss", () => {
        const onSave = vi.fn();
        const onDismiss = vi.fn();
        render(
            <WelcomeTemplateSaveOfferBanner
                lang="zh"
                theme={lightTheme}
                title="做一份可汇报的竞品分析"
                onSave={onSave}
                onDismiss={onDismiss}
                autoDismissMs={0}
            />,
        );
        expect(screen.getByTestId("welcome-template-save-offer")).toBeTruthy();
        expect(screen.getByText("做一份可汇报的竞品分析")).toBeTruthy();
        fireEvent.click(screen.getByTestId("welcome-template-save-offer-confirm"));
        expect(onSave).toHaveBeenCalledTimes(1);
        fireEvent.click(screen.getByTestId("welcome-template-save-offer-dismiss"));
        expect(onDismiss).toHaveBeenCalledTimes(1);
    });

    it("auto-dismisses after the countdown", () => {
        const onDismiss = vi.fn();
        render(
            <WelcomeTemplateSaveOfferBanner
                lang="zh"
                theme={lightTheme}
                title="周报"
                onSave={() => {}}
                onDismiss={onDismiss}
                autoDismissMs={5_000}
            />,
        );
        expect(onDismiss).not.toHaveBeenCalled();
        act(() => {
            vi.advanceTimersByTime(5_000);
        });
        expect(onDismiss).toHaveBeenCalledTimes(1);
    });

    it("pauses auto-dismiss while hovered", () => {
        const onDismiss = vi.fn();
        render(
            <WelcomeTemplateSaveOfferBanner
                lang="en"
                theme={lightTheme}
                title="Weekly update"
                onSave={() => {}}
                onDismiss={onDismiss}
                autoDismissMs={5_000}
            />,
        );
        const banner = screen.getByTestId("welcome-template-save-offer");
        fireEvent.mouseEnter(banner);
        act(() => {
            vi.advanceTimersByTime(5_000);
        });
        expect(onDismiss).not.toHaveBeenCalled();
        expect(screen.getByText(/paused/i)).toBeTruthy();

        fireEvent.mouseLeave(banner);
        act(() => {
            // Remaining time was reduced by the paused interval; advance full window again.
            vi.advanceTimersByTime(5_000);
        });
        expect(onDismiss).toHaveBeenCalledTimes(1);
    });

    it("exports a positive default auto-dismiss duration", () => {
        expect(WELCOME_TEMPLATE_OFFER_AUTO_DISMISS_MS).toBeGreaterThan(0);
    });
});
