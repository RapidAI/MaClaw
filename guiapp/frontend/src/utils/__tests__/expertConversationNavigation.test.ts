import { describe, expect, it, vi } from "vitest";
import { OPEN_EXPERT_CONVERSATION_EVENT, openExpertConversation } from "../expertConversationNavigation";

describe("expertConversationNavigation", () => {
    it("opens an expert conversation with a trimmed id and name", () => {
        const seen: unknown[] = [];
        const listener = (event: Event) => seen.push((event as CustomEvent).detail);
        window.addEventListener(OPEN_EXPERT_CONVERSATION_EVENT, listener);
        try {
            openExpertConversation({ id: " builtin-paper ", name: " Paper polish ", description: "rewrite" });
            expect(seen).toEqual([{
                expert: expect.objectContaining({ id: "builtin-paper", name: "Paper polish", description: "rewrite" }),
            }]);
        } finally {
            window.removeEventListener(OPEN_EXPERT_CONVERSATION_EVENT, listener);
        }
    });

    it("ignores experts without an id", () => {
        const listener = vi.fn();
        window.addEventListener(OPEN_EXPERT_CONVERSATION_EVENT, listener);
        try {
            openExpertConversation({ id: "  ", name: "Ghost" });
            expect(listener).not.toHaveBeenCalled();
        } finally {
            window.removeEventListener(OPEN_EXPERT_CONVERSATION_EVENT, listener);
        }
    });
});
