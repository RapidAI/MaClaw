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

    it("does not split ordinary pictographs used inside a sentence", () => {
        render(<div>{renderContentWithCodeBlocks("Good job \u2705 keep going", lightTheme)}</div>);

        expect(screen.getByText("Good job \u2705 keep going")).toBeTruthy();
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
});
