import { describe, expect, it } from "vitest";
import { CONVERSATION_VIEW_HOT_MESSAGES, splitConversationView } from "./conversationViewWindow";

describe("splitConversationView", () => {
    it("keeps the newest tail and leaves the prefix archived", () => {
        const messages = Array.from({ length: 50 }, (_, index) => ({ id: `m${index}`, role: "user" }));
        const split = splitConversationView(messages);
        expect(split.visible).toHaveLength(CONVERSATION_VIEW_HOT_MESSAGES);
        expect(split.earlier).toHaveLength(10);
        expect(split.visible[0]?.id).toBe("m10");
        expect(split.earlier[0]?.id).toBe("m0");
    });

    it("walks back to the user row when the cut lands on an assistant row", () => {
        const messages = [
            { id: "u", role: "user" },
            { id: "a", role: "assistant" },
            ...Array.from({ length: CONVERSATION_VIEW_HOT_MESSAGES - 1 }, (_, index) => ({ id: `x${index}`, role: "user" })),
        ];
        const split = splitConversationView(messages);
        expect(split.earlier).toHaveLength(0);
        expect(split.visible.map((message) => message.id)).toEqual(messages.map((message) => message.id));
    });

    it("keeps a nearby open recovery card in the live tail", () => {
        const messages = Array.from({ length: CONVERSATION_VIEW_HOT_MESSAGES + 8 }, (_, index) => ({
            id: `c${index}`,
            role: "user",
            unfinishedSlot: index === 4 ? { status: "interrupted" } : undefined,
        }));
        const split = splitConversationView(messages);
        expect(split.visible[0]?.id).toBe("c4");
        expect(split.earlier).toHaveLength(4);
    });
});
