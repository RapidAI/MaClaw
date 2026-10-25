import { describe, expect, it, vi } from "vitest";
import { deliverMermaidPdfRaster, installMermaidPdfRaster, mermaidPdfRasterRequest } from "../mermaidPdfRaster";

describe("mermaid PDF raster", () => {
    it("reads the request the PDF renderer sends", () => {
        expect(mermaidPdfRasterRequest({ id: " 7 ", source: "flowchart LR\n  A --> B" })).toEqual({
            id: "7",
            source: "flowchart LR\n  A --> B",
        });
        expect(mermaidPdfRasterRequest({ ID: "8", Source: "sequenceDiagram\n  A->>B: hi" })?.id).toBe("8");
        expect(mermaidPdfRasterRequest({ source: "flowchart LR" })).toBeNull();
    });

    it("returns the picture for that request", async () => {
        const render = vi.fn(async () => "data:image/png;base64,QQ==");
        const submit = vi.fn();
        await deliverMermaidPdfRaster({ id: "3", source: "flowchart LR\n  A --> B" }, render, submit);
        expect(render).toHaveBeenCalledWith("flowchart LR\n  A --> B");
        expect(submit).toHaveBeenCalledWith("3", "data:image/png;base64,QQ==");
    });

    it("returns an empty picture when the diagram will not draw", async () => {
        const submit = vi.fn();
        await deliverMermaidPdfRaster({ id: "4", source: "nope" }, async () => null, submit);
        expect(submit).toHaveBeenCalledWith("4", "");
        submit.mockClear();
        await deliverMermaidPdfRaster({ id: "5", source: "nope" }, async () => {
            throw new Error("parse");
        }, submit);
        expect(submit).toHaveBeenCalledWith("5", "");
    });

    it("announces again when the ready call does not reach the renderer", async () => {
        vi.useFakeTimers();
        const ready = vi.fn()
            .mockRejectedValueOnce(new Error("early"))
            .mockResolvedValue(undefined);
        const w = window as unknown as {
            go?: unknown;
            runtime?: unknown;
            __maclawMermaidPdfRaster?: boolean;
            __maclawMermaidPdfRasterWait?: boolean;
        };
        w.runtime = { EventsOn: vi.fn() };
        w.go = { main: { App: { MermaidRasterReady: ready } } };
        w.__maclawMermaidPdfRaster = false;
        w.__maclawMermaidPdfRasterWait = false;
        try {
            installMermaidPdfRaster();
            await Promise.resolve();
            await Promise.resolve();
            expect(ready).toHaveBeenCalledTimes(1);
            await vi.advanceTimersByTimeAsync(250);
            expect(ready).toHaveBeenCalledTimes(2);
        } finally {
            vi.useRealTimers();
            delete w.go;
            delete w.runtime;
            delete w.__maclawMermaidPdfRaster;
            delete w.__maclawMermaidPdfRasterWait;
        }
    });
});
