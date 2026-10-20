import { describe, expect, it } from "vitest";
import { pdfShotContainSize, rasterizePdfShot, retainShotRaster } from "./pdfShotRaster";

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
