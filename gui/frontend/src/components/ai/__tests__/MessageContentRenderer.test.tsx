import React from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MessageContentRenderer } from "../MessageContentRenderer";
import { findRolePrefixForDisplay, stripRolePrefixForDisplay, truncateRolePrefixForDisplay } from "../rolePrefixDisplay";
import { lightTheme } from "../aiAssistantPanelTheme";

describe("MessageContentRenderer role prefix display guard", () => {
    it("strips a leading Browser role prefix before rendering assistant content", () => {
        render(<MessageContentRenderer content="Browser: 现在情况清楚了。" theme={lightTheme} />);

        expect(screen.getByText("现在情况清楚了。")).toBeTruthy();
        expect(screen.queryByText(/Browser:/)).toBeNull();
    });

    it("truncates a streamed Browser tail after valid assistant content", () => {
        const content = "有效回答。\n\nBrowser: 重复回答。";

        expect(stripRolePrefixForDisplay(content)).toBe("有效回答。");
    });

    it("does not strip Browser text inside fenced code blocks", () => {
        const content = "```text\nBrowser: connected\n```";

        expect(stripRolePrefixForDisplay(content)).toBe(content);
    });

    it("strips markdown-quoted role prefixes", () => {
        expect(stripRolePrefixForDisplay("> Browser：重复回答。")).toBe("重复回答。");
    });

    it("strips list and numbered markdown role prefixes", () => {
        expect(stripRolePrefixForDisplay("- Browser: 重复回答。")).toBe("重复回答。");
        expect(stripRolePrefixForDisplay("1. Tool: 调用回显。")).toBe("调用回显。");
    });

    it("truncates role-prefixed tails without consuming the previous newline as prefix", () => {
        expect(stripRolePrefixForDisplay("有效回答。\n\nBrowser: 重复回答。")).toBe("有效回答。");
    });

    it("reports display role prefix match metadata for diagnostics", () => {
        expect(findRolePrefixForDisplay("有效回答。\n\n- Tool: 调用回显。")).toMatchObject({
            kind: "Tool",
            atStart: false,
        });
    });

    it("does not report role prefixes inside fenced code blocks", () => {
        expect(findRolePrefixForDisplay("```text\nBrowser: connected\n```")).toBeNull();
    });

    it("truncates role-prefixed reasoning instead of keeping the tool echo body", () => {
        expect(truncateRolePrefixForDisplay("Browser: hidden tool echo")).toBe("");
        expect(truncateRolePrefixForDisplay("thinking\nBrowser: hidden tool echo")).toBe("thinking");
    });

    it("keeps normal inline mentions of Browser text", () => {
        const content = "The Browser: tool already closed cleanly.";

        expect(stripRolePrefixForDisplay(content)).toBe(content);
    });

    it("does not strip user-authored content", () => {
        render(<MessageContentRenderer content="Browser: 我输入的文本" theme={lightTheme} isUser />);

        expect(screen.getByText("Browser: 我输入的文本")).toBeTruthy();
    });
});
