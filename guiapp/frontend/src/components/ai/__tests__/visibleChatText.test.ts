import { describe, expect, it } from "vitest";
import { firstVEStreamText, sanitizeVisibleChatText, sanitizeVisibleVEText, visibleHistoryMessageContent, visibleVEStreamContent } from "../visibleChatText";

describe("visibleChatText", () => {
    it("drops reasoning-lane stream deltas and keeps assembled answers", () => {
        expect(visibleVEStreamContent("\x01private reasoning")).toBe("");
        expect(sanitizeVisibleVEText("\x01visible answer")).toBe("visible answer");
        expect(visibleVEStreamContent("I am \uEB90Kate")).toBe("I am Kate");
        expect(sanitizeVisibleChatText("\x01The\x01 user\x01 wants")).toBe("The user wants");
        expect(sanitizeVisibleVEText("\x01The\x01 user\x01 wants")).toBe("The user wants");
        expect(sanitizeVisibleChatText("reason\u25A1ing")).toBe("reasoning");
    });

    it("prefers the first non-empty string and treats history kinds correctly", () => {
        expect(firstVEStreamText("", "legacy")).toBe("legacy");
        expect(visibleHistoryMessageContent("stream_chunk", "\x01private", "visible")).toBe("");
        expect(visibleHistoryMessageContent("answer", "", "\x01kept answer")).toBe("kept answer");
    });
});
