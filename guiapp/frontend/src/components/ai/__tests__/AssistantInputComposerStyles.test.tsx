// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { ASSISTANT_COMPOSER_RADIUS, getAssistantInputComposerStyles } from "../AssistantInputComposerStyles";
import { COMPOSER_CONTROL_HEIGHT_PX, overlayTheme } from "../aiAssistantPanelTheme";

const base = {
    cancelPending: false,
    hasInputOverlay: false,
    isExpandedInput: false,
    ready: true,
    theme: overlayTheme,
};

describe("getAssistantInputComposerStyles", () => {
    it("keeps floating bottom margin when not flush (standalone / no footer)", () => {
        const { inputBarStyle } = getAssistantInputComposerStyles({
            ...base,
            inline: false,
            flushBottom: false,
        });
        expect(inputBarStyle.margin).toBe("0 10px 10px 10px");
        expect(inputBarStyle.paddingBottom).toBe("max(6px, env(safe-area-inset-bottom))");
    });

    it("drops floating bottom margin when flushBottom (main chat / VE + quick-settings)", () => {
        const { inputBarStyle } = getAssistantInputComposerStyles({
            ...base,
            inline: false,
            flushBottom: true,
        });
        expect(inputBarStyle.margin).toBe("0 10px 0 10px");
        // Safe-area moves to the footer bar; composer keeps a fixed inner pad only.
        expect(inputBarStyle.paddingBottom).toBe("6px");
        expect(inputBarStyle.borderRadius).toBe(`${ASSISTANT_COMPOSER_RADIUS} ${ASSISTANT_COMPOSER_RADIUS} 0 0`);
        expect(inputBarStyle.borderBottom).toBe("none");
    });

    it("keeps full radius and bottom border when floating with bottom gap", () => {
        const { inputBarStyle } = getAssistantInputComposerStyles({
            ...base,
            inline: false,
            flushBottom: false,
        });
        expect(inputBarStyle.borderRadius).toBe(ASSISTANT_COMPOSER_RADIUS);
        expect(inputBarStyle.borderBottom).toBeUndefined();
    });

    it("uses a rounded workbench field for inline composers", () => {
        const { inputBarStyle } = getAssistantInputComposerStyles({
            ...base,
            inline: true,
            flushBottom: false,
        });
        expect(inputBarStyle.margin).toBeUndefined();
        expect(inputBarStyle.boxShadow).toBeUndefined();
        expect(inputBarStyle.width).toBe("100%");
        expect(inputBarStyle.borderRadius).toBe(ASSISTANT_COMPOSER_RADIUS);
        expect(inputBarStyle.border).toContain("1.5px solid");
        // Mid-page welcome field must not inherit the window safe-area inset.
        expect(inputBarStyle.paddingBottom).toBe("6px");
    });

    it("sizes the toolbar row to the shared composer control height", () => {
        const { toolbarStyle } = getAssistantInputComposerStyles({
            ...base,
            inline: false,
            flushBottom: true,
        });
        // The row's tallest child is a control of exactly this height; a smaller
        // floor would let the toolbar collapse under it during layout.
        expect(toolbarStyle.minHeight).toBe(`${COMPOSER_CONTROL_HEIGHT_PX}px`);
    });
});
