import React from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MarkdownPreview } from "../CodePreviewMarkdown";
import { createCodePreviewTheme } from "../CodePreviewPanel";
import { fluentLightScheme } from "../assistantLightSchemes";
import { darkTheme } from "../aiAssistantPanelTheme";
import { markdownPreviewInk } from "../markdownPreviewInk";
import { cssColor, cssPaint, hexToRgb } from "./markdownPreviewCss";

const md = [
    "# Title",
    "",
    "Hello **world** and [link](https://example.com) plus `code`.",
    "",
    "plain paragraph",
].join("\n");

describe("MarkdownPreview ink", () => {
    it("does not inherit fluent scheme blues for markdown prose", () => {
        const theme = createCodePreviewTheme(fluentLightScheme.assistantTheme);

        render(<MarkdownPreview content={md} theme={theme} />);

        const ink = markdownPreviewInk(false);
        const root = screen.getByTestId("code-preview-markdown-view");
        expect(cssColor(root)).toBe(hexToRgb(ink.body));

        expect(cssColor(screen.getByText("Title"))).toBe(hexToRgb(ink.emphasis));
        const bold = screen.getByText("world");
        expect(bold.tagName).toBe("STRONG");
        expect(cssColor(bold)).toBe(hexToRgb(ink.emphasis));
        expect(cssColor(screen.getByText("plain paragraph"))).toBe(hexToRgb(ink.body));
        expect(cssColor(screen.getByText("link"))).toBe(hexToRgb(ink.link));
        expect(cssColor(root)).not.toBe(hexToRgb("#2f78d0"));
        expect(cssPaint(screen.getByText("Title").style.borderBottom)).toContain(hexToRgb(ink.rule));

        const code = screen.getByText("code");
        expect(code.tagName).toBe("CODE");
        expect(cssPaint(code.style.background)).toBe(hexToRgb(ink.wash));
        expect(cssPaint(code.style.background).toLowerCase()).not.toBe(hexToRgb("#e2edfc"));
    });

    it("uses light-on-dark grayscale when the preview canvas is dark", () => {
        const theme = createCodePreviewTheme(darkTheme);
        render(<MarkdownPreview content={md} theme={theme} />);

        const ink = markdownPreviewInk(true);
        expect(cssColor(screen.getByTestId("code-preview-markdown-view"))).toBe(hexToRgb(ink.body));
        expect(cssColor(screen.getByText("Title"))).toBe(hexToRgb(ink.emphasis));
        expect(cssColor(screen.getByText("world"))).toBe(hexToRgb(ink.emphasis));
        expect(cssPaint(screen.getByText("code").style.background)).toBe(hexToRgb(ink.wash));
    });
});
