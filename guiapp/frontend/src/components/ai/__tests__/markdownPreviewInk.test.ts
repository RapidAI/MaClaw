import { describe, expect, it } from "vitest";
import {
    markdownPreviewInk,
    markdownPreviewInkFromBg,
    markdownPreviewIsDark,
    withMarkdownProseTheme,
} from "../markdownPreviewInk";

describe("markdownPreviewInk", () => {
    it("uses near-black emphasis and dark-gray body in light mode", () => {
        const ink = markdownPreviewInk(false);
        expect(ink.emphasis).toBe("#111111");
        expect(ink.body).toBe("#3f3f3f");
        expect(ink.body).not.toBe(ink.emphasis);
        expect(ink.link).toMatch(/^#[0-9a-f]{6}$/i);
        // No blue channel dominance: R ≈ G ≈ B.
        for (const hex of [ink.body, ink.emphasis, ink.muted, ink.link, ink.rule, ink.wash]) {
            const n = parseInt(hex.slice(1), 16);
            const r = (n >> 16) & 255;
            const g = (n >> 8) & 255;
            const b = n & 255;
            expect(Math.abs(r - g)).toBeLessThanOrEqual(2);
            expect(Math.abs(g - b)).toBeLessThanOrEqual(2);
        }
    });

    it("uses near-white emphasis and light-gray body in dark mode", () => {
        const ink = markdownPreviewInk(true);
        expect(ink.emphasis).toBe("#f5f5f5");
        expect(ink.body).toBe("#c8c8c8");
        const emphasisN = parseInt(ink.emphasis.slice(1), 16);
        const bodyN = parseInt(ink.body.slice(1), 16);
        expect(emphasisN).toBeGreaterThan(bodyN);
    });

    it("treats white canvases as light and charcoal canvases as dark", () => {
        expect(markdownPreviewIsDark("#ffffff")).toBe(false);
        expect(markdownPreviewIsDark("#0f141b")).toBe(true);
        expect(markdownPreviewIsDark("#ffffff", true)).toBe(true);
        expect(markdownPreviewIsDark("#0f141b", false)).toBe(false);
        expect(markdownPreviewInkFromBg("#ffffff").body).toBe(markdownPreviewInk(false).body);
        expect(markdownPreviewInkFromBg("#0f141b").emphasis).toBe(markdownPreviewInk(true).emphasis);
    });
});

describe("withMarkdownProseTheme", () => {
    const scheme = {
        bg: "#ffffff",
        isDark: false as boolean | undefined,
        text: "#2f78d0",
        textMuted: "#56677c",
        headingColor: "#4f46e5",
        linkColor: "#2563eb",
        quoteText: "#4e6179",
        quoteBorder: "#a9c8f0",
        quoteBg: "#f8fafc",
        codeBg: "#e2edfc",
        codeText: "#2f78d0",
        codeBlockBg: "#f4f8fe",
        codeBlockBorder: "#cfdff5",
        headerBg: "#eef5ff",
        border: "#c9ddf6",
        accentColor: "#2f78d0",
        accentBg: "#eef5ff",
        accentText: "#ffffff",
        extraChrome: "#1769e8",
    };

    it("recolors document tokens to grayscale and leaves unrelated fields alone", () => {
        const ink = markdownPreviewInk(false);
        const next = withMarkdownProseTheme(scheme);
        expect(next.text).toBe(ink.body);
        expect(next.headingColor).toBe(ink.emphasis);
        expect(next.linkColor).toBe(ink.link);
        expect(next.quoteBorder).toBe(ink.rule);
        expect(next.quoteBg).toBe(ink.wash);
        expect(next.codeBg).toBe(ink.wash);
        expect(next.codeText).toBe(ink.body);
        expect(next.codeBlockBg).toBe(ink.wash);
        expect(next.codeBlockBorder).toBe(ink.rule);
        expect(next.headerBg).toBe(ink.wash);
        expect(next.border).toBe(ink.rule);
        expect(next.accentColor).toBe(ink.emphasis);
        expect(next.accentBg).toBe(ink.wash);
        expect(next.accentText).toBe(ink.emphasis);
        expect(next.extraChrome).toBe("#1769e8");
        expect(next.bg).toBe("#ffffff");
    });

    it("infers dark ink from canvas when isDark is omitted", () => {
        const ink = markdownPreviewInk(true);
        const next = withMarkdownProseTheme({ ...scheme, isDark: undefined, bg: "#0f141b" });
        expect(next.text).toBe(ink.body);
        expect(next.headingColor).toBe(ink.emphasis);
        expect(next.accentColor).toBe(ink.emphasis);
    });
});
