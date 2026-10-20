import { describe, expect, it } from "vitest";
import { companionNoteStatus, formatNoteElapsed, noteClockFor, noteMediaFromKind, noteStatusPercent } from "./noteStatusModel";

describe("companionNoteStatus", () => {
    it("leaves an ordinary chat line as text", () => {
        expect(companionNoteStatus("正在整理上下文并准备模型请求", "web")).toBeNull();
        expect(companionNoteStatus("临时进度", "video")).toBeNull();
    });

    it("places a web recording on the sound step", () => {
        const model = companionNoteStatus("正在记录声音", "web");
        expect(model?.steps.map((step) => step.label)).toEqual(["画面", "声音", "语音", "整理"]);
        expect(model?.index).toBe(1);
        expect(model?.precise).toBe(false);
        expect(noteStatusPercent(model!, 0)).toBe(35);
    });

    it("advances a speech segment inside the speech step", () => {
        const early = companionNoteStatus("正在识别语音（第 1/4 段）", "web");
        const later = companionNoteStatus("正在识别语音（第 2/4 段）", "web");
        expect(early?.precise).toBe(true);
        expect(early?.index).toBe(2);
        expect(noteStatusPercent(later!, 0)).toBeGreaterThan(noteStatusPercent(early!, 0));
        const done = companionNoteStatus("正在整理笔记", "web");
        expect(done?.index).toBe(3);
        expect(noteStatusPercent(done!, 0)).toBeGreaterThan(noteStatusPercent(later!, 0));
    });

    it("uses the local video pipeline for frames", () => {
        const segment = companionNoteStatus("正在识别语音（第 4/4 段）", "video")!;
        const frames = companionNoteStatus("正在抽取画面", "video")!;
        const ocr = companionNoteStatus("正在识别画面文字", "video")!;
        const write = companionNoteStatus("正在整理笔记", "video")!;
        expect(frames.steps.map((step) => step.label)).toEqual(["检查", "音轨", "语音", "画面", "整理"]);
        expect(frames.index).toBe(3);
        expect(noteStatusPercent(frames, 0)).toBeGreaterThan(noteStatusPercent(segment, 0));
        expect(noteStatusPercent(ocr, 10 * 60 * 1000)).toBeGreaterThanOrEqual(noteStatusPercent(frames, 10 * 60 * 1000));
        expect(noteStatusPercent(write, 0)).toBeGreaterThan(noteStatusPercent(ocr, 0));
    });

    it("keeps opus and silk notes off the picture step", () => {
        expect(noteMediaFromKind("code", "C:/rec/voice.opus")).toBe("audio");
        expect(noteMediaFromKind("code", "voice.silk")).toBe("audio");
        const model = companionNoteStatus("正在提取音轨", noteMediaFromKind("code", "voice.opus"));
        expect(model?.steps.map((step) => step.id)).toEqual(["check", "audio", "speech", "write"]);
    });

    it("keeps an audio note off the picture step", () => {
        const model = companionNoteStatus("正在提取音轨", "audio");
        expect(model?.steps.map((step) => step.id)).toEqual(["check", "audio", "speech", "write"]);
        expect(model?.index).toBe(1);
    });

    it("does not pull a finished step backward", () => {
        const model = companionNoteStatus("正在识别画面文字", "web")!;
        expect(noteStatusPercent(model, 5 * 60 * 1000)).toBe(noteStatusPercent(model, 0));
    });

    it("stops before the next phrase of the same step", () => {
        const install = companionNoteStatus("正在安装视频解码器", "video")!;
        const extract = companionNoteStatus("正在提取音轨", "video")!;
        expect(noteStatusPercent(install, 10 * 60 * 1000)).toBeLessThanOrEqual(noteStatusPercent(extract, 0));
        const check = companionNoteStatus("正在检查文件", "video")!;
        const subs = companionNoteStatus("正在读取字幕", "video")!;
        expect(noteStatusPercent(check, 10 * 60 * 1000)).toBeLessThanOrEqual(noteStatusPercent(subs, 0));
    });

    it("creeps inside the current step and does not finish it", () => {
        const model = companionNoteStatus("正在记录声音", "web")!;
        const started = noteStatusPercent(model, 0);
        const later = noteStatusPercent(model, 3 * 60 * 1000);
        expect(later).toBeGreaterThan(started);
        expect(later).toBeLessThan(noteStatusPercent(companionNoteStatus("正在识别语音（第 1/4 段）", "web")!, 0));
    });

    it("keeps one clock per document and restarts a phrase when the sentence changes", () => {
        const first = noteClockFor("", "web", undefined, undefined, "正在记录声音", 1_000);
        const same = noteClockFor("正在记录声音", "web", first.noteStartedAt, first.phaseStartedAt, "正在记录声音", 5_000);
        const next = noteClockFor("正在记录声音", "web", same.noteStartedAt, same.phaseStartedAt, "正在整理笔记", 9_000);
        const other = noteClockFor("", "video", undefined, undefined, "正在检查文件", 9_000);
        const cleared = noteClockFor("正在整理笔记", "web", next.noteStartedAt, next.phaseStartedAt, "临时进度", 12_000);
        expect(same).toEqual(first);
        expect(next.noteStartedAt).toBe(1_000);
        expect(next.phaseStartedAt).toBe(9_000);
        expect(other.noteStartedAt).toBe(9_000);
        expect(cleared).toEqual({ noteStartedAt: undefined, phaseStartedAt: undefined });
    });

    it("formats the elapsed clock", () => {
        expect(formatNoteElapsed(0)).toBe("00:00");
        expect(formatNoteElapsed(65_000)).toBe("01:05");
        expect(formatNoteElapsed(3_600_000 + 125_000)).toBe("1:02:05");
    });
});
