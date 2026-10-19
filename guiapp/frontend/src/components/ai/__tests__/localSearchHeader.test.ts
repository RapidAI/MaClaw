import { describe, expect, it } from "vitest";
import { knowledgeSearchHits } from "../unifiedHeaderSearch";
import { headerCloudHitsFromLocalSearch, knowledgePayloadsFromLocalSearch } from "../localSearchHeader";

describe("localSearchHeader", () => {
    it("maps knowledge citation payloads through the header hit helper", () => {
        const payloads = knowledgePayloadsFromLocalSearch({
            corpus: "knowledge",
            route: "hybrid",
            freshness: "fresh",
            hits: [
                {
                    locator: "card:c1",
                    knowledge: {
                        card_id: "c1",
                        card_title: "Gateway",
                        snippet: "edge topology",
                        source: { id: "s1", title: "Network" },
                        result_type: "card",
                    },
                },
                {
                    locator: "node:n1",
                    knowledge: {
                        node_id: "n1",
                        node_title: "Duplicate",
                        snippet: "same card shown twice",
                        source: { id: "s1" },
                    },
                },
                { locator: "card:c1", title: "missing citation" },
            ],
        });
        expect(knowledgeSearchHits(payloads as Parameters<typeof knowledgeSearchHits>[0], 8)).toEqual([
            expect.objectContaining({ id: "c1", title: "Gateway", sourceId: "s1", resultType: "card" }),
            expect.objectContaining({ id: "n1", title: "Duplicate", sourceId: "s1" }),
        ]);
    });

    it("maps cloud scan hits onto openable header rows", () => {
        const hits = headerCloudHitsFromLocalSearch({
            corpus: "cloud_workspace",
            route: "scan",
            freshness: "unindexed",
            hits: [
                {
                    locator: "cws_a/papers/brief.md",
                    title: "brief.md",
                    snippet: "经营材料 · 竞品分析",
                    workspace_id: "cws_a",
                    workspace_name: "经营材料",
                    project_path: "D:/tasks/notes",
                    relative_path: "papers/brief.md",
                    match: "content",
                    tags: ["cloud_workspace:cws_a", ""],
                },
                { title: "missing path", snippet: "no locator" },
            ],
        });
        expect(hits).toEqual([
            expect.objectContaining({
                id: "cws_a/papers/brief.md",
                workspaceId: "cws_a",
                workspaceName: "经营材料",
                projectPath: "D:/tasks/notes",
                relativePath: "papers/brief.md",
                preview: "经营材料 · 竞品分析",
                match: "content",
                tags: ["cloud_workspace:cws_a"],
            }),
        ]);
    });

    it("returns no hits when the result has no hit list", () => {
        expect(knowledgePayloadsFromLocalSearch(null)).toEqual([]);
        expect(headerCloudHitsFromLocalSearch({ hits: null })).toEqual([]);
    });
});
