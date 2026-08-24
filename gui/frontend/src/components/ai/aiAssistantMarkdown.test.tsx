// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
    assistantMessageHasVisibleBody,
    buildAssistantReplyCopyText,
    copyTextToClipboard,
    renderCodingAgentThinkingTimelineItem,
    renderContentWithCodeBlocks,
    renderMessage,
} from "./aiAssistantMarkdown";
import { renderScreenshotPreview } from "./aiAssistantMarkdownMedia";
import { darkTheme, lightTheme } from "./aiAssistantPanelTheme";

// Minimal JPEG stream with a 1x1 SOF0 frame. The renderer only needs header
// validation before assigning a data URL to img.src; browser image loading is
// outside jsdom's scope.
const safeKBImageJPEG = "/9j/2wCEAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDIBCQkJDAsMGA0NGDIhHCEyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMjIyMv/AABEIAAEAAQMBIgACEQEDEQH/xAGiAAABBQEBAQEBAQAAAAAAAAAAAQIDBAUGBwgJCgsQAAIBAwMCBAMFBQQEAAABfQECAwAEEQUSITFBBhNRYQcicRQygZGhCCNCscEVUtHwJDNicoIJChYXGBkaJSYnKCkqNDU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6g4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2drh4uPk5ebn6Onq8fLz9PX29/j5+gEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoLEQACAQIEBAMEBwUEBAABAncAAQIDEQQFITEGEkFRB2FxEyIygQgUQpGhscEJIzNS8BVictEKFiQ04SXxFxgZGiYnKCkqNTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqCg4SFhoeIiYqSk5SVlpeYmZqio6Slpqeoqaqys7S1tre4ubrCw8TFxsfIycrS09TV1tfY2dri4+Tl5ufo6ery8/T19vf4+fr/2gAMAwEAAhEDEQA/AOLooor5k/cT/9k=";

const { openFileOrShowInFolderMock, showItemInFolderMock, knowledgeOpenImageAssetMock, attachmentPreviewDataURLMock, attachmentFullDataURLMock } = vi.hoisted(() => ({
    openFileOrShowInFolderMock: vi.fn(async () => undefined),
    showItemInFolderMock: vi.fn(async () => undefined),
    knowledgeOpenImageAssetMock: vi.fn(async () => undefined),
    attachmentPreviewDataURLMock: vi.fn(async () => "data:image/png;base64,HOSTTHUMB"),
    attachmentFullDataURLMock: vi.fn(async () => "data:image/png;base64,HOSTFULL"),
}));

vi.mock("../../../wailsjs/go/main/App", () => ({
    OpenFileOrShowInFolder: openFileOrShowInFolderMock,
    ShowItemInFolder: showItemInFolderMock,
    KnowledgeOpenImageAsset: knowledgeOpenImageAssetMock,
    AIAssistantAttachmentPreviewDataURL: attachmentPreviewDataURLMock,
    AIAssistantAttachmentFullDataURL: attachmentFullDataURLMock,
}));

vi.mock("../../../wailsjs/runtime", () => ({
    BrowserOpenURL: vi.fn(),
}));

describe("renderContentWithCodeBlocks", () => {
    beforeEach(() => {
        openFileOrShowInFolderMock.mockClear();
        showItemInFolderMock.mockClear();
        knowledgeOpenImageAssetMock.mockClear();
        attachmentFullDataURLMock.mockClear();
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

    it("renders inline LaTeX without interpreting formulas inside code spans", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("设 $x \\in X$，而 `const price = $5;` 保持为代码。", lightTheme)}</div>,
        );

        expect(screen.getByTestId("assistant-inline-math").textContent).toContain("x");
        expect(container.querySelector("code")?.textContent).toBe("const price = $5;");
        expect(container.textContent).not.toContain("$x \\in X$");
    });

    it("keeps currency-like dollar pairs as text instead of interpreting them as math", () => {
        render(<div>{renderContentWithCodeBlocks("预算从 $5 到 $10，公式为 $x + 1$。", lightTheme)}</div>);

        expect(screen.getByText(/预算从 \$5 到 \$10，公式为/)).toBeTruthy();
        expect(screen.getAllByTestId("assistant-inline-math")).toHaveLength(1);
        expect(screen.getByTestId("assistant-inline-math").textContent).toContain("x");
    });

    it("renders several inline formulas in the same Chinese sentence", () => {
        render(<div>{renderContentWithCodeBlocks("设 $X$ 是集合，$d: X \\times X \\to \\mathbb{R}$ 满足条件。", lightTheme)}</div>);

        expect(screen.getAllByTestId("assistant-inline-math")).toHaveLength(2);
        expect(screen.getByText(/设/).textContent).not.toContain("$X$");
        expect(screen.getByText(/满足条件/).textContent).not.toContain("$d:");
    });

    it("keeps an inline formula readable without a nested horizontal scrollbar", () => {
        render(<div>{renderContentWithCodeBlocks("定义为 $d: X \\times X \\to \\mathbb{R}$。", lightTheme)}</div>);

        const formula = screen.getByTestId("assistant-inline-math") as HTMLElement;
        expect(formula.style.display).toBe("");
        expect(formula.style.maxWidth).toBe("");
        expect(formula.style.overflowX).toBe("");
        expect(formula.style.overflowY).toBe("");
        expect(formula.style.minWidth).toBe("");
    });

    it("keeps malformed inline formulas aligned without introducing a scroll container", () => {
        render(<div>{renderContentWithCodeBlocks("定义为 $\\notARealCommand$。", lightTheme)}</div>);

        const formula = screen.getByTestId("assistant-inline-math") as HTMLElement;
        expect(formula.style.verticalAlign).toBe("middle");
        expect(formula.style.display).toBe("");
        expect(formula.style.overflowX).toBe("");
        expect(formula.style.overflowY).toBe("");
    });

    it("renders a formula containing a pipe inside a single table cell", () => {
        render(
            <div>{renderContentWithCodeBlocks("| 公式 | 含义 |\n| --- | --- |\n| $P(A|B)$ | 条件概率 |", lightTheme)}</div>,
        );

        const table = screen.getByTestId("markdown-table") as HTMLTableElement;
        expect(table.querySelectorAll("thead th")).toHaveLength(2);
        expect(table.querySelectorAll("tbody td")).toHaveLength(2);
        expect(screen.getByTestId("assistant-inline-math").textContent).toContain("P");
        expect(table.textContent).toContain("条件概率");
    });

    it("recognizes formulas immediately adjacent to Chinese prose", () => {
        render(<div>{renderContentWithCodeBlocks("令$x$属于$X$，则$d(x,y)=0$。", lightTheme)}</div>);

        expect(screen.getAllByTestId("assistant-inline-math")).toHaveLength(3);
        expect(document.body.textContent).not.toContain("$x$");
        expect(document.body.textContent).not.toContain("$d(x,y)=0$");
    });

    it("keeps formulas literal inside multi-backtick inline code", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("``const price = $5;`` 且 $x$ 是公式。", lightTheme)}</div>,
        );

        expect(container.querySelector("code")?.textContent).toBe("const price = $5;");
        expect(screen.getAllByTestId("assistant-inline-math")).toHaveLength(1);
    });

    it("does not let escaped dollars or identifier-adjacent dollars swallow a later formula", () => {
        render(<div>{renderContentWithCodeBlocks("保留 \\$5、US$ price；公式为 $x_1 + y$。", lightTheme)}</div>);

        expect(document.body.textContent).toContain("保留 \\$5、US$ price；公式为");
        expect(screen.getAllByTestId("assistant-inline-math")).toHaveLength(1);
        expect(screen.getByTestId("assistant-inline-math").textContent).toContain("x");
    });

    it("renders delimited display LaTeX blocks", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("距离公理\n$$\nd(x, y) \\geq 0, \\quad d(x, y) = 0 \\iff x = y\n$$\n成立。", lightTheme)}</div>,
        );

        expect(screen.getByTestId("assistant-display-math").textContent).toContain("d");
        expect(container.textContent).not.toContain("$$");
    });

    it("renders display formulas when the opening delimiter shares its first line with TeX", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("$$d(x, y) \\geq 0$$", lightTheme)}</div>,
        );

        expect(screen.getByTestId("assistant-display-math").textContent).toContain("d");
        expect(container.textContent).not.toContain("$$");
    });

    it("renders display formulas when the closing delimiter shares its final line with TeX", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("$$\n\\begin{aligned}\nf(x) &= x^2\\\\\n\\end{aligned}$$", lightTheme)}</div>,
        );

        expect(screen.getByTestId("assistant-display-math").textContent).toContain("f");
        expect(container.textContent).not.toContain("$$");
    });

    it("renders bracket-delimited display formulas when the closing delimiter shares its final line with TeX", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("\\[\n\\frac{a}{b}\\]", lightTheme)}</div>,
        );

        expect(screen.getByTestId("assistant-display-math").textContent).toContain("a");
        expect(container.textContent).not.toContain("\\]");
    });

    it("keeps an unfinished display formula legible until its closing delimiter arrives", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("$$d(x, y) \\geq 0", lightTheme)}</div>,
        );

        expect(screen.queryByTestId("assistant-display-math")).toBeNull();
        expect(container.textContent).toContain("$$");
        expect(container.textContent).toContain("d(x, y)");
    });

    it("does not turn TeX hashes in an unfinished display formula into a Markdown heading", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("$$\n###\nx + y", lightTheme)}</div>,
        );

        expect(container.textContent).toContain("###");
        expect(container.textContent).toContain("x + y");
        expect(container.querySelector('[style*="font-weight: 600"]')?.textContent).not.toBe("x + y");
    });

    it("still repairs a bare Markdown heading after a completed display formula", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("$$\n###\nx + y\n$$\n###\nActual title", lightTheme)}</div>,
        );

        expect(screen.getByText("Actual title")).toBeTruthy();
        expect(container.textContent).not.toMatch(/\$\$\n?###\n?x \+ y\n?\$\$\n?###/);
    });

    it("does not disable heading-marker repair for dollar text inside fenced code", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("```text\n$$\n```\n###\nActual title", lightTheme)}</div>,
        );

        expect(container.querySelector("code")?.textContent).toBe("$$");
        expect(container.textContent).not.toContain("###");
        expect(screen.getByText("Actual title")).toBeTruthy();
    });

    it("renders LaTeX parentheses and brackets delimiters", () => {
        render(
            <div>{renderContentWithCodeBlocks("内联 \\(x^2\\)\n\\[\n\\frac{a}{b}\n\\]", lightTheme)}</div>,
        );

        expect(screen.getByTestId("assistant-inline-math").textContent).toContain("x");
        expect(screen.getByTestId("assistant-display-math").textContent).toContain("a");
    });

    it("preserves display-math TeX commands that begin with an escaped newline token", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("$$\n\\newcommand{\\foo}{x}\n\\foo + 1\n$$", lightTheme)}</div>,
        );

        expect(screen.getByTestId("assistant-display-math").textContent).toContain("x");
        expect(container.textContent).not.toContain("$$");
    });

    it("preserves inline-math TeX commands that begin with an escaped newline token", () => {
        render(<div>{renderContentWithCodeBlocks("Value $\\newcommand{\\foo}{x}\\foo$", lightTheme)}</div>);

        expect(screen.getByTestId("assistant-inline-math").textContent).toContain("x");
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
            <div>{renderContentWithCodeBlocks(`[KB_IMAGE:asset-1|data:image/jpeg;base64,${safeKBImageJPEG}]`, lightTheme)}</div>
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

    it("renders path-free KB image markers", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks(`[KB_IMAGE:asset-1|data:image/jpeg;base64,${safeKBImageJPEG}]`, lightTheme)}</div>
        );
        const image = container.querySelector("img") as HTMLImageElement;
        expect(image).toBeTruthy();
        expect(image.src).toContain(`data:image/jpeg;base64,${safeKBImageJPEG}`);
        expect(image.title).toBe("Click to open: asset-1");
    });

    it("opens a KB image only through its opaque asset ID", async () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks(`[KB_IMAGE:asset-1|data:image/jpeg;base64,${safeKBImageJPEG}]`, lightTheme)}</div>
        );
        const image = container.querySelector("img") as HTMLImageElement;
        fireEvent.click(image);
        expect(knowledgeOpenImageAssetMock).not.toHaveBeenCalled();
        fireEvent.load(image);
        fireEvent.click(image);

        await waitFor(() => expect(knowledgeOpenImageAssetMock).toHaveBeenCalledWith("asset-1"));
        expect(openFileOrShowInFolderMock).not.toHaveBeenCalled();
    });

    it("does not render base64 that is not a managed JPEG thumbnail", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("[KB_IMAGE:asset-1|data:image/jpeg;base64,YWJj]", lightTheme)}</div>,
        );
        expect(container.querySelector("img")).toBeNull();
    });

    it("never opens an asset for a JPEG-shaped marker until browser decoding succeeds", () => {
        // A model can imitate JPEG headers. Header checks bound the candidate,
        // but only the image element's successful decoder event enables the
        // asset-ID action.
        const jpegShapedGarbage = btoa("\xff\xd8\xff\xc0\x00\x0b\x08\x00\x01\x00\x01\x03\x01\x11\x00\xff\xd9");
        const { container } = render(
            <div>{renderContentWithCodeBlocks(`[KB_IMAGE:asset-1|data:image/jpeg;base64,${jpegShapedGarbage}]`, lightTheme)}</div>,
        );
        const image = container.querySelector("img") as HTMLImageElement;
        expect(image).toBeTruthy();
        fireEvent.click(image);
        expect(knowledgeOpenImageAssetMock).not.toHaveBeenCalled();
        fireEvent.error(image);
        expect(container.querySelector("img")).toBeNull();
    });

    it("rejects a JPEG marker whose first frame exceeds the managed thumbnail size", () => {
        // A later fake small SOF must not override the first real frame. JPEG
        // segment parsing intentionally stops at that first SOF, matching the
        // managed thumbnail producer's dimensional contract.
        const oversizedFirstFrame = btoa("\xff\xd8\xff\xc0\x00\x0b\x08\x00\x79\x00\x79\x03\x01\x11\x00\xff\xc0\x00\x0b\x08\x00\x01\x00\x01\x03\x01\x11\x00\xff\xd9");
        const { container } = render(
            <div>{renderContentWithCodeBlocks(`[KB_IMAGE:asset-1|data:image/jpeg;base64,${oversizedFirstFrame}]`, lightTheme)}</div>,
        );

        expect(container.querySelector("img")).toBeNull();
        expect(knowledgeOpenImageAssetMock).not.toHaveBeenCalled();
    });

    it("does not load a model-authored remote URL from a KB image marker", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("[KB_IMAGE:asset-1|https://tracker.example/collect.png]", lightTheme)}</div>
        );

        expect(container.querySelector("img")).toBeNull();
        expect(knowledgeOpenImageAssetMock).not.toHaveBeenCalled();
    });

    it("does not load non-image data URLs from a KB image marker", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("[KB_IMAGE:asset-1|data:text/html;base64,PHNjcmlwdD4=]", lightTheme)}</div>
        );

        expect(container.querySelector("img")).toBeNull();
    });

    it("rejects legacy KB image markers carrying an agent-provided file path", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("[KB_IMAGE:asset-1|data:image/jpeg;base64,abc|C:\\private\\image.png]", lightTheme)}</div>
        );
        expect(container.querySelector("img")).toBeNull();
        expect(knowledgeOpenImageAssetMock).not.toHaveBeenCalled();
        expect(openFileOrShowInFolderMock).not.toHaveBeenCalled();
    });

    it("rejects a KB image marker whose asset ID could be a filesystem path", () => {
        const { container } = render(
            <div>{renderContentWithCodeBlocks("[KB_IMAGE:../../private|data:image/jpeg;base64,YWJj]", lightTheme)}</div>
        );

        expect(container.querySelector("img")).toBeNull();
        expect(knowledgeOpenImageAssetMock).not.toHaveBeenCalled();
    });

    it("rejects an oversized but syntactically valid KB image data URL", () => {
        const oversized = "A".repeat(Math.ceil((256 * 1024 + 1) / 3) * 4);
        const { container } = render(
            <div>{renderContentWithCodeBlocks(`[KB_IMAGE:asset-1|data:image/jpeg;base64,${oversized}]`, lightTheme)}</div>
        );

        expect(container.querySelector("img")).toBeNull();
        expect(knowledgeOpenImageAssetMock).not.toHaveBeenCalled();
    });

    it("keeps screenshot previews inside the message width", () => {
        const { container } = render(<div>{renderScreenshotPreview("abc", "C:\\Users\\demo\\screen.png", lightTheme, "zh")}</div>);

        const wrapper = screen.getByTestId("screenshot-preview-block") as HTMLElement;
        const thumbnail = screen.getByTestId("attachment-image-thumbnail") as HTMLButtonElement;
        const image = container.querySelector("img") as HTMLImageElement;
        expect(wrapper.style.maxWidth).toBe("100%");
        expect(wrapper.style.minWidth).toBe("0px");
        expect(wrapper.style.overflow).toBe("hidden");
        expect(thumbnail.style.width).toBe("180px");
        expect(thumbnail.style.maxWidth).toBe("100%");
        expect(thumbnail.style.boxSizing).toBe("border-box");
        expect(image.style.maxHeight).toBe("120px");
        expect(image.style.objectFit).toBe("contain");
        expect(image.style.display).toBe("block");
    });

    it("zooms a tool screenshot in the shared preview overlay, reading the saved capture", async () => {
        const savedPath = "C:\\Users\\demo\\screen.png";
        render(<div>{renderScreenshotPreview("abc", savedPath, lightTheme, "zh")}</div>);

        const thumbnail = await screen.findByTestId("attachment-image-thumbnail");
        fireEvent.click(thumbnail);

        // The inline base64 is only the tool's downscaled copy, so the overlay
        // upgrades to the file on disk.
        await waitFor(() => {
            expect(screen.getByTestId("attachment-image-preview-image").getAttribute("src")).toBe("data:image/png;base64,HOSTFULL");
        });
        expect(attachmentFullDataURLMock).toHaveBeenCalledWith(savedPath);
        expect(screen.getByTestId("attachment-image-preview-status").textContent).toBe("screen.png");

        fireEvent.click(screen.getByTestId("attachment-image-preview-close"));
        expect(screen.queryByTestId("attachment-image-preview-dialog")).toBeNull();
    });

    it("reveals a saved screenshot in its folder instead of opening the OS viewer", async () => {
        const savedPath = "C:\\Users\\demo\\screen.png";
        render(<div>{renderScreenshotPreview("abc", savedPath, lightTheme, "zh")}</div>);

        fireEvent.click(await screen.findByTestId("attachment-image-thumbnail"));
        fireEvent.click(screen.getByTestId("attachment-image-preview-open-file"));

        expect(showItemInFolderMock).toHaveBeenCalledWith(savedPath);
        expect(openFileOrShowInFolderMock).not.toHaveBeenCalled();
    });

    it("previews an unsaved screenshot from its inline bytes without a reveal action", async () => {
        render(<div>{renderScreenshotPreview("abc", undefined, lightTheme, "zh")}</div>);

        fireEvent.click(await screen.findByTestId("attachment-image-thumbnail"));

        expect(screen.getByTestId("attachment-image-preview-image").getAttribute("src")).toBe("data:image/png;base64,abc");
        expect(screen.queryByTestId("attachment-image-preview-open-file")).toBeNull();
        expect(attachmentFullDataURLMock).not.toHaveBeenCalled();
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

describe("renderMessage user attachment chips", () => {
    const imageAttachment = {
        filePath: "D:\\tmp\\paste_1.png",
        fileName: "paste_1.png",
        extension: ".png",
        isImage: true,
        thumbnailDataUrl: "blob:maclaw/pasted-image",
    };

    beforeEach(() => {
        attachmentPreviewDataURLMock.mockClear();
        attachmentFullDataURLMock.mockClear();
    });

    it("opens a full-image preview from an attached image and closes it again", async () => {
        render(<div>{renderMessage({
            id: "with-image",
            role: "user",
            content: "看看这张图",
            attachments: [imageAttachment],
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "zh", false)}</div>);

        fireEvent.click(await screen.findByTestId("attachment-image-thumbnail"));
        await waitFor(() => {
            expect(screen.getByTestId("attachment-image-preview-image").getAttribute("src")).toBe("data:image/png;base64,HOSTFULL");
        });
        expect(attachmentFullDataURLMock).toHaveBeenCalledWith(imageAttachment.filePath);

        fireEvent.click(screen.getByTestId("attachment-image-preview-close"));
        await waitFor(() => expect(screen.queryByTestId("attachment-image-preview-overlay")).toBeNull());
    });

    it("uses the saved attachment preview instead of a transient composer object URL", async () => {
        render(<div>{renderMessage({
            id: "revoked-image",
            role: "user",
            content: "",
            attachments: [imageAttachment],
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "zh", false)}</div>);

        const chipImage = (await screen.findByTestId("attachment-image-thumbnail")).querySelector("img") as HTMLImageElement;
        expect(chipImage.getAttribute("src")).toBe("data:image/png;base64,HOSTTHUMB");

        expect(attachmentPreviewDataURLMock).toHaveBeenCalledWith(imageAttachment.filePath);
    });

    it("does not briefly show a file-type badge while the saved image preview is loading", () => {
        let resolvePreview: (value: string) => void = () => undefined;
        attachmentPreviewDataURLMock.mockImplementationOnce(() => new Promise<string>((resolve) => { resolvePreview = resolve; }));
        render(<div>{renderMessage({
            id: "loading-image",
            role: "user",
            content: "",
            attachments: [imageAttachment],
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, false, "Saved file", "zh", false)}</div>);

        expect(screen.getByLabelText("paste_1.png").textContent).toBe("");
        resolvePreview("data:image/png;base64,HOSTTHUMB");
    });
});

describe("renderMessage assistant display guard", () => {
    it("keeps coding timeline reasoning out of the assistant bubble", () => {
        const message = {
            id: "coding-timeline",
            role: "assistant" as const,
            content: "",
            reasoning: "Inspect the timeline renderer",
            codingTimeline: [{ id: "thought", sequence: 1, kind: "thinking" as const, content: "Inspect the timeline renderer", timestamp: 1 }],
            timestamp: 1,
        };
        expect(assistantMessageHasVisibleBody(message)).toBe(false);
        render(<div>{renderCodingAgentThinkingTimelineItem(message.codingTimeline[0], lightTheme, "en")}</div>);
        expect(screen.getByText("Thought")).toBeTruthy();
        expect(screen.getByText("Inspect the timeline renderer")).toBeTruthy();
    });

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

    it("strips leftover coding-audit headings from collapsed reasoning", () => {
        render(<div>{renderMessage({
            id: "assistant-reasoning-audit-heading",
            role: "assistant",
            content: "Created hello.cpp.",
            reasoning: "I'll write a small C++ file.\n\n## \u9a8c\u8bc1\u7ed3\u679c\ncl passed",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "en", false)}</div>);

        expect(screen.getByText("I'll write a small C++ file.")).toBeTruthy();
        expect(screen.queryByText(/cl passed/)).toBeNull();
        expect(screen.queryByText(/\u9a8c\u8bc1\u7ed3\u679c/)).toBeNull();
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

    it("folds ordinary-chat reasoning when streaming completes", () => {
        const message = {
            id: "assistant-streaming-reasoning",
            role: "assistant" as const,
            content: "Final answer.",
            reasoning: "Inspecting the request.",
            timestamp: Date.now(),
        };
        const { rerender, unmount } = render(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);

        const streamingDetails = screen.getByText("Thinking process...").closest("details");
        expect(streamingDetails?.open).toBe(true);
        expect(screen.getByText("Inspecting the request.")).toBeTruthy();
        const reasoningBody = screen.getByTestId("assistant-reasoning-body");
        expect(reasoningBody.hasAttribute("data-nested-scroll")).toBe(true);
        Object.defineProperties(reasoningBody, {
            scrollHeight: { configurable: true, value: 480 },
            clientHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        rerender(<div>{renderMessage({ ...message, reasoning: "Inspecting the request.\nChecking Ningbo weather." }, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
        expect(reasoningBody.scrollTop).toBe(480);

        rerender(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", false)}</div>);
        const completedDetails = screen.getByText("Thinking process...").closest("details");
        expect(completedDetails?.open).toBe(false);
        unmount();
    });

    it("keeps ordinary-chat reasoning open while a tool is running", () => {
        const message = {
            id: "assistant-tool-running-reasoning",
            role: "assistant" as const,
            content: "",
            reasoning: "Searching the forecast source.",
            timestamp: Date.now(),
        };
        const { rerender } = render(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);

        expect(screen.getByText("Thinking process...").closest("details")?.open).toBe(true);

        rerender(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", false)}</div>);
        expect(screen.getByText("Thinking process...").closest("details")?.open).toBe(false);
    });

    it("preserves a manual reasoning toggle until the streaming phase changes", () => {
        const message = {
            id: "assistant-manual-reasoning-toggle",
            role: "assistant" as const,
            content: "Draft answer.",
            reasoning: "Checking the request.",
            timestamp: Date.now(),
        };
        const renderChat = (next = message, streaming = true) =>
            <div>{renderMessage(next, vi.fn(), lightTheme, true, "Saved file", "en", streaming)}</div>;
        const { rerender } = render(renderChat());

        const summary = screen.getByText("Thinking process...");
        const details = summary.closest("details");
        expect(details?.open).toBe(true);

        fireEvent.click(summary);
        expect(details?.open).toBe(false);

        rerender(renderChat({ ...message, reasoning: "Checking the request.\nReviewing constraints." }));
        expect(screen.getByText("Thinking process...").closest("details")?.open).toBe(false);

        rerender(renderChat(message, false));
        const completedDetails = screen.getByText("Thinking process...").closest("details");
        expect(completedDetails?.open).toBe(false);

        fireEvent.click(screen.getByText("Thinking process..."));
        expect(completedDetails?.open).toBe(true);

        rerender(renderChat({ ...message, content: "Final answer." }, false));
        expect(screen.getByText("Thinking process...").closest("details")?.open).toBe(true);
    });

    it("reopens reasoning when a paused stream resumes", () => {
        const message = {
            id: "assistant-resumed-reasoning",
            role: "assistant" as const,
            content: "Draft answer.",
            reasoning: "Checking the request.",
            timestamp: Date.now(),
        };
        const renderChat = (streaming: boolean) =>
            <div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", streaming)}</div>;
        const { rerender } = render(renderChat(true));

        expect(screen.getByText("Thinking process...").closest("details")?.open).toBe(true);

        rerender(renderChat(false));
        expect(screen.getByText("Thinking process...").closest("details")?.open).toBe(false);

        rerender(renderChat(true));
        expect(screen.getByText("Thinking process...").closest("details")?.open).toBe(true);
    });

    it("keeps coding-workbench reasoning collapsed while streaming and after completion", () => {
        const message = {
            id: "assistant-coding-reasoning",
            role: "assistant" as const,
            content: "Created hello.cpp.",
            reasoning: "I'll write a small C++ file.",
            timestamp: Date.now(),
        };
        const renderCoding = (next = message, streaming = true) =>
            renderMessage(next, vi.fn(), lightTheme, true, "Saved file", "en", streaming, undefined, undefined, true);
        const { rerender, unmount } = render(<div>{renderCoding()}</div>);

        expect(screen.getByText("Thinking process...").closest("details")?.open).toBe(false);
        rerender(<div>{renderCoding({ ...message, reasoning: "I'll write a small C++ file.\nChecking compile." })}</div>);
        expect(screen.getByText("Thinking process...").closest("details")?.open).toBe(false);
        rerender(<div>{renderCoding(message, false)}</div>);
        expect(screen.getByText("Thinking process...").closest("details")?.open).toBe(false);
        unmount();
    });

    it("does not hide a coding-workbench recovery card as an empty placeholder", () => {
        render(<div>{renderMessage({
            id: "assistant-coding-recover",
            role: "assistant",
            content: "",
            recoverableSession: { sessionID: "sess-1", title: "Resume coding" },
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "en", true, undefined, undefined, true)}</div>);

        expect(screen.getByTestId("assistant-chat-ai-assistant-coding-recover")).toBeTruthy();
    });

    it("treats chat status-only reasoning as an empty coding placeholder", () => {
        expect(assistantMessageHasVisibleBody({
            id: "status-only",
            role: "assistant",
            content: "",
            reasoning: "\u6536\u5230\uff0c\u6b63\u5728\u5904\u7406\n\u2022 \u5df2\u63a5\u6536\u4efb\u52a1\uff0c\u6b63\u5728\u51c6\u5907\u6267\u884c\u8def\u5f84",
            timestamp: Date.now(),
        })).toBe(false);
        const { container } = render(<div>{renderMessage({
            id: "assistant-coding-status",
            role: "assistant",
            content: "",
            reasoning: "\u2022 \u6536\u5230\uff0c\u6b63\u5728\u5904\u7406",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", true, undefined, undefined, true)}</div>);
        expect(container.querySelector('[data-testid="assistant-chat-ai-assistant-coding-status"]')).toBeNull();
        expect(container.textContent || "").not.toMatch(/处理中|思考中|Working|收到/);
    });

    it("treats audit-only coding content as an empty placeholder", () => {
        expect(assistantMessageHasVisibleBody({
            id: "audit-only",
            role: "assistant",
            content: "## \u9a8c\u8bc1\u7ed3\u679c\ncl passed\n\n## \u6d89\u53ca\u6587\u4ef6\nhello.cpp",
            reasoning: "## \u8d28\u91cf\u5ba1\u8ba1\npassed",
            timestamp: Date.now(),
        })).toBe(false);
        const { container } = render(<div>{renderMessage({
            id: "assistant-coding-audit",
            role: "assistant",
            content: "## \u9a8c\u8bc1\u7ed3\u679c\ncl passed",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", true, undefined, undefined, true)}</div>);
        expect(container.querySelector('[data-testid="assistant-chat-ai-assistant-coding-audit"]')).toBeNull();
    });

    it("treats whitespace-only coding placeholders as empty", () => {
        expect(assistantMessageHasVisibleBody({
            id: "ws",
            role: "assistant",
            content: "  \n",
            reasoning: "   ",
            timestamp: Date.now(),
        })).toBe(false);
        const { container } = render(<div>{renderMessage({
            id: "assistant-coding-ws",
            role: "assistant",
            content: "   ",
            reasoning: "\n",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", true, undefined, undefined, true)}</div>);
        expect(container.querySelector('[data-testid="assistant-chat-ai-assistant-coding-ws"]')).toBeNull();
    });

    it("does not show a chat Working/思考中 bubble for an empty coding-workbench placeholder", () => {
        const { container } = render(<div>{renderMessage({
            id: "assistant-coding-empty",
            role: "assistant",
            content: "",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", true, undefined, undefined, true)}</div>);

        expect(container.querySelector('[data-testid="assistant-chat-ai-assistant-coding-empty"]')).toBeNull();
        expect(container.textContent || "").not.toMatch(/处理中|思考中|Working/);
    });

    it("keeps the ordinary-chat Working placeholder on an empty streaming bubble", () => {
        render(<div>{renderMessage({
            id: "assistant-chat-empty",
            role: "assistant",
            content: "",
            timestamp: Date.now(),
        }, vi.fn(), lightTheme, true, "Saved file", "zh", true)}</div>);

        expect(screen.getByText("\u5904\u7406\u4e2d\u2026")).toBeTruthy();
    });

    it("keeps the thinking pane pinned after a delayed layout grow", () => {
        let resizeCb: ResizeObserverCallback | undefined;
        class MockResizeObserver {
            constructor(cb: ResizeObserverCallback) {
                resizeCb = cb;
            }
            observe() {}
            disconnect() {}
            unobserve() {}
        }
        vi.stubGlobal("ResizeObserver", MockResizeObserver);

        try {
            const message = {
                id: "assistant-streaming-reasoning-grow",
                role: "assistant" as const,
                content: "",
                reasoning: "Inspecting the request.",
                timestamp: Date.now(),
            };
            const { rerender, unmount } = render(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
            const reasoningBody = screen.getByTestId("assistant-reasoning-body");
            Object.defineProperties(reasoningBody, {
                scrollHeight: { configurable: true, value: 480 },
                clientHeight: { configurable: true, value: 400 },
                scrollTop: { configurable: true, value: 0, writable: true },
            });
            rerender(<div>{renderMessage({ ...message, reasoning: "Inspecting the request.\nChecking Ningbo weather." }, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
            expect(reasoningBody.scrollTop).toBe(480);

            Object.defineProperty(reasoningBody, "scrollHeight", { configurable: true, value: 720 });
            resizeCb?.([], {} as ResizeObserver);
            expect(reasoningBody.scrollTop).toBe(720);
            unmount();
        } finally {
            vi.unstubAllGlobals();
        }
    });

    it("does not unpin thinking follow when content growth fires a scroll event", () => {
        const message = {
            id: "assistant-streaming-reasoning-growth-scroll",
            role: "assistant" as const,
            content: "",
            reasoning: "Inspecting the request.",
            timestamp: Date.now(),
        };
        const { rerender, unmount } = render(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
        const reasoningBody = screen.getByTestId("assistant-reasoning-body");
        Object.defineProperties(reasoningBody, {
            scrollHeight: { configurable: true, value: 480 },
            clientHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 80, writable: true },
        });
        Object.defineProperty(reasoningBody, "scrollHeight", { configurable: true, value: 720 });
        fireEvent.scroll(reasoningBody);
        rerender(<div>{renderMessage({ ...message, reasoning: "Inspecting the request.\nI'll commit sources only." }, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
        expect(reasoningBody.scrollTop).toBe(720);
        unmount();
    });

    it("does not yank the thinking pane when an upward wheel has not yet produced a scroll event", () => {
        const message = {
            id: "assistant-streaming-reasoning-wheel-race",
            role: "assistant" as const,
            content: "",
            reasoning: "Inspecting the request.",
            timestamp: Date.now(),
        };
        const { rerender, unmount } = render(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
        const reasoningBody = screen.getByTestId("assistant-reasoning-body");
        Object.defineProperties(reasoningBody, {
            scrollHeight: { configurable: true, value: 720 },
            clientHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.wheel(reasoningBody, { deltaY: -40 });
        rerender(<div>{renderMessage({ ...message, reasoning: "Inspecting the request.\nChecking Ningbo weather." }, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
        expect(reasoningBody.scrollTop).toBe(0);
        unmount();
    });

    it("does not yank the thinking pane when the user scrolls up", () => {
        const message = {
            id: "assistant-streaming-reasoning-user-up",
            role: "assistant" as const,
            content: "",
            reasoning: "Inspecting the request.",
            timestamp: Date.now(),
        };
        const { rerender, unmount } = render(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
        const reasoningBody = screen.getByTestId("assistant-reasoning-body");
        Object.defineProperties(reasoningBody, {
            scrollHeight: { configurable: true, value: 720 },
            clientHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.wheel(reasoningBody, { deltaY: -40 });
        fireEvent.scroll(reasoningBody);
        rerender(<div>{renderMessage({ ...message, reasoning: "Inspecting the request.\nChecking Ningbo weather." }, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
        expect(reasoningBody.scrollTop).toBe(0);
        unmount();
    });

    it("does not yank the thinking pane after a scrollbar pointer drag", () => {
        const message = {
            id: "assistant-streaming-reasoning-scrollbar",
            role: "assistant" as const,
            content: "",
            reasoning: "Inspecting the request.",
            timestamp: Date.now(),
        };
        const { rerender, unmount } = render(<div>{renderMessage(message, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
        const reasoningBody = screen.getByTestId("assistant-reasoning-body");
        Object.defineProperties(reasoningBody, {
            scrollHeight: { configurable: true, value: 720 },
            clientHeight: { configurable: true, value: 400 },
            scrollTop: { configurable: true, value: 0, writable: true },
        });
        fireEvent.pointerDown(reasoningBody);
        fireEvent.scroll(reasoningBody);
        rerender(<div>{renderMessage({ ...message, reasoning: "Inspecting the request.\nChecking Ningbo weather." }, vi.fn(), lightTheme, true, "Saved file", "en", true)}</div>);
        expect(reasoningBody.scrollTop).toBe(0);
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
