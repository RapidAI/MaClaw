import { describe, expect, it, vi } from "vitest";
import { PDF_SHOT_MAX_BYTES, pdfShotContainSize, rasterizePdfShot, readShotPdfBytes, retainShotRaster } from "./pdfShotRaster";

describe("pdfShotContainSize", () => {
    it("fits a portrait page inside the shot without cropping either side", () => {
        const size = pdfShotContainSize(595, 842, 160, 120);
        expect(size.height).toBeCloseTo(120, 5);
        expect(size.width).toBeLessThan(160);
        expect(size.width).toBeGreaterThan(80);
    });

    it("fits a landscape page inside the shot without cropping either side", () => {
        const size = pdfShotContainSize(842, 595, 160, 120);
        expect(size.width).toBeCloseTo(160, 5);
        expect(size.height).toBeLessThan(120);
        expect(size.height).toBeGreaterThan(80);
    });
});

describe("rasterizePdfShot", () => {
    it("rejects an empty preview url before loading the file", async () => {
        await expect(rasterizePdfShot("  ", 1, 160, 120)).rejects.toThrow(/empty pdf preview/);
    });

    it("rejects an oversized preview before copying the body", async () => {
        expect(PDF_SHOT_MAX_BYTES).toBe(128 * 1024 * 1024);
        const fetchMock = vi.fn(async () => ({
            ok: true,
            headers: { get: (name: string) => (name.toLowerCase() === "content-length" ? String(PDF_SHOT_MAX_BYTES + 1) : null) },
            body: {
                cancel: async () => undefined,
                getReader: () => { throw new Error("body was read"); },
            },
            arrayBuffer: async () => { throw new Error("body was read"); },
        }));
        vi.stubGlobal("fetch", fetchMock);
        try {
            await expect(rasterizePdfShot("/maclaw-preview/v1/file?t=0123456789abcdef0123456789abcdef", 1)).rejects.toThrow(/too large/);
            expect(fetchMock).toHaveBeenCalledTimes(1);
        } finally {
            vi.unstubAllGlobals();
        }
    });
});

describe("readShotPdfBytes", () => {
    it("returns a body that fits in the shot buffer", async () => {
        const response = new Response(new Uint8Array([1, 2, 3, 4, 5]));
        const data = await readShotPdfBytes(response, 8);
        expect(Array.from(data)).toEqual([1, 2, 3, 4, 5]);
    });

    it("copies each chunk before the stream reuses its buffer", async () => {
        const shared = new Uint8Array(3);
        let phase = 0;
        const stream = new ReadableStream<Uint8Array>({
            pull(controller) {
                if (phase === 0) {
                    shared.set([1, 2, 3]);
                    controller.enqueue(shared);
                    phase = 1;
                    return;
                }
                if (phase === 1) {
                    shared.set([4, 5, 6]);
                    controller.enqueue(shared);
                    phase = 2;
                    return;
                }
                controller.close();
            },
        });
        const data = await readShotPdfBytes(new Response(stream), 8);
        expect(Array.from(data)).toEqual([1, 2, 3, 4, 5, 6]);
    });

    it("stops a chunked body once it passes the buffer", async () => {
        const stream = new ReadableStream<Uint8Array>({
            start(controller) {
                controller.enqueue(new Uint8Array([1, 2, 3]));
                controller.enqueue(new Uint8Array([4, 5, 6]));
                controller.close();
            },
        });
        await expect(readShotPdfBytes(new Response(stream), 4)).rejects.toThrow(/too large/);
    });
});

describe("retainShotRaster", () => {
    it("keeps the newest eight rasters and refreshes a repeated key", () => {
        const cache = new Map<string, string>();
        for (let i = 0; i < 10; i++) retainShotRaster(cache, String(i), String(i));
        expect([...cache.keys()]).toEqual(["2", "3", "4", "5", "6", "7", "8", "9"]);
        retainShotRaster(cache, "2", "2");
        expect(cache.size).toBe(8);
        expect([...cache.keys()]).toEqual(["3", "4", "5", "6", "7", "8", "9", "2"]);
    });
});
