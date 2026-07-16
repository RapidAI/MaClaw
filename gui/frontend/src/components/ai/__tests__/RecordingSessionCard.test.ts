import { describe, expect, it } from "vitest";
import {
    __recordAudioTestUtils,
    formatRecordingCompletionDisplay,
    formatRecordingCompletionMessage,
    isRecordingInputLocked,
} from "../RecordingSessionCard";

const {
    COMPACT_CHUNK_THRESHOLD,
    COMPACT_MAX_HEAD_BYTES,
    LIVE_MAX_QUEUED_FLUSHES,
    LIVE_STREAM_FLUSH_BYTES,
    UPLOAD_CHUNK_BYTES,
    compactInt16Chunks,
    encodeWAVFromChunks,
    encodeWAVFromInt16,
    formatDuration,
    formatRecordingSaveError,
    isLikelyNetworkFetchError,
    mergeInt16Chunks,
    splitPcmForUpload,
    takePendingPCM,
    takePendingInt16AsBytes,
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

    it("toBase64 round-trips small buffers and Uint8Array views", () => {
        const raw = new Uint8Array([1, 2, 3, 4, 255]).buffer;
        const b64 = toBase64(raw);
        expect(b64).toBe(btoa(String.fromCharCode(1, 2, 3, 4, 255)));
        // View into a larger buffer must encode only the view range.
        const bigger = new Uint8Array([0, 0, 10, 20, 30, 0]);
        const view = bigger.subarray(2, 5);
        expect(toBase64(view)).toBe(btoa(String.fromCharCode(10, 20, 30)));
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

    it("compactInt16Chunks caps each head under COMPACT_MAX_HEAD_BYTES", () => {
        // Simulate many capture slabs that would formerly merge into one multi-MB head.
        const many: Int16Array[] = [];
        const samplesPer = 8000; // 16kB each
        for (let i = 0; i < COMPACT_CHUNK_THRESHOLD; i++) {
            const slab = new Int16Array(samplesPer);
            slab.fill(i);
            many.push(slab);
        }
        const totalSamples = many.reduce((n, c) => n + c.length, 0);
        const compacted = compactInt16Chunks(many, COMPACT_MAX_HEAD_BYTES);
        for (const c of compacted) {
            expect(c.byteLength).toBeLessThanOrEqual(COMPACT_MAX_HEAD_BYTES);
        }
        const flat = mergeInt16Chunks(compacted, totalSamples);
        expect(flat.length).toBe(totalSamples);
        // First sample of first source slab preserved.
        expect(flat[0]).toBe(0);
        // Last sample of last head slab (before keepTail) preserved.
        expect(flat[totalSamples - 1]).toBe(COMPACT_CHUNK_THRESHOLD - 1);
    });

    it("splitPcmForUpload slices compacted multi-MB heads under maxBytes", () => {
        // ~43s mono 16kHz 16-bit ≈ 1.34 MiB — exceeds backend 512 KiB limit if sent whole.
        const samples = Math.floor(43 * 16000);
        const big = new Int16Array(samples);
        for (let i = 0; i < samples; i++) big[i] = i & 0xffff;
        const maxBytes = 256 * 1024;
        const slabs = splitPcmForUpload([big], maxBytes);
        expect(slabs.length).toBeGreaterThan(1);
        for (const s of slabs) {
            expect(s.byteLength).toBeLessThanOrEqual(maxBytes);
            // Even slab sizes (except possibly last if odd total — total is even).
            expect(s.byteLength % 2).toBe(0);
        }
        const total = slabs.reduce((n, s) => n + s.byteLength, 0);
        expect(total).toBe(samples * 2);
        // Round-trip: first/last sample bytes preserved across splits.
        const first = new DataView(slabs[0].buffer, slabs[0].byteOffset, slabs[0].byteLength);
        expect(first.getInt16(0, true)).toBe(big[0]);
        const lastSlab = slabs[slabs.length - 1];
        const last = new DataView(lastSlab.buffer, lastSlab.byteOffset, lastSlab.byteLength);
        expect(last.getInt16(lastSlab.byteLength - 2, true)).toBe(big[samples - 1]);
    });

    it("splitPcmForUpload keeps small chunks under UPLOAD_CHUNK_BYTES", () => {
        const a = new Int16Array(100);
        const b = new Int16Array(50);
        const slabs = splitPcmForUpload([a, b], UPLOAD_CHUNK_BYTES);
        expect(slabs.length).toBe(1);
        expect(slabs[0].byteLength).toBe((100 + 50) * 2);
    });

    it("UPLOAD_CHUNK_BYTES stays within backend 512 KiB limit", () => {
        expect(UPLOAD_CHUNK_BYTES).toBeLessThanOrEqual(512 * 1024);
        expect(UPLOAD_CHUNK_BYTES % 2).toBe(0);
        expect(COMPACT_MAX_HEAD_BYTES % 2).toBe(0);
        expect(COMPACT_MAX_HEAD_BYTES).toBe(UPLOAD_CHUNK_BYTES);
    });

    it("takePendingPCM merges and clears pending list", () => {
        const a = new Uint8Array([1, 2]);
        const b = new Uint8Array([3, 4, 5]);
        const pending = [a, b];
        const out = takePendingPCM(pending, 5);
        expect(Array.from(out)).toEqual([1, 2, 3, 4, 5]);
        expect(pending.length).toBe(0);
    });

    it("takePendingInt16AsBytes merges Int16 slabs to LE bytes", () => {
        const a = new Int16Array([0x0102, 0x0304]);
        const b = new Int16Array([0x0506]);
        const pending = [a, b];
        const out = takePendingInt16AsBytes(pending, 3);
        expect(pending.length).toBe(0);
        expect(out.byteLength).toBe(6);
        const view = new DataView(out.buffer, out.byteOffset, out.byteLength);
        expect(view.getInt16(0, true)).toBe(0x0102);
        expect(view.getInt16(2, true)).toBe(0x0304);
        expect(view.getInt16(4, true)).toBe(0x0506);
    });

    it("isLikelyNetworkFetchError detects transport failures only", () => {
        expect(isLikelyNetworkFetchError(new TypeError("Failed to fetch"))).toBe(true);
        expect(isLikelyNetworkFetchError(new Error("upload session not found"))).toBe(false);
        expect(isLikelyNetworkFetchError(new Error("chunk too large"))).toBe(false);
    });

    it("live flush constants stay within backend chunk and queue caps", () => {
        expect(LIVE_STREAM_FLUSH_BYTES).toBeLessThanOrEqual(UPLOAD_CHUNK_BYTES);
        expect(LIVE_STREAM_FLUSH_BYTES % 2).toBe(0);
        expect(LIVE_MAX_QUEUED_FLUSHES).toBeGreaterThan(0);
        expect(LIVE_STREAM_FLUSH_BYTES * LIVE_MAX_QUEUED_FLUSHES).toBeLessThanOrEqual(4 * 1024 * 1024);
    });

    it("formatRecordingSaveError maps chunk too large for zh/en", () => {
        expect(formatRecordingSaveError(new Error("chunk too large: 900000 bytes (max 524288)"), true)).toContain(
            "分片过大",
        );
        expect(formatRecordingSaveError(new Error("chunk too large"), false).toLowerCase()).toContain("chunk too large");
        expect(formatRecordingSaveError(new Error("upload session not found"), true)).toContain("会话");
        // Disk permission must not be mislabeled as microphone permission.
        const diskDenied = formatRecordingSaveError(new Error("open recordings: access denied"), true);
        expect(diskDenied).toContain("access denied");
        expect(diskDenied).not.toContain("麦克风");
        expect(formatRecordingSaveError(new Error("NotAllowedError: microphone"), true)).toContain("麦克风");
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
