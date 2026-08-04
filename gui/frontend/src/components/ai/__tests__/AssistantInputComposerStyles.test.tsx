// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { getAssistantInputComposerStyles } from "../AssistantInputComposerStyles";
import { overlayTheme } from "../aiAssistantPanelTheme";

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
        expect(inputBarStyle.borderRadius).toBe("14px 14px 0 0");
        expect(inputBarStyle.borderBottom).toBe("none");
    });

    it("keeps full radius and bottom border when floating with bottom gap", () => {
        const { inputBarStyle } = getAssistantInputComposerStyles({
            ...base,
            inline: false,
            flushBottom: false,
        });
        expect(inputBarStyle.borderRadius).toBe("14px");
        expect(inputBarStyle.borderBottom).toBeUndefined();
    });

    it("uses no outer margin for inline composers", () => {
        const { inputBarStyle } = getAssistantInputComposerStyles({
            ...base,
            inline: true,
            flushBottom: true,
        });
        expect(inputBarStyle.margin).toBeUndefined();
        expect(inputBarStyle.borderTop).toContain("1px solid");
    });
});
