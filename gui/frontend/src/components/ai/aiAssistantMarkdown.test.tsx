// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderContentWithCodeBlocks } from "./aiAssistantMarkdown";
import { lightTheme } from "./aiAssistantPanelTheme";

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

    it("splits dense digital employee capability lists into readable lines", () => {
        render(
            <div>
                {renderContentWithCodeBlocks(
                    "I can help: \u{1f4c1} read local files \u{1f310} search the web \u{1f4ac} answer questions and analyze",
                    lightTheme
                )}
            </div>
        );

        expect(screen.getByText("I can help:")).toBeTruthy();
        expect(screen.getByText("\u{1f4c1} read local files")).toBeTruthy();
        expect(screen.getByText("\u{1f310} search the web")).toBeTruthy();
        expect(screen.getByText("\u{1f4ac} answer questions and analyze")).toBeTruthy();
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
        expect(screen.getByText("\u{1f4c5}Tomorrow")).toBeTruthy();
        expect(screen.getByText("Cloudy")).toBeTruthy();
    });

    it("does not split ordinary pictographs used inside a sentence", () => {
        render(<div>{renderContentWithCodeBlocks("Good job \u2705 keep going", lightTheme)}</div>);

        expect(screen.getByText("Good job \u2705 keep going")).toBeTruthy();
    });

    it("normalizes compact markdown headings at the start of a line", () => {
        render(<div>{renderContentWithCodeBlocks("###\u{1f4c5}Today\nClear", lightTheme)}</div>);

        expect(screen.getByText("\u{1f4c5}Today")).toBeTruthy();
        expect(screen.getByText("Clear")).toBeTruthy();
    });

    it.each([
        ["real newline", "####\n\u{1f4ca} Resource usage", "\u{1f4ca} Resource usage", "####"],
        ["escaped newline", "####\\n\u{1f4ca} Resource usage", "\u{1f4ca} Resource usage", "####"],
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

    it("splits compact emoji headings even when the previous text has no punctuation", () => {
        render(<div>{renderContentWithCodeBlocks("晴天###\u{1f4c5}明天\n多云", lightTheme)}</div>);

        expect(screen.getByText("晴天")).toBeTruthy();
        expect(screen.getByText("\u{1f4c5}明天")).toBeTruthy();
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

    it("does not split a single capability icon used inline", () => {
        render(<div>{renderContentWithCodeBlocks("Open the \u{1f4c1} folder", lightTheme)}</div>);

        expect(screen.getByText("Open the \u{1f4c1} folder")).toBeTruthy();
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
        const th = container.querySelector("th") as HTMLTableCellElement;
        const td = container.querySelector("td") as HTMLTableCellElement;
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

        expect(screen.getByTestId("markdown-table-prefix").textContent).toContain("introductory sentence");
        expect(screen.getByTestId("markdown-table-note").textContent).toContain("trailing note");
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
