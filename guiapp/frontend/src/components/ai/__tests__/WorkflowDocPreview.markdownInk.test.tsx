import React from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { WorkflowDocPreview } from "../WorkflowDocPreview";
import { markdownPreviewInk } from "../markdownPreviewInk";
import type { DocPreviewTheme } from "../WorkflowDocPreview";
import { cssColor, cssPaint, hexToRgb } from "./markdownPreviewCss";

const blueTheme: DocPreviewTheme = {
    isDark: false,
    bg: "#ffffff",
    text: "#2f78d0",
    textMuted: "#56677c",
    border: "#c9ddf6",
    headerBg: "#eef5ff",
    accentColor: "#2f78d0",
    accentBg: "#eef5ff",
    codeBg: "#e2edfc",
    codeText: "#2f78d0",
    codeBlockBg: "#f4f8fe",
    codeBlockBorder: "#cfdff5",
    headingColor: "#4f46e5",
    linkColor: "#2563eb",
    quoteBorder: "#a9c8f0",
    quoteText: "#4e6179",
    quoteBg: "#f8fafc",
};

const previewDoc = (
    theme: DocPreviewTheme,
    markdown: string,
) => (
    <WorkflowDocPreview
        phaseDocuments={new Map([["requirements", markdown]])}
        currentPhaseID="requirements"
        latestDocumentPhaseID="requirements"
        phases={[{ id: "requirements", name: "需求", index: 0, expectsDocument: true }]}
        workflowType="coding"
        gateResults={new Map()}
        theme={theme}
    />
);

describe("WorkflowDocPreview markdown ink", () => {
    it("renders markdown documents in black-gray instead of scheme blues", () => {
        render(previewDoc(blueTheme, "# Title\n\nHello **world** and [link](https://example.com) plus `code`.\n\n> quoted\n\nplain paragraph"));

        const ink = markdownPreviewInk(false);
        expect(cssColor(screen.getByTestId("workflow-doc-markdown"))).toBe(hexToRgb(ink.body));
        expect(cssColor(screen.getByText("Title"))).toBe(hexToRgb(ink.emphasis));
        const bold = screen.getByText("world");
        expect(bold.tagName).toBe("STRONG");
        expect(cssColor(bold)).toBe(hexToRgb(ink.emphasis));
        expect(cssColor(screen.getByText("link"))).toBe(hexToRgb(ink.link));
        expect(cssColor(screen.getByText("Title"))).not.toBe(hexToRgb("#4f46e5"));
        expect(cssColor(screen.getByText("link"))).not.toBe(hexToRgb("#2563eb"));

        const quote = screen.getByText("quoted").closest("blockquote") as HTMLElement;
        expect(cssPaint(quote.style.borderLeft)).toContain(hexToRgb(ink.rule));
        expect(quote.style.borderLeft.toLowerCase()).not.toContain("a9c8f0");
        expect(cssPaint(quote.style.background)).toBe(hexToRgb(ink.wash));

        const code = screen.getByText("code");
        expect(code.tagName).toBe("CODE");
        expect(cssPaint(code.style.background)).toBe(hexToRgb(ink.wash));
        expect(cssPaint(code.style.background).toLowerCase()).not.toBe(hexToRgb("#e2edfc"));
    });

    it("infers dark ink from a dark canvas when isDark is omitted", () => {
        render(previewDoc({ ...blueTheme, isDark: undefined, bg: "#0f141b" }, "# Title\n\nplain paragraph"));

        const ink = markdownPreviewInk(true);
        expect(cssColor(screen.getByTestId("workflow-doc-markdown"))).toBe(hexToRgb(ink.body));
        expect(cssColor(screen.getByText("Title"))).toBe(hexToRgb(ink.emphasis));
    });
});
