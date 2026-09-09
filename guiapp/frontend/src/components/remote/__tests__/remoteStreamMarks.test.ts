import { describe, expect, it } from "vitest";
import {
    isPathLine,
    isUserPromptLine,
    legacyMarkVisual,
    parseLegacyStreamMark,
    SPECIAL_LINE_PREFIX,
    stripUserPromptPrefix,
} from "../remoteStreamMarks";

describe("remoteStreamMarks", () => {
    it("parses legacy stream marks without keeping the glyph in body", () => {
        expect(parseLegacyStreamMark("\u23FA Bash ls")).toEqual({ mark: "\u23FA", body: "Bash ls" });
        expect(parseLegacyStreamMark("\u2705 done")).toEqual({ mark: "\u2705", body: "done" });
        expect(parseLegacyStreamMark("\uFE0F done")).toBeNull();
        expect(parseLegacyStreamMark("plain")).toBeNull();
        // optional VS16 after mark
        expect(parseLegacyStreamMark("\u26A0\uFE0F warn")).toEqual({ mark: "\u26A0", body: "warn" });
    });

    it("maps marks to status visuals for SVG rendering", () => {
        expect(legacyMarkVisual("\u23FA")).toEqual({ kind: "status", status: "tool" });
        expect(legacyMarkVisual("\u23F3")).toEqual({ kind: "status", status: "pending" });
        expect(legacyMarkVisual("\u26A1")).toEqual({ kind: "bolt" });
        expect(legacyMarkVisual("\u2713")).toEqual({ kind: "status", status: "ok" });
        expect(legacyMarkVisual("\u2705")).toEqual({ kind: "status", status: "ok" });
        expect(legacyMarkVisual("\u26A0")).toEqual({ kind: "status", status: "warn" });
        expect(legacyMarkVisual("\u274C")).toEqual({ kind: "status", status: "error" });
        expect(legacyMarkVisual("x")).toBeNull();
    });

    it("detects special lines and path lines for stream layout", () => {
        expect(SPECIAL_LINE_PREFIX.test("### Title")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("> quote")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("- item")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("\u23FA Bash")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("C:\\work\\a")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("/Users/me/src")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("hello")).toBe(false);

        // Ordered list: full body, multi-digit, bare streaming frames, indented
        expect(SPECIAL_LINE_PREFIX.test("1. item")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("10. 世界杯")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("10.")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("11)")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("  10. nested")).toBe(true);
        expect(SPECIAL_LINE_PREFIX.test("\t11) tabbed")).toBe(true);
        // Decimals / versions must not look like ordered markers
        expect(SPECIAL_LINE_PREFIX.test("1.0")).toBe(false);
        expect(SPECIAL_LINE_PREFIX.test("v2.0. 3")).toBe(false);

        expect(isPathLine("C:\\Users\\me")).toBe(true);
        expect(isPathLine("d:\\work\\app")).toBe(true);
        expect(isPathLine("~/src/app")).toBe(true);
        expect(isPathLine("/usr/local/bin")).toBe(true);
        expect(isPathLine("/Users/me/src")).toBe(true);
        expect(isPathLine("not a path")).toBe(false);
    });

    it("strips legacy user prompt prefix for display", () => {
        expect(isUserPromptLine("\u276F hello")).toBe(true);
        expect(stripUserPromptPrefix("\u276F hello")).toBe("hello");
        expect(stripUserPromptPrefix("hello")).toBe("hello");
    });
});
