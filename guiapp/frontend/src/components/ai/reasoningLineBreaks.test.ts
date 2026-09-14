import { describe, expect, it } from "vitest";
import { repairReasoningLineBreaks } from "./reasoningLineBreaks";

describe("repairReasoningLineBreaks", () => {
    it("joins a newline immediately inside fullwidth brackets", () => {
        expect(repairReasoningLineBreaks("整理歌曲（\n1996\n）列表")).toBe("整理歌曲（1996）列表");
    });

    it("joins a hyphenated line range split inside parentheses", () => {
        expect(repairReasoningLineBreaks("snake.cpp (lines 251-\n504)")).toBe("snake.cpp (lines 251-504)");
    });

    it("joins a hyphenated numeric range even outside parentheses", () => {
        expect(repairReasoningLineBreaks("see 251-\n504 in the log")).toBe("see 251-504 in the log");
        expect(repairReasoningLineBreaks("see 251–\n504 in the log")).toBe("see 251–504 in the log");
    });

    it("joins a column wrap inside a parenthetical", () => {
        expect(repairReasoningLineBreaks("(ninja log only\nrecords completed outputs)")).toBe(
            "(ninja log only records completed outputs)",
        );
    });

    it("does not join ordinary prose wraps outside parentheses", () => {
        expect(repairReasoningLineBreaks("Let me read the rest of\nsnake.cpp")).toBe(
            "Let me read the rest of\nsnake.cpp",
        );
    });

    it("keeps ordered and bullet lists on their own lines", () => {
        expect(repairReasoningLineBreaks("1. Read the file\n2. Compile")).toBe("1. Read the file\n2. Compile");
        expect(repairReasoningLineBreaks("- alpha\n- beta")).toBe("- alpha\n- beta");
    });

    it("does not let an unclosed paren swallow a following numbered list", () => {
        expect(repairReasoningLineBreaks("Need to inspect foo(\n1. Read the file\n2. Compile")).toBe(
            "Need to inspect foo(\n1. Read the file\n2. Compile",
        );
        expect(repairReasoningLineBreaks("Need to inspect foo(\n2) Compile")).toBe(
            "Need to inspect foo(\n2) Compile",
        );
    });

    it("does not join a hyphenated wrap onto a following numbered list", () => {
        expect(repairReasoningLineBreaks("versions 1-\n2. Compile")).toBe("versions 1-\n2. Compile");
        expect(repairReasoningLineBreaks("versions 1-\n2) Compile")).toBe("versions 1-\n2) Compile");
    });

    it("keeps blank-line paragraph breaks", () => {
        expect(repairReasoningLineBreaks("first paragraph.\n\nsecond paragraph.")).toBe(
            "first paragraph.\n\nsecond paragraph.",
        );
    });

    it("does not rewrite fenced code", () => {
        const fenced = "before\n```\n(lines 251-\n504)\n```\nafter";
        expect(repairReasoningLineBreaks(fenced)).toBe(fenced);
    });

    it("does not join prose onto a closing fence", () => {
        expect(repairReasoningLineBreaks("```\ncode\n```\nstill outside")).toBe("```\ncode\n```\nstill outside");
    });

    it("joins the thinking-panel wrap from a coding-agent thought", () => {
        const wrapped = [
            "The build log shows successful compile+link of snake.cpp.obj and snake.exe multiple times, meaning builds succeeded (ninja log only records completed outputs). Let me read the rest of",
            "snake.cpp (lines 251-",
            "504) to confirm it's complete, especially WinMain, selftest, etc.",
        ].join("\n");
        const repaired = repairReasoningLineBreaks(wrapped);
        expect(repaired).toContain("snake.cpp (lines 251-504)");
        expect(repaired).not.toContain("251-\n");
        expect(repaired).toBe([
            "The build log shows successful compile+link of snake.cpp.obj and snake.exe multiple times, meaning builds succeeded (ninja log only records completed outputs). Let me read the rest of",
            "snake.cpp (lines 251-504) to confirm it's complete, especially WinMain, selftest, etc.",
        ].join("\n"));
    });
});
