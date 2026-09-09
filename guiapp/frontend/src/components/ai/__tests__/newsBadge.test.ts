import { describe, expect, it } from "vitest";
import { resolveNewsBadge } from "../newsBadge";

describe("resolveNewsBadge", () => {
    it("maps known categories to plain labels and glyphs", () => {
        expect(resolveNewsBadge({ category: "notice", icon: "\u{1F4F0}" })).toEqual({ label: "INFO", glyph: "info" });
        expect(resolveNewsBadge({ category: "update", icon: "x" })).toEqual({ label: "NEW", glyph: "info" });
        expect(resolveNewsBadge({ category: "tip", icon: "" })).toEqual({ label: "TIP", glyph: "info" });
        expect(resolveNewsBadge({ category: "alert", icon: "!" })).toEqual({ label: "ALERT", glyph: "warn" });
    });

    it("accepts plain icon labels from createNewsMessage", () => {
        expect(resolveNewsBadge({ category: "", icon: "INFO" }).label).toBe("INFO");
        expect(resolveNewsBadge({ category: "", icon: "alert" }).label).toBe("ALERT");
    });

    it("never returns pictograph labels for legacy emoji icons", () => {
        const badge = resolveNewsBadge({ category: "", icon: "\u{1F4F0}" });
        expect(badge.label).toBe("INFO");
        expect(badge.label).not.toMatch(/\p{Extended_Pictographic}/u);
    });
});
