import { afterEach, describe, expect, it } from "vitest";
import { OPEN_SETTINGS_EVENT } from "../settingsNavigation";
import { consumePendingKnowledgeSearch, KNOWLEDGE_SEARCH_EVENT, openKnowledgeSearch, peekPendingKnowledgeSearch } from "../knowledgeSearchNavigation";

describe("knowledgeSearchNavigation", () => {
    afterEach(() => {
        consumePendingKnowledgeSearch();
    });

    it("opens the knowledge settings tab with a pending search query", () => {
        const seen: Array<{ type: string; detail: unknown }> = [];
        const listener = (event: Event) => seen.push({ type: event.type, detail: (event as CustomEvent).detail });
        window.addEventListener(KNOWLEDGE_SEARCH_EVENT, listener);
        window.addEventListener(OPEN_SETTINGS_EVENT, listener);
        try {
            openKnowledgeSearch({ query: " gateway ", sourceId: " src-1 " });
            expect(seen).toEqual([
                { type: KNOWLEDGE_SEARCH_EVENT, detail: { query: "gateway", sourceId: "src-1" } },
                { type: OPEN_SETTINGS_EVENT, detail: { tab: "knowledge" } },
            ]);
            expect(peekPendingKnowledgeSearch()).toEqual({ query: "gateway", sourceId: "src-1" });
            expect(consumePendingKnowledgeSearch()).toEqual({ query: "gateway", sourceId: "src-1" });
            expect(consumePendingKnowledgeSearch()).toBeNull();
        } finally {
            window.removeEventListener(KNOWLEDGE_SEARCH_EVENT, listener);
            window.removeEventListener(OPEN_SETTINGS_EVENT, listener);
        }
    });
});
