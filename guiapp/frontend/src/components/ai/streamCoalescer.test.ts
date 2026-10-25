import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { STREAM_COALESCE_FLUSH_MS, createStreamCoalescer } from "./streamCoalescer";

describe("createStreamCoalescer", () => {
    beforeEach(() => {
        vi.useFakeTimers();
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    it("applies the first delta immediately and buffers later deltas for one 33ms flush", () => {
        const flushed: Array<[string, string[]]> = [];
        const coalescer = createStreamCoalescer<string, string>({
            onFlush: (key, items) => flushed.push([key, items]),
        });

        expect(coalescer.push("r1", "a")).toBe(true);
        expect(coalescer.push("r1", "b")).toBe(false);
        expect(coalescer.push("r1", "c")).toBe(false);
        expect(flushed).toEqual([]);

        vi.advanceTimersByTime(STREAM_COALESCE_FLUSH_MS);
        expect(flushed).toEqual([["r1", ["b", "c"]]]);
    });

    it("flushes again on the next burst boundary after a flush", () => {
        const flushed: string[][] = [];
        const coalescer = createStreamCoalescer<string, string>({
            onFlush: (_key, items) => flushed.push(items),
        });

        coalescer.push("r1", "a");
        coalescer.push("r1", "b");
        vi.advanceTimersByTime(STREAM_COALESCE_FLUSH_MS);
        expect(flushed).toEqual([["b"]]);

        // A new burst starts: its first delta is immediate again.
        expect(coalescer.push("r1", "c")).toBe(true);
        expect(coalescer.push("r1", "d")).toBe(false);
        vi.advanceTimersByTime(STREAM_COALESCE_FLUSH_MS);
        expect(flushed).toEqual([["b"], ["d"]]);
    });

    it("keeps buffers independent per key", () => {
        const flushed: Array<[string, string[]]> = [];
        const coalescer = createStreamCoalescer<string, string>({
            onFlush: (key, items) => flushed.push([key, items]),
        });

        coalescer.push("r1", "a1");
        coalescer.push("r2", "a2");
        coalescer.push("r1", "b1");
        coalescer.push("r2", "b2");
        vi.advanceTimersByTime(STREAM_COALESCE_FLUSH_MS);
        expect(flushed).toEqual([
            ["r1", ["b1"]],
            ["r2", ["b2"]],
        ]);
    });

    it("flush applies pending deltas immediately and resets the burst", () => {
        const flushed: string[][] = [];
        const coalescer = createStreamCoalescer<string, string>({
            onFlush: (_key, items) => flushed.push(items),
        });

        coalescer.push("r1", "a");
        coalescer.push("r1", "b");
        coalescer.flush("r1");
        expect(flushed).toEqual([["b"]]);

        vi.advanceTimersByTime(STREAM_COALESCE_FLUSH_MS * 3);
        expect(flushed).toEqual([["b"]]);

        expect(coalescer.push("r1", "c")).toBe(true);
    });

    it("flush with an empty buffer is a no-op", () => {
        const flushed: string[][] = [];
        const coalescer = createStreamCoalescer<string, string>({
            onFlush: (_key, items) => flushed.push(items),
        });

        coalescer.flush("missing");
        coalescer.push("r1", "a");
        coalescer.flush("r1");
        coalescer.flush("r1");
        expect(flushed).toEqual([]);
    });

    it("drop discards pending deltas without flushing", () => {
        const flushed: string[][] = [];
        const coalescer = createStreamCoalescer<string, string>({
            onFlush: (_key, items) => flushed.push(items),
        });

        coalescer.push("r1", "a");
        coalescer.push("r1", "b");
        coalescer.drop("r1");
        vi.advanceTimersByTime(STREAM_COALESCE_FLUSH_MS * 3);
        expect(flushed).toEqual([]);
        expect(coalescer.push("r1", "c")).toBe(true);
    });

    it("dispose flushes every buffered key and stops timers", () => {
        const flushed: Array<[string, string[]]> = [];
        const coalescer = createStreamCoalescer<string, string>({
            onFlush: (key, items) => flushed.push([key, items]),
        });

        coalescer.push("r1", "a1");
        coalescer.push("r1", "b1");
        coalescer.push("r2", "a2");
        coalescer.push("r2", "b2");
        coalescer.dispose();
        expect(flushed).toEqual([
            ["r1", ["b1"]],
            ["r2", ["b2"]],
        ]);

        vi.advanceTimersByTime(STREAM_COALESCE_FLUSH_MS * 3);
        expect(flushed).toHaveLength(2);
    });
});
