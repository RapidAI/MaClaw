// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
    buildAssistantReplyCopyText,
    copyTextToClipboard,
    renderContentWithCodeBlocks,
    renderMessage,
} from "./aiAssistantMarkdown";
import { renderScreenshotPreview } from "./aiAssistantMarkdownMedia";
import { darkTheme, lightTheme } from "./aiAssistantPanelTheme";

const { openFileOrShowInFolderMock, showItemInFolderMock } = vi.hoisted(() => ({
    openFileOrShowInFolderMock: vi.fn(async () => undefined),
    showItemInFolderMock: vi.fn(async () => undefined),
}));

vi.mock("../../../wailsjs/go/main/App", () => ({
    OpenFileOrShowInFolder: openFileOrShowInFolderMock,
    ShowItemInFolder: showItemInFolderMock,
}));

vi.mock("../../../wailsjs/runtime", () => ({
    BrowserOpenURL: vi.fn(),
}));

describe("renderContentWithCodeBlocks", () => {
    beforeEach(() => {
        openFileOrShowInFolderMock.mockClear();
        showItemInFolderMock.mockClear();
    });

    it("normalizes escaped newline sequences before rendering", () => {
        render(<div>{renderContentWithCodeBlocks("First line\\nSecond line\\nThird line", lightTheme)}</div>);

        expect(screen.getByText("First line")).toBeTruthy();
        expect(screen.getByText("Second line")).toBeTruthy();
        expect(screen.getByText("Third line")).toBeTruthy();
    });

    it("keeps escaped newline sequences inside fenced code blocks", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("```json\\n{\\\"ok\\\": true}\\n```", lightTheme)}</div>
        );

        const code = container.querySelector("code");
        expect(code?.textContent).toBe('{\\"ok\\": true}');
    });

    it("does not rewrite escaped newline string literals inside fenced code blocks", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("```ts\\nconst value = \\\"a\\\\nb\\\";\\n```", lightTheme)}</div>
        );

        const code = container.querySelector("code");
        expect(code?.textContent).toBe('const value = \\"a\\\\nb\\";');
    });

    it("renders empty fenced code blocks instead of dropping them", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("```ts\n```", lightTheme)}</div>);

        const pre = container.querySelector("pre") as HTMLPreElement;
        const code = container.querySelector("code");
        expect(pre).toBeTruthy();
        expect(pre.textContent).toContain("ts");
        expect(code?.textContent).toBe("\u00A0");
    });

    it("keeps an unfinished Mermaid fence as source until streaming closes it", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("```mermaid\ngraph TD\nA --> B", lightTheme)}</div>,
        );

        expect(container.querySelector("pre")?.textContent).toContain("A --> B");
        expect(screen.queryByTestId("assistant-mermaid-loading")).toBeNull();
        expect(screen.queryByTestId("assistant-mermaid-diagram")).toBeNull();
    });

    it("recognizes a closed tilde-fenced Mermaid block", () => {
        render(
            <div>{renderContentWithCodeBlocks("~~~mermaid\ngraph TD\nA --> B\n~~~", lightTheme)}</div>,
        );

        expect(screen.getByTestId("assistant-mermaid-loading")).toBeTruthy();
    });

    it("does not close a four-backtick Mermaid block at a triple backtick inside its source", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("````mermaid\ngraph TD\n```\nA --> B", lightTheme)}</div>,
        );

        expect(container.querySelector("pre")?.textContent).toContain("A --> B");
        expect(screen.queryByTestId("assistant-mermaid-loading")).toBeNull();
    });

    it("keeps long code blocks constrained to the message width with local horizontal scrolling", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("```text\nThisIsAVeryLongUnbrokenCodeLineThatShouldScrollInsideTheCodeBlockInsteadOfStretchingTheAssistantPanel\n```", lightTheme)}</div>
        );

        const pre = container.querySelector("pre") as HTMLPreElement;
        expect(pre.style.width).toBe("100%");
        expect(pre.style.maxWidth).toBe("100%");
        expect(pre.style.minWidth).toBe("0px");
        expect(pre.style.boxSizing).toBe("border-box");
        expect(pre.style.overflowX).toBe("auto");
        expect(pre.style.overscrollBehaviorX).toBe("contain");
    });

    it("wraps long unbroken plain markdown lines within the message width", () => {
        const longText = "ThisIsAVeryLongUnbrokenPlainMarkdownLineThatShouldWrapInsteadOfStretchingTheAssistantPanel";
        render(<div>{renderContentWithCodeBlocks(longText, lightTheme)}</div>);

        const line = screen.getByText(longText) as HTMLElement;
        expect(line.style.minWidth).toBe("0px");
        expect(line.style.overflowWrap).toBe("anywhere");
        expect(line.style.wordBreak).toBe("break-word");
    });

    it("wraps long heading and numbered markdown content within the message width", () => {
        const headingText = "ThisIsAVeryLongUnbrokenHeadingThatShouldWrapInsideTheAssistantPanel";
        const numberedText = "ThisIsAVeryLongUnbrokenNumberedItemThatShouldWrapInsideTheAssistantPanel";
        render(<div>{renderContentWithCodeBlocks(`#### ${headingText}\n1. ${numberedText}`, lightTheme)}</div>);

        const heading = screen.getByText(headingText) as HTMLElement;
        const numbered = screen.getByText(numberedText) as HTMLElement;
        expect(heading.style.overflowWrap).toBe("anywhere");
        expect(numbered.style.minWidth).toBe("0px");
        expect(numbered.style.overflowWrap).toBe("anywhere");
    });

    it("keeps multi-digit ordered list markers intact and non-wrapping", () => {
        const { container } = render(
            <div>
                {renderContentWithCodeBlocks(
                    "9. 第九条\n10. 世界杯决赛对阵图来了\n11. 查尔斯国王\n100. 百项\n10) paren form\n完成。1. 第一步\n1. a 2. b 10. c\n  12. nested",
                    lightTheme,
                )}
            </div>
        );

        const markers10 = screen.getAllByText("10.") as HTMLElement[];
        const marker10 = markers10[0];
        const marker11 = screen.getByText("11.") as HTMLElement;
        const marker100 = screen.getByText("100.") as HTMLElement;
        const marker10Paren = screen.getByText("10)") as HTMLElement;
        expect(markers10.length).toBe(2); // line-start "10." + compact-line expanded "10."
        expect(marker10.style.whiteSpace).toBe("nowrap");
        expect(marker10.style.wordBreak).toBe("normal");
        expect(marker10.style.overflowWrap).toBe("normal");
        expect(marker10.style.fontVariantNumeric).toBe("tabular-nums");
        expect(marker10.style.width).toBe("");
        expect(marker11.style.whiteSpace).toBe("nowrap");
        expect(marker100.style.whiteSpace).toBe("nowrap");
        // ")" form must not be rewritten to "."
        expect(marker10Paren.textContent).toBe("10)");
        // Normalize must not peel the leading digit into its own line ("1" + "0.").
        expect(screen.queryByText(/^0\./)).toBeNull();
        expect(container.textContent).toContain("10.世界杯决赛对阵图来了");
        expect(container.textContent).toContain("100.百项");
        expect(container.textContent).toContain("10)paren form");
        // Mid-line single-digit glue still becomes a real list row.
        expect(screen.getAllByText("1.").length).toBeGreaterThanOrEqual(1);
        expect(container.textContent).toContain("第一步");
        // Compact multi-item line expands; two-digit item renders as its own marker.
        expect(container.textContent).toContain("10.c");
        expect(screen.getByText("2.")).toBeTruthy();
        // Nested indent is preserved as padding on the row.
        const marker12 = screen.getByText("12.") as HTMLElement;
        expect(marker12.parentElement?.style.paddingLeft).toBe("1em");
    });

    it("wraps long inline code and link text within the message width", () => {
        const codeText = "ThisIsAVeryLongInlineCodeTokenThatShouldWrapInsideTheAssistantPanel";
        const linkText = "ThisIsAVeryLongMarkdownLinkTextThatShouldWrapInsideTheAssistantPanel";
        const { container } = render(<div>{renderContentWithCodeBlocks(`Use \`${codeText}\` and [${linkText}](https://example.com)`, lightTheme)}</div>);

        const code = container.querySelector("code") as HTMLElement;
        const link = screen.getByText(linkText) as HTMLElement;
        expect(code.style.overflowWrap).toBe("anywhere");
        expect(code.style.wordBreak).toBe("break-word");
        expect(link.style.overflowWrap).toBe("anywhere");
        expect(link.style.wordBreak).toBe("break-word");
    });

    it("wraps long inline emphasis within the message width", () => {
        const boldText = "ThisIsAVeryLongBoldTokenThatShouldWrapInsideTheAssistantPanel";
        const italicText = "ThisIsAVeryLongItalicTokenThatShouldWrapInsideTheAssistantPanel";
        const { container } = render(<div>{renderContentWithCodeBlocks(`**${boldText}** and *${italicText}*`, lightTheme)}</div>);

        const strong = container.querySelector("strong") as HTMLElement;
        const em = container.querySelector("em") as HTMLElement;
        expect(strong.textContent).toBe(boldText);
        expect(strong.style.overflowWrap).toBe("anywhere");
        expect(em.textContent).toBe(italicText);
        expect(em.style.overflowWrap).toBe("anywhere");
    });

    it("wraps long path links within the message width", () => {
        render(<div>{renderContentWithCodeBlocks("Open C:\\Users\\demo\\verylongfoldernamewithoutbreaks\\verylongfilenamewithoutbreaks.pdf", lightTheme)}</div>);

        const link = screen.getByTitle("C:\\Users\\demo\\verylongfoldernamewithoutbreaks\\verylongfilenamewithoutbreaks.pdf") as HTMLElement;
        expect(link.style.overflowWrap).toBe("anywhere");
        expect(link.style.wordBreak).toBe("break-word");
    });

    it("keeps KB image thumbnails inside the message width", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("[KB_IMAGE:asset-1|data:image/png;base64,abc|C:\\Users\\demo\\image.png]", lightTheme)}</div>
        );

        const wrapper = container.querySelector("img")?.parentElement as HTMLElement;
        const image = container.querySelector("img") as HTMLImageElement;
        expect(wrapper.style.maxWidth).toBe("100%");
        expect(wrapper.style.minWidth).toBe("0px");
        expect(wrapper.style.overflow).toBe("hidden");
        expect(image.style.width).toBe("120px");
        expect(image.style.maxWidth).toBe("100%");
        expect(image.style.boxSizing).toBe("border-box");
        expect(image.style.display).toBe("block");
    });

    it("keeps screenshot previews inside the message width", () => {
        const { container } = render(<div>{renderScreenshotPreview("abc", "C:\\Users\\demo\\screen.png", vi.fn(), lightTheme)}</div>);

        const wrapper = screen.getByTestId("screenshot-preview-block") as HTMLElement;
        const link = container.querySelector("a") as HTMLAnchorElement;
        const image = container.querySelector("img") as HTMLImageElement;
        expect(wrapper.style.maxWidth).toBe("100%");
        expect(wrapper.style.minWidth).toBe("0px");
        expect(wrapper.style.overflow).toBe("hidden");
        expect(link.style.maxWidth).toBe("100%");
        expect(image.style.width).toBe("180px");
        expect(image.style.maxWidth).toBe("100%");
        expect(image.style.boxSizing).toBe("border-box");
        expect(image.style.display).toBe("block");
    });

    it("splits dense digital employee capability lists into plain dash lists without pictographs", () => {
        render(
            <div>
                {renderContentWithCodeBlocks(
                    "I can help: \u{1f4c1} read local files \u{1f310} search the web \u{1f4ac} answer questions and analyze",
                    lightTheme
                )}
            </div>
        );

        expect(screen.getByText("I can help:")).toBeTruthy();
        expect(screen.getByText("read local files")).toBeTruthy();
        expect(screen.getByText("search the web")).toBeTruthy();
        expect(screen.getByText("answer questions and analyze")).toBeTruthy();
        expect(screen.queryByText(/\u{1f4c1}/u)).toBeNull();
        expect(screen.queryByText(/\u{1f310}/u)).toBeNull();
        expect(screen.queryByText(/\u{1f4ac}/u)).toBeNull();
    });

    it("splits inline markdown headings emitted by digital employees", () => {
        render(
            <div>
                {renderContentWithCodeBlocks(
                    "Weather update: ### Today\nSunny and warm 0%###\u{1f4c5}Tomorrow\nCloudy",
                    lightTheme
                )}
            </div>
        );

        expect(screen.getByText("Weather update:")).toBeTruthy();
        expect(screen.getByText("Today")).toBeTruthy();
        expect(screen.getByText("Sunny and warm 0%")).toBeTruthy();
        // Decorative heading pictograph stripped after compact-heading normalize.
        expect(screen.getByText("Tomorrow")).toBeTruthy();
        expect(screen.queryByText(/\u{1f4c5}/u)).toBeNull();
        expect(screen.getByText("Cloudy")).toBeTruthy();
    });

    it("does not split status marks into list items; renders them as SVG glyphs", () => {
        render(<div>{renderContentWithCodeBlocks("Good job \u2705 keep going", lightTheme)}</div>);

        expect(screen.getByText(/Good job/)).toBeTruthy();
        expect(screen.getByText(/keep going/)).toBeTruthy();
        // Semantic check mark → StatusGlyph SVG, not emoji glyph.
        expect(screen.getByTestId("inline-status-glyph")).toBeTruthy();
        expect(screen.getByTestId("inline-status-glyph").getAttribute("data-status")).toBe("ok");
        expect(screen.queryByText(/\u2705/u)).toBeNull();
    });

    it("normalizes compact markdown headings at the start of a line", () => {
        render(<div>{renderContentWithCodeBlocks("###\u{1f4c5}Today\nClear", lightTheme)}</div>);

        expect(screen.getByText("Today")).toBeTruthy();
        expect(screen.queryByText(/\u{1f4c5}/u)).toBeNull();
        expect(screen.getByText("Clear")).toBeTruthy();
    });

    it.each([
        ["real newline", "####\n\u{1f4ca} Resource usage", "Resource usage", "####"],
        ["escaped newline", "####\\n\u{1f4ca} Resource usage", "Resource usage", "####"],
        ["escaped CRLF", "####\\r\\nSummary", "Summary", "####"],
        ["trailing spaces", "###   \nSummary", "Summary", "###"],
        ["CRLF line ending", "####\r\nSummary", "Summary", "####"],
    ])("attaches bare markdown heading markers with %s", (_label, input, renderedText, marker) => {
        const { container } = render(<div>{renderContentWithCodeBlocks(input, lightTheme)}</div>);

        expect(screen.getByText(renderedText)).toBeTruthy();
        expect(container.textContent).not.toContain(marker);
    });

    it("does not attach bare markdown heading markers inside fenced code blocks", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("```md\n####\nTitle\n```", lightTheme)}</div>);

        const code = container.querySelector("code");
        expect(code?.textContent).toBe("####\nTitle");
    });

    it.each([
        ["real newlines", "####\n####\nSlide 1: Cover\nBody"],
        ["escaped newlines", "####\\n####\\nSlide 1: Cover\\nBody"],
    ])("collapses repeated bare heading markers before the real title line with %s", (_label, input) => {
        const { container } = render(<div>{renderContentWithCodeBlocks(input, lightTheme)}</div>);

        expect(screen.getByText("Slide 1: Cover")).toBeTruthy();
        expect(screen.getByText("Body")).toBeTruthy();
        expect(container.textContent).not.toContain("####");
    });

    it("attaches bare heading markers when the title line is indented after a blank line", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("####\n\n   \u{1F4C4} 幻灯片1：封面\nBody", lightTheme)}</div>);

        expect(screen.getByText("幻灯片1：封面")).toBeTruthy();
        expect(screen.queryByText(/\u{1F4C4}/u)).toBeNull();
        expect(screen.getByText("Body")).toBeTruthy();
        expect(container.textContent).not.toContain("####");
    });

    it("keeps a trailing bare heading marker when there is no title line to attach", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("Before\n####", lightTheme)}</div>);

        expect(screen.getByText("Before")).toBeTruthy();
        expect(container.textContent).toContain("####");
    });

    it("keeps repeated trailing bare heading markers when there is no title line to attach", () => {
        render(<div>{renderContentWithCodeBlocks("Before\n####\n\n#####", lightTheme)}</div>);

        expect(screen.getByText("Before")).toBeTruthy();
        expect(screen.getByText("####")).toBeTruthy();
        expect(screen.getByText("#####")).toBeTruthy();
    });

    it("uses the nearest repeated bare heading marker level before the title line", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("####\n#####\nNested title", lightTheme)}</div>);

        const heading = screen.getByText("Nested title");
        expect(heading).toBeTruthy();
        expect((heading as HTMLElement).style.fontSize).toBe("0.9em");
        expect(container.textContent).not.toContain("####");
    });

    it("uses the nearest marker level when repeated bare heading markers share one line", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("#### #####\nNested title", lightTheme)}</div>);

        const heading = screen.getByText("Nested title");
        expect(heading).toBeTruthy();
        expect((heading as HTMLElement).style.fontSize).toBe("0.9em");
        expect(container.textContent).not.toContain("####");
    });

    it("does not attach a bare heading marker to a following fenced code block", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("####\n```ts\nconst ok = true;\n```", lightTheme)}</div>);

        expect(screen.getByText("####")).toBeTruthy();
        expect(container.querySelector("code")?.textContent).toBe("const ok = true;");
    });

    it("does not attach a bare heading marker to a following markdown table", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("####\n| Name | Status |\n| --- | --- |\n| Alpha | Ready |", lightTheme)}</div>);

        expect(screen.getByText("####")).toBeTruthy();
        expect(container.querySelector("table")).toBeTruthy();
        expect(screen.getByText("Alpha")).toBeTruthy();
    });

    it("does not attach a bare heading marker to a following markdown table without outer pipes", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("####\nName | Status\n--- | ---\nAlpha | Ready", lightTheme)}</div>);

        expect(screen.getByText("####")).toBeTruthy();
        expect(container.querySelector("table")).toBeTruthy();
        expect(screen.getByText("Name")).toBeTruthy();
        expect(screen.getByText("Alpha")).toBeTruthy();
    });

    it("can attach a bare heading marker to ordinary pipe text that is not a table", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("####\nInput | Output semantics", lightTheme)}</div>);

        expect(screen.getByText("Input | Output semantics")).toBeTruthy();
        expect(container.querySelector("table")).toBeNull();
        expect(container.textContent).not.toContain("####");
    });

    it("can attach a bare heading marker to escaped pipe text before a separator-looking line", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("####\nInput \\| Output semantics\n--- | ---", lightTheme)}</div>);

        expect(screen.getByText("Input \\| Output semantics")).toBeTruthy();
        expect(container.querySelector("table")).toBeNull();
        expect(container.textContent).not.toContain("####");
    });

    it("does not attach a bare heading marker to a following markdown heading", () => {
        render(<div>{renderContentWithCodeBlocks("####\n### Existing title", lightTheme)}</div>);

        expect(screen.getByText("####")).toBeTruthy();
        expect(screen.getByText("Existing title")).toBeTruthy();
    });

    it("attaches a bare heading marker to a following list-marked title", () => {
        // Digital employees commonly emit section titles as bare ### + "- title".
        const { container } = render(<div>{renderContentWithCodeBlocks("###\n- 北京·城区天气预报", lightTheme)}</div>);

        expect(screen.getByText("北京·城区天气预报")).toBeTruthy();
        expect(container.textContent).not.toContain("###");
        // List marker is consumed into the heading (not left as a bullet row).
        expect(container.textContent).not.toMatch(/^[•·]/m);
    });

    it("attaches a bare heading marker to a unicode-bullet title", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("###\n\u2022 \u4eca\u65e5\u751f\u6d3b\u6307\u6570", lightTheme)}</div>,
        );

        expect(screen.getByText("\u4eca\u65e5\u751f\u6d3b\u6307\u6570")).toBeTruthy();
        expect(container.textContent).not.toContain("###");
    });

    it("renders digital-employee weather tables without a GFM separator", () => {
        const content = [
            "好的，我来查一下北京天气。",
            "|---|---|---|",
            "|---|---|---|",
            "",
            "###",
            "- 北京·城区天气预报（7月24日更新）",
            "| 日期 | 天气 | 温度 | 风力 |",
            "今天 (24日) | 雷阵雨转多云",
            "→| 30°C / 23°C | <3级 |",
            "明天 (25日) | 雷阵雨转多云",
            "→| 30°C / 24°C | <3级 |",
            // No wrap glyph — continuation still starts with "|".
            "周一 (27日) | 多云",
            "| 33°C / 25°C | <3级 |",
            "",
            "###",
            "- 今日生活指数",
            "- 这几天都有雷阵雨，出门一定带伞！",
            "\u2022 \u4f53\u611f\u8f83\u70ed\uff0830\u00b0C\u5de6\u53f3\uff09",
        ].join("\n");

        const { container } = render(<div>{renderContentWithCodeBlocks(content, lightTheme)}</div>);

        // Bare ### should not leak; titles become real headings.
        expect(container.textContent).not.toContain("###");
        expect(screen.getByText("北京·城区天气预报（7月24日更新）")).toBeTruthy();
        expect(screen.getByText("今日生活指数")).toBeTruthy();
        // Orphan separator noise discarded.
        expect(container.textContent).not.toMatch(/\|-{3,}/);
        // Weather grid is a real table with merged split rows (glyph + no-glyph).
        const table = screen.getByTestId("markdown-table") as HTMLTableElement;
        expect(table.querySelectorAll("thead th")).toHaveLength(4);
        expect(table.querySelectorAll("tbody tr")).toHaveLength(3);
        expect(table.textContent).toContain("日期");
        expect(table.textContent).toContain("今天 (24日)");
        expect(table.textContent).toContain("30°C / 23°C");
        expect(table.textContent).toContain("明天 (25日)");
        expect(table.textContent).toContain("周一 (27日)");
        expect(table.textContent).toContain("33°C / 25°C");
        // Remaining list items still render as bullets (ASCII + unicode).
        expect(screen.getByText(/这几天都有雷阵雨/)).toBeTruthy();
        expect(screen.getByText(/\u4f53\u611f\u8f83\u70ed/)).toBeTruthy();
    });

    it("splits compact emoji headings even when the previous text has no punctuation", () => {
        render(<div>{renderContentWithCodeBlocks("晴天###\u{1f4c5}明天\n多云", lightTheme)}</div>);

        expect(screen.getByText("晴天")).toBeTruthy();
        expect(screen.getByText("明天")).toBeTruthy();
        expect(screen.queryByText(/\u{1f4c5}/u)).toBeNull();
        expect(screen.getByText("多云")).toBeTruthy();
    });

    it("splits compact markdown headings after sentence text", () => {
        render(<div>{renderContentWithCodeBlocks("previous text###Next heading", lightTheme)}</div>);

        expect(screen.getByText("previous text")).toBeTruthy();
        expect(screen.getByText("Next heading")).toBeTruthy();
    });

    it("supports compact markdown heading levels without keyword lists", () => {
        render(<div>{renderContentWithCodeBlocks("Intro##Second level\nBody#####Fifth level", lightTheme)}</div>);

        expect(screen.getByText("Intro")).toBeTruthy();
        expect(screen.getByText("Second level")).toBeTruthy();
        expect(screen.getByText("Body")).toBeTruthy();
        expect(screen.getByText("Fifth level")).toBeTruthy();
    });

    it("does not split compact heading markers before digits or punctuation", () => {
        render(<div>{renderContentWithCodeBlocks("issue ##42 and value###.1 remain inline", lightTheme)}</div>);

        expect(screen.getByText("issue ##42 and value###.1 remain inline")).toBeTruthy();
    });

    it("does not split compact heading markers inside inline code", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("Use `foo###bar` then continue", lightTheme)}</div>);

        expect(container.querySelector("code")?.textContent).toBe("foo###bar");
        expect(screen.getByText(/Use/).textContent).toContain("foo###bar");
    });

    it("does not split compact heading markers inside URLs", () => {
        render(<div>{renderContentWithCodeBlocks("See https://example.com/a###anchor now", lightTheme)}</div>);

        expect(screen.getByText("See https://example.com/a###anchor now")).toBeTruthy();
    });

    it("does not split compact heading markers inside markdown links", () => {
        render(<div>{renderContentWithCodeBlocks("Open [foo###bar](https://example.com/a###anchor) now", lightTheme)}</div>);

        expect(screen.getByText(/Open/).textContent).toContain("foo###bar");
        expect(screen.getByText("foo###bar")).toBeTruthy();
    });

    it("does not split compact heading markers inside markdown image syntax", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("Preview ![foo###bar](https://example.com/a###anchor.png) now", lightTheme)}</div>);

        expect(screen.getByText(/Preview/).textContent).toContain("foo###bar");
        expect(container.querySelectorAll("div")).toHaveLength(2);
    });

    it("does not split compact heading markers inside inline emphasis", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("Keep **foo###bar** and *baz###qux* inline", lightTheme)}</div>);

        expect(container.querySelector("strong")?.textContent).toBe("foo###bar");
        expect(container.querySelector("em")?.textContent).toBe("baz###qux");
    });

    it("does not corrupt text that resembles protected markdown placeholder tokens", () => {
        render(<div>{renderContentWithCodeBlocks("Keep __MACLAW_MD_PROTECTED__0__ and `foo###bar`", lightTheme)}</div>);

        expect(screen.getByText(/Keep/).textContent).toContain("__MACLAW_MD_PROTECTED__0__");
        expect(screen.getByText(/Keep/).textContent).toContain("foo###bar");
    });

    it("does not treat ordinary hashtags as compact markdown headings", () => {
        render(<div>{renderContentWithCodeBlocks("#topic remains inline", lightTheme)}</div>);

        expect(screen.getByText("#topic remains inline")).toBeTruthy();
    });

    it("does not treat C# text as a compact markdown heading", () => {
        render(<div>{renderContentWithCodeBlocks("熟悉 C# 开发和 .NET", lightTheme)}</div>);

        expect(screen.getByText("熟悉 C# 开发和 .NET")).toBeTruthy();
    });

    it("does not split a single capability icon used inline; decorative mark is stripped", () => {
        render(<div>{renderContentWithCodeBlocks("Open the \u{1f4c1} folder", lightTheme)}</div>);

        // Single mid-sentence pictograph is not rewritten into a list item.
        expect(screen.queryByText(/^- /)).toBeNull();
        // Decorative folder mark is stripped (product UI: no emoji chrome).
        expect(screen.getByText("Open the folder")).toBeTruthy();
        expect(screen.queryByText(/\u{1f4c1}/u)).toBeNull();
    });

    it("does not rewrite escaped separators inside Windows paths", () => {
        render(<div>{renderContentWithCodeBlocks("Open C:\\Users\\demo\\notes\\report.pdf then continue", lightTheme)}</div>);

        expect(screen.getByTitle("C:\\Users\\demo\\notes\\report.pdf")).toBeTruthy();
        expect(screen.queryByText("Users")).toBeNull();
    });

    it("renders single-quoted Windows folder paths in bold text as clickable links", () => {
        render(<div>{renderContentWithCodeBlocks("Saved to: **'C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\'**", lightTheme)}</div>);

        const link = screen.getByTitle("C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\");
        expect(link.tagName).toBe("A");
        expect(link.textContent).toContain("C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\");
        expect(link.textContent).not.toContain("'");
        fireEvent.click(link);
        expect(openFileOrShowInFolderMock).toHaveBeenCalledWith("C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\");
    });

    it("renders single-quoted bare Windows folder paths as clickable links", () => {
        render(<div>{renderContentWithCodeBlocks("Saved to: 'C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\', open it.", lightTheme)}</div>);

        const link = screen.getByTitle("C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\");
        expect(link.tagName).toBe("A");
        fireEvent.click(link);
        expect(openFileOrShowInFolderMock).toHaveBeenCalledWith("C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\");
    });

    it("renders single-quoted Windows file paths as clickable links", () => {
        render(<div>{renderContentWithCodeBlocks("Saved file: 'C:\\Users\\ma139\\Desktop\\report.pdf', open it.", lightTheme)}</div>);

        const link = screen.getByTitle("C:\\Users\\ma139\\Desktop\\report.pdf");
        expect(link.tagName).toBe("A");
        expect(link.textContent).not.toContain("'");
        fireEvent.click(link);
        expect(openFileOrShowInFolderMock).toHaveBeenCalledWith("C:\\Users\\ma139\\Desktop\\report.pdf");
    });

    it("keeps sentence punctuation out of quoted Windows path links", () => {
        render(<div>{renderContentWithCodeBlocks("Saved file: **'C:\\Users\\ma139\\Desktop\\report.pdf'.**", lightTheme)}</div>);

        const link = screen.getByTitle("C:\\Users\\ma139\\Desktop\\report.pdf");
        expect(link.tagName).toBe("A");
        expect(link.textContent).toContain("C:\\Users\\ma139\\Desktop\\report.pdf");
        expect(link.textContent).not.toContain("'");
    });

    it("uses the cleaned path when falling back to ShowItemInFolder", async () => {
        openFileOrShowInFolderMock.mockRejectedValueOnce(new Error("open failed"));
        render(<div>{renderContentWithCodeBlocks("Saved to: 'C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\', open it.", lightTheme)}</div>);

        fireEvent.click(screen.getByTitle("C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\"));

        await waitFor(() => {
            expect(showItemInFolderMock).toHaveBeenCalledWith("C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\");
        });
    });

    it("renders portable home paths (~/) as clickable links", () => {
        render(<div>{renderContentWithCodeBlocks("工作目录：~/.maclaw/workspace/self_evolving_papers/", lightTheme)}</div>);

        const link = screen.getByTitle("~/.maclaw/workspace/self_evolving_papers/");
        expect(link.tagName).toBe("A");
        fireEvent.click(link);
        expect(openFileOrShowInFolderMock).toHaveBeenCalledWith("~/.maclaw/workspace/self_evolving_papers/");
    });

    it("renders Windows-style home paths (~\\) as clickable links", () => {
        render(<div>{renderContentWithCodeBlocks("Open ~\\.maclaw\\workspace\\notes", lightTheme)}</div>);

        const link = screen.getByTitle("~\\.maclaw\\workspace\\notes");
        expect(link.tagName).toBe("A");
        fireEvent.click(link);
        expect(openFileOrShowInFolderMock).toHaveBeenCalledWith("~\\.maclaw\\workspace\\notes");
    });

    it("renders bare home directories inside code blocks as clickable links", () => {
        render(<div>{renderContentWithCodeBlocks("```text\n~/.maclaw/workspace/self_evolving_papers/\n```", lightTheme)}</div>);

        const link = screen.getByTitle("~/.maclaw/workspace/self_evolving_papers/");
        expect(link.tagName).toBe("A");
        fireEvent.click(link);
        expect(openFileOrShowInFolderMock).toHaveBeenCalledWith("~/.maclaw/workspace/self_evolving_papers/");
    });

    it("renders single-quoted Windows paths inside code blocks without keeping quote wrappers", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("```text\n'C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\'\n```", lightTheme)}</div>);

        const link = screen.getByTitle("C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\");
        expect(link.tagName).toBe("A");
        expect(container.querySelector("code")?.textContent).toBe("C:\\Users\\ma139\\Desktop\\2602.06052v3_pages\\");
    });

    it("renders GitHub-style pipe tables without leading outer pipes", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("Name | Status\n--- | ---\nAlpha | Ready\nBeta | Waiting", lightTheme)}</div>
        );

        expect(container.querySelector("table")).toBeTruthy();
        expect(screen.getByText("Name")).toBeTruthy();
        expect(screen.getByText("Ready")).toBeTruthy();
        expect(screen.getByText("Waiting")).toBeTruthy();
    });

    it("keeps table header and body columns aligned when cells contain long text", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("Title | Content\n--- | ---\nLong | ThisIsAVeryLongUnbrokenDigitalEmployeeTableCellThatShouldWrapInsideItsColumn", lightTheme)}</div>
        );

        const table = container.querySelector("table") as HTMLTableElement;
        const wrapper = screen.getByTestId("markdown-table-block") as HTMLElement;
        const th = container.querySelector("th") as HTMLTableCellElement;
        const td = container.querySelector("td") as HTMLTableCellElement;
        expect(wrapper.style.width).toBe("100%");
        expect(wrapper.style.minWidth).toBe("0px");
        expect(wrapper.style.boxSizing).toBe("border-box");
        expect(wrapper.style.overflowX).toBe("auto");
        expect(wrapper.style.overscrollBehaviorX).toBe("contain");
        expect(table.style.tableLayout).toBe("fixed");
        expect(table.style.minWidth).toBe("360px");
        expect(th.style.overflowWrap).toBe("anywhere");
        expect(td.style.wordBreak).toBe("break-word");
        expect(td.style.verticalAlign).toBe("top");
    });

    it("preserves extra body columns so cells do not shift under the wrong header", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("Name | Status\n--- | ---\nAlpha | Ready | Notes", lightTheme)}</div>
        );

        expect(container.querySelectorAll("th")).toHaveLength(3);
        expect(container.querySelectorAll("td")).toHaveLength(3);
        expect(screen.getByText("Notes")).toBeTruthy();
    });

    it("repairs streamed table rows whose leading cell arrives on a separate line", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("日期 | 天气 | 气温 | 风力\n--- | --- | --- | ---\n|今天|\n|- 阴转雷阵雨 | 24~29°C | 东风 1-3级|\n|明天|\n|- 小雨转多云 | 24~32°C | 东北风 1-3级|", lightTheme)}</div>
        );

        const rows = Array.from(container.querySelectorAll("tbody tr"));
        expect(rows).toHaveLength(2);
        expect(Array.from(rows[0].querySelectorAll("td")).map(cell => cell.textContent)).toEqual(["今天", "- 阴转雷阵雨", "24~29°C", "东风 1-3级"]);
        expect(Array.from(rows[1].querySelectorAll("td")).map(cell => cell.textContent)).toEqual(["明天", "- 小雨转多云", "24~32°C", "东北风 1-3级"]);
    });

    it("repairs weather-style split rows with continuation markers (→ / -)", () => {
        // Digital employees often emit one forecast day as two partial rows:
        // date+condition, then "→" / "-" plus temperature and wind.
        const markdown = [
            "| 日期 | 天气 | 温度 | 风力 |",
            "| --- | --- | --- | --- |",
            "| 今天 (14日) | 多云转晴 |",
            "| → | 34°C / 22°C | <3级 |",
            "| 明天 (15日) | 晴 |",
            "| - | 35°C / 23°C | <3级 |",
            "| 后天 (16日) | 晴转多云 |",
            "| → | 34°C / 24°C | <3级 |",
        ].join("\n");
        const { container } = render(
            <div>{renderContentWithCodeBlocks(markdown, lightTheme)}</div>
        );

        const rows = Array.from(container.querySelectorAll("tbody tr"));
        expect(rows).toHaveLength(3);
        expect(Array.from(rows[0].querySelectorAll("td")).map(cell => cell.textContent)).toEqual([
            "今天 (14日)", "多云转晴", "34°C / 22°C", "<3级",
        ]);
        expect(Array.from(rows[1].querySelectorAll("td")).map(cell => cell.textContent)).toEqual([
            "明天 (15日)", "晴", "35°C / 23°C", "<3级",
        ]);
        expect(Array.from(rows[2].querySelectorAll("td")).map(cell => cell.textContent)).toEqual([
            "后天 (16日)", "晴转多云", "34°C / 24°C", "<3级",
        ]);
        // Continuation markers must not appear as a date-column value.
        const cellTexts = Array.from(container.querySelectorAll("td")).map(cell => cell.textContent || "");
        expect(cellTexts.some(text => text === "→" || text === "-")).toBe(false);
    });

    it("without a wrap marker only merges the classic 1+(N-1) streamed form", () => {
        // Two independent short rows must stay separate (key|val style tables).
        const independent = [
            "| Key | Val | Key2 | Val2 |",
            "| --- | --- | --- | --- |",
            "| alpha | 1 |",
            "| beta | 2 |",
        ].join("\n");
        const { container: independentContainer } = render(
            <div>{renderContentWithCodeBlocks(independent, lightTheme)}</div>
        );
        expect(independentContainer.querySelectorAll("tbody tr")).toHaveLength(2);

        // Classic label-then-rest still merges without a marker.
        const classic = [
            "日期 | 天气 | 温度 | 风力",
            "--- | --- | --- | ---",
            "| 17日 (周五) |",
            "| 多云转雷阵雨 | 34°C / 22°C | <3级 |",
        ].join("\n");
        const { container } = render(
            <div>{renderContentWithCodeBlocks(classic, lightTheme)}</div>
        );
        const rows = Array.from(container.querySelectorAll("tbody tr"));
        expect(rows).toHaveLength(1);
        expect(Array.from(rows[0].querySelectorAll("td")).map(cell => cell.textContent)).toEqual([
            "17日 (周五)", "多云转雷阵雨", "34°C / 22°C", "<3级",
        ]);
    });

    it("keeps list-prefixed pipe rows inside the table and repairs them", () => {
        // Day-20 style tails: model switches to "- cell | cell" mid-forecast.
        const markdown = [
            "| 日期 | 天气 | 温度 | 风力 |",
            "| --- | --- | --- | --- |",
            "| 19日 (周日) | 多云 |",
            "| - | 30°C / 21°C | <3级 |",
            "- 20日 (周一) | 雷阵雨转多云 |",
            "- → | 29°C / 22°C | <3级 |",
            "1. 21日 (周二) | 晴 |",
            "2. → | 28°C / 20°C | <3级 |",
        ].join("\n");
        const { container } = render(
            <div>{renderContentWithCodeBlocks(markdown, lightTheme)}</div>
        );

        const rows = Array.from(container.querySelectorAll("tbody tr"));
        expect(rows).toHaveLength(3);
        expect(Array.from(rows[0].querySelectorAll("td")).map(cell => cell.textContent)).toEqual([
            "19日 (周日)", "多云", "30°C / 21°C", "<3级",
        ]);
        expect(Array.from(rows[1].querySelectorAll("td")).map(cell => cell.textContent)).toEqual([
            "20日 (周一)", "雷阵雨转多云", "29°C / 22°C", "<3级",
        ]);
        expect(Array.from(rows[2].querySelectorAll("td")).map(cell => cell.textContent)).toEqual([
            "21日 (周二)", "晴", "28°C / 20°C", "<3级",
        ]);
        expect(container.textContent || "").not.toMatch(/•\s*20日/);
    });

    it("does not treat leading empty cells alone as a wrap marker for multi-cell joins", () => {
        // Without a glyph marker, "| a | b |" + "|  | c | d |" must stay two rows.
        const markdown = [
            "| Key | Val | Key2 | Val2 |",
            "| --- | --- | --- | --- |",
            "| alpha | 1 |",
            "|  | beta | 2 |",
        ].join("\n");
        const { container } = render(
            <div>{renderContentWithCodeBlocks(markdown, lightTheme)}</div>
        );
        expect(container.querySelectorAll("tbody tr")).toHaveLength(2);
    });

    it("repairs split rows that pad trailing empty cells on either half", () => {
        const markdown = [
            "| 日期 | 天气 | 温度 | 风力 |",
            "| --- | --- | --- | --- |",
            "| 今天 (14日) | 多云转晴 | |",
            "| → | 34°C / 22°C | <3级 | |",
        ].join("\n");
        const { container } = render(
            <div>{renderContentWithCodeBlocks(markdown, lightTheme)}</div>
        );

        const rows = Array.from(container.querySelectorAll("tbody tr"));
        expect(rows).toHaveLength(1);
        expect(Array.from(rows[0].querySelectorAll("td")).map(cell => cell.textContent)).toEqual([
            "今天 (14日)", "多云转晴", "34°C / 22°C", "<3级",
        ]);
    });

    it("does not glue two complete body rows that already fill the columns", () => {
        const markdown = [
            "| 日期 | 天气 | 温度 | 风力 |",
            "| --- | --- | --- | --- |",
            "| 今天 | 晴 | 30°C | <3级 |",
            "| 明天 | 雨 | 28°C | 3-4级 |",
        ].join("\n");
        const { container } = render(
            <div>{renderContentWithCodeBlocks(markdown, lightTheme)}</div>
        );

        const rows = Array.from(container.querySelectorAll("tbody tr"));
        expect(rows).toHaveLength(2);
        expect(Array.from(rows[0].querySelectorAll("td")).map(cell => cell.textContent)).toEqual([
            "今天", "晴", "30°C", "<3级",
        ]);
        expect(Array.from(rows[1].querySelectorAll("td")).map(cell => cell.textContent)).toEqual([
            "明天", "雨", "28°C", "3-4级",
        ]);
    });

    it("does not treat weather text that starts with a dash as a wrap marker", () => {
        // "- 阴转雷阵雨" is real cell content, not a pure "-" continuation glyph.
        const { container } = render(
            <div>{renderContentWithCodeBlocks("日期 | 天气 | 气温 | 风力\n--- | --- | --- | ---\n|今天|\n|- 阴转雷阵雨 | 24~29°C | 东风 1-3级|", lightTheme)}</div>
        );

        const cells = Array.from(container.querySelectorAll("tbody td")).map(cell => cell.textContent);
        expect(cells).toEqual(["今天", "- 阴转雷阵雨", "24~29°C", "东风 1-3级"]);
    });

    it("preserves escaped pipes and backslashes while repairing streamed table rows", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("日期 | 天气 | 路径\n--- | --- | ---\n|今天|\n|雷雨\\|大风 | C:\\weather\\today|", lightTheme)}</div>
        );

        const cells = Array.from(container.querySelectorAll("tbody td")).map(cell => cell.textContent);
        expect(cells).toEqual(["今天", "雷雨|大风", "C:\\weather\\today"]);
    });

    it("keeps escaped pipes inside table cells", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("Name | Note\n--- | ---\nAlpha | A \\| B", lightTheme)}</div>
        );

        expect(container.querySelectorAll("th")).toHaveLength(2);
        expect(container.querySelectorAll("td")).toHaveLength(2);
        expect(screen.getByText("A | B")).toBeTruthy();
    });

    it("keeps escaped trailing pipes inside table cells", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("Name | Note\n--- | ---\nAlpha | Ends with \\|", lightTheme)}</div>
        );

        expect(container.querySelectorAll("td")).toHaveLength(2);
        expect(screen.getByText("Ends with |")).toBeTruthy();
    });

    it("uses markdown table alignment markers for headers and cells", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("Name | Count | Score\n:--- | ---: | :---:\nAlpha | 12 | 98", lightTheme)}</div>
        );

        const headers = Array.from(container.querySelectorAll("th"));
        const cells = Array.from(container.querySelectorAll("td"));
        expect(headers.map(cell => (cell as HTMLElement).style.textAlign)).toEqual(["left", "right", "center"]);
        expect(cells.map(cell => (cell as HTMLElement).style.textAlign)).toEqual(["left", "right", "center"]);
    });

    it("renders escaped-newline GitHub-style pipe tables from digital employee text", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("Name | Status\\n--- | ---\\nAlpha | Ready", lightTheme)}</div>
        );

        expect(container.querySelector("table")).toBeTruthy();
        expect(screen.getByText("Alpha")).toBeTruthy();
    });

    it("keeps ordinary pipe text as a plain line when it is not a markdown table", () => {
        const { container } = render(<div>{renderContentWithCodeBlocks("Use A | B as a label", lightTheme)}</div>);

        expect(container.querySelector("table")).toBeNull();
        expect(screen.getByText("Use A | B as a label")).toBeTruthy();
    });

    it("renders compact digital-employee pipe tables that lost line breaks", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("北京天气|项目|详情|---|---|天气|小阵雨|温度|17C", lightTheme)}</div>
        );

        expect(container.querySelector("table")).toBeTruthy();
        expect(screen.getByText("项目")).toBeTruthy();
        expect(screen.getByText("小阵雨")).toBeTruthy();
    });

    it("renders compact tables with doubled pipe row boundaries", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("||项目详情|---|---||天气|小阵雨||湿度|100%", lightTheme)}</div>
        );

        expect(container.querySelector("table")).toBeTruthy();
        expect(screen.getByText("天气")).toBeTruthy();
        expect(screen.getByText("100%")).toBeTruthy();
    });

    it("repairs prose accidentally placed in the first table header by structure", () => {
        render(
            <div>{renderContentWithCodeBlocks("| This introductory sentence should be outside the grid | Column A | Column B |\n| --- | --- | --- |\n| Alpha | Beta | trailing note |", lightTheme)}</div>
        );

        const prefix = screen.getByTestId("markdown-table-prefix") as HTMLElement;
        const note = screen.getByTestId("markdown-table-note") as HTMLElement;
        expect(prefix.textContent).toContain("introductory sentence");
        expect(prefix.style.overflowWrap).toBe("anywhere");
        expect(note.textContent).toContain("trailing note");
        expect(note.style.overflowWrap).toBe("anywhere");
        expect(screen.getByText("Column A")).toBeTruthy();
        expect(screen.getByTestId("markdown-table").querySelector("thead")?.textContent).not.toContain("introductory sentence");
    });

    it("keeps long title-like table headers inside normal tables", () => {
        render(
            <div>{renderContentWithCodeBlocks("| Internationalization Compatibility Matrix | Column A | Column B |\n| --- | --- | --- |\n| Alpha | Beta | Gamma |", lightTheme)}</div>
        );

        expect(screen.queryByTestId("markdown-table-prefix")).toBeNull();
        expect(screen.getByText("Internationalization Compatibility Matrix")).toBeTruthy();
    });
});

describe("renderMessage assistant display guard", () => {
    it("renders legacy guide receipts as compact status instead of a system card", () => {
        render(<div>{renderMessage({
            id: "guide-receipt",
            role: "system",
            kind: "guideReceipt",
            content: "这条补充已接上当前任务：\n> 可以顺重搜索一下相关申报软件的资料。\n\n下一步会顺着这点继续。",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "zh", false)}</div>);

        const receipt = screen.getByTestId("assistant-chat-system-guide-receipt") as HTMLElement;
        expect(receipt.getAttribute("role")).toBe("status");
        expect(receipt.getAttribute("aria-live")).toBeNull();
        expect(receipt.textContent || "").toContain("这条补充已接上当前任务");
        expect(receipt.textContent || "").toContain("下一步会顺着这点继续");
        expect(receipt.textContent || "").toContain("可以顺重搜索一下相关申报软件的资料。");
        expect(receipt.style.border).toBe("");
        expect(receipt.style.background).toBe("");
        expect(receipt.style.padding).toBe("");
        const quote = screen.getByText("可以顺重搜索一下相关申报软件的资料。") as HTMLElement;
        expect(quote.style.fontStyle).toBe("italic");
        expect(quote.style.opacity).toBe("");
    });

    it("keeps guide receipt detail even when it repeats the title text", () => {
        render(<div>{renderMessage({
            id: "guide-receipt-repeated-detail",
            role: "system",
            kind: "guideReceipt",
            content: "这条补充已接上当前任务：\n> 补充内容\n\n这条补充已接上当前任务：",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "zh", false)}</div>);

        const receiptText = screen.getByTestId("assistant-chat-system-guide-receipt-repeated-detail").textContent || '';
        expect(receiptText).toContain("这条补充已接上当前任务");
        expect(receiptText).toContain("这条补充已接上当前任务：");
    });

    it("keeps legacy guide receipt detail intact", () => {
        const longQuote = `请优先核对资料来源${"，并标注出处".repeat(20)}。TAIL_SHOULD_STAY_IN_TITLE_ONLY`;
        render(<div>{renderMessage({
            id: "guide-receipt-long",
            role: "system",
            kind: "guideReceipt",
            content: `这条补充已接上当前任务：\n> ${longQuote}\n\n下一步会顺着这点继续。`,
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "zh", false)}</div>);

        const receipt = screen.getByTestId("assistant-chat-system-guide-receipt-long") as HTMLElement;
        const quotePreview = receipt.querySelector("div > div:nth-child(2)") as HTMLElement;
        expect(quotePreview.textContent || "").toContain("请优先核对资料来源");
        expect(quotePreview.textContent || "").toContain("TAIL_SHOULD_STAY_IN_TITLE_ONLY");
        expect(quotePreview.getAttribute("title")).toBeNull();
        expect(quotePreview.getAttribute("aria-label")).toBeNull();
    });

    it("strips Browser role prefixes in the main assistant message path", () => {
        render(<div>{renderMessage({
            id: "assistant-browser-prefix",
            role: "assistant",
            content: "现在情况清楚了。\n\nBrowser: 重复回答。",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        expect(screen.getByText("现在情况清楚了。")).toBeTruthy();
        expect(screen.queryByText(/Browser:/)).toBeNull();
    });

    it("strips leading decorative pictographs from assistant body display", () => {
        render(<div>{renderMessage({
            id: "assistant-leading-emoji",
            role: "assistant",
            content: "\u{1F680} 已完成部署。",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        expect(screen.getByText("已完成部署。")).toBeTruthy();
        expect(screen.queryByText(/\u{1F680}/u)).toBeNull();
    });

    it("strips line-leading pictographs after markdown list/heading prefixes", () => {
        render(<div>{renderMessage({
            id: "assistant-line-leading-emoji",
            role: "assistant",
            content: "### \u{1F3AF} 目标\n\n- \u{1F4CC} 第一项\n- 第二项",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        expect(screen.getByText("目标")).toBeTruthy();
        expect(screen.getByText(/第一项/)).toBeTruthy();
        expect(screen.queryByText(/\u{1F3AF}/u)).toBeNull();
        expect(screen.queryByText(/\u{1F4CC}/u)).toBeNull();
    });

    it("strips mid-sentence decorative pictographs from assistant body display", () => {
        render(<div>{renderMessage({
            id: "assistant-mid-decorative",
            role: "assistant",
            content: "完全可以，搭配非常棒！\u{1F44D}\n赶紧安排\u{1F60A}",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        expect(screen.getByText(/完全可以/)).toBeTruthy();
        expect(screen.getByText(/赶紧安排/)).toBeTruthy();
        expect(screen.queryByText(/\u{1F44D}/u)).toBeNull();
        expect(screen.queryByText(/\u{1F60A}/u)).toBeNull();
    });

    it("maps status and star marks to SVG glyphs in assistant body display", () => {
        // Keep below the dense-capability-list threshold (2+ pictographs used as list markers).
        render(<div>{renderMessage({
            id: "assistant-status-star",
            role: "assistant",
            content: "评分 \u2B50\u2B50 很高。",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        expect(screen.getByText(/评分/)).toBeTruthy();
        expect(screen.getByText(/很高/)).toBeTruthy();
        expect(screen.getAllByTestId("inline-star-glyph").length).toBe(2);
        expect(screen.queryByText(/\u2B50/u)).toBeNull();
    });

    it("maps check and warn marks to StatusGlyph SVG in table-like prose", () => {
        render(<div>{renderMessage({
            id: "assistant-status-marks",
            role: "assistant",
            content: "\u2705 醋可以放\n\u26A0 香油少放\n\u274C 白糖不放",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        const statusGlyphs = screen.getAllByTestId("inline-status-glyph");
        expect(statusGlyphs.length).toBe(3);
        expect(statusGlyphs.map((el) => el.getAttribute("data-status"))).toEqual(["ok", "warn", "error"]);
        expect(screen.queryByText(/\u2705/u)).toBeNull();
        expect(screen.queryByText(/\u26A0/u)).toBeNull();
        expect(screen.queryByText(/\u274C/u)).toBeNull();
        expect(screen.getByText(/醋可以放/)).toBeTruthy();
        expect(screen.getByText(/香油少放/)).toBeTruthy();
        expect(screen.getByText(/白糖不放/)).toBeTruthy();
    });

    it("keeps pictographs inside fenced code blocks", () => {
        render(<div>{renderMessage({
            id: "assistant-fence-emoji",
            role: "assistant",
            content: "Intro\n\n```\n\u{1F680} keep\n```",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        expect(screen.getByText(/keep/)).toBeTruthy();
        // Code block text is rendered as a single pre/code node.
        expect(screen.getByText(/\u{1F680} keep/u)).toBeTruthy();
    });

    it("does not strip leading pictographs from user messages", () => {
        render(<div>{renderMessage({
            id: "user-leading-emoji",
            role: "user",
            content: "\u{1F680} 请部署",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "zh", false)}</div>);

        expect(screen.getByText(/\u{1F680} 请部署/u)).toBeTruthy();
    });

    it("renders the assistant and user in labelled, opposing chat bubbles", () => {
        const { rerender } = render(<div>{renderMessage({
            id: "user-bubble",
            role: "user",
            content: "Please review this",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "en", false)}</div>);

        const userGroup = screen.getByTestId("assistant-chat-user-user-bubble");
        expect(userGroup.getAttribute("role")).toBe("group");
        expect(userGroup.getAttribute("aria-label")).toBe("Your message");
        expect(userGroup.style.alignItems).toBe("flex-end");
        expect(screen.getByText("You")).toBeTruthy();
        const userBubble = screen.getByTestId("assistant-chat-user-bubble-user-bubble") as HTMLElement;
        expect(userBubble.style.background).toContain("color-mix(in srgb");
        const userTail = screen.getByTestId("assistant-chat-tail-user-user-bubble");
        expect(userTail.getAttribute("aria-hidden")).toBe("true");
        expect(userTail.style.right).toBe("13px");
        expect(userTail.style.top).toBe("-6px");
        expect(userTail.style.transform).toBe("rotate(45deg)");
        expect(userTail.style.background).toBe(userBubble.style.background);

        rerender(<div>{renderMessage({
            id: "ai-bubble",
            role: "assistant",
            content: "I have reviewed it.",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "en", false)}</div>);

        const assistantGroup = screen.getByTestId("assistant-chat-ai-ai-bubble");
        expect(assistantGroup.getAttribute("role")).toBe("group");
        expect(assistantGroup.getAttribute("aria-label")).toBe("AI assistant message");
        expect(assistantGroup.style.alignItems).toBe("flex-start");
        expect(screen.getByText("AI Assistant")).toBeTruthy();
        const assistantBubble = screen.getByTestId("assistant-chat-ai-bubble-ai-bubble") as HTMLElement;
        const assistantTail = screen.getByTestId("assistant-chat-tail-ai-ai-bubble");
        expect(assistantTail.getAttribute("aria-hidden")).toBe("true");
        expect(assistantTail.style.left).toBe("13px");
        expect(assistantTail.style.top).toBe("-6px");
        expect(assistantTail.style.transform).toBe("rotate(45deg)");
        expect(assistantTail.style.background).toBe(assistantBubble.style.background);
    });

    it("marks a fired guide bubble as injected without turning it into a new turn", () => {
        render(<div>{renderMessage({
            id: "injected-guide-bubble",
            role: "user",
            kind: "guideInjection",
            content: "Keep the active task focused on the regression.",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "en", false)}</div>);

        const group = screen.getByTestId("assistant-chat-user-injected-guide-bubble");
        expect(group.getAttribute("aria-label")).toBe("Your injected guidance");
        expect(screen.getByTestId("guide-injection-badge").textContent).toBe("Injected");
        expect(screen.getByText("Keep the active task focused on the regression.")).toBeTruthy();
    });

    it("shows a copy control on assistant replies and copies the full content", async () => {
        const writeText = vi.fn().mockResolvedValue(undefined);
        Object.assign(navigator, { clipboard: { writeText } });

        render(<div>{renderMessage({
            id: "copy-ai-bubble",
            role: "assistant",
            content: "Full reply body to copy",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        const btn = screen.getByTestId("assistant-chat-copy-copy-ai-bubble");
        expect(btn.getAttribute("aria-label")).toMatch(/复制|Copy/i);
        expect(screen.getByTestId("assistant-chat-ai-bubble-copy-ai-bubble-top-right")).toBeTruthy();
        await fireEvent.click(btn);
        await waitFor(() => expect(writeText).toHaveBeenCalledWith("Full reply body to copy"));
        // Allow post-copy state tick (busy → ok) to settle without act warnings.
        await waitFor(() => expect(btn.getAttribute("aria-label")).toMatch(/已复制|Copied/i));
    });

    it("hides the copy control when the assistant reply has no content yet", () => {
        render(<div>{renderMessage({
            id: "empty-ai-bubble",
            role: "assistant",
            content: "",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);

        expect(screen.queryByTestId("assistant-chat-copy-empty-ai-bubble")).toBeNull();
        expect(screen.queryByTestId("assistant-chat-ai-bubble-empty-ai-bubble-top-right")).toBeNull();
    });

    it("buildAssistantReplyCopyText strips Browser role prefixes for paste-ready text", () => {
        const text = buildAssistantReplyCopyText("Browser: deployment complete");
        expect(text).toContain("deployment complete");
        expect(text.toLowerCase().startsWith("browser:")).toBe(false);
    });

    it("copyTextToClipboard returns false for empty input", async () => {
        await expect(copyTextToClipboard("")).resolves.toBe(false);
        await expect(copyTextToClipboard("   ")).resolves.toBe(false);
    });

    it("keeps the assistant bubble tail visually paired with the active theme", () => {
        render(<div>{renderMessage({
            id: "dark-ai-bubble",
            role: "assistant",
            content: "Dark theme response",
            timestamp: Date.now(),
        }, vi.fn(), darkTheme, false, "Saved file", "en", false)}</div>);

        const tail = screen.getByTestId("assistant-chat-tail-ai-dark-ai-bubble");
        // Pair against live theme tokens (jsdom normalizes hex → rgb).
        const probe = document.createElement("div");
        probe.style.background = darkTheme.fieldBg;
        probe.style.color = darkTheme.fieldBorder;
        expect(tail.style.background).toBe(probe.style.background);
        expect(tail.style.borderTop).toContain(probe.style.color);
        expect(tail.style.pointerEvents).toBe("none");
        expect(tail.style.top).toBe("-6px");
        expect(tail.style.left).toBe("13px");
    });

    it("keeps failures compact and announced within the message flow", () => {
        render(<div>{renderMessage({
            id: "request-failed",
            role: "error",
            content: "Request failed",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "en", false)}</div>);

        const error = screen.getByTestId("assistant-chat-error-request-failed");
        expect(error.getAttribute("role")).toBe("alert");
        expect(error.style.justifyContent).toBe("flex-start");
        expect(screen.getByText("Request failed")).toBeTruthy();
    });

    it("renders ordinary progress as a compact status instead of a log line", () => {
        render(<div>{renderMessage({
            id: "plain-progress",
            role: "progress",
            content: "Fetching details",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "en", false)}</div>);

        const progress = screen.getByTestId("assistant-chat-progress-plain-progress");
        expect(progress.getAttribute("role")).toBe("status");
        expect(progress.getAttribute("aria-live")).toBe("polite");
        expect(progress.style.justifyContent).toBe("flex-start");
        expect(screen.getByText("Fetching details")).toBeTruthy();
    });

    it("keeps ordinary system notices contained in the chat flow", () => {
        render(<div>{renderMessage({
            id: "system-notice",
            role: "system",
            content: "Task moved to the background",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "en", false)}</div>);

        const notice = screen.getByTestId("assistant-chat-system-system-notice");
        expect(notice.getAttribute("role")).toBe("status");
        expect(notice.style.justifyContent).toBe("flex-start");
        expect(screen.getByText("Task moved to the background")).toBeTruthy();
    });

    it("strips Browser role prefixes from /btw body without dropping the body", () => {
        render(<div>{renderMessage({
            id: "assistant-btw-browser-prefix",
            role: "assistant",
            requestId: "btw-test",
            content: "\u{1F50D} **/btw 查询结果**\n\nBrowser: 旁路查询正文。",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        expect(screen.getByText("/btw")).toBeTruthy();
        expect(screen.getAllByText("旁路查询正文。").length).toBeGreaterThan(0);
        expect(screen.queryByText(/Browser:/)).toBeNull();
    });

    it("hides Browser role-prefixed reasoning tails", () => {
        render(<div>{renderMessage({
            id: "assistant-reasoning-browser-prefix",
            role: "assistant",
            content: "最终回答。",
            reasoning: "思考中\nBrowser: hidden tool echo",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        expect(screen.getByText("思考中")).toBeTruthy();
        expect(screen.queryByText(/hidden tool echo/)).toBeNull();
        expect(screen.queryByText(/Browser:/)).toBeNull();
    });

    it("expands reasoning while streaming and collapses it after completion", () => {
        const message = {
            id: "assistant-streaming-reasoning",
            role: "assistant" as const,
            content: "Final answer.",
            reasoning: "Inspecting the request.",
            timestamp: Date.now(),
        };
        const { rerender, unmount } = render(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);

        const openDetails = screen.getByText("Thinking process...").closest("details");
        expect(openDetails?.open).toBe(true);
        expect(screen.getByText("Inspecting the request.")).toBeTruthy();

        rerender(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", false)}</div>);
        const closedDetails = screen.getByText("Thinking process...").closest("details");
        expect(closedDetails?.open).toBe(false);
        unmount();
    });

    it("does not render an empty reasoning panel after stripping a Browser-only reasoning echo", () => {
        render(<div>{renderMessage({
            id: "assistant-browser-only-reasoning",
            role: "assistant",
            content: "最终回答。",
            reasoning: "Browser: hidden tool echo",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", false)}</div>);

        expect(screen.getByText("最终回答。")).toBeTruthy();
        expect(screen.queryByText("思考过程...")).toBeNull();
        expect(screen.queryByText(/hidden tool echo/)).toBeNull();
    });
});
