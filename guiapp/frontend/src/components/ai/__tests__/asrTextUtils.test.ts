import { describe, expect, it } from "vitest";
import {
    isPunctuationOnlyASRText,
    normalizeASRText,
    resolveNormalizedVoiceText,
    shouldDispatchASRText,
} from "../asrTextUtils";

describe("normalizeASRText", () => {
    it("trims strings and coerces nullish / non-string values", () => {
        expect(normalizeASRText("  你好  ")).toBe("你好");
        expect(normalizeASRText("")).toBe("");
        expect(normalizeASRText(null)).toBe("");
        expect(normalizeASRText(undefined)).toBe("");
        expect(normalizeASRText(123)).toBe("123");
    });
});

describe("isPunctuationOnlyASRText / shouldDispatchASRText", () => {
    it("treats empty and whitespace as noise", () => {
        expect(isPunctuationOnlyASRText("")).toBe(true);
        expect(isPunctuationOnlyASRText("   ")).toBe(true);
        expect(isPunctuationOnlyASRText("\n\t")).toBe(true);
        expect(isPunctuationOnlyASRText(null)).toBe(true);
        expect(shouldDispatchASRText("")).toBe(false);
    });

    it("ignores lone Chinese and ASCII punctuation from continuous ASR", () => {
        const noise = ["。", ".", "！", "?", "…", "、", "，", "。。", "...", "。！？", " !?.,;: ", "——"];
        for (const text of noise) {
            expect(isPunctuationOnlyASRText(text)).toBe(true);
            expect(shouldDispatchASRText(text)).toBe(false);
        }
    });

    it("ignores pure symbols with no speech content", () => {
        expect(isPunctuationOnlyASRText("~")).toBe(true);
        expect(isPunctuationOnlyASRText("\u2605")).toBe(true);
        expect(isPunctuationOnlyASRText("※")).toBe(true);
        expect(shouldDispatchASRText("\u2605")).toBe(false);
    });

    it("keeps real speech content with trailing punctuation", () => {
        const keep = ["查询北京天气。", "hello.", "嗯。", "OK", "123"];
        for (const text of keep) {
            expect(isPunctuationOnlyASRText(text)).toBe(false);
            expect(shouldDispatchASRText(text)).toBe(true);
        }
    });
});

describe("resolveNormalizedVoiceText", () => {
    it("uses corrected text when LLM fixes ASR for continuous mode", () => {
        const got = resolveNormalizedVoiceText("查询背景天气", {
            is_command: true,
            corrected_text: "查询北京天气",
        }, "continuous");
        expect(got).toEqual({ dispatch: true, text: "查询北京天气", reason: "corrected" });
    });

    it("drops non-commands in continuous mode even if corrected_text is empty", () => {
        const got = resolveNormalizedVoiceText("哈哈哈", {
            is_command: false,
            corrected_text: "",
        }, "continuous");
        expect(got.dispatch).toBe(false);
        expect(got.reason).toBe("not_a_command");
    });

    it("keeps hold mic input even when LLM says not a command", () => {
        const got = resolveNormalizedVoiceText("嗯那个", {
            is_command: false,
            corrected_text: "",
        }, "hold");
        // Empty corrected falls back to raw input; hold never drops.
        expect(got.dispatch).toBe(true);
        expect(got.text).toBe("嗯那个");
    });

    it("falls back to raw when normalization is missing", () => {
        const got = resolveNormalizedVoiceText("打开设置", null, "continuous");
        expect(got).toEqual({ dispatch: true, text: "打开设置", reason: "no_normalization" });
    });

    it("treats empty corrected_text on a command as keep-raw (backend fallback semantics)", () => {
        const got = resolveNormalizedVoiceText("查询天气", {
            is_command: true,
            corrected_text: "",
        }, "continuous");
        expect(got).toEqual({ dispatch: true, text: "查询天气", reason: "unchanged" });
    });

    it("keeps raw ASR when correction is punctuation-only (do not drop good speech)", () => {
        const got = resolveNormalizedVoiceText("查询天气", {
            is_command: true,
            corrected_text: "。",
        }, "continuous");
        expect(got).toEqual({ dispatch: true, text: "查询天气", reason: "unchanged" });
    });

    it("keeps identical corrected text as unchanged", () => {
        const got = resolveNormalizedVoiceText("查询天气", {
            is_command: true,
            corrected_text: "查询天气",
        }, "continuous");
        expect(got).toEqual({ dispatch: true, text: "查询天气", reason: "unchanged" });
    });
});
