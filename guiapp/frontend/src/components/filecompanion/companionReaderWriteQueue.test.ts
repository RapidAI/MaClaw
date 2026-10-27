import { describe, expect, it } from "vitest";
import { enqueueCompanionReaderWrite } from "./companionReaderWriteQueue";

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("companion reader write queue", () => {
    it("starts the next write only after the previous one settles", async () => {
        const seen: string[] = [];
        let release: () => void = () => undefined;
        const gate = new Promise<void>((resolve) => {
            release = resolve;
        });
        enqueueCompanionReaderWrite(async () => {
            seen.push("start-a");
            await gate;
            seen.push("end-a");
        });
        enqueueCompanionReaderWrite(async () => {
            seen.push("start-b");
        });

        await tick();
        expect(seen).toEqual(["start-a"]);

        release();
        await tick();
        expect(seen).toEqual(["start-a", "end-a", "start-b"]);
    });

    it("keeps running after a write fails", async () => {
        const seen: string[] = [];
        enqueueCompanionReaderWrite(async () => {
            seen.push("bad");
            throw new Error("nope");
        });
        enqueueCompanionReaderWrite(async () => {
            seen.push("good");
        });

        await tick();
        expect(seen).toEqual(["bad", "good"]);
    });
});
