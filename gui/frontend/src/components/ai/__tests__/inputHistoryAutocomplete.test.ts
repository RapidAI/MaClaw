import { describe, expect, it } from "vitest";
import { matchHistoryPrefix } from "../inputHistoryAutocompleteUtils";

describe("matchHistoryPrefix", () => {
    const history = [
        "hello world",
        "hello there",
        "实现用户登录",
        "实现用户注册功能",
        "hello world", // duplicate
        "other",
    ];

    it("returns empty for empty, exact full matches, or empty history", () => {
        expect(matchHistoryPrefix("", history)).toEqual([]);
        expect(matchHistoryPrefix("other", history)).toEqual([]);
        expect(matchHistoryPrefix("hello", [])).toEqual([]);
        expect(matchHistoryPrefix("hello", ["", null as unknown as string])).toEqual([]);
    });

    it("matches prefix, newest first, de-duplicated", () => {
        expect(matchHistoryPrefix("hello", history)).toEqual([
            "hello world",
            "hello there",
        ]);
        expect(matchHistoryPrefix("实现用户", history)).toEqual([
            "实现用户注册功能",
            "实现用户登录",
        ]);
    });

    it("respects max limit", () => {
        const many = Array.from({ length: 20 }, (_, i) => `prefix-${i}`);
        expect(matchHistoryPrefix("prefix-", many, 3)).toHaveLength(3);
        expect(matchHistoryPrefix("prefix-", many, 3)[0]).toBe("prefix-19");
    });
});
