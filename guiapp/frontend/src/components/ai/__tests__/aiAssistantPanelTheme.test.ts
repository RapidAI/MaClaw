import type { CSSProperties } from "react";
import { describe, expect, it } from "vitest";
import { maximizedInlineStyle, overlayStyle } from "../aiAssistantPanelTheme";

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
