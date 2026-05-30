// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderContentWithCodeBlocks } from "./aiAssistantMarkdown";
import { lightTheme } from "./aiAssistantPanelTheme";

describe("renderContentWithCodeBlocks", () => {
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
});
