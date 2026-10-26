import type { CSSProperties } from "react";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
    baseInputBtnStyle,
    COMPOSER_CONTROL_HEIGHT_PX,
    getInputActionButtonStyle,
    maximizedInlineStyle,
    overlayStyle,
    overlayTheme,
} from "../aiAssistantPanelTheme";

/** True if any string style value uses viewport units (unsafe under UI-scale transform). */
function styleUsesViewportUnits(style: CSSProperties): boolean {
    return Object.values(style).some(
        (value) => typeof value === "string" && /\d(?:\.\d+)?(?:vw|vh|dvh|svh|lvh)\b/i.test(value),
    );
}

function expectFixedContainingBlockFill(style: CSSProperties) {
    expect(style.position).toBe("fixed");
    expect(style.inset).toBe(0);
    expect(style.width).toBeUndefined();
    expect(style.height).toBeUndefined();
    expect(style.maxWidth).toBeUndefined();
    expect(style.maxHeight).toBeUndefined();
    expect(styleUsesViewportUnits(style)).toBe(false);
    expect(style.minHeight).toBe(0);
    expect(style.minWidth).toBe(0);
    expect(style.display).toBe("flex");
    expect(style.flexDirection).toBe("column");
}

describe("fixed containing-block fill (UI scale safe)", () => {
    it("maximizedInlineStyle fills via inset:0 without viewport units", () => {
        // Regression: 100vw/100vh under .app-scale-layer clipped composer + window controls.
        expectFixedContainingBlockFill(maximizedInlineStyle);
        expect(maximizedInlineStyle.overflow).toBe("hidden");
        expect(maximizedInlineStyle.zIndex).toBe(12000);
    });

    it("overlayStyle uses the same fill strategy", () => {
        expectFixedContainingBlockFill(overlayStyle);
        expect(overlayStyle.zIndex).toBe(10000);
    });
});

describe("composer control height", () => {
    it("keeps the TS constant and the CSS token in sync", () => {
        // Two sources of truth by necessity (inline styles vs. stylesheet), so a
        // guard is the only thing stopping one side from drifting.
        const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../../../styles/partials/00-tokens.css"), "utf8");
        const match = /--mc-composer-control-height:\s*(\d+)px/.exec(css);
        expect(match, "missing --mc-composer-control-height token").toBeTruthy();
        expect(Number(match![1])).toBe(COMPOSER_CONTROL_HEIGHT_PX);
    });

    it("gives the icon button the shared height with border-box sizing", () => {
        // border-box is what makes the 1px border sit inside the height instead
        // of pushing the button 2px taller than the chips beside it.
        expect(baseInputBtnStyle.height).toBe(`${COMPOSER_CONTROL_HEIGHT_PX}px`);
        expect(baseInputBtnStyle.minHeight).toBe(`${COMPOSER_CONTROL_HEIGHT_PX}px`);
        const styled = getInputActionButtonStyle(overlayTheme, "light", "neutral");
        expect(styled.boxSizing).toBe("border-box");
    });
});
