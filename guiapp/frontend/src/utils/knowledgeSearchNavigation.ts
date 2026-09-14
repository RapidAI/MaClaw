import { openSettingsTab } from "./settingsNavigation";

export const KNOWLEDGE_SEARCH_EVENT = "maclaw:knowledge-search";

export type KnowledgeSearchOpenDetail = {
    query?: string;
    sourceId?: string;
};

let pendingKnowledgeSearch: KnowledgeSearchOpenDetail | null = null;

export function openKnowledgeSearch(detail: KnowledgeSearchOpenDetail = {}): void {
    pendingKnowledgeSearch = {
        query: String(detail.query || "").trim() || undefined,
        sourceId: String(detail.sourceId || "").trim() || undefined,
    };
    if (typeof window !== "undefined") {
        window.dispatchEvent(new CustomEvent(KNOWLEDGE_SEARCH_EVENT, { detail: { ...pendingKnowledgeSearch } }));
    }
    openSettingsTab("knowledge");
}

export function peekPendingKnowledgeSearch(): KnowledgeSearchOpenDetail | null {
    return pendingKnowledgeSearch;
}

export function consumePendingKnowledgeSearch(): KnowledgeSearchOpenDetail | null {
    const value = pendingKnowledgeSearch;
    pendingKnowledgeSearch = null;
    return value;
}
