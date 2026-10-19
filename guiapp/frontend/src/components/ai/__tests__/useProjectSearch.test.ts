// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useProjectSearch } from "../useProjectSearch";
import { ListExperts, ListMobileLibraryItems, LocalSearch, SearchTasks } from "../../../../wailsjs/go/main/App";

function localSearchResult(hits: unknown[] = []) {
    return { hits } as Awaited<ReturnType<typeof LocalSearch>>;
}

vi.mock("../../../../wailsjs/go/main/App", () => ({
    SearchTasks: vi.fn().mockResolvedValue([]),
    ListMobileLibraryItems: vi.fn().mockResolvedValue([]),
    LocalSearch: vi.fn().mockResolvedValue({ hits: [] }),
    ListExperts: vi.fn().mockResolvedValue("[]"),
}));

describe("useProjectSearch", () => {
    beforeEach(() => {
        vi.mocked(SearchTasks).mockClear().mockResolvedValue([]);
        vi.mocked(ListMobileLibraryItems).mockClear().mockResolvedValue([]);
        vi.mocked(LocalSearch).mockClear().mockResolvedValue(localSearchResult());
        vi.mocked(ListExperts).mockClear().mockResolvedValue("[]");
        delete (window as unknown as { go?: unknown }).go;
    });

    it("puts cloud workspace file hits into cloudResults", async () => {
        vi.mocked(LocalSearch).mockImplementation(async (req) => {
            if (req.corpus !== "cloud_workspace") return localSearchResult();
            return localSearchResult([{
                locator: "cws_1/notes/预算说明.txt",
                workspace_id: "cws_1",
                workspace_name: "经营分析",
                project_path: "C:/cloud/cws_1",
                relative_path: "notes/预算说明.txt",
                title: "预算说明.txt",
                snippet: "经营分析 · notes/预算说明.txt",
                match: "name",
                tags: ["cloud_workspace:cws_1"],
            }]);
        });

        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("预算");
        });

        await waitFor(() => expect(result.current.cloudResults).toHaveLength(1));
        expect(LocalSearch).toHaveBeenCalledWith({
            corpus: "cloud_workspace",
            query: "预算",
            limit: 8,
        });
        expect(result.current.cloudResults[0]).toMatchObject({
            workspaceId: "cws_1",
            relativePath: "notes/预算说明.txt",
            title: "预算说明.txt",
            match: "name",
        });
    });

    it("puts knowledge citation hits into knowledgeResults", async () => {
        vi.mocked(LocalSearch).mockImplementation(async (req) => {
            if (req.corpus !== "knowledge") return localSearchResult();
            return localSearchResult([{
                knowledge: {
                    card_id: "c1",
                    card_title: "Gateway",
                    snippet: "edge topology",
                    source: { id: "s1", title: "Network" },
                    result_type: "card",
                },
            }]);
        });

        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("gateway");
        });

        await waitFor(() => expect(result.current.knowledgeResults).toHaveLength(1));
        expect(LocalSearch).toHaveBeenCalledWith({
            corpus: "knowledge",
            query: "gateway",
            retrieval: "auto",
            limit: 8,
        });
        expect(result.current.knowledgeResults[0]).toMatchObject({
            id: "c1",
            title: "Gateway",
            sourceId: "s1",
            resultType: "card",
        });
    });

    it("puts data directory workspace hits into dataDirResults", async () => {
        const searchData = vi.fn().mockResolvedValue([{
            id: "notes-1/papers/brief.md",
            task_name: "经营材料",
            project_path: "D:/data/tasks/notes-1",
            relative_path: "papers/brief.md",
            title: "brief.md",
            preview: "经营材料 · 竞品分析",
            match: "content",
            tags: ["task_management"],
        }]);
        (window as unknown as { go: unknown }).go = { main: { App: { SearchDataDirectoryWorkspaces: searchData } } };

        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("竞品");
        });

        await waitFor(() => expect(result.current.dataDirResults).toHaveLength(1));
        expect(searchData).toHaveBeenCalledWith("竞品", 8);
        expect(result.current.dataDirResults[0]).toMatchObject({
            taskName: "经营材料",
            projectPath: "D:/data/tasks/notes-1",
            relativePath: "papers/brief.md",
            match: "content",
        });
    });

    it("refreshes to the task list when the header sends an empty query while open", async () => {
        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("report");
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("report", 20));
        vi.mocked(SearchTasks).mockClear();
        vi.mocked(ListMobileLibraryItems).mockClear();
        vi.mocked(LocalSearch).mockClear();
        vi.mocked(ListExperts).mockClear();

        await act(async () => {
            result.current.openWithQuery("   ");
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("", 20));
        expect(ListMobileLibraryItems).not.toHaveBeenCalled();
        expect(LocalSearch).not.toHaveBeenCalled();
        expect(ListExperts).not.toHaveBeenCalled();
    });

    it("still searches after a header open is cancelled before the panel stays open", async () => {
        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("report");
            result.current.close();
        });
        vi.mocked(SearchTasks).mockClear();

        await act(async () => {
            result.current.toggle();
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("", 20));
    });

    it("debounces header query updates while search is already open", async () => {
        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("report");
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("report", 20));
        vi.mocked(SearchTasks).mockClear();

        await act(async () => {
            result.current.openWithQuery("repo");
            result.current.openWithQuery("reports");
        });
        expect(SearchTasks).not.toHaveBeenCalled();
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("reports", 20));
        expect(SearchTasks).toHaveBeenCalledTimes(1);
    });

    it("updates the draft query during IME composition without searching", async () => {
        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("");
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalled());
        vi.mocked(SearchTasks).mockClear();
        vi.mocked(ListMobileLibraryItems).mockClear();

        await act(async () => {
            result.current.onQueryDraft("zhuan");
        });
        expect(result.current.query).toBe("zhuan");
        expect(SearchTasks).not.toHaveBeenCalled();
        expect(ListMobileLibraryItems).not.toHaveBeenCalled();
    });

    it("keeps the previous cloud hits until the next search resolves", async () => {
        let resolveSecond: (value: Awaited<ReturnType<typeof LocalSearch>>) => void = () => {};
        vi.mocked(LocalSearch).mockImplementation((req) => {
            if (req.corpus !== "cloud_workspace") return Promise.resolve(localSearchResult());
            if (req.query === "a") {
                return Promise.resolve(localSearchResult([{
                    locator: "cws_1/a.txt",
                    workspace_id: "cws_1",
                    project_path: "C:/cloud/cws_1",
                    relative_path: "a.txt",
                    title: "a.txt",
                    snippet: "a",
                    match: "name",
                    tags: [],
                }]));
            }
            return new Promise<Awaited<ReturnType<typeof LocalSearch>>>(resolve => { resolveSecond = resolve; });
        });
        vi.mocked(SearchTasks)
            .mockResolvedValueOnce([{ id: "t1", name: "First", project_path: "D:/t1", has_output: true } as never])
            .mockResolvedValueOnce([{ id: "t2", name: "Second", project_path: "D:/t2", has_output: true } as never]);

        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("a");
        });
        await waitFor(() => expect(result.current.cloudResults).toHaveLength(1));
        await waitFor(() => expect(result.current.results.map(item => item.name)).toEqual(["First"]));
        await waitFor(() => expect(result.current.loading).toBe(false));

        await act(async () => {
            result.current.openWithQuery("ab");
        });
        expect(result.current.query).toBe("a");
        expect(result.current.loading).toBe(false);
        await waitFor(() => expect(result.current.loading).toBe(true));
        expect(result.current.query).toBe("a");
        expect(result.current.pendingQuery).toBe("ab");
        expect(result.current.cloudResults).toHaveLength(1);
        await act(async () => { await Promise.resolve(); });
        expect(result.current.results.map(item => item.name)).toEqual(["First"]);

        await act(async () => {
            resolveSecond(localSearchResult());
        });
        await waitFor(() => expect(result.current.cloudResults).toHaveLength(0));
        expect(result.current.results.map(item => item.name)).toEqual(["Second"]);
        expect(result.current.query).toBe("ab");
    });

    it("reuses the expert and file lists while typing", async () => {
        const { result } = renderHook(() => useProjectSearch("en"));
        await act(async () => {
            result.current.openWithQuery("one");
        });
        await waitFor(() => expect(ListExperts).toHaveBeenCalledTimes(1));
        expect(ListMobileLibraryItems).toHaveBeenCalledTimes(1);

        await act(async () => {
            result.current.openWithQuery("two");
        });
        await waitFor(() => expect(SearchTasks).toHaveBeenCalledWith("two", 20));
        expect(ListExperts).toHaveBeenCalledTimes(1);
        expect(ListMobileLibraryItems).toHaveBeenCalledTimes(1);
    });
});
