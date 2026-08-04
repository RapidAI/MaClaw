import { describe, it, expect } from "vitest";
import {
    contrastingInkOnFill,
    formFieldLabelColor,
    parseCssHexColor,
    primaryFilledButtonStyle,
    relativeLuminance,
    resolvePrimaryFilledColors,
    darkTheme,
    lightTheme,
} from "../aiAssistantPanelTheme";
import { getAssistantDarkScheme } from "../assistantDarkSchemes";

describe("parseCssHexColor / relativeLuminance", () => {
    it("parses 3- and 6-digit hex", () => {
        expect(parseCssHexColor("#fff")).toEqual({ r: 255, g: 255, b: 255 });
        expect(parseCssHexColor("#2f5f98")).toEqual({ r: 0x2f, g: 0x5f, b: 0x98 });
        expect(parseCssHexColor("not-a-color")).toBeNull();
    });

    it("ranks light fill above dark fill", () => {
        const light = relativeLuminance("#d4d4d4");
        const dark = relativeLuminance("#2f5f98");
        expect(light).not.toBeNull();
        expect(dark).not.toBeNull();
        expect(light!).toBeGreaterThan(dark!);
    });
});

describe("contrastingInkOnFill", () => {
    it("picks dark ink on light fills and white on dark fills", () => {
        expect(contrastingInkOnFill("#d4d4d4")).toBe("#111111");
        expect(contrastingInkOnFill("#2f5f98")).toBe("#ffffff");
        expect(contrastingInkOnFill("#7dd3fc")).toBe("#111111"); // aurora send fill
    });

    it("keeps preferred ink when contrast is sufficient", () => {
        expect(contrastingInkOnFill("#2f5f98", "#ffffff")).toBe("#ffffff");
        expect(contrastingInkOnFill("#d4d4d4", "#111111")).toBe("#111111");
    });

    it("overrides preferred white-on-light", () => {
        expect(contrastingInkOnFill("#d4d4d4", "#ffffff")).toBe("#111111");
    });
});

describe("primaryFilledButtonStyle / resolvePrimaryFilledColors", () => {
    it("pairs sendBtnBg fill with sendBtnColor label across all dark schemes", () => {
        for (const id of ["graphite", "classic", "aurora", "ember", "violet"] as const) {
            const t = getAssistantDarkScheme(id).assistantTheme;
            const style = primaryFilledButtonStyle(t);
            expect(style.background).toBe(t.sendBtnBg);
            // Must remain readable on fill (auto-ink may reinforce preferred)
            const resolved = resolvePrimaryFilledColors(t);
            expect(style.color).toBe(resolved.fg);
            if (t.btnColor !== t.sendBtnBg) {
                expect(style.background).not.toBe(t.btnColor);
            }
            if (id === "graphite") {
                expect(style.background).toBe("#7ea8e0");
                expect(style.color).toBe("#0f141b");
            }
            if (id === "classic") {
                expect(style.background).toBe("#2f5f98");
                expect(style.color).toBe("#ffffff");
            }
        }
    });

    it("works for light theme", () => {
        const style = primaryFilledButtonStyle(lightTheme);
        expect(style.background).toBe(lightTheme.sendBtnBg);
        expect(style.color).toBe(lightTheme.sendBtnColor);
    });

    it("never yields white-on-light when only btnColor is provided", () => {
        const style = primaryFilledButtonStyle({
            btnColor: "#d4d4d4",
            sendBtnBg: "",
            sendBtnColor: "",
            sendBtnBorder: "",
        });
        expect(style.background).toBe("#d4d4d4");
        expect(style.color).toBe("#111111");
    });

    it("falls back to product blue when nothing is set", () => {
        const style = primaryFilledButtonStyle({
            btnColor: "",
            sendBtnBg: "",
            sendBtnColor: "",
            sendBtnBorder: "",
        });
        expect(style.background).toBe("#2f6fbc");
        expect(style.color).toBe("#ffffff");
    });
});

describe("formFieldLabelColor", () => {
    it("prefers fieldLabel over washed textMuted", () => {
        expect(formFieldLabelColor(darkTheme)).toBe(darkTheme.fieldLabel);
        expect(formFieldLabelColor(getAssistantDarkScheme("graphite").assistantTheme)).toBe("#bfccdb");
    });
});
