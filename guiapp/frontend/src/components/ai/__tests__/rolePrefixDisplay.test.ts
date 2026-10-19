import { describe, expect, it } from "vitest";
import { appendRolePrefixStrip, createRolePrefixStreamCursor, findRolePrefixForDisplay, isBrowserEchoFieldLabel, stripRolePrefixForDisplay, truncateRolePrefixForDisplay } from "../rolePrefixDisplay";

describe("rolePrefixDisplay", () => {
    it("matches only the Browser echo chip label", () => {
        expect(isBrowserEchoFieldLabel("Browser")).toBe(true);
        expect(isBrowserEchoFieldLabel(" browser ")).toBe(true);
        expect(isBrowserEchoFieldLabel("Browser session")).toBe(false);
        expect(isBrowserEchoFieldLabel("浏览器")).toBe(false);
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
        expect(stripRolePrefixForDisplay("1) Tool: 调用回显。")).toBe("调用回显。");
        expect(stripRolePrefixForDisplay("> - Browser: 重复回答。")).toBe("重复回答。");
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
});

describe("appendRolePrefixStrip", () => {
    const samples = [
        "The Browser: tool already closed cleanly.",
        "有效回答。\n\nBrowser: 重复回答。",
        "```text\nBrowser: connected\n```",
        "```text\nBrowser: connected\n```\nTool: after the fence",
        "> Browser：重复回答。",
        "1. Tool: 调用回显。",
        "Hello Tool: stay",
        "Browser: hidden tool echo",
        "thinking\nBrowser: hidden tool echo",
        "Brow" + "ser: visible",
        "hello\rTool: after cr",
        "``" + "`\nTool: inside",
        "```\ncode\n```\nTool: outside",
        "Toolbox is not a prefix\nstill prose",
    ];

    it("matches the full scan for keep-body and truncate, one chunk at a time", () => {
        for (const sample of samples) {
            for (const step of [1, 3, 8]) {
                const cursor = createRolePrefixStreamCursor("keep-body");
                let built = "";
                while (built.length < sample.length) {
                    built = sample.slice(0, Math.min(sample.length, built.length + step));
                    expect(appendRolePrefixStrip(cursor, built)).toBe(stripRolePrefixForDisplay(built));
                }
                const truncated = createRolePrefixStreamCursor("truncate");
                built = "";
                while (built.length < sample.length) {
                    built = sample.slice(0, Math.min(sample.length, built.length + step));
                    expect(appendRolePrefixStrip(truncated, built)).toBe(truncateRolePrefixForDisplay(built));
                }
            }
        }
    });

    it("keeps a leading body growing after Browser and drops a later Tool tail", () => {
        const cursor = createRolePrefixStreamCursor("keep-body");
        expect(appendRolePrefixStrip(cursor, "Browser: hello")).toBe("hello");
        expect(appendRolePrefixStrip(cursor, "Browser: hello world")).toBe("hello world");
        const cut = createRolePrefixStreamCursor("keep-body");
        expect(appendRolePrefixStrip(cut, "answer\nTool: echo")).toBe("answer");
        expect(appendRolePrefixStrip(cut, "answer\nTool: echo more")).toBe("answer");
    });
});
