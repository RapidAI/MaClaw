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

    it("keeps parentheses in a document image and link", () => {
        const theme = createCodePreviewTheme(fluentLightScheme.assistantTheme);
        const href = "https://upload.wikimedia.org/wikipedia/commons/a/ab/Foo_(bar).png";
        render(<MarkdownPreview content={`![roofline](${href})\n\nSee [figure](${href}) and ![local](./Foo_(bar).png).`} theme={theme} />);

        const images = screen.getAllByRole("img");
        expect(images.map((image) => image.getAttribute("src"))).toEqual([
            expect.stringContaining("Foo_(bar).png"),
            "./Foo_(bar).png",
        ]);
        const link = screen.getByTestId("markdown-external-link");
        expect(link.getAttribute("href")).toContain("Foo_(bar).png");
        expect(link.textContent).toBe("figure");
    });

    it("keeps a linked document image beside its page link", () => {
        const theme = createCodePreviewTheme(fluentLightScheme.assistantTheme);
        const src = "https://upload.wikimedia.org/wikipedia/commons/a/ab/Foo_(bar).png";
        const page = "https://en.wikipedia.org/wiki/Roofline_model";
        const { container } = render(<MarkdownPreview content={`[![roofline](${src})](${page})`} theme={theme} />);
        const image = container.querySelector("img");
        const link = screen.getByTestId("markdown-external-link");
        expect(image?.getAttribute("src")).toContain("Foo_(bar).png");
        expect(link.getAttribute("href")).toBe(page);
        expect(link.querySelector("img")).toBeNull();
        expect(container.textContent).not.toContain("](");
    });

    it("does not leave a tail after a parenthesized document link", () => {
        const theme = createCodePreviewTheme(fluentLightScheme.assistantTheme);
        const { container } = render(<MarkdownPreview content={"See [guide](./Foo_(bar).md) now."} theme={theme} />);
        expect(container.textContent).toContain("guide");
        expect(container.textContent).not.toContain(".md)");
    });

    it("does not load a protocol-relative document image", () => {
        const theme = createCodePreviewTheme(fluentLightScheme.assistantTheme);
        const { container } = render(<MarkdownPreview content={"![x](//evil.example/Foo_(bar).png) next"} theme={theme} />);
        expect(container.querySelector("img")).toBeNull();
        expect(container.textContent).not.toContain(".png)");
        expect(container.textContent).not.toContain("evil.example");
        expect(container.textContent).toContain("next");
    });

    it("renders inline and display math through KaTeX", () => {
        const theme = createCodePreviewTheme(fluentLightScheme.assistantTheme);
        const doc = [
            "Observation process $\\mathcal{O}_{t,p}$ with budget $t$ and loss $p$.",
            "",
            "$$F_1 = 2 \\cdot \\frac{P \\cdot R}{P + R}$$",
            "",
            "Macro-F1 reaches $0.905 \\pm 0.012$.",
        ].join("\n");
        const { container } = render(<MarkdownPreview content={doc} theme={theme} />);
        expect(container.querySelectorAll(".katex").length).toBeGreaterThanOrEqual(4);
        expect(container.querySelector(".math.math-display")).toBeTruthy();
        // KaTeX keeps the raw TeX in a MathML <annotation>; the $ delimiters must be gone.
        expect(container.textContent).not.toContain("$\\mathcal");
        expect(container.textContent).not.toContain("$0.905");
        expect(container.textContent).not.toContain("$$");
    });

    it("keeps currency dollar amounts as prose", () => {
        const theme = createCodePreviewTheme(fluentLightScheme.assistantTheme);
        const { container } = render(<MarkdownPreview content={"It costs $5 and $1,200 total."} theme={theme} />);
        expect(container.querySelector(".katex")).toBeNull();
        expect(container.textContent).toContain("$5 and $1,200 total.");
    });

    it("renders GFM markup the line painter never reached", () => {
        const theme = createCodePreviewTheme(fluentLightScheme.assistantTheme);
        const doc = [
            "A ~~gone~~ word and a footnote.[^1]",
            "",
            "[^1]: The note body.",
            "",
            "- [x] done item",
        ].join("\n");
        const { container } = render(<MarkdownPreview content={doc} theme={theme} />);
        expect(container.querySelector("del")?.textContent).toBe("gone");
        expect(container.querySelector("sup")).toBeTruthy();
        expect(container.querySelector('input[type="checkbox"]')).toBeTruthy();
        expect(container.textContent).toContain("The note body.");
    });

    it("renders ==highlight== as a mark element", () => {
        const theme = createCodePreviewTheme(fluentLightScheme.assistantTheme);
        const { container } = render(<MarkdownPreview content={"some ==key phrase== text"} theme={theme} />);
        expect(container.querySelector("mark")?.textContent).toBe("key phrase");
    });

    it("drops raw html instead of showing or executing it", () => {
        const theme = createCodePreviewTheme(fluentLightScheme.assistantTheme);
        const { container } = render(<MarkdownPreview content={'keep <a id="sec-i"></a> text <script>window.x=1</script>'} theme={theme} />);
        expect(container.querySelector("script")).toBeNull();
        expect(container.querySelector("a")).toBeNull();
        expect(container.textContent).toContain("keep");
        expect(container.textContent).toContain("text");
        expect(container.textContent).not.toContain("sec-i");
    });
});
