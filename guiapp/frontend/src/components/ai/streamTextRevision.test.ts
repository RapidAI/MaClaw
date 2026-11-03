import { describe, expect, it } from "vitest";
import { markStreamTextReplacement, streamTextRevision } from "./streamTextRevision";

describe("streamTextRevision", () => {
    it("keeps revisions monotonic and bounds retained message metadata", () => {
        for (let index = 0; index <= 1024; index += 1) {
            markStreamTextReplacement(`message-${index}`);
        }

        expect(streamTextRevision("message-0")).toBe(0);
        expect(streamTextRevision("message-1")).toBe(1);
        expect(streamTextRevision("message-1024")).toBe(1);
        markStreamTextReplacement("message-1024");
        expect(streamTextRevision("message-1024")).toBe(2);
    });
});
