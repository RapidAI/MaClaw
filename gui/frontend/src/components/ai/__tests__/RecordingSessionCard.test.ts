import { describe, expect, it } from "vitest";
import {
    __recordAudioTestUtils,
    formatRecordingCompletionDisplay,
    formatRecordingCompletionMessage,
    isRecordingInputLocked,
} from "../RecordingSessionCard";

const {
    COMPACT_CHUNK_THRESHOLD,
    compactInt16Chunks,
    encodeWAVFromChunks,
    encodeWAVFromInt16,
    formatDuration,
    mergeInt16Chunks,
    toBase64,
} = __recordAudioTestUtils;

describe("RecordingSessionCard helpers", () => {
    it("formatDuration pads minutes and hours", () => {
        expect(formatDuration(5)).toBe("0:05");
        expect(formatDuration(65)).toBe("1:05");
        expect(formatDuration(3661)).toBe("1:01:01");
    });

    it("formatRecordingCompletionMessage includes path and duration", () => {
        const msg = formatRecordingCompletionMessage({
            status: "stopped",
            title: "周会",
            path: "C:\\\\tmp\\\\a.wav",
            durationSec: 12.34,
            sizeBytes: 1000,
            format: "wav",
        });
        expect(msg).toContain("[Recording completed]");
        expect(msg).toContain("status: stopped");
        expect(msg).toContain("title: 周会");
        expect(msg).toContain("path: C:\\\\tmp\\\\a.wav");
        expect(msg).toContain("duration_sec: 12.3");
        expect(msg).toContain("size_bytes: 1000");
        expect(msg).toContain("format: wav");
    });

    it("formatRecordingCompletionDisplay localizes cancel and error", () => {
        expect(formatRecordingCompletionDisplay({ status: "cancelled" }, "zh")).toContain("取消");
        expect(formatRecordingCompletionDisplay({ status: "error", error: "boom" }, "en")).toContain("boom");
        expect(formatRecordingCompletionDisplay({ status: "stopped", path: "/a.wav", durationSec: 9 }, "zh")).toContain(
            "录音已保存",
        );
    });

    it("encodeWAVFromInt16 writes valid RIFF/WAVE header and PCM size", () => {
        const samples = new Int16Array([0, 1000, -1000, 0]);
        const buf = encodeWAVFromInt16(samples, 16000);
        const bytes = new Uint8Array(buf);
        const tag = (off: number, n: number) => String.fromCharCode(...bytes.subarray(off, off + n));
        expect(tag(0, 4)).toBe("RIFF");
        expect(tag(8, 4)).toBe("WAVE");
        expect(tag(12, 4)).toBe("fmt ");
        expect(tag(36, 4)).toBe("data");
        expect(buf.byteLength).toBe(44 + samples.length * 2);
        const view = new DataView(buf);
        expect(view.getUint32(40, true)).toBe(samples.length * 2);
        expect(view.getUint32(24, true)).toBe(16000);
        expect(view.getUint16(22, true)).toBe(1);
    });

    it("toBase64 round-trips small buffers", () => {
        const raw = new Uint8Array([1, 2, 3, 4, 255]).buffer;
        const b64 = toBase64(raw);
        expect(b64).toBe(btoa(String.fromCharCode(1, 2, 3, 4, 255)));
    });

    it("mergeInt16Chunks concatenates in order", () => {
        const a = new Int16Array([1, 2]);
        const b = new Int16Array([3]);
        const merged = mergeInt16Chunks([a, b], 3);
        expect(Array.from(merged)).toEqual([1, 2, 3]);
    });

    it("compactInt16Chunks merges head when over threshold", () => {
        const many: Int16Array[] = [];
        for (let i = 0; i < COMPACT_CHUNK_THRESHOLD; i++) {
            many.push(new Int16Array([i]));
        }
        const compacted = compactInt16Chunks(many);
        expect(compacted.length).toBeLessThan(many.length);
        const flat = mergeInt16Chunks(compacted, many.length);
        expect(Array.from(flat)).toEqual(many.map((_, i) => i));
    });

    it("encodeWAVFromChunks matches encodeWAVFromInt16", () => {
        const a = new Int16Array([1, 2, 3]);
        const b = new Int16Array([4, 5]);
        const fromChunks = new Uint8Array(encodeWAVFromChunks([a, b], 5, 16000));
        const merged = mergeInt16Chunks([a, b], 5);
        const fromMerged = new Uint8Array(encodeWAVFromInt16(merged, 16000));
        expect(Array.from(fromChunks)).toEqual(Array.from(fromMerged));
    });

    it("encodeWAVFromChunks trusts chunk lengths over wrong totalSamples", () => {
        const a = new Int16Array([10, 20]);
        const b = new Int16Array([30]);
        // Stale totalSamples=99 must not produce oversized empty-padded WAV.
        const buf = encodeWAVFromChunks([a, b], 99, 16000);
        expect(buf.byteLength).toBe(44 + 3 * 2);
        const view = new DataView(buf);
        expect(view.getUint32(40, true)).toBe(6);
    });

    it("isRecordingInputLocked matches last-assistant active session only", () => {
        expect(isRecordingInputLocked([], false)).toBe(false);
        expect(
            isRecordingInputLocked(
                [{ role: "assistant", recordingSession: { active: true } }],
                true,
            ),
        ).toBe(false);
        expect(
            isRecordingInputLocked(
                [{ role: "assistant", recordingSession: { active: true } }],
                false,
            ),
        ).toBe(true);
        // Stale active on older assistant must not lock once a newer assistant exists.
        expect(
            isRecordingInputLocked(
                [
                    { role: "assistant", recordingSession: { active: true } },
                    { role: "user" },
                    { role: "assistant", recordingSession: { active: false } },
                ],
                false,
            ),
        ).toBe(false);
        expect(
            isRecordingInputLocked(
                [
                    { role: "assistant", recordingSession: { active: true } },
                    { role: "user" },
                    { role: "assistant" },
                ],
                false,
            ),
        ).toBe(false);
    });
});
