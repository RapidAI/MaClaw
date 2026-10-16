import { describe, expect, it } from "vitest";
import {
    filterExpertHits,
    filterMobileLibraryHits,
    headerLibrarySearchJobs,
    isOpenableExpert,
    knowledgeHitPreview,
    knowledgeHitTitle,
    knowledgeSearchHits,
    matchLibraryQuery,
    searchHeaderLibraries,
} from "../unifiedHeaderSearch";

describe("unifiedHeaderSearch", () => {
    it("matches library items by title, preview, filename, or id", () => {
        expect(matchLibraryQuery("Quarterly report draft.docx", "report")).toBe(true);
        expect(matchLibraryQuery("notes", "report")).toBe(false);
        expect(matchLibraryQuery("anything", "")).toBe(true);
    });

    it("filters mobile library items and keeps audio/document kinds", () => {
        const hits = filterMobileLibraryHits([
            { id: "d1", title: "Quarterly report", preview: "Q3 summary", type: "document" },
            { id: "a1", title: "Team standup", type: "audio", source_filename: "standup.m4a" },
            { id: "d2", title: "Unrelated memo", preview: "office snacks" },
            { id: "", title: "Missing id" },
        ], "stand", 8);

        expect(hits).toEqual([
            expect.objectContaining({ id: "a1", title: "Team standup", type: "audio" }),
        ]);
    });

    it("returns recent files when the query is empty", () => {
        const hits = filterMobileLibraryHits([
            { id: "d1", title: "Alpha" },
            { id: "d2", title: "Beta" },
            { id: "d3", title: "Gamma" },
        ], "", 2);
        expect(hits.map((hit) => hit.id)).toEqual(["d1", "d2"]);
    });

    it("maps knowledge search rows onto display hits", () => {
        expect(knowledgeHitTitle({ card_title: "ACL card", source: { title: "Policy" } })).toBe("ACL card");
        expect(knowledgeHitPreview({ snippet: "Deny by default", summary: "unused" })).toBe("Deny by default");
        const hits = knowledgeSearchHits([
            { node_id: "n1", node_title: "Gateway", snippet: "edge topology", source: { id: "s1", title: "Network" }, result_type: "node" },
            { node_id: "n1", node_title: "Duplicate" },
            { card_id: "c1", card_title: "Risk card", source: { id: "s2" } },
        ], 8);
        expect(hits).toEqual([
            expect.objectContaining({ id: "n1", title: "Gateway", sourceId: "s1", resultType: "node" }),
            expect.objectContaining({ id: "c1", title: "Risk card", sourceId: "s2" }),
        ]);
    });

    it("resolves files without waiting for knowledge search", async () => {
        let resolveKnowledge: (value: unknown[]) => void = () => {};
        const jobs = headerLibrarySearchJobs("report", {
            listMobileLibraryItems: async () => [{ id: "d1", title: "Q3 report" }],
            knowledgeSearch: () => new Promise(resolve => { resolveKnowledge = resolve; }),
        });
        await expect(jobs.files).resolves.toEqual([expect.objectContaining({ id: "d1", title: "Q3 report" })]);
        resolveKnowledge([]);
        await expect(jobs.knowledge).resolves.toEqual([]);
    });

    it("searches files even when knowledge lookup fails", async () => {
        const result = await searchHeaderLibraries("report", {
            listMobileLibraryItems: async () => [{ id: "d1", title: "Q3 report" }],
            knowledgeSearch: async () => { throw new Error("knowledge offline"); },
        });
        expect(result.files).toEqual([expect.objectContaining({ id: "d1", title: "Q3 report" })]);
        expect(result.knowledge).toEqual([]);
    });

    it("skips files, knowledge, and experts for an empty query", async () => {
        let filesCalled = false;
        let knowledgeCalled = false;
        let expertsCalled = false;
        const result = await searchHeaderLibraries("   ", {
            listMobileLibraryItems: async () => {
                filesCalled = true;
                return [{ id: "d1", title: "Notes" }];
            },
            knowledgeSearch: async () => {
                knowledgeCalled = true;
                return [{ node_id: "n1", node_title: "Should not run" }];
            },
            listExperts: async () => {
                expertsCalled = true;
                return [{ id: "e1", name: "Should not run" }];
            },
        });
        expect(filesCalled).toBe(false);
        expect(knowledgeCalled).toBe(false);
        expect(expertsCalled).toBe(false);
        expect(result).toEqual({ files: [], knowledge: [], experts: [] });
    });

    it("keeps installed experts and skips uninstalled industry placeholders", () => {
        expect(isOpenableExpert({ id: "builtin-paper" })).toBe(true);
        expect(isOpenableExpert({ id: "ind-1", managed_industry: true, industry_installed: true })).toBe(true);
        expect(isOpenableExpert({ id: "ind-2", managed_industry: true, industry_installed: false })).toBe(false);
        const hits = filterExpertHits([
            { id: "builtin-paper", name: "Paper polish", description: "Rewrite academic drafts" },
            { id: "ind-locked", name: "Legal reviewer", managed_industry: true, industry_installed: false },
            { id: "writer", name: "Contract writer", description: "office memos" },
        ], "paper", 8);
        expect(hits).toEqual([
            expect.objectContaining({ id: "builtin-paper", title: "Paper polish", preview: "Rewrite academic drafts" }),
        ]);
    });

    it("searches experts from a ListExperts JSON payload", async () => {
        const result = await searchHeaderLibraries("polish", {
            listMobileLibraryItems: async () => [],
            knowledgeSearch: async () => [],
            listExperts: async () => JSON.stringify([
                { id: "builtin-paper", name: "Paper polish", description: "Rewrite academic drafts", icon: "📝" },
            ]),
        });
        expect(result.experts).toEqual([
            expect.objectContaining({ id: "builtin-paper", title: "Paper polish", icon: "📝" }),
        ]);
    });
});
