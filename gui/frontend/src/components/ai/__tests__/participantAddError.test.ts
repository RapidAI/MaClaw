import { describe, expect, it } from "vitest";
import { participantAddErrorText } from "../participantAddError";

describe("participantAddErrorText", () => {
    it("keeps backend details visible in English", () => {
        expect(participantAddErrorText(new Error("runtime missing"), "en")).toBe("Failed to add: runtime missing");
    });

    it("keeps backend details visible in Chinese", () => {
        expect(participantAddErrorText(new Error("runtime missing"), "zh-CN")).toBe("\u6dfb\u52a0\u5931\u8d25\uff1aruntime missing");
    });
});
