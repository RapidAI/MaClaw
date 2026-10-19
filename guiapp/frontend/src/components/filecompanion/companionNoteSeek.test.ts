import { describe, expect, it } from "vitest";
import { noteStampAtLineStart, noteTimestampSeconds } from "./companionNoteSeek";

describe("noteStampAtLineStart", () => {
    it("seeks a chapter range to its start and keeps a plain stamp", () => {
        expect(noteStampAtLineStart("0:00–2:00 机器翻译的几个时代（推断）")).toEqual({
            label: "0:00–2:00",
            seconds: 0,
            rest: "机器翻译的几个时代（推断）",
        });
        expect(noteStampAtLineStart("- 3:00–4:00 Transformer 编码器（推断）")).toEqual({
            label: "3:00–4:00",
            seconds: 180,
            rest: "Transformer 编码器（推断）",
        });
        expect(noteStampAtLineStart("0:00-2:00 用连字符")).toEqual({
            label: "0:00-2:00",
            seconds: 0,
            rest: "用连字符",
        });
        expect(noteStampAtLineStart("1:02:03–1:10:00 后一节")).toEqual({
            label: "1:02:03–1:10:00",
            seconds: 3723,
            rest: "后一节",
        });
        expect(noteStampAtLineStart("12:05 讲了板书")).toEqual({
            label: "12:05",
            seconds: 725,
            rest: "讲了板书",
        });
        expect(noteStampAtLineStart("1:02:03 讲了下一节")?.seconds).toBe(3723);
        expect(noteStampAtLineStart("- 12:05 only in the plan")?.label).toBe("12:05");
        expect(noteStampAtLineStart("90:00–91:00 九十分钟")).toEqual({
            label: "90:00–91:00",
            seconds: 5400,
            rest: "九十分钟",
        });
        expect(noteStampAtLineStart("12:99 不是时间")).toBeNull();
        expect(noteStampAtLineStart("99:99 也不是")).toBeNull();
        expect(noteStampAtLineStart("12:99–13:00 秒越界")).toBeNull();
        expect(noteStampAtLineStart("没有时间的一句")).toBeNull();
        expect(noteStampAtLineStart("时间不明 太晚")).toBeNull();
        expect(noteTimestampSeconds("12:05")).toBe(725);
    });
});
