import { describe, expect, it } from "vitest";
import { isPreviewAbortError, readPreviewBody } from "../previewBytes";

function streamed(parts: Uint8Array[], length: string | null, pauseAt: number) {
    let index = 0;
    let release = () => {};
    const gate = new Promise<void>((resolve) => {
        release = resolve;
    });
    const response = {
        headers: {
            get: (name: string) => (name.toLowerCase() === "content-length" ? length : null),
        },
        body: {
            getReader: () => ({
                read: async () => {
                    if (index === pauseAt) await gate;
                    if (index >= parts.length) return { done: true as const, value: undefined };
                    const value = parts[index];
                    index += 1;
                    return { done: false as const, value };
                },
                cancel: async () => {},
            }),
        },
        arrayBuffer: async () => {
            throw new Error("stream should be used");
        },
    };
    return { response: response as unknown as Response, release: () => release() };
}

describe("readPreviewBody", () => {
    it("reports the byte ratio while a known-length body is still arriving", async () => {
        const first = new Uint8Array(40).fill(7);
        const second = new Uint8Array(60).fill(8);
        const { response, release } = streamed([first, second], "100", 1);
        const ratios: Array<number | null> = [];
        const pending = readPreviewBody(response, { onProgress: (ratio) => ratios.push(ratio) });
        const start = Date.now();
        while (!ratios.some((ratio) => ratio === 0.4) && Date.now() - start < 1000) {
            await new Promise((resolve) => setTimeout(resolve, 10));
        }
        expect(ratios).toContain(0.4);
        release();
        const bytes = await pending;
        expect(bytes.byteLength).toBe(100);
        expect(bytes[0]).toBe(7);
        expect(bytes[40]).toBe(8);
        expect(ratios[ratios.length - 1]).toBe(1);
    });

    it("stays indeterminate when the body has no length", async () => {
        const { response, release } = streamed([new Uint8Array([1, 2, 3])], null, 99);
        const ratios: Array<number | null> = [];
        const bytes = await readPreviewBody(response, { onProgress: (ratio) => ratios.push(ratio) });
        release();
        expect(bytes).toEqual(new Uint8Array([1, 2, 3]));
        expect(ratios).toEqual([null]);
    });

    it("reads a body that is already buffered", async () => {
        const response = {
            headers: { get: () => null },
            body: null,
            arrayBuffer: async () => new Uint8Array([4, 5]).buffer,
        } as unknown as Response;
        const ratios: Array<number | null> = [];
        const bytes = await readPreviewBody(response, { onProgress: (ratio) => ratios.push(ratio) });
        expect(Array.from(bytes)).toEqual([4, 5]);
        expect(ratios).toEqual([null]);
    });

    it("stops when the read is already aborted", async () => {
        const controller = new AbortController();
        controller.abort();
        const response = {
            headers: { get: () => "4" },
            body: null,
            arrayBuffer: async () => {
                throw new Error("should not buffer");
            },
        } as unknown as Response;
        await expect(readPreviewBody(response, { signal: controller.signal })).rejects.toSatisfy(isPreviewAbortError);
    });
});
